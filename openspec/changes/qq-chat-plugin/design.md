# design.md — qq-chat-plugin

> 本文件是唯一实现口径：为什么这么切、边界在哪、协议长什么样、失败长什么样。
> 任务清单见 `tasks.md`。

## 1. 事实基线（动手前已实读，不是推测）

| 事实 | 出处 |
| --- | --- |
| 插件能力只有 11 项，无长连接、无入站端口 | `internal/plugins/capability.go`、`app_plugins.go` 的 `pluginMethods` |
| `net.fetch` 只能 GET/POST、无请求头、30s、1 MiB | `app_plugins.go` 的 `pluginNetFetch` |
| 插件事件只投给**已挂载帧** | `frontend/src/plugin-host.js` 的 `routeEvent`（无帧即丢） |
| 后台帧唯一，且要求 manifest 声明 `background` + 持有 `events` | `plugin-host.js`、`manifest.go` 的 `validateBackground` |
| `llm.chat` 只收 `{text, requestId}`，宿主拼 `system + 单条 user` | `app_plugins.go` 的 `pluginLLMChat` |
| 模型配置属于**发起请求的那个插件** | `pluginChatConfig`（从零值 `store.Config` 起算，绝不读桌宠的） |
| 协议端是 Yogurt，Milky 协议：事件 `GET /event`（WS 或 SSE），API `POST /api/<name>`，令牌走 `Authorization: Bearer` 或 `?access_token=` | `E:\lagrangeV2\yogurt-windows-x64 (1)\config.json`、`@saltify/milky-tea` 的 `dist/index.mjs`（`joinURL(baseURL, "api/" + name)`、`joinURL(baseURL, "/event")`） |
| 群消息事件形如 `{event_type:"message_receive", self_id, data:{message_scene:"group", peer_id, message_seq, sender_id, segments[], group_member{...}, group{...}}}` | `@saltify/milky-types` 的 `Event`/`IncomingMessage`；`xunlu-core` 的 `milky-binding.js` 读取的字段 |
| 发消息 `POST /api/send_group_message {group_id, message:[{type:"text",data:{text}}]}`，答题可带 `{type:"reply",data:{message_seq}}` | `SendGroupMessageInput` |

## 2. 决策 D1：长连接由宿主托管（而不是插件自己开 socket）

三条理由，任一条都足以否掉"插件页面自己连"：

1. **令牌只能走请求头**。Milky 与 OneBot 都用 `Authorization: Bearer <token>`；浏览器的
   `new WebSocket()` 无法设置请求头，于是所有开了 token 的部署都连不上。
   （Milky 还接受 `?access_token=`，但 OneBot 实现并不都接受，方案不能只靠它。）
2. **页面是可抛弃的**。插件更新会按 `id+version` 重建帧，列表重渲染也会摘掉帧；
   连接若挂在页面上，重建期间到达的群消息就永久丢失——而"丢一条 @"和"没人在 @"长得一模一样。
3. **白名单必须是宿主的决定**。`net.fetch` 的整套设计就是"宿主说这个插件能访问哪里"；
   页面能自己开 socket 会让它变成装饰（沙箱没有 CSP，全靠自己守）。

因此新增 `net.ws`：

```
ws.open  {url, token?}  校验 scheme(ws/wss) 与 host:port 是否在白名单 → 起 supervisor，立即返回
ws.send  {text}          仅在 open 状态下写一帧文本
ws.close {}              停止 supervisor（不再重连）
ws.state {}              当前状态，给"刚挂上来的界面"对时用

event ws.message {text}  一条入站文本帧
event ws.state   {state, url, error, since, received, sent}
```

`state` 取 `connecting | open | error | closed`；`error` **不是终态**——supervisor 继续重连，
所以界面可以显示"上次失败是什么"而不必把它当结论。

### 2.1 生命周期（`pluginWSHub.supervise`）

```
loop:
  alive? 否 → 忘掉自己，退出
  发 connecting
  dial（10s 握手超时，不代理）
    失败 → 发 error + 按退避等待（1,2,4,8,15,30s）→ 重试
    成功 → 发 open → 起心跳；读循环直到出错
  连接断了且未被 close → 发 error + 退避 → 重试
```

