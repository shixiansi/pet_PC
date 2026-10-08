package main

import (
	"testing"
	"time"

	"PetPC/internal/store"
)

// reminderConfig returns a config with reminders on and no jitter, so the
// schedule arithmetic is deterministic in tests.
func reminderConfig() store.Config {
	c := store.DefaultConfig()
	c.ReminderEnabled = true
	c.ReminderIntervalMin = 30
	c.ReminderJitterMaxMin = 0
	c.ReminderFrom = "09:00"
	c.ReminderTo = "22:00"
	c.ReminderMaxItems = 3
	return c
}

// midday is a time comfortably inside the default active window.
func midday() time.Time {
	return time.Date(2026, 3, 4, 12, 0, 0, 0, time.Local)
}

func idlePomodoro() store.PomodoroState {
	return store.PomodoroState{Enabled: true, Phase: store.PhaseIdle}
}

// TestReminderFiresOnlyWhenDue pins the basic schedule.
func TestReminderFiresOnlyWhenDue(t *testing.T) {
	cfg := reminderConfig()
	now := midday()

	// Nothing scheduled yet: the first check schedules rather than fires, so
	// switching reminders on does not interrupt immediately.
	d := decideReminder(cfg, idlePomodoro(), now, time.Time{}, nil, false)
	if d.Fire {
		t.Error("the first evaluation must schedule, not fire")
	}
	if d.NextIn != 30*time.Minute {
		t.Errorf("NextIn = %v, want 30m", d.NextIn)
	}

	// Before the due time: still waiting.
	d = decideReminder(cfg, idlePomodoro(), now, now.Add(5*time.Minute), nil, false)
	if d.Fire {
		t.Error("fired before it was due")
	}

	// At the due time: fire.
	d = decideReminder(cfg, idlePomodoro(), now, now, nil, false)
	if !d.Fire {
		t.Errorf("did not fire when due: %+v", d)
	}

	// Past the due time (the machine slept): fire rather than skip.
	d = decideReminder(cfg, idlePomodoro(), now, now.Add(-time.Hour), nil, false)
	if !d.Fire {
		t.Errorf("a long-overdue reminder should fire: %+v", d)
	}
}

// TestReminderSuppressedWhileFocusing is the acceptance criterion the whole
// feature is built around.
func TestReminderSuppressedWhileFocusing(t *testing.T) {
	cfg := reminderConfig()
	now := midday()
	end := now.Add(10 * time.Minute)
	due := now.Add(-time.Minute) // already due

	focusing := store.PomodoroState{Enabled: true, Phase: store.PhaseFocus, RunState: store.RunRunning, EndsAt: &end}
	d := decideReminder(cfg, focusing, now, due, nil, false)
	if d.Fire {
		t.Error("a reminder fired in the middle of a focus phase")
	}
	if !d.Missed {
		t.Error("the suppressed reminder must be recorded as missed so it can be made up")
	}
}

// TestPausedAndStoppedAllowReminders is docs 4.3: pause means "not focusing".
func TestPausedAndStoppedAllowReminders(t *testing.T) {
	cfg := reminderConfig()
	now := midday()
	due := now.Add(-time.Minute)

	for _, run := range []string{store.RunPaused, store.RunStopped} {
		st := store.PomodoroState{Enabled: true, Phase: store.PhaseFocus, RunState: run, RemainingOnHalt: 600}
		d := decideReminder(cfg, st, now, due, nil, false)
		if !d.Fire {
			t.Errorf("a %s pomodoro must allow reminders: %+v", run, d)
		}
		if d.Missed {
			t.Errorf("a %s pomodoro should not mark a miss: %+v", run, d)
		}
	}
}

// TestRestingAllowsReminders covers the "best time to interrupt" case.
func TestRestingAllowsReminders(t *testing.T) {
	cfg := reminderConfig()
	now := midday()
	due := now.Add(-time.Minute)
	end := now.Add(3 * time.Minute)

	for _, phase := range []string{store.PhaseShortBreak, store.PhaseLongBreak} {
		st := store.PomodoroState{Enabled: true, Phase: phase, RunState: store.RunRunning, EndsAt: &end}
		d := decideReminder(cfg, st, now, due, nil, false)
		if !d.Fire {
			t.Errorf("%s should allow reminders: %+v", phase, d)
		}
	}
}

// TestRemindDuringBreakSwitchIsEnforced: the setting has to actually gate the
// break, or it is decorative.
func TestRemindDuringBreakSwitchIsEnforced(t *testing.T) {
	cfg := reminderConfig()
	cfg.RemindDuringBreak = false
	now := midday()
	due := now.Add(-time.Minute)
	end := now.Add(3 * time.Minute)

	for _, phase := range []string{store.PhaseShortBreak, store.PhaseLongBreak} {
		st := store.PomodoroState{Enabled: true, Phase: phase, RunState: store.RunRunning, EndsAt: &end}
		d := decideReminder(cfg, st, now, due, nil, false)
		if d.Fire {
			t.Errorf("%s fired even though RemindDuringBreak is off: %+v", phase, d)
		}
	}
	// It must NOT be recorded as a make-up: the user asked for quiet during
	// breaks, so there is nothing owed afterwards.
	if d := decideReminder(cfg, store.PomodoroState{Enabled: true, Phase: store.PhaseShortBreak, RunState: store.RunRunning, EndsAt: &end}, now, due, nil, false); d.Missed {
		t.Error("a deliberate break-time silence should not be queued as a missed reminder")
	}
	// The bundle must carry the same value the gate reads, or the scheduler and
	// the settings screen would disagree about what is switched on.
	if cfg.Reminder().DuringBreak {
		t.Error("the bundle reports DuringBreak=true while the config has it off")
	}
}

