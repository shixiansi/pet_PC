package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The settings/state split, from the store's side.
//
// The property under test is the one that removes a whole class of bug: ONE WRITER PER
// FILE. settings.json belongs to the control center, config.json to the pet, and
// neither program can clobber the other's values by saving a stale in-memory copy of
// "everything".

// readFileMap decodes a JSON object so a test can assert on KEYS rather than on the
// struct - "this key must not be in that file" is the whole point here.
func readFileMap(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse %s: %v (%s)", path, err, raw)
	}
	return m
}

func TestSettingsFileHoldsPreferencesAndNotRuntimeKeys(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Touch a runtime value so config.json exists and the split is exercised.
	if err := s.SetPosition(120, 300); err != nil {
		t.Fatal(err)
	}

	settings := readFileMap(t, filepath.Join(dir, settingsFileName))
	if _, ok := settings["focusMinutes"]; !ok {
		t.Errorf("settings.json has no focusMinutes: %v", settings)
	}
	for _, k := range runtimeKeys {
		if _, ok := settings[k]; ok {
			t.Errorf("settings.json carries the runtime key %q; the control center would then be able to write it", k)
		}
	}

	runtime := readFileMap(t, filepath.Join(dir, "config.json"))
	if _, ok := runtime["position"]; !ok {
		t.Errorf("config.json lost the position: %v", runtime)
	}
	// The other direction matters just as much: a preference in config.json would mean
	// the pet is writing one.
	if _, ok := runtime["focusMinutes"]; ok {
		t.Errorf("config.json carries the preference focusMinutes; the pet would then be its second writer: %v", runtime)
	}
}

// TestSettingsWinOverLegacyConfig covers the migration's read order.
//
// An old config.json still holds preference keys (the migration copies, it does not
// move), so settings.json has to be read LAST or the legacy values would win.
func TestSettingsWinOverLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"focusMinutes":30,"petScale":1.5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A settings file that disagrees with it.
	if err := os.WriteFile(filepath.Join(dir, settingsFileName),
		[]byte(`{"focusMinutes":25}`), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := s.Config()
	if cfg.FocusMinutes != 25 {
		t.Errorf("focusMinutes = %d, want 25 (settings.json must beat the legacy config.json)", cfg.FocusMinutes)
	}
	// A key the settings file does not mention keeps the legacy value: the file is
	// decoded ON TOP of what config.json produced, exactly like config.json is decoded
	// on top of DefaultConfig.
	if cfg.PetScale != 1.5 {
		t.Errorf("petScale = %v, want 1.5 from the legacy file", cfg.PetScale)
	}
}

// TestMigrationCopiesInsteadOfMoving pins the safety property: after the first run the
// old file is still there, with its keys, and a backup exists.
//
// "Copy, do not move" is what makes the migration reversible - deleting settings.json
// brings the previous behaviour back.
func TestMigrationCopiesInsteadOfMoving(t *testing.T) {
	dir := t.TempDir()
	original := `{"focusMinutes":40,"reminderIntervalMin":45,"position":{"x":10,"y":20}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Config().FocusMinutes; got != 40 {
		t.Errorf("focusMinutes = %d, want 40 from the legacy file", got)
	}

	// The preferences were written out...
	settings := readFileMap(t, filepath.Join(dir, settingsFileName))
	if settings["focusMinutes"] != float64(40) {
		t.Errorf("settings.json focusMinutes = %v, want 40", settings["focusMinutes"])
	}
	// ...the old file still has them (nothing was moved)...
	legacy := readFileMap(t, filepath.Join(dir, "config.json"))
	if legacy["focusMinutes"] != float64(40) {
		t.Errorf("config.json lost focusMinutes: %v", legacy)
	}
	// ...and the backup exists.
	if _, err := os.Stat(filepath.Join(dir, settingsBackupName)); err != nil {
		t.Errorf("no backup of the pre-split config: %v", err)
	}
}

// TestConfigPicksUpSettingsChangedOnDisk is the whole deliverable in one test: another
// program writes the file, and the running pet sees it.
func TestConfigPicksUpSettingsChangedOnDisk(t *testing.T) {
	oldInterval := settingsReloadInterval
	settingsReloadInterval = 0 // no throttling: assert the mechanism, not the throttle
	t.Cleanup(func() { settingsReloadInterval = oldInterval })

	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Config().FocusMinutes; got == 42 {
		t.Fatal("the fixture already has the value under test")
	}

	// The control center saves through its own entry point, in its own process.
	if _, err := SaveSettings(dir, func(c *Config) { c.FocusMinutes = 42 }); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	if got := s.Config().FocusMinutes; got != 42 {
		t.Errorf("after an external save FocusMinutes = %d, want 42 - the pet did not re-read settings.json", got)
	}
}

// TestSaveSettingsLeavesRuntimeStateAlone pins the control center's half of the split:
// saving a preference must not touch the position the pet is keeping in the other file.
func TestSaveSettingsLeavesRuntimeStateAlone(t *testing.T) {
	oldInterval := settingsReloadInterval
	settingsReloadInterval = 0
	t.Cleanup(func() { settingsReloadInterval = oldInterval })

	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPosition(77, 88); err != nil {
		t.Fatal(err)
	}

	if _, err := SaveSettings(dir, func(c *Config) { c.FocusMinutes = 33 }); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	runtime := readFileMap(t, filepath.Join(dir, "config.json"))
	pos, ok := runtime["position"].(map[string]any)
	if !ok {
		t.Fatalf("config.json lost the position: %v", runtime)
	}
	if pos["x"] != float64(77) || pos["y"] != float64(88) {
		t.Errorf("position = %v, want 77/88", pos)
	}

	// And the pet sees the new preference.
	if got := s.Config().FocusMinutes; got != 33 {
		t.Errorf("FocusMinutes = %d, want 33", got)
	}
}

// TestPetSettingsUISurvivesTheTransition pins the transitional branch in UpdateConfig.
//
// Until the settings UI moves to the control center, a preference changed through the
// pet's own panel still has to survive a restart. And a position write must NOT touch
// the settings file: the pet writes that many times an hour, and a settings write is
// the one thing that can race the other program.
func TestPetSettingsUISurvivesTheTransition(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(dir, settingsFileName)
	if err := s.SetPosition(1, 2); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	// A runtime-only write leaves the settings file alone.
	if err := s.SetPosition(3, 4); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("dragging the pet rewrote settings.json; that is the file the control center owns")
	}

	// A preference write persists, and survives a reopen - the pet's own panel is still
	// the only settings UI until phase 3.
	if _, err := s.UpdateConfig(func(c *Config) { c.FocusMinutes = 47 }); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Config().FocusMinutes; got != 47 {
		t.Errorf("FocusMinutes = %d, want 47 after a reopen", got)
	}
}

// TestSettingsReloadIsThrottled pins that the stat is not done on every Config() call.
//
// Not a performance measurement: it asserts that a same-millisecond rewrite is NOT
// picked up inside the interval, which is the behaviour that keeps a per-tick reader
// from hammering the filesystem.
func TestSettingsReloadIsThrottled(t *testing.T) {
	oldInterval := settingsReloadInterval
	settingsReloadInterval = time.Hour
	t.Cleanup(func() { settingsReloadInterval = oldInterval })

	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	before := s.Config().FocusMinutes
	if _, err := SaveSettings(dir, func(c *Config) { c.FocusMinutes = 55 }); err != nil {
		t.Fatal(err)
	}
	if got := s.Config().FocusMinutes; got != before {
		t.Errorf("a change was picked up inside the throttle window: %d -> %d", before, got)
	}
}
