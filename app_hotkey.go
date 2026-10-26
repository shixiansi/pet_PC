package main

import (
	"errors"
	"log"

	"PetPC/internal/desktop"
	"PetPC/internal/store"
)

// errSummaryBlocksHiding is returned when hiding is refused because a finished
// session is still waiting for its summary.
//
// The summary prompt is the gate on the break (docs/reminder-pomodoro.md 4.5.1).
// Hiding the pet would take the only way to answer it off screen and leave the
// timer parked forever, so the refusal is the same one Escape and dragging get.
var errSummaryBlocksHiding = errors.New("番茄钟还在等你写总结，先答完它再隐藏桌宠")

// errPlanBlocksHiding is returned when hiding is refused because the plan
// question is still waiting. It is the same promise as the summary refusal, with
// one extra edge: the records tab can answer a summary, so a folded summary
// prompt is recoverable from the panel - a plan question has NO answering surface
// in the panel at all, so taking it off screen is a dead end
// (docs/reminder-pomodoro.md 4.5.2).
var errPlanBlocksHiding = errors.New("番茄钟还在等你写计划，先答完它再隐藏桌宠")

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
// attached to. The summary prompt is the exception, for the same reason Escape
// and dragging except it - it is the gate on the break and has to be answered.
//
// It is a no-op when the pet is already hidden, so a repeated shortcut cannot
// produce an error or a second layout.
func (a *App) HidePet() error {
	// A pending summary prompt is a question, not decoration: hiding the pet
	// would take the only way to answer it off screen and park the timer forever.
	//
	// The gate is checked BEFORE the window is resolved, which matters for the
	// error the user sees: with a question waiting, "先答完它再隐藏桌宠" is the
	// useful answer, and a window-resolution failure would replace it with
	// something about a missing window.
	if a.pendingSummaryWaiting() {
		a.DebugLog("hide: refused, a record is waiting for its summary")
		return errSummaryBlocksHiding
	}
	// The plan question blocks hiding for the same reason, and the check sits
	// beside the summary one rather than after the window is resolved: with a
	// question waiting, "先答完它再隐藏桌宠" is the useful answer, and a
	// window-resolution failure would replace it with something about a missing
	// window.
	if a.pendingPlanWaiting() {
		a.DebugLog("hide: refused, a focus is waiting for its plan")
		return errPlanBlocksHiding
	}

	hwnd, err := a.resolveWindow()
	if err != nil {
		return err
	}

	a.mu.Lock()
	wasBubble := a.bubble != nil
	wasOwed := a.revealedForQuestion
	// Fold the panel, the bubble and the floating surface away without
	// re-cropping: the window is about to be hidden, and a geometry call would
	// resolve the window and re-crop it for a frame nobody ever sees.
	a.panelOpen = false
	a.setPluginSurfaceLocked(nil)
	a.setBubbleLocked(nil)
	a.hidden = true
	// A hide the user asked for supersedes a reveal the question did: from here
	// the pet is away because THIS call put it there, and the answer to a prompt
	// (or the end of one) must not decide to show it.
	a.revealedForQuestion = false
	a.mu.Unlock()

	if wasBubble {
		a.emitBubble(BubbleNone, false)
	}

	if err := desktop.Hide(hwnd); err != nil {
		// Put BOTH back: claiming the pet is hidden when it is not would make
		// the next show a no-op, and dropping the debt would silently cancel the
		// re-hide the question still owes.
		a.mu.Lock()
		a.hidden = false
		a.revealedForQuestion = wasOwed
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
//
// Showing is the user's own request, so it also settles any debt a question
// reveal left behind: whatever happens to a prompt afterwards, this pet stays
// out until it is hidden again.
func (a *App) ShowPet() error {
	hwnd, err := a.resolveWindow()
	if err != nil {
		return err
	}
	a.mu.Lock()
	wasHidden := a.hidden
	wasOwed := a.revealedForQuestion
	a.hidden = false
	a.revealedForQuestion = false
	a.mu.Unlock()

	if err := desktop.Show(hwnd); err != nil {
		// Put BOTH back: claiming the pet is visible when it is not would make
		// the next show a no-op, and dropping the debt would leave a pet that is
		// hidden with nothing left that will ever hide it again.
		a.mu.Lock()
		a.hidden = wasHidden
		a.revealedForQuestion = wasOwed
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

// --- the question reveal ----------------------------------------------------

// setQuestionVisibility performs the native step the reveal cycle needs: make the
// pet's window visible (without activating it) or hidden again.
//
// It is the ONE place either call is made, which is what keeps the state machine
// and the window in step by construction. The injected seam, when there is one,
// replaces the whole step - resolving the handle included - because a windowless
// test has no handle to resolve and the state machine is the half under test.
func (a *App) setQuestionVisibility(visible bool) error {
	if fn := a.questionVisibility; fn != nil {
		return fn(0, visible)
	}
	hwnd, err := a.resolveWindow()
	if err != nil {
		return err
	}
	if visible {
		// SW_SHOWNOACTIVATE, not Show: the user is very likely typing in
		// another application, and an activated pet would swallow their next
		// keystroke into its own input box.
		return desktop.ShowNoActivate(hwnd)
	}
	return desktop.Hide(hwnd)
}

// revealForQuestion brings the pet back so a question the timer is parked behind
// can actually be answered.
//
// WHY THE PET COMES BACK AT ALL. Hiding is a promise to leave the user alone,
// and both the summary prompt and the plan prompt hold the timer: the rest does
// not start until the first is answered and the focus does not start until the
// second is. Hiding was refused while either was ALREADY waiting (see HidePet),
// but that guard only covers the order "question first, then hide". The reverse
// order - hide the pet, then a phase runs out - used to leave the prompt drawn
// into an SW_HIDEd window: the timer parked forever, no pixels on screen saying
// so, and the only way out was to remember the show shortcut. That is the dead
// end the refusal exists to prevent, so it has to be prevented from this side
// too.
//
// It is deliberately NOT a plain ShowPet:
//
//   - The window is shown WITHOUT activation (ShowNoActivate). The user is very
//     likely typing in another application - the whole reason the prompts do not
//     steal focus (docs/reminder-pomodoro.md 4.5) - and an activated pet would
//     swallow the next keystroke into its own box. It also goes through
//     desktop.ShowNoActivate rather than desktop.Show for the same reason: the
//     latter activates by design.
//   - The panel, the overlay and the plugin surface are NOT restored. They were
//     folded away by the hide and the question is the only thing that has to be
//     visible; bringing them back would be a second, larger interruption.
//   - `hidden` becomes false for as long as the question is up, because the
//     window really is on screen: the reminder and proactive gates read that
//     flag, and leaving it true would keep them suppressed against a visible
//     pet.
//
// The user's own "hidden" is remembered in revealedForQuestion, and
// hideAfterQuestion is what puts the pet back. An explicit show - the shortcut,
// the tray, opening the panel - clears the memory, because "I asked for it back"
// must not be undone later by answering a prompt.
//
// A window that cannot be resolved is a no-op rather than an error: the caller
// is a timer tick with nowhere to report a failure, and a reveal that fails
// leaves the state exactly as it was.
func (a *App) revealForQuestion() {
	a.mu.Lock()
	wasHidden := a.hidden
	a.mu.Unlock()
	if !wasHidden {
		return
	}

	if err := a.setQuestionVisibility(true); err != nil {
		// Nothing changed on screen, so nothing may change in the state: the pet
		// is still hidden, and the show shortcut remains the way back.
		a.DebugLog("question reveal: show failed: " + err.Error())
		return
	}

	a.mu.Lock()
	a.hidden = false
	// The debt is recorded only once the window really is visible, so a failed
	// reveal can never make a later answer hide a pet that was never shown.
	a.revealedForQuestion = true
	a.mu.Unlock()
	a.emitVisibility()
	// The re-crop is deliberately NOT done here. Every caller is about to paint
	// something beside the pet (showBubble -> applyWindowGeometry), and that pass
	// computes the region from the state as it then stands, so cropping here
	// would crop to the pet alone and be immediately redone.
	a.DebugLog("question reveal: the pet came back for a question")
}

// hideAfterQuestion puts the pet away again once the question that brought it
// back has been answered.
//
// It is the other half of revealForQuestion, and it runs from every path that
// clears a gate: saving or skipping the summary, answering the plan, giving the
// session up, switching the pomodoro off. It is a no-op unless the window is
// visible ONLY because of a question - revealedForQuestion is set and the user
// has not asked for the pet back in the meantime - so an explicit show is never
// undone, and a pet that was never hidden never moves.
//
// The panel, the overlay and the bubble are folded away exactly as HidePet folds
// them, because they are all painted against a pet that is about to disappear.
// No gate can be waiting here (that is the caller's contract), so nothing on
// screen is lost by doing so.
func (a *App) hideAfterQuestion() {
	a.mu.Lock()
	owed := a.revealedForQuestion
	// Cleared unconditionally, including when the hide below fails. A debt that
	// survives a failure is worse than a missed re-hide: nothing would ever run
	// the answer path again, so it would sit there until the user showed the pet
	// for their own reasons - and then put it away. Failing visible is also the
	// safe direction, because a visible pet is never the dead end this whole
	// cycle exists to prevent.
	a.revealedForQuestion = false
	if !owed || a.hidden {
		a.mu.Unlock()
		return
	}
	a.panelOpen = false
	wasBubble := a.bubble != nil
	a.setPluginSurfaceLocked(nil)
	a.setBubbleLocked(nil)
	a.mu.Unlock()

	if wasBubble {
		a.emitBubble(BubbleNone, false)
	}

	if err := a.setQuestionVisibility(false); err != nil {
		// The fold above is NOT undone, exactly as HidePet leaves it: the panels
		// are painted against a pet that was going away, and putting a panel back
		// beside a visible pet would be a second, larger change than the failure.
		a.DebugLog("question re-hide: hide failed: " + err.Error())
		return
	}
	a.mu.Lock()
	a.hidden = true
	a.mu.Unlock()
	a.emitVisibility()
	a.DebugLog("question re-hide: the pet went back into hiding")
}

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
