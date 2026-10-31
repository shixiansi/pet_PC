# design.md — plugin-sidecar

## D1 为什么是宿主启动，而不是"插件继续自己启动、宿主只提供路径"

因为插件**没法**自己拿到那两条路径：`process.launch` 要绝对路径，而插件不知道自己的安装目录
（`storage.dir` 给的是私有数据目录，不是插件目录）。所以"自己启动"必然要求用户手填——那正是
四个插件从没跑起来的原因。宿主知道安装目录，也知道用户授了什么权，它是唯一能不做错这件事的一方。

## D2 `script` 是相对路径，而且必须写进 `assets`

- **相对**：绝对路径的含义取决于桌宠从哪儿启动，`process.launch` 已经为同一个理由要求绝对路径；
  manifest 里反过来要求相对，因为参照物是"插件自己的目录"，而那个目录由宿主决定。
- **必须进 `assets`**：打包器发的是 manifest + 入口 + 三个固定资源 + `assets`。一个只写在
  `sidecar.script` 里、没进 `assets` 的脚本不会进包，症状是装好之后"找不到文件"——而那时作者
  在本地跑得好好的（仓库里那个文件确实存在）。这条规则让"打包"和"声明"不可能分叉。

## D3 必须申请 `process`，必须声明 `background`

- `process`：sidecar 就是一个本机程序。安装对话框里那一项（"启动本机程序"）必须真的覆盖它，
  否则用户被问的是一件事、发生的是另一件事。
- `background`：常驻帧是读它输出的那一半。没有读者的进程不该被启动——这条也顺带挡住"用 sidecar
  绕过 background 门控"的写法（`background` 本身要求 `events`/`net.ws`/`net.fetch`/`plugin.call`
  之一，见 `validateBackground`）。

## D4 上下文走环境变量，不走配置项

`PETPC_PLUGIN_ID` + `PETPC_PLUGIN_DATA`（私有目录的绝对路径）。理由：

1. 用户不该为"我的脚本在哪、它该往哪写"填两个路径；
2. 私有目录是宿主的决定（`plugin-data/<id>`），把它作为参数传下去，比让插件从 `storage.dir`
   再转述一遍更短；
3. 脚本写进那个目录的文件，插件用 `storage.get` 就能读——`storage` 的键到文件是逐段映射的，
   两边对得上（这也是 privacy-guard 那次"键带扩展名"事故的教训：文件名与键必须一致）。

## D5 一次 per（插件，版本），崩溃不重启

回收者每 5 秒跑一次。如果"子进程退出就重启"，一个必然崩溃的脚本会变成每 5 秒一次的后台崩溃循环：
日志刷满、机器空转，而用户看不出发生了什么。

所以：**启动记录按（插件 id → 已启动的版本）**保存。同版本再扫到就不动；版本变了（升级）才重来；
停用再启用不会重来（要重来就重启桌宠）。要更顽强是脚本自己的事——它是程序，可以自己重试。

## D6 复用既有的子进程表，而不是另起一套监管

`trackChild(pluginID, version, pid, handle)` 已经是"谁启动了什么、什么版本、怎么停"的唯一账本，
`process.kill` 与回收者都读它。sidecar 走同一条路，于是：

- 插件停用/卸载/升级时，`reapPluginChildren` 顺手就收掉了 helper（版本判据正好匹配）；
- 桌宠退出时 `logSurvivingChildren` 会点名它；
- 插件自己也能 `process.kill` 它（如果它想提前停）。

另起一套监管会让这三件事各有两份实现，而"两份实现必然分叉"是这个仓库反复吃过的亏。

## D7 解释器：`.ps1` 用 `powershell.exe -NoProfile -ExecutionPolicy Bypass -File`

- `-NoProfile`：用户 profile 可能打印、可能阻塞、可能改掉变量——一个后台 helper 不该受它影响。
- `-ExecutionPolicy Bypass`：包本身是用户批准安装的内容；在这里拒绝执行只会教用户去改机器级策略，
  那更糟。
- 路径从 `%SystemRoot%` 拼，而不是 `PATH`：`powershell.exe` 在 PATH 里可能是 PowerShell 7，
  而脚本是按 5.1 写的（本仓 `.dev` 的 BOM 那条坑就是这个原因）。
