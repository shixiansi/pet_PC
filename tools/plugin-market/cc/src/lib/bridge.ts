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
 */
import * as host from '../../../frontend/wailsjs/go/main/Market';

const DEV = import.meta.env.DEV;

export type PluginRow = {
  id: string;
  version?: string;
  enabled: boolean;
  granted?: string[];
  error?: string;
  manifest?: { name?: string; permissions?: string[] };
};

const FAKE_ROWS: PluginRow[] = [
  {
    id: 'com.shixiansi.plugin-chat',
    version: '1.4.1',
    enabled: true,
    granted: ['llm.chat', 'pet.bubble', 'pet.surface', 'plugin.config', 'storage', 'todos.read'],
    manifest: {
      name: '对话插件',
      permissions: ['llm.chat', 'pet.bubble', 'pet.surface', 'plugin.config', 'storage', 'todos.read', 'todos.write'],
    },
  },
  {
    id: 'com.shixiansi.plugin-voice',
    version: '1.0.1',
    enabled: false,
    granted: [],
    manifest: { name: '语音插件', permissions: ['audio', 'events', 'storage'] },
  },
];

const FAKE_LABELS: Record<string, string> = {
  'llm.chat': '调用你已配置的大模型（流式返回）',
  'pet.bubble': '让桌宠弹出一句话的气泡',
  'pet.surface': '在桌宠旁显示自定义浮层面',
  'plugin.config': '保存你自己的插件设置',
  storage: '在自己的私有目录里保存数据',
  'todos.read': '读取待办列表',
  'todos.write': '新增、修改、完成与删除待办',
  audio: '保存并播放音频（例如语音提醒）',
  events: '订阅待办与番茄钟的变化事件',
};

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
export const PermissionLabels = (): Promise<Record<string, string>> =>
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