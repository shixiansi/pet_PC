# tasks: 待办与番茄钟搬进插件（路 A）

> 三个台阶，每阶单独交付、单独回退。阶 3（删面板）做完就回不去，放最后。

## 0. 交接状态（2026-10-27，第 10 轮）

**已经落地并且验证过的**

- `plugin-todo` 1.0.2：**已发布、已安装**。界面（宠物旁边那一列）、提醒常驻帧、`plugin.config`；
  数据仍读写宿主的 `todos.*`（单一数据源）。
- `plugin-pomodoro`：**源码 2.0.0**（自持状态机 `machine.js` + 记录 + 两道门控 + `config`），
  `market/tests/plugin-pomodoro-machine.test.js` **25 条全绿**。
  **但 2.0.0 未发布**：`market/index.json`、`dist/`、内嵌 sample、`%APPDATA%` 里都还是 1.0.0
  （驱动宿主计时器的那版脸）。
- 宿主侧：`panelSlot` 新增 `todos` / `pomodoro` 两个槽位 + 校验；右键菜单「日程」「番茄钟」
  优先打开槽位认领者的浮层面，没人认领时**回退**到旧行为。
- 门禁现状：`go vet` 0；`go test ./...` 除 `internal/desktop` 的自启项用例（`AGENTS.md` §7 的
  沙箱限制）全绿；`check-frontend.cjs` 169；`market npm test` 439。

**没做完的（按顺序）**

