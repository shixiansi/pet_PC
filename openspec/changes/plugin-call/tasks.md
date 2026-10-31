# tasks: plugin.call

## 阶段 1 —— 宿主侧（本仓，Go）

- [x] `capability.go`：`PermPluginCall` + 进入 `AllowedPermissions` + `PermissionLabels` 一句
      人话；顺手修掉同文件里两处漂移（第 8 行"fifteen"、`PermEvents` 的标签还写着
      "订阅待办与番茄钟的变化事件"，而那两条广播已随 `timer.changed`/`todos.changed` 退休）
- [x] `manifest.go`：`Serves []string` / `CallTargets []string` + `validateCalls()`
      （两侧都要求 `plugin.call`；精确对子；不列自己；不重复；各限 8 项）+
      `SplitCallTarget` / `CallTargetKey` / `DeclaredCall` / `ServesMethod`；
      顺手修掉 `Background` 注释里"还要申请 background 权限"那句（没有这项权限）
- [x] `internal/plugins/call.go`：`CallPlugin(gate, id, payload)` → 授权信封，七步校验按顺序，
      信封只由校验过的值拼成（`ticket` 16 字节随机十六进制）
- [x] `app_plugins.go`：`"plugin.call": {perm: plugins.PermPluginCall, run: pluginPluginCall}`
- [x] `manifest_test.go`：`want` 列表 + 测试名（`TestAllowedPermissionsIsTheFourteen`）+
      `TestManifestCallDeclarations`（10 种拒绝 + 1 种通过）
- [x] `call_test.go`：成功用例断言信封是"声明的那一对 + 原样 payload + 不重复的 ticket"；
      拒绝用例 9 种，每种都断言"返回 nil"和"错误里点名了原因"；外加畸形 JSON

## 阶段 2 —— 页面侧（本仓，前端）

- [x] `plugin-host.js`：`TYPE_CALL` / `TYPE_CALL_RESULT`；`PluginHost.request()` 成为三处
      `onRequest` 的唯一实现（也是本页唯一调 `App.PluginRequest` 的地方）；
      `plugin.call` 两阶段投递；超时 30 s、深度 3、回答 64 KiB 上限；没有常驻帧时立刻失败；
      目标帧关闭时立刻让等待者失败（而不是等 30 秒）；顺手删掉 `PluginFrame` 里那份从未被
      用过的 `pending` / `nextId`
- [x] `frontend/src/plugin-call.test.js`：9 个用例（成功、对方拒绝、冒充帧被忽略、没有帧、
      帧中途关闭、深度上限、回答超限、超时、普通方法行为不变）

## 阶段 3 —— 文档与静态门禁

- [x] `README.md`：权限表加一行 + 标题数字 13 → 14 + `background` 那句补上 `net.fetch`/`plugin.call`
      + 一段"牌盘与调用是两种不同的距离"
- [x] `.dev/check-frontend.cjs`：期望词表 13 → 14，并断言 `PermPluginCall`
- [x] `tools/plugin-market/cc/src/lib/bridge.ts`：dev 桩的 `LABELS` 加一项（+ `events` 文案跟上）
- [x] `market/docs/plugin-dev.md`：manifest 两字段、消息总表两条、新增 §6.8（含被调用方的
      完整处理器片段）、`§9` 两宿主差异、`§15` 方法表；§6.8 之后各节顺延 6.9–6.12
- [x] `market/docs/plugin-testkit/fake-host.mjs`：`PERMISSIONS` / `METHODS` / `okResult`
- [x] `market/docs/plugin-testkit/plugin-check.mjs`：`serves` / `callTargets` 的形状、上限、
      不列自己、不重复、必须有 `plugin.call`
- [x] `docs/plugin-dev.md`（本仓的指针）：14 项权限 / 36 个方法

## 验证（阶段 1–3）

