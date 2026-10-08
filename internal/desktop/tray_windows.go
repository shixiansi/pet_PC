//go:build windows

package desktop

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Wails v2 has no system-tray support, and its OnStartup callback runs on a
// goroutine rather than the thread that pumps the Win32 message queue. A tray
// icon therefore cannot simply be attached to the app's window: the shell posts
// its callbacks to the window that registered the icon, and those messages must
// be dispatched by a loop on the same thread that created it.
//
// So this file owns a small message-only window with its own message loop, all on
// one dedicated OS thread locked for the lifetime of the process.

var (
	shell32        = windows.NewLazySystemDLL("shell32.dll")
	kernel32       = windows.NewLazySystemDLL("kernel32.dll")
	procNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
	procShellExec  = shell32.NewProc("ShellExecuteW")
	procModuleHndl = kernel32.NewProc("GetModuleHandleW")

	procLoadImageW       = user32.NewProc("LoadImageW")
	procLoadIconW        = user32.NewProc("LoadIconW")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procSetForegroundWin = user32.NewProc("SetForegroundWindow")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procUnregisterClassW = user32.NewProc("UnregisterClassW")
	procExtractIconExW   = shell32.NewProc("ExtractIconExW")
)

const (
	// Shell_NotifyIconW operations.
	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimDelete     = 0x00000002
	nimSetVersion = 0x00000004
	// notifyIconVersion4 selects the modern callback layout, where the event is
	// in the high word of lParam.
	notifyIconVersion4 = 4

	// wmTrayCallback is the message the shell posts to our window. WM_APP + 1 is
	// free because this window is ours alone.
	wmTrayCallback = 0x8000 + 1

	// Tray callback events (with NOTIFYICON_VERSION_4).
	ninSelect       = 0x0400
	ninKeySelect    = 0x0401
	ninKeyMenu      = 0x0402
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmQuit          = 0x0012

	// NOTIFYICONDATAW uFlags bits.
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	// Menu flags.
	mfString    = 0x00000000
	mfSeparator = 0x00000800

	// TrackPopupMenu flags.
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNonotify    = 0x0080

	// LoadImage flags and resource types.
	imageIcon      = 1
	lrDefaultSize  = 0x00000040
	lrShared       = 0x00008000
	idiApplication = 32512

	// Tray menu command IDs.
	trayCmdTogglePanel = 1001
	trayCmdResetPos    = 1002
	trayCmdOpenData    = 1003
	trayCmdQuit        = 1004
	trayCmdRemindNow   = 1005
	trayCmdTogglePomo  = 1006
	trayCmdShowPet     = 1007
	trayCmdHidePet     = 1008
)

// Exported command IDs so the app can map a menu choice to an action.
const (
	TrayCmdTogglePanel = trayCmdTogglePanel
	TrayCmdResetPos    = trayCmdResetPos
	TrayCmdOpenData    = trayCmdOpenData
	TrayCmdQuit        = trayCmdQuit
	// TrayCmdRemindNow asks for a reminder immediately, whatever the schedule
	// says (docs/reminder-pomodoro.md 5.2: manual wins).
	TrayCmdRemindNow = trayCmdRemindNow
	// TrayCmdTogglePomodoro switches the timer on or off.
	TrayCmdTogglePomodoro = trayCmdTogglePomo
	// TrayCmdShowPet and TrayCmdHidePet are the mouse-only equivalent of the
	// global hot keys.
	//
	// They exist because the hot keys can FAIL - another program may already own
	// the combination - and the tray is then the only way back. A "hide" that
	// could outlive every way to "show" is the one unrecoverable state this
	// feature could reach, so the tray offers both directions unconditionally.
	TrayCmdShowPet = trayCmdShowPet
	TrayCmdHidePet = trayCmdHidePet
)

// notifyIconData mirrors NOTIFYICONDATAW (the Vista+ layout). cbSize is filled in
// with the real struct size, which is what the shell validates.
type notifyIconData struct {
	CbSize           uint32
	HWnd             windows.HWND
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     windows.Handle
}

// wndClassExW mirrors WNDCLASSEXW.
type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

// point mirrors POINT.
type point struct{ X, Y int32 }

