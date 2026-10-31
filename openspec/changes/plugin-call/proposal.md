# proposal: 插件之间可以互相提问（plugin.call）

## 现状（已实读代码确认）

插件之间今天只有一条路：**`plugin.share` 托盘**。

- 它是"一个键一个文件"，写在**发布者**私有目录的 `share/` 下；读别人要先在 manifest 的
  `shareRead` 里点名，单值 64 KiB，最多 8 项（`internal/plugins/share.go`、
  `manifest.go` 的 `validateShareRead`）。
- **它没有推送，也没有订阅**：消费方只能自己轮询。仓库里唯一的跨插件请求/响应样本是 B 站那条线
  ——QQ 插件每 2 秒读一次 `bilibili/bili-results`（`market/plugins/com.shixiansi.plugin-qq/lib/bili.js`
  的 `pollBridge`）。

于是"人设 + 记忆放一个核心插件，接入端放瘦适配器"这件事，逻辑上成立，**在当前 API 上却做不出
请求-响应**：一条消息要过两次轮询（去一趟、回一趟），邮箱只有一格（同群两条并发消息互相覆盖），
没有 ack，没有超时语义，失败只能靠调用方自建 requestId 去猜。

这不是"延迟高一点"的问题：它是"这个形状根本跑不起来"，除非给宿主加一条真正的调用通路。

## 方案

加一个能力：**`plugin.call`**。调用方插件用一次普通的能力调用，问另一个**已安装、已启用、
声明了自己会答这个问题**的插件，拿回它给的答案。

```js
// 接入端（瘦适配器，有 net.ws）
const reply = await request('plugin.call', {
  target: 'com.example.memory-core',
  method: 'chat',
  payload: { platform: 'qq', groupId: '123', userId: '456', text: '在吗' },
});
```

```js
// 核心插件（background: true，有 llm.chat + storage）——它要答的话，实现在自己的帧里
// 收到 {type:'call', method, payload}，回一条 {type:'call-result', ticket, ok, result}
```

两处声明，方向各一：

```json
// 调用方
"permissions": ["plugin.call", "net.ws"],
"callTargets": ["com.example.memory-core/chat"]

// 被调用方
"permissions": ["plugin.call", "llm.chat", "storage"],
"serves": ["chat"]
```

## 三条硬规则

1. **调用不转移权限。** 被调用方用它自己那一套已授予的权限干活。A 调 B 不能让 B 做 B
   本来不被允许做的事，A 也只能拿到 B 选择返回的那一点东西。这条是整个安全故事的骨架：
   `plugin.call` 不是"借用对方的身份"，是"请对方用它自己的身份回答"。
2. **闸门仍然只有一个。** 授权在 Go（`Registry.Dispatch` 的顺序不变：已装已启用 → 方法存在 →
   权限已授予 → 才碰参数），送达在前端。前端**从不判断权限**，它只在 Go 已经点头之后
   把信封投给目标帧；投不出去（目标没有常驻帧）和答不回来（超时）都不是权限决定。
3. **一次调用一条日志。** `plugin.call` 是可见的动作（INFO），不是管道；被拒绝是 WARN。

## 为什么是"一个权限 + 两个 manifest 字段"

- 加第二项权限（`plugin.serve`）会是一个**背后没有任何方法**的开关，正好违反
  `TestCapabilityTableIsComplete` 守着的那条不变量；而用户在安装对话框里读到的两句话
  是同一句话："与其它插件互相调用"。
- `callTargets` / `serves` 是**精确点名的对子**，和 `shareRead` 同一个形状：安装时能逐条
  念出来（"它会调用 QQ 插件的 chat"），永远不会退化成通配。

## 非目标

- **流式**。`llm.chat` 的流仍然属于调用它的那个插件；被调用方想把中间态递给调用方，是它自己
  的事（它可以再走一次 `plugin.call`，或者写 `plugin.share`）。
- **广播 / 订阅**。仍然没有插件事件总线：`SubscribableEvents` 只有宿主的三项事实事件。
- **共享内存**。沙箱没有变：帧仍然是 opaque origin，串数据只能复制。
- **控制中心（市场程序）支持**。它只挂一页、不服务 `events`/`llm.chat`，也没有常驻帧；
  在那里调 `plugin.call` 的答案是"本程序不支持该方法"，与 `llm.chat` 同一处理。
- **让宿主持有记忆/人设**。那是撤销 `AGENTS.md` §1 已经做过的决定：宿主不认识任何具体插件。
