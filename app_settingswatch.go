package main

import (
	"time"

	"PetPC/internal/desktop"
)

// Re-applying preferences that only take effect when APPLIED.
//
// The store re-reads settings.json lazily (Store.Config), and that is enough for
// everything a render-time decision reads: the focus length, the reminder switches, the
// LLM budget. The pet's SIZE is not one of those - it is baked into the window geometry
// when the window is placed, so a new value on its own changes nothing at all. Without
// this loop, changing the size in the control center does exactly what the user
// reported: nothing until the pet is restarted.
//
// The distinction is the point: "the data is fresh" and "the change has been applied"
// are two different things, and only the first one comes for free.
const settingsWatchInterval = time.Second

// applyScale re-places the window for a new pet size, holding the bottom-right corner
// steady so the pet appears to grow from there.
//
// It does NOT persist anything: the value has already been read from the settings file
// (or written by SetScale, which then calls this). Keeping the two apart is what lets
// the watcher apply an outside change without the pet writing the file back - the pet
// is not a writer of settings.json.
func (a *App) applyScale(scale float64) (Layout, error) {
	a.mu.Lock()
	pet := a.petRect
	a.mu.Unlock()

	newW, newH := a.petPixelSize()
	x := pet.X + pet.Width - newW
	y := pet.Y + pet.Height - newH

	a.mu.Lock()
	a.petRect = desktop.Bounds{X: x, Y: y, Width: newW, Height: newH}
	a.mu.Unlock()
	return a.applyWindowGeometry()
}

// startSettingsWatch applies an outside change to the pet's size.
//
// One second is the cost of a stat plus a comparison, and the person who moved the
// slider is looking at the pet, so a second is the difference between "instant" and
// "did it work?". The ticker ends with the Wails context, so a quit does not leave a
// goroutine behind holding the store.
func (a *App) startSettingsWatch() {
	if a.ctx == nil {
		return
	}
	go func() {
		applied := a.store.Config().PetScale
		ticker := time.NewTicker(settingsWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				next := a.store.Config().PetScale
				if next == applied {
					continue
				}
				applied = next
				if _, err := a.applyScale(next); err != nil {
					a.DebugLog("settings: 外部改动缩放后重新布局失败: " + err.Error())
				}
			}
		}
	}()
}
