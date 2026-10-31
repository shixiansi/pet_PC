# tasks.md — plugin-process-and-index

## 1. `process.kill`

- [x] `pluginChildProc` 加 `version` + `handle`（句柄只为止血，退出即丢弃）
- [x] `trackChild(pluginID, version, pid, handle)`；`finishChild` 里 `handle = nil`
- [x] `pluginVersionOf(id)`：manifest 读不到时返回 `""`（回收者不拿"不知道"当判据）
- [x] `pluginProcessKill`：三种结局（杀掉 / 已退出 / 未知 pid）+ 别人的进程拒绝
- [x] 方法表登记 `process.kill`（权限仍是 `process`，**没有新权限**）
- [x] 测试：`TestProcessKillStopsTheChildItStarted` / `…RefusesAnotherPluginsChild` /
      `…IsIdempotentAfterExit` / `…UnknownPidIsNotAnError` / `…NeedsThePermission`

## 2. 子进程回收

- [x] `startPluginChildReaper()`（5 秒）挂在 `app.go` 的启动序列
- [x] `reapPluginChildren()`：卸载 / 停用 / **版本变化**三种判据；锁序 manager → childMu
- [x] `logSurvivingChildren()`：退出前点名还活着的子进程（不杀，见 design D4）
- [x] `shutdown()` 里回收一次 + 点名
- [x] 测试：`TestReapStopsChildrenOfDisabledPlugins` / `…OfUpdatedPlugins` /
      `TestReapLeavesChildrenOfHealthyPlugins`

## 3. `plugins.json` 争用重试

- [x] `atomicWriteFile` 的 rename 换成 `renameWithRetry`
- [x] `isSharingViolation`：32 / 5 / EACCES / EPERM；其他错误立即返回
- [x] `renameFile` 测试缝（唯一的包级变量，注释写明理由）
- [x] 测试：重试成功 / 有界放弃（8 次）/ 非争用不重试 / `saveIndex` 真的走了重试

## 4. 文档同步（改了行为就必须同步）

- [x] 宿主 `README.md`：`process` 那一行（能停、只能停自己的、升级/停用/卸载由宿主收、退出不收）
- [x] 宿主 `AGENTS.md`：方法数 39 → 41（含 `plugins.services`）
- [x] 插件仓 `docs/plugin-dev.md`：§4 权限行、§6.12 方法表与语义、§9 两宿主差异表、附录方法表
- [x] 插件仓 `docs/plugin-testkit/fake-host.mjs`：`METHODS` 加 `process.kill`
- [x] 本 change 的 proposal / design / tasks

## 5. 验证

```powershell
go vet ./...
go test -count=1 .                    # 8 条新用例
go test -count=1 ./internal/plugins/  # 4 条新用例
node .dev\check-frontend.cjs
cd market; npm test
cd market; node docs/plugin-testkit/plugin-check.mjs plugins/*
```

已知既有红灯（与本次改动无关，见 proposal 第 5 节）：

- `internal/plugins` 的 `TestBuiltinSample*`：内置 sample 是 10 个插件、市场是 36 个，
  且 9 份同版本 zip 的字节本来就不同（`package.ps1` 与 `package.py` 的压缩器不同）。

## 6. 后续（不在本 change）

- [ ] sidecar：manifest 声明辅助脚本，宿主托管启动/重启/回收/超时 —— 跨重启的孤儿靠它解决
- [ ] `share.subscribe`：把"等一个值变化"从轮询变成推送
- [ ] 两个 CI 门禁：`plugin-check.mjs` 全仓、`index.json ↔ plugins/` 一致性
- [ ] 插件侧跟进：`privacy-guard` / `media-sync` / `pet-sense` / `screen-vision` 的 helper 路径
      仍是手工配置（今天 4 个从没跑起来）；sidecar 落地后应删掉那两个 config 字段
