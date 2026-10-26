package main

import (
	"fmt"
	"log"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/pomodoro"
	"PetPC/internal/store"
)

// Bubble kinds. The bubble shares the panel's slot beside the pet and is
// mutually exclusive with it; only one of the three states (idle, bubble, panel)
// is ever visible at a time (docs/reminder-pomodoro.md 8.2).
const (
	BubbleNone     = ""
	BubblePick     = "pick"
	BubbleSummary  = "summary"
	BubbleReminder = "reminder"
	// BubblePlan asks what the NEXT focus session is about, and the focus does
	// not start until it is answered (docs/reminder-pomodoro.md 4.5.2).
	BubblePlan = "plan"
)

// Bubble sizes in logical pixels.
//
// The sizes are FIXED rather than content-driven because the window region must
// match what the page draws exactly: a region larger than the drawn card leaves
// glass-composited pixels that read as a pale blue band, and a region smaller
// than it clips the card (docs/reminder-pomodoro.md 8.1).
const (
	bubbleWidth          = 300
	bubbleHeightPick     = 200
	bubbleHeightSummary  = 160
	bubbleHeightReminder = 140
	// bubbleHeightPlan is taller than the summary because it carries an input
	// box the user is expected to type a sentence into, and shorter than the
	// picker because it lists no todos.
	bubbleHeightPlan = 180
	// bubbleArrow is the strip reserved inside the box for the pointer triangle.
	// It is inside the box, not outside it, so the arrow can never draw past the
	// window region.
	bubbleArrow = 10
	// pickListMax caps how many todos the picker lists before summarising the
	// rest, keeping the bubble a fixed height.
	pickListMax = 6
)

// BubbleItem is one row in a bubble's list.
type BubbleItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Due   string `json:"due,omitempty"`
	// Status is "", "soon" or "overdue", matching the panel's colouring.
	Status string `json:"status,omitempty"`
}

// BubbleView is the speech bubble beside the pet: what it says and where it is.
//
// Geometry travels with the content because the region crop is derived from it,
// and the region is what decides whether the pixels under the bubble are visible
// at all.
type BubbleView struct {
	Kind string `json:"kind"`
	// Title is the bubble's heading.
	Title string `json:"title"`
	// TodoTitle is the associated todo, shown by the summary bubble.
	TodoTitle string `json:"todoTitle,omitempty"`
	// Plan carries the plan text, for two different bubbles: on the PLAN bubble
	// it is the input box's initial value (what the previous session was about),
	// and on the SUMMARY bubble it is the 「计划：」 hint line's content - what the
	// user set out to do, shown beside the answer they are typing now.
	Plan string `json:"plan,omitempty"`
	// RecordID names the record the summary bubble edits.
	RecordID string `json:"recordId,omitempty"`
	// Items is the todo list a pick or reminder bubble shows.
	Items []BubbleItem `json:"items,omitempty"`
	// SelectedID is the entry the picker highlights by default.
	SelectedID string `json:"selectedId,omitempty"`
	// More is how many entries did not fit.
	More int `json:"more,omitempty"`
	// AllowUnattached lets the picker start a focus with no todo at all; the
	// list being empty makes this the only way to start.
	AllowUnattached bool `json:"allowUnattached"`

	// Geometry, in CSS pixels relative to the window's top-left corner.
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	W       float64 `json:"w"`
	H       float64 `json:"h"`
	OnRight bool    `json:"onRight"`
}

// bubbleLogicalHeight returns the fixed height for a bubble kind.
func bubbleLogicalHeight(kind string) int {
	switch kind {
	case BubblePick:
		return bubbleHeightPick
	case BubbleSummary:
		return bubbleHeightSummary
	case BubblePlan:
		return bubbleHeightPlan
	default:
		return bubbleHeightReminder
	}
}

// PomodoroView is the frontend-facing snapshot of the timer. It is recomputed
// from the state on every read so the countdown is never stale.
type PomodoroView struct {
	Enabled  bool   `json:"enabled"`
	Phase    string `json:"phase"`
	RunState string `json:"runState"`
	Label    string `json:"label"`
	// RemainingSec is how long is left in the current phase, in whole seconds.
	//
	// It is rounded UP, exactly like Countdown, so the two can never disagree.
	// They used to round differently (this one truncated, Countdown rounded up),
	// which made the badge flicker between two values once a second while the
	// frontend recomputed the same number its own way.
	RemainingSec int `json:"remainingSec"`
	// Countdown is RemainingSec preformatted as MM:SS, so the badge and the
	// panel always agree without either of them re-deriving it.
	Countdown string `json:"countdown"`
	// EndsAtMs is the phase's absolute end time as Unix milliseconds, or 0 when
	// nothing is counting down.
	//
	// The frontend ticks the countdown locally so it stays smooth even if an
	// event is dropped, and deriving that tick from an absolute instant rather
	// than by decrementing a counter is what keeps it honest: a late or missed
	// event cannot make it drift, and it cannot double-count against the events
	// Go sends. Both sides then apply the same rounding to the same instant.
	EndsAtMs int64 `json:"endsAtMs"`
	// Running is true only while the phase is actually counting down.
	Running bool `json:"running"`
	// Focusing is the flag the reminder scheduler gates on: a paused or stopped
	// pomodoro is not focusing.
	Focusing bool `json:"focusing"`
	// TodoTitle is the session's associated todo (a snapshot).
	TodoTitle      string `json:"todoTitle,omitempty"`
	CompletedCount int    `json:"completedCount"`
	// LongBreakEvery and UntilLongBreak drive the "3 more to a long break" hint.
	LongBreakEvery int `json:"longBreakEvery"`
	UntilLongBreak int `json:"untilLongBreak"`
	// PendingSummary is true while a finished session is waiting for its summary
	// to be written or skipped.
	PendingSummary bool `json:"pendingSummary"`
}

