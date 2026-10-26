package main

import (
	"testing"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/store"
)

// The plan gate: a break that runs out in real time stops and asks what the next
// focus session is about, and the focus does not start counting until that
// question is answered (design.md 1.5, 2.2; docs/reminder-pomodoro.md 4.5.2).
//
// Everything here is windowless: newLLMTestAppWithEvents gives a store and an
// event collector but no native window, so the geometry call a bubble makes
// fails and is logged - exactly as the existing gate tests tolerate. The
// full-screen probe is the one place a window would otherwise be needed, which
// is why App carries an injection point for it.

// --- helpers ---------------------------------------------------------------

// planGateApp builds the App every test here drives.
//
// The sound and the voice are switched off deliberately: no criterion in this
// file is about them, and leaving them on would make the tests depend on the
// machine's audio device rather than on the gate.
func planGateApp(t *testing.T, tune func(*store.Config)) *App {
	t.Helper()
	a, _ := newLLMTestAppWithEvents(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.AutoStartNextPhase = true
		c.FocusMinutes = 25
		c.ShortBreakMinutes = 5
		c.PomodoroSound = false
		if tune != nil {
			tune(c)
		}
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	return a
}

// stubFullscreen replaces the injected foreground-full-screen probe, which is
// the seam that makes the full-screen branch reachable without a window.
func stubFullscreen(a *App, full bool) {
	a.foregroundFullscreen = func(desktop.Window) bool { return full }
}

// expiredBreak is a break that is counting down and whose end time is already in
// the past: exactly the state the ticker hands to completePhaseLocked.
func expiredBreak(cfg store.Config, now time.Time) store.PomodoroState {
	end := now.Add(-time.Second)
	started := end.Add(-cfg.PhaseDuration(store.PhaseShortBreak))
	return store.PomodoroState{
		Enabled:        true,
		Phase:          store.PhaseShortBreak,
		RunState:       store.RunRunning,
		CompletedCount: 1,
		EndsAt:         &end,
		PhaseStartedAt: &started,
		NextTodoID:     "t1",
		NextTodoTitle:  "写周报",
	}
}

// runOutBreak drives one break to its end through the same pair of calls the
// ticker uses, under the lock the ticker holds.
//
// The lock is not incidental: completePhaseLocked is written for a caller that
// already owns pomoMu (design.md 2.2), so driving it unlocked would not be
// exercising the real path.
func runOutBreak(t *testing.T, a *App, st store.PomodoroState) {
	t.Helper()
	a.pomoMu.Lock()
	defer a.pomoMu.Unlock()
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	cfg := a.store.Config()
	now := time.Now()
	a.completePhase(finishPhase(st, cfg, *st.EndsAt, now, false), false)
}

// presetPlanGate parks the timer on the plan question: the state a break that
// has just run out leaves behind.
func presetPlanGate(t *testing.T, a *App, nextTodoID, nextTodoTitle string) {
	t.Helper()
	st := a.store.Pomodoro()
	st.Enabled = true
	st.Phase = store.PhaseFocus
	st.RunState = store.RunStopped
	st.EndsAt = nil
	st.PlanPending = true
	st.NextTodoID = nextTodoID
	st.NextTodoTitle = nextTodoTitle
	st.RemainingOnHalt = int(a.store.Config().PhaseDuration(store.PhaseFocus).Seconds())
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
}

// focusPlanOf reads the in-flight plan under the lock that guards it, the same
// contract every production read/write of the field obeys (T27).
func focusPlanOf(a *App) string {
	a.pomoMu.Lock()
	defer a.pomoMu.Unlock()
	return a.focusPlan
}

// seedFocusPlan writes the in-flight plan directly, under the same lock. It
// exists so "the no-op path left focusPlan alone" can be asserted against a
// non-empty starting value instead of against the zero one.
func seedFocusPlan(a *App, plan string) {
	a.pomoMu.Lock()
	defer a.pomoMu.Unlock()
	a.focusPlan = plan
}

// --- T4: shouldAskPlan, and the break that runs out ------------------------

// TestShouldAskPlanAsksOnlyForARestThatJustRanOut is the table for the one
// predicate that decides whether the question is asked at all.
//
// It is a pure function precisely so each of the four ways of NOT asking can be
// driven independently: a catch-up on launch, a full-screen window, a phase that
// was not a break, and a transition that does not lead into a focus.
func TestShouldAskPlanAsksOnlyForARestThatJustRanOut(t *testing.T) {
	cases := []struct {
		name       string
		out        phaseOutcome
		restored   bool
		fullscreen bool
		want       bool
	}{
		{
			name: "a short break that ran out, in real time",
			out:  phaseOutcome{EndedPhase: store.PhaseShortBreak, NextPhase: store.PhaseFocus},
			want: true,
		},
		{
			name: "a long break that ran out, in real time",
			out:  phaseOutcome{EndedPhase: store.PhaseLongBreak, NextPhase: store.PhaseFocus},
			want: true,
		},
		{
			name:     "the same break, but caught up on launch",
			out:      phaseOutcome{EndedPhase: store.PhaseShortBreak, NextPhase: store.PhaseFocus},
			restored: true,
			want:     false,
		},
		{
			name:       "the same break, but under a full-screen window",
			out:        phaseOutcome{EndedPhase: store.PhaseShortBreak, NextPhase: store.PhaseFocus},
			fullscreen: true,
			want:       false,
		},
		{
			name: "a focus that ran out",
			out:  phaseOutcome{EndedPhase: store.PhaseFocus, NextPhase: store.PhaseShortBreak},
			want: false,
		},
		{
			name: "a focus that ran out into another focus",
			out:  phaseOutcome{EndedPhase: store.PhaseFocus, NextPhase: store.PhaseFocus},
			want: false,
		},
		{
			name: "a break that did not lead into a focus",
			out:  phaseOutcome{EndedPhase: store.PhaseShortBreak, NextPhase: store.PhaseShortBreak},
			want: false,
		},
		{
			name: "nothing finished at all",
			out:  phaseOutcome{},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAskPlan(tc.out, tc.restored, tc.fullscreen); got != tc.want {
				t.Errorf("shouldAskPlan(%+v, restored=%v, fullscreen=%v) = %v, want %v",
					tc.out, tc.restored, tc.fullscreen, got, tc.want)
			}
		})
	}
}

