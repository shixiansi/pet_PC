# tasks.md — bilibili-plugin

> 上游：`proposal.md`（需求，Q0–Q8 已确认）+ `design.md`（技术方案）。
> 排序即依赖顺序：**先宿主、后插件、最后打包**。每个任务自带判据，判据不过就不算完成。

## 阶段一览

| 阶段 | 任务 | 出口门禁 | 状态 |
|---|---|---|---|
| A 宿主 | T1–T5 | `go test ./...` 全绿；`node .dev/check-frontend.cjs` 全绿 | **完成**（见 A 末尾的实测记录） |
| B 插件内核 | T6–T13 | `market` 的 `npx vitest run` 全绿 | **完成**（51 例；203 全绿，含扫码登录） |
| C 界面 | T14 + T14b | 同上 + 工坊真机截图 | **完成**（真机截图待 T17） |
| D 发布 | T15–T17 | `package.ps1` 四个包 + `build-all.ps1` + 真机一轮 | 未开始（**需要网络与桌宠进程**） |

> **D 为什么还没做**：T15 会把新包写进 `market/index.json` 与宿主的内置源（`internal/plugins/sample/`），
> 而 T17 要求先用一个真 BV 号跑通三步接口。没有网络就打包，等于把一个「从没对真接口跑过」的包
> 塞进离线兜底目录。顺序不能反。

---

## A 宿主（`internal/plugins` + `app_plugins.go`）——已完成

### T1 权限词表加 `net.credentials` ✅

- **改**：`internal/plugins/capability.go` —— 常量 + 文档注释（照 design §2.1 的措辞）、
  `AllowedPermissions` 追加（`net.fetch` 之后）、`PermissionLabels` 追加；文件头「The fourteen permissions」改「fifteen」。
- **判据**：`TestAllowedPermissionsIsTheFifteen` 过；标签非空；顺序稳定（`net.fetch` 之后）。

### T2 `pluginMethod.also` + 完整性测试并集 ✅

- **改**：`app_plugins.go` 的 `pluginMethod` 加 `also []string` 与注释；`pluginMethods` 的
  `net.fetch` 填 `[]string{plugins.PermNetCredentials}`；`app_plugins_test.go` 的
  `TestCapabilityTableIsComplete` 把 `also` 并入 `used`。
- **注意**：`pluginMethods` 现有 29 条都是**位置字面量**（`{perm, run}`），加字段会让它们一次性编译失败。
  统一改成带字段名的写法（`{perm: …, run: …}`）而不是补 `nil` —— 可读，而且下次加字段不会再全表重排。
- **判据**：删掉 `also` 那一项时该测试**必须失败**（先手工验证一次，再恢复）。

### T3 `net.fetch` 的凭据注入与受控 `referer` ✅

- **改**：`pluginNetFetch` 的入参结构加 `Credentials` / `Referer`；判定顺序照 design §2.1（白名单 → 权限 →
  声明为 secret → 明文非空 → 注入）；`referer` 解析 + 主机白名单。**不**改 `netFetchHeaders`。
  主机白名单的计算**上提为一次**（URL / Referer / 每跳重定向共用），三份拷贝就是三次走样的机会。
- **判据**：T3 的测试全绿；错误文案含 key 名、不含值；重定向用例里第二跳没被访问。

### T4 manifest 的配对校验 ✅

- **改**：`internal/plugins/manifest.go` 新增 `validateCredentials`（`net.credentials` ⇒ `net.fetch`），
  紧随 `validateNetHosts` 调用（同一族规则放在一起）；`manifest_test.go` 加 `TestManifestCredentialsRequiresNetFetch`。
- **判据**：只声明 `net.credentials` 的清单解析失败且文案点名两个权限。

### T5 `background` 接受 `net.fetch` ✅

- **改**：`validateBackground` 加第三类 + 注释；错误文案更新；
  **`frontend/src/main.js` 的 `backgroundPlugins()` 同步加 `net.fetch`**（这条原计划漏了：Go 的校验只决定
  「清单合法」，真正决定「帧挂不挂」的是前端那个谓词，只改一边会得到「装得上但永远不跑」）；
  `plugin-host.js` 的说明注释；`.dev/check-frontend.cjs` 的 14 → 15、常驻帧选择器加第三支。
