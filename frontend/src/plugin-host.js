/**
 * Plugin host: the sandboxed iframes a plugin's interface runs in.
 *
 * This is the boundary between the pet and third-party code, so the rules here
 * are not stylistic:
 *
 *  - Each plugin interface is an `<iframe sandbox="allow-scripts">`. The
 *    sandbox attribute deliberately does NOT include `allow-same-origin`, which
 *    leaves the iframe's document on an OPAQUE origin: it cannot reach the
 *    parent document, its storage, or any cookie, and `document.domain` cannot
 *    be used to climb back out. `allow-scripts` alone is what lets the plugin
 *    run at all. Popups, top-level navigation and forms are refused for the same
 *    reason - none of them is a capability a plugin needs, and each is a way out
 *    of the frame.
 *  - The only channel is `postMessage`, and the host answers exactly one
 *    message shape: a `request` naming a method. Every method goes through
 *    `App.PluginRequest`, which is where the permission check lives, so this
 *    file never decides what a plugin may do.
 *  - Messages are accepted only from the `contentWindow` of an iframe this file
 *    created. Without that check any page in the webview could impersonate a
 *    plugin.
 *  - The plugin's frames are PLACED at the rectangles Go computed. Nothing here
 *    derives a position or a size: those rectangles are the same ones the window
 *    is cropped to, and a second calculation could only disagree with the crop.
 *
 * Two kinds of surface share this machinery. A PANEL view fills one of the
 * panel's named slots - today only 'chat', the 对话 tab, whose body is a mount
 * point rather than a set of controls - which adds no window geometry at all. A
 * FLOATING surface places one iframe per declared rectangle.
 */

import * as App from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

/** Protocol version. Both sides refuse a message that does not carry it. */
const V = 1;

/**
 * The one panel slot a plugin can claim today: the 对话 tab's body.
 *
 * It is exported because the HOST has to agree with this file about the name
 * twice - it looks the slot up in the installed list when it resolves which
 * plugin owns the slot, and it mounts that plugin here - and a string written
 * out in two files is a string that can drift.
 */


/**
 * The scheme the page is currently painted in.
 *
 * Read from `<html data-theme>` rather than stored: that attribute is what style.css keys both
 * palettes off, and applyLayout writes it from Layout.Theme - so there is exactly one answer to
 * "which theme is it", and the frames are told that one.
 */
function currentTheme() {
  return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
}

/**
 * The palette pack the page is painted in, or "" when none was chosen.
 *
 * Same rule as currentTheme: read the attribute, never a copy. main.js writes it from
 * `Layout.ThemePack`, and the host splices the pack's tokens into each document it serves - so this
 * is the one place the frames learn which pack to be drawn in.
 */
function currentPack() {
  return document.documentElement.dataset.themePack || '';
}

/** The message the plugin sends to announce itself, and the host's answer. */
const TYPE_READY = 'ready';
/**
 * The host telling a mounted frame that the interface's colour scheme changed.
 *
 * A plugin document cannot see the host's CSS, so the scheme reaches it three ways, and the
 * manual (docs/plugin-dev.md §几何与主题) names all three: ?theme= on the frame URL (so the
 * first paint is already right - no flash), init.theme, and this message, for a frame that
 * outlives a flip of the switch. A plugin that only reads the first is a plugin that looks
 * wrong from the second click onward.
 */
const TYPE_THEME = 'theme';
const TYPE_INIT = 'init';
const TYPE_REQUEST = 'request';
const TYPE_RESPONSE = 'response';
const TYPE_EVENT = 'event';
/**
 * The host telling a MOUNTED frame that its rectangle changed.
 *
 * `init` is sent once, and a surface frame outlives many geometry passes: the pet is resized (the
 * user's 桌宠大小), the answer box is asked to grow, the window's side flips. A plugin that keeps
 * using the rectangle from `init` ends up drawing a tail at the pet's old centre, and it cannot
 * know how much room it has above the pet - which is what decides whether its own blocks still fit.
 *
 * So this message carries the new rectangle (including `room`) to every frame whose rect changed.
 */
const TYPE_SURFACE = 'surface';
/**
 * One plugin asking another a question, and the answer coming back.
 *
 * This is the second half of `plugin.call`, and it lives HERE because only this file holds
 * the frames: Wails lets Go emit an event into a page, but there is no way for Go to ask a
 * document something and wait for the reply. So Go authorises (it answers `plugin.call`
 * with an envelope - see internal/plugins/call.go) and this file delivers, waits, settles.
 *
 * The three numbers below are limits Go cannot enforce, because Go never sees the chain:
 *
 *  - CALL_TIMEOUT_MS: the callee may itself be waiting on an `llm.chat`, so this is much
 *    longer than a capability call. A timeout is NOT a permission refusal - nothing was
 *    declined, the answer simply did not arrive - which is why the page may decide it.
 *  - CALL_MAX_DEPTH: A asks B asks C asks D and stops. Without a cap, two plugins asking
 *    each other is an endless loop whose only symptom is that both stop answering.
 *  - CALL_MAX_RESULT: an answer is copied through two documents. REFUSED rather than
 *    truncated, because half a JSON document is a parse error at the far end that says
 *    nothing about the size limit.
 */
const TYPE_CALL = 'call';
const TYPE_CALL_RESULT = 'call-result';
const CALL_TIMEOUT_MS = 30_000;
const CALL_MAX_DEPTH = 3;
const CALL_MAX_RESULT = 64 << 10;

/**
 * A granted set as one comparable string.
 *
 * Sorted and joined so the value answers "is this the same set" rather than "did the
 * array happen to be built in the same order": the two lists reaching this file come from
 * different producers (Go's plugin index for one, the same index re-read 1.5 s later for
 * the other), and an order-sensitive comparison would remount a frame for nothing.
 *
 * Exported for the test that pins the remount-on-grant-change behaviour; it is the one
 * piece of that behaviour that can be asserted without a live iframe.
 */
