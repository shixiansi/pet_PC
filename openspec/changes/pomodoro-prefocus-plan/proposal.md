# 番茄钟「专注前计划气泡」+ 记录页计划/实际双行展示

- change_name: `pomodoro-prefocus-plan`
- 目标仓库: `F:\pet_pc`（greenfield=true：目录内已有全部源码，仅缺 `.git`；本 change 不建仓，`git init` 属 D 阶段）
- 上游规格卡: 已批准规格卡（番茄钟：休息结束/开始专注时输入计划，专注结束输入结果，记录页双行）
- 上游澄清清单: `wiki/queries/checklists/req-muyylyax.md`（Q1–Q9、doubts 7 条、risks 5 条、placeholders 空）

## 1. 问题（现状实证，已逐处复读源码确认）

| 事实 | 出处（已实读） |
|---|---|
| 只在**专注结束**提问，且休息计时门控在该次应答上 | `app_pomodoro.go:691-711` `showSummaryBubble`；`pomodoro.go:227-233`；`app_pomodoro.go:591-626` `SavePomodoroSummary` |
| **休息结束**什么都不问，只播声音+语音，随后直接开始下一个专注 | `app_pomodoro.go:324-330`（else 分支）；`pomodoro.go:235-245`（`AutoStartNextPhase` 默认 true） |
| 记录结构无「预先要做的事」字段 | `internal/store/pomodoro.go:198-213` |
| 记录页只有「时间 · 待办名」一行 + 总结一行 | `frontend/src/main.js:925-1001` `renderRecordItem` |
| 专注结束时 `CurrentTodoID/CurrentTodoTitle` 被清空，休息阶段**不继承**待办关联 | `pomodoro.go:218-219`；断言见 `pomodoro_test.go:190-193` |
| 气泡位四态互斥，提问气泡是例外，且浮层「占了就是删掉问题」 | `docs/reminder-pomodoro.md` §8.2；`app_live.go:1033-1040`；`app_pomodoro.go:803-841` |
| 隐藏桌宠在「总结待答」时被拒绝（否则门控变死路） | `app_hotkey.go:218-229`、`var errSummaryBlocksHiding` L17；`app_hotkey_test.go:283-311` |
| 打开面板会把气泡折叠掉（`setBubbleLocked(nil)`） | `app.go:942-961` |
| 前端不可关闭守卫是硬编码的 `summary` 字面量（Escape、拖拽两处） | `frontend/src/main.js:1450`、`frontend/src/main.js:1596` |
| 前端接线断言硬编码旧守卫文本与旧气泡种类 | `.dev/check-frontend.cjs:569、576-577、632、643-647` |

## 2. 变更目标（逐条对应规格卡七点，无遗漏）

1. **状态字段**：`PomodoroState` 新增 `PlanPending bool`、`NextTodoID string`、`NextTodoTitle string`。
2. **记录字段**：`PomodoroRecord` 新增 `Plan string`、`PlanEditedAt *time.Time`；旧记录缺字段按零值读，**零迁移代码**，仍「永久、可改不可删」，**不新增任何删除方法**。
3. **BubblePlan 气泡**：`"plan"` 常量、`bubbleLogicalHeight` 新增 case、`bubbleHeightPlan = 180` 固定尺寸；标题「接下来想专注做什么？」、待办提示行（有则显示）、输入框 placeholder、按钮「跳过」「开始专注」；Enter 提交 / Shift+Enter 换行 / `isComposing` 不提交，与总结气泡**完全一致**；留空合法＝「跳过」。
4. **两条进入专注的路径都必经计划气泡**：① 休息自然到点（app 层在状态机上补「暂停 + PlanPending」并弹气泡，应答当刻起算；`restored=true` 补算路径与全屏静默路径都不弹、直接开始专注）；② 手动点「开始专注」→ 选待办气泡（标题改「想专注做哪件事？」）→ 确认后弹同一个 BubblePlan，其应答起算；选待办仍不标记完成。
5. **计划保存与落库**：前端绑定 `StartPomodoroWithPlan(plan string)`；Go 侧在 `pomoMu` 下把 `NextTodo*` 搬进 `CurrentTodo*`、清 `PlanPending`、`RunRunning + EndsAt = now + cfg.FocusMinutes`；专注结束时把 plan 写进 `out.Record.Plan` 一并落盘；跳过/留空同路径，Plan 为空字符串，记录照旧产生。
6. **总结气泡显示计划**：`BubbleView` 新增 `Plan string`（omitempty）；待办提示行下方显示「计划：<plan>」，为空则整行不显示；输入框仍为「实际做了什么」，Enter 语义不变。
7. **记录页双行 + 就地编辑**：有 plan → 第一行「计划：」，第二行「实际：」；无 plan（含全部历史）→ 第一行退回待办名；「改」→ 两个文本域 + 保存/取消，调新绑定一次性写回两字段（各自更新时间戳）；顶部按天折叠、只展开最新一天、分组标题「条数 · 分钟数」、底部「共 N 条 · 累计专注 X 小时」**全部不变**。

