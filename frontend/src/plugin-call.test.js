/**
 * `plugin.call`: the page's half of the primitive.
 *
 * Go authorises a call and answers with an envelope; this page delivers it to another
 * plugin's frame and settles the caller's promise with that plugin's answer. The division
 * is forced by the platform - Wails gives Go no way to ask a document a question and wait -
 * so what these cases pin is exactly the part Go cannot check:
 *
 *  - an answer is accepted only from the frame that was asked, and only for a ticket that
 *    is still waiting;
 *  - a target with no frame is refused AT ONCE with a sentence that says so, rather than
 *    sitting through the timeout;
 *  - a call that dies with its target's frame fails immediately;
 *  - the chain depth and the timeout stop the two failure modes a mailbox never had to
 *    think about: two plugins asking each other forever, and an answer that never comes.
 *
 * The fake here is the Wails bridge, not the protocol: `window.go.main.App.PluginRequest`
 * is the real binding's name, and the assertions are about the messages that reach a
 * plugin's frame.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { PluginHost } from './plugin-host.js';

const CALLER = 'com.example.adapter';
const CORE = 'com.example.core';

/** Builds a host with one background frame for CORE, ready to be asked. */
function makeHost() {
  const els = {
    pluginSurface: document.createElement('div'),
    pluginBackground: document.createElement('div'),
  };
  document.body.append(els.pluginSurface, els.pluginBackground);
  const host = new PluginHost(els, { fail: () => {}, debug: () => {} });
  host.syncBackground([{
    id: CORE,
    version: '1.0.0',
    entry: 'panel.html',
    permissions: ['plugin.call'],
    granted: ['plugin.call'],
  }]);
  const frame = host.backgroundFrames.get(CORE);
  if (!frame) throw new Error('the fixture did not mount the callee frame');
  // Nothing is listening inside a jsdom iframe, so the outgoing message is captured here
  // and the answer is delivered back through the host's own message entry point - which is
  // the same path a real plugin's `call-result` takes.
  frame.post = vi.fn();
  return { host, frame };
}

/** A Wails bridge that answers `plugin.call` with the envelope Go would mint. */
function bridgeEnvelope(envelope) {
  window.go = {
    main: {
      App: {
        PluginRequest: vi.fn(() => Promise.resolve(JSON.stringify(envelope))),
      },
    },
  };
}

const envelope = (over = {}) => ({
  ticket: 'a'.repeat(32),
  target: CORE,
  method: 'chat',
  payload: { text: '在吗' },
  ...over,
});

/**
 * Lets the host's promise chain run to the point where it has POSTED to a frame.
 *
 * A macrotask rather than `await Promise.resolve()` twice: the chain is
 * `App.PluginRequest` (a promise the bridge resolves) → `.then` → deliverCall, and the
 * number of microtask ticks that takes is not something a test should be counting.
 */
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

beforeEach(() => {
  document.body.innerHTML = '';
  window.runtime = {
    EventsOnMultiple: () => {},
    EventsOff: () => {},
    EventsEmit: () => {},
  };
  window.go = { main: { App: { PluginRequest: () => Promise.resolve('null') } } };
});

