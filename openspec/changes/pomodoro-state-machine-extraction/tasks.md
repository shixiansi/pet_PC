# tasks: 番茄钟状态机下沉

## B4 —— 已完成（逻辑搬迁）

- [x] 通读 `pomodoro.go`，确认它不引用 App / 窗口 / Wails（只有注释提到 app）
- [x] 量化搬迁半径：8 个文件、约 110 处引用，其中 5 个测试文件
- [x] 确认只有 `.finished()` 需要升为导出（7 处调用）
- [x] 建 `internal/pomodoro/machine.go`（包名 `pomodoro`，含包文档）
- [x] `pomodoro.go`（main）换成接缝：类型别名 + 15 个一行转发（395 → 96 行）
- [x] `app_pomodoro.go` 两处、`pomodoro_test.go` 五处 `.finished()` → `.Finished()`
- [x] 导出后 14 处文档注释的首词改成导出名

## B4b —— 已完成（测试分家）

- [x] 给新包补一条**从包外**验证导出面的用例（`external_test.go`，`package pomodoro_test`）：
      `StartFocus` → `FinishPhase` 产生 record、`Finished()` 语义、消费后不再伪造记录、
      `PhaseLabel` / `FormatCountdown` / `Remaining` 可用。它同时钉住"包内改名一致但
      外部接缝断了"这类改动。
- [x] `pomodoro_carry_test.go`（4 个用例，0 处 App 引用）搬进 `internal/pomodoro/carry_test.go`
- [x] `pomodoro_test.go` 拆分：20 个纯状态迁移用例 + `TestPhaseLabel` 搬进
      `internal/pomodoro/machine_test.go`；只剩 `TestPomodoroViewCountdownMatchesSeconds`
      留在 main（它要 `App.pomodoroView()`）
- [x] 两个 fixture（`testConfig` / `enabledIdle`）在新包各留一份**副本**：它们仍被 4 个留在
      main 的测试文件使用（`app_hotkey_reveal_test.go`、`pomodoro_gate_test.go`、
      `pomodoro_plan_gate_test.go`），而 Go 没有跨包共享测试夹具的办法
- [x] main 的 `pomodoro_test.go`：582 → 67 行

### 实际验证

| 命令 | 结果 |
| --- | --- |
| `gofmt -l internal/pomodoro pomodoro_test.go pomodoro.go app_pomodoro.go` | 无输出 |
| `go vet ./...` | 退出码 0 |
| `go test ./internal/pomodoro -v` | **25 个用例全 PASS**（20 状态迁移 + 4 carry + 1 包外） |
| `go test ./...` | `PetPC` / `internal/llm` / `internal/plugins` / `internal/pomodoro` / `internal/store` 全 ok |

`internal/desktop` 仍是那 9 个 `open Run key: Access is denied`（沙箱限制，与本改动无关）。

### 留在 main 的番茄钟测试（这是对的，不是遗漏）

`pomodoro_gate_test.go`（5）、`pomodoro_plan_gate_test.go`（6）、
`pomodoro_plan_paths_test.go`（2）、`pomodoro_test.go`（1）——它们断言的是**宿主门控**：
记录未写就不开始休息、全屏时不问计划、重启后停泊的阶段不自己跑。这些需要 App、
store 和窗口，本来就该在 main。

## B4c —— 未开始

- [ ] app_pomodoro.go 与剩余测试的调用点改成 `pomodoro.X`
- [ ] 删除接缝文件 `pomodoro.go`（main）
- [ ] 确认 `package main` 里不再有状态迁移逻辑，只剩视图、循环、气泡与 Wails 绑定
- [ ] 那时两个 fixture 的副本可以随接缝一起评估是否还需要

## 明确不做

- 不改 `app_pomodoro.go` 的结构（它主体是 UI，见 proposal 的非目标）
- 不引入 `Machine` 接口（没有第二种实现，见 design）
- 不动番茄钟的任何行为
