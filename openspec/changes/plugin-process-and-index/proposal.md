# proposal.md — plugin-process-and-index

> 本 change 补的是插件与宿主之间**两块最底层的地基**，两条都来自 2026-10-31 的真机日志：
>
> 1. **进程只能启动、不能停**：`process.launch` / `process.state` 有了，`process.kill` 没有，
>    子进程也从来没有回收者。当天的后果是一个辅助脚本在插件升级之后继续用**旧文件名**写文件，
>    唯一的解决办法是用户在任务管理器里手动结束它。
> 2. **`plugins.json` 跨进程没有互斥**：原子写只在"一个进程"的前提下成立，而它是**两个程序**在
>    写（控制中心存授权、桌宠每 1.5 秒读）。当天 14:42:53 的
>    `保存授权失败：rename …plugins.json: Access is denied` 就是它，而那一刻界面显示"已授权"、
>    闸门却说"未获权限"，没有任何东西会去纠正这个分歧。
>
> 本文件只写「为什么、改变什么、影响哪些文件」；技术决策在 `design.md`，执行清单在 `tasks.md`。

## 1. 现状（本仓实读事实）

| 事实 | 出处 |
| --- | --- |
| 方法表 41 项，`process` 名下只有 `process.launch` / `process.state` | `app_plugins.go:442-450` |
| `pluginHost.children` 记 pid → 插件；超过 32 个淘汰最旧的**记录** | `app_plugins.go:3053-3077` |
| 全仓库唯一一处 `.Kill()` 在测试里 | `app_plugin_market_window_test.go:50` |
| `process.launch` 用 `cmd.Start()` + goroutine `cmd.Wait()`，句柄被 `trackChild` 丢掉 | `app_plugins.go:3017-3034` |
| 子进程**故意**不随桌宠退出（协议端要跨重启活着） | `app_plugins.go:2926-2933` 的注释 |
| `atomicWriteFile` = temp + `os.Rename`，**没有重试、没有跨进程锁** | `internal/plugins/manager.go:184-232` |
| 同一份索引有两个写者（控制中心存授权/启停）+ 一个读者（桌宠 1.5 秒轮询） | `tools/plugin-market/market.go:303`、`frontend/src/main.js:928,1295` |
| 插件停用/卸载/更新后，桌宠的帧会被重挂（1.5 秒内） | `frontend/src/main.js:1288-1298` 的注释与 `refreshInstalled` |

## 2. 变更说明（做什么）

### 2.1 `process.kill`

- 新方法：`{pid}` → `{killed, known, exitCode?}`，权限仍是 `process`（**不新增权限**）。
- **按"谁启动的"授权，不按 pid**：只允许停宿主记录在案的、由**本插件**启动的进程；别人的 pid 得到
  一句中文拒绝。
- 三种结局：跑着且是我们的 → 杀掉；我们的但已经自己退了 → `killed:false`（不是错误，调用方的意图
  已满足）；不在表里（上一次运行留下的 pid）→ `known:false`（也不是错误，与 `process.state` 同一规则）。

### 2.2 子进程回收者

- 每 5 秒检查一次：**插件已卸载 / 已停用 / 换了版本**时，停掉它名下的子进程，并各记一行 INFO
  （写明原因）。
- 版本是判据之一：升级会重挂帧，旧版本启动的进程"已经不是谁的子进程了"——这正是当天那个孤儿。
- **桌宠退出时不收**：协议端（Yogurt 之类）按设计要跨桌宠重启活着。退出时改为**点名**还剩哪些进程
  在跑（INFO），让"为什么这个进程还在"有日志可查，而不是只有任务管理器。
- 回收一次也在 `shutdown()` 里跑一遍，所以"停用插件然后退出"不会留下东西。

### 2.3 `plugins.json` 的争用重试

- `atomicWriteFile` 的 `os.Rename` 改为**有界重试**（8 次，约 200 ms）：只在 Windows 的
  `ERROR_SHARING_VIOLATION` / `ERROR_ACCESS_DENIED`（以及 POSIX 的 `EACCES`/`EPERM`）上重试。
- 其他错误**第一次就返回**：磁盘满不该看起来像争用。
- 读路径不动：原子写保证读者看不到半个文件，这一条本来就成立。

## 3. 明确不改

- **不新增权限**（`process` 一项覆盖启动、查状态、停止）。§3.1 的"加一项权限要同步七处"不触发。
- **不改 manifest 格式**：`process.kill` 是运行时方法，没有新字段。
- **不改前端**：帧的重挂、授权刷新、方法表下发都不变。
- **不落盘子进程表**（见 `design.md` D5：pid 复用风险，且真正的解法是 sidecar，见第 5 节）。

## 4. 影响面

| 文件 | 改什么 |
| --- | --- |
| `app_plugins.go` | 方法表加 `process.kill`；`pluginChildProc` 加 `version`/`handle`；`trackChild` 签名；`pluginProcessKill`；`reapPluginChildren`；`logSurvivingChildren`；`pluginVersionOf` |
| `app.go` | 启动序列加 `startPluginChildReaper()` |
| `app_reminder.go` | `shutdown()` 里回收一次 + 点名存活者 |
| `internal/plugins/manager.go` | `renameWithRetry` / `isSharingViolation` / `renameFile` 测试缝 |
| `app_plugins_test.go` | 8 条：kill 的三种结局、别人的进程被拒、权限顺序、回收三种情形（停用/升级/健康） |
| `internal/plugins/manager_test.go` | 4 条：重试成功、有界放弃、非争用不重试、`saveIndex` 真的走了重试 |
| `README.md` | `process` 那一行（用户可见的行为） |
| `AGENTS.md` | 方法数 39 → 41（`plugins.services` 那次未提交的改动也算在内） |
| `market/docs/plugin-dev.md` | §4 权限行、§6.12 方法表与语义、§9 两宿主差异表、附录方法表 |
| `market/docs/plugin-testkit/fake-host.mjs` | `METHODS` 加 `process.kill` |

## 5. 不做但记在案（后续 change）

1. **sidecar（辅助脚本一等公民）**：manifest 声明 `sidecar`，宿主负责启动/重启/回收/超时。它才是
   "桌宠重启后不留旧 helper" 的正解——本 change 只能收"同一次运行内"的孤儿。
2. **`share.subscribe`**：插件之间只有"拉"，于是每个插件 1–2 秒轮询（日志 94% 是 DEBUG）。
3. **两个 CI 门禁**：`plugin-check.mjs` 全仓跑、`index.json ↔ plugins/` 一致性。

## 6. 验证

```powershell
go vet ./...
go test -count=1 .                                  # 新增 8 条 process.kill/回收用例
go test -count=1 ./internal/plugins/                # 新增 4 条 rename 重试用例
node .dev\check-frontend.cjs                        # 静态接线（权限词表没动，应仍绿）
cd market; npm test; node docs/plugin-testkit/plugin-check.mjs plugins/*   # 手册与假宿主同步
```
