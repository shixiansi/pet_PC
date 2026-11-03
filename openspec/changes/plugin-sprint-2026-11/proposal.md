# 插件冲刺 2026-11-03：12 个新插件 + 三处宿主机制修补

> 状态：**已实施**（2026-11-03）。计划的来源是 [docs/plugin-roadmap.md](../../../docs/plugin-roadmap.md)
> 的 §7（72 小时五件事）与 §8（一晚 12 个插件的名单）。这份文件记的是"为什么这样切、验证了什么、
> 什么留着没做"，不是需求文档。

## Why

计划稿盘出生态的三个空洞，其中两个当晚就能填：

1. **读多写少**：36 个插件里能对外**执行动作**的只有 4 个，49 个 `serves` 里带写入的只有六个，
   而 `todo` 只 serve `stats` —— 于是"把会议纪要里的待办加进列表"这类最常见的助手动作做不到。
2. **三个场景域是空的**：日历/日程、健康、形象。前两个不需要任何新宿主机制。

另外有一条"现在就是坏"的：CI 的 `gofmt` 门禁在 HEAD 上就是红的（30 个文件会被改写），
它会挡住后面每一个 PR。

## What Changes

### 市场侧（`market/`，独立仓）

| id | 一句话 | 落点 | serves |
| --- | --- | --- | --- |
| `calendar` | 订阅 ICS，会前提醒，"今天有什么"能答 | 浮层面 1 块 + 插件页 | `today` / `next` / `agenda` |
| `habits` | 习惯打卡 + 连续天数 + 头顶徽标 | 徽标 + 浮层面 + 插件页 | `streak` / `today` |
| `anniversary` | 生日 / 纪念日倒计时 | 徽标 + 浮层面 + 插件页 | `next` |
| `table-lab` | CSV → 类型推断 / 分组统计 / SVG 图 + 模型解读 | 插件页 + 浮层面（模型那一半） | — |
| `convert-kit` | 单位 / 时区 / 进制 / 汇率换算，零网络 | 插件页（`panel:true`） | `convert` |
| `health-reminder` | 久坐 / 喝水 / 护眼，免打扰时段 | 徽标 + 浮层面 + 插件页 | — |
| `weekly-review` | 周日汇总待办 / 专注 / 日记 → ≤300 字周报 | 浮层面 + 插件页 | `report` / `latest` |
| `github-notify` | PR / Issue / CI 通知 + 一句话摘要 | 徽标 + 插件页 | — |
| `price-watch` | 盯网页变化，变了才说一句 | 插件页 + 常驻帧 | — |
| `finance-board` | 行情看板（SVG sparkline） | 浮层面 + 插件页 | — |
| `quicksave` | 一句话速记，可搜、可淘汰 | 浮层面 + 插件页 | `add` / `recent` |
| `telegram` | 薄适配器（只收发，人设问 `memcore`） | 常驻帧 + 插件页 | `on_message` |

外加**存量一个**：`todo` 从 1.2.3 → 1.3.0，`serves` 从 `["stats"]` 变成
`["stats", "add_task", "toggle", "list"]` —— 计划稿 §1.3(b) 说这是"便宜且立刻能改"的最贵一条。

### 宿主侧（本仓）

1. `gofmt -w` 30 个文件（纯机械 diff），CI 的 gofmt 门禁转绿。
2. **`surface` 必须配 `pet.surface`**（`internal/plugins/manifest.go` 的 `validateSurface`）。
   以前的形状是：manifest 通过校验（`surface` 算一种界面落点）、窗口区域也裁了出来，
   但 `OpenPluginSurface` 按 `pet.surface` 授权拒绝 —— 卡片永远打不开，而报错只在用户点了之后。
   2026-11-03 真的有一个新插件这么写了（300×150 的卡片，权限表里没有 `pet.surface`）。
3. `plugin-check.mjs` 加同一条静态检查（`surface-without-permission`），作者侧在装之前就能看到。
4. `plugin-dev.md` 补三处：`surface` 与 `pet.surface` 的配对、**"我是不是浮层面"要用
   `kind === 'surface'` 而不是 `slot`**、`secret` 只能进 Cookie 头或模型密钥（所以 Telegram /
   GitHub 的 token 只能是 `text`，取舍要写进 README）。
5. `AGENTS.md` §3.9：`storage.pressure` 早就在代码里退休了，文档还写着"目前只有桌宠发"。
   `app_plugins.go` 里那两个死字段（`pressureMu` / `pressured` / `overCap`）与两处过期注释一起清掉。

## Impact

- **新增 12 个插件目录 + 12 个测试文件**（市场仓），`index.json` 37 → 49 条，`dist/` 同步。
- **改动 1 个已发布插件**（`todo` 1.2.3 → 1.3.0）：权限列表一字未动，`serves` 加三项，
  原有 12 条断言一条未改（新用例 +11）。
- **宿主**：`manifest.go` 一条新校验 + `manifest_test.go` 一条新用例（并把三处用 `surface` 却
  没申请 `pet.surface` 的测试夹具改对）+ `app_plugin_surface_state_test.go` 一个用例改成
  "先装再撤授权"（它测的是打开时的授权闸门，而不是安装期校验）。
- **门禁**：`go test ./...`、`tools/plugin-market` 的 `go vet`/`go test`、`check-frontend.cjs`（153 条）、
  `check-catalogue.mjs`（49 条）、市场仓 `vitest`（1933 条）全部通过。

## 明确留着没做（不是忘了）

| 项 | 为什么 |
| --- | --- |
| **浮层面的通用打开入口** | 右键菜单只有 `chat` / `todos` / `pomodoro` 三格，都被占了。所以**新插件的浮层面只能由它自己 `surface.show`**（到点、有下一场…）。`table-lab` / `finance-board` / `quicksave` 的卡片在真机上因此没有用户可达的入口——它们的主界面在控制中心的插件页里，卡片是次要入口。要补需要宿主加一格"插件卡片"（`panelSlot: "card"`），那是界面改动 + 真机验证，留给下一轮 |
| M1–M7 的其余机制 | M7（`llm.usage`）只写了 openspec（见 `plugin-llm-usage`）；其余六项都要动宿主的请求路径或窗口，一晚上验不完 |
| `llm.chat` 的渠道配置 | 用它的插件（`table-lab` / `weekly-review`）依赖用户在渠道表里配好一个模型；没配时它们显示宿主的拒绝原文并降级 —— 这是设计，不是缺口 |
