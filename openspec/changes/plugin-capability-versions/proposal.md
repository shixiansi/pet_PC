# proposal.md — plugin-capability-versions

> 给「插件问另一个插件」补上**版本协商**：`callTargets` 可以写版本下限 `@>=1.2.3`，
> `plugins.services` 的答案带上 `version`。
>
> 触发它的是 2026-10-31 的事故：看板插件对着一个**从来没声明过 `stats`** 的待办插件调了一周。
> 宿主的拒绝很准确（「目标插件 … 没有声明会回答 stats」），调用方也 catch 了——catch 进了一行
> 没人看的日志，每 30 秒一条，而仪表盘上那两格永远是空的。

## 1. 现状（实读事实）

| 事实 | 出处 |
| --- | --- |
| `callTargets` 只有 `<id>/<method>` 一种形状，没有版本 | `internal/plugins/manifest.go` 的 `SplitCallTarget` |
| `plugins.services` 回答 `{id,name,description,serves}`，**没有版本** | `app_plugins.go` 的 `pluginServices` |
| `plugin.call` 在调用时校验：目标启用、持有 `plugin.call`、`serves` 里有这个方法 | `internal/plugins/call.go` |
| 版本比较已经有一份实现（市场程序的更新检查也用它） | `internal/plugins/version.go` 的 `CompareVersions` |

## 2. 变更说明

- `callTargets` 的每一项可以带 `@>=x.y.z`（可选）。带了的项在**调用那一刻**校验目标版本；
  低了就拒绝，错误里写明「目标 X 是 a.b.c，低于你声明的 <id>/<method>@>=x.y.z」。
- `plugins.services` 的每个条目加 `version`，调用方可以自己先比再决定调不调。
- `SplitCallTargetVersion` 解析并校验下限；`SplitCallTarget` 保留（丢掉下限），
  因为安装对话框与既有校验只关心那一对。
- 版本比较复用 `version.go` 的 `CompareVersions`：两处实现「哪个版本更新」正是会让下限与
  更新检查互相矛盾的做法。

## 3. 明确不改

- 不引入 JSON Schema 式的"返回形状声明"：宿主就要校验每一次回答，而多一个字段就会破。
- 不在安装期校验下限：目标可能还没装，而插件必须能先装后补依赖。
- 不强制写下限：大多数调用不在乎版本，强制只会训练作者抄一个没核对过的数字。

## 4. 影响面

| 文件 | 改什么 |
| --- | --- |
| `internal/plugins/manifest.go` | `callFloorPattern`、`SplitCallTargetVersion`、`DeclaredCallFloor`、`SplitCallTarget`/`DeclaredCall` 改为基于它们；`validateCalls` 的报错文案带上格式提示 |
| `internal/plugins/call.go` | 第 5 步之后加版本下限检查（检查顺序的注释同步） |
| `app_plugins.go` | `pluginServices` 的答案加 `version` |
| `internal/plugins/call_test.go` / `manifest_test.go` / `app_plugins_test.go` | 下限满足/不满足/未声明三种；解析与拒绝；services 带版本 |
| `market/docs/plugin-dev.md` | §4 的 `callTargets` 行、§6.8 的版本下限小节、`plugins.services` 的返回形状 |
| `market/docs/plugin-testkit/plugin-check.mjs` | `callTargets` 检查认下限（并按"那一对"去重） |
| `README.md` | `plugin.call` 那一行 |

## 5. 验证

```powershell
go vet ./...
go test -count=1 ./internal/plugins/     # TestCallPluginHonoursTheDeclaredVersionFloor / TestCallTargetVersionFloorParsing
go test -count=1 .                       # TestPluginServicesReportsVersions
node .dev\check-frontend.cjs
cd market; npm test; node docs/plugin-testkit/plugin-check.mjs plugins/*
```