// TestABreakThatRunsOutAsksForTheNextPlan is the behaviour the user asked for:
// the break rolling out stops the timer and asks what the next focus is about,
// instead of silently starting the next focus.
func TestABreakThatRunsOutAsksForTheNextPlan(t *testing.T) {
	a := planGateApp(t, nil)
	stubFullscreen(a, false)
	cfg := a.store.Config()

	runOutBreak(t, a, expiredBreak(cfg, time.Now()))

	got := a.store.Pomodoro()
	if !got.PlanPending {
		t.Fatalf("the break ended without asking what to focus on next: %+v", got)
	}
	if got.Phase != store.PhaseFocus {
		t.Errorf("phase = %q, want focus", got.Phase)
	}
	if got.RunState != store.RunStopped {
		t.Errorf("runState = %q, want stopped while the plan is unanswered", got.RunState)
	}
	if got.EndsAt != nil {
		t.Errorf("a parked focus must have no end time, got %v", got.EndsAt)
	}
	if want := int(cfg.PhaseDuration(store.PhaseFocus).Seconds()); got.RemainingOnHalt != want {
		t.Errorf("remainingOnHalt = %d, want the full focus length %d", got.RemainingOnHalt, want)
	}

	showing, advertised := a.bubbleState()
	if showing == nil || showing.Kind != BubblePlan {
		t.Fatalf("the plan question was not raised: %+v", showing)
	}
	if advertised == nil || advertised.Kind != BubblePlan {
		t.Errorf("the layout does not advertise the plan question: %+v", advertised)
	}
}

