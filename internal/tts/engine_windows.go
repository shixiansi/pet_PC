//go:build windows

package tts

import "syscall"

// hiddenProcessAttr keeps the engine's console window from appearing.
//
// Without it, starting the server flashes a black terminal on screen - for a
// desktop pet whose whole point is to not intrude, that is a visible defect even
// though the server itself is working.
func hiddenProcessAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
