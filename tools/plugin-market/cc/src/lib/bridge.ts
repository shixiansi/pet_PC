/*
 * The one place a page talks to the host.
 *
 * WHY THIS EXISTS: six pages were written against the real Wails bindings, and one of
 * them shipped a defect that could not be diagnosed at all - it sat on "读取中" with no
 * error, no console output and no way to attach a debugger, so every attempt to fix it
 * was a guess. A page that cannot be opened in a browser is a page that cannot be
 * debugged.
 *
 * So: in `npm run dev` every call is served from the fake data below. In a build
 * (`import.meta.env.DEV === false`) it forwards to the real bindings. The pages do not
 * know which one they are talking to, which is what makes the dev loop honest instead of
 * a parallel universe.
 *
 * WHY EVERY TYPE HERE COMES FROM wailsjs/go/models.ts AND NOT FROM THIS FILE: the types
 * were hand-written once, in camelCase, because camelCase is what this file uses and what
 * the fake data was written in.
 *
 *   - `plugins.Installed` has no json tags, so the host sends `ID`, `Enabled`,
 *     `Manifest.Name`. A page reading `row.id` got `undefined` four times over, and the
 *     browser showed nothing wrong because the fake data agreed with the wrong type.
 *   - `store.Config` DOES have json tags, which hid the same mistake in the other
 *     direction: `MainSettings()` was declared `Promise<any>`, so the start page's
 *     `cfg.PetScale` (the host sends `petScale`) compiled happily and the slider was
 *     pinned at 1.0 forever.
 *
 * Both are the same bug wearing different clothes: a description of the payload that is
 * not the payload. So the descriptions are gone. Every type below IS the generated class,
 * the fakes are built through `createFrom` (the same conversion a real call does), and a
 * field name the host does not send is now a `npm run check` error.
 */
import * as host from '../../../frontend/wailsjs/go/main/Market';
import { desktop, main, plugins, store } from '../../../frontend/wailsjs/go/models';

const DEV = import.meta.env.DEV;

// The pages import these names instead of restating fields, so there is exactly one
// description of every payload and it is the generated one.
export type PluginRow = plugins.Installed;
export type PermissionLabel = main.LabelPair;
export type Config = store.Config;
export type Todo = store.Todo;
export type PomodoroRecord = store.PomodoroRecord;
export type HotkeyView = desktop.HotkeyView;

// Three rows, three shapes the host really produces:
//
//   - a healthy plugin with a manifest and a mix of granted / not-granted permissions;
//   - a healthy plugin that is switched off, so `Enabled` and an empty `Granted` are
//     exercised rather than assumed;
//   - a row whose on-disk manifest no longer parses. That is the shape that looks like
//     "an empty object" if a page reads the wrong field names: `ID` is there, `Manifest`
//     is nil and `Error` says why. A page that falls back to the id still names it.
//
// A nil Manifest is `undefined` here and `null` from Go; both are falsy, and the page must
// not care which.
const FAKE_ROWS: PluginRow[] = [
  plugins.Installed.createFrom({
    ID: 'com.shixiansi.plugin-chat',
    Version: '1.4.1',
    Enabled: true,
    Granted: ['llm.chat', 'pet.bubble', 'pet.surface', 'plugin.config', 'storage', 'todos.read'],
    InstalledAt: '2026-10-18T09:20:00+08:00',
    Manifest: {
      id: 'com.shixiansi.plugin-chat',
      name: '对话插件',
      version: '1.4.1',
      author: 'shixiansi',
      description: '在桌宠旁聊天，并把对话记进自己的私有目录。',
      permissions: ['llm.chat', 'pet.bubble', 'pet.surface', 'plugin.config', 'storage', 'todos.read', 'todos.write'],
      panel: true,
      panelSlot: 'chat',
      entry: 'index.html',
    },
    Dir: 'C:\\Users\\fan\\AppData\\Roaming\\PetPC\\plugins\\com.shixiansi.plugin-chat',
    Error: '',
  }),
  plugins.Installed.createFrom({
    ID: 'com.shixiansi.plugin-voice',
    Version: '1.0.1',
    Enabled: false,
    Granted: [],
    InstalledAt: '2026-10-02T21:05:00+08:00',
    Manifest: {
      id: 'com.shixiansi.plugin-voice',
      name: '语音插件',
      version: '1.0.1',
      author: 'shixiansi',
      description: '把提醒念出来。',
      permissions: ['audio', 'events', 'storage'],
      panel: true,
      entry: 'index.html',
    },
    Dir: 'C:\\Users\\fan\\AppData\\Roaming\\PetPC\\plugins\\com.shixiansi.plugin-voice',
    Error: '',
  }),
  // The row the .exe kept showing as an empty object: an id with no manifest. It is here
  // so that a page reading the wrong field names reproduces the blank row IN THE BROWSER
  // instead of only after a rebuild.
  plugins.Installed.createFrom({
    ID: 'com.example.broken',
    Version: '0.0.3',
    Enabled: false,
    Granted: [],
    InstalledAt: '2026-09-30T12:00:00+08:00',
    Manifest: null,
    Dir: 'C:\\Users\\fan\\AppData\\Roaming\\PetPC\\plugins\\com.example.broken',
    Error: 'manifest.json: 第 4 行 permissions 里有一项不在权限表内（"todos.remove"）',
  }),
];