// msg mirrors MSG.
type msg struct {
	HWnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

// TrayMenuItem is one entry in the tray context menu.
type TrayMenuItem struct {
	ID        uintptr
	Label     string
	Separator bool
}

// TrayMenuAction builds a clickable menu entry.
func TrayMenuAction(id uintptr, label string) TrayMenuItem {
	return TrayMenuItem{ID: id, Label: label}
}

// TrayMenuSeparator builds a divider.
func TrayMenuSeparator() TrayMenuItem { return TrayMenuItem{Separator: true} }

// TrayOptions configures the tray icon.
type TrayOptions struct {
	Tooltip  string
	IconPath string
	Items    []TrayMenuItem
	// OnCommand runs on the tray thread when a menu entry is chosen.
	OnCommand func(id uintptr)
	// OnActivate runs on the tray thread when the icon is clicked.
	OnActivate func()
}

// trayRuntime is the per-icon state, reached by the window procedure through a
// package variable because a Win32 callback cannot carry Go context.
type trayRuntime struct {
	hwnd      windows.HWND
	options   TrayOptions
	iconAdded bool
}

var (
	trayMu      sync.Mutex
	trayCurrent *trayRuntime
)

// Tray is a live notification-area icon.
type Tray struct {
	ready chan error
	hwnd  windows.HWND
	mu    sync.Mutex
	dead  bool
}

// NewTray creates the icon on its own thread and blocks until it is registered,
// so a failure is reported instead of silently leaving no tray entry.
func NewTray(opts TrayOptions) (*Tray, error) {
	t := &Tray{ready: make(chan error, 1)}
	go t.run(opts)
	if err := <-t.ready; err != nil {
		return nil, err
	}
	return t, nil
}

// Close removes the icon and stops the tray thread. Safe to call more than once.
func (t *Tray) Close() {
	t.mu.Lock()
	if t.dead {
		t.mu.Unlock()
		return
	}
	t.dead = true
	hwnd := t.hwnd
	t.mu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(uintptr(hwnd), wmQuit, 0, 0)
	}
}

func (t *Tray) run(opts TrayOptions) {
	// The window and its message loop must stay on the same OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procModuleHndl.Call(0)
	if hInst == 0 {
		t.ready <- fmt.Errorf("GetModuleHandle failed")
		return
	}

	className := utf16Ptr("PetPCTrayWindow")
	wc := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   windows.NewCallback(trayWndProc),
		HInstance:     windows.Handle(hInst),
		LpszClassName: className,
	}
	if ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		t.ready <- fmt.Errorf("RegisterClassEx: %v", err)
		return
	}

	// HWND_MESSAGE (-3) creates a message-only window: it receives the shell's
	// callbacks and is never visible.
	const hwndMessage = ^uintptr(2)
	hwnd, _, err := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)),
		0, 0, 0, 0, 0, 0,
		hwndMessage, 0, hInst, 0,
	)
	if hwnd == 0 {
		t.ready <- fmt.Errorf("CreateWindowEx: %v", err)
		return
	}

	rt := &trayRuntime{
		hwnd:    windows.HWND(hwnd),
		options: opts,
	}
	trayMu.Lock()
	trayCurrent = rt
	trayMu.Unlock()

	t.mu.Lock()
	t.hwnd = rt.hwnd
	t.mu.Unlock()

	if err := trayAdd(rt); err != nil {
		t.ready <- err
		procDestroyWindow.Call(hwnd)
		return
	}
	t.ready <- nil

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	trayRemove(rt)
	trayMu.Lock()
	trayCurrent = nil
	trayMu.Unlock()
	procDestroyWindow.Call(hwnd)
	procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), hInst)
}

// trayWndProc receives the shell's tray callbacks.
func trayWndProc(hwnd windows.HWND, message uint32, wparam, lparam uintptr) uintptr {
	trayMu.Lock()
	rt := trayCurrent
	trayMu.Unlock()

	if rt != nil && message == wmTrayCallback {
		event := uint32(lparam & 0xFFFF)
		switch event {
		case wmLButtonUp, ninSelect, ninKeySelect:
			if rt.options.OnActivate != nil {
				rt.options.OnActivate()
			}
		case wmRButtonUp, ninKeyMenu:
			trayShowMenu(rt)
		case wmLButtonDblClk:
			if rt.options.OnActivate != nil {
				rt.options.OnActivate()
			}
		}
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wparam, lparam)
	return ret
}

