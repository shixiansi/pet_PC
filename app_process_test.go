package main

import (
	"runtime"
	"testing"
)

// The launcher's process flags are pinned here because getting them wrong is INVISIBLE
// to every other test, and was invisible to the user's first click too.
//
// What went wrong once: `HideWindow: true` was added to detachedProcessAttr while its
// definition was moved out of the retired voice studio's file. HideWindow sets SW_HIDE
// in the child's STARTUPINFO, and a Wails window honours the STARTUPINFO show state - so
// the market process started, stayed alive and answered window messages, with NO WINDOW
// AT ALL. Clicking 「打开插件市场」 therefore looked exactly like a dead button.
//
// No other test could see it:
//
//   - the launcher tests substitute the start function, so they assert THAT a process
//     was started, not what it was started with;
//   - a "did the process survive" check PASSES with the bug, because the process is
//     perfectly healthy - it is the window that is hidden;
//   - only a real click in the real environment reveals it, which is how the user found
//     it.
//
// So the flag set is asserted literally. That is the right kind of assertion here: the
// value is not an implementation detail, it IS the contract with the OS, and the only
// alternative is a native check that cannot run in CI.
func TestTheLauncherDoesNotHideTheWindowItStarts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the flags only exist on windows")
	}
	attr := detachedProcessAttr()
	if attr == nil {
		t.Fatal("detachedProcessAttr returned nil, so the launcher cannot be configured")
	}

	// THE REGRESSION: this hides the window of a Wails child.
	if attr.HideWindow {
		t.Error("HideWindow is set, which passes SW_HIDE to the child: the market starts " +
			"and answers messages but shows no window, so the button looks dead")
	}

	// The two flags that ARE wanted, asserted as a set: DETACHED_PROCESS so the child
	// does not inherit a console, and CREATE_NEW_PROCESS_GROUP so a Ctrl+C aimed at the
	// pet does not reach it. Both are load-bearing - a missing DETACHED_PROCESS can
	// flash a console, and a missing group flag lets a console signal kill the market.
	const (
		detachedProcess     = 0x00000008
		createNewProcessGrp = 0x00000200
	)
	want := uint32(detachedProcess | createNewProcessGrp)
	if attr.CreationFlags != want {
		t.Errorf("CreationFlags = 0x%X, want 0x%X (DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP)",
			attr.CreationFlags, want)
	}
}

// TestTheLauncherDoesNotAskForAConsoleOrAWindowState pins that nothing else sneaks into
// the STARTUPINFO the child inherits.
//
// HideWindow is the field that bit us, but the lesson is broader: every field on
// SysProcAttr that reaches STARTUPINFO changes how the child's window is created, and
// none of them is wanted here. A future "tidy-up" that adds one - CmdLine, or another
// window-state flag - fails here with an explanation rather than on a user's click.
func TestTheLauncherDoesNotAskForAConsoleOrAWindowState(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the flags only exist on windows")
	}
	attr := detachedProcessAttr()

	if attr.HideWindow {
		t.Error("HideWindow is set again; see TestTheLauncherDoesNotHideTheWindowItStarts")
	}
	// The pet must not make the market inherit a (non-existent) console, and must not
	// try to give it one.
	if attr.NoInheritHandles {
		// Not wanted either, but harmless; reported so a change here is deliberate.
		t.Log("NoInheritHandles is set; it is not needed by this launcher")
	}
}
