# tasks.md — plugin-llm-usage

> 顺序是**代码 → 测试 → 文档六处 → 计数测试 → 手工验证**。
> 每一条都写"跑什么、看什么"，因为"应该能过"不算验证（`AGENTS.md` §9）。
>
> 本机工具链：系统 Go 是 1.21，`go.mod` 要 1.22。要么走 `pwsh -File .dev\env.ps1`，
> 要么 `$env:GOTOOLCHAIN='auto'` 且把 `GOCACHE` 指到 `%TEMP%` 下面（见 `AGENTS.md` §7）。

## A. 代码

- [x] **A1 · 估算函数**（新文件 `internal/llm/estimate.go`）
  写 `EstimateTokens(string) int`、`EstimatePromptTokens([]Message) int`、
  `EstimateCompletionTokens(Result) int` 与五个系数常量（design §3 的表）。
  图片按**张**计 1024，**不看** `data:image` 串的长度；`ToolCall.Function.Arguments` 计入 in；
  out 含 `Result.Reasoning`。
  验证：`go build ./...` 通过；`gofmt -l internal/llm` 无输出。

- [x] **A2 · 估算的用例**（新文件 `internal/llm/estimate_test.go`）
  中文 / 英文 / 代码三段、空串、只有空格、一条带 1 张图的 message、一条带工具调用的 message。
  验证：`go test ./internal/llm/ -run Estimate -count=1 -v` 六条全 PASS。

- [x] **A3 · 一把锁**（`app.go` + `app_plugins.go`）
  `App` 加 `usageMu sync.Mutex`（`callsMu` 旁边，`app.go:47-48` 那片）；
  把三处写入（`app_plugins.go:2083`、`:2094`、`:2177`）与 `llm.usage` 的读都锁进去；
  顺手在 `writePluginUsage` 的注释里补一句"这保证不撕裂，**不**保证不丢更新"（design §5）。
  验证：`go test ./... -run 'LLMChat' -count=1` 全绿（既有行为不变）。

- [x] **A4 · 退化路径**（`pluginLLMChat` 的 `send`，`app_plugins.go:2166-2188`）
  `res.Usage == nil` 时算 in（从 `round`）与 out（从 `res`），走同一个 `addReportedTokens` 写盘；
  日志那一行的 `tokens` 换成 `fmt.Sprintf("tokens in=%d out=%d（估算）", in, out)`。
  验证：`go test ./... -run 'LLMChat' -count=1` 全绿；
  新增用例**先写后跑**（B1）时应当看到"没有报用量"这句话不再出现在估算路径上。

- [x] **A5 · `pluginConfigInt` 重构**（`app_plugins.go`）
  抽出 `pluginConfigInt(man plugins.Manifest, values map[string]string, key string) int`，
  让 `pluginChatConfig` 的 `num`（`:2304-2307`）与 `:2326` 改用它。纯重构，无行为变化。
  验证：`go test ./... -count=1` 全绿（尤其 `TestLLMChatUsesThePluginsOwnEndpoint`、
  `TestLLMChatBorrowsTheLendersSettings`）。

- [x] **A6 · `llmDailyTokens`**（`pluginLLMChat`）
  在次数上限判定（`:2079-2090`）之后、`sent := usage.PlusAttempt` 之前加 token 上限判定：
  `pluginConfigInt(*row.Manifest, cfgStore.Values(), "llmDailyTokens")`，`0`/负数 = 不限，
  比较 `PromptTokens + CompletionTokens`；被拒那次**也计一次尝试**（照 `:2080-2083`）。
  文案点名数字：`已达今日 token 上限（%d，已用 %d：in %d/out %d）`。
  验证：B3 跑通；`go test ./... -count=1` 全绿。

