# proposal.md — plugin-storage-usage

> 补一个 `storage.usage`（私有目录用了多少 / 上限多少），并给"文档里的能力计数"装一道守卫。
>
> 这一轮还带一条**审计更正**：原计划里的"退休 `memory.read` / `memory.append`"被证据否掉了。

## 1. 现状（实读事实）

| 事实 | 出处 |
| --- | --- |
| `storage.stat` 是一个键，`storage.pressure` 是 80% 那一次——**都不回答"我还要删多少"** | `internal/plugins/privatestore.go` |
| 宿主早就在算整棵树（`PluginDataBytes`），只是没暴露 | 同上 |
| 代码里 42 个方法，而 AGENTS/README/插件仓 README 里还写着 **41** | 本次 grep（5 处） |
| 计数没有任何东西守着：39 → 41 → 42 每次都落后一轮 | 本仓 AGENTS.md §5 的腐坏清单 |
| `memory.read` / `memory.append` 在 36 个插件的 JS 里 **0 次**，但 `chat` 插件把它们当**模型工具**用（`memory_read` / `memory_append`），`app_plugin_tools.go` 的工具目录映射到这两个方法 | 本轮核对，推翻了"死面"的结论 |

## 2. 变更说明

- 新方法 `storage.usage` → `{used, limit}`，借用 `storage` 权限；`used` 与"超限被拒"用同一套
  计算（`privateDirBytes`），所以两个数字不会互相漂。两个宿主都服务（`RegisterPrivateStore` +
  桌宠的方法表）。归数据面（DEBUG）。
- 文档：手册 §6.3 的方法表与用法段、附录方法总表、`fake-host` 的 `METHODS`（43）；
  clipask 0.1.19 在 `storage.pressure` 里真的用它算"还能再用多少"。
- 计数：六份文档一律 43，并新增 `TestDocumentedMethodCountMatchesTheTable`
  （`pluginMethods` 条数 / 文档里的「N 个方法」/ `fake-host.mjs` 的 `METHODS`，三者一起比）。

## 3. 明确不改

- **不退休 `memory.*`**：它不是死面，是给模型的工具（见 §1 最后一行）。这条写进 AGENTS.md §5
  当作反面教材：**别用"grep 不到字面量"判断能力死活**。
- `audio.remove` / `share.remove` 也没有调用方，但它们是"清理自己"的唯一手段，留着。
- 不给 `storage.usage` 加"按子目录拆分"：那是 `storage.list` + `storage.stat` 的活，而且会让
  这个方法的成本随目录结构增长。

## 4. 效果

插件收到 `storage.pressure` 之后，一次调用就能知道"还要删多少"，而不是 `list` + 逐键 `stat`
几十次；文档里的能力计数与方法表**由测试绑定**，不会再出现"代码 43、文档 41"。
