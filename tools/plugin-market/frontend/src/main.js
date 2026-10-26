import './style.css';

/**
 * The plugin market's front end.
 *
 * Everything this page does is a projection of two Go lists - installed plugins
 * and the catalogue - plus the editor for one plugin's declared settings. It
 * holds no plugin logic: it does not decide what a package may contain, whether
 * a permission is legal, or what installing means. Those answers live in
 * internal/plugins and are shared with the pet, so the two programs cannot
 * disagree about what is installed.
 *
 * Three layout facts are load-bearing and are asserted in main.test.js:
 *
 *  1. #grid-view and #detail-view are SIBLINGS and exactly ONE is in the flow at a
 *     time. The window is a storefront: a grid of cards, and one plugin's page. It
 *     used to be a two-pane master-detail, which put a plugin's own runtime page on
 *     the main stage of a MARKET and left browsing as a 310px sidebar.
 *  2. A permission row is one full-width wrapping container. The old tab clipped
 *     long explanations at the panel's right edge; here they wrap.
 *  3. A card is ONE plugin, built from the catalogue AND the installed list (see
 *     gridEntries): a plugin that is installed but missing from the catalogue still
 *     appears, because it is still installed.
 *
 * The API key is write-only on this side: the backend never sends plaintext, so
 * there is nothing to backfill and the field is cleared after a save.
 *
 * WHAT THIS FILE OWNS ABOUT THE TWO VIEWS
 *
 * `state.view` ('grid' | 'detail') is a GLANCE, not a preference: it is not persisted.
 * Which plugin you were last on IS persisted (Market.RememberSelection), and it is
 * what the window reopens to, because this program also hosts the plugin's own
 * workspace - making that take an extra click every launch would be a worse trade than
 * the grid being one click away behind 「插件市场」.
 *
 * The permission list and the settings form used to be two floating surfaces (a
 * popover and a modal) because they had no room in a two-pane layout. On a page they
 * have a column and a dialog's worth of room, so:
 *
 *   - 权限 is the detail page's right rail: always visible, ticked in place, its count
 *     in the panel header. No popover, no positioning, nothing to dismiss.
 *   - 设置 is a DIALOG (the same .sheet as the install sheet): a settings form is a
 *     detour - change these, save, get back to work - and a detour covers the page
 *     instead of being stacked under it. The gear only exists when the plugin declares
 *     settings at all.
 *   - The plugin's own interface is the page's main column, in a .workspace box with a
 *     caption strip: one box, one scrollbar, and a visible boundary between the
 *     market's chrome and a document the market does not style.
 *
 * The settings dialog's open state is deliberately NOT persisted: it is a glance, and
 * it is reset whenever the selection changes so a new plugin never opens on a form
 * that belongs to the old one.
 */

import * as Market from '../wailsjs/go/main/Market';
import { mountPluginUI, clearPluginUI, setFrameTheme } from './plugin-ui';

/**
 * The permission vocabulary, with the explanation the user is consenting to.
 *
 * This mirrors internal/plugins.PermissionLabels, which is the authority: the Go
 * side decides what a permission means and this map is only how it reads. The
 * duplication is checked rather than trusted - a permission the backend allows but
 * this map omits renders as a bare name like "audio", which is exactly the kind of
 * half-explained consent the wording exists to prevent.
 */
const PERMISSION_LABELS = {
  'todos.read': '读取待办列表',
  'todos.write': '新增、修改、完成与删除待办',
  'pet.bubble': '让桌宠弹出一句话的气泡',
  'timer.read': '读取番茄钟阶段与剩余时间',
  events: '订阅待办与番茄钟的变化事件（关闭界面后也会送达）',
  'net.fetch': '访问网络（仅限插件声明的域名）',
  'net.credentials': '以你保存的网站登录凭据发请求（凭据本身不会显示给插件）',
  storage: '在自己的私有目录里保存数据',
  'pet.surface': '在桌宠旁显示自定义浮层面',
  'llm.chat': '调用你已配置的大模型（流式返回）',
  'plugin.config': '保存你自己的插件设置',
  audio: '保存并播放音频（例如语音提醒）',
  'net.ws': '保持一条到插件所声明地址的 WebSocket 长连接（收发消息）',
  'llm.borrow': '使用其它插件已配置的大模型与密钥（密钥不会显示给你）',
  process: '启动本机程序（只会启动你在插件设置里填了绝对路径的 .exe）',
  'plugin.share': '与其它插件交换消息（只能读清单里点名的键，只能写自己名下的键）',
};

/**
 * A permission this map does not explain is a HALF-EXPLAINED consent, so the two
 * tables are kept in step by a gate rather than by discipline: the market is a
 * separate program with its own bundle and cannot import the Go table, so
 * .dev/check-frontend.cjs compares the keys here against
 * internal/plugins.PermissionLabels and fails the build when they differ.
 */

/** Renders a permission name with its explanation, falling back to the name. */
function permissionLabel(name) {
  return PERMISSION_LABELS[name] || '';
}

const els = {
  // The colour-scheme switch. Icon-only, and the glyph is the DESTINATION (a moon
  // while the window is dark, a sun while it is light) - see applyTheme.
  themeToggle: document.getElementById('theme-toggle'),
  headline: document.getElementById('headline'),
  search: document.getElementById('search'),
  marketRefresh: document.getElementById('market-refresh'),
  notice: document.getElementById('notice'),

  // The market's own setting, behind the gear. The address used to BE the top bar;
  // it is a field in a sheet now (see the note in index.html).
  marketSettings: document.getElementById('market-settings'),
  marketSheet: document.getElementById('market-sheet'),
  marketUrl: document.getElementById('market-url'),
  marketSave: document.getElementById('market-save'),
  marketCancel: document.getElementById('market-cancel'),
  marketNote: document.getElementById('market-note'),

  // ==== view 1: the catalogue grid, behind four filter chips ================
  gridView: document.getElementById('grid-view'),
  grid: document.getElementById('plugin-grid'),
  gridEmpty: document.getElementById('grid-empty'),
  filterAll: document.getElementById('filter-all'),
  filterInstalled: document.getElementById('filter-installed'),
  filterUpdate: document.getElementById('filter-update'),
  filterMarket: document.getElementById('filter-market'),
  countAll: document.getElementById('count-all'),
  countInstalled: document.getElementById('count-installed'),
  countUpdate: document.getElementById('count-update'),
  countMarket: document.getElementById('count-market'),

  // ==== view 2: one plugin ==================================================
  detailView: document.getElementById('detail-view'),
  backToGrid: document.getElementById('back-to-grid'),
  crumbName: document.getElementById('crumb-name'),
  // The monogram beside the name. Static in index.html, painted in place on every
  // render: see paintAvatar.
  detailAvatar: document.getElementById('detail-avatar'),
  detailName: document.getElementById('detail-name'),
  detailTagline: document.getElementById('detail-tagline'),
  detailVersion: document.getElementById('detail-version'),
  detailAuthor: document.getElementById('detail-author'),
  detailPermsBadge: document.getElementById('detail-perms-badge'),
  detailError: document.getElementById('detail-error'),
  detailFacts: document.getElementById('detail-facts'),
  detailInstall: document.getElementById('detail-install'),
  detailUpdate: document.getElementById('detail-update'),
  // The update button's TEXT lives in a span of its own: the button also carries
  // an icon, and setting textContent on the button would delete the icon the
  // moment the version numbers changed.
  detailUpdateLabel: document.getElementById('detail-update-label'),
  detailUninstall: document.getElementById('detail-uninstall'),
  detailSettings: document.getElementById('detail-settings'),
  // The label, not the checkbox: hiding only the input would leave the switch
  // track and the word 启用 on screen for a plugin that is not installed.
  detailEnabledRow: document.getElementById('detail-enabled-row'),
  detailEnabled: document.getElementById('detail-enabled'),

  // The permission COLUMN (right rail). It used to be a popover pinned to a button;
  // on a page it is simply always there - see the note in index.html.
  permList: document.getElementById('perm-list'),
  permCount: document.getElementById('perm-count'),
  permEmpty: document.getElementById('perm-empty'),

  // The plugin's own interface, hosted in this window. See plugin-ui.js.
  panelInterface: document.getElementById('panel-interface'),
  workspaceEmpty: document.getElementById('workspace-empty'),
  pluginUI: document.getElementById('plugin-ui'),
  pluginUIFrame: document.getElementById('plugin-ui-frame'),

  // The settings dialog, built on the same .sheet as the install sheet.
  settingsSheet: document.getElementById('settings-sheet'),
  settingsTitle: document.getElementById('settings-title'),
  settingsCancel: document.getElementById('settings-cancel'),
  cfgEmpty: document.getElementById('cfg-empty'),
  cfgForm: document.getElementById('cfg-form'),
  cfgSave: document.getElementById('cfg-save'),
  cfgNote: document.getElementById('cfg-note'),

  sheet: document.getElementById('install-sheet'),
  sheetTitle: document.getElementById('sheet-title'),
  sheetDesc: document.getElementById('sheet-desc'),
  sheetPerms: document.getElementById('sheet-perms'),
  sheetStatus: document.getElementById('sheet-status'),
  sheetCancel: document.getElementById('sheet-cancel'),
  sheetConfirm: document.getElementById('sheet-confirm'),

  toast: document.getElementById('toast'),
};

