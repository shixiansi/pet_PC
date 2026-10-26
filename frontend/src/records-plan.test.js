/**
 * The records page: the two-line body, the in-place double edit, and the
 * regression fence around the grouping/collapsing/summary that must not move.
 *
 * T33a-T33d add no behaviour. They exist because the spec card's last sentence
 * ("top folds by day, only the newest day is open, the group title is
 * 条数 · 分钟数, the footer is 共 N 条 · 累计专注 X 小时 - all unchanged") was the
 * one promise this change could quietly break, and nothing in the repo asserted
 * it. The assertions are therefore written against the RENDERED DOM, so a
 * restructured renderRecords fails here rather than in a screenshot review.
 *
 * The day keys are computed from `new Date()` so the local-day grouping is
 * genuinely exercised: a fixed timestamp would still pass under a UTC-based
 * implementation in a UTC+8 runner's morning.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { mountApp, baseState } from './test-harness.js';

const here = path.dirname(fileURLToPath(import.meta.url));
const styleCss = readFileSync(path.join(here, 'style.css'), 'utf8');

/** The local end time `daysAgo` days back, at hour:minute. */
function at(daysAgo, hour, minute) {
  const d = new Date();
  d.setDate(d.getDate() - daysAgo);
  d.setHours(hour, minute, 0, 0);
  return d.toISOString();
}

const record = (over) => ({
  id: 'r',
  todoId: '',
  todoTitle: '',
  startedAt: at(0, 17, 45),
  endedAt: at(0, 18, 10),
  focusMinutes: 25,
  summary: '',
  plan: '',
  ...over,
});

const items = () => [...document.querySelectorAll('#records-list .record-item')];
const days = () => [...document.querySelectorAll('#records-list li.record-day')];
const linesOf = (item) => [...item.querySelectorAll('.record-summary .record-line')];

/**
 * The TEXT of a body line. The 实际 line carries its 改 control, so the button is
 * stripped: what is being asserted is the line's wording, which is what the
 * spec fixes, not the markup that carries the control.
 */
function lineText(item, i) {
  const clone = linesOf(item)[i].cloneNode(true);
  for (const button of clone.querySelectorAll('button')) button.remove();
  return clone.textContent;
}

/** Every CSS rule whose selector mentions `cls`, concatenated. */
function rulesFor(cls) {
  const blocks = [];
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = re.exec(styleCss)) !== null) {
    if (new RegExp(`\\.${cls}\\b`).test(m[1])) blocks.push(m[2]);
  }
  return blocks;
}

describe('records-plan: the two-line body (T18)', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    vi.resetModules();
  });

  const mount = (over) => mountApp({ state: baseState({ records: [record(over)] }) });

  it('keeps the head row word for word and shows 计划 then 实际 in the body', async () => {
    await mount({ id: 'r1', plan: '整理接口', summary: '写完了数据部分', todoTitle: '写周报' });

    const item = items()[0];
    expect(item.querySelector('.record-when').textContent).toBe('18:10 · 写周报');
    expect(item.querySelector('.record-mins').textContent).toBe('25分');

    expect(linesOf(item).length).toBe(2);
    expect(lineText(item, 0)).toBe('计划：整理接口');
    expect(lineText(item, 1)).toBe('实际：写完了数据部分');
  });

  it('does not repeat the todo name on the plan line when the plan is set', async () => {
    await mount({ id: 'r1', plan: '整理接口', summary: '写完了数据部分', todoTitle: '写周报' });
    expect(lineText(items()[0], 0)).not.toContain('写周报');
  });

  it('falls back to the todo name when there is no plan, and marks a blank summary', async () => {
    await mount({ id: 'r2', plan: '', summary: '', todoTitle: '写周报' });

    const item = items()[0];
    expect(lineText(item, 0)).toBe('计划：写周报');
    expect(lineText(item, 1)).toBe('实际：（未填写）');
  });

  it('falls back to 未关联待办 when there is neither a plan nor a todo', async () => {
    await mount({ id: 'r3', plan: '', summary: '', todoTitle: '' });
    expect(lineText(items()[0], 0)).toBe('计划：未关联待办');
  });

  it('marks a blank summary even when the plan is present', async () => {
    await mount({ id: 'r4', plan: '整理接口', summary: '' });
    expect(lineText(items()[0], 1)).toBe('实际：（未填写）');
  });

  it('keeps the 改 button on the 实际 line', async () => {
    await mount({ id: 'r1', plan: '整理接口', summary: '写完了数据部分' });

    const item = items()[0];
    const edit = item.querySelector('.record-summary .link-btn');
    expect(edit.textContent).toBe('改');
    expect(linesOf(item)[1].contains(edit)).toBe(true);
    expect(linesOf(item)[0].contains(edit)).toBe(false);
  });
});

