/**
 * Loads the REAL page into jsdom and returns handles for asserting on it.
 *
 * The app is one module that grabs its elements at import time and then wires
 * itself up, so a test cannot simply import it: the markup has to be in the
 * document FIRST, and main.js has to be pulled in afterwards. That is why the
 * import here is dynamic - a static one would be hoisted above the DOM setup.
 *
 * The markup is the shipping index.html rather than a copy, so an id the app
 * looks for but that the page does not have fails here instead of in
 * .dev/check-frontend.cjs (which checks the same thing statically).
 *
 * The bridge below is the entire surface the frontend talks to: `window.go.main.App`
 * for the bindings and `window.runtime` for the events. It records every call, so
 * the assertions can be about what the app ASKED THE BACKEND TO DO, which is the
 * half of the behaviour a DOM-only test would miss.
 */

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');

/**
 * Builds the fake backend and returns the call log.
 *
 * `handlers` lets a test answer a binding with a specific payload (a Layout, a
 * State); everything else resolves to null, which is what the real bindings do
 * for the calls whose result the frontend ignores.
 */
function installBridge(handlers = {}) {
  const calls = [];
  const bind = (name) => (...args) => {
    calls.push({ name, args });
    const handler = handlers[name];
    try {
      return Promise.resolve(handler ? handler(...args) : null);
    } catch (err) {
      return Promise.reject(err);
    }
  };

  // Every binding the app may call. A missing one would make the app's own
  // `.catch(fail)` fire, which surfaces as an unrelated failure further on; a
  // surplus one would accept a call the real app cannot make. The list is
  // therefore the exact export set of frontend/wailsjs/go/main/App.js, and
  // .dev/run-frontend-harness.cjs carries the same list for the live-page
  // harness - T113 compares the two as sets.
  const methods = [
    'ChoosePetImage', 'ClosePluginSurface',
    'DebugLog', 'DismissBubble', 'DragTo',
    'EndDrag', 'GetLayout', 'GetState', 'HidePet',
    'HidePetForUser', 'OpenChatPluginSurface', 'OpenPetMenu', 'OpenPluginMarket',
    'OpenPluginSurface', 'OpenTodoFromBubble', 'SetPluginSurfaceBlocks',
    'PetImageURL', 'PluginMarketCandidates',
    'PluginMarketPath', 'PluginRequest', 'PluginsInstalled', 'Quit', 'Ready', 'RemindNow', 'ResetPetImage', 'ResetToDefaultPosition', 'SaveHotkeySettings', 'SaveReminderSettings', 'SetAutostart', 'SetBubbleHover',
    'SetReminderEnabled', 'SetScale',
    'SetSnapToEdge', 'ShowPet', 'ShowPetForUser', 'SnapNow', 'StartDrag', 
    'WindowReady',
  ];
  const app = {};
  for (const m of methods) app[m] = bind(m);
  window.go = { main: { App: app } };

  const listeners = {};
  const subscribe = (name, cb) => {
    (listeners[name] ||= []).push(cb);
    return () => {
      listeners[name] = (listeners[name] || []).filter((f) => f !== cb);
    };
  };
  window.runtime = {
    EventsOn: subscribe,
    EventsOnMultiple: (name, cb) => subscribe(name, cb),
    EventsOnce(name, cb) {
      const off = subscribe(name, (...a) => { off(); cb(...a); });
      return off;
    },
    EventsOff(name) { delete listeners[name]; },
    EventsOffAll() { for (const k of Object.keys(listeners)) delete listeners[k]; },
    EventsEmit(name, ...data) { for (const cb of listeners[name] || []) cb(...data); },
    WindowShow() {}, WindowHide() {}, WindowSetTitle() {},
    LogPrint() {}, LogTrace() {}, LogDebug() {}, LogInfo() {},
    LogWarning() {}, LogError() {}, LogFatal() {},
  };

  return { calls, emit: (name, ...data) => window.runtime.EventsEmit(name, ...data) };
}

/**
 * Mounts the shipping page and imports the app against it.
 *
 * Returns the element handles, the fake backend's call log, an `emit` used to
 * push a backend event the way Go would, and the `handlers` map itself - which
 * is returned by reference so a test can swap a binding's behaviour partway
 * through (the bridge reads it at call time), which is how a failure path is
 * driven without remounting the page.
 */
