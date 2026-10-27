package main

import (
	"testing"

	"PetPC/internal/desktop"
	"PetPC/internal/store"
)

// The global shortcuts and the pet's visibility.
//
// The tests here are about the ONE property that makes this feature safe: hiding
// must never be a state the user cannot leave. Registration itself cannot be
// exercised without a real window (this harness has none), so the OS-facing half
// is covered by internal/desktop's own tests and by the manual checks in
// README.md; what is pinned here is the state machine around it.

// TestHotkeysAreOffByDefault is the promise the README makes. It matters more
// than the other defaults: a global hot key takes the combination away from every
// other application, so claiming one unasked could break a shortcut in the user's
// editor.
func TestHotkeysAreOffByDefault(t *testing.T) {
	if store.DefaultConfig().GlobalHotkeys {
		t.Error("global hot keys are on by default, so the pet takes a shortcut unasked")
	}
}

// TestHotkeyViewReportsTheDocumentedKeys pins the labels the panel prints, so the
// shortcut the user is told about is the one that was registered.
func TestHotkeyViewReportsTheDocumentedKeys(t *testing.T) {
	a := newLLMTestApp(t)

	view := a.hotkeyView()
	if view.Show != "Ctrl+Alt+S" || view.Hide != "Ctrl+Alt+H" {
		t.Errorf("labels are show=%q hide=%q, want Ctrl+Alt+S / Ctrl+Alt+H", view.Show, view.Hide)
	}
	if view.Enabled {
		t.Error("the view claims the feature is on by default")
	}
	if view.Active {
		t.Error("a disabled feature claims its shortcuts are active")
	}
	if view.Note == "" {
		t.Error("the disabled state has no explanation for the user")
	}
}

// TestHotkeyViewNeverClaimsActiveWithoutTheOS is the honesty rule: the panel's
// "已生效" line must reflect what the OS accepted, not what the config asked for.
//
// With no window this harness cannot register anything, so the strongest form of
// the assertion is that switching the feature on does not by itself produce
// Active=true from a view that never reached the OS.
func TestHotkeyViewNeverClaimsActiveWithoutTheOS(t *testing.T) {
	a := newLLMTestApp(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.GlobalHotkeys = true }); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	// No manager has been started in this harness, so nothing was registered.
	// applyHotkeys must not invent an Active view from the config alone.
	a.applyHotkeys()

	if view := a.hotkeyView(); view.Active {
		t.Errorf("the view claims the shortcuts work without the OS accepting them: %+v", view)
	}
}

// TestHotkeyViewKeepsTheSwitchOn is the regression test for the switch that could
// not be turned on.
//
// Enabled and Active are two different facts: Enabled is "the user asked for
// this" (the config), Active is "the registration took" (the OS). The manager
// builds its view from a bare literal, so its Enabled is Go's zero value - false.
// Reporting that as the switch position made the panel write the checkbox back to
// OFF the instant a save came back, so every click appeared to bounce straight off
// and the switch flashed instead of staying on. That is a real bug this project
// shipped once, and it is what this test exists to stop coming back.
func TestHotkeyViewKeepsTheSwitchOn(t *testing.T) {
	a := newLLMTestApp(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.GlobalHotkeys = true }); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	// Even with no manager at all (no window in this harness), the view the panel
	// renders must say the switch is ON, because the user turned it on.
	view := a.hotkeyView()
	if !view.Enabled {
		t.Errorf("the switch reads as OFF after being turned on, so the click bounces back: %+v", view)
	}
	if view.Active {
		t.Errorf("the view also claims the keys work, which the OS never confirmed: %+v", view)
	}

	// Turning it off must read as off, and must not claim Active.
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.GlobalHotkeys = false }); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	off := a.hotkeyView()
	if off.Enabled {
		t.Errorf("the switch reads as ON after being turned off: %+v", off)
	}
	if off.Active {
		t.Errorf("a disabled feature claims its shortcuts are active: %+v", off)
	}
	if off.Note == "" {
		t.Error("the off state explains nothing")
	}
}

