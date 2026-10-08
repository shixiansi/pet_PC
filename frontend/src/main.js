/**
 * PetPC frontend.
 *
 * Responsibilities are split with the Go side deliberately:
 *  - Go owns the window: its size, its position, where the pet sits inside it,
 *    and which rectangle of the window is visible at all. It also owns the
 *    pomodoro state machine and the reminder schedule.
 *  - This file owns the presentation: painting the layout Go reports, the todo
 *    UI, the speech bubble, and the settings panel.
 *
 * The single most important rule here is that nothing may draw outside the
 * rectangles Go crops the window to. The pet and the panel/bubble are the only
 * painted things, and any decoration that spills past its box - a shadow, a
 * filter, a glow - lands directly on the user's desktop. See style.css.
 */

import './style.css';
import defaultPet from './assets/default-pet.svg';

import * as App from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { ChatTab } from './chat';
import { ChatPopup } from './live';

const $ = (id) => document.getElementById(id);

const els = {
  pet: $('pet'),
  petImage: $('pet-image'),
  badge: $('badge'),
  timerBadge: $('timer-badge'),
  panel: $('panel'),
  close: $('close-panel'),
  toast: $('toast'),

  tabs: document.querySelectorAll('.tab'),
  tabTodos: $('tab-todos'),
  tabChat: $('tab-chat'),
  tabRecords: $('tab-records'),
  tabSettings: $('tab-settings'),

  form: $('todo-form'),
  title: $('todo-title'),
  due: $('todo-due'),
  notes: $('todo-notes'),
  submit: $('todo-form').querySelector('button[type="submit"]'),
  summary: $('summary'),
  clearDone: $('clear-done'),
  list: $('todo-list'),
  empty: $('empty-hint'),

  // Pomodoro strip in the todo tab.
  pomoStrip: $('pomo-strip'),
  pomoPhase: $('pomo-phase'),
  pomoCountdown: $('pomo-countdown'),
  pomoTodo: $('pomo-todo'),
  pomoToggle: $('pomo-toggle'),
  pomoStart: $('pomo-start'),
  pomoPause: $('pomo-pause'),
  pomoResume: $('pomo-resume'),
  pomoStop: $('pomo-stop'),
  pomoSkip: $('pomo-skip'),

  // Records tab.
  recordsList: $('records-list'),
  recordsEmpty: $('records-empty'),
  recordsTotal: $('records-total'),

  // Bubble (reminder / picker / summary).
  bubble: $('bubble'),
  bubbleArrow: $('bubble-arrow'),
  bubbleTitle: $('bubble-title'),
  bubbleTodo: $('bubble-todo'),
  bubbleList: $('bubble-list'),
  bubbleMore: $('bubble-more'),
  bubbleInput: $('bubble-input'),
  bubbleActions: $('bubble-actions'),
  bubbleSecondary: $('bubble-secondary'),
  bubblePrimary: $('bubble-primary'),

  // Settings.
  imagePath: $('image-path'),
  chooseImage: $('choose-image'),
  resetImage: $('reset-image'),
  scale: $('scale'),
  scaleValue: $('scale-value'),
  snap: $('snap'),
  autostart: $('autostart'),
  dataDir: $('data-dir'),
  quit: $('quit'),

  confirm: $('confirm'),
  confirmText: $('confirm-text'),
  confirmOk: $('confirm-ok'),
  confirmCancel: $('confirm-cancel'),

  pomoOnStartup: $('pomo-on-startup'),
  pomoAutoNext: $('pomo-auto-next'),
  pomoSound: $('pomo-sound'),
  pomoFocus: $('pomo-focus'),
  pomoShort: $('pomo-short'),
  pomoLong: $('pomo-long'),
  pomoEvery: $('pomo-every'),
  pomoReset: $('pomo-reset'),

  remEnabled: $('rem-enabled'),
  remOnStartup: $('rem-on-startup'),
  remDuringBreak: $('rem-during-break'),
  remInterval: $('rem-interval'),
  remJitter: $('rem-jitter'),
  remFrom: $('rem-from'),
  remTo: $('rem-to'),
  remItems: $('rem-items'),
  remHide: $('rem-hide'),
  remEmpty: $('rem-empty'),
  remNow: $('rem-now'),
  remStatus: $('rem-status'),

  // LLM settings.
  llmEnabled: $('llm-enabled'),
  llmProvider: $('llm-provider'),
  llmProviderHint: $('llm-provider-hint'),
  llmBase: $('llm-base'),
  llmModel: $('llm-model'),
  llmKey: $('llm-key'),
  llmKeySave: $('llm-key-save'),
  llmKeyClear: $('llm-key-clear'),
  llmKeyStatus: $('llm-key-status'),
  llmKeyFile: $('llm-key-file'),
  llmTest: $('llm-test'),
  llmTestStatus: $('llm-test-status'),
  llmTemp: $('llm-temp'),
  llmMax: $('llm-max'),
  llmTimeout: $('llm-timeout'),
  llmHistory: $('llm-history'),
  llmBudget: $('llm-budget'),
  llmBudgetReset: $('llm-budget-reset'),
  llmUsage: $('llm-usage'),
  llmContext: $('llm-context'),
  llmPrompt: $('llm-prompt'),

  // Fixed voice: three switches and nothing to configure.
  voiceEnabled: $('voice-enabled'),
  voicePomodoro: $('voice-pomodoro'),
  voiceReminder: $('voice-reminder'),
  voiceTest: $('voice-test'),
  voiceStop: $('voice-stop'),
  voiceStatus: $('voice-status'),
  voiceOpenStudio: $('voice-open-studio'),
  voiceActiveRole: $('voice-active-role'),
  voiceBuiltinNote: $('voice-builtin-note'),
  voiceReplacedNote: $('voice-replaced-note'),

  // Chat tab (inside the panel).
  chatBar: $('chat-bar'),
  chatStatus: $('chat-status'),
  chatClear: $('chat-clear'),
  chatTodos: $('chat-todos'),
  chatLog: $('chat-log'),
  chatEmpty: $('chat-empty'),
  chatList: $('chat-list'),
  chatForm: $('chat-form'),
  chatInput: $('chat-input'),
  chatSend: $('chat-send'),
  chatRegen: $('chat-regen'),

  // Chat popup (the pet's real-time surface). It is a self-contained component:
  // it builds its own markup inside this one mount point, so the only element
  // main.js owns for it is #live.
  live: $('live'),

  // Proactive speech settings.
  llmProactive: $('llm-proactive'),
  llmProactiveGap: $('llm-proactive-gap'),
  llmProactiveStatus: $('llm-proactive-status'),
  llmSpeakNow: $('llm-speak-now'),

  // Global hide/show shortcuts.
  hotkeyEnabled: $('hotkey-enabled'),
  hotkeyShow: $('hotkey-show'),
  hotkeyHide: $('hotkey-hide'),
  hotkeyStatus: $('hotkey-status'),
};

/**
 * Drag diagnostics.
 *
 * Pointer handling is entirely in the webview, so this is the only way to see
 * drag events from the Go side. Enable by launching with PETPC_DEBUG=1; the app
 * then appends to debug.log in the data directory.
 */
const debug = (msg) => {
  App.DebugLog(msg).catch(() => {});
};

/** Latest server-side snapshot. */
let state = null;
/** ID of the todo currently being edited, or null when adding. */
let editingId = null;
/** The bubble currently on screen (a copy of the layout's bubble), or null. */
let bubble = null;
/** ID of the record whose summary is being edited in the records tab. */
let editingRecordId = null;
/**
 * Day groups the records tab has open, or null before the first render.
 *
 * The archive is grouped by day and every group but the newest starts closed,
 * because "all records in one column" meant the only way to reach the bottom was
 * to scroll past a year of sessions. The set lives here rather than in the DOM so
 * a re-render (every todo toggle returns a whole new state snapshot) cannot
 * throw the user's expand/collapse choices away.
 */
let openRecordDays = null;
/**
 * Day groups that have ever been rendered, or null before the first render.
 *
 * This is what makes a day that appears LATER - a session finished while the app
 * was open, or the first record of a new day - start expanded, without also
 * expanding the whole back catalogue on the first render.
 */
let seenRecordDays = null;
/** Auto-hide timer for a reminder bubble. */
let bubbleHideTimer = null;
/** Todo ids ticked in the picker, and the picker's chosen id. */
let pickerChoice = '';

/**
 * Active drag, or null.
 *
 * Dragging is done here rather than with Wails' --wails-draggable because that
 * hands the move to Windows as a modal loop: the webview never sees the matching
 * mouseup, so a plain click and a drag become indistinguishable. Driving the
 * window from the pointer stream lets us treat a press that never moves as a
 * click (open the panel) and a press that moves as a drag.
 */
let drag = null;
/** Pointer movement (px) before a press counts as a drag rather than a click. */
const DRAG_THRESHOLD = 4;

/**
 * Pending single-click action, or null.
 *
 * The pet has to distinguish one click (toggle the panel) from two (open the
 * chat). Without this, a double click unwinds as click-click and the panel
 * visibly opens and closes again before the chat appears. The press is therefore
 * held for the system's double-click interval; see the pointerup handler.
 */
let pendingClick = null;
/**
 * Timestamp of the last completed click, for the double-click test.
 *
 * The double click is detected here, from the pointer stream, rather than by
 * listening for the `dblclick` event. The pet's pointerdown handler calls
 * preventDefault (so the webview never starts a text selection or a native drag),
 * and relying on which compatibility mouse events a browser still synthesises
 * after that is a subtlety this code should not depend on. The pointer stream is
 * already the single source of truth for press/move/release.
 */
