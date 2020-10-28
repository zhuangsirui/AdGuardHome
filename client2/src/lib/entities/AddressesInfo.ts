// This file was autogenerated. Please do not change.
// All changes will be overwrited on commit.
export interface IAddressesInfo {
    dns_port?: number;
    interfaces?: any;
    web_port?: number;
}

export default class AddressesInfo {
    readonly _dns_port: number | undefined;

    /**
     * Description: undefined
     * Example: 53
     */
    get dnsPort(): number | undefined {
        return this._dns_port;
    }

    readonly _interfaces: any | undefined;

    /** */
    get interfaces(): any | undefined {
        return this._interfaces;
    }

    readonly _web_port: number | undefined;

    /**
     * Description: undefined
     * Example: 80
     */
    get webPort(): number | undefined {
        return this._web_port;
    }

    constructor(props: IAddressesInfo) {
        if (typeof props.dns_port === 'number') {
            this._dns_port = props.dns_port;
        }
        if (props.interfaces) {
            this._interfaces = props.interfaces;
        }
        if (typeof props.web_port === 'number') {
            this._web_port = props.web_port;
        }
    }

    serialize(): IAddressesInfo {
        const data: IAddressesInfo = {
        };
        if (typeof this._dns_port !== 'undefined') {
            data.dns_port = this._dns_port;
        }
        if (typeof this._interfaces !== 'undefined') {
            data.interfaces = this._interfaces;
        }
        if (typeof this._web_port !== 'undefined') {
            data.web_port = this._web_port;
        }
        return data;
    }

    validate(): string[] {
        const validateRequired = {
            dns_port: !this._dns_port ? true : typeof this._dns_port === 'number',
            web_port: !this._web_port ? true : typeof this._web_port === 'number',
        };
        const isError: string[] = [];
        Object.keys(validateRequired).forEach((key) => {
            if (!(validateRequired as any)[key]) {
                isError.push(key);
            }
        });
        return isError;
    }

    update(props: IAddressesInfo): AddressesInfo {
        return new AddressesInfo(props);
    }

    readonly keys: { [key: string]: string } = {
        dnsPort: 'dns_port',
        interfaces: 'interfaces',
        webPort: 'web_port',
        }
;

    mergeDeepWith(props: Partial<AddressesInfo>): AddressesInfo {
        const updateData: Partial<IAddressesInfo> = {};
        Object.keys(props).forEach((key: keyof AddressesInfo) => {
            const updateKey = this.keys[key] as keyof IAddressesInfo;
            if ((props[key] as any).serialize) {
                (updateData[updateKey] as any) = (props[key] as any).serialize() as Pick<IAddressesInfo, keyof IAddressesInfo>;
            } else {
                (updateData[updateKey] as any) = props[key];
            }
        });
        return new AddressesInfo({ ...this.serialize(), ...updateData });
    }
}