describe('delivering an authorised call', () => {
  it('posts the question to the callee and settles with its answer', async () => {
    const { host, frame } = makeHost();
    bridgeEnvelope(envelope());

    const answer = host.request(CALLER, 'plugin.call', {
      target: CORE, method: 'chat', payload: { text: '在吗' },
    });
    await flush();

    const sent = frame.post.mock.calls[0][0];
    expect(sent.type).toBe('call');
    expect(sent.ticket).toBe('a'.repeat(32));
    expect(sent.from).toBe(CALLER);
    expect(sent.method).toBe('chat');
    expect(sent.payload).toEqual({ text: '在吗' });

    host.onMessage({
      source: frame.el.contentWindow,
      data: { v: 1, type: 'call-result', ticket: sent.ticket, ok: true, result: { text: '在' } },
    });

    await expect(answer).resolves.toEqual({ text: '在' });
  });

  it('passes a callee refusal through as the caller’s error', async () => {
    const { host, frame } = makeHost();
    bridgeEnvelope(envelope());

    const answer = host.request(CALLER, 'plugin.call', { target: CORE, method: 'chat', payload: {} });
    await flush();

    host.onMessage({
      source: frame.el.contentWindow,
      data: { v: 1, type: 'call-result', ticket: 'a'.repeat(32), ok: false, error: '模型没配置' },
    });

    await expect(answer).rejects.toThrow('模型没配置');
  });

  it('ignores an answer from a frame that was not asked', async () => {
    const { host, frame } = makeHost();
    bridgeEnvelope(envelope());

    const answer = host.request(CALLER, 'plugin.call', { target: CORE, method: 'chat', payload: {} });
    await flush();

    // A DIFFERENT window claims the ticket. It is dropped without settling anything.
    const impostor = document.createElement('iframe');
    document.body.append(impostor);
    host.onMessage({
      source: impostor.contentWindow,
      data: { v: 1, type: 'call-result', ticket: 'a'.repeat(32), ok: true, result: '假的' },
    });
    expect(host.pendingCalls.size).toBe(1);

    host.onMessage({
      source: frame.el.contentWindow,
      data: { v: 1, type: 'call-result', ticket: 'a'.repeat(32), ok: true, result: '真的' },
    });
    await expect(answer).resolves.toBe('真的');
  });

  it('refuses immediately when the target has no frame at all', async () => {
    const { host } = makeHost();
    bridgeEnvelope(envelope({ target: 'com.example.absent' }));

    await expect(
      host.request(CALLER, 'plugin.call', { target: 'com.example.absent', method: 'chat', payload: {} }),
    ).rejects.toThrow(/没有常驻帧/);
  });

  it('fails a waiting call when the callee frame goes away', async () => {
    const { host } = makeHost();
    bridgeEnvelope(envelope());

    const answer = host.request(CALLER, 'plugin.call', { target: CORE, method: 'chat', payload: {} });
    await flush();

    host.closeBackground(CORE);
    await expect(answer).rejects.toThrow(/常驻帧已关闭/);
    expect(host.pendingCalls.size).toBe(0);
  });

  it('refuses a chain deeper than the cap', async () => {
    const { host } = makeHost();
    bridgeEnvelope(envelope());
    // CORE is already serving three calls, so asking it again would be hop four.
    host.callDepth.set(CALLER, 3);

    await expect(
      host.request(CALLER, 'plugin.call', { target: CORE, method: 'chat', payload: {} }),
    ).rejects.toThrow(/调用链已到/);
  });

  it('refuses an answer larger than the cap rather than truncating it', async () => {
    const { host, frame } = makeHost();
    bridgeEnvelope(envelope());

    const answer = host.request(CALLER, 'plugin.call', { target: CORE, method: 'chat', payload: {} });
    await flush();

    host.onMessage({
      source: frame.el.contentWindow,
      data: { v: 1, type: 'call-result', ticket: 'a'.repeat(32), ok: true, result: 'x'.repeat(70000) },
    });

    await expect(answer).rejects.toThrow(/上限/);
  });

  it('times out with a sentence that names the pair', async () => {
    vi.useFakeTimers();
    try {
      const { host } = makeHost();
      bridgeEnvelope(envelope());

      const answer = host.request(CALLER, 'plugin.call', { target: CORE, method: 'chat', payload: {} });
      // The rejection has to be observed before the timer fires, or the failure surfaces as
      // an unhandled rejection instead of as this assertion.
      const expectation = expect(answer).rejects.toThrow(/超时/);
      // Delivery first (that is what registers the timer), then the clock.
      await vi.advanceTimersByTimeAsync(0);
      expect(host.pendingCalls.size).toBe(1);
      await vi.advanceTimersByTimeAsync(30_000);
      await expectation;
      expect(host.pendingCalls.size).toBe(0);
      expect(host.callDepth.size).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });

  it('leaves ordinary methods exactly as they were', async () => {
    const { host, frame } = makeHost();
    window.go.main.App.PluginRequest = vi.fn(() => Promise.resolve(JSON.stringify({ value: 'hello' })));

    await expect(host.request(CORE, 'share.get', { name: 'k' })).resolves.toEqual({ value: 'hello' });
    expect(frame.post).not.toHaveBeenCalled();
  });
});
