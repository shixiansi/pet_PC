//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachedProcessAttr returns the SysProcAttr that starts a sibling program as an
// INDEPENDENT window.
//
// The pet launches the plugin market and nothing else, and it deliberately does not
// wait for it, does not own it and does not close it: the market is the user's own
// window, so it must outlive the pet if the user closes the pet - and it must not die
// with a console the pet never had.
//
// DETACHED_PROCESS (0x8) removes the console association; CREATE_NEW_PROCESS_GROUP
// (0x200) keeps a Ctrl+C aimed at the pet from reaching the market.
//
// HideWindow is DELIBERATELY ABSENT, and that is worth stating because it was added
// here once and broke the feature outright. HideWindow sets SW_HIDE in the child's
// STARTUPINFO, and a Wails window honours the STARTUPINFO show state: the market
// process started, stayed alive and responded, with NO WINDOW AT ALL - so clicking
// 「打开插件市场」 looked like nothing happened. It is an easy thing to add while
// "tidying" the flags, because hiding a console sounds harmless; there is no console
// here to hide, and the flag hides the application instead.
//
// This is the same attribute the retired voice studio used, moved rather than
// rewritten: the studio's launcher was deleted with the studio, and the market needs
// exactly these two flags.
func detachedProcessAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: 0x00000008 | 0x00000200, // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	}
}

// Keep the exec import meaningful on this platform: the attribute exists to be
// assigned to a *exec.Cmd, and a build-tag pair is easier to audit when the type it
// serves is named in it.
var _ = func(c *exec.Cmd) { c.SysProcAttr = detachedProcessAttr() }

// pluginLaunchAttr returns the SysProcAttr for a program a PLUGIN starts
// (the `process.launch` capability).
//
// HideWindow is REQUIRED here and its absence would be visible: the programs a
// plugin starts are console applications - a chat-protocol side such as Yogurt
// prints its log to a console window - so without SW_HIDE every launch paints a
// black box on the user's desktop, which reads as "the pet opened a hacker window".
//
// This is the OPPOSITE of detachedProcessAttr above, and the difference is the
// program rather than the intent: that one starts a Wails window (where HideWindow
// hides the APPLICATION, see its comment), this one starts a console program the
// user asked for by path and expects to keep running in the background.
func pluginLaunchAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

var _ = func(c *exec.Cmd) { c.SysProcAttr = pluginLaunchAttr() }
