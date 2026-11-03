# 右键菜单显示哪些格子（设置住在控制中心，2026-11-03）

## Why

右键菜单是桌宠唯一的入口面板：九格。九个格子里 `归位`、`主题`、`提醒` 对多数人是"一年用
一次"，而 `日程`、`对话` 是每天几十次。菜单不能排序（顺序就是 markup），所以"少几个格子"
是它唯一需要的自由度。

**为什么设置页在控制中心，而不在这张卡片里**（这一条是先做错、再改回来的）：

第一版把开关页放进了右键菜单本身——同一张卡片换一层，底部加一条 26px 的「设置」，点进去
九格换成十个开关。技术上成立（卡片还是那个矩形，窗口只裁一次），但用户的第一反馈是
**"加载右键菜单里显示效果不好"**：226px 分成 3 行格子 + 一条底栏之后，每格只剩 52px，
比原来的 63px 挤；而"改设置"本来就不是一个要反复做的事，为它牺牲主要界面的呼吸空间是
错的账。

而"桌宠长什么样"本来就是控制中心「开始」页的事（大小、图片、贴边、开机启动、快捷键）。
菜单也是它的一部分。所以开关搬过去，右键菜单回到九格——**一行 CSS 都不用为它改**。

## What Changes

- **`internal/petmenu`**（新包，两个程序共用）：九格的 `Cell{id,label,fixed}` 与
  `Normalise(ids)`。为什么是共享包而不是 package main：桌宠按它判"哪些不许关"，控制中心按它
  画开关行——两套规则必然分叉，而分叉的那套会让用户在两个窗口里得出不同结论
  （`internal/plugins/share.go` 让两个宿主共用牌盘规则，是同一个理由）。
- **`settings.json` 的 `menuHidden`**（`store.Config`）：被关掉的格子 id。**denylist 而不是
  allowlist** —— 新版本加的格子默认可见；allowlist 会让"曾经进过这一页的人"从此看不见任何
  新动作，而且他不会知道少了什么。
- **只有 `quit` 是 fixed**：它是桌宠那一侧唯一的退出口。其余八格都关得掉，包括 `控制中心` ——
  托盘菜单一直开得出来，而"改这些开关"的地方本身就在控制中心里，所以关掉任何一格都不会把
  人关在外面。
- **归一化两处都过**：控制中心写入时（`Market.SetPetMenuHidden`），桌宠读取时
  （`App.GetState`）。`settings.json` 是用户能直接编辑的纯文件，只拦一处就是留一个洞。
- **控制中心「开始」页**多一张「右键菜单」卡片：一行一格，`fixed` 的那行 `disabled` 并写明原因。

## Impact

- 宿主：`internal/petmenu/`（新包 + 测试）、`internal/store/store.go`（一个字段）、
  `app_pet_menu.go`（去掉表与两个绑定）、`app.go`（GetState 过 `petmenu.Normalise`）、
  `frontend/index.html` + `style.css` + `main.js`（菜单回到九格；`applyMenuHidden()` 只照
  State 藏格子）。
- 控制中心：`tools/plugin-market/settingsapi_pet.go`（`PetMenuCells` / `SetPetMenuHidden`）、
  `cc/src/lib/bridge.ts`（两个函数 + dev 桩）、`cc/src/pages/Start.svelte`（那张卡片）。
- 守门：`internal/petmenu/petmenu_test.go`（表自洽 / 只有 quit 是 fixed / 归一化七种输入 /
  Cells 是拷贝）、`app_pet_menu_test.go`（GetState 的读路径）、
  `frontend/src/pet-menu.test.js`（State 说藏就藏、默认九格、藏掉几格后别的格子仍能点）、
  `.dev/check-frontend.cjs`（+2：共享表 ↔ markup ↔ 两个程序的绑定；两头都接上，并且卡片里
  **没有**第二层）、`.dev/run-frontend-harness.cjs`（真引擎：九格都在 226px 的盒子里、
  卡片里没有 leftover 的开关层）。
- 文档：README 的右键菜单一行 + 控制中心那一段。

## 明确不做

- **菜单排序 / 自定义格子**：顺序是 markup，动它要重排卡片图标；"少几个格子"已经解决了真实
  痛点。
- **两个入口**（菜单里再放一个「设置」）：那是第一版；两处都要正确，而收益只是少点一次鼠标。
