/**
 * 桌宠那一页的主题包：`Layout.ThemePack → <html data-theme-pack>`。
 *
 * WHY THIS FILE EXISTS. 主题包是**另一个程序**写的（控制中心「偏好」页），桌宠只读。那条链路有
 * 三处各自会静默失败：
 *
 *   1. `Layout` 里没有这个字段（Go 侧忘了加）——页面永远不知道有包，宠物保持默认紫，而控制中心
 *      已经是粉的了。这正是这个仓库历史上「一个亮一个暗」的形状，所以它值得一条测试；
 *   2. 页面读到了但写错位置（写成 `data-pack`、或者没落到 `<html>` 上）——CSS 一条都不匹配；
 *   3. **空字符串被当成一个包写下去**（`data-theme-pack=""`）。那是一个不存在的包，而"没有属性"
 *      才是默认配色。两者在今天的 CSS 里看起来一样，直到有人改默认值。
 *
 * 前两条靠静态接线检查（`.dev/check-frontend.cjs`）看不见运行时，第三条只有这里能测。
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mountApp, baseState, baseLayout } from './test-harness.js';

beforeEach(() => {
  document.body.innerHTML = '';
  delete document.documentElement.dataset.themePack;
  vi.resetModules();
});

describe('主题包', () => {
  it('把 layout 里的 themePack 落到 <html data-theme-pack> 上', async () => {
    const { emit } = await mountApp({ state: baseState() });
    emit('pet:layout', baseLayout({ theme: 'light', themePack: 'sakura' }));
    expect(document.documentElement.dataset.themePack).toBe('sakura');
  });

  it('换包时跟着换，不需要重新挂载', async () => {
    const { emit } = await mountApp({ state: baseState() });
    emit('pet:layout', baseLayout({ theme: 'light', themePack: 'sakura' }));
    emit('pet:layout', baseLayout({ theme: 'light', themePack: 'graphite' }));
    expect(document.documentElement.dataset.themePack).toBe('graphite');
  });

  it('空字符串是"没有包"：删掉属性，而不是写一个空的', async () => {
    const { emit } = await mountApp({ state: baseState() });
    emit('pet:layout', baseLayout({ theme: 'light', themePack: 'sakura' }));
    expect(document.documentElement.dataset.themePack).toBe('sakura');
    emit('pet:layout', baseLayout({ theme: 'light', themePack: '' }));
    expect('themePack' in document.documentElement.dataset).toBe(false);
  });

  it('配色与主题包互不影响：切深色不会把包丢掉', async () => {
    const { emit } = await mountApp({ state: baseState() });
    emit('pet:layout', baseLayout({ theme: 'light', themePack: 'forest' }));
    emit('pet:layout', baseLayout({ theme: 'dark', themePack: 'forest' }));
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.dataset.themePack).toBe('forest');
  });
});
