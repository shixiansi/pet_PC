package main

import (
	"testing"
	"time"

	"PetPC/internal/store"
)

// testConfig returns a config with short, easy-to-reason-about timings.
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

func TestStartFocusSetsTodaysSession(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	cfg := testConfig()

	out := startFocus(enabledIdle(), cfg, "t1", "写周报", now)

	st := out.State
	if st.Phase != store.PhaseFocus || !st.IsRunning() || !st.IsFocusing() {
		t.Fatalf("focus did not start: %+v", st)
	}
	if st.CurrentTodoID != "t1" || st.CurrentTodoTitle != "写周报" {
		t.Errorf("todo association lost: %+v", st)
	}
	if got := Remaining(st, now); got != 25*time.Minute {
		t.Errorf("remaining = %v, want 25m", got)
	}
	// Selecting a todo must not touch the todo list: the state holds a copy only.
	if st.CompletedCount != 0 {
		t.Errorf("starting a focus should not complete anything: %+v", st)
	}
}

// TestStartFocusWithoutTodo documents the answer to the last open question in
// docs/reminder-pomodoro.md 13.2: focusing unattached is allowed.
func TestStartFocusWithoutTodo(t *testing.T) {
	now := time.Now()
	out := startFocus(enabledIdle(), testConfig(), "", "", now)
	if !out.State.IsFocusing() {
		t.Fatalf("focus without a todo should be allowed: %+v", out.State)
	}
	if out.State.CurrentTodoID != "" || out.State.CurrentTodoTitle != "" {
		t.Errorf("expected no association: %+v", out.State)
	}
}

// TestPausedFocusIsNotFocusing is the rule the reminder scheduler depends on.
func TestPausedFocusIsNotFocusing(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	cfg := testConfig()
	st := startFocus(enabledIdle(), cfg, "t1", "写周报", now).State

	later := now.Add(10 * time.Minute)
	paused := pause(st, later).State
	if paused.IsFocusing() {
		t.Error("a paused pomodoro must not count as focusing")
	}
	if paused.RunState != store.RunPaused {
		t.Errorf("runState = %q, want paused", paused.RunState)
	}
	// The remainder is frozen at the moment of pausing.
	if got := Remaining(paused, later.Add(time.Hour)); got != 15*time.Minute {
		t.Errorf("remaining while paused = %v, want 15m (must not tick down)", got)
	}
	// Time spent paused does not count against the session.
	resumed := resume(paused, cfg, later.Add(time.Hour)).State
	if !resumed.IsFocusing() {
		t.Fatalf("resume did not restart the phase: %+v", resumed)
	}
	if got := Remaining(resumed, later.Add(time.Hour)); got != 15*time.Minute {
		t.Errorf("remaining after resume = %v, want 15m", got)
	}
}

// TestStopIsRecoverable is docs/reminder-pomodoro.md 13.1: stop differs from
// abandon, and "continue" picks the same session back up.
func TestStopIsRecoverable(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	cfg := testConfig()
	st := startFocus(enabledIdle(), cfg, "t1", "写周报", now).State

	stopped := stop(st, now.Add(7*time.Minute)).State
	if stopped.RunState != store.RunStopped {
		t.Fatalf("runState = %q, want stopped", stopped.RunState)
	}
	if stopped.IsFocusing() {
		t.Error("a stopped pomodoro must not count as focusing (reminders may interrupt)")
	}
	if stopped.Phase != store.PhaseFocus {
		t.Errorf("stop must not change the phase: %+v", stopped)
	}
	if stopped.CurrentTodoID != "t1" {
		t.Errorf("stop lost the association: %+v", stopped)
	}
	if got := Remaining(stopped, now.Add(20*time.Minute)); got != 18*time.Minute {
		t.Errorf("remaining = %v, want 18m", got)
	}

	back := resume(stopped, cfg, now.Add(20*time.Minute)).State
	if !back.IsFocusing() || Remaining(back, now.Add(20*time.Minute)) != 18*time.Minute {
		t.Errorf("resume after stop must continue the same session: %+v", back)
	}
	if back.CurrentTodoID != "t1" {
		t.Errorf("resume lost the association: %+v", back)
	}
}

