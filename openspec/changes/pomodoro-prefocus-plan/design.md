# design.md — pomodoro-prefocus-plan

## 1. 状态机与数据流

### 1.1 阶段流转（新增「计划门控」用 `⏸PLAN` 标注）

```
                        手动「开始专注」
                              │
                              ▼
                    ┌──────────────────┐
                    │ 选待办气泡 Pick   │  标题「想专注做哪件事？」
                    │（保留，可不选）   │  AllowUnattached 仅列表为空
                    └────────┬─────────┘
                             │ 确认 → StartPomodoro（不再直接计时）
                             ▼
   [专注 25:00 running] ──到点──▶ [写总结 Summary] ──应答──▶ [休息 running]
            ▲                          （休息由记录门控，§4.5.1 不变）
            │                                                        │
            │                                                        │ 休息自然到点
            │                                                        ▼
            │                                              ⏸PLAN「暂停 + PlanPending」
            │                                                        │
            └──────── StartPomodoroWithPlan(plan) ◀──────── [计划气泡 Plan] ◀── 应答
                         起算当刻 = now                         （跳过 / 开始专注 同路径）
```

- **专注 → 写总结**：`finishPhase` 专注分支**一字不变**：`RunStopped` + `RemainingOnHalt = 休息整长`，`out.Record` 非空 → app 层写记录并置 `PendingRecordID`。总结气泡是休息的门控（§4.5.1 保持）。
- **休息 → 专注**：`finishPhase` 非专注分支**语义一字不变**（`AutoStartNextPhase` 为真则 `RunRunning + EndsAt = now + 下阶段时长`）。app 层 `completePhaseLocked` 在其结果上追加「暂停 + `PlanPending` + 弹 `BubblePlan`」；**专注由计划气泡的应答起算**。
- **`AutoStartNextPhase = false` 时**：`finishPhase` 已给出 `RunStopped + RemainingOnHalt = 专注整长`；app 层只补 `PlanPending = true` 并弹气泡（**不依赖该开关**，因为两条进入专注的路径都必须问一次）。

### 1.2 数据字段

| 结构 | 字段 | 类型 / tag | 语义 |
|---|---|---|---|
| `PomodoroState` | `PlanPending` | `bool` `json:"planPending,omitempty"` | 正等用户填写本次专注要做什么（**门控标记**） |
| `PomodoroState` | `NextTodoID` | `string` `json:"nextTodoId,omitempty"` | 结转给下一个专注的待办 id |
| `PomodoroState` | `NextTodoTitle` | `string` `json:"nextTodoTitle,omitempty"` | 结转的待办标题快照 |
| `PomodoroRecord` | `Plan` | `string` `json:"plan,omitempty"` | 预先要做的事；旧记录缺字段按零值读（零迁移） |
| `PomodoroRecord` | `PlanEditedAt` | `*time.Time` `json:"planEditedAt,omitempty"` | 计划被事后修改的时间 |
| `BubbleView` | `Plan` | `string` `json:"plan,omitempty"` | 计划气泡=输入框初值；总结气泡=「计划：」提示行内容 |
| `App`（main 包） | `focusPlan` | `string`（无 json） | 本次专注的计划文本，**由 `pomoMu` 保护**，仅存活于专注开始 → 记录落盘之间 |
| `App`（main 包） | `foregroundFullscreen` | `func(desktop.Window) bool`（无 json） | 前台全屏判定入口，生产为 `desktop.IsForegroundFullscreen`；**仅测试可替换**，使 §4.7 全屏分支可被两态独立驱动（见 §1.5） |

`NormaliseState` 追加：`Phase == PhaseIdle` 时一并清 `PlanPending`（与既有 idle 归零块同处，避免「空闲却等计划」的僵尸门控）。

### 1.3 `NextTodo*` 结转时点（冲突 C1 的唯一收敛）

规格卡方案四-1 写「休息结束时从 `CurrentTodoID/CurrentTodoTitle` 复制」，但仓库事实是 **`finishPhase` 在专注结束时已清空 `CurrentTodo*`**（`pomodoro.go:218-219`），且 `pomodoro_test.go:190-193` 断言休息阶段不得继承关联。因此**结转点前移到专注结束**：