1. **宿主退休**（进行中）
   - [x] **提醒调度器已删**：`initReminders` / `startReminderLoop` / `tickReminder` 与 app.go 的两处调用；
         `RemindNow`（手动）保留并去掉它的排期副作用。`go vet` 0、`go build` 0、测试全绿（除 §7 沙箱项）
   - [x] 剩下的死代码：`decideReminder` / `nextInterval` / `jitterFunc` / `setNextReminder` /
         `logReminderState` / `makeUpMissedReminder` / `noteBreakReminderOwed` 与
         **app_pomodoro.go 的 4 处调用**（421/865/876/1097），以及 `remStop` / `remNextAt` /
         `remMissed` / `reminderCheckEvery` / `reminderView`（含 `State.Reminder` 与面板那条
         「下次提醒」条）——已全部删除；`go build` / `go vet` / `go test` 全绿（除 §7 沙箱项）
   - [x] **提醒恢复**：宿主调度器删掉之后，待办插件的 `remindEnabled` 默认值已改回 `true`
         （现在只有它一个在发），1.0.2 已重新打包/安装，`PetPC.exe` 重建于 18:32
   - [ ] `internal/store` 的待办族、面板的待办 tab（`timer.*` 与宿主计时器已完成，见下）
     - [x] **宿主计时器整块退休（第 13 轮）**：`app_pomodoro.go` 删除（它的气泡半边移进新的
           `app_bubble.go`，只留 `BubbleReminder` 与 `BubbleMenu`）；`internal/pomodoro` 整包删除；
           `internal/store/pomodoro.go` 删除（`PomodoroState` / `PomodoroRecord` / 记录与统计 /
           `IsFocusPhase`）；`internal/llm` 的 `PromptPomodoro` / `PromptSession` 与 prompt 的两段
           渲染删除；`store.Config` 的 8 个番茄钟字段与 `ReminderSettings.DuringBreak` 删除；
           托盘的 `TrayCmdTogglePomodoro` 删除；`BubblePick`、`unansweredQuestionKind`、
           `bubbleAwaitingAnswer`、`petMenuBlocked`、`pluginSurfaceBlocked` 删除——**宿主已经没有
           任何"提问"**，所以菜单与浮层面不再拒绝任何气泡；`revealForQuestion` /
           `hideAfterQuestion` / `revealedForQuestion` / `questionVisibility` 整块删除（连同
           `app_hotkey_reveal_test.go`）。第 11 轮删两道门控的那段记录随之作废，git 里有。
           前端：番茄条、折叠的番茄记录档案、番茄徽章、`plan`/`summary` 两个气泡分支与
           `#bubble-plan` 全部删除，桌宠菜单的番茄钟格改成"打开番茄钟插件的浮层面"，
           15 个绑定（含 `SavePomodoroSummary` / `StartPomodoroWithPlan` 两个早已失效的）与
           三份假绑定同步删除。
     - [ ] **遗留一：`timer.phaseEnded` 现在没有发射方**。它唯一的发射者是宿主计时器
           （`completePhaseLocked` → `notifyPluginPhaseEnded`），所以**语音插件的「番茄钟结束」
           那句不会再响**。事件名与 `SubscribableEvents` 暂时保留——删掉会让已装的语音插件
           `events.subscribe` 直接报错，比不响更糟。三条候选路（接受 / `plugin.share` 轮询 /
           新增一条很窄的"插件声明事实"方法）写在 `design.md` §4.5 的那一行里。
     - [x] **门禁已恢复（Go 与静态检查）**：`go build` 0、`go vet` 0、`go test ./...` 除
           `internal/desktop` 的 9 条 §7 沙箱自启项外全 `ok`、`node .dev\check-frontend.cjs`
           **150 条全绿**。`check-frontend.cjs` 为此改了三处：`goAppPomodoro`/`goPomodoroEngine`
           改成空串（两个文件都删了，留着只是让残留断言**响**而不是抛 ENOENT）、文件清单里的
           `app_pomodoro.go` 换成 `app_bubble.go`、"playback 路径不合成"读的文件同步换掉；
           另外删掉了 5 条拒绝类断言与整张 `RETIRED_GATE_CHECKS`（连同那 14 条被跳过的）。
     - [x] **遗留二：前端界面还没动（最要紧的一条）**。`main.js` 仍在调用 15 个已经不存在的绑定
           （`renderPomodoro` / `renderRecords` / `saveSummary` / `startWithPlan` /
           `ShowPomodoroPicker` / `SetPomodoroEnabled` / `Pause/Resume/Stop/Reset/SkipPhase` /
           `EditPomodoroRecord` / `ConfirmPomodoroStart`），`index.html` 还有 `#pomodoro*`、
           番茄记录 `<details>`、`#bubble-plan`、番茄钟菜单格，`test-harness.js` 与 `wailsjs`
           仍是 61 个绑定（Go 只剩 46）。**注意 `check-frontend.cjs` 抓不到这件事**：那条
           "fakes exactly the bindings Go exports" 只比对 `App.js` 与三份假绑定，不读 Go，
           所以它现在是绿的而应用会在运行时炸。要删的还有 `bubble-plan.test.js`、
           `records-plan.test.js`，以及 `pet-menu.test.js` 里关于番茄钟格与"提问占位"的断言。
     - [x] **绑定已删（第 14 轮）**：`App.js` 的 61 → **47** 个导出（删 14 个），`App.d.ts` 同步
           删 14 条声明，`frontend/src/test-harness.js`（jsdom 假绑定）同步删干净。
           **`models.ts` 未动**——它是生成的类型、前端是纯 JS 用不到；正确的修法是重跑
           `wails generate module`，别手改。
     - [x] **更正上一轮的一条错判**：`check-frontend.cjs` **抓得到**这件事，它有一条
           "every App.* call is an exported binding"。所以删完绑定之后门禁**变红了**，
           报的是 4 条：11 个已删绑定仍被 `main.js` 调用（`EditPomodoroRecord`、`SetPomodoroEnabled`、
           `ConfirmPomodoroStart`、`SavePomodoroSummary`、`StartPomodoroWithPlan`、
           `ShowPomodoroPicker`、`Pause/Resume/Stop/Reset/SkipPhase`）、`App.d.ts` 同样 11 条、
           "the two new bindings exist in the generated module and its types"、
           以及 **headless harness 的假绑定仍是 61**。
           **这个红是诚实的**：绿的时候门禁在说谎（`main.js` 调 11 个不存在的东西）。
           要修的是 `main.js` + `.dev/run-frontend-harness.cjs`，两者必须同一批：
           后者自带一份名单（约 90–102 行）**和**一批番茄钟场景（`SavePomodoroSummary` 的
           写记录/清提示/Enter 提交，约 517–522、767–813 行），只删名单会让场景调用不存在的假函数。
     - [x] **遗留三：文档已同步（第 15 轮）**。`README.md` 的番茄钟章节、操作表与"隐藏期间的例外"、
           `docs/reminder-pomodoro.md`（53KB，应整篇归档）、`docs/llm-chat.md`、`docs/voice.md`、
           `docs/genshin-tts.md` 都还在描述宿主的计时器。`AGENTS.md` §1 的立场
           （"待办/番茄钟/提醒是主角"）留到阶 3 面板退场时与 `panelSlot` 一起改写。
     - [ ] **教训**：这一轮把"修 Go 测试"和"删前端界面"各委派给一个子代理，**两个都 failed、
           一个文件都没落**。结论同第 8 轮：这种跨十几处、需要边改边跑门禁的收尾不能委派，
           要自己在上下文充足时做完，或者拆成"一个文件族 + 一次门禁"的极小步。
