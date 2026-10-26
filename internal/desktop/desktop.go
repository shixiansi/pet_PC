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

// RegionRect is a rectangle the window's visible region should cover, with
// rounded corners or an elliptical outline.
//
// The radius matters because the region is what decides which pixels of the
// window are shown at all: anywhere inside the region that the page does not
// paint still shows the window's own background (a translucent glass tint), so a
// square region around a rounded panel leaves a tinted sliver in each corner.
// Matching the page's own border-radius keeps the two identical.
type RegionRect struct {
	Bounds
	// Radius is the corner radius in physical pixels, matching the CSS
	// border-radius of what the page draws there.
	Radius int
	// Ellipse crops to the ellipse inscribed in Bounds instead of a rectangle.
	//
	// It exists for the thought-trail puffs beside the live overlay, which the
	// page draws with border-radius: 50%. A rounded rectangle is not a close
	// enough approximation there: a 30px-high puff gets a 15px corner radius, so
	// the two shapes differ by tens of pixels along every edge, and each pixel
	// inside the region that the page leaves transparent shows the composited
	// glass tint instead of the desktop. CreateEllipticRgn is the exact
	// counterpart of border-radius: 50%, so the two outlines coincide by
	// construction rather than by a matching calculation.
	Ellipse bool
}

// ExecutablePath returns the absolute path of the running executable, which is
// what an auto-start entry must point at.
func ExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}
