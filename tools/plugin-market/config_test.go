package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"PetPC/internal/plugins"
	"PetPC/internal/store"
)

// This file covers the market program's OWN settings file.
//
// The separation from the pet's config.json is the single most load-bearing
// property of this program, and it is asserted here by BYTES rather than by "the
// write went to a different path": the guarantee the design makes is that the
// pet's file is untouched after a market save, so the test reads it before and
// after and compares. Asserting only the destination path would still pass if
// something else in the call chain also wrote the pet's file.

// marketDir prepares a temp data directory with a pet config.json in it, and
// returns both paths.
//
// The pet's config is seeded with a marketBaseUrl so the SEEDING path has
// something real to read - a fixture without it would make every seeding test
// pass through the fallback branch and prove nothing.
func marketDir(t *testing.T, petConfig string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if petConfig != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(petConfig), 0o644); err != nil {
			t.Fatalf("seed pet config: %v", err)
		}
	}
	return dir, filepath.Join(dir, "plugin-market.json")
}

// petConfigWithURL is the pet's settings as the pet writes them: a JSON object
// carrying the mirror the user chose.
func petConfigWithURL(url string) string {
	return `{"petScale":1,"snapToEdge":true,"marketBaseUrl":"` + url + `"}`
}

// T5: saving the market's preferences must not touch the pet's config, byte for
// byte.
func TestSaveMarketConfigLeavesThePetConfigByteIdentical(t *testing.T) {
	dir, cfgPath := marketDir(t, petConfigWithURL("https://pet.example/market"))
	petPath := filepath.Join(dir, "config.json")

	before, err := os.ReadFile(petPath)
	if err != nil {
		t.Fatalf("read pet config before: %v", err)
	}

	cfg := loadMarketConfig(cfgPath, dir)
	cfg.MarketBaseURL = "https://market.example/x"
	cfg.LastSelectedID = "com.example.demo"
	cfg.WindowW = 1024
	cfg.WindowH = 768
	if err := saveMarketConfig(cfgPath, cfg); err != nil {
		t.Fatalf("saveMarketConfig: %v", err)
	}

	after, err := os.ReadFile(petPath)
	if err != nil {
		t.Fatalf("read pet config after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("the pet's config.json changed:\n before=%q\n after =%q", before, after)
	}

	// And the write really landed in the market's own file.
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read market config: %v", err)
	}
	for _, want := range []string{"https://market.example/x", "com.example.demo", "1024", "768"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("market config is missing %q:\n%s", want, raw)
		}
	}
}

// The seed: a fresh install adopts the address the pet was configured with, so an
// existing user keeps their mirror.
func TestMarketConfigSeedsFromThePetConfig(t *testing.T) {
	dir, cfgPath := marketDir(t, petConfigWithURL("https://mirror.example/plugins/"))

	cfg := loadMarketConfig(cfgPath, dir)
	if cfg.MarketBaseURL != "https://mirror.example/plugins" {
		t.Fatalf("seeded base = %q, want the pet's address without its trailing slash", cfg.MarketBaseURL)
	}
	if cfg.WindowW != 980 || cfg.WindowH != 700 {
		t.Fatalf("seeded window = %dx%d, want the configured default 980x700", cfg.WindowW, cfg.WindowH)
	}
}

// With nothing configured anywhere, the built-in sample source is the floor.
func TestMarketConfigFallsBackToTheBuiltInSource(t *testing.T) {
	// No pet config at all.
	dir, cfgPath := marketDir(t, "")
	if got := loadMarketConfig(cfgPath, dir).MarketBaseURL; got != plugins.DefaultMarketBaseURL {
		t.Fatalf("base = %q, want the built-in default", got)
	}

	// And a pet config that exists but carries no address.
	dir2, cfgPath2 := marketDir(t, `{"petScale":1}`)
	if got := loadMarketConfig(cfgPath2, dir2).MarketBaseURL; got != plugins.DefaultMarketBaseURL {
		t.Fatalf("base with an address-less pet config = %q, want the built-in default", got)
	}
}

// T6: a settings file that cannot be parsed is quarantined and defaults take over,
// because a settings file must never be able to stop the program from opening.
func TestCorruptMarketConfigIsQuarantinedAndDefaultsApply(t *testing.T) {
	dir, cfgPath := marketDir(t, "")
	if err := os.WriteFile(cfgPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}

	cfg := loadMarketConfig(cfgPath, dir)

	if cfg.MarketBaseURL != plugins.DefaultMarketBaseURL {
		t.Fatalf("base = %q, want the default after quarantine", cfg.MarketBaseURL)
	}
	if cfg.WindowW != 980 || cfg.WindowH != 700 {
		t.Fatalf("window = %dx%d, want 980x700 after quarantine", cfg.WindowW, cfg.WindowH)
	}
	if _, err := os.Stat(cfgPath + ".bak"); err != nil {
		t.Fatalf("the bad copy was not kept as .bak: %v", err)
	}
	// The original is gone: it was RENAMED, not copied, so the next load starts
	// clean rather than quarantining the same bad bytes forever.
	if _, err := os.Stat(cfgPath); err == nil {
		t.Fatal("the corrupt config is still in place, so it would be quarantined again on every launch")
	}
}