```
专注结束 finishPhase（专注分支）
  out.Record.TodoID/TodoTitle = 本次会话的待办
  st.NextTodoID/NextTodoTitle = 同上（新）      ← 结转在此发生
  st.CurrentTodoID/CurrentTodoTitle = ""        ← 既有行为不变
        │
        ▼（休息期间 NextTodo* 保持不动，不参与任何渲染）
休息到点 / 手动 picker
  StartPomodoro(ConfirmPomodoroStart) 只写 NextTodo*
        │
        ▼
计划气泡应答 → StartPomodoroWithPlan(plan)
  CurrentTodoID = NextTodoID; CurrentTodoTitle = NextTodoTitle（同一无条件搬运）
  NextTodoID/NextTodoTitle = ""; PlanPending = false
  RunRunning; EndsAt = now + cfg.FocusMinutes; RemainingOnHalt = 0; PhaseStartedAt = now
  focusPlan = plan
```

两条路径共用**同一个搬运语句**，因此不存在「picker 选的 id 与结转值谁为准」的分支：picker 路径的 `StartPomodoro` 已经改写 `NextTodo*`，`StartPomodoroWithPlan` 只负责搬运。

### 1.4 计划文本生命周期

| 时刻 | 行为 |
|---|---|
| `showPlanBubble` | 输入框初值 = **最近一条记录的 `Plan`**（Q3 落地口径），经 `BubbleView.Plan` 下传；取不到则空 |
| `StartPomodoroWithPlan(plan)` | `focusPlan = plan`（空字符串合法） |
| 专注结束 `completePhaseLocked` | **写盘前**：`out.Record.Plan = a.focusPlan; a.focusPlan = ""` |
| 应用中途重启 | `focusPlan` 是内存态，随进程消失 → 该次记录 `Plan` 为空（不报错、不补写），与 Q3「没有上次就留空」一致 |
| 显式放弃/关闭（`skipPhase`、`SetPomodoroEnabled(false)`、`ResetPomodoro`） | 三条路径都**清空 `a.focusPlan`**（与 `PlanPending`/`NextTodo*` 同批清理，见 §4 D7 与 §5）。`skipPhase` 是不接触 `App` 的纯状态函数，其 `focusPlan` 清理由 `App.SkipPhase` 在 `pomoMu` 下完成；`SetPomodoroEnabled`/`ResetPomodoro` 原位清理。未清则遗留计划会被写进下一条永久记录 |
| 总结气泡 | 从**记录**取 `Plan`（与既有 `TodoTitle` 同一个遍历），故重启后仍能显示 |
| 面板就地编辑 | `SetPomodoroPlanAndSummary(id, plan, summary)` 一次性写回两字段，各自更新时间戳；plan 允许清空 |

### 1.5 幂等兜底（不重复提问）

`completePhaseLocked` 只在**过渡刚发生**时弹气泡，判定收敛为一个**纯函数**（可无窗口单测，沿用 `decideReminder(cfg, st, now, ..., fullscreen)` 的可测性先例）：

```go
// shouldAskPlan: 休息刚结束、且不是补算、且不在全屏静默中，才需要提问一次。
func shouldAskPlan(out phaseOutcome, restored, fullscreen bool) bool {
    if restored || fullscreen { return false }
    return out.finished() && store.IsBreakPhase(out.EndedPhase) && out.NextPhase == store.PhaseFocus
}
```

| 路径 | `restored` | 结论 |
|---|---|---|
| `tickPomodoro` 休息到点 | `false` | **弹**（唯一会弹的路径） |
| `initPomodoro`/`SetPomodoroEnabled(true)` 补算 | `true` | 不弹，按既有语义直接开始专注 |
| 全屏静默（§4.7） | — | 不弹、不置 `PlanPending`、不播声音，计时照常 |
| 同一状态被重复处理 | — | `PlanPending` 已为真 ⇒ `showPlanBubble` 前置条件命中同一气泡；`out.EndedPhase` 在重放时会因 `Phase` 已切换而不再产生 break→focus 过渡 |