export function grantedKey(list) {
  return (Array.isArray(list) ? [...list] : []).slice().sort().join(',');
}

/**
 * One plugin's sandboxed interface.
 *
 * `kind` is 'panel', 'surface' or 'body'. A panel view fills its slot; a surface view is
 * placed at a rectangle; a body view is placed at the PET's rectangle, which the host owns
 * and the user sizes. They differ only in where the iframe goes and what the handshake says
 * about it - the transport, the sandbox and the permission check are identical, which is
 * why they share this class.
 *
 * A panel view also carries `slot`, the name of the mount point it fills. The
 * kind alone cannot say where the interface went: the panel has exactly one
 * plugin slot (the 对话 tab), and a plugin told only "you are a panel" could not
 * tell that slot from any other the host may add later.
 */
class PluginFrame {
  /**
   * @param {object} opts
   * @param {string} opts.pluginId
   * @param {string} [opts.version] the installed version the frame was built from
   * @param {string} opts.entry   relative HTML path inside the plugin
   * @param {'panel'|'surface'|'body'} opts.kind
   * @param {string} [opts.slot]  the panel slot name, for a panel view
   * @param {string[]} opts.permissions everything the manifest declares
   * @param {string[]} opts.granted     what the user allowed
   * @param {number} [opts.rectIndex]
   * @param {number} [opts.rectCount]
   * @param {object|null} [opts.rect]
   * @param {function} opts.onRequest   (method, payload) => Promise<any>
   * @param {object|null} [opts.host]   `{program, methods}` for init.host
   */
  constructor(opts) {
    this.pluginId = opts.pluginId;
    // The version this frame's document was loaded from.
    //
    // It exists so the host can tell "the same plugin, still installed" from "the
    // same plugin, UPDATED" - the id is identical in both cases, and only the
    // version distinguishes them. Without it a plugin update left the old document
    // running: the frame was reused because its id matched, so the user saw the
    // previous version's interface until they restarted the pet, which is exactly
    // the opposite of what an update in the market appears to promise.
    this.version = opts.version || '';
    this.entry = opts.entry;
    this.kind = opts.kind;
    this.slot = opts.slot || '';
    this.permissions = opts.permissions || [];
    this.granted = opts.granted || [];
    this.rectIndex = opts.rectIndex ?? 0;
    this.rectCount = opts.rectCount ?? 1;
    this.rect = opts.rect || null;
    this.onRequest = opts.onRequest;
    // What this program serves, as `{program, methods}` - see initMessage. Null until the
    // startup read lands, and null is a legal value: `init.host` is optional.
    this.host = opts.host || null;

    /** True once this frame has been answered with its one init. */
    this.initialised = false;
    this.el = this.createElement();
  }

  /**
   * Builds the iframe.
   *
   * `sandbox` is exactly 'allow-scripts'. Adding `allow-same-origin` here would
   * hand every installed plugin the parent page's DOM, which is the one thing
   * the sandbox exists to prevent.
   */
  createElement() {
    const frame = document.createElement('iframe');
    frame.className = 'plugin-frame';
    frame.setAttribute('sandbox', 'allow-scripts');
    // The plugin's files come from the host's own asset route. It is a same-origin
    // URL, so the plugin's own script and stylesheet load (subresource fetches are
    // not origin-checked the way DOM access is), while the DOCUMENT stays opaque
    // because allow-same-origin is absent.
    // `?theme=` is part of the contract, not decoration: the plugin puts it on `<html
    // data-theme>` before its first paint, so a light window never shows a dark frame first.
    // The asset route keys on `r.URL.Path` (main.go), so the query costs nothing.
    //
    // `?pack=` rides along for the same reason and is NOT part of the plugin's contract: the host
    // splices a `<style>` into the document it serves (internal/plugins/asset.go), so a pack needs
    // nothing from the plugin author. That is the only way it can work at all - the frame is an
    // opaque origin, and the host cannot touch its DOM.
    //
    // Consequence worth knowing: a pack change reaches frames that mount AFTER it. The stylesheet is
    // baked into the served document, so an already-open panel keeps its palette until it is
    // reopened - re-creating frames to re-theme them would throw away a half-filled form.
    frame.setAttribute(
      'src',
      `/plugin-asset/${this.pluginId}/${this.entry}?theme=${currentTheme()}&pack=${currentPack()}`,
    );
    frame.dataset.pluginId = this.pluginId;
    frame.dataset.kind = this.kind;
    // The version the document came from. It is on the element as well as on the
    // frame object so a test (and a DOM dump) can tell WHICH version is loaded
    // without reaching into the host's internals.
    if (this.version) frame.dataset.pluginVersion = this.version;
    return frame;
  }

  /** The init payload: everything the plugin needs to know about its situation. */
  initMessage() {
    const surface = { kind: this.kind };
    // `slot` travels whenever the host knows one, for both kinds: a panel view fills a
    // named slot, and a BODY frame is the one mount point whose `kind` alone does not say
    // which rectangle it is (a surface block also has kind 'surface', and a body frame must
    // be able to tell "I am the pet" from "I am a card beside it").
    if (this.slot) surface.slot = this.slot;
    // A panel view fills a named slot and has no rectangle of its own; every other kind is
    // placed at one, and these are the numbers the window region was cropped to.
    if (this.kind !== 'panel') {
      surface.rectIndex = this.rectIndex;
      surface.rectCount = this.rectCount;
      surface.rect = this.rect;
    }
    const message = {
      v: V,
      type: TYPE_INIT,
      pluginId: this.pluginId,
      permissions: this.permissions,
      granted: this.granted,
      surface,
      theme: currentTheme(),
    };
    // WHICH PROGRAM THIS IS, and which methods it answers.
    //
    // Without it a plugin can only tell the two hosts apart by `surface.slot`, and "does this
    // window serve llm.profile" becomes a matrix every plugin author re-derives by hand - the
    // shape that produced 571 `本程序不支持该方法` lines in one day's logs. The field is
    // OPTIONAL: a plugin that never looks at `host` behaves exactly as before, which is what
    // lets this ship without republishing every installed plugin.
    if (this.host) {
      message.host = { program: this.host.program, methods: [...this.host.methods] };
    }
    return message;
  }

