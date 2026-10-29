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

type themeFile struct {
	Theme string `json:"theme"`
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
func WritePanelTheme(dir, theme string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(themeFile{Theme: NormalizeTheme(theme)}, "", "  ")
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
