package main

// The control center's half of "tell a plugin when something of its own changed".
//
// # WHY THE MARKET NEEDS THIS AT ALL
//
// The pet has watched plugin settings and private directories since 2026-10 (app_plugin_configwatch.go
// / app_plugin_storagewatch.go). This program only ever emitted `share.changed`, and that was written
// when its plugin page was a list of permissions. It now mounts the SAME documents the pet does
// (`cc/src/lib/plugin-frame.ts`), so a plugin's list or workshop can be open in a real window -
// and had no way to hear that its data changed, short of polling. That is exactly what todo did
// (a 2-second `storage.get`) and ast-coder did (a 5-second `config.get`).
//
// # WHY THE COMPARISON IS NOT WRITTEN HERE
//
// "What counts as changed" cannot have two answers: the same write would then mean different things
// in the two windows. The stamps and the comparison live in internal/plugins/watch.go, and this file
// is only the loop plus this program's emit channel. Same rule as the share tray (`RegisterShare`).
//
// # WHAT IT DOES NOT DO
//
// No `events.subscribe`: this program does not serve that method (AGENTS.md §3.3 - subscription
// gates belong to the pet). A frame here gets `config.changed` addressed to its own plugin id, which
// is what `plugin-frame.ts` routes on anyway.

import (
	"sort"
	"time"

	"PetPC/internal/plugins"
)

// pluginWatchInterval matches the pet's: small enough that a user never notices, large enough that
// stat-ing a handful of files is free.
const pluginWatchInterval = 2 * time.Second

// maxStorageEventsPerTick bounds one plugin's notifications in a single tick, for the same reason
// the pet does: a helper that writes a hundred files must not become a hundred events.
const maxStorageEventsPerTick = 8

// pluginEvent is one thing to tell one plugin, before it is turned into a Wails event.
type pluginEvent struct {
	ID      string
	Name    string
	Payload map[string]any
}

// marketEvents is the whole decision, as a pure function of two snapshots.
//
// seeded=false means "this is the first pass": announcing every plugin's state at launch would tell
// each frame "your data changed" before it finished its handshake - noise it cannot act on, and the
// same reason the pet's watchers only seed.
func marketEvents(
	prevCfg, curCfg map[string]plugins.FileStamp,
	prevStorage, curStorage map[string]map[string]plugins.FileStamp,
	seeded bool,
) []pluginEvent {
	if !seeded {
		return nil
	}
	var out []pluginEvent

	for _, id := range plugins.ChangedStamps(prevCfg, curCfg) {
		out = append(out, pluginEvent{ID: id, Name: plugins.EventConfigChanged,
			Payload: map[string]any{"pluginId": id}})
	}

	changed := plugins.ChangedStorageStamps(prevStorage, curStorage)
	ids := make([]string, 0, len(changed))
	for id := range changed {
		ids = append(ids, id)
	}
	// Deterministic emission order: two ticks that saw the same change must produce the same
	// sequence, so a test (and a reader of the log) does not have to sort.
	sort.Strings(ids)
	for _, id := range ids {
		keys := changed[id]
		if len(keys) > maxStorageEventsPerTick {
			keys = keys[:maxStorageEventsPerTick]
		}
		for _, key := range keys {
			// The tray is a FILE inside the publisher's own directory, so it shows up in this
			// walk too - and routing it as `share.changed` is what lets a publish made by the
			// OTHER program reach a frame mounted here (the pet's watcher does the mirror).
			if name, ok := plugins.ShareTrayName(key); ok {
				out = append(out, pluginEvent{ID: id, Name: plugins.EventShareChanged,
					Payload: map[string]any{
						"key":   plugins.SharedKey(id, name),
						"owner": id,
						"name":  name,
					}})
				continue
			}
			out = append(out, pluginEvent{ID: id, Name: plugins.EventStorageChanged,
				Payload: map[string]any{"key": key}})
		}
	}
	return out
}

// startPluginWatches begins both loops. Called once from startup.
func (m *Market) startPluginWatches() {
	if m.ctx == nil {
		return
	}
	go func() {
		prevCfg := m.configStamps()
		prevStorage := m.storageStamps()
		seeded := false
		ticker := time.NewTicker(pluginWatchInterval)
		defer ticker.Stop()
		for range ticker.C {
			curCfg := m.configStamps()
			curStorage := m.storageStamps()
			for _, event := range marketEvents(prevCfg, curCfg, prevStorage, curStorage, seeded) {
				m.emitPluginEvent(event.ID, event.Name, event.Payload)
			}
			prevCfg, prevStorage, seeded = curCfg, curStorage, true
		}
	}()
}

// configStamps reads one stamp per installed plugin.
func (m *Market) configStamps() map[string]plugins.FileStamp {
	rows := m.manager.Plugins()
	out := make(map[string]plugins.FileStamp, len(rows))
	for _, row := range rows {
		dir, err := m.manager.PrivateDir(row.ID)
		if err != nil {
			continue
		}
		if stamp, ok := plugins.ConfigStamp(dir); ok {
			out[row.ID] = stamp
		}
	}
	return out
}

// storageStamps walks every installed plugin's private directory.
func (m *Market) storageStamps() map[string]map[string]plugins.FileStamp {
	rows := m.manager.Plugins()
	out := make(map[string]map[string]plugins.FileStamp, len(rows))
	for _, row := range rows {
		dir, err := m.manager.PrivateDir(row.ID)
		if err != nil {
			continue
		}
		out[row.ID] = plugins.PrivateDirStamps(dir)
	}
	return out
}

// sortStrings is sort.Strings without pulling the import into a file whose whole point is the
// pure decision above.
func sortStrings(items []string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j] < items[j-1]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