- **一条连接一个插件 id**：`ws.open` 是替换语义（先停旧的再起新的），
  所以"哪根 socket 收到 `ws.send`"永远只有一个答案。
- **心跳即存活检测**：30s 一次 ping。同一 tick 里检查插件是否仍"已装 + 已启用"，
  不是就 `close`。这一条不是冗余——supervisor 只在两次连接之间检查存活，
  一条健康的连接若不检查，会在用户停用插件后继续接收群消息。
- **关闭时机**：`ws.close`、插件被停用/卸载、宿主退出（`App.shutdown` → `closePluginWS`）。
  宿主退出必须显式关：让 socket 随进程消失，对协议端而言和"掉线"没区别，它会继续为这个
  会话排队事件。

### 2.2 白名单与令牌

- 白名单 = manifest 的 `netHosts` **∪** 那个 `hostWhitelist` 设置项的当前值
  （`plugins.ConfigHosts`）——和 `net.fetch` **同一个表达式**，不另写一份。
  为此 `ConfigHosts` 的 URL 分支额外接受 `ws/wss`：用户填的是同一个地址。
- 匹配是**整串精确**（`host:port`），`HostAllowed` 不做后缀通配，这一条沿用既有实现。
- 令牌：`Authorization: Bearer <token>` 头 + 插件自己把 `?access_token=` 编进 URL
  （Milky 的 SDK 就是这么做的）。因此 **url 里可能出现凭据**，所以：
  - `pluginWSView.URL` 只放**打码后**的地址（查询串每个值变 `***`）；
  - 拨号用的原始 URL 只存在连接结构体里，永不进事件、永不进日志；
  - 错误文案经过 `redactError` 再过一遍同样规则。

### 2.3 帧的形态

只要**文本帧**。二进制帧丢弃而不是转 base64：这套能力服务的协议（Milky/OneBot）都是 JSON 文本，
"可能是字节"的载荷只会逼每个插件写一条永不执行的分支。读上限 8 MiB，写上限 512 KiB。

## 3. 决策 D2：后台帧从"唯一"改为"每个合格插件一个"

`PluginHost.backgroundFrame` → `backgroundFrames: Map<pluginId, frame>`，对外三个动作：
`syncBackground(plugins)`（新出现的挂上、不在了的摘掉、同 id 同版本复用）、
`closeBackground(id)`、`backgroundPlugins()`（`main.js` 里算合格集合）。

合格判据四条不变，只有第四条放宽：已装 + 已启用 + 声明 `background` + 持有
**`events` 或 `net.ws`**。放宽带两处同步：

- `manifest.validateBackground`：`events` 或 `net.ws`，否则拒绝并同时点名两者；
- 前端同一个断言，两侧都查——只看一侧的话，另一侧漂移了也没人知道。

为什么必须放宽容忍两个权限：`events` 是"宿主告诉我番茄钟结束了"，`net.ws` 是"我守着一根自己
打开的连接"，是两件不同的事。要求 QQ 插件申请 `events` 会变成"让用户勾一个插件永不读的开关"，
正是权限词表一直拒绝的那种东西。

## 4. 决策 D3：`llm.chat` 接受 `history` 与 `system`

```json
{ "text": "...", "requestId": "...",
  "system": "可选的完整系统提示词",
  "history": [{"role": "user"|"assistant", "text": "..."}] }
```

宿主侧的裁剪（数值即常量，见 `app_plugins.go`）：

| 项 | 上限 | 理由 |
| --- | --- | --- |
| 新消息 `text` | 4000 字 | 一条群消息不该把一条请求变成一次账单 |
| `system` | 4000 字 | 同上 |
| `history` 条数 | 32 | 丢**最旧**的，保住贴着新消息的那几轮 |
| `history` 合计 | 12000 字 | 32×4000 仍然太大，得有一个总量闸 |
| role 取值 | 仅 `user`/`assistant` | 未知 role **丢弃**：`system` 出现在历史里就是"第二个系统提示词"，且位置在真正的系统提示词之后 |

