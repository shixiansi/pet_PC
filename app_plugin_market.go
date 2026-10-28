package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// This file is the pet's side of the PLUGIN MARKET, which is a SEPARATE PROGRAM.
//
// The market used to be the pet's 「插件」tab. It could not stay there: the panel
// is a fixed 380x520 rectangle the OS crops the window to, and a manager needs a
// plugin list, a detail pane, a permission sheet and the plugin's own settings
// form on screen at the same time - the screenshots that prompted the split show
// permission rows clipped by the right edge and the market address box half
// visible. So the whole management surface moved into PetPCPluginMarket.exe, and
// what is left here is one launcher.
//
// It is deliberately parallel to app_voice_studio.go, down to the candidate
// order and the error wording: the two programs are the same kind of thing, and
// a user who has found one executable should be able to find the other the same
// way. The pet does NOT manage plugins any more, does NOT wait for the market and
// does NOT close it - the market is the user's own window, not a child job.
//
// What the pet keeps is the part that cannot be delegated: the in-page surfaces
// (the 对话 slot and the floating rectangles) and PluginRequest, the single entry
// point every plugin capability goes through. Those live in app_plugins.go and
// app_plugin_surface.go; nothing in this file resolves a permission.

// pluginMarketExeName is the standalone market's executable name.
const pluginMarketExeName = "PetPCPluginMarket.exe"

// pluginMarketOpenEnv asks the startup sequence to launch the market once.
//
// It exists ONLY so a pixel gate can drive the launch without a page control:
// this environment has no way to deliver a synthetic click to a control inside
// the webview (the click reaches the window but not the page), and the gate in
// .dev/verify-plugin-surface.ps1 has to prove that the market process really
// appears. It is not a user feature: unset, which is the default, the startup
// sequence behaves exactly as before and the settings button remains the only
// product entry point. It adds no binding and no menu item.
//
// The name is spelled once, in this constant, so that "how many places can
// launch the market at startup" is answerable by reading one line of source.
const pluginMarketOpenEnv = "PETPC_OPEN_PLUGIN_MARKET"

// pluginMarketState is the launcher's mutable state.
//
// It carries the process handle so a second click can be told apart from the
// first, and it carries the three external inputs as fields rather than calling
// os.Executable/os.Getwd/exec.Cmd.Start directly. That is not decoration: the
// candidate list and the launch are exactly what the tests must be able to point
// at a scratch directory and observe, and a test that cannot substitute them can
// only assert that SOME path was tried.
type pluginMarketState struct {
	mu sync.Mutex
	// launched is the market process this pet started, if any.
	launched *os.Process
	// exe and getwd default to os.Executable and os.Getwd.
	exe   func() (string, error)
	getwd func() (string, error)
	// launch starts an already-configured command and returns its handle. The
	// default starts it detached and reaps it in the background; a test replaces
	// it to record the command without spawning a window.
	launch func(cmd *exec.Cmd) (*os.Process, error)
}

// pluginMarketExe locates the pet's own executable, or the test's stand-in.
func (a *App) pluginMarketExe() (string, error) {
	a.pluginMarket.mu.Lock()
	fn := a.pluginMarket.exe
	a.pluginMarket.mu.Unlock()
	if fn == nil {
		fn = os.Executable
	}
	return fn()
}

// pluginMarketWorkdir is the working directory the source-tree candidate is
// resolved against.
func (a *App) pluginMarketWorkdir() (string, error) {
	a.pluginMarket.mu.Lock()
	fn := a.pluginMarket.getwd
	a.pluginMarket.mu.Unlock()
	if fn == nil {
		fn = os.Getwd
	}
	return fn()
}

// pluginMarketStart runs the launch seam.
//
// The command arrives fully configured (path and Dir), so the default and any
// substitute agree on everything except whether a real process appears - which
// is what lets a test assert cmd.Dir.
func (a *App) pluginMarketStart(cmd *exec.Cmd) (*os.Process, error) {
	a.pluginMarket.mu.Lock()
	fn := a.pluginMarket.launch
	a.pluginMarket.mu.Unlock()
	if fn == nil {
		fn = launchDetachedProcess
	}
	return fn(cmd)
}

