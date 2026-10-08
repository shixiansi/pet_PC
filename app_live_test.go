package main

import (
	"testing"

	"PetPC/internal/desktop"
)

// The chat popup's geometry is the whole design: what it draws and what the
// window is cropped to must be the same rectangles, or the difference shows up as
// pixels the page never painted - which render as a tinted glass band rather than
// as the desktop (README 10(d)).
//
// The popup hangs off the PET rather than sitting in the panel's slot. The window
// is a constant 540x520 (the panel slot is 380 wide, the pet 150x200, with a 10px
// gap and the pet vertically centred at y=160).

const (
	liveWinW = 540
	liveWinH = 520
	// The pet's rectangle with the panel to its right, which is where a pet on the
	// right half of the screen ends up.
	petX = 0
	petY = 160
	petW = 150
	petH = 200
)

// liveLayout is the popup's geometry for the standard pet position at scale 1.
func liveLayout(scale float64) ([]desktop.RegionRect, *LiveView) {
	// The window SCALES WITH THE PET: at 200% it is 1080x1040 physical pixels, not
	// 540x520. Passing the logical size to a function that works in physical
	// pixels would make every rectangle look like it overflows.
	w := int(liveWinW*scale + 0.5)
	h := int(liveWinH*scale + 0.5)
	px := int(petX * scale)
	py := int(petY * scale)
	pw := int(petW * scale)
	ph := int(petH * scale)
	return computeLiveOverlay(px, py, pw, ph, w, h, scale)
}

// TestLiveOverlayHangsOffThePet is the property the user asked for: the reply card
// sits directly ABOVE the pet and the input box directly BELOW it.
//
// An earlier version put both in the panel's slot, which is the far side of the
// window from the pet - the reply card ended up ~300px away from the cat it was
// supposed to be coming out of.
func TestLiveOverlayHangsOffThePet(t *testing.T) {
	regions, view := liveLayout(1)
	if view == nil {
		t.Fatal("no view")
	}

	// The input box hangs immediately under the pet.
	barGap := view.Bar.Y - (petY + petH)
	if barGap != liveGap {
		t.Errorf("the input box is %v px below the pet, want %d", barGap, liveGap)
	}
	// The card's bottom edge is above the pet, with the trail between them.
	if view.Card.Y+view.Card.H >= petY {
		t.Errorf("the card (bottom %v) is not above the pet (top %d)",
			view.Card.Y+view.Card.H, petY)
	}
	// Both surfaces sit OVER the pet, so they read as belonging to it.
	//
	// They cannot be exactly centred on it in the real app: the window is 540 wide
	// and the pet is always against one edge (the panel takes the other side), so a
	// 280-wide card centred on a pet whose centre is 75px from that edge would
	// reach outside the window. It is therefore centred and then clamped, which
	// leaves the pet under the inner part of the card - still "the card is over the
	// pet", which is what matters, and never off the window.
	petCX := float64(petX + petW/2)
	for _, part := range []struct {
		name string
		r    LiveRect
	}{{"card", view.Card}, {"input box", view.Bar}} {
		if petCX < part.r.X || petCX > part.r.X+part.r.W {
			t.Errorf("the %s (%v..%v) does not cover the pet's centre (%v)",
				part.name, part.r.X, part.r.X+part.r.W, petCX)
		}
	}
	// The card and the bar share a column, so they line up with each other.
	if view.Card.X != view.Bar.X || view.Card.W != view.Bar.W {
		t.Errorf("the card (%v x %v) and the bar (%v x %v) are not aligned",
			view.Card.X, view.Card.W, view.Bar.X, view.Bar.W)
	}
	_ = regions
}

