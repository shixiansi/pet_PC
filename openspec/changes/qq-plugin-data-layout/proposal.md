# proposal.md — qq-plugin-data-layout

> 一句话：给插件的私有目录**分层**——`state/` `history/` `media/` `share/` 各管一类数据，
> 并把 1.10 那个"333 个文件平铺在同一个文件夹里"的目录一次性搬过来。

## 1. 为什么需要这次变更

用户的原话是"数据都混在一起，表情包、视频还有其他配置都很混乱"。真机上打开
`%AppData%\PetPC\plugin-data\com.shixiansi.plugin-qq\` 看到的就是这个：

| 事实 | 出处 |
| --- | --- |
| 333 个文件在同一个目录里，380 MB | `Get-ChildItem` 真机统计 |
| 其中 296 个是 `sticker_<hash>`，每个 0.06–1.4 MB 的 base64 | 表情包导入（`importStickers`） |
| 2 个 24 MB 的 `bilivideo*`，是发过群、还没被 `pruneVideoFiles` 收走的成品 | B 站联动（`runFfmpeg`） |
| `hist_<群号>` / `status` / `groups` / `log` / `stickers` / `categories` / `share-bili-jobs` 混在中间 | 群记忆、状态快照、表情包索引、宿主托盘 |

三层原因叠在一起：

1. **键就是文件名**。`storageKeyPattern` 是 `^[A-Za-z0-9_-]{1,64}$`（`app_plugins.go`），
   一个键只能是一个文件，插件没有任何办法表达"这是一类数据"。
2. **二进制借道文本**。表情包以 base64 存进 `storage.set`，于是 1 MB 的图占 1.4 MB，
   而 `storage.remove` 的键不许带点，文件名连扩展名都没有。
3. **宿主自己的东西也扔在里面**。跨插件托盘是 `share-<name>`，和插件的数据同级。

## 2. 目标

1. **一类数据一个文件夹**：状态、历史、表情包（索引与字节分开）、视频（成品与中间流分开）、
   宿主托盘各有各的位置。
2. **升级不丢数据**：1.10 的平铺文件由插件自己**一次性、幂等**地搬过去；三百个文件靠
   `os.Rename` 搬，不走桥（380 MB 的 base64 走一遍桥是三倍流量）。
3. **旧宿主不崩**：桌宠没跟着升级时（键只能是文件名），插件退回 1.10 的键名继续工作，
   日志说清"要升级桌宠才能整理"。
4. **不扩大信任面**：键仍然只能是相对路径，每一段仍是 `[A-Za-z0-9_-]`，
   `.`、`..`、盘符、反斜杠一律拒绝，写不出去自己的目录。

## 2b. 目标目录，以及一个不得不绕开的坑

```
state/status   state/log   state/groups   state/video   state/layout
history/<群号>
media/stickers/{index, categories, bytes/<hash>}
media/video/{<name>, tmp/<name>}
share/<name>                     （宿主写）
config.json                      （宿主按 manifest 写的设置，留在根目录）
```

第一版写的是 `stickers/` 和 `video/` 两个顶层文件夹，**那样搬不动**：1.10 的表情包索引
本身就是一个名叫 `stickers` 的**文件**，而新布局要在 `stickers\` 这个**目录**里写
`index` / `bytes\<hash>`。`MkdirAll` 碰到一个同名文件直接失败，于是索引、分类、296 个字节
文件全都搬不动，插件随即按新键读回一个空图库——用户看到的是"我的表情包没了"。

所以顶层文件夹起名时**全部避开 1.10 的键名**（`state` / `history` / `media` / `share`
都不是旧键），顺带把会占盘的字节收进 `media/`。这样每一个搬迁都是普通的改名，不依赖顺序、
不需要"先改成临时名再搬回来"那种特殊处理——而那正是"搬了一半"的来源。这条不变量有测试守着：
`顶层文件夹与 1.10 的键名不重名`（从源码里读出所有目标键再逐个比）。

## 3. 非目标

- **不把表情包改成真二进制**。要做得先有二进制写入口（`storage.set` 收的是字符串），
  而迁移就得把 296 个 base64 文件解码重写一遍——这一版先用 `storage.move` 零拷贝搬，
  格式不变。想做的时候是一次独立的变更。
- **不动 `config.json`**：它是宿主按 manifest 写的插件设置（`internal/plugins/config.go`），
  一个插件一份，留在根目录正好表示"这是这台机器的设置，不是插件的数据"。
- **不改 `audio\<name>.wav`**：语音插件已经在用这个子目录，形状一致，没有理由变。
- **不给插件通用文件系统**：新增的只有 `storage.move` 一个方法，两端都是键。

## 4. 变更面

| 层 | 文件 | 变更 |
| --- | --- | --- |
| 宿主（共享） | `internal/plugins/privatestore.go`（新） | `StorageKeyPath`（键 → 相对路径）、七个方法 `StorageGet/Set/Remove/Stat/Dir/List/Move`、`RegisterPrivateStore` |
| 宿主（桌宠） | `app_plugins.go` | `storage.*` 改为调用共享实现；新增 `storage.move`；`storage.dir` 支持 `sub`；`share-<name>` → `share/<name>`（带旧文件兼容读取） |
| 宿主（市场） | `tools/plugin-market/pluginhost.go` | 删掉本地那三份 `marketStorage*`，改用 `plugins.RegisterPrivateStore`——两个程序对"键合不合法"因此只有一个答案 |
| 插件 | `plugins/com.shixiansi.plugin-qq/app.js` | `DATA` 常量表、`historyKey` / `stickerBytesKey` / `videoKeyIn`、`migrateDataLayout`、`loadGroupsIndex`；所有读写改到新键；视频中间流落 `media/video/tmp/` |
| 插件 | `manifest.json` | 1.10.0 → 1.11.0 |
| 测试 | `internal/plugins/privatestore_test.go`（新）、`app_plugins_test.go`、`market/tests/plugin-qq*.test.js` | 键的语义、`sub`、`move` 的三态、托盘搬家、迁移与退回、顶层文件夹不撞名 |

## 5. 顺带修掉的一处真 bug

`state/groups`（1.10 叫 `groups`）是"哪些群最近有人说话"的索引，而 1.10 的机器人在启动时
**先把这张表写成空**：`state.groups` 从空 Map 开始，第一条日志触发的 `persistNow` 就把
`[]` 写回去了。于是每次重启，群名、计数、以及主动开口用来避开空群的 `lastAt` 全被清空。
这次加了 `state.groupsLoaded`：**没读过就不发布**。它同时是迁移的前提——否则刚搬过去的
群索引会在同一次启动里被抹掉。

## 6. 真机上发生的两件事，以及因此改掉的设计

这两条都是"跑起来才发现"的，写在这里是因为它们改变了实现，而不是补充说明。

1. **升级那一刻，索引已经是空的了。** 真机上的 `groups` 在迁移前就被上面那个 bug 清成了
   `[]`，于是"照着索引搬 `hist_<群号>`"一份都没动 —— 四份群记忆留在根目录，而机器人读的
   是 `history/<群号>`，用户看到的就是"它把每个群都忘了"。所以迁移不再以索引为准：搬完再
   用 `storage.list` 扫一遍根目录，按**名字形状**把 `hist_`、`sticker_`、`bili*` 归位，并
   把"有记忆没索引"的群补回索引。

2. **整理完十分钟，目录又变乱了。** 因为桌宠/市场内存里还是 1.10 的代码，它照旧往根目录写
   `status` / `groups` / `log` / `categories`。迁移有标记只跑一次，挡不住这个。所以加了
   `tidyLeftovers`：每次启动一次 `storage.list`，把六个平铺键**按内容合并**（不是覆盖：
   平铺那份带着旧进程刚记住的群名）后删掉平铺文件，同名记忆留**条数多的**那份——无脑用新
   写的会把四轮记忆盖成两句话，这也是真机上发生过的。