// TestResetClearsEverythingButCadence covers the explicit "abandon" escape hatch.
func TestResetClearsEverythingButCadence(t *testing.T) {
	now := time.Now()
	cfg := testConfig()
	st := startFocus(enabledIdle(), cfg, "t1", "写周报", now).State
	st.CompletedCount = 2

	out := reset(st)
	if out.State.Phase != store.PhaseIdle || out.State.RunState != "" {
		t.Errorf("reset did not return to idle: %+v", out.State)
	}
	if out.State.CurrentTodoID != "" || out.State.RemainingOnHalt != 0 {
		t.Errorf("reset left session data behind: %+v", out.State)
	}
	if out.State.CompletedCount != 2 {
		t.Errorf("reset should keep the long-break cadence, got %d", out.State.CompletedCount)
	}
	if !out.State.Enabled {
		t.Error("reset should not switch the feature off")
	}
}

// TestFocusEndProducesRecordAndBreak is the core transition.
func TestFocusEndProducesRecordAndBreak(t *testing.T) {
	start := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	cfg := testConfig()
	st := startFocus(enabledIdle(), cfg, "t1", "写周报", start).State

	end := start.Add(25 * time.Minute)
	out := finishPhase(st, cfg, end, end, false)

	if out.Record == nil {
		t.Fatal("a finished focus phase must always produce a record")
	}
	rec := *out.Record
	if rec.TodoID != "t1" || rec.TodoTitle != "写周报" {
		t.Errorf("record lost its todo snapshot: %+v", rec)
	}
	if rec.FocusMinutes != 25 {
		t.Errorf("focusMinutes = %d, want 25", rec.FocusMinutes)
	}
	if !rec.StartedAt.Equal(start) || !rec.EndedAt.Equal(end) {
		t.Errorf("record timestamps wrong: %v -> %v", rec.StartedAt, rec.EndedAt)
	}
	if rec.Summary != "" {
		t.Errorf("summary should start empty: %+v", rec)
	}

	// The break is in place but PARKED: a finished focus waits for its record
	// before the rest starts counting. AutoStartNextPhase does not override that,
	// which is the whole point of the gate - a session nobody described must not
	// bank a break.
	if out.NextPhase != store.PhaseShortBreak {
		t.Errorf("nextPhase = %q, want short_break", out.NextPhase)
	}
	if out.State.IsRunning() {
		t.Errorf("the break started before the record was answered: %+v", out.State)
	}
	if out.State.RunState != store.RunStopped {
		t.Errorf("runState = %q, want stopped while the record is pending", out.State.RunState)
	}
	// Its full length is banked, so answering the prompt starts a whole break.
	if got := Remaining(out.State, end); got != 5*time.Minute {
		t.Errorf("parked break remaining = %v, want the full 5m", got)
	}
	if out.State.CompletedCount != 1 {
		t.Errorf("completedCount = %d, want 1", out.State.CompletedCount)
	}
	// The association belongs to the finished session, not to the break.
	if out.State.CurrentTodoID != "" {
		t.Errorf("break inherited the focus association: %+v", out.State)
	}
	// The record is what the summary bubble edits.
	if out.Record.ID != "" {
		t.Errorf("the engine must not invent an ID; the store assigns it: %q", out.Record.ID)
	}
}

// TestFocusEndWithoutAutoStart parks the next phase instead of running it.
func TestFocusEndWithoutAutoStart(t *testing.T) {
	start := time.Now()
	cfg := testConfig()
	cfg.AutoStartNextPhase = false
	st := startFocus(enabledIdle(), cfg, "", "", start).State

	out := finishPhase(st, cfg, start.Add(25*time.Minute), start.Add(25*time.Minute), false)
	if out.State.RunState != store.RunStopped {
		t.Errorf("runState = %q, want stopped", out.State.RunState)
	}
	if out.State.Phase != store.PhaseShortBreak {
		t.Errorf("phase = %q, want short_break", out.State.Phase)
	}
	if out.State.RemainingOnHalt != int((5 * time.Minute).Seconds()) {
		t.Errorf("the parked break should hold its full length, got %ds", out.State.RemainingOnHalt)
	}
	if out.State.IsFocusing() {
		t.Error("a parked break is not focusing")
	}
	// The record is written either way: the session happened.
	if out.Record == nil {
		t.Error("record must be produced regardless of auto-start")
	}
}

