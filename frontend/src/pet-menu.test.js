/**
 * The pet's own menu: a right-click on the pet, a grid of icon cells, one action
 * per cell.
 *
 * Every assertion drives the REAL shipping page (index.html + src/main.js) through
 * the bridge, the way Go hands events over - the markup, the `els` entry, the
 * renderBubble branch and the action map all have to line up for any of this to be
 * observable, so a rename in one place fails here rather than in the app.
 *
 * Two things this file is deliberately NOT about:
 *
 *   - how the card looks (style.css, and the pixel gates);
 *   - where the card IS. Go owns that: the menu is a bubble kind, so its rectangle
 *     comes from the layout and this page never derives one. The geometry tests for
 *     it live in app_pet_menu_test.go.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { mountApp, layout, bubbleView, callsTo, baseState, petMenuCellsFromGo } from './test-harness.js';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');

const indexHtml = readFileSync(path.join(root, 'index.html'), 'utf8');

/** The cells the menu ships with, in the order they are drawn. */
// 9 格：插件与设置合并成一格「控制中心」（两者原本调的是同一个 binding），腾出来的那一格给了
// 主题切换。格子数必须还是 9 —— 菜单的高度是 Go 里一个常量（bubbleHeightMenu）。
const CELLS = ['todos', 'chat', 'pomodoro', 'remind', 'plugins', 'theme', 'snap', 'hide', 'quit'];

/**
 * 底部那一条（设置 / 完成）。
 *
 * 它不在 3×3 里：卡片高度是 Go 里钉死的常量（bubbleHeightMenu），第十个格子只能从那九个
 * 那里抢高度。所以"设置"是**换一层**而不是加一格——同一张卡片、同一个高度，底部多一条
 * 只占它自己那点高度的按钮。
 */
const WIDE = 'settings';

const $ = (id) => document.getElementById(id);
const isHidden = (id) => $(id).classList.contains('hidden');

/**
 * The app installs its listeners on `window` and on `document`, both of which
 * outlive the body the harness replaces between tests. A previous test's blur
 * handler would therefore act on ITS last bubble, so the listeners are tracked and
 * removed after each test.
 */
const nativeWinAdd = window.addEventListener.bind(window);
const nativeWinRemove = window.removeEventListener.bind(window);
let winBlur = [];
window.addEventListener = (type, fn, opts) => {
  if (type === 'blur') winBlur.push(fn);
  return nativeWinAdd(type, fn, opts);
};

const nativeDocAdd = document.addEventListener.bind(document);
const nativeDocRemove = document.removeEventListener.bind(document);
let docKeydowns = [];
document.addEventListener = (type, fn, opts) => {
  if (type === 'keydown') docKeydowns.push(fn);
  return nativeDocAdd(type, fn, opts);
};

/** Lets the app's already-resolved promise chains settle. */
const tick = () => new Promise((r) => setTimeout(r, 0));

/** The menu card as Go reports it: the bubble slot with the menu's fixed height. */
const menuBubble = (over = {}) => bubbleView('menu', { y: 147, h: 226, ...over });

/** Shows the menu the way Go does: through the layout. */
const showMenu = (emit) => emit('pet:layout', layout({ bubble: menuBubble() }));

/** Right-clicks the pet. */
async function rightClickPet() {
  $('pet').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
  await tick();
}

/** Clicks one cell of the open menu. */
async function clickCell(name) {
  const cell = $('bubble-menu').querySelector(`[data-menu="${name}"]`);
  if (!cell) throw new Error(`no cell for ${name}`);
  cell.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
  await tick();
}

function resetPage() {
  for (const fn of winBlur) nativeWinRemove('blur', fn);
  winBlur = [];
  for (const fn of docKeydowns) nativeDocRemove('keydown', fn);
  docKeydowns = [];
  document.body.innerHTML = '';
  vi.resetModules();
}

afterEach(resetPage);