// pomodoroView renders the current state for the frontend.
func (a *App) pomodoroView() PomodoroView {
	st := a.store.Pomodoro()
	cfg := a.store.Config()
	now := time.Now()
	remaining := pomodoro.Remaining(st, now)

	untilLong := 0
	if st.Phase == store.PhaseFocus {
		every := cfg.LongBreakEvery
		if every < store.MinLongBreakEvery {
			every = store.DefaultLongBreakEvery
		}
		done := st.CompletedCount % every
		if done == 0 && st.CompletedCount > 0 {
			untilLong = 0
		} else {
			untilLong = every - done
		}
	}

	// Round once and derive both representations from the same value, so the
	// badge and the panel cannot show different numbers for the same instant.
	remainingSec := pomodoro.CeilSeconds(remaining)
	var endsAtMs int64
	if st.IsRunning() && st.EndsAt != nil {
		endsAtMs = st.EndsAt.UnixMilli()
	}

	return PomodoroView{
		Enabled:        st.Enabled,
		Phase:          st.Phase,
		RunState:       st.RunState,
		Label:          pomodoro.PhaseLabel(st.Phase),
		RemainingSec:   remainingSec,
		Countdown:      pomodoro.FormatSeconds(remainingSec),
		EndsAtMs:       endsAtMs,
		Running:        st.IsRunning(),
		Focusing:       st.IsFocusing(),
		TodoTitle:      st.CurrentTodoTitle,
		CompletedCount: st.CompletedCount,
		LongBreakEvery: cfg.LongBreakEvery,
		UntilLongBreak: untilLong,
		PendingSummary: pomodoro.HasPendingSummary(st),
	}
}

// --- lifecycle ------------------------------------------------------------

// startPomodoroLoop drives the timer.
//
// A one-second tick is enough: the countdown is derived from an absolute end
// time rather than accumulated, so nothing drifts even if a tick is late, and a
// phase that ended while the machine slept is caught on the first tick after
// waking.
//
// The stop channel is captured once, as a local. Re-reading the a.pomoStop field
// on every iteration would race shutdown, which closes the channel - and if it
// read the field after shutdown had cleared it, the loop would block on a nil
// channel forever instead of returning.
func (a *App) startPomodoroLoop() {
	stop := a.pomoStop
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				a.tickPomodoro()
			}
		}
	}()
}

// initPomodoro applies the startup preference and reconciles the persisted
// state, so a focus session survives closing and reopening the pet.
//
// PomodoroOnStartup only ever switches the timer ON. It deliberately does not
// switch it off: the enabled flag is part of the running state, and a session
// that was mid-focus when the pet closed has to come back (docs 4.1, 13.1). On a
// fresh install the persisted state is empty, so the feature is off until the
// user asks for it, which is the documented default.
func (a *App) initPomodoro() {
	cfg := a.store.Config()
	st := a.store.Pomodoro()

	if cfg.PomodoroOnStartup {
		st.Enabled = true
	}

	// A phase that ran out while the pet was closed is completed now, and its
	// record written: the time really did pass, so silently restarting it would
	// both lose the session and misreport the day's work.
	out := pomodoro.Reconcile(st, cfg, time.Now())
	if out.ClockJump {
		a.DebugLog("pomodoro: clock jumped backwards, reset to idle")
	}
	if out.Finished() {
		a.completePhase(out, true)
		return
	}
	if err := a.store.SavePomodoro(out.State); err != nil {
		log.Printf("pet: save pomodoro state: %v", err)
	}
	if out.State.Enabled {
		a.DebugLog(fmt.Sprintf("pomodoro: restored phase=%s run=%s pending=%v",
			out.State.Phase, out.State.RunState, out.State.PendingRecordID != ""))
	}

	// A phase parked on an unanswered record is re-offered on the next launch.
	//
	// Without this the pet would come back with a stopped timer and NO prompt:
	// PendingRecordID survived on disk, but the bubble did not, and nothing else
	// would ever ask the question again - so the gate would silently become a
	// dead end after any restart. Re-showing the prompt is what makes the state
	// self-healing.
	//
	// The prompt is only re-offered, never auto-answered: the record is still
	// waiting for the user, and starting the rest on their behalf would be exactly
	// the silent banked break the gate exists to prevent.
	if out.State.PendingRecordID != "" {
		a.DebugLog("pomodoro: a record is still waiting for its summary; re-asking")
		a.showSummaryBubble()
	}

	// The plan question is re-offered for exactly the same reason, with one extra
	// edge: the records tab can answer a summary, but NOTHING in the panel can
	// answer a plan question - so a launch that did not re-ask would park the
	// timer with no way forward at all.
	//
	// It is only re-offered, never auto-answered: PlanPending stays true and the
	// focus is still not running, because starting a session the user never
	// described is the thing the gate exists to prevent.
	if out.State.PlanPending {
		a.DebugLog("pomodoro: a focus is still waiting for its plan; re-asking")
		a.showPlanBubble()
	}
}

// tickPomodoro advances the timer by one step.
func (a *App) tickPomodoro() {
	a.pomoMu.Lock()
	defer a.pomoMu.Unlock()

	st := a.store.Pomodoro()
	cfg := a.store.Config()
	if !st.Enabled {
		return
	}
	now := time.Now()

	if st.IsRunning() && st.EndsAt != nil {
		if now.Before(*st.EndsAt) {
			// Still counting: push the new remainder so the badge ticks.
			a.emitPomodoro()
			return
		}
		a.completePhaseLocked(pomodoro.FinishPhase(st, cfg, *st.EndsAt, now, false), false)
		return
	}

	// A parked phase with no end time cannot advance on its own; the countdown
	// it shows is frozen on purpose.
	a.emitPomodoro()
}

// completePhase applies an outcome that ended a phase (the locked variant is used
// by the ticker, which already holds the mutex).
func (a *App) completePhase(out pomodoro.PhaseOutcome, restored bool) {
	a.completePhaseLocked(out, restored)
}

