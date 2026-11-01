package store

import (
	"os"
	"path/filepath"
	"strings"
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

// The pack lives in the SAME file as the scheme, and the two fields have two different writers: the
// pet's menu flips the scheme, the control center's cards change the pack. So the load-bearing
// property is not "it round-trips" (that is easy) but "each writer leaves the other's field alone".
//
// WHY THIS IS A TEST AND NOT A CONVENTION. A writer that marshals only its own field looks correct
// in review, correct in its own unit test, and correct on screen for as long as the user only uses
// one of the two windows. The failure mode is a colour that silently reverts, and nothing in either
// program's log would say why.
func TestPanelThemePackAndSchemeDoNotClobberEachOther(t *testing.T) {
	dir := t.TempDir()

	if got := ReadPanelThemePack(dir); got != "" {
		t.Fatalf("nothing stored yet reads as %q, want empty (the caller decides its default)", got)
	}

	if err := WritePanelThemePack(dir, "sakura"); err != nil {
		t.Fatalf("WritePanelThemePack: %v", err)
	}
	if got := ReadPanelThemePack(dir); got != "sakura" {
		t.Fatalf("pack read back as %q, want sakura", got)
	}
	// The pet's menu writes a scheme without knowing packs exist.
	if err := WritePanelTheme(dir, ThemeDark); err != nil {
		t.Fatalf("WritePanelTheme: %v", err)
	}
	if got := ReadPanelThemePack(dir); got != "sakura" {
		t.Fatalf("the scheme write dropped the pack: reads %q, want sakura", got)
	}
	if got := ReadPanelTheme(dir); got != ThemeDark {
		t.Fatalf("scheme read back as %q, want dark", got)
	}
	// And the other direction: choosing a pack must not reset the scheme to light.
	if err := WritePanelThemePack(dir, "graphite"); err != nil {
		t.Fatalf("WritePanelThemePack: %v", err)
	}
	if got := ReadPanelTheme(dir); got != ThemeDark {
		t.Fatalf("the pack write reset the scheme to %q, want dark", got)
	}

	// A hand-edited or future id must not survive into the stylesheet: no pack is a look, an
	// unknown id is an unstyled window.
	for _, junk := range []string{"../etc", "樱花", "a b", "sakura/pink", strings.Repeat("x", 33)} {
		if err := WritePanelThemePack(dir, junk); err != nil {
			t.Fatalf("WritePanelThemePack(%q): %v", junk, err)
		}
		if got := ReadPanelThemePack(dir); got != "" {
			t.Fatalf("junk pack %q was stored as %q, want empty", junk, got)
		}
	}
	// Case and padding are normalised rather than rejected: an id is an id.
	if err := WritePanelThemePack(dir, "  SAKURA  "); err != nil {
		t.Fatal(err)
	}
	if got := ReadPanelThemePack(dir); got != "sakura" {
		t.Fatalf("padded/upper-case pack read back as %q, want sakura", got)
	}
}
