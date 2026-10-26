# tasks: 事件时间戳 + 工具调用

## 第一批 —— 事件带本地时间（已完成）

- [x] `PluginEventView` 加 `At string \`json:"at"\``，RFC 3339 + 本地偏移
- [x] 在 `emitPluginEvent` 里盖章（唯一那个发射函数，而不是每个调用点各盖一次）
- [x] `TestEveryEventCarriesTheHostsLocalTime`：断言形状、**本地**偏移（不是 UTC）、
      时间落在发射时刻附近，并且**两种事件**都带（证明戳在发射函数里而不是某个调用点）
- [x] README §「宿主这边只剩两样东西」记下 `at` 与它为什么存在

### 实际验证

| 命令 | 结果 |
| --- | --- |
| `gofmt -l app_plugins.go app_plugins_events_test.go` | 无输出 |
| `go test . -run 'TestEveryEventCarriesTheHostsLocalTime\|TestPhaseEndedCarriesTheHostsConclusion\|TestEdgeEvents'` | 4 个用例全 PASS |
| `go vet ./...` | 退出码 0 |
| `go test ./...` | `PetPC` / `internal/llm` / `internal/plugins` / `internal/pomodoro` / `internal/store` 全绿 |

`internal/desktop` 仍是那 9 个 `open Run key: Access is denied`（沙箱限制）。

## 第二批 —— 工具调用（未开始）

顺序是按"先后能独立验证"排的，不是按重要性。

- [x] 0. **`pomodoro_start` 的权限归属已定**：新增第 17 项 `timer.control`
      （`timer.start` / `timer.pause` / `timer.resume` / `timer.stop` 四个方法共用）。
      已于 `feat(plugins): 新增第 17 项权限 timer.control` 落地，
      连带四处同步 + `TestAllowedPermissionsIsTheSixteen` → `...Seventeen`
- [ ] 1. `internal/plugins` 导出 `HasGranted`，`dispatch.go` 改用它（同一份实现两处共用）
- [ ] 2. `internal/llm/protocol.go`：`Tools []ToolSpec`、`ToolCall`、`tool_calls` 解析、
      `Message` 表达 `tool` 角色；**确认不带 tools 的请求字节形状不变**（现有测试守着）
- [ ] 3. 宿主侧工具目录：`internal/plugins` 还是 `package main`？倾向 `package main`
      （`app_plugin_tools.go`），因为每个工具都要直接调 `App` 的方法，而
      `internal/plugins` 不认识 `App`
- [ ] 4. `llm.chat` 的循环：4 轮上限、按轮计预算、`llm.tool` 帧（**不带参数**）
- [ ] 5. 测试：模型要求调两个工具 → 两次都执行 → 最终文本流式返回；
      未授权的工具不出现在请求里；工具失败回给模型而不是抛给插件
- [ ] 6. `check-frontend.cjs` 加一条静态断言：`llm.tool` 帧的构造里没有参数透传
      （AGENTS.md §2：门禁按名字/路径读东西，新增帧要一并纳入）

## 更后（不在本提案内）

- 长期记忆（一份由模型读写的 md 文件 + 每次改动一个版本）
- 形象包 `figure.json`（词表 / 动作 / 音效 / 精灵图帧动画）
- 本地语音识别（sherpa-onnx SenseVoice int8，约 230 MB）

## 明确不做

- 宿主自带对话界面（会撤销 `app_chat_slot.go` 的架构决定）
- 插件自己执行工具（工具必须是宿主内置，见 design 决定 1、2）
- 把工具参数放进 `llm.tool` 帧（会绕过 `todos.read`）
- Anthropic 原生接口与 provider 抽象
