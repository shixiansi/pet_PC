package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"PetPC/internal/plugins"
	"PetPC/internal/store"
)

// --- fixtures --------------------------------------------------------------

// pluginApp builds an App with a store in a temp directory AND a plugin manager
// whose market address points at the built-in sample source, so tests install
// from a source that is always present.
//
// There is no window, so anything that reaches the native handle fails and is
// logged rather than applied - which is the same arrangement the existing
// bubble tests use, and deliberate here: the state/layout invariants this file
// pins must hold even when the geometry call cannot complete.
func pluginApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.MarketBaseURL = "" }); err != nil {
		t.Fatalf("point the market at the built-in source: %v", err)
	}
	return a
}

// collectPluginEvents installs an emit collector and returns an accessor for the
// pet:plugin-event frames carrying a given name.
//
// The collector is the same technique the LLM tests use, and it is the only way
// to assert on the down-link without a running window: emitEvent is a Wails call
// that refuses any context other than the one its lifecycle hook supplied.
func collectPluginEvents(a *App) func(name string) []PluginEventView {
	var mu sync.Mutex
	all := []PluginEventView{}
	a.emitFn = func(event string, data ...any) {
		// The event NAME is part of the contract: the frontend subscribes to
		// exactly one, so a frame emitted under any other name would never reach
		// a plugin at all. Frames under other names are ignored here so the
		// assertion is about plugin traffic rather than about every event the app
		// sends.
		if event != "pet:plugin-event" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, d := range data {
			if ev, ok := d.(PluginEventView); ok {
				all = append(all, ev)
			}
		}
	}
	return func(name string) []PluginEventView {
		mu.Lock()
		defer mu.Unlock()
		out := []PluginEventView{}
		for _, ev := range all {
			if ev.Name == name {
				out = append(out, ev)
			}
		}
		return out
	}
}

// --- management helpers ----------------------------------------------------
//
// The pet no longer exposes bindings for installing, enabling, granting or
// uninstalling a plugin: that management surface lives in PetPCPluginMarket.exe,
// which calls internal/plugins directly. These helpers therefore do the same
// thing the market program does - they drive the MANAGER, not the app - so the
// tests exercise the same code path the market does and keep working when the
// host's bindings are gone.
//
// They take the app's own manager (a.pluginManager()) rather than opening a
// second one over the same directory: a second Manager would work, but the app
// would then be reading an index it did not write, which is the cross-process
// case that app_chat_slot_test.go covers separately.

// managerInstall installs the plugin with this id from the built-in source.
func managerInstall(t *testing.T, a *App, id string) {
	t.Helper()
	m := a.pluginManager()
	got, err := m.Market()
	if err != nil {
		t.Fatalf("Market: %v", err)
	}
	for _, e := range got.Entries {
		if e.ID != id {
			continue
		}
		if _, err := m.Install(e); err != nil {
			t.Fatalf("Install(%s): %v", id, err)
		}
		return
	}
	t.Fatalf("the built-in source has no plugin %s", id)
}

// managerGrant grants everything the plugin declared.
func managerGrant(t *testing.T, a *App, id string) {
	t.Helper()
	row, ok := a.pluginManager().Get(id)
	if !ok || row.Manifest == nil {
		t.Fatalf("plugin %s is not installed with a readable manifest", id)
	}
	if _, err := a.pluginManager().GrantPermissions(id, row.Manifest.Permissions); err != nil {
		t.Fatalf("GrantPermissions(%s): %v", id, err)
	}
}

// managerSetEnabled switches a plugin on or off.
func managerSetEnabled(t *testing.T, a *App, id string, on bool) {
	t.Helper()
	if err := a.pluginManager().SetEnabled(id, on); err != nil {
		t.Fatalf("SetEnabled(%s, %v): %v", id, on, err)
	}
}

// managerUninstall removes a plugin.
func managerUninstall(t *testing.T, a *App, id string) {
	t.Helper()
	if err := a.pluginManager().Uninstall(id, true); err != nil {
		t.Fatalf("Uninstall(%s): %v", id, err)
	}
}

// samplePluginID is the catalogue entry these tests mean by "the sample plugin".
//
// It is named rather than taken as `Entries[0]`, and that is a fix rather than tidiness:
// `Entries[0]` used to be this plugin, then a `tools/package.ps1` run re-sorted the catalogue
// alphabetically and put the B站 plugin first - which declares no `surface` and no
// `pet.surface`, so TestBubbleAndLayoutStayInStep started failing with 「未获权限 pet.surface」
// on a tree where nothing about permissions had changed. The ORDER of a catalogue is a
// packaging artifact; a test that depends on it is testing the packaging.
const samplePluginID = "com.shixiansi.plugin-chat"

// installSample installs the built-in sample plugin and returns its id.
func installSample(t *testing.T, a *App) string {
	t.Helper()
	view, err := a.pluginManager().Market()
	if err != nil {
		t.Fatalf("Market: %v", err)
	}
	if len(view.Entries) == 0 {
		t.Fatal("the built-in source is empty")
	}
	found := false
	for _, e := range view.Entries {
		if e.ID == samplePluginID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the built-in source no longer lists %s (it lists %d entries)", samplePluginID, len(view.Entries))
	}
	managerInstall(t, a, samplePluginID)
	return samplePluginID
}

// readyPlugin installs, grants everything, and enables.
func readyPlugin(t *testing.T, a *App) string {
	t.Helper()
	id := installSample(t, a)
	managerGrant(t, a, id)
	managerSetEnabled(t, a, id, true)
	return id
}

// installSampleWith installs a plugin whose manifest is the standard test
// manifest overridden with the given fields, served from a local HTTP server,
// and returns its id with everything it declared already granted and enabled.
//
// Capability tests need a plugin that DECLARES the permission under test: the
// shipped sample only asks for llm.chat/pet.bubble/pet.surface, and granting a
// plugin a permission it never declared is refused by design.
func installSampleWith(t *testing.T, a *App, over map[string]any) string {
	t.Helper()
	id := installTestPlugin(t, a, over)
	managerGrant(t, a, id)
	managerSetEnabled(t, a, id, true)
	return id
}

// installTestPlugin installs the overridden test manifest WITHOUT granting or
// enabling anything, for the tests that drive those steps themselves.
func installTestPlugin(t *testing.T, a *App, over map[string]any) string {
	t.Helper()
	man := map[string]any{
		"id":          "com.example.test",
		"name":        "测试插件",
		"version":     "1.0.0",
		"author":      "tester",
		"description": "用于测试的插件",
		"permissions": []string{"pet.bubble"},
		"panel":       true,
		"entry":       "panel.html",
	}
	for k, v := range over {
		if v == nil {
			delete(man, k)
			continue
		}
		man[k] = v
	}
	manifestBytes, err := json.Marshal(man)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	files := map[string]string{
		"manifest.json": string(manifestBytes),
		"panel.html":    "<!doctype html><p>test</p>",
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
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

	perms := stringsFrom(man["permissions"])
	hosts := stringsFrom(man["netHosts"])
	entry := plugins.MarketEntry{
		ID:          man["id"].(string),
		Name:        man["name"].(string),
		Version:     man["version"].(string),
		Author:      man["author"].(string),
		Description: man["description"].(string),
		Permissions: perms,
		NetHosts:    hosts,
		Download:    srv.URL + "/pkg.zip",
		SHA256:      hex.EncodeToString(sum[:]),
	}
	if _, err := a.pluginManager().Install(entry); err != nil {
		t.Fatalf("Install: %v", err)
	}
	return entry.ID
}

// stringsFrom reads a manifest field as a string slice, accepting both []string
// (built in Go) and []any (the shape a test may build inline).
func stringsFrom(v any) []string {
	switch got := v.(type) {
	case []string:
		return got
	case []any:
		out := make([]string, 0, len(got))
		for _, item := range got {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// --- T33/T34: the dispatcher's first two gates ------------------------------

// TestPluginRequestUnknownMethod covers T33: an unknown method is refused and
// nothing at all changes.
func TestPluginRequestUnknownMethod(t *testing.T) {
	a := pluginApp(t)
	id := readyPlugin(t, a)

	todosBefore := len(a.store.Todos())
	layoutBefore := a.currentLayout()
	_, err := a.PluginRequest(id, "no.such.method", "{}")
	if err == nil {
		t.Fatal("an unknown method must be refused")
	}
	want := "插件 " + id + " 调用了未知方法 no.such.method"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if len(a.store.Todos()) != todosBefore {
		t.Error("the todo list changed")
	}
	if got := a.currentLayout(); got.PluginSurface != layoutBefore.PluginSurface {
		t.Error("the layout changed")
	}
	a.mu.Lock()
	bub := a.bubble
	a.mu.Unlock()
	if bub != nil {
		t.Error("a bubble appeared")
	}
}

// TestPluginRequestDisabledOrMissing covers T34: both a disabled plugin and an
// uninstalled one are refused with the same sentence, and neither changes state.
func TestPluginRequestDisabledOrMissing(t *testing.T) {
	a := pluginApp(t)
	id := readyPlugin(t, a)
	// Grant, then disable: the plugin is installed but switched off.
	managerSetEnabled(t, a, id, false)

	for _, target := range []string{id, "com.example.never-installed"} {
		before := len(a.store.Todos())
		_, err := a.PluginRequest(target, "storage.get", `{}`)
		if err == nil {
			t.Fatalf("%s must be refused", target)
		}
		want := "插件 " + target + " 未启用"
		if err.Error() != want {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
		if len(a.store.Todos()) != before {
			t.Error("the todo list changed")
		}
	}
}

// --- T35: an unauthorised call changes NOTHING -------------------------------

// TestUnauthorisedCallsChangeNothing is the single most important assertion in
// this file. Every capability is called by a plugin that was granted nothing, and
// each refusal must leave every observable piece of state exactly as it was:
// the todos, the bubble, the layout, the surface, the installed set and the
// plugin's private directory.
//
// Without this, "you declined this permission" would be a message printed after
// the work had already been done.
func TestUnauthorisedCallsChangeNothing(t *testing.T) {
	a := pluginApp(t)
	id := installSample(t, a)
	// Enable WITHOUT granting anything: the plugin can be switched on and every
	// capability must still be refused.
	managerSetEnabled(t, a, id, true)

	before := a.store.Todos()
	beforeCount := len(before)
	beforePlugins := len(a.pluginManager().Plugins())
	layoutBefore := a.currentLayout()

	methods := []string{
		"events.subscribe", "bubble.say", "net.fetch",
		"storage.get", "storage.set", "storage.remove",
		"surface.show", "surface.hide", "llm.chat",
		"config.get", "config.set",
	}
	for _, method := range methods {
		_, err := a.PluginRequest(id, method, `{"title":"x","id":"y","key":"k","value":"v","text":"hi","names":["reminder.fired"],"url":"http://127.0.0.1/","method":"GET","values":{"llmBaseUrl":"https://evil.example.com"}}`)
		if err == nil {
			t.Fatalf("%s must be refused without a grant", method)
		}
		if !strings.Contains(err.Error(), "未获权限") {
			t.Fatalf("%s: error %q must say the permission was not granted", method, err.Error())
		}
	}

	a.mu.Lock()
	bubble := a.bubble
	surface := a.pluginSurface
	layoutSurface := a.layout.PluginSurface
	a.mu.Unlock()

	if len(a.store.Todos()) != beforeCount {
		t.Errorf("the todo list changed: %d -> %d", beforeCount, len(a.store.Todos()))
	}
	if bubble != nil {
		t.Error("a bubble was shown")
	}
	if layoutSurface != nil {
		t.Error("the layout advertises a bubble")
	}
	if surface != nil {
		t.Error("a plugin surface was opened")
	}
	if got := a.currentLayout(); got.PluginSurface != layoutBefore.PluginSurface {
		t.Error("the layout changed")
	}
	if got := len(a.pluginManager().Plugins()); got != beforePlugins {
		t.Errorf("the installed set changed: %d -> %d", beforePlugins, got)
	}
	private, _ := a.pluginManager().PrivateDir(id)
	entries, _ := os.ReadDir(private)
	if len(entries) != 0 {
		t.Errorf("private storage was written: %v", entries)
	}
}

// --- T36-T40: the todos capabilities are GONE -------------------------------

// TestTodosCapabilitiesAreGone pins the retirement from the plugin's side: the five
// `todos.*` methods were the host's todo store handed to plugins, and the store is the
// todo plugin's own `storage` now (openspec/changes/todo-pomodoro-as-plugins). A plugin
// that still asks gets the dispatcher's "no such method" - NOT a permission error - which
// is the distinction worth pinning: the method is absent, not gated.
//
// The tests that used to live here proved the read and the four writes worked. They are
// deleted rather than rewritten because there is nothing left to call: the data moved,
// and the pomodoro reads the snapshot the todo plugin publishes through `plugin.share`
// (covered by market/tests/plugin-todo.test.js on the publishing side).
func TestTodosCapabilitiesAreGone(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"pet.bubble"}})

	for _, method := range []string{"todos.list", "todos.add", "todos.update", "todos.toggle", "todos.remove"} {
		if _, err := a.PluginRequest(id, method, `{"title":"x","id":"y"}`); err == nil {
			t.Fatalf("%s must not exist any more", method)
		} else if strings.Contains(err.Error(), "未获权限") {
			t.Errorf("%s: refused for the wrong reason (%v) - the method is gone, not gated", method, err)
		}
	}
}
// --- T42-T44: storage ------------------------------------------------------

// TestStorageRoundTrip covers T42 and T43: a value is written inside the
// plugin's own directory, read back, and removed.
func TestStorageRoundTrip(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"storage"}})

	if _, err := a.PluginRequest(id, "storage.set", `{"key":"k1","value":"v1"}`); err != nil {
		t.Fatalf("storage.set: %v", err)
	}
	private, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(private, "k1"))
	if err != nil {
		t.Fatalf("the value was not written: %v", err)
	}
	if string(data) != "v1" {
		t.Fatalf("stored %q, want v1", string(data))
	}

	out, err := a.PluginRequest(id, "storage.get", `{"key":"k1"}`)
	if err != nil {
		t.Fatalf("storage.get: %v", err)
	}
	var got struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Value != "v1" {
		t.Fatalf("storage.get returned %q", got.Value)
	}

	if _, err := a.PluginRequest(id, "storage.remove", `{"key":"k1"}`); err != nil {
		t.Fatalf("storage.remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(private, "k1")); !os.IsNotExist(err) {
		t.Fatalf("the value survived removal: %v", err)
	}
}