func (a *App) completePhaseLocked(out pomodoro.PhaseOutcome, restored bool) {
	// The plan of the session in flight travels into its record, BEFORE the
	// record is handed to the store: this is the only moment the plan and the
	// record exist together, and it is what makes the record's Plan mean "what
	// the user set out to do" rather than "what was in memory at some later
	// point".
	//
	// The field is cleared in the same breath because it belongs to this session
	// alone: leaving it set would write one session's plan into the next
	// session's permanent record (design.md 4 D7, 1.4). It is read and written
	// here under pomoMu, like every other use of it.
	if out.Record != nil {
		out.Record.Plan = a.focusPlan
		a.focusPlan = ""
	}
	// The record is written immediately, before the summary is asked for. That is
	// deliberate: it is what makes "skip" unable to erase a session, because by
	// the time the bubble appears the session is already archived
	// (docs/reminder-pomodoro.md 4.5, 6.1).
	if out.Record != nil {
		rec, err := a.store.AddPomodoroRecord(*out.Record)
		if err != nil {
			log.Printf("pet: archive pomodoro record: %v", err)
		} else {
			out.State.PendingRecordID = rec.ID
		}
	}
	// A break that has just run out does not roll straight into the next focus:
	// the timer parks here and asks what that focus is about, and the answer is
	// what starts it. The engine has already done its part (the next phase is in
	// place, running or stopped by AutoStartNextPhase); this only adds the
	// question on top, so the two paths into a focus both pass through it.
	//
	// The gate is applied to out.State BEFORE the save below, so what is written
	// is the state the user is looking at - a parked focus, not a running one.
	//
	// out.State is read DIRECTLY rather than through pendingPlanWaiting(): this
	// runs while holding pomoMu, and that accessor takes pomoMu itself (design.md
	// 2.2).
	//
	// The full-screen answer comes from the injected probe and nowhere else, so
	// the silent path (docs 4.7) is reachable from a windowless test. Reading it
	// unconditionally keeps shouldAskPlan the ONLY authority on the decision:
	// pre-filtering the call here would be a second, silently drifting copy of
	// its conditions.
	askPlan := pomodoro.ShouldAskPlan(out, restored, a.isForegroundFullscreen())
	if askPlan {
		cfg := a.store.Config()
		out.State.RunState = store.RunStopped
		out.State.EndsAt = nil
		out.State.RemainingOnHalt = int(cfg.PhaseDuration(store.PhaseFocus).Seconds())
		out.State.PlanPending = true
	}
	if err := a.store.SavePomodoro(out.State); err != nil {
		log.Printf("pet: save pomodoro state: %v", err)
	}

	// A finished focus phase asks for its record, and the REST WAITS for the answer.
	// The break is not counting yet: SavePomodoroSummary (or dismissing the prompt)
	// is what starts it, so the announcement here is "you are done, tell me what you
	// did" rather than "your break has begun".
	if out.EndedPhase == store.PhaseFocus {
		a.playPhaseSound(store.PhaseFocus)
		// The plugin event carries the CONCLUSION (a focus phase ended) and no
		// wording: which line to say, in which voice, is the voice plugin's
		// business. It fires whether or not the panel is open, because the plugin
		// keeps a headless frame mounted for exactly this.
		a.notifyPluginPhaseEnded(phaseKind(store.PhaseFocus))
		// A reminder is NOT marked owed here any more. That flag means "a reminder
		// was suppressed by the focus phase and is due now", and the rest has not
		// started - so there is nothing to deliver yet. It is noted when the break
		// actually begins instead, in startParkedPhaseLocked's caller.
		a.showSummaryBubble()
	} else if out.EndedPhase != "" && !restored {
		a.playPhaseSound(out.EndedPhase)
		a.notifyPluginPhaseEnded(phaseKind(out.EndedPhase))
		// The focus phase that suppressed reminders is over, so a missed one can
		// be delivered now.
		a.makeUpMissedReminder()
	}

	// The question comes up AFTER the state is on disk, so that answering it can
	// never race the write that made it necessary. It is also outside the branch
	// above and outside any a.mu section: showPlanBubble calls showBubble, which
	// takes a.mu, and taking that while a.mu is held would deadlock (the same
	// inversion DismissBubble documents).
	if askPlan {
		a.showPlanBubble()
	}

	a.DebugLog(fmt.Sprintf("pomodoro: %s ended -> %s (missed=%v, planPending=%v)",
		out.EndedPhase, out.NextPhase, out.Missed, out.State.PlanPending))
	a.emitPomodoro()
	a.emitLayout()
}

// playPhaseSound plays the system sound that announces the end of a phase.
//
// The two directions get different sounds so the cue is unambiguous without
// looking. Failures are ignored: a missing sound scheme must not stop the timer.
func (a *App) playPhaseSound(endedPhase string) {
	if !a.store.Config().PomodoroSound {
		return
	}
	name := desktop.SoundExclamation
	if store.IsFocusPhase(endedPhase) {
		name = desktop.SoundAsterisk
	}
	if err := desktop.PlaySystemSound(name); err != nil {
		log.Printf("pet: play sound: %v", err)
	}
}

// --- pomodoro commands ----------------------------------------------------

// PomodoroSettingsInput carries the pomodoro preferences from the settings tab.
type PomodoroSettingsInput struct {
	OnStartup          bool `json:"onStartup"`
	AutoStartNextPhase bool `json:"autoStartNextPhase"`
	RemindDuringBreak  bool `json:"remindDuringBreak"`
	Sound              bool `json:"sound"`
	FocusMinutes       int  `json:"focusMinutes"`
	ShortBreakMinutes  int  `json:"shortBreakMinutes"`
	LongBreakMinutes   int  `json:"longBreakMinutes"`
	LongBreakEvery     int  `json:"longBreakEvery"`
}

// SavePomodoroSettings persists the timer preferences.
//
// Out-of-range values are clamped by the store's Normalise rather than rejected,
// because a settings form that silently refuses a value is more confusing than
// one that snaps it to the nearest legal one.
func (a *App) SavePomodoroSettings(in PomodoroSettingsInput) (State, error) {
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.PomodoroOnStartup = in.OnStartup
		c.AutoStartNextPhase = in.AutoStartNextPhase
		c.RemindDuringBreak = in.RemindDuringBreak
		c.PomodoroSound = in.Sound
		c.FocusMinutes = in.FocusMinutes
		c.ShortBreakMinutes = in.ShortBreakMinutes
		c.LongBreakMinutes = in.LongBreakMinutes
		c.LongBreakEvery = in.LongBreakEvery
	}); err != nil {
		return State{}, err
	}
	// The new timings change what the current phase should be showing, so the
	// frontend is refreshed from the store rather than left with stale numbers.
	a.emitPomodoro()
	return a.GetState()
}

// SetPomodoroEnabled switches the whole feature on or off.
//
// Switching off halts a running phase rather than abandoning it, and the stored
// remainder means switching back on later continues where it stopped instead of
// counting the intervening hours as focus time.
func (a *App) SetPomodoroEnabled(enabled bool) (State, error) {
	a.pomoMu.Lock()
	st := a.store.Pomodoro()
	st.Enabled = enabled
	if !enabled && st.IsRunning() {
		st = pomodoro.Pause(st, time.Now()).State
		st.Enabled = false
	}
	if enabled {
		// Switching back on picks the session up where it was left, and catches
		// up on any phase that ran out while the feature was off.
		out := pomodoro.Reconcile(st, a.store.Config(), time.Now())
		if out.Finished() {
			a.completePhaseLocked(out, true)
			a.pomoMu.Unlock()
			a.emitPomodoro()
			a.emitLayout()
			return a.GetState()
		}
		st = out.State
	}
	if err := a.store.SavePomodoro(st); err != nil {
		a.pomoMu.Unlock()
		return State{}, err
	}
	if !enabled {
		// The prompts belong to the feature being switched off, and so does the
		// plan of the session in flight: nothing about an abandoned question may
		// outlive the feature that asked it (design.md 4 D7; tasks.md T26).
		st.PendingRecordID = ""
		st.PlanPending = false
		st.NextTodoID = ""
		st.NextTodoTitle = ""
		a.focusPlan = ""
		_ = a.store.SavePomodoro(st)
		a.clearBubbleLocked(BubbleSummary)
		a.clearBubbleLocked(BubblePlan)
	}
	a.pomoMu.Unlock()

	a.emitPomodoro()
	a.emitLayout()
	// Switching the feature off clears both gates (above), so a pet that was on
	// screen only for the question that asked about them goes away again.
	a.hideAfterQuestion()
	return a.GetState()
}