// TestThePlanQuestionIsAskedOnlyOnce is the idempotence the ticker depends on:
// the parked state has no end time, so the next tick advances nothing - and must
// not re-ask or disturb what is up.
func TestThePlanQuestionIsAskedOnlyOnce(t *testing.T) {
	a := planGateApp(t, nil)
	stubFullscreen(a, false)
	cfg := a.store.Config()

	runOutBreak(t, a, expiredBreak(cfg, time.Now()))
	before := a.store.Pomodoro()
	showingBefore, advertisedBefore := a.bubbleState()
	if !before.PlanPending || showingBefore == nil {
		t.Fatalf("the gate was not up to begin with: state=%+v bubble=%+v", before, showingBefore)
	}

	a.tickPomodoro()

	after := a.store.Pomodoro()
	if after != before {
		t.Errorf("a tick changed the parked state:\n after = %+v\nbefore = %+v", after, before)
	}
	if !after.PlanPending {
		t.Error("a tick answered the plan question")
	}
	if after.RunState != store.RunStopped {
		t.Errorf("runState = %q, want stopped", after.RunState)
	}
	if after.EndsAt != nil {
		t.Errorf("a tick gave the parked focus an end time: %v", after.EndsAt)
	}
	if after.RemainingOnHalt != int(cfg.PhaseDuration(store.PhaseFocus).Seconds()) {
		t.Errorf("remainingOnHalt = %d, want the full focus length", after.RemainingOnHalt)
	}

	showing, advertised := a.bubbleState()
	if showing == nil || showing.Kind != BubblePlan {
		t.Errorf("the question disappeared on a tick: %+v", showing)
	}
	if advertised == nil || advertised.Kind != BubblePlan {
		t.Errorf("the tick took the question out of the layout: %+v", advertised)
	}
	// Content-wise identical, so nothing was re-assembled behind the user's back.
	if showingBefore.Title != showing.Title || showingBefore.Plan != showing.Plan || showingBefore.TodoTitle != showing.TodoTitle {
		t.Errorf("the question was rebuilt on a tick: %+v -> %+v", showingBefore, showing)
	}
	if advertisedBefore == nil {
		t.Error("the layout lost the question before the tick")
	}
}

// TestThePlanIsAskedForEvenWhenPhasesDoNotChain pins the design decision that the
// gate does NOT depend on AutoStartNextPhase: both paths into a focus have to ask
// once, and the switch only decides whether the engine leaves the next phase
// running or stopped. With the switch off the engine has already parked the focus,
// so the gate adds nothing but the question.
func TestThePlanIsAskedForEvenWhenPhasesDoNotChain(t *testing.T) {
	a := planGateApp(t, func(c *store.Config) { c.AutoStartNextPhase = false })
	stubFullscreen(a, false)
	cfg := a.store.Config()

	runOutBreak(t, a, expiredBreak(cfg, time.Now()))

	got := a.store.Pomodoro()
	if !got.PlanPending {
		t.Fatalf("the plan was not asked for with AutoStartNextPhase off: %+v", got)
	}
	if got.RunState != store.RunStopped {
		t.Errorf("runState = %q, want stopped", got.RunState)
	}
	if got.EndsAt != nil {
		t.Errorf("a parked focus must have no end time, got %v", got.EndsAt)
	}
	if want := int(cfg.PhaseDuration(store.PhaseFocus).Seconds()); got.RemainingOnHalt != want {
		t.Errorf("remainingOnHalt = %d, want the full focus length %d", got.RemainingOnHalt, want)
	}
	showing, _ := a.bubbleState()
	if showing == nil || showing.Kind != BubblePlan {
		t.Fatalf("the plan question was not raised: %+v", showing)
	}
}

// --- T5: the full-screen silent path --------------------------------------

// TestAFullScreenBreakEndAsksNothing is docs 4.7: a bubble over a film or a game
// is worse than a missed question, so the focus simply starts and nothing is
// asked. The two states are independently driveable precisely because the probe
// is injected.
func TestAFullScreenBreakEndAsksNothing(t *testing.T) {
	a := planGateApp(t, nil)
	stubFullscreen(a, true)

	runOutBreak(t, a, expiredBreak(a.store.Config(), time.Now()))

	got := a.store.Pomodoro()
	if got.PlanPending {
		t.Error("the plan question was raised over a full-screen window")
	}
	if got.RunState != store.RunRunning {
		t.Errorf("runState = %q, want the focus running", got.RunState)
	}
	if got.EndsAt == nil {
		t.Fatal("the focus did not start under a full-screen window")
	}
	if got.RemainingOnHalt != 0 {
		t.Errorf("remainingOnHalt = %d, want 0 for a running phase", got.RemainingOnHalt)
	}
	showing, advertised := a.bubbleState()
	if showing != nil {
		t.Errorf("a bubble was raised over a full-screen window: %+v", showing)
	}
	if advertised != nil {
		t.Errorf("the layout advertises a bubble over a full-screen window: %+v", advertised)
	}

	// With the question never asked there is nothing to catch up on later: the
	// panel closing (which re-raises an unanswered plan) and the pet being shown
	// must not conjure one up.
	if _, err := a.SetPanelOpen(false); err != nil {
		t.Logf("SetPanelOpen (no window): %v", err)
	}
	if _, err := a.ShowPetForUser(); err != nil {
		t.Logf("ShowPetForUser (no window): %v", err)
	}
	showing, advertised = a.bubbleState()
	if showing != nil {
		t.Errorf("a late plan question appeared after a full-screen break end: %+v", showing)
	}
	if advertised != nil {
		t.Errorf("the layout advertises a late plan question: %+v", advertised)
	}
	if got := a.store.Pomodoro(); got.PlanPending {
		t.Errorf("the full-screen path left a gate behind: %+v", got)
	}

	// The same break end with the probe answering "no" DOES ask - the contrast
	// that proves the stub, and not something else, decided the outcome.
	stubFullscreen(a, false)
	runOutBreak(t, a, expiredBreak(a.store.Config(), time.Now()))
	showing, advertised = a.bubbleState()
	if showing == nil || showing.Kind != BubblePlan {
		t.Fatalf("the non-full-screen break end asked nothing: %+v", showing)
	}
	if advertised == nil || advertised.Kind != BubblePlan {
		t.Errorf("the layout does not advertise the question: %+v", advertised)
	}
	if got := a.store.Pomodoro(); !got.PlanPending || got.RunState != store.RunStopped {
		t.Errorf("the non-full-screen break end was not gated: %+v", got)
	}
}

