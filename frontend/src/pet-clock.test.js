/**
 * The line a plugin draws on the pet itself (`pet.badge`).
 *
 * WHY THIS FILE EXISTS. The corner badge's test file records the failure that made these worth
 * writing: a badge once rendered `function open() { [native code] }` and every gate stayed green,
 * because the fixture carried no value and the element was simply hidden. The clock has the same
 * shape of risk - a string that arrives from a plugin through Go - so these tests hand the page a
 * value, an empty value, and an event, and assert what is on screen.
 *
 * The host side of the same contract (a plugin that is disabled stops being able to paint on the
 * pet) is asserted in app_pet_badge_test.go.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mountApp } from './test-harness.js';

beforeEach(() => {
  document.body.innerHTML = '';
  vi.resetModules();
});

const clock = () => document.getElementById('pet-clock');

describe('the line on the pet', () => {
  it('shows what the host hands it, verbatim', async () => {
    await mountApp({ handlers: { PetBadge: () => '24:59' } });
    expect(clock().classList.contains('hidden')).toBe(false);
    // Verbatim: the page never reformats, because it does not know what the string means.
    expect(clock().textContent).toBe('24:59');
  });

  it('is hidden when nothing is set, rather than an empty pill', async () => {
    await mountApp({ handlers: { PetBadge: () => '' } });
    expect(clock().classList.contains('hidden')).toBe(true);
    expect(clock().textContent).toBe('');
  });

  it('follows the event, and disappears when the plugin clears it', async () => {
    const { emit } = await mountApp({ handlers: { PetBadge: () => '' } });
    emit('pet:badge', { text: '05:00' });
    expect(clock().textContent).toBe('05:00');
    expect(clock().classList.contains('hidden')).toBe(false);

    // Clearing is the empty string - that is how a paused or finished timer removes the line.
    emit('pet:badge', { text: '' });
    expect(clock().classList.contains('hidden')).toBe(true);
  });

  it('survives an event with no payload instead of printing "undefined"', async () => {
    const { emit } = await mountApp({ handlers: { PetBadge: () => '' } });
    emit('pet:badge', undefined);
    expect(clock().textContent).toBe('');
    expect(clock().classList.contains('hidden')).toBe(true);
  });
});
