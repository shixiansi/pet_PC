/**
 * Chat tab: the LLM conversation, its streaming renderer and its settings.
 *
 * It lives in its own module rather than in main.js because the streaming
 * renderer has state of its own (a reply arriving over hundreds of events) and
 * mixing that into the panel's render pass would mean every delta re-ran the
 * todo and record renderers.
 *
 * The rules it must respect are the app's, not the browser's:
 *
 *  - The panel's rectangle is fixed and the window is cropped to it, so nothing
 *    here may change the panel's size or draw outside it. Everything that
 *    scrolls scrolls inside `#chat-log`, which has a fixed flex basis.
 *  - The reply arrives as many small events. They are appended to ONE text node
 *    that is kept alive for the whole reply, because rebuilding the list per
 *    delta is what makes a streaming UI stutter - and, worse here, perturb the
 *    panel's layout while the native region is being held.
 */

import * as App from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

/** Formats an RFC3339 stamp as HH:MM for the message footer. */
function clockTime(iso) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n) => String(n).padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/**
 * ChatTab owns the conversation UI.
 *
 * It is constructed with the elements it drives plus two callbacks into
 * main.js: `onState` (apply a fresh snapshot) and `fail` (report an error the
 * same way the rest of the app does).
 */
export class ChatTab {
  constructor(els, { onState, fail, debug }) {
    this.els = els;
    this.onState = onState;
    this.fail = fail;
    this.debug = debug || (() => {});

    /** Latest LLM snapshot from the server. */
    this.view = null;
    /** The transcript as rendered, kept so a delta can be appended in place. */
    this.messages = [];
    /** The <li> of the reply currently arriving, or null. */
    this.pending = null;
    /** The text node the pending reply is appended to. */
    this.pendingText = null;
    /** Request id of the reply arriving, so late events can be recognised. */
    this.pendingId = null;
    /** Reasoning collected for the pending reply. */
    this.reasoning = '';
    /** Whether the user is pinned to the bottom of the log. */
    this.stick = true;
  }