// TestTheFullScreenProbeFallsBackWhenUnset pins the nil fallback: a real App
// never has to set the field (NewApp does), but an App built without it must
// still get a real answer rather than a nil dereference.
//
// The expectation is derived from the production helper rather than hard-coded,
// because on a machine whose foreground window really is full screen the honest
// answer is "do not ask" - what is under test is the fallback, not the desktop.
func TestTheFullScreenProbeFallsBackWhenUnset(t *testing.T) {
	a := planGateApp(t, nil)
	a.foregroundFullscreen = nil

	runOutBreak(t, a, expiredBreak(a.store.Config(), time.Now()))

	wantGated := !desktop.IsForegroundFullscreen(a.windowHandle())
	if got := a.store.Pomodoro(); got.PlanPending != wantGated {
		t.Errorf("planPending = %v, want %v (the production probe says fullscreen=%v)",
			got.PlanPending, wantGated, !wantGated)
	}
}

// --- T6: the catch-up paths never ask -------------------------------------

// TestARestartCatchUpDoesNotAskForAPlan is the complement of the gate: a break
// that elapsed while the pet was closed is completed on launch, and the focus
// starts straight away. Asking there would be asking about a session that began
// half an hour ago.
func TestARestartCatchUpDoesNotAskForAPlan(t *testing.T) {
	a := planGateApp(t, nil)
	if err := a.store.SavePomodoro(expiredBreak(a.store.Config(), time.Now())); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}

	// A fresh App over the same store, as a relaunch would be.
	restarted := &App{store: a.store}
	restarted.ctx = a.ctx
	restarted.emitFn = a.emitFn
	stubFullscreen(restarted, false)

	restarted.initPomodoro()

	got := a.store.Pomodoro()
	if got.PlanPending {
		t.Error("the launch catch-up asked what the next focus is about")
	}
	if got.RunState != store.RunRunning {
		t.Errorf("runState = %q, want the focus running", got.RunState)
	}
	if got.EndsAt == nil {
		t.Fatal("the catch-up did not start the focus")
	}
	showing, advertised := restarted.bubbleState()
	if showing != nil {
		t.Errorf("the catch-up raised a bubble: %+v", showing)
	}
	if advertised != nil {
		t.Errorf("the layout advertises a bubble after the catch-up: %+v", advertised)
	}
	if recs := a.store.PomodoroRecords(); len(recs) != 0 {
		t.Errorf("the catch-up wrote a record it should not have: %+v", recs)
	}
}