// TestStorageRejectsBadKeys covers T44: a key names a path under the plugin's own
// directory, so anything that could climb out of it - or that is not a name at all -
// must be refused and NOTHING may be written.
//
// `a/b` used to be in this list, back when a key was a bare file name. It is legal
// now (keys name folders, see plugins.StorageKeyPath) and its legality is asserted
// by TestStorageKeyNamesFolders; what stays illegal is every spelling of "outside"
// and every name that could mean two different paths on two platforms.
func TestStorageRejectsBadKeys(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"storage"}})
	private, _ := a.pluginManager().PrivateDir(id)
	before, _ := os.ReadDir(private)

	bad := []string{
		"../escape",             // traversal
		"..",                    // the directory itself
		"a/../../escape",        // traversal in the middle
		"/absolute",             // absolute path
		"",                      // nothing at all
		"a//b",                  // an empty segment is a second spelling of the same path
		"a/b/",                  // trailing separator
		`a\b`,                   // the Windows separator, i.e. two names for one file
		"with.dot",              // an extension, which the video bridge's cleanup relies on
		"a/./b",                 // a dot segment
		strings.Repeat("x", 65), // one segment over the limit
		"a/b/c/d/e",             // deeper than the limit
		strings.Repeat("y", 60) + "/" + strings.Repeat("y", 60) + "/" + strings.Repeat("y", 60) + "/" + strings.Repeat("y", 60), // too long overall
	}
	for _, key := range bad {
		payload, _ := json.Marshal(map[string]string{"key": key, "value": "x"})
		if _, err := a.PluginRequest(id, "storage.set", string(payload)); err == nil {
			t.Fatalf("the key %q must be refused", key)
		}
	}

	after, _ := os.ReadDir(private)
	if len(after) != len(before) {
		t.Fatalf("a refused write created files: %d -> %d", len(before), len(after))
	}
	// Nothing escaped into the data directory either.
	entries, _ := os.ReadDir(a.pluginManager().DataDir())
	for _, e := range entries {
		if e.Name() == "escape" {
			t.Error("a traversal key escaped the plugin directory")
		}
	}
}

// TestStorageKeyNamesFolders is the other half of the key contract, and the change
// that motivated it: the QQ plugin's flat directory held two hundred stickers, two
// 24 MB videos and a transcript per group, and nothing could tell them apart.
//
// Four things have to hold for a folder layout to be usable at all - the parent is
// created for you, the value round-trips, `stat` sees it where `set` put it, and
// removal takes the file and not the folder (an empty folder is harmless; a folder
// deleted from under a plugin is not).
func TestStorageKeyNamesFolders(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"storage"}})
	private, _ := a.pluginManager().PrivateDir(id)

	if _, err := a.PluginRequest(id, "storage.set", `{"key":"stickers/bytes/790ed6cc","value":"blob"}`); err != nil {
		t.Fatalf("storage.set: %v", err)
	}
	// The folders were created by the write: a plugin has no mkdir, and asking it to
	// create three of them with a capability it does not have would be a gap, not a
	// safety property.
	onDisk := filepath.Join(private, "stickers", "bytes", "790ed6cc")
	if data, err := os.ReadFile(onDisk); err != nil || string(data) != "blob" {
		t.Fatalf("the value is not at %s: %v (%q)", onDisk, err, string(data))
	}

	out, err := a.PluginRequest(id, "storage.get", `{"key":"stickers/bytes/790ed6cc"}`)
	if err != nil {
		t.Fatalf("storage.get: %v", err)
	}
	var got struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Value != "blob" {
		t.Fatalf("storage.get returned %s (%v)", out, err)
	}

	raw, err := a.PluginRequest(id, "storage.stat", `{"key":"stickers/bytes/790ed6cc"}`)
	if err != nil {
		t.Fatalf("storage.stat: %v", err)
	}
	if !strings.Contains(raw, `"bytes":4`) {
		t.Fatalf("storage.stat = %s, want 4 bytes", raw)
	}

	if _, err := a.PluginRequest(id, "storage.remove", `{"key":"stickers/bytes/790ed6cc"}`); err != nil {
		t.Fatalf("storage.remove: %v", err)
	}
	if _, err := os.Stat(onDisk); !os.IsNotExist(err) {
		t.Fatalf("the value survived removal: %v", err)
	}
	if info, err := os.Stat(filepath.Dir(onDisk)); err != nil || !info.IsDir() {
		t.Fatalf("removal took the folder with it: %v", err)
	}
}

// --- T45/T46: net.fetch ----------------------------------------------------