/** Everything the page knows. Replaced wholesale, never patched field by field. */
const state = {
  installed: [],
  warnings: [],
  // The last catalogue that arrived. Kept across a failed refresh on purpose:
  // a network hiccup must not empty a list the user was reading (T18b).
  catalogue: { base: '', fallback: false, notice: '', entries: [] },
  // The id the detail view is showing, whether it is installed or only in the
  // catalogue.
  selected: '',
  config: null,
  busy: false,
  pending: null,
  // Which view is on screen: 'grid' or 'detail'. `filter` is which chip is applied
  // to the grid ('all' | 'installed' | 'update' | 'market'), and `query` is the text
  // in the search box. None of the three is persisted: what you were looking at is a
  // glance, and the settings file is for things worth remembering across runs (the
  // address, the last selected plugin).
  view: 'grid',
  filter: 'all',
  query: '',
  // Whether the plugin's settings dialog is open. It starts CLOSED - a settings form
  // is a detour, not a state. Reset when the selection changes; see select().
  cfgOpen: false,
  // The colour scheme in force: 'dark' or 'light'. Read from this program's own
  // settings file on boot and written back on every switch - it is a look the user
  // chose, so it has to survive a restart.
  theme: 'dark',
};

// --- the colour scheme ------------------------------------------------------

/**
 * Applies a colour scheme and repaints the switch.
 *
 * The theme is one attribute on <html> and nothing else: every colour in the
 * stylesheet is a custom property, and `:root[data-theme="light"]` redefines them,
 * so no rule has to know which theme is running (see style.css).
 *
 * The button shows the DESTINATION rather than the current state - a moon while the
 * window is dark, a sun while it is light - because that is what a click does, and
 * an icon-only control with no text has to promise an outcome. `aria-label` names
 * the same thing, and `data-theme` is the single source of truth for the rest of
 * the page (and for the tests).
 */
function applyTheme(name) {
  const theme = name === 'light' ? 'light' : 'dark';
  state.theme = theme;
  document.documentElement.dataset.theme = theme;

  const use = els.themeToggle?.querySelector('use');
  if (use) use.setAttribute('href', theme === 'light' ? '#ico-sun' : '#ico-moon');
  const action = theme === 'light' ? '切换到深色主题' : '切换到浅色主题';
  els.themeToggle?.setAttribute('aria-label', action);
  els.themeToggle?.setAttribute('title', action);

  // The plugin's hosted document is a FOREIGN document in a sandboxed frame: it
  // cannot see this page's custom properties, so it is told which scheme to paint.
  // Mounted frames are updated in place (no remount - that would throw away a
  // half-typed form); the next mount reads the attribute itself.
  setFrameTheme(theme);
  return theme;
}

/** Switches the scheme, then remembers it. The look changes first: a saved
 * preference that takes a round trip to appear reads as a dead button. */
async function toggleTheme() {
  const next = applyTheme(state.theme === 'light' ? 'dark' : 'light');
  try {
    await Market.SetTheme(next);
  } catch (err) {
    // The window stays in the scheme the user just picked for this session; only
    // the remembering failed, and saying so is better than silently reverting.
    toast(`主题已切换，但没能记住：${err?.message || err}`, true);
  }
}

// --- the two views ----------------------------------------------------------

/**
 * Which of the two views is on screen.
 *
 * They are siblings in <body> and exactly one is in the flow at a time, so there is
 * never a second scrollbar and never two things claiming to be "the page". Both stay
 * in the DOM and both keep being rendered: coming back to the grid must not need a
 * round trip, and the detail page's data is what the grid's cards were built from.
 *
 * The view is NOT persisted: which page you were on is a glance, while the plugin you
 * were last on (state.selected) is remembered in the settings file and is what the
 * window opens to.
 */
function goGrid() {
  state.view = 'grid';
  show(els.gridView, true);
  show(els.detailView, false);
  // The grid may have changed while the detail page was open (an install, an
  // uninstall, a catalogue refresh), so the counts are repainted on arrival.
  renderChips();
}

function goDetail() {
  state.view = 'detail';
  show(els.gridView, false);
  show(els.detailView, true);
}

/**
 * Applies a filter chip.
 *
 * The chips are not tabs over two lists: every plugin appears ONCE, and the chip
 * narrows that one list. `aria-pressed` follows the class, or a screen reader
 * announces a filter that is not the one in force.
 */
function setFilter(name) {
  state.filter = ['installed', 'update', 'market'].includes(name) ? name : 'all';
  for (const [el, key] of [
    [els.filterAll, 'all'],
    [els.filterInstalled, 'installed'],
    [els.filterUpdate, 'update'],
    [els.filterMarket, 'market'],
  ]) {
    const on = state.filter === key;
    el?.classList.toggle('is-on', on);
    el?.setAttribute('aria-pressed', String(on));
  }
  renderGrid();
}

// --- small helpers ----------------------------------------------------------

function show(el, on = true) {
  el?.classList.toggle('hidden', !on);
}

function text(el, value) {
  if (el) el.textContent = value == null ? '' : String(value);
}

let toastTimer = 0;
function toast(message, isError = false) {
  if (!els.toast) return;
  els.toast.textContent = message;
  els.toast.classList.toggle('error', isError);
  show(els.toast, true);
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => show(els.toast, false), isError ? 6000 : 2600);
}

function build(tag, className, content) {
  const el = document.createElement(tag);
  if (className) el.className = className;
  if (content != null) el.textContent = content;
  return el;
}

// NOTE ON THE ICONS: every glyph in this window is a static <svg><use href="#ico-…">
// in index.html. There is deliberately no `icon(name)` helper here any more - the
// only thing that needed one was the tab strip, which built its buttons per plugin,
// and the strip is gone. An icon created from script would be a second way to draw
// the same thing.

// --- plugin monograms -------------------------------------------------------

