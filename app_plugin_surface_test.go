package main

import (
	"testing"

	"PetPC/internal/desktop"
	"PetPC/internal/plugins"
)

// desktopRegion is a test-side projection of a desktop.RegionRect. Comparing the
// four numbers rather than the struct keeps a failure message readable, which
// matters for the clamp table below.
type desktopRegion struct {
	X, Y, W, H, R int
}

func regionOf(r desktop.RegionRect) desktopRegion {
	return desktopRegion{X: r.Bounds.X, Y: r.Bounds.Y, W: r.Bounds.Width, H: r.Bounds.Height, R: r.Radius}
}

// window360 is the geometry the root tests use: a 540x520 window with the pet at
// its usual place, at scale 1. It is the same shape the pixel gates measure.
const (
	testPetX, testPetY     = 0, 160
	testPetW, testPetH     = 150, 200
	testTotalW, testTotalH = 540, 520
)

// TestComputePluginSurfaceMapsAndRounds covers T53: the declared rectangle is
// mapped onto the window and its corners are rounded, and the CSS view reports
// the SAME rectangle divided back by the scale.
func TestComputePluginSurfaceMapsAndRounds(t *testing.T) {
	rects := []plugins.Rect{{DX: 160, DY: 10, W: 300, H: 220, Radius: 14}}

	regions, view := computePluginSurface(testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects)
	if len(regions) != 1 {
		t.Fatalf("want 1 region, got %d", len(regions))
	}
	want := desktopRegion{X: 160, Y: 170, W: 300, H: 220, R: 14}
	if got := regionOf(regions[0]); got != want {
		t.Fatalf("region = %+v, want %+v", got, want)
	}
	if view == nil || len(view.Rects) != 1 {
		t.Fatalf("view must carry 1 rect, got %+v", view)
	}
	if got := view.Rects[0]; got.Index != 0 || got.X != 160 || got.Y != 170 || got.W != 300 || got.H != 220 || got.R != 14 {
		t.Fatalf("view rect = %+v", got)
	}
}

// TestComputePluginSurfaceAtScaleTwo pins that the PHYSICAL region scales while
// the CSS view does not: the two describe one shape at two units, and getting
// that backwards is what would put the drawn box and the cropped region out of
// step at any scale but 100%.
//
// The window scales with the pet, which is why the inputs below are doubled too:
// at 200% the pet is 300x400 in a 1080x1040 window, and the declared CSS
// rectangle lands at the same relative place.
func TestComputePluginSurfaceAtScaleTwo(t *testing.T) {
	rects := []plugins.Rect{{DX: 160, DY: 10, W: 300, H: 220, Radius: 14}}
	regions, view := computePluginSurface(0, 320, 300, 400, 1080, 1040, 2, rects)
	if len(regions) != 1 {
		t.Fatalf("want 1 region, got %d", len(regions))
	}
	want := desktopRegion{X: 320, Y: 340, W: 600, H: 440, R: 28}
	if got := regionOf(regions[0]); got != want {
		t.Fatalf("physical region = %+v, want %+v", got, want)
	}
	// The CSS values are the un-scaled declaration: the page lays out in CSS
	// pixels, and the same rectangle has to be expressed in both units.
	got := view.Rects[0]
	if got.X != 160 || got.Y != 170 || got.W != 300 || got.H != 220 || got.R != 14 {
		t.Fatalf("css rect = %+v, want the unscaled declaration", got)
	}
}

