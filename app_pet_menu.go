package main

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
		a.DebugLog("pet menu: re-crop failed: " + err.Error())
		return a.currentLayout(), nil
	}
	return layout, nil
}
