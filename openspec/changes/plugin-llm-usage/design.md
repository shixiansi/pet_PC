# design.md — plugin-llm-usage

> 一句话：token 的**汇合点**是 `pluginLLMChat` 里的 `send` 闭包（`app_plugins.go:2166`）——
> 它一次握着"发出去的那一串消息"和"回来的那个 `llm.Result`"，别处都只有一半。
>
> 本文件只做两件事：把每个要碰的标识符钉到文件与行号上；把要同步的每一处**连当前数字一起**写死。

## 1. 现状核对（实读，不是回忆）

| 标识符 | 位置 | 现状 |
| --- | --- | --- |
| `pluginMethods` | `app_plugins.go:364-484` | **43 条**（逐条数过）。`llm.chat` 在 `:451`，`{perm: plugins.PermLLMChat, run: pluginLLMChat}` |
| `pluginMethod` | `app_plugins.go:341-356` | `perm` / `also` / `run func(a *App, id string, payload []byte) (any, error)`；`perm` 必填 |
| 闸门顺序 | `dispatchPluginRequest` `app_plugins.go:566-587` | 已装已启用 → 方法存在 → 权限 → **才**碰 payload |
| `store.LLMUsage` | `internal/store/llm.go:199-212` | `Date` / `Requests` / `PromptTokens` / `CompletionTokens`，json tag 为 `date` / `requests` / `promptTokens` / `completionTokens` |
| `UsageDate` / `RolledTo` / `PlusAttempt` / `addTokens` | 同文件 `:215` / `:229` / `:243` / `:295` | 本地日 `"2006-01-02"`；翻页在**读的时候**发生；零值 token 不加 |
| `OverBudget` | 同文件 `:318-323` | `budget <= 0` 视为不限 |
| `LLMDailyBudget`（字段 / 默认 / 夹取） | `internal/store/store.go:164`、`internal/store/llm.go:53-55`、`:173` | 默认 200，范围 1–10000。**这是桌宠旧设置文件的字段，现在是迁移源**（`docs/llm-chat.md:152-160`） |
| 插件自己的次数上限 | `pluginChatConfig` `app_plugins.go:2326`（`cfg.LLMDailyBudget = num("llmDailyBudget")`） | 从**插件自己的 config** 读，零值 `Config` 起算（`:2292-2293`） |
| 次数上限的判定点 | `app_plugins.go:2079-2090` | 超限也计一次尝试（`:2083`）并返回中文错误 |
| 尝试计数点 | `app_plugins.go:2094` | `sent := usage.PlusAttempt(0, 0)`；**只有这一处** |
| 账本文件名 | `pluginUsageFile = "llm-usage"` `app_plugins.go:3034` | 宿主独占写；插件想读只能 `storage.get` |
| `readPluginUsage` / `writePluginUsage` | `app_plugins.go:3042` / `:3060` | 读时翻页；写是"同目录临时文件 + `Sync` + `Rename`" |
| `addReportedTokens` | `app_plugins.go:3087-3102` | 已有的"服务器报了才加"的规则，本次只加一个退化分支 |
| `res.Usage` 的来源 | `internal/llm/client.go:52`（字段）、`:342`（流式末帧）、`:421`（非流式）、`:497`（`stream_options.include_usage`） | 网关不认 `stream_options` 时会去掉重试（`:280-290`），**这条链上真可能整轮没有 usage** |
| `PermLLMChat` | `internal/plugins/capability.go:58` | `"llm.chat"`，门控 `llm.chat` / `llm.channels.get` / `llm.channels.set` |
| `AllowedPermissions` | `internal/plugins/capability.go:203` | 14 项，**本次不动** |
| `DataMethod` | `internal/plugins/calllog.go:28-48` | 数据面白名单；不认识的方法默认**动作面**（INFO，`:22-24`）。两个宿主的调用日志都用它：`app_plugins.go:527`、`tools/plugin-market/pluginhost.go:189` |
| 计数守卫 | `TestDocumentedMethodCountMatchesTheTable` `app_plugin_docs_test.go:20-75` | 期望值是 `len(pluginMethods)`，比六份文档的「N 个方法」与 `fake-host.mjs` 的 `METHODS` 条数 |
| `fake-host.mjs` 的 `METHODS` | `market/docs/plugin-testkit/fake-host.mjs:37-88` | **43 条**（38–87 行，一条一行） |
| `fake-host.mjs` 的 `okResult` | 同文件 `:117-164` | 没有 `llm.usage` 分支 → 落到 `default: { ok: true }` |