- **判据**：背景三态测试过（`events` / `net.ws` / `net.fetch` 各能、三者皆无仍拒）；门禁全绿。

### A 的实测记录（本轮真跑）

| 门禁 | 结果 |
|---|---|
| `.dev\go.ps1 vet ./...` | 通过 |
| `.dev\go.ps1 test ./... -count=1` | 通过；**唯一失败**是 `internal/desktop` 的自启动用例（`open Run key: Access is denied`）——沙箱不放行 HKCU 注册表；放宽一次后 `internal/desktop` `ok`，确认与本次改动无关 |
| `tools/plugin-market` 的 `go test ./...` | 通过 |
| `node .dev/check-frontend.cjs` | **169 项全过**（新增「net.credentials 只与 net.fetch 同现」） |
| `frontend` 的 `npx vitest run` | 71 项全过 |
| `tools/plugin-market/frontend` 的 `npx vitest run` | 34 项全过（含 15 项权限标签断言） |
| `node .dev/run-frontend-harness.cjs` | 147 项全过 |
| `market` 的 `npx vitest run` | 152 项全过（三个既有插件未受影响） |

### 环境注意（下一轮省时间）

- Go 命令必须带 `$env:GOCACHE='F:\pet_pc\zz-gocache2'`：默认缓存目录 `%LOCALAPPDATA%\go-build` 在沙箱里被拒，
  而 `.dev\gocache` 里的旧条目也读不了（ACL 来自别的用户）。`.dev\gocache2` / `zz-gocache2` 是可写的那个。
- `internal\plugins\` 目录**不允许新建文件**（gofmt 的临时文件会被拒），但**允许改已存在的文件**。
  所以 `gofmt -w` 对那个目录会失败：先 `gofmt -d` 看差异，再用编辑器把对齐手动改过去（本轮 `capability.go`
  的 map 对齐就是这样修的）。`app_plugins.go` / `app_plugins_test.go` 在仓库根，`gofmt -w` 正常。
- 三套 vitest / harness 都要 Node 子进程（esbuild），受限沙箱下 `spawn` 报 `EPERM`：需要放宽一次，
  或改成不走 vitest 的检查方式。这是沙箱边界，不是测试坏了。

---

## B 插件内核（`market/plugins/com.shixiansi.plugin-bilibili/`）

### T6 骨架：manifest + 桥 + 存储 + 主题 ✅

- **改**：`manifest.json`（31 项 config、7 项权限、7 个 netHosts、`background:true`、`entry: panel.html`）、
  `panel.html`（两个角色：`background` 只跑不画 / `workshop` 画全部）、`style.css`（深浅 token）、
  `app.js` §0–§2（常量、桥、存储）。
- **判据**：`package.ps1` 能打出 zip（T15 做）；装进市场后工坊能打开、`init` 之后显示状态；`?theme=light` 首帧生效。
- **两处实现判断**：
  ① **不写 `shareLLM`**：它表示「本插件的模型配置可以借给别人」，而本插件是**借入方**（`llm.borrow`）。
     原计划里写了它，是抄 QQ 插件抄错了方向。
  ② **`llmApiKey` 必须排在 `biliCookie` 之前**：宿主把配置里**第一个 secret** 当作 `llm.chat` 的凭据
     （`pluginSecretItem` 取第一个 `secret: true`）。顺序反了就是把 B 站 Cookie 当 API Key 发出去，
     而报错会出现在模型端点那一侧，指向完全错误的地方。这条有成文测试钉住。

### T7 B 站客户端：md5 / wbi / 限速 / 错误码 ✅

- **改**：`app.js` §3。
- **判据**：md5 已知向量正确（含中文走 UTF-8）；`encWbi` 与服务端重算一致；限速器串行；`-352/-412` 触发 `pausedUntil`。
- **实现时的两处判断**：
  ① `wbiKeys` 走 `nav` 时**不带凭据** —— `nav` 对匿名调用也给 wbi key，而要求登录才签名会让
     「没登录 = 所有字幕都拿不到」，那几乎是所有人；
  ② 所有接口错误统一成 `BiliError{code, message, hint}`，风控码（`-352/-412/-509`）单独判，
     其余（含 `-404`）是「这条视频的事」，不是「这一轮的事」。

### T8 候选与本地初筛 ✅

- **改**：`app.js` §4（四个来源 + 六条过滤 + 排序取 N）。
- **判据**：已看/黑名单/时长/播放/关键词各一例；`upWhitelist` 非空时白名单外全过滤；候选不足 N 条不报错。
- **实测**：4 例（两种字段形状 / 非 BV 丢弃 / 六条规则各自计数 / 排序）。四个来源的**请求**只在集成测试里
  走过 `ranking` 与 `view`，`region`/`search`/`space` 仍未对真接口跑过（见 T17）。

### T9 取字幕三步 + 降级 ✅

- **改**：`app.js` §5（`view` → `player(wbi)` → 下载 → 选轨 → 清洗/切块；`desc` 降级）。
- **判据**：选轨顺序（zh-CN → zh-Hans → ai-zh → 其它中文 → 首条）；`//` 前缀补全；`need_login_subtitle`
  与「无轨」都走降级且带「非字幕」标记；1 MiB 截断按「跳过 + 日志」。
