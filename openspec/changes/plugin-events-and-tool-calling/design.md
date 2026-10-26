# design: 工具是宿主能力，循环在宿主里跑

## 决定 1：`tools` 是一串**名字**，不是 schema

`llm.chat` 的 payload 加一个字段：

```json
{ "text": "...", "tools": ["todo_add", "todo_complete", "pet_say"] }
```

插件给的是**宿主内置工具目录里的名字**，不是一个 JSON Schema。理由：

- 插件自己给的 schema 是"不受信任的文本 → 喂给模型 → 再回到宿主执行器"的闭环，
  这条链上每一段都得校验，而且校验的对象是自然语言说明。
- 工具目录在宿主里，才可能做到"权限没授予的工具**根本不出现在模型看到的列表里**"——
  而不是出现了、被模型调了、再被拒绝。后者会让模型看到一堆它其实不能用的一双手，
  然后一遍遍尝试。

宿主按名字自己组装 `tools` 数组（`name` / `description` / JSON Schema），
名字不在目录里整条请求直接拒绝——和 `events.subscribe` 对未知事件名说不的方式一致。

## 决定 2：首批目录就是六个，每个都映射到已有能力

| 工具 | 落到哪个既有方法 | 需要的权限 |
| --- | --- | --- |
| `todo_add` | `App.AddTodo` | `todos.write` |
| `todo_complete` | `App.CompleteTodo` | `todos.write` |
| `pomodoro_start` | `App.StartPomodoro` | `timer.read`？→ 见下 |
| `reminder_set` | 提醒的既有设定路径 | `todos.write` |
| `pet_say` | `showBubble` | `pet.bubble` |
| `pet_ask` | 带选项的气泡 | `pet.bubble` |

`pomodoro_start` 的权限要单独想：现有权限里只有 `timer.read`（**读**阶段与剩余时间），
没有"控制计时器"。硬套 `timer.read` 会让"能读倒计时"隐含"能开番茄钟"。两个选择：
（a）新增第 17 项权限 `timer.control`；（b）第一批先不放 `pomodoro_start`。
**倾向（a）**，但要连带改 `manifest_test.go` 的 `want` 列表、README 权限表、
`TestCapabilityTableIsComplete`——这正是 AGENTS.md §3.1 说的"加一项权限要同步四处"。
这是本设计里唯一需要产品决定的地方。

## 决定 3：循环在宿主里，插件只看到更丰富的流

请求进来后宿主自己跑：

```
round 0: 请求(messages + tools) → 流式文本 + tool_calls?
  有 tool_calls → 逐个执行 → 把 (assistant 消息 + tool 结果) 追加进 messages → 下一轮
  没有 → 结束
```

最多 4 轮（常量，不是配置）。理由：一轮对话里模型最多需要"加一条待办 → 确认加好了 →
再说一句话"这么多来回；轮数无上限等于把一次请求的账单交给模型。

新增一种帧让插件能显示"它正在干活"（体感提升最大的一处，且不需要新权限）：

```
llm.tool  {requestId, name, ok, error?}
```

不把工具**参数**发给插件：参数里可能有用户的待办正文，而插件本来就通过
`todos.read` 才有资格看那些——把参数塞进无条件的事件流等于绕过它。

## 决定 4：预算按轮计，且失败的工具结果回给模型而不是抛给插件

- 每一次 HTTP 请求算一次 attempt（沿用 `PlusAttempt` 的语义）。一次带两个工具调用的
  用户提问因此消耗 3 次额度。这是**如实计费**：额度是"你一天能问几次"的近似，
  而工具轮确实花了三次往返。上限 200 的默认值下这不构成问题。
- **工具执行失败**（比如待办 id 不存在）不进 `llm.error`，而是作为 tool 结果回给模型，
  让它自己纠正或道歉。只有传输层失败才走 `llm.error`。
- **工具没被授权 / 不在目录里**：请求阶段就失败，不进入循环。

## 决定 5：`hasGranted` 要导出，闸门只留一份实现

`internal/plugins/dispatch.go` 里 `hasGranted` 是未导出的，而工具执行要问同一个问题
（"这个插件有没有这项权限"）。两份实现就是两个会漂移的答案——那正是
`dispatch.go` 顶部那段注释在防的事。所以导出 `plugins.HasGranted(granted, perm)`，
两处共用。

## `internal/llm` 要动的地方

| 文件 | 改动 |
| --- | --- |
| `protocol.go` | `ChatRequest` 加 `Tools []ToolSpec` + `ToolChoice`；`chunk` 的 `delta` 加 `ToolCalls`；新增 `ToolCall` / `ToolSpec` / `ToolResult` 类型；`Message` 要能表达 `tool` 角色与 `tool_call_id` |
| `client.go` | `Chat` 现在返回一次结果；要加一个能跑多轮的入口（`ChatWithTools`），或者让调用方自己循环并把 `Messages` 传回去。**倾向后者**：多轮是产品规则（4 轮上限、预算计数、帧的发送），不该埋在传输层里 |
| `sse.go` | 无需改动（帧解析在 `ParseFrame`） |

`Message.MarshalJSON` 现在对 `Images` 有自定义逻辑，加 `tool_calls` / `tool_call_id`
时要保证文本请求的字节形状**不变**——这是 `protocol.go` 现有注释里明确承诺过的。

## 风险

| 风险 | 对策 |
| --- | --- |
| 模型调用不存在或未授权的工具 | 目录 + 授权两道过滤在**请求组装时**完成，模型看不到不可用的工具 |
| 工具参数被插件看到 | `llm.tool` 帧不带参数 |
| 循环失控 | 4 轮上限；每轮计入预算 |
| 现有插件的字节形状被改坏 | 不带 `tools` 的请求走完全相同的路径；`Message.MarshalJSON` 的既有分支不动 |
| 权限表被悄悄改动 | 若采纳 `timer.control`，四处同步 + `TestAllowedPermissionsIsTheSixteen` 改名成 Seventeen |