**全屏取值注入点（T4/T5 的可执行性前提）**：`fullscreen` 在单测中无真实前台窗口可造，故 `App` 增加一个仅用于取值的函数字段：

```go
// foregroundFullscreen 是 App 上唯一读取前台全屏状态的入口。
// 生产由 initApp 设为 desktop.IsForegroundFullscreen；测试可替换为恒 true/false 的替身。
foregroundFullscreen func(desktop.Window) bool
```

`completePhaseLocked` 只经该字段取值（字段为 `nil` 时回退 `desktop.IsForegroundFullscreen`），与既有 `decideReminder(..., fullscreen bool)` 把全屏作为**入参**注入、以及 `jitter func() time.Duration` 作为可替换函数的仓库先例同构。因此「全屏中休息到点」在无窗口单测中是**可独立驱动**的两态（替身 true / 替身 false），不需要执行阶段自行发明接缝。

## 2. 计划气泡（`BubblePlan`）

| 项 | 取值 |
|---|---|
| const | `BubblePlan = "plan"`（加入 `clearAllBubblesLocked` 的清理列表） |
| 尺寸 | `bubbleHeightPlan = 180`（固定；`bubbleHeightPick=200`/`Summary=160`/`Reminder=140` **不动**） |
| 标题 | 「接下来想专注做什么？」 |
| 待办行 | 有 `NextTodoTitle` 才显示「待办：\<title\>」（复用 `BubbleView.TodoTitle`） |
| 输入框 | placeholder「这件事是…（可留空，Enter 开始）」 |
| 按钮 | 次「跳过」、主「开始专注」；**两者同一提交路径**，仅 plan 为空字符串 |
| 键盘 | `Enter` 提交、`Shift+Enter` 换行、`isComposing` 为真不提交（与总结气泡同构） |
| 留空 | 合法，等同「跳过」，记录照旧产生 |
| 可关闭 | **不可**：`DismissBubble` 直接返回当前 `Layout`；前端 Escape/拖拽/聚焦同构守卫 |
| 槽位 | 计入 `unansweredQuestionKind` ⇒ `OpenLive`、`speakProactive`、提醒气泡一律让位（§8.2 既有表格语义） |

### 2.1 槽位与自愈（冲突 C2 的唯一收敛）

计划气泡占用与总结气泡同一块矩形，而**面板里没有应答计划气泡的入口**（记录页只能改已存在的记录）。因此「面板打开 → 气泡被折叠」必须自愈，否则计时器永久停住且无解释：

| 事件 | 既有行为 | 本变更 |
|---|---|---|
| `SetPanelOpen(false)` | 仅重新裁剪 | `PlanPending` 仍为真且当前无气泡 ⇒ **重弹计划气泡**（等价于「问题回来了」），随 `State` 一起重渲染 |
| `initPomodoro` | `PendingRecordID != ""` 时重弹总结气泡 | `PlanPending` 为真时**重弹计划气泡**（同构自愈） |
| `HidePet` | 总结待答时以 `errSummaryBlocksHiding` 拒绝 | 计划待答时以**新** `errPlanBlocksHiding` 拒绝（文案「番茄钟还在等你写计划，先答完它再隐藏桌宠」） |
| `SetPanelOpen(true)` | 折叠气泡 | 不变（由关闭时自愈；不新增面板内应答入口） |

### 2.2 锁序（新增代码必须遵守）

既有链：`tickPomodoro` 持 `pomoMu` → `completePhaseLocked` → `showBubble` 取 `a.mu`，即 **`pomoMu` → `a.mu`**。`DismissBubble` 反序会死锁，故既有代码先把 `pendingSummaryWaiting()`（自行取 `pomoMu`）读在 `a.mu` 之前。