2. **退休两条权限**（`timer.*` 已完成，`todos.*` 未做）
   - [x] **`timer.read` / `timer.control` 已退休（17 → 15）**：五个 `timer.*` 插件方法
         （`timer.state/start/pause/resume/stop`）与模型工具 `pomodoro_start` 一起删——
         番茄钟插件 2.0.0 已经自己计时，宿主没有计时器可以借给插件了。
         七处全改：`capability.go` 的常量 / `AllowedPermissions` / `PermissionLabels`、
         `app_plugins.go` 的方法表与五个实现、`manifest_test.go` 的 `want` 与
         `TestAllowedPermissionsIsTheFifteen`、`.dev/check-frontend.cjs` 的硬编码期望词表、
         `cc/src/lib/bridge.ts` 的 dev 桩 `LABELS`；外加 README 的表格与标题、`AGENTS.md`
         的数字与测试名。`.dev/check-frontend.cjs` 还加了"这两项必须不存在"的负向断言。
         对话插件声明了 `timer.control` 却从不调用它（它只是宿主的 `pomodoro_start` 工具所需），
         所以同批升到 **1.6.9**：manifest 去掉该权限、`app.js` 的 `BUILD` 跟进、`TOOLS` 与
         `TOOL_STATUS` 去掉 `pomodoro_start`，重新打包并同步 `market/index.json` 与内嵌 sample
         （`embed.go` 的 `//go:embed` 换成 1.6.9，旧的 1.6.8 zip 已删）。
         门禁：`go build` 0、`go vet` 0、`go test ./...` 除 `internal/desktop` 的 §7 沙箱项全绿、
         `node .dev\check-frontend.cjs` 169、`market npm test` 439、前端 78、
         `tools/plugin-market` 的 `go vet`/`go test` 0。
   - [ ] `todos.read` / `todos.write` 退休 —— 见 §0.1：必须与"待办数据搬进插件"**同一次**做
   - [ ] 对话插件同步：`TOOLS` 去掉 `todo_list`/`todo_add`/`todo_complete`、manifest 去掉
         `todos.*`（与上一条同一批，版本再升一次）
3. **阶 3**：删面板与 `panelSlot`、左键解绑、`AGENTS.md` §1 立场改写、`docs/*` 同步
4. **发布**：`package.ps1` + 安装 + `wails build`（用户已授权）

**已知的过渡态**（`design.md` §4.5 有完整表）：提醒双发已用"插件侧默认关"止住；面板 tab 还在；
番茄钟两套计时器（宿主 1.0.0 已装、插件 2.0.0 未发布）。

**教训**：第 8 轮把"宿主退休"委派给子代理，三轮没有落下任何文件改动（它一直占着
`app_reminder.go`/`app.go`/`app_pomodoro.go`），最后只能中断。这类跨 4 个文件 + 删一批测试的
手术，要么在上下文充足时自己做，要么拆成更小的委派（一次只删一个函数族 + 跑一次门禁）。

