# tasks.md — bili-qq-bridge

> 上游：`proposal.md`（需求，Q1–Q6 已拍板）。顺序即依赖：先宿主、后 B 站侧、再 QQ 侧、最后真机。

| 阶段 | 任务 | 状态 |
|---|---|---|
| A 宿主 | T1–T3 | **完成**（`go vet` / `go test` 全绿；新增 4 个测试函数） |
| B B 站侧 | T4–T5 | **完成**（59 例插件测试全绿） |
| C QQ 侧 | T6–T8 | **完成**（新增 11 例桥测试；QQ 插件共 63 例） |
| D 真机 | T9 | **链路验到「出文件为止」**；群里那一步等协议端 |

> 想用起来只差三件事，全在 QQ 插件的设置里：填 `ffmpegPath`（本机是
> `D:\ffmpeg\ffmpeg-master-latest-win64-gpl\bin\ffmpeg.exe`）、打开 `biliLinkReply`、
> 想让推荐也进群就填 `biliRecommendGroup`。协议端在跑之后，群里发一条 BV 链接即可。

---

## A 宿主（`internal/plugins` + `app_plugins.go`）

### T1 `plugin.share`：跨插件托盘 ✅

- **改**：`capability.go` 加 `PermPluginShare`（词表 15 → **16**）+ 标签；`manifest.go` 加
  `shareRead []string` 与 `validateShareRead`（必须有 `plugin.share`、必须是 `<id>/<name>`、
  不许列自己、最多 8 项）；`app_plugins.go` 加 `share.set` / `share.get` / `share.remove`。
- **判据**：自己写自己读；别人的键必须 manifest 点名，否则拒绝且报错点名那一条；
  64 KiB 上限；读自己的未写值是空值不是错误；`TestShareCapabilities` + `TestManifestShareRead` 覆盖。
- **实现选择**：值存在**发布者自己的私有目录**里（`share-<name>`），所以「只能写自己名下的」
  由文件系统保证，而不是靠字符串检查。

### T2 `process.state`：知道子进程结束了没有 ✅

- **为什么需要**：`process.launch` 原来 `Process.Release()`，等于把「结束了吗」这条信息扔了，
  而 ffmpeg 下完没下完正是 QQ 侧必须知道的（插件没有文件系统能力，戳不到产物）。
- **改**：改成 goroutine 里 `cmd.Wait()`（仍然不阻塞请求、仍然不持有父进程生命周期），
  记进 `pluginHost.children`（带 **owner 插件 id**，pid 可猜但看不到别人的），
  新增 `process.state {pid}` → `{known, running, exitCode, ranMs}`。挂在**既有** `PermProcess` 上，词表不变。
- **判据**：`TestProcessStateTracksExit`（`cmd /c exit 3` → 结束且 `exitCode 3`；另一个插件查同一个 pid → `known:false`）。

### T3 `storage.dir`：告诉插件它自己的目录 ✅

- **为什么需要**：插件要把输出路径交给 ffmpeg，也要把文件路径交给协议端 —— 它此前无法知道自己在哪。
- **改**：`storage.dir` → `{dir}`，**只回调用者自己的**目录，挂在既有 `PermStorage` 上。
- **判据**：`TestStorageDirIsTheCallersOwn`（绝对路径、存在、两个插件拿到不同目录、路径里含自己的 id）。

---

## B B 站侧（`com.shixiansi.plugin-bilibili`）

### T4 取流与画质降级 ✅

- **改**：`ENDPOINTS.playurl`（wbi 签名）+ `qnForDuration` / `estimateStreamBytes` /
  `pickVideoStream` / `pickAudioStream` / `normaliseDashStream` / `resolveStreams`。
- **判据**：阶梯边界照 xunlu（`<120→120, <180→112, <300→80, <480→64, <720→32, 否则 16`）；
  「先按时长选、再按预估体积往下退」两段都测；退到最低仍超 → `ok:false` 而不是硬发。
- **踩到的坑**：`pickAudioStream` 过滤的是 `.url`，而原始 DASH 音频是 `baseUrl` —— 先归一化再挑，
  否则音频永远是 `null`（测试直接报出来的）。

### T5 共享托盘：jobs → results / recommend ✅

- **改**：manifest `shareRead: ["com.shixiansi.plugin-qq/bili-jobs"]` + `plugin.share` 权限；
  `readShared` / `writeShared` / `resolveBvid`（BV 直取，b23.tv 跟一次跳转再正则）/
  `handleShareJob` / `pollShare`（同一个 3 秒 tick，与命令通道共用一个节拍）/ `shareRecommend`。
