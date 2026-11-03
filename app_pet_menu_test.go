package main

import (
	"testing"

	"PetPC/internal/store"
)

// The pet's menu is a BUBBLE kind, and these tests pin exactly that. They are not
// about how the grid looks - that is index.html and style.css - but about the two
// things that would silently break the window:
//
//   - the card must be registered in BOTH a.bubble and a.layout.Bubble, because
//     the page renders it FROM THE LAYOUT and the region is cropped from the same
//     value (app_bubble_test.go pins the rule; this file pins that the menu obeys
//     it);
//   - it must be one of the bubbles everything else folds away, or a card would
//     survive the panel opening, a phase change or a hide and keep the window
//     cropped around an interface nobody draws.

// TestPetMenuCardSizeIsFixed pins the height the window region is cut from.
//
// The size cannot be content-driven: the region is cropped to the rectangle Go
// computes BEFORE the page has laid anything out, so a card that grew with its
// content would be clipped (or leave a band of desktop beside it). style.css fills
// this box with `1fr` rows, which is why one number is enough.
func TestPetMenuCardSizeIsFixed(t *testing.T) {
	if got := bubbleLogicalHeight(BubbleMenu); got != 226 {
		t.Errorf("bubbleLogicalHeight(BubbleMenu) = %d, want the frozen 226", got)
	}
	if BubbleMenu != "menu" {
		t.Errorf("BubbleMenu = %q, want the frozen literal %q", BubbleMenu, "menu")
	}
}

// TestOpenPetMenuShowsTheMenuCard walks the gesture: nothing is up, the menu is
// asked for, and the card becomes the thing in the slot - in the state AND in the
// layout, with the fixed shape already filled in.
func TestOpenPetMenuShowsTheMenuCard(t *testing.T) {
	a := newTestApp(t)

	layout, err := a.OpenPetMenu()
	// No window in this harness: the crop fails, which is the documented "the card
	// is up, the window could not be re-cropped" case - never an error.
	if err != nil {
		t.Fatalf("OpenPetMenu: %v", err)
	}
	if layout.Bubble == nil {
		t.Fatalf("the returned layout advertises no bubble: %+v", layout)
	}

	showing, advertised := a.bubbleState()
	if showing == nil || advertised == nil {
		t.Fatalf("the menu was not registered in both places: showing=%+v advertised=%+v", showing, advertised)
	}
	if showing.Kind != BubbleMenu || advertised.Kind != BubbleMenu {
		t.Fatalf("wrong kind: showing=%q advertised=%q", showing.Kind, advertised.Kind)
	}
	// The logical shape travels with the state even when the window cannot be
	// re-cropped, so the page always has a box to draw into.
	if showing.W != float64(bubbleWidth) || showing.H != float64(bubbleHeightMenu) {
		t.Errorf("menu box = %vx%v, want %dx%d", showing.W, showing.H, bubbleWidth, bubbleHeightMenu)
	}
}

// TestOpenPetMenuTakesTheSlot is the "one thing at a time" invariant: the menu shares the slot
// with a plugin's floating surface, so opening it folds that away. A leftover surface would
// keep the window cropped around rectangles the page is no longer placing anything into.
//
// The panel was the other occupant it folded, and the panel is gone - so the surface alone is
// now the whole of "something else was in the slot".
func TestOpenPetMenuTakesTheSlot(t *testing.T) {
	a := newTestApp(t)

	a.mu.Lock()
	a.setPluginSurfaceLocked(&PluginSurfaceView{PluginID: "com.example.chat"})
	a.mu.Unlock()

	if _, err := a.OpenPetMenu(); err != nil {
		t.Fatalf("OpenPetMenu: %v", err)
	}

	a.mu.Lock()
	surface := a.pluginSurface
	a.mu.Unlock()
	if surface != nil {
		t.Errorf("a plugin surface was left in the slot: %+v", surface)
	}
}

