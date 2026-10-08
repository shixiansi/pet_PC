/**
 * Chat popup: the pet's real-time conversation surface.
 *
 * A self-contained component. It owns its own markup (there is no #live-card,
 * #live-input etc. in index.html - only the mount point), its own geometry, its
 * own streaming state and its own event subscriptions. main.js constructs it,
 * hands it the mount element and two callbacks, and otherwise stays out of it.
 *
 * WHAT IT SHOWS
 *
 * Only the pet's REPLY. Not the user's question, not a title bar, not a record
 * button - the card floats above the pet and the input box sits below it, so the
 * user can already see what they typed. Everything that is not the reply is
 * chrome competing with the reply for a 112px-tall card.
 *
 * This is NOT a second conversation. There is one transcript, persisted by Go,
 * and two views of it:
 *
 *  - the panel's 对话 tab (chat.js) is the RECORD: the whole conversation,
 *    scrollable, with regenerate and clear;
 *  - this popup is the LIVE view: the reply as it streams in.
 *
 * They share the same three events (`pet:chat-delta` / `-done` / `-error`) and
 * the same request ids. Both listen; neither interferes with the other, because
 * both render into their own DOM and Go owns the truth. Closing the popup
 * mid-reply does not stop the reply, and opening the panel afterwards shows it
 * complete.
 *
 * FOUR RULES COME FROM THE APP, NOT THE BROWSER
 *
 *  1. The popup is drawn in THREE separate places (a card, a trail of puffs, an
 *     input box) and Go crops the window to exactly those rectangles. The
 *     geometry therefore comes from the layout and is applied VERBATIM; nothing
 *     here computes a position of its own. A card drawn where no region reaches
 *     is a card the user cannot see.
 *  2. Every one of those rectangles is fixed. The card scrolls internally; it
 *     must never grow, or the drawn box would no longer match the region.
 *  3. Nothing may draw outside its box: no shadow, no filter, no transform that
 *     spills. Outside the box is the user's desktop.
 *  4. A streaming reply is appended to ONE text node kept alive for the whole
 *     reply. Rebuilding the card per delta is what makes a streaming UI stutter -
 *     and, worse here, perturbs the layout while the native region is held.
 */

import * as App from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

/**
 * The component's own markup.
 *
 * It is built here rather than written in index.html so the component is
 * genuinely self-contained: the ids below are private to it, and the static
 * wiring check treats this whole file as the owner of them.
 *
 * Note what is NOT here: a title, a status line, a record button, a close
 * button, or a bubble for the user's question. The card is the reply and only
 * the reply.
 */
const TEMPLATE = `
  <div class="chat-card" data-part="card">
    <div class="chat-reply" data-part="log">
      <p class="chat-hint" data-part="hint">双击我说话</p>
      <div class="chat-say hidden" data-part="say">
        <span data-part="say-text"></span><span class="chat-cursor hidden" data-part="cursor"></span>
      </div>
    </div>
    <span class="chat-tail hidden" data-part="tail"></span>
  </div>

  <div class="chat-puffs" data-part="puffs"></div>

  <form class="chat-bar" data-part="form">
    <textarea data-part="input" rows="1" placeholder="说点什么…（Enter 发送）" maxlength="8000"></textarea>
    <button type="submit" class="primary" data-part="send">发送</button>
  </form>
`;

/**
 * The chat popup component.
 *
 * Constructed with the mount element plus two callbacks into main.js: `onState`
 * (apply a fresh snapshot) and `fail` (report an error the app's way).
 */