## 2. 计量发生在哪：in 与 out 的汇合点

一轮 `llm.chat` 的形状（`app_plugins.go`）：

```text
buildPluginChatMessages(...)  :2431   → msgs（system 提示词 + 历史 + 这一句）
msgs[last].Images = images    :2118-2121
send(round, tools)            :2166   ← 唯一同时握着 in 和 out 的地方
    res, err := a.llmClient().ChatWithTools(ctx, settings, round, tools, handlers)
    res.Usage != nil → addReportedTokens(&latest, ...)   :2175-2182
工具轮：msgs = append(msgs, assistant(ToolCalls))  :2251
        msgs = append(msgs, tool(结果))            :2265
finish(text)                  :2199
```

- **in = `round`**。它不是"用户这一句"：system 提示词（显式给的 `system`，或 `llm.BuildMessages`
  生成的，`internal/llm/prompt.go:43-47`）、历史、以及第 2 轮起追加的工具结果都在里面。
  所以按 `round` 估，一次 4 轮的工具对话会自然算成 4 次 in——这正好是它的贵。
- **out = `res.Text` + `res.Reasoning`**。思考链**也计费**，而且 `:2204-2211` 那条 WARN 记着一个
  真实形状："tokens out=1353、回复 0 字"。只数 `res.Text` 会把最贵的那种回答记成 0。
- 图片**不按字符串长度算**：`Message.Images`（`internal/llm/protocol.go:27`）里允许
  `data:image/…;base64,…`，一张 300 KB 的截图是 ~40 万字符。按字符估就是 10 万 token，
  而视觉模型实际只收它一千出头——一个视觉插件发一张图就会撞上自己的日上限。
  **一张图计一个扁平常数**（§3）。
- 工具调用的参数串 `ToolCall.Function.Arguments`（`protocol.go:145-151`）是 JSON**字符串**，
  会随 assistant 轮一起回到 `round` 里，按文本计；漏掉它会让工具对话的 in 偏小。

## 3. 估算公式（只在 `res.Usage == nil` 时用）

新文件 `internal/llm/estimate.go`，三个纯函数 + 五个常量：

| 常量 | 值 | 依据 |
| --- | --- | --- |
| `estimateTokensPerCJK` | `1.0` / 字 | 汉字与全角标点的真实值约 0.6 token/字，取 1.0 是**往上取** |
| `estimateCharsPerOther` | `3` 字符 / token | 英文散文真实约 4 字符/token，代码与 JSON 约 2.5–3；取 3 |
| `estimateTokensPerMessage` | `4` | 一条消息的角色、分隔符与结束符 |
| `estimateTokensPerImage` | `1024` | 一张图一个扁平值，理由见 §2 最后 |
| `estimateMinTokens` | `1` | 空串算 0，但一条消息至少算 overhead |

```go
// 分类：CJK / 全角 → 1 token/字；其余 → 每 3 字符 1 token（不足则进一）。
func EstimateTokens(text string) int
// 逐条消息：overhead + 文本 + 每张图 1024 + 每个工具调用（函数名 + 参数串）。
func EstimatePromptTokens(msgs []Message) int
// 回复 = 可见文本 + 思考链（思考也计费）。
func EstimateCompletionTokens(r Result) int
```

为什么不是真的 tokenizer：BPE 表**随模型变**（同一个端点的两个模型能差 20%），
而且它要一份随上游更新的词表、一个依赖、以及一套"这份词表覆盖哪些语言"的说法。
宿主里没有任何地方需要"精确到个位"，需要精确的地方服务器自己会报——报了就不用估算。

## 4. 误差，以及它为什么可接受

| 文本 | 估算 / 真实（方向） |
| --- | --- |
| 中文 | 约 1.6 倍（高估） |
| 英文散文 | 约 1.3 倍（高估） |
| 代码 / JSON | 约 1.0 倍（贴合） |
| 一张图 | 由 1024 与模型的分辨率档位决定，0.3–3 倍，**不可控** |

四条理由说明这个误差可以用：

1. **只在服务器什么都没报的时候用**。报了就是精确值，估算完全不参与。
2. 它要回答的问题是**排序**（"哪个插件在烧钱"）和**一把尺子**（上限），不是出账单。
3. 系数**统一偏高**。上限被低估会失效，被高估只会让用户早一点看到"今天用完了"——
   而后者是他自己设的数字。
