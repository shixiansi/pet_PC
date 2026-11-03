package store

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestSettingsFileCarriesEveryPreferenceKey 是 MenuHidden / PetImageFile 那两条 bug 的根目录
// 级守卫。
//
// `refreshSettingsLocked` 解码时是"把文件读进**已有结构**"，它的注释写着"文件里带着每一个
// preference 键，所以每一个都会被覆盖"。**那句话曾经是错的**：任何带 `omitempty` 的 preference
// 字段一旦被清空，键就从文件里消失，解码时那个字段原地不动——内存里留着旧值，于是
//
//	控制中心：写空 → 读回自己刚写的空 → 勾选框回到「开」
//	桌宠：  同一个文件 → 旧值还在 → 菜单里那一格永远藏着
//
// 两个人看着同一份文件得出相反的结论，而重启（从零构造，没有旧值可留）又是对的：
// "退出重开就好了"。2026-11-03 的「提醒关了再开就没了」就是这一条。
//
// 所以这条断言守的是**那句注释**本身：settings.json 里必须有每一个 preference 键，包括
// 清空之后的空值。谁再给某个 preference 加 `omitempty`，它会在这里红——而不是等到用户在
// 界面上点了"关掉再打开"之后。
func TestSettingsFileCarriesEveryPreferenceKey(t *testing.T) {
	dir := t.TempDir()
	// 一份"全清空"的配置：所有字符串/切片都空、所有开关都关。这正是 omitempty 会吃掉键的
	// 那个状态。
	if _, err := SaveSettings(dir, func(c *Config) {
		*c = Config{}
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := readJSON(settingsPathOf(dir), &raw); err != nil {
		t.Fatalf("读 settings.json：%v", err)
	}

	// runtimeKeys 是**故意**不写进 settings.json 的（它们属于 config.json，写进来就等于让
	// 桌宠成为偏好的第二个写者）。
	runtime := make(map[string]bool, len(runtimeKeys))
	for _, key := range runtimeKeys {
		runtime[key] = true
	}

	typ := reflect.TypeOf(Config{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		key := field.Tag.Get("json")
		if key == "" || key == "-" {
			continue
		}
		if idx := indexOfComma(key); idx >= 0 {
			key = key[:idx]
		}
		if runtime[key] {
			continue
		}
		if _, ok := raw[key]; !ok {
			t.Errorf("settings.json 里没有 %q（Config.%s）：解码到已有结构上时它会保留旧值，"+
				"于是控制中心与桌宠会看着同一份文件得出相反的结论。"+
				"去掉这个字段的 omitempty，或者把它加进 runtimeKeys。", key, field.Name)
		}
	}
}

// indexOfComma 找 json tag 里第一个逗号（"name,omitempty" 的形状）。
func indexOfComma(tag string) int {
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' {
			return i
		}
	}
	return -1
}
