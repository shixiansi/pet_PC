package main

import (
	"errors"
	"testing"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/store"
)

// The question reveal: hiding the pet must not turn a pomodoro prompt into a
// dead end.
//
// Refusing to hide WHILE a question waits (app_hotkey.go, errSummaryBlocksHiding
// / errPlanBlocksHiding) only covers one of the two orders. The other one - hide
// the pet first, then let a phase run out - used to draw the prompt into an
// SW_HIDEd window: the timer parked forever, no pixels explaining why, and the
// only way out being a shortcut the user has to remember. What is pinned here is
// the whole cycle, in both directions, plus the ways it must NOT fire.
//
// These are windowless unit tests. They drive the cycle through the same
// injected seam the full-screen probe uses (App.questionVisibility), because
// without it the reveal would be unreachable in this harness - and everything
// asserted below is the STATE machine around the native call, which is exactly
// the half that can be wrong without any pixel changing.

// visibilityRecorder captures what the reveal cycle asked the window to do.
type visibilityRecorder struct {
	calls    []bool // true = show, false = hide
	err      error  // returned for every call when non-nil
	hwndSeen desktop.Window
}

func (r *visibilityRecorder) record(hwnd desktop.Window, visible bool) error {
	r.hwndSeen = hwnd
	r.calls = append(r.calls, visible)
	return r.err
}

// shown reports whether the last thing the cycle asked for was a show.
func (r *visibilityRecorder) last() (bool, bool) {
	if len(r.calls) == 0 {
		return false, false
	}
	return r.calls[len(r.calls)-1], true
}

