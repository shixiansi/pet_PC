package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Settings live in settings.json and runtime state in config.json. The split is by
// WHO WRITES the file, not by what a field means:
//
//   - settings.json is written by the CONTROL CENTER (a separate program). The pet
//     only reads it, and re-reads it when it changes on disk.
//   - config.json keeps the three values the PET writes while it runs: the window
//     position (every drag), the autostart mirror (it reconciles that with the
//     Windows registry), and the reminder toggle (the tray menu).
//
// One writer per file is the whole point. One file with two programs writing it needs
// a lost-update protocol, and losing a settings change to a stale in-memory copy
// shows up as "I changed it and it did not take" - the class of bug this split exists
// to remove.
//
// Config itself stays ONE struct: it is read in a hundred places, and splitting the
// type would be a rename with no benefit. Only the PERSISTENCE is split.
const settingsFileName = "settings.json"

// settingsBackupName is the copy of config.json taken when the split first happens.
//
// A migration that only moves values is one bug away from losing a user's settings, so
// the old file is copied before anything is written. It is written ONCE: the second
// run finds it and leaves it alone.
const settingsBackupName = "config.before-settings-split.json"

// runtimeKeys are the config keys config.json owns; everything else is a preference.
//
// They are DELETED from the settings file rather than zeroed, so a preferences file
// never carries `"position":null` that reads like a setting someone could edit.
var runtimeKeys = []string{"position", "autostart", "reminderEnabled"}

// settingsReloadInterval bounds how often Config() checks the settings file.
//
// 500ms is invisible to a person changing a setting, and costs one stat per call at
// worst. A file watcher would be a dependency bought for a delay nobody can perceive.
//
// A var rather than a const so a test can drop it to zero instead of sleeping; nothing
// in production writes it.
var settingsReloadInterval = 500 * time.Millisecond

// settingsPath is where the control center writes preferences.
func (s *Store) settingsPath() string {
	return filepath.Join(s.dir, settingsFileName)
}

// loadSettings reads settings.json on top of whatever config.json produced, and
// migrates an installation that predates the split.
//
// Settings are read LAST on purpose: an old config.json still contains preference
// keys, and the preferences file has to win over them. Leaving those keys in place is
// what makes the migration reversible - delete settings.json and the previous
// behaviour comes back.
func (s *Store) loadSettings() error {
	if _, err := os.Stat(s.settingsPath()); err != nil {
		// No settings file: this installation predates the split (or it is a fresh
		// one). Write the current preferences out, so the control center has
		// something to edit, and keep the old file on disk.
		if err := s.migrateSettings(); err != nil {
			s.warnings = append(s.warnings, "settings.json 无法创建（"+err.Error()+"），设置仍从 config.json 读取")
			return nil
		}
	}
	if err := readJSON(s.settingsPath(), &s.cfg); err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("%s 无法解析（%v），已重置为 config.json 中的值", settingsFileName, err))
		if backup := quarantine(s.settingsPath()); backup != "" {
			s.warnings[len(s.warnings)-1] += "，原文件已备份为 " + filepath.Base(backup)
		}
	}
	s.cfg.Normalise()
	s.stampSettingsLocked()
	return nil
}

// migrateSettings writes the preferences into settings.json for the first time.
//
// The old config.json is COPIED, not consumed: every preference key stays where it is,
// so a failed migration costs nothing and rolling back means deleting one file.
func (s *Store) migrateSettings() error {
	backup := filepath.Join(s.dir, settingsBackupName)
	if _, err := os.Stat(backup); err != nil {
		if raw, readErr := os.ReadFile(s.configPath()); readErr == nil {
			_ = os.WriteFile(backup, raw, 0o644)
		}
	}
	return s.writeSettings(s.cfg)
}

// writeSettings persists ONLY the preference keys.
func (s *Store) writeSettings(cfg Config) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		return err
	}
	for _, k := range runtimeKeys {
		delete(all, k)
	}
	if err := writeJSON(s.settingsPath(), all); err != nil {
		return err
	}
	s.stampSettingsLocked()
	return nil
}

// writeRuntime persists ONLY the keys config.json owns.
//
// Writing the whole struct here would quietly make the pet a second writer of the
// preferences - the exact thing the split removes - and it would also resurrect the
// legacy keys the migration left behind in the old file.
func (s *Store) writeRuntime(cfg Config) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		return err
	}
	runtimeOnly := make(map[string]any, len(runtimeKeys))
	for _, k := range runtimeKeys {
		if v, ok := all[k]; ok {
			runtimeOnly[k] = v
		}
	}
	return writeJSON(s.configPath(), runtimeOnly)
}

// stampSettingsLocked remembers the file's state so the next check can tell whether it
// changed. Callers already hold s.mu (or are still inside New).
func (s *Store) stampSettingsLocked() {
	s.settingsChecked = time.Now()
	if info, err := os.Stat(s.settingsPath()); err == nil {
		s.settingsStamp = info.ModTime().UnixNano() ^ info.Size()
	}
}

// refreshSettingsLocked re-reads settings.json when it has changed on disk.
//
// This is what makes "change a setting in the control center, watch the pet react"
// true without restarting anything. It is throttled, and it is called from Config()
// so every reader benefits from one call site rather than a reload ritual in each of
// the hundred places that read a setting.
func (s *Store) refreshSettingsLocked() {
	now := time.Now()
	if now.Sub(s.settingsChecked) < settingsReloadInterval {
		return
	}
	s.settingsChecked = now

	info, err := os.Stat(s.settingsPath())
	if err != nil {
		return
	}
	stamp := info.ModTime().UnixNano() ^ info.Size()
	if stamp == s.settingsStamp {
		return
	}
	s.settingsStamp = stamp

	// Decoded over the current struct: the file carries every preference key, so each
	// one is overwritten. A key someone deleted by hand keeps its previous value,
	// which is the same rule config.json has always followed.
	if err := readJSON(s.settingsPath(), &s.cfg); err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("%s 无法解析（%v），继续使用上一份设置", settingsFileName, err))
		return
	}
	s.cfg.Normalise()
}

// preferencesChanged reports whether two configs differ in any PREFERENCE field.
//
// Compared as JSON maps with the runtime keys removed rather than with
// reflect.DeepEqual: the runtime fields change constantly (the position moves on every
// drag) and including them would report "changed" every time, which is the opposite of
// what this predicate is for.
func (s *Store) preferencesChanged(before Config) bool {
	was, err := preferenceMap(before)
	if err != nil {
		return true // cannot prove it did not change, so persist
	}
	now, err := preferenceMap(s.cfg)
	if err != nil {
		return true
	}
	if len(was) != len(now) {
		return true
	}
	for k, v := range now {
		if fmt.Sprint(was[k]) != fmt.Sprint(v) {
			return true
		}
	}
	return false
}

// preferenceMap is the config's preference keys, as a map.
func preferenceMap(cfg Config) (map[string]any, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	for _, k := range runtimeKeys {
		delete(all, k)
	}
	return all, nil
}

// SaveSettings applies fn to the preferences and writes them for the control center.
//
// THE CONTROL CENTER'S ENTRY POINT. It reads the file first so a save can never
// overwrite a change the other program made a moment earlier, and it writes only the
// preference keys - the runtime values in config.json belong to the pet and are left
// exactly as they are on disk.
func SaveSettings(dir string, fn func(*Config)) (Config, error) {
	s, err := New(dir)
	if err != nil {
		return Config{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
	s.cfg.Normalise()
	if err := s.writeSettings(s.cfg); err != nil {
		return s.cfg, err
	}
	return s.cfg, nil
}
