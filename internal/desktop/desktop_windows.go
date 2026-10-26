//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// appWindowClass is the window class the Wails v2 Windows backend registers for
// the main application window. Wails does not expose the HWND to bound
// methods, so we locate the window by (process id, class name).
const appWindowClass = "wailsWindow"

const (
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	monitorDefaultToNearest = 0x00000002

	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	// autostartValueName is the value written into the Run key.
	autostartValueName = "PetPC"

	// startupApprovedKeyPath is where Explorer and Task Manager record whether a
	// Run entry is enabled. It is separate from the Run key itself.
	startupApprovedKeyPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	dwmapi                       = windows.NewLazySystemDLL("dwmapi.dll")
	gdi32                        = windows.NewLazySystemDLL("gdi32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procShowWindow               = user32.NewProc("ShowWindow")
	procMonitorFromWindow        = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	procDwmExtendFrame           = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
	procDwmSetWindowAttribute    = dwmapi.NewProc("DwmSetWindowAttribute")
	procGetWindowLongPtr         = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtr         = user32.NewProc("SetWindowLongPtrW")
	// SetClassLongPtrW exists only on 64-bit Windows; 32-bit builds export
	// SetClassLongW instead. Both are resolved lazily and tried in turn.
	procSetClassLongPtr  = user32.NewProc("SetClassLongPtrW")
	procSetClassLongW    = user32.NewProc("SetClassLongW")
	procGetStockObject   = gdi32.NewProc("GetStockObject")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procCreateRectRgn    = gdi32.NewProc("CreateRectRgn")
	procCreateRoundRgn   = gdi32.NewProc("CreateRoundRectRgn")
	procCreateEllipRgn   = gdi32.NewProc("CreateEllipticRgn")
	procCombineRgn       = gdi32.NewProc("CombineRgn")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procSetWindowRgn     = user32.NewProc("SetWindowRgn")
	// SetWindowCompositionAttribute is undocumented but stable, and is the only
	// way to turn OFF the blur that Wails enables for a translucent window.
	procSetWindowCompositionAttribute = user32.NewProc("SetWindowCompositionAttribute")
)

const (
	// gwlStyle is GWL_STYLE (-16), gwlExStyle is GWL_EXSTYLE (-20) and
	// gclpHbrBackground is GCLP_HBRBACKGROUND (-10): all are negative indices,
	// encoded as the 64-bit two's-complement value the Win32 call expects. The
	// callee reads the low 32 bits as a signed int.
	gwlStyle          = ^uintptr(15)
	gwlExStyle        = ^uintptr(19)
	gclpHbrBackground = ^uintptr(9)
	// ShowWindow commands used when re-evaluating the taskbar button.
	swHide    = 0
	swShowna  = 8
	nullBrush = 5
	// rgnOr is the CombineRgn mode that unions two regions.
	rgnOr = 2
	// Frame styles that make DWM draw a title bar or a resize border around the
	// glass window.
	wsCaption     = 0x00C00000
	wsThickFrame  = 0x00040000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsMaximizeBox = 0x00010000
	wsBorder      = 0x00800000
	wsDlgFrame    = 0x00400000
	// wsOverlappedWindow is the style Wails creates the window with, and
	// wsPopup is the frameless style that replaced it.
	wsOverlappedWindow = wsCaption | wsThickFrame | wsSysMenu | wsMinimizeBox | wsMaximizeBox
	wsPopup            = 0x80000000
	// Extended styles: APPWINDOW forces a taskbar button, TOOLWINDOW suppresses
	// one. Wails sets the former, so the pet shows up in the taskbar by default.
	wsExAppWindow   = 0x00040000
	wsExToolWindow  = 0x00000080
	swpFrameChanged = 0x0020
	// hwndTopmost places the window above all non-topmost windows.
	hwndTopmost = ^uintptr(0) // HWND_TOPMOST is -1
	// dwmwaColorNone tells DWM to draw no border. It is the DWMWA_COLOR_NONE
	// sentinel (0xFFFFFFFE) reinterpreted as a signed 32-bit value.
	dwmwaColorNone int32 = -2
)

// StripWindowFrame removes every window-decoration style in one call.
//
// Wails creates the window with WS_OVERLAPPEDWINDOW and only hides the frame
// later, from its WM_NCCALCSIZE handler. Between creation and the first
// WM_NCCALCSIZE the window is genuinely decorated, so the very first frames
// show a real title bar over the pet before it disappears - visible as a flash
// when the app starts.
//
// Clearing the styles as soon as the handle can be found closes that gap. The
// frame styles are also what makes DWM draw a 1px outline around the glass
// window, so this removes that too.
// StripWindowFrame makes the window structurally incapable of showing a title
// bar, by replacing its style with WS_POPUP.
//
// Wails creates the window with WS_OVERLAPPEDWINDOW and only hides the frame
// later, from its WM_NCCALCSIZE handler. Between creation and the first
// WM_NCCALCSIZE the window is genuinely decorated, which is why a decorated
// window can flash during startup.
//
// Clearing the decoration bits is enough in principle, but it leaves the window
// class as WS_OVERLAPPEDWINDOW minus some flags, and anything that later
// re-applies a standard frame style (a resize recalculation, a show/restore, the
// window being re-activated) can bring a caption back. Setting WS_POPUP instead
// removes the frame from the definition of the window: a popup has no caption,
// no border and no resize frame, so no later operation can conjure one.
//
// WS_VISIBLE is preserved so this does not hide the window.
func StripWindowFrame(w Window) error {
	style, _, _ := procGetWindowLongPtr.Call(uintptr(w), gwlStyle)
	// Remove every bit of WS_OVERLAPPEDWINDOW plus the dialog frame/border, then
	// make it a popup.
	newStyle := style&^uintptr(wsOverlappedWindow|wsDlgFrame|wsBorder) | uintptr(wsPopup)
	if newStyle == style {
		// Already a frameless popup: skip the SWP_FRAMECHANGED below, which
		// would force a full non-client recalculation and repaint on every call.
		return nil
	}
	procSetWindowLongPtr.Call(uintptr(w), gwlStyle, newStyle)
	// SWP_FRAMECHANGED makes Windows re-evaluate the non-client area, so the
	// frame disappears immediately rather than at the next resize.
	if ret, _, err := procSetWindowPos.Call(
		uintptr(w), 0, 0, 0, 0, 0,
		uintptr(swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged),
	); ret == 0 {
		return fmt.Errorf("SetWindowPos(SWP_FRAMECHANGED): %v", err)
	}
	return nil
}

// HasWindowFrame reports whether any decoration style is still present.
func HasWindowFrame(w Window) bool {
	style, _, _ := procGetWindowLongPtr.Call(uintptr(w), gwlStyle)
	return style&uintptr(wsCaption|wsThickFrame|wsSysMenu|wsMinimizeBox|wsMaximizeBox|wsBorder|wsDlgFrame) != 0
}

// HideFromTaskbar stops the window appearing as a taskbar button.
//
// Wails creates the window with WS_EX_APPWINDOW (window.go: `exStyle =
// w32.WS_EX_CONTROLPARENT | w32.WS_EX_APPWINDOW`), and that style exists
// precisely to force a taskbar button for an otherwise unowned top-level window.
//
// Clearing it is not enough on its own: a window with no owner and no
// WS_EX_TOOLWINDOW is still treated as an application window. So the exchange
// made here is APPWINDOW -> TOOLWINDOW, which is the combination Windows
// documents as "do not put this in the taskbar". The pet belongs in the
// notification area instead (see tray_windows.go).
//
// The window is NOT shown or hidden here. An earlier version re-showed it to make
// the shell re-read the styles, but this is called during startup while the window
// is deliberately still hidden (StartHidden, revealed later by App.Ready once the
// page has painted). Showing it at that point put an unpainted window on screen -
// a white rectangle the size of the pet. Changing the extended style is enough on
// its own; the taskbar picks it up without any show/hide cycle.
func HideFromTaskbar(w Window) error {
	ex, _, _ := procGetWindowLongPtr.Call(uintptr(w), gwlExStyle)
	newEx := ex&^uintptr(wsExAppWindow) | uintptr(wsExToolWindow)
	if newEx == ex {
		return nil
	}
	procSetWindowLongPtr.Call(uintptr(w), gwlExStyle, newEx)
	return nil
}

// SetWindowCompositionAttribute is the undocumented user32 entry point behind
// the ACCENT_POLICY / WINDOWCOMPOSITIONATTRIBDATA pair.
const (
	wcaAccentPolicy = 19
	// accentDisabled turns every composition effect off.
	accentDisabled = 0
	// accentEnableBlurBehind is the frosted-glass blur Wails applies.
	accentEnableBlurBehind = 3
)

// accentPolicy mirrors the ACCENT_POLICY struct.
type accentPolicy struct {
	AccentState   int32
	AccentFlags   int32
	GradientColor uint32
	AnimationID   uint32
}

// compositionAttribData mirrors WINDOWCOMPOSITIONATTRIBDATA.
type compositionAttribData struct {
	Attrib uint32
	Data   unsafe.Pointer
	SizeOf uintptr
}

// SetAccent sets the window's composition accent to the given state.
func setAccent(w Window, state int32) error {
	policy := accentPolicy{AccentState: state}
	data := compositionAttribData{
		Attrib: wcaAccentPolicy,
		Data:   unsafe.Pointer(&policy),
		SizeOf: unsafe.Sizeof(policy),
	}
	ret, _, err := procSetWindowCompositionAttribute.Call(
		uintptr(w), uintptr(unsafe.Pointer(&data)),
	)
	if ret == 0 {
		return fmt.Errorf("SetWindowCompositionAttribute: %v", err)
	}
	return nil
}

// DisableWindowBlur removes the frosted-glass blur behind the window.
//
// Setting WindowIsTranslucent is what gives the window a real per-pixel alpha
// surface (WS_EX_NOREDIRECTIONBITMAP), which is required or every transparent
// pixel of the page is composited onto an opaque white backing. But on Windows
// 10 - where Wails cannot use the Win11 backdrop APIs - that same option also
// calls SetTranslucentBackground, which applies ACCENT_ENABLE_BLURBEHIND: the
// desktop behind the pet is blurred into a soft coloured wash that follows the
// window rectangle.
//
// The blur is not wanted here at all; the pet must sit on the untouched desktop.
// Overriding the accent with ACCENT_DISABLED clears it while leaving the alpha
// surface (and therefore the transparency) intact.
func DisableWindowBlur(w Window) error {
	if err := setAccent(w, accentDisabled); err != nil {
		return err
	}
	// Force a non-client recalculation so the change takes effect immediately
	// rather than at the next resize.
	procSetWindowPos.Call(
		uintptr(w), 0, 0, 0, 0, 0,
		uintptr(swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged),
	)
	return nil
}

// DWM window attribute IDs.
const (
	dwmwaBorderColor = 34
)

// winMargins is the DWM frame-extension margin rectangle.
type winMargins struct {
	Left, Right, Top, Bottom int32
}

// MakeTransparent turns the window into glass so the desktop shows through
// wherever the page does not paint.
//
// Why this is not just WebviewIsTransparent: that option only sets the WebView2
// default background colour to fully transparent. The Win32 window itself still
// paints its background brush over the whole client area, which shows up as a
// solid black (or opaque) rectangle behind the pet. Wails does call
// DwmExtendFrameIntoClientArea, but with a 1px margin, which only buys the
// default frame styling; it does not composite the client area.
//
// Passing -1 for every margin is the documented "sheet of glass" mode: the
// entire client area becomes DWM-composited, so the window's own painting is
// discarded and only what the page draws remains.
//
// Note: once WindowIsTranslucent gives the window its own alpha surface
// (WS_EX_NOREDIRECTIONBITMAP), this extension is largely redundant - measured
// with the surface present, toggling it changed no pixel. It is kept because it
// costs nothing and still covers the case where the surface is unavailable.
func MakeTransparent(w Window) error {
	m := winMargins{Left: -1, Right: -1, Top: -1, Bottom: -1}
	if ret, _, err := procDwmExtendFrame.Call(uintptr(w), uintptr(unsafe.Pointer(&m))); ret != 0 {
		return fmt.Errorf("DwmExtendFrameIntoClientArea: %v", err)
	}
	// Drop the frame styles: they are what makes DWM draw a title bar and a 1px
	// outline around the glass window.
	if err := StripWindowFrame(w); err != nil {
		return err
	}
	// Clear the blur Wails enables for a translucent window, so the pet sits on
	// the untouched desktop instead of a frosted wash.
	if err := DisableWindowBlur(w); err != nil {
		return err
	}
	// DWMWA_COLOR_NONE only exists on Windows 11, so a failure here is expected
	// and harmless on Windows 10.
	none := dwmwaColorNone
	procDwmSetWindowAttribute.Call(
		uintptr(w),
		uintptr(dwmwaBorderColor),
		uintptr(unsafe.Pointer(&none)),
		unsafe.Sizeof(none),
	)
	return nil
}

// SetWindowRegion crops the window to a rectangle given in window-relative
// physical pixels. Passing a zero-sized rectangle restores the full window.
//
// This is what lets the window keep a constant size while only the parts we
// actually draw are visible. Resizing a window makes WebView2 recreate its
// viewport a frame or two later, and during that gap an edge-anchored element is
// laid out against the wrong viewport size - the cause of the pet visibly moving
// when the panel opened. Cropping instead of resizing avoids touching the
// viewport at all, so the page keeps exactly the geometry it already had and
// nothing can shift.
//
// Cropping also matters for what is NOT drawn. The page paints only the panel and
// the pet; the window is larger than their union, and those leftover pixels are
// transparent. With the DWM frame extended over the client area they render as a
// tinted glass strip rather than as the desktop, which is visible as a pale blue
// band. Keeping the region to the drawn rectangles removes that area entirely.
func SetWindowRegion(w Window, rects ...RegionRect) error {
	// A single zero-sized rectangle means "clear the region".
	if len(rects) == 0 || (len(rects) == 1 && (rects[0].Width <= 0 || rects[0].Height <= 0)) {
		if ret, _, err := procSetWindowRgn.Call(uintptr(w), 0, 1); ret == 0 {
			return fmt.Errorf("SetWindowRgn(clear): %v", err)
		}
		return nil
	}

	// Union the rectangles so the region covers everything that must stay visible
	// while excluding the gaps between them.
	var total uintptr
	for _, r := range rects {
		if r.Width <= 0 || r.Height <= 0 {
			continue
		}
		// CreateEllipticRgn and CreateRoundRectRgn take the same bounding
		// rectangle, so both get the same +1 on the lower-right corner and the
		// resulting outline covers the same pixels the page does. CreateRectRgn
		// is exclusive there and takes the bounds unchanged.
		var hrgn uintptr
		var err error
		switch {
		case r.Ellipse:
			hrgn, _, err = procCreateEllipRgn.Call(
				uintptr(r.X), uintptr(r.Y),
				uintptr(r.X+r.Width+1), uintptr(r.Y+r.Height+1),
			)
		case r.Radius > 0:
			d := uintptr(r.Radius * 2)
			hrgn, _, err = procCreateRoundRgn.Call(
				uintptr(r.X), uintptr(r.Y),
				uintptr(r.X+r.Width+1), uintptr(r.Y+r.Height+1),
				d, d,
			)
		default:
			hrgn, _, err = procCreateRectRgn.Call(
				uintptr(r.X), uintptr(r.Y),
				uintptr(r.X+r.Width), uintptr(r.Y+r.Height),
			)
		}
		if hrgn == 0 {
			if total != 0 {
				procDeleteObject.Call(total)
			}
			return fmt.Errorf("create region: %v", err)
		}
		if total == 0 {
			total = hrgn
			continue
		}
		// CombineRgn(total, total, hrgn, RGN_OR) folds hrgn into total.
		ret, _, err := procCombineRgn.Call(total, total, hrgn, uintptr(rgnOr))
		// The combined region is copied in, so hrgn is always released here.
		procDeleteObject.Call(hrgn)
		if ret == 0 {
			procDeleteObject.Call(total)
			return fmt.Errorf("CombineRgn: %v", err)
		}
	}
	if total == 0 {
		return nil
	}
	// SetWindowRgn takes ownership of the region on success. It must NOT be
	// deleted in that case, so only clean up when the call fails.
	ret, _, err := procSetWindowRgn.Call(uintptr(w), total, 1)
	if ret == 0 {
		procDeleteObject.Call(total)
		return fmt.Errorf("SetWindowRgn: %v", err)
	}
	return nil
}

// IsWindowVisible reports whether the window is currently visible.
func IsWindowVisible(w Window) bool {
	ret, _, _ := procIsWindowVisible.Call(uintptr(w))
	return ret != 0
}

// Show reveals the window and brings it to the foreground.
//
// Wails' runtime.WindowShow is gated on an internal hasBeenShown flag and races
// WebView2 navigation, so calling it early - before the page has finished
// loading - can silently do nothing and leave the window hidden forever.
// Calling ShowWindow directly is deterministic.
func Show(w Window) error {
	const (
		swShownormal = 1
		swShow       = 5
	)
	// ShowWindow's return value means "was previously visible", not success, so
	// the result is verified with IsWindowVisible instead.
	procShowWindow.Call(uintptr(w), uintptr(swShow))
	// SW_SHOWNORMAL also clears any minimised state carried over from a
	// previous run.
	procShowWindow.Call(uintptr(w), uintptr(swShownormal))
	// Re-assert always-on-top: showing a window can drop it below others.
	procSetWindowPos.Call(
		uintptr(w), uintptr(hwndTopmost), 0, 0, 0, 0,
		uintptr(swpNoMove|swpNoSize|swpNoActivate),
	)
	if !IsWindowVisible(w) {
		return fmt.Errorf("window is still not visible after ShowWindow")
	}
	return nil
}

// ShowNoActivate reveals the window on top of everything WITHOUT activating it.
//
// It exists for exactly one caller: a question that arrives while the user has
// hidden the pet. The pet has to be visible for the question to be answerable at
// all - the summary prompt gates the rest and the plan prompt gates the next
// focus, so a prompt drawn into a hidden window parks the timer with nothing on
// screen explaining why - but taking the keyboard there would be precisely the
// interruption the prompt is designed to avoid (docs/reminder-pomodoro.md 4.5):
// the user may be mid-sentence in another application, and an activated pet
// would swallow their next keystrokes into its own input box.
//
// SW_SHOWNOACTIVATE displays the window in its most recent size and position
// without activating it, and the topmost re-assert keeps SWP_NOACTIVATE so the
// z-order change does not activate it either. The result is verified with
// IsWindowVisible, exactly as Show verifies itself: ShowWindow's return value
// means "was previously visible", not success.
func ShowNoActivate(w Window) error {
	const swShowNoActivate = 4
	procShowWindow.Call(uintptr(w), uintptr(swShowNoActivate))
	procSetWindowPos.Call(
		uintptr(w), uintptr(hwndTopmost), 0, 0, 0, 0,
		uintptr(swpNoMove|swpNoSize|swpNoActivate),
	)
	if !IsWindowVisible(w) {
		return fmt.Errorf("window is still not visible after ShowWindow(SW_SHOWNOACTIVATE)")
	}
	return nil
}

// Hide hides the window without destroying it.
//
// SW_HIDE, rather than moving the window off-screen or making it transparent: a
// region-cropped window that is moved away still occupies its monitor, and the
// user's "hide the pet" has to mean the pixels are gone, including from Alt+Tab
// and from a screenshot of the desktop.
//
// The window is only hidden, never closed: the webview, the timers and the tray
// icon all stay alive, so showing it again is instant and loses no state. That is
// also why the hot keys can bring it back at all.
func Hide(w Window) error {
	procShowWindow.Call(uintptr(w), uintptr(swHide))
	if IsWindowVisible(w) {
		return fmt.Errorf("window is still visible after SW_HIDE")
	}
	return nil
}

// SuppressBackgroundErasure replaces the window class background brush with
// NULL_BRUSH so the window never erases itself to a solid colour.
//
// This is what stops the pet from flashing a black rectangle. Wails sets the
// class background brush from BackgroundColour, and that call passes only
// R/G/B - the alpha of 0 is ignored, so the brush is solid black. The brush is
// normally invisible because the webview covers the whole client area; but the
// window is resized on every panel toggle and drag, and Windows erases the
// newly exposed region with that brush before the webview repaints. Against the
// glass background the erase is not discarded either, so a black flash shows.
//
// With NULL_BRUSH the erase leaves whatever DWM already composited, and the
// only thing that ever becomes visible is the webview's own (transparent)
// output.
func SuppressBackgroundErasure(w Window) error {
	return SetBackgroundBrush(w, 0, 0, 0, false)
}

// SetBackgroundBrush sets the window class background brush, which is what
// Windows paints over a newly exposed region when the window grows.
//
// It takes either a solid colour or NULL_BRUSH (erase nothing). The colour form
// exists to hide the one transition NULL_BRUSH cannot: when the panel opens, the
// window grows and the extra area has not been painted by the webview yet, so
// with NULL_BRUSH the desktop shows through for a frame or two. Filling it with
// the panel's own background colour makes that transition invisible, because the
// fill and the panel are the same colour.
//
// Use NULL_BRUSH whenever the window is at pet size: there the newly exposed
// area must stay genuinely transparent, and a colour fill would show as a
// rectangle around the pet.
func SetBackgroundBrush(w Window, r, g, b uint8, solid bool) error {
	var brush uintptr
	if solid {
		// COLORREF is 0x00BBGGRR.
		col := uintptr(uint32(r) | uint32(g)<<8 | uint32(b)<<16)
		var err error
		brush, _, err = procCreateSolidBrush.Call(col)
		if brush == 0 {
			return fmt.Errorf("CreateSolidBrush: %v", err)
		}
	} else {
		var err error
		brush, _, err = procGetStockObject.Call(uintptr(nullBrush))
		if brush == 0 {
			return fmt.Errorf("GetStockObject(NULL_BRUSH): %v", err)
		}
	}

	// The brush belongs to the window class, so it outlives this call and must
	// not be deleted while the class still references it. Solid brushes are
	// created once per panel colour in practice, so the leak is bounded.
	if err := procSetClassLongPtr.Find(); err == nil {
		if ret, _, _ := procSetClassLongPtr.Call(uintptr(w), gclpHbrBackground, brush); ret != 0 {
			return nil
		}
	}
	// SetClassLongPtrW is unavailable on 32-bit Windows; SetClassLongW takes
	// the value directly.
	if ret, _, err := procSetClassLongW.Call(uintptr(w), gclpHbrBackground, brush); ret == 0 {
		return fmt.Errorf("SetClassLong(GCLP_HBRBACKGROUND): %v", err)
	}
	return nil
}

type winRect struct {
	Left, Top, Right, Bottom int32
}

// winMonitorInfo mirrors the Win32 MONITORINFO structure (40 bytes).
type winMonitorInfo struct {
	CbSize    uint32
	RcMonitor winRect
	RcWork    winRect
	DwFlags   uint32
}

// FindWindow locates the app's main Wails window.
func FindWindow() (Window, error) {
	pid := uint32(os.Getpid())
	var found Window
	cb := windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1 // keep enumerating
		}
		var buf [256]uint16
		n, _, _ := procGetClassNameW.Call(
			uintptr(hwnd),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
		)
		if n == 0 {
			return 1
		}
		if strings.EqualFold(windows.UTF16ToString(buf[:n]), appWindowClass) {
			found = Window(hwnd)
			return 0 // stop enumerating
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	if found == 0 {
		return 0, fmt.Errorf("main window %q not found for pid %d", appWindowClass, pid)
	}
	return found, nil
}

// WaitForWindow retries FindWindow until timeout. The window may not exist yet
// while the frontend is still starting up.
func WaitForWindow(timeout time.Duration) (Window, error) {
	deadline := time.Now().Add(timeout)
	for {
		hwnd, err := FindWindow()
		if err == nil {
			return hwnd, nil
		}
		if time.Now().After(deadline) {
			return 0, err
		}
		time.Sleep(40 * time.Millisecond)
	}
}

// IsWindow reports whether the handle still refers to a live window.
func IsWindow(w Window) bool {
	if w == 0 {
		return false
	}
	ret, _, _ := procIsWindow.Call(uintptr(w))
	return ret != 0
}

// WindowBounds returns the window rectangle in physical pixels.
func WindowBounds(w Window) (Bounds, error) {
	var r winRect
	ret, _, err := procGetWindowRect.Call(uintptr(w), uintptr(unsafe.Pointer(&r)))
	if ret == 0 {
		return Bounds{}, fmt.Errorf("GetWindowRect: %v", err)
	}
	return Bounds{
		X:      int(r.Left),
		Y:      int(r.Top),
		Width:  int(r.Right - r.Left),
		Height: int(r.Bottom - r.Top),
	}, nil
}

// SetBounds moves and resizes the window. All values are physical pixels.
//
// SetWindowPos is used directly rather than runtime.WindowSetPosition because
// Wails' Windows backend treats the x/y given to SetPos as relative to the
// monitor work area while its GetPosition returns absolute screen
// coordinates. Mixing the two silently misplaces the window on monitors whose
// work area does not start at (0,0).
func SetBounds(w Window, b Bounds) error {
	ret, _, err := procSetWindowPos.Call(
		uintptr(w),
		0, // no Z-order change
		uintptr(b.X),
		uintptr(b.Y),
		uintptr(b.Width),
		uintptr(b.Height),
		uintptr(swpNoZOrder|swpNoActivate),
	)
	if ret == 0 {
		return fmt.Errorf("SetWindowPos: %v", err)
	}
	return nil
}

// MoveTo moves the window without resizing it (physical pixels).
func MoveTo(w Window, x, y int) error {
	ret, _, err := procSetWindowPos.Call(
		uintptr(w), 0,
		uintptr(x), uintptr(y), 0, 0,
		uintptr(swpNoSize|swpNoZOrder|swpNoActivate),
	)
	if ret == 0 {
		return fmt.Errorf("SetWindowPos: %v", err)
	}
	return nil
}

// Scale returns the window's DPI scale factor (1.0 at 96 DPI).
func Scale(w Window) float64 {
	if err := procGetDpiForWindow.Find(); err != nil {
		return 1
	}
	dpi, _, _ := procGetDpiForWindow.Call(uintptr(w))
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96.0
}

// WorkArea returns the work area (screen minus taskbar and docked appbars) of
// the monitor the window currently sits on, in physical pixels.
func WorkArea(w Window) (Bounds, error) {
	mon, _, _ := procMonitorFromWindow.Call(uintptr(w), uintptr(monitorDefaultToNearest))
	if mon == 0 {
		return Bounds{}, errors.New("MonitorFromWindow failed")
	}
	mi := winMonitorInfo{CbSize: uint32(unsafe.Sizeof(winMonitorInfo{}))}
	ret, _, err := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	if ret == 0 {
		return Bounds{}, fmt.Errorf("GetMonitorInfo: %v", err)
	}
	return Bounds{
		X:      int(mi.RcWork.Left),
		Y:      int(mi.RcWork.Top),
		Width:  int(mi.RcWork.Right - mi.RcWork.Left),
		Height: int(mi.RcWork.Bottom - mi.RcWork.Top),
	}, nil
}

// startupApprovedDisabled reports whether Explorer's own record marks the named
// Run entry as switched off.
//
// Task Manager's "Startup" tab does not delete the Run value; it writes a
// 12-byte blob under StartupApproved\Run whose first byte is 0x03 for disabled
// and 0x02 (or 0x06) for enabled. Windows then skips the entry at logon even
// though it is still present, which looks exactly like auto-start being broken:
// the checkbox reads "on", the value exists, and nothing starts.
func startupApprovedDisabled(valueName string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedKeyPath, registry.QUERY_VALUE)
	if err != nil {
		// No key means Explorer never recorded a preference: not disabled.
		return false
	}
	defer k.Close()

	b, _, err := k.GetBinaryValue(valueName)
	if err != nil || len(b) == 0 {
		return false
	}
	// Observed values: 0x02 and 0x06 mean enabled, 0x03 means disabled
	// (cross-checked against which entries actually launched at logon).
	switch b[0] {
	case 0x03:
		return true
	default:
		return false
	}
}

