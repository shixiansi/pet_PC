package main

import (
	"PetPC/internal/plugins"
)

// This file is the pet's end of the ONE-TIME move of its own AI settings into
// the conversation plugin's private storage.
//
// The move itself is internal/plugins/migrate.go, and it is shared: the market
// program calls the same function when IT starts, because either program may be
// the first one opened after the upgrade. What lives here is only the wiring the
// root package owns - which data directory, which secret keeper - plus the
// decision to run it once per launch and never let it stop the pet from
// appearing.
//
// The keeper is the same one the plugin host stores secrets with, and it is
// obtained WITHOUT opening the plugin manager: a machine that never installs a
// plugin should not grow a plugins/ directory just because an upgrade happened.
// See keeperLocked in app_plugins.go.

// migrateChatData carries the pet's legacy AI settings into the conversation
// plugin's private storage.
//
// A failure is logged and swallowed. The migration is written so that a failure
// leaves every source file exactly where it was (nothing is renamed until every
// target has been written), which means the next launch simply tries again; the
// only thing a hard failure here could add is a pet that refuses to start over a
// plugin's settings. A missing plugin is not a failure at all - it is the normal
// state of a machine that has not installed the conversation plugin yet, and the
// migration reports it as success.
func (a *App) migrateChatData() {
	if err := plugins.MigrateChatData(a.store.Dir(), a.pluginKeeper()); err != nil {
		a.log.Warnf("plugin", "迁移对话插件的数据失败（下次启动会重试）：%v", err)
	}
}

// pluginKeeper returns the plugin host's secret keeper, creating it on first use.
//
// It exists so the migration can reach the keeper through the host's own state
// (pluginHost.keeper) rather than constructing a second, independent store: two
// SecretStore values over the same file would still agree, but "which keeper does
// this program use" should have exactly one answer.
func (a *App) pluginKeeper() plugins.SecretKeeper {
	a.pluginHost.mu.Lock()
	defer a.pluginHost.mu.Unlock()
	return a.keeperLocked()
}
