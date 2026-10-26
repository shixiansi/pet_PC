# tasks.md — qq-chat-plugin

> 唯一执行清单。每条都写"做什么 + 完成判据"，已完成的勾上并注明验证方式。

## P1 宿主：`net.ws` 能力

- [x] **T1 权限位**
  内容：`internal/plugins/capability.go` 追加 `PermNetWS = "net.ws"` 与 `PermLLMBorrow = "llm.borrow"`，
  追加两条中文标签，`AllowedPermissions` 顺序追加（不插队）。
  判据：`TestAllowedPermissionsIsTheThirteen` 断言 13 项、每项都有标签、`background` 仍不是权限。
  （后续变更追加 `process` 后该测试改名为 `TestAllowedPermissionsIsTheFourteen` 并断言 14 项，
  见 proposal §4「权限词表」与 design §7.5。）

- [x] **T2 `net.ws` 的四个方法**
  内容：`app_plugin_ws.go` 实现 `ws.open` / `ws.send` / `ws.close` / `ws.state`，注册进
  `pluginMethods`，全部挂 `net.ws`。
  判据：`TestPluginWSCapabilityMethodsAreRegistered` 断言四个名字在表里且权限正确。

- [x] **T3 白名单与 scheme**
  内容：`ws.open` 只接受 `ws`/`wss`，`host:port` 必须命中 `netHosts ∪ ConfigHosts`，拒绝时不拨号。
  判据：`TestPluginWSOpenRefusesUnlistedHost`（并用桩服务断言"零次连接"）、
  `TestPluginWSOpenRefusesNonWebSocketScheme`、`TestPluginWSRequiresThePermission`。

- [x] **T4 令牌与打码**
  内容：令牌进 `Authorization: Bearer`；`ws.state` 的 url 查询串逐值打码；错误文案过
  `redactError`。
  判据：`TestPluginWSSendsTheTokenAsAHeader`（断言服务端收到的头）、
  `TestPluginWSStateNeverCarriesTheToken`（断言事件与 `ws.state` 里没有明文，且 URL 仍带着
  `access_token=***`）。

- [x] **T5 双向收发**
  内容：文本帧 → `ws.message` 事件；`ws.send` 写一帧。
  判据：`TestPluginWSCarriesBothDirections`（真 `httptest` + gorilla 服务端）。

- [x] **T6 退避重连**
  内容：拨号失败/连接断开按 1,2,4,8,15,30s 重试，成一次即重置阶梯。
  判据：`TestPluginWSReconnectsAfterTheServerDrops`（测试里把阶梯改成 10ms）。

- [x] **T7 关闭语义**
  内容：`ws.close` 停止 supervisor 且不再重连；关闭后 `ws.send` 报错。
  判据：`TestPluginWSCloseStopsRetrying`（关闭后 120ms 内无新 `ws.state`、状态为 `closed`）。

- [x] **T8 停用即断**
  内容：心跳 tick 顺带检查"已装 + 已启用"，不是就关闭。
  判据：`TestPluginWSClosesWhenThePluginIsDisabled`（心跳间隔调成 20ms）。

- [x] **T9 宿主退出关闭**
  内容：`App.shutdown` 调 `closePluginWS`。
  判据：代码路径 + `go vet`/`go test ./...` 全绿；已在 `app_reminder.go` 的 shutdown 注释里
  写明"不留给进程消失"的理由。

- [x] **T10 依赖**
  内容：`gorilla/websocket` 由间接依赖提升为直接依赖（版本 v1.5.3 不变）。
  判据：`go build ./...` 通过，`go.sum` 无新增行。

## P2 宿主：`llm.chat` 的上下文与借用

- [x] **T11 `history`**
  内容：接受 `[{role,text}]`，裁剪 role/条数/总字数，最旧先丢，拼在系统提示词与新消息之间。
  判据：`TestLLMChatReplaysHistory`（顺序）、`TestLLMChatDropsUnknownRolesAndCapsHistory`
  （丢 `system`/`tool`/空串、超出上限丢最旧）。

