package main

import (
	"testing"
	"time"

	"PetPC/internal/store"
)

// Proactive speech is the pet speaking up on its own (docs/llm-chat.md 9).
//
// The decision is a pure function - decideProactive - so the whole gate list can
// be tested without a clock, a window or a running pet. Every gate exists to
// answer "would this be an interruption the user did not ask for?", so the
// default answer is no, and that is what most of these tests pin.

// proactiveConfig is a config with everything the gate needs switched on.
func proactiveConfig() store.Config {
	cfg := store.DefaultConfig()
	cfg.LLMEnabled = true
	cfg.LLMProactive = true
	cfg.LLMProactiveMinGapMin = 30
	cfg.ReminderEnabled = true
	cfg.ReminderFrom = "00:00"
	cfg.ReminderTo = "23:59"
	return cfg
}

// idleTimer is a pomodoro that is on but not focusing, so it does not suppress.
// RunStopped is the "on but not counting" state; PhaseIdle is what makes
// IsFocusing false.
var idleTimer = store.PomodoroState{Enabled: true, Phase: store.PhaseIdle, RunState: store.RunStopped}

// TestProactiveSpeaksWhenDue is the happy path: every gate open and the schedule
// due.
func TestProactiveSpeaksWhenDue(t *testing.T) {
	now := time.Date(2026, 3, 10, 14, 0, 0, 0, time.Local)
	d := decideProactive(proactiveConfig(), idleTimer, now,
		now.Add(-2*time.Hour), now.Add(-time.Minute), false, false, false, true, false, false)
	if !d.Fire {
		t.Fatalf("did not speak: %s", d.Reason)
	}
}

// TestProactiveIsOffByDefault is the promise the README makes: the feature exists
// but never speaks unless it is asked to.
func TestProactiveIsOffByDefault(t *testing.T) {
	cfg := store.DefaultConfig()
	cfg.LLMEnabled = true // on, so the only closed gate is the proactive switch
	if cfg.LLMProactive {
		t.Fatal("proactive speech must be off by default")
	}
	now := time.Now()
	d := decideProactive(cfg, idleTimer, now, time.Time{}, now.Add(-time.Hour), false, false, false, true, false, false)
	if d.Fire {
		t.Error("the pet spoke with proactive speech switched off")
	}
}

// TestProactiveGatesEachSuppress walks the whole gate list. Each entry is one
// reason the user would not want to be interrupted, and each must hold on its
// own: a gate that only works in combination is not a gate.
func TestProactiveGatesEachSuppress(t *testing.T) {
	now := time.Date(2026, 3, 10, 14, 0, 0, 0, time.Local)
	due := now.Add(-time.Minute)
	spoke := now.Add(-2 * time.Hour)
	focusEnd := now.Add(10 * time.Minute)

	focusing := store.PomodoroState{
		Enabled: true, Phase: store.PhaseFocus, RunState: store.RunRunning, EndsAt: &focusEnd,
	}

	cases := []struct {
		name string
		cfg  store.Config
		st   store.PomodoroState
		last time.Time
		due  time.Time
		full bool
		budg bool
		panl bool
		mat  bool
		ask  bool
		hid  bool
	}{
		{name: "the LLM chat is off", cfg: func() store.Config {
			c := proactiveConfig()
			c.LLMEnabled = false
			return c
		}(), st: idleTimer, last: spoke, due: due, mat: true},
		{name: "proactive speech is off", cfg: func() store.Config {
			c := proactiveConfig()
			c.LLMProactive = false
			return c
		}(), st: idleTimer, last: spoke, due: due, mat: true},
		{name: "the budget is spent", cfg: proactiveConfig(), st: idleTimer, last: spoke, due: due, budg: true, mat: true},
		{name: "a focus phase is running", cfg: proactiveConfig(), st: focusing, last: spoke, due: due, mat: true},
		{name: "a full-screen window is in front", cfg: proactiveConfig(), st: idleTimer, last: spoke, due: due, full: true, mat: true},
		{name: "the panel is open", cfg: proactiveConfig(), st: idleTimer, last: spoke, due: due, panl: true, mat: true},
		{name: "a bubble is waiting for an answer", cfg: proactiveConfig(), st: idleTimer, last: spoke, due: due, mat: true, ask: true},
		{name: "the pet is hidden", cfg: proactiveConfig(), st: idleTimer, last: spoke, due: due, mat: true, hid: true},
		{name: "nothing worth saying", cfg: proactiveConfig(), st: idleTimer, last: spoke, due: due},
		{name: "inside the minimum gap", cfg: proactiveConfig(), st: idleTimer, last: now.Add(-time.Minute), due: due, mat: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := decideProactive(tc.cfg, tc.st, now, tc.last, tc.due, tc.full, tc.budg, tc.panl, tc.mat, tc.ask, tc.hid)
			if d.Fire {
				t.Error("the pet spoke when it should not have")
			}
			if d.Reason == "" {
				t.Error("no reason reported; the debug log would say nothing useful")
			}
		})
	}
}