// TestNetFetchReadsLargeBodiesAndSaysWhenItTruncated covers the cap change that the sticker
// library forced.
//
// The old ceiling was 1 MiB and it was SILENT: the QQ plugin ended up with 156 of its 317
// stickers at exactly 1 MiB of base64 - truncated animations that looked like they worked.
// So the two claims here are "a 5 MiB body arrives whole" and "a 9 MiB body is cut at the
// cap AND the reply says so", because a ceiling nobody can detect is the bug.
func TestNetFetchReadsLargeBodiesAndSaysWhenItTruncated(t *testing.T) {
	const fiveMiB = 5 << 20
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size := fiveMiB
		if strings.Contains(r.URL.Path, "huge") {
			size = maxPluginFetchBytes + (1 << 20)
		}
		_, _ = w.Write(bytes.Repeat([]byte("A"), size))
	}))
	defer srv.Close()

	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{
		"permissions": []string{"net.fetch"},
		"netHosts":    []string{strings.TrimPrefix(srv.URL, "http://")},
	})

	read := func(path string) (int, bool) {
		raw, err := a.PluginRequest(id, "net.fetch", `{"method":"GET","url":"`+srv.URL+path+`","encoding":"base64"}`)
		if err != nil {
			t.Fatalf("net.fetch %s: %v", path, err)
		}
		var out struct {
			Body      string `json:"body"`
			Truncated bool   `json:"truncated"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		decoded, err := base64.StdEncoding.DecodeString(out.Body)
		if err != nil {
			t.Fatalf("body is not base64: %v", err)
		}
		return len(decoded), out.Truncated
	}

	if size, truncated := read("/ok"); size != fiveMiB || truncated {
		t.Fatalf("5 MiB body = %d bytes, truncated=%v; want %d, false", size, truncated, fiveMiB)
	}
	if size, truncated := read("/huge"); size != maxPluginFetchBytes || !truncated {
		t.Fatalf("over-cap body = %d bytes, truncated=%v; want %d, true", size, truncated, maxPluginFetchBytes)
	}
}

// TestNetFetchHonoursTheAllowList covers T45 and T46 together: an allowed host
// gets the request, a host outside the list gets a refusal AND no request at all.
//
// The second half is the one that matters. A refusal issued after the request
// would let a plugin use a rejected host as a probe for what is listening.
func TestNetFetchHonoursTheAllowList(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("pong"))
	}))
	defer srv.Close()

	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{
		"permissions": []string{"net.fetch"},
		"netHosts":    []string{strings.TrimPrefix(srv.URL, "http://")},
	})

	if _, err := a.PluginRequest(id, "net.fetch", `{"method":"GET","url":"`+srv.URL+`/x"}`); err != nil {
		t.Fatalf("an allowed host must be reachable: %v", err)
	}
	if hits != 1 {
		t.Fatalf("the request did not reach the server: hits=%d", hits)
	}

	before := hits
	_, err := a.PluginRequest(id, "net.fetch", `{"method":"GET","url":"http://other.example/"}`)
	if err == nil {
		t.Fatal("a host outside the allow-list must be refused")
	}
	if !strings.Contains(err.Error(), "未声明的域名") {
		t.Fatalf("error %q must name the undeclared host", err)
	}
	if hits != before {
		t.Fatalf("a refused fetch still made a request: %d -> %d", before, hits)
	}

	_, err = a.PluginRequest(id, "net.fetch", `{"method":"GET","url":"file:///x"}`)
	if err == nil {
		t.Fatal("a non-http URL must be refused")
	}
	if hits != before {
		t.Fatalf("a refused scheme still made a request: %d -> %d", before, hits)
	}
}

// --- net.credentials: the host fills the cookie, the plugin never sees it -----

// credentialsTestManifest declares one secret and one plain item, so a test can
// show both halves of the rule: only the declared SECRET may be named, and its
// plaintext goes into the request instead of into the reply.
func credentialsTestManifest(hosts []string) map[string]any {
	return map[string]any{
		"permissions": []string{"net.fetch", "net.credentials", "plugin.config"},
		"netHosts":    hosts,
		"config": []map[string]any{
			{"key": "biliCookie", "label": "登录凭据", "type": "secret", "secret": true},
			{"key": "biliJct", "label": "CSRF 令牌", "type": "text"},
		},
	}
}

// TestNetFetchCredentialsRequirePermission is the consent half: without
// net.credentials the request is refused AND never sent.
//
// The secret is stored first on purpose. A refusal that could also be explained
// by "no credential saved" would prove nothing about the permission check.
func TestNetFetchCredentialsRequirePermission(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	a := pluginApp(t)
	man := credentialsTestManifest([]string{strings.TrimPrefix(srv.URL, "http://")})
	man["permissions"] = []string{"net.fetch", "plugin.config"} // deliberately no net.credentials
	id := installSampleWith(t, a, man)

	set, _ := json.Marshal(map[string]any{"values": map[string]string{"biliCookie": "SESSDATA=abc"}})
	if _, err := a.PluginRequest(id, "config.set", string(set)); err != nil {
		t.Fatalf("config.set: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"method": "GET", "url": srv.URL + "/x", "credentials": "biliCookie",
	})
	_, err := a.PluginRequest(id, "net.fetch", string(body))
	if err == nil {
		t.Fatal("carrying a credential without net.credentials must be refused")
	}
	if !strings.Contains(err.Error(), plugins.PermNetCredentials) {
		t.Fatalf("error %q must name the missing permission", err)
	}
	if hits != 0 {
		t.Fatalf("a refused credentialed fetch still reached the server: %d", hits)
	}
}

// TestNetFetchInjectsTheCredentialAndNeverEchoesIt is the whole point of the
// design: the value arrives at the far end, and nowhere else.
//
// Four assertions, because "it works" is the least interesting of them:
//  1. the server receives the stored secret as the Cookie header;
//  2. the reply does not contain it (a reply is what the sandboxed page reads);
//  3. config.get still answers {set, mask}, so the page cannot read it back;
//  4. a redirect to an undeclared host is refused and that host is never asked.
func TestNetFetchInjectsTheCredentialAndNeverEchoesIt(t *testing.T) {
	const secret = "SESSDATA=super-secret-value-9f2c; DedeUserID=42"

	var gotCookie, gotReferer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotReferer = r.Header.Get("Referer")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	a := pluginApp(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	id := installSampleWith(t, a, credentialsTestManifest([]string{host}))

	set, _ := json.Marshal(map[string]any{"values": map[string]string{
		"biliCookie": secret,
		"biliJct":    "jct-value",
	}})
	if _, err := a.PluginRequest(id, "config.set", string(set)); err != nil {
		t.Fatalf("config.set: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"method":      "GET",
		"url":         srv.URL + "/x",
		"credentials": "biliCookie",
		"referer":     "http://" + host + "/video/BV1xx411c7mD",
	})
	reply, err := a.PluginRequest(id, "net.fetch", string(body))
	if err != nil {
		t.Fatalf("net.fetch with a declared secret must work: %v", err)
	}
	if gotCookie != secret {
		t.Fatalf("the server saw Cookie %q, want the stored secret", gotCookie)
	}
	if want := "http://" + host + "/video/BV1xx411c7mD"; gotReferer != want {
		t.Fatalf("the server saw Referer %q, want %q", gotReferer, want)
	}
	if strings.Contains(reply, "super-secret-value") {
		t.Fatalf("the credential leaked into the reply: %s", reply)
	}

	cfg, err := a.PluginRequest(id, "config.get", "{}")
	if err != nil {
		t.Fatalf("config.get: %v", err)
	}
	if strings.Contains(cfg, "super-secret-value") {
		t.Fatalf("the credential is readable through config.get: %s", cfg)
	}
	if !strings.Contains(cfg, `"set":true`) {
		t.Fatalf("config.get should still report the secret as set: %s", cfg)
	}

	// A redirect to a host the manifest never declared is refused, and the
	// second server is never contacted - which is what keeps the injected Cookie
	// from travelling to whatever a listed host happens to point at.
	var secondHits int
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits++
	}))
	defer second.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL+"/x", http.StatusFound)
	}))
	defer redirector.Close()

	redMan := credentialsTestManifest([]string{strings.TrimPrefix(redirector.URL, "http://")})
	// A second plugin: one app cannot hold two rows with the same id.
	redMan["id"] = "com.example.test-redirect"
	redID := installSampleWith(t, a, redMan)
	redSet, _ := json.Marshal(map[string]any{"values": map[string]string{"biliCookie": secret}})
	if _, err := a.PluginRequest(redID, "config.set", string(redSet)); err != nil {
		t.Fatalf("config.set: %v", err)
	}
	redBody, _ := json.Marshal(map[string]any{
		"method": "GET", "url": redirector.URL + "/x", "credentials": "biliCookie",
	})
	if _, err := a.PluginRequest(redID, "net.fetch", string(redBody)); err == nil {
		t.Fatal("a redirect to an undeclared host must fail the request")
	}
	if secondHits != 0 {
		t.Fatalf("the redirect target was contacted %d times", secondHits)
	}
}

// TestNetFetchCredentialsMustNameADeclaredSecret covers the three keys that are
// not a usable credential: a plain text item, an undeclared key, and a declared
// secret nobody has saved yet.
func TestNetFetchCredentialsMustNameADeclaredSecret(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	a := pluginApp(t)
	id := installSampleWith(t, a, credentialsTestManifest([]string{strings.TrimPrefix(srv.URL, "http://")}))

	for name, key := range map[string]string{
		"a plain text item": "biliJct",
		"an undeclared key": "nope",
	} {
		body, _ := json.Marshal(map[string]any{"method": "GET", "url": srv.URL + "/x", "credentials": key})
		_, err := a.PluginRequest(id, "net.fetch", string(body))
		if err == nil {
			t.Fatalf("%s must not be usable as a credential", name)
		}
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("error %q must name the offending key %q", err, key)
		}
	}

	// Declared and secret, but empty: the message has to say "not saved" rather
	// than pretending the request worked.
	body, _ := json.Marshal(map[string]any{"method": "GET", "url": srv.URL + "/x", "credentials": "biliCookie"})
	_, err := a.PluginRequest(id, "net.fetch", string(body))
	if err == nil {
		t.Fatal("an unsaved credential must be refused")
	}
	if !strings.Contains(err.Error(), "还没有保存") {
		t.Fatalf("error %q must say the credential is not saved", err)
	}
	if hits != 0 {
		t.Fatalf("refused requests reached the server: %d", hits)
	}
}

// TestNetFetchRefererMustBeADeclaredHost covers the second header net.credentials
// unlocks: the Referer's host goes through the SAME allow-list as the URL,
// otherwise "only these hosts" would describe one field of the request instead of
// where the request leads.
func TestNetFetchRefererMustBeADeclaredHost(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	a := pluginApp(t)
	id := installSampleWith(t, a, credentialsTestManifest([]string{strings.TrimPrefix(srv.URL, "http://")}))

	for _, referer := range []string{"https://evil.example/video/BV1", "ftp://evil.example/x", "not a url"} {
		body, _ := json.Marshal(map[string]any{"method": "GET", "url": srv.URL + "/x", "referer": referer})
		_, err := a.PluginRequest(id, "net.fetch", string(body))
		if err == nil {
			t.Fatalf("referer %q must be refused", referer)
		}
	}
	if hits != 0 {
		t.Fatalf("a refused referer still made a request: %d", hits)
	}
}

// TestNetFetchReadsSetCookieOnlyWithPermission covers the second half of
// net.credentials: a session that arrives as Set-Cookie.
//
// It exists because B 站's QR login does exactly that - the poll answers "success" and
// hands the session over in headers, with nothing usable in the body. Four assertions:
// the pairs arrive, attributes are stripped, the flag is required, and the PERMISSION is
// checked before the request leaves (a caller that may not read cookies must not get to
// ask the far end for them).
func TestNetFetchReadsSetCookieOnlyWithPermission(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "abc%2Cdef", Path: "/", HttpOnly: true})
		http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: "the-jct", Path: "/"})
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	a := pluginApp(t)
	id := installSampleWith(t, a, credentialsTestManifest([]string{host}))

	withFlag := fmt.Sprintf(`{"method":"GET","url":%q,"readCookies":true}`, srv.URL+"/x")
	reply, err := a.PluginRequest(id, "net.fetch", withFlag)
	if err != nil {
		t.Fatalf("net.fetch with readCookies must work: %v", err)
	}
	if !strings.Contains(reply, "SESSDATA=abc%2Cdef") || !strings.Contains(reply, "bili_jct=the-jct") {
		t.Fatalf("the cookies did not come back: %s", reply)
	}
	for _, attribute := range []string{"HttpOnly", "Path=", "SameSite"} {
		if strings.Contains(reply, attribute) {
			t.Fatalf("cookie attribute %q must be dropped, got %s", attribute, reply)
		}
	}

	// Without the flag the reply must not carry cookies at all: the default has to be
	// "don't", or every net.fetch call quietly starts collecting them.
	plain, err := a.PluginRequest(id, "net.fetch", fmt.Sprintf(`{"method":"GET","url":%q}`, srv.URL+"/x"))
	if err != nil {
		t.Fatalf("net.fetch: %v", err)
	}
	if strings.Contains(plain, "SESSDATA") {
		t.Fatalf("cookies leaked without readCookies: %s", plain)
	}

	// A plugin holding only net.fetch: the request is refused BEFORE it is sent.
	man := credentialsTestManifest([]string{host})
	man["id"] = "com.example.test-nocookie"
	man["permissions"] = []string{"net.fetch", "plugin.config"}
	noPerm := installSampleWith(t, a, man)
	before := hits
	_, err = a.PluginRequest(noPerm, "net.fetch", withFlag)
	if err == nil {
		t.Fatal("reading Set-Cookie without net.credentials must be refused")
	}
	if !strings.Contains(err.Error(), plugins.PermNetCredentials) {
		t.Fatalf("error %q must name the missing permission", err)
	}
	if hits != before {
		t.Fatalf("a refused cookie-reading fetch still reached the server: %d -> %d", before, hits)
	}
}

// --- cross-plugin sharing ----------------------------------------------------

// TestShareCapabilities covers the whole authorisation model of `plugin.share` in one
// place, because the four rules only make sense together: write your own name, read what
// your manifest asked for, nothing else, and bounded.
func TestShareCapabilities(t *testing.T) {
	a := pluginApp(t)
	pub := installSampleWith(t, a, map[string]any{
		"id":          "com.example.pub",
		"permissions": []string{"plugin.share"},
	})

	const value = `{"id":"job-1","bvid":"BV1xx411c7mD"}`
	set, _ := json.Marshal(map[string]string{"name": "jobs", "value": value})
	if _, err := a.PluginRequest(pub, "share.set", string(set)); err != nil {
		t.Fatalf("share.set on your own name must work: %v", err)
	}
	// The reply is JSON with the value as a STRING field, so it is decoded rather than
	// substring-matched: a substring check would also pass on a truncated or re-escaped
	// copy, which is exactly the kind of "test that cannot fail".
	readShared := func(plugin, payload string) (string, int64, error) {
		raw, err := a.PluginRequest(plugin, "share.get", payload)
		if err != nil {
			return "", 0, err
		}
		var out struct {
			Value string `json:"value"`
			At    int64  `json:"at"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return "", 0, err
		}
		return out.Value, out.At, nil
	}

	// Own value, no declaration needed.
	own, _, err := readShared(pub, `{"name":"jobs"}`)
	if err != nil {
		t.Fatalf("share.get on your own value: %v", err)
	}
	if own != value {
		t.Fatalf("own value came back as %q", own)
	}

	// A plugin that did not declare the pair is refused, and the message names it.
	blind := installSampleWith(t, a, map[string]any{
		"id":          "com.example.blind",
		"permissions": []string{"plugin.share"},
	})
	_, _, err = readShared(blind, `{"owner":"com.example.pub","name":"jobs"}`)
	if err == nil {
		t.Fatal("reading another plugin's value without a shareRead entry must be refused")
	}
	if !strings.Contains(err.Error(), "shareRead") || !strings.Contains(err.Error(), "com.example.pub/jobs") {
		t.Fatalf("error %q must name the field and the pair", err)
	}

	// With the pair declared it works, and the reply carries the write time.
	sub := installSampleWith(t, a, map[string]any{
		"id":          "com.example.sub",
		"permissions": []string{"plugin.share"},
		"shareRead":   []string{"com.example.pub/jobs"},
	})
	shared, at, err := readShared(sub, `{"owner":"com.example.pub","name":"jobs"}`)
	if err != nil {
		t.Fatalf("a declared pair must be readable: %v", err)
	}
	if shared != value {
		t.Fatalf("shared value came back as %q", shared)
	}
	if at <= 0 {
		t.Fatalf("the write time is missing, so a consumer cannot ignore stale work")
	}

	// Declaring the owner is not declaring every name under it.
	if _, _, err := readShared(sub, `{"owner":"com.example.pub","name":"other"}`); err == nil {
		t.Fatal("a name that was not declared must be refused")
	}

	// An unpublished value reads as empty, not as an error (own name, never written).
	if empty, _, err := readShared(pub, `{"name":"never"}`); err != nil || empty != "" {
		t.Fatalf("a missing value must read as empty: value=%q err=%v", empty, err)
	}

	// Oversized values are refused.
	big, _ := json.Marshal(map[string]string{"name": "big", "value": strings.Repeat("x", 70<<10)})
	if _, err := a.PluginRequest(pub, "share.set", string(big)); err == nil {
		t.Fatal("a value over 64 KiB must be refused")
	}

	// Removing your own value works and clears it.
	if _, err := a.PluginRequest(pub, "share.remove", `{"name":"jobs"}`); err != nil {
		t.Fatalf("share.remove: %v", err)
	}
	if after, _, err := readShared(pub, `{"name":"jobs"}`); err != nil || after != "" {
		t.Fatalf("the value survived removal: value=%q err=%v", after, err)
	}
}

// --- process.state / storage.dir --------------------------------------------

// TestProcessStateTracksExit covers the pair that makes "run a downloader and send the
// result" possible without a filesystem capability: launch, then ask whether it finished.
//
// The exit code matters as much as the flag - a downloader that failed is not the same as
// one that produced nothing, and the plugin has to word its message differently.
func TestProcessStateTracksExit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe is the only program guaranteed to exist on this platform")
	}
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"process"}})
	cmdPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if _, err := os.Stat(cmdPath); err != nil {
		t.Skipf("cmd.exe not found: %v", err)
	}

	launch, _ := json.Marshal(map[string]any{"path": cmdPath, "args": []string{"/c", "exit", "3"}})
	raw, err := a.PluginRequest(id, "process.launch", string(launch))
	if err != nil {
		t.Fatalf("process.launch: %v", err)
	}
	var started struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal([]byte(raw), &started); err != nil || started.PID <= 0 {
		t.Fatalf("no pid came back: %s", raw)
	}

	poll := func(plugin string) (known, running bool, code int) {
		for attempt := 0; attempt < 60; attempt++ {
			reply, err := a.PluginRequest(plugin, "process.state", fmt.Sprintf(`{"pid":%d}`, started.PID))
			if err != nil {
				t.Fatalf("process.state: %v", err)
			}
			var state struct {
				Known    bool `json:"known"`
				Running  bool `json:"running"`
				ExitCode int  `json:"exitCode"`
			}
			if err := json.Unmarshal([]byte(reply), &state); err != nil {
				t.Fatalf("bad state reply: %s", reply)
			}
			if state.Known && !state.Running {
				return state.Known, state.Running, state.ExitCode
			}
			time.Sleep(50 * time.Millisecond)
		}
		return true, true, -1
	}

	known, running, code := poll(id)
	if !known || running {
		t.Fatalf("the child never reported as finished (known=%v running=%v)", known, running)
	}
	if code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}

	// A pid is guessable; another plugin must not learn anything about it.
	intruder := installSampleWith(t, a, map[string]any{
		"id":          "com.example.intruder",
		"permissions": []string{"process"},
	})
	reply, err := a.PluginRequest(intruder, "process.state", fmt.Sprintf(`{"pid":%d}`, started.PID))
	if err != nil {
		t.Fatalf("process.state (intruder): %v", err)
	}
	if !strings.Contains(reply, `"known":false`) {
		t.Fatalf("another plugin could see this child: %s", reply)
	}
}

