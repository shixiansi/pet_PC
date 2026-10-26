/**
 * Tests for the plugin market page.
 *
 * These load the REAL index.html and the REAL style.css and then import the real
 * main.js against a fake Wails bridge. That combination is deliberate: several of
 * the things asserted here are properties of the MARKUP AND STYLESHEET (one view in
 * the flow at a time, a wrapping permission row, a card that is one plugin), and a
 * hand-written fixture would happily satisfy them while the shipping page did not.
 *
 * The bridge records every call, so the other kind of assertion - what the page ASKED
 * THE BACKEND TO DO after a refresh - is checked against the actual call log rather
 * than against a re-implementation of it.
 *
 * What this file CANNOT check is anything about real layout: jsdom does not do
 * layout, so "one view is in the flow" is verified structurally and by the cascade,
 * and the rest is left to .dev/verify-plugin-market.ps1, which drives the real window.
 *
 * ROUND 7 REWROTE THE SKELETON, and with it this file. The window is a storefront now
 * (a grid of cards, and one plugin's page) instead of a two-pane master-detail, so the
 * tests that pinned the OLD shape - "the address box takes every pixel the buttons do
 * not", "one list at a time behind a tab switch", "the head card holds the toolbar" -
 * were replaced by the requirements the new shape has to meet. The intent behind each
 * one survived the rewrite; the assertions had to, because the thing they measure is
 * not the same object any more.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');

const css = readFileSync(path.join(here, 'style.css'), 'utf8');
const page = readFileSync(path.join(root, 'index.html'), 'utf8');
// The shipping markup, minus the module script (jsdom must not fetch it; the app
// is imported by the test instead, after the DOM exists).
const bodyHtml = page
  .slice(page.indexOf('<body>') + '<body>'.length, page.indexOf('</body>'))
  .replace(/<script[\s\S]*?<\/script>/g, '');

/** One catalogue row, shaped like plugins.MarketEntry. */
function entry(over = {}) {
  return {
    id: 'com.shixiansi.plugin-chat',
    name: '对话插件',
    version: '1.3.0',
    author: 'shixiansi',
    description: '让你的桌宠接入大模型，支持自定义皮肤与浮层面板。',
    permissions: ['llm.chat', 'pet.bubble', 'pet.surface', 'plugin.config', 'storage'],
    netHosts: [],
    download: 'dist/com.shixiansi.plugin-chat-1.3.0.zip',
    installed: false,
    installedVersion: '',
    ...over,
  };
}

/** One installed row, shaped like plugins.Installed. */
function installed(over = {}) {
  return {
    ID: 'com.shixiansi.plugin-chat',
    Version: '1.3.0',
    Enabled: true,
    Granted: ['llm.chat', 'pet.bubble', 'pet.surface', 'plugin.config', 'storage'],
    InstalledAt: '2024-01-01T00:00:00Z',
    Dir: 'C:/tmp/plugins/com.shixiansi.plugin-chat',
    Error: '',
    Manifest: {
      id: 'com.shixiansi.plugin-chat',
      name: '对话插件',
      version: '1.3.0',
      author: 'shixiansi',
      description: '让你的桌宠接入大模型，支持自定义皮肤与浮层面板。',
      permissions: ['llm.chat', 'pet.bubble', 'pet.surface', 'plugin.config', 'storage'],
      panel: true,
      panelSlot: 'chat',
      // `entry` is REQUIRED by the manifest validator, so every real installed row
      // carries it, and the workspace decides whether to host an interface from it.
      entry: 'panel.html',
      config: [
        // The shipping plugin declares fourteen items, and exactly one of them is a
        // secret and exactly one is the host-whitelist item. The fixture carries the
        // same shape - including a real `llm*` key name - because the editor's behaviour
        // depends on those three facts, and a fixture with invented keys would let a
        // regression in the secret/whitelist split pass unnoticed.
        { key: 'llmBaseUrl', label: '接口地址', type: 'text', default: 'https://api.deepseek.com/v1', hostWhitelist: true },
        { key: 'llmModel', label: '模型', type: 'text', default: 'deepseek-chat' },
        { key: 'llmTemperature', label: '随机性', type: 'number', default: '0.8' },
        { key: 'llmApiKey', label: 'API Key', type: 'secret', secret: true },
      ],
    },
    ...over,
  };
}

let calls = [];
let handlers = {};

/**
 * Installs the fake bridge. Only the two bindings the page actually uses are
 * faked with data; everything else resolves to null, which is what the real
 * bindings do for the calls whose result the page ignores.
 */
function installBridge() {
  calls = [];
  const bind = (name) => (...args) => {
    calls.push({ name, args });
    const handler = handlers[name];
    try {
      return Promise.resolve(handler ? handler(...args) : null);
    } catch (err) {
      return Promise.reject(err);
    }
  };

  const names = [
    'Catalogue', 'GetConfig', 'Grant', 'Install', 'Installed', 'PluginMarketPath',
    'PluginRequest', 'RememberSelection', 'SaveConfig', 'SaveMarketURL', 'SelectedID',
    'SetEnabled', 'SetTheme', 'Theme', 'Uninstall', 'Update', 'Warnings',
  ];
  const Market = {};
  for (const n of names) Market[n] = bind(n);
  window.go = { main: { Market } };
}

