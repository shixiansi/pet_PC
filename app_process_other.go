//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachedProcessAttr returns the SysProcAttr for a non-Windows build.
//
// The platforms differ in what can be expressed rather than in intent: there is no
// DETACHED_PROCESS flag to set, so the child simply inherits nothing special. The
// pet's supported target is Windows; this exists so the package still builds for the
// other platforms the toolchain is asked about, which is how the cross-platform
// compile check stays meaningful.
func detachedProcessAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

var _ = func(c *exec.Cmd) { c.SysProcAttr = detachedProcessAttr() }

// pluginLaunchAttr returns the SysProcAttr for a plugin-started program on a
// non-Windows build.
//
// There is no console window to hide here, so the only thing this must do is exist:
// `process.launch` itself is platform-neutral (os/exec), and the `.exe` extension it
// insists on simply makes it a Windows feature in practice.
func pluginLaunchAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

var _ = func(c *exec.Cmd) { c.SysProcAttr = pluginLaunchAttr() }
