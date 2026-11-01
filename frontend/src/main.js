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
import { PluginHost } from './plugin-host';

const $ = (id) => document.getElementById(id);

const els = {
  pet: $('pet'),
  petImage: $('pet-image'),
  // The mount point for a plugin that draws the pet itself (manifest `body`). Empty unless
  // such a plugin is installed, enabled and granted - see petBodyPlugin below.
  petBody: $('pet-body'),
  // The hover-only conversation button inside the pet's box. Its visibility is
  // the stylesheet's business (#pet:hover); the page only decides what a click
  // on it does.
  chatButton: $('pet-chat'),
  badge: $('badge'),
  // The line a plugin draws on the pet itself (the pomodoro countdown). Separate from the
  // badge above: that one counts the todo plugin's list and sits in the corner, this one is
  // centred above the head and shows whatever string a plugin last set.
  clock: $('pet-clock'),
  toast: $('toast'),

  // THREE mount points: the floating surface's, the headless frame's, and the pet's own
  // body. All three are fill-only containers - what goes inside them is built by
  // plugin-host.js - and they are independent of each other.
  //
  // THE PANEL'S WENT WITH THE PANEL: #chat-slot was the 对话 tab's body, and the chat
  // plugin renders itself as a floating surface now. A host mount point for a panel that
  // no longer exists would be a `div` nothing ever fills.
  pluginSurface: $('plugin-surface'),
  pluginBackground: $('plugin-background'),
  // The body mount point travels to plugin-host.js with the other two, because the body IS a
  // sandboxed plugin document like them: the host only decides where it goes and who owns it.

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
  // 那一格主题按钮的标签。只有它在页面生命周期里会变（写着"点下去会变成什么"），所以它
  // 需要单独的引用；其余格子的文字是静态的。
  menuThemeLabel: $('menu-theme-label'),
  // No entry for #bubble-actions: it is the row the two buttons below sit in, and the
  // page never touches the row itself - only the buttons. (The element stays in the
  // markup because it is what lays them out.)
  bubbleSecondary: $('bubble-secondary'),
  bubblePrimary: $('bubble-primary'),

  // No entries for the panel, its tabs, its header buttons, its form or its todo list.
  // The panel was the 380x520 column beside the pet; 待办 moved into the todo plugin's
  // surface and 对话 into the chat plugin's, so the whole section is gone from
  // index.html and every `els` entry pointing at it would be a null that fails at the
  // first click. That is the same rule the settings tab and the timer strip followed.
  //
  // No settings entries either: the settings tab moved to the control center, and the few
  // controls that could not move are in the tray menu (打开控制中心 / 退出 / 立刻提醒 /
  // 打开数据目录). The textarea that used to be here (bubble-input) went with the two
  // prompts that typed into it.

  // No confirm-dialog entries: the self-drawn dialog went with the two destructive
  // timer actions it used to guard (跳过 / 放弃当前番茄钟), and the markup went with it.

  // Speech has NO entries here: the voice plugin owns its switches, and it is
  // configured in the plugin market's own window. See the "speech" section near the
  // bottom of this file for why the pet has nothing to render.
};

/**
 * Drag diagnostics.
 *
 * Pointer handling is entirely in the webview, so this is the only way to see drag
 * events from the Go side. The line lands in <dataDir>\logs\pet.log at DEBUG level,
 * which the shipping level (INFO) drops: PETPC_DEBUG=1 or PETPC_LOG_LEVEL=debug brings
 * it back, and the control center's 日志 page reads the same file.
 */
const debug = (msg) => {
  App.DebugLog(msg).catch(() => {});
};

/** Latest server-side snapshot. */
let state = null;
/** ID of the todo currently being edited, or null when adding. */
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

// No panel-open state, no chat-tab watcher and no slot-read latch: all three existed to
// keep the PANEL's tab strip in step with the installed plugin, and the panel is gone. The
// installed list is still read - the floating surfaces and the headless frames are routed
// from it - but on the paths that actually need it (see refreshInstalledThenApply).
// --- helpers --------------------------------------------------------------

function toast(message) {
  els.toast.textContent = message;
  els.toast.classList.remove('hidden');
  clearTimeout(toast._timer);
  toast._timer = setTimeout(() => els.toast.classList.add('hidden'), 3200);
}

