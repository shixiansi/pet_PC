/**
 * PetPC frontend.
 *
 * Responsibilities are split with the Go side deliberately:
 *  - Go owns the window: its size, its position, where the pet sits inside it,
 *    and which rectangle of the window is visible at all. It also owns the two
 *    bubbles the pet speaks with - the manual reminder and the pet's own menu.
 *  - This file owns the presentation: painting the layout Go reports, the todo
 *    UI, the speech bubble and the menu's contents.
 *
 * There is NO timer here, and no timer state to render. The pomodoro is a PLUGIN
 * (com.shixiansi.plugin-pomodoro): it runs its own state machine, keeps its own
 * records, and draws its own phase, countdown and session list in its own floating
 * surface. The host used to own all of that, and the strip, the folded records
 * archive, the countdown badge and the two prompts (summary and plan) retired with
 * it (openspec/changes/todo-pomodoro-as-plugins). A page still painting them would
 * be drawing a timer the host no longer runs - which is exactly what the static
 * gate caught as eleven calls to bindings that no longer exist.
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
  // The hover-only conversation button inside the pet's box. Its visibility is
  // the stylesheet's business (#pet:hover); the page only decides what a click
  // on it does.
  chatButton: $('pet-chat'),
  badge: $('badge'),
  panel: $('panel'),
  close: $('close-panel'),
  // Opens the control center - the window that owns settings and plugins now. In the panel
  // HEADER rather than in a tab: a tab for it would be a page with one button on it, and the
  // tray menu (its only entry point for a while) is invisible unless you already know it.
  openSettings: $('open-settings'),
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

  // Bubble (reminder / menu). The picker, the summary prompt and the plan prompt
  // were the other three kinds, and all three were questions about the host's
  // timer: they are the pomodoro plugin's own surface now.
  bubble: $('bubble'),
  bubbleArrow: $('bubble-arrow'),
  bubbleTitle: $('bubble-title'),
  bubbleList: $('bubble-list'),
  bubbleMore: $('bubble-more'),
  // The pet's own menu: a grid of icon buttons INSIDE the bubble card, opened by
  // a right-click on the pet. The cells are declared in index.html (one button
  // per action, with its icon and its label) and wired by delegation in wire();
  // what each one DOES lives in PET_MENU_ACTIONS.
  bubbleMenu: $('bubble-menu'),
  // No entry for #bubble-actions: it is the row the two buttons below sit in, and the
  // page never touches the row itself - only the buttons. (The element stays in the
  // markup because it is what lays them out.)
  bubbleSecondary: $('bubble-secondary'),
  bubblePrimary: $('bubble-primary'),

  // No settings entries: the settings tab moved to the control center, and the few
  // controls that could not move are in the tray menu (打开控制中心 / 退出 / 立刻提醒 /
  // 打开数据目录). An `els` entry for an element the page no longer has would be a null
  // that fails at the first click. The textarea that used to be here (bubble-input) is
  // one of those: its only users were the summary and plan prompts.

  // No confirm-dialog entries: the self-drawn dialog went with the two destructive
  // timer actions it used to guard (跳过 / 放弃当前番茄钟), and the markup went with it.

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
/** Auto-hide timer for a reminder bubble. */
let bubbleHideTimer = null;

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
 * The floating surface can be opened from the pet's hover button WITHOUT the 对话
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