- [x] **T12 `system`**
  内容：给了 `system` 就只用它，不注入世界快照。
  判据：`TestLLMChatSystemPromptReplacesTheWorldSnapshot`（断言系统消息逐字相等且不含
  「以下是当前的真实情况」）、`TestLLMChatWithoutSystemKeepsTheWorldSnapshot`（另一半不回归）。

- [x] **T13 `shareLLM`**
  内容：manifest 新增字段；校验"必须同时申请 `llm.chat`"。
  判据：`TestManifestShareLLM`（合法、默认 false、缺 `llm.chat` 被拒且错误点名两者）。

- [x] **T14 借用解析**
  内容：自有优先 → 否则按 `llm.borrow` 找 id 最小的合格借出方 → 都没有则报错点名。
  判据：`TestLLMChatBorrowsTheLendersSettings`、`TestLLMChatPrefersItsOwnSettings`、
  `TestLLMChatBorrowRefusedWithoutThePermission`、`TestLLMChatBorrowNamesTheMissingLender`。

- [x] **T15 `llm.profile`**
  内容：报告 `ownReady` / `borrowAllowed` / `ready` / `source` / `model` / `lender{id,name}`，
  不含端点与密钥。
  判据：`TestLLMChatBorrowsTheLendersSettings` 里对 `llm.profile` 的断言。

## P3 宿主：`net.fetch` 请求头

- [x] **T16 受控请求头**
  内容：`headers` 只接受 `accept`/`authorization`/`content-type`/`user-agent`，值禁 CR/LF 且 ≤1024。
  判据：`applyPluginRequestHeader`（拒绝并点名允许集合）；由 `plugin-qq.test.js` 的
  「content-type 是 application/json」间接覆盖真实用法。

- [x] **T17 重定向复检**
  内容：`CheckRedirect` 逐跳复核白名单，最多 5 跳。
  判据：代码路径 + 说明写进 `pluginNetFetch` 注释（"一次性 GET 泄漏不了什么，一个
  Authorization 头就不同了"）。

- [x] **T18 `ws/wss` 参与白名单**
  内容：`ConfigHosts` 的 URL 分支额外接受 `ws`/`wss`。
  判据：`TestConfigHostsParsing` 新增两条（`ws://127.0.0.1:3000/event` → `127.0.0.1:3000`、
  `wss://bot.example.com/event` → `bot.example.com`）。

## P4 前端：多个常驻帧

- [x] **T19 `PluginHost` 改造**
  内容：`backgroundFrame` → `backgroundFrames: Map`；`syncBackground` / `closeBackground(id)` /
   `closeAllBackground` / `backgroundPluginIds`；`onMessage` 与 `routeEvent` 逐帧匹配。
  判据：`check-frontend.cjs` 静态断言 `new Map()` + `syncBackground(plugins)`；
  harness 的行为断言见 T21/T22。

- [x] **T20 `main.js` 选择逻辑**
  内容：`backgroundPlugins()`（已装 + 已启用 + 声明 background + 持有 `events` **或** `net.ws`），
  `renderBackgroundPlugins()` 调 `syncBackground`。
  判据：`check-frontend.cjs`「常驻帧的挂载点与资格门」与「每个合格插件各一帧」。

- [x] **T21 harness：两帧并存**
  内容：新增 `com.example.ws`（`net.ws` 授权）与 `com.example.bg`（`events` 授权）共存用例。
  判据：`run-frontend-harness.cjs` 的「two eligible plugins get two headless frames at once」、
  「both headless frames are sandboxed」、「each headless frame is mounted for its own plugin,
  and net.ws alone is enough」。

- [x] **T22 harness：摘一个留一个**
  判据：「removing one background plugin leaves the other frame mounted」、「the surviving frame
  belongs to the plugin that is still installed」。

## P5 插件本体

