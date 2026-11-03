package main

import (
	"testing"

	"PetPC/internal/store"
)

// TestSyncMenuHiddenPushesState covers the "实时生效" half of the right-click menu
// switches.
//
// 写入者是控制中心，读者是桌宠那一页。没有这一推，用户在控制中心关掉一格、回到桌宠右键，
// 菜单还是九格——用户的原话就是"这里设置不能实时生效右键菜单"。而推错方向同样是个 bug：
// 每次 tick 都推一遍会让页面每秒重画一次（applyState 里有 renderPet / renderBadge），
// 日志也会被 INFO 淹没。
func TestSyncMenuHiddenPushesState(t *testing.T) {
	a := newTestApp(t)
	var events []string
	a.emitFn = func(event string, _ ...any) { events = append(events, event) }

	// 第一次：文件里说关掉「主题」，而这一侧应用的是"什么都没关" → 推一次。
	applied := a.syncMenuHidden([]string{"theme"}, nil)
	if len(events) != 1 || events[0] != eventPetState {
		t.Fatalf("events = %v, want one %s", events, eventPetState)
	}
	if len(applied) != 1 || applied[0] != "theme" {
		t.Fatalf("applied = %v, want [theme]", applied)
	}

	// 同样的值再来一遍：不推。这是"每秒一拍也不吵"的那一半。
	applied = a.syncMenuHidden([]string{"theme"}, applied)
	if len(events) != 1 {
		t.Fatalf("a second identical tick pushed again: %v", events)
	}

	// 全开回来：也是一次变化，也要推。
	applied = a.syncMenuHidden(nil, applied)
	if len(events) != 2 || len(applied) != 0 {
		t.Fatalf("clearing must push once: events = %v, applied = %v", events, applied)
	}

	// 归一化之后没变（未知 id 或多写一格 fixed）就不该推：文件里多一行废话不是一次变化。
	if got := a.syncMenuHidden([]string{"nonsense", "quit", "plugins"}, applied); len(got) != 0 || len(events) != 2 {
		t.Fatalf("a no-op normalisation pushed: events = %v, applied = %v", events, got)
	}
}

// TestSyncMenuHiddenIgnoresAFixedCell: 一个手改过的文件写着 menuHidden:["plugins"]，
// 推给页面的那一份里**不能**有它——否则「控制中心」在菜单上消失，而改这些开关的地方就在
// 控制中心里（这正是 2026-11-03 用户要求的：控制中心也不能关）。
//
// 顺序按真实的那一条走：文件先变（控制中心写的），ticker 才看见。所以这里也先写文件、
// 再把读回来的值喂给 syncMenuHidden——推出去的那份是 GetState() 的结果，和页面真正收到的
// 完全一样。
func TestSyncMenuHiddenIgnoresAFixedCell(t *testing.T) {
	a := newTestApp(t)
	var pushed []store.Config
	a.emitFn = func(event string, data ...any) {
		if event != eventPetState || len(data) == 0 {
			return
		}
		if state, ok := data[0].(State); ok {
			pushed = append(pushed, state.Config)
		}
	}

	// 只有 fixed 的格子：没有东西可关，也就没有东西要推。
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.MenuHidden = []string{"plugins", "quit"}
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if got := a.syncMenuHidden(a.store.Config().MenuHidden, nil); len(got) != 0 {
		t.Fatalf("applied = %v, want nothing (both are fixed)", got)
	}
	if len(pushed) != 0 {
		t.Fatalf("nothing was applied, so nothing should have been pushed: %v", pushed)
	}

	// 真的关了一格才推，而且推出去的那份里没有 fixed 的格子。
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.MenuHidden = []string{"plugins", "snap"}
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	a.syncMenuHidden(a.store.Config().MenuHidden, nil)
	if len(pushed) != 1 {
		t.Fatalf("want exactly one push, got %d", len(pushed))
	}
	if got := pushed[0].MenuHidden; len(got) != 1 || got[0] != "snap" {
		t.Fatalf("the pushed state carries %v, want [snap]", got)
	}
}