  /**
   * Handles one message from the frame's window.
   *
   * Returns true when the message was accepted, which is what lets a test tell
   * "the plugin's ready was answered" from "some other window was ignored".
   */
  handleMessage(data) {
    if (!data || typeof data !== 'object' || data.v !== V) return false;

    if (data.type === TYPE_READY) {
      // The handshake: exactly one init per frame, sent only when the plugin
      // says it is ready. Until it arrives the plugin must not send a request -
      // it does not yet know what the user granted (design §9.2).
      if (this.initialised) return false;
      this.initialised = true;
      this.post(this.initMessage());
      return true;
    }

    if (data.type === TYPE_REQUEST) {
      this.dispatch(data);
      return true;
    }
    return false;
  }

  /**
   * Runs one plugin request.
   *
   * The method and the id must both be non-empty strings; anything else is
   * dropped WITHOUT a reply, because replying to a malformed message would be a
   * way for a plugin to probe the host. A well-formed request goes to `onRequest`,
   * which is `PluginHost.request` - the one place that talks to `App.PluginRequest`,
   * and the only place that can deliver a `plugin.call` (it needs the other frames,
   * which a single frame does not have).
   */
  dispatch(data) {
    const { id, method } = data;
    if (typeof id !== 'string' || id === '') return;
    if (typeof method !== 'string' || method === '') return;

    const payload = data.payload === undefined ? {} : data.payload;
    this.onRequest(method, payload)
      .then((value) => {
        this.post({ v: V, type: TYPE_RESPONSE, id, ok: true, result: value });
      })
      .catch((err) => {
        // The message is passed through unchanged so the plugin can show the
        // host's own words ("未获权限 llm.chat") rather than a generic failure.
        this.post({ v: V, type: TYPE_RESPONSE, id, ok: false, error: String(err?.message || err) });
      });
  }

  /** Sends one host->plugin event. */
  sendEvent(name, payload) {
    this.post({ v: V, type: TYPE_EVENT, name, payload });
  }

  /**
   * Tells this frame its rectangle changed.
   *
   * Only for a MOUNTED frame whose rect actually moved or resized: `init` already carried the
   * first one, and a message sent before the document installed its listener would be lost.
   */
  sendGeometry(rect) {
    this.post({ v: V, type: TYPE_SURFACE, rect });
  }

  /** Posts a message into the frame. */
  post(message) {
    if (!this.el.contentWindow) return;
    this.el.contentWindow.postMessage(message, '*');
  }

  /** Removes the frame. */
  destroy() {
    this.el.remove();
  }

  /**
   * Tells a MOUNTED frame that the interface's colour scheme changed.
   *
   * Only after the handshake: before it the plugin has not read anything yet, and its `init` (sent
   * on `ready`) already carries the current theme - so posting now would be a message the plugin's
   * handler has no reason to accept yet.
   */
  sendTheme(theme) {
    if (!this.initialised) return;
    this.post({ v: V, type: TYPE_THEME, theme });
  }
}

/**
 * PluginHost owns every plugin iframe on the page.
 *
 * It keeps four KINDS: the floating surface's frames (one per declared
 * rectangle), the one panel slot view, the headless background frames (one per
 * plugin that asked), and the one frame that draws the PET itself. They are
 * independent - a floating surface stays up while the body frame keeps drawing -
 * which is why they are tracked separately rather than as "the current view".
 *
 * The BACKGROUND frames are the headless ones. They exist because events are
 * delivered only to a frame the host has mounted, and some plugins must act when
 * the user has no interface open at all: a voice plugin has to hear the pomodoro
 * end whether or not the panel is showing, a chat bridge has to keep a socket's
 * inbound frames flowing, and a polling plugin (net.fetch) has to wake up on its
 * own schedule and make a request. Each is declared (`background` in the manifest)
 * and authorised (one of `events`, `net.ws`, `net.fetch`) rather than being an
 * unconditional extra frame, so a plugin cannot run invisibly without the user
 * having agreed to it.
 */
