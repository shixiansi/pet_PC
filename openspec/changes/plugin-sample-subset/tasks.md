# tasks.md — plugin-sample-subset

- [x] 九个 sample 包用 `market/dist` 同名文件覆盖，`sample/index.json` 的 `sha256` 同步
- [x] `TestBuiltinSampleMatchesIndex`：市场对照覆盖**每一条**
- [x] `TestBuiltinSampleAgreesWithTheMarketCatalogue`：改成子集语义（不比条数/版本）
- [x] AGENTS.md §3.7 写清两条规矩
- [x] proposal / tasks

## 后续

- [ ] 把它们写进 CI：`go test ./internal/plugins` 在托管 runner 上没有 `market/`，这两条会自动
      SKIP——所以"字节漂了"只能在本地发现。要让它进 CI，就得在 CI 里也检出插件仓（两个仓库分开，
      需要在 workflow 里多一步 checkout + 路径约定）
