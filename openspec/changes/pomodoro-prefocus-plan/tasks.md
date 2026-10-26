# tasks.md — pomodoro-prefocus-plan

> 任务编号与 design.md §6 依赖图、§5 边界表一一对应。每条任务的「完成判据」都是**实施完成后的单一终态**；执行层阶段（先失败后通过等）属 D 阶段执行协议，此处不写。
> 依赖顺序见 design.md §6；`[前置]` 列出必须已完成的编号。
> 返工轮（2026-09-15）定点修复：原 **T7** 拆为 **T7a/T7b/T7c**，记录页不变项补为 **T33a–T33d**；其余任务编号一律沿用、不重排。

## A. 数据与状态机（T1–T3）

- [ ] **T1 — `PomodoroState` 新增门控与结转字段**
  文件：`internal/store/pomodoro.go`
  内容：`PlanPending bool \`json:"planPending,omitempty"\``、`NextTodoID string \`json:"nextTodoId,omitempty"\``、`NextTodoTitle string \`json:"nextTodoTitle,omitempty"\`` 三字段及其语义注释；`NormaliseState` 在 `Phase == PhaseIdle` 的既有归零块内一并清空 `PlanPending/NextTodoID/NextTodoTitle`。
  完成判据：`store.PomodoroState{}` 经 `json.Marshal`/`Unmarshal` 往返后三字段保持零值且输出 JSON 中不出现三个 key（`omitempty` 生效）；`NormaliseState(PomodoroState{Phase: PhaseIdle, PlanPending: true, NextTodoID: "x", NextTodoTitle: "y"})` 返回的 `PlanPending == false` 且 `NextTodoID == ""` 且 `NextTodoTitle == ""`；既有 `TestNormaliseStateRepairsGarbage` 断言仍全部成立。

- [ ] **T2 — `PomodoroRecord` 新增计划字段 + 记录编辑方法**
  文件：`internal/store/pomodoro.go`
  内容：`Plan string \`json:"plan,omitempty"\``、`PlanEditedAt *time.Time \`json:"planEditedAt,omitempty"\``；新增 `SetPomodoroPlanAndSummary(id, plan, summary string) (PomodoroRecord, error)`，一次性写回两字段并各自更新 `PlanEditedAt`/`EditedAt`（plan 可与旧值相同或为空，仍更新时间戳）；未找到 id 返回既有文案的错误 `errors.New("未找到该记录")`。
  完成判据：对一个不含 `plan`/`planEditedAt` 键的旧 `pomodoro-records.json` 调用 `store.New` 后，`PomodoroRecords()` 返回条数与文件内条数相同、每条 `Plan == ""` 与 `PlanEditedAt == nil`，且 `Warnings()` 为空；对该文件中一条记录调用 `SetPomodoroPlanAndSummary(id, "", "改后的实际")` 后返回记录的 `Plan == ""`、`PlanEditedAt != nil`、`EditedAt != nil`；`SetPomodoroPlanAndSummary("不存在", "a", "b")` 返回错误；`grep -n "DeletePomodoroRecord\|ClearPomodoroRecords\|DeleteRecord" internal/store/pomodoro.go` 无匹配（store 层仍无任何删除入口）。

- [ ] **T3 — 状态机在专注结束时结转 `NextTodo*`（不改休息分支语义）**
  [前置] T1
  文件：`pomodoro.go`
  内容：`finishPhase` 专注分支内（`out.Record` 组装处附近）把本次会话的 `TodoID/TodoTitle` 同时写入 `st.NextTodoID/st.NextTodoTitle`；`st.CurrentTodoID/CurrentTodoTitle` 照旧清空。
  完成判据：`startFocus(state, cfg, "t1", "写周报", now)` 后调用 `finishPhase(..., endedAt, now, false)`，返回的 `out.State.NextTodoID == "t1"` 且 `out.State.NextTodoTitle == "写周报"`，同时 `out.State.CurrentTodoID == ""` 且 `out.State.CurrentTodoTitle == ""`、`out.Record.TodoID == "t1"`；对休息阶段调用 `finishPhase` 时 `NextTodo*` 不被改写；`TestBreakEndReturnsToFocus` 与 `TestFocusEndProducesRecordAndBreak` 中关于「非专注分支照旧 `RunRunning + EndsAt`」「休息阶段不继承关联」的断言仍全部成立（既有测试文件不改期望值）。

## B. 进入专注的两条路径（T4–T6）

- [ ] **T4 — 休息到点由 app 层补「暂停 + PlanPending」并弹计划气泡**
  [前置] T1、T3、T7c
  文件：`app_pomodoro.go`、`pomodoro.go`（新增纯函数所在文件）
  内容：新增纯函数 `shouldAskPlan(out phaseOutcome, restored, fullscreen bool) bool`（判定见 design.md §1.5）；`App` 新增**全屏取值注入点** `foregroundFullscreen func(desktop.Window) bool` 字段，`initApp`/构造处默认为 `desktop.IsForegroundFullscreen`（`nil` 时回退到该默认实现），使全屏分支在**无窗口单测**中可被替换驱动（与既有 `decideReminder(..., jitter, fullscreen)` 把 `jitter`/`fullscreen` 作为可注入入参的仓库先例同构）；`completePhaseLocked` 在既有 `else if out.EndedPhase != "" && !restored` 分支内，当 `shouldAskPlan(...)` 为真时把 `out.State` 改为 `RunState=RunStopped`、`EndsAt=nil`、`RemainingOnHalt=int(cfg.PhaseDuration(store.PhaseFocus).Seconds())`、`PlanPending=true`，**落盘后**再调用 `showPlanBubble()`。
  完成判据：构造「休息运行到点」场景调用 `a.completePhase(finishPhase(breakState, cfg, ended, now, false), false)` 后（无窗口，`applyWindowGeometry` 失败被忽略；`a.foregroundFullscreen` 置为恒返回 `false` 的替身），`a.store.Pomodoro()` 的 `PlanPending == true` 且 `RunState == store.RunStopped` 且 `EndsAt == nil` 且 `RemainingOnHalt == int(cfg.PhaseDuration(store.PhaseFocus).Seconds())`；`a.bubble.Kind == BubblePlan` 且 `a.layout.Bubble.Kind == BubblePlan`；**幂等**：随后调用 `a.tickPomodoro()`（其推进前提为 `IsRunning() && EndsAt != nil`，门控态下为假）后上述四项状态与 `BubblePlan` 气泡逐项不变；`shouldAskPlan` 的表驱动断言覆盖：`{restored:false, fullscreen:false, EndedPhase:short_break, NextPhase:focus}` → true，其余 `restored=true` / `fullscreen=true` / `EndedPhase=focus` / `NextPhase != focus` → false。

