package main

import (
	"errors"
	"testing"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/store"
)

// The two ways into a focus session, the self-healing paths that keep the plan
// question from becoming a dead end, and the cleanup when the user gives up.
//
// The gate itself (a break running out raises the question, the plan is consumed
// by the record) is pinned in pomodoro_plan_gate_test.go; what is pinned here is
// everything that happens AROUND it: the picker, "continue", hiding the pet,
// closing the panel, a restart, and the three explicit ways out.

// --- helpers ---------------------------------------------------------------

// gatedApp builds an App whose timer is parked on the plan question, which is
// the state every path in this file is about.
//
// The callers pass the association explicitly because the two paths fill
// NextTodo* from different sources: a finished focus carries its own todo, and
// the picker writes the user's choice.
func gatedApp(t *testing.T, todoID, todoTitle string, tune func(*store.Config)) *App {
	t.Helper()
	a := newLLMTestApp(t)
	// A windowless harness cannot answer "is something full screen?", and the
	// break-end branch always asks: a constant false keeps this file about the
	// gate rather than about the full-screen path (which pomodoro_plan_gate_test.go
	// drives from both states).
	a.foregroundFullscreen = func(desktop.Window) bool { return false }

	if tune != nil {
		if _, err := a.store.UpdateConfig(tune); err != nil {
			t.Fatalf("UpdateConfig: %v", err)
		}
	}

	st := a.store.Pomodoro()
	st.Enabled = true
	st.Phase = store.PhaseFocus
	st.RunState = store.RunStopped
	st.CurrentTodoID, st.CurrentTodoTitle = "", ""
	st.NextTodoID, st.NextTodoTitle = todoID, todoTitle
	st.PlanPending = true
	st.RemainingOnHalt = int(a.store.Config().PhaseDuration(store.PhaseFocus).Seconds())
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	return a
}

// setPlanInFlight parks a plan on the App the way StartPomodoroWithPlan does,
// so the cleanup tests can prove the field was actually cleared rather than
// never set.
func setPlanInFlight(t *testing.T, a *App, plan string) {
	t.Helper()
	st := a.store.Pomodoro()
	st.PlanPending = true
	st.Phase = store.PhaseFocus
	st.RunState = store.RunStopped
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	if _, err := a.StartPomodoroWithPlan(plan); err != nil {
		t.Fatalf("StartPomodoroWithPlan: %v", err)
	}
	if a.focusPlan != plan {
		t.Fatalf("the plan was not parked in memory: %q", a.focusPlan)
	}
}

// --- T13: the manual path ------------------------------------------------

// TestPickerAsksWhichThingInTheUsersWords pins the picker's title change: the
// question is about what to work ON, and the plan question that follows asks
// what the user sets out to DO, so the two must not read as the same prompt.
func TestPickerAsksWhichThingInTheUsersWords(t *testing.T) {
	a := newLLMTestApp(t)

	// There is no native window here, so the returned layout is empty and the
	// assertions are on the STATE - the same convention the existing bubble tests
	// use. The bubble field and the layout field are kept in step by
	// setBubbleLocked, so checking both is what proves the frontend would draw it.
	if _, err := a.ShowPomodoroPicker(); err != nil {
		t.Logf("ShowPomodoroPicker (no window): %v", err)
	}
	b, advertised := a.bubbleState()
	if b == nil {
		t.Fatal("the picker did not take the slot")
	}
	if b.Kind != BubblePick {
		t.Errorf("kind = %q, want %q", b.Kind, BubblePick)
	}
	if b.Title != "想专注做哪件事？" {
		t.Errorf("picker title = %q, want 想专注做哪件事？", b.Title)
	}
	if advertised == nil || advertised.Kind != BubblePick {
		t.Errorf("the layout does not advertise the picker: %+v", advertised)
	}
}

