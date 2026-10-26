/**
 * The plugin market entry point, and the 对话 tab's plugin slot.
 *
 * These two are the visible half of the change that moved plugin management out
 * of the pet and the conversation into a plugin. What is asserted here is what
 * the user can actually observe:
 *
 *   - the settings page has ONE button that launches the market program, and it
 *     neither renders a market of its own nor passes any argument;
 *   - the 对话 tab is a MOUNT POINT whose contents are decided from the installed
 *     list, so "no usable plugin" and "a usable plugin" are the two states to pin.
 *
 * The page is the shipping one and the bridge is the fake in test-harness.js, so
 * a rename in main.js or a changed binding name fails here. The slot's resolution
 * RULE is deliberately exercised through three separate rows: a plugin that is
 * enabled but not granted, and one that is granted but switched off, must both
 * lose the tab - which a test that only checked "is anything installed" would
 * happily pass.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { mountApp, callsTo, baseLayout } from './test-harness.js';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');

const mainJs = readFileSync(path.join(root, 'src', 'main.js'), 'utf8');
const html = readFileSync(path.join(root, 'index.html'), 'utf8');

/** The sentence the slot shows when no installed plugin can fill it. */
const SLOT_HINT = '对话插件还没装好';

const $ = (id) => document.getElementById(id);

/** Lets the app's already-resolved promise chains settle. */
const tick = () => new Promise((r) => setTimeout(r, 0));

/** The call log for one binding. */
const calls = (log, name) => callsTo(log, name);

/**
 * One installed plugin, in the REAL `PluginView` shape.
 *
 * The fields are copied from the Go struct rather than invented, because the
 * resolution reads `panelSlot` (a plain string) and `surface` (a boolean) and an
 * earlier version of this feature got those two wrong in exactly this way - the
 * fake carried a nested `surface:{kind,slot}` object the host never sends, and
 * every test passed against a model that did not exist.
 */
function pluginRow(over = {}) {
  return {
    id: 'com.example.chat',
    name: '示例对话插件',
    version: '1.1.0',
    author: 'shixiansi',
    description: '在面板里与大模型对话',
    enabled: true,
    permissions: ['llm.chat', 'pet.surface'],
    granted: ['llm.chat', 'pet.surface'],
    pending: [],
    netHosts: [],
    panel: true,
    panelSlot: 'chat',
    surface: true,
    entry: 'panel.html',
    dir: 'C:\\data\\plugins\\com.example.chat',
    error: '',
    ...over,
  };
}

/** Mounts the page with the given installed rows, then opens the 对话 tab. */
async function mountWithSlot(rows) {
  // The layout says the panel is OPEN, because that is the state a mounted slot
  // implies: the tab strip is on screen, which is what makes the 对话 tab's
  // visibility observable (and what arms the watcher that keeps it in step).
  const mounted = await mountApp({
    handlers: {
      PluginsInstalled: () => rows,
      GetLayout: () => baseLayout({ panelOpen: true }),
    },
  });
  // The slot is resolved when the tab is ARRIVED AT, not at startup: the market
  // program can install or remove the plugin while the pet runs, so the tab is the
  // refresh point. Clicking it is therefore the only way to observe the rule.
  $('tab-todos').classList.add('hidden');
  $('tab-chat').classList.remove('hidden');
  document.querySelector('.tab[data-tab="chat"]')
    .dispatchEvent(new Event('click', { bubbles: true }));
  await tick();
  await tick();
  return mounted;
}

const slotFrames = () => [...$('chat-slot').querySelectorAll('iframe')];

/** Clears the page and the module registry between tests. */
function resetPage() {
  document.body.innerHTML = '';
  vi.resetModules();
}

afterEach(resetPage);