// TestReminderSilentWhenOff.
func TestReminderSilentWhenOff(t *testing.T) {
	cfg := reminderConfig()
	cfg.ReminderEnabled = false
	now := midday()
	d := decideReminder(cfg, idlePomodoro(), now, now.Add(-time.Hour), nil, false)
	if d.Fire || d.Missed {
		t.Errorf("reminders are off, so nothing should happen: %+v", d)
	}
}

// TestReminderSilentFullscreen is docs 4.7: a bubble over a full-screen film is
// worse than a missed reminder.
func TestReminderSilentFullscreen(t *testing.T) {
	cfg := reminderConfig()
	now := midday()
	d := decideReminder(cfg, idlePomodoro(), now, now.Add(-time.Minute), nil, true)
	if d.Fire {
		t.Error("a reminder fired over a full-screen window")
	}
	if !d.Missed {
		t.Error("a full-screen suppression should be made up later")
	}
}

// TestReminderRespectsActiveHours.
func TestReminderRespectsActiveHours(t *testing.T) {
	cfg := reminderConfig()
	night := time.Date(2026, 3, 4, 3, 0, 0, 0, time.Local)
	d := decideReminder(cfg, idlePomodoro(), night, night.Add(-time.Minute), nil, false)
	if d.Fire {
		t.Error("a reminder fired outside the active window")
	}
	if d.Missed {
		t.Error("being outside the active window is not a miss to make up")
	}
}

// TestJitterStaysWithinTheConfiguredBand is the acceptance criterion from
// docs 10: the actual gap lands in [interval, interval+jitter].
func TestJitterStaysWithinTheConfiguredBand(t *testing.T) {
	cfg := reminderConfig()
	cfg.ReminderIntervalMin = 30
	cfg.ReminderJitterMaxMin = 3
	rem := cfg.Reminder()

	jitter := jitterFunc(rem.JitterMax)
	seen := map[time.Duration]bool{}
	for i := 0; i < 400; i++ {
		d := nextInterval(rem, jitter)
		seen[d] = true
		if d < 30*time.Minute || d > 33*time.Minute {
			t.Fatalf("interval %v is outside [30m, 33m]", d)
		}
	}
	// If it never varied, the "random" slack would be a constant and the pet
	// would still be a metronome.
	if len(seen) < 10 {
		t.Errorf("expected a spread of intervals, got %d distinct values", len(seen))
	}

	// Zero jitter means exactly the configured interval, every time.
	cfg.ReminderJitterMaxMin = 0
	fixed := jitterFunc(cfg.Reminder().JitterMax)
	for i := 0; i < 20; i++ {
		if d := nextInterval(cfg.Reminder(), fixed); d != 30*time.Minute {
			t.Fatalf("zero jitter should be exact, got %v", d)
		}
	}
}

// TestJitterIsRedrawnPerInterval: drawing once at startup would just be a random
// constant offset, which is not what "avoid a metronome" means.
func TestJitterIsRedrawnPerInterval(t *testing.T) {
	jitter := jitterFunc(3)
	first := jitter()
	changed := false
	for i := 0; i < 100; i++ {
		if jitter() != first {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("the jitter appears to be computed once rather than per interval")
	}
}

// TestDecideReminderNeverFiresWhileFocusingEvenIfOverdue hammers the single most
// important gate with a range of inputs.
func TestDecideReminderNeverFiresWhileFocusingEvenIfOverdue(t *testing.T) {
	cfg := reminderConfig()
	base := midday()
	end := base.Add(10 * time.Minute)

	for _, offset := range []time.Duration{0, -time.Minute, -time.Hour, -24 * time.Hour} {
		for _, inWindow := range []time.Time{base, base.Add(-4 * time.Hour)} {
			_ = inWindow
			st := store.PomodoroState{Enabled: true, Phase: store.PhaseFocus, RunState: store.RunRunning, EndsAt: &end}
			d := decideReminder(cfg, st, base, base.Add(offset), nil, false)
			if d.Fire {
				t.Fatalf("fired while focusing with dueAt offset %v: %+v", offset, d)
			}
		}
	}
}

// TestNextIntervalFallsBackFromNonsense keeps a corrupt interval from producing a
// zero-length schedule that would fire on every tick.
func TestNextIntervalFallsBackFromNonsense(t *testing.T) {
	rem := store.ReminderSettings{IntervalMin: 0}
	if got := nextInterval(rem, nil); got != 30*time.Minute {
		t.Errorf("NextIn = %v, want the 30m default", got)
	}
	rem.IntervalMin = -5
	if got := nextInterval(rem, nil); got != 30*time.Minute {
		t.Errorf("NextIn = %v, want the 30m default", got)
	}
}
