package main

import (
	"fmt"
	"time"

	"PetPC/internal/store"
)

// The reminder BUBBLE, and the one reminder the host still sends.
//
// The automatic scheduler that used to live here is gone: decideReminder (a pure function), the 30
// second tick, the interval + jitter arithmetic, the catch-up for a reminder a focus phase
// suppressed, and the break trigger that queued one when a rest began. Reminders belong to the todo
// plugin (market/plugins/com.shixiansi.plugin-todo) now: its headless frame reads the same todo list,
// applies the user's interval / jitter / hours / limit and calls `bubble.say`. Two schedulers over
// one list meant the user could be reminded twice, which is why this is a deletion and not a
// tidy-up - and a reminder the host held back for a later moment would fire outside the plugin's
// schedule, which is that same double reminder in another form.
//
// The "next reminder at" strip went with it (ReminderView / reminderView / logReminderState): it
// described a due time the host no longer computes, so it could only ever show a stale one. The todo
// plugin knows when it will next speak; the host does not.
//
// What stays is the MANUAL reminder: "立刻提醒" from the tray menu and the pet's menu cell, plus the
// bubble it shows. That is a user action rather than a schedule, and it is the one path a plugin
// cannot take over - nothing outside the host can ask the host to remind *now* on the user's behalf.

// RemindNow shows a reminder immediately, from the tray menu or the pet's menu cell.
//
// This is the manual escape hatch the design calls for: "manual wins" even in the
// middle of a focus phase (docs/reminder-pomodoro.md 5.2). It is also the only reminder the host
// still sends, because the schedule belongs to the todo plugin.
func (a *App) RemindNow() (State, error) {
	a.DebugLog("reminder: manual")
	if !a.showReminderBubble(true) {
		st, _ := a.GetState()
		return st, fmt.Errorf("没有需要提醒的待办")
	}
	return a.GetState()
}

// showReminderBubble shows the todo reminder bubble. It reports whether there was
// anything to show.
//
// The content gate used to be the last of several: the schedule decided WHETHER to speak and this
// decided WHAT to say. With the schedule gone, all that is left to decide is "is there something to
// say?" - the slot gates below (an unanswered question, a plugin's surface, a hidden pet) are about
// where the bubble may appear, not about whether it should. With RemindWhenEmpty on, an empty list
// is itself the message.
func (a *App) showReminderBubble(manual bool) bool {
	// Nor may it displace a plugin's floating surface. That surface is a
	// transient thing holding the round the user is in the middle of reading,
	// and a reminder is at best redundant there - the pet is already the thing
	// they are looking at. Manual is no exception either, for the same reason it
	// is not one for the picker.
	//
	// The check reads the SLOT rather than a named feature: the surface used to
	// be the pet's own conversation overlay, and the rule is about what occupies
	// the slot, not about who put it there.
	a.mu.Lock()
	surfaceOpen := a.pluginSurface != nil
	a.mu.Unlock()
	if surfaceOpen {
		a.DebugLog("reminder: suppressed（对话浮层面打开中）")
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

	// The list comes from the todo plugin's published snapshot, NOT from the host's store:
	// the store's copy is the one that was migrated into the plugin, so reading it here
	// would remind the user about whatever the list looked like on the last launch.
	//
	// This is the only host-side feature that still needs the list, which is why it reads
	// the SHARED snapshot rather than anything private: the writing plugin made it
	// readable on purpose (that is what `share.set` means), and the pomodoro reads it for
	// the same reason.
	todos := a.sharedTodoList()
	items := reminderItems(todos, rem.MaxItems, now)
	if len(items) == 0 && !rem.WhenEmpty {
		return false
	}

	bubbleItems := make([]BubbleItem, 0, len(items))
	for _, t := range items {
		bubbleItems = append(bubbleItems, BubbleItem{ID: t.ID, Title: t.Title, Due: dueLabel(t), Status: dueStatusOf(t)})
	}
	openCount := openTodoCount(todos)
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
		// The list is what this bubble is about, so the button that opens it belongs here. Every
		// other `bubble.say` caller leaves it off (see BubbleView.ShowTodoAction).
		ShowTodoAction: true,
	})

	// Announced AFTER the bubble is up, so a slow reaction cannot delay the visual
	// notice - and so the two arrive together rather than the voice coming first.
	// The counts come from the whole snapshot rather than from the bubble's rows: the
	// bubble is capped at MaxItems, so counting the rows would understate a long list.
	//
	// The plugin event carries the three numbers and no wording. A plugin is free to
	// synthesize a sentence with the count in it, which is something the pet's own
	// fixed clips never could - they had no numbers. What to say remains the
	// plugin's business, which is the separation this change is for.
	dueSoon, overdue, open := reminderCounts(todos, now)
	a.notifyPluginReminderFired(dueSoon, overdue, open)

	a.DebugLog(fmt.Sprintf("reminder: shown (%d items, manual=%v)", len(bubbleItems), manual))
	return true
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