## 3. 与规格卡的三处事实性冲突及收敛结论（**必须由 PT 仲裁的头号项**）

规格卡的七点目标全部保留，但有三处**字面口径与仓库事实不符或留有死路**，本计划已在计划内收敛为唯一路径，不留未定分支：

| # | 规格卡/清单原话 | 仓库事实 | 收敛结论（唯一路径） |
|---|---|---|---|
| C1 | （方案五）「把 `NextTodo*` 搬进 `CurrentTodo*`（用户刚在 picker 选的 id 以其为准并刷新标题快照）」＋ 规格卡四-1「休息结束时从 `CurrentTodoID/CurrentTodoTitle` 复制过来」 | 专注结束时 `finishPhase` **已经清空** `CurrentTodo*`（`pomodoro.go:218-219`），且 `pomodoro_test.go:190-193` 断言「休息阶段不得继承专注的关联」。因此「休息结束时从 `CurrentTodo*` 复制」在休息到点那一刻**无值可复制** | 结转点前移到**专注结束时**：`finishPhase` 专注分支把本次会话的 `TodoID/TodoTitle`（已进 `out.Record`）同时写入 `NextTodoID/NextTodoTitle`；休息阶段 `CurrentTodo*` 仍为空（既有断言不变）。手动 picker 路径同样只写 `NextTodo*`。`StartPomodoroWithPlan` 用**同一个无条件搬运**把 `NextTodo*` → `CurrentTodo*` 并清空 `NextTodo*`。两条路径合并为一条数据流，无「以谁为准」的分支 |
| C2 | （清单 Q5「不能随手关掉」+ §8.2 槽位互斥）未规定「面板打开时门控气泡被折叠」怎么办 | `SetPanelOpen(true)` 会 `setBubbleLocked(nil)` 折叠气泡（`app.go:942-961`）。总结气泡允许如此，因为**记录页能改总结**；计划气泡在面板里**没有任何应答入口** → 计时器会永久停住且屏幕上无解释（与 risks 第 3 条同类、后果更严重） | 新增自愈口径：**面板关闭时**若 `PlanPending` 仍为真且当前无气泡，则重弹计划气泡（等价于「问题回来了」）。同时 `initPomodoro` 在 `PlanPending` 为真时重弹（与 `PendingRecordID` 的既有自愈同构），`HidePet` 在计划待答时按既有 `errSummaryBlocksHiding` 同构拒绝 |
| C3 | （清单 Q3 落地口径）「下次进入专注时计划输入框的默认值取『最近一条记录的 plan』」 | 规格卡七点未包含此条 | 补入：`showPlanBubble` 把「最近一条记录的 `Plan`」作为 `BubbleView.Plan` 下传，前端把它设为输入框初值（取不到则空）。**仅**用于气泡初值；被静默跳过的路径（全屏 / 补算 / 番茄钟关闭）该次记录 `Plan` 仍为空字符串，与 Q3「没有上次就留空」一致 |

另有一处**规格卡未规定的 UI 取值**（非仓库事实冲突）：记录页「第一行」是否指 head 行——规格卡只规定 body 两行，head 行（`时间 · 待办名` + `分钟数`）未提及。计划裁定 **head 逐字不变、body 固定两行**（改动面最小），其对老记录的唯一可见后果是待办名在 head 与 body 各出现一次。完整裁定与取舍见 design.md §4.1，并列入 `pt_decision.reason` 核对项。

## 4. 上游协议遵循说明（上游协议引用 → 本计划如何遵守）

下列协议**全部来自规格卡/澄清清单声明的可定位出处**，未引入任何外部协议标准；每条均已实读原始文件（非二手转述）。

