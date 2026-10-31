# tasks: sidecar 脚本的编码，和一份能读的日志

## 宿主（本仓）

- [x] `internal/desktop/text_windows.go`：`DecodeChildOutput` —— 先认 UTF-8，否则按本机
      ANSI 代码页（`MultiByteToWideChar(GetACP())`）转换，失败保留原字节。
- [x] `internal/desktop/text_other.go`：非 Windows 的同一函数（原样返回），这样调用方不必带
      构建标签。
- [x] `internal/desktop/text_windows_test.go`：GBK 字节 → 中文；UTF-8 / ASCII / 空输入原样。
      机器不是 936 代码页时跳过（断言的是"用了本机代码页"，不是"用了 GBK"）。
- [x] `app_plugin_sidecarlog.go`：`decodeSidecarLine`（先切 CR 再解码）+ `sidecarScriptEncoding`
      （`.ps1` + 无 BOM + 含非 ASCII → 一句话说明）。
- [x] `app_plugins.go`：`startPluginSidecar` 启动前调用它，WARN 里点名脚本与原因；**不阻止**启动。
- [x] `app_plugin_sidecarlog_test.go`：`TestSidecarScriptEncodingNamesTheTrap`（纯 ASCII 正常、
      BOM 正常、无 BOM 含中文被标记、`.exe` 不适用）。

## 插件仓（`market/`）

- [x] 加 UTF-8 BOM：`pet-sense-helper.ps1`、`media-helper.ps1`、`privacy-guard-helper.ps1`、
      `vision-helper.ps1`（源 + 本机已装的那份，二者一致）。
- [x] `clipask/assets/clip-watch.ps1`：补上 `while ($true) {` 缺的那个 `}`（文件原来在 `catch`
      里就结束了）。
- [x] `docs/plugin-dev.md` §4 `sidecar` 表：新增"`.ps1` 必须带 BOM 或纯 ASCII"一行，写明
      PowerShell 5.1 的解码行为与症状。
- [x] `docs/plugin-testkit/plugin-check.mjs`：`sidecar` 四条校验 + `sidecar-encoding`；
      并把 `assets` / `sidecar` 从 `checkManifest` 返回出去（顺带修掉"声明正确的 assets 被误报").

## 文档

- [x] `AGENTS.md` §3.9 第 2 条：把编码写进 sidecar 契约，附两个测试名。
- [x] `openspec/changes/sidecar-encoding/`（本文件与 proposal）。

## 发布（这一轮真的打了包）

- [x] 五个插件的 `version` 各加一个补丁号：pet-sense 0.1.7→0.1.8、media-sync 0.1.7→0.1.8、
      privacy-guard 0.1.3→0.1.4、screen-vision 0.1.3→0.1.4、clipask 0.2.0→0.2.1
      （宿主只接受**严格更高**的版本，不 bump 就等于安装了也拿不到修好的脚本）。
- [x] `market/tools/package.ps1`：重打 36 个包、回写 `index.json`（每条 sha256 与 zip 对得上）。
- [x] `market/index.html`：静态目录页里那三张卡（clipask / pet-sense / media-sync）的版本、
      下载链接与按钮文案跟着改，`tests/ui-market.test.js` 因此重新变绿。
- [x] 两个程序重建：`PetPC.exe` / `PetPCPluginMarket.exe` 都嵌入了刷新后的
      `internal/plugins/sample`（扫字节确认新 `updatedAt` 在、旧的不在）。

## 打包装箱时踩到的坑（同一个 change 里一起修掉）

- [x] **`package.ps1` 第 5 步会把整个市场目录塞进内置样例源**：复制 36 个 zip 并把
      `market/index.json` **覆盖**样例的 `index.json`，而 `internal/plugins/sample/embed.go` 只
      按名字嵌了 10 个包——于是兜底目录宣告 36 个下载、二进制只带 10 个，点安装就 404。
      `TestBuiltinSampleMatchesIndex` 事后抓到了，但修的地方是脚本：现在它**只刷新样例已有的包**
      （同名 = 同版本才换字节），样例索引按磁盘重算 sha256，**不新增文件**，并丢掉市场已经不认的 id。
      加包仍然是手改 `embed.go` + 重跑脚本。
- [x] `AGENTS.md` §3.7：把这条写进"sample 是子集"那段——它的坑在于"照抄 index.json"看起来最省事。
- [x] **`package.ps1` 只能跑在 `pwsh` 下**（`param()` 的默认值用了 `$PSScriptRoot`，Windows
      PowerShell 5.1 在参数绑定阶段还没有它）。这是既有性质，不是这次引入的；写进脚本头部的
      `.EXAMPLE` 附近？不——写进 AGENTS §7 的一句更合适（见下）。
- [x] 顺手修掉两条**跨午夜/跨月就红**的用例（与本次改动无关，是 2026-11-01 00:00 之后才暴露的
      既有缺陷）：`tests/plugin-expense-tracker.test.js` 把 `Date` 冻在夹具的 NOW 上（`toFake:['Date']`，
      定时器保持真实，泵还要靠它们）；`tests/plugin-diary-writer.test.js` 里那条拿测试进程的
      `Date.now()` 和插件 realm 的时钟相比的断言改成"落回一个合法 day key"。

## 没做（有意）

- 没有把新版本**装到本机**：本机已装的五份脚本上一轮已经改成与源码一致的字节，行为已经是对的；
  `plugins.json` 里的版本号仍是旧的，要不要点"更新到 vX"是用户/市场页的决定。
- 26 个**未改动**插件的 zip 也被重打了包（内容与插件目录逐字节一致，只有压缩表示不同，索引里的
  sha256 因此都变了）。这是"用当前打包器重打一次"的正常结果，不是内容漂移。