- **判据**：一条链接 → `bili-results` 里 `ok:true`、`qn 64`、video/audio 地址；超时长 → `ok:false` 且原因含「时长」；
  认不出 BV → 也回一条（不沉默）；**同一个 job 只处理一次**（轮询每 3 秒，不能重复请求）。
- **分工**：这个插件只产出「能下载的流」，**不决定发给谁**；cookie 从头到尾没离开它。

---

## C QQ 侧（`com.shixiansi.plugin-qq`）✅

### T6 配置与声明 ✅

- **改**：`manifest.json` 加 6 项（`biliLinkReply` / `biliLinkAtOwner` / `biliMaxMB=80` /
  `biliMaxMinutes=25` / `biliRecommendGroup` / `ffmpegPath`）+ `plugin.share` 权限 +
  `shareRead: ["…bilibili/bili-results", "…bilibili/recommend"]`。合计 **32 项**，正好顶到上限。
- **判据**：`manifest.config.length <= 32` ✓；「第一个 secret」仍是 `llmApiKey` ✓（`plugin-qq.test.js` 已有断言）。

### T7 群消息里的链接 → 请求解析 ✅

- **改**：`onGroupMessage` 在 `touchGroup` 之后、`shouldAnswer` **之前**加钩子（不 await，
  免得一次远端的往返拖慢真正的回答）；`biliLinkIn` 认 BV 与 b23.tv；同一条链接 5 分钟内不重复；
  `biliMaxMB` / `biliMaxMinutes` 随 job 一起送过去（**谁发谁定上限**）。
- **判据**：开关关→零共享写；开→写 `bili-jobs` 且带两个上限；重复链接不重复请求；`groupAllow` 外的群不写。
  （`plugin-qq-bili.test.js` 4 例）

### T8 结果 → ffmpeg → 发视频（或降级发链接）✅

- **改**：`pollBridge`（5 秒，独立于 WebSocket 节拍）、`onBiliResult`（结果里没有群号，
  靠本地 `biliPending` 记住它属于哪个群）、`runFfmpeg`（`storage.dir` → `process.launch` →
  `process.state` 轮询 ≤90 秒）、`sendBiliVideo`（@ + 文字 + `file:///` 视频段）、
  `notifyGroupThrottled`（同群 5 分钟不重复）、`announceRecommend`、产物用 `storage.remove` 清掉。
- **判据**：起 ffmpeg 的参数形状、`@` 只 @ `ownerQQ`、没配 ffmpeg → 只发链接并说明、
  结果 `ok:false` → 回一句原因、ffmpeg 非 0 → 改发链接、同一个结果只处理一次。
  （`plugin-qq-bili.test.js` 7 例，共 11 例）

---

### T11 共享值的体积上限（真机抓到的静默丢件）✅

真机日志：`写共享值 bili-results 失败：share 的值超过 65536 字节：73523` —— 每条答复都带着
**三个备用直链**（每条 1–3 KB），攒到 20 条就顶穿宿主的 64 KiB 上限，于是**整条写入被拒，
最新的几条答复根本没送到 QQ**（表现是「发了一串链接，只有前几条有反应」）。三处一起修：

1. 答复里只带**主地址**（消费方从没用过备份直链）；
2. 保留条数 20 → **10**；
3. `writeShared` 加 `trimShared`：超限时**从最旧的开始丢**直到装得下，并记一条 warn ——
   宁可少几条历史，也不能让最新那条写不进去。

真机复核：三条答复共 **4948 字节**（此前三条就 7 万+）。

### T12 配置改成选择器（用户：「谁知道 36 是知识」）✅

给宿主加了第五种控件 `select`（可 `multiple`），清单直接列 `{value,label}`，市场画下拉/复选框组，
存进去仍是逗号分隔字符串。B 站插件改了 4 项：分区（22 个）、来源（4 个）、摘要长度（3 个）、
评论模式（2 个）。详见 `docs/plugin-market.md` §2.5。

---

### T13 字幕取不到（用户：「怎么都是这样好像获取不到字幕一样」）✅

真机卡片全是「非字幕」。原因和 `playurl` 是**同一类错误**：`player/wbi/v2` 也是匿名请求的
——匿名拿不到字幕轨列表（早先实测 20/20 条都是 0 轨）。三处一起修：