// TestProactiveRespectsTheMinimumGap checks the arithmetic, not just the gate: the
// gap is measured from the last remark, and the wait it reports must be the
// remainder rather than the whole gap again.
func TestProactiveRespectsTheMinimumGap(t *testing.T) {
	now := time.Date(2026, 3, 10, 14, 0, 0, 0, time.Local)
	cfg := proactiveConfig()
	cfg.LLMProactiveMinGapMin = 30
	spoke := now.Add(-20 * time.Minute)

	d := decideProactive(cfg, idleTimer, now, spoke, now.Add(-time.Minute), false, false, false, true, false, false)
	if d.Fire {
		t.Fatal("spoke inside the minimum gap")
	}
	if want := 10 * time.Minute; d.NextIn != want {
		t.Errorf("NextIn = %v, want %v (the remainder, not the whole gap)", d.NextIn, want)
	}

	// Exactly at the gap it may speak.
	d = decideProactive(cfg, idleTimer, now, now.Add(-30*time.Minute), now.Add(-time.Minute), false, false, false, true, false, false)
	if !d.Fire {
		t.Errorf("did not speak at exactly the gap: %s", d.Reason)
	}
	// And one second short of it may not.
	d = decideProactive(cfg, idleTimer, now, now.Add(-30*time.Minute+time.Second), now.Add(-time.Minute), false, false, false, true, false, false)
	if d.Fire {
		t.Error("spoke one second before the gap elapsed")
	}
}

// TestProactiveNeverSpeaksWhileFocusing hammers the gate the user cares about
// most: the pet's own opinion must never interrupt a focus phase, whatever the
// schedule says.
func TestProactiveNeverSpeaksWhileFocusing(t *testing.T) {
	now := time.Date(2026, 3, 10, 14, 0, 0, 0, time.Local)
	cfg := proactiveConfig()

	for i := 0; i < 200; i++ {
		end := now.Add(time.Duration(i) * time.Minute)
		st := store.PomodoroState{
			Enabled: true, Phase: store.PhaseFocus, RunState: store.RunRunning, EndsAt: &end,
		}
		// The schedule is long overdue in every iteration.
		d := decideProactive(cfg, st, now, time.Time{}, now.Add(-24*time.Hour), false, false, false, true, false, false)
		if d.Fire {
			t.Fatalf("iteration %d: the pet interrupted a focus phase", i)
		}
	}
}

// TestProactiveSchedulesRatherThanSpeakingImmediately pins the "switching it on
// does not interrupt at once" rule, which is also what the reminder does.
func TestProactiveSchedulesRatherThanSpeakingImmediately(t *testing.T) {
	now := time.Date(2026, 3, 10, 14, 0, 0, 0, time.Local)
	cfg := proactiveConfig()
	cfg.LLMProactiveMinGapMin = 45

	// Never spoken, nothing scheduled: the pet must wait, not talk.
	d := decideProactive(cfg, idleTimer, now, time.Time{}, time.Time{}, false, false, false, true, false, false)
	if d.Fire {
		t.Fatal("spoke the moment the feature was switched on")
	}
	if want := 45 * time.Minute; d.NextIn != want {
		t.Errorf("NextIn = %v, want the configured gap %v", d.NextIn, want)
	}
}

// TestProactiveFallsBackFromANonsenseGap keeps a corrupt config from turning the
// pet into a metronome: a gap of zero would otherwise mean "speak on every tick".
func TestProactiveFallsBackFromANonsenseGap(t *testing.T) {
	now := time.Date(2026, 3, 10, 14, 0, 0, 0, time.Local)
	for _, gap := range []int{0, -5} {
		cfg := proactiveConfig()
		cfg.LLMProactiveMinGapMin = gap
		d := decideProactive(cfg, idleTimer, now, now.Add(-time.Minute), now.Add(-time.Minute),
			false, false, false, true, false, false)
		if d.Fire {
			t.Errorf("gap %d: spoke 1 minute after the last remark", gap)
		}
	}
}

// TestProactiveReplyIsPersistedWithItsFlag is what the popup and the panel both
// read to label a reply the pet started.
//
// The instruction that produced the remark is NEVER persisted (it is a system
// prompt, not something the user typed), so the flag on the REPLY is the only
// record that the round was unprompted. Losing it makes the panel show the pet
// opening a conversation for no visible reason.
func TestProactiveReplyIsPersistedWithItsFlag(t *testing.T) {
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}

	if _, err := s.AppendChat(store.RoleUser, "我在做什么？", false); err != nil {
		t.Fatalf("append question: %v", err)
	}
	if _, err := s.AppendChatMessage(store.ChatMessage{
		Role:      store.RoleAssistant,
		Text:      "在忙什么呀？",
		Proactive: true,
	}); err != nil {
		t.Fatalf("append proactive reply: %v", err)
	}

	hist := s.ChatHistory()
	if len(hist) != 2 {
		t.Fatalf("history has %d entries, want 2", len(hist))
	}
	if !hist[1].Proactive {
		t.Error("the proactive flag did not survive persistence")
	}
	if hist[0].Proactive {
		t.Error("a user message was marked proactive")
	}

	// And it must survive a reload, because the panel renders it after a restart.
	reloaded, err := store.New(s.Dir())
	if err != nil {
		t.Fatalf("store.New (reload): %v", err)
	}
	again := reloaded.ChatHistory()
	if len(again) != 2 || !again[1].Proactive {
		t.Errorf("the flag did not survive a reload: %+v", again)
	}
}
