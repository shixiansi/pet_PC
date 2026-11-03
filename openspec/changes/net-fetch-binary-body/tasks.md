# tasks: net.fetch 的二进制请求体（bodyBase64）

## 阶段 1 —— 宿主：规则一份，两个程序都调

- [x] `internal/plugins/fetchbody.go`：`RequestBody(textBody, base64Body string) ([]byte, error)`
      —— 二选一（同时给是错误、**请求不会发出**）、解出来 ≤ **16 MiB**（先按编码长度粗判，避免
      为了拒绝而先分配）、带不带 `=` 填充都认、空 base64 是一个错误而不是"没有 body"
- [x] `internal/plugins/fetchheaders.go`：把 `netFetchHeaders` / `applyPluginRequestHeader`
      从 `app_plugins.go` 搬过来（`FetchHeaders` / `ApplyFetchHeader` / `ApplyFetchHeaders`），
      四个白名单头、单值 ≤1024、不许换行、整张表失败即整体拒绝
- [x] `internal/plugins/fetchbody.go`：`MaxFetchResponseBytes = 8 << 20` —— 顺手修掉一处
      **代码-代码漂移**：控制中心那份 `net.fetch` 的上限是 1 MiB 且不报 `truncated`，而手册写的
      是 8 MiB + `truncated`（§6.10）。两处一个数的时代结束了
- [x] `app_plugins.go`：`BodyBase64` 字段 + 走 `plugins.RequestBody`（`bytes.Reader`，所以
      `Content-Length` 由 net/http 自己填，不用 chunked）+ 头部改调 `plugins.ApplyFetchHeaders`；
      `maxPluginFetchBytes` 变成 `plugins.MaxFetchResponseBytes` 的别名（用例仍在用这个名字）
- [x] `tools/plugin-market/fetch.go`：同一份 `RequestBody` / `ApplyFetchHeaders`；
      **`headers` 这一项这个程序原来根本没有**（收下就丢），所以"同一个插件在桌宠里能上传、
      在插件页里不能"；响应上限改成共享常量并补上 `truncated`
- [x] 打包后市场那两张静态页（`index.html` / `manage.html`）由 `tools/render-pages.mjs` 重新
      生成（37 个插件；`tests/ui-market.test.js` 会数卡片，手写一定会漏）

## 阶段 2 —— 用例

- [x] `internal/plugins/fetchbody_test.go`（7 条）：字节一字不差（含 `0x00`/`0xFF` 这类非 UTF-8）、
      文本与"没有 body"、两个 body 互斥、非法 base64、无填充 base64、上限两侧、空 base64
- [x] `internal/plugins/fetchheaders_test.go`（4 条）：白名单恰好那四个（多一个就要改手册）、
      `cookie`/`host`/`content-length` 被拒且错误里点名四个可用的、大小写归一 + 换行 + 长度、
      "整张表失败即整体拒绝"
- [x] `app_plugins_test.go`（3 条）：`TestNetFetchSendsTheBinaryBodyIntact`（对端按字节比对
      multipart 正文、`content-type` 带 boundary、`Content-Length`、回包照旧）、
      `TestNetFetchRefusesTwoBodiesOrBadBase64BeforeSending`（拒绝时 **hits == 0**）、
      `TestNetFetchCapsTheUploadBody`（超限拒绝且不联网）
- [x] `tools/plugin-market/fetch_test.go`（3 条）：同一个字节一致的用例走本程序的真实绑定
      （install → enable → grant → `PluginRequest`）、拒绝路径、
      `TestMarketNetFetchSaysWhenItTruncated`（上限处不截断、超限处**截断并说明**）

## 阶段 3 —— 文档与契约

- [x] `market/docs/plugin-dev.md` §6.10：payload 加 `bodyBase64`；三条 bullet —— 二选一、
      16 MiB、"上传一个文件的完整写法"（`new Request(url,{body:formData})` 读回
      `arrayBuffer` + 原样带上 `content-type`）；并写明**两个宿主都应用 `headers` 与
      `bodyBase64`**，附 2026-11-02 之前控制中心会忽略 `headers` 的那一段
- [x] `market/docs/plugin-testkit/fake-host.mjs`：`previewAnswers` 给这个插件一条**像真的**
      `net.fetch` 应答（按插件 id 点名，别的插件行为不变），于是 `preview.mjs --shot` 能看到
      "上传成功"那一屏
- [x] `README.md`：「`net.fetch` 的 `encoding`、`bodyBase64` 与体积上限」一节
- [x] `openspec/changes/net-fetch-binary-body/{proposal,design,tasks}.md`

## 阶段 4 —— 第一个消费者：`com.shixiansi.plugin-imgbed`（market 仓）