// TestStorageDirIsTheCallersOwn covers the path a plugin hands to a program it starts.
// TestStorageStatTellsTheCallerHowBigItsOwnFilesAre covers the method that exists because
// a clock cannot answer "did the download finish?" - a 60 MB merge takes four minutes on
// one connection and forty seconds on another, but its size is comparable to the estimate
// the playurl response already gave.
func TestStorageStatTellsTheCallerHowBigItsOwnFilesAre(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"storage"}})

	if _, err := a.PluginRequest(id, "storage.set", `{"key":"clip","value":"hello"}`); err != nil {
		t.Fatalf("storage.set: %v", err)
	}

	stat := func(key string) (exists bool, bytes int64, err error) {
		raw, err := a.PluginRequest(id, "storage.stat", `{"key":`+quoteJSON(key)+`}`)
		if err != nil {
			return false, 0, err
		}
		var out struct {
			Exists bool  `json:"exists"`
			Bytes  int64 `json:"bytes"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("bad reply %s: %v", raw, err)
		}
		return out.Exists, out.Bytes, nil
	}

	exists, bytes, err := stat("clip")
	if err != nil {
		t.Fatalf("storage.stat: %v", err)
	}
	if !exists || bytes != 5 {
		t.Fatalf("clip = exists %v, %d bytes; want true, 5", exists, bytes)
	}

	// A key that is not there yet is a normal answer, not an error: that is what a
	// download in progress looks like.
	exists, bytes, err = stat("notyet")
	if err != nil || exists || bytes != 0 {
		t.Fatalf("missing key = %v, %d, %v; want false, 0, nil", exists, bytes, err)
	}

	// Same key validation as every other storage call, so this cannot reach outside.
	for _, bad := range []string{"../secret", "with.dot", "", "/abs", "a//b"} {
		if _, _, err := stat(bad); err == nil {
			t.Fatalf("key %q must be refused", bad)
		}
	}
}

func TestStorageDirIsTheCallersOwn(t *testing.T) {
	a := pluginApp(t)
	first := installSampleWith(t, a, map[string]any{"permissions": []string{"storage"}})
	second := installSampleWith(t, a, map[string]any{
		"id":          "com.example.other",
		"permissions": []string{"storage"},
	})

	readDir := func(plugin string) string {
		raw, err := a.PluginRequest(plugin, "storage.dir", "{}")
		if err != nil {
			t.Fatalf("storage.dir: %v", err)
		}
		var out struct {
			Dir string `json:"dir"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil || out.Dir == "" {
			t.Fatalf("no dir came back: %s", raw)
		}
		return out.Dir
	}
	mine := readDir(first)
	theirs := readDir(second)
	if !filepath.IsAbs(mine) {
		t.Fatalf("the directory must be absolute: %q", mine)
	}
	if info, err := os.Stat(mine); err != nil || !info.IsDir() {
		t.Fatalf("the directory does not exist: %v", err)
	}
	if mine == theirs {
		t.Fatalf("two plugins got the same directory: %s", mine)
	}
	if !strings.Contains(mine, first) {
		t.Fatalf("the directory %q does not name the plugin %q", mine, first)
	}
}

// TestStorageDirCanNameAFolderInsideThePrivateDir covers the reason `sub` exists.
//
// A program the plugin STARTS (curl, ffmpeg) can only be pointed at a PATH, so the
// sorted-into-folders layout is impossible for it without this: the plugin's own
// writes can name `video/`, but it cannot tell curl to write there unless the host
// both resolves and creates that folder. Refusing a `sub` that climbs out is the
// same rule storage keys follow, for the same reason.
func TestStorageDirCanNameAFolderInsideThePrivateDir(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"storage"}})
	private, _ := a.pluginManager().PrivateDir(id)

	readDir := func(payload string) (string, error) {
		raw, err := a.PluginRequest(id, "storage.dir", payload)
		if err != nil {
			return "", err
		}
		var out struct {
			Dir string `json:"dir"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("bad reply %s: %v", raw, err)
		}
		return out.Dir, nil
	}

	dir, err := readDir(`{"sub":"video/tmp"}`)
	if err != nil {
		t.Fatalf("storage.dir with sub: %v", err)
	}
	want := filepath.Join(private, "video", "tmp")
	if filepath.Clean(dir) != filepath.Clean(want) {
		t.Fatalf("sub gave %q, want %q", dir, want)
	}
	// Created on demand: the caller is about to hand this path to another program,
	// which will not create it.
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("the sub folder does not exist: %v", err)
	}

	for _, sub := range []string{"../outside", "..", "/abs", "a/b/c/d/e", "with.dot"} {
		payload, _ := json.Marshal(map[string]string{"sub": sub})
		if _, err := readDir(string(payload)); err == nil {
			t.Fatalf("sub %q must be refused", sub)
		}
	}
}

// TestStorageMoveRenamesOwnFiles covers the method that made the QQ plugin's
// reorganisation possible without pumping 300 MB through the bridge as base64.
//
// The three outcomes are the contract, and each one exists because a MIGRATION calls
// this function on every start: missing source = nothing to do, occupied target =
// already moved (and the source is left alone, because "rename over a file" is data
// loss), otherwise a real rename.
func TestStorageMoveRenamesOwnFiles(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"storage"}})
	private, _ := a.pluginManager().PrivateDir(id)

	move := func(from, to string) (moved, targetExists bool, err error) {
		payload, _ := json.Marshal(map[string]string{"from": from, "to": to})
		raw, err := a.PluginRequest(id, "storage.move", string(payload))
		if err != nil {
			return false, false, err
		}
		var out struct {
			Moved        bool `json:"moved"`
			TargetExists bool `json:"targetExists"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("bad reply %s: %v", raw, err)
		}
		return out.Moved, out.TargetExists, nil
	}

	if _, err := a.PluginRequest(id, "storage.set", `{"key":"hist_10086","value":"[]"}`); err != nil {
		t.Fatalf("storage.set: %v", err)
	}
	moved, _, err := move("hist_10086", "history/10086")
	if err != nil {
		t.Fatalf("storage.move: %v", err)
	}
	if !moved {
		t.Fatal("the file was not moved")
	}
	if _, err := os.Stat(filepath.Join(private, "hist_10086")); !os.IsNotExist(err) {
		t.Fatalf("the source survived the move: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(private, "history", "10086")); err != nil || string(data) != "[]" {
		t.Fatalf("the target is not where the key says: %v (%q)", err, string(data))
	}

	// Second run of the same migration: nothing left to move, and that is not an error.
	moved, _, err = move("hist_10086", "history/10086")
	if err != nil || moved {
		t.Fatalf("moving an absent source = %v, %v; want false, nil", moved, err)
	}

	// An occupied target is reported, and the source is NOT destroyed.
	if _, err := a.PluginRequest(id, "storage.set", `{"key":"log","value":"new"}`); err != nil {
		t.Fatalf("storage.set: %v", err)
	}
	moved, target, err := move("log", "history/10086")
	if err != nil {
		t.Fatalf("storage.move onto a target: %v", err)
	}
	if moved || !target {
		t.Fatalf("occupied target = moved %v, targetExists %v; want false, true", moved, target)
	}
	if data, err := os.ReadFile(filepath.Join(private, "log")); err != nil || string(data) != "new" {
		t.Fatalf("the source was overwritten or deleted: %v (%q)", err, string(data))
	}
	if data, err := os.ReadFile(filepath.Join(private, "history", "10086")); err != nil || string(data) != "[]" {
		t.Fatalf("the target was overwritten: %v (%q)", err, string(data))
	}

	// Both ends obey the key rules, so a move cannot be the way out of the directory.
	for _, pair := range [][2]string{{"../escape", "x"}, {"log", "../escape"}, {"log", "a//b"}} {
		if _, _, err := move(pair[0], pair[1]); err == nil {
			t.Fatalf("move %q -> %q must be refused", pair[0], pair[1])
		}
	}
}

// TestShareValuesLiveInTheirOwnFolder covers the layout change for the cross-plugin
// tray: the host's bookkeeping files were sitting in the plugin's data root next to
// its own data.
//
// The load-bearing assertion is the LEGACY read: a value published before the folder
// existed must still be readable, and must end up in the folder afterwards, because
// an upgrade in the middle of a job list is not a user-visible event.
func TestShareValuesLiveInTheirOwnFolder(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"plugin.share"}})
	private, _ := a.pluginManager().PrivateDir(id)

	// Written by an older version: a file in the root named share-<name>.
	if err := os.WriteFile(filepath.Join(private, "share-bili-jobs"), []byte(`[{"id":"old"}]`), 0o644); err != nil {
		t.Fatalf("seed legacy value: %v", err)
	}
	raw, err := a.PluginRequest(id, "share.get", `{"name":"bili-jobs"}`)
	if err != nil {
		t.Fatalf("share.get: %v", err)
	}
	if !strings.Contains(raw, `old`) {
		t.Fatalf("the legacy value was not read: %s", raw)
	}
	if _, err := os.Stat(filepath.Join(private, "share", "bili-jobs")); err != nil {
		t.Fatalf("the legacy value was not moved into share/: %v", err)
	}

	// A fresh write lands in the folder, and the legacy file is not left behind.
	if _, err := a.PluginRequest(id, "share.set", `{"name":"bili-results","value":"[]"}`); err != nil {
		t.Fatalf("share.set: %v", err)
	}
	if _, err := os.Stat(filepath.Join(private, "share", "bili-results")); err != nil {
		t.Fatalf("share.set did not write share/bili-results: %v", err)
	}

	// Removing a value removes BOTH copies: a stale root file would keep reading back.
	if _, err := a.PluginRequest(id, "share.remove", `{"name":"bili-jobs"}`); err != nil {
		t.Fatalf("share.remove: %v", err)
	}
	for _, name := range []string{filepath.Join("share", "bili-jobs"), "share-bili-jobs"} {
		if _, err := os.Stat(filepath.Join(private, name)); !os.IsNotExist(err) {
			t.Fatalf("%s survived removal: %v", name, err)
		}
	}
}

// --- T43b/T43c: the plugin's own declared settings -------------------------

// configTestManifest is the override map that gives a test plugin one of every
// control type, plus the secret and the host-whitelist item.
func configTestManifest() map[string]any {
	return map[string]any{
		"permissions": []string{"plugin.config"},
		"config": []map[string]any{
			{"key": "llmBaseUrl", "label": "接口地址", "type": "text", "hostWhitelist": true},
			{"key": "llmModel", "label": "模型", "type": "text", "default": "deepseek-chat"},
			{"key": "llmMaxTokens", "label": "回复上限", "type": "number"},
			{"key": "llmContextTodos", "label": "知道待办", "type": "bool"},
			{"key": "llmApiKey", "label": "API Key", "type": "secret", "secret": true},
		},
	}
}

// TestConfigCapabilities covers T43b and T43c together, because they are the two
// halves of one rule: the plugin may manage ITS OWN declared settings and may
// read back everything except the plaintext of a secret.
//
// The load-bearing assertion is that the plaintext never appears in the
// config.get REPLY. A plugin holds its own key in order to store it; being able
// to read it back out would turn "the user configured this plugin" into "the
// plugin can exfiltrate the user's credential", which is the entire reason the
// secret lives on the host side and is masked on the way out.
func TestConfigCapabilities(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, configTestManifest())

	const plaintext = "sk-super-secret-value-9f2c"
	payload, _ := json.Marshal(map[string]any{"values": map[string]string{
		"llmBaseUrl":      "https://api.deepseek.com",
		"llmModel":        "deepseek-chat",
		"llmMaxTokens":    "512",
		"llmContextTodos": "true",
		"llmApiKey":       plaintext,
	}})
	if _, err := a.PluginRequest(id, "config.set", string(payload)); err != nil {
		t.Fatalf("config.set: %v", err)
	}

	// The non-secret values are in the plugin's OWN config.json...
	private, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(private, "config.json"))
	if err != nil {
		t.Fatalf("config.json: %v", err)
	}
	if !strings.Contains(string(raw), "api.deepseek.com") {
		t.Fatalf("a declared value was not stored: %s", raw)
	}
	// ...and the secret is NOT, which is the one outcome this whole path exists
	// to prevent.
	if strings.Contains(string(raw), plaintext) {
		t.Fatalf("the plaintext secret reached config.json: %s", raw)
	}

	out, err := a.PluginRequest(id, "config.get", "")
	if err != nil {
		t.Fatalf("config.get: %v", err)
	}
	if strings.Contains(out, plaintext) {
		t.Fatalf("config.get leaked the plaintext secret: %s", out)
	}
	var got plugins.ConfigValues
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v (raw %s)", err, out)
	}
	if got.Values["llmBaseUrl"] != "https://api.deepseek.com" {
		t.Errorf("llmBaseUrl = %q", got.Values["llmBaseUrl"])
	}
	if got.Values["llmModel"] != "deepseek-chat" {
		t.Errorf("llmModel = %q", got.Values["llmModel"])
	}
	// A bool is normalised, so a plugin reading back what it wrote sees the
	// canonical spelling rather than whatever it happened to send.
	if got.Values["llmContextTodos"] != "true" {
		t.Errorf("llmContextTodos = %q, want the normalised \"true\"", got.Values["llmContextTodos"])
	}
	// The secret is reported as PRESENT and masked, and nothing more.
	state, ok := got.Secrets["llmApiKey"]
	if !ok || !state.Set {
		t.Fatalf("the secret must be reported as set: %+v", got.Secrets)
	}
	if state.Mask == "" || state.Mask == plaintext {
		t.Fatalf("the mask must obscure the secret, got %q", state.Mask)
	}
	if _, leaked := got.Values["llmApiKey"]; leaked {
		t.Error("the secret must not also appear among the plain values")
	}
}

// TestConfigSetUndeclaredKeyChangesNothing covers T37/T43b: a setting the
// manifest does not describe is refused, and the refusal is all-or-nothing -
// the declared key that travelled with it is not written either.
func TestConfigSetUndeclaredKeyChangesNothing(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, configTestManifest())

	// Establish a known-good value first.
	seed, _ := json.Marshal(map[string]any{"values": map[string]string{"llmModel": "deepseek-chat"}})
	if _, err := a.PluginRequest(id, "config.set", string(seed)); err != nil {
		t.Fatalf("config.set: %v", err)
	}
	private, _ := a.pluginManager().PrivateDir(id)
	before, err := os.ReadFile(filepath.Join(private, "config.json"))
	if err != nil {
		t.Fatalf("config.json: %v", err)
	}

	// One undeclared key poisons the whole payload.
	bad, _ := json.Marshal(map[string]any{"values": map[string]string{
		"llmModel":      "changed",
		"notDeclaredAt": "whatever",
	}})
	if _, err := a.PluginRequest(id, "config.set", string(bad)); err == nil {
		t.Fatal("an undeclared key must be refused")
	} else if !strings.Contains(err.Error(), "未声明配置项") {
		t.Fatalf("error %q must name the undeclared item", err.Error())
	}

	after, err := os.ReadFile(filepath.Join(private, "config.json"))
	if err != nil {
		t.Fatalf("config.json: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("a refused write changed the file:\n%s\n->\n%s", before, after)
	}
}

