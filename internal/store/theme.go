package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

/*
 * The colour scheme is the ONE preference BOTH programs own.
 *
 * It lives in its own file instead of in either program's config, because both windows can change
 * it: the pet's right-click menu has a 主题 cell, and the control center has a 偏好 page. A single
 * value with two owners in two files is exactly how "the menu is dark and the settings are light"
 * happens - reported 2026-10-29 as 一个亮一个暗的，像是独立的, after each window had grown its own
 * switch. One file, one value, either window may write it.
 *
 * LAST WRITER WINS, on purpose. This is a switch, not a document: both writes are a
 * read-modify-write of one key, so the worst case is a flip landing a moment later - and the
 * alternative (a lock, or a hidden owner) costs more than the thing it protects.
 *
 * The pet's `settings.json` used to carry `cardTheme`, and the market's `plugin-market.json`
 * carries `theme`. Both are read ONCE as a fallback when this file is missing, so an install that
 * predates it keeps whatever it was showing; after the first flip from either window the shared
 * file is the only thing that matters.
 */

// The two values, and the file they live in. The name is the one the pet's page already uses for
// its `<html data-theme>` attribute.
const (
	ThemeLight = "light"
	ThemeDark  = "dark"

	themeFileName = "theme.json"
)

// themeFile is the shared look: a base scheme plus a palette pack.
//
// TWO fields, and they are written by DIFFERENT windows: the pet's right-click menu only flips the
// scheme, the control center's 主题包 cards only change the pack. That is why every write below is a
// read-modify-write of the whole file - a writer that marshals only its own field would silently
// erase the other window's choice, and the user would see "我选的主题包一会儿就没了" with nothing in
// any log to explain it.
type themeFile struct {
	Theme string `json:"theme"`
	// Pack is the id of the palette pack ("violet", "sakura", …). Empty means "whatever the
	// stylesheet's default is", which is what an install that predates packs has.
	Pack string `json:"pack,omitempty"`
}

// NormalizePack validates a pack id, returning "" for anything that is not one.
//
// The shape is deliberately the same character class a theme DIRECTORY would need
// (`[a-z0-9-]`), even though today packs are compiled into the stylesheet and never touch the
// filesystem: the next step in the plan is reading them from `themes/<id>/`, and a validator that
// has to be tightened later is a validator that will be missed. Empty is the answer for "not a
// pack", never an error - a look must not be able to fail a save.
func NormalizePack(name string) string {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" || len(trimmed) > 32 {
		return ""
	}
	for _, r := range trimmed {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return ""
		}
	}
	return trimmed
}

// NormalizeTheme maps anything that is not "dark" onto "light".
//
// Light is the default because it is the scheme the two windows are designed around: the control
// center is a light window, and the pet's cards read as the same product. An unknown value must
// never fail a save - a scheme that cannot be stored is a scheme the window silently stops
// remembering.
func NormalizeTheme(name string) string {
	if strings.EqualFold(strings.TrimSpace(name), ThemeDark) {
		return ThemeDark
	}
	return ThemeLight
}

// ReadPanelTheme returns the stored scheme, or "" when nothing has been chosen yet.
//
// Empty is not the same as "light": a caller that has a legacy field of its own falls back to that
// once, and a caller with no history takes its own default. Reporting "light" here would silently
// throw that away.
func ReadPanelTheme(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, themeFileName))
	if err != nil {
		return ""
	}
	var f themeFile
	if err := json.Unmarshal(data, &f); err != nil {
		// A corrupt file is not a preference. Falling back (rather than refusing to start) keeps
		// the window usable; the next flip rewrites the file.
		return ""
	}
	if strings.TrimSpace(f.Theme) == "" {
		return ""
	}
	return NormalizeTheme(f.Theme)
}

// WritePanelTheme stores the scheme for both windows.
//
// Written through a temporary file and renamed: a hard kill between the two must not leave a
// half-written theme that no reader can parse, because the reader is the other program.
//
// It PRESERVES the stored pack (read-modify-write). The pet's menu flips the scheme and knows
// nothing about packs; without this, one click there would throw the user's chosen palette away
// and the only symptom would be "颜色自己变回来了".
func WritePanelTheme(dir, theme string) error {
	stored := readThemeFile(dir)
	return writeThemeFile(dir, themeFile{Theme: NormalizeTheme(theme), Pack: NormalizePack(stored.Pack)})
}

// ReadPanelThemePack returns the stored pack id, or "" when none was chosen.
//
// Empty is not "the default pack": the caller decides what its default is (the control center draws
// violet), and an install that predates packs must not be told it chose violet.
func ReadPanelThemePack(dir string) string {
	return NormalizePack(readThemeFile(dir).Pack)
}

// WritePanelThemePack stores the pack and leaves the scheme alone (read-modify-write, the mirror of
// WritePanelTheme). A pack id that does not validate is stored as empty, i.e. "no choice", rather
// than as a broken one: the stylesheet's default is a look, an unknown id is an unstyled window.
func WritePanelThemePack(dir, pack string) error {
	theme := NormalizeTheme(readThemeFile(dir).Theme)
	return writeThemeFile(dir, themeFile{Theme: theme, Pack: NormalizePack(pack)})
}

// readThemeFile is the raw read every writer starts from. A missing or corrupt file reads as a zero
// value, which the callers then normalise - the same "no opinion" rule ReadPanelTheme follows.
func readThemeFile(dir string) themeFile {
	data, err := os.ReadFile(filepath.Join(dir, themeFileName))
	if err != nil {
		return themeFile{}
	}
	var f themeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return themeFile{}
	}
	return f
}

// writeThemeFile is the one place that touches the file, so the atomicity argument is written once.
func writeThemeFile(dir string, f themeFile) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	path := filepath.Join(dir, themeFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
