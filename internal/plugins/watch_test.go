package plugins

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The pure half of the two watchers, tested where both hosts share it.
//
// 这些断言之所以放在这个包里：`config.changed` 与 `storage.changed` 现在由**两个**宿主发出
// （桌宠与控制中心），而"什么算变了"只能有一份定义——否则同一个插件的两个窗口会对同一次写入
// 给出不同结论。

func TestStampFileAndConfigStamp(t *testing.T) {
	dir := t.TempDir()
	// 不存在 = ok=false（调用方据此把"后来出现了"当成变化）。
	if _, ok := ConfigStamp(dir); ok {
		t.Fatal("a missing config.json must not produce a stamp")
	}
	path := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	when := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	stamp, ok := ConfigStamp(dir)
	if !ok {
		t.Fatal("config.json is there and produced no stamp")
	}
	if stamp.Size != 2 {
		t.Fatalf("size = %d, want 2", stamp.Size)
	}
	if stamp.Mod != when.UnixNano() {
		t.Fatalf("mod = %d, want %d", stamp.Mod, when.UnixNano())
	}
}

func TestPrivateDirStampsSkipsConfigAndUsesStorageKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "outbox"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, body := range map[string]string{
		"state/x":          "abc",
		"outbox/system":    "12",
		ConfigFileName:     "{}",
	} {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	stamps := PrivateDirStamps(dir)
	// 键是**存储键**（斜杠分隔、相对私有目录），不是 OS 路径。
	for _, want := range []string{"state/x", "outbox/system"} {
		if _, ok := stamps[want]; !ok {
			t.Fatalf("stamps = %v, want the key %q", stamps, want)
		}
	}
	if _, ok := stamps[ConfigFileName]; ok {
		t.Fatalf("stamps includes %s: config.changed already reports that file, and two events "+
			"for one save run every plugin's settings handler twice", ConfigFileName)
	}
	if stamps["state/x"].Size != 3 {
		t.Fatalf("state/x size = %d, want 3", stamps["state/x"].Size)
	}
}

func TestChangedStampsCountsAppearingAsChangedAndSorts(t *testing.T) {
	prev := map[string]FileStamp{
		"com.example.b": {Mod: 1, Size: 1},
		"com.example.a": {Mod: 1, Size: 1},
	}
	cur := map[string]FileStamp{
		"com.example.b": {Mod: 1, Size: 1}, // unchanged
		"com.example.a": {Mod: 2, Size: 1}, // touched
		"com.example.c": {Mod: 1, Size: 1}, // appeared
	}
	got := ChangedStamps(prev, cur)
	want := []string{"com.example.a", "com.example.c"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("changed = %v, want %v (sorted, appeared counts)", got, want)
	}
	if again := ChangedStamps(cur, cur); len(again) != 0 {
		t.Fatalf("two identical snapshots reported %v", again)
	}
}

func TestShareTrayName(t *testing.T) {
	for _, ok := range []string{"share/command", "share/media-now"} {
		name, valid := ShareTrayName(ok)
		if !valid || name != ok[len("share/"):] {
			t.Fatalf("ShareTrayName(%q) = (%q,%v), want the name and true", ok, name, valid)
		}
	}
	// share/ 是扁平的：一个名字里再带斜杠就不是宿主写的那种值。
	for _, bad := range []string{"share/", "share/a/b", "state/x", "config.json", ""} {
		if name, valid := ShareTrayName(bad); valid {
			t.Fatalf("ShareTrayName(%q) = (%q,true), want false", bad, name)
		}
	}
}