- **实现时改了一处判据**：`player` 调用失败（含 -404）**不再中止这条视频**，而是记下原因走降级 ——
  否则「这条视频没有字幕轨」会表现成「这条视频处理失败」，而它其实能出摘要。
  只有 `-352/-412/-509` 才中止整轮。

### T10 摘要：map / reduce / 解析 / 预算 ✅

- **改**：`app.js` §6（`buildSystem`、`chunkNote`、`parseDigest`、单遍与 map-reduce）。
- **判据**：6000 字字幕 → 调用次数 ≤ `summaryMaxCalls`；块数超预算时卡片标「只读了前 X 分钟」；
  `parseDigest` 能吃代码块包裹与前后噪声；解析失败 → `score 0` 且不推送。
- **实测**：`planDigest` 单独可测（调用数与 clipped 标志），因此预算不需要真的发 6 次请求来验证。

### T11 四道闸 + 调度器 ✅

- **改**：`app.js` §7、§10（纯函数 `decidePush` / `decideComment`；`tick` 每 60s；`nextRunAt` 落盘；
  跨零点 `activeHours`；启动 90s 后补跑）。
- **判据**：每道闸各一例（分值/上限/时段/间隔），`reason` 文案进日志；休眠恢复后不连跑两轮。
- **实现时的两个判断**：① 时段**读不出来**（`09:00-23:00` 写成中文）按「不设时段」处理 —— 静默全天关闭
  比静默全天运行更难被发现；② `throttle` 的间隔读 `state.minGapMs`，只为让测试把它归零，
  成品里没有任何代码写这个字段。

### T12 推送：气泡 + 收件箱 ✅

- **改**：`app.js` §8（气泡 ≤ 40 字；收件箱写盘与 200 条上限；本轮最多 1 次气泡）。
- **判据**：同一轮两条命中只弹一次；收件箱写入含 `bvid/score/why/summary/points`；超限按时间淘汰。
- **实测**：一轮 3 条 → 1 个气泡、3 张卡、计数 `videos=3 / pushes=1` 落盘；第二轮零 LLM 调用（去重）。

### T13 评论子系统 ✅

- **改**：`app.js` §9（`draft`/`auto`、六项本地检查、`replyAdd` 请求形状、风控停机、`rpid` 回填）。
- **判据**：`commentEnabled=false` → **零 POST**；`draft` → 只写卡片；`auto` → 一次 POST，`csrf` 在表单体、
  Cookie **不在**请求体（断言 payload 里没有 `biliCookie` 的值）；`-101` → 标记未登录并停用评论。
