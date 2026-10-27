package main

import (
	"testing"

	"PetPC/internal/plugins"
)

// TestComputePluginSurfaceHidesBlocksWithoutDeletingThem pins the contract of
// `surface.blocks`: a block the plugin hid contributes NO REGION but KEEPS its view entry
// (marked Hidden), keeping its DECLARATION index.
//
// Three halves, and each one is a bug that has already happened or nearly did:
//
//   - no region: the region decides what the window is cropped to, so a hidden block left
//     in it would still swallow clicks in an area nothing is drawn in - the whole reason the
//     capability exists.
//   - the view entry stays: the page keys its iframes by position, so a block that vanished
//     renumbered the survivors and forced every frame to be rebuilt. A rebuilt frame is a
//     destroyed document - a half-typed message and an answer still streaming in both go
//     with it, and events posted while the new document loads are lost. That is why hiding
//     a block mid-exchange was unusable before this.
//   - the index is the declaration index: the page picks each block's appearance by it, so
//     renumbering the survivors would turn one block's job into another's.
func TestComputePluginSurfaceHidesBlocksWithoutDeletingThem(t *testing.T) {
	rects := []plugins.Rect{
		{DX: -75, DY: -156, W: 300, H: 48, Radius: 14},
		{DX: -75, DY: -100, W: 300, H: 100, Radius: 16},
		{DX: -75, DY: 208, W: 300, H: 56, Radius: 16},
	}

	all, allView := computePluginSurface(
		testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects, nil)
	if len(all) != 3 || len(allView.Rects) != 3 {
		t.Fatalf("nothing hidden: want 3 regions and 3 blocks, got %d and %d",
			len(all), len(allView.Rects))
	}
	for _, r := range allView.Rects {
		if r.Hidden {
			t.Fatalf("nothing hidden, but block %d says it is: %+v", r.Index, r)
		}
	}

	only, view := computePluginSurface(
		testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects,
		map[int]bool{0: true})
	if len(only) != 2 {
		t.Fatalf("with block 0 hidden: want the other 2 regions, got %d", len(only))
	}
	if len(view.Rects) != 3 {
		t.Fatalf("with block 0 hidden: want all 3 blocks in the view, got %d", len(view.Rects))
	}
	if view.Rects[0].Index != 0 || !view.Rects[0].Hidden {
		t.Fatalf("block 0 must stay in the view, at its own index, marked hidden: %+v", view.Rects[0])
	}
	if view.Rects[1].Index != 1 || view.Rects[1].Hidden {
		t.Fatalf("block 1 must stay visible at index 1: %+v", view.Rects[1])
	}
	// The frame keeps the geometry it was cropped to when it was visible, so the page does
	// not have to re-place it when the plugin un-hides it: a box that jumps on re-show reads
	// as a different box.
	if view.Rects[0].W != float64(rects[0].W) || view.Rects[0].H != float64(rects[0].H) {
		t.Fatalf("the hidden block lost its declared size: %+v", view.Rects[0])
	}
	// ...and the hidden block's absence must not shift the survivors.
	if got, want := regionOf(only[0]), regionOf(all[1]); got != want {
		t.Fatalf("the surviving blocks moved: %+v, want %+v", got, want)
	}
}

// TestComputePluginSurfaceAllHiddenReturnsNil covers the degenerate case: hiding every block
// leaves nothing to crop to, which the caller already treats as "the surface has nothing to
// show" rather than as an error.
//
// It is deliberately NOT "the view still lists three hidden blocks": a surface whose blocks
// are all hidden has no region at all, so advertising it would hand the page a layout it
// cannot place anything in. Hiding everything is therefore the same as closing.
func TestComputePluginSurfaceAllHiddenReturnsNil(t *testing.T) {
	rects := []plugins.Rect{
		{DX: -75, DY: -156, W: 300, H: 48},
		{DX: -75, DY: -100, W: 300, H: 100},
	}
	regions, view := computePluginSurface(
		testPetX, testPetY, testPetW, testPetH, testTotalW, testTotalH, 1, rects,
		map[int]bool{0: true, 1: true})
	if regions != nil || view != nil {
		t.Fatalf("all blocks hidden: want nil/nil, got %+v / %+v", regions, view)
	}
}