- [ ] **T5 — 全屏静默路径：不弹计划气泡、不置 PlanPending**
  [前置] T4
  文件：`app_pomodoro.go`
  内容：`completePhaseLocked` 的休息结束分支经 `App.foregroundFullscreen` 注入点（默认 `desktop.IsForegroundFullscreen`）取得 `fullscreen` 并传给 `shouldAskPlan`（§4.7）；全屏时不置 `PlanPending`、不弹气泡，专注照常起算。
  完成判据：把 `a.foregroundFullscreen` 替换为恒返回 `true` 的替身（**测试注入点已在 T4 声明**），调用 `a.completePhase(finishPhase(breakState, cfg, ended, now, false), false)` 走真实全屏分支后：`a.store.Pomodoro().PlanPending == false`、`RunState == store.RunRunning`、`EndsAt != nil`、`a.bubble == nil` 且 `a.layout.Bubble == nil`；同一场景下把替身换回恒返回 `false` 再驱动一次，得到 T4 的 `BubblePlan` 门控终态（两态可对照）；随后调用 `SetPanelOpen(false)`（此时 `PlanPending == false`，故 T23 的重弹钩子**不应**触发）与 `ShowPetForUser()` 后 `a.bubble` 仍为 `nil`——即不补一个迟到的计划问题。（全屏真分支依赖真实前台窗口，人工验收第 14 条另作实机覆盖，不构成本判据的前置。）

- [ ] **T6 — 重启补算路径：不弹计划气泡，直接开始专注**
  [前置] T4
  文件：`app_pomodoro.go`
  内容：`initPomodoro` 与 `SetPomodoroEnabled(true)` 走 `completePhase(out, true)`（`restored=true`），`shouldAskPlan` 因此为假，计划留空。
  完成判据：预置一个「在应用关闭期间早已到点」的休息状态后新建 `App` 并调用 `initPomodoro()`：`a.store.Pomodoro().PlanPending == false`、`RunState == store.RunRunning`、`EndsAt != nil`、`a.bubble == nil`，且未新增任何 `PomodoroRecord`；同构地，`SetPomodoroEnabled(false)` → `SetPomodoroEnabled(true)` 之后 `PlanPending == false` 且 `a.bubble == nil`。

## C. 气泡（T7a–T7c、T8–T11、T21–T22）

> 原 T7 混装了三个互不依赖的可观察结果（尺寸映射 / 清理列表+类型字段 / 气泡装配门控），本轮按可独立核对的终态拆成 **T7a/T7b/T7c** 三条；**编号沿用 T7 前缀，其余任务编号一律不动**（返工轮定点修复，不重排）。

- [ ] **T7a — `BubblePlan` 常量与固定尺寸映射**
  [前置] T1、T2
  文件：`app_pomodoro.go`
  内容：const 区新增 `BubblePlan = "plan"`；尺寸常量新增 `bubbleHeightPlan = 180`；`bubbleLogicalHeight` 新增 `case BubblePlan: return bubbleHeightPlan`。既有 200/160/140 与宽度 300 一字不动。
  完成判据：`bubbleLogicalHeight(BubblePlan) == 180`、`bubbleLogicalHeight(BubblePick) == 200`、`bubbleLogicalHeight(BubbleSummary) == 160`、`bubbleLogicalHeight(BubbleReminder) == 140`（四种映射成立、既有三种不变）。

- [ ] **T7b — `BubblePlan` 纳入清理列表 + `BubbleView.Plan` 字段**
  [前置] T7a
  文件：`app_pomodoro.go`
  内容：`clearAllBubblesLocked` 的清理列表加入 `BubblePlan`；`BubbleView` 新增 `Plan string \`json:"plan,omitempty"\``（计划气泡＝输入框初值；总结气泡＝「计划：」提示行内容）。
  完成判据：先 `showBubble(&BubbleView{Kind: BubblePlan, ...})` 再调用 `clearAllBubblesLocked()`，`a.bubble == nil` 且 `a.layout.Bubble == nil`；`json.Marshal(BubbleView{Kind: BubblePlan, Title: "t"})` 的输出中不出现 `plan` 键（`omitempty` 生效），而 `BubbleView{Kind: BubblePlan, Plan: "整理接口"}` 的输出含 `"plan":"整理接口"`。

- [ ] **T7c — `showPlanBubble()` 前置门控与内容装配**
  [前置] T7a、T7b
  文件：`app_pomodoro.go`
  内容：新增 `showPlanBubble()`，**前置条件：`PlanPending` 为真，否则直接返回不起气泡**；标题「接下来想专注做什么？」、`TodoTitle` 取 `NextTodoTitle`、`Plan` 取最近一条记录的 `Plan` 作为输入框初值（无则空）。
  完成判据：在 `PlanPending=false` 时调用 `showPlanBubble()` 后 `a.bubble == nil` 且 `a.layout.Bubble == nil`（不起气泡、不改任何状态）；在 `PlanPending=true`、`NextTodoTitle="写周报"`、存在最近一条记录 `Plan="整理接口"` 的状态下调用 `showPlanBubble()` 后 `a.bubble.Kind == BubblePlan`、`a.bubble.Title == "接下来想专注做什么？"`、`a.bubble.TodoTitle == "写周报"`、`a.bubble.Plan == "整理接口"`（四项取值逐项成立）。

- [ ] **T8 — 前端计划气泡渲染与 Enter 提交**
  [前置] T7c、T21
  文件：`frontend/src/main.js`
  内容：`renderBubble` 新增 `b.kind === 'plan'` 分支：`els.bubbleTodo` 显示「待办：\<todoTitle\>」（无则不显示）、`els.bubbleInput` 显示并以 `b.plan` 为初值、运行时改写 `placeholder = '这件事是…（可留空，Enter 开始）'`、次按钮「跳过」、主按钮「开始专注」（两者都提交）；Enter 提交与总结分支同构（`e.key === 'Enter' && !e.shiftKey && !e.isComposing` → `preventDefault` → 提交），提交调 `App.StartPomodoroWithPlan(els.bubbleInput.value)`；总结分支使用 `els.bubblePlan` 显示计划提示行（见 T16）。
  完成判据：以 `renderBubble({kind:'plan', plan:'整理接口', todoTitle:'写周报'})` 驱动后，`els.bubbleInput.value === '整理接口'`、`els.bubbleInput.placeholder === '这件事是…（可留空，Enter 开始）'`、`els.bubblePrimary.textContent === '开始专注'`、`els.bubbleSecondary.textContent === '跳过'`、`els.bubbleTodo.textContent === '待办：写周报'`；对输入框派发 `{key:'Enter'}` 触发一次 `App.StartPomodoroWithPlan` 且参数为当前 value，派发 `{key:'Enter', isComposing:true}` 与 `{key:'Enter', shiftKey:true}` 均不触发提交；渲染 `{kind:'reminder'}` 后 `els.bubbleInput.onkeydown === null`。