// An empty file is not "corrupt": it is what a truncated write or a fresh touch
// leaves, and quarantining it would litter the directory with .bak files.
func TestEmptyMarketConfigIsNotQuarantined(t *testing.T) {
	dir, cfgPath := marketDir(t, "")
	if err := os.WriteFile(cfgPath, []byte("   \n"), 0o644); err != nil {
		t.Fatalf("write empty config: %v", err)
	}
	cfg := loadMarketConfig(cfgPath, dir)
	if cfg.MarketBaseURL != plugins.DefaultMarketBaseURL {
		t.Fatalf("base = %q, want the default", cfg.MarketBaseURL)
	}
	if _, err := os.Stat(cfgPath + ".bak"); err == nil {
		t.Fatal("an empty settings file was quarantined")
	}
}

// The URL gate: anything that is not http/https is refused BEFORE the write, so a
// typo cannot replace a working address.
func TestCheckMarketURL(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"", false},
		{"   ", false},
		{"https://example.com/m", false},
		{"http://example.com/m", false},
		{"ftp://example.com/m", true},
		{"file:///c:/plugins", true},
		{"javascript:alert(1)", true},
		{"example.com/m", true},
		{"//example.com/m", true},
	}
	for _, tc := range cases {
		err := checkMarketURL(tc.in)
		if tc.wantErr && err == nil {
			t.Fatalf("checkMarketURL(%q) accepted a non-http(s) address", tc.in)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("checkMarketURL(%q) rejected a usable address: %v", tc.in, err)
		}
	}
}

// The save path trims a trailing slash, so the address the CATALOGUE is fetched
// from and the address shown in the box agree.
func TestMarketConfigNormalisesTheSavedAddress(t *testing.T) {
	dir, cfgPath := marketDir(t, "")
	cfg := marketConfig{MarketBaseURL: "https://example.com/m/", WindowW: 980, WindowH: 700}
	if err := saveMarketConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	// A reload normalises whatever was stored, which is what the binding relies on.
	cfg.MarketBaseURL = strings.TrimRight(cfg.MarketBaseURL, "/")
	if err := saveMarketConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save normalised: %v", err)
	}
	got := loadMarketConfig(cfgPath, dir)
	if got.MarketBaseURL != "https://example.com/m" {
		t.Fatalf("stored address = %q, want the trailing slash removed", got.MarketBaseURL)
	}
}

// The settings write is atomic, so a crash mid-write cannot leave a half-written
// file that the next launch quarantines.
func TestSaveMarketConfigLeavesNoTempFiles(t *testing.T) {
	dir, cfgPath := marketDir(t, "")
	if err := saveMarketConfig(cfgPath, defaultMarketConfig(dir)); err != nil {
		t.Fatalf("save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("a staging file was left behind: %s", e.Name())
		}
	}
}

// The colour scheme is ONE preference for BOTH windows, stored in the file the two programs share
// (internal/store/theme.go) - not in this program's config, and not in the webview's storage.
//
// This test used to assert the opposite (the value lives in plugin-market.json, and anything that
// is not "light" reads back as dark). That arrangement is what let the pet's menu and this window
// disagree: each had its own switch in its own file. What matters now:
//
//   - a fresh install answers LIGHT from both programs, so they cannot start out different;
//   - a flip is written to the shared file, where the other program reads it;
//   - an unknown value lands on the default (light) rather than on the other extreme, because
//     "unreadable" means "no opinion", and no opinion is the default.
func TestThemeIsSharedAndNormalised(t *testing.T) {
	m := newTestMarket(t, "")

	// A settings file written before the scheme was shared has no value at all: both windows
	// answer the default, which is the light scheme they are designed around.
	if got := m.Theme(); got != themeLight {
		t.Fatalf("default theme = %q, want %q", got, themeLight)
	}

	if err := m.SetTheme("dark"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if got := m.Theme(); got != themeDark {
		t.Fatalf("theme after set = %q, want %q", got, themeDark)
	}
	// It is in the SHARED file, not only in memory: the pet reads the same file, and a restart of
	// either program has to open in it.
	if got := store.ReadPanelTheme(m.dataDir); got != themeDark {
		t.Fatalf("stored theme = %q, want %q", got, themeDark)
	}

	// Case and surrounding space are forgiven...
	for _, in := range []string{"  Light  ", "LIGHT"} {
		if err := m.SetTheme(in); err != nil {
			t.Fatalf("SetTheme(%q): %v", in, err)
		}
		if got := m.Theme(); got != themeLight {
			t.Fatalf("SetTheme(%q) -> %q, want %q", in, got, themeLight)
		}
	}
	// ...an explicit dark is dark...
	if err := m.SetTheme("DARK"); err != nil {
		t.Fatalf("SetTheme(DARK): %v", err)
	}
	if got := m.Theme(); got != themeDark {
		t.Fatalf("SetTheme(DARK) -> %q, want %q", got, themeDark)
	}
	// ...and anything else is the default, not the opposite end.
	for _, in := range []string{"", "purple"} {
		if err := m.SetTheme(in); err != nil {
			t.Fatalf("SetTheme(%q): %v", in, err)
		}
		if got := m.Theme(); got != themeLight {
			t.Fatalf("SetTheme(%q) -> %q, want the default %q", in, got, themeLight)
		}
	}
}
