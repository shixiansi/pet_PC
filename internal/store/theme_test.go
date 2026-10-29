package store

import (
	"os"
	"path/filepath"
	"testing"
)

// The colour scheme is ONE value that BOTH programs read and write, so these three properties are
// the whole contract: it round-trips, an unreadable value lands on the default, and a missing file
// says "no opinion" instead of guessing.
//
// The last one is what lets a caller fall back to its own legacy field exactly once. Returning
// "light" for a missing file would look identical to a user who never chose anything - and would
// silently overwrite an install that had chosen dark in its own config before this file existed.
func TestPanelThemeRoundTrips(t *testing.T) {
	dir := t.TempDir()

	if got := ReadPanelTheme(dir); got != "" {
		t.Fatalf("nothing stored yet reads as %q, want empty", got)
	}

	for _, want := range []string{ThemeLight, ThemeDark} {
		if err := WritePanelTheme(dir, want); err != nil {
			t.Fatalf("WritePanelTheme(%q): %v", want, err)
		}
		if got := ReadPanelTheme(dir); got != want {
			t.Fatalf("read back %q, want %q", got, want)
		}
	}

	// A value written by hand (or by a future version) must not leave the window in a scheme no
	// stylesheet knows.
	if err := os.WriteFile(filepath.Join(dir, themeFileName), []byte(`{"theme":"  DARK  "}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadPanelTheme(dir); got != ThemeDark {
		t.Fatalf("padded/upper-case value read back as %q, want %q", got, ThemeDark)
	}
	if err := os.WriteFile(filepath.Join(dir, themeFileName), []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadPanelTheme(dir); got != "" {
		t.Fatalf("a corrupt file read back as %q, want empty so the caller falls back", got)
	}

	// No staging file survives a write: the reader is the OTHER program, and a leftover .tmp that
	// it might pick up would be a theme nobody chose.
	if err := WritePanelTheme(dir, ThemeDark); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("a staging file was left behind: %s", e.Name())
		}
	}
}
