package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// installedTodoPlugin writes a manifest for the todo plugin, which is what
// MigrateTodoData looks for before doing anything.
func installedTodoPlugin(t *testing.T, dataDir string) {
	t.Helper()
	dir := filepath.Join(dataDir, "plugins", TodoPluginID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	man := `{"id":"` + TodoPluginID + `","name":"待办插件","version":"1.1.0","author":"t",
		"description":"d","permissions":["storage"],"panel":true,"entry":"panel.html"}`
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(man), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// hostTodos writes the pet's old todos.json in the store's shape.
func hostTodos(t *testing.T, dataDir string) {
	t.Helper()
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	due := now.Add(2 * time.Hour)
	list := []map[string]any{
		{"id": "t1", "title": "写周报", "done": false, "createdAt": now, "due": due},
		{"id": "t2", "title": "整理纸箱", "done": true, "createdAt": now, "notes": "客厅那堆"},
		{"id": "", "title": "没有 id 的一条", "done": false, "createdAt": now},
	}
	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "todos.json"), raw, 0o644); err != nil {
		t.Fatalf("write todos.json: %v", err)
	}
}

func readMigrated(t *testing.T, dataDir string) []pluginTodo {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dataDir, "plugin-data", TodoPluginID, todoStorageKey))
	if err != nil {
		t.Fatalf("read migrated list: %v", err)
	}
	var out []pluginTodo
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode migrated list: %v", err)
	}
	return out
}

// TestTodoMigrationCarriesTheList is the whole point: the entries the user already had
// must appear in the plugin's storage, in the shape its app.js parses, and the host's
// file must be left alone (batch B decides its fate; renaming it here would empty the
// panel in the same launch that filled the plugin).
func TestTodoMigrationCarriesTheList(t *testing.T) {
	dataDir := t.TempDir()
	installedTodoPlugin(t, dataDir)
	hostTodos(t, dataDir)

	if err := MigrateTodoData(dataDir); err != nil {
		t.Fatalf("MigrateTodoData: %v", err)
	}

	got := readMigrated(t, dataDir)
	if len(got) != 2 {
		t.Fatalf("migrated %d entries, want 2 (the one without an id is dropped)", len(got))
	}
	if got[0].ID != "t1" || got[0].Title != "写周报" || got[0].Done {
		t.Errorf("entry 0 = %+v", got[0])
	}
	if got[0].Due == "" {
		t.Error("the due date was dropped, so the plugin cannot show it")
	}
	if got[1].ID != "t2" || !got[1].Done || got[1].Notes != "客厅那堆" {
		t.Errorf("entry 1 = %+v", got[1])
	}

	// The source is still there, byte for byte.
	if _, err := os.Stat(filepath.Join(dataDir, "todos.json")); err != nil {
		t.Errorf("todos.json was moved or removed: %v", err)
	}

	if _, err := os.Stat(TodoMigrateMarkerPath(dataDir)); err != nil {
		t.Errorf("no marker was written: %v", err)
	}
}

// TestTodoMigrationRunsOnce pins the marker: a second launch must not overwrite what the
// plugin has done since.
func TestTodoMigrationRunsOnce(t *testing.T) {
	dataDir := t.TempDir()
	installedTodoPlugin(t, dataDir)
	hostTodos(t, dataDir)

	if err := MigrateTodoData(dataDir); err != nil {
		t.Fatalf("first: %v", err)
	}
	// The plugin edits its list (adds one, drops one).
	edited := []pluginTodo{{ID: "only", Title: "插件里加的"}}
	raw, _ := json.Marshal(edited)
	path := filepath.Join(dataDir, "plugin-data", TodoPluginID, todoStorageKey)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write edited list: %v", err)
	}

	if err := MigrateTodoData(dataDir); err != nil {
		t.Fatalf("second: %v", err)
	}
	got := readMigrated(t, dataDir)
	if len(got) != 1 || got[0].ID != "only" {
		t.Fatalf("the second run overwrote the plugin's own list: %+v", got)
	}
}

// TestTodoMigrationRespectsAPluginThatAlreadyHasData covers the upgrade path WITHOUT a
// marker: the user installed 1.1.0 by hand, used it, and only then restarted. The list in
// the plugin is the newer one, so the host's copy must not win.
func TestTodoMigrationRespectsAPluginThatAlreadyHasData(t *testing.T) {
	dataDir := t.TempDir()
	installedTodoPlugin(t, dataDir)
	hostTodos(t, dataDir)

	dir := filepath.Join(dataDir, "plugin-data", TodoPluginID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw, _ := json.Marshal([]pluginTodo{{ID: "newer", Title: "安装后加的"}})
	if err := os.WriteFile(filepath.Join(dir, todoStorageKey), raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := MigrateTodoData(dataDir); err != nil {
		t.Fatalf("MigrateTodoData: %v", err)
	}
	if got := readMigrated(t, dataDir); len(got) != 1 || got[0].ID != "newer" {
		t.Fatalf("the host's copy overwrote the plugin's own list: %+v", got)
	}
	// A marker is still written: there is nothing left to do on the next launch either.
	if _, err := os.Stat(TodoMigrateMarkerPath(dataDir)); err != nil {
		t.Errorf("no marker: %v", err)
	}
}

// TestTodoMigrationIsANoOpWithoutThePlugin pins the ordinary case: a machine that never
// installed the todo plugin must not get a directory, a marker or an error.
func TestTodoMigrationIsANoOpWithoutThePlugin(t *testing.T) {
	dataDir := t.TempDir()
	hostTodos(t, dataDir)

	if err := MigrateTodoData(dataDir); err != nil {
		t.Fatalf("MigrateTodoData: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "plugin-data", TodoPluginID)); !os.IsNotExist(err) {
		t.Errorf("a private directory was created for a plugin that is not installed (err=%v)", err)
	}
}

// TestTodoMigrationReportsACorruptFile: silently producing an empty plugin would look
// exactly like "the migration worked and your todos are gone", so a file that cannot be
// parsed is an error and leaves no marker (the next launch retries).
func TestTodoMigrationReportsACorruptFile(t *testing.T) {
	dataDir := t.TempDir()
	installedTodoPlugin(t, dataDir)
	if err := os.WriteFile(filepath.Join(dataDir, "todos.json"), []byte(`{"not":"an array"`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := MigrateTodoData(dataDir); err == nil {
		t.Fatal("a corrupt todos.json must be reported")
	}
	if _, err := os.Stat(TodoMigrateMarkerPath(dataDir)); !os.IsNotExist(err) {
		t.Error("a marker was written after a failed migration, so the next launch would skip it")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "plugin-data", TodoPluginID, todoStorageKey)); !os.IsNotExist(err) {
		t.Error("a half-written list was left behind")
	}
}
