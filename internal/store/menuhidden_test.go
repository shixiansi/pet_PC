package store

import (
	"sync"
	"testing"
)

// TestMenuHiddenCanBeClearedWhileRunning 是 2026-11-03 用户报的那条的复现：
//
//	"关掉提醒再打开，右键菜单里的提醒直接没了，不出现了，开关都没反应"
//	（以及上一条"退出桌宠重开又出现了"）
//
// 形状：`menuHidden` 是 denylist，**关掉 = 往数组里加**、**打开 = 从数组里删**。加进去没问题；
// 删空的后果是这个键被 `omitempty` 从文件里整条抹掉，而 refreshSettingsLocked 是"把文件解码到
// **当前结构**上"——文件里没有这个键，内存里那份旧值就留着不动。于是：
//
//	控制中心：写空 → 文件里没有 menuHidden → 页面读到"什么都没关"，勾选框回到「开」
//	桌宠：   同一个文件 → 解码不覆盖 → 内存里还是 ["remind"] → 菜单里那一格永远藏着
//
// 两边看到的不是同一份真相，界面上的开关就成了"点了没反应"。重启桌宠会好，因为它从零构造，
// 没有旧值可留——这正是用户说的"退出重开又出现了"。
func TestMenuHiddenCanBeClearedWhileRunning(t *testing.T) {
	oldInterval := settingsReloadInterval
	settingsReloadInterval = 0
	t.Cleanup(func() { settingsReloadInterval = oldInterval })

	dir := t.TempDir()
	pet, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 控制中心关掉「提醒」这一格（它是另一个进程，但走的是同一个文件与同一个入口）。
	if _, err := SaveSettings(dir, func(c *Config) { c.MenuHidden = []string{"remind"} }); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if got := pet.Config().MenuHidden; len(got) != 1 || got[0] != "remind" {
		t.Fatalf("桌宠没读到刚写进去的 menuHidden：%v", got)
	}

	// 用户又把它打开：denylist 清空。
	if _, err := SaveSettings(dir, func(c *Config) { c.MenuHidden = nil }); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	// 桌宠必须跟着清空。它不清空，菜单里那一格就永远回不来——而控制中心会显示"开着"。
	if got := pet.Config().MenuHidden; len(got) != 0 {
		t.Fatalf("桌宠把「提醒」这一格永远藏起来了（内存里还留着 %v）", got)
	}
}

// TestMenuHiddenRoundTripsThroughTheFile 是上一条的镜像：不管空还是非空，写下去再读回来都
// 必须是同一件事。它同时钉住文件里那个键**始终在**——"不在"和"空数组"在解码到已有结构上
// 之后不是一回事。
func TestMenuHiddenRoundTripsThroughTheFile(t *testing.T) {
	dir := t.TempDir()
	for _, want := range [][]string{nil, []string{"snap"}, nil, []string{"theme", "hide"}, nil} {
		if _, err := SaveSettings(dir, func(c *Config) { c.MenuHidden = want }); err != nil {
			t.Fatalf("SaveSettings(%v): %v", want, err)
		}
		fresh, err := New(dir)
		if err != nil {
			t.Fatal(err)
		}
		got := fresh.Config().MenuHidden
		if len(got) != len(want) {
			t.Fatalf("写 %v 读回 %v", want, got)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("写 %v 读回 %v", want, got)
			}
		}
	}
}

// TestClearingMenuHiddenRewritesTheFile pins the file side of the same rule.
//
// 只修"读"是不够的：如果写的时候仍然让 omitempty 把键抹掉，下一次读又会回到上一条的坑里。
// 所以这里直接看文件——那个键必须在，值是空数组。
func TestClearingMenuHiddenRewritesTheFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := SaveSettings(dir, func(c *Config) { c.MenuHidden = []string{"remind"} }); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveSettings(dir, func(c *Config) { c.MenuHidden = nil }); err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := readJSON(settingsPathOf(dir), &raw); err != nil {
		t.Fatalf("读 settings.json：%v", err)
	}
	value, ok := raw["menuHidden"]
	if !ok {
		t.Fatal("settings.json 里没有 menuHidden —— 下一次读会把内存里的旧值留住（这正是那个 bug）")
	}
	if value != nil {
		if slice, isSlice := value.([]any); !isSlice || len(slice) != 0 {
			t.Fatalf("menuHidden 应该是空（null 或 []），实际 %#v", value)
		}
	}
}

// 一个只在测试里用的小工具：settings.json 的路径跟着数据目录走。
var settingsPathCache sync.Map

func settingsPathOf(dir string) string {
	if cached, ok := settingsPathCache.Load(dir); ok {
		return cached.(string)
	}
	path := ""
	if s, err := New(dir); err == nil {
		path = s.settingsPath()
	}
	settingsPathCache.Store(dir, path)
	return path
}