// TestConfirmingThePickerParksOnThePlanQuestion is the whole point of the manual
// path: confirming a todo no longer starts the clock. The session is parked with
// the chosen todo carried forward, and the plan question is what starts it.
func TestConfirmingThePickerParksOnThePlanQuestion(t *testing.T) {
	a := newLLMTestApp(t)
	now := time.Now()
	if err := a.store.SaveTodos([]store.Todo{
		{ID: "t1", Title: "写周报", CreatedAt: now},
	}); err != nil {
		t.Fatalf("SaveTodos: %v", err)
	}

	if _, err := a.StartPomodoro(PomodoroStartInput{TodoID: "t1"}); err != nil {
		t.Fatalf("StartPomodoro: %v", err)
	}

	st := a.store.Pomodoro()
	if !st.PlanPending {
		t.Fatal("the focus started without asking what it is about")
	}
	if st.RunState == store.RunRunning || st.EndsAt != nil {
		t.Errorf("the clock is already running: run=%q endsAt=%v", st.RunState, st.EndsAt)
	}
	if st.NextTodoID != "t1" {
		t.Errorf("nextTodoId = %q, want t1 (the choice must be carried)", st.NextTodoID)
	}
	if st.NextTodoTitle != "写周报" {
		t.Errorf("nextTodoTitle = %q, want 写周报", st.NextTodoTitle)
	}
	if st.CurrentTodoID != "" {
		t.Errorf("the association was moved in early: %q", st.CurrentTodoID)
	}

	a.mu.Lock()
	b := a.bubble
	advertised := a.layout.Bubble
	a.mu.Unlock()
	if b == nil || b.Kind != BubblePlan {
		t.Fatalf("the plan question is not showing: %+v", b)
	}
	if advertised == nil || advertised.Kind != BubblePlan {
		t.Errorf("the layout does not advertise the plan question: %+v", advertised)
	}
	if b.TodoTitle != "写周报" {
		t.Errorf("the question does not name the chosen todo: %q", b.TodoTitle)
	}

	// Choosing a todo is not doing it: it must still be open afterwards.
	todos := a.store.Todos()
	if len(todos) != 1 || todos[0].Done {
		t.Errorf("selecting the todo completed it: %+v", todos)
	}
}

// TestPickerOnlyLetsYouSkipTheTodoWhenThereIsNone keeps the documented rule:
// "unattached" is the fallback for an empty list, not a general escape from
// choosing.
func TestPickerOnlyLetsYouSkipTheTodoWhenThereIsNone(t *testing.T) {
	a := newLLMTestApp(t)
	if err := a.store.SaveTodos([]store.Todo{
		{ID: "t1", Title: "写周报", CreatedAt: time.Now()},
	}); err != nil {
		t.Fatalf("SaveTodos: %v", err)
	}
	layout, err := a.ShowPomodoroPicker()
	if err != nil {
		t.Logf("ShowPomodoroPicker (no window): %v", err)
	}
	_ = layout
	b, _ := a.bubbleState()
	if b == nil {
		t.Fatal("the picker did not take the slot")
	}
	if b.AllowUnattached {
		t.Error("an empty choice was offered while there were todos to pick from")
	}

	// And with nothing open it is the only way the feature stays usable.
	if err := a.store.SaveTodos(nil); err != nil {
		t.Fatalf("SaveTodos: %v", err)
	}
	if _, err := a.ShowPomodoroPicker(); err != nil {
		t.Logf("ShowPomodoroPicker (no window): %v", err)
	}
	b, _ = a.bubbleState()
	if b == nil || !b.AllowUnattached {
		t.Errorf("with nothing open there is no way to start: %+v", b)
	}
}

// --- T14: the resume bypass ----------------------------------------------

// TestStartingWithNoTodoCannotSneakPastTheGate is the bypass C3 found: after a
// break ends the parked state has Phase=focus, RunStopped, CurrentTodoID="" and
// the full focus length banked - which is EXACTLY the shape the "continue the
// session" shortcut looks for when the user picks "no todo". Without the guard
// the click would start the clock and the plan question would never be answered.
func TestStartingWithNoTodoCannotSneakPastTheGate(t *testing.T) {
	a := gatedApp(t, "", "", nil)

	if _, err := a.StartPomodoro(PomodoroStartInput{TodoID: ""}); err != nil {
		t.Fatalf("StartPomodoro: %v", err)
	}

	st := a.store.Pomodoro()
	if !st.PlanPending {
		t.Error("the resume shortcut answered the plan question for the user")
	}
	if st.RunState == store.RunRunning || st.EndsAt != nil {
		t.Errorf("the clock started behind the question: run=%q endsAt=%v", st.RunState, st.EndsAt)
	}
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b == nil || b.Kind != BubblePlan {
		t.Errorf("the question was taken off screen: %+v", b)
	}
}