// TestLiveOverlayCentredOnAPetInTheMiddleOfTheWindow is the centring case without
// the edge clamp: a pet in the middle has room for the column to be genuinely
// centred on it.
//
// The clamp is what makes this a separate case. A 280-wide column centred on a pet
// that is 8px from the window edge would reach outside it, so the column is pulled
// back - and then the pet is no longer under its centre, which is exactly why the
// tail's offset is computed in Go rather than guessed from "which side".
func TestLiveOverlayCentredOnAPetInTheMiddleOfTheWindow(t *testing.T) {
	// The pet in the middle of the window: room on both sides.
	const midX = (liveWinW - petW) / 2
	_, view := computeLiveOverlay(midX, petY, petW, petH, liveWinW, liveWinH, 1)
	if view == nil {
		t.Fatal("no view")
	}
	petCX := float64(midX + petW/2)
	if got := view.Card.X + view.Card.W/2; got != petCX {
		t.Errorf("the card's centre is %v, want the pet's %v", got, petCX)
	}
	if got := view.Bar.X + view.Bar.W/2; got != petCX {
		t.Errorf("the input box's centre is %v, want the pet's %v", got, petCX)
	}
	// The tail points at the pet, so it belongs near the card's centre here.
	if want := petCX - view.Card.X - 9; view.TailX != want {
		t.Errorf("tailX = %v, want %v (the pet's centre less the tail's half-width)",
			view.TailX, want)
	}
}

// TestLiveOverlayTailStaysInsideTheCard pins the clamp on the tail: it is drawn
// inside the card's rectangle, because outside that rectangle is outside the
// window region and would be clipped.
func TestLiveOverlayTailStaysInsideTheCard(t *testing.T) {
	for _, px := range []int{0, 40, 190, liveWinW - petW} {
		_, view := computeLiveOverlay(px, petY, petW, petH, liveWinW, liveWinH, 1)
		if view.TailX < 0 || view.TailX+14 > view.Card.W {
			t.Errorf("petX %d: tailX = %v puts the tail outside the %v-wide card",
				px, view.TailX, view.Card.W)
		}
	}
}

// TestLiveOverlayStaysInsideTheWindow is the invariant the region crop depends on:
// a rectangle that reaches outside the window cannot be cropped to.
//
// The pet is tested at every position it can occupy, including hard against each
// edge, because the column is centred on the pet and then clamped - which is
// exactly where an off-by-one would push a rectangle out of the window.
func TestLiveOverlayStaysInsideTheWindow(t *testing.T) {
	scales := []float64{1, 1.25, 1.5, 2}
	// Every x the pet can sit at, including both extremes.
	xs := []int{0, 1, 60, 190, liveWinW - petW - 1, liveWinW - petW}
	for _, s := range scales {
		// The window scales with the pet, so the bounds and the pet are converted
		// to physical pixels here - the function under test works in physical
		// pixels, and comparing its output against a logical window would report
		// overflow that does not exist.
		w := int(liveWinW*s + 0.5)
		h := int(liveWinH*s + 0.5)
		py := int(petY * s)
		pw := int(petW * s)
		ph := int(petH * s)
		for _, logicalX := range xs {
			px := int(float64(logicalX) * s)
			regions, view := computeLiveOverlay(px, py, pw, ph, w, h, s)
			if view == nil {
				t.Fatalf("scale %v petX %d: no view", s, logicalX)
			}

			sawEllipse := false
			for _, r := range regions {
				if r.Width <= 0 || r.Height <= 0 {
					t.Errorf("scale %v petX %d: degenerate region %+v", s, logicalX, r)
				}
				if r.X < 0 || r.Y < 0 || r.X+r.Width > w || r.Y+r.Height > h {
					t.Errorf("scale %v petX %d: region %+v leaves the %dx%d window",
						s, logicalX, r, w, h)
				}
				if r.Ellipse {
					sawEllipse = true
				}
			}
			// The trail is decoration and gives way when the gap above the pet
			// cannot hold both it and the card, so it is only required when there
			// is room - the standard vertical position, which these cases use.
			if !sawEllipse {
				t.Errorf("scale %v petX %d: the thought puffs are not elliptical regions", s, logicalX)
			}
		}
	}
}