### 0.1 第 15 轮：前端与文档的收尾（已完成）

上面 1 里那三条遗留（二 / 更正 / 三）在这一轮一起做完，**全部门禁复核过**。记录一下做了什么，
因为"删干净"比"删一半"多的那一半全在细节里：

- **`frontend/src/main.js`**：删掉 `renderPomodoro` / `renderRecords` / `renderRecordDay` /
  `renderRecordItem` / `localDayKey` / `formatClock` / `renderTimerBadge` / `tickCountdown` /
  `confirmStart` / `saveSummary` / `startWithPlan` / `focusAnswer` / `bubbleAwaitingAnswer` /
  `confirmDialog` / `confirmOpen`，以及 `pickerChoice` / `editingRecordId` / `openRecordDays` /
  `seenRecordDays` 四个变量、`pet:pomodoro` 订阅、250ms 倒计时定时器、`renderBubble` 里
  `pick` / `plan` / `summary` 三个分支与 `renderBubbleList` 的 `selectable` 参数。
  Escape 与拖动两处"不许关掉提问"的守卫换成无条件关闭。**番茄钟菜单格**保留（它是插件的入口），
  回退从 `SetPomodoroEnabled`（已删）改成打开控制中心——一个什么都不做的格子比"去装插件"更糟，
  而 toast 长在面板里、此刻必然关着。
- **`frontend/index.html`**：去掉 `#timer-badge`、`#bubble-todo`、`#bubble-plan`、`#bubble-input`、
  整条 `#pomo-*`、`#records-fold`、`#confirm`；气泡只剩两种 kind（提醒 / 菜单）。
- **`frontend/src/style.css`**：去掉 `.badge.timer`（连同让待办角标下沉的 sibling 规则）、
  `.pomo-*`、`.record-*`、`.bubble-plan`、`.bubble-todo`、`.bubble-choice`、`.bubble-input`、
  `.confirm*`，以及滚动条那组选择器里的 `.record-list`（5 处）。
- **`.dev/run-frontend-harness.cjs`**：假绑定名单 61 → 47；`baseState` 去掉
  `pomodoro` / `records` / `recordTotals` / `reminder` 与 `pomodoro*` 配置键；`DismissBubble`
  不再拒绝；`__showSummaryPrompt` 换成 `__showReminder`；**2c 从"浮层面不许顶掉提问"改成反方向**
  （气泡卡片里没有输入框、气泡开着时对话按钮照常请求浮层面、浮层面随即拿走槽位）；
  "记录档案在待办 tab 里"那条换成"宿主计时器没在页面上留控件"。
- **测试**：`bubble-plan.test.js`、`records-plan.test.js` 删除（它们描述的是已删的界面）；
  `pet-menu.test.js` 的"提问占位"三条删掉，换成一条反向断言（提醒气泡在时菜单照样打开），
  番茄钟格改成两条（认领槽位 → `OpenPluginSurface`；没人认领 → 控制中心）；
  `test-harness.js` 的 `baseState` / `bubbleView` 同步。
- **`check-frontend.cjs`**：删 17 条描述已删界面的断言（9b/9c/9d 六条、记录档案九条、
  "档案仍在待办页"、两条生成绑定），新增 4 条**缺席**断言（宿主计时器的 id 不许回来、
  `main.js` 不许读 `state.pomodoro`、样式表不许留旧卡片规则、两块角标的 sibling 规则不许回来）。
  结果 **133 条全绿**。
- **`wails generate module` 重跑了**（`App.js` / `App.d.ts` / `models.ts` 一起）：上一轮手删的
  14 个导出与 `PomodoroView` / `PomodoroRecord` / `PomodoroSettingsInput` / `PomodoroStartInput` /
  `RecordTotals` 全部由生成器重算出来，`App.d.ts` 里也不再声明它们。**没有手改 `models.ts`**。
