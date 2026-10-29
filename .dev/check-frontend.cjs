// Checks the frontend wiring without a browser.
//
// WebView2 refuses to create a controller in this environment (the pre-change
// binary from before this feature fails identically), so the GUI cannot be
// exercised. These checks cover the class of mistake that would otherwise only
// show up as "the button does nothing": an id referenced from JS that does not
// exist in the HTML, a binding called that Go does not expose, or a border-box
// decoration in the new CSS that would paint onto the user's desktop.

const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');
const html = fs.readFileSync(path.join(root, 'frontend/index.html'), 'utf8');
const mainJs = fs.readFileSync(path.join(root, 'frontend/src/main.js'), 'utf8');
// The one plugin module the pet still has. It is read separately from main.js
// because it is scanned in THREE places below (element ids, App.* calls, and
// EventsOn subscriptions), and a module the scans do not read is a module none of
// them check: a typo in plugin-host.js would reach the user as "the button does
// nothing" while every assertion here stayed green.
//
// The pet's other frontend modules are gone. chat.js and live.js were the host's
// own conversation UI and plugins.js was the plugin tab; all three were deleted
// when the conversation became a plugin and the tab moved into the market
// program, and reading them here would now throw at startup rather than fail an
// assertion.
const pluginHostJs = fs.readFileSync(path.join(root, 'frontend/src/plugin-host.js'), 'utf8');
// Every module whose id lookups and binding calls are checked. Add a module here
// and it is checked in all three scans; leave it out and it is checked nowhere.
//
// This is exactly the two modules the pet's page is built from: main.js is the
// shell, plugin-host.js is the sandboxed-iframe protocol. The market program's
// frontend is a SEPARATE program with its own gate (section 17 below reads it
// through its own variable) and deliberately does NOT enter this set - its ids
// live in a different index.html, so checking them against this one would report
// every one of them as missing.
const scannedJs = [mainJs, pluginHostJs];
// The market program's frontend, read for section 17's "it is a separate program"
// checks. It is deliberately not part of scannedJs.
const marketMainJs = fs.readFileSync(
  path.join(root, 'tools/plugin-market/frontend/src/main.js'),
  'utf8',
);

const css = fs.readFileSync(path.join(root, 'frontend/src/style.css'), 'utf8');
const appJs = fs.readFileSync(path.join(root, 'frontend/wailsjs/go/main/App.js'), 'utf8');
const appDts = fs.readFileSync(path.join(root, 'frontend/wailsjs/go/main/App.d.ts'), 'utf8');
// The chat plugin's document, read for the auto-hide guard's flag (see the check at the end).
// `market/` is a separate repo, so a missing checkout must not crash the gate: the check that uses
// this reports a plain failure instead.
const chatPluginJs = (() => {
  try {
    return fs.readFileSync(path.join(root, 'market/plugins/com.shixiansi.plugin-chat/app.js'), 'utf8');
  } catch {
    return '';
  }
})();
const modelsTs = fs.readFileSync(path.join(root, 'frontend/wailsjs/go/models.ts'), 'utf8');
// The main app file, read here because the tray menu and its command handling
// live in it - the shortcut feature's mouse fallback is checked against it.
const mainGo = fs.readFileSync(path.join(root, 'app.go'), 'utf8');
// The pomodoro gate used to read app_pomodoro.go and internal/pomodoro/machine.go.
// BOTH FILES ARE GONE: the host's timer retired into the pomodoro plugin
// (openspec/changes/todo-pomodoro-as-plugins), taking the state machine, the two
// gates, the picker and the question-reveal cycle with it. The bindings below are
// empty strings so a straggling assertion fails LOUDLY instead of throwing ENOENT -
// a gate that throws is a gate nobody reads.
const goAppPomodoro = '';
const goPomodoroEngine = '';
// The chat request path, for the "no reply is read aloud" invariants.
const goAppLlm = fs.readFileSync(path.join(root, 'app_llm.go'), 'utf8');
// The plugin capability table and the shared asset/dispatch layer. Speech became a
// plugin, so what used to be read from app_voice.go and internal/voice is now about
// the generic mechanisms the plugin runs on: the audio capability, the two edge
// events, and the headless frame that lets a plugin act with no interface open.
const goAppPlugins = fs.readFileSync(path.join(root, 'app_plugins.go'), 'utf8');
// The permission vocabulary itself. Read once here because THREE checks compare
// against it - the count below, the market's label map in section 17, and the "every
// market plugin's permissions are known" check - and a table each of them read
// separately would be three chances for them to disagree about what was read.
const capsGo = fs.readFileSync(path.join(root, 'internal/plugins/capability.go'), 'utf8');
const goPluginsDispatch = fs.readFileSync(path.join(root, 'internal/plugins/dispatch.go'), 'utf8');
const goPluginsAsset = fs.readFileSync(path.join(root, 'internal/plugins/asset.go'), 'utf8');
const goPluginsVoiceMigrate = fs.readFileSync(path.join(root, 'internal/plugins/voice_migrate.go'), 'utf8');
// The private store: where a plugin's own keys become paths. Read here because the
// audio-path check in the plugin section asserts StorageKeyPath there, and that
// validator MOVED into this file when the private store left package main - the
// check went on looking for its old name in app_plugins.go and was red for it.
const goPrivateStore = fs.readFileSync(path.join(root, 'internal/plugins/privatestore.go'), 'utf8');
const goPluginHostJs = fs.readFileSync(path.join(root, 'frontend/src/plugin-host.js'), 'utf8');
// The voice plugin's package, and the market's plugin hosting. Read here rather than
// in section 16 because the shared-dispatcher check in section 11 needs
// marketPluginHost, and a const declared later would be in its temporal dead zone.
const voicePluginDir = path.join(root, 'market/plugins/com.shixiansi.plugin-voice');
const voicePluginManifest = JSON.parse(
  fs.readFileSync(path.join(voicePluginDir, 'manifest.json'), 'utf8'));
const voicePluginJS = fs.readFileSync(path.join(voicePluginDir, 'app.js'), 'utf8');
const voicePluginHTML = fs.readFileSync(path.join(voicePluginDir, 'panel.html'), 'utf8');
const voicePluginCSS = fs.readFileSync(path.join(voicePluginDir, 'style.css'), 'utf8');
// Every plugin this repo ships, for the "it follows the window's scheme" check. They
// are separate documents in sandboxed frames, so each one has to do its own part.
const bundledPlugins = ['com.shixiansi.plugin-chat', 'com.shixiansi.plugin-qq', 'com.shixiansi.plugin-voice']
  .map((id) => {
    const dir = path.join(root, 'market/plugins', id);
    return {
      id,
      css: fs.readFileSync(path.join(dir, 'style.css'), 'utf8'),
      html: fs.readFileSync(path.join(dir, 'panel.html'), 'utf8'),
      js: fs.readFileSync(path.join(dir, 'app.js'), 'utf8'),
    };
  });
// The market's plugin hosting: the asset route and the dispatcher that answer a
// plugin's files and requests in THAT program.
const marketPluginHost = fs.readFileSync(path.join(root, 'tools/plugin-market/pluginhost.go'), 'utf8');
// The FRAME that hosts a plugin's own interface, i.e. the sandbox boundary.
//
// This used to read `tools/plugin-market/frontend/src/plugin-ui.js`. That file is still in
// the tree but it is NO LONGER SHIPPED: the control center (`cc/`) became this program's
// interface, and the plugin page it grew only listed plugins and their permissions - which
// is exactly how three bundled plugins (voice / qq / bilibili, all `panel: false`) ended up
// with no visible interface in any program. The frame host moved to
// `cc/src/lib/plugin-frame.ts` and the assertions below follow it
// (openspec/changes/plugin-page-workspace). A gate that pins a file nobody builds is worse
// than no gate: it reports PASS about code that cannot run.
const ccPluginFrame = fs.readFileSync(path.join(root, 'tools/plugin-market/cc/src/lib/plugin-frame.ts'), 'utf8');
const ccPluginFrameHost = fs.readFileSync(
  path.join(root, 'tools/plugin-market/cc/src/components/PluginFrame.svelte'), 'utf8');
const ccPluginsPage = fs.readFileSync(path.join(root, 'tools/plugin-market/cc/src/pages/Plugins.svelte'), 'utf8');
// The control center's palette, i.e. what a light window actually looks like. Its light
// scheme is a token block keyed on `data-theme` (not a second stylesheet).
const ccTokens = fs.readFileSync(path.join(root, 'tools/plugin-market/cc/src/tokens.css'), 'utf8');

const failures = [];
const notes = [];

const check = (name, ok, detail) => {
  if (ok) notes.push(`  PASS  ${name}`);
  else failures.push(`  FAIL  ${name} -- ${detail}`);
};