export class PluginHost {
  constructor(els, { fail, debug }) {
    this.els = els;
    this.fail = fail;
    this.debug = debug || (() => {});

    /**
     * The floating surface's frames, keyed by the block's DECLARATION index.
     *
     * A Map, not an array, and the key is `rect.index` rather than the position in the
     * array. Two reasons, both of which were bugs first:
     *
     *   - `surface.blocks` can hide a block, and a hidden block still comes back in the
     *     list (with `hidden: true`; see PluginSurfaceRect.Hidden). With positional
     *     addressing, hiding block 0 made the block that was declared 1 look like block 0,
     *     and a frame built as `rectIndex: i` then told the surviving block it had the
     *     other block's job - the input pill rendered as a result strip and the composer
     *     disappeared.
     *   - when the SET of blocks changes the frames still have to be rebuilt, but now that
     *     is a comparison of keys instead of a count, so "one block was hidden" no longer
     *     looks like "the surface was replaced".
     */
    this.surfaceFrames = new Map();
    /** The plugin id the current surface belongs to, or ''. */
    this.surfacePluginId = '';
    /** The version the current surface frames were built from, or ''. */
    this.surfaceVersion = '';
    /**
     * The GRANTED SET the current surface frames were built from, as `grantedKey`.
     *
     * It is part of "is this the same surface" for the same reason the version is, and the
     * reason is a bug this field exists to kill: `init` carries `granted` ONCE, and a plugin
     * is taught to gate its own behaviour on it (`if (granted.has('pet.bubble')) …` - see
     * market/docs/plugin-dev.md §2). Grants are edited in the CONTROL CENTER, a different
     * process, so a frame mounted before the user ticked a box would hold that stale answer
     * for the rest of the session: the capability gate in Go re-reads plugins.json on every
     * call and would say yes, while the plugin had already decided never to ask.
     *
     * The real machine showed exactly that: weather-env's headless frame mounted while only
     * `net.fetch` was granted, the user then granted `pet.surface`, and the plugin's badge -
     * guarded by `granted.has('pet.surface')` - never once appeared, with no error anywhere.
     * The market had already got this right by putting `granted` in its iframe key
     * (cc/src/components/PluginFrame.svelte); this is the pet agreeing with it.
     */
    this.surfaceGranted = '';
    /**
     * The headless frames, keyed by plugin id.
     *
     * ONE PER PLUGIN that declared `background` and holds a permission that fills
     * a frame. It used to be a single frame with "the first qualifying plugin
     * wins", because one was all any plugin needed: the voice plugin listens for
     * the pomodoro ending. The QQ bridge is the second case and it cannot share a
     * frame with the voice plugin - they are different documents - so the limit
     * had to go. Nothing else about the mechanism changed: the frame is still
     * hidden, still sandboxed to `allow-scripts`, and still an interface the event
     * router delivers to.
     */
    this.backgroundFrames = new Map();

    /**
     * The frame that draws the PET ITSELF, or null.
     *
     * ONE, never a set: there is one pet. A manifest declares `body` and the host mounts the
     * first eligible plugin in install order (see main.js's petBodyPlugin), so two plugins
     * asking for the pet cannot end up drawing halves of it.
     *
     * It is kept apart from `surfaceFrames` because its lifetime is different: a surface comes
     * and goes with `surface.show`/`surface.hide`, while the body is mounted for as long as
     * the plugin is enabled, granted and installed - it is the pet, and a pet that disappeared
     * when a card closed would be a pet that cannot be dragged.
     */
    this.bodyFrame = null;
    /**
     * Which (plugin, version, granted-set) the body frame was built from.
     *
     * Same rule the surface and background frames follow: an update keeps the id and replaces
     * the files behind it, and `init` carries `granted` only once, so either change has to
     * rebuild the document rather than leave the old one running.
     */
    this.bodyKey = '';

    /**
     * What this program serves, as `{program, methods}`, or null before the startup read.
     *
     * Every frame gets it as `init.host` (see PluginFrame.initMessage). Null is a legal value:
     * the field is optional in the protocol, and a plugin that never reads it is unaffected.
     */
    this.host = null;

    /**
     * In-flight `plugin.call`s, keyed by the ticket Go minted.
     *
     * Each entry remembers WHICH frame was asked, because an answer is only accepted from
     * that frame's `contentWindow` - otherwise one plugin could settle another's call by
     * guessing a ticket. The entry is removed the moment it is settled (answered, timed
     * out, or its frame destroyed), so a replayed ticket finds nothing to settle.
     */
    this.pendingCalls = new Map();
    /**
     * How deep the call chain is for each plugin CURRENTLY SERVING one.
     *
     * A call from a plugin that is itself answering a call is one hop deeper. Go cannot
     * count this (it never sees the chain), and a plugin cannot be trusted to say - so the
     * count lives here, where both ends of every hop pass through.
     *
     * It is conservative rather than exact: two calls being served by the same plugin at
     * once share one counter, so the depth reads higher than any single chain. That errs
     * towards refusing a call that would have been legal, which is the right direction for
     * a limit whose purpose is stopping a loop.
     */
    this.callDepth = new Map();

    this.onMessage = this.onMessage.bind(this);
    window.addEventListener('message', this.onMessage);
    // The host tells the plugins about state changes. The event name is emitted
    // by Go as a double-quoted literal, and this subscription is in the scan
    // list the static gate checks against it.
    EventsOn('pet:plugin-event', (ev) => this.routeEvent(ev));
  }

  /**
   * True while a plugin's floating surface is on screen.
   *
   * "on screen in either slot" used to be the question, because the panel had a slot view of
   * its own. The panel is gone and the chat plugin renders itself as a floating surface, so a
   * surface is now the whole of "something is on screen".
   */
  isOpen() {
    return this.surfaceFrames.size > 0;
  }

  /**
   * Records what this program serves, for every `init` from now on.
   *
   * Called once, from the startup read (`App.PluginHostInfo`), and awaited before the frames
   * are mounted: a frame that mounts first would get no `host` at all and would fall back to
   * `surface.slot` - correct but second best. Existing frames are updated too, which closes the
   * one race that cannot be ordered away: `init` is sent when the PLUGIN says `ready`, so a
   * frame can be mounted before this lands.
   */
  setHostInfo(info) {
    if (!info || typeof info.program !== 'string' || !Array.isArray(info.methods)) {
      this.host = null;
      for (const frame of this.allFrames()) frame.host = null;
      return;
    }
    // Copied rather than referenced: the CC's bridge hands back Svelte proxies, and a proxy
    // cannot cross `postMessage` (the DataCloneError this project already paid for once).
    this.host = { program: info.program, methods: [...info.methods] };
    for (const frame of this.allFrames()) frame.host = this.host;
  }

  /** Every mounted frame, of all kinds. */
  allFrames() {
    return [
      ...this.surfaceFrames.values(),
      ...this.backgroundFrames.values(),
      ...(this.bodyFrame ? [this.bodyFrame] : []),
    ];
  }

