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
const modelsTs = fs.readFileSync(path.join(root, 'frontend/wailsjs/go/models.ts'), 'utf8');
// The main app file, read here because the tray menu and its command handling
// live in it - the shortcut feature's mouse fallback is checked against it.
const mainGo = fs.readFileSync(path.join(root, 'app.go'), 'utf8');
// The pomodoro gate: the app bindings and the pure state machine. Read separately
// because the "does not auto-start" rule lives in the engine and everything that
// releases it lives in the app layer.
//
// The engine is now a PACKAGE, not a file next to app_pomodoro.go: the extraction
// (openspec/changes/pomodoro-state-machine-extraction) moved it to
// internal/pomodoro/machine.go. This line is the second thing that has to move
// with it - a gate that reads a file by path throws ENOENT the moment the file
// moves, and a check that throws is a check nobody reads. The assertion below it
// is unchanged, because the code shape it pins is unchanged.
const goAppPomodoro = fs.readFileSync(path.join(root, 'app_pomodoro.go'), 'utf8');
const goPomodoroEngine = fs.readFileSync(path.join(root, 'internal/pomodoro/machine.go'), 'utf8');
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
const marketPluginUI = fs.readFileSync(
  path.join(root, 'tools/plugin-market/frontend/src/plugin-ui.js'), 'utf8');

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
const layoutReturning = ['OpenChat', 'SetPanelOpen', 'TogglePanel', 'DismissBubble', 'OpenPluginSurface', 'ClosePluginSurface', 'OpenChatPluginSurface'];
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
const goFiles = [
  'app_llm.go',
  'app.go',
  'app_pomodoro.go',
  'app_hotkey.go',
  'app_plugins.go',
  'app_plugin_market.go',
  'app_chat_slot.go',
  'app_plugin_migrate.go',
  'app_plugin_surface.go',
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
// The three names the pet's page still speaks. Each is checked as a PAIR - emitted
// by Go and subscribed by the page - because either half alone is the failure
// mode that matters: an event nobody sends leaves a dead listener, and one nobody
// listens to leaves a feature that does nothing.
//
// pet:plugin-event and pet:layout are subscribed by plugin-host.js and main.js;
// pet:chat-open is subscribed by main.js and emitted by app_llm.go's OpenChat, and
// it is what tells the panel which tab to show when the conversation plugin asks
// for the slot.
check(
  'the plugin and layout events are both emitted and subscribed',
  ['pet:plugin-event', 'pet:layout', 'pet:chat-open'].every(
    (n) => emittedNames.has(n) && subscribed.has(n),
  ),
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
  { marker: '/* --- chat tab', source: css },
  { marker: '/* --- settings: compact controls', source: css },
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

// --- 4b. The settings page is grouped, and stays one scrolling column -------
// The layout change this guards: ten flat rows separated by the same 1px line
// became five cards, each with a heading, and every control in the form now
// starts at the same x. Both halves are load-bearing - the cards carry the
// grouping, the shared column is what makes a long form scannable - and both are
// easy to undo by accident when editing the markup.
//
// Five, not seven: the AI configuration card and the proactive-speech card moved
// to the market program, so the pet's settings describe the pet.

const settingsBody = (() => {
  const start = html.indexOf('id="tab-settings"');
  if (start < 0) return '';
  // The settings body ends where the panel's next top-level child begins.
  const end = html.indexOf('id="toast"', start);
  return html.slice(start, end < 0 ? undefined : end);
})();

const groups = [...settingsBody.matchAll(/<section class="sgroup">([\s\S]*?)<\/section>/g)].map(
  (m) => m[1],
);
check(
  `the settings page is four named groups (${groups.length} found)`,
  // Four, not five: the speech group left with the subsystem, so the pet's own
  // settings are appearance, pomodoro, reminders and the data group that carries the
  // way into the market.
  groups.length === 4,
  `expected 4 .sgroup cards, found ${groups.length}`,
);
check(
  'every group has a heading',
  groups.length > 0 && groups.every((g) => /<h3 class="sgroup-head">[^<]+<\/h3>/.test(g)),
  'a group is unlabelled, so the grouping says nothing',
);
// The AI configuration is not merely hidden here, it is GONE from the pet: the
// conversation is a plugin, and the plugin's own settings are edited in the
// market program. A leftover sub-block or switch would be a control that writes
// to a config no request reads - the failure mode is silent, which is why it is
// asserted as an absence rather than left to the id whitelist below.
//
// The check is on the sub-headings by NAME rather than on a forbidden string:
// pinning the exact set is both stronger (a new sub-block is a failure, not just
// the old one) and free of the words themselves, which the T91 criterion requires
// this file not to contain at all - a grep that has to match the thing it forbids
// is a grep that cannot be run.
const subHeads = [...settingsBody.matchAll(/<h4 class="ssub">([^<]+)<\/h4>/g)].map((m) => m[1]);
check(
  'the settings sub-headings are exactly the pet\'s own',
  subHeads.length > 0 &&
    subHeads.every((t) => ['隐藏与显示', '阶段时长', '当前番茄', '节奏'].includes(t)),
  `a settings sub-block belongs to another program: ${subHeads.join(', ')}`,
);
check(
  'the pet settings carry no AI configuration',
  // The ids are the second half of the same fact: a re-added generation-parameter
  // row would have to bring an id with it, and every AI id is out of the
  // whitelist below - so this catches the case where the markup was restored but
  // the heading was renamed.
  !/\bllm-/.test(settingsBody),
  'the pet still has an AI configuration section',
);
check(
  'the flat layout left nothing behind',
  // The old classes are not merely unused: leaving the rules in place would keep
  // two competing metric systems in the same file, and the next setting added
  // would pick whichever one its author copied.
  !/class="(setting|num-row|setting-label)[" ]/.test(html) &&
    !/^\.(setting|num-row|setting-label)\s*[{,]/m.test(css),
  'the previous flat settings classes are still present',
);

// The alignment invariant, checked as one thing rather than as a regex on a
// single declaration: every row in the form must be one of the two shared shapes,
// and the .field label column must be a fixed width. Miss either half and rows
// silently drift back to finding their own left edge.
const settingsRows = [...settingsBody.matchAll(/<div class="(field|pairs|float)[^"]*"/g)].map(
  (m) => m[1],
);
check(
  `every settings row uses a shared shape (${settingsRows.length} field rows)`,
  settingsRows.length > 0 && settingsRows.every((c) => c === 'field'),
  `a row uses its own layout: ${[...new Set(settingsRows)].join(', ')}`,
);
check(
  'the field rows build on one shared label column',
  /--label-col:\s*\d+(\.\d+)?(px|rem|em)\s*;/.test(css) &&
    (css.match(/grid-template-columns:\s*var\(--label-col\)\s+1fr/g) || []).length >= 1 &&
    /\.field\s*\{[^}]*grid-template-columns:\s*var\(--label-col\)/.test(css),
  'the labelled rows do not share one label-column definition',
);
// The switch rows are deliberately NOT that grid: a checkbox and its caption are
// one unit, so the text stays beside the box rather than being pushed out to the
// value column. Pinning the negative keeps someone from "fixing" the alignment by
// re-applying the grid and stranding every checkbox from its label.
check(
  'switch rows keep the checkbox beside its own text',
  (() => {
    const m = /#tab-settings \.row\s*\{([^}]*)\}/.exec(css);
    if (!m) return false;
    return !/grid-template-columns/.test(m[1]) && /display:\s*flex/.test(m[1]);
  })(),
  'the switch rows were put back on the label grid, so the text leaves its checkbox',
);
// Nothing in the settings tab may set its own size: the panel's rectangle comes
// from the layout and the native window region is cut to exactly it.
check(
  'the settings tab still scrolls inside the panel and sizes nothing itself',
  /#tab-settings\s*\{[^}]*overflow-y:\s*auto/.test(css) &&
    !/(panel|settingsBody|tabSettings)\.style\.(width|height)\s*=/.test(mainJs),
  'the settings tab grows the panel or sets a size directly',
);
// A setting removed from BOTH the markup and main.js would pass every check
// above and silently disappear, so the set is pinned. Reordering and regrouping
// are free; losing a control is not.
const settingsIds = new Set(
  [...settingsBody.matchAll(/\bid="([^"]+)"/g)]
    .map((m) => m[1])
    // The body's own id is the anchor, not a setting.
    .filter((id) => id !== 'tab-settings'),
);
const expectedSettingsIds = [
  'image-path', 'choose-image', 'reset-image', 'scale', 'scale-value', 'snap', 'autostart',
  'hotkey-enabled', 'hotkey-show', 'hotkey-hide', 'hotkey-status',
  'pomo-on-startup', 'pomo-auto-next', 'pomo-sound', 'pomo-focus', 'pomo-short', 'pomo-long',
  'pomo-every', 'pomo-reset',
  'rem-enabled', 'rem-on-startup', 'rem-during-break', 'rem-interval', 'rem-jitter', 'rem-from',
  'rem-to', 'rem-items', 'rem-hide', 'rem-empty', 'rem-now', 'rem-status',
  // The one AI-shaped control left in the pet: it launches the market program
  // rather than configuring anything, which is the whole point of the split.
  'open-plugin-market',
  // No voice ids: speech is a plugin, configured in the market's own window, so the
  // pet's settings must carry none of them. The absence has its own check above, and
  // leaving them here would make this whitelist disagree with the page.
  'data-dir', 'quit',
];
const missingSettings = expectedSettingsIds.filter((id) => !settingsIds.has(id));
const extraSettings = [...settingsIds].filter((id) => !expectedSettingsIds.includes(id));
check(
  `the settings page still carries every control (${expectedSettingsIds.length} ids)`,
  missingSettings.length === 0 && extraSettings.length === 0,
  `missing: ${missingSettings.join(', ') || 'none'} / unexpected: ${extraSettings.join(', ') || 'none'}`,
);

// --- 5. The conversation lives in the panel as a tab ------------------------
check(
  'index.html has a conversation tab body',
  htmlIds.has('tab-chat'),
  'no #tab-chat',
);
check(
  'the tabs list includes the conversation tab',
  /data-tab="chat"/.test(html),
  'no data-tab="chat" button',
);
// Four tabs, not five: the plugin tab is gone. Managing plugins is the market
// program's job now (a separate .exe with its own window), so the pet's panel
// kept the four tabs that describe the pet itself.
check(
  'the tabs are todos/chat/records/settings',
  (html.match(/data-tab="/g) || []).length === 4,
  'unexpected tab count',
);
// The conversation tab is a MOUNT POINT, not a transcript: the plugin's iframe
// is placed into #chat-slot by plugin-host.js, and the slot is what resolves
// which plugin owns it. Without the element the plugin would have nowhere to
// draw and the tab would be blank.
check(
  'index.html has a conversation slot for the plugin',
  htmlIds.has('chat-slot'),
  'no #chat-slot',
);
check(
  'switchTab toggles the conversation tab with the others',
  /tabChat\.classList\.toggle\('hidden', name !== 'chat'\)/.test(mainJs),
  'switchTab does not toggle #tab-chat',
);
// The 对话 TAB (the button) is shown only while a plugin fills it.
//
// The pet has no conversation of its own: the tab is a mount point, so with no
// plugin it opens onto an explanation and nothing else. The button is hidden in
// that state, which means the markup must carry an id for it (separate from
// #tab-chat, which is the BODY and is toggled by switchTab for a different
// reason).
check(
  'the conversation tab button has its own id, separate from its body',
  htmlIds.has('tab-chat-btn') && htmlIds.has('tab-chat') && /tabChatBtn: \$\('tab-chat-btn'\)/.test(mainJs),
  'the tab button and the tab body are not separately addressable',
);
check(
  'the conversation tab is hidden when no plugin fills the slot',
  // The availability setter is what owns it, and it must both hide the button and
  // move the panel off the tab - a hidden tab whose body is still showing would
  // leave the strip claiming 待办 while the conversation's box stayed on screen.
  /function setChatTabAvailable\(available\)/.test(mainJs) &&
    /btn\.classList\.toggle\('hidden', !available\)/.test(mainJs) &&
    /if \(btn\.classList\.contains\('active'\)\) switchTab\('todos'\)/.test(mainJs),
  'the tab button is not hidden and the panel is not moved off it',
);
check(
  'switchTab re-resolves the slot, which is what decides the tab exists',
  // The switch must NOT refuse itself when the tab is hidden: reading the installed
  // list is what resolves the slot, and the same read is what would discover the
  // plugin that un-hides the tab. A guard that refused the switch would deadlock -
  // the refused call was the only thing that could have re-read the list.
  /if \(name === 'chat'\) \{\s*refreshChatSlot\(\);\s*\}/.test(mainJs) &&
    !/name === 'chat' && els\.tabChatBtn/.test(mainJs),
  'switchTab refuses the conversation tab instead of re-resolving the slot',
);
// The plugin slot must be re-read while the tab strip is ON SCREEN, because both
// observable changes happen outside this process: the market installs, updates,
// disables and removes plugins, and Go's State carries none of that.
//
// The bound is the PANEL being open, not the tab being hidden. The first version
// polled only while the tab was hidden, which missed the direction the user
// reported (the plugin disabled -> the tab must go away): in that case the tab is
// visible, so the watcher was not running at all.
check(
  'the conversation tab is watched while the panel is open',
  // The pieces are asserted separately rather than as one span: the interval call
  // and the period constant sit a few lines apart, and a single regex over both
  // would be brittle against any comment added between them - which is the kind of
  // assertion that gets "fixed" by loosening it until it stops checking anything.
  /function setChatTabWatch\(wanted\)/.test(mainJs) &&
    /chatTabWatchTimer = setInterval\(/.test(mainJs) &&
    /\}, CHAT_TAB_WATCH_MS\);/.test(mainJs) &&
    /clearInterval\(chatTabWatchTimer\)/.test(mainJs) &&
    /setChatTabWatch\(!!layout\.panelOpen\)/.test(mainJs),
  'nothing re-reads the installed list while the tab strip is on screen',
);
check(
  'the watch is NOT bound to the tab being hidden',
  // Guards the specific regression: binding it to `!available` leaves the
  // "plugin disabled -> hide the tab" direction unwatched, because the tab is
  // visible in exactly that case.
  !/setChatTabWatch\(!available\)/.test(mainJs),
  'the watcher is bound to the tab being hidden, which misses the hide direction',
);
check(
  'opening the panel re-reads the installed list at once',
  // Without this the first read after a change made while the panel was closed
  // waits out a whole period, so the tab can be visibly wrong for up to 1.5s.
  //
  // The transition is asserted via its two halves rather than as one span, because
  // the comment explaining it sits between the brace and the call.
  /panelWasOpen === false/.test(mainJs) &&
    /const panelWasOpen = panelOpenState;/.test(mainJs) &&
    /panelOpenState = !!layout\.panelOpen;/.test(mainJs),
  'the open transition does not refresh the slot immediately',
);
// An UPDATE keeps the plugin id and replaces the files behind it, so an id-only
// reuse test leaves the previous version's document running - the market says
// "updated" and the pet keeps drawing the old interface.
check(
  'the plugin slot frame is rebuilt when the plugin VERSION changes',
  /pluginHost\.slotFrame\.version === plugin\.version/.test(mainJs),
  'an updated plugin would keep running its previous document',
);
check(
  'the floating surface frames are rebuilt when the version changes',
  /this\.surfaceVersion !== \(view\?\.version \|\| ''\)/.test(pluginHostJs),
  'an updated plugin would keep running its previous surface document',
);
check(
  'the iframe carries the version it was loaded from',
  /frame\.dataset\.pluginVersion = this\.version/.test(pluginHostJs),
  'the loaded version is not observable from the DOM',
);
// There must be no leftover floating-card element: the surface is a tab, and a
// stray #chat would be an unpositioned box with no region to be cropped to.
check(
  'no leftover conversation card element',
  !htmlIds.has('chat') && !/applyChatCard/.test(mainJs),
  'the removed card element or its positioning code is still present',
);
// The conversation tab's "待办" jump is still reported through Go, but the button
// that made the call lived in the host's own conversation UI (chat.js) and was
// deleted with it. The conversation is a plugin now: its own page decides whether
// to offer the jump, and it reaches the host through the same Go binding. What is
// checked here is that the binding still exists to be called, and that it is still
// typed as a layout call.
check(
  'the "待办" jump is still available to the conversation plugin',
  exported.has('OpenTodoPanelFromChat') && typed.get('OpenTodoPanelFromChat') === 'main.Layout',
  'the jump binding is gone, so a plugin cannot send the user to the todo tab',
);
check(
  'Go reports which tab the frontend should show',
  /eventChatOpen, map\[string\]any\{"open":/.test(
    fs.readFileSync(path.join(root, 'app_llm.go'), 'utf8'),
  ),
  'the event carries no direction',
);

// --- 6. Double click is detected in the pointer stream ---------------------
// A `dblclick` listener would also be defensible, but the detection lives in
// endDrag so the single- and double-click paths cannot disagree; these checks
// pin that arrangement and, crucially, that the second click does NOT let the
// pending single click fire.
check(
  'the double click is detected from the pointer stream',
  /now - lastClickAt < DOUBLE_CLICK_MS/.test(mainJs),
  'no double-click interval test',
);
check(
  'a single click is deferred so a double click cannot flash the panel',
  /pendingClick\s*=\s*setTimeout/.test(mainJs) && /clearTimeout\(pendingClick\)/.test(mainJs),
  'the click/double-click split is missing',
);
// The double-click branch is one contiguous block, so the checks anchor on the
// branch itself rather than on a character budget: a budget silently starts
// passing once unrelated code is inserted between the two lines, and it fails
// when a comment grows. Both of those happened while this feature was written.
const dblStart = mainJs.indexOf('if (now - lastClickAt < DOUBLE_CLICK_MS)');
let dbl = '';
if (dblStart >= 0) {
  // Up to the closing brace of the branch, which is the first line that starts
  // with the branch's own indentation followed by "}".
  const rest = mainJs.slice(dblStart);
  const end = rest.indexOf('\n        }');
  dbl = end < 0 ? rest : rest.slice(0, end);
}
check(
  'the double-click branch was found',
  dbl.length > 0,
  'the branch is no longer in main.js',
);
check(
  'the second click cancels the pending single click before opening the surface',
  /clearTimeout\(pendingClick\)/.test(dbl) && /openChatPluginSurface\(\)/.test(dbl) &&
    dbl.indexOf('clearTimeout(pendingClick)') < dbl.indexOf('openChatPluginSurface()'),
  'the double-click branch does not cancel the pending click first',
);
check(
  'the double-click branch returns rather than scheduling a toggle',
  /\breturn;/.test(dbl) && !/TogglePanel/.test(dbl),
  'the double-click branch may also toggle the panel',
);
check(
  'the deferred single click is what toggles the panel',
  /pendingClick = setTimeout\([\s\S]{0,200}App\.TogglePanel\(\)/.test(mainJs),
  'the single click no longer toggles the panel',
);
// A double click asks the conversation plugin for its floating surface, which is
// the whole point of the redesign: the pet's own live overlay is gone, and what
// opens is the plugin's window. The call goes through Go rather than straight to
// the surface binding, because the slot's owner has to be resolved from the
// on-disk install list (and refused when a question owns the slot).
check(
  'a double click asks the conversation plugin for its surface',
  /function openChatPluginSurface\(\)[\s\S]{0,400}App\.OpenChatPluginSurface\(\)/.test(mainJs),
  'the double click does not reach the plugin surface',
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
  /r\.ellipse === true \? '50%'/.test(pluginHostJs) &&
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

// --- 9b. The rest is GATED on the record ------------------------------------
// A finished focus phase parks the break until its record is answered, and the
// prompt that asks for it cannot be waved away. Both halves are load-bearing:
// without the gate an undescribed session banks a break, and without the
// non-dismissable prompt the timer could be parked with nothing on screen
// explaining why.
check(
  'a finished focus does not auto-start the rest',
  /if ended == store\.PhaseFocus \{\s*\n\s*st\.RunState = store\.RunStopped/.test(goPomodoroEngine),
  'the break starts before the record is answered',
);
check(
  'answering the record is what starts the rest',
  /st = a\.startParkedPhaseLocked\(st\)/.test(goAppPomodoro),
  'the timer is parked with nothing that starts it',
);
check(
  'the summary prompt cannot be dismissed',
  // The guard is on the bubble kind AND on a record actually being pending, so an
  // unrelated summary bubble (a stale view) cannot make the UI unresponsive.
  /b\.Kind == BubbleSummary && pending/.test(goAppPomodoro),
  'the prompt can be dismissed, so the gate can be bypassed',
);
check(
  'the plan prompt cannot be dismissed either',
  // The plan question is the gate on the FOCUS, the way the summary prompt is the
  // gate on the break: waving it away would strand the timer behind a question
  // nobody can see. Its own pending flag is read, so a plan bubble left over after
  // the question was answered (by skipping, or by giving the session up) still
  // closes like any other bubble.
  /b\.Kind == BubblePlan && planPending/.test(goAppPomodoro) &&
    /planPending := a\.pendingPlanWaiting\(\)/.test(goAppPomodoro) &&
    /func \(a \*App\) pendingPlanWaiting\(\) bool \{[\s\S]{0,220}return a\.store\.Pomodoro\(\)\.PlanPending/.test(
      goAppPomodoro,
    ),
  'the plan question can be dismissed, or its pending flag is read the wrong way',
);
check(
  'the pending flags are read before the window lock is taken',
  // setBubbleLocked and its friends need a.mu, while the pending flags live behind
  // pomoMu (whose holder calls into the bubble helpers) - so reading them inside the
  // a.mu section would invert the two locks and deadlock for real (design.md 2.2).
  /pending := a\.pendingSummaryWaiting\(\)[\s\S]{0,220}planPending := a\.pendingPlanWaiting\(\)[\s\S]{0,220}a\.mu\.Lock\(\)/.test(
    goAppPomodoro,
  ),
  'a pending flag is read while holding the window lock, which inverts the lock order',
);
check(
  'the frontend does not dismiss the summary prompt either',
  // Escape and dragging both reach DismissBubble; the backend refuses, but the
  // frontend must not redraw a bubble that is still there. The guard names BOTH
  // gated kinds (summary and plan) in both places, so this asserts the summary is
  // still among them rather than pinning the whole expression.
  /bubble\.kind !== 'summary'/.test(mainJs) &&
    /if \(bubble && bubble\.kind !== 'summary' && bubble\.kind !== 'plan'\) \{/.test(mainJs),
  'a drag or Escape closes the summary prompt from the frontend',
);
check(
  'the frontend does not dismiss the plan prompt either',
  // The plan prompt is protected by the same two guards, in both places Escape and
  // dragging reach. Both kinds are spelled out rather than shared in a helper,
  // because the guard is also what stops the panel fall-through in the Escape chain.
  /bubble\.kind !== 'summary' && bubble\.kind !== 'plan'/.test(mainJs) &&
    /if \(bubble && bubble\.kind !== 'summary' && bubble\.kind !== 'plan'\) \{/.test(mainJs) &&
    /bubble\?\.kind === 'summary' \|\| bubble\?\.kind === 'pick' \|\| bubble\?\.kind === 'plan'/.test(
      mainJs,
    ),
  'Escape or a drag closes the plan prompt from the frontend',
);
check(
  'a reminder bubble is still dismissable',
  // Making the summary prompt stick must not make every bubble stick.
  /if \(b\.kind !== 'reminder'\) return;/.test(mainJs),
  'reminders no longer auto-hide',
);
// --- 9c. The plugin surface must not eat the question -----------------------
//
// The surface and the speech bubble share one slot, and OpenPluginSurface TAKES
// the slot rather than sharing it, so opening the conversation over the summary
// prompt deleted it - leaving the timer parked behind a question that was no
// longer on screen. Both halves are needed: the backend must refuse, or a stale
// or non-frontend caller still destroys the prompt; the frontend must check too,
// or the refusal arrives as an error toast that cannot be seen, because the toast
// lives inside the panel that is necessarily closed while the bubble is up.
check(
  'the backend refuses to open the surface over an unanswered question',
  /func \(a \*App\) OpenPluginSurface\(pluginID string\) \(Layout, error\) \{[\s\S]{0,1400}if kind := a\.unansweredQuestionKind\(\); kind != BubbleNone \{/.test(
    fs.readFileSync(path.join(root, 'app_plugin_surface.go'), 'utf8'),
  ),
  'OpenPluginSurface takes the slot from a question that is still waiting',
);
check(
  'the refusal names which question is in the way',
  // "choose a todo" and "write the summary" are not the same instruction, so one
  // shared string would tell the user to do the wrong thing. The sentence lives in
  // pluginSurfaceBlocked now - the surface's own wording, written in the style the
  // retired overlay's questionOwnsTheSlot set.
  /func pluginSurfaceBlocked\(kind string\) string \{/.test(
    fs.readFileSync(path.join(root, 'app_plugin_surface.go'), 'utf8'),
  ) &&
    /BubblePick/.test(goAppPomodoro.slice(goAppPomodoro.indexOf('func questionOwnsTheSlot'))) &&
    /unansweredQuestionKind/.test(goAppPomodoro),
  'the refusal does not say which question is holding the slot',
);
check(
  'the frontend turns the refused gesture into "go answer it"',
  // A silent no-op would look like the pet ignoring a double click; the caret
  // landing in the box is the explanation, and a toast would be invisible here.
  /function bubbleAwaitingAnswer\(\)/.test(mainJs) &&
    /if \(bubbleAwaitingAnswer\(\)\) \{\s*\n\s*focusAnswer\(\);/.test(mainJs) &&
    /function focusAnswer\(\)/.test(mainJs) &&
    /els\.bubbleInput\.focus\(\)/.test(mainJs),
  'a double click on the pet silently does nothing while the summary prompt is up',
);
check(
  'the surface still takes the slot from a notification and the panel',
  // Deferring to a question must not make the surface defer to everything: a
  // reminder asks nothing, and the panel is the record view. The guard is the one
  // in OpenPluginSurface, and it is expressed as "a question kind is waiting" -
  // BubbleReminder is not one, so it is still replaced.
  /unansweredQuestionKind\(\); kind != BubbleNone/.test(
    fs.readFileSync(path.join(root, 'app_plugin_surface.go'), 'utf8'),
  ) &&
    /func \(a \*App\) unansweredQuestionKind\(\) string \{/.test(goAppPomodoro) &&
    /BubbleReminder/.test(
      goAppPomodoro.slice(goAppPomodoro.indexOf('func (a *App) unansweredQuestionKind')),
    ) === false,
  'the surface now defers to reminders too, so the conversation can be blocked',
);
check(
  'the picker cannot be eaten by the popup either',
  // All three unanswered kinds are protected, not just the summary: the picker is
  // the only way to start a focus phase, and the plan prompt is its continuation,
  // so displacing either silently loses the user's half-made decision.
  /func \(a \*App\) unansweredQuestionKind\(\) string \{/.test(goAppPomodoro) &&
    /if a\.bubble\.Kind == BubblePick \|\| a\.bubble\.Kind == BubbleSummary \|\| a\.bubble\.Kind == BubblePlan \{/.test(
      goAppPomodoro,
    ),
  'only the summary prompt is protected, so the picker can still be displaced',
);
check(
  'the refusal names all three questions differently',
  // "choose a todo", "write the summary" and "write the plan" are three different
  // things to go and do, so one shared sentence would send the user to the wrong
  // one. Go's own tests assert the three strings are pairwise distinct; what is
  // checked here is that the plan branch exists at all.
  /case BubblePlan:/.test(goAppPomodoro.slice(goAppPomodoro.indexOf('func questionOwnsTheSlot'))),
  'the plan question has no explanation of its own for the refusal',
);
// --- 9d. Enter confirms the summary -----------------------------------------
//
// The bubble is not a <form>, so without an explicit handler Enter inserts a
// newline and the user has to reach for the mouse to release the gate. The IME
// check is the load-bearing part: typing Chinese, the Enter that commits a
// candidate would otherwise save a half-finished summary.
check(
  'Enter confirms the summary',
  /els\.bubbleInput\.onkeydown = \(e\) => \{/.test(mainJs) &&
    /e\.key === 'Enter' && !e\.shiftKey && !e\.isComposing/.test(mainJs) &&
    /saveSummary\(b\.recordId, els\.bubbleInput\.value\)/.test(mainJs),
  'Enter does not confirm the summary, so the gate needs the mouse',
);
check(
  'Enter confirms the plan too',
  // The plan prompt is a second gate, and it must be answerable from the keyboard
  // for the same reason: the bubble is not a <form>, so without this handler Enter
  // would insert a newline and the timer would stay parked until the user reached
  // for the mouse. Both buttons go through the same call, with the empty string as
  // a real answer - "skip" is not a dismissal.
  /startWithPlan\(els\.bubbleInput\.value\)/.test(mainJs) &&
    /startWithPlan\(''\)/.test(mainJs) &&
    /App\.StartPomodoroWithPlan\(plan\)/.test(mainJs),
  'Enter does not answer the plan prompt, or "skip" cannot start the focus',
);
check(
  'the plan prompt advertises Enter',
  // Written at RUNTIME rather than in index.html: the textarea is shared with the
  // summary prompt, whose own attribute is the one in the markup, so the plan
  // branch has to set its hint when it renders.
  /placeholder = '这件事是…（可留空，Enter 开始）'/.test(mainJs) &&
    /placeholder="写点什么…（Enter 保存）"/.test(html),
  'the plan placeholder does not say what Enter does, or the summary one was overwritten',
);
check(
  'the summary prompt gets its own placeholder back',
  // The two prompts share one textarea, so the summary branch must restore its hint
  // whenever it renders after the plan branch.
  /placeholder = '写点什么…（Enter 保存）'/.test(mainJs),
  'the plan placeholder leaks into the summary prompt',
);
check(
  'the summary prompt advertises Enter',
  /（Enter 保存）/.test(html),
  'the placeholder does not say what Enter does',
);
check(
  'the Enter handler cannot leak into another bubble kind',
  // It is installed per render, so a stale handler from the previous summary
  // prompt must be cleared or Enter would save a record while the reminder is up.
  /els\.bubbleInput\.onkeydown = null;/.test(mainJs),
  'a stale Enter handler survives into the next bubble',
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
  'hiding is refused while a summary is waiting',
  // The summary prompt is the gate on the break; hiding it would park the timer
  // forever with nothing on screen explaining why.
  /if a\.pendingSummaryWaiting\(\) \{[\s\S]{0,140}return errSummaryBlocksHiding/.test(goHotkey),
  'the pet can be hidden behind the summary prompt, stranding the pomodoro',
);
check(
  'hiding is refused while the plan is waiting',
  // The plan prompt is the gate on the FOCUS, and it has one extra edge over the
  // summary: the records tab can edit a summary, but NOTHING in the panel can
  // answer a plan question - so hiding the pet would be a dead end, not a nuisance.
  /var errPlanBlocksHiding = errors\.New\(/.test(goHotkey) &&
    /if a\.pendingPlanWaiting\(\) \{[\s\S]{0,140}return errPlanBlocksHiding/.test(goHotkey),
  'the pet can be hidden behind the plan prompt, stranding the pomodoro',
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
check(
  'the settings switch is wired and its keys are shown',
  htmlIds.has('hotkey-enabled') &&
    /hotkeyEnabled: \$\('hotkey-enabled'\)/.test(mainJs) &&
    /App\.SaveHotkeySettings\(\{ enabled: els\.hotkeyEnabled\.checked \}\)/.test(mainJs) &&
    /function renderHotkeySettings\(\)/.test(mainJs) &&
    /function wireHotkeySettings\(\)/.test(mainJs) &&
    /wireHotkeySettings\(\);/.test(mainJs),
  'the shortcut switch is rendered but never sent, or never wired',
);
check(
  'the key labels come from Go rather than being written in the page',
  // The advertised keys and the registered keys must be the same value, or the
  // panel can tell the user to press something that was never registered.
  /els\.hotkeyShow\.textContent = h\.show/.test(mainJs) &&
    /els\.hotkeyHide\.textContent = h\.hide/.test(mainJs) &&
    /Hotkeys\s+desktop\.HotkeyView/.test(mainGo),
  'the page hard-codes the shortcuts, so it can disagree with what was registered',
);
check(
  'a refused registration is surfaced rather than silently swallowed',
  // "I pressed it and nothing happened" is the failure this exists to prevent.
  /h\.problems\?\.length/.test(mainJs) &&
    /未能生效/.test(mainJs) &&
    /Problems \[\]string `json:"problems,omitempty"`/.test(goDesktopHotkey),
  'a hot key the OS refused is reported nowhere the user can see',
);
check(
  'reminders are suppressed while the pet is hidden',
  // A bubble is painted inside the window, so on a hidden pet it is a
  // notification nobody can see. The question reveal is the ONE exception, and
  // it is deliberate: a gate that parks the timer is not a notification.
  /if a\.petHidden\(\) \{[\s\S]{0,120}reminder: suppressed \(the pet is hidden\)/.test(
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
check(
  'a parked record is re-offered after a restart',
  // Otherwise the gate becomes a dead end after any relaunch: the record survives
  // on disk but the bubble does not.
  /PendingRecordID != "" \{\s*\n\s*a\.DebugLog\("pomodoro: a record is still waiting/.test(goAppPomodoro),
  'a restart leaves the timer parked with no prompt',
);
check(
  'a hidden pet is brought back for a question the timer is parked behind',
  // Refusing to hide WHILE a question waits only covers one order. The other one
  // - hide first, then let a phase run out - would draw the prompt into an
  // SW_HIDEd window: the timer parked forever, no pixels explaining why, and the
  // only way out a shortcut the user has to remember.
  /func \(a \*App\) revealForQuestion\(\)/.test(goHotkey) &&
    /a\.revealForQuestion\(\)[\s\S]{0,220}Title:     "这一个番茄做了什么？"/.test(goAppPomodoro) &&
    /a\.revealForQuestion\(\)[\s\S]{0,220}Title:     "接下来想专注做什么？"/.test(goAppPomodoro),
  'a question can be raised into a hidden window, stranding the timer invisibly',
);
check(
  'the pet is revealed WITHOUT being activated',
  // The prompts deliberately do not steal the keyboard (the user is very likely
  // typing elsewhere), so the reveal must not either: an activated pet would
  // swallow their next keystroke into its own input box.
  /func ShowNoActivate\(w Window\) error \{\s*\n\s*const swShowNoActivate = 4/.test(
    fs.readFileSync(path.join(root, 'internal/desktop/desktop_windows.go'), 'utf8'),
  ) &&
    /return desktop\.ShowNoActivate\(hwnd\)/.test(goHotkey) &&
    !/desktop\.Show\(hwnd\)[\s\S]{0,200}question reveal/.test(goHotkey),
  'the reveal activates the pet and steals the keyboard from whatever the user was typing in',
);
check(
  'answering a question hides the pet again',
  // The reveal is temporary by definition: the whole point of hiding is to be
  // left alone, so the pet has to go back the moment the gate is released. Every
  // path that clears a gate calls it - the summary, the plan, giving the session
  // up and switching the feature off.
  /func \(a \*App\) hideAfterQuestion\(\)/.test(goHotkey) &&
    (goAppPomodoro.match(/a\.hideAfterQuestion\(\)/g) || []).length >= 4,
  'the pet stays on screen after the question that brought it back is answered',
);
check(
  'an explicit show is not undone by answering a question',
  // "I asked for it back" must not be reversed later by a prompt being answered.
  // Both routes back have to settle the debt: the shortcut/tray (ShowPet) and
  // opening the panel, which is one of the documented ways back.
  /a\.hidden = false\s*\n\s*a\.revealedForQuestion = false/.test(goHotkey) &&
    /a\.hidden = false\s*\n\s*a\.revealedForQuestion = false/.test(mainGo),
  'answering a prompt hides a pet the user deliberately brought back',
);
check(
  'a failed reveal does not record a debt',
  // Nothing changed on screen, so nothing may change in the state: recording the
  // debt anyway would hide a pet that was never shown.
  /if err := a\.setQuestionVisibility\(true\); err != nil \{[\s\S]{0,420}a\.revealedForQuestion = true/.test(goHotkey),
  'a reveal that failed still claims the pet is on screen only for a question',
);

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
  'the permission vocabulary is the expected seventeen, and by `net.ws` + `llm.borrow` + `process` + `net.credentials` + `plugin.share` + `timer.control`',
  // The count is asserted so that adding a permission is a VISIBLE two-place edit here,
  // and so that the "no background permission" decision cannot be quietly undone.
  //
  // Seventeen, not eighteen: a headless frame is a manifest FIELD gated on an existing
  // permission (`events` for host events, `net.ws` for a socket a plugin holds open,
  // `net.fetch` for a polling plugin) rather than a permission of its own. A permission
  // with no method behind it would be a switch the user is asked about that does
  // nothing, which is the invariant the pet's own TestCapabilityTableIsComplete
  // enforces. The negative assertion below is the same decision stated from the other
  // side.
  //
  // The five additions each have a METHOD behind them: `net.ws` gates ws.open/send/close/
  // state, `llm.borrow` gates llm.profile (which is what tells a plugin's own UI where
  // its model settings come from), `process` gates process.launch (which starts one
  // .exe the user typed the path of, and does not wait for it), `net.credentials` is
  // the SECOND gate of net.fetch (recorded in that entry's `also`, which the pet's
  // completeness test counts), and `plugin.share` gates share.set/get/remove - the tray
  // two plugins use to hand each other a job without either one calling the other.
  // `timer.control` gates timer.start/pause/resume/stop: driving the timer, as opposed
  // to watching it, which is what timer.read already covered.
  (() => {
    const list = capsGo.slice(capsGo.indexOf('var AllowedPermissions'), capsGo.indexOf('func IsAllowedPermission'));
    const entries = [...list.matchAll(/Perm[A-Za-z]+,/g)];
    return entries.length === 17 && /PermAudio,/.test(list) && /PermNetWS,/.test(list) &&
      /PermLLMBorrow,/.test(list) && /PermProcess,/.test(list) &&
      /PermNetCredentials,/.test(list) && /PermPluginShare,/.test(list) &&
      /PermTimerControl,/.test(list) &&
      !/PermBackground/.test(capsGo);
  })(),
  'the permission vocabulary is not the expected seventeen entries (net.ws + llm.borrow + process + net.credentials + plugin.share + timer.control appended, background deliberately not)',
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
      fs.readFileSync(path.join(root, 'app_pomodoro.go'), 'utf8'),
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

// --- 13. The records archive is folded by day -------------------------------
// A flat list of every session ever put the newest records - the only ones anyone
// reads - on top of an unbounded history. The fix is only correct if ALL of it
// holds: the grouping, the fold, and the fact that the fold survives a re-render
// (every todo toggle pushes a whole new state snapshot through applyState).
check(
  'the records list is grouped into collapsible days',
  // The rows are built in JS, not markup, so the class is asserted on main.js and
  // the styling on the stylesheet.
  /renderRecordDay\(/.test(mainJs) &&
    /className = 'record-day'/.test(mainJs) &&
    /\.record-day-list\s*\{/.test(css) &&
    /\.record-day-head\s*\{/.test(css),
  'the day grouping or its styles are missing',
);
check(
  'the newest day is the one that starts open',
  // Opening every group would be the flat list again; opening none would hide the
  // data behind a click on first use.
  /openRecordDays = new Set\(groups\.length \? \[groups\[0\]\.key\] : \[\]\)/.test(mainJs),
  'the first render does not open the newest day',
);
check(
  'a day the user collapsed stays collapsed across re-renders',
  // The set is module state, not the DOM: applyState re-renders records on every
  // state push, so a fold kept in the DOM would spring back open constantly.
  /let openRecordDays = null/.test(mainJs) &&
    /seenRecordDays/.test(mainJs) &&
    /openRecordDays\.has\(group\.key\)/.test(mainJs),
  'the fold state is not remembered outside the DOM',
);
check(
  'a newly appeared day opens by itself',
  // A session finishing while the app is open (or the first record of a new day)
  // must not land inside a closed group where the user cannot see it.
  /!seenRecordDays\.has\(group\.key\)[\s\S]{0,80}openRecordDays\.add\(group\.key\)/.test(mainJs),
  'a day that appears later would stay folded, hiding the new record',
);
check(
  'the per-session row no longer repeats the date',
  // The date is on the group header, so a per-row "12鏈?1鏃?18:00" is noise. What
  // is left must be the clock time.
  /formatClock\(rec\.endedAt\)/.test(mainJs) && !/formatStamp/.test(mainJs),
  'the row still carries the day, or the clock formatter is unused',
);
check(
  'the plan bubble has its own hint element',
  // The summary prompt shows what the user set out to do above the box where they
  // type what happened. It needs an element of its own - reusing the todo line
  // would make one of the two sentences disappear - and it must sit between the
  // todo hint and the list so the card reads in order.
  htmlIds.has('bubble-plan') &&
    /bubblePlan: \$\('bubble-plan'\)/.test(mainJs) &&
    /<p id="bubble-plan" class="bubble-plan hidden"><\/p>\s*\n\s*<ul id="bubble-list"/.test(html) &&
    /<p id="bubble-todo"[^>]*><\/p>\s*\n\s*<p id="bubble-plan"/.test(html),
  'the plan hint line is missing, or it is not between the todo line and the list',
);
check(
  'the plan hint line is reset on every render',
  // It is shared with the plan prompt (which shows no plan line), so a render that
  // did not hide it would leave the previous summary's plan on screen.
  /els\.bubblePlan\.classList\.add\('hidden'\)/.test(mainJs) &&
    /els\.bubblePlan\.textContent = `计划：\$\{b\.plan\}`/.test(mainJs),
  'the plan line is not reset, so it can leak between bubbles',
);
check(
  'the records row shows the plan above the actual result',
  // The two-line layout, in order: what was planned first, what happened second.
  // Without a plan the first line falls back to the todo name, which is what keeps
  // records written before this feature looked the way they always did.
  /const planText = rec\.plan \|\| rec\.todoTitle \|\| '未关联待办'/.test(mainJs) &&
    /`计划：\$\{planText\}`/.test(mainJs) &&
    /`实际：\$\{rec\.summary\}`/.test(mainJs) &&
    /'实际：（未填写）'/.test(mainJs),
  'the record row is not the plan/actual pair, or the fallback for old records is gone',
);
check(
  'the records editor edits both lines in one write',
  // Two textareas (plan first, then actual) saved through ONE binding: saving them
  // separately could leave a record half-edited if the second call failed.
  /App\.EditPomodoroRecord\(rec\.id, planInput\.value, summaryInput\.value\)/.test(mainJs) &&
    (mainJs.match(/className = 'record-input'/g) || []).length === 2 &&
    /planInput\.value = rec\.plan \|\| ''/.test(mainJs) &&
    /summaryInput\.value = rec\.summary \|\| ''/.test(mainJs),
  'the editor does not offer both fields, or it saves them in two calls',
);
check(
  'the two new bindings exist in the generated module and its types',
  // The generated files are committed, so a binding added on the Go side but not
  // regenerated here would fail at runtime as "App.StartPomodoroWithPlan is not a
  // function" - with nothing in the panel to explain it.
  //
  // The record's plan is `plan?: string` rather than a required `plan: string`
  // because the Go field carries `omitempty`, and that is exactly what the Wails
  // generator does with every omittable string (the sibling todoId/todoTitle are
  // optional the same way). Asserting the required form here would be asserting
  // against the generator, not against the wiring.
  /export function StartPomodoroWithPlan\(arg1\)/.test(appJs) &&
    /export function EditPomodoroRecord\(arg1, arg2, arg3\)/.test(appJs) &&
    /StartPomodoroWithPlan\(arg1:string\):Promise<main\.State>/.test(appDts) &&
    /EditPomodoroRecord\(arg1:string,arg2:string,arg3:string\):Promise<main\.State>/.test(appDts) &&
    /plan\?: string;/.test(modelsTs) &&
    /planEditedAt\?: any;/.test(modelsTs) &&
    // And the constructor really assigns them, or the model would type-check and
    // still drop the fields when a record round-trips through the frontend.
    /this\.plan = source\["plan"\]/.test(modelsTs) &&
    /this\.planEditedAt = this\.convertValues\(source\["planEditedAt"\], null\)/.test(modelsTs),
  'a new binding or model field is missing from the generated types',
);
check(
  'the records tab still scrolls inside the panel',
  // Same rule as the settings tab: the panel's rectangle comes from the layout and
  // the native region is cut to it, so the records list must scroll, never grow.
  /\.record-list\s*\{[^}]*overflow-y:\s*auto/.test(css) &&
    !/recordsList\.style\.(width|height)\s*=/.test(mainJs),
  'records sizes itself or grows the panel',
);

// --- 14. One scrollbar style across the panel -------------------------------
// The settings tab and the todo list had private scrollbar rules while the records
// list and the chat log were left on WebView2's default opaque grey widget, which
// on a dark 380px panel reads as a different application.
check(
  'the scrollable surfaces share one scrollbar rule',
  // Four selectors: #tab-plugins went with the plugin tab, and .voice-scroll went with
  // the voice studio (speech is a plugin now, and its workshop is a document inside the
  // MARKET's window - so the pet has no voice surface to scroll). The group is still
  // ONE rule rather than four copies, which is what the two checks below pin - the list
  // here is the membership, they are the shape.
  /\.todo-list,\s*\n\.record-list,\s*\n\.chat-log,\s*\n#tab-settings\s*\{/.test(css),
  'the scrollable surfaces do not share one scrollbar rule',
);
check(
  'the records list and the chat log are on that rule',
  // These two are the ones the user reported as still default-styled.
  /\.record-list::-webkit-scrollbar-thumb/.test(css) &&
    /\.chat-log::-webkit-scrollbar-thumb/.test(css),
  'a surface is still on the native scrollbar',
);
check(
  'the private scrollbar copies are gone',
  // Leaving them would mean two competing definitions of the same widget. The
  // test is on the SHAPE of each scrollbar rule - a rule that names only one
  // surface is a private copy - rather than on a regex, because `#tab-settings`
  // legitimately appears as the last selector of the consolidated group.
  (() => {
    const rules = [...css.matchAll(/([^{}]*?)\{/g)]
      .map((m) => m[1])
      .filter((sel) => sel.includes('-webkit-scrollbar'));
    return rules.length > 0 && rules.every((sel) => sel.split(',').length >= 4);
  })(),
  'a per-element scrollbar copy survived the consolidation',
);
check(
  'the scrollbar is translucent and trackless',
  // It should read as a position hint, not as a control: no opaque track, no
  // fixed light grey, and narrow enough not to eat the 380px panel.
  /\.record-list::-webkit-scrollbar-track[\s\S]{0,400}background:\s*transparent/.test(css) &&
    /scrollbar-color:\s*rgba\(255,\s*255,\s*255,\s*0\.\d+\)\s*transparent/.test(css),
  'the scrollbar has an opaque track or a hard-coded colour',
);

// --- 15. The panel's close button centres its glyph -------------------------
// "×" is a math symbol whose ink sits high in an ascent-heavy box, so the default
// button metrics drew it visibly off-centre. The correction is a MEASURED padding,
// and both halves are load-bearing: flex centring alone leaves the ink 2.5px low,
// and the inherited button padding leaves it 1.5px high.
check(
  'the close button centres its glyph with flex, not line metrics',
  /\.icon-btn\s*\{[^}]*display:\s*flex[^}]*align-items:\s*center[^}]*justify-content:\s*center/.test(
    css,
  ),
  'the close button does not leave the glyph to the font baseline',
);
check(
  'the close button carries the measured optical offset',
  // Measured in the real engine: 18px + inherited 7px padding -> +1.5px;
  // 17px + padding 0 -> +2.5px; 17px + 5px bottom padding -> 0.00px.
  /\.icon-btn\s*\{[^}]*font-size:\s*17px/.test(css) &&
    /\.icon-btn\s*\{[^}]*padding:\s*0 0 5px/.test(css),
  'the glyph offset was changed without re-measuring',
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
  'the pet has no voice studio tab and no speech tab',
  // Four tabs: plugin management is the market program's window, and the pet's own
  // plugin surfaces are mounted by main.js rather than by a tab. A stray fifth tab
  // would be a duplicate UI competing with the market.
  !htmlIds.has('tab-voice') && (html.match(/data-tab="/g) || []).length === 4,
  'the pet grew a speech tab, or the tab count changed',
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

// The plugin's own interface, hosted by the market.
check(
  'the market can host a plugin interface',
  // It serves the plugin's files and answers its requests through the SAME shared
  // implementations the pet uses, so the sandbox, the traversal defence and the gate
  // have one implementation rather than two that can drift.
  /plugins\.ServePluginAsset/.test(marketPluginHost) &&
    /PluginRequest/.test(marketPluginHost),
  'the market cannot host a plugin interface, so the workshop has nowhere to run',
);
check(
  'the hosted interface is sandboxed to allow-scripts alone',
  // Adding allow-same-origin would hand every installed plugin the market page's DOM,
  // which is the one thing the sandbox exists to prevent. The negative assertion is on
  // the ATTRIBUTE VALUE rather than on the word, because the comment above the line
  // necessarily names the flag it must not have.
  /setAttribute\('sandbox', 'allow-scripts'\)/.test(marketPluginUI) &&
    !/sandbox',\s*'[^']*allow-same-origin/.test(marketPluginUI) &&
    !/frame\.setAttribute\('sandbox'[^;]*allow-same-origin/.test(marketPluginUI),
  'the hosted plugin can reach the host page',
);
check(
  'the market mounts an interface only for an installed and ENABLED plugin',
  // Switching a plugin off must stop it RUNNING, not merely stop it being listed: an
  // interface left on screen would keep its document alive with its permissions intact.
  // Round 7 made the gate a named condition, because the workspace BOX has to disappear
  // with the frame - a caption strip over an empty area is furniture around nothing.
  /const hasInterface = Boolean\(inst && inst\.Manifest && inst\.Enabled && inst\.Manifest\.entry\)/
    .test(marketMainJs) &&
    /if \(hasInterface\) \{\s*mountPluginUI\(inst, els\)/.test(marketMainJs) &&
    /clearPluginUI\(els\)/.test(marketMainJs) &&
    /show\(els\.panelInterface, hasInterface\)/.test(marketMainJs),
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
    // …and the host has to actually say it, on both channels.
    /plugin-asset\/\$\{id\}\/\$\{entry\}\?theme=\$\{windowTheme\(\)\}/.test(marketPluginUI) &&
    /theme: current \? current\.theme : windowTheme\(\)/.test(marketPluginUI) &&
    /export function setFrameTheme\(/.test(marketPluginUI) &&
    /setFrameTheme\(theme\)/.test(marketMainJs) &&
    /\[data-theme="light"\]/.test(marketCss),
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
  // id in the install sheet. The two tables cannot be shared (the market is a
  // separate program with its own bundle), so they are compared here: the Go const
  // block's string values against the market's label map keys. This is what makes
  // adding a permission a two-place edit instead of a consent prompt nobody can read.
  (() => {
    const consts = capsGo.slice(capsGo.indexOf('const ('), capsGo.indexOf('// AllowedPermissions'));
    const allowed = [...consts.matchAll(/Perm[A-Za-z]+\s*=\s*"([^"]+)"/g)].map((m) => m[1]);
    const marketSrc = fs.readFileSync(
      path.join(root, 'tools/plugin-market/frontend/src/main.js'), 'utf8');
    const mapAt = marketSrc.indexOf('const PERMISSION_LABELS');
    const mapBody = marketSrc.slice(mapAt, marketSrc.indexOf('};', mapAt));
    const labelled = [...mapBody.matchAll(/^\s*'?([a-z][a-z.]*)'?:\s*'/gm)].map((m) => m[1]);
    if (allowed.length === 0 || labelled.length === 0) return false;
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

for (const line of notes) console.log(line);
console.log('');
if (failures.length) {
  for (const line of failures) console.log(line);
  console.log(`\n${failures.length} FRONTEND CHECK(S) FAILED`);
  process.exit(1);
}
console.log(`ALL FRONTEND CHECKS PASSED (${notes.length})`);
