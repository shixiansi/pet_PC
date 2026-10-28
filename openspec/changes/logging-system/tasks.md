# tasks: 日志系统

## A. 共用的一层

- [x] **A1 — `internal/logging` 的写者**
  内容：`Level`（DEBUG/INFO/WARN/ERROR，`ParseLevel` 接受中英文）、`Entry`、`Line`/`ParseLine`、
  `Open(dir, name, Options{MaxBytes, Level, Now})`、`Log`/`Debug`/`Info`/`Warn`/`Error`/
  `Debugf`/`Infof`/`Warnf`/`Errorf`/`Pluginf`、2 MB 轮转保留一代、每条 open-append-close。
  判据：`go test ./internal/logging/` 9 个用例全绿（格式往返、等级词表、nil 接收者、等级门、
  轮转、过滤与排序、坏行丢弃、换行伪造、缺失文件照报）。

- [x] **A2 — `internal/logging` 的读者**
  内容：`Files`、`Query{MinLevel, Scope, Search, Limit}`、`Read`（两个文件 + `.1` 按时间归并、
  新在前、512 KB 尾读）、`Scopes`。
  判据：`TestReadFiltersAndOrders`（等级/范围/搜索/上限/大小写）与 `TestFilesReportsMissingOnes`。

## B. 桌宠

- [x] **B1 — 接上并一直在写**
  内容：`App.log`；`NewApp` 里 `logging.Open(dir, PetLog, {Level: logLevelFromEnv()})`；
  `logLevelFromEnv`（`PETPC_LOG_LEVEL` 优先，其次 `PETPC_DEBUG`，否则 INFO）；`startup` 写一行
  「启动：数据目录 / 日志级别 / 日志文件」。
  判据：真机 `PETPC_DATA_DIR=<临时> PETPC_LOG_LEVEL=debug` 启动 9 秒后 `logs\pet.log` 有
  启动/托盘/模型配置/页面 DEBUG/显示窗口 5 行（2026-10-28 实测，格式见 design §2）。

- [x] **B2 — `DebugLog` 与 `PageLog`**
  内容：`DebugLog` 变成 DEBUG 级转发（不再有 `PETPC_DEBUG` 门）；新增绑定
  `PageLog(level, message)`（未知等级按 ERROR）；`main.js` 的 `fail()` 与 toast 同句进 ERROR，
  `debug()` 的注释改成"落 `logs\pet.log` 的 DEBUG"。
  判据：`frontend/wailsjs/go/main/App.js` 由 `wails build` 重新生成后，
  `.dev/check-frontend.cjs` 的「两个 fake 恰好等于导出集合（39）」通过。

- [x] **B3 — 30 处旧调用按语义分级**
  内容：`app.go`（托盘/窗口/裁剪/位置/自启动）、`app_hotkey.go`、`app_chat_slot.go`、
  `app_pet_menu.go`、`app_plugin_surface.go`、`app_plugin_market.go`、`app_plugin_migrate.go`、
  `app_reminder.go`、`app_settingswatch.go`、`app_bubble.go` 的 `log.Printf`/`DebugLog` 全部换成
  带子系统的 INFO/WARN/ERROR；`boolText` 随之删除（唯一使用者是那条日志）。
  判据：`grep 'DebugLog(' app*go` 只剩绑定本体；`go vet ./...` 干净。

## C. 插件行为与模型调用

- [x] **C1 — 闸门每次调用一行**
  内容：`PluginRequest` 拆成「计时/记录」+ `dispatchPluginRequest`（检查顺序一字未改），成功
  INFO、拒绝或失败 WARN，带方法、payload 字节、返回字节、耗时、错误原文；payload **不进日志**。
  判据：`TestPluginRequestsAreLoggedWithTheirOutcome`（成功/未授权/未知方法各一行，且
  payload 里的句子不在文件里）。

- [x] **C2 — `llm.chat` 的形状**
  内容：请求（端点/模型/消息数/历史轮/工具名/图片数，DEBUG）、每轮（耗时/尝试/字数/tokens/
  finish/工具调用数，尝试>1 升 WARN）、完成（INFO）、失败（WARN + kind）、预算拒绝（WARN）、
  工具执行（INFO/WARN）。
  判据：`TestLLMChatIsLoggedWithoutTheConversationText`（请求与完成都在，用户的话与模型回复
  都不在）。

- [x] **C3 — `internal/llm` 的 `Logf` 钩子**
  内容：`Client.Logf`（nil 安全）+ 六处调用：POST 形状、HTTP 状态、`stream_options` 自愈、
  重试与等待、流摘要（帧数/字数/tokens）、非流式回退。宿主在 `llmClient()` 里接到 DEBUG。
  判据：`TestLogfRecordsAShapeAndNoContent`（429 后被记录，且不含请求正文、密钥、服务端返回体）。

## D. 控制中心

- [x] **D1 — 自己的日志**
  内容：`Market.log` + `logLevelFromEnv`；启动、安装/更新/卸装/启用/授权、市场地址、设置保存
  （只记键名）各一行；卸装 WARN（它删掉了插件的私有目录）。
  判据：`go test ./tools/plugin-market/` 全绿。

