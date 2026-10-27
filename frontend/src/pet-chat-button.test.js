/**
 * The conversation button: hover reveals it, a click opens the 对话 slot's
 * plugin surface.
 *
 * This is the gesture the DOUBLE CLICK on the pet used to be, and the assertions
 * are split the same way the behaviour is:
 *
 *  - the markup half pins that the button lives INSIDE #pet's box. That is not
 *    cosmetic: while the panel and the bubble are away the window is cropped to
 *    exactly the pet's rectangle (AGENTS.md 3.2), so a button hanging off the pet's
 *    edge would be sliced by the native region - or, if the region were grown to
 *    fit it, the strip beside the pet would be window the page never paints.
 *
 *  - the stylesheet half pins that it is invisible AND inert until the pointer is
 *    on the pet. A transparent button that still takes pointer events is the one
 *    regression that would silently change what a click on the pet does.
 *
 *  - the behaviour half drives the real page: pressing the button must not start a
 *    drag, clicking it must ask Go for the surface exactly once, and a click on the
 *    PET must toggle the panel at once - the 200ms wait only existed to keep a
 *    double click from unwinding as click-click, and there is no double click any
 *    more.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { mountApp, callsTo } from './test-harness.js';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');

const css = readFileSync(path.join(root, 'src', 'style.css'), 'utf8');
const mainJs = readFileSync(path.join(root, 'src', 'main.js'), 'utf8');

const $ = (id) => document.getElementById(id);
const tick = () => new Promise((r) => setTimeout(r, 0));

/** A pointer event on `target`. jsdom has no PointerEvent, and the handlers only
 * read the fields a MouseEvent already carries. */
function pointer(target, type, x, y) {
  target.dispatchEvent(
    new MouseEvent(type, { bubbles: true, cancelable: true, screenX: x, screenY: y, button: 0 }),
  );
}

/** The CSS block belonging to one selector, so an assertion cannot borrow a
 * declaration from a neighbouring rule. */
function rule(selector) {
  const start = css.indexOf(selector);
  if (start < 0) return '';
  const open = css.indexOf('{', start);
  const close = css.indexOf('}', open);
  return open < 0 || close < 0 ? '' : css.slice(open + 1, close);
}

/**
 * The app installs an Escape handler on `document`, which outlives the body the
 * harness replaces between tests; a stale one would answer a later test's key.
 */
const nativeAdd = document.addEventListener.bind(document);
const nativeRemove = document.removeEventListener.bind(document);
let docKeydowns = [];
document.addEventListener = (type, fn, opts) => {
  if (type === 'keydown') docKeydowns.push(fn);
  return nativeAdd(type, fn, opts);
};

function resetPage() {
  for (const fn of docKeydowns) nativeRemove('keydown', fn);
  docKeydowns = [];
  document.body.innerHTML = '';
  vi.resetModules();
}

afterEach(resetPage);

describe('pet-chat: the button stays inside the rectangle the window is cropped to', () => {
  beforeEach(resetPage);

  it('is a child of #pet, not a box beside it', async () => {
    await mountApp();
    const pet = $('pet');
    const button = $('pet-chat');
    expect(pet).toBeTruthy();
    expect(button).toBeTruthy();
    expect(pet.contains(button)).toBe(true);
  });

  it('is a real button with an accessible name', async () => {
    await mountApp();
    const button = $('pet-chat');
    expect(button.tagName).toBe('BUTTON');
    expect(button.getAttribute('type')).toBe('button');
    expect(button.getAttribute('aria-label')).toBe('对话');
    expect(button.getAttribute('title')).toBe('对话');
  });

  it('the glyph cannot take the click away from the button', () => {
    expect(rule('.pet-chat-icon')).toMatch(/pointer-events:\s*none/);
  });
});

describe('pet-chat: painted by hover, and only then', () => {
  beforeEach(resetPage);

  it('is transparent AND inert by default', () => {
    const base = rule('.pet-chat');
    expect(base).toMatch(/opacity:\s*0/);
    expect(base).toMatch(/visibility:\s*hidden/);
    expect(base).toMatch(/pointer-events:\s*none/);
  });

  it('the pet reveals it on hover and hands it the pointer', () => {
    expect(css).toMatch(/#pet:hover\s+\.pet-chat/);
    const hover = css.slice(css.indexOf('#pet:hover .pet-chat'));
    expect(hover).toMatch(/pointer-events:\s*auto/);
    expect(hover).toMatch(/visibility:\s*visible/);
  });

  it('an anchored pet (panel open) does not offer it', () => {
    expect(rule('#pet.anchored .pet-chat')).toMatch(/visibility:\s*hidden/);
    expect(rule('#pet.anchored .pet-chat')).toMatch(/pointer-events:\s*none/);
  });

  it('it moves by opacity alone, so nothing is drawn outside the box mid-animation', () => {
    expect(rule('.pet-chat')).toMatch(/transition:\s*opacity/);
    expect(rule('.pet-chat')).not.toMatch(/transform/);
  });
});

describe('pet-chat: a click opens the conversation, a press starts no drag', () => {
  beforeEach(resetPage);

  it('asks Go for the plugin surface exactly once, and toggles nothing', async () => {
    const { calls } = await mountApp();
    $('pet-chat').dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    await tick();

    expect(callsTo(calls, 'OpenChatPluginSurface').length).toBe(1);
    expect(callsTo(calls, 'TogglePanel').length).toBe(0);
  });

  it('the press never reaches the pet, so no drag begins', async () => {
    const { calls } = await mountApp();
    const button = $('pet-chat');
    pointer(button, 'pointerdown', 10, 10);
    pointer(button, 'pointerup', 10, 10);
    button.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    await tick();

    expect(callsTo(calls, 'StartDrag').length).toBe(0);
    expect(callsTo(calls, 'TogglePanel').length).toBe(0);
    expect(callsTo(calls, 'OpenChatPluginSurface').length).toBe(1);
  });
});

describe('pet-chat: a click on the pet toggles the panel at once', () => {
  beforeEach(resetPage);

  it('acts on the first click, without waiting for a second one', async () => {
    const { calls } = await mountApp();
    const pet = $('pet');
    pointer(pet, 'pointerdown', 40, 40);
    pointer(pet, 'pointerup', 40, 40);
    await tick();

    expect(callsTo(calls, 'TogglePanel').length).toBe(1);
    expect(callsTo(calls, 'OpenChatPluginSurface').length).toBe(0);
  });

  it('a double click is two toggles now, never a chat surface', async () => {
    const { calls } = await mountApp();
    const pet = $('pet');
    for (let i = 0; i < 2; i += 1) {
      pointer(pet, 'pointerdown', 40, 40);
      pointer(pet, 'pointerup', 40, 40);
    }
    await tick();
    expect(callsTo(calls, 'TogglePanel').length).toBe(2);
    expect(callsTo(calls, 'OpenChatPluginSurface').length).toBe(0);
  });

  it('the click/double-click arbitration is gone from main.js', () => {
    expect(mainJs).not.toMatch(/DOUBLE_CLICK_MS|pendingClick|lastClickAt/);
  });
});
