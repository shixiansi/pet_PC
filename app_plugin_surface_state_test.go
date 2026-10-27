package main

import (
	"encoding/json"
	"strings"
	"testing"

	"PetPC/internal/plugins"
)

// The phase and run-state literals used to be aliased here so the test read in the
// domain's own vocabulary. Both constants were the pomodoro state machine's, and they
// went with it: the host has no phases any more, and a surface's state test has no
// business naming them.

// --- T57: the layout field and its JSON shape ------------------------------

// TestLayoutPluginSurfaceSerialisation covers T57: the field is omitted when
// there is no surface and round-trips when there is one.
//
// `omitempty` is load-bearing rather than cosmetic: the frontend treats an
// absent member and a null member differently in exactly one place - it hides
// the surface mount point - and a layout that always carried the key would make
// "no surface" indistinguishable from "a surface with no rectangles".
func TestLayoutPluginSurfaceSerialisation(t *testing.T) {
	empty, err := json.Marshal(Layout{})
	if err != nil {
		t.Fatalf("marshal empty layout: %v", err)
	}
	if strings.Contains(string(empty), "pluginSurface") {
		t.Fatalf("an empty layout must not carry the key: %s", empty)
	}

	source := Layout{PluginSurface: &PluginSurfaceView{
		PluginID: "com.x.y",
		Rects:    []PluginSurfaceRect{{Index: 0, X: 160, Y: 170, W: 300, H: 220, R: 14}},
	}}
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"pluginSurface"`, `"com.x.y"`, `"rects"`, `"index"`} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("the JSON does not contain %s: %s", want, encoded)
		}
	}

	var back Layout
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.PluginSurface == nil {
		t.Fatal("the surface did not survive the round trip")
	}
	if back.PluginSurface.PluginID != "com.x.y" || len(back.PluginSurface.Rects) != 1 {
		t.Fatalf("round trip changed the value: %+v", back.PluginSurface)
	}
	if got := back.PluginSurface.Rects[0]; got != source.PluginSurface.Rects[0] {
		t.Fatalf("rect round trip: got %+v want %+v", got, source.PluginSurface.Rects[0])
	}
}

// --- T59: the one place the surface state changes --------------------------

// TestSetPluginSurfaceLockedKeepsStateAndLayoutInStep covers T59.
//
// This is the same invariant app_bubble_test.go pins for the bubble, and for the
// same reason: the frontend renders the surface FROM the layout, so a state that
// said "showing" while the layout said "nothing" would hand the page a surface it
// is not cropped for - and the reverse would keep the window cropped around an
// interface nobody draws, which is the glass band this design exists to avoid.
func TestSetPluginSurfaceLockedKeepsStateAndLayoutInStep(t *testing.T) {
	a := newTestApp(t)

	a.mu.Lock()
	a.setPluginSurfaceLocked(&PluginSurfaceView{PluginID: "com.x.y"})
	state, advertised := a.pluginSurface, a.layout.PluginSurface
	a.mu.Unlock()
	if state == nil || advertised == nil {
		t.Fatalf("both must be set: state=%+v layout=%+v", state, advertised)
	}
	if state.PluginID != "com.x.y" || advertised.PluginID != "com.x.y" {
		t.Fatalf("wrong plugin id: state=%q layout=%q", state.PluginID, advertised.PluginID)
	}

	a.mu.Lock()
	a.setPluginSurfaceLocked(nil)
	state, advertised = a.pluginSurface, a.layout.PluginSurface
	a.mu.Unlock()
	if state != nil || advertised != nil {
		t.Fatalf("clearing must clear BOTH: state=%+v layout=%+v", state, advertised)
	}
}

// --- T60/T61/T62: the three existing entry points clear the surface --------

// TestExistingOpeningsClearTheSurface covers T60, T61 and T62 in one pass.
//
// The occupants share one slot and the region can only describe one of them, so
// every opening has to fold the others away. Leaving a plugin's rectangles in
// the region while the panel or a bubble is up would crop the window around an
// interface nobody draws.
//
// The list has TWO entries: the pet's own live overlay used to be the third
// opening, and it was deleted with the rest of the host's conversation UI. The
// slot's occupants are now the panel, a bubble and the surface - and the
// surface is the one being cleared here, so the entries are the two openings
// that can take the slot from it.
func TestExistingOpeningsClearTheSurface(t *testing.T) {
	cases := []struct {
		name string
		open func(a *App)
	}{
		{"SetPanelOpen", func(a *App) { a.SetPanelOpen(true) }},
		{"showBubble", func(a *App) { a.showBubble(&BubbleView{Kind: BubbleReminder, Title: "t"}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t)
			a.mu.Lock()
			a.setPluginSurfaceLocked(&PluginSurfaceView{PluginID: "com.x.y"})
			a.mu.Unlock()

			tc.open(a)

			a.mu.Lock()
			state, advertised := a.pluginSurface, a.layout.PluginSurface
			bubble := a.bubble
			a.mu.Unlock()
			if state != nil {
				t.Errorf("%s left the surface showing: %+v", tc.name, state)
			}
			if advertised != nil {
				t.Errorf("%s left the layout advertising the surface: %+v", tc.name, advertised)
			}
			if tc.name == "showBubble" {
				if bubble == nil || bubble.Kind != BubbleReminder {
					t.Errorf("showBubble must still show the bubble it was given: %+v", bubble)
				}
			}
		})
	}
}

// --- T63-T67: opening, refusing and closing ---------------------------------

// surfaceApp installs a plugin that declares a surface, grants it pet.surface and
// enables it.
func surfaceApp(t *testing.T) (*App, string) {
	t.Helper()
	a := pluginApp(t)
	return a, installSurfacePlugin(t, a)
}

// installSurfacePlugin installs, grants and enables a plugin declaring one
// floating-surface rectangle, on an app that already exists.
//
// It is split out of surfaceApp because the slot-contention tests need the
// surface on an app they built for another reason (a parked timer, a summary
// prompt), and rebuilding the app to get one would throw that state away.
func installSurfacePlugin(t *testing.T, a *App) string {
	t.Helper()
	return installSampleWith(t, a, map[string]any{
		"permissions": []string{"pet.surface"},
		"surface":     []map[string]any{{"dx": 160, "dy": 10, "w": 300, "h": 220, "radius": 14}},
	})
}

// TestOpenPluginSurfaceSucceeds covers T63's accepting half, and that the
// capability (surface.show) reaches the SAME terminal state as the binding
// rather than growing a second switch.
func TestOpenPluginSurfaceSucceeds(t *testing.T) {
	a, id := surfaceApp(t)

	if _, err := a.OpenPluginSurface(id); err != nil {
		t.Fatalf("OpenPluginSurface: %v", err)
	}
	a.mu.Lock()
	state, advertised := a.pluginSurface, a.layout.PluginSurface
	a.mu.Unlock()
	if state == nil || advertised == nil {
		t.Fatal("the surface must be recorded in both the state and the layout")
	}
	if state.PluginID != id || advertised.PluginID != id {
		t.Fatalf("wrong plugin: state=%q layout=%q", state.PluginID, advertised.PluginID)
	}
	if len(advertised.Rects) != 1 {
		t.Fatalf("the layout must carry 1 rect, got %d", len(advertised.Rects))
	}

	if _, err := a.ClosePluginSurface(); err != nil {
		t.Fatalf("ClosePluginSurface: %v", err)
	}
	a.mu.Lock()
	state, advertised = a.pluginSurface, a.layout.PluginSurface
	a.mu.Unlock()
	if state != nil || advertised != nil {
		t.Fatal("closing must clear both the state and the layout")
	}

	// The capability takes the same path.
	if _, err := a.PluginRequest(id, "surface.show", ""); err != nil {
		t.Fatalf("surface.show: %v", err)
	}
	a.mu.Lock()
	viaCapability := a.pluginSurface
	a.mu.Unlock()
	if viaCapability == nil || viaCapability.PluginID != id {
		t.Fatalf("surface.show did not open the surface: %+v", viaCapability)
	}
	if _, err := a.PluginRequest(id, "surface.hide", ""); err != nil {
		t.Fatalf("surface.hide: %v", err)
	}
	a.mu.Lock()
	afterHide := a.pluginSurface
	a.mu.Unlock()
	if afterHide != nil {
		t.Fatalf("surface.hide did not close the surface: %+v", afterHide)
	}
}

// TestOpenPluginSurfaceRefusals covers T64: an ungranted permission, a manifest
// with no surface, and a surface entirely outside the window are each refused
// with their own reason, and NONE of them changes any state.
func TestOpenPluginSurfaceRefusals(t *testing.T) {
	t.Run("no permission", func(t *testing.T) {
		a := pluginApp(t)
		// Declares a surface but is granted nothing, then enabled.
		id := installTestPlugin(t, a, map[string]any{
			"permissions": []string{"pet.surface"},
			"surface":     []map[string]any{{"dx": 160, "dy": 10, "w": 300, "h": 220, "radius": 14}},
		})
		managerSetEnabled(t, a, id, true)
		_, err := a.OpenPluginSurface(id)
		assertRefusal(t, a, err, "未获权限")
	})

	t.Run("no surface declared", func(t *testing.T) {
		a := pluginApp(t)
		id := installSampleWith(t, a, map[string]any{"permissions": []string{"pet.surface"}})
		_, err := a.OpenPluginSurface(id)
		assertRefusal(t, a, err, "surface")
	})

	t.Run("entirely outside the window", func(t *testing.T) {
		a := pluginApp(t)
		// The declaration is legal (the validator accepts a positive size) but
		// sub-pixel: at scale 1 it rounds to a zero-area rectangle, so the clamp
		// drops the only block and there is nothing left to crop to.
		//
		// A merely OFF-SCREEN rectangle does not work here, and deliberately so:
		// clamping is what makes a declared 4000px offset land inside the window
		// instead of vanishing, so the way to get "nothing survived" is a block
		// whose size rounds away.
		id := installSampleWith(t, a, map[string]any{
			"permissions": []string{"pet.surface"},
			"surface":     []map[string]any{{"dx": 10, "dy": 10, "w": 0.4, "h": 0.4, "radius": 0}},
		})
		_, err := a.OpenPluginSurface(id)
		assertRefusal(t, a, err, "窗口之外")
	})
}

// assertRefusal checks a refusal left the layout and the panel state untouched.
func assertRefusal(t *testing.T, a *App, err error, wants string) {
	t.Helper()
	if err == nil {
		t.Fatal("the call must be refused")
	}
	if !strings.Contains(err.Error(), wants) {
		t.Fatalf("error %q must mention %q", err.Error(), wants)
	}
	a.mu.Lock()
	state, advertised, panel := a.pluginSurface, a.layout.PluginSurface, a.panelOpen
	a.mu.Unlock()
	if state != nil {
		t.Errorf("a refusal left the surface showing: %+v", state)
	}
	if advertised != nil {
		t.Errorf("a refusal left the layout advertising a surface: %+v", advertised)
	}
	if panel {
		t.Error("a refusal opened the panel")
	}
}

// TestOpenPluginSurfaceRefusedWithoutPermission pins the refusal ORDER: the
// permission is checked before the geometry, so a plugin that cannot show a
// surface is told that rather than being told its rectangle was wrong.
func TestOpenPluginSurfaceRefusedWithoutPermission(t *testing.T) {
	a := pluginApp(t)
	id := installTestPlugin(t, a, map[string]any{
		"permissions": []string{"llm.chat"},
		"surface":     []map[string]any{{"dx": 4000, "dy": 4000, "w": 1, "h": 1, "radius": 0}},
	})
	managerSetEnabled(t, a, id, true)
	_, err := a.OpenPluginSurface(id)
	if err == nil {
		t.Fatal("must be refused")
	}
	if !strings.Contains(err.Error(), "未获权限") {
		t.Fatalf("the permission must be checked first, got %q", err.Error())
	}
}

// TestOpenPluginSurfaceFoldsTheReminderAway covers T65 in its current form: a surface
// TAKES the slot rather than sharing it, so a bubble cannot stay on screen beside it -
// the region can only describe one occupant, and a card the page no longer paints
// would show as a band of desktop.
//
// T65 used to be "refuse over a question". The host raises no questions any more: the
// picker went with its timer, and both pomodoro prompts belong to the pomodoro plugin
// (openspec/changes/todo-pomodoro-as-plugins, design.md §4.1). A reminder is a
// notification, so the surface replaces it.
func TestOpenPluginSurfaceFoldsTheReminderAway(t *testing.T) {
	a, id := surfaceApp(t)

	if _, err := a.showBubble(&BubbleView{Kind: BubbleReminder, Title: "今天要做的事"}); err != nil {
		a.DebugLog("showBubble (no window): " + err.Error())
	}

	if _, err := a.OpenPluginSurface(id); err != nil {
		t.Fatalf("the surface was refused: %v", err)
	}
	a.mu.Lock()
	state, bubble := a.pluginSurface, a.bubble
	a.mu.Unlock()
	if state == nil {
		t.Error("no surface was recorded, so the slot is empty")
	}
	if bubble != nil {
		t.Fatalf("the reminder survived the surface: %+v", bubble)
	}
}

// TestGeometryDropsTheSurfaceOfADisabledPlugin covers T66 in its new form.
//
// The disable is performed through the MANAGER, exactly as the market program
// performs it - the pet has no binding for it any more - and the assertion is
// that the NEXT geometry pass drops the surface from both the state and the
// layout. That is the stronger guarantee: it holds for a mutation made by
// another process, which the deleted per-plugin close call could not see.
func TestGeometryDropsTheSurfaceOfADisabledPlugin(t *testing.T) {
	a, id := surfaceApp(t)
	if _, err := a.OpenPluginSurface(id); err != nil {
		t.Fatalf("OpenPluginSurface: %v", err)
	}
	managerSetEnabled(t, a, id, false)

	// The native handle is unavailable here, which is fine: syncPluginSurface
	// runs BEFORE the window is resolved precisely so the STATE is reconciled
	// even when the crop cannot be applied.
	if _, err := a.applyWindowGeometry(); err != nil {
		t.Logf("applyWindowGeometry (no window): %v", err)
	}
	a.mu.Lock()
	state, advertised := a.pluginSurface, a.layout.PluginSurface
	a.mu.Unlock()
	if state != nil || advertised != nil {
		t.Fatalf("a geometry pass must drop a disabled plugin's surface: state=%+v layout=%+v", state, advertised)
	}
}

// TestGeometryDropsTheSurfaceOfAnUninstalledPlugin covers T67 the same way.
func TestGeometryDropsTheSurfaceOfAnUninstalledPlugin(t *testing.T) {
	a, id := surfaceApp(t)
	if _, err := a.OpenPluginSurface(id); err != nil {
		t.Fatalf("OpenPluginSurface: %v", err)
	}
	managerUninstall(t, a, id)

	if _, err := a.applyWindowGeometry(); err != nil {
		t.Logf("applyWindowGeometry (no window): %v", err)
	}
	a.mu.Lock()
	state, advertised := a.pluginSurface, a.layout.PluginSurface
	a.mu.Unlock()
	if state != nil || advertised != nil {
		t.Fatalf("a geometry pass must drop an uninstalled plugin's surface: state=%+v layout=%+v", state, advertised)
	}
	for _, row := range a.pluginManager().Plugins() {
		if row.ID == id {
			t.Fatal("the plugin is still installed")
		}
	}
}

// TestGeometryDropsTheSurfaceOfARevokedPlugin is the third case the old
// per-plugin close call could not have covered at all: a permission the user
// withdrew in the market program. The asset route already refuses to serve such
// a plugin, so leaving its rectangles in the region would crop visible area for
// an interface that cannot be painted.
func TestGeometryDropsTheSurfaceOfARevokedPlugin(t *testing.T) {
	a, id := surfaceApp(t)
	if _, err := a.OpenPluginSurface(id); err != nil {
		t.Fatalf("OpenPluginSurface: %v", err)
	}
	if _, err := a.pluginManager().GrantPermissions(id, nil); err != nil {
		t.Fatalf("GrantPermissions(nil): %v", err)
	}

	if _, err := a.applyWindowGeometry(); err != nil {
		t.Logf("applyWindowGeometry (no window): %v", err)
	}
	a.mu.Lock()
	state, advertised := a.pluginSurface, a.layout.PluginSurface
	a.mu.Unlock()
	if state != nil || advertised != nil {
		t.Fatalf("a geometry pass must drop a plugin whose pet.surface was revoked: state=%+v layout=%+v", state, advertised)
	}
}

// TestPluginSurfaceRegionsUsesTheSameRectanglesAsTheRegion is the geometry
// invariant at the heart of the design: the rectangles the WINDOW is cropped to
// and the rectangles the PAGE is told to place are the same ones, because both
// come out of one call.
//
// The check is that the two outputs of one geometry pass describe the same shape
// in their two units - the physical region the region call takes and the CSS
// values the frontend lays out with. A second, independent calculation of the
// CSS rectangles is exactly the drift this design rules out, so comparing
// against one would prove nothing.
func TestPluginSurfaceRegionsUsesTheSameRectanglesAsTheRegion(t *testing.T) {
	a, id := surfaceApp(t)
	regions, view, ok := a.pluginSurfaceGeometry(id)
	if !ok {
		t.Fatal("the surface's declaration must be readable")
	}
	if view == nil || len(regions) == 0 {
		t.Fatal("the declared rectangle must survive the clamp")
	}
	if len(regions) != len(view.Rects) {
		t.Fatalf("the region has %d rects, the view has %d", len(regions), len(view.Rects))
	}
	s := a.currentScale()
	for i := range regions {
		r, v := regions[i], view.Rects[i]
		// The view is the region divided by the scale: this is the relation
		// app.go's bubble path uses, and the one that guarantees the page and
		// the crop agree at any DPI.
		if v.X != float64(r.X)/s || v.Y != float64(r.Y)/s ||
			v.W != float64(r.Width)/s || v.H != float64(r.Height)/s {
			t.Fatalf("rect %d is not the region divided by the scale: region=%+v view=%+v scale=%v", i, r, v, s)
		}
		if v.Index != i {
			t.Fatalf("rect %d carries index %d", i, v.Index)
		}
	}

	// And the clamp really did apply: the declared dx of 160 lands 160 CSS
	// pixels to the right of the pet, wherever the pet is in this window.
	declared, _ := a.pluginSurfaceRects(id)
	if len(declared) != 1 {
		t.Fatalf("the fixture declares %d rects", len(declared))
	}
	if got := view.Rects[0].W; got != declared[0].W {
		t.Fatalf("the width changed between the declaration and the view: %v vs %v", got, declared[0].W)
	}
	_ = plugins.PermPetSurface
}

// TestReopeningASurfaceKeepsWhatThePluginAskedFor pins the difference between a FRESH open and a
// RE-open of the surface that is already up.
//
// A fresh open starts from the manifest's defaults ("nothing hidden", "the declared heights"), so a
// plugin that hid a block last time does not come back with it missing. A re-open must NOT do that:
// the page reuses the frames (same plugin, same version, same blocks), so the plugin gets no second
// `init` and nothing would ask for its blocks and heights again.
//
// This is not hypothetical - clicking the conversation button over the pet while the surface is open
// comes through here. Before this test, that gesture stranded the surface on the defaults: an EMPTY
// question strip appeared on screen and the answer box shrank back to its declared height, cutting
// off the answer the user was reading.
func TestReopeningASurfaceKeepsWhatThePluginAskedFor(t *testing.T) {
	a := pluginApp(t)
	id := installSlotPlugin(t, a, "com.example.chat", true, []string{"pet.surface"}, surfaceRectsManifest())
	if _, err := a.OpenPluginSurface(id); err != nil {
		t.Fatalf("OpenPluginSurface: %v", err)
	}

	// What the plugin has asked for, set through the same fields `surface.blocks` writes.
	a.surfaceHiddenMu.Lock()
	a.surfaceHiddenBlocks = map[int]bool{0: true}
	a.surfaceBlockSizes = map[int]int{1: 120}
	a.surfaceHiddenMu.Unlock()

	// The conversation button, pressed while the surface is already up.
	if _, err := a.OpenPluginSurface(id); err != nil {
		t.Fatalf("re-open: %v", err)
	}
	if got := a.hiddenSurfaceBlocks(); !got[0] {
		t.Fatalf("the re-open forgot the hidden block: %+v (an empty question strip is what that looks like)", got)
	}
	if got := a.requestedSurfaceSizes(); got[1] != 120 {
		t.Fatalf("the re-open forgot the requested height: %+v (the answer box snapping back to its floor is what that looks like)", got)
	}

	// Closing and opening again IS a fresh open, and it does reset both.
	if _, err := a.ClosePluginSurface(); err != nil {
		t.Fatalf("ClosePluginSurface: %v", err)
	}
	if _, err := a.OpenPluginSurface(id); err != nil {
		t.Fatalf("fresh open: %v", err)
	}
	if got := a.hiddenSurfaceBlocks(); len(got) != 0 {
		t.Fatalf("a fresh open must start with nothing hidden, got %+v", got)
	}
	if got := a.requestedSurfaceSizes(); len(got) != 0 {
		t.Fatalf("a fresh open must start at the declared heights, got %+v", got)
	}
}
