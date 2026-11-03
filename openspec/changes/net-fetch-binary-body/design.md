# design: net.fetch 的二进制请求体

## 1. 为什么不是 sidecar

同一件事有第二种做法：随包一个 PS1，宿主托管、`process` 权限、脚本里用 `HttpClient` 发
multipart，插件读脚本写的 JSON。`com.shixiansi.plugin-screen-vision` 就是这么把截图发出去的。

不选它的理由是**形状**，不是能力：

| | `bodyBase64` | sidecar |
| --- | --- | --- |
| 用户要授予的权限 | `net.fetch`（已经为这条请求授予了） | `net.fetch` + `process`（"启动本机程序"） |
| 在哪个窗口里能用 | 两个宿主都行 | 只有桌宠起 sidecar；控制中心没有 `process` |
| 失败时看得见什么 | 宿主那句中文（"未声明的域名"/"超过上限"） | 脚本的 stderr → 日志；页面只能看到"文件没出现" |
| 这是一次网络请求还是一次本机调用 | 网络 | 本机程序 |

最后一行是重点：**用户在这里同意的是"这个插件可以往它声明的域名发请求"**，而 `process` 说的是
"这个插件可以在这台机器上启动程序"。为了一次 POST 让人对后者点头，是把两件事混成一件；
而 `process` 的文案（"启动并停掉本机程序"）根本描述不了用户实际上要允许的事。

还有一条实测事实：`process` + 手填路径的插件里有四个从来没跑起来过（AGENTS.md §3.9 记着）。
一个"上传图片"的功能不该继承那条失败史。

## 2. 为什么是"两个字段"而不是"encoding 也管请求"

另一个自然的写法是给 `encoding` 加一个方向：

```jsonc
{ "encoding": "base64", "body": "iVBOR…" }   // 意思是"body 是 base64"
```

这和响应侧的 `encoding` 会让同一个字段有两种含义（同一个 payload 里，`encoding` 说的是响应的
形状），而"这个字符串是正文还是 base64"是**请求**的形状。一个字段两种方向，第一次读代码的人
一定会读错一边——而这正是本仓库 §3 一堆注释在防的那类事。

两个显式字段（`body` / `bodyBase64`）让 payload 自解释，代价是"同时给"要判一次：

```go
if hasText && hasBytes { return nil, fmt.Errorf("…只能给一个…") }
```

## 3. 规则为什么住在 `internal/plugins`

`net.fetch` 有**两份实现**：桌宠（`app_plugins.go`，带凭据、重定向复查）和控制中心
（`tools/plugin-market/fetch.go`，精简版）。两份实现是刻意的（两个程序的能力表不同），但**规则**
不该有两份：

- `RequestBody(text, base64)`：二选一、16 MiB、填充可选、非法 base64 的中文错误；
- `ApplyFetchHeader(s)`：四个白名单头、单值 1024、不许换行。

这一条是本次改动的**实证**而不是洁癖：写插件时发现控制中心那份**根本没有 `headers`**，
于是同一个插件在桌宠里能上传、在工坊里不能——而工坊恰恰是这个插件唯一的界面。
`internal/plugins` 里已经有 `RegisterShare` / `RegisterPrivateStore` / `llmchannels` 三个
"规则一份、宿主各留一层壳"的先例，这是第四个。

## 4. 上限为什么是 16 MiB

- 与 `audio.save` 的单段上限相同：能存进私有目录的文件，也应该能发出去；
- 一张手机照片 3–8 MB，一屏截图 1–3 MB，一张 GIF 十几 MB——16 MiB 覆盖"人不假思索会传的东西"；
- 传输是 JSON，线上是 4/3，所以 16 MiB 的字节 = ~21 MiB 的字符串。`audio.save` 已经在
  同一条桥上来回这个量级（真机上 QQ 插件的贴纸库就是这条路），不是新风险；
- 先按**编码长度**粗判再解码：为了拒绝一个 500 MB 的字符串而先把它解码出来，是这条上限本要
  防的事。

## 5. 与权限闸门的关系（没有变）

`bodyBase64` 不新增权限、不改变检查顺序。它只在第 4 步"碰参数"里生效，而第 1–3 步
（已装已启用 → 方法存在 → 权限已授予）仍然在它之前。所以"没授权时什么都没发生"这句话
对它依然成立——包括**没有一次网络请求**。

## 6. 真机形状（2026-11-02，在实例 `https://imgcloud.shipixiv.de5.net` 上实测）

```
POST /upload?authCode=…&responseFormat=object&returnFormat=full&uploadChannel=telegram
Content-Type: multipart/form-data; boundary=…
→ 200 {"src":"https://imgcloud.shipixiv.de5.net/file/1793637171292_pettest.png"}
```

两点值得记下来，因为它们决定了插件怎么写：

1. **`responseFormat=object` 才有对象**；默认是数组 `[{"src":"/file/…"}]`，而 `src` 默认是
   相对路径（`/file/…`），要 `returnFormat=full` 才是能直接贴进 Markdown 的完整链接。
2. **没有 `publicUrl`**：那个字段只在管理员配了"默认 URL 前缀"时才出现。所以"链接"这件事
   不能只认一个字段名。

## 7. 失败会是什么样

| 情况 | 用户看到 |
| --- | --- |
| 域名不在白名单 | `net.fetch 未声明的域名：…`（请求没发出） |
| 两个 body 都给 | `net.fetch 的 body 与 bodyBase64 只能给一个…`（请求没发出） |
| base64 写坏 | `net.fetch 的 bodyBase64 不是合法 base64：…`（请求没发出） |
| 超过 16 MiB | `net.fetch 的请求体超过上限 16777216 字节`（请求没发出） |
| 请求头不在白名单 | `net.fetch 不允许设置请求头 "cookie"（只支持 …）`（请求没发出） |
| 图床自己拒绝 | 插件把 `401 Unauthorized` 翻译成"认证码不对"（插件的事，不是宿主的） |