// The host's wording, in the host's order (plugins.AllowedPermissions), copied from
// internal/plugins/capability.go. It is duplicated ON PURPOSE here and only here: this is
// dev-only fake data, and a browser that shows a bare `timer.control` because the stub is
// stale is a smaller problem than a page that cannot be run without the .exe. The real
// screen never reads these - it reads App.PermissionLabels().
const LABELS: Array<[id: string, label: string]> = [
  ['todos.read', '读取待办列表'],
  ['todos.write', '新增、修改、完成与删除待办'],
  ['pet.bubble', '让桌宠弹出一句话的气泡'],
  ['timer.read', '读取番茄钟阶段与剩余时间'],
  ['timer.control', '开始、暂停、继续与停止番茄钟（不能结束阶段或写记录）'],
  ['events', '订阅待办与番茄钟的变化事件（关闭界面后也会送达）'],
  ['net.fetch', '访问网络（仅限插件声明的域名）'],
  ['net.credentials', '以你保存的网站登录凭据发请求（凭据本身不会显示给插件）'],
  ['storage', '在自己的私有目录里保存数据'],
  ['pet.surface', '在桌宠旁显示自定义浮层面'],
  ['llm.chat', '调用你已配置的大模型（流式返回）'],
  ['plugin.config', '保存你自己的插件配置'],
  ['audio', '保存并播放音频（例如语音提醒）'],
  ['net.ws', '保持一条到插件所声明地址的 WebSocket 长连接（收发消息）'],
  ['llm.borrow', '使用其它插件已配置的大模型与密钥（密钥不会显示给你）'],
  ['process', '启动本机程序'],
  ['plugin.share', '与其它插件交换消息（只能读清单里点名的键，只能写自己名下的键）'],
];

const FAKE_LABELS: PermissionLabel[] = LABELS.map(([ID, Label]) => main.LabelPair.createFrom({ ID, Label }));

// Every field store.DefaultConfig sets, at its default value, because that is what the host
// actually sends: a fake with four convenient fields would hide a page reading a fifth.
// `petScale` is 1.2 rather than 1.0 so a page that renders it CANNOT look right by accident
// - which is exactly how the start page's `cfg.PetScale` stayed invisible in the browser.
const FAKE_CONFIG: Config = store.Config.createFrom({
  petScale: 1.2,
  position: { x: 1180, y: 640 },
  snapToEdge: true,
  edgeMargin: 8,
  autostart: false,
  marketBaseUrl: 'https://gh-proxy.com/https://raw.githubusercontent.com/shixiansi/pet-pc-plugins/main',
  globalHotkeys: false,
  pomodoroOnStartup: false,
  autoStartNextPhase: true,
  remindDuringBreak: true,
  pomodoroSound: true,
  focusMinutes: 25,
  shortBreakMinutes: 5,
  longBreakMinutes: 15,
  longBreakEvery: 4,
  reminderOnStartup: false,
  reminderEnabled: false,
  reminderIntervalMin: 30,
  reminderJitterMaxMin: 3,
  reminderFrom: '09:00',
  reminderTo: '22:00',
  reminderMaxItems: 3,
  reminderAutoHideSec: 12,
  remindWhenEmpty: false,
  llmEnabled: false,
  llmProvider: 'deepseek',
  llmBaseUrl: 'https://api.deepseek.com/v1',
  llmModel: 'deepseek-chat',
  llmTemperature: 0.8,
  llmMaxTokens: 800,
  llmTimeoutSec: 90,
  llmSystemPrompt: '你是一只住在用户桌面上的小猫桌宠，名字叫「桌宠」。',
  llmHistoryTurns: 12,
  llmContextTodos: true,
  llmProactive: false,
  llmProactiveMinGapMin: 60,
  llmDailyBudget: 200,
});