| 新增函数 | 取锁 | 约束 |
|---|---|---|
| `pendingPlanWaiting()` | 自取 `pomoMu` | 与 `pendingSummaryWaiting` 同构；**不得**在持有 `a.mu` 时调用 |
| `showPlanBubble()` | 由调用方持 `pomoMu`（或启动期单线程），内部 `showBubble` 取 `a.mu` | 不得在持 `a.mu` 时调用 |
| `SetPanelOpen` 的自愈钩子 | 在 `a.mu.Unlock()` **之后**调用 | 否则 `showBubble` 重入 `a.mu` 死锁 |
| `completePhaseLocked` 内读 `PlanPending` | 直接读 `out.State` | 已持 `pomoMu`，不得调用 `pendingPlanWaiting()` |
| `completePhaseLocked` 内取全屏 | `a.foregroundFullscreen`（`nil` 回退默认实现） | 纯函数字段调用，不取任何锁 |

## 3. 前端

### 3.1 气泡渲染（`renderBubble`）

新增 `b.kind === 'plan'` 分支，复用既有元素：`els.bubbleTitle` / `els.bubbleTodo`（待办行）/ `els.bubbleInput` / `els.bubbleSecondary` / `els.bubblePrimary`；新增 `els.bubblePlan`（`#bubble-plan`）供**总结**分支显示「计划：\<plan\>」，plan 为空则整行隐藏。`index.html` 的 `placeholder` 属性**保持原样**（既有接线断言依赖「（Enter 保存）」字样），plan 分支在运行时改写 `placeholder`。每类渲染入口照旧先 `els.bubbleInput.onkeydown = null`，防止 Enter 处理器跨气泡泄漏。

### 3.2 记录页（`renderRecordItem`）

**收敛后的唯一布局**（见 4.1 歧义 A1 的裁定）：

```
[head]  18:10 · 写周报                25分      ← 逐字保持现状，不改
[body]  计划：<plan>                             ← plan 非空
        实际：<summary 或（未填写）>    [改]
   —— 或（plan 为空，含全部历史记录）——
[body]  计划：<rec.todoTitle 或 未关联待办>      ← 第一行退回待办名
        实际：<summary 或（未填写）>    [改]
```

- 编辑态：上方文本框（计划，初值 `rec.plan`，可清空）+ 下方文本框（实际，初值 `rec.summary`）+ 保存/取消；保存调 `App.EditPomodoroRecord(rec.id, planValue, summaryValue)`，成功即退出编辑态并 `applyState(next)`。
- **不变项**：按天折叠、只展开最新一天（`openRecordDays`/`seenRecordDays` 语义）、分组标题「N 条 · M 分」、底部「共 N 条 · 累计专注 X 小时」、倒序、无删除入口、head 行内容；上述各项由 **T33a–T33d** 逐条断言（只做防回归，不新增行为）。

### 3.3 样式

在 `.bubble-*` 与 `.record-*` 既有小节内扩展 `.bubble-plan`、`.record-plan`、`.record-inputs`；**本任务新增的三条规则**内禁止 `box-shadow`/`filter`/`transform`（§3 技术约束、§8.3 视觉）。文件内既有的 `box-shadow`/`transform` 用法（如确认卡片、会话浮层、待办项动画）不在本变更范围内，不得改动。

## 4. 计划内收敛的设计决定（不留未定分支）

