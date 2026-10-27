package main

import (
	"log"

	"PetPC/internal/desktop"
	"PetPC/internal/store"
)

// Hiding is refused for nothing any more. The two refusals that used to live here
// - a finished session waiting for its summary, and the plan question - were the
// host's own pomodoro gates; both questions belong to the pomodoro plugin now, and
// the host neither parks a timer behind them nor holds the window on screen for
// them (openspec/changes/todo-pomodoro-as-plugins, design.md §4.1).

// eventPetVisible tells the frontend the pet was hidden or shown, so the settings
// panel can describe the current state.
const eventPetVisible = "pet:visible"

// Global hot keys and the pet's visibility.
//
// The feature is one promise: if the pet can be hidden, it can be brought back.
// Everything here is arranged around that, because the failure mode is not a
// cosmetic one - a hidden pet with no working shortcut is a program the user can
// only reach through the tray, or through Task Manager.
//
// Three independent ways back exist, and they are deliberately redundant:
//
//  1. Ctrl+Shift+S, the global shortcut (this file).
//  2. The tray menu's 显示桌宠 / 隐藏桌宠 entries, which work with the mouse.
//  3. Restarting the app: the window is always shown on startup, so hiding is
//     never persisted as a state to come back to.
//
// (3) is the backstop that cannot fail, which is why "hidden" is NOT stored in
// the config: a saved hidden state would have to be un-hidden by one of the
// other two, and there is no reason to take that risk for a cosmetic preference.

// hotkeys holds the live manager and the last view it reported.
//
// The view is cached rather than recomputed because it is the answer to "did the
// registration actually take?", which only the OS knows - re-deriving it from the
// config would claim the shortcuts work whenever they are switched on, which is
// exactly the lie the panel must not tell.
type hotkeyState struct {
	manager desktop.HotkeyManager
	view    desktop.HotkeyView
}

// applyHotkeys brings the registered shortcuts in line with the config.
//
// Called at startup and whenever the switch changes. A failure is reported
// through the view rather than by refusing to start: the pet works fine without
// shortcuts, and the panel explains what happened.
func (a *App) applyHotkeys() {
	cfg := a.store.Config()
	specs := desktop.DefaultHotkeys()

	planned, _ := desktop.PlanHotkeys(cfg.GlobalHotkeys, specs)

	if !cfg.GlobalHotkeys {
		// Switching off releases the combinations immediately, so the rest of the
		// desktop gets them back without a restart.
		a.hotkeyMu.Lock()
		mgr := a.hotkeys.manager
		a.hotkeyMu.Unlock()
		if mgr != nil {
			// Register(nil) unregisters everything and returns the disabled view.
			// The result is kept (rather than a hard-coded literal) so the panel
			// renders whatever the manager actually did.
			view := mgr.Register(nil)
			a.hotkeyMu.Lock()
			a.hotkeys.view = view
			a.hotkeyMu.Unlock()
		} else {
			a.hotkeyMu.Lock()
			a.hotkeys.view = desktop.DisabledHotkeyView()
			a.hotkeyMu.Unlock()
		}
		a.DebugLog("hotkeys: disabled")
		return
	}

	a.hotkeyMu.Lock()
	mgr := a.hotkeys.manager
	a.hotkeyMu.Unlock()

	if mgr == nil {
		a.DebugLog("hotkeys: unavailable on this platform")
		return
	}
	view := mgr.Register(planned)
	a.hotkeyMu.Lock()
	a.hotkeys.view = view
	a.hotkeyMu.Unlock()
	for _, p := range view.Problems {
		log.Printf("pet: hotkey %s", p)
	}
	a.DebugLog("hotkeys: registered active=" + boolText(view.Active))
}

// initHotkeys starts the manager, if the feature is switched on.
//
// Started lazily like every other optional subsystem, so a build that never uses
// it never creates the thread or takes a shortcut from another program.
func (a *App) initHotkeys() {
	cfg := a.store.Config()
	if !cfg.GlobalHotkeys {
		a.hotkeyMu.Lock()
		a.hotkeys.view = desktop.DisabledHotkeyView()
		a.hotkeyMu.Unlock()
		a.DebugLog("hotkeys: off at startup")
		return
	}

	mgr, view, err := desktop.NewHotkeys(
		desktop.DefaultHotkeys(),
		a.onHotkey,
	)
	a.hotkeyMu.Lock()
	a.hotkeys.manager = mgr
	a.hotkeys.view = view
	a.hotkeyMu.Unlock()
	if err != nil {
		log.Printf("pet: global hot keys unavailable: %v", err)
		a.DebugLog("hotkeys: FAILED: " + err.Error())
		return
	}
	for _, p := range view.Problems {
		log.Printf("pet: hotkey %s", p)
		a.DebugLog("hotkeys: " + p)
	}
	a.DebugLog("hotkeys: ready active=" + boolText(view.Active))
}