- [x] **D2 — 插件请求与 `LogTail` 绑定**
  内容：`Market.PluginRequest` 记一行（同宿主的形状）；新增 `logapi.go`：`LogQuery`/`LogLine`/
  `LogFileView`/`LogView`/`LogTail`（归并两程序、新在前、`note` 解释空结果、目录与四个文件）。
  判据：`TestLogTailMergesBothProgramsAndIsNewestFirst`、`TestLogTailFiltersReachTheRead`、
  `TestLogTailExplainsAnEmptyAnswer`。

- [x] **D3 — 诊断报告换用真实日志**
  内容：`dataFiles` 换成 `logs/pet.log` + `logs/market.log`；`== 日志尾 ==` → `== 日志 ==`
  （目录、四个文件、最后 40 行真实格式的行、被截断时的说明、空日志的三种解释）；
  删掉 `readLogTail` 与 `logTailBytes`。
  判据：`TestDiagnosticsCarriesTheLogTailAndSaysWhyItMayBeEmpty`（60 行经共享写者落盘 →
  报告含最新一行、"只显示最后 40 行"、不含第 0 行）。

- [x] **D4 — 左栏「日志」页**
  内容：`cc/src/pages/Logs.svelte`（等级 / 子系统 / 搜索 / 自动刷新开关 / 刷新 / 复制 /
  行表（时间|等级|子系统|插件|正文）/ 文件与目录 / 隐私与开关说明）；`App.svelte` 加一页；
  `tokens.css` 加 `--warn`（深色 `#d9a13c`、浅色 `#9a6b12`）；`bridge.ts` 加类型与
  **会筛选的** dev 桩。
  判据：`npm run check` 0 error；`npm run smoke` 61/61（新增 7 条：四段都在 / 等级有配色与
  竖线 / 默认不含 DEBUG / 文件列表 / 说明文字 / 切到「全部」出现 DEBUG / 无结果给出原因）。

## E. 门禁、文档与重建

- [x] **E1 — 静态接线检查**
  内容：`.dev/check-frontend.cjs` 新增 4 条（日志页在 rail 且读 `LogTail`、四个等级各有颜色与
  警告/错误竖线、dev 桩真的按三个条件筛选、两个程序都无条件开日志且 `PETPC_DEBUG` 的门没回来）；
  同时修掉两条因本次改动而失效的旧断言（提醒抑制的日志文案、桩里退役的 `todos.read`）。
  判据：`node .dev/check-frontend.cjs` → `ALL FRONTEND CHECKS PASSED (122)`。

- [x] **E2 — 文档**
  内容：README 新增「日志」一节 + 数据文件表两行；`docs/logging.md`（为什么、格式契约、等级、
  保留、隐私、加一行日志要改哪儿、边界）；`openspec/changes/logging-system/`；AGENTS.md §3.8
  的不变量。
  判据：文档里的标识符逐个在代码里确认存在（`logging.Open`、`LogTail`、`PageLog`、
  `Query.MinLevel`、`DefaultMaxBytes`、`DirName`）。

- [x] **E3 — 重建两个程序**
  内容：`pwsh -File tools\build-all.ps1`（先建 cc → 覆盖 `frontend/dist` → 再 `wails build -s`，
  两个程序都重建）。
  判据：`build\bin\PetPC.exe` 与 `build\bin\PetPCPluginMarket.exe` 时间戳都是本次；
  扫 `PetPCPluginMarket.exe` 的 UTF-8 字节串能找到「自动刷新（2 秒）」与「没有匹配的日志行」
  （编译期 embed，只改 cc/ 不重建等于没改）。

## F. 未做（明确留下）

- [ ] 页面上的「清空日志」——写者是另一个进程，删除/截断一个正在追加的文件只会得到稀疏文件。
- [ ] 跨进程实时推送——2 秒轮询在"一边操作一边看"的用法下看不出延迟。
- [ ] 日志里的请求 id 串联（一次 `llm.chat` 的请求行与完成行目前靠时间相邻而非 id）。
- [ ] 桌宠侧的日志视图（窗口被裁剪到宠物 + 一个槽位，塞不进可读的日志）。

## G. 本轮 `check-all.ps1` 的结果，以及两条**先前就红**的门

`pwsh -File .dev\check-all.ps1`：**10/12 PASS**（script encodings、两个 module 的 go vet/test、
frontend builds、control center checks、static wiring checks、plugin front-end tests、behaviour
harness、wails build、plugin surface 与 plugin market 两道像素门全绿——后者是在真实窗口里跑完的，
980×700、权限栏、工作区都量到了）。

两条红的与本改动无关，是**面板/实时浮层退休时留下的旧门**，本轮只是把证据记下来：

| 门 | 失败行 | 原因 |
| --- | --- | --- |
| `region pixel checks` | `FAIL panel became visible -- panel only 1.2%` | 它断言"打开面板后那块深色矩形可见"，而**面板已经退休**（`9208e18`）。脚本最后一次修改在面板退休之前 |
| `live overlay pixel checks` | `FAIL every floating box sits in the same column -- x=0 w=300 …` | 它断言**实时交流浮层**的三块矩形同列，而那个浮层已随对话插件搬走（`61be4bf` 之前） |

两条都需要一次独立的"退休这两道门"的改动（要么改成断言浮层面/气泡，要么删掉并在 `check-all.ps1`
里说明为什么不再有像素证据）。放在这里而不是顺手改：它们不是这次改动的产物，而"顺手把红门改绿"
正是这个仓库最不该做的事。
