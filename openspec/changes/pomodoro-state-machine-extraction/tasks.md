# tasks: 番茄钟状态机下沉

## 本批（B4）—— 已完成

- [x] 通读 `pomodoro.go`，确认它不引用 App / 窗口 / Wails（只有注释提到 app）
- [x] 量化搬迁半径：8 个文件、约 110 处引用，其中 5 个测试文件
- [x] 确认只有 `.finished()` 需要升为导出（7 处调用：app_pomodoro.go 2、pomodoro_test.go 5）
- [x] 写 proposal / design
- [x] 建 `internal/pomodoro/machine.go`（包名 `pomodoro`，含包文档）
- [x] `pomodoro.go`（main）换成接缝：类型别名 + 15 个一行转发
- [x] `app_pomodoro.go` 两处 `.finished()` → `.Finished()`
- [x] `pomodoro_test.go` 五处 `.finished()` → `.Finished()`
- [x] 导出后把 14 处文档注释的首词改成导出名（`// startFocus …` → `// StartFocus …`）

### 实际验证

| 命令 | 结果 |
| --- | --- |
| `gofmt -l internal/pomodoro/machine.go pomodoro.go app_pomodoro.go pomodoro_test.go` | 无输出 |
| `go build ./internal/pomodoro` | 退出码 0 |
| `go vet ./...` | 退出码 0 |
| `go test ./...` | `PetPC` / `internal/llm` / `internal/plugins` / `internal/store` 全 ok |
| `go test . -run 'TestPomodoro\|TestFocus\|TestSkip\|TestReconcile\|TestCarry\|TestPhase\|TestPlan\|TestPause\|TestResume\|TestStop\|TestReset\|TestStart'` | ok（88 行测试输出，全部通过） |

`internal/desktop` 仍有 9 个失败，全部是 `open Run key: Access is denied`——本机沙箱
不允许访问开机自启注册表项，与本改动无关（改动前就是这样）。

`internal/pomodoro` 目前**没有自己的测试文件**：现有测试在 `package main` 里通过接缝
跑同一份代码，所以覆盖率没变，但 `go test ./internal/pomodoro` 现在什么也不跑。

## 下一批（B4b，未开始）

- [ ] 给新包补一条最小自检用例：`StartFocus` → `FinishPhase` 产生一条 record，
      且 `PhaseOutcome.Finished()` 语义不变（**先钉住行为，再搬测试**）
- [ ] 把 `pomodoro_carry_test.go`（0 处 App 引用）搬进 `internal/pomodoro`，
      连同它依赖的 `enabledIdle` / `testConfig` helper
- [ ] 拆 `pomodoro_test.go`：用 `App.pomodoroView()` 的那个用例留在 main，其余搬走

## 更后（B4c，未开始）

- [ ] app_pomodoro.go 与剩余测试的调用点改成 `pomodoro.X`
- [ ] 删除接缝文件 `pomodoro.go`（main）
- [ ] 确认 `package main` 里不再有状态迁移逻辑，只剩视图、循环、气泡与 Wails 绑定

## 明确不做

- 不改 `app_pomodoro.go` 的结构（它主体是 UI，见 proposal 的非目标）
- 不引入 `Machine` 接口（没有第二种实现，见 design）
- 不动番茄钟的任何行为
