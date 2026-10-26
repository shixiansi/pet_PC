# proposal: 把番茄钟状态机下沉到 internal/pomodoro

## 为什么

`app_pomodoro.go`（1229 行）+ `pomodoro.go`（395 行）+ 5 个测试文件（约 2000 行）全部
挂在 `package main` 上。宿主根目录现在有 44 个 `.go` 文件、`App` 上一个 155+ 方法的
结构体，任何一处改动都要整包编译、整包测试。

其中 **`pomodoro.go` 是真正纯的**：所有函数只吃 `(store.PomodoroState, store.Config,
time.Time)`，返回 `phaseOutcome`，不碰 App、不碰窗口、不碰 Wails（已核对全文：注释里
提到 app，代码没有引用）。它是最没有争议的抽取对象。

## 目标

1. 状态机的逻辑搬进 `internal/pomodoro`，依赖面被 import 图强制为「只有 internal/store」。
2. `package main` 里的 `pomodoro.go` 从 395 行降到约 70 行，只留接缝。
3. **不改任何调用点语义**：约 110 处引用、5 个测试文件保持原样。

## 非目标（明确留给后续批次）

- 不搬测试。`pomodoro_test.go` 里有 1 个用例用 `App.pomodoroView()`，另外三个
  「gate / plan_paths / gate_plan」文件重度依赖 App（82 / 102 / 86 处），它们测的是
  宿主门控，本来就该留在 main。
- 不改 `app_pomodoro.go` 的结构。它主体是 UI（气泡、槽位、窗口裁剪、Wails 方法），
  「App 方法变薄委托」在这一层做不到。
- 不引入新依赖，不改行为。

## 风险与对策

| 风险 | 对策 |
| --- | --- |
| 别名 + 转发层让调用点「看起来没变」，从而没人再往里加逻辑 | 接缝文件顶部写清这是临时层，tasks 里列了删除它的后续步骤 |
| 未导出方法 `.finished()` 通过类型别名在 main 里不可见 | 改导出为 `Finished()`，只动 7 处调用（app_pomodoro.go 2、pomodoro_test.go 5） |
| 重命名误伤注释或别的同名标识符 | 只对「名字 + 左括号」重命名，类型 `phaseOutcome` 单独按词边界处理；`clockJumpSlack`、`nextPhaseAfter` 保持未导出 |

## 验收

- `go vet ./...` 退出码 0
- `go test ./...` 中 `PetPC`、`internal/pomodoro`、`internal/store` 全绿
- `gofmt -l` 对新文件无输出
- `git diff` 里 `app_pomodoro.go` 只有 2 行变化（`.finished()` → `.Finished()`）