// TestOpenPetMenuRefusesToTakeAQuestion pins the gate.
//
// The menu TAKES the slot rather than sharing it, so opening it over the picker
// would delete the question rather than cover it - and the user's half-made choice
// would be gone with nothing on screen to finish it with. The refusal must change
// NOTHING: the question has to still be there to answer.
//
// The picker is the only question the host still raises. The summary and the plan
// prompts are the pomodoro plugin's now, and it opens its own surface instead of
// borrowing this slot, so there is nothing here to protect them from
// (openspec/changes/todo-pomodoro-as-plugins, design.md §4.1).
func TestOpenPetMenuFoldsTheReminderAway(t *testing.T) {
	a := newTestApp(t)
	a.mu.Lock()
	a.setBubbleLocked(&BubbleView{Kind: BubbleReminder, Title: "今天要做的事"})
	a.mu.Unlock()

	// This test used to assert that the menu REFUSED to take the slot from a question.
	// There is no question left to protect: the picker went with the host's timer, and
	// both pomodoro questions belong to the pomodoro plugin. A reminder is a
	// notification, so the menu replaces it - which is what right-clicking asked for.
	if _, err := a.OpenPetMenu(); err != nil {
		t.Fatalf("OpenPetMenu refused: %v", err)
	}

	showing, advertised := a.bubbleState()
	if showing == nil || showing.Kind != BubbleMenu {
		t.Errorf("the menu is not showing: %+v", showing)
	}
	if advertised == nil || advertised.Kind != BubbleMenu {
		t.Errorf("the layout does not advertise the menu: %+v", advertised)
	}
}

// TestPetMenuClosesLikeAnyBubble is the other half of "it is a bubble": every way
// the app already closes a card must close this one, without the menu teaching the
// backend a second mechanism.
func TestPetMenuClosesLikeAnyBubble(t *testing.T) {
	t.Run("DismissBubble", func(t *testing.T) {
		a := newTestApp(t)
		a.openMenuForTest(t)

		if _, err := a.DismissBubble(); err != nil {
			t.Logf("DismissBubble (no window): %v", err)
		}
		assertNoMenu(t, a)
	})

	t.Run("opening a plugin surface", func(t *testing.T) {
		a := newTestApp(t)
		a.openMenuForTest(t)

		// The panel used to be this half's trigger, and the panel is gone. A plugin's
		// floating surface is the occupant that replaced it, and it must fold the menu away
		// for the same reason: one card at a time in the slot, and the window cropped to
		// whichever one it is.
		a.mu.Lock()
		a.setPluginSurfaceLocked(&PluginSurfaceView{PluginID: "com.example.chat"})
		a.mu.Unlock()
		a.mu.Lock()
		a.setBubbleLocked(nil)
		a.mu.Unlock()

		assertNoMenu(t, a)
	})

	t.Run("clearing the menu's own kind", func(t *testing.T) {
		a := newTestApp(t)
		a.openMenuForTest(t)

		// clearAllBubblesLocked used to fold every kind away when a phase started; it
		// went with the host's timer. What is still worth pinning is that the menu is a
		// bubble of its OWN kind: closing that kind takes the card and the crop with it.
		// A kind that survived its own clear would keep the window cropped around a card
		// nobody draws.
		a.clearBubble(BubbleMenu)

		assertNoMenu(t, a)
	})
}

// openMenuForTest shows the menu and fails the test if it did not register.
func (a *App) openMenuForTest(t *testing.T) {
	t.Helper()
	if _, err := a.OpenPetMenu(); err != nil {
		t.Fatalf("OpenPetMenu: %v", err)
	}
	showing, _ := a.bubbleState()
	if showing == nil || showing.Kind != BubbleMenu {
		t.Fatalf("the menu did not open: %+v", showing)
	}
}

// assertNoMenu fails when the menu is still up in either place.
func assertNoMenu(t *testing.T, a *App) {
	t.Helper()
	showing, advertised := a.bubbleState()
	if showing != nil {
		t.Errorf("the menu is still showing: %+v", showing)
	}
	if advertised != nil {
		t.Errorf("the layout still advertises a bubble: %+v", advertised)
	}
}