- [x] **A7 · 新方法**（`app_plugins.go`）
  `func pluginLLMUsage(a *App, id string, _ []byte) (any, error)`：`PrivateDir` → `readPluginUsage` →
  返回 `{day, requests, tokensIn, tokensOut, budgetTokens}`（design §6）；
  `pluginMethods` 加一条 `"llm.usage": {perm: plugins.PermLLMChat, run: pluginLLMUsage}`，
  位置紧挨 `llm.chat`（`:451`）。
  验证：`go test ./... -run 'TestCapabilityTableIsComplete' -count=1` PASS；
  `go test ./... -count=1` 里 `TestDocumentedMethodCountMatchesTheTable` **此时应该是红的**
  （文档还写着 43）——这是预期，接着做 C 段。

- [x] **A8 · 归类**（`internal/plugins/calllog.go:28-48`）
  `DataMethod` 的数据面 case 加 `"llm.usage"`。
  验证：`go test ./internal/plugins/ -count=1` 全绿；真机 D 段的日志里它出现在 DEBUG 而不是 INFO。

## B. 测试

- [x] **B1 · `TestLLMChatEstimatesWhenTheServerReportsNoUsage`**
  一个 SSE 端点，末帧**不带** `usage`；打一次 → `pluginUsageOnDisk(t, a, id)`（`app_plugins_test.go:2015`）
  的 `PromptTokens > 0 && CompletionTokens > 0`，且 `Requests == 1`。
  验证：`go test ./... -run TestLLMChatEstimatesWhenTheServerReportsNoUsage -count=1 -v` PASS。
  反向对照：既有 `TestLLMChatStreamsToThePlugin`（`:2216`）断言服务器报的 3/2 **不被估算覆盖**。

- [x] **B2 · `TestLLMUsageReportsTodaysCounters`**
  打两次 → `llm.usage` 的 `requests == 2`、`day == store.UsageDate(time.Now())`、
  `tokensIn/Out` 与 `pluginUsageOnDisk` 逐项相等、`budgetTokens` 等于插件 config 里声明的那个数。
  验证：`go test ./... -run TestLLMUsage -count=1 -v` PASS。

- [x] **B3 · `TestLLMUsageNeedsTheChatPermission`**
  只授予 `plugin.config`/`storage` 的插件调 `llm.usage` → 返回错误，且错误文本含
  `未获权限 llm.chat`（闸门在 `dispatchPluginRequest`，`app_plugins.go:575-577`）。
  验证：同一条命令 PASS。

- [x] **B4 · `TestLLMDailyTokensStopsTheNextCall`**
  `llmDailyTokens` 设小值（例如 10，配一段必然超过它的中文）→ 第一次成功、第二次被拒，
  错误文本含那个数字；把 `llmDailyTokens` 改成 `0` → 连打三次都不被拒。
  验证：`go test ./... -run TestLLMDailyTokens -count=1 -v` PASS。

- [x] **B5 · `TestLLMChatUsageSurvivesConcurrentCalls`**
  两个 goroutine 各调一次 `llm.chat` → `pluginUsageOnDisk(...).Requests == 2`。
  能跑 `-race` 的机器上再加 `-race`（Windows 上 `-race` 需要 cgo 工具链，没有就只跑不带的那一遍）。
  验证：`go test ./... -run TestLLMChatUsageSurvivesConcurrentCalls -count=1` PASS；
  把 A3 的锁临时去掉时这条**应该红**（确认它真的在测锁）。

## C. 文档（六处 + 手册三节 + 假宿主）

- [x] **C1 · 假宿主的 `METHODS`**（`market/docs/plugin-testkit/fake-host.mjs:37-88`）
  加 `'llm.usage': { perm: 'llm.chat', where: 'pet' },`（控制中心不服务，理由同 `llm.chat`）。
  验证（**别对全文件 grep**，那条会给出 53，见 design §9 的注）：
  ```powershell
  $f='market\docs\plugin-testkit\fake-host.mjs'; $in=$false
  $rows = Get-Content $f | Where-Object {
    if ($_ -match 'export const METHODS') { $in=$true; return $false }
    if ($in -and $_ -eq '};') { $in=$false; return $false }
    $in -and $_ -match "^  '[a-z]+\.[a-z.]+':" }
  $rows.Count      # 现在 43，改完 44
  ```

