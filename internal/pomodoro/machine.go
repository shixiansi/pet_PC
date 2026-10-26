// Package pomodoro is the timer's state machine: pure transitions over
// (state, config, now) that report what happened instead of doing it.
//
// Nothing here touches a window, a clock or a store. Every externally visible
// effect a transition can produce - a record to archive, a phase that ended, a
// question that should be asked - is returned in the outcome, and the app layer
// decides what to do with it. That is what makes the whole machine testable
// without a desktop.
//
// The rules it encodes (why a finished focus waits for its record before the
// break starts, why the plan question is refused under a full-screen window, why
// a catch-up keeps the end time and the processing time apart) are documented on
// the individual transitions below and in docs/reminder-pomodoro.md.
package pomodoro

import (
	"fmt"
	"time"

	"PetPC/internal/store"
)

// The pomodoro state machine.
//
// The transitions are written as pure functions over (state, config, now) so the
// whole thing can be tested without a clock, a window or a store: the caller
// persists what they return. Everything the machine can produce that has an
// external effect - a record to archive, a sound to play, a bubble to show - is
// reported in the returned outcome rather than performed here.

// clockJumpSlack is how far past its own length a phase's end time may sit
// before the countdown is treated as nonsense.
//
// A running phase can never legitimately have more time left than the phase is
// long. If it does, the system clock was moved backwards while the pet was
// running, and the only sane response is to abandon the countdown rather than
// display an hour-long focus session (docs/reminder-pomodoro.md 9).
const clockJumpSlack = 2 * time.Minute

// PhaseOutcome is what a transition produced.
type PhaseOutcome struct {
	// State is the resulting state, ready to be persisted.
	State store.PomodoroState
	// Record is a finished focus session to archive, or nil. It is always
	// produced when a focus phase ends, even if the user will go on to skip the
	// summary: that is what makes "skip" unable to erase a session.
	Record *store.PomodoroRecord
	// EndedPhase is the phase that just finished ("" when nothing did).
	EndedPhase string
	// NextPhase is the phase that took over.
	NextPhase string
	// Missed is true when the phase ran out while the app was not watching, so
	// the caller can announce it once instead of silently jumping ahead.
	Missed bool
	// ClockJump is true when the countdown was abandoned because the clock moved
	// backwards.
	ClockJump bool
}

// finished reports whether a phase ended in this outcome.
func (o PhaseOutcome) Finished() bool { return o.EndedPhase != "" }

// Remaining returns how long is left in the current phase.
//
// A running phase counts down to its end time; a paused or stopped phase reports
// the remainder frozen at the moment it halted; an idle state has nothing left.
func Remaining(st store.PomodoroState, now time.Time) time.Duration {
	switch st.Phase {
	case store.PhaseIdle:
		return 0
	}
	if st.RunState == store.RunRunning && st.EndsAt != nil {
		d := st.EndsAt.Sub(now)
		if d < 0 {
			return 0
		}
		return d
	}
	return time.Duration(st.RemainingOnHalt) * time.Second
}

// StartFocus begins a focus phase for the given todo.
//
// todoID/todoTitle may both be empty: with nothing open to work on, the user is
// allowed to focus unattached rather than being locked out of the feature
// (docs/reminder-pomodoro.md 13.2). Selecting a todo never marks it done, and it
// stays in the list for the next session.
func StartFocus(st store.PomodoroState, cfg store.Config, todoID, todoTitle string, now time.Time) PhaseOutcome {
	st.Enabled = true
	st.Phase = store.PhaseFocus
	st.RunState = store.RunRunning
	end := now.Add(time.Duration(cfg.FocusMinutes) * time.Minute)
	st.EndsAt = &end
	st.PhaseStartedAt = &now
	st.RemainingOnHalt = 0
	st.CurrentTodoID = todoID
	st.CurrentTodoTitle = todoTitle
	return PhaseOutcome{State: st, NextPhase: store.PhaseFocus}
}

