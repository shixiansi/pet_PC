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
export type PluginConfig = main.PluginConfig;
export type ConfigEditor = main.ConfigEditor;
export type Config = store.Config;
export type Todo = store.Todo;
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
      // One item of every KIND the vocabulary has (`internal/plugins.ConfigValueTypes`), so the
      // editor's five controls are all exercised in the browser: a long-ish text field, a
      // number, a bool, a `select` with `multiple` (stored as a comma-joined string), and a
      // write-only secret. A fake that declared only text fields would let four of them rot.
      config: [
        { key: 'model', label: '模型', type: 'text', default: 'deepseek-chat' },
        { key: 'temperature', label: '温度', type: 'number', default: '0.8' },
        { key: 'stream', label: '流式返回', type: 'bool', default: 'true' },
        {
          key: 'providers',
          label: '服务商（可多选）',
          type: 'select',
          multiple: true,
          default: 'deepseek',
          options: [
            { value: 'deepseek', label: '深度求索' },
            { value: 'openai', label: 'OpenAI' },
          ],
        },
        { key: 'apiKey', label: 'API Key', type: 'secret', secret: true },
      ],
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
].map(asHostSends);

/**
 * Turns a fake row into the VALUE the host actually sends.
 *
 * `createFrom` builds class instances, and Wails does not: a binding's result is JSON. The
 * difference is not cosmetic - Svelte 5's `$state` DEEP-PROXIES plain objects and arrays but
 * leaves class instances alone, so class-instance fakes silently lack the proxy that the real
 * page has. That gap is exactly how this page shipped a `DataCloneError`:
 *
 *   Uncaught DataCloneError: Failed to execute 'postMessage' on 'Window':
 *   [object Object] could not be cloned.
 *
 * - the plugin frame's `init` was handed `Manifest.permissions` and `Granted` from a `$state`
 * row, i.e. Proxied arrays, and a Proxy cannot cross `postMessage`. In the browser it worked
 * (class instances), in the .exe it threw inside the message listener, so the plugin sat on
 * 「等待宿主初始化…」 while the host looked fine. jsdom could not see it either, for the same
 * reason: it was being fed these very class instances.
 *
 * So the fakes are flattened: `createFrom` keeps the FIELD NAMES honest (a typo is still a
 * type error), and this keeps the SHAPE honest (plain data, like the wire).
 */
function asHostSends<T>(row: T): T {
  return JSON.parse(JSON.stringify(row)) as T;
}

