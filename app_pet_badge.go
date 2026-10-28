package main

// The one line of text the pet draws inside its OWN box, above its head.
//
// # WHY THIS IS A HOST FEATURE AND NOT A SURFACE BLOCK
//
// A plugin's surface lives in the slot beside the pet, and that slot holds ONE thing at a time:
// opening the todo list closes the pomodoro card, and vice versa. A countdown that has to stay
// visible WHILE the user works cannot live there - it would fight every other plugin for the
// slot. The pet's own box is the only place that is always on screen and belongs to nobody.
//
// # WHY IT IS STILL GENERIC
//
// The host knows a STRING and nothing else: it never parses `24:59`, never knows what a pomodoro
// is, and never asks which plugin set it. Clearing is the empty string. That keeps "宿主不认识
// 任何具体的插件" intact while giving the plugin the one rectangle it cannot declare for itself.
//
// # WHY THE GRANT IS `pet.surface`
//
// A permission of its own would be the 14th entry in the contract, seven sync points, and - the
// part that decided it - a fresh authorisation dialog for every already-installed plugin that
// wants a clock. `pet.surface` already means "this plugin may put its own pixels next to the
// pet", and the badge is the same authority in a different rectangle. The method still goes
// through the gate like every other one.
//
// # WHY THE TEXT IS FILTERED AT READ TIME
//
// A plugin that is disabled or uninstalled stops calling, and a string it left behind would sit
// on the pet forever. The alternative - watching the plugin list - is the polling pattern this
// codebase just removed from the pomodoro. So the text is DERIVED: whoever reads it skips ids
// that are no longer installed and enabled, which makes every refresh self-correcting.

import (
	"sync"
)

// MaxBadgeRunes is what the host accepts: a clock, a count, maybe "24:59 专注". Long enough for
// any of those, short enough that the pet's box cannot be covered by text.
const MaxBadgeRunes = 16

var petBadge struct {
	mu sync.Mutex
	// byPlugin is keyed by plugin id so one plugin cannot overwrite another's line, and so the
	// filter below can drop a plugin that went away without knowing what it was showing.
	byPlugin map[string]string
	// last is the last text the frontend was told about, so a per-second push from a timer does
	// not repaint the box when the visible line did not change.
	last string
}

// setPetBadge records text for pluginID and tells the frontend when the visible line changed.
func (a *App) setPetBadge(pluginID, text string) {
	petBadge.mu.Lock()
	if petBadge.byPlugin == nil {
		petBadge.byPlugin = map[string]string{}
	}
	if text == "" {
		delete(petBadge.byPlugin, pluginID)
	} else {
		petBadge.byPlugin[pluginID] = text
	}
	petBadge.mu.Unlock()

	// Recomputed outside the lock: liveBadge asks the plugin manager, and holding the badge lock
	// across that would couple two unrelated locks for no reason.
	visible := a.liveBadge()

	petBadge.mu.Lock()
	changed := visible != petBadge.last
	petBadge.last = visible
	petBadge.mu.Unlock()

	// Emitted only when the text actually changed: a timer pushes once a second, and the box
	// would otherwise repaint 3600 times an hour for 60 distinct values.
	if changed {
		a.emitEvent("pet:badge", map[string]any{"text": visible})
	}
}

// liveBadge is the line that SHOULD be on the pet right now: the most recently set text among
// plugins that are still installed and enabled.
func (a *App) liveBadge() string {
	petBadge.mu.Lock()
	ids := make([]string, 0, len(petBadge.byPlugin))
	for id := range petBadge.byPlugin {
		ids = append(ids, id)
	}
	texts := make(map[string]string, len(petBadge.byPlugin))
	for id, text := range petBadge.byPlugin {
		texts[id] = text
	}
	petBadge.mu.Unlock()

	visible := ""
	for _, id := range ids {
		row, ok := a.pluginManager().Get(id)
		if !ok || !row.Enabled {
			// Gone or switched off: drop it so it cannot come back if the plugin is re-enabled
			// (it will set its own text again on the next tick).
			petBadge.mu.Lock()
			delete(petBadge.byPlugin, id)
			petBadge.mu.Unlock()
			continue
		}
		visible = texts[id]
	}
	return visible
}

// PetBadge is the frontend's read of the current line: first paint, and after a reload.
//
// It filters like every other read (see liveBadge), so a pet that was hidden while its plugin got
// uninstalled comes back clean.
func (a *App) PetBadge() string {
	visible := a.liveBadge()
	petBadge.mu.Lock()
	petBadge.last = visible
	petBadge.mu.Unlock()
	return visible
}