**`system` 是隐私开关，不是格式选项**：给了 `system` 就**只**用它，不再注入 `BuildMessages`
的世界快照（时间、待办、番茄钟、最近的番茄总结）。群聊的答案是给一屋子人看的，
"以下是当前的真实情况"会把用户的待办清单一起发出去。没给 `system` 时行为与之前完全一致
（对话插件仍走快照路径）。

## 5. 决策 D4：跨插件借用模型配置（`shareLLM` + `llm.borrow`）

两端各一个声明，谁也不越权：

- **借出方**在自己 manifest 里 `"shareLLM": true`（校验：必须同时申请 `llm.chat`，
  否则就是在广告一个没有代码读的配置）。对话插件 1.4.0 起这么做。
- **借入方**在自己 manifest 里申请 `llm.borrow`——安装时被问的是这一条，
  也就是"允许它用别的插件已经配好的模型"。

解析顺序（`resolvePluginChatCredential`）：

1. **自有配置完整** → 用自有的。这是用户"停止借用"的唯一开关，不需要卸载任何东西；
2. 否则，若持有 `llm.borrow`，找**已装 + 已启用 + 声明 `shareLLM` + 配置完整**的插件，
   按 id 升序取第一个（不让"安装顺序"影响结果，因为安装顺序在界面上看不见）；
3. 都没有 → 报错并点名该补什么：「「X」还没有可用的大模型配置：它自己没填，也没有其它插件
   声明可以借出（shareLLM）」。

预算与用量仍记在**借入方**的私有目录（`llm-usage`）：预算是宿主强制的，谁发起请求谁记账，
不然借入方就成了一个不受预算约束的旁路。

`llm.profile` 是这个权限背后的**方法**（权限词表要求每个权限至少被一个方法使用）：

```json
{ "ownReady": false, "borrowAllowed": true, "ready": true,
  "source": "borrowed", "model": "deepseek-chat",
  "lender": { "id": "com.shixiansi.plugin-chat", "name": "对话插件" } }
```

它只报告"有没有、叫什么、哪个模型"，**不含端点也不含密钥**。

## 6. 决策 D5：`net.fetch` 增加受控请求头

Milky 的 API 是 `POST /api/<name>` + `content-type: application/json` + 可选
`Authorization: Bearer`。原来的 `net.fetch` 一个头都设不了，所以加 `headers`：

- **白名单四个**：`accept`、`authorization`、`content-type`、`user-agent`。
  不放 `host`（虚拟主机）、`cookie`（环境凭据）、`content-length`（与 body 冲突）；
- 值不许含 CR/LF、长度 ≤1024（防注入）；
- **重定向复检**：`CheckRedirect` 逐跳复核白名单，最多 5 跳。
  以前只查首个 URL 就跟随 Go 默认的 10 次跳转——一次性 GET 泄漏不了什么，
  但一个 `Authorization` 头就不同了。

## 7. 插件侧设计（`com.shixiansi.plugin-qq`）

### 7.1 manifest

```
permissions: net.ws, net.fetch, llm.chat, llm.borrow, storage, plugin.config, process
background: true            // 没有 events：它不订阅宿主事件
netHosts: ["127.0.0.1:3000"]
config: 26 项（server / accessToken / atOnly / replyChance / proactiveMinutes / prefix /
        groupAllow / persona / contextTurns / replyMaxChars / cooldownSec / quoteReply /
        visionModel / visionMaxImages / visionPrompt / stickerUrls / ownerQQ / stickerChance /
        stickerCooldownSec / llmProvider / llmBaseUrl / llmModel / llmApiKey[secret] /
        clientPath / clientArgs / autoStartClient）   // 后三项见 7.5
entry: panel.html
```

**只有一个 secret，且必须是 `llmApiKey`**：宿主把"第一个 secret 项"当作 `llm.chat` 的凭据，
所以协议端令牌走 `text` 类型（它也确实是本机回环上的东西），否则一个插件会把自己的
协议令牌当 API Key 发给模型服务商。

### 7.2 一份文档两个外观

`panel.html` 同时是**常驻帧**（宿主的 `slot: "background"`）与**市场工作台**
（`slot: "workshop"`）。`init` 消息里的 `surface.slot` 是唯一开关；两个区块都在 markup 里，
只显示其中一个（`[hidden] { display: none !important }` 这条规则是必需的：
`.workshop { display: flex }` 会盖掉 UA 的 `[hidden]`）。

