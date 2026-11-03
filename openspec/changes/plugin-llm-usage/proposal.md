# proposal.md — plugin-llm-usage

> 让"今天用了多少 token"变成可观测的：`llm.chat` 记 in/out，新方法 `llm.usage` 读它，
> 插件自己的 `config` 多一个 `llmDailyTokens` 上限。**不新增权限。**
>
> 计划稿在 `docs/plugin-roadmap.md` §3.1 的 M7；本文件把它落到**当前工作树**上，
> 顺手记下计划稿里两个不存在 / 说错的标识符（见 §What Changes 末尾）。

## Why

| 事实 | 出处（逐条核过） |
| --- | --- |
| 宿主只数**请求次数**：进去一次 `llm.chat` 加 1 | `app_plugins.go:2094`（`usage.PlusAttempt(0, 0)`） |
| 每日上限是**次数**上限，默认 200 | `internal/store/llm.go:53`（`DefaultLLMDailyBudget = 200`）、`app_plugins.go:2079`（`usage.OverBudget`） |
| token 已经记了一半：**渠道报了 `usage` 就累加** | `app_plugins.go:2175-2182` → `store.LLMUsage.PromptTokens` / `CompletionTokens`（`internal/store/llm.go:210-211`） |
| 但渠道**不报**时（`res.Usage == nil`）整轮记 0，日志只留一句"没有报用量" | `app_plugins.go:2174` |
| 账本文件是宿主私有格式，没有方法能读 | `pluginUsageFile = "llm-usage"`（`app_plugins.go:3034`）、`readPluginUsage`（:3042） |
| 渠道响应**确实有** `usage`：流式与非流式都解析，而且主动要 `stream_options.include_usage` | `internal/llm/protocol.go:169`（`Usage`）、`:214`（`chunk.Usage`）、`:242`（`Frame.Usage`）、`internal/llm/client.go:342` / `:421`（落到 `Result.Usage`）、`:497`（要 usage） |
| 那次"要 usage"被网关拒绝时会**去掉它重试**，于是这条链上真有可能一轮都不报 | `internal/llm/client.go:280-290` |

于是：一次 200 字的提问与一次 12000 字历史（`maxPluginChatHistoryRunes`，`app_plugins.go:2359`）的提问都记作 **1**。
"某个插件今天烧了多少 token"这个问题，今天的程序里**没有任何一处**能回答：

- 插件读不到——`llm-usage` 不是契约，想读只能 `storage.get` 一个宿主私有文件（而且路径要在自己目录里猜）；
- 用户读不到——要开资源管理器找 JSON；
- 宿主自己只有一个会在下次调用时**悄悄翻页**的计数器（`RolledTo`，`internal/store/llm.go:229`）。

## What Changes

1. **补上退化路径。** `res.Usage == nil` 时按字符数估算 in/out，照样累加（新文件 `internal/llm/estimate.go`）。
   主路径不动：服务器报了就用服务器报的。
2. **新方法 `llm.usage {}` → `{day, requests, tokensIn, tokensOut, budgetTokens}`。**
   `app_plugins.go` 的 `pluginMethods` +1（43 → 44），挂 `plugins.PermLLMChat`。
   只读：不写文件、不计数、不花钱。
3. **新配置项 `llmDailyTokens`**（0 = 不限），与 `llmDailyBudget` 并列，由**插件自己的 manifest `config`**
   声明、用户在该插件的设置里改。超限拒绝并**点名数字**。
4. **写入串行化。** `App` 上新增 `usageMu`，把"读-改-写"包起来——现在一把锁都没有（design §5）。
5. **归类。** `llm.usage` 进 `internal/plugins.DataMethod`（数据面 / DEBUG）。忘了它就会落到
   "默认按动作记 INFO"，一个状态条每 5 秒刷一次就能把日志变成那次 9729 行的样子（`internal/plugins/calllog.go:7-11`）。
6. **文档。** 手册 §6.6 的 token 计量与上限、§9 两宿主差异表、§15 总表；`fake-host.mjs` 的
   `METHODS` 与 `okResult`；六处「N 个方法」43 → 44（design §9 有逐条清单与当前数字）。

### 为什么**不**新增权限

- 它读的是**宿主替这个插件自己记的账**（`plugin-data/<id>/llm-usage`），主语和 `llm.channels.get`
  完全一样："我自己的模型配置/用量"。同一句话已经有一个权限词，就是 `llm.chat`
  （`internal/plugins/capability.go:58`，`llm.channels.get/set` 就是这么挂的，`app_plugins.go:459-460`）。
