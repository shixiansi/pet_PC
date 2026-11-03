# proposal: 让 net.fetch 能发**字节**（bodyBase64）

## 现状（已实读代码确认）

`net.fetch` 的请求体只有一条路：payload 的 `body` 字段，一个 **string**。

```go
// app_plugins.go, pluginNetFetch
var body io.Reader
if in.Body != "" { body = strings.NewReader(in.Body) }
```

响应那一侧早就有了对称的答案：`encoding:"base64"` 存在的理由写在注释里——"文本形式对非
UTF-8 内容是有损的，看起来成功了、放出来是噪音"。**请求这一侧没有对应的东西。**

于是"把一个文件发出去"这件事在插件里做不到：

- 图片/音频的字节里必然有非 UTF-8 序列；把它塞进 JSON 字符串，Go 的 `encoding/json` 会在解码
  时把无效字节换成 U+FFFD。请求照样发出去、对方照样 200，存下来的是一张坏图。
- 换个形状也不行：`/upload` 这类接口要的是 `multipart/form-data` 里的**二进制分片**，
  base64 只能当文本字段，而服务端按 File 解析。

## 谁需要它（这不是为一个插件开的门）

- **图床上传**（本次的 `com.shixiansi.plugin-imgbed`，CloudFlare-ImgBed 的 `/upload`）：
  用户在控制中心的插件页里拖一张图进来 → 拿到一条链接。它在**控制中心**里跑，因为桌宠的右键
  菜单只有三格（chat / todos / pomodoro），新插件没有格里可去。
- 任何 `/upload`、`PUT`-风格的贴图接口、把本地生成的 PNG 交给某个服务。
- 反面证据：`com.shixiansi.plugin-screen-vision` 为了把截图 POST 出去，绕道随包 PS1
  （`vision-helper.ps1` 的 `Invoke-Upload`）——它是**网络**能力，却要申请 `process`。
  这正是 `bodyBase64` 应该消掉的那种绕路。

## 方案

`net.fetch` 的 payload 加一个字段：

```jsonc
{
  "method": "POST",
  "url": "https://imgcloud.example/upload?authCode=…&responseFormat=object&returnFormat=full",
  "bodyBase64": "<整段 multipart 的字节>",   // 与 body 二选一
  "headers": { "content-type": "multipart/form-data; boundary=…" }
}
```

规则（**一份**，住在 `internal/plugins/fetchbody.go`，两个程序都调它）：

1. `body` 与 `bodyBase64` **二选一**；同时给是错误，且**请求不会发出**（和"拒绝的域名不联网"
   同一条纪律：两次 body 是插件 bug，不能靠猜）。
2. 解出来的字节上限 **16 MiB**，与 `audio.save` 的单段上限同一个量级——"能存一个文件"就该
   "能发一个文件"。先按 base64 长度粗判，再解码，避免为了拒绝而先分配。
3. 带不带 `=` 填充都认（`btoa` 填充，有些客户端不填充）。
4. `Content-Length` 由宿主自己填（`bytes.Reader`），不用 chunked——有些上传接口不接受 chunked。

## 顺手修掉的一处漂移：控制中心会丢掉 `headers`

写这个插件时发现的：`tools/plugin-market/fetch.go` 的 payload 结构里**根本没有 `headers`**，
而手册 §6.10 把它列成 `net.fetch` 的一部分、桌宠也一直支持它。症状会是"上传在桌宠里好、
在控制中心的插件页里坏"，而两边的日志都没有一句说得出口的原因（缺 `content-type` 时对方只会回
"没有文件"）。

所以同一份改动里把它变成**共用规则**（`internal/plugins/fetchheaders.go`：白名单
accept/authorization/content-type/user-agent、单值 ≤1024、不许换行），两边都调它。这与仓库既有
做法一致：`RegisterShare` / `RegisterPrivateStore` / `llmchannels` 都是"规则一份、宿主各留一层壳"。

## 非目标

- **不做文件系统**。插件仍然拿不到路径，`bodyBase64` 的字节来自页面（用户拖进来的文件、粘贴板、
  或它自己拼的图）。
- **不做 GET/DELETE/PUT**。仍然只有 GET/POST；真正的 REST 客户端不是这个能力要做的事。
- **不动响应侧**。`encoding:"base64"`、8 MiB 上限、`truncated` 都保持原样。
- **不给 `net.fetch` 加凭据花样**。`credentials` / `referer` / `readCookies` 仍是桌宠那一份的
  行为；控制中心继续只做无凭据请求（它没有 `net.credentials` 的授权入口）。
- **不为"大文件"开辟新通道**。16 MiB 之外的事情（视频、备份）不属于桌宠。

## 兼容性

- 老插件一行不用改：`body` 的语义没变，新字段缺省是空。
- 新插件在**老宿主**上会得到一个"上传了空 body"的请求（字段被忽略）。手册 §11.9 的规矩照旧：
  **先发宿主，再发用新语法的包**；本次两个程序的 exe 都在同一轮重建。
