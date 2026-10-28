export namespace desktop {
	
	export class HotkeyView {
	    enabled: boolean;
	    show: string;
	    hide: string;
	    active: boolean;
	    problems?: string[];
	    note?: string;
	
	    static createFrom(source: any = {}) {
	        return new HotkeyView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.show = source["show"];
	        this.hide = source["hide"];
	        this.active = source["active"];
	        this.problems = source["problems"];
	        this.note = source["note"];
	    }
	}

}

export namespace main {
	
	export class BubbleItem {
	    id: string;
	    title: string;
	    due?: string;
	    status?: string;
	
	    static createFrom(source: any = {}) {
	        return new BubbleItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.due = source["due"];
	        this.status = source["status"];
	    }
	}
	export class BubbleView {
	    kind: string;
	    title: string;
	    items?: BubbleItem[];
	    more?: number;
	    showTodoAction?: boolean;
	    x: number;
	    y: number;
	    w: number;
	    h: number;
	    onRight: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BubbleView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.items = this.convertValues(source["items"], BubbleItem);
	        this.more = source["more"];
	        this.showTodoAction = source["showTodoAction"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.w = source["w"];
	        this.h = source["h"];
	        this.onRight = source["onRight"];
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
	export class DragState {
	    x: number;
	    y: number;
	    scale: number;
	
	    static createFrom(source: any = {}) {
	        return new DragState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.scale = source["scale"];
	    }
	}
	export class HotkeySettingsInput {
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HotkeySettingsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	    }
	}
	export class PluginSurfaceRect {
	    index: number;
	    x: number;
	    y: number;
	    w: number;
	    h: number;
	    r: number;
	    ellipse: boolean;
	    tailX: number;
	    hidden: boolean;
	    room: number;
	
	    static createFrom(source: any = {}) {
	        return new PluginSurfaceRect(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.w = source["w"];
	        this.h = source["h"];
	        this.r = source["r"];
	        this.ellipse = source["ellipse"];
	        this.tailX = source["tailX"];
	        this.hidden = source["hidden"];
	        this.room = source["room"];
	    }
	}
	export class PluginSurfaceView {
	    pluginId: string;
	    rects: PluginSurfaceRect[];
	