| # | 决定 | 被拒方案（仅记录，不实现） |
|---|---|---|
| D1 | `NextTodo*` 在**专注结束时**结转（1.3） | 在休息结束时从 `CurrentTodo*` 复制 —— 该时点已被 `finishPhase` 清空，无值可复制 |
| D2 | 计划文本只入 `PomodoroRecord.Plan` + 内存 `focusPlan`，**不**在 `PomodoroState` 里独立持久化 | 在 `PomodoroState` 加计划字段并持久化 —— 与澄清 doubts 第 1 条明确裁定冲突 |
| D3 | 门控判定抽成纯函数 `shouldAskPlan(out, restored, fullscreen)` | 在 `completePhaseLocked` 内联 `IsForegroundFullscreen` —— 无窗口的单测无法覆盖全屏分支 |
| D4 | 「面板关闭重弹」作为计划气泡自愈（C2） | 面板内新增应答入口 —— 超纲（规格卡未要求改面板）且改动面更大 |
| D5 | `StartPomodoro` 在 `PlanPending` 为真时**不走 resume 旁路**（见 4.2） | 保留 resume 旁路 —— 会直接起算，绕过门控 |
| D6 | 休息期间 `SkipPhase` 保持「立即起算下一个专注」（**不弹计划气泡**）；其门控/内存清理范围见 D7 | 让 skip 也弹计划气泡 —— 超出规格卡「两条路径」的范围，且 skip 是显式放弃手势 |
| D7 | 关闭番茄钟 / `ResetPomodoro` / 休息期间 `SkipPhase` 三条路径都清空 `PlanPending`/`NextTodo*`/**`focusPlan`** 与气泡 | 只清 `PlanPending`/`NextTodo*` 而保留 `focusPlan` —— 上一次遗留的计划会被写进下一条永久记录（与 §1.4 生命周期表、§5 边界表两行同时矛盾） |
| D8 | 计划气泡的初值取「最近一条记录的 plan」（C3） | 从 `PomodoroState` 取 —— 与 D2 冲突 |

### 4.1 歧义 A1：记录页「第一行」的裁定

规格卡/清单同时要求「有计划时第一行「计划：」、第二行「实际：」」与「老记录看起来与今天一致」，而仓库现状是 head 行 `时间 · 待办名` + body 行（总结）。规格卡对 head 行**未作规定**，只说 body 变成两行。

**裁定（唯一路径）**：**head 行保持今天的样子不变**（`${formatClock(rec.endedAt)} · ${rec.todoTitle || '未关联待办'}` + `${rec.focusMinutes}分`），**body 固定两行**：

```
[head]  18:10 · 写周报                 25分      ← 与今天逐字一致，不改
[body]  计划：<plan 非空 ? plan : (rec.todoTitle || '未关联待办')>
        实际：<summary 或（未填写）>      [改]
```

理由：这是对规格卡文字改动面最小的读法——head 不动、`focusMinutes` 位置不动，只有 body 由一行变两行，完全落在规格卡七的授权范围内；同时任何记录都不会丢失已有信息。

**已知取舍（须在 PT 明确确认，见 `pt_decision.reason` 核对项 2）**：`rec.plan` 为空的**老记录**会在 head（`· 写周报`）与 body 第一行（`计划：写周报`）各出现一次待办名。这是规格卡「无计划时第一行显示「计划：\<rec.todoTitle\>」」与「head 行保持不变」两条同时成立的必然结果，本计划按字面执行、不做去重。若 PT 判定老记录必须只出现一次待办名，属**返工定点修改（只改 T18 的 head/body 分配）**，不影响其余任何任务。

### 4.2 冲突 C1 的连带后果：`StartPomodoro` 的 resume 旁路（**必须堵**）

`StartPomodoro` 既有旁路（`app_pomodoro.go:475-484`）条件为 `Phase == focus && !IsRunning && CurrentTodoID == in.TodoID && Remaining > 0`。休息到点后状态恰为 `Phase=focus, RunStopped, CurrentTodoID="", RemainingOnHalt=专注整长`：

- 用户在选待办气泡里选「不关联待办」确认（`in.TodoID == "" == CurrentTodoID`）⇒ **旁路命中**，`resume` 立即起算 ⇒ **计划气泡被绕过**。

**收敛**：`StartPomodoro` 在 `PlanPending` 为真时**跳过 resume 旁路**，改写 `NextTodo*`（若有选择）并弹/刷新计划气泡。resume 旁路只在**非** `PlanPending`（即用户主动 stop/暂停后的「继续」）时保留原语义（§13.1 不变）。

## 5. 边界与错误处理（每条都有任务映射）

| 场景 | 期望 | 任务 |
|---|---|---|
| 全屏中休息到点 | 不弹、不播声音、不置 `PlanPending`，专注照常起算；退出全屏后不补问 | T5 |
| 重启后补算（休息早已到点） | 不弹，直接开始专注 | T6 |
| 重启时 `PlanPending` 为真（应用在等计划时被关掉） | `initPomodoro` 重弹计划气泡；输入初值取最近记录 plan | T24 |
| 计划气泡在时隐藏桌宠 | 拒绝（`errPlanBlocksHiding`），门控与气泡原样保留 | T25 |
| 计划气泡在时打开再关闭面板 | 关闭时重弹 | T23 |
| 计划气泡在时浮层/主动说话/提醒 | 一律让位（`unansweredQuestionKind` 含 `BubblePlan`） | T10 |
| 计划气泡在时按 Esc / 拖桌宠 / 点别处 | 不消失 | T9、T11 |
| 休息期间 `SkipPhase` | 立即起算专注（既有语义），清 `PlanPending`/`NextTodo*`/`focusPlan`，不弹 | T26 |
| 计划待答时关闭番茄钟 / `ResetPomodoro` | 清 `PlanPending`/`NextTodo*`/`focusPlan` + 清气泡，不留僵尸门控、不留遗留计划 | T26 |
| 专注进行中应用重启 | `focusPlan` 丢失 ⇒ 记录 `Plan` 为空、不报错、记录仍产生 | T27 |
| 留空提交 / 点「跳过」 | 同路径，`Plan` 为空字符串，记录照旧产生 | T8、T15 |
| 旧 `pomodoro-records.json`（无 plan/planEditedAt） | 正常读取，`Plan == ""`，条数不变，`SetPomodoroPlanAndSummary` 可写 | T2、T17 |
| `PlanPending` 为真但 `Phase == idle`（坏状态文件） | `NormaliseState` 清标记，不弹气泡 | T1 |
| 记录页编辑时计划被清空 | 允许；`PlanEditedAt` 仍更新，记录不消失 | T17、T19 |
| store 删除入口 | 仍然不存在（§6.1） | T2、T17 |
| 气泡尺寸 | `BubblePlan` 恒为 180，既有 200/160/140 不变；卡片精确填满矩形 | T7a、T20、T21 |

## 6. 任务依赖顺序

> 返工轮定点修复：原 T7 拆为 **T7a/T7b/T7c**（其余编号一律不变，见 tasks.md §C 说明）；记录页不变项新增防回归任务 **T33a–T33d**。

```
T1,T2（数据字段）
  └▶ T3（状态机 NextTodo* 结转）
       └▶ T7a（BubblePlan 常量/尺寸映射；另需 T2）
            └▶ T7b（清理列表 + BubbleView.Plan）
                 └▶ T7c（showPlanBubble 门控与装配）
                      ├▶ T4（休息到点门控 + shouldAskPlan + foregroundFullscreen 注入点）─┬▶ T5（全屏，替身 true 驱动）
                      │                                                                      └▶ T6（补算）
                      ├▶ T21（HTML 元素 #bubble-plan）─┬▶ T8（前端渲染/提交）
                      │                                ├▶ T16（总结气泡显示计划）
                      │                                └▶ T22（绑定/类型同步，另需 T12、T17）
                      │                                     └▶ T29（前端 harness 同步）
                      ├▶ T9（Go 不可关闭）─▶ T11（前端守卫）
                      ├▶ T10（占提问名额）
                      └▶ T12（StartPomodoroWithPlan）
                           ├▶ T13（picker 路径）─▶ T14（堵 resume 旁路）
                           ├▶ T15（plan 落记录）─▶ T16
                           ├▶ T23（面板关闭重弹）
                           ├▶ T24（重启重弹）
                           ├▶ T25（拒绝隐藏）
                           └▶ T26（skip/关闭/Reset 清 PlanPending/NextTodo*/focusPlan）
T17（记录编辑绑定）─▶ T18（双行渲染）─▶ T19（就地编辑）─▶ T20（样式）
T18,T19 ─▶ T33a（按天分组容器）─┬▶ T33b（折叠状态/新日期自动展开）
                                ├▶ T33c（分组标题 N 条 · M 分）
                                └▶ T33d（底部汇总文案）
T15 ─▶ T27（focusPlan 丢失容忍）
T28（既有 Go 单测预期同步）——依赖 T4,T13,T14,T26
T29（前端 harness 预期同步）——依赖 T8,T11,T22,T23,T25
T30,T31（文档/README）——依赖 T28
T32（构建与全量验证）——依赖 T28,T29,T30,T31,T33a–T33d
```
