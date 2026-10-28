/**
 * Frontend behaviour harness.
 *
 * Loads the REAL html, CSS and JS modules in a real browser engine against a
 * fake Wails bridge, then drives the app the way a user would and asserts on
 * what it asked the backend to do. This is the substitute for a live GUI run:
 * WebView2 cannot create a controller in this environment (the pre-change binary
 * fails identically), so the pet's click handling, the streaming renderer
 * and the settings wiring would otherwise be unverified.
 *
 * Run:  node .dev/run-frontend-harness.cjs
 */

const fs = require('fs');
const path = require('path');
const http = require('http');
const { spawnSync } = require('child_process');

const root = path.resolve(__dirname, '..');
const frontend = path.join(root, 'frontend');
const built = path.join(__dirname, 'harness-build.html');

// --- build the harness page from the real markup and the BUILT bundle -------
//
// The page must load the compiled bundle rather than /src/main.js. The source
// modules import './style.css' and an .svg, which only Vite can resolve; a
// browser loading them raw fails on the first import and the whole module graph
// never executes (which is exactly what happened on the first attempt: every
// check failed because nothing had run). Loading dist also means this harness
// exercises the artifact that actually ships.
const html = fs.readFileSync(path.join(frontend, 'index.html'), 'utf8');
const body = html.slice(html.indexOf('<body>') + '<body>'.length, html.indexOf('</body>'));
const markup = body.replace(/<script[\s\S]*?<\/script>/g, '');

const assetsDir = path.join(frontend, 'dist', 'assets');
const bundleJs = fs.readdirSync(assetsDir).find((f) => /^index-.*\.js$/.test(f));
const bundleCss = fs.readdirSync(assetsDir).find((f) => /^index-.*\.css$/.test(f));
if (!bundleJs || !bundleCss) {
  console.error('the frontend bundle is missing; run the frontend build first');
  process.exit(2);
}
console.log(`harness using built bundle ${bundleJs} + ${bundleCss}`);

// The page is assembled once, in order, rather than by chained replacements: the
// checks script has to be injected AFTER the markup is substituted (or it is
// swallowed by it) and the bundle's own script tag is already inside the markup
// that gets dropped.
const page = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <title>PetPC frontend harness</title>
  <link rel="stylesheet" href="/dist/assets/${bundleCss}" />
</head>
<body>
${markup}
<script>
  // A fake Wails bridge, installed BEFORE the app bundle loads. The bindings call
  // window.go.main.App.<Name>(...) and the runtime calls
  // window.runtime.EventsOn/EventsEmit, so this is the entire surface the
  // frontend talks to.
  window.__calls = [];
  function bind(name) {
    return function (...args) {
      window.__calls.push({ name, args });
      // The real Wails bindings return promises, so the fake ones must too: the
      // app calls .then() on every one of them.
      //
      // The scenario table may not be installed YET, and that is a fact about this
      // page rather than defensiveness: it is installed by the checks module, which the
      // browser evaluates AFTER the app bundle, and the app's own boot can call a
      // binding in between. Reading through an undefined window.__handlers threw
      // synchronously ("Cannot read properties of undefined (reading 'PageLog')"), which
      // escaped as an unhandled rejection and killed the boot - exactly what happened
      // when fail() in main.js gained a binding call. A missing handler means "this
      // scenario does not answer this call", which is what the null below already means.
      const handler = window.__handlers ? window.__handlers[name] : null;
      try {
        return Promise.resolve(handler ? handler(...args) : null);
      } catch (err) {
        return Promise.reject(err);
      }
    };
  }
  // The fake bridge is the whole surface the app talks to, so this list is the
  // REAL binding set: every export of frontend/wailsjs/go/main/App.js, and
  // nothing else. It is kept in step by hand because the harness runs in a page
  // with no module loader, but the two halves of the check matter in opposite
  // directions - a MISSING method makes wire() throw partway through and the
  // failure surfaces much later as something unrelated, while a SURPLUS one
  // silently accepts a call the real app could not make, which is how a deleted
  // binding stays "covered" by a test that can no longer fail.
  //
  // frontend/src/test-harness.js carries the same list for the jsdom tests
  // (T113 compares the two as sets, so this is the single place it is written
  // down twice).
  const methods = [
    'ChoosePetImage', 'ClosePluginSurface',
    'DebugLog', 'DismissBubble', 'DragTo',
    'EndDrag', 'GetLayout', 'GetState', 'HidePet',
    'HidePetForUser', 'OpenChatPluginSurface', 'OpenPetMenu', 'OpenPluginMarket',
    'OpenPluginSurface', 'OpenTodoFromBubble', 'PageLog', 'PetBadge', 'SetPluginSurfaceBlocks',
    'PetImageURL', 'PluginMarketCandidates',
    'PluginMarketPath', 'PluginRequest', 'PluginsInstalled',
    'Quit', 'Ready', 'RemindNow', 'ResetPetImage',
    'ResetToDefaultPosition',
    'SaveHotkeySettings', 'SaveReminderSettings', 'SetAutostart', 'SetBubbleHover',
    'SetReminderEnabled', 'SetScale',
    'SetSnapToEdge', 'ShowPet', 'ShowPetForUser',
    'SnapNow', 'StartDrag',

    'WindowReady',
  ];
  const app = {};
  for (const m of methods) app[m] = bind(m);
  window.go = { main: { App: app } };
  // The list itself is published so a scenario can assert against the EXPORT SET
  // rather than against the fake: "the host no longer has a conversation binding"
  // is a fact about what Go exposes, and the fake is the one place in the page
  // that knows it.
  window.__methods = methods.slice();

  const listeners = {};
  const subscribe = (name, cb) => {
    (listeners[name] ||= []).push(cb);
    return () => {
      listeners[name] = (listeners[name] || []).filter((f) => f !== cb);
    };
  };
  window.runtime = {
    EventsOn: subscribe,
    // The generated runtime module calls EventsOnMultiple for a bounded
    // subscription; without it the frontend's wire() throws partway through and
    // only the listeners registered before that point survive, which is a
    // confusing partial failure.
    EventsOnMultiple(name, cb) { return subscribe(name, cb); },
    EventsOnce(name, cb) {
      const off = subscribe(name, (...a) => { off(); cb(...a); });
      return off;
    },
    EventsOff(name) { delete listeners[name]; },
    EventsOffAll() { for (const k of Object.keys(listeners)) delete listeners[k]; },
    EventsEmit(name, ...data) { for (const cb of listeners[name] || []) cb(...data); },
    WindowShow() {},
    WindowHide() {},
    WindowSetTitle() {},
    LogPrint() {},
    LogTrace() {},
    LogDebug() {},
    LogInfo() {},
    LogWarning() {},
    LogError() {},
    LogFatal() {},
  };
  window.__emit = (name, ...data) => window.runtime.EventsEmit(name, ...data);

  // Diagnostics: anything the page throws is recorded, so a harness failure can
  // say WHY rather than only that an assertion did not hold.
  window.__allMsgCount = 0;
  window.addEventListener('message', (ev) => {
    window.__allMsgCount++;
    window.__allMsgLog = window.__allMsgLog || [];
    window.__allMsgLog.push(JSON.stringify(ev.data));
  });
  window.__errors = [];
  window.addEventListener('error', (e) => window.__errors.push(String(e.message || e.error)));
  window.addEventListener('unhandledrejection', (e) =>
    window.__errors.push('unhandled: ' + String((e.reason && e.reason.stack) || e.reason)));

  // Headless Chrome has no compositor, so requestAnimationFrame never fires and
  // anything waiting on it hangs forever. The app uses it in nextPaints() to wait
  // for the first real frames before revealing the window; a timer is a faithful
  // stand-in for the harness's purposes (which are behavioural, not visual).
  let rafId = 0;
  window.requestAnimationFrame = (cb) => setTimeout(() => cb(Date.now()), 8);
  window.cancelAnimationFrame = (id) => clearTimeout(id);
</script>

<script type="module" src="/dist/assets/${bundleJs}"></script>
<script type="module">
  import './harness-checks.js';
  const report = (payload) =>
    fetch('/result', { method: 'POST', body: JSON.stringify(payload) }).catch(() => {});
  // An error before, during or after the scenarios must still be reported, or the
  // harness fails as "never reported back" with no explanation.
  window.addEventListener('error', (e) =>
    report({ ok: false, error: 'window error: ' + (e.message || e.error) + '\\n' + ((e.error && e.error.stack) || '') }));
  window.addEventListener('unhandledrejection', (e) =>
    report({ ok: false, error: 'unhandled rejection: ' + ((e.reason && e.reason.stack) || e.reason) }));
  try {
    await new Promise((r) => setTimeout(r, 400));
    const log = await window.__run();
    await report({ ok: true, log });
  } catch (err) {
    await report({ ok: false, error: err && err.stack ? err.stack : String(err) });
  }