- [x] **T23 manifest**
  内容：`market/plugins/com.shixiansi.plugin-qq/manifest.json`（14 项配置、`net.ws` +
  `net.fetch` + `llm.chat` + `llm.borrow` + `storage` + `plugin.config`、`background: true`、
  `netHosts: ["127.0.0.1:3000"]`）。
  判据：`plugin-qq.test.js` 的 manifest 五条断言（含"唯一 secret 是 llmApiKey"与"≤16 项"）。

- [x] **T24 端点推导**
  内容：一个 `server` 设置推出 `http(s)://host[/prefix]` 与 `ws(s)://host[/prefix]/event`，
  令牌同时进查询串与 `ws.open.token`。
  判据：三条 handshake 用例（默认、带 token、整条 `https://…/milky/`）。

- [x] **T25 触发判定**
  内容：`atOnly` → 必须 @；否则 @ 或前缀；私聊直接丢；自己发的消息直接丢。
  判据：「a message that does not address the bot is ignored」「the bot's own messages never
  trigger it」「private messages are out of scope」「prefix mode answers without a mention」。

- [x] **T26 群过滤与限流**
  内容：`groupAllow` 白名单、同群最小间隔、同群单飞。
  判据：「the group allow-list is honoured」「the same group is not answered twice inside the
  cooldown」。

- [x] **T27 回复与引用**
  内容：`send_group_message`，`quoteReply` 时先 `{type:"reply",data:{message_seq}}` 再文本。
  判据：「a mention produces one model call and one reply, quoting the question」（断言 body
  逐段相等，含 `group_id` 为数字）。

- [x] **T28 每群记忆**
  内容：`hist_<群号>` 读/写，保留 `contextTurns×2` 条，用户那条带说话人昵称。
  判据：同一用例的两条记忆断言 + 「previous turns are replayed, capped to the configured count」。

- [x] **T29 失败只在日志里**
  判据：「a refusal is logged, never said into the group」（断言没有 `send_group_message`，
  且日志含宿主原文）。

- [x] **T30 状态观测**
  内容：`status` / `groups` / `log` 三个 storage key；工作台读它们渲染，并提供"忘掉这个群"。
  判据：「the workshop view never opens a second socket」（读三个 key、不发 `ws.open`）。

## P6 市场与打包

- [x] **T31 对话插件 1.4.0**
  内容：manifest 加 `"shareLLM": true`；`app.js` 回放历史（`history` 去掉刚问的那条、
  按 `llmHistoryTurns` 截断，该设置通过 `config.get` 读取）。
  判据：`market` 的 `plugin-app.test.js` 41 个用例全绿（含 `payload.text` 断言）。

- [x] **T32 打包**
  内容：`pwsh -File market/tools/package.ps1` 产出三个 zip、回写两份 `index.json`；
  `internal/plugins/sample/embed.go` 的文件清单更新（chat 1.4.0、qq 1.0.0），旧 1.3.0 删除。
  判据：`go test ./internal/plugins`（`TestBuiltinSampleMatchesIndex` /
  `TestBuiltinSampleAgreesWithTheMarketCatalogue`）全绿；语音包 sha256 与上一版逐字节相同。

## P7 文档

- [x] **T33 openspec 三件套**（本目录）
- [x] **T34 README**：权限表 11→13、`background` 双门、常驻帧多条、新增「QQ 群聊插件」章节
  （含数据流、为什么连接不在插件里、配置表、记忆与观测）。
- [x] **T35 `docs/voice.md` 的历史数字不动**（那份文档记录的是当时的门禁输出，改成今天的数字
  会让"当时的结论"变成假话；数字差异由 README 与本目录承担）。

## P8 1.1.0：插话概率与主动开口

- [x] **T36 三档触发**
  内容：`shouldAnswer` 由布尔改为三档——@/前缀一定回；`atOnly`（默认）拒绝其余；关掉后按
  新增配置 `replyChance`（0–100）抽签。manifest 加 `replyChance`（配置项 14 → 15，仍 ≤16）。
  判据：`plugin-qq.test.js` 四条——chance 100 时非 @ 也回、chance 0 时非 @ 不回、
  `atOnly` 开着时 chance 100 也不回、chance 0 时 @ 仍然回。