// PomodoroStartInput is the request to begin focusing.
type PomodoroStartInput struct {
	// TodoID may be empty, which means "focus without associating a todo". This
	// is the answer to the last open question in the design document: with
	// nothing open to work on the feature must still be usable.
	TodoID string `json:"todoId"`
}

// StartPomodoro begins a focus phase for the given todo.
//
// Selecting a todo never completes it, and the engine keeps only a snapshot of
// its title: an unfinished todo is still offered the next time the picker opens,
// which is what makes several pomodoros in a row on the same task work.
func (a *App) StartPomodoro(in PomodoroStartInput) (State, error) {
	a.pomoMu.Lock()

	cfg := a.store.Config()
	st := a.store.Pomodoro()

	title := ""
	if in.TodoID != "" {
		found := false
		for _, t := range a.store.Todos() {
			if t.ID == in.TodoID {
				title, found = t.Title, true
				break
			}
		}
		if !found {
			a.pomoMu.Unlock()
			return State{}, fmt.Errorf("未找到该待办")
		}
	}

	st.Enabled = true
	now := time.Now()

	// Continuing a session is the default when the same todo comes back: "stop"
	// means "pause this session", not "throw it away"
	// (docs/reminder-pomodoro.md 13.1).
	//
	// The plan question is excluded: the parked state a finished break leaves
	// behind is Phase=focus, RunStopped, CurrentTodoID="" with the full focus
	// length banked, which is EXACTLY the shape this shortcut looks for when the
	// user picks "no todo" - so without !st.PlanPending the click would start the
	// clock and the question would never be answered (design.md 4.2).
	if !st.PlanPending && st.Phase == store.PhaseFocus && !st.IsRunning() && st.CurrentTodoID == in.TodoID && pomodoro.Remaining(st, now) > 0 {
		out := pomodoro.Resume(st, cfg, now)
		if err := a.store.SavePomodoro(out.State); err != nil {
			a.pomoMu.Unlock()
			return State{}, err
		}
		a.clearAllBubblesLocked()
		a.emitPomodoro()
		a.emitLayout()
		a.pomoMu.Unlock()
		return a.GetState()
	}

	// Confirming the picker does NOT start the clock any more. It writes what the
	// session will be about and hands over to the plan question, which is the one
	// thing that starts a focus - so both ways in pass through it
	// (docs/reminder-pomodoro.md 4.5.2).
	//
	// Going through the gate rather than through startFocus also means the todo
	// moves in the same single statement as it does on the break-end path: the
	// user's choice here is written to NextTodo* and carried by
	// StartPomodoroWithPlan, so there is no "which todo wins" branch anywhere.
	//
	// The phase is put in place but parked, exactly as a finished break leaves it:
	// Phase=focus, RunStopped, nothing counting, the whole focus length banked and
	// CurrentTodo* still empty. That shape is what makes "continue" mean the same
	// thing on both paths, and it is also why PlanPending has to be set on a
	// non-idle phase - an idle timer has no next session to plan for, and
	// NormaliseState clears the flag there on purpose (design.md 1.3).
	st.NextTodoID, st.NextTodoTitle = in.TodoID, title
	st.Phase = store.PhaseFocus
	st.RunState = store.RunStopped
	st.EndsAt = nil
	st.PhaseStartedAt = nil
	st.RemainingOnHalt = int(cfg.PhaseDuration(store.PhaseFocus).Seconds())
	st.PlanPending = true
	if err := a.store.SavePomodoro(st); err != nil {
		a.pomoMu.Unlock()
		return State{}, err
	}
	// Whatever the bubble was saying is replaced by the question: the picker is
	// what was just answered, and a reminder left beside a pet that is being
	// asked a question would be noise.
	a.clearAllBubblesLocked()
	a.emitPomodoro()
	a.emitLayout()
	// showPlanBubble reaches showBubble, which takes a.mu. That must not happen
	// while pomoMu is held (the documented pomoMu -> a.mu order), so the lock is
	// released here rather than by a defer - the same shape StartPomodoroWithPlan
	// uses.
	a.pomoMu.Unlock()
	a.showPlanBubble()
	a.DebugLog(fmt.Sprintf("pomodoro: waiting for the plan of the next focus (todo=%q)", title))
	return a.GetState()
}

// StartPomodoroWithPlan answers the plan question and starts the focus session
// from NOW.
//
// No-op when PlanPending is false: with nothing waiting, this command would
// otherwise start a session the user never asked for - and "no-op" means every
// field is left exactly as it was, including the in-flight plan. That is what
// makes the frontend free to call it from a button that is only meaningful while
// the question is up.
//
// An empty plan is legal and identical to "skip": the session is still started
// and still produces a record, because the record is what makes the session real
// (design.md 1.4, 2).
//
// The carried todo moves in unconditionally, which is why there is no "which
// todo wins" question here: whichever path raised the question already wrote
// NextTodo* (a finished focus carried its own, the picker wrote the user's
// choice), so this only has to move it.
func (a *App) StartPomodoroWithPlan(plan string) (State, error) {
	a.pomoMu.Lock()

	st := a.store.Pomodoro()
	if !st.PlanPending {
		a.pomoMu.Unlock()
		return a.GetState()
	}

	cfg := a.store.Config()
	now := time.Now()
	end := now.Add(cfg.PhaseDuration(store.PhaseFocus))

	st.CurrentTodoID, st.CurrentTodoTitle = st.NextTodoID, st.NextTodoTitle
	st.NextTodoID, st.NextTodoTitle = "", ""
	st.PlanPending = false
	st.Phase = store.PhaseFocus
	st.RunState = store.RunRunning
	st.EndsAt = &end
	st.RemainingOnHalt = 0
	st.PhaseStartedAt = &now
	// The plan is the session's, from here until the record is written. It is
	// set under pomoMu, like every other use of the field.
	a.focusPlan = plan

	if err := a.store.SavePomodoro(st); err != nil {
		a.pomoMu.Unlock()
		return State{}, err
	}
	a.pomoMu.Unlock()

	// Answering the question frees the slot, exactly like answering the summary
	// does. Both calls are made without pomoMu held by the bubble helpers'
	// contract: showBubble takes a.mu, and clearBubble re-crops the window.
	a.clearBubble(BubblePlan)
	a.emitPomodoro()
	a.emitLayout()
	a.DebugLog(fmt.Sprintf("pomodoro: focus started from the plan gate (plan=%q)", plan))
	// The question is answered, so a pet that only came back for it goes away
	// again. Last, so the focus phase it started is already on screen (badge,
	// state) when the window folds.
	a.hideAfterQuestion()
	return a.GetState()
}

