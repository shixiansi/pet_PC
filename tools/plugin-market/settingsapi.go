package main

import (
	"fmt"

	"PetPC/internal/store"
)

// The control center's window onto the pet's preferences.
//
// The pet and this program share a data directory, and since the settings split
// (openspec/changes/ui-rebuild, phase 1) `settings.json` belongs to THIS program while
// `config.json` belongs to the pet. So these methods are the only correct way to change
// a preference: they go through store.SaveSettings, which re-reads the file before
// writing so a change is never lost to a stale copy.
//
// The pet picks the change up through Store.Config()'s mtime check - no IPC, no restart.

// MainSettings returns the pet's current configuration.
//
// Returns the whole Config rather than a hand-picked view: the pages need different
// fields, and a per-page projection would be a second definition of "the settings" that
// drifts from the struct the pet actually reads.
func (m *Market) MainSettings() (store.Config, error) {
	s, err := store.New(m.dataDir)
	if err != nil {
		return store.Config{}, fmt.Errorf("读取设置失败：%w", err)
	}
	return s.Config(), nil
}

// SetPetScale changes how big the pet is drawn.
//
// One method per preference rather than a generic "patch the config" call: a generic
// writer would let a page change a field it has no UI for, which is the difference
// between a settings screen and a config editor.
func (m *Market) SetPetScale(scale float64) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.PetScale = scale })
}