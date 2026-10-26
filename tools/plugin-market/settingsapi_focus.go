package main

import (
	"PetPC/internal/store"
)

// The pomodoro preferences, one method per field.
//
// A generic "patch the config" call would let this program change anything the struct
// has, including fields no page owns; one method per preference keeps the writable set
// equal to the visible set.
//
// Ranges are NOT validated here. store.SaveSettings calls Config.Normalise, which clamps
// every one of these to the supported range, and the caller gets the CLAMPED config back
// - so the page can show what was actually stored instead of what was typed.
func (m *Market) SetFocusMinutes(v int) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.FocusMinutes = v })
}

func (m *Market) SetShortBreakMinutes(v int) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.ShortBreakMinutes = v })
}

func (m *Market) SetLongBreakMinutes(v int) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.LongBreakMinutes = v })
}

func (m *Market) SetLongBreakEvery(v int) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.LongBreakEvery = v })
}

func (m *Market) SetAutoStartNextPhase(on bool) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.AutoStartNextPhase = on })
}
