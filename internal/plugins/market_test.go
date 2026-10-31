package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
		if err := m.Uninstall(entry.ID, true); err != nil {
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
	// The market comparison covers EVERY entry, not just the first.
	//
	// It used to compare doc.Plugins[0] alone, which is how nine of the ten sample packages
	// drifted from the market's copies without this test saying anything: the one it looked at
	// happened to match, or the failure named a plugin nobody suspected. One loop, every entry.
	//
	// When the market repository's working copy is present, the two copies must be the same
	// bytes (AGENTS.md §3.7: the offline fallback and the market artefact cannot diverge). It is
	// absent in a plain checkout of this repo alone (market/ is a separate repository), so that
	// half is SKIPPED rather than failed - the digest check above is the always-verifiable one.
	for _, entry := range doc.Plugins {
		zipName := filepath.Base(strings.ReplaceAll(entry.Download, "\\", "/"))
		marketZip := filepath.Join("..", "..", "market", "dist", zipName)
		marketBytes, err := os.ReadFile(marketZip)
		if err != nil {
			t.Logf("SKIP %s: %v", zipName, err)
			continue
		}
		marketSum := sha256.Sum256(marketBytes)
		if hex.EncodeToString(marketSum[:]) != entry.SHA256 {
			t.Fatalf("%s: the market copy and the embedded copy differ:\n  market   %s\n  embedded %s",
				entry.ID, hex.EncodeToString(marketSum[:]), entry.SHA256)
		}
	}
}

// TestBuiltinSampleAgreesWithTheMarketCatalogue covers T71: the sample is a SUBSET of the
// market, and it says so.
//
// # WHY THIS IS NOT "THE TWO CATALOGUES ARE EQUAL"
//
// That is what it used to assert, and it was right when the market had ten plugins. It is 36
// now: `market/index.json` carries ONE entry per plugin (the latest), while the sample is a
// pinned offline fallback that ships whatever the last packaging run produced - deliberately
// older, because bumping the fallback on every release would mean re-embedding megabytes in
// the binary each time. Demanding equal lengths made this test fail on a fact about the
// product rather than about a bug (36 against 10), which is the kind of red that teaches
// people to ignore red.
//
// What must hold instead:
//
//  1. every plugin the sample ships is still one the market lists - the fallback must not
//     resurrect something that was removed;
//  2. the package it ships is byte-identical to the market's copy of that SAME version (that
//     is TestBuiltinSampleMatchesIndex's job, asserted for every entry);
//  3. every sample entry names a package its own index carries (also that test).
//
// Like the digest comparison, the market repository is a separate checkout and may be absent;
// when it is, the assertion is SKIPPED rather than failed.
func TestBuiltinSampleAgreesWithTheMarketCatalogue(t *testing.T) {
	marketIdx := filepath.Join("..", "..", "market", "index.json")
	raw, err := os.ReadFile(marketIdx)
	if err != nil {
		t.Skipf("cannot read %s (the market repo is a separate checkout): %v", marketIdx, err)
	}
	var marketDoc struct {
		Plugins []struct {
			ID string `json:"id"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &marketDoc); err != nil {
		t.Fatalf("parse %s: %v", marketIdx, err)
	}
	listed := make(map[string]bool, len(marketDoc.Plugins))
	for _, e := range marketDoc.Plugins {
		listed[e.ID] = true
	}

	sampleRaw, err := SampleIndexBytes()
	if err != nil {
		t.Fatalf("read the built-in catalogue: %v", err)
	}
	var sampleDoc struct {
		Plugins []struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(sampleRaw, &sampleDoc); err != nil {
		t.Fatalf("parse the built-in catalogue: %v", err)
	}
	if len(sampleDoc.Plugins) == 0 {
		t.Fatal("the built-in catalogue is empty: the offline fallback would have nothing to offer")
	}
	for _, e := range sampleDoc.Plugins {
		if !listed[e.ID] {
			t.Errorf("the sample ships %s v%s, which the market no longer lists: an offline "+
				"fallback for a plugin the catalogue dropped is a package nobody can update",
				e.ID, e.Version)
		}
	}
}
// TestEveryPublishedManifestParses is the "we never publish a manifest we refuse" guard.
//
// The failure it exists for, hit for real on 2026-11-01: the market shipped web-dashboard with
// `callTargets: ["com.shixiansi.plugin-todo/stats@>=1.2.2"]` while the HOST the user was running
// predated the version-floor syntax. The update failed with "callTargets 项 … 不是
// <插件id>/<方法名>" - correct, and completely confusing, because nothing in the message says
// "your host is older than this manifest".
//
// A schema change and the parser that accepts it have to reach the user together; this test
// catches the half that CAN be checked in one place - our own current rules against everything we
// publish. The other half (an old binary reading a new catalogue) can only be fixed by shipping
// the host first, which is the ordering rule in docs/plugin-dev.md §4.
//
// SKIPped when market/ is not checked out; the CI `contract` job checks it out, so it does run
// there.
func TestEveryPublishedManifestParses(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("..", "..", "market", "plugins", "*", "manifest.json"))
	if err != nil || len(dirs) == 0 {
		t.Skipf("no catalogued manifests to check (the market repo is a separate checkout): %v", err)
	}
	for _, path := range dirs {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		m, err := ParseManifest(raw)
		if err != nil {
			// The path, not the id: a manifest that fails to parse may not have a usable id.
			t.Errorf("%s: the host refuses a manifest we published: %v", path, err)
			continue
		}
		// And the parts the version floors touch have to survive their own parser.
		for _, entry := range m.CallTargets {
			if _, _, _, ok := SplitCallTargetVersion(entry); !ok {
				t.Errorf("%s: callTargets entry %q does not parse", m.ID, entry)
			}
		}
	}
}