// TestResumeAnswersTheQuestionRatherThanSkippingIt: "continue" is not an escape
// hatch from a question. It means "answer it with no plan", which starts the
// session the user came back for instead of leaving them parked forever.
func TestResumeAnswersTheQuestionRatherThanSkippingIt(t *testing.T) {
	a := gatedApp(t, "t1", "写周报", nil)

	if _, err := a.ResumePomodoro(); err != nil {
		t.Fatalf("ResumePomodoro: %v", err)
	}

	st := a.store.Pomodoro()
	if st.PlanPending {
		t.Error("continue left the plan question pending")
	}
	if st.RunState != store.RunRunning || st.EndsAt == nil {
		t.Errorf("continue did not start the focus: run=%q endsAt=%v", st.RunState, st.EndsAt)
	}
	if a.focusPlan != "" {
		t.Errorf("the plan should be empty when the question was skipped, got %q", a.focusPlan)
	}
	if st.CurrentTodoID != "t1" || st.CurrentTodoTitle != "写周报" {
		t.Errorf("the carried todo was lost: %+v", st)
	}
}

// TestContinueStillWorksWhenNothingIsBeingAsked is the other half: the guard
// added above must not change what "continue" means for a session the user
// stopped by hand (docs 13.1).
func TestContinueStillWorksWhenNothingIsBeingAsked(t *testing.T) {
	a := newLLMTestApp(t)
	now := time.Now()
	if err := a.store.SaveTodos([]store.Todo{
		{ID: "t1", Title: "写周报", CreatedAt: now},
	}); err != nil {
		t.Fatalf("SaveTodos: %v", err)
	}
	// A session the user stopped by hand: no question, half the focus banked.
	st := store.PomodoroState{
		Enabled: true, Phase: store.PhaseFocus, RunState: store.RunStopped,
		CurrentTodoID: "t1", CurrentTodoTitle: "写周报",
		RemainingOnHalt: int((12 * time.Minute).Seconds()),
	}
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}

	if _, err := a.StartPomodoro(PomodoroStartInput{TodoID: "t1"}); err != nil {
		t.Fatalf("StartPomodoro: %v", err)
	}
	got := a.store.Pomodoro()
	if got.CurrentTodoID != "t1" || got.RunState != store.RunRunning {
		t.Errorf("the stopped session was not continued: %+v", got)
	}
	if got.PlanPending {
		t.Error("continuing a hand-stopped session raised a plan question")
	}
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b != nil {
		t.Errorf("continuing a hand-stopped session produced a bubble: %+v", b)
	}
}

// --- T17: editing both lines at once -------------------------------------

// TestEditPomodoroRecordWritesBothLines is the new binding the records page
// needs: the plan and the actual result are edited together, in one write, so
// the page cannot half-save an edit.
func TestEditPomodoroRecordWritesBothLines(t *testing.T) {
	a := newLLMTestApp(t)
	rec, err := a.store.AddPomodoroRecord(store.PomodoroRecord{
		TodoID: "t1", TodoTitle: "写周报", Plan: "整理接口",
		StartedAt: time.Now().Add(-25 * time.Minute), EndedAt: time.Now(), FocusMinutes: 25,
	})
	if err != nil {
		t.Fatalf("AddPomodoroRecord: %v", err)
	}

	next, err := a.EditPomodoroRecord(rec.ID, "新计划", "新实际")
	if err != nil {
		t.Fatalf("EditPomodoroRecord: %v", err)
	}
	if len(next.Records) != 1 {
		t.Fatalf("the edit changed the number of records: %d", len(next.Records))
	}
	got := next.Records[0]
	if got.Plan != "新计划" || got.Summary != "新实际" {
		t.Errorf("the edit did not land: plan=%q summary=%q", got.Plan, got.Summary)
	}
	if got.PlanEditedAt == nil || got.EditedAt == nil {
		t.Errorf("the edit was not stamped: planEditedAt=%v editedAt=%v", got.PlanEditedAt, got.EditedAt)
	}
	// The store's own copy agrees with the returned snapshot.
	if stored := a.store.PomodoroRecords(); len(stored) != 1 || stored[0].Plan != "新计划" {
		t.Errorf("the store disagrees with the returned state: %+v", stored)
	}
}