// TestLongBreakAfterNthFocus pins the cadence: the 4th completed focus is
// followed by a long break, not a short one.
func TestLongBreakAfterNthFocus(t *testing.T) {
	now := time.Now()
	cfg := testConfig()

	for completed := 1; completed <= 5; completed++ {
		st := store.PomodoroState{
			Enabled:        true,
			Phase:          store.PhaseFocus,
			RunState:       store.RunRunning,
			CompletedCount: completed - 1,
		}
		out := finishPhase(st, cfg, now, now, false)
		want := store.PhaseShortBreak
		if completed%4 == 0 {
			want = store.PhaseLongBreak
		}
		if out.NextPhase != want {
			t.Errorf("after focus #%d: next = %q, want %q", completed, out.NextPhase, want)
		}
		if out.State.CompletedCount != completed {
			t.Errorf("after focus #%d: completedCount = %d", completed, out.State.CompletedCount)
		}
	}
}

// TestBreakEndReturnsToFocus checks the other half of the cycle.
func TestBreakEndReturnsToFocus(t *testing.T) {
	now := time.Now()
	cfg := testConfig()
	st := store.PomodoroState{
		Enabled: true, Phase: store.PhaseShortBreak, RunState: store.RunRunning,
		CompletedCount: 1,
	}
	out := finishPhase(st, cfg, now, now, false)
	if out.NextPhase != store.PhaseFocus {
		t.Errorf("nextPhase = %q, want focus", out.NextPhase)
	}
	if out.Record != nil {
		t.Error("a finished break must not write a record")
	}
	if !out.State.IsFocusing() {
		t.Errorf("the next focus should be running: %+v", out.State)
	}
}

// TestSkipMovesOnWithoutRecording: skipping is the one way to keep a session out
// of the permanent archive, so it must genuinely not record.
func TestSkipMovesOnWithoutRecording(t *testing.T) {
	now := time.Now()
	cfg := testConfig()
	st := startFocus(enabledIdle(), cfg, "t1", "写周报", now).State

	out := skipPhase(st, cfg, now.Add(3*time.Minute))
	if out.EndedPhase != store.PhaseFocus {
		t.Errorf("endedPhase = %q, want focus", out.EndedPhase)
	}
	if out.Record != nil {
		t.Error("a skipped focus must not produce a record")
	}
	if out.State.Phase != store.PhaseShortBreak {
		t.Errorf("phase = %q, want short_break", out.State.Phase)
	}
	// It still advances the cadence, so a skipping user still reaches a long break.
	if out.State.CompletedCount != 1 {
		t.Errorf("completedCount = %d, want 1", out.State.CompletedCount)
	}
}

// TestReconcileCompletesElapsedPhase covers closing the pet mid-focus.
func TestReconcileCompletesElapsedPhase(t *testing.T) {
	start := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	cfg := testConfig()
	st := startFocus(enabledIdle(), cfg, "t1", "写周报", start).State

	// Reopened 30 minutes later: the focus ran out while the pet was closed.
	out := reconcile(st, cfg, start.Add(30*time.Minute))
	if !out.Missed {
		t.Error("an elapsed phase should be reported as missed")
	}
	if out.Record == nil {
		t.Fatal("an elapsed focus phase must still be recorded: the time really passed")
	}
	// The record ends when the phase ended, not when the app was reopened:
	// otherwise every record would be stretched by however long the pet was shut.
	if want := start.Add(25 * time.Minute); !out.Record.EndedAt.Equal(want) {
		t.Errorf("record endedAt = %v, want %v (the phase's own end, not the reopen time)", out.Record.EndedAt, want)
	}
	if out.State.Phase != store.PhaseShortBreak {
		t.Errorf("phase = %q, want short_break", out.State.Phase)
	}
}