| 协议引用（可定位出处） | 本计划的遵守方式 |
|---|---|
| `docs/reminder-pomodoro.md` §3 技术约束（固定尺寸、无阴影/发光/不因内容变尺寸变尺寸） | 「三、新增气泡尺寸」只新增 `bubbleHeightPlan = 180`，既有 200/160/140 一字不动；前端**本变更新增的三条样式规则**内禁止 `box-shadow`/`filter`/`transform`（任务 T20 完成判据含此负面判据，且不改动文件内既有用法） |
| §4.1 状态机（自动进入下一阶段开关默认开；跨重启按剩余时间续算） | `finishPhase` 非专注分支的 `RunRunning + EndsAt` 语义**字面保留**（T3 完成判据含「非专注分支语义不变」断言）；app 层只在其上追加「暂停 + PlanPending」，不改状态机既有转移 |
| §4.2 开始专注必须选待办 / 选待办 ≠ 完成待办 / 上次选过的高亮 | 选待办气泡保留，标题改为「想专注做哪件事？」；`AllowUnattached` 仅在列表为空时为真（`len(todos)==0`，不变）；选择待办仍不 `ToggleTodo`（T13 含负面判据）；「上次选过的那条继续沿用」由 `NextTodo*` 结转承载 |
| §4.5 / §4.5.1 专注结束必须写总结 + 休息由记录门控 | 总结气泡的输入框、Enter 语义、「跳过也产生记录」、门控时机（`SavePomodoroSummary` 启动休息）全部不变；`SavePomodoroSummary` **签名不变**（T17 明确要求），仅新增旁路的双字段编辑方法；§4.5.1 末句按 §3 冲突表改写（T30） |
| §4.7 全屏时静默（不弹气泡、不播声音、退出后不补一串） | T5：全屏中休息到点**不置 PlanPending、不弹计划气泡**，专注照常起算；退出全屏后**不补**迟到的计划问题（清单 doubts 已确认）。全屏取值经 `App.foregroundFullscreen` 注入点（默认 `desktop.IsForegroundFullscreen`，T4 声明、design §1.5 记载），故该分支在无窗口单测中可被 true/false 两态独立驱动 |
| §6.1 记录永久、可改不可删 | 不新增任何删除/清空入口；只新增 `SetPomodoroPlanAndSummary`（改）；T2、T17 完成判据含「store/绑定层仍无删除方法」的负面判据 |
| §6.2 记录页按天折叠 / 只展开最新一天 / 分组标题「条数 · 分钟数」/ 底部「共 N 条 · 累计专注 X 小时」/ `openRecordDays`+`seenRecordDays` 跨重渲染保留 | 四项**逐条落成任务级判据**：T33a（`.record-day`+`.record-day-head` 分组容器存在、同本地日期聚合）、T33b（分组倒序、初次渲染 `openRecordDays` 只含最新一天、新出现日期自动展开、点 head 翻转成员关系）、T33c（`record-day-meta` 文案 `N 条 · M 分`）、T33d（底部 `共 N 条 · 累计专注 X 小时`，空态为 `''`）；T29 另要求 `.dev/check-frontend.cjs` 既有记录页四条检查与滚动条检查逐条保留并通过（不得只核新增四条）；改动只落在 `renderRecordItem` 内部与 `.record-*` 样式一节内 |
| §8.1 气泡尺寸表 / 区域裁剪必须精确 | T7a 断言 `bubbleLogicalHeight(BubblePlan)==180` 且既有三种映射不变；T21 要求 `#bubble-plan` 元素存在、T20 要求卡片精确填满 Go 给的矩形（固定高度，不随内容变） |
| §8.2 四态互斥 + 提问气泡例外 + 表格语义 | T10：`BubblePlan` 纳入 `unansweredQuestionKind` → `OpenLive`/`speakProactive` 拒绝并给出专属文案；`showReminderBubble` 的 `bubbleAwaitingAnswer()` 拒绝路径自动覆盖计划气泡（复用既有表格语义，不新增规则） |
| §9 边界情况表（气泡显示时拖动 → 收起；进入专注 → 收起） | T11：前端 Escape 与拖拽两处 `bubble.kind !== 'summary'` 守卫扩为 `summary|plan`（计划气泡按 Q5 不可收起）；进入专注时 `showBubble` 照旧顶掉其他气泡 |
| §10 验收标准（像素级、逐条勾） | T30 要求把本变更的验收项按 §10 风格补入文档；T32 含 `go test ./...` 与打包实机核对 |
| §13.1 「停止」可恢复语义 | 非门控状态下停止/继续语义不变（T14 完成判据含既有 `TestStopIsRecoverable` 仍通过）；额外收敛见 design.md §4.2，`ResumePomodoro` 在 `PlanPending` 时按「应答（空计划）」处理，避免门控被绕过 |
| §15 实现状态与文件清单 | T30 要求同步 §15.1 文件清单与新增的「实现中做出的决定」条目 |
| `README.md` L58-73（番茄钟/记录页面向用户的描述与浮层不顶掉提问气泡的说明） | T31 同步，含「计划气泡也是不可顶掉的提问」这一句 |
| `.dev/check-frontend.cjs` 既有工程协议：JS 引用的 id 必须存在于 HTML、前端调用的绑定必须存在、气泡区不得有 border-box 外装饰、Enter 处理器不得跨气泡泄漏 | T22（绑定/类型同步）、T29（该 harness 的旧守卫正则与旧断言同步，并新增计划气泡断言；既有记录页四条检查 `.dev/check-frontend.cjs:980-1011` 与滚动条检查 `:1020-1027` 逐条保留、只可保留不可放宽）为显式任务；T8 保留 `els.bubbleInput.onkeydown = null` 的清理语义 |