| 改动 | 说明 |
|---|---|
| `fetchSubtitleTracks` 带 `credentials` | 真机复核：`player: ok 2 条轨（ai-zh/ai-en）`、`subtitle: ok 8 段 → 4 块 · 4194 字`、`transcript=True` |
| `summaryMaxCalls` 默认 4 → **6** | 13 分钟的视频有 4 块，配 4 次预算必然少读一块（卡片上写着「只读了前半段」）。6 次 = 4 段 map + 1 次 reduce，读全 |
| `DEFAULTS` 与 manifest 逐项对齐 + **新测试** | 改了清单默认值却忘了 app.js 里的镜像，于是「用户没动过」时用的是旧值——真机上 `6` 又变回 `4`。新测试逐项比对两张表，任一处漂移就失败 |

**还清了一个重启浪费**：QQ 的 job 列表是持久的，而 B 站插件的「已处理」只在内存里，于是**每重启
一次就把旧 job 全部重新解析一遍**（日志里一串「给 QQ 的答复」）。现在超过 10 分钟的 job 直接跳过
（QQ 那边的去重窗口是 5 分钟，更旧的一定是上一次会话的）。

---

### T14 视频显示「已过期」（用户反馈）✅

真机日志：`已发出视频 BV1iKp16LE1a（uri=file://C:/…/bilivideomvkyt182）` —— **发送本身是成功的**，
但群里那条视频显示「已过期」，而那个文件**已经被删了**（旧逻辑：发送成功即 `storage.remove`）。

结论：协议端的上传是异步的，API 返回 ok ≠ 文件已经读完；我们删得太早。

| 改动 | 说明 |
|---|---|
| 改成「记账 + 延迟清理」 | 发完记 `{key, at}` 到 `bilivideo-files`，由**下一次轮询**删掉超过 10 分钟的；失败也记账（留着给人手动发） |
| 正文里加链接 | 媒体万一还是过期，群里至少还剩一个能点的地址 |
| 扩展名不是原因（已排除） | 用不存在的路径探过三种：`bilivideoxxx` / `.mp4` / `.txt` 都只回 `File not found` —— 协议端不按扩展名判类型，所以「无扩展名 + `-f mp4`」可以保留（这样 `storage.remove` 才删得掉） |

---

### T15 「请求参数不对」的噪声（用户反馈：群里多出一条失败）✅

真机日志：`已请求解析 BVlkbstVNrVJ` → `失败（请求参数不对：请求错误）`。`BVlkbstVNrVJ`
**不是任何视频的 BV 号** —— 它是从卡片负载的 base64/URL 噪声里被正则蒙出来的
（`/BV[0-9A-Za-z]{10}/` 在随机串里也能命中）。于是群里多了一句「这条视频没法直接发」。

两层一起修：

| 层 | 改动 |
|---|---|
| 提取 | 两侧正则都加词边界：`(?<![0-9A-Za-z])BV[0-9A-Za-z]{10}(?![0-9A-Za-z])` —— 粘在更长的字母数字串里的不算 |
| 兜底 | `view` 回 -400（请求错误）时把结果标成 `notVideo: true`；QQ 侧看到 `notVideo`（或「没认出 BV 号」）**只在日志里记一句**，不再往群里发 |

真机验证（同时投一个假 BV 和一个真 BV）：

```
noise1: ok=False notVideo=True  bvid=BVlkbstVNrVJ  原因=这不是一个能打开的视频
real1 : ok=True                bvid=BV1w6ZEYrEsb  画质=720p
```

---

### T16 查协议端日志（用户：「看一下 yogurt 的日志」）✅

Yogurt 原本是裸启动的，DEBUG 日志只在控制台里，读不到。经用户同意重启了一次，改成
`yogurt.exe > yogurt.log 2>&1`（顺带把空着的 `milky.ffmpegPath` 填上，原配置备份成
`config.json.bak`）。日志证明**视频链路是健康的**：

```
21:11:32 DEBUG MessageTransform 视频 file://… 信息：640x360，时长 792 秒
21:11:50 DEBUG FlashTransfer 上传进度: 100% / Highway 上传进度: 100%
21:11:52  INFO 调用 API /send_group_message（成功 20316ms）
21:11:53 DEBUG [纳西妲] [测试] 视频发送链路探测[视频 640x360 792s]
```

