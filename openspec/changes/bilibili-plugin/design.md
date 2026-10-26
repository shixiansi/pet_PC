# design.md — bilibili-plugin

> 上游：`proposal.md`（初版需求，已按推荐值确认 Q0–Q8）。本文写**怎么做**，不含执行清单（见 `tasks.md`）。
> 体例沿用本仓既有的 `openspec/changes/*/design.md`：先钉住「不可动的事实」，再给选择与理由。

## 0. 已定的事

| 决策 | 结论 |
|---|---|
| 宿主 | PetPC 桌宠插件（manifest + 沙箱 iframe + `PluginRequest`） |
| 登录 | v1 手动粘 Cookie；扫码留到 P3（要宿主回响应头） |
| 推送 | 桌宠气泡一句话 + 工坊收件箱卡片；不做系统通知 / B 站私信 |
| 评论 | 默认关；开了默认 `draft`（只出草稿，点「发送」才发） |
| 人设 | 拆成 `persona`（语气）与 `preferences`（判断标准）两项 |
| 待办 | 不接 `todos.write` |
| 默认值 | 3 条/轮、间隔 120 分钟、阈值 70、`enabled=false`（装上先不开） |
| 配置项数 | **31 项**（宿主上限 32，`internal/plugins/manifest.go:178`） |

## 1. 三个决定形态的约束

**约束 1 —— `llm.chat` 一条只能塞 4000 字**（`app_plugins.go:1402`、`:1404`，按 **rune** 计，`clampPluginText` at `:1419`）。
20 分钟视频的字幕 5000–15000 字，装不下。所以流水线**必须自己分段**，且分段次数直接等于成本，
`summaryMaxCalls` 是硬闸而不是建议。

**约束 2 —— 凭据不能过桥。** `config.get` 对 `secret: true` 的项只回 `{set, mask}`（`internal/plugins/config.go:101`），
`MaskSecret` 连短值都整段遮（`:304`）。所以插件页面**读不到自己的 Cookie**，评论用的 `csrf` 也就不能由页面从 Cookie 里取。
设计必须顺着这条走，而不是绕开它 —— 见 §2.2。

**约束 3 —— 常驻帧要「说明用途的权限」。** `validateBackground`（`internal/plugins/manifest.go:318`）
只认 `events` 与 `net.ws`。本插件是**定时轮询 HTTP**：它自己决定何时请求，宿主不推任何东西给它。
所以这一条要放宽成三类（§2.3），否则插件只能在工坊开着时跑 —— 那不是「自己刷」。

## 2. 宿主改动（三处，每一处都带不变量）

> 全部落在既有机制上，不新增方法名、不改插件包格式、不动市场程序。

### 2.1 `net.credentials`：让宿主**代填**凭据，而不是让插件发 Cookie

**不这样做**：给 `net.fetch` 开放 `cookie` 头。那等于把 Cookie 明文交给沙箱页面，
而页面能把它发给任何 `netHosts` 里的地址 —— 一旦哪个 B 站域名以后被复用、或者插件代码被改，
凭据就跟着走了。而且它和约束 2 直接矛盾（页面本来就拿不到明文，开放头只会诱导作者绕路）。

**这样做**：`net.fetch` 增加一个**名字**，不是值。

```go
// 只在这里出现一次：凭据的明文从加密存储直接进请求头，中间不经过任何跨进程边界。
var in struct {
    Method, URL, Body, Encoding string
    Headers map[string]string
    // Credentials names a DECLARED SECRET config item of the caller. When set,
    // the host reads its plaintext and sends it as the Cookie header. The value
    // never enters the payload, the response, an error or a log line.
    Credentials string `json:"credentials"`
    // Referer is the page the request pretends to come from. Bilibili's comment
    // endpoint checks it and its host must pass the SAME allow-list.
    Referer string `json:"referer"`
}
```

判据顺序（照抄既有 `pluginNetFetch` 的顺序，注入放在**主机白名单之后**、与 `app_plugins.go:693-699`
现在处理 `headers` 的位置一致）：

1. 主机白名单 → 不允许就到此为止（凭据**一个字节都不出去**）；
2. `Credentials != ""` 时：调用方必须持有 `net.credentials`（新权限）；该 key 必须是本插件**声明过的 secret 配置项**
   （`ConfigItem(key).Secret`）；明文非空。任一不满足 → 拒绝，错误里**只出现 key 名**；
3. 成功后 `req.Header.Set("Cookie", secret)`；
4. `Referer != ""` 时同样要求 `net.credentials`，且解析成 http(s) URL、其 `Host` 过同一张白名单（否则
   「允许的域名」这句话在 Referer 上就不成立）；
5. 重定向复检照旧（`app_plugins.go:711`）：Cookie 不跟随跨域跳转，因为第二跳的主机要再过白名单。

`netFetchHeaders` 的闭集**一个都不加**（`cookie`/`referer` 仍然不能在 `headers` 里出现）。
凭证头只可能由宿主注入 —— 这是本条改动的全部意义。

**权限词表**（`internal/plugins/capability.go`）

```go
// PermNetCredentials lets a plugin authenticate as the user on a declared host,
// WITHOUT ever seeing the credential.
//
// It is separate from net.fetch because "reach this API" and "speak as me on
// this API" are different questions: a plugin that only reads public data should
// not have to ask for the second one, and an install dialog that lumped them
// together would be asking for consent it cannot describe.
PermNetCredentials = "net.credentials"
```

- 加进 `AllowedPermissions`（14 → **15**），标签「以你保存的网站登录凭据发请求（凭据不显示给插件）」；
- `manifest.go` 新增 `validateCredentials`：声明了 `net.credentials` 却**没有** `net.fetch` 的清单直接拒绝
  —— 否则又是一把「勾了也没用的开关」，而这个词表存在的意义就是消灭这种开关；
