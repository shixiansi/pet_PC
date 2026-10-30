/**
 * `init.host`: which program this is, and which methods it answers.
 *
 * The two hosts serve different method sets, and until this field existed a plugin could only
 * infer the program from `surface.slot` and then consult a table it hardcoded itself. The cost
 * was measurable: 571 `本程序不支持该方法` lines in one day's logs, half of all WARN lines.
 *
 * Three things are pinned here, and the third is the one that needed care:
 *
 *  - absent is legal: a plugin that ignores `host` behaves exactly as before;
 *  - when set, it is COPIED (a Proxy cannot cross `postMessage`, which this project already paid
 *    for once) and it reaches every `init` from then on;
 *  - a frame that mounted BEFORE the answer arrived still gets it, because `init` is sent when
 *    the PLUGIN says `ready` - not when the frame is created. That ordering is what makes the
 *    startup read cheap enough to not be awaited everywhere.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { PluginHost } from './plugin-host.js';

const PLUGIN = 'com.example.sensor';
const HOST = { program: 'pet', methods: ['bubble.say', 'storage.get'] };

function makeHost() {
  const els = {
    pluginSurface: document.createElement('div'),
    pluginBackground: document.createElement('div'),
  };
  document.body.append(els.pluginSurface, els.pluginBackground);
  return new PluginHost(els, { fail: () => {}, debug: () => {} });
}

const row = (over = {}) => ({
  id: PLUGIN,
  version: '1.0.0',
  entry: 'panel.html',
  permissions: ['storage'],
  granted: ['storage'],
  ...over,
});

/** Mounts a headless frame, silences its transport, and hands back the frame. */
function mountFrame(host) {
  host.syncBackground([row()]);
  const frame = host.backgroundFrames.get(PLUGIN);
  if (!frame) throw new Error('the fixture did not mount a frame');
  frame.post = vi.fn();
  return frame;
}

/** Says `ready` and returns the init the frame posted in answer. */
function handshake(frame) {
  frame.handleMessage({ v: 1, type: 'ready' });
  const init = frame.post.mock.calls.map(([msg]) => msg).find((m) => m && m.type === 'init');
  if (!init) throw new Error('no init was posted');
  return init;
}

beforeEach(() => {
  document.body.innerHTML = '';
  window.runtime = {
    EventsOnMultiple: () => {},
    EventsOff: () => {},
    EventsEmit: () => {},
  };
  window.go = { main: { App: { PluginRequest: () => Promise.resolve('null') } } };
});

describe('init.host', () => {
  it('is omitted when the host has not said what it serves', () => {
    const host = makeHost();
    const init = handshake(mountFrame(host));
    // Absent, not empty: a plugin must be able to tell "no answer" from "serves nothing".
    expect('host' in init).toBe(false);
  });

  it('carries the program name and the method list once the host has answered', () => {
    const host = makeHost();
    host.setHostInfo(HOST);
    const init = handshake(mountFrame(host));
    expect(init.host).toEqual({ program: 'pet', methods: ['bubble.say', 'storage.get'] });
  });

  it('copies the list, so a caller cannot mutate what a plugin was told', () => {
    const host = makeHost();
    const info = { program: 'pet', methods: ['storage.get'] };
    host.setHostInfo(info);
    info.methods.push('llm.chat');
    const init = handshake(mountFrame(host));
    expect(init.host.methods).toEqual(['storage.get']);
    expect(init.host.methods).not.toBe(info.methods);
  });

  it('reaches a frame that was mounted BEFORE the answer arrived', () => {
    const host = makeHost();
    // The race this closes: the frame exists, its document is still loading, so `ready` has not
    // been posted yet. `setHostInfo` has to update the frame that is already there, or that
    // plugin - the very first one mounted after launch - never learns what the host serves.
    const frame = mountFrame(host);
    host.setHostInfo(HOST);
    expect(handshake(frame).host).toEqual({ program: 'pet', methods: ['bubble.say', 'storage.get'] });
  });

  it('refuses a malformed answer rather than forwarding it', () => {
    const host = makeHost();
    host.setHostInfo({ program: 'pet' }); // no methods
    expect('host' in handshake(mountFrame(host))).toBe(false);
    host.setHostInfo(null);
    expect(host.host).toBe(null);
  });

  it('applies to surface frames too, not just headless ones', () => {
    const host = makeHost();
    host.setHostInfo(HOST);
    host.applyGeometry(
      { pluginId: PLUGIN, rects: [{ index: 0, x: 0, y: 0, w: 200, h: 80, r: 0 }] },
      row(),
    );
    const frame = [...host.surfaceFrames.values()][0];
    frame.post = vi.fn();
    expect(handshake(frame).host.program).toBe('pet');
  });
});