发出去的消息结构也完整：`resource_id` / `temp_url` / `width` / `height` / `duration` 齐全，
和群里真人发的视频同构。**对照实验**：13 分钟（792 s）和 10 秒各发一条，用户确认**两条都能播** ——
所以 QQ 侧没有撞上时长上限，`biliMaxMinutes = 25` 不必改；此前那个「已过期」就是 T14 里
「删文件太早」造成的，修完即好。

顺手加了一处排查设施：发送成功的日志里带上协议端返回的 `message_seq`，两边日志能对上号。

---

### T17 判据从「时间」改成「体积」（用户：「通过下载时间判断不合理」）✅

原来 `runFfmpeg` 里写死 90 秒：超时就放弃合并、改发链接。59 MB 的 720p 在普通家宽下要两三分钟，
于是必然超时——用户看到的就是「下载超过 90 秒」。用户指出：**解析时已经能预估体积，用时间判断不合理**，
并点名 xunlu-core 有对应实现。去看了一眼，它确实是按字节判断的：

```js
await download.downloadFile(videoUrl, videoRelativePath, { headers, maxBytes })   // 按字节封顶
const resultSize = fs.statSync(resultPath).size                                   // 真实体积
if (resultSize > BILIBILI_VIDEO_MAX_RESULT_BYTES) throw createVideoTooLargeError(...)
```

我们的沙箱读不到文件大小，所以给宿主补了一个方法：

| 改动 | 说明 |
|---|---|
| 宿主 `storage.stat` | `{key} → {exists, bytes}`，仍走 `^[A-Za-z0-9_-]{1,64}$` 校验、仍只看自己的私有目录；文件还没出现时返回 `exists:false`（这是「正在下载」的正常答案，不是错误） |
| 插件判据 | ffmpeg 退 0 之后查真实体积：`0` → 空文件；`< 预估的一半` → 没下完；`> 上限 ×1.05` → 超限。三种都改成发链接并说明原因 |
| 时间只剩兜底 | `ffmpegBudgetMs` 按预估体积给（最小 90 秒、最大 15 分钟），**只用于卡死**；措辞也改成「ffmpeg 超过 N 秒还没结束」 |

---

### T18 CDN 连接卡死（用户：「这还是不行啊」）✅

用户报 `BV19Dny6BEZp 没发成视频 —— ffmpeg 超过 94 秒还没结束（预估 24 MB）`。现场一看，**不是慢，是卡住**：

| 证据 | 值 |
|---|---|
| 那次产物文件 | `bilivideo…` = **0.00 MB**（两分钟一个字节没写） |
| 那次 ffmpeg 进程 | 还在跑，**CPU 只用了 0.40 秒** → 一直阻塞在网络读 |
| 同一视频前一次 | `bilivideo…` = 23.64 MB **成功**（21:20） |
| 同一条直链手动重跑（加超时参数后） | **5.9 秒下完 23.64 MB**（约 4 MB/s） |

根因：**ffmpeg 默认的网络读超时是无限的**，死连接就一直等下去，而我原来唯一的判据是「等多久」。

| 改动 | 说明 |
|---|---|
| `-rw_timeout 15000000` | 15 秒读不到数据就失败（死连接 ≠ 慢连接） |
| `-reconnect 1 -reconnect_streamed 1 -reconnect_delay_max 3` | 断流由 ffmpeg 自己重连，不整个任务失败 |
| 进度看门狗 | 每 2 秒查一次 `storage.stat`；**45 秒体积没涨**就认定这条连接废了，不再干等 |
| 换节点重试 | B 站答复里每个流带**一个**备用地址（三条会顶穿 64 KiB，见 T11）；主地址失败就换备用 CDN 再试一次 |
| 记账 | 每次尝试的产物都进清理清单（失败的那次可能是 0 字节或半截） |

真机复核：答复 3201 字节、`video.backups` / `audio.backups` 各 1 条 —— 既够重试，又远低于上限。

---

### T19 分工：curl 下载，ffmpeg 只合并（用户：「ffmpeg 不是只用负责合成视频吗」）✅

上一轮为了修「连接卡死」，我给 ffmpeg 加了 `-rw_timeout` + 三个 `-reconnect*`，两条输入各一份 ——
结果命令变成 **38 个参数**，直接撞穿宿主的 32 个上限，群里收到的是
`process.launch 的 args 最多 32 个，收到 38 个`。用户的判断是对的：**这活儿本来不该 ffmpeg 干**。

