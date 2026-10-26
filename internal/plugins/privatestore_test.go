package plugins

// Tests for the private store: the key vocabulary and the six file operations every
// host of a plugin answers with.
//
// They live in this package rather than next to one program because the point of the
// file under test is that the PET and the MARKET cannot disagree about it: the QQ
// plugin's bot writes its sticker library in one process and its workshop reads it in
// the other, so "a key is legal" has to be one fact.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeCall runs one storage method against dir and decodes its JSON result.
func storeCall(t *testing.T, run func(dir string, payload []byte) (any, error), dir, payload string) map[string]any {
	t.Helper()
	raw, err := run(dir, []byte(payload))
	if err != nil {
		t.Fatalf("%s: %v", payload, err)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return out
}

// TestStorageKeyPathIsARelativePathAndNothingElse covers the whole key vocabulary in
// one table, because the interesting cases are the REFUSALS: every spelling of
// "outside" and every name that would mean two paths on two platforms.
func TestStorageKeyPathIsARelativePathAndNothingElse(t *testing.T) {
	ok := map[string]string{
		"log":                   "log",
		"status":                "status",
		"hist_10086":            "hist_10086",
		"state/log":             filepath.Join("state", "log"),
		"stickers/bytes/abcdef": filepath.Join("stickers", "bytes", "abcdef"),
		"a/b/c/d":               filepath.Join("a", "b", "c", "d"),
		strings.Repeat("y", 64): strings.Repeat("y", 64),
	}
	for key, want := range ok {
		got, err := StorageKeyPath(key)
		if err != nil {
			t.Fatalf("StorageKeyPath(%q): %v", key, err)
		}
		if got != want {
			t.Fatalf("StorageKeyPath(%q) = %q, want %q", key, got, want)
		}
	}

	bad := []string{
		"",
		"..",
		".",
		"../x",
		"a/../b",
		"a/./b",
		"/abs",
		"a/",
		"a//b",
		`a\b`, // the Windows separator: two names for one file
		"a b", // a space is not a name
		"with.dot",
		strings.Repeat("y", 65), // one segment over the limit
		"a/b/c/d/e",             // deeper than the limit
		strings.Repeat("y", 60) + "/" + strings.Repeat("y", 60) + "/" +
			strings.Repeat("y", 60) + "/" + strings.Repeat("y", 60), // longer than the limit
	}
	for _, key := range bad {
		if got, err := StorageKeyPath(key); err == nil {
			t.Fatalf("StorageKeyPath(%q) = %q, want a refusal", key, got)
		}
	}
}

// TestStorageSetCreatesTheFoldersItsKeyNames is the property the QQ plugin needs: it
// has no mkdir, so a folder layout is only usable if writing into it creates it.
func TestStorageSetCreatesTheFoldersItsKeyNames(t *testing.T) {
	dir := t.TempDir()

	storeCall(t, StorageSet, dir, `{"key":"stickers/bytes/790ed6cc","value":"blob"}`)
	path := filepath.Join(dir, "stickers", "bytes", "790ed6cc")
	if data, err := os.ReadFile(path); err != nil || string(data) != "blob" {
		t.Fatalf("read back: %v (%q)", err, string(data))
	}

	got := storeCall(t, StorageGet, dir, `{"key":"stickers/bytes/790ed6cc"}`)
	if got["value"] != "blob" {
		t.Fatalf("StorageGet = %v", got["value"])
	}
	// A folder that was never written to is an empty value, not an error.
	if got := storeCall(t, StorageGet, dir, `{"key":"state/log"}`); got["value"] != "" {
		t.Fatalf("a missing value = %v, want empty", got["value"])
	}
	// storeCall already fails the test on error; this asserts the shape a poller reads.
	if got := storeCall(t, StorageStat, dir, `{"key":"state/never"}`); got["exists"] != false {
		t.Fatalf("stat of a missing file = %v", got)
	}
	if got := storeCall(t, StorageStat, dir, `{"key":"stickers/bytes/790ed6cc"}`); got["bytes"] != float64(4) {
		t.Fatalf("stat bytes = %v, want 4", got["bytes"])
	}

	// Removing takes the file, not the folders: a folder that vanished under a
	// running plugin is a worse surprise than an empty one left behind.
	storeCall(t, StorageRemove, dir, `{"key":"stickers/bytes/790ed6cc"}`)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the file survived removal: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dir, "stickers", "bytes")); err != nil || !info.IsDir() {
		t.Fatalf("removal took the folder: %v", err)
	}
	// Removing it twice is not an error either.
	storeCall(t, StorageRemove, dir, `{"key":"stickers/bytes/790ed6cc"}`)
}

