package main

import (
	"testing"
	"time"

	"PetPC/internal/store"
)

// parkedAfterFocus runs one full focus phase and returns the state it leaves
// behind, which is the "waiting for a summary" state the gate is about.
func parkedAfterFocus(t *testing.T, tune func(*store.Config)) (*App, store.PomodoroState) {
	t.Helper()
	a, _ := newLLMTestAppWithEvents(t)

	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.AutoStartNextPhase = true
		c.FocusMinutes = 25
		c.ShortBreakMinutes = 5
		if tune != nil {
			tune(c)
		}
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	cfg := a.store.Config()
	start := time.Now()
	st := startFocus(enabledIdle(), cfg, "t1", "写周报", start).State
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}

	a.completePhase(finishPhase(st, cfg, start.Add(25*time.Minute), start.Add(25*time.Minute), false), false)
	return a, a.store.Pomodoro()
}

// TestFocusEndWaitsForItsRecord is the behaviour the user asked for: a finished
// focus phase does NOT start the rest until the record has been answered.
//
// The earlier design started the break behind the summary bubble, on the argument
// that the question should not block the timer. The user reversed that: a session
// nobody described should not bank a break, because the break is time the user
// never agreed to spend.
func TestFocusEndWaitsForItsRecord(t *testing.T) {
	a, st := parkedAfterFocus(t, nil)

	if st.Phase != store.PhaseShortBreak {
		t.Fatalf("phase = %q, want the break to be next", st.Phase)
	}
	if st.PendingRecordID == "" {
		t.Fatal("no record is pending, so nothing is gating the break")
	}
	if st.IsRunning() {
		t.Errorf("the break is already counting down: %+v", st)
	}
	if st.EndsAt != nil {
		t.Errorf("a waiting break must have no end time, got %v", st.EndsAt)
	}
	if got := Remaining(st, time.Now()); got != 5*time.Minute {
		t.Errorf("remaining = %v, want the full 5m banked", got)
	}
	// The prompt must be up, or the user has no way to release the gate.
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b == nil || b.Kind != BubbleSummary {
		t.Errorf("the summary prompt is not showing, so the gate cannot be opened")
	}
}

// TestAnsweringTheRecordStartsTheBreak is the other half: saving a summary starts
// the rest, from NOW.
//
// Starting from now rather than from when the focus ended is the point of the gate -
// the time spent writing the summary is the user's, and counting it as rest would
// hand them less rest than the setting promises.
func TestAnsweringTheRecordStartsTheBreak(t *testing.T) {
	a, st := parkedAfterFocus(t, nil)
	id := st.PendingRecordID

	before := time.Now()
	if _, err := a.SavePomodoroSummary(id, "写完了数据部分"); err != nil {
		t.Fatalf("SavePomodoroSummary: %v", err)
	}
	after := time.Now()

	got := a.store.Pomodoro()
	if got.PendingRecordID != "" {
		t.Errorf("the pending record was not cleared: %q", got.PendingRecordID)
	}
	if !got.IsRunning() {
		t.Fatalf("the break did not start when the record was answered: %+v", got)
	}
	if got.EndsAt == nil {
		t.Fatal("a running break must have an end time")
	}
	// Its end is a full break from the moment it was answered, within the window
	// this test took to run.
	lo, hi := before.Add(5*time.Minute), after.Add(5*time.Minute)
	if got.EndsAt.Before(lo) || got.EndsAt.After(hi) {
		t.Errorf("break endsAt = %v, want between %v and %v", got.EndsAt, lo, hi)
	}
	// The summary really was stored.
	recs := a.store.PomodoroRecords()
	if len(recs) != 1 || recs[0].Summary != "写完了数据部分" {
		t.Errorf("the summary was not saved: %+v", recs)
	}
}

// TestSkippingTheRecordAlsoStartsTheBreak pins that the gate is on ANSWERING the
// prompt, not on typing something into it. "Skip" stores an empty summary and must
// release the break exactly like "save" does.
func TestSkippingTheRecordAlsoStartsTheBreak(t *testing.T) {
	a, st := parkedAfterFocus(t, nil)

	if _, err := a.SavePomodoroSummary(st.PendingRecordID, ""); err != nil {
		t.Fatalf("SavePomodoroSummary: %v", err)
	}
	got := a.store.Pomodoro()
	if !got.IsRunning() {
		t.Fatalf("skipping did not start the break: %+v", got)
	}
	recs := a.store.PomodoroRecords()
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1 (a skip still records the session)", len(recs))
	}
	if recs[0].Summary != "" {
		t.Errorf("a skip stored a summary: %q", recs[0].Summary)
	}
}

