# design: 桌宠右键菜单 = 气泡的一种 kind

## 1. 为什么是「气泡的一种 kind」

桌宠的窗口永远只有那么大，而**可见区域**是 `SetWindowRgn` 按"当前那一个占位"的包围盒
裁出来的（`AGENTS.md §3.2`、`docs/reminder-pomodoro.md` 8.2）。所以页面里任何新控件都必须
先回答"它在哪个矩形里"。菜单有三种可能的住处：

1. **第四个占位。** 给 `App` 加一个 `petMenu` 字段、在 `applyWindowGeometry` 的 switch 里
   加一条分支、给 `Layout` 加一个字段、再把它接进每一个"折叠别的占位"的地方
   （`SetPanelOpen`、`showBubble`、`OpenPluginSurface`、`HidePet`、`ResetToDefaultPosition`、
   `clearAllBubblesLocked`、`StartDrag`……）。漏一处就是"窗口被裁到一张没人画的卡片上"，
   正是本项目反复出现的 bug。
2. **挂进面板矩形**（借用 `panelOpen` 那条分支）。菜单会和面板抢同一块 380×520，任何一个
   都会在对方还活着的时候把区域裁错。
3. **气泡的一种 kind。** 气泡已经拥有这次需要的一切：固定尺寸、写在 layout 里的矩形、
   进入区域计算、被所有折叠路径处理、被 `Escape` 链处理、在页面里由同一个 `#bubble` 画。
   菜单只是"卡片里装什么"不同。

选 3。它让这次改动在 Go 侧只有三处：一个新常量、一个高度、一个 `OpenPetMenu` 的新实现；
`app.go` 的 region switch **一行未改**。代价是菜单继承了气泡的形状限制（见 §8）。

## 2. 几何：一张 300×226 的卡

- 宽：`bubbleWidth = 300`（与提醒/总结/计划同一栏宽）。区域计算把它放在
  `petX + petW + gap`（宠物在右时是 `panelX` 侧），垂直居中于宠物并 clamp 进窗口。
- 高：`bubbleHeightMenu = 226`。来由：卡片 `padding: 12` + `border: 1px`，三行格子 + 两条
  `6px` 的行间距 → `12*2 + 1*2 + 3*62 + 6*2 ≈ 226`。
- 高度**必须是常数**：区域在页面布局之前就裁好了（`docs/reminder-pomodoro.md` 8.1）。
  `style.css` 里 `.bubble-menu` 的 `grid-auto-rows: 1fr` 让格子去填这个盒子，而不是自己
  声明高度——自己声明要么留下一条空卡片带，要么被区域切掉一角。
- 菜单卡没有标题：`renderBubble` 把空标题 **隐藏**（不是留一个空行），否则 flex 的
  `gap: 8px` 会白白吃掉 8px，让九格挤在一起。

## 3. 三个动作来源，一份定义

`PET_MENU_ACTIONS`（`frontend/src/main.js`）是一张 `data-menu → () => Promise` 的表，
每格调的都是这个动作**本来就有**的绑定。因此：

- 托盘菜单里的"开启/关闭番茄钟"和菜单里的番茄钟格是同一个 `SetPomodoroEnabled`；
- 若哪天某个动作改了名字，`check-frontend.cjs` 的"每格都有动作、每个动作都有格"会红，
  而不是在运行时变成一个点了没反应的按钮。

「插件」与「设置」两格都启动控制中心：控制中心是一个程序（`PetPCPluginMarket.exe`），
页签是它自己的 Svelte 状态，不接受参数。一格只能叫出一半的名字，剩下那一半的用户会以为
"桌宠没有入口"；两格各自在 `title` 里说清是哪半，就是当前事实的诚实表达。

## 4. 每格的动作与它们的"关闭"语义