  /**
   * Mounts, re-places or removes the frame that draws the pet.
   *
   * `plugin` is the installed row that declares `body`, or null when nobody does (or the
   * claimant is disabled or ungranted) - in which case the bundled picture is what the user
   * sees, and the mount point is hidden so nothing can occupy the pet's box.
   *
   * `rect` is the pet's box in CSS pixels relative to the window. It is NOT used for
   * positioning - the mount point is inside #pet and the frame fills it (style.css) - but it
   * is what the plugin is told, because a plugin cannot measure the host's layout and has to
   * know how big its own body is: the pet's size is the user's setting (桌宠大小), and an
   * animated pet that assumed 150x200 would draw off its own feet at 1.5x.
   *
   * Re-placed rather than rebuilt on a pure resize: `surface` carries the new rectangle to a
   * document that already handshook, which is the same mechanism a surface block uses when the
   * pet grows (openspec/changes/surface-follows-pet-size).
   */
  syncBody(plugin, rect) {
    const mount = this.els.petBody;
    if (!mount) return;
    if (!plugin || !plugin.id) {
      this.closeBody();
      return;
    }
    const key = [plugin.id, plugin.version || '', grantedKey(plugin.granted)].join('\u0001');
    if (this.bodyFrame && this.bodyKey === key) {
      this.placeBody(rect);
      return;
    }
    this.closeBody();
    const frame = new PluginFrame({
      pluginId: plugin.id,
      version: plugin.version || '',
      entry: plugin.entry || 'panel.html',
      // 'body', not 'surface': the plugin's document has to be able to tell "I am drawing the
      // pet" from "I am a card beside it", and the two have different sizes, different
      // colours behind them and different lifetime.
      kind: 'body',
      // Same slot vocabulary the plugin learns to recognise elsewhere ('chat' / 'todos' /
      // 'pomodoro' / 'background'), so "where am I" has one spelling in both directions.
      slot: 'body',
      permissions: plugin.permissions || [],
      granted: plugin.granted || [],
      rect: rect || null,
      host: this.host,
      onRequest: (m, p) => this.request(plugin.id, m, p),
    });
    this.bodyFrame = frame;
    this.bodyKey = key;
    frame.el.dataset.slot = 'body';
    mount.append(frame.el);
    mount.classList.remove('hidden');
  }

  /** Tells a mounted body frame its rectangle changed, and remembers it. */
  placeBody(rect) {
    const frame = this.bodyFrame;
    if (!frame || !rect) return;
    const moved = JSON.stringify(frame.rect) !== JSON.stringify(rect);
    frame.rect = rect;
    // Only a MOUNTED frame can hear it: before the handshake the plugin has not installed its
    // listener, and `init` (sent on `ready`) already carries the current rectangle.
    if (moved) frame.sendGeometry(rect);
  }

  /** Removes the body frame and puts the pet's own picture back. */
  closeBody() {
    const frame = this.bodyFrame;
    this.bodyFrame = null;
    this.bodyKey = '';
    if (this.els.petBody) this.els.petBody.classList.add('hidden');
    if (!frame) return;
    frame.destroy();
    // A caller waiting on this plugin through `plugin.call` has to hear "it is gone" now:
    // after the timeout it would be told "no answer", which is a different diagnosis.
    this.failCallsFor(frame, '画桌宠本体的插件已经卸下');
  }

  /**
   * Mounts the headless frames for every plugin that asked for one.
   *
   * `plugins` is the eligible list, computed by the caller from the installed rows
   * (enabled + declares `background` + still holds a permission that fills a
   * frame). Frames for plugins no longer in the list are destroyed; frames for
   * plugins that are already mounted are left alone unless their VERSION changed,
   * because an update keeps the id and replaces the files behind it.
   *
   * @param {object[]} plugins eligible rows from PluginsInstalled
   */
  syncBackground(plugins) {
    const keep = new Set();
    for (const plugin of plugins || []) {
      if (!plugin || !plugin.id) continue;
      keep.add(plugin.id);
      this.openBackground(plugin);
    }
    for (const id of [...this.backgroundFrames.keys()]) {
      if (!keep.has(id)) this.closeBackground(id);
    }
  }

  /**
   * Mounts one plugin's headless frame.
   *
   * The frame is hidden rather than merely unpainted: it must not be clickable,
   * must not take layout space, and - unlike a surface frame - must never be able
   * to swallow a click meant for the pet. `display: none` accomplishes all three,
   * and a hidden iframe keeps running, which is exactly what is wanted.
   *
   * Re-mounted when the plugin, its version, or its GRANTED SET changes. The version
   * because an update keeps the id and replaces the files behind it; the granted set
   * because `init` carries it once and a plugin is taught to gate on it (see
   * `surfaceGranted` for the real-machine bug that produced this rule).
   *
   * @param {object} plugin a row from PluginsInstalled
   */
  openBackground(plugin) {
    if (!plugin || !plugin.id) return;
    const existing = this.backgroundFrames.get(plugin.id);
    // The GRANTED set is part of "still the same frame", for the reason surfaceGranted
    // documents: `init` carries it once, and a plugin that gates on it would otherwise keep
    // obeying a set the user has since changed. Rebuilding is the only honest fix, because
    // the protocol has no "your permissions changed" message - and adding one would leave
    // every installed plugin unable to hear it.
    const granted = grantedKey(plugin.granted);
    if (existing &&
        existing.version === (plugin.version || '') &&
        existing.grantedKey === granted) {
      return;
    }
    if (existing) this.closeBackground(plugin.id);
    const mount = this.els.pluginBackground;
    if (!mount) return;
    const frame = new PluginFrame({
      pluginId: plugin.id,
      version: plugin.version || '',
      entry: plugin.entry || 'panel.html',
      kind: 'panel',
      // The slot name is how the plugin knows WHERE it landed. A background frame
      // is not a visible slot, so it gets its own name rather than pretending to
      // be the panel - a plugin that declared both would otherwise be unable to
      // tell its two frames apart.
      slot: 'background',
      permissions: plugin.permissions || [],
      granted: plugin.granted || [],
      host: this.host,
      onRequest: (m, p) => this.request(plugin.id, m, p),
    });
    frame.grantedKey = granted;
    this.backgroundFrames.set(plugin.id, frame);
    frame.el.dataset.slot = 'background';
    mount.append(frame.el);
    // The mount point STAYS hidden, and that is the point rather than an
    // oversight: a headless frame must occupy no layout space and must never be
    // clickable, and `display: none` on the container accomplishes both while the
    // iframe inside keeps running. Removing the class here - which is what the
    // visible slot does - would put a live, invisible-to-nobody plugin document on
    // screen and let it swallow clicks meant for the pet.
    mount.classList.add('hidden');
  }

