package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// --- T26/T27: scheme checks ------------------------------------------------

// TestMarketRejectsNonHTTPScheme covers T26. A file:// or ftp:// address is a
// value the user typed, so it is REFUSED rather than treated as unreachable -
// silently falling back would hide the typo behind a list that looks fine.
func TestMarketRejectsNonHTTPScheme(t *testing.T) {
	for _, bad := range []string{"file:///C:/x", "ftp://h/x"} {
		m := newTestManager(t)
		m.BaseURL = func() string { return bad }
		if _, err := m.Market(); err == nil {
			t.Fatalf("%s must be refused", bad)
		} else if !strings.Contains(err.Error(), "http/https") {
			t.Fatalf("error %q must name the supported schemes", err)
		}
	}
}

// TestCheckDownloadScheme covers T27's rules on the package address.
func TestCheckDownloadScheme(t *testing.T) {
	if err := checkDownloadScheme("dist/x.zip"); err != nil {
		t.Errorf("a relative path must be accepted: %v", err)
	}
	if err := checkDownloadScheme("https://h/x.zip"); err != nil {
		t.Errorf("an https URL must be accepted: %v", err)
	}
	if err := checkDownloadScheme("file:///x.zip"); err == nil {
		t.Error("a file:// download must be refused")
	}
	if err := checkDownloadScheme("ftp://h/x.zip"); err == nil {
		t.Error("an ftp download must be refused")
	}
}

// TestInstallRejectsNonHTTPSDownload covers T27 end to end: the refusal happens
// before the installed set can change.
func TestInstallRejectsNonHTTPSDownload(t *testing.T) {
	m := newTestManager(t)
	before := m.Plugins()
	entry := MarketEntry{
		ID:          "com.example.demo",
		Version:     "1.0.0",
		Permissions: []string{"pet.bubble", "storage"},
		Download:    "file:///x.zip",
		SHA256:      strings.Repeat("0", 64),
	}
	_, err := m.Install(entry)
	if err == nil {
		t.Fatal("a file:// download must be refused")
	}
	if !strings.Contains(err.Error(), "http/https") {
		t.Fatalf("error %q must name the supported schemes", err)
	}
	if after := m.Plugins(); len(after) != len(before) {
		t.Error("a refused install changed the installed set")
	}
}

// --- T28/T29: fetching and per-row validation -------------------------------

// indexServer serves a catalogue payload.
func indexServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/index.json") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// catalogueWith renders a catalogue payload from rows.
func catalogueWith(t *testing.T, rows []map[string]any) string {
	t.Helper()
	doc := map[string]any{"version": 1, "plugins": rows}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal catalogue: %v", err)
	}
	return string(data)
}

func row(id, version string, over map[string]any) map[string]any {
	r := map[string]any{
		"id":          id,
		"name":        "插件 " + id,
		"version":     version,
		"author":      "tester",
		"description": "描述",
		"permissions": []string{"pet.bubble"},
		"download":    "dist/" + id + "-" + version + ".zip",
		"sha256":      strings.Repeat("a", 64),
	}
	for k, v := range over {
		if v == nil {
			delete(r, k)
			continue
		}
		r[k] = v
	}
	return r
}

// TestMarketFetchesRemoteCatalogue covers T28, including the installed marker
// that has to come from the LOCAL index rather than from the catalogue.
func TestMarketFetchesRemoteCatalogue(t *testing.T) {
	m := newTestManager(t)
	// Install one plugin so the catalogue's same-id row can be marked installed.
	entry, _ := testEntryFromFiles(t, sampleZipFiles(), nil)
	if _, err := m.Install(entry); err != nil {
		t.Fatalf("Install: %v", err)
	}

	srv := indexServer(t, catalogueWith(t, []map[string]any{
		row(entry.ID, "1.0.0", nil),
		row("com.example.other", "2.0.0", nil),
		row("com.example.third", "3.0.0", nil),
	}))
	m.BaseURL = func() string { return srv.URL }

	view, err := m.Market()
	if err != nil {
		t.Fatalf("Market: %v", err)
	}
	if view.Fallback {
		t.Error("a reachable catalogue must not be reported as a fallback")
	}
	if len(view.Entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(view.Entries))
	}
	for _, e := range view.Entries {
		if e.ID == entry.ID {
			if !e.Installed {
				t.Error("the installed plugin must be marked installed")
			}
			if e.InstalledVersion != "1.0.0" {
				t.Errorf("installedVersion: got %q want 1.0.0", e.InstalledVersion)
			}
		} else if e.Installed {
			t.Errorf("%s is not installed", e.ID)
		}
	}
}