// TestConfigSetWithoutPermissionLeavesNoTrace covers the "state changes nothing"
// half of T43b at the FILE level rather than the error-message level: a plugin
// that was never granted plugin.config cannot write a setting, and the check
// happens before the payload is parsed, so not even a malformed body matters.
func TestConfigSetWithoutPermissionLeavesNoTrace(t *testing.T) {
	a := pluginApp(t)
	// Enabled, but gateway-less: nothing is granted.
	id := installTestPlugin(t, a, configTestManifest())
	managerSetEnabled(t, a, id, true)
	private, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(private, "config.json"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("config.json: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"values": map[string]string{"llmModel": "sneaky"}})
	for _, method := range []string{"config.set", "config.get"} {
		_, err := a.PluginRequest(id, method, string(body))
		if err == nil {
			t.Fatalf("%s must be refused without plugin.config", method)
		}
		if !strings.Contains(err.Error(), plugins.PermPluginConfig) {
			t.Fatalf("%s: error %q must name %s", method, err.Error(), plugins.PermPluginConfig)
		}
	}

	after, err := os.ReadFile(filepath.Join(private, "config.json"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("config.json: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("a refused config call wrote the file: %q -> %q", before, after)
	}
}

// --- T45/T46: net.fetch ----------------------------------------------------

// TestNetFetchConfiguredWhitelist covers T45: the allow-list is the manifest's
// static netHosts PLUS the hosts parsed out of the declared host-whitelist
// setting, and a method outside GET/POST is still refused.
func TestNetFetchConfiguredWhitelist(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("pong"))
	}))
	defer srv.Close()

	man := configTestManifest()
	man["netHosts"] = []string{"static.example.com"}
	man["permissions"] = []string{"net.fetch", "plugin.config"}

	a := pluginApp(t)
	id := installSampleWith(t, a, man)

	// The dynamic host comes from the SETTING, not from the manifest, so the
	// request only succeeds once the configured value is in place.
	if _, err := a.PluginRequest(id, "net.fetch", `{"method":"GET","url":"`+srv.URL+`/x"}`); err == nil {
		t.Fatal("a host that is not on any allow-list must be refused")
	}
	if hits != 0 {
		t.Fatalf("a refused fetch reached the server: %d", hits)
	}

	dynamic := strings.TrimPrefix(srv.URL, "http://")
	set, _ := json.Marshal(map[string]any{"values": map[string]string{
		"llmBaseUrl": srv.URL + "/v1",
	}})
	if _, err := a.PluginRequest(id, "config.set", string(set)); err != nil {
		t.Fatalf("config.set: %v", err)
	}

	if _, err := a.PluginRequest(id, "net.fetch", `{"method":"GET","url":"`+srv.URL+`/x"}`); err != nil {
		t.Fatalf("the configured host must be reachable: %v", err)
	}
	if hits != 1 {
		t.Fatalf("the configured host did not reach the server: hits=%d (host %s)", hits, dynamic)
	}

	// The static list keeps working alongside the configured one.
	before := hits
	if _, err := a.PluginRequest(id, "net.fetch", `{"method":"PUT","url":"`+srv.URL+`/x"}`); err == nil {
		t.Fatal("a method other than GET/POST must be refused")
	}
	if hits != before {
		t.Fatalf("a refused method still made a request: %d -> %d", before, hits)
	}
}

// TestConfigWhitelistCannotWidenArbitraryHosts covers T46: the configured value
// narrows what may be reached; it can never become a wildcard.
//
// The counter is the point. Refusing AFTER the request would still let a plugin
// use a rejected host as a probe for what is listening on the machine.
func TestConfigWhitelistCannotWidenArbitraryHosts(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	man := configTestManifest()
	man["permissions"] = []string{"net.fetch", "plugin.config"}
	// The manifest must name at least one static host to declare net.fetch at
	// all; that static list is the thing the setting is NOT allowed to widen.
	man["netHosts"] = []string{"static.example.com"}

	a := pluginApp(t)
	id := installSampleWith(t, a, man)

	for _, value := range []string{"", "ftp://evil.example.com/x", "*.example.com", "evil.example.com, *.example.com"} {
		set, _ := json.Marshal(map[string]any{"values": map[string]string{"llmBaseUrl": value}})
		if _, err := a.PluginRequest(id, "config.set", string(set)); err != nil {
			t.Fatalf("config.set(%q): %v", value, err)
		}
		for _, target := range []string{"https://evil.example.com/x", "https://sub.example.com/x"} {
			body, _ := json.Marshal(map[string]string{"method": "GET", "url": target})
			if _, err := a.PluginRequest(id, "net.fetch", string(body)); err == nil {
				t.Fatalf("with %q configured, %s must still be refused", value, target)
			}
		}
	}
	if hits != 0 {
		t.Fatalf("a refused fetch reached a server: %d requests", hits)
	}
}

// --- T47/T48/T50b: events ---------------------------------------------------

// TestEventsSubscribeAndEmit covers T47 and the Go half of T50b: a subscription is
// recorded, and a state change reaches the subscriber as ONE pet:plugin-event carrying
// the right plugin and event name.
//
// The trigger used to be `todos.add` (the todo bindings emitted `todos.changed`). Both
// that event and those bindings are gone - the list is the todo plugin's own storage, so
// the host has nothing to broadcast about it. What the host still emits by itself is
// `reminder.fired` (the manual RemindNow), and the mechanism it exercises - subscription
// set → one frame per subscriber - is the same one, so the trigger is now internal.
func TestEventsSubscribeAndEmit(t *testing.T) {
	a := pluginApp(t)
	collected := collectPluginEvents(a)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"events"}})

	out, err := a.PluginRequest(id, "events.subscribe", `{"names":["reminder.fired","timer.phaseEnded"]}`)
	if err != nil {
		t.Fatalf("events.subscribe: %v", err)
	}
	var names []string
	if err := json.Unmarshal([]byte(out), &names); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("the subscription set is %v, want both events", names)
	}

	a.notifyPluginReminderFired(1, 0, 2)
	events := collected("reminder.fired")
	if len(events) != 1 {
		t.Fatalf("want exactly 1 reminder.fired event, got %d (%+v)", len(events), events)
	}
	if events[0].PluginID != id {
		t.Fatalf("the event names %q, want %q", events[0].PluginID, id)
	}

	// And the edge event the host keeps for compatibility still travels: it has no
	// emitter left (see notifyPluginPhaseEnded), but the transition the voice plugin
	// depends on must keep working the day one comes back.
	a.notifyPluginPhaseEnded("focus")
	if got := collected("timer.phaseEnded"); len(got) != 1 {
		t.Fatalf("want 1 timer.phaseEnded, got %d", len(got))
	}
}

// TestEventsRejectUnknownName covers T48: an unknown name is refused outright
// and adds no subscription.
func TestEventsRejectUnknownName(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"events"}})
	if _, err := a.PluginRequest(id, "events.subscribe", `{"names":["reminder.fired"]}`); err != nil {
		t.Fatalf("events.subscribe: %v", err)
	}

	// `todos.changed` is a name that USED to be valid and is now exactly as unknown as
	// any typo - which is what makes it a useful negative: a retired event must not linger
	// in the list just because it once worked.
	_, err := a.PluginRequest(id, "events.subscribe", `{"names":["reminder.fired","todos.changed"]}`)
	if err == nil {
		t.Fatal("a retired event name must be refused")
	}
	if !strings.Contains(err.Error(), "todos.changed") {
		t.Fatalf("error %q must name the bad event", err)
	}

	a.pluginHost.mu.Lock()
	set := a.pluginHost.subs[id]
	a.pluginHost.mu.Unlock()
	if len(set) != 1 || !set["reminder.fired"] {
		t.Fatalf("a refused subscribe changed the set: %v", set)
	}
}
// --- T72–T77: llm.chat ------------------------------------------------------

// ssePieces is the reply the stub endpoint streams, in the order it sends it.
//
// It is deliberately longer than the client's batching threshold: the streaming
// path coalesces token-sized deltas before they cross the bridge, so a
// two-token reply legitimately arrives as ONE frame. Asserting "at least two
// frames" against a two-token reply would be asserting against the batcher rather
// than against the streaming path, and would fail for the right reason.
var ssePieces = []string{"你好", "呀，", "这是", "一段", "用来", "测试的", "流式", "回复"}

// sseFullReply is the text those pieces add up to.
const sseFullReply = "你好呀，这是一段用来测试的流式回复"

// chatStub is a stand-in model endpoint.
//
// It COUNTS the requests it receives and remembers the last one, because the
// load-bearing assertion of T72 is about a request that must not happen: "the
// pet's own endpoint was never contacted" is only a fact if something was
// counting, and an endpoint that merely answers would let a regression pass.
type chatStub struct {
	srv *httptest.Server

	mu   sync.Mutex
	hits int
	last map[string]any
}

// newChatStub starts the endpoint; it is closed with the test.
func newChatStub(t *testing.T) *chatStub {
	t.Helper()
	st := &chatStub{}
	st.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		st.mu.Lock()
		st.hits++
		st.last = req
		st.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		write := func(s string) {
			_, _ = w.Write([]byte(s))
			if flusher != nil {
				flusher.Flush()
			}
		}
		for _, piece := range ssePieces {
			body, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{"delta": map[string]any{"content": piece}}},
			})
			write("data: " + string(body) + "\n\n")
		}
		write("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n")
		write("data: [DONE]\n\n")
	}))
	t.Cleanup(st.srv.Close)
	return st
}

// URL is the endpoint root a plugin's llmBaseUrl is pointed at.
func (s *chatStub) URL() string { return s.srv.URL }

// Hits is how many requests the endpoint has received.
func (s *chatStub) Hits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

// LastRequest is the last decoded request body, nil when nothing arrived.
func (s *chatStub) LastRequest() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// RequestModel is the `model` field of the last request body.
func (s *chatStub) RequestModel() string {
	m, _ := s.LastRequest()["model"].(string)
	return m
}

// chatTestManifest turns the generic test plugin into a conversation plugin: it
// declares the capabilities the llm.chat path needs and the settings that path
// reads.
//
// The provider default is Ollama, the preset that does NOT require an API key:
// the credential check is a case of its own (T73) and the point of the streaming
// tests is the streaming path. A local endpoint is also what a plugin author
// testing against a local model would use.
func chatTestManifest() map[string]any {
	return map[string]any{
		"permissions": []string{"llm.chat", "plugin.config", "storage"},
		"config": []map[string]any{
			{"key": "llmProvider", "label": "服务商预设", "type": "text", "default": "ollama"},
			{"key": "llmBaseUrl", "label": "接口地址", "type": "text", "hostWhitelist": true},
			{"key": "llmModel", "label": "模型", "type": "text"},
			{"key": "llmMaxTokens", "label": "回复上限", "type": "number", "default": "800"},
			{"key": "llmTimeoutSec", "label": "超时（秒）", "type": "number", "default": "10"},
			{"key": "llmDailyBudget", "label": "每日上限（次）", "type": "number", "default": "200"},
			{"key": "llmApiKey", "label": "API Key", "type": "secret", "secret": true},
		},
	}
}

// setPluginConfig writes the plugin's OWN settings through the same capability a
// real plugin uses, so the test exercises the storage the request path reads
// rather than a shortcut into it.
func setPluginConfig(t *testing.T, a *App, id string, values map[string]string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"values": values})
	if _, err := a.PluginRequest(id, "config.set", string(payload)); err != nil {
		t.Fatalf("config.set: %v", err)
	}
}

// pluginUsageOnDisk reads the host-owned usage counter for one plugin.
//
// It reads the FILE rather than a view: the counter is the host's private
// bookkeeping for a plugin, so a test that went through an API would be
// asserting on a projection of the thing under test.
func pluginUsageOnDisk(t *testing.T, a *App, id string) store.LLMUsage {
	t.Helper()
	dir, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "llm-usage"))
	if err != nil {
		t.Fatalf("the host did not record usage: %v", err)
	}
	var u store.LLMUsage
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatalf("llm-usage is not a usage record: %v (%s)", err, raw)
	}
	return u
}

