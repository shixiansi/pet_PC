/**
 * The pet's BODY as a plugin mount (manifest `body`).
 *
 * What these tests are about is the seam, not the drawing: the host has to hand the pet's
 * rectangle to a sandboxed document exactly when an eligible plugin owns it, take it back the
 * moment that stops being true, and never rebuild the document for a mere resize. The last one
 * is the expensive mistake - a re-created iframe is a destroyed document, so a pet that
 * remounted on every layout push would lose whatever it was doing on every frame of a drag.
 *
 * The Go half of the same contract (a manifest that declares `body` without `pet.surface` is
 * refused) is TestManifestBodyPinsThePetAndItsPermission.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mountApp, baseLayout } from './test-harness.js';

beforeEach(() => {
  document.body.innerHTML = '';
  vi.resetModules();
});

const settle = async () => {
  for (let i = 0; i < 6; i += 1) await new Promise((r) => setTimeout(r, 0));
};

/** One installed row that draws the pet. */
const bodyRow = (over = {}) => ({
  id: 'com.example.pet-body',
  name: '本体',
  version: '1.0.0',
  enabled: true,
  permissions: ['pet.surface'],
  granted: ['pet.surface'],
  pending: [],
  body: true,
  background: false,
  surface: false,
  entry: 'pet.html',
  dir: 'C:\\data\\plugins\\com.example.pet-body',
  ...over,
});

const mountPoint = () => document.getElementById('pet-body');
const pet = () => document.getElementById('pet');
const frame = () => mountPoint().querySelector('iframe');

/** Mounts the page with `rows` installed and a real layout, and lets the reads settle. */
async function withRows(rows, layoutOver = {}) {
  const app = await mountApp({
    handlers: {
      GetLayout: () => baseLayout(layoutOver),
      PluginsInstalled: () => rows,
    },
  });
  await settle();
  return app;
}

describe('the pet drawn by a plugin', () => {
  it('stays the host picture when nobody claims the body', async () => {
    await withRows([bodyRow({ body: false })]);
    expect(mountPoint().classList.contains('hidden')).toBe(true);
    expect(frame()).toBeNull();
    // The bundled <img> is what the user sees, so nothing may hide it.
    expect(pet().classList.contains('body-from-plugin')).toBe(false);
  });

  it('mounts the claimant inside the pet box and steps the picture aside', async () => {
    await withRows([bodyRow()]);
    const el = frame();
    expect(el).not.toBeNull();
    expect(mountPoint().classList.contains('hidden')).toBe(false);
    expect(pet().classList.contains('body-from-plugin')).toBe(true);
    // The frame is the plugin's own file from the host's asset route, and it is sandboxed to
    // scripts only - the same rule every other plugin frame follows.
    expect(el.getAttribute('src')).toBe('/plugin-asset/com.example.pet-body/pet.html?theme=dark');
    expect(el.getAttribute('sandbox')).toBe('allow-scripts');
    expect(el.dataset.pluginId).toBe('com.example.pet-body');
  });

  it('does not mount a plugin that is switched off, or one without the permission', async () => {
    await withRows([bodyRow({ enabled: false })]);
    expect(frame()).toBeNull();
    expect(pet().classList.contains('body-from-plugin')).toBe(false);

    document.body.innerHTML = '';
    vi.resetModules();
    await withRows([bodyRow({ granted: [] })]);
    expect(frame()).toBeNull();
    expect(pet().classList.contains('body-from-plugin')).toBe(false);
  });

  it('keeps the same document when the pet is resized', async () => {
    // The pet's size is the user's setting, and applyLayout runs on every state push - so a
    // resize must reach the mounted document, not replace it.
    const app = await withRows([bodyRow()], { petW: 150, petH: 200 });
    const first = frame();
    expect(first).not.toBeNull();

    app.emit(
      'pet:layout',
      baseLayout({ petW: 300, petH: 400, petX: 20, petY: 60 }),
    );
    await settle();

    expect(frame()).toBe(first);
    expect(pet().classList.contains('body-from-plugin')).toBe(true);
  });

  it('takes the frame away again when the plugin disappears', async () => {
    // The installed list is re-read on a timer (1.5 s), so "uninstalled while the pet was
    // running" is the path that matters: the pet has to get its own picture back without a
    // restart. That wait is the timer's, not this test's - which is why it is the only slow
    // assertion in the file.
    const app = await withRows([bodyRow()]);
    expect(frame()).not.toBeNull();

    app.handlers.PluginsInstalled = () => [];
    await new Promise((r) => setTimeout(r, 1700));
    await settle();

    expect(frame()).toBeNull();
    expect(mountPoint().classList.contains('hidden')).toBe(true);
    expect(pet().classList.contains('body-from-plugin')).toBe(false);
  });
});
