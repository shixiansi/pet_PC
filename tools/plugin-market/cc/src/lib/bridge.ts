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
 * The fakes are deliberately shaped like the real payloads (a plugin row, a Config with
 * the fields the pages read) rather than like what the pages find convenient.
 *
 * WHY THE TYPES COME FROM wailsjs/go/models.ts AND ARE NOT DECLARED HERE: they were
 * declared here once, in camelCase, because camelCase is what the rest of this file uses
 * and what the fake data was written in. The host sends `plugins.Installed` WITHOUT json
 * tags, so its keys are its Go field names - `ID`, `Enabled`, `Manifest.Name` - and every
 * page that read `row.id` got `undefined` for all four fields on every row. In the browser
 * it looked perfect, because the fake data agreed with the wrong type: the stub was a
 * parallel universe after all, and the only thing that could see the difference was the
 * .exe.
 *
 * So the row is the generated class itself. A field name that the host does not send now
 * fails `npm run check`, and the fakes cannot quietly disagree with the host because they
 * are that same type.
 */
import * as host from '../../../frontend/wailsjs/go/main/Market';
import { main, plugins } from '../../../frontend/wailsjs/go/models';

const DEV = import.meta.env.DEV;

// PluginRow IS the host's row. Re-exported so pages import one name for it instead of
// restating eight fields that then drift.
export type PluginRow = plugins.Installed;
export type PermissionLabel = main.LabelPair;

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
// `createFrom` is how the generated models are meant to be built from a payload - it is
// the same conversion the bindings would do for a real call, so the fake cannot acquire a
// shape the real one could not have.
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

const FAKE_CONFIG = {
  petScale: 1.2,
  focusMinutes: 25,
  shortBreakMinutes: 5,
  longBreakMinutes: 15,
  longBreakEvery: 4,
  autoStartNextPhase: true,
  llmEnabled: true,
  llmProvider: 'deepseek',
  llmBaseUrl: 'https://api.deepseek.com/v1',
  llmModel: 'deepseek-chat',
  llmDailyBudget: 200,
};

// A dev-only delay so a "读取中" state is actually observable instead of flashing past.
const later = <T>(v: T, ms = 120): Promise<T> => new Promise((r) => setTimeout(() => r(v), ms));

export const Installed = (): Promise<PluginRow[]> => (DEV ? later(FAKE_ROWS) : host.Installed());
export const PermissionLabels = (): Promise<PermissionLabel[]> =>
  DEV ? later(FAKE_LABELS) : host.PermissionLabels();
export const Grant = (id: string, granted: string[]): Promise<void | unknown> =>
  DEV ? later(undefined) : host.Grant(id, granted);
export const SetEnabled = (id: string, on: boolean): Promise<void | unknown> =>
  DEV ? later(undefined) : host.SetEnabled(id, on);

export const MainSettings = (): Promise<any> => (DEV ? later(FAKE_CONFIG) : host.MainSettings());
export const SetPetScale = (_v: number): Promise<any> => (DEV ? later(FAKE_CONFIG) : host.SetPetScale(_v));
export const SetFocusMinutes = (_v: number): Promise<any> => (DEV ? later(FAKE_CONFIG) : host.SetFocusMinutes(_v));
export const SetShortBreakMinutes = (_v: number): Promise<any> =>
  DEV ? later(FAKE_CONFIG) : host.SetShortBreakMinutes(_v);
export const SetLongBreakMinutes = (_v: number): Promise<any> =>
  DEV ? later(FAKE_CONFIG) : host.SetLongBreakMinutes(_v);
export const SetLongBreakEvery = (_v: number): Promise<any> => (DEV ? later(FAKE_CONFIG) : host.SetLongBreakEvery(_v));
export const SetAutoStartNextPhase = (_v: boolean): Promise<any> =>
  DEV ? later(FAKE_CONFIG) : host.SetAutoStartNextPhase(_v);

export const Todos = (): Promise<any[]> =>
  DEV
    ? later([
        { id: 'a', title: '写周报', done: false, createdAt: new Date().toISOString() },
        { id: 'b', title: '整理纸箱', done: true, createdAt: new Date().toISOString() },
      ])
    : host.Todos();
export const PomodoroRecords = (): Promise<any[]> =>
  DEV
    ? later([
        { todoTitle: '写周报', focusMinutes: 25, endedAt: new Date().toISOString(), plan: '写完第三节', summary: '写了两节' },
      ])
    : host.PomodoroRecords();
export const DataDir = (): Promise<string> => (DEV ? later('C:\\fake\\PetPC') : host.DataDir());
