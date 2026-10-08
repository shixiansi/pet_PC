package main

import (
	"fmt"
	"math/rand"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/store"
)

// The reminder scheduler.
//
// The decision of whether to interrupt is a pure function - decideReminder - so
// the entire gating ruleset can be tested without a clock, a window or a running
// pet. This file's other job is to feed it a tick every 30 seconds and act on the
// verdict.
//
// The rules, from docs/reminder-pomodoro.md 5.2:
//
//	focusing          -> never (record the miss, make it up when resting)
//	resting           -> yes, on the interval
//	paused / stopped  -> yes, on the interval (a paused pomodoro is not focus)
//	pomodoro off      -> yes, on the interval
//	full screen       -> never
//	outside active hours -> never
//	nothing to show   -> never, unless told otherwise
//
// A manual "remind me now" bypasses every one of these except the content check.

// reminderCheckEvery is how often the scheduler evaluates. It is deliberately
// much shorter than the shortest interval so the interval itself is honoured to
// within half a minute regardless of when the tick lands.
const reminderCheckEvery = 30 * time.Second

// reminderDecision is the scheduler's verdict for one tick.
type reminderDecision struct {
	// Fire is true when a bubble should be shown now.
	Fire bool
	// Reason explains the verdict, for the debug log. It is the single most
	// useful thing when a reminder "did not appear": it says which gate closed.
	Reason string
	// Missed is true when a reminder came due but was suppressed because a focus
	// phase was running. It is made up once the phase ends.
	Missed bool
	// NextIn is how long to wait before evaluating again.
	NextIn time.Duration
}

// decideReminder evaluates one tick.
//
// now is the current time, dueAt is when the next reminder is scheduled (zero
// when none is pending) and lastFired is when the previous one appeared.
func decideReminder(cfg store.Config, st store.PomodoroState, now, dueAt time.Time, jitter func() time.Duration, fullscreen bool) reminderDecision {
	rem := cfg.Reminder()

	if !rem.Enabled {
		return reminderDecision{Reason: "reminders are off"}
	}
	// A focus phase suppresses everything automatic. The miss is recorded by the
	// caller so it can be made up when the phase ends.
	if st.IsFocusing() {
		return reminderDecision{Reason: "a focus phase is running", Missed: true}
	}
	// A break is the best moment to interrupt, but only if the user wants to be
	// interrupted while resting: this is the RemindDuringBreak switch
	// (docs 4.8), and it has to be enforced here or the setting is decorative.
	if store.IsBreakPhase(st.Phase) && !rem.DuringBreak {
		return reminderDecision{Reason: "resting, and reminders during breaks are off"}
	}
	if fullscreen {
		return reminderDecision{Reason: "a full-screen window is in front", Missed: true}
	}
	if !store.WithinActiveWindow(rem.From, rem.To, now) {
		return reminderDecision{Reason: fmt.Sprintf("outside the active window %s-%s", rem.From, rem.To)}
	}
	if dueAt.IsZero() {
		// Nothing scheduled yet: schedule the first one rather than firing
		// immediately, so switching reminders on does not interrupt at once.
		return reminderDecision{Reason: "scheduled", NextIn: nextInterval(rem, jitter)}
	}
	if now.Before(dueAt) {
		return reminderDecision{Reason: "waiting", NextIn: dueAt.Sub(now)}
	}
	return reminderDecision{Fire: true, Reason: "due"}
}

// nextInterval is the gap until the next reminder: the configured interval plus
// a random 0..jitter slack, so the pet never becomes a metronome
// (docs/reminder-pomodoro.md 5.3).
func nextInterval(rem store.ReminderSettings, jitter func() time.Duration) time.Duration {
	base := time.Duration(rem.IntervalMin) * time.Minute
	if base <= 0 {
		base = time.Duration(store.DefaultReminderIntervalMin) * time.Minute
	}
	if jitter == nil {
		return base
	}
	return base + jitter()
}