// No confirmDialog() here any more. It existed for the two operations that could
// lose work - 跳过 (a focus phase that would not be recorded) and 放弃当前番茄钟 - and
// both belonged to the host's timer. With the timer gone there is nothing on this
// page destructive enough to ask about, so the function, its #confirm markup and its
// CSS went with it: an unreachable dialog is a second UI nobody can check.

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
    // opened, NOT at startup. Opening the surface from the pet's hover button can
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
  // Reset everything first: the two kinds share the card.
  els.bubbleList.replaceChildren();
  els.bubbleList.classList.add('hidden');
  els.bubbleMore.classList.add('hidden');
  els.bubbleMenu.classList.add('hidden');
  els.bubbleSecondary.classList.add('hidden');
  els.bubblePrimary.classList.add('hidden');
  els.bubbleTitle.textContent = b.title || '';
  // An empty heading is hidden rather than left in the flow: the menu card has no
  // title, and a zero-height element still costs the flex gap it stands in.
  els.bubbleTitle.classList.toggle('hidden', !b.title);

  if (b.kind === 'menu') {
    // The grid is the whole card - no heading, no list, no buttons - and Go sized
    // the box for exactly that (bubbleHeightMenu). Everything else has just been
    // reset, so this branch only has to reveal the grid.
    els.bubbleMenu.classList.remove('hidden');
    return;
  }

  // Reminder. It is the only other kind left: BubbleView has two (app_bubble.go),
  // and the three question kinds - picker, summary, plan - were all questions about
  // the host's timer. The pomodoro plugin asks them in its own surface, so this card
  // has no textarea, no radio list and no Enter handler to install any more.
  renderBubbleList(b.items || []);
  els.bubbleMore.textContent = b.more ? `还有 ${b.more} 项` : '';
  els.bubbleMore.classList.toggle('hidden', !b.more);
  els.bubbleSecondary.textContent = '知道了';
  els.bubbleSecondary.classList.remove('hidden');
  els.bubbleSecondary.onclick = () => App.DismissBubble().then(applyLayout).catch(fail);
  els.bubblePrimary.textContent = '看看待办';
  els.bubblePrimary.classList.remove('hidden');
  els.bubblePrimary.onclick = () => App.OpenTodoPanelFromBubble().then(applyLayout).catch(fail);
}

/** Renders the reminder's todo list. */
function renderBubbleList(items) {
  if (!items.length) return;
  els.bubbleList.replaceChildren();
  els.bubbleList.classList.remove('hidden');

  for (const item of items) {
    const li = document.createElement('li');
    li.className = 'bubble-item';
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
    els.bubbleList.append(li);
  }
}

/**
 * Starts the countdown that fades a reminder bubble.
 *
 * The guard names the one kind that may auto-hide rather than listing the kinds
 * that may not: a notification that vanishes on its own is the feature, and a
 * question that did the same was the bug. The questions are the plugin's now, so
 * this card only ever holds the notification - the check stays because the menu is
 * also a kind here, and a menu that timed out would be worse than a stray one.
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
 * the backend resolves the conversation button's target from the same rule, so a
 * page that mounted a plugin the host refuses (or refused one the host accepts)
 * would show an interface whose every capability call comes back "未获权限".
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
const CHAT_SLOT_HINT = '对话插件还没装好：点右上角的 ⚙ 打开控制中心，装上并授权它。';

/**
 * Asks Go to open the 对话 slot's plugin surface (a click on the hover button).
 *
 * A click on the pet toggles the panel; the conversation is the BUTTON that
 * appears over the pet while the pointer is on it. The host used to have its own
 * conversation overlay, and a double click opened it; the overlay is gone, so the
 * gesture now asks the plugin for its own floating surface - the plugin decides
 * what (if anything) to show, and with no slot plugin installed Go answers with a
 * bubble that says so.
 *
 * There used to be a "a question owns the slot, go answer it instead" branch here.
 * It is gone with the questions: the summary and plan prompts were the host
 * timer's, so no bubble on this page is waiting for an answer any more, and the
 * backend no longer refuses the surface for holding the slot (OpenPluginSurface).
 */
function openChatPluginSurface() {
  App.OpenChatPluginSurface()
    .then(applyLayout)
    .catch(fail);
}

// --- the pet's own menu ----------------------------------------------------

/**
 * True while the menu card is the bubble on screen.
 *
 * The menu is a bubble KIND (BubbleMenu in app_pet_menu.go), not an overlay of its
 * own: it shares the slot beside the pet and the card the pet speaks in, so "is
 * the menu up?" is a question about the bubble this page already tracks. That is
 * also why nothing here has to hide it: opening the panel, opening a plugin's
 * surface, hiding the pet, dragging it, Escape and a phase transition all fold a
 * bubble away, and the menu goes with them.
 */
function petMenuOpen() {
  return bubble?.kind === 'menu';
}

/** Closes the menu card; Go re-crops the window to the pet alone. */
function closePetMenu() {
  return App.DismissBubble().then(applyLayout);
}