// TestCatchUpDoesNotCascade is a regression test for a bug found by running the
// packaged app with a focus phase that had expired while it was closed.
//
// The record must be dated from the phase's own end (03:35), but the next phase
// must start from "now" (14:00). Deriving the break's end time from the phase's
// end instead made it expire the moment it began - and because the pet then
// completed the *following* focus phase instantly too, a long absence
// fabricated a fresh 25-minute focus record on every tick.
func TestCatchUpDoesNotCascade(t *testing.T) {
	start := time.Date(2026, 3, 4, 3, 10, 0, 0, time.Local)
	phaseEnd := start.Add(25 * time.Minute) // 03:35
	reopened := time.Date(2026, 3, 4, 14, 0, 0, 0, time.Local)

	cfg := testConfig()
	st := startFocus(enabledIdle(), cfg, "t1", "写周报", start).State

	out := reconcile(st, cfg, reopened)
	if out.Record == nil {
		t.Fatal("the elapsed focus phase should have been recorded")
	}
	// The record is bounded by the phase, not by the absence.
	if !out.Record.EndedAt.Equal(phaseEnd) {
		t.Errorf("record endedAt = %v, want %v", out.Record.EndedAt, phaseEnd)
	}
	if !out.Record.StartedAt.Equal(start) {
		t.Errorf("record startedAt = %v, want %v", out.Record.StartedAt, start)
	}

	// The break is in place with its full length banked, but PARKED: it waits for
	// the record to be answered before it starts counting. Note that this is also
	// what keeps the anti-cascade property - a parked phase cannot expire, so the
	// second pass below cannot roll the pet into another focus phase.
	if out.NextPhase != store.PhaseShortBreak {
		t.Fatalf("nextPhase = %q, want short_break", out.NextPhase)
	}
	if got := Remaining(out.State, reopened); got != 5*time.Minute {
		t.Errorf("parked break remaining = %v, want the full 5m banked", got)
	}
	if out.State.IsRunning() {
		t.Errorf("the break started before its record was answered: %+v", out.State)
	}
	if out.State.EndsAt != nil {
		t.Errorf("a parked phase must have no end time, got %v", out.State.EndsAt)
	}

	// Feeding the result straight back in must complete nothing: a parked phase has
	// nothing to expire, so a long absence cannot fabricate a second record.
	again := reconcile(out.State, cfg, reopened)
	if again.Finished() {
		t.Errorf("the parked phase expired by itself: %+v", again)
	}
	if again.Record != nil {
		t.Error("a cascade fabricated a second record")
	}
}

// TestReconcileLeavesRunningPhaseAlone: a phase with time left is untouched, so
// the countdown survives a restart.
func TestReconcileLeavesRunningPhaseAlone(t *testing.T) {
	start := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	cfg := testConfig()
	st := startFocus(enabledIdle(), cfg, "t1", "写周报", start).State

	out := reconcile(st, cfg, start.Add(10*time.Minute))
	if out.Finished() || out.ClockJump {
		t.Errorf("a live phase should be left alone: %+v", out)
	}
	if got := Remaining(out.State, start.Add(10*time.Minute)); got != 15*time.Minute {
		t.Errorf("remaining = %v, want 15m", got)
	}
}

// TestReconcileDetectsBackwardsClock: a countdown longer than its own phase is
// impossible, so it is abandoned rather than displayed (docs 9).
func TestReconcileDetectsBackwardsClock(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	cfg := testConfig()
	st := startFocus(enabledIdle(), cfg, "", "", now).State

	// The clock jumped back a day.
	out := reconcile(st, cfg, now.Add(-24*time.Hour))
	if !out.ClockJump {
		t.Fatalf("a backwards clock jump should be detected: %+v", out)
	}
	if out.State.Phase != store.PhaseIdle {
		t.Errorf("state should be reset to idle, got %q", out.State.Phase)
	}
	if Remaining(out.State, now) != 0 {
		t.Error("a reset countdown must not report time left")
	}
	// No record: nothing verifiably happened.
	if out.Record != nil {
		t.Error("a clock jump must not fabricate a record")
	}
}

// TestReconcileIgnoresDisabledTimer: with the feature off, nothing is running
// even if a stale file says otherwise.
func TestReconcileIgnoresDisabledTimer(t *testing.T) {
	now := time.Now()
	cfg := testConfig()
	end := now.Add(-time.Hour)
	st := store.PomodoroState{
		Enabled: false, Phase: store.PhaseFocus, RunState: store.RunRunning, EndsAt: &end,
	}
	out := reconcile(st, cfg, now)
	if out.Finished() || out.Record != nil {
		t.Errorf("a disabled timer must not advance: %+v", out)
	}
}