- **文档**：README 的操作表 / 番茄钟章节 / 隐藏期间的例外 / 数据文件 / 项目结构 / 验证清单 /
  已知限制 / 关于对话框全部改过；`docs/llm-chat.md`（世界快照只剩待办）、`docs/voice.md`
  （能力表去掉 `timer.*`，并写明 `timer.phaseEnded` 现在没有发射方）、
  `docs/reminder-pomodoro.md`（开头加"整块退休"的说明，**没有整篇归档**——`AGENTS.md` §3.2 与
  `app_bubble.go` 按 §8.2 的编号引用它，而 §8 仍是窗口区域裁剪的唯一权威描述）。

**门禁（第 15 轮实测）**：`go build` 0、`go vet` 0、`go test ./...` 全 `ok`
（提权跑时 `internal/desktop` 的 §7 自启项也过）、`frontend` 的 `npm run build` ✓、
`npm test` **33 条全绿**、`node .dev\check-frontend.cjs` **133 条全绿**、
`node .dev\run-frontend-harness.cjs` **107 条全绿**。`tools/plugin-market` 这一轮没动，未重跑。

**仍然开着的**（不属这一轮）：遗留一（`timer.phaseEnded` 没有发射方）、`todos.*` 退休与待办数据搬家、
阶 3（删面板与 `panelSlot`）、以及 `plugin-pomodoro` 2.0.0 的发布。

## 阶 1：待办插件接管数据与提醒

- [x] 查清谁在用 `todos.read` / `todos.write`
      → **只有对话插件**（`com.shixiansi.plugin-chat`；QQ 与 bilibili 都没声明）。
      它的 `TOOLS` 用 `todo_list` / `todo_add` / `todo_complete` 让模型操作待办。
      **后果：退休 `todos.*` 就等于"AI 不能再替你加/勾待办"**——这是路 A 的直接代价，
      要跟用户确认（或者将来做一条插件间通道，`llm.borrow` 就是"借别人的配置"的先例）。
- [ ] 确认上面那条代价（默认接受：AI 只聊天，待办归待办插件）
- [x] 新插件 `market/plugins/com.shixiansi.plugin-todo`（manifest + panel.html + app.js + style.css）
      - [x] `storage` 存待办数组（`todos-v1`）
      - [x] 浮层面：面板那一列（`dx 160, dy -160, 380×520`），列表 / 勾选 / 删除 / 添加
      - [x] 常驻帧：提醒循环（间隔 + 抖动 + 时段 + 上限），挑条目后 `bubble.say`，上次时间存 `reminded-at-v1`
      - [x] `plugin.config`：`remindEnabled/IntervalMin/JitterMin/From/To/MaxItems`
      - [x] 主题三件套（否则 `check-frontend.cjs` 红）
- [x] **入口**：`panelSlot: "todos"` + 宿主校验（要求声明 surface）+ 前端 `openSlotPluginSurface('todos')`
      —— 右键菜单「日程」优先打开槽位认领者的浮层面，没人认领时**回退**到面板（阶 3 之前都不破）
- [x] **数据先不搬（对计划的修正）**：番茄钟是待办的**第二个消费者**（开始专注要先选一件事），
      所以数据不能在阶 1 就翻主——那样计时器会从旧列表里挑。插件这一版改用 `todos.read/write`
      读写宿主的同一份列表，**单一数据源**；`todos.*` 与数据的退休挪到阶 2，和番茄钟一起。
- [x] 待办插件接上宿主数据：`todos.list/add/toggle/remove`；`todos.changed` 事件驱动重读
      （两个帧 + 计时器看到同一份）；自己只存 `reminded-at-v1`
- [ ] 阶 2 开始：番茄钟插件（状态机 + 两道门控），然后**同时**把待办数据搬进插件并退休 `todos.*`
- [ ] 宿主：删 `app_reminder.go`；删待办的 store 读写；删面板的待办 tab
      - [ ] **提醒调度器**（进行中）：删 `startReminderLoop`/`tickReminder`/`setNextReminder`/
            `makeUpMissedReminder`/`noteBreakReminderOwed` 与 `decideReminder`/`nextInterval`/
            `jitterFunc`，保留 `RemindNow`（手动）与气泡机制；`ReminderEnabled` 若失去读者，
            改由 `RemindNow` 尊重它，别留死开关
      - [ ] 收尾：把待办插件的 `remindEnabled` 默认值改回 `true`（宿主不再自动发之后）
