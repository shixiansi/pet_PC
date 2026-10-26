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
// The slot name is imported rather than written out here: the host resolves the
// slot from the installed list and then mounts into it, so the two halves of
// "which slot is the conversation" have to be the same string by construction.
import { PluginHost, SLOT_CHAT } from './plugin-host';

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
  // The 对话 tab's BUTTON, as opposed to tabChat which is the tab's body. The
  // button is hidden while no plugin fills the slot, so the two are separate
  // elements with separate visibility.
  tabChatBtn: $('tab-chat-btn'),
  // Two tabs: 待办 (which now also holds the folded 记录 archive) and 对话 (a plugin slot).
  // 记录 and 设置 are gone - see index.html's tab-strip comment for where each went.

  // The chat plugin's mount point inside the 对话 tab, the floating surface's
  // mount point, and the headless frame's mount point. All three are fill-only
  // containers: what goes inside them is built by plugin-host.js, and they are
  // independent of each other.
  chatSlot: $('chat-slot'),
  pluginSurface: $('plugin-surface'),
  pluginBackground: $('plugin-background'),

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

  // Finished sessions, folded into the 待办 tab as a collapsed archive.
  recordsList: $('records-list'),
  recordsEmpty: $('records-empty'),
  recordsTotal: $('records-total'),

  // Bubble (reminder / picker / summary / plan).
  bubble: $('bubble'),
  bubbleArrow: $('bubble-arrow'),
  bubbleTitle: $('bubble-title'),
  bubbleTodo: $('bubble-todo'),
  bubblePlan: $('bubble-plan'),
  bubbleList: $('bubble-list'),
  bubbleMore: $('bubble-more'),
  bubbleInput: $('bubble-input'),
  bubbleActions: $('bubble-actions'),
  bubbleSecondary: $('bubble-secondary'),
  bubblePrimary: $('bubble-primary'),

  // No settings entries: the settings tab moved to the control center, and the few
  // controls that could not move are in the pomodoro strip below (放弃) or the tray menu
  // (打开控制中心 / 退出 / 立刻提醒 / 打开数据目录). An `els` entry for an element the page no
  // longer has would be a null that fails at the first click.

  confirm: $('confirm'),
  confirmText: $('confirm-text'),
  confirmOk: $('confirm-ok'),
  confirmCancel: $('confirm-cancel'),

  pomoReset: $('pomo-reset'),

  // Speech has NO entries here: the voice plugin owns its switches, and it is
  // configured in the plugin market's own window. See the "speech" section near the
  // bottom of this file for why the pet has nothing to render.
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

/** The sandboxed-iframe host for plugin interfaces, created in wire(). */
let pluginHost = null;
/**
 * The installed plugins, as of the last PluginsInstalled() call, or null before
 * the first one.
 *
 * The 对话 tab's plugin is resolved from this list rather than from a second
 * binding: the market program can install a plugin while the pet is running, so
 * the list has to be re-read rather than remembered from startup. It is also
 * what tells the floating surface's iframes which permissions were granted,
 * which is a fact about the installed plugin rather than about the layout.
 */
let installedPlugins = null;

/**
 * True while an on-demand read of the installed list is in flight.
 *
 * The floating surface can be opened by double-clicking the pet WITHOUT the 对话
 * tab ever having been visited, so the list may not be read yet. That read is
 * kicked off from `applyLayout`, which runs on every state push - so without this
 * flag one un-read list would start a read per push.
 */
let installedRefreshInFlight = false;

/**
 * The most recent layout Go sent.
 *
 * Kept so a surface that arrived before the installed list was known can be
 * re-applied once the list arrives: the frames already exist with their geometry,
 * and re-applying is what hands them their entry file and their grants.
 */
let lastLayout = null;

/**
 * Whether the panel was open as of the last applyLayout, and null before the
 * first one.
 *
 * It exists so the open TRANSITION can be told from "still open": the plugin slot
 * is re-read the moment the tab strip becomes visible, but not on every layout
 * call, which would add a backend read to each pet nudge.
 */
let panelOpenState = null;

/**
 * The watcher that keeps the 对话 tab in step with the installed plugin, or null
 * when it is not running.
 *
 * See setChatTabWatch. It runs while the panel is OPEN, because that is when the
 * tab strip is on screen - and both directions of change (a plugin appearing and a
 * plugin going away) have to be noticed, which is what the first version of this,
 * bound to the tab being hidden, got wrong.
 */
let chatTabWatchTimer = null;

/**
 * True while a read of the installed list is in flight.
 *
 * refreshChatSlot runs from three places now (the tab, the watch timer, and the
 * panel opening), so two reads can overlap; this keeps the older one from landing
 * after the newer one.
 */