// TestPetMenuCellTableIsConsistent guards the table itself: unique ids, and the two
// "must stay on" cells are exactly the two the page cannot do without.
//
// The ids are compared against frontend/index.html by .dev/check-frontend.cjs (a
// static check), so a cell added on one side only fails there rather than in a
// support thread.
func TestPetMenuCellTableIsConsistent(t *testing.T) {
	if len(petMenuCells) != 10 {
		t.Fatalf("petMenuCells has %d rows, want 10 (nine action cells + the bottom strip)", len(petMenuCells))
	}
	seen := map[string]bool{}
	fixed := map[string]bool{}
	for _, cell := range petMenuCells {
		if seen[cell.ID] {
			t.Errorf("duplicate id %q", cell.ID)
		}
		seen[cell.ID] = true
		if cell.Label == "" {
			t.Errorf("%s has no label: the 设置 page would draw a nameless switch", cell.ID)
		}
		if cell.Fixed {
			fixed[cell.ID] = true
		}
	}
	for _, id := range []string{"settings", "quit"} {
		if !fixed[id] {
			t.Errorf("%s must be fixed: hiding it takes away a way out (or the way back in)", id)
		}
	}
	// Everything else must be hideable, or the feature is a lie.
	for _, id := range []string{"todos", "chat", "pomodoro", "remind", "plugins", "theme", "snap", "hide"} {
		if fixed[id] {
			t.Errorf("%s must NOT be fixed: it is a normal action", id)
		}
	}
}

// TestNormalisePetMenuHidden covers the rule that makes 「只能开不能关」 real.
//
// It is a pure function and it is the enforcement point: settings.json is a plain
// file the user can edit, so the filter runs on the way IN and on the way OUT
// (GetState). Enforcing it in one place only would leave the other as the hole.
func TestNormalisePetMenuHidden(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty stays empty", nil, nil},
		{"a hideable cell survives", []string{"theme"}, []string{"theme"}},
		// The whole point: 设置 is the only way back to this page, and 退出 is the
		// only way out of the program from the pet's side.
		{"fixed cells are dropped", []string{"settings", "quit"}, nil},
		{"fixed cells are dropped, the rest kept", []string{"quit", "theme", "settings"}, []string{"theme"}},
		{"unknown ids are dropped", []string{"nope", "sales"}, nil},
		{"duplicates collapse", []string{"theme", "theme"}, []string{"theme"}},
		// Menu order, not the caller's order: the same selection always produces the
		// same bytes, so a diff of settings.json only shows what really changed.
		{"output follows menu order", []string{"hide", "chat", "snap"}, []string{"chat", "snap", "hide"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalisePetMenuHidden(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("NormalisePetMenuHidden(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("NormalisePetMenuHidden(%v) = %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}
}

// TestSetPetMenuHiddenPersistsAndAnswersState walks the binding the page calls.
//
// Two halves matter and they fail differently: the FILE must hold the normalised
// list (a restart must remember the choice), and the ANSWER must already be the
// normalised one (the page repaints from it - an answer that still carried
// "settings" would hide the cell that opens the page).
func TestSetPetMenuHiddenPersistsAndAnswersState(t *testing.T) {
	a := newTestApp(t)

	state, err := a.SetPetMenuHidden([]string{"theme", "settings", "nonsense"})
	if err != nil {
		t.Fatalf("SetPetMenuHidden: %v", err)
	}
	if got := state.Config.MenuHidden; len(got) != 1 || got[0] != "theme" {
		t.Fatalf("State.config.menuHidden = %v, want [theme]", got)
	}
	if saved := a.store.Config().MenuHidden; len(saved) != 1 || saved[0] != "theme" {
		t.Fatalf("settings.json holds %v, want [theme]", saved)
	}

	// Clearing writes an empty list (not a stale one): "all cells back on" has to be
	// expressible, or a user who hid everything could never undo it.
	if _, err := a.SetPetMenuHidden(nil); err != nil {
		t.Fatalf("SetPetMenuHidden(nil): %v", err)
	}
	if saved := a.store.Config().MenuHidden; len(saved) != 0 {
		t.Fatalf("settings.json still holds %v after clearing", saved)
	}
}

// TestGetStateDropsAHiddenSettingsCell is the READ path of the same rule.
//
// settings.json is editable by hand, so a file saying menuHidden:["settings"] must
// not lock the user out of the page that would let them undo it. The file keeps
// what it says (the host does not rewrite it behind the user's back); what gets
// normalised is the snapshot the page renders from.
func TestGetStateDropsAHiddenSettingsCell(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.MenuHidden = []string{"settings", "quit", "theme"}
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	state, err := a.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got := state.Config.MenuHidden; len(got) != 1 || got[0] != "theme" {
		t.Fatalf("State.config.menuHidden = %v, want [theme]", got)
	}
	if saved := a.store.Config().MenuHidden; len(saved) != 3 {
		t.Fatalf("the file was rewritten behind the user's back: %v", saved)
	}
}