// TestMarketDropsOnlyTheBadRow covers T29: one malformed row must not cost the
// user the rest of the catalogue, and the drop has to be reported.
func TestMarketDropsOnlyTheBadRow(t *testing.T) {
	m := newTestManager(t)
	srv := indexServer(t, catalogueWith(t, []map[string]any{
		row("com.example.good", "1.0.0", nil),
		row("com.example.nohash", "1.0.0", map[string]any{"sha256": nil}),
		row("com.example.badperm", "1.0.0", map[string]any{"permissions": []string{"fs.read"}}),
	}))
	m.BaseURL = func() string { return srv.URL }

	view, err := m.Market()
	if err != nil {
		t.Fatalf("Market must not fail on a bad row: %v", err)
	}
	if len(view.Entries) != 1 {
		t.Fatalf("want 1 surviving entry, got %d", len(view.Entries))
	}
	if view.Entries[0].ID != "com.example.good" {
		t.Fatalf("wrong survivor: %s", view.Entries[0].ID)
	}
	if view.Notice == "" {
		t.Fatal("the dropped rows must be reported")
	}
	for _, id := range []string{"com.example.nohash", "com.example.badperm"} {
		if !strings.Contains(view.Notice, id) {
			t.Errorf("the notice does not mention the dropped row %s: %s", id, view.Notice)
		}
	}
}

// --- T30/T31: the built-in fallback ----------------------------------------

// TestMarketFallsBackWhenUnreachable covers T30, including the requirement that
// "not configured" and "unreachable" read DIFFERENTLY: they are different
// problems with different fixes.
func TestMarketFallsBackWhenUnreachable(t *testing.T) {
	// An address nothing is listening on. A closed port fails fast locally.
	m := newTestManager(t)
	m.BaseURL = func() string { return "http://127.0.0.1:1" }

	view, err := m.Market()
	if err != nil {
		t.Fatalf("an unreachable market must not be an error: %v", err)
	}
	if !view.Fallback {
		t.Fatal("an unreachable market must be reported as a fallback")
	}
	if view.Notice == "" {
		t.Fatal("a fallback must explain itself")
	}
	unreachableNotice := view.Notice
	unreachableCount := len(view.Entries)

	m2 := newTestManager(t)
	m2.BaseURL = func() string { return "" }
	view2, err := m2.Market()
	if err != nil {
		t.Fatalf("an empty address must not be an error: %v", err)
	}
	if !view2.Fallback {
		t.Fatal("an empty address must be reported as a fallback")
	}
	if view2.Notice == unreachableNotice {
		t.Fatalf("未配置 and 不可达 must read differently, both said %q", view2.Notice)
	}
	if len(view2.Entries) != unreachableCount {
		t.Fatalf("both fallbacks read the same built-in source: %d vs %d", len(view2.Entries), unreachableCount)
	}
}