// jitterFunc builds the random-slack generator for a configured maximum.
//
// It returns a function rather than a duration so that each interval draws its
// own value: drawing once at startup would just be a random constant offset.
func jitterFunc(maxMinutes int) func() time.Duration {
	if maxMinutes <= 0 {
		return func() time.Duration { return 0 }
	}
	span := time.Duration(maxMinutes) * time.Minute
	return func() time.Duration {
		// Second-level granularity is plenty for a reminder and makes the
		// distribution easy to reason about.
		return time.Duration(rand.Int63n(int64(span/time.Second)+1)) * time.Second
	}
}

// --- view ------------------------------------------------------------------

// ReminderView is the scheduler's state as the frontend sees it.
type ReminderView struct {
	Enabled bool `json:"enabled"`
	// NextAt is when the next automatic reminder is due (RFC3339, "" if none).
	NextAt string `json:"nextAt,omitempty"`
	// NextInSec is the same thing as a countdown, for display.
	NextInSec int `json:"nextInSec"`
	// Suppressed explains why nothing will fire right now, "" when it will.
	Suppressed string `json:"suppressed,omitempty"`
	// Missed is true when a reminder was suppressed by a focus phase and is owed.
	Missed bool `json:"missed"`
}

// reminderView renders the scheduler for the frontend, including whether it is
// currently suppressed - the answer to "why didn't it pop up?".
func (a *App) reminderView() ReminderView {
	cfg := a.store.Config()
	rem := cfg.Reminder()

	a.remMu.Lock()
	nextAt := a.remNextAt
	missed := a.remMissed
	a.remMu.Unlock()

	view := ReminderView{Enabled: rem.Enabled, Missed: missed}

	if !rem.Enabled {
		return view
	}
	now := time.Now()
	if !nextAt.IsZero() {
		view.NextAt = nextAt.Format(time.RFC3339)
		if d := nextAt.Sub(now); d > 0 {
			view.NextInSec = int(d.Seconds())
		}
	}
	switch {
	case a.store.Pomodoro().IsFocusing():
		view.Suppressed = "专注中"
	case desktop.IsForegroundFullscreen(a.windowHandle()):
		view.Suppressed = "全屏中"
	case !store.WithinActiveWindow(rem.From, rem.To, now):
		view.Suppressed = "不在活跃时段"
	}
	return view
}

// --- scheduling ------------------------------------------------------------

// initReminders applies the startup preference and schedules the first check.
func (a *App) initReminders() {
	cfg, err := a.store.UpdateConfig(func(c *store.Config) { c.ReminderEnabled = c.ReminderOnStartup })
	if err != nil {
		a.DebugLog("reminder: could not apply the startup preference: " + err.Error())
		return
	}
	a.DebugLog(fmt.Sprintf("reminder: enabled=%v interval=%dm jitter=%dm",
		cfg.ReminderEnabled, cfg.ReminderIntervalMin, cfg.ReminderJitterMaxMin))
}

// startReminderLoop drives the scheduler.
//
// The stop channel is captured once for the same reason as the pomodoro loop: a
// field read inside the loop would race shutdown closing (and then clearing) it.
func (a *App) startReminderLoop() {
	stop := a.remStop
	go func() {
		ticker := time.NewTicker(reminderCheckEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				a.tickReminder()
			}
		}
	}()
}

// tickReminder evaluates the schedule and shows a bubble when it says to.
func (a *App) tickReminder() {
	cfg := a.store.Config()
	rem := cfg.Reminder()
	now := time.Now()

	a.remMu.Lock()
	dueAt := a.remNextAt
	a.remMu.Unlock()

	decision := decideReminder(cfg, a.store.Pomodoro(), now, dueAt, jitterFunc(rem.JitterMax), desktop.IsForegroundFullscreen(a.windowHandle()))

	a.remMu.Lock()
	if decision.Missed {
		a.remMissed = true
	}
	a.remMu.Unlock()

	if decision.NextIn > 0 {
		a.setNextReminder(now.Add(decision.NextIn))
	}
	if !decision.Fire {
		// The reason is logged every tick on purpose: when a reminder does not
		// appear, this line says which gate closed (docs 11, step 2).
		a.DebugLog("reminder: no (" + decision.Reason + ")")
		return
	}
	// The panel already shows every todo, so a bubble beside it would be
	// redundant - and the two cannot be visible at once anyway.
	if a.panelIsOpen() {
		a.DebugLog("reminder: suppressed (the panel is open)")
		return
	}
	if !a.showReminderBubble(false) {
		// Nothing worth showing: the interval restarts so the pet does not retry
		// on every tick.
		a.DebugLog("reminder: nothing to show")
	}
}

