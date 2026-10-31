# proposal.md — plugin-storage-pressure

> 插件私有目录跨过 80% 时发一条 `storage.pressure`：一次跨越一次，回落才重置。

## 1. 现状（实读事实）

| 事实 | 出处 |
| --- | --- |
| 上限 64 MiB 已在 `storage.set` 与 `audio.save` 上强制，超了拒绝并写明数字 | `internal/plugins/privatestore.go` 的 `checkStorageBudget` |
| 但插件只有在**被拒的那一次**才知道自己满了 | 同上 |
| 被拒时它已经没得选：这次写入没了，要么丢数据要么丢信任 | 本次会话的 quota 设计（design D1） |

## 2. 变更说明

- `plugins.PluginDataPressureRatio = 0.8` + `PluginDataPressureAt(limit)`。
- `App.noteStoragePressure(id, used, limit)`：跨过线 → 发 `storage.pressure {used, limit, at}`；
  在线下 → 清 latch。**一次跨越只发一次**（latch 在 `pluginHost`，带锁）。
- 只在**写成功之后**调用（`storage.set` 与 `audio.save`）：数字是磁盘上的数字，读路径不参与。
- 插件侧：clipask 收到了会 `pushState` + `plugin.log warn`（它的 `clip/inbox/` 会随每次复制长大）。

## 3. 明确不改

- 不在读路径上算（`storage.get` 是热路径，而读不会让目录变大）。
- 不自动清理：删什么只有插件知道（`storage.list` / `storage.remove` 就是给它用的）。
- 不做"可配置阈值"：80% 是提醒，不是套餐。

## 4. 影响面

| 文件 | 改什么 |
| --- | --- |
| `internal/plugins/privatestore.go` | `PluginDataPressureRatio`、`PluginDataPressureAt` |
| `app_plugins.go` | `pluginHost.pressureMu/pressured`、`noteStoragePressure`、`pluginPrivateStoreWithPressure`、音频保存后也提示 |
| `app_plugins_test.go` | `TestStoragePressureFiresOncePerCrossing`（线下不发 / 跨越发一次 / 再写不发 / 回落重置后再发） |
| `market/docs/plugin-dev.md` | §7.3 |
| `AGENTS.md` §3.9 | 第 3 条补 `storage.pressure` |
| clipask | 响应它（`pushState` + `plugin.log`），版本 0.1.18 |

## 5. 验证

```powershell
go vet ./...; go test -count=1 . ; node .dev\check-frontend.cjs
cd market; npm test; node docs/plugin-testkit/plugin-check.mjs plugins/*
```