  /** Removes one plugin's headless frame. */
  closeBackground(pluginID) {
    const frame = this.backgroundFrames.get(pluginID);
    if (!frame) return;
    this.backgroundFrames.delete(pluginID);
    frame.destroy();
    // Anyone waiting on an answer from this frame must hear "it went away" now rather than
    // after CALL_TIMEOUT_MS: the frame is gone, and 30 seconds of silence is a worse
    // description of what happened than the truth.
    this.failCallsFor(frame, `目标插件 ${pluginID} 的常驻帧已关闭`);
    if (this.backgroundFrames.size === 0 && this.els.pluginBackground) {
      this.els.pluginBackground.classList.add('hidden');
    }
  }

  /** Removes every headless frame. */
  closeAllBackground() {
    for (const id of [...this.backgroundFrames.keys()]) {
      this.closeBackground(id);
    }
  }

  /** The plugin ids that currently have a headless frame. */
  backgroundPluginIds() {
    return [...this.backgroundFrames.keys()];
  }

  /**
   * Accepts a message only from a frame this component created.
   *
   * `event.source` is the window that sent it, so comparing it against each
   * frame's own contentWindow is what stops any other page in the webview from
   * speaking as a plugin.
   */
  onMessage(event) {
    // Never trust the origin: the frames are opaque by design, so their origin
    // is "null" and an origin check would be both useless and wrong. Identity of
    // the window is the check that matters.
    for (const frame of this.surfaceFrames.values()) {
      if (frame.el.contentWindow === event.source) {
        if (this.handleCallResult(frame, event.data)) return;
        frame.handleMessage(event.data);
        return;
      }
    }
    for (const frame of this.backgroundFrames.values()) {
      if (frame.el.contentWindow === event.source) {
        if (this.handleCallResult(frame, event.data)) return;
        frame.handleMessage(event.data);
        return;
      }
    }
    if (this.bodyFrame && this.bodyFrame.el.contentWindow === event.source) {
      if (this.handleCallResult(this.bodyFrame, event.data)) return;
      this.bodyFrame.handleMessage(event.data);
    }
  }

  /**
   * Runs one capability call for a plugin.
   *
   * This is the ONLY caller of `App.PluginRequest` on the pet's page, and that is what
   * makes `plugin.call` possible at all: the method is authorised by Go like any other,
   * but its answer has to come from another plugin's frame - and the frames live here.
   *
   * @param {string} pluginId the calling plugin
   * @param {string} method
   * @param {any} payload
   * @returns {Promise<any>} the method's value, parsed
   */
  request(pluginId, method, payload) {
    return App.PluginRequest(pluginId, method, JSON.stringify(payload)).then((result) => {
      // The binding answers with the method's JSON-encoded value.
      let value = result;
      if (typeof result === 'string' && result !== '') {
        try {
          value = JSON.parse(result);
        } catch {
          value = result;
        }
      }
      if (method !== 'plugin.call') return value;
      return this.deliverCall(pluginId, value);
    });
  }