let chatSlotReadInFlight = false;

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
  // Whether the panel was open BEFORE this layout, for the open-transition
  // refresh below.
  //
  // It is captured here rather than read off `lastLayout` afterwards, because
  // `lastLayout` is assigned the NEW layout a few lines down - reading it there
  // would compare the new value with itself and the transition test would never
  // fire.
  const panelWasOpen = panelOpenState;
  panelOpenState = !!layout.panelOpen;
  // Remembered for the deferred surface handshake below: a surface can arrive
  // before the installed list has been read, and it is completed by re-applying
  // this same layout once the list lands.
  lastLayout = layout;

  Object.assign(els.pet.style, {
    left: `${layout.petX}px`,
    top: `${layout.petY}px`,
    width: `${layout.petW}px`,
    height: `${layout.petH}px`,
  });

  els.panel.classList.toggle('hidden', !layout.panelOpen);
  els.pet.classList.toggle('anchored', !!layout.panelOpen);

  // Keep the 对话 tab in step with the installed plugin for as long as the tab
  // strip is on screen.
  //
  // The strip's observable state changes outside this process - the market program
  // installs, updates, disables and removes plugins, and Go carries none of that in
  // State - so the tab is refreshed by re-reading the installed list, and this is
  // the bound that decides when.
  //
  // The panel's visibility is the right bound because the strip is invisible when
  // the panel is closed: a poll then would spend a round trip per period updating
  // something nobody can see. See setChatTabWatch for why "while the tab is hidden"
  // - the first attempt - missed the case the user reported.
  setChatTabWatch(!!layout.panelOpen);
  if (layout.panelOpen && panelWasOpen === false) {
    // The moment the strip becomes visible, re-read rather than waiting out a
    // period: a change made while the panel was closed otherwise shows for up to
    // 1.5s. This is the transition only (not every layout call), so a layout
    // update that does not open the panel costs nothing.
    refreshChatSlot();
  }

  if (layout.panelOpen) {
    Object.assign(els.panel.style, {
      left: `${layout.panelX}px`,
      top: `${layout.panelY}px`,
      width: `${layout.panelW}px`,
      height: `${layout.panelH}px`,
    });
  }

  applyBubble(layout.bubble || null);

  // The plugin floating surface is placed from the layout the same way: the
  // rectangles it is handed ARE the ones the window was cropped to, so nothing
  // here derives a position or a size of its own.
  const surface = layout.pluginSurface || null;
  if (surface) {
    // The surface needs the plugin's METADATA (its entry file and its grants), and
    // that comes from the installed list - which is read when the 对话 tab is
    // opened, NOT at startup. Opening the surface by double-clicking the pet can
    // therefore arrive before the list has ever been read, and the old code passed
    // an empty view in that case: the iframe got a default entry and no grants, and
    // the plugin rendered as unauthorised (or asked for an asset path that does not
    // exist). Reading the list on demand is what makes the surface independent of
    // which tab the user happened to visit first.
    if (!pluginViewFor(surface.pluginId)) {
      refreshInstalledThenApply(pluginViewFor);
    }
  }
  pluginHost?.applyGeometry(surface, pluginViewFor(surface?.pluginId));
}

/**
 * Reads the installed list and re-runs `applyLayout`, so a surface that arrived
 * before the list was known is completed rather than rendered half-empty.
 *
 * It is fire-and-forget on purpose: `applyLayout` is idempotent, and the failing
 * case (the read fails) leaves the surface showing what it had, which is the same
 * behaviour as before - a surface with geometry but no metadata - rather than
 * taking the whole window down.
 */
function refreshInstalledThenApply() {
  if (installedRefreshInFlight) return;
  installedRefreshInFlight = true;
  App.PluginsInstalled()
    .then((rows) => {
      installedPlugins = rows || [];
      renderChatSlot();
      renderBackgroundPlugins();
      // Re-apply so the frames that were created without metadata get theirs.
      if (lastLayout) applyLayout(lastLayout);
    })
    .catch(() => {})
    .finally(() => {
      installedRefreshInFlight = false;
    });
}

/**
 * Finds the installed plugin a surface belongs to, for the iframe's entry and
 * its permission set, or null when the list does not know it yet.
 *
 * A null result is NOT the same as "no such plugin": it means "not read yet", and
 * the caller above treats it as a reason to read the list rather than as a final
 * answer.
 */
