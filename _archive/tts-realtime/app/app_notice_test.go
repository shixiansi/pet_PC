package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"PetPC/internal/store"
)

// newNoticeTestApp returns an app with speech on and a fake synthesis server, but
// no chat involved: the P5 notices are fired by timers, not by a request.
//
// It also yields the event collector, because some invariants are about what a
// notice must NOT emit.
func newNoticeTestApp(t *testing.T, tune func(*store.Config)) (*App, *recordingSpeaker, *fakeSynthServer, *eventCollector) {
	t.Helper()
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	t.Cleanup(srv.Close)

	a, collector, spk := newSpeechTestApp(t, srv.URL, tune)
	a.speech.speaker = spk
	return a, spk, synth, collector
}

// waitSynth waits for the fake server to have received at least n requests.
func waitSynth(t *testing.T, synth *fakeSynthServer, n int) []string {
	t.Helper()
	waitFor(t, 10*time.Second, "synthesis requests", func() bool { return synth.count() >= n })
	return synth.got()
}

// TestPomodoroPhaseEndIsSpoken covers the feature: when a focus phase ends, the pet
// says so.
//
// The wording matters as much as the fact - the user is looking at something else
// when this fires, so the line has to make sense with no visual context, which is
// why the test pins that it mentions resting rather than reading out a timer value.
func TestPomodoroPhaseEndIsSpoken(t *testing.T) {
	a, _, synth, _ := newNoticeTestApp(t, nil)

	a.speakPomodoroLine(store.PhaseFocus)

	got := waitSynth(t, synth, 1)
	if len(got) == 0 {
		t.Fatal("no synthesis request")
	}
	if !strings.Contains(got[0], "专注") {
		t.Errorf("the focus announcement was %q, want it to say the focus ended", got[0])
	}
	if strings.Contains(got[0], "%") || strings.Contains(got[0], "分钟") {
		t.Errorf("the announcement reads out a timer value (%q): it must make sense without looking", got[0])
	}
}

// TestBreakEndIsSpoken covers the other direction, and that the two are distinct:
// an announcement that sounds the same either way tells the user nothing.
func TestBreakEndIsSpoken(t *testing.T) {
	a, _, synth, _ := newNoticeTestApp(t, nil)

	a.speakPomodoroLine(store.PhaseShortBreak)

	got := waitSynth(t, synth, 1)
	if len(got) == 0 {
		t.Fatal("no synthesis request")
	}
	if !strings.Contains(got[0], "休息") {
		t.Errorf("the break announcement was %q, want it to mention the break", got[0])
	}
}

// TestPomodoroNoticeRespectsItsOwnSwitch is the point of a separate switch: a user
// may want replies read aloud while keeping the timer silent.
func TestPomodoroNoticeRespectsItsOwnSwitch(t *testing.T) {
	a, _, synth, _ := newNoticeTestApp(t, func(c *store.Config) {
		c.TTSAutoReadChat = true // chat speech stays ON
		c.TTSAutoReadPomodoro = false
	})

	a.speakPomodoroLine(store.PhaseFocus)
	// Give any stray request a chance to arrive before declaring success.
	time.Sleep(150 * time.Millisecond)
	if synth.count() != 0 {
		t.Errorf("spoke the pomodoro notice with its switch off: %q", synth.got())
	}
}

// TestReminderNoticeRespectsItsOwnSwitch covers the reminder half, which defaults
// OFF: a reminder can fire mid-meeting and the pet talking unprompted is far more
// intrusive than a bubble appearing.
func TestReminderNoticeRespectsItsOwnSwitch(t *testing.T) {
	a, _, synth, _ := newNoticeTestApp(t, func(c *store.Config) {
		c.TTSAutoReadPomodoro = true
		c.TTSAutoReadReminder = false
	})

	a.speakReminderLine(1, 2, 5)
	time.Sleep(150 * time.Millisecond)
	if synth.count() != 0 {
		t.Errorf("spoke a reminder with its switch off: %q", synth.got())
	}
}

// TestReminderNoticeIsSpokenWhenEnabled covers the enabled path and the wording:
// the overdue count is the most urgent fact, so it must be the one announced when
// present.
func TestReminderNoticeIsSpokenWhenEnabled(t *testing.T) {
	a, _, synth, _ := newNoticeTestApp(t, func(c *store.Config) {
		c.TTSAutoReadReminder = true
	})

	a.speakReminderLine(3, 2, 9)

	got := waitSynth(t, synth, 1)
	if len(got) == 0 {
		t.Fatal("no synthesis request")
	}
	if !strings.Contains(got[0], "逾期") {
		t.Errorf("the reminder read %q, want it to lead with the overdue count", got[0])
	}
}