// TestSummaryPromptCannotBeDismissed is the property that makes the gate real.
//
// The prompt is the gate on the rest, so it must not be possible to wave it away:
// if Escape, a drag or a stray click closed it, the timer would either stay parked
// forever with nothing on screen explaining why, or the break would start without
// the user ever choosing to skip. Answering it is one click, and both buttons
// resolve it.
func TestSummaryPromptCannotBeDismissed(t *testing.T) {
	a, st := parkedAfterFocus(t, nil)
	if st.PendingRecordID == "" {
		t.Fatal("nothing was pending")
	}

	// DismissBubble finishes by re-cropping the native window, which needs a real
	// window; the guard runs before that, so a panic-free call is what is asserted
	// here and the STATE is what proves the refusal.
	_, _ = a.DismissBubble()

	got := a.store.Pomodoro()
	if got.PendingRecordID == "" {
		t.Error("the dismissal answered the record, so the gate can be bypassed")
	}
	if got.IsRunning() {
		t.Error("the dismissal started the break")
	}
	// And the prompt is still on screen, because a refusal that hides the question
	// would be the worst of both worlds.
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b == nil || b.Kind != BubbleSummary {
		t.Errorf("the prompt disappeared without being answered")
	}
}

// TestSummaryPromptEvictsAnOpenChatPopup is the reverse direction, and the reason
// the guard on OpenLive is enough: a question that appears while the user is
// already talking still wins the slot.
//
// It has to, because the alternative is the parked timer with no visible prompt
// that OpenLive's guard exists to prevent. The overlay is a view of a
// conversation that keeps arriving and is still recorded, so folding it away
// costs the user nothing they cannot get back by double-clicking again.
func TestSummaryPromptEvictsAnOpenChatPopup(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)

	// The conversation is up, then a focus phase ends behind it.
	_, _ = a.OpenLive()
	if !a.liveIsOpen() {
		t.Fatal("the popup did not open, so this test cannot show it being evicted")
	}

	_, st := parkedAfterFocus(t, nil)
	if st.PendingRecordID == "" {
		t.Fatal("nothing was pending")
	}
	// parkedAfterFocus builds its own App; drive this one into the same state.
	a.pomoMu.Lock()
	if err := a.store.SavePomodoro(st); err != nil {
		a.pomoMu.Unlock()
		t.Fatalf("SavePomodoro: %v", err)
	}
	a.pomoMu.Unlock()
	a.showSummaryBubble()

	if a.liveIsOpen() {
		t.Error("the popup is still up beside the summary prompt")
	}
	a.mu.Lock()
	b := a.bubble
	advertised := a.layout.Bubble
	live := a.layout.Live
	a.mu.Unlock()
	if b == nil || b.Kind != BubbleSummary {
		t.Fatalf("the prompt did not take the slot: %+v", b)
	}
	if advertised == nil || advertised.Kind != BubbleSummary {
		t.Errorf("the layout does not advertise the prompt: %+v", advertised)
	}
	// The layout must not still describe an overlay the page no longer draws: the
	// region is cut from it, so a stale Live leaves a cropped area with nothing in
	// it (the pale band).
	if live != nil {
		t.Errorf("the layout still advertises the popup: %+v", live)
	}
}

// TestChatPopupCannotEatTheSummaryPrompt is the answer to "the chat popup pushes
// the pomodoro summary popup out of the way".
//
// The overlay and the speech bubble share one slot, and OpenLive TAKES the slot
// rather than sharing it - so before this guard, a double click on the pet while
// the summary prompt was up deleted the question. For the summary that is not a
// cosmetic loss: the prompt is the gate on the break, so the timer stayed parked
// with nothing on screen saying why, and the only way back was to discover that
// the panel's records tab can answer it.
func TestChatPopupCannotEatTheSummaryPrompt(t *testing.T) {
	a, st := parkedAfterFocus(t, nil)
	id := st.PendingRecordID
	if id == "" {
		t.Fatal("nothing was pending")
	}

	// The prompt is up, so the overlay must refuse to open - and it must say so.
	// The refusal short-circuits before any geometry is touched, so the error is
	// the explanation itself rather than this harness's missing window.
	_, err := a.OpenLive()
	if err == nil {
		t.Fatal("the chat popup opened over the summary prompt and destroyed it")
	}
	if err.Error() != questionOwnsTheSlot(BubbleSummary) {
		t.Errorf("the refusal does not explain itself: %v", err)
	}
	if a.liveIsOpen() {
		t.Error("the overlay is showing over a question that is waiting for an answer")
	}
	// The refusal must leave the question exactly where it was: refusing to open
	// the overlay is only correct if the prompt is still up afterwards.
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b == nil || b.Kind != BubbleSummary {
		t.Fatalf("the refusal took the prompt with it: %+v", b)
	}
	if b.RecordID != id {
		t.Errorf("the prompt now edits %q, want %q", b.RecordID, id)
	}
	// And the timer is still parked, so nothing was answered behind the user's back.
	if got := a.store.Pomodoro(); got.PendingRecordID != id || got.IsRunning() {
		t.Errorf("the refusal disturbed the parked phase: %+v", got)
	}
}