// TestEnabledAndActiveAreIndependent pins the pair as separate facts in both
// directions, so neither can be derived from the other.
func TestEnabledAndActiveAreIndependent(t *testing.T) {
	// Enabled but not active: switched on, and the OS refused (or has not
	// answered). This is the state the flashing bug misrendered.
	a := newLLMTestApp(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.GlobalHotkeys = true }); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	// The manager's own view for a refused registration: enabled, not active, and
	// carrying the reason.
	refused := desktop.HotkeyView{
		Enabled:  true,
		Show:     "Ctrl+Alt+S",
		Hide:     "Ctrl+Alt+H",
		Active:   false,
		Problems: []string{"Ctrl+Alt+H：已被其他程序占用，或系统不允许"},
	}
	a.hotkeyMu.Lock()
	a.hotkeys.view = refused
	a.hotkeyMu.Unlock()

	view := a.hotkeyView()
	if !view.Enabled {
		t.Error("a refused registration reported the switch as off")
	}
	if view.Active {
		t.Error("a refused registration reported the shortcuts as working")
	}
	if len(view.Problems) == 0 {
		t.Error("a refused registration reported no problem")
	}

	// And a refused view must not survive being switched off: the keys were
	// released, so the problem is gone.
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.GlobalHotkeys = false }); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	after := a.hotkeyView()
	if len(after.Problems) != 0 {
		t.Errorf("a released shortcut still reports a problem: %+v", after.Problems)
	}
	if after.Active {
		t.Error("a released shortcut still reports as active")
	}
}

// TestManagerViewNeverReportsTheSwitchOff pins the same rule on the manager's own
// output, which is what gets cached and rendered.
//
// It is a pure-function test: no window, no registration, just the view the
// manager hands back for a non-empty spec set.
func TestManagerViewNeverReportsTheSwitchOff(t *testing.T) {
	// The platform manager is Windows-only, so this asserts the SHARED helpers
	// that build its views. Anything enabled must not come back Enabled=false.
	on := desktop.ConfiguredHotkeyView(true)
	if !on.Enabled {
		t.Error("ConfiguredHotkeyView(true) reports the switch as off")
	}
	off := desktop.ConfiguredHotkeyView(false)
	if off.Enabled {
		t.Error("ConfiguredHotkeyView(false) reports the switch as on")
	}
	// The unavailable platform is still "the user asked for it": saying false
	// would render their switch as off and make the click look broken.
	unavailable := desktop.DisabledHotkeyView()
	if unavailable.Enabled {
		t.Error("the disabled view reports the feature as enabled")
	}
}

// TestConfiguredHotkeyViewIsHonest covers the pre-startup view, which is what the
// panel renders on the very first state read.
func TestConfiguredHotkeyViewIsHonest(t *testing.T) {
	off := desktop.ConfiguredHotkeyView(false)
	if off.Enabled || off.Active {
		t.Error("the switched-off view claims the feature is working")
	}
	if off.Note == "" {
		t.Error("the switched-off view explains nothing")
	}
	if off.Show != "Ctrl+Alt+S" || off.Hide != "Ctrl+Alt+H" {
		t.Errorf("wrong labels: %q / %q", off.Show, off.Hide)
	}

	on := desktop.ConfiguredHotkeyView(true)
	if !on.Enabled {
		t.Error("the switched-on view reports the feature as off")
	}
	// Still not Active: the OS has not been asked yet, and claiming otherwise
	// would be the exact lie the view exists to avoid.
	if on.Active {
		t.Error("the configured view claims the OS accepted the registration")
	}
}

// TestPetStartsVisible: hiding is never persisted, so every launch starts with
// the pet on screen. That is the backstop that cannot fail - whatever goes wrong
// with the shortcuts, restarting the app brings the pet back.
func TestPetStartsVisible(t *testing.T) {
	a := newLLMTestApp(t)
	if a.petHidden() {
		t.Error("a fresh app reports the pet hidden")
	}
	st, err := a.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if !st.PetVisible {
		t.Error("a fresh app reports petVisible=false in its state")
	}
}

