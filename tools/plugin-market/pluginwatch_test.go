package main

// The control center's "what changed" decision, pinned without a Wails context.
//
// 为什么这些用例值得写：这两个事件以前只有桌宠会发，控制中心里的帧就只能轮询——todo 每 2 秒
// `storage.get`、ast-coder 每 5 秒 `config.get`。规则本身（compare / seed / tray 路由 / 每拍上限）
// 是纯的，所以就按纯函数测，不启窗口。

import (
	"testing"

	"PetPC/internal/plugins"
)

func stamp(mod int64) plugins.FileStamp { return plugins.FileStamp{Mod: mod, Size: 1} }

func TestMarketEventsSeedsWithoutSpeaking(t *testing.T) {
	cur := map[string]plugins.FileStamp{"com.example.a": stamp(1)}
	storage := map[string]map[string]plugins.FileStamp{"com.example.a": {"state/x": stamp(1)}}
	// 第一次扫描只播种：挂载中的帧还没握完手，那时候告诉它"你的数据变了"是它无法处理的噪音。
	if got := marketEvents(nil, cur, nil, storage, false); len(got) != 0 {
		t.Fatalf("first pass emitted %d events, want 0", len(got))
	}
}

func TestMarketEventsReportsConfigAndStorage(t *testing.T) {
	prevCfg := map[string]plugins.FileStamp{"com.example.a": stamp(1), "com.example.b": stamp(1)}
	curCfg := map[string]plugins.FileStamp{"com.example.a": stamp(2), "com.example.b": stamp(1)}
	prevSt := map[string]map[string]plugins.FileStamp{
		"com.example.a": {"state/x": stamp(1)},
	}
	curSt := map[string]map[string]plugins.FileStamp{
		"com.example.a": {"state/x": stamp(1), "state/y": stamp(1)}, // y appeared
	}
	got := marketEvents(prevCfg, curCfg, prevSt, curSt, true)
	var names []string
	for _, event := range got {
		names = append(names, event.ID+"|"+event.Name)
	}
	want := []string{"com.example.a|config.changed", "com.example.a|storage.changed"}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("events = %v, want %v", names, want)
		}
	}
	if key := got[1].Payload["key"]; key != "state/y" {
		t.Fatalf("storage payload key = %v, want the key that appeared", key)
	}
	if id := got[0].Payload["pluginId"]; id != "com.example.a" {
		t.Fatalf("config payload pluginId = %v, want the plugin whose file changed", id)
	}
}

func TestMarketEventsRoutesTheTrayAsShareChanged(t *testing.T) {
	// 牌盘值住在发布者自己的目录里，所以这个走查也会看见它——而它要按 share.changed 发，
	// 那正是"另一个程序写的发布到达这一侧帧"的通道（桌宠那边有对称的一段）。
	cur := map[string]map[string]plugins.FileStamp{
		"com.example.owner": {"share/command": stamp(1), "state/x": stamp(1)},
	}
	got := marketEvents(nil, nil, map[string]map[string]plugins.FileStamp{}, cur, true)
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2 (one share.changed + one storage.changed)", len(got))
	}
	var share, storage *pluginEvent
	for i := range got {
		switch got[i].Name {
		case plugins.EventShareChanged:
			share = &got[i]
		case plugins.EventStorageChanged:
			storage = &got[i]
		}
	}
	if share == nil || storage == nil {
		t.Fatalf("events = %+v, want one of each", got)
	}
	if share.Payload["key"] != "com.example.owner/command" || share.Payload["name"] != "command" {
		t.Fatalf("share payload = %v, want the <owner>/<name> pair", share.Payload)
	}
	if storage.Payload["key"] != "state/x" {
		t.Fatalf("storage payload = %v, want the plugin's own key", storage.Payload)
	}
}

func TestMarketEventsCapsOneTickPerPlugin(t *testing.T) {
	cur := map[string]map[string]plugins.FileStamp{}
	for i := 0; i < maxStorageEventsPerTick+4; i++ {
		cur["com.example.a"] = map[string]plugins.FileStamp{}
	}
	keys := map[string]plugins.FileStamp{}
	for i := 0; i < maxStorageEventsPerTick+4; i++ {
		keys[string(rune('a'+i))] = stamp(int64(i + 1))
	}
	cur["com.example.a"] = keys
	got := marketEvents(nil, nil, map[string]map[string]plugins.FileStamp{}, cur, true)
	if len(got) != maxStorageEventsPerTick {
		t.Fatalf("events = %d, want the cap %d", len(got), maxStorageEventsPerTick)
	}
}

func TestMarketEventsIsSilentWhenNothingChanged(t *testing.T) {
	snap := map[string]plugins.FileStamp{"com.example.a": stamp(1)}
	st := map[string]map[string]plugins.FileStamp{"com.example.a": {"state/x": stamp(1)}}
	if got := marketEvents(snap, snap, st, st, true); len(got) != 0 {
		t.Fatalf("nothing changed but %d events came out", len(got))
	}
}
