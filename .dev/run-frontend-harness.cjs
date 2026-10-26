/**
 * Frontend behaviour harness.
 *
 * Loads the REAL html, CSS and JS modules in a real browser engine against a
 * fake Wails bridge, then drives the app the way a user would and asserts on
 * what it asked the backend to do. This is the substitute for a live GUI run:
 * WebView2 cannot create a controller in this environment (the pre-change binary
 * fails identically), so the click/double-click split, the streaming renderer
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
      const handler = window.__handlers[name];
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
    'AddTodo', 'ChoosePetImage', 'ClearDone', 'ClosePluginSurface',
    'ConfirmPomodoroStart', 'DebugLog', 'DeleteTodo', 'DismissBubble', 'DragTo',
    'EditPomodoroRecord', 'EndDrag', 'GetLayout', 'GetState', 'HidePet',
    'HidePetForUser', 'OpenChat', 'OpenChatPluginSurface', 'OpenPluginMarket',
    'OpenPluginSurface', 'OpenTodoPanelFromBubble', 'OpenTodoPanelFromChat',
    'PausePomodoro', 'PetImageURL', 'PluginMarketCandidates',
    'PluginMarketPath', 'PluginRequest', 'PluginsInstalled', 'PomodoroRecords',
    'Quit', 'Ready', 'RemindNow', 'ResetPetImage', 'ResetPomodoro',
    'ResetToDefaultPosition', 'ResumePomodoro',
    'SaveHotkeySettings', 'SavePomodoroSettings', 'SavePomodoroSummary',
    'SaveReminderSettings', 'SetAutostart', 'SetBubbleHover',
    'SetPanelOpen', 'SetPomodoroEnabled', 'SetReminderEnabled', 'SetScale',
    'SetSnapToEdge', 'ShowPet', 'ShowPetForUser', 'ShowPomodoroPicker', 'SkipPhase',
    'SnapNow', 'StartDrag', 'StartPomodoro', 'StartPomodoroWithPlan', 'StopPomodoro',
    'TogglePanel', 'ToggleTodo', 'UpdateTodo',
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
      pomodoroOnStartup: false, autoStartNextPhase: true, remindDuringBreak: true,
      pomodoroSound: true, focusMinutes: 25, shortBreakMinutes: 5,
      longBreakMinutes: 15, longBreakEvery: 4,
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
    pomodoro: { enabled: false, phase: 'idle', runState: 'idle', label: '番茄钟未开启',
      remainingSec: 0, countdown: '', endsAtMs: 0, running: false, focusing: false,
      completedCount: 0, longBreakEvery: 4, untilLongBreak: 0, pendingSummary: false },
    records: [], recordTotals: { count: 0, focusMinutes: 0 },
    reminder: { enabled: false },
    // There is no "voice" block in this fixture: speech is a plugin now, and the pet
    // reports nothing about it. The panel therefore has no voice section to render,
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
      permissions: ['todos.read'], granted: ['todos.read'], pending: [], netHosts: [],
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
    TogglePanel: () => { panelOpen = !panelOpen; if (panelOpen) { currentSurface = null; currentBubble = null; } return layout(panelOpen, currentBubble, currentSurface); },
    SetPanelOpen: (open) => { panelOpen = open; if (open) { currentSurface = null; currentBubble = null; } return layout(open, currentBubble, currentSurface); },
    // The conversation is a tab inside the panel, so OpenChat opens the panel and
    // the frontend switches the tab from the event.
    OpenChat: () => { panelOpen = true; currentSurface = null; currentBubble = null; return layout(true); },
    OpenTodoPanelFromChat: () => { panelOpen = true; currentSurface = null; currentBubble = null; return layout(true); },
    // Dismissing the summary prompt is refused by the real backend while a record
    // is pending, so the fake refuses too - otherwise the escape hatches would look
    // like they work here and fail on the real window.
    DismissBubble: () => {
      if (currentBubble && currentBubble.kind === 'summary') return layout(panelOpen, currentBubble);
      currentBubble = null;
      return layout(panelOpen);
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
    // The double-click target: the chat slot's own plugin, or the explanation
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
          title: '对话插件还没装好：在设置里点「打开插件市场」装上并授权它。',
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
    // The real SavePomodoroSummary writes the record, clears the prompt and
    // RETURNS the whole state. The fake must clear it too: without that the next
    // summary prompt would arrive while the previous one is still "showing", the
    // kind would not change, and the card would never be re-rendered - which would
    // make the stale-handler check below pass for the wrong reason.
    SavePomodoroSummary: (recordId, summary) => {
      currentBubble = null;
      window.__savedSummary = { recordId, summary };
      return window.__handlers.GetState();
    },
  };

  // --- helpers to drive the UI ----------------------------------------------
  const pet = document.getElementById('pet');
  const petRect = { x: 100, y: 300 };

  // Models what Go does when the double click resolves to the chat plugin's own
  // floating surface: the surface takes the slot from the panel and the bubble,
  // and the layout carries the rectangles. The checks cannot set the closure
  // variable themselves, so the fake exposes the transition.
  window.__openSurface = () => {
    panelOpen = false;
    currentBubble = null;
    currentSurface = surfaceView();
    return currentSurface;
  };

  // Raises the summary prompt the way Go does: the bubble takes the slot (the
  // panel and the surface go away) and pet:bubble announces it, then the layout
  // carries the card and its geometry.
  //
  // The geometry is the real one for this kind - 300x160 at the panel's slot,
  // centred on the pet's band - because the frontend positions the card verbatim
  // from it and a made-up rectangle would let a placement bug pass.
  window.__showSummaryPrompt = (recordId) => {
    panelOpen = false;
    currentSurface = null;
    currentBubble = {
      kind: 'summary',
      title: '这一个番茄做了什么？',
      todoTitle: '写周报',
      recordId: recordId || 'rec-1',
      x: 168, y: 180, w: 300, h: 160, onRight: true,
    };
    window.__emit('pet:bubble', { kind: 'summary', shown: true });
    // The layout is what actually paints the card: the frontend renders the bubble
    // FROM the layout, never from the event alone (that is the bug that once made
    // the prompt impossible to dismiss).
    window.__emit('pet:layout', layout(false, currentBubble));
    return layout(false, currentBubble);
  };

  // Takes the prompt down and repaints, so the scenarios that follow start from a
  // known slot.
  window.__clearBubble = () => {
    currentBubble = null;
    window.__emit('pet:bubble', { kind: 'summary', shown: false });
    window.__emit('pet:layout', layout(panelOpen, null, currentSurface));
    return layout(panelOpen, null, currentSurface);
  };

  function pressPet(times) {
    // Two presses within the double-click window. The pointer handlers are what
    // the app actually listens to, so the harness dispatches those rather than
    // synthetic click events.
    const fire = (type, extra = {}) => pet.dispatchEvent(new PointerEvent(type, {
      bubbles: true, cancelable: true, button: 0, buttons: 1,
      pointerId: 1, pointerType: 'mouse',
      screenX: petRect.x, screenY: petRect.y, ...extra,
    }));
    fire('pointerdown');
    fire('pointerup');
    if (times === 2) { fire('pointerdown'); fire('pointerup'); }
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

    // Grab the surfaces once: they are referenced throughout.
    const panelEl = document.getElementById('panel');
    const chatTab = document.getElementById('tab-chat');
    const surfaceMount = document.getElementById('plugin-surface');

    // Reset the click bookkeeping between scenarios.
    //
    // The wait has to exceed DOUBLE_CLICK_MS comfortably: a single click schedules
    // its toggle 200ms out, so a scenario that starts too soon has the PREVIOUS
    // scenario's timer fire in the middle of it and overwrite the layout under
    // test. That produced a very convincing "the card does not show" failure that
    // was purely a harness race.
    const settle = () => sleep(500);

    // --- 1. single click toggles the panel, after the double-click window ----
    panelOpen = false;
    await settle();
    pressPet(1);
    ok('single click does not act immediately', !panelOpen, 'panel opened synchronously');
    await sleep(300);
    ok('single click opens the panel after the delay', panelOpen === true, 'panel state ' + panelOpen);
    // The single click must not reach for the conversation plugin either: the two
    // gestures share the pointer stream, and a single click that opened the
    // surface would make the panel impossible to open at all.
    ok('single click never asks for the plugin surface',
      calls('OpenChatPluginSurface').length === 0, 'OpenChatPluginSurface was called');

    // --- 2. double click asks the conversation plugin for its surface --------
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
    pressPet(2);
    ok('double click asks the conversation plugin for its surface',
      calls('OpenChatPluginSurface').length === 1,
      'OpenChatPluginSurface calls: ' + calls('OpenChatPluginSurface').length);
    ok('double click does not open the panel tab', calls('OpenChat').length === 0, 'OpenChat calls: ' + calls('OpenChat').length);
    ok('double click does not toggle the panel', calls('TogglePanel').length === 0, 'TogglePanel calls: ' + calls('TogglePanel').length);
    await sleep(300);
    ok('double click does not toggle the panel afterwards', calls('TogglePanel').length === 0, 'TogglePanel calls: ' + calls('TogglePanel').length);

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

    // --- 2c. the surface must NOT eat the pomodoro summary prompt ------------
    //
    // The surface and the speech bubble share one slot and OpenPluginSurface TAKES
    // it, so a double click on the pet used to delete the summary prompt - leaving
    // the timer parked behind a question that was no longer on screen. The refused
    // gesture is turned into "go answer it": the caret lands in the box.
    //
    // The refusal is the BACKEND's (OpenChatPluginSurface never reaches the plugin
    // while a question waits), but the frontend has to check too, or the refusal
    // arrives as an error toast that cannot be seen - the toast lives inside the
    // panel, which is necessarily closed while the bubble is up.
    await settle();
    window.__calls.length = 0;
    const bubbleEl = document.getElementById('bubble');
    const bubbleInput = document.getElementById('bubble-input');
    window.__showSummaryPrompt('rec-42');
    await waitFor('the summary prompt', () => !bubbleEl.classList.contains('hidden'));
    ok('the summary prompt is up',
      !bubbleEl.classList.contains('hidden') && !bubbleInput.classList.contains('hidden'),
      'bubble hidden=' + bubbleEl.classList.contains('hidden') + ' input hidden=' + bubbleInput.classList.contains('hidden'));
    ok('the prompt advertises Enter in its placeholder',
      bubbleInput.placeholder === '写点什么…（Enter 保存）',
      'placeholder: ' + bubbleInput.placeholder);

    // The double click that would open the conversation instead answers the prompt.
    document.activeElement?.blur?.();
    pressPet(2);
    await sleep(120);
    ok('a double click does NOT ask for the plugin surface over the prompt',
      calls('OpenChatPluginSurface').length === 0,
      'OpenChatPluginSurface calls: ' + calls('OpenChatPluginSurface').length);
    ok('the surface stayed closed', surfaceMount.classList.contains('hidden'), 'the surface opened anyway');
    ok('the refused gesture sent the caret to the summary box',
      document.activeElement === bubbleInput, 'active element: ' + (document.activeElement?.id || document.activeElement?.tagName));
    ok('the prompt survived the gesture',
      !bubbleEl.classList.contains('hidden'), 'the prompt was eaten by the double click');

    // Enter confirms it, without the mouse. The backend is told the record id the
    // prompt carries, not a stale one from a previous session.
    window.__calls.length = 0;
    bubbleInput.value = '写完了数据部分';
    bubbleInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
    await waitFor('SavePomodoroSummary from Enter', () => calls('SavePomodoroSummary').length === 1);
    ok('Enter saved the summary against the record the prompt carried',
      last('SavePomodoroSummary')?.args[0] === 'rec-42' && last('SavePomodoroSummary')?.args[1] === '写完了数据部分',
      'args: ' + JSON.stringify(last('SavePomodoroSummary')?.args));

    // Shift+Enter must still insert a newline: a summary is prose, and Enter is
    // now claimed.
    await sleep(60);
    window.__showSummaryPrompt('rec-43');
    await waitFor('the second prompt', () => !bubbleEl.classList.contains('hidden'));
    // Enter must not have leaked the previous prompt's handler: the fake replaces
    // the card rather than rebuilding it, so a stale handler saving rec-42 would
    // show up here as a call with the wrong id.
    bubbleInput.value = '第二轮';
    window.__calls.length = 0;
    const shiftEnter = new KeyboardEvent('keydown', {
      key: 'Enter', shiftKey: true, bubbles: true, cancelable: true,
    });
    bubbleInput.dispatchEvent(shiftEnter);
    await sleep(60);
    ok('Shift+Enter does not save the summary',
      calls('SavePomodoroSummary').length === 0,
      'SavePomodoroSummary calls: ' + calls('SavePomodoroSummary').length);
    ok('Shift+Enter is left to the textarea',
      !shiftEnter.defaultPrevented, 'the keydown was cancelled, so no newline can appear');

    // An IME composition commit must not save either: typing Chinese, that Enter
    // picks a candidate, and saving on it would store a half-finished summary.
    window.__calls.length = 0;
    bubbleInput.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Enter', isComposing: true, bubbles: true, cancelable: true,
    }));
    await sleep(60);
    ok('an IME Enter does not save the summary',
      calls('SavePomodoroSummary').length === 0,
      'SavePomodoroSummary calls: ' + calls('SavePomodoroSummary').length);

    // And the prompt that replaced it answers its OWN record.
    window.__calls.length = 0;
    bubbleInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
    await waitFor('the second save', () => calls('SavePomodoroSummary').length === 1);
    ok('the second prompt saved its own record, not the first one',
      last('SavePomodoroSummary')?.args[0] === 'rec-43' && last('SavePomodoroSummary')?.args[1] === '第二轮',
      'args: ' + JSON.stringify(last('SavePomodoroSummary')?.args));

    // Put the slot back to a known state for the scenarios that follow.
    window.__clearBubble();
    await sleep(60);

    // --- 3. the 对话 tab is a MOUNT POINT, and the slot resolves from the list
    //
    // The host has no conversation of its own any more: the tab body holds one
    // element, and which plugin's interface lands in it is decided from the
    // installed list. That makes the resolution RULE the thing worth asserting -
    // three rows are installed here and only one of them may win.
    //
    // The panel is opened with the pet's own single click and then the tab is
    // clicked, which is the only way in: the double click opens the plugin's
    // floating surface, not the panel, and the tab strip does not open the panel
    // by itself.
    if (panelEl.classList.contains('hidden')) {
      await settle();
      pressPet(1);
      await sleep(350);
    }
    document.querySelector('.tab[data-tab="chat"]').dispatchEvent(new Event('click', { bubbles: true }));
    await sleep(200);
    ok('the panel is open', !panelEl.classList.contains('hidden'), 'the panel is hidden');
    ok('the conversation tab is showing', !chatTab.classList.contains('hidden'), 'the chat tab is hidden');
    ok('the todo tab is hidden', document.getElementById('tab-todos').classList.contains('hidden'), 'the todo tab is visible');
    ok('the panel box matches the layout Go sent',
      panelEl.style.width === '380px' && panelEl.style.height === '520px',
      'panel box is ' + panelEl.style.width + 'x' + panelEl.style.height);
    ok('the panel is clipped, so nothing can spill onto the desktop',
      getComputedStyle(panelEl).overflow === 'hidden', 'panel overflow is ' + getComputedStyle(panelEl).overflow);

    const slotEl = document.getElementById('chat-slot');
    const slotFrames = () => [...slotEl.querySelectorAll('iframe')];
    ok('the slot mounted the enabled plugin that claims the 对话 slot',
      slotFrames().length === 1 &&
        slotFrames()[0].dataset.pluginId === 'com.example.chat' &&
        slotFrames()[0].dataset.slot === 'chat',
      'frames: ' + slotFrames().map((f) => f.dataset.pluginId + '/' + f.dataset.slot).join(','));
    // The sandbox is the security boundary, so it is asserted as an exact string
    // on the SLOT frame too: allow-same-origin is what would hand a plugin the
    // host page's DOM, and it must never appear on any frame.
    ok('the slot frame is sandboxed to allow-scripts alone',
      slotFrames().length === 1 && slotFrames()[0].getAttribute('sandbox') === 'allow-scripts' &&
        slotFrames()[0].getAttribute('allow') === null,
      'sandbox=' + (slotFrames()[0] && slotFrames()[0].getAttribute('sandbox')));
    ok('the slot frame is loaded from the host asset route',
      slotFrames().length === 1 &&
        slotFrames()[0].getAttribute('src') === '/plugin-asset/com.example.chat/panel.html',
      'src=' + (slotFrames()[0] && slotFrames()[0].getAttribute('src')));
    // The slot's own box was sized by the stylesheet before any plugin existed,
    // and mounting must not change it: the panel's rectangle - which is what the
    // native window region is cut to - is the thing this protects.
    ok('mounting a plugin did not resize the slot',
      getComputedStyle(slotEl).overflow === 'hidden' || slotEl.clientHeight > 0,
      'slot box: ' + slotEl.clientWidth + 'x' + slotEl.clientHeight);
    ok('the hint is gone once a plugin fills the slot',
      !slotEl.classList.contains('chat-slot-empty') &&
        !(slotEl.textContent || '').includes('对话插件还没装好'),
      'the empty hint is still in the slot: ' + slotEl.textContent);

    // --- 3b. the slot resolution follows the installed list, live -------------
    //
    // The market program can install, remove or switch off the chat plugin while
    // the pet is running, and the slot is re-resolved on every arrival at the tab
    // for exactly that reason. Each of the three ways the slot can lose its owner
    // is exercised here, because a resolution that only looked at "is anything
    // installed" would pass the first check and fail all three.
    const resolveWith = async (rows) => {
      window.__installed = rows;
      document.querySelector('.tab[data-tab="todos"]').dispatchEvent(new Event('click', { bubbles: true }));
      await sleep(40);
      document.querySelector('.tab[data-tab="chat"]').dispatchEvent(new Event('click', { bubbles: true }));
      await sleep(150);
    };
    const allRows = window.__installed.slice();

    // (a) The claiming plugin switched off: the slot must fall back to the hint
    // rather than keep a switched-off plugin's interface on screen.
    await resolveWith(allRows.map((p) => (p.id === 'com.example.chat' ? { ...p, enabled: false } : p)));    // The hint may be the GENERIC sentence or the specific diagnosis - what this
    // asserts is that a hint is shown at all, and that no frame is mounted. The
    // diagnosis names the actual reason when the installed rows can explain it,
    // which is strictly more useful than the generic sentence and must not be
    // mistaken for the slot silently succeeding.
    ok('a switched-off chat plugin loses the slot',
      slotFrames().length === 0 && slotEl.classList.contains('chat-slot-empty') &&
        (slotEl.textContent || '').length > 0,
      'frames=' + slotFrames().length + ' hint=' + slotEl.classList.contains('chat-slot-empty') +
        ' text=' + slotEl.textContent);

    // (b) The grant revoked: a plugin that may not draw outside the pet's own box
    // must not fill the tab, or the slot would be a way around the prompt.
    await resolveWith(allRows.map((p) => (p.id === 'com.example.chat' ? { ...p, granted: ['llm.chat'] } : p)));
    ok('a chat plugin without the surface grant loses the slot',
      slotFrames().length === 0 && slotEl.classList.contains('chat-slot-empty'),
      'frames=' + slotFrames().length);

    // (c) Uninstalled: nothing left to mount.
    await resolveWith(allRows.filter((p) => p.id !== 'com.example.chat'));
    // The row that claimed the slot is gone, but com.example.other is still enabled
    // and claims no slot - so the DIAGNOSIS fires here and names it as an old
    // version rather than falling back to the generic sentence. Both are correct;
    // what must hold is that nothing is mounted and something is said.
    ok('an uninstalled chat plugin leaves the slot empty',
      slotFrames().length === 0 && (slotEl.textContent || '').length > 0,
      'text: ' + slotEl.textContent);

    // (d) A plugin that is enabled and granted but claims NO slot must not be
    // mounted: the row exists, and only the manifest's own declaration says
    // whether it belongs in the 对话 tab.
    await resolveWith([allRows.find((p) => p.id === 'com.example.other')]);
    ok('an enabled plugin that claims no slot is not mounted',
      slotFrames().length === 0,
      'frames=' + slotFrames().length);

    // And back to the full list, so the rest of the run starts from the state the
    // fake was built with.
    await resolveWith(allRows);
    ok('the chat plugin takes the slot back when it is installed and enabled again',
      slotFrames().length === 1 && slotFrames()[0].dataset.pluginId === 'com.example.chat',
      'frames: ' + slotFrames().map((f) => f.dataset.pluginId).join(','));

    // (e) The plugin's interface must be REACHABLE - nothing may sit on top of it.
    //
    // The user's report was that the plugin renders inside the panel but none of its
    // controls respond, which is what a transparent overlay swallowing the clicks
    // looks like. The check is the browser's own hit test: ask what element is at
    // the centre of the slot's box, and require that it is the plugin's frame (or
    // something inside it - the frame is a separate document, so the browser reports
    // the IFRAME element itself from the parent).
    //
    // This is asserted here because it is the ONE property that separates "the
    // plugin is drawn" from "the plugin can be used", and nothing else in this suite
    // covers it: every other check is about markup, geometry or the protocol.
    const slotBox = slotEl.getBoundingClientRect();
    const hit = document.elementFromPoint(
      slotBox.left + slotBox.width / 2,
      slotBox.top + slotBox.height / 2,
    );
    const hitChain = [];
    for (let el = hit; el; el = el.parentElement) {
      hitChain.push(el.id ? '#' + el.id : el.tagName.toLowerCase() +
        (el.className && typeof el.className === 'string' ? '.' + el.className.split(' ')[0] : ''));
    }
    ok('the plugin slot is reachable, with nothing covering it',
      hit === slotFrames()[0],
      'the element at the slot centre is ' + hitChain.join(' < ') +
        ' (want the plugin iframe)');

    // (f) The 对话 TAB itself comes and goes with the plugin.
    //
    // The pet has no conversation of its own - the tab is a mount point - so with
    // no plugin to fill it the tab would open onto an explanation and nothing
    // else. The button is therefore hidden, and the panel is moved off the tab if
    // it was showing it: a hidden tab whose body is still on screen leaves the tab
    // strip claiming 待办 while the conversation's box occupies the panel.
    const chatTabBtn = document.getElementById('tab-chat-btn');
    ok('the conversation tab is offered while a plugin fills it',
      !!chatTabBtn && !chatTabBtn.classList.contains('hidden'),
      'the tab button is hidden although a plugin owns the slot');

    // Take the plugin away while the conversation is the ACTIVE tab, which is the
    // case that has to move the panel.
    document.querySelector('.tab[data-tab="chat"]').dispatchEvent(new Event('click', { bubbles: true }));
    await sleep(120);
    ok('the conversation tab is active before the plugin goes away',
      chatTabBtn.classList.contains('active') && !chatTab.classList.contains('hidden'),
      'the tab did not become active');

    await resolveWith(allRows.filter((p) => p.id !== 'com.example.chat'));
    // A row that claims no slot is still installed, so the DIAGNOSIS renders; what
    // matters here is the tab, not the sentence.
    await sleep(160);
    ok('the conversation tab disappears with the plugin',
      chatTabBtn.classList.contains('hidden'),
      'the tab button is still offered although no plugin fills it');
    ok('the panel moves off the conversation tab when it goes away',
      document.getElementById('tab-todos').classList.contains('hidden') === false &&
        chatTab.classList.contains('hidden'),
      'the panel stayed on the conversation tab: todos hidden=' +
        document.getElementById('tab-todos').classList.contains('hidden') +
        ' chat hidden=' + chatTab.classList.contains('hidden'));
    ok('the tab strip shows the todo tab as the active one',
      document.querySelector('.tab[data-tab="todos"]').classList.contains('active'),
      'no tab is marked active after the conversation tab went away');

    // A request from GO to show the conversation must not reopen it. Go resolves
    // the slot on its own, so this can arrive before the page's next snapshot.
    window.__emit('pet:chat-open', { open: true });
    await sleep(120);
    ok('a request from the host cannot reopen the removed conversation tab',
      document.getElementById('tab-todos').classList.contains('hidden') === false,
      'pet:chat-open put the panel back on a tab that no longer exists');

    // Restore the plugin and the active tab, so the rest of the run starts from the
    // state the fake was built with.
    await resolveWith(allRows);
    await sleep(120);
    document.querySelector('.tab[data-tab="todos"]').dispatchEvent(new Event('click', { bubbles: true }));
    await sleep(80);

    // (g) An UPDATED plugin gets a NEW document, and an unchanged one does not.
    //
    // An update in the market keeps the plugin id and replaces the files behind it,
    // so the id alone cannot tell "same plugin, re-laid out" from "same plugin,
    // updated". The version is what distinguishes them, and both halves matter:
    // reusing on an update leaves the old interface running while the market says
    // it was updated, and remounting on every snapshot would restart the plugin's
    // conversation each time any state changed.
    {
      const before = slotFrames()[0];
      ok('the slot frame records the version it was loaded from',
        !!before && before.dataset.pluginVersion === '1.1.0',
        'frame version = ' + (before && before.dataset.pluginVersion));

      // Same rows, arriving again: the frame must be REUSED. This is the case that
      // protects an in-progress conversation from being thrown away on every state
      // push.
      await resolveWith(allRows);
      await sleep(120);
      ok('an unchanged plugin keeps its running document',
        slotFrames()[0] === before,
        'the frame was rebuilt even though the plugin did not change');

      // The same id at a new version: the frame must be REPLACED.
      const bumped = allRows.map((p) => (
        p.id === 'com.example.chat' ? { ...p, version: '1.2.0' } : p
      ));
      await resolveWith(bumped);
      await sleep(120);
      const after = slotFrames();
      ok('an updated plugin replaces its running document',
        after.length === 1 && after[0] !== before && after[0].dataset.pluginVersion === '1.2.0',
        'frames=' + after.length +
          ' same=' + (after[0] === before) +
          ' version=' + (after[0] && after[0].dataset.pluginVersion));

      // Back to the original version for the rest of the run.
      await resolveWith(allRows);
      await sleep(120);
      ok('the slot is back on the installed version',
        slotFrames().length === 1 && slotFrames()[0].dataset.pluginVersion === '1.1.0',
        'version=' + (slotFrames()[0] && slotFrames()[0].dataset.pluginVersion));
    }

    // (h) The tab hides with NO CLICK AT ALL when the plugin goes away.
    //
    // This is the user's report: 「取消点击，面板上的对话tab没有及时的隐藏」. Every
    // other case here reaches the slot through resolveWith, which CLICKS the tab
    // strip - and that click is what used to re-read the installed list, so those
    // cases passed while the real behaviour was broken.
    //
    // The marketplace is a separate process, so the only honest way to model it is
    // to change what the binding answers and then WAIT, touching nothing. The period
    // is 1.5s, so this waits past it with margin.
    {
      ok('the conversation tab is showing before the plugin is disabled',
        !chatTabBtn.classList.contains('hidden'),
        'the tab is already hidden, so this case would be vacuous');

      // What the market does when it disables the plugin. Nothing is dispatched.
      window.__installed = allRows.map((p) => (
        p.id === 'com.example.chat' ? { ...p, enabled: false } : p
      ));
      await sleep(1900);

      ok('the conversation tab hides on its own when the plugin is disabled',
        chatTabBtn.classList.contains('hidden'),
        'the tab is still offered 1.9s after the plugin was disabled, with no click');
      ok('the disabled plugin has no document left in the slot',
        slotFrames().length === 0,
        'frames=' + slotFrames().length);

      // And it comes back by itself, with no click, when the plugin is re-enabled.
      window.__installed = allRows;
      await sleep(1900);
      ok('the conversation tab comes back on its own when the plugin is re-enabled',
        !chatTabBtn.classList.contains('hidden'),
        'the tab is still hidden 1.9s after the plugin was re-enabled, with no click');
      ok('the re-enabled plugin is mounted again',
        slotFrames().length === 1,
        'frames=' + slotFrames().length);
    }

    // --- 4. the host no longer offers any conversation control ---------------
    //
    // This is the point of the change, so it is asserted STRUCTURALLY rather than
    // by "the old id is absent from the script": the tab body must contain the
    // slot and nothing else, and the host page must own no transcript, no input
    // and no send button anywhere. A leftover control would be a second, drifting
    // copy of the plugin's own interface.
    ok('the conversation tab body holds the slot and nothing else',
      chatTab.children.length === 1 && chatTab.firstElementChild === slotEl,
      'children: ' + [...chatTab.children].map((c) => c.id || c.tagName).join(','));
    // The host owns no conversation markup anywhere in the page. This is phrased
    // as a WHITELIST over the chat- class family rather than a list of the old
    // names: the slot is the only chat- thing the host may still draw, so anything
    // else in that family is a leftover, including a control this change never
    // thought to name.
    const chatClasses = new Set();
    document.querySelectorAll('[class]').forEach((el) => {
      [...el.classList].forEach((c) => {
        if (c.startsWith('chat-')) chatClasses.add(c);
      });
    });
    ok('the host page draws no conversation chrome of its own',
      [...chatClasses].every((c) => c === 'chat-slot' || c === 'chat-slot-empty'),
      'unexpected chat- classes: ' + [...chatClasses].join(','));
    // The plugin owns the conversation's data path, so the host must have no
    // binding that drives one. Asserted as a whitelist of the names that may end
    // in "Chat" plus a ban on the whole model-settings family, which is stronger
    // than naming the removed ones: a binding that was merely left behind in Go
    // under a name nobody listed here still fails.
    const allowedChat = ['OpenChat', 'OpenChatPluginSurface', 'OpenTodoPanelFromChat'];
    const chatBindings = window.__methods.filter((m) => /Chat/.test(m));
    ok('the host exports no conversation bindings',
      chatBindings.every((m) => allowedChat.includes(m)) &&
        !window.__methods.some((m) => /LLM/.test(m)),
      'unexpected chat bindings: ' +
        chatBindings.filter((m) => !allowedChat.includes(m)).join(','));
    // The 待办 jump is the ONE conversation action the host still owes the plugin:
    // a plugin cannot switch the host's tab, so it asks. The binding is asserted
    // by name because that is the contract the plugin's own interface codes to.
    ok('the 待办 jump is still available to the conversation plugin',
      window.__methods.includes('OpenTodoPanelFromChat'),
      'OpenTodoPanelFromChat is missing from the export set');

    // --- 5. (deleted) the settings entry point -------------------------------
    //
    // This drove the 设置 tab's 「打开插件市场」 button. The button and the tab are gone
    // (openspec/changes/ui-rebuild 阶段 3): launching a program is a tray menu item now, and
    // a page harness cannot click a tray menu. The guarantee moved one layer down and is
    // checked by .dev/check-frontend.cjs ("the pet still offers a way into the market, from
    // its tray menu"), which pins the menu item AND the switch case that handles it.

    // --- 6. the panel box comes only from the layout -------------------------
    //
    // Switching tabs must not move the panel: its rectangle comes from the layout
    // Go reports, and the native window region is cut to that same rectangle. A tab
    // that shifted it would put the drawn panel and the cropped region out of step,
    // which shows up as a strip of desktop along one edge.
    //
    // The document's own scroll is pinned to zero first. Focusing a control makes
    // the browser scroll its nearest scrollable ancestor to reveal it, and this page
    // is overflow:hidden rather than unscrollable - so a focus from an earlier
    // scenario can leave the document scrolled by a few pixels. That is a harness
    // leftover, not the panel moving, and it would otherwise be measured here as a
    // failure. The scroll offsets are reported on failure so a REAL shift is still
    // distinguishable from one.
    const scrollOffsets = () => 'docScroll=' + document.documentElement.scrollTop +
      '/' + document.documentElement.scrollLeft + ' bodyScroll=' +
      document.body.scrollTop + '/' + document.body.scrollLeft;
    document.documentElement.scrollTop = 0;
    document.documentElement.scrollLeft = 0;
    document.body.scrollTop = 0;
    document.body.scrollLeft = 0;
    await sleep(30);
    // Measured with offsetTop/offsetLeft, NOT getBoundingClientRect, and the
    // difference is the whole reliability of this check. The property under test is
    // "the panel's position comes from the layout and not from its contents" - a
    // LAYOUT fact, which offset* reports directly. getBoundingClientRect reports the
    // layout position MINUS the document's scroll, and this page is overflow:hidden
    // rather than unscrollable, so a focus from any earlier scenario can leave the
    // document scrolled by a few pixels and shift the rect without the panel having
    // moved at all. That is a harness artefact, and it produced exactly one
    // intermittent failure (top: 4 -> 0 with both scroll offsets already zero,
    // i.e. the scroll changed BETWEEN the two reads because the tab click moved
    // focus). Comparing offset* asks the question the check is actually about.
    const boxOf = (el) => ({ top: el.offsetTop, left: el.offsetLeft, w: el.offsetWidth, h: el.offsetHeight });
    const boxBefore = boxOf(panelEl);
    document.querySelector('.tab[data-tab="todos"]').dispatchEvent(new Event('click', { bubbles: true }));
    await sleep(30);
    const boxAfter = boxOf(panelEl);
    ok('the panel box is identical after switching tabs',
      boxBefore.w === boxAfter.w && boxBefore.h === boxAfter.h &&
      boxBefore.left === boxAfter.left && boxBefore.top === boxAfter.top,
      JSON.stringify(boxBefore) + ' -> ' + JSON.stringify(boxAfter) + ' ' + scrollOffsets());

    // --- 7. the layer that scrolls is inside the PANEL, not the panel itself -
    // This is the structural guarantee that a long list cannot change the panel's
    // rectangle, which is what the native window region is cut to. The
    // conversation used to be the case that proved it; now it is the todo list,
    // which is the same shape of content - an unbounded list inside a fixed box.
    const panelBoxBefore = panelEl.getBoundingClientRect();
    const listEl = document.getElementById('todo-list');
    for (let i = 0; i < 60; i++) {
      listEl.append(document.createElement('li'));
    }
    await sleep(30);
    const panelBoxTall = panelEl.getBoundingClientRect();
    ok('the panel box is unchanged by a very long list',
      panelBoxTall.width === panelBoxBefore.width && panelBoxTall.height === panelBoxBefore.height,
      JSON.stringify(panelBoxBefore) + ' -> ' + JSON.stringify(panelBoxTall));
    ok('the list scrolls internally rather than growing the panel',
      listEl.scrollHeight > listEl.clientHeight,
      'scrollHeight ' + listEl.scrollHeight + ' vs clientHeight ' + listEl.clientHeight);
    // The invariant is that the SCROLL CONTAINER's own box stays inside the panel
    // (which is what the native window region is cut to). The content inside it is
    // expected to extend past the container - that is what makes it scroll - and
    // the panel's overflow:hidden is what clips it.
    const panelRect = panelEl.getBoundingClientRect();
    const listRect = listEl.getBoundingClientRect();
    ok('the scroll container stays inside the panel box',
      listRect.bottom <= panelRect.bottom + 1 && listRect.top >= panelRect.top - 1,
      'list ' + JSON.stringify(listRect) + ' vs panel ' + JSON.stringify(panelRect));

    // --- 8. Escape unwinds the innermost thing first -------------------------
    // Inside the panel with no bubble up, that is the panel itself.
    document.querySelector('.tab[data-tab="chat"]').dispatchEvent(new Event('click', { bubbles: true }));
    await sleep(30);
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await sleep(60);
    ok('Escape closed the panel from the conversation tab', panelEl.classList.contains('hidden'), 'the panel is still up');
    ok('Escape used the panel path, not the reminder dismissal',
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
    ok('the panel has no settings tab and no shortcut switch any more',
      !document.querySelector('.tab[data-tab="settings"]') &&
        !document.querySelector('.tab[data-tab="records"]') &&
        !document.getElementById('hotkey-enabled') &&
        !document.getElementById('rem-enabled'),
      'settings UI came back into the panel');
    ok('the 记录 archive is inside the 待办 tab instead',
      document.getElementById('records-list')?.closest('#tab-todos') !== null,
      'the archive is not in the todo tab');

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
    // The slot is taken away and handed back first, because a handshake happens
    // ONCE per frame and the frame mounted back in scenario 3 has already had its.
    // Observing that same frame would read an empty log and pass for the wrong
    // reason - and it is also the only way to exercise the panel handshake at all,
    // since nothing else re-creates the slot frame.
    await resolveWith(allRows.filter((p) => p.id !== 'com.example.chat'));
    await probeReset();
    await resolveWith(allRows);
    const slotProbe = (await probeReceivedWhen(
      (e) => e.filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.chat').length >= 1,
      6000,
    )).filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.chat');
    ok('the slot frame is told it is a panel filling the chat slot',
      slotProbe.length >= 1 && slotProbe[0].surface &&
        slotProbe[0].surface.kind === 'panel' && slotProbe[0].surface.slot === 'chat',
      JSON.stringify(slotProbe.map((m) => m.surface)));

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
    // A dedicated row is used rather than reusing com.example.chat, because the slot is
    // first-declarer-wins and mutating that row would disturb every slot check above it.
    //
    // pet.surface is granted as well as events because the slot resolver requires it
    // (slotPlugin ANDs panelSlot === 'chat' with a granted pet.surface). That pairing
    // is surprising for a plugin that declares no floating rectangle, and a fixture that
    // omitted it would silently test "no slot owner" instead of the coexistence this
    // check is about.
    const bothRow = {
      id: 'com.example.both', name: '两副面孔插件', version: '1.0.0', author: 'shixiansi',
      description: '同时占槽位与常驻帧', enabled: true,
      permissions: ['events', 'pet.surface'], granted: ['events', 'pet.surface'],
      pending: [], netHosts: [],
      panel: true, panelSlot: 'chat', surface: false, background: true,
      entry: 'panel.html', dir: pluginDir('com.example.both'), error: '',
    };
    await probeReset();
    await resolveWith([bothRow]);
    const slotFrameCount = document.querySelectorAll('#chat-slot iframe').length;
    ok('one plugin can hold the slot and the headless frame at once',
      slotFrameCount === 1 && bgFrames().length === 1,
      'slot frames=' + slotFrameCount + ' background frames=' + bgFrames().length);
    const bothInits = (await probeReceivedWhen(
      (e) => e.filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.both').length >= 2,
      6000,
    )).filter((m) => m && m.type === 'init' && m.pluginId === 'com.example.both');
    const bothSlots = bothInits.map((m) => m.surface && m.surface.slot).sort();
    ok('each of the two frames is told which appearance it is',
      bothSlots.includes('chat') && bothSlots.includes('background'),
      'slots=' + JSON.stringify(bothSlots));

    // Restored once more: the checks below expect the normal row set.
    await resolveWith(allRows);

    // (d) The request round trip, through the SLOT frame.
    //
    // Every capability a plugin has goes through ONE gated binding, so this is
    // the whole plugin-to-host channel. The probe is told to send the request
    // over the wire it really uses - the host posts a command in, the probe posts
    // a protocol request back - so what is exercised is the assembled protocol
    // rather than a shortcut around it.
    const slotFrame = document.querySelector('#chat-slot iframe');
    ok('the slot has a frame to talk to', !!slotFrame, 'no iframe in #chat-slot');
    const responseRelays = (entries) => entries.filter((m) => m && m.type === 'response');
    // Drives one frame over the wire the plugin really uses: the host posts a
    // command in, the probe posts a protocol request back. The frame is a
    // parameter because the refusal below has to be exercised on the plugin the
    // host words its refusal for.
    const sendFromFrame = async (cmd, frame) => {
      if (!frame || !frame.contentWindow) return [];
      const before = responseRelays(await probeReceived()).length;
      frame.contentWindow.postMessage(cmd, '*');
      const after = await probeReceivedWhen((e) => responseRelays(e).length > before, 4000);
      return responseRelays(after).slice(before);
    };
    const sendFromProbe = (cmd) => sendFromFrame(cmd, slotFrame);

    const dispatchedBefore = window.__pluginRequests.length;
    const responses = await sendFromProbe({ __cmd: 'request', id: 'rq1', method: 'todos.list', payload: {} });
    const dispatched = window.__pluginRequests.slice(dispatchedBefore);
    ok('a plugin request reaches the single gated dispatcher',
      dispatched.length === 1 && dispatched[0].id === 'com.example.chat' &&
        dispatched[0].method === 'todos.list',
      'dispatched: ' + JSON.stringify(dispatched));
    ok('the dispatcher answer is returned to the plugin as a response',
      responses.length === 1 && responses[0].id === 'rq1' && responses[0].ok === true,
      'responses: ' + JSON.stringify(responses));
    ok('the response carries the value the binding returned',
      responses.length === 1 && responses[0].result && responses[0].result.method === 'todos.list',
      'result: ' + JSON.stringify(responses[0] && responses[0].result));

    // The failure path passes the host's own words through, so the plugin can
    // show the reason rather than a generic failure. The capability exercised is
    // plugin.config - the tenth permission, and the one whose refusal wording is
    // new with this change - because the plugin's own settings screen is where a
    // user is most likely to meet it.
    //
    // The refusal is asserted on the FLOATING surface's plugin, not the slot's:
    // the two frames are separate documents with separate PluginFrames, so a
    // message that arrived at the wrong one would otherwise still look like a
    // pass. Driving the beta frame is what proves the refusal travels back down
    // the channel the request came up.
    const surfaceFrame = surfaceMount.querySelector('iframe');
    ok('the surface has a frame to talk to', !!surfaceFrame, 'no iframe in #plugin-surface');
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