package pomodoro

import (
	"testing"
	"time"

	"PetPC/internal/store"
)

// Carrying the finished focus session's todo forward.
//
// A focus session takes its association from the plan question ("this is what I
// am about to work on"), and the session it ends is the one that knows *what*
// that turned out to be. So the todo is carried into NextTodo*, not left behind:
// the plan bubble for the NEXT focus gives the user something to confirm or
// replace, which is what makes the manual path a single choice rather than two.
//
// The rule this file pins is the asymmetry: a finished FOCUS fills NextTodo* and
// clears CurrentTodo*; a finished BREAK touches neither.

// TestFocusEndCarriesTheTodoIntoThePlanGate is the core of T3.
func TestFocusEndCarriesTheTodoIntoThePlanGate(t *testing.T) {
	start := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	cfg := testConfig()
	st := StartFocus(enabledIdle(), cfg, "t1", "写周报", start).State

	end := start.Add(25 * time.Minute)
	out := FinishPhase(st, cfg, end, end, false)

	if out.State.NextTodoID != "t1" {
		t.Errorf("nextTodoId = %q, want t1 (the todo of the session that just ended)", out.State.NextTodoID)
	}
	if out.State.NextTodoTitle != "写周报" {
		t.Errorf("nextTodoTitle = %q, want 写周报", out.State.NextTodoTitle)
	}

	// The association is the finished session's, so it does NOT survive on the
	// current-session fields - that is what stops the break inheriting it.
	if out.State.CurrentTodoID != "" || out.State.CurrentTodoTitle != "" {
		t.Errorf("the break inherited the focus association: %+v", out.State)
	}

	// And the record still carries it, because the record is the permanent
	// description of what the session was about.
	if out.Record == nil {
		t.Fatal("a finished focus phase must always produce a record")
	}
	if out.Record.TodoID != "t1" || out.Record.TodoTitle != "写周报" {
		t.Errorf("record lost its todo snapshot: %+v", *out.Record)
	}
}

// TestFocusEndCarriesAnEmptyAssociation documents that "carry" is a copy, not a
// value that is required to be set: focusing unattached is legitimate
// (docs/reminder-pomodoro.md 13.2), and the next plan bubble must then simply
// have no todo line.
func TestFocusEndCarriesAnEmptyAssociation(t *testing.T) {
	now := time.Now()
	cfg := testConfig()
	st := StartFocus(enabledIdle(), cfg, "", "", now).State

	out := FinishPhase(st, cfg, now.Add(25*time.Minute), now.Add(25*time.Minute), false)
	if out.State.NextTodoID != "" || out.State.NextTodoTitle != "" {
		t.Errorf("an unattached session invented an association: %+v", out.State)
	}
	if out.Record == nil || out.Record.TodoID != "" {
		t.Errorf("record for an unattached session is wrong: %+v", out.Record)
	}
}

// TestBreakEndLeavesTheCarriedTodoAlone is the other half of the asymmetry.
//
// A finished break does not know anything about a todo, so it must not overwrite
// what the preceding focus carried. If it cleared the fields, the plan question
// after a break would lose the todo the user had just worked on; if it wrote
// something, it would invent an association.
func TestBreakEndLeavesTheCarriedTodoAlone(t *testing.T) {
	now := time.Now()
	cfg := testConfig()
	st := store.PomodoroState{
		Enabled: true, Phase: store.PhaseShortBreak, RunState: store.RunRunning,
		CompletedCount: 1,
		// A carried todo is already in place, as it would be in a real cycle.
		NextTodoID: "t1", NextTodoTitle: "写周报",
		// And the break's own association fields are clear.
		CurrentTodoID: "", CurrentTodoTitle: "",
	}

	out := FinishPhase(st, cfg, now, now, false)

	if out.State.NextTodoID != "t1" || out.State.NextTodoTitle != "写周报" {
		t.Errorf("the break rewrote the carried todo: %+v", out.State)
	}
	// The break branch keeps its own published semantics: with AutoStartNextPhase
	// on, the next focus is already RUNNING with an end time. (T28: this assertion
	// must not be weakened - the plan gate is added by the app layer on top of it,
	// not by changing the engine.)
	if out.NextPhase != store.PhaseFocus {
		t.Fatalf("nextPhase = %q, want focus", out.NextPhase)
	}
	if !out.State.IsRunning() || out.State.EndsAt == nil {
		t.Errorf("the break must still roll straight into a running focus: %+v", out.State)
	}
	if out.Record != nil {
		t.Errorf("a finished break wrote a record: %+v", out.Record)
	}
}

// TestFocusCycleCarriesTheTodoAcrossTheBreak pins the carried value surviving a
// whole cycle the way the app drives it, without going through the plan gate
// (which is the app layer's business, not the state machine's).
func TestFocusCycleCarriesTheTodoAcrossTheBreak(t *testing.T) {
	start := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	cfg := testConfig()

	focus := StartFocus(enabledIdle(), cfg, "t1", "写周报", start).State
	afterFocus := FinishPhase(focus, cfg, start.Add(25*time.Minute), start.Add(25*time.Minute), false).State
	if afterFocus.NextTodoID != "t1" {
		t.Fatalf("the focus did not carry its todo: %+v", afterFocus)
	}

	// The break runs out five minutes later.
	breakEnd := start.Add(30 * time.Minute)
	afterBreak := FinishPhase(afterFocus, cfg, breakEnd, breakEnd, false).State
	if afterBreak.NextTodoID != "t1" || afterBreak.NextTodoTitle != "写周报" {
		t.Errorf("the carried todo did not survive the break: %+v", afterBreak)
	}
}
