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
import { mountApp, layout, bubbleView, callsTo } from './test-harness.js';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');

const indexHtml = readFileSync(path.join(root, 'index.html'), 'utf8');

/** The cells the menu ships with, in the order they are drawn. */
// 9 格：插件与设置合并成一格「控制中心」（两者原本调的是同一个 binding），腾出来的那一格给了
// 主题切换。格子数必须还是 9 —— 菜单的高度是 Go 里一个常量（bubbleHeightMenu）。
const CELLS = ['todos', 'chat', 'pomodoro', 'remind', 'plugins', 'theme', 'snap', 'hide', 'quit'];

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
    expect(found).toEqual(CELLS);
    // One <svg> and one label per cell: a cell with no picture is not the grid this
    // menu is, and a cell with no word is a button nobody can name.
    expect((body.match(/class="menu-ico"/g) || []).length).toBe(CELLS.length);
    expect((body.match(/class="menu-label"/g) || []).length).toBe(CELLS.length);
    expect((body.match(/<button type="button" class="menu-cell/g) || []).length).toBe(
      CELLS.length,
    );
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
  it('主题 flips the card scheme through the binding, then re-applies the layout', async () => {
    const { calls } = await openAndClick('theme');
    expect(callsTo(calls, 'ToggleCardTheme').length).toBe(1);
    expect(callsTo(calls, 'GetLayout').length).toBeGreaterThan(0);
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