// TestLLMChatUsesThePluginsOwnEndpoint covers T72: every model parameter comes
// from the CALLING PLUGIN's own settings, never from the pet's.
//
// The pet's config.json is pointed at a second stub on purpose. The pet's llm*
// values survive as a one-time migration source, and the failure this test
// exists to catch is exactly the plausible-looking shortcut of reading them
// again: it would pass every single-endpoint test and silently send a plugin's
// traffic to the pet's account.
func TestLLMChatUsesThePluginsOwnEndpoint(t *testing.T) {
	pluginEndpoint := newChatStub(t)
	petEndpoint := newChatStub(t)
	a := pluginApp(t)
	id := installSampleWith(t, a, chatTestManifest())

	setPluginConfig(t, a, id, map[string]string{
		"llmBaseUrl": pluginEndpoint.URL(),
		"llmModel":   "plugin-model",
	})
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.LLMEnabled = true
		c.LLMProvider = "ollama"
		c.LLMBaseURL = petEndpoint.URL()
		c.LLMModel = "pet-model"
		c.LLMTimeoutSec = 10
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"在吗"}`); err != nil {
		t.Fatalf("llm.chat: %v", err)
	}
	if got := pluginEndpoint.Hits(); got != 1 {
		t.Fatalf("the plugin's endpoint received %d requests, want 1", got)
	}
	if got := petEndpoint.Hits(); got != 0 {
		t.Fatalf("the PET's endpoint received %d requests, want 0: the request used the wrong configuration", got)
	}
	if got := pluginEndpoint.RequestModel(); got != "plugin-model" {
		t.Fatalf("the request carried model %q, want the plugin's own %q", got, "plugin-model")
	}
}

// TestLLMChatRefusesBeforeAnyRequest covers T73: the two "not configured" states
// are reported separately, and neither of them reaches the network.
//
// The separation is the point. A user who has not typed an address and a user
// who has not pasted a key need different actions, and one shared message would
// leave them guessing which of the two the pet means.
func TestLLMChatRefusesBeforeAnyRequest(t *testing.T) {
	endpoint := newChatStub(t)
	a := pluginApp(t)
	id := installSampleWith(t, a, chatTestManifest())

	// 1. No endpoint and no model: the plugin's manifest declares no default for
	//    either, so a freshly installed plugin is in this state.
	_, err := a.PluginRequest(id, "llm.chat", `{"text":"在吗"}`)
	if err == nil {
		t.Fatal("a plugin with no endpoint and no model must be refused")
	}
	if !strings.Contains(err.Error(), "还没配置端点与模型") {
		t.Fatalf("the refusal does not name the missing endpoint: %v", err)
	}

	// 2. Endpoint and model present, credential absent, provider needs one.
	setPluginConfig(t, a, id, map[string]string{
		"llmProvider": "deepseek",
		"llmBaseUrl":  endpoint.URL(),
		"llmModel":    "deepseek-chat",
	})
	_, err = a.PluginRequest(id, "llm.chat", `{"text":"在吗"}`)
	if err == nil {
		t.Fatal("a plugin with no API Key must be refused")
	}
	if !strings.Contains(err.Error(), "还没有填写 API Key") {
		t.Fatalf("the refusal does not name the missing key: %v", err)
	}

	// Neither refusal may have cost anything: an unconfigured plugin must be
	// free to ask, because the plugin asks again the moment the user saves the
	// form, and a counter that moved would let a typo burn the day's budget.
	if got := endpoint.Hits(); got != 0 {
		t.Fatalf("a refused call reached the network %d times", got)
	}
	dir, derr := a.pluginManager().PrivateDir(id)
	if derr != nil {
		t.Fatalf("PrivateDir: %v", derr)
	}
	if _, serr := os.Stat(filepath.Join(dir, "llm-usage")); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("a refusal before any attempt was counted as one: %v", serr)
	}
}

// TestLLMChatBudgetIsEnforcedByTheHost covers T74: the daily cap is read from
// the plugin's settings, enforced by the host, and the counter lives in the
// plugin's private directory.
//
// The budget bounds ATTEMPTS, not successes, and the refusal is counted too:
// otherwise a caller sitting exactly on the boundary could ask forever for free,
// which is the whole reason a cap exists.
func TestLLMChatBudgetIsEnforcedByTheHost(t *testing.T) {
	endpoint := newChatStub(t)
	a := pluginApp(t)
	id := installSampleWith(t, a, chatTestManifest())
	setPluginConfig(t, a, id, map[string]string{
		"llmBaseUrl":     endpoint.URL(),
		"llmModel":       "test-model",
		"llmDailyBudget": "1",
	})

	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"第一次"}`); err != nil {
		t.Fatalf("the first call is within budget and must be allowed: %v", err)
	}
	_, err := a.PluginRequest(id, "llm.chat", `{"text":"第二次"}`)
	if err == nil {
		t.Fatal("the second call is over budget and must be refused")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Fatalf("the refusal does not mention the cap: %v", err)
	}
	if got := endpoint.Hits(); got != 1 {
		t.Fatalf("the endpoint received %d requests, want 1: the refused call was sent anyway", got)
	}

	u := pluginUsageOnDisk(t, a, id)
	if u.Requests != 2 {
		t.Fatalf("requests = %d, want 2: the refused attempt must be counted too", u.Requests)
	}
	if want := store.UsageDate(time.Now()); u.Date != want {
		t.Fatalf("date = %q, want %q", u.Date, want)
	}
}

// TestLLMChatWritesNoTranscript covers T76: the transcript belongs to the
// plugin, and the host never writes one.
//
// Two writers for one conversation is the failure this pins: the host's copy
// would be invisible to the only UI that shows the conversation, so the user
// would see a history the pet silently disagreed with.
func TestLLMChatWritesNoTranscript(t *testing.T) {
	endpoint := newChatStub(t)
	a := pluginApp(t)
	id := installSampleWith(t, a, chatTestManifest())
	setPluginConfig(t, a, id, map[string]string{
		"llmBaseUrl": endpoint.URL(),
		"llmModel":   "test-model",
	})

	dir, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}
	historyPath := filepath.Join(dir, "llm-history")

	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"在吗"}`); err != nil {
		t.Fatalf("llm.chat: %v", err)
	}
	if _, err := os.Stat(historyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the host wrote the plugin's transcript: %v", err)
	}

	// The plugin's own round trip is the other half: the file it may write is
	// the file it reads back, through the ordinary storage capability.
	line := `[{"role":"user","text":"在吗"}]`
	payload, _ := json.Marshal(map[string]any{"key": "llm-history", "value": line})
	if _, err := a.PluginRequest(id, "storage.set", string(payload)); err != nil {
		t.Fatalf("storage.set: %v", err)
	}
	out, err := a.PluginRequest(id, "storage.get", `{"key":"llm-history"}`)
	if err != nil {
		t.Fatalf("storage.get: %v", err)
	}
	if !strings.Contains(out, "在吗") {
		t.Fatalf("the plugin's own transcript did not survive the round trip: %s", out)
	}
}

// TestLLMChatStreamsToThePlugin covers T49 and T77: the reply arrives as several
// llm.delta frames followed by one llm.done, all carrying the same requestId,
// and the text the frames add up to is the whole reply.
//
// The requestId is what lets a plugin ignore a frame from a superseded round, so
// its consistency is not cosmetic. The frames come out of the UNCHANGED
// internal/llm streaming path: this test is the evidence that moving the
// parameters did not move the transport.
func TestLLMChatStreamsToThePlugin(t *testing.T) {
	endpoint := newChatStub(t)
	a := pluginApp(t)
	collected := collectPluginEvents(a)
	id := installSampleWith(t, a, chatTestManifest())
	setPluginConfig(t, a, id, map[string]string{
		"llmBaseUrl": endpoint.URL(),
		"llmModel":   "test-model",
	})

	out, err := a.PluginRequest(id, "llm.chat", `{"text":"在吗"}`)
	if err != nil {
		t.Fatalf("llm.chat: %v", err)
	}
	if !strings.Contains(out, sseFullReply) {
		t.Fatalf("the return value must carry the reply, got %s", out)
	}

	deltas := collected("llm.delta")
	done := collected("llm.done")
	if len(deltas) < 2 {
		t.Fatalf("want at least 2 delta frames, got %d", len(deltas))
	}
	if len(done) != 1 {
		t.Fatalf("want exactly 1 done frame, got %d", len(done))
	}

	var requestID string
	var text strings.Builder
	for _, ev := range deltas {
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			t.Fatalf("delta payload has the wrong type: %T", ev.Payload)
		}
		rid, _ := payload["requestId"].(string)
		if requestID == "" {
			requestID = rid
		} else if rid != requestID {
			t.Fatalf("the deltas carry different requestIds: %q and %q", requestID, rid)
		}
		piece, _ := payload["text"].(string)
		text.WriteString(piece)
		if ev.PluginID != id {
			t.Fatalf("a delta names the wrong plugin: %q", ev.PluginID)
		}
	}
	if text.String() != sseFullReply {
		t.Fatalf("the deltas add up to %q, want %q", text.String(), sseFullReply)
	}
	donePayload, _ := done[0].Payload.(map[string]any)
	if got, _ := donePayload["requestId"].(string); got != requestID {
		t.Fatalf("the done frame's requestId is %q, want %q", got, requestID)
	}
	if got, _ := donePayload["text"].(string); got != sseFullReply {
		t.Fatalf("the done frame carries %q, want the whole reply", got)
	}

	// The tokens the server reported land in the plugin's OWN counter, on top of
	// the attempt that was counted when the request went out. Both halves come
	// from store.LLMUsage, so the plugin's budget rolls over on the same clock
	// as the pet's always did.
	u := pluginUsageOnDisk(t, a, id)
	if u.Requests != 1 {
		t.Errorf("requests = %d, want 1", u.Requests)
	}
	if u.PromptTokens != 3 || u.CompletionTokens != 2 {
		t.Errorf("tokens = %d/%d, want the reported 3/2", u.PromptTokens, u.CompletionTokens)
	}
}

// --- T51: bubble ------------------------------------------------------------

// TestBubbleSayCapability covers T51: the bubble goes through the ordinary
// showBubble path, so the layout reports the same box the region is cropped to.
func TestBubbleSayCapability(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"pet.bubble"}})

	if _, err := a.PluginRequest(id, "bubble.say", `{"text":"插件说的"}`); err != nil {
		t.Fatalf("bubble.say: %v", err)
	}
	a.mu.Lock()
	bubble := a.bubble
	advertised := a.layout.Bubble
	a.mu.Unlock()

	if bubble == nil || advertised == nil {
		t.Fatal("the bubble must be recorded in BOTH the state and the layout")
	}
	if bubble.Kind != BubbleReminder {
		t.Errorf("kind = %q, want %q", bubble.Kind, BubbleReminder)
	}
	if bubble.Title != "插件说的" {
		t.Errorf("title = %q", bubble.Title)
	}
	if advertised.Kind != bubble.Kind || advertised.Title != bubble.Title {
		t.Errorf("the layout disagrees with the state: %+v vs %+v", advertised, bubble)
	}
	if advertised.H != float64(bubbleHeightReminder) {
		t.Errorf("the bubble's height = %v, want %d", advertised.H, bubbleHeightReminder)
	}
}

// --- T52: the capability table is complete ----------------------------------

// TestCapabilityTableIsComplete covers T52: the set of permissions the table
// uses must be EXACTLY the fifteen the manifest validator accepts.
//
// A permission in the vocabulary but used by no method is a switch the user is
// asked about that does nothing; a method naming a permission outside it is a
// capability the user would never be asked to allow.
//
// "Used" counts pluginMethod.also, not just perm. `net.credentials` is the second
// gate of net.fetch (a method cannot require it to be callable, or every net.fetch
// plugin would break), but it IS a gate the dispatcher's sibling read: the payload
// names a secret and the host fills the Cookie header only for a caller that holds
// it. Leaving it out of `used` would report it as a switch with nothing behind it,
// which is the very thing this test exists to catch.
//
// This is also where the retired timer.* pair stays retired: with timer.state and the
// four driving methods gone, timer.read and timer.control have nothing behind them, so
// putting either back in AllowedPermissions fails here instead of reaching a user's
// install dialog as a switch that does nothing.
func TestCapabilityTableIsComplete(t *testing.T) {
	used := map[string]bool{}
	for name, spec := range pluginMethods {
		if spec.perm == "" {
			t.Errorf("%s has no permission", name)
		}
		if !plugins.IsAllowedPermission(spec.perm) {
			t.Errorf("%s names the unknown permission %q", name, spec.perm)
		}
		used[spec.perm] = true
		for _, extra := range spec.also {
			if !plugins.IsAllowedPermission(extra) {
				t.Errorf("%s names the unknown second gate %q", name, extra)
			}
			if extra == spec.perm {
				t.Errorf("%s lists %q as its own second gate", name, extra)
			}
			used[extra] = true
		}
	}
	for _, perm := range plugins.AllowedPermissions {
		if !used[perm] {
			t.Errorf("the permission %s is used by no method", perm)
		}
	}
	if len(used) != len(plugins.AllowedPermissions) {
		t.Fatalf("the table uses %d permissions, the vocabulary has %d", len(used), len(plugins.AllowedPermissions))
	}
	// Every method must resolve to exactly one permission, which the map
	// guarantees structurally; this records it as an assertion rather than a
	// comment.
	for name, spec := range pluginMethods {
		if spec.run == nil {
			t.Errorf("%s has no implementation", name)
		}
		if spec.perm == "" {
			t.Errorf("%s has an empty permission", name)
		}
	}
}

// --- the host's own capability answer ---------------------------------------

// TestPluginHostInfoIsTheDispatchTable pins the one thing that makes `init.host` worth having.
//
// A plugin decides from this list whether to send a request at all. So a name that appears here
// but is absent from `pluginMethods` would invite a call the gate refuses - turning "this window
// cannot do that" into "it can, try it", which is the mistake that produced 571 WARN lines on
// 2026-10-30. The list is computed from the table rather than written out, and this test is what
// keeps a future "just hardcode the four the workshop needs" from passing.
func TestPluginHostInfoIsTheDispatchTable(t *testing.T) {
	a := pluginApp(t)
	info := a.PluginHostInfo()
	if info.Program != "pet" {
		t.Fatalf("Program = %q, want pet", info.Program)
	}
	if len(info.Methods) != len(pluginMethods) {
		t.Fatalf("the answer names %d methods, the table has %d", len(info.Methods), len(pluginMethods))
	}
	for i, name := range info.Methods {
		if _, ok := pluginMethods[name]; !ok {
			t.Errorf("%q is advertised but not dispatchable", name)
		}
		// Sorted: the list travels into a plugin's document, and Go's map order is random.
		if i > 0 && info.Methods[i-1] > name {
			t.Errorf("Methods are not sorted at %d: %q after %q", i, name, info.Methods[i-1])
		}
	}
	// And the direction that matters for a plugin's UI: everything callable is advertised.
	for name := range pluginMethods {
		found := false
		for _, advertised := range info.Methods {
			if advertised == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q is dispatchable but not advertised", name)
		}
	}
}

// --- process.launch ---------------------------------------------------------

// TestProcessLaunchRefusesBadPaths covers the checks that must happen BEFORE
// anything is started: a plugin cannot reach cmd.Start with a path the user never
// typed (relative), a directory, a file that is not an .exe, or an argument list
// that contains a newline.
//
// Each case asserts the message NAMES THE FIELD. The plugin shows these sentences
// verbatim in its own log, so "invalid input" would leave the user with no idea
// which of five inputs to fix.
func TestProcessLaunchRefusesBadPaths(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"process"}})

	dir := t.TempDir()
	notExe := filepath.Join(dir, "yogurt.txt")
	if err := os.WriteFile(notExe, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// An .exe that exists but is not a real program. The ARGUMENT checks run before
	// anything is started, so this file is never executed - it only has to get past
	// the extension and size checks to reach them.
	fakeExe := filepath.Join(dir, "yogurt.exe")
	if err := os.WriteFile(fakeExe, []byte("not a real program"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	missing := filepath.Join(dir, "nope.exe")

	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"empty path", `{}`, "需要 path"},
		{"whitespace path", `{"path":"   "}`, "需要 path"},
		{"relative path", `{"path":"yogurt.exe"}`, "必须是绝对路径"},
		{"missing file", `{"path":` + quoteJSON(missing) + `}`, "找不到文件"},
		{"directory", `{"path":` + quoteJSON(dir) + `}`, "是目录"},
		{"not an exe", `{"path":` + quoteJSON(notExe) + `}`, "只允许启动 .exe"},
		{"too many args", `{"path":` + quoteJSON(fakeExe) + `,"args":[` +
			strings.Repeat(`"a",`, maxLaunchArgs) + `"a"]}`, "最多"},
		{"newline in an arg", `{"path":` + quoteJSON(fakeExe) + `,"args":["a\nb"]}`, "换行"},
		{"long arg", `{"path":` + quoteJSON(fakeExe) + `,"args":[` +
			quoteJSON(strings.Repeat("x", maxLaunchArgLen+1)) + `]}`, "超过"},
		// The TOTAL has its own bound: 32 x 4096 would sail past the Windows limit, and a
		// command line that is too long fails at exec time with a message about nothing in
		// particular. Five 4000-character arguments are under the per-argument cap and over
		// the total one, so this case is about the total and nothing else.
		{"args total too long", `{"path":` + quoteJSON(fakeExe) + `,"args":[` +
			strings.Repeat(quoteJSON(strings.Repeat("y", 4000))+`,`, 4) +
			quoteJSON(strings.Repeat("y", 4000)) + `]}`, "总长度"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := a.PluginRequest(id, "process.launch", tc.payload)
			if err == nil {
				t.Fatal("this payload must be refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

// TestProcessLaunchStartsAndReturnsAPid proves the successful path: the program
// starts, the call returns its pid, and it returns WITHOUT waiting for the child
// (a command that sleeps briefly would otherwise make this test take that long).
//
// The program is the system shell running `exit`, which is the smallest thing that
// is certainly an .exe and certainly harmless.
func TestProcessLaunchStartsAndReturnsAPid(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("process.launch insists on a .exe, so the happy path is Windows-only")
	}
	shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if _, err := os.Stat(shell); err != nil {
		t.Skipf("no cmd.exe at %s", shell)
	}

	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"process"}})

	started := time.Now()
	out, err := a.PluginRequest(id, "process.launch",
		`{"path":`+quoteJSON(shell)+`,"args":["/c","ping -n 6 127.0.0.1 >nul"]}`)
	if err != nil {
		t.Fatalf("process.launch: %v", err)
	}
	// The ping sleeps ~5s. Returning in well under that is what "does not wait"
	// means, and it is the only observable difference between Start and Run.
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("process.launch took %v, so it waited for the child", elapsed)
	}
	var res struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result %q is not {pid}: %v", out, err)
	}
	if res.PID <= 0 {
		t.Fatalf("pid = %d, want a real process id", res.PID)
	}
}

// TestProcessLaunchNeedsThePermission pins the ORDER the dispatcher promises: the
// permission is checked before the payload is even parsed, so a plugin without
// `process` cannot use a malformed call as a probe for what the host would accept.
func TestProcessLaunchNeedsThePermission(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"pet.bubble"}})
	_, err := a.PluginRequest(id, "process.launch", `{"path":"C:\\x.exe"}`)
	if err == nil || !strings.Contains(err.Error(), plugins.PermProcess) {
		t.Fatalf("error = %v, want a refusal naming %s", err, plugins.PermProcess)
	}
}

// --- process.kill 与子进程回收 ----------------------------------------------

// launchSleeper starts a ~5 second child through the host and returns its pid.
//
// cmd.exe + `ping` is the smallest program that is certainly an .exe, certainly
// harmless and certainly still running when the assertions below fire.
func launchSleeper(t *testing.T, a *App, id string) int {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("process.launch insists on a .exe, so this is Windows-only")
	}
	shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if _, err := os.Stat(shell); err != nil {
		t.Skipf("no cmd.exe at %s", shell)
	}
	out, err := a.PluginRequest(id, "process.launch",
		`{"path":`+quoteJSON(shell)+`,"args":["/c","ping -n 6 127.0.0.1 >nul"]}`)
	if err != nil {
		t.Fatalf("process.launch: %v", err)
	}
	var res struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result %q is not {pid}: %v", out, err)
	}
	if res.PID <= 0 {
		t.Fatalf("pid = %d, want a real process id", res.PID)
	}
	return res.PID
}

// waitChildRunning polls process.state until it agrees with want.
func waitChildRunning(t *testing.T, a *App, id string, pid int, want bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for {
		out, err := a.PluginRequest(id, "process.state", fmt.Sprintf(`{"pid":%d}`, pid))
		if err != nil {
			t.Fatalf("process.state: %v", err)
		}
		var st struct {
			Known   bool `json:"known"`
			Running bool `json:"running"`
		}
		if err := json.Unmarshal([]byte(out), &st); err != nil {
			t.Fatalf("state %q is not JSON: %v", out, err)
		}
		if st.Known && st.Running == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process.state still says running=%v (known=%v), want %v", st.Running, st.Known, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// childStillRunning reads the host's own table, for the cases where the plugin can no
// longer ask (it was disabled, so PluginRequest refuses everything).
func childStillRunning(a *App, pid int) bool {
	a.pluginHost.childMu.Lock()
	defer a.pluginHost.childMu.Unlock()
	child, ok := a.pluginHost.children[pid]
	if !ok {
		return false
	}
	return child.handle != nil && !child.proc.done
}

func waitChildReaped(t *testing.T, a *App, pid int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for childStillRunning(a, pid) {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d is still running after the reap", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestProcessKillStopsTheChildItStarted is the whole point of the method: a plugin can
// stop the program it started, and the host's own table agrees afterwards.
func TestProcessKillStopsTheChildItStarted(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"process"}})
	pid := launchSleeper(t, a, id)
	waitChildRunning(t, a, id, pid, true)

	out, err := a.PluginRequest(id, "process.kill", fmt.Sprintf(`{"pid":%d}`, pid))
	if err != nil {
		t.Fatalf("process.kill: %v", err)
	}
	var res struct {
		Killed bool `json:"killed"`
		Known  bool `json:"known"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result %q is not JSON: %v", out, err)
	}
	if !res.Killed || !res.Known {
		t.Fatalf("result = %s, want killed and known", out)
	}
	waitChildRunning(t, a, id, pid, false)
}