// TestSwitchingTheTimerOnDoesNotAskForAPlan is the same restored path reached
// through SetPomodoroEnabled, plus the literal off-then-on sequence the spec card
// asks about: neither may leave a plan gate behind.
func TestSwitchingTheTimerOnDoesNotAskForAPlan(t *testing.T) {
	// Switching ON over an elapsed break takes the catch-up path, exactly like a
	// launch.
	a := planGateApp(t, nil)
	stubFullscreen(a, false)
	if err := a.store.SavePomodoro(expiredBreak(a.store.Config(), time.Now())); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	if _, err := a.SetPomodoroEnabled(true); err != nil {
		t.Fatalf("SetPomodoroEnabled(true): %v", err)
	}
	got := a.store.Pomodoro()
	if got.PlanPending {
		t.Error("the catch-up on switching the timer on asked for a plan")
	}
	if got.RunState != store.RunRunning || got.EndsAt == nil {
		t.Errorf("the catch-up did not start the focus: %+v", got)
	}
	if showing, _ := a.bubbleState(); showing != nil {
		t.Errorf("switching the timer on raised a bubble: %+v", showing)
	}
	if recs := a.store.PomodoroRecords(); len(recs) != 0 {
		t.Errorf("the catch-up wrote a record it should not have: %+v", recs)
	}

	// The literal isomorphism: off, then on again.
	b := planGateApp(t, nil)
	stubFullscreen(b, false)
	if err := b.store.SavePomodoro(expiredBreak(b.store.Config(), time.Now())); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	if _, err := b.SetPomodoroEnabled(false); err != nil {
		t.Fatalf("SetPomodoroEnabled(false): %v", err)
	}
	if b.store.Pomodoro().PlanPending {
		t.Error("switching the timer off set a plan gate")
	}
	if _, err := b.SetPomodoroEnabled(true); err != nil {
		t.Fatalf("SetPomodoroEnabled(true): %v", err)
	}
	if b.store.Pomodoro().PlanPending {
		t.Error("switching the timer back on left a plan gate behind")
	}
	if showing, _ := b.bubbleState(); showing != nil {
		t.Errorf("switching the timer back on raised a bubble: %+v", showing)
	}
}

// --- T12: answering the question starts the focus -------------------------

// TestStartPomodoroWithPlanStartsTheFocusOnce is the whole terminal state of one
// answer: the carried todo moves into the session, the gate clears, and the
// countdown starts from the moment of the answer.
func TestStartPomodoroWithPlanStartsTheFocusOnce(t *testing.T) {
	a := planGateApp(t, nil)
	presetPlanGate(t, a, "t1", "写周报")
	// The question is on screen, as it would be when the user answers it.
	a.showPlanBubble()
	if showing, _ := a.bubbleState(); showing == nil || showing.Kind != BubblePlan {
		t.Fatalf("the plan question was not up: %+v", showing)
	}

	before := time.Now()
	if _, err := a.StartPomodoroWithPlan("整理接口"); err != nil {
		t.Fatalf("StartPomodoroWithPlan: %v", err)
	}
	after := time.Now()

	got := a.store.Pomodoro()
	if got.PlanPending {
		t.Error("answering the plan left the gate up")
	}
	if got.NextTodoID != "" || got.NextTodoTitle != "" {
		t.Errorf("the carried todo was not consumed: %+v", got)
	}
	if got.CurrentTodoID != "t1" || got.CurrentTodoTitle != "写周报" {
		t.Errorf("the carried todo did not become the session's: %+v", got)
	}
	if got.Phase != store.PhaseFocus {
		t.Errorf("phase = %q, want focus", got.Phase)
	}
	if got.RunState != store.RunRunning {
		t.Errorf("runState = %q, want running", got.RunState)
	}
	if got.RemainingOnHalt != 0 {
		t.Errorf("remainingOnHalt = %d, want 0 for a running phase", got.RemainingOnHalt)
	}
	if got.PhaseStartedAt == nil {
		t.Error("the started focus has no start time")
	}
	if got.EndsAt == nil {
		t.Fatal("the focus has no end time")
	}
	focus := time.Duration(a.store.Config().FocusMinutes) * time.Minute
	if got.EndsAt.Before(before.Add(focus)) || got.EndsAt.After(after.Add(focus)) {
		t.Errorf("focus endsAt = %v, want between %v and %v",
			got.EndsAt, before.Add(focus), after.Add(focus))
	}
	if plan := focusPlanOf(a); plan != "整理接口" {
		t.Errorf("focusPlan = %q, want %q", plan, "整理接口")
	}
	showing, advertised := a.bubbleState()
	if showing != nil {
		t.Errorf("the question is still on screen after being answered: %+v", showing)
	}
	if advertised != nil {
		t.Errorf("the layout still advertises the answered question: %+v", advertised)
	}
}

