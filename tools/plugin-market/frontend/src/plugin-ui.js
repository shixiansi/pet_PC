/**
 * Hosts a plugin's OWN interface inside the market window.
 *
 * WHY THIS EXISTS
 *
 * A plugin can ship a workspace that does not fit the pet's panel. The pet's panel
 * is a FIXED 380x520 rectangle cropped out of a window that never resizes, so a
 * plugin needing a character list beside five lines of takes cannot have one there.
 * This window is 980x700 and resizable, so the same workspace fits without being
 * redesigned smaller.
 *
 * That is the whole reason the voice plugin's workshop is hosted here instead of in
 * a panel tab, and it is why the standalone voice studio could be retired without
 * losing the work surface it provided.
 *
 * WHAT IS IDENTICAL TO THE PET'S HOST
 *
 * The sandbox, the asset URL space, the bridge protocol and the gate. A plugin
 * cannot tell which program mounted it except by which capabilities answer:
 *
 *   - `sandbox="allow-scripts"` alone. Adding allow-same-origin would hand every
 *     installed plugin this page's DOM, which is the one thing the sandbox exists
 *     to prevent.
 *   - the frame loads `/plugin-asset/<id>/<entry>`, served by the shared handler.
 *   - `request` messages go to Market.PluginRequest, which is the shared
 *     dispatcher with THIS program's capability table.
 *
 * WHAT DIFFERS, AND WHY A REFUSAL IS NORMAL
 *
 * This program has the plugin's files, its private directory, its settings and the
 * network. It has no todo list, no pomodoro, no pet window and no reminder
 * scheduler, so `todos.list`, `timer.state`, `bubble.say`, `surface.show` and
 * `events.subscribe` are refused. That is an honest answer about where the plugin
 * is running, not a failure - and it is why a plugin that wants events must
 * subscribe from the frame the PET mounts, which is what the voice plugin's
 * `background` declaration is for.
 */

import * as Market from '../wailsjs/go/main/Market';

/** The protocol version, matching internal/plugins.BridgeJS. */
const V = 1;

/**
 * The plugin interface currently mounted, or null.
 *
 * One at a time: the detail pane shows one plugin, so a second frame would be a
 * document nobody is looking at, still running and still holding permissions.
 */
let current = null;
/** Pending requests, so a response can find its caller. */
let pending = new Map();
let nextId = 1;

/**
 * The colour scheme this window is currently drawing.
 *
 * Read from the document rather than passed in, so a frame mounted at any moment
 * (boot, a plugin switch, a re-render) picks up whatever scheme is in force without
 * a second source of truth. `main.js` owns the attribute; this only reads it.
 */
function windowTheme() {
  return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
}

/**
 * Tells a mounted plugin that the window switched schemes.
 *
 * The HOST owns the scheme, and a plugin document lives in a sandboxed frame, so it
 * cannot see this page's CSS and has to be told. Two channels, on purpose:
 *
 *   - `?theme=` on the frame URL, so a plugin can apply the right one BEFORE its
 *     first paint and never flashes the wrong scheme;
 *   - this message, for a switch that happens while the frame is already running -
 *     remounting instead would throw away a half-typed form or a scroll position.
 *
 * A plugin that ignores both keeps its own colours, and that is its business: this
 * is a notification, not a stylesheet injection.
 */
export function setFrameTheme(theme) {
  const name = theme === 'light' ? 'light' : 'dark';
  if (!current) return;
  current.theme = name;
  current.frame.style.colorScheme = name;
  if (!current.initialised) return; // `init` carries it anyway
  post({ type: 'theme', theme: name });
}

/** Wire the single window-level listener once. */
let listening = false;
function listen() {
  if (listening) return;
  listening = true;
  window.addEventListener('message', (event) => {
    const frame = current && current.frame;
    if (!frame || event.source !== frame.contentWindow) return;
    const data = event.data;
    if (!data || data.v !== V) return;
    if (data.type === 'request') onRequest(data);
  });
}

/**
 * Answers one plugin request through the shared gate.
 *
 * The reply is posted back into the frame with the id the plugin chose, which is
 * how the bridge matches it to the caller's promise. A refusal is sent as
 * `{ok:false, error}` rather than swallowed: the plugin's own UI decides whether a
 * missing capability is worth telling the user about.
 */
function onRequest(data) {
  const frame = current && current.frame;
  if (!frame) return;
  const id = data.id;
  const method = data.method;
  if (typeof id !== 'string' || typeof method !== 'string') return;
  const payload = JSON.stringify(data.payload ?? {});

  Market.PluginRequest(frame.dataset.pluginId, method, payload)
    .then((result) => {
      // The binding returns the method's JSON as a STRING; the bridge expects the
      // value itself, so it is parsed back. A method that returns nothing yields
      // an empty string, which is not valid JSON - hence the guard rather than a
      // bare parse that would reject on a successful no-op.
      let value = null;
      if (result !== '' && result != null) {
        try {
          value = JSON.parse(result);
        } catch {
          value = result;
        }
      }
      post({ type: 'response', id, ok: true, result: value });
    })
    .catch((err) => {
      post({ type: 'response', id, ok: false, error: String(err && err.message ? err.message : err) });
    });
}