describe('pet menu: the markup', () => {
  beforeEach(resetPage);

  it('the grid lives INSIDE the bubble card, after the list', () => {
    // Inside #bubble is the whole reason the menu needs no geometry: the card's
    // rectangle is the window region, so a grid in this box is already cropped for.
    //
    // It used to be asserted as "between the list and the textarea". The textarea is
    // gone: its only users were the summary and plan prompts, and both were the host
    // timer's questions. What is left to sit between is the list and the two buttons
    // the reminder card draws.
    const more = indexHtml.indexOf('id="bubble-more"');
    const menu = indexHtml.indexOf('id="bubble-menu"');
    const actions = indexHtml.indexOf('id="bubble-actions"');
    const bubbleEnd = indexHtml.indexOf('<!-- Plugin floating surface');
    expect(more).toBeGreaterThan(-1);
    expect(menu).toBeGreaterThan(more);
    expect(actions).toBeGreaterThan(menu);
    expect(menu).toBeLessThan(bubbleEnd);
  });

  it('has one cell per action, each with an icon and a word', () => {
    const body = indexHtml.slice(
      indexHtml.indexOf('id="bubble-menu"'),
      indexHtml.indexOf('<!-- Plugin floating surface'),
    );
    const found = [...body.matchAll(/data-menu="([a-z]+)"/g)].map((m) => m[1]);
    // 九格动作 + 底部那一条（设置）。顺序就是绘制顺序。
    expect(found).toEqual([...CELLS, WIDE]);
    // One <svg> and one label per cell: a cell with no picture is not the grid this
    // menu is, and a cell with no word is a button nobody can name.
    const cells = CELLS.length + 1;
    expect((body.match(/class="menu-ico"/g) || []).length).toBe(cells);
    expect((body.match(/class="menu-label"/g) || []).length).toBe(cells);
    expect((body.match(/<button type="button" class="menu-cell/g) || []).length).toBe(cells);
    // 开关页是空的：它由 main.js 按 PET_MENU_ITEMS 生成，标签只写一份。
    expect(body).toContain('id="menu-prefs"');
  });
});

describe('pet menu: opening it', () => {
  beforeEach(resetPage);

  it('a right-click on the pet asks the host for the menu', async () => {
    const { calls } = await mountApp();
    await rightClickPet();
    expect(callsTo(calls, 'OpenPetMenu').length).toBe(1);
  });

  it('the right-click is not the tray menu any more', async () => {
    // The regression this pins: the pet's menu used to be the tray icon's list,
    // popped on the tray thread (ShowMenuAtCursor). The page owns this gesture now.
    const { calls } = await mountApp();
    await rightClickPet();
    expect(calls.some((c) => c.name === 'ShowMenuAtCursor')).toBe(false);
  });

  it('the rendered card is the grid alone', async () => {
    const { emit } = await mountApp();
    showMenu(emit);
    await tick();
    expect(isHidden('bubble-menu')).toBe(false);
    // The other occupants of the shared card are cleared, not merely overlapped:
    // the menu has no heading, no list and no buttons of its own.
    expect(isHidden('bubble-title')).toBe(true);
    expect(isHidden('bubble-list')).toBe(true);
    expect($('bubble-secondary').classList.contains('hidden')).toBe(true);
    expect($('bubble-primary').classList.contains('hidden')).toBe(true);
  });

  it('the grid goes away again when the card does', async () => {
    const { emit } = await mountApp();
    showMenu(emit);
    await tick();
    expect(isHidden('bubble-menu')).toBe(false);

    // The card itself can go without a kind change (Escape, a drag, the panel
    // opening): the grid goes with it because it is inside the card.
    emit('pet:layout', layout({ bubble: null }));
    await tick();
    expect(isHidden('bubble')).toBe(true);

    // And when the card comes back for ANOTHER kind the grid must be cleared
    // rather than merely covered: a reminder drawn over a launcher is two cards in
    // one rectangle.
    emit('pet:layout', layout({ bubble: bubbleView('reminder') }));
    await tick();
    expect(isHidden('bubble')).toBe(false);
    expect(isHidden('bubble-menu')).toBe(true);
  });
});

describe('pet menu: what each cell does', () => {
  beforeEach(resetPage);

  const openAndClick = async (name, { state, handlers } = {}) => {
    const mounted = await mountApp({ state, handlers });
    showMenu(mounted.emit);
    await tick();
    await clickCell(name);
    await tick();
    return mounted;
  };

  it('日程 opens the surface of whichever plugin claims the slot', async () => {
    // The cell used to open the PANEL's 待办 tab. There is no panel and no tab: the list is
    // the todo plugin's data, and the cell resolves whichever plugin claims the slot - the
    // same rule the 对话 and 番茄钟 cells follow.
    const claim = {
      id: 'com.example.todo', name: '示例待办', version: '1.1.0', author: 'shixiansi',
      description: '', enabled: true,
      permissions: ['pet.surface'], granted: ['pet.surface'], pending: [], netHosts: [],
      panel: false, panelSlot: 'todos', surface: true, background: true,
      entry: 'panel.html', dir: 'C:\\data\\plugins\\com.example.todo', error: '',
    };
    const { calls } = await openAndClick('todos', {
      handlers: { PluginsInstalled: () => [claim] },
    });
    const opens = callsTo(calls, 'OpenPluginSurface');
    expect(opens.length).toBe(1);
    expect(opens[0].args).toEqual(['com.example.todo']);
    expect(calls.some((c) => /Panel/.test(c.name))).toBe(false);
  });

  it('日程 falls back to the control center when nothing claims the slot', async () => {
    const { calls } = await openAndClick('todos', { handlers: { PluginsInstalled: () => [] } });
    expect(callsTo(calls, 'OpenPluginSurface').length).toBe(0);
    expect(callsTo(calls, 'OpenPluginMarket').length).toBe(1);
  });

  it('对话 opens the conversation surface (the same call the hover button makes)', async () => {
    const { calls } = await openAndClick('chat');
    expect(callsTo(calls, 'OpenChatPluginSurface').length).toBe(1);
  });

  it('番茄钟 opens the plugin that claims the slot, through its own surface', async () => {
    // The timer is a plugin now (com.shixiansi.plugin-pomodoro): the cell's job is to
    // hand the gesture to whoever claims the `pomodoro` slot, and the host never has to
    // know which plugin that is - the same rule as the 日程 and 对话 cells.
    const claim = {
      id: 'com.example.pomo', name: '示例番茄钟', version: '2.0.0', author: 'shixiansi',
      description: '', enabled: true,
      permissions: ['pet.surface'], granted: ['pet.surface'], pending: [], netHosts: [],
      panel: false, panelSlot: 'pomodoro', surface: true, background: false,
      entry: 'panel.html', dir: 'C:\\data\\plugins\\com.example.pomo', error: '',
    };
    const { calls } = await openAndClick('pomodoro', {
      handlers: { PluginsInstalled: () => [claim] },
    });
    // The install list is read fresh: the market program can install the plugin while
    // the pet is running, so a cached "nobody claims it" would be wrong forever after.
    expect(callsTo(calls, 'PluginsInstalled').length).toBeGreaterThan(0);
    const opens = callsTo(calls, 'OpenPluginSurface');
    expect(opens.length).toBe(1);
    expect(opens[0].args).toEqual(['com.example.pomo']);
    // The surface takes the slot from the card, and the cell does not dismiss it.
    expect(callsTo(calls, 'DismissBubble').length).toBe(0);
  });

  it('番茄钟 falls back to the control center when nothing claims the slot', async () => {
    // There is no host timer to switch on any more, so the fallback is not "flip it":
    // it is the one place the user can do something about a missing plugin. A cell that
    // did nothing would read as a broken button, and a toast would be invisible (the
    // toast lives inside the panel, which is closed while this card is up).
    const { calls } = await openAndClick('pomodoro', { handlers: { PluginsInstalled: () => [] } });
    expect(callsTo(calls, 'OpenPluginSurface').length).toBe(0);
    expect(callsTo(calls, 'OpenPluginMarket').length).toBe(1);
  });

  it('提醒 asks for a reminder now, and leaves the card to be replaced by it', async () => {
    const { calls } = await openAndClick('remind');
    expect(callsTo(calls, 'RemindNow').length).toBe(1);
    // Go re-crops to the reminder bubble, so dismissing here would be a second
    // crop in the same gesture.
    expect(callsTo(calls, 'DismissBubble').length).toBe(0);
  });

  it('控制中心 opens the market and closes the card', async () => {
    const { calls } = await openAndClick('plugins');
    expect(callsTo(calls, 'OpenPluginMarket').length).toBe(1);
    expect(callsTo(calls, 'DismissBubble').length).toBe(1);
  });

  // 主题那一格走 TogglePanelTheme，然后**重新应用一次 layout**：配色是 Layout.Theme 说的，
  // 页面不自己猜（自己猜就会出现"文件写深色、界面是浅色"）。
  // 主题那一格只调 ToggleCardTheme；**重画靠宿主推过来的 `pet:layout` 事件**（Layout.Theme 是
  // 页面唯一的配色来源）。这里刻意断言"没有额外去取 layout"：上一版把 binding 返回的主题字符串
  // 直接喂给了 applyLayout，于是 `layout.petX` 变成 undefined，菜单先消失、再以旧配色回来。
  it('主题 flips the shared scheme through the binding, and does not re-apply a layout itself', async () => {
    const { calls } = await openAndClick('theme');
    expect(callsTo(calls, 'ToggleCardTheme').length).toBe(1);
    // The LAST call is the binding: nothing after it fetches a layout. (A count of zero would be
    // wrong - the page asks for one at boot - so the assertion is about what the click added.)
    expect(calls[calls.length - 1].name).toBe('ToggleCardTheme');
  });

  it('归位 and 隐藏 call the bindings the tray uses', async () => {
    resetPage();
    const snap = await openAndClick('snap');
    expect(callsTo(snap.calls, 'ResetToDefaultPosition').length).toBe(1);

    resetPage();
    const hide = await openAndClick('hide');
    expect(callsTo(hide.calls, 'HidePetForUser').length).toBe(1);
  });

  it('退出 quits', async () => {
    const { calls } = await openAndClick('quit');
    expect(callsTo(calls, 'Quit').length).toBe(1);
  });

  it('a cell with no action does nothing instead of throwing', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { calls, emit } = await mountApp();
    showMenu(emit);
    await tick();
    // A cell added to the markup and forgotten in PET_MENU_ACTIONS: the grid has to
    // survive it, and say so in the console rather than looking like a dead button.
    const before = calls.length;
    const stray = document.createElement('button');
    stray.dataset.menu = 'no-such-action';
    $('bubble-menu').append(stray);
    stray.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await tick();
    expect(warn).toHaveBeenCalled();
    expect(calls.length).toBe(before);
    warn.mockRestore();
  });
});

describe('pet menu: closing it', () => {
  beforeEach(resetPage);

  it('a second right-click closes it, so the gesture has both directions', async () => {
    const { calls, emit } = await mountApp();
    showMenu(emit);
    await tick();
    await rightClickPet();
    expect(callsTo(calls, 'DismissBubble').length).toBe(1);
    expect(callsTo(calls, 'OpenPetMenu').length).toBe(0);
  });

  it('Escape closes it like any other bubble', async () => {
    const { calls, emit } = await mountApp();
    showMenu(emit);
    await tick();
    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );
    await tick();
    expect(callsTo(calls, 'DismissBubble').length).toBe(1);
  });

  it('losing focus closes it, because a card cannot see the click that dismissed it', async () => {
    const { calls, emit } = await mountApp();
    showMenu(emit);
    await tick();
    window.dispatchEvent(new Event('blur'));
    await tick();
    expect(callsTo(calls, 'DismissBubble').length).toBe(1);
  });

  it('losing focus with nothing up calls nothing', async () => {
    const { calls } = await mountApp();
    window.dispatchEvent(new Event('blur'));
    await tick();
    expect(callsTo(calls, 'DismissBubble').length).toBe(0);
  });
});

describe('pet menu: it can take the slot back from a bubble', () => {
  beforeEach(resetPage);

  it('a right-click while a reminder is up still opens the menu', async () => {
    // This block used to assert the OPPOSITE for three kinds, and the reason was the
    // host timer: the picker, the summary prompt and the plan prompt were gates the
    // timer was parked behind, so the menu had to refuse the slot and send the caret
    // into the question instead.
    //
    // Those three questions are the pomodoro plugin's own surface now, and nothing on
    // this page is waiting for an answer. So the gesture simply works: a reminder is
    // dismissable like anything else, and the menu takes its place.
    const { calls, emit } = await mountApp();
    emit('pet:layout', layout({ bubble: bubbleView('reminder', { title: '该歇一会儿了' }) }));
    await tick();
    await rightClickPet();
    expect(callsTo(calls, 'OpenPetMenu').length).toBe(1);
  });
});

/**
 * 设置页：右键菜单里哪些格子显示。
 *
 * 三件事在这里被钉住，因为它们是这个功能唯一会真出错的地方：
 *
 *   1. 它是一层**模式**，不是第十个格子（卡片高度是 Go 的常量，加一格要从九格那里抢）；
 *   2. 「只能开不能关」的判据在宿主，页面只是不去提供那个开关——所以一个手改过的
 *      settings.json 藏不掉「设置」与「退出」；
 *   3. 每一次点击都立刻生效（本地先落、再让宿主对齐），因为一个点了要等一次往返的开关
 *      读起来就是"没反应"。
 */
describe('pet menu: the switches (设置)', () => {
  beforeEach(resetPage);

  /** 打开菜单，并点进设置页。 */
  async function openPrefs(extra = {}) {
    const mounted = await mountApp(extra);
    showMenu(mounted.emit);
    await tick();
    await clickCell('settings');
    return mounted;
  }

  const prefsRows = () => [...$('menu-prefs').querySelectorAll('[data-toggle]')];
  const wideLabel = () => $('menu-wide-label').textContent;

  it('「设置」把同一张卡片换成开关页，格子藏起来，底部那一条变成「完成」', async () => {
    await openPrefs();
    // 同一张卡片：外层还是那一块（Go 只裁一次），换的是里面的那一层。
    expect(isHidden('bubble-menu')).toBe(false);
    expect(isHidden('menu-grid')).toBe(true);
    expect(isHidden('menu-prefs')).toBe(false);
    expect(wideLabel()).toBe('完成');
    expect($('menu-wide').dataset.menu).toBe('done');

    await clickCell('done');
    expect(isHidden('menu-grid')).toBe(false);
    expect(isHidden('menu-prefs')).toBe(true);
    expect(wideLabel()).toBe('设置');
  });

  it('每次重新打开都落在动作模式', async () => {
    const { emit } = await openPrefs();
    // 停在开关页时把卡片收起，再右键一次：菜单是"选一件事做"的卡片，不该记得
    // 上一次停在哪一层（用户也没有别的地方看那九个格子）。
    emit('pet:layout', layout({ bubble: null }));
    await tick();
    showMenu(emit);
    await tick();
    expect(isHidden('menu-grid')).toBe(false);
    expect(isHidden('menu-prefs')).toBe(true);
    expect(wideLabel()).toBe('设置');
  });

  it('一行一个格子；「设置」与「退出」写着「固定」且点不动', async () => {
    await openPrefs();
    const rows = prefsRows();
    // 行与顺序都来自宿主那张表（app_pet_menu.go 的 petMenuCells），不是这一页编的——
    // 所以这条断言等于"页面上有的格子，宿主都认识；宿主认识的，页面上都有"。
    expect(rows.map((row) => row.dataset.toggle)).toEqual(petMenuCellsFromGo().map((c) => c.id));
    expect(rows.map((row) => row.dataset.toggle)).toContain('settings');
    expect(rows.map((row) => row.dataset.toggle)).toContain('quit');
    for (const row of rows) {
      const label = row.querySelector('.menu-label');
      const stateText = row.querySelector('.menu-state');
      expect(label, '每个开关都要有名字').toBeTruthy();
      expect(stateText, '每个开关都要说它现在是开还是关').toBeTruthy();
      const fixed = row.dataset.toggle === 'settings' || row.dataset.toggle === 'quit';
      expect(row.disabled).toBe(fixed);
      expect(stateText.textContent).toBe(fixed ? '固定' : '开');
    }
  });

  it('关掉一格：立刻藏起来，并按菜单顺序存给宿主', async () => {
    const { calls } = await openPrefs();
    const row = $('menu-prefs').querySelector('[data-toggle="theme"]');
    row.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    await tick();

    const saved = callsTo(calls, 'SetPetMenuHidden');
    expect(saved.length).toBe(1);
    // 顺序就是菜单顺序：同一个选择永远生成同一份文件，diff 里只出现真正改了的那一行。
    expect(saved[0].args[0]).toEqual(['theme']);
    // 本地先落：不用等宿主那一趟往返。
    expect($('menu-grid').querySelector('[data-menu="theme"]').classList.contains('hidden')).toBe(
      true,
    );
    expect(row.querySelector('.menu-state').textContent).toBe('关');
  });

  it('再点一次就开回来（denylist 里删掉）', async () => {
    const { calls } = await openPrefs({ state: baseState({ config: { menuHidden: ['theme', 'snap'] } }) });
    const row = $('menu-prefs').querySelector('[data-toggle="theme"]');
    row.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    await tick();
    expect(callsTo(calls, 'SetPetMenuHidden')[0].args[0]).toEqual(['snap']);
  });

  it('State 里的 menuHidden 决定哪些格子显示，开关页那一行写「关」', async () => {
    const { emit } = await mountApp({
      state: baseState({ config: { menuHidden: ['theme', 'hide'] } }),
    });
    showMenu(emit);
    await tick();
    const grid = $('menu-grid');
    expect(grid.querySelector('[data-menu="theme"]').classList.contains('hidden')).toBe(true);
    expect(grid.querySelector('[data-menu="hide"]').classList.contains('hidden')).toBe(true);
    expect(grid.querySelector('[data-menu="todos"]').classList.contains('hidden')).toBe(false);

    await clickCell('settings');
    expect($('menu-prefs').querySelector('[data-toggle="theme"] .menu-state').textContent).toBe('关');
    expect($('menu-prefs').querySelector('[data-toggle="todos"] .menu-state').textContent).toBe('开');
  });

  it('手改过的 settings.json 藏不掉「设置」与「退出」', async () => {
    // Go 侧的 NormalisePetMenuHidden 已经会把它们丢掉（那是真正的边界，settings.json 是
    // 一个用户能直接编辑的纯文件）。页面这一层再挡一次：两处都拦才叫"只能开不能关"——
    // 只有一处时，另一处的一个手滑就是"用户把回家的路关了"。
    const { emit } = await mountApp({
      state: baseState({ config: { menuHidden: ['settings', 'quit'] } }),
    });
    showMenu(emit);
    await tick();
    const grid = $('menu-grid');
    for (const id of CELLS) {
      expect(
        grid.querySelector(`[data-menu="${id}"]`).classList.contains('hidden'),
        `${id} 不该被藏`,
      ).toBe(false);
    }
    expect($('menu-wide').classList.contains('hidden')).toBe(false);
  });
});