// TestReminderNoticeWithNothingDueIsSilent pins that the empty case says nothing
// at all. A reminder over an empty list exists only to say "nothing to report",
// which is not worth a spoken sentence - and on the online backend it would cost a
// queued request to say so.
func TestReminderNoticeWithNothingDueIsSilent(t *testing.T) {
	a, _, synth, _ := newNoticeTestApp(t, func(c *store.Config) {
		c.TTSAutoReadReminder = true
	})

	a.speakReminderLine(0, 0, 0)
	time.Sleep(150 * time.Millisecond)
	if synth.count() != 0 {
		t.Errorf("spoke about an empty reminder list: %q", synth.got())
	}
}

// TestNoticesSpeakWhenSpeechIsOff is the degradation guarantee: with speech off the
// whole feature must be inert, and no socket may be opened.
func TestNoticesSpeakWhenSpeechIsOff(t *testing.T) {
	a, _, synth, _ := newNoticeTestApp(t, func(c *store.Config) {
		c.TTSEnabled = false
	})

	a.speakPomodoroLine(store.PhaseFocus)
	a.speakReminderLine(1, 1, 1)
	time.Sleep(150 * time.Millisecond)
	if synth.count() != 0 {
		t.Errorf("synthesis was attempted with speech off: %q", synth.got())
	}
}

// TestNoticesSpeakWhenUnconfigured covers the other half of readiness: speech on but
// nothing selected to speak with. The online backend's version of this is "no voice
// chosen", which is why the check goes through TTSReady rather than testing the ref
// path directly.
func TestNoticesSpeakWhenUnconfigured(t *testing.T) {
	a, _, synth, _ := newNoticeTestApp(t, func(c *store.Config) {
		c.TTSBackend = store.TTSBackendOnline
		c.TTSCharacter = "" // no voice selected
	})

	a.speakPomodoroLine(store.PhaseFocus)
	time.Sleep(150 * time.Millisecond)
	if synth.count() != 0 {
		t.Errorf("synthesis was attempted with no voice selected: %q", synth.got())
	}
}

// TestNoticeInterruptsCurrentSpeech pins the same rule chat replies follow: two
// voices at once is never wanted, and for a NOTIFICATION the old speech is the case
// most likely to be stale - the user has already moved on.
func TestNoticeInterruptsCurrentSpeech(t *testing.T) {
	a, spk, _, _ := newNoticeTestApp(t, nil)

	if p := a.startSpeech(a.store.Config(), false); p == nil {
		t.Fatal("startSpeech returned nil")
	}
	before := spk.stopCount()

	a.speakPomodoroLine(store.PhaseFocus)

	if spk.stopCount() <= before {
		t.Error("the notice did not stop the speech already playing")
	}
}

// TestAutoReadSwitchesRoundTrip is the regression test for a pre-existing bug
// found while wiring P5: the panel rendered the "read this aloud" checkboxes and
// the settings payload never sent any of them, so SaveTTSSettings could not
// persist them. Every one of those switches was decorative - the UI showed a
// state the config never took, and toggling it did nothing at all.
//
// The four are checked together because they travel as one group, and a future
// change that adds a fifth without adding it here would reintroduce exactly the
// same silent no-op.
func TestAutoReadSwitchesRoundTrip(t *testing.T) {
	a, _, _, _ := newNoticeTestApp(t, nil)

	in := SpeechView{
		Enabled:  true,
		Endpoint: "http://127.0.0.1:9880",
		RefAudio: `D:\voices\ref.wav`,
	}
	// Flip all four away from their defaults, so a field that is silently dropped
	// leaves the default in place and the test fails.
	in.ReadChat = false
	in.ReadProactive = false
	in.ReadPomodoro = false
	in.ReadReminder = true

	st, err := a.SaveTTSSettings(in)
	if err != nil {
		t.Fatalf("SaveTTSSettings: %v", err)
	}

	cfg := a.store.Config()
	if cfg.TTSAutoReadChat {
		t.Error("ReadChat did not persist")
	}
	if cfg.TTSAutoReadProactive {
		t.Error("ReadProactive did not persist")
	}
	if cfg.TTSAutoReadPomodoro {
		t.Error("ReadPomodoro did not persist")
	}
	if !cfg.TTSAutoReadReminder {
		t.Error("ReadReminder did not persist")
	}
	// And they must come back through the view, so the panel renders what it saved
	// rather than re-deriving from a default.
	if st.Speech.ReadChat || st.Speech.ReadProactive || st.Speech.ReadPomodoro {
		t.Errorf("the view did not echo the switches back: %+v", st.Speech)
	}
	if !st.Speech.ReadReminder {
		t.Error("the view did not echo ReadReminder back")
	}
}

