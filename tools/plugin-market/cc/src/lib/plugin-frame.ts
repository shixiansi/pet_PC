/**
 * Hosts a plugin's OWN interface inside the control center's window.
 *
 * WHY THIS FILE EXISTS (SECOND TIME)
 *
 * It is the control center's rewrite of `tools/plugin-market/frontend/src/plugin-ui.js`,
 * and the reason it exists at all is that the old one stopped being shipped: the control
 * center became this program's interface, and the plugin page it grew only listed plugins
 * and their permissions. Three of the four bundled plugins (voice / qq / bilibili) declare
 * `panel: false`, no `surface` blocks and `background: true` - their documents are meant to
 * be mounted HERE, with `surface.slot = "workshop"`, and nowhere else. Without this file
 * those three plugins have no visible interface in any program: just a headless frame in
 * the pet.
 *
 * THE THIRD CALLER: the 「扩展能力」 page's test bench (`pages/Bench.svelte`). It mounts the same
 * documents to answer a different question - "what does this plugin do when it lands HERE?" - so it
 * is the SAME implementation rather than a second one. Two iframe hosts sharing one window would
 * drift, and the copy that drifts is the one nobody ships. What the bench needs on top is
 * visibility (every message, via `onMessage`) and one sanctioned lie (`denyRequests`: answer every
 * request as refused without ever reaching the gate, so the "user granted nothing" path can be
 * walked in the UI without editing the real grants).
 *
 * WHAT IS IDENTICAL TO THE PET'S HOST, AND MUST STAY SO
 *
 * `sandbox="allow-scripts"` alone - adding allow-same-origin would hand every installed
 * plugin this page's DOM, which is the one thing the sandbox exists to prevent. The frame
 * loads `/plugin-asset/<id>/<entry>`, served by the shared Go handler
 * (`internal/plugins.ServePluginAsset`). Requests go to `Market.PluginRequest`, i.e. the
 * shared gate with THIS program's capability table. This file never decides whether a call
 * is allowed.
 *
 * WHAT DIFFERS, AND WHY A REFUSAL IS NORMAL
 *
 * This program has the plugin's files, its private directory, its settings, the network and
 * audio. It has no todo list, no pomodoro, no pet window and no reminder scheduler, so
 * `todos.*`, `timer.*`, `bubble.say`, `surface.*` and `events.subscribe` are refused. That
 * is an honest answer about where the plugin is running, not a failure - and it is why a
 * plugin that wants events subscribes from the frame the PET mounts (its `background`
 * declaration), while its workshop here talks to it through `storage`.
 *
 * WHY THE MODULE OWNS THE ELEMENT AND SVELTE DOES NOT
 *
 * A re-created iframe is a DESTROYED document: a half-filled form, a scroll position, and
 * an already-answered `init` handshake all go with it. Svelte's template is free to rebuild
 * nodes (conditional blocks, keyed blocks, list diffs), so the frame is created here,
 * imperatively, on a stable mount point the page owns. `PluginFrame.svelte` is only the
 * adapter that decides WHEN.
 */
import { PluginHostInfo, PluginRequest } from './bridge';
// The host's down-link (`pet:plugin-event`). Imported here rather than through bridge.ts because
// this module owns the frames: a subscription that outlived its frame would post into a torn-down
// window, so mount and destroy are the only two places it may be created and dropped.
import { EventsOn } from '../../../frontend/wailsjs/runtime/runtime';

/** The protocol version, matching internal/plugins.BridgeJS. */
const V = 1;

/**
 * What this program serves, as `{program, methods}` - the workshop's half of `init.host`.
 *
 * Fetched once per page load and read when a plugin says `ready` (init is the answer to that,
 * not to the mount, so there is always a document load's worth of time to fill this in). Absent
 * is a legal state: `init.host` is optional, and a plugin that does not find it falls back to
 * `surface.slot` - the rule that existed before the field.
 *
 * WHY IT COMES FROM GO: the list is `marketRegistry`'s own table. A copy kept here would answer
 * "does this window serve `llm.profile`" with something that is not the gate's answer, which is
 * the mistake this field exists to stop (571 `本程序不支持该方法` lines in one day's logs).
 */
