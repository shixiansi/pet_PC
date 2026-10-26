# proposal.md — qq-chat-plugin

> 一句话：让大模型在 QQ 群里聊天——新增一个**常驻插件**，它自己拿一根到协议端的长连接、
> 自己记每个群的对话、自己决定说不说话；模型配置借用已有的对话插件，不重复填。

## 1. 为什么需要这次变更

桌宠已经有「对话插件」：它有面板槽位、浮层面、自己的 AI 配置与密钥，**但只在桌宠窗口里说话**。
用户想要的是同一张嘴出现在 QQ 群里：群友 @ 它，它用同一套模型配置回答。

放在插件机制上就撞到四个**已存在的边界**，四个都不是笔误而是当初刻意的设计：

| 边界 | 事实出处 | 后果 |
| --- | --- | --- |
| 插件没有长连接能力 | `internal/plugins/capability.go` 只有 `net.fetch`（一次性 GET/POST、30s、1 MiB、无请求头） | 协议端的事件是**推**的，收不到；而且 OneBot/Milky 的 token 都在请求头里 |
| 后台帧只有一个 | `frontend/src/plugin-host.js` 的 `backgroundFrame`（`At most ONE`），语音插件已占用 | QQ 插件装不上第二个常驻帧，或者把语音插件挤掉 |
| `llm.chat` 不收历史 | `app_plugins.go` 的 `pluginLLMChat` 只读 `{text, requestId}`，宿主拼 `system + 单条 user` | 群聊没有上下文；「上下文条数」这个配置项对任何插件都是摆设 |
| 模型配置属于插件自己 | `docs/llm-chat.md` §6.1（配置从宿主搬进插件）与 `pluginChatConfig` | 第二个要模型的插件只能让用户再粘一次 Key |

## 2. 目标

1. **群聊可用**：被 @ 时（或配置好的前缀）由大模型回答，引用提问那条消息，回复有字数与频率上限。
2. **每群独立记忆**：每个群各记一段往来，落在**插件自己的**私有目录里，市场里能单独清空。
3. **模型配置只填一次**：默认借用对话插件的配置与密钥，密钥不出宿主进程；想另用一套时自有配置优先。
4. **常驻且可关**：没有界面打开也在工作；插件一停用，连接立刻断，不留下"用户已经关掉、却还在收群消息"的会话。
5. **不扩大信任面**：长连接的地址受同一个域名白名单约束，`ws.state` 里的 token 打码，
   `llm.chat` 在带显式 `system` 时**不再注入**桌宠的世界快照（群聊不该看到你的待办）。

## 3. 非目标

- **不做 QQ 官方机器人 API / Satori / go-cqhttp 旧协议**：这次只按 Milky（Yogurt）实现，
  传输层是通用的 `net.ws`，别的协议端可以由别的插件接。
- **不做私聊、群管理、图片理解、语音消息**：私聊明确不处理（`message_scene !== "group"` 直接返回）。
- **不做多连接**：一个插件一条连接（`ws.open` 是替换语义），多协议端是以后的事。
- **不做流式进群**：QQ 没有"半条消息"这回事，回复在整段完成后一次发出。
- **不把记忆搬进宿主**：宿主对插件调用不留转写，这是 `llm.chat` 的既有约定。

## 4. 变更面

| 层 | 文件 | 变更 |
| --- | --- | --- |
| 权限词表 | `internal/plugins/capability.go` | 追加 `net.ws`、`llm.borrow`（11 → 13）；后续追加 `process`（13 → 14，「启动本机程序」，方法 `process.launch`，用于可选地拉起协议端） |
| manifest | `internal/plugins/manifest.go` | 新增 `shareLLM`；`background` 由「必须 `events`」放宽为「`events` 或 `net.ws`」 |
| 白名单 | `internal/plugins/config.go` | `hostWhitelist` 的 URL 形态额外接受 `ws/wss` |
| 传输 | `app_plugin_ws.go`（新） | `net.ws` 四个方法 + 连接托管（拨号、读循环、心跳、退避重连、停用即断、token 打码） |
| 能力表 | `app_plugins.go` | 注册 `ws.*`、`llm.profile`；`net.fetch` 增加受控请求头与重定向复检；`llm.chat` 增加 `history`/`system` 与借用解析 |
| 生命周期 | `app_reminder.go` | `shutdown` 关闭所有插件连接 |
| 前端 | `frontend/src/plugin-host.js`、`main.js` | 后台帧由「唯一一个」改为「每个合格插件一个」，按 id 同步 |
| 插件 | `market/plugins/com.shixiansi.plugin-qq/`（新） | manifest + panel.html + app.js + style.css |
| 市场 | `market/index.json`、`internal/plugins/sample/` | QQ 插件 1.0.0；对话插件 1.4.0（`shareLLM` + 回放历史） |
| 门禁 | `.dev/check-frontend.cjs`、`.dev/run-frontend-harness.cjs` | 权限 14 项（13 项 + `process`）、市场每个插件用到的权限都必须在词表里、多帧同步、`net.ws` 两道门 |
| 依赖 | `go.mod` | `gorilla/websocket` 由间接变直接（版本不变，模块图与 `go.sum` 未变） |

## 5. 验收

- [x] `go test ./...` 全绿（新增 19 个测试函数：11 个 `net.ws`、8 个 `llm.chat` 上下文/借用）
- [x] `market` 的 vitest 全绿（新增 `plugin-qq.test.js` 24 个用例；对话插件 41 个、语音插件 59 个不受影响）
- [x] `node .dev/check-frontend.cjs` 全绿（159 项）
- [x] `node .dev/run-frontend-harness.cjs` 全绿（147 项，含「两个常驻插件各一帧」与「摘掉一个另一个还在」）
- [x] `market/tools/package.ps1` 产出三个 zip，语音包的 sha256 与上一版**逐字节相同**
- [x] 真机连 `E:\lagrangeV2\yogurt-windows-x64`：群内 @ 机器人 → 大模型回复（见 design §9 的手工清单）
