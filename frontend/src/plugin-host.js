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
export const SLOT_CHAT = 'chat';

/** The message the plugin sends to announce itself, and the host's answer. */
const TYPE_READY = 'ready';
const TYPE_INIT = 'init';
const TYPE_REQUEST = 'request';
const TYPE_RESPONSE = 'response';
const TYPE_EVENT = 'event';

/**
 * One plugin's sandboxed interface.
 *
 * `kind` is 'panel' or 'surface'. A panel view fills its slot; a surface view is
 * placed at a rectangle. The two differ only in where the iframe goes and what
 * the handshake says about it - the transport, the sandbox and the permission
 * check are identical, which is why they share this class.
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
   * @param {'panel'|'surface'} opts.kind
   * @param {string} [opts.slot]  the panel slot name, for a panel view
   * @param {string[]} opts.permissions everything the manifest declares
   * @param {string[]} opts.granted     what the user allowed
   * @param {number} [opts.rectIndex]
   * @param {number} [opts.rectCount]
   * @param {object|null} [opts.rect]
   * @param {function} opts.onRequest   (method, payload) => Promise<any>
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

    /** True once this frame has been answered with its one init. */
    this.initialised = false;
    /** Pending requests, so a response can find its caller. */
    this.pending = new Map();
    this.nextId = 1;
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
    frame.setAttribute('src', `/plugin-asset/${this.pluginId}/${this.entry}`);
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
    if (this.kind === 'panel') {
      // Which slot the host mounted this interface into. A plugin that declares
      // panelSlot:"chat" uses it to confirm it landed where it asked for.
      surface.slot = this.slot;
    } else {
      surface.rectIndex = this.rectIndex;
      surface.rectCount = this.rectCount;
      surface.rect = this.rect;
    }
    return {
      v: V,
      type: TYPE_INIT,
      pluginId: this.pluginId,
      permissions: this.permissions,
      granted: this.granted,
      surface,
    };
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
   * way for a plugin to probe the host. A well-formed request is forwarded to
   * App.PluginRequest, which is where the plugin's permission is checked and
   * where a refusal is turned into an error - this file never decides what is
   * allowed.
   */
  dispatch(data) {
    const { id, method } = data;
    if (typeof id !== 'string' || id === '') return;
    if (typeof method !== 'string' || method === '') return;

    const payload = data.payload === undefined ? {} : data.payload;
    App.PluginRequest(this.pluginId, method, JSON.stringify(payload))
      .then((result) => {
        // The binding answers with the method's JSON-encoded value.
        let value = result;
        if (typeof result === 'string' && result !== '') {
          try {
            value = JSON.parse(result);
          } catch {
            value = result;
          }
        }
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

  /** Posts a message into the frame. */
  post(message) {
    if (!this.el.contentWindow) return;
    this.el.contentWindow.postMessage(message, '*');
  }

  /** Removes the frame. */
  destroy() {
    this.el.remove();
  }
}

/**
 * PluginHost owns every plugin iframe on the page.
 *
 * It keeps three KINDS: the floating surface's frames (one per declared
 * rectangle), the one panel slot view, and the headless background frames (one per
 * plugin that asked). They are independent - a floating surface stays up while the
 * panel is open - which is why they are tracked separately rather than as "the
 * current view".
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

    /** The floating surface's frames, in declaration order. */
    this.surfaceFrames = [];
    /** The panel slot's single frame, or null. */
    this.slotFrame = null;
    /** The slot name the current slot view fills, or ''. */
    this.slotName = '';
    /** The plugin id the current surface belongs to, or ''. */
    this.surfacePluginId = '';
    /** The version the current surface frames were built from, or ''. */
    this.surfaceVersion = '';
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

    this.onMessage = this.onMessage.bind(this);
    window.addEventListener('message', this.onMessage);
    // The host tells the plugins about state changes. The event name is emitted
    // by Go as a double-quoted literal, and this subscription is in the scan
    // list the static gate checks against it.
    EventsOn('pet:plugin-event', (ev) => this.routeEvent(ev));
  }

  /** True while a plugin's interface is on screen in either slot. */
  isOpen() {
    return this.surfaceFrames.length > 0 || !!this.slotFrame;
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
   * Re-mounted only when the plugin or its version changes, for the same reason
   * the slot frame is: an update keeps the id and replaces the files behind it.
   *
   * @param {object} plugin a row from PluginsInstalled
   */
  openBackground(plugin) {
    if (!plugin || !plugin.id) return;
    const existing = this.backgroundFrames.get(plugin.id);
    if (existing && existing.version === (plugin.version || '')) {
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
      onRequest: (m, p) => App.PluginRequest(plugin.id, m, JSON.stringify(p)),
    });
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
    for (const frame of this.surfaceFrames) {
      if (frame.el.contentWindow === event.source) {
        frame.handleMessage(event.data);
        return;
      }
    }
    if (this.slotFrame && this.slotFrame.el.contentWindow === event.source) {
      this.slotFrame.handleMessage(event.data);
      return;
    }
    for (const frame of this.backgroundFrames.values()) {
      if (frame.el.contentWindow === event.source) {
        frame.handleMessage(event.data);
        return;
      }
    }
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
    for (const frame of this.surfaceFrames) {
      if (frame.pluginId === id) {
        frame.sendEvent(name, ev.payload);
        delivered++;
      }
    }
    if (this.slotFrame && this.slotFrame.pluginId === id) {
      this.slotFrame.sendEvent(name, ev.payload);
      delivered++;
    }
    // The headless frames count as mounted interfaces: that is the whole reason
    // they exist. Without this a background-only plugin would never receive
    // anything, which is the bug the frame was added to fix.
    const bg = this.backgroundFrames.get(id);
    if (bg) {
      bg.sendEvent(name, ev.payload);
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
    // Rebuild when the plugin changes, when its VERSION changes, or when the count
    // changes; otherwise reuse, so a plugin's interface is not torn down and
    // re-created on every layout.
    //
    // The version is part of the test for the same reason it is on the slot frame:
    // an update keeps the id and changes the files behind it, so an id-only
    // comparison would leave the previous version's document running in a surface
    // the user had just updated.
    if (this.surfacePluginId !== surface.pluginId ||
        this.surfaceVersion !== (view?.version || '') ||
        this.surfaceFrames.length !== rects.length) {
      this.clearSurface();
      this.surfacePluginId = surface.pluginId;
      this.surfaceVersion = view?.version || '';
      for (let i = 0; i < rects.length; i++) {
        const frame = new PluginFrame({
          pluginId: surface.pluginId,
          version: view?.version || '',
          entry: view?.entry || 'panel.html',
          kind: 'surface',
          permissions: view?.permissions || [],
          granted: view?.granted || [],
          rectIndex: i,
          rectCount: rects.length,
          rect: rects[i],
          onRequest: (m, p) => App.PluginRequest(surface.pluginId, m, JSON.stringify(p)),
        });
        this.surfaceFrames.push(frame);
        mount.append(frame.el);
      }
    }
    mount.classList.remove('hidden');
    rects.forEach((r, i) => {
      const frame = this.surfaceFrames[i];
      if (!frame) return;
      frame.rect = r;
      frame.el.style.left = `${r.x}px`;
      frame.el.style.top = `${r.y}px`;
      frame.el.style.width = `${r.w}px`;
      frame.el.style.height = `${r.h}px`;
      // An ellipse block is cropped to an ELLIPSE by the native region, so the
      // frame has to be drawn as one too: 50% on all four corners is the only
      // radius that matches an ellipse of this box. `r.r` is 0 for these blocks
      // (Go zeroes it, because a rounded rectangle and an ellipse are different
      // shapes and the crop uses one or the other), so falling through to `r.r`
      // would draw a hard-edged rectangle inside an elliptical hole - the
      // corners of the frame would be visible tint over the desktop.
      const radius = r.ellipse === true ? '50%' : `${r.r}px`;
      frame.el.style.borderTopLeftRadius = radius;
      frame.el.style.borderTopRightRadius = radius;
      frame.el.style.borderBottomLeftRadius = radius;
      frame.el.style.borderBottomRightRadius = radius;
    });
  }

  /** Hides and removes every surface frame. */
  clearSurface() {
    for (const frame of this.surfaceFrames) frame.destroy();
    this.surfaceFrames = [];
    this.surfacePluginId = '';
    this.surfaceVersion = '';
  }

  /**
   * Shows one plugin's interface in a named panel slot.
   *
   * This is what "filling a slot" means for a plugin: its view fills the slot's
   * existing body, which for 'chat' is the 对话 tab's rectangle. No window
   * geometry is involved, so the panel's 380x520 rectangle - and the region crop
   * that matches it - are untouched.
   *
   * `slot` is passed through to the frame so the handshake can tell the plugin
   * which slot it landed in (T51). At most one slot view exists at a time: the
   * panel shows one tab at a time, so opening a second would leave the first
   * plugin running invisibly with no way for the user to close it.
   *
   * The mount point is the slot element itself. The host does not add classes to
   * it beyond clearing the "no plugin here" hint - the slot's box was sized by
   * its own stylesheet before any plugin existed, so mounting must not change it.
   */
  openSlot(slot, plugin) {
    this.closeSlot();
    const frame = new PluginFrame({
      pluginId: plugin.id,
      version: plugin.version || '',
      entry: plugin.entry || 'panel.html',
      kind: 'panel',
      slot,
      permissions: plugin.permissions || [],
      granted: plugin.granted || [],
      onRequest: (m, p) => App.PluginRequest(plugin.id, m, JSON.stringify(p)),
    });
    this.slotFrame = frame;
    this.slotName = slot;
    frame.el.dataset.slot = slot;
    const mount = this.els.chatSlot;
    if (!mount) return;
    mount.textContent = '';
    mount.append(frame.el);
  }

  /** Hides the slot's interface and removes its frame. */
  closeSlot() {
    if (this.slotFrame) {
      this.slotFrame.destroy();
      this.slotFrame = null;
    }
    this.slotName = '';
  }

  /** True while a slot view is showing. */
  slotOpen() {
    return !!this.slotFrame;
  }
}