function pluginViewFor(pluginId) {
  if (!pluginId) return null;
  const found = (installedPlugins || []).find((p) => p.id === pluginId);
  if (!found) return null;
  return {
    // The version travels with the view so the surface's frames can tell an update
    // from a plain re-layout: the id is unchanged by an update, so it is the only
    // thing that distinguishes "same document, rebuilt" from "the plugin changed".
    version: found.version || '',
    entry: found.entry,
    permissions: found.permissions,
    granted: found.granted,
  };
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
  // Reset everything first: the kinds share the card.
  els.bubbleList.replaceChildren();
  els.bubbleList.classList.add('hidden');
  els.bubbleTodo.classList.add('hidden');
  els.bubblePlan.classList.add('hidden');
  els.bubbleMore.classList.add('hidden');
  els.bubbleInput.classList.add('hidden');
  // The Enter handler is installed per kind, so a stale one from the previous
  // prompt must not survive into the next render.
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

  if (b.kind === 'plan') {
    // The plan question. It is asked before a focus starts, so it has no record
    // to name - and it must be answerable with nothing at all, which is why both
    // buttons submit rather than one of them only closing the card.
    if (b.todoTitle) {
      els.bubbleTodo.textContent = `待办：${b.todoTitle}`;
      els.bubbleTodo.classList.remove('hidden');
    }
    // Go offers the PREVIOUS session's plan as a starting value: a user who is
    // still on the same task then only confirms it.
    els.bubbleInput.value = b.plan || '';
    els.bubbleInput.classList.remove('hidden');
    // Written at RUNTIME, not in index.html: the textarea's attribute belongs to
    // the summary prompt, and this branch has to be able to hand it back (see the
    // summary branch below). A static attribute could not describe both prompts.
    els.bubbleInput.placeholder = '这件事是…（可留空，Enter 开始）';
    els.bubbleSecondary.textContent = '跳过';
    els.bubbleSecondary.classList.remove('hidden');
    els.bubbleSecondary.onclick = () => startWithPlan('');
    els.bubblePrimary.textContent = '开始专注';
    els.bubblePrimary.classList.remove('hidden');
    els.bubblePrimary.onclick = () => startWithPlan(els.bubbleInput.value);

    // Enter submits, exactly as it does on the summary prompt and in the chat
    // box. The reasoning is the same and is spelled out there: Shift+Enter is a
    // newline, and an IME's Enter commits a candidate rather than answering.
    els.bubbleInput.onkeydown = (e) => {
      if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
        e.preventDefault();
        startWithPlan(els.bubbleInput.value);
      }
    };

    // Deliberately NOT focused: the question can appear while the user is typing
    // in another application (same rule as the summary prompt).
    if (document.hasFocus()) els.bubbleInput.focus();
    return;
  }

  // Summary prompt.
  if (b.todoTitle) {
    els.bubbleTodo.textContent = `待办：${b.todoTitle}`;
    els.bubbleTodo.classList.remove('hidden');
  }
  // What the user set out to do, shown above the answer. The record's own plan is
  // used, so the line survives a restart; with no plan there is nothing to say
  // and the line stays hidden rather than showing an empty 「计划：」.
  if (b.plan) {
    els.bubblePlan.textContent = `计划：${b.plan}`;
    els.bubblePlan.classList.remove('hidden');
  }
  els.bubbleInput.value = '';
  els.bubbleInput.classList.remove('hidden');
  // The shared textarea carries the plan prompt's wording too, and this branch
  // runs after it whenever the two prompts follow each other. index.html's
  // attribute is left alone: it is the summary prompt's own text, so restoring it
  // here is what keeps the two prompts from sharing the wrong hint.
  els.bubbleInput.placeholder = '写点什么…（Enter 保存）';
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
  // Same rule as 跳过: there has to be a session to abandon. 停止 is the reversible one and
  // 放弃 is not, which is why the button carries its own confirmation in the click handler.
  els.pomoReset.classList.toggle('hidden', !p.enabled || idle);
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
    // BOTH fields are editable in one form and saved in one write: a page that
    // wrote them separately could leave the record half-edited if the second call
    // failed, and a plan that can be set but never corrected would be worse than
    // no plan at all.
    const inputs = document.createElement('div');
    inputs.className = 'record-inputs';

    const planInput = document.createElement('textarea');
    planInput.rows = 2;
    planInput.maxLength = 2000;
    planInput.value = rec.plan || '';
    planInput.className = 'record-input';
    planInput.placeholder = '计划：这一段要做什么';
    planInput.setAttribute('aria-label', '计划');

    const summaryInput = document.createElement('textarea');
    summaryInput.rows = 2;
    summaryInput.maxLength = 2000;
    summaryInput.value = rec.summary || '';
    summaryInput.className = 'record-input';
    summaryInput.placeholder = '实际：最后做了什么';
    summaryInput.setAttribute('aria-label', '实际');

    inputs.append(planInput, summaryInput);

    const actions = document.createElement('div');
    actions.className = 'record-actions';
    const save = document.createElement('button');
    save.type = 'button';
    save.className = 'primary';
    save.textContent = '保存';
    save.onclick = () => {
      // One call, both fields: an empty plan is a legitimate value (the user may
      // have decided the session had no plan after all).
      App.EditPomodoroRecord(rec.id, planInput.value, summaryInput.value)
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
      // Cancel only leaves edit mode: the archive is not touched, so the re-render
      // puts the stored values back.
      editingRecordId = null;
      renderRecords();
    };
    actions.append(save, cancel);
    body.append(inputs, actions);
  } else {
    // Two fixed lines, plan first. The plan falls back to the todo the session was
    // associated with: for records written before plans existed (and for sessions
    // nobody planned) that name IS what the user set out to do, and an empty line
    // would lose the association the row used to carry.
    const planText = rec.plan || rec.todoTitle || '未关联待办';
    const plan = document.createElement('div');
    plan.className = 'record-line record-plan';
    plan.textContent = `计划：${planText}`;

    // The 实际 line is the one that can be edited, so the 改 control lives here.
    const real = document.createElement('div');
    real.className = 'record-line';
    const text = document.createElement('span');
    if (rec.summary) {
      text.textContent = `实际：${rec.summary}`;
    } else {
      text.className = 'record-blank';
      text.textContent = '实际：（未填写）';
    }
    const edit = document.createElement('button');
    edit.type = 'button';
    edit.className = 'link-btn';
    edit.textContent = '改';
    edit.title = '修改计划与实际';
    edit.onclick = () => {
      editingRecordId = rec.id;
      renderRecords();
    };
    real.append(text, edit);

    body.append(plan, real);
  }

  li.append(head, body);
  // There is deliberately no delete control anywhere on this page: records are
  // permanent (docs/reminder-pomodoro.md 6.1).
  return li;
}