4. 图片那一项是区间最大的，所以文档里必须说清"含图的请求，token 数只是量级"。

同一份计数器会**混着**精确值与估算值（有的轮报了、有的没报）。不为此加两个计数器：
代价是每轮多一次写文件，收益是一个"这个数可信吗"的布尔——它对**日聚合**没有意义
（哪天的哪几轮可信，一个 bool 说不出来）。要分辨，看日志：估算的那一行会写 `（估算）`（§7）。

验收要实测，不是断言：同一段 4000 字中文、一段 4000 字英文散文，各发一次给一个**会报 usage** 的渠道，
记下 `估算 / 服务器报值`，中文落在 1.0–1.8、英文散文落在 0.8–1.5 才算这条设计站得住。
实测值写在本节末尾（实现那条 task 的产出）。

**本轮实测（2026-11-03）**：对真渠道那一步**没跑**（这台机器上没有被配置过的、会报 usage 的端点，
而这条不该拿假数据去凑）。已跑的是"对基线"的那一半——`internal/llm/estimate_test.go` 拿估算量除以
本节自己给的基线（汉字 0.6 token/字、英文散文 4 字符/token、代码 3 字符/token），本机得到
**中文 1.67 倍 / 英文散文 1.33 倍 / 代码与 JSON 1.00 倍**，三条都落在上表与上面的验收区间里。
欠着的那一次（`tasks.md` E 段）验的是"基线本身准不准"，不是"系数选得对不对"。

## 5. 并发：现在没有锁，而且不只是"少算一次"

- 现在的"读-改-写"：`readPluginUsage(dir, now)` → 改 → `writePluginUsage(dir, u)`，
  **没有任何互斥**（`app_plugins.go:2078-2097`、`:2177-2181`）。两个在飞的 `llm.chat`
  同时读到 N，各写 N+1，**一次更新丢了**。
- `writePluginUsage` 的注释（`:3057-3059`）说的是"别的进程读不到**半个**文件"——那是
  `Rename` 的原子性，**不是不丢更新**。这两件事必须分开写，否则下一个人会以为这里已经安全。
- token 比次数更容易丢：次数在**发出前**写（窗口毫秒级），token 在**返回后**写，
  而返回要等整条流——几十秒。
- 方案：`App` 上新增 `usageMu sync.Mutex`（`app.go:47-48` 那片，`callsMu` 旁边），
  把三处写入（拒绝 `:2083`、发送 `:2094`、token `:2177`）与 `llm.usage` 的**读**都包进去。
- 为什么是一把全局锁而不是 per-plugin：写入频率是"每分钟几次"，争用为零；
  per-plugin 要一张 `map[string]*sync.Mutex` 加生命周期管理，为一个不存在的热点付费。
  同一把锁也顺带让"拒绝也要 +1"那条规则不会在并发下变成 0.5 次。
- 锁**不**解决的那件事：`llm.usage` 被读到的可能是"这次请求的 attempts 已经加了、token 还没到"。
  那是时间顺序，不是竞态——调用方看到的永远是它自己那几条已完成调用的账，除非它正好在
  一条请求在飞的时候问。文档里要写这一句，否则它会变成一个"数字偶尔少"的 bug 报告。

## 6. `llm.usage` 的形状

```jsonc
// payload {}（忽略；与 llm.channels.get / llm.providers 同形）
{
  "day": "2026-11-02",   // 本地日
  "requests": 12,        // 今天几条 llm.chat
  "tokensIn": 18421,
  "tokensOut": 3022,
  "budgetTokens": 0      // 0 = 不限
}
```

- **字段名只在 handler 的返回语句里转换一次**：`store.LLMUsage` 的 json tag 是
  `date`/`promptTokens`/`completionTokens`，而插件文档说的是 `day`/`tokensIn`/`tokensOut`
  （in/out 是日志里已经在用的词，`app_plugins.go:2176` 的 `tokens in=%d out=%d`）。
  转换点集中在一处，是为了**文件格式不出口**：`llm-usage` 是宿主私有的可重生成状态，
  直接把 `store.LLMUsage` 结构体丢给插件，等于把它升成契约，以后改文件就是破坏性变更。
- **不加 `budgetRequests`**：插件自己的 `config.get` 就读得到它声明的 `llmDailyBudget`。
  **不加 `estimated`**：见 §4 末段。
- **不加 `scope` 之类的字段**：这张账是"调用方自己"的，跨插件汇总不是这次的事
  （要回答"谁在烧钱"，宿主侧读各插件目录的 `llm-usage` 就够了，不需要新方法）。
