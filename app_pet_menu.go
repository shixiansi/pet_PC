package main

import "PetPC/internal/store"

// This file owns the PET'S OWN MENU: the grid of icon buttons that a right-click
// on the pet opens.
//
// It is a BUBBLE (BubbleMenu), not a fourth placeholder in the slot beside the
// pet. That is the whole design, and it is why the feature costs so little:
//
//   - the window region already crops to "the pet plus whatever the bubble is",
//     so the menu needs no geometry of its own: bubbleLogicalHeight(BubbleMenu) is
//     its box, and app.go's region switch is untouched (AGENTS.md §3.2);
//   - the page already renders bubbles FROM THE LAYOUT, so the menu arrives through
//     the same one-way push as the reminder and the prompts - no second state
//     channel, and no second crop that could disagree with the first;
//   - everything that folds a bubble away - opening the panel, opening a plugin's
//     floating surface, hiding the pet, dragging it, Escape, a phase transition -
//     folds the menu away too, for free. A new placeholder kind would have had to
//     be added to every one of those paths, and the one that was missed would keep
//     the window cropped around a card nobody draws.
//
// What is NOT reused is the thing it replaces. The pet's right-click used to pop
// the TRAY's own Win32 menu (App.OpenPetMenu -> Tray.ShowMenuAtCursor), and the
// result was that right-clicking the pet looked exactly like right-clicking the
// tray icon: a column of grey system text with no picture, no relation to the card
// the pet speaks in, and (the part that prompted this) no room to grow. The tray
// keeps that menu - it is the program-level list, and it is the only way back when
// the pet is hidden - and the pet now has its own.
//
// The ACTIONS are deliberately not re-listed here. Every cell is a page-side
// button calling the binding the action already had, so "开启/关闭番茄钟" from the
// menu and from the tray are the same call, and this file defines no second
// version of what an item does.

// Nothing refuses the menu any more, and the reason is worth keeping.
//
// It used to yield to "a question the user has not answered" - the pomodoro picker -
// because the menu TAKES the slot rather than sharing it, so opening it over the
// picker would not have covered the question, it would have deleted it and left a
// half-made choice with nothing on screen to finish it with. The picker went with the
// host's timer: both pomodoro questions belong to the pomodoro plugin, which asks
// them in its own surface, so the host has no question left to protect
// (openspec/changes/todo-pomodoro-as-plugins, design.md §4.1).
//
// A reminder bubble is still folded away by the menu, which is right: it is a
// notification, and the user just asked for something else.

// OpenPetMenu shows the pet's own menu and returns the layout it cropped to.
//
// It replaces the pet's right-click, so it is called with the pointer sitting on
// the pet and the card appears beside it - in the slot the panel, the bubble and a
// plugin's floating surface take turns in.
//
// A crop failure is deliberately NOT reported as an error. showBubble has already
// recorded the bubble in the state and in the layout, so the failure is "the
// window could not be re-cropped", not "the menu did not open" - returning the
// error would tell the frontend the opposite of what its next GetLayout says.
// OpenChatPluginSurface takes the same position for the same reason.
func (a *App) OpenPetMenu() (Layout, error) {
	layout, err := a.showBubble(&BubbleView{Kind: BubbleMenu})
	if err != nil {
		a.log.Warnf("pet", "桌宠菜单：重新裁剪窗口失败：%v", err)
		return a.currentLayout(), nil
	}
	return layout, nil
}

// --- 设置：哪些格子显示 ---------------------------------------------------------
//
// The menu's 设置 cell turns the grid into a list of switches: the user decides which
// cells they want to see, and the choice is saved in settings.json. The cells
// themselves are markup and their actions are bindings the page already called, so
// this section is NOT a second definition of the menu - it answers exactly one
// question: "may this cell be switched off?"
//
// Why the host has an opinion at all (and not just the page):
//
//   - 设置 must never be hideable. It is the only way back to this page, so a file
//     that hides it leaves the user with a menu they can shrink and never grow - the
//     kind of "the feature ate itself" that only shows up in support threads.
//   - 退出 must never be hideable. The tray still has it, but a pet-side menu with no
//     way out is a trap, and the tray is not where someone looks for a way out.
//
// The rule is enforced here, on the READ path as well as the write path (GetState
// normalises), because settings.json is a plain file the user can edit.

// petMenuCell is one cell of the pet's menu as the host knows it.
type petMenuCell struct {
	// ID matches the cell's `data-menu` in frontend/index.html and its key in
	// PET_MENU_ACTIONS in frontend/src/main.js. A static check compares the three.
	ID string
	// Label is the row text on the 设置 page.
	Label string
	// Fixed marks 「只能开不能关」: the cell is always visible.
	Fixed bool
}

// petMenuCells is the whole menu, in the order the 设置 page lists it.
//
// The order is also the order of the file: NormalisePetMenuHidden emits ids in this
// order, so the same selection always produces the same bytes and a diff of
// settings.json stays readable.
var petMenuCells = []petMenuCell{
	{"todos", "日程", false},
	{"chat", "对话", false},
	{"pomodoro", "番茄钟", false},
	{"remind", "提醒", false},
	{"plugins", "控制中心", false},
	{"theme", "主题", false},
	{"snap", "归位", false},
	{"hide", "隐藏", false},
	{"settings", "设置", true},
	{"quit", "退出", true},
}

// NormalisePetMenuHidden keeps the ids that are known AND hideable, de-duplicated,
// in menu order. Unknown ids are dropped rather than kept: a cell that no longer
// exists must not stay in the file forever (it would come back the day someone
// reuses the id for a different cell), and a fixed id is dropped because hiding it
// is not a thing the user can ask for.
func NormalisePetMenuHidden(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []string
	for _, cell := range petMenuCells {
		if cell.Fixed || !want[cell.ID] {
			continue
		}
		out = append(out, cell.ID)
	}
	return out
}

// PetMenuCells is the menu as the settings page lists it: id, label, and whether the
// user is allowed to switch it off. The page draws its rows from this rather than
// keeping a copy, so the "只能开不能关" rule cannot be forgotten on one side.
func (a *App) PetMenuCells() []PetMenuPrefs {
	out := make([]PetMenuPrefs, 0, len(petMenuCells))
	for _, cell := range petMenuCells {
		out = append(out, PetMenuPrefs{ID: cell.ID, Label: cell.Label, Fixed: cell.Fixed})
	}
	return out
}

// PetMenuPrefs is one row of the 设置 page.
type PetMenuPrefs struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Fixed bool   `json:"fixed"`
}

// SetPetMenuHidden saves which cells the user switched off and returns the state the
// page should render from.
//
// Returning State (not just the list) is deliberate: the page already repaints from
// one State snapshot, and a separate "here is your new list" answer would be a second
// source of truth for the same grid - the shape that made the todo badge disagree
// with the todo list once.
//
// A fixed id in the request is not an error: the page does not offer those switches,
// and a stale page (or a hand-rolled call) that sends one gets it dropped. Refusing
// would be telling the user off for something they cannot see.
func (a *App) SetPetMenuHidden(hidden []string) (State, error) {
	clean := NormalisePetMenuHidden(hidden)
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.MenuHidden = clean }); err != nil {
		return State{}, err
	}
	return a.GetState()
}
