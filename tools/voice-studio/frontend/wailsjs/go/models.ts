export namespace main {
	
	export class Counts {
	    replaced: number;
	    rendered: number;
	    takes: number;
	    bytes: number;
	
	    static createFrom(source: any = {}) {
	        return new Counts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.replaced = source["replaced"];
	        this.rendered = source["rendered"];
	        this.takes = source["takes"];
	        this.bytes = source["bytes"];
	    }
	}
	export class EngineView {
	    url: string;
	    dir: string;
	    venv: string;
	    ready: boolean;
	    managed: boolean;
	    note?: string;
	
	    static createFrom(source: any = {}) {
	        return new EngineView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.dir = source["dir"];
	        this.venv = source["venv"];
	        this.ready = source["ready"];
	        this.managed = source["managed"];
	        this.note = source["note"];
	    }
	}
	export class View {
	    engine: EngineView;
	    builtInRole: string;
	    roles: voice.Role[];
	    activeRole: string;
	    activeRoleName: string;
	    roleError?: string;
	    available: number;
	    lines: voice.LineState[];
	    counts: Counts;
	    dir: string;
	    busy: boolean;
	    progress?: string;
	    done: number;
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new View(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.engine = this.convertValues(source["engine"], EngineView);
	        this.builtInRole = source["builtInRole"];
	        this.roles = this.convertValues(source["roles"], voice.Role);
	        this.activeRole = source["activeRole"];
	        this.activeRoleName = source["activeRoleName"];
	        this.roleError = source["roleError"];
	        this.available = source["available"];
	        this.lines = this.convertValues(source["lines"], voice.LineState);
	        this.counts = this.convertValues(source["counts"], Counts);
	        this.dir = source["dir"];
	        this.busy = source["busy"];
	        this.progress = source["progress"];
	        this.done = source["done"];
	        this.total = source["total"];
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

}

export namespace voice {
	
	export class Role {
	    id: string;
	    name: string;
	    gpt: string;
	    sovits: string;
	    ref: string;
	    promptText: string;
	    promptLang: string;
	    lang: string;
	    speed: number;
	    temperature: number;
	    topK: number;
	    topP: number;
	    repetitionPenalty: number;
	    seed: number;
	
	    static createFrom(source: any = {}) {
	        return new Role(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.gpt = source["gpt"];
	        this.sovits = source["sovits"];
	        this.ref = source["ref"];
	        this.promptText = source["promptText"];
	        this.promptLang = source["promptLang"];
	        this.lang = source["lang"];
	        this.speed = source["speed"];
	        this.temperature = source["temperature"];
	        this.topK = source["topK"];
	        this.topP = source["topP"];
	        this.repetitionPenalty = source["repetitionPenalty"];
	        this.seed = source["seed"];
	    }
	}
	export class Take {
	    version: number;
	    role: string;
	    roleName: string;
	    text: string;
	    file: string;
	    bytes: number;
	    durationMs: number;
	    params: Role;
	    // Go type: time
	    generatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Take(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.role = source["role"];
	        this.roleName = source["roleName"];
	        this.text = source["text"];
	        this.file = source["file"];
	        this.bytes = source["bytes"];
	        this.durationMs = source["durationMs"];
	        this.params = this.convertValues(source["params"], Role);
	        this.generatedAt = this.convertValues(source["generatedAt"], null);
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
	export class LineState {
	    line: string;
	    text: string;
	    label: string;
	    embedded: boolean;
	    builtInRole?: string;
	    effect: string;
	    effectSource: string;
	    activeRole: string;
	    activeRoleName?: string;
	    chosenForActiveRole?: boolean;
	    chosenRoleId?: string;
	    roleError?: string;
	    takes?: Take[];
	    chosen: number;
	    chosenRole?: string;
	    // Go type: time
	    chosenAt?: any;
	    durationMs?: number;
	    missing?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LineState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.text = source["text"];
	        this.label = source["label"];
	        this.embedded = source["embedded"];
	        this.builtInRole = source["builtInRole"];
	        this.effect = source["effect"];
	        this.effectSource = source["effectSource"];
	        this.activeRole = source["activeRole"];
	        this.activeRoleName = source["activeRoleName"];
	        this.chosenForActiveRole = source["chosenForActiveRole"];
	        this.chosenRoleId = source["chosenRoleId"];
	        this.roleError = source["roleError"];
	        this.takes = this.convertValues(source["takes"], Take);
	        this.chosen = source["chosen"];
	        this.chosenRole = source["chosenRole"];
	        this.chosenAt = this.convertValues(source["chosenAt"], null);
	        this.durationMs = source["durationMs"];
	        this.missing = source["missing"];
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
	

}