// TestStartPomodoroWithPlanAcceptsAnEmptyPlan: "skip" is the same path with an
// empty string, which is what makes the question answerable without typing.
func TestStartPomodoroWithPlanAcceptsAnEmptyPlan(t *testing.T) {
	a := planGateApp(t, nil)
	presetPlanGate(t, a, "", "")

	if _, err := a.StartPomodoroWithPlan(""); err != nil {
		t.Fatalf("StartPomodoroWithPlan(\"\"): %v", err)
	}

	got := a.store.Pomodoro()
	if got.PlanPending {
		t.Error("skipping left the gate up")
	}
	if got.Phase != store.PhaseFocus || got.RunState != store.RunRunning {
		t.Errorf("skipping did not start the focus: %+v", got)
	}
	if got.EndsAt == nil {
		t.Fatal("skipping did not give the focus an end time")
	}
	if got.RemainingOnHalt != 0 {
		t.Errorf("remainingOnHalt = %d, want 0", got.RemainingOnHalt)
	}
	if plan := focusPlanOf(a); plan != "" {
		t.Errorf("focusPlan = %q, want empty for a skip", plan)
	}
}

// TestStartPomodoroWithPlanIsANoOpOffTheGate is the idempotence half: with
// nothing waiting, the command must not start a session the user never asked
// for, and must not touch the state or the in-flight plan at all.
func TestStartPomodoroWithPlanIsANoOpOffTheGate(t *testing.T) {
	a := planGateApp(t, nil)

	// A focus the user stopped by hand: recoverable, but not waiting on a plan.
	st := a.store.Pomodoro()
	st.Enabled = true
	st.Phase = store.PhaseFocus
	st.RunState = store.RunStopped
	st.RemainingOnHalt = 600
	st.CurrentTodoID = "t1"
	st.CurrentTodoTitle = "写周报"
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	seedFocusPlan(a, "上一个计划")
	before := a.store.Pomodoro()

	if _, err := a.StartPomodoroWithPlan("x"); err != nil {
		t.Fatalf("StartPomodoroWithPlan: %v", err)
	}

	after := a.store.Pomodoro()
	if after != before {
		t.Errorf("the no-op path changed the state:\n after = %+v\nbefore = %+v", after, before)
	}
	if plan := focusPlanOf(a); plan != "上一个计划" {
		t.Errorf("the no-op path consumed the in-flight plan: %q", plan)
	}
	if showing, _ := a.bubbleState(); showing != nil {
		t.Errorf("the no-op path raised a bubble: %+v", showing)
	}
}

// --- T15: the plan travels into the record --------------------------------

// TestThePlanIsWrittenToTheRecord is the point of the whole feature: the session
// is archived with what the user set out to do before it started.
func TestThePlanIsWrittenToTheRecord(t *testing.T) {
	a := planGateApp(t, nil)
	presetPlanGate(t, a, "t1", "写周报")
	if _, err := a.StartPomodoroWithPlan("整理接口"); err != nil {
		t.Fatalf("StartPomodoroWithPlan: %v", err)
	}

	finishFocusSession(t, a)

	recs := a.store.PomodoroRecords()
	if len(recs) != 1 {
		t.Fatalf("got %d records, want exactly 1", len(recs))
	}
	if recs[0].Plan != "整理接口" {
		t.Errorf("record plan = %q, want %q", recs[0].Plan, "整理接口")
	}
	// The plan is consumed: it belongs to the session that just ended, not to
	// whatever comes next (design.md 1.4).
	if plan := focusPlanOf(a); plan != "" {
		t.Errorf("focusPlan = %q, want it consumed by the record", plan)
	}
}

// TestSkippingThePlanStillWritesARecord: an empty plan is a legitimate answer,
// and the session is still archived - the record is what makes the session real.
func TestSkippingThePlanStillWritesARecord(t *testing.T) {
	a := planGateApp(t, nil)
	presetPlanGate(t, a, "t1", "写周报")
	if _, err := a.StartPomodoroWithPlan(""); err != nil {
		t.Fatalf("StartPomodoroWithPlan(\"\"): %v", err)
	}

	finishFocusSession(t, a)

	recs := a.store.PomodoroRecords()
	if len(recs) != 1 {
		t.Fatalf("got %d records, want exactly 1 (a skip still records the session)", len(recs))
	}
	if recs[0].Plan != "" {
		t.Errorf("record plan = %q, want empty for a skip", recs[0].Plan)
	}
	if plan := focusPlanOf(a); plan != "" {
		t.Errorf("focusPlan = %q, want empty", plan)
	}
}