/**
 * Renders the 对话 tab's plugin slot.
 *
 * The tab body is a MOUNT POINT: the host resolves which installed plugin owns
 * the 'chat' slot and mounts its sandboxed iframe. The resolution is re-run from
 * every state snapshot rather than once at startup, because the market program
 * can install or remove the plugin while the pet is running - a slot resolved
 * once would leave the tab stale until a restart.
 *
 * With no such plugin installed the slot shows a sentence pointing at the market
 * instead of an empty rectangle: a blank tab reads as "broken", and the fix is
 * one click away. The hint is built here rather than written in index.html so
 * that the tab body holds no host copy of the conversation's controls - the one
 * id in the markup is the slot, and everything inside it belongs to whoever
 * fills it.
 */
function renderChatSlot() {
  const slot = els.chatSlot;
  if (!slot) return;
  const plugin = slotPlugin();
  // The tab is shown only while a plugin owns it.
  //
  // The pet has no conversation of its own any more: the 对话 tab is a mount point
  // and nothing else. With no plugin to fill it the tab is a rectangle whose only
  // content is an explanation of why it is empty - and a tab you can open onto
  // nothing is worse than no tab, because it reads as a broken feature rather than
  // as an absent one. So the BUTTON is hidden too, and the panel falls back to 待办
  // if it was showing the conversation when the plugin went away.
  //
  // The button is hidden rather than removed: the four `data-tab` values stay
  // stable, so every currentTab comparison, the tab strip's layout and the static
  // gate's "exactly four tabs" assertion all keep working. The slot's own hint is
  // still rendered underneath, which is what the user sees for the moment before
  // the panel switches away - and what they see if they were already on 待办 and
  // the plugin is removed.
  setChatTabAvailable(!!plugin);
  if (!plugin) {
    pluginHost?.closeSlot();
    // Name the ACTUAL reason when a plugin is installed but does not qualify.
    //
    // The generic hint is right when nothing is installed, and misleading when
    // something is: the user sees a plugin listed in the market, reads "not
    // installed yet", and has no way to learn that the copy on disk is an older
    // package whose manifest predates the slot declaration. That is not a
    // hypothetical - it is what an install of the previous version produces, and
    // the symptom is a tab that insists the plugin is missing while the market
    // shows it as installed.
    slot.textContent = chatSlotDiagnosis() || CHAT_SLOT_HINT;
    slot.classList.add('chat-slot-empty');
    return;
  }
  // Re-mounting on every snapshot would restart the plugin's conversation on
  // every state change, so the frame is reused while it is already the right
  // plugin in the right slot - at the right VERSION.
  //
  // The version belongs in this test, and leaving it out was a real defect: an
  // update in the market keeps the plugin id and replaces the files behind it, so
  // an id-only comparison reused the running document and the user kept seeing the
  // PREVIOUS version's interface until the pet was restarted. The market's own
  // update button says the plugin was updated; the pet has to agree.
  //
  // This test has to come BEFORE the slot is emptied. Clearing first and then
  // returning early detached the live iframe from the document while the host
  // still held it as mounted: the plugin kept running with nothing on screen, and
  // the slot looked empty until the plugin changed. Arriving at the tab calls
  // this function every time, so it was not a rare path.
  if (pluginHost?.slotFrame?.pluginId === plugin.id &&
      pluginHost.slotFrame.version === plugin.version &&
      pluginHost.slotName === SLOT_CHAT) return;
  slot.textContent = '';
  slot.classList.remove('chat-slot-empty');
  pluginHost?.openSlot(SLOT_CHAT, plugin);
}

/**
 * Shows or hides the 对话 tab, and moves off it when it goes away.
 *
 * Two things have to happen together, and they are the whole reason this is a
 * function rather than one line in the caller:
 *
 *   - the BUTTON is hidden, so the tab strip stops offering a tab that has nothing
 *     in it. It is `.hidden` (display:none) rather than `disabled`, because a
 *     greyed-out tab invites a click that cannot be answered, while a missing one
 *     simply is not there.
 *   - if the panel is CURRENTLY on the conversation, it switches to 待办. Without
 *     this the panel would sit on a hidden tab's body: the strip would show 待办 as
 *     active while the conversation's (empty) body stayed on screen, and the user
 *     would have no way back except clicking a tab that looks unselected.
 *
 * The fallback is 待办 rather than 设置 because 待办 is the panel's home - it is
 * what opens by default and what the pet's single click leads to.
 */