// TestEditPomodoroRecordRejectsAnUnknownRecord keeps the edit contract honest:
// a bad id is an error, never a silent success.
func TestEditPomodoroRecordRejectsAnUnknownRecord(t *testing.T) {
	a := newLLMTestApp(t)

	next, err := a.EditPomodoroRecord("does-not-exist", "a", "b")
	if err == nil {
		t.Fatal("editing a missing record should fail")
	}
	if next.Config.FocusMinutes != 0 || len(next.Records) != 0 {
		t.Errorf("a failed edit returned a populated state: %+v", next)
	}
}

// TestSavingASummaryLeavesThePlanAlone: the summary bubble still calls the
// original binding, and it must not disturb the plan the user wrote up front -
// that pairing is the whole point of showing the plan beside the result.
func TestSavingASummaryLeavesThePlanAlone(t *testing.T) {
	a := newLLMTestApp(t)
	rec, err := a.store.AddPomodoroRecord(store.PomodoroRecord{
		Plan: "整理接口", StartedAt: time.Now().Add(-time.Minute), EndedAt: time.Now(), FocusMinutes: 25,
	})
	if err != nil {
		t.Fatalf("AddPomodoroRecord: %v", err)
	}

	next, err := a.SavePomodoroSummary(rec.ID, "只改实际")
	if err != nil {
		t.Fatalf("SavePomodoroSummary: %v", err)
	}
	if len(next.Records) != 1 {
		t.Fatalf("the summary edit changed the number of records: %d", len(next.Records))
	}
	if got := next.Records[0]; got.Plan != "整理接口" || got.Summary != "只改实际" {
		t.Errorf("saving a summary disturbed the plan: plan=%q summary=%q", got.Plan, got.Summary)
	}
}

// --- T23: the panel fold must not swallow the question -------------------

// TestClosingThePanelBringsTheQuestionBack is the self-healing path that makes
// the gate survivable. The panel and the bubble share one slot, so opening the
// panel folds the question away - and unlike the summary, the records tab has no
// way to answer a plan question, so losing it would park the timer with nothing
// on screen explaining why.
func TestClosingThePanelBringsTheQuestionBack(t *testing.T) {
	a := gatedApp(t, "t1", "写周报", nil)

	// Open the panel: the question is folded away, exactly as before.
	if _, err := a.SetPanelOpen(true); err != nil {
		t.Logf("SetPanelOpen(true) (no window): %v", err)
	}
	a.mu.Lock()
	folded := a.bubble
	open := a.panelOpen
	a.mu.Unlock()
	if folded != nil {
		t.Fatalf("opening the panel left the bubble up: %+v", folded)
	}
	if !open {
		t.Fatal("the panel did not open, so this test cannot show the fold")
	}

	// Close it: the unanswered question comes back with it.
	if _, err := a.SetPanelOpen(false); err != nil {
		t.Logf("SetPanelOpen(false) (no window): %v", err)
	}
	a.mu.Lock()
	b := a.bubble
	advertised := a.layout.Bubble
	stillOpen := a.panelOpen
	a.mu.Unlock()
	if stillOpen {
		t.Error("the panel is still open")
	}
	if b == nil || b.Kind != BubblePlan {
		t.Fatalf("the question was not brought back: %+v", b)
	}
	if advertised == nil || advertised.Kind != BubblePlan {
		t.Errorf("the layout in step with the bubble does not carry it: %+v", advertised)
	}
}