- **实测**：三条都写成了测试；`ENDPOINTS` 漏掉 `replyAdd` 正是被「auto 只发一次」这条抓出来的。

---

## C 界面

### T14 工坊界面全量 ✅（桌面真窗口待 T17）

- **改**：`panel.html` + `app.js` §11 + `style.css`：状态条、登录卡片（整串 Cookie 拆分 → 两次 `config.set` →
  `nav` 校验 → 显示 UID/昵称）、单条调试、收件箱分页（20/页）、日志（50 条）、隐私说明。
- **判据**：`[hidden]` 权威；样式无 `box-shadow/filter/transform`；「发送」是描边按钮且与「删除」分开摆；
  深浅两套主题在真窗口都读得清；收件箱 200 条不卡（分页读）。
- **实现时的结构性改动**：工坊是**另一个进程**里的页面，市场程序不实现 `llm.chat` / `bubble.say`，
  它那份 `net.fetch` 也不带请求头与凭据。所以按钮改成**写命令**、由桌宠的常驻帧执行
  （design §9.1）：`cmd` 单槽、3 秒轮询、120 秒过期，工坊 5 秒回读 `state/inbox/usage/log/debug`。
  文案如实写成「已请桌宠…」，并在桌宠没运行时说明只能看和改配置。

---

## D 发布

### 真机验证（T15 之前先做的这一步）——**已执行：真机跑通，并抓到两个问题**

本机试装（`%AppData%\PetPC\plugins\com.shixiansi.plugin-bilibili\` + `plugins.json` 里追加一条，
`granted` 与 manifest 一致、`enabled: true`；旧文件已备份成 `plugins.json.before-bilibili`），
重编桌宠（`tools\build-all.ps1`，前端要含新的 `net.fetch` 常驻帧判定），启动它，然后用**插件自己的命令通道**
投递真 BV 号。

**① 真机链路（两条视频）**

| 步骤 | 结果 |
|---|---|
| 常驻帧 | `plugin-data/<id>/log` 出现「常驻帧起来了（1.0.0）」→ **`background` + `net.fetch` 这条新分支真的挂了帧** |
| 命令通道 | 同一帧写出「命令通道就绪」→ `storage.get/set` 通，工坊按钮那套协议成立 |
| `view` | `cid 29158869842 · 13 分钟 · UP 赛博食录` / `cid 29161818466 · 6 分钟 · UP 队友去质器` |
| `player`（wbi 签名，经 Go 宿主） | `code 0`，**0 条轨**，卡片写明「B 站对未登录请求不返回字幕列表」 |
| 摘要 | 走「简介+热评」降级，**1 次 `llm.chat`**（经宿主借用了对话插件的模型），分数 65，摘要自己写明「素材是标题、简介、标签与热评，不是逐字稿」 |
| 评论 | `draft` 里是一条引用了热评具体点的句子；`comment` 为 null（没发） |

也就是说：**从按钮协议、宿主网络、真实接口、模型、到卡片落盘，整条链路在真机上通了**，
唯一没验的是「有字幕」那半条 —— 它需要贴凭据，属于用户账户动作，只能由主人自己在登录卡片里做。

**② 真机抓到的两个问题（都已修）**

1. **匿名 `player/wbi/v2` 一律 0 轨**（20/20，跨 4 个分区），`need_login_subtitle` 已不在返回里；
   而 `nav` 用 `code: -101` 报未登录的同时**带着** `data.wbi_img`。原实现「非零码即失败」会让
   `wbiKeys()` 直接抛错 → **所有字幕对所有未登录用户全废**。现在 `allowCodes` 放行 `-101`，
   「没有字幕轨」也按登录态分成两种说法。详见 design §5.1。
2. **`logLine` 会丢最后一行**：写盘在飞时新来的行只进内存、不补写，于是「命令通道就绪」永远不落盘。
   真机上表现成「日志停在启动那一行，看起来像插件死了」。改成 `logDirty` 循环，写盘期间到达的行会被接着写。

**③ 一条尚未证实的怀疑（记为已知限制）**

第一次投递的命令在 30 秒内没被执行，重启桌宠后同一投递 4 秒内完成。最像的解释是
**WebView 在窗口被遮挡时节流/冻结了 iframe 的定时器**（Chromium 的常规行为），但本轮没有做对照实验，
所以只记为怀疑。缓解：命令 120 秒过期不补跑、工坊可以重复点击、`tick` 周期本来就粗（60 秒）。

> 这一步为什么值：单元测试全绿、静态门禁全绿、194 项行为测试全绿 —— 而**产品最核心的一步在真机上
> 是空的**。它的表现是「每条视频都走降级」，那看起来像一个保守的策略，而不是一个 bug。

### T14b 扫码登录（追加要求，已完成）✅

- **改**：`app.js` §3.5（二维码编码器：byte 模式 / M 级 / v1–12，含掩码罚分与格式位）+
  §10（`qr-start` / `qr-cancel` 命令与轮询回路）+ §11（两个登录页签、`<svg>` 画码）+
  `panel.html` / `style.css` / `manifest.json`（`passport.bilibili.com` 进 `netHosts`）。
- **判据**：矩阵与参考实现逐位一致；生成→已扫码→成功三段状态都能上屏；成功后
  `biliCookie` 与 `biliJct` 由常驻帧写进 `config.set`，页面拿不到明文；过期/取消都会停轮询。
- **两个发现**：
  ① 参考库会按内容自动选模式（`HELLO` → 字母数字模式），夹具必须强制 `mode:'byte'`
     才能与我们的编码器比对（定位过程见 design §9.2）。
  ② **「不需要动宿主」这句话是错的**：第一版按「凭据在 `data.url` 里」实现，真机扫成功后
     `url` 是空的、会话在 `Set-Cookie` 里。补了 `net.fetch` 的 `readCookies`（需 `net.credentials`，
     只回 `name=value`，属性在 Go 侧丢掉，请求前先查权限），并重编桌宠。见 design §2.4b。
- **真机**：
  · 第一次扫（旧实现）：`qrcode` 报告 `status: error`「登录成功，但返回里没有凭据」→ 由此定位。
  · 修好后：`qr-start` 拿回真的 `qrcode_key`，轮询带 `readCookies` 不报错。
  · **终态仍待主人再扫一次**（凭据落盘那一步只能真扫）。

### T15 打包与内置源

- **改**：`market/tools/package.ps1` 跑一次（自动纳入新插件）；`internal/plugins/sample/embed.go` 的
  `//go:embed` 加 `com.shixiansi.plugin-bilibili-1.0.0.zip`；`market/index.json` 由脚本回写。