- [x] 插件本体：`manifest.json`（`net.fetch` + `storage` + `plugin.config` + `plugin.call`，
      `background: true`，`serves: ["upload"]`，`netHosts: ["imgcloud.shipixiv.de5.net"]`，
      `baseUrl` 是 `hostWhitelist` 项）、`panel.html`、`style.css`、`ui.css`（与
      `docs/ui/petpc-ui.css` 逐字节相同的副本）、`lib/{core,multipart,upload,history,workshop}.js`、
      `app.js`、`README.md`
- [x] 界面：拖进来 / <kbd>Ctrl</kbd>+<kbd>V</kbd> 粘贴 / 选文件，一次多张；每张上传中 → 成功（缩略图 +
      链接 + 复制）或失败（图床/宿主原话）；「最近上传」按 `maxHistory` 存 `uploads-v1`
- [x] `plugin.call` 的 `upload`：收 `{dataUrl}`（≤40KB，payload 上限 64 KiB 的 4/3 余量），
      否则**当场**说清"大图请在插件页里拖"——不让调用方等到 30 秒超时
- [x] `market/tests/plugin-imgbed.test.js`（18 条）：manifest 形状与"页面引用的每个文件都进包"、
      multipart 字节（边界、文件名消毒、中文文件名的 UTF-8 字节）、请求形状（`responseFormat=object`
      / `returnFormat=full` / 认证码在查询串 / Token 在 Authorization 头 / 只给 `bodyBase64`）、
      回来的四种形状、401/400 的人话、凭据缺失时**一个请求都不发**、无 `net.fetch` 时控件禁用、
      历史（最新在前 / 去重 / 截断 / 清空删键）、`plugin.call` 三条
- [x] `node market/docs/plugin-testkit/plugin-check.mjs`：0 FAIL（1 条预期内的 WARN：常驻帧握手后
      不发请求——它是一个**应答者**）
- [x] 打包：`pwsh -File market/tools/package.ps1` → `dist/com.shixiansi.plugin-imgbed-0.1.0.zip`
      + `index.json`（只多了这一条，其余 36 条一字未动）
- [x] 兜底源：把同一个 zip 放进 `internal/plugins/sample/`、手改 `embed.go` 的 `//go:embed` 清单、
      在 `sample/index.json` 里插一条（`TestBuiltinSampleMatchesIndex` /
      `TestBuiltinSampleAgreesWithTheMarketCatalogue` 都绿）

## 验证

| 命令 | 结果 |
| --- | --- |
| `go vet ./...` + `go test ./... -count=1`（根 module） | 退出码 0，7 个包全 ok |
| `go vet ./...` + `go test ./... -count=1`（`tools/plugin-market`） | ok |
| `cd market && npm test` | 50 个文件 / **1497 通过**、1 skip（含新增的 18 条） |
| `node .dev/check-frontend.cjs` | ALL FRONTEND CHECKS PASSED (153) |
| `node market/docs/plugin-testkit/plugin-check.mjs plugins/com.shixiansi.plugin-imgbed` | 0 FAIL、1 WARN（常驻帧是应答者，握手后不发请求） |
| `node market/tools/render-pages.mjs` | 两张静态页按 37 个插件重新生成（`ui-market` 的 6 条断言先红后绿） |
| `pwsh -File market/tools/package.ps1` | imgbed 0.1.0 的 zip + `index.json`（只多这一条，其余 36 条未动） |
| `pwsh -File tools/build-all.ps1 -SkipFrontend` | 两个 exe 都重建（前端一个字节没动） |
| **真机**：`node scratch/verify-imgbed-e2e.mjs <认证码>` | 用**插件自己的代码**（`lib/multipart.js` + `lib/upload.js`）往 `https://imgcloud.shipixiv.de5.net` 传了一张 PNG：拿到 `…/file/petpc-test/1793640106284_桌宠_e2e_验证.png`，再 `GET` 回来 200 / image/png / 67 字节且**字节一致** |

真机那一条是这次改动里最值钱的一行：它同时证明了手拼的 multipart（边界、`Content-Disposition`
里的中文文件名、UTF-8 头）、`responseFormat=object&returnFormat=full` 的解析、以及"相对路径要补
站点根"这条规则，全部对着真实服务跑通了——而 `httptest` 只能证明"我们发的东西我们自己也这么读"。

## 没做的（明确留着）

- [ ] **`PUT`/`DELETE`/`PATCH`**：仍然只有 GET/POST。REST 客户端不是这个能力的目标。
- [ ] **控制中心的 `net.credentials`**：`credentials` / `referer` / `readCookies` 仍是桌宠那一份的
      行为（控制中心没有密钥编辑器，也就没有"用户在那里授权了它"这件事）。
- [ ] **流式上传**：整段字节在内存里（页面一份、宿主一份、base64 一份）。16 MiB 之内可以接受；
      再大就该换形状，而不是给 `net.fetch` 加管道。
- [ ] 把 sample 里的 11 个包改成"从市场目录整目录同步"：仍然是手改 `embed.go`（AGENTS §3.7）。