// TestClosingThePanelWithoutAQuestionProducesNothing: the re-raise is gated on
// the question actually being unanswered, so it cannot invent a bubble.
func TestClosingThePanelWithoutAQuestionProducesNothing(t *testing.T) {
	a := newLLMTestApp(t)
	if _, err := a.SetPanelOpen(true); err != nil {
		t.Logf("SetPanelOpen(true) (no window): %v", err)
	}
	if _, err := a.SetPanelOpen(false); err != nil {
		t.Logf("SetPanelOpen(false) (no window): %v", err)
	}
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b != nil {
		t.Errorf("closing the panel invented a bubble: %+v", b)
	}
}

// --- T24: a restart must not strand the timer ---------------------------

// TestThePlanQuestionComesBackAfterARestart is the restart half of the
// self-healing: the pending flag survives on disk, the bubble does not, so a
// launch that did not re-ask would turn every restart into a dead end.
func TestThePlanQuestionComesBackAfterARestart(t *testing.T) {
	a := gatedApp(t, "t1", "写周报", nil)

	// A fresh App over the same store, as a relaunch would be.
	restarted := &App{store: a.store}
	restarted.ctx = a.ctx
	restarted.emitFn = a.emitFn
	restarted.foregroundFullscreen = func(desktop.Window) bool { return false }

	restarted.initPomodoro()

	restarted.mu.Lock()
	b := restarted.bubble
	restarted.mu.Unlock()
	if b == nil || b.Kind != BubblePlan {
		t.Fatalf("the question was not re-offered after a restart: %+v", b)
	}
	st := a.store.Pomodoro()
	if !st.PlanPending {
		t.Error("the restart answered the question on the user's behalf")
	}
	if st.RunState == store.RunRunning {
		t.Errorf("the restart started the focus: %+v", st)
	}
}

// --- T25: hiding the pet must not strand the timer -----------------------

// TestHidingIsRefusedWhileThePlanIsWaiting: the bubble is painted inside the
// window, so hiding the pet would take the only way to answer the question off
// screen and park the timer for good.
func TestHidingIsRefusedWhileThePlanIsWaiting(t *testing.T) {
	a := gatedApp(t, "t1", "写周报", nil)
	if _, err := a.showBubble(&BubbleView{Kind: BubblePlan, Title: "接下来想专注做什么？"}); err != nil {
		t.Logf("showBubble (no window): %v", err)
	}

	err := a.HidePet()
	if !errors.Is(err, errPlanBlocksHiding) {
		t.Fatalf("HidePet returned %v, want errPlanBlocksHiding", err)
	}
	a.mu.Lock()
	hidden := a.hidden
	b := a.bubble
	a.mu.Unlock()
	if hidden {
		t.Error("the pet was hidden despite the waiting question")
	}
	if b == nil || b.Kind != BubblePlan {
		t.Errorf("the refusal took the question with it: %+v", b)
	}
	if got := a.store.Pomodoro(); !got.PlanPending {
		t.Error("the refusal disturbed the gate")
	}
}

// TestHidingIsNotBlockedByThePlanQuestionWhenThereIsNone keeps the refusal
// specific: only a real waiting question blocks hiding.
func TestHidingIsNotBlockedByThePlanQuestionWhenThereIsNone(t *testing.T) {
	a := newLLMTestApp(t)
	if err := a.HidePet(); err != nil {
		if errors.Is(err, errPlanBlocksHiding) {
			t.Fatal("hiding was refused with no plan question waiting")
		}
		// A missing window is the expected failure in this harness.
		t.Logf("HidePet (no window): %v", err)
	}
}

// --- T26: the three explicit ways out -----------------------------------

