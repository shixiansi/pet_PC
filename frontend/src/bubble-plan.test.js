/**
 * The plan bubble and the guards that keep it on screen.
 *
 * Every assertion here drives the REAL shipping page through `pet:layout` with a
 * `plan` view, the way Go hands it over, rather than calling a renderer by name:
 * the bubble branch, the `els` table entry and the markup all have to line up
 * for any of this to be observable, and a rename in one place fails here instead
 * of in the app (tasks.md T8/T11/T16/T21/T22).
 *
 * The textarea's `placeholder` is the load-bearing oddity: the page's markup is
 * shared by two kinds of prompt, so the plan text is written at RUNTIME and the
 * summary branch has to put the attribute's own text back when it takes over.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { mountApp, layout, bubbleView, callsTo } from './test-harness.js';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');

const indexHtml = readFileSync(path.join(root, 'index.html'), 'utf8');
const mainJs = readFileSync(path.join(root, 'src', 'main.js'), 'utf8');
const appJs = readFileSync(path.join(root, 'wailsjs', 'go', 'main', 'App.js'), 'utf8');
const appDts = readFileSync(path.join(root, 'wailsjs', 'go', 'main', 'App.d.ts'), 'utf8');
const modelsTs = readFileSync(path.join(root, 'wailsjs', 'go', 'models.ts'), 'utf8');

/** The plan bubble's own prompt, written into the shared textarea at runtime. */
const PLAN_PLACEHOLDER = '这件事是…（可留空，Enter 开始）';
/** The textarea's attribute - the summary prompt's wording. */
const SUMMARY_PLACEHOLDER = '写点什么…（Enter 保存）';

const $ = (id) => document.getElementById(id);
const isHidden = (id) => $(id).classList.contains('hidden');

/**
 * The app installs its Escape handler on `document`, which outlives the body the
 * harness replaces between tests. A previous test's handler would therefore see
 * ITS last bubble and call the CURRENT test's bridge, so every document-level
 * keydown listener the app adds is tracked and removed after each test. Without
 * this, the reminder bubble of an earlier test answers a later test's Escape.
 */
const nativeAdd = document.addEventListener.bind(document);
const nativeRemove = document.removeEventListener.bind(document);
let docKeydowns = [];
document.addEventListener = (type, fn, opts) => {
  if (type === 'keydown') docKeydowns.push(fn);
  return nativeAdd(type, fn, opts);
};

/** Lets the app's already-resolved promise chains settle. */
const tick = () => new Promise((r) => setTimeout(r, 0));

const showBubble = (emit, view) => emit('pet:layout', layout({ bubble: view }));

/** Dispatches a keydown and hands the event back for the defaultPrevented check. */
function press(target, init) {
  const event = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init });
  target.dispatchEvent(event);
  return event;
}

/** A pointer gesture on the pet. jsdom has no PointerEvent, and the handlers only
 * read the pointer fields a MouseEvent already carries. */
function pointer(target, type, x, y) {
  target.dispatchEvent(
    new MouseEvent(type, { bubbles: true, cancelable: true, screenX: x, screenY: y, button: 0 }),
  );
}

/** Press-move-release past the drag threshold. */
async function dragPet(x0, y0) {
  const pet = $('pet');
  pointer(pet, 'pointerdown', x0, y0);
  pointer(pet, 'pointermove', x0 + 30, y0 + 30);
  // Let StartDrag() resolve before the release: the app fills in `drag.origin`
  // from that promise, and releasing while it is still in flight makes the app's
  // own continuation write into the drag it has just cleared.
  await tick();
  pointer(pet, 'pointerup', x0 + 30, y0 + 30);
  await tick();
}

/** Two clicks inside the double-click interval: the pet's "go answer it" gesture. */
async function doubleClickPet() {
  const pet = $('pet');
  for (let i = 0; i < 2; i += 1) {
    pointer(pet, 'pointerdown', 40, 40);
    pointer(pet, 'pointerup', 40, 40);
  }
  await tick();
}

const escape = () =>
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));