// TestChatPopupCannotEatTheTodoPicker is the other unanswered question. The picker
// is not a gate on the timer, but displacing it silently loses the user's
// half-made choice and the picker is the only way to start a focus phase.
func TestChatPopupCannotEatTheTodoPicker(t *testing.T) {
	a := newLLMTestApp(t)

	a.setBubbleLocked(&BubbleView{Kind: BubblePick, Title: "这次要专注做哪件事？"})

	if _, err := a.OpenLive(); err == nil {
		t.Error("the chat popup opened over the todo picker")
	}
	if a.liveIsOpen() {
		t.Error("the overlay is showing over the picker")
	}
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b == nil || b.Kind != BubblePick {
		t.Errorf("the refusal took the picker with it: %+v", b)
	}
}

// TestChatPopupStillTakesTheSlotFromANotification is the other half: making the
// popup defer to a QUESTION must not make it defer to everything. A reminder asks
// nothing, and the panel is the permanent record rather than a question, so both
// are still replaced by the overlay.
func TestChatPopupStillTakesTheSlotFromANotification(t *testing.T) {
	a := newLLMTestApp(t)

	a.setBubbleLocked(&BubbleView{Kind: BubbleReminder, Title: "今天要做的事"})
	// The geometry call needs a real window and fails here, which is why the
	// assertion is the STATE: the guard is what this test is about, and a run that
	// reached the re-crop has already passed it.
	_, _ = a.OpenLive()
	if !a.liveIsOpen() {
		t.Error("the overlay did not open, so a notification can block the conversation")
	}
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b != nil {
		t.Errorf("the reminder survived the overlay opening: %+v", b)
	}

	// The panel is not a question either: the overlay takes the slot from it.
	_, _ = a.CloseLive()
	if _, err := a.SetPanelOpen(true); err != nil {
		t.Logf("SetPanelOpen (no window): %v", err)
	}
	_, _ = a.OpenLive()
	a.mu.Lock()
	panelOpen := a.panelOpen
	a.mu.Unlock()
	if panelOpen {
		t.Error("the panel stayed open under the overlay")
	}
}

// TestUnansweredQuestionKindNamesTheRightBubble pins the mapping the refusal
// message and the frontend both depend on. Getting it wrong is not silent: the
// message would tell the user to write a summary while the picker is up.
func TestUnansweredQuestionKindNamesTheRightBubble(t *testing.T) {
	a := newLLMTestApp(t)

	if got := a.unansweredQuestionKind(); got != BubbleNone {
		t.Errorf("a fresh app reports %q", got)
	}
	if a.bubbleAwaitingAnswer() {
		t.Error("a fresh app claims a question is waiting")
	}

	for _, kind := range []string{BubblePick, BubbleSummary} {
		a.setBubbleLocked(&BubbleView{Kind: kind})
		if got := a.unansweredQuestionKind(); got != kind {
			t.Errorf("unansweredQuestionKind = %q, want %q", got, kind)
		}
		if !a.bubbleAwaitingAnswer() {
			t.Errorf("%s is not treated as a question", kind)
		}
	}

	// The two that ask nothing.
	for _, kind := range []string{BubbleReminder, BubbleNone} {
		var b *BubbleView
		if kind != BubbleNone {
			b = &BubbleView{Kind: kind}
		}
		a.setBubbleLocked(b)
		if got := a.unansweredQuestionKind(); got != BubbleNone {
			t.Errorf("%s reported as a question: %q", kind, got)
		}
	}
	// And the messages differ, because "choose a todo" and "write the summary" are
	// not the same instruction.
	if questionOwnsTheSlot(BubblePick) == questionOwnsTheSlot(BubbleSummary) {
		t.Error("the picker and the summary share one refusal message")
	}
	for _, kind := range []string{BubblePick, BubbleSummary} {
		if questionOwnsTheSlot(kind) == "" {
			t.Errorf("%s has no explanation for the refusal", kind)
		}
	}
}