let lastClickAt = 0;
/** How long a single click waits for a possible second one. */
const DOUBLE_CLICK_MS = 200;

/** The chat tab controller, created in wire(). */
let chat = null;
/** The chat popup component, created in wire(). */
let live = null;

// --- helpers --------------------------------------------------------------

function toast(message) {
  els.toast.textContent = message;
  els.toast.classList.remove('hidden');
  clearTimeout(toast._timer);
  toast._timer = setTimeout(() => els.toast.classList.add('hidden'), 3200);
}

/** Logs and surfaces a rejected backend call. */
function fail(err) {
  console.error(err);
  toast(typeof err === 'string' ? err : err?.message || '操作失败');
}

const pad = (n) => String(n).padStart(2, '0');

/**
 * Asks the user to confirm something, in the app's own styling.
 *
 * Replaces window.confirm(), which WebView2 renders as a native browser dialog:
 * a white "wails.localhost 显示" box that ignores the pet's theme, covers the
 * panel, and lives outside the window region the app is cropped to. It looked
 * like a stray browser window rather than part of the pet.
 *
 * Resolves true when confirmed and false otherwise, so callers can await it.
 */
function confirmDialog(message, okLabel = '确定') {
  return new Promise((resolve) => {
    els.confirmText.textContent = message;
    els.confirmOk.textContent = okLabel;
    els.confirm.classList.remove('hidden');

    const finish = (result) => {
      els.confirm.classList.add('hidden');
      els.confirmOk.onclick = null;
      els.confirmCancel.onclick = null;
      els.confirm.onkeydown = null;
      resolve(result);
    };

    els.confirmOk.onclick = () => finish(true);
    els.confirmCancel.onclick = () => finish(false);
    // Escape and Enter work while the dialog is up, which is what a user expects
    // from a modal.
    els.confirm.onkeydown = (e) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        finish(false);
      } else if (e.key === 'Enter') {
        e.stopPropagation();
        finish(true);
      }
    };
    // Focus the safe choice, so a stray Enter does not destroy the session.
    els.confirmCancel.focus();
  });
}

/** True while the confirmation dialog is up. */
function confirmOpen() {
  return !els.confirm.classList.contains('hidden');
}