	    static createFrom(source: any = {}) {
	        return new PluginSurfaceView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pluginId = source["pluginId"];
	        this.rects = this.convertValues(source["rects"], PluginSurfaceRect);
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
	export class Layout {
	    windowW: number;
	    windowH: number;
	    petX: number;
	    petY: number;
	    petW: number;
	    petH: number;
	    panelX: number;
	    panelY: number;
	    panelW: number;
	    panelH: number;
	    panelOnRight: boolean;
	    scale: number;
	    bubble?: BubbleView;
	    pluginSurface?: PluginSurfaceView;
	
	    static createFrom(source: any = {}) {
	        return new Layout(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.windowW = source["windowW"];
	        this.windowH = source["windowH"];
	        this.petX = source["petX"];
	        this.petY = source["petY"];
	        this.petW = source["petW"];
	        this.petH = source["petH"];
	        this.panelX = source["panelX"];
	        this.panelY = source["panelY"];
	        this.panelW = source["panelW"];
	        this.panelH = source["panelH"];
	        this.panelOnRight = source["panelOnRight"];
	        this.scale = source["scale"];
	        this.bubble = this.convertValues(source["bubble"], BubbleView);
	        this.pluginSurface = this.convertValues(source["pluginSurface"], PluginSurfaceView);
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
	
	
	export class PluginView {
	    id: string;
	    name: string;
	    version: string;
	    author: string;
	    description: string;
	    enabled: boolean;
	    permissions: string[];
	    granted: string[];
	    pending: string[];
	    netHosts?: string[];
	    panel: boolean;
	    panelSlot?: string;
	    surface: boolean;
	    background: boolean;
	    entry?: string;
	    dir: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.author = source["author"];
	        this.description = source["description"];
	        this.enabled = source["enabled"];
	        this.permissions = source["permissions"];
	        this.granted = source["granted"];
	        this.pending = source["pending"];
	        this.netHosts = source["netHosts"];
	        this.panel = source["panel"];
	        this.panelSlot = source["panelSlot"];
	        this.surface = source["surface"];
	        this.background = source["background"];
	        this.entry = source["entry"];
	        this.dir = source["dir"];
	        this.error = source["error"];
	    }
	}
	export class ReminderSettingsInput {
	    onStartup: boolean;
	    enabled: boolean;
	    intervalMin: number;
	    jitterMax: number;
	    from: string;
	    to: string;
	    maxItems: number;
	    autoHideSec: number;
	    whenEmpty: boolean;
	    silentFullscreen: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ReminderSettingsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.onStartup = source["onStartup"];
	        this.enabled = source["enabled"];
	        this.intervalMin = source["intervalMin"];
	        this.jitterMax = source["jitterMax"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.maxItems = source["maxItems"];
	        this.autoHideSec = source["autoHideSec"];
	        this.whenEmpty = source["whenEmpty"];
	        this.silentFullscreen = source["silentFullscreen"];
	    }
	}
	export class State {
	    config: store.Config;
	    petImageUrl: string;
	    hasCustomImage: boolean;
	    dataDir: string;
	    autostart: boolean;
	    now: string;
	    hotkeys: desktop.HotkeyView;
	    petVisible: boolean;
	    todoBadge: store.Stats;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.config = this.convertValues(source["config"], store.Config);
	        this.petImageUrl = source["petImageUrl"];
	        this.hasCustomImage = source["hasCustomImage"];
	        this.dataDir = source["dataDir"];
	        this.autostart = source["autostart"];
	        this.now = source["now"];
	        this.hotkeys = this.convertValues(source["hotkeys"], desktop.HotkeyView);
	        this.petVisible = source["petVisible"];
	        this.todoBadge = this.convertValues(source["todoBadge"], store.Stats);
	        this.warnings = source["warnings"];
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

export namespace store {
	
	export class Position {
	    x: number;
	    y: number;
	
	    static createFrom(source: any = {}) {
	        return new Position(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	    }
	}
	export class Config {
	    petImageFile?: string;
	    originalPath?: string;
	    petScale: number;
	    position?: Position;
	    snapToEdge: boolean;
	    edgeMargin: number;
	    autostart: boolean;
	    marketBaseUrl: string;
	    globalHotkeys: boolean;
	    reminderOnStartup: boolean;
	    reminderEnabled: boolean;
	    reminderIntervalMin: number;
	    reminderJitterMaxMin: number;
	    reminderFrom: string;
	    reminderTo: string;
	    reminderMaxItems: number;
	    reminderAutoHideSec: number;
	    remindWhenEmpty: boolean;
	    llmEnabled: boolean;
	    llmProvider: string;
	    llmBaseUrl: string;
	    llmModel: string;
	    llmTemperature: number;
	    llmMaxTokens: number;
	    llmTimeoutSec: number;
	    llmSystemPrompt: string;
	    llmHistoryTurns: number;
	    llmProactive: boolean;
	    llmProactiveMinGapMin: number;
	    llmDailyBudget: number;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.petImageFile = source["petImageFile"];
	        this.originalPath = source["originalPath"];
	        this.petScale = source["petScale"];
	        this.position = this.convertValues(source["position"], Position);
	        this.snapToEdge = source["snapToEdge"];
	        this.edgeMargin = source["edgeMargin"];
	        this.autostart = source["autostart"];
	        this.marketBaseUrl = source["marketBaseUrl"];
	        this.globalHotkeys = source["globalHotkeys"];
	        this.reminderOnStartup = source["reminderOnStartup"];
	        this.reminderEnabled = source["reminderEnabled"];
	        this.reminderIntervalMin = source["reminderIntervalMin"];
	        this.reminderJitterMaxMin = source["reminderJitterMaxMin"];
	        this.reminderFrom = source["reminderFrom"];
	        this.reminderTo = source["reminderTo"];
	        this.reminderMaxItems = source["reminderMaxItems"];
	        this.reminderAutoHideSec = source["reminderAutoHideSec"];
	        this.remindWhenEmpty = source["remindWhenEmpty"];
	        this.llmEnabled = source["llmEnabled"];
	        this.llmProvider = source["llmProvider"];
	        this.llmBaseUrl = source["llmBaseUrl"];
	        this.llmModel = source["llmModel"];
	        this.llmTemperature = source["llmTemperature"];
	        this.llmMaxTokens = source["llmMaxTokens"];
	        this.llmTimeoutSec = source["llmTimeoutSec"];
	        this.llmSystemPrompt = source["llmSystemPrompt"];
	        this.llmHistoryTurns = source["llmHistoryTurns"];
	        this.llmProactive = source["llmProactive"];
	        this.llmProactiveMinGapMin = source["llmProactiveMinGapMin"];
	        this.llmDailyBudget = source["llmDailyBudget"];
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
	
	export class Stats {
	    total: number;
	    open: number;
	    overdue: number;
	    dueSoon: number;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.open = source["open"];
	        this.overdue = source["overdue"];
	        this.dueSoon = source["dueSoon"];
	    }
	}

}

