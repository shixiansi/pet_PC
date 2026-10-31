# tasks.md — plugin-capability-versions

## 1. 宿主

- [x] `callFloorPattern` + `SplitCallTargetVersion`（解析 `<id>/<method>@>=x.y.z`）
- [x] `SplitCallTarget` / `DeclaredCall` 改为基于它（既有调用方零改动）
- [x] `DeclaredCallFloor` 返回某对的版本下限
- [x] `validateCalls` 的报错文案带上格式提示
- [x] `call.go` 第 5 步之后校验下限（目标版本低 → 拒绝并写明两边版本）
- [x] `pluginServices` 的答案加 `version`
- [x] 测试：`TestCallPluginHonoursTheDeclaredVersionFloor`（满足/不满足/未声明）、
      `TestCallTargetVersionFloorParsing`（解析 + 四种坏下限 + 去重按对）、
      `TestPluginServicesReportsVersions`

## 2. 文档与工具

- [x] `market/docs/plugin-dev.md`：§4 的 `callTargets` 行、§6.8 的版本下限小节、
      `plugins.services` 的返回形状
- [x] `market/docs/plugin-testkit/plugin-check.mjs`：`callTargets` 检查认下限，坏下限报 FAIL，
      去重按"那一对"（与宿主一致）
- [x] `README.md`：`plugin.call` 那一行
- [x] 本 change 的 proposal / design / tasks

## 3. 验证

```powershell
go vet ./...
go test -count=1 ./internal/plugins/
go test -count=1 .
node .dev\check-frontend.cjs
cd market; npm test; node docs/plugin-testkit/plugin-check.mjs plugins/*
```

## 4. 后续（不在本 change）

- [ ] 给已有的跨插件调用补下限：`web-dashboard` → `todo/stats@>=1.2.2`、`pomodoro/stats@>=2.3.13`
      （那两个方法就是这一轮才加的），以及 `memcore/chat` 这类长期契约
- [ ] `plugins.services` 是否要顺带回答"这个方法的最小版本"——今天要靠约定