describe('records-plan: the in-place edit of both fields (T19)', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    vi.resetModules();
  });

  /** Enters edit mode by clicking the row's own 改 button. */
  async function openEditor(stateOver, handlers = {}) {
    const app = await mountApp({
      state: baseState({ records: [record({ id: 'r1', ...stateOver })] }),
      handlers: { EditPomodoroRecord: () => baseState({ records: [record({ id: 'r1' })] }), ...handlers },
    });
    items()[0].querySelector('.record-summary .link-btn').click();
    return app;
  }

  const inputs = () => [...document.querySelectorAll('#records-list textarea.record-input')];

  it('renders two textareas seeded with the plan and the summary, wrapped for the two-line layout', async () => {
    await openEditor({ plan: '整理接口', summary: '写完了数据部分' });

    expect(inputs().length).toBe(2);
    expect(inputs()[0].value).toBe('整理接口');
    expect(inputs()[1].value).toBe('写完了数据部分');

    const wrap = items()[0].querySelector('.record-summary .record-inputs');
    expect(wrap).not.toBeNull();
    expect(wrap.contains(inputs()[0])).toBe(true);
    expect(wrap.contains(inputs()[1])).toBe(true);
    expect([...items()[0].querySelectorAll('.record-actions button')].map((b) => b.textContent)).toEqual([
      '保存',
      '取消',
    ]);
  });

  it('saves BOTH values through one EditPomodoroRecord call and leaves edit mode', async () => {
    const { calls } = await openEditor({ plan: '整理接口', summary: '写完了数据部分' });

    inputs()[0].value = '整理接口（改）';
    inputs()[1].value = '写完了数据部分（改）';
    items()[0].querySelector('.record-actions button').click();
    await new Promise((r) => setTimeout(r, 0));

    const saved = calls.filter((c) => c.name === 'EditPomodoroRecord');
    expect(saved.length).toBe(1);
    expect(saved[0].args).toEqual(['r1', '整理接口（改）', '写完了数据部分（改）']);
    // editingRecordId === null is observable as the editor being gone.
    expect(inputs().length).toBe(0);
  });

  it('a plan can be cleared: the saved value is the empty string', async () => {
    const { calls } = await openEditor({ plan: '整理接口', summary: '写完了数据部分' });

    inputs()[0].value = '';
    items()[0].querySelector('.record-actions button').click();
    await new Promise((r) => setTimeout(r, 0));

    const saved = calls.filter((c) => c.name === 'EditPomodoroRecord');
    expect(saved.length).toBe(1);
    expect(saved[0].args[1]).toBe('');
  });

  it('seeds an empty textarea for a record with nothing in it', async () => {
    await openEditor({ plan: '', summary: '' });
    expect(inputs()[0].value).toBe('');
    expect(inputs()[1].value).toBe('');
  });

  it('cancel leaves edit mode without touching the backend', async () => {
    const { calls } = await openEditor({ plan: '整理接口', summary: '写完了数据部分' });

    const buttons = [...items()[0].querySelectorAll('.record-actions button')];
    buttons[1].click();
    await new Promise((r) => setTimeout(r, 0));

    expect(calls.filter((c) => c.name === 'EditPomodoroRecord').length).toBe(0);
    expect(inputs().length).toBe(0);
  });
});