// TestHiddenStateIsNotPersisted is the backstop, stated as a storage contract.
//
// "Hidden" lives in the App struct and NOT in the config on purpose: a persisted
// hidden state would have to be undone by a shortcut that may not be available
// (another program can own the combination), which is the one unrecoverable
// failure this feature could have.
func TestHiddenStateIsNotPersisted(t *testing.T) {
	a := newLLMTestApp(t)

	// Simulate a hide without a window: the state is what is under test, not the
	// ShowWindow call.
	a.mu.Lock()
	a.hidden = true
	a.mu.Unlock()
	if !a.petHidden() {
		t.Fatal("the hidden flag did not take")
	}

	// Reload the config from disk, as a relaunch would. Nothing about visibility
	// may come back with it.
	reloaded, err := store.New(a.store.Dir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	restarted := &App{store: reloaded}
	if restarted.petHidden() {
		t.Error("the pet came back hidden after a restart, so it can be lost")
	}
}

// TestStateReportsVisibility: the panel renders state.petVisible, so it has to be
// in the snapshot rather than only in the event.
func TestStateReportsVisibility(t *testing.T) {
	a := newLLMTestApp(t)
	st, err := a.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if !st.PetVisible {
		t.Error("a visible pet reports petVisible=false")
	}
	a.mu.Lock()
	a.hidden = true
	a.mu.Unlock()
	st, err = a.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.PetVisible {
		t.Error("a hidden pet still reports petVisible=true")
	}
}

// TestHidingIsNotRefusedForAPomodoroQuestion is what replaced the two refusals
// this file used to pin.
//
// Hiding was refused while a finished session waited for its summary, and while the
// plan question waited: both gates held the timer, so taking the prompt off screen
// parked it forever. Those questions belong to the pomodoro plugin now, and the host
// parks nothing behind them - so nothing the host asks may change what hiding does
// (openspec/changes/todo-pomodoro-as-plugins, design.md §4.1).
//
// The picker is the one question the host still raises. The assertion is structural
// rather than about an error string: hiding with it up must fail the SAME way as
// hiding with a clean screen - in this harness that is the missing window, and a
// surviving gate would have produced its own, different refusal.
func TestHidingIsNotRefusedForAPomodoroQuestion(t *testing.T) {
	a := newLLMTestApp(t)

	plain := a.HidePet()

	a.mu.Lock()
	a.setBubbleLocked(&BubbleView{Kind: BubbleReminder, Title: "想专注做哪件事？"})
	a.mu.Unlock()
	withQuestion := a.HidePet()

	if plain == nil || withQuestion == nil {
		t.Fatalf("HidePet claimed to work without a window, so this test proves nothing")
	}
	if plain.Error() != withQuestion.Error() {
		t.Errorf("a question changed the outcome of hiding: %q vs %q", plain, withQuestion)
	}
}

// TestRemindersAreSuppressedWhileHidden: a bubble is painted inside the window, so
// with the pet hidden it would be a notification nobody can see. Dropping it is
// right - the pet was put away on purpose and will get another turn at the next
// interval.
func TestRemindersAreSuppressedWhileHidden(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.ReminderEnabled = true
		c.RemindWhenEmpty = true
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	// Shown: the reminder is allowed to appear (there is nothing due, and
	// WhenEmpty is on, so the empty list is itself the message).
	a.mu.Lock()
	a.hidden = false
	a.mu.Unlock()
	if !a.showReminderBubble(true) {
		t.Fatal("a reminder was refused while the pet was visible, so this test proves nothing")
	}
	// Take it down again, then hide and retry.
	a.clearBubble(BubbleReminder)

	a.mu.Lock()
	a.hidden = true
	a.mu.Unlock()
	if a.showReminderBubble(true) {
		t.Error("a reminder was shown while the pet was hidden, so nobody could see it")
	}
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b != nil {
		t.Errorf("a bubble was left up on a hidden pet: %+v", b)
	}
}