/**
 * Opens the surface of whichever plugin claims a SLOT, or runs `fallback`.
 *
 * A slot is a named mount point and the name is all this page knows: `todos` and `pomodoro`
 * today, and the claimant decides what that looks like (its own declared rectangles). Resolution
 * mirrors the backend's rule for the chat slot - installed, enabled, holding `pet.surface`,
 * claiming the name - so a disabled or unauthorised plugin simply does not answer and the
 * fallback runs.
 *
 * `App.OpenPluginSurface` still refuses for reasons of its own (not enabled, no grant, no
 * declared surface, rectangles entirely outside the window), which is why its rejection is passed
 * through instead of swallowed: the user needs to see why the surface did not appear.
 */
function openSlotPluginSurface(slot, fallback) {
  return App.PluginsInstalled()
    .then((rows) => {
      const owner = (rows || []).find(
        (row) =>
          row &&
          row.enabled &&
          row.panelSlot === slot &&
          (row.granted || []).includes('pet.surface'),
      );
      if (!owner) return fallback();
      return App.OpenPluginSurface(owner.id).then(applyLayout);
    })
    .catch(fail);
}

/**
 * Opens or closes the menu: one gesture, both directions.
 *
 * A right-click that only ever opened it would leave two ways to dismiss (pick a
 * cell, press Escape) and none with the hand already on the mouse - which is what
 * a menu's own gesture is for.
 *
 * There used to be a third outcome here: a bubble waiting for an answer owned the
 * slot, and the caret went into its textarea instead of the menu taking the slot.
 * That bubble was the host timer's question, so nothing on this page can be
 * waiting for an answer now, and the backend's matching refusal (petMenuBlocked)
 * is gone too.
 */
function togglePetMenu() {
  if (petMenuOpen()) {
    closePetMenu().catch(fail);
    return;
  }
  App.OpenPetMenu().then(applyLayout).catch(fail);
}

/**
 * Launches the control center: the SEPARATE PROGRAM that holds settings and plugin
 * management.
 *
 * 插件 and 设置 are two cells for one program, deliberately. The program is called
 * the control center because it holds both halves, and it takes no arguments - so
 * there is no way to open it "on the plugins page". A single cell would have to be
 * named for one half, and a user looking for the other would conclude the pet has
 * no way in. The two cells' titles name which half each one is for.
 */
function launchControlCenter() {
  return App.OpenPluginMarket().then((next) => {
    applyState(next);
    // A card left beside the pet while a window opens over it is a card in the way
    // of the thing the user just asked for.
    return closePetMenu();
  });
}

/**
 * What each cell does, keyed by the cell's `data-menu` in index.html.
 *
 * Every entry calls the binding its action already had, so a menu cell and the
 * tray entry it mirrors stay ONE action rather than two definitions that can
 * drift. Which cells exist is the markup's business; what they do is this map's.
 */
const PET_MENU_ACTIONS = {
  // 日程: whoever claims the `todos` SLOT, through its own floating surface - the column beside
  // the pet. The slot name is resolved from the installed list, so the host never has to know
  // which plugin is "the todo one" (the same idea as the chat slot, see app_chat_slot.go).
  //
  // The PANEL is the FALLBACK, not the target: with no claimant the gesture still has to do
  // something, and an older installation's panel tab is what it used to do. That is also what
  // keeps this cell alive until the panel is deleted for good
  // (openspec/changes/todo-pomodoro-as-plugins, stage 3).
  todos: () =>
    openSlotPluginSurface('todos', () =>
      App.SetPanelOpen(true).then((next) => {
        applyLayout(next);
        // The panel renders from `state`, and the list may have been edited in the
        // control center since the last push.
        return App.GetState().then(applyState);
      })),

  // 对话: the conversation plugin's floating surface - the same thing the hover
  // button opens. That call folds the card away itself, or says why it cannot
  // open (no chat plugin, missing grant, an old package).
  chat: () => openChatPluginSurface(),

  // 番茄钟: whoever claims the `pomodoro` slot gets the cell, through its own surface - the timer
  // is something you glance at, and the plugin draws it beside the pet.
  //
  // There is no host timer to fall back on any more, so the fallback is not "flip it on/off"
  // (that binding is gone) but the one place the user can DO something about a missing plugin:
  // the control center, where the pomodoro plugin is installed, switched on and granted. A cell
  // that quietly did nothing would look like a broken button, and a toast would be invisible -
  // the toast lives inside the panel, which is closed while the menu card is up.
  pomodoro: () => openSlotPluginSurface('pomodoro', () => launchControlCenter()),

  // 提醒: one reminder now. It REPLACES the card with the reminder bubble (Go
  // re-crops to it), so there is nothing to dismiss here - and when there is
  // nothing worth reminding, the call fails and the menu stays up to pick from.
  remind: () => App.RemindNow().then(applyState),

  plugins: () => launchControlCenter(),
  settings: () => launchControlCenter(),

  // 归位: back to the first-run resting place. It re-places the window, and folds
  // the card away with the panel and any surface - a card beside a pet that just
  // moved somewhere else is a card pointing at nothing.
  snap: () => App.ResetToDefaultPosition().then(applyLayout),

  // 隐藏: the same HidePet the shortcut and the tray use. It folds everything
  // beside the pet away, because the pet itself is about to disappear.
  hide: () => App.HidePetForUser().then(applyState),

  quit: () => App.Quit(),
};