// launchDetachedProcess starts a command in its own window and returns at once.
//
// "At once" is the point: the market is the user's window, so the pet must not
// block on it and must not tie its lifetime to it. The process is reaped in the
// background purely so it does not linger as a zombie for the rest of the pet's
// life - the wait is on a goroutine and nothing reads its result.
func launchDetachedProcess(cmd *exec.Cmd) (*os.Process, error) {
	cmd.SysProcAttr = detachedProcessAttr()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go cmd.Wait() //nolint:errcheck // the exit status is not actionable here
	return cmd.Process, nil
}

// PluginMarketCandidates lists where the market may be, most preferred first.
//
// The order is the release layout first (beside the pet, which is where a
// packaged build puts it) and then the build tree, so a development build finds
// the copy it just compiled. It is a separate, pure enumeration rather than a
// loop inside PluginMarketPath so the search order itself is testable - the
// failure it guards against ("the release puts it elsewhere and the pet can
// never find it") is invisible until a user hits it.
func (a *App) PluginMarketCandidates() []string {
	var out []string
	if exe, err := a.pluginMarketExe(); err == nil {
		// Beside the pet: the release layout, and also where `wails build` puts
		// a development binary's neighbours.
		out = append(out, filepath.Join(filepath.Dir(exe), pluginMarketExeName))
		// The market's own build output, for running the pet from the repository.
		out = append(out, filepath.Join(filepath.Dir(exe), "..", "tools", "plugin-market", "build", "bin", pluginMarketExeName))
	}
	// The source tree is not reachable from the data directory, so the working
	// directory is tried too: it is the repo root under `wails dev`.
	if wd, err := a.pluginMarketWorkdir(); err == nil {
		out = append(out, filepath.Join(wd, "tools", "plugin-market", "build", "bin", pluginMarketExeName))
	}
	return out
}

// PluginMarketPath returns the market's location, and whether it exists.
//
// The settings page shows the answer, because "the button did nothing" and "the
// market is not installed next to the pet" are different problems and only one
// of them is the user's to fix.
func (a *App) PluginMarketPath() (string, bool) {
	for _, p := range a.PluginMarketCandidates() {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true
		}
	}
	return "", false
}

// OpenPluginMarket launches the standalone market.
//
// Launching rather than instructing is the difference between a feature and a
// chore: the market is a separate program and the user should not have to find
// it in the file system. The process is started detached with its own window (no
// inherited console), and it is recorded so a second click is distinguishable
// from the first.
//
// The pet does NOT wait for it and does NOT shut it down. Both programs only
// ever share files - plugins.json, the plugin's private directory and the data
// directory - so neither has to be running for the other to work, and closing
// the pet must not close a window the user is still reading.
func (a *App) OpenPluginMarket() (State, error) {
	path, ok := a.PluginMarketPath()
	if !ok {
		return State{}, fmt.Errorf("找不到插件市场程序（%s）。它需要与桌宠放在同一个目录里，或者先用 wails build 构建 tools/plugin-market", pluginMarketExeName)
	}

	cmd := exec.Command(path)
	// The market resolves its own paths and its own frontend assets relative to
	// its executable, so it must be started from its own directory rather than
	// inheriting the pet's.
	cmd.Dir = filepath.Dir(path)
	proc, err := a.pluginMarketStart(cmd)
	if err != nil {
		return State{}, fmt.Errorf("启动插件市场失败：%w", err)
	}
	a.pluginMarket.mu.Lock()
	a.pluginMarket.launched = proc
	a.pluginMarket.mu.Unlock()
	a.log.Info("market", "已启动控制中心："+path)

	// The state snapshot is returned rather than a bare nil so the caller's
	// wiring is the same shape as every other settings action, and the button
	// handler can refresh from it without a second round trip.
	return a.GetState()
}

// startPluginMarketFromEnv runs the gate-only startup hook.
//
// Called once from the startup sequence. A failure is logged and swallowed: the
// hook exists to make a gate observable, and a machine without the market
// installed next to the pet must still start the pet normally.
func (a *App) startPluginMarketFromEnv() {
	if os.Getenv(pluginMarketOpenEnv) != "1" {
		return
	}
	if _, err := a.OpenPluginMarket(); err != nil {
		a.log.Warnf("market", "启动钩子（%s）没能打开控制中心：%v", pluginMarketOpenEnv, err)
	}
}
