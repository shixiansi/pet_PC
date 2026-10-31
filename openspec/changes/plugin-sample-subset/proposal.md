# proposal.md — plugin-sample-subset

> 让 `go test ./internal/plugins` 不再有既有红灯：sample 与市场的关系是**子集**，不是副本；
> 并把九个漂掉的 sample 包按市场重新对齐。

## 1. 现状（实读事实）

| 事实 | 出处 |
| --- | --- |
| `TestBuiltinSampleMatchesIndex` 只拿 `doc.Plugins[0]` 和 `market/dist` 比字节 | `internal/plugins/market_test.go` |
| 十个 sample 包里**九个**与 `market/dist` 的同名文件字节不同（只有 memcore 0.1.7 一致） | 本次实测 |
| `TestBuiltinSampleAgreesWithTheMarketCatalogue` 要求两边条数相等 | 同文件 |
| 市场现在 36 个插件、sample 10 个 → 那条断言每天红 | 市场 `index.json` |
| 市场的 `index.json` 每个插件只留**最新**一条，sample 故意更旧 | `tools/package.py` |

所以红灯有两个来源，一个是**真漂**（九个包），一个是**过时的断言**（条数相等）。

## 2. 变更说明

- 九个 sample 包用 `market/dist` 的同名文件覆盖（版本不变，字节对齐），`sample/index.json` 的
  `sha256` 同步更新。`embed.go` 的文件清单不变（版本号没变）。
- `TestBuiltinSampleMatchesIndex` 的市场对照从"只看第一条"改成**对每一条**都比——这正是九个包
  能悄悄漂掉的原因。
- `TestBuiltinSampleAgreesWithTheMarketCatalogue` 改成**子集语义**：sample 里的每个插件市场还认；
  不比条数、不比版本（原因写在测试的注释里）。

## 3. 明确不改

- 不把 sample 升到最新版本：那是"每次发版都往二进制里塞几 MB"，而兜底源的价值是"断网也能装一个
  能用的"，不是"和最新一模一样"。
- 不改 `tools/package.ps1` 的"把打好的包复制进宿主 sample"那条行为：它是这套东西的生成方式。

## 4. 影响面

| 文件 | 改什么 |
| --- | --- |
| `internal/plugins/sample/*.zip` | 九个包按市场重新对齐（二进制） |
| `internal/plugins/sample/index.json` | 九个 `sha256` |
| `internal/plugins/market_test.go` | 逐条对照 + 子集语义（两个测试的注释也写清了为什么） |
| `AGENTS.md` §3.7 | 两条规矩（同版本字节一致、sample 是子集） |

## 5. 验证

```powershell
go vet ./...
go test -count=1 ./internal/plugins/      # 从此全绿（原先 2 条红）
go test -count=1 .
```