export async function mountApp({ handlers = {}, state = null } = {}) {
  const html = readFileSync(path.join(root, 'index.html'), 'utf8');
  const body = html.slice(html.indexOf('<body>') + '<body>'.length, html.indexOf('</body>'));
  // The module script is dropped: the app is imported below instead, so it runs
  // against this document rather than trying to load from a dev server.
  document.body.innerHTML = body.replace(/<script[\s\S]*?<\/script>/g, '');

  const bridgeHandlers = {
    // A test almost always wants the app to see a real snapshot; without one the
    // renderers bail out early and every assertion would be vacuous.
    GetState: () => state || baseState(),
    ...handlers,
  };
  const { calls, emit } = installBridge(bridgeHandlers);

  // The app waits for animation frames before revealing the window, and jsdom
  // has no compositor. A timer is a faithful stand-in for the purpose here,
  // which is behavioural rather than visual.
  window.requestAnimationFrame = (cb) => setTimeout(() => cb(Date.now()), 0);
  window.cancelAnimationFrame = (id) => clearTimeout(id);

  await import('./main.js');
  // boot() is async and kicks off a refresh; one macrotask is enough for the
  // fake bindings (all already-resolved promises) to settle.
  await new Promise((r) => setTimeout(r, 0));

  return { calls, emit, handlers: bridgeHandlers, html: document.body.innerHTML };
}

/**
 * The layout snapshot the fake backend answers GetLayout with.
 *
 * It exists because the app DOES reach the layout path in tests: `applyLayout`
 * returns early on a null layout, and several host behaviours hang off it - the
 * panel's visibility, the plugin surface's geometry, and the watcher that keeps the
 * 对话 tab in step with the installed plugin. With the default `null` those were all
 * silently unreachable, so a test that meant to exercise them would pass while
 * testing nothing.
 *
 * The numbers mirror the real geometry: a 540x520 window with a 150x200 pet, which
 * is the shape the Go-side surface tests and the pixel gates use.
 */
export function baseLayout(over = {}) {
  return {
    petX: 0, petY: 160, petW: 150, petH: 200,
    panelX: 160, panelY: 0, panelW: 380, panelH: 520,
    panelOpen: false,
    bubble: null,
    pluginSurface: null,
    ...over,
  };
}

/** The state snapshot the fake backend answers GetState with. */
export function baseState(over = {}) {  return {
    config: {
      petScale: 1, snapToEdge: true, edgeMargin: 8,
      // No pomodoro* keys: the timer, its phase lengths and its "sound" switch belong
      // to the pomodoro plugin's own config now, and the pet's GetState does not carry
      // them. A fixture that still sent them would let a renderer reading
      // config.focusMinutes pass in jsdom while the real snapshot has stopped sending it.
      reminderEnabled: false, reminderOnStartup: false, reminderIntervalMin: 30,
      reminderJitterMaxMin: 3, reminderFrom: '09:00', reminderTo: '22:00',
      reminderMaxItems: 3, reminderAutoHideSec: 12, remindWhenEmpty: false,
      // No `llmEnabled` and no `llm` section below: the AI configuration moved to
      // the chat plugin, so the pet's state does not carry it any more. Leaving
      // it here would let a renderer that still reads it pass in jsdom while the
      // real GetState has already stopped sending it.
      ...(over.config || {}),
    },
    todos: [], stats: { total: 0, open: 0, overdue: 0, dueSoon: 0 },
    petImageUrl: '', hasCustomImage: false, dataDir: 'C:\\data', autostart: false,
    now: new Date().toISOString(),
    // No pomodoro / records / recordTotals / voice block: State carries none of them
    // (app.go's State comment says why). The timer's phase, its archive and its
    // counters are the pomodoro plugin's, and speech is the voice plugin's.
    hotkeys: { enabled: false, show: '', hide: '', active: false, note: '' },
    petVisible: true,
  };
}

/**
 * A Layout as Go hands it over, with the bubble occupying the slot beside the pet.
 *
 * `pluginSurface` replaces the old `live` member: the host's own conversation
 * overlay was deleted, and the floating surface a plugin declares is what takes
 * the slot now. It is `null` by default, which is the "nothing floating" case.
 */
export function layout({ panelOpen = false, bubble = null, pluginSurface = null } = {}) {
  return {
    panelOpen, windowW: 540, windowH: 520,
    petX: 0, petY: 160, petW: 150, petH: 200,
    panelX: panelOpen ? 160 : 0, panelY: 0, panelW: panelOpen ? 380 : 0, panelH: 520,
    panelOnRight: true, scale: 1, bubble, pluginSurface,
  };
}

/**
 * A bubble payload with the geometry Go gives that kind.
 *
 * TWO kinds left (app_bubble.go): the reminder the host raises itself, and the pet's
 * own menu. The picker, the summary prompt and the plan prompt were the host timer's
 * questions and went with it; asking for one of them here would build a card the
 * frontend no longer has a branch for, and the test would pass while asserting on a
 * blank rectangle.
 */
export function bubbleView(kind, over = {}) {
  const heights = { reminder: 140, menu: 226 };
  return {
    kind, title: '', x: 168, y: 180, w: 300, h: heights[kind] ?? 140,
    onRight: true, ...over,
  };
}

/** The calls of one binding, in order. */
export function callsTo(calls, name) {
  return calls.filter((c) => c.name === name);
}