/**
 * A stable tint for one plugin id, as a hue in [0, 360).
 *
 * Hashed from the id rather than looked up in a table, for two reasons: a plugin
 * nobody has seen before still gets a colour of its own, and the SAME plugin
 * gets the same colour in the installed list, in the catalogue and in the detail
 * header without any of the three knowing about the other two. A table would
 * have to be kept in step with the market, and would silently give every new
 * plugin the same grey.
 */
function avatarTint(id) {
  let hue = 0;
  for (const ch of String(id || '')) hue = (hue * 31 + ch.codePointAt(0)) % 360;
  return hue;
}

/** The character a plugin's monogram shows: the first of its name, or of its id. */
function avatarInitial(name, id) {
  const label = String(name || id || '').trim();
  const first = [...label][0];
  return first ? first.toUpperCase() : '?';
}

/**
 * Paints a monogram tile IN PLACE.
 *
 * In place rather than by replacing the element, because `#detail-avatar` is a
 * static node from index.html: swapping it would leave `els.detailAvatar`
 * pointing at a node that is no longer in the document, and every later render
 * would paint a tile nobody can see.
 *
 * The hue reaches the stylesheet as two custom properties, not as a finished
 * `background`: the gradient itself belongs to the stylesheet, so changing the
 * palette stays one CSS edit. It is also the only form the jsdom tests can read
 * back - cssstyle parses `background` as a colour and silently DROPS a gradient,
 * which would make a test unable to tell a tinted tile from a plain one.
 *
 * `[...label][0]` is deliberate: a plugin named with an emoji must not be cut in
 * the middle of a surrogate pair.
 */
function paintAvatar(el, name, id) {
  if (!el) return;
  const hue = avatarTint(id);
  el.textContent = avatarInitial(name, id);
  el.style.setProperty('--tint-a', `hsl(${hue}, 68%, 58%)`);
  el.style.setProperty('--tint-b', `hsl(${(hue + 42) % 360}, 62%, 46%)`);
}

/* (`avatarTile` - a monogram built as a detached tile for a list row - is gone: the
 * card builds its icon in place because it also has to hang the state dot on it, and
 * the detail hero's tile is a static node from index.html. `paintAvatar` is the one
 * painter both use, which is what keeps the tint identical between them.) */

/**
 * Renders one permission as a full-width row.
 *
 * The checkbox, the name and the explanation share ONE container that wraps, and
 * nothing here sets a pixel width: a long Chinese explanation becomes a second
 * line rather than disappearing past the pane's right edge.
 */
function permissionRow(name, { checked = null, note = '' } = {}) {
  const row = build('li', 'perm');

  if (checked !== null) {
    const box = document.createElement('input');
    box.type = 'checkbox';
    box.checked = checked;
    box.dataset.perm = name;
    row.appendChild(box);
  }

  const body = build('div', 'perm-text');
  body.appendChild(build('span', 'perm-name', name));
  const label = permissionLabel(name);
  if (label) body.appendChild(build('span', 'perm-label', label));
  if (note) body.appendChild(build('span', 'perm-note', note));
  row.appendChild(body);

  return row;
}

/** The installed row for one id, or null. */
function installedRow(id) {
  return state.installed.find((p) => p.ID === id) || null;
}

/* (`markRow` and the row's left state stripe are gone with the rows: a card carries
 * its state as a dot on the app icon - see app-dot in style.css - and a "scan the left
 * edge of the list" affordance needs a list, which the grid is not.) */

/** The catalogue entry for one id, or null. */
function catalogueRow(id) {
  return state.catalogue.entries.find((e) => e.id === id) || null;
}

function renderInstalled() {
  // Kept as a name with its own job: the INSTALLED list is no longer a view, it is
  // the source the grid reads its state from. Every function that changes what is
  // installed ends with `refreshInstalled()`, and the grid then has to be repainted
  // because a card's dot, its verb and its update pill all come from this list.
  renderGrid();
}

/**
 * Every plugin the grid can show, one entry each.
 *
 * The catalogue is the source of ORDER and of what the market OFFERS; the installed
 * list is the source of what is ACTUALLY here, including a plugin the catalogue does
 * not know about (an older mirror, a local install, a package pulled from a directory
 * by hand). That second half is why this is a merge rather than a map over the
 * catalogue: a plugin that is installed but missing from the catalogue used to live in
 * a separate 已安装 list, and dropping it here would have made it invisible.
 *
 * The installed flag is read from the installed list rather than from the catalogue
 * row's own field, because the two can disagree for a moment: the catalogue was
 * fetched a moment ago and the pet (or a second market window) may have installed
 * something since.
 */
function gridEntries() {
  const out = [];
  const seen = new Set();

  for (const entry of state.catalogue.entries || []) {
    const inst = installedRow(entry.id);
    const shownVersion = inst ? inst.Version : entry.installed ? entry.installedVersion : '';
    const upToDate = inst || entry.installed ? !isNewer(entry.version, shownVersion) : false;
    out.push({
      id: entry.id,
      entry,
      inst,
      shownVersion,
      updatable: Boolean(inst || entry.installed) && !upToDate,
    });
    seen.add(entry.id);
  }

  for (const inst of state.installed) {
    if (seen.has(inst.ID)) continue;
    out.push({ id: inst.ID, entry: null, inst, shownVersion: inst.Version, updatable: false });
  }

  return out;
}

/** Whether a row survives the search box: name, id, author, description, permissions. */
function matchesQuery(row, query) {
  const q = String(query || '').trim().toLowerCase();
  if (!q) return true;
  const name = row.inst?.Manifest?.name || row.entry?.name || row.id;
  const author = row.inst?.Manifest?.author || row.entry?.author || '';
  const desc = row.inst?.Manifest?.description || row.entry?.description || '';
  const perms = (row.entry?.permissions || row.inst?.Manifest?.permissions || []).join(' ');
  return `${name} ${row.id} ${author} ${desc} ${perms}`.toLowerCase().includes(q);
}

/** Whether a row survives the filter chip. */
function matchesFilter(row) {
  switch (state.filter) {
    case 'installed':
      return Boolean(row.inst);
    case 'update':
      return Boolean(row.updatable);
    case 'market':
      return !row.inst;
    default:
      return true;
  }
}

/** The chip counts. Counted over the WHOLE catalogue, never over the filtered view:
 * a chip that says 0 because another chip is applied would be a lie. */
function renderChips() {
  const rows = gridEntries();
  text(els.countAll, String(rows.length));
  text(els.countInstalled, String(rows.filter((r) => r.inst).length));
  text(els.countUpdate, String(rows.filter((r) => r.updatable).length));
  text(els.countMarket, String(rows.filter((r) => !r.inst).length));
}

/**
 * One card: an app icon with its state dot, the name, version·author, a two-line
 * pitch, and a footer with the state in words plus the verb that fits it.
 *
 * The verb is the one thing a card can DO that the card itself cannot: the card
 * navigates to the plugin's page, and the button installs, updates or opens it.
 */