xunlu-core 的分工是 `downloadFile(...)` → `composeVideoFile(...)`，我们也照这个拆开：

| 谁 | 干什么 | 参数个数 |
|---|---|---|
| `curl.exe`（系统自带） | 下载视频流、音频流。`--max-time 900` 兜总时长、`--retry 2` 处理瞬时错误、`-A` 诚实 UA、`-e` Referer | 18 |
| `ffmpeg` | **只**把两个本地文件合并（`-map 0:v:0 -map 1:a:0 -c copy -f mp4`） | 15 |

好处不只是参数少：网络问题的表现变成 curl 的明确退出码（6/7 连接、28 超时、22 HTTP 错），
合成变成纯本地 I/O（3 分钟兜底足够），中间文件用完即删，产物按体积验收（T17）。

真机实测同一条 23.6 MB 的流：

```
curl 视频 3.9 s / 19.25 MB
curl 音频 5.8 s /  4.29 MB
ffmpeg 本地合并 exit=0 → 23.64 MB（预估 23.6 MB ✓）
总用时 6 秒（旧版本这里还在等 90 秒超时）
```

---

### T20 正文里的链接会被别的机器人拿去解析（用户：「发出的视频好像会触发其他机器人的 b 站视频解析」）✅

T14 为了「媒体万一过期也留个能点的地址」，我把 `https://www.bilibili.com/video/BV…` 放进了消息正文。
群里还有别的「B 站链接解析」机器人，于是我们发一条视频、它跟着发一张卡片。群历史把它记全了：

```
45762 [纳西妲] 【B 站】…UP：活跃黑江乐 · 3 分钟 · 1080p
              https://www.bilibili.com/video/BV…     ← 我塞的
45763 [何沁]   [图片] 识别：哔哩哔哩，🥳kkr：…          ← 它解析了
45768 [群友]   大肥鱼给视频里塞链接了                    ← 群友的原话
```

改法：**成功发视频时正文里不放任何 URL**；链接只出现在「发不出视频」的说明里（那时它是唯一交付物）。
测试钉住两头：成功那条消息 `not.toContain('bilibili.com')`，失败说明里必须有链接。

---

### T21 内容审核拒绝被当成「摘要失败」（用户：「这是什么错误」）✅

工坊日志里两条看起来像 bug 的行，其实是**同一件事**：

```
小说推荐：服务端返回错误：The request contains sensitive content. Please modify your input and try again.
【性转小说】修仙雌堕之后…：模型没有返回可解析的摘要
```

那条视频的标题/简介带「性转/雌堕/鼎炉」这类词，**输入**就被模型服务商拦了；第二条则是模型
只回了一句「无法协助」，于是 JSON 解析失败。两者都不是网络问题，重试也没用。

改法：把「内容被拒」从「失败」里分出来。

| 位置 | 行为 |
|---|---|
| `chatOnce` | 错误信息命中 `sensitive content` / 审核 / 敏感 等 → 抛带 `contentRejected` 的错误；回复正文像拒绝（「抱歉」「无法协助」且没有 `{`）→ 同样处理 |
| `processVideo` | 捕获它 → 步骤写 `跳过：内容被模型服务拒绝…`，抛带 `skip` 的错误 |
| 轮次循环 | `skip` 只记一行 `info`（`跳过 —— …`），不算 error、不推送、不占推送额度 |

这样日志里读到的是一句人话，而不是服务商的英文报错。

---

### T22 「次次都是同一批」（用户反馈）+ 主页短链 ✅

用户：「这视频选取不对吧，怎么次次都是一样的」。日志给出了算式：

```
候选 322 条 → {"dup":0,"seen":9,"own":0,"up":0,"duration":90,"play":19,"keyword":200} → 取 3 条
```

三个原因叠在一起：

1. **失败的视频从不记账**。`rememberSeen` 只在成功时调用，于是被内容审核拒掉的那几条、
   每轮都被重新抓一遍 —— 这就是「次次一样」。
2. **`keywordsInclude` 身兼两职**：`source` 含 `search` 时它是**搜索词**，同时又是标题过滤词。
   用户填了「绘画，小说」，于是每轮都在 B 站搜「小说」，捞回来的正是小说推广
   （也正是被审核拒的那批）。
3. 过滤器再把 322 条压到 3–4 条：keyword 掉 200、duration 掉 90、play 掉 19。

