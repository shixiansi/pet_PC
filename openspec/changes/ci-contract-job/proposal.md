# proposal.md — ci-contract-job

> CI 里多一个 `contract` job：把公开的插件仓检出到 `market/`，让两条"只在开发机上跑过"的检查
> 真的进 CI。

## 1. 现状（实读事实）

| 事实 | 出处 |
| --- | --- |
| `ci.yml` 头部写着：`check-frontend.cjs` 不覆盖，因为它读 `market/plugins/*`，而 `market/` 在新检出里不存在 | `.github/workflows/ci.yml` |
| `TestBuiltinSampleMatchesIndex` / `TestBuiltinSampleAgreesWithTheMarketCatalogue` 找不到 `market/` 时 **SKIP** | `internal/plugins/market_test.go` |
| 后果是实的：十个内置包里**九个**与市场漂了整整一轮，直到有人在本地跑才发现 | 提交 4001fec |
| 插件仓是**公开**仓库（`shixiansi/pet-pc-plugins`），检出它不需要任何 secret | GitHub |

## 2. 变更说明

- 新增 `contract` job（`windows-latest`）：检出本仓 → 检出插件仓到 `market/` → 跑
  `go test ./internal/plugins/ -run TestBuiltinSample` 与 `node .dev/check-frontend.cjs`。
- `ci.yml` 头部那份"刻意没覆盖"的清单同步：把这两条移出去，并写清为什么曾经不覆盖、现在为什么能。

## 3. 明确不改

- 不在 CI 里跑 `market/tests/*.test.js`：那是插件仓自己的 CI 在跑（npm test + plugin-check +
  check-catalogue）。
- 不 pin 插件仓的 ref：要的正是"今天的市场"，否则字节漂移又会被钉住的旧 commit 掩盖。
- 不加 `market/tools/package.py` 的往返（它要在 market 仓里生成 dist，CI 只读）。

## 4. 影响面

| 文件 | 改什么 |
| --- | --- |
| `.github/workflows/ci.yml` | 新增 `contract` job；头部清单更新 |
| `AGENTS.md` §4 | 说明 `contract` job 覆盖了什么 |
| `openspec/changes/plugin-sample-subset/tasks.md` | 那一条后续勾掉 |

## 5. 验证

```powershell
# 两组命令本地都有 market/ 时是绿的，CI 里的环境由 job 自己造：
go test ./internal/plugins/ -run 'TestBuiltinSample' -count=1 -v
node .dev/check-frontend.cjs
# YAML 结构用 python -c "import yaml…" 解析过：3 个 job，contract 6 个 step
```
