package main

// Watching every installed plugin's settings file, and telling that plugin when it changes.
//
// # WHY THIS EXISTS (and why it is not the plugin's job)
//
// A plugin's settings are edited in ANOTHER PROCESS - the control center's 插件 page - while the
// frame that has to obey them (the pomodoro's headless timer) lives here, in the pet, for the
// whole session. The two programs share no IPC on purpose: they share the data directory and
// nothing else (AGENTS.md §2). So "the setting changed" is only observable as "the file changed".
//
// The first cut of this got the OWNER wrong: the pomodoro itself polled `config.get` every three
// seconds. That is a per-plugin timer doing the host's job - every plugin that wants to notice
// its own settings would grow one - and it produced the report 「设置了 5 分钟，停止后下一个
// 番茄钟还是 25 分钟」 for as long as the poll window was.
//
// So the host watches, once, for every plugin: `config.json` inside each plugin's private
// directory. This is the same pattern app_settingswatch.go already uses for the pet's own
// settings.json, and for the same reason.
//
// # WHAT IT COVERS
//
// Both editors, by construction: the control center writing through SaveConfig, and a plugin
// writing its own settings through `config.set`. Both land in the same file.
//
// # WHY IT CANNOT MISS A CHANGE
//
// It compares (mtime, size) per plugin. A rewrite that keeps the size AND the timestamp is
// possible in theory and not worth defending against here: the writer is a JSON encoder that
// rewrites the whole file, and a lost event only means the plugin keeps its old settings until
// the next edit - the same state the bug report was about, minus the polling.

import (
	"os"
	"path/filepath"
	"time"

	"PetPC/internal/plugins"
)

// pluginConfigWatchInterval is how often the config files are checked.
//
// Two seconds is the smallest value that is invisible to a user ("I changed it and pressed 开始"
// must not lose) without making the stat loop a thing anybody notices. It stats a handful of
// files; the previous arrangement ran one full `config.get` IPC round trip per plugin per three
// seconds.
const pluginConfigWatchInterval = 2 * time.Second

// configStamp is what "did this file change" is decided on.
type configStamp struct {
	mod  int64
	size int64
}

// configStamps reads one stamp per installed plugin, keyed by plugin id.
//
// A plugin with no config file yet has no entry, which is why changedConfigs treats "appeared"
// as a change: the first `config.set` from a workshop must wake the other frames too.
func (a *App) configStamps() map[string]configStamp {
	rows := a.pluginManager().Plugins()
	out := make(map[string]configStamp, len(rows))
	for _, row := range rows {
		dir, err := a.pluginManager().PrivateDir(row.ID)
		if err != nil {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, "config.json"))
		if err != nil {
			continue
		}
		out[row.ID] = configStamp{mod: info.ModTime().UnixNano(), size: info.Size()}
	}
	return out
}

// changedConfigs reports which plugin ids changed between two snapshots.
//
// Pure on purpose: the interesting part of this file is the comparison, and a test can assert it
// without a goroutine, a sleep or a filesystem.
func changedConfigs(prev, cur map[string]configStamp) []string {
	var out []string
	for id, now := range cur {
		if before, seen := prev[id]; !seen || before != now {
			out = append(out, id)
		}
	}
	return out
}

// startPluginConfigWatch begins the watch. Called once from the startup sequence.
//
// The first pass only SEEDS the snapshot: announcing every plugin's config at launch would tell
// each plugin "your settings changed" before it has finished its handshake, which is noise the
// plugin cannot act on.
func (a *App) startPluginConfigWatch() {
	go func() {
		prev := a.configStamps()
		ticker := time.NewTicker(pluginConfigWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				cur := a.configStamps()
				for _, id := range changedConfigs(prev, cur) {
					// Broadcast with the id in the payload: the subscription gate is the host's
					// (only subscribers get it), and each plugin compares against its own id.
					a.emitPluginStateChange(plugins.EventConfigChanged, map[string]any{"pluginId": id})
				}
				prev = cur
			}
		}
	}()
}