// Pause halts the countdown, keeping the remainder.
//
// A paused pomodoro is explicitly NOT a focusing one, so reminders are allowed
// to interrupt it again (docs/reminder-pomodoro.md 4.3).
func Pause(st store.PomodoroState, now time.Time) PhaseOutcome {
	if !st.IsRunning() {
		return PhaseOutcome{State: st}
	}
	st.RemainingOnHalt = int(Remaining(st, now).Seconds())
	st.EndsAt = nil
	st.RunState = store.RunPaused
	return PhaseOutcome{State: st}
}

// Resume restarts a paused or stopped phase from where it stopped.
func Resume(st store.PomodoroState, cfg store.Config, now time.Time) PhaseOutcome {
	if st.Phase == store.PhaseIdle || st.RunState == store.RunRunning {
		return PhaseOutcome{State: st}
	}
	remaining := time.Duration(st.RemainingOnHalt) * time.Second
	if remaining <= 0 {
		// Nothing was banked (a state written by an older build, say): fall back
		// to a full phase rather than resuming a zero-length one that would end
		// instantly.
		remaining = cfg.PhaseDuration(st.Phase)
	}
	end := now.Add(remaining)
	st.EndsAt = &end
	st.RunState = store.RunRunning
	st.RemainingOnHalt = 0
	if st.PhaseStartedAt == nil {
		st.PhaseStartedAt = &now
	}
	return PhaseOutcome{State: st, NextPhase: st.Phase}
}

// Stop halts the countdown like pause does, but records that the user asked for
// it. Stop is deliberately recoverable - resume picks the session back up - so
// the UI labels the pair "停止 / 继续" rather than "放弃"
// (docs/reminder-pomodoro.md 13.1).
func Stop(st store.PomodoroState, now time.Time) PhaseOutcome {
	out := Pause(st, now)
	if out.State.Phase == store.PhaseIdle {
		return out
	}
	out.State.RunState = store.RunStopped
	return out
}

// Reset abandons everything: no phase, no association, no banked time. This is
// the explicit escape hatch that stop deliberately is not
// (docs/reminder-pomodoro.md 13.1), so it keeps CompletedCount - the long-break
// cadence is a property of the day, not of the current session.
func Reset(st store.PomodoroState) PhaseOutcome {
	enabled := st.Enabled
	completed := st.CompletedCount
	return PhaseOutcome{State: store.PomodoroState{Enabled: enabled, Phase: store.PhaseIdle, CompletedCount: completed}}
}

// nextPhaseAfter returns the phase that follows the one that just ended.
//
// A focus phase is followed by a long break once the configured number of focus
// phases have been completed, and by a short break otherwise.
func nextPhaseAfter(st store.PomodoroState, cfg store.Config) string {
	if st.Phase != store.PhaseFocus {
		return store.PhaseFocus
	}
	every := cfg.LongBreakEvery
	if every < store.MinLongBreakEvery {
		every = store.DefaultLongBreakEvery
	}
	if st.CompletedCount > 0 && st.CompletedCount%every == 0 {
		return store.PhaseLongBreak
	}
	return store.PhaseShortBreak
}

