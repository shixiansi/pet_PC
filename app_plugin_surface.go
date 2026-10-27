package main

import (
	"fmt"
	"math"

	"PetPC/internal/desktop"
	"PetPC/internal/plugins"
	"PetPC/internal/store"
)

// PluginSurfaceRect is one block of a plugin's floating surface, in CSS pixels
// relative to the window's top-left corner.
//
// It is the CSS-side twin of the desktop.Bounds the window is cropped to: both come
// out of ONE call to computePluginSurface, which is what makes "the rectangle the
// region covers" and "the rectangle the page places the iframe at" the same rectangle
// rather than two calculations that have to be kept in agreement.
//
// The SHAPE lives here and not in the region: R and Ellipse tell the page how to draw,
// and the region only says which pixels of the window exist. A region cannot carry a
// curve - it is a 1-bit mask - so drawing one there would quantise an edge the renderer
// had already anti-aliased.
type PluginSurfaceRect struct {
	// Index is the block's position in the manifest's declaration order. It is
	// the declaration index, NOT a renumbering of the surviving blocks: the
	// frontend builds the i-th surface iframe from the i-th manifest rectangle,
	// and the handshake tells that iframe which rectangle it owns.
	Index int     `json:"index"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
	R     float64 `json:"r"`
	// Ellipse reports that the page must draw this block as a true ellipse
	// (border-radius: 50%) rather than a rounded rectangle.
	//
	// It travels with the geometry rather than being re-derived on the page because
	// the plugin declared it: the shape a block should take is part of what the plugin
	// asked for, not something the host may guess from the numbers. The page draws
	// border-radius: 50% for these and a rounded rectangle for the rest.
	Ellipse bool `json:"ellipse"`
	// TailX is where the PET's centre falls inside this rectangle, in CSS px from the
	// rectangle's left edge.
	//
	// A plugin that draws a tail pointing at the pet must use this instead of 50%: the host
	// clamps every rectangle into the window, and the pet sits flush against the window's
	// right edge, so a block wider than the pet is shifted left of the pet's centre.
	TailX float64 `json:"tailX"`
	// Hidden reports that the plugin asked the host to stop SHOWING this block
	// (`surface.blocks`), without the block leaving the declaration.
	//
	// The entry stays in the view on purpose. It used to be dropped, which made "hide this
	// block" indistinguishable from "this block does not exist" - and since the page keys
	// its iframes by position in `rects`, a block that vanished renumbered every survivor
	// and forced the page to tear down and rebuild ALL of them. A rebuilt frame is a
	// destroyed document: whatever the plugin had rendered (a half-typed message, an answer
	// still streaming in) is gone, and events posted while the new document is loading are
	// lost.
	//
	// So the split is: the REGION drops a hidden block (nothing is painted there, so nothing
	// may be clickable there - AGENTS.md §3.2), while the VIEW keeps it with this flag set,
	// so the page can keep the iframe alive at its declared rectangle and merely stop
	// showing it.
	Hidden bool `json:"hidden"`
	// Room is the free space ABOVE the pet inside the window, in CSS pixels.
	//
	// It travels with every rectangle because a plugin cannot derive it and cannot do without
	// it: the band the floating blocks share is whatever the user's 桌宠大小 leaves (the pet is
	// centred, so a 1.5x pet leaves 110px where the default leaves 160). A block that grows
	// upwards - the answer box - has to know how tall it may become, and the question strip has
	// to know whether it still fits above it.
	Room float64 `json:"room"`
}

// PluginSurfaceView is the plugin's floating surface as the frontend sees it.
//
// Nil means "no surface is showing", which is the same convention Bubble and
// Live use: absence is expressed by the pointer, not by an empty value, so
// there is one way to say "nothing is up".
type PluginSurfaceView struct {
	PluginID string              `json:"pluginId"`
	Rects    []PluginSurfaceRect `json:"rects"`
}

// mirrorDeclaredRects flips a declaration horizontally about the pet.
//
// It exists because a manifest has ONE declaration but the pet has TWO possible
// sides, and the window is always petW + gap + panelW wide:
//
//   - pet on the LEFT (panelOnRight): the pet occupies 0..150 and the free column
//     is 160..540. The declaration is written for this side - `dx: 160` puts a
//     300-wide card at 160..460, immediately beside the pet with the usual gap.
//   - pet on the RIGHT: the pet occupies 390..540 and the free column is on its
//     LEFT. The same `dx: 160` would land at 550 - outside a 540-wide window - and
//     the clamp would drag it back to 240..540, squarely ON TOP of the pet and over
//     the panel. That is not cosmetic: those frames carry `pointer-events: auto`,
//     so they swallow the clicks meant for the panel underneath and the panel stops
//     responding entirely.
//
// Measuring `dx` from the pet's RIGHT edge instead keeps the same stand-off and
// lands the block in the free column: dx' = petW - dx - w puts the card at
// 390 + (150 - 160 - 300) = 80..380 against a pet at 390..540 - the mirror image of
// the pet-on-left case, which is what a user expects when the pet is on the other
// edge of the screen. The vertical coordinate is untouched: which side the pet is
// on is a horizontal property.
//
// `petWLogical` is in the same CSS pixels as the rectangles' own dx/w, NOT the
// physical pixels the region uses.
func mirrorDeclaredRects(rects []plugins.Rect, petWLogical float64) []plugins.Rect {
	if len(rects) == 0 {
		return rects
	}
	out := make([]plugins.Rect, len(rects))
	for i, r := range rects {
		r.DX = petWLogical - r.DX - r.W
		out[i] = r
	}
	return out
}

// computePluginSurface turns a plugin's declared rectangles into the window
// region and the frontend view.
//
// The pet's rectangle and the window size are PHYSICAL pixels (that is what the
// region needs); the declared rectangles are CSS pixels relative to the pet's
// top-left corner, and the returned view is converted back to CSS pixels. Both
// outputs come from this one call, so they cannot drift - the property the
// whole design rests on ("几何只算一次").
//
// The rectangles are expected to already be expressed for THIS pet position;
// mirrorDeclaredRects is what adapts a manifest's one-sided declaration, and it is
// applied by the callers so this function stays a pure computation over the
// geometry it is handed.
//
// Everything is clamped into the window. A rectangle outside the window cannot
// be cropped to: it would simply not be visible, while its share of the region
// would still be cut away from the area the page is allowed to paint, leaving
// the pet's neighbours unpainted. Clamping is therefore not a nicety; it is
// what keeps the region equal to what is actually drawn.
//
// A block whose clamped width or height is zero or negative is dropped. If
// every block is dropped the result is (nil, nil): no region extension, no
// layout field, and the caller reports that the surface fell outside the
// window.
func computePluginSurface(petX, petY, petW, petH, totalW, totalH int, s float64, rects []plugins.Rect, hidden map[int]bool) ([]desktop.Bounds, *PluginSurfaceView) {
	return computePluginSurfaceSized(petX, petY, petW, petH, totalW, totalH, s, rects, hidden, nil)
}

// computePluginSurfaceSized is computePluginSurface plus the heights a plugin asked for.
//
// `sizes` maps a DECLARATION index to the height the plugin wants (see `surface.blocks`). A
// request is honoured only when the declaration gave the block a ceiling (`maxH`) and the value
// is clamped into [h, maxH]; the block grows UPWARDS, keeping its bottom edge where it was, so a
// reply that gets longer never moves the input pill it points at.
//
// The wrapper above exists so the dozen existing call sites - tests included - keep the old
// signature: a pure geometry function that grew a second optional input should not force every
// caller to say `nil`.
func computePluginSurfaceSized(petX, petY, petW, petH, totalW, totalH int, s float64, rects []plugins.Rect, hidden map[int]bool, sizes map[int]int) ([]desktop.Bounds, *PluginSurfaceView) {
	if s <= 0 {
		s = 1
	}
	// math.Round, not `int(v + 0.5)`: the latter TRUNCATES toward zero once the
	// value is negative, so -309.5 became -309 and a mirrored block landed a pixel
	// to the right of where the arithmetic says. Offsets are negative now that a
	// declaration written for one side is mirrored onto the other (see
	// mirrorDeclaredRects), and this function is the only place that converts them
	// to integers - so the rounding has to be symmetric about zero.
	round := func(v float64) int { return int(math.Round(v)) }

	var regions []desktop.Bounds
	view := &PluginSurfaceView{Rects: []PluginSurfaceRect{}}
	// regionOf[k] is the index in `regions` of the block described by view.Rects[k], or -1
	// for a HIDDEN block (which is described but not cropped in). The two slices were
	// appended in lockstep until hiding stopped removing blocks; the indirection is what
	// keeps the growth pass below aimed at the right rectangle.
	regionOf := make([]int, 0, len(rects))

	for i, r := range rects {
		// A block the plugin asked the host to hide contributes NO REGION - that is the whole
		// point of `surface.blocks`: an area nothing is drawn in must not keep swallowing
		// clicks, because `pointer-events` lives on the parent document's frame element and a
		// plugin cannot touch that.
		//
		// It still gets a VIEW entry, marked Hidden (see the field's comment). Dropping it
		// instead would renumber the survivors - `i` is the DECLARATION index and travels on
		// in PluginSurfaceRect.Index - and the page would rebuild every frame.
		hideThis := hidden[i]

		// 1. Map the declared rectangle onto the window, relative to the pet. `anchor` picks the
		//    pet's edge `dy` is measured from: a block under the pet must be anchored to its
		//    FEET, or a bigger 桌宠大小 swallows it (it used to sit on the pet's legs).
		y0 := petY
		if r.Anchor == plugins.RectAnchorBottom {
			y0 = petY + petH
		}
		x := petX + round(r.DX*s)
		y := y0 + round(r.DY*s)
		w := round(r.W * s)
		h := round(r.H * s)

		// 2. Pin the size first, then clamp the origin. Clamping the origin
		//    against an un-pinned size would let a rectangle wider than the
		//    window settle at a negative origin and then be pinned, which puts
		//    its right edge outside the window.
		if w > totalW {
			w = totalW
		}
		if h > totalH {
			h = totalH
		}
		if w <= 0 || h <= 0 {
			continue
		}
		if x > totalW-w {
			x = totalW - w
		}
		if x < 0 {
			x = 0
		}
		if y > totalH-h {
			y = totalH - h
		}
		if y < 0 {
			y = 0
		}

		// 3. Round the corners of what the PAGE draws, but never past half the
		//    shorter side: a larger CSS radius degenerates, and the drawn shape
		//    would stop matching the box the plugin declared.
		//
		//    This radius goes into the VIEW only. The window region is the plain
		//    rectangle: a GDI region is a 1-bit mask, so giving it a radius would
		//    re-quantise the curve the renderer had already anti-aliased (see
		//    SetWindowRegion in internal/desktop).
		//
		//    An elliptical block takes no corner radius at all: the page draws
		//    border-radius: 50%, so a non-zero Radius here would be a number that
		//    describes neither shape, and the next person to read it would
		//    reasonably trust it.
		radius := round(r.Radius * s)
		if r.Ellipse {
			radius = 0
		} else if half := minInt(w, h) / 2; radius > half {
			radius = half
		}
		if radius < 0 {
			radius = 0
		}

		// Boxes, not shapes: the region's job is which pixels of the WINDOW exist,
		// and the shape of each block is the page's business.
		//
		// A HIDDEN block stops here: no pixels of the window belong to it, so it is not
		// cropped in - but it is still described below, so the page keeps its frame.
		regionOf = append(regionOf, -1)
		if !hideThis {
			regions = append(regions, desktop.Bounds{X: x, Y: y, Width: w, Height: h})
			regionOf[len(regionOf)-1] = len(regions) - 1
		}
		// The view carries the SAME rectangle divided by the scale, so the page
		// places its iframes on exactly the pixels the region covers.
		// The pet's centre as a distance from THIS rectangle's left edge, in CSS px.
		//
		// The plugin needs it, and cannot derive it: the host CLAMPS rectangles into the
		// window, and the pet sits flush against the window's right edge, so a block wider
		// than the pet ends up shifted left of the pet's centre. A tail drawn at 50% of such
		// a block points ~75px away from the pet's head - which is what it did when this
		// field did not exist.
		tailX := (float64(petX) + float64(petW)/2 - float64(x)) / s

		view.Rects = append(view.Rects, PluginSurfaceRect{
			Index:   i,
			X:       float64(x) / s,
			Y:       float64(y) / s,
			W:       float64(w) / s,
			H:       float64(h) / s,
			R:       float64(radius) / s,
			Ellipse: r.Ellipse,
			TailX:   tailX,
			Hidden:  hideThis,
			Room:    float64(petY) / s,
		})
	}

	if len(regions) == 0 {
		// Nothing survived: do not advertise a surface and do not extend the
		// region, so the pet keeps the window to itself.
		return nil, nil
	}
	// Heights the plugin asked for. Applied AFTER the declared geometry is settled, because a
	// request may only change the height of a block that already exists: it cannot move one,
	// cannot widen one, and cannot introduce one.
	//
	// `regionOf[k]` answers "which rectangle does view.Rects[k] own" (or -1 for a hidden
	// block). The guard on `declared.H` is what makes the growth opt-in: a block without
	// `maxH` in its manifest ignores every request, which is every plugin but the chat one
	// today.
	for k := range view.Rects {
		block := &view.Rects[k]
		ri := regionOf[k]
		if block.Hidden || ri < 0 {
			// A hidden block has no region to grow into: the request describes a box
			// nothing is painted in, and honouring it would move the page's frame away
			// from the pixels the window was cropped to.
			continue
		}
		want, asked := sizes[block.Index]
		if !asked || want <= 0 || block.Index < 0 || block.Index >= len(rects) {
			continue
		}
		declared := rects[block.Index]
		ceiling := int(math.Round(declared.MaxH * s))
		floor := int(math.Round(declared.H * s))
		if ceiling <= floor {
			continue // no ceiling declared: this block cannot grow
		}
		max := int(math.Round(float64(want) * s))
		if max > ceiling {
			max = ceiling
		}
		// ...and the window has the last word. A block anchored to the pet's head grows UP into
		// the band the user's 桌宠大小 leaves above it, and a request taller than that band used to
		// be honoured anyway: the top edge went to a negative y, the region lost it (the region
		// cannot leave the window) while the page still drew the block there - which is exactly the
		// answer that was cut off at the top on a 1.5x pet. Clamping the grown height to what is
		// available turns "as tall as it asked" into "as tall as it fits".
		if room := regions[ri].Y + regions[ri].Height; max > room {
			max = room
		}
		if max <= floor {
			continue // a request that is not taller than the declaration is not a request
		}
		grew := max - floor
		// Upwards: the bottom edge stays put, the top edge rises. Both the region and the view
		// rect have to move together or the page would draw the box somewhere the window is not
		// cropped for.
		regions[ri].Y -= grew
		regions[ri].Height = max
		block.Y -= float64(grew) / s
		block.H = float64(max) / s
	}

	return regions, view
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- state -----------------------------------------------------------------

// setPluginSurfaceLocked is the ONLY place a.pluginSurface and
// a.layout.PluginSurface change.
//
// The two are maintained together for the same reason setBubbleLocked does it:
// the frontend renders the surface FROM the layout. If the state said "showing"
// and the layout said "nothing", the next GetLayout would hand the page a
// surface it is not cropped for; if the state said "nothing" and the layout kept
// the rectangles, the window would stay cropped around an interface nobody
// draws, and the lost area would show as the glass band this whole design
// exists to avoid (app_bubble_test.go pins the same invariant for the bubble).
//
// Callers must hold a.mu.
func (a *App) setPluginSurfaceLocked(view *PluginSurfaceView) {
	a.pluginSurface = view
	a.layout.PluginSurface = view
}

// pluginSurfaceRects reads a plugin's declared rectangles from its manifest, and
// reports whether the plugin is currently ALLOWED to occupy the region at all.
//
// The availability test is deliberately the full one - installed, enabled,
// holding pet.surface, and with a readable manifest - rather than merely "the
// manifest parses". It has to be, because the guard moved here: the two bindings
// that used to close a surface when its plugin stopped qualifying
// (SetPluginEnabled(false) and UninstallPlugin) are gone with the rest of the
// host's management surface, and the plugin market is a SEPARATE PROCESS that can
// disable or uninstall at any moment without telling the pet.
//
// So the question "may this plugin paint?" is answered fresh on every geometry
// pass, from the on-disk state. A disabled or uninstalled plugin's rectangles
// simply stop being available, the geometry branch falls through to the
// pet-only region, and the surface state and layout are cleared together - which
// is strictly stronger than the old implementation, since that one only noticed
// when the pet itself performed the mutation.
func (a *App) pluginSurfaceRects(pluginID string) ([]plugins.Rect, bool) {
	row, ok := a.pluginManager().Get(pluginID)
	if !ok || !row.Enabled || row.Manifest == nil {
		return nil, false
	}
	if !hasPermission(row.Granted, plugins.PermPetSurface) {
		return nil, false
	}
	return row.Manifest.Surface, true
}

// A per-plugin "close this one's surface" helper used to live here: it closed a
// plugin's surface when the pet itself disabled or uninstalled it. Both of those
// bindings are gone (the market program owns them now), and a guard that only
// fires on a mutation the pet performs cannot see a mutation made by another
// process - so the guard moved into syncPluginSurface and pluginSurfaceRects,
// where it is evaluated on every geometry pass and therefore holds across
// processes too.

// OpenPluginSurface shows a plugin's floating surface.
//
// It refuses in three cases, each with its own sentence, and in every one of
// them NOTHING changes: the surface stays as it was, the panel keeps its state
// and the layout is untouched. A refusal that left the layout half-updated would
// be worse than not refusing at all.
func (a *App) OpenPluginSurface(pluginID string) (Layout, error) {
	// A FRESH surface shows everything the manifest declared. Without this, a plugin that hid a
	// block last time would come back with it missing, and "nothing hidden" is the default that
	// every plugin which never calls `surface.blocks` has to keep.
	//
	// NOT when this plugin's surface is ALREADY up, though. The page reuses the frames in that
	// case (same plugin, same version, same blocks), so the plugin gets no second `init` and
	// nothing would ask for its hidden blocks and heights again - the reset would simply strand
	// the surface on the manifest's defaults. That is not theoretical: clicking the conversation
	// button over the pet while the surface is open goes through here, and it left an EMPTY
	// question strip on screen and shrank the answer box back to its declared height, losing the
	// answer that was being read. Pressing a button should not destroy the state the plugin owns.
	if !a.surfaceShowingFor(pluginID) {
		a.clearHiddenSurfaceBlocks()
	}

	row, ok := a.pluginManager().Get(pluginID)
	if !ok || !row.Enabled || row.Manifest == nil {
		return a.currentLayout(), fmt.Errorf("插件 %s 未启用", pluginID)
	}
	if !hasPermission(row.Granted, plugins.PermPetSurface) {
		return a.currentLayout(), fmt.Errorf("插件 %s 未获权限 %s", pluginID, plugins.PermPetSurface)
	}
	if len(row.Manifest.Surface) == 0 {
		return a.currentLayout(), fmt.Errorf("插件 %s 未声明 surface", pluginID)
	}
	// Nothing refuses the slot for being a question any more. The only question the
	// host asked was the pomodoro picker, and it went with the host's timer: both
	// pomodoro questions belong to the pomodoro plugin, which opens its own surface
	// (openspec/changes/todo-pomodoro-as-plugins, design.md §4.1). A reminder bubble
	// is folded away below instead - it is a notification, and the surface is the
	// round the user is in the middle of.
	// Prove the rectangles can actually be cropped to before changing any
	// state: a surface entirely outside the window has nothing to draw in, and
	// admitting it would leave the layout advertising a surface with no region.
	regions, view, ok := a.pluginSurfaceGeometry(pluginID)
	if !ok || view == nil || len(regions) == 0 {
		return a.currentLayout(), fmt.Errorf("插件 %s 的浮层面完全在窗口之外", pluginID)
	}
	// The geometry function works from the declared rectangles alone, so the
	// plugin's identity is stamped on here: the frontend routes the iframe's
	// asset URL and its events by this field, and an empty one would render a
	// surface belonging to nobody.
	view.PluginID = pluginID

	a.mu.Lock()
	wasBubble := a.bubble != nil
	a.setBubbleLocked(nil)
	// The view computed above is recorded as-is, so the state is complete even
	// when the crop below cannot run: without this, a windowless caller would
	// hold a surface with no rectangles, which is a state the frontend cannot
	// render and the region cannot be derived from.
	a.setPluginSurfaceLocked(view)
	a.mu.Unlock()
	if wasBubble {
		a.emitBubble(BubbleNone, false)
	}
	// The STATE is already set, so a failure here is a failure to crop the
	// window - not a failure to open the surface. Reporting it as the latter
	// would tell the plugin the opposite of what happened and make it try again;
	// the layout it gets back still advertises the surface, because the next
	// geometry pass will crop to it.
	layout, err := a.applyWindowGeometry()
	if err != nil {
		a.DebugLog("plugin surface: re-crop failed: " + err.Error())
		layout = a.currentLayout()
	}
	return layout, nil
}

// surfaceShowingFor reports whether pluginID's surface is the one currently on screen.
//
// It exists so a RE-open of the same surface can be told apart from a fresh one: the two must
// not behave the same way, because only a fresh one has frames that will be created (and thus
// told everything) again. See OpenPluginSurface.
func (a *App) surfaceShowingFor(pluginID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pluginSurface != nil && a.pluginSurface.PluginID == pluginID
}

// ClosePluginSurface hides the floating surface and restores the region to the
// pet alone.
func (a *App) ClosePluginSurface() (Layout, error) {
	a.mu.Lock()
	was := a.pluginSurface
	if was == nil {
		layout := a.layout
		a.mu.Unlock()
		return layout, nil
	}
	a.setPluginSurfaceLocked(nil)
	a.mu.Unlock()
	layout, err := a.applyWindowGeometry()
	if err != nil {
		// The surface is gone from the state either way; only the crop failed.
		a.DebugLog("plugin surface: re-crop after close failed: " + err.Error())
		return a.currentLayout(), nil
	}
	return layout, nil
}

// currentLayout returns the last layout without touching the window, for a
// refusal path that must change nothing at all (resolving the native handle
// would re-crop the window, which is itself a change).
func (a *App) currentLayout() Layout {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.layout
}

// syncPluginSurface clears the showing surface when its plugin has stopped
// qualifying to occupy the region, and reports whether it did.
//
// This is the cross-process guard. The pet used to close a surface itself, in the
// two bindings that disabled or uninstalled a plugin - but those bindings are
// gone, and the plugin market is a SEPARATE PROCESS that can disable, uninstall
// or revoke pet.surface at any moment without telling the pet. A guard that only
// fired on a mutation the pet performed cannot see any of that.
//
// So the question is asked on the geometry path instead, from the on-disk state,
// every time the window is laid out: if the plugin no longer qualifies, the
// surface goes away and the layout stops advertising it, which re-crops the
// window to the pet alone. That is strictly stronger than the old
// implementation - it holds for mutations made by any process, and it holds for
// one that happened while the pet was not looking.
//
// It is deliberately safe to call with nothing showing, and it is called
// unconditionally from applyWindowGeometry.
func (a *App) syncPluginSurface() bool {
	a.mu.Lock()
	cur := a.pluginSurface
	a.mu.Unlock()
	if cur == nil {
		return false
	}
	// The availability test is the same one the region branch uses, so "may this
	// plugin paint?" has exactly one answer in this file.
	if _, ok := a.pluginSurfaceRects(cur.PluginID); ok {
		return false
	}
	a.mu.Lock()
	// Re-check under the lock: another caller may have replaced or cleared the
	// surface between the read above and here, and clearing a DIFFERENT plugin's
	// surface would be a bug the caller never asked for.
	if a.pluginSurface != nil && a.pluginSurface.PluginID == cur.PluginID {
		a.setPluginSurfaceLocked(nil)
	}
	a.mu.Unlock()
	a.DebugLog("plugin surface: dropped, " + cur.PluginID + " no longer qualifies")
	return true
}

// pluginSurfaceRegions computes the surface's region rectangles for a plugin at
// the CURRENT pet geometry, and reports whether any survived the clamp.
//
// It is the same computation applyWindowGeometry performs, exposed so the open
// path can decide before it changes state.
func (a *App) pluginSurfaceRegions(pluginID string) ([]desktop.Bounds, bool) {
	regions, view, ok := a.pluginSurfaceGeometry(pluginID)
	return regions, ok && view != nil
}

// pluginSurfaceGeometry is the shared computation behind the open path and the
// region branch.
//
// It reports whether the plugin's declaration could be read at all; whether any
// rectangle survived the clamp is expressed by the returned slices being empty.
func (a *App) pluginSurfaceGeometry(pluginID string) ([]desktop.Bounds, *PluginSurfaceView, bool) {
	declared, ok := a.pluginSurfaceRects(pluginID)
	if !ok {
		return nil, nil, false
	}
	petW, petH := a.petPixelSize()
	s := a.currentScale()
	gap := int(float64(panelGap)*s + 0.5)
	panelW := int(float64(store.PanelWidth)*s + 0.5)
	panelH := int(float64(store.PanelHeight)*s + 0.5)
	totalW := petW + gap + panelW
	totalH := panelH
	if petH > totalH {
		totalH = petH
	}
	a.mu.Lock()
	pet := a.petRect
	panelOnRight := a.layout.PanelOnRight
	a.mu.Unlock()
	petX := 0
	if !panelOnRight {
		petX = panelW + gap
	}
	petY := (totalH - petH) / 2
	if petY < 0 {
		petY = 0
	}
	if pet.Width <= 0 {
		pet = desktop.Bounds{Width: petW, Height: petH}
	}
	// The manifest declares its rectangles for the pet on the LEFT (the panel on
	// the right), which is the layout its numbers were written against. With the pet
	// on the right the same offsets would land outside the window and be clamped
	// back on top of it, so the declaration is mirrored into the free column on the
	// pet's left instead. See mirrorDeclaredRects.
	if !panelOnRight {
		declared = mirrorDeclaredRects(declared, float64(petW)/s)
	}
	regions, view := computePluginSurfaceSized(petX, petY, petW, petH, totalW, totalH, s, declared, a.hiddenSurfaceBlocks(), a.requestedSurfaceSizes())
	// The plugin's identity is stamped on HERE, and it has to be stamped on EVERY
	// view that leaves this file.
	//
	// computePluginSurface works from the declared rectangles alone - it has no
	// plugin to name - so the view it returns carries an empty PluginID, and the
	// frontend builds every iframe's asset URL and routes every request by that
	// field. An empty one produced `src="/plugin-asset//panel.html"` (answered "bad
	// plugin asset path"), capability calls for an empty plugin, and - the part that
	// is not cosmetic - frames that still mounted with pointer-events:auto and
	// swallowed the clicks meant for the panel underneath.
	if view != nil {
		view.PluginID = pluginID
	}
	return regions, view, true
}

// pluginSurfaceBlocked used to hold the three sentences a refused surface could say:
// "a summary is waiting", "a plan is waiting", "a todo is waiting". None of them can
// happen now - both pomodoro questions belong to the pomodoro plugin and the picker
// went with the host's timer - so the surface is never refused for a question and the
// sentences are gone rather than left as text nothing can produce
// (openspec/changes/todo-pomodoro-as-plugins, design.md §4.1).