- 归属：`{perm: plugins.PermLLMChat, run: pluginLLMUsage}`。`PermLLMChat` 已经门控
  `llm.chat` / `llm.channels.get` / `llm.channels.set`（`app_plugins.go:451,459,460`），
  这次是第四个头。
- 只读的副作用：不写文件（`readPluginUsage` 的翻页只在内存里发生），所以 00:00 之后第一次
  问会看到"今天 0 次"，而磁盘上还是昨天的记录——下一次 `llm.chat` 才会落盘。这是对的：
  翻页的规则只有一份（`RolledTo`）。

## 7. `llmDailyTokens` 放在哪

三个候选，选 **B**。

- **A：加进 `store.Config`，和 `LLMDailyBudget` 并排。** 看起来最对称，代价最贵：
  `store.Config` 的 `llm*` 字段已经降级成**迁移源**（`docs/llm-chat.md:152-160` 明写：
  "桌宠这边保留这些字段的唯一理由是它们是迁移的源"）。新字段没有迁移源、没有设置页，
  却会得到 `Normalise` 的夹取（`:173`）、默认值（`:277`）、`internal/store/llm_test.go`
  与那份配置表的**四个新副本**，全部为了一个只给插件用的数。
- **B：插件自己 manifest `config` 里的一个键，宿主在调用点读一次。** 选它。
  与 `llmDailyBudget` 的用户体验完全一致（同一个编辑器、同一个私有目录、同一个"上限是宿主的"），
  而实现只是**多读一个键**。
- **C：一个 `llm.setBudget` 方法。** 这会把"我愿意花多少"从用户手里交给插件
  （`app_plugins.go:2073-2076` 已经把这件事说死了：上限是宿主的，插件能改就不是上限）。

实现细节：

- 新增 `pluginConfigInt(man plugins.Manifest, values map[string]string, key string) int`，
  把 `pluginChatConfig` 里 `raw`+`num` 的两行规则（`app_plugins.go:2295-2307`）抽出来共用，
  `:2326` 改成调它。于是"有值用值、没值用 manifest 的 `default`"**仍然只有一处**——
  这条规则本身就是踩过的坑（`pluginChatConfig` 的注释 `:2287-2291`：不做这个回退，
  刚装好的插件会以"未配置"拒绝）。
- `pluginLLMChat` 里拿 `pluginConfigInt(*row.Manifest, cfgStore.Values(), "llmDailyTokens")`
  （`cfgStore.Values()` 的用法见 `:2464`）。
- 语义：`0` = 不限（与 `OverBudget` 的 `budget <= 0` 一致）；负数按 0；比较的是
  `PromptTokens + CompletionTokens`（服务商按这个收费）。
- **判定点只有一处**：紧跟次数上限之后、`sent := usage.PlusAttempt` 之前（`:2090`-`:2094` 之间）。
  被拒的那次**也计一次尝试**（照 `:2080-2083` 的规则）。
- **工具多轮不中途检查**，即使一轮就把上限撞穿。理由：中途拒绝意味着丢掉模型已经说出来的话，
  而"下一轮不再放行"和次数上限的性格一致（次数上限也是发之前判一次）。
  代价写进文档：**一次 `llm.chat` 可能越过 token 上限**，越多少由这一轮的历史决定。

## 8. 日志：`llm.usage` 属于数据面

- `DataMethod` 的判据（`internal/plugins/calllog.go:13-24`）：**数据面**是"插件读自己的东西"
  （`storage.*` / `share.*` / `config.*` / `memory.*`）；**动作面**是"人能看见或要花钱的事"；
  不认识的默认动作面。
- `llm.usage` 读的是宿主替它记的账，不花钱、不改状态、会被状态条按固定节奏拉一次 →
  **数据面**，加进 `calllog.go` 的 case 列表。漏了就是每 5 秒一行 INFO，
  而那正是 `:7-11` 记下的那次事故（9729 行 `share.get` 对 32 行 `llm.chat`）。
- 与 `PETPC_LOG_LEVEL` 的关系：默认 INFO 下 `llm.usage` **不出现在日志里**；
  `PETPC_LOG_LEVEL=debug` 才有 `llm.usage ok（payload 0 B → …）`。这不矛盾——
  诊断报告只收 INFO 及以上（`settingsapi_diagnostics.go` 的 `MinLevel`），
  而"今天用了多少"是数据面的事实，不是事件。
