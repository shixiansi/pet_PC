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
	export class ChatMessageView {
	    role: string;
	    text: string;
	    at: string;
	    error?: boolean;
	    proactive?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ChatMessageView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.text = source["text"];
	        this.at = source["at"];
	        this.error = source["error"];
	        this.proactive = source["proactive"];
	    }
	}
	export class ChatStart {
	    requestId: string;
	    proactive?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ChatStart(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.proactive = source["proactive"];
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
	export class LLMSettingsInput {
	    enabled: boolean;
	    provider: string;
	    baseUrl: string;
	    model: string;
	    temperature: number;
	    maxTokens: number;
	    timeoutSec: number;
	    systemPrompt: string;
	    historyTurns: number;
	    contextTodos: boolean;
	    proactive: boolean;
	    proactiveMinGapMin: number;
	    dailyBudget: number;
	
	    static createFrom(source: any = {}) {
	        return new LLMSettingsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.provider = source["provider"];
	        this.baseUrl = source["baseUrl"];
	        this.model = source["model"];
	        this.temperature = source["temperature"];
	        this.maxTokens = source["maxTokens"];
	        this.timeoutSec = source["timeoutSec"];
	        this.systemPrompt = source["systemPrompt"];
	        this.historyTurns = source["historyTurns"];
	        this.contextTodos = source["contextTodos"];
	        this.proactive = source["proactive"];
	        this.proactiveMinGapMin = source["proactiveMinGapMin"];
	        this.dailyBudget = source["dailyBudget"];
	    }
	}
	export class LLMTestResult {
	    ok: boolean;
	    model: string;
	    baseUrl: string;
	    elapsedMs: number;
	    error?: string;
	    kind?: string;
	
	    static createFrom(source: any = {}) {
	        return new LLMTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.model = source["model"];
	        this.baseUrl = source["baseUrl"];
	        this.elapsedMs = source["elapsedMs"];
	        this.error = source["error"];
	        this.kind = source["kind"];
	    }
	}
	export class ProactiveView {
	    enabled: boolean;
	    nextAt?: string;
	    nextInSec: number;
	    lastSpokeAt?: string;
	    suppressed?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProactiveView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.nextAt = source["nextAt"];
	        this.nextInSec = source["nextInSec"];
	        this.lastSpokeAt = source["lastSpokeAt"];
	        this.suppressed = source["suppressed"];
	    }
	}
	export class LLMView {
	    enabled: boolean;
	    ready: boolean;
	    notReadyReason?: string;
	    hasKey: boolean;
	    keySource: string;
	    keyMask?: string;
	    keyFile: string;
	    needsKey: boolean;
	    providers: store.ProviderPreset[];
	    usage: store.LLMUsage;
	    usageLine: string;
	    overBudget: boolean;
	    messages: ChatMessageView[];
	    busy: boolean;
	    proactive: ProactiveView;
	
	    static createFrom(source: any = {}) {
	        return new LLMView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.ready = source["ready"];
	        this.notReadyReason = source["notReadyReason"];
	        this.hasKey = source["hasKey"];
	        this.keySource = source["keySource"];
	        this.keyMask = source["keyMask"];
	        this.keyFile = source["keyFile"];
	        this.needsKey = source["needsKey"];
	        this.providers = this.convertValues(source["providers"], store.ProviderPreset);
	        this.usage = this.convertValues(source["usage"], store.LLMUsage);
	        this.usageLine = source["usageLine"];
	        this.overBudget = source["overBudget"];
	        this.messages = this.convertValues(source["messages"], ChatMessageView);
	        this.busy = source["busy"];
	        this.proactive = this.convertValues(source["proactive"], ProactiveView);
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
	export class LiveRect {
	    x: number;
	    y: number;
	    w: number;
	    h: number;
	
	    static createFrom(source: any = {}) {
	        return new LiveRect(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.w = source["w"];
	        this.h = source["h"];
	    }
	}
	export class LiveView {
	    card: LiveRect;
	    bar: LiveRect;
	    puffs: LiveRect[];
	    tailX: number;
	    onRight: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LiveView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.card = this.convertValues(source["card"], LiveRect);
	        this.bar = this.convertValues(source["bar"], LiveRect);
	        this.puffs = this.convertValues(source["puffs"], LiveRect);
	        this.tailX = source["tailX"];
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
	    live?: LiveView;
	
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
	        this.live = this.convertValues(source["live"], LiveView);
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
	export class VoiceView {
	    enabled: boolean;
	    pomodoro: boolean;
	    reminder: boolean;
	    lines: number;
	    dir: string;
	    speaking: boolean;
	    builtInRole: string;
	    activeRole: string;
	    activeRoleName: string;
	    activeRoleIsBuiltIn: boolean;
	    roleError?: string;
	    replaced: number;
	
	    static createFrom(source: any = {}) {
	        return new VoiceView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.pomodoro = source["pomodoro"];
	        this.reminder = source["reminder"];
	        this.lines = source["lines"];
	        this.dir = source["dir"];
	        this.speaking = source["speaking"];
	        this.builtInRole = source["builtInRole"];
	        this.activeRole = source["activeRole"];
	        this.activeRoleName = source["activeRoleName"];
	        this.activeRoleIsBuiltIn = source["activeRoleIsBuiltIn"];
	        this.roleError = source["roleError"];
	        this.replaced = source["replaced"];
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
	    llm: LLMView;
	    voice: VoiceView;
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
	        this.llm = this.convertValues(source["llm"], LLMView);
	        this.voice = this.convertValues(source["voice"], VoiceView);
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
	export class VoiceSettingsInput {
	    enabled: boolean;
	    pomodoro: boolean;
	    reminder: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VoiceSettingsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.pomodoro = source["pomodoro"];
	        this.reminder = source["reminder"];
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
	    voiceEnabled: boolean;
	    voicePomodoro: boolean;
	    voiceReminder: boolean;
	
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
	        this.voiceEnabled = source["voiceEnabled"];
	        this.voicePomodoro = source["voicePomodoro"];
	        this.voiceReminder = source["voiceReminder"];
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
	export class LLMUsage {
	    date: string;
	    requests: number;
	    promptTokens: number;
	    completionTokens: number;
	
	    static createFrom(source: any = {}) {
	        return new LLMUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.requests = source["requests"];
	        this.promptTokens = source["promptTokens"];
	        this.completionTokens = source["completionTokens"];
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
	
	export class ProviderPreset {
	    id: string;
	    label: string;
	    baseUrl: string;
	    model: string;
	    hint: string;
	    needsKey: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderPreset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.baseUrl = source["baseUrl"];
	        this.model = source["model"];
	        this.hint = source["hint"];
	        this.needsKey = source["needsKey"];
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

