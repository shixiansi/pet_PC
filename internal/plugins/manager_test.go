package plugins

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// --- helpers ---------------------------------------------------------------

// makeZip builds a zip from name->content and returns its bytes plus the
// lowercase hex digest the installer will compare against.
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

// sampleZipFiles is a package that passes every install rule.
func sampleZipFiles() map[string]string {
	man, _ := json.Marshal(map[string]any{
		"id":          "com.example.demo",
		"name":        "演示插件",
		"version":     "1.0.0",
		"author":      "tester",
		"description": "用于测试的插件",
		"permissions": []string{"pet.bubble", "storage"},
		"panel":       true,
		"entry":       "panel.html",
	})
	return map[string]string{
		"manifest.json": string(man),
		"panel.html":    "<!doctype html><p>hi</p>",
	}
}

// localEntry is a market entry whose download is an absolute file:// URL is not
// allowed, so tests serve the bytes from a real httptest server instead.
func writeZipTo(t *testing.T, dir string, files map[string]string) (path, sum string) {
	t.Helper()
	data, sum := makeZip(t, files)
	path = filepath.Join(dir, "pkg.zip")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}
	return path, sum
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// --- index persistence (T8/T9) ---------------------------------------------

// TestIndexRoundTrip pins that every field of an entry survives a save and a
// reload. A round trip that dropped `granted`, for instance, would silently
// revoke the user's authorisations on every restart.
func TestIndexRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	at := time.Now().Round(time.Second)
	entry := IndexEntry{
		ID:          "com.example.demo",
		Version:     "1.2.3",
		Enabled:     true,
		Granted:     []string{"pet.bubble"},
		InstalledAt: at,
	}
	if err := m.saveIndex(Index{Version: 1, Plugins: []IndexEntry{entry}}); err != nil {
		t.Fatalf("saveIndex: %v", err)
	}

	again, err := NewManager(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	idx, err := again.loadIndex()
	if err != nil {
		t.Fatalf("loadIndex: %v", err)
	}
	if len(idx.Plugins) != 1 {
		t.Fatalf("want 1 entry, got %d", len(idx.Plugins))
	}
	got := idx.Plugins[0]
	if got.ID != entry.ID || got.Version != entry.Version || got.Enabled != entry.Enabled {
		t.Fatalf("entry fields changed: %+v", got)
	}
	if !got.InstalledAt.Equal(at) {
		t.Fatalf("installedAt: got %v want %v", got.InstalledAt, at)
	}
	if len(got.Granted) != 1 || got.Granted[0] != "pet.bubble" {
		t.Fatalf("granted: %v", got.Granted)
	}

	// The atomic write must not leave its temp file behind: a stray .tmp-* is
	// how a crashed write becomes a leak the user can never clean up.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// TestCorruptIndexIsQuarantined covers T9. A plugin index that cannot be parsed
// must never stop the pet from starting, so it is set aside and the manager
// carries on with an empty one.
func TestCorruptIndexIsQuarantined(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, indexFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager must not fail on a corrupt index: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, indexFileName+".bak")); err != nil {
		t.Errorf("the bad file was not kept as .bak: %v", err)
	}
	if got := m.Plugins(); len(got) != 0 {
		t.Errorf("want an empty plugin list, got %d", len(got))
	}
	if len(m.Warnings()) == 0 {
		t.Errorf("a quarantined index must be reported as a warning")
	}
}

// --- install (T16-T20) -----------------------------------------------------

// entryFor builds a market entry served from a local file server.
func entryFor(t *testing.T, m *Manager, files map[string]string, mutate func(*MarketEntry)) MarketEntry {
	t.Helper()
	entry, _ := testEntryFromFiles(t, files, mutate)
	return entry
}