</script>
</body>
</html>
`;
fs.writeFileSync(built, page, 'utf8');

// --- the checks themselves run inside the page ------------------------------
const checksJs = `
  const log = [];
  const ok = (name, cond, detail) => log.push({ name, ok: !!cond, detail: detail || '' });
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  const calls = (name) => window.__calls.filter((c) => c.name === name);
  const last = (name) => calls(name).at(-1);
  // Never let a missing element throw: a crash loses every check after it, and
  // the point of the harness is to report all of them.
  const $ = (sel) => document.querySelector(sel);
  const text = (sel) => $(sel)?.textContent ?? null;
  const count = (sel) => document.querySelectorAll(sel).length;

  async function waitFor(what, cond, timeout = 3000) {
    const end = Date.now() + timeout;
    while (Date.now() < end) {
      if (cond()) return true;
      await sleep(10);
    }
    ok('timed out: ' + what, false);
    return false;
  }

  // --- fake backend ---------------------------------------------------------
  //
  // The floating surface a plugin declares, as a layout carries it. The rects are
  // deliberately arbitrary: what the checks assert is that a frame lands on
  // EXACTLY what the host said, not that any particular number is right - a
  // placement that recomputed a position would pass a "roughly here" check and
  // still disagree with the native crop.
  //
  // Five rectangles, three of them ellipses. The rects are deliberately arbitrary
  // - what the checks assert is that a frame lands on EXACTLY what the host said,
  // not that any particular number is right - but the MIX OF SHAPES is deliberate:
  // the two non-ellipse blocks are what makes the ellipse assertion meaningful,
  // since a page that rounded EVERYTHING would satisfy "the ellipse is round".
  //
  // This is a SYNTHETIC surface, not a copy of the shipping plugin's. The
  // conversation plugin used to declare this shape (a card, three thought puffs and
  // an input bar) and now declares two rounded rectangles - a reply card above the
  // pet and an input bar below it - so mirroring it here would delete the only
  // coverage of the host's "ellipse === true -> border-radius: 50%" branch. That
  // branch is generic plugin-host behaviour and still shipping code, so the fixture
  // keeps an ellipse in it on purpose. The plugin's real geometry is pinned where
  // it belongs: by its own manifest, its vitest suite, and .dev/verify-live.ps1.
  const SURFACE_RECTS = [
    { index: 0, x: 160, y: 170, w: 300, h: 112, r: 12, ellipse: false },
    { index: 1, x: 118, y: 125, w: 34, h: 20, r: 0, ellipse: true },
    { index: 2, x: 94, y: 150, w: 24, h: 14, r: 0, ellipse: true },
    { index: 3, x: 75, y: 171, w: 15, h: 9, r: 0, ellipse: true },
    { index: 4, x: 160, y: 368, w: 300, h: 56, r: 12, ellipse: false },
  ];
  const surfaceView = (pluginId = 'com.example.beta', rects = SURFACE_RECTS) => ({
    pluginId, rects: rects.map((r) => ({ ...r })),
  });

  const layout = (open, bubble = null, pluginSurface = null) => ({
    panelOpen: open, windowW: 540, windowH: 520,
    petX: open ? 0 : 0, petY: 160, petW: 150, petH: 200,
    panelX: open ? 160 : 0, panelY: 0, panelW: 380, panelH: 520,
    panelOnRight: true, scale: 1, bubble, pluginSurface,
  });

  const baseState = (over = {}) => ({
    config: {
      petScale: 1, snapToEdge: true, edgeMargin: 8,
      // No pomodoro* keys: the timer, its phase lengths and its "sound" switch are the
      // pomodoro plugin's own config now. A fake that still sent them would let a
      // renderer reading config.focusMinutes pass here while the real GetState has
      // stopped producing it - the same trap the llm keys below are absent for.
      reminderEnabled: false, reminderOnStartup: false, reminderIntervalMin: 30,
      reminderJitterMaxMin: 3, reminderFrom: '09:00', reminderTo: '22:00',
      reminderMaxItems: 3, reminderAutoHideSec: 12, remindWhenEmpty: false,
      // No llm* keys and no llm section: the AI configuration belongs to the
      // chat plugin and is edited in the market program. A fake that still sent
      // it would let a renderer that reads it pass here while the real GetState
      // has stopped producing it.
      ...(over.config || {}),
    },
    todos: [], stats: { total: 0, open: 0, overdue: 0, dueSoon: 0 },
    petImageUrl: '', hasCustomImage: false, dataDir: 'C:\\\\data', autostart: false,
    now: new Date().toISOString(),
    // No pomodoro, records, recordTotals or reminder block: State carries none
    // of them any more (app.go's State comment says why), so a fixture that sent them
    // would be testing a snapshot the host cannot produce.
    // There is no "voice" block in this fixture either: speech is a plugin now, and the
    // pet reports nothing about it. The panel therefore has no voice section to render,
    // which is what the checks below assert.
    // Global hide/show shortcuts. The fake mirrors desktop.HotkeyView, and the
    // default is the honest "off" shape: disabled, with the documented labels and
    // an explanation, exactly as Go reports before the manager has started.
    hotkeys: {
      enabled: false, show: 'Ctrl+Alt+S', hide: 'Ctrl+Alt+H',
      active: false, note: '全局快捷键已关闭',
      ...(over.hotkeys || {}),
    },
    petVisible: over.petVisible !== false,
  });

  // The conversation is a panel tab, so the two surfaces are told apart by which
  // tab is showing rather than by a bubble.
  let panelOpen = false;
  // The plugin's floating surface: null, or the PluginSurfaceView the layout
  // carries. It is the THIRD occupant of the slot beside the pet (with the panel
  // and the speech bubble), so opening any one of the three closes the others -
  // the fake backend has to model that or the mutual-exclusion checks would pass
  // vacuously.
  let currentSurface = null;
  // The speech bubble occupying the same slot: null, or a BubbleView. Only the
  // summary prompt is modelled, because it is the one that carries a question
  // with a real consequence (the parked break) rather than a notification.
  let currentBubble = null;
  let activeTab = 'todos';
  let savedConfig = null;
  // The global-shortcut switch and the view the "OS" reported for it. Stored
  // separately because the switch position and whether the keys actually took are
  // different facts, and the panel renders both.
  let savedHotkey = false;
  let savedHotkeyActive = false;
  let savedHotkeyProblems = [];

  // --- plugin state ---------------------------------------------------------
  //
  // The host no longer has a plugin tab, a catalogue view or an install flow:
  // managing plugins is the market program's job, and the pet only ever ASKS who
  // is installed. So the fake keeps exactly one list - the installed rows, in the
  // shape PluginsInstalled() returns - and nothing else.
  //
  // The rows are the REAL PluginView shape, field for field: panelSlot is the
  // string the manifest declared (and the slot is resolved from it) and surface
  // is the boolean "does this plugin declare any floating rectangle". Inventing a
  // nested surface:{kind,slot} object here would have made the harness pass
  // against a model the host does not send - which is exactly the bug this
  // rewrite found in the product code.
  const pluginDir = (id) => 'C:' + String.fromCharCode(92) + 'data' +
    String.fromCharCode(92) + 'plugins' + String.fromCharCode(92) + id;
  // One row claims the 对话 slot and is enabled; one is enabled but claims
  // nothing; one is disabled although it does claim the slot. The last two are
  // what make the resolution rule testable rather than "is there any plugin".
  //
  // The background field is carried on every row (false unless stated) because the
  // page reads it to decide which plugin gets the headless frame, and a fixture
  // that omitted the field would exercise the undefined path rather than the real
  // one. It is false here so the four background cases below can each turn it on
  // for one row.
  let installedPlugins = [
    {
      id: 'com.example.chat', name: '示例对话插件', version: '1.1.0', author: 'shixiansi',
      description: '在面板里与大模型对话', enabled: true,
      permissions: ['llm.chat', 'pet.surface'], granted: ['llm.chat', 'pet.surface'],
      pending: [], netHosts: [], panel: true, panelSlot: 'chat', surface: true,
      background: false,
      entry: 'panel.html', dir: pluginDir('com.example.chat'), error: '',
    },
    {
      id: 'com.example.other', name: '别的插件', version: '0.9.0', author: 'someone',
      description: '启用了但没有认领对话槽位', enabled: true,
      permissions: ['pet.bubble'], granted: ['pet.bubble'], pending: [], netHosts: [],
      panel: false, panelSlot: '', surface: false, background: false,
      entry: '', dir: pluginDir('com.example.other'), error: '',
    },
    {
      id: 'com.example.off', name: '被关掉的对话插件', version: '0.4.0', author: 'someone',
      description: '认领了槽位但被关掉了', enabled: false,
      permissions: ['llm.chat', 'pet.surface'], granted: ['llm.chat', 'pet.surface'],
      pending: [], netHosts: [], panel: true, panelSlot: 'chat', surface: true,
      background: false,
      entry: 'panel.html', dir: pluginDir('com.example.off'), error: '',
    },
    {
      // Declares the headless frame AND holds the permission that gates it: the
      // one row that must produce a background frame.
      id: 'com.example.bg', name: '后台插件', version: '2.0.0', author: 'shixiansi',
      description: '声明了常驻帧的插件', enabled: true,
      permissions: ['events', 'audio'], granted: ['events', 'audio'], pending: [], netHosts: [],
      panel: true, panelSlot: '', surface: false, background: true,
      entry: 'panel.html', dir: pluginDir('com.example.bg'), error: '',
    },
  ];
  window.__installed = installedPlugins;
  window.__pluginRequests = [];

  // __pluginsInstalled is the projection PluginsInstalled() returns: a fresh
  // copy of the rows, so a check that mutates one cannot corrupt the next.
  window.__pluginsInstalled = () => window.__installed.map((p) => ({
    ...p,
    granted: p.granted.slice(),
    pending: p.pending.slice(),
  }));

  // The persisted transcript and the chat request ids are GONE from this fake,
  // along with the conversation the pet used to host. The pet no longer sends a
  // question, streams a reply or keeps a history: the chat plugin does all of
  // that inside its own sandboxed iframe, over the llm.chat capability. A fake
  // that still modelled a transcript would let a scenario assert against a
  // conversation the product no longer has.

  window.__handlers = {
    GetLayout: () => layout(panelOpen, currentBubble, currentSurface),
    // The plugin's line on the pet: empty until a plugin sets one.
    PetBadge: () => '',
    GetState: () => {
      const cfgOver = savedConfig || {};
      const st = baseState(Object.keys(cfgOver).length ? { config: cfgOver } : {});
      // There is no "st.voice" any more: the pet has no speech state to report, so
      // the fake must not invent one. A fixture that still carried it would let a
      // scenario assert against a settings section the product no longer has.
      // The shortcut view, built from the two separate facts. Enabled comes from
      // the persisted switch; Active and Problems come from what the "OS" said.
      st.hotkeys = {
        enabled: savedHotkey,
        show: 'Ctrl+Alt+S', hide: 'Ctrl+Alt+H',
        active: savedHotkeyActive,
        problems: savedHotkeyProblems,
        note: savedHotkey ? '' : '全局快捷键已关闭',
      };
      return st;
    },
    Ready: () => null,
    PetImageURL: () => '',
    // The three occupants of the slot beside the pet are mutually exclusive, and
    // the fake has to model that or the exclusion checks pass vacuously. The panel
    // is the one that yields to both of the others.

    // Nothing refuses a dismissal any more: the two bubbles the host used to protect
    // were the summary and plan prompts, and both were the host timer's questions. A
    // reminder and the menu card are dismissable, so the fake dismisses whatever is up.
    DismissBubble: () => {
      currentBubble = null;
      return layout(panelOpen);
    },
    // The pet's own menu: a BUBBLE the page draws, not an OS popup. The fake has to
    // model it the way it models the reminder - a kind in the slot, with the box Go
    // crops the window to (bubbleHeightMenu = 226 in app_bubble.go) - because a
    // fake that answered null would make "the grid is on screen" unobservable.
    OpenPetMenu: () => {
      if (currentBubble) return layout(panelOpen, currentBubble);
      panelOpen = false;
      currentSurface = null;
      currentBubble = { kind: 'menu', title: '', x: 160, y: 147, w: 300, h: 226, onRight: true };
      return layout(false, currentBubble);
    },

    // --- plugins -----------------------------------------------------------
    //
    // Only two bindings are left, because the host only does two things with
    // plugins now: it reports who is installed (which the 对话 slot is resolved
    // from) and it opens a plugin's floating surface. Everything else - the
    // catalogue, the install, the grant, the switch, the market address - is the
    // market program's, in its own process, and the pet has no binding for any of
    // it.
    PluginsInstalled: () => window.__pluginsInstalled(),
    // Opening the surface takes the slot from the panel, exactly as the real
    // OpenPluginSurface does.
    OpenPluginSurface: () => {
      panelOpen = false;
      currentBubble = null;
      currentSurface = surfaceView();
      return layout(false, null, currentSurface);
    },
    // The conversation button's target: the chat slot's own plugin, or the explanation
    // bubble when there is none. The fake resolves the slot from the same rows and
    // by the same rule the frontend does, so a change to the rule cannot pass here
    // and fail on the host.
    OpenChatPluginSurface: () => {
      const row = window.__installed.find(
        (p) => p.enabled && p.panelSlot === 'chat' &&
          Array.isArray(p.granted) && p.granted.includes('pet.surface'),
      );
      if (!row) {
        panelOpen = false;
        currentSurface = null;
        currentBubble = {
          kind: 'reminder',
          title: '对话插件还没装好：在托盘菜单里点「打开控制中心」装上并授权它。',
          x: 168, y: 180, w: 300, h: 140, onRight: true,
        };
        return layout(false, currentBubble);
      }
      return window.__handlers.OpenPluginSurface();
    },
    ClosePluginSurface: () => {
      currentSurface = null;
      return layout(panelOpen, currentBubble);
    },
    // The settings entry point. It launches a SEPARATE PROGRAM, so there is
    // nothing to wait for and no state to return: the call itself is the whole
    // observable effect, which is exactly what the check asserts.
    OpenPluginMarket: () => null,
    // A plugin request is routed exactly as the host routes it: the sandboxed
    // iframe asks, the host decides. Here the decision is the fake's, and it
    // echoes the method back so the scenario can assert the round trip.
    PluginRequest: (id, method, payload) => {
      window.__pluginRequests.push({ id, method, payload });
      if (method === 'llm.chat') return JSON.stringify({ requestId: 'r1', text: '你好呀' });
      return JSON.stringify({ ok: true, method });
    },

    // --- global hide/show shortcuts --------------------------------------
    //
    // The real one persists the switch, re-registers the keys and returns the
    // whole State. The fake models the TWO SEPARATE FACTS the real view carries:
    // Enabled is the persisted switch, Active is whether the "OS" took the keys.
    // Deriving one from the other is exactly the bug that made the switch flash
    // instead of staying on, so the fake keeps them apart and the scenario can
    // force either independently.
    SaveHotkeySettings: (input) => {
      window.__savedHotkey = input;
      savedHotkey = input.enabled;
      const outcome = window.__hotkeyOutcome || {};
      savedHotkeyActive = input.enabled ? outcome.active !== false : false;
      savedHotkeyProblems = input.enabled ? outcome.problems || [] : [];
      return window.__handlers.GetState();
    },

    DebugLog: () => null,
    SetBubbleHover: () => null,
  };

  // --- helpers to drive the UI ----------------------------------------------
  const pet = document.getElementById('pet');
  const petRect = { x: 100, y: 300 };

  // Models what Go does when the conversation button resolves to the chat plugin's
  // own floating surface: the surface takes the slot from the panel and the bubble,
  // and the layout carries the rectangles. The checks cannot set the closure
  // variable themselves, so the fake exposes the transition.
  window.__openSurface = () => {
    panelOpen = false;
    currentBubble = null;
    currentSurface = surfaceView();
    return currentSurface;
  };

  // Takes the floating surface back down: the slot goes back to whatever else was
  // there, and the layout says so. Without this the fake could still hold a surface
  // the page has already unmounted, and the next GetLayout would contradict the DOM.
  window.__closeSurface = () => {
    currentSurface = null;
    const next = layout(panelOpen, currentBubble, null);
    window.__emit('pet:layout', next);
    return next;
  };

  // Raises a reminder bubble the way Go does: the card takes the slot (the panel and
  // the surface go away), pet:bubble announces it, and the layout carries the card and
  // its geometry.
  //
  // The geometry is the real one for this kind - 300x140 at the panel's slot, centred
  // on the pet's band - because the frontend positions the card verbatim from it and a
  // made-up rectangle would let a placement bug pass.
  //
  // A REMINDER, not the summary prompt this helper used to raise: the prompts were the
  // host timer's questions, and a reminder is the only bubble the host still raises on
  // its own (RemindNow). It is what a surface has to be able to open over.
  window.__showReminder = () => {
    panelOpen = false;
    currentSurface = null;
    currentBubble = {
      kind: 'reminder',
      title: '该歇一会儿了',
      items: [{ id: 't1', title: '写周报', due: '今天 18:00' }],
      // A reminder's whole content IS the list, so it asks for the button that opens it.
      showTodoAction: true,
      more: 0,
      x: 168, y: 180, w: 300, h: 140, onRight: true,
    };
    window.__emit('pet:bubble', { kind: 'reminder', shown: true });
    // The layout is what actually paints the card: the frontend renders the bubble
    // FROM the layout, never from the event alone (that is the bug that once made
    // the prompt impossible to dismiss).
    window.__emit('pet:layout', layout(false, currentBubble));
    return layout(false, currentBubble);
  };

  // Takes the card down and repaints, so the scenarios that follow start from a
  // known slot.
  window.__clearBubble = () => {
    currentBubble = null;
    window.__emit('pet:bubble', { kind: 'reminder', shown: false });
    window.__emit('pet:layout', layout(panelOpen, null, currentSurface));
    return layout(panelOpen, null, currentSurface);
  };

  function pressPet(times) {
    // A press on the pet. The pointer handlers are what the app actually listens
    // to, so the harness dispatches those rather than synthetic click events.
    const fire = (type, extra = {}) => pet.dispatchEvent(new PointerEvent(type, {
      bubbles: true, cancelable: true, button: 0, buttons: 1,
      pointerId: 1, pointerType: 'mouse',
      screenX: petRect.x, screenY: petRect.y, ...extra,
    }));
    fire('pointerdown');
    fire('pointerup');
    if (times === 2) { fire('pointerdown'); fire('pointerup'); }
  }

  // The conversation entry: the button painted over the pet on hover. Hover
  // itself is a stylesheet rule, which a dispatched event cannot turn on - the
  // gate pins the rule statically - so the scenario clicks the button the way a
  // user would once it is visible.
  function clickChatButton() {
    document.getElementById('pet-chat')
      .dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
  }

  async function run() {
    // The plugin probes' reports, read from the harness SERVER rather than from
    // a postMessage listener.
    //
    // A sandboxed frame's window cannot be reached from the host page at all (its
    // origin is opaque by design), so the only way to learn what a plugin
    // received is for the plugin to say. It says it in a POST, because a report
    // over postMessage could arrive before this suite's listener existed - and a
    // lost report is indistinguishable from a handshake that never happened.
    const probeReceived = async () => {
      const res = await fetch('/__probe-log', { cache: 'no-store' });
      const entries = await res.json();
      return entries.filter((e) => e && e.recv).map((e) => e.recv);
    };
    // Empties the accumulated reports. A scenario that counts handshakes has to
    // start from an empty log: the reports accumulate for the whole run, and
    // every layout the suite emits re-handshakes the frames that are already up.
    const probeReset = async () => {
      await fetch('/__probe-reset', { method: 'POST' });
    };
    // Polls until pred(entries) holds or the deadline passes, returning the last
    // snapshot either way.
    //
    // A fixed sleep is not enough here: the probe is a SEPARATE document that
    // fetches and then posts its report, so when the report lands is bound by
    // another page's network and script scheduling, not by this one's. Asserting
    // after a fixed wait made the result depend on how loaded the machine was,
    // which is the definition of a flaky gate. A prediction that stays false
    // ("the forged ready produced nothing") gets a short grace period instead of
    // no wait at all, so it is still measuring "nothing arrived" rather than
    // "nothing had arrived yet".
    const probeReceivedWhen = async (pred, timeoutMs = 6000) => {
      const deadline = Date.now() + timeoutMs;
      let got = await probeReceived();
      while (!pred(got) && Date.now() < deadline) {
        await sleep(100);
        got = await probeReceived();
      }
      return got;
    };

    const booted = await waitFor('boot', () => window.__calls.some((c) => c.name === 'Ready'));
    ok('the app booted and asked to be revealed', booted, 'Ready was never called');
    ok('boot raised no errors', window.__errors.length === 0, window.__errors.join(' | '));

    // Grab the surface mount once: it is referenced throughout.
    //
    // The panel and its 对话 tab used to be grabbed here too. Both are gone from the markup
    // (openspec/changes/todo-pomodoro-as-plugins, stage 3), so document.getElementById
    // returns null and the next .classList read is a TypeError - which is exactly how the
    // harness announced it on the first run after the panel was deleted. The scenarios below
    // were rewritten around what replaced them: the pet's gesture, the floating surface, and
    // the headless frames.
    const surfaceMount = document.getElementById('plugin-surface');

    // Reset the click bookkeeping between scenarios.
    //
    // A click on the pet acts at once now, but the binding it calls is still a
    // promise: a scenario that starts too soon can see the PREVIOUS scenario's
    // layout land in the middle of it and overwrite the state under test. That
    // produced a very convincing "the card does not show" failure that was purely
    // a harness race.
    const settle = () => sleep(500);

    // --- 1. a click on the pet does nothing, and asks for nothing -----------
    //
    // It used to toggle the panel, then (briefly) to be deferred for the system's
    // double-click interval. Both destinations are plugin surfaces now, reached from the pet's
    // right-click menu and from the hover button, and there is no panel left to toggle.
    //
    // The assertion that MATTERS is the negative one: a bare click must not reach for either
    // surface. A click that opened one of them silently would be a gesture nobody documented,
    // and it would be indistinguishable from "the pet is broken" the moment the user tried to
    // use the surface it took over.
    window.__calls.length = 0;
    await settle();
    pressPet(1);
    await sleep(150);
    ok('a single click on the pet opens no panel and no surface',
      calls('OpenChatPluginSurface').length === 0 && calls('OpenPluginSurface').length === 0,
      'OpenChatPluginSurface=' + calls('OpenChatPluginSurface').length +
        ' OpenPluginSurface=' + calls('OpenPluginSurface').length);

    // --- 2. the conversation button asks the plugin for its surface ----------
    //
    // The gesture is ONE binding call, not a host-side overlay: the host does not
    // know how to draw a conversation, so it asks Go to open whatever plugin owns
    // the slot. Go answers with a Layout carrying that plugin's rectangles (or a
    // bubble saying no plugin is installed), which is why nothing about the
    // surface is asserted here - only that the right request was made and that the
    // panel was not toggled along the way.
    panelOpen = false;
    window.__calls.length = 0;
    await settle();
    // Inert until the pet is hovered - asserted in a real engine, because this is
    // the one rule whose failure would silently change what a click on the pet's
    // upper-left corner does. Hover itself cannot be simulated from script, so the
    // :hover rule is pinned by .dev/check-frontend.cjs instead.
    const chatButtonEl = document.getElementById('pet-chat');
    ok('the conversation button is inert while the pet is not hovered',
      getComputedStyle(chatButtonEl).visibility === 'hidden' &&
        getComputedStyle(chatButtonEl).pointerEvents === 'none',
      'visibility=' + getComputedStyle(chatButtonEl).visibility +
        ' pointer-events=' + getComputedStyle(chatButtonEl).pointerEvents);
    // Inside the pet's box, measured rather than assumed: the window region is cut
    // to that box while the panel and the bubble are away, so a button even one
    // pixel outside it would be clipped by the OS - or worse, a wider region would
    // be a strip of window the page never paints.
    const petBox = pet.getBoundingClientRect();
    const chatBox = chatButtonEl.getBoundingClientRect();
    ok('the conversation button sits inside the pet box the region is cut to',
      chatBox.left >= petBox.left && chatBox.top >= petBox.top &&
        chatBox.right <= petBox.right && chatBox.bottom <= petBox.bottom,
      'pet ' + [petBox.left, petBox.top, petBox.right, petBox.bottom].join(',') +
        ' button ' + [chatBox.left, chatBox.top, chatBox.right, chatBox.bottom].join(','));
    clickChatButton();
    ok('the conversation button asks the plugin for its surface',
      calls('OpenChatPluginSurface').length === 1,
      'OpenChatPluginSurface calls: ' + calls('OpenChatPluginSurface').length);
    await sleep(300);

    // --- 2b. every declared rectangle gets a frame at its exact pixels -------
    //
    // This is the property the native window region depends on: Go crops the
    // window to exactly these rectangles, so the page must place a frame at each
    // one and nowhere else. A frame even a pixel off would be clipped by the
    // region, and a frame drawn where no region reaches would not be visible at
    // all.
    //
    // The surface is driven from the layout rather than by pressing the pet, so
    // the rectangles under test are the ones written above and the assertion can
    // be per-rectangle.
    window.__emit('pet:layout', layout(false, null, surfaceView()));
    await sleep(200);
    const surfaceFrames = [...surfaceMount.querySelectorAll('iframe')];
    ok('a five-rectangle surface gets one frame per rectangle (' + surfaceFrames.length + ')',
      surfaceFrames.length === 5,
      'got ' + surfaceFrames.length + ' frames');
    ok('every surface frame sits exactly on its own rectangle',
      surfaceFrames.length === 5 && surfaceFrames.every((f, i) => {
        const r = SURFACE_RECTS[i];
        return f.style.left === r.x + 'px' && f.style.top === r.y + 'px' &&
          f.style.width === r.w + 'px' && f.style.height === r.h + 'px';
      }),
      surfaceFrames.map((f) => f.style.left + ',' + f.style.top + ' ' +
        f.style.width + 'x' + f.style.height).join(' | '));
    // The sandbox is the security boundary and it is asserted as an EXACT string:
    // allow-same-origin is what would hand a plugin the host page's DOM, and it
    // must never appear on any frame, not merely on the first one.
    ok('every surface frame is sandboxed to allow-scripts alone',
      surfaceFrames.length === 5 && surfaceFrames.every(
        (f) => f.getAttribute('sandbox') === 'allow-scripts' && f.getAttribute('allow') === null),
      'sandboxes: ' + surfaceFrames.map((f) => f.getAttribute('sandbox')).join(','));
    // An ellipse rectangle is cropped to an ELLIPSE by the native region, so the
    // drawn shape has to be one too: a rounded rectangle would leave the four
    // corners inside the region but unpainted, which shows the composited glass
    // tint instead of the desktop. The non-ellipse blocks keep their declared
    // radius, which is what makes this assertion about the FLAG rather than about
    // "is the radius big".
    ok('an elliptical rectangle is drawn as a true ellipse, a rounded one as a rounded box',
      surfaceFrames.length === 5 && surfaceFrames.every((f, i) => {
        const want = SURFACE_RECTS[i].ellipse ? '50%' : SURFACE_RECTS[i].r + 'px';
        return f.style.borderTopLeftRadius === want &&
          f.style.borderTopRightRadius === want &&
          f.style.borderBottomLeftRadius === want &&
          f.style.borderBottomRightRadius === want;
      }),
      surfaceFrames.map((f, i) => SURFACE_RECTS[i].ellipse + ':' + f.style.borderTopLeftRadius).join(' | '));

    // --- 2c. nothing refuses the surface any more ----------------------------
    //
    // The surface and the speech bubble share one slot and OpenPluginSurface TAKES it,
    // so a bubble that was waiting for an answer used to be destroyed by the
    // conversation gesture - which is why that gesture was refused and the caret went
    // into the question instead ("go answer it").
    //
    // There is no host question left: the summary and plan prompts were the host
    // timer's, and they are the pomodoro plugin's own surface now
    // (openspec/changes/todo-pomodoro-as-plugins). So the rule INVERTED, and this
    // scenario pins the inversion: the card carries no box to answer into, and the
    // conversation button opens the plugin's surface OVER a bubble instead of
    // protecting it.
    await settle();
    window.__calls.length = 0;
    const bubbleEl = document.getElementById('bubble');
    ok('the bubble card has no answer box any more',
      document.getElementById('bubble-input') === null,
      'a textarea for a question the host no longer asks is still in the markup');

    window.__showReminder();
    await waitFor('the reminder bubble', () => !bubbleEl.classList.contains('hidden'));
    ok('a reminder bubble is up',
      !bubbleEl.classList.contains('hidden'),
      'the card did not paint: hidden=' + bubbleEl.classList.contains('hidden'));

    // 「看看待办」 is only drawn when the sentence is ABOUT the list. A reminder is, so its
    // showTodoAction is set and the button must be there.
    const primary = document.getElementById('bubble-primary');
    ok('a reminder offers the list it is about',
      !!primary && !primary.classList.contains('hidden') &&
        primary.textContent === '看看待办',
      'primary hidden=' + (primary && primary.classList.contains('hidden')) +
        ' text=' + (primary && primary.textContent));

    // A bubble.say line (a chat reply) is NOT about the list: the same card without the flag must
    // draw no such button. Reported as 「怎么一直弹这个窗口」, with a button that led nowhere useful.
    window.__emit('pet:layout', layout(false, {
      kind: 'reminder', title: '在呢在呢！', items: [], more: 0,
      x: 168, y: 180, w: 300, h: 140, onRight: true,
    }));
    await sleep(120);
    ok('a plain spoken line offers no todo button',
      !!primary && primary.classList.contains('hidden'),
      'the todo button appeared on a bubble that carries no list');

    document.activeElement?.blur?.();
    window.__calls.length = 0;
    clickChatButton();
    await waitFor('OpenChatPluginSurface over the bubble',
      () => calls('OpenChatPluginSurface').length === 1);
    ok('the conversation button asks for the surface even while a bubble is up',
      calls('OpenChatPluginSurface').length === 1,
      'OpenChatPluginSurface calls: ' + calls('OpenChatPluginSurface').length);
    ok('the surface took the slot from the bubble',
      !surfaceMount.classList.contains('hidden') && bubbleEl.classList.contains('hidden'),
      'surface hidden=' + surfaceMount.classList.contains('hidden') +
      ' bubble hidden=' + bubbleEl.classList.contains('hidden'));

    // Put the slot back to a known state for the scenarios that follow.
    window.__calls.length = 0;
    window.__closeSurface();
    window.__clearBubble();
    await sleep(60);

    // --- 3..7. (deleted) the panel, its two tabs and the slot frame ----------
    //
    // Five scenarios lived here and every one drove #panel: the 对话 tab as a mount point,
    // the slot resolution against four rows of the installed list, the host owning no
    // conversation chrome, the header's control-center button, the panel's box, the layer
    // that scrolled inside it, and Escape closing it.
    //
    // The panel is gone (openspec/changes/todo-pomodoro-as-plugins, stage 3), and each
    // guarantee has a home that can still fail:
    //
    //   - the slot's resolution rule: it is now the SURFACE's rule, and sections 18+ below
    //     assert it against the frames themselves (which plugin gets a frame, from which
    //     asset URL, sandboxed to allow-scripts, refusing a disabled plugin);
    //   - "the plugin is reachable, with nothing over it": the surface's own hit test, also
    //     below - the panel used to cover it, and now nothing does;
    //   - "the way into the control center": the pet's own menu cells plus the tray item,
    //     pinned by .dev/check-frontend.cjs (a browser cannot click a tray icon);
    //   - "the panel's box comes only from the layout": the surface geometry, below.
    //
    // What is asserted HERE is the absence, because a half-restored panel is the failure
    // mode that still matters: a tab strip with one tab, a #chat-slot nothing mounts into,
    // or an element at a coordinate the region does not cover.
    ok('the page has no panel, no tab strip and no chat slot',
      !document.getElementById('panel') && !document.querySelector('.tab') &&
        !document.getElementById('chat-slot') && !document.getElementById('close-panel'),
      'a panel element survived the panel');
    ok('the pet does not ask for a panel on any gesture',
      !window.__methods.some((m) => /Panel/.test(m)),
      'a panel binding is still exported: ' +
        window.__methods.filter((m) => /Panel/.test(m)).join(','));

    // The helpers the HEADLESS and SURFACE scenarios below share, defined here because the
    // panel block that used to define them is gone.
    //
    // resolveWith swaps what PluginsInstalled() answers and then WAITS for the page to pick it
    // up. It used to do that by clicking the 对话 tab, because a tab click was what re-read the
    // list; the panel's watcher did the same thing on a timer, and the tab click only existed
    // to skip that wait.
    //
    // With the panel gone, the timer is the ONLY path - which is exactly the property the old
    // case (h) below was added for, and it is now the only case there is. So this waits past
    // one period instead of clicking anything: touching nothing is what a market process
    // installing a plugin actually looks like from the page's side.
    const allRows = window.__installed.slice();
    const INSTALLED_WATCH_MS = 1500;
    const resolveWith = async (rows) => {
      window.__installed = rows;
      await sleep(INSTALLED_WATCH_MS + 500);
    };
    // Put the full row set back so the scenarios below start from what the fake was built
    // with: the checks above deliberately remove plugins, and a scenario that started from
    // whatever they left would assert against a state the product never produces.
    await resolveWith(allRows);
    // --- 8. Escape unwinds the ONE thing it can still unwind ------------------
    //
    // It used to be a chain: an edit, then a bubble, then the panel. The edit and the panel
    // both belonged to the panel, so a bubble is the whole chain now - and Escape on a page
    // with nothing up must do NOTHING rather than reach for a dismissal that has no target.
    //
    // The call log is cleared first: an earlier scenario's own DismissBubble (the pet menu
    // closing on a second right-click, for instance) would otherwise read as "Escape
    // dismissed a bubble".
    window.__calls.length = 0;
    await settle();
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await sleep(60);
    ok('Escape with nothing on screen does not dismiss anything',
      calls('DismissBubble').length === 0,
      'DismissBubble calls: ' + calls('DismissBubble').length);

    // --- 14..17. (deleted) the settings page, its switches and its layout ------
    //
    // Seven sections lived here and every one drove #tab-settings: the tab opening, the
    // voice-absence checks, the global-shortcut switch and its three states, the panel size
    // across a tab switch, the "State survives a save" round trip, and the measured settings
    // layout (shared column, no overflow, one scrolling column).
    //
    // The page they described does not exist any more (openspec/changes/ui-rebuild 阶段 3),
    // and each guarantee has a home that can still fail:
    //
    //   - the switch and its wiring: the control center owns it; svelte-check types it against
    //     the generated bindings, and .dev/check-frontend.cjs pins both halves by name;
    //   - "a refused registration reaches the user": still behavioural, still this page - see
    //     the toast assertion just below, which replaced the red row on the deleted page;
    //   - "a save binding returns the whole State": every setter in the control center is
    //     typed store.Config by the generated binding, so a narrower return is a compile
    //     error instead of a silently reverted pet picture.
    //
    // A page-level version of these would be testing a UI nobody ships.

    // --- 14b. the settings UI is gone from the live page ----------------------
    //
    // Queried against the DOM the app actually built, not against the source: a tab button
    // or a shortcut switch that came back would be a second writer of settings.json, and
    // that is the property the split exists to hold.
    //
    // The "refused registration is said out loud" assertion that belongs here could not be
    // driven from this harness: the page learns about it through applyState, and the fake
    // backend in this harness does not answer GetState at all (every refresh here is the
    // layout plus targeted events). That guarantee is pinned statically instead - see
    // .dev/check-frontend.cjs ("a refused registration still reaches the user"), which
    // asserts both the toast in main.js and the Problems field it reads.
    ok('the page has no settings tab and no shortcut switch',
      !document.getElementById('hotkey-enabled') && !document.getElementById('rem-enabled'),
      'settings UI came back into the pet');
    // The 记录 archive used to be asserted HERE as well, because it had moved into the
    // 待办 tab rather than leaving. It has left now: the archive was the host timer's,
    // and the pomodoro plugin keeps its own sessions in its own surface. So the
    // assertion is the absence, queried against the DOM the app actually built.
    ok('the host timer left no controls behind in the panel',
      !document.getElementById('records-list') &&
        !document.getElementById('records-fold') &&
        !document.getElementById('pomo-strip') &&
        !document.getElementById('pomo-toggle') &&
        !document.getElementById('timer-badge'),
      'a control the host timer owned is still on the page');

    // --- 18. the floating surface: geometry, handshake and routing -----------
    //
    // The surface is placed at the layout's rectangles, which ARE the ones the
    // native region is cut to, so the placement itself is asserted in 2b. What is
    // left is the PROTOCOL around it, and every part of that has to be observed
    // through a real sandboxed frame: the iframe's origin is opaque by design, so
    // the host cannot reach into it and the only way to learn what a plugin
    // received is for the plugin to say so. The probe served for /plugin-asset/
    // does exactly that - it announces itself and reports every protocol frame it
    // receives to the harness server.
    //
    // The surface is driven from the layout rather than by pressing the pet, so
    // the rectangles under test are the five written down in SURFACE_RECTS.
    window.__emit('pet:layout', layout(false, null, surfaceView()));
    await sleep(200);

    const surfaceFramesNow = () => [...surfaceMount.querySelectorAll('iframe')];
    ok('the surface mount point is up with one frame per rectangle',
      !surfaceMount.classList.contains('hidden') && surfaceFramesNow().length === 5,
      'hidden=' + surfaceMount.classList.contains('hidden') +
        ' frames=' + surfaceFramesNow().length);

    // (a) Every frame is told WHICH rectangle it owns. That is how a plugin that
    // declared several knows which one it is drawing into, and it is the one fact
    // the geometry alone cannot carry: the numbers say where the box is, not
    // which of the plugin's own blocks it represents.
    //
    // The log is emptied first, because a report accumulates for the whole run and
    // every layout the suite emits re-handshakes the frames already up: without
    // the reset this would count handshakes from scenarios that ran earlier and
    // the total would grow with each one.
    await probeReset();
    window.__emit('pet:layout', layout(false, null, surfaceView()));
    await sleep(200);
    const surfaceInits = (await probeReceivedWhen(
      (e) => e.filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.beta').length >= 5,
      6000,
    )).filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.beta');
    ok('a five-rectangle surface produces one init per frame (' + surfaceInits.length + ')',
      surfaceInits.length === 5,
      'inits: ' + surfaceInits.length);
    // The probes report no frame identity - a sandboxed frame has none to give -
    // so the assertion is made on the SET of (index, count, x) triples rather than
    // on report order. Ordering would be a property of how fast five documents
    // happen to load, which is exactly the kind of thing that makes a gate flaky.
    const initTriples = surfaceInits
      .map((m) => [m.surface && m.surface.rectIndex, m.surface && m.surface.rectCount,
        m.surface && m.surface.rect && m.surface.rect.x])
      .sort((a, b) => a[0] - b[0]);
    ok('each surface frame is told its own rectangle index, count and geometry',
      initTriples.length === 5 && initTriples.every((t, i) =>
        t[0] === i && t[1] === 5 && t[2] === SURFACE_RECTS[i].x),
      JSON.stringify(initTriples));
    // The frames are a FLOATING surface, so the handshake must say so - and must
    // not claim to be a panel: the two kinds render completely different things,
    // and a plugin told the wrong one draws the wrong interface.
    ok('the surface handshake says the frame is a floating surface, not a panel',
      surfaceInits.length === 5 &&
        surfaceInits.every((m) => m.surface && m.surface.kind === 'surface' &&
          m.surface.slot === undefined),
      JSON.stringify(surfaceInits.map((m) => m.surface)));

    // (b) The panel slot's own frame is told it is a PANEL and which slot it
    // fills. This is the other half of the same contract: a handshake that got
    // the kind right for one and wrong for the other is the failure this catches.
    //
    // The surface's handshake names what it is, and the PANEL kind is gone with the panel.
    //
    // This used to assert the panel handshake - kind 'panel' with slot 'chat' - and then
    // it asserted the floating surface's own handshake. The frame is taken away and handed
    // back first, because a handshake happens ONCE per frame and one that is merely reused
    // never says anything - reading an empty log would pass for the wrong reason.
    // The fixture's five-rectangle surface belongs to com.example.beta (see SURFACE_RECTS), so
    // that is the plugin whose handshake is asserted here. It used to be com.example.chat's
    // PANEL slot frame; the chat plugin is a surface now too, but the synthetic five-block
    // geometry above is the fixture that exists to exercise "one init per frame", so its own
    // handshake is the one worth reading.
    await probeReset();
    // Taken down first, then put back: the five frames from 2b are REUSED when the same
    // surface is re-applied (that is the point of the version check), and a reused frame never
    // says anything again. Clearing the surface destroys them, so the ones mounted here are
    // fresh documents with a handshake each.
    window.__emit('pet:layout', layout(false, null, null));
    await sleep(150);
    window.__emit('pet:layout', layout(false, null, surfaceView()));
    const surfaceProbe = (await probeReceivedWhen(
      (e) => e.filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.beta').length >= 5,
      8000,
    )).filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.beta');
    ok('every surface frame is told it is a floating surface, not a panel',
      surfaceProbe.length >= 5 &&
        surfaceProbe.every((m) => m.surface && m.surface.kind === 'surface' &&
          !('slot' in m.surface)),
      JSON.stringify(surfaceProbe.map((m) => m.surface)));

    // (c) An event for a plugin with frames on screen reaches ALL of them; one
    // for a plugin with nothing on screen is dropped rather than broadcast. The
    // frames are opaque origins, so "who is this for" can only come from the
    // payload, and broadcasting would hand one plugin another's stream.
    const eventsFor = (entries, requestId) => entries.filter(
      (m) => m && m.type === 'event' && m.name === 'llm.delta' &&
        m.payload && m.payload.requestId === requestId,
    );
    window.__emit('pet:plugin-event', {
      pluginId: 'com.example.beta', name: 'llm.delta', payload: { requestId: 'from-beta', text: '你' },
    });
    const afterBeta = await probeReceivedWhen((e) => eventsFor(e, 'from-beta').length >= 5, 5000);
    const delivered = eventsFor(afterBeta, 'from-beta');
    ok('a plugin event reaches every frame of its own plugin (' + delivered.length + ')',
      delivered.length === 5 && delivered.every((m) => m.payload.text === '你'),
      'received ' + delivered.length + ' of 5');

    // An event for a plugin with nothing on screen is dropped rather than
    // broadcast: the frames are opaque origins, so the only routing key is the
    // payload, and a broadcast would hand one plugin another's stream.
    const beforeNobody = afterBeta.filter((m) => m && m.type === 'event').length;
    window.__emit('pet:plugin-event', {
      pluginId: 'com.example.nobody', name: 'llm.delta', payload: { requestId: 'from-nobody', text: 'x' },
    });
    const afterNobody = await probeReceivedWhen(
      (e) => e.filter((m) => m && m.type === 'event').length > beforeNobody, 1500);
    ok('an event for a plugin with no interface is dropped',
      afterNobody.filter((m) => m && m.type === 'event').length === beforeNobody &&
        eventsFor(afterNobody, 'from-nobody').length === 0,
      'an unroutable event was delivered somewhere');

    // (c2) The HEADLESS frame.
    //
    // A plugin that declares "background" and holds "events" gets a permanently
    // mounted, hidden frame, and events reach it. This is what lets a voice plugin
    // speak while the panel is closed - without it the host drops the event with
    // "no interface on screen", which is precisely the gap the frame fills.
    //
    // The page is put back on the full row set first: the checks above deliberately
    // removed plugins, and a section that started from whatever they left would
    // assert against a state the product never produces.
    await resolveWith(allRows);
    const bgMount = document.getElementById('plugin-background');
    ok('the background mount point exists',
      !!bgMount, 'no #plugin-background element');
    // The frame must be HIDDEN: an unpainted-but-present iframe would still take
    // layout space and could swallow a click meant for the pet.
    ok('the background mount point is hidden, so the frame takes no space and no clicks',
      !!bgMount && getComputedStyle(bgMount).display === 'none',
      'display=' + (bgMount && getComputedStyle(bgMount).display));
    const bgFrames = () => [...bgMount.querySelectorAll('iframe')];
    ok('a plugin that declares background gets exactly one headless frame',
      bgFrames().length === 1, 'frames=' + bgFrames().length);
    ok('the headless frame is sandboxed to allow-scripts alone',
      bgFrames().length === 1 && bgFrames()[0].getAttribute('sandbox') === 'allow-scripts',
      'sandbox=' + (bgFrames()[0] && bgFrames()[0].getAttribute('sandbox')));

    // Its handshake names the slot "background", so a plugin that also fills a
    // visible slot can tell its two frames apart.
    //
    // The frame is taken away and handed back first, because a handshake happens
    // ONCE per frame and the frame mounted above has already had its - re-resolving
    // the same rows reuses it (by design: a rebuild per layout would restart the
    // plugin), so reading the log without a remount would find nothing and look
    // like a broken handshake. This mirrors what the slot-frame check does.
    await resolveWith(allRows.filter((p) => p.id !== 'com.example.bg'));
    await probeReset();
    await resolveWith(allRows);
    const bgInits = (await probeReceivedWhen(
      (e) => e.filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.bg').length >= 1,
      6000,
    )).filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.bg');
    ok('the headless frame is told it is a panel filling the background slot',
      bgInits.length >= 1 && bgInits[0].surface &&
        bgInits[0].surface.kind === 'panel' && bgInits[0].surface.slot === 'background',
      JSON.stringify(bgInits.map((m) => m.surface)));

    // And an event reaches it - the whole point of mounting it.
    const bgEvents = (entries) => entries.filter(
      (m) => m && m.type === 'event' && m.name === 'timer.phaseEnded');
    const beforeBg = bgEvents(await probeReceived()).length;
    window.__emit('pet:plugin-event', {
      pluginId: 'com.example.bg', name: 'timer.phaseEnded', payload: { phase: 'focus' },
    });
    const afterBg = await probeReceivedWhen((e) => bgEvents(e).length > beforeBg, 5000);
    ok('a timer.phaseEnded event reaches the headless frame',
      bgEvents(afterBg).length === beforeBg + 1 &&
        bgEvents(afterBg).every((m) => m.payload && m.payload.phase === 'focus'),
      'received ' + (bgEvents(afterBg).length - beforeBg) + ' background events');

    // The four gates, one at a time: each row must produce ZERO frames. They are
    // asserted as a group because the rule is "all four must hold", and a check
    // that only ever exercised the positive case would pass for a page that
    // mounted a frame for every plugin.
    const offRow = allRows.find((p) => p.id === 'com.example.off');
    const otherRow = allRows.find((p) => p.id === 'com.example.other');
    const bgRow = allRows.find((p) => p.id === 'com.example.bg');
    const withoutBackground = allRows.map((p) =>
      p.id === 'com.example.bg' ? { ...p, background: false } : p);
    const withoutGrant = allRows.map((p) =>
      p.id === 'com.example.bg' ? { ...p, granted: ['audio'] } : p);
    const disabledBg = allRows.map((p) =>
      p.id === 'com.example.bg' ? { ...p, enabled: false } : p);

    for (const [why, rows] of [
      ['does not declare it', withoutBackground],
      ['was not granted events', withoutGrant],
      ['is disabled', disabledBg],
    ]) {
      await resolveWith(rows);
      ok('no headless frame when the plugin ' + why, bgFrames().length === 0,
        'frames=' + bgFrames().length);
    }
    // A row that declares nothing and a row that is switched off are both already
    // in the fixture; they must not produce one either.
    ok('a plugin that never declared background gets no frame',
      otherRow && otherRow.background === false && offRow && offRow.enabled === false,
      'the fixture lost its negative rows');
    // Restored, so the checks below see the page as the product would have it.
    await resolveWith(allRows);
    ok('the headless frame comes back when the declaration and grant are restored',
      bgFrames().length === 1, 'frames=' + bgFrames().length);

    // (c2b) TWO headless frames at once.
    //
    // The QQ bridge is why this exists. It declares the background field and holds
    // the net.ws permission rather than events, and it cannot share a document with
    // the voice plugin - they are different programs' interfaces. The host used to
    // keep ONE frame and mount it for the first eligible plugin, so a second
    // background plugin silently took the first one's frame away; for a chat bridge
    // that means a dropped socket, and for the voice plugin a pomodoro nobody hears.
    //
    // The pair is chosen so the two GATES are also pinned separately: the first row is
    // eligible through events, the second through net.ws alone.
    const wsRow = {
      id: 'com.example.ws', name: '群聊桥插件', version: '1.0.0', author: 'shixiansi',
      description: '后台常驻的群聊桥', enabled: true,
      permissions: ['net.ws'], granted: ['net.ws'], pending: [], netHosts: ['127.0.0.1:3000'],
      panel: false, panelSlot: '', surface: false, background: true,
      entry: 'panel.html', dir: pluginDir('com.example.ws'), error: '',
    };
    await probeReset();
    await resolveWith([bgRow, wsRow]);
    ok('two eligible plugins get two headless frames at once',
      bgFrames().length === 2, 'frames=' + bgFrames().length);
    ok('both headless frames are sandboxed to allow-scripts alone',
      bgFrames().every((f) => f.getAttribute('sandbox') === 'allow-scripts'),
      JSON.stringify(bgFrames().map((f) => f.getAttribute('sandbox'))));
    const bgInitsBoth = await probeReceivedWhen(
      (e) => new Set(e.filter((m) => m && m.type === 'init' && m.surface &&
        m.surface.slot === 'background').map((m) => m.pluginId)).size >= 2,
      6000,
    );
    const bgPluginIds = [...new Set(bgInitsBoth
      .filter((m) => m && m.type === 'init' && m.surface && m.surface.slot === 'background')
      .map((m) => m.pluginId))].sort();
    ok('each headless frame is mounted for its own plugin, and net.ws alone is enough',
      bgPluginIds.length === 2 && bgPluginIds.includes('com.example.bg') &&
        bgPluginIds.includes('com.example.ws'),
      'ids=' + JSON.stringify(bgPluginIds));
    // Removing one leaves the other mounted: the sync is per id, not "all or nothing",
    // which is what makes disabling ONE background plugin safe.
    await resolveWith([wsRow]);
    ok('removing one background plugin leaves the other frame mounted',
      bgFrames().length === 1, 'frames=' + bgFrames().length);
    const survivor = await probeReceivedWhen(
      (e) => e.filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.ws').length >= 1,
      6000,
    );
    ok('the surviving frame belongs to the plugin that is still installed',
      survivor.some((m) => m && m.type === 'init' && m.pluginId === 'com.example.ws'),
      'ids=' + JSON.stringify(survivor.map((m) => m.pluginId)));
    // Restored once more: the checks below expect the normal row set.
    await resolveWith(allRows);

    // (c3) One plugin occupying BOTH the panel slot and the headless frame.
    //
    // This is the shape the voice plugin would take if it ever gained a visible panel
    // view, and it is the case where the two could collide: they are separate fields on
    // PluginHost, and a change that stored "the current frame" in one field would make
    // one of them silently replace the other. The assertion is that BOTH exist at once
    // and that each got its OWN init - so a plugin with two appearances can still tell
    // them apart by the slot name.
    //
    // ONE PLUGIN, TWO FRAMES - and the SLOT half of it is gone.
    //
    // This scenario used to prove that a plugin could hold the panel's 对话 tab AND a headless
    // frame at once, and that the two frames were told apart by their handshake ("chat" vs
    // "background"). The panel is gone, so the only appearance a plugin has now is its
    // FLOATING SURFACE; the request round trip below therefore runs through that frame instead
    // of the deleted slot frame.
    //
    // The handshake's slot field survives, and it is worth keeping the assertion for: the
    // headless frame must still say "background" so a plugin that also declares a surface can
    // tell its two documents apart. That is the half of the old check that still has a subject.
    const bothRow = {
      id: 'com.example.both', name: '两副面孔插件', version: '1.0.0', author: 'shixiansi',
      description: '同时声明浮层面与常驻帧', enabled: true,
      permissions: ['events', 'pet.surface'], granted: ['events', 'pet.surface'],
      pending: [], netHosts: [],
      panel: false, panelSlot: '', surface: true, background: true,
      entry: 'panel.html', dir: pluginDir('com.example.both'), error: '',
    };
    await probeReset();
    await resolveWith([bothRow]);
    const bothInits = (await probeReceivedWhen(
      (e) => e.filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.both').length >= 1,
      6000,
    )).filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.both');
    ok('the headless frame is told it is the background appearance',
      bothInits.some((m) => m.surface && m.surface.slot === 'background'),
      'slots=' + JSON.stringify(bothInits.map((m) => m.surface && m.surface.slot)));

    // Restored once more: the checks below expect the normal row set.
    await resolveWith(allRows);

    // (d) The request round trip, through the FLOATING SURFACE's frame.
    //
    // Every capability a plugin has goes through ONE gated binding, so this is the whole
    // plugin-to-host channel. The probe is told to send the request over the wire it really
    // uses - the host posts a command in, the probe posts a protocol request back - so what is
    // exercised is the assembled protocol rather than a shortcut around it.
    //
    // It used to be driven through the panel's slot frame. That frame is gone, and the floating
    // surface is the only visible frame left, so the round trip moves here - against the
    // CHAT plugin's surface, because that is the one the fixture mounts.
    const surfaceFrame = surfaceMount.querySelector('iframe');
    ok('the surface has a frame to talk to', !!surfaceFrame, 'no iframe in #plugin-surface');
    const responseRelays = (entries) => entries.filter((m) => m && m.type === 'response');
    // Drives one frame over the wire the plugin really uses: the host posts a command in, the
    // probe posts a protocol request back.
    const sendFromFrame = async (cmd, frame) => {
      if (!frame || !frame.contentWindow) return [];
      const before = responseRelays(await probeReceived()).length;
      frame.contentWindow.postMessage(cmd, '*');
      const after = await probeReceivedWhen((e) => responseRelays(e).length > before, 4000);
      return responseRelays(after).slice(before);
    };

    const dispatchedBefore = window.__pluginRequests.length;
    const responses = await sendFromFrame(
      { __cmd: 'request', id: 'rq1', method: 'todos.list', payload: {} },
      surfaceFrame,
    );
    const dispatched = window.__pluginRequests.slice(dispatchedBefore);
    ok('a plugin request reaches the single gated dispatcher',
      dispatched.length === 1 && dispatched[0].method === 'todos.list',
      'dispatched: ' + JSON.stringify(dispatched));
    ok('the dispatcher answer is returned to the plugin as a response',
      responses.length === 1 && responses[0].id === 'rq1' && responses[0].ok === true,
      'responses: ' + JSON.stringify(responses));
    ok('the response carries the value the binding returned',
      responses.length === 1 && responses[0].result && responses[0].result.method === 'todos.list',
      'result: ' + JSON.stringify(responses[0] && responses[0].result));
    // The short name the scenarios below use. It used to point at the panel's slot frame; the
    // floating surface is the only visible frame left, so both names are this one call.
    const sendFromProbe = (cmd) => sendFromFrame(cmd, surfaceFrame);
    // The failure path passes the host's own words through, so the plugin can
    // show the reason rather than a generic failure. The capability exercised is
    // plugin.config - the tenth permission, and the one whose refusal wording is
    // new with this change - because the plugin's own settings screen is where a
    // user is most likely to meet it.
    //
    // The refusal is asserted on the FLOATING surface's plugin: the slot frame it used to be
    // asserted against is gone with the panel, and the surface is the only visible frame left.
    // Driving it is what proves the refusal travels back down the channel the request came up.
    const realPluginRequest = window.__handlers.PluginRequest;
    window.__handlers.PluginRequest = () => {
      throw new Error('插件 com.example.beta 未获权限 plugin.config');
    };
    const failed = await sendFromFrame({
      __cmd: 'request', id: 'rq2', method: 'config.get', payload: {},
    }, surfaceFrame);
    ok('a refused request carries the host own reason back to the plugin',
      failed.length === 1 && failed[0].ok === false && /未获权限/.test(failed[0].error),
      'response: ' + JSON.stringify(failed));
    ok('the refusal names the capability the plugin asked for',
      failed.length === 1 && /plugin\.config/.test(failed[0].error),
      'error: ' + (failed[0] && failed[0].error));
    ok('the refusal names the plugin it was refused for',
      failed.length === 1 && /com\.example\.beta/.test(failed[0].error),
      'error: ' + (failed[0] && failed[0].error));
    window.__handlers.PluginRequest = realPluginRequest;

    // A malformed message is dropped WITHOUT a reply: answering one would give a
    // plugin a way to probe the host.
    const malformed = await sendFromProbe({
      __cmd: 'request', __omitId: true, method: 'todos.list', payload: {},
    });
    ok('a request without an id is dropped without a reply',
      malformed.length === 0,
      'the host replied to a malformed request: ' + JSON.stringify(malformed));

    // A ready from anywhere else must NOT be answered: without the source check,
    // any page in the webview could impersonate a plugin.
    const initsBefore = (await probeReceived()).filter((m) => m && m.type === 'init').length;
    window.dispatchEvent(Object.assign(new Event('message'), {
      data: { v: 1, type: 'ready' }, source: window,
    }));
    await sleep(300);
    ok('a ready from another window is ignored',
      (await probeReceived()).filter((m) => m && m.type === 'init').length === initsBefore,
      'a forged ready produced an init');

    // (e) The surface goes away entirely when the layout stops carrying one.
    window.__emit('pet:layout', layout(false, null, null));
    await sleep(300);
    const leftovers = surfaceFramesNow().length;
    ok('a layout with no surface hides the mount point and removes its frames',
      surfaceMount.classList.contains('hidden') && leftovers === 0,
      'frames left: ' + leftovers);

    return log;
  }

  window.__run = run;
