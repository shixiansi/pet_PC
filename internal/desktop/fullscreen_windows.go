//go:build windows

package desktop

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetWindowLongPtrW = user32.NewProc("GetWindowLongPtrW")

// Window styles used to recognise a full-screen window. A window that covers its
// whole monitor and carries none of these is something the user is watching (a
// video, a game, a presentation) rather than a normal application window.
const (
	gwlStyleW    = ^uintptr(15) // GWL_STYLE
	wsChildW     = 0x40000000
	wsPopupW     = 0x80000000
	gwlExStyleW  = ^uintptr(19) // GWL_EXSTYLE
	wsExToolWinW = 0x00000080   // WS_EX_TOOLWINDOW: overlays, not real windows
)

// IsForegroundFullscreen reports whether the foreground window covers an entire
// monitor.
//
// This is what makes the pet go quiet during a video or a game
// (docs/reminder-pomodoro.md 4.7): a bubble appearing over a full-screen film is
// worse than a missed reminder, and the timer keeps running regardless.
//
// The check compares the foreground window's rectangle with the full monitor
// rectangle - not the work area - because a full-screen window is exactly the
// case that covers the taskbar. Several false positives are excluded first:
//
//   - the desktop and the shell's own windows (the desktop is always
//     "full-screen", and so is the taskbar's owner on some configurations);
//   - anything without WS_VISIBLE;
//   - child windows and tool windows, which are overlays rather than a
//     full-screen application;
//   - our own window, which is topmost and normally small, but would otherwise
//     count while the panel is open.
func IsForegroundFullscreen(self Window) bool {
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == 0 || Window(fg) == self {
		return false
	}
	hwnd := windows.HWND(fg)

	// The desktop window and the shell's tray/worker windows are always
	// full-screen and must never suppress reminders.
	if cls := windowClass(hwnd); cls == "Progman" || cls == "WorkerW" || cls == "Shell_TrayWnd" || cls == "Shell_SecondaryTrayWnd" {
		return false
	}

	style, _, _ := procGetWindowLongPtrW.Call(fg, gwlStyleW)
	if style&(wsChildW|wsPopupW) != 0 {
		// A popup is usually a menu or a tooltip; a child window is never a
		// full-screen application.
		if style&wsChildW != 0 {
			return false
		}
	}
	ex, _, _ := procGetWindowLongPtrW.Call(fg, gwlExStyleW)
	if ex&wsExToolWinW != 0 {
		return false
	}
	if !IsWindowVisible(Window(fg)) {
		return false
	}

	var wr winRect
	if ret, _, _ := procGetWindowRect.Call(fg, uintptr(unsafe.Pointer(&wr))); ret == 0 {
		return false
	}
	mon, _, _ := procMonitorFromWindow.Call(fg, uintptr(monitorDefaultToNearest))
	if mon == 0 {
		return false
	}
	mi := winMonitorInfo{CbSize: uint32(unsafe.Sizeof(winMonitorInfo{}))}
	if ret, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ret == 0 {
		return false
	}
	// A one-pixel tolerance absorbs the borderless-window quirks some games and
	// video players have, without letting a merely maximised window (which stops
	// at the taskbar) pass.
	const tolerance = 1
	monRect := mi.RcMonitor
	return int(wr.Left) <= int(monRect.Left)+tolerance &&
		int(wr.Top) <= int(monRect.Top)+tolerance &&
		int(wr.Right) >= int(monRect.Right)-tolerance &&
		int(wr.Bottom) >= int(monRect.Bottom)-tolerance
}

// windowClass returns the class name of a window.
func windowClass(hwnd windows.HWND) string {
	var buf [256]uint16
	n, _, _ := procGetClassNameW.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if n == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