// onHotkey handles a shortcut press.
//
// It is called on the manager's own thread, so it must not block: every action it
// takes is a call that posts to the window (ShowWindow / SetWindowPos), which is
// safe from another thread, and the layout it returns is emitted through the same
// path the tray already uses from its thread.
func (a *App) onHotkey(action desktop.HotkeyAction) {
	switch action {
	case desktop.HotkeyShow:
		a.DebugLog("hotkeys: show")
		a.ShowPet()
	case desktop.HotkeyHide:
		a.DebugLog("hotkeys: hide")
		a.HidePet()
	}
}

// closeHotkeys releases the shortcuts and stops the manager thread.
func (a *App) closeHotkeys() {
	a.hotkeyMu.Lock()
	mgr := a.hotkeys.manager
	a.hotkeys.manager = nil
	a.hotkeyMu.Unlock()
	if mgr != nil {
		mgr.Close()
	}
}

// hotkeyView renders the hot-key state for the settings panel.
//
// Enabled is taken from the CONFIG, and only from the config, while Active comes
// from what the OS last reported. The two are different facts and conflating them
// is a real bug, not a tidiness point:
//
//   - Enabled is "the user asked for this". It is the switch position.
//   - Active is "the registration took". Only the manager can know it.
//
// The manager builds its view from a bare HotkeyView literal, so its Enabled is
// the Go zero value - false. Reporting that as the switch position made the panel
// re-render the checkbox as OFF the moment a save came back, so the user's click
// appeared to bounce straight back and the switch could never be turned on.
func (a *App) hotkeyView() desktop.HotkeyView {
	enabled := a.store.Config().GlobalHotkeys

	a.hotkeyMu.Lock()
	view := a.hotkeys.view
	a.hotkeyMu.Unlock()

	view.Enabled = enabled
	if view.Show == "" && view.Hide == "" {
		// Nothing has been applied yet (before startup finished). The documented
		// labels with the switch position are more honest than blank text: the
		// panel should not flicker empty on the first render.
		view.Show = desktop.DefaultShowHotkey.Label()
		view.Hide = desktop.DefaultHideHotkey.Label()
	}
	if !enabled {
		// Switching off has to LOOK switched off, whatever the last registration
		// reported: the keys were released, so claiming Active would be wrong.
		view.Active = false
		view.Problems = nil
		if view.Note == "" {
			view.Note = desktop.NoteDisabled
		}
	}
	return view
}

// --- visibility ------------------------------------------------------------

// HidePet hides the pet.
//
// Anything beside the pet goes first, exactly as dragging does (docs 9): the
// panel and the overlay are laid out relative to a pet that is about to
// disappear, and leaving them up would put a card on screen with nothing it is
// attached to.
//
// It is a no-op when the pet is already hidden, so a repeated shortcut cannot
// produce an error or a second layout.
func (a *App) HidePet() error {
	hwnd, err := a.resolveWindow()
	if err != nil {
		return err
	}

	a.mu.Lock()
	wasBubble := a.bubble != nil
	// Fold the panel, the bubble and the floating surface away without
	// re-cropping: the window is about to be hidden, and a geometry call would
	// resolve the window and re-crop it for a frame nobody ever sees.
	a.setPluginSurfaceLocked(nil)
	a.setBubbleLocked(nil)
	a.hidden = true
	a.mu.Unlock()

	if wasBubble {
		a.emitBubble(BubbleNone, false)
	}

	if err := desktop.Hide(hwnd); err != nil {
		// Claiming the pet is hidden when it is not would make the next show a
		// no-op, so the flag goes back.
		a.mu.Lock()
		a.hidden = false
		a.mu.Unlock()
		return err
	}
	a.emitVisibility()
	return nil
}