// TestLiveOverlayReportsCSSPixels pins the unit conversion. The region is computed
// in physical pixels because that is what SetWindowRgn takes, while the frontend
// positions elements in CSS pixels; if only one of the two were scaled, the drawn
// card and the visible area would drift apart by the DPI factor - the exact
// failure that produces a pale band beside the pet at 125% scaling.
func TestLiveOverlayReportsCSSPixels(t *testing.T) {
	// A variable rather than a constant, so the arithmetic below is done at run
	// time: as untyped constants these expressions fold into a single value with a
	// fractional part, which cannot be converted to int at all.
	s := 1.25
	regions, view := liveLayout(s)

	// The layout's copy of the card must match the region's card, converted.
	first := regions[0]
	if got, want := view.Card.X, float64(first.X)/s; got != want {
		t.Errorf("card x = %v, want %v (region %d / scale %v)", got, want, first.X, s)
	}
	// The comparison is made in PHYSICAL pixels, where the arithmetic is exact:
	// the layout is rounded to whole physical pixels (a region cannot be
	// fractional) and then divided back, so comparing the CSS values would be
	// testing float rounding rather than the conversion.
	//
	// The card's height is a CAP rather than a constant (it takes what the gap
	// above the pet allows), so at the standard pet position there is room for the
	// full height and this pins exactly that.
	if got, want := int(view.Card.H*s+0.5), int(float64(liveCardHeight)*s+0.5); got != want {
		t.Errorf("card is %d physical px tall, want %d for a %d logical px card at scale %v",
			got, want, liveCardHeight, s)
	}
	if got, want := int(view.Bar.H*s+0.5), int(float64(liveBarHeight)*s+0.5); got != want {
		t.Errorf("bar is %d physical px tall, want %d for a %d logical px bar at scale %v",
			got, want, liveBarHeight, s)
	}
}

// TestLiveOverlayCoversTheCardAndBarExactly is the property the region crop rests
// on: every rectangle the page paints must be inside a region, and the two must be
// the same shape. A card drawn where no region reaches is a card the user cannot
// see.
func TestLiveOverlayCoversTheCardAndBarExactly(t *testing.T) {
	for _, s := range []float64{1, 1.25, 1.5, 2} {
		regions, view := liveLayout(s)

		// Region 0 is the card and region 1 the bar, by construction.
		for i, want := range []LiveRect{view.Card, view.Bar} {
			r := regions[i]
			if float64(r.X) != want.X*s || float64(r.Y) != want.Y*s ||
				float64(r.Width) != want.W*s || float64(r.Height) != want.H*s {
				t.Errorf("scale %v: region %d %+v does not match the reported %+v at scale %v",
					s, i, r, want, s)
			}
		}
		// Every puff must be an elliptical region of exactly its own rectangle: the
		// puffs are drawn with border-radius: 50%, and a rounded rectangle is not
		// close enough - the two outlines would differ along every edge.
		for i, p := range view.Puffs {
			r := regions[2+i]
			if !r.Ellipse {
				t.Errorf("scale %v: puff %d is not an elliptical region", s, i)
			}
			if float64(r.Width) != p.W*s || float64(r.Height) != p.H*s {
				t.Errorf("scale %v: puff %d region %+v does not match %+v", s, i, r, p)
			}
		}
	}
}

// TestLiveOverlayPuffsConnectCardToPet checks the thought trail: it must start at
// the card's bottom edge and walk toward the pet.
func TestLiveOverlayPuffsConnectCardToPet(t *testing.T) {
	_, view := liveLayout(1)
	if len(view.Puffs) != len(livePuffs) {
		t.Fatalf("got %d puffs, want %d", len(view.Puffs), len(livePuffs))
	}

	// The first puff starts at the card's bottom edge (plus the trail gap).
	first := view.Puffs[0]
	if want := view.Card.Y + view.Card.H + liveTrailGap; first.Y != want {
		t.Errorf("the first puff's top is %v, want %v (the card's bottom + gap)", first.Y, want)
	}
	// Puffs shrink and move DOWN as they approach the pet, so the trail reads as
	// receding toward it rather than as three dots in a column.
	for i := 1; i < len(view.Puffs); i++ {
		prev, cur := view.Puffs[i-1], view.Puffs[i]
		if cur.W >= prev.W {
			t.Errorf("puff %d w = %v is not smaller than puff %d w = %v", i, cur.W, i-1, prev.W)
		}
		if cur.Y <= prev.Y {
			t.Errorf("puff %d y = %v does not sit below puff %d y = %v", i, cur.Y, i-1, prev.Y)
		}
	}
	// The last puff must REACH the pet: the trail is allowed to dip into the pet's
	// box (its top is transparent), and stopping short would leave the bubbles
	// floating detached above the cat's head.
	last := view.Puffs[len(view.Puffs)-1]
	if bottom := last.Y + last.H; bottom < petY {
		t.Errorf("the trail ends at y=%v, short of the pet's top edge (%d)", bottom, petY)
	}
	if last.Y+last.H > petY+petH/2 {
		t.Errorf("the trail reaches y=%v, past the middle of the pet (y=%d)",
			last.Y+last.H, petY+petH/2)
	}
}