function cardFor(row) {
  const inst = row.inst;
  const man = inst?.Manifest || null;
  const name = man?.name || row.entry?.name || row.id;
  const author = man?.author || row.entry?.author || '';
  const version = inst?.Version || row.entry?.version || '';
  const desc = man?.description || row.entry?.description || '';

  const card = build('li', 'card');
  card.dataset.id = row.id;
  card.tabIndex = 0;
  card.setAttribute('role', 'button');
  if (row.id === state.selected) card.classList.add('is-selected');
  card.title = desc || name;

  const head = build('div', 'card-head');
  const icon = build('span', 'app-icon');
  icon.setAttribute('aria-hidden', 'true');
  paintAvatar(icon, name, row.id);
  // The state as a DOT on the icon. A pill under every row said 已启用 four times in
  // a four-plugin list; the dot is read at a glance and costs no line.
  const dot = build('span', 'app-dot');
  if (inst?.Error) dot.classList.add('is-err');
  else if (row.updatable) dot.classList.add('is-update');
  else if (inst) dot.classList.add(inst.Enabled ? 'is-on' : 'is-off');
  icon.appendChild(dot);
  head.appendChild(icon);

  const title = build('div', 'card-title');
  title.appendChild(build('h3', 'card-name', name));
  title.appendChild(
    build('div', 'card-meta', [version, author].filter(Boolean).join(' · ')),
  );
  head.appendChild(title);
  card.appendChild(head);

  card.appendChild(build('p', 'card-desc', desc));

  const foot = build('div', 'card-foot');
  // NOT called `state`: that name is the page's whole store, and a local of the same
  // name puts every earlier read of it in this function's temporal dead zone - which
  // fails at runtime as "cannot access 'state' before initialization", not as a lint
  // error.
  const stateTag = build('span', 'state');
  if (inst?.Error) {
    stateTag.classList.add('is-err');
    text(stateTag, 'manifest 读取失败');
  } else if (row.updatable) {
    stateTag.classList.add('is-update');
    text(stateTag, `可更新 ${row.shownVersion} → ${row.entry.version}`);
  } else if (inst) {
    stateTag.classList.add(inst.Enabled ? 'is-on' : 'is-off');
    text(stateTag, inst.Enabled ? '已启用' : '已停用');
  } else {
    text(stateTag, '未安装');
  }
  foot.appendChild(stateTag);

  const verb = build('button', 'small');
  verb.type = 'button';
  verb.dataset.verb = row.updatable ? 'update' : inst ? 'open' : 'install';
  if (!inst || row.updatable) verb.classList.add('primary');
  text(verb, row.updatable ? '更新' : inst ? '打开' : '安装');
  foot.appendChild(verb);
  card.appendChild(foot);

  return card;
}

/** Paints the grid and its counts. */
function renderGrid() {
  renderChips();
  if (!els.grid) return;
  els.grid.replaceChildren();

  const rows = gridEntries().filter((r) => matchesFilter(r) && matchesQuery(r, state.query));
  for (const row of rows) els.grid.appendChild(cardFor(row));

  // The empty state says WHY it is empty, because the three reasons are three
  // different next steps: nothing published, nothing matching a search, nothing in
  // this filter.
  let message = '';
  if (gridEntries().length === 0) {
    message = '市场里暂时没有可安装的插件。';
  } else if (rows.length === 0 && String(state.query || '').trim()) {
    message = `没有匹配「${state.query.trim()}」的插件。`;
  } else if (rows.length === 0) {
    message = state.filter === 'update' ? '所有插件都是最新的。' : '这个筛选下没有插件。';
  }
  text(els.gridEmpty, message);
  show(els.gridEmpty, rows.length === 0);
}

/**
 * Renders the notice line.
 *
 * A fallback is not an error - the built-in source is a working catalogue - so
 * it is stated plainly and the list stays usable.
 */
function renderNotice() {
  const parts = [];
  if (state.catalogue.fallback) {
    parts.push('市场地址打不开，正在用内置示例源。');
  }
  if (state.catalogue.notice) parts.push(state.catalogue.notice);
  parts.push(...state.warnings);

  text(els.notice, parts.join(' '));
  show(els.notice, parts.length > 0);

  if (els.marketUrl && document.activeElement !== els.marketUrl) {
    els.marketUrl.value = state.catalogue.base || '';
  }
  text(els.headline, state.catalogue.fallback ? '内置示例源' : '');
}

// --- detail pane ------------------------------------------------------------

/**
 * Orders two dotted numeric versions, mirroring `plugins.CompareVersions`.
 *
 * The market has to answer "is the installed copy behind the catalogue?" in two
 * places - the row tag and whether the detail pane offers an update - and doing it
 * with `!==` instead, as an earlier version did, labels a plugin "可更新" for every
 * version string that merely DIFFERS: a local 1.1.0 against a catalogue 1.1.0 is
 * fine, but so is comparing "1.1" with "1.1.0", and once a downgrade reaches the
 * mirror it would offer to move backwards.
 *
 * A part that is not a plain number TERMINATES the version, so "1.2.0-beta.1"
 * compares equal to "1.2.0" rather than newer. Same reasoning as the Go side: an
 * unrecognised suffix must never be read as an upgrade.
 */
function isNewer(candidate, current) {
  const parse = (v) => {
    const out = [];
    for (const part of String(v ?? '').replace(/^[vV]/, '').split('.')) {
      const n = Number.parseInt(part, 10);
      // A non-numeric part ends the version: appending a 0 here would let a
      // prerelease's own digits compare as a higher release.
      if (!Number.isFinite(n) || n < 0 || String(n) !== part.trim()) return out;
      out.push(n);
    }
    return out;
  };
  const a = parse(candidate);
  const b = parse(current);
  const len = Math.max(a.length, b.length);
  for (let i = 0; i < len; i += 1) {
    const av = a[i] ?? 0;
    const bv = b[i] ?? 0;
    if (av > bv) return true;
    if (av < bv) return false;
  }
  return false;
}

/* THE PERMISSION POPOVER IS GONE (round 7), and with it the whole positioning
 * problem it existed to solve: `position: fixed`, the button-rect maths, the clamping
 * to the viewport, the re-pin on scroll and resize, and the "click anywhere else
 * closes it" listener.
 *
 * It was the right answer to the wrong layout. While the detail was a scrolling pane
 * holding three stacked sections, a floating list was the only way to consult the
 * permissions without pushing the plugin's workspace down. On a PAGE the list has a
 * column of its own in the right rail, where it is always visible, never clipped,
 * needs no click to open and no Escape to dismiss - which is also why the switch's
 * `is-active` state and the granted count moved with it (see renderPermCount).
 */

/** Closes the settings dialog without writing anything. */
function closeSettings() {
  state.cfgOpen = false;
  show(els.settingsSheet, false);
  els.detailSettings?.setAttribute('aria-expanded', 'false');
}

/**
 * The granted count in the permission panel's header: "6 / 8 已授予".
 *
 * It is a HEADING note, not a control's badge any more: the list is always on screen
 * beside the plugin it belongs to, so "how much is granted" is read from the same place
 * as the ticks themselves. For a plugin that is not installed the count is just how
 * many it will ask for.
 */
function renderPermCount(inst, declared, granted) {
  if (!declared.length) {
    text(els.permCount, '');
    return;
  }
  text(els.permCount, inst ? `${granted.size} / ${declared.length} 已授予` : `${declared.length} 项`);
}

