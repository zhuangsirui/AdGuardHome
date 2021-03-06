package aghnet

import (
	"bufio"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/AdguardTeam/AdGuardHome/internal/aghos"
	"github.com/AdguardTeam/golibs/log"
	"github.com/fsnotify/fsnotify"
	"github.com/miekg/dns"
)

type onChangedT func()

// EtcHostsContainer - automatic DNS records
//
// TODO(e.burkov): Move the logic under interface.  Refactor.  Probably remove
// the resolving logic.
type EtcHostsContainer struct {
	// lock protects table and tableReverse.
	lock sync.RWMutex
	// table is the host-to-IPs map.
	table map[string][]net.IP
	// tableReverse is the IP-to-hosts map.
	//
	// TODO(a.garipov): Make better use of newtypes.  Perhaps a custom map.
	tableReverse map[string][]string

	hostsFn   string            // path to the main hosts-file
	hostsDirs []string          // paths to OS-specific directories with hosts-files
	watcher   *fsnotify.Watcher // file and directory watcher object

	// onlyWritesChan used to contain only writing events from watcher.
	onlyWritesChan chan fsnotify.Event

	onChanged onChangedT // notification to other modules
}

// SetOnChanged - set callback function that will be called when the data is changed
func (ehc *EtcHostsContainer) SetOnChanged(onChanged onChangedT) {
	ehc.onChanged = onChanged
}

// Notify other modules
func (ehc *EtcHostsContainer) notify() {
	if ehc.onChanged == nil {
		return
	}
	ehc.onChanged()
}

// Init - initialize
// hostsFn: Override default name for the hosts-file (optional)
func (ehc *EtcHostsContainer) Init(hostsFn string) {
	ehc.table = make(map[string][]net.IP)
	ehc.onlyWritesChan = make(chan fsnotify.Event, 2)

	ehc.hostsFn = "/etc/hosts"
	if runtime.GOOS == "windows" {
		ehc.hostsFn = os.ExpandEnv("$SystemRoot\\system32\\drivers\\etc\\hosts")
	}
	if len(hostsFn) != 0 {
		ehc.hostsFn = hostsFn
	}

	if aghos.IsOpenWrt() {
		// OpenWrt: "/tmp/hosts/dhcp.cfg01411c".
		ehc.hostsDirs = append(ehc.hostsDirs, "/tmp/hosts")
	}

	// Load hosts initially
	ehc.updateHosts()

	var err error
	ehc.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		log.Error("etchostscontainer: %s", err)
	}
}

// Start - start module
func (ehc *EtcHostsContainer) Start() {
	if ehc == nil {
		return
	}

	log.Debug("Start etchostscontainer module")

	ehc.updateHosts()

	if ehc.watcher != nil {
		go ehc.watcherLoop()

		err := ehc.watcher.Add(ehc.hostsFn)
		if err != nil {
			log.Error("Error while initializing watcher for a file %s: %s", ehc.hostsFn, err)
		}

		for _, dir := range ehc.hostsDirs {
			err = ehc.watcher.Add(dir)
			if err != nil {
				log.Error("Error while initializing watcher for a directory %s: %s", dir, err)
			}
		}
	}
}

// Close - close module
func (ehc *EtcHostsContainer) Close() {
	if ehc == nil {
		return
	}

	if ehc.watcher != nil {
		_ = ehc.watcher.Close()
	}
	close(ehc.onlyWritesChan)
}

// Process returns the list of IP addresses for the hostname or nil if nothing
// found.
func (ehc *EtcHostsContainer) Process(host string, qtype uint16) []net.IP {
	if qtype == dns.TypePTR {
		return nil
	}

	var ipsCopy []net.IP
	ehc.lock.RLock()
	defer ehc.lock.RUnlock()

	if ips, ok := ehc.table[host]; ok {
		ipsCopy = make([]net.IP, len(ips))
		copy(ipsCopy, ips)
	}

	log.Debug("etchostscontainer: answer: %s -> %v", host, ipsCopy)
	return ipsCopy
}

// ProcessReverse processes a PTR request.  It returns nil if nothing is found.
func (ehc *EtcHostsContainer) ProcessReverse(addr string, qtype uint16) (hosts []string) {
	if qtype != dns.TypePTR {
		return nil
	}

	ipReal := UnreverseAddr(addr)
	if ipReal == nil {
		return nil
	}

	ipStr := ipReal.String()

	ehc.lock.RLock()
	defer ehc.lock.RUnlock()

	hosts = ehc.tableReverse[ipStr]

	if len(hosts) == 0 {
		return nil // not found
	}

	log.Debug("etchostscontainer: reverse-lookup: %s -> %s", addr, hosts)

	return hosts
}