const FAKE_TODOS: Todo[] = [
  store.Todo.createFrom({
    id: 'a',
    title: '写周报',
    notes: '第三节讲清楚为什么不做多窗口',
    due: '2026-10-27T18:00:00+08:00',
    done: false,
    createdAt: '2026-10-26T09:12:00+08:00',
  }),
  store.Todo.createFrom({
    id: 'b',
    title: '整理纸箱',
    done: true,
    createdAt: '2026-10-20T20:00:00+08:00',
    doneAt: '2026-10-21T10:30:00+08:00',
  }),
];

const FAKE_RECORDS: PomodoroRecord[] = [
  store.PomodoroRecord.createFrom({
    id: 'r1',
    todoId: 'a',
    todoTitle: '写周报',
    startedAt: '2026-10-26T09:30:00+08:00',
    endedAt: '2026-10-26T09:55:00+08:00',
    focusMinutes: 25,
    plan: '写完第三节',
    summary: '写了两节，第三节开头',
  }),
];

// A dev-only delay so a "读取中" state is actually observable instead of flashing past.
const later = <T>(v: T, ms = 120): Promise<T> => new Promise((r) => setTimeout(() => r(v), ms));

// The setters return the config the store SAVED, normalise included, so the fake has to
// come back with the change applied - a setter that returned the old config would make the
// page look like it ignored the click.
const savedConfig = (patch: Partial<Config>): Promise<Config> =>
  later(store.Config.createFrom({ ...FAKE_CONFIG, ...patch }));

export const Installed = (): Promise<PluginRow[]> => (DEV ? later(FAKE_ROWS) : host.Installed());
export const PermissionLabels = (): Promise<PermissionLabel[]> =>
  DEV ? later(FAKE_LABELS) : host.PermissionLabels();
export const Grant = (id: string, granted: string[]): Promise<void | unknown> =>
  DEV ? later(undefined) : host.Grant(id, granted);
export const SetEnabled = (id: string, on: boolean): Promise<void | unknown> =>
  DEV ? later(undefined) : host.SetEnabled(id, on);

export const MainSettings = (): Promise<Config> => (DEV ? later(FAKE_CONFIG) : host.MainSettings());
export const SetPetScale = (v: number): Promise<Config> =>
  DEV ? savedConfig({ petScale: v }) : host.SetPetScale(v);
export const SetFocusMinutes = (v: number): Promise<Config> =>
  DEV ? savedConfig({ focusMinutes: v }) : host.SetFocusMinutes(v);
export const SetShortBreakMinutes = (v: number): Promise<Config> =>
  DEV ? savedConfig({ shortBreakMinutes: v }) : host.SetShortBreakMinutes(v);
export const SetLongBreakMinutes = (v: number): Promise<Config> =>
  DEV ? savedConfig({ longBreakMinutes: v }) : host.SetLongBreakMinutes(v);
export const SetLongBreakEvery = (v: number): Promise<Config> =>
  DEV ? savedConfig({ longBreakEvery: v }) : host.SetLongBreakEvery(v);
export const SetAutoStartNextPhase = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ autoStartNextPhase: on }) : host.SetAutoStartNextPhase(on);

// --- 外观与启动 / 番茄钟开关 / 对话提醒 ----------------------------------------------
//
// These used to be the pet's settings tab. Nothing about them needs the pet to ACT: the
// hot keys are the one exception, and the pet re-registers them from its settings watcher
// (app_settingswatch.go), so the switch works while both programs are running.

export const SetSnapToEdge = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ snapToEdge: on }) : host.SetSnapToEdge(on);
export const SetAutostart = (on: boolean): Promise<Config> => (DEV ? savedConfig({ autostart: on }) : host.SetAutostart(on));
export const SetGlobalHotkeys = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ globalHotkeys: on }) : host.SetGlobalHotkeys(on);
export const HotkeyLabels = (): Promise<HotkeyView> =>
  DEV
    ? later(desktop.HotkeyView.createFrom({ enabled: true, show: 'Ctrl+Alt+S', hide: 'Ctrl+Alt+H', active: true }))
    : host.HotkeyLabels();