// RemindNow shows a reminder immediately, ignoring the schedule and every
// suppression except the content check.
//
// This is the manual escape hatch the design calls for: "manual wins" even in
// the middle of a focus phase (docs/reminder-pomodoro.md 5.2).
func (a *App) RemindNow() (State, error) {
	a.DebugLog("reminder: manual")
	if !a.showReminderBubble(true) {
		st, _ := a.GetState()
		return st, fmt.Errorf("没有需要提醒的待办")
	}
	// A manual reminder restarts the interval, so it is not immediately followed
	// by an automatic one.
	cfg := a.store.Config()
	a.setNextReminder(time.Now().Add(nextInterval(cfg.Reminder(), jitterFunc(cfg.ReminderJitterMaxMin))))
	return a.GetState()
}

// showReminderBubble shows the todo reminder bubble. It reports whether there was
// anything to show.
//
// The content gate applies to manual reminders too: "remind me now" overrides the
// schedule and the focus suppression, but it cannot conjure up something to say.
// With RemindWhenEmpty on, an empty list is itself the message.
func (a *App) showReminderBubble(manual bool) bool {
	// Never displace a question the user has not answered. The picker and the
	// summary prompt hold a decision; a reminder is only a notification, and
	// letting it overwrite either one would silently lose the question (and, for
	// the summary, leave the session's prompt unreachable). A manual reminder is
	// no exception: the user asking to be reminded is not asking to cancel the
	// picker they just opened.
	if a.bubbleAwaitingAnswer() {
		a.DebugLog("reminder: suppressed (a bubble is waiting for an answer)")
		return false
	}
	// Nor may it displace the live conversation. The overlay is a transient
	// surface holding the round the user is in the middle of reading, and a
	// reminder is at best redundant there - the pet is already the thing they are
	// looking at. Manual is no exception either, for the same reason it is not
	// one for the picker.
	if a.liveIsOpen() {
		a.DebugLog("reminder: suppressed (the live conversation is open)")
		return false
	}
	// A bubble is drawn inside the window, so with the pet hidden it would be a
	// notification nobody can see - and the panel check below cannot catch it,
	// because hiding sets the panel closed. The reminder is dropped rather than
	// deferred: the pet is out of the way on purpose, and it will have another
	// turn at the next interval.
	if a.petHidden() {
		a.DebugLog("reminder: suppressed (the pet is hidden)")
		return false
	}

	cfg := a.store.Config()
	rem := cfg.Reminder()
	now := time.Now()

	items := a.store.ReminderItems(rem.MaxItems, now)
	if len(items) == 0 && !rem.WhenEmpty {
		return false
	}

	bubbleItems := make([]BubbleItem, 0, len(items))
	for _, t := range items {
		bubbleItems = append(bubbleItems, BubbleItem{ID: t.ID, Title: t.Title, Due: dueLabel(t), Status: dueStatusOf(t)})
	}
	openCount := a.store.Stats().Open
	more := openCount - len(bubbleItems)
	if more < 0 {
		more = 0
	}

	title := "今天要做的事"
	if len(bubbleItems) == 0 {
		title = "现在没有待办"
	}
	a.showBubble(&BubbleView{
		Kind:  BubbleReminder,
		Title: title,
		Items: bubbleItems,
		More:  more,
	})

	// Spoken AFTER the bubble is up, so a slow synthesis cannot delay the visual
	// notice - and so the two arrive together rather than the voice coming first.
	// The counts come from Stats rather than from the items: the bubble is capped
	// at MaxItems, so counting the rows would understate a long list.
	stats := a.store.Stats()
	a.speakReminderLine(stats.DueSoon, stats.Overdue, stats.Open)

	a.remMu.Lock()
	a.remMissed = false
	a.remMu.Unlock()
	a.DebugLog(fmt.Sprintf("reminder: shown (%d items, manual=%v)", len(bubbleItems), manual))
	return true
}