// TestLiveOverlayDropsTheTrailWhenThereIsNoRoom pins the priority: the card is
// content and the trail is decoration, so when the gap above the pet cannot hold
// both, the trail is what gives way.
//
// This matters because the card is DRAWN - a trail kept at the expense of the
// card would leave a card too short to hold a sentence.
func TestLiveOverlayDropsTheTrailWhenThereIsNoRoom(t *testing.T) {
	// A pet pushed to the very top of the window leaves almost no room above it.
	const tightY = 60
	for _, s := range []float64{1, 1.25, 1.5, 2} {
		regions, view := computeLiveOverlay(petX, tightY, petW, petH, liveWinW, liveWinH, s)
		if len(view.Puffs) == 0 {
			// The trail was dropped, which is the point - the card must still be
			// usable and inside the window.
			minH := int(float64(liveCardMinHeight)*s + 0.5)
			if int(view.Card.H*s+0.5) < minH {
				t.Errorf("scale %v: the trail was dropped but the card is only %v tall",
					s, view.Card.H)
			}
		}
		for _, r := range regions {
			if r.Y < 0 {
				t.Errorf("scale %v: region %+v has a negative y", s, r)
			}
		}
	}
}

// TestLiveOverlayIsTheThirdSlot pins the mutual exclusion: the panel, a bubble and
// the popup all occupy the same area beside the pet, and the window region
// describes exactly one of them.
func TestLiveOverlayIsTheThirdSlot(t *testing.T) {
	a := newTestApp(t)

	// There is no window in this harness, so the geometry call fails - which is
	// fine and deliberate: the slot invariant is maintained by the locked helpers
	// and must hold whether or not the window can be resolved (see newTestApp).
	if _, err := a.OpenLive(); err != nil {
		t.Logf("OpenLive (no window): %v", err)
	}
	if !a.liveIsOpen() {
		t.Fatal("the popup did not open")
	}

	// Opening the panel folds the popup away.
	if _, err := a.SetPanelOpen(true); err != nil {
		t.Logf("SetPanelOpen (no window): %v", err)
	}
	if a.liveIsOpen() {
		t.Error("the popup survived the panel opening")
	}
	if a.layout.Live != nil {
		t.Error("the layout still advertises the popup")
	}
	a.mu.Lock()
	open := a.panelOpen
	a.mu.Unlock()
	if !open {
		t.Error("SetPanelOpen(true) did not open the panel")
	}

	// And the reverse: opening the popup folds the panel away.
	if _, err := a.OpenLive(); err != nil {
		t.Logf("OpenLive (no window): %v", err)
	}
	a.mu.Lock()
	open = a.panelOpen
	a.mu.Unlock()
	if open {
		t.Error("the panel stayed open under the popup")
	}
	if _, err := a.CloseLive(); err != nil {
		t.Logf("CloseLive (no window): %v", err)
	}
	if a.liveIsOpen() {
		t.Error("the popup is still open after CloseLive")
	}
	if a.layout.Live != nil {
		t.Error("the layout still advertises a closed popup")
	}
}

// TestCloseLiveIsIdempotent keeps a repeated close (a click and an Escape arriving
// together) from doing the work twice.
//
// A close with nothing to do must not even touch the native window - there is no
// re-crop to make and no event to emit, and resolving the handle for a no-op is
// how a harmless double close turns into a reported error.
func TestCloseLiveIsIdempotent(t *testing.T) {
	a := newTestApp(t)
	// This harness has no window at all, so this call succeeding proves the
	// nothing-to-do path never tried to resolve one.
	if _, err := a.CloseLive(); err != nil {
		t.Errorf("closing an already-closed popup did work: %v", err)
	}
	if _, err := a.OpenLive(); err != nil {
		t.Logf("OpenLive (no window): %v", err)
	}
	if !a.liveIsOpen() {
		t.Fatal("the popup did not open")
	}

	// The first close does re-crop, so it reports the missing window here.
	a.CloseLive()
	if a.liveIsOpen() {
		t.Error("the popup stayed open after the first close")
	}
	if _, err := a.CloseLive(); err != nil {
		t.Errorf("the second close still did work: %v", err)
	}
	if a.liveIsOpen() {
		t.Error("the popup reopened itself")
	}
}