- [x] **C2 · 假宿主的 `okResult`**（同文件 `:117-164`）
  加 `case 'llm.usage': return { day: '2026-11-02', requests: 3, tokensIn: 1200, tokensOut: 300, budgetTokens: 0 };`
  验证：`node -e "import('./market/docs/plugin-testkit/fake-host.mjs').then(m => console.log(m.okResult('llm.usage')))"`
  → 打印那个对象，而不是 `{ ok: true }`。

- [x] **C3 · 手册 §6.6**（`market/docs/plugin-dev.md:818` 起）
  加一小节写清四件事：in/out 怎么算（服务器报了就用它，没报就估算，估算只是量级）；
  `llm.usage` 的 payload 与返回；`llmDailyTokens`（`0` = 不限，**一次调用可能越界**，下一轮才拒）；
  含图请求的 token 是量级不是账单。
  **同时改正 `:889` 那句**「每一轮都算一次请求上限」——代码是每次 `llm.chat` 计一次
  （`app_plugins.go:2094`，`send` 里没有 `PlusAttempt`）。
  验证：`(Select-String -Path market\docs\plugin-dev.md -Pattern '每一轮都算一次').Count` → **0**。

- [x] **C4 · 手册 §9 两宿主表**（同文件 `:1496-1506`，`llm.chat` 在 `:1501`）
  把 `llm.usage` 加进 `llm.chat / llm.profile / llm.providers` 那一行（只有桌宠 ✅）。
  验证：`Select-String -Path market\docs\plugin-dev.md -Pattern 'llm\.usage'` 至少命中 §6.6、§9、§15 三处。

- [x] **C5 · 手册 §15 总表**（同文件 `:1780-1825`）
  加一行（写成表格行）：``| `llm.usage` | `llm.chat` | 今天用了几次、多少 token、上限多少 |``
  验证（数 §15 里的表格行，**别写死行号**——C3/C4 会把 §15 往下推）：
  ```powershell
  $in=$false; $n=0
  foreach ($line in Get-Content market\docs\plugin-dev.md) {
    if ($line -match '^## 15\.') { $in=$true; continue }
    if ($in -and $line -match '^\| `[a-z]+\.[a-z.]+`') { $n++ } }
  $n               # 现在 42，改完 43
  ```
  **注意 43 不是方法数**（总表本来就少 `plugins.services`，
  它是另一个漂移，不在本 change 的验收里；要补就单独一行、单独一次提交）。

- [x] **C6 · 六处「N 个方法」→ 44**
  `README.md:564-565`、`AGENTS.md:214`、`AGENTS.md:227`、`docs/plugin-dev.md:13`、
  `market/README.md:16`、`market/docs/plugin-testkit/README.md:153`。
  验证：`Select-String -Path README.md,AGENTS.md,docs\plugin-dev.md,market\README.md,market\docs\plugin-testkit\README.md -Pattern '\d+ 个(能力)?方法'`
  → 六条全都是 44。
  （第六份 `market/docs/plugin-dev.md` **没有**这个句子，别为它编一句；`43` 在那里只出现在
  `:1753` 的端口例子。）

- [ ] **C7 · 可选、另一个 module**：`tools/plugin-market/market_test.go:711` 的 pet-only 名单
  加 `llm.usage`。验证：`cd tools\plugin-market; go test ./... -run TestPluginHostInfoIsTheWorkshopTable -count=1` PASS。
  **本轮未做**：`tools/plugin-market/` 是独立 module，不在这次改动的授权写入范围内（只许根目录与
  `internal/**` 的 `.go`、`README.md`、`AGENTS.md`、`docs/**`、本 change 目录，以及市场里那两个
  docs 文件）。它现在是**绿的**，因为那条测试只断言 pet-only 的方法**不**在控制中心被广告，
  而 `llm.usage` 本来也没有进控制中心的注册表（design §11）。这一行留给下一次动那个 module 的人。