/** Logs and surfaces a rejected backend call. */
function fail(err) {
  const message = typeof err === 'string' ? err : err?.message || '操作失败';
  console.error(err);
  // The same sentence goes to the host's log at ERROR level, because the toast is the
  // only thing the USER sees and it lasts 3.2 s. A binding that threw while nobody was
  // watching used to leave no evidence anywhere at all, and "点了一下，什么也没发生" is
  // the report that costs the most to answer. The 日志 page in the control center shows
  // this line next to the host's own, which is what makes it diagnosable.
  App.PageLog('error', message).catch(() => {});
  toast(message);
}

const pad = (n) => String(n).padStart(2, '0');

// No confirmDialog() here any more. It existed for the two operations that could
// lose work - 跳过 (a focus phase that would not be recorded) and 放弃当前番茄钟 - and
// both belonged to the host's timer. With the timer gone there is nothing on this
// page destructive enough to ask about, so the function, its #confirm markup and its
// CSS went with it: an unreachable dialog is a second UI nobody can check.

// toLocalInput / formatDue / dueStatus used to live here. All three rendered a todo's
// deadline for the panel's form and list, and both of those are gone: the list is the todo
// plugin's data, and the plugin formats its own dates. What the host still renders from a
// deadline is the reminder bubble's label, and that has been Go-side all along (dueLabel in
// app_bubble.go) because the bubble is painted inside the window region, not by the page.
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
  // Remembered for the deferred surface handshake below: a surface can arrive
  // before the installed list has been read, and it is completed by re-applying
  // this same layout once the list lands.
  lastLayout = layout;

  // The cards' colour scheme. `<html data-theme>` is the CSS switch (style.css keys both palettes
  // off it), so this is the only place the page decides which one is live - and it decides from
  // the layout, i.e. from the host, so the file and the pixels cannot disagree.
  //
  // The menu cell's label names the ACTION ("深色" while the cards are light), which is the way
  // round that does not need a second word for "currently".
  if (layout.theme) {
    const dark = layout.theme === 'dark';
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
    if (els.menuThemeLabel) els.menuThemeLabel.textContent = dark ? '浅色' : '深色';
    // And the mounted plugin frames, because they are opaque documents with their own
    // stylesheet: the chat panel was open when the switch was flipped and stayed dark until
    // the surface was re-opened. `?theme=` covers frames that mount later; this covers the
    // ones already on screen.
    pluginHost?.setTheme(dark ? 'dark' : 'light');
  }

  // The palette pack, from the same push and for the same reason: `<html data-theme-pack>` is what
  // style.css keys the pack blocks off. An EMPTY pack removes the attribute rather than writing an
  // empty one - "no attribute" is the stylesheet's default look, while `data-theme-pack=""` is a
  // pack that does not exist. That difference is invisible until the day the default changes.
  //
  // The pet never chooses a pack: the control center's cards do. This line is the whole reason the
  // two windows cannot end up in different palettes.
  if (typeof layout.themePack === 'string') {
    if (layout.themePack) document.documentElement.dataset.themePack = layout.themePack;
    else delete document.documentElement.dataset.themePack;
  }

  Object.assign(els.pet.style, {
    left: `${layout.petX}px`,
    top: `${layout.petY}px`,
    width: `${layout.petW}px`,
    height: `${layout.petH}px`,
  });

  // Nothing to show or hide here any more. The panel used to be the second rectangle this
 // function painted - it read the layout's panel flag and copied the four offsets onto the panel -
  // and the section itself is gone (see index.html). What the panel's GEOMETRY still buys
  // is the window: `Layout.PanelX/Y/W/H` is where a plugin's floating surface may sit beside
  // the pet, and plugin-host.js places the frames from the layout's own rectangles.

  applyBubble(layout.bubble || null);

  // The plugin floating surface is placed from the layout the same way: the
  // rectangles it is handed ARE the ones the window was cropped to, so nothing
  // here derives a position or a size of its own.
  const surface = layout.pluginSurface || null;
  if (surface) {
    // The surface needs the plugin's METADATA (its entry file and its grants), and that comes
    // from the installed list, which is read on demand rather than at startup. Opening a
    // surface can therefore arrive before the list has ever been read, and passing an empty
    // view in that case gave the iframe a default entry and no grants - the plugin rendered as
    // unauthorised (or asked for an asset path that does not exist). Reading the list here is
    // what makes the surface independent of which path the user happened to take first.
    if (!pluginViewFor(surface.pluginId)) {
      refreshInstalledThenApply(pluginViewFor);
    }
  }
  pluginHost?.applyGeometry(surface, pluginViewFor(surface?.pluginId));

  // The pet's own body: a plugin may draw it (manifest `body`), and this is where the frame
  // is placed. What the host still owns is the BOX - which is why the numbers passed on are
  // the pet's own, not a rectangle the plugin declared: the pet's size is the user's setting,
  // and a body that measured itself would draw off its own feet at any scale but 1.0.
  syncPetBody(layout);

  // Nothing here knew whether the box above belongs to a plugin, because the installed list
  // has not been read yet. This is the same "read it on demand" rule the surface follows, and
  // it is what makes the first paint independent of the order things happen in: the 1.5 s poll
  // is a long time to show the wrong pet.
  if (!installedPlugins) refreshInstalledThenApply();
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
  // Same reason as refreshInstalled: a frame mounted from here must already know what this
  // program serves, or its `init` goes out without `host`.
  ensureHostInfo()
    .then(() => App.PluginsInstalled())
    .then((rows) => {
      installedPlugins = rows || [];
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
 * The installed plugin that draws the pet, or null.
 *
 * Three gates, and they are the same three the headless frame needs: installed (it is in the
 * list), enabled (switching a plugin off must stop it doing anything - a disabled plugin still
 * painting the pet would be the loudest possible version of "disable does not work"), and
 * holding the permission the install dialog words. Go refuses a manifest that declares `body`
 * without `pet.surface`, so that last gate can only fail for a plugin whose grants were
 * narrowed after the fact - which is exactly when the pet must go back to its own picture.
 *
 * The FIRST eligible row wins, in install order. There is one pet: two plugins drawing halves
 * of it is not a state the user could resolve, so the page decides deterministically and says
 * so once, in the console, where a surprising result is diagnosable.
 */
function petBodyPlugin() {
  const claimed = (installedPlugins || []).filter(
    (p) => p && p.body === true && p.enabled === true && (p.granted || []).includes('pet.surface'),
  );
  if (claimed.length > 1) {
    console.warn(
      `pet body: ${claimed.map((p) => p.id).join('、')} 都声明了 body，只有 ${claimed[0].id} 会画桌宠`,
    );
  }
  return claimed[0] || null;
}

/**
 * Mounts (or removes) the frame that draws the pet, and keeps its size in step.
 *
 * Both halves matter: the row decides WHOSE document it is, the rectangle decides how big the
 * pet currently is (桌宠大小), and both arrive through `applyLayout` - which runs on every
 * state push, including every frame of a drag. `syncBody` therefore has to be cheap and
 * idempotent, and it is: an unchanged (id, version, grants) keeps the document, and an
 * unchanged rectangle sends nothing.
 */
function syncPetBody(layout) {
  if (!pluginHost) return;
  const row = petBodyPlugin();
  const rect = layout && layout.petW > 0
    ? { x: 0, y: 0, w: layout.petW, h: layout.petH }
    : null;
  pluginHost.syncBody(row, rect);
  // The bundled picture steps aside while a plugin draws the pet: two pets at once is the one
  // outcome that is unambiguously wrong, and a half-transparent plugin drawing would show both.
  els.pet.classList.toggle('body-from-plugin', !!row);
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
  const wasSignature = bubbleSignature(bubble);
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

  // Re-render whenever the CARD'S CONTENT changed, not only when its kind did.
  //
  // This guard used to be `kindChanged` alone, and that was wrong in a way the user felt before
  // anyone read the code: two `bubble.say` lines in a row are both kind "reminder", so the second
  // one's text never replaced the first's. What the user saw was the SAME sentence coming back -
  // 「怎么一直弹这个窗口」 - because the card they were being shown was the one written minutes
  // earlier. It also meant the button row could not follow the content (see showTodoAction).
  //
  // The timer is still rescheduled only on a content change: a geometry-only layout (the pet moved,
  // the region was recut) arrives many times a second during a drag, and resetting the countdown on
  // each one would make a reminder bubble live for ever.
  if (kindChanged || wasSignature !== bubbleSignature(next)) {
    renderBubble(next);
    scheduleBubbleHide(next);
  }
  els.bubble.classList.remove('hidden');
}

/**
 * What makes two bubbles the SAME card, for the "does it need repainting" test above.
 *
 * Geometry is deliberately absent: a card that only moved is the same card, and repainting it would
 * restart its countdown.
 */
function bubbleSignature(b) {
  if (!b) return '';
  const items = (b.items || [])
    .map((i) => `${i.id}|${i.title}|${i.due || ''}|${i.status || ''}`)
    .join(';');
  return [b.kind, b.title, b.more || 0, b.showTodoAction ? 1 : 0, items].join('\u0001');
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
  // 「看看待办」 only when the sentence is actually ABOUT the list.
  //
  // Every `bubble.say` used to get this button, so a chat reply ("在呢在呢！…") came with a
  // 「看看待办」 that opened a list it had nothing to do with - the user read that as the window
  // misbehaving. Go decides now: a plugin has to ask for it, and the reminder (whose whole content
  // IS the list) is the one caller that does.
  els.bubblePrimary.classList.toggle('hidden', !b.showTodoAction);
  els.bubblePrimary.textContent = '看看待办';
  els.bubblePrimary.onclick = () => App.OpenTodoFromBubble().then(applyLayout).catch(fail);
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

/**
 * Draws the plugin-supplied line above the pet's head, or hides it.
 *
 * The text is OPAQUE here: `24:59`, `专注 24:59`, `3 项` - the page never parses it, never
 * decides what it means, and never asks which plugin sent it. That is the whole reason the
 * capability exists as `pet.badge` (a string) rather than as "show the pomodoro timer": the
 * host stays ignorant of what a pomodoro is (app_pet_badge.go).
 *
 * Empty text hides the element rather than leaving an empty pill on the pet - a plugin that
 * pauses or finishes clears by sending "".
 */
function renderClock(text) {
  const value = typeof text === 'string' ? text : '';
  if (!value) {
    els.clock.classList.add('hidden');
    els.clock.textContent = '';
    return;
  }
  els.clock.textContent = value;
  els.clock.classList.remove('hidden');
}

function renderBadge() {
  if (!state) return;
  // From the TODO PLUGIN's published list, computed by Go: the host has no stats of its own any
  // more (State.Stats went with the host's todo store).
  //
  // Defaulted field by field rather than `|| {}` on the whole object: a snapshot that carries the
  // object but omits one counter would otherwise compare `undefined > 0` and lose the badge's
  // colour without failing anywhere.
  //
  // THIS LINE WAS ONCE A COMMENT. A rewritten comment ended up with literal backslash-n instead of
  // real newlines, so `const { open, ... }` was swallowed by the `//` above it - which made `open`
  // resolve to the GLOBAL `window.open`, and the badge rendered `function open() { [native code] }`
  // on the pet's shoulder. A missing declaration in sloppy-mode-looking code is a ReferenceError in
  // a module; here it was not even that, because a global with the same name existed.
  const { open = 0, overdue = 0, dueSoon = 0 } = state.todoBadge || {};
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
  // 日程: whoever claims the `todos` slot, through its own floating surface - the column beside
  // the pet. The slot name is resolved from the installed list, so the host never has to know
  // which plugin is "the todo one" (the same idea as the chat slot, see app_chat_slot.go).
  //
  // The fallback used to be "open the panel's 待办 tab", and that tab is gone with the panel -
  // the list is the plugin's data. So the fallback is now the same one the 番茄钟 cell uses:
  // the control center, where the plugin can be installed, enabled and granted. A cell that
  // quietly did nothing would read as a broken button.
  todos: () => openSlotPluginSurface('todos', () => launchControlCenter()),

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
  // 空列表上的「立刻提醒」是正常结果，不是失败（宿主在 INFO 里记了原因）。这里必须显式接住那个
  // 拒绝：不接就是未处理的 promise rejection，页面会把它当 ERROR 报给日志——2026-10-31 四秒里
  // 就攒了 7 条，把 ERROR 这一档教成了噪音。
  remind: () => App.RemindNow().then(applyState).catch(() => {}),

  plugins: () => launchControlCenter(),

  // 主题: flips the shared scheme. The repaint arrives as a `pet:layout` event the host pushes after
  // writing it (Layout.Theme is the page's single source), so there is nothing to apply here - and
  // passing the binding's STRING answer to applyLayout is what made the menu vanish and come back
  // unchanged: `layout.petX` off a string is undefined, and the pet got positioned at "undefinedpx".
  theme: () => App.ToggleCardTheme().catch(fail),

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
  renderBadge();
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
    // The plugin's line on the pet is separate state (a string, not part of State), so it is
    // read here rather than folded into applyState - a reload in the middle of a countdown must
    // not come back with a blank pet. Deliberately not awaited inside the try: an older binary
    // or the test harness may not have the binding, and that must not fail the whole refresh.
    App.PetBadge?.()
      .then(renderClock)
      .catch(() => {});
  } catch (err) {
    fail(err);
  }
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
 *
 * THE LIST MUST MATCH internal/plugins/manifest.go's validateBackground, and it did not:
 * `plugin.call` was added there (a plugin that is ASKED by another plugin has no host
 * events, no socket of its own and no polling schedule), and this copy kept the old
 * three. The symptom on a real machine: `plugin.call ok` in the log — the gate said yes —
 * and then "问不到 … 它此刻没有常驻帧", because no frame had ever been mounted for the
 * memory core. Two lists, one truth: change both, and the check in
 * .dev/check-frontend.cjs asserts this one names all four.
 */
function backgroundPlugins() {
  return (installedPlugins || []).filter((p) => {
    if (p.enabled !== true || p.background !== true) return false;
    if (!Array.isArray(p.granted)) return false;
    return p.granted.includes('events') ||
      p.granted.includes('net.ws') ||
      p.granted.includes('net.fetch') ||
      p.granted.includes('plugin.call');
  });
}

/**
 * Re-reads the installed plugins and re-mounts the headless frames.
 *
 * The list is read on demand rather than at startup because the market program owns
 * installation: the pet must not cache "no chat plugin" from launch and then keep using a
 * stale answer.
 *
 * It used to be called refreshChatSlot, and it also rendered the panel's 对话 tab. The tab is
 * gone, and dropping it nearly took the ONLY re-read path with it: the tab's watcher was the
 * one thing that noticed an install, an update or a disable made by the market program while
 * the pet was running. The surfaces still resolve on demand (pluginViewFor), but the HEADLESS
 * FRAMES are mounted from a list that nothing else refreshes - so their sync is what this
 * function exists for now, and the poll below is what calls it.
 */
async function refreshInstalled() {
  // One read at a time. Two overlapping reads could land out of order and mount frames from
  // a list that has already been replaced.
  if (installedRefreshInFlight) return installedPlugins;
  installedRefreshInFlight = true;
  try {
    // What this program serves, before any frame is mounted: `init.host` is how a plugin
    // learns which methods exist HERE, instead of hardcoding the slot→method matrix (see
    // PluginHost.setHostInfo). Memoised, so this costs one round trip per launch.
    await ensureHostInfo();
    try {
      installedPlugins = await App.PluginsInstalled();
    } catch (err) {
      // A failed read must not leave the previous list in place: pretending a plugin is
      // still installed would mount an iframe whose files may be gone.
      installedPlugins = [];
      fail(err);
    }
    // The headless frames are resolved from the SAME read. Both are answers to "what is
    // installed right now", so splitting them across two reads would let the frames describe
    // a list that has already been replaced - which was a real defect while the panel existed.
    renderBackgroundPlugins();
    // ...and so is the pet's body: installing, disabling or ungranting the plugin that draws
    // the pet has to change what is on the pet without a restart, which is the whole reason
    // this list is re-read on a timer.
    syncPetBody(lastLayout);
    return installedPlugins;
  } finally {
    installedRefreshInFlight = false;
  }
}

/**
 * Reads the host's capability table once, and hands it to the plugin host.
 *
 * A failure is not fatal: `init.host` is an optional field, and a plugin that does not find it
 * falls back to `surface.slot` - the rule that existed before this. So the catch keeps the
 * pet's plugins working on a build where the binding is missing, rather than taking the whole
 * plugin subsystem down over an optimisation.
 */
let hostInfoPromise = null;
function ensureHostInfo() {
  if (!hostInfoPromise) {
    hostInfoPromise = App.PluginHostInfo()
      .then((info) => {
        pluginHost?.setHostInfo(info);
        return info;
      })
      .catch(() => {
        // Not memoised as a success: the next poll retries, because a binding that failed
        // once (a page loaded against an older exe) may well work on the next attempt.
        hostInfoPromise = null;
        return null;
      });
  }
  return hostInfoPromise;
}

/**
 * How often the installed list is re-read.
 *
 * The market is a SEPARATE PROCESS with its own file, and Go's State carries no plugin
 * information, so there is nothing to subscribe to: the only way to learn that a plugin was
 * installed, updated, disabled or removed is to ask again.
 *
 * 1.5 s is the period the panel's tab watcher used, and it was chosen for exactly this
 * problem: a background plugin switched off in the market has to stop running in the pet
 * without a restart, and its headless frame is what "running" means. Live-reloading a frame
 * is not a visible action, so the cost is a round trip per period rather than a flicker.
 */
const INSTALLED_WATCH_MS = 1500;
// --- wiring ---------------------------------------------------------------

function wire() {
  // --- plugins ------------------------------------------------------------
  // The host owns every sandboxed frame on the page: the floating surface's frames and the
  // headless ones. It is the only object here that knows about plugin interfaces.
  //
  // It used to take a third mount point (`chatSlot`, the panel's 对话 tab body). The panel
  // is gone and the chat plugin renders itself as a floating surface, so the slot frame and
  // the whole openSlot/closeSlot pair went with it - see plugin-host.js.
  pluginHost = new PluginHost(
    {
      pluginSurface: els.pluginSurface,
      pluginBackground: els.pluginBackground,
      petBody: els.petBody,
    },
    { fail, debug },
  );

  // The installed list is not known until it is read, and both the surface routing and the
  // headless frames are resolved from it, so it is read here rather than rendered.
  refreshInstalled();
  renderBackgroundPlugins();

  // No 打开控制中心 button on the page any more: it lived in the panel header, and the panel
  // is gone. The tray menu item and the pet's own 插件/设置 cells are the way in now - see
  // PET_MENU_ACTIONS.

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
        // race the window move and use a stale rectangle.
        await startPromise;
        applyLayout(await App.EndDrag());
      }
      // A press that never moved is a DELIBERATE no-op now.
      //
      // It used to toggle the panel, then (briefly) also open the conversation. Both
      // destinations are plugin surfaces today: 待办 and 对话 are reached from the pet's own
      // right-click menu and from the hover button, and there is no panel left to toggle.
      // A single click that did nothing visible would be worse than no gesture, so the
      // click is simply not wired - the pet's title says what the gestures are.
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

  // `pet:chat-open` is gone, and both halves went together: it told the PANEL to switch to
  // its 对话 tab, the tab went with the panel, and Go's OpenChat (its only emitter) went
  // with them. A listener left behind would be a subscription nothing ever sends - which is
  // what the static gate's dangling-event scan reports, and it is right to.
  //
  // The conversation is opened by a BINDING now (OpenChatPluginSurface), because it is the
  // chat plugin's floating surface and the host has no tab to select.

  // The line a plugin draws on the pet itself. Go only emits when the VISIBLE text changed,
  // so a per-second timer push costs one message per second at most, and a paused timer
  // sends "" once.
  EventsOn('pet:badge', (payload) => {
    renderClock(payload?.text ?? '');
  });

  // The pet was hidden or shown from outside the page (the global shortcut, or
  // the tray). The window is hidden natively - the page is not told to hide
  // itself - so this only has to keep the rendering honest: the shortcut state
  // lives in State, and Go folds a hidden pet's bubbles and surfaces away.
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

  // Escape unwinds the most transient thing on this page - a bubble - and nothing more.
  //
  // There were three rungs below it: a record edit, a form edit and the panel. All three
  // belonged to the panel (the archive, the todo form, the section itself), so all three are
  // gone. The conversation overlay is not in this list either: its interface belongs to the
  // plugin, and the plugin decides what Escape means inside its own document.
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    // Nothing on this page is a gate any more. The two that were - the summary prompt and
    // the plan prompt - held the host timer's break and focus, and they belong to the
    // pomodoro plugin now, whose own surface decides what Escape means there. So a bubble is
    // always dismissable here, and DismissBubble no longer refuses anything.
    if (bubble) App.DismissBubble().then(applyLayout).catch(fail);
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

// THE WATCHER THAT REPLACED THE PANEL TAB'S.
//
// The panel's 对话 tab was refreshed by a 1.5s poll, and that poll was the ONLY thing that
// noticed the market program installing, updating, disabling or removing a plugin while the
// pet ran. Deleting the panel deleted the poll, and with it the headless frames' ability to
// follow the installed list - a background plugin switched off in the control center would
// have kept running in the pet until the next launch. This is that poll, minus the tab: it
// re-reads the list and re-syncs the frames, which is the part that was load-bearing.
setInterval(() => {
  refreshInstalled().catch(() => {});
}, INSTALLED_WATCH_MS);

// Keep the badge honest between full refreshes.
//
// There is no "next reminder at" line here any more: the host has no reminder schedule to
// display (the todo plugin owns reminders now), so this poll only re-reads what it still
// renders. The badge itself now counts the list the TODO PLUGIN publishes, which Go reads
// from the plugin's `share` snapshot - so an edit in the plugin shows up here one poll late.
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