| 格 | 调用 | 谁负责收起卡片 |
| --- | --- | --- |
| 日程 | `SetPanelOpen(true)` | Go：开面板即折叠气泡 |
| 对话 | `OpenChatPluginSurface()` | Go：开浮层面即折叠气泡（或返回气泡说明原因） |
| 番茄钟 | `SetPomodoroEnabled` → `DismissBubble` | 页面：动作本身不碰槽位，所以显式收起 |
| 提醒 | `RemindNow()` | Go：提醒气泡**替换**这张卡（多收一次就是同一手势里裁两次） |
| 插件 / 设置 | `OpenPluginMarket()` → `DismissBubble` | 页面：窗口开在卡片上，卡片得让开 |
| 归位 | `ResetToDefaultPosition()` | Go：重排窗口时折叠气泡 |
| 隐藏 | `HidePetForUser()` | Go：`HidePet` 折叠一切 |
| 退出 | `Quit()` | 无关 |

这条表就是"不要每个动作都先 `DismissBubble`"的原因：多一次收起，就多一次"裁回宠物再裁到
新目标"，肉眼是一次闪烁。

## 5. 关闭：五种方式，为什么需要第五种

1. **再右键一次**（`togglePetMenu`）：菜单自己的手势应当能开也能关，否则用户手上还按着鼠标
   却只剩 `Esc`。
2. **`Esc`**：`document` 上那条已有的 Escape 链会 `DismissBubble` 非提问气泡，菜单自动在列。
3. **选一个格子**：见 §4。
4. **别处打开东西**：Go 折叠气泡时顺带收掉（见 §1 的好处）。
5. **窗口失焦**（本次唯一新增的机制）：页面画的卡片**看不到**"点别处"这一下——窗口被裁到
   卡片，点击落在别的程序上。失焦是"用户走开了"在页面里唯一可观测的那一半。只对
   `kind === 'menu'` 生效：提醒气泡有自己的倒计时，提示卡是必须回答的，两者都不该被失焦
   丢掉。

## 6. 提问待答时不开

总结提示卡着休息、计划提示卡着下一段专注、选择器卡着第一次专注。它们和菜单共用同一个
矩形，所以菜单不是"盖住"它们而是"顶掉"它们——计时器会停在一张没人看得见的问题上。

两端都拒绝：

- **后端** `OpenPetMenu` 先问 `unansweredQuestionKind()`，非 `BubbleNone` 就返回
  `petMenuBlocked(kind)` 的句子，并且**什么都不改**（和 `OpenPluginSurface` 同一套）。
- **前端** `togglePetMenu` 先看 `bubbleAwaitingAnswer()`，命中就 `focusAnswer()`：
  把光标送进那个输入框。理由和对话按钮一样——错误会变成一个 toast，而 toast 住在面板里，
  气泡在时面板必然是关着的。

## 7. 删掉的东西

- `App.OpenPetMenu` 的旧实现（转发给托盘线程）与 `error` 返回值 → 改成 `(Layout, error)`，
  让前端拿到新矩形，与其他"会改变布局"的绑定同形。
- `internal/desktop/tray_windows.go` 的 `ShowMenuAtCursor` 与 `wmShowPetMenu`。托盘图标的
  右键仍走 `wmTrayCallback`（不变）；被删掉的是"替别人弹菜单"这条没有调用者的第二入口。
  `WM_APP + 2` 保留空号而不是复用：旧二进制对新二进制的兼容不是值得推理的状态。

## 8. 已知代价 / 后续（YAGNI）

- 卡片圆角外那几像素的点击仍然落回本窗口（区域是矩形，`§3.2` 记录的代价），和面板、气泡
  一致。要拿回它们需要逐像素命中测试，是另一个量级的机制。
- 菜单高度锁在 226，所以格子只能是 3×3。**再加动作时**：要么加一行（改成 3×4 并同步
  `bubbleHeightMenu` 与 `style.css`），要么两页。现在九个动作已经覆盖"宠物自己能做的事"，
  再加就说明有东西该进控制中心。
- 失焦关闭依赖窗口真的拿到焦点（右键会把焦点给本窗口）。若某天窗口改成不抢焦点，第 5 种
  关闭方式会失效——那时菜单仍然可以用右键/Esc 收起，不会变成关不掉的卡片。