### 7.3 一条群消息的处理链

```
ws.message → JSON.parse → event_type === "message_receive"?
  data.message_scene !== "group"        → 丢（私聊不在范围）
  sender_id === self_id                 → 丢（协议端会上报机器人自己说的话）
  !groupAllowed(peer_id)                → 丢
  touchGroup(peer_id)                   → 记「这个群还活着」（主动开口用得到）
  !shouldAnswer(text, mentioned)        → 丢（见下）
  stripPrefix 后为空                    → 丢
  busy(同群已有请求在飞) / 冷却未过       → 丢（并记一行日志）
  ↓
  storage.get hist_<群号>              → 每次都重读，市场里"忘掉这个群"立刻生效
  llm.chat {text, system, history}     → system 由人格 + 群名 + 字数上限 + 时间组成
  tidyReply(去 Markdown/多余空白/截断)
  net.fetch POST /api/send_group_message {group_id, message:[reply?, text]}
  storage.set hist_<群号>（保留 contextTurns×2 条）+ groups 索引
```

`shouldAnswer` 是三档（1.1.0 起），而不是一个布尔：

| 情况 | 结果 |
| --- | --- |
| 被 @，或命中 `prefix` | 一定回 |
| `atOnly` 开着（默认） | 其他消息一律不回 |
| `atOnly` 关掉 | 按 `replyChance`（0–100）抽签：0 不插话、100 每条都回 |

为什么不是"关掉 atOnly = 全回"：那是另一个功能，且配上 `cooldownSec` 会变成
"每五秒回一条"，看起来像坏了而不是像话多。中间那档才是通常想要的。

### 7.3.1 主动开口（1.1.0 起，默认关闭）

`proactiveMinutes = N > 0` 时，插件每 60 秒抽一次 `1/N` 的概率（平均间隔 N 分钟、实际不规律），
命中后从「最近 N 分钟内有人说过话」的群里随机挑一个，用 `proactivePromptFor` 的提示词
（明确写"没有人在问你，是你自己想插一句"）与那个群的记忆生成一句话发出去，**不引用任何消息**
（没有可引用的提问）。三条限制：空群不开口、同群单飞与冷却共用回答路径、`ws` 不在 `open` 时不动。

它只记一条 assistant 轮次（`remember(..., null, reply, ...)`）：凭空造一条"用户说过 X"
会让下一轮把这句话当成真的有人问过。

失败路径：`llm.chat` 的拒绝文案（宿主的中文句子）**只进日志**，不发进群里——
一屋子人不需要知道机器人是怎么配的。

### 7.4 观测：三个文件，两个进程

市场窗口是**另一个进程**，拿不到常驻帧的内存，所以插件把状态写进自己的 storage：

| key | 内容 | 写者 | 读者 |
| --- | --- | --- | --- |
| `status` | 连接状态、错误、self id/昵称、计数、写入时间 | 常驻帧 | 工作台 |
| `groups` | 群号 → 群名 / 往来数 / 最近时间 | 常驻帧 | 工作台（含"忘掉这个群"） |
| `log` | 最近 60 行 | 常驻帧 | 工作台 |

写入**没有防抖定时器**，只有"在飞即合并"：稀有事件（一次权限拒绝、一次掉线）若被定时器
压在缓冲区里，工作台那一秒的空白和"没反应"无法区分。

### 7.5 协议端的可选自动启动（1.8.0 起）

协议端（Yogurt）是**另一个程序**，"没人监听"是首次运行的常态而不是错误。所以连接前先探一次：

```
config.get → endpointsFor(server, token)
net.fetch POST <base>/api/get_login_info  （requestWithTimeout，1.5s）
  有响应（哪怕是 status: "failed"）        → 程序在跑，照常 ws.open
  传输层失败（拒绝/超时/未声明域名）
    autoStartClient 关                     → 日志「没开启自动启动」，照常 ws.open
    clientPath 为空                        → 日志「路径为空」，照常 ws.open
    缺 process 权限                        → 日志「缺少 process 权限」，照常 ws.open
    本次页面已试过                          → 日志「不再重复拉起」，照常 ws.open
    否则 process.launch {path, args} → {pid} → 日志「已启动协议端客户端（pid N）」
         → sleep(2s) → ws.open（失败照旧退避重连）
```