- 完整性问题：`TestCapabilityTableIsComplete` 要求「每个权限至少被一个方法用到」。`net.credentials` 不是
  独立方法，而是 `net.fetch` 的**第二道判据**。所以给 `pluginMethod` 加一个字段并在测试里纳入统计：

```go
type pluginMethod struct {
    perm string
    // also lists permissions the IMPLEMENTATION consults as a second gate. They
    // are not required to call the method (net.fetch works without them), which
    // is why they cannot live in perm - but they are still real gates, so the
    // completeness test counts them. Without this field "net.credentials" would
    // be a vocabulary entry with no method behind it, i.e. exactly the switch
    // the invariant forbids.
    also []string
    run  func(a *App, id string, payload []byte) (any, error)
}
```

`"net.fetch": {PermNetFetch, []string{plugins.PermNetCredentials}, pluginNetFetch}`。

### 2.2 评论的 `csrf`：一个显式的非密文配置项

评论接口要 `csrf=bili_jct`，而 `bili_jct` 就躺在 Cookie 串里 —— 但页面读不到 Cookie（约束 2）。
三条路：

| 方案 | 评价 |
|---|---|
| 让宿主把 Cookie 里的 `bili_jct` 抠出来塞进表单体 | 宿主就得懂「B 站 cookie 的哪个字段是 csrf」——把某个网站的字段名写进通用能力层，不做 |
| 让宿主支持「把声明的 secret 填进表单字段」 | 通用性够，但为一个字段新增一套模板机制，收益不抵复杂度 |
| **`biliJct` 单独一项，普通配置（不加密）** | ✅ 采用 |

理由写清楚：`bili_jct` **单独无用**（CSRF 令牌必须配 SESSDATA 才能成事），而 SESSDATA 始终在加密存储里、
永不进页面。所以「jct 落在明文 `config.json`」与「SESSDATA 落在 DPAPI 里」是同一条风险的两种边：
真正能接管账号的那个值走的是密文路径。

用户体验由工坊的一个**登录卡片**兜住（§9）：整串 Cookie 粘一次，页面在本地把它拆成
`biliCookie`（secret）与 `biliJct`（text）两次 `config.set`，然后调一次 `nav` 验证，显示「已登录 UID/昵称」。

### 2.3 `background` 的第三类依据

```go
func (m Manifest) validateBackground() error {
    if !m.Background { return nil }
    if !m.HasPermission(PermEvents) && !m.HasPermission(PermNetWS) && !m.HasPermission(PermNetFetch) {
        return fmt.Errorf(...)  // 文案改为「既未申请 events、net.ws，也未申请 net.fetch」
    }
    return nil
}
```

注释里补第三类，措辞要和已有两段并列：

> `net.fetch` —— the plugin has nothing to be pushed and knows when to ask: a
> **polling** plugin. Requiring `events` here would force it to subscribe to the
> pomodoro it never reads, which is the "switch that does nothing" this function's
> sibling invariant forbids; `net.ws` would be a lie about the transport.

风险与对价：常驻帧 = 一个永远不会被用户看见的 iframe 在跑代码。所以这条**不放松**：
它只是承认「会定时发请求的插件」也需要常驻；用户仍然在安装对话框里逐项勾权限，且卸载/停用即刻拆除。

**这条规则有两份实现，必须同时改。** 除了 Go 的 `validateBackground`，桌宠前端的
`frontend/src/main.js` 里 `backgroundPlugins()` 也自己判一次「谁配拿常驻帧」
（`p.granted.includes('events') || p.granted.includes('net.ws')`），Go 的检查是「清单合法」，
它才是「帧真的挂上去」——只改一边的结果是：清单校验通过、插件装得上、帧永远不挂，
表现为「配置好了却什么都不发生」，而两条门禁都还是绿的。所以第三类要同时加在那两处，
外加 `.dev/check-frontend.cjs` 里那条**静态**断言（它钉的是选择器本身）。

### 2.4b 读 `Set-Cookie`：一处**被真机追加**的宿主改动

原本这份设计里写着「扫码不需要动宿主，凭据在 `data.url` 里」。**那条是错的**：真机扫成功后
`data.url` 是空的，会话在 `Set-Cookie` 里（详见 §9.2 的实测记录）。于是补了这一处：

```go
// ReadCookies asks for the response's Set-Cookie values.
ReadCookies bool `json:"readCookies"`
```

四条约束，全部与已有做法一致：

1. **需要 `net.credentials`**：cookie 与「能发送的凭据」是同一种秘密，读它不该比发它更容易；
2. **在请求发出之前检查**（和凭据头一样）：不允许读的人，连问都不去问远端；
3. **只回 `name=value`**：`Path`/`HttpOnly`/`SameSite`/`Max-Age` 在 Go 侧就丢掉，页面拿不到也用不上；
4. **有界**：最多 20 条、每条第 512 字节。

测试 `TestNetFetchReadsSetCookieOnlyWithPermission` 钉住四件事：配对回来了、属性被剥掉、
没给 `readCookies` 时回复里没有 cookie、没有权限时**请求根本没发出**。

### 2.5 同步要改的断言（漏了就是门禁说谎）