// ShowPet brings the pet back and puts it in front.
//
// The window is only ever hidden, never closed, so this is a ShowWindow plus the
// always-on-top re-assert the desktop helper already does. It also re-crops the
// window, because hiding folded whatever was beside the pet away and the region
// has to follow the state the page is now painting.
func (a *App) ShowPet() error {
	hwnd, err := a.resolveWindow()
	if err != nil {
		return err
	}
	a.mu.Lock()
	wasHidden := a.hidden
	a.hidden = false
	a.mu.Unlock()

	if err := desktop.Show(hwnd); err != nil {
		// Claiming the pet is visible when it is not would make the next show a
		// no-op.
		a.mu.Lock()
		a.hidden = wasHidden
		a.mu.Unlock()
		return err
	}
	a.emitVisibility()
	// Re-crop: the pet is alone now (hiding folded the panel and the bubble away),
	// and showing without re-cropping would leave the region describing the last
	// thing that was up.
	_, err = a.applyWindowGeometry()
	return err
}

// petHidden reports whether the pet is currently hidden.
func (a *App) petHidden() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hidden
}

// The question-reveal cycle used to live here: setQuestionVisibility,
// revealForQuestion and hideAfterQuestion, plus App.revealedForQuestion and the
// App.questionVisibility seam they were testable through.
//
// It existed for ONE reason: hiding the pet is a promise to leave the user alone,
// and both pomodoro gates parked the timer behind a question. Hiding while a
// question was already up was refused (see HidePet), but the reverse order - hide
// the pet, then a phase runs out - would have drawn the prompt into a hidden window
// and parked the timer forever, with no pixels on screen saying so. So a question
// revealed the pet WITHOUT activating it, and answering it put the pet back.
//
// None of that can happen now. Both questions belong to the pomodoro plugin, which
// reveals its own floating surface, and the host's timer is gone: nothing left in
// the host can raise a question, so there is no debt to remember and no cycle to
// run. The mechanism went with the timer it existed for
// (openspec/changes/todo-pomodoro-as-plugins, design.md §4.1).

// emitVisibility tells the frontend the pet was hidden or shown.
//
// It travels as its own event rather than inside the layout because the layout is
// about geometry and the pet's visibility is about the window itself: while the
// pet is hidden there is no meaningful layout to send, and the frontend still has
// to know the state changed so its settings panel can say so.
func (a *App) emitVisibility() {
	if a.ctx == nil && a.emitFn == nil {
		return
	}
	a.emitEvent(eventPetVisible, map[string]any{"visible": !a.petHidden()})
}

// --- binding ---------------------------------------------------------------

// HotkeySettingsInput carries the global-shortcut switch from the settings tab.
type HotkeySettingsInput struct {
	Enabled bool `json:"enabled"`
}

// SaveHotkeySettings persists the switch and applies it immediately.
//
// Applying here rather than at the next launch is the point of a switch: turning
// it off has to give the combinations back to the rest of the desktop now, and
// turning it on has to take effect without a restart. It returns the whole State,
// like every other settings method, because main.js assigns the result straight
// to its `state`.
func (a *App) SaveHotkeySettings(in HotkeySettingsInput) (State, error) {
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.GlobalHotkeys = in.Enabled
	}); err != nil {
		return State{}, err
	}
	// The manager is started lazily, so switching the feature on for the first
	// time has to create it. Switching it off keeps the manager alive but idle:
	// recreating the thread on every toggle would leak a window each time.
	a.hotkeyMu.Lock()
	started := a.hotkeys.manager != nil
	a.hotkeyMu.Unlock()
	if in.Enabled && !started {
		a.initHotkeys()
	} else {
		a.applyHotkeys()
	}
	return a.GetState()
}

// ShowPetForUser is the explicit "bring it back" command.
//
// It is the same action the hot key and the tray perform, exposed as a binding so
// the frontend has a named way to ask. HidePetForUser is its counterpart; there is
// deliberately no panel control that hides the pet, because a control you can
// only reach by opening the panel is a strange way to get the pet out of the way -
// the shortcut and the tray are the real entry points.
func (a *App) ShowPetForUser() (State, error) {
	if err := a.ShowPet(); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// HidePetForUser hides the pet from an explicit command.
func (a *App) HidePetForUser() (State, error) {
	if err := a.HidePet(); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// boolText renders a bool for the debug log.
func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