function setChatTabAvailable(available) {
  const btn = els.tabChatBtn;
  if (!btn) return;
  btn.classList.toggle('hidden', !available);
  if (available) return;
  // Only the ACTIVE tab needs moving. A panel on 待办 must not be disturbed: this
  // runs from every state snapshot, so switching unconditionally would yank the
  // user off whatever tab they were reading each time a plugin disappeared.
  if (btn.classList.contains('active')) switchTab('todos');
}

/**
 * The watcher that keeps the 对话 tab in step with the installed plugin, while the
 * panel is open.
 *
 * WHY A TIMER IS NEEDED AT ALL: the host learns about installation changes only by
 * reading the installed list, and nothing pushes it. The market is a SEPARATE
 * PROCESS with its own settings file, and Go's State snapshot carries no plugin
 * information - so there is no event to subscribe to and no field to compare. The
 * tab is therefore refreshed by re-reading, and the only question is when.
 *
 * WHY "WHILE THE PANEL IS OPEN" IS THE RIGHT BOUND, and why the first version of
 * this (which polled only while the tab was HIDDEN) was wrong:
 *
 *   - the tab strip is only on screen while the panel is open, so a change made
 *     while the panel is closed cannot be seen by anyone until the panel opens -
 *     and opening it goes through applyLayout, which re-reads anyway. Polling with
 *     the panel closed spends a round trip every period to update something nobody
 *     is looking at.
 *   - the two directions of change are NOT symmetric. Hiding the tab (the plugin
 *     was disabled or removed) had no watcher under the old bound, because the tab
 *     was visible in exactly that case - so the strip kept offering a tab whose
 *     plugin was gone until the user happened to leave and re-enter the tab. That
 *     is the complaint this fixes: 「取消点击，面板上的对话tab没有及时的隐藏」.
 *
 * The period is deliberately slow. The observable effect is one tab appearing or
 * disappearing in a panel the user is already looking at, so half a second either
 * way is invisible, while the cost is a process-local read of plugins.json plus a
 * round trip - and it runs for as long as the panel is open.
 */
const CHAT_TAB_WATCH_MS = 1500;

function setChatTabWatch(wanted) {
  if (wanted && chatTabWatchTimer === null) {
    chatTabWatchTimer = setInterval(() => {
      // refreshChatSlot re-reads the list and re-renders, which is what shows or
      // hides the tab and mounts or unmounts the plugin's frame.
      refreshChatSlot();
    }, CHAT_TAB_WATCH_MS);
  } else if (!wanted && chatTabWatchTimer !== null) {
    clearInterval(chatTabWatchTimer);
    chatTabWatchTimer = null;
  }
}

/**
 * Explains WHY no installed plugin can fill the 对话 slot, or '' when nothing is
 * installed at all.
 *
 * Each clause of the resolution rule gets its own sentence, because they have
 * different fixes and the user cannot tell them apart from the outside:
 *
 *   - a plugin that declares no slot at all is an OLD PACKAGE. `panelSlot` was
 *     added with the conversation plugin 1.1.0; a 1.0.0 install has no such key,
 *     so it parses into `panelSlot: ""`. The fix is to update the plugin in the
 *     market, not to reinstall it - and "reinstall" is exactly what the generic
 *     hint would lead to, because it would install the same old package again if
 *     the catalogue still offers it.
 *   - a slot holder that is switched off needs the switch turned on;
 *   - one without pet.surface needs that permission ticked.
 *
 * The list is searched in the same order the Go resolver uses, so the sentence
 * describes the row that WOULD have won.
 */
function chatSlotDiagnosis() {
  const rows = (installedPlugins || []).filter((p) => p.enabled !== false);
  if (rows.length === 0) return '';
  const claimants = rows.filter((p) => p.panelSlot === SLOT_CHAT);
  if (claimants.length === 0) {
    const names = rows.map((p) => `${p.name || p.id}（v${p.version}）`).join('、');
    return `对话插件是旧版本：${names} 没有声明「对话」槽位，` +
      '在插件市场里更新它（或卸载后重新安装新版本）。';
  }
  const ungranted = claimants.filter((p) => !(Array.isArray(p.granted) && p.granted.includes('pet.surface')));
  if (ungranted.length > 0) {
    const names = ungranted.map((p) => p.name || p.id).join('、');
    return `对话插件缺少 pet.surface 授权：在插件市场里勾上 ${names} 的这项权限。`;
  }
  // A displaced holder: one declares the slot and is granted, but the resolver
  // still refused it - the only remaining clause is that its files could not be
  // read, which is what `error` reports.
  const broken = claimants.filter((p) => p.error);
  if (broken.length > 0) {
    const first = broken[0];
    return `对话插件无法读取：${first.name || first.id} — ${first.error}`;
  }
  return '';
}