// The host's wording, in the host's order (plugins.AllowedPermissions), copied from
// internal/plugins/capability.go. It is duplicated ON PURPOSE here and only here: this is
// dev-only fake data, and a browser that shows a bare `plugin.share` because the stub is
// stale is a smaller problem than a page that cannot be run without the .exe. The real
// screen never reads these - it reads App.PermissionLabels().
const LABELS: Array<[id: string, label: string]> = [
  ['todos.read', '读取待办列表'],
  ['todos.write', '新增、修改、完成与删除待办'],
  ['pet.bubble', '让桌宠弹出一句话的气泡'],
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

// --- 插件的配置编辑器（manifest.config） ----------------------------------------------
//
// The PAGE's own bindings, not the frame's: the editor lists what the manifest declared and
// writes it through the same `plugins.ConfigStore` the pet and the plugin read. So a value
// typed here is what the plugin's `config.get` answers on its next call - there is no second
// store and no "applied on restart".
//
// A secret is never backfilled: the host sends `value: ''` plus `secretSet` / `secretMask`, so
// the page has nothing to leak and nothing to overwrite by accident. Blank means "keep".
const FAKE_VALUES: Record<string, Record<string, string>> = {
  'com.shixiansi.plugin-chat': {
    model: 'deepseek-chat',
    temperature: '0.8',
    stream: 'true',
    providers: 'deepseek',
  },
};
const FAKE_SECRET_SET = new Set<string>(['com.shixiansi.plugin-chat']);

export const GetConfig = (id: string): Promise<PluginConfig> =>
  DEV ? fakeConfig(id) : host.GetConfig(id);
export const SaveConfig = (id: string, values: Record<string, string>): Promise<void | unknown> => {
  if (!DEV) return host.SaveConfig(id, values);
  const declared = FAKE_ROWS.find((r) => r.ID === id)?.Manifest?.config ?? [];
  const secretKeys = new Set(declared.filter((c) => c.type === 'secret').map((c) => c.key));
  const store = (FAKE_VALUES[id] ??= {});
  for (const [key, value] of Object.entries(values)) {
    if (secretKeys.has(key)) {
      // A secret is WRITE-ONLY: keep the fact that one is stored, never the value.
      if (value !== '') FAKE_SECRET_SET.add(id);
      continue;
    }
    store[key] = value;
  }
  return later(undefined);
};

/**
 * Builds the same shape `Market.GetConfig` sends, out of the fake declarations.
 *
 * It is derived from `Manifest.config` rather than hand-written, so the dev editor cannot
 * describe controls the real one would not draw.
 */
function fakeConfig(id: string): Promise<PluginConfig> {
  const row = FAKE_ROWS.find((r) => r.ID === id);
  if (!row) return Promise.reject(new Error('插件 ' + id + ' 未安装'));
  if (!row.Manifest) return Promise.reject(new Error('插件 ' + id + ' 的 manifest 无法解析'));
  const store = FAKE_VALUES[id] ?? {};
  const items = (row.Manifest.config ?? []).map((decl) => {
    if (decl.type === 'secret') {
      const set = FAKE_SECRET_SET.has(id);
      return main.ConfigEditor.createFrom({
        key: decl.key,
        label: decl.label,
        type: decl.type,
        value: '',
        secret: true,
        secretSet: set,
        secretMask: set ? '••••a1b2' : '',
      });
    }
    return main.ConfigEditor.createFrom({
      ...decl,
      value: store[decl.key] ?? '',
      default: decl.default,
    });
  });
  return later(
    asHostSends(
      main.PluginConfig.createFrom({
        pluginId: id,
        name: row.Manifest.name,
        dir: row.Dir,
        items,
        usage: id === 'com.shixiansi.plugin-chat' ? '今天已用 3 / 200 次' : '',
      }),
    ),
  );
}

// --- 插件自己的界面（workshop） ------------------------------------------------------
//
// The plugin page mounts each plugin's document in a sandboxed frame (`lib/plugin-frame.ts`),
// and every call that document makes comes back through `Market.PluginRequest` - the SAME
// dispatcher the pet uses, with THIS program's capability table (audio / storage / config /
// net.fetch; `todos.*`, `timer.*`, `events.subscribe` and the like are refused here, on
// purpose: they need things only the pet has).
//
// There is deliberately no dev fake for it. In a browser nothing can reach it: the frame's
// `src` is `/plugin-asset/...`, which only the .exe's AssetServer serves, so no document boots
// and no `ready` is ever posted. A plausible-looking fake would be worse than none - one that
// answered `todos.list` from FAKE_TODOS would let a workshop look correct in `npm run dev`
// while the .exe refuses the very same call.
/** One plugin call, answered by the shared gate. Returns the method's JSON as a string. */
export const PluginRequest = (id: string, method: string, payload: string): Promise<string> => {
  if (!DEV) return host.PluginRequest(id, method, payload);
  return Promise.reject(
    new Error(
      '浏览器里没有插件帧，没有真实请求可答（' + id + ' → ' + method + '，payload ' + payload + '）',
    ),
  );
};

// The remembered selection, so reopening this window lands where the user left off - the
// reason the old market had these two bindings. `''` in dev, i.e. first-run: nothing selected,
// and the page says so instead of showing an empty pane.
export const SelectedID = (): Promise<string> => (DEV ? later('') : host.SelectedID());
export const RememberSelection = (id: string): Promise<void | unknown> =>
  DEV ? later(undefined) : host.RememberSelection(id);

export const MainSettings = (): Promise<Config> => (DEV ? later(FAKE_CONFIG) : host.MainSettings());
export const SetPetScale = (v: number): Promise<Config> =>
  DEV ? savedConfig({ petScale: v }) : host.SetPetScale(v);

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

export const SetReminderEnabled = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ reminderEnabled: on }) : host.SetReminderEnabled(on);
export const SetReminderOnStartup = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ reminderOnStartup: on }) : host.SetReminderOnStartup(on);
export const SetRemindWhenEmpty = (on: boolean): Promise<Config> =>
  DEV ? savedConfig({ remindWhenEmpty: on }) : host.SetRemindWhenEmpty(on);
export const SetReminderCadence = (intervalMin: number, jitterMaxMin: number): Promise<Config> =>
  DEV ? savedConfig({ reminderIntervalMin: intervalMin, reminderJitterMaxMin: jitterMaxMin }) : host.SetReminderCadence(intervalMin, jitterMaxMin);
export const SetReminderWindow = (from: string, to: string): Promise<Config> =>
  DEV ? savedConfig({ reminderFrom: from, reminderTo: to }) : host.SetReminderWindow(from, to);
export const SetReminderDisplay = (items: number, autoHideSec: number): Promise<Config> =>
  DEV ? savedConfig({ reminderMaxItems: items, reminderAutoHideSec: autoHideSec }) : host.SetReminderDisplay(items, autoHideSec);

export const Todos = (): Promise<Todo[]> => (DEV ? later(FAKE_TODOS) : host.Todos());
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