- [ ] 权限表：退休 `todos.read` / `todos.write`（`AGENTS.md` §3.1 的七处 + README）
- [ ] 对话插件同步：`TOOLS` 去掉 `todo_list`/`todo_add`/`todo_complete`，manifest 去掉 `todos.*`
- [ ] 门禁：`go vet`、`go test`、`market npm test`、`check-frontend.cjs`、`package.ps1`、真机

## 阶 2：番茄钟插件接管状态机与门控

- [x] **第一半：番茄钟插件的"脸"**（`com.shixiansi.plugin-pomodoro` 1.0.0）
      - [x] 驱动宿主现有的计时器：`timer.state` + `timer.start/pause/resume/stop`（每次调用的返回就是新视图）
      - [x] 倒计时按宿主给的**绝对时刻** `endsAtMs` 本地走（丢事件不漂移、迟到的也不重复计）
      - [x] 选待办：`todos.list` 里未完成的那些
      - [x] `timer.changed` / `timer.phaseEnded` / `todos.changed` 事件驱动重读
      - [x] 入口：`panelSlot: "pomodoro"` + 菜单「番茄钟」优先开槽位认领者的浮层面，**回退**到原来的开关
      - [x] 主题三件套
      - 注：这一半**没有**第二份计时器——插件只是宿主那台的另一个界面
- [x] 第二半（第一段）：`internal/pomodoro/machine.go` 的**纯状态机**移植成插件里的 `machine.js`
      - [x] 阶段/运行态、开始/暂停/继续/停止/重置、阶段结束、跳过、`shouldAskPlan`、`reconcile`、
            `ceilSeconds/formatSeconds`（向上取整、分钟不封顶）
      - [x] 三条硬规则照搬：结束的**专注**一律产记录并把休息**驻留**（不受 autoStart 影响）；
            补课用 `endedAt` 记记录、用 `now` 起下一阶段；跳过计入长休息节律但不产记录
      - [x] `market/tests/plugin-pomodoro-machine.test.js`（25 条，当规格）
      - [x] `machine.js` 是第五个文件，打包脚本只带三个固定资源 → 用 manifest 的 `assets` 声明，
            已确认 zip 里有它
- [ ] 第二半（第二段）：把插件**接到** `machine.js` 上（自己的 storage + 每秒 tick + 记录），
      两道门控改成插件自己的输入界面
- [ ] **然后同时**：待办数据搬进插件、退休 `todos.*` 与 `timer.*`（§3.1 七处 + README）
- [ ] 托盘/热键：番茄钟相关项改为交给插件（或删掉）
- [ ] 门禁同上 + 行为的逐条对照（搬之前/之后的阶段与结转一致）

## 阶 3：面板退场

- [ ] 前端：删 `#panel`、三个 tab、`#chat-slot`、左键开关（`TogglePanel` 解绑）
- [ ] `panelSlot` 退休：对话插件的记录视图改走它自己的浮层面（面板那一列）
- [ ] 删 `store.PanelWidth/PanelHeight` 的使用方（区域几何变成"宠物 + 声明矩形"）
- [ ] 文档：`AGENTS.md` §1 的立场、§3.1 的权限数字、README 的操作表与冒烟清单、
      `docs/llm-chat.md` 的槽位章节、`docs/reminder-pomodoro.md` 整体归档
- [ ] 门禁：全量 + `verify-region.ps1` / `verify-plugin-surface.ps1`（区域三态变两态）

## 0.1 下一段必须知道的顺序（2026-10-27 收尾时补，第 12 轮更新）

**待办数据不能先退休。** 顺序被两件事钉住：