// SaveReminderSettings persists the reminder preferences.
//
// It writes the config and nothing else: the countdown these values describe is the todo plugin's,
// and the plugin reads them from the same settings file, so there is no host schedule to restart.
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
	return a.GetState()
}

// SetReminderEnabled switches reminders on or off from the panel or the tray.
func (a *App) SetReminderEnabled(enabled bool) (State, error) {
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.ReminderEnabled = enabled }); err != nil {
		return State{}, err
	}
	if !enabled {
		// Switching reminders off takes the bubble down with it; switching them on has nothing to
		// schedule here, because the schedule lives in the todo plugin.
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

// OpenTodoFromBubble handles a click on a reminder bubble: the bubble goes away and the todo
// plugin's surface opens in its place, so the user can act immediately.
//
// It used to be called OpenTodoPanelFromBubble and it opened the PANEL's 待办 tab. Both halves
// of that are gone - the tab with the panel, and the list into the todo plugin - so the binding
// is RENAMED rather than kept: a name that says "panel" would be the next person's reason to
// look for one.
//
// A failure is not reported as an error here. The bubble has already been folded by
// setBubbleLocked, so the state and the layout agree; what can fail is opening the surface, and
// OpenPluginSurface explains that in a bubble of its own (not enabled, no grant). Passing the
// error up would have the frontend toast it inside a panel that no longer exists.
func (a *App) OpenTodoFromBubble() (Layout, error) {
	a.mu.Lock()
	a.setBubbleLocked(nil)
	a.setPluginSurfaceLocked(nil)
	a.mu.Unlock()
	a.emitBubble(BubbleReminder, false)

	id := a.todoSlotPluginID()
	if id == "" {
		a.DebugLog("todo slot: no plugin claims it")
		return a.applyWindowGeometry()
	}
	if _, err := a.OpenPluginSurface(id); err != nil {
		a.DebugLog("todo slot: " + err.Error())
	}
	return a.applyWindowGeometry()
}
// shutdown stops the background loops.
//
// The channel that used to end the pomodoro ticker is gone with the timer itself,
// so what is left is the two things that are still owned by this process and must
// be handed back deliberately. The closer is made idempotent with a dedicated flag
// under its own mutex.
func (a *App) shutdown() {
	a.shutdownOnce.Do(func() {
		// There is nothing to tear down for speech any more: the pet does not play
		// the lines, the voice plugin does, and it owns its own audio sink and its
		// own lifetime. The pet's shutdown is now purely its timer loops and its
		// hot keys.
		//
		// The global shortcuts are released explicitly rather than left to process
		// exit: a hot key owned by a dead process can stay reserved until Windows
		// notices, so the user's combination would be dead for a moment after they
		// quit the pet.
		a.closeHotkeys()
		// Plugin WebSocket connections are closed here too, and NOT left to process
		// exit. A socket that dies with the process is indistinguishable - at the
		// far end - from one that dropped, so the service keeps the bot "online"
		// until it notices and, for a chat protocol side, may keep queueing events
		// at a session nobody is reading.
		a.closePluginWS()
	})
}