export class ChatPopup {
  constructor(mount, { onState, onLayout, onOpenChat, fail, debug }) {
    this.mount = mount;
    this.onState = onState;
    this.fail = fail;
    this.debug = debug || (() => {});

    /**
     * Applies a whole Layout returned by a window call.
     *
     * Kept separate from `applyGeometry` below, which takes only the popup's own
     * rectangles out of a layout: confusing the two means either the popup is
     * positioned from a stale layout or the rest of the window is left unpainted
     * after a close.
     */
    this.applyLayout = onLayout || (() => {});
    /**
     * Opens the panel on the conversation tab.
     *
     * This is the jump to the RECORD, and it lives in main.js because opening the
     * panel is not just a layout change - it also has to switch the panel's tab.
     */
    this.openChat = onOpenChat || (() => {});

    /** Whether the popup is showing, from the layout. */
    this.open = false;
    /**
     * The request id whose reply is being rendered, or null.
     *
     * A local placeholder is used between pressing Enter and SendChat's promise
     * resolving, because the backend starts streaming immediately and its events
     * can beat that round trip (see acceptsEvent).
     */
    this.pendingId = null;
    /** True while a reply is streaming into this view. */
    this.streaming = false;
    /** The text node the streamed reply is appended to. */
    this.sayText = null;

    this.build();
  }

  /** Creates the DOM and caches the elements. Called once, from the constructor. */
  build() {
    this.mount.innerHTML = TEMPLATE;
    const el = (part) => this.mount.querySelector(`[data-part="${part}"]`);
    this.els = {
      card: el('card'),
      log: el('log'),
      hint: el('hint'),
      say: el('say'),
      sayText: el('say-text'),
      cursor: el('cursor'),
      tail: el('tail'),
      puffs: el('puffs'),
      form: el('form'),
      input: el('input'),
      send: el('send'),
    };
    this.wire();
  }

