package main

import (
	"fmt"
	"log"
	"time"

	"PetPC/internal/desktop"
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
	remaining := Remaining(st, now)

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
	remainingSec := ceilSeconds(remaining)
	var endsAtMs int64
	if st.IsRunning() && st.EndsAt != nil {
		endsAtMs = st.EndsAt.UnixMilli()
	}

	return PomodoroView{
		Enabled:        st.Enabled,
		Phase:          st.Phase,
		RunState:       st.RunState,
		Label:          phaseLabel(st.Phase),
		RemainingSec:   remainingSec,
		Countdown:      formatSeconds(remainingSec),
		EndsAtMs:       endsAtMs,
		Running:        st.IsRunning(),
		Focusing:       st.IsFocusing(),
		TodoTitle:      st.CurrentTodoTitle,
		CompletedCount: st.CompletedCount,
		LongBreakEvery: cfg.LongBreakEvery,
		UntilLongBreak: untilLong,
		PendingSummary: hasPendingSummary(st),
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
	out := reconcile(st, cfg, time.Now())
	if out.ClockJump {
		a.DebugLog("pomodoro: clock jumped backwards, reset to idle")
	}
	if out.finished() {
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
		a.completePhaseLocked(finishPhase(st, cfg, *st.EndsAt, now, false), false)
		return
	}

	// A parked phase with no end time cannot advance on its own; the countdown
	// it shows is frozen on purpose.
	a.emitPomodoro()
}

// completePhase applies an outcome that ended a phase (the locked variant is used
// by the ticker, which already holds the mutex).
func (a *App) completePhase(out phaseOutcome, restored bool) { a.completePhaseLocked(out, restored) }

func (a *App) completePhaseLocked(out phaseOutcome, restored bool) {
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
	if err := a.store.SavePomodoro(out.State); err != nil {
		log.Printf("pet: save pomodoro state: %v", err)
	}

	// A finished focus phase asks for its record, and the REST WAITS for the answer.
	// The break is not counting yet: SavePomodoroSummary (or dismissing the prompt)
	// is what starts it, so the announcement here is "you are done, tell me what you
	// did" rather than "your break has begun".
	if out.EndedPhase == store.PhaseFocus {
		a.playPhaseSound(store.PhaseFocus)
		a.speakPomodoroLine(store.PhaseFocus)
		// A reminder is NOT marked owed here any more. That flag means "a reminder
		// was suppressed by the focus phase and is due now", and the rest has not
		// started - so there is nothing to deliver yet. It is noted when the break
		// actually begins instead, in startParkedPhaseLocked's caller.
		a.showSummaryBubble()
	} else if out.EndedPhase != "" && !restored {
		a.playPhaseSound(out.EndedPhase)
		a.speakPomodoroLine(out.EndedPhase)
		// The focus phase that suppressed reminders is over, so a missed one can
		// be delivered now.
		a.makeUpMissedReminder()
	}

	a.DebugLog(fmt.Sprintf("pomodoro: %s ended -> %s (missed=%v)", out.EndedPhase, out.NextPhase, out.Missed))
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
		st = pause(st, time.Now()).State
		st.Enabled = false
	}
	if enabled {
		// Switching back on picks the session up where it was left, and catches
		// up on any phase that ran out while the feature was off.
		out := reconcile(st, a.store.Config(), time.Now())
		if out.finished() {
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
		// The summary prompt belongs to the feature being switched off.
		st.PendingRecordID = ""
		_ = a.store.SavePomodoro(st)
		a.clearBubbleLocked(BubbleSummary)
	}
	a.pomoMu.Unlock()

	a.emitPomodoro()
	a.emitLayout()
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
	defer a.pomoMu.Unlock()

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
			return State{}, fmt.Errorf("未找到该待办")
		}
	}

	st.Enabled = true
	now := time.Now()

	// Continuing a session is the default when the same todo comes back: "stop"
	// means "pause this session", not "throw it away"
	// (docs/reminder-pomodoro.md 13.1).
	if st.Phase == store.PhaseFocus && !st.IsRunning() && st.CurrentTodoID == in.TodoID && Remaining(st, now) > 0 {
		out := resume(st, cfg, now)
		if err := a.store.SavePomodoro(out.State); err != nil {
			return State{}, err
		}
		a.clearAllBubblesLocked()
		a.emitPomodoro()
		a.emitLayout()
		return a.GetState()
	}

	out := startFocus(st, cfg, in.TodoID, title, now)
	if err := a.store.SavePomodoro(out.State); err != nil {
		return State{}, err
	}
	// Starting a focus phase takes priority over whatever the bubble was saying:
	// a reminder left on screen would sit beside a pet that has just been told
	// not to be disturbed (docs 9).
	a.clearAllBubblesLocked()
	a.emitPomodoro()
	a.emitLayout()
	a.DebugLog(fmt.Sprintf("pomodoro: focus started for %q", title))
	return a.GetState()
}

