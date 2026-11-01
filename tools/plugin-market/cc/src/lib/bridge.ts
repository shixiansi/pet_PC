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
import { EventsOn } from '../../../frontend/wailsjs/runtime/runtime';
import { desktop, main, plugins, store } from '../../../frontend/wailsjs/go/models';

const DEV = import.meta.env.DEV;

// The pages import these names instead of restating fields, so there is exactly one
// description of every payload and it is the generated one.
export type PluginRow = plugins.Installed;
export type PermissionLabel = main.LabelPair;
export type PluginConfig = main.PluginConfig;
export type ConfigEditor = main.ConfigEditor;
export type Config = store.Config;
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
    Granted: ['llm.chat', 'pet.bubble', 'pet.surface', 'plugin.config'],
    InstalledAt: '2026-10-18T09:20:00+08:00',
    Manifest: {
      id: 'com.shixiansi.plugin-chat',
      name: '对话插件',
      version: '1.4.1',
      author: 'shixiansi',
      description: '在桌宠旁聊天，并把对话记进自己的私有目录。',
      permissions: ['llm.chat', 'pet.bubble', 'pet.surface', 'plugin.config', 'storage'],
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
  // A SURFACE plugin, and the reason it is here: the page used to refuse these outright
  // (they declare rectangles measured from the pet, which this window does not have), so the
  // three plugins whose pages the user actually wanted — todo, pomodoro, chat — showed no
  // interface at all. They mount now; what a document cannot do here is pet-shaped, and it
  // says so itself. Without this row in the stub, `mountable()` could go back to refusing
  // surfaces and the smoke test would stay green.
  plugins.Installed.createFrom({
    ID: 'com.shixiansi.plugin-pomodoro',
    Version: '2.2.0',
    Enabled: true,
    Granted: ['storage', 'plugin.config', 'plugin.share'],
    InstalledAt: '2026-10-21T20:10:00+08:00',
    Manifest: {
      id: 'com.shixiansi.plugin-pomodoro',
      name: '番茄钟插件',
      version: '2.2.0',
      author: 'shixiansi',
      description: '桌宠旁的番茄钟：自己的状态机与记录，计时在常驻帧里跑，浮层面只是视图。',
      permissions: ['storage', 'plugin.config', 'plugin.share', 'events', 'pet.surface', 'pet.bubble'],
      panelSlot: 'pomodoro',
      background: true,
      surface: [{ dx: 160, dy: -160, w: 380, h: 260, radius: 14 }],
      // The four keys `machine.js` reads, declared so the page's 设置 button has something
      // honest to open. They used to be hard-coded defaults with NO declaration at all,
      // which is exactly why the plugin had no settings to show.
      config: [
        { key: 'focusMinutes', label: '专注时长（分钟）', type: 'number', default: '25' },
        { key: 'shortBreakMinutes', label: '短休息（分钟）', type: 'number', default: '5' },
        { key: 'longBreakMinutes', label: '长休息（分钟）', type: 'number', default: '15' },
        { key: 'longBreakEvery', label: '每几个专注后来一次长休息', type: 'number', default: '4' },
        { key: 'autoStartNextPhase', label: '一个阶段结束后自动开始下一段', type: 'bool', default: 'true' },
      ],
      entry: 'panel.html',
    },
    Dir: 'C:\\Users\\fan\\AppData\\Roaming\\PetPC\\plugins\\com.shixiansi.plugin-pomodoro',
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
  ['pet.bubble', '让桌宠弹出一句话的气泡'],
  ['events', '订阅宿主的状态变化事件（提醒触发、计时阶段结束、配置变更）'],
  ['net.fetch', '访问网络（仅限插件声明的域名）'],
  ['net.credentials', '以你保存的网站登录凭据发请求（凭据本身不会显示给插件）'],
  ['storage', '在自己的私有目录里保存数据'],
  ['pet.surface', '在桌宠身上或旁边显示你自己的界面（浮层面、头顶的一行字、宠物本体）'],
  ['llm.chat', '调用你已配置的大模型（流式返回）'],
  ['plugin.config', '保存你自己的插件配置'],
  ['audio', '保存并播放音频（例如语音提醒）'],
  ['net.ws', '保持一条到插件所声明地址的 WebSocket 长连接（收发消息）'],
  ['llm.borrow', '使用其它插件已配置的大模型与密钥（密钥不会显示给你）'],
  ['process', '启动本机程序'],
  ['plugin.share', '与其它插件交换消息（只能读清单里点名的键，只能写自己名下的键）'],
  ['plugin.call', '与其它插件互相调用（只能调用清单里点名的目标，也只能回答清单里声明的方法）'],
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
  marketBaseUrl: 'https://gh-proxy.com/https://raw.githubusercontent.com/shixiansi/PetPC-plugin/main',
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

/**
 * What THIS program serves, for `init.host` (see `internal/plugins.HostInfo`).
 *
 * No dev fake, and for the same reason `PluginRequest` has none: a plausible one would be a
 * hand-written copy of `marketRegistry`'s method table, and a copy that drifts is exactly the
 * failure this field exists to remove - a plugin would be told "yes, this window serves
 * `llm.profile`" by a browser session and refused by the .exe. In dev the frame simply gets no
 * `host`, and a plugin falls back to `surface.slot`, which is the pre-existing rule.
 */
export const PluginHostInfo = (): Promise<plugins.HostInfo> => {
  if (!DEV) return host.PluginHostInfo();
  return Promise.reject(new Error('浏览器里没有 Go 侧的能力表（init.host 会缺省，插件按 slot 判断）'));
};

// --- 市场目录：只为了回答「有没有更新」 ------------------------------------------
//
// The catalogue is here for ONE reason so far, and it is not browsing: a plugin's interface
// ships WITH the plugin. The bundled documents learned new behaviour in this round (a list that
// converges between two windows, the pomodoro's records view, the chat plugin's read-only
// composer in a management window) - and an older installed package would show none of it while
// the page around it looked finished. So the page has to be able to say "v2.2.0 is out" and to
// install it.
//
// Browsing and installing something you do NOT have is still the open half of stage 3
// (docs/plugin-market.md §2.8). The update goes through `Market.Update`, which keeps the plugin
// enabled and keeps the permissions the user already granted.
export type MarketView = plugins.MarketView;
export type MarketEntry = plugins.MarketEntry;

// Two entries in the shape the host sends (`installed` / `installedVersion` are filled in by the
// manager): the chat row the page lists at 1.7.4 has 1.7.5 available, and the voice row is
// current - so a page that offered an update for everything would fail the smoke test.
const FAKE_MARKET: MarketView = plugins.MarketView.createFrom({
  base: 'https://gh-proxy.com/https://raw.githubusercontent.com/shixiansi/PetPC-plugin/main',
  fallback: false,
  notice: '',
  entries: [
    {
      id: 'com.shixiansi.plugin-chat',
      name: '对话插件',
      version: '1.7.5',
      author: 'shixiansi',
      description: '在面板里与桌宠旁和大模型对话的官方插件。',
      permissions: ['llm.chat', 'pet.surface', 'plugin.config', 'storage'],
      download: 'https://example.invalid/chat-1.7.5.zip',
      sha256: '0'.repeat(64),
      installed: true,
      installedVersion: '1.7.4',
    },
    {
      id: 'com.shixiansi.plugin-voice',
      name: '语音插件',
      version: '1.0.1',
      author: 'shixiansi',
      description: '把提醒念出来。',
      permissions: ['audio', 'events', 'storage'],
      download: 'https://example.invalid/voice-1.0.1.zip',
      sha256: '0'.repeat(64),
      installed: true,
      installedVersion: '1.0.1',
    },
    // And one this machine does NOT have, so the 市场 page's 安装 is exercised: a catalogue
    // where everything is already installed would make "install does nothing" look exactly
    // like "install works".
    {
      id: 'com.shixiansi.plugin-todo',
      name: '待办插件',
      version: '1.2.0',
      author: 'shixiansi',
      description: '桌面上的待办清单：提醒由它自己排，宿主只管把它画出来。',
      permissions: ['plugin.share', 'storage', 'pet.bubble', 'pet.surface', 'plugin.config'],
      download: 'https://example.invalid/todo-1.2.0.zip',
      sha256: '0'.repeat(64),
      installed: false,
    },
  ],
});

// A FRESH OBJECT every call, not the shared literal: the host sends JSON, so every call is a new
// value. Handing the page the same reference meant "install, then re-read" re-assigned an object
// whose identity had not changed, so nothing re-rendered - the row still offered 安装 after it had
// installed. (Svelte can only see a change it is told about; the mutation happens in here.)
export const Catalogue = (): Promise<MarketView> =>
  DEV ? later(plugins.MarketView.createFrom(FAKE_MARKET)) : host.Catalogue();

/**
 * Installs the newer package over the installed one.
 *
 * In dev the fake rows are MUTATED so a refresh shows the new version: a stub that resolved but
 * changed nothing would leave the 更新 chip on screen after a successful update, which is exactly
 * the "the button did nothing" impression this page exists to avoid.
 */
export const Update = (id: string): Promise<void | unknown> => {
  if (!DEV) return host.Update(id);
  const entry = FAKE_MARKET.entries.find((e) => e.id === id);
  const row = FAKE_ROWS.find((r) => r.ID === id);
  if (entry && row) {
    row.Version = entry.version;
    if (row.Manifest) row.Manifest.version = entry.version;
    entry.installedVersion = entry.version;
  }
  return later(undefined);
};

/**
 * Installs a package this machine does not have yet.
 *
 * The other half of `Update`, and the difference is one the user feels: an update carries the
 * enabled flag and the permissions already granted (it is NOT a re-authorisation), while a fresh
 * install arrives DISABLED with nothing granted - they have seen what it asks for but have not
 * said yes to any of it. That is why the market row says 「安装」 and then points at the 插件 page;
 * silently enabling it would hand a plugin authority nobody approved.
 */
export const Install = (id: string): Promise<void | unknown> => {
  if (!DEV) return host.Install(id);
  const entry = FAKE_MARKET.entries.find((e) => e.id === id);
  if (entry) {
    entry.installed = true;
    entry.installedVersion = entry.version;
  }
  return later(undefined);
};

/**
 * Removes a plugin, and — only when asked — the data it left behind.
 *
 * `purgeData` is the checkbox in the confirmation dialog and nothing else. `false` is a rollback:
 * the package goes, `plugin-data/<id>` stays (settings, stored keys, whatever the plugin was in
 * the middle of), so installing it again continues where it stopped. `true` destroys that
 * directory, which nothing can undo. Two decisions, so two arguments - a default buried in the
 * host would be the host answering for the user.
 *
 * In dev the fake row is REMOVED, because a stub that resolved and changed nothing would leave
 * the row on screen after a successful uninstall - the same "the button did nothing" impression
 * `Update`'s stub mutates its rows to avoid.
 */
export const Uninstall = (id: string, purgeData: boolean): Promise<void | unknown> => {
  if (!DEV) return host.Uninstall(id, purgeData);
  const at = FAKE_ROWS.findIndex((r) => r.ID === id);
  if (at >= 0) FAKE_ROWS.splice(at, 1);
  const entry = FAKE_MARKET.entries.find((e) => e.id === id);
  if (entry) {
    entry.installed = false;
    entry.installedVersion = '';
  }
  return later(undefined);
};

/**
 * Saves the catalogue address and answers with the catalogue fetched from it.
 *
 * One call rather than "save then reload": the address is only interesting if it produced a list,
 * and a save that left the old list on screen would look like it did nothing.
 */
export const SaveMarketURL = (url: string): Promise<MarketView> => {
  if (!DEV) return host.SaveMarketURL(url);
  FAKE_MARKET.base = url;
  return later(plugins.MarketView.createFrom(FAKE_MARKET));
};

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
// net.fetch / plugin.share). What is still refused is the genuinely pet-shaped half:
// `todos.*`, `timer.*`, `bubble.say`, `surface.*`, `llm.chat`, `events.subscribe`. A document
// that is designed for this window says what it cannot do in its own words; the pomodoro
// hides its timer controls here and the chat plugin's composer explains where to send.
//
// `plugin.share` is the one that had to move, and the history is worth keeping: it used to be
// refused, which made the todo/pomodoro documents show a host error in the middle of their own
// page, and the workaround was to refuse to mount any plugin that declared a `surface` at all.
// The tray is a folder inside plugin-data - this program owns it too - so the rule moved to
// internal/plugins/share.go and both hosts serve it.
//
// There is deliberately no dev fake for `PluginRequest`. In a browser nothing can reach it: the
// frame's `src` is `/plugin-asset/...`, which only the .exe's AssetServer serves, so no document
// boots and no `ready` is ever posted. A plausible-looking fake would be worse than none - one
// that answered `storage.get` from a constant would let a workshop look correct in `npm run dev`
// while the .exe answers with the plugin's real file.
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

export const DataDir = (): Promise<string> => (DEV ? later('C:\\fake\\PetPC') : host.DataDir());

// --- 外观：配色 ----------------------------------------------------------------
//
// The value lives in `plugin-market.json` (the host's own config file - this program never writes
// the pet's), and the WINDOW reads it back at boot: `main.ts` puts it on
// `<html data-theme>` before mounting, which is what `tokens.css` keys on and what the plugin
// frames read to pick their own scheme. Until this existed the binding was dead: `SetTheme` was
// written, tested on the Go side, and nothing in the interface ever called it.
let FAKE_THEME = 'light';
export const Theme = (): Promise<string> => (DEV ? later(FAKE_THEME) : host.Theme());
export const SetTheme = (name: string): Promise<void | unknown> => {
  if (!DEV) return host.SetTheme(name);
  // The host normalises anything that is not "light" onto "dark"; the fake does the same, or a
  // page that passed garbage would look correct here and flip to dark on the real thing.
  FAKE_THEME = name === 'light' ? 'light' : 'dark';
  return later(undefined);
};

// --- 外观：主题包 --------------------------------------------------------------
//
// The pack is the OTHER half of the look and lives in the same shared file (theme.json's `pack`),
// so the pet's windows follow it too. `''` is a real answer: "no pack chosen", which means the
// stylesheet's default palette (violet) - not "unknown pack".
//
// The fake keeps it in memory for the same reason FAKE_THEME exists: the dev loop has to be able to
// click a card and see the window change, without a host process.
let FAKE_PACK = '';
export const ThemePack = (): Promise<string> => (DEV ? later(FAKE_PACK) : host.ThemePack());
export const SetThemePack = (id: string): Promise<void | unknown> => {
  if (!DEV) return host.SetThemePack(id);
  // The host validates the shape and stores '' for anything that is not a pack id; the fake does
  // the same, or a page that passed a display name would look correct here and lose the choice on
  // the real thing.
  FAKE_PACK = /^[a-z0-9-]{1,32}$/.test(id) ? id : '';
  return later(undefined);
};

// 已安装的主题包。**开发桩返回空**：浏览器里没有数据目录，也没有人往 themes/ 里放过包。
// 空列表是诚实的答案（页面上"已安装"那一栏就是空的），伪造几个包会让人以为装成功了。
export type PackEntry = {
  id: string;
  name: string;
  author?: string;
  version?: string;
  builtin?: boolean;
  problems?: string[];
  error?: string;
  tokens?: Record<string, Record<string, string>>;
};
export const Themes = (): Promise<PackEntry[]> => (DEV ? later([]) : host.Themes());
export const ThemePacksProblem = (): Promise<string> => (DEV ? later('') : host.ThemePacksProblem());

// 主题商店。桩里是空货架 + 一句说明，理由同上：浏览器里没有市场源，也没有数据目录。
export type ThemeStoreRow = {
  id: string;
  name: string;
  version: string;
  author: string;
  description: string;
  download: string;
  sha256: string;
};
export type ThemeStoreView = { entries?: ThemeStoreRow[] | null; note?: string };
export const ThemeStore = (): Promise<ThemeStoreView> =>
  DEV ? later({ entries: [], note: '（浏览器里的假货架：真实清单来自市场源的 themes/index.json）' }) : host.ThemeStore();
export const InstallTheme = (id: string): Promise<void | unknown> => (DEV ? later(undefined) : host.InstallTheme(id));
export const RemoveTheme = (id: string): Promise<void | unknown> => (DEV ? later(undefined) : host.RemoveTheme(id));

// --- 高级页的诊断面 --------------------------------------------------------------

// The same text the Go side builds, shortened but not reshaped: a fake that drops the
// "不存在的文件也要说出来" lines would let the page look right while the real report is empty.
//
// The file table is the REAL one (settingsapi_diagnostics.go's dataFiles): no todos.json, no
// llm-key.bin, no pomodoro*.json. Those moved out of this directory's meaning - the first two
// into the todo plugin's private storage, the key into the conversation plugin's - and a stub
// that still listed them would keep the retired shape alive in the only place a browser-only
// session can see it.
const FAKE_REPORT = `PetPC 诊断报告
生成时间: 2026-10-28 18:40:12
数据目录: C:\\fake\\PetPC

== 文件 ==
settings.json                 412 B  2026-10-28 18:31:04
config.json                   188 B  2026-10-28 18:38:51
plugins.json                  264 B  2026-10-18 09:20:11
plugin-market.json             64 B  2026-10-02 21:05:44
logs/pet.log                18422 B  2026-10-28 18:40:02
logs/market.log               812 B  2026-10-28 18:39:58

== 已装插件 ==
com.shixiansi.plugin-chat  v1.4.1  启用  权限 7（已授 6）  对话插件
com.example.broken  v0.0.3  停用  权限 0（已授 0）
    manifest: manifest.json: 第 4 行 permissions 里有一项不在权限表内（"todos.remove"）

== 警告 ==
（无）

== 日志 ==
目录: C:\\fake\\PetPC\\logs（轮转上限 2048 KB，每个程序两个文件）
pet.log                    18422 B  2026-10-28 18:40:02
pet.log.1                      0 B  2026-10-28 09:12:44
market.log                   812 B  2026-10-28 18:39:58
market.log.1                   0 B  2026-10-28 09:12:44

2026-10-28 18:38:51.004 INFO  pet - 启动：数据目录 C:\\fake\\PetPC，日志级别 INFO，日志文件 C:\\fake\\PetPC\\logs\\pet.log
2026-10-28 18:39:12.331 INFO  plugin com.shixiansi.plugin-chat llm.chat ok（payload 84 B → 196 B，in 812ms）
2026-10-28 18:39:58.010 INFO  market com.shixiansi.plugin-chat 授权已保存：[llm.chat pet.bubble pet.surface plugin.config storage]
`;

export const Diagnostics = (): Promise<string> => (DEV ? later(FAKE_REPORT) : host.Diagnostics());

// --- 日志页 -------------------------------------------------------------------------
//
// The REAL read is two files in the data directory (`internal/logging.Read`), because half
// the interesting lines are written by the OTHER process - the pet. So there is nothing to
// mock away here: the fake is a plausibly-shaped list of rows, and what it must get right is
// that the page's FILTERS still work in the browser.
//
// That is why `fakeLog` applies the query instead of ignoring it: a stub that returned the
// same five rows for every filter would let a page whose level/scope/search were wired to
// nothing look perfectly correct in `npm run dev` - the exact class of bug this whole file
// exists to prevent.
export type LogView = main.LogView;
export type LogQuery = main.LogQuery;
export type LogLine = main.LogLine;
export type LogFileView = main.LogFileView;

// The levels a real log contains, including one WARN (a retried model call) and one ERROR (a
// plugin refused for a permission) - the two rows whose colour and left stripe are the whole
// point of having levels, and the two a fake made only of INFO rows would never exercise.
const FAKE_LOG_ROWS: Array<[at: string, level: string, scope: string, plugin: string, message: string]> = [
  ['2026-10-28 14:31:02.118', 'ERROR', 'page', '', '操作失败：未获权限 llm.chat'],
  ['2026-10-28 14:31:02.101', 'WARN', 'plugin', 'com.shixiansi.plugin-chat', 'llm.chat 没有执行（payload 84 B，in 0ms）：插件 com.shixiansi.plugin-chat 未获权限 llm.chat'],
  ['2026-10-28 14:30:58.442', 'WARN', 'llm', 'com.shixiansi.plugin-chat', 'llm.chat 一轮 in 2380ms（尝试 2 次）：0 字，tokens in=812 out=0，finish=，工具调用 0 个'],
  ['2026-10-28 14:30:56.004', 'INFO', 'plugin', 'com.shixiansi.plugin-chat', 'config.get ok（payload 0 B → 196 B，in 1ms）'],
  ['2026-10-28 14:30:41.771', 'INFO', 'reminder', '', '提醒气泡已弹出：2 条，手动=false'],
  ['2026-10-28 14:30:12.003', 'DEBUG', 'llm', 'com.shixiansi.plugin-chat', 'POST https://api.deepseek.com/v1/chat/completions model=deepseek-chat 消息 3 条 工具 2 个 温度 0.8 max_tokens=800'],
  ['2026-10-28 14:29:58.500', 'INFO', 'market', 'com.shixiansi.plugin-chat', '授权已保存：[llm.chat pet.bubble pet.surface plugin.config storage]'],
  ['2026-10-28 14:29:41.000', 'INFO', 'pet', '', '启动：数据目录 C:\\fake\\PetPC，日志级别 INFO，日志文件 C:\\fake\\PetPC\\logs\\pet.log'],
];

const FAKE_LOG_LEVELS: Record<string, number> = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 };

function fakeLog(q: LogQuery): LogView {
  const min = FAKE_LOG_LEVELS[(q.level || 'debug').toUpperCase()] ?? 0;
  const search = (q.search || '').trim().toLowerCase();
  const scope = (q.scope || '').trim();
  const rows = FAKE_LOG_ROWS.filter(
    (r) =>
      (FAKE_LOG_LEVELS[r[1]] ?? 0) >= min &&
      (scope === '' || r[2] === scope) &&
      (search === '' || r[4].toLowerCase().includes(search) || r[3].toLowerCase().includes(search)),
  );
  return asHostSends(
    main.LogView.createFrom({
      // Newest first, like the Go side sends them.
      entries: rows.slice().reverse().map(([at, level, scope_, plugin, message]) => ({ at, level, scope: scope_, plugin, message })),
      files: [
        { name: 'pet.log', path: 'C:\\fake\\PetPC\\logs\\pet.log', bytes: 18422, modified: '2026-10-28 14:31:02' },
        { name: 'pet.log.1', path: 'C:\\fake\\PetPC\\logs\\pet.log.1', bytes: 2097120, modified: '2026-10-28 09:12:44' },
        { name: 'market.log', path: 'C:\\fake\\PetPC\\logs\\market.log', bytes: 812, modified: '2026-10-28 14:29:58' },
        { name: 'market.log.1', path: 'C:\\fake\\PetPC\\logs\\market.log.1', bytes: 0, modified: '' },
      ],
      scopes: [...new Set(FAKE_LOG_ROWS.map((r) => r[2]))].sort(),
      dir: 'C:\\fake\\PetPC\\logs',
      note: rows.length === 0 ? '没有匹配的日志行：试试把等级调到「全部」，或者清空搜索与范围。' : '',
    }) as unknown as LogView,
  );
}

export const LogTail = (q: LogQuery): Promise<LogView> => (DEV ? later(fakeLog(q)) : host.LogTail(q));
// --- 另一个窗口改了配色 ------------------------------------------------------------
//
// The scheme is one file both programs write (internal/store/theme.go). When the pet's right-click
// menu flips it, THIS window never loses focus and never re-mounts, so it would keep drawing the old
// scheme until it was reopened - reported as 「在控制面板打开的情况下…不会实时改变」. The host polls
// that file (tools/plugin-market/theme_watch.go) and emits eventMarketTheme; this is the listener.
//
// The dev stub answers with a no-op disposer: there is no second window in the browser, and a fake
// that fired the event would make the smoke test unable to tell "the listener works" from "the stub
// called it".
export const OnTheme = (cb: (theme: string) => void): (() => void) => {
  if (DEV) return () => {};
  return EventsOn('market:theme', (theme: string) => cb(theme === 'light' ? 'light' : 'dark'));
};

// 主题包的那条事件。**单独一条**，不是给上面那条加个字段：明暗连桌宠的右键菜单都能改，
// 而包只有「偏好」页会改。合成一条的话，每次切明暗都得替对方编一个包值，编错了就是
// "切深色顺手把用户选的主题包换回默认"。
//
// 空字符串是合法值（= 没有包），所以这里不做归一化，只校验形状。
export const OnThemePack = (cb: (pack: string) => void): (() => void) => {
  if (DEV) return () => {};
  return EventsOn('market:theme-pack', (pack: string) => cb(/^[a-z0-9-]{1,32}$/.test(pack) ? pack : ''));
};

// --- 私人目录占用 --------------------------------------------------------------
//
// 宿主不设容量上限（2026-11-01 删掉了那道 64 MiB 的闸门，见 internal/plugins/occupancy.go），
// 取而代之的是"把占用量出来给人看"。分级由插件自己给：宿主不知道 `media/stickers/bytes` 是
// 用户的图还是能重下的缓存，猜错的方向是把用户的图标成"可清理"。
// 类型来自生成的那一份（wailsjs/go/models.ts → plugins.PluginOccupancy），不在这里重抄一遍：
// 重抄的代价是两边慢慢对不上，而 `npm run check` 正是为了让"宿主没送这个字段"变成编译错误
// 而不是界面上的一格空白。
export type PluginOccupancy = plugins.PluginOccupancy;
export type FolderOccupancy = plugins.FolderOccupancy;
export const Occupancy = (): Promise<PluginOccupancy[]> => (DEV ? later([]) : host.Occupancy());
export const CleanPluginFolder = (id: string, folder: string): Promise<Record<string, any>> =>
  DEV ? later({ ok: true, freed: 0 }) : host.CleanPluginFolder(id, folder);