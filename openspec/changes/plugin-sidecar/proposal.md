# proposal.md — plugin-sidecar

> 把「插件带一个本机辅助脚本」从**每个插件各写一遍**变成**manifest 声明一次、宿主托管**。
>
> 触发它的是 2026-10-31 的一次实测：五个这样写的插件（`pet-sense` / `media-sync` /
> `privacy-guard` / `screen-vision` / `clipask`）里，**四个的脚本从来没跑起来过**——它们各自把
> `helperPath`（powershell.exe 的绝对路径）和 `helperScript`（脚本的绝对路径）做成两个设置项，
> 等用户在插件页里手填。没填的后果不是报错，而是"装好了但什么也没有"：privacy-guard 的脱敏读到的
> 一直是空值，日志里只有一行 `storage.get ok`（12 字节）。

## 1. 现状（本仓实读事实）

| 事实 | 出处 |
| --- | --- |
| 五个插件各带一份 `.ps1`，各自暴露 `helperPath` + `helperScript` 两个绝对路径设置项 | 各插件 `manifest.json` 的 `config` |
| 各自经 `process.launch` 启动，各自轮询自己私有目录里的 `outbox/*` 文件 | 各插件 `app.js` / `lib/*.js` |
| 启动失败在界面上只是一句话，日志里没有一行 | `privacy-guard/app.js:124-135` |
| `process.launch` 的子进程表按插件归属记账（本 change 依赖它） | `app_plugins.go` 的 `children` / `trackChild` |
| 打包器只发 manifest + 入口 + 三个固定资源 + `assets` | `market/tools/package.py`、`package.ps1` |

## 2. 变更说明

- **manifest 新字段** `sidecar: {script, args?}`：`script` 是插件目录内的相对路径（`.ps1` 或
  `.exe`），且必须同时出现在 `assets` 里。
- **校验四条**：路径在包内、脚本在 `assets` 里、申请了 `process`、声明了 `background`。
- **宿主托管生命周期**：插件启用且用户授予 `process` 时启动一次；停用 / 卸载 / 换版本时由既有的
  回收者收掉；桌宠退出时不停（与 `process.launch` 的既有契约一致）。
- **脚本从环境变量拿上下文**：`PETPC_PLUGIN_ID`、`PETPC_PLUGIN_DATA`。插件不再需要问用户要路径。
- **崩了不自动重启**：回收者每 5 秒扫一次，"退出就重启"会变成后台崩溃循环。

## 3. 明确不改

- **不新增权限**：`sidecar` 要求插件申请已有的 `process`——它就是一个本机程序。
- **不新增方法**：插件侧没有 `sidecar.*` 可调；读输出仍然走 `storage.get`（脚本写进
  `PETPC_PLUGIN_DATA`，也就是插件自己的私有目录）。
- **不改打包器**：`script` 必须写进 `assets`，所以现有打包逻辑原样可用。

## 4. 影响面

| 文件 | 改什么 |
| --- | --- |
| `internal/plugins/manifest.go` | `Sidecar *SidecarSpec` 字段、`validateSidecar`（四条规则 + args 上限） |
| `app_plugins.go` | `pluginHost.sidecars`（按插件记"哪个版本已经启动过"）、`ensurePluginSidecars`、`startPluginSidecar`；挂在既有的 5 秒回收 tick 上 |
| `app_plugins_test.go` | 2 条：启动 + 幂等 + 停用后被回收；没授予 `process` 时一个都不启动 |
| `internal/plugins/manifest_test.go` | 1 组：合法声明通过 + 六种拒绝 |
| `market/docs/plugin-dev.md` | 顶层字段表加一行 + `sidecar` 小节（规则、环境变量、生命周期、迁移提示） |
| `README.md` | `process` 那一行补一句"辅助脚本可以交给宿主托管" |

## 5. 不做但记在案

- **四个插件的迁移**（删掉 `helperPath`/`helperScript`、改读 `PETPC_PLUGIN_DATA`）——那是四个插件
  各自的版本升级 + 重新打包，按插件仓的惯例一次一个 patch。
- **崩溃重启策略**（退避、最大次数）——等有插件真的需要再说；今天的五个 helper 都是"写文件"，
  崩了重启也只是再崩一次。
- **sidecar 的 stdout/stderr 收集**：现在丢弃（与 `process.launch` 一致），要排查请让脚本自己写日志文件。

## 6. 验证

```powershell
go vet ./...
go test -count=1 .                       # TestSidecarStartsAndIsReaped / …NeedsTheGrantedProcessPermission
go test -count=1 ./internal/plugins/     # TestManifestSidecar（1 组 + 6 个子用例）
node .dev\check-frontend.cjs
cd market; npm test; node docs/plugin-testkit/plugin-check.mjs plugins/*
```