`;

// --- drive it with a headless engine ---------------------------------------
const runner = `
  const checksJs = ${JSON.stringify(checksJs)};
  // Written next to the page so the module graph resolves.
`;

// The checks are written from a template string that contains Chinese literals.
// `fs.writeFileSync` defaults to UTF-8, and the page declares UTF-8, so the two
// agree; what must NOT happen is a PowerShell round trip through the file, which
// re-encodes it and turns every Chinese string into mojibake (and then the
// assertions compare against the wrong bytes).
//
// It is written into .dev/ rather than frontend/ so the repository stays clean:
// the harness is a development tool, and .dev is already ignored.
fs.writeFileSync(path.join(__dirname, 'harness-checks.js'), checksJs, 'utf8');

// A tiny static server: module scripts cannot be loaded over file://.
const MIME = {
  '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css',
  '.svg': 'image/svg+xml', '.json': 'application/json',
};
let resolveResult = null;
const resultArrived = new Promise((resolve) => {
  resolveResult = resolve;
});

// What the plugin probes reported. The server accumulates it and the page reads
// it back through /__probe-log.
//
// This channel exists because a sandboxed frame's window cannot be reached from
// the host page at all (its origin is opaque by design), so the ONLY way to learn
// what a plugin received is for the plugin to say. Reporting over postMessage
// would race the harness: a plugin announces itself the moment its script runs,
// which can precede the harness's listener, and a lost report is
// indistinguishable from a handshake that never happened.
const probeLog = [];

const server = http.createServer((req, res) => {
  // The page POSTs its results back here. This has to be the FIRST thing the
  // handler checks: otherwise it is treated as a static file request, 404s, and
  // the response is already committed by the time the report is read.
  if (req.method === 'POST' && req.url === '/result') {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      res.writeHead(204);
      res.end();
      resolveResult(body);
    });
    return;
  }

  // The plugin probes report here. The server ACCUMULATES, and the page reads
  // the log back through window.__probeLog(); a report that travelled over
  // postMessage could arrive before the harness installed its listener, and a
  // missing report is indistinguishable from a handshake that never happened.
  if (req.method === 'POST' && req.url === '/plugin-report') {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      // The probe lives in an opaque-origin sandbox, so this is a cross-origin
      // request from the browser's point of view and the response has to say so
      // - without it the fetch is sent but its promise rejects, and the probe's
      // own error path swallows that into a silent no-op.
      res.writeHead(204, {
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Headers': '*',
      });
      res.end();
      try {
        probeLog.push(JSON.parse(body));
      } catch {
        probeLog.push({ raw: body });
      }
    });
    return;
  }
  if (req.url === '/__probe-log') {

    // The page reads the log back through a cross-origin fetch of its own (the
    // page is at 127.0.0.1:<port> and so is this, but the browser still applies
    // the same rules to a fetch from a module script).
    res.writeHead(200, {
      'Content-Type': 'application/json',
      'Access-Control-Allow-Origin': '*',
      'Cache-Control': 'no-store',
    });
    res.end(JSON.stringify(probeLog));
    return;
  }
  // Empties the accumulated reports. A scenario that asserts "exactly N frames
  // each received one init" has to start from an empty log: reports accumulate
  // for the whole run, so earlier scenarios' handshakes would be counted again
  // and the count would grow with every layout the suite emits.
  if (req.method === 'POST' && req.url === '/__probe-reset') {
    probeLog.length = 0;
    res.writeHead(204, { 'Access-Control-Allow-Origin': '*' });
    res.end();
    return;
  }
  // CORS preflight for the probe's POST, should the browser decide to send one.
  if (req.method === 'OPTIONS') {
    res.writeHead(204, {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'POST, OPTIONS',
      'Access-Control-Allow-Headers': '*',
    });
    res.end();
    return;
  }

  const url = req.url.split('?')[0];
  // The plugin asset route, served the way the app serves it: the sandboxed
  // iframes point here, and a 404 would leave them with no document - which
  // makes the handshake assertions vacuous rather than wrong, the worse of the
  // two failures. The page is minimal on purpose; what is under test is the
  // postMessage protocol between host and frame, not the sample plugin's UI
  // (which has its own vitest suite in the market repository).

  if (url.startsWith('/plugin-asset/')) {
    res.writeHead(200, {
      'Content-Type': 'text/html',
      'Access-Control-Allow-Origin': '*',
      'Cache-Control': 'no-store',
    });
    // The probe page stands in for a plugin. It does the two things a real
    // plugin does - announces itself with `ready`, and listens - and then
    // relays everything it receives back to the host window, tagged so the
    // harness can tell a probe report from a host message.
    //
    // This is the ONLY way to observe a sandboxed frame from outside it: the
    // iframe's origin is opaque by design, so the harness cannot reach into
    // its window to intercept postMessage (attempting it throws SecurityError,
    // which is the sandbox working). Relaying from inside is also more faithful
    res.end(`<!doctype html><meta charset="utf-8"><title>plugin probe</title><script>
      // The probe is a plugin on the other side of the sandbox boundary. It can
      // only postMessage and fetch subresources, which is exactly the situation
      // a real plugin is in.
      //
      // Everything it RECEIVES is reported to the harness SERVER, not relayed
      // over postMessage. The relay form races the harness: a plugin announces
      // itself the moment its script runs, which is before the harness's own
      // listener is necessarily installed, and a report that goes missing is
      // indistinguishable from a handshake that never happened. A POST is
      // accumulated by the server and read back on demand, so nothing depends on
      // the two pages' timing.
      let nextId = 0;
      const report = (msg) => {
        try {
          fetch('/plugin-report', { method: 'POST', body: JSON.stringify(msg) }).catch(() => {});
        } catch (e) { /* the harness is not listening; the check will say so */ }
      };
      window.addEventListener('message', (ev) => {
        const d = ev.data;
        if (d && d.__cmd === 'request') {
          // A host command: do what a real plugin's user interface would do.
          const msg = { v: 1, type: 'request', method: d.method, payload: d.payload || {} };
          // __omitId exercises the host's malformed-message path.
          if (!d.__omitId) msg.id = d.id || ('p' + (++nextId));
          parent.postMessage(msg, '*');
          return;
        }
        if (d && d.__cmd === 'ready') {
          // Re-announce on demand. A plugin announces itself once, at startup,
          // which is usually before the harness reaches the handshake
          // assertions - so the probe can be asked to do it again when they run.
          parent.postMessage({ v: 1, type: 'ready' }, '*');
          return;
        }
        // A protocol frame (init, response, event) from the host. Record what
        // the plugin ACTUALLY received.
        report({ recv: d });
      });
      // Announce, the way every plugin does as soon as its script runs.
      report({ sent: { v: 1, type: 'ready' } });
      parent.postMessage({ v: 1, type: 'ready' }, '*');
    <\/script>`);
    return;
  }
  // The harness's own files live in .dev/, so map them explicitly: the static
  // root is frontend/ (so the built bundle resolves), and only the generated
  // checks module comes from elsewhere.
  let file;
  if (url === '/harness-checks.js') file = path.join(__dirname, 'harness-checks.js');
  else file = url === '/' ? built : path.join(frontend, url);
  if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) {
    res.writeHead(404);
    res.end('not found');
    return;
  }
  res.writeHead(200, { 'Content-Type': MIME[path.extname(file)] || 'application/octet-stream' });
  res.end(fs.readFileSync(file));
});

const edge = [
  'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
  'C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe',
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
].find((p) => fs.existsSync(p));

if (!edge) {
  console.error('no headless browser found');
  process.exit(2);
}

// The harness page imports the checks module and runs them, printing the result
// into the DOM (the title AND a marker element), which `--dump-dom` returns.
// The results are deliberately not read from the console: headless Chrome's
// --dump-dom is the reliable channel here, and console logging from a module
// after the load event is not always flushed before the dump.
const pageWithChecks = fs
  .readFileSync(built, 'utf8')
  .replace(
    '<script type="module" src="/src/main.js"></script>',
    `<script type="module" src="/src/main.js"></script>
     <script type="module">
       import './harness-checks.js';
       const report = (payload) =>
         fetch('/result', { method: 'POST', body: JSON.stringify(payload) }).catch(() => {});
       // Any error before, during or after the scenarios must still be reported,
       // or the harness fails as "never reported back" with no explanation.
       window.addEventListener('error', (e) =>
         report({ ok: false, error: 'window error: ' + (e.message || e.error) + '\n' + (e.error?.stack || '') }));
       window.addEventListener('unhandledrejection', (e) =>
         report({ ok: false, error: 'unhandled rejection: ' + (e.reason?.stack || e.reason) }));
       try {
         // main.js's boot() is async; give it a moment, then run the scenarios.
         await new Promise((r) => setTimeout(r, 400));
         const log = await window.__run();
         await report({ ok: true, log });
       } catch (err) {
         await report({ ok: false, error: err && err.stack ? err.stack : String(err) });
       }
     </script>`,
  );
fs.writeFileSync(built, pageWithChecks, 'utf8');

server.listen(0, () => {
  const port = server.address().port;
  const url = `http://127.0.0.1:${port}/`;
  console.log(`harness serving ${url}`);

  const child = require('child_process').spawn(
    edge,
    [
      '--headless=old',
      '--disable-gpu',
      '--no-sandbox',
      '--disable-extensions',
      '--no-first-run',
      '--disable-sync',
      '--user-data-dir=' + path.join(__dirname, 'harness-profile'),
      '--virtual-time-budget=60000',
      url,
    ],
    { stdio: 'ignore' },
  );

  const timeout = new Promise((r) => setTimeout(() => r(null), 120000));

  Promise.race([resultArrived, timeout]).then((body) => {
    try {
      child.kill();
    } catch {
      /* already gone */
    }
    server.close();

    if (body === null) {
      console.error('the page never reported back');
      process.exit(2);
    }

    // The POST body is consumed by our own handler, so it is plain JSON rather
    // than multipart: fetch() with a string body sends it as text/plain.
    let payload;
    try {
      payload = JSON.parse(body.trim().replace(/^=+/, ''));
    } catch (err) {
      console.error('could not parse the report:', body.slice(0, 2000));
      process.exit(2);
    }

    if (!payload.ok) {
      console.error('the harness crashed:\n' + payload.error);
      process.exit(2);
    }

    const log = payload.log || [];
    for (const l of log) {
      console.log(`  ${l.ok ? 'PASS' : 'FAIL'}  ${l.name}${l.ok ? '' : ' -- ' + l.detail}`);
    }
    const failed = log.filter((l) => !l.ok);
    console.log('');
    if (failed.length) {
      console.error(`FRONTEND HARNESS FAILED (${failed.length} of ${log.length})`);
      process.exit(1);
    }
    console.log(`FRONTEND HARNESS PASSED (${log.length} checks)`);
  });
});