// clearStartupApproved removes Explorer's enabled/disabled record for the named
// Run entry, so the next logon honours the Run key itself.
//
// This is the counterpart of writing the Run value: a user who once switched the
// pet off in Task Manager would otherwise tick the box in our panel, see it stay
// ticked, and still get no pet at logon.
func clearStartupApproved(valueName string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedKeyPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			// Nothing recorded, so nothing to clear.
			return nil
		}
		return fmt.Errorf("open StartupApproved key: %w", err)
	}
	defer k.Close()

	if err := k.DeleteValue(valueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("clear StartupApproved entry: %w", err)
	}
	return nil
}

// autostartCommand builds the command line stored in the Run key for exe.
//
// The Run value is a *Windows command line*, so it needs Windows quoting: wrap
// the path in double quotes so a directory containing spaces stays one token.
//
// It must NOT be built with fmt's %q verb. %q produces a Go *source* literal,
// which escapes every backslash, so `F:\pet_pc\build\bin\PetPC.exe` was written
// as `"F:\\pet_pc\\build\\bin\\PetPC.exe"`. Explorer hands that string to the
// shell, fails to resolve it, and skips the entry - Shell-Core logs event 9708
// with PID 0, and the app never starts at logon even though the setting is on.
func autostartCommand(exe string) (string, error) {
	if exe == "" {
		return "", errors.New("cannot determine executable path")
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	// A double quote is not legal in a Windows path, so its presence means the
	// path is already malformed. Refusing beats writing a value Explorer would
	// misparse into something else.
	if strings.ContainsRune(exe, '"') {
		return "", fmt.Errorf("executable path contains a quote: %q", exe)
	}
	return `"` + exe + `"`, nil
}

// autostartEnabled reports whether the named Run entry exists.
//
// The value name is a parameter rather than a constant so tests can point at a
// scratch value. They must never touch autostartValueName: that is the user's
// real auto-start setting, and a test that deletes it silently turns the
// feature off for whoever runs the suite.
func autostartEnabled(valueName string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(valueName)
	return err == nil
}

// AutostartEnabled reports whether auto-start is actually in effect.
//
// That is two conditions, not one: the Run value must exist *and* Explorer's
// StartupApproved record must not mark it disabled. Checking only for the value
// is what let the panel show a ticked box while nothing started at logon.
func AutostartEnabled() bool {
	return autostartEnabled(autostartValueName) && !startupApprovedDisabled(autostartValueName)
}

// repairAutostart fixes the named Run entry when it is genuinely unusable, and
// reports whether it changed anything.
//
// The only defect it repairs is the Go-escaped path an older build wrote with
// fmt.Sprintf("%q"): `"F:\\pet_pc\\...\PetPC.exe"`, which Explorer cannot
// resolve. That is why the checkbox stayed ticked while nothing started.
//
// It deliberately does NOT rewrite an entry that simply points somewhere else.
// GetState runs this on every read, so a blanket "make it match the running
// executable" would let any other copy of the binary - a test binary, a second
// install, a build in another directory - silently hijack the user's auto-start
// entry. That is exactly what happened during development: `go test ./...`
// repointed the real Run value at a temporary PetPC.test.exe.
//
// A failure is reported as "nothing repaired" rather than an error, because the
// caller is a read path (GetState) that must not fail over a cosmetic fix.
func repairAutostart(valueName string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	current, _, err := k.GetStringValue(valueName)
	if err != nil {
		// No entry (or unreadable): nothing to repair. Enabling writes it.
		return false
	}
	if !isGoEscapedPath(current) {
		// Either correct or pointing at a different install the user chose.
		// Leave it alone; only the settings toggle may repoint it.
		return false
	}

	want, err := autostartCommand(ExecutablePath())
	if err != nil {
		return false
	}
	if err := k.SetStringValue(valueName, want); err != nil {
		return false
	}
	return true
}

// isGoEscapedPath reports whether a stored Run command is the malformed output
// of Go's %q verb rather than a valid Windows path.
//
// The tell is a doubled separator: Windows accepts redundant separators in many
// APIs, but Go-style `\\` is what %q emits and what Explorer's own resolution
// rejects. A legitimate path never contains `\\`, so this is unambiguous.
func isGoEscapedPath(command string) bool {
	return strings.Contains(command, `\\`)
}

// RepairAutostart heals the real auto-start entry. See repairAutostart.
//
// It also clears a Task Manager "disabled" record, because otherwise the entry
// is present and correct yet still skipped at logon.
func RepairAutostart() bool {
	repaired := repairAutostart(autostartValueName)
	if autostartEnabled(autostartValueName) && startupApprovedDisabled(autostartValueName) {
		if err := clearStartupApproved(autostartValueName); err == nil {
			repaired = true
		}
	}
	return repaired
}

// setAutostart adds or removes the named Run entry for this executable.
func setAutostart(valueName string, enable bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Run key: %w", err)
	}
	defer k.Close()

	if !enable {
		if err := k.DeleteValue(valueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("remove Run entry: %w", err)
		}
		return nil
	}

	command, err := autostartCommand(ExecutablePath())
	if err != nil {
		return err
	}
	if err := k.SetStringValue(valueName, command); err != nil {
		return fmt.Errorf("write Run entry: %w", err)
	}
	// A stale "disabled" record in StartupApproved would keep Windows from
	// honouring the value we just wrote, so clear it. Failing here is not fatal
	// for the scratch-value path but must not be reported as success either.
	if err := clearStartupApproved(valueName); err != nil {
		return err
	}
	return nil
}

// SetAutostart adds or removes the per-user Run entry for this executable.
// Writing to HKCU needs no elevation.
func SetAutostart(enable bool) error {
	return setAutostart(autostartValueName, enable)
}
