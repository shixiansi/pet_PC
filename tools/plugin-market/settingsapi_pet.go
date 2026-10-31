package main

import (
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PetPC/internal/desktop"
	"PetPC/internal/store"
)

// The pet's own preferences: the groups the pet's panel used to own.
//
// This file exists because the settings tab is being retired (openspec/changes/ui-rebuild
// 阶段 3): the panel is a 380x520 rectangle that cannot hold five groups of controls, and
// the control center is a normal window that can. What moves here is the part that is
// purely "write a preference" - `settings.json` belongs to this program, so these methods
// are the ONE writer of those keys.
//
// What does NOT move, and must not be faked here: anything that is an ACTION on the running
// pet (quit it, abandon the current pomodoro, remind right now) and anything that owns a
// file this program does not write (the pet's image, its records). Those keep a home in the
// pet or in its tray menu; a switch here that silently did nothing would be worse than no
// switch at all.
//
// One method per preference, matching settingsapi_focus.go: a generic "patch the config"
// call would let a page change a field no page owns.

// --- 外观与启动 -------------------------------------------------------------------

func (m *Market) SetSnapToEdge(on bool) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.SnapToEdge = on })
}

// SetAutostart writes the per-user Run entry AND the preference, in that order.
//
// The order is the whole point: if the registry write fails, the preference must not be
// saved, or `settings.json` would claim the pet starts with Windows while it does not - and
// the user would have no way to see the lie. The pet's own SetAutostart does the same thing
// for the same reason.
func (m *Market) SetAutostart(on bool) (store.Config, error) {
	if err := desktop.SetAutostart(on); err != nil {
		return store.Config{}, fmt.Errorf("写入开机启动项失败：%w", err)
	}
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.Autostart = on })
}

// SetGlobalHotkeys switches the global shortcuts on or off.
//
// The PET applies this: its settings watcher re-registers (or releases) the keys when the
// preference changes, so the switch takes effect while both programs are running. Saving the
// preference alone would have been a switch that only works after a restart, which is the
// failure mode AGENTS.md §7 is about.
func (m *Market) SetGlobalHotkeys(on bool) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.GlobalHotkeys = on })
}

// HotkeyLabels returns the shortcut pair as the panel words it, and whether it is on.
//
// Read from internal/desktop (the package that registers them) rather than typed here: a
// second copy of "Ctrl+Alt+S" would be a second place to be wrong, and the labels are shown
// next to a switch the user is deciding about.
func (m *Market) HotkeyLabels() (desktop.HotkeyView, error) {
	s, err := store.New(m.dataDir)
	if err != nil {
		return desktop.HotkeyView{}, fmt.Errorf("读取设置失败：%w", err)
	}
	return desktop.ConfiguredHotkeyView(s.Config().GlobalHotkeys), nil
}

// --- 桌宠图片 ---------------------------------------------------------------------
//
// The picture moves here rather than to the tray, because choosing it needs a MODAL file
// dialog and this is the window that has one. The copying itself is store.SetPetImage - the
// same call the pet makes - so there is still one implementation of "put this picture in the
// data directory".
//
// The pet applies it within a second: its settings watcher compares the configured image and
// re-reads the state when it changes (app_settingswatch.go), which is what makes this an
// edit rather than a "restart to see it".

func (m *Market) ChoosePetImage() (store.Config, error) {
	if m.ctx == nil {
		return store.Config{}, fmt.Errorf("窗口还没准备好，稍后再试")
	}
	path, err := runtime.OpenFileDialog(m.ctx, runtime.OpenDialogOptions{
		Title: "选择桌宠图片",
		Filters: []runtime.FileFilter{{
			DisplayName: "图片 (*.png;*.jpg;*.jpeg;*.gif;*.webp;*.bmp;*.svg)",
			Pattern:     "*.png;*.jpg;*.jpeg;*.gif;*.webp;*.bmp;*.svg",
		}},
	})
	if err != nil {
		return store.Config{}, fmt.Errorf("打开文件对话框失败：%w", err)
	}
	if strings.TrimSpace(path) == "" {
		// Cancelled: not an error, and the settings must not change.
		return m.MainSettings()
	}
	s, err := store.New(m.dataDir)
	if err != nil {
		return store.Config{}, fmt.Errorf("读取设置失败：%w", err)
	}
	if _, err := s.SetPetImage(path); err != nil {
		return store.Config{}, err
	}
	return s.Config(), nil
}

// ResetPetImage goes back to the bundled default picture.
func (m *Market) ResetPetImage() (store.Config, error) {
	s, err := store.New(m.dataDir)
	if err != nil {
		return store.Config{}, fmt.Errorf("读取设置失败：%w", err)
	}
	if err := s.ClearPetImage(); err != nil {
		return store.Config{}, err
	}
	return s.Config(), nil
}

// --- 对话提醒 ---------------------------------------------------------------------

// These switches write the todo PLUGIN's reminder preferences, not a host loop's.
//
// The host's own 30-second reminder scheduler retired with the host timer
// (app_reminder.go keeps only the manual "立刻提醒"), and the schedule lives in the todo
// plugin's resident frame now - which reads the SAME settings.json file. So every setter below
// is still "save a preference": there is no loop here to start, stop or restart, and the
// plugin picks the new value up on its next turn.
//
// What that means for the page's wording: 「这些改动是即时的」 stays true, but the reason is the
// plugin's frame, not a host tick. Saying otherwise was the stale sentence this comment used to
// repeat.

func (m *Market) SetReminderEnabled(on bool) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.ReminderEnabled = on })
}

func (m *Market) SetReminderOnStartup(on bool) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.ReminderOnStartup = on })
}

func (m *Market) SetRemindWhenEmpty(on bool) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.RemindWhenEmpty = on })
}

// SetReminderCadence is the interval row: a base interval plus the random jitter added to it.
//
// Two fields in one method because they are one row in the UI and one sentence in the user's
// head ("every 30 minutes, plus up to 3"). Two calls could silently leave the pair in a state
// nobody asked for.
func (m *Market) SetReminderCadence(intervalMin, jitterMaxMin int) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) {
		c.ReminderIntervalMin = intervalMin
		c.ReminderJitterMaxMin = jitterMaxMin
	})
}

// SetReminderWindow is the active hours, "09:00" and "22:00".
//
// NOT validated here: Config.Normalise owns the accepted format and what a bad value falls
// back to, and the config it returns is what the page shows - so a typo shows up as the value
// that was actually stored instead of as the value that was typed.
func (m *Market) SetReminderWindow(from, to string) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) {
		c.ReminderFrom = from
		c.ReminderTo = to
	})
}

// SetReminderDisplay is how many todos one bubble shows and how long it stays.
func (m *Market) SetReminderDisplay(items, autoHideSec int) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) {
		c.ReminderMaxItems = items
		c.ReminderAutoHideSec = autoHideSec
	})
}