// clearAllBubblesLocked dismisses whatever bubble is up.
func (a *App) clearAllBubblesLocked() {
	for _, kind := range []string{BubblePick, BubbleSummary, BubbleReminder, BubblePlan} {
		a.clearBubbleLocked(kind)
	}
}

// PausePomodoro halts the countdown, keeping the remainder.
func (a *App) PausePomodoro() (State, error) {
	return a.transition(func(st store.PomodoroState, cfg store.Config, now time.Time) pomodoro.PhaseOutcome {
		return pomodoro.Pause(st, now)
	})
}

// ResumePomodoro restarts a paused or stopped phase.
//
// It is also one of the answers to the plan question. "Continue" means "get on
// with this session", and the only plan it can imply is the empty one - so when
// the question is waiting it answers it rather than leaving the user parked at a
// button that appears to do nothing. Reading the flag and then answering in a
// separate locked step is safe: only the user's own clicks reach either of them,
// and the answer is idempotent (design.md 4.2).
func (a *App) ResumePomodoro() (State, error) {
	if a.pendingPlanWaiting() {
		return a.StartPomodoroWithPlan("")
	}
	return a.transition(func(st store.PomodoroState, cfg store.Config, now time.Time) pomodoro.PhaseOutcome {
		return pomodoro.Resume(st, cfg, now)
	})
}

// StopPomodoro halts the phase like pause does, but is offered to the user as
// "stop" - the session can still be continued afterwards.
func (a *App) StopPomodoro() (State, error) {
	return a.transition(func(st store.PomodoroState, cfg store.Config, now time.Time) pomodoro.PhaseOutcome {
		return pomodoro.Stop(st, now)
	})
}

// SkipPhase abandons the current phase and moves to the next one.
//
// Skipping a focus phase deliberately produces no record, which is why the UI
// confirms first.
//
// It also drops the plan of the session in flight. That field lives on the App
// rather than in the state the pure function sees, so it is cleared here, inside
// the callback that transition runs under pomoMu - the only place that can touch
// it without changing either signature (design.md 4 D7; tasks.md T26).
func (a *App) SkipPhase() (State, error) {
	return a.transition(func(st store.PomodoroState, cfg store.Config, now time.Time) pomodoro.PhaseOutcome {
		out := pomodoro.SkipPhase(st, cfg, now)
		a.focusPlan = ""
		return out
	})
}

// ResetPomodoro abandons the session entirely: this is the explicit "give up"
// that stop is not (docs/reminder-pomodoro.md 13.1). It lives in the settings
// tab so it cannot be hit by accident.
func (a *App) ResetPomodoro() (State, error) {
	a.pomoMu.Lock()
	st := pomodoro.Reset(a.store.Pomodoro()).State
	// Any pending summary prompt goes with it, and so does the plan question and
	// the plan of the session in flight: reset is the explicit "give up", which
	// must leave no gate and no plan behind (design.md 4 D7; tasks.md T26).
	st.PendingRecordID = ""
	st.PlanPending = false
	st.NextTodoID = ""
	st.NextTodoTitle = ""
	a.focusPlan = ""
	if err := a.store.SavePomodoro(st); err != nil {
		a.pomoMu.Unlock()
		return State{}, err
	}
	a.clearBubbleLocked(BubbleSummary)
	a.clearBubbleLocked(BubblePick)
	a.clearBubbleLocked(BubblePlan)
	a.emitPomodoro()
	a.emitLayout()
	a.pomoMu.Unlock()
	// Giving the session up is a way of answering both gates, so a pet that came
	// back for one of them goes away again.
	a.hideAfterQuestion()
	return a.GetState()
}

// transition runs one state-machine step under the pomodoro lock and persists it.
func (a *App) transition(fn func(store.PomodoroState, store.Config, time.Time) pomodoro.PhaseOutcome) (State, error) {
	a.pomoMu.Lock()
	defer a.pomoMu.Unlock()

	cfg := a.store.Config()
	out := fn(a.store.Pomodoro(), cfg, time.Now())
	if err := a.store.SavePomodoro(out.State); err != nil {
		return State{}, err
	}
	a.emitPomodoro()
	return a.GetState()
}

// --- records --------------------------------------------------------------

// PomodoroRecords returns the permanent archive, newest first.
func (a *App) PomodoroRecords() ([]store.PomodoroRecord, error) {
	return a.store.PomodoroRecords(), nil
}

