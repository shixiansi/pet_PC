# tasks.md — qq-plugin-data-layout

> 唯一执行清单。每条都写"做什么 + 完成判据"，已完成的勾上并注明验证方式。

## P1 宿主：键可以是一棵树的路径

- [x] **T1 键的语义**
  内容：`internal/plugins/privatestore.go` 新增 `StorageKeyPath`：按 `/` 切段、每段必须过
  `ValidStorageSegment`（`[A-Za-z0-9_-]{1,64}`，不含点）、最多 4 段、整条 200 字符。
  返回 OS 分隔符的相对路径，供 `filepath.Join` 到私有目录。
  判据：`TestStorageKeyPathIsARelativePathAndNothingElse` —— 8 个合法键（含 `a/b/c/d` 与
  64 字符段）与 15 个非法键（`..`、`a/../b`、`/abs`、`a//b`、`a/`、`a\b`、`with.dot`、
  65 字符段、5 段、超长）逐个断言。
  （`a/b` 从"非法"变成"合法"是这次变更的核心；桌宠侧的原断言
  `TestStorageRejectsBadKeys` 同步改成仍然非法的那些拼法。）

- [x] **T2 六个方法一份实现**
  内容：`StorageGet/Set/Remove/Stat/Dir/Move` 收在 `internal/plugins`，签名
  `(dir string, payload []byte)`；`RegisterPrivateStore(r *Registry)` 供市场注册，
  桌宠的 `pluginMethods` 用 `pluginPrivateStore(fn)` 适配。
  判据：`TestStorageSetCreatesTheFoldersItsKeyNames`（写就建父目录、读/stat/remove 沿同一路径）、
  `TestStorageNoCallEscapesThePrivateDir`（四个方法 × 四个越界键，且父目录没有多出文件）。

- [x] **T3 `storage.dir` 的 `sub`**
  内容：`{"sub":"video/tmp"}` 校验并 `MkdirAll`，返回该子目录的绝对路径；无 `sub` 行为不变。
  判据：`TestStorageDirCreatesOnlyAFolderInsideThePrivateDir`、桌宠侧
  `TestStorageDirCanNameAFolderInsideThePrivateDir`（`../`、`/abs`、5 段、带点全部拒绝）。

- [x] **T4 `storage.move`**
  内容：`{from,to}` 两端都过键校验；源缺失 → `moved:false`；目标已存在 → `moved:false,
  targetExists:true` 且**不动源**；否则 `MkdirAll` + `os.Rename`。
  判据：`TestStorageMoveIsIdempotent`、`TestStorageMoveRenamesOwnFiles` —— 迁移要的三态、
  "不许覆盖"、"两端都是键"。

## P2 宿主：托盘文件进 `share/`

- [x] **T5 `share/<name>`**
  内容：`shareFileName` → `filepath.Join("share", name)`；`set` 前建目录并删掉旧的
  `share-<name>`；`get` 读不到新路径时回退读旧文件（并尽力 `os.Rename` 过去）；
  `remove` 两份都删。
  判据：`TestShareValuesLiveInTheirOwnFolder` —— 旧文件读得到、搬进 `share/`、
  新写入落在 `share/`、`remove` 之后两处都没有。

## P3 插件：目录结构与迁移

- [x] **T5b `storage.list`**
  内容：`{sub}` → `{dir, files:[{name,bytes}], dirs:[...]}`，只读、只看一层、目录不存在算空表；
  `sub` 与 `storage.dir` 同一套校验。
  判据：`TestStorageListShowsWhatIsActuallyThere` —— 空目录与不存在的目录都是空表、不递归、
  `bytes` 是真的、五个越界 `sub` 全被拒。
  为什么加：真机上两个 24 MB 的视频成品在账本被清之后**再也不会被删**，而插件连"目录里有
  什么"都问不出来——没有这个方法，"清理自己的垃圾"只剩"按猜的名字删"这一条路。

- [x] **T5c 每次启动的轻量整理 `tidyLeftovers`**
  内容：`storage.list` 列一次根目录，然后三件事：`absorbLegacyFiles`（六个平铺键目标不在就
  改名、已在就**按内容合并**再删平铺那份）、`sweepLeftovers`（`bili*`/`bilivideo*`、
  `hist_<群号>`、`sticker_<hash>` 按名字形状归位；同名已存在时记忆留条数多的、字节按 hash
  去重）、`seedGroupsFromHistory`（有记忆没索引的群补进 `state/groups`）。
  判据：`没人记账的旧视频产物被收进 media/video…`、`群索引被 1.10 的清空 bug 弄空了…`、
  `索引里没有的表情包字节也跟着搬…`、`升级时旧进程还在写平铺文件：合并进新位置…`、
  `旧进程又写了一遍同名记忆/字节：留条数多的那份…`、`旧进程写来的记忆更全时，用它的…`。
  为什么不能只靠标记：真机上整理完 10 分钟后，根目录里又冒出 `status`/`groups`/`log` ——
  桌宠内存里还是 1.10 的代码。迁移有标记只跑一次，所以"目录保持干净"必须每次启动兜一次。

