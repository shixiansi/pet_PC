# proposal.md — bili-qq-bridge（B 站插件 ↔ QQ 插件联动）

> 一句话：群里出现 B 站链接时，**B 站插件用它的 cookie 解析出可播放的流**，**QQ 插件**把流下载、
> 合成一个 mp4 发回群（可选 @主人）；B 站插件刷到值得看的视频时，也通过同一条路让 QQ 通知主人。

**状态**：初版需求，待你点头。§9 有 6 个待定项，每项带推荐值。

---

## 1. 现在为什么做不到（三条已存在的墙）

| 墙 | 事实 | 后果 |
|---|---|---|
| 插件之间**不能互相调用** | 每个插件有自己的 `<dataDir>/plugin-data/<id>/`、自己的 config/secret；除 `llm.borrow` 外没有任何跨插件通道（`internal/plugins/dispatch.go`） | 「QQ 用 B 站插件的 cookie」这件事没有现成的表达方式 |
| `net.fetch` 响应体上限 **1 MiB**（`app_plugins.go:769`） | 一次请求最多拿 1 MB | 谁也别想用 `net.fetch` 下视频 |
| `process.launch` 是**发射后不管** | 给路径与参数就返回 pid，不等待、不继承 stdio、拿不到退出码（`capability.go` 的 `PermProcess` 注释） | 「下载完再发」只能靠轮询产物文件 |

顺带两条本机事实：**ffmpeg 在**（`D:\ffmpeg\ffmpeg-master-latest-win64-gpl\bin\ffmpeg.exe`），
**yt-dlp / BBDown / you-get 都不在 PATH 上**。所以合成走 ffmpeg 直连 CDN，不引入新依赖。

## 2. 一条新机制：`plugin.share`（跨插件共享键，显式声明）

**为什么不是「借凭据」（`credential.borrow`，仿 `llm.borrow`）**：借了之后 QQ 插件自己就能拿 cookie 打
B 站接口——那等于把「以主人身份发请求」的权力复制一份到另一个插件里，而它并不需要（它需要的是
**一条能下载的流地址**）。而且真要做「解析」，B 站插件里已经有一整套（wbi 签名、错误码、风控退避），
复制一份就是两份会走样的实现。

**做法**：宿主提供一个极小的跨插件键值通道。

- 新权限 **`plugin.share`**（词表 15 → 16），方法 `share.set` / `share.get` / `share.remove`。
- **写自己的前缀随便写**：`share.set({key: "<自己的 id>/<名字>"})` 永远允许。
- **读别人的前缀必须在 manifest 里点名声明**：`"shareRead": ["com.shixiansi.plugin-qq/bili-jobs"]`，
  精确匹配、不支持通配。安装对话框把这几个键列出来，用户看到的是「它会读 QQ 插件的 bili-jobs」。
- 值只收 JSON 字符串，单个 ≤ 64 KB，一个插件最多 32 个键，读到的条目带 `at` 时间戳（消费方自己判过期）。
- 权限本身需要用户勾选：**不勾，两个插件都读不到对方**。

这样跨插件的数据流是**声明式 + 可审计**的：谁写、谁读、写什么名字，三处都写在 manifest 里。

## 3. 两条数据流

### 3.1 群里来了 B 站链接 → 发视频

```
QQ 插件（net.ws 收到群消息）
  ├ 认链接：BV/av 号、b23.tv 短链、带参数的完整 URL
  ├ share.set  com.shixiansi.plugin-qq/bili-jobs  {id, bvid|url, groupID, atOwner, at}
  ▼
B 站插件（常驻帧轮询，每 3 秒 —— 与已有的命令通道同一个节奏）
  ├ view → aid/cid/标题/时长
  ├ playurl（带 cookie，fnval=16 走 DASH）→ 视频流 + 音频流 URL
  │   · 拿不到 DASH 就退 durl（单文件 mp4/flv）
  │   · 都不行（会员/付费/地区）→ 写失败原因，不硬试
  ├ share.set  com.shixiansi.plugin-bilibili/bili-results {id, ok, title, streams[], reason}
  ▼
QQ 插件（轮询 results，每 3 秒）
  ├ ffmpeg -headers "Referer: https://www.bilibili.com/" -i <video> -i <audio> -c copy <id>.mp4
  │   （process.launch 发射后不管 → 轮询产物：大小连续两次不变即视为完成，上限 90 秒）
  ├ 体积/时长超限 → 不发视频，改发「标题 + 摘要 + 链接」（规则见 §6）
  ├ send_group_message：[{text: 标题+UP+时长}, {at: 主人}, {video: file:///…}]
  └ 清掉产物与两条共享键
```