/** Puts the shipping markup and stylesheet into the document. */
function resetDom() {
  document.head.innerHTML = '';
  document.body.innerHTML = bodyHtml;
  const style = document.createElement('style');
  style.textContent = css;
  document.head.appendChild(style);
  // Sanity: a fixture mismatch here would make every other assertion lie.
  expect(document.getElementById('grid-view')).not.toBeNull();
  expect(document.getElementById('detail-view')).not.toBeNull();
}

/**
 * Merges every stylesheet declaration whose selector matches `el`.
 *
 * jsdom does not do layout and its computed styles only cover part of the CSS
 * property table, so asking for `flex-grow` or `white-space` through
 * getComputedStyle is not dependable. Walking the real rules keeps the assertion
 * honest: it is still the shipping stylesheet deciding, just read directly.
 */
function declared(el) {
  const out = {};
  for (const sheet of document.styleSheets) {
    let rules;
    try {
      rules = sheet.cssRules;
    } catch {
      continue;
    }
    for (const rule of rules) {
      if (!rule.selectorText || !rule.style) continue;
      for (const sel of rule.selectorText.split(',')) {
        const one = sel.trim();
        if (!one) continue;
        try {
          if (!el.matches(one)) continue;
        } catch {
          continue;
        }
        for (let i = 0; i < rule.style.length; i++) {
          const prop = rule.style[i];
          out[prop] = rule.style.getPropertyValue(prop);
        }
      }
    }
  }
  return out;
}

/** Lets the page's promise chains settle. */
async function flush(n = 10) {
  for (let i = 0; i < n; i++) await new Promise((r) => setTimeout(r, 0));
}

/** Loads a fresh copy of the app against the currently installed bridge. */
async function loadApp() {
  vi.resetModules();
  return import('./main.js');
}

const $ = (id) => document.getElementById(id);
const hidden = (el) => Boolean(el?.classList.contains('hidden'));

beforeEach(() => {
  handlers = {
    Installed: () => [],
    Warnings: () => [],
    SelectedID: () => '',
    // Dark unless a test says otherwise: the settings file's default, and the
    // scheme this window shipped with.
    Theme: () => 'dark',
    SetTheme: () => null,
    GetConfig: (id) => ({ pluginId: id, name: '对话插件', dir: '', items: [] }),
    Catalogue: () => ({
      base: 'https://example.invalid/plugins',
      fallback: false,
      notice: '',
      entries: [entry()],
    }),
  };
  resetDom();
  installBridge();
});