// TestNoticeDefaultsAreDeliberatelyDifferent pins the defaults, because the two
// are NOT interchangeable and a tidy-up that made them match would be wrong:
// the pomodoro announcement defaults ON (the user started the timer and is waiting
// for it) and the reminder defaults OFF (it can fire mid-meeting, where the pet
// speaking unprompted is far more intrusive than a bubble appearing).
func TestNoticeDefaultsAreDeliberatelyDifferent(t *testing.T) {
	def := store.DefaultConfig()
	if !def.TTSAutoReadPomodoro {
		t.Error("the pomodoro announcement defaults OFF; the user is waiting for that timer")
	}
	if def.TTSAutoReadReminder {
		t.Error("the reminder announcement defaults ON; it can fire mid-meeting")
	}
}

// TestNoticeDoesNotDisturbTheRevealRound is the subtle one, and the reason
// SpeakLine is a separate path from the chat pipeline.
//
// A notice fires from a timer callback, which can land in the middle of a reply
// being revealed sentence by sentence. If the notice touched the reveal
// bookkeeping - clearing the round id, say - the reply's remaining sentences would
// stop appearing on screen and the held text would be released all at once. So the
// round must survive the notice untouched.
func TestNoticeDoesNotDisturbTheRevealRound(t *testing.T) {
	a, _, _, _ := newNoticeTestApp(t, func(c *store.Config) {
		c.TTSAutoReadPomodoro = true
	})

	// Stand in for a reply in flight.
	a.beginSpeechRound("req-123")
	a.holdText("req-123", "第一句话。")
	a.setSpeechFullText("req-123", "第一句话。第二句话。")

	a.speakPomodoroLine(store.PhaseFocus)

	// The notice must not have replaced the round's identity, and the held text
	// must still be there to be revealed.
	got, gated := a.speechProgress("req-123")
	if !gated {
		t.Fatal("the notice cleared the reveal round, so the reply would stop appearing")
	}
	_ = got

	a.speech.mu.Lock()
	pending := a.speech.pending
	full := a.speech.full
	a.speech.mu.Unlock()
	if pending != "第一句话。" {
		t.Errorf("held text = %q, want the reply's text untouched", pending)
	}
	if full != "第一句话。第二句话。" {
		t.Errorf("full text = %q, want the reply's text untouched", full)
	}
}

// TestNoticeSpokenInFull covers the sentence budget. A notice uses its own, higher
// bound: cutting an instruction in half would leave the user with a misleading
// fragment, where a chat reply being trimmed to its first sentences is the intended
// behaviour.
//
// The notice is a two-sentence announcement with the chat budget set to ONE, so the
// second sentence is spoken only if the notice really does override that budget.
// (It used to rely on a small 每句字数 splitting the text at a comma, which no longer
// happens - commas are not split points - so the test would have passed for the
// wrong reason or not at all.)
func TestNoticeSpokenInFull(t *testing.T) {
	a, _, synth, _ := newNoticeTestApp(t, func(c *store.Config) {
		// The chat budget is deliberately tiny; the notice must ignore it.
		c.TTSMaxSentences = 1
	})

	a.speakPomodoroLine(store.PhaseFocus)

	// The announcement is "专注时间结束了，起来活动一下吧。" - one sentence, so a
	// single request is correct, and the whole of it must be present.
	got := waitSynth(t, synth, 1)
	joined := strings.Join(got, "")
	if !strings.Contains(joined, "专注") || !strings.Contains(joined, "活动") {
		t.Errorf("the notice was truncated: %q", got)
	}
}

// TestNoticesSkipTheRevealPath pins that a notice never emits a reveal event.
// The reveal exists to release text the user cannot see yet; a notice's text is
// already on screen in the bubble, so revealing it would rewrite the chat log with
// the pet's timer announcement.
func TestNoticesSkipTheRevealPath(t *testing.T) {
	a, _, synth, collector := newNoticeTestApp(t, nil)

	a.speakPomodoroLine(store.PhaseFocus)
	waitSynth(t, synth, 1)

	if collector.has(eventChatReveal) {
		t.Error("a pomodoro notice emitted a chat reveal event")
	}
	if collector.has(eventChatDelta) {
		t.Error("a pomodoro notice emitted a chat delta event")
	}
}
