import { SetTheme } from './bridge';

/*
 * 这个窗口的配色：壳子（顶栏的切换按钮）与「偏好」页的浅/深控件共用同一份状态。
 *
 * 为什么需要这个模块而不是各页各存一个 `$state`：两处都能改配色，而它们必须同意"现在是哪个"。
 * `document.documentElement.dataset.theme` 仍然是 **CSS 的唯一真相**（`tokens.css` 与
 * 插件帧都按它取值）；这里的 `themeState` 只是它的**响应式镜像**，给按钮和分段控件用。
 */
function read(): 'light' | 'dark' {
  return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
}

export const themeState = $state({ current: read() });

/**
 * 把镜像对齐到 DOM。
 *
 * WHY IT IS NEEDED: 这个模块在 `import` 时就读了一次（那时 `<html>` 上还没有 `data-theme`），
 * 而 `main.ts` 要到 mount 之前才把它设上——于是"顶栏的图标"和"偏好页的分段控件"会先显示
 * 默认值（深色），页面却是浅色。一个说的是 A、显示的是 B 的开关，比没有开关更糟。
 */
export function syncThemeState(): void {
  themeState.current = read();
}

/**
 * 改配色：先改 DOM 属性（CSS 与插件帧立刻看到），再落盘。
 *
 * 落盘失败就翻回去：一个"看着是深色、文件里写着浅色"的窗口，下次打开会自己变回来，
 * 而用户不会知道是哪一次点击没生效。
 */
export async function setTheme(next: 'light' | 'dark'): Promise<void> {
  const prev = read();
  document.documentElement.dataset.theme = next;
  themeState.current = next;
  try {
    await SetTheme(next);
  } catch {
    document.documentElement.dataset.theme = prev;
    themeState.current = prev;
  }
}

/** 顶栏那个按钮用的：在两者之间翻。 */
export function toggleTheme(): Promise<void> {
  return setTheme(themeState.current === 'light' ? 'dark' : 'light');
}