// TestInstallAcceptsAGoodPackage is the accepting half of T16/T17: the package
// lands in plugins/<id>/ byte for byte, and the index records it as installed but
// NOT enabled, with nothing granted.
func TestInstallAcceptsAGoodPackage(t *testing.T) {
	m := newTestManager(t)
	entry, files := testEntryFromFiles(t, sampleZipFiles(), nil)
	row, err := m.Install(entry)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if row.ID != entry.ID {
		t.Fatalf("installed id: got %q want %q", row.ID, entry.ID)
	}
	if row.Enabled {
		t.Errorf("a fresh install must not be enabled")
	}
	if len(row.Granted) != 0 {
		t.Errorf("a fresh install must grant nothing, got %v", row.Granted)
	}

	dir, _ := m.PluginDir(entry.ID)
	got, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read installed manifest: %v", err)
	}
	if string(got) != files["manifest.json"] {
		t.Errorf("the installed manifest differs from the one in the package")
	}
	if rows := m.Plugins(); len(rows) != 1 {
		t.Fatalf("want 1 installed plugin, got %d", len(rows))
	}
	// The staging directory is emptied on the way out, success or failure.
	tmpEntries, err := os.ReadDir(m.tmpDir())
	if err != nil {
		t.Fatalf("readdir tmp: %v", err)
	}
	if len(tmpEntries) != 0 {
		t.Errorf("plugins-tmp was not emptied: %v", tmpEntries)
	}
}

// TestExtractZipAcceptsDirectoryEntries covers the branch that used to be dead code.
//
// A zip may carry an entry for a FOLDER - name ends in "/", mode says directory - and every
// common packer writes them: PowerShell's Compress-Archive emits one per folder. The path
// check ran before the "is this a directory" test and refused the trailing slash as an empty
// segment, so a perfectly ordinary package was rejected with 「非法条目路径」 naming a folder
// as if it were a traversal attempt. That is what happened to the QQ-bot package on
// 2026-10-29 (three failed installs, "lib/" in the message).
//
// The files-only packages every earlier test built could not catch it, which is why this one
// constructs the archive by hand instead of going through makeZip.
func TestExtractZipAcceptsDirectoryEntries(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pkg.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	addDir := func(name string) {
		h := &zip.FileHeader{Name: name}
		h.SetMode(os.ModeDir | 0o755)
		if _, err := zw.CreateHeader(h); err != nil {
			t.Fatalf("zip dir %s: %v", name, err)
		}
	}
	addFile := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip file %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	// An empty folder, a folder that also has files, and a nested one: the three shapes a
	// packer writes and the three that failed differently before the fix.
	addDir("lib/")
	addDir("lib/nested/")
	addFile("manifest.json", `{"id":"com.example.demo"}`)
	addFile("lib/core.js", "export const x = 1;\n")
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	dest := filepath.Join(dir, "out")
	if err := extractZip(zipPath, dest); err != nil {
		t.Fatalf("a package with directory entries must install: %v", err)
	}
	for _, folder := range []string{"lib", filepath.Join("lib", "nested")} {
		info, err := os.Stat(filepath.Join(dest, folder))
		if err != nil {
			t.Fatalf("folder %s: %v", folder, err)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", folder)
		}
	}
	got, err := os.ReadFile(filepath.Join(dest, "lib", "core.js"))
	if err != nil {
		t.Fatalf("read lib/core.js: %v", err)
	}
	if string(got) != "export const x = 1;\n" {
		t.Errorf("lib/core.js content = %q", got)
	}
}

// TestExtractZipStillRefusesTraversalAfterTrimmingTheSlash keeps the fix from becoming a hole:
// the trim applies to entries the READER calls a directory, so "../" and an absolute path are
// still refused. (There is no "file whose name ends in /" to worry about: the zip format defines
// a trailing slash as the directory marker, so Go's reader reports IsDir() for it - which is
// exactly the signal the trim keys off.)
func TestExtractZipStillRefusesTraversalAfterTrimmingTheSlash(t *testing.T) {
	cases := []struct{ label, name string }{
		{"parent dir entry", "../"},
		{"nested escape", "lib/../../"},
		{"absolute dir", "/etc/"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			dir := t.TempDir()
			zipPath := filepath.Join(dir, "pkg.zip")
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			h := &zip.FileHeader{Name: tc.name}
			h.SetMode(os.ModeDir | 0o755)
			if _, err := zw.CreateHeader(h); err != nil {
				t.Fatalf("zip dir: %v", err)
			}
			if err := zw.Close(); err != nil {
				t.Fatalf("zip close: %v", err)
			}
			if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
				t.Fatalf("write zip: %v", err)
			}
			err := extractZip(zipPath, filepath.Join(dir, "out"))
			if err == nil {
				t.Fatalf("%s (%q) must be refused", tc.label, tc.name)
			}
			if !strings.Contains(err.Error(), "非法条目路径") {
				t.Fatalf("error %q must name the illegal entry", err)
			}
		})
	}
}

