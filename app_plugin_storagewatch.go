package main

// Telling a plugin that a file in its OWN private directory changed.
//
// # WHY THIS EXISTS
//
// The helper scripts a plugin ships (clipboard watcher, SMTC reader, CPU sampler) cannot talk to
// the plugin: they are separate processes writing files into the plugin's private directory
// (`PETPC_PLUGIN_DATA`). So the plugin POLLS - `storage.get 'outbox/clipboard'` once a second,
// forever. On 2026-10-31 that was measurable: the pet's log had 10 605 DEBUG `plugin` lines in one
// day and `storage.get` was half of them (pet-sense 660, media-sync 659, clipask 659, bilibili
// 659, privacy-guard 659, qq 485 ...), each answering the same value it answered a second earlier.
//
// `share.changed` (app_plugins.go) covers the cross-plugin half of the same problem. This file is
// the private half: the host already watches every plugin's `config.json` on a 2 s tick
// (app_plugin_configwatch.go) for exactly this reason, and the private directory is the same
// shape of problem with a different file.
//
// # WHY THE HOST AND NOT THE PLUGIN
//
// A plugin cannot watch a directory: `storage.*` is get/set/list/stat, and `fsnotify` is not a
// capability. Every plugin that wants to notice its own helper's output would have to grow a
// timer - which is what all of them did, and what this removes.
//
// # WHAT IT COSTS
//
// One recursive walk of each installed plugin's private directory every two seconds. It is
// bounded by what plugins actually keep there (tens of files; the QQ plugin's 437 files are the
// outlier and still cost well under a millisecond to stat). Compare with what it replaces: one
// IPC round trip per plugin per second, each one logged.
//
// # WHAT IT DELIBERATELY EXCLUDES
//
// `config.json`, because `config.changed` already reports it and two events for one write would
// make every plugin's handler run twice.

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"PetPC/internal/logging"
)

// pluginStorageWatchInterval matches the config watch: the same tick, the same reasoning ("small
// enough that a user never notices, large enough that the stat loop is free").
const pluginStorageWatchInterval = 2 * time.Second

// storageStamp is what "did this file change" is decided on, per file.
type storageStamp struct {
	mod  int64
	size int64
}

// maxStorageEventsPerTick bounds one plugin's notifications in a single tick.
//
// A helper that writes a hundred files at once (an import, a log rotation) must not turn into a
// hundred events in one frame's message queue: the plugin can always fall back to reading the
// directory, and the log says the burst was truncated.
const maxStorageEventsPerTick = 8

// pluginStorageStamps walks every installed plugin's private directory.
//
// The key is the STORAGE KEY the plugin would pass to `storage.get` (slash-separated, relative to
// the private directory), not the OS path: that is the string the plugin can act on.
func (a *App) pluginStorageStamps() map[string]map[string]storageStamp {
	rows := a.pluginManager().Plugins()
	out := make(map[string]map[string]storageStamp, len(rows))
	for _, row := range rows {
		dir, err := a.pluginManager().PrivateDir(row.ID)
		if err != nil {
			continue
		}
		stamps := map[string]storageStamp{}
		root := dir
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			key := filepath.ToSlash(rel)
			// config.json is `config.changed`'s business; reporting it here too would run every
			// plugin's settings handler twice for one save.
			if key == "config.json" {
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil
			}
			stamps[key] = storageStamp{mod: info.ModTime().UnixNano(), size: info.Size()}
			return nil
		})
		out[row.ID] = stamps
	}
	return out
}

// changedStorageKeys reports which (plugin, key) pairs changed between two snapshots.
//
// Pure on purpose: the comparison is the interesting part, and a test can assert it without a
// goroutine, a sleep or a filesystem. A key that APPEARED counts as changed - that is the first
// write of a helper's outbox, which is exactly the case a polling plugin could not tell from
// "still empty".
func changedStorageKeys(prev, cur map[string]map[string]storageStamp) map[string][]string {
	out := map[string][]string{}
	for id, now := range cur {
		before := prev[id]
		for key, stamp := range now {
			if old, seen := before[key]; !seen || old != stamp {
				out[id] = append(out[id], key)
			}
		}
	}
	return out
}

// startPluginStorageWatch begins the watch. Called once from the startup sequence.
//
// The first pass only SEEDS the snapshot, for the same reason the config watch does: announcing
// every existing file at launch would tell each plugin its outbox "changed" before it has
// finished its handshake.
func (a *App) startPluginStorageWatch() {
	go func() {
		prev := a.pluginStorageStamps()
		ticker := time.NewTicker(pluginStorageWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				prev = a.emitChangedStorage(prev)
			}
		}
	}()
}

// emitChangedStorage announces what changed and returns the new snapshot.
//
// Split out of the goroutine so a test can drive exactly one tick.
func (a *App) emitChangedStorage(prev map[string]map[string]storageStamp) map[string]map[string]storageStamp {
	cur := a.pluginStorageStamps()
	for id, keys := range changedStorageKeys(prev, cur) {
		// Sorted for a stable order: a plugin that logs these sees the same sequence twice, and a
		// test does not have to sort.
		sort.Strings(keys)
		if len(keys) > maxStorageEventsPerTick {
			a.log.Pluginf(logging.LevelWarn, "plugin", id,
				"私有目录一次变了 %d 个文件，只通知前 %d 个（其余请自己 storage.list）",
				len(keys), maxStorageEventsPerTick)
			keys = keys[:maxStorageEventsPerTick]
		}
		at := time.Now().Format(time.RFC3339)
		for _, key := range keys {
			// Addressed to ONE plugin: this is its own directory, and nobody else may read it.
			a.emitPluginEvent(id, "storage.changed", map[string]any{"key": key, "at": at})
		}
	}
	return cur
}

