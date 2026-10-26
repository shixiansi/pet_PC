package main

import (
	"errors"
	"fmt"
	"math"

	"PetPC/internal/desktop"
	"PetPC/internal/plugins"
	"PetPC/internal/store"
)

// PluginSurfaceRect is one block of a plugin's floating surface, in CSS pixels
// relative to the window's top-left corner.
//
// It is the CSS-side twin of the desktop.RegionRect the window is cropped to:
// both come out of ONE call to computePluginSurface, which is what makes "the
// rectangle the region uses" and "the rectangle the page places the iframe at"
// the same rectangle rather than two calculations that have to be kept in
// agreement.
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
	// It travels with the geometry rather than being re-derived on the page
	// because the REGION is already elliptical for these blocks: if the page drew
	// a rounded rectangle where the region is an ellipse, the corners inside the
	// region and outside the drawn shape would show the window's glass tint - the
	// same sliver problem Radius exists to solve, just at the four corners
	// instead of the sides.
	Ellipse bool `json:"ellipse"`
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
func computePluginSurface(petX, petY, petW, petH, totalW, totalH int, s float64, rects []plugins.Rect) ([]desktop.RegionRect, *PluginSurfaceView) {
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

	var regions []desktop.RegionRect
	view := &PluginSurfaceView{Rects: []PluginSurfaceRect{}}

	for i, r := range rects {
		// 1. Map the declared rectangle onto the window, relative to the pet.
		x := petX + round(r.DX*s)
		y := petY + round(r.DY*s)
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

		// 3. Round the corners, but never past half the shorter side: a larger
		//    radius would make the region's rounded-rect degenerates, and the
		//    visible area would no longer match the box the page draws.
		//
		//    An elliptical block takes no corner radius at all: the region uses
		//    CreateEllipticRgn, which ignores Radius, and the page uses
		//    border-radius: 50%, which overrides it. Writing a non-zero radius
		//    here would be a number that describes neither shape, and the next
		//    person to read it would reasonably trust it.
		radius := round(r.Radius * s)
		if r.Ellipse {
			radius = 0
		} else if half := minInt(w, h) / 2; radius > half {
			radius = half
		}
		if radius < 0 {
			radius = 0
		}

		regions = append(regions, desktop.RegionRect{
			Bounds:  desktop.Bounds{X: x, Y: y, Width: w, Height: h},
			Radius:  radius,
			Ellipse: r.Ellipse,
		})
		// The view carries the SAME rectangle divided by the scale, so the page
		// and the region describe one shape.
		view.Rects = append(view.Rects, PluginSurfaceRect{
			Index:   i,
			X:       float64(x) / s,
			Y:       float64(y) / s,
			W:       float64(w) / s,
			H:       float64(h) / s,
			R:       float64(radius) / s,
			Ellipse: r.Ellipse,
		})
	}

	if len(regions) == 0 {
		// Nothing survived: do not advertise a surface and do not extend the
		// region, so the pet keeps the window to itself.
		return nil, nil
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
	// A question the user has not answered owns this slot. The surface would
	// TAKE the slot rather than share it, so opening it over the plan prompt
	// would not cover the question, it would delete it - and the timer would
	// stay parked with nothing on screen explaining why.
	if kind := a.unansweredQuestionKind(); kind != BubbleNone {
		a.DebugLog("plugin surface: refused, a " + kind + " bubble is waiting for an answer")
		return a.currentLayout(), errors.New(pluginSurfaceBlocked(kind))
	}
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
	a.panelOpen = false
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
func (a *App) pluginSurfaceRegions(pluginID string) ([]desktop.RegionRect, bool) {
	regions, view, ok := a.pluginSurfaceGeometry(pluginID)
	return regions, ok && view != nil
}

// pluginSurfaceGeometry is the shared computation behind the open path and the
// region branch.
//
// It reports whether the plugin's declaration could be read at all; whether any
// rectangle survived the clamp is expressed by the returned slices being empty.
func (a *App) pluginSurfaceGeometry(pluginID string) ([]desktop.RegionRect, *PluginSurfaceView, bool) {
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
	regions, view := computePluginSurface(petX, petY, petW, petH, totalW, totalH, s, declared)
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

// pluginSurfaceBlocked is the reason a surface is refused the slot while a
// question is on screen.
//
// The sentence names the consequence, in the style questionOwnsTheSlot set for
// the retired overlay: the plan prompt is the gate on the next focus, so a user
// who cannot see why the plugin's window will not open has no way to know the
// timer is parked behind a question.
func pluginSurfaceBlocked(kind string) string {
	switch kind {
	case BubblePick:
		return "番茄钟还在等你选一件事，先处理它再打开插件浮层面（plan）"
	case BubblePlan:
		return "番茄钟还在等你写计划，先答完再打开插件浮层面（plan）"
	default:
		return "番茄钟还在等你写总结，先答完它再打开插件浮层面（plan）"
	}
}
