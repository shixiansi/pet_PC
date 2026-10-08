//go:build windows

package main

import "syscall"

// detachedProcessAttr starts the studio as an independent window.
//
// DETACHED_PROCESS keeps it from inheriting the pet's (hidden) console, and
// CREATE_NEW_PROCESS_GROUP keeps a Ctrl+C aimed at the pet from reaching it. The
// studio is the user's window, not a child job: closing the pet must not close it.
func detachedProcessAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: 0x00000008 | 0x00000200, // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	}
}