- 加第 15 项权限要同步七处（`AGENTS.md` §3.1）+ README 的表格与标题，并在安装对话框里多问一句
  用户**无法区分**的话——"允许它看你自己的用量"和"允许它用模型"是同一件事的两面。
  先例：`pet.badge` 借 `pet.surface`（`app_plugins.go:448-450`）、`memory.read` 借 `storage`（`:426-429`）。
- 反方向也不通：`pluginMethod.perm` 是**必填字段**（`app_plugins.go:341-356`），"没有权限的方法"
  这个形状不存在；何况那会让"谁都能读别人烧了多少"变成一个必须另外回答的问题。

### 顺手改正两句错话（不改行为，也不改计数）

`market/docs/plugin-dev.md:889` 写着「一条 `llm.chat` 可能因为工具跑好几轮，每一轮都算一次请求上限」，
`app_plugins.go:2160-2165` 的注释也这么说。**代码不是这样**：尝试只在 `:2094` 计一次，
`send`（`:2166-2188`）里没有任何 `PlusAttempt`，所以工具多轮与传输重试（`res.Attempts`）都**不**再加。

本次**不动计数语义**（改它就是在动预算，而且 `Requests` 一直是"用户视角的问了几次"），
只把那两句话改成实际行为，并写明 `llm.usage` 的 `requests` 就是"几条 `llm.chat`"。

### 计划稿里两个对不上的标识符

- M7 写的 `usage.Tokens` **不存在**。真实字段是 `store.LLMUsage.PromptTokens` / `CompletionTokens`
  （`internal/store/llm.go:210-211`），wire 上的名字是 `tokensIn` / `tokensOut`（design §6 说映射在哪一行）。
- M7 写的「`TestDocumentedMethodCountMatchesTheTable` 的数字 43 → 44」也不准确：那个测试里
  **没有 43 这个字面量**，它比的是 `len(pluginMethods)`、六份文档里的「N 个方法」、
  以及 `fake-host.mjs` 的 `METHODS` 三者。要改的是**六处文档 + 方法表**，测试本身不动。

## Impact

| 文件 | 改什么 |
| --- | --- |
| `app_plugins.go` | `pluginMethods` +1；新 `pluginLLMUsage`；`send` 里的估算与记账；`pluginConfigInt` 辅助；`llmDailyTokens` 检查 |
| `app.go` | `App` 加一个 `usageMu sync.Mutex`（放在 `callsMu` 那片，:47-48） |
| `internal/llm/estimate.go`（新） | `EstimateTokens` / `EstimatePromptTokens` / `EstimateCompletionTokens` + 系数常量 |
| `internal/llm/estimate_test.go`（新） | 三段文本的比值、空串、图片、工具参数 |
| `internal/plugins/calllog.go` | `DataMethod` 的数据面 case 加 `"llm.usage"` |
| `market/docs/plugin-dev.md` | §6.6 新小节、§9 表一行、§15 表一行、:889 那句改正 |
| `market/docs/plugin-testkit/fake-host.mjs` | `METHODS` 加一条；`okResult` 加一条示例 |
| `README.md` / `AGENTS.md` / `docs/plugin-dev.md` / `market/README.md` / `market/docs/plugin-testkit/README.md` | 「N 个方法」→ 44（共 6 处） |
| `tools/plugin-market/market_test.go`（另一个 module，可选） | `TestPluginHostInfoIsTheWorkshopTable` 的 pet-only 名单加 `llm.usage` |

门禁（每条都能跑，见 tasks.md）：

- `go test ./... -count=1` — `TestDocumentedMethodCountMatchesTheTable` 从 43 逼到 44；
  `TestCapabilityTableIsComplete` 不变（`llm.chat` 已被三个方法用过）；
  `TestAllowedPermissionsIsTheFourteen` **不动**。
- `node .dev\check-frontend.cjs` — **不需要改**：它那份硬编码清单是**权限词表**和 `App` 的 Wails
  导出面（`:2041-2076` 比对 `export function`），能力方法不经过这些。但要跑（142 条）。
- `pwsh -File .dev\check-all.ps1` — 真机十二道；这条改动不碰窗口、不碰前端。
- **没有前端改动**，所以不需要为它重嵌 `frontend/dist`（发版仍要 `tools/build-all.ps1` 重建 exe）。

不新增权限 ⇒ `AGENTS.md` §3.1 的七处、`manifest_test.go` 的 `want`、`.dev/check-frontend.cjs` 的权限词表、
`tools/plugin-market/cc/src/lib/bridge.ts` 的 `LABELS` **一处都不动**。这是这条改动最省的地方，
也是"借 `llm.chat`"这个决定的直接回报。