// TestBuiltinSourceIsUsable covers T31: every plugin in the embedded catalogue
// installs through the SAME digest-checked path the remote source uses.
//
// It asserts a property of the whole catalogue rather than of its first row. The
// earlier version required exactly one plugin and then installed that one, so
// adding a second built-in package silently narrowed what was covered: the new
// entry was never installed by the test that exists to prove built-in entries
// install. Every row is exercised now, which also means a package that is listed
// but missing from the binary fails here rather than on a user's install click.
func TestBuiltinSourceIsUsable(t *testing.T) {
	m := newTestManager(t)
	m.BaseURL = func() string { return "" }

	view, err := m.Market()
	if err != nil {
		t.Fatalf("Market: %v", err)
	}
	if len(view.Entries) == 0 {
		t.Fatal("the built-in source is empty: the offline fallback would offer nothing")
	}

	seen := map[string]bool{}
	for _, entry := range view.Entries {
		if entry.ID == "" {
			t.Fatal("a built-in entry has no id")
		}
		if seen[entry.ID] {
			t.Fatalf("the built-in catalogue lists %s twice", entry.ID)
		}
		seen[entry.ID] = true
		if !strings.HasPrefix(entry.Download, embeddedPrefix) {
			t.Fatalf("a built-in entry must be marked as embedded, got %q", entry.Download)
		}

		row, err := m.Install(entry)
		if err != nil {
			t.Fatalf("installing the built-in plugin %s must work: %v", entry.ID, err)
		}
		if row.ID != entry.ID {
			t.Fatalf("installed %s for the entry %s", row.ID, entry.ID)
		}
		dir, _ := m.PluginDir(entry.ID)
		if _, err := os.Stat(filepath.Join(dir, ManifestName)); err != nil {
			t.Fatalf("the installed manifest of %s is missing: %v", entry.ID, err)
		}
		// The packaged assets must arrive with it: a plugin whose manifest
		// declares files the installer dropped would install "successfully" and
		// then fail at the first audio.import.
		man, err := LoadManifest(dir)
		if err != nil {
			t.Fatalf("read the installed manifest of %s: %v", entry.ID, err)
		}
		for _, asset := range man.Assets {
			p := filepath.Join(dir, filepath.FromSlash(asset))
			if _, err := os.Stat(p); err != nil {
				t.Fatalf("%s declares the asset %s but the installed copy lacks it: %v",
					entry.ID, asset, err)
			}
		}
		// Uninstall, so the next entry installs into a clean state rather than
		// tripping the "already installed" refusal.
		if err := m.Uninstall(entry.ID); err != nil {
			t.Fatalf("uninstall %s: %v", entry.ID, err)
		}
	}

	// The conversation plugin is the one the pet's 对话 slot resolves, so its
	// presence is a product requirement rather than a sample detail.
	if !seen["com.shixiansi.plugin-chat"] {
		t.Fatal("the built-in source no longer carries the conversation plugin")
	}
}