// TestInstallRejectsBadDigest covers T16's refusing half, including the part
// that matters most: the installed set is unchanged and nothing was left behind.
func TestInstallRejectsBadDigest(t *testing.T) {
	m := newTestManager(t)
	before := m.Plugins()
	entry, _ := testEntryFromFiles(t, sampleZipFiles(), func(e *MarketEntry) {
		// Flip one hex character, so the digest is still well-formed.
		last := e.SHA256[len(e.SHA256)-1]
		replacement := byte('0')
		if last == '0' {
			replacement = '1'
		}
		e.SHA256 = e.SHA256[:len(e.SHA256)-1] + string(replacement)
	})
	_, err := m.Install(entry)
	if err == nil {
		t.Fatal("a mismatched digest must be refused")
	}
	if !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("error %q must name sha256", err)
	}
	after := m.Plugins()
	if len(after) != len(before) {
		t.Fatalf("the installed set changed: %d -> %d", len(before), len(after))
	}
	dir, _ := m.PluginDir(entry.ID)
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Error("a refused install must not create the plugin directory")
	}
	tmpEntries, _ := os.ReadDir(m.tmpDir())
	if len(tmpEntries) != 0 {
		t.Errorf("plugins-tmp was not emptied after a failure: %v", tmpEntries)
	}
}

// TestInstallRejectsMalformedDigest keeps a catalogue from smuggling in a hash
// that is not the shape it claims to be.
func TestInstallRejectsMalformedDigest(t *testing.T) {
	m := newTestManager(t)
	entry, _ := testEntryFromFiles(t, sampleZipFiles(), func(e *MarketEntry) {
		e.SHA256 = "NOTAHASH"
	})
	_, err := m.Install(entry)
	if err == nil {
		t.Fatal("a malformed sha256 must be refused")
	}
	if !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("error %q must name sha256", err)
	}
}

// TestInstallRejectsTraversal covers T18. Each archive contains exactly one
// malicious entry, so a pass here cannot be an accident of another entry being
// rejected first.
func TestInstallRejectsTraversal(t *testing.T) {
	cases := []string{"../escape.txt", "/abs.txt", "a/../../escape.txt"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			m := newTestManager(t)
			files := sampleZipFiles()
			files[name] = "pwned"
			before := m.Plugins()
			entry, _ := testEntryFromFiles(t, files, nil)
			_, err := m.Install(entry)
			if err == nil {
				t.Fatalf("an archive containing %q must be refused", name)
			}
			if !strings.Contains(err.Error(), "路径") && !strings.Contains(err.Error(), "非法条目") {
				t.Fatalf("error %q must explain the path problem", err)
			}
			if _, statErr := os.Stat(filepath.Join(m.DataDir(), "escape.txt")); statErr == nil {
				t.Error("a file escaped the data directory")
			}
			if _, statErr := os.Stat(filepath.Join(m.DataDir(), "abs.txt")); statErr == nil {
				t.Error("an absolute entry landed in the data directory")
			}
			subdirs, _ := os.ReadDir(m.pluginsDir())
			if len(subdirs) != 0 {
				t.Errorf("a refused install created a plugin directory: %v", subdirs)
			}
			if after := m.Plugins(); len(after) != len(before) {
				t.Errorf("the installed set changed")
			}
		})
	}
}

// TestInstallRejectsOversizedEntry covers T19. The entry's DECLARED size is over
// the cap, so the check fires before anything is written.
func TestInstallRejectsOversizedEntry(t *testing.T) {
	m := newTestManager(t)
	files := sampleZipFiles()
	files["big.bin"] = strings.Repeat("x", 1024)
	data, sum := makeZip(t, files)
	// Rewrite the central directory's uncompressed size to claim more than the
	// cap, which is what a hostile (or broken) producer would do.
	patched := patchZipSize(t, data, "big.bin", maxEntrySize+1)
	path := filepath.Join(t.TempDir(), "big.zip")
	if err := os.WriteFile(path, patched, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = sum
	entry := MarketEntry{
		ID:          "com.example.demo",
		Version:     "1.0.0",
		Permissions: []string{"pet.bubble", "storage"},
		Download:    fileURL(path),
		SHA256:      digestOf(patched),
	}
	_, err := m.Install(entry)
	if err == nil {
		t.Fatal("an oversized entry must be refused")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Fatalf("error %q must name the limit", err)
	}
	dir, _ := m.PluginDir(entry.ID)
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Error("a refused install must not create the plugin directory")
	}
}