### 3.2 刷到值得看的 → 通知主人 / 发群

```
B 站插件（已有的四道闸通过之后）
  ├ 原有的气泡 + 收件箱（保留）
  └ share.set  com.shixiansi.plugin-bilibili/recommend {bvid, title, up, score, why, summary}
        ▼
QQ 插件（轮询 recommend）
  ├ biliNotifyPrivate = true → send_private_message 给 ownerQQ（一条摘要，不发视频）
  └ biliPushGroup = 群号 → 按 §3.1 同一条路发视频并 @ 主人
```

**分工的理由**：B 站插件只说「这条值得看」和「这条的流在哪」，**从不决定发给谁**；QQ 插件只说
「发给谁、怎么发」，**从不碰 B 站接口与 cookie**。两边各有一半，中间只有 JSON。

## 4. 配置（全部落在 QQ 插件，B 站插件不加配置项）

B 站插件已经卡在 `maxConfigItems = 32` 的 31 项上（`manifest.go:178`），所以新旋钮放在**行为所属**的
那一侧。QQ 插件现在 **26 项**，加 6 项 = **32**，正好顶到上限（`≤ 32`，一项都不多）。

| key | label | 类型 | 默认 | 说明 |
|---|---|---|---|---|
| `biliLinkReply` | 群里出现 B 站链接时自动发视频 | bool | `false` | 总开关，装上默认关 |
| `biliLinkAtOwner` | 发视频时 @ 主人 | bool | `true` | 用已有的 `ownerQQ` |
| `biliMaxMB` | 视频体积上限（MB，超过就只发链接） | number | `80` | |
| `biliMaxMinutes` | 视频时长上限（分钟，超过就只发链接） | number | `25` | |
| `biliRecommendGroup` | B 站刷到值得看的时发到这个群（群号，留空=不推） | text | `` | 发一条摘要并 @ 主人，不带视频 |
| `ffmpegPath` | ffmpeg 可执行文件路径（绝对路径，留空=不发视频） | text | `` | 走已有的 `process` 权限；**由用户手填**，不自动探测 |

B 站插件侧**只加一个 manifest 字段**：`shareRead: ["com.shixiansi.plugin-qq/bili-jobs"]`；
QQ 插件侧 `shareRead: ["com.shixiansi.plugin-bilibili/bili-results", "…/recommend"]`。

## 5. 安全与边界（逐条可验）

1. **cookie 不出 B 站插件**：QQ 插件从头到尾没有 `net.credentials`，也读不到那个 secret。
2. **共享通道有名字、有大小、有方向**：写只能写自己的前缀，读必须 manifest 点名，验收里有一条
   「未声明的键读不到、写别人的前缀被拒」。
3. **视频只发到 `biliLinkReply` / `biliPushGroup` 指定的群**；`groupAllow` 仍然管着「这个群允不允许机器人说话」，
   两把锁是 AND。
4. **@主人只 @ `ownerQQ`**，不接受从群消息里学来的 QQ 号。
5. **失败要说出来**：解析失败、超限、ffmpeg 缺失/超时，都在群里回一句（可选，见 §9-Q5），
   并在工坊日志里留完整原因。
6. **产物有清理**：发完即删；启动时清掉超过 1 小时的残留（`plugin-data/<id>/bili-video/`）。
7. **不做**：不搬运会员/付费内容（拿不到流就明确拒绝）、不批量抓取、不下载弹幕、不自动投币。

## 6. 画质：只有预算说话（时长只管能不能发）

第一版照抄了 xunlu 的两段式（① 按时长选起始档 ② 按预估体积往下降），**第二段是对的，
第一段是错的**：13 分钟的视频一律从 360p 起，哪怕 80 MB 的预算放得下 720p —— 用户的原话是
「好糊」。现在的规则只有一条：

> **在 `biliMaxMB` 以内，取画质最高的那条流。** 时长只决定「发不发」（`biliMaxMinutes` 硬上限），
> 不参与选档。

