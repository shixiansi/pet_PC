package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The UPDATE path.
//
// It was added because there was no way to move an installed plugin forward: Install
// refuses an id that is already present, and the market hides its install button once
// a plugin is installed. The only route was uninstall-then-install - and Uninstall
// deletes `plugin-data/<id>/`, which is where a plugin keeps its own settings and
// API key. These tests pin the two halves of what makes Update usable: it keeps the
// plugin's enabled state and grants, and it keeps the private directory.

// updateFixture installs one plugin at a version and returns the manager, its id and
// a function to publish a newer package.
func updateFixture(t *testing.T, from, to string) (*Manager, string, func() MarketEntry) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// The two packages differ in their manifest version and in one file's contents,
	// so "which one is on disk" is checkable from the unpacked files.
	oldFiles := map[string]string{
		"manifest.json": testManifestFor(t, "com.example.up", from),
		"panel.html":    "<!doctype html><p>old</p>",
	}
	oldEntry, _ := testEntryFromFiles(t, oldFiles, func(e *MarketEntry) {
		e.ID = "com.example.up"
		e.Version = from
	})
	if _, err := m.Install(oldEntry); err != nil {
		t.Fatalf("install %s: %v", from, err)
	}

	return m, "com.example.up", func() MarketEntry {
		newFiles := map[string]string{
			"manifest.json": testManifestFor(t, "com.example.up", to),
			"panel.html":    "<!doctype html><p>new</p>",
		}
		entry, _ := testEntryFromFiles(t, newFiles, func(e *MarketEntry) {
			e.ID = "com.example.up"
			e.Version = to
		})
		return entry
	}
}

func TestUpdateKeepsEnabledStateAndGrants(t *testing.T) {
	m, id, newer := updateFixture(t, "1.0.0", "1.1.0")

	// Make the installed plugin look like one the user has been running: enabled,
	// with a permission granted.
	if err := m.SetEnabled(id, true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if _, err := m.GrantPermissions(id, []string{"pet.bubble"}); err != nil {
		t.Fatalf("GrantPermissions: %v", err)
	}

	row, err := m.Update(newer())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if row.Version != "1.1.0" {
		t.Fatalf("version after update = %q, want 1.1.0", row.Version)
	}
	// The two things an update must NOT reset. Resetting `enabled` would switch off a
	// plugin the user was using; resetting the grants would re-prompt for permissions
	// they already accepted, which is worse than not offering updates at all.
	if !row.Enabled {
		t.Fatal("the update switched the plugin off")
	}
	if len(row.Granted) != 1 || row.Granted[0] != "pet.bubble" {
		t.Fatalf("granted after update = %v, want the previous grant preserved", row.Granted)
	}

	// The FILES really were replaced, not just the index.
	dir, err := m.PluginDir(id)
	if err != nil {
		t.Fatalf("PluginDir: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "panel.html"))
	if err != nil {
		t.Fatalf("read panel.html: %v", err)
	}
	if !strings.Contains(string(body), "new") {
		t.Fatalf("panel.html still holds the old package's contents: %s", body)
	}

	// And the index has exactly ONE row for the id. Appending instead of replacing
	// would list the plugin twice, and the slot resolver - which takes the first
	// match - could hand back the OLD version's row.
	rows := m.Plugins()
	if len(rows) != 1 {
		t.Fatalf("the index has %d rows after an update, want 1", len(rows))
	}
}

func TestUpdateKeepsThePrivateDirectory(t *testing.T) {
	m, id, newer := updateFixture(t, "1.0.0", "1.1.0")

	// A plugin's private data - its settings and its API key - lives here. This is
	// the whole reason Update exists: uninstall-then-install would delete it.
	private, err := m.PrivateDir(id)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}
	sentinel := filepath.Join(private, "config.json")
	if err := os.WriteFile(sentinel, []byte(`{"values":{"model":"keep-me"}}`), 0o644); err != nil {
		t.Fatalf("write private data: %v", err)
	}

	if _, err := m.Update(newer()); err != nil {
		t.Fatalf("Update: %v", err)
	}

	body, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("the plugin's private data did not survive the update: %v", err)
	}
	if !strings.Contains(string(body), "keep-me") {
		t.Fatalf("the private data was rewritten: %s", body)
	}
}

func TestUpdateRefusesToGoBackwards(t *testing.T) {
	m, _, same := updateFixture(t, "1.1.0", "1.1.0")

	// Same version: refused, so a catalogue that merely lists what is installed does
	// not present an endless "update" that does nothing.
	if _, err := m.Update(same()); err == nil {
		t.Fatal("updating to the same version succeeded")
	}

	// Strictly older: refused, so a stale mirror cannot silently downgrade a working
	// plugin and reintroduce the bug the newer version fixed.
	older, _ := testEntryFromFiles(t, map[string]string{
		"manifest.json": testManifestFor(t, "com.example.up", "1.0.0"),
		"panel.html":    "<!doctype html><p>older</p>",
	}, func(e *MarketEntry) {
		e.ID = "com.example.up"
		e.Version = "1.0.0"
	})
	if _, err := m.Update(older); err == nil {
		t.Fatal("updating to an older version succeeded")
	}
}

func TestUpdateRequiresAnExistingInstall(t *testing.T) {
	m, _, newer := updateFixture(t, "1.0.0", "1.1.0")
	id := "com.example.up"
	if err := m.Uninstall(id); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	// With nothing installed, Update must refuse rather than quietly install - the
	// two verbs mean different things to the user's state, and conflating them would
	// make "更新" enable something the user never authorised.
	if _, err := m.Update(newer()); err == nil {
		t.Fatal("updating a plugin that is not installed succeeded")
	}
}

// testManifestFor is a manifest JSON document with a chosen id and version.
func testManifestFor(t *testing.T, id, version string) string {
	t.Helper()
	return `{
  "id": "` + id + `",
  "name": "升级测试插件",
  "version": "` + version + `",
  "author": "tester",
  "description": "用于测试更新路径",
  "permissions": ["pet.bubble", "storage"],
  "panel": true,
  "entry": "panel.html"
}`
}