- [x] **T37 主动开口**
  内容：新增 `proactiveMinutes`（0=关闭）；每 60 秒抽 `1/N`（平均间隔 N 分钟、实际不规律）；
  只在最近 N 分钟有人说话、未在飞的群里开口；提示词明确"没有人在问你"；不引用消息；
  只记 assistant 轮次。
  判据：`plugin-qq.test.js` 两条——随机命中时产生一次带"没有人 @ 你"的 `llm.chat` 并发出
  一条**不含 reply 段**的消息；空群推进 6 分钟后**一次也不开口**。

- [x] **T38 房间活跃度索引**
  内容：`touchGroup` 在**每条**群消息（无论是否回答）更新 `groups` 索引的 `lastAt`；
  `remember` 只在回答时累加 `count`。
  判据：主动开口的两条用例（命中/空群）依赖它，且"不回答也不等于这个群死了"由第二条反证。

- [x] **T39 市场 exe 同步进构建门禁**
  内容：`.dev/check-all.ps1` 的 `wails build` 门禁在构建后把
  `tools/plugin-market/build/bin/PetPCPluginMarket.exe` 复制到 `build\bin\`（与
  `tools/build-all.ps1` 一致），并打印一行说明。
  理由：桌宠的「打开插件市场」启动的是 `PetPC.exe` 同目录的那份；只跑 `wails build -s`
  会让 `build\bin` 留着旧程序——旧程序不认识新权限，把已装插件显示成"manifest 无法解析"。
  判据：全量 `check-all.ps1` 跑完后 `build\bin\PetPCPluginMarket.exe` 的 `LastWriteTime`
  等于本次构建时间。

- [x] **T40 打包与本地安装**
  内容：`market/tools/package.ps1` 出 `com.shixiansi.plugin-qq-1.1.0.zip`，
  `embed.go` 清单更新，旧 1.0.0 包删除，`tools/install-qq-plugin.ps1` 指向 1.1.0。
  判据：`go test ./internal/plugins` 全绿（内置源摘要/版本一致性）、本地
  `plugins\com.shixiansi.plugin-qq\manifest.json` 的 `version` 为 `1.1.0`。

## 门禁结果

`pwsh -File .dev/check-all.ps1` → **ALL GATES PASSED**（13 道门，含四道真窗口像素门禁）：

| 门禁 | 结果 |
| --- | --- |
| `go vet ./...` | PASS |
| `go test ./... -count=1` | PASS（`PetPC` 9.9s / `internal/plugins` 3.8s / 其余同前） |
| `plugin market go vet + test` | PASS |
| `frontend builds`（两个程序） | PASS |
| `static wiring checks`（`check-frontend.cjs`） | PASS，**160 项** |
| `plugin front-end tests`（market vitest） | PASS，**124 个用例**（新增 plugin-qq 24） |
| `behaviour harness`（`run-frontend-harness.cjs`） | PASS，**147 项** |
| `wails build`（两个 exe） | PASS，`build\bin\PetPC.exe` 与 `PetPCPluginMarket.exe` |
| `region pixel checks`（真窗口） | PASS |
| `live overlay pixel checks`（真窗口） | PASS |
| `plugin surface pixel checks`（真窗口） | PASS |
| `plugin market window checks`（真窗口） | PASS |

> 一次中间运行里 `region` 失败过一次，报文是 `pet size unchanged (exact) -- cat height
> changed: 198 -> 189`。重跑时两次测量的猫框**逐像素相同**（`418,187 113x168`）并全绿，
> 与脚本自己写在注释里的已知抖动一致：顶边是 20 像素阈值的抗锯齿行，桌面（壁纸）透过来会
> 让那一行落在阈值两侧。它测的是"宠物有没有动"，而不是本次变更涉及的插件帧——
> 本次没有触碰窗口几何、区域裁剪与浮层面。
>
> 真机联通性（Yogurt ↔ 插件 ↔ 模型）无法在门禁里自动验证，逐步清单见 `design.md` §9。