/** Clears the page, the module registry and the app's document-level listeners. */
function resetPage() {
  for (const fn of docKeydowns) nativeRemove('keydown', fn);
  docKeydowns = [];
  document.body.innerHTML = '';
  vi.resetModules();
}

afterEach(resetPage);

describe('bubble-plan: markup and the els entry (T21)', () => {
  beforeEach(resetPage);

  it('the #bubble-plan block sits between #bubble-todo and #bubble-list', () => {
    const todo = indexHtml.indexOf('id="bubble-todo"');
    const plan = indexHtml.indexOf('id="bubble-plan"');
    const list = indexHtml.indexOf('id="bubble-list"');
    expect(todo).toBeGreaterThan(-1);
    expect(plan).toBeGreaterThan(todo);
    expect(list).toBeGreaterThan(plan);
  });

  it('the new block is the shipped secondary line: <p id="bubble-plan" class="bubble-plan hidden">', () => {
    expect(indexHtml).toMatch(/<p id="bubble-plan" class="bubble-plan hidden"><\/p>/);
  });

  it('the bubble input keeps its own placeholder attribute untouched', () => {
    expect(indexHtml).toMatch(
      /<textarea id="bubble-input"[^>]*placeholder="写点什么…（Enter 保存）"/,
    );
  });

  it('main.js registers bubblePlan in the els table', () => {
    expect(mainJs).toMatch(/bubblePlan: \$\('bubble-plan'\)/);
  });

  it('a plan line nobody assigned to stays hidden after any other kind renders', async () => {
    const { emit } = await mountApp();
    showBubble(emit, bubbleView('reminder', { title: '今天要做的事' }));
    expect(isHidden('bubble-plan')).toBe(true);

    showBubble(emit, bubbleView('pick', { items: [{ id: 't1', title: '写周报' }] }));
    expect(isHidden('bubble-plan')).toBe(true);

    showBubble(emit, bubbleView('plan', { plan: '整理接口', todoTitle: '写周报' }));
    expect(isHidden('bubble-plan')).toBe(true);
  });
});

describe('bubble-plan: the plan branch (T8)', () => {
  beforeEach(resetPage);

  it('prefills the input, retitles the buttons and writes the plan placeholder at runtime', async () => {
    const { emit } = await mountApp();
    showBubble(emit, bubbleView('plan', { plan: '整理接口', todoTitle: '写周报' }));

    expect($('bubble-input').value).toBe('整理接口');
    expect($('bubble-input').placeholder).toBe(PLAN_PLACEHOLDER);
    expect(isHidden('bubble-input')).toBe(false);
    expect($('bubble-primary').textContent).toBe('开始专注');
    expect($('bubble-secondary').textContent).toBe('跳过');
    expect($('bubble-todo').textContent).toBe('待办：写周报');
    expect(isHidden('bubble-todo')).toBe(false);
  });

  it('hides the todo hint when the plan question has no todo', async () => {
    const { emit } = await mountApp();
    showBubble(emit, bubbleView('plan', { plan: '' }));

    expect(isHidden('bubble-todo')).toBe(true);
    expect($('bubble-input').value).toBe('');
  });

  it('both buttons submit: 开始专注 sends the text, 跳过 sends the empty string', async () => {
    const { calls, emit } = await mountApp();
    showBubble(emit, bubbleView('plan', { plan: '整理接口' }));

    $('bubble-input').value = '整理接口第二版';
    $('bubble-primary').click();
    await tick();

    $('bubble-secondary').click();
    await tick();

    expect(callsTo(calls, 'StartPomodoroWithPlan').map((c) => c.args)).toEqual([
      ['整理接口第二版'],
      [''],
    ]);
  });

  it('Enter submits the current value, and neither Shift+Enter nor an IME Enter does', async () => {
    const { calls, emit } = await mountApp();
    showBubble(emit, bubbleView('plan', { plan: '整理接口' }));
    const input = $('bubble-input');

    // The IME's Enter commits a candidate; a Shift+Enter is a newline. Neither is
    // an answer, and answering on them would start the clock on a half-typed plan.
    const composing = press(input, { key: 'Enter', isComposing: true });
    const shifted = press(input, { key: 'Enter', shiftKey: true });
    await tick();
    expect(callsTo(calls, 'StartPomodoroWithPlan').length).toBe(0);
    expect(composing.defaultPrevented).toBe(false);
    expect(shifted.defaultPrevented).toBe(false);

    input.value = '整理接口第二版';
    const plain = press(input, { key: 'Enter' });
    await tick();

    const sent = callsTo(calls, 'StartPomodoroWithPlan');
    expect(sent.length).toBe(1);
    expect(sent[0].args).toEqual(['整理接口第二版']);
    // Enter must be swallowed, or it would insert a newline into the box as well.
    expect(plain.defaultPrevented).toBe(true);
  });

  it('the Enter handler does not survive into another bubble kind', async () => {
    const { emit } = await mountApp();
    showBubble(emit, bubbleView('plan', { plan: '整理接口' }));
    expect(typeof $('bubble-input').onkeydown).toBe('function');

    showBubble(emit, bubbleView('reminder', { title: '今天要做的事' }));
    expect($('bubble-input').onkeydown).toBeNull();
  });
});

