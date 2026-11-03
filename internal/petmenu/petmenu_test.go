package petmenu

import (
	"strings"
	"testing"
)

// TestCellsMatchTheMenuMarkup 钉住这张表与它服务的那个菜单是同一个菜单。
//
// 表里的每一格都必须在 markup 里有一格（data-menu），而且是**同一个顺序**：控制中心那一页
// 按表的顺序列开关，markup 按自己的顺序画格子，两个顺序一旦不同，"我关掉的是第三个"这件
// 事就开始骗人。markup 那一侧由 .dev/check-frontend.cjs 比对（它读得到两个文件，这个包
// 读不到 frontend/）。
func TestCellsMatchTheMenuMarkup(t *testing.T) {
	cells := Cells()
	if len(cells) != 9 {
		t.Fatalf("the menu has %d cells in this table, want 9 (the 3x3 grid)", len(cells))
	}
	seen := map[string]bool{}
	for _, cell := range cells {
		if cell.ID == "" || cell.Label == "" {
			t.Errorf("a cell is missing its id or label: %+v", cell)
		}
		if strings.ContainsAny(cell.ID, "ABCDEFGHIJKLMNOPQRSTUVWXYZ _") {
			t.Errorf("%q is not a data-menu id (lowercase, no spaces)", cell.ID)
		}
		if seen[cell.ID] {
			t.Errorf("duplicate id %q", cell.ID)
		}
		seen[cell.ID] = true
	}
}

// TestOnlyTheWayOutAndTheWayBackAreFixed pins the decision itself, not just its
// implementation.
//
// 只有两格固定：退出（桌宠那一侧唯一的退出口）与 控制中心（**改这些开关的地方就在它里面**）。
// 其余七格都关得掉，因为关掉之后还回得去——控制中心永远在。
//
// 这条断言存在的意义是：哪天有人想再加一个 fixed，他得先回答"关掉它之后用户从哪回来"；
// 反过来，想拿掉这两个里的任何一个，得先回答"那用户怎么退出 / 怎么改回来"。
func TestOnlyTheWayOutAndTheWayBackAreFixed(t *testing.T) {
	var fixed []string
	for _, cell := range Cells() {
		if cell.Fixed {
			fixed = append(fixed, cell.ID)
		}
	}
	if len(fixed) != 2 || fixed[0] != "plugins" || fixed[1] != "quit" {
		t.Fatalf("fixed cells = %v, want exactly [plugins quit]", fixed)
	}
}

// TestNormalise covers the rule that makes "只能开不能关" real.
//
// 它是纯函数，也是两个程序共用的那个判据：桌宠在 GetState 里过它，控制中心在写入与读取
// 时都过它。只在一处拦就是给 settings.json 留了个洞——那是一个用户能直接编辑的文件。
func TestNormalise(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty stays empty", nil, nil},
		{"a hideable cell survives", []string{"theme"}, []string{"theme"}},
		{"the way out is dropped", []string{"quit"}, nil},
		{"the way back is dropped", []string{"plugins"}, nil},
		{"the way out is dropped, the rest kept", []string{"quit", "theme"}, []string{"theme"}},
		{"the way back is dropped, the rest kept", []string{"plugins", "snap"}, []string{"snap"}},
		{"unknown ids are dropped", []string{"nope", "sales"}, nil},
		{"duplicates collapse", []string{"theme", "theme"}, []string{"theme"}},
		// 菜单顺序，不是调用方的顺序。
		{"output follows menu order", []string{"hide", "chat", "snap"}, []string{"chat", "snap", "hide"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalise(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("Normalise(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("Normalise(%v) = %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}
}

// TestCellsIsACopy: 调用方拿到的必须是拷贝，否则控制中心那一页的一个手滑就能改掉桌宠的
// 菜单定义（同一个进程里，另一条渲染路径正在读它）。
func TestCellsIsACopy(t *testing.T) {
	cells := Cells()
	cells[0].Label = "改坏了"
	if Cells()[0].Label == "改坏了" {
		t.Fatal("Cells() handed out the package's own slice")
	}
}
