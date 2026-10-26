# tasks: 事件时间戳 + 工具调用

## 第一批 —— 事件带本地时间（已完成）

- [x] `PluginEventView` 加 `At string \`json:"at"\``，RFC 3339 + 本地偏移
- [x] 在 `emitPluginEvent` 里盖章（唯一那个发射函数）
- [x] `TestEveryEventCarriesTheHostsLocalTime`：形状、**本地**偏移、时间落在发射时刻
      附近，并且两种事件都带
- [x] README 记下 `at` 与它为什么存在

## 第二批 —— 工具调用（已完成）

- [x] 0. 第 17 项权限 `timer.control`（`timer.start/pause/resume/stop`）
- [x] 1. 导出 `plugins.HasGranted`，三份实现合并成一份
- [x] 2. 协议层：`Tools` / `ToolChoice` / `ToolCall` / `ToolCallDelta`，
      `Message` 的三种形状，流式碎片解析，`Result.addToolCallDelta` 合并
- [x] 3. 工具目录 `app_plugin_tools.go`：5 个工具，每个映射到一个已有的插件方法
- [x] 4. `pluginLLMChat` 的循环：4 轮上限、按轮计预算、`llm.tool` 帧
- [x] 5. 测试：5 个循环测试 + 4 个协议测试 + 1 个合并测试 + 1 个权限判断测试
- [x] 6. "`llm.tool` 帧不带参数"这条改由**行为测试**守：`TestToolFrameCarriesNoArguments`
      直接断言帧里不含标题文本、也不含 `arguments`/`args`/`title` 任何键。
      比静态扫描更强 —— 它跑的是真实路径，而不是在源码里找字符串。

### 与 proposal 的偏差（都是实读代码后的修正）

1. **工具从 6 个减到 5 个。** `reminder_set` 和 `pet_ask` 没有对应的插件方法：
   宿主的提醒是从待办派生出来的、没有给插件的创建入口，`bubble.say` 也是唯一对插件
   开放的气泡方法。没有方法就没有权限可挂，硬加就是"用户被问了一个什么都不做的开关"。
   两个都留了注释说明后续怎么补（先加方法，再加工具）。
2. **多了一个 `todo_list`。** 模型要完成某条待办就得知道它的 id，而 id 只能从列表拿。
   没有它，`todo_complete` 在"插件传了自己的 system 提示、宿主的快照被丢掉"那条路径上
   根本无法使用。
3. **`todo_complete` 不是纯转发。** `todos.toggle` 是**翻转**，对已完成的条目再调一次
   会把它变回未完成——与工具名字正好相反。所以它先查列表，已完成就答"已经完成了"。
4. **工具调的是实现函数，不是 `PluginRequest`。** 不是选择而是被迫：`PluginRequest`
   读 `pluginMethods`，而 `pluginMethods` 持有 `llm.chat`，`llm.chat` 又到达这份目录
   —— Go 把这条**函数值之间**的链判为初始化循环。闸门没有因此丢失：`runPluginTool`
   自己做调度器那两项检查（已启用、`plugins.HasGranted`），用的是同一个原语。

### 实际验证

| 命令 | 结果 |
| --- | --- |
| `gofmt -l`（全部 148 个已跟踪 .go） | 全部通过 |
| `go vet ./...` | 退出码 0 |
| `go test ./...` | `PetPC` / `internal/llm` / `internal/plugins` / `internal/pomodoro` / `internal/store` 全绿 |
| `node .dev/check-frontend.cjs` | ALL FRONTEND CHECKS PASSED (169) |
| 工具循环 5 个测试 | 全 PASS（真实 SSE 端点、真实待办存储、真实事件流） |
| 协议 5 个测试 | 全 PASS（含"不带 tools 的请求字节不变"） |

`internal/desktop` 仍是那 9 个 `open Run key: Access is denied`；完全权限下它们是
全绿的，所以那是沙箱限制。

## 下一步（不在本提案内）

- 长期记忆（一份由模型读写的 md 文件 + 每次改动一个版本）
- 形象包 `figure.json`（词表 / 动作 / 音效 / 精灵图帧动画）
- 本地语音识别（sherpa-onnx SenseVoice int8，约 230 MB）
- 若要做 `reminder_set` / `pet_ask`：先加插件方法，再加工具

## 明确不做

- 宿主自带对话界面（会撤销 `app_chat_slot.go` 的架构决定）
- 插件自己执行工具（工具必须是宿主内置）
- 把工具参数放进 `llm.tool` 帧（会绕过 `todos.read`）
- Anthropic 原生接口与 provider 抽象