- [ ] **T9 — 计划气泡不可关闭（Go 侧）**
  [前置] T7c
  文件：`app_pomodoro.go`
  内容：新增 `pendingPlanWaiting() bool`（自行取 `pomoMu`，与 `pendingSummaryWaiting` 同构）；`DismissBubble` 的既有守卫扩展为「`b.Kind ∈ {BubbleSummary, BubblePlan}` 且对应 pending 为真 ⇒ 返回当前 `Layout`，不动气泡」；`DismissBubble` 中两个 pending 读取都在取 `a.mu` **之前**完成。
  完成判据：在 `PlanPending=true` 且 `a.bubble.Kind == BubblePlan` 时调用 `DismissBubble()`：`a.store.Pomodoro().PlanPending` 仍为 `true`、`RunState` 仍为 `store.RunStopped`、`a.bubble.Kind == BubblePlan`、`a.layout.Bubble.Kind == BubblePlan`；在 `PlanPending=false`（但气泡恰好残留 `BubblePlan`）时调用 `DismissBubble()` 该气泡被清掉且 `a.layout.Bubble == nil`；既有 `TestSummaryPromptCannotBeDismissed` 与 `TestOtherBubblesAreStillDismissable` 断言仍全部成立。

- [ ] **T10 — 计划气泡计入「待回答提问」名额**
  [前置] T7c
  文件：`app_pomodoro.go`
  内容：`unansweredQuestionKind` 的判定条件扩为 `BubblePick || BubbleSummary || BubblePlan`；`questionOwnsTheSlot` 为 `BubblePlan` 返回专属文案（说明「还在等你写计划」）。
  完成判据：`a.bubble.Kind == BubblePlan` 时 `a.unansweredQuestionKind() == BubblePlan`、`a.bubbleAwaitingAnswer()` 为 `true`、`questionOwnsTheSlot(BubblePlan)` 非空且与 `questionOwnsTheSlot(BubblePick)`、`questionOwnsTheSlot(BubbleSummary)` 三者互不相同；此时调用 `a.OpenLive()` 返回的 `error.Error() == questionOwnsTheSlot(BubblePlan)` 且 `a.liveIsOpen() == false` 且气泡仍为 `BubblePlan`；`a.bubble.Kind == BubbleReminder` 时 `a.OpenLive()` 仍成功且提醒气泡被清掉；既有 `TestUnansweredQuestionKindNamesTheRightBubble` 断言对 `{BubblePick, BubbleSummary, BubbleReminder, BubbleNone}` 仍成立。

- [ ] **T11 — 前端 Escape 与拖拽守卫扩展到计划气泡**
  [前置] T8
  文件：`frontend/src/main.js`、`frontend/src/live.js`（仅当有守卫时）
  内容：`bubbleAwaitingAnswer()` 扩为 `kind === 'summary' || kind === 'pick' || kind === 'plan'`；`focusAnswer()` 对 `plan` 分支把光标送进 `els.bubbleInput`；Escape 处理器与拖拽起点的 `bubble.kind !== 'summary'` 守卫改为「`summary` 或 `plan` 时不调用 `DismissBubble`」。
  完成判据：`bubble = {kind:'plan'}` 时 `bubbleAwaitingAnswer() === true`、`focusAnswer()` 调用后 `document.activeElement === els.bubbleInput`、`openLive()` 直接返回且未调用 `App.OpenLive`；以 `bubble = {kind:'plan'}` 派发 Escape keydown 后 `App.DismissBubble` 未被调用；以 `bubble = {kind:'summary'}` 的同样断言成立；以 `bubble = {kind:'reminder'}` 派发 Escape 后 `App.DismissBubble` 被调用且 `{kind:'reminder'}` 的拖拽越过阈值后 `App.DismissBubble` 被调用。

- [ ] **T21 — 气泡新区块元素 `#bubble-plan`**
  [前置] T7c
  文件：`frontend/index.html`、`frontend/src/main.js`（`els` 表新增 `bubblePlan`）
  内容：气泡区块在 `#bubble-todo` 之后新增 `<p id="bubble-plan" class="bubble-plan hidden"></p>`；`els` 表登记 `bubblePlan`；`renderBubble` 的公共重置段把该元素置为 `hidden`。既有 `#bubble-input` 的 `placeholder` 属性**保持原样**。
  完成判据：`index.html` 中 `id="bubble-plan"` 存在且位于 `id="bubble-todo"` 与 `id="bubble-list"` 之间；`main.js` 的 `els` 表含 `bubblePlan: $('bubble-plan')`；重渲染任意气泡 kind（含 `reminder`/`pick`/`plan`）后未显式赋值的场合 `#bubble-plan` 处于 `hidden`；`index.html` 中 `id="bubble-input"` 的 `placeholder` 仍为「写点什么…（Enter 保存）」。

- [ ] **T22 — Wails 绑定与前端类型同步**
  [前置] T12、T17、T21
  文件：`frontend/wailsjs/go/main/App.js`、`frontend/wailsjs/go/main/App.d.ts`、`frontend/wailsjs/go/models.ts`
  内容：生成/更新 `StartPomodoroWithPlan(arg1:string):Promise<main.State>`、`EditPomodoroRecord(arg1:string,arg2:string,arg3:string):Promise<main.State>`；`models.ts` 的 `store.PomodoroRecord` 增 `plan`/`planEditedAt`，`main.BubbleView` 增 `plan`。
  完成判据：`frontend/wailsjs/go/main/App.js` 含 `StartPomodoroWithPlan` 与 `EditPomodoroRecord` 两个导出且分别转发到对应 `window['go']['main']['App']` 方法；`App.d.ts` 的签名与 Go 侧一致（`StartPomodoroWithPlan(string)`、`EditPomodoroRecord(string,string,string)` 均返回 `Promise<main.State>`）；`models.ts` 中 `store.PomodoroRecord` 含 `plan: string` 与 `planEditedAt?: any`，`main.BubbleView` 含 `plan?: string`；`.dev/check-frontend.cjs` 中「JS 调用的绑定在 Go 侧存在」这一既有检查通过。

## D. 计划的保存与落库（T12–T16）