function renderDetail() {
  const id = state.selected;
  const inst = installedRow(id);
  const entry = catalogueRow(id);

  if (!id || (!inst && !entry)) {
    // Nothing is selected: the detail page has no subject, so it is not shown at all
    // and the grid is what "no plugin" looks like.
    state.cfgOpen = false;
    show(els.settingsSheet, false);
    show(els.detailView, false);
    goGrid();
    return;
  }

  show(els.detailView, true);

  const man = inst?.Manifest || null;
  const name = man?.name || entry?.name || id;
  text(els.detailName, name);
  text(els.crumbName, name);
  // The hero's monogram, from the same hash the card used: clicking a card and
  // arriving at a differently-coloured tile would read as a different plugin.
  paintAvatar(els.detailAvatar, name, id);

  const version = inst?.Version || man?.version || entry?.version || '';
  const author = man?.author || entry?.author || '';
  const description = man?.description || entry?.description || '';
  const declared = man?.permissions || entry?.permissions || [];

  // The pitch is ONE line here, with the whole string in `title`: the hero is a
  // header, and the same sentence is readable in full on the card behind it.
  text(els.detailTagline, description);
  if (els.detailTagline) els.detailTagline.title = description;

  // Three facts, three badges. Empty ones leave the row (`.badge:empty`), so a plugin
  // with no author does not render a blank pill.
  text(els.detailVersion, version ? `v${version}` : '');
  text(els.detailAuthor, author);
  text(els.detailPermsBadge, declared.length ? `${declared.length} 项权限` : '无需权限');

  text(els.detailError, inst?.Error || '');
  show(els.detailError, Boolean(inst?.Error));

  // The install button is hidden once a plugin is present, and the UPDATE button
  // takes its place when the catalogue is ahead of what is on disk. The two are
  // mutually exclusive by construction: an update only makes sense for something
  // already installed, and an install only for something that is not.
  const canUpdate = Boolean(inst) && Boolean(entry) && isNewer(entry.version, inst.Version);
  show(els.detailInstall, !inst && Boolean(entry));
  show(els.detailUpdate, canUpdate);
  show(els.detailUninstall, Boolean(inst));
  // The LABEL is hidden, not the input: the checkbox is visually replaced by the
  // switch track, so hiding only the input would leave the track and the word
  // 启用 on screen for a plugin that is not installed.
  show(els.detailEnabledRow, Boolean(inst));
  if (els.detailEnabled && inst) {
    els.detailEnabled.checked = Boolean(inst.Enabled);
    els.detailEnabled.disabled = Boolean(inst.Error);
  }
  if (els.detailInstall) {
    els.detailInstall.disabled = !entry || state.busy;
  }
  if (els.detailUpdate) {
    els.detailUpdate.disabled = !canUpdate || state.busy;
    // The LABEL span, never the button: the button also carries an arrow icon and
    // `textContent` on it would delete that icon the first time a version arrived.
    text(els.detailUpdateLabel, canUpdate ? `更新到 ${entry.version}` : '更新到新版本');
  }

  // Permissions: what the manifest declares, ticked to match what was granted.
  // For a not-yet-installed plugin every box starts ticked, because "install and
  // authorise" is one action - the sheet that confirms it is where the user can still
  // say no.
  const granted = new Set(inst?.Granted || (inst ? [] : declared));

  els.permList?.replaceChildren();
  for (const permission of declared) {
    const note = inst && !granted.has(permission) ? '未授予' : '';
    const row = permissionRow(permission, { checked: granted.has(permission), note });
    // The row is dimmed as a whole when it is withheld, so "this one is off" reads at
    // a glance and not only from the small note under the name.
    if (inst && !granted.has(permission)) row.classList.add('is-off');
    row.dataset.permission = permission;
    els.permList?.appendChild(row);
  }
  show(els.permEmpty, declared.length === 0);
  renderPermCount(inst, declared, granted);

  renderFacts(inst, entry, id, version, author);
  renderSettingsState(inst);

  renderConfigPane(inst);
  renderPluginUI(inst);
}

/**
 * The 关于 panel: what this install IS.
 *
 * Four facts, all of which the page would otherwise make the user open a dialog or a
 * file to find: the version, the author, when it was installed, and the id (the string
 * they paste into an issue). Rendered as a definition list so the labels are real
 * labels to a screen reader rather than three grey words and a column of values.
 */
function renderFacts(inst, entry, id, version, author) {
  const host = els.detailFacts;
  if (!host) return;
  host.replaceChildren();

  const facts = [
    ['版本', version || '—', false],
    ['作者', author || '—', false],
    ['安装于', formatInstallDate(inst?.InstalledAt), false],
    ['插件 id', id, true],
  ];
  for (const [label, value, mono] of facts) {
    if (!value) continue;
    const row = build('div');
    row.appendChild(build('dt', '', label));
    row.appendChild(build('dd', mono ? 'mono' : '', value));
    host.appendChild(row);
  }
}

/** "2026/10/22 16:13" from an RFC3339 stamp, or '' when there is none. */
function formatInstallDate(stamp) {
  if (!stamp) return '';
  const at = new Date(stamp);
  if (Number.isNaN(at.getTime())) return '';
  const pad = (n) => String(n).padStart(2, '0');
  return `${at.getFullYear()}/${pad(at.getMonth() + 1)}/${pad(at.getDate())} ${pad(at.getHours())}:${pad(at.getMinutes())}`;
}

/**
 * Whether this plugin has a settings form at all, and keeps the dialog's buttons in
 * step with that.
 *
 * The gear exists only when the plugin declares settings AND is installed: a gear that
 * opens nothing is a dead control, and a settings form for something that is not on
 * disk has nothing to write to. The dialog is force-closed when its reason to exist
 * goes away, or it would be left open behind a button that is now hidden.
 */
function renderSettingsState(inst) {
  const hasConfig = Boolean(inst) && (inst.Manifest?.config || []).length > 0;
  show(els.detailSettings, hasConfig);
  if (!hasConfig) state.cfgOpen = false;
  show(els.settingsSheet, state.cfgOpen && hasConfig);
  text(els.settingsTitle, hasConfig ? `${inst?.Manifest?.name || inst?.ID || ''} · 设置` : '');
  els.detailSettings?.setAttribute('aria-expanded', String(Boolean(state.cfgOpen && hasConfig)));
}

/* There is no status chip any more. It existed because the pane once conveyed state
 * only through which buttons happened to be visible - an update button meant
 * "installed and behind" - but it ended up saying 已启用 directly beside the switch
 * that shows and changes exactly that. The switch is now the state; the list row
 * keeps its stripe and tag for scanning; a broken manifest keeps the red error line
 * and a disabled switch. */

/**
 * Mounts the plugin's own interface, when it ships one this window can host.
 *
 * A plugin's `entry` is the document the host loads; whether it wants a workspace
 * here is decided by the plugin, not by this page guessing from its id. The gate is
 * simply "is it installed and enabled with a readable manifest" - the frame itself
 * then asks for whatever capabilities it needs and is refused the ones this program
 * does not have (see plugin-ui.js).
 *
 * Nothing is mounted for a plugin that is disabled: switching a plugin off has to
 * stop it RUNNING, not just stop it being listed, and an interface left on screen
 * would keep its document alive with its permissions intact.
 */
function renderPluginUI(inst) {
  const hasInterface = Boolean(inst && inst.Manifest && inst.Enabled && inst.Manifest.entry);

  // The workspace BOX follows the frame: a caption strip over an empty area would be
  // furniture around nothing. When there is no interface the box is replaced by one
  // sentence that says WHY, because the three reasons (not installed yet, disabled,
  // ships no interface) are three different things for the user to do.
  show(els.panelInterface, hasInterface);
  show(els.workspaceEmpty, !hasInterface);

  if (hasInterface) {
    mountPluginUI(inst, els);
    return;
  }

  clearPluginUI(els);
  let why = '这个插件没有自带界面。';
  if (!inst) why = '安装之后，插件自己的界面会显示在这里。';
  else if (inst.Error) why = `界面不可用：${inst.Error}`;
  else if (!inst.Enabled) why = '插件已停用，界面已关闭。打开「启用」它才会运行。';
  else if (!inst.Manifest?.entry) why = '这个插件没有声明界面（manifest 里没有 entry）。';
  text(els.workspaceEmpty, why);
}