未声明任何上述清单之外的协议标准。

## 5. 影响面（文件级）

Go：`internal/store/pomodoro.go`、`pomodoro.go`、`app_pomodoro.go`、`app_hotkey.go`、`app_live.go`（仅校验，若无断言改动则不动）、`app.go`（`SetPanelOpen` 自愈钩子；`App` 新增 `foregroundFullscreen` 全屏取值注入点，默认 `desktop.IsForegroundFullscreen`）。
前端：`frontend/index.html`、`frontend/src/main.js`、`frontend/src/style.css`、`frontend/wailsjs/go/main/App.js`、`App.d.ts`、`frontend/wailsjs/go/models.ts`。
测试：`pomodoro_test.go`、`pomodoro_gate_test.go`、`app_bubble_test.go`、`app_hotkey_test.go`、`app_reminder_test.go`、`internal/store/pomodoro_test.go`。
工程断言：`.dev/check-frontend.cjs`。
文档：`docs/reminder-pomodoro.md`、`README.md`。
构建：`frontend/dist/`（`.gitignore` 忽略但 `wails build` 依赖）→ `build/bin/PetPC.exe`。

## 6. 风险与缓解（承自澄清清单，逐条落到任务）

| 风险 | 缓解（任务映射） |
|---|---|
| 新门控与文档 §4.5.1 末句「休息结束 → 自动接下一个专注，不受影响」冲突，若不同步改文档会被审计判为违背设计 | T30 显式改写 §4.5.1 该句并新增 §4.5.2；T31 同步 README |
| 既有测试 `TestBreakEndReturnsToFocus`（`pomodoro_test.go:253`）与 gate 测试断言会因新门控失败，可能被误当回归 | T28 在**同一批**内更新既有断言（新增 T4/T13/T14/T26 的用例）；「预期变更」标注写入交付报告（非验收备注） |
| 隐藏桌宠时计划气泡不可见又不可关闭 → 永久停住 | T25（拒绝隐藏 + 同构单测）、T24（重启重弹）、T23（面板关闭重弹） |
| 未纳入 `unansweredQuestionKind` → 浮层会删掉门控提问 | T10（含 `OpenLive`/主动说话拒绝的断言） |
| app 层补暂停与 ticker 竞争，出现「先起算、后被暂停」抖动 | T4（`pomoMu` 内落盘后 emit；断言门控态下 `tickPomodoro` 不改变任何状态） |
| 面板打开折叠气泡 → 计划气泡无应答入口（本计划新发现的同类死路） | T23（面板关闭重弹） |

## 7. 范围外（承自规格卡，逐条不做）

不做计划→待办的新关联机制；不改提醒调度规则（§5 现状不变）；不改窗口尺寸/裁剪机制、不引入阴影/滤镜/transform；不做数据迁移脚本；不新增删除/清空记录入口；不改总结气泡的门控语义；不做周期计划模板/历史计划下拉；不改设置页任何配置项（不新增开关关掉计划气泡）。