describe('bubble-plan: the summary branch carries the plan (T16, frontend half)', () => {
  beforeEach(resetPage);

  it('shows 「计划：…」 under the todo hint and puts the summary placeholder back', async () => {
    const { emit } = await mountApp();
    // The plan kind first, so the shared textarea is carrying the plan wording
    // when the summary takes the card over.
    showBubble(emit, bubbleView('plan', { plan: '整理接口', todoTitle: '写周报' }));
    showBubble(
      emit,
      bubbleView('summary', { plan: '整理接口', recordId: 'r1', todoTitle: '写周报' }),
    );

    expect($('bubble-plan').textContent).toBe('计划：整理接口');
    expect(isHidden('bubble-plan')).toBe(false);
    expect($('bubble-todo').textContent).toBe('待办：写周报');
    expect($('bubble-secondary').textContent).toBe('跳过');
    expect($('bubble-primary').textContent).toBe('保存');
    expect($('bubble-input').placeholder).toBe(SUMMARY_PLACEHOLDER);
    expect($('bubble-input').value).toBe('');
  });

  it('hides the whole plan line when the record has no plan', async () => {
    const { emit } = await mountApp();
    showBubble(emit, bubbleView('plan', { plan: '' }));
    showBubble(emit, bubbleView('summary', { plan: '', recordId: 'r1', todoTitle: '写周报' }));

    expect(isHidden('bubble-plan')).toBe(true);
    expect($('bubble-input').placeholder).toBe(SUMMARY_PLACEHOLDER);
  });

  it('Enter still saves the ACTUAL result through SavePomodoroSummary', async () => {
    const { calls, emit } = await mountApp();
    showBubble(emit, bubbleView('summary', { plan: '整理接口', recordId: 'r1' }));

    const input = $('bubble-input');
    input.value = '写完了数据部分';
    press(input, { key: 'Enter' });
    await tick();

    const saved = callsTo(calls, 'SavePomodoroSummary');
    expect(saved.length).toBe(1);
    expect(saved[0].args).toEqual(['r1', '写完了数据部分']);
  });
});

