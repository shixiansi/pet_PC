package main

// The pomodoro state machine now lives in internal/pomodoro.
//
// WHY THE MOVE: the transitions are pure functions over (state, config, now) and
// never touched this package's window, timers or store wiring - they were only
// here because that is where they were first written. Moving them makes the
// dependency surface a fact of the import graph (internal/pomodoro imports
// internal/store and nothing else) instead of a convention someone has to keep
// remembering.
//
// WHY THIS FILE STILL EXISTS: it is the seam of an incremental move, not a
// permanent layer. ~110 call sites across the app layer and five test files keep
// their old names, so this step changes no behaviour and no semantics - it only
// relocates the logic. The aliases below are what make that true: phaseOutcome
// is an ALIAS, so composite literals, field access and the Finished method all
// keep working unchanged.
//
// IT IS MEANT TO BE DELETED. openspec/changes/pomodoro-state-machine-extraction
// lists the follow-up: move the pure tests into internal/pomodoro, then rewrite
// the call sites as pomodoro.StartFocus etc. and remove this file. Until then,
// add nothing here beyond a forward for a name this package still uses - logic
// belongs in the package, not in the seam.

import (
	"time"

	"PetPC/internal/pomodoro"
	"PetPC/internal/store"
)

// phaseOutcome is the package's name for what a transition produced.
//
// An alias rather than a defined type, deliberately: the app layer and the
// existing tests construct these values and read their fields, and a defined
// type would have meant touching every one of those sites in the same commit as
// the extraction.
type phaseOutcome = pomodoro.PhaseOutcome

// Remaining returns how long is left in the current phase.
func Remaining(st store.PomodoroState, now time.Time) time.Duration {
	return pomodoro.Remaining(st, now)
}

// startFocus begins a focus phase for the given todo.
func startFocus(st store.PomodoroState, cfg store.Config, todoID, todoTitle string, now time.Time) phaseOutcome {
	return pomodoro.StartFocus(st, cfg, todoID, todoTitle, now)
}

// pause halts the countdown, keeping the remainder.
func pause(st store.PomodoroState, now time.Time) phaseOutcome {
	return pomodoro.Pause(st, now)
}

// resume restarts a paused or stopped phase from where it stopped.
func resume(st store.PomodoroState, cfg store.Config, now time.Time) phaseOutcome {
	return pomodoro.Resume(st, cfg, now)
}

// stop halts the countdown like pause does, but records that the user asked for it.
func stop(st store.PomodoroState, now time.Time) phaseOutcome {
	return pomodoro.Stop(st, now)
}

// reset abandons everything: no phase, no association, no banked time.
func reset(st store.PomodoroState) phaseOutcome {
	return pomodoro.Reset(st)
}

// finishPhase ends the current phase and moves to the next one.
func finishPhase(st store.PomodoroState, cfg store.Config, endedAt, now time.Time, missed bool) phaseOutcome {
	return pomodoro.FinishPhase(st, cfg, endedAt, now, missed)
}

// skipPhase abandons the current phase without recording it.
func skipPhase(st store.PomodoroState, cfg store.Config, now time.Time) phaseOutcome {
	return pomodoro.SkipPhase(st, cfg, now)
}

// shouldAskPlan reports whether a transition must stop and ask what is next.
func shouldAskPlan(out phaseOutcome, restored, fullscreen bool) bool {
	return pomodoro.ShouldAskPlan(out, restored, fullscreen)
}

// reconcile repairs a state loaded from disk and decides whether a phase ran out
// while the pet was not running.
func reconcile(st store.PomodoroState, cfg store.Config, now time.Time) phaseOutcome {
	return pomodoro.Reconcile(st, cfg, now)
}

// hasPendingSummary reports whether a finished focus session still waits for its
// summary.
func hasPendingSummary(st store.PomodoroState) bool {
	return pomodoro.HasPendingSummary(st)
}

// phaseLabel names a phase for the interface.
func phaseLabel(phase string) string {
	return pomodoro.PhaseLabel(phase)
}

// ceilSeconds rounds a duration up to whole seconds.
func ceilSeconds(d time.Duration) int {
	return pomodoro.CeilSeconds(d)
}

// formatSeconds renders whole seconds as MM:SS.
func formatSeconds(secs int) string {
	return pomodoro.FormatSeconds(secs)
}

// formatCountdown renders a duration as MM:SS, rounding up.
func formatCountdown(d time.Duration) string {
	return pomodoro.FormatCountdown(d)
}