// List returns an IP-to-hostnames table.  It is safe for concurrent use.
func (ehc *EtcHostsContainer) List() (ipToHosts map[string][]string) {
	ehc.lock.RLock()
	defer ehc.lock.RUnlock()

	ipToHosts = make(map[string][]string, len(ehc.tableReverse))
	for k, v := range ehc.tableReverse {
		ipToHosts[k] = v
	}

	return ipToHosts
}

// update table
func (ehc *EtcHostsContainer) updateTable(table map[string][]net.IP, host string, ipAddr net.IP) {
	ips, ok := table[host]
	if ok {
		for _, ip := range ips {
			if ip.Equal(ipAddr) {
				// IP already exists: don't add duplicates
				ok = false
				break
			}
		}
		if !ok {
			ips = append(ips, ipAddr)
			table[host] = ips
		}
	} else {
		table[host] = []net.IP{ipAddr}
		ok = true
	}
	if ok {
		log.Debug("etchostscontainer: added %s -> %s", ipAddr, host)
	}
}

// updateTableRev updates the reverse address table.
func (ehc *EtcHostsContainer) updateTableRev(tableRev map[string][]string, newHost string, ipAddr net.IP) {
	ipStr := ipAddr.String()
	hosts, ok := tableRev[ipStr]
	if !ok {
		tableRev[ipStr] = []string{newHost}
		log.Debug("etchostscontainer: added reverse-address %s -> %s", ipStr, newHost)

		return
	}

	for _, host := range hosts {
		if host == newHost {
			return
		}
	}

	tableRev[ipStr] = append(tableRev[ipStr], newHost)
	log.Debug("etchostscontainer: added reverse-address %s -> %s", ipStr, newHost)
}

// Read IP-hostname pairs from file
// Multiple hostnames per line (per one IP) is supported.
func (ehc *EtcHostsContainer) load(table map[string][]net.IP, tableRev map[string][]string, fn string) {
	f, err := os.Open(fn)
	if err != nil {
		log.Error("etchostscontainer: %s", err)
		return
	}
	defer f.Close()
	r := bufio.NewReader(f)
	log.Debug("etchostscontainer: loading hosts from file %s", fn)

	for done := false; !done; {
		var line string
		line, err = r.ReadString('\n')
		if err == io.EOF {
			done = true
		} else if err != nil {
			log.Error("etchostscontainer: %s", err)

			return
		}

		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		ip := net.ParseIP(fields[0])
		if ip == nil {
			continue
		}

		for i := 1; i != len(fields); i++ {
			host := fields[i]
			if len(host) == 0 {
				break
			}

			sharp := strings.IndexByte(host, '#')
			if sharp == 0 {
				// Skip the comments.
				break
			} else if sharp > 0 {
				host = host[:sharp]
			}

			ehc.updateTable(table, host, ip)
			ehc.updateTableRev(tableRev, host, ip)
			if sharp >= 0 {
				// Skip the comments again.
				break
			}
		}
	}
}

// onlyWrites is a filter for (*fsnotify.Watcher).Events.
func (ehc *EtcHostsContainer) onlyWrites() {
	for event := range ehc.watcher.Events {
		if event.Op&fsnotify.Write == fsnotify.Write {
			ehc.onlyWritesChan <- event
		}
	}
}

// Receive notifications from fsnotify package
func (ehc *EtcHostsContainer) watcherLoop() {
	go ehc.onlyWrites()
	for {
		select {
		case event, ok := <-ehc.onlyWritesChan:
			if !ok {
				return
			}

			// Assume that we sometimes have the same event occurred
			// several times.
			repeat := true
			for repeat {
				select {
				case _, ok = <-ehc.onlyWritesChan:
					repeat = ok
				default:
					repeat = false
				}
			}

			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Debug("etchostscontainer: modified: %s", event.Name)
				ehc.updateHosts()
			}

		case err, ok := <-ehc.watcher.Errors:
			if !ok {
				return
			}
			log.Error("etchostscontainer: %s", err)
		}
	}
}

// updateHosts - loads system hosts
func (ehc *EtcHostsContainer) updateHosts() {
	table := make(map[string][]net.IP)
	tableRev := make(map[string][]string)

	ehc.load(table, tableRev, ehc.hostsFn)

	for _, dir := range ehc.hostsDirs {
		fis, err := ioutil.ReadDir(dir)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.Error("etchostscontainer: Opening directory: %q: %s", dir, err)
			}

			continue
		}

		for _, fi := range fis {
			ehc.load(table, tableRev, filepath.Join(dir, fi.Name()))
		}
	}

	func() {
		ehc.lock.Lock()
		defer ehc.lock.Unlock()

		ehc.table = table
		ehc.tableReverse = tableRev
	}()

	ehc.notify()
}
