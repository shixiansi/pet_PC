# 插件补充方案（附 2.8 亿 token 预算）

> **状态：计划稿，不是契约。** 唯一契约仍然是插件仓里的
> [`plugin-dev.md`](../market/docs/plugin-dev.md)（14 项权限 / 43 个方法 / manifest 校验）。
> 这份文档回答三个问题：**主体程序还缺什么机制、市场还缺什么插件、2.8 亿 token 怎么花才不白花。**
> 基线：当前工作树 —— 市场条目 36 条、插件目录 37 个（`imgbed` 未上架）、14 权限 / 43 方法、
> 51 个前端测试文件。
>
> **怎么读**：§1 与 §2 是诊断（哪里空了）；§3 是补什么（按批次，含每项的准入条件）；
> §4 是 2.8 亿 token 的两本账；§5 是执行协议；§7 是 72 小时内能落地的五件事。
> **只有一晚 → 直接跳到 §8**（一晚冲刺版：12 个插件、6 条泳道、8 小时时刻表）。

---

## 0. 一页结论

1. **宿主的机制不弱，但"AI 化"的接口只开了一条缝。** 43 个方法里，模型能调用的工具**只有 3 个**
   （`pet_say` / `memory_read` / `memory_append`），而市场里已经有 **25 个插件声明了 `serves`**、
   合计 **49 个**能力方法。这些能力对人是按钮，对模型是不可见的 —— 这是整个生态最大的单点浪费。
2. **市场是"阅读器生态"，不是"执行器生态"。** 36 个插件里能**改变外部世界**的只有 4 个
   （qq / onebot / qqbot / live-danmaku），其余全在"读进来 → 说一句话 → 存起来"。
   新增插件不应再堆第 37 个阅读器。
3. **三个场景域是空的**：日历/日程（todo 只是列表）、健康（久坐/喝水/护眼/睡眠全无）、
   **形象**（`body: true` 的插件 0 个，主题只有 2 套）。其中形象那条最刺眼：宿主为它专门做了
   `#pet-body` 整格透明帧、`pointer-events: none`、不许 `z-index` 的一整套约束，**至今没有一个
   插件用过**。
4. **要跑 2.8 亿 token，卡点不在插件数，在三个宿主机制**：`llm.chat` 单次只有 4000 字、
   没有 embedding、**没有 token 计量**（宿主只数"请求次数"，默认 200、上限 10000）。
   不补这三样，2.8 亿 token 只能靠"多问几万次"，既慢又测不出来。
5. **建议的分配**：建设账 280M ≈ 7 项机制 + 24 个新插件 + 36 个存量深度化 + 联调回归 + 19.7% 缓冲；
   若你指的是**运行吞吐**，同一份预算下 5 个大户插件 + 分片批处理 ≈ 4~5 天跑完 2.8 亿。

---

## 1. 主体程序：能给插件什么、不能给什么

### 1.1 机制清单

| 层 | 现状 | 位置 |
| --- | --- | --- |
| 权限 | **14 项**，顺序即安装对话框绘制顺序 | [`capability.go`](../internal/plugins/capability.go) |
| 能力方法 | **43 个**，唯一一张表 | [`app_plugins.go:365`（`pluginMethods`）](../app_plugins.go#L365) |
| 闸门顺序 | 已装已启用 → 方法存在 → 权限已授予 → **才碰参数** | [`dispatch.go`](../internal/plugins/dispatch.go) |
| 界面落点 | 浮层面（≤8 块矩形）/ 常驻帧 / 宠物本体（`body`）/ 右键菜单格（`panelSlot`） | `manifest.go`、`app.go` 的 region switch |
| 插件互通 | `plugin.share`（牌盘，64 KiB×32 键）+ `plugin.call`（一问一答，64 KiB、3 跳、30 s） | [`call.go`](../internal/plugins/call.go)、`share.go` |
| 辅助脚本 | `sidecar: {script}` 由宿主托管（包内相对路径 + `assets` + `process` + `background` 四条校验） | [`app_plugin_sidecarlog.go`](../app_plugin_sidecarlog.go) |
| 模型 | `llm.chat`（流式 + 借配置 + 渠道表） | [`app_llm.go`](../app_llm.go)、`app_plugin_llm_channels*.go` |
| 变化推送 | `config.changed` / `storage.changed` / `share.changed`，两个宿主都推 | [`watch.go`](../internal/plugins/watch.go) |

### 1.2 真正决定"写不写得出来"的硬上限

这些数字比权限表更能决定一个插件能不能做。**新插件的设计从这张表开始读，而不是从权限表。**

| 能力 | 上限 | 卡住了什么 |
| --- | --- | --- |
| `llm.chat` 单条 `text` | **4000 字符** | 长文精读、整份 PDF/字幕喂进去、全文件代码审查 |
| `llm.chat` `history` | 32 条 / 合计 12000 字符 | 长对话、多轮 agent、跨文档推理 |
| `llm.chat` 工具 | **3 个名字**（宿主目录固定），最多 4 轮 | 插件自己的 `serves` 无法成为模型工具（见 M3） |
| `llm.chat` 预算 | **按"请求次数"**计（默认 200 / 上限 10000 次/日） | **没有 token 计量**（见 M7），2.8 亿无从度量 |
| 向量 / embedding | **不存在** | 知识库只能 TF-IDF（`knowledge-base` 现状）、记忆检索靠关键词 |
| `storage.set` 单值 | 1 MiB（键深 ≤4、键长 ≤200） | 大 JSON 要分片写 |
| `share` 值 | 64 KiB / 32 键 | 传列表要压缩或分页 |
| `plugin.call` | 64 KiB、3 跳、30 s 超时 | 长任务必须"接单 + 轮询/推送"，不能一次答完 |
| 浮层面 | ≤8 块矩形，坐标相对宠物左上角 | 复杂表格/看板塞不进去 → 去插件页 |
| `assets` | ≤32 项 | 素材型插件（音景/形象）要算法生成或外链 |
| 音频 | 单条 ≤16 MiB | 长音频要切片循环 |

### 1.3 三个结构性约束（这是新增插件的天花板）

**a) 模型只看得见 3 个工具。** `pluginToolCatalog()` 是一张写死的表
（[`app_plugin_tools.go:83`](../app_plugin_tools.go#L83)），注释里写明了理由：工具的 schema 必须住在宿主里，
否则"插件可以把它描述成另一件事"。这个理由成立，但它把"插件能被告知给模型"这件事整个关掉了：
`flashcards/add_cards`、`expense-tracker/chat_on_message`、`weather-env/get_weather`、
`diange/song`、`privacy-guard/scan` 全都已经存在并且可被 `plugin.call`，**模型却一个都调不到**。
→ 这是 M3。

**b) 插件之间"只能读别人，不能写别人"。** 49 个 `serves` 里绝大多数是查询；
真正带写入或触发动作的只有六个（`flashcards/add_cards`、`pet-evolution/grant_exp`、
`diary-writer/write_now`、`expense-tracker/chat_on_message`、`slang/learn`、`web-scraper/scrape_one`），
而 `todo` 只 serve `stats` —— 没有 `add_task`。于是"把会议纪要里的待办加进列表"、
"问一句天气然后建个提醒"这类**最常见的助手动作，今天做不到**。
这不是宿主机制问题（`plugin.call` 足够），而是**插件侧的 `serves` 面太窄** —— 便宜且立刻能改。

**c) 没有 embedding、没有长输入、没有 token 计量。** 三者一起把"大规模 token 任务"挡在门外：
知识库是关键词检索、单次调用喂不进一份文档、花掉多少 token 没有任何一处记录。
→ 这是 M1 / M2 / M7。

---

## 2. 市场盘点

### 2.1 覆盖矩阵（36 条市场条目 / 37 个插件目录）

`●●●` 厚（多个成熟插件）｜`●●` 中｜`●` 薄（1 个或功能半）｜`○` 空

| 场景域 | 覆盖 | 成员 |
| --- | --- | --- |
| 时间与专注 | `●●` | todo、pomodoro、agent-worker（队列） |
| 日历与日程 | `○` | **无**（todo 是列表，不是时间轴；没有 ICS/会议/生日/农历） |
| 信息摄入 | `●●●` | rss-reader、websearch、web-scraper、bilibili、live-danmaku、email-digest、weather-env |
| 知识沉淀 | `●●` | knowledge-base、flashcards、slang、moegirl、state-snapshot |
| 记忆与人格 | `●●` | memcore、pet-evolution、diary-writer |
| 沟通 | `●●●` 但**单一** | qq、onebot、qqbot、live-danmaku（全是 QQ / B 站；无 Telegram/Discord/Slack/邮件发信） |
| 财务 | `●` | expense-tracker |
| 健康 | `○` | **无**（久坐、喝水、护眼、睡眠、屏幕时间全空） |
| 系统与环境感知 | `●●` | pet-sense、media-sync、screen-vision、privacy-guard、desktop-wallpaper |
| 创作与开发 | `●` | ast-coder、agent-worker、clipask |
| 形象 / 皮肤 | `○` | **`body` 插件 0 个**；主题 2 套（mint / sunset） |
| 娱乐与养成 | `●` | game-center、pet-evolution |
| 工具生态（协议/MCP） | `●` | 只有 3 个 IM 适配器；无 MCP、无本地 ASR、无唤醒词 |