describe('layout', () => {
  it('shows one view at a time: the grid and the plugin page are siblings', async () => {
    await loadApp();
    await flush();

    const grid = $('grid-view');
    const detail = $('detail-view');

    expect(grid).not.toBe(detail);
    expect(grid.contains(detail)).toBe(false);
    expect(detail.contains(grid)).toBe(false);

    // Exactly one is in the flow, and neither is nested in a second scroller: two
    // scroll containers on the same axis was the defect that started this window.
    expect(hidden(grid)).toBe(false);
    expect(hidden(detail)).toBe(true);
    expect(declared(grid)['flex-direction']).toBe('column');
    expect(declared(detail)['flex-direction']).toBe('column');
    // The grid's scroller is the card area, not the view itself.
    expect(declared(document.querySelector('.grid-scroll'))['overflow-y']).toBe('auto');
  });

  it('hands the hosted plugin the permission list the row actually carries', async () => {
    // THE DEAD-CONTROLS REGRESSION. `plugin-ui.js` read `inst.granted`, but the market
    // passes Go's `plugins.Installed` struct STRAIGHT THROUGH - no JSON tags - so the
    // property is `Granted`, capital G. The typo produced `undefined`, the `|| []`
    // turned that into an empty list, and the plugin was told the user had granted
    // nothing.
    handlers.Installed = () => [installed()];
    await loadApp();
    await flush();

    // The frame the workspace mounted is the plugin's OWN document, addressed by id:
    // that is how the plugin's requests are attributed to the right plugin on the Go
    // side, so a wrong id here is a permission check against the wrong plugin.
    const frame = document.querySelector('.plugin-frame');
    expect(frame, 'the workspace did not mount an iframe').not.toBeNull();
    expect(frame.getAttribute('src')).toContain('/plugin-asset/com.shixiansi.plugin-chat/panel.html');
    expect(frame.getAttribute('sandbox'), 'the plugin document is not sandboxed').toBe('allow-scripts');
    expect(frame.dataset.pluginId).toBe('com.shixiansi.plugin-chat');
  });

  it('renders every declared permission as a wrapping row', async () => {
    handlers.Installed = () => [installed()];
    await loadApp();
    await flush();

    const list = $('perm-list');
    const rows = [...list.querySelectorAll('.perm')];
    expect(rows).toHaveLength(5);
    for (const row of rows) {
      const text = row.querySelector('.perm-text');
      expect(text, 'a permission row has no text container').not.toBeNull();
      const style = declared(text);
      expect(style.display).toBe('flex');
      expect(style['flex-wrap']).toBe('wrap');
      // Load-bearing: without this the row cannot shrink below its content width, and a
      // long name then pushes the row wider than the panel instead of wrapping.
      expect(style['min-width'], 'the row cannot shrink below its content without this')
        .toMatch(/^0(px)?$/);
    }
  });

  it('lays the permission list out as one column, not a grid', async () => {
    handlers.Installed = () => [installed()];
    await loadApp();
    await flush();

    // A two-column grid was tried once and reverted: halving the list's height moved
    // what followed it into the real-window gate's sampled band.
    expect(declared($('perm-list'))['flex-direction']).toBe('column');
  });

  it('builds one card per plugin, from the catalogue AND the installed list', async () => {
    // A plugin that is installed but that the catalogue does not list must still be
    // visible: it is installed, and the old separate 已安装 list existed for exactly
    // that case.
    handlers.Installed = () => [installed()];
    handlers.Catalogue = () => ({
      base: '',
      fallback: false,
      notice: '',
      entries: [
        entry(),
        entry({ id: 'com.shixiansi.plugin-qq', name: 'QQ 群聊插件', version: '1.12.0', installed: false }),
      ],
    });
    const app = await loadApp();
    await flush();
    app.backToGrid?.();
    await flush();

    const cards = [...$('plugin-grid').querySelectorAll('.card')];
    expect(cards).toHaveLength(2);
    expect(cards.map((c) => c.dataset.id)).toEqual([
      'com.shixiansi.plugin-chat',
      'com.shixiansi.plugin-qq',
    ]);

    // …and a locally installed plugin the catalogue has never heard of is a third:
    // it is installed, so it must be visible even though no catalogue row describes it.
    handlers.Installed = () => [
      installed(),
      installed({
        ID: 'com.local.plugin',
        Version: '0.1.0',
        Manifest: { ...installed().Manifest, id: 'com.local.plugin', name: '本地插件' },
      }),
    ];
    await app.refreshInstalled();
    app.renderGrid();
    await flush();
    const withLocal = [...$('plugin-grid').querySelectorAll('.card')].map((c) => c.dataset.id);
    expect(withLocal).toContain('com.local.plugin');
  });

  it('filters the grid by chip and by the search box', async () => {
    handlers.Installed = () => [installed()];
    handlers.Catalogue = () => ({
      base: '',
      fallback: false,
      notice: '',
      entries: [
        entry(),
        entry({
          id: 'com.shixiansi.plugin-bili',
          name: 'B 站刷视频插件',
          version: '1.0.0',
          author: 'someone-else',
          description: '按分区刷 B 站并让大模型摘要。',
          permissions: ['net.fetch'],
        }),
      ],
    });
    const app = await loadApp();
    await flush();
    app.backToGrid();
    await flush();

    const count = () => $('plugin-grid').querySelectorAll('.card').length;
    expect(count()).toBe(2);

    // Filter: 未安装 leaves only the one that is not on disk.
    $('filter-market').click();
    await flush();
    expect(count()).toBe(1);
    expect($('plugin-grid').querySelector('.card').dataset.id).toBe('com.shixiansi.plugin-bili');
    expect($('filter-market').classList.contains('is-on')).toBe(true);
    expect($('filter-market').getAttribute('aria-pressed')).toBe('true');

    // Filter: 已安装 is the other one.
    $('filter-installed').click();
    await flush();
    expect(count()).toBe(1);
    expect($('plugin-grid').querySelector('.card').dataset.id).toBe('com.shixiansi.plugin-chat');

    // Back to all, then search. The query matches the DESCRIPTION/author too, not just
    // the name, which is what makes "find that plugin that does X" work.
    $('filter-all').click();
    await flush();
    const search = $('search');
    search.value = 'someone-else';
    search.dispatchEvent(new Event('input', { bubbles: true }));
    await flush();
    expect(count()).toBe(1);
    expect($('plugin-grid').querySelector('.card').dataset.id).toBe('com.shixiansi.plugin-bili');

    // A query that matches nothing says so instead of showing an empty page.
    search.value = 'zzz-nothing';
    search.dispatchEvent(new Event('input', { bubbles: true }));
    await flush();
    expect(count()).toBe(0);
    expect(hidden($('grid-empty'))).toBe(false);
    expect($('grid-empty').textContent).toMatch(/zzz-nothing/);
  });

  it('opens on the grid when nothing is installed', async () => {
    // A first run is here to install something: landing it on a plugin page would be
    // landing it on a page about a plugin it does not have.
    await loadApp();
    await flush();
    expect(hidden($('grid-view'))).toBe(false);
    expect(hidden($('detail-view'))).toBe(true);
    expect($('plugin-grid').querySelectorAll('.card')).toHaveLength(1);
  });

  it('opens on the remembered plugin when there is one', async () => {
    // The window doubles as the place a plugin's own workspace lives, so "where I was"
    // survives a restart - while "which view I was on" does not.
    handlers.Installed = () => [installed()];
    handlers.SelectedID = () => 'com.shixiansi.plugin-chat';
    await loadApp();
    await flush();
    expect(hidden($('detail-view'))).toBe(false);
    expect(hidden($('grid-view'))).toBe(true);
    expect($('detail-name').textContent).toBe('对话插件');
  });

  it('gives a plugin one monogram, the same on its card and in the hero', async () => {
    // IDENTITY. The tint is a hash of the plugin id, so the same plugin looks like
    // itself in the grid and on its page - which is what makes "the card I clicked"
    // and "the page I arrived at" visibly the same object.
    handlers.Installed = () => [installed()];
    handlers.SelectedID = () => '';
    const app = await loadApp();
    await flush();
    app.backToGrid?.();
    await flush();

    const card = $('plugin-grid').querySelector('.card');
    const tile = card.querySelector('.app-icon');
    expect(tile, 'the card has no app icon').not.toBeNull();
    expect(tile.textContent, 'the monogram is not the plugin name\'s first character').toBe('对');
    expect(tile.getAttribute('aria-hidden')).toBe('true');
    const tileStyle = tile.getAttribute('style') || '';
    expect(tileStyle, 'the icon was left untinted').toMatch(/--tint-a:/);

    card.click();
    await flush();
    const hero = $('detail-avatar');
    expect(hero.textContent).toBe('对');
    expect(hero.getAttribute('style'), 'the hero tile does not match the card tile').toBe(tileStyle);
  });

  it('keeps the state on the card, as a dot, and the version beside the name', async () => {
    handlers.Installed = () => [installed()];
    await loadApp();
    await flush();
    document.getElementById('back-to-grid')?.click();
    await flush();

    const card = $('plugin-grid').querySelector('.card');
    // The dot: read at a glance, and the thing the old per-row 已启用 pill became.
    expect(card.querySelector('.app-dot').classList.contains('is-on')).toBe(true);
    expect(card.querySelector('.card-name').textContent).toBe('对话插件');
    expect(card.querySelector('.card-meta').textContent).toMatch(/1\.3\.0/);
    expect(card.querySelector('.card-meta').textContent).toMatch(/shixiansi/);
    // The state is stated ONCE in words, in the footer, and only because a dot cannot
    // say 可更新.
    expect(card.querySelectorAll('.state').length).toBe(1);
  });

  it('hides the whole enable switch, not just its checkbox', async () => {
    // THE BARE-CHECKBOX TRAP. The switch replaces the checkbox input with a track and a
    // thumb, so the input is 1x1 and transparent. Code that hid the INPUT to hide the
    // control would leave the track, the thumb and the word 启用 on screen.
    await loadApp();
    await flush();

    // A plugin that is only in the catalogue.
    $('plugin-grid').querySelector('.card').click();
    await flush();

    const row = $('detail-enabled-row');
    expect(row, 'the switch has no wrapper to hide').not.toBeNull();
    expect(hidden(row), 'the switch is visible for a plugin that is not installed').toBe(true);
    // …and the state classes are what CSS keys off, so the switch's own parts must live
    // INSIDE it - otherwise hiding the wrapper would hide nothing.
    expect(row.querySelector('input#detail-enabled')).not.toBeNull();
    expect(row.querySelector('.switch-track')).not.toBeNull();
    expect(row.querySelector('.switch-thumb')).not.toBeNull();

    // The gear beside it is hidden too: a gear that opens nothing is a dead control.
    expect(hidden($('detail-settings'))).toBe(true);
  });

  it('lays the settings form out as a grid, with long fields full width', async () => {
    handlers.Installed = () => [installed()];
    handlers.GetConfig = (id) => ({
      pluginId: id,
      name: '对话插件',
      dir: '',
      items: [
        { key: 'llmBaseUrl', label: '接口地址', type: 'text', value: 'https://api.deepseek.com/v1', hostWhitelist: true },
        { key: 'llmModel', label: '模型', type: 'text', value: 'deepseek-chat' },
        { key: 'llmApiKey', label: 'API Key', type: 'secret', secretSet: true },
        { key: 'tags', label: '分区', type: 'select', multiple: true, options: [
          { value: 'a', label: 'A' }, { value: 'b', label: 'B' },
        ] },
      ],
    });
    const app = await loadApp();
    await flush();

    const form = $('cfg-form');
    const style = declared(form);
    expect(style.display).toBe('grid');
    expect(style['grid-template-columns']).toMatch(/auto-fit/);
    // The whitelist item and the multi-select take the whole row; a plain text field
    // does not. The decision comes from the item's own shape, not from a key list.
    const fields = [...form.querySelectorAll('.field')];
    const wide = fields.filter((f) => f.classList.contains('wide')).map((f) => f.querySelector('input')?.dataset.key);
    expect(wide).toContain('llmBaseUrl');
    expect(wide).toContain('tags');
    expect(wide).not.toContain('llmModel');
    void app;
  });

  it('states how many permissions are granted, in the panel header', async () => {
    handlers.Installed = () => [installed({ Granted: ['llm.chat', 'storage'] })];
    await loadApp();
    await flush();
    // Two of five granted: the count is the one fact worth having without reading the
    // list, and it is the panel's, not a button's badge.
    expect($('perm-count').textContent).toBe('2 / 5 已授予');
  });
});

