package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"PetPC/internal/plugins"
)

// This file covers the market program's BINDINGS.
//
// Every one of them is a projection over internal/plugins, and that is exactly
// what these tests assert: they drive the real binding against a temp data
// directory and then read the FILES, so a binding that grew its own copy of a rule
// (its own index parsing, its own permission check, its own idea of what
// "installed" means) would show up here as the two disagreeing. The pet calls the
// same manager, so a second implementation in this program would be a second
// answer to "what is installed".

// --- fixtures ---------------------------------------------------------------

// makeZip builds a package from name->content, returning its bytes and digest.
func makeZip(t *testing.T, files map[string]string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:])
}

// testManifest is a manifest JSON document with the fields these tests vary.
func testManifest(t *testing.T, id string, extra map[string]any) string {
	t.Helper()
	doc := map[string]any{
		"id":          id,
		"name":        "示例插件",
		"version":     "1.0.0",
		"author":      "tester",
		"description": "用于测试",
		"permissions": []string{"pet.bubble", "storage"},
		"panel":       true,
		"entry":       "panel.html",
	}
	for k, v := range extra {
		doc[k] = v
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return string(raw)
}

// serveZip publishes a package on a real HTTP server. A file:// download is
// refused by design, so the install path exercised is the one that ships.
func serveZip(t *testing.T, data []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/pkg.zip"
}

// serveCatalogue publishes an index.json and returns the base URL to point a
// manager at.
func serveCatalogue(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/index.json") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// newTestMarket opens a Market against a temp data directory, with the catalogue
// served by base (empty means the built-in sample source).
func newTestMarket(t *testing.T, base string) *Market {
	t.Helper()
	dir := t.TempDir()
	m, err := newMarketAt(dir, "")
	if err != nil {
		t.Fatalf("newMarketAt: %v", err)
	}
	if base != "" {
		m.cfg.MarketBaseURL = base
		m.manager.BaseURL = func() string { return base }
	}
	return m
}

// catalogueRow is one entry of an index.json body.
func catalogueRow(id, download, sum string) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        "示例插件",
		"version":     "1.0.0",
		"author":      "tester",
		"description": "用于测试",
		"permissions": []string{"pet.bubble", "storage"},
		"download":    download,
		"sha256":      sum,
	}
}

func indexBody(rows ...map[string]any) string {
	doc := map[string]any{"version": 1, "plugins": rows}
	raw, _ := json.Marshal(doc)
	return string(raw)
}

// --- T7: Installed is a projection ------------------------------------------

func TestInstalledProjectsTheIndexOnDisk(t *testing.T) {
	m := newTestMarket(t, "")

	// Hand-write the data directory exactly the way a completed install leaves it.
	id := "com.example.demo"
	pluginDir := filepath.Join(m.dataDir, "plugins", id)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"),
		[]byte(testManifest(t, id, nil)), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	index := fmt.Sprintf(`{"version":1,"plugins":[{"id":%q,"version":"1.0.0","enabled":true,`+
		`"granted":["pet.bubble"],"installedAt":"2026-01-01T00:00:00Z"}]}`, id)
	if err := os.WriteFile(filepath.Join(m.dataDir, "plugins.json"), []byte(index), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	rows, err := m.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Installed returned %d rows, want 1", len(rows))
	}
	if rows[0].ID != id || rows[0].Version != "1.0.0" || !rows[0].Enabled {
		t.Fatalf("row = %+v, want the on-disk id/version/enabled", rows[0])
	}
	if rows[0].Manifest == nil {
		t.Fatalf("the manifest was not loaded: %+v", rows[0])
	}
	if got := rows[0].Granted; len(got) != 1 || got[0] != "pet.bubble" {
		t.Fatalf("granted = %v, want the on-disk list", got)
	}
}

// --- T8: Install goes through the shared manager ----------------------------

