package themepacks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 从磁盘读主题包：能用的装进来，不能用的**说清楚为什么**。
//
// WHY THE ERROR TEXT IS TESTED: a pack that silently does nothing is the worst outcome for a user who
// just installed one - they have no way to tell "the pack is broken" from "the host ignores packs".
// Every rejection below therefore has to name the field.

func writeManifest(t *testing.T, dataDir, dirName, body string) {
	t.Helper()
	dir := filepath.Join(dataDir, "themes", dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const goodLight = `"bg":"#f2fdfb","bg-raised":"#ffffff","bg-sunken":"#e6fffb","bg-active":"#d5f7f2",
 "text":"#0c201d","text-dim":"#4b6b67","accent":"#0f766e","accent-strong":"#0f766e","accent-soft":"#e6fffb"`

func TestInstalledPackLoadsAndPaints(t *testing.T) {
	dataDir := t.TempDir()
	writeManifest(t, dataDir, "mint", `{"name":"薄荷气泡","author":"Yue","version":"0.8.1","tokens":{`+
		`"light":{`+goodLight+`},"dark":{`+strings.ReplaceAll(goodLight, "#f2fdfb", "#0b1a18")+`}}}`)

	set := NewSet(dataDir)
	entries := set.Entries()
	var found *Entry
	for i := range entries {
		if entries[i].ID == "mint" {
			found = &entries[i]
		}
	}
	if found == nil {
		t.Fatalf("装进来的包没有出现在清单里：%+v", entries)
	}
	if found.Name != "薄荷气泡" || found.Author != "Yue" || found.Version != "0.8.1" {
		t.Fatalf("清单里的元信息不对：%+v", found)
	}
	if found.Builtin || found.Error != "" {
		t.Fatalf("它应该是已安装、可用的：%+v", found)
	}
	tag := set.CSS("light", "mint")
	if !strings.Contains(tag, `:root:root{`) || !strings.Contains(tag, "--accent:#0f766e;") {
		t.Fatalf("注入内容不对：%s", tag)
	}
	// 内建包仍然走内建表，即使磁盘上有同名目录（下面的用例证明同名会被拒）。
	if !strings.Contains(set.CSS("light", "sakura"), "--accent:#be1857;") {
		t.Fatal("内建包被用户的目录盖掉了")
	}
	if got := set.CSS("light", ""); got != "" {
		t.Fatalf("没有包时不该有注入，得到 %q", got)
	}
}

func TestBrokenPacksAreListedWithAReason(t *testing.T) {
	cases := []struct {
		name    string
		dirName string
		body    string
		want    string
	}{
		{"not json", "broken", `{`, "不是合法 JSON"},
		{"missing dark", "half", `{"tokens":{"light":{` + goodLight + `}}}`, "tokens.dark"},
		{"token name not allowed", "evil", `{"tokens":{"light":{` + goodLight + `,"--x":"#ffffff"},"dark":{` + goodLight + `}}}`, "白名单"},
		{"value is not a colour", "url", `{"tokens":{"light":{` + goodLight + `,"accent":"url(http://x/a)"},"dark":{` + goodLight + `}}}`, "颜色字面量"},
		{"id does not match directory", "mine", `{"id":"other","tokens":{"light":{` + goodLight + `},"dark":{` + goodLight + `}}}`, "不一致"},
		{"missing skeleton token", "thin", `{"tokens":{"light":{"accent":"#123456"},"dark":{"accent":"#123456"}}}`, "骨架令牌"},
		{"builtin id", "sakura", `{"tokens":{"light":{` + goodLight + `},"dark":{` + goodLight + `}}}`, "内建包"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dataDir := t.TempDir()
			writeManifest(t, dataDir, c.dirName, c.body)
			set := NewSet(dataDir)
			var found *Entry
			entries := set.Entries()
			for i := range entries {
				if entries[i].ID == c.dirName {
					found = &entries[i]
				}
			}
			if found == nil {
				t.Fatalf("坏掉的包也应该列出来（否则用户不知道为什么它没生效）：%+v", entries)
			}
			if found.Error == "" {
				t.Fatalf("应该带上原因：%+v", found)
			}
			if !strings.Contains(found.Error, c.want) {
				t.Fatalf("原因里应包含 %q，实际是 %q", c.want, found.Error)
			}
			// 内建 id 的那一例特殊：包被拒，但那个 id 依然可用——用的是**内建**的那一份，
			// 这正是"用户不能覆盖内建包"的意思。
			if c.dirName == "sakura" {
				if got := set.CSS("light", "sakura"); got != StyleTag("light", "sakura") {
					t.Fatalf("内建包被用户目录替换了：%s", got)
				}
				return
			}
			if got := set.CSS("light", c.dirName); got != "" {
				t.Fatalf("坏掉的包不能被应用，得到 %q", got)
			}
		})
	}
}

func TestPackDirectoryNamesMustBeSafe(t *testing.T) {
	dataDir := t.TempDir()
	// 目录名会变成 id，而 id 是路径的一段。大写与点号都不该通过。
	for _, bad := range []string{"Mint", "mint.x", "mint x"} {
		writeManifest(t, dataDir, bad, `{"tokens":{"light":{`+goodLight+`},"dark":{`+goodLight+`}}}`)
	}
	set := NewSet(dataDir)
	for _, bad := range []string{"Mint", "mint.x", "mint x"} {
		var found *Entry
		for _, e := range set.Entries() {
			if e.ID == bad {
				found = &e
			}
		}
		if found == nil || found.Error == "" {
			t.Fatalf("目录名 %q 应该被拒（id 是路径的一段）", bad)
		}
	}
}

func TestLowContrastPacksAreListedButStillUsable(t *testing.T) {
	dataDir := t.TempDir()
	// #bbbbbb 压在白底上是 1.9:1 —— 用户要是自己写一个这样的包，宿主该做的是说出来，
	// 而不是替他决定。
	writeManifest(t, dataDir, "washed", `{"tokens":{"light":{`+
		`"bg":"#ffffff","bg-raised":"#ffffff","bg-sunken":"#ffffff","bg-active":"#ffffff",
		 "text":"#111111","text-dim":"#bbbbbb","accent":"#777777","accent-strong":"#777777","accent-soft":"#ffffff"},
		"dark":{`+goodLight+`}}}`)
	set := NewSet(dataDir)
	var found *Entry
	for _, e := range set.Entries() {
		if e.ID == "washed" {
			found = &e
		}
	}
	if found == nil {
		t.Fatal("没有列出来")
	}
	if len(found.Problems) == 0 {
		t.Fatal("对比度不合格应该被报告出来")
	}
	joined := strings.Join(found.Problems, "; ")
	if !strings.Contains(joined, "--text-dim") || !strings.Contains(joined, "浅色") {
		t.Fatalf("报告应该说清是哪一颗、哪一侧：%s", joined)
	}
	if got := set.CSS("light", "washed"); got == "" {
		t.Fatal("有问题的包仍然可以被应用（这是用户的选择）")
	}
}

func TestContrastRatioMatchesTheNodeGate(t *testing.T) {
	// 与 .dev/check-contrast.cjs 同一组数字：两套实现必须量出同一个值，否则"界面说 5.0、
	// 门禁说 4.4"会变成一场没人能裁决的争论。
	cases := []struct {
		fg, bg string
		want   float64
	}{
		{"#a6b1c0", "#f6f7f9", 2.03},
		{"#7a8798", "#f6f7f9", 3.41},
		{"#5b6472", "#f6f7f9", 5.58},
		{"#7c3aed", "#1e1b3a", 2.89},
		{"#a78bfa", "#1e1b3a", 6.06},
		{"#ffffff", "#6d28d9", 7.10},
	}
	for _, c := range cases {
		got, err := ContrastRatio(c.fg, c.bg)
		if err != nil {
			t.Fatalf("%s on %s：%v", c.fg, c.bg, err)
		}
		if diff := got - c.want; diff > 0.02 || diff < -0.02 {
			t.Fatalf("%s on %s = %.2f，门禁算的是 %.2f", c.fg, c.bg, got, c.want)
		}
	}
	if _, err := ContrastRatio("red", "#fff"); err == nil {
		t.Fatal("认不出的颜色应该报错，而不是猜一个数")
	}
}