| 改动 | 说明 |
|---|---|
| 失败单独记账 | `state.failed`（`{bvid, at, title, reason}`，**6 小时** TTL）+ 持久化到 `failed` 键；`seenSet` 把它并进去。不写 `seen` 是故意的：一次网络抖动不该让一条视频永久跳过 |
| 成功即清失败记录 | `rememberSeen` 顺带把同 bvid 的失败记录删掉 |
| 字段标签说实话 | 「关键词：source 含 search 时它就是搜索词，同时用来过滤；留空=不搜索也不过滤」 |

**顺带修的**：`b23.tv/jJKBcEG` → `space.bilibili.com/3747…`（UP 主主页，不是视频）。
跳转后没有 BV 号、或重定向指向 space 域，都返回 `notVideo` —— QQ 那边只记日志、不往群里发失败。
`space.bilibili.com` 也加进了 `netHosts`，这样能真的跟过去看一眼再下结论。

**测试时钟依赖**：`a round pushes once` 这条用例依赖默认活跃时段，23:05 跑就必然失败
（日志写着「不在活跃时段」）。现在显式钉成 `activeHours: '00:00-23:59'`，与运行时刻无关。

---

## D 真机（T9）——**链路验到「群里收到视频」为止**

已经验证的（本机，真接口 / 真 ffmpeg）：

| 检查 | 结果 |
|---|---|
| 共享托盘（读别人的键要 manifest 点名） | 手写一条 job 进 QQ 的 `share-bili-jobs`，B 站插件在真桌宠里取到并答复 |
| `playurl` + 画质阶梯 | `BV1w6ZEYrEsb`（13 分钟）→ qn 16，预估 39 MB；`BV1EPZEYKEvW`（约 8 分钟）→ qn 32，29 MB |
| ffmpeg 合并 | 用答复里的两条直链跑 `-map 0:v:0 -map 1:a:0 -c copy -f mp4` → **exit 0，39.26 MB**（与预估一致） |
| 发到群里 | `send_group_message` 带 `uri=file://C:/…` → `retcode 0`（群 1061170515 收到） |
| 短链 | `b23.tv/HTPbBxO` → `BV1Zypi6tEy1`（补 `b23.tv` 到 `netHosts` 之后） |
| 画质 | 见下 |

### T10 画质策略（用户追加：不要糊）✅

第一版把 xunlu 的**前半段**（按时长定档）也照抄了，于是「13 分钟一律 360p」。改成只按预算：

| 视频 | 改前 | 改后 |
|---|---|---|
| 13.2 分钟 | 360p（qn 16）39 MB | **720p（qn 64）59.1 MB** |
| 16 秒 | 480p（qn 32）1.8 MB | **1080p（qn 80）8.6 MB** |

两处改动：
1. `pickVideoStream` **不看时长**，按 qn 从高到低挑第一条「预估 ≤ `maxMB`」的，同档优先 `avc1`；
   时长只剩硬上限（25 分钟）这一个作用。
2. `playurl` **带凭据** —— 匿名只给到 480p 左右，这就是「16 秒的视频也只给 480p」的原因。
   凭据是配置项**名字**（`credentials: 'biliCookie'`），cookie 明文没进页面；没贴凭据时退回匿名并记日志。

**真机抓到的两个问题（都已修）**：

1. **宿主给单个参数定的 512 字符上限，短于真实直链。** B 站 CDN 的签名直链有 **643–656 字符** ——
   于是「能起 ffmpeg、却永远指不到东西」。`maxLaunchArgLen` 提到 **4096**，并加了
   `maxLaunchArgsTotal = 16384`（32 × 4096 会超过 Windows 32767 的命令行硬上限，
   只放开单参数等于把一个拒绝换成一次「程序启动了但什么也没做」）。各有一条测试。
2. **浏览器 UA 会被 CDN 403。** 对同一条直链，`ffmpeg` 带 `Mozilla/5.0 … Chrome/124` → **403**，
   带任意非浏览器 UA（`UA-TEST-123`）→ **200**；`curl` 带同一个 Chrome UA 却是 206。
   顺序反过来结果不变，所以不是限流。最像的解释是 CDN 对「像浏览器却没带 cookie」的请求直接拒。
   现在下载用 `PetPC-bili-bridge/1.0` 这个诚实的名字（API 调用仍用浏览器 UA，那边正好相反）。
   顺带发现 `-headers` 在这条路上根本用不了：它的值需要 CRLF，而宿主拒绝含换行的参数 ——
   改用 ffmpeg 自带的 `-referer`。
