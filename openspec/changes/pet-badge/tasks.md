# tasks: 宠物头顶的一行字（`pet.badge`）

## A. 宿主

- [x] **A1 — 能力与授权**
  `app_plugins.go` 方法表加 `"pet.badge": {perm: plugins.PermPetSurface, run: pluginBadge}`。
  判据：没有 `pet.surface` 的插件被闸门拒绝，且拒绝文案里出现 `pet.surface`
  （`TestPetBadgeNeedsTheSurfaceGrant`）。

- [x] **A2 — 状态与事件**
  `app_pet_badge.go`：按 plugin id 存、`MaxBadgeRunes = 16`、只在可见文字变化时
  `emitEvent("pet:badge", {text})`、`PetBadge()` 供首帧读。
  判据：相同文字重复设置不重复发事件；清空发空串（`TestPetBadgeShowsTheTextAndEmitsOnlyOnChange`）。

- [x] **A3 — 读时过滤**
  `liveBadge()` 跳过已停用/已卸载的 id，并在读取时把它们从表里删掉。
  判据：停用后 `PetBadge()` 返回空（`TestPetBadgeDropsTextFromADisabledPlugin`）。

- [x] **A4 — 超长拒绝**
  判据：`MaxBadgeRunes + 1` 个字符被拒，且没有画上去（`TestPetBadgeRejectsOverlongText`）。

## B. 前端

- [x] **B1 — `#pet-clock`**
  `index.html` 里加在 `#pet` 框内（img 之后、todo 徽章之前），注释写清"为什么必须在框里"。

- [x] **B2 — 渲染与订阅**
  `main.js`：`els.clock`、`renderClock(text)`（空串隐藏）、`EventsOn('pet:badge', …)`、
  `refresh()` 里 `App.PetBadge?.().then(renderClock)`（用 `.catch` 兜住没有该绑定的旧二进制）。
  判据：`frontend/src/pet-clock.test.js` 四条（首帧、空值、跟事件、无 payload 不打印 `undefined`）。

- [x] **B3 — 样式**
  `style.css`：`top: 2px` 水平居中、`tabular-nums`、`pointer-events: none`、用 border 而不是
  box-shadow（阴影会画到框外被裁掉，看着像半个胶囊）。

- [x] **B4 — 门禁接线**
  `.dev/check-frontend.cjs` 的 `goFiles` 加 `app_pet_badge.go`（否则 `pet:badge` 会被判成
  "没有 Go 发过"）；两个 harness 的绑定名单加 `PetBadge`，headless 那份还要一个返回 `''` 的桩。
  判据：`node .dev/check-frontend.cjs` → 125 条全过。

## C. 插件（番茄钟 2.3.0）

- [x] **C1 — 设置项**
  `manifest.json` 加 `showCountdown`（bool，默认 `"false"`——新画在宠物身上的东西应当由用户
  打开）。判据：那条"声明的配置键都被读过"的断言跟着改成"机器读的 + 页面读的"。

- [x] **C2 — 推送**
  `app.js`：`clockText(ms)`、`pushBadge()`（只在 timer 角色、有 `pet.surface`、文字变化时推），
  在 `tick()` 的三条出口与 `applyOutcome` 之后调用；暂停/停止/阶段结束推空串。

## D. 契约与分发

- [x] **D1 — 手册**
  `market/docs/plugin-dev.md`：能力表加一行（含"借 `pet.surface` 授权、空串清除、16 字符"），
  附录方法表加一行；方法总数 34 → 35（README、AGENTS、testkit README 同步）。

- [x] **D2 — 自检工具**
  `market/docs/plugin-testkit/plugin-check.mjs` 的 `METHODS` 加
  `'pet.badge': { perm: 'pet.surface', where: 'pet' }`。

- [x] **D3 — 打包与重建**
  `tools/package.ps1` 打出 2.3.0 → 同步 `internal/plugins/sample` 与 `embed.go` →
  `wails build -s` 重建 `PetPC.exe` 与 `PetPCPluginMarket.exe` → 推送插件仓（`ad60364`）。

## E. 未做

- [ ] 两个插件同时要头顶时的仲裁（现在后设置的赢，前一个不报错）。
- [ ] 宠物隐藏时清空再恢复（现在状态保留——计时没停，这是有意的）。