// SavePomodoroSummary writes the summary for a finished session and STARTS the
// rest that was waiting on it.
//
// An empty summary is a legitimate value: that is what "skip" stores, and the
// record stays in the archive either way. There is no matching delete method,
// by design.
//
// Starting the next phase here is what makes the record gate the timer: finishPhase
// parks the rest when a focus ends, so this - or the dismiss path below - is the
// only thing that sets it going. Both buttons of the summary bubble land here, so
// "skip" starts the rest exactly like "save" does; the gate is on ANSWERING the
// prompt, not on typing something into it.
func (a *App) SavePomodoroSummary(recordID, summary string) (State, error) {
	a.pomoMu.Lock()
	if _, err := a.store.SetPomodoroSummary(recordID, summary); err != nil {
		a.pomoMu.Unlock()
		return State{}, err
	}
	st := a.store.Pomodoro()
	startedBreak := false
	if st.PendingRecordID == recordID {
		st.PendingRecordID = ""
		// Answering the prompt is what starts the rest. It is done here rather
		// than in the frontend so that every path - typing a summary, skipping,
		// and the panel's own record editor - starts it exactly once.
		st = a.startParkedPhaseLocked(st)
		startedBreak = store.IsBreakPhase(st.Phase)
		if err := a.store.SavePomodoro(st); err != nil {
			a.pomoMu.Unlock()
			return State{}, err
		}
	}
	a.pomoMu.Unlock()

	a.clearBubble(BubbleSummary)
	a.emitPomodoro()
	// Entering a rest is an immediate reminder trigger (docs 5.2), and the rest
	// begins HERE rather than when the focus ended. It is marked as owed rather
	// than shown now, because the prompt only just released the bubble slot - the
	// call below delivers it.
	if startedBreak {
		a.noteBreakReminderOwed()
	}
	// If the pet is only on screen because the question brought it back, it goes
	// away again now that the question has been answered. This runs BEFORE the
	// make-up reminder below on purpose: the pet is about to be hidden, and a
	// reminder shown into a window that is vanishing would be a one-frame flash.
	// With the pet back in hiding the delivery is suppressed the normal way, the
	// owed flag stays set, and the reminder arrives on the real next opportunity.
	a.hideAfterQuestion()
	// Answering the summary frees the bubble slot, so a reminder owed from the
	// start of the break can now be delivered.
	a.makeUpMissedReminder()
	return a.GetState()
}

// EditPomodoroRecord rewrites the plan and the summary of an existing record.
//
// The records page edits both lines at once, so there is one binding and one
// write: a page that saved them separately could leave a record half-edited if
// the second call failed. An empty plan is a legitimate value - the user may have
// decided the session had no plan after all - and the record itself is never
// removed (docs/reminder-pomodoro.md 6.1).
func (a *App) EditPomodoroRecord(recordID, plan, summary string) (State, error) {
	a.pomoMu.Lock()
	if _, err := a.store.SetPomodoroPlanAndSummary(recordID, plan, summary); err != nil {
		a.pomoMu.Unlock()
		return State{}, err
	}
	a.pomoMu.Unlock()
	return a.GetState()
}

// startParkedPhaseLocked resumes a phase that was waiting for its record.
//
// It is a no-op unless the phase is genuinely parked, which is what keeps it safe
// to call from every path that answers a summary: a phase the user has since
// started by hand, or one that never had a record pending, must not be restarted
// underneath them (that would silently reset a break in progress).
//
// The break starts from NOW, not from when the focus ended. That is the point of
// the gate: the time spent filling in the summary is the user's, and counting it
// as rest would give them less rest than the setting promises.
func (a *App) startParkedPhaseLocked(st store.PomodoroState) store.PomodoroState {
	if st.Phase == store.PhaseIdle || st.IsRunning() {
		return st
	}
	cfg := a.store.Config()
	now := time.Now()
	end := now.Add(cfg.PhaseDuration(st.Phase))
	st.RunState = store.RunRunning
	st.EndsAt = &end
	startedAt := now
	st.PhaseStartedAt = &startedAt
	st.RemainingOnHalt = 0
	a.DebugLog(fmt.Sprintf("pomodoro: %s started after its record was answered", st.Phase))
	return st
}

// --- bubbles --------------------------------------------------------------

// ShowPomodoroPicker opens the "which todo is this for?" bubble.
//
// The timer does NOT start here: it starts when the user confirms, which is what
// makes the choice mandatory (docs/reminder-pomodoro.md 4.2).
func (a *App) ShowPomodoroPicker() (Layout, error) {
	todos := a.store.OpenTodos()
	items := make([]BubbleItem, 0, pickListMax)
	for i, t := range todos {
		if i == pickListMax {
			break
		}
		items = append(items, BubbleItem{ID: t.ID, Title: t.Title, Due: dueLabel(t), Status: dueStatusOf(t)})
	}
	more := len(todos) - len(items)
	if more < 0 {
		more = 0
	}

	st := a.store.Pomodoro()
	b := &BubbleView{
		Kind:       BubblePick,
		Title:      "想专注做哪件事？",
		Items:      items,
		More:       more,
		SelectedID: st.CurrentTodoID,
		// With nothing open there is nothing to choose, so focusing unattached
		// has to be allowed or the feature would be unusable.
		AllowUnattached: len(todos) == 0,
	}
	// Coming back to a stopped session offers to continue it rather than
	// silently starting a fresh one.
	return a.showBubble(b)
}

// showSummaryBubble asks for the summary of the session that just ended.
func (a *App) showSummaryBubble() {
	st := a.store.Pomodoro()
	if !pomodoro.HasPendingSummary(st) {
		return
	}
	// Fill in the associated todo's title from the record, so a todo renamed
	// since the session started cannot change what the prompt refers to.
	//
	// The plan comes out of the SAME lookup: it is the record's own Plan, taken
	// from the archive rather than from memory, which is what lets the prompt
	// still show it after a restart - the in-flight plan is memory-only and long
	// since consumed (design.md 1.4).
	title := st.CurrentTodoTitle
	plan := ""
	for _, r := range a.store.PomodoroRecords() {
		if r.ID == st.PendingRecordID {
			title = r.TodoTitle
			plan = r.Plan
			break
		}
	}
	// The pet comes back if the user had it hidden. This prompt gates the rest,
	// so a hidden window here is a timer parked with nothing on screen saying so
	// (see revealForQuestion). It happens BEFORE showBubble, which re-crops the
	// region around the bubble that is about to be drawn.
	a.revealForQuestion()
	a.showBubble(&BubbleView{
		Kind:      BubbleSummary,
		Title:     "这一个番茄做了什么？",
		TodoTitle: title,
		Plan:      plan,
		RecordID:  st.PendingRecordID,
	})
}

// showPlanBubble asks what the next focus session is about.
//
// Precondition: PlanPending is true. Otherwise it does NOTHING AT ALL - no
// bubble and no state change - because a plan question nobody is waiting on
// could not be answered into anything: answering it would start a focus phase
// the user never asked for, and leaving it up would just block the slot.
//
// The box starts with the most recent record's plan. That is the answer to "what
// should the field say after a silent path (full screen, or a catch-up on
// launch) wrote no plan": the last thing the user set out to do is the best
// guess at what they are still working on, and an empty box would make them
// retype it. It is a starting value, not a decision - the field is editable.
//
// The caller must hold pomoMu (or be single-threaded startup): the plan question
// is part of the timer's state, and showBubble takes a.mu, which must not be
// acquired while a.mu is already held (docs/reminder-pomodoro.md 8.2).
func (a *App) showPlanBubble() {
	st := a.store.Pomodoro()
	if !st.PlanPending {
		return
	}

	// The previous plan is offered as the starting value; with no records at all
	// there is nothing to offer, and the box is simply empty.
	plan := ""
	if recs := a.store.PomodoroRecords(); len(recs) > 0 {
		plan = recs[0].Plan
	}

	// Same rule as the summary prompt, and it matters more here: this question
	// gates the FOCUS, and the panel has no way to answer it - so a hidden window
	// would leave the timer parked with no entry point anywhere but the shortcut.
	// Done before showBubble so the re-crop that follows includes the bubble.
	a.revealForQuestion()
	a.showBubble(&BubbleView{
		Kind:      BubblePlan,
		Title:     "接下来想专注做什么？",
		TodoTitle: st.NextTodoTitle,
		Plan:      plan,
	})
}

