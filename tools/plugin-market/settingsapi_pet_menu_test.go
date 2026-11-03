package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"PetPC/internal/petmenu"
	"PetPC/internal/store"
)

// 控制中心这一侧的「右键菜单显示哪几格」：绑定本身 + 归一化。
//
// 2026-11-03 用户报的那条（"关掉提醒再打开，右键菜单里那一格直接没了，开关都没反应"）根因在
// Store：`menuHidden` 清空后 `omitempty` 让键从 settings.json 里消失，而桌宠的懒重读是
// "解码到已有结构上"——键不在就不覆盖，内存里留着旧值。**修在 internal/store**（见
// menuhidden_test.go，那里能控制重读节流，才测得出"陈旧内存"这一半）。
//
// 这个文件守的是另一半：这一页写下去的必须是归一化之后的那一份，而且落盘要能读回来。绑定如果
// 原样存 fixed 的 id（或未知 id），桌宠那边照样会把它剔掉，但文件会一直脏着——而"一直脏着"
// 正是下一次读出新旧混杂的地方。

func newMenuMarket(t *testing.T) *Market {
	t.Helper()
	return &Market{dataDir: t.TempDir()}
}

// TestSetPetMenuHiddenStoresTheNormalisedList: fixed（控制中心 / 退出）与未知 id 不许落盘。
//
// 不能指望页面：手改过的 settings.json、一个过期的页面，都可能把这些 id 发上来。判据只有一份
// （petmenu.Normalise），写入路径必须自己过它。
func TestSetPetMenuHiddenStoresTheNormalisedList(t *testing.T) {
	m := newMenuMarket(t)

	cfg, err := m.SetPetMenuHidden([]string{"plugins", "quit", "remind", "nonsense", "remind"})
	if err != nil {
		t.Fatalf("SetPetMenuHidden: %v", err)
	}
	if got := cfg.MenuHidden; len(got) != 1 || got[0] != "remind" {
		t.Fatalf("返回的配置里是 %v，want [remind]", got)
	}

	var raw map[string]any
	if err := readSettingsFile(filepath.Join(m.dataDir, "settings.json"), &raw); err != nil {
		t.Fatalf("读 settings.json: %v", err)
	}
	slice, _ := raw["menuHidden"].([]any)
	if len(slice) != 1 || slice[0] != "remind" {
		t.Fatalf("settings.json 里的 menuHidden 是 %#v，want [remind]", raw["menuHidden"])
	}
}

// TestSetPetMenuHiddenClearsToAnEmptyListNotAMissingKey 是那个 bug 的**文件侧**：
// 打开最后一格之后，`menuHidden` 必须还是文件里的一个键（空数组 / null），不能整个消失。
//
// 键一消失，`refreshSettingsLocked`（解码到已有结构上）就会把内存里那份旧值留住 —— 于是
// 控制中心读成"没关"、桌宠读成"还关着"，同一份文件两个结论。
func TestSetPetMenuHiddenClearsToAnEmptyListNotAMissingKey(t *testing.T) {
	m := newMenuMarket(t)

	if _, err := m.SetPetMenuHidden([]string{"remind"}); err != nil {
		t.Fatalf("SetPetMenuHidden(关): %v", err)
	}
	if _, err := m.SetPetMenuHidden(nil); err != nil {
		t.Fatalf("SetPetMenuHidden(开): %v", err)
	}

	var raw map[string]any
	if err := readSettingsFile(filepath.Join(m.dataDir, "settings.json"), &raw); err != nil {
		t.Fatalf("读 settings.json: %v", err)
	}
	value, ok := raw["menuHidden"]
	if !ok {
		t.Fatal("打开之后 settings.json 里没有 menuHidden —— 桌宠下次重读会把旧值留住，" +
			"菜单里那一格就永远回不来（2026-11-03 的 bug）")
	}
	if value != nil {
		if slice, isSlice := value.([]any); !isSlice || len(slice) != 0 {
			t.Fatalf("menuHidden 应该是空（null 或 []），实际 %#v", value)
		}
	}
}

// TestSetPetMenuHiddenIsIdempotent: 同样的选择存两次，文件字节不变。
//
// 不是洁癖：这一页每点一下就写一次文件，而桌宠的重读是按 mtime+size 判断"变了没有"的。
// 每次点都产生一份新的字节，会让那次比较永远为真（白重读），而日志里也会多出一串无意义的
// "外部改动了右键菜单的开关"。
func TestSetPetMenuHiddenIsIdempotent(t *testing.T) {
	m := newMenuMarket(t)
	path := filepath.Join(m.dataDir, "settings.json")

	if _, err := m.SetPetMenuHidden([]string{"theme", "snap"}); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 顺序反着喂同一组：归一化按菜单顺序输出，所以落盘的那一份必须一模一样。
	if _, err := m.SetPetMenuHidden([]string{"snap", "theme"}); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("同一组选择写出了两份不同的文件:\n%s\n---\n%s", first, second)
	}
}