| 位置 | 改动 |
|---|---|
| `internal/plugins/manifest_test.go:168` `TestAllowedPermissionsIsTheFourteen` | 改名 `...IsTheFifteen`，期望列表加 `net.credentials`，并**钉住顺序**（顺序就是安装对话框的画法） |
| `internal/plugins/manifest_test.go:784` 标签完整性 | 自动跟随（遍历 `AllowedPermissions`），但要补一条「新标签非空」 |
| `app_plugins_test.go:1414` `TestCapabilityTableIsComplete` | `used` 集合并入 `spec.also`（并拒绝「自己把自己列为第二道闸」） |
| `.dev/check-frontend.cjs:1228/1245` | 14 → **15**，并在文案里写清 `net.credentials` 是「net.fetch 的第二道判据」；`!PermBackground` 负断言保留 |
| `.dev/check-frontend.cjs:1339` 常驻帧选择器 | 加 `granted.includes('net.fetch')` |
| `.dev/check-frontend.cjs` 的「市场插件权限都在词表里」 | 自动覆盖新插件，无需改 |
| `.dev/check-frontend.cjs` 新增一条 | `net.credentials` 必须与 `net.fetch` 同现（把「装不上」提前成「打不了包」） |
| `tools/plugin-market/frontend/src/main.js` 的 `PERMISSION_LABELS` | 加 `net.credentials`（市场是独立程序，有自己的 map；门禁比对两张表的键） |
| `tools/plugin-market/frontend/src/main.test.js:1357` | 计数 14 → **15**，并断言新标签含「不显示」三个字 |

**不改**：市场程序的权限 UI 结构、`net.fetch` 的 1 MiB 上限、响应头（扫码 P3 再谈）。

## 3. 插件包结构

```
market/plugins/com.shixiansi.plugin-bilibili/
  manifest.json    31 项 config · 7 项权限 · 7 个 netHosts · background:true · shareLLM:true
  panel.html       工坊：状态条 / 单条调试 / 收件箱 / 日志 / 登录 / 隐私说明（同一份也是后台帧，draw 不到东西）
  app.js           全部逻辑，单文件分节（先例：QQ 插件 99 KB 单文件）
  style.css        深/浅两套 token
```

**为什么单文件**：包只装 `manifest.json` + `entry` + 三个固定资源（`market/tools/package.ps1:151-156`），
`assets` 是「额外二进制/资源」的通道，把它当模块系统用会让 MIME 路由与打包规则都为一件小事服务。
`app.js` 用**分节 banner + 命名前缀**保持可读（`bili*` / `sub*` / `llm*` / `gate*` / `ui*`）。

`app.js` 的节（顺序即依赖顺序）：

```
§0  常量与默认值（含 40 字气泡、200 条收件箱、180s 切块、1200ms 最小间隔）
§1  桥：postToHost / request / onInit / theme（照 qq 插件）
§2  存储：readJSON / writeJSON / 体积守卫 / 每日计数
§3  B 站客户端：md5 · wbi · get() · 错误码 · 限速器 · 重试
§4  候选与本地初筛
§5  字幕：view → player/v2 → 下载 → 清洗 → 切块
§6  摘要：map / reduce / JSON 解析 / 预算
§7  四道闸
§8  推送：气泡 + 收件箱
§9  评论：草稿 / 发送 / 风控停机
§10 调度器
§11 工坊渲染（状态、调试、收件箱分页、日志）
§12 启动与恢复
```

## 4. B 站客户端

### 4.1 端点表（一处集中，改接口只改这里）

| 名字 | 方法与路径 | 签名 | 用途 |
|---|---|---|---|
| `view` | `GET /x/web-interface/view?bvid=` | 否 | cid / 标题 / 简介 / 时长 / owner / stat / pages |
| `player` | `GET /x/player/wbi/v2?bvid=&cid=` | **wbi** | 字幕轨列表 |
| `nav` | `GET /x/web-interface/nav` | 否 | wbi keys + 登录态（`data.mid/uname`） |
| `ranking` | `GET /x/web-interface/ranking/v2?rid=&type=all` | 否 | 分区排行 |
| `region` | `GET /x/web-interface/dynamic/region?rid=&pn=1&ps=20` | 视风控 | 分区最新 |
| `search` | `GET /x/web-interface/wbi/search/type?search_type=video&keyword=` | **wbi** | 关键词 |
| `space` | `GET /x/space/wbi/arc/search?mid=&pn=1&ps=10` | **wbi** | 指定 UP |
| `tag` | `GET /x/web-interface/view/detail/tag?aid=&cid=` | 否 | 降级素材 |
| `replyList` | `GET /x/v2/reply?type=1&oid=&sort=2&ps=20` | 否 | 降级素材（热评） |
| `replyAdd` | `POST /x/v2/reply/add`（form） | 需 Cookie+csrf | 评论 |
| `subtitle` | `GET {subtitle_url}`（补 `https:`） | 否 | 字幕 JSON |

`subtitle_url` 常见主机：`aisubtitle.hdslb.com`、`i0/i1/i2.hdslb.com` —— 都在 `netHosts` 里逐个列名
（宿主是**精确匹配**，`capability.go:211` 的 `HostAllowed` 连端口都不通配）。

### 4.2 wbi 签名