- [x] **T6 键表**
  内容：`app.js` 新增 `DATA`（`state/status`、`state/log`、`state/groups`、`state/layout`、
  `state/video`、`media/stickers/index`、`media/stickers/categories`）与 `historyKey` /
  `stickerBytesKey` / `videoKeyIn` / `cleanSegment`；所有读写改过去。
  判据：`market/tests/plugin-qq.test.js` 里 62 条用例全部按新键断言（`state/log`、
  `history/10086`、`media/stickers/bytes/<hash>`）。

- [x] **T6b 顶层文件夹不与 1.10 的键名撞车**
  内容：顶层用 `state` / `history` / `media` / `share`，没有一个等于 `status|log|groups|
  stickers|categories|bilivideo-files`。第一版用的 `stickers/` 会让"把索引搬进
  `stickers/index`"需要先建一个与它同名的目录，`MkdirAll` 必失败 —— 见 proposal §2b。
  判据：`顶层文件夹与 1.10 的键名不重名` —— 从源码读出 `DATA` 与两个目录常量，
  逐个断言首段不在旧键名表里。

- [x] **T7 一次性迁移 `migrateDataLayout`**
  内容：探 `state/layout`；按索引搬 6 个平铺键、每个 `sticker_<hash>`、每个 `hist_<群号>`；
  最后写标记。索引不知道的由 T5c 每次启动兜住。
  判据：`数据目录 > 1.10 的平铺文件被搬进各自的文件夹，一个字节都不丢` —— 新键有内容、
  九个旧名字一个不剩、`share-bili-jobs` 不归插件管所以还在、`ws.open` 仍然发生；
  `第二次启动只查一个标记，不再搬任何东西`。

- [x] **T8 旧宿主退回旧键名**
  内容：探测失败（`state/layout` 被宿主拒绝）→ `useLegacyKeys()` 把 `DATA` 改回
  1.10 的名字，`historyKey` / `stickerBytesKey` / `videoKeyIn` 一并退回；日志写明要升级桌宠。
  判据：`桌宠还是 1.10（拒绝子目录键）时退回旧名字，数据不写到一半` —— 零次 `storage.move`、
  零次带斜杠的 `storage.set`、日志含"升级到 1.11"。

- [x] **T9 视频分成成品与中间流**
  内容：`storage.dir {sub:'media/video'}` 与 `{sub:'media/video/tmp'}`；`downloadStream` /
  `muxStreams` 用 `fileNameOf(key)` 拼路径；键是 `media/video/tmp/bili…` 与
  `media/video/bilivideo…`，保留清单在 `state/video`。
  判据：`plugin-qq-bili.test.js > curl 下两条流…` —— curl 的 `-o` 落在
  `…\media\video\tmp\bili`、发出去的 `file://…/media/video/bilivideo…`、账本键
  `media/video/bilivideo…`；`够旧的产物由下一次轮询清掉` 断言只删 `media/video/bilivideoold*`。

- [x] **T10 群索引不再被启动时清空**
  内容：`loadGroupsIndex()`（`startBot` 第一件事，`lastAt` 原样保留）+ `state.groupsLoaded`
  门控 `persistNow` 的 groups 写入。
  判据：迁移用例里 `state/groups` 仍是 `[{id:'10086'}]`（若不清空，这一条会变成 `[]`）。

## P4 文档

- [x] **T11 README**
  内容：`storage` 权限行、`插件数据放在哪`（新增 `share\` 与"键是相对路径"一段）、QQ 插件
  一节的目录表与迁移说明、`数据目录` 表里 QQ 插件那一行。
  判据：`hist_<群号>` 不再出现在 README 里。

- [x] **T12 本变更记录**
  内容：`openspec/changes/qq-plugin-data-layout/{proposal,tasks}.md`。
  判据：本文件。

## 验证方式（真机可复现）

| # | 操作 | 应该看到 |
| --- | --- | --- |
| 1 | 升级前记下 `plugin-data\com.shixiansi.plugin-qq\` 的文件数（真机 333） | 平铺 |
| 2 | 装上 1.11.0 的 QQ 插件，重启桌宠 | 目录变成 `state\` `history\` `media\` 三个文件夹（外加宿主的 `config.json`、`share\`），根目录只剩这几项 |
| 3 | 打开市场里的「表情包」卡片 | 图还在、分类还在（索引与字节都搬过去了） |
| 4 | 群里 @ 一次它 | 回答里用得上升级前的上下文（`history\<群号>` 搬过去了） |
| 5 | 再次重启 | 日志里没有"数据目录已整理"，迁移只跑一次 |
| 6 | 把桌宠退回 1.10、只装 1.11.0 的插件 | 机器人照常回答，日志一行"这个桌面版本还不支持分文件夹存放…升级到 1.11 后会自动整理" |