// DismissBubble closes whatever bubble is up, EXCEPT a question the timer is
// parked behind: the summary prompt waiting for its record, or the plan prompt
// waiting for the next session's plan.
//
// A dismissal can never be an answer, so this never triggers the re-hide a
// question reveal owes: the gates below are the only way to settle one, and each
// of them calls hideAfterQuestion itself. A reminder - the one bubble a hidden
// pet can be showing - leaves the flag untouched, so dismissing it puts nothing
// away.
//
// The summary prompt is deliberately not dismissable. It is the gate on the rest:
// the break does not start until this question is answered, so a prompt that could
// be waved away - by Escape, by dragging the pet, or by a stray click - would make
// the gate meaningless, and the user would be left with a stopped timer and nothing
// on screen explaining why. Answering it is a real choice, and it is one click: both
// buttons ("保存" and "跳过") resolve it.
//
// The plan prompt is not dismissable for the same reason and in the same shape: it
// is the gate on the FOCUS, which does not start counting until the plan is answered
// (or skipped). It has one extra edge - the panel has no way to answer it, unlike
// the summary, which the records tab can edit - which is why losing it would be a
// dead end rather than a nuisance; SetPanelOpen and initPomodoro re-raise it
// (design.md 2.1).
//
// This replaces an earlier design in which a dismissal was treated as "skip". That
// kept the timer unstuck, but it also made the choice implicit - and the user asked
// for the opposite: an explicit decision before the rest begins.
//
// Every other bubble kind is dismissed as before. A reminder, in particular, still
// auto-hides, because it asks nothing.
//
// The refusal is tied to the PENDING question rather than to the bubble kind: a
// plan bubble whose question has already been answered - by another path, or by
// giving the session up - is just a bubble, and closes like any other.
func (a *App) DismissBubble() (Layout, error) {
	// Both gates' state is read BEFORE a.mu is taken, and that ordering is
	// load-bearing rather than tidiness: tickPomodoro holds pomoMu while calling
	// into the bubble helpers (completePhaseLocked -> showSummaryBubble ->
	// showBubble, which takes a.mu), so taking pomoMu while holding a.mu here would
	// be a lock-order inversion - and a real deadlock, not a theoretical one.
	pending := a.pendingSummaryWaiting()
	planPending := a.pendingPlanWaiting()

	a.mu.Lock()
	b := a.bubble
	// A pending question is not dismissable. Reporting the CURRENT layout rather
	// than an error keeps the contract simple for the frontend: it re-applies
	// geometry and the bubble stays exactly where it is.
	if b != nil && ((b.Kind == BubbleSummary && pending) || (b.Kind == BubblePlan && planPending)) {
		a.mu.Unlock()
		return a.GetLayout()
	}
	a.setBubbleLocked(nil)
	a.mu.Unlock()

	if b != nil {
		a.emitBubble(b.Kind, false)
	}
	// The slot is free again, so a reminder that a focus phase suppressed - or
	// one owed because a rest just began - can be delivered now.
	a.makeUpMissedReminder()
	return a.applyWindowGeometry()
}

// pendingSummaryWaiting reports whether a record is still waiting for its summary.
//
// It takes pomoMu on its own and must NOT be called while holding a.mu: the
// scheduler holds pomoMu across its calls into the bubble helpers, so the reverse
// order would invert the two locks.
func (a *App) pendingSummaryWaiting() bool {
	a.pomoMu.Lock()
	defer a.pomoMu.Unlock()
	return a.store.Pomodoro().PendingRecordID != ""
}

// pendingPlanWaiting reports whether the plan question is still unanswered.
//
// It takes pomoMu on its own and must NOT be called while holding a.mu (same
// contract as pendingSummaryWaiting, and for the same reason: the scheduler holds
// pomoMu across its calls into the bubble helpers, so the reverse order inverts
// the two locks).
func (a *App) pendingPlanWaiting() bool {
	a.pomoMu.Lock()
	defer a.pomoMu.Unlock()
	return a.store.Pomodoro().PlanPending
}

// showBubble replaces any current bubble and re-crops the window around it.
//
// A bubble and the panel are mutually exclusive: both live in the same slot
// beside the pet, and the region must describe exactly one of them. A plugin's
// floating surface is the third occupant of that slot, so it is folded away
// here too.
//
// Callers that show something merely informational (the reminder) must check
// bubbleAwaitingAnswer first, so a display-only bubble can never displace a
// question the user has not answered yet.
func (a *App) showBubble(b *BubbleView) (Layout, error) {
	a.mu.Lock()
	a.panelOpen = false
	// The plugin floating surface shares this slot too, so showing a bubble
	// folds it away: the region can only describe one occupant, and the surface's
	// rectangles would otherwise keep the window cropped around an interface that
	// is no longer being drawn.
	a.setPluginSurfaceLocked(nil)
	a.setBubbleLocked(b)
	a.mu.Unlock()
	a.emitBubble(b.Kind, true)
	return a.applyWindowGeometry()
}

// unansweredQuestionKind reports which bubble is holding the slot with a question
// the user has not answered, or BubbleNone when none is.
//
// The picker, the summary prompt and the plan prompt are the three. The reminder
// is just a notification and the live overlay shows a reply, so neither asks
// anything; a surface may replace those, but not a question.
//
// The plan prompt counts even though the timer is stopped rather than RUNNING
// behind it: "stopped" here means "parked on an unanswered question", and losing
// it leaves a timer that cannot be started from anything the user can see.
//
// The KIND is returned rather than a bool because every surface that is refused
// the slot has to explain itself, and "select a todo", "write the summary" and
// "write the plan" are not the same sentence.
func (a *App) unansweredQuestionKind() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.bubble == nil {
		return BubbleNone
	}
	if a.bubble.Kind == BubblePick || a.bubble.Kind == BubbleSummary || a.bubble.Kind == BubblePlan {
		return a.bubble.Kind
	}
	return BubbleNone
}