- 动作面那一半不动：`llm.chat` 的 INFO 行（`:2183-2185`）里已经有 `tokens in=… out=…`
  （`:2176`）。要回答"今天花了多少"看 `llm.usage`；要回答"这一轮为什么这么贵"看那一行。
- 新增一行给估算：`res.Usage == nil` 时把 `:2174` 的 `tokens := "没有报用量"` 改成
  `tokens = fmt.Sprintf("tokens in=%d out=%d（估算）", in, out)`。日志里必须能分出
  "这是服务器说的"还是"这是宿主猜的"——否则排查时会拿猜测当账单。

## 9. 检查清单：加一个能力方法要同步哪几处

**当前数字一律现数过**，不要照抄计划稿的 43。

| # | 位置 | 当前 | 改成 | 谁会发现漏了 |
| --- | --- | --- | --- | --- |
| 1 | `app_plugins.go` `pluginMethods`（`:364-484`） | **43 条** | 44 条 | `TestCapabilityTableIsComplete`、计数测试 |
| 2 | `market/docs/plugin-testkit/fake-host.mjs` `METHODS`（`:37-88`，38–87 行一条一行） | **43 条** | 44 条（`'llm.usage': { perm: 'llm.chat', where: 'pet' }`） | `TestDocumentedMethodCountMatchesTheTable`（`app_plugin_docs_test.go:66-74` 数条数） |
| 3 | 同文件 `okResult`（`:117-164`） | 无 `llm.usage` 分支，落到 `default: { ok: true }` | 一条示例 `{day, requests, tokensIn, tokensOut, budgetTokens}` | **没有人**。预览页会拿 `undefined` 去画状态条（`TypeError`）。这条最容易漏，因为测试看不见它 |
| 4 | `market/docs/plugin-dev.md` §6.6（`:818` 起） | 只讲次数上限（`:876-877`）；`:889` 的"每一轮都算一次请求上限"**与代码不符** | 加一小节（in/out 怎么算、`llm.usage`、`llmDailyTokens`、误差），并改正那句 | 人（评审）+ `plugin-check.mjs` 的 `command-unknown` |
| 5 | 同文件 §9 两宿主表（`:1496-1506`，`llm.chat` 在 `:1501`） | 没有 `llm.usage` | 加进 `llm.chat` 那一行 | `fake-host.mjs` 的 `where` 与这张表不一致时，插件会在 workshop 里被真宿主拒一次 |
| 6 | 同文件 §15 总表（`:1780-1825`） | **42 行**（方法表是 43——`plugins.services` 只在 §6.8 里讲，总表没有它） | 加一行 → 43 行 | 没有守卫。**别把总表行数当方法数**，也别把 42→43 当成"跟上了" |
| 7 | 「N 个方法」六处 → **44** | 6 处（5 个文件）：`README.md:564-565`（43 个能力方法）、`AGENTS.md:214`（43 个方法表）、`AGENTS.md:227`（43 个方法）、`docs/plugin-dev.md:13`（43 个方法）、`market/README.md:16`（43 个能力方法）、`market/docs/plugin-testkit/README.md:153`（43 个方法） | 44 | `TestDocumentedMethodCountMatchesTheTable`（正则 `(\d+) 个(?:能力)?方法`） |
| 8 | 第六份文档 `market/docs/plugin-dev.md` | **没有**「N 个方法」这句（`43` 只在 `:1753` 的端口例子里） | 无数字可改 | 它在测试的清单里但靠别的文档满足 `found > 0`；不要为了"六份都改"去编一句 |
| 9 | `TestDocumentedMethodCountMatchesTheTable`（`app_plugin_docs_test.go:20-75`） | 期望值是 `len(pluginMethods)`，**没有 43 字面量** | **不改** | 它就是守卫本身 |
| 10 | `internal/plugins/calllog.go` `DataMethod`（`:28-48`） | 不含 `llm.usage` | 加进数据面 case | 没有守卫（默认 INFO）；表现是日志变噪音 |
| 11 | `.dev/check-frontend.cjs` | 有硬编码清单，但那是**权限词表**与 `App` 的 Wails 导出面（`:2041-2076` 比对 `export function`） | **不改**：能力方法不经过 Wails 绑定，且没有新权限 | 不需要改，但要跑一遍 |
| 12 | `tools/plugin-market/`（独立 module） | `market_test.go:711` 硬编码 pet-only 名单 | 建议加 `llm.usage`（一行） | 根 `go vet ./...` **不下潜**，只有它自己的 `go test` 会跑 |
| 13 | `README.md` 的权限表与标题 | 14 项 | **不动** | 没有新权限 |