let hostInfo: { program: string; methods: string[] } | null = null;
let hostInfoAsked = false;
function loadHostInfo(): void {
  if (hostInfoAsked) return;
  hostInfoAsked = true;
  void PluginHostInfo()
    .then((info) => {
      // Copied into a plain object: the binding's value may be a `models.ts` class instance,
      // and anything object-shaped that came through Svelte would be a Proxy - which cannot
      // cross `postMessage` (see mountPlugin's DataCloneError note).
      hostInfo = info && typeof info.program === 'string' && Array.isArray(info.methods)
        ? { program: info.program, methods: [...info.methods] }
        : null;
    })
    .catch(() => {
      hostInfo = null;
    });
}

/** The colour scheme a frame is drawn in. */
export type FrameTheme = 'light' | 'dark';

/** One declared rectangle, as `init.surface.rect` carries it (measured from the pet). */
export interface MountRect {
  dx: number;
  dy: number;
  w: number;
  h: number;
  radius?: number;
}

/**
 * Where the document is told it landed (`init.surface`).
 *
 * The pet has three shapes and this window has one, so the bench can show a plugin all four:
 *
 *   - `{kind:'panel', slot:'workshop'}`  - here, filling the page (this window's own answer);
 *   - `{kind:'panel', slot:'background'}` - the pet's hidden常驻帧 (no rectangle at all);
 *   - `{kind:'surface', rectIndex, rect}` - one of the pet's floating rectangles;
 *   - `{kind:'body', slot:'body', rect}` - the PET itself (`manifest.body`): one rectangle, the
 *     pet's own, which the user sizes with 桌宠大小.
 *
 * It is a TYPED COPY rather than "whatever the caller had": the values are told to the document
 * across `postMessage`, so they must be plain data (a Svelte `$state` proxy would throw) - see the
 * DataCloneError note in `mountPlugin`.
 */
export interface MountSurface {
  kind: 'panel' | 'surface' | 'body';
  slot?: string;
  rectIndex?: number;
  rectCount?: number;
  rect?: MountRect | null;
}

/** One message that crossed the boundary, as the bench's log shows it. */
export interface BoundaryEvent {
  /** `HH:MM:SS.mmm`, local. */
  at: string;
  /** `in` = the plugin said it, `out` = the host said it. */
  dir: 'in' | 'out';
  type: 'ready' | 'init' | 'request' | 'response' | 'event' | 'theme' | 'error';
  /** One line, already worded: the bench prints it verbatim. */
  text: string;
  /** For `response` only: did the host answer `ok`. */
  ok?: boolean;
}

/** What one mount needs from the installed row. Nothing here is derived. */
export interface MountRequest {
  pluginId: string;
  version: string;
  entry: string;
  permissions: string[];
  granted: string[];
  /** Optional; the default is this window's own answer, `panel/workshop`. */
  surface?: MountSurface;
  /** Optional observer of every message in both directions. The bench's log; nothing else uses it. */
  onMessage?: (ev: BoundaryEvent) => void;
  /**
   * Answer every request as refused, WITHOUT calling the gate, and tell the document in `init`
   * that nothing was granted.
   *
   * It exists so the "the user ticked nothing" path can be walked without touching the real
   * grants: `Grant()` would change what the gate enforces for the plugin everywhere, and that is
   * the user's decision on the 插件 page, not a test-bench toggle. Both halves are forced to agree
   * here (empty `granted` + refusal at this layer), which is the only way this can be honest.
   */
  denyRequests?: boolean;
}

/** A mounted frame, as the page holds it. */
export interface PluginFrame {
  readonly pluginId: string;
  /** Tells the document the window's scheme changed - WITHOUT remounting it. */
  setTheme(theme: FrameTheme): void;
  /**
   * Posts one `event` at the document - the same wire message the host's real down-link
   * (`share.changed` / `storage.changed` / `config.changed`) arrives as, but triggered by hand.
   * The bench's "推一条事件"; the plugin cannot tell the two apart, which is the point.
   */
  sendEvent(name: string, payload: unknown): void;
  /** Removes the frame and the window listener. Safe to call twice. */
  destroy(): void;
}