function post(message) {
  const frame = current && current.frame;
  if (!frame || !frame.contentWindow) return;
  message.v = V;
  frame.contentWindow.postMessage(message, '*');
}

/** Sends one event into the mounted interface, if it has completed its handshake. */
function sendEvent(name, payload) {
  if (!current || !current.initialised) return;
  post({ type: 'event', name, payload });
}

/**
 * Mounts a plugin's interface, or removes it.
 *
 * `inst` is the installed row (which carries the entry and the permissions), or
 * null to clear. Nothing is mounted for a plugin that is not installed: an
 * interface for a plugin whose files may be gone would be a frame that renders an
 * error page, which reads as a broken plugin rather than an absent one.
 */
export function mountPluginUI(inst, els) {
  listen();
  const mount = els.pluginUIFrame;
  const section = els.pluginUI;
  if (!mount || !section) return;

  // Reused while it is already the right plugin at the right VERSION.
  //
  // The version belongs in this test for the same reason it is in the pet's slot
  // resolution: an update keeps the id and replaces the files behind it, so an
  // id-only comparison leaves the PREVIOUS version's document running and the user
  // keeps seeing the interface they just updated away from.
  const id = inst && inst.Manifest ? inst.ID : '';
  const version = inst && inst.Manifest ? inst.Manifest.version : '';
  const entry = inst && inst.Manifest ? inst.Manifest.entry : '';
  if (!id || !entry) {
    clearPluginUI(els);
    return;
  }
  if (current && current.pluginId === id && current.version === version) return;

  clearPluginUI(els);

  const frame = document.createElement('iframe');
  frame.className = 'plugin-frame';
  // Exactly allow-scripts: no allow-same-origin, so the document stays an opaque
  // origin and cannot reach this page's DOM.
  frame.setAttribute('sandbox', 'allow-scripts');
  // `?theme=` is how the plugin knows which scheme to paint on its FIRST frame; the
  // `theme` message below covers a switch that happens later. See setFrameTheme.
  frame.setAttribute('src', `/plugin-asset/${id}/${entry}?theme=${windowTheme()}`);
  frame.dataset.pluginId = id;
  if (version) frame.dataset.pluginVersion = version;
  // Native widgets inside the document (scrollbars, carets, a <select> popup) would
  // otherwise keep the platform's dark chrome inside a light window.
  frame.style.colorScheme = windowTheme();

  current = {
    pluginId: id,
    version,
    frame,
    theme: windowTheme(),
    // The handshake has not happened yet, so no event may be sent in: the plugin
    // does not know its permissions until it has received its one `init`.
    initialised: false,
  };
  pending = new Map();

  mount.replaceChildren(frame);
  section.classList.remove('hidden');

  // The plugin's own script announces itself with `ready`; the answer is exactly
  // one `init` carrying what the user granted. It is sent from here rather than
  // from Go because the grant list is already in the row this function was handed.
  const onReady = (event) => {
    if (!current || event.source !== frame.contentWindow) return;
    const data = event.data;
    if (!data || data.v !== V || data.type !== 'ready') return;
    if (current.initialised) return;
    current.initialised = true;
    window.removeEventListener('message', onReady);
    post({
      type: 'init',
      pluginId: id,
      permissions: inst.Manifest.permissions || [],
      // `Granted`, capital G - the Go field name, which is what the generated model
      // carries. It was `inst.granted` here, and the mismatch was SILENT: the property
      // does not exist, so this evaluated to `undefined`, the `|| []` turned it into an
      // empty list, and the plugin was told the user had granted it NOTHING.
      //
      // The symptom was three dead switches and an empty character column: the plugin
      // gates its controls on the granted set (correctly - it must not offer a control
      // it cannot use), so an empty grant list makes every control read-only while the
      // interface still renders perfectly. Nothing errored, which is why the lowercase
      // name survived review.
      //
      // The other fields above are NOT wrong, and that is worth stating so a later fix
      // does not "correct" them: `ID`, `Manifest`, `.version`, `.permissions` and
      // `.entry` are all spelled that way on their own models.
      granted: inst.Granted || [],
      // The window's colour scheme. The same value was on the frame URL for the first
      // paint; this is the one a plugin should trust, because a switch updates it.
      theme: current ? current.theme : windowTheme(),
      // The slot name is how a plugin with several appearances tells them apart.
      // The pet mounts the same document as 'background' or 'chat'; here it is the
      // workshop, so it is named as such rather than inheriting a slot the pet owns.
      surface: { kind: 'panel', slot: 'workshop' },
    });
  };
  window.addEventListener('message', onReady);
}

/** Removes the mounted interface, if any. */
export function clearPluginUI(els) {
  if (current && current.frame) current.frame.remove();
  current = null;
  pending = new Map();
  if (els && els.pluginUIFrame) els.pluginUIFrame.replaceChildren();
  if (els && els.pluginUI) els.pluginUI.classList.add('hidden');
}

/** The plugin id whose interface is mounted, or ''. For tests and diagnostics. */
export function mountedPluginId() {
  return current ? current.pluginId : '';
}

export { sendEvent };