// TestProcessKillRefusesAnotherPluginsChild: pids are guessable, so ownership is the
// check that makes this method safe to hand to every plugin that has `process`.
func TestProcessKillRefusesAnotherPluginsChild(t *testing.T) {
	a := pluginApp(t)
	owner := installSampleWith(t, a, map[string]any{
		"id": "com.example.owner", "permissions": []string{"process"},
	})
	other := installSampleWith(t, a, map[string]any{
		"id": "com.example.other", "permissions": []string{"process"},
	})
	pid := launchSleeper(t, a, owner)

	_, err := a.PluginRequest(other, "process.kill", fmt.Sprintf(`{"pid":%d}`, pid))
	if err == nil || !strings.Contains(err.Error(), "不是本插件启动的") {
		t.Fatalf("error = %v, want a refusal naming the ownership", err)
	}
	// And the refusal really changed nothing.
	if !childStillRunning(a, pid) {
		t.Fatal("the refusal killed the other plugin's process")
	}
	// Leave nothing behind for the rest of the run.
	if _, err := a.PluginRequest(owner, "process.kill", fmt.Sprintf(`{"pid":%d}`, pid)); err != nil {
		t.Fatalf("cleanup kill: %v", err)
	}
}

// TestProcessKillIsIdempotentAfterExit: "make sure it is not running" must not fail just
// because the program already stopped on its own.
func TestProcessKillIsIdempotentAfterExit(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"process"}})
	pid := launchSleeper(t, a, id)
	waitChildRunning(t, a, id, pid, true)
	if _, err := a.PluginRequest(id, "process.kill", fmt.Sprintf(`{"pid":%d}`, pid)); err != nil {
		t.Fatalf("first kill: %v", err)
	}
	waitChildRunning(t, a, id, pid, false)

	out, err := a.PluginRequest(id, "process.kill", fmt.Sprintf(`{"pid":%d}`, pid))
	if err != nil {
		t.Fatalf("second kill: %v", err)
	}
	var res struct {
		Killed bool `json:"killed"`
		Known  bool `json:"known"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result %q is not JSON: %v", out, err)
	}
	if res.Killed || !res.Known {
		t.Fatalf("result = %s, want killed:false known:true", out)
	}
}

// TestProcessKillUnknownPidIsNotAnError: a pid from a previous run of the pet is
// legitimately unknown, and the same rule as process.state applies.
func TestProcessKillUnknownPidIsNotAnError(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"process"}})
	out, err := a.PluginRequest(id, "process.kill", `{"pid":999999}`)
	if err != nil {
		t.Fatalf("process.kill: %v", err)
	}
	var res struct {
		Killed bool `json:"killed"`
		Known  bool `json:"known"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result %q is not JSON: %v", out, err)
	}
	if res.Killed || res.Known {
		t.Fatalf("result = %s, want killed:false known:false", out)
	}
}

// TestProcessKillNeedsThePermission pins the same order as launch: the permission is
// checked before the payload is parsed.
func TestProcessKillNeedsThePermission(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"pet.bubble"}})
	_, err := a.PluginRequest(id, "process.kill", `{"pid":1}`)
	if err == nil || !strings.Contains(err.Error(), plugins.PermProcess) {
		t.Fatalf("error = %v, want a refusal naming %s", err, plugins.PermProcess)
	}
}

// TestReapStopsChildrenOfDisabledPlugins: switching a plugin off has to take its
// programs with it, or the helper keeps writing files nobody reads.
func TestReapStopsChildrenOfDisabledPlugins(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"process"}})
	pid := launchSleeper(t, a, id)
	waitChildRunning(t, a, id, pid, true)

	managerSetEnabled(t, a, id, false)
	a.reapPluginChildren()
	waitChildReaped(t, a, pid)
}

// TestReapStopsChildrenOfUpdatedPlugins: an update remounts the frames, so the child the
// OLD version started is nobody's - which is exactly the 2026-10-31 orphan.
func TestReapStopsChildrenOfUpdatedPlugins(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"process"}})
	pid := launchSleeper(t, a, id)
	waitChildRunning(t, a, id, pid, true)

	// Pretend the child was started by an older version of the same plugin.
	a.pluginHost.childMu.Lock()
	a.pluginHost.children[pid].version = "0.0.1-old"
	a.pluginHost.childMu.Unlock()

	a.reapPluginChildren()
	waitChildReaped(t, a, pid)
}

// TestReapLeavesChildrenOfHealthyPlugins is the other half of the contract: a protocol
// side that is still wanted must not be stopped by a background tick.
func TestReapLeavesChildrenOfHealthyPlugins(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"process"}})
	pid := launchSleeper(t, a, id)
	waitChildRunning(t, a, id, pid, true)

	a.reapPluginChildren()
	if !childStillRunning(a, pid) {
		t.Fatal("the reaper stopped a child of an enabled, unchanged plugin")
	}
	if _, err := a.PluginRequest(id, "process.kill", fmt.Sprintf(`{"pid":%d}`, pid)); err != nil {
		t.Fatalf("cleanup kill: %v", err)
	}
}

// --- share.changed（发布 → 声明过它的读者） ---------------------------------