// TestStorageNoCallEscapesThePrivateDir is the security half, stated once for every
// method that takes a key: a refused key leaves the disk untouched.
func TestStorageNoCallEscapesThePrivateDir(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Dir(dir)

	calls := map[string]func(dir string, payload []byte) (any, error){
		"get":    StorageGet,
		"set":    StorageSet,
		"remove": StorageRemove,
		"stat":   StorageStat,
	}
	for name, run := range calls {
		for _, key := range []string{"../escape", "/abs", `a\b`, "a/../../b"} {
			payload, _ := json.Marshal(map[string]string{"key": key, "value": "x"})
			if _, err := run(dir, payload); err == nil {
				t.Fatalf("storage.%s accepted the key %q", name, key)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(parent, "escape")); !os.IsNotExist(err) {
		t.Fatalf("a traversal key escaped the private directory: %v", err)
	}
}

// TestStorageDirCreatesOnlyAFolderInsideThePrivateDir covers `sub`: the path a plugin
// hands to a program it starts.
func TestStorageDirCreatesOnlyAFolderInsideThePrivateDir(t *testing.T) {
	dir := t.TempDir()

	got := storeCall(t, StorageDir, dir, "{}")
	if got["dir"] != dir {
		t.Fatalf("StorageDir = %v, want %q", got["dir"], dir)
	}

	got = storeCall(t, StorageDir, dir, `{"sub":"video/tmp"}`)
	want := filepath.Join(dir, "video", "tmp")
	if got["dir"] != want {
		t.Fatalf("StorageDir{sub} = %v, want %q", got["dir"], want)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Fatalf("the sub folder was not created: %v", err)
	}

	for _, sub := range []string{"..", "../x", "/abs", `a\b`, "a//b"} {
		payload, _ := json.Marshal(map[string]string{"sub": sub})
		if _, err := StorageDir(dir, payload); err == nil {
			t.Fatalf("StorageDir accepted sub %q", sub)
		}
	}
}

// TestStorageListShowsWhatIsActuallyThere covers the method that makes "clean up your own
// folder" possible at all: a plugin that keeps hundreds of megabytes needs to be able to
// ask what is in there, because a ledger can lose entries and an unlisted file is a leak.
func TestStorageListShowsWhatIsActuallyThere(t *testing.T) {
	dir := t.TempDir()

	// An empty folder (and one that does not exist yet) is an empty listing, not an error.
	got := storeCall(t, StorageList, dir, "{}")
	if files, _ := got["files"].([]any); len(files) != 0 {
		t.Fatalf("a bare directory listed %v", got["files"])
	}
	got = storeCall(t, StorageList, dir, `{"sub":"media/video"}`)
	if files, _ := got["files"].([]any); len(files) != 0 {
		t.Fatalf("a missing folder listed %v", got["files"])
	}

	storeCall(t, StorageSet, dir, `{"key":"media/video/bilivideoabc","value":"12345"}`)
	storeCall(t, StorageSet, dir, `{"key":"media/video/tmp/biliabcv","value":"12"}`)
	storeCall(t, StorageSet, dir, `{"key":"log","value":"x"}`)

	got = storeCall(t, StorageList, dir, `{"sub":"media/video"}`)
	files, _ := got["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("media/video = %v, want one file", got["files"])
	}
	entry, _ := files[0].(map[string]any)
	if entry["name"] != "bilivideoabc" || entry["bytes"] != float64(5) {
		t.Fatalf("file entry = %v", entry)
	}
	if dirs, _ := got["dirs"].([]any); len(dirs) != 1 || dirs[0] != "tmp" {
		t.Fatalf("dirs = %v, want [tmp]", got["dirs"])
	}

	// The root listing sees the folders, not their contents: it is one level, on purpose.
	got = storeCall(t, StorageList, dir, "{}")
	if dirs, _ := got["dirs"].([]any); len(dirs) != 1 || dirs[0] != "media" {
		t.Fatalf("root dirs = %v, want [media]", got["dirs"])
	}
	if files, _ := got["files"].([]any); len(files) != 1 {
		t.Fatalf("root files = %v, want just log", got["files"])
	}

	// `sub` obeys the key rules, so a listing cannot be aimed outside the directory.
	for _, sub := range []string{"..", "../x", "/abs", `a\b`, "with.dot"} {
		payload, _ := json.Marshal(map[string]string{"sub": sub})
		if _, err := StorageList(dir, payload); err == nil {
			t.Fatalf("StorageList accepted sub %q", sub)
		}
	}
}

// TestStorageMoveIsIdempotent covers the three outcomes a migration depends on.
func TestStorageMoveIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hist_1"), []byte("[]"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	move := func(from, to string) map[string]any {
		payload, _ := json.Marshal(map[string]string{"from": from, "to": to})
		return storeCall(t, StorageMove, dir, string(payload))
	}

	if got := move("hist_1", "history/1"); got["moved"] != true {
		t.Fatalf("first move = %v, want moved", got)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "history", "1")); err != nil || string(data) != "[]" {
		t.Fatalf("target: %v (%q)", err, string(data))
	}
	// Running the same migration again is a no-op, not an error.
	if got := move("hist_1", "history/1"); got["moved"] != false {
		t.Fatalf("second move = %v, want no-op", got)
	}
	// A move onto an existing file reports it and destroys nothing.
	if err := os.WriteFile(filepath.Join(dir, "log"), []byte("new"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := move("log", "history/1")
	if got["moved"] != false || got["targetExists"] != true {
		t.Fatalf("move onto an existing file = %v", got)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "log")); string(data) != "new" {
		t.Fatalf("the source was destroyed: %q", string(data))
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "history", "1")); string(data) != "[]" {
		t.Fatalf("the target was overwritten: %q", string(data))
	}
	// A key equals itself: nothing to do, and no error.
	if got := move("log", "log"); got["moved"] != false {
		t.Fatalf("self move = %v", got)
	}
	// Both ends are keys, so neither can leave the directory.
	for _, pair := range [][2]string{{"../x", "y"}, {"log", "../y"}, {"log", "a//b"}} {
		payload, _ := json.Marshal(map[string]string{"from": pair[0], "to": pair[1]})
		if _, err := StorageMove(dir, payload); err == nil {
			t.Fatalf("move %q -> %q must be refused", pair[0], pair[1])
		}
	}
}

// TestValidStorageSegment pins the flat-name rule the share tray and audio takes use.
func TestValidStorageSegment(t *testing.T) {
	for _, name := range []string{"log", "bili-results", "sticker_790ed6cc", strings.Repeat("y", 64)} {
		if !ValidStorageSegment(name) {
			t.Fatalf("%q must be legal", name)
		}
	}
	for _, name := range []string{"", "a/b", "a.b", "a b", "..", strings.Repeat("y", 65), "表情"} {
		if ValidStorageSegment(name) {
			t.Fatalf("%q must be refused", name)
		}
	}
}
