package plugins

// One-shot migration of the host's todo file into the todo plugin.
//
// The pet used to own the list: <dataDir>/todos.json, read and written by
// internal/store, and reachable by plugins through the `todos.*` capabilities. Both
// halves move here - the data to the plugin's own `storage` (key `todos-v1`), the
// access to `plugin.share` - so without this migration every existing user's list
// would still be on disk and nothing would show it.
//
// The three properties are the voice migration's, and they matter for the same
// reasons:
//
//  1. THE SOURCE IS ONLY EVER READ. <dataDir>/todos.json is left exactly as it is, so
//     a bad migration is recoverable: the file is still there, and deleting the marker
//     tries again.
//  2. THE DATA IS WRITTEN BEFORE ANYTHING IS MARKED. A failure halfway leaves no
//     marker, so the next launch retries instead of shipping an empty list that looks
//     like "your todos are gone".
//  3. A MARKER ENDS IT. Its presence means "this already ran", so the source is not
//     re-read on every launch.
//
// WHY THE SOURCE IS NOT RENAMED (unlike the chat migration, which renames to .bak):
// batch B of the same change deletes the host's todo store outright, so the file's
// fate is decided there rather than here - and until then the pet's own panel still
// reads it. Renaming it now would empty the panel in the same launch that populated
// the plugin, which is a state nobody asked to see.
//
// WHY "ALREADY HAS DATA" ALSO ENDS IT: the plugin writes `todos-v1` on every edit, so a
// non-empty value means the plugin has been in use and its list is the newer one.
// Overwriting it with the host's copy would silently undo every edit made in the plugin
// between installing it and this launch.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TodoPluginID is the plugin this migration populates.
const TodoPluginID = "com.shixiansi.plugin-todo"

// todoStorageKey is the plugin's list slot.
//
// It must match TODOS_KEY in the plugin's app.js: the plugin reads and writes its whole
// list under this one key, so a migration that wrote a different name would populate
// the disk and change nothing the user can see.
const todoStorageKey = "todos-v1"

// hostTodoFile is the host's file, relative to the data directory.
const hostTodoFile = "todos.json"

// legacyTodo is the shape of one entry in <dataDir>/todos.json.
//
// It is declared HERE rather than by importing internal/store because that package's
// Todo type is being deleted in the same change (batch B): a migration that depended on
// it would have to be deleted with it, leaving every existing user's list unmigrated.
// Only the fields this migration carries are declared; unknown fields are ignored.
type legacyTodo struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Notes     string     `json:"notes,omitempty"`
	Due       *time.Time `json:"due,omitempty"`
	Done      bool       `json:"done"`
	CreatedAt time.Time  `json:"createdAt"`
}

// pluginTodo is what the plugin's app.js expects in its array.
//
// A struct rather than a map so the field names are checked by the compiler against the
// JSON the plugin parses: the plugin's `parseTodos` drops any entry without a string
// `id` and `title`, so a typo here would migrate to a list that renders empty.
type pluginTodo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
	Due   string `json:"due"`
	// Notes rides along for the day the plugin renders it; the panel's per-todo note
	// field is the only thing in the host that has no home in the plugin yet, and
	// dropping it at migration time would throw away text the user typed.
	Notes string `json:"notes,omitempty"`
	// CreatedAt keeps the list's order stable: the plugin appends, and a migrated list
	// that lost its timestamps could not be told from one built by hand.
	CreatedAt string `json:"createdAt,omitempty"`
}

