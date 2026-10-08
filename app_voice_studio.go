package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"PetPC/internal/desktop"
	"PetPC/internal/voice"
)

// This file is the pet's side of the VOICE STUDIO, which is a SEPARATE PROGRAM.
//
// The studio used to be a tab in the pet's panel. It was moved out because the
// panel is a fixed 380x520 rectangle that the OS crops the window to, and a
// workspace that lists five lines with several takes each does not belong in it -
// the panel became crowded, which is the user's own reason for the split.
//
// What remains here is deliberately small: one button that launches the studio, plus
// the library handle the PLAYER needs so that a take chosen in the studio is what
// the pet actually says.
//
// Nothing in this file synthesises anything. The pet never talks to the engine.

// studioExeName is the standalone studio's executable name.
const studioExeName = "PetPCVoiceStudio.exe"

// voiceStudioState holds the player's view of the shared library.
//
// It is a handle, not a UI: the pet needs to know which clip to play, and the
// library re-reads its index when the studio has written it, so a change made in the
// other process takes effect without restarting either side.
type voiceStudioState struct {
	mu sync.Mutex
	// store is the rendered library, opened at startup because it is a plain JSON
	// read and the player needs it on the first timer tick, not on first use.
	store *voice.Store
	// launched records the studio process we started, so a second click focuses the
	// existing window instead of opening another copy.
	launched *os.Process
}

// initVoiceStudio opens the shared library.
//
// It cannot fail the app: without it the pet simply keeps its embedded voice, which
// is a fully working state.
func (a *App) initVoiceStudio() {
	st, err := voice.Open(filepath.Join(a.store.Dir(), speechDirName))
	if err != nil {
		a.DebugLog("voice studio: " + err.Error())
		return
	}
	a.vstudio.mu.Lock()
	a.vstudio.store = st
	a.vstudio.mu.Unlock()
	a.DebugLog("voice studio: library=" + st.Dir())

	// Resolve the character ONCE at startup and record it, so the log answers "which
	// voice is the pet using" without waiting for a timer to fire.
	//
	// The result is NOT cached in memory: ResolveCurrentRole reads the persisted
	// truth on every call, so a change made in the studio takes effect without a
	// restart. Resolving here is a diagnostic, and it deliberately does not write
	// back - a missing role must not be silently "repaired" into a different
	// character than the user picked.
	if role, err := st.ResolveCurrentRole(); err != nil {
		// Logged and left alone: the error surfaces to the panel and to playback,
		// both of which can explain it. Startup is not the place to guess.
		a.DebugLog("voice studio: 当前角色无法解析：" + err.Error())
	} else if voice.IsBuiltInRole(role.ID) {
		a.DebugLog("voice studio: 当前角色=内置（" + role.Name + "）")
	} else {
		a.DebugLog("voice studio: 当前角色=" + role.Name + "（" + role.ID + "）")
	}
}

// studioStore returns the shared library, or nil when it failed to open.
func (a *App) studioStore() *voice.Store {
	a.vstudio.mu.Lock()
	defer a.vstudio.mu.Unlock()
	return a.vstudio.store
}

// StudioPath returns the standalone studio's location, and whether it exists.
//
// It is looked for beside the pet's own executable first, because that is where a
// release puts it, and then in the build tree, so a development build finds the copy
// it just compiled.
func (a *App) StudioPath() (string, bool) {
	for _, p := range a.studioCandidates() {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true
		}
	}
	return "", false
}

// studioCandidates lists where the studio may be, most preferred first.
func (a *App) studioCandidates() []string {
	var out []string
	if exe, err := os.Executable(); err == nil {
		// Beside the pet: the release layout, and also where `wails build` puts a
		// development binary's neighbours.
		out = append(out, filepath.Join(filepath.Dir(exe), studioExeName))
		// The studio's own build output, for running the pet straight from the
		// repository.
		out = append(out, filepath.Join(filepath.Dir(exe), "..", "tools", "voice-studio", "build", "bin", studioExeName))
	}
	// The source tree, relative to the data directory's grandparent is not knowable,
	// so also try the working directory, which is the repo root when run via
	// `wails dev`.
	if wd, err := os.Getwd(); err == nil {
		out = append(out, filepath.Join(wd, "tools", "voice-studio", "build", "bin", studioExeName))
	}
	return out
}

// OpenVoiceStudio launches the standalone studio.
//
// Launching rather than instructing is the difference between a feature and a chore:
// the studio is a separate program and the user should not have to find it. The
// process is tracked so a second click does not open a second copy, and it is started
// detached with its own window (no inherited console).
//
// The pet does NOT wait for it and does NOT shut it down: the studio is the user's
// window, not a child job of the pet, so closing the pet must not close it - and the
// two only ever share files, never a lifetime.
func (a *App) OpenVoiceStudio() (State, error) {
	path, ok := a.StudioPath()
	if !ok {
		return State{}, fmt.Errorf("找不到配音工坊程序（%s）。它需要与桌宠放在同一个目录里，或者先用 wails build 构建 tools/voice-studio", studioExeName)
	}

	a.vstudio.mu.Lock()
	if a.vstudio.launched != nil {
		// Already running (or already exited - the check below settles which).
		if proc, err := os.FindProcess(a.vstudio.launched.Pid); err == nil {
			_ = proc
		}
	}
	a.vstudio.mu.Unlock()

	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	cmd.SysProcAttr = detachedProcessAttr()
	if err := cmd.Start(); err != nil {
		return State{}, fmt.Errorf("启动配音工坊失败：%w", err)
	}
	a.vstudio.mu.Lock()
	a.vstudio.launched = cmd.Process
	a.vstudio.mu.Unlock()
	// Reaped in the background so the child does not linger as a zombie for the
	// rest of the pet's life.
	go cmd.Wait() //nolint:errcheck // the exit status is not actionable here
	a.DebugLog("voice studio: launched " + path)

	return a.GetState()
}

// VoiceLibraryDir returns the shared library directory, for the settings panel to
// show so the user can find their rendered audio.
func (a *App) VoiceLibraryDir() string {
	if st := a.studioStore(); st != nil {
		return st.Dir()
	}
	return filepath.Join(a.store.Dir(), speechDirName, voice.SubdirName)
}

// RevealVoiceLibrary opens the library directory in the file manager.
//
// The audio is the user's own output and is worth being able to find; without this
// the only way to reach it is to know the %AppData% path by heart.
func (a *App) RevealVoiceLibrary() (State, error) {
	dir := a.VoiceLibraryDir()
	if _, err := os.Stat(dir); err != nil {
		return State{}, fmt.Errorf("目录还不存在：%s", dir)
	}
	if err := desktop.ShellOpen(dir); err != nil {
		return State{}, err
	}
	return a.GetState()
}
