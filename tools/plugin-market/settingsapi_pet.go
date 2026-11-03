package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PetPC/internal/desktop"
	"PetPC/internal/petmenu"
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

// petExeName is the pet's executable: the program the Run entry must name.
const petExeName = "PetPC.exe"

// petExecutableCandidates lists where the pet may be, most preferred first.
//
// It mirrors app_plugin_market.go's PluginMarketCandidates in the other direction: the
// two programs are copied side by side by tools\build-all.ps1, and each also knows how to
// find the other's build output while running from its own build tree. A separate, pure
// enumeration rather than a loop inside petExecutablePath so the search order itself is
// testable - "the release puts one of them somewhere else and the switch can never work"
// is invisible until a user hits it.
func petExecutableCandidates(self string) []string {
	dir := filepath.Dir(self)
	return []string{
		// Beside the control center: the release layout, and where build-all puts both.
		filepath.Join(dir, petExeName),
		// The pet's build output, for a control center started from its own build
		// tree (tools\plugin-market\build\bin) in the repository.
		filepath.Join(dir, "..", "..", "..", "..", "build", "bin", petExeName),
	}
}

// petExecutablePath returns the pet's location, and whether it was found.
func petExecutablePath() (string, bool) {
	self, err := os.Executable()
	if err != nil {
		return "", false
	}
	return firstExistingFile(petExecutableCandidates(self))
}

// firstExistingFile returns the first candidate that is a file that exists.
func firstExistingFile(candidates []string) (string, bool) {
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true
		}
	}
	return "", false
}

// SetAutostart switches the pet's auto-start on or off.
//
// THE POINT OF THIS METHOD: the Run entry must name the PET, so the pet's path is
// resolved here and passed in explicitly (desktop.SetAutostartFor). It used to call
// desktop.SetAutostart, which resolves os.Executable() - and when the control center
// calls it, that is the control center. Windows then started the wrong program at logon,
// which looks exactly like auto-start being broken: no pet, ever.
//
// Why the registry and not a saved preference (the settings split says settings.json
// belongs to this program)? Because `autostart` is a RUNTIME key of config.json: the
// pet writes it as a mirror of the registry, and store.SaveSettings strips it out of the
// preferences file. Saving it here wrote nothing - so the old code wrote the wrong
// executable AND updated nothing. The registry is what Windows reads at logon, so it is
// the only honest place for this switch to write.
func (m *Market) SetAutostart(on bool) (store.Config, error) {
	if !on {
		// Disabling needs no path, and must work with no pet binary in sight: a user
		// whose install is half-deleted still has to be able to switch this off.
		if err := desktop.SetAutostart(false); err != nil {
			return store.Config{}, fmt.Errorf("删除开机启动项失败：%w", err)
		}
		return m.MainSettings()
	}
	pet, ok := petExecutablePath()
	if !ok {
		return store.Config{}, fmt.Errorf("找不到桌宠程序（%s），而开机自启必须指向它：请把两个程序放在同一个目录里，或者用 tools\\build-all.ps1 重新构建", petExeName)
	}
	if err := desktop.SetAutostartFor(pet, true); err != nil {
		return store.Config{}, fmt.Errorf("写入开机启动项失败：%w", err)
	}
	return m.MainSettings()
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

// --- 对话提醒：**没有**了（2026-11-03 删掉）------------------------------------
//
// 这里原来有一组开关：开启对话提醒 / 启动时自动开启 / 没有待办时也提醒 / 间隔 / 抖动 /
// 活跃时段 / 一次提几件 / 气泡停留几秒。它们写的是宿主 `settings.json` 里的 `reminder*`
// 字段，而**宿主的提醒调度器随「提醒搬进待办插件」一起删掉了**（app_reminder.go 的文件头
// 写着这件事：只剩手动「立刻提醒」）。于是那组开关一个字都没人读——用户把「开启对话提醒」
// 关了又开，什么也没发生，因为那个值没有任何消费者。
//
// 2026-11-03 用户的两句反馈正是从这里来的（"提醒开了关了后就没了""关了后再开也不显示了"），
// 而真正的提醒（待办插件自己的 config）是另一回事：它那条是插件不认 `config.changed`
// 导致的，已经在插件 1.3.1 修掉。
//
// 所以这组开关连同它的五个绑定一起删了。**别再照着"这里少了一组开关"把它加回来**：一组
// 点了没反应的开关比没有这组开关更糟（AGENTS §3.1 的那条不变量，用在 UI 上也一样）。提醒的
// 设置项现在只有一处：控制中心「插件」页里待办插件自己声明的那几项。
// --- 桌宠右键菜单显示哪些格子（2026-11-03）-------------------------------------
//
// 「开始」页那一组开关：桌宠的右键菜单有九格，用户可以把不常用的关掉（归位、主题这种）。
//
// 表与规则都在 internal/petmenu，两个程序共用一份——桌宠按它判"哪些不能关"，这一页按它画
// 行。**不要**在这个文件里再写一份标签表：两套规则必然分叉，而分叉的那套会让用户在一个
// 窗口里能关掉某格、在另一个窗口里关不掉。
//
// 为什么设置在这里而不是在右键菜单里：那一张卡片的高度是宿主钉死的常量（226px），
// 2026-11-03 试过在底部塞一条「设置」、把九格换成开关页，第一反馈是"显示效果不好"。而
// "桌宠长什么样"本来就是这一页的事，菜单也是它的一部分。

// PetMenuCells 是菜单的九格（id / 标签 / 是否固定），给这一页画开关行用。
func (m *Market) PetMenuCells() []petmenu.Cell {
	return petmenu.Cells()
}

// SetPetMenuHidden 存"用户关掉了哪些格子"，并把保存后的 config 回给页面。
//
// 归一化在写入时做一遍（未知 id、固定那一格丢掉），读的时候桌宠那一侧还会再做一遍：
// settings.json 是用户手能改的纯文件，只拦一处就是留个洞。
func (m *Market) SetPetMenuHidden(hidden []string) (store.Config, error) {
	clean := petmenu.Normalise(hidden)
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.MenuHidden = clean })
}