- [ ] **T12 — `StartPomodoroWithPlan(plan string)` 起算专注**
  [前置] T1、T7c
  文件：`app_pomodoro.go`、`app.go`（`App` 新增 `focusPlan` 字段）
  内容：`app.go` 的 `App` 新增 `focusPlan string` 字段（无 json tag，由 `pomoMu` 保护）；`app_pomodoro.go` 新增绑定方法 `StartPomodoroWithPlan(plan string) (State, error)`：在 `pomoMu` 下取 `PlanPending/NextTodo*` → 把 `NextTodoID/NextTodoTitle` 搬进 `CurrentTodoID/CurrentTodoTitle` 并清空 `NextTodo*` → `PlanPending=false` → `Phase=store.PhaseFocus`、`RunState=RunRunning`、`EndsAt = now + cfg.FocusMinutes`、`RemainingOnHalt=0`、`PhaseStartedAt=now` → `focusPlan=plan`（空字符串合法）；落盘、清计划气泡、`emitPomodoro()` + `emitLayout()`；若 `PlanPending` 为假则不作任何状态改动（幂等）。
  完成判据：`PlanPending=true`、`NextTodoID="t1"`、`NextTodoTitle="写周报"` 时调用 `StartPomodoroWithPlan("整理接口")`，返回后 `a.store.Pomodoro()` 满足：`PlanPending == false`、`NextTodoID == ""`、`NextTodoTitle == ""`、`CurrentTodoID == "t1"`、`CurrentTodoTitle == "写周报"`、`Phase == store.PhaseFocus`、`RunState == store.RunRunning`、`RemainingOnHalt == 0`、`PhaseStartedAt != nil`、`EndsAt` 落在 `[调用前+FocusMinutes, 调用后+FocusMinutes]` 区间内、`a.focusPlan == "整理接口"`、`a.bubble == nil`；以 `StartPomodoroWithPlan("")` 走同一路径后 `a.focusPlan == ""` 且其余终态同上；在 `PlanPending=false` 的状态下调用 `StartPomodoroWithPlan("x")` 后 `a.store.Pomodoro()` 与调用前逐字段相等且 `a.focusPlan` 未变。

- [ ] **T13 — 手动路径：选待办气泡改标题并在确认后弹计划气泡**
  [前置] T12
  文件：`app_pomodoro.go`
  内容：`ShowPomodoroPicker` 标题改为「想专注做哪件事？」；`StartPomodoro` 在非 resume 分支下**不再直接起算**，而是写入 `NextTodo*`（`in.TodoID` 与其标题快照）、`st.Enabled = true`、`PlanPending = true`，落盘并弹 `BubblePlan`；`AllowUnattached` 仍仅在 `len(openTodos) == 0` 时为真；选择待办仍不 `ToggleTodo`。
  完成判据：`ShowPomodoroPicker()` 返回的 `Layout.Bubble.Title == "想专注做哪件事？"` 且 `Kind == BubblePick`；在有待办 `t1` 且 `PlanPending=false` 时调用 `StartPomodoro({TodoID:"t1"})` 后：`a.store.Pomodoro().PlanPending == true`、`RunState != store.RunRunning`、`EndsAt == nil`、`NextTodoID == "t1"`、`NextTodoTitle` 等于该待办标题，且 `a.bubble.Kind == BubblePlan`、`a.bubble.TodoTitle` 同为该标题；该待办的 `Done` 仍为 `false`；`len(openTodos)==0` 时 `ShowPomodoroPicker()` 的 `AllowUnattached == true`，有未完成待办时 `AllowUnattached == false`。

- [ ] **T14 — 堵住会绕过门控的 resume 旁路**
  [前置] T13
  文件：`app_pomodoro.go`
  内容：`StartPomodoro` 的既有 resume 分支（`Phase==focus && !IsRunning && CurrentTodoID==in.TodoID && Remaining>0`）增加 `&& !st.PlanPending` 条件——`PlanPending` 为真时必须走 T13 的门控路径；`ResumePomodoro` 在 `PlanPending` 为真时按「应答（空计划）」处理（等同 `StartPomodoroWithPlan("")`），使门控不可被计时命令绕过（§13.1 的「停止可恢复」语义在非门控状态下一字不变）。
  完成判据：构造「休息到点后的门控态」（`Phase=focus, RunStopped, CurrentTodoID="", PlanPending=true, RemainingOnHalt=专注整长`），调用 `StartPomodoro({TodoID:""})` 后 `a.store.Pomodoro().PlanPending == true` 且 `RunState != store.RunRunning` 且 `EndsAt == nil` 且 `a.bubble.Kind == BubblePlan`；同一门控态下调用 `ResumePomodoro()` 后 `PlanPending == false`、`RunState == store.RunRunning`、`EndsAt != nil`、`a.focusPlan == ""`；在 `PlanPending=false` 的「用户主动停止」状态（`Phase=focus, RunStopped, RemainingOnHalt>0, CurrentTodoID="t1"`）调用 `StartPomodoro({TodoID:"t1"})` 后 `CurrentTodoID == "t1"`、`RunState == store.RunRunning`，且未设置 `PlanPending`、未弹任何气泡（既有 resume 语义保持）；既有 `TestStopIsRecoverable`、`TestResumeWithNoBankedTimeFallsBackToFullPhase` 断言仍全部成立。

- [ ] **T15 — 计划随记录落盘**
  [前置] T12、T3
  文件：`app_pomodoro.go`
  内容：`completePhaseLocked` 在调用 `a.store.AddPomodoroRecord(*out.Record)` **之前**执行 `out.Record.Plan = a.focusPlan` 并清空 `a.focusPlan`（专注结束即消费）。
  完成判据：走完「`StartPomodoroWithPlan("整理接口")` → 专注到点 `completePhase(finishPhase(focusState, cfg, ended, ended, false), false)`」后，`a.store.PomodoroRecords()` 中新增的那条 `Plan == "整理接口"` 且 `len(records) == 1`；以 `StartPomodoroWithPlan("")` 走同一流程后新增记录的 `Plan == ""` 且 `len(records) == 1`（跳过/留空仍产生记录）；两次流程结束后 `a.focusPlan == ""`。

- [ ] **T16 — 总结气泡显示本次计划**
  [前置] T15、T21
  文件：`app_pomodoro.go`、`frontend/src/main.js`
  内容：`showSummaryBubble` 在与既有 `TodoTitle` 相同的记录遍历中取出该记录的 `Plan` 并填入 `BubbleView.Plan`；前端总结分支在待办提示行下方显示 `els.bubblePlan`（文案「计划：\<plan\>」，`plan` 为空则整行 `hidden`）；输入框 placeholder 与「跳过」/「保存」按钮、Enter 提交逻辑不变。
  完成判据：在存在 `PendingRecordID` 且该记录 `Plan == "整理接口"` 时调用 `showSummaryBubble()`，`a.bubble.Kind == BubbleSummary`、`a.bubble.Plan == "整理接口"`；该记录 `Plan == ""` 时 `a.bubble.Plan == ""`；前端以 `{kind:'summary', plan:'整理接口', recordId:'r'}` 渲染后 `els.bubblePlan.textContent === '计划：整理接口'` 且未隐藏、`els.bubbleSecondary.textContent === '跳过'`、`els.bubblePrimary.textContent === '保存'`，以 `plan` 为空渲染后 `els.bubblePlan` 处于 `hidden`；对该输入框派发 `{key:'Enter'}` 仍触发 `App.SavePomodoroSummary(recordId, value)`。