// TestAFocusWithNoPlanInFlightStillRecords is the tolerance T27 turns into a
// requirement: the in-flight plan is memory only, so a focus that outlives a
// restart has none - and that must degrade to an empty Plan, not to a missing
// record or an error.
func TestAFocusWithNoPlanInFlightStillRecords(t *testing.T) {
	a := planGateApp(t, nil)
	now := time.Now()
	cfg := a.store.Config()
	st := startFocus(enabledIdle(), cfg, "t1", "写周报", now).State
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	if plan := focusPlanOf(a); plan != "" {
		t.Fatalf("this test needs an empty in-flight plan, got %q", plan)
	}

	finishFocusSession(t, a)

	recs := a.store.PomodoroRecords()
	if len(recs) != 1 {
		t.Fatalf("got %d records, want exactly 1", len(recs))
	}
	if recs[0].Plan != "" {
		t.Errorf("record plan = %q, want empty when no plan was in flight", recs[0].Plan)
	}
	if recs[0].TodoID != "t1" {
		t.Errorf("the session lost its todo: %+v", recs[0])
	}
}

// finishFocusSession drives the focus that is currently in flight to its end, the
// way the ticker would when it runs out - under pomoMu, as the ticker holds it.
func finishFocusSession(t *testing.T, a *App) {
	t.Helper()
	a.pomoMu.Lock()
	defer a.pomoMu.Unlock()
	st := a.store.Pomodoro()
	if !st.IsRunning() || st.EndsAt == nil {
		t.Fatalf("there is no running focus to finish: %+v", st)
	}
	cfg := a.store.Config()
	a.completePhase(finishPhase(st, cfg, *st.EndsAt, time.Now(), false), false)
}

// --- T16: the summary bubble shows the plan -------------------------------

// TestSummaryBubbleShowsThePlan: the question about what was actually done comes
// with the reminder of what the user set out to do, taken from the RECORD (so it
// survives a restart) rather than from memory.
func TestSummaryBubbleShowsThePlan(t *testing.T) {
	a := planGateApp(t, nil)
	rec := pendingSummary(t, a, "整理接口")

	a.showSummaryBubble()

	showing, advertised := a.bubbleState()
	if showing == nil || showing.Kind != BubbleSummary {
		t.Fatalf("the summary prompt was not raised: %+v", showing)
	}
	if showing.Plan != "整理接口" {
		t.Errorf("bubble plan = %q, want the record's plan %q", showing.Plan, "整理接口")
	}
	if showing.RecordID != rec {
		t.Errorf("the prompt edits %q, want %q", showing.RecordID, rec)
	}
	if advertised == nil || advertised.Kind != BubbleSummary {
		t.Errorf("the layout does not advertise the prompt: %+v", advertised)
	}
}

// TestSummaryBubbleWithNoPlanLeavesTheLineEmpty is the other half: a session with
// no plan (a skip, or a session that outlived a restart) must not borrow the
// previous one - the hint line is simply empty, and the frontend hides it.
func TestSummaryBubbleWithNoPlanLeavesTheLineEmpty(t *testing.T) {
	a := planGateApp(t, nil)
	pendingSummary(t, a, "")

	a.showSummaryBubble()

	showing, _ := a.bubbleState()
	if showing == nil || showing.Kind != BubbleSummary {
		t.Fatalf("the summary prompt was not raised: %+v", showing)
	}
	if showing.Plan != "" {
		t.Errorf("bubble plan = %q, want empty for a record with no plan", showing.Plan)
	}
}

// pendingSummary archives one finished session and parks the timer on it, then
// returns the record's id.
func pendingSummary(t *testing.T, a *App, plan string) string {
	t.Helper()
	now := time.Now()
	rec, err := a.store.AddPomodoroRecord(store.PomodoroRecord{
		TodoID: "t1", TodoTitle: "写周报",
		StartedAt: now.Add(-25 * time.Minute), EndedAt: now, FocusMinutes: 25, Plan: plan,
	})
	if err != nil {
		t.Fatalf("AddPomodoroRecord: %v", err)
	}
	st := a.store.Pomodoro()
	st.Enabled = true
	st.Phase = store.PhaseShortBreak
	st.RunState = store.RunRunning
	end := now.Add(5 * time.Minute)
	st.EndsAt = &end
	st.PendingRecordID = rec.ID
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	return rec.ID
}