  /**
   * Delivers one AUTHORISED call and settles it with the callee's answer.
   *
   * By the time this runs, Go has already decided that this caller may ask this question
   * of that plugin (see internal/plugins/call.go). Everything that can still go wrong here
   * is a delivery problem - no frame, too deep, no answer - and none of it is a permission
   * decision, which is why this file is allowed to make it.
   *
   * @param {string} callerId
   * @param {object} envelope {ticket, target, method, payload} from Go
   */
  deliverCall(callerId, envelope) {
    const ticket = typeof envelope?.ticket === 'string' ? envelope.ticket : '';
    const target = typeof envelope?.target === 'string' ? envelope.target : '';
    const method = typeof envelope?.method === 'string' ? envelope.method : '';
    if (!ticket || !target || !method) {
      return Promise.reject(new Error('plugin.call 的授权信封不完整（缺少 ticket / target / method）'));
    }
    const frame = this.frameForCall(target);
    if (!frame) {
      return Promise.reject(new Error(
        `plugin.call 问不到 ${target}：它此刻没有常驻帧，也没有浮层面在显示（目标是 background 插件吗？）`));
    }
    // Depth: this hop is one more than the depth of the plugin doing the asking.
    const depth = (this.callDepth.get(callerId) || 0) + 1;
    if (depth > CALL_MAX_DEPTH) {
      return Promise.reject(new Error(
        `plugin.call 的调用链已到 ${CALL_MAX_DEPTH} 层（${callerId} → ${target}/${method}）：两个插件互相提问会在这里停下`));
    }
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pendingCalls.delete(ticket);
        this.releaseDepth(target, 1);
        reject(new Error(
          `plugin.call 等 ${target}/${method} 的回答超时（${CALL_TIMEOUT_MS / 1000} 秒）`));
      }, CALL_TIMEOUT_MS);
      this.pendingCalls.set(ticket, { frame, target, method, depth, timer, resolve, reject });
      this.callDepth.set(target, (this.callDepth.get(target) || 0) + 1);
      frame.post({
        v: V,
        type: TYPE_CALL,
        ticket,
        from: callerId,
        method,
        payload: envelope.payload === undefined ? {} : envelope.payload,
      });
    });
  }

  /**
   * The frame that answers for `targetId`, or null.
   *
   * Background frames come first because a plugin that answers questions is expected to be
   * resident - the whole point of asking another plugin is that it is there when the user
   * has no interface open. Only then the floating surface, and if that plugin declared
   * several blocks (several documents) the first one answers: there is no "which block"
   * question in the protocol, and picking one deterministically is better than asking a
   * plugin that cannot know.
   */
  frameForCall(targetId) {
    const bg = this.backgroundFrames.get(targetId);
    if (bg) return bg;
    // Then the body frame: it is resident for as long as the plugin is installed and enabled,
    // which is the same promise a headless frame makes - and a plugin that draws the pet is the
    // most likely one to be asked something ("what are you doing right now?").
    if (this.bodyFrame && this.bodyFrame.pluginId === targetId) return this.bodyFrame;
    for (const frame of this.surfaceFrames.values()) {
      if (frame.pluginId === targetId) return frame;
    }
    return null;
  }

  /**
   * Settles one pending call from a callee's answer.
   *
   * Accepted only from the frame that was asked: the ticket is a correlation id, and without
   * the window check any plugin could settle any call by replaying one it saw... except that
   * it never sees one - but the check costs nothing and this is the boundary where a plugin's
   * bytes become another plugin's return value.
   *
   * @returns {boolean} true when the message was a call result (so it must not also be
   *   offered to the frame's own handler)
   */
  handleCallResult(frame, data) {
    if (!data || typeof data !== 'object' || data.v !== V || data.type !== TYPE_CALL_RESULT) {
      return false;
    }
    const ticket = typeof data.ticket === 'string' ? data.ticket : '';
    const entry = this.pendingCalls.get(ticket);
    if (!entry) return true;      // unknown or already settled: nothing to settle
    if (entry.frame !== frame) return true; // only the plugin that was asked may answer
    this.pendingCalls.delete(ticket);
    clearTimeout(entry.timer);
    this.releaseDepth(entry.target, 1);
    if (data.ok === true) {
      const encoded = JSON.stringify(data.result === undefined ? null : data.result);
      if (encoded && encoded.length > CALL_MAX_RESULT) {
        entry.reject(new Error(
          `plugin.call 的回答有 ${encoded.length} 字节，超过 ${CALL_MAX_RESULT} 上限（${entry.target}/${entry.method}）`));
        return true;
      }
      entry.resolve(data.result === undefined ? null : data.result);
      return true;
    }
    const reason = typeof data.error === 'string' && data.error !== ''
      ? data.error
      : `${entry.target}/${entry.method} 没有说为什么拒绝`;
    entry.reject(new Error(reason));
    return true;
  }

  /** Fails every pending call whose target frame is going away. */
  failCallsFor(frame, why) {
    for (const [ticket, entry] of [...this.pendingCalls]) {
      if (entry.frame !== frame) continue;
      this.pendingCalls.delete(ticket);
      clearTimeout(entry.timer);
      this.releaseDepth(entry.target, 1);
      entry.reject(new Error(`${why}：${entry.target}/${entry.method} 没有回答`));
    }
  }

  /** Lowers a serving-depth counter, deleting the entry rather than storing 0. */
  releaseDepth(pluginId, by) {
    const now = (this.callDepth.get(pluginId) || 0) - by;
    if (now > 0) this.callDepth.set(pluginId, now);
    else this.callDepth.delete(pluginId);
  }

  /**
   * Tells every mounted frame which colour scheme the interface is in.
   *
   * Called from `applyLayout` (main.js) on every push, not only when the value changes: a frame
   * that mounted between two pushes must still hear it, and `sendTheme` is a no-op before that
   * frame's handshake - so "send it every time" is what makes "never miss it" true.
   */
  setTheme(theme) {
    const value = theme === 'light' ? 'light' : 'dark';
    for (const frame of this.surfaceFrames.values()) frame.sendTheme(value);
    for (const frame of this.backgroundFrames.values()) frame.sendTheme(value);
    this.bodyFrame?.sendTheme(value);
  }

  /**
   * Routes one host event to the plugin it names.
   *
   * An event for a plugin with no frame on screen is dropped rather than
   * broadcast: the frames are opaque origins, so "who is this for" has to come
   * from the payload, and sending a plugin another plugin's stream would leak
   * one plugin's data into another.
   */
  routeEvent(ev) {
    const id = ev?.pluginId;
    const name = ev?.name;
    if (typeof id !== 'string' || typeof name !== 'string') return;
    let delivered = 0;
    for (const frame of this.surfaceFrames.values()) {
      if (frame.pluginId === id) {
        frame.sendEvent(name, ev.payload);
        delivered++;
      }
    }
    // The headless frames count as mounted interfaces: that is the whole reason
    // they exist. Without this a background-only plugin would never receive
    // anything, which is the bug the frame was added to fix.
    const bg = this.backgroundFrames.get(id);
    if (bg) {
      bg.sendEvent(name, ev.payload);
      delivered++;
    }
    // The body frame counts too, and for the same reason: it is mounted for the whole session,
    // so "the pet flinches when a reminder fires" is a thing a body plugin can do even though
    // the user has no card open.
    if (this.bodyFrame && this.bodyFrame.pluginId === id) {
      this.bodyFrame.sendEvent(name, ev.payload);
      delivered++;
    }
    if (delivered === 0) {
      this.debug(`plugin event for ${id} dropped: no interface on screen`);
    }
  }

  /**
   * Places the floating surface from the layout.
   *
   * `surface` is null when there is none, which is the same convention the
   * bubble and the overlay use. Every rectangle is used VERBATIM: these are the
   * numbers the window was cropped to, so deriving a position here could only
   * ever disagree with what is visible.
   *
   * The i-th declared rectangle gets the i-th frame, and the i-th frame is told
   * its own index in the handshake - that is how a plugin knows which of its
   * rectangles it is drawing into when it declared several.
   */
  applyGeometry(surface, view) {
    const mount = this.els.pluginSurface;
    if (!surface || !Array.isArray(surface.rects) || surface.rects.length === 0) {
      this.clearSurface();
      mount.classList.add('hidden');
      return;
    }
    // A surface with no plugin id is not renderable, and rendering it anyway is
    // worse than showing nothing.
    //
    // Every operation on a frame needs the id: `src` is
    // `/plugin-asset/<id>/<entry>` and each request is `PluginRequest(<id>, ...)`.
    // With an empty id the asset route answers "bad plugin asset path", the
    // capability calls are refused, and - the part that is not merely cosmetic -
    // the frames still MOUNT with `pointer-events: auto`, so they sit over the
    // panel and swallow every click meant for it. A blank surface area is a visible
    // bug; a dead panel is a much worse one, and it is very hard to attribute to a
    // plugin.
    //
    // This is a guard, not the fix: Go now stamps the id on the view it sends (see
    // the region branch in app.go, where an unstamped view made every surface
    // unusable). It is kept because the failure mode is so disproportionate to the
    // cause, and because the check is free.
    if (!surface.pluginId) {
      this.debug('plugin surface has no plugin id; not mounting its frames');
      this.clearSurface();
      mount.classList.add('hidden');
      return;
    }

    const rects = surface.rects;
    // Frames are keyed by the block's DECLARATION index (`rect.index`), never by position.
    // A hidden block still arrives in `rects` (with `hidden: true`), so the key set of a
    // surface does not change when a block is shown or hidden - which is what lets a plugin
    // toggle a box mid-exchange without losing the document inside it.
    const wanted = rects.map((r, i) => ({ rect: r, index: r.index ?? i }));

    // Rebuild when the plugin changes, when its VERSION changes, or when the SET of blocks
    // changes; otherwise reuse, so a plugin's interface is not torn down and re-created on
    // every layout.
    //
    // The version is part of the test for the same reason it is on the slot frame: an update
    // keeps the id and changes the files behind it, so an id-only comparison would leave the
    // previous version's document running in a surface the user had just updated.
    const sameSet =
      this.surfaceFrames.size === wanted.length &&
      wanted.every((w) => this.surfaceFrames.has(w.index));
    const granted = grantedKey(view?.granted);
    if (this.surfacePluginId !== surface.pluginId ||
        this.surfaceVersion !== (view?.version || '') ||
        this.surfaceGranted !== granted ||
        !sameSet) {
      this.clearSurface();
      this.surfacePluginId = surface.pluginId;
      this.surfaceVersion = view?.version || '';
      this.surfaceGranted = granted;
      for (const { rect, index } of wanted) {
        const frame = new PluginFrame({
          pluginId: surface.pluginId,
          version: view?.version || '',
          entry: view?.entry || 'panel.html',
          kind: 'surface',
          permissions: view?.permissions || [],
          granted: view?.granted || [],
          // The DECLARATION index, which is also the block's JOB (0 question, 1 answer,
          // 2 input for the chat plugin). Passing the position in this loop instead told a
          // surviving block that it had another block's job - the input pill rendered as a
          // result strip and the composer disappeared.
          rectIndex: index,
          rectCount: rects.length,
          rect,
          host: this.host,
          onRequest: (m, p) => this.request(surface.pluginId, m, p),
        });
        this.surfaceFrames.set(index, frame);
        mount.append(frame.el);
      }
    }
    mount.classList.remove('hidden');
    for (const { rect, index } of wanted) {
      const frame = this.surfaceFrames.get(index);
      if (!frame) continue;
      // The plugin draws inside its own document, so it has to be TOLD when the box under it
      // moves or resizes - `init` happened once, long ago. Compared by value: the layout object is
      // rebuilt on every geometry pass, so identity would always differ.
      const moved = JSON.stringify(frame.rect) !== JSON.stringify(rect);
      frame.rect = rect;
      frame.el.style.left = `${rect.x}px`;
      frame.el.style.top = `${rect.y}px`;
      frame.el.style.width = `${rect.w}px`;
      frame.el.style.height = `${rect.h}px`;
      // An ellipse block is cropped to an ELLIPSE by the native region, so the
      // frame has to be drawn as one too: 50% on all four corners is the only
      // radius that matches an ellipse of this box. `r.r` is 0 for these blocks
      // (Go zeroes it, because a rounded rectangle and an ellipse are different
      // shapes and the crop uses one or the other), so falling through to `r.r`
      // would draw a hard-edged rectangle inside an elliptical hole - the
      // corners of the frame would be visible tint over the desktop.
      const radius = rect.ellipse === true ? '50%' : `${rect.r}px`;
      frame.el.style.borderTopLeftRadius = radius;
      frame.el.style.borderTopRightRadius = radius;
      frame.el.style.borderBottomLeftRadius = radius;
      frame.el.style.borderBottomRightRadius = radius;
      // A HIDDEN block keeps its frame, its document and its box: it is only taken out of
      // sight and out of hit-testing.
      //
      // `visibility` rather than `display: none`: hiding an iframe with display collapses its
      // viewport to nothing, so the plugin's document would re-lay-out (and a scrolled
      // transcript would lose its position) the moment the block came back. These two lines
      // are the page agreeing with the window region, which already stopped including a
      // hidden block - nothing is painted there, so nothing may be clickable there.
      const hidden = rect.hidden === true;
      frame.el.style.visibility = hidden ? 'hidden' : '';
      frame.el.style.pointerEvents = hidden ? 'none' : '';
      if (hidden) frame.el.dataset.hidden = '1';
      else delete frame.el.dataset.hidden;
      if (moved) frame.sendGeometry(rect);
    }
  }

  /** Hides and removes every surface frame. */
  clearSurface() {
    for (const frame of this.surfaceFrames.values()) {
      frame.destroy();
      // Same reason as closeBackground: a caller waiting on this frame must be told the
      // frame is gone, not left to sit through the timeout.
      this.failCallsFor(frame, '目标插件的浮层面已关闭');
    }
    this.surfaceFrames = new Map();
    this.surfacePluginId = '';
    this.surfaceVersion = '';
    this.surfaceGranted = '';
  }
}