// FinishPhase ends the current phase and moves to the next one.
//
// endedAt is when the phase actually ran out, and now is the wall clock at the
// moment it is being processed. The two differ exactly when the pet was not
// running: a phase can end at 11:35 while the app is only reopened at 14:00.
//
// Keeping them separate is what stops a catch-up from cascading. The record uses
// endedAt (so it is not stretched by however long the pet was closed), but the
// next phase starts at now. Deriving the next phase's end from endedAt instead
// would make it expire the instant it began, and on a long absence the pet would
// then invent a fresh focus record on every subsequent tick.
//
// A finished focus phase always produces a record, and the move to the rest is
// GATED on that record being answered.
//
// This reverses an earlier design, which started the break counting behind the
// summary bubble. The user asked for the gate: a session whose record has not been
// written should not silently bank a break, because the record is what makes the
// session real - and a break that runs while the question is unanswered is time the
// user never agreed to spend.
//
// So a focus that ends with AutoStartNextPhase on leaves the NEXT phase in place but
// STOPPED, with its full length banked, and SavePomodoroSummary (or a dismiss) is
// what starts it. AutoStartNextPhase still governs every other transition: a break
// that ends still runs straight into the next focus, because no record is involved.
func FinishPhase(st store.PomodoroState, cfg store.Config, endedAt, now time.Time, missed bool) PhaseOutcome {
	if st.Phase == store.PhaseIdle {
		return PhaseOutcome{State: st}
	}
	ended := st.Phase
	out := PhaseOutcome{EndedPhase: ended, Missed: missed}

	// Keep the phase duration before the phase field is overwritten.
	duration := cfg.PhaseDuration(ended)

	if ended == store.PhaseFocus {
		st.CompletedCount++
		started := endedAt.Add(-duration)
		if st.PhaseStartedAt != nil {
			started = *st.PhaseStartedAt
		}
		out.Record = &store.PomodoroRecord{
			TodoID:       st.CurrentTodoID,
			TodoTitle:    st.CurrentTodoTitle,
			StartedAt:    started,
			EndedAt:      endedAt,
			FocusMinutes: cfg.FocusMinutes,
		}
		// The session that just ended is the one that knows what it was about, so
		// its association is carried forward rather than dropped: the plan bubble
		// for the NEXT focus offers it back to the user, who confirms or replaces
		// it. CurrentTodo* below is still cleared - the todo belongs to the
		// finished session, not to the break.
		st.NextTodoID, st.NextTodoTitle = st.CurrentTodoID, st.CurrentTodoTitle
	}

	next := nextPhaseAfter(st, cfg)
	out.NextPhase = next
	st.Phase = next
	st.CurrentTodoID = ""
	st.CurrentTodoTitle = ""
	st.PhaseStartedAt = nil
	st.EndsAt = nil

	// A finished FOCUS waits for its record, whatever AutoStartNextPhase says. The
	// record is written by the caller before this state is saved, so PendingRecordID
	// is what tells the timer it is parked rather than merely stopped - a user who
	// pressed stop mid-focus has RunStopped too, but nothing pending.
	if ended == store.PhaseFocus {
		st.RunState = store.RunStopped
		st.RemainingOnHalt = int(cfg.PhaseDuration(next).Seconds())
		// out.Record is non-nil here, so the caller sets PendingRecordID from it.
		out.State = st
		return out
	}

	if cfg.AutoStartNextPhase {
		end := now.Add(cfg.PhaseDuration(next))
		st.RunState = store.RunRunning
		st.EndsAt = &end
		startedAt := now
		st.PhaseStartedAt = &startedAt
		st.RemainingOnHalt = 0
	} else {
		st.RunState = store.RunStopped
		st.RemainingOnHalt = int(cfg.PhaseDuration(next).Seconds())
	}

	out.State = st
	return out
}

// SkipPhase abandons the current phase without recording it.
//
// Only a focus phase can produce a record, so skipping one is the single way to
// leave a session out of the archive - which is why the UI asks before doing it.
func SkipPhase(st store.PomodoroState, cfg store.Config, now time.Time) PhaseOutcome {
	if st.Phase == store.PhaseIdle {
		return PhaseOutcome{State: st}
	}
	ended := st.Phase
	if ended == store.PhaseFocus {
		// A skipped focus still counts as done for the long-break cadence:
		// otherwise a user who skips would never reach a long break.
		st.CompletedCount++
	}
	next := nextPhaseAfter(st, cfg)
	st.Phase = next
	st.RunState = store.RunRunning
	end := now.Add(cfg.PhaseDuration(next))
	st.EndsAt = &end
	st.PhaseStartedAt = &now
	st.RemainingOnHalt = 0
	st.CurrentTodoID = ""
	st.CurrentTodoTitle = ""
	// Skipping is a flat "not this" about the whole session, so the question and
	// the todo carried into it go too. A gate left behind here would be one nobody
	// could answer: skip starts the next phase straight away and shows no bubble,
	// so the leftover flag would park the timer on the following transition
	// (design.md 4 D7; tasks.md T26).
	st.PlanPending = false
	st.NextTodoID = ""
	st.NextTodoTitle = ""
	return PhaseOutcome{State: st, EndedPhase: ended, NextPhase: next}
}