// TestSkippingClearsTheGateAndTheParkedPlan: skipping is the user saying "not
// this, move on", so the question and the plan in flight go with it. Leaving the
// plan behind would write one session's intention into the NEXT session's
// permanent record.
func TestSkippingClearsTheGateAndTheParkedPlan(t *testing.T) {
	a := newLLMTestApp(t)
	setPlanInFlight(t, a, "整理接口")

	// Park a rest with the gate up, which is the state skip is reached from.
	st := a.store.Pomodoro()
	st.Phase = store.PhaseShortBreak
	st.RunState = store.RunRunning
	end := time.Now().Add(2 * time.Minute)
	st.EndsAt = &end
	st.PlanPending = true
	st.NextTodoID, st.NextTodoTitle = "t1", "写周报"
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}

	if _, err := a.SkipPhase(); err != nil {
		t.Fatalf("SkipPhase: %v", err)
	}

	got := a.store.Pomodoro()
	if got.PlanPending {
		t.Error("skipping left the plan question up (a zombie gate)")
	}
	if got.NextTodoID != "" || got.NextTodoTitle != "" {
		t.Errorf("skipping left the carried todo behind: %q/%q", got.NextTodoID, got.NextTodoTitle)
	}
	// Skipping still means "start the next phase now" (docs 4.1 D6).
	if got.RunState != store.RunRunning || got.EndsAt == nil {
		t.Errorf("skipping no longer starts the next phase: run=%q endsAt=%v", got.RunState, got.EndsAt)
	}
	if a.focusPlan != "" {
		t.Errorf("skipping kept the abandoned plan: %q", a.focusPlan)
	}
}

// TestSwitchingTheTimerOffClearsTheGateAndTheParkedPlan: switching off is an
// explicit "I am done", so nothing about the question survives it.
func TestSwitchingTheTimerOffClearsTheGateAndTheParkedPlan(t *testing.T) {
	a := newLLMTestApp(t)
	setPlanInFlight(t, a, "整理接口")

	st := a.store.Pomodoro()
	st.PlanPending = true
	st.NextTodoID, st.NextTodoTitle = "t1", "写周报"
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	if _, err := a.showBubble(&BubbleView{Kind: BubblePlan, Title: "接下来想专注做什么？"}); err != nil {
		t.Logf("showBubble (no window): %v", err)
	}

	if _, err := a.SetPomodoroEnabled(false); err != nil {
		t.Fatalf("SetPomodoroEnabled(false): %v", err)
	}

	got := a.store.Pomodoro()
	if got.PlanPending {
		t.Error("switching the timer off left the plan question pending")
	}
	if got.NextTodoID != "" || got.NextTodoTitle != "" {
		t.Errorf("switching off left the carried todo behind: %q/%q", got.NextTodoID, got.NextTodoTitle)
	}
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b != nil {
		t.Errorf("the plan bubble outlived the feature: %+v", b)
	}
	if a.focusPlan != "" {
		t.Errorf("switching off kept the parked plan: %q", a.focusPlan)
	}
}

// TestResettingClearsTheGateAndTheParkedPlan is the explicit "give up".
func TestResettingClearsTheGateAndTheParkedPlan(t *testing.T) {
	a := newLLMTestApp(t)
	setPlanInFlight(t, a, "整理接口")

	st := a.store.Pomodoro()
	st.PlanPending = true
	st.NextTodoID, st.NextTodoTitle = "t1", "写周报"
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	if _, err := a.showBubble(&BubbleView{Kind: BubblePlan, Title: "接下来想专注做什么？"}); err != nil {
		t.Logf("showBubble (no window): %v", err)
	}

	if _, err := a.ResetPomodoro(); err != nil {
		t.Fatalf("ResetPomodoro: %v", err)
	}

	got := a.store.Pomodoro()
	if got.PlanPending {
		t.Error("reset left the plan question pending")
	}
	if got.Phase != store.PhaseIdle {
		t.Errorf("phase = %q, want idle", got.Phase)
	}
	if got.NextTodoID != "" {
		t.Errorf("reset left the carried todo behind: %q", got.NextTodoID)
	}
	a.mu.Lock()
	b := a.bubble
	a.mu.Unlock()
	if b != nil {
		t.Errorf("the plan bubble survived the reset: %+v", b)
	}
	if a.focusPlan != "" {
		t.Errorf("reset kept the parked plan: %q", a.focusPlan)
	}
}
