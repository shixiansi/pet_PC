package main

import (
	"slices"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/petmenu"
)

// Re-applying preferences that only take effect when APPLIED.
//
// The store re-reads settings.json lazily (Store.Config), and that is enough for
// everything a render-time decision reads: the focus length, the reminder switches, the
// LLM budget. Two things are NOT in that category:
//
//   - the pet's SIZE, which is baked into the window geometry when the window is placed,
//     so a new value on its own changes nothing at all;
//   - the GLOBAL HOTKEYS, which are a registration with the OS, made once. A switch that
//     only took effect on the next start would be a switch that lies.
//
// Without this loop, changing either one in the control center does exactly what the user
// reported: nothing until the pet is restarted.
//
// The distinction is the point: "the data is fresh" and "the change has been applied"
// are two different things, and only the first one comes for free.
const settingsWatchInterval = time.Second

// eventPetImage tells the page the pet's picture changed on disk.
//
// A constant rather than a literal at the call site, like every other event name in this
// program: .dev/check-frontend.cjs pairs each EventsOn() in the page with an emit in Go, and
// it resolves constants named eventXxx - a literal buried in a two-argument call is an emit
// that scan cannot see, which would report the listener as dangling.
const eventPetImage = "pet:image"

// eventPetState carries a WHOLE fresh State to the page.
//
// 为什么需要它：`menuHidden`（右键菜单显示哪几格）唯一的写入者是控制中心，而这一页读的是它
// 启动时拿到的那份 State —— 两份快照之间没有任何东西通知它。用户的原话是"这里设置不能实时
// 生效右键菜单"：在控制中心关掉一格，回到桌宠右键，菜单还是九格。
//
// 推而不是让页面轮询：本仓库的规矩是「变化是推的，不是拉的」（AGENTS §3.9 第 3 条）——
// 一个"等一个文件变化"的 setInterval 会在日志里变成几万行，而它等的东西一天变不了几次。
const eventPetState = "pet:state"

// syncMenuHidden pushes a fresh State when the denylist changed underneath us, and returns
// the value now applied.
//
// 拆成方法而不是塞进那个 ticker 里，是为了能测：ticker 要一个 Wails ctx 才转得动，而
// "变了就推、没变就不推"正是这里唯一会错的地方（推两次让页面白重画一遍，或者干脆不推——
// 后者就是用户报的那个 bug）。
func (a *App) syncMenuHidden(hidden []string, applied []string) []string {
	want := petmenu.Normalise(hidden)
	if slices.Equal(want, applied) {
		return applied
	}
	state, err := a.GetState()
	if err != nil {
		a.log.Warnf("settings", "外部改动了右键菜单的开关，但读不到状态：%v", err)
		return applied
	}
	a.emitEvent(eventPetState, state)
	a.log.Infof("settings", "外部改动了右键菜单的开关（关掉 %d 格），已推送状态", len(state.Config.MenuHidden))
	return want
}

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

// startSettingsWatch applies an outside change to the pet's size and to the global hot key
// registration.
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
		cfg := a.store.Config()
		appliedScale := cfg.PetScale
		appliedHotkeys := cfg.GlobalHotkeys
		appliedImage := a.store.ImagePath()
		// The colour scheme is shared with the control center (internal/store/theme.go), which
		// can flip it at any moment. Same rule as the scale: a fresh value on disk is not an
		// applied change - the page has to be told, and a layout push is what tells it.
		//
		// The PACK is the same story from the same file, and it has exactly one writer (the
		// control center's 主题包 cards). Watching them in one comparison is deliberate: the two
		// travel together in the layout, so two separate pushes would paint the same frame twice
		// and - worse - could paint the new scheme with the old pack in between.
		// 右键菜单显示哪几格：写入者是控制中心，读者是这一页，所以要盯着它。
		appliedMenu := petmenu.Normalise(cfg.MenuHidden)
		appliedTheme := a.panelTheme()
		appliedPack := a.panelThemePack()
		ticker := time.NewTicker(settingsWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				cfg := a.store.Config()
				if cfg.PetScale != appliedScale {
					appliedScale = cfg.PetScale
					if _, err := a.applyScale(cfg.PetScale); err != nil {
						a.log.Warnf("settings", "外部改动了缩放，重新布局失败：%v", err)
					}
				}
				// Re-registering is the ONLY way the switch can take effect in this
				// process: RegisterHotKey happened once, on this thread's message loop.
				// applyHotkeys releases the keys as well when the switch went off, so the
				// rest of the desktop gets them back without a restart.
				if cfg.GlobalHotkeys != appliedHotkeys {
					appliedHotkeys = cfg.GlobalHotkeys
					a.applyHotkeys()
					a.log.Info("settings", "外部改动了全局快捷键开关，已重新登记")
				}
				// The picture is drawn by the WEBVIEW from a URL, so a new file on disk
				// changes nothing on its own - unlike the scale, there is no geometry to
				// re-apply, there is mark-up to repaint. The control center is where it is
				// now chosen, so the pet has to be told; the event carries the URL so the
				// page does not have to guess when to ask again.
				if theme, pack := a.panelTheme(), a.panelThemePack(); theme != appliedTheme || pack != appliedPack {
					appliedTheme, appliedPack = theme, pack
					if l, err := a.GetLayout(); err == nil {
						a.emitEvent("pet:layout", l)
					}
					a.log.Infof("settings", "外部改动了外观，已推送布局（配色 %s，主题包 %q）", theme, pack)
				}
				appliedMenu = a.syncMenuHidden(cfg.MenuHidden, appliedMenu)
				if img := a.store.ImagePath(); img != appliedImage {
					appliedImage = img
					a.emitEvent(eventPetImage, map[string]any{
						"petImageUrl":    a.PetImageURL(),
						"hasCustomImage": img != "",
					})
					a.log.Info("settings", "外部改动了桌宠图片，已通知页面重画")
				}
			}
		}
	}()
}
