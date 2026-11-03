# 任务与验收记录（右键菜单显示哪些格子，2026-11-03）

## A. 共享规则

- [x] `internal/petmenu`：`Cell{ID,Label,Fixed}`（带 json 标签——它经 Wails 过到 TS 那一侧）、
      `Cells()`（返回拷贝）、`Normalise(ids)`（未知 / fixed / 重复丢掉，按菜单顺序输出）。
- [x] `internal/petmenu/petmenu_test.go`：表自洽（9 格、id 形状、无重复）、
      **只有 quit 是 fixed**（这条断言的意义是：再想加一个 fixed 的人得先回答"关掉它之后
      用户从哪回来"）、归一化 7 种输入、`Cells()` 是拷贝。
- [x] `go test ./internal/petmenu -count=1`

## B. 桌宠那一侧

- [x] `app_pet_menu.go` 只剩 `OpenPetMenu`：表与两个绑定搬走了（`PetMenuCells` /
      `SetPetMenuHidden` 不再由桌宠导出）。
- [x] `app.go` 的 `GetState`：`cfg.MenuHidden = petmenu.Normalise(cfg.MenuHidden)`，而且放在
      **交出 cfg 的那一行之前**（放函数开头会被"读注册表之后可能换掉 cfg"那一段吃掉）。
- [x] `frontend/index.html` / `style.css` / `main.js`：菜单回到九格，`applyMenuHidden()` 只照
      `State.config.menuHidden` 藏格子——**不复制"哪些不能关"**（Go 已经归一过）。
- [x] `frontend/src/pet-menu.test.js`：State 点名就藏、默认九格、藏掉几格后别的格子仍能点。
- [x] `cd frontend && npm test` → 9 files / **79 passed**

## C. 控制中心那一侧

- [x] `tools/plugin-market/settingsapi_pet.go`：`PetMenuCells()` + `SetPetMenuHidden()`
      （写入时过 `petmenu.Normalise`，回一份保存后的 config）。
- [x] `cc/src/lib/bridge.ts`：两个函数 + dev 桩（九格，与 Go 同形同序）+ `menuHidden: []`
      加进假 config。
- [x] `cc/src/pages/Start.svelte`：`<Card title="右键菜单">`，一行一格，fixed 的 `disabled`
      并写明原因；`toggleMenuCell` 按宿主那张表的顺序发 denylist。
- [x] `cd tools/plugin-market/cc && npm run check` → **0 errors 0 warnings**

## D. 门禁

- [x] `gofmt -l`（排除 .dev/）为空；`go vet ./...`；`go test ./... -count=1` 七包 ok
- [x] `cd tools/plugin-market && go vet ./... && go test ./... -count=1`
- [x] `node .dev/check-frontend.cjs` → **ALL FRONTEND CHECKS PASSED (155)**
      （两张新检查：共享表 ↔ markup ↔ 两个程序；两头都接上、且卡片里**没有**第二层）
- [x] `node .dev/run-frontend-harness.cjs` → **FRONTEND HARNESS PASSED (57 checks)**
      （真引擎：九格都在 226px 的盒子里、没有 leftover 的开关层）
- [x] `node .dev/check-contrast.cjs` → 472 组合全部达 AA
- [x] `pwsh -File tools/build-all.ps1` → cc 构建 → 覆盖 frontend/dist → 两个 wails build

## E. 这一版为什么把上一版推翻了

上一版把开关页塞进了右键菜单卡片（底部一条 + 九格换开关页）。它过了所有门禁——包括真引擎的
七条几何检查——但用户的判断是"显示效果不好"，而那判断是对的：226px 里三层之后每格只剩 52px
（原来 63px），而"改设置"不是一件要反复做的事。**门禁能证明"没坏"，证明不了"好看"**；这一次
是那句反馈先把方向改对，然后才是把这套门禁补上（现在它盯的是"卡片里不该有第二层"）。

一个实现细节值得留下的：桌宠那一侧曾经用过一个"本地镜像"（用户点完先本地生效，等宿主 State
回来再对齐）。开关搬去控制中心之后它没有存在的理由了——那一页本来就是"写 → 拿回 config →
重画"，没有"点完要等一次往返"的问题。

## F. 建成之后的两条反馈（2026-11-03，同一天）

1. **「控制中心」也不能关**。第一版只固定了「退出」，理由是"托盘菜单一直开得出来"。用户否了：
   改这些开关的地方就在控制中心里，关掉它虽然还能从托盘进去，但那条路是他下次想改设置时**不会
   想到**的地方。现在 fixed = `[plugins, quit]`，`petmenu_test` 里那条断言也跟着变成
   "固定的恰好是回家与出门这两格"。
2. **改完不实时生效**。写入者是控制中心，读者是桌宠那一页，而两者之间没有任何东西通知它：
   在控制中心关掉一格、回来右键，菜单还是九格（用户原话）。补的是**推**，不是轮询：
   `app_settingswatch.go` 每秒看一眼 `menuHidden`，变了就 `emitEvent(eventPetState, GetState())`，
   页面 `EventsOn('pet:state', applyState)`。
   - 为什么是整份 State 而不是只发那个数组：这一页对状态的消费全部走 `applyState`，多一条
     "只发差量"的通道就是第二个真相源。
   - 为什么 `syncMenuHidden` 被拆成方法：ticker 要一个 Wails ctx 才转得动，而"变了就推、
     没变就不推"正是唯一会错的地方——推两次让页面白重画，不推就是用户报的那个 bug。
     用例 `TestSyncMenuHiddenPushesState` / `TestSyncMenuHiddenIgnoresAFixedCell` 直接调它。
   - 页面那一半：`pet-menu.test.js` 里一条"收到 pet:state 就当场生效"，断言被点名的格子藏起
     来、没被点名的不动。