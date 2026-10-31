# proposal.md — plugin-log

> 给插件一个说一句话的地方：`plugin.log {level?, message}`。

## 1. 现状（实读事实）

| 事实 | 出处 |
| --- | --- |
| 插件的 `console.error` 进不了 `pet.log`：帧是 opaque origin 的 iframe，它的控制台只在 devtools 里 | `frontend/src/plugin-host.js` |
| 宿主只在**闸门**这一侧记日志：一次调用一行（方法、字节数、耗时、失败原因） | `app_plugins.go` 的 `dispatchPluginRequest` |
| 所以"插件自己发现了什么"从来没进过日志：2026-10-31 的排查是靠读 `pet.log` + grep 插件源码 | 本仓 docs/logging.md、AGENTS.md §3.8 |
| 日志三级（DEBUG / INFO / WARN）、5 秒节流、INFO 及以上进诊断报告 | `internal/logging`、`settingsapi_diagnostics.go` |

## 2. 变更说明

- 新方法 `plugin.log {level?, message}` → `{ok, swallowed}`；两个宿主都服务。
- **一行**：换行压成空格（否则插件能伪造一行"宿主的日志"）；≤500 字符截断并标注。
- `level` 只接受 debug / info / warn（默认 info）。没有 error：日志只有三级。
- 每行带插件 id（`Pluginf`），与宿主自己的行区分开。
- 同一（插件, 等级）5 秒内只写一行，被合并次数在 `swallowed` 里返回。
- **借用 `storage` 权限**：把插件自己的一句话交给宿主，与往自己私有目录写文件是同一件事；
  不新增第 15 项权限（先例：`pet.badge` 借 `pet.surface`）。
- 调用本身按数据面记 DEBUG（`DataMethod`）：否则一个在轮询里写日志的插件会把调用日志变成噪音。

## 3. 明确不改

- 不新增权限；不加 error 等级；不做结构化字段（一行文本足够，结构化会变成第二套日志格式）。
- 不在宿主侧过滤"敏感词"：拦不住，也只会给人错觉。规则写在手册里，靠作者。

## 4. 影响面

| 文件 | 改什么 |
| --- | --- |
| `app_plugins.go` | 方法表加 `plugin.log`（借 `storage`）；`pluginLog` 处理函数 |
| `internal/plugins/calllog.go` | `plugin.log` 归数据面（DEBUG） |
| `tools/plugin-market/pluginhost.go` | 市场程序的服务端副本（同样的边界） |
| `app_log_test.go` | `TestPluginLogWritesOneAttributedLine` |
| `market/docs/plugin-dev.md` | §6.13 小节、附录方法表、权限表的 `storage` 行 |
| `market/docs/plugin-testkit/fake-host.mjs` | `METHODS` 加 `plugin.log`（42 项，与 Go 一致） |
| `README.md` | 「日志」一节说明 `plugin <id>` 开头的行有两种来源 |

## 5. 验证

```powershell
go vet ./...（两个 module）; go test -count=1 .
node .dev\check-frontend.cjs
cd market; npm test; node docs/plugin-testkit/plugin-check.mjs plugins/*
```