// clearAllBubblesLocked dismisses whatever bubble is up.
func (a *App) clearAllBubblesLocked() {
	for _, kind := range []string{BubblePick, BubbleSummary, BubbleReminder} {
		a.clearBubbleLocked(kind)
	}
}

// PausePomodoro halts the countdown, keeping the remainder.
func (a *App) PausePomodoro() (State, error) {
	return a.transition(func(st store.PomodoroState, cfg store.Config, now time.Time) phaseOutcome {
		return pause(st, now)
	})
}

// ResumePomodoro restarts a paused or stopped phase.
func (a *App) ResumePomodoro() (State, error) {
	return a.transition(func(st store.PomodoroState, cfg store.Config, now time.Time) phaseOutcome {
		return resume(st, cfg, now)
	})
}

// StopPomodoro halts the phase like pause does, but is offered to the user as
// "stop" - the session can still be continued afterwards.
func (a *App) StopPomodoro() (State, error) {
	return a.transition(func(st store.PomodoroState, cfg store.Config, now time.Time) phaseOutcome {
		return stop(st, now)
	})
}

// SkipPhase abandons the current phase and moves to the next one.
//
// Skipping a focus phase deliberately produces no record, which is why the UI
// confirms first.
func (a *App) SkipPhase() (State, error) {
	return a.transition(func(st store.PomodoroState, cfg store.Config, now time.Time) phaseOutcome {
		return skipPhase(st, cfg, now)
	})
}

// ResetPomodoro abandons the session entirely: this is the explicit "give up"
// that stop is not (docs/reminder-pomodoro.md 13.1). It lives in the settings
// tab so it cannot be hit by accident.
func (a *App) ResetPomodoro() (State, error) {
	a.pomoMu.Lock()
	defer a.pomoMu.Unlock()
	st := reset(a.store.Pomodoro()).State
	// Any pending summary prompt goes with it.
	st.PendingRecordID = ""
	if err := a.store.SavePomodoro(st); err != nil {
		return State{}, err
	}
	a.clearBubbleLocked(BubbleSummary)
	a.clearBubbleLocked(BubblePick)
	a.emitPomodoro()
	a.emitLayout()
	return a.GetState()
}

// transition runs one state-machine step under the pomodoro lock and persists it.
func (a *App) transition(fn func(store.PomodoroState, store.Config, time.Time) phaseOutcome) (State, error) {
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
	// Answering the summary frees the bubble slot, so a reminder owed from the
	// start of the break can now be delivered.
	a.makeUpMissedReminder()
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
		Title:      "这次要专注做哪件事？",
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
	if !hasPendingSummary(st) {
		return
	}
	// Fill in the associated todo's title from the record, so a todo renamed
	// since the session started cannot change what the prompt refers to.
	title := st.CurrentTodoTitle
	for _, r := range a.store.PomodoroRecords() {
		if r.ID == st.PendingRecordID {
			title = r.TodoTitle
			break
		}
	}
	a.showBubble(&BubbleView{
		Kind:      BubbleSummary,
		Title:     "这一个番茄做了什么？",
		TodoTitle: title,
		RecordID:  st.PendingRecordID,
	})
}