describe('detail surfaces', () => {
  it('opens with the settings dialog shut and the interface in the workspace', async () => {
    handlers.Installed = () => [installed()];
    await loadApp();
    await flush();

    expect(hidden($('settings-sheet'))).toBe(true);
    // The interface is not a modal and not a card in a column: it is the page's main
    // column, with one caption strip and one scrollbar inside it.
    expect(hidden($('panel-interface'))).toBe(false);
    expect($('panel-interface').querySelector('.workspace-bar')).not.toBeNull();
    expect(document.querySelector('.plugin-frame')).not.toBeNull();
    // No permission popover exists any more: the list is a column of the page.
    expect($('perm-popover')).toBeNull();
    expect($('detail-permissions')).toBeNull();
  });

  it('saves a permission the moment it is ticked', async () => {
    handlers.Installed = () => [installed({ Granted: ['llm.chat'] })];
    await loadApp();
    await flush();

    const box = $('perm-list').querySelector('input[data-perm="storage"]');
    expect(box).not.toBeNull();
    box.checked = true;
    box.dispatchEvent(new Event('change', { bubbles: true }));
    await flush();

    const grant = calls.filter((c) => c.name === 'Grant').pop();
    expect(grant, 'ticking a permission did not reach the backend').toBeTruthy();
    expect(grant.args[0]).toBe('com.shixiansi.plugin-chat');
    expect(grant.args[1]).toContain('storage');
  });

  it('opens the settings as a dialog, and only when the plugin declares settings', async () => {
    handlers.Installed = () => [
      installed(),
      installed({ ID: 'com.shixiansi.plugin-plain', Manifest: { ...installed().Manifest, id: 'com.shixiansi.plugin-plain', name: '无配置插件', config: [] } }),
    ];
    await loadApp();
    await flush();

    const gear = $('detail-settings');
    expect(hidden(gear), 'a plugin with settings has no gear').toBe(false);
    gear.click();
    await flush();
    expect(hidden($('settings-sheet'))).toBe(false);
    expect($('settings-title').textContent).toContain('设置');

    // 关闭 closes it without writing anything.
    $('settings-cancel').click();
    await flush();
    expect(hidden($('settings-sheet'))).toBe(true);

    // A plugin that declares no settings gets no gear: a gear that opens nothing is a
    // dead control.
    handlers.SelectedID = () => 'com.shixiansi.plugin-plain';
    const app = await loadApp();
    await flush();
    expect(hidden($('detail-settings'))).toBe(true);
    void app;
  });

  it('replaces the workspace with a sentence for a plugin that is not running', async () => {
    // Switching a plugin off has to stop it RUNNING, not just stop it being listed: an
    // interface left on screen would keep its document alive with its permissions.
    handlers.Installed = () => [installed({ Enabled: false })];
    await loadApp();
    await flush();

    expect(hidden($('panel-interface'))).toBe(true);
    expect(hidden($('workspace-empty'))).toBe(false);
    expect($('workspace-empty').textContent).toMatch(/停用/);
    expect(document.querySelector('.plugin-frame')).toBeNull();
  });

  it('keeps the settings dialog open across a re-render that is not the user\'s', async () => {
    handlers.Installed = () => [installed()];
    const app = await loadApp();
    await flush();

    $('detail-settings').click();
    await flush();
    expect(hidden($('settings-sheet'))).toBe(false);

    // A permission tick re-renders the page. The dialog is the user's, not the
    // render's, so it stays.
    const box = $('perm-list').querySelector('input[data-perm="storage"]');
    box.checked = false;
    box.dispatchEvent(new Event('change', { bubbles: true }));
    await flush();
    expect(hidden($('settings-sheet'))).toBe(false);
    void app;
  });

  it('closes the dialog when another plugin is selected', async () => {
    handlers.Installed = () => [
      installed(),
      installed({ ID: 'com.shixiansi.plugin-other', Manifest: { ...installed().Manifest, id: 'com.shixiansi.plugin-other', name: '另一个插件' } }),
    ];
    const app = await loadApp();
    await flush();

    $('detail-settings').click();
    await flush();
    expect(hidden($('settings-sheet'))).toBe(false);

    await app.select?.('com.shixiansi.plugin-other');
    await flush();
    expect(hidden($('settings-sheet'))).toBe(true);
  });

  it('puts an icon on every control in the action bar', async () => {
    handlers.Installed = () => [installed()];
    await loadApp();
    await flush();

    const bar = document.querySelector('.action-bar');
    expect(bar).not.toBeNull();
    const controls = [...bar.children].filter((el) => !hidden(el));
    expect(controls.length).toBeGreaterThanOrEqual(3);
    for (const el of controls) {
      expect(el.querySelector('svg.ico'), `${el.id} has no icon`).not.toBeNull();
    }
    // The destructive one is last, outlined, and the only coloured one.
    expect(bar.lastElementChild).toBe($('detail-uninstall'));
    expect(declared($('detail-uninstall')).background).toBe('transparent');
  });

  it('keeps the update button\'s icon when it writes the new version number', async () => {
    handlers.Installed = () => [installed({ Version: '1.2.0' })];
    handlers.Catalogue = () => ({ base: '', fallback: false, notice: '', entries: [entry({ version: '1.3.0' })] });
    await loadApp();
    await flush();

    const button = $('detail-update');
    expect(hidden(button)).toBe(false);
    expect(button.querySelector('svg.ico')).not.toBeNull();
    expect($('detail-update-label').textContent).toBe('更新到 1.3.0');
    expect(button.textContent).toContain('更新到 1.3.0');
  });

  it('goes back to the grid from a plugin page, keeping the selection', async () => {
    handlers.Installed = () => [installed()];
    const app = await loadApp();
    await flush();
    expect(hidden($('detail-view'))).toBe(false);

    $('back-to-grid').click();
    await flush();
    expect(hidden($('grid-view'))).toBe(false);
    expect(hidden($('detail-view'))).toBe(true);
    // The selected card is still marked: coming back to a plugin must not need finding
    // it again, and it is what the window reopens to.
    expect($('plugin-grid').querySelector('.card.is-selected')).not.toBeNull();
    expect(app.state.selected).toBe('com.shixiansi.plugin-chat');
  });
});

