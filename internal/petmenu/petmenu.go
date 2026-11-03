// Package petmenu 是桌宠右键菜单那张格子表，以及"哪些格子不许关"的规则。
//
// # 为什么它不在 package main 里
//
// 这张表有两个消费者，而它们在两个程序里：
//
//   - 桌宠：渲染菜单时读 State.config.MenuHidden，藏掉用户关掉的格子；
//   - 控制中心：那**才是**改这件事的地方（右键菜单里塞一个开关页，把九个格子从 226px
//     里挤掉一条，用户的第一反馈是"显示效果不好"）。控制中心的「开始」页要画出这些开关，
//     所以它也需要这张表（标签、以及哪些固定）。
//
// 两个程序不能共享 package main，但可以共享 internal/。规则只写一份，和
// internal/plugins/share.go 让两个宿主共用牌盘规则是同一个理由：两套规则必然分叉，而
// 分叉的那套比没有更坏（同一个开关在一个窗口能关、在另一个窗口关不掉）。
package petmenu

// Cell 是菜单里的一格。
//
// json 标签写全（id / label / fixed）：这个类型会经 Wails 的绑定过到控制中心的 Vue/TS 那一侧，
// 而 Go 默认的大写字段名会让那一页读 `cell.ID`——同一个仓库里别处的 payload 都是小写
// （store.Config 就是），两套命名只会让下一个读代码的人多查一次。
type Cell struct {
	// ID 与 frontend/index.html 里那一格的 data-menu 逐字相同。
	ID string `json:"id"`
	// Label 是控制中心那一行开关的文字。
	Label string `json:"label"`
	// Fixed 标记「只能开不能关」。
	//
	// 今天只有 退出：它是桌宠那一侧唯一的退出口（托盘也有，但托盘不是有人找"怎么退出"
	// 的地方）。其余八格都关得掉，包括 控制中心 —— 那个程序托盘菜单一直开得出来，而且
	// 用户的这一页本身就在它里面，所以关掉它不会把人关在外面。
	//
	// 反过来说：**没有任何一格是"关掉就回不来"的**，因为"改这些开关"这件事住在控制
	// 中心里，而不是住在菜单里。这正是把开关页搬出菜单换来的东西。
	Fixed bool `json:"fixed"`
}

// cells 是菜单的九格，顺序就是 markup 的顺序（控制中心那一页也按这个顺序列）。
var cells = []Cell{
	{"todos", "日程", false},
	{"chat", "对话", false},
	{"pomodoro", "番茄钟", false},
	{"remind", "提醒", false},
	{"plugins", "控制中心", false},
	{"theme", "主题", false},
	{"snap", "归位", false},
	{"hide", "隐藏", false},
	{"quit", "退出", true},
}

// Cells 返回菜单的全部格子（一份拷贝：调用方改不动这张表）。
func Cells() []Cell {
	out := make([]Cell, len(cells))
	copy(out, cells)
	return out
}

// Normalise 把一份"用户想藏掉的格子"筛成能存的那一份：只留下存在的、可以关的，
// 去重，并按菜单顺序排列。
//
// 三个"为什么"：
//
//   - 未知 id 丢掉，而不是留着。留着的后果是它永远待在文件里；哪天那个 id 被复用成
//     另一格，它会带着旧含义回来。
//   - Fixed 丢掉。settings.json 是一个用户能直接编辑的纯文件，所以这条规则必须在**读**的
//     那一侧也成立（两个程序的读取路径都过这里），而不只是在写入时拦一下。
//   - 按菜单顺序输出：同一套选择永远生成同一份字节，settings.json 的 diff 里只会出现
//     真正改了的那一行。
func Normalise(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []string
	for _, cell := range cells {
		if cell.Fixed || !want[cell.ID] {
			continue
		}
		out = append(out, cell.ID)
	}
	return out
}
