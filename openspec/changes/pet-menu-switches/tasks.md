# 任务与验收记录（右键菜单开关页，2026-11-03）

## A. 宿主

- [x] `store.Config.MenuHidden []string`（`json:"menuHidden,omitempty"`），denylist 的取舍写在
      字段注释里（新格子默认可见、allowlist 会让新动作对老用户永久消失）。
- [x] `app_pet_menu.go`：`petMenuCell` + `petMenuCells`（10 行：九格 + 底部条）、
      `NormalisePetMenuHidden`（纯函数）、`PetMenuCells()`、`SetPetMenuHidden(hidden)`。
- [x] `app.go` 的 `GetState`：**在交出 cfg 的那一行之前**归一化
      （放在函数开头会被"读注册表之后可能换掉 cfg"那一段吃掉——第一版就是这么红的）。
- [x] `go test . -run 'TestPetMenu|TestNormalisePetMenuHidden|TestSetPetMenuHidden|TestGetStateDropsAHidden'`
- [x] `gofmt -l`（排除 `.dev/`）为空；`go vet ./...`；`go test ./... -count=1` 七包 ok

## B. 页面

- [x] `index.html`：`#bubble-menu` 里三层（`#menu-grid` / `#menu-prefs` / `#menu-wide`）。
      不引第十个格子——卡片高度是 Go 的常量（226），加一格要从九格那里抢高度。
- [x] `style.css`：`.bubble-menu` 变 flex 列，`.menu-grid` 保留 3 列网格
      （`grid-auto-rows: minmax(0, 1fr)`），`.menu-wide` 是 `flex: 0 0 auto` 的一条。
- [x] `main.js`：模式切换（`setMenuMode`）、开关（`toggleMenuItem`）、本地镜像
      （`menuHiddenLocal`）、格子表来自宿主（`App.PetMenuCells()`）。
- [x] `npm test` → 9 files / **83 passed**（`pet-menu.test.js` 29 条，其中 7 条新）
- [x] `npm run build` → dist 重新生成（harness 与真机都用它）

## C. 门禁

- [x] `node .dev/check-frontend.cjs` → **ALL FRONTEND CHECKS PASSED (155)**（+2 条新检查）
- [x] `node .dev/run-frontend-harness.cjs` → **FRONTEND HARNESS PASSED (62 checks)**，
      其中 7 条是新加的：九格在 226px 卡片里都有真实盒子、开关页的行不溢出、点一下真的
      调了 `SetPetMenuHidden(["theme"])`、被关掉的格子在动作层立刻隐藏。
- [x] `node .dev/check-contrast.cjs` → 472 个组合全部达 AA
- [x] `tools/build-all.ps1 -SkipFrontend` → 两个 exe 重建

## D. 为什么要那 7 条真引擎检查

jsdom 里 `getBoundingClientRect()` 恒为 0，所以"格子被挤成 0 高""开关页长出卡片外"这两件
事在 `npm test` 那一层**看不见**。这一轮把卡片从"一层网格"改成"三层 flex"，恰恰是那种
"jsdom 全绿、真机上格子变成一条线"的改动——所以真引擎那一组现在盯的是高度与包围盒，而不
只是"元素在不在 DOM 里"。