// TestPetMenuCellsAreTheSharedTable: 这一页画的行必须和桌宠判的那张表是同一张。
//
// 页面上多一行 / 少一行 / fixed 标错，用户就会看到"开着一个关不掉的开关"或者"关掉一格之后
// 桌宠那边没反应"。两边共用 internal/petmenu，这条断言守的是"这一页真的用了它"。
func TestPetMenuCellsAreTheSharedTable(t *testing.T) {
	m := newMenuMarket(t)
	cells := m.PetMenuCells()
	want := petmenu.Cells()
	if len(cells) != len(want) {
		t.Fatalf("控制中心画了 %d 行，共享表里有 %d 格", len(cells), len(want))
	}
	for i := range want {
		if cells[i].ID != want[i].ID || cells[i].Label != want[i].Label || cells[i].Fixed != want[i].Fixed {
			t.Fatalf("第 %d 行是 %+v，共享表里是 %+v", i, cells[i], want[i])
		}
	}
}

// TestPetMenuCellsSurviveTheRoundTripThroughTheStore: 存了再读，归一化之后的列表不变。
//
// 这一条把两个程序串起来：控制中心写、Store 读（桌宠走的是同一个 Store 类型）。它抓到的是
// "写入正常化了但读取没正常化"这种半修——那样手改过的文件仍然能把 fixed 的格子藏起来。
func TestPetMenuCellsSurviveTheRoundTripThroughTheStore(t *testing.T) {
	m := newMenuMarket(t)
	if _, err := m.SetPetMenuHidden([]string{"hide", "nonsense", "quit"}); err != nil {
		t.Fatal(err)
	}
	// 新的 Store = 桌宠那边刚起来的进程。
	s, err := store.New(m.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Config().MenuHidden; len(got) != 1 || got[0] != "hide" {
		t.Fatalf("读回来是 %v，want [hide]", got)
	}
}

// readSettingsFile 读一份 JSON 文件（不经过 Store：这些断言要看的是落盘的那一份）。
func readSettingsFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// TestMainSettingsNormalisesTheMenuHiddenList 是上面那段注释的用例。
//
// 一份被手改过（或旧版本写下）的 settings.json 里可能躺着 "plugins" / "quit" —— 那两格在
// 菜单里是 fixed（回家与出门）。如果这一页不归一化就读，它们会显示成**未勾选、又因为 fixed
// 点不动**：两个灰掉的空框，而桌宠那边归一化过、照样画着它们。用户看到的就是"开关都没反应"
// 的第一种形状。
func TestMainSettingsNormalisesTheMenuHiddenList(t *testing.T) {
	dir := t.TempDir()
	// 直接写文件，绕开这一页的写入路径（模拟手改与旧版本）。
	if err := writeSettingsFile(filepath.Join(dir, "settings.json"), map[string]any{
		"menuHidden": []string{"plugins", "quit", "remind"},
	}); err != nil {
		t.Fatal(err)
	}

	m := &Market{dataDir: dir}
	cfg, err := m.MainSettings()
	if err != nil {
		t.Fatalf("MainSettings: %v", err)
	}
	if got := cfg.MenuHidden; len(got) != 1 || got[0] != "remind" {
		t.Fatalf("MainSettings 回的是 %v，want [remind]（fixed 的格子必须读的时候就被剔掉）", got)
	}
}

// TestMenuCellStatesMatchBetweenTheTwoPrograms 把两个程序的读路径比一遍。
//
// 同一个 id 集合，桌宠（App.GetState → petmenu.Normalise）与控制中心（Market.MainSettings）
// 必须得出同一个答案。它们今天用的是同一个函数——这条断言守的是"将来有人只在一处加了逻辑"。
func TestMenuCellStatesMatchBetweenTheTwoPrograms(t *testing.T) {
	dir := t.TempDir()
	if err := writeSettingsFile(filepath.Join(dir, "settings.json"), map[string]any{
		"menuHidden": []string{"quit", "snap", "unknown-id", "snap"},
	}); err != nil {
		t.Fatal(err)
	}

	m := &Market{dataDir: dir}
	fromMarket, err := m.MainSettings()
	if err != nil {
		t.Fatal(err)
	}

	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	fromPet := petmenu.Normalise(s.Config().MenuHidden)

	if len(fromMarket.MenuHidden) != len(fromPet) {
		t.Fatalf("控制中心看到 %v，桌宠看到 %v", fromMarket.MenuHidden, fromPet)
	}
	for i := range fromPet {
		if fromMarket.MenuHidden[i] != fromPet[i] {
			t.Fatalf("控制中心看到 %v，桌宠看到 %v", fromMarket.MenuHidden, fromPet)
		}
	}
}

// writeSettingsFile 直接写一份 settings.json（这些用例要模拟"文件不是这一页写的"）。
func writeSettingsFile(path string, values map[string]any) error {
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