// TestBuiltinSampleMatchesIndex covers T32's always-verifiable half: every zip
// that ships inside the binary has the digest the catalogue claims for it, so a
// stale copy cannot be published by accident.
//
// It walks the WHOLE catalogue rather than its first row. The earlier version
// asserted `len == 1` and then checked that one, which meant adding a second
// built-in plugin changed what the test covered instead of extending it - and the
// new package's digest would have gone unchecked by the very test whose job that
// is. Every entry is now verified, and an empty catalogue is still refused, so
// the check cannot pass by having nothing to check.
func TestBuiltinSampleMatchesIndex(t *testing.T) {
	indexBytes, err := SampleIndexBytes()
	if err != nil {
		t.Fatalf("read the built-in catalogue: %v", err)
	}
	var doc struct {
		Plugins []struct {
			ID       string `json:"id"`
			Download string `json:"download"`
			SHA256   string `json:"sha256"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(indexBytes, &doc); err != nil {
		t.Fatalf("parse the built-in catalogue: %v", err)
	}
	if len(doc.Plugins) == 0 {
		t.Fatal("the built-in catalogue is empty: the offline fallback would have nothing to offer")
	}

	for _, entry := range doc.Plugins {
		// The download is a path relative to the market root ("dist/<name>.zip");
		// the embedded copy is flat, so the archive is named by its last segment.
		zipName := filepath.Base(strings.ReplaceAll(entry.Download, "\\", "/"))
		zipBytes, err := readSample(zipName)
		if err != nil {
			// Named explicitly: the failure a user sees for a missing package is a
			// 404 on install, and the id is what connects that to this list.
			t.Fatalf("read the embedded package %s (declared by %s): %v", zipName, entry.ID, err)
		}
		sum := sha256.Sum256(zipBytes)
		got := hex.EncodeToString(sum[:])
		if got != entry.SHA256 {
			t.Fatalf("the embedded package %s does not match the catalogue:\n  computed %s\n  indexed  %s",
				entry.ID, got, entry.SHA256)
		}
	}
	entry := doc.Plugins[0]

	// The download is a path relative to the market root ("dist/<name>.zip");
	// the embedded copy is flat, so the archive is named by its last segment.
	zipName := filepath.Base(strings.ReplaceAll(entry.Download, "\\", "/"))
	zipBytes, err := readSample(zipName)
	if err != nil {
		t.Fatalf("read the embedded package %s: %v", zipName, err)
	}
	sum := sha256.Sum256(zipBytes)
	got := hex.EncodeToString(sum[:])
	if got != entry.SHA256 {
		t.Fatalf("the embedded package does not match the catalogue:\n  computed %s\n  indexed  %s", got, entry.SHA256)
	}

	// When the market repository's working copy is present, the two copies must
	// be the same bytes. It is absent in a plain checkout of this repo alone
	// (market/ is a separate repository), so that half is SKIPPED rather than
	// failed - the assertion above is the always-verifiable one.
	marketZip := filepath.Join("..", "..", "market", "dist", zipName)
	if _, err := os.Stat(marketZip); err != nil {
		t.Logf("SKIP: %s is not present (the market repo is a separate checkout)", marketZip)
		return
	}
	marketBytes, err := os.ReadFile(marketZip)
	if err != nil {
		t.Fatalf("read %s: %v", marketZip, err)
	}
	marketSum := sha256.Sum256(marketBytes)
	if hex.EncodeToString(marketSum[:]) != got {
		t.Fatalf("the market copy and the embedded copy differ:\n  market   %s\n  embedded %s",
			hex.EncodeToString(marketSum[:]), got)
	}
}

// TestBuiltinSampleAgreesWithTheMarketCatalogue covers T71: the two catalogues
// describe the same package, field by field.
//
// The digest check above proves the BYTES agree; this one proves the two files
// that name them agree, which is the half that can drift silently. A version bump
// that repackaged the zip but left the market's index.json pointing at the old
// name would ship a catalogue whose download 404s - and the embedded copy would
// still be self-consistent, so nothing else would notice.
//
// Like the digest comparison, the market repository is a separate checkout and
// may be absent; when it is, the assertion is SKIPPED rather than failed.
func TestBuiltinSampleAgreesWithTheMarketCatalogue(t *testing.T) {
	type entry struct {
		ID       string `json:"id"`
		Version  string `json:"version"`
		Download string `json:"download"`
		SHA256   string `json:"sha256"`
	}
	read := func(path string) []entry {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("cannot read %s: %v", path, err)
		}
		var doc struct {
			Plugins []entry `json:"plugins"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		return doc.Plugins
	}

	market := read(filepath.Join("..", "..", "market", "index.json"))
	sampleRaw, err := SampleIndexBytes()
	if err != nil {
		t.Fatalf("read the built-in catalogue: %v", err)
	}
	var doc struct {
		Plugins []entry `json:"plugins"`
	}
	if err := json.Unmarshal(sampleRaw, &doc); err != nil {
		t.Fatalf("parse the built-in catalogue: %v", err)
	}

	if len(market) != len(doc.Plugins) {
		t.Fatalf("the catalogues list %d and %d plugins", len(market), len(doc.Plugins))
	}
	for i := range market {
		m, s := market[i], doc.Plugins[i]
		if m.ID != s.ID {
			t.Errorf("entry %d: market id %q, built-in id %q", i, m.ID, s.ID)
			continue
		}
		if m.Version != s.Version {
			t.Errorf("%s: market version %q, built-in version %q", m.ID, m.Version, s.Version)
		}
		// The download differs by directory - "dist/x.zip" against the flat
		// embedded name - so the comparison is on the base name, which is what
		// actually has to match.
		if mb, sb := path.Base(m.Download), path.Base(s.Download); mb != sb {
			t.Errorf("%s: market package %q, built-in package %q", m.ID, mb, sb)
		}
		if m.SHA256 != s.SHA256 {
			t.Errorf("%s: market sha256 %s, built-in sha256 %s", m.ID, m.SHA256, s.SHA256)
		}
	}
}
