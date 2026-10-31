# proposal.md — plugin-storage-quota

> 给插件的私有目录加上限：单值 ≤ 1 MiB、每插件整棵树 ≤ 64 MiB，超了拒绝并写明数字。

## 1. 现状（实读事实）

| 事实 | 出处 |
| --- | --- |
| `storage.set` 只校验键的形状（≤4 段、每段 `[A-Za-z0-9_-]`），**不校验值的大小** | `internal/plugins/privatestore.go` 的 `StorageSet` |
| `share.set` 有 64 KiB 上限，`storage` 没有——同一份代码里两种态度 | `share.go` 的 `MaxShareValue` |
| `audio.save` 有单文件上限（`maxAudioBytes`），但没有"这个插件一共占多少" | `app_plugins.go` 的 `pluginAudioSave` |
| 插件的私有目录是 `plugin-data/<id>/`，里面有 `audio/`、`share/` 与它自己的键 | README「插件数据放在哪」 |

## 2. 变更说明

- `MaxStorageValue = 1 MiB`：一次 `storage.set` 的值上限。
- `MaxPluginDataBytes = 64 MiB`：一个插件整棵私有目录的上限，**audio 也算**。
- 检查在**写之前**（拒绝就什么都不发生，与闸门的其它检查同一个顺序）；覆盖同一个键时扣掉旧值，
  否则"一个 800 KiB 的缓存永远无法更新"。
- 拒绝信息分两种，因为两种修法不同：单值超限（"拆成几个键，或换一种存法"）、目录超限
  （"先删掉不用的键"）。两个数字都写进消息。
- `audio.save` / `audio.import` 也走同一个计数器（`plugins.PluginDataBytes`）：只堵 `storage.set`
  的话，插件可以一个片段一个片段地把盘占满。

## 3. 明确不改

- 不做"按插件的配额可配置"：这是天花板，不是套餐。
- 不自动清理：删什么只有插件自己知道（`storage.list` / `storage.remove` 就是给它用的）。
- 不在 `storage.get` 上加限制（读不产生磁盘）。

## 4. 影响面

| 文件 | 改什么 |
| --- | --- |
| `internal/plugins/privatestore.go` | `MaxStorageValue`、`MaxPluginDataBytes`、`checkStorageBudget`、`privateDirBytes`、`PluginDataBytes`；`StorageSet` 写前校验 |
| `app_plugins.go` | `pluginAudioSave` / `pluginAudioImport` 走同一个计数器 |
| `internal/plugins/privatestore_test.go` | 两种拒绝 + 覆盖大值放行 + 小目录放行；`StorageSet` 真方法上的越界拒绝（且没写文件） |
| `README.md` | 「插件数据放在哪」补一段上限 |
| `market/docs/plugin-dev.md` | §6.3 顶部补上限与"两个数字都不是预算" |

## 5. 验证

```powershell
go vet ./...
go test -count=1 ./internal/plugins/     # TestStorageBudgetRefusesTwoDifferentMistakes / TestStorageSetRefusesAnOversizedValue
go test -count=1 .                       # audio 既有用例仍绿
node .dev\check-frontend.cjs
cd market; npm test; node docs/plugin-testkit/plugin-check.mjs plugins/*
```