/**
 * The colour scheme this window is currently drawing.
 *
 * Read from the document rather than passed in, so a frame mounted at any moment (boot, a
 * plugin switch, a re-render) picks up whatever scheme is in force without a second source
 * of truth. Nothing in the control center sets `data-theme` yet, so this is 'dark' today -
 * which is exactly why it reads the attribute instead of assuming: the day the theme switch
 * lands, every frame follows it with no edit here.
 */
export function windowTheme(): FrameTheme {
  return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
}

/**
 * The palette pack in force, or '' when none was chosen.
 *
 * The frame does not RECEIVE the palette: it is spliced into the document the host serves
 * (`internal/plugins/asset.go`), because the frame is an opaque origin and this window cannot touch
 * its DOM. So all this function does is name the pack in the URL.
 *
 * A pack change reaches frames mounted AFTER it: an open document keeps the stylesheet it was served
 * with, and re-creating frames to re-theme them would throw away whatever the user had typed into a
 * plugin's own form.
 */
export function windowPack(): string {
  return document.documentElement.dataset.themePack ?? '';
}

/**
 * Mounts one plugin interface under `mount`.
 *
 * Callers must destroy the previous frame before mounting the next one: the page shows one
 * plugin at a time, and a second document would still be running with permissions the user
 * granted to it. The rail swaps pages, so the plugin page and the bench are never both mounted -
 * which is what keeps that "one document at a time" claim true across the whole window.
 */
