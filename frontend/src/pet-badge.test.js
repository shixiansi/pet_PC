/**
 * The pet's corner badge: the number of open todos, drawn from the TODO PLUGIN's snapshot.
 *
 * WHY THIS FILE EXISTS. The badge once rendered the literal text
 * `function open() { [native code] }` on the pet's shoulder, and every gate was green. The cause was
 * a comment that had swallowed the declaration below it (`\n` written as literal backslash-n), so
 * `open` was never declared and resolved to the GLOBAL `window.open`; `String(open)` then printed the
 * function. Nothing caught it because the jsdom fixture did not carry `todoBadge` at all, so the
 * badge was simply hidden and there was nothing to assert.
 *
 * So these tests do two things the old ones could not: they hand the page a snapshot, and they assert
 * the rendered text is a NUMBER. The second assertion is the one that would have failed loudly.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mountApp, baseState } from './test-harness.js';

const withBadge = (badge) => baseState({ todoBadge: badge });

// The app is a MODULE that boots once at import, so a second `mountApp` in the same file would reuse
// the first test's state and assert against the wrong snapshot. Resetting the module registry (and
// the document) is what makes each case independent - without it the odd-numbered cases passed and
// the even ones saw the previous state.
beforeEach(() => {
  document.body.innerHTML = '';
  vi.resetModules();
});

describe('the corner badge', () => {
  it('draws the number of open entries', async () => {
    await mountApp({ state: withBadge({ open: 3, overdue: 0, dueSoon: 0 }) });
    const badge = document.getElementById('badge');
    expect(badge.classList.contains('hidden')).toBe(false);
    expect(badge.textContent).toBe('3');
  });

  it('never prints a function - the failure that shipped', async () => {
    await mountApp({ state: withBadge({ open: 2, overdue: 1, dueSoon: 0 }) });
    const badge = document.getElementById('badge');
    // A bare `open` in that function used to resolve to `window.open`, and this is what came out.
    expect(badge.textContent).not.toMatch(/function|native|=>/);
    expect(badge.textContent).toMatch(/^\d+$/);
  });

  it('hides itself when nothing is open', async () => {
    await mountApp({ state: withBadge({ open: 0, overdue: 0, dueSoon: 0 }) });
    expect(document.getElementById('badge').classList.contains('hidden')).toBe(true);
  });

  it('marks an overdue entry, and prefers that over "due soon"', async () => {
    await mountApp({ state: withBadge({ open: 2, overdue: 1, dueSoon: 2 }) });
    const badge = document.getElementById('badge');
    expect(badge.classList.contains('overdue')).toBe(true);
    // Both cannot colour it: overdue wins, because it is the one that needs acting on.
    expect(badge.classList.contains('soon')).toBe(false);
    expect(badge.title).toContain('逾期');
  });

  it('marks a due-soon entry when nothing is overdue', async () => {
    await mountApp({ state: withBadge({ open: 2, overdue: 0, dueSoon: 2 }) });
    const badge = document.getElementById('badge');
    expect(badge.classList.contains('soon')).toBe(true);
    expect(badge.classList.contains('overdue')).toBe(false);
  });

  it('survives a snapshot that omits a counter', async () => {
    // Defaulted field by field, so a partial object cannot turn into `undefined > 0` and silently
    // lose the badge's colour.
    await mountApp({ state: baseState({ todoBadge: { open: 4 } }) });
    const badge = document.getElementById('badge');
    expect(badge.textContent).toBe('4');
    expect(badge.classList.contains('overdue')).toBe(false);
    expect(badge.classList.contains('soon')).toBe(false);
  });
});