describe('header', () => {
  it('is a store header: mark, search and three window controls - no title, no address', async () => {
    await loadApp();
    await flush();

    const top = document.querySelector('.top');
    expect(top, 'the top bar is gone').not.toBeNull();
    // The window's own title bar says 「插件市场」; a second copy was removed in round 5
    // and has not come back.
    expect(top.querySelector('h1'), 'the redundant page title is back').toBeNull();
    // …and the mirror URL is a setting now, not a strip across the top of the window.
    // The ONE field up here is the search box; there is no second text input.
    expect(top.querySelector('#market-url')).toBeNull();
    const fields = [...top.querySelectorAll('input')].map((el) => el.id);
    expect(fields).toEqual(['search']);
    expect($('market-url'), 'the address field is gone entirely').not.toBeNull();
    expect($('market-url').closest('#market-sheet')).not.toBeNull();

    // The order is what the eye reads: the mark, then the search.
    const children = [...top.children].map((el) => el.className || el.id);
    expect(children[0]).toBe('brand-mark');
    expect(children[1]).toBe('search');
    expect(declared(top)['flex-wrap']).not.toBe('wrap');
    expect(declared(top).display).toBe('flex');

    // The search box is the one field, and it is a label wrapping its input: the whole
    // box focuses it, which is the one place a wrapping label is right.
    const search = top.querySelector('.search');
    expect(search.querySelector('#search')).not.toBeNull();
    expect(declared(search).display).toBe('flex');
  });

  it('keeps the market address in the settings sheet, and saves from it', async () => {
    await loadApp();
    await flush();

    expect(hidden($('market-sheet'))).toBe(true);
    $('market-settings').click();
    await flush();
    expect(hidden($('market-sheet'))).toBe(false);
    // Pre-filled with the address in force, so the field is a confirmation as much as
    // an editor.
    expect($('market-url').value).toBe('https://example.invalid/plugins');
    expect($('market-settings').getAttribute('aria-expanded')).toBe('true');

    $('market-url').value = 'https://mirror.invalid/x';
    $('market-save').click();
    await flush();

    const save = calls.filter((c) => c.name === 'SaveMarketURL').pop();
    expect(save.args).toEqual(['https://mirror.invalid/x']);
    expect(hidden($('market-sheet')), 'the sheet stayed open after a successful save').toBe(true);
  });
});

