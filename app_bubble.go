package main

import (
	"fmt"
	"log"
	"time"

	"PetPC/internal/store"
)

// The speech bubble beside the pet: what it says and where it is.
//
// This file used to be the first half of app_pomodoro.go. It was split out when the
// host's pomodoro retired, because the bubble is NOT the timer's: it is the host's
// one way of saying something in the slot beside the pet, and two of its three kinds
// outlived the timer that was named in the file's title.
//
// What is left is what the host still says on its own:
//
//   - BubbleReminder: the MANUAL reminder (RemindNow). The schedule belongs to the
//     todo plugin, but "remind me now" is a user action no plugin can take on the
//     user's behalf, so it stays here (app_reminder.go).
//   - BubbleMenu: the pet's own right-click menu (app_pet_menu.go).
//
// BubblePick died with the timer: it asked "which todo is this focus for?", and the
// pomodoro plugin asks that in its own floating surface now.

// Bubble kinds. The bubble shares the panel's slot beside the pet and is
// mutually exclusive with it; only one of the three states (idle, bubble, panel)
// is ever visible at a time (docs/reminder-pomodoro.md 8.2).
const (
	BubbleNone     = ""
	BubbleReminder = "reminder"
	// BubbleMenu is the pet's own menu: the grid of icon buttons a right-click on
	// the pet opens (app_pet_menu.go).
	//
	// It is a bubble KIND rather than a fourth occupant of the slot beside the pet,
	// and that is the whole trick: the card needs a fixed box, a crop to it, and the
	// rule that anything else opening in the slot folds it away - which is what a
	// bubble already is. Nothing in app.go's region switch, in the layout, or in the
	// "one placeholder at a time" invariant (AGENTS.md §3.2) had to change for it.
	BubbleMenu = "menu"
)

// Bubble sizes in logical pixels.
//
// The sizes are FIXED rather than content-driven because the window region must
// match what the page draws exactly: a region larger than the drawn card leaves
// glass-composited pixels that read as a pale blue band, and a region smaller
// than it clips the card (docs/reminder-pomodoro.md 8.1).
const (
	bubbleWidth          = 300
	bubbleHeightReminder = 140
	// bubbleHeightMenu fits the menu's 3x3 grid of icon buttons: three rows of
	// about 62px, two 6px gaps between them, plus the card's own padding and
	// border. It is FIXED like the others, because the region is cut from it -
	// style.css gives the grid's rows `1fr`, so whatever this number says is what
	// the buttons get, and a number that disagrees with the drawn card would show
	// as a torn edge or a band of desktop.
	bubbleHeightMenu = 226
	// bubbleArrow is the strip reserved inside the box for the pointer triangle.
	// It is inside the box, not outside it, so the arrow can never draw past the
	// window region.
	bubbleArrow = 10
)

// BubbleItem is one row in a bubble's list.
type BubbleItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Due   string `json:"due,omitempty"`
	// Status is "", "soon" or "overdue", matching the panel's colouring.
	Status string `json:"status,omitempty"`
}

// BubbleView is the speech bubble beside the pet: what it says and where it is.
//
// Geometry travels with the content because the region crop is derived from it,
// and the region is what decides whether the pixels under the bubble are visible
// at all.
type BubbleView struct {
	Kind string `json:"kind"`
	// Title is the bubble's heading.
	Title string `json:"title"`
	// Items is the todo list the reminder bubble shows.
	Items []BubbleItem `json:"items,omitempty"`
	// More is how many entries did not fit.
	More int `json:"more,omitempty"`

	// Geometry, in CSS pixels relative to the window's top-left corner.
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	W       float64 `json:"w"`
	H       float64 `json:"h"`
	OnRight bool    `json:"onRight"`
}

// bubbleLogicalHeight returns the fixed height for a bubble kind.
func bubbleLogicalHeight(kind string) int {
	switch kind {
	case BubbleMenu:
		return bubbleHeightMenu
	default:
		return bubbleHeightReminder
	}
}

// DismissBubble closes whatever bubble is up.
//
// Nothing here refuses anything. The two refusals this method used to carry - the
// summary prompt and the plan prompt - were the host's own gates on the break and
// on the focus, and both questions belong to the pomodoro plugin now: the plugin
// asks them in its own surface, so there is no host-side question a dismissal could
// destroy and no parked timer it could strand (design.md §4.1). The picker went with
// the timer, and it was never refused anyway: it is raised by the user's own
// gesture, so waving it away just puts the card back in the menu.
func (a *App) DismissBubble() (Layout, error) {
	a.mu.Lock()
	b := a.bubble
	a.setBubbleLocked(nil)
	a.mu.Unlock()

	if b != nil {
		a.emitBubble(b.Kind, false)
	}
	// Freeing the slot no longer delivers a queued reminder: there is no host-side queue any more,
	// because the todo plugin owns reminders (see app_reminder.go).
	return a.applyWindowGeometry()
}

