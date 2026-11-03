# 右键菜单的开关页（2026-11-03）

## Why

右键菜单是桌宠唯一的入口面板：九格 + 底部一条。九个格子里，`归位`、`主题`、`提醒`
对多数人是"一年用一次"，而 `日程`、`对话` 是每天几十次。菜单不能排序（格子是 markup，
顺序就是那九格），所以一个只有"少几个格子"这一种自由度的定制恰恰是它需要的那个。

计划稿（docs/plugin-roadmap.md 附「判断一个想法该不该做成插件」）问的第一个问题是
"它需要宠物窗口之外的界面吗"——这里不需要：菜单本身就是一张卡片，开关页是同一张卡片的
另一层。

## What Changes

- **`settings.json` 多一个键 `menuHidden`**（`store.Config`）：被关掉的格子 id 列表。
  **它是 denylist 而不是 allowlist**——新版本加的格子默认可见；allowlist 会让"曾经进过
  这一页的人"从此看不见任何新动作，而且他不会知道少了什么。
- **`app_pet_menu.go` 多一张表 `petMenuCells`**：id / 标签 / **是否固定**。它只回答一件事
  ——"这一格能不能被关掉"。`settings` 与 `quit` 是固定的：前者是回到这一页的唯一入口，
  后者是桌宠那一侧唯一的退出口。关掉它们等于把门锁在身后。
- **归一化是判据的落点**（`NormalisePetMenuHidden`）：未知 id、固定 id、重复项都在这里被
  剔掉，而且**读和写都过它**（`SetPetMenuHidden` 与 `GetState`）。settings.json 是用户能
  直接编辑的纯文件，只在一处拦就是留了一个洞。
- **页面那一层是同一张卡片的第二个模式**，不是第十个格子：卡片高度是 Go 里钉死的常量
  （`bubbleHeightMenu` = 226，`TestPetMenuCardSizeIsFixed` 守着），第十格只能从那九个
  那里抢高度；换一层不占空间，而且两种模式本来就该一样大。
- 格子表由宿主提供（`App.PetMenuCells()`），页面**不保留第二份**标签表。

## Impact

- 宿主：`internal/store/store.go`（一个字段）、`app_pet_menu.go`（表 + 归一化 + 两个绑定）、
  `app.go`（GetState 归一化）、`frontend/index.html`（两层 + 底部一条）、
  `frontend/src/style.css`（`.bubble-menu` 变 flex 列、新增 `.menu-grid` / `.menu-wide` /
  `.off` / `.menu-state`）、`frontend/src/main.js`（模式切换、开关、本地镜像）。
- 守门：`app_pet_menu_test.go`（表的自洽、归一化的六种输入、写盘 + 应答、读路径）、
  `frontend/src/pet-menu.test.js`（+7 条：模式切换、每次重开回到动作层、固定项不可点、
  关掉立刻生效并按菜单顺序存、State 决定显示、手改文件藏不掉那两格）、
  `.dev/check-frontend.cjs`（+2 条：Go 表 ↔ markup 的 id 集合、开关页接线；
  并把旧的"九格"断言改成"十格 + 动作表多一个 `done`"）、
  `.dev/run-frontend-harness.cjs`（+7 条真引擎检查：九格在 226px 里都有真实盒子、
  开关页不溢出、点一下真的调了 `SetPetMenuHidden`）。
- 文档：README 的右键菜单两行（3×3 → 3×3 + 底部一条；新增「设置」的说明）。

## 明确不做

- **排序与自定义格子**：菜单顺序是 markup，动它要重排卡片图标；而"少几个格子"已经解决了
  真实痛点。
- **控制中心里的同一份设置**：这一页就在宠物旁边，多一个窗口去改它没有收益；而两个入口
  意味着两处要同时正确。