// TestComputePluginSurfaceClampsToTheWindow covers T54. A rectangle outside the
// window cannot be cropped to: it would simply not be visible, while its share
// of the region would still be cut away from the area the page may paint.
func TestComputePluginSurfaceClampsToTheWindow(t *testing.T) {
	cases := []struct {
		name string
		rect plugins.Rect
		want desktopRegion
	}{
		{
			"pushed past the right edge",
			plugins.Rect{DX: 400, DY: 10, W: 300, H: 220},
			desktopRegion{X: 240, Y: 170, W: 300, H: 220},
		},
		{
			"pushed past the top-left corner",
			plugins.Rect{DX: -4000, DY: -4000, W: 300, H: 220},
			desktopRegion{X: 0, Y: 0, W: 300, H: 220},
		},
		{
			"larger than the window",
			plugins.Rect{DX: 0, DY: 0, W: 2000, H: 2000},
			desktopRegion{X: 0, Y: 0, W: testTotalW, H: testTotalH},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			regions, _ := computePluginSurface(testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, []plugins.Rect{tc.rect})
			if len(regions) != 1 {
				t.Fatalf("want 1 region, got %d", len(regions))
			}
			if got := regionOf(regions[0]); got != tc.want {
				t.Fatalf("region = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestComputePluginSurfaceDropsEmptyBlocks covers T55: a block that clamps away
// to nothing is dropped, and the survivors keep their DECLARATION index. The
// index is what the handshake tells each iframe, so renumbering the survivors
// would tell a plugin it owned a rectangle it did not declare.
func TestComputePluginSurfaceDropsEmptyBlocks(t *testing.T) {
	rects := []plugins.Rect{
		{DX: 0, DY: 0, W: 0, H: 0},
		{DX: 160, DY: 10, W: 300, H: 220},
	}
	regions, view := computePluginSurface(testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects)
	if len(regions) != 1 {
		t.Fatalf("want 1 surviving region, got %d", len(regions))
	}
	if len(view.Rects) != 1 || view.Rects[0].Index != 1 {
		t.Fatalf("the survivor must keep its declaration index 1, got %+v", view.Rects)
	}
}

// TestComputePluginSurfaceAllDroppedReturnsNil covers T56: nothing survives, so
// nothing is advertised and the region is not extended by anything.
func TestComputePluginSurfaceAllDroppedReturnsNil(t *testing.T) {
	regions, view := computePluginSurface(testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1,
		[]plugins.Rect{{DX: 0, DY: 0, W: 0, H: 0}})
	if regions != nil {
		t.Errorf("regions must be nil, got %+v", regions)
	}
	if view != nil {
		t.Errorf("view must be nil, got %+v", view)
	}
}

// TestComputePluginSurfaceRadiusIsCapped pins that a radius larger than half the
// shorter side is capped. Beyond that the rounded rectangle degenerates, and the
// visible area stops matching the box the page draws.
func TestComputePluginSurfaceRadiusIsCapped(t *testing.T) {
	regions, view := computePluginSurface(testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1,
		[]plugins.Rect{{DX: 10, DY: 10, W: 40, H: 100, Radius: 200}})
	if len(regions) != 1 {
		t.Fatalf("want 1 region, got %d", len(regions))
	}
	if got := regionOf(regions[0]); got.R != 20 {
		t.Fatalf("radius = %d, want half the shorter side (20)", got.R)
	}
	if view.Rects[0].R != 20 {
		t.Fatalf("css radius = %g, want 20", view.Rects[0].R)
	}
}

// TestComputePluginSurfaceEllipseFlag covers T52: the ellipse flag travels from
// the declaration to BOTH outputs, and the blocks that did not ask for it stay
// rectangular.
//
// Sorting the flag out per-output is exactly the kind of thing that works until
// a mixed declaration arrives, so the fixture declares one of each.
func TestComputePluginSurfaceEllipseFlag(t *testing.T) {
	rects := []plugins.Rect{
		{DX: 160, DY: 10, W: 300, H: 220, Radius: 14},
		{DX: 200, DY: 240, W: 34, H: 20, Radius: 10, Ellipse: true},
	}
	regions, view := computePluginSurface(testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects)
	if len(regions) != 2 || len(view.Rects) != 2 {
		t.Fatalf("want 2 of each, got %d regions and %d rects", len(regions), len(view.Rects))
	}
	if regions[0].Ellipse {
		t.Error("the first block did not declare an ellipse")
	}
	if !regions[1].Ellipse {
		t.Error("the second block declared an ellipse")
	}
	if view.Rects[0].Ellipse {
		t.Error("the CSS view of the first block must not be elliptical")
	}
	if !view.Rects[1].Ellipse {
		t.Error("the CSS view of the second block must be elliptical")
	}
}

// TestComputePluginSurfaceEllipseHasNoRadius covers T53: an elliptical block
// carries Radius 0 into the region.
//
// The number is not cosmetic. desktop's CreateEllipticRgn ignores Radius, and the
// page draws border-radius: 50% - so a non-zero radius here would describe a
// shape that neither the region nor the page produces, and the next reader would
// have no way to tell it was meaningless.
func TestComputePluginSurfaceEllipseHasNoRadius(t *testing.T) {
	// A radius that WOULD be valid for this box, so the assertion cannot pass
	// merely because the cap zeroed it.
	rects := []plugins.Rect{{DX: 200, DY: 240, W: 34, H: 20, Radius: 9, Ellipse: true}}
	regions, view := computePluginSurface(testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects)
	if len(regions) != 1 {
		t.Fatalf("want 1 region, got %d", len(regions))
	}
	if got := regions[0].Radius; got != 0 {
		t.Fatalf("an elliptical region must carry radius 0, got %d", got)
	}
	if got := view.Rects[0].R; got != 0 {
		t.Fatalf("an elliptical view rect must carry r 0, got %g", got)
	}
	// The rectangle itself is untouched: only the outline changed.
	if got := regionOf(regions[0]); got.X != 200 || got.Y != 400 || got.W != 34 || got.H != 20 {
		t.Fatalf("the ellipse bounds moved: %+v", got)
	}
}

// TestComputePluginSurfaceEllipseCoexistsWithRoundedBlocks covers the mixed case
// at scale 2: the elliptical block's region stays an ellipse with radius 0 while
// its rounded neighbour keeps its (scaled) cap, and the CSS view divides both
// back.
func TestComputePluginSurfaceEllipseCoexistsWithRoundedBlocks(t *testing.T) {
	rects := []plugins.Rect{
		{DX: 160, DY: 10, W: 40, H: 100, Radius: 200, Ellipse: true},
		{DX: 220, DY: 10, W: 40, H: 100, Radius: 200},
	}
	regions, view := computePluginSurface(0, 320, 300, 400, 1080, 1040, 2, rects)
	if len(regions) != 2 {
		t.Fatalf("want 2 regions, got %d", len(regions))
	}
	if !regions[0].Ellipse || regions[0].Radius != 0 {
		t.Errorf("the first block: ellipse=%v radius=%d, want true/0", regions[0].Ellipse, regions[0].Radius)
	}
	// The rounded neighbour is capped at half its scaled shorter side
	// (80/2 = 40), which is the same rule TestComputePluginSurfaceRadiusIsCapped
	// pins - the ellipse branch must not have changed it.
	if regions[1].Ellipse || regions[1].Radius != 40 {
		t.Errorf("the second block: ellipse=%v radius=%d, want false/40", regions[1].Ellipse, regions[1].Radius)
	}
	if view.Rects[0].R != 0 || view.Rects[1].R != 20 {
		t.Errorf("css radii = %g and %g, want 0 and 20", view.Rects[0].R, view.Rects[1].R)
	}
}