export function mountPlugin(mount: HTMLElement, req: MountRequest, theme: FrameTheme): PluginFrame {
  // Start the capability-table read on the first mount rather than at module load: this module
  // is imported by tests too, and a top-level call would reach for a binding that does not exist
  // in that environment. `init` is only sent once the plugin says `ready`, which is a document
  // load later, so there is time for the promise to settle.
  loadHostInfo();
  // Everything this frame will ever repeat back to the plugin is read ONCE here, out of
  // whatever the caller handed over.
  //
  // WHY: those values come from a Svelte 5 `$state` row, which DEEP-PROXIES what it holds - so
  // `inst.Manifest.permissions` and `inst.Granted` are Proxies, not arrays. A Proxy cannot go
  // through `postMessage`'s structured clone, and the failure is not subtle:
  //
  //   Uncaught DataCloneError: Failed to execute 'postMessage' on 'Window':
  //   [object Object] could not be cloned.
  //
  // …thrown inside the message listener, i.e. the plugin never receives its ONE `init` and sits
  // on 「等待宿主初始化…」 forever while the host's own page looks perfectly fine. Measured on the
  // real window, not theorised. A spread reads through the proxy and yields a plain array of
  // strings - exactly what the wire format wants - so no caller has to know this rule.
  const pluginId = req.pluginId;
  const entry = req.entry;
  const version = req.version;
  const permissions = [...(req.permissions ?? [])];
  // Both halves of the bench's "模拟未授权" are decided HERE, together, so they can never
  // disagree: a frame that refuses every request while its `init` claims grants is a frame that
  // teaches its plugin the wrong thing about itself.
  const deny = req.denyRequests === true;
  const granted = deny ? [] : [...(req.granted ?? [])];
  const surface = plainSurface(req.surface);
  const observe = typeof req.onMessage === 'function' ? req.onMessage : null;

  /** Hands one line to the observer. A throwing observer must not break the bridge. */
  function emit(ev: Omit<BoundaryEvent, 'at'>) {
    if (!observe) return;
    try {
      observe({ at: clock(), ...ev });
    } catch {
      /* the log is a viewer; it does not get to break the transport */
    }
  }

  const frame = document.createElement('iframe');
  frame.className = 'plugin-frame';
  // Exactly allow-scripts. See the file header; this is AGENTS.md §3.3.
  frame.setAttribute('sandbox', 'allow-scripts');
  // `?theme=` is how the plugin knows which scheme to paint on its FIRST frame; the `theme`
  // message below covers a switch that happens later. Two channels on purpose - see
  // setTheme.
  //
  // `?pack=` has only ONE channel, and it is not a message: the host splices the pack's tokens into
  // the document it serves, so the scheme can be switched live while the pack takes effect on the
  // next mount. That asymmetry is the price of not requiring plugin authors to implement anything.
  frame.setAttribute('src', `/plugin-asset/${pluginId}/${entry}?theme=${theme}&pack=${windowPack()}`);
  frame.dataset.pluginId = pluginId;
  frame.dataset.pluginVersion = version;
  // Native widgets inside the document (scrollbars, carets, a <select> popup) would otherwise
  // keep the platform's dark chrome inside a light window.
  frame.style.colorScheme = theme;
  // The element is NOT Svelte's, so Svelte's scoped CSS cannot reach it. Its box rules live
  // here, next to the code that creates it. `min-width: 0` is not decoration: an iframe's
  // default size is 300x150, so without it that 300px becomes the frame's min-content width
  // and it overflows a narrower column (measured at the window's minimum size).
  frame.style.cssText +=
    'display:block;width:100%;height:100%;min-width:0;max-width:100%;border:0;margin:0;padding:0;background:transparent;';

  let alive = true;
  let initialised = false;
  let current: FrameTheme = theme;

  function post(message: Record<string, unknown>) {
    const win = frame.contentWindow;
    if (!alive || !win) return;
    try {
      // `*` because the frame is an opaque origin: there is no origin to name. Safe because the
      // only listener on the other side is the plugin's own, and nothing else is ever posted in.
      win.postMessage({ ...message, v: V }, '*');
    } catch (err) {
      // Loud, because the alternative is what this file just spent a round on: an exception
      // inside a `message` listener is invisible (no stack in the page, the plugin simply never
      // boots), and the symptom reads as "the plugin's界面 is broken" rather than "the host sent
      // something uncloneable". Name the type and the shape instead.
      console.error(
        '[plugin-frame] 发给插件的是含不可结构化克隆的值（' +
          String(message.type) +
          '）：面板/插件的状态可能被 Svelte 的 $state 代理着，过边界前要复制成纯数据。',
        err,
      );
      // …and one line for the bench, because the console is not a screen anybody looks at. A
      // message that never crossed is the single most confusing failure this file has, and it
      // looked like "the plugin's界面 is broken" for a whole round.
      emit({ dir: 'out', type: 'error', text: String(message.type) + ' 没发出去：值过不了结构化克隆' });
    }
  }

  /**
   * Answers one plugin request through the shared gate.
   *
   * A refusal is sent as `{ok:false, error}` rather than swallowed: the plugin's own UI
   * decides whether a missing capability is worth telling the user about (a workshop that
   * cannot read the todo list simply does not draw that card).
   */
  async function answer(data: { id?: unknown; method?: unknown; payload?: unknown }) {
    const id = data.id;
    const method = data.method;
    if (typeof id !== 'string' || typeof method !== 'string') return;
    const payload = JSON.stringify(data.payload ?? {});
    emit({ dir: 'in', type: 'request', text: method + '  ← ' + byteCount(payload) });
    if (deny) {
      // The bench's "拒绝全部": the refusal happens HERE, before the gate, and says so. Calling the
      // gate and then overwriting its answer would make the log disagree with the host's log.
      const error = '测试台：模拟未授权（这个插件在 init 里被告知什么都没授予）';
      post({ type: 'response', id, ok: false, error });
      emit({ dir: 'out', type: 'response', ok: false, text: method + '  → ' + error });
      return;
    }
    try {
      const raw = await PluginRequest(req.pluginId, method, payload);
      // The binding returns the method's JSON as a STRING; the bridge expects the value
      // itself, so it is parsed back. A method that returns nothing yields an empty string,
      // which is not valid JSON - hence the guard rather than a bare parse that would reject
      // on a successful no-op.
      let value: unknown = null;
      if (raw !== '' && raw != null) {
        try {
          value = JSON.parse(raw);
        } catch {
          value = raw;
        }
      }
      post({ type: 'response', id, ok: true, result: value });
      emit({ dir: 'out', type: 'response', ok: true, text: method + '  → ok  ' + byteCount(raw) });
    } catch (err) {
      const message = why(err);
      post({ type: 'response', id, ok: false, error: message });
      emit({ dir: 'out', type: 'response', ok: false, text: method + '  → ' + message });
    }
  }

  const onMessage = (event: MessageEvent) => {
    // THE security condition: only the frame this function created. Without it any other
    // frame in this window - or the page itself - could drive the plugin's bridge.
    if (!alive || event.source !== frame.contentWindow) return;
    const data = event.data as { v?: unknown; type?: unknown } | null;
    if (!data || data.v !== V) return;

    if (data.type === 'ready') {
      // Exactly one init. The plugin announces itself once; a second answer would be a second
      // grant list applied after the plugin already rendered against the first.
      if (initialised) {
        emit({ dir: 'in', type: 'ready', text: 'ready（第二条：忽略，init 只发一次）' });
        return;
      }
      initialised = true;
      emit({ dir: 'in', type: 'ready', text: 'ready' });
      post({
        type: 'init',
        pluginId,
        permissions,
        // `Granted`, capital G, on the Go side (`plugins.Installed` has no json tags). It was
        // `granted` in the old file and the mismatch was SILENT: the property did not exist,
        // `|| []` turned it into an empty list, and every plugin was told the user had granted
        // NOTHING - so each control rendered read-only while the interface looked perfect.
        granted,
        // The window's scheme. The same value was on the frame URL for the first paint; this
        // is the one a plugin should trust, because a switch updates it.
        theme: current,
        // Where this document landed. It used to be hard-coded to the workshop; the bench mounts
        // the same documents as the pet's floating rectangles and as its hidden常驻帧, and a
        // plugin that reads `surface.kind`/`slot`/`rect` to decide what to draw would otherwise
        // be told it is always in this window (see the bench's 位置 selector).
        surface,
        // Which program this is, and what it answers. Optional in the protocol, and omitted
        // when the read has not landed yet - a plugin that ignores it is unaffected.
        ...(hostInfo ? { host: hostInfo } : {}),
      });
      emit({
        dir: 'out',
        type: 'init',
        text:
          surface.kind === 'surface' || surface.kind === 'body'
            ? surface.kind + ' #' + ((surface.rectIndex ?? 0) + 1) + '/' + (surface.rectCount ?? 1) +
              ' · granted ' + granted.length + '/' + permissions.length + (deny ? '（模拟未授权）' : '')
            : 'panel/' + (surface.slot || '') + ' · granted ' + granted.length + '/' + permissions.length +
              (deny ? '（模拟未授权）' : ''),
      });
      return;
    }

    if (data.type === 'request') void answer(data as { id?: unknown; method?: unknown; payload?: unknown });
  };

  // Attached BEFORE the element is in the document: the plugin's `ready` can arrive on the
  // first task after its script runs, and a listener attached later would miss it and leave
  // the document waiting for an init that never comes.
  window.addEventListener('message', onMessage);
  mount.replaceChildren(frame);

  // The host's down-link: `share.changed` (and, one day, whatever else the host emits) reaches
  // the plugin as the same `event` message the pet sends.
  //
  // WHY THIS WINDOW NEEDS IT: three bundled plugins write and read their OWN `command` key from
  // inside this page (ast-coder / voice-call / live-danmaku). Until this listener existed they
  // polled once a second, because the program that emits the event was the pet - and the pet has
  // no frame to deliver to when the document is mounted here. See docs/plugin-dev.md §7.1.
  //
  // The id check is not decoration: `EventsOn` is a broadcast, and a frame must never receive
  // another plugin's traffic.
  //
  // IT MAY NOT EXIST. `EventsOn` forwards to `window.runtime`, which only a Wails window has -
  // in `npm run dev` and in the render smoke (jsdom) there is none, and an unguarded call threw
  // `Cannot read properties of undefined (reading 'EventsOnMultiple')` from inside a `$effect`,
  // i.e. the frame never mounted and the error surfaced as "the page is broken". A browser that
  // can mount a frame but has no host events is exactly the true situation, so the fallback is
  // "no events" rather than "no frame".
  let stopEvents: () => void = () => {};
  try {
    stopEvents = EventsOn('pet:plugin-event', (ev: unknown) => {
      const msg = ev as { pluginId?: unknown; name?: unknown; payload?: unknown } | null;
      if (!msg || msg.pluginId !== pluginId || typeof msg.name !== 'string') return;
      // Copied field by field: `ev` came through the Wails binding and may be a proxy, and a
      // proxy cannot cross `postMessage` (see the DataCloneError note above).
      const payload = msg.payload ?? {};
      post({ type: 'event', name: msg.name, payload });
      emit({ dir: 'out', type: 'event', text: msg.name + '（宿主推送）' });
    });
  } catch {
    stopEvents = () => {};
  }

  return {
    pluginId,
    setTheme(next: FrameTheme) {
      current = next === 'light' ? 'light' : 'dark';
      frame.style.colorScheme = current;
      // Nothing to tell a document that has not completed its handshake - its `init` carries
      // the scheme anyway.
      if (initialised) {
        post({ type: 'theme', theme: current });
        emit({ dir: 'out', type: 'theme', text: current });
      }
    },
    sendEvent(name: string, payload: unknown) {
      const label = typeof name === 'string' ? name.trim() : '';
      if (!label) return;
      post({ type: 'event', name: label, payload: payload ?? {} });
      emit({ dir: 'out', type: 'event', text: label + '（手动推送）' });
    },
    destroy() {
      if (!alive) return;
      alive = false;
      window.removeEventListener('message', onMessage);
      // The host subscription goes with the frame: a listener left behind would keep posting
      // into a torn-down window, and a second mount would then deliver every event twice.
      stopEvents();
      frame.remove();
      mount.replaceChildren();
    },
  };
}