// TestInstallRejectsPermissionMismatch covers T20: what the user was shown must
// be what the package asks for.
func TestInstallRejectsPermissionMismatch(t *testing.T) {
	m := newTestManager(t)
	before := m.Plugins()
	entry, _ := testEntryFromFiles(t, sampleZipFiles(), func(e *MarketEntry) {
		e.Permissions = []string{"pet.bubble", "storage", "llm.chat"}
	})
	_, err := m.Install(entry)
	if err == nil {
		t.Fatal("a permission mismatch must be refused")
	}
	if !strings.Contains(err.Error(), "权限声明不一致") {
		t.Fatalf("error %q must explain the mismatch", err)
	}
	if after := m.Plugins(); len(after) != len(before) {
		t.Error("the installed set changed")
	}
	dir, _ := m.PluginDir(entry.ID)
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Error("a refused install must not create the plugin directory")
	}
}

// TestInstallRefusesWhenAlreadyInstalled keeps the second install from silently
// replacing the first, which would swap a plugin's code under a granted set the
// user approved for different code.
func TestInstallRefusesWhenAlreadyInstalled(t *testing.T) {
	m := newTestManager(t)
	entry, _ := testEntryFromFiles(t, sampleZipFiles(), nil)
	if _, err := m.Install(entry); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if _, err := m.Install(entry); err == nil {
		t.Fatal("a second install must be refused")
	}
}

// --- lifecycle (T21-T25) ---------------------------------------------------