// bubbleAwaitingAnswer reports whether the bubble on screen is waiting for the
// user to choose or type something.
//
// The picker, the summary prompt and the plan prompt hold a decision the user has
// not made; the reminder is just a notification. That asymmetry is why a reminder
// must never overwrite them, while starting a focus phase (which dismisses
// everything) is allowed to.
//
// It is also why a floating surface must not take the slot from them - see
// OpenPluginSurface. That surface occupies the same rectangle and has nowhere
// else to go, so opening it over a question does not cover the question, it
// destroys it.
func (a *App) bubbleAwaitingAnswer() bool {
	return a.unansweredQuestionKind() != BubbleNone
}

// questionOwnsTheSlot is the sentence the pet's own live overlay used to answer
// with when a bubble was already waiting for the user's answer.
//
// The overlay is gone (its replacement is a plugin's floating surface, which
// answers with pluginSurfaceBlocked instead), so nothing on a request path calls
// this any more. It is kept because the three sentences it defines are the
// reference the surface's wording was written against, and the tests still pin
// that the three are distinct - a regression here would silently make "choose a
// todo" and "write the summary" the same instruction.
//
// It names the consequence rather than the rule: the summary prompt is the gate
// on the rest and the plan prompt is the gate on the focus, so a user who cannot
// see why the pet will not open the conversation has no way to know that the
// timer is parked behind that question. Each question gets its own sentence,
// because "choose a todo", "write the summary" and "write the plan" are three
// different things to go and do.
func questionOwnsTheSlot(kind string) string {
	switch kind {
	case BubblePick:
		return "番茄钟还在等你选一件事，先处理它再打开对话"
	case BubblePlan:
		return "番茄钟还在等你写计划，先答完再打开对话"
	default:
		return "番茄钟还在等你写总结，先答完它再打开对话"
	}
}

// setBubbleLocked sets or clears the showing bubble and keeps the layout in step.
//
// This is THE place that maintains the invariant "a.layout.Bubble describes the
// bubble that is actually showing". Every mutation of a.bubble goes through here
// (or showBubble), because the frontend renders the bubble FROM THE LAYOUT: if
// the two ever disagree, GetLayout hands the frontend a bubble that no longer
// exists and it is drawn again immediately. That is exactly how "save" came to
// look like it did nothing - the record was written, the bubble was cleared, and
// the stale layout redrew the prompt.
//
// Callers must hold a.mu.
func (a *App) setBubbleLocked(b *BubbleView) {
	if b != nil && b.W == 0 && b.H == 0 {
		// The logical shape travels with the state as soon as the bubble is
		// recorded, not only once the window has been re-cropped. Two things
		// depend on that: a caller that cannot crop (no window yet, a geometry
		// failure) still advertises a bubble whose box is the one the page will
		// draw, and the region pass has a shape to work from rather than a zero.
		// applyWindowGeometry overwrites both with the DPI-scaled values.
		withShape := *b
		withShape.W = float64(bubbleWidth)
		withShape.H = float64(bubbleLogicalHeight(b.Kind))
		b = &withShape
	}
	a.bubble = b
	a.layout.Bubble = b
}

// clearBubble closes a bubble of the given kind, if it is the one showing.
//
// It also re-crops the window: the region has to shrink back to just the pet,
// or the area the bubble occupied stays part of the visible window while the
// page no longer paints there, showing the glass-composited band again.
func (a *App) clearBubble(kind string) {
	a.mu.Lock()
	cur := a.bubble
	matched := cur != nil && cur.Kind == kind
	if matched {
		a.setBubbleLocked(nil)
	}
	a.mu.Unlock()
	if !matched {
		return
	}
	a.emitBubble(kind, false)
	if _, err := a.applyWindowGeometry(); err != nil {
		log.Printf("pet: re-crop after closing bubble: %v", err)
	}
}

// clearBubbleLocked is clearBubble for callers already holding pomoMu. It still
// takes a.mu for the bubble field, which is guarded separately.
func (a *App) clearBubbleLocked(kind string) { a.clearBubble(kind) }

// emitBubble tells the frontend that a bubble appeared or went away.
//
// The geometry itself arrives with the layout; this event exists so the
// frontend can run the fade and decide about keyboard focus without having to
// diff the layout.
func (a *App) emitBubble(kind string, shown bool) {
	if a.ctx == nil {
		return
	}
	a.emitEvent("pet:bubble", map[string]any{"kind": kind, "shown": shown})
}

// emitPomodoro pushes the current timer snapshot to the frontend.
func (a *App) emitPomodoro() {
	if a.ctx == nil {
		return
	}
	view := a.pomodoroView()
	a.emitEvent("pet:pomodoro", view)
	// Plugins that subscribed to timer.changed hear about a PHASE change, not
	// about every tick. The countdown is published once a second, and forwarding
	// each of those to every subscribed plugin would be a cross-process message
	// per plugin per second for a number that changed by one.
	a.pluginHost.mu.Lock()
	changed := a.pluginHost.lastPhase != view.Phase+"/"+view.RunState
	a.pluginHost.lastPhase = view.Phase + "/" + view.RunState
	a.pluginHost.mu.Unlock()
	if changed {
		a.emitPluginStateChange("timer.changed", nil)
	}
}

// --- helpers --------------------------------------------------------------

// dueStatusOf classifies a due date for colouring, mirroring the frontend's
// rules so the bubble and the panel agree.
func dueStatusOf(t store.Todo) string {
	if t.Done || t.Due == nil {
		return ""
	}
	now := time.Now()
	if t.Due.Before(now) {
		return "overdue"
	}
	if t.Due.Sub(now) <= 24*time.Hour {
		return "soon"
	}
	return ""
}

// dueLabel renders a due date the way the panel's own formatter does.
func dueLabel(t store.Todo) string {
	if t.Due == nil {
		return ""
	}
	d := *t.Due
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
	diff := int(day.Sub(today).Hours() / 24)
	clock := fmt.Sprintf("%02d:%02d", d.Hour(), d.Minute())
	switch diff {
	case 0:
		return "今天 " + clock
	case 1:
		return "明天 " + clock
	case -1:
		return "昨天 " + clock
	default:
		return fmt.Sprintf("%d月%d日 %s", d.Month(), d.Day(), clock)
	}
}
