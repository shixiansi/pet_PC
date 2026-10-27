package main

import (
	"testing"

	"PetPC/internal/plugins"
)

// The tests below pin the three rules that make `sizes` safe to expose to a plugin:
//
//  1. a block can only grow UPWARDS - the bottom edge of the region must not move, or the reply
//     block would drift away from the input pill it points at;
//  2. the manifest's `maxH` is a ceiling that cannot be exceeded, whatever the plugin asks for;
//  3. a block that declares no `maxH` cannot grow at all, so every existing plugin keeps exactly
//     the geometry it declares.
// The fixture mirrors the shapes that actually use `sizes`: a block anchored to the pet's head that
// grows upwards, and a block under the pet's feet.
//
// It used to be the 1.5.x reply card - dy -166 with the pet at 160 - whose top edge was already 6px
// OUTSIDE the window. Growing that block never worked, and the reason is pinned separately below:
// the band above the pet is the real ceiling.
func sizedFixture() []plugins.Rect {
	return []plugins.Rect{
		{DX: -75, DY: -60, W: 308, H: 60, MaxH: 156, Radius: 16},                       // the answer block
		{DX: -79, DY: 4, W: 308, H: 64, Radius: 16, Anchor: plugins.RectAnchorBottom}, // the input pill
	}
}

func TestSurfaceSizesGrowUpwardsAndKeepTheBottomEdge(t *testing.T) {
	rects := sizedFixture()
	base, _ := computePluginSurface(
		testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects, nil)
	grown, grownView := computePluginSurfaceSized(
		testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects, nil,
		map[int]int{0: 152})

	before := regionOf(base[0])
	after := regionOf(grown[0])
	if after.H == before.H {
		t.Fatalf("HEIGHT DID NOT CHANGE: %+v", after)
	}
	if after.Y+after.H != before.Y+before.H {
		t.Fatalf("bottom edge moved: before %d, after %d", before.Y+before.H, after.Y+after.H)
	}
	if after.H != 152 {
		t.Fatalf("want the requested 152, got %d", after.H)
	}
	// The view has to agree with the region, or the page draws the box where the window is not
	// cropped for it.
	if int(grownView.Rects[0].H) != 152 {
		t.Fatalf("view height = %v, want 152", grownView.Rects[0].H)
	}
	if int(grownView.Rects[0].Y) != after.Y {
		t.Fatalf("the view says y=%v, the region says %d", grownView.Rects[0].Y, after.Y)
	}
	// The sibling block is untouched.
	if regionOf(grown[1]).H != regionOf(base[1]).H {
		t.Fatalf("the pill changed height: %+v", regionOf(grown[1]))
	}
}

func TestSurfaceSizesAreClampedToTheDeclaredCeiling(t *testing.T) {
	rects := sizedFixture()
	grown, view := computePluginSurfaceSized(
		testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects, nil,
		map[int]int{0: 9999})
	if got := regionOf(grown[0]).H; got != 156 {
		t.Fatalf("want the ceiling (156), got %d", got)
	}
	if int(view.Rects[0].H) != 156 {
		t.Fatalf("view height = %v, want the ceiling 156", view.Rects[0].H)
	}
}

// The window has the last word, not the manifest. A block anchored to the pet's head grows UP into
// the band the user's 桌宠大小 leaves above the pet (160px at 1.0x, 110px at 1.5x), and a request
// taller than that band used to be honoured: the top edge went negative, the region could not
// follow it out of the window, and the page drew a bubble whose top half was cut off mid-line.
func TestSurfaceSizesAreClampedToTheBandAboveThePet(t *testing.T) {
	rects := []plugins.Rect{{DX: -75, DY: -60, W: 300, H: 60, MaxH: 400, Radius: 16}}
	regions, view := computePluginSurfaceSized(
		testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects, nil,
		map[int]int{0: 9999})

	if got := regionOf(regions[0]); got.Y != 0 || got.H != testPetY {
		t.Fatalf("want the block to fill exactly the band above the pet (y=0 h=%d), got %+v", testPetY, got)
	}
	if int(view.Rects[0].Y) != 0 || int(view.Rects[0].H) != testPetY {
		t.Fatalf("the view left the window: %+v", view.Rects[0])
	}
}

// A taller pet leaves less band, so the same request has to come out smaller - and the plugin is
// told how much band there is (`room`), because it cannot measure it from inside its own frame.
func TestSurfaceRoomShrinksWithThePetAndClampsGrowth(t *testing.T) {
	// The 1.5x pet from the settings: 225x300 logical, centred in the same 520px window.
	const petH, petW = 300, 225
	const petY = (520 - petH) / 2
	rects := []plugins.Rect{{DX: -75, DY: -60, W: 300, H: 60, MaxH: 400, Radius: 16}}

	regions, view := computePluginSurfaceSized(
		0, petY, petW, petH, 540, 520, 1, rects, nil, map[int]int{0: 9999})

	if got := view.Rects[0].Room; got != float64(petY) {
		t.Fatalf("room = %v, want %d (the band above the pet)", got, petY)
	}
	if got := regionOf(regions[0]); got.Y != 0 || got.H != petY {
		t.Fatalf("want the block clamped to the %dpx band, got %+v", petY, got)
	}
}

// `anchor: bottom` measures `dy` from the pet's FEET. Without it a block under the pet is pinned to
// the pet's top - which was right for the default 200-tall pet and put the composer on the pet's
// legs at 1.5x, where the pet is 300 tall.
func TestSurfaceAnchorBottomMeasuresFromThePetFeet(t *testing.T) {
	rect := plugins.Rect{DX: -79, DY: 4, W: 300, H: 64, Anchor: plugins.RectAnchorBottom, Radius: 16}

	for _, petH := range []int{200, 300} {
		petY := (520 - petH) / 2
		regions, _ := computePluginSurface(0, petY, 150, petH, 540, 520, 1, []plugins.Rect{rect}, nil)
		want := petY + petH + 4
		if got := regionOf(regions[0]).Y; got != want {
			t.Fatalf("pet height %d: block top = %d, want %d (4px under the feet)", petH, got, want)
		}
	}

	// The same rect anchored to the top keeps the old behaviour, so nothing that does not ask for
	// `bottom` changes meaning.
	topAnchored := rect
	topAnchored.Anchor = ""
	regions, _ := computePluginSurface(0, 160, 150, 300, 540, 520, 1, []plugins.Rect{topAnchored}, nil)
	if got := regionOf(regions[0]).Y; got != 164 {
		t.Fatalf("top-anchored block top = %d, want 160+4", got)
	}
}

func TestSurfaceSizesAreIgnoredWithoutACeiling(t *testing.T) {
	rects := sizedFixture()
	grown, _ := computePluginSurfaceSized(
		testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects, nil,
		map[int]int{1: 400}) // the pill declares no MaxH
	if got := regionOf(grown[1]).H; got != 64 {
		t.Fatalf("a block without maxH must not grow: got %d, want the declared 64", got)
	}
}

// A request that is not taller than the declaration is not a request: `sizes` is a growth
// channel, not a resize channel, or a plugin could shrink its own block to a sliver and make the
// surface unusable with no way back.
func TestSurfaceSizesCannotShrink(t *testing.T) {
	rects := sizedFixture()
	grown, _ := computePluginSurfaceSized(
		testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects, nil,
		map[int]int{0: 20})
	if got := regionOf(grown[0]).H; got != 60 {
		t.Fatalf("want the declared 60, got %d", got)
	}
}
