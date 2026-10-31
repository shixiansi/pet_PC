# tasks: 模型渠道（`provider` + `llm.providers`）

## 阶段 1 —— 宿主（Go）

- [x] `llm.chat` 的 payload 增加 `Provider string`（借出方插件 id），注释写清它不是扩权
- [x] `resolvePluginChatCredential(..., provider)`：点名时**不回退**；自己 → 拒绝；没有 `llm.borrow`
      → 拒绝并点名权限；被点名者不是就绪借出方 → 拒绝并点名那个 id
- [x] `findLLMLender(exclude, only)`：`only` 与默认路径共用同一个"现在能不能答"的谓词
- [x] 新方法 `llm.providers`（`pluginLLMProviders`，挂 `llm.borrow`）：列出渠道、默认渠道、自己的状态；
      **不含端点、不含密钥**
- [x] `capability.go` 的 `PermLLMBorrow` 注释：两个方法与 `provider` 都在这一份同意之内
- [x] `app_plugin_llm_channels_test.go`（6 个用例）：点名生效且不回退、未配置的渠道报错、未知/自己、
      没有 `llm.borrow`、列表与默认值、列表不泄漏端点

## 阶段 2 —— 契约与自检工具（插件仓）

- [x] 手册 §6.6：`provider` 字段 + "端点与密钥从哪来"的三条顺序；新增 `llm.providers` 一节
- [x] 手册 §15 方法表加一行；方法总数 36 → 37（本仓指针、AGENTS.md、市场 README 同步）
- [x] `fake-host.mjs`：`METHODS` 加 `llm.providers`，`okResult` 给一个空列表
- [x] README 的 `llm.borrow` 行：写明 `llm.providers` 与 `provider` 点名

## 阶段 3 —— 一个纯渠道插件

- [x] `com.shixiansi.plugin-llmchannel` 0.1.0：`shareLLM` + `llm.chat` + `plugin.config`；
      config 只有 4 项（预设 / 地址 / 模型 / 密钥），预设是 `select`（含 ollama 与"其它 OpenAI 兼容"）
- [x] 它**自己从不调 `llm.chat`**，页面也**不发任何 `llm.*` 请求**（控制中心不服务它们，问了就是 WARN）
- [x] `market/tests/plugin-llmchannel.test.js`（8 个用例）：声明、唯一的 secret、选项都有 value、
      两条"不调模型"的静态断言、页面只发 `config.get`、显示渠道 id 与模型与密钥已存、缺模型时说出口

## 验证

| 命令 | 结果 |
| --- | --- |
| `go test . -run 'TestLLMChat\|TestLLMProviders' -count=1` | 6 个新用例全过（连同既有的借用用例） |
| `go test ./... -count=1` | 全绿 |
| `cd market && npm test` | 全绿（新增 8 个渠道插件用例） |
| `plugin-check.mjs`（`…plugin-llmchannel`） | 0 FAIL / 0 WARN |
| `node .dev/check-frontend.cjs` | 通过 |

## 没做的（明确留着）

- [ ] 负载均衡 / 故障转移 / 按价路由（一个渠道一个端点，选哪个由调用方点名）
- [ ] 把 `model` 做成渠道里的"模型清单"（现在是一个字段 + 每次调用的 `model` 覆盖）
- [ ] 让 `llm.providers` 报告每个渠道支持哪些模型（需要渠道侧先有清单，见上）
- [ ] 在桌宠里给"渠道"做一个选择界面（现在由消费方点名或按默认）

## 阶段 4 —— 渠道表（多渠道 / 多模型 / 多密钥）

- [x] `internal/plugins/llmchannels.go`：表的形状与校验（8 渠道 / 16 模型 / 8 key、id 形状、
      http(s) 地址、`keys` 列表即真相）、`Read/WriteChannelTable`、密钥文件路径、`ChannelsGet/Set`
      与 `RegisterChannels`（两个宿主共用）
- [x] 密钥：`SecretKeeper`（DPAPI）+ 一 key 一文件；读只回 `{set, mask}`；从列表里删掉一个 key
      会清掉它的文件
- [x] `llm.chat` 的 `channel` / `keyId`；解析顺序与"地址要过 netHosts"（否则表就是 SSRF 原语）
- [x] `llm.providers` 带上每个渠道的行（id / 模型 / key 数量，仍不含地址与密钥）
- [x] `market/plugins/com.shixiansi.plugin-llmchannel` 0.2.0：manifest 只留一个 `hostWhitelist`
      配置项；`panel.html` + `app.js` 是渠道编辑器（增删渠道、模型列表、密钥行、默认渠道、保存时
      自动补白名单）
- [x] 测试：`internal/plugins/llmchannels_test.go`（读写/掩码/保留/清理/9 种非法表）、
      `app_plugin_llm_channels_table_test.go`（表生效、点名渠道与模型覆盖、无密钥拒绝、地址越界拒绝、
      只有表的借出方、写表要 llm.chat）、`market/tests/plugin-llmchannel.test.js`（8 个：渲染、
      保存形状、补白名单、不重复补、失败可见、不调 llm.chat）

| 命令 | 结果 |
| --- | --- |
| `go test ./... -count=1`（根 module） | 全绿 |
| `go test ./... -count=1`（`tools/plugin-market`） | 全绿 |
| `cd market && npm test` | 542 个用例通过 |
| `plugin-check.mjs …plugin-llmchannel` | 0 FAIL / 0 WARN |
| `node .dev/check-frontend.cjs` | ALL PASSED (138) |
| `package.ps1` + 两个 `wails build -s` | 10 个 zip；桌宠 21:02:37、控制中心 21:02:26 |