> **数 `METHODS` 的坑（本次写文档时踩到的）**：`fake-host.mjs` 里**还有一张表**——`PERMISSIONS`
> （`:19` 起），它的键也是 2 空格缩进、单引号、带点（`'llm.chat'`、`'net.fetch'`、`'pet.surface'`…）。
> 所以"匹配 `^\s{2}'[a-z]+\.[a-z.]+':`"这种数法会把两张表一起数进去，得到 **53** 而不是 43。
> 要数就只数 `export const METHODS = {` 到它那一行 `};` 之间的部分（`tasks.md` C1 给了可以直接跑的写法），
> 或者干脆相信 `TestDocumentedMethodCountMatchesTheTable`——它用的就是这个范围。

另外两个"顺手但不进本 change 验收"的：`docs/plugin-roadmap.md:4` 与 `:17` 的「43 个方法」，
以及 §3.1 M7 那条"先做这个"的标记。它们不在计数测试的六份里，可以留到这一轮结束时一起收。

## 10. 测试

Go（新增，除非注明）：

- `internal/llm/estimate_test.go`
  - 中文 / 英文 / 代码三段按 §4 的区间收口；
  - 空串 → 0；一条只有空格的消息 → 只有 overhead；
  - 一条带 1 张 `data:image/…` 的消息 → overhead + 1024，**与那个 base64 串的长度无关**；
  - `ToolCall.Function.Arguments` 计入 in。
- `app_plugins_test.go`
  - `TestLLMChatEstimatesWhenTheServerReportsNoUsage`：SSE 不含 `usage` → `pluginUsageOnDisk`（`:2015`）
    的 `PromptTokens`/`CompletionTokens` 都 > 0；
  - 既有 `TestLLMChatStreamsToThePlugin`（`:2216`，断言 3/2）就是"服务器报了就用它"，**不改**；
  - `TestLLMUsageReportsTodaysCounters`：打两次 → `requests: 2`，`day` 是今天，
    `tokensIn/Out` 与 `pluginUsageOnDisk` 一致；
  - `TestLLMUsageNeedsTheChatPermission`：没授予 `llm.chat` → 拒绝，错误点名 `llm.chat`；
  - `TestLLMDailyTokensStopsTheNextCall`：`llmDailyTokens` 设成 10 + 一次正常请求 → 第二次被拒、
    文案含数字；`0` 时永不拒绝；
  - `TestLLMChatUsageSurvivesConcurrentCalls`：两个 goroutine 各打一次 → `Requests == 2`
    （不加锁时这条会红；`-race` 能跑就一起跑）。
- `TestCapabilityTableIsComplete` 与 `TestAllowedPermissionsIsTheFourteen` **不改**。

Node：

- `node .dev\check-frontend.cjs`：改的是 Go 与文档，期望仍全绿（142 条）。
- 真机（`tasks.md` 最后一条）：装一个声明了 `llm.chat` + `llmDailyBudget` + `llmDailyTokens`
  的插件，问三句，`llm.usage` 的 `requests` 与日志里 `llm.chat 完成` 的行数一致，
  `tokensIn/Out` 与日志里 `tokens in=… out=…` 的累加一致（估算轮次会标「估算」）。

## 11. 明确不做

- **不做真的 tokenizer**（§3）。也不把"令牌表"塞进插件仓的 kit。
- **不用估算覆盖服务器报的值**：精确的永远优先。
- **不拆 in 的构成**（历史 / 图片 / 工具各占多少）。一个 `tokensIn` 够回答"谁在烧钱"，
  拆开是给计费系统用的。
- **不改 `Requests` 的语义**（proposal 末尾那段）：改它就是在动预算，而且 §6.6 那句话
  本来就该改文档而不是改代码。
- **不在控制中心服务 `llm.usage`**：那里没有模型调用，账本是另一个进程写的。
  将来那一页要显示"这一天用了多少"，读同一个 `llm-usage` 文件即可，不需要新方法，
  也不需要把它变成契约。
- **不把 `llm-usage` 文件升成契约**：wire 名的转换只发生在 §6 那一个返回语句里。
- **不给 token 上限做"预估这一轮要花多少再决定发不发"**：那要先估算、再用估算拒绝，
  等于用一个大概的数去关掉一次用户已经点的请求，而 §7 的"下一轮不再放行"已经把上限兜住了。
