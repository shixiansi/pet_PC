//go:build !windows

package main

import "syscall"

// detachedProcessAttr is unused off Windows; the studio is a Windows target.
func detachedProcessAttr() *syscall.SysProcAttr { return nil }
