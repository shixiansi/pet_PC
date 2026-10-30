/**
 * Grants change, so the frame has to be rebuilt.
 *
 * `init` carries `granted` exactly once (plugin-dev.md §5: "收到 ready 后**恰好一次**"), and
 * the manual teaches every plugin to gate its own behaviour on it - `if (granted.has('storage'))`,
 * `if (granted.has('pet.surface'))`. Permissions are edited in the CONTROL CENTER, which is a
 * different process, so the pet learns about a new grant only by re-reading plugins.json on its
 * 1.5 s poll.
 *
 * Re-reading the list is not enough: the frames were already mounted, and the check that decides
 * whether to rebuild them used to look at the id and the version only. The real machine showed the
 * consequence - weather-env's headless frame mounted while only `net.fetch` was granted, the user
 * then granted `pet.surface`, and the badge guarded by `granted.has('pet.surface')` never appeared,
 * with no error in any log. The capability gate in Go re-reads the index on every call and would
 * have said yes; the plugin had already decided never to ask.
 *
 * These cases pin the rebuild, on both kinds of frame, and pin the negative: an unchanged set must
 * NOT rebuild, because remounting is destroying a running document.
 */

import { describe, it, expect, beforeEach } from 'vitest';
import { PluginHost, grantedKey } from './plugin-host.js';

const PLUGIN = 'com.example.sensor';

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
  permissions: ['net.fetch', 'pet.surface', 'storage'],
  granted: ['net.fetch'],
  ...over,
});

/** The layout one surface block needs; `granted` travels on the view, not the rects. */
const layout = () => ({
  pluginId: PLUGIN,
  rects: [{ index: 0, x: 0, y: 0, w: 200, h: 80, r: 0 }],
});

beforeEach(() => {
  document.body.innerHTML = '';
  window.runtime = {
    EventsOnMultiple: () => {},
    EventsOff: () => {},
    EventsEmit: () => {},
  };
  window.go = { main: { App: { PluginRequest: () => Promise.resolve('null') } } };
});

describe('grantedKey', () => {
  it('ignores order and treats a missing list as empty', () => {
    expect(grantedKey(['b', 'a'])).toBe(grantedKey(['a', 'b']));
    expect(grantedKey(undefined)).toBe('');
    expect(grantedKey([])).toBe('');
  });
});

describe('a headless frame follows the granted set', () => {
  it('is rebuilt when a permission is added', () => {
    const host = makeHost();
    host.syncBackground([row()]);
    const first = host.backgroundFrames.get(PLUGIN);
    expect(first.granted).toEqual(['net.fetch']);

    host.syncBackground([row({ granted: ['net.fetch', 'pet.surface'] })]);
    const second = host.backgroundFrames.get(PLUGIN);
    expect(second).not.toBe(first);
    // The new frame is handed the new set, which is the whole point: `init` is the only
    // delivery of `granted`, and it happens after the plugin says `ready`.
    expect(second.granted).toEqual(['net.fetch', 'pet.surface']);
  });

  it('is rebuilt when a permission is REMOVED, so the plugin stops trying', () => {
    const host = makeHost();
    host.syncBackground([row({ granted: ['net.fetch', 'pet.surface'] })]);
    const first = host.backgroundFrames.get(PLUGIN);

    host.syncBackground([row({ granted: ['net.fetch'] })]);
    const second = host.backgroundFrames.get(PLUGIN);
    expect(second).not.toBe(first);
    expect(second.granted).toEqual(['net.fetch']);
  });

  it('is left alone when the set is unchanged, including its order', () => {
    const host = makeHost();
    host.syncBackground([row({ granted: ['net.fetch', 'storage'] })]);
    const first = host.backgroundFrames.get(PLUGIN);

    host.syncBackground([row({ granted: ['storage', 'net.fetch'] })]);
    expect(host.backgroundFrames.get(PLUGIN)).toBe(first);
  });

  it('is still left alone between polls, which is what makes the poll affordable', () => {
    const host = makeHost();
    host.syncBackground([row()]);
    const first = host.backgroundFrames.get(PLUGIN);
    host.syncBackground([row()]);
    host.syncBackground([row()]);
    expect(host.backgroundFrames.get(PLUGIN)).toBe(first);
  });
});

describe('a floating surface follows the granted set', () => {
  it('is rebuilt when a permission is added', () => {
    const host = makeHost();
    host.applyGeometry(layout(), row());
    const first = [...host.surfaceFrames.values()][0];
    expect(first.granted).toEqual(['net.fetch']);

    host.applyGeometry(layout(), row({ granted: ['net.fetch', 'pet.surface'] }));
    const second = [...host.surfaceFrames.values()][0];
    expect(second).not.toBe(first);
    expect(second.granted).toEqual(['net.fetch', 'pet.surface']);
  });

  it('is left alone when only the rectangle moved', () => {
    const host = makeHost();
    host.applyGeometry(layout(), row());
    const first = [...host.surfaceFrames.values()][0];

    const moved = layout();
    moved.rects[0].y = 40;
    host.applyGeometry(moved, row());
    const after = [...host.surfaceFrames.values()][0];
    // Same document: moving a box must not destroy what is inside it (a scrolled
    // transcript, a half-typed message). Only the geometry message is sent.
    expect(after).toBe(first);
  });
});
