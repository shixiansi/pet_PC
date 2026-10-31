# tasks.md — plugin-sidecar

## 1. manifest

- [x] `Sidecar *SidecarSpec`（`script` + `args?`）
- [x] `validateSidecar`：路径在包内（`ValidRelPath`）、`.ps1`/`.exe`、必须写进 `assets`、
      必须申请 `process`、必须声明 `background`、args ≤8 个 / 每个 ≤256 字符 / 总长 ≤1024
- [x] 测试：`TestManifestSidecar`（合法声明 + 六种拒绝，含"跑出插件目录"那条只让 sidecar 违规）

## 2. 宿主

- [x] `pluginHost.sidecars map[string]string`（插件 id → 已启动的版本）
- [x] `ensurePluginSidecars()`：只对"已启用 + manifest 读得出来 + 声明了 sidecar + 用户授予了
      `process`"的插件动手
- [x] `startPluginSidecar()`：解析插件目录内的脚本、`.ps1` 走 `powershell.exe -NoProfile
      -ExecutionPolicy Bypass -File`、`.exe` 直接跑；`cmd.Dir` = 脚本所在目录；
      环境变量 `PETPC_PLUGIN_ID` / `PETPC_PLUGIN_DATA`；`pluginLaunchAttr()` 隐藏窗口
- [x] 挂到既有的 5 秒回收 tick 上（启动序列里先跑一次），与 `reapPluginChildren` 同一个定时器
- [x] 测试：`TestSidecarStartsAndIsReaped`（启动 → 幂等 → 停用后回收）、
      `TestSidecarNeedsTheGrantedProcessPermission`（没授权就一个都不启动）

## 3. 文档

- [x] 插件仓 `docs/plugin-dev.md`：顶层字段表加一行 + `sidecar` 小节（规则表、环境变量、
      生命周期、迁移提示"删掉 helperPath/helperScript"）
- [x] 宿主 `README.md`：`process` 那一行补一句
- [x] 本 change 的 proposal / design / tasks

## 4. 验证

```powershell
go vet ./...
go test -count=1 .
go test -count=1 ./internal/plugins/
node .dev\check-frontend.cjs
cd market; npm test
cd market; node docs/plugin-testkit/plugin-check.mjs plugins/*
```

## 5. 后续（不在本 change）

- [ ] **四个插件的迁移**（一次一个 patch 版本）：删 `helperPath`/`helperScript`、删自己那段
      `process.launch`、改读 `PETPC_PLUGIN_DATA`。清单：
      `pet-sense` / `media-sync` / `privacy-guard` / `screen-vision`（`clipask` 的 helper 是另一个
      用途，单独看）。
- [ ] 崩溃重启策略（退避 + 上限）：等真有需要再说
- [ ] sidecar 的 stdout/stderr：现在丢弃，与 `process.launch` 一致
- [ ] 插件仓的 `plugin-check.mjs` 加一条：声明了 `sidecar` 却没写进 `assets` 时报 FAIL
      （宿主已经会拒，但作者在本地就该看见）