// TestOtherBubblesAreStillDismissable is the other half: making the summary prompt
// stick must not make EVERY bubble stick. A reminder asks nothing, so it must still
// close on demand (and still auto-hides).
func TestOtherBubblesAreStillDismissable(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.VoiceEnabled = false
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	a.setBubbleLocked(&BubbleView{Kind: BubbleReminder, Title: "今天要做的事"})
	a.mu.Lock()
	before := a.bubble
	a.mu.Unlock()
	if before == nil {
		t.Fatal("the reminder bubble was not set")
	}

	// The STATE is the assertion; the geometry call needs a window.
	_, _ = a.DismissBubble()

	a.mu.Lock()
	after := a.bubble
	a.mu.Unlock()
	if after != nil {
		t.Errorf("a reminder bubble could not be dismissed: %+v", after)
	}
}

// TestAnsweringAStaleRecordDoesNotRestartARunningBreak covers the guard on the
// answer path: a summary saved for a record that is no longer pending - an old
// record edited from the panel - must not restart a break already in progress.
func TestAnsweringAStaleRecordDoesNotRestartARunningBreak(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)

	// A break mid-flight, with nothing pending.
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.ShortBreakMinutes = 5
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	end := time.Now().Add(2 * time.Minute)
	started := time.Now()
	if err := a.store.SavePomodoro(store.PomodoroState{
		Enabled: true, Phase: store.PhaseShortBreak, RunState: store.RunRunning,
		EndsAt: &end, PhaseStartedAt: &started,
	}); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}

	// An old record, then a summary saved against it.
	rec, err := a.store.AddPomodoroRecord(store.PomodoroRecord{
		StartedAt: time.Now().Add(-time.Hour), EndedAt: time.Now().Add(-30 * time.Minute),
		FocusMinutes: 25,
	})
	if err != nil {
		t.Fatalf("AddPomodoroRecord: %v", err)
	}

	if _, err := a.SavePomodoroSummary(rec.ID, "补写的总结"); err != nil {
		t.Fatalf("SavePomodoroSummary: %v", err)
	}

	got := a.store.Pomodoro()
	if got.EndsAt == nil || !got.EndsAt.Equal(end) {
		t.Errorf("the running break was restarted: endsAt %v, want %v", got.EndsAt, end)
	}
	if recs := a.store.PomodoroRecords(); len(recs) != 1 || recs[0].Summary != "补写的总结" {
		t.Errorf("the summary was not stored: %+v", recs)
	}
}

// TestParkedRecordIsReOfferedAfterRestart covers the self-healing property. A phase
// parked on an unanswered record survives on disk, but the bubble does not - so
// without re-asking on startup, the gate would become a dead end after any restart.
func TestParkedRecordIsReOfferedAfterRestart(t *testing.T) {
	a, st := parkedAfterFocus(t, nil)
	if st.PendingRecordID == "" {
		t.Fatal("nothing was pending")
	}

	// A fresh App over the same store, as a relaunch would be.
	restarted := &App{store: a.store}
	restarted.ctx = a.ctx
	restarted.emitFn = a.emitFn

	restarted.initPomodoro()

	restarted.mu.Lock()
	b := restarted.bubble
	restarted.mu.Unlock()
	if b == nil || b.Kind != BubbleSummary {
		t.Errorf("the pending prompt was not re-offered after a restart, so the gate is a dead end")
	}
	// And it must NOT have auto-answered: the record is still waiting.
	if got := a.store.Pomodoro(); got.PendingRecordID == "" {
		t.Error("the restart answered the record on the user's behalf")
	}
}

// TestParkedPhaseSurvivesRestartWithoutRunning covers the storage contract: the
// parked state must round-trip through disk without turning into a running phase.
func TestParkedPhaseSurvivesRestartWithoutRunning(t *testing.T) {
	a, st := parkedAfterFocus(t, nil)
	if st.PendingRecordID == "" {
		t.Fatal("nothing was pending")
	}

	// Reload from disk through the store's own normaliser.
	reloaded := store.NormaliseState(st)
	if reloaded.IsRunning() {
		t.Errorf("a parked phase came back running: %+v", reloaded)
	}
	if reloaded.RunState != store.RunStopped {
		t.Errorf("runState = %q, want stopped", reloaded.RunState)
	}
	if reloaded.PendingRecordID != st.PendingRecordID {
		t.Errorf("the pending record was lost: %q", reloaded.PendingRecordID)
	}
	_ = a
}