  /** Wires the DOM listeners. Called once, from build(). */
  wire() {
    // Enter sends, Shift+Enter inserts a newline.
    this.els.input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
        e.preventDefault();
        this.send();
      }
    });

    this.els.form.addEventListener('submit', (e) => {
      e.preventDefault();
      this.send();
    });

    this.els.send.addEventListener('click', (e) => {
      // The button doubles as "stop" while a reply is arriving, so it must not
      // submit the form in that state.
      if (this.isStreaming()) {
        e.preventDefault();
        this.stop();
      }
    });

    // Double-clicking the reply card opens the full record. The card has no
    // button chrome, so this is how the record stays reachable without putting a
    // "记录" label in a card that is meant to be nothing but the reply.
    this.els.card.addEventListener('dblclick', () => {
      this.openChat();
    });

    // --- streaming events ---------------------------------------------------
    // The same three events chat.js listens to. Both views render them; this one
    // only cares while it is showing.
    EventsOn('pet:chat-delta', (payload) => this.onDelta(payload));
    EventsOn('pet:chat-done', (payload) => this.onDone(payload));
    EventsOn('pet:chat-error', (payload) => this.onError(payload));
  }

  /**
   * True while a reply is in flight.
   *
   * This is the view's own state rather than the server's `busy`, because the
   * popup may be opened in the middle of a reply started from the panel: in that
   * case there is something to stop, and the button must say so.
   */
  isStreaming() {
    return this.streaming || !!this.pendingId;
  }

  /**
   * Applies the popup's geometry from the layout.
   *
   * `live` is nil when the popup is hidden. Every rectangle is used verbatim:
   * these are the same numbers Go cropped the window to, so a position computed
   * here could only ever disagree with what is visible.
   */
  applyGeometry(live) {
    this.open = !!live;
    this.mount.classList.toggle('hidden', !live);
    if (!live) {
      this.streaming = false;
      this.sayText = null;
      return;
    }

    const place = (el, r) => {
      el.style.left = `${r.x}px`;
      el.style.top = `${r.y}px`;
      el.style.width = `${r.w}px`;
      el.style.height = `${r.h}px`;
    };
    place(this.els.card, live.card);
    place(this.els.form, live.bar);

    // The tail is the card's little pointer at the pet. It lives INSIDE the
    // card's rectangle (along its bottom edge), because anything outside the
    // card's box would be outside the window region and clipped. Its offset comes
    // from Go, which knows where the pet actually is - the card is centred on the
    // pet and then clamped inside the window, so near a screen edge "which half
    // is the pet on" is not enough to place it.
    const puffs = live.puffs || [];
    const hasTail = puffs.length > 0;
    this.els.tail.classList.toggle('hidden', !hasTail);
    if (hasTail) this.els.tail.style.left = `${live.tailX || 0}px`;

    // The puffs are rebuilt only when their count changes; their boxes are then
    // positioned from the layout.
    if (this.els.puffs.childElementCount !== puffs.length) {
      this.els.puffs.replaceChildren();
      for (let i = 0; i < puffs.length; i++) {
        const dot = document.createElement('span');
        dot.className = 'chat-puff';
        this.els.puffs.append(dot);
      }
    }
    puffs.forEach((r, i) => place(this.els.puffs.children[i], r));
  }

  /** Renders the input box's state from a snapshot. */
  applyState(state) {
    if (!state || !this.open) return;
    const view = state.llm;
    const ready = !!view && view.enabled && view.ready && !view.overBudget;
    this.els.input.disabled = !ready;
    this.els.send.disabled = !ready;
    this.els.input.placeholder = this.placeholder(view);

    // The reply is re-synced from the transcript only when nothing is arriving
    // here: a snapshot landing mid-reply must not rip out the streaming node.
    if (!this.isStreaming()) this.syncFromTranscript(state);
  }

  /** The input placeholder, which doubles as the "cannot send" explanation. */
  placeholder(view) {
    if (!view) return '说点什么…';
    if (!view.enabled) return '到「设置」里开启大模型对话';
    if (!view.ready) return view.notReadyReason || '还不能使用';
    if (view.overBudget) return '已达今日上限';
    return '说点什么…（Enter 发送）';
  }

  /**
   * Fills the card from the persisted transcript.
   *
   * So reopening the popup shows the last reply rather than an empty card. A
   * reply the pet started on its own is shown the same way - it is still the
   * last thing it said, and the card has no field to distinguish it in anyway.
   */
  syncFromTranscript(state) {
    const msgs = state?.llm?.messages || [];
    let last = null;
    for (let i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].role === 'assistant' || msgs[i].error) {
        last = msgs[i];
        break;
      }
    }
    if (!last) {
      this.clearReply();
      return;
    }
    this.showReply(last.text, { error: !!last.error });
  }

  /** Empties the card and shows the hint. */
  clearReply() {
    this.els.sayText.textContent = '';
    this.els.say.classList.add('hidden');
    this.els.say.classList.remove('error');
    this.els.cursor.classList.add('hidden');
    this.els.hint.classList.remove('hidden');
  }

  /** Renders the reply, or a placeholder while one is arriving. */
  showReply(text, { error = false, streaming = false } = {}) {
    const hasText = !!text;
    this.els.hint.classList.toggle('hidden', hasText || streaming);
    this.els.say.classList.toggle('hidden', !hasText && !streaming);
    this.els.say.classList.toggle('error', error);
    this.els.sayText.textContent = text;
    this.els.cursor.classList.toggle('hidden', !streaming);
    if (streaming) this.sayText = this.els.sayText;
    this.scrollToEnd();
  }

  /** Starts (or restarts) the streaming node for a reply. */
  beginReply(requestId) {
    this.pendingId = requestId || `local-${Date.now()}`;
    this.streaming = true;
    this.showReply('', { streaming: true });
    this.updateSendButton();
  }

  /**
   * Whether an event carrying `id` belongs to the reply being rendered.
   *
   * A reply starts life with a local placeholder id, because the question is sent
   * the moment the user presses Enter while the backend's real request id only
   * arrives when SendChat's promise resolves. Events can beat that round trip -
   * the backend starts streaming immediately - so the placeholder must ACCEPT the
   * first real id rather than treating it as a stale event from a superseded
   * request. Getting this wrong drops the beginning of every fast reply, and
   * drops a fast failure entirely.
   */
  acceptsEvent(id) {
    if (!id) return true;
    if (!this.pendingId) return true;
    if (this.pendingId === id) return true;
    return this.pendingId.startsWith('local-');
  }

  /** Appends a streamed delta. */
  onDelta(payload) {
    if (!this.open) return;
    const id = payload?.requestId;
    const text = payload?.text;
    if (!text) return;
    if (!this.acceptsEvent(id)) return;

    if (!this.streaming) {
      // A reply that started while the popup was closed (the panel's tab, or the
      // pet speaking on its own): adopt it rather than dropping the text.
      this.beginReply(id);
      if (!this.streaming) return;
    }
    this.els.say.classList.remove('hidden');
    this.els.hint.classList.add('hidden');
    this.els.sayText.textContent += text;
    this.pendingId = id || this.pendingId;
    this.scrollToEnd();
  }

  /** Finalises the reply. */
  onDone(payload) {
    if (!this.open) return;
    const id = payload?.requestId;
    if (!this.acceptsEvent(id)) return;

    // The done event carries the whole reply, which is authoritative: if an event
    // was dropped, this repairs the text rather than leaving it short.
    const full = payload?.text || '';

    if (this.streaming) {
      if (full) this.els.sayText.textContent = full;
      this.showReply(this.els.sayText.textContent || '');
    } else if (full) {
      // Nothing was being streamed here: the reply happened elsewhere, so it is
      // rendered whole.
      this.showReply(full);
    }

    this.streaming = false;
    this.pendingId = null;
    this.sayText = null;
    this.els.cursor.classList.add('hidden');
    this.updateSendButton();

    // The transcript on disk and the token counters changed, so re-read.
    App.GetState().then(this.onState).catch(() => {});
  }

  /** Reports a failure. */
  onError(payload) {
    if (!this.open) return;
    const id = payload?.requestId;
    if (!this.acceptsEvent(id)) return;

    // The partial text stays: the user watched it arrive.
    const message = payload?.message || '请求失败';
    const partial = this.els.sayText.textContent || '';
    this.streaming = false;
    this.pendingId = null;
    this.sayText = null;
    this.els.cursor.classList.add('hidden');
    this.showReply(partial || message, { error: true });
    this.updateSendButton();
    App.GetState().then(this.onState).catch(() => {});
  }

  /** Keeps the newest line visible inside the fixed-height card. */
  scrollToEnd() {
    const el = this.els.log;
    el.scrollTop = el.scrollHeight;
  }

  /** Shows "发送" or "停止" depending on whether a reply is in flight. */
  updateSendButton() {
    const busy = this.isStreaming();
    this.els.send.textContent = busy ? '停止' : '发送';
    this.els.send.classList.toggle('danger', busy);
    this.els.send.classList.toggle('primary', !busy);
  }

  /** Sends whatever is in the input. */
  send() {
    if (this.isStreaming()) {
      this.stop();
      return;
    }
    const text = this.els.input.value.trim();
    if (!text) return;

    // Show an empty streaming reply immediately; the server persists the
    // question and will confirm with the next snapshot, but waiting for that
    // round trip would make the input feel unresponsive.
    this.beginReply(null);
    this.els.input.value = '';

    App.SendChat(text)
      .then((start) => {
        // Adopt the server's id so the streamed events are recognised.
        if (start?.requestId && this.streaming) {
          this.pendingId = start.requestId;
        }
      })
      .catch((err) => {
        // The turn never started, so the optimistic card is a lie: replace it
        // with the reason.
        this.streaming = false;
        this.pendingId = null;
        this.sayText = null;
        this.els.cursor.classList.add('hidden');
        this.showReply(err?.message || String(err), { error: true });
        this.updateSendButton();
      });
  }

  /** Stops the reply that is arriving. */
  stop() {
    const id = this.pendingId || '';
    App.CancelChat(id && !id.startsWith('local-') ? id : '').catch(() => {});
  }

  /** Focuses the input. Only ever called for an explicit user action. */
  focus() {
    if (!this.open) return;
    this.els.input.focus();
  }
}