describe('theme', () => {
  it('opens in the scheme the settings file names, and the switch promises the other one', async () => {
    // Dark is what a settings file written before themes existed means, so a fresh
    // install and an old one both open the same way.
    let app = await loadApp();
    await flush();
    expect(document.documentElement.dataset.theme).toBe('dark');
    const use = () => document.querySelector('#theme-toggle use').getAttribute('href');
    expect(use(), 'the switch does not offer light mode').toBe('#ico-moon');
    expect($('theme-toggle').getAttribute('aria-label')).toBe('切换到浅色主题');

    // A remembered light scheme is applied on boot, from the FILE (one binding, asked
    // for before anything else is fetched).
    handlers.Theme = () => 'light';
    app = await loadApp();
    await flush();
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(use(), 'the switch does not offer dark mode').toBe('#ico-sun');
    expect($('theme-toggle').getAttribute('aria-label')).toBe('切换到深色主题');

    // An unknown value is dark, not a third scheme: the stylesheet only defines two.
    handlers.Theme = () => 'purple';
    const third = await loadApp();
    await flush();
    expect(document.documentElement.dataset.theme).toBe('dark');
    // And the export agrees with the DOM, so the two cannot drift.
    expect(third.state.theme).toBe('dark');
  });

  it('switches on click, before the preference is written', async () => {
    handlers.Installed = () => [installed()];
    const app = await loadApp();
    await flush();
    const button = $('theme-toggle');

    // The look changes synchronously: a saved preference that needs a round trip to
    // appear reads as a dead button.
    button.click();
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(document.querySelector('#theme-toggle use').getAttribute('href')).toBe('#ico-sun');
    await flush();

    // ...and it is remembered through this program's own settings file.
    const sets = calls.filter((c) => c.name === 'SetTheme');
    expect(sets.length, 'the choice was not written').toBe(1);
    expect(sets[0].args).toEqual(['light']);

    button.click();
    await flush();
    const after = calls.filter((c) => c.name === 'SetTheme');
    expect(after[after.length - 1].args).toEqual(['dark']);
    void app;
  });

  it('paints every colour from tokens, so a scheme is one attribute', async () => {
    // The light theme is a second block of custom properties, not a second
    // stylesheet: if a rule hard-codes a colour, that element keeps its dark values
    // in light mode and the scheme stops being one attribute.
    expect(css).toMatch(/:root\s*\{[\s\S]*?--bg:/);
    expect(css, 'there is no light palette').toMatch(/:root\[data-theme="light"\]\s*\{/);
    // Both palettes define the same COLOUR names, or a token silently falls back to
    // the dark value in light mode. (The radius/font tokens are shared, so the light
    // block does not repeat them.)
    const shared = new Set(['--radius', '--radius-sm', '--font', '--mono']);
    const tokensOf = (block) =>
      new Set([...block.matchAll(/(--[a-z-]+):/g)].map((m) => m[1]));
    const darkBlock = css.slice(css.indexOf(':root {'), css.indexOf(':root[data-theme="light"]'));
    const lightBlock = css.slice(css.indexOf(':root[data-theme="light"]'));
    const dark = new Set([...tokensOf(darkBlock)].filter((t) => !shared.has(t)));
    const light = tokensOf(lightBlock);
    expect([...dark].filter((t) => !light.has(t)), 'a colour token is missing from the light palette')
      .toEqual([]);
    expect([...light].filter((t) => !dark.has(t)), 'the light palette invents a token')
      .toEqual([]);

    // A token that DEFINES ITSELF (`--ok-fg: var(--ok-fg)`) is the failure mode a
    // mechanical search-and-replace produces: it still reads as "defined", but at
    // computed-value time the substitution is invalid, so every `var()` that reads it
    // silently falls back to an inherited colour.
    for (const [name, block] of [['dark', darkBlock], ['light', lightBlock]]) {
      const selfRef = [...block.matchAll(/(--[a-z-]+):\s*var\((--[a-z-]+)\)/g)]
        .filter((m) => m[1] === m[2])
        .map((m) => m[1]);
      expect(selfRef, `a ${name} palette token defines itself`).toEqual([]);
    }

    // The selected card's NAME colour is part of the palette too. It was a white
    // literal, which the dark scheme needs and the light scheme cannot have: white on
    // a pale blue card is invisible.
    expect(css).toMatch(/\.card\.is-selected \.card-name\s*\{[^}]*color:\s*var\(--row-sel-text\)/);
    const lightSelText = (/--row-sel-text:\s*([^;]+);/.exec(lightBlock) || [])[1] || '';
    expect(lightSelText.trim(), 'the selected card name is white on the light scheme')
      .not.toMatch(/^#(fff|ffffff)$/i);
    expect(lightSelText.trim().length, 'the light palette does not define the selected card name colour')
      .toBeGreaterThan(0);
  });
});

describe('catalogue', () => {
  it('renders the catalogue and marks what is already installed', async () => {
    handlers.Installed = () => [installed()];
    handlers.Catalogue = () => ({
      base: 'https://example.invalid/plugins',
      fallback: false,
      notice: '',
      entries: [entry({ installed: true, installedVersion: '1.3.0' })],
    });
    const app = await loadApp();
    await flush();
    app.backToGrid();
    await flush();

    expect($('plugin-grid').querySelectorAll('.card')).toHaveLength(1);
    // Installed and up to date: the dot says so, and the footer's verb is 打开.
    expect($('plugin-grid').querySelector('.app-dot').classList.contains('is-on')).toBe(true);
    expect($('plugin-grid').querySelector('button[data-verb="open"]')).not.toBeNull();
    expect($('count-all').textContent).toBe('1');
    expect($('count-installed').textContent).toBe('1');
    expect($('count-update').textContent).toBe('0');
  });

  it('offers an update when the catalogue is ahead, and a chip counts it', async () => {
    handlers.Installed = () => [installed({ Version: '1.2.0' })];
    handlers.Catalogue = () => ({ base: '', fallback: false, notice: '', entries: [entry({ version: '1.3.0' })] });
    const app = await loadApp();
    await flush();
    app.backToGrid();
    await flush();

    expect($('plugin-grid').querySelector('.app-dot').classList.contains('is-update')).toBe(true);
    expect($('plugin-grid').querySelector('.state').textContent).toMatch(/可更新 1\.2\.0 → 1\.3\.0/);
    expect($('plugin-grid').querySelector('button[data-verb="update"]')).not.toBeNull();
    expect($('count-update').textContent).toBe('1');
  });

  it('refetches the catalogue on refresh and repaints from the response', async () => {
    const app = await loadApp();
    await flush();
    expect($('plugin-grid').querySelectorAll('.card')).toHaveLength(1);

    handlers.Catalogue = () => ({
      base: '',
      fallback: false,
      notice: '',
      entries: [entry(), entry({ id: 'com.shixiansi.plugin-qq', name: 'QQ 群聊插件', version: '1.12.0' })],
    });
    $('market-refresh').click();
    await flush();

    expect($('plugin-grid').querySelectorAll('.card')).toHaveLength(2);
    void app;
  });

  it('shows the reason a fetch failed without emptying the list already on screen', async () => {
    handlers.Catalogue = () => ({
      base: '',
      fallback: false,
      notice: '',
      entries: [entry(), entry({ id: 'com.shixiansi.plugin-qq', name: 'QQ 群聊插件', version: '1.12.0' })],
    });
    await loadApp();
    await flush();
    expect($('plugin-grid').querySelectorAll('.card')).toHaveLength(2);

    handlers.Catalogue = () => {
      throw new Error('网络不可达');
    };
    $('market-refresh').click();
    await flush();

    expect($('plugin-grid').querySelectorAll('.card'), 'the list was emptied by a failed refresh')
      .toHaveLength(2);
    expect(hidden($('notice'))).toBe(false);
    expect($('notice').textContent).toMatch(/网络不可达/);
  });

  it('states a fallback to the built-in source without treating it as an error', async () => {
    handlers.Catalogue = () => ({
      base: '',
      fallback: true,
      notice: '',
      entries: [entry()],
    });
    await loadApp();
    await flush();

    expect(hidden($('notice'))).toBe(false);
    expect($('notice').textContent).toMatch(/内置示例源/);
    // The provenance is a pill in the header, not an error banner.
    expect($('headline').textContent).toBe('内置示例源');
    expect($('plugin-grid').querySelectorAll('.card')).toHaveLength(1);
  });
});

describe('settings editor', () => {
  it('never puts a stored secret back into the page', async () => {
    handlers.Installed = () => [installed()];
    handlers.GetConfig = (id) => ({
      pluginId: id,
      name: '对话插件',
      dir: '',
      items: [{ key: 'llmApiKey', label: 'API Key', type: 'secret', secretSet: true, value: '' }],
    });
    await loadApp();
    await flush();
    $('detail-settings').click();
    await flush();

    const input = $('cfg-form').querySelector('input[data-key="llmApiKey"]');
    expect(input).not.toBeNull();
    expect(input.value, 'a stored secret is in the page').toBe('');
    expect($('cfg-form').querySelector('.field-note').textContent).toMatch(/已保存/);
  });

  it('draws a picker for a select item, and reads it back as the value the plugin parses', async () => {
    handlers.Installed = () => [installed()];
    handlers.GetConfig = (id) => ({
      pluginId: id,
      name: '对话插件',
      dir: '',
      items: [{
        key: 'categories',
        label: '分区',
        type: 'select',
        multiple: true,
        value: 'a',
        options: [{ value: 'a', label: 'A' }, { value: 'b', label: 'B' }],
      }],
    });
    const app = await loadApp();
    await flush();
    $('detail-settings').click();
    await flush();

    const boxes = [...$('cfg-form').querySelectorAll('input[data-key="categories"][type="checkbox"]')];
    expect(boxes).toHaveLength(2);
    expect(boxes[0].checked).toBe(true);

    // Ticking one more and saving sends the pair, comma-joined: that is the shape the
    // plugin parses.
    boxes[1].checked = true;
    const values = app.collectConfig();
    expect(values.categories).toBe('a,b');
  });
});

describe('permission vocabulary', () => {
  it('names every permission the backend can grant, including audio', async () => {
    const app = await loadApp();
    await flush();
    // The window must not say `audio` is unknown: the table here and the Go table are
    // compared by .dev/check-frontend.cjs, and a missing entry shows up as a permission
    // row with no explanation.
    expect(app.PERMISSION_LABELS.audio).toBeTruthy();
    expect(app.PERMISSION_LABELS['llm.chat']).toBeTruthy();
    expect(app.PERMISSION_LABELS['net.fetch']).toBeTruthy();
  });
});