// showBubble replaces any current bubble and re-crops the window around it.
//
// A bubble and the panel are mutually exclusive: both live in the same slot
// beside the pet, and the region must describe exactly one of them. A plugin's
// floating surface is the third occupant of that slot, so it is folded away
// here too.
//
// There is no longer anything a bubble must not displace. Both remaining kinds are
// notifications the user can wave away - the reminder and the menu - so a bubble
// replacing a bubble is always the right answer, and the "do not overwrite an
// unanswered question" check went with the picker.
func (a *App) showBubble(b *BubbleView) (Layout, error) {
	a.mu.Lock()
	a.panelOpen = false
	// The plugin floating surface shares this slot too, so showing a bubble
	// folds it away: the region can only describe one occupant, and the surface's
	// rectangles would otherwise keep the window cropped around an interface that
	// is no longer being drawn.
	a.setPluginSurfaceLocked(nil)
	a.setBubbleLocked(b)
	a.mu.Unlock()
	a.emitBubble(b.Kind, true)
	return a.applyWindowGeometry()
}

// setBubbleLocked sets or clears the showing bubble and keeps the layout in step.
//
// This is THE place that maintains the invariant "a.layout.Bubble describes the
// bubble that is actually showing". Every mutation of a.bubble goes through here
// (or showBubble), because the frontend renders the bubble FROM THE LAYOUT: if
// the two ever disagree, GetLayout hands the frontend a bubble that no longer
// exists and it is drawn again immediately. That is exactly how "save" came to
// look like it did nothing - the record was written, the bubble was cleared, and
// the stale layout redrew the prompt.
//
// Callers must hold a.mu.
func (a *App) setBubbleLocked(b *BubbleView) {
	if b != nil && b.W == 0 && b.H == 0 {
		// The logical shape travels with the state as soon as the bubble is
		// recorded, not only once the window has been re-cropped. Two things
		// depend on that: a caller that cannot crop (no window yet, a geometry
		// failure) still advertises a bubble whose box is the one the page will
		// draw, and the region pass has a shape to work from rather than a zero.
		// applyWindowGeometry overwrites both with the DPI-scaled values.
		withShape := *b
		withShape.W = float64(bubbleWidth)
		withShape.H = float64(bubbleLogicalHeight(b.Kind))
		b = &withShape
	}
	a.bubble = b
	a.layout.Bubble = b
}

// clearBubble closes a bubble of the given kind, if it is the one showing.
//
// It also re-crops the window: the region has to shrink back to just the pet,
// or the area the bubble occupied stays part of the visible window while the
// page no longer paints there, showing the glass-composited band again.
func (a *App) clearBubble(kind string) {
	a.mu.Lock()
	cur := a.bubble
	matched := cur != nil && cur.Kind == kind
	if matched {
		a.setBubbleLocked(nil)
	}
	a.mu.Unlock()
	if !matched {
		return
	}
	a.emitBubble(kind, false)
	if _, err := a.applyWindowGeometry(); err != nil {
		log.Printf("pet: re-crop after closing bubble: %v", err)
	}
}

// emitBubble tells the frontend that a bubble appeared or went away.
//
// The geometry itself arrives with the layout; this event exists so the
// frontend can run the fade and decide about keyboard focus without having to
// diff the layout.
func (a *App) emitBubble(kind string, shown bool) {
	if a.ctx == nil {
		return
	}
	a.emitEvent("pet:bubble", map[string]any{"kind": kind, "shown": shown})
}

// --- helpers --------------------------------------------------------------

// dueStatusOf classifies a due date for colouring, mirroring the frontend's
// rules so the bubble and the panel agree.
func dueStatusOf(t store.Todo) string {
	if t.Done || t.Due == nil {
		return ""
	}
	now := time.Now()
	if t.Due.Before(now) {
		return "overdue"
	}
	if t.Due.Sub(now) <= 24*time.Hour {
		return "soon"
	}
	return ""
}

// dueLabel renders a due date the way the panel's own formatter does.
func dueLabel(t store.Todo) string {
	if t.Due == nil {
		return ""
	}
	d := *t.Due
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
	diff := int(day.Sub(today).Hours() / 24)
	clock := fmt.Sprintf("%02d:%02d", d.Hour(), d.Minute())
	switch diff {
	case 0:
		return "今天 " + clock
	case 1:
		return "明天 " + clock
	case -1:
		return "昨天 " + clock
	default:
		return fmt.Sprintf("%d月%d日 %s", d.Month(), d.Day(), clock)
	}
}