/** A rejection's message, printed rather than stringified. */
function why(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === 'string') return e;
  try {
    return JSON.stringify(e);
  } catch {
    return String(e);
  }
}

/**
 * Turns a caller's `surface` into the exact plain object `init` carries.
 *
 * Default is this window's own answer (`panel/workshop`), because that is what the 插件 page's
 * frames are and what every plugin has been told since the workshop existed - a caller that never
 * mentions `surface` must keep getting byte-identical behaviour.
 *
 * Every field is re-read as a primitive: the caller's object may be a Svelte `$state` proxy, and a
 * proxy cannot cross `postMessage` (see mountPlugin's DataCloneError note). Numbers are coerced
 * rather than trusted because a `<input type="number">` hands back strings at the edges.
 */
function plainSurface(s: MountSurface | undefined): MountSurface {
  if (!s) return { kind: 'panel', slot: 'workshop' };
  if (s.kind === 'body') {
    // The pet's own rectangle. `slot` is what the PET sends, and the plugin uses it to tell "I am
    // the pet" from "I am a card beside it" - so the bench has to say the same thing.
    const r = s.rect;
    return {
      kind: 'body',
      slot: 'body',
      rectIndex: 0,
      rectCount: 1,
      rect: r
        ? { dx: 0, dy: 0, w: Number(r.w) || 0, h: Number(r.h) || 0 }
        : null,
    };
  }
  if (s.kind === 'surface') {
    const r = s.rect;
    return {
      kind: 'surface',
      rectIndex: Number(s.rectIndex ?? 0) || 0,
      rectCount: Number(s.rectCount ?? 1) || 1,
      rect: r
        ? {
            dx: Number(r.dx) || 0,
            dy: Number(r.dy) || 0,
            w: Number(r.w) || 0,
            h: Number(r.h) || 0,
            ...(r.radius === undefined ? {} : { radius: Number(r.radius) || 0 }),
          }
        : null,
    };
  }
  // An unknown slot is passed through as given: the host does not decide where a plugin is
  // allowed to think it landed, and a bench that silently rewrote it would be testing its own
  // spelling rather than the plugin's behaviour.
  return { kind: 'panel', slot: s.slot || 'workshop' };
}

/** `HH:MM:SS.mmm`, local - the log's time column. */
function clock(): string {
  const d = new Date();
  const p = (n: number, w = 2) => String(n).padStart(w, '0');
  return p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds()) + '.' + p(d.getMilliseconds(), 3);
}

/** A payload's size in the log's own words - the pattern the host's log lines already use. */
function byteCount(value: unknown): string {
  if (typeof value !== 'string') return '0 B';
  // UTF-8 length, like the Go side's byte counts: a Chinese message is 3 bytes per character and
  // a log that said "1 B" for it would make the two programs' numbers disagree.
  let bytes = 0;
  for (const ch of value) {
    const code = ch.codePointAt(0) ?? 0;
    bytes += code < 0x80 ? 1 : code < 0x800 ? 2 : code < 0x10000 ? 3 : 4;
  }
  return bytes + ' B';
}
