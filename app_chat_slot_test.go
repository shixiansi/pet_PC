package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"PetPC/internal/plugins"
)

// The tests in this file pin the CHAT SLOT: which installed plugin owns the
// pet's 对话 tab, and what a double-click on the pet opens.
//
// The slot is resolved from declarations rather than from a built-in id, so the
// fixtures deliberately use ids that are NOT the shipped plugin's. A test that
// used the real id would pass for the wrong reason on the day someone
// reintroduces a special case for it.

// --- fixtures --------------------------------------------------------------

// installSlotPlugin installs a plugin that DECLARES the chat slot, with the
// given enabled and granted states, and returns its id.
//
// It goes through the manager directly rather than the (about-to-be-removed)
// management bindings, so these tests keep working once the plugin tab is gone:
// the slot resolver reads the same on-disk index either way, and going through
// fewer layers is what makes "which plugin won" the thing under test.
func installSlotPlugin(t *testing.T, a *App, id string, on bool, granted []string, over map[string]any) string {
	t.Helper()
	man := map[string]any{
		"id":          id,
		"name":        "槽位插件 " + id,
		"version":     "1.0.0",
		"author":      "tester",
		"description": "声明对话槽位的测试插件",
		"permissions": []string{"pet.surface"},
		"panel":       true,
		"panelSlot":   plugins.PanelSlotChat,
		"entry":       "panel.html",
	}
	for k, v := range over {
		if v == nil {
			// A nil override DELETES the field, which is how the "does not claim
			// the slot" case is built: omitting panelSlot entirely is not the
			// same fixture as setting it to another string, and only the first
			// matches what such a plugin would ship.
			delete(man, k)
			continue
		}
		man[k] = v
	}
	installManifest(t, a, man)
	if _, err := a.pluginManager().GrantPermissions(id, granted); err != nil {
		t.Fatalf("GrantPermissions(%s): %v", id, err)
	}
	if on {
		if err := a.pluginManager().SetEnabled(id, true); err != nil {
			t.Fatalf("SetEnabled(%s): %v", id, err)
		}
	}
	return id
}

// installManifest packs the given manifest into a zip served over HTTP and
// installs it, WITHOUT granting or enabling anything.
//
// The zip has to exist: the installer verifies the digest against the catalogue
// entry and unpacks the manifest from the archive, so a manifest cannot simply
// be handed to the manager.
func installManifest(t *testing.T, a *App, man map[string]any) {
	t.Helper()
	installManifestWith(t, a.pluginManager(), man)
}

// installManifestWith installs a manifest through a caller-supplied manager.
//
// It takes the manager rather than the App so a test can install through a
// SECOND manager over the same data directory - the arrangement the separate
// market executable produces.
func installManifestWith(t *testing.T, m *plugins.Manager, man map[string]any) {
	t.Helper()
	manifestBytes, err := json.Marshal(man)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"manifest.json": string(manifestBytes),
		"panel.html":    "<!doctype html><p>slot test</p>",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	entry := plugins.MarketEntry{
		ID:          man["id"].(string),
		Name:        man["name"].(string),
		Version:     man["version"].(string),
		Author:      man["author"].(string),
		Description: descriptionOf(man),
		Permissions: stringsFrom(man["permissions"]),
		NetHosts:    stringsFrom(man["netHosts"]),
		Download:    srv.URL + "/pkg.zip",
		SHA256:      hex.EncodeToString(sum[:]),
	}
	if _, err := m.Install(entry); err != nil {
		t.Fatalf("Install(%s): %v", entry.ID, err)
	}
}

// descriptionOf reads the optional description, which several fixtures omit.
func descriptionOf(man map[string]any) string {
	if s, ok := man["description"].(string); ok {
		return s
	}
	return ""
}

// --- T47/T48: the installed snapshot ---------------------------------------

