// Package pomodoro_test checks the package from OUTSIDE it.
//
// Every other test in this directory is an internal test (package pomodoro), so a
// rename that stays consistent inside the package would keep them all green while
// the host - which only sees the exported names - stopped compiling. That is not
// hypothetical: the extraction's own seam in package main calls exactly these
// names, and the two are the only places the boundary is crossed.
//
// This file is also the pin for the two behaviours the internal tests could not
// have covered before the move existed: that a finished focus produces a record,
// and that it PARKS the next phase instead of running it.
package pomodoro_test

import (
	"testing"
	"time"

	"PetPC/internal/pomodoro"
	"PetPC/internal/store"
)

// TestExportedSurfaceFromOutside walks one full focus phase through the API the
// host actually uses.
func TestExportedSurfaceFromOutside(t *testing.T) {
	cfg := store.DefaultConfig()
	cfg.FocusMinutes = 25
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	idle := store.PomodoroState{Enabled: true, Phase: store.PhaseIdle}

	started := pomodoro.StartFocus(idle, cfg, "t1", "写周报", now)
	if started.Finished() {
		t.Fatal("starting a focus phase must not report a finished phase")
	}
	if started.State.Phase != store.PhaseFocus || started.State.RunState != store.RunRunning {
		t.Fatalf("StartFocus produced phase=%q run=%q", started.State.Phase, started.State.RunState)
	}
	if started.State.CurrentTodoID != "t1" {
		t.Errorf("the association was not taken: currentTodo=%q", started.State.CurrentTodoID)
	}

	end := now.Add(25 * time.Minute)
	done := pomodoro.FinishPhase(started.State, cfg, end, end, false)
	if !done.Finished() {
		t.Fatal("FinishPhase must report the phase it ended")
	}
	if done.EndedPhase != store.PhaseFocus {
		t.Errorf("endedPhase = %q, want %q", done.EndedPhase, store.PhaseFocus)
	}
	if done.Record == nil {
		t.Fatal("a finished focus phase must produce a record")
	}
	if done.Record.TodoID != "t1" || done.Record.TodoTitle != "写周报" {
		t.Errorf("the record lost its association: %+v", done.Record)
	}
	if done.Record.FocusMinutes != 25 {
		t.Errorf("record.FocusMinutes = %d, want 25", done.Record.FocusMinutes)
	}

	// The gate: a finished FOCUS parks the next phase on its record, whatever
	// AutoStartNextPhase says. If this ever starts running by itself, the summary
	// question becomes a notification instead of a gate.
	if done.State.RunState != store.RunStopped {
		t.Errorf("the break must wait for the record, got run=%q", done.State.RunState)
	}
	if done.State.PendingRecordID != "" {
		t.Errorf("the machine must not invent the record id: got %q", done.State.PendingRecordID)
	}
	if done.State.NextTodoID != "t1" {
		t.Errorf("the session's todo must be carried into the next plan, got %q", done.State.NextTodoID)
	}

	// A second call over the parked state must not fabricate another record.
	again := pomodoro.Reconcile(done.State, cfg, end.Add(time.Hour))
	if again.Finished() || again.Record != nil {
		t.Error("a parked phase expired by itself: a long absence fabricated a record")
	}

	if got := pomodoro.PhaseLabel(store.PhaseLongBreak); got != "长休息" {
		t.Errorf("PhaseLabel(longBreak) = %q", got)
	}
	if got := pomodoro.FormatCountdown(90 * time.Second); got != "01:30" {
		t.Errorf("FormatCountdown(90s) = %q", got)
	}
	if got := pomodoro.Remaining(done.State, end); got != 5*time.Minute {
		t.Errorf("Remaining on a parked short break = %v, want 5m", got)
	}
}