describe('settings-market: the market entry point (T116)', () => {
  beforeEach(resetPage);

  it('the settings page offers exactly one button that opens the market', async () => {
    await mountApp();

    const buttons = [...document.querySelectorAll('#open-plugin-market')];
    expect(buttons).toHaveLength(1);
    // It lives on the settings tab, which is where a user looks for it.
    expect($('tab-settings').contains(buttons[0])).toBe(true);
  });

  it('clicking the entry button asks the host to open the market exactly once', async () => {
    const { calls: log } = await mountApp();
    const before = calls(log, 'OpenPluginMarket').length;

    $('open-plugin-market').dispatchEvent(new Event('click', { bubbles: true }));
    await tick();
    await tick();

    expect(calls(log, 'OpenPluginMarket')).toHaveLength(before + 1);
  });

  it('the entry button passes no arguments', async () => {
    const { calls: log } = await mountApp();

    $('open-plugin-market').dispatchEvent(new Event('click', { bubbles: true }));
    await tick();

    expect(calls(log, 'OpenPluginMarket')[0].args).toEqual([]);
  });

  it('the host page renders no market of its own', async () => {
    await mountApp();

    // The market is a separate window. An in-page copy would be the 插件 tab this
    // change removed, under a new id.
    expect($('market-list')).toBeNull();
    expect($('plugin-manage')).toBeNull();
    expect($('tab-plugins')).toBeNull();
    // And no tab strip entry for it: the four tabs are the pet's own.
    const tabs = [...document.querySelectorAll('.tab')].map((t) => t.dataset.tab);
    expect(tabs).toEqual(['todos', 'chat', 'records', 'settings']);
  });

  it('the entry button is not an in-page navigation control', async () => {
    // It must not be wired to a tab: clicking it launches a program, and a button
    // that ALSO switched tabs would leave the panel on a tab that shows nothing
    // new.
    const { calls: log } = await mountApp();
    const settings = document.querySelector('.tab[data-tab="settings"]');
    settings.dispatchEvent(new Event('click', { bubbles: true }));
    await tick();

    $('open-plugin-market').dispatchEvent(new Event('click', { bubbles: true }));
    await tick();

    expect($('tab-settings').classList.contains('hidden')).toBe(false);
    expect(calls(log, 'OpenPluginMarket')).toHaveLength(1);
  });
});

