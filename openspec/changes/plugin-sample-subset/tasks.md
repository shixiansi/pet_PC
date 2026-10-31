# tasks.md — plugin-sample-subset

- [x] 九个 sample 包用 `market/dist` 同名文件覆盖，`sample/index.json` 的 `sha256` 同步
- [x] `TestBuiltinSampleMatchesIndex`：市场对照覆盖**每一条**
- [x] `TestBuiltinSampleAgreesWithTheMarketCatalogue`：改成子集语义（不比条数/版本）
- [x] AGENTS.md §3.7 写清两条规矩
- [x] proposal / tasks

## 后续

- [x] 把它们写进 CI —— 已由 `openspec/changes/ci-contract-job` 完成：`contract` job 把公开的
      插件仓检出到 `market/`，于是这里的字节对比与 `check-frontend.cjs` 都在 CI 里跑