// setNextReminder records when the next automatic reminder is due.
//
// remMu guards both the schedule and the missed flag: the scheduler goroutine
// writes them while webview calls read them, and they are read together when
// rendering the status line.
func (a *App) setNextReminder(at time.Time) {
	a.remMu.Lock()
	a.remNextAt = at
	a.remMu.Unlock()
}

// panelIsOpen reports whether the todo panel is currently showing.
//
// The panel and a bubble cannot both be visible, so a reminder that comes due
// while the user is already looking at their todos is redundant.
func (a *App) panelIsOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.panelOpen
}

// ReminderSettingsInput carries the reminder preferences from the settings tab.
type ReminderSettingsInput struct {
	OnStartup   bool   `json:"onStartup"`
	Enabled     bool   `json:"enabled"`
	IntervalMin int    `json:"intervalMin"`
	JitterMax   int    `json:"jitterMax"`
	From        string `json:"from"`
	To          string `json:"to"`
	MaxItems    int    `json:"maxItems"`
	AutoHideSec int    `json:"autoHideSec"`
	WhenEmpty   bool   `json:"whenEmpty"`
	SilentFull  bool   `json:"silentFullscreen"`
}

// SaveReminderSettings persists the reminder preferences and reschedules.
func (a *App) SaveReminderSettings(in ReminderSettingsInput) (State, error) {
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.ReminderOnStartup = in.OnStartup
		c.ReminderEnabled = in.Enabled
		c.ReminderIntervalMin = in.IntervalMin
		c.ReminderJitterMaxMin = in.JitterMax
		c.ReminderFrom = in.From
		c.ReminderTo = in.To
		c.ReminderMaxItems = in.MaxItems
		c.ReminderAutoHideSec = in.AutoHideSec
		c.RemindWhenEmpty = in.WhenEmpty
	}); err != nil {
		return State{}, err
	}
	cfg := a.store.Config()
	// Restart the countdown from now so a changed interval takes effect for the
	// next reminder rather than after the old one.
	a.setNextReminder(time.Now().Add(nextInterval(cfg.Reminder(), jitterFunc(cfg.ReminderJitterMaxMin))))
	return a.GetState()
}

// SetReminderEnabled switches reminders on or off from the panel or the tray.
func (a *App) SetReminderEnabled(enabled bool) (State, error) {
	cfg, err := a.store.UpdateConfig(func(c *store.Config) { c.ReminderEnabled = enabled })
	if err != nil {
		return State{}, err
	}
	if enabled {
		a.setNextReminder(time.Now().Add(nextInterval(cfg.Reminder(), jitterFunc(cfg.ReminderJitterMaxMin))))
	} else {
		a.setNextReminder(time.Time{})
		a.clearBubble(BubbleReminder)
	}
	a.emitLayout()
	return a.GetState()
}

// SetBubbleHover pauses or resumes a reminder bubble's auto-hide countdown.
//
// The countdown itself lives in the frontend (it owns the timer that fades the
// bubble), so this only records the fact for the Go side's own bookkeeping and
// keeps the two in step.
func (a *App) SetBubbleHover(hovered bool) {
	a.remMu.Lock()
	a.remHovered = hovered
	a.remMu.Unlock()
}

// ConfirmPomodoroStart starts focusing on the todo the picker selected.
//
// It is separate from StartPomodoro so the frontend's confirm button has a
// single, explicit call to make: the picker never starts a session on its own.
func (a *App) ConfirmPomodoroStart(todoID string) (State, error) {
	return a.StartPomodoro(PomodoroStartInput{TodoID: todoID})
}