/** Renders (or clears) the settings editor for the selected plugin. */
function renderConfigPane(inst) {
  const declared = inst?.Manifest?.config || [];
  const items = state.config?.items || [];
  els.cfgForm?.replaceChildren();

  if (!inst) {
    show(els.cfgEmpty, false);
    // The grid itself is hidden, not just emptied. An empty settings PANEL under
    // the words "安装后才能配置。" reads as a control that failed to load, and the
    // panel is part of the section's furniture rather than a message.
    show(els.cfgForm, false);
    if (els.cfgSave) els.cfgSave.disabled = true;
    text(els.cfgNote, '安装后才能配置。');
    text(els.cfgCount, '');
    return;
  }
  if (declared.length === 0) {
    show(els.cfgEmpty, true);
    show(els.cfgForm, false);
    if (els.cfgSave) els.cfgSave.disabled = true;
    text(els.cfgNote, '');
    text(els.cfgCount, '');
    return;
  }

  show(els.cfgEmpty, false);
  show(els.cfgForm, true);
  if (els.cfgSave) els.cfgSave.disabled = state.busy || !state.config;
  text(els.cfgNote, state.config?.usage ? `用量：${state.config.usage}` : '');
  text(els.cfgCount, `${items.length} 项`);

  for (const item of items) {
    els.cfgForm?.appendChild(configField(item));
  }
}

/**
 * Whether one settings control wants the whole width of the config grid.
 *
 * The decision is made from the ITEM's own shape rather than from a list of key
 * names, so a plugin that adds a long field gets a sensible layout without anyone
 * editing this file - and no plugin's private key names are hard-coded into the
 * window that merely edits them.
 *
 * A control is full-width when it is one of:
 *   - a text box whose label or stored value is long (a path or a URL: the voice
 *     plugin's weight paths are the case that prompted this);
 *   - a field carrying a note underneath it, because the note is a full sentence
 *     and a half-width cell wraps it to three lines;
 *   - a textarea, which is multi-line by definition.
 */
function fieldWantsFullWidth(item) {
  if (item.type === 'textarea') return true;
  // A checkbox group wraps: give it the whole row instead of squeezing labels.
  if (item.type === 'select') return true;
  const note = item.type === 'secret' || item.hostWhitelist;
  if (note) return true;
  if (item.type === 'text') {
    const label = String(item.label || '');
    const value = String(item.value || item.default || '');
    // 14 characters is roughly where a half-width cell starts truncating a path,
    // and it is a threshold about the TEXT rather than about any one plugin.
    return label.length > 14 || value.length > 20;
  }
  return false;
}

/**
 * Builds one settings control.
 *
 * A `secret` item is deliberately a blank write-only field: the backend sends
 * only whether a value is stored and how it masks, so there is nothing to
 * backfill and the plaintext never reaches the page. Leaving it blank means
 * "keep what is stored"; typing in it replaces it.
 */
function configField(item) {
  const field = build('label', 'field');
  field.dataset.key = item.key;
  if (fieldWantsFullWidth(item)) field.classList.add('wide');
  field.appendChild(build('span', null, item.label || item.key));

  let input;
  switch (item.type) {
    case 'bool': {
      input = document.createElement('input');
      input.type = 'checkbox';
      input.checked = String(item.value ?? item.default ?? '') === 'true';
      break;
    }
    case 'number': {
      input = document.createElement('input');
      input.type = 'number';
      input.step = 'any';
      input.value = item.value ?? '';
      if (!input.value && item.default) input.placeholder = item.default;
      break;
    }
    case 'secret': {
      input = document.createElement('input');
      input.type = 'password';
      input.autocomplete = 'new-password';
      input.spellcheck = false;
      // Never populated from state, ever: the value is write-only.
      input.value = '';
      input.placeholder = item.secretSet ? item.secretMask || '已保存' : '未设置';
      break;
    }
    case 'select': {
      if (item.multiple) {
        // A checkbox group: the stored value is the chosen values joined by commas,
        // which is the shape a plugin was already parsing with a split. Returned early
        // because there is no single control to hang the rest of this function on.
        const group = build('div', 'checks');
        group.id = `cfg-${item.key}`;
        group.dataset.key = item.key;
        group.dataset.type = 'select';
        const chosen = new Set(String(item.value || item.default || '').split(/[,，\s]+/).filter(Boolean));
        for (const option of item.options || []) {
          const row = build('label', 'check');
          const box = document.createElement('input');
          box.type = 'checkbox';
          box.value = option.value;
          box.dataset.key = item.key;
          box.checked = chosen.has(option.value);
          row.append(box, build('span', null, option.label || option.value));
          group.append(row);
        }
        field.appendChild(group);
        return field;
      }
      input = document.createElement('select');
      for (const option of item.options || []) {
        const node = document.createElement('option');
        node.value = option.value;
        node.textContent = option.label || option.value;
        input.appendChild(node);
      }
      const current = String(item.value || item.default || '');
      if (current) input.value = current;
      break;
    }
    default: {
      input = document.createElement('input');
      input.type = 'text';
      input.spellcheck = false;
      input.value = item.value ?? '';
      if (!input.value && item.default) input.placeholder = item.default;
    }
  }

  input.id = `cfg-${item.key}`;
  input.dataset.key = item.key;
  input.dataset.type = item.type;
  field.appendChild(input);

  if (item.type === 'secret') {
    if (item.secretSet) field.classList.add('set');
    field.appendChild(
      build('span', 'field-note', item.secretSet ? '已保存；留空则保持不变，输入新值则替换' : '未设置'),
    );
  } else if (item.hostWhitelist) {
    field.appendChild(build('span', 'field-note', '这个值也决定 net.fetch 可以访问哪些域名。'));
  }

  return field;
}

// --- install sheet ----------------------------------------------------------

function openSheet(id) {
  const entry = catalogueRow(id);
  if (!entry) return;
  state.pending = id;

  text(els.sheetTitle, `安装「${entry.name || id}」`);
  text(els.sheetDesc, entry.description || '');
  text(els.sheetStatus, '安装后会自动授予下面这些权限；之后可以随时停用或卸载。');

  els.sheetPerms?.replaceChildren();
  for (const name of entry.permissions || []) {
    els.sheetPerms?.appendChild(permissionRow(name));
  }
  if ((entry.permissions || []).length === 0) {
    els.sheetPerms?.appendChild(build('li', 'empty', '这个插件不申请任何权限。'));
  }

  show(els.sheet, true);
  els.sheetConfirm?.focus();
}

function closeSheet() {
  state.pending = null;
  show(els.sheet, false);
}

// --- actions ----------------------------------------------------------------

function setBusy(on) {
  state.busy = on;
  for (const el of [els.marketSave, els.marketRefresh, els.sheetConfirm, els.cfgSave]) {
    if (el) el.disabled = on;
  }
}

/** Pulls the catalogue. A failure keeps the previous list on screen. */
async function refresh({ silent = false } = {}) {
  try {
    state.catalogue = await Market.Catalogue();
  } catch (err) {
    // Deliberately NOT clearing state.catalogue: the list the user was reading
    // stays, and the reason is stated next to it.
    text(els.notice, `读取市场失败：${err?.message || err}`);
    show(els.notice, true);
    if (!silent) toast(`读取市场失败：${err?.message || err}`, true);
    return false;
  }
  renderGrid();
  renderNotice();
  renderDetail();
  return true;
}