describe('settings-market: the 对话 slot resolution (T117/T118)', () => {
  beforeEach(resetPage);

  it('an empty installed list leaves the slot with the hint and no frame (T117)', async () => {
    await mountWithSlot([]);

    expect(slotFrames()).toHaveLength(0);
    // With NOTHING installed the generic sentence is the right one: there is no row
    // to explain, and "install it" is genuinely the next move.
    expect($('chat-slot').textContent).toContain(SLOT_HINT);
    expect($('chat-slot').classList.contains('chat-slot-empty')).toBe(true);
  });

  it('an installed, enabled, granted chat plugin gets one sandboxed frame (T118)', async () => {
    await mountWithSlot([pluginRow()]);

    const frames = slotFrames();
    expect(frames).toHaveLength(1);
    // The sandbox is the security boundary: allow-same-origin is what would hand
    // a plugin the host page's DOM, so it must never appear.
    expect(frames[0].getAttribute('sandbox')).toBe('allow-scripts');
    expect(frames[0].getAttribute('src')).toMatch(/^\/plugin-asset\/com\.example\.chat\//);
    expect(frames[0].dataset.pluginId).toBe('com.example.chat');
    expect(frames[0].dataset.slot).toBe('chat');
    // And no hint is left behind, of either kind.
    expect($('chat-slot').textContent).not.toContain(SLOT_HINT);
    expect($('chat-slot').classList.contains('chat-slot-empty')).toBe(false);
  });

  // The three refusals below assert that the slot shows A reason, not which
  // sentence. Which one it shows is itself behaviour worth pinning - and is
  // asserted separately, right after - but coupling "no frame" to one exact
  // string would make a MORE informative message look like a regression.

  it('a switched-off chat plugin does not keep the slot', async () => {
    // The user switched it off; keeping its interface on screen would contradict
    // the switch.
    await mountWithSlot([pluginRow({ enabled: false })]);

    expect(slotFrames()).toHaveLength(0);
    expect(($('chat-slot').textContent || '').length).toBeGreaterThan(0);
  });

  it('a chat plugin without the surface grant does not get the slot', async () => {
    // The slot draws a sandboxed iframe outside the pet's own box, which is what
    // pet.surface covers - so mounting without it would be a way around the
    // permission prompt.
    await mountWithSlot([pluginRow({ granted: ['llm.chat'] })]);

    expect(slotFrames()).toHaveLength(0);
    expect(($('chat-slot').textContent || '').length).toBeGreaterThan(0);
  });

  it('an enabled plugin that claims no slot is not mounted', async () => {
    // The row is perfectly valid; only the manifest's own declaration says whether
    // it belongs in the 对话 tab.
    await mountWithSlot([pluginRow({ panelSlot: '' })]);

    expect(slotFrames()).toHaveLength(0);
    expect(($('chat-slot').textContent || '').length).toBeGreaterThan(0);
  });

  // --- the diagnosis itself --------------------------------------------------
  //
  // This is the behaviour that makes the difference between a user being stuck and
  // a user knowing what to do. The state it covers is reachable in practice: the
  // conversation plugin 1.0.0 has no `panelSlot`, so installing it leaves the tab
  // insisting the plugin is missing while the market lists it as installed. The
  // generic sentence sends that user off to reinstall the same old package.

  it('names an installed plugin that declares no slot as an OLD VERSION', async () => {
    await mountWithSlot([pluginRow({ id: 'com.example.old', name: '对话插件', version: '1.0.0', panelSlot: '' })]);

    const text = $('chat-slot').textContent;
    expect(slotFrames()).toHaveLength(0);
    // It must say the plugin is present but out of date, name it, and point at the
    // market - because "reinstall" is the wrong move when the catalogue still
    // offers the old package.
    expect(text).toContain('旧版本');
    expect(text).toContain('对话插件');
    expect(text).toContain('插件市场');
    // And it must NOT claim nothing is installed, which is the misleading part.
    expect(text).not.toContain(SLOT_HINT);
  });

  it('names the missing permission when a slot holder was not granted pet.surface', async () => {
    await mountWithSlot([pluginRow({ granted: ['llm.chat'] })]);

    const text = $('chat-slot').textContent;
    expect(text).toContain('pet.surface');
    expect(text).toContain('插件市场');
  });

  it('falls back to the generic sentence when nothing is installed at all', async () => {
    await mountWithSlot([]);
    // Covered above for the frame count; repeated here so the two sentence paths
    // are visibly distinguished in one place.
    expect($('chat-slot').textContent).toContain(SLOT_HINT);
  });

  it('the right plugin wins when several are installed', async () => {
    await mountWithSlot([
      pluginRow({ id: 'com.example.off', enabled: false }),
      pluginRow({ id: 'com.example.other', panelSlot: '', surface: false }),
      pluginRow(),
    ]);

    const frames = slotFrames();
    expect(frames).toHaveLength(1);
    expect(frames[0].dataset.pluginId).toBe('com.example.chat');
  });
});

describe('settings-market: the 对话 tab exists only while a plugin fills it', () => {
  beforeEach(resetPage);

  // The pet has no conversation of its own, so with no plugin the tab opens onto
  // an explanation and nothing else. Offering a tab that can only say "there is
  // nothing here" reads as a broken feature rather than an absent one, so the
  // BUTTON goes away with the plugin.

  it('the tab is offered while a plugin owns the slot', async () => {
    await mountWithSlot([pluginRow()]);
    // mountWithSlot arrives at the tab, so the slot has just been resolved.
    expect($('tab-chat-btn').classList.contains('hidden')).toBe(false);
  });

  it('the tab is hidden when nothing is installed', async () => {
    await mountWithSlot([]);
    expect($('tab-chat-btn').classList.contains('hidden')).toBe(true);
  });

  it('the tab is hidden when the installed plugin cannot fill the slot', async () => {
    // An enabled plugin that claims no slot: the tab has nothing to show, so it
    // must not be offered even though SOMETHING is installed.
    await mountWithSlot([pluginRow({ panelSlot: '' })]);
    expect($('tab-chat-btn').classList.contains('hidden')).toBe(true);
  });

  it('the panel moves to 待办 when the plugin goes away while the tab is showing', async () => {
    // The case that needs the fallback: the conversation is the ACTIVE tab and the
    // plugin is removed underneath it. Leaving the panel there would show an empty
    // conversation body while the strip marked 待办 as active.
    const mounted = await mountWithSlot([pluginRow()]);
    expect($('tab-chat-btn').classList.contains('active')).toBe(true);
    expect($('tab-chat').classList.contains('hidden')).toBe(false);

    // The next read of the installed list reports no usable plugin. `handlers` is
    // returned by reference, so swapping the binding is how a removal is driven.
    mounted.handlers.PluginsInstalled = () => [];
    document.querySelector('.tab[data-tab="chat"]')
      .dispatchEvent(new Event('click', { bubbles: true }));
    await tick();
    await tick();

    expect($('tab-chat-btn').classList.contains('hidden')).toBe(true);
    expect($('tab-chat').classList.contains('hidden')).toBe(true);
    expect($('tab-todos').classList.contains('hidden')).toBe(false);
    expect(document.querySelector('.tab[data-tab="todos"]').classList.contains('active')).toBe(true);
  });

  it('the tab comes back when the plugin is installed again', async () => {
    // The counterpart of the test above, and the one that catches a guard that
    // refuses the switch outright: re-reading the installed list is what discovers
    // the plugin, and the read happens when the tab is arrived at. A guard that
    // blocked the arrival would make the tab impossible to restore.
    const mounted = await mountWithSlot([]);
    expect($('tab-chat-btn').classList.contains('hidden')).toBe(true);

    mounted.handlers.PluginsInstalled = () => [pluginRow()];
    document.querySelector('.tab[data-tab="chat"]')
      .dispatchEvent(new Event('click', { bubbles: true }));
    await tick();
    await tick();

    expect($('tab-chat-btn').classList.contains('hidden')).toBe(false);
    expect($('tab-chat').classList.contains('hidden')).toBe(false);
    expect(slotFrames()).toHaveLength(1);
  });

  it('an updated plugin replaces the running document', async () => {
    // An update in the market keeps the plugin ID and replaces the files behind it,
    // so an id-only reuse test left the PREVIOUS version's document running until
    // the pet was restarted - the market said "updated" and the pet disagreed.
    const mounted = await mountWithSlot([pluginRow({ version: '1.1.0' })]);
    const before = slotFrames()[0];
    expect(before.getAttribute('src')).toBe('/plugin-asset/com.example.chat/panel.html');
    expect(before.dataset.pluginVersion).toBe('1.1.0');

    mounted.handlers.PluginsInstalled = () => [pluginRow({ version: '1.2.0' })];
    document.querySelector('.tab[data-tab="todos"]')
      .dispatchEvent(new Event('click', { bubbles: true }));
    await tick();
    document.querySelector('.tab[data-tab="chat"]')
      .dispatchEvent(new Event('click', { bubbles: true }));
    await tick();
    await tick();

    const after = slotFrames();
    expect(after).toHaveLength(1);
    // A NEW frame, not the same element: the old document cannot be re-pointed at
    // the new files while it is running, so an update has to remount.
    expect(after[0]).not.toBe(before);
    expect(after[0].dataset.pluginVersion).toBe('1.2.0');
  });

  it('a plain re-layout does NOT remount the plugin', async () => {
    // The reason the reuse test exists at all: re-creating the frame restarts the
    // plugin's page, which would throw away an in-progress conversation on every
    // state snapshot. So the version check must not be so eager that it rebuilds on
    // an unchanged plugin.
    const mounted = await mountWithSlot([pluginRow({ version: '1.1.0' })]);
    const before = slotFrames()[0];

    // The same rows again - a snapshot with nothing changed about the plugin.
    document.querySelector('.tab[data-tab="todos"]')
      .dispatchEvent(new Event('click', { bubbles: true }));
    await tick();
    document.querySelector('.tab[data-tab="chat"]')
      .dispatchEvent(new Event('click', { bubbles: true }));
    await tick();
    await tick();

    expect(slotFrames()).toHaveLength(1);
    expect(slotFrames()[0]).toBe(before);
  });

  it('the tab hides on its own when the plugin is disabled, with no click', async () => {
    // The user's report: 「取消点击，面板上的对话tab没有及时的隐藏」 - disabling the
    // plugin in the market left the tab on screen until something else re-read the
    // list. Nothing the user clicks can fix that, because in this direction the tab
    // is VISIBLE - so a watcher bound to "the tab is hidden" never runs.
    //
    // The panel is open here, which is the only state in which the strip is on
    // screen and therefore the only state that is watched.
    //
    // REAL time, not fake timers: mountWithSlot awaits a real setTimeout for the
    // app's boot, so installing fake timers first deadlocks the mount. The watch
    // period is 1.5s, which fits comfortably inside the default test timeout.
    const mounted = await mountWithSlot([pluginRow()]);
    expect($('tab-chat-btn').classList.contains('hidden')).toBe(false);

    // The market disables it. No click, no state push - just the new answer from
    // the binding the watcher polls.
    mounted.handlers.PluginsInstalled = () => [pluginRow({ enabled: false })];

    // Wait out one watch period, plus slack for the read it triggers to settle.
    await new Promise((r) => setTimeout(r, 1700));
    await tick();
    await tick();

    expect($('tab-chat-btn').classList.contains('hidden')).toBe(true);
    expect($('tab-chat').classList.contains('hidden')).toBe(true);
    expect($('tab-todos').classList.contains('hidden')).toBe(false);
    expect(slotFrames()).toHaveLength(0);
  }, 10000);

  it('the watch stops while the panel is closed', async () => {
    // The bound exists to avoid a backend read per period for a tab strip nobody
    // can see.
    //
    // Asserted through the module's own decision rather than by driving a whole
    // panel open/close cycle: GetLayout is not one of the faked bindings, so
    // closing the panel would mean modelling the entire layout payload here - a
    // large fake for a one-line bound. What matters is that the bound is read from
    // the LAYOUT's panelOpen and not from the tab's own visibility, which is the
    // mistake this replaced; the static gate pins that, and the test above proves
    // the watcher itself hides the tab.
    expect(mainJs).toContain('setChatTabWatch(!!layout.panelOpen)');
    expect(mainJs).not.toContain('setChatTabWatch(!available)');
  });
});

describe('settings-market: the host keeps no conversation of its own', () => {
  beforeEach(resetPage);

  it('the 对话 tab body holds the slot and nothing else', async () => {
    await mountApp();

    const chatTab = $('tab-chat');
    expect(chatTab.children).toHaveLength(1);
    expect(chatTab.firstElementChild).toBe($('chat-slot'));
  });

  it('the markup carries no AI configuration and no conversation controls', () => {
    // This is the point of the change, asserted against the SHIPPING markup: the
    // model, the endpoint, the key and the proactive switches are the chat
    // plugin's business now, edited in the market program.
    expect(html).not.toMatch(/id="llm-/);
    expect(html).not.toContain('id="chat-log"');
    expect(html).not.toContain('id="chat-form"');
    expect(html).not.toContain('id="chat-input"');
  });

  it('the rendered settings page mentions neither the AI settings nor 主动说话', async () => {
    // Asserted on the RENDERED TEXT rather than the source: a comment recording
    // where the AI section used to be is worth keeping, and the thing that would
    // mislead a user is a visible control or label, not a note to the next reader.
    await mountApp();

    const settings = $('tab-settings');
    expect(settings.querySelectorAll('[id^="llm-"]')).toHaveLength(0);
    expect(settings.textContent).not.toContain('主动说话');
    expect(settings.textContent).not.toContain('API Key');
  });

  it('main.js no longer names the removed conversation ids', () => {
    // The static gate covers this too; it is repeated here because the two run in
    // different contexts (node vs jsdom) and a module-level rename is exactly the
    // kind of change that can slip past one of them.
    for (const gone of ['chat-log', 'chat-form', 'chat-input', 'chat-cursor', 'llm-enabled']) {
      expect(mainJs).not.toContain(gone);
    }
  });
});