## E. 记录页（T17–T20、T33a–T33d）

- [ ] **T17 — `EditPomodoroRecord` 绑定（一次性写回两字段）**
  [前置] T2
  文件：`app_pomodoro.go`
  内容：新增 `EditPomodoroRecord(recordID, plan, summary string) (State, error)`，在 `pomoMu` 下调用 `store.SetPomodoroPlanAndSummary`，成功返回 `a.GetState()`，失败返回该错误；`SavePomodoroSummary(recordID, summary string) (State, error)` **签名与语义一字不变**（总结气泡仍调用它）。
  完成判据：对一个已存在记录调用 `EditPomodoroRecord(id, "新计划", "新实际")` 后返回的 `State.Records` 中该记录 `Plan == "新计划"`、`Summary == "新实际"`、`PlanEditedAt != nil`、`EditedAt != nil`，且 `PomodoroRecord` 条数不变；`EditPomodoroRecord("不存在", "a", "b")` 返回非 nil 错误与零值 `State`；`SavePomodoroSummary(id, "只改实际")` 后该记录 `Plan` 保持不变而 `Summary` 更新；`internal/store` 与 `app_pomodoro.go` 中不存在任何删除记录的导出方法。

- [ ] **T18 — 记录页两行渲染**
  [前置] T17
  文件：`frontend/src/main.js`
  内容：`renderRecordItem` 的 **head 行逐字保持现状**（`${formatClock(rec.endedAt)} · ${rec.todoTitle || '未关联待办'}` + `${rec.focusMinutes}分`）；body 由一行改为固定两行：第一行「计划：\<plan\>」（`rec.plan` 非空）或退回「计划：\<rec.todoTitle 或 未关联待办\>」（`rec.plan` 为空），第二行「实际：\<summary 或（未填写）\>」；「改」按钮保留在「实际」行。
    裁定依据见 design.md §4.1（head 不动是规格卡文字的最小读法）。
  完成判据：以 `rec = {id:'r1', plan:'整理接口', summary:'写完了数据部分', todoTitle:'写周报', focusMinutes:25, endedAt:'2026-03-04T18:10:00+08:00'}` 渲染后，head 文本为「18:10 · 写周报」与「25分」；body 内依次出现两行文本「计划：整理接口」「实际：写完了数据部分」；`rec.plan === '整理接口'` 时 body 第一行不含「写周报」；以 `rec = {id:'r2', plan:'', summary:'', todoTitle:'写周报', ...}` 渲染后 body 第一行文本为「计划：写周报」、第二行文本为「实际：（未填写）」；以 `rec = {id:'r3', plan:'', summary:'', todoTitle:'', ...}` 渲染后 body 第一行文本为「计划：未关联待办」；以 `rec = {id:'r4', plan:'整理接口', summary:'', ...}` 渲染后 body 第二行文本为「实际：（未填写）」。

- [ ] **T19 — 记录页就地编辑两个文本域**
  [前置] T18
  文件：`frontend/src/main.js`
  内容：`editingRecordId === rec.id` 时渲染两个 `textarea`（上「计划」初值 `rec.plan`、下「实际」初值 `rec.summary`）+ 「保存」「取消」；保存调 `App.EditPomodoroRecord(rec.id, planValue, summaryValue)`，成功后 `editingRecordId = null` 并 `applyState(next)`；取消只退出编辑态并重渲染。
  完成判据：以 `editingRecordId = 'r1'` 渲染 `rec = {id:'r1', plan:'整理接口', summary:'写完了数据部分'}` 后存在两个 `textarea.record-input`，第一个 `value === '整理接口'`、第二个 `value === '写完了数据部分'`；改写两个值后点「保存」恰调用一次 `App.EditPomodoroRecord('r1', <新计划>, <新实际>)` 且 `editingRecordId` 变为 `null`；点「取消」不调用 `App.EditPomodoroRecord` 且 `editingRecordId` 变为 `null`；以 `rec = {id:'r2', plan:'', summary:''}` 进入编辑态后第一个 `textarea.value === ''`（计划可清空）。

- [ ] **T20 — 记录/气泡样式扩展（新增规则不含边框盒外装饰）**
  [前置] T18、T8
  文件：`frontend/src/style.css`
  内容：在既有 `.bubble-*` 一节内新增 `.bubble-plan`（与 `.bubble-todo` 同族的次要提示行样式）；在既有 `.record-line`/`.record-summary` 一节内新增 `.record-plan`/`.record-inputs`（两行文本域的纵向间隔与换行），使 body 两行在固定面板宽度内不溢出。
  完成判据：`frontend/src/style.css` 中 `.bubble-plan`、`.record-plan`、`.record-inputs` 三条规则存在；**新增的这三条规则体**内不出现 `box-shadow`、`filter`、`transform`（负面判据只作用于本任务新增的规则，不改动文件内既有的 `box-shadow`/`transform` 用法——例如既有确认卡片与会话浮层各自的规则）；`.dev/check-frontend.cjs` 中「气泡/面板区不得有边框盒外装饰」的既有检查通过；`cd frontend && npm run build` 退出码 0（样式可编译）。

- [ ] **T33a — 记录页按天分组的容器不变**
  [前置] T18、T19
  文件：`frontend/src/main.js`（断言既有实现，预期**不改动**）
  内容：把规格卡七「顶部按天折叠」落成可核对的判据；改动只允许落在 `renderRecordItem` 内部与 `.record-*` 样式小节，`renderRecords` 的分组构造与 `renderRecordDay` 的容器结构不得改动。
  完成判据：以「今天 2 条 + 昨天 1 条」的记录集渲染后，记录列表内存在 2 个 `li.record-day` 分组容器，每个分组内含 `button.record-day-head` 与 `ul.record-day-list`，且同一本地日期（`localDayKey(rec.endedAt)`）的记录落在同一分组（今天那组含 2 个 `li.record-item`、昨天那组含 1 个）。

- [ ] **T33b — 记录页折叠状态（只展开最新一天 / 新日期自动展开）不变**
  [前置] T33a
  文件：`frontend/src/main.js`（断言既有实现，预期**不改动**）
  内容：把规格卡七「只展开最新一天」及其跨重渲染保持语义落成可核对的判据。
  完成判据：以「今天 2 条 + 昨天 1 条」记录集首次渲染后，分组顺序为倒序（最新一天在前），`openRecordDays` 仅含最新一天（`groups[0].key`），该组 `.record-day-list` 不隐藏而另一组隐藏；随后 push 一条属于**新一天**的记录并重渲染后，该新日期被加入 `openRecordDays` 且其 `.record-day-list` 不隐藏（此前已存在的日期成员关系不变）；再点击某组 `record-day-head` 后该组在 `openRecordDays` 中的成员关系翻转一次（展开↔收起）。

