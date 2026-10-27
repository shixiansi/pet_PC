# tasks: 桌宠右键菜单改成图标格子

## T1 Go：菜单成为气泡的一种 kind

- [x] `app_pomodoro.go`：`BubbleMenu = "menu"`、`bubbleHeightMenu = 226`、
      `bubbleLogicalHeight` 的分支、`clearAllBubblesLocked` 的列表
- [x] 验证：`go test . -run TestPetMenu`

## T2 Go：`OpenPetMenu` 弹出卡片，而不是交给托盘

- [x] 新增 `app_pet_menu.go`：`OpenPetMenu() (Layout, error)` + `petMenuBlocked(kind)`
- [x] 删除 `app.go` 里的旧实现（托盘转发）
- [x] 验证：`go test . -run 'OpenPetMenu|PetMenu'`

## T3 Go：删掉托盘的"替别人弹菜单"入口

- [x] `internal/desktop/tray_windows.go`：删 `ShowMenuAtCursor`、`wmShowPetMenu`
      与 `trayWndProc` 里的转发分支（`WM_APP + 2` 留空号并注明原因）
- [x] 验证：`go vet ./...`

## T4 前端：卡片里的九宫格

- [x] `index.html`：`#bubble-menu`（`#bubble` 的子元素，位于列表与输入框之间），
      九格 `data-menu` + 内联 SVG 图标 + 文字
- [x] `style.css`：`/* --- pet menu ---` 一节（`display:grid`、三列、`1fr` 行、
      hover/`:focus-visible`、`danger` 格）
- [x] 验证：`node .dev/check-frontend.cjs`（`the menu grid fills the card it is drawn in`）

## T5 前端：手势与动作表

- [x] `main.js`：`petMenuOpen` / `closePetMenu` / `togglePetMenu` / `PET_MENU_ACTIONS` /
      `runPetMenuAction` / `launchControlCenter`
- [x] `contextmenu` → `togglePetMenu`；`#bubble-menu` 上一处委托监听；
      `window.blur` 关闭（只对 `kind==='menu'`）
- [x] `renderBubble`：`menu` 分支 + 空标题隐藏
- [x] `wailsjs/go/main/App.d.ts`：`OpenPetMenu():Promise<main.Layout>`
- [x] 验证：`cd frontend && npm test`

## T6 测试

- [x] `app_pet_menu_test.go`：尺寸冻结、登记到 state+layout、折叠面板与浮层面、
      提问待答时拒绝且不改状态、`DismissBubble`/开面板/相位切换都收起
- [x] `frontend/src/pet-menu.test.js`：九格与图标、右键调 `OpenPetMenu`、每格调对绑定、
      再右键/Esc/失焦收起、三个提问气泡在时右键不调 `OpenPetMenu`
- [x] 验证：`go test ./... -count=1`（`internal/desktop` 的 HKCU 用例在沙箱里必然失败，
      见 AGENTS §7）、`cd frontend && npm test`

## T7 门禁

- [x] `.dev/check-frontend.cjs`：把"原生弹出菜单"那条换成五条——
      端到端接线、托盘不再被借用、相位切换会收起、每格都有动作、格子填满卡片
- [x] `.dev/run-frontend-harness.cjs` / `.dev/harness-checks.js`：假桥接加 `OpenPetMenu`
      返回菜单卡片；场景断言"格子真的画在卡片里、九格都有字、再右键收起"
- [x] 验证：`node .dev/check-frontend.cjs`；真浏览器 harness（`.dev/verify-live.ps1`）

## T8 文档

- [x] `README.md`：右键那一行改成页面菜单（九格、如何收起、与托盘菜单的分工）
- [x] `README.md`：门禁清单里加上这次新增的两组断言
- [x] `AGENTS.md §3.2`：写明"菜单是气泡，不是第四种占位"
- [x] `openspec/changes/pet-menu-grid/`：本目录

## T9 真机

- [ ] 右键桌宠：卡片出现在宠物旁边，九格可点，图标与文字对齐
- [ ] 选「日程」打开待办面板；选「对话」打开浮层面；选「番茄钟」开关生效且卡片收起
- [ ] 再右键一次收起；`Esc` 收起；点别的窗口（失焦）收起
- [ ] 番茄钟等总结/计划时右键：**不**出现菜单，光标进输入框
- [ ] 宠物贴左右两侧各试一次（卡片始终在宠物与面板之间那一栏，不压面板）
- [ ] 高 DPI（125% / 150%）下卡片与格子仍然对齐、无边缘透桌面