- **判据**：四个 zip；**旧三个包的 sha256 逐字不变**；`go build ./...` 通过（漏列 zip 会编译失败）。

### T16 文档

- **改**：新增 `docs/bilibili-plugin.md`（面向使用者：配置项含义、Cookie 从哪来、评论风险、数据在哪）；
  `README.md` 的插件清单加一行；`proposal.md` §5 已按 31 项回填（已完成）。
- **判据**：文档里的默认值与 `manifest.json` 逐项一致（有一处 drift 就算没过）。

### T17 全量门禁 + 真机一轮

- **跑**：`.\.dev\go.ps1 vet ./...` → `.\.dev\go.ps1 test ./... -count=1` → 两个前端 `npm run build` →
  `node .dev/check-frontend.cjs` → `node .dev/run-frontend-harness.cjs` → `market` 的 `npx vitest run` →
  `pwsh -File tools/build-all.ps1`。
- **真机**：装插件 → 贴 Cookie → 「单条调试」跑一个真 BV 号 →（网络允许时）等一轮自动刷 → 看一眼收件箱。
- **判据**：全绿；停用插件后 30 秒内零请求（抓 `debug.log` 或宿主计数）。

---

## 不做（避免范围漂移）

- 扫码登录、响应头回传（P3，要再动宿主）。
- 关注流 / 稍后再看作为来源、`todos.write` 联动。
- 弹幕、ASR、视频下载、点赞投币收藏、多账号。
- 系统通知、B 站私信、QQ 转发。