- [ ] **T33c — 记录页分组标题「N 条 · M 分」不变**
  [前置] T33a
  文件：`frontend/src/main.js`（断言既有实现，预期**不改动**）
  内容：把规格卡七「分组标题『条数 · 分钟数』」落成可核对的判据。
  完成判据：以「某天 2 条、`focusMinutes` 分别为 25 与 15」的记录集渲染后，该组 `record-day-meta` 的 `textContent === '2 条 · 40 分'`；折叠该组后文案逐字不变。

- [ ] **T33d — 记录页底部汇总「共 N 条 · 累计专注 X 小时」不变**
  [前置] T33a
  文件：`frontend/src/main.js`（断言既有实现，预期**不改动**）
  内容：把规格卡七「底部『共 N 条 · 累计专注 X 小时』」落成可核对的判据。
  完成判据：`state.recordTotals = {count: 3, focusMinutes: 90}` 时 `els.recordsTotal.textContent === '共 3 条 · 累计专注 1.5 小时'`；`recordTotals.count` 为 0 时 `els.recordsTotal.textContent === ''`（空态不显示）。

## F. 门控自愈与边界（T23–T27）

- [ ] **T23 — 关闭面板时重弹计划气泡**
  [前置] T12
  文件：`app.go`
  内容：`SetPanelOpen(false)` 在 `a.mu.Unlock()` **之后**，若 `PlanPending` 为真且当前无气泡，则调用 `showPlanBubble()` 并重新计算布局返回（避免「面板折叠掉门控提问后计时器永久停住」，且不在持 `a.mu` 时调用 `showBubble`）。
  完成判据：在 `PlanPending=true`、`a.bubble == nil`（模拟面板打开时被折叠）且 `a.panelOpen == true` 时调用 `SetPanelOpen(false)`：`a.bubble.Kind == BubblePlan`、`a.layout.Bubble.Kind == BubblePlan`、`a.panelOpen == false`；在 `PlanPending=false` 时调用 `SetPanelOpen(false)` 不产生任何气泡（`a.bubble == nil`）；在 `SetPanelOpen(true)` 后 `a.bubble == nil`（既有折叠行为不变）；`TestBubbleAndLayoutStayInStep` 断言仍全部成立。

- [ ] **T24 — 重启时重弹计划气泡**
  [前置] T12、T4
  文件：`app_pomodoro.go`
  内容：`initPomodoro` 在既有「`PendingRecordID != ""` 时重弹总结气泡」之外，增加「`PlanPending` 为真时重弹计划气泡」（同一 `out.State` 判读），避免门控在重启后变成死路。
  完成判据：预置 `PlanPending=true` 且 `Phase=focus, RunStopped, RemainingOnHalt=专注整长` 的状态文件后，用同一 store 新建 `App` 并调用 `initPomodoro()`：`a.bubble.Kind == BubblePlan`、`a.store.Pomodoro().PlanPending == true`（未替用户应答）、`RunState != store.RunRunning`；既有 `TestParkedRecordIsReOfferedAfterRestart` 与 `TestParkedPhaseSurvivesRestartWithoutRunning` 断言仍全部成立。

- [ ] **T25 — 计划待答时拒绝隐藏桌宠**
  [前置] T12
  文件：`app_hotkey.go`
  内容：新增 `var errPlanBlocksHiding = errors.New("番茄钟还在等你写计划，先答完它再隐藏桌宠")`；`HidePet` 在既有 `pendingSummaryWaiting()` 检查旁增加计划门控检查（判定在解析窗口之前），命中即返回该错误且不改变 `hidden`/气泡/计时状态。
  完成判据：在 `PlanPending=true` 且 `a.bubble.Kind == BubblePlan` 时调用 `HidePet()`：返回 `errors.Is(err, errPlanBlocksHiding)` 为真、`a.hidden == false`、`a.bubble.Kind == BubblePlan`、`a.store.Pomodoro().PlanPending == true`；在无任何 pending 时调用 `HidePet()` 不返回 `errPlanBlocksHiding`（窗口缺失导致的其它错误可接受）；既有 `TestHideIsRefusedWhileASummaryIsWaiting`、`TestHideIsAllowedWithNothingPending` 断言仍全部成立。

- [ ] **T26 — 显式放弃/关闭路径清理门控标记与内存计划**
  [前置] T12
  文件：`pomodoro.go`、`app_pomodoro.go`
  内容：`skipPhase`（`pomodoro.go` 的纯函数）在离开阶段时清空状态上的 `PlanPending/NextTodo*`（跳过休息 = 立即起算下一个专注，既有「不弹计划气泡」语义不变）；`SetPomodoroEnabled(false)` 与 `ResetPomodoro` 清空 `PlanPending/NextTodo*` 并清掉 `BubblePlan` 气泡；**上述三条路径同时清空 `a.focusPlan`**——其中 `skipPhase` 是纯状态函数、不接触 `App`，故 `a.focusPlan = ""` 由 `App.SkipPhase`（`app_pomodoro.go:533`，既有 `a.transition(func(st, cfg, now) phaseOutcome { return skipPhase(...) })` 绑定入口）完成：唯一做法是**在该 `transition` 闭包体内**、紧跟 `skipPhase(...)` 的结果之后清空 `a.focusPlan`（闭包由 `transition` 在 `pomoMu` 下执行，`a` 已被捕获，无需新增锁、无需改动 `transition` 签名、无需改 `pomodoro.go` 的纯函数）；`SetPomodoroEnabled`/`ResetPomodoro` 本就在 `pomoMu` 下，原位清理。`a.focusPlan` 的读写全部位于 `pomoMu` 已加锁的函数体内（与 T12/T27 一致）。**理由是数据正确性**：不清会把上一次遗留的计划写进下一条永久记录，与 design.md §4 D7、§5 边界表两行自相矛盾。`reset` 的既有「保留 Enabled/CompletedCount」语义不变。
  完成判据（三条路径各一次，每条同时断言门控字段与 `a.focusPlan`；`focusPlan` 均在预置阶段经 `a.StartPomodoroWithPlan("整理接口")` 置为 `"整理接口"`，与 T12 同一驱动方式）：
  ① `SkipPhase()` 路径：预置休息态 `PlanPending=true, NextTodoID="t1", NextTodoTitle="写周报"`，执行 `a.SkipPhase()` 后 `a.store.Pomodoro()` 满足 `PlanPending == false`、`NextTodoID == ""`、`NextTodoTitle == ""`、`RunState == store.RunRunning`、`EndsAt != nil`，且 `a.focusPlan == ""`；
  ② `SetPomodoroEnabled(false)` 路径：预置 `PlanPending=true`，执行后 `a.store.Pomodoro().PlanPending == false`、`a.bubble == nil`、`a.focusPlan == ""`；
  ③ `ResetPomodoro()` 路径：预置 `PlanPending=true`，执行后 `a.store.Pomodoro().PlanPending == false`、`Phase == store.PhaseIdle`、`NextTodoID == ""`、`a.bubble == nil`、`a.focusPlan == ""`；
  另：既有 `TestResetClearsEverythingButCadence`、`TestSkipMovesOnWithoutRecording` 断言仍全部成立。