describe('bubble-plan: the guards treat plan like the other gate (T11)', () => {
  beforeEach(resetPage);

  it('a double click on the pet sends the caret to the input instead of opening the popup', async () => {
    const { calls, emit } = await mountApp();
    showBubble(emit, bubbleView('plan', { plan: '整理接口' }));

    await doubleClickPet();

    expect(callsTo(calls, 'OpenLive').length).toBe(0);
    expect(document.activeElement).toBe($('bubble-input'));
  });

  it('Escape does not dismiss the plan or the summary prompt, but still closes a reminder', async () => {
    const { calls, emit } = await mountApp();

    showBubble(emit, bubbleView('plan', { plan: '整理接口' }));
    escape();
    await tick();
    expect(callsTo(calls, 'DismissBubble').length).toBe(0);

    showBubble(emit, bubbleView('summary', { recordId: 'r1' }));
    escape();
    await tick();
    expect(callsTo(calls, 'DismissBubble').length).toBe(0);

    showBubble(emit, bubbleView('reminder', { title: '今天要做的事' }));
    escape();
    await tick();
    expect(callsTo(calls, 'DismissBubble').length).toBe(1);
  });

  it('dragging the pet away does not dismiss the plan prompt, but does dismiss a reminder', async () => {
    const { calls, emit } = await mountApp();

    showBubble(emit, bubbleView('plan', { plan: '整理接口' }));
    await dragPet(200, 200);
    expect(callsTo(calls, 'DismissBubble').length).toBe(0);

    showBubble(emit, bubbleView('reminder', { title: '今天要做的事' }));
    await dragPet(400, 400);
    expect(callsTo(calls, 'DismissBubble').length).toBe(1);
  });
});

describe('bubble-plan: the Wails bindings and types (T22)', () => {
  beforeEach(resetPage);

  it('App.js exports both new bindings and forwards them to the Go bridge', () => {
    expect(appJs).toMatch(
      /export function StartPomodoroWithPlan\(arg1\) \{\s*return window\['go'\]\['main'\]\['App'\]\['StartPomodoroWithPlan'\]\(arg1\);\s*\}/,
    );
    expect(appJs).toMatch(
      /export function EditPomodoroRecord\(arg1, arg2, arg3\) \{\s*return window\['go'\]\['main'\]\['App'\]\['EditPomodoroRecord'\]\(arg1, arg2, arg3\);\s*\}/,
    );
  });

  it('calling them really reaches the bridge with the right arguments', async () => {
    const { calls } = await mountApp();
    const bindings = await import('../wailsjs/go/main/App.js');

    await bindings.StartPomodoroWithPlan('整理接口');
    await bindings.EditPomodoroRecord('r1', '整理接口', '写完了');

    expect(callsTo(calls, 'StartPomodoroWithPlan').map((c) => c.args)).toEqual([['整理接口']]);
    expect(callsTo(calls, 'EditPomodoroRecord').map((c) => c.args)).toEqual([
      ['r1', '整理接口', '写完了'],
    ]);
  });

  it('App.d.ts declares Promise<main.State> for both, matching the Go signatures', () => {
    expect(appDts).toMatch(
      /export function StartPomodoroWithPlan\(arg1:string\):Promise<main\.State>;/,
    );
    expect(appDts).toMatch(
      /export function EditPomodoroRecord\(arg1:string,arg2:string,arg3:string\):Promise<main\.State>;/,
    );
  });

  it('models.ts carries the plan fields on the record and the bubble view', () => {
    const record = modelsTs
      .slice(modelsTs.indexOf('export class PomodoroRecord'))
      .split('\n\texport class')[0];
    // Optional (`plan?`), not required: the Go field carries `omitempty`, and the
    // Wails generator marks every omittable string optional - the sibling
    // todoId/todoTitle come out the same way. What matters is that the field is
    // declared AND assigned by the constructor, or a record would round-trip
    // through the frontend with its plan silently dropped.
    expect(record).toMatch(/^\s*plan\?: string;$/m);
    expect(record).toMatch(/^\s*planEditedAt\?: any;$/m);
    expect(record).toMatch(/this\.plan = source\["plan"\];/);

    const bubble = modelsTs
      .slice(modelsTs.indexOf('export class BubbleView'))
      .split('\n\texport class')[0];
    expect(bubble).toMatch(/^\s*plan\?: string;$/m);
    expect(bubble).toMatch(/this\.plan = source\["plan"\];/);
  });

  it('the record type keeps its generated convertValues plumbing for planEditedAt', () => {
    const record = modelsTs
      .slice(modelsTs.indexOf('export class PomodoroRecord'))
      .split('\n\texport class')[0];
    expect(record).toMatch(
      /this\.planEditedAt = this\.convertValues\(source\["planEditedAt"\], null\);/,
    );
  });
});