// TestSetEnabledPersists covers T21.
func TestSetEnabledPersists(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	entry, _ := testEntryFromFiles(t, sampleZipFiles(), nil)
	if _, err := m.Install(entry); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := m.GrantPermissions(entry.ID, []string{"pet.bubble"}); err != nil {
		t.Fatalf("GrantPermissions: %v", err)
	}
	if err := m.SetEnabled(entry.ID, true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	again, err := NewManager(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	row, ok := again.Get(entry.ID)
	if !ok {
		t.Fatal("the plugin disappeared")
	}
	if !row.Enabled {
		t.Error("enabled did not persist")
	}
	// Enabling is not granting: the granted set must survive untouched.
	if len(row.Granted) != 1 || row.Granted[0] != "pet.bubble" {
		t.Errorf("enabling changed the granted set: %v", row.Granted)
	}
}

// TestSetEnabledUnknownID covers T21's refusal path.
func TestSetEnabledUnknownID(t *testing.T) {
	m := newTestManager(t)
	before := m.Plugins()
	if err := m.SetEnabled("com.example.nope", true); err == nil {
		t.Fatal("an unknown id must be refused")
	} else if !strings.Contains(err.Error(), "未安装") {
		t.Fatalf("error %q must say the plugin is not installed", err)
	}
	if after := m.Plugins(); len(after) != len(before) {
		t.Error("the installed set changed")
	}
}

// TestGrantPermissionsPersists covers T22: the granted subset and the pending
// remainder are both correct, and the grant survives a reload.
func TestGrantPermissionsPersists(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	files := sampleZipFiles()
	// A manifest declaring exactly two permissions, so pending has one entry.
	man, _ := json.Marshal(map[string]any{
		"id": "com.example.demo", "name": "演示", "version": "1.0.0",
		"author": "t", "description": "d",
		"permissions": []string{"llm.chat", "pet.bubble"},
		"panel":       true, "entry": "panel.html",
	})
	files["manifest.json"] = string(man)
	entry, _ := testEntryFromFiles(t, files, func(e *MarketEntry) {
		e.Permissions = []string{"llm.chat", "pet.bubble"}
	})
	row, err := m.Install(entry)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if row.Manifest == nil {
		t.Fatal("the installed row has no manifest")
	}

	got, err := m.GrantPermissions(entry.ID, []string{"llm.chat"})
	if err != nil {
		t.Fatalf("GrantPermissions: %v", err)
	}
	if len(got.Granted) != 1 || got.Granted[0] != "llm.chat" {
		t.Fatalf("granted: %v", got.Granted)
	}
	pending := Pending(*got.Manifest, got.Granted)
	if len(pending) != 1 || pending[0] != "pet.bubble" {
		t.Fatalf("pending: %v", pending)
	}

	again, err := NewManager(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	reloaded, _ := again.Get(entry.ID)
	if len(reloaded.Granted) != 1 || reloaded.Granted[0] != "llm.chat" {
		t.Fatalf("the grant did not persist: %v", reloaded.Granted)
	}
}

// TestGrantUndeclaredPermission covers T23.
func TestGrantUndeclaredPermission(t *testing.T) {
	m := newTestManager(t)
	entry, _ := testEntryFromFiles(t, sampleZipFiles(), nil)
	if _, err := m.Install(entry); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := m.GrantPermissions(entry.ID, []string{"pet.bubble"}); err != nil {
		t.Fatalf("GrantPermissions: %v", err)
	}
	before, _ := m.Get(entry.ID)
	if _, err := m.GrantPermissions(entry.ID, []string{"net.fetch"}); err == nil {
		t.Fatal("an undeclared permission must be refused")
	}
	after, _ := m.Get(entry.ID)
	if len(after.Granted) != len(before.Granted) {
		t.Fatalf("a refused grant changed the granted set: %v -> %v", before.Granted, after.Granted)
	}
}

// TestPluginsReportsBrokenManifestPerRow covers T24: a row whose manifest no
// longer parses is reported ON THAT ROW, and the good rows are unaffected.
func TestPluginsReportsBrokenManifestPerRow(t *testing.T) {
	m := newTestManager(t)
	good, _ := testEntryFromFiles(t, sampleZipFiles(), nil)
	if _, err := m.Install(good); err != nil {
		t.Fatalf("Install good: %v", err)
	}

	// Hand-place a second plugin whose manifest is an empty object.
	badDir, err := m.PluginDir("com.example.broken")
	if err != nil {
		t.Fatalf("PluginDir: %v", err)
	}
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(badDir, ManifestName), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	idx, _ := m.loadIndex()
	idx.Plugins = append(idx.Plugins, IndexEntry{ID: "com.example.broken", Version: "1.0.0", Enabled: true})
	if err := m.saveIndex(idx); err != nil {
		t.Fatalf("saveIndex: %v", err)
	}

	rows := m.Plugins()
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	var sawGood, sawBad bool
	for _, r := range rows {
		switch r.ID {
		case good.ID:
			sawGood = true
			if r.Error != "" {
				t.Errorf("the good row reports an error: %s", r.Error)
			}
			if r.Manifest == nil {
				t.Error("the good row has no manifest")
			}
		case "com.example.broken":
			sawBad = true
			if r.Error == "" {
				t.Error("the broken row must report why it cannot be loaded")
			}
			if r.Enabled {
				t.Error("a row whose manifest cannot be read must not stay enabled")
			}
		}
	}
	if !sawGood || !sawBad {
		t.Fatalf("both rows must be present: good=%v bad=%v", sawGood, sawBad)
	}
}

// TestUninstallRemovesEverything covers T25, including the private storage.
func TestUninstallRemovesEverything(t *testing.T) {
	m := newTestManager(t)
	entry, _ := testEntryFromFiles(t, sampleZipFiles(), nil)
	if _, err := m.Install(entry); err != nil {
		t.Fatalf("Install: %v", err)
	}
	private, err := m.PrivateDir(entry.ID)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(private, "k1"), []byte("v1"), 0o644); err != nil {
		t.Fatalf("write private: %v", err)
	}

	if err := m.Uninstall(entry.ID, true); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	dir, _ := m.PluginDir(entry.ID)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the plugin directory survived: %v", err)
	}
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Errorf("the private storage survived: %v", err)
	}
	for _, r := range m.Plugins() {
		if r.ID == entry.ID {
			t.Error("the uninstalled plugin is still in the index")
		}
	}

	before := m.Plugins()
	if err := m.Uninstall("com.example.nope", true); err == nil {
		t.Error("uninstalling an unknown id must be refused")
	}
	if after := m.Plugins(); len(after) != len(before) {
		t.Error("a refused uninstall changed the index")
	}
}