// OpenTodoPanelFromBubble handles a click on a reminder bubble: the bubble goes
// away and the todo panel opens in its place, so the user can act immediately.
func (a *App) OpenTodoPanelFromBubble() (Layout, error) {
	a.mu.Lock()
	// Go through the invariant helper so the layout cannot keep advertising the
	// bubble that the panel is replacing.
	a.setBubbleLocked(nil)
	a.panelOpen = true
	// The overlay shares the slot as well; the panel is the record view the user
	// asked for, so it wins.
	wasLive := a.liveOpen
	a.setLiveLocked(false)
	a.mu.Unlock()
	if wasLive {
		a.emitLive(false)
	}
	a.emitBubble(BubbleReminder, false)
	return a.applyWindowGeometry()
}

// shutdown stops the background loops.
//
// The channels are closed but deliberately NOT cleared: the loops hold their own
// reference to them, and clearing the fields would only mean a second call could
// not close anything. Closing twice would panic, so the closer is made
// idempotent with a dedicated flag under its own mutex.
func (a *App) shutdown() {
	a.shutdownOnce.Do(func() {
		close(a.pomoStop)
		close(a.remStop)
		// An in-flight chat request holds a socket and a goroutine that emits
		// events into a ctx that is going away. Cancelling it here is what keeps
		// Quit from appearing to hang until the transport's own timeout
		// (docs/llm-chat.md 10, "生成中关闭应用").
		a.cancelChat()
		// Any playing line is stopped, so the pet does not keep talking after its
		// window has gone. There is nothing else to tear down: the clips are fixed
		// and there are no workers to exit, which is one of the things the
		// fixed-line design bought.
		a.stopVoice()
		// The global shortcuts are released explicitly rather than left to process
		// exit: a hot key owned by a dead process can stay reserved until Windows
		// notices, so the user's combination would be dead for a moment after they
		// quit the pet.
		a.closeHotkeys()
	})
}

// logReminderState writes the scheduler's view to the debug log. It exists so
// step 2 of the implementation plan can be verified from the log alone, before
// any bubble UI is trusted.
func (a *App) logReminderState() {
	v := a.reminderView()
	a.DebugLog(fmt.Sprintf("reminder: enabled=%v next=%s in=%ds suppressed=%q missed=%v",
		v.Enabled, v.NextAt, v.NextInSec, v.Suppressed, v.Missed))
}

// makeUpMissedReminder shows the reminder that a focus phase suppressed, if there
// was one.
//
// Called when a bubble that was occupying the slot goes away, so a missed
// reminder is delivered once rather than as a backlog
// (docs/reminder-pomodoro.md 4.7, 9).
func (a *App) makeUpMissedReminder() {
	a.remMu.Lock()
	missed := a.remMissed
	a.remMu.Unlock()
	if !missed {
		return
	}
	cfg := a.store.Config()
	if !cfg.ReminderEnabled {
		return
	}
	// Resting only counts as a make-up opportunity if the user allows reminders
	// during breaks. Leaving the flag set would deliver it at the next break
	// instead, which is not what the switch says.
	if store.IsBreakPhase(a.store.Pomodoro().Phase) && !cfg.RemindDuringBreak {
		a.remMu.Lock()
		a.remMissed = false
		a.remMu.Unlock()
		return
	}
	if a.panelIsOpen() {
		return
	}
	if !a.showReminderBubble(true) {
		return
	}
	a.remMu.Lock()
	a.remMissed = false
	a.remMu.Unlock()
	// The interval restarts from the make-up reminder.
	a.setNextReminder(time.Now().Add(nextInterval(cfg.Reminder(), jitterFunc(cfg.ReminderJitterMaxMin))))
}

// noteBreakReminderOwed records that entering a rest should pop a reminder.
//
// docs 5.2 makes "the pomodoro entered a break" an immediate trigger rather than
// a scheduled one. The summary prompt is occupying the bubble slot at that exact
// moment, so the reminder is marked as owed and delivered as soon as that prompt
// is answered - the break itself is already counting down behind it.
func (a *App) noteBreakReminderOwed() {
	cfg := a.store.Config()
	if !cfg.ReminderEnabled || !cfg.RemindDuringBreak {
		return
	}
	a.remMu.Lock()
	a.remMissed = true
	a.remMu.Unlock()
}