/**
 * Runs one cell's action and surfaces a rejection like every other control does.
 *
 * A cell with no entry in the map is a wiring mistake, not a refusal, so it says
 * so in the console rather than looking like a button that does nothing.
 */
function runPetMenuAction(name) {
  const run = PET_MENU_ACTIONS[name];
  if (!run) {
    console.warn(`pet menu: no action for ${name}`);
    return;
  }
  try {
    Promise.resolve(run()).catch(fail);
  } catch (err) {
    fail(err);
  }
}

function applyState(next) {
  if (!next) return;
  state = next;
  renderPet();
  renderTodos();
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
  // 打开控制中心 from the panel header: one click, one call, no state to read back. The
  // market is a separate process, so the button's whole observable effect is the launch -
  // a button that also tried to render something would be the host doing the market's job
  // again. Failure is visible rather than silent: the program is a sibling exe that may not
  // be there, and "nothing happened" would look like a dead button.
  els.openSettings.addEventListener('click', () => {
    App.OpenPluginMarket()
      .then((next) => {
        applyState(next);
        // The panel is in the way of a window that just opened over it.
        closePanel();
      })
      .catch(fail);
  });

  // --- pet dragging -------------------------------------------------------
  // Coordinates are captured on pointerdown, but the drag is not started with
  // the backend until the pointer actually moves past the threshold. That way a
  // plain click never triggers the drag path (which collapses the panel), so
  // click-to-toggle stays unambiguous.
  // Right-click on the PET opens the pet's own menu: a 3x3 grid of icon buttons in
  // the same card the pet speaks in (see app_pet_menu.go and #bubble-menu).
  //
  // It used to pop the TRAY's native menu here, which is exactly what it looked
  // like: a column of grey system text, no picture, nothing in common with the rest
  // of the app. A page-drawn menu was avoided for a while because a menu inside the
  // page has to fit inside the window region - but the region's slot already
  // carries one card at a time, and a BUBBLE is that card, so the menu is a bubble
  // kind rather than a fourth placeholder (AGENTS.md §3.2).
  //
  // Not on the panel, the bubble or a plugin's surface: those are windows with their
  // own contents, and a right-click inside them is about what is in them, not about
  // the pet.
  els.pet.addEventListener('contextmenu', (e) => {
    e.preventDefault();
    togglePetMenu();
  });

  // One delegated listener for the whole grid, so adding a cell is an edit to
  // index.html plus an entry in PET_MENU_ACTIONS - not a line of wiring per cell.
  els.bubbleMenu.addEventListener('click', (e) => {
    const cell = e.target.closest('[data-menu]');
    if (!cell) return;
    runPetMenuAction(cell.dataset.menu);
  });

  // A page-drawn menu cannot see the click that dismisses it: the window is cropped
  // to the card, so clicking anywhere else lands on another program. Losing focus is
  // the observable half of "the user went elsewhere", so that is what closes the
  // card - otherwise it sits there until it is picked from, which reads as stuck.
  window.addEventListener('blur', () => {
    if (!petMenuOpen()) return;
    closePetMenu().catch(() => {});
  });

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
      // Unconditionally, now. The exception was the summary and plan prompts: both
      // were gates on the host timer, both refused dismissal, and redrawing a bubble
      // that is still there would have looked like a flicker. They are the pomodoro
      // plugin's questions, so neither this card nor DismissBubble has a gate to
      // respect.
      if (bubble) App.DismissBubble().catch(() => {});
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
        // A press that never moved is a click, and a click toggles the panel.
        //
        // It used to be HELD for the system's double-click interval, because a
        // second click opened the conversation and letting the first one act at
        // once made a double click unwind as click-click: the panel opened and
        // closed and only then did the chat appear. The conversation entry is a
        // hover button now (see els.chatButton), so nothing competes with this
        // click any more - and the delay, which every click paid, is gone.
        applyLayout(await App.TogglePanel());
        if (editingId === null) els.title.focus();
      }
    } catch (err) {
      fail(err);
    }
  };

  els.pet.addEventListener('pointerup', endDrag);
  els.pet.addEventListener('pointercancel', endDrag);

  // The conversation button drawn over the pet on hover (style.css).
  //
  // It is the entry to the 对话 slot's plugin surface - exactly what the double
  // click on the pet used to be, now with a target the user can see.
  //
  // pointerdown is stopped here rather than handled: the pet's own pointerdown
  // starts a drag and calls preventDefault, and letting it see this press would
  // both register a phantom drag origin and swallow the click that follows in
  // engines that tie compatibility mouse events to the prevented press.
  els.chatButton.addEventListener('pointerdown', (e) => e.stopPropagation());
  els.chatButton.addEventListener('click', () => openChatPluginSurface());

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

  // Panel interaction.
  els.close.addEventListener('click', closePanel);
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      // Escape unwinds the most transient thing first: a bubble, then a form edit,
      // then the panel. The conversation overlay is not in this list any more: its
      // interface belongs to the plugin, so the plugin decides what Escape means
      // inside it. Neither is a record edit - there is no archive on this page to
      // edit a record in. Nor is a confirmation dialog: there is no destructive
      // action left on this page to confirm.
      if (bubble) {
        // Nothing on this page is a gate any more. The two that were - the summary
        // prompt and the plan prompt - held the host timer's break and focus, and
        // they belong to the pomodoro plugin now; its own surface decides what
        // Escape means there. So a bubble is always dismissable here, and
        // DismissBubble no longer refuses anything.
        App.DismissBubble().then(applyLayout).catch(fail);
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

  // No pomodoro wiring here any more: the strip's eight buttons drove eight bindings
  // (SetPomodoroEnabled / ShowPomodoroPicker / Pause / Resume / Stop / SkipPhase /
  // ResetPomodoro, plus the picker's ConfirmPomodoroStart) and every one of them went
  // with the host timer. The timer's controls are the pomodoro plugin's, drawn in its
  // own floating surface and wired by the plugin's own script.
  //
  // No settings wiring here either. Everything this block used to do belongs to one of
  // three places: the control center (preferences, the picture), the pet's own menu
  // (日程 / 对话 / 番茄钟 / 提醒 / 归位 / 隐藏 / 退出 and the two control-center cells), or
  // the tray menu (打开控制中心 / 立刻提醒 / 打开数据目录 / 退出) - each the place that owns
  // the thing being changed.
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

// Keep the badge honest between full refreshes.
//
// There is no "next reminder at" line here any more: the host has no reminder
// schedule to display (the todo plugin owns reminders now), so this poll only
// re-reads what it still renders - the open-todo count. The two things it used to
// also keep in step (the countdown badge and the strip) went with the host timer,
// and the 250ms local countdown tick below went with them: it existed to correct
// drift against Go's own rounding of a phase deadline, and there is no phase here
// to count down.
setInterval(() => {
  if (!state) return;
  renderBadge();
  App.GetState()
    .then((next) => {
      state = next;
      renderBadge();
    })
    .catch(() => {});
}, 30000);