// trayShowMenu builds and shows the context menu at the cursor.
func trayShowMenu(rt *trayRuntime) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	for _, it := range rt.options.Items {
		if it.Separator {
			procAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		label := utf16Ptr(it.Label)
		procAppendMenuW.Call(menu, mfString, it.ID, uintptr(unsafe.Pointer(label)))
	}

	// Required so the menu is dismissed when the user clicks elsewhere.
	procSetForegroundWin.Call(uintptr(rt.hwnd))

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	cmd, _, _ := procTrackPopupMenu.Call(
		menu, tpmRightButton|tpmReturnCmd|tpmNonotify,
		uintptr(pt.X), uintptr(pt.Y), 0, uintptr(rt.hwnd), 0,
	)
	// The shell needs a posted message to release the menu's mouse capture.
	procPostMessageW.Call(uintptr(rt.hwnd), 0, 0, 0)

	if cmd != 0 && rt.options.OnCommand != nil {
		rt.options.OnCommand(cmd)
	}
}

// trayAdd registers the icon with the shell.
func trayAdd(rt *trayRuntime) error {
	icon := trayIcon(rt.options.IconPath)
	if icon == 0 {
		return fmt.Errorf("could not load an icon for the tray")
	}
	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             rt.hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayCallback,
		HIcon:            icon,
	}
	copy(nid.SzTip[:], windows.StringToUTF16(rt.options.Tooltip))

	if ret, _, err := procNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); ret == 0 {
		return fmt.Errorf("Shell_NotifyIcon(NIM_ADD): %v", err)
	}
	// Opt into the v4 callback layout with NIM_SETVERSION. (NIM_MODIFY is for
	// changing the icon or tooltip; using it here would leave the callback layout
	// at v0.) Failure is harmless: the older layout still delivers clicks.
	nid.UVersion = notifyIconVersion4
	procNotifyIcon.Call(nimSetVersion, uintptr(unsafe.Pointer(&nid)))
	rt.iconAdded = true
	return nil
}

// trayRemove deletes the icon; safe when it was never added.
func trayRemove(rt *trayRuntime) {
	if rt == nil || !rt.iconAdded {
		return
	}
	nid := notifyIconData{
		CbSize: uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:   rt.hwnd,
		UID:    1,
	}
	procNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	rt.iconAdded = false
}

// trayIcon loads the application icon, preferring an explicit file.
//
// LR_SHARED means the icon is owned by the system and must not be destroyed,
// which keeps ownership trivial for an icon that lives as long as the process.
func trayIcon(path string) windows.Handle {
	if path != "" {
		p := utf16Ptr(path)
		if h, _, _ := procLoadImageW.Call(
			0, uintptr(unsafe.Pointer(p)), imageIcon, 0, 0, lrDefaultSize|lrShared,
		); h != 0 {
			return windows.Handle(h)
		}
	}
	// The exe's own embedded icon: build/windows/icon.ico is compiled into the
	// binary by `wails build`.
	var large, small uintptr
	exe := utf16Ptr(ExecutablePath())
	if ret, _, _ := procExtractIconExW.Call(
		uintptr(unsafe.Pointer(exe)), 0,
		uintptr(unsafe.Pointer(&large)), uintptr(unsafe.Pointer(&small)), 1,
	); ret != 0 {
		if small != 0 {
			return windows.Handle(small)
		}
		if large != 0 {
			return windows.Handle(large)
		}
	}
	// Never leave the tray entry blank.
	if h, _, _ := procLoadIconW.Call(0, uintptr(idiApplication)); h != 0 {
		return windows.Handle(h)
	}
	return 0
}

// ShellOpen opens a file, folder or URL with the shell's default handler, the
// same thing as double-clicking it in Explorer.
func ShellOpen(target string) error {
	verb := utf16Ptr("open")
	file := utf16Ptr(target)
	ret, _, err := procShellExec.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0, 0,
		1, // SW_SHOWNORMAL
	)
	// ShellExecute returns a value > 32 on success.
	if ret <= 32 {
		return fmt.Errorf("ShellExecute(%q): %v", target, err)
	}
	return nil
}

// utf16Ptr converts a Go string to a NUL-terminated UTF-16 pointer for Win32.
func utf16Ptr(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		// A string containing NUL cannot be converted; substitute an empty one so
		// the call fails cleanly instead of panicking.
		empty, _ := windows.UTF16PtrFromString("")
		return empty
	}
	return p
}