/** Re-reads the installed list. */
async function refreshInstalled() {
  try {
    state.installed = (await Market.Installed()) || [];
    state.warnings = (await Market.Warnings()) || [];
  } catch (err) {
    toast(`读取已安装插件失败：${err?.message || err}`, true);
    return false;
  }
  renderInstalled();
  return true;
}

/** Loads the selected plugin's settings editor. */
async function loadConfig(id) {
  state.config = null;
  if (!installedRow(id)) return;
  try {
    state.config = await Market.GetConfig(id);
  } catch (err) {
    text(els.cfgNote, `读取配置失败：${err?.message || err}`);
    show(els.cfgNote, true);
  }
}

/** Reads the editor back into a value map, applying the declared defaults. */
function collectConfig() {
  const out = {};
  for (const item of state.config?.items || []) {
    // A picker is read before the input lookup below: a single <select> is not an
    // `input`, and a group is many of them, so the generic "one control per key"
    // assumption would either miss it or send only the first ticked box.
    if (item.type === 'select') {
      if (item.multiple) {
        const boxes = [...(els.cfgForm?.querySelectorAll(`input[data-key="${item.key}"][type="checkbox"]`) || [])];
        const chosen = boxes.filter((box) => box.checked).map((box) => box.value);
        out[item.key] = chosen.length ? chosen.join(',') : String(item.default || '');
      } else {
        const select = els.cfgForm?.querySelector(`select[data-key="${item.key}"]`);
        if (select) out[item.key] = select.value || String(item.default || '');
      }
      continue;
    }
    // Scoped to the CONTROL, not the field wrapper: both carry data-key, and the
    // wrapper comes first in document order, so a bare [data-key] selector would
    // hand back the <label> and read .value off it as undefined.
    const input = els.cfgForm?.querySelector(`input[data-key="${item.key}"]`);
    if (!input) continue;

    if (item.type === 'bool') {
      out[item.key] = input.checked ? 'true' : 'false';
    } else if (item.type === 'secret') {
      // Absent means "leave it alone": the editor cannot read the stored value,
      // so an untouched field must not be sent as an empty replacement.
      if (input.value !== '') out[item.key] = input.value;
    } else if (input.value !== '') {
      out[item.key] = input.value;
    } else if (item.default) {
      out[item.key] = item.default;
    }
  }
  return out;
}

async function saveConfig() {
  if (!state.selected || !state.config) return;
  setBusy(true);
  try {
    await Market.SaveConfig(state.selected, collectConfig());
    await loadConfig(state.selected);
    renderConfigPane(installedRow(state.selected));
    // Saving closes the dialog: the form WAS the detour, and a dialog that stays
    // open after "保存" makes the user close what they already finished with. A
    // failure keeps it open, because there is something to fix in it.
    state.cfgOpen = false;
    renderSettingsState(installedRow(state.selected));
    toast('设置已保存');
  } catch (err) {
    toast(`保存失败：${err?.message || err}`, true);
  } finally {
    setBusy(false);
  }
}

/** Installs the pending catalogue entry, then grants it its declared permissions. */
async function confirmInstall() {
  const id = state.pending;
  if (!id) return;
  const entry = catalogueRow(id);
  setBusy(true);
  try {
    text(els.sheetStatus, '正在下载并校验…');
    await Market.Install(id);

    if (entry?.permissions?.length) {
      text(els.sheetStatus, '正在授权…');
      await Market.Grant(id, entry.permissions);
    }

    closeSheet();
    await refreshInstalled();
    await Market.RememberSelection(id);
    state.selected = id;
    // A freshly installed plugin opens on its own page with the settings dialog shut:
    // what changed is that it now exists, and the interface it ships is shown straight
    // away.
    state.cfgOpen = false;
    await loadConfig(id);
    await refresh({ silent: true });
    renderDetail();
    goDetail();
    toast(`已安装 ${entry?.name || id}`);
  } catch (err) {
    text(els.sheetStatus, `安装失败：${err?.message || err}`);
    toast(`安装失败：${err?.message || err}`, true);
  } finally {
    setBusy(false);
  }
}

/**
 * Replaces the selected plugin with the newer catalogue version.
 *
 * It does NOT go through the install sheet, and that is deliberate: the sheet exists
 * to show what a plugin will ASK FOR before it is on disk, and an update grants
 * nothing new - the permissions were already accepted for this id, and the manager
 * carries them across. Re-prompting would train the user to click through the one
 * dialog that is supposed to make them stop and read.
 *
 * The installed list and the config editor are both re-read afterwards, because an
 * update can add configuration items the previous version did not declare - which is
 * exactly the case this whole path was added for.
 */
async function updateSelected() {
  const id = state.selected;
  const entry = catalogueRow(id);
  if (!id || !entry || !installedRow(id)) return;
  setBusy(true);
  try {
    await Market.Update(id);
    await refreshInstalled();
    await refresh({ silent: true });
    await loadConfig(id);
    renderDetail();
    toast(`已更新到 ${entry.version}`);
  } catch (err) {
    toast(`更新失败：${err?.message || err}`, true);
  } finally {
    setBusy(false);
  }
}

async function uninstallSelected() {
  const id = state.selected;
  if (!id || !installedRow(id)) return;
  setBusy(true);
  try {
    await Market.Uninstall(id);
    await refreshInstalled();
    await refresh({ silent: true });
    renderDetail();
    toast('已卸载');
  } catch (err) {
    toast(`卸载失败：${err?.message || err}`, true);
  } finally {
    setBusy(false);
  }
}

async function toggleEnabled(on) {
  const id = state.selected;
  if (!id) return;
  setBusy(true);
  try {
    await Market.SetEnabled(id, on);
    await refreshInstalled();
    renderDetail();
  } catch (err) {
    toast(`切换失败：${err?.message || err}`, true);
    // Put the switch back where the backend says it is.
    if (els.detailEnabled) els.detailEnabled.checked = !on;
  } finally {
    setBusy(false);
  }
}

/**
 * Persists the permission ticks.
 *
 * Granting is per-plugin and wholesale: the backend replaces the granted set, so
 * un-ticking a box really does revoke it.
 */
async function savePermissions() {
  const id = state.selected;
  if (!id || !installedRow(id)) return;
  const granted = [];
  for (const box of els.permList?.querySelectorAll('input[data-perm]') || []) {
    if (box.checked) granted.push(box.dataset.perm);
  }
  try {
    await Market.Grant(id, granted);
    await refreshInstalled();
    renderDetail();
  } catch (err) {
    toast(`保存权限失败：${err?.message || err}`, true);
  }
}

async function saveMarketURL() {
  const raw = els.marketUrl?.value ?? '';
  setBusy(true);
  try {
    // `|| state.catalogue` rather than a bare assignment: the binding answers with the
    // catalogue it just read, and a backend that answers with nothing (or a test double
    // that returns null) must not blank the page's own copy of it - the next render
    // reads `state.catalogue.base` and would throw on null.
    state.catalogue = (await Market.SaveMarketURL(raw)) || state.catalogue;
    renderGrid();
    renderNotice();
    renderDetail();
    // The sheet closes only on success: a rejected address keeps the field in front of
    // the user, with their text still in it, so they can fix the typo instead of
    // retyping it.
    closeMarketSheet();
    toast('市场地址已保存');
  } catch (err) {
    // Nothing was written, so the old address is still in force; say so rather
    // than leaving the user to guess.
    text(els.marketNote, `这个地址不可用：${err?.message || err}`);
    toast(`地址无效：${err?.message || err}`, true);
  } finally {
    setBusy(false);
  }
}