- [ ] **T27 — `focusPlan` 内存态丢失的容忍**
  [前置] T15
  文件：`app_pomodoro.go`
  内容：直接读取 `a.focusPlan` 的代码路径不得假定它非空；专注进行中重启后该次记录 `Plan` 为空字符串、不报错、记录仍产生；`focusPlan` 只允许在 `pomoMu` 下读写。
  完成判据：模拟「`a.focusPlan` 为空但专注正在进行」时驱动专注到点，`a.store.PomodoroRecords()` 新增记录存在且 `Plan == ""`；`grep -n "focusPlan" app_pomodoro.go` 的每一处赋值/读取都位于 `pomoMu` 已加锁的函数体内（`StartPomodoroWithPlan`、`completePhaseLocked`、`SetPomodoroEnabled`、`ResetPomodoro`，以及 `App.SkipPhase` 传入 `transition` 的闭包体——闭包由 `transition` 在 `pomoMu` 下执行）；`go test ./... -race` 无 data race 报告。

## G. 既有断言的预期同步（T28–T29）

- [ ] **T28 — 既有 Go 单测的预期同步**
  [前置] T4、T13、T14、T26
  文件：`pomodoro_test.go`、`pomodoro_gate_test.go`、`app_bubble_test.go`、`app_hotkey_test.go`、`app_reminder_test.go`、`internal/store/pomodoro_test.go`
  内容：只更新因本变更而必然改变的期望（休息结束由「自动接专注」变为「等计划应答」；`NextTodo*` 结转），并补入 design.md §5 边界表对应的断言；**不得**为了让测试通过而放宽既有不变量断言（门控不可关闭、记录不可删、非专注分支 `RunRunning + EndsAt` 语义、提示音/语音时机、提醒让位规则）。
  完成判据：`go test ./...` 全部通过；`pomodoro_test.go` 中关于「休息阶段 `CurrentTodoID/CurrentTodoTitle` 为空」的断言仍存在且通过；`pomodoro_gate_test.go` 中总结气泡不可关闭、应答才启动休息、重启重弹三条断言仍存在且通过；`internal/store/pomodoro_test.go` 中记录不可删除的断言仍存在且通过。

- [ ] **T29 — 前端接线 harness 同步与新增断言**
  [前置] T8、T11、T22、T23、T25
  文件：`.dev/check-frontend.cjs`
  内容：更新因本变更而改变的正则（气泡不可关闭的 Go 守卫由 `b\.Kind == BubbleSummary && pending` 改为覆盖 `BubblePlan`；前端守卫字面量由 `!== 'summary'` 改为覆盖 `plan`；`unansweredQuestionKind` 的三分支判定；Enter 提交断言覆盖 `StartPomodoroWithPlan`），并新增断言：`#bubble-plan` 元素存在、新增绑定在 Go 侧存在、`errPlanBlocksHiding` 存在、`:plan` 分支的 placeholder 文案存在。**既有记录页/滚动条检查逐条保留、不得删除或放宽**：①「the records list is grouped into collapsible days」（`renderRecordDay(` / `className = 'record-day'` / `.record-day-list` / `.record-day-head`）；②「the newest day is the one that starts open」（`openRecordDays = new Set(groups.length ? [groups[0].key] : [])`）；③「a day the user collapsed stays collapsed across re-renders」（`let openRecordDays = null` / `seenRecordDays` / `openRecordDays.has(group.key)`）；④「a newly appeared day opens by itself」（`!seenRecordDays.has(group.key)` → `openRecordDays.add(group.key)`）；⑤「the records tab still scrolls inside the panel」（`.record-list { overflow-y: auto }` 且 `recordsList.style.(width|height)` 未被赋值）。
  完成判据：`node .dev/check-frontend.cjs` 退出码为 0 且输出中 FAIL 计数为 0；该脚本内新增的四条断言（`#bubble-plan`、`StartPomodoroWithPlan`/`EditPomodoroRecord` 绑定、`errPlanBlocksHiding`、计划气泡 placeholder 文案）均出现在 PASS 列表中；且上述既有记录页/滚动条检查（`.dev/check-frontend.cjs:980-1027` 的 ①–⑤）**逐条仍在脚本内且出现在 PASS 列表中**（逐条点名核对，不得只核新增四条的通过情况）。

## H. 文档与验收（T30–T32）

- [ ] **T30 — `docs/reminder-pomodoro.md` 同步**
  [前置] T28
  文件：`docs/reminder-pomodoro.md`
  内容：§4.1 状态机图补「写总结 → 休息 → 计划 → 专注」；§4.2 标题与「可不选待办、只写计划」；新增 §4.5.2「计划气泡与专注由计划应答启动」（含不可关闭、幂等兜底、全屏静默、重启自愈）；改写 §4.5.1 末句「休息结束 → 自动接下一个专注，不受影响」为「休息结束 → 由计划应答启动下一个专注」；§6.2 记录页示意与说明改为计划/实际两行；§7.2/§7.3 补新字段与新绑定方法；§8.1 尺寸表新增 `300×180` 一行；§8.2 表格把计划气泡纳入「提问气泡」；§10 补本变更验收项；§15.1 文件清单与 §15.2 决定表补条目。
  完成判据：文档中 `§4.5.1` 末句不再包含「休息结束 → 自动接下一个专注，不受影响」字样；§8.1 尺寸表含 `300×180` 且既有 300×140/200/160 三行仍在；存在 `§4.5.2` 小节且含「计划」与「应答」；§7.2 列出 `planPending/nextTodoId/nextTodoTitle`、§7.3 列出 `plan/planEditedAt`、§7.4 列出 `StartPomodoroWithPlan` 与 `EditPomodoroRecord`；§10 含「计划气泡」「计划：」「实际：」各至少一条待勾验收项。