1. 宿主那台计时器**仍会按 todo id 查标题**（`StartPomodoro` → 选完待办即 `pomodoro.StartFocus`），
   所以 `internal/store` 的待办族必须等宿主计时器整块删掉之后才能动 → **先删宿主计时器**。
2. 待办插件现在的数据源**就是** `todos.read/write`（阶 1 的"自持 storage"被撤回，因为数据只能跟着
   它的最后一个读者走）。所以"删宿主待办"和"插件改用自己的 storage"必须是**同一次**改动。

**第 12 轮已经做完的**：`timer.read` / `timer.control` 退休（17 → 15），五个 `timer.*` 插件方法与
`pomodoro_start` 工具一起删，对话插件升 1.6.9 并重新打包（详见上面第 2 条）。所以"碰权限表"的
第一半已经过去了，剩下的是 **`todos.*` 那一次**。

**下一段的第一件事：删宿主计时器本体**（纯删除，不再动权限表）。顺序建议：

1. `app_pomodoro.go` 的计时器半边（`startPomodoroLoop` / `initPomodoro` / `tickPomodoro` /
   `completePhase*` / `StartPomodoro` / `PausePomodoro` / `ResumePomodoro` / `StopPomodoro` /
   `ResetPomodoro` / `FinishPhase` / 记录与统计的导出方法 / `PomodoroView` / `pomodoroView`）——
   **留下气泡**：`BubbleReminder`（`RemindNow` 还在用）与 `BubbleMenu`（桌宠右键菜单），
   `BubblePick` 随选择待办一起死。
2. `internal/pomodoro`（`machine.go` 与它的测试）与 `internal/store/pomodoro.go`（含
   `PomodoroState` / 记录 / `SavePomodoro` / `AddPomodoroRecord`）整包删除；`store.go`、
   `prompt.go`（对话上下文里的番茄钟那一段）、`voice_migrate.go`、`tray_windows.go` 的引用同步。
3. 前端：面板的番茄钟 tab、`#bubble-plan`、`saveSummary()`/`startWithPlan()` 与
   `RETIRED_GATE_CHECKS` 那张显式跳过表一起删；`app.go` 的 region/layout 分支。
4. 托盘与热键里番茄钟相关项删掉（插件自己开界面）。
5. 门禁：`go build` / `go vet` / `go test` / `check-frontend` / 前端 / `market npm test`。

**第 12 轮修正的两条环境事实**（上一轮记的"三处只读、不能委派"已经不成立）：

- `.dev/check-frontend.cjs`、`tools/plugin-market/cc/src/lib/bridge.ts`、
  `market/plugins/com.shixiansi.plugin-chat/*` **都能被文件编辑工具写入**；只读的只是
  **pwsh 进程**（工作区根和 `scratch/` 可写，`.dev` / `internal` / `frontend/src` /
  `tools` / `market` 的子目录对它是只读的）。所以这些编辑可以委派。
- 但**打包、复制内嵌样例、`npm test`** 必须提权跑一次：`market/tools/package.ps1` 要写
  `market/dist`、`market/index.json` 与 `internal/plugins/sample`；`vitest` 会因
  `spawn EPERM`（沙箱不许开命名管道）直接失败。这两类命令各用一次 `danger-full-access` 即可。
- Go 工具链：系统 Go 是 1.21，但**不要**设 `GOTOOLCHAIN=local`——模块缓存里已经有
  `toolchain@v0.0.1-go1.22.12`，`GOTOOLCHAIN=auto` 会自动切过去；只要把 `GOCACHE` 指到
  `%TEMP%` 下（`.dev\gocache` 对 pwsh 只读）。

**当前树是绿的**（`go test ./...` 全包 ok、`check-frontend` 169、前端 78、`market npm test` 439），
`PetPC.exe` 19:10（**第 12 轮没有改 `.go` 以外的行为，所以没有重建**；下次改完 `.go` 记得重建）。
`check-frontend.cjs` 里新增的 `RETIRED_GATE_CHECKS`（14 条）是**显式跳过表**，阶 3 删前端番茄钟
界面时连同它一起删。