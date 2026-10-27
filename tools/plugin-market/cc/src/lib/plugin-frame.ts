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
import { PluginRequest } from './bridge';

/** The protocol version, matching internal/plugins.BridgeJS. */
const V = 1;

/** The colour scheme a frame is drawn in. */
export type FrameTheme = 'light' | 'dark';

/** What one mount needs from the installed row. Nothing here is derived. */
export interface MountRequest {
  pluginId: string;
  version: string;
  entry: string;
  permissions: string[];
  granted: string[];
}

/** A mounted frame, as the page holds it. */
export interface PluginFrame {
  readonly pluginId: string;
  /** Tells the document the window's scheme changed - WITHOUT remounting it. */
  setTheme(theme: FrameTheme): void;
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
 * Mounts one plugin interface under `mount`.
 *
 * Callers must destroy the previous frame before mounting the next one: the page shows one
 * plugin at a time, and a second document would still be running with permissions the user
 * granted to it.
 */
export function mountPlugin(mount: HTMLElement, req: MountRequest, theme: FrameTheme): PluginFrame {
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
  const granted = [...(req.granted ?? [])];

  const frame = document.createElement('iframe');
  frame.className = 'plugin-frame';
  // Exactly allow-scripts. See the file header; this is AGENTS.md §3.3.
  frame.setAttribute('sandbox', 'allow-scripts');
  // `?theme=` is how the plugin knows which scheme to paint on its FIRST frame; the `theme`
  // message below covers a switch that happens later. Two channels on purpose - see
  // setTheme.
  frame.setAttribute('src', `/plugin-asset/${pluginId}/${entry}?theme=${theme}`);
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
    } catch (err) {
      post({ type: 'response', id, ok: false, error: why(err) });
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
      if (initialised) return;
      initialised = true;
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
        // The slot name is how a plugin with several appearances tells them apart: the pet
        // mounts the same document as 'background' or 'chat', and here it is the workshop.
        surface: { kind: 'panel', slot: 'workshop' },
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

  return {
    pluginId,
    setTheme(next: FrameTheme) {
      current = next === 'light' ? 'light' : 'dark';
      frame.style.colorScheme = current;
      // Nothing to tell a document that has not completed its handshake - its `init` carries
      // the scheme anyway.
      if (initialised) post({ type: 'theme', theme: current });
    },
    destroy() {
      if (!alive) return;
      alive = false;
      window.removeEventListener('message', onMessage);
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