// TestShareSetNotifiesDeclaredReaders pins the new down-link: publishing a key wakes the
// plugins that named it in `shareRead`, and nobody else.
//
// The "nobody else" half is the authorisation: a third plugin that holds `plugin.share`
// but did not declare the pair must NOT be told. That is the same rule `share.get`
// enforces, spent one delivery earlier.
func TestShareSetNotifiesDeclaredReaders(t *testing.T) {
	a := pluginApp(t)
	events := collectPluginEvents(a)

	publisher := installSampleWith(t, a, map[string]any{
		"id": "com.example.publisher", "permissions": []string{"plugin.share"},
	})
	reader := installSampleWith(t, a, map[string]any{
		"id": "com.example.reader", "permissions": []string{"plugin.share"},
		"shareRead": []string{"com.example.publisher/now-playing"},
	})
	installSampleWith(t, a, map[string]any{
		"id": "com.example.bystander", "permissions": []string{"plugin.share"},
	})

	if _, err := a.PluginRequest(publisher, "share.set",
		`{"name":"now-playing","value":"{\"title\":\"晴天\"}"}`); err != nil {
		t.Fatalf("share.set: %v", err)
	}

	got := events("share.changed")
	if len(got) != 1 {
		t.Fatalf("share.changed events = %d, want 1 (only the declared reader)", len(got))
	}
	if got[0].PluginID != reader {
		t.Fatalf("event went to %q, want the declared reader %q", got[0].PluginID, reader)
	}
	payload, ok := got[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want an object", got[0].Payload)
	}
	if payload["key"] != "com.example.publisher/now-playing" {
		t.Fatalf("payload.key = %v, want the <owner>/<name> pair", payload["key"])
	}
	if payload["name"] != "now-playing" || payload["owner"] != publisher {
		t.Fatalf("payload = %#v, want name/owner of the published key", payload)
	}

	// A key nobody declared wakes nobody: the loop is driven by shareRead, not by
	// "everyone who can read anything".
	if _, err := a.PluginRequest(publisher, "share.set",
		`{"name":"nobody-wants-this","value":"x"}`); err != nil {
		t.Fatalf("share.set: %v", err)
	}
	if again := events("share.changed"); len(again) != 1 {
		t.Fatalf("share.changed events = %d after publishing an undeclared key, want still 1", len(again))
	}
}

// --- sidecar（manifest 声明的辅助脚本） -------------------------------------

// installSidecarPlugin installs a package whose zip ALSO carries the helper script, so
// the `sidecar.script` declaration points at a file that is really in the package (the
// manifest validator refuses a script that is not in `assets`, and the installer only
// extracts what the package contains).
func installSidecarPlugin(t *testing.T, a *App, man map[string]any, script, body string) string {
	t.Helper()
	manifestBytes, err := json.Marshal(man)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	files := map[string]string{
		"manifest.json": string(manifestBytes),
		"panel.html":    "<!doctype html><p>test</p>",
		script:          body,
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
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
		Description: man["description"].(string),
		Permissions: stringsFrom(man["permissions"]),
		Download:    srv.URL + "/pkg.zip",
		SHA256:      hex.EncodeToString(sum[:]),
	}
	if _, err := a.pluginManager().Install(entry); err != nil {
		t.Fatalf("Install: %v", err)
	}
	id := entry.ID
	managerGrant(t, a, id)
	managerSetEnabled(t, a, id, true)
	return id
}

// sidecarManifest is the smallest legal sidecar declaration: process + background, the
// script listed in assets, and nothing else.
func sidecarManifest(id, script string) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        "带辅助脚本的插件",
		"version":     "1.0.0",
		"author":      "tester",
		"description": "用于测试的插件",
		"permissions": []string{"process", "events"},
		"background":  true,
		"entry":       "panel.html",
		"assets":      []string{script},
		"sidecar":     map[string]any{"script": script},
	}
}

// TestSidecarStartsAndIsReaped is the whole point of the declaration: the host starts the
// helper, tracks it like any other child, and takes it away when the plugin goes.
func TestSidecarStartsAndIsReaped(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("sidecars are .ps1 or .exe; this is Windows-only")
	}
	a := pluginApp(t)
	id := installSidecarPlugin(t, a, sidecarManifest("com.example.sidecar", "helper.ps1"),
		"helper.ps1", "Start-Sleep -Seconds 30\n")

	a.ensurePluginSidecars()

	// The child is tracked under the plugin's id, which is what makes process.kill and
	// the reaper work on it.
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for pid == 0 {
		a.pluginHost.childMu.Lock()
		for p, child := range a.pluginHost.children {
			if child.pluginID == id {
				pid = p
			}
		}
		a.pluginHost.childMu.Unlock()
		if pid == 0 {
			if time.Now().After(deadline) {
				t.Fatal("the sidecar was never started")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// A second call must not start a second helper (the tick runs every five seconds).
	a.ensurePluginSidecars()
	a.pluginHost.childMu.Lock()
	count := 0
	for _, child := range a.pluginHost.children {
		if child.pluginID == id {
			count++
		}
	}
	a.pluginHost.childMu.Unlock()
	if count != 1 {
		t.Fatalf("tracked children for the plugin = %d, want 1 (the tick must be idempotent)", count)
	}

	// Disabling the plugin takes the helper with it.
	managerSetEnabled(t, a, id, false)
	a.reapPluginChildren()
	waitChildReaped(t, a, pid)
}

// TestSidecarNeedsTheGrantedProcessPermission: the manifest asks, the USER decides. A
// plugin whose `process` grant was unticked must not have a program started for it.
func TestSidecarNeedsTheGrantedProcessPermission(t *testing.T) {
	a := pluginApp(t)
	id := installTestPlugin(t, a, sidecarManifest("com.example.nosidecar", "helper.ps1"))
	if err := a.pluginManager().SetEnabled(id, true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	// Granted nothing at all.
	a.ensurePluginSidecars()
	a.pluginHost.childMu.Lock()
	defer a.pluginHost.childMu.Unlock()
	for pid, child := range a.pluginHost.children {
		if child.pluginID == id {
			t.Fatalf("started a sidecar (pid %d) for a plugin that was not granted process", pid)
		}
	}
}

// quoteJSON renders s as a JSON string literal, for payloads built by hand.
func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// --- llm.chat 的图片 ---------------------------------------------------------

// TestChatImagesAllowInlinePicturesAndBoundThem covers the two forms one llm.chat call may
// carry, and the bounds on the second one.
//
// The inline form exists because a chat composer promises "attach a picture", and a picture
// the user just picked off their disk has no host to allow-list. Its bytes travel through
// this process, so the type and the size are checked HERE - the model endpoint is the only
// other place that would see them, and it is too late there.
//
// This path had no test at all before: the http(s) allow-list was exercised only indirectly,
// by whichever plugin happened to send a picture.
func TestChatImagesAllowInlinePicturesAndBoundThem(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{
		"permissions": []string{"llm.chat", "net.fetch"},
		"netHosts":    []string{"cdn.example.com"},
	})
	row, ok := a.pluginManager().Get(id)
	if !ok || row.Manifest == nil {
		t.Fatalf("the fixture plugin is not installed")
	}

	// A real (tiny) PNG, so "valid base64" is not the only thing being asserted.
	const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	inline := "data:image/png;base64," + tinyPNG

	t.Run("http(s) on a declared host still works", func(t *testing.T) {
		got, err := a.pluginChatImages(id, *row.Manifest, []string{"https://cdn.example.com/a.png"})
		if err != nil {
			t.Fatalf("a declared host was refused: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("http(s) on an undeclared host is still refused", func(t *testing.T) {
		if _, err := a.pluginChatImages(id, *row.Manifest, []string{"https://evil.example/x.png"}); err == nil {
			t.Fatal("an undeclared host was accepted")
		} else if !strings.Contains(err.Error(), "未声明的域名") {
			t.Fatalf("error must name the reason, got %q", err)
		}
	})

	t.Run("an inline picture passes through unchanged", func(t *testing.T) {
		got, err := a.pluginChatImages(id, *row.Manifest, []string{inline})
		if err != nil {
			t.Fatalf("an inline picture was refused: %v", err)
		}
		if len(got) != 1 || got[0] != inline {
			t.Fatalf("the data URI must survive verbatim, got %v", got)
		}
	})

	t.Run("only image types, and only base64", func(t *testing.T) {
		cases := []struct{ uri, want string }{
			{"data:text/plain;base64," + tinyPNG, "png/jpeg/webp/gif"},
			{"data:image/png," + tinyPNG, "base64"},
			{"data:image/png;base64,!!!not base64!!!", "base64"},
		}
		for _, c := range cases {
			if _, err := a.pluginChatImages(id, *row.Manifest, []string{c.uri}); err == nil {
				t.Errorf("%q was accepted", c.uri)
			} else if !strings.Contains(err.Error(), c.want) {
				t.Errorf("%q: error %q does not mention %q", c.uri, err, c.want)
			}
		}
	})

	t.Run("a picture larger than the per-image cap is refused by name", func(t *testing.T) {
		// Just over the decoded limit; the payload is one big repetitive string, so this
		// costs a few MB of memory and nothing else.
		tooBig := "data:image/png;base64," + strings.Repeat("A", base64.StdEncoding.EncodedLen(maxChatImageBytes)+8)
		_, err := a.pluginChatImages(id, *row.Manifest, []string{tooBig})
		if err == nil {
			t.Fatal("an oversized picture was accepted")
		}
		if !strings.Contains(err.Error(), "上限") {
			t.Fatalf("error must name the limit, got %q", err)
		}
	})

	t.Run("several pictures may not exceed the per-call cap", func(t *testing.T) {
		// ~3 MiB decoded each, three of them: over the 8 MiB per-call budget, under the
		// 4 MiB per-image one. Reusing one string keeps the test cheap.
		chunk := "data:image/jpeg;base64," + strings.Repeat("A", base64.StdEncoding.EncodedLen(3<<20))
		_, err := a.pluginChatImages(id, *row.Manifest, []string{chunk, chunk, chunk})
		if err == nil {
			t.Fatal("a call carrying 9 MiB of pictures was accepted")
		}
		if !strings.Contains(err.Error(), "合计") {
			t.Fatalf("error must say the pictures add up, got %q", err)
		}
	})
}

// --- 能力发现：plugins.services ---------------------------------------------

// TestPluginServicesListsWhatEachPluginServes covers the "who can answer what" method.
//
// The pressure behind it: a caller can only reach the targets it DECLARES (`callTargets`), and
// nothing told it what those targets were - so "who can answer a definition / a search" was
// hardcoded in every consumer, and a newly installed provider stayed invisible until someone
// edited the core. Reading the install state makes `serves` a promise people can hear.
func TestPluginServicesListsWhatEachPluginServes(t *testing.T) {
	a := pluginApp(t)
	lexicon := installSampleWith(t, a, map[string]any{
		"id":          "com.example.lexicon",
		"serves":      []string{"slang", "wiki"},
		"permissions": []string{"plugin.call"},
	})
	search := installSampleWith(t, a, map[string]any{
		"id":          "com.example.search",
		"serves":      []string{"search"},
		"permissions": []string{"plugin.call"},
	})
	// 一个不提供任何方法的插件（纯界面 / 纯常驻）不该出现在列表里。
	installSampleWith(t, a, map[string]any{
		"id":          "com.example.plain",
		"serves":      nil,
		"permissions": []string{"storage"},
	})
	// 已停用的也不该出现：装着但关掉的功能不该被推荐出去。
	disabled := installSampleWith(t, a, map[string]any{
		"id":          "com.example.off",
		"serves":      []string{"audio"},
		"permissions": []string{"plugin.call"},
	})
	if err := a.pluginManager().SetEnabled(disabled, false); err != nil {
		t.Fatalf("停用插件：%v", err)
	}

	raw, err := pluginServices(a, "com.example.caller", nil)
	if err != nil {
		t.Fatalf("plugins.services: %v", err)
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("序列化：%v", err)
	}
	var decoded struct {
		Services []struct {
			ID     string   `json:"id"`
			Name   string   `json:"name"`
			Serves []string `json:"serves"`
		} `json:"services"`
	}
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("反序列化：%v", err)
	}

	got := map[string][]string{}
	for _, row := range decoded.Services {
		got[row.ID] = row.Serves
	}
	if _, ok := got[lexicon]; !ok {
		t.Fatalf("提供 slang/wiki 的插件没有被列出来：%v", decoded.Services)
	}
	if want := []string{"slang", "wiki"}; !reflect.DeepEqual(got[lexicon], want) {
		t.Fatalf("%s 的 serves 是 %v，want %v", lexicon, got[lexicon], want)
	}
	if _, ok := got[search]; !ok {
		t.Fatalf("提供 search 的插件没有被列出来")
	}
	if _, ok := got["com.example.plain"]; ok {
		t.Fatal("不提供任何方法的插件不该出现在列表里")
	}
	if _, ok := got[disabled]; ok {
		t.Fatal("停用的插件不该出现在列表里")
	}

	// 自己不在列表里：一个插件问"谁能干什么"时，自己那张名片没有用。
	self, err := pluginServices(a, lexicon, nil)
	if err != nil {
		t.Fatalf("plugins.services（自问）：%v", err)
	}
	selfBlob, _ := json.Marshal(self)
	var selfDecoded struct {
		Services []struct {
			ID string `json:"id"`
		} `json:"services"`
	}
	_ = json.Unmarshal(selfBlob, &selfDecoded)
	for _, row := range selfDecoded.Services {
		if row.ID == lexicon {
			t.Fatal("调用方自己不该出现在列表里")
		}
	}
}

// TestClampPluginMaxTokens covers the bounds on a per-call output budget.
//
// The field exists because thinking models bill their reasoning against the same budget: a
// small cap turns into an EMPTY reply with a non-empty reasoning block, and the only lever a
// plugin has is "ask again with more room". The host still decides how much more.
func TestClampPluginMaxTokens(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{1, minPluginChatTokens},
		{minPluginChatTokens - 1, minPluginChatTokens},
		{480, 480},
		{maxPluginChatTokens, maxPluginChatTokens},
		{maxPluginChatTokens + 1, maxPluginChatTokens},
		{1 << 20, maxPluginChatTokens},
	}
	for _, c := range cases {
		if got := clampPluginMaxTokens(c.in); got != c.want {
			t.Errorf("clampPluginMaxTokens(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