describe('records-plan: the grouping that must not move (T33a/T33b/T33c/T33d)', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    vi.resetModules();
  });

  // Today: two sessions (25 + 15 minutes). Yesterday: one (25 minutes).
  const threeRecords = () => [
    record({ id: 't2', endedAt: at(0, 16, 0), focusMinutes: 15, todoTitle: '写周报' }),
    record({ id: 't1', endedAt: at(0, 11, 0), focusMinutes: 25, todoTitle: '写周报' }),
    record({ id: 'y1', endedAt: at(1, 20, 0), focusMinutes: 25, todoTitle: '整理接口' }),
  ];

  it('T33a: groups by local day into li.record-day, each with a head button and a list', async () => {
    await mountApp({ state: baseState({ records: threeRecords() }) });

    const [today, yesterday] = days();
    expect(days().length).toBe(2);

    expect(today.querySelector('button.record-day-head')).not.toBeNull();
    expect(today.querySelector('ul.record-day-list')).not.toBeNull();
    expect(today.querySelectorAll('.record-item').length).toBe(2);

    expect(yesterday.querySelector('button.record-day-head')).not.toBeNull();
    expect(yesterday.querySelectorAll('.record-item').length).toBe(1);
  });

  it('T33a: the same local day lands in the same group, not one group per record', async () => {
    await mountApp({ state: baseState({ records: threeRecords() }) });
    // Two of the three records share a local date, so two groups rather than three.
    expect(days().length).toBe(2);
  });

  it('T33b: only the newest day starts open, and it is first', async () => {
    await mountApp({ state: baseState({ records: threeRecords() }) });

    const [today, yesterday] = days();
    expect(today.querySelector('.record-day-list').classList.contains('hidden')).toBe(false);
    expect(yesterday.querySelector('.record-day-list').classList.contains('hidden')).toBe(true);
    expect(today.querySelector('.record-day-name').textContent).toMatch(/^今天 · /);
  });

  it('T33b: a day that appears later opens by itself without disturbing the others', async () => {
    let snapshot = baseState({ records: threeRecords() });
    const { emit } = await mountApp({
      state: snapshot,
      handlers: { GetState: () => snapshot },
    });

    // The user folds/unfolds a day the page already knows about.
    days()[1].querySelector('button.record-day-head').click();
    expect(days()[1].querySelector('.record-day-list').classList.contains('hidden')).toBe(false);

    // A session from a day the page has never seen. It arrives the way every
    // refresh does - through a state re-read - so the fold's memory of the
    // existing days is exercised as well.
    snapshot = baseState({
      records: [...threeRecords(), record({ id: 'o1', endedAt: at(2, 9, 0), todoTitle: '老记录' })],
    });
    emit('pet:visible', { visible: true });
    await new Promise((r) => setTimeout(r, 0));

    const [today, yesterday, older] = days();
    expect(days().length).toBe(3);
    // The new day opens by itself...
    expect(older.querySelector('.record-day-list').classList.contains('hidden')).toBe(false);
    // ...and the two that were already on screen keep the state they were in.
    expect(today.querySelector('.record-day-list').classList.contains('hidden')).toBe(false);
    expect(yesterday.querySelector('.record-day-list').classList.contains('hidden')).toBe(false);
  });

  it('T33b: a day the user collapsed stays collapsed when a new day arrives', async () => {
    let snapshot = baseState({ records: threeRecords() });
    const { emit } = await mountApp({
      state: snapshot,
      handlers: { GetState: () => snapshot },
    });

    days()[0].querySelector('button.record-day-head').click();
    expect(days()[0].querySelector('.record-day-list').classList.contains('hidden')).toBe(true);

    snapshot = baseState({
      records: [...threeRecords(), record({ id: 'o1', endedAt: at(2, 9, 0), todoTitle: '老记录' })],
    });
    emit('pet:visible', { visible: true });
    await new Promise((r) => setTimeout(r, 0));

    expect(days()[0].querySelector('.record-day-list').classList.contains('hidden')).toBe(true);
  });

  it('T33b: the day header toggles that day once per click', async () => {
    await mountApp({ state: baseState({ records: threeRecords() }) });

    const head = () => days()[1].querySelector('button.record-day-head');
    const list = () => days()[1].querySelector('.record-day-list');

    // Yesterday starts folded.
    expect(list().classList.contains('hidden')).toBe(true);

    head().click();
    expect(list().classList.contains('hidden')).toBe(false);

    head().click();
    expect(list().classList.contains('hidden')).toBe(true);
  });

  it('T33c: the day header carries 条数 · 分钟数, and folding does not change it', async () => {
    await mountApp({ state: baseState({ records: threeRecords() }) });

    const meta = () => days()[0].querySelector('.record-day-meta').textContent;
    expect(meta()).toBe('2 条 · 40 分');

    days()[0].querySelector('button.record-day-head').click();
    expect(meta()).toBe('2 条 · 40 分');
  });

  it('T33d: the footer totals the archive, and says nothing when it is empty', async () => {
    await mountApp({
      state: baseState({
        records: threeRecords(),
        recordTotals: { count: 3, focusMinutes: 90 },
      }),
    });
    expect(document.getElementById('records-total').textContent).toBe('共 3 条 · 累计专注 1.5 小时');
  });

  it('T33d: an empty archive shows no footer line', async () => {
    await mountApp({
      state: baseState({ records: [], recordTotals: { count: 0, focusMinutes: 0 } }),
    });
    expect(document.getElementById('records-total').textContent).toBe('');
    expect(document.getElementById('records-list').children.length).toBe(0);
  });
});

describe('records-plan: the three new CSS rules (T20)', () => {
  it('all three exist', () => {
    for (const cls of ['bubble-plan', 'record-plan', 'record-inputs']) {
      expect(rulesFor(cls).length, `.${cls} is missing`).toBeGreaterThan(0);
    }
  });

  it('none of the new rule bodies draws outside the fixed box', () => {
    for (const cls of ['bubble-plan', 'record-plan', 'record-inputs']) {
      const body = rulesFor(cls).join('\n');
      // A shadow, a filter and a transform all paint outside the border box, and
      // immediately outside is the user's desktop (README 9/11). The bubble and
      // the panel are exact native regions, so a spill is a visible defect.
      expect(body, `.${cls} carries a box-shadow`).not.toMatch(/box-shadow\s*:/);
      expect(body, `.${cls} carries a filter`).not.toMatch(/filter\s*:/);
      expect(body, `.${cls} carries a transform`).not.toMatch(/transform\s*:/);
    }
  });

  it('.bubble-plan is a secondary hint line, like the todo line it sits under', () => {
    const body = rulesFor('bubble-plan').join('\n');
    expect(body).toMatch(/margin:\s*0/);
    expect(body).toMatch(/font-size:\s*\d+px/);
    expect(body).toMatch(/flex:\s*0 0 auto/);
  });
});
