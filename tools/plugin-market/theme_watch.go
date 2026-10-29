package main

import (
	"time"

	"PetPC/internal/store"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

/*
 * Telling this window that the OTHER window changed the colour scheme.
 *
 * The scheme lives in one file both programs read and write (internal/store/theme.go). When the
 * user flips it from the pet's right-click menu, this window has no idea: there is no IPC between
 * the two, and a page that reads the value once at mount keeps drawing the old scheme. Reported
 * exactly that way: 「在控制面板打开的情况下，右键菜单的主题切换不会实时改变控制面板的主题」.
 *
 * WHY THE POLL IS HERE AND NOT IN THE PAGE. A `setInterval` in App.svelte was the first attempt and
 * it hung the render smoke test: a page with a timer that never stops is a page that never settles,
 * so every `settle()` in the harness waits for ever. A ticker in Go plus one event costs the page
 * nothing and keeps the smoke deterministic.
 *
 * The reverse direction needs no code here: the pet already re-reads its own preferences once a
 * second (app_settingswatch.go) and pushes a fresh layout when the scheme changed.
 */

// eventMarketTheme carries the new scheme to the page.
//
// A named constant, like every other event in this repository: `.dev/check-frontend.cjs` pairs each
// EventsOn() in a page with an emit in Go, and it resolves constants named eventXxx.
const eventMarketTheme = "market:theme"

// themeWatchInterval is the same one second the pet's settings watcher uses: two stats of a tiny
// file per second, and below the "did it work?" threshold for a colour change.
const themeWatchInterval = time.Second

// startThemeWatch emits eventMarketTheme whenever the shared scheme changes.
//
// The first tick compares against what the caller already painted, so starting the watcher never
// re-sends the value the window opened with.
func (m *Market) startThemeWatch(applied string) {
	if m.ctx == nil {
		return
	}
	ctx := m.ctx
	go func() {
		ticker := time.NewTicker(themeWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				theme := store.ReadPanelTheme(m.dataDir)
				if theme == "" || theme == applied {
					continue
				}
				applied = theme
				runtime.EventsEmit(ctx, eventMarketTheme, theme)
				m.log.Infof("market", "另一个窗口改了配色（%s），已通知页面", theme)
			}
		}
	}()
}
