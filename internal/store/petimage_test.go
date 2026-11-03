package store

import (
	"testing"
)

// TestClearedImageDoesNotComeBack 是 MenuHidden 那条 bug 的**同形状兄弟**，只是没人报过。
//
// 「恢复默认图片」把 `petImageFile` 清成空串，而它同样带着 `omitempty`：键从 settings.json
// 里整条消失 → Store 的懒重读把文件解码到**已有结构**上、不做清零 → 内存里那个文件名还在。
//
// 后果比 menuHidden 温柔（`ImagePath` 还会 stat 一下文件在不在，而 ClearPetImage 已经把文件
// 删了，所以画面上确实会回到默认图），但它仍然是"文件里没有 ≠ 内存里清空"这件事的第二个实例：
// 一个手删了图片文件、又想让设置回到默认的用户，重启之后会看到那个已经不存在的文件名重新回到
// 设置里，而 UI 上写着"当前：pet-image.png"——一张不存在的图。
//
// 修法与 menuHidden 一样：非 omitempty，空值也写出来。
func TestClearedImageDoesNotComeBack(t *testing.T) {
	oldInterval := settingsReloadInterval
	settingsReloadInterval = 0
	t.Cleanup(func() { settingsReloadInterval = oldInterval })

	dir := t.TempDir()
	pet, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 控制中心选了一张图。
	if _, err := SaveSettings(dir, func(c *Config) {
		c.PetImageFile = "pet-image.png"
		c.OriginalPath = `C:\Users\me\cat.png`
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if got := pet.Config().PetImageFile; got != "pet-image.png" {
		t.Fatalf("桌宠没读到刚写进去的图片：%q", got)
	}

	// 又点了「恢复默认」，把两个字段都清空。
	if _, err := SaveSettings(dir, func(c *Config) {
		c.PetImageFile = ""
		c.OriginalPath = ""
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	if got := pet.Config().PetImageFile; got != "" {
		t.Fatalf("桌宠内存里还留着已经删掉的图片名 %q（重启后它会重回设置里）", got)
	}
	if got := pet.Config().OriginalPath; got != "" {
		t.Fatalf("桌宠内存里还留着图片来源 %q", got)
	}
}

// TestClearedImageFieldsStayInTheFile: 与 TestClearingMenuHiddenRewritesTheFile 同一件事，
// 盯的是写出去的那一份。
func TestClearedImageFieldsStayInTheFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := SaveSettings(dir, func(c *Config) { c.PetImageFile = "pet-image.png" }); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveSettings(dir, func(c *Config) { c.PetImageFile = ""; c.OriginalPath = "" }); err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := readJSON(settingsPathOf(dir), &raw); err != nil {
		t.Fatalf("读 settings.json：%v", err)
	}
	for _, key := range []string{"petImageFile", "originalPath"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("settings.json 里没有 %s —— 下一次读会把内存里的旧值留住", key)
		}
	}
}