// ShouldAskPlan reports whether a phase transition must stop and ask the user
// what the next focus session is about. Only a break that has just run out,
// processed in real time (not a catch-up on launch) and not under a full-screen
// window, asks.
//
// It is a pure function over the outcome rather than a method on App so the
// gate's decision can be tested on its own, without a clock, a window or a
// store - the same reasoning that keeps the rest of the state machine pure. The
// app layer calls it from the branch that has just observed a phase end.
//
// The three refusals are each a different kind of "the user is not here to
// answer":
//
//   - restored: the phase ran out while the pet was closed, so the question
//     would be about a session that begins long after it was asked
//     (docs/reminder-pomodoro.md 4.5.2). The focus starts immediately instead.
//   - fullscreen: something is playing full screen in front of the user, and a
//     bubble over it is worse than a missed question (docs 4.7). The focus
//     starts silently, and the question is NOT asked later either.
//   - anything but a finished break leading into a focus: a finished FOCUS is
//     gated on its record instead (a different question, asked by
//     completePhaseLocked's other branch), and there is nothing to plan for when
//     the next phase is a rest.
func ShouldAskPlan(out PhaseOutcome, restored, fullscreen bool) bool {
	if restored || fullscreen {
		return false
	}
	return out.Finished() && store.IsBreakPhase(out.EndedPhase) && out.NextPhase == store.PhaseFocus
}

// Reconcile repairs a state loaded from disk, and decides whether a phase ran out
// while the pet was not running.
//
// This is what makes closing the pet mid-focus harmless: the end time is an
// absolute timestamp, so the remainder is known on the next launch. A phase that
// elapsed while away is completed (and its record written) rather than restarted,
// because the time really did pass.
func Reconcile(st store.PomodoroState, cfg store.Config, now time.Time) PhaseOutcome {
	if !st.Enabled || !st.IsRunning() || st.EndsAt == nil {
		return PhaseOutcome{State: st}
	}
	remaining := st.EndsAt.Sub(now)
	// More time left than the phase is long: the clock moved backwards.
	if duration := cfg.PhaseDuration(st.Phase); remaining > duration+clockJumpSlack {
		return PhaseOutcome{State: Reset(st).State, ClockJump: true, EndedPhase: st.Phase}
	}
	if remaining > 0 {
		return PhaseOutcome{State: st}
	}
	// The phase ended at EndsAt, not at "now": using now would credit the time
	// the app spent closed to the *next* phase and stretch every record.
	return FinishPhase(st, cfg, *st.EndsAt, now, true)
}

// HasPendingSummary reports whether a finished focus session is still waiting for
// its summary to be written.
//
// The record itself already exists on disk; this flag only drives the bubble, so
// dismissing the bubble loses the prompt, never the record.
func HasPendingSummary(st store.PomodoroState) bool { return st.PendingRecordID != "" }

// PhaseLabel names a phase for the interface.
func PhaseLabel(phase string) string {
	switch phase {
	case store.PhaseFocus:
		return "专注"
	case store.PhaseShortBreak:
		return "短休息"
	case store.PhaseLongBreak:
		return "长休息"
	default:
		return "空闲"
	}
}

// CeilSeconds rounds a duration up to whole seconds.
//
// Rounding UP is what keeps a countdown from ever showing 00:00 while there is
// still time left. It is the single rounding step for the whole timer: the
// seconds value and the MM:SS string are both derived from it, so the badge and
// the panel cannot disagree. (They once rounded differently - one truncated,
// one rounded up - and the number visibly flickered between two values.)
func CeilSeconds(d time.Duration) int {
	if d < 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

// FormatSeconds renders whole seconds as MM:SS.
//
// Minutes are not capped at 99: a 120-minute focus phase must read "120:00"
// rather than wrapping into a meaningless hour-less value.
func FormatSeconds(secs int) string {
	if secs < 0 {
		secs = 0
	}
	return fmt.Sprintf("%02d:%02d", secs/60, secs%60)
}

// FormatCountdown renders a duration as MM:SS, rounding up so a countdown never
// shows 00:00 while there is still time left.
func FormatCountdown(d time.Duration) string {
	return FormatSeconds(CeilSeconds(d))
}