// MigrateTodoData copies the host's todo file into the todo plugin's private directory.
//
// It reports no error when there is nothing to do: "the plugin is not installed", "the
// list was never created" and "this already ran" are ordinary states. An error means the
// data was there and could not be copied, and in that case the plugin's directory is left
// without a marker so the next launch retries.
func MigrateTodoData(dataDir string) error {
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("迁移缺少数据目录")
	}
	privateDir, ok := todoPluginOnDisk(dataDir)
	if !ok {
		// Not installed yet. No marker: the next launch looks again, which is what makes
		// "install the plugin, then restart the pet" work.
		return nil
	}
	if markerExists(privateDir) {
		return nil
	}

	// The plugin has been in use already. Its list is the newer one.
	if existing, ok := readPrivateValue(privateDir, todoStorageKey); ok && strings.TrimSpace(existing) != "" {
		return writeTodoMarker(privateDir, "plugin already had data")
	}

	legacy, err := readLegacyTodos(dataDir)
	if err != nil {
		return err
	}
	if len(legacy) == 0 {
		// No host list at all. Nothing to carry, and nothing to mark: the user may still
		// create one in an older build, and marking here would skip it.
		return nil
	}

	out := make([]pluginTodo, 0, len(legacy))
	for _, t := range legacy {
		if t.ID == "" || t.Title == "" {
			// The plugin's parseTodos drops these, so carrying them would only make the
			// count in the marker disagree with what the user sees.
			continue
		}
		due := ""
		if t.Due != nil {
			due = t.Due.Format(time.RFC3339)
		}
		created := ""
		if !t.CreatedAt.IsZero() {
			created = t.CreatedAt.Format(time.RFC3339)
		}
		out = append(out, pluginTodo{
			ID:        t.ID,
			Title:     t.Title,
			Done:      t.Done,
			Due:       due,
			Notes:     t.Notes,
			CreatedAt: created,
		})
	}
	if len(out) == 0 {
		return nil
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("编码待办迁移数据失败：%w", err)
	}
	if err := atomicWriteFile(filepath.Join(privateDir, todoStorageKey), encoded); err != nil {
		return fmt.Errorf("写入待办迁移数据失败：%w", err)
	}

	return writeTodoMarker(privateDir, fmt.Sprintf("%d todos", len(out)))
}

// writeTodoMarker records that the migration ran, and what it carried.
func writeTodoMarker(privateDir, what string) error {
	marker := fmt.Sprintf("%s\n%s\n", time.Now().Format(time.RFC3339), what)
	return atomicWriteFile(filepath.Join(privateDir, migrateMarkerName), []byte(marker))
}

// readPrivateValue reads one storage value the way the `storage` capability would, and
// reports whether a value was there at all.
//
// It goes through StorageKeyPath rather than joining the path by hand: the key becomes a
// file name, so the same validation the capability applies has to apply here or the
// migration would be a way around it.
func readPrivateValue(privateDir, key string) (string, bool) {
	path, err := storagePath(privateDir, key)
	if err != nil {
		return "", false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// readLegacyTodos reads <dataDir>/todos.json.
//
// A file that cannot be parsed is reported rather than skipped: silently producing an
// empty plugin would look exactly like "the migration worked and your todos are gone".
func readLegacyTodos(dataDir string) ([]legacyTodo, error) {
	path := filepath.Join(dataDir, hostTodoFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取待办失败：%w", err)
	}
	var list []legacyTodo
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("解析待办失败：%w", err)
	}
	return list, nil
}

// todoPluginOnDisk finds the todo plugin's installed manifest and prepares its private
// directory.
//
// The directory is CREATED rather than required, for the reason voice_migrate.go spells
// out: `Install` unpacks the package but nothing makes a private directory until
// something first writes to one, so a user who installed the plugin and restarted the pet
// would otherwise get no migration at all - silently.
func todoPluginOnDisk(dataDir string) (string, bool) {
	manifestPath := filepath.Join(dataDir, "plugins", TodoPluginID, ManifestName)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", false
	}
	parsed, err := ParseManifest(raw)
	if err != nil || parsed.ID != TodoPluginID {
		return "", false
	}
	private := filepath.Join(dataDir, "plugin-data", TodoPluginID)
	if err := os.MkdirAll(private, 0o755); err != nil {
		return "", false
	}
	return private, true
}

// TodoMigrateMarkerPath returns where the todo migration's marker lives.
func TodoMigrateMarkerPath(dataDir string) string {
	return filepath.Join(dataDir, "plugin-data", TodoPluginID, migrateMarkerName)
}