## D. 计数测试与全部门禁

- [x] **D1 · 计数守卫**
  `go test ./... -run TestDocumentedMethodCountMatchesTheTable -count=1 -v`
  → PASS。红的两种读法：`says 43 methods, the table has 44`（漏了 C6 的某一处）、
  `fake-host.mjs lists 43 methods, the host has 44`（漏了 C1）。

- [x] **D2 · CI 子集**
  ```powershell
  go vet ./...
  go test ./... -count=1
  cd tools\plugin-market; go vet ./...; go test ./... -count=1; cd ..\..
  node .dev\check-frontend.cjs
  ```
  看什么：`go vet` 无输出；`TestAllowedPermissionsIsTheFourteen` 与
  `TestCapabilityTableIsComplete` PASS（前者不动，后者只要求"每个权限至少一个方法"）；
  `check-frontend.cjs` 的 142 条全 PASS（本次不碰它盯的东西）。

- [ ] **D3 · 真机十二道**（需要交互桌面）
  `pwsh -File .dev\check-all.ps1` → 十二道全绿。
  改了 `.go` 就必须重建，否则 D4 验的是旧二进制：`wails build`（改的只有 Go，
  但**别**用 `-s` 省掉前端那一遍——省了会嵌旧的 `frontend/dist`，虽然这次前端没变）。

## E. 手工验证（真机，一条一条对）

> **E1–E5 本轮没跑**：它们要求一台交互桌面 + 一个真的模型渠道，而且 E1 要改
> `market/plugins/**`（本次授权里明确不许动）。**替代物是 B 段那六条自动化用例**：跨日（E5）=
> `TestUsageRollsOverOnANewDay`（手改 `date` 为昨天，断言 `requests == 0` 且 `day` 是今天，
> 且磁盘没被读改写），上限（E3）= `TestUsageDailyTokensStopTheNextCall`，账与日志一致（E2）=
> `TestUsageReportsTodaysCounters`。仍然**只有真机能验**的是两件：等级在默认 INFO 下不出现
> （E4，靠 `DataMethod` 归类 + 那条单测性质上是静态的）、以及一次真渠道的
> 估算/服务器比值（design §4）。前者由 `internal/plugins` 的归类表保证，后者需要网络。

- [ ] **E1 · 准备一个会说话的插件**
  用市场里的对话插件（`market/plugins/com.shixiansi.plugin-chat`，它的 manifest 在
  `:111` 已经声明了 `llmDailyBudget`）。在它的 `config` 里加一项
  `{ "key": "llmDailyTokens", "label": "每日 token 上限", "type": "number", "default": "0" }`
  重新装一次。**注意 `market/` 是独立 git 仓**：改完要么在那边单独提交，要么验完还原，
  别把实验留在工作树里。

- [ ] **E2 · 问三句，看账**
  在插件界面里问三句（其中一句带长历史，一句带图更好）。然后：
  - `llm.usage` 返回 `requests == 3`；
  - `<dataDir>\logs\pet.log` 里 `llm.chat 完成` **正好 3 行**（同一时间段）；
  - `tokensIn/Out` 与那三行里 `tokens in=… out=…` 的累加**逐位相等**；
    若某行带「估算」，说明那一轮服务器没报 usage，差值是估算的正常误差（design §4 的区间）；
  - `day` 是今天的本地日期（`2026-11-02` 这种形状），`budgetTokens` 是你填的那个数。

- [ ] **E3 · 上限真的会挡住**
  把 `llmDailyTokens` 改到**刚好小于** E2 的 `tokensIn + tokensOut`，重启桌宠，再问一句 →
  被拒，界面上的错误文本里有那个数字和"已用"。把 `llmDailyTokens` 改回 `0` → 立刻又能问。