// The picture is the one setting whose dialog lives here rather than in the pet: the pet's
// settings page is going away, and a MODAL file dialog needs a window - this program has
// one. The copying is store.SetPetImage, i.e. the same call the pet makes.
export const ChoosePetImage = (): Promise<Config> => (DEV ? savedConfig({ petImageFile: 'C:\\fake\\pet.png' }) : host.ChoosePetImage());
export const ResetPetImage = (): Promise<Config> =>
  DEV ? savedConfig({ petImageFile: undefined, originalPath: undefined }) : host.ResetPetImage();

export const SetPomodoroOnStartup = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ pomodoroOnStartup: on }) : host.SetPomodoroOnStartup(on);
export const SetPomodoroSound = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ pomodoroSound: on }) : host.SetPomodoroSound(on);

export const SetReminderEnabled = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ reminderEnabled: on }) : host.SetReminderEnabled(on);
export const SetReminderOnStartup = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ reminderOnStartup: on }) : host.SetReminderOnStartup(on);
export const SetRemindDuringBreak = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ remindDuringBreak: on }) : host.SetRemindDuringBreak(on);
export const SetRemindWhenEmpty = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ remindWhenEmpty: on }) : host.SetRemindWhenEmpty(on);
export const SetReminderCadence = (intervalMin: number, jitterMaxMin: number): Promise<Config> =>
  DEV ? savedConfig({ reminderIntervalMin: intervalMin, reminderJitterMaxMin: jitterMaxMin }) : host.SetReminderCadence(intervalMin, jitterMaxMin);
export const SetReminderWindow = (from: string, to: string): Promise<Config> =>
  DEV ? savedConfig({ reminderFrom: from, reminderTo: to }) : host.SetReminderWindow(from, to);
export const SetReminderDisplay = (items: number, autoHideSec: number): Promise<Config> =>
  DEV ? savedConfig({ reminderMaxItems: items, reminderAutoHideSec: autoHideSec }) : host.SetReminderDisplay(items, autoHideSec);

export const Todos = (): Promise<Todo[]> => (DEV ? later(FAKE_TODOS) : host.Todos());
export const PomodoroRecords = (): Promise<PomodoroRecord[]> =>
  DEV ? later(FAKE_RECORDS) : host.PomodoroRecords();
export const DataDir = (): Promise<string> => (DEV ? later('C:\\fake\\PetPC') : host.DataDir());

// --- 高级页的诊断面 --------------------------------------------------------------

export type HostKeyState = main.HostKey;

// The same text the Go side builds, shortened but not reshaped: a fake that drops the
// "不存在的文件也要说出来" lines would let the page look right while the real report is empty.
const FAKE_REPORT = `PetPC 诊断报告
生成时间: 2026-10-26 18:40:12
数据目录: C:\\fake\\PetPC

== 文件 ==
settings.json                 412 B  2026-10-26 18:31:04
config.json                   188 B  2026-10-26 18:38:51
plugins.json                  264 B  2026-10-18 09:20:11
plugin-market.json             64 B  2026-10-02 21:05:44
todos.json                    398 B  2026-10-26 09:12:00
pomodoro.json                 120 B  2026-10-26 09:55:00
pomodoro-records.json         512 B  2026-10-26 09:55:00
llm-key.bin                   452 B  2026-10-18 09:21:30
debug.log                    不存在

== 已装插件 ==
com.shixiansi.plugin-chat  v1.4.1  启用  权限 7（已授 6）  对话插件
com.example.broken  v0.0.3  停用  权限 0（已授 0）
    manifest: manifest.json: 第 4 行 permissions 里有一项不在权限表内（"todos.remove"）

== 警告 ==
（无）

== 密钥 ==
llm-key.bin 存在（DPAPI 密文，本程序不解密）

== 日志尾 ==
debug.log 不存在：它只在桌宠以 PETPC_DEBUG=1 启动时才被写，平时是正常的。
`;

export const HostKey = (): Promise<HostKeyState> =>
  DEV
    ? later(main.HostKey.createFrom({ Path: 'C:\\fake\\PetPC\\llm-key.bin', Present: true }))
    : host.HostKey();
export const Diagnostics = (): Promise<string> => (DEV ? later(FAKE_REPORT) : host.Diagnostics());