三条判断值得单独说：

- **"有响应"就看 `net.fetch` 有没有 reject，不看 HTTP 状态**：`api()` 在 `status: "failed"`
  时抛异常，而"在跑但还没扫码登录"正是那个答复。用 `api()` 探会让插件在协议端**正在运行**时
  再拉起一份——这是这个功能唯一不能犯的错。所以探测只用 `request('net.fetch', …)` 的传输层
  成败（并为此加了 `requestWithTimeout`，宿主 `net.fetch` 的超时是 30 秒，等不起）。
- **一次页面生命周期只拉一次**（`clientLaunchTried` 在调用前就置位）：帧会被插件更新重建，
  否则每次重建都会再开一个 Yogurt。
- **探测失败不阻断连接**：四种拒绝启动的情况都照常 `ws.open`——用户可能一分钟前自己开过了，
  或者程序已经在别的端口上。

`process.launch` 的参数由 `clientArgs` 按**空白**切分（不是 `listOf` 的逗号规则：一个参数里
可以合法地含逗号，例如路径）。超 32 个参数不截断，交给宿主拒绝并显示它的中文句子。

## 8. 边界与失败（逐条对应实现）

| 情况 | 行为 |
| --- | --- |
| 协议端没起 | `ws.state error` + 退避重连；插件照常在，随时能连上 |
| 地址不在白名单 | `ws.open` 拒绝（"未声明的地址"），**不拨号**，不留下连接 |
| scheme 写成 `http://` | 拒绝并点名只支持 `ws/wss` |
| `accessToken` 错 | 握手失败 → `error` → 重连；令牌在状态里打码 |
| 插件被停用 | 下一次心跳（≤30s）关闭连接；帧被摘掉 |
| 插件被更新（版本变） | 帧按 id+version 重建 → 重新 `ws.open` |
| 宿主退出 | `shutdown` → `closePluginWS` 显式关闭 |
| 群里没 @ 它 | 不回、不产生模型请求 |
| 模型未配置/无可用借用 | 只记日志，群里沉默 |
| 模型超时 | 同上（`llm.error` 的文案已中文化） |
| 同群连发多条 | 冷却 + 单飞：一条在答时其余丢弃并记日志 |
| 历史文件损坏 | 当空历史开始（`JSON.parse` 失败即空） |
| 群号出现在文件名里 | `hist_<群号>` 先过滤成 `[0-9A-Za-z_-]`，再截到 64 字符 |

## 9. 真机验证清单（Yogurt）

1. 启动 `E:\lagrangeV2\yogurt-windows-x64 (1)\yogurt.exe`（`milky.http` = `127.0.0.1:3000`）；
2. 装对话插件并在市场里填好 `llmBaseUrl` / `llmModel` / `llmApiKey`；
3. 装 QQ 插件（默认配置即可：`server = 127.0.0.1:3000`，`atOnly = true`）；
4. 打开市场里 QQ 插件的界面：状态应为「已连接」，机器人账号为协议端登录的号；
5. 在群里 @ 它并提问 → 应引用你的消息回复；再问一句带指代的问题 → 应记得上一轮；
6. 停用插件 → 状态变「未连接」，群里再 @ 不应答；重新启用 → 自动重连。

## 10. 已知取舍

- **协议端令牌是明文配置项**：`ConfigItem` 只有一个 secret，而那个位置必须留给 `llmApiKey`
  （宿主用它当模型凭据）。本机回环令牌落在 `config.json` 里的风险可接受；要加密就得给
  ConfigItem 增加"角色"标记，那是另一次变更。
- **一条连接一个插件**：多协议端/多账号要拆插件或等 `ws.open` 支持多路。
- **没有流式进群**：QQ 的"消息"是一次性的，逐字发送等于刷屏。
- **`ws.open` 不返回握手结果**：立即返回 `connecting`，成功/失败都走 `ws.state`。
  好处是插件的一条请求不会被网络卡住；代价是"地址写错了"要等一秒才在状态里看到。