| 步骤 | 做法 |
|---|---|
| ① 取流 | `playurl` **带凭据**（`credentials`）：匿名只给到 480p 左右，登录后才有 1080p/4K。没贴凭据时退回匿名请求并在日志里说明 |
| ② 排序 | qn 从高到低；同一档优先 `avc1`（`-c copy` 不重编码，AV1/HEVC 会让一部分手机放不出来） |
| ③ 选第一条装得下的 | 预估字节 = `bandwidth / 8 × 时长`（音频另算）；一条都装不下 → 只发链接 |
| ④ 硬上限 | 时长 > `biliMaxMinutes`（25 分钟）→ 不发视频，只发「标题 + UP + 时长 + 摘要 + 链接」 |

真机对照（同为 80 MB 预算）：

| 视频 | 改前 | 改后 |
|---|---|---|
| 13.2 分钟 | 360p（qn 16）39 MB | **720p（qn 64）59.1 MB** |
| 16 秒 | 480p（qn 32）1.8 MB | **1080p（qn 80）8.6 MB** |

群里那条消息显示的是画质名（`720p`）而不是 `qn 64`。

其余失败路径：

| 情况 | 行为 |
|---|---|
| 只有 durl 单文件 | 直接发那个文件（不再合并） |
| playurl 无流（会员/付费/地区） | 群里回一句原因 + 链接 |
| `ffmpegPath` 为空或文件不存在 | 只发链接，并在工坊提示「没找到 ffmpeg」 |
| 下载/合并超时（90 秒） | 发链接、记日志；**产物保留**（失败时删掉等于把证据删了） |

## 7. 变更面（文件级）

| 层 | 文件 | 变更 |
|---|---|---|
| 权限词表 | `internal/plugins/capability.go` | 加 `plugin.share`（15 → 16）+ 标签 |
| manifest | `internal/plugins/manifest.go` | 加 `shareRead []string`（只读声明）；校验 `plugin.share` 与它的配对 |
| 通道 | `app_plugins.go`（+ `share-*` 落在发布者私有目录） | `share.set/get/remove`：自己的前缀可写、读要声明、64 KB/32 键上限 |
| **子进程可观测** | `app_plugins.go` | `process.launch` 由 `Release` 改成 goroutine 里 `Wait`（不阻塞、不绑定生命周期），并新增 `process.state {pid}` → `{known, running, exitCode, ranMs}`；挂在既有 `process` 权限上 |
| **自己的目录** | `app_plugins.go` | 新增 `storage.dir` → `{dir}`（只回调用者自己的），挂在既有 `storage` 权限上 |
| 门禁 | `app_plugins_test.go`、`.dev/check-frontend.cjs`、市场前端标签表 | 16 项、键权测试、子进程与目录测试 |
| B 站插件 | `app.js` §5b（取流与降级）+ §10c（jobs/results/recommend）+ `manifest.json` | `playurl`、画质阶梯、共享托盘 |
| QQ 插件 | `app.js`（链接识别、jobs/results/recommend、ffmpeg 合成、发送与 @）+ `manifest.json` | 6 项配置 + `shareRead` |
| 打包 | `market/tools/package.ps1`（自动）、`sample/embed.go`（两个 zip 名 + 版本号） | |

## 8. 不做的

- 不做「插件间直接调用方法」（RPC）——那是把沙箱拆了。
- 不把 cookie 复制给 QQ 插件（§2 的理由）。
- 不在 B 站插件里下载视频（1 MiB 上限 + 它不该碰磁盘大文件）。
- 不做视频转码/压缩（`-c copy` 只合并，不重编码）；要压就自己装 ffmpeg 参数另说。
- 不做多群广播、不做定时汇总。

## 9. 已拍板（原待定项，你的答复）

| # | 结论 |
|---|---|
| **Q1** | 群里**任何人**发链接都解析；受 `biliLinkReply` 与 `groupAllow` 双重限制 |
| **Q2** | **两条都发到群里**：链接回复发回提问的那个群；推荐发到 `biliRecommendGroup` 并 @ 主人（不走私聊） |
| **Q3** | 上限 **25 分钟 / 80 MB**；超时长的视频按 §6 的画质阶梯降级，退到底仍超就只发链接 |
| **Q4** | 失败/超限**要在群里回一句**（同一群 5 分钟内不重复同样的提示） |
| **Q5** | ffmpeg 路径**由用户手填**（`ffmpegPath`），不自动探测 |
| **Q6** | 机制名照用：权限 `plugin.share`，键 `<ownerId>/<name>` |