### 2.2 四个观察

1. **读多写少。** 能对外**执行动作**的插件只有 4 个（qq / onebot / qqbot / live-danmaku）；
   49 个 `serves` 里带写入的只有六个（见 §1.3b）。**桌宠今天更像"看板"，不像"助理"。**
2. **5 个插件带 sidecar 读本机**（clipask / media-sync / pet-sense / privacy-guard /
   screen-vision），另有 qq 用 `process.launch` 起协议端。
   其中 clipask 与 privacy-guard **各自开一个剪贴板轮询器** —— 同一块剪贴板被两个进程每秒看一次。
   这不是插件写错了，是宿主没有原生门（M5）。
3. **宿主为"形象"做的机制没人用。** `body: true` 的插件 0 个，主题 2 套。
   换句话说：**这条产品线只有代码，没有货。**
4. **生态里的能力发现已经跑通了**（`plugins.services` + `serves` + `callTargets`，
   memcore 按能力名找搜索/词典/点歌），但**只服务于"插件找插件"**，没有服务于"模型找插件"。

### 2.3 盘点中顺手查到的五个具体问题

| # | 问题 | 证据 | 处理 |
| --- | --- | --- | --- |
| 1 | **CI 的 gofmt 门禁现在是红的** | 按 `go.mod` 的 go1.22.12 实测，**HEAD 里 22 个 `.go` 文件**会被 `gofmt` 改写（`app.go`、`app_plugins.go`、`internal/plugins/manifest.go`、`tools/plugin-market/settingsapi.go` …），而 [`ci.yml`](../.github/workflows/ci.yml) 的 gofmt 步骤会 `throw` | `gofmt -w` 一次（纯机械 diff），0.5 天 |
| 2 | **`imgbed` 已写好、有测试，但没上架** | 目录 + `tests/plugin-imgbed.test.js` 都在，`market/index.json` 里没有它 | 补条目 + 打包，0.2 天 |
| 3 | **文档与代码漂移**：`AGENTS.md` §3.9 仍把 `storage.pressure` 写成"活着（只有桌宠发）" | 代码里 2026-11-01 已删（[`app_plugins.go:1053`](../app_plugins.go#L1053)、`:1749`），`plugin-dev.md` §7.3 已标"已退休" | 改 AGENTS 那一句 |
| 4 | **`ui.css` 有 28 份逐字副本**（彼此一致，无校验） | 28 个插件各带一份 22.8 KB；`plugin-check.mjs` 只校验"引用了就必须进 assets"，不校验"与 kit 同源" | 加一条哈希一致性检查（或打包期注入） |
| 5 | 本机工作树有 **29 个文件混入 CRLF**（HEAD 里是纯 LF） | `gofmt -l .` 的噪声主要来自这里 | 提交前 `gofmt -w`；别用会改写行尾的编辑器 |

> 第 1 条是这一轮唯一"现在就是坏"的：它会挡住所有后续 PR。所以它排在 §7 的第 1 位。

---

## 3. 需要补充什么

### 3.1 宿主机制（M1–M7）：按"解锁多少插件"排序

每条都写清：**为什么必须有**、**最小实现**、**要同步改什么**（本仓库最贵的不是写代码，是漏改）。

#### M7 `llm.usage`：token 计量与配额（**先做这个**）
- **为什么**：现在宿主只数"请求次数"（`usage.Requests` / `LLMDailyBudget`），一次 200 字的提问与
  一次 12000 字历史的提问记作同样的 1。**2.8 亿 token 的预算在今天的程序里无法观测**，
  也无法回答"哪个插件在烧钱"。
- **最小实现**：`llm.chat` 完成时把 `in/out` 估算（按字符数与语言权重，或取渠道响应的 `usage`）
  累加进 `usage.Tokens`；新方法 `llm.usage {}` → `{day, requests, tokensIn, tokensOut, budgetTokens}`；
  `llmDailyBudget` 旁加一个 `llmDailyTokens`（0 = 不限）。
- **要同步**：`pluginMethods` +1 方法、`plugin-dev.md` §6.6 与方法总表、
  `fake-host.mjs` 的 `METHODS`、`TestDocumentedMethodCountMatchesTheTable` 的数字（43 → 44）、
  `check-frontend.cjs` 若有硬编码清单。**不新增权限**（借 `llm.chat`）。
- **代价**：3.5M token。

#### M3 插件声明的模型工具（`manifest.tools`）
- **为什么**：把 25 个插件的 49 个 `serves` 变成模型能看见的手。做法不是"让插件提供 schema"，
  而是**宿主保留 schema 的权威**：manifest 只声明 `tools: ["add_task"]`，宿主用
  `<插件 id>/<方法>` 去 `plugin.call`，schema 从**宿主内置的一张受控模板表**里取
  （`add_task` / `query` / `convert` 等少数几种形状）。这样第 1.3(a) 条的安全理由不变，
  而插件不必为每个工具改宿主。
- **要同步**：manifest 校验（新字段 + 安装对话框展示）、`pluginMethods` 无变化、
  `app_plugin_tools.go` 的目录改成"内置 3 个 + 声明式 N 个"、`plugin-dev.md` 新增一节、
  `plugin-check.mjs` 加校验、`fake-host.mjs` 同步。
- **代价**：6M（这是本轮最贵也最值的一条）。

#### M2 `llm.chunk`：无状态分片调用（批量吞吐的前提）
- **为什么**：4000 字 + 12k 历史的上限决定了一次调用只能处理一份"短文档"。
  批量任务的正确形状不是"把上限调大"（那会让单次失败更贵），而是
  **一个明确的批量原语**：`llm.chunk {text[], system, concurrency}` → 逐片返回，**不带历史**、
  **按片计量**、**可中断可续跑**（片级幂等，失败只重跑那一片）。
- **要同步**：新方法 + `plugin-dev.md` + `fake-host.mjs` + 计数（→ 45）+ 一份
  "什么时候用 chunk、什么时候用 chat"的判据（否则所有人都会拿 chunk 当 chat）。
- **代价**：5M。

#### M1 `llm.embed` + 本地向量索引
- **为什么**：`knowledge-base` 是 TF-IDF（同义词查不到）、`memcore` 的记忆检索是关键词。
  这是"知识库/记忆"这条线的天花板。宿主只需提供 **embedding 调用 + 一个进程内的向量文件格式**，
  检索（余弦、分片）留在插件里，保持"规则住插件、密钥住宿主"的既有分工。
- **要同步**：新方法（借 `llm.chat` 权限）+ 渠道表加 `embeddingModel` 字段 + 文档 + 计数（→ 46）。
- **代价**：5M。

#### M4 `schedule.*`：墙钟定时（带补跑）
- **为什么**：每个插件都在用 `setInterval` 自己数时间（"每 5 分钟""每 2 小时""每天 23:00"），
  而机器休眠、插件更新重建帧、用户关掉浮层面都会让它们失准。墙钟任务应该是宿主的能力：
  `schedule.add {at|every, id}` → 到点以 `event` 推 `schedule.fired`，**错过的那次在恢复后补一次**。
- **要同步**：新方法（借 `events` 权限）+ 事件名进 `SubscribableEvents` + 文档 + 计数（→ 47）。
- **代价**：4M。

#### M5 `clipboard.read` / `clipboard.write`
- **为什么**：现在两个插件各自开一个 PowerShell 轮询剪贴板（§2.2 第 2 条）。这是**同一件事的两份实现**，
  而且都是 1 秒轮询（本项目已经定为反模式）。
- **最小实现**：宿主用 `AddClipboardFormatListener` 收窗口消息，只在变化时推 `clipboard.changed`；
  `clipboard.read {}` 返回文本（≤64 KiB）；`clipboard.write {text}`。
  需要一项**新权限** `clipboard`（这是 14 → 15 的第一次扩张，走 AGENTS §3.1 的七处清单 + README 表格与标题）。
  方法计数 47 → 49（`clipboard.read` / `clipboard.write`）。
- **代价**：6M（含权限扩张的全套同步）。

#### M6 系统通知（toast）
- **为什么**：`pet.bubble` 只在桌宠旁边出现。用户不在电脑前时，"该吃药了""会要开始了"没有任何出路。
  这不是新界面，是**同一条消息的第二个信道**。
- **最小实现**：`notify.show {title, body}`（借 `pet.bubble` 权限，或独立 `notify`），
  Windows 侧用 Shell_NotifyIcon 的气泡或 Toast XML；失败静默退化为气泡。
- **代价**：3.5M（方法计数 49 → 50）。

> **不建议**在这一轮做：`pet.animate`（宠物动作/表情）。它比 `body` 更诱人，但它要求宿主定义
> 骨骼/帧协议 —— 那是"重画一只宠物"，不是"加一个方法"。先用 `body` 插件把形象这条线跑通（见 P2-5）。

### 3.2 第一批（P0，8 个）：今天就能写，且填的是空格

> 共同要求：`plugin-check.mjs` 全绿 + `preview.mjs --check` 不越界 + 一条 vitest 用例
> （含"宿主拒绝一切"那一遍）+ 版本号 +1 + `index.json` 条目。逐项验收见 §5。

| # | 插件 id（`com.shixiansi.plugin-`） | 一句话 | 权限 | `serves` | 宿主前置 |
| --- | --- | --- | --- | --- | --- |
| P0-1 | `calendar` | 订阅 ICS（Google/Outlook/飞书公开日历），会前 10 分钟说一句，问"我今天有什么"能答 | net.fetch, storage, plugin.config, pet.bubble, pet.surface, events | `today` `next` `agenda` | 无（建议 M4） |
| P0-2 | `habits` | 习惯打卡 + 连续天数 + 头顶徽标；断签重置有明确规则 | storage, plugin.config, pet.surface, pet.bubble, plugin.share | `streak` `today` | 无 |
| P0-3 | `clipboard-history` | 剪贴板历史（环形 500 条、按 privacy-guard 结果过滤、可回填） | process, storage, plugin.config, pet.surface, plugin.call | `recent` `pin` | 有 sidecar 即可；M5 后改原生 |
| P0-4 | `focus-sound` | 专注音景（雨/咖啡/白噪，程序合成 + 短文件循环），与番茄钟联动 | audio, storage, plugin.config, plugin.call, plugin.share | `play` `stop` | 无 |
| P0-5 | `weekly-review` | 周日 20:00 汇总待办/专注/日记 → ≤300 字周报，可复制可存档 | plugin.call, storage, plugin.config, pet.bubble, events | `report` `latest` | 无 |
| P0-6 | `meeting-notes` | 盯一个目录里的新 `.md/.srt/.txt` → 摘要 + 抽待办 → 写进待办 | process, storage, plugin.config, plugin.call, pet.bubble, pet.surface | `digest` | **依赖 todo 增加 `add_task`** |
| P0-7 | `table-lab` | 用户拖入 CSV（`FileReader`，**不读磁盘路径**）→ 类型推断、分组统计、SVG 图、模型解读 | storage, plugin.config, pet.bubble, plugin.call | `analyze` | 无 |
| P0-8 | `notes-sync` | 指向 Obsidian/任意 Markdown 目录（sidecar 只读），切片建索引，面板可搜、命中就喂模型 | process, storage, plugin.config, plugin.call | `search` `read` | 无（M1 后升级为向量） |

三条设计约束（P0 全部适用）：

- **不许新增权限**。这 8 个都能用现有 14 项拼出来；任何"必须加权限才能做"的想法先回到 §3.1。
- **提醒走宿主已有的路**：气泡 + 浮层面 + 徽标，不自己弹窗、不自己开窗口。
- **不写 1 秒轮询**：用 `share.changed` / `storage.changed` + 15 秒兜底（AGENTS §3.9 第 4 条）。

### 3.3 第二批（P1，9 个）：把"能力"接进生态

| # | 插件 | 做什么 | 关键权限/方法 | 前置 | 主要风险 |
| --- | --- | --- | --- | --- | --- |
| P1-1 | `mcp` | **MCP 桥**：sidecar 把 MCP server 的 stdio 转成本机 HTTP，插件用 `net.fetch`/`net.ws` 问它；`serves: tools/call` 让其他插件（以及 M3 之后的模型）用上任意 MCP 工具 | process(sidecar), net.fetch, net.ws, storage, plugin.config | M3 才有"模型可用" | 子进程生命周期、端口占用、工具名冲突 |
| P1-2 | `asr-local` | 本地转写（whisper.cpp sidecar 队列）：把音频文件转成 `.srt` + 纯文本，交 `llm.chunk` 做纪要 | process, storage, plugin.config, plugin.call, audio | M2 | 模型体积/首次下载、CPU 占用 |
| P1-3 | `wake-word` | 唤醒词（sidecar 常听，本地小模型）→ 触发 `voice-call` 或提问 | process, plugin.call, pet.badge, storage | 无 | 麦克风常开的隐私与耗电（必须默认关 + 明确指示灯） |
| P1-4 | `telegram` | 薄适配器（同 onebot 的形状：只收发，人设记忆问 memcore） | net.fetch, plugin.call, storage, plugin.config | 无 | 长轮询 vs webhook 的取舍 |
| P1-5 | `discord` | 同上（Gateway WS） | net.ws, plugin.call, storage | 无 | 网关重连/心跳 |
| P1-6 | `slack` | 同上（Socket Mode） | net.ws, plugin.call, storage | 无 | 企业授权流程 |
| P1-7 | `mail-reply` | 读未读 → 生成**草稿** → 用户点头才发（SMTP），发送前必过一次 `privacy-guard/scan` | net.fetch, net.credentials, llm.chat, plugin.call, storage | 无 | 误发/凭据；必须默认"只出草稿" |
| P1-8 | `podcast` | 订阅播客 → 下载音频 → 转写 → 摘要成卡片，进收件箱 | net.fetch, storage, plugin.call, audio, pet.bubble | M2 + P1-2 | 版权与存储体积（需自动清理） |
| P1-9 | `price-watch` | 盯网页变化（价格/库存/公告）→ 变化才说一句 | net.fetch, storage, plugin.config, pet.bubble | M4 更好 | 反爬、频率、误报 |

### 3.4 第三批（P2，7 个）：长线，其中一个是补机制欠账

| # | 插件 | 做什么 | 前置/备注 |
| --- | --- | --- | --- |
| P2-1 | `github-notify` | PR/Issue/CI 通知 + 摘要（`net.fetch` + token） | 无；注意速率限制 |
| P2-2 | `finance-board` | 股票/基金/加密看板（浮层面 sparkline + 每日收盘一句） | 无；行情源要可换 |
| P2-3 | `screen-time` | 屏幕使用时间（sidecar 采前台窗口标题，按应用聚合，日报） | 隐私：只存应用名与时长，不存标题 |
| P2-4 | `health-reminder` | 久坐/喝水/护眼（墙钟 + 全屏检测，全屏时静默） | M4 + M6 |
| P2-5 | **`pet-look`** | **第一个 `body: true` 插件**：整格透明画布 + 呼吸/眨眼/朝向 + 3 套外观；养成（`pet-evolution`）通过 `plugin.share` 驱动 | 无（纯宿主既有机制）；这是"形象"这条线的验收件 |
| P2-6 | `theme-studio` | 可视化改 token → 导出 `theme.json`（主题商店从 2 套到可自助） | 无 |
| P2-7 | `convert-kit` | 单位/汇率/时区/进制换算（`serves: convert`，是 M3 之后模型最爱用的工具之一） | 无；零权限也能跑（内置表） |

### 3.5 存量 36 个：不是重写，是"补一条筋"

每个约 **1.5M token**（读现状 + 一处结构性改进 + 用例 + 版本）。挑最值的 14 个先做：

| 插件 | 补什么 | 为什么值得 |
| --- | --- | --- |
| `todo` | **`serves: add_task / toggle / list`** + 自然语言录入 + 重复任务（RRULE 子集） | 补上 §1.3(b)：不加这三个，"助手帮你记一件事"永远做不到 |
| `pomodoro` | 周统计 + 与待办任务绑定（`share` 里带 taskId） | 让"专注时长"能按事归因 |
| `memcore` | 记忆检索换向量（M1）+ 工具面扩展（M3）+ `compact` 策略可配 | 它是生态的"大脑"，升级它等于升级所有接入端 |
| `knowledge-base` | embedding 索引 + 引用片段 + 增量重建 | 从"关键词搜索"到"答得上" |
| `chat` | 会话管理/导出、图片（data URI 已有）、工具面板 | 用户每天用的那个界面 |
| `qq` | 拆 `lib/`（9063 行、197 KB 测试是维护债） | 后续任何改动都更便宜 |
| `web-scraper` | 增量抓取 + 去重 + 文章库检索 | 已有 2835 行，最接近"个人剪藏" |
| `expense-tracker` | 月度预算 + CSV 导入 + 月报 | 财务域只有它一个 |
| `flashcards` | SM-2 → FSRS + `serves: due_cards` | 复习质量 |
| `voice` | 音色/语速/SSML + 与 `focus-sound` 的互斥规则 | 声音是桌宠的性格 |
| `bilibili` | 关注流 + 稍后再看 + 与 `asr-local` 打通 | 现在只能"按分区刷" |
| `privacy-guard` + `clipask` | 合并成一个剪贴板监听（M5 之后），告警走 `notify` | 去掉一份重复轮询 |
| `agent-worker` | 真正拆任务（M2/M3）+ 进度浮层面 | 现在是"5 分钟看一次队列" |
| `state-snapshot` | 每日自动快照 + 差异对比 + 过期清理 | 记忆的保险丝 |

### 3.6 明确不补（YAGNI，AGENTS §8 的延伸）

- **CUA / 让模型直接操作电脑**：与本项目定位冲突，风险不可控。
- **密码管理器**：密钥只能单向进宿主（DPAPI），插件拿不到明文；做"密码库"等于要开一个反向的口子。
- **微信协议接入**：协议端不稳定 + 账号风险，收益不抵。
- **跨平台**：Windows-only 是定位，不是债。
- **再来一个"通用聊天"插件**：`chat` 已经占住位置，第 37 个阅读器没有意义。

---

## 4. 2.8 亿 token 的账

### 4.1 口径先定死

| 项 | 换算（用于估算，不追求精确） |
| --- | --- |
| 中文 | 1 token ≈ 1.2~1.5 汉字 |
| 代码 | 1 token ≈ 3~3.5 字符 |
| 一轮 agent 的**实耗** | ≈ 单轮上下文 × 轮数（上下文每轮重放，所以"读一遍大文件"会被记很多次） |
| 2.8 亿 token 的量级感 | ≈ 3.6 亿汉字 ≈ 6 万篇 6000 字文章 ≈ 2 千万行代码 |

**为什么必须记账**：宿主只数请求次数（默认 200 次/日、上限 10000），一次 200 字的问答与一次
12000 字历史的问答各记 1 次。**没有 M7 之前，2.8 亿这个数字在程序里没有任何一处能被观测到。**

### 4.2 账本 A：建设预算（把 2.8 亿花在"造插件"上）

单位成本（中位值，按 §3 的验收标准：check + preview + vitest + 文档 + 版本）：

| 阶段 | 内容 | token |
| --- | --- | --- |
| 摸底 | 读 `plugin-dev.md`（110 KB ≈ 35k）+ 3 个同类插件 + 相关宿主代码 | 0.6M |
| 设计 | openspec proposal/design + manifest 草案 + 界面骨架 | 0.3M |
| 首版 | 1.2~2k 行 JS/HTML/CSS（市场现状的中位数是 ~1.5k 行） | 0.8M |
| **自检循环** | `plugin-check` + `preview --shot` 6~10 轮（**这一项最大**） | 1.6M |
| 测试 | vitest + 假宿主往返（含"拒绝一切"那一遍） | 0.5M |
| 评审 | 对抗式复审（"这个插件在什么情况下会静默失效"） | 0.5M |
| 文档/发布 | 手册条目、`index.json`、版本、打包、sample 一致性 | 0.3M |
| **合计** | | **4.6M ≈ 5M** |

| 预算项 | 单价 | 数量 | 小计 |
| --- | --- | --- | --- |
| M1–M7 宿主机制（含契约文档与计数同步） | 4.5M | 7 | 31.5M |
| P0 新插件 | 5.0M | 8 | 40.0M |
| P1 新插件 | 5.0M | 9 | 45.0M |
| P2 新插件 | 4.0M | 7 | 28.0M |
| 存量 36 个深度化 | 1.5M | 36 | 54.0M |
| 联调、回归、发布（60 项 × 0.35M） | 0.35M | 60 | 21.0M |
| 对抗式复审与文档同步（每阶段一次） | 1.2M | 12 | 14.4M |
| **小计** | | | **233.9M** |
| 返工与踩坑缓冲（+19.7%，历史真实比例） | | | 46.1M |
| **合计** | | | **280.0M** |

三档情景（同一个 2.8 亿，花法不同）：

| 情景 | 每插件当量 | 能买到的量 | 适合 |
| --- | --- | --- | --- |
| 省着花（3M/个） | 93 | 7 机制 + 24 新 + 36 存量 + 联调 | 想**铺满**场景 |
| 计划（5M/个） | 56 | 上面那张表 | 推荐：质量与覆盖面平衡 |
| 抠细节（8M/个） | 35 | 约 12 个"精品" | 想**做深**两个域（如形象 + 知识） |

**熔断线**（写进任务卡，不靠自觉）：

- 单个插件 **8M 停手**：停下来复盘"卡在 check、preview 还是测试"，而不是继续加预算。
- 单阶段超预算 25% → 该阶段的剩余项**降级为"只做 manifest + 首版"**，验收留到下阶段。
- 每个里程碑必须有一次**对抗式复审**（另一个 agent 只看代码和文档，不看我的解释）。
- 每完成一个插件，账本记一行（§4.4），**实际值连续两次超过估算 40% 就调单价**。

### 4.3 账本 B：运行吞吐（把 2.8 亿花在"跑任务"上）

现状天花板（`llm.chat` 的真实约束，见 §1.2）：

| 用法 | 单次 token（估） | 次数/日 | 日吞吐 | 跑完 2.8 亿要 |
| --- | --- | --- | --- | --- |
| 对话（默认 `llmDailyBudget=200`，按每次 ~8k 估：4k 字提问 + 部分历史 + 回复） | ~8k | 200 | 1.6M | **175 天** |
| 对话（预算调到 1000） | ~8k | 1000 | 8M | 35 天 |
| 单轮摘要（4000 字、无历史） | ~3.2k | 200 | 0.64M | 438 天 |
| **M2 分片批处理**（4 个插件各 5000 片/日） | ~3.2k | 20000 | 64M | **4.4 天** |
| 全库代码审查（1.2 万文件 × 0.6k） | 7.2M/仓 | 1 | 7.2M | 39 个仓 |
| 知识库全量向量化（1 万切片 × 300） | 3M/次 | 1 | 3M | 93 次重建 |

结论：**2.8 亿 token 的"运行"不是靠多装插件，是靠 M2 + M7。** 没有分片原语，只能靠堆次数；
没有计量，跑起来就不知道停在哪。

五个"吃 token 有产出"的大户（也是 P0/P1 的价值所在）：

| 插件 | 单次规模 | 2.8 亿 token 换来什么 |
| --- | --- | --- |
| `asr-local` + `llm.chunk` | 1 小时音频 ≈ 9k 字 → 12 片（≈38k token） | ≈ 7000 小时录音变成可检索的纪要 |
| `ast-coder`（升级） | 1.2 万文件/仓 | ≈ 39 个中型仓库的全量审查报告 |
| `knowledge-base`（升级） | 1 万切片 | ≈ 93 次全量索引重建（或一次 10 万切片的大库） |
| `bilibili` / `podcast` | 每条 6~8 片（≈22k token） | ≈ 1.2 万条内容的摘要与归档 |
| `weekly-review` | 每周 ~5 次调用（≈40k token/周） | ≈ 每年 2M token —— 这个用法用不掉预算的 1%，正好说明**预算要按需分配** |

**护栏**（缺一条就不要开批处理）：

1. **每插件每日 token 配额**（M7），超了拒绝并写明"今日已用 X/Y"。
2. **片级幂等 + 断点续跑**：失败只重跑那一片，不重跑整个任务。
3. **去重缓存**（内容哈希 → 结果），同一份文档第二次调用不花钱。
4. **失败重试上限 2 次**，`kind=auth/model` 直接不重试（换级没用，见手册的兜底链表）。
5. **冷静期**：批量任务只在用户点了"开始"之后跑，不做开机自启的隐性消费。

### 4.4 统一账本（建设与运行用同一张表）

`token-ledger.csv`（放 `scratch/` 或你自己的追踪表，**不进仓库**）：

```csv
date,phase,item,kind,est_tokens,actual_tokens,rounds,verdict,artifact
2026-11-05,M7,llm.usage,build,3500000,3820000,7,accepted,openspec/changes/plugin-llm-usage
2026-11-06,P0-1,calendar,build,5000000,0,0,wip,
2026-11-06,run,weekly-review,run,0,182000,41,accepted,2026-W45
```

- `actual_tokens` 的采样：每轮结束时记 `in/out`（有渠道 `usage` 就用它，没有就按 §4.1 换算）。
- 每周一次汇总：`Group-Object phase | Measure-Object actual_tokens -Sum`，和 `est_tokens` 对比。
- **verdict 只有四种**：`accepted` / `rework` / `dropped` / `wip`。`dropped` 必须写一句为什么。

### 4.5 里程碑（21 周，按每周 ~13.3M 的节奏）

> 这张表的前提是"有五个月"。**如果只有一晚，读 §8** —— 那里的名单、时刻表与熔断线是另一套。

| 周 | 内容 | 累计 |
| --- | --- | --- |
| W1 | 修门禁（gofmt 22 文件、`imgbed` 上架、AGENTS 漂移）+ M7 提案 | 6M |
| W2–W3 | M7 实施 + M2（`llm.chunk`）+ todo 的 `serves` 三件套 | 24M |
| W4–W6 | **M3（声明式模型工具）** + P0-1/2/7（无宿主依赖的三个） | 66M |
| W7–W9 | P0-3/4/5/6/8 + M4（定时） | 104M |
| W10–W12 | M1（embedding）+ M5（剪贴板，含权限 14→15 的七处）+ P1-1/2/3 | 140M |
| W13–W15 | P1-4~9 + M6（通知） | 180M |
| W16–W17 | P2 七个（含 `pet-look`） | 212M |
| W18–W20 | 存量 36 个深度化（每批 12 个） | 266M |
| W21 | 全量回归、市场页与文档、发布 | 280M |

---

## 5. 执行协议

### 5.1 一个插件的 Definition of Done（九条，缺一条不算完）

1. `node docs/plugin-testkit/plugin-check.mjs plugins/<id>` 全绿（四条检查组都过）。
2. `node docs/plugin-testkit/preview.mjs plugins/<id> --check` 不越界；`--shot` 出三张图
   （空态 / 主态 / 门控态）；声明了 `background` 的必须连常驻帧一起挂。
3. `npx vitest run tests/plugin-<id>.test.js` 通过，含"宿主拒绝一切"那一遍。
   （**沙箱下需要提权**：vitest 要起子进程，受限模式是 `spawn EPERM`。）
4. manifest 申请的权限 == 实际调用的集合（不多不少）；`serves` 与 `callTargets` 双向齐；
   用到的对方方法要带版本下限。
5. 两个宿主的行为差异写清：在 `workshop` 里缺能力要**说出口**（按钮只读 + 说明去哪用）。
6. `version` +1；`market/index.json` 条目、`dist/` 的 zip、`sha256` 三者一致。
7. 若进兜底源：`internal/plugins/sample/embed.go` 手加文件名 + `sample/index.json` 同步，
   `go test ./internal/plugins/ -run TestBuiltinSample` 绿。
8. 契约有变（方法/上限/manifest 字段）→ `plugin-dev.md` + `fake-host.mjs` + 六份文档里的计数
   一起改；`TestDocumentedMethodCountMatchesTheTable` 绿。
9. 注释写"为什么"，提交信息写"为什么改 + 验证了什么（可复跑的命令）"。

### 5.2 并发与分工

- **同时最多 3 个插件**，且必须属于**不同场景域**（都写 `market/plugins/<id>/`，但共用
  `market/index.json`、`tests/`、`docs/` —— 同域并发最容易在 `index.json` 上打架）。
- 每个插件**自带一份 `lib/`**（现状就是如此），不要新建跨插件共享库；
  `ui.css` 从 `market/docs/ui/petpc-ui.css` 复制（并在提交前 diff 一次）。
- 一个插件开工前先建 `openspec/changes/<id>/`（proposal/design/tasks）——**非平凡改动走 openspec**
  是这个仓库的既有习惯，也是"两个月后还能看懂为什么"的唯一保险。
- 契约先行：**先改 `plugin-dev.md`，再写插件**。反过来的结果是文档永远落后一轮。

### 5.3 反模式（都踩过）

| 反模式 | 后果 |
| --- | --- |
| 改了文件不升 `version` | 宿主按 `id+version+矩形数` 判断重建，改了 CSS/JS 也看不到；重启也没用 |
| 改了 `frontend/` 却 `wails build -s` | exe 里嵌的还是旧 `dist` |
| 只重建桌宠、忘了市场程序 | 插件页/工坊界面不更新（`cc/dist` 是编译期嵌入的） |
| `1 秒 setInterval` 等一个文件/共享键 | 日志被轮询淹没（实测占 94%）；用事件 + 15 秒兜底 |
| 浮层面用不透明底板 | 圆角外露出深色方块（本项目查了五轮的那个 bug） |
| 把 `$state` 代理对象直接 `postMessage` | `DataCloneError`，且异常在监听器里，界面看起来正常 |
| sidecar 脚本无 BOM 且含中文 | PowerShell 5.1 按 ANSI 解码 → ParserError，行号指向正常代码 |
| 一次性开 20 个插件 | 没有一个过得了 `preview --check`，然后全部返工 |

---

## 6. 风险

| 风险 | 触发条件 | 缓解 |
| --- | --- | --- |
| **成本失控** | 批量任务 + 无计量 | 先做 M7；每插件日配额；冷静期；去重缓存 |
| **版权/授权** | 音景、形象贴图、TTS 音色 | AGENTS §6 红线：不复制 AGPL 项目的代码与素材；素材只用 CC0/自绘/程序合成 |
| **协议端不稳定** | Yogurt/NapCat/Milky 版本漂移 | 适配器保持"薄"（人设与记忆在 memcore），协议端出问题只换薄壳 |
| **双宿主行为分叉** | 新插件只在桌宠验证 | DoD 第 5 条：`init.host.methods` 判断 + 缺能力说出口 + `Bench` 里模拟拒绝 |
| **文档漂移** | 方法/权限/计数手写 | 已有的 `TestDocumentedMethodCountMatchesTheTable` 继续用；每次加方法都跑一遍 |
| **门禁变慢** | 插件从 36 → 60+ | 每个插件用例 ≤400 行、单文件 <3 s；跨插件的组合用例集中在 `tests/` 少数几个文件 |
| **本机工具链坑** | Go 1.21 vs 1.22、`.ps1` 无 BOM、沙箱只读 | 见 AGENTS §7；打包与 `npm test` 提权；`env.ps1` 已做工具链回退 |

---

## 7. 接下来 72 小时

| # | 做什么 | 产出 | 预算 |
| --- | --- | --- | --- |
| 1 | `gofmt -w` 全仓（22 个文件），跑 `go vet ./... && go test ./... -count=1` | CI 的 gofmt 门禁转绿 | 0.3M |
| 2 | 给 `imgbed` 补 `index.json` 条目并打包（`market/tools/package.ps1`，需提权） | 市场的第 37 个可装插件 | 0.3M |
| 3 | 改 `AGENTS.md` §3.9 那句 `storage.pressure`（与代码一致） | 少一个"文档说 X、代码是 Y" | 0.1M |
| 4 | 写 **M7 `llm.usage`** 的 openspec（proposal/design/tasks + 计数从 43 到 44 的同步清单） | 2.8 亿 token 可观测的第一步 | 1.2M |
| 5 | `todo` 加 `serves: add_task / toggle / list` + 用例 | 打通"助手帮你记一件事"，也是 M3 的试金石 | 1.5M |

做完这五件，再开 P0-1（`calendar`）—— 因为它同时验证三件事：常驻帧 + 墙钟提醒 + `serves` 被发现。

---

## 附：判断一个想法该不该做成插件

1. 它需要**宠物窗口之外的界面**吗？→ 是：插件页（市场程序托管）。
2. 它需要**一块自己的矩形**吗？→ 先问"它能不能就是一种气泡"（右键菜单就是这么解决的）。
3. 它要**读别人的数据**吗？→ `plugin.share`（贴便条）还是 `plugin.call`（问一句）？
4. 它要**写别人的数据**吗？→ 对方必须 `serves` 一个写方法；不许碰别人的私有目录。
5. 它要**模型看得见**吗？→ 今天只能落在那 3 个内置工具里；否则等 M3。
6. 它的**提醒**在用户离开电脑时还有效吗？→ 没有就等 M6。
7. 它的**时间**靠 `setInterval` 数吗？→ 是则写清"休眠/重建后会失准"，或者等 M4。

---

## 8. 一晚冲刺版（2.8 亿 token / 8 小时）

> 前提变了：预算是**造插件**的，而且只有**一晚**。所以 §4 的 21 周表作废，§3 的名单要按
> "今晚能不能验收"重切。目标从"铺满场景"改成**"天亮时 `market/` 里多出 12 个能装、能跑、
> check 全绿的新插件"**。

### 8.0 先把账算清：一晚买不到 24 个

| 量 | 值 |
| --- | --- |
| 可用墙钟 | 8 h（按 22:00 → 06:00） |
| 2.8 亿 token 折成吞吐 | **≈ 35M token/小时**聚合 |
| 一个插件的实耗（§4.2） | ≈ 5M（今晚版可以压到 4M，见 §8.5） |
| 折算产能 | 7 个插件/小时 → 8 h ≈ **56 个**（理论） ÷ 6 条泳道 ≈ 每泳道 9 个 |

**理论值和现实差三倍，差在三处**，而它们都不是 token：

1. **单通道吞吐**：一条模型通道常见只有 2~6M token/h，35M/h 需要 6~12 条并行通道（多 key / 多 provider）。
   通道不够时，2.8 亿**今晚花不完** —— 这是最可能发生的事。
2. **验收带宽**：`plugin-check` / `vitest` / `preview --shot` 都由**我串行**跑（同一个人看截图），
   一波 6 个插件的验收 ≈ 20 分钟，两波就是 40 分钟，返工还要再一轮。
3. **共享写点**：`market/index.json` 只有一个，40 个 zip 都要它；并发写必然打架。

**结论：12 个是稳的，16 个是上限，24 个只能靠放弃 check/vitest（那等于没做）。**

### 8.1 今晚的硬过滤器（四条，缺一不进名单）

1. **不碰 Go、不碰 `frontend/`** —— 不动宿主就不需要 `wails build`、不需要交互桌面门禁。
   → **7 项宿主机制（M1–M7）整批移出今晚**：它们要重建 exe、要真机看裁剪与浮层，一晚上验不完。
2. **不申请新权限** —— 只用现有 14 项，避免走"加权限要同步七处"的长流程。
3. **不依赖别的插件还没有的方法** —— 否则要先改 `todo`/`pomodoro` 这些**已发布**插件（版本 + 回归 + 串行冲突）。
4. **不写 sidecar** —— `.ps1` 的 BOM 坑 + 要真机；唯一例外见 §8.6。

被这四条挡掉、但确实该做的，一律记进"明天清单"，不要在现场硬塞。

### 8.2 名单（12 个，两波）

**共同规格**：`version` 一律 `0.1.0`；各自带一份 `ui.css`（从 `market/docs/ui/petpc-ui.css` 复制）；
`app.js` ≤800 行；声明 `background` 的**必须**同时有 `events` / `net.ws` / `net.fetch` / `plugin.call` 之一
（否则 manifest 解析即失败）；**不许** 1 秒轮询、不许内联十六进制色值、浮层面底板必须透明。

**第一波（T+0:40 → T+3:40，6 条泳道）**

| 泳道 | id | 一句话 | 权限 | 落点 | 必测断言 |
| --- | --- | --- | --- | --- | --- |
| L1 | `calendar` | 订阅 ICS（公开链接），会前 10 分钟提醒，"今天有什么"能答 | net.fetch, storage, plugin.config, pet.bubble, pet.surface, events | 浮层 1 块 + 面板 | ICS 单次事件 + 每日重复 + 跨时区；断网 3 天用缓存 |
| L2 | `habits` | 习惯打卡 + 连续天数 + 头顶徽标 | storage, plugin.config, pet.surface, events | 徽标 + 浮层 | 跨日按本机 04:00 切；断签重置只发生一次 |
| L3 | `anniversary` | 生日/纪念日倒计时（`serves: next`，别的插件和模型都能问） | storage, plugin.config, pet.surface, pet.bubble, plugin.call | 徽标 + 浮层 | 闰年 2/29、跨年、当天显示"今天" |
| L4 | `table-lab` | 拖入 CSV → 类型推断 + 分组统计 + SVG 图 + 模型解读 | storage, plugin.config, llm.chat, pet.bubble | 面板 | 空文件/单列/带引号逗号/BOM；不读磁盘路径 |
| L5 | `convert-kit` | 单位/汇率/时区/进制换算（`serves: convert`，模型最爱的工具形状） | plugin.call, storage, plugin.config | 面板 | 20 组换算表逐条断言；未知单位要**点名拒绝**而不是猜 |
| L6 | `health-reminder` | 久坐/喝水/护眼，全屏时静默 | events, pet.surface, pet.bubble, plugin.config, storage | 徽标 + 浮层 | 全屏时不弹；关掉开关立即停；跨日重置 |

**第二波（T+4:00 → T+6:30，6 条泳道）**

| 泳道 | id | 一句话 | 权限 | 落点 | 必测断言 |
| --- | --- | --- | --- | --- | --- |
| L7 | `weekly-review` | 周日汇总待办/专注/日记 → ≤300 字周报 | plugin.call, storage, plugin.config, pet.bubble, events, llm.chat | 浮层 + 面板 | 三个数据源任一缺失仍能出报告（写明缺哪块） |
| L8 | `github-notify` | PR/Issue/CI 通知 + 一句摘要 | net.fetch, storage, plugin.config, pet.bubble | 徽标 + 面板 | 401/403/限流分别说清；ETag 命中不重复通知 |
| L9 | `price-watch` | 盯网页变化，变了才说一句 | net.fetch, storage, plugin.config, pet.bubble | 面板 | 只发一次（去抖）；首次抓取不报警 |
| L10 | `finance-board` | 股票/基金看板（浮层面 sparkline） | net.fetch, storage, plugin.config, pet.surface | 浮层 + 面板 | 行情源可切换；休市/节假日不刷 |
| L11 | `quicksave` | 速记盒子：一句话记下、标签、可搜、可喂模型 | storage, plugin.config, pet.bubble, plugin.call | 浮层 + 面板 | 500 条上限的淘汰策略可预测；重复内容合并 |
| L12 | `telegram` | 薄适配器（长轮询收发，人设记忆问 memcore） | net.fetch, plugin.call, storage, plugin.config | 后台帧 | 网络断开后 15 秒退避重连；未配置时**不**发请求 |

> `theme-studio` 不在名单里：主题包要装到 `market/themes/`，那是市场程序的路径，插件写不进去 ——
> 它其实需要一条宿主/市场侧的新通道，属于明天的活。

### 8.3 时刻表（T+0 起）

| 时刻 | 做什么 | 我做什么 | 判据（不过就转向） |
| --- | --- | --- | --- |
| 0:00–0:20 | **环境自检**：`plugin-check` 跑一个现有插件、`vitest` 跑一个用例、`preview --check` 跑一次、`gofmt -w` + `git status` | 我 | 三样有一个跑不起来 → 今晚降级为"只产出代码、不跑门禁"，并立刻告知 |
| 0:20–0:40 | 建 12 个目录骨架（manifest + 空 app.js/panel.html/style.css/ui.css）+ 冻结名单 | 我 | manifest 先过一遍 `plugin-check` 的 schema，避免 3 小时后才发现字段错 |
| 0:40–3:40 | **第一波 6 个并行** | 巡检 + 中途答疑，不插手写码 | 3 小时到点即冻结，没绿不救 |
| 3:40–4:00 | 第一波验收：check + vitest + `preview --shot` 截图 | 我（串行） | 出 6 张图，肉眼过一遍排版 |
| 4:00–6:30 | **第二波 6 个并行** | 同上 | 同时把第一波的 rework 派回原泳道（若还有额度） |
| 6:30–7:00 | 第二波验收 + 冻结 | 我 | 出 6 张图 |
| 7:00–7:40 | **串行发布**：`package.ps1`（需提权）→ `index.json` 12 条 → `dist/` → sha256 | **只有我一人动 `index.json`** | `index.json` 与 zip 的 sha256 逐个核对 |
| 7:40–8:00 | 账本结算 + 明天清单 | 我 | 12 行 ledger，写明 `accepted` / `rework` |

### 8.4 今晚的预算与熔断

| 项 | 单价 | 数量 | 小计 |
| --- | --- | --- | --- |
| 12 个插件（首版 + check/vitest 循环） | 5.0M | 12 | 60M |
| 环境自检 + 骨架 + 发布 | 10M | 1 | 10M |
| 第一波 rework（按 40% 命中） | 2.5M | 5 | 12.5M |
| 对抗式复审（第二遍，只看"什么时候会静默失效"） | 1.2M | 12 | 14.4M |
| 视觉/文案打磨（逐张截图过） | 1.0M | 12 | 12M |
| 加塞 4 个（§8.6，仅当吞吐撑得住） | 5.0M | 4 | 20M |
| **小计** | | | **128.9M** |
| 未用额度（**不要为了花完而花**，见 §8.7） | | | 151.1M |

- **单插件 6M 硬停**：到线就交"能跑的首版"并标 `rework`，不继续加钱。
- **一条泳道只跑一个插件**，禁止"一条泳道顺序做三个"（那样时刻表全乱，且验收堆到天亮）。
- 到点冻结优先于做完：**没绿的不救**，明天再来 —— 一晚的价值在于"有 12 个能装的"，不在于"有一个完美的"。

### 8.5 今晚版 DoD（5 条）与明天补的 4 条

**今晚必须（不过就不进 `index.json`）**

1. `node docs/plugin-testkit/plugin-check.mjs plugins/<id>` 全绿。
2. `npx vitest run tests/plugin-<id>.test.js` 通过，含"宿主拒绝一切"那一遍。
3. manifest 申请的权限 == 实际调用的集合；`serves` / `callTargets` 双向齐。
4. `preview --check` 不越界（有 Edge/Chrome 的机器上）；没有浏览器则只做 1–3 并标记。
5. `index.json` 一条 + `dist/` 的 zip + sha256 三者一致。

**明天补（记进清单，不占今晚）**：三张截图（空态/主态/门控态）、双宿主差异文案
（`init.host.methods`）、对抗式复审结论、`plugin-dev.md` 若有新方法则同步、兜底源（`sample`）。

### 8.6 如果 2.8 亿今晚清零（跑满模式）

先花在**质量**上，再考虑加量：

1. 每个插件一遍对抗式复审（12 × 1.2M，已在上表）。
2. 逐张截图过视觉，改间距/对比度/空态文案（12 × 1M）。
3. 还有余量再**加塞 4 个**（自包含、但需要 sidecar 或更多打磨）：
   `focus-sound`（程序合成音景）、`clipboard-history`、`screen-time`、`notes-sync`。

加塞的代价写在前面：`sidecar` 要求 `process` 权限 + `assets` 里声明 + 包内相对路径 +
**`.ps1` 必须带 BOM 或纯 ASCII**（无 BOM 的中文注释会让 PowerShell 5.1 吃引号，
报错行号指向一行正常的代码）。这四条任一漏掉，插件到用户机器上就是"启动了但什么都不产出"。

### 8.7 不要为了花完预算而做的事

| 别做 | 为什么 |
| --- | --- |
| 开 20 条泳道 | 一次 `preview --shot` 就吃满一个核；浏览器实例互相拖死，最后全部超时 |
| 并发改 `market/index.json` | 唯一真冲突点。只在 T+7:00 由一人写一次 |
| 今晚打包 sample（`internal/plugins/sample`） | 脚本只在"同名同版本"时刷新字节；新增要手改 `embed.go`，弄错就是"点安装 404" |
| 今晚动存量 36 个 | 已发布插件要升版本 + 回归 + 重打包，串行冲突风险最高 |
| 为了让 `rework` 变绿而反复重跑整条链 | 一条链 4M；两次不过就冻结，明天带着失败现场看 |

### 8.8 天亮时的交付物

- `market/plugins/` 下 12 个新目录（manifest + panel.html + app.js + style.css + ui.css）。
- `market/tests/` 下 12 个新用例文件。
- 打包好的 `market/dist/*.zip` + 更新后的 `market/index.json`（12 条新条目）。
- `token-ledger.csv` 12 行（`est` / `actual` / `rounds` / `verdict`）。
- **明天清单**：谁 `rework`、谁缺截图、谁在等 M1–M7、`theme-studio` 为什么得等市场侧通道。

---

## 9. 多窗口作战手册（一个窗口 / 多个窗口）

> 这里说的"窗口"= **一个独立 agent 会话**（各自的上下文互不可见）。所以每一条泳道拿到的必须是
> **自包含的 prompt**：它不知道 §8 的存在，不知道别的泳道在做什么，也不知道 `index.json` 归谁。
> 规则全部写进 prompt，不靠"它应该知道"。

### 9.1 先定窗口数

| 窗口数 | 一晚的实际交付 | 代价 / 风险 |
| --- | --- | --- |
| **1** | 3~4 个（顺序做，每个 ~90 分钟） | 最稳，但要人一直守着；没有并行返工的余地 |
| **3** | 8 个（两波 × 3 + 收尾） | 平衡；通道吞吐 3×3M/h 就够 |
| **6** | **12 个**（§8 的两波各 6） | 推荐。需要 ≥6×3M/h 的通道吞吐；CPU 尚可（泳道不跑浏览器） |
| **8** | 12 个 + 4 个加塞（§8.6） | 需要额外 2 条泳道专门做 sidecar 版；`.ps1` 的 BOM 坑要盯 |
| **12** | 不推荐 | 瓶颈从"写码"变成"验收"：12 张截图 + 12 次 check 拉不开，且 `index.json` 冲突概率翻倍 |

**开窗之前先做一件事**：拿一条泳道把 `calendar` 端到端走完（check → vitest → 总控打包），
确认工具链在本机通。**这一步失败的话，多开 6 个窗口只是把同一个坑踩 6 次。**

### 9.2 角色与写权（这是多窗口唯一会真打架的地方）

| 角色 | 谁 | 只准写 | 绝对不碰 |
| --- | --- | --- | --- |
| **总控**（1 个，可以是你的主窗口） | 人 / 主会话 | `market/index.json`、`market/dist/`、`internal/plugins/sample/**`、`scratch/sprint/**` | 不写任何插件的 `app.js`（会和泳道抢同一文件） |
| **泳道**（N 个） | 每个插件一个窗口 | `market/plugins/<id>/**`、`market/tests/plugin-<id>.test.js`、`scratch/sprint/<id>.md`、`scratch/sprint/ledger-<id>.csv` | **`index.json`**、`docs/**`、`tools/**`、别的插件目录、任何 `.go`、`frontend/` |

三条由此推出的硬规矩：

1. **泳道不跑 `preview.mjs`**。它要起无头 Edge/Chrome，6 个实例同时跑会互相拖死，截图也糊。
   截图与 `--check` 一律由总控在冻结后统一跑（一波 6 张，约 20 分钟）。
2. **没人 `git commit`**。`market/` 是独立仓，并发提交必然锁冲突；总控在 T+7:40 一次性提交。
3. **`index.json` 只在 T+7:00 被写一次**，由总控从 12 个 `dist/*.zip` 反推条目（id/version/perms/sha256）。

### 9.3 泳道 prompt 模板（复制即用，只换 6 个占位符）

> `{{…}}` 的取值见 §9.4 的参数表。六个占位符之外**一个字都别改** —— 每一条规则都是踩过的坑。

```text
你是「PetPC 插件冲刺」的一条泳道，只负责一个插件：com.shixiansi.plugin-{{ID}}（{{NAME}}）。
你没有别的上下文，下面这些就是全部信息。不要再向我提问：按规则做完，然后写交接文件。

【0. 开工前必读（10 分钟，只读这三处）】
1. F:\dev\pet_pc\market\docs\plugin-dev.md —— 第 2 节（最小插件）、第 5 节（消息表）、
   第 8 节（几何与 CSS 纪律）、第 12 节（自检清单）。这是唯一契约。
2. F:\dev\pet_pc\market\plugins\com.shixiansi.plugin-todo —— 最小的完整形状。
3. F:\dev\pet_pc\market\plugins\com.shixiansi.plugin-diange —— 有 lib/、有面板、有 serves 的形状。

【1. 只准写这两个位置】
- F:\dev\pet_pc\market\plugins\com.shixiansi.plugin-{{ID}}\**   （新建）
- F:\dev\pet_pc\market\tests\plugin-{{ID}}.test.js              （新建）
禁止修改：market/index.json、market/docs/**、market/tools/**、market/plugins/ 下任何别的目录、
以及 F:\dev\pet_pc\ 下的任何 .go 或 frontend/。违反即视为没做。

【2. 这个插件要做什么】
{{ONE_LINE}}
- 权限（manifest.permissions，不许超出、也不许多申请）：{{PERMS}}
- 界面落点：{{SURFACE}}
- serves（要能被 plugin.call 到就必须同时 background: true）：{{SERVES}}
- version 固定 0.1.0；id 必须是 com.shixiansi.plugin-{{ID}}；目录名 == id。

【3. 硬性实现规则（每条都有前车之鉴）】
- app.js ≤ 800 行；不引第三方库、不引 CDN、不引外链字体。
- ui.css 从 market\docs\ui\petpc-ui.css 复制一份进插件目录；style.css 里不许出现十六进制色值，
  只用 var(--…)。
- 浮层面底板保持透明（不要给 .plugin-frame 加 background）；:root 上写 color-scheme: only light，
  否则 Chromium 会把画布铺成 #121212。
- 不许用 1 秒 setInterval 等文件或共享键；用 events（share.changed / storage.changed）+ 15 秒兜底。
  例外：倒计时重绘可以 1 秒。
- 声明 background: true 时，permissions 必须同时含 events / net.ws / net.fetch / plugin.call 之一。
- net.fetch 的所有域名必须写进 manifest.netHosts（没有"运行时再要"）。
- 私有数据只进 storage.*，键是相对路径（≤4 层）；单值 ≤1 MiB。
- 注释写"为什么"，不写"是什么"。

【4. 验收（必须自己跑，把输出贴进交接文件）】
cd F:\dev\pet_pc\market
node docs/plugin-testkit/plugin-check.mjs plugins/com.shixiansi.plugin-{{ID}}
npx vitest run tests/plugin-{{ID}}.test.js
用例必须覆盖三件事：握手成功 → 主路径 → 宿主拒绝一切时插件不崩且不写数据。
不要跑 preview.mjs（浏览器实例由总控统一跑，避免抢核）。

【5. 必须让这些断言成立】
{{ASSERTS}}

【6. 收工：写交接文件（不写等于没做）】
新建 F:\dev\pet_pc\scratch\sprint\{{ID}}.md，内容严格是：
id: com.shixiansi.plugin-{{ID}}
status: accepted | rework | dropped
check: pass | fail（附最后 5 行输出）
vitest: pass | fail（附最后 5 行输出）
files: 新增/修改的文件清单
tokens: 估算实耗（输入+输出，M）
open: 还没解决的问题，一句话一条
再往 F:\dev\pet_pc\scratch\sprint\ledger-{{ID}}.csv 追加一行：
{{ID}},build,<est>,<actual>,<轮数>,<status>

【7. 熔断】
- 自估超过 6M token 或墙钟超过 3 小时：停手，status 写 rework，卡点写进 open。
- plugin-check 连续两次栽在同一类错误：把规则抄进 open，不要硬试第三种写法。
- "该不该做"拿不准时：不做，写进 open。
```

### 9.4 参数表（填进 §9.3 的六个占位符）

| ID | NAME | PERMS | SURFACE | SERVES | ASSERTS（必须成立的断言） |
| --- | --- | --- | --- | --- | --- |
| `calendar` | 日程日历 | net.fetch, storage, plugin.config, pet.bubble, pet.surface, events | 浮层 1 块 + 面板 | today/next/agenda | ICS 单次事件、每日重复、跨时区各一条；断网 3 天后仍能用缓存出"今天有什么" |
| `habits` | 习惯打卡 | storage, plugin.config, pet.surface, events | 头顶徽标 + 浮层 | streak/today | 本机 04:00 切日；断签只重置一次；跨月连续天数不错 |
| `anniversary` | 纪念日倒计时 | storage, plugin.config, pet.surface, pet.bubble, plugin.call | 徽标 + 浮层 | next | 闰年 2/29；跨年；当天返回"今天" |
| `table-lab` | 表格分析 | storage, plugin.config, llm.chat, pet.bubble | 面板 | analyze | 空文件、单列、字段内带逗号、带 BOM 四种输入；不读磁盘路径（只用 FileReader） |
| `convert-kit` | 换算工具 | plugin.call, storage, plugin.config | 面板 | convert | 20 组换算逐条断言；未知单位点名拒绝，不猜 |
| `health-reminder` | 久坐喝水提醒 | events, pet.surface, pet.bubble, plugin.config, storage | 徽标 + 浮层 | — | 全屏时不弹；关开关立刻停；跨日重置 |
| `weekly-review` | 周报 | plugin.call, storage, plugin.config, pet.bubble, events, llm.chat | 浮层 + 面板 | report/latest | 待办/番茄钟/日记三个数据源任一缺失仍出报告，并写明缺哪块 |
| `github-notify` | GitHub 通知 | net.fetch, storage, plugin.config, pet.bubble | 徽标 + 面板 | — | 401/403/限流分别说清；ETag 命中不重复通知 |
| `price-watch` | 网页变化监控 | net.fetch, storage, plugin.config, pet.bubble | 面板 | — | 首次抓取不报警；变化只发一次（去抖） |
| `finance-board` | 行情看板 | net.fetch, storage, plugin.config, pet.surface | 浮层 + 面板 | — | 行情源可切换；休市/节假日不刷 |
| `quicksave` | 速记盒子 | storage, plugin.config, pet.bubble, plugin.call | 浮层 + 面板 | recent/add | 500 条上限的淘汰顺序可预测；重复内容合并 |
| `telegram` | Telegram 适配器 | net.fetch, plugin.call, storage, plugin.config | 后台帧 | on_message | 未配置时不发任何请求；断网后按 15 秒退避重连 |

> `anniversary` / `habits` / `convert-kit` / `quicksave` 要能被 `plugin.call` 到，**必须**声明
> `background: true`，于是 `events`（或 `net.fetch` / `plugin.call` / `net.ws`）之一必须同时在
> `permissions` 里 —— 这一条漏了，manifest 直接解析失败，而报错信息不会告诉你缺的是它。

### 9.5 总控 prompt（另一个窗口，或你自己的主会话）

```text
你是「PetPC 插件冲刺」的总控。你写 index.json、跑截图、打包、记账；你不写任何插件的业务代码。

【T+0:00–0:20 环境自检（三样，缺一就立刻停并告知）】
1) cd F:\dev\pet_pc\market; node docs/plugin-testkit/plugin-check.mjs plugins/com.shixiansi.plugin-todo
2) cd F:\dev\pet_pc\market; npx vitest run tests/plugin-todo.test.js
3) cd F:\dev\pet_pc\market; node docs/plugin-testkit/preview.mjs plugins/com.shixiansi.plugin-todo --check
另外：cd F:\dev\pet_pc; gofmt -l . 应为空。若不为空，先 gofmt -w <那些文件>（只刷格式，别改逻辑）。
三样任一失败 → 今晚降级为"只产代码不跑门禁"，立刻说出来，不要让 6 条泳道白跑。

【T+0:20–0:40 骨架】
为 12 个 id 建目录，每个只放：manifest.json（id/name/version 0.1.0/permissions/surface 或 background/
entry）+ 从 market\docs\ui\petpc-ui.css 复制的 ui.css + 空的 app.js / panel.html / style.css。
每个 skeleton 先单独跑一次 plugin-check（schema 错要在这一步暴露，不是三小时后）。

【T+3:40 与 T+6:30 两次冻结验收（串行，一批 6 个）】
对每个插件依次：plugin-check → npx vitest run tests/plugin-<id>.test.js →
node docs/plugin-testkit/preview.mjs plugins/<id> --check --shot scratch/sprint/<id>.png
出 6 张图，肉眼过排版与空态；把结论写进 scratch/sprint/<id>.md 的 review 段。
想一次看一批：node docs/plugin-testkit/preview.mjs --gallery plugins --cols 3 --shot sheet.png
（`--state gate` 直达"等你回答"那一屏，`--granted ""` 模拟全部未授权）
没绿的不救：status 标 rework，进明天清单。

【T+7:00 发布（只有你做，且只做一次）】
按 market\tools\package.ps1 打包（需提权）；把 12 个 zip 的 id/version/permissions/sha256 写进
market/index.json；核对 index 里的 sha256 与 dist 里的文件逐个一致。
不要新增 internal/plugins/sample 的包（那是手改 embed.go 才能加的，今晚别碰）。

【T+7:40 结算】
把 scratch/sprint/ledger-*.csv 合并成一张表，输出：每插件 est/actual/轮数/status、
总实耗、以及"明天清单"（rework 谁、缺谁截图、谁在等 M1–M7）。
```

### 9.6 只有一个窗口时怎么办（顺序与取舍）

按这个顺序做，每个 ~90 分钟，**到点就写交接文件换下一个**，不要恋战：

1. `calendar`（同时验证 net.fetch + 定时 + 浮层面，是最有信息量的第一个）
2. `habits`（最便宜，练熟 storage + 徽标）
3. `table-lab`（零网络，练熟面板与 `FileReader`）
4. `convert-kit`（零网络，`serves` 能跑通就是明天所有 `plugin.call` 的地基）

做完这四个再谈第五个；如果只剩 2 小时，**改做 `anniversary`**（最小、最不容易翻车），
然后把余下时间全部投在这 4~5 个的复审与截图上 —— 一个插件被复审过，比五个半成品有用。

### 9.7 泳道之间的三条"边界协议"

1. **早收工不要帮忙改别人的插件**：去 §8.6 的加塞队列取下一个，先在
   `scratch/sprint/<新 id>.md` 写一行 `claimed by lane-<n>`，再开工（避免两条泳道抢同一个 id）。
2. **发现契约本身有问题**（比如某个方法的行为和 `plugin-dev.md` 不一致）：**不要自己改文档**，
   把它写进 `scratch/sprint/contract-issues.md`，由总控在 T+7:40 统一处理 —— 泳道改文档，
   明天六个插件的文档口径就各不相同了。
3. **两个泳道都要用同一个能力**（例如都想要"读剪贴板"）：各自在 `contract-issues.md` 里写一行
   `need: clipboard.read`，然后**各自绕开**（不做），别一个去改宿主、一个去写 sidecar ——
   那会把一晚变成两晚。