```js
const MIXIN_TAB = [46,47,18,2,53,8,/* … 共 64 项，照搬 Bwbi.cjs … */];
function mixinKey(img, sub) { return MIXIN_TAB.map(i => (img + sub)[i]).join('').slice(0, 32); }
function encWbi(params, img, sub) {
  const wts = Math.floor(Date.now() / 1000);
  const q = Object.keys({ ...params, wts }).sort()
    .map(k => `${encodeURIComponent(k)}=${encodeURIComponent(String(params[k] ?? '').replace(/[!'()*]/g, ''))}`)
    .join('&');
  return `${q}&w_rid=${md5(q + mixinKey(img, sub))}`;
}
```

- **MD5 自带**（`SubtleCrypto` 没有 MD5）：`md5.js` 那套 64 步实现，用三个已知向量自测
  （`""`、`"abc"`、`"中文"`）。
- keys 缓存 30 分钟 + 落 `storage: wbi`，重启不重新取；`-403`（签名失效）时**清缓存重取一次**，不循环。
- `view` 不需要签名，但 `player` 需要 —— 这就是为什么「取字幕」绕不开 wbi。

### 4.3 限速、重试、风控

| 规则 | 值 | 理由 |
|---|---|---|
| 全局串行 | 一次一个请求 | 并发是最容易被判「机器人」的形状 |
| 最小间隔 | 1200ms + 0–300ms 抖动 | 一轮 3 条约 10–20 个请求，约 20 秒；不快也不慢 |
| 网络/5xx 重试 | 2 次，500ms → 2s | 抖动 |
| `-352` / `-412` / HTTP 412 / `-509` | **本轮立即中止 + 暂停 6 小时** | 连撞只会把风控窗口拉长 |
| `-101`（未登录） | 评论路径 → 置「未登录」并停用评论；字幕路径 → 走降级 | 不是错误，是状态 |
| `-404` / `62002`（不可见） | 跳过该条，记日志 | |
| `bvid` 形状 | `^BV[0-9A-Za-z]{10}$` | 「单条调试」的输入校验，别把任意 URL 拼进接口 |

请求头：固定 Chrome UA；`referer: https://www.bilibili.com/`（走 §2.1 的受校验注入；
`www.bilibili.com` 因此在 `netHosts` 里）。

## 5. 字幕 → 摘要流水线

```
view ─► bvid/cid/pages ─► player(wbi) ─► subtitles[] ─► 选轨
                                                        │
                                    ┌───────────────────┴────────────────┐
                           有中文轨 │                                    │无轨（未登录 / 真没 CC）
                                    ▼                                    ▼
                           下载 subtitle_url                    desc 降级（标题+简介+标签+热评5）
                                    ▼                                    │
                        segments[{from,to,content}]                      │
                                    ▼                                    │
                    clean → merge(gap<400ms) → stripTags                 │
                                    ▼                                    │
                       全文 rune 数 ≤ 3500 ? ──是──► single pass          │
                                    │否                                │
                                    ▼                                  │
                   chunk(180s, 断句不切断, 单块 ≤3500)                  │
                                    ▼                                  │
                   map × min(块数, summaryMaxCalls-1) ─► 每块 ≤250 字笔记│
                                    ▼                                  │
                   reduce（笔记 + 标题/UP/时长/简介≤300 字）◄────────────┘
                                    ▼
                        严格 JSON → 校验/裁剪 → 卡片
```

**纯函数**（可单测、无副作用，与 vitest 一一对应）：

| 函数 | 契约 |
|---|---|
| `stripTags(s)` | 删掉 `[音乐]`、`[掌声]` 这类 1–12 字的方括号标记与 `♪`；空段丢弃 |
| `mergeSegments(segs, gapMs=400)` | 相邻两段间隔 < 400ms 且前段不以 `。！？…` 结尾 → 合并 |
| `chunk(segs, seconds=180, maxRunes=3500)` | 累积到 180s 或 3500 rune 就切。**切点落在段边界**（`mergeSegments` 之后的段通常就是一句），不额外做标点回溯——实现时发现那一步是多余的回退逻辑；单段本身超长时不切开、不静默截断，留给预算步骤报错 |
| `buildSystem(persona, prefs)` | 固定指令块（≤700 字）+ persona（≤1500）+ prefs（≤1500），总 ≤ 3900 rune（宿主上限 4000） |
| `parseDigest(text)` | 抠第一个 `{` 到最后一个 `}` → `JSON.parse` → 字段校验/裁剪；失败返回 `null` |
| `chunkNote(text)` | map 输出压到 ≤ 250 字，去掉「这段讲了…」这类废话前缀 |

**预算语义**（必须一次说清，否则会偷偷超支）：

- `summaryMaxCalls = N` 计的是**这条视频的全部 `llm.chat` 次数**，含 map、reduce、JSON 重试。
- 块数 > N-1 时只 map 前 N-1 块，卡片标「只读了前 X 分钟」。
- 宿主另有每日请求上限；被拒（文案含「已达今日请求上限」）→ 当天停止总结、记日志、不重试。
- 调用间隔 ≥ 800ms（流式回复本身就有耗时，这里只防连发）。

**降级素材**：`标题 + 简介（≤500 字）+ 标签前 8 个 + 热评前 5 条（各 ≤80 字）`。
卡片与气泡都标「非字幕」，模型提示词里也写明「这是简介与评论，不是逐字稿」，避免它假装听过。

### 5.1 真机实测：匿名拿不到字幕列表（这条改了产品的形状）

`BILI_LIVE=1 npx vitest run tests/plugin-bilibili-live.test.js` 用插件自己的签名代码打真接口，结果：

| 步骤 | 结果 |
|---|---|
| `nav`（匿名） | `code: -101`「账号未登录」，**但 `data.wbi_img` 在**（key 可用） |
| `ranking/v2?rid=36` | `code: 0`，95 条 |
| `view?bvid=` | `code: 0`，cid / 时长 / owner 都在 |
| `player/wbi/v2`（签名） | `code: 0` —— **wbi 签名被接受**，但 `subtitle.subtitles` 是空数组 |
| 采样 20 条（4 个分区） | **20/20 都是 0 轨**；`need_login_subtitle` 字段已经不存在 |

两个直接后果，都已经落到实现里：

1. **`parseBiliJSON` 不能「非零码即失败」**：`nav` 用 -101 报「未登录」却同时给出 wbi key，
   而拿不到 key 就意味着所有签名接口全废。现在 `biliGet` 接受 `allowCodes`，`wbiKeys` 传 `[-101]`，
   `verifyAccount` 也按同一方式读 `data.isLogin`（用标志位而不是拿异常当控制流）。
2. **「没有字幕轨」有两种意思**，只有调用方知道是哪一种，因为只有调用方知道有没有凭据：
   已登录 →「这条大概真没有 CC 字幕」；未登录 →「B 站对未登录请求不返回字幕列表」。
   卡片与步骤条上分别写清楚，而不是统一说「无字幕」。

**这也把需求的重心挪了一格**：`biliCookie` 不再是「只有评论才要」的可选配置，而是**摘要质量的前置条件**。
不贴凭据插件仍然能跑（简介+热评摘要、打分、推送都工作），但每条都会标「非字幕」。

## 6. 四道闸与状态机

```
                ┌────────── 每轮 ──────────┐
candidate ──► filter ──► selected ──► digested ──► scored
  │             │             │            │           │
  │ 已看/黑名单  │ 时长/播放    │ 无字幕      │ 解析失败   │
  │ 关键词      │ 超出 perRound│ → 降级      │ → score 0  │
  ▼             ▼             ▼            ▼           ▼
 skip          skip          note         note       gate
                                                     │
   gate = score ≥ T && useful && pushes < pushDailyCap && inWindow && gapOK
                                                     │
                                       ┌─────────────┴─────────────┐
                                    真 ▼                        假 ▼
                                   bubble + inbox            inbox only
                                       │
                     commentEnabled && mode=auto && score ≥ commentMinScore
                     && comments < cap && !cooling && rid ∈ rids && textOK
                                       ▼
                                   replyAdd → 记录 rpid / 失败即停
```

**闸的判据是纯函数** `decidePush(ctx)` / `decideComment(ctx)`，输入全是数字与字符串，输出 `{ok, reason}`，
`reason` 直接进日志（「今日已推 5 次」「不在活跃时段」「分数 62 < 70」）。这一条是为了让「为什么没推」可回答 ——
否则用户只会看到「它今天什么都没说」。

## 7. 评论子系统

状态：`draft`（存卡片）→ 用户点「发送」→ `replying` → `sent{ rpid }` / `failed{ code, msg }`。

| 检查（发送前，全部本地） | 规则 |
|---|---|
| 长度 | ≤ 60 rune（常量，不设配置项） |
| 具体性 | 必须命中标题/UP 名/摘要里的一个专有名词或数字（正则粗判）—— 挡住「讲得真好」这种万能话 |
| 自曝 | 不含 `AI`、`作为一个(观众|路人)`、`此评论由` |
| 黑名单 | 广告/引流/联系方式/二维码词表 |
| 频率 | 每日 ≤ `commentMaxPerDay`；两次间隔 ≥ 30 分钟（常量）；同 UP 每日 ≤ 1 |
| 范围 | 视频 `tname` 命中 `rids`（跟随刷视频的分区，不再单设一项） |
| 风控 | `-352/-412/-509/-101` → `pausedUntil = now + 6h`，评论当天停用，工坊红字 |

请求形状：

```js
await bili.post('replyAdd', {
  body: new URLSearchParams({ oid: aid, type: '1', message: text, plat: '1', csrf: cfg.biliJct }),
  credentials: 'biliCookie',                     // 宿主代填 Cookie，页面看不到
  referer: `https://www.bilibili.com/video/${bvid}`,
  contentType: 'application/x-www-form-urlencoded',
});
```

`auto` 模式也要过同一套检查，唯一的区别是没有人按那个按钮。

## 8. 调度器（时间语义）

- 后台帧里 `setInterval(tick, 60_000)`；每次 tick 只判断「到点没有」，不用长定时器 ——
  笔记本休眠/唤醒后长定时器会漂，而「每分钟看一眼」什么都不会错。
- `state.nextRunAt` 落盘：`now = max(now, nextRunAt)`，跑完 `nextRunAt = now + intervalMinutes`。
- 启动时若 `nextRunAt` 已过期（关机一夜）→ **90 秒后**跑一轮，不立刻跑（避免开机抢占网络）。
- `activeHours` 支持跨零点（`22:00-02:00`）；不在时段内**不跑**（省请求也省钱），到点后由下一个 tick 接手。
- 停用/卸载：iframe 卸载 → 定时器消失。验收项「停用后 30 秒内零请求」由此成立。

## 9. 工坊界面

一份 `panel.html` 同时承担两个角色（先例：QQ 插件）：后台帧里只有副作用、什么都不画；
市场工坊里画全部内容。角色由 `init.slot` 决定，**不靠猜**。

| 区块 | 内容 | 交互 |
|---|---|---|
| 状态条 | 运行/暂停 · 下次时间 · 今日 刷/推/评 · 模型来源 · 账号状态 | 暂停/继续 · 立刻跑一轮 |
| 登录 | 一个 `<textarea>`「整串粘 Cookie」+ 验证 + 清除 | 拆分 → 两次 `config.set` → `nav` 校验 → 显示 `UID 昵称` |
| 单条调试 | `[BV 号] [跑一遍]` | 显示 ① cid ② 字幕轨 ③ 段数/字数 ④ 摘要 ⑤ 分数；**不推送、不评论** |
| 收件箱 | 卡片列表，20/页 | 复制链接 · 发送评论草稿 · 删除；「清空收件箱」 |
| 日志 | 最近 50 条（时间 + 事件 + 原因） | 清空 |
| 隐私 | 会发送什么、Cookie 在哪、评论何时才会发出 | 纯文本 |

样式纪律（沿用市场与本仓门禁）：`[hidden]` 权威；不画裁剪框（无 `box-shadow`/`filter`/`transform`）；
深浅两套 token；`?theme=` 首帧 + `init.theme` + `theme` 消息三条通道；
**所有会让账号发言的按钮用描边样式**，与「删除」分开摆。

### 9.1 两个程序，一个插件：按钮在工坊，活儿在桌宠

**这条是实现时撞出来的，写在这里是因为它决定了整个界面契约。** 市场程序是**另一个进程**，
它的能力表只有 `storage.*` / `config.*` / `audio.*` / `net.fetch`
（`tools/plugin-market/pluginhost.go:95-110`）——**没有 `llm.chat`，没有 `bubble.say`，而且它那份
`net.fetch` 不接受请求头、不接受 `credentials`**（`tools/plugin-market/fetch.go`）。也就是说：

- 工坊里点「跑一遍」，**那台机器上根本发不出带 UA/Referer/Cookie 的 B 站请求**，也叫不动模型；
- 而插件的真正工作必须发生在**桌宠的常驻帧**里。

所以界面与执行之间是一个**文件命令通道**（不新增宿主能力，与 QQ 插件「工坊只读状态」同一思路）：

```
工坊（市场进程）                     常驻帧（桌宠进程）
  storage.set cmd ──────────────────► pollCommand() 每 3 秒读一次
                                        ├ debug   → 跑一条，写 debug
                                        ├ round   → 立刻跑一轮
                                        ├ verify  → nav 验登录，写 state.account
                                        └ comment → 发某张卡片的评论草稿
  storage.get state/inbox/usage/log/debug ◄──── 每 5 秒回读并渲染
```

三条由此而来的硬约束：

1. **按钮是异步的**，文案必须这么说（「已请桌宠…」），而不是假装点了就完成；
2. **桌宠没在跑时，工坊只能看和改配置** —— 文案要说明，不能让用户以为插件坏了；
3. `cmd` 单槽、带 `at`，超过 120 秒的命令作废（关机一夜后不该补跑一次三小时前的点击）。

命令一共五种：`debug`（跑一条真 BV 号）、`round`（立刻跑一轮）、`verify`（验登录）、
`qr-start` / `qr-cancel`（扫码登录）、`comment`（发某张卡片的评论草稿）。

### 9.2 扫码登录：整段都在常驻帧里，二维码自己画

```
工坊（市场进程）                          常驻帧（桌宠进程）
  点「扫码登录」→ storage.set cmd ─────► qr-start
                                          generate ─► {url, qrcode_key}
                                          storage.set qrcode {status:'wait', url}
  每 5 秒读 qrcode ◄──────────────────────┘
  用 url 在本页**自己**算矩阵画 <svg>
                                          poll(qrcode_key) 每 3 秒，最多 5 分钟
                                            ├ 86101 未扫 → 不变
                                            ├ 86090 已扫 → status:'scanned'
                                            ├ 86038 过期 → status:'expired'（停）
                                            └ 0 成功 → 凭据取自 data.url 查询串
                                                        → config.set（secret）
                                                        → nav 验身份 → status:'ok'
```

**下面三条理由是这段为什么长这样：**

1. **凭据有两种来路，都得认。** 老流程把会话放在 `data.url` 的查询串里
   （`crossDomain?SESSDATA=…&bili_jct=…`）；**当前的真实形状是 `url` 为空、会话在 `Set-Cookie` 里**。
   第一版只认前者，真机扫成功后报「返回里没有凭据」——所以现在两条都读，优先 `Set-Cookie`，
   两条都没有时把**字段名**写进日志（不写值），下次再变也不用猜。
   为此给宿主加了 `net.fetch` 的 `readCookies: true`（需要 `net.credentials`），
   返回 `cookies: ["name=value", …]`，属性（`Path`/`HttpOnly`/`SameSite`）在 Go 侧就丢掉。
   这一条回到了「必须动宿主」，而原因不是设计偏好，是一次真实的扫码结果。
2. **二维码必须自己编码。** 包里只装四个固定文件（`package.ps1`），装不进 JS 库；
   第三方渲染服务要拿到 `qrcode_key`，而那个 key 就是登录权，绝不能出本机。
   于是插件里有一个**只管 byte 模式、纠错等级 M、版本 1–12** 的窄编码器（`§3.5`），
   登录 URL 131 字落在 v8。正确性靠夹具钉住：用本机另一个仓库真实存在的 `qrcode` 包
   离线生成参考矩阵，逐位比对（`market/tests/qr-fixtures.ndjson`）。
3. **二维码永远深色画在白色上**，并把 4 个模块的静区烘进 `viewBox`。跟随主题画成浅色
   二维码，扫不出来，而「看起来挺好」是任何测试都抓不住的那种错。

一个花了半天才定位的坑，写在这里免得下次重犯：参考库默认会**按内容自动选模式**，
`HELLO` 被它编成字母数字模式（更短），而我们的编码器只会 byte 模式——两张都合法、
但永远对不上。定位办法是「用我方的放置顺序去反读参考矩阵」：反读出来的第一个字节是
`0x20`（字母数字模式指示符）而不是 `0x40`（byte 模式），一眼就知道差在哪一层。
夹具现在强制 `mode:'byte'`。

## 10. 安全与隐私（不变量，逐条可验）

1. **Cookie 明文不进 iframe**：页面只能拿到 `{set:true, mask}`；`net.fetch` 里传的是**配置项名**。
2. **Cookie 只可能发往 `netHosts` 里的主机**，且白名单判定在注入之前（顺序不可调换）。
3. **Cookie 不跟随跨域重定向**：第二跳主机要再过白名单，否则整条请求拒绝。
4. **`netFetchHeaders` 闭集不变**：`cookie`/`referer` 永远不能出现在 payload 的 `headers` 里。
5. **日志/气泡/错误文案不含凭据**：错误里只出现配置项名；`log` 落盘前过一遍 `redactSecrets()`。
6. **评论是唯一会「替你说话」的路径**：`commentEnabled=false` 时零 POST（有测试钉住）。
7. **不做**点赞/投币/收藏/弹幕/关注/私信 —— 没有第二个会改账号状态的端点。
8. **字幕原文不落盘**：只在内存里清洗、压缩，落盘的只有摘要。
9. **`llm.chat` 的 system 不含世界快照**：带显式 `system` 时宿主本来就不注入（`app_plugins.go:1485` 附近），
   这里如实利用，不额外索取上下文。

## 11. 测试计划

### 11.1 Go（宿主改动）——已实现，名字以下表为准

| 测试 | 断言 |
|---|---|
| `TestAllowedPermissionsIsTheFifteen` | 15 项；**顺序**（对话框的画法）含 `net.credentials`；无 `background` |
| `TestCapabilityTableIsComplete` | `used` 并 `also`；`net.credentials` 被 `net.fetch` 覆盖；`also` 不许把方法自己的 `perm` 再列一次 |
| `TestNetFetchCredentialsRequirePermission` | 只有 `net.fetch` → 报错提到 `net.credentials`，**且没发出请求**（httptest 计数为 0）；先把 secret 存好，免得「没保存」也能解释失败 |
| `TestNetFetchInjectsTheCredentialAndNeverEchoesIt` | 服务端收到 `Cookie: <secret>` 与校验过的 `Referer`；reply 里没有 secret；`config.get` 仍只回 `set:true`；302 到未声明主机的第二跳**从未被访问** |
| `TestNetFetchCredentialsMustNameADeclaredSecret` | 明文项 / 未声明 key / 已声明但没保存 → 三种拒绝，文案点名 key |
| `TestNetFetchRefererMustBeADeclaredHost` | `https://evil.example/`、`ftp://…`、非 URL → 拒绝且零请求 |
| `TestManifestCredentialsRequiresNetFetch` | 只声明 `net.credentials` → 解析失败，文案点名两个权限 |
| `TestManifestBackground`（扩了三处） | `events` / `net.ws` / **`net.fetch`** 各能支撑 `background`；三者皆无仍拒绝，且文案点名三者 |
| `TestNetFetchReadsSetCookieOnlyWithPermission` | `readCookies` 回来的是 `name=value`（属性被剥）；没给开关就没有 cookie；没有 `net.credentials` → 拒绝且**零请求** |

### 11.2 插件（`market/tests/plugin-bilibili.test.js`，vitest + jsdom）——已实现 42 例

| 组 | 例数 | 钉住什么 |
|---|---|---|
| manifest | 6 | `background` 的第三类依据 · `net.credentials` 与 `net.fetch` 同现 · **第一个 secret 必须是模型 Key** · 评论默认关且默认 draft · 配置项不超宿主上限 · `netHosts` 覆盖代码里用到的每个主机 |
| style.css | 3 | `[hidden]` 必须带 `!important` · 不许 `box-shadow/filter/text-shadow/transform` · 必须有浅色调色板 |
| md5 / wbi | 5 | 与 `node:crypto` 逐字对比（含空串、多块、**中文走 UTF-8**）· wbi 串与服务端重算一致 · 过滤 `!'()*` 且键有序 · 确定性 |
| 字幕清洗与切块 | 6 | 标记清除 · 合并不跨句号 · 时间上限与字数上限 · 超长单段不静默截断 · `planDigest` 的调用数与「只读了前半段」 |
| 摘要解析 | 4 | 代码块包裹 + 前后噪声 · 半截 JSON 返回 null 而不是半个摘要 · 分数/要点/评论裁剪 · system 不超 3900 |
| 四道闸 | 5 | 跨零点时段 · 未配置时段按「不设」处理 · 推送五条件 · 静默时段只入箱 · 评论九条件 · 文案检查（广告/链接/自曝/超长） |
| 候选与初筛 | 4 | 两种字段形状 · 非 BV 丢弃 · 六条规则各自计数 · 主人口播 + 白名单 · 排序 |
| handshake + 工坊 | 3 | 只发一帧 ready、init 前零请求 · 工坊只渲染不排程 · **点「跑一遍」只写命令、自己不发请求** |
| 一轮（推送与评论） | 4 | 一轮只弹一次气泡且卡片全入箱、计数落盘 · 第二轮零 LLM 调用（去重） · **评论关时零 POST** · auto 模式只发一次、`csrf` 在表单体、`credentials` 是**名字**、请求体里没有 SESSDATA |
| 单条调试（常驻帧执行） | 2 | 完整管线写回报告（cid/轨道/段数/分数）且**不花预算不推** · 无字幕走降级且提示词里写明「不是逐字稿」 |
| 二维码（扫码登录） | 4 | 矩阵与参考实现逐位一致（v1/v2/v7/v8，含 131 字真实 URL）· 自选版本与掩码仍合法 · 内容太长时明确失败 · SVG path 只含暗块 |
| 扫码登录流程（常驻帧执行） | 5 | `crossDomain` 凭据解析与解码 · 生成→已扫码→成功并把凭据写进 `config.set` · 中途报「已扫码待确认」· 过期即停且不保存 · 取消会停下轮询 |

> 一处刻意的测试缝：`window.__bili` 暴露纯函数（md5 / chunk / parseDigest / 四道闸等）。
> 经典脚本的局部变量无法 import，而这几处 bug 从外部看不见、又最费 token。它**不是能力**：
> 所有与宿主的交互仍然只能走 `request`，也就是走分发器那道权限门。

### 11.2b 真接口冒烟检查（默认跳过）

`market/tests/plugin-bilibili-live.test.js`，`describe.skipIf(!process.env.BILI_LIVE)`：

```
cd market; $env:BILI_LIVE='1'; npx vitest run tests/plugin-bilibili-live.test.js
```

它用**插件自己的** `encWbi` 打真接口，所以验的是「服务器认不认这个签名」——本地自算自比的单元测试
永远测不出这个。只读端点、不带凭据、不碰评论；断言形状与签名，不断言榜单内容。
2026-10-23 实跑的结果见 §5.1，**其中「匿名 0 轨」是它抓出来的**。

### 11.3 门禁

| 门禁 | 新增/改动 |
|---|---|
| `node .dev/check-frontend.cjs` | 权限 15；`background` 三类；新插件用到的权限都在词表里（自动）；`net.credentials` 必须与 `net.fetch` 同现 |
| `node .dev/run-frontend-harness.cjs` | 现有用例不受影响（不动前端宿主） |
| `market` 的 `npx vitest run` | 三个旧插件 + 新插件全绿 |
| 打包 | `pwsh -File market/tools/package.ps1` 产出 4 个 zip；旧三包的 sha256 **逐字节不变** |
| 真机 | 工坊打开、登录卡片、单条调试跑通一个真 BV 号（网络允许时）；不允许时降级为「用固定夹具回放」 |

## 12. 打包与发版

1. `market/plugins/com.shixiansi.plugin-bilibili/` 四个文件齐 → `package.ps1` 自动纳入（它遍历目录）。
2. `market/index.json` 由脚本回写，`permissions`/`netHosts` 与包内 manifest 一致（安装器会逐字比对）。
3. `internal/plugins/sample/embed.go` 的 `//go:embed` **手工加**新 zip 名（它逐一列名，漏了就**编译失败** —— 好事）。
4. `pwsh -File tools/build-all.ps1`（市场 exe 内嵌前端与内置源）。
5. 已开的工坊窗口不会热更新：关掉重开。
6. `git push` 到 `pet-pc-plugins` 由维护者执行，不由 agent 做。

## 13. 已知限制

- **没有字幕就看不出「讲了什么」**：降级只能给简介/热评，质量差距明显，卡片会标「非字幕」。
- **字幕可用性取决于登录态**：实测匿名 `player/wbi/v2` 一律 0 轨，且 `need_login_subtitle` 已经不在返回里。
  没有 `biliCookie` 时只能做「简介+热评」摘要；有了它也仍会有真的没有 CC 的视频 —— 两种情形在卡片上是分开写的。
- **wbi 是社区逆向结论**：B 站改签名规则的那天，取字幕会整体失效；`log` 里会留下 `-403`，工坊「单条调试」能定位到第 2 步。
- **评论是账号公开行为**：风控规则不透明，`auto` 模式永远有被封评论/降权的可能，所以默认 `draft`。
- **气泡会被覆盖**：`bubble.say` 是提醒类气泡，后来的提醒会顶掉它 —— 这是宿主的既有行为，不改。
- **单文件 app.js** 会长到 1500–2000 行：这是打包规则与先例共同决定的取舍，用分节与命名前缀换可读性。
- **常驻帧的定时器可能被 WebView 节流**：真机上观测到一次「命令投递 30 秒未执行，重启后同样投递 4 秒完成」。
  最像的解释是窗口被遮挡时 Chromium 节流/冻结了 iframe 的定时器，但没有做对照实验，所以只算怀疑。
  缓解是结构性的：命令 120 秒过期不补跑、工坊可重复点击、`tick` 周期本来就是 60 秒 ——
  也就是说「慢了」不会变成「错了」。
- **二维码的终态没被自动化验证过**：generate/poll 两条接口与状态机都有测试和真机记录，但「扫码成功→
  凭据落盘」那一步只有主人拿手机扫一次才能验到。它的实现依据是 `data.url` 里确实带着三个凭据，
  以及一条「没有 SESSDATA 就报错且什么都不保存」的兜底。
- **两处失败是「状态」不是「异常」**：`nav` 的 -101（未登录却带 wbi key）与 `player` 的空字幕列表。
  它们曾经被当成错误处理，代价是「所有未登录用户的字幕全废 / 每条视频处理失败」。代码里现在是
  `allowCodes` 与「按登录态分两种说法」，见 §5.1。

## 14. 与需求文档的差异（需知情）

| 项 | proposal（v0.1） | design（本文件） | 原因 |
|---|---|---|---|
| 配置项 | 37 项 | **31 项** | 宿主上限 32（`manifest.go:178`）；砍掉的是调参项，见 proposal §5 尾注 |
| Cookie 传递 | 「允许 `cookie`/`referer` 头」 | **`credentials: "<配置项名>"` 由宿主代填** | 页面本来就拿不到明文（约束 2）；开放头等于劝作者绕路 |
| `csrf` | 未展开 | `biliJct` 单列一项、非加密 | 单独无用；真正敏感的是 SESSDATA |
| 评论分区 | `commentRids` | 跟随 `rids` | 少一项配置，语义也更直白 |
| `www.bilibili.com` | 标为 P2 | **v1 就在 netHosts 里** | 评论 Referer 与部分字幕请求都需要 |