  /** Wires the DOM listeners. Called once, from main.js's wire(). */
  wire() {
    this.els.form.addEventListener('submit', (e) => {
      e.preventDefault();
      this.send();
    });

    // Enter sends, Shift+Enter inserts a newline. A textarea is used rather than
    // an input so a multi-line question is possible at all.
    this.els.input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
        e.preventDefault();
        this.send();
      }
    });

    this.els.send.addEventListener('click', (e) => {
      // The button doubles as "stop" while a reply is arriving, so it must not
      // submit the form in that state.
      if (this.isBusy()) {
        e.preventDefault();
        this.stop();
      }
    });

    this.els.regen.addEventListener('click', () => {
      App.Regenerate()
        .then((start) => {
          this.beginReply(start?.requestId);
          // The transcript changed shape (the old answer is gone), so re-read
          // rather than patching the list.
          return App.GetState();
        })
        .then(this.onState)
        .catch(this.fail);
    });

    this.els.clear.addEventListener('click', () => {
      App.ClearChat().then(this.onState).catch(this.fail);
    });

    // Only auto-scroll while the user is already at the bottom; otherwise a long
    // reply would yank the view away from whatever they scrolled back to read.
    this.els.log.addEventListener('scroll', () => {
      const el = this.els.log;
      this.stick = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    });

    // --- streaming events ---------------------------------------------------
    EventsOn('pet:chat-delta', (payload) => {
      this.onDelta(payload);
    });
    EventsOn('pet:chat-done', (payload) => {
      this.onDone(payload);
    });
    EventsOn('pet:chat-error', (payload) => {
      this.onError(payload);
    });
  }

  /** Renders the LLM part of a fresh state snapshot. */
  applyState(state) {
    if (!state) return;
    const view = state.llm;
    if (!view) return;
    this.view = view;
    this.messages = view.messages || [];

    // A snapshot arriving mid-reply (a periodic refresh, say) must not rip out
    // the streaming node; the pending reply is re-attached instead.
    const wasPending = this.pendingId;
    this.renderMessages();
    if (wasPending && view.busy) {
      const last = this.messages[this.messages.length - 1];
      this.beginReply(wasPending, last ? last.text : '');
    }
    this.renderStatus();
  }

  /** Renders the settings section from the same snapshot. */
  applySettings(state) {
    const els = this.els;
    const cfg = state?.config || {};
    const view = state?.llm;
    if (!view) return;

    // The provider list is data, so it is rebuilt from the server's presets
    // rather than hard-coded here.
    if (els.provider.options.length !== (view.providers || []).length) {
      els.provider.replaceChildren();
      for (const p of view.providers || []) {
        const opt = document.createElement('option');
        opt.value = p.id;
        opt.textContent = p.label;
        els.provider.append(opt);
      }
    }
    els.provider.value = cfg.llmProvider || 'deepseek';

    els.base.value = cfg.llmBaseUrl || '';
    els.model.value = cfg.llmModel || '';
    els.temp.value = String(cfg.llmTemperature ?? 0.8);
    els.max.value = String(cfg.llmMaxTokens || 800);
    els.timeout.value = String(cfg.llmTimeoutSec || 90);
    els.history.value = String(cfg.llmHistoryTurns ?? 12);
    els.budget.value = String(cfg.llmDailyBudget || 200);
    els.prompt.value = cfg.llmSystemPrompt || '';
    els.enabled.checked = !!cfg.llmEnabled;
    els.context.checked = cfg.llmContextTodos !== false;

    this.renderProviderHint();
    this.renderKeyStatus();
    this.renderUsage();
  }

  /** Shows the selected preset's explanation. */
  renderProviderHint() {
    const view = this.view;
    if (!view) return;
    const preset = (view.providers || []).find((p) => p.id === this.els.provider.value);
    this.els.providerHint.textContent = preset?.hint || '';
  }

  /**
   * Renders where the API key comes from.
   *
   * The panel never shows the key itself, only a mask. An env-supplied key is
   * reported as such and cannot be deleted from here, which has to be said out
   * loud or the delete button looks broken.
   */
  renderKeyStatus() {
    const view = this.view;
    const el = this.els.keyStatus;
    el.classList.remove('ok', 'error');
    if (!view.hasKey) {
      el.textContent = view.needsKey
        ? '还没有密钥。粘贴后点「保存密钥」（会加密存放）。'
        : '该服务不需要密钥。';
      return;
    }
    if (view.keySource === 'env') {
      el.textContent = `正在使用环境变量 PETPC_LLM_API_KEY（${view.keyMask}），它的优先级高于这里保存的密钥，且无法从这里删除。`;
      return;
    }
    el.textContent = `已保存密钥：${view.keyMask}`;
    el.classList.add('ok');
  }

  /** Renders today's usage against the budget. */
  renderUsage() {
    const view = this.view;
    const el = this.els.usage;
    if (!view) return;
    const u = view.usage || {};
    const parts = [view.usageLine || ''];
    if (u.promptTokens || u.completionTokens) {
      parts.push(`输入 ${u.promptTokens || 0} / 输出 ${u.completionTokens || 0} tokens`);
    }
    el.textContent = parts.filter(Boolean).join(' · ');
    el.classList.toggle('error', !!view.overBudget);
    el.classList.toggle('ok', !view.overBudget);
  }

  /** True while a reply is arriving. */
  isBusy() {
    return !!this.pendingId || !!this.view?.busy;
  }

  /** Renders the one-line status above the log. */
  renderStatus() {
    const view = this.view;
    const el = this.els.status;
    el.classList.remove('busy', 'error');
    if (!view) return;
    if (this.pendingId) {
      el.textContent = '正在回答…';
      el.classList.add('busy');
      return;
    }
    if (!view.enabled) {
      el.textContent = '未开启 — 到「设置」里开启大模型对话';
      return;
    }
    if (!view.ready) {
      el.textContent = view.notReadyReason || '还不能使用';
      el.classList.add('error');
      return;
    }
    el.textContent = view.overBudget ? '已达今日上限' : '';
  }

  // --- rendering -----------------------------------------------------------

  /** Rebuilds the whole message list from the snapshot. */
  renderMessages() {
    const list = this.els.list;
    list.replaceChildren();
    this.pending = null;
    this.pendingText = null;

    for (const m of this.messages) {
      list.append(this.buildMessage(m.role, m.text, { at: m.at, error: m.error }));
    }
    this.els.empty.classList.toggle('hidden', this.messages.length > 0);
    this.scrollToEnd(true);
  }

  /**
   * Builds one message element and returns it.
   *
   * `role` is the server's role string: "user", "assistant" or "system" (the
   * last is used for the pet's own error notes, which must not read as something
   * it said).
   */
  buildMessage(role, text, { at = '', error = false, streaming = false } = {}) {
    const li = document.createElement('li');
    li.className = `chat-msg ${role}${error ? ' error' : ''}`;

    const bubble = document.createElement('div');
    bubble.className = 'chat-bubble';
    const body = document.createElement('span');
    body.textContent = text;
    bubble.append(body);

    if (streaming) {
      const cursor = document.createElement('span');
      cursor.className = 'chat-cursor';
      bubble.append(cursor);
      this.pendingText = body;
    }

    li.append(bubble);
    if (at) {
      const time = document.createElement('div');
      time.className = 'chat-time';
      time.textContent = clockTime(at);
      li.append(time);
    }
    return li;
  }

  /**
   * Starts (or restarts) the streaming node for a reply.
   *
   * `seed` is any text the server already reported for this reply, so a re-render
   * that happens mid-stream does not lose what has arrived.
   */
  beginReply(requestId, seed = '') {
    if (!requestId) {
      requestId = `local-${Date.now()}`;
    }
    if (this.pendingId === requestId && this.pending) return;

    this.pendingId = requestId;
    this.reasoning = '';
    const li = this.buildMessage('assistant', seed, { streaming: true });
    this.els.list.append(li);
    this.pending = li;
    this.els.empty.classList.add('hidden');
    this.renderStatus();
    this.updateSendButton();
    this.scrollToEnd();
  }

  /**
   * Whether an event carrying `id` belongs to the reply being rendered.
   *
   * A pending reply starts life with a local placeholder id, because the
   * question is shown the moment the user presses Enter while the backend's real
   * request id only arrives when SendChat's promise resolves. Events can beat
   * that round trip - the backend starts streaming immediately - so a
   * placeholder must accept the first real id rather than treating it as a stale
   * event from a superseded request. Getting this wrong drops the beginning of
   * every fast reply, and drops a fast failure entirely.
   */
  acceptsEvent(id) {
    if (!id) return true;
    if (!this.pendingId) return true;
    if (this.pendingId === id) return true;
    return this.pendingId.startsWith('local-');
  }

  /** Appends a streamed delta to the pending reply. */
  onDelta(payload) {
    const id = payload?.requestId;
    const text = payload?.text;
    if (!text) return;
    if (!this.acceptsEvent(id)) return;

    if (!this.pending) {
      // A reply that started while the panel was closed: adopt it rather than
      // dropping the text.
      this.beginReply(id, this.lastAssistantText());
      if (!this.pending) return;
    }
    if (this.pendingText) {
      this.pendingText.textContent += text;
    }
    this.pendingId = id || this.pendingId;
    this.scrollToEnd();
  }


  /** Finalises the pending reply. */
  onDone(payload) {
    const id = payload?.requestId;
    if (!this.acceptsEvent(id)) return;

    if (this.pending && this.pendingText) {
      // The done event carries the whole reply, which is authoritative: if an event
      // was dropped, this repairs the text rather than leaving it short.
      const full = payload?.text || '';
      if (full) this.pendingText.textContent = full;
      this.pending.querySelector('.chat-cursor')?.remove();
    } else if (payload?.text) {
      this.els.list.append(this.buildMessage('assistant', payload.text, { at: new Date().toISOString() }));
      this.els.empty.classList.add('hidden');
    }

    this.pendingId = null;
    this.pending = null;
    this.pendingText = null;
    this.renderStatus();
    this.updateSendButton();
    this.scrollToEnd();

    // The tokens and the transcript on disk changed, so take a fresh snapshot.
    App.GetState().then(this.onState).catch(() => {});
  }

  /** Turns a failure into a system note. */
  onError(payload) {
    const id = payload?.requestId;
    if (!this.acceptsEvent(id)) return;

    // The partial text stays: the user watched it arrive.
    if (this.pending?.querySelector('.chat-cursor')) {
      this.pending.querySelector('.chat-cursor').remove();
    }
    const message = payload?.message || '请求失败';
    this.els.list.append(this.buildMessage('system', message, { error: true }));
    this.pendingId = null;
    this.pending = null;
    this.pendingText = null;
    this.renderStatus();
    this.updateSendButton();
    this.scrollToEnd();
    App.GetState().then(this.onState).catch(() => {});
  }

  /** The last assistant text in the snapshot, used when adopting a live reply. */
  lastAssistantText() {
    for (let i = this.messages.length - 1; i >= 0; i--) {
      if (this.messages[i].role === 'assistant') return this.messages[i].text;
    }
    return '';
  }

  /** Keeps the newest message visible. */
  scrollToEnd(force = false) {
    if (!force && !this.stick) return;
    const el = this.els.log;
    el.scrollTop = el.scrollHeight;
    if (force) this.stick = true;
  }

  /** Shows "发送" or "停止" depending on whether a reply is in flight. */
  updateSendButton() {
    const busy = this.isBusy();
    this.els.send.textContent = busy ? '停止' : '发送';
    this.els.send.classList.toggle('danger', busy);
    this.els.send.classList.toggle('primary', !busy);
  }

  // --- actions -------------------------------------------------------------

  /** Sends whatever is in the input. */
  send() {
    if (this.isBusy()) {
      this.stop();
      return;
    }
    const text = this.els.input.value.trim();
    if (!text) return;

    // Show the question immediately; the server persists it and will confirm
    // with the next snapshot, but waiting for that round trip would make the
    // input feel unresponsive.
    this.els.list.append(
      this.buildMessage('user', text, { at: new Date().toISOString() }),
    );
    this.els.empty.classList.add('hidden');
    this.els.input.value = '';
    this.beginReply(null);
    this.scrollToEnd(true);

    App.SendChat(text)
      .then((start) => {
        // Adopt the server's id so the streamed events are recognised.
        if (start?.requestId && this.pending) {
          this.pendingId = start.requestId;
        }
        this.renderStatus();
      })
      .catch((err) => {
        // The turn never started, so the optimistic bubble is a lie: replace it
        // with the reason.
        this.pending?.remove();
        this.pending = null;
        this.pendingText = null;
        this.pendingId = null;
        this.els.list.append(
          this.buildMessage('system', err?.message || String(err), { error: true }),
        );
        this.renderStatus();
        this.updateSendButton();
        this.scrollToEnd();
      });
  }

  /** Stops the reply that is arriving. */
  stop() {
    const id = this.pendingId || '';
    App.CancelChat(id && !id.startsWith('local-') ? id : '')
      .catch(() => {});
    this.renderStatus();
  }
}