- [ ] **T31 — `README.md` 同步**
  [前置] T30
  文件：`README.md`
  内容：L58-73 的番茄钟/记录页段落补「休息结束弹计划气泡、由应答起算专注」「计划可留空、跳过同路径」「专注结束气泡显示本次计划」「记录页计划/实际两行、两者都可改」；既有「浮层不会顶掉提问气泡」引用块补上计划气泡。
  完成判据：README 番茄钟段落包含「计划气泡」与「计划」「实际」；引用块中出现「计划」与「提问气泡」同句；README 中不再出现「选待办不必选」之类的旧口径残留（即不再声称选待办是开始的唯一前置）。

- [ ] **T32 — 构建与全量验证**
  [前置] T28、T29、T30、T31、T33a、T33b、T33c、T33d
  文件：`frontend/dist/`（构建产物，被 `.gitignore` 忽略）、`build/bin/PetPC.exe`
  内容：`cd frontend && npm run build` → `wails build -s` → `go test ./...`。
  完成判据：`npm run build` 退出码 0 且 `frontend/dist/index.html` 存在；`wails build -s` 退出码 0 且 `build/bin/PetPC.exe` 存在；`go test ./...` 退出码 0 且无 FAIL；清理数据目录 + `PETPC_DEBUG=1` 启动该 exe 后，debug.log 中出现计划气泡相关的 `pomodoro:` 记录行且无 panic。

## I. 需求对齐映射（双向，逐条不遗漏）

### 规格卡七点 → 任务

| 规格卡点 | 任务 |
|---|---|
| 一、状态字段 | T1 |
| 二、记录字段（含向后兼容、无删除方法） | T2 |
| 三、BubblePlan 气泡（const/尺寸/内容/提交语义） | T7a、T7b、T7c、T8、T21 |
| 四、两条路径（休息到点 + 手动 picker；幂等兜底） | T4、T5、T6、T13、T14、T23、T24、T25 |
| 五、计划保存与落库 | T12、T15、T26、T27 |
| 六、总结气泡显示计划 | T16 |
| 七、记录页双行 + 就地编辑（顶部折叠/分组标题/底部汇总不变） | T17、T18、T19、T20、T33a、T33b、T33c、T33d |

### 澄清清单 Q1–Q9 → 任务

| 澄清项 | 结论 | 任务 |
|---|---|---|
| Q1 两个时刻都弹 | 休息到点 + 手动点「开始专注」 | T4、T13 |
| Q2 保留选待办气泡、改标题、可不选、确认后弹计划气泡 | 唯一路径 | T13 |
| Q3 静默路径沿用上次计划 / 取不到留空 | 气泡初值取最近记录 plan；静默路径记录 Plan 空 | T6、T5、T7c |
| Q4 未应答则计时器停住 | 计划气泡应答才起算 | T4、T12、T14 |
| Q5 不可随手关掉 | Go 拒绝 + 前端守卫 | T9、T11 |
| Q6 记录页两行、可改、老记录退回待办名 | 唯一布局 | T18、T19、T33a–T33d |
| Q7 留空按 Enter 也算完成；Enter/Shift+Enter/isComposing | 与总结同构 | T8、T12 |
| Q8 次按钮文案「跳过」 | 两按钮同路径 | T8 |
| Q9 绿地：D 阶段 git init，P 只读 | 本计划不含建仓任务 | （范围外） |

### 澄清 doubts / risks / placeholders → 任务

| 条目 | 任务 |
|---|---|
| doubts-1 计划存记录不存 State | T2、T12、T15 |
| doubts-2 补暂停放 app 层 | T4 |
| doubts-3 补算路径不弹 | T6 |
| doubts-4 全屏不弹、不补 | T5 |
| doubts-5 占「待回答提问」名额 | T10 |
| doubts-6 隐藏桌宠不得永久停住 | T25（+T23、T24 自愈） |
| doubts-7 尺寸 300×180 | T7a、T20 |
| risks-1 与 §4.5.1 文档冲突 | T30、T31 |
| risks-2 既有测试预期变更 | T28 |
| risks-3 隐藏后门控死路 | T25、T24、T23 |
| risks-4 未纳入 unansweredQuestionKind | T10 |
| risks-5 起算/暂停竞争抖动 | T4（`pomoMu` 内落盘后 emit + 幂等断言） |
| placeholders | 无（清单为空）→ 无对应任务 |
| prdCollection | 无 PRD 链接（清单为空）→ 无对应任务 |

### 超纲任务的溯源说明

| 任务 | 溯源 |
|---|---|
| T14（堵 resume 旁路） | 规格卡四-2「确认后不直接计时，紧接弹计划气泡，由其应答起算」；该旁路会违反此条，属规格卡条款的必要实现，非新增功能 |
| T23（面板关闭重弹） | docs §8.2「占了就是删掉问题」+ 规格卡四-1「幂等兜底（不弹的前提是用户已知情）」；面板折叠门控会使计时器永久停住，属 risks-3 同类的必要兜底 |
| T24（重启重弹） | 规格卡四-1「避免每次启动重复弹同一个问题」的反面（该重弹的是 `PendingRecordID`）；清单 doubts-6 与 risks-3 要求门控不得成死路 |
| T26（skip/关闭/Reset 清 `PlanPending`/`NextTodo*`/`focusPlan`） | 规格卡五「清空 PlanPending」的收尾一致性；避免僵尸门控（`NormaliseState` 只覆盖 idle 一种）；`focusPlan` 一并清空是 design.md §4 D7 与 §5 边界表两行的字面要求——不清会把上一次遗留的计划写进下一条永久记录，违反规格卡二「记录字段 Plan 表示『预先要做的事』」与五「专注结束时把该计划写进 PomodoroRecord.Plan」 |
| T27（`focusPlan` 丢失容忍） | 规格卡五「plan 存内存『当前专注计划』」的必然推论：内存态跨重启不存在，需明确其降级行为（`Plan` 空、记录照旧） |
| T29（`.dev/check-frontend.cjs`） | 仓库既有工程协议（该脚本是前端接线/气泡约束的回归闸门），本变更改动其断言对象，必须同批同步；既有记录页四条检查属「既有不变量」，只可保留不可放宽 |
| T33a–T33d（记录页不变项判据） | 规格卡七末句「顶部按天折叠、只展开最新一天、分组标题『条数 · 分钟数』、底部『共 N 条 · 累计专注 X 小时』全部保持不变」——该句在任务层原无判据，本轮补为四条只做防回归的断言（不新增行为） |

### 范围外（清单/规格卡明确不做，故无任务）

不做计划→待办新关联机制；不改提醒调度规则；不改窗口尺寸/裁剪、不引入阴影/滤镜/transform（T20 以负面判据守住）；不做数据迁移脚本；不新增删除/清空记录入口（T2、T17 以负面判据守住）；不改总结气泡门控语义；不做周期计划模板/历史计划下拉；不改设置页任何配置项；本 change 不执行 `git init`（D 阶段职责）。
