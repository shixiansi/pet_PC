package main

import (
	"testing"
	"time"

	"PetPC/internal/store"
)

// testConfig returns a config with short, easy-to-reason-about timings.
//
// It and enabledIdle are used by the state-machine tests too (they live in
// internal/pomodoro now, with their own copies) and by the gate tests here, so
// this file keeps them even though its only remaining test does not need them.

func testConfig() store.Config {
	c := store.DefaultConfig()
	c.FocusMinutes = 25
	c.ShortBreakMinutes = 5
	c.LongBreakMinutes = 15
	c.LongBreakEvery = 4
	c.AutoStartNextPhase = true
	return c
}

// enabledIdle is the state after the user switches the pomodoro on.
func enabledIdle() store.PomodoroState {
	return store.PomodoroState{Enabled: true, Phase: store.PhaseIdle}
}

// TestPomodoroViewCountdownMatchesSeconds checks the view the frontend actually
// receives, so the two fields cannot drift apart again at the boundary.
func TestPomodoroViewCountdownMatchesSeconds(t *testing.T) {
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	a := &App{store: s}
	now := time.Now()

	// A phase ending a fraction of a second from now is the case that used to
	// disagree: truncation said 0, rounding up said 1.
	end := now.Add(400 * time.Millisecond)
	st := store.PomodoroState{
		Enabled: true, Phase: store.PhaseFocus, RunState: store.RunRunning, EndsAt: &end,
	}
	if err := s.SavePomodoro(st); err != nil {
		t.Fatal(err)
	}
	view := a.pomodoroView()
	if view.RemainingSec != 1 {
		t.Errorf("remainingSec = %d, want 1 (rounded up, not truncated to 0)", view.RemainingSec)
	}
	if view.Countdown != formatSeconds(view.RemainingSec) {
		t.Errorf("countdown %q disagrees with remainingSec %d", view.Countdown, view.RemainingSec)
	}
	if view.EndsAtMs != end.UnixMilli() {
		t.Errorf("endsAtMs = %d, want %d", view.EndsAtMs, end.UnixMilli())
	}

	// A parked phase has no end time, so the frontend must not be told to tick.
	parked := store.PomodoroState{
		Enabled: true, Phase: store.PhaseFocus, RunState: store.RunStopped, RemainingOnHalt: 90,
	}
	if err := s.SavePomodoro(parked); err != nil {
		t.Fatal(err)
	}
	view = a.pomodoroView()
	if view.EndsAtMs != 0 {
		t.Errorf("a halted phase must not report an end time, got %d", view.EndsAtMs)
	}
	if view.RemainingSec != 90 || view.Countdown != "01:30" {
		t.Errorf("halted phase shows %d/%q, want 90/\"01:30\"", view.RemainingSec, view.Countdown)
	}
}