// revealApp builds an App whose window calls are recorded instead of performed,
// parked on the state a phase that has just run out leaves behind.
//
// The sound, the voice and the reminder scheduler are all switched off: no
// assertion here is about them, and leaving them on would make the tests depend
// on the machine's audio device and on a clock.
func revealApp(t *testing.T) (*App, *visibilityRecorder) {
	t.Helper()
	a, _ := newLLMTestAppWithEvents(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.AutoStartNextPhase = true
		c.FocusMinutes = 25
		c.ShortBreakMinutes = 5
		c.PomodoroSound = false
		// No VoiceEnabled line: the field is gone with the subsystem, and this
		// fixture only ever set it to false, which is now the only state there is.
		c.ReminderEnabled = false
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	rec := &visibilityRecorder{}
	a.questionVisibility = rec.record
	return a, rec
}

// hideSimulated hides the pet the way HidePet does, without a native window.
//
// The flag is what the rest of the app reads, so setting it directly is the same
// state a real "hide the pet" leaves behind - and it is the state every gate in
// this file branches on.
func hideSimulated(a *App) {
	a.mu.Lock()
	a.hidden = true
	a.mu.Unlock()
}

// secretShown reports whether the pet is on screen, i.e. the state the panel and
// the reminder gate actually read.
func secretShown(a *App) bool { return !a.petHidden() }

// parkedSummary is the state a focus phase that has just run out leaves behind,
// reached through the same engine calls the ticker uses.
func parkedSummary(t *testing.T, a *App) store.PomodoroState {
	t.Helper()
	cfg := a.store.Config()
	start := time.Now()
	st := startFocus(enabledIdle(), cfg, "t1", "写周报", start).State
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	a.completePhase(finishPhase(st, cfg, start.Add(25*time.Minute), start.Add(25*time.Minute), false), false)
	return a.store.Pomodoro()
}

// --- the reveal itself ------------------------------------------------------

// TestFocusEndRevealsAHiddenPet is the feature: a hidden pet comes back when the
// focus ends, because the summary prompt is what starts the rest.
func TestFocusEndRevealsAHiddenPet(t *testing.T) {
	a, rec := revealApp(t)
	hideSimulated(a)

	st := parkedSummary(t, a)
	if st.PendingRecordID == "" {
		t.Fatal("nothing was pending, so this test proves nothing")
	}
	if !secretShown(a) {
		t.Error("the pet stayed hidden with the summary prompt waiting, so the timer is parked invisibly")
	}
	last, any := rec.last()
	if !any || !last {
		t.Errorf("the reveal did not ask the window to show: %v", rec.calls)
	}
	// The prompt really is up, or "revealed" would be a claim about nothing.
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b == nil || b.Kind != BubbleSummary {
		t.Errorf("the summary prompt is not showing after the reveal: %+v", b)
	}
}

// TestBreakEndRevealsAHiddenPet is the same promise for the plan prompt, which
// gates the FOCUS - and which the panel cannot answer at all, so a hidden window
// there is a dead end with no entry point but the shortcut.
func TestBreakEndRevealsAHiddenPet(t *testing.T) {
	a, rec := revealApp(t)
	hideSimulated(a)

	cfg := a.store.Config()
	now := time.Now()
	st := expiredBreak(cfg, now)
	a.pomoMu.Lock()
	if err := a.store.SavePomodoro(st); err != nil {
		a.pomoMu.Unlock()
		t.Fatalf("SavePomodoro: %v", err)
	}
	a.completePhase(finishPhase(st, cfg, *st.EndsAt, now, false), false)
	a.pomoMu.Unlock()

	if !a.store.Pomodoro().PlanPending {
		t.Fatal("the plan question was not raised, so this test proves nothing")
	}
	if !secretShown(a) {
		t.Error("the pet stayed hidden with the plan prompt waiting")
	}
	if last, any := rec.last(); !any || !last {
		t.Errorf("the reveal did not ask the window to show: %v", rec.calls)
	}
}

// TestRevealedPetIsNotShownTwice: a pet that is already visible must not produce
// window calls. The reveal runs from every path that shows one of the gates, and
// a redundant ShowWindow would be a pointless window operation on every prompt.
func TestRevealedPetIsNotShownTwice(t *testing.T) {
	a, rec := revealApp(t)

	parkedSummary(t, a)
	if len(rec.calls) != 0 {
		t.Errorf("a visible pet produced window calls: %v", rec.calls)
	}
}

// TestRevealFailureLeavesThePetHidden: nothing changed on screen, so nothing may
// change in the state. Claiming the pet is visible after a failed ShowWindow
// would also make the show shortcut a no-op.
func TestRevealFailureLeavesThePetHidden(t *testing.T) {
	a, rec := revealApp(t)
	rec.err = errors.New("ShowWindow refused")
	hideSimulated(a)

	st := parkedSummary(t, a)

	if st.PendingRecordID == "" {
		t.Fatal("nothing was pending, so this test proves nothing")
	}
	if secretShown(a) {
		t.Error("a failed reveal reported the pet as visible")
	}
	if a.revealedForQuestion {
		t.Error("a failed reveal recorded a debt, so a later answer would hide a pet that was never shown")
	}
}

// --- the re-hide ------------------------------------------------------------

// TestAnsweringTheSummaryHidesThePetAgain is the other half of the promise: the
// reveal is temporary, and the answer is what ends it.
func TestAnsweringTheSummaryHidesThePetAgain(t *testing.T) {
	a, rec := revealApp(t)
	hideSimulated(a)
	st := parkedSummary(t, a)
	id := st.PendingRecordID

	if _, err := a.SavePomodoroSummary(id, "完成了数据部分"); err != nil {
		t.Fatalf("SavePomodoroSummary: %v", err)
	}

	if secretShown(a) {
		t.Error("the pet stayed on screen after the summary was answered")
	}
	if last, any := rec.last(); !any || last {
		t.Errorf("the re-hide did not ask the window to hide: %v", rec.calls)
	}
	// The break must have started: answering the prompt is what releases it, and
	// hiding the pet must not have eaten that.
	if !a.store.Pomodoro().IsRunning() {
		t.Errorf("the rest did not start after the summary was answered: %+v", a.store.Pomodoro())
	}
	// And nothing is left up: a bubble painted against a hidden pet is exactly
	// the state HidePet folds away.
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b != nil {
		t.Errorf("a bubble was left up on the re-hidden pet: %+v", b)
	}
}

// TestSkippingTheSummaryAlsoHides is the same path by the other button: the gate
// is on ANSWERING the prompt, not on typing into it.
func TestSkippingTheSummaryAlsoHides(t *testing.T) {
	a, _ := revealApp(t)
	hideSimulated(a)
	st := parkedSummary(t, a)

	if _, err := a.SavePomodoroSummary(st.PendingRecordID, ""); err != nil {
		t.Fatalf("SavePomodoroSummary: %v", err)
	}
	if secretShown(a) {
		t.Error("skipping the summary left the pet on screen")
	}
}

// TestAnsweringThePlanHidesThePetAgain covers the focus gate.
func TestAnsweringThePlanHidesThePetAgain(t *testing.T) {
	a, rec := revealApp(t)
	hideSimulated(a)

	st := a.store.Pomodoro()
	st.Enabled = true
	st.Phase = store.PhaseFocus
	st.RunState = store.RunStopped
	st.EndsAt = nil
	st.PlanPending = true
	st.NextTodoID, st.NextTodoTitle = "t1", "写周报"
	st.RemainingOnHalt = int(a.store.Config().PhaseDuration(store.PhaseFocus).Seconds())
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	a.showPlanBubble()
	if !secretShown(a) {
		t.Fatal("the plan prompt did not reveal a hidden pet, so this test proves nothing")
	}

	if _, err := a.StartPomodoroWithPlan("整理接口"); err != nil {
		t.Fatalf("StartPomodoroWithPlan: %v", err)
	}

	if secretShown(a) {
		t.Error("the pet stayed on screen after the plan was answered")
	}
	if last, any := rec.last(); !any || last {
		t.Errorf("the re-hide did not ask the window to hide: %v", rec.calls)
	}
	if !a.store.Pomodoro().IsRunning() {
		t.Errorf("the focus did not start: %+v", a.store.Pomodoro())
	}
}

// TestResumeAnswersThePlanAndHides: "继续" is one of the answers to the plan
// question, so it has to settle the debt too.
func TestResumeAnswersThePlanAndHides(t *testing.T) {
	a, _ := revealApp(t)
	hideSimulated(a)

	st := a.store.Pomodoro()
	st.Enabled = true
	st.Phase = store.PhaseFocus
	st.RunState = store.RunStopped
	st.PlanPending = true
	st.RemainingOnHalt = int(a.store.Config().PhaseDuration(store.PhaseFocus).Seconds())
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	a.showPlanBubble()

	if _, err := a.ResumePomodoro(); err != nil {
		t.Fatalf("ResumePomodoro: %v", err)
	}
	if secretShown(a) {
		t.Error("answering the plan with 继续 left the pet on screen")
	}
}

// TestGivingTheSessionUpHides covers the two escape hatches that clear the gates
// without answering them. Either one settles the debt: nothing is waiting any
// more, so there is no reason for the pet to be out.
func TestGivingTheSessionUpHides(t *testing.T) {
	t.Run("ResetPomodoro", func(t *testing.T) {
		a, _ := revealApp(t)
		hideSimulated(a)
		if st := parkedSummary(t, a); st.PendingRecordID == "" {
			t.Fatal("nothing was pending")
		}
		if !secretShown(a) {
			t.Fatal("the pet was not revealed")
		}
		if _, err := a.ResetPomodoro(); err != nil {
			t.Fatalf("ResetPomodoro: %v", err)
		}
		if secretShown(a) {
			t.Error("giving the session up left the pet on screen")
		}
	})

	t.Run("switching the pomodoro off", func(t *testing.T) {
		a, _ := revealApp(t)
		hideSimulated(a)
		if st := parkedSummary(t, a); st.PendingRecordID == "" {
			t.Fatal("nothing was pending")
		}
		if _, err := a.SetPomodoroEnabled(false); err != nil {
			t.Fatalf("SetPomodoroEnabled: %v", err)
		}
		if secretShown(a) {
			t.Error("switching the feature off left the pet on screen")
		}
	})
}

// --- the paths that must NOT hide -------------------------------------------

// TestAnExplicitShowIsNotUndoneIsTheUserPromise is the rule the whole design
// turns on. The user brought the pet back themselves, so answering a prompt -
// the very thing that had brought it back - must not put it away again.
//
// The route here is opening the panel, which is one of the documented ways back
// and the one a windowless harness can actually perform (it changes the state
// before it touches the window). The other route, the shortcut, is the same
// single rule applied in ShowPet; the gate script pins that it is there
// (check-frontend.cjs, "an explicit show is not undone by answering a question"),
// and the test below pins the other half of it - a show that FAILED must not
// cancel the debt.
func TestAnExplicitShowIsNotUndoneIsTheUserPromise(t *testing.T) {
	a, _ := revealApp(t)
	hideSimulated(a)
	st := parkedSummary(t, a)
	if !secretShown(a) {
		t.Fatal("the pet was not revealed")
	}

	// The user opens the panel: a deliberate "bring it back".
	if _, err := a.SetPanelOpen(true); err != nil {
		t.Logf("SetPanelOpen (no window): %v", err)
	}
	if _, err := a.SetPanelOpen(false); err != nil {
		t.Logf("SetPanelOpen (no window): %v", err)
	}
	a.mu.Lock()
	owed := a.revealedForQuestion
	a.mu.Unlock()
	if owed {
		t.Error("opening the panel left the question debt outstanding, so answering would hide the pet")
	}

	if _, err := a.SavePomodoroSummary(st.PendingRecordID, ""); err != nil {
		t.Fatalf("SavePomodoroSummary: %v", err)
	}
	if !secretShown(a) {
		t.Error("answering the prompt hid a pet the user had explicitly shown")
	}
}

// TestAFailedShowLeavesTheDebtStanding is the honesty rule for the shortcut
// route. `ShowPet` deliberately resolves the window BEFORE it changes any state,
// so a show that could not happen leaves the pet hidden - and therefore still
// owing a re-hide once the question is answered. Cancelling the debt on a failed
// call would leave a pet that is hidden with nothing that will ever hide it
// again, and, worse, would report the user's intent backwards.
func TestAFailedShowLeavesTheDebtStanding(t *testing.T) {
	a, _ := revealApp(t)
	hideSimulated(a)
	st := parkedSummary(t, a)
	if !secretShown(a) {
		t.Fatal("the pet was not revealed")
	}

	// No window exists in this harness, so this is a failure path by construction.
	if err := a.ShowPet(); err == nil {
		t.Fatal("ShowPet claimed to work without a window, so this test proves nothing")
	}
	if !secretShown(a) {
		t.Fatal("a failed show left the pet marked hidden while the reveal had it on screen")
	}
	a.mu.Lock()
	owed := a.revealedForQuestion
	a.mu.Unlock()
	if !owed {
		t.Error("a failed show cancelled the debt, so the pet would never be put away again")
	}

	if _, err := a.SavePomodoroSummary(st.PendingRecordID, ""); err != nil {
		t.Fatalf("SavePomodoroSummary: %v", err)
	}
	if secretShown(a) {
		t.Error("the pet stayed on screen after the question was answered")
	}
}

// TestHideAfterQuestionIsIdempotent: the answer paths can overlap (the plan gate
// is released by several callers), and a second re-hide must be a no-op rather
// than a second window call.
func TestHideAfterQuestionIsIdempotent(t *testing.T) {
	a, rec := revealApp(t)
	hideSimulated(a)
	st := parkedSummary(t, a)
	if _, err := a.SavePomodoroSummary(st.PendingRecordID, ""); err != nil {
		t.Fatalf("SavePomodoroSummary: %v", err)
	}
	before := len(rec.calls)

	a.hideAfterQuestion()
	a.hideAfterQuestion()

	if len(rec.calls) != before {
		t.Errorf("a settled debt produced more window calls: %v", rec.calls)
	}
	if secretShown(a) {
		t.Error("the pet came back on a repeated re-hide")
	}
}

// TestHidingAgainSuppressesTheOwedReHide: pressing the hide key after a reveal
// makes the pet hidden because the USER asked, so the answer to the prompt must
// not decide to show it. The state has to agree with the screen either way.
func TestHidingAgainSuppressesTheOwedReHide(t *testing.T) {
	a, _ := revealApp(t)
	hideSimulated(a)
	parkedSummary(t, a)
	if !secretShown(a) {
		t.Fatal("the pet was not revealed")
	}

	// The user hides it again. Without a window the native call cannot run, so
	// the state is set the way a successful call leaves it.
	a.mu.Lock()
	a.hidden = true
	a.revealedForQuestion = false
	a.mu.Unlock()

	a.hideAfterQuestion()
	if !a.petHidden() {
		t.Error("the re-hide un-hid a pet the user had just put away")
	}
}

// TestHidingWithNoWindowKeepsTheDebt: HidePet resolves the window before it
// changes any state, so without one it returns an error - and a real hide will
// still be owed. The failure path must not cancel the debt, which is exactly the
// bug that would leave a pet on screen forever.
func TestHidingWithNoWindowKeepsTheDebt(t *testing.T) {
	a, _ := revealApp(t)
	hideSimulated(a)
	parkedSummary(t, a)
	if !secretShown(a) {
		t.Fatal("the pet was not revealed")
	}

	if err := a.HidePet(); err == nil {
		t.Fatal("HidePet claimed to work without a window, so this test proves nothing")
	}
	if !secretShown(a) {
		t.Fatal("a failed hide left the pet marked hidden while it is on screen")
	}
	a.mu.Lock()
	owed := a.revealedForQuestion
	a.mu.Unlock()
	if !owed {
		t.Error("a failed hide cancelled the debt, so the pet would never be put away again")
	}
}

// TestAMissedReminderSurvivesTheReHide pins the interaction between the two
// features that both key off "the pet is hidden". A reminder that came due
// during the focus phase is owed when the rest begins - and the rest begins at
// the very moment the summary is answered, which is the moment the pet goes back
// into hiding. The delivery is suppressed (the pet is away), the flag stays set,
// and the reminder arrives on the next real opportunity instead of being thrown
// away or flashing into a vanishing window.
func TestAMissedReminderSurvivesTheReHide(t *testing.T) {
	a, _ := revealApp(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.ReminderEnabled = true
		c.RemindDuringBreak = true
		c.RemindWhenEmpty = true
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	hideSimulated(a)

	st := parkedSummary(t, a)
	// The focus phase suppressed a reminder: that is the real-world state this
	// test is about.
	a.remMu.Lock()
	a.remMissed = true
	a.remMu.Unlock()

	if _, err := a.SavePomodoroSummary(st.PendingRecordID, ""); err != nil {
		t.Fatalf("SavePomodoroSummary: %v", err)
	}

	if secretShown(a) {
		t.Fatal("the pet stayed on screen, so the reminder path under test was not exercised")
	}
	a.remMu.Lock()
	stillOwed := a.remMissed
	a.remMu.Unlock()
	if !stillOwed {
		t.Error("the owed reminder was consumed by a hide, so the user never gets it")
	}
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b != nil {
		t.Errorf("a bubble was left up on the re-hidden pet: %+v", b)
	}
}

// TestRemindersAreStillSuppressedWhileHiddenAfterAReveal pins the boundary of
// the feature: only the two QUESTIONS break through hiding. A reminder is a
// notification, and one arriving while the pet is away is still dropped.
func TestRemindersAreStillSuppressedWhileHiddenAfterAReveal(t *testing.T) {
	a, _ := revealApp(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.ReminderEnabled = true
		c.RemindWhenEmpty = true
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	hideSimulated(a)

	if a.showReminderBubble(true) {
		t.Error("a reminder was shown while the pet was hidden")
	}
}

// TestTheRevealDoesNotCountAsTheUserShowingThePet guards the state the two
// accessors report: while a question is up the pet IS visible (the panel says
// so, and the reminder gate reads it), but the debt is still owed.
func TestTheRevealDoesNotCountAsTheUserShowingThePet(t *testing.T) {
	a, _ := revealApp(t)
	hideSimulated(a)

	hidden, err := a.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if hidden.PetVisible {
		t.Error("the hidden pet reports petVisible=true, so the panel would lie about it")
	}

	parkedSummary(t, a)
	revealed, err := a.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if !revealed.PetVisible {
		t.Error("the revealed pet still reports petVisible=false, so the panel would claim it is hidden")
	}
	a.mu.Lock()
	owed := a.revealedForQuestion
	a.mu.Unlock()
	if !owed {
		t.Error("the reveal forgot that the pet is out only for the question")
	}
}