// TestUninstallKeepsDataUnlessAsked pins the default. Removing the package is a ROLLBACK —
// install the same version again and it picks up where it left off — while deleting
// plugin-data/<id> destroys settings and stored secrets that cannot be regenerated. So the
// data goes only when the caller asked, and this test is what stops "卸载" from quietly
// becoming the most destructive button in the program again.
func TestUninstallKeepsDataUnlessAsked(t *testing.T) {
	m := newTestManager(t)
	entry, _ := testEntryFromFiles(t, sampleZipFiles(), nil)
	if _, err := m.Install(entry); err != nil {
		t.Fatalf("Install: %v", err)
	}
	private, err := m.PrivateDir(entry.ID)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}
	keep := filepath.Join(private, "k1")
	if err := os.WriteFile(keep, []byte("v1"), 0o644); err != nil {
		t.Fatalf("write private: %v", err)
	}

	if err := m.Uninstall(entry.ID, false); err != nil {
		t.Fatalf("Uninstall(purge=false): %v", err)
	}
	dir, _ := m.PluginDir(entry.ID)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the plugin directory survived: %v", err)
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "v1" {
		t.Errorf("the private data was deleted although nobody asked: %v", err)
	}
	for _, r := range m.Plugins() {
		if r.ID == entry.ID {
			t.Error("the uninstalled plugin is still in the index")
		}
	}

	// And reinstalling finds the data still there: that is the whole point of keeping it.
	if _, err := m.Install(entry); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "v1" {
		t.Errorf("the data did not survive a reinstall: %v", err)
	}
}

// package has to otherwise be a valid zip the reader will parse.
func patchZipSize(t *testing.T, data []byte, name string, size uint64) []byte {
	t.Helper()
	return patchZipEntrySize(t, data, name, size)
}

// digestOf is the lowercase hex sha256 of raw bytes.
func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// --- the contended rename ---------------------------------------------------
//
// `plugins.json` has two writer programs and one reader: the control center saves
// grants, the pet re-reads the file every 1.5s. On Windows a rename over a file
// somebody has open fails with ERROR_ACCESS_DENIED - the real log line was
// "保存授权失败：rename …plugins.json: Access is denied" - so the atomic write
// retries. These tests pin the retry, its bound, and the fact that it is wired
// into saveIndex rather than only into the helper.

// withRenameStub swaps the package's rename for the duration of one test.
func withRenameStub(t *testing.T, fn func(from, to string) error) *int {
	t.Helper()
	orig := renameFile
	calls := 0
	renameFile = func(from, to string) error {
		calls++
		return fn(from, to)
	}
	t.Cleanup(func() { renameFile = orig })
	return &calls
}

// sharingViolation is what Windows reports for "that file is open elsewhere".
func sharingViolation(from, to string) error {
	return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.Errno(32)}
}

func TestRenameWithRetrySurvivesASharingViolation(t *testing.T) {
	attempts := 0
	withRenameStub(t, func(from, to string) error {
		// Fail twice, then let it through: a reader's handle is microseconds long.
		attempts++
		if attempts < 3 {
			return sharingViolation(from, to)
		}
		return nil
	})
	if err := renameWithRetry("a", "b"); err != nil {
		t.Fatalf("renameWithRetry: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (two violations, then success)", attempts)
	}
}

func TestRenameWithRetryGivesUpAfterTheBound(t *testing.T) {
	attempts := 0
	withRenameStub(t, func(from, to string) error {
		attempts++
		return sharingViolation(from, to)
	})
	err := renameWithRetry("a", "b")
	if err == nil {
		t.Fatal("a rename that never succeeds has to report the failure")
	}
	if attempts != 8 {
		t.Fatalf("attempts = %d, want the documented bound of 8", attempts)
	}
}

func TestRenameWithRetryDoesNotRetryOtherErrors(t *testing.T) {
	attempts := 0
	withRenameStub(t, func(from, to string) error {
		attempts++
		return os.ErrNotExist
	})
	if err := renameWithRetry("a", "b"); err == nil {
		t.Fatal("want the original error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1: a missing file must not look like contention", attempts)
	}
}

// TestSaveIndexRetriesAContendedRename proves the retry is in the SAVE path, not just in
// the helper: a grant saved while the pet holds the file open must still land.
func TestSaveIndexRetriesAContendedRename(t *testing.T) {
	m := newTestManager(t)
	entry, _ := testEntryFromFiles(t, sampleZipFiles(), nil)
	if _, err := m.Install(entry); err != nil {
		t.Fatalf("Install: %v", err)
	}
	id := entry.ID
	attempts := 0
	withRenameStub(t, func(from, to string) error {
		attempts++
		if attempts == 1 {
			return sharingViolation(from, to)
		}
		return os.Rename(from, to)
	})
	if err := m.SetEnabled(id, true); err != nil {
		t.Fatalf("SetEnabled with one contended rename: %v", err)
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want the save to have retried", attempts)
	}
	// And the write really landed.
	row, ok := m.Get(id)
	if !ok || !row.Enabled {
		t.Fatalf("row = %+v (ok=%v), want enabled", row, ok)
	}
}
