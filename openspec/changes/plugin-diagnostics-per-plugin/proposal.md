# proposal.md — plugin-diagnostics-per-plugin

> 诊断报告从"已装插件一行（版本/启停/权限数）"变成**逐插件点名缺口**：没勾的权限、包里没带的
> 辅助脚本、指向没装插件的 `callTargets`、被停用的插件、以及它占了多少磁盘。

## 1. 现状（实读事实）

| 事实 | 出处 |
| --- | --- |
| 报告已经有「已装插件」一节：id、版本、启停、权限数与已授数、manifest 错误 | `settingsapi_diagnostics.go` 的 `Diagnostics` |
| 但这些都不说**具体缺什么**：`权限 7（已授 3）` 之后没有下一步 | 同上 |
| 2026-10-31 的四个"装好了但什么也不动"的插件，报告里全是"正常" | 真机日志与本次会话 |
| 插件的私有目录已经能量出大小（配额那一条引入的 `plugins.PluginDataBytes`） | `internal/plugins/privatestore.go` |

## 2. 变更说明

`pluginProblems(m, row, installed)` 返回该插件的问题行（每条都是一句可执行的话）：

- `停用中：它的常驻帧不会跑，别的插件也调用不到它`
- `缺授权：plugin.call、storage（装的时候没勾，或后来取消了）`
- `辅助脚本不在：xxx-helper.ps1（包里没有它，宿主会一直启动失败）`
- `callTargets 指向没装的插件：…（调用会失败）`
- `私有目录占用 X（上限 Y）`（信息行，不是问题）

## 3. 明确不改

- 不做结构化 JSON（报告的存在意义是被粘贴；一个函数生成、一个测试断言字节）。
- 不猜"哪个配置项没填"：宿主不知道某个 config 项是必填的（manifest 没有这个语义）。
- 不把报告升级成"自动修复"：这些问题的修法各不相同，说清楚比偷偷改好。

## 4. 影响面

| 文件 | 改什么 |
| --- | --- |
| `tools/plugin-market/settingsapi_diagnostics.go` | `pluginProblems`、`humanBytes`；报告里逐插件输出 |
| `tools/plugin-market/settingsapi_diagnostics_test.go` | 逐条断言四类问题；另有一条走真 `Diagnostics()` 证明它到得了报告 |
| `README.md` | 报告那一节补"它还会点名每个插件的具体缺口" |
| `market/docs/plugin-dev.md` | §12 的自检清单末尾加一节：用户说"没反应"时先要诊断报告 |

## 5. 验证

```powershell
cd tools/plugin-market; go vet ./...; go test -count=1 ./...
node .dev\check-frontend.cjs
cd market; npm test
```
