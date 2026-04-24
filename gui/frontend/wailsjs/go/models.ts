export namespace service {
	
	export class MemcachedServer {
	    host: string;
	    port: number;
	
	    static createFrom(source: any = {}) {
	        return new MemcachedServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	    }
	}
	export class Context {
	    id: string;
	    name: string;
	    servers: MemcachedServer[];
	
	    static createFrom(source: any = {}) {
	        return new Context(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.servers = this.convertValues(source["servers"], MemcachedServer);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class OperationResult {
	    success: boolean;
	    data: string;
	    error?: string;
	    key?: string;
	    value?: string;
	    ttl?: number;
	    lastAccessedTime?: number;
	    cas?: number;
	    flags?: number;
	    size?: number;
	    hitBefore?: boolean;
	    opaque?: number;
	    valueKind?: string;
	
	    static createFrom(source: any = {}) {
	        return new OperationResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = source["data"];
	        this.error = source["error"];
	        this.key = source["key"];
	        this.value = source["value"];
	        this.ttl = source["ttl"];
	        this.lastAccessedTime = source["lastAccessedTime"];
	        this.cas = source["cas"];
	        this.flags = source["flags"];
	        this.size = source["size"];
	        this.hitBefore = source["hitBefore"];
	        this.opaque = source["opaque"];
	        this.valueKind = source["valueKind"];
	    }
	}

}

