# design: 接缝式抽取（别名 + 转发）

## 决定：先建接缝，再迁移调用点

抽取有两条路：

**A. 全量迁移**：把 `startFocus` 改成 `pomodoro.StartFocus` 等约 110 处调用点，
删掉 main 里的旧名字。
代价：8 个文件、110 处编辑，其中 5 个是测试文件。任何一处漏改都是编译错误
（这个还算好），但把名字改进了注释或字符串就是静默失真。

**B. 接缝式（本批采用）**：`internal/pomodoro` 用导出的正确名字，`package main`
保留一个约 70 行的接缝文件（`type phaseOutcome = pomodoro.PhaseOutcome` 加一串
一行转发）。**调用点零改动**。
代价：多一层间接，且这层间接会被后来者误当成"就是这样的设计"。

采用 B，理由是这一步的价值在**依赖面被固化**（import 图保证状态机只依赖
internal/store），而不在少敲几个字符的调用点。B 能把这个收益拿到手，同时把
110 处编辑的风险推迟到「已经有编译器和测试覆盖的后续批次」。

B 的代价用文档压住：接缝文件顶部明确写它是临时层，tasks.md 里列出删除它的
具体步骤。

## 导出面

| main 里的旧名 | 新包里的名字 | 说明 |
| --- | --- | --- |
| `phaseOutcome`（类型） | `PhaseOutcome` | main 里用别名 `type phaseOutcome = pomodoro.PhaseOutcome`，复合字面量与字段访问全部照旧 |
| `(o phaseOutcome) finished()` | `(o PhaseOutcome) Finished()` | **必须导出**：类型别名不会把未导出方法带进 main，而 app_pomodoro.go 有 2 处、pomodoro_test.go 有 5 处调用 |
| `Remaining` | `Remaining` | 本来就已导出 |
| `startFocus` / `pause` / `resume` / `stop` / `reset` | `StartFocus` / `Pause` / `Resume` / `Stop` / `Reset` | |
| `finishPhase` / `skipPhase` | `FinishPhase` / `SkipPhase` | |
| `shouldAskPlan` / `reconcile` | `ShouldAskPlan` / `Reconcile` | |
| `hasPendingSummary` / `phaseLabel` | `HasPendingSummary` / `PhaseLabel` | |
| `ceilSeconds` / `formatSeconds` / `formatCountdown` | `CeilSeconds` / `FormatSeconds` / `FormatCountdown` | |
| `nextPhaseAfter` / `clockJumpSlack` | 保持未导出 | 外部 0 引用，不该扩大导出面 |

## 文件与命名

- 新文件 `internal/pomodoro/machine.go`（包名已叫 pomodoro，再叫
  `pomodoro/pomodoro.go` 是结巴；这个文件就是"状态机"）。
- `pomodoro.go`（main）保留原名与位置，内容换成接缝——这样 `git log` 能顺着文件名
  看到历史，而不是出现一个"删掉又新增"的假象。

## 为什么不用接口（interface）

抽一个 `Machine` 接口再实现它，会让状态机变成可替换的实现——但这里没有第二种
实现，也没有测试替身的需求（纯函数已经足够好测）。接口在这里只增加间接层，
违反本仓库的 YAGNI 约定（AGENTS.md §8）。

## 迁移顺序（后续批次）

1. 把 `pomodoro_test.go` 里那 1 个用 `App.pomodoroView()` 的用例拆出来留在 main，
   其余（连同 `enabledIdle` / `testConfig` 两个 helper）搬进 `internal/pomodoro`。
2. `pomodoro_carry_test.go`（0 处 App 引用）直接搬。
3. 把 app_pomodoro.go 与剩余测试里的调用点改成 `pomodoro.X`，删掉接缝文件。
   第 3 步做完后，`package main` 里不应再有任何 `store.PhaseFocus` 的状态迁移逻辑。