// TestPluginsInstalledProjectsTheManifest covers T47: the row the slot logic and
// the frontend both read carries every field the decision needs, and it agrees
// with what is on disk.
//
// panelSlot in particular has to be here. Without it the page would have to
// fetch each manifest to find out who owns its own tab, and that second lookup
// is where "the host and the page disagree" bugs live.
func TestPluginsInstalledProjectsTheManifest(t *testing.T) {
	a := pluginApp(t)
	id := installSlotPlugin(t, a, "com.example.slot", true, []string{"pet.surface"}, nil)

	rows, err := a.PluginsInstalled()
	if err != nil {
		t.Fatalf("PluginsInstalled: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	got := rows[0]
	if got.ID != id {
		t.Errorf("id = %q, want %q", got.ID, id)
	}
	if !got.Enabled {
		t.Error("the row must report the plugin as enabled")
	}
	if got.PanelSlot != plugins.PanelSlotChat {
		t.Errorf("panelSlot = %q, want %q", got.PanelSlot, plugins.PanelSlotChat)
	}
	if !got.Panel {
		t.Error("a chat-slot plugin must report panel=true")
	}
	if len(got.Permissions) != 1 || got.Permissions[0] != "pet.surface" {
		t.Errorf("permissions = %v, want [pet.surface]", got.Permissions)
	}
	if len(got.Granted) != 1 || got.Granted[0] != "pet.surface" {
		t.Errorf("granted = %v, want [pet.surface]", got.Granted)
	}
	if len(got.Pending) != 0 {
		t.Errorf("pending = %v, want nothing once everything is granted", got.Pending)
	}
	// The name comes from the manifest, not from the id: a row that fell back to
	// the id would render the raw reverse-domain name in the UI.
	if got.Name != "槽位插件 com.example.slot" {
		t.Errorf("name = %q", got.Name)
	}
}

// TestPluginsInstalledSeesAnotherProcessWrite covers T48: the market program is
// a SEPARATE process that writes the same index, so the pet has to see its
// changes without a restart.
//
// The writer here is a second manager over the same data directory - the exact
// arrangement the separate market executable produces, minus the process
// boundary, which the pet cannot observe anyway.
func TestPluginsInstalledSeesAnotherProcessWrite(t *testing.T) {
	a := pluginApp(t)
	if rows, err := a.PluginsInstalled(); err != nil {
		t.Fatalf("PluginsInstalled: %v", err)
	} else if len(rows) != 0 {
		t.Fatalf("the pet must start with no plugins, got %d", len(rows))
	}

	// The second manager installs through the same path the market program uses.
	other, err := plugins.NewManager(a.store.Dir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	installManifestWith(t, other, map[string]any{
		"id":          "com.example.other",
		"name":        "另一个进程装的",
		"version":     "1.0.0",
		"author":      "tester",
		"description": "由第二个 manager 安装",
		"panel":       true,
		"entry":       "panel.html",
	})

	rows, err := a.PluginsInstalled()
	if err != nil {
		t.Fatalf("PluginsInstalled: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the pet must see the new plugin, got %d rows", len(rows))
	}
	if rows[0].ID != "com.example.other" {
		t.Fatalf("the pet saw %q, want com.example.other", rows[0].ID)
	}
	if rows[0].Name != "另一个进程装的" {
		t.Fatalf("the pet read a stale manifest: name = %q", rows[0].Name)
	}
}

// --- T49: the slot resolver ------------------------------------------------

// TestChatSlotPluginPicksTheFirstUsableClaimant covers T49: three claimants are
// installed and only the third is usable, so the third wins.
//
// The order is plugins.json order, which is install order here. Each of the two
// rejected rows fails for a DIFFERENT reason (disabled, then ungranted), so a
// resolver that only checked one clause cannot pass by accident.
func TestChatSlotPluginPicksTheFirstUsableClaimant(t *testing.T) {
	a := pluginApp(t)
	installSlotPlugin(t, a, "com.example.disabled", false, []string{"pet.surface"}, nil)
	installSlotPlugin(t, a, "com.example.ungranted", true, nil, nil)
	want := installSlotPlugin(t, a, "com.example.usable", true, []string{"pet.surface"}, nil)

	row, ok := a.chatSlotPlugin()
	if !ok {
		t.Fatal("a usable claimant must own the slot")
	}
	if row.ID != want {
		t.Fatalf("the slot went to %q, want %q", row.ID, want)
	}
	// The returned row must be the full manager row: the surface geometry reads
	// Manifest.Surface from it, so a row without a manifest would open a surface
	// with no rectangles.
	if row.Manifest == nil {
		t.Fatal("the resolved row must carry its manifest")
	}
	if row.Manifest.PanelSlot != plugins.PanelSlotChat {
		t.Fatalf("the resolved row declares panelSlot %q", row.Manifest.PanelSlot)
	}
}

// TestChatSlotPluginStaysEmptyWithoutAUsableClaimant covers the other half of
// T49: with only the two unusable rows installed, nobody owns the slot.
//
// "Found nothing" is a normal answer, not an error: the pet has to start, show
// the slot's explanation and let the user install the plugin later.
func TestChatSlotPluginStaysEmptyWithoutAUsableClaimant(t *testing.T) {
	a := pluginApp(t)
	installSlotPlugin(t, a, "com.example.disabled", false, []string{"pet.surface"}, nil)
	installSlotPlugin(t, a, "com.example.ungranted", true, nil, nil)

	if row, ok := a.chatSlotPlugin(); ok {
		t.Fatalf("the slot must stay empty, but %q claimed it", row.ID)
	}
}

// TestChatSlotPluginIgnoresAPluginThatDoesNotClaimTheSlot pins the declaration
// clause: a fully enabled and granted plugin that never asked for the chat slot
// must not be handed it.
//
// This is the assertion that keeps the slot mechanism generic. A resolver that
// fell back to "some enabled plugin with pet.surface" would work for the shipped
// chat plugin and quietly put an unrelated plugin in the conversation tab.
func TestChatSlotPluginIgnoresAPluginThatDoesNotClaimTheSlot(t *testing.T) {
	a := pluginApp(t)
	// panelSlot omitted entirely - the empty string, which is the default.
	installSlotPlugin(t, a, "com.example.generic", true, []string{"pet.surface"},
		map[string]any{"panelSlot": nil})

	if row, ok := a.chatSlotPlugin(); ok {
		t.Fatalf("%q does not declare the slot but was given it", row.ID)
	}
}

// TestChatSlotPluginRequiresTheSurfaceGrant pins the grant clause on its own: an
// enabled claimant without pet.surface owns nothing, because the slot renders a
// sandboxed iframe that draws outside the pet's box.
func TestChatSlotPluginRequiresTheSurfaceGrant(t *testing.T) {
	a := pluginApp(t)
	id := installSlotPlugin(t, a, "com.example.nogrant", true, nil, nil)
	if row, ok := a.chatSlotPlugin(); ok {
		t.Fatalf("the slot must require the grant, but %q claimed it", row.ID)
	}
	// Granting it makes the same plugin usable, so the earlier failure was the
	// grant and not something else about the fixture.
	if _, err := a.pluginManager().GrantPermissions(id, []string{"pet.surface"}); err != nil {
		t.Fatalf("GrantPermissions: %v", err)
	}
	row, ok := a.chatSlotPlugin()
	if !ok || row.ID != id {
		t.Fatalf("after granting pet.surface the slot must be %q, got %q (ok=%v)", id, row.ID, ok)
	}
}

// TestChatSlotPluginOrderFollowsTheIndex pins that install ORDER decides, not
// some other ordering.
//
// Two usable claimants are installed, so the answer is observable: the first one
// installed wins, and the result does not change when the second is re-enabled
// (which rewrites the index entry but keeps its position). "Which of two
// claimants wins" has to be a fact the user can predict.
func TestChatSlotPluginOrderFollowsTheIndex(t *testing.T) {
	a := pluginApp(t)
	first := installSlotPlugin(t, a, "com.example.first", true, []string{"pet.surface"}, nil)
	second := installSlotPlugin(t, a, "com.example.second", true, []string{"pet.surface"}, nil)
	if first == second {
		t.Fatal("the fixture installed the same id twice")
	}

	if row, _ := a.chatSlotPlugin(); row.ID != first {
		t.Fatalf("the first installed must win, got %q", row.ID)
	}
	if err := a.pluginManager().SetEnabled(second, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if err := a.pluginManager().SetEnabled(second, true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if row, _ := a.chatSlotPlugin(); row.ID != first {
		t.Fatalf("re-enabling the second must not move the slot, got %q", row.ID)
	}
}

// --- T55: the double-click entry -------------------------------------------

// surfaceRectsManifest is the override that gives a slot plugin a floating
// surface, so the double-click path has something to open.
func surfaceRectsManifest() map[string]any {
	return map[string]any{
		"permissions": []string{"pet.surface"},
		"surface": []map[string]any{
			{"dx": 10, "dy": 10, "w": 200, "h": 120, "radius": 12},
			{"dx": 40, "dy": 140, "w": 34, "h": 20, "radius": 10, "ellipse": true},
		},
	}
}

// TestOpenChatPluginSurfaceOpensTheClaimantsSurface covers T55's positive half:
// the double-click entry opens the claiming plugin's own declared rectangles,
// through the existing OpenPluginSurface.
//
// The layout's PluginID is the load-bearing assertion. A path that opened "a"
// surface - any surface - would still crop the window and still draw something;
// only the id proves it opened the CHAT plugin's, which is what the double click
// promises.
func TestOpenChatPluginSurfaceOpensTheClaimantsSurface(t *testing.T) {
	a := pluginApp(t)
	id := installSlotPlugin(t, a, "com.example.chat", true, []string{"pet.surface"}, surfaceRectsManifest())

	layout, err := a.OpenChatPluginSurface()
	if err != nil {
		t.Fatalf("OpenChatPluginSurface: %v", err)
	}
	if layout.PluginSurface == nil {
		t.Fatal("the layout must advertise the surface")
	}
	if layout.PluginSurface.PluginID != id {
		t.Fatalf("the surface belongs to %q, want %q", layout.PluginSurface.PluginID, id)
	}
	if len(layout.PluginSurface.Rects) != 2 {
		t.Fatalf("want the 2 declared rectangles, got %d", len(layout.PluginSurface.Rects))
	}
	// The ellipse flag travels with the geometry, so the page draws the puff as
	// the ellipse the region is cropped to.
	if layout.PluginSurface.Rects[1].Ellipse != true {
		t.Error("the second rectangle declared an ellipse")
	}

	// The state agrees with the layout - the invariant the whole geometry design
	// rests on.
	a.mu.Lock()
	state := a.pluginSurface
	a.mu.Unlock()
	if state == nil || state.PluginID != id {
		t.Fatalf("the surface state disagrees with the layout: %+v", state)
	}
}

// TestOpenChatPluginSurfaceWithoutAPluginShowsABubble covers T55's negative
// half: with nobody claiming the slot, nothing is opened and the pet explains
// why in a bubble.
//
// The bubble must be non-nil and must say where to go. A double-click that
// silently did nothing is indistinguishable from a frozen app, which is the
// failure this bubble exists to prevent.
func TestOpenChatPluginSurfaceWithoutAPluginShowsABubble(t *testing.T) {
	a := pluginApp(t)
	// Installed and granted, but disabled: an unusable claimant still means
	// "nothing to open".
	installSlotPlugin(t, a, "com.example.disabled", false, []string{"pet.surface"}, surfaceRectsManifest())

	layout, err := a.OpenChatPluginSurface()
	if err != nil {
		t.Fatalf("a missing plugin is not an error: %v", err)
	}
	if layout.PluginSurface != nil {
		t.Fatalf("no surface may be advertised: %+v", layout.PluginSurface)
	}
	if layout.Bubble == nil {
		t.Fatal("the pet must explain itself in a bubble")
	}
	// It has to name where the installer actually IS. That used to be 「设置 → 打开插件市场」;
	// the settings tab is gone (ui-rebuild 阶段 3), so the one entry point left is the
	// pet's tray menu - and a bubble that points at a button nobody can find is the same
	// failure as a bubble that says nothing.
	if !strings.Contains(layout.Bubble.Title, "控制中心") {
		t.Fatalf("the bubble must name the control center, got %q", layout.Bubble.Title)
	}
	if !strings.Contains(layout.Bubble.Title, "对话插件") {
		t.Fatalf("the bubble must name the chat plugin, got %q", layout.Bubble.Title)
	}
	if layout.Bubble.Kind != BubbleReminder {
		t.Fatalf("the bubble kind is %q, want %q", layout.Bubble.Kind, BubbleReminder)
	}
}

// TestOpenChatPluginSurfaceDoesNotDisplaceAQuestion pins that the double-click
// entry inherits OpenPluginSurface's guard rather than bypassing it.
//
// A question the user has not answered owns the slot. Opening the surface over
// it would not cover the question, it would DELETE it - and the timer would stay
// parked with nothing on screen explaining why. The refusal has to change
// nothing: no surface, and the bubble still the one that was there.
func TestOpenChatPluginSurfaceDoesNotDisplaceAQuestion(t *testing.T) {
	a := pluginApp(t)
	installSlotPlugin(t, a, "com.example.chat", true, []string{"pet.surface"}, surfaceRectsManifest())

	if _, err := a.showBubble(&BubbleView{Kind: BubblePlan, Title: "下一次专注做什么？"}); err != nil {
		// A window error is expected and harmless: there is no window, so the
		// crop cannot run, but the STATE the guard reads is already recorded.
		t.Logf("showBubble (no window): %v", err)
	}

	layout, err := a.OpenChatPluginSurface()
	if err == nil {
		t.Fatal("opening over an unanswered question must be refused")
	}
	if layout.PluginSurface != nil {
		t.Fatalf("a refused open must not advertise a surface: %+v", layout.PluginSurface)
	}
	a.mu.Lock()
	bubble := a.bubble
	surface := a.pluginSurface
	a.mu.Unlock()
	if bubble == nil || bubble.Kind != BubblePlan {
		t.Fatalf("the question was displaced: %+v", bubble)
	}
	if surface != nil {
		t.Fatalf("a surface was opened anyway: %+v", surface)
	}
}

// TestOpenChatPluginSurfaceRequiresDeclaredRectangles pins that a claimant with
// no surface blocks falls into the "cannot crop to it" refusal rather than
// opening an empty surface.
//
// The plugin is otherwise perfect (enabled, granted, claims the slot), so the
// only thing under test is what happens when a declaration cannot describe a
// shape. An empty surface would leave the layout advertising blocks that do not
// exist.
func TestOpenChatPluginSurfaceRequiresDeclaredRectangles(t *testing.T) {
	a := pluginApp(t)
	installSlotPlugin(t, a, "com.example.norects", true, []string{"pet.surface"}, nil)

	layout, err := a.OpenChatPluginSurface()
	if err == nil {
		t.Fatal("a claimant with no surface declaration must be refused")
	}
	if layout.PluginSurface != nil {
		t.Fatalf("no surface may be advertised: %+v", layout.PluginSurface)
	}
	if !strings.Contains(err.Error(), "surface") {
		t.Fatalf("error %q must say the plugin declared no surface", err.Error())
	}
}
