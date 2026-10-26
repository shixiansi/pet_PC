// Package desktop isolates the small amount of native platform glue the pet
// needs: finding the app's window, moving it in physical pixels, reading the
// monitor work area for edge snapping, and toggling auto-start.
//
// The Windows implementation lives in desktop_windows.go. Other platforms get
// a stub so the module still builds and vets everywhere, even though the pet
// is a Windows-first app.
package desktop

import "os"

// Window is an opaque native window handle.
type Window uintptr

// Bounds is a rectangle in physical pixels. Physical (rather than logical)
// pixels are used throughout so the window manager and our own arithmetic
// always agree, at any DPI.
type Bounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Why the window's visible region is a union of plain RECTANGLES and nothing else: see
// SetWindowRegion in desktop_windows.go. The short version is that a GDI region is a 1-bit
// mask, so handing it a curve quantises the very edge the page had already anti-aliased.

// ExecutablePath returns the absolute path of the running executable, which is
// what an auto-start entry must point at.
func ExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}
