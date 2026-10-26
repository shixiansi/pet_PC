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
	    todoTitle?: string;
	    plan?: string;
	    recordId?: string;
	    items?: BubbleItem[];
	    selectedId?: string;
	    more?: number;
	    allowUnattached: boolean;
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
	        this.todoTitle = source["todoTitle"];
	        this.plan = source["plan"];
	        this.recordId = source["recordId"];
	        this.items = this.convertValues(source["items"], BubbleItem);
	        this.selectedId = source["selectedId"];
	        this.more = source["more"];
	        this.allowUnattached = source["allowUnattached"];
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
	    panelOpen: boolean;
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
	        this.panelOpen = source["panelOpen"];
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
	export class PomodoroSettingsInput {
	    onStartup: boolean;
	    autoStartNextPhase: boolean;
	    remindDuringBreak: boolean;
	    sound: boolean;
	    focusMinutes: number;
	    shortBreakMinutes: number;
	    longBreakMinutes: number;
	    longBreakEvery: number;
	
	    static createFrom(source: any = {}) {
	        return new PomodoroSettingsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.onStartup = source["onStartup"];
	        this.autoStartNextPhase = source["autoStartNextPhase"];
	        this.remindDuringBreak = source["remindDuringBreak"];
	        this.sound = source["sound"];
	        this.focusMinutes = source["focusMinutes"];
	        this.shortBreakMinutes = source["shortBreakMinutes"];
	        this.longBreakMinutes = source["longBreakMinutes"];
	        this.longBreakEvery = source["longBreakEvery"];
	    }
	}
	export class PomodoroStartInput {
	    todoId: string;
	
	    static createFrom(source: any = {}) {
	        return new PomodoroStartInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.todoId = source["todoId"];
	    }
	}
	export class PomodoroView {
	    enabled: boolean;
	    phase: string;
	    runState: string;
	    label: string;
	    remainingSec: number;
	    countdown: string;
	    endsAtMs: number;
	    running: boolean;
	    focusing: boolean;
	    todoTitle?: string;
	    completedCount: number;
	    longBreakEvery: number;
	    untilLongBreak: number;
	    pendingSummary: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PomodoroView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.phase = source["phase"];
	        this.runState = source["runState"];
	        this.label = source["label"];
	        this.remainingSec = source["remainingSec"];
	        this.countdown = source["countdown"];
	        this.endsAtMs = source["endsAtMs"];
	        this.running = source["running"];
	        this.focusing = source["focusing"];
	        this.todoTitle = source["todoTitle"];
	        this.completedCount = source["completedCount"];
	        this.longBreakEvery = source["longBreakEvery"];
	        this.untilLongBreak = source["untilLongBreak"];
	        this.pendingSummary = source["pendingSummary"];
	    }
	}
	export class RecordTotals {
	    count: number;
	    focusMinutes: number;
	
	    static createFrom(source: any = {}) {
	        return new RecordTotals(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.count = source["count"];
	        this.focusMinutes = source["focusMinutes"];
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
	export class ReminderView {
	    enabled: boolean;
	    nextAt?: string;
	    nextInSec: number;
	    suppressed?: string;
	    missed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ReminderView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.nextAt = source["nextAt"];
	        this.nextInSec = source["nextInSec"];
	        this.suppressed = source["suppressed"];
	        this.missed = source["missed"];
	    }
	}
	export class State {
	    config: store.Config;
	    todos: store.Todo[];
	    stats: store.Stats;
	    petImageUrl: string;
	    hasCustomImage: boolean;
	    dataDir: string;
	    autostart: boolean;
	    now: string;
	    pomodoro: PomodoroView;
	    records: store.PomodoroRecord[];
	    recordTotals: RecordTotals;
	    reminder: ReminderView;
	    hotkeys: desktop.HotkeyView;
	    petVisible: boolean;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.config = this.convertValues(source["config"], store.Config);
	        this.todos = this.convertValues(source["todos"], store.Todo);
	        this.stats = this.convertValues(source["stats"], store.Stats);
	        this.petImageUrl = source["petImageUrl"];
	        this.hasCustomImage = source["hasCustomImage"];
	        this.dataDir = source["dataDir"];
	        this.autostart = source["autostart"];
	        this.now = source["now"];
	        this.pomodoro = this.convertValues(source["pomodoro"], PomodoroView);
	        this.records = this.convertValues(source["records"], store.PomodoroRecord);
	        this.recordTotals = this.convertValues(source["recordTotals"], RecordTotals);
	        this.reminder = this.convertValues(source["reminder"], ReminderView);
	        this.hotkeys = this.convertValues(source["hotkeys"], desktop.HotkeyView);
	        this.petVisible = source["petVisible"];
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
	export class TodoInput {
	    id: string;
	    title: string;
	    notes: string;
	    due: string;
	
	    static createFrom(source: any = {}) {
	        return new TodoInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.notes = source["notes"];
	        this.due = source["due"];
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
	    pomodoroOnStartup: boolean;
	    autoStartNextPhase: boolean;
	    remindDuringBreak: boolean;
	    pomodoroSound: boolean;
	    focusMinutes: number;
	    shortBreakMinutes: number;
	    longBreakMinutes: number;
	    longBreakEvery: number;
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
	    llmContextTodos: boolean;
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
	        this.pomodoroOnStartup = source["pomodoroOnStartup"];
	        this.autoStartNextPhase = source["autoStartNextPhase"];
	        this.remindDuringBreak = source["remindDuringBreak"];
	        this.pomodoroSound = source["pomodoroSound"];
	        this.focusMinutes = source["focusMinutes"];
	        this.shortBreakMinutes = source["shortBreakMinutes"];
	        this.longBreakMinutes = source["longBreakMinutes"];
	        this.longBreakEvery = source["longBreakEvery"];
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
	        this.llmContextTodos = source["llmContextTodos"];
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
	export class PomodoroRecord {
	    id: string;
	    todoId?: string;
	    todoTitle?: string;
	    // Go type: time
	    startedAt: any;
	    // Go type: time
	    endedAt: any;
	    focusMinutes: number;
	    summary: string;
	    // Go type: time
	    editedAt?: any;
	    plan?: string;
	    // Go type: time
	    planEditedAt?: any;
	
	    static createFrom(source: any = {}) {
	        return new PomodoroRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.todoId = source["todoId"];
	        this.todoTitle = source["todoTitle"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.endedAt = this.convertValues(source["endedAt"], null);
	        this.focusMinutes = source["focusMinutes"];
	        this.summary = source["summary"];
	        this.editedAt = this.convertValues(source["editedAt"], null);
	        this.plan = source["plan"];
	        this.planEditedAt = this.convertValues(source["planEditedAt"], null);
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
	export class Todo {
	    id: string;
	    title: string;
	    notes?: string;
	    // Go type: time
	    due?: any;
	    done: boolean;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    doneAt?: any;
	
	    static createFrom(source: any = {}) {
	        return new Todo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.notes = source["notes"];
	        this.due = this.convertValues(source["due"], null);
	        this.done = source["done"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.doneAt = this.convertValues(source["doneAt"], null);
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