// TestResumeWithNoBankedTimeFallsBackToFullPhase guards against a zero-length
// phase ending instantly.
func TestResumeWithNoBankedTimeFallsBackToFullPhase(t *testing.T) {
	now := time.Now()
	cfg := testConfig()
	st := store.PomodoroState{
		Enabled: true, Phase: store.PhaseFocus, RunState: store.RunStopped, RemainingOnHalt: 0,
	}
	out := resume(st, cfg, now)
	if got := Remaining(out.State, now); got != 25*time.Minute {
		t.Errorf("remaining = %v, want a full 25m phase", got)
	}
}

// TestNoOpTransitionsAreHarmless: every button can be pressed at the wrong time
// without corrupting the state.
func TestNoOpTransitionsAreHarmless(t *testing.T) {
	now := time.Now()
	cfg := testConfig()
	idle := enabledIdle()

	if out := pause(idle, now); out.State.Phase != store.PhaseIdle {
		t.Errorf("pausing idle changed the state: %+v", out.State)
	}
	if out := resume(idle, cfg, now); out.State.Phase != store.PhaseIdle {
		t.Errorf("resuming idle changed the state: %+v", out.State)
	}
	if out := stop(idle, now); out.State.Phase != store.PhaseIdle {
		t.Errorf("stopping idle changed the state: %+v", out.State)
	}
	if out := finishPhase(idle, cfg, now, now, false); out.Finished() || out.Record != nil {
		t.Errorf("finishing idle produced something: %+v", out)
	}
	if out := skipPhase(idle, cfg, now); out.Finished() {
		t.Errorf("skipping idle produced something: %+v", out)
	}

	running := startFocus(idle, cfg, "", "", now).State
	if out := resume(running, cfg, now); out.State.RunState != store.RunRunning {
		t.Errorf("resuming a running phase changed it: %+v", out.State)
	}
	if out := pause(running, now); out.State.RunState != store.RunPaused {
		t.Errorf("double pause: %+v", out.State)
	}
}

func TestFormatCountdown(t *testing.T) {
	cases := map[time.Duration]string{
		25 * time.Minute:                   "25:00",
		90 * time.Second:                   "01:30",
		0:                                  "00:00",
		-5 * time.Second:                   "00:00",
		1500*time.Millisecond + 0:          "00:02", // rounds up: never shows 00:00 early
		time.Minute + 200*time.Millisecond: "01:01",
		// A long focus phase must not wrap into a meaningless value.
		120 * time.Minute: "120:00",
	}
	for d, want := range cases {
		if got := formatCountdown(d); got != want {
			t.Errorf("formatCountdown(%v) = %q, want %q", d, got, want)
		}
	}
}

// TestSecondsAndCountdownAgree is a regression test for the countdown flickering.
//
// The badge flickered between two values once a second because RemainingSec
// truncated while Countdown rounded up, so the same instant had two different
// numbers - and the frontend's own tick recomputed a third. Both representations
// must now come from one rounding step.
func TestSecondsAndCountdownAgree(t *testing.T) {
	// Sweep across a whole second boundary in fine steps, which is where the two
	// rounding rules used to diverge.
	base := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	for ms := 0; ms < 2000; ms += 37 {
		d := 5*time.Minute - time.Duration(ms)*time.Millisecond
		secs := ceilSeconds(d)
		if got := formatSeconds(secs); got != formatCountdown(d) {
			t.Fatalf("at %v: formatSeconds(%d) = %q but formatCountdown = %q", d, secs, got, formatCountdown(d))
		}
		// The value must never be less than the true remainder (that would show
		// 00:00 early), nor more than a second ahead of it.
		if time.Duration(secs)*time.Second < d {
			t.Fatalf("at %v: %d seconds is less than the remaining time", d, secs)
		}
		if time.Duration(secs)*time.Second-d >= time.Second {
			t.Fatalf("at %v: %d seconds overshoots the remainder by a second or more", d, secs)
		}
		_ = base
	}
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

func TestPhaseLabel(t *testing.T) {
	want := map[string]string{
		store.PhaseIdle:       "空闲",
		store.PhaseFocus:      "专注",
		store.PhaseShortBreak: "短休息",
		store.PhaseLongBreak:  "长休息",
		"nonsense":            "空闲",
	}
	for phase, label := range want {
		if got := phaseLabel(phase); got != label {
			t.Errorf("phaseLabel(%q) = %q, want %q", phase, got, label)
		}
	}
}
