package plugins

import (
	"encoding/json"
	"os"
	"strconv"
	"path/filepath"
	"testing"
)

/*
 * 占用清单：量出多少、按插件自己的分级摊开、只有 evictable 的部分才可能被清理。
 *
 * WHY THIS FILE EXISTS. 2026-11-01 之前这里是一道容量上限（64 MiB，超过就拒绝写入）。它没有
 * 阻止任何事，反而逼每个需要空间的插件自己发明淘汰策略，而 qq 插件的策略先删掉用户 318 张
 * 表情、再删掉 318 条识图换来的记录。上限删掉之后的替代品就是这份"量出来给人看"的数字，
 * 所以它的两条性质必须是硬的：
 *
 *   1. 数字是真的（走整棵树，不估算）；
 *   2. 分级只信插件自己，宿主猜的一律 unknown —— 猜错的方向是把用户的图标成"可清理"。
 */

func writeFile(t *testing.T, dir, rel string, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMeasureOccupancyCountsEveryFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "media/stickers/bytes/a", "0123456789")
	writeFile(t, dir, "media/stickers/bytes/b", "0123456789")
	writeFile(t, dir, "media/video/tmp/c", "0123456789")
	writeFile(t, dir, "state/index", "0123456789")
	writeFile(t, dir, "config.json", "0123456789")

	occ := MeasureOccupancy(dir, "com.example.x", "示例", nil)
	if occ.Bytes != 50 {
		t.Fatalf("总字节 = %d，want 50（每个文件 10 字节，共 5 个）", occ.Bytes)
	}
	if occ.Files != 5 {
		t.Fatalf("文件数 = %d，want 5", occ.Files)
	}
	// 每个文件夹一行，而且按字节倒序：最大的一块排最前，这是用户最先要看的。
	keys := make([]string, 0, len(occ.Folders))
	for _, f := range occ.Folders {
		keys = append(keys, f.Key)
	}
	if len(keys) != 4 {
		t.Fatalf("文件夹数 = %d（media/stickers/bytes、media/video/tmp、state、根），得到 %v", len(keys), keys)
	}
	// 没有任何分级时，一个字节都不该算作可清理。
	if occ.EvictableBytes != 0 {
		t.Fatalf("没有分级时 EvictableBytes = %d，必须为 0（宿主不猜）", occ.EvictableBytes)
	}
	for _, f := range occ.Folders {
		if f.Grade != GradeUnknown {
			t.Fatalf("%s 的等级 = %q，没有分级时必须 unknown", f.Key, f.Grade)
		}
	}
}

func TestMeasureOccupancyTrustsOnlyThePluginsOwnGrading(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "media/stickers/bytes/a", "0123456789")
	writeFile(t, dir, "media/video/tmp/b", "0123456789")
	writeFile(t, dir, "media/stickers/index", "0123456789")

	// 插件的分级：中间产物可清理，表情包是用户数据。
	writeFile(t, dir, OccupancyHintKey, `[
		{"key":"media/video/tmp","grade":"evictable","label":"下载中间产物"},
		{"key":"media/stickers/bytes","grade":"user-data","label":"你的表情包"},
		{"key":"media/stickers","grade":"user-data","label":"表情索引"}
	]`)

	hints := ReadOccupancyHints(dir)
	occ := MeasureOccupancy(dir, "com.example.y", "示例", hints)

	if occ.EvictableBytes != 10 {
		t.Fatalf("EvictableBytes = %d，want 10（只有 media/video/tmp 那一个文件）", occ.EvictableBytes)
	}
	for _, f := range occ.Folders {
		switch f.Key {
		case "media/video/tmp":
			if f.Grade != GradeEvictable {
				t.Fatalf("media/video/tmp 的等级 = %q，插件说它是 evictable", f.Grade)
			}
			if f.Label != "下载中间产物" {
				t.Fatalf("label = %q，应该用插件自己写的字", f.Label)
			}
		case "media/stickers/bytes":
			if f.Grade != GradeUserData {
				t.Fatalf("表情包被标成了 %q——这正是 2026-11-01 那次事故的形状", f.Grade)
			}
		}
	}
}

func TestReadOccupancyHintsToleratesEveryKindOfBadInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing", ""},
		{"not json", "{"},
		{"wrong shape", `{"key":"media/x"}`},
		{"unknown grade", `[{"key":"media/x","grade":"whatever"}]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.body != "" {
				writeFile(t, dir, OccupancyHintKey, c.body)
			}
			hints := ReadOccupancyHints(dir)
			if len(hints) != 0 {
				t.Fatalf("坏输入应该读成空分级，得到 %v", hints)
			}
		})
	}
}

func TestStorageSetNoLongerRefusesForTotalSize(t *testing.T) {
	// 这条路曾经有第二道闸门：整目录超 64 MiB 就拒写。删掉之后，唯一会拒绝的是"单值超过
	// MaxStorageValue"（形状规则）。这条测试钉住"总量不再被拒"——也就是用户能存下自己的东西。
	dir := t.TempDir()
	for i := 0; i < 90; i++ {
		key := "media/stickers/bytes/" + strconv.Itoa(i)
		body := make([]byte, 900*1024) // 每块 900 KiB，90 块 ≈ 79 MiB，远超旧上限
		payload, err := json.Marshal(map[string]any{"key": key, "value": string(body)})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := StorageSet(dir, payload); err != nil {
			t.Fatalf("第 %d 次写入被拒：%v（总量不该再有上限）", i, err)
		}
	}
	if got := PluginDataBytes(dir); got < 64<<20 {
		t.Fatalf("目录只有 %d 字节，测试没真的超过旧上限", got)
	}
}

func TestStorageSetStillRefusesOneHugeValue(t *testing.T) {
	// 保留的那一条是形状规则：一个键不该是 blob。它不会对表情库发生（一张图一个键）。
	dir := t.TempDir()
	body := make([]byte, MaxStorageValue+1)
	payload, marshalErr := json.Marshal(map[string]any{"key": "one", "value": string(body)})
	if marshalErr != nil {
		t.Fatalf("marshal: %v", marshalErr)
	}
	_, err := StorageSet(dir, payload)
	if err == nil {
		t.Fatal("单值超过 MaxStorageValue 应该被拒")
	}
}