/** Opens the market's settings sheet, pre-filled with the address in force. */
function openMarketSheet() {
  if (els.marketUrl) els.marketUrl.value = state.catalogue.base || '';
  text(els.marketNote, '');
  show(els.marketSheet, true);
  els.marketSettings?.setAttribute('aria-expanded', 'true');
  els.marketUrl?.focus();
  els.marketUrl?.select();
}

function closeMarketSheet() {
  show(els.marketSheet, false);
  els.marketSettings?.setAttribute('aria-expanded', 'false');
}

/**
 * Opens one plugin's page.
 *
 * This is what a card click does, and it is also what the window does on boot when the
 * settings file names a plugin - so "where I was" survives a restart, while "which of
 * the two views I was looking at" does not.
 */
async function select(id) {
  // A different plugin starts with the settings dialog shut: the previous plugin's
  // open form is about THAT plugin, and a settings form for the wrong plugin is worse
  // than one more click.
  if (id !== state.selected) state.cfgOpen = false;
  state.selected = id;
  await Market.RememberSelection(id).catch(() => {});
  await loadConfig(id);
  // The grid repaints because the selected card is drawn differently, and the page
  // repaints because everything on it comes from the selection.
  renderInstalled();
  renderDetail();
  goDetail();
}

/** Back to the catalogue. The selection is KEPT: coming back to a plugin should not
 * need finding it again, and the remembered selection is what the window reopens to. */
function backToGrid() {
  state.cfgOpen = false;
  show(els.settingsSheet, false);
  goGrid();
}

// --- wiring -----------------------------------------------------------------

function wire() {
  // --- the catalogue ---------------------------------------------------------
  //
  // Card clicks are DELEGATED: the grid is rebuilt on every render, so one listener
  // on the container cannot go stale. A click on the footer's verb is an ACTION
  // (install / update / open) and is handled first - the card beneath it navigates.
  els.grid?.addEventListener('click', (e) => {
    const card = e.target.closest('.card');
    if (!card?.dataset.id) return;
    const verb = e.target.closest('button[data-verb]');
    if (!verb) {
      select(card.dataset.id);
      return;
    }
    const id = card.dataset.id;
    if (verb.dataset.verb === 'install') openSheet(id);
    else if (verb.dataset.verb === 'update') select(id).then(() => updateSelected());
    else select(id);
  });

  // The cards are focusable (role="button" + tabindex), so Enter and Space have to do
  // what a click does. Without this the grid is unreachable by keyboard, which is the
  // kind of thing a mouse-only review never notices.
  els.grid?.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    const card = e.target.closest('.card');
    if (!card?.dataset.id) return;
    e.preventDefault();
    select(card.dataset.id);
  });

  for (const [el, name] of [
    [els.filterAll, 'all'],
    [els.filterInstalled, 'installed'],
    [els.filterUpdate, 'update'],
    [els.filterMarket, 'market'],
  ]) {
    el?.addEventListener('click', () => setFilter(name));
  }

  // Search is live. No debounce: filtering an array of a few dozen objects is cheaper
  // than the frame it would take to schedule it, and a debounce would make typing feel
  // laggy at exactly the moment the user is looking for feedback.
  els.search?.addEventListener('input', () => {
    state.query = els.search.value || '';
    renderGrid();
  });

  els.backToGrid?.addEventListener('click', () => backToGrid());

  // --- actions ---------------------------------------------------------------
  els.marketRefresh?.addEventListener('click', () => refresh());
  els.marketSettings?.addEventListener('click', () => openMarketSheet());
  els.marketCancel?.addEventListener('click', () => closeMarketSheet());
  els.marketSave?.addEventListener('click', () => saveMarketURL());
  els.marketSheet?.addEventListener('click', (e) => {
    if (e.target === els.marketSheet) closeMarketSheet();
  });
  els.marketUrl?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') saveMarketURL();
  });
  els.themeToggle?.addEventListener('click', () => toggleTheme());

  // A permission tick is saved as soon as it changes for an INSTALLED plugin: there is
  // no separate "apply" step to forget.
  els.permList?.addEventListener('change', (e) => {
    if (e.target.matches('input[data-perm]') && installedRow(state.selected)) {
      savePermissions();
    }
  });

  els.detailInstall?.addEventListener('click', () => openSheet(state.selected));
  els.detailUpdate?.addEventListener('click', () => updateSelected());
  els.detailUninstall?.addEventListener('click', () => uninstallSelected());
  els.detailEnabled?.addEventListener('change', (e) => toggleEnabled(e.target.checked));
  els.cfgSave?.addEventListener('click', () => saveConfig());

  // 设置 opens the settings DIALOG. There is no toggle: the modal backdrop covers the
  // gear, so a second click on it cannot happen - it closes by 关闭, Escape, clicking
  // the backdrop, or saving.
  els.detailSettings?.addEventListener('click', () => {
    state.cfgOpen = true;
    renderSettingsState(installedRow(state.selected));
  });
  els.settingsCancel?.addEventListener('click', () => closeSettings());
  els.settingsSheet?.addEventListener('click', (e) => {
    if (e.target === els.settingsSheet) closeSettings();
  });

  els.sheetCancel?.addEventListener('click', () => closeSheet());
  els.sheetConfirm?.addEventListener('click', () => confirmInstall());
  els.sheet?.addEventListener('click', (e) => {
    if (e.target === els.sheet) closeSheet();
  });

  // Escape closes whichever surface is open, innermost first. The two remaining
  // surfaces are both modals; the permission list is part of the page now and has
  // nothing to dismiss.
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    if (!els.sheet?.classList.contains('hidden')) {
      closeSheet();
      return;
    }
    if (!els.settingsSheet?.classList.contains('hidden')) {
      closeSettings();
      return;
    }
    if (!els.marketSheet?.classList.contains('hidden')) {
      closeMarketSheet();
    }
  });
}

/**
 * First paint: the colour scheme, then the data, then the view.
 *
 * WHICH VIEW. A window with anything installed opens on the plugin it was last on -
 * this program doubles as the place a plugin's own workspace lives, and making that
 * take an extra click every launch would be a worse trade than the storefront being
 * one click away behind 「插件市场」. With nothing installed there is no such page, so
 * it opens on the grid, which is also what a first run should look at.
 */
async function boot() {
  wire();

  // The theme is the ONE thing that has to be right on the first frame, so it is
  // asked for on its own before anything else is fetched. It lives in this program's
  // settings file (not in the webview's own storage), which is also what makes the
  // appearance deterministic for the real-window gate: it seeds that file, so it
  // always opens in the scheme the file names.
  applyTheme(await Market.Theme().catch(() => 'dark'));

  await Promise.all([refreshInstalled(), refresh()]);

  const remembered = await Market.SelectedID().catch(() => '');
  const installedIds = new Set(state.installed.map((p) => p.ID));
  const wanted = installedIds.has(remembered) ? remembered : state.installed[0]?.ID || '';

  if (wanted) {
    await select(wanted);
    return;
  }

  renderGrid();
  goGrid();
}

boot();

export {
  state,
  els,
  PERMISSION_LABELS,
  permissionLabel,
  collectConfig,
  fieldWantsFullWidth,
  applyTheme,
  // The view, filter and data rules, exported so "which view is on screen", "which
  // chip is applied", "which rows a filter+search produce" and "what a click does"
  // can be asserted directly as well as through the DOM.
  goGrid,
  goDetail,
  setFilter,
  select,
  backToGrid,
  refreshInstalled,
  renderGrid,
  gridEntries,
  matchesFilter,
  matchesQuery,
  // The settings-form rule, exported so "does this plugin get a gear at all" can be
  // asserted directly as well as through the DOM.
  renderSettingsState,
};