// DismissBubble closes whatever bubble is up, EXCEPT a summary prompt that is still
// waiting for its record.
//
// The summary prompt is deliberately not dismissable. It is the gate on the rest:
// the break does not start until this question is answered, so a prompt that could
// be waved away - by Escape, by dragging the pet, or by a stray click - would make
// the gate meaningless, and the user would be left with a stopped timer and nothing
// on screen explaining why. Answering it is a real choice, and it is one click: both
// buttons ("保存" and "跳过") resolve it.
//
// This replaces an earlier design in which a dismissal was treated as "skip". That
// kept the timer unstuck, but it also made the choice implicit - and the user asked
// for the opposite: an explicit decision before the rest begins.
//
// Every other bubble kind is dismissed as before. A reminder, in particular, still
// auto-hides, because it asks nothing.
func (a *App) DismissBubble() (Layout, error) {
	// The gate's state is read BEFORE a.mu is taken, and that ordering is
	// load-bearing rather than tidiness: tickPomodoro holds pomoMu while calling
	// into the bubble helpers (completePhaseLocked -> showSummaryBubble ->
	// showBubble, which takes a.mu), so taking pomoMu while holding a.mu here would
	// be a lock-order inversion - and a real deadlock, not a theoretical one.
	pending := a.pendingSummaryWaiting()

	a.mu.Lock()
	b := a.bubble
	// A pending summary is not dismissable. Reporting the CURRENT layout rather
	// than an error keeps the contract simple for the frontend: it re-applies
	// geometry and the bubble stays exactly where it is.
	if b != nil && b.Kind == BubbleSummary && pending {
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

// showBubble replaces any current bubble and re-crops the window around it.
//
// A bubble and the panel are mutually exclusive: both live in the same slot
// beside the pet, and the region must describe exactly one of them. The live
// conversation overlay is the third occupant of that slot, so it is folded away
// here too.
//
// Callers that show something merely informational (the reminder) must check
// bubbleAwaitingAnswer first, so a display-only bubble can never displace a
// question the user has not answered yet.
func (a *App) showBubble(b *BubbleView) (Layout, error) {
	a.mu.Lock()
	a.panelOpen = false
	wasLive := a.liveOpen
	a.setLiveLocked(false)
	a.setBubbleLocked(b)
	a.mu.Unlock()
	if wasLive {
		a.emitLive(false)
	}
	a.emitBubble(b.Kind, true)
	return a.applyWindowGeometry()
}

// unansweredQuestionKind reports which bubble is holding the slot with a question
// the user has not answered, or BubbleNone when none is.
//
// The picker and the summary prompt are the two. The reminder is just a
// notification and the live overlay shows a reply, so neither asks anything; a
// surface may replace those, but not a question.
//
// The KIND is returned rather than a bool because every surface that is refused
// the slot has to explain itself, and "select a todo" and "write the summary"
// are not the same sentence.
func (a *App) unansweredQuestionKind() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.bubble == nil {
		return BubbleNone
	}
	if a.bubble.Kind == BubblePick || a.bubble.Kind == BubbleSummary {
		return a.bubble.Kind
	}
	return BubbleNone
}

// bubbleAwaitingAnswer reports whether the bubble on screen is waiting for the
// user to choose or type something.
//
// The picker and the summary prompt hold a decision the user has not made; the
// reminder is just a notification. That asymmetry is why a reminder must never
// overwrite them, while starting a focus phase (which dismisses everything) is
// allowed to.
//
// It is also why the chat popup must not take the slot from them - see OpenLive.
// The overlay occupies the same rectangle and has nowhere else to go, so opening
// it over a question does not cover the question, it destroys it.
func (a *App) bubbleAwaitingAnswer() bool {
	return a.unansweredQuestionKind() != BubbleNone
}

// questionOwnsTheSlot is the reason a surface is refused the slot when a bubble
// is already waiting for the user's answer.
//
// It names the consequence rather than the rule: the summary prompt is the gate
// on the rest, so a user who cannot see why the pet will not open the
// conversation has no way to know that the timer is parked behind that question.
func questionOwnsTheSlot(kind string) string {
	if kind == BubblePick {
		return "番茄钟还在等你选一件事，先处理它再打开对话"
	}
	return "番茄钟还在等你写总结，先答完它再打开对话"
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
	a.emitEvent("pet:pomodoro", a.pomodoroView())
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
