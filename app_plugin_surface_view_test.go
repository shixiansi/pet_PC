package main

import (
	"testing"

	"PetPC/internal/desktop"
	"PetPC/internal/plugins"
)

// The floating surface's plugin identity, and the mirroring that keeps its blocks
// on the free side of the pet.
//
// Both of these shipped broken, and both broke the same user-visible thing: a
// surface mounted over the panel, and the panel stopped responding. Neither was
// caught, for two different reasons - these tests exist so they cannot come back.
//
//   - The identity test drives the GEOMETRY function rather than going through
//     OpenPluginSurface. That path stamps the id at its own call site, so
//     TestOpenPluginSurfaceSucceeds passes even with the stamping removed from the
//     geometry branch; and the geometry branch only runs once a real window
//     resolves, which a windowless unit test never reaches. The frontend, however,
//     is fed by that branch.
//   - The mirroring test asserts the rectangles land on the pet's free side for
//     BOTH pet positions. Without it, "the pet is on the right" clamped every block
//     back over the pet and nothing failed, because the clamp is supposed to keep
//     rectangles inside the window - and it did.

// TestPluginSurfaceGeometryStampsThePluginID pins the identity on the view the
// geometry function produces.
//
// An unstamped view means `pluginId: ""` on the wire, which the page turns into
// `src="/plugin-asset//panel.html"` (answered "bad plugin asset path"), capability
// calls for an empty plugin id, and five frames that mount over the panel and eat
// its clicks.
func TestPluginSurfaceGeometryStampsThePluginID(t *testing.T) {
	a, id := surfaceApp(t)

	// A real pet rectangle, so the geometry function has a position to work from.
	a.mu.Lock()
	a.petRect = desktop.Bounds{X: 0, Y: 0, Width: testPetW, Height: testPetH}
	a.mu.Unlock()

	_, view, ok := a.pluginSurfaceGeometry(id)
	if !ok {
		t.Fatal("the plugin's declaration could not be read")
	}
	if view == nil {
		t.Fatal("no view was produced")
	}
	if view.PluginID != id {
		t.Fatalf("the geometry view carries plugin id %q, want %q - an empty id makes "+
			"every asset URL and capability call the surface makes unusable",
			view.PluginID, id)
	}
	if len(view.Rects) == 0 {
		t.Fatal("the view has no rectangles")
	}
}

// TestPluginSurfaceMirrorsWhenThePetIsOnTheRight pins where the blocks land for
// BOTH pet positions.
//
// A manifest declares its rectangles relative to the pet's top-left corner and can
// describe only ONE side. When the pet is on the right the declaration has to be
// mirrored: otherwise `dx: 160` lands past the window's right edge and the clamp
// drags it back to x = totalW - w, putting a 300-wide card from 240 to 540 - on top
// of the pet, and covering the panel the user is trying to click.
func TestPluginSurfaceMirrorsWhenThePetIsOnTheRight(t *testing.T) {
	// testPetW/testPetH/testTotalW/testTotalH are the shared surface-test constants
	// (150x200 pet in a 540x520 window); the panel column is the rest.
	const gap = 10
	panelW := testTotalW - testPetW - gap

	rects := []plugins.Rect{{DX: 160, DY: 10, W: 300, H: 112, Radius: 14}}

	// Pet on the LEFT: the declaration is used verbatim, so the card sits in the
	// column to the pet's right.
	_, left := computePluginSurface(testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects)
	if len(left.Rects) != 1 {
		t.Fatalf("pet on the left produced %d rects", len(left.Rects))
	}
	if left.Rects[0].X != 160 {
		t.Fatalf("pet on the left: card x = %v, want 160 - the declaration is written for this side",
			left.Rects[0].X)
	}

	// Pet on the RIGHT: mirrored, so the card sits in the column to the pet's left
	// rather than being clamped back over the pet.
	petX := panelW + gap
	mirrored := mirrorDeclaredRects(rects, testPetW)
	_, right := computePluginSurface(petX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, mirrored)
	if len(right.Rects) != 1 {
		t.Fatalf("pet on the right produced %d rects", len(right.Rects))
	}
	got := right.Rects[0]
	if got.X+got.W > float64(petX) {
		t.Fatalf("pet on the right: the card spans %v..%v but the pet starts at %d, so it "+
			"covers the pet - and these frames carry pointer-events:auto, so they would "+
			"swallow the clicks meant for whatever is underneath", got.X, got.X+got.W, petX)
	}
	// The mirror of dx=160 for a 300-wide card about a 150-wide pet: the card's right
	// edge mirrors to the pet's left edge minus the same 10px stand-off the
	// declaration expressed (150 - 160 = -10, i.e. 10px to the left of the pet).
	if want := float64(petX) - 10 - 300; got.X != want {
		t.Fatalf("pet on the right: card x = %v, want %v (the mirror of the declaration)", got.X, want)
	}
}