/**
 * The installed plugin that owns the 对话 slot, or null.
 *
 * "Owns the slot" is the manifest's own declaration, read from the installed
 * list as `panelSlot === 'chat'`. There is no built-in id here on purpose: the
 * host does not know which plugin should render the conversation, only which
 * slot the plugin asked to fill.
 *
 * The three clauses mirror `chatSlotPlugin()` in Go exactly, and they have to:
 * the backend resolves the double-click target from the same rule, so a page
 * that mounted a plugin the host refuses (or refused one the host accepts) would
 * show an interface whose every capability call comes back "未获权限".
 *
 *  - `enabled`: a plugin the user switched off must not keep the tab;
 *  - `granted` contains `pet.surface`: the slot draws a sandboxed iframe outside
 *    the pet's own box, which is what that permission covers, so the slot would
 *    otherwise be a way around the permission prompt;
 *  - `panelSlot === 'chat'`: the slot is claimed by declaration.
 */
function slotPlugin() {
  return (installedPlugins || []).find(
    (p) => p.enabled !== false &&
      p.panelSlot === SLOT_CHAT &&
      Array.isArray(p.granted) && p.granted.includes('pet.surface'),
  ) || null;
}

/**
 * The sentence shown in an empty 对话 slot.
 *
 * Exported through a constant so the settings hint and this one cannot drift:
 * both tell the user the same two things - the plugin is missing, and where to
 * get it.
 */
const CHAT_SLOT_HINT = '对话插件还没装好：在设置里点「打开插件市场」装上并授权它。';

/**
 * True while a bubble is holding the slot with a question the user must answer.
 *
 * The picker, the summary prompt and the plan prompt are the three. They occupy
 * the same rectangle as a plugin's floating surface, so the surface cannot open
 * beside them - and it must not take the slot from them either, because the
 * summary prompt is the gate on the break and the plan prompt is the gate on the
 * focus: replacing either leaves the timer parked with nothing on screen
 * explaining why. The backend refuses too (OpenPluginSurface); checking here as
 * well keeps a refused gesture from becoming a round trip that ends in an
 * invisible error.
 */
function bubbleAwaitingAnswer() {
  return bubble?.kind === 'summary' || bubble?.kind === 'pick' || bubble?.kind === 'plan';
}

/**
 * Asks Go to open the 对话 slot's plugin surface (the double-click gesture).
 *
 * A single-click on the pet toggles the panel; a double-click used to open the
 * conversation overlay. The overlay is gone, so the gesture now asks the plugin
 * for its own floating surface - the plugin decides what (if anything) to show,
 * and with no slot plugin installed Go answers with a bubble that says so.
 */
function openChatPluginSurface() {
  if (bubbleAwaitingAnswer()) {
    focusAnswer();
    return;
  }
  App.OpenChatPluginSurface()
    .then(applyLayout)
    .catch(fail);
}