| 命令 | 结果 |
| --- | --- |
| `go vet ./...` | 退出码 0 |
| `go test ./... -count=1` | 6 个包全 ok |
| `go vet ./...` + `go test ./...`（`tools/plugin-market`） | ok |
| `cd frontend && npm test` | 5 个文件 / 53 个用例通过 |
| `cd frontend && npm run build` | 通过（`dist/assets/index-*.js` 含 `call-result`） |
| `node .dev/check-frontend.cjs` | ALL FRONTEND CHECKS PASSED (138) |
| `.dev/bin/wails.exe build -s` | `Built 'build\bin\PetPC.exe'`，exe 字节里能查到 `call-result` |

## 阶段 4 —— 记忆核心 + OneBot 接入端（插件仓 `market/`）

- [x] `validateBackground` 接受 `plugin.call`：一个"被问"的插件原来没法申请常驻帧（它没有宿主
      事件、没有 socket、没有轮询表）。同时改 `manifest.go` 的注释、README 的 `background` 行、
      手册的 §1/§4/§4.1、以及 `plugin-check.mjs` 的同一条规则
- [x] `market/plugins/com.shixiansi.plugin-memcore`：L0 人设 + L1 缓冲 + L2 摘要 + L3 情节 +
      L4 画像 + 全局账本/角色链；`serves: ["chat"]`；插件页只读浏览 + 清空一个会话
- [x] `market/plugins/com.shixiansi.plugin-onebot`：OneBot v11 正向 WS；@ / 前缀 / 私聊三种寻址；
      去重、冷却、引用与 @；`callTargets` 点名核心；插件页显示连接、计数与"最近一次失败"
- [x] `market/tests/plugin-memcore.test.js`：握手、`call` 往返、人设必在 system、
      两个会话互不可见、L1→L2/L3 晋升（一次请求同时产出摘要与事实）、坏 JSON 的兜底、
      画像按次数生成、插件页列出并清空
- [x] `market/tests/plugin-onebot-memcore-pair.test.js`：两个独立的 window 一起装，假宿主按
      manifest 的 `callTargets` / `serves` 转发；断言"一帧 → 群里一句"、记忆只在核心、
      两个群互不污染、重发只答一次、核心缺席时适配器只记不说、坏帧不影响后续、
      **协议端不回 `echo` 时回答仍然算送出去了**
- [x] `market/tools/package.ps1` 打包 + 回写 `index.json` + 复制进宿主内置源；
      `internal/plugins/sample/embed.go` 显式加上新 zip（那个 `//go:embed` 是逐个文件列出的）
- [x] `market/README.md`：权限一节改成指向手册（它抄的那份还列着早已退休的 `todos.*`/`timer.*`）、
      manifest 表里的数字与 `background` 规则更新、新增 §4.6 讲这套组合

## 阶段 5 —— QQ 端（Milky）成为接入端（用户手上没有 OneBot 协议端）

用户实际在用的是 Milky（Yogurt）那条，所以真正要跑起来的是**已有的 `com.shixiansi.plugin-qq`**：

- [x] `com.shixiansi.plugin-qq` 1.13.0：新增 `plugin.call` 权限 + `callTargets`
      （`…memcore/chat`、`…memcore/seed`）+ 新文件 `lib/memcore.js`
- [x] 回答路径**只在最前面加一条早退**（`bili.js` 的 `takeTurn`）：开关打开时交给
      `takeTurnViaCore`，关着时原路径一行未变——184 个既有用例因此全部保持绿
- [x] 收尾照旧：发进群、写本插件自己的转写、结算社交能量、随机贴一张表情。**不**做的是拼 prompt、
      解析围栏、写群记忆（核心自己记）
- [x] 主动开口 / 攒批插话在核心模式**停下**，并在日志里写明原因（它们要靠结构化回复里的 `silent`，
      而核心只给一句话；每次 tick 都变成"一定要说点什么"比不说更糟）
- [x] 图片过不了 `plugin.call`：数图片段、把"这条带了 N 张图"如实告诉核心
- [x] 核心新增 `seed` 方法（`serves: ["chat","seed"]`）：把 QQ 插件攒下的群记忆一次性导入 L3/L2，
      去重、不覆盖已有摘要。没有它，打开开关的那一刻旧记忆会变成没人再读的文件