func TestInstallUsesTheSharedManager(t *testing.T) {
	data, sum := makeZip(t, map[string]string{
		"manifest.json": testManifest(t, "com.example.demo", nil),
		"panel.html":    "<!doctype html><p>hi</p>",
	})
	base := serveCatalogue(t, indexBody(catalogueRow("com.example.demo", serveZip(t, data), sum)))
	m := newTestMarket(t, base)

	if err := m.Install("com.example.demo"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Asserted on DISK, because that is what the pet reads: a binding that
	// installed into memory would pass a check against Installed() alone.
	if _, err := os.Stat(filepath.Join(m.dataDir, "plugins", "com.example.demo", "manifest.json")); err != nil {
		t.Fatalf("the manifest was not unpacked: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(m.dataDir, "plugins.json"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if !strings.Contains(string(raw), "com.example.demo") {
		t.Fatalf("the index has no entry for the installed plugin:\n%s", raw)
	}

	// A second install of the same id is refused by the manager, not accepted as
	// an overwrite.
	if err := m.Install("com.example.demo"); err == nil {
		t.Fatal("installing an already-installed plugin succeeded")
	}
	// And an id that is not in the catalogue is refused with a message naming it.
	if err := m.Install("com.example.nope"); err == nil {
		t.Fatal("installing an unknown id succeeded")
	}
}

// --- T9: Grant and SetEnabled go through the shared manager -----------------

// installDemo installs one plugin through the real path and returns its id.
func installDemo(t *testing.T, m *Market) string {
	t.Helper()
	const id = "com.example.demo"
	data, sum := makeZip(t, map[string]string{
		"manifest.json": testManifest(t, id, nil),
		"panel.html":    "<!doctype html><p>hi</p>",
	})
	m.manager.BaseURL = func() string {
		return serveCatalogue(t, indexBody(catalogueRow(id, serveZip(t, data), sum)))
	}
	if err := m.Install(id); err != nil {
		t.Fatalf("install %s: %v", id, err)
	}
	return id
}

func TestGrantRecordsExactlyTheDeclaredPermissions(t *testing.T) {
	m := newTestMarket(t, "")
	id := installDemo(t, m)

	granted := []string{"pet.bubble", "storage"}
	if err := m.Grant(id, granted); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	rows, err := m.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	row := rows[0]
	if len(row.Granted) != len(granted) {
		t.Fatalf("granted = %v, want %v", row.Granted, granted)
	}
	for i, name := range granted {
		if row.Granted[i] != name {
			t.Fatalf("granted[%d] = %q, want %q", i, row.Granted[i], name)
		}
	}

	// An undeclared name is refused, and the refusal must not have written
	// anything: a partial write would leave the plugin holding a permission its
	// manifest never asked for.
	before, err := os.ReadFile(filepath.Join(m.dataDir, "plugins.json"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if err := m.Grant(id, []string{"pet.bubble", "net.fetch"}); err == nil {
		t.Fatal("granting an undeclared permission succeeded")
	}
	after, err := os.ReadFile(filepath.Join(m.dataDir, "plugins.json"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("a refused grant still wrote the index:\n before=%s\n after =%s", before, after)
	}
}

func TestSetEnabledRoundTripsThroughTheManager(t *testing.T) {
	m := newTestMarket(t, "")
	id := installDemo(t, m)

	// A fresh install is not enabled: installing and enabling are separate steps
	// so the user can look at what was installed first.
	rows, _ := m.Installed()
	if rows[0].Enabled {
		t.Fatal("a freshly installed plugin is already enabled")
	}
	if err := m.SetEnabled(id, true); err != nil {
		t.Fatalf("SetEnabled(true): %v", err)
	}
	rows, _ = m.Installed()
	if !rows[0].Enabled {
		t.Fatal("SetEnabled(true) did not take effect")
	}
	if err := m.SetEnabled(id, false); err != nil {
		t.Fatalf("SetEnabled(false): %v", err)
	}
	rows, _ = m.Installed()
	if rows[0].Enabled {
		t.Fatal("SetEnabled(false) did not take effect")
	}
}

// --- T10: SaveMarketURL validates, then writes only its own file ------------

func TestSaveMarketURLRefusesABadSchemeAndWritesNothing(t *testing.T) {
	m := newTestMarket(t, "")
	m.cfg.MarketBaseURL = "https://good.example/m"
	if err := saveMarketConfig(m.cfgPath, m.cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	beforeMarket, err := os.ReadFile(m.cfgPath)
	if err != nil {
		t.Fatalf("read market config: %v", err)
	}

	if _, err := m.SaveMarketURL("ftp://x/y"); err == nil {
		t.Fatal("SaveMarketURL accepted an ftp address")
	}

	afterMarket, err := os.ReadFile(m.cfgPath)
	if err != nil {
		t.Fatalf("read market config: %v", err)
	}
	if string(beforeMarket) != string(afterMarket) {
		t.Fatalf("a refused address still rewrote the settings file:\n before=%s\n after =%s",
			beforeMarket, afterMarket)
	}
	// The previous, working address is still the one in force.
	if got := m.SelectedID(); got != "" {
		t.Fatalf("SelectedID = %q, want empty", got)
	}
}

func TestSaveMarketURLTrimsTheTrailingSlash(t *testing.T) {
	base := serveCatalogue(t, indexBody())
	m := newTestMarket(t, base)
	// Point the write at the real server so the catalogue fetch after the save
	// succeeds; the value under test is what gets STORED.
	if _, err := m.SaveMarketURL(base + "/"); err != nil {
		t.Fatalf("SaveMarketURL: %v", err)
	}
	raw, err := os.ReadFile(m.cfgPath)
	if err != nil {
		t.Fatalf("read market config: %v", err)
	}
	if !strings.Contains(string(raw), `"marketBaseUrl": "`+base+`"`) {
		t.Fatalf("the stored address is not the trimmed base:\n%s", raw)
	}
	if strings.Contains(string(raw), base+"/\"") {
		t.Fatalf("the stored address kept its trailing slash:\n%s", raw)
	}
}

// --- T11: GetConfig never reveals a secret ----------------------------------

func TestGetConfigHidesSecrets(t *testing.T) {
	const secretValue = "sk-super-secret-value-0123456789"
	m := newTestMarket(t, "")
	id := installDemoSecret(t, m, secretValue)

	cfg, err := m.GetConfig(id)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}

	byKey := map[string]ConfigEditor{}
	for _, it := range cfg.Items {
		byKey[it.Key] = it
	}

	// The plaintext must not be in the struct ANYWHERE - not as a value, not in a
	// mask, not in the error. fmt.Sprint walks the whole thing, so a field added
	// later that leaked it would fail here without this test having to name it.
	if dumped := fmt.Sprint(cfg); strings.Contains(dumped, secretValue) {
		t.Fatalf("the plaintext secret appears in the returned struct: %s", dumped)
	}

	secret, ok := byKey["apiKey"]
	if !ok {
		t.Fatal("the declared secret item is missing from the editor")
	}
	if !secret.Secret {
		t.Fatal("the secret item is not marked as one")
	}
	if secret.Value != "" {
		t.Fatalf("a secret item carries a value: %q", secret.Value)
	}
	if !secret.SecretSet {
		t.Fatal("the stored secret is not reported as set")
	}
	if secret.SecretMask == "" || secret.SecretMask == secretValue {
		t.Fatalf("mask = %q, want a non-revealing reminder", secret.SecretMask)
	}
	if !strings.HasPrefix(secret.SecretMask, secretValue[:3]) ||
		!strings.HasSuffix(secret.SecretMask, secretValue[len(secretValue)-4:]) {
		t.Fatalf("mask = %q, want the first 3 and last 4 characters only", secret.SecretMask)
	}

	// The non-secret items DO carry their values.
	if got := byKey["model"].Value; got != "deepseek-chat" {
		t.Fatalf("non-secret item value = %q, want the stored value", got)
	}
}

// installDemoSecret installs a plugin declaring one text item, one secret and one
// already-stored secret value.
func installDemoSecret(t *testing.T, m *Market, secret string) string {
	t.Helper()
	const id = "com.example.secretive"
	data, sum := makeZip(t, map[string]string{
		"manifest.json": testManifest(t, id, map[string]any{
			"config": []map[string]any{
				{"key": "model", "label": "模型", "type": "text", "default": "deepseek-chat"},
				{"key": "apiKey", "label": "API Key", "type": "secret", "secret": true},
			},
		}),
		"panel.html": "<!doctype html><p>hi</p>",
	})
	base := serveCatalogue(t, indexBody(catalogueRow(id, serveZip(t, data), sum)))
	m.manager.BaseURL = func() string { return base }
	if err := m.Install(id); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := m.SaveConfig(id, map[string]string{"model": "deepseek-chat", "apiKey": secret}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return id
}

// --- T12: SaveConfig writes atomically and splits secrets out --------------

func TestSaveConfigSplitsSecretsFromValues(t *testing.T) {
	m := newTestMarket(t, "")
	id := installDemoSecret(t, m, "sk-abcdefghijklmnop")

	dir, err := m.manager.PrivateDir(id)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read plugin config: %v", err)
	}
	var parsed struct {
		Values map[string]string `json:"values"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse plugin config: %v\n%s", err, raw)
	}
	if parsed.Values["model"] != "deepseek-chat" {
		t.Fatalf("the non-secret value was not stored: %v", parsed.Values)
	}
	if _, leaked := parsed.Values["apiKey"]; leaked {
		t.Fatal("the secret key is in the plaintext settings file")
	}
	if strings.Contains(string(raw), "sk-abcdefghijklmnop") {
		t.Fatalf("the secret's plaintext is on disk in config.json:\n%s", raw)
	}

	// The secret lives in its own file, encrypted by the keeper.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read private dir: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Name() == "secret-apiKey.bin" {
			found = true
			blob, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read secret file: %v", err)
			}
			if strings.Contains(string(blob), "sk-abcdefghijklmnop") {
				t.Fatal("the secret file stores its plaintext")
			}
		}
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("a staging file was left behind: %s", e.Name())
		}
	}
	if !found {
		t.Fatalf("no secret file was written; directory holds %v", entries)
	}
}

// A secret absent from the map means "leave it alone", not "clear it": the
// editor sends the mask back untouched when the user did not retype it.
func TestSaveConfigKeepsAnOmittedSecret(t *testing.T) {
	m := newTestMarket(t, "")
	id := installDemoSecret(t, m, "sk-keep-me-please")

	if err := m.SaveConfig(id, map[string]string{"model": "other-model"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	cfg, err := m.GetConfig(id)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	for _, it := range cfg.Items {
		if it.Key == "apiKey" && !it.SecretSet {
			t.Fatal("saving the other fields cleared the stored secret")
		}
	}
}

// --- T14: no in-memory caching of the index --------------------------------

func TestInstalledSeesAnotherWritersChange(t *testing.T) {
	m := newTestMarket(t, "")

	first, err := m.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if len(first) != 0 {
		t.Fatalf("a fresh data directory already has %d plugins", len(first))
	}

	// A SECOND manager writes, standing in for the pet (or another window):
	// nothing about the two is shared but the directory.
	other, err := plugins.NewManager(m.dataDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	id := installDemoWith(t, other, "com.example.other")

	second, err := m.Installed()
	if err != nil {
		t.Fatalf("Installed (second): %v", err)
	}
	if len(second) != len(first)+1 {
		t.Fatalf("Installed returned %d rows after another writer added one, want %d",
			len(second), len(first)+1)
	}
	found := false
	for _, row := range second {
		if row.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("the new plugin %s is not in the list %v", id, second)
	}
}

// installDemoWith installs a plugin through an arbitrary manager, so a test can
// act as "the other process".
func installDemoWith(t *testing.T, mgr *plugins.Manager, id string) string {
	t.Helper()
	data, sum := makeZip(t, map[string]string{
		"manifest.json": testManifest(t, id, nil),
		"panel.html":    "<!doctype html><p>hi</p>",
	})
	url := serveZip(t, data)
	entry := plugins.MarketEntry{
		ID:          id,
		Name:        "示例插件",
		Version:     "1.0.0",
		Author:      "tester",
		Description: "用于测试",
		Permissions: []string{"pet.bubble", "storage"},
		Download:    url,
		SHA256:      sum,
	}
	if _, err := mgr.Install(entry); err != nil {
		t.Fatalf("manager install: %v", err)
	}
	return id
}

// --- the built-in sample source --------------------------------------------

// With no address configured, the catalogue comes from the embedded sample and
// installing it works end to end - the offline floor the market promises.
//
// The address has to be cleared EXPLICITLY: a fresh data directory seeds the
// address from the pet's config, or from DefaultMarketBaseURL when the pet has
// none, so "nothing configured" is a state the user reaches by emptying the box,
// not the state a fresh install starts in.
func TestBuiltInSampleSourceInstalls(t *testing.T) {
	m := newTestMarket(t, "")
	m.cfg.MarketBaseURL = ""
	m.manager.BaseURL = func() string { return "" }

	view, err := m.Catalogue()
	if err != nil {
		t.Fatalf("Catalogue: %v", err)
	}
	if len(view.Entries) == 0 {
		t.Fatal("the built-in source has no entries")
	}
	if !view.Fallback {
		t.Fatal("the built-in source is not reported as a fallback")
	}
	if view.Base != "" {
		t.Fatalf("the view reports a base of %q for the built-in source", view.Base)
	}

	id := view.Entries[0].ID
	if err := m.Install(id); err != nil {
		t.Fatalf("install %s from the built-in source: %v", id, err)
	}
	rows, err := m.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("installed rows = %+v, want just %s", rows, id)
	}
	if rows[0].Manifest == nil {
		t.Fatalf("the installed manifest did not load: %+v", rows[0])
	}
}

// --- Uninstall --------------------------------------------------------------

func TestUninstallRemovesFilesIndexAndPrivateData(t *testing.T) {
	m := newTestMarket(t, "")
	id := installDemoSecret(t, m, "sk-abcdefghijklmnop")

	dir, err := m.manager.PrivateDir(id)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}
	if err := m.Uninstall(id, true); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(m.dataDir, "plugins", id)); err == nil {
		t.Fatal("the plugin's files survived the uninstall")
	}
	// The private directory is the part that disappears with the plugin: a
	// secret belonging to an uninstalled plugin must not be left readable on
	// disk for whatever is installed under the same id next.
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("the plugin's private data survived the uninstall")
	}
	rows, err := m.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("the index still lists %d plugins", len(rows))
	}
}

// TestUninstallKeepsPrivateDataUnlessPurged pins the DEFAULT, which is the opposite of what this
// method used to do. Uninstalling is two decisions, not one: the package goes always, and the
// private directory (settings, the stored key, whatever the plugin was in the middle of) goes
// only when the user ticked the box. Anything else makes "卸载" quietly the most destructive
// button in the program, and nothing about the word says "your key is gone".
func TestUninstallKeepsPrivateDataUnlessPurged(t *testing.T) {
	m := newTestMarket(t, "")
	id := installDemoSecret(t, m, "sk-abcdefghijklmnop")

	dir, err := m.manager.PrivateDir(id)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}
	if err := m.Uninstall(id, false); err != nil {
		t.Fatalf("Uninstall(purge=false): %v", err)
	}

	if _, err := os.Stat(filepath.Join(m.dataDir, "plugins", id)); err == nil {
		t.Fatal("the plugin's files survived the uninstall")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("the private data was deleted although the user did not ask for it")
	}
	rows, err := m.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("the index still lists %d plugins", len(rows))
	}
}

// --- the workshop's own capability answer -----------------------------------

// TestPluginHostInfoIsTheWorkshopTable pins that `init.host` cannot promise what the gate refuses.
//
// The workshop is the mount where "does this window serve X" used to be answered by guessing, so
// a list that disagreed with `marketRegistry` would be worse than no list: it would tell a plugin
// to call `llm.profile` here and then refuse it - one WARN line per poll, which is exactly how the
// real machine spent 445 lines on a single plugin.
//
// It also pins the PROGRAM NAME, because that string is how a plugin says "I am in the control
// center" without translating between `surface.slot` vocabulary and a second one.
func TestPluginHostInfoIsTheWorkshopTable(t *testing.T) {
	m := newTestMarket(t, "")
	info := m.PluginHostInfo()
	if info.Program != "workshop" {
		t.Fatalf("Program = %q, want workshop", info.Program)
	}
	table := m.marketRegistry().Methods()
	if len(info.Methods) != len(table) {
		t.Fatalf("the answer names %d methods, the table has %d", len(info.Methods), len(table))
	}
	for i, name := range info.Methods {
		if !m.marketRegistry().Has(name) {
			t.Errorf("%q is advertised but the gate does not accept it", name)
		}
		if i > 0 && info.Methods[i-1] > name {
			t.Errorf("Methods are not sorted at %d: %q after %q", i, name, info.Methods[i-1])
		}
	}
	// The pet-only half must NOT be advertised here: that is the difference the field reports.
	for _, petOnly := range []string{"llm.profile", "llm.providers", "llm.chat", "bubble.say", "plugin.call"} {
		for _, advertised := range info.Methods {
			if advertised == petOnly {
				t.Errorf("%q is advertised in the workshop, where the gate refuses it", petOnly)
			}
		}
	}
}