function applyState(next) {
  if (!next) return;
  state = next;
  renderPet();
  renderTodos();
  renderPomodoro();
  renderRecords();
  renderBadge();
  renderChatSlot();
  renderBackgroundPlugins();
  // Surface any data files that had to be reset, once per session.
  if (next.warnings?.length && !applyState._warned) {
    applyState._warned = true;
    toast(next.warnings[0]);
  }
  // A refused hot key has to reach the user SOMEWHERE, and the settings page that used to
  // say so is gone. Go reports it in State.hotkeys.problems ("Ctrl+Alt+S：已被其它程序占用"),
  // and a toast at the moment it is discovered is the honest equivalent of a red row on a
  // page that no longer exists: switched on, doing nothing, and now said out loud once.
  // Once per session, like the warnings above - a toast on every state refresh would be
  // worse than the silence it replaces.
  if (next.hotkeys?.problems?.length && !applyState._hotkeyWarned) {
    applyState._hotkeyWarned = true;
    toast(`快捷键没生效：${next.hotkeys.problems[0]}`);
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
  // Arriving at the conversation is the moment the slot has to be right, and it is
  // also the moment that DECIDES WHETHER THE TAB SHOULD EXIST.
  //
  // Both jobs belong to this one call, and they cannot be separated: reading the
  // installed list is what resolves which plugin owns the slot, and the answer to
  // that is the same fact that says whether the tab has anything to show. An
  // earlier version of this function refused the switch outright when the tab was
  // hidden - which deadlocked, because the refused call was the only thing that
  // would have read the list and discovered the plugin that un-hides it.
  //
  // So the switch always goes through, `refreshChatSlot` re-reads, and
  // `renderChatSlot` hides the tab and moves the panel back to 待办 if the fresh
  // read finds no owner. The panel may therefore sit on the conversation for the
  // duration of one backend call in the "plugin was just removed" case, which is
  // invisible next to the round trip that removed it.
  if (name === 'chat') {
    refreshChatSlot();
  }
  els.tabs.forEach((t) => t.classList.toggle('active', t.dataset.tab === name));
  els.tabTodos.classList.toggle('hidden', name !== 'todos');
  els.tabChat.classList.toggle('hidden', name !== 'chat');
}

/**
 * Mounts (or removes) the headless plugin frames.
 *
 * A plugin that declares `background: true` and still holds a permission that
 * fills a frame runs a permanently mounted, hidden frame. That is what lets it act
 * while the user has no interface open: without such a frame its events are
 * dropped, because the host delivers an event only to a frame it has mounted.
 *
 * The gates are each load-bearing:
 *
 *   - installed: a plugin that is not there has no files to load;
 *   - enabled: switching a plugin off must stop it doing anything, including in
 *     the background - otherwise "disable" would leave a running process behind;
 *   - declares `background`: the host does not guess which plugins want this;
 *   - holds `events` OR `net.ws` OR `net.fetch`: the same pairing the manifest
 *     validator enforces, and the check happens on BOTH sides - here and in Go.
 *     `events` is for a plugin the host tells about the pomodoro; `net.ws` is for
 *     one whose traffic arrives over a socket it holds open; `net.fetch` is for a
 *     POLLING plugin, which is told nothing and decides when to ask. Asking that
 *     third kind for `events` would be asking the user to grant a switch it never
 *     reads, which is the rule all three branches exist to keep.
 *
 * EVERY qualifying plugin gets a frame, and the ids are checked on both sides
 * (open the new, close the vanished), so an update or a revoke takes effect
 * without a restart.
 */
function renderBackgroundPlugins() {
  if (!pluginHost) return;
  pluginHost.syncBackground(backgroundPlugins());
}

/**
 * The installed plugins that want a headless frame.
 *
 * One function rather than two copies of the gates, because the selection is the
 * thing under test: a duplicated predicate is a predicate that can drift, and the
 * drift would show up as a frame that mounts for a plugin the panel says is not
 * eligible.
 */
function backgroundPlugins() {
  return (installedPlugins || []).filter((p) => {
    if (p.enabled !== true || p.background !== true) return false;
    if (!Array.isArray(p.granted)) return false;
    return p.granted.includes('events') ||
      p.granted.includes('net.ws') ||
      p.granted.includes('net.fetch');
  });
}

/**
 * Re-reads the installed plugins and re-renders the 对话 slot from them.
 *
 * The list is read here rather than at startup because the market program owns
 * installation: the pet must not cache "no chat plugin" from launch and show the
 * hint forever after one is installed. Reading it on arrival at the tab keeps
 * the cost off the startup path - the same reason the old plugin tab deferred its
 * market read - while still being fresh when it matters.
 */
async function refreshChatSlot() {
  // One read at a time.
  //
  // This used to be called only on arrival at the tab, where overlapping calls were
  // not possible. It now also runs from the watch timer AND from applyLayout when
  // the panel opens, so a slow read could be overtaken by a newer one and land
  // second - rendering the older list over the newer one. Dropping the extra read
  // is safe: the timer fires again, and the result being discarded is only ever a
  // list that is already being superseded.
  if (chatSlotReadInFlight) return;
  chatSlotReadInFlight = true;
  try {
    try {
      installedPlugins = await App.PluginsInstalled();
    } catch (err) {
      // A failed read must not leave the previous list in place: pretending the
      // plugin is still installed would mount an iframe whose files may be gone.
      installedPlugins = [];
      fail(err);
    }
    renderChatSlot();
    // The headless frame is resolved from the SAME list, in the same place. Both
    // are answers to "what is installed right now", so splitting them across two
    // reads would let the frame describe a list the slot had already replaced -
    // and it was a real defect: this function assigned the list but only
    // re-rendered the slot, so a background plugin installed or switched off while
    // the pet was running kept its headless frame either way.
    renderBackgroundPlugins();
  } finally {
    chatSlotReadInFlight = false;
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
 * Sends the caret to whatever the on-screen question is waiting for.
 *
 * This is what a double click on the pet does when the plugin surface cannot
 * open: the gesture is turned into "go answer it" rather than silently doing
 * nothing. No toast is used, because the toast lives inside the panel and the
 * panel is necessarily closed while a bubble holds the slot - the caret landing
 * in the box IS the explanation.
 */
function focusAnswer() {
  if (bubble?.kind === 'summary' || bubble?.kind === 'plan') {
    // Only the caret: the user may have typed something already, and selecting
    // it would make the next keystroke replace their own words.
    els.bubbleInput.focus();
    return;
  }
  // The picker: its primary button is the answer ("开始专注").
  els.bubblePrimary.focus();
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

/**
 * Answers the plan question and starts the focus session.
 *
 * Both buttons go through here, and the empty string is a real answer rather than
 * a cancellation: "跳过" means "just start, this session has no plan" and Go
 * produces the record all the same. Making the secondary button merely close the
 * bubble would leave the timer parked behind a question nobody can see.
 */
function startWithPlan(plan) {
  App.StartPomodoroWithPlan(plan)
    .then((next) => {
      applyState(next);
      return App.GetLayout();
    })
    .then(applyLayout)
    .catch(fail);
}

// --- wiring ---------------------------------------------------------------

function wire() {
  // --- plugins ------------------------------------------------------------
  // The host owns every sandboxed frame on the page: the floating surface's
  // frames and the panel slot's single frame. It is the only object here that
  // knows about plugin interfaces, and the 对话 tab's content is one of them.
  pluginHost = new PluginHost(
    {
      pluginSurface: els.pluginSurface,
      chatSlot: els.chatSlot,
      pluginBackground: els.pluginBackground,
    },
    { fail, debug },
  );

  // The conversation plugin's identity is not known until the installed list is
  // read, so the slot is filled from the first snapshot rather than here. The
  // headless frame is resolved from the same snapshot, for the same reason.
  refreshChatSlot();
  renderBackgroundPlugins();

  // The plugin market is a separate program (like the voice studio), not a page
  // inside this one. Clicking the entry launches it; the pet keeps running.
  //
  // The state is applied because a launch can fail - the program is a sibling
  // executable that may not be there - and Go reports that failure as the call's
  // error, which fail() turns into the toast. There is nothing else to render:
  // which market source to read is the market program's own setting now.
  // 打开控制中心 is NOT a button here any more: launching a program is what the tray menu
  // does (打开控制中心 / 打开数据目录 / 退出), and the settings tab that held it is retired.
  // App.OpenPluginMarket is still bound - the tray calls it - so the launcher, its failure
  // messages and its "sibling executable" search are all unchanged.

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
      // EXCEPT the questions the timer is parked behind: the summary prompt holds
      // the break and the plan prompt holds the focus, so both must be answered
      // rather than waved away. The backend refuses to dismiss them, and checking
      // here as well keeps the frontend from redrawing a bubble that is still there
      // - which would look like a flicker.
      if (bubble && bubble.kind !== 'summary' && bubble.kind !== 'plan') {
        App.DismissBubble().catch(() => {});
      }
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
          // pending single click and ask the conversation plugin for its floating
          // surface. Doing it here rather than on a `dblclick` listener means the
          // two paths cannot disagree.
          lastClickAt = 0;
          clearTimeout(pendingClick);
          pendingClick = null;
          openChatPluginSurface();
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

  // Go asks for one of the panel tabs (the tray, or a plugin's own "待办"
  // button). The payload says which, because the layout carries geometry rather
  // than which tab is showing.
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

  // The picture was changed outside this program (the control center is where it is
  // chosen now). Unlike the scale, there is no geometry to re-apply: the image is drawn
  // by the page, so the page has to re-read the state to get the new URL. The payload
  // carries it, but a full state read is what keeps the settings rows in step too - and
  // it is one call on an event that fires when a human changes a picture, not a tick.
  EventsOn('pet:image', () => {
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
      // then the panel. The conversation overlay is not in this list any more:
      // its interface belongs to the plugin, so the plugin decides what Escape
      // means inside it.
      if (editingRecordId !== null) {
        editingRecordId = null;
        renderRecords();
      } else if (bubble) {
        // The summary prompt and the plan prompt are gates: the break waits on the
        // first and the focus waits on the second, so Escape does not close either.
        // They have to be answered. The backend refuses too (DismissBubble), so
        // this only saves a round trip - but it also stops the panel fall-through
        // below, which would otherwise close the panel behind a question that is
        // still waiting.
        if (bubble.kind !== 'summary' && bubble.kind !== 'plan') {
          App.DismissBubble().then(applyLayout).catch(fail);
        }
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

  // No settings wiring here any more. Everything this block used to do now belongs to one
  // of three places: the control center (preferences, the picture), the pomodoro strip
  // (放弃, wired above with the other strip buttons), or the tray menu (打开控制中心 /
  // 立刻提醒 / 打开数据目录 / 退出) - each the place that owns the thing being changed.
}

// --- speech ---------------------------------------------------------------
//
// The pet has NO speech controls, and that is the point rather than an omission.
//
// Speech is a plugin (com.shixiansi.plugin-voice): the lines, the character library,
// the switches and the rendered clips all belong to it. Its workshop is hosted by the
// plugin market program - a 980x700 resizable window, which is where a work surface
// that lists five lines with several takes each can actually fit.
//
// What the pet kept is the part that is genuinely the pet's: it emits
// timer.phaseEnded and reminder.fired, and it serves the audio.* capability the
// plugin stores and plays through. Which line to say, in which voice, and whether to
// speak at all are the plugin's decisions, so there is nothing here to switch.
//
// The functions that used to live here (renderVoiceSettings, wireVoiceSettings,
// voiceSettingsPayload, saveVoiceSettings) are gone with the three config fields and
// the four bindings they drove: SaveVoiceSettings, TestVoice, StopVoice and
// OpenVoiceStudio.

// The global-shortcut switch is in the CONTROL CENTER now (开始 → 外观与启动), and the
// pet applies it within a second: its settings watcher re-registers the keys when the
// preference changes (app_settingswatch.go). The state still reaches this page - State
// carries Hotkeys, and pet:visible refreshes it - but nothing here renders it any more,
// because there is no switch here to render it next to.
//
// What stayed: the keys themselves. Showing and hiding the pet is app_hotkey.go's job and
// the tray offers the same two actions for the mouse.

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