- [x] `chat` 接受一个可选的 `hint`（≤200 字，只进本轮 system、不落盘），预留给"主动开口"这类临时指令
- [x] 开关住在插件自己的 `state/core` 里而**不是** manifest 的 config：宿主的 config 上限是 40 项，
      这个插件已经用满；跟着 `config.get` 的 10 秒轮询一起重读，所以最多 10 秒生效
- [x] `market/tests/plugin-qq-memcore.test.js`（13 个用例）：开关打开时一次 @ 变成一次
      `plugin.call` 且**不**调 `llm.chat`、核心的文本原样发进群、图片被如实告知、核心失败只进
      日志、本插件转写照写、开关关着时老路径照旧、`seed` 把 notes 变成 facts、主动开口与攒批停下
- [x] **真机修掉第二个自己的错**（宿主前端 + 1.13.2）：`frontend/src/main.js` 的
      `backgroundPlugins()` 是**第二份**"哪些权限能撑起常驻帧"的清单，`validateBackground` 加第四项
      时它没跟上。症状是这次日志里那三行：`plugin.call ok`（闸门点头了）→ 紧接着
      「问不到 … 它此刻没有常驻帧」——记忆核心从来没被挂上过帧。
      `.dev/check-frontend.cjs` 因此补了一条：两份清单必须同时点名 `plugin.call`。
      桌宠也因此**不带 `-s`** 重建（改了 `frontend/`）
- [x] 1.13.2：导入**全部失败时不盖章**（否则"试过了"被当成"导成功了"，用户只有手动拨开关才会
      重试），失败后 5 分钟内不再试；盖章加上 `seedVersion`，于是 1.13.1 留下的旧章不算数，
      升级后的下一次轮询会自动重试一次
- [x] **真机修掉第三个自己的错**（memcore 0.1.2）：核心的插件页每 5 秒问一次 `llm.profile`，
      而控制中心不服务它 → `llm.profile 没有执行` 把市场日志刷满。改成**按 slot 判断**，
      在 workshop 里根本不发这个请求（插件的 catch 只让界面好看，不让日志安静——被拒的调用在宿主
      那侧已经记了账）

## 验证（阶段 4–5）

| 命令 | 结果 |
| --- | --- |
| `cd market && npm test` | 11 个文件 / **529 个用例**通过（含 qq 原有的 184 个） |
| `plugin-check.mjs`（memcore / onebot / qq） | 各 0 FAIL（2 个 WARN：无条件读 config/storage，失败路径已处理） |
| `pwsh -File market/tools/package.ps1` | qq 1.13.0、memcore 0.1.1、onebot 0.1.0 的 zip + index.json + 内置源同步 |
| `go test ./... -count=1` | 6 个包全 ok（`TestBuiltinSampleMatchesIndex` / `…AgreesWithTheMarketCatalogue` 跟着新版本走） |
| `node .dev/check-frontend.cjs` | ALL FRONTEND CHECKS PASSED (138) |
| `.dev/bin/wails.exe build -s`（两个程序） | 桌宠与市场程序都重建；exe 字节里能查到 `…memcore-0.1.1.zip` 与 `…plugin-qq-1.13.2.zip` |

## 没做的（明确留着）

- [ ] 推送插件仓到 GitHub：本地 `market/` 落后 origin/main 三个提交（远端有 qqbot 1.0.2），
      而且**市场目录是从远端拉的**，本地打包不会出现在运行中的应用里（除非清空市场基址走内置兜底）
- [ ] 核心模式下的主动开口/攒批：要保留就得给核心加"要不要说"的判定（两次调用），现在选择停掉
- [ ] 核心模式的看图：图片过不了 `plugin.call`
- [ ] `plugin.call` 的流式转发（非目标：`llm.chat` 的流属于调用它的插件）
- [ ] 控制中心支持（非目标：它只挂一页、没有常驻帧，另一端没人答）
