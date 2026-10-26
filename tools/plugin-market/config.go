package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"PetPC/internal/plugins"
	"PetPC/internal/store"
)

// This file is the market program's OWN preferences.
//
// They are a SEPARATE file from the pet's config, and the separation is
// load-bearing rather than tidy: the pet pushes its whole config to its webview
// on every state read, so anything stored there is a leak surface, and a market
// address is not a pet setting. The two programs share the plugin DIRECTORIES,
// not their configuration.
//
// The address is seeded ONCE from the pet's settings (so an existing user keeps
// the mirror they chose) and from then on this file is the only thing this
// program reads or writes. The pet's config is never modified - not one byte -
// which is what keeps a rollback a zero-step operation.
type marketConfig struct {
	// MarketBaseURL is the catalogue address. Empty means the built-in sample
	// source.
	MarketBaseURL string `json:"marketBaseUrl"`
	// LastSelectedID remembers which plugin was open, so reopening the window
	// lands where the user left off.
	LastSelectedID string `json:"lastSelectedId"`
	// Theme is the colour scheme: "dark" or "light". Empty means a file written
	// before themes existed, and empty READS AS DARK - the scheme this window
	// shipped with - so an existing settings file opens exactly as it did.
	Theme string `json:"theme,omitempty"`
	// WindowW and WindowH are remembered dimensions; they are advisory and may
	// be overridden by the OS.
	WindowW int `json:"windowW"`
	WindowH int `json:"windowH"`
}

// The two colour schemes.
//
// They live here, not only in the front end, because the VALUE IS PERSISTED: this
// file has to name them, and a typo has to be a compile error rather than a window
// that opens in the wrong scheme.
const (
	themeDark  = "dark"
	themeLight = "light"
)

// defaultMarketConfig is what a fresh install starts from.
func defaultMarketConfig(dataDir string) marketConfig {
	return marketConfig{
		MarketBaseURL: seedMarketURL(dataDir),
		WindowW:       980,
		WindowH:       700,
	}
}

// seedMarketURL reads the pet's configured address, falling back to the
// built-in default.
//
// The read itself lives in internal/store, next to the config file's name and
// the rest of its parsing: this program never names the pet's settings file, and
// that is deliberate rather than tidy. The moment it can name it, it can be
// edited to WRITE it, and "the pet's config is never modified by another program"
// is what keeps a rollback a zero-step operation (design §2.3, Q3/Q4). What
// arrives here is already trimmed and unset-to-empty.
func seedMarketURL(dataDir string) string {
	if v := store.ReadMarketBaseURL(dataDir); v != "" {
		return v
	}
	return plugins.DefaultMarketBaseURL
}

// loadMarketConfig reads this program's settings.
//
// A file that cannot be parsed is quarantined (renamed .bak) and replaced with
// defaults, the same treatment the pet gives its own config and its plugin
// index: a settings file must never be able to stop the program from opening,
// and the bad copy is kept so the user can look at it.
func loadMarketConfig(path, dataDir string) marketConfig {
	cfg := defaultMarketConfig(dataDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return cfg
	}
	var parsed marketConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		quarantineFile(path)
		return cfg
	}
	if strings.TrimSpace(parsed.MarketBaseURL) != "" {
		cfg.MarketBaseURL = strings.TrimRight(strings.TrimSpace(parsed.MarketBaseURL), "/")
	}
	cfg.LastSelectedID = parsed.LastSelectedID
	// The scheme is normalised on the way IN, so everything downstream - Theme(),
	// the front end, the gate's seeded file - sees one of exactly two names.
	cfg.Theme = normalizeTheme(parsed.Theme)
	if parsed.WindowW > 0 {
		cfg.WindowW = parsed.WindowW
	}
	if parsed.WindowH > 0 {
		cfg.WindowH = parsed.WindowH
	}
	return cfg
}

// saveMarketConfig writes this program's settings atomically.
func saveMarketConfig(path string, cfg marketConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// quarantineFile renames an unreadable file to "<name>.bak", keeping only the
// most recent bad copy.
func quarantineFile(path string) {
	backup := path + ".bak"
	os.Remove(backup)
	_ = os.Rename(path, backup)
}

// checkMarketURL rejects anything that is not http/https before it is stored.
//
// Refusing BEFORE the write is what makes a typo harmless: the previous, working
// address stays in place and the user sees why the new one was rejected.
func checkMarketURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		return errors.New("市场地址只支持 http/https：" + trimmed)
	}
	return nil
}
