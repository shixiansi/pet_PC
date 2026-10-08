//go:build !windows

package tts

import "syscall"

// hiddenProcessAttr is a no-op off Windows, where the engine is normally started
// by the user in a terminal they already have open.
func hiddenProcessAttr() *syscall.SysProcAttr { return nil }