- [ ] **E4 · 等级是对的**
  默认 INFO 下 `Select-String <dataDir>\logs\pet.log -Pattern 'llm\.usage'` → **0 命中**；
  `PETPC_LOG_LEVEL=debug` 重启后再问两次、再调一次 `llm.usage` → 出现
  `llm.usage ok（payload 0 B → …）`。这两条一起看，才说明它真的落在数据面（design §8）。

- [ ] **E5 · 跨天不靠定时器**
  把 `plugin-data/com.shixiansi.plugin-chat/llm-usage` 的 `date` 手改成昨天 →
  调 `llm.usage` → `requests == 0`、`day` 是今天（翻页发生在读的时候，
  `internal/store/llm.go:229`）。**别去改系统时钟**。

## F. 本轮的实测记录（2026-11-03，本机）

Go 工具链走 `GOTOOLCHAIN=auto` + `GOCACHE=%TEMP%\petpc-gocache`（`AGENTS.md §7`）。

| 命令 | 结果（末行） |
| --- | --- |
| `gofmt -l`（根目录 + `internal/**` + `tools/plugin-market`，不含 `.dev/`） | 无输出 |
| `go vet ./...` | 无输出 |
| `go test ./... -count=1` | `ok PetPC 21.603s`（七个子包全 ok） |
| `go test . -run TestDocumentedMethodCountMatchesTheTable -count=1 -v` | `--- PASS` / `ok PetPC 0.104s` |
| `go test ./internal/llm/ -run Estimate -count=1` | `ok PetPC/internal/llm` |
| `go test . -run TestUsage -count=1 -v` | 六条全 PASS（见下） |
| `cd tools\plugin-market; go vet ./...; go test ./... -count=1` | 无输出 / `ok PetPC/tools/plugin-market 2.302s` |
| `node .dev\check-frontend.cjs` | `ALL FRONTEND CHECKS PASSED (153)` |
| `cd market; node docs/plugin-testkit/plugin-check.mjs plugins/com.shixiansi.plugin-chat` | `1 个插件，0 个 FAIL`（18 条检查） |

`-run TestUsage` 的六条就是 B1–B5 + 跨日：

```text
--- PASS: TestUsageEstimatesWhenTheServerReportsNoUsage
--- PASS: TestUsageReportsTodaysCounters
--- PASS: TestUsageNeedsTheChatPermission
--- PASS: TestUsageDailyTokensStopTheNextCall
--- PASS: TestUsageRollsOverOnANewDay
--- PASS: TestUsageSurvivesConcurrentCalls
```

**A3 的锁做了变异验证**：把 `pluginUsageCountAttempt` / `pluginUsageAddTokens` 里的
`usageMu.Lock/Unlock` 临时去掉，`TestUsageSurvivesConcurrentCalls -count=5` **5 次全红**
（`requests = 1, want 2: a concurrent update was lost`），恢复后绿。所以那条用例真的在测锁，
不是在测运气。

**没做的**：D3（`check-all.ps1` 十二道，要交互桌面）与 E1–E5（真机 + 真渠道，且 E1 要改
`market/plugins/**`，不在授权范围）、C7（独立 module，见上）。**因此也没有重建 exe**：
这一轮验的是源码与 Go/Node 门禁，`build\bin` 里仍是上一个二进制。

**design §4 的实测比值没有跑**（需要一台会报 usage 的真渠道）。替代物是
`internal/llm/estimate_test.go`：它把估算量除以设计稿自己给的"真实值基线"
（汉字 0.6 token/字、英文 4 字符/token、代码 3 字符/token），断言中文落在 1.0–1.8、
英文散文 0.8–1.5、代码/JSON 0.9–1.2。本机实测这三个比值是 **1.67 / 1.33 / 1.00**，
都在 design §4 的区间里 —— 但这是"对基线"而非"对真服务器"，真渠道那一半仍待 E 段。