// --- 1. Every $('id') in the frontend exists in the HTML --------------------
const htmlIds = new Set([...html.matchAll(/\bid="([^"]+)"/g)].map((m) => m[1]));
const usedIds = new Set();
for (const src of scannedJs) {
  for (const m of src.matchAll(/\$\('([^']+)'\)/g)) usedIds.add(m[1]);
}
const missingIds = [...usedIds].filter((id) => !htmlIds.has(id));
check(
  `every element id referenced from JS exists in index.html (${usedIds.size} ids)`,
  missingIds.length === 0,
  `missing: ${missingIds.join(', ')}`,
);

// The chat popup's data-part check is gone with the popup. It looked its parts up
// by data-part rather than by id, and both the component (live.js) and the
// template it checked against were deleted with the host's conversation UI. The
// plugin surface has no equivalent: its iframes are created by plugin-host.js
// from the layout's rectangles, so there is no template to cross-check.

// --- 2. Every binding the frontend calls is exported by the Wails module -----
const exported = new Set([...appJs.matchAll(/export function (\w+)/g)].map((m) => m[1]));
const called = new Set();
for (const src of scannedJs) {
  for (const m of src.matchAll(/\bApp\.(\w+)\(/g)) called.add(m[1]);
}
const missingBindings = [...called].filter((n) => !exported.has(n));
check(
  `every App.* call is an exported binding (${called.size} calls, ${exported.size} exports)`,
  missingBindings.length === 0,
  `not exported: ${missingBindings.join(', ')}`,
);

// The .d.ts is generated, but it is committed and the frontend's call sites must
// agree with it. A binding added to Go but graded as returning nothing would make
// every `.then(applyLayout)` silently receive undefined - and the surface would
// stay laid out for the window region the app just stopped cropping.
const typed = new Map(
  [...appDts.matchAll(/export function (\w+)\(([^)]*)\)\s*:\s*Promise<([^>]+)>/g)]
    .map((m) => [m[1], m[3]]),
);
const untyped = [...called].filter((n) => !typed.has(n));
check(
  'every App.* call is declared in App.d.ts',
  untyped.length === 0,
  `not declared: ${untyped.join(', ')}`,
);
// The window calls whose result the frontend feeds to applyLayout must be typed
// as returning a layout, or the type and the code disagree.
const layoutReturning = ['DismissBubble', 'OpenPluginSurface', 'ClosePluginSurface', 'OpenChatPluginSurface', 'OpenTodoFromBubble'];
const badLayoutTypes = layoutReturning.filter((n) => typed.get(n) !== 'main.Layout');
check(
  'every window call the frontend applies as a layout is typed as one',
  badLayoutTypes.length === 0,
  badLayoutTypes.map((n) => `${n}: ${typed.get(n)}`).join(', '),
);
// The plugin surface's model, and only the models that still exist: the live
// overlay's LiveRect/LiveView and proactive speech's ProactiveView were deleted
// with the features, and asserting on a class nobody generates would fail for the
// right reason once and then be deleted for the wrong one.
check(
  'the plugin surface types exist in models.ts',
  ['PluginSurfaceView', 'PluginSurfaceRect'].every((n) => new RegExp(`class ${n}\\b`).test(modelsTs)),
  'a new model class is missing',
);
// The same rule for the plugin surface: Layout gains a geometry member and the
// generated model has to carry it, because the frontend places the surface's
// iframes from exactly these rectangles. Without the member the page would have
// no coordinates and the window would be cropped around nothing.
check(
  'the Layout model carries the plugin surface geometry',
  /pluginSurface\?: PluginSurfaceView;/.test(modelsTs),
  'Layout has no pluginSurface member',
);
// The ellipse flag travels with the geometry rather than being re-derived on the
// page, because the REGION is already elliptical for those blocks: a rounded
// rectangle drawn where the region is an ellipse would show the window's glass
// tint in the four corners. Losing the field would silently turn every elliptical
// surface into a rounded one with a tinted sliver.
check(
  'the surface rectangle model carries the ellipse flag',
  /ellipse:\s*boolean;/.test(modelsTs),
  'PluginSurfaceRect has no ellipse member',
);
// The transcript model is gone with the host's conversation, so there is no
// proactive flag left to assert on. What replaces it is the negative: a
// ChatMessageView class in the generated models would mean a host transcript
// survived the deletion.
check(
  'the host transcript model is gone with the host conversation',
  !/class ChatMessageView\b/.test(modelsTs),
  'ChatMessageView is still generated',
);

// --- 3. Events the frontend subscribes to are the ones Go emits -------------
// app_hotkey.go is in this list because it emits pet:visible: the event scan is
// only as good as the file list, and a file left out makes its events look like
// subscriptions nothing ever sends.
//
// app_plugin_market.go, app_chat_slot.go and app_plugin_migrate.go are here for
// the same reason, one step further: they are new files, and a new file that is
// not listed is a file whose events look like subscriptions nothing ever sends.
// app_live.go was in this list and is gone with the host's live overlay.
// app_settingswatch.go is here because pet:image is emitted from it: the watcher is
// what notices a picture chosen in the control center.
const goFiles = [
  'app_llm.go',
  'app.go',
  'app_bubble.go',
  'app_hotkey.go',
  'app_plugins.go',
  'app_plugin_market.go',
  'app_chat_slot.go',
  'app_plugin_migrate.go',
  'app_plugin_surface.go',
  'app_settingswatch.go',
  // app_pet_badge.go emits pet:badge (the line a plugin draws on the pet itself). Listed for the
  // reason above: a new file that emits events and is not here makes its event look like a
  // subscription nothing ever sends.
  'app_pet_badge.go',
].map((f) => fs.readFileSync(path.join(root, f), 'utf8'));
const goSource = goFiles.join('\n');
const emitted = new Set([...goSource.matchAll(/emitEvent\(\s*([A-Za-z_]\w*)/g)].map((m) => m[1]));
const emittedLiterals = new Set([...goSource.matchAll(/emitEvent\("([^"]+)"/g)].map((m) => m[1]));
// Resolve the event-name constants.
//
// The pattern is `event<Name>` rather than a fixed list: it used to match only
// `eventChat*`, so a constant introduced elsewhere (eventPetVisible) resolved to
// its Go identifier and the event looked like a subscription nothing ever sent.
const constants = new Map(
  [...goSource.matchAll(/(event[A-Z]\w*)\s*=\s*"([^"]+)"/g)].map((m) => [m[1], m[2]]),
);
const emittedNames = new Set([...emitted].map((n) => constants.get(n) || n));
for (const n of emittedLiterals) emittedNames.add(n);
const subscribed = new Set([...mainJs.matchAll(/EventsOn\('([^']+)'/g)].map((m) => m[1]));
// The plugin module subscribes too, and plugin-host.js is where the host's own
// event reaches the sandboxed frames. Leaving it out would make
// pet:plugin-event a subscription this scan never sees - which is exactly the
// direction that hides a real break: the frontend stops receiving plugin events
// and every assertion here still passes.
//
// The subscriptions that used to be collected from the two deleted conversation
// modules and the deleted plugin tab are gone with them: those modules listened
// for the host conversation's own frames, which no longer exist, and for the
// plugin tab's copy of the plugin event. The plugin event itself is still
// covered, by plugin-host.js - which is where the host's own event reaches the
// sandboxed frames.
const subscribedInPluginHost = new Set([...pluginHostJs.matchAll(/EventsOn\('([^']+)'/g)].map((m) => m[1]));
for (const n of subscribedInPluginHost) subscribed.add(n);
const dangling = [...subscribed].filter((n) => !emittedNames.has(n) && n !== 'pet:layout' && n !== 'pet:pomodoro');
check(
  `every subscribed event is emitted by Go (${subscribed.size} subscriptions)`,
  dangling.length === 0,
  `never emitted: ${dangling.join(', ')}`,
);
// The names the pet's page still speaks. Each is checked as a PAIR - emitted by Go
// and subscribed by the page - because either half alone is the failure mode that
// matters: an event nobody sends leaves a dead listener, and one nobody listens to
// leaves a feature that does nothing.
//
// TWO, not three. `pet:chat-open` used to be here, emitted by app_llm.go's OpenChat to
// tell the panel which tab to show. OpenChat went with the panel, so nothing emits it -
// and the page's listener for it is reduced to "re-read the layout", which the
// subscription scan above still covers as a dangling name. That scan is deliberately
// name-agnostic now: it allows pet:layout and pet:pomodoro through because they are
// frontend-side names, and a third allowance for an event NOTHING emits would be a
// check that has been taught to ignore its own failure mode.
check(
  'the plugin and layout events are both emitted and subscribed',
  ['pet:plugin-event', 'pet:layout'].every((n) => emittedNames.has(n) && subscribed.has(n)),
  `emitted=${[...emittedNames].join(',')} subscribed=${[...subscribed].join(',')}`,
);

// --- 4. The new CSS obeys the no-decoration-outside-the-box rule ------------
// Only the sections this feature added are checked. The pre-existing panel and
// toast rules are already covered by the pixel verification, and the toast's
// own shadow is a deliberate, previously-verified exception.
//
// Every banner added by a change gets an entry here, which is also what keeps the
// rule below total: a section nobody listed would be a section nobody checked.
const marketCss = fs.readFileSync(
  path.join(root, 'tools/plugin-market/frontend/src/style.css'),
  'utf8',
);
// Every banner added by a change gets an entry here, which is also what keeps the
// rule below total: a section nobody listed would be a section nobody checked.
//
// Each entry names the stylesheet it belongs to, because there are two frontends
// now. The market's `/* --- plugin market` banner is the top of ITS stylesheet,
// so resolving it against the pet's would silently produce an empty section and
// the check would pass while checking nothing.
const newSections = [
  // '/* --- settings: compact controls' was removed with the settings page itself
  // (openspec/changes/ui-rebuild 阶段 3): the section is gone from the stylesheet, and a
  // marker for a section nobody has is a check that can only fail or lie.
  // The voice studio's section is gone with the studio: speech is a plugin now, and
  // its workshop stylesheet belongs to the PLUGIN, not to the pet's page. The plugin's
  // own stylesheet is checked in section 16 instead, where its document and script are
  // checked together - a banner list here would be checking the wrong file.
  { marker: '/* --- plugin surface', source: css },
  { marker: '/* --- plugin market', source: marketCss },
].map(({ marker, source }) => {
  const start = source.indexOf(marker);
  if (start < 0) return '';
  // Each section ends at the next top-level section banner.
  const next = source.indexOf('/* ---', start + marker.length);
  return source.slice(start, next < 0 ? undefined : next);
});
check(
  `all ${newSections.length} checked CSS sections are present`,
  newSections.every((s) => s.length > 0),
  'a section banner is missing',
);
const forbidden = [];
for (const section of newSections) {
  for (const m of section.matchAll(/^\s*(box-shadow|filter|text-shadow|transform)\s*:/gm)) {
    forbidden.push(m[1]);
  }
}
check(
  'no box-shadow/filter/text-shadow/transform in the new CSS (they paint onto the desktop)',
  forbidden.length === 0,
  `found: ${forbidden.join(', ')}`,
);
// The two banners this change owns, each in exactly one stylesheet. A duplicate
// would mean the pet had grown a copy of the market's section (or the reverse),
// and a missing one would be caught above only as an empty string.
check(
  'each new section banner appears in exactly one stylesheet',
  (css.match(/\/\* --- plugin surface/g) || []).length === 1 &&
    (marketCss.match(/\/\* --- plugin market/g) || []).length === 1,
  'a section banner is duplicated or missing',
);
// The banner the deleted plugin tab owned is gone. `plugins tab` styled a tab
// inside the pet's panel, and that tab - along with the stylesheet section - was
// removed when plugin management moved into the market program.
check(
  'the deleted plugin tab has no banner left behind',
  !css.includes('/* --- plugins tab'),
  'a banner for the deleted plugin tab survived',
);

// --- 4a. The window region is RECTANGLES ONLY -------------------------------
// It used to carry the panel's corner radius and the puffs' ellipse and hand them to
// CreateRoundRectRgn / CreateEllipticRgn. A GDI region is a 1-bit mask, so the OS drew a
// staircase over the browser's anti-aliased border-radius - every rounded corner in the app
// was visibly jagged. The shape belongs to the page now, and the region only says which
// boxes the window covers.
//
// This is checked because it is the kind of thing a well-meaning fix puts back: "the corner
// shows the desktop, so let us crop it to a round rect". The answer is CSS, not a region.
const winDesktop = fs.readFileSync(path.join(root, 'internal/desktop/desktop_windows.go'), 'utf8');
// Comments are stripped first: the file's own doc explains WHICH calls were removed and why,
// and naming them there is the point. The check is about what the code still does.
const winDesktopCode = winDesktop
  .replace(/\/\*[\s\S]*?\*\//g, '')
  .replace(/^\s*\/\/.*$/gm, '');
check(
  'the window region is built from plain rectangles',
  /procCreateRectRgn\s*=\s*gdi32\.NewProc\("CreateRectRgn"\)/.test(winDesktopCode) &&
    !/CreateRoundRectRgn/.test(winDesktopCode) &&
    !/CreateEllipticRgn/.test(winDesktopCode) &&
    /func SetWindowRegion\(w Window, rects \.\.\.Bounds\) error/.test(winDesktopCode) &&
    !/desktop\.RegionRect/.test(mainGo) &&
    !/Radius:\s*radius/.test(mainGo),
  'the region carries a curve again: rounded/elliptical regions are what made the corners jagged',
);

// The pet's own menu is a CARD IN THE PAGE, and this pins the whole chain:
// right-click -> App.OpenPetMenu -> a BubbleMenu card -> the grid in #bubble-menu ->
// one delegated click listener -> PET_MENU_ACTIONS.
//
// It used to be the tray's native popup, forwarded to the tray thread, and that is
// exactly what it looked like: right-clicking the pet and right-clicking the tray
// icon produced the same grey column of system text. The pet owns the gesture now.
//
// The card is a BUBBLE KIND, not a fourth placeholder in the region's slot: it
// borrows the bubble's rectangle, its crop and the rule that anything else opening
// in the slot folds it away (AGENTS.md 3.2). That is why there is no new geometry to
// check here - app_pet_menu.go and app_pet_menu_test.go own that half, and the
// region switch in app.go was deliberately left alone.
const goTray = fs.readFileSync(path.join(root, 'internal/desktop/tray_windows.go'), 'utf8');
const goPetMenu = fs.readFileSync(path.join(root, 'app_pet_menu.go'), 'utf8');
check(
  'right-clicking the pet opens the pet\'s own menu card, end to end',
  /els\.pet\.addEventListener\('contextmenu'/.test(mainJs) &&
    /function togglePetMenu\(\)/.test(mainJs) &&
    /App\.OpenPetMenu\(\)\.then\(applyLayout\)/.test(mainJs) &&
    /els\.bubbleMenu\.addEventListener\('click'/.test(mainJs) &&
    /const PET_MENU_ACTIONS = \{/.test(mainJs) &&
    htmlIds.has('bubble-menu') &&
    /func \(a \*App\) OpenPetMenu\(\) \(Layout, error\) \{/.test(goPetMenu) &&
    /BubbleView\{Kind: BubbleMenu\}/.test(goPetMenu),
  'the pet menu is not wired end to end (page -> binding -> BubbleMenu card -> grid -> action map)',
);
// ...and the tray no longer lends it out. A leftover forward would be a second way
// for the pet to ask for the tray's list, i.e. the bug this change fixed.
//
// Comments are stripped first, for the same reason the region check above strips
// them: the tray file's own doc names the message and the method that were REMOVED
// ("wmShowPetMenu is deliberately not defined any more"), and naming them there is
// the point. This check is about what the code still does.
const stripGoComments = (src) =>
  src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
check(
  'the pet no longer borrows the tray menu',
  !/wmShowPetMenu/.test(stripGoComments(goTray)) &&
    !/ShowMenuAtCursor/.test(stripGoComments(goTray)) &&
    !/ShowMenuAtCursor/.test(stripGoComments(mainGo)) &&
    !/OpenPetMenu\(\) error \{/.test(stripGoComments(mainGo)),
  'the pet menu still goes through the tray thread',
);
// The menu is a bubble KIND, so it must be on the list every transition folds away:
// a kind missing from clearAllBubblesLocked survives a focus starting and leaves the
// window cropped around a launcher nobody asked for any more.
// Every cell in the markup needs an action, and every action a cell: a cell whose
// button quietly does nothing is the one failure a menu cannot survive, because the
// click IS the interface.
const menuCells = [...html.matchAll(/data-menu="([a-z]+)"/g)].map((m) => m[1]);
const menuMapStart = mainJs.indexOf('const PET_MENU_ACTIONS = {');
const menuMap = mainJs.slice(menuMapStart, mainJs.indexOf('\n};', menuMapStart));
check(
  'every menu cell has an action, and every action a cell',
  menuCells.length === 9 &&
    menuCells.every((name) => new RegExp(`\\n\\s{2}${name}: \\(\\)`).test(menuMap)) &&
    (menuMap.match(/\n {2}[a-z]+: \(\)/g) || []).length === menuCells.length,
  'a menu cell and PET_MENU_ACTIONS disagree: ' + JSON.stringify(menuCells),
);
// The grid fills the card Go sizes - the rows are fractions, not a pixel height of
// their own. A grid that declared its own height would either come up short (a band
// of empty card) or run over (clipped by the region, which reads as a torn edge).
check(
  'the menu grid fills the card it is drawn in',
  /\.bubble-menu\s*\{[\s\S]{0,400}?display: grid;/.test(css) &&
    /\.bubble-menu\s*\{[\s\S]{0,400}?grid-template-columns: repeat\(3, 1fr\);/.test(css) &&
    /\.bubble-menu\s*\{[\s\S]{0,400}?grid-auto-rows: 1fr;/.test(css) &&
    !/\.bubble-menu\s*\{[\s\S]{0,400}?height: \d+px/.test(css),
  'the menu grid declares a size of its own instead of filling the bubble box',
);


// --- 4b. The settings page is GONE, and must stay gone ----------------------
//
// This block used to hold ten checks: four named groups, a heading per group, the set of
// sub-headings, the absence of any AI configuration, one shared row shape, one label
// column, the scroll rule, and a 33-id whitelist of every control the page carried.
//
// They are deleted, not loosened, because the page they described no longer exists
// (openspec/changes/ui-rebuild 阶段 3): 设置 moved to the control center, and the controls
// that could not move went to the pomodoro strip (放弃) or the tray menu (打开控制中心 /
// 立刻提醒 / 打开数据目录 / 退出). Keeping them would have meant one of two lies - an empty
// page that passes, or a whitelist nobody can satisfy.
//
// What replaces them is the ABSENCE check, which is the failure mode that still matters:
// a half-restored tab would be a SECOND writer of settings.json, and "one file, one
// writer" is the property the settings split exists to hold.
check(
  'the pet has no settings tab any more',
  !html.includes('id="tab-settings"') && !/data-tab="settings"/.test(html),
  'the settings tab came back: settings.json would have two writers again',
);
check(
  'the pet has no records tab any more',
  !html.includes('id="tab-records"') && !/data-tab="records"/.test(html),
  'the records tab came back: the archive belongs to the pomodoro plugin now',
);
// The archive ITSELF is gone too, and that is a second, later change: it used to be
// folded into the 待办 tab when its own tab retired, and it left with the host timer
// (openspec/changes/todo-pomodoro-as-plugins). Section 13 below asserts the absence of
// every id the host timer used to paint into; there is deliberately no "the archive
// still exists" check here any more, because keeping one would mean keeping the
// renderer, the store and the six bindings that fed it.
// The way into the control center used to be a button on the settings page, and it was the
// ONLY one - losing it would leave the user with a pet and no way to reach settings. It then
// became a tray-menu item ALONE, which is not an entry point but a rumour: the tray icon can
// be folded into the overflow area, and nothing on screen says it exists. The gear in the
// panel header was added for that reason, and the panel is gone, so the on-screen entry is
// the pet's own menu again - TWO cells of it (插件 and 设置), which is two ways in that are
// both visible the moment the user right-clicks the pet.
//
// All three are checked: the two menu cells and the tray item. The tray alone would be the
// rumour this comment started with.
check(
  'the pet offers a way into the control center, from its menu AND the tray',
  // Inline reads rather than shared consts: each file is used once, and a const declared
  // further down would be a temporal-dead-zone crash, not a check.
  /plugins: \(\) => launchControlCenter\(\)/.test(mainJs) &&
    /settings: \(\) => launchControlCenter\(\)/.test(mainJs) &&
    /function launchControlCenter\(\)[\s\S]{0,200}App\.OpenPluginMarket\(\)/.test(mainJs) &&
    /TrayCmdOpenMarket\s*=\s*trayCmdOpenMarket/.test(
      fs.readFileSync(path.join(root, 'internal/desktop/tray_windows.go'), 'utf8'),
    ) &&
    /desktop\.TrayCmdOpenMarket,/.test(mainGo) &&
    /case desktop\.TrayCmdOpenMarket:/.test(mainGo),
  'a way into the control center went missing: the two menu cells or the tray item',
);

// --- 5. THERE IS NO PANEL, and it must stay gone ----------------------------
//
// This block used to hold thirteen checks about the panel: its two tabs, the 对话 tab's
// mount point, the slot's frame being rebuilt on a version change, the watcher that kept
// the tab in step with the installed list, and the two events that moved the panel between
// tabs. All of it described a 380x520 column that is no longer in the markup
// (openspec/changes/todo-pomodoro-as-plugins, stage 3): 待办 became the todo plugin's
// floating surface and 对话 the chat plugin's own three blocks.
//
// So the checks are DELETED rather than loosened, and what replaces them is the ABSENCE
// check - which is the failure mode that still matters. Half a panel is worse than none: an
// `els` entry pointing at a section nothing draws is a null that fails at the first click,
// and a tab strip with one tab is a strip that has to be kept working for no reason.
check(
  'the panel is gone from the markup',
  ['panel', 'tab-todos', 'tab-chat', 'tab-chat-btn', 'chat-slot', 'close-panel', 'open-settings']
    .every((id) => !htmlIds.has(id)) && !/data-tab="/.test(html),
  'a panel element or a tab strip is back in index.html',
);
check(
  'main.js renders no panel and no tab',
  // The binding scan above already proves no dead App.* call survives; this is the other
  // half. A renderer touching els.panel or a `switchTab` would throw on the null element at
  // the first layout push, which is worse than the feature being absent.
  !/els\.panel|els\.tabs|els\.tabChat|els\.chatSlot|switchTab|renderChatSlot|setChatTabWatch/
    .test(mainJs) &&
    !/layout\.panelOpen/.test(mainJs),
  'main.js still drives a panel the page no longer has',
);
check(
  'the stylesheet carries no panel rules',
  // A stylesheet rule for a section nobody draws is a rule that can silently win a cascade
  // later, and `#panel` was the widest surface in this file.
  !/#panel\s*\{/.test(css) && !/#panel\s/.test(css) && !/\.panel-head|\.tab-body|\.chat-slot\s*\{/
    .test(css),
  'a stylesheet rule outlived the panel it styled',
);
check(
  'the conversation is a floating surface, and its frame survives a re-render',
  // What the panel's slot used to guarantee - "an updated plugin gets a fresh document, an
  // unchanged one keeps the running one" - is now the floating surface's job. The version
  // belongs in the comparison for the reason it always did: an update keeps the plugin id
  // and replaces the files behind it, so an id-only reuse test leaves the PREVIOUS version's
  // document on screen while the market says "updated".
  /this\.surfaceVersion !== \(view\?\.version \|\| ''\)/.test(pluginHostJs) &&
    /frame\.dataset\.pluginVersion = this\.version/.test(pluginHostJs) &&
    !/this\.slotFrame/.test(pluginHostJs),
  'the surface frames are not version-aware, or a panel slot frame came back',
);
check(
  'the pet has a way into the control center WITHOUT the panel',
  // The gear in the panel header was one of the two entries, and the panel is gone. The tray
  // item and the pet's own two menu cells (插件 / 设置) are what is left, so all three are
  // pinned: losing the header button without these would leave a pet with no way in at all.
  /TrayCmdOpenMarket\s*=\s*trayCmdOpenMarket/.test(
    fs.readFileSync(path.join(root, 'internal/desktop/tray_windows.go'), 'utf8'),
  ) &&
    /desktop\.TrayCmdOpenMarket,/.test(mainGo) &&
    /case desktop\.TrayCmdOpenMarket:/.test(mainGo) &&
    /plugins: \(\) => launchControlCenter\(\)/.test(mainJs) &&
    /settings: \(\) => launchControlCenter\(\)/.test(mainJs),
  'a way into the control center went missing: the tray item or the two menu cells',
);
// --- 6. The conversation button: hover target inside the pet's box ----------
//
// The double click on the pet is gone. The conversation entry is a BUTTON the page
// paints over the pet while the pointer is on it, and three things have to hold or
// the gesture is invisible, unclickable, or in the wrong place:
//
//   1. the button is INSIDE #pet in the markup. The window is cropped to the pet's
//      rectangle while nothing else is up (AGENTS.md 3.2), so a button hanging off
//      the pet's edge would be sliced by the native region - and growing the region
//      to fit it would expose a strip beside the pet that the page never paints;
//   2. it is hidden AND `pointer-events: none` until :hover. A transparent button
//      that still takes clicks would make the pet's upper-left corner open the
//      conversation instead of the todo panel - a silent change to what clicking
//      the pet does;
//   3. a click asks Go for the slot plugin's surface, which is the call the double
//      click used to make.
const petChatAt = html.indexOf('id="pet-chat"');
check(
  'the conversation button lives inside the pet box',
  petChatAt > html.indexOf('id="pet"') && petChatAt < html.indexOf('id="bubble"'),
  'the button is outside #pet, where the window region does not reach',
);
check(
  'the pet wires the button to the conversation',
  /chatButton: \$\('pet-chat'\)/.test(mainJs) &&
    /els\.chatButton\.addEventListener\('click', \(\) => openChatPluginSurface\(\)\)/.test(mainJs),
  'the button is not wired to the plugin surface',
);
// The press has to stop at the button: the pet's pointerdown starts a drag and
// calls preventDefault, which would both book a phantom drag origin and eat the
// click that follows.
check(
  'pressing the button does not start a drag on the pet',
  /els\.chatButton\.addEventListener\('pointerdown', \(e\) => e\.stopPropagation\(\)\)/.test(mainJs),
  'the button press reaches the pet, so it starts a drag and swallows its own click',
);
// Hidden means inert. Both declarations are checked, in the button's own rule, so a
// neighbouring rule cannot satisfy the assertion.
const petChatRule = (() => {
  const start = css.indexOf('.pet-chat {');
  const end = start < 0 ? -1 : css.indexOf('}', start);
  return start < 0 || end < 0 ? '' : css.slice(start, end);
})();
check(
  'the button is transparent and inert until the pointer is on the pet',
  /visibility:\s*hidden/.test(petChatRule) && /pointer-events:\s*none/.test(petChatRule),
  'a transparent button still takes the click, so the pet\'s corner would open the chat',
);
check(
  'hovering the pet is what reveals it',
  // The second half of this used to be "#pet.anchored hides it again": while the PANEL was
  // open the pet was its anchor rather than a button, so a second conversation entry there
  // would have opened a surface over the window the user was already reading. The panel is
  // gone and nothing sets `anchored` any more, so the rule was deleted with it - the hover
  // reveal is the whole of the gesture now.
  /#pet:hover \.pet-chat[^{]*\{[^}]*pointer-events:\s*auto/.test(css) && !/\.anchored/.test(css),
  'the button is not tied to the pet\'s hover state, or the anchored state came back',
);
// A click on the pet does NOTHING now, and that is the decision rather than an omission.
//
// It used to toggle the panel, and before that it was held for the system's double-click
// interval so two clicks would not unwind as open-then-close. The panel is gone: 待办 and
// 对话 are both plugin surfaces, reached from the pet's right-click menu and from the hover
// button, so there is no destination left for a bare click. What must NOT come back is a
// click that half-does something - a deferred click, or a click that opens one of the two
// surfaces silently.
check(
  'a single click on the pet does not open anything, and is not deferred',
  !/App\.TogglePanel|ToggledPanel/.test(mainJs) &&
    !/DOUBLE_CLICK_MS|pendingClick|lastClickAt/.test(mainJs) &&
    !/applyLayout\(await App\.(SetPanel|OpenChat)/.test(mainJs),
  'the single click opens a panel, or is still deferred for a double click',
);
// The click asks the conversation plugin for its floating surface, which is the
// whole point of the redesign: the pet's own live overlay is gone, and what opens
// is the plugin's window. The call goes through Go rather than straight to the
// surface binding, because the slot's owner has to be resolved from the on-disk
// install list (and refused when a question owns the slot).
check(
  'the button asks the conversation plugin for its surface',
  /function openChatPluginSurface\(\)[\s\S]{0,400}App\.OpenChatPluginSurface\(\)/.test(mainJs),
  'the button does not reach the plugin surface',
);

// --- 6b. The plugin surface obeys the region rules --------------------------
// The surface is drawn from the layout's rectangles, which ARE the window region.
// plugin-host.js must take every position and size from those numbers: computing
// one of its own would put the drawn frame outside the region Go cropped.
check(
  'plugin-host.js positions every surface frame from the layout',
  /pluginSurface/.test(pluginHostJs) && /rects/.test(pluginHostJs) &&
    /\.style\.(left|top|width|height)\s*=/.test(pluginHostJs),
  'the surface frames are not placed from the layout rectangles',
);
// The ellipse flag has to reach the CSS as a true circle rather than a rounded
// rectangle: the REGION is an ellipse for those blocks, so a rounded box would
// leave the window's glass tint visible in the four corners. Go zeroes `r` for
// these blocks precisely because a rounded rectangle and an ellipse are different
// shapes, so the page must branch on the flag rather than on the radius.
check(
  'an elliptical surface block is drawn as a true ellipse',
  // The receiver is deliberately not pinned: it was `r` while the rect loop was a
  // `forEach((r, i) => ...)`, and it became `rect` when the frames started being keyed by
  // declaration index. What must hold is that the page branches on the FLAG (not on `r`,
  // which Go zeroes for these blocks) and applies the result to all four corners.
  /\.ellipse === true \? '50%'/.test(pluginHostJs) &&
    /borderTopLeftRadius\s*=\s*radius/.test(pluginHostJs) &&
    /borderBottomRightRadius\s*=\s*radius/.test(pluginHostJs),
  'the ellipse flag does not reach the drawn shape',
);
// The frames are sandboxed. `allow-scripts` alone is the whole point: adding
// allow-same-origin would give a plugin's page the host's origin, and the
// postMessage protocol exists precisely so it does not need it. The assertion is
// on the setAttribute call rather than on the file, because the file legitimately
// MENTIONS allow-same-origin in the comments that explain why it is absent.
check(
  'the plugin frames are sandboxed to scripts only',
  /setAttribute\('sandbox',\s*'allow-scripts'\)/.test(pluginHostJs) &&
    !/setAttribute\('sandbox',[^)]*allow-same-origin/.test(pluginHostJs),
  'a plugin frame can reach the host origin',
);

// --- 7. The key input is never written back into the DOM --------------------
// The key field moved to the market program with the rest of the AI
// configuration, so the assertion reads THAT frontend now. The rule is unchanged
// and still load-bearing, but the market's field is stronger than the pet's was:
// it is a write-only input that is never populated from state at all, and an
// untouched one is OMITTED from the payload rather than sent as an empty
// replacement (which would erase a stored key on every unrelated save).
check(
  'the API key field is never populated from state',
  // The secret branch assigns '' unconditionally. Any other assignment to
  // input.value inside the secret case would be the value coming back.
  (() => {
    const start = marketMainJs.indexOf("case 'secret'");
    if (start < 0) return false;
    const branch = marketMainJs.slice(start, marketMainJs.indexOf('break;', start));
    return /input\.value = ''/.test(branch) && !/input\.value = [^']/.test(branch);
  })(),
  'the key may be written back into the form',
);
check(
  'an untouched key field is omitted rather than sent empty',
  /item\.type === 'secret'\) \{[\s\S]{0,220}if \(input\.value !== ''\) out\[item\.key\] = input\.value;/.test(
    marketMainJs,
  ),
  'saving an unrelated setting would erase the stored key',
);

// --- 9b. Nothing on this page can refuse a dismissal any more ----------------
//
// This block used to hold six checks about the two GATED prompts: the summary prompt
// parked the break until its record was answered and the plan prompt parked the focus,
// so Escape, a drag and the plugin surface all had to refuse to destroy them - and
// Enter had to answer them, because the card is not a <form>.
//
// Both prompts were the host timer's questions. The timer belongs to the pomodoro
// plugin now (openspec/changes/todo-pomodoro-as-plugins) and the plugin asks them in
// its own surface, whose keyboard and dismissal rules are its own business. So the six
// checks are DELETED rather than loosened: there is no host-side gate left to protect,
// and a check that kept naming one would only pass while the timer came back with it.
//
// What still matters is the other direction - that making the prompts stick never made
// EVERY bubble stick. Only a reminder may auto-hide (scheduleBubbleHide's guard), and
// every bubble stays dismissable.
check(
  'a reminder bubble is still dismissable',
  /if \(b\.kind !== 'reminder'\) return;/.test(mainJs) &&
    /function closePetMenu\(\) \{[\s\S]{0,120}App\.DismissBubble\(\)\.then\(applyLayout\)/.test(mainJs),
  'reminders no longer auto-hide, or the menu card cannot be dismissed',
);

// --- 9e. The global hide/show shortcuts --------------------------------------
//
// The whole feature is one promise: if the pet can be hidden, it can be brought
// back. The checks below are about the ways that promise can be broken - a
// missing way back, a registration that silently failed, or a key combination
// taken from the user's desktop without being asked.
const goHotkey = fs.readFileSync(path.join(root, 'app_hotkey.go'), 'utf8');
const goDesktopHotkey = fs.readFileSync(path.join(root, 'internal/desktop/hotkey.go'), 'utf8');
const goDesktopHotkeyWin = fs.readFileSync(path.join(root, 'internal/desktop/hotkey_windows.go'), 'utf8');

check(
  'the shortcuts are off by default',
  // A global hot key is taken from EVERY other application, so claiming one
  // unasked could break a shortcut in the user's editor.
  /GlobalHotkeys:\s*false/.test(fs.readFileSync(path.join(root, 'internal/store/store.go'), 'utf8')),
  'the pet claims a global shortcut without being asked',
);
check(
  'show is registered before hide',
  // RegisterHotKey fails per key, so the order is the safety rule: a failure must
  // leave the pet visible, never hide-only.
  /func DefaultHotkeys\(\) \[\]HotkeySpec \{\s*\n\s*return \[\]HotkeySpec\{DefaultShowHotkey, DefaultHideHotkey\}/.test(goDesktopHotkey),
  'hide could be registered without a working show key, leaving the pet unrecoverable',
);
check(
  'the shortcuts avoid the Ctrl+Shift combinations applications already use',
  // A global hot key is taken from EVERY application. Ctrl+Shift+S is "Save As"
  // in most editors and Ctrl+Shift+H is find-and-replace in several, so claiming
  // them breaks a shortcut in whatever the user actually works in - and when the
  // other program wins the race, the pet's own key silently fails instead.
  // Ctrl+Alt is the collision-free choice, and this pins it so a future edit
  // cannot quietly walk back into the crowded space.
  //
  // The assertion is scoped to the two DEFAULT declarations on purpose: the
  // ModShift identifier legitimately appears elsewhere in the file (the list of
  // modifiers the validator accepts), so a whole-file negative match would fail
  // for a reason that has nothing to do with the shortcuts.
  (() => {
    // The whole []HotkeyModifier{...} literal has to be captured, not just a
    // bracketed run: the slice type itself starts with "[]", so a naive [^\]]*
    // stops there and matches the empty slice instead. \s* absorbs the column
    // alignment gofmt inserts.
    const re = /Default(Show|Hide)Hotkey\s*=\s*HotkeySpec\{[^}]*?Modifiers:\s*(\[\]HotkeyModifier\{[^}]*\})[^}]*?Key:\s*"([SH])"/g;
    const seen = {};
    let m;
    while ((m = re.exec(goDesktopHotkey)) !== null) {
      seen[m[1]] = { mods: m[2].replace(/\s+/g, ' '), key: m[3] };
    }
    if (!seen.Show || !seen.Hide) return false;
    const want = '[]HotkeyModifier{ModCtrl, ModAlt}';
    return seen.Show.mods === want && seen.Show.key === 'S' &&
      seen.Hide.mods === want && seen.Hide.key === 'H';
  })(),
  'the shortcuts use Ctrl+Shift, which collides with common application shortcuts',
);
check(
  'a failed registration stops the chain rather than continuing',
  // Registering a later key after an earlier one failed would break the ordering
  // the safety rule depends on.
  /view\.Problems = append\(view\.Problems, fmtHotkeyProblem\(s, "已被其他程序占用，或系统不允许"\)\)[\s\S]{0,120}break/.test(goDesktopHotkeyWin),
  'a failed hot key does not stop the chain, so a hide key can outlive the show key',
);
check(
  'the shortcut is released when the feature is switched off',
  // Otherwise the combinations stay stolen until the app exits.
  /mgr\.Register\(nil\)/.test(goHotkey),
  'switching the feature off does not give the keys back',
);
check(
  'the shortcuts are released on shutdown',
  // A hot key owned by a dead process stays reserved until Windows notices.
  /a\.closeHotkeys\(\)/.test(fs.readFileSync(path.join(root, 'app_reminder.go'), 'utf8')) &&
    /func \(a \*App\) closeHotkeys\(\)/.test(goHotkey),
  'quitting leaves the key combinations reserved',
);
check(
  'the tray offers both directions as a mouse fallback',
  // The hot keys CAN fail, and the tray is then the only way back. A "hide" with
  // no working way to "show" is the one unrecoverable state this feature has.
  /TrayCmdShowPet, "显示桌宠"/.test(mainGo) &&
    /TrayCmdHidePet, "隐藏桌宠"/.test(mainGo) &&
    /case desktop\.TrayCmdShowPet:/.test(mainGo) &&
    /case desktop\.TrayCmdHidePet:/.test(mainGo),
  'the tray cannot show a pet that its own hide entry put away',
);
check(
  'a real window call hides the pet rather than a CSS trick',
  // Hiding has to mean the pixels are gone, including from Alt+Tab and from a
  // desktop screenshot; a moved or transparent window still occupies the screen.
  /func Hide\(w Window\) error \{\s*\n\s*procShowWindow\.Call\(uintptr\(w\), uintptr\(swHide\)\)/.test(
    fs.readFileSync(path.join(root, 'internal/desktop/desktop_windows.go'), 'utf8'),
  ),
  'hiding does not actually hide the window',
);
check(
  'the hidden state is runtime-only, not persisted',
  // A saved "hidden" would have to be undone by a shortcut that may be
  // unavailable (another program can own the combination). Every launch starts
  // visible, which is the backstop that cannot fail. So: the App carries the
  // flag, and the Config does NOT.
  /hidden bool/.test(mainGo) &&
    !/Hidden\s+bool\s+`json:/.test(fs.readFileSync(path.join(root, 'internal/store/store.go'), 'utf8')),
  'the hidden state is written to the config, so a restart can come back hidden',
);
// The shortcut switch lives in the CONTROL CENTER now (开始 → 外观与启动), and the pet is
// what applies it. Three guarantees, kept but pointed at the new homes:
//
//   1. the switch is actually sent somewhere (a checkbox nobody saves is a lie);
//   2. the labels shown are the ones internal/desktop registers, not a copy in a page;
//   3. a key the OS REFUSED still reaches the user - which used to be a red row on the
//      settings page, and is now a toast in the pet (main.js, applyState). "I pressed it
//      and nothing happened" is the failure all three exist to prevent.
const ccBridge = fs.readFileSync(path.join(root, 'tools/plugin-market/cc/src/lib/bridge.ts'), 'utf8');
const ccStart = fs.readFileSync(path.join(root, 'tools/plugin-market/cc/src/pages/Start.svelte'), 'utf8');
const marketPetAPI = fs.readFileSync(path.join(root, 'tools/plugin-market/settingsapi_pet.go'), 'utf8');
check(
  'the shortcut switch is wired from the control center',
  /SetGlobalHotkeys\(/.test(ccStart) &&
    /host\.SetGlobalHotkeys\(/.test(ccBridge) &&
    /func \(m \*Market\) SetGlobalHotkeys/.test(marketPetAPI),
  'the switch is rendered in the control center but never sent, or the binding is gone',
);
check(
  'the key labels come from Go rather than being written in the control center',
  // Same value advertised and registered, or the page can tell the user to press something
  // that was never registered. `ConfiguredHotkeyView` is the only source of those strings.
  /HotkeyLabels\(\)/.test(ccStart) &&
    /host\.HotkeyLabels\(\)/.test(ccBridge) &&
    /desktop\.ConfiguredHotkeyView/.test(marketPetAPI) &&
    /Hotkeys\s+desktop\.HotkeyView/.test(mainGo),
  'the shortcuts are hard-coded somewhere, so a page can disagree with what was registered',
);
check(
  'a refused registration still reaches the user',
  // The settings page used to show a red row. It is gone, so the pet says it once as a
  // toast the moment Go reports it - which is only honest if the problems field survives.
  /next\.hotkeys\?\.problems\?\.length/.test(mainJs) &&
    /快捷键没生效/.test(mainJs) &&
    /Problems \[\]string `json:"problems,omitempty"`/.test(goDesktopHotkey),
  'a hot key the OS refused is reported nowhere the user can see',
);
check(
  'reminders are suppressed while the pet is hidden',
  // A bubble is painted inside the window, so on a hidden pet it is a
  // notification nobody can see. The question reveal is the ONE exception, and
  // it is deliberate: a gate that parks the timer is not a notification.
  /if a\.petHidden\(\) \{[\s\S]{0,160}这次提醒没有弹：桌宠是隐藏的/.test(
    fs.readFileSync(path.join(root, 'app_reminder.go'), 'utf8'),
  ) &&
    !/revealForQuestion/.test(fs.readFileSync(path.join(root, 'app_reminder.go'), 'utf8')),
  'a reminder can fire at a hidden pet, invisibly',
);
// The "the pet does not talk to itself while hidden" check is gone with the
// feature it guarded. It read app_live.go's petHidden branch, and both the file
// and the branch were deleted with the host's live overlay: the pet no longer
// speaks unprompted at all, so there is no path that could talk to a hidden
// window. A plugin that wants to speak on its own does so through pet.bubble,
// which the reminder path already suppresses while hidden - the check above.

// --- 10. The fixed voice is wired ------------------------------------------
//
// Speech LEFT the pet. It is a plugin now (com.shixiansi.plugin-voice), so what the
// pet must be shown to have is not a settings form but the two GENERIC mechanisms
// the plugin runs on, plus the absence of everything that used to be here.
//
// The absence is the load-bearing half. Leaving a voice switch, an audition button
// or a studio launcher behind would give one feature two places to configure it, and
// the stale copy would be the one that looks like it works while changing nothing -
// because the pet no longer reads those values. Same for the bindings: a surviving
// export is callable JavaScript for a feature that does not exist.
check(
  'the pet settings carry no speech controls at all',
  // The id scan is on the markup, and the CALL scan is on `App.<name>(` rather than
  // on the bare name: the comment that explains what was removed necessarily NAMES
  // the retired bindings, and matching the words alone would make the explanation and
  // the guarantee mutually exclusive.
  !['voice-enabled', 'voice-pomodoro', 'voice-reminder', 'voice-test', 'voice-stop',
    'voice-status', 'voice-open-studio', 'voice-active-role', 'voice-builtin-note',
    'voice-replaced-note'].some((id) => htmlIds.has(id)) &&
    !/App\.(SaveVoiceSettings|TestVoice|StopVoice|OpenVoiceStudio|VoiceLibraryDir|StudioPath|RevealVoiceLibrary)\(/.test(mainJs) &&
    !/els\.voice\w+/.test(mainJs),
  'a speech control survived in the panel, so one feature has two places to configure it',
);
check(
  'no retired speech binding is exported',
  !/SaveVoiceSettings|TestVoice|StopVoice|OpenVoiceStudio|StudioPath|VoiceLibraryDir|RevealVoiceLibrary/.test(appDts) &&
    !['SaveVoiceSettings', 'TestVoice', 'StopVoice', 'OpenVoiceStudio', 'StudioPath',
      'VoiceLibraryDir', 'RevealVoiceLibrary'].some((m) =>
      new RegExp(`export function ${m}\\(`).test(appJs)),
  'a retired binding is still exported, so dead JavaScript can call it',
);
check(
  'the pet has no speech fields in its own config',
  !/VoiceEnabled|VoicePomodoro|VoiceReminder/.test(
    fs.readFileSync(path.join(root, 'internal/store/store.go'), 'utf8')),
  'the pet still persists speech switches it never reads',
);

// --- 11. The plugin runs on two generic mechanisms ---------------------------
//
// The pet kept the part that is genuinely the pet's: it emits facts, and it stores
// and plays audio on a plugin's behalf. Everything else - the lines, the voice, the
// switches, when to speak - belongs to the plugin.
check(
  'the audio capability exists and is permission-gated',
  /"audio\.save"/.test(goAppPlugins) && /"audio\.play"/.test(goAppPlugins) &&
    /PermAudio/.test(goAppPlugins),
  'the plugin has no way to store or play the audio it renders',
);
check(
  'the permission vocabulary is the expected thirteen, and by `net.ws` + `llm.borrow` + `process` + `net.credentials` + `plugin.share`',
  // The count is asserted so that adding a permission is a VISIBLE two-place edit here,
  // and so that the "no background permission" decision cannot be quietly undone.
  //
  // Thirteen, not fourteen: a headless frame is a manifest FIELD gated on an existing
  // permission (`events` for host events, `net.ws` for a socket a plugin holds open,
  // `net.fetch` for a polling plugin) rather than a permission of its own. A permission
  // with no method behind it would be a switch the user is asked about that does
  // nothing, which is the invariant the pet's own TestCapabilityTableIsComplete
  // enforces. The negative assertions below are the same decision stated from the other
  // side.
  //
  // The five additions each have a METHOD behind them: `net.ws` gates ws.open/send/close/
  // state, `llm.borrow` gates llm.profile (which is what tells a plugin's own UI where
  // its model settings come from), `process` gates process.launch (which starts one
  // .exe the user typed the path of, and does not wait for it), `net.credentials` is
  // the SECOND gate of net.fetch (recorded in that entry's `also`, which the pet's
  // completeness test counts), and `plugin.share` gates share.set/get/remove - the tray
  // two plugins use to hand each other a job without either one calling the other.
  //
  // Fifteen rather than seventeen because `timer.read` and `timer.control` retired with
  // timer.state/start/pause/resume/stop: the pomodoro plugin keeps its own state machine
  // and records now, so the host has no plugin-facing timer left to watch or to drive.
  //
  // Thirteen rather than fifteen because `todos.read` / `todos.write` retired with the
  // five `todos.*` methods: the list is the todo plugin's own storage, the pomodoro reads
  // the snapshot published through `plugin.share`, and the chat plugin's three todo tools
  // were deleted rather than re-pointed. The four retired words are asserted ABSENT - the
  // count alone would not say which entries left.
  (() => {
    const list = capsGo.slice(capsGo.indexOf('var AllowedPermissions'), capsGo.indexOf('func IsAllowedPermission'));
    const entries = [...list.matchAll(/Perm[A-Za-z]+,/g)];
    return entries.length === 13 && /PermAudio,/.test(list) && /PermNetWS,/.test(list) &&
      /PermLLMBorrow,/.test(list) && /PermProcess,/.test(list) &&
      /PermNetCredentials,/.test(list) && /PermPluginShare,/.test(list) &&
      !/PermTimer(Read|Control)/.test(capsGo) &&
      !/PermTodos(Read|Write)/.test(capsGo) &&
      !/PermBackground/.test(capsGo);
  })(),
  'the permission vocabulary is not the expected thirteen entries (net.ws + llm.borrow + process + net.credentials + plugin.share appended, timer.read/timer.control and todos.read/todos.write retired, background deliberately not)',
);
check(
  'net.credentials is only ever declared together with net.fetch',
  // It is the second gate of that capability, and the host refuses the manifest when it
  // is declared alone (validateCredentials). Stating it here as well turns a mistake
  // that would surface as "this plugin cannot be installed" into one that fails at
  // packaging time, next to the manifest that made it.
  (() => {
    const pluginsDir = path.join(root, 'market/plugins');
    const manifests = fs.readdirSync(pluginsDir)
      .map((entry) => path.join(pluginsDir, entry, 'manifest.json'))
      .filter((file) => fs.existsSync(file));
    for (const file of manifests) {
      const man = JSON.parse(fs.readFileSync(file, 'utf8'));
      const perms = man.permissions || [];
      if (perms.includes('net.credentials') && !perms.includes('net.fetch')) {
        console.log('    ' + man.id + ' declares net.credentials without net.fetch');
        return false;
      }
    }
    return true;
  })(),
  'a packaged plugin asks to carry credentials but not for the network, so the switch would unlock nothing',
);
check(
  'every permission a market plugin asks for exists in the host vocabulary',
  // The host REFUSES to parse a manifest that names an unknown permission, and the
  // market renders such a plugin as "manifest 无法解析" - so a typo in a package here is
  // a plugin that cannot be installed and an error message that blames the user's
  // install. The comparison is against the SAME const block the vocabulary is built
  // from, and it covers both places the list travels to: each package's own
  // manifest.json, and market/index.json (which tools/package.ps1 rewrites from the
  // package, and against which the installer compares what it extracted).
  //
  // A plugin asking for LESS than the host allows is fine - that is every plugin - so
  // this is a subset check, not an equality one.
  (() => {
    const known = new Set(
      [...capsGo
        .slice(capsGo.indexOf('const ('), capsGo.indexOf('// AllowedPermissions'))
        .matchAll(/Perm[A-Za-z]+\s*=\s*"([^"]+)"/g)].map((m) => m[1]),
    );
    if (known.size === 0) return false;
    const asking = [];
    const pluginsDir = path.join(root, 'market/plugins');
    const manifestFiles = fs.readdirSync(pluginsDir)
      .map((entry) => path.join(pluginsDir, entry, 'manifest.json'))
      .filter((file) => fs.existsSync(file));
    for (const file of manifestFiles) {
      const man = JSON.parse(fs.readFileSync(file, 'utf8'));
      for (const perm of man.permissions || []) asking.push([man.id, perm]);
    }
    const cataloguePath = path.join(root, 'market/index.json');
    if (fs.existsSync(cataloguePath)) {
      const catalogue = JSON.parse(fs.readFileSync(cataloguePath, 'utf8'));
      for (const row of catalogue.plugins || []) {
        for (const perm of row.permissions || []) asking.push([row.id, perm]);
      }
    }
    if (asking.length === 0) {
      console.log('    no market plugin declares a permission, so nothing was compared');
      return false;
    }
    const unknown = asking.filter(([, perm]) => !known.has(perm));
    if (unknown.length) {
      console.log('    unknown=' + JSON.stringify(unknown));
      return false;
    }
    return true;
  })(),
  'a market plugin asks for a permission the host would refuse, which makes it uninstallable',
);
check(
  'the headless frame is mounted and hidden, and only for an eligible plugin',
  // The mount point is in the markup and hidden; the selection logic requires all three
  // of enabled + declared background + a permission that fills a frame. This is checked
  // STATICALLY here and BEHAVIOURALLY in the harness: the static half catches a deleted
  // mount point or a selector that dropped one of the gates, which the harness cannot see
  // if its fixture happens to satisfy them anyway.
  //
  // All THREE permissions are asserted, including `net.fetch`: a polling plugin is the one
  // kind whose frame does nothing the user can see, so dropping its branch would leave it
  // mounted in the market's workshop and silent everywhere else - a plugin that looks
  // configured and never runs.
  htmlIds.has('plugin-background') &&
    /pluginBackground: \$\(.plugin-background.\)/.test(mainJs) &&
    /function backgroundPlugins\(\)/.test(mainJs) &&
    /p\.background !== true/.test(mainJs) &&
    /granted\.includes\('events'\)/.test(mainJs) &&
    /granted\.includes\('net\.ws'\)/.test(mainJs) &&
    /granted\.includes\('net\.fetch'\)/.test(mainJs),
  'the headless frame mount point or one of its eligibility gates is gone',
);
check(
  'audio is played from a FILE, never from memory',
  // winmm's SND_MEMORY form needs the caller's buffer to outlive the call with the GC
  // unaware of it; a collection mid-sentence produces silence or a crash roughly once
  // in a hundred runs. This is the same invariant the retired voice player had, kept
  // because the hazard travels with the API rather than with the feature.
  /PlayWAVFile\(path\)/.test(goAppPlugins) && !/SND_MEMORY/.test(goAppPlugins),
  'the audio path uses the in-memory form, which is unsafe with Go garbage collection',
);
check(
  'every audio file name is checked before it becomes a path',
  // A name becomes a file name in the plugin's private directory, so a separator or
  // a ".." would be a way out of it. Two validators cover the two kinds of name, and
  // each is asserted where it now LIVES rather than where it was first written:
  //   - audio slots are FLAT, so they are checked by flatNamePattern (and its alias
  //     audioNamePattern) in app_plugins.go;
  //   - storage keys may name FOLDERS, so they go through StorageKeyPath in
  //     internal/plugins/privatestore.go, which is path-aware.
  //
  // The storage half used to be the text `storageKeyPattern` in app_plugins.go. That
  // identifier was consolidated away when the private store moved into
  // internal/plugins, and this assertion then failed on EVERY run - a stale check
  // inside the gate whose whole job is catching stale wiring between page and host.
  // A permanently red check is worse than no check: it hides the next real failure.
  /var flatNamePattern = regexp\.MustCompile/.test(goAppPlugins) &&
    /audioNamePattern\.MatchString/.test(goAppPlugins) &&
    /func StorageKeyPath\(key string\)/.test(goPrivateStore) &&
    (goPrivateStore.match(/StorageKeyPath\(/g) || []).length >= 2,
  'an audio name reaches the filesystem unchecked',
);
check(
  'the two edge events are emitted as double-quoted literals',
  // The static scan below finds every subscription by this literal form; a constant
  // would read as an identifier and the subscription would be reported as dangling.
  /"timer\.phaseEnded"/.test(goAppPlugins) && /"reminder\.fired"/.test(goAppPlugins),
  'an edge event is emitted through a constant, so the subscription scan cannot see it',
);
check(
  'the edge events carry facts and no wording',
  // The host knows a phase ended; it does not know what to SAY about it. A text field
  // here would quietly move that decision back into the host, which is the whole
  // thing this change removed.
  /notifyPluginPhaseEnded/.test(goAppPlugins) &&
    !/notifyPluginPhaseEnded\([^)]*\btext\b/.test(goAppPlugins),
  'the phase event carries wording, so the host is deciding what to say again',
);
check(
  'a plugin can keep a headless frame, and only with a permission that fills one',
  // Without the frame the host drops an event with "no interface on screen", and both
  // plugins that need one act exactly when no interface IS open (the voice plugin hears
  // the pomodoro end; the chat bridge keeps a socket's frames flowing). The permission
  // pairing is what keeps the user shown what they are authorising.
  /openBackground/.test(goPluginHostJs) &&
    /backgroundFrame/.test(goPluginHostJs) &&
    /background/.test(fs.readFileSync(path.join(root, 'internal/plugins/manifest.go'), 'utf8')) &&
    /PermEvents/.test(goAppPlugins) && /PermNetWS/.test(goAppPlugins),
  'a plugin cannot receive events while no interface is open',
);
check(
  'every eligible background plugin gets its own headless frame',
  // One frame was enough while the voice plugin was the only case. The chat bridge is a
  // second, and the two cannot share a document: a single-slot implementation would take
  // the first plugin's frame away when the second was installed - silently, because the
  // frame it lost is invisible by construction.
  /this\.backgroundFrames = new Map\(\)/.test(goPluginHostJs) &&
    /syncBackground\(plugins\)/.test(goPluginHostJs) &&
    /syncBackground\(backgroundPlugins\(\)\)/.test(mainJs),
  'the host still keeps one background frame, so a second background plugin would evict the first',
);
check(
  'the headless frame is hidden, so it takes no space and no clicks',
  // An unpainted-but-present iframe would still occupy layout and could swallow a
  // click meant for the pet.
  /mount\.classList\.add\('hidden'\)/.test(goPluginHostJs),
  'the headless frame is visible, so it can cover the pet or steal its clicks',
);
check(
  'the dispatcher gate is shared, not repeated per program',
  // The order of the checks is a security decision; two implementations of it are two
  // places for it to drift. The market program hosts plugin interfaces too, so it uses
  // the same dispatcher and the same asset handler.
  //
  // The asset side is asserted on its DEFINITION (the exported function in the shared
  // package) plus the market's CALL of it, rather than on the shared file naming
  // itself - which it cannot, because that file IS the package.
  /func \(r \*Registry\) Dispatch/.test(goPluginsDispatch) &&
    /^func ServePluginAsset\(/m.test(goPluginsAsset) &&
    /plugins\.ServePluginAsset\(/.test(marketPluginHost),
  'the capability gate or the asset route exists in more than one place',
);

// --- 12. The legacy voice library is migrated, not abandoned -----------------
//
// The user built that library by spending minutes of CPU per line. The migration is
// the difference between "your voice moved" and "your voice is gone", so its three
// load-bearing properties are asserted here.
check(
  'the legacy voice library is migrated into the plugin',
  /func MigrateVoiceData/.test(goPluginsVoiceMigrate) &&
    /voice-user\.json/.test(goPluginsVoiceMigrate) &&
    /MigrateVoiceData/.test(mainGo),
  'the legacy library is never carried over, so the user\'s renders would be orphaned',
);
check(
  'the migration only READS its source',
  // These clips took minutes of GPU time EACH and need a running service to make
  // again, so the originals are the rollback path. Unlike the conversation migration,
  // nothing here is renamed away.
  !/os\.Remove|os\.Rename\(\s*source|RemoveAll/.test(goPluginsVoiceMigrate),
  'the migration moves or deletes the source, so a bad migration is unrecoverable',
);
check(
  'the migration never points a line at a file that is missing',
  // A chosen take whose clip is gone would play NOTHING, silently - which looks
  // exactly like the feature being broken.
  /copied\[/.test(goPluginsVoiceMigrate),
  'a chosen take may name a clip that was not copied',
);
check(
  'migration slot names satisfy the audio capability',
  // The name is the join between the file on disk and what audio.play accepts; a name
  // outside that set would migrate successfully and never make a sound.
  /func sanitiseSlot/.test(goPluginsVoiceMigrate) &&
    /audioNamePattern|\[A-Za-z0-9_-\]/.test(goPluginsVoiceMigrate + goAppPlugins),
  'a migrated clip name would be refused at play time',
);

// --- 13. The real-time synthesis path is gone --------------------------------
// Its removal was the point of an earlier change: it was slow (11s for a
// 54-character reply), heavy (3.3GB resident) and unstable (the upstream service
// documents its own output as random). These checks keep it from creeping back.
check(
  'no reply is read aloud',
  !/startSpeech|speechGated|eventChatReveal/.test(goAppLlm),
  'the reply path still wires real-time speech',
);
check(
  'nothing on the playback path synthesises',
  // The pet no longer synthesises at all - the plugin does, in its own process, on
  // the user's explicit request. What must stay true is that PLAYING a line never
  // reaches a synthesis client: a render takes seconds and speaking must not block.
  (() => {
    const playback = [
      goAppPlugins,
      fs.readFileSync(path.join(root, 'app_bubble.go'), 'utf8'),
      fs.readFileSync(path.join(root, 'app_reminder.go'), 'utf8'),
    ].join('\n');
    return !/internal\/tts/.test(playback) && !/\btts\./.test(playback);
  })(),
  'something on the playback path imports a synthesis client, so speaking can block on a render',
);
check(
  'the archived implementation is out of the build',
  // Go ignores directories starting with "_", which is what keeps retired code from
  // being compiled while still being readable.
  fs.existsSync(path.join(root, '_archive/tts-realtime')) &&
    !/_archive/.test(mainJs + goAppPlugins),
  'the archived real-time code is referenced again',
);
check(
  'the settings carry no synthesis fields',
  !/ttsBaseUrl|ttsCharacter|ttsGptWeights|ttsOnlineUrl/.test(
    fs.readFileSync(path.join(root, 'frontend/wailsjs/go/models.ts'), 'utf8') + mainJs),
  'a synthesis setting survived the removal',
);

// --- 13. The host timer's UI is GONE, and must stay gone --------------------
//
// This block used to hold nine checks about the records archive: the day grouping,
// the fold that survived a re-render, the plan/actual row, the editor that saved both
// lines in one write, and the two bindings that made the plan editable. All of it
// described the host's timer, and the timer is the pomodoro plugin's now
// (openspec/changes/todo-pomodoro-as-plugins): the plugin keeps its own sessions and
// draws them in its own surface.
//
// So the checks are DELETED, not loosened, and what replaces them is the ABSENCE
// check - which is the failure mode that still matters. Half a timer is worse than
// none: an element main.js still looks up resolves to the element but has no state
// behind it, a strip whose bindings are gone fails on the first click, and a
// stylesheet rule for a card nobody draws is a rule that can silently win a cascade
// later.
check(
  'no pomodoro strip, records archive or timer badge is left in the markup',
  ['pomo-strip', 'pomo-phase', 'pomo-toggle', 'pomo-reset', 'records-list', 'records-fold',
    'records-empty', 'records-total', 'timer-badge', 'bubble-plan', 'bubble-todo', 'bubble-input']
    .every((id) => !htmlIds.has(id)),
  'a control the host timer owned survived the timer',
);
check(
  'main.js renders no timer state',
  // The binding scan above already proves no dead App.* call survives; this is the
  // other half. A renderer reading `state.pomodoro` would paint a timer the host has
  // stopped sending - every field on it would be undefined, and the strip would be
  // blank rather than absent.
  !/state\??\.pomodoro/.test(mainJs) &&
    !/renderPomodoro|renderRecords|renderTimerBadge|tickCountdown/.test(mainJs) &&
    !/recordTotals/.test(mainJs),
  'main.js still reads a snapshot the host no longer sends',
);
check(
  'the stylesheet carries no rules for the retired cards',
  !/\.pomo-strip|\.pomo-phase|\.record-day|\.record-item|\.badge\.timer|\.bubble-plan|\.bubble-todo|\.bubble-choice/.test(
    css,
  ),
  'a stylesheet rule outlived the card it styled',
);
check(
  'the timer badge is not stacked above the todo badge any more',
  // The pair was one layout: the countdown sat above the count, and the count dropped
  // when the countdown appeared. With one badge left there is no second element and no
  // sibling rule, so the surviving badge owns the corner on its own.
  !/\.badge\.timer/.test(css) && !/timerBadge/.test(mainJs),
  'the two-badge stack is still wired up',
);

// --- 14. NO scrollbar rules, because nothing on this page scrolls -----------
//
// This block held four checks about one shared scrollbar rule: the todo list, the chat log
// and (at various times) the settings tab, the plugin tab, the records list and the voice
// studio all put the same thin translucent bar on their scrollers instead of WebView2's
// opaque grey widget.
//
// Every one of those surfaces left the page - the settings tab and the plugin tab to the
// control center, the records list and the voice studio to plugins, the todo list and the
// chat log to the todo and chat plugins' own iframes. A plugin's document brings its own
// stylesheet, so even the ones that still scroll do it out of this file's reach.
//
// What replaces them is the absence check, which is the failure mode that still matters: a
// scrollbar rule here would style a scroller the page does not contain.

check(
  'the page carries no scrollbar rules',
  !/-webkit-scrollbar/.test(css) && !/scrollbar-width|scrollbar-color/.test(css),
  'a scrollbar rule survived the surface it styled',
);
// --- 15. NO panel chrome: the close button's glyph measurement is gone --------
//
// This block held two checks about the panel header's × button: it was centred with flex
// rather than line metrics, and it carried a MEASURED 5px bottom padding because "×" is a
// math symbol whose ink sits high in an ascent-heavy box. Both the button and its stylesheet
// rule went with the panel, so the checks are deleted rather than loosened - there is no
// element left whose glyph could be off-centre.
//
// What this section now pins is the absence, and the one piece of the old measurement that
// still has a reader: `.icon-btn` is used by nothing on the page, so a rule for it would be
// the same dead-CSS trap the panel's other rules were.

check(
  'no panel chrome is left in the stylesheet',
  !/\.icon-btn/.test(css) && !/\.tab\b/.test(css) && !/\.tabs\b/.test(css),
  'a stylesheet rule for the panel header or its tab strip survived the panel',
);
// --- 16. Speech is a PLUGIN, and its workshop lives in the market -----------
//
// The pet used to own speech AND a standalone voice studio. Both are gone: the
// plugin (com.shixiansi.plugin-voice) owns the lines, the character library, the
// switches and the rendered clips, and its workshop is hosted by the plugin market
// program's window.
//
// Why the market hosts the workshop rather than the pet's panel: the pet's panel is a
// FIXED 380x520 rectangle cropped out of a window that never resizes, and a work
// surface that lists five lines with several takes each cannot fit in it. The market
// window is 980x700 and resizable, so the work surface did not have to shrink - which
// is exactly why the standalone studio could be retired without losing anything.
//
// A stylesheet that exists but is never imported produces no CSS in the bundle at
// all - the window then renders as bare HTML on the window's background colour,
// which looks like "the styling is wrong" rather than "the styling is absent". That
// is not hypothetical: the studio shipped that way once and it was only caught by
// looking at a screenshot. The check is on the IMPORT, because that is the failure -
// a CSS file on disk proves nothing.
//
// The voice plugin's own files are read near the top of this file, before the
// shared-dispatcher check that uses them.

check(
  'both frontends actually load their stylesheet',
  /^import\s+['"]\.\/style\.css['"]/m.test(marketMainJs) &&
    /^import\s+['"]\.\/style\.css['"]/m.test(mainJs),
  'a frontend has a style.css that nothing imports, so its CSS is never bundled',
);
check(
  'the voice plugin loads its own stylesheet and script',
  /href="\.\/style\.css"/.test(voicePluginHTML) && /src="\.\/app\.js"/.test(voicePluginHTML),
  'the plugin ships a stylesheet or script that its document never loads, so its CSS is never applied',
);

check(
  'the pet has no tabs at all',
  // There is no tab strip any more. It used to hold 待办 and 对话; both are plugin surfaces
  // now, and the strip went with the panel that framed it. The check used to pin "exactly
  // two tabs, and no speech tab" - the count is now zero, which subsumes both.
  (html.match(/data-tab="/g) || []).length === 0 && !htmlIds.has('tab-voice'),
  'a tab strip came back into the pet, or a speech tab with it',
);
check(
  'the pet has no in-panel way to open a voice studio',
  // The studio program is retired; a button pointing at it would be a dead entry. The
  // call form is matched rather than the bare name, because the comment recording the
  // removal has to name what it removed.
  !/voice-open-studio/.test(html) && !/App\.OpenVoiceStudio\(/.test(mainJs),
  'the pet still offers to launch a studio that no longer exists',
);
check(
  'the standalone voice studio is gone',
  !fs.existsSync(path.join(root, 'tools/voice-studio')) &&
    !fs.existsSync(path.join(root, 'internal/voice')) &&
    !fs.existsSync(path.join(root, 'internal/tts')),
  'the retired studio or its packages are still in the tree',
);

// The plugin's declaration, and the four things it must ask for to work at all.
check(
  'the voice plugin declares the capabilities it needs',
  voicePluginManifest.background === true &&
    ['audio', 'events', 'storage', 'plugin.config', 'net.fetch']
      .every((p) => voicePluginManifest.permissions.includes(p)),
  'the voice plugin cannot store audio, hear events, or reach the engine it renders with',
);
check(
  'the voice plugin carries its built-in clips as declared assets',
  // Without them the plugin is silent until the user renders every line themselves,
  // which is a cliff right after installing it.
  Array.isArray(voicePluginManifest.assets) &&
    voicePluginManifest.assets.length > 0 &&
    voicePluginManifest.assets.every((a) =>
      fs.existsSync(path.join(voicePluginDir, a))),
  'a declared asset is missing from the package, so install would ship a broken plugin',
);
check(
  'the built-in clips are real audio, not empty placeholders',
  (() => {
    const wavs = (voicePluginManifest.assets || []).filter((a) => a.endsWith('.wav'));
    if (wavs.length === 0) return false;
    return wavs.every((a) => {
      const b = fs.readFileSync(path.join(voicePluginDir, a));
      return b.length > 1000 && b.subarray(0, 4).toString() === 'RIFF';
    });
  })(),
  'a built-in clip is too small or is not a RIFF/WAVE file',
);

// The two halves of the workshop's contract, kept from the studio: a render does not
// become what the pet says until it is adopted, and the user can always get back to
// the built-in voice.
check(
  'rendering a take and adopting it are separate steps',
  // A render that committed itself would make a bad take live immediately, and the
  // user would hear it before they had a chance to audition it.
  /audio\.save/.test(voicePluginJS) &&
    /settings\.chosen\[lineID\] = name/.test(voicePluginJS) &&
    /savingLine/.test(voicePluginJS),
  'a render becomes the live take before it was saved or heard',
);
check(
  'a missing take falls back to the packaged clip rather than to silence',
  // "This character has not recorded this sentence" is the NORMAL state after a
  // switch, so it must degrade to the built-in voice. A silent pet would look exactly
  // like a broken feature.
  /builtin-/.test(voicePluginJS) && /resolveClipFor/.test(voicePluginJS),
  'a line with no adopted take would play nothing at all',
);
check(
  'the plugin asks for bytes when it renders, not text',
  // A synthesis engine answers with audio/wav, and the text form of a response body is
  // LOSSY for anything that is not valid UTF-8: every 0x80+ byte comes back as U+FFFD,
  // so the file is the right size and plays as noise. It looked like it had worked.
  /encoding: 'base64'/.test(voicePluginJS) &&
    /encoding/.test(fs.readFileSync(path.join(root, 'app_plugins.go'), 'utf8')),
  'the plugin asks for the lossy text form, so every render would be corrupt audio',
);
check(
  'the plugin never tries to start the engine itself',
  // A plugin has no capability that launches a process, and adding one would hand
  // every installed plugin the ability to run arbitrary code. The workshop reports the
  // engine's state and the user starts it.
  !/exec|spawn|StartEngine|python/i.test(voicePluginJS),
  'the plugin tries to launch the synthesis engine, which no plugin may do',
);

// The plugin's own interface, hosted by the market (now: the control center).
check(
  'the market can host a plugin interface',
  // It serves the plugin's files and answers its requests through the SAME shared
  // implementations the pet uses, so the sandbox, the traversal defence and the gate
  // have one implementation rather than two that can drift.
  /plugins\.ServePluginAsset/.test(marketPluginHost) &&
    /PluginRequest/.test(marketPluginHost) &&
    // …and a page actually MOUNTS one. Without this the Go side can host an interface that
    // no window ever loads - which is the regression this whole change exists to undo.
    /mountPlugin\(/.test(ccPluginFrameHost) &&
    /plugin-asset\//.test(ccPluginFrame) &&
    /Market\.PluginRequest|PluginRequest/.test(ccPluginFrame),
  'the market cannot host a plugin interface, so the workshop has nowhere to run',
);
check(
  'the hosted interface is sandboxed to allow-scripts alone',
  // Adding allow-same-origin would hand every installed plugin the market page's DOM,
  // which is the one thing the sandbox exists to prevent. The negative assertion is on
  // the ATTRIBUTE VALUE rather than on the word, because the comment above the line
  // necessarily names the flag it must not have.
  /setAttribute\('sandbox', 'allow-scripts'\)/.test(ccPluginFrame) &&
    !/sandbox',\s*'[^']*allow-same-origin/.test(ccPluginFrame) &&
    !/frame\.setAttribute\('sandbox'[^;]*allow-same-origin/.test(ccPluginFrame),
  'the hosted plugin can reach the host page',
);
check(
  'the market mounts an interface only for an installed and ENABLED plugin',
  // Switching a plugin off must stop it RUNNING, not merely stop it being listed: an
  // interface left on screen would keep its document alive with its permissions intact.
  // Two places have to agree: the page decides whether to render the host at all, and the
  // host itself refuses to mount without an entry.
  //
  // There was a FOURTH clause here for one round: a plugin that declares `surface` blocks was not
  // mountable at all, because those rectangles are measured from the PET's corner and one such
  // document had spoken to its other frame through `plugin.share`, which this program did not
  // serve - the user met that as 「插件 … 调用了本程序不支持的方法 share.set」 in the middle of the
  // plugin page.
  //
  // The clause is gone, and the assertion is now the NEGATIVE one: the tray is served by this
  // program too (internal/plugins/share.go, registered in pluginhost.go), and a document learns
  // where it is from `surface.slot`. What must still hold is the part this check was really for:
  // a disabled plugin gets no document.
  /function mountable\(r: PluginRow \| null\): boolean \{\s*if \(!r \|\| !r\.Manifest \|\| !r\.Enabled \|\| !r\.Manifest\.entry\) return false;/
    .test(ccPluginsPage) &&
    !/return !\(surface && surface\.length > 0\);/.test(ccPluginsPage) &&
    /\{#if mountable\(selected\)\}/.test(ccPluginsPage) &&
    /if \(!id \|\| !entry \|\| !inst\?\.Enabled\) return '';/.test(ccPluginFrameHost) &&
    /if \(!target \|\| !mountKey\) return;/.test(ccPluginFrameHost) &&
    /return \(\) => frame\.destroy\(\);/.test(ccPluginFrameHost),
  'a disabled plugin keeps a live interface in the market window',
);

// --- 17. The plugin market is a SEPARATE PROGRAM ----------------------------
// Plugin management used to be the pet's 「插件」tab, and it was moved out for the
// same reason the voice studio was: the panel is a fixed 380x520 rectangle the OS
// crops the window to, and a market needs a catalogue list, a detail pane, a
// permission sheet and a settings form at once. The screenshots that prompted the
// move show the alternative - permission rows clipped by the right edge, the
// address box half visible, and two lists fighting over one scrollbar.
//
// The split introduces the same two things worth guarding as §16 does, and both
// are easy to undo by accident:
//
//   1. The market must be its own nested module with its own exe name, not a
//      second entry point inside the pet's module. If it were the latter the pet
//      would grow a market window and the two would share a config file.
//   2. The market keeps its OWN settings file. The pet pushes its whole config to
//      its webview on every state read, so anything stored in config.json is a
//      leak surface - and it is the rollback story: the pet's file must be
//      byte-identical before and after the market runs.
const marketDir = path.join(root, 'tools/plugin-market');
const marketGoMod = fs.readFileSync(path.join(marketDir, 'go.mod'), 'utf8');
const marketWails = JSON.parse(fs.readFileSync(path.join(marketDir, 'wails.json'), 'utf8'));
// The market's PRODUCT code, with the tests explicitly excluded. The exclusion is
// load-bearing rather than tidy: config_test.go has to name the pet's config.json
// to prove the market did not touch it (T5, T10), so a scan that included tests
// would make this check and that guarantee mutually exclusive.
const marketProductGo = ['config.go', 'market.go', 'main.go']
  .map((f) => fs.readFileSync(path.join(marketDir, f), 'utf8'))
  .join('\n');
// Two of those files read on their own, for checks that are about WHERE a thing is
// declared: the settings struct (config.go) and the bindings (market.go).
const marketGo = fs.readFileSync(path.join(marketDir, 'market.go'), 'utf8');
const marketGoConfig = fs.readFileSync(path.join(marketDir, 'config.go'), 'utf8');
// The market's markup and its id set. Declared HERE rather than next to section 17b
// below, because the theme and top-bar checks that follow need them - and a `const`
// is not hoisted.
const marketHtml = fs.readFileSync(
  path.join(root, 'tools/plugin-market/frontend/index.html'),
  'utf8',
);
const marketHtmlIds = new Set([...marketHtml.matchAll(/\bid="([^"]+)"/g)].map((m) => m[1]));

check(
  'the plugin market is its own nested module reusing the pet package',
  /^module PetPC\/tools\/plugin-market$/m.test(marketGoMod) &&
    /^replace PetPC => \.\.\/\.\.$/m.test(marketGoMod),
  'the market is not a nested module on the shared pet package',
);
check(
  'the plugin market builds to its own executable name',
  marketWails.outputfilename === 'PetPCPluginMarket',
  'the market would overwrite the pet or the studio',
);
check(
  'the market frontend imports its own stylesheet',
  /^import '\.\/style\.css';/m.test(marketMainJs),
  'the market window would render as bare HTML',
);
check(
  'the plugin market is a separate program keeping its own settings file',
  // The name is a criterion of its own: it is what the report and the task list
  // refer to, so it must be greppable here.
  /plugin-market\.json/.test(marketProductGo),
  'the market has no settings file of its own',
);
check(
  'the theme is a scheme on <html>, remembered in the market settings file',
  // Three parts, and all three are needed for the switch to mean anything: the
  // palette (a second block of custom properties, not a second stylesheet), the
  // code that puts the attribute on <html>, and a preference that SURVIVES A
  // RESTART - which is why it lives in plugin-market.json and not in the webview's
  // own storage (that would also make the real-window gate depend on whoever used
  // the window last).
  /:root\[data-theme="light"\]\s*\{/.test(marketCss) &&
    /id="theme-toggle"/.test(marketHtml) &&
    /document\.documentElement\.dataset\.theme = theme/.test(marketMainJs) &&
    /Market\.SetTheme\(/.test(marketMainJs) &&
    /Market\.Theme\(\)/.test(marketMainJs) &&
    /`json:"theme,omitempty"`/.test(marketGoConfig) &&
    /func \(m \*Market\) SetTheme\(/.test(marketGo) &&
    /func \(m \*Market\) Theme\(/.test(marketGo),
  'the theme switch paints from tokens, is not remembered, or has no Go-side preference',
);
check(
  'the market top bar is a store header: mark, search, three window controls',
  // The window's title bar already says 「PetPC 插件市场」, so the page never repeats it
  // (round 5). Round 7 also took the ADDRESS out of this bar: a 70-character mirror URL
  // in the most valuable strip of the window is what made the program read as a
  // developer tool. The bar now holds the mark, the search field and three controls,
  // and the address is a field in #market-sheet - asserted by its own check below.
  !/<h1[^>]*>/.test(marketHtml) &&
    !/class="top-left"/.test(marketHtml) &&
    !/class="top-right"/.test(marketHtml) &&
    (() => {
      const top = marketHtml.slice(marketHtml.indexOf('<header class="top">'), marketHtml.indexOf('</header>'));
      return /class="brand-mark"/.test(top) &&
        /id="search"/.test(top) &&
        /id="theme-toggle"/.test(top) &&
        !/id="market-url"/.test(top);
    })(),
  'the page title came back, the address drifted back into the header, or the header lost its search',
);

// --- 17b. The market's left pane is ONE list at a time ----------------------
//
// The window used to stack 已安装 over 市场 and draw every installed plugin TWICE -
// same name, same version, same description - so three plugins read as six equally
// weighted cards and nothing on screen said which list was which. The two lists are
// now two views behind a segmented switch, and these checks pin the parts of that
// structure which are easy to undo by accident.
//
// Why it is worth a static check as well as the jsdom test in
// tools/plugin-market/frontend/src/main.test.js: the jsdom test exercises the
// switch's BEHAVIOUR, while this pins the MARKUP CONTRACT it depends on - that both
// views exist as siblings, that they are the two the code looks up, and that the
// address label did not turn into a wrapper around the input. A wrapper is the
// natural thing to write and it silently breaks the input's flex share, which only
// the real-window gate measures.
//
// NOTE: marketHtml/marketHtmlIds are declared ABOVE, next to the other market
// sources, because the theme and top-bar checks run before this section.
check(
  'the market is two sibling views, with exactly one in the flow',
  // Round 7 replaced the two-pane master-detail with a storefront: a grid of cards and
  // one plugin's page, as siblings. Both stay in the DOM and only one is ever in the
  // flow, so there is no second scrollbar and nothing claims to be "the page" twice.
  ['grid-view', 'detail-view', 'plugin-grid', 'back-to-grid', 'search']
    .every((id) => marketHtmlIds.has(id)) &&
    /function goGrid\(/.test(marketMainJs) &&
    /function goDetail\(/.test(marketMainJs) &&
    /show\(els\.gridView, true\)/.test(marketMainJs) &&
    /show\(els\.detailView, false\)/.test(marketMainJs) &&
    /show\(els\.gridView, false\)/.test(marketMainJs) &&
    /show\(els\.detailView, true\)/.test(marketMainJs),
  'the market lost one of its two views, or draws both at once',
);
check(
  'the market address is a field in the settings sheet, not a strip on the page',
  // It was the top bar for six rounds. As a SETTING it is read and edited deliberately,
  // and it stops being the loudest thing in the window - which is what the user was
  // reacting to when they said the whole window looked like a debug tool.
  /id="market-sheet" class="sheet/.test(marketHtml) &&
    marketHtml.indexOf('id="market-url"') > marketHtml.indexOf('id="market-sheet"') &&
    /id="market-save"/.test(marketHtml) &&
    !/class="top-label"/.test(marketHtml) &&
    /function openMarketSheet\(/.test(marketMainJs) &&
    /Market\.SaveMarketURL\(/.test(marketMainJs),
  'the address label wraps the input, or the address is not editable at all',
);
// The plugin's whole settings form is hidden as a UNIT, never by hiding its parts.
// The enable control is a switch whose real checkbox is visually replaced by a
// track, so code that hid the INPUT would leave the track and the word 启用 on
// screen for a plugin that is not installed - a visible control that does nothing.
check(
  'the enable control is a switch whose wrapper is what gets hidden',
  /id="detail-enabled-row"/.test(marketHtml) &&
    /class="switch-track"/.test(marketHtml) &&
    /show\(els\.detailEnabledRow, Boolean\(inst\)\)/.test(marketMainJs) &&
    !/show\(els\.detailEnabled, Boolean\(inst\)\)/.test(marketMainJs),
  'the market hides the switch input instead of the switch, leaving a dead control',
);
check(
  'the market page lays the workspace beside a permission rail, and 设置 is a dialog',
  // The list has been through four shapes: stacked panels in one scroll, a tab strip,
  // two panels under the action bar, a popover pinned to a button. On a PAGE it has a
  // column, which is why there is no popover to position, clip or dismiss any more.
  // What this pins: the rail is a real column that scrolls on its own (or its second
  // panel is pushed out of the page and becomes unreachable), and the settings form is
  // still the same modal frame the install sheet uses.
  ['detail-settings', 'settings-sheet', 'perm-list', 'perm-count', 'panel-interface', 'workspace-empty']
    .every((id) => marketHtmlIds.has(id)) &&
    !/id="perm-popover"/.test(marketHtml) &&
    !/id="detail-permissions"/.test(marketHtml) &&
    !/\.popover\s*\{/.test(marketCss) &&
    /class="detail-body"/.test(marketHtml) &&
    /<aside class="rail">/.test(marketHtml) &&
    /id="settings-sheet" class="sheet/.test(marketHtml) &&
    /function renderSettingsState\(/.test(marketMainJs) &&
    // ONE scroll container for the column: the first attempt gave the permission panel
    // its own `overflow-y: auto`, which grew instead of scrolling and pushed 关于 out
    // of the page.
    /\.rail \{[\s\S]{0,500}?overflow-y: auto/.test(marketCss) &&
    // The interface panel is a static node: it hosts a document that is running with
    // the user's permissions, so rebuilding it would tear that document down.
    !/panelInterface\?\.replaceChildren/.test(marketMainJs),
  'a surface moved back into the pane flow, the permission rail lost its scroller, or the popover returned',
);
check(
  'the granted count lives in the permission panel header',
  // The answer to "is this fully granted?" is read from the same panel as the ticks, so
  // it is a heading note. It used to be a badge on a button because the list was shut
  // most of the time.
  /id="perm-count"/.test(marketHtml) &&
    /class="panel-note"/.test(marketHtml) &&
    /\.panel-note\s*\{/.test(marketCss) &&
    /function renderPermCount\(/.test(marketMainJs) &&
    !/class="tool-count"/.test(marketHtml) &&
    !/class="tool-count"/.test(marketCss),
  'the granted count drifted out of the permission panel, or the badge style came back',
);
check(
  'every icon in the market window is a symbol in its own sprite',
  // Every #ico-… referenced anywhere has to have a <symbol> behind it, or the
  // control renders with an empty box where its glyph should be - the exact failure
  // a shared sprite exists to prevent, and one no DOM test can see.
  //
  // The references are taken from BOTH the markup and the code: the theme switch
  // swaps its glyph at runtime (`applyTheme` writes #ico-sun / #ico-moon), so a
  // markup-only scan would report the sun as unused and then, if someone deleted it,
  // would happily pass while light mode showed nothing.
  (() => {
    const referenced = new Set([
      ...[...marketHtml.matchAll(/href="#(ico-[a-z]+)"/g)].map((m) => m[1]),
      ...[...marketMainJs.matchAll(/'#(ico-[a-z]+)'/g)].map((m) => m[1]),
    ]);
    const defined = new Set(
      [...marketHtml.matchAll(/<symbol id="(ico-[a-z]+)"/g)].map((m) => m[1]),
    );
    const missing = [...referenced].filter((id) => !defined.has(id));
    // …and the other direction: a symbol nothing points at is dead weight in a
    // window whose whole icon set is one <svg>.
    const unused = [...defined].filter((id) => !referenced.has(id));
    if (missing.length) console.log('    missing icons=' + JSON.stringify(missing));
    if (unused.length) console.log('    unused icons=' + JSON.stringify(unused));
    return missing.length === 0 && unused.length === 0 && referenced.size > 0;
  })(),
  'an icon is referenced but not defined in the sprite, or defined and never used',
);
check(
  'the hero states no status chip, and the controls are one bar at its end',
  // The chip said 已启用 beside the switch that shows and changes exactly that, so it
  // was the same fact twice. The controls are ONE BAR, right-aligned in the hero: a
  // footer row cost a row of height and left half of it empty (round 5), and round 7
  // kept the bar while the name moved above its own pitch.
  !/id="detail-state"/.test(marketHtml) &&
    !/renderStateChip/.test(marketMainJs) &&
    /class="hero-actions action-bar"/.test(marketHtml) &&
    ['detail-install', 'detail-update', 'detail-enabled-row', 'detail-settings', 'detail-uninstall']
      .every((id) => {
        const at = marketHtml.indexOf(`id="${id}"`);
        return at > marketHtml.indexOf('class="hero-actions action-bar"') &&
          at < marketHtml.indexOf('id="panel-interface"');
      }) &&
    /\.action-bar\s*\{/.test(marketCss) &&
    !/class="action-gap"/.test(marketHtml) &&
    !/\.action-gap\s*\{/.test(marketCss),
  'the status chip came back, or the controls left the hero’s action bar',
);
check(
  'each bundled plugin follows the window’s colour scheme',
  // A plugin's workshop is a FOREIGN document in a sandboxed frame: it cannot read the
  // market's custom properties, so the host tells it. Three parts per plugin, and each
  // one covers a moment the others miss - the URL for the first paint (no flash), the
  // bridge message for a switch while it runs, and the `data-theme` attribute that its
  // stylesheet keys on.
  bundledPlugins.every(
    (p) =>
      /html\[data-theme="light"\]\s*\{/.test(p.css) &&
      /URLSearchParams\(location\.search\)\.get\('theme'\)/.test(p.html) &&
      /case 'theme':/.test(p.js) &&
      /function applyTheme\(/.test(p.js),
  ) &&
    // …and the host has to actually say it, on both channels: the scheme on the frame URL
    // (first paint, no flash) and the `theme` message (a switch while the document runs).
    // Read from the CONTROL CENTER's frame host now - the old market frontend is not built
    // any more, so assertions against it certified code no window could run.
    /plugin-asset\/\$\{pluginId\}\/\$\{entry\}\?theme=\$\{theme\}/.test(ccPluginFrame) &&
    /if \(initialised\) post\(\{ type: 'theme', theme: current \}\)/.test(ccPluginFrame) &&
    /theme: current,/.test(ccPluginFrame) &&
    /document\.documentElement\.dataset\.theme === 'light'/.test(ccPluginFrame) &&
    /:root\[data-theme='light'\]/.test(ccTokens),
  'a plugin (or the host) does not carry the window’s scheme, so it stays dark in a light window',
);
check(
  'the market states a plugin\'s state as a dot on its card icon',
  // The state is a badge ON THE ICON now, the way an app list shows "running". It used
  // to be a colour stripe down a row's left edge plus a pill under the text; in a grid
  // of cards the stripe has nowhere to be, and the pill repeated 已启用 on every card.
  // Each state needs a rule, or the class is decoration with no effect.
  /\.app-dot\s*\{/.test(marketCss) &&
    ['is-on', 'is-off', 'is-err', 'is-update'].every((c) =>
      new RegExp(`\\.app-dot\\.${c}`).test(marketCss)) &&
    /app-dot/.test(marketMainJs),
  'a card state has no dot rule, so the class paints nothing',
);
check(
  'the market never names the pet config, let alone writes it',
  // Zero hits for the PET's filename anywhere in the market's product code, not
  // even in a comment. The seeding read lives in internal/store, which already
  // owns that name and its parsing - and keeping the name out of this program is
  // the point rather than tidiness: the moment the market can name the file, it
  // can be edited to write it, and "the pet's config is never modified by another
  // program" is what makes a rollback a zero-step operation.
  //
  // The test file may still name it, which is exactly why the scan is over the
  // product code: config_test.go asserts the pet's config is byte-identical
  // before and after a save, and it cannot do that without naming the file.
  !/config\.json/.test(marketProductGo),
  'the market can name the pet config, so it can be edited to write it',
);

check(
  'the market explains every permission the backend allows',
  // Zero hits for a permission the user is asked to grant but that renders as a bare
  // id. This check used to compare the Go const block against a map in the market's OLD
  // frontend (`frontend/src/main.js` → `PERMISSION_LABELS`); that file is not built any
  // more, so the copy it guarded is gone with it.
  //
  // What replaced it is BETTER than a second table: the shipping page renders the host's
  // own wording (`Market.PermissionLabels()` → `main.LabelPair`), so "consent described two
  // ways is consent to two things" cannot happen at all. The remaining duplicate is the
  // DEV STUB in `cc/src/lib/bridge.ts`, which must stay complete - a browser that shows a
  // bare `plugin.share` is a browser where nobody notices the label really was dropped.
  //
  // AGENTS.md §3.1 lists this as one of the seven places a new permission must be added.
  (() => {
    const consts = capsGo.slice(capsGo.indexOf('const ('), capsGo.indexOf('// AllowedPermissions'));
    const allowed = [...consts.matchAll(/Perm[A-Za-z]+\s*=\s*"([^"]+)"/g)].map((m) => m[1]);
    const stubAt = ccBridge.indexOf('const LABELS');
    const stubBody = ccBridge.slice(stubAt, ccBridge.indexOf('];', stubAt));
    const labelled = [...stubBody.matchAll(/^\s*\['([a-z][a-z.]*)',/gm)].map((m) => m[1]);
    // …and the real screen must ask the HOST, not a map of its own.
    const asksHost = /PermissionLabels\(\)/.test(ccBridge) && /PermissionLabels\(/.test(ccPluginsPage);
    if (allowed.length === 0 || labelled.length === 0 || !asksHost) return false;
    const missing = allowed.filter((name) => !labelled.includes(name));
    const extra = labelled.filter((name) => !allowed.includes(name));
    if (missing.length || extra.length) {
      console.log('    missing=' + JSON.stringify(missing) + ' extra=' + JSON.stringify(extra));
      return false;
    }
    return true;
  })(),
  'a permission has no explanation in the market, or the market explains one the backend dropped',
);

// --- 18. The three fake bridges are ONE set ----------------------------------
// The pet's page is exercised through three different fakes, and they have to
// describe the SAME host or the coverage is an illusion:
//
//   - frontend/wailsjs/go/main/App.js is the real generated bridge, and is the
//     definition of what the app can call;
//   - .dev/run-frontend-harness.cjs fakes it for the headless browser run;
//   - frontend/src/test-harness.js fakes it for the jsdom unit tests.
//
// A method MISSING from a fake makes the page throw partway through its own
// wiring, and the failure surfaces much later as something unrelated - the panel
// never opens and the cause reads as a missing element. A method that is PRESENT
// but no longer exported lets a deleted binding stay "covered" by a test that can
// no longer fail. Both directions are checked here, and against the generated
// file rather than against a list kept by hand, so adding a binding in Go without
// teaching the fakes about it fails here instead of in the browser run.
const extractMethods = (src, marker) => {
  const at = src.indexOf(marker);
  if (at < 0) return null;
  const start = src.indexOf('[', at);
  const end = src.indexOf('];', start);
  if (start < 0 || end < 0) return null;
  return src.slice(start + 1, end)
    .split(',')
    .map((s) => s.trim().replace(/^['"]|['"]$/g, ''))
    .filter(Boolean);
};
const realMethods = [...appJs.matchAll(/^export function (\w+)/gm)].map((m) => m[1]);
const harnessMethods = extractMethods(
  fs.readFileSync(path.join(root, '.dev/run-frontend-harness.cjs'), 'utf8'),
  'const methods = [',
);
const jsdomMethods = extractMethods(
  fs.readFileSync(path.join(root, 'frontend/src/test-harness.js'), 'utf8'),
  'methods = [',
);
// Sorted-join comparison rather than set arithmetic, so a DUPLICATE entry is also
// caught: a name listed twice would pass a set comparison while proving the list
// had stopped being maintained.
const sameSet = (a, b) => a && b && [...a].sort().join('\n') === [...b].sort().join('\n');
const onlyIn = (a, b) => (a || []).filter((m) => !(b || []).includes(m));
check(
  `the headless harness fakes exactly the bindings Go exports (${realMethods.length})`,
  sameSet(harnessMethods, realMethods),
  `missing: ${onlyIn(realMethods, harnessMethods).join(', ')} | surplus: ` +
    `${onlyIn(harnessMethods, realMethods).join(', ')}`,
);
check(
  `the jsdom harness fakes exactly the bindings Go exports (${realMethods.length})`,
  sameSet(jsdomMethods, realMethods),
  `missing: ${onlyIn(realMethods, jsdomMethods).join(', ')} | surplus: ` +
    `${onlyIn(jsdomMethods, realMethods).join(', ')}`,
);

// --- the chat plugin's auto-hide guard --------------------------------------
//
// The surface closes itself 3 s after an answer once the composer has lost focus. That timer is
// skipped while "another question is in flight", and WHICH FLAG says that is the whole point:
//
//   - `busy` is set and cleared by the frame that sent the request - the input frame, which is the
//     only one that can also see the composer's focus;
//   - `asking` is cleared by the ANSWER frame's end-of-exchange timer, and `endExchange` returns
//     immediately in every other role, so in the input frame it stayed true FOR EVER after the
//     first message. The hide was therefore skipped every single time - the user's 「完全不会收起」.
check(
  'the chat surface auto-hides by ASKING whether it has focus, not by waiting for blur',
  (() => {
    const start = chatPluginJs.indexOf('function startAutoHidePoll');
    if (start < 0) return false;
    const body = chatPluginJs.slice(start, chatPluginJs.indexOf('\n}\n', start));
    return (
      // It must POLL. A `blur` inside an opaque-origin iframe whose whole window is losing focus is
      // not reliable in WebView2 - the first version listened for it and did nothing when the user
      // clicked the desktop.
      /setInterval\(/.test(body) &&
      /documentFocused\(\)/.test(body) &&
      /composerFocused\(\)/.test(body) &&
      // Both must be true to SKIP the hide, so either signal reporting "gone" is enough to act on.
      /if \(documentFocused\(\) && composerFocused\(\)\) return;/.test(body) &&
      // `busy` is the flag this frame clears itself; `asking` is cleared only by the ANSWER frame.
      /if \(busy\) return;/.test(body) &&
      !/if \(asking\) return;/.test(body) &&
      /request\('surface\.hide'/.test(body) &&
      // And the event path must be gone rather than left alongside it.
      !/addEventListener\('blur'/.test(chatPluginJs)
    );
  })(),
  'the auto-hide waits for a blur that may never arrive, or guards on a flag this frame never clears',
);

// --- the 日志 page, and the log behind it ------------------------------------
//
// Three of these are wiring rather than taste:
//
//  1. the page reads the HOST's log through the one binding, because half of the lines are
//     written by the OTHER process (the pet). A page with its own in-memory buffer could only
//     ever show this program's half - and the questions people actually ask ("对话插件不回话")
//     are about the pet's half;
//  2. every level has a rule of its own. "分级" that paints nothing is a word in a <select>;
//  3. the dev stub APPLIES the query. A stub that returned the same rows for every filter is
//     exactly how a page whose filters are wired to nothing looks perfect in a browser - the
//     failure this whole file exists to catch (see bridge.ts's header).
const ccLogsPage = fs.readFileSync(path.join(root, 'tools/plugin-market/cc/src/pages/Logs.svelte'), 'utf8');
const ccApp = fs.readFileSync(path.join(root, 'tools/plugin-market/cc/src/App.svelte'), 'utf8');
const petAppGo = fs.readFileSync(path.join(root, 'app.go'), 'utf8');
check(
  'the control center has a 日志 page, and it reads the host log',
  /id: 'logs', label: '日志'/.test(ccApp) &&
    /LogTail\(/.test(ccLogsPage) &&
    /LogTail\(/.test(ccBridge) &&
    /export function LogTail/.test(
      fs.readFileSync(path.join(root, 'tools/plugin-market/frontend/wailsjs/go/main/Market.d.ts'), 'utf8'),
    ),
  'the 日志 page is missing from the rail, does not call LogTail, or the Go binding is gone',
);
check(
  'every log level the page promises has a rule that paints it',
  // The four levels are drawn as `lv-<lowercase>`; a level with no rule renders as plain
  // text, which makes ERROR look like INFO in a list of 500 rows.
  ['debug', 'info', 'warn', 'error'].every((lv) =>
    new RegExp(`\\.lv-${lv} \\.level \\{[^}]*color:`).test(ccLogsPage)) &&
    ['.lv-warn', '.lv-error'].every((c) => new RegExp(`\\${c} \\{[^}]*box-shadow`).test(ccLogsPage)),
  'a level has no colour (or no warning stripe), so the level column carries no information',
);
check(
  'the dev stub filters, so a filter wired to nothing cannot look correct in a browser',
  // The three knobs the page exposes must each be read by the fake: level, scope, search.
  /FAKE_LOG_LEVELS\[r\[1\]\]/.test(ccBridge) &&
    /r\[2\] === scope/.test(ccBridge) &&
    /r\[4\]\.toLowerCase\(\)\.includes\(search\)/.test(ccBridge),
  'the log stub ignores the query, so the page\'s filters are unpinned',
);
check(
  'the log is always on: neither program gates it behind PETPC_DEBUG any more',
  // The old DebugLog opened debug.log only when PETPC_DEBUG was set, which is why every
  // report arrived with no evidence. The two programs now open their own file unconditionally
  // (the level is what varies), and a re-introduced gate here would silently undo that.
  /logging\.Open\(dir, logging\.PetLog/.test(petAppGo) &&
    /logging\.Open\(dir, logging\.MarketLog/.test(fs.readFileSync(path.join(root, 'tools/plugin-market/market.go'), 'utf8')) &&
    !/PETPC_DEBUG"\) == ""/.test(petAppGo) &&
    !/debug\.log/.test(ccLogsPage),
  'one program stopped opening its log, or a PETPC_DEBUG gate came back',
);

// --- the conversation is in the log at DEBUG, and the report stops at INFO ----------------
//
// One decision with two halves, and each half is useless without the other: logging the words at
// INFO would make every pasted diagnostics report a transcript, while keeping the report at DEBUG
// after the log gained the words would leak them through the other door. Together they buy the
// thing that did not exist before — "turn on debug and read what was actually sent and received" —
// without costing the "a report is safe to paste" property.
check(
  'the model conversation is logged at DEBUG and nowhere else',
  /func logChatRequest\(/.test(goAppPlugins) &&
    /func logChatReply\(/.test(goAppPlugins) &&
    // The guard is what makes it DEBUG-only: a content line without it would ride whatever level
    // the file happens to be at, which is INFO for every user.
    /log\.Level\(\) != logging\.LevelDebug/.test(goAppPlugins) &&
    /"llm\.chat 发出\[/.test(goAppPlugins) &&
    /llm\.chat 收到（/.test(goAppPlugins),
  'the DEBUG content lines are gone, or their level guard was removed',
);
check(
  'the diagnostics report stops at INFO, so a pasted report has no conversation in it',
  /MinLevel: logging\.LevelInfo/.test(
    fs.readFileSync(path.join(root, 'tools/plugin-market/settingsapi_diagnostics.go'), 'utf8'),
  ),
  'the report reads DEBUG lines, so copying it can carry the conversation',
);

// --- the 市场 page: the catalogue is visible, and installing is not enabling -----------
//
// WHY THIS IS A GATE AND NOT ONLY A SMOKE TEST. The two failures worth catching here are both
// invisible in a screenshot: (a) the page is in the rail but reads nothing (a catalogue fetched by
// nobody), and (b) it installs by switching the plugin ON - the host granting authority on the
// user's behalf, which is the one thing the install path deliberately does not do (a fresh install
// lands disabled with nothing granted, and the page says so and points at 插件).
//
// The negative half is the load-bearing half: `SetEnabled` / `Grant` must NOT appear on this page.
const ccMarketPage = fs.readFileSync(path.join(root, 'tools/plugin-market/cc/src/pages/Market.svelte'), 'utf8');
check(
  'the control center has a 市场 page, and it reads the catalogue through the binding',
  /id: 'market', label: '市场'/.test(ccApp) &&
    /Catalogue\(/.test(ccMarketPage) &&
    /Catalogue = \(/.test(ccBridge) &&
    /export function Catalogue/.test(
      fs.readFileSync(path.join(root, 'tools/plugin-market/frontend/wailsjs/go/main/Market.d.ts'), 'utf8'),
    ),
  'the 市场 page is missing from the rail, does not call Catalogue, or the Go binding is gone',
);
check(
  'the 市场 page installs and updates, and both reach the host',
  /Install\(/.test(ccMarketPage) &&
    /Update\(/.test(ccMarketPage) &&
    /export const Install/.test(ccBridge) &&
    /export const SaveMarketURL/.test(ccBridge) &&
    /export function Install/.test(
      fs.readFileSync(path.join(root, 'tools/plugin-market/frontend/wailsjs/go/main/Market.d.ts'), 'utf8'),
    ),
  'installing from the market would do nothing (no Install/Update call, or no Go binding)',
);
check(
  'installing from the 市场 page never enables or authorises anything',
  // Install lands the package DISABLED with nothing granted; the user says yes on 插件. A market
  // page calling either of these is the difference between "it is here" and "it is running with
  // permission you were never asked for".
  !/SetEnabled\s*\(/.test(ccMarketPage) && !/\bGrant\s*\(/.test(ccMarketPage),
  'the 市场 page enables or grants on the user\'s behalf',
);

// --- uninstalling: it asks first, and the answer decides the data ----------------------
//
// Two failures worth pinning, and neither shows up in a screenshot of the dialog:
//   (a) 卸载 acting on the click instead of opening the dialog - one mis-click loses a plugin;
//   (b) the data being deleted regardless of the checkbox - which is exactly what the host did
//       before this change, and it made 「卸载」 the most destructive button in the program with
//       nothing in the word saying your keys were going with it.
// The box must also START unticked: the safe answer is the one that has to be asked for.
check(
  'uninstalling asks first, and the data question is a checkbox in that dialog',
  /Uninstall\s*\(/.test(ccPluginsPage) &&
    /aria-label="卸载插件"/.test(ccPluginsPage) &&
    /bind:checked=\{uninstallPurge\}/.test(ccPluginsPage) &&
    /uninstallPurge = false;/.test(ccPluginsPage) &&
    /export const Uninstall = \(id: string, purgeData: boolean\)/.test(ccBridge) &&
    /export function Uninstall\(arg1:string,arg2:boolean\)/.test(
      fs.readFileSync(path.join(root, 'tools/plugin-market/frontend/wailsjs/go/main/Market.d.ts'), 'utf8'),
    ),
  'the uninstall dialog, its checkbox, or the two-argument binding went missing',
);
check(
  'the host deletes the plugin\'s private data only when it was asked to',
  (() => {
    const managerGo = fs.readFileSync(path.join(root, 'internal/plugins/manager.go'), 'utf8');
    const at = managerGo.indexOf('func (m *Manager) Uninstall(');
    if (at < 0) return false;
    const body = managerGo.slice(at, managerGo.indexOf('\n}\n', at));
    // The signature carries the flag, the body guards the data directory with it, and the
    // plugin directory is removed unconditionally (that half is the rollback).
    return /Uninstall\(id string, purge bool\)/.test(body) &&
      /if purge \{[\s\S]*dataDirFor\(id\)/.test(body) &&
      /os\.RemoveAll\(dir\)/.test(body);
  })(),
  'uninstall deletes plugin-data/<id> without being asked, or the flag is not honoured',
);

// --- the plugin page mounts the plugin's own document ---------------------------------
//
// One round went wrong here, and the wrongness was two decisions propping each other up: the
// market did not serve `plugin.share`, so mounting a document that publishes a shared key put
// 「本程序不支持该方法」 in the middle of the plugin's own page - and the fix was to refuse to
// mount any plugin that declared a `surface`. That refused exactly the three plugins whose
// pages the user wanted (todo / pomodoro / chat), and it left the 待办 page reading
// `todos.json`, a file the todo plugin stopped writing when the list moved into its storage.
//
// So the tray moved to internal/plugins/share.go and both hosts serve it. This checks the
// three pieces that have to stay in step: the market registers the tray, `mountable()` gates on
// "installed + enabled + ships a document" and nothing else, and no 待办 page has crept back.
check(
  'the plugin page mounts every installed plugin\'s own document',
  (() => {
    const at = ccPluginsPage.indexOf('function mountable');
    if (at < 0) return false;
    const body = ccPluginsPage.slice(at, ccPluginsPage.indexOf('\n  }', at));
    return (
      /!r\.Enabled/.test(body) &&
      /!r\.Manifest\.entry/.test(body) &&
      // No surface refusal: a block of coordinates measured from the pet is not a reason to
      // hide a plugin's page from the only window with room for it.
      !/surface/.test(body) &&
      /plugins\.RegisterShare\(r, gateAdapter\{m\.manager\}\)/.test(marketPluginHost) &&
      // The pet's forwards live in app_plugins.go (that is where its capability table is),
      // not in app.go.
      /plugins\.ShareSet\(a\.pluginManager\(\)/.test(
        fs.readFileSync(path.join(root, 'app_plugins.go'), 'utf8'),
      )
    );
  })(),
  'the plugin page refuses a surface plugin again, or a host stopped serving the tray',
);
check(
  "the todo list has one home: the plugin's document, not a control-center page",
  !/id: 'todos'/.test(ccApp) &&
    !/export const Todos/.test(ccBridge) &&
    !/func \(m \*Market\) Todos\(/.test(
      fs.readFileSync(path.join(root, 'tools/plugin-market/settingsapi_read.go'), 'utf8'),
    ) &&
    !fs.existsSync(path.join(root, 'tools/plugin-market/cc/src/pages/Todos.svelte')),
  'a 待办 page (or its binding) came back, so two programs draw one list',
);
check(
  'an installed plugin can be UPDATED from its page',
  // A plugin's interface ships with the plugin, so an installed package that is behind shows the
  // old behaviour while the page around it looks finished - this round's documents would simply
  // not arrive. The page therefore reads the catalogue (best effort, offline allowed) and offers
  // the upgrade; the binding it must call is `Market.Update`, which keeps the plugin enabled and
  // keeps the permissions the user granted.
  /Catalogue\b/.test(ccPluginsPage) &&
    /Update\(/.test(ccPluginsPage) &&
    /export const Update = /.test(ccBridge) &&
    /export function Update/.test(
      fs.readFileSync(path.join(root, 'tools/plugin-market/frontend/wailsjs/go/main/Market.d.ts'), 'utf8'),
    ) &&
    // A DOWNGRADE must not be offered: only the "newer" direction has a button.
    /isNewer\(entry\.version, installed\)/.test(ccPluginsPage),
  'the plugin page cannot offer a newer package, so an older plugin keeps its old interface',
);

for (const line of notes) console.log(line);
console.log('');
if (failures.length) {
  for (const line of failures) console.log(line);
  console.log(`\n${failures.length} FRONTEND CHECK(S) FAILED`);
  process.exit(1);
}
console.log(`ALL FRONTEND CHECKS PASSED (${notes.length})`);