/** Formats a Date as the value <input type="datetime-local"> expects. */
function toLocalInput(date) {
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}`
  );
}

/** Renders a due date relative to today, e.g. "今天 14:30". */
function formatDue(date) {
  const now = new Date();
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const dueDay = new Date(date.getFullYear(), date.getMonth(), date.getDate());
  const dayDiff = Math.round((dueDay - startOfToday) / 86400000);
  const time = `${pad(date.getHours())}:${pad(date.getMinutes())}`;

  if (dayDiff === 0) return `今天 ${time}`;
  if (dayDiff === 1) return `明天 ${time}`;
  if (dayDiff === -1) return `昨天 ${time}`;
  return `${date.getMonth() + 1}月${date.getDate()}日 ${time}`;
}

/** Classifies a todo's due date for colouring. */
function dueStatus(todo) {
  if (!todo.due || todo.done) return '';
  const due = new Date(todo.due);
  const now = Date.now();
  if (due.getTime() < now) return 'overdue';
  if (due.getTime() - now <= 86400000) return 'soon';
  return '';
}

/** Renders an RFC3339 timestamp as "18:00". */
function formatClock(iso) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// --- layout ---------------------------------------------------------------

/**
 * Positions the pet, the panel and the bubble from the geometry Go reports.
 *
 * Every value here is effectively constant for a given pet size and side: the
 * window is always the full pet+panel rectangle and is never resized when the
 * panel or a bubble is toggled, so the pet's offset inside it does not change.
 * Showing either one only removes a native region crop, which Windows applies
 * without touching the page layout.
 *
 * That is why there is no viewport bookkeeping here any more. An earlier version
 * resized the window on every toggle and then had to hide the pet while WebView2
 * caught up with the new viewport - a race that cannot be won, and which showed
 * up as the picture blinking on every click.
 */
function applyLayout(layout) {
  if (!layout) return;

  Object.assign(els.pet.style, {
    left: `${layout.petX}px`,
    top: `${layout.petY}px`,
    width: `${layout.petW}px`,
    height: `${layout.petH}px`,
  });

  els.panel.classList.toggle('hidden', !layout.panelOpen);
  els.pet.classList.toggle('anchored', !!layout.panelOpen);

  if (layout.panelOpen) {
    Object.assign(els.panel.style, {
      left: `${layout.panelX}px`,
      top: `${layout.panelY}px`,
      width: `${layout.panelW}px`,
      height: `${layout.panelH}px`,
    });
  }

  applyBubble(layout.bubble || null);

  // The chat popup positions itself from the layout's own rectangles, which are
  // the same ones Go cropped the window to. It is applied even when the payload
  // is absent (nil), so closing it comes through the same path as opening it.
  live?.applyGeometry(layout.live || null);
}

/**
 * Shows, hides or repositions the speech bubble.
 *
 * The bubble's box is fixed by Go and the card fills it exactly, so the region
 * Go cropped to and the pixels drawn here are the same rectangle. Nothing in the
 * bubble may draw outside its box: the arrow is positioned inside it, and the
 * card has no shadow (a shadow is drawn outside the border box, and outside the
 * box is the user's desktop).
 */
function applyBubble(next) {
  const wasKind = bubble?.kind || null;
  bubble = next;

  if (!next) {
    els.bubble.classList.add('hidden');
    clearTimeout(bubbleHideTimer);
    bubbleHideTimer = null;
    if (wasKind === 'reminder') App.SetBubbleHover(false).catch(() => {});
    return;
  }

  const kindChanged = wasKind !== next.kind;
  Object.assign(els.bubble.style, {
    left: `${next.x}px`,
    top: `${next.y}px`,
    width: `${next.w}px`,
    height: `${next.h}px`,
  });
  // The arrow points at the pet, so it sits on the bubble's edge that faces it.
  // `onRight` means the bubble is to the RIGHT of the pet, which puts the arrow
  // on the bubble's LEFT edge. (The class names describe where the arrow IS, not
  // which way it points - getting this backwards silently aims it at nothing.)
  els.bubbleArrow.classList.toggle('at-left', !!next.onRight);
  els.bubbleArrow.classList.toggle('at-right', !next.onRight);

  if (kindChanged) {
    renderBubble(next);
    scheduleBubbleHide(next);
  }
  els.bubble.classList.remove('hidden');
}

/** Renders a bubble's contents for its kind. */
function renderBubble(b) {
  // Reset everything first: the three kinds share the card.
  els.bubbleList.replaceChildren();
  els.bubbleList.classList.add('hidden');
  els.bubbleTodo.classList.add('hidden');
  els.bubbleMore.classList.add('hidden');
  els.bubbleInput.classList.add('hidden');
  // The Enter handler is installed per kind, so a stale one from the previous
  // summary prompt must not survive into the next render.
  els.bubbleInput.onkeydown = null;
  els.bubbleSecondary.classList.add('hidden');
  els.bubblePrimary.classList.add('hidden');
  els.bubbleTitle.textContent = b.title || '';

  if (b.kind === 'reminder') {
    renderBubbleList(b.items || [], false);
    els.bubbleMore.textContent = b.more ? `还有 ${b.more} 项` : '';
    els.bubbleMore.classList.toggle('hidden', !b.more);
    els.bubbleSecondary.textContent = '知道了';
    els.bubbleSecondary.classList.remove('hidden');
    els.bubbleSecondary.onclick = () => App.DismissBubble().then(applyLayout).catch(fail);
    els.bubblePrimary.textContent = '看看待办';
    els.bubblePrimary.classList.remove('hidden');
    els.bubblePrimary.onclick = () => App.OpenTodoPanelFromBubble().then(applyLayout).catch(fail);
    return;
  }

  if (b.kind === 'pick') {
    pickerChoice = b.selectedId || '';
    renderBubbleList(b.items || [], true);
    els.bubbleMore.textContent = b.more ? `还有 ${b.more} 项` : '';
    els.bubbleMore.classList.toggle('hidden', !b.more);
    els.bubbleSecondary.textContent = '取消';
    els.bubbleSecondary.classList.remove('hidden');
    els.bubbleSecondary.onclick = () => App.DismissBubble().then(applyLayout).catch(fail);
    els.bubblePrimary.textContent = '开始专注';
    els.bubblePrimary.classList.remove('hidden');
    els.bubblePrimary.onclick = confirmStart;
    // With nothing open to choose from there is no list to show, so the hint
    // explains why "start" is nevertheless clickable (the timer would otherwise
    // be unusable with an empty todo list).
    if (b.allowUnattached && !(b.items || []).length) {
      els.bubbleMore.textContent = '没有未完成的待办，可直接开始';
      els.bubbleMore.classList.remove('hidden');
    }
    return;
  }

  // Summary prompt.
  if (b.todoTitle) {
    els.bubbleTodo.textContent = `待办：${b.todoTitle}`;
    els.bubbleTodo.classList.remove('hidden');
  }
  els.bubbleInput.value = '';
  els.bubbleInput.classList.remove('hidden');
  els.bubbleSecondary.textContent = '跳过';
  els.bubbleSecondary.classList.remove('hidden');
  els.bubbleSecondary.onclick = () => saveSummary(b.recordId, '');
  els.bubblePrimary.textContent = '保存';
  els.bubblePrimary.classList.remove('hidden');
  els.bubblePrimary.onclick = () => saveSummary(b.recordId, els.bubbleInput.value);

  // Enter confirms the summary, exactly as it sends a chat message in the popup
  // and the conversation tab. The bubble is not a <form>, so this has to be
  // spelled out here or Enter would insert a newline and the user would have to
  // reach for the mouse to release the gate.
  //
  // Shift+Enter still inserts a newline: a summary is prose and one line is not
  // always enough. `isComposing` guards the IME, whose Enter commits the
  // candidate - without that check, typing Chinese would save a half-finished
  // summary on the keystroke that was meant to pick a character.
  els.bubbleInput.onkeydown = (e) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      saveSummary(b.recordId, els.bubbleInput.value);
    }
  };

  // Do NOT focus the textarea automatically. The prompt appears while the user
  // is very likely typing in another application, and stealing the keyboard
  // there would be worse than making them click once
  // (docs/reminder-pomodoro.md 4.5). It is only focused if this window already
  // had focus, which means the user was already working in the pet.
  if (document.hasFocus()) els.bubbleInput.focus();
}

/** Renders a bubble's todo list. `selectable` adds picker radio behaviour. */
function renderBubbleList(items, selectable) {
  if (!items.length) return;
  els.bubbleList.replaceChildren();
  els.bubbleList.classList.remove('hidden');

  for (const item of items) {
    const li = document.createElement('li');
    li.className = 'bubble-item';

    if (selectable) {
      const label = document.createElement('label');
      label.className = 'bubble-choice';
      const radio = document.createElement('input');
      radio.type = 'radio';
      radio.name = 'pomo-todo';
      radio.value = item.id;
      radio.checked = item.id === pickerChoice;
      radio.addEventListener('change', () => {
        pickerChoice = item.id;
      });
      label.append(radio);
      li.append(label);
      const body = document.createElement('div');
      body.className = 'bubble-item-main';
      const t = document.createElement('span');
      t.className = 'bubble-item-title';
      t.textContent = item.title;
      body.append(t);
      if (item.due) {
        const m = document.createElement('span');
        m.className = `bubble-item-due ${item.status || ''}`.trim();
        m.textContent = item.due;
        body.append(m);
      }
      label.append(body);
      // Clicking anywhere on the row picks it.
      li.addEventListener('click', (e) => {
        if (e.target !== radio) radio.checked = true;
        pickerChoice = item.id;
      });
    } else {
      const t = document.createElement('span');
      t.className = 'bubble-item-title';
      t.textContent = item.title;
      li.append(t);
      if (item.due) {
        const m = document.createElement('span');
        m.className = `bubble-item-due ${item.status || ''}`.trim();
        m.textContent = item.due;
        li.append(m);
      }
    }
    els.bubbleList.append(li);
  }
}

/**
 * Starts the countdown that fades a reminder bubble.
 *
 * The picker and the summary prompt never auto-hide: they are waiting for a
 * decision, and a question that vanishes on its own is a bug, not a feature.
 */
function scheduleBubbleHide(b) {
  clearTimeout(bubbleHideTimer);
  bubbleHideTimer = null;
  if (b.kind !== 'reminder') return;
  const secs = state?.config?.reminderAutoHideSec || 12;
  bubbleHideTimer = setTimeout(() => {
    App.DismissBubble().then(applyLayout).catch(() => {});
  }, secs * 1000);
}

// --- rendering ------------------------------------------------------------

function renderBadge() {
  if (!state) return;
  const { open, overdue, dueSoon } = state.stats || {};
  if (!open) {
    els.badge.classList.add('hidden');
  } else {
    els.badge.textContent = String(open);
    els.badge.classList.remove('hidden');
    els.badge.classList.toggle('overdue', overdue > 0);
    els.badge.classList.toggle('soon', overdue === 0 && dueSoon > 0);
    els.badge.title = overdue
      ? `${overdue} 项已逾期`
      : dueSoon
        ? `${dueSoon} 项即将到期`
        : `${open} 项待办`;
  }
  renderTimerBadge();
}

/**
 * Renders the pomodoro badge, which stacks above the todo badge.
 *
 * Both are shown at once while the timer runs (docs 4.4): the countdown says how
 * long is left, the count says how much is waiting. Hiding either would make the
 * user choose which half of the状态 to see.
 */
function renderTimerBadge() {
  const p = state?.pomodoro;
  if (!p || !p.enabled || p.phase === 'idle') {
    els.timerBadge.classList.add('hidden');
    return;
  }
  els.timerBadge.textContent = p.countdown;
  els.timerBadge.classList.remove('hidden');
  els.timerBadge.classList.toggle('focus', p.phase === 'focus' && p.running);
  els.timerBadge.classList.toggle('break', p.phase !== 'focus' && p.running);
  els.timerBadge.classList.toggle('halted', !p.running);
  els.timerBadge.title = p.running
    ? `${p.label} · 剩余 ${p.countdown}`
    : `${p.label}已暂停 · 剩余 ${p.countdown}`;
}

/**
 * Points the pet <img> at the current picture.
 *
 * The source is only assigned when it actually changed. Writing `src` - even
 * with the identical value - makes the browser throw away the decoded bitmap and
 * start over, which blanks the picture for a frame or two. Every todo toggle
 * returns a full state snapshot and re-runs this, so an unconditional assignment
 * made the pet flicker each time a checkbox was clicked, despite the picture
 * having nothing to do with the todo list.
 */
let petImageSrc = null;

function renderPet() {
  if (!state) return;
  const wanted = state.petImageUrl ? state.petImageUrl : defaultPet;
  if (wanted === petImageSrc) return;
  petImageSrc = wanted;
  els.petImage.src = wanted;
  debug(`renderPet: src set to ${wanted}`);
}

function renderSummary() {
  if (!state) return;
  const { open, overdue, total } = state.stats || {};
  const parts = [];
  if (total) parts.push(`共 ${total} 项`);
  parts.push(`${open || 0} 项未完成`);
  if (overdue) parts.push(`${overdue} 项已逾期`);
  els.summary.textContent = parts.join(' · ');
  els.clearDone.disabled = !(state.todos || []).some((t) => t.done);
}

function renderTodos() {
  if (!state) return;
  const todos = state.todos || [];
  els.list.replaceChildren();
  els.empty.classList.toggle('hidden', todos.length > 0);

  for (const todo of todos) {
    els.list.appendChild(renderTodoItem(todo));
  }
  renderSummary();
}

function renderTodoItem(todo) {
  const li = document.createElement('li');
  const status = dueStatus(todo);
  li.className = `todo-item${todo.done ? ' done' : ''}${status === 'overdue' ? ' overdue' : ''}`;

  const check = document.createElement('input');
  check.type = 'checkbox';
  check.checked = !!todo.done;
  check.title = todo.done ? '标记为未完成' : '标记为已完成';
  check.addEventListener('change', () => {
    App.ToggleTodo(todo.id).then(applyState).catch(fail);
  });

  const main = document.createElement('div');
  main.className = 'todo-main';

  // textContent (not innerHTML) so a title containing markup stays inert.
  const title = document.createElement('span');
  title.className = 'todo-title';
  title.textContent = todo.title;
  main.appendChild(title);

  if (todo.due) {
    const meta = document.createElement('div');
    meta.className = 'todo-meta';
    const due = document.createElement('span');
    due.className = `due ${status}`.trim();
    due.textContent = formatDue(new Date(todo.due));
    meta.appendChild(due);
    main.appendChild(meta);
  }

  if (todo.notes) {
    const notes = document.createElement('div');
    notes.className = 'todo-notes';
    notes.textContent = todo.notes;
    main.appendChild(notes);
  }

  const actions = document.createElement('div');
  actions.className = 'todo-actions';

  const edit = document.createElement('button');
  edit.type = 'button';
  edit.textContent = '编辑';
  edit.title = '编辑';
  edit.addEventListener('click', () => startEdit(todo));

  const del = document.createElement('button');
  del.type = 'button';
  del.textContent = '删除';
  del.title = '删除';
  del.addEventListener('click', () => {
    App.DeleteTodo(todo.id).then(applyState).catch(fail);
  });

  actions.append(edit, del);
  li.append(check, main, actions);
  return li;
}

// --- pomodoro UI ----------------------------------------------------------

/** Renders the pomodoro strip in the todo tab. */
function renderPomodoro() {
  const p = state?.pomodoro;
  if (!p) return;

  els.pomoToggle.textContent = p.enabled ? '关闭番茄钟' : '开启番茄钟';

  const idle = !p.enabled || p.phase === 'idle';
  els.pomoPhase.textContent = !p.enabled ? '番茄钟未开启' : p.label;
  els.pomoPhase.classList.toggle('hidden', false);
  els.pomoCountdown.textContent = idle ? '' : p.countdown;

  if (p.todoTitle) {
    els.pomoTodo.textContent = `本轮：${p.todoTitle}`;
    els.pomoTodo.classList.remove('hidden');
  } else {
    els.pomoTodo.classList.add('hidden');
  }

  // Start is offered when the timer is on and nothing is running. Everything
  // else depends on whether a phase is actually counting down.
  els.pomoStart.classList.toggle('hidden', !p.enabled || !idle);
  els.pomoStart.textContent = p.phase === 'focus' ? '选择待办' : '开始专注';
  els.pomoPause.classList.toggle('hidden', !p.running);
  els.pomoResume.classList.toggle('hidden', !p.enabled || p.running || idle);
  els.pomoStop.classList.toggle('hidden', !p.running);
  els.pomoSkip.classList.toggle('hidden', !p.enabled || idle);
}

/**
 * Renders the permanent records archive, grouped by day.
 *
 * A flat list of every session ever is unusable once the archive is a few weeks
 * old: the newest records - the only ones anyone reads - share one screen with a
 * year of history, and finding anything means scrolling through all of it. So the
 * list is folded by the day each session ENDED on, newest day first, and only the
 * newest day is open on first render.
 *
 * The day keys are local YYYY-MM-DD rather than the raw timestamp, because
 * "which day was this" is a question about the user's clock - a 23:30 session
 * belongs to that evening, not to the UTC date the JSON happens to store.
 */
function renderRecords() {
  if (!state) return;
  const records = state.records || [];
  els.recordsList.replaceChildren();
  els.recordsEmpty.classList.toggle('hidden', records.length > 0);

  // Records arrive newest first (store.PomodoroRecords sorts by endedAt), so the
  // groups come out in the right order by simply walking the list once.
  const groups = [];
  for (const rec of records) {
    const key = localDayKey(rec.endedAt);
    const last = groups[groups.length - 1];
    if (last && last.key === key) last.records.push(rec);
    else groups.push({ key, records: [rec] });
  }

  // The first render opens the newest day. After that only days that have never
  // been seen before open by themselves - a session finishing while the app is
  // open, or the first record of a new day, lands expanded and visible; a day the
  // user has already collapsed stays collapsed.
  if (openRecordDays === null || seenRecordDays === null) {
    openRecordDays = new Set(groups.length ? [groups[0].key] : []);
    seenRecordDays = new Set(groups.map((g) => g.key));
  } else {
    for (const group of groups) {
      if (!seenRecordDays.has(group.key)) {
        seenRecordDays.add(group.key);
        openRecordDays.add(group.key);
      }
    }
  }

  const today = localDayKey(new Date().toISOString());
  const yesterday = localDayKey(new Date(Date.now() - 86400000).toISOString());
  for (const group of groups) {
    els.recordsList.append(renderRecordDay(group, today, yesterday));
  }

  const totals = state.recordTotals || { count: 0, focusMinutes: 0 };
  const hours = (totals.focusMinutes / 60).toFixed(1);
  els.recordsTotal.textContent = totals.count
    ? `共 ${totals.count} 条 · 累计专注 ${hours} 小时`
    : '';
}

/** The local calendar day of an RFC3339 timestamp, as YYYY-MM-DD. */
function localDayKey(iso) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/**
 * One collapsible day: a header that toggles, then that day's sessions.
 *
 * The header carries the whole summary of the day - its date, how many sessions,
 * how long - so a collapsed group still answers "did I work that day?" without
 * being opened. That is the point of the fold: the closed state has to be
 * informative, not just short.
 */
function renderRecordDay(group, today, yesterday) {
  const open = openRecordDays.has(group.key);
  const first = group.records[0];
  const day = new Date(first.endedAt);
  const minutes = group.records.reduce((sum, r) => sum + (r.focusMinutes || 0), 0);

  // "今天/昨天" only when they are true: the labels are computed from the local
  // clock, not from the index in the list, so a gap in the archive cannot
  // mislabel a day.
  let label = `${day.getMonth() + 1}月${day.getDate()}日`;
  if (group.key === today) label = `今天 · ${label}`;
  else if (group.key === yesterday) label = `昨天 · ${label}`;
  else if (day.getFullYear() !== new Date().getFullYear()) label = `${day.getFullYear()}年${label}`;

  const li = document.createElement('li');
  li.className = 'record-day';

  const head = document.createElement('button');
  head.type = 'button';
  head.className = `record-day-head${open ? ' open' : ''}`;
  head.setAttribute('aria-expanded', open ? 'true' : 'false');

  const caret = document.createElement('span');
  caret.className = 'record-caret';
  // Two glyphs rather than one rotated by CSS: a transform inside the panel would
  // be safe, but keeping the "nothing transforms" rule total is worth more than
  // the smoother animation (see style.css).
  caret.textContent = open ? '▾' : '▸';

  const name = document.createElement('span');
  name.className = 'record-day-name';
  name.textContent = label;

  const meta = document.createElement('span');
  meta.className = 'record-day-meta';
  meta.textContent = `${group.records.length} 条 · ${minutes} 分`;

  head.append(caret, name, meta);
  head.addEventListener('click', () => {
    if (openRecordDays.has(group.key)) openRecordDays.delete(group.key);
    else openRecordDays.add(group.key);
    renderRecords();
  });

  const list = document.createElement('ul');
  list.className = 'record-day-list';
  list.classList.toggle('hidden', !open);
  for (const rec of group.records) list.append(renderRecordItem(rec));

  li.append(head, list);
  return li;
}

function renderRecordItem(rec) {
  const li = document.createElement('li');
  li.className = 'record-item';

  const head = document.createElement('div');
  head.className = 'record-head';
  const when = document.createElement('span');
  when.className = 'record-when';
  // The DATE is on the group header above, so repeating it on every row would be
  // noise; what is left is the time within the day plus what was worked on.
  when.textContent = `${formatClock(rec.endedAt)} · ${rec.todoTitle || '未关联待办'}`;
  const mins = document.createElement('span');
  mins.className = 'record-mins';
  mins.textContent = `${rec.focusMinutes}分`;
  head.append(when, mins);

  const body = document.createElement('div');
  body.className = 'record-summary';

  if (editingRecordId === rec.id) {
    // Editing happens in place, which is the only mutation the archive allows.
    const input = document.createElement('textarea');
    input.rows = 2;
    input.maxLength = 2000;
    input.value = rec.summary || '';
    input.className = 'record-input';
    const actions = document.createElement('div');
    actions.className = 'record-actions';
    const save = document.createElement('button');
    save.type = 'button';
    save.className = 'primary';
    save.textContent = '保存';
    save.onclick = () => {
      App.SavePomodoroSummary(rec.id, input.value)
        .then((next) => {
          editingRecordId = null;
          applyState(next);
        })
        .catch(fail);
    };
    const cancel = document.createElement('button');
    cancel.type = 'button';
    cancel.textContent = '取消';
    cancel.onclick = () => {
      editingRecordId = null;
      renderRecords();
    };
    actions.append(save, cancel);
    body.append(input, actions);
  } else {
    const text = document.createElement('span');
    if (rec.summary) {
      text.textContent = rec.summary;
    } else {
      text.className = 'record-blank';
      text.textContent = '（未填写）';
    }
    const edit = document.createElement('button');
    edit.type = 'button';
    edit.className = 'link-btn';
    edit.textContent = '改';
    edit.title = '修改总结';
    edit.onclick = () => {
      editingRecordId = rec.id;
      renderRecords();
    };
    const line = document.createElement('div');
    line.className = 'record-line';
    line.append(text, edit);
    body.append(line);
  }

  li.append(head, body);
  // There is deliberately no delete control anywhere on this page: records are
  // permanent (docs/reminder-pomodoro.md 6.1).
  return li;
}

function renderSettings() {
  if (!state) return;
  const cfg = state.config || {};

  els.imagePath.textContent = state.hasCustomImage
    ? cfg.originalPath || '已使用自定义图片'
    : '当前使用默认图片';
  els.resetImage.disabled = !state.hasCustomImage;

  const pct = Math.round((cfg.petScale || 1) * 100);
  els.scale.value = String(pct);
  els.scaleValue.textContent = `${pct}%`;

  els.snap.checked = cfg.snapToEdge !== false;
  els.autostart.checked = !!state.autostart;
  els.dataDir.textContent = state.dataDir || '';

  // Pomodoro settings.
  els.pomoOnStartup.checked = !!cfg.pomodoroOnStartup;
  els.pomoAutoNext.checked = !!cfg.autoStartNextPhase;
  els.pomoSound.checked = !!cfg.pomodoroSound;
  els.pomoFocus.value = String(cfg.focusMinutes || 25);
  els.pomoShort.value = String(cfg.shortBreakMinutes || 5);
  els.pomoLong.value = String(cfg.longBreakMinutes || 15);
  els.pomoEvery.value = String(cfg.longBreakEvery || 4);

  // Reminder settings.
  els.remEnabled.checked = !!cfg.reminderEnabled;
  els.remOnStartup.checked = !!cfg.reminderOnStartup;
  els.remDuringBreak.checked = cfg.remindDuringBreak !== false;
  els.remInterval.value = String(cfg.reminderIntervalMin || 30);
  els.remJitter.value = String(cfg.reminderJitterMaxMin ?? 0);
  els.remFrom.value = cfg.reminderFrom || '09:00';
  els.remTo.value = cfg.reminderTo || '22:00';
  els.remItems.value = String(cfg.reminderMaxItems || 3);
  els.remHide.value = String(cfg.reminderAutoHideSec || 12);
  els.remEmpty.checked = !!cfg.remindWhenEmpty;
  renderReminderStatus();

  // Proactive speech settings.
  els.llmProactive.checked = !!cfg.llmProactive;
  els.llmProactiveGap.value = String(cfg.llmProactiveMinGapMin ?? 60);
  renderProactiveStatus();

  // Fixed voice: three switches, rendered from the server's snapshot.
  renderVoiceSettings();

  // Global hide/show shortcuts.
  renderHotkeySettings();

  // Where the encrypted key lives. Shown so the user can find (or delete) it
  // without having to hunt through AppData.
  if (state.llm?.keyFile) {
    els.llmKeyFile.textContent = `密钥保存在：${state.llm.keyFile}（DPAPI 加密，仅当前 Windows 账户可读）`;
  }
}

/** Renders why the next reminder will or will not appear. */
function renderReminderStatus() {
  const r = state?.reminder;
  if (!r) return;
  if (!r.enabled) {
    els.remStatus.textContent = '提醒已关闭。';
    return;
  }
  const parts = [];
  if (r.nextInSec > 0) parts.push(`下次约 ${Math.ceil(r.nextInSec / 60)} 分钟后`);
  if (r.suppressed) parts.push(`当前不打扰（${r.suppressed}）`);
  if (r.missed) parts.push('有错过的提醒，稍后补上');
  els.remStatus.textContent = parts.join(' · ') || '已开启。';
}

/**
 * Renders why the pet will or will not speak up on its own.
 *
 * The server already worked out which gate is closed; this only formats it, so
 * the settings line and the debug log can never disagree about the reason.
 */
function renderProactiveStatus() {
  const el = els.llmProactiveStatus;
  if (!el) return;
  const p = state?.llm?.proactive;
  if (!p) return;
  el.classList.remove('ok', 'error');
  if (!p.enabled) {
    el.textContent = '主动说话已关闭。';
    return;
  }
  const parts = [];
  if (p.nextInSec > 0) parts.push(`下次约 ${Math.ceil(p.nextInSec / 60)} 分钟后`);
  else if (p.lastSpokeAt) parts.push('等待下次机会');
  else parts.push('已开启，等待首次开口');
  if (p.suppressed) parts.push(`当前不会说话（${p.suppressed}）`);
  el.textContent = parts.join(' · ');
  el.classList.add(p.suppressed ? 'error' : 'ok');
}

function applyState(next) {
  if (!next) return;
  state = next;
  renderPet();
  renderTodos();
  renderPomodoro();
  renderRecords();
  renderSettings();
  renderBadge();
  chat?.applyState(next);
  chat?.applySettings(next);
  // The overlay renders from the same snapshot but only while it is showing, so
  // a reply that arrives with the overlay closed does not touch its DOM.
  live?.applyState(next);
  // Surface any data files that had to be reset, once per session.
  if (next.warnings?.length && !applyState._warned) {
    applyState._warned = true;
    toast(next.warnings[0]);
  }
}

// --- actions --------------------------------------------------------------

async function refresh() {
  try {
    applyState(await App.GetState());
    applyLayout(await App.GetLayout());
  } catch (err) {
    fail(err);
  }
}

function resetForm() {
  editingId = null;
  els.form.reset();
  els.submit.textContent = '添加';
}

function startEdit(todo) {
  editingId = todo.id;
  els.title.value = todo.title;
  els.notes.value = todo.notes || '';
  els.due.value = todo.due ? toLocalInput(new Date(todo.due)) : '';
  els.submit.textContent = '保存';
  switchTab('todos');
  els.title.focus();
  els.title.select();
}

async function submitForm(event) {
  event.preventDefault();
  const payload = {
    id: editingId || '',
    title: els.title.value,
    notes: els.notes.value,
    due: els.due.value,
  };
  if (!payload.title.trim()) {
    toast('请先输入内容');
    return;
  }
  try {
    const next = editingId
      ? await App.UpdateTodo(payload)
      : await App.AddTodo(payload);
    resetForm();
    applyState(next);
  } catch (err) {
    fail(err);
  }
}

async function closePanel() {
  try {
    applyLayout(await App.SetPanelOpen(false));
  } catch (err) {
    fail(err);
  }
}

function switchTab(name) {
  els.tabs.forEach((t) => t.classList.toggle('active', t.dataset.tab === name));
  els.tabTodos.classList.toggle('hidden', name !== 'todos');
  els.tabChat.classList.toggle('hidden', name !== 'chat');
  els.tabRecords.classList.toggle('hidden', name !== 'records');
  els.tabSettings.classList.toggle('hidden', name !== 'settings');
  // The conversation's caret is only worth placing when the user has actually
  // arrived at that tab; stealing the keyboard on any tab switch would be rude.
  if (name === 'chat') {
    els.chatLog.scrollTop = els.chatLog.scrollHeight;
    els.chatInput.focus();
  }
}

/**
 * Opens the panel on the conversation tab.
 *
 * The tab is switched here rather than in applyLayout because the layout carries
 * geometry, not which tab is showing. Doing it after the window call means the
 * tab is right even if the geometry call fails.
 */
function openChat() {
  App.OpenChat()
    .then((layout) => {
      applyLayout(layout);
      switchTab('chat');
    })
    .catch((err) => {
      // Still show the tab: if the pet cannot be re-cropped the user should see
      // the conversation rather than nothing happening at all.
      switchTab('chat');
      fail(err);
    });
}

/**
 * Opens the chat popup.
 *
 * This is what a double click on the pet does. The PANEL's 对话 tab is unchanged
 * and remains the permanent record; the popup is the real-time surface. They show
 * the same conversation from the same transcript - the popup just shows the reply
 * as it arrives, floating directly above and below the pet.
 *
 * On a double click this toggles: the same gesture that opens it puts it away
 * again, which is the only way to close it without reaching for Escape.
 *
 * The input is only focused on this explicit user action. The pet speaking on
 * its own (pet:live from Go) must never steal the keyboard, because the user is
 * very likely typing in another application at that moment.
 */
function toggleLive() {
  if (live?.open) {
    closeLive();
    return;
  }
  openLive({ focus: true });
}

/**
 * True while a bubble is holding the slot with a question the user must answer.
 *
 * The picker and the summary prompt are the two. They occupy the same rectangle
 * as the popup, so the popup cannot open beside them - and it must not take the
 * slot from them either, because the summary prompt is the gate on the break:
 * replacing it leaves the timer parked with nothing on screen explaining why.
 * The backend refuses too (OpenLive); checking here as well keeps a refused
 * gesture from becoming a round trip that ends in an invisible error.
 */
function bubbleAwaitingAnswer() {
  return bubble?.kind === 'summary' || bubble?.kind === 'pick';
}

/**
 * Sends the caret to whatever the on-screen question is waiting for.
 *
 * This is what a double click on the pet does when the chat cannot open: the
 * gesture is turned into "go answer it" rather than silently doing nothing. No
 * toast is used, because the toast lives inside the panel and the panel is
 * necessarily closed while a bubble holds the slot - the caret landing in the
 * box IS the explanation.
 */
function focusAnswer() {
  if (bubble?.kind === 'summary') {
    // Only the caret: the user may have typed something already, and selecting
    // it would make the next keystroke replace their own words.
    els.bubbleInput.focus();
    return;
  }
  // The picker: its primary button is the answer ("开始专注").
  els.bubblePrimary.focus();
}

/** Opens the chat popup, optionally taking the keyboard. */
function openLive({ focus = false } = {}) {
  if (bubbleAwaitingAnswer()) {
    focusAnswer();
    return;
  }
  App.OpenLive()
    .then((layout) => {
      applyLayout(layout);
      if (focus) live?.focus();
    })
    .catch(fail);
}

/** Closes the chat popup, if it is open. */
function closeLive() {
  if (!live?.open) return false;
  App.CloseLive()
    .then((layout) => applyLayout(layout))
    .catch(fail);
  return true;
}

// --- pomodoro actions -----------------------------------------------------

/** Confirms the picker's choice and starts focusing. */
function confirmStart() {
  // The design requires a choice before focusing starts; the picker only offers
  // "no todo" when there is genuinely nothing to pick from.
  if (!pickerChoice && !(bubble?.allowUnattached)) {
    toast('请先选择一条待办');
    return;
  }
  App.ConfirmPomodoroStart(pickerChoice || '')
    .then((next) => {
      applyState(next);
      return App.GetLayout();
    })
    .then(applyLayout)
    .catch(fail);
}

/** Saves (or skips) a summary. An empty summary is a legitimate value. */
function saveSummary(recordId, text) {
  App.SavePomodoroSummary(recordId, text)
    .then((next) => {
      applyState(next);
      return App.GetLayout();
    })
    .then(applyLayout)
    .catch(fail);
}

function savePomodoroSettings() {
  App.SavePomodoroSettings({
    onStartup: els.pomoOnStartup.checked,
    autoStartNextPhase: els.pomoAutoNext.checked,
    remindDuringBreak: els.remDuringBreak.checked,
    sound: els.pomoSound.checked,
    focusMinutes: Number(els.pomoFocus.value),
    shortBreakMinutes: Number(els.pomoShort.value),
    longBreakMinutes: Number(els.pomoLong.value),
    longBreakEvery: Number(els.pomoEvery.value),
  })
    .then(applyState)
    .catch(fail);
}

function saveReminderSettings() {
  App.SaveReminderSettings({
    onStartup: els.remOnStartup.checked,
    enabled: els.remEnabled.checked,
    intervalMin: Number(els.remInterval.value),
    jitterMax: Number(els.remJitter.value),
    from: els.remFrom.value,
    to: els.remTo.value,
    maxItems: Number(els.remItems.value),
    autoHideSec: Number(els.remHide.value),
    whenEmpty: els.remEmpty.checked,
    silentFullscreen: true,
  })
    .then(applyState)
    .catch(fail);
}

// --- wiring ---------------------------------------------------------------

function wire() {
  // The chat tab owns its own rendering and streaming state; see chat.js.
  chat = new ChatTab(
    {
      form: els.chatForm,
      input: els.chatInput,
      send: els.chatSend,
      regen: els.chatRegen,
      clear: els.chatClear,
      log: els.chatLog,
      list: els.chatList,
      empty: els.chatEmpty,
      status: els.chatStatus,

      provider: els.llmProvider,
      providerHint: els.llmProviderHint,
      base: els.llmBase,
      model: els.llmModel,
      keyStatus: els.llmKeyStatus,
      usage: els.llmUsage,
      enabled: els.llmEnabled,
      temp: els.llmTemp,
      max: els.llmMax,
      timeout: els.llmTimeout,
      history: els.llmHistory,
      budget: els.llmBudget,
      context: els.llmContext,
      prompt: els.llmPrompt,
    },
    { onState: applyState, fail, debug },
  );
  chat.wire();

  // The popup is a component: it builds its own markup inside #live and owns its
  // own streaming state. main.js only hands it the mount point, the layout
  // callback and the jump to the record.
  live = new ChatPopup(els.live, {
    onState: applyState,
    // The popup positions itself from the geometry Go reports; this is the only
    // way it can ask for a new layout.
    onLayout: (layout) => applyLayout(layout),
    // The jump to the record lives here because it has to switch the panel's tab
    // as well, and that is this file's concern rather than the component's.
    onOpenChat: openChat,
    fail,
    debug,
  });

  // --- pet dragging -------------------------------------------------------
  // Coordinates are captured on pointerdown, but the drag is not started with
  // the backend until the pointer actually moves past the threshold. That way a
  // plain click never triggers the drag path (which collapses the panel), so
  // click-to-toggle stays unambiguous.
  els.pet.addEventListener('pointerdown', (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    debug(`pointerdown btn=${e.button} screen=${e.screenX},${e.screenY}`);
    drag = {
      // Screen coordinates of the pointer, in CSS px.
      startX: e.screenX,
      startY: e.screenY,
      // Filled in from the backend once the drag really begins.
      origin: null,
      startPromise: null,
      moved: false,
      pointerId: e.pointerId,
    };
    try {
      els.pet.setPointerCapture(e.pointerId);
    } catch {
      /* capture is best-effort */
    }
  });

  els.pet.addEventListener('pointermove', (e) => {
    if (!drag || e.pointerId !== drag.pointerId) return;
    const dx = e.screenX - drag.startX;
    const dy = e.screenY - drag.startY;
    if (!drag.moved && Math.abs(dx) < DRAG_THRESHOLD && Math.abs(dy) < DRAG_THRESHOLD) {
      return; // still within the click threshold
    }
    if (!drag.moved) {
      drag.moved = true;
      debug(`drag start dx=${dx} dy=${dy}`);
      // Moving the pet while a bubble is up would leave the bubble pointing at
      // nothing, so the bubble goes away first (docs 9).
      //
      // EXCEPT the summary prompt: it is the gate on the rest, so it must be
      // answered rather than waved away. The backend refuses to dismiss it, and
      // checking here as well keeps the frontend from redrawing a bubble that is
      // still there - which would look like a flicker.
      if (bubble && bubble.kind !== 'summary') App.DismissBubble().catch(() => {});
      // First movement past the threshold: hand over to the backend.
      drag.startPromise = App.StartDrag()
        .then((origin) => {
          drag.origin = origin;
        })
        .catch((err) => {
          console.error(err);
          drag.moved = false; // treat as a click after all
        });
    }
    if (!drag.origin || !drag.startPromise) return;
    // screenX/Y are CSS px; the window API takes physical px.
    const scale = drag.origin.scale || 1;
    App.DragTo(
      Math.round(drag.origin.x + dx * scale),
      Math.round(drag.origin.y + dy * scale),
    ).catch(() => {});
  });

  const endDrag = async (e) => {
    if (!drag || (e && e.pointerId !== drag.pointerId)) return;
    const { moved, startPromise } = drag;
    drag = null;
    debug(`pointerup moved=${moved}`);
    try {
      if (moved && startPromise) {
        // Make sure StartDrag has resolved before snapping, or the snap would
        // race the panel collapse and use a stale rectangle.
        await startPromise;
        applyLayout(await App.EndDrag());
      } else {
        // A press that never moved is a click, but it may still turn out to be
        // the first half of a double click, so the action is held for the
        // system's double-click interval. Acting immediately made a double click
        // unwind as click-click: the panel opened and closed and only then did
        // the chat appear.
        const now = Date.now();
        if (now - lastClickAt < DOUBLE_CLICK_MS) {
          // Second click inside the interval: this is a double click. Cancel the
          // pending single click and open the live conversation. Doing it here
          // rather than on a `dblclick` listener means the two paths cannot
          // disagree.
          lastClickAt = 0;
          clearTimeout(pendingClick);
          pendingClick = null;
          // A double click toggles the popup, so the same gesture that opens it
          // puts it away again.
          toggleLive();
          return;
        }
        lastClickAt = now;
        clearTimeout(pendingClick);
        pendingClick = setTimeout(() => {
          pendingClick = null;
          lastClickAt = 0;
          App.TogglePanel()
            .then(applyLayout)
            .then(() => {
              if (editingId === null) els.title.focus();
            })
            .catch(fail);
        }, DOUBLE_CLICK_MS);
      }
    } catch (err) {
      fail(err);
    }
  };

  els.pet.addEventListener('pointerup', endDrag);
  els.pet.addEventListener('pointercancel', endDrag);

  // The `dblclick` event is not used: the double click is detected in the
  // pointer stream (see endDrag), because pointerdown calls preventDefault and
  // which compatibility mouse events still follow that is not something this
  // code should have to know.

  // Go emits the layout after asynchronous work (initial placement, snapping).
  EventsOn('pet:layout', applyLayout);

  // The overlay appeared or went away.
  //
  // The geometry arrives with the layout, but relying on that ORDER would be
  // fragile: this event is what announces an unprompted remark, and the layout
  // that goes with it is emitted by a separate Go call. Re-reading the layout
  // here makes the handler self-sufficient, so the overlay comes up whether the
  // layout event lands before or after this one.
  EventsOn('pet:live', (payload) => {
    if (payload?.shown === false) return;
    App.GetLayout().then(applyLayout).catch(() => {});
    // Refresh the round, but do NOT focus the input: the user did not ask for
    // this remark and is very likely typing in another application.
    if (state) live?.applyState(state);
    else App.GetState().then((next) => live?.applyState(next)).catch(() => {});
  });

  // Go asks for one of the two panel tabs (the tray, or the conversation's own
  // "待办" button). The payload says which, because the layout carries geometry
  // rather than which tab is showing.
  EventsOn('pet:chat-open', (payload) => {
    switchTab(payload?.open === false ? 'todos' : 'chat');
  });

  // The pet was hidden or shown from outside the page (the global shortcut, or
  // the tray). The window is hidden natively - the page is not told to hide
  // itself - so this only has to keep the panel's rendering honest: the settings
  // panel reports the shortcut state, and a hidden pet also had its panel and
  // bubbles folded away by Go.
  EventsOn('pet:visible', (payload) => {
    const visible = payload?.visible !== false;
    if (state) state.petVisible = visible;
    // Re-read rather than guess: Go changed the panel, the bubble and the
    // overlay together, and the layout is the only thing that says what is
    // actually being painted now.
    App.GetLayout().then(applyLayout).catch(() => {});
    App.GetState()
      .then((next) => applyState(next))
      .catch(() => {});
  });

  // The timer snapshot arrives on its own so the countdown ticks without a full
  // state refresh every second.
  EventsOn('pet:pomodoro', (view) => {
    if (state) state.pomodoro = view;
    renderTimerBadge();
    renderPomodoro();
  });

  // Panel interaction.
  els.close.addEventListener('click', closePanel);
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      // A confirmation dialog swallows Escape: it is the innermost thing on
      // screen, and closing the whole panel out from under it would leave the
      // pending action ambiguous.
      if (confirmOpen()) return;
      // Escape unwinds the most transient thing first: an edit, then a bubble,
      // then the overlay, then the panel.
      if (editingRecordId !== null) {
        editingRecordId = null;
        renderRecords();
      } else if (bubble) {
        // The summary prompt is the gate on the rest, so Escape does not close it:
        // it has to be answered. The backend refuses too, so this only saves a
        // round trip - but it also stops the panel fall-through below, which would
        // otherwise close the panel behind a question that is still waiting.
        if (bubble.kind !== 'summary') {
          App.DismissBubble().then(applyLayout).catch(fail);
        }
      } else if (live?.open) {
        closeLive();
      } else if (editingId !== null) {
        resetForm();
      } else {
        closePanel();
      }
    }
  });

  els.tabs.forEach((tab) => {
    tab.addEventListener('click', () => switchTab(tab.dataset.tab));
  });

  // --- conversation tab ----------------------------------------------------
  // The two surfaces share the panel, so this is the jump back.
  els.chatTodos.addEventListener('click', () => {
    App.OpenTodoPanelFromChat()
      .then((layout) => {
        applyLayout(layout);
        switchTab('todos');
      })
      .catch(fail);
  });

  els.form.addEventListener('submit', submitForm);

  els.clearDone.addEventListener('click', () => {
    App.ClearDone().then(applyState).catch(fail);
  });

  // --- bubble hover -------------------------------------------------------
  // Hovering a reminder bubble pauses its countdown; moving away restarts it.
  els.bubble.addEventListener('pointerenter', () => {
    if (bubble?.kind !== 'reminder') return;
    clearTimeout(bubbleHideTimer);
    bubbleHideTimer = null;
    App.SetBubbleHover(true).catch(() => {});
  });
  els.bubble.addEventListener('pointerleave', () => {
    if (bubble?.kind !== 'reminder') return;
    App.SetBubbleHover(false).catch(() => {});
    scheduleBubbleHide(bubble);
  });

  // --- pomodoro strip -----------------------------------------------------
  els.pomoToggle.addEventListener('click', () => {
    App.SetPomodoroEnabled(!state?.pomodoro?.enabled).then(applyState).catch(fail);
  });
  els.pomoStart.addEventListener('click', () => {
    App.ShowPomodoroPicker().then(applyLayout).catch(fail);
  });
  els.pomoPause.addEventListener('click', () => {
    App.PausePomodoro().then(applyState).catch(fail);
  });
  els.pomoResume.addEventListener('click', () => {
    App.ResumePomodoro().then(applyState).catch(fail);
  });
  els.pomoStop.addEventListener('click', () => {
    App.StopPomodoro().then(applyState).catch(fail);
  });
  els.pomoSkip.addEventListener('click', async () => {
    // Skipping a focus phase is the one way to leave a session out of the
    // permanent archive, so it is confirmed.
    const p = state?.pomodoro;
    const warn = p?.phase === 'focus' ? '跳过这个专注不会生成记录，确定吗？' : '跳过当前阶段？';
    if (!(await confirmDialog(warn, '跳过'))) return;
    App.SkipPhase().then(applyState).catch(fail);
  });
  els.pomoReset.addEventListener('click', async () => {
    if (!(await confirmDialog('彻底放弃当前番茄钟（不会生成记录）？', '放弃'))) return;
    App.ResetPomodoro().then(applyState).catch(fail);
  });

  // Settings.
  els.chooseImage.addEventListener('click', () => {
    App.ChoosePetImage().then(applyState).catch(fail);
  });

  els.resetImage.addEventListener('click', () => {
    App.ResetPetImage().then(applyState).catch(fail);
  });

  els.scale.addEventListener('input', () => {
    els.scaleValue.textContent = `${els.scale.value}%`;
  });
  els.scale.addEventListener('change', () => {
    // Only commit on release: each call resizes the native window.
    App.SetScale(Number(els.scale.value) / 100)
      .then(applyLayout)
      .catch(fail);
  });

  els.snap.addEventListener('change', () => {
    App.SetSnapToEdge(els.snap.checked).then(applyState).catch(fail);
  });

  els.autostart.addEventListener('change', () => {
    const wanted = els.autostart.checked;
    App.SetAutostart(wanted)
      .then((next) => {
        applyState(next);
        toast(wanted ? '已开启开机自启' : '已关闭开机自启');
      })
      .catch((err) => {
        // Revert the checkbox: the registry write failed.
        els.autostart.checked = !wanted;
        fail(err);
      });
  });

  // Pomodoro settings: committed on change so there is no separate save button.
  for (const el of [
    els.pomoOnStartup,
    els.pomoAutoNext,
    els.pomoSound,
    els.pomoFocus,
    els.pomoShort,
    els.pomoLong,
    els.pomoEvery,
    els.remDuringBreak,
  ]) {
    el.addEventListener('change', savePomodoroSettings);
  }

  // Reminder settings.
  for (const el of [
    els.remEnabled,
    els.remOnStartup,
    els.remInterval,
    els.remJitter,
    els.remFrom,
    els.remTo,
    els.remItems,
    els.remHide,
    els.remEmpty,
  ]) {
    el.addEventListener('change', saveReminderSettings);
  }

  els.remNow.addEventListener('click', () => {
    App.RemindNow()
      .then((next) => {
        applyState(next);
        return App.GetLayout();
      })
      .then(applyLayout)
      .catch(fail);
  });

  els.quit.addEventListener('click', () => {
    App.Quit().catch(() => {});
  });

  wireLLMSettings();
  wireVoiceSettings();
  wireHotkeySettings();
}

// --- LLM settings ---------------------------------------------------------

/**
 * Collects the LLM settings form into the payload Go expects.
 *
 * It is a function rather than five copies of the field list so the "save on
 * change" handlers and any explicit save stay in step by construction.
 */
function llmSettingsPayload() {
  return {
    enabled: els.llmEnabled.checked,
    provider: els.llmProvider.value,
    baseUrl: els.llmBase.value.trim(),
    model: els.llmModel.value.trim(),
    temperature: Number(els.llmTemp.value),
    maxTokens: Number(els.llmMax.value),
    timeoutSec: Number(els.llmTimeout.value),
    systemPrompt: els.llmPrompt.value,
    historyTurns: Number(els.llmHistory.value),
    contextTodos: els.llmContext.checked,
    proactive: els.llmProactive.checked,
    proactiveMinGapMin: Number(els.llmProactiveGap.value),
    dailyBudget: Number(els.llmBudget.value),
  };
}

/** Persists the LLM settings form. */
function saveLLMSettings() {
  App.SaveLLMSettings(llmSettingsPayload()).then(applyState).catch(fail);
}

/** Wires the LLM part of the settings tab. */
function wireLLMSettings() {
  // Committed on change, like the pomodoro settings: there is no separate save
  // button for the fields, only for the key (which must not be written on every
  // keystroke).
  for (const el of [
    els.llmEnabled,
    els.llmTemp,
    els.llmMax,
    els.llmTimeout,
    els.llmHistory,
    els.llmBudget,
    els.llmContext,
    els.llmProactive,
    els.llmProactiveGap,
  ]) {
    el.addEventListener('change', saveLLMSettings);
  }
  // The two free-text fields are committed on blur, so a half-typed URL is never
  // saved and a request is never sent to it.
  els.llmBase.addEventListener('blur', saveLLMSettings);
  els.llmModel.addEventListener('blur', saveLLMSettings);
  els.llmPrompt.addEventListener('blur', saveLLMSettings);

  // Choosing a preset fills in its endpoint and model.
  els.llmProvider.addEventListener('change', () => {
    const preset = (state?.llm?.providers || []).find((p) => p.id === els.llmProvider.value);
    if (preset && preset.id !== 'custom') {
      els.llmBase.value = preset.baseUrl;
      els.llmModel.value = preset.model;
    }
    chat?.renderProviderHint();
    saveLLMSettings();
  });

  els.llmKeySave.addEventListener('click', () => {
    const key = els.llmKey.value.trim();
    if (!key) {
      toast('请先粘贴密钥');
      return;
    }
    App.SetLLMKey(key)
      .then((next) => {
        // Clear the field: the key is stored encrypted, and leaving it in a form
        // field keeps it in the DOM for as long as the panel is open.
        els.llmKey.value = '';
        applyState(next);
        toast('密钥已加密保存');
      })
      .catch(fail);
  });

  els.llmKeyClear.addEventListener('click', () => {
    App.ClearLLMKey()
      .then((next) => {
        applyState(next);
        const source = next?.llm?.keySource;
        toast(source === 'env' ? '环境变量里的密钥无法从这里删除' : '已删除保存的密钥');
      })
      .catch(fail);
  });

  els.llmBudgetReset.addEventListener('click', () => {
    App.ResetLLMUsage().then(applyState).catch(fail);
  });

  // "Say something now" is the manual escape hatch, exactly as "立刻提醒一次" is
  // for the reminder schedule: no gate may refuse a request the user just made.
  els.llmSpeakNow.addEventListener('click', async () => {
    try {
      applyState(await App.SpeakNow());
      // The pet speaking is the case the overlay exists for, so it comes up.
      openLive();
    } catch (err) {
      fail(err);
    }
  });

  els.llmTest.addEventListener('click', async () => {
    const el = els.llmTestStatus;
    el.classList.remove('ok', 'error');
    el.textContent = '正在测试…';
    els.llmTest.disabled = true;
    try {
      // Save first: testing a form that has not been saved would report on the
      // previous configuration, which is exactly the confusing outcome the
      // button exists to prevent.
      await App.SaveLLMSettings(llmSettingsPayload());
      const res = await App.TestLLMConnection();
      if (res?.ok) {
        el.textContent = `连接成功：${res.model || '(未报告模型)'} · ${res.elapsedMs}ms`;
        el.classList.add('ok');
      } else {
        el.textContent = `失败：${res?.error || '未知原因'}`;
        el.classList.add('error');
      }
      applyState(await App.GetState());
    } catch (err) {
      el.textContent = `失败：${err?.message || err}`;
      el.classList.add('error');
    } finally {
      els.llmTest.disabled = false;
    }
  });
}

// --- fixed voice ----------------------------------------------------------

/**
 * Collects the voice switches into the payload Go expects.
 *
 * A function rather than repeated field lists, for the same reason
 * llmSettingsPayload is one: the change handlers and the explicit saves cannot
 * drift apart.
 *
 * There are only three fields, and there is nothing to type in. That is the whole
 * point of the fixed-line design: the clips are embedded in the executable, so
 * there is no endpoint, no character path and no service to start - turning the
 * switch on IS the setup.
 */
function voiceSettingsPayload() {
  return {
    enabled: els.voiceEnabled.checked,
    pomodoro: els.voicePomodoro.checked,
    reminder: els.voiceReminder.checked,
  };
}

/** Persists the voice switches. */
function saveVoiceSettings() {
  App.SaveVoiceSettings(voiceSettingsPayload()).then(applyState).catch(fail);
}

/** Renders the fixed-voice section from the server's snapshot. */
function renderVoiceSettings() {
  const v = state?.voice || {};
  els.voiceEnabled.checked = v.enabled === true;
  els.voicePomodoro.checked = v.pomodoro === true;
  els.voiceReminder.checked = v.reminder === true;

  // The per-notice switches are only meaningful with the master switch on, so they
  // are disabled rather than left clickable: a switch that appears to work but
  // cannot take effect is worse than one that is visibly inactive.
  els.voicePomodoro.disabled = !v.enabled;
  els.voiceReminder.disabled = !v.enabled;
  els.voiceTest.disabled = !v.enabled;

  // Which character is in effect, which character the BUILT-IN clips are, and how
  // many lines the current character has actually replaced. All three come from Go
  // rather than being assumed here: the built-in character is a fact about the
  // build, while the current character and the replaced count are read from the
  // shared library the studio writes.
  //
  // This is the answer to "I changed the character and still hear the old one": the
  // selection is now what the pet actually speaks as, and the panel reports both it
  // and the built-in character so the two can never be confused for one another.
  //
  // A stored character that no longer exists is a THIRD state, and it is reported as
  // such: nothing plays at all then, so showing the built-in character's name beside
  // the error would claim a working voice that the pet does not have.
  const roleBroken = !!v.roleError;
  if (els.voiceActiveRole) {
    els.voiceActiveRole.textContent = roleBroken ? '角色配置有误' : v.activeRoleName || '内置';
  }
  if (els.voiceBuiltinNote) {
    // Only worth saying when a user character is in effect, or the two names would
    // be printed side by side and read as a contradiction. The flag comes from Go
    // because a user role may legitimately be NAMED 可莉.
    els.voiceBuiltinNote.textContent = v.activeRoleIsBuiltIn || roleBroken
      ? ''
      : `（内置音色是 ${v.builtInRole || '内置'}）`;
  }
  if (els.voiceReplacedNote) {
    const n = v.replaced || 0;
    els.voiceReplacedNote.textContent = roleBroken
      ? ` · ${v.roleError}，请打开配音工坊重新选一个角色`
      : n > 0
        ? `，已换成该音色的 ${n} 条`
        : '，还没有该音色的台词';
  }

  const el = els.voiceStatus;
  el.classList.remove('ok', 'error');
  if (!v.enabled) {
    el.textContent = '语音朗读已关闭。';
    return;
  }
  el.textContent = `已就绪：${v.lines || 0} 条内置语音，点击即时播放。`;
  el.classList.add('ok');
}

/** Wires the fixed-voice switches. */
function wireVoiceSettings() {
  for (const el of [els.voiceEnabled, els.voicePomodoro, els.voiceReminder]) {
    el.addEventListener('change', () => {
      // The master switch changes which of the others are usable, so the form is
      // re-rendered from the saved state rather than left showing stale disabled
      // flags.
      saveVoiceSettings();
    });
  }

  els.voiceTest.addEventListener('click', () => {
    App.TestVoice()
      .then((next) => {
        applyState(next);
        toast('试听中…');
      })
      .catch(fail);
  });

  // The escape hatch: nothing may refuse a stop the user just asked for.
  els.voiceStop.addEventListener('click', () => {
    App.StopVoice().then(applyState).catch(fail);
  });

  // Open the standalone voice studio.
  //
  // It is a separate PROGRAM, not a tab: the panel is a fixed 380x520 rectangle the
  // OS crops the window to, and a workspace that lists five lines with several takes
  // each does not fit in it. Launching rather than telling the user to find it is
  // the difference between a feature and a chore.
  els.voiceOpenStudio.addEventListener('click', () => {
    App.OpenVoiceStudio()
      .then(() => toast('已打开配音工坊'))
      .catch(fail);
  });
}

// --- global hide/show shortcuts -------------------------------------------

/**
 * Renders the shortcut switch, its key labels and whether they actually took.
 *
 * The labels come from Go rather than being written here: the registered keys and
 * the advertised keys are then the same value, so the panel cannot tell the user
 * to press something that was never registered.
 */
function renderHotkeySettings() {
  const h = state?.hotkeys || {};
  els.hotkeyEnabled.checked = !!h.enabled;

  if (h.show) els.hotkeyShow.textContent = h.show;
  if (h.hide) els.hotkeyHide.textContent = h.hide;

  const el = els.hotkeyStatus;
  el.classList.remove('ok', 'error');

  // Three distinct states, and the difference matters to the user: switched off,
  // switched on but refused by Windows, and working.
  if (!h.enabled) {
    el.textContent = h.note || '全局快捷键已关闭。';
    return;
  }
  if (h.problems?.length) {
    el.textContent = `未能生效：${h.problems.join('；')}`;
    el.classList.add('error');
    return;
  }
  if (h.active) {
    el.textContent = `已生效：在任意程序里按 ${h.show} 显示，按 ${h.hide} 隐藏。`;
    el.classList.add('ok');
    return;
  }
  // Enabled, no reported problem, but not active: the OS has not answered yet.
  el.textContent = h.note || '正在注册快捷键…';
}

/** Wires the shortcut switch. */
function wireHotkeySettings() {
  // Committed on change, like the other switches: there is no save button, and
  // turning it off has to give the key combinations back immediately.
  els.hotkeyEnabled.addEventListener('change', () => {
    App.SaveHotkeySettings({ enabled: els.hotkeyEnabled.checked })
      .then((next) => {
        applyState(next);
        const h = next?.hotkeys;
        if (h?.enabled && h.active) toast('快捷键已开启');
        else if (h?.enabled && h.problems?.length) toast('快捷键被占用，未能注册');
        else if (!h?.enabled) toast('快捷键已关闭');
      })
      .catch((err) => {
        // Revert the checkbox: the store write failed, so the switch is a lie.
        els.hotkeyEnabled.checked = !els.hotkeyEnabled.checked;
        fail(err);
      });
  });
}

// --- boot -----------------------------------------------------------------

/**
 * Resolves once the pet image is decoded and ready to paint.
 *
 * Checking naturalWidth only proves the bytes arrived; the decoder may still
 * run asynchronously, so revealing the window right then can catch a frame with
 * an empty pet. `img.decode()` resolves only once the image can be painted
 * without further work, which is what we actually need before showing the
 * window.
 *
 * Falls back to the load event (and finally a timeout) so a broken or
 * unsupported image can never leave the window hidden forever.
 */
async function petImageReady() {
  const img = els.petImage;
  if (!img.src) return;
  try {
    if (typeof img.decode === 'function') {
      await img.decode();
      return;
    }
  } catch {
    // decode() rejects for a broken image; fall through to the load event.
  }
  if (img.complete && img.naturalWidth > 0) return;
  await new Promise((resolve) => {
    const done = () => resolve();
    img.addEventListener('load', done, { once: true });
    img.addEventListener('error', done, { once: true });
    setTimeout(done, 2000);
  });
}

/**
 * Waits for `count` animation frames to elapse.
 *
 * Waiting one frame is not enough to guarantee pixels are on screen. The DOM
 * commits in this process, but WebView2 renders in a separate process and
 * presents the result a few frames later; showing the window in between catches
 * a frame where the page is laid out but the pet has not been composited yet,
 * which reads as a flicker. A handful of frames covers that handoff, and the
 * window is still hidden, so the wait costs nothing visually.
 */
function nextPaints(count = 5) {
  return new Promise((resolve) => {
    let left = count;
    const tick = () => (left-- > 0 ? requestAnimationFrame(tick) : resolve());
    requestAnimationFrame(tick);
  });
}

/**
 * First paint: load state, apply the layout, wait for the picture, then reveal
 * the window. The window starts hidden (StartHidden in main.go) so the
 * decorated frame Wails creates during startup is never on screen.
 */
async function boot() {
  await refresh();
  await petImageReady();
  await nextPaints();
  try {
    await App.Ready();
  } catch (err) {
    // If the reveal fails the window would stay hidden, which is worse than a
    // flash: fall back to asking Wails to show it.
    console.error('Ready failed', err);
    try {
      window.runtime?.WindowShow?.();
    } catch {
      /* nothing more we can do */
    }
  }
}

wire();
boot();

// Keep the badge, the countdown and the reminder status honest between full
// refreshes.
setInterval(() => {
  if (!state) return;
  renderBadge();
  renderReminderStatus();
  // Re-read the schedule so the "next reminder in N minutes" hint stays true.
  App.GetState()
    .then((next) => {
      state = next;
      renderTimerBadge();
      renderPomodoro();
      renderReminderStatus();
    })
    .catch(() => {});
}, 30000);

// The countdown between Go's events.
//
// Go already pushes a snapshot every second, but the display must not depend on
// an event arriving: a dropped or late event would freeze or jump the number.
// This tick therefore recomputes the remainder from the phase's ABSOLUTE end
// time (endsAtMs) rather than decrementing a counter.
//
// That distinction is the whole fix for the countdown flickering. Decrementing a
// local copy raced the events: Go rounds its seconds up while the local tick
// rounded down, so the two fought over the same digit every second. Deriving
// both from the same instant, and applying Go's own rounding (ceil) here, makes
// them agree by construction and lets this tick self-correct any drift.
function tickCountdown() {
  const p = state?.pomodoro;
  if (!p || !p.enabled || !p.running || !p.endsAtMs) return;
  const msLeft = p.endsAtMs - Date.now();
  // Ceil, exactly like ceilSeconds on the Go side.
  const secs = msLeft <= 0 ? 0 : Math.ceil(msLeft / 1000);
  p.remainingSec = secs;
  p.countdown = `${String(Math.floor(secs / 60)).padStart(2, '0')}:${String(secs % 60).padStart(2, '0')}`;
  renderTimerBadge();
  renderPomodoro();
}

setInterval(tickCountdown, 250);
