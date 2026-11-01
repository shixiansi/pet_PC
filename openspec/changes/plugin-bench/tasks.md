# tasks: 扩展能力 —— 控制中心里的插件测试台

> 执行位置：`F:\dev\pet_pc\tools\plugin-market\cc\`（控制中心的界面）+ 根目录的文档与静态门禁。
> 不碰 Go：这一轮没有新绑定——`Installed` / `PermissionLabels` / `PluginRequest` / `PluginHostInfo`
> 四个现有的就够。

## A. 宿主模块（`lib/plugin-frame.ts`）

- [x] **A1 — `init.surface` 变成参数**
  `MountRequest` 多一个可选 `surface?: MountSurface`（`{kind:'panel'|'surface', slot?, rectIndex?,
  rectCount?, rect?}`），默认仍是 `{kind:'panel', slot:'workshop'}`——插件页那一处**一个字节都没变**。
  过边界前用 `plainSurface()` 读成纯数据（数也 `Number()` 一遍：`<input type="number">` 在边上给的是
  字符串），因为 Proxy 过不了 `postMessage`（§3.3 那条 DataCloneError）。
  判据：插件页的 `init` 仍然只有 `{kind:'panel', slot:'workshop'}`（冒烟里那条老断言原样通过）。

- [x] **A2 — 消息观察**
  `onMessage?: (ev: BoundaryEvent) => void`：`ready` / `init` / `request` / `response` / `event` /
  `theme`，带方向、时间与字节数（UTF-8 计数，和 Go 侧日志同一口径）；`response` 带 `ok`。
  观察者抛异常不许影响传输（`emit` 里 try/catch）——日志是查看器，不是链路。

- [x] **A3 — 「模拟未授权」**
  `denyRequests?: boolean`：`granted` 在**同一处**被换成 `[]`，并且 `answer()` 在调闸门**之前**
  就用中文拒绝（"测试台：模拟未授权…"）。两半一起决定，所以不可能只做一半。
  判据：冒烟里翻开开关后 `init.granted` 是空数组，且请求得到的是那一层的拒绝。

- [x] **A4 — `sendEvent(name, payload)`**
  句柄多一个方法：手推一条 `event`，形状与宿主的 `pet:plugin-event` 转发**完全一致**
  （插件分不出是手推的还是桌宠推的，这正是它的用途）。

- [x] **A5 — 修掉没有保护的 `EventsOn`**（撞上的，不是本轮引入的）
  `EventsOn('pet:plugin-event', …)` 转发到 `window.runtime`，而只有 Wails 窗口有它。于是
  `npm run dev` 和 render smoke 里挂帧就抛 `Cannot read properties of undefined (reading
  'EventsOnMultiple')`——在 `$effect` 里，于是帧根本没挂上。现在 try/catch 降级成"没有宿主事件"。
  判据：冒烟第一次跑就崩在它上面（26 条新断言之前），修完 108/108。

## B. 页面（`pages/Bench.svelte` + `App.svelte`）

- [x] **B1 — 左栏第七页「扩展能力」**，位置在「市场」之后（装完就想测），
  `App.svelte` 里写明"为什么它不是一页只给开发者的页"。

- [x] **B2 — 选插件**
  只列挂得上的（启用 + manifest 能解析 + 有 `entry`）；其余在下面点名并给**宿主会给的**理由
  （停用的插件 `/plugin-asset` 回 403，因此挂不上）。默认选第一个，位置按清单选：有矩形就浮层面，
  否则有 `background` 就常驻帧，否则工作区。

- [x] **B3 — 三种位置 + 矩形**
  下拉按清单生成；选浮层面时框的宽高**逐字等于**声明值（冒烟断言 `380px`/`260px`），
  虚线是 `SetWindowRgn` 的包围盒，缩放只影响看得见的大小（`transform: scale`，内层仍是 W×H）。

- [x] **B4 — 改尺寸要重新握手**
  宽高夹在 80–2000；`onchange`（回车/失焦）才提交，提交后进 key → 销毁重挂。

- [x] **B5 — 消息日志**
  新的在最上面、上限 200 行；`ok:false` 有自己的左边条与颜色；「清空日志」。

- [x] **B6 — 推事件 / 调方法**
  三个真事件名（`share.changed` / `storage.changed` / `config.changed`）+ 可编辑载荷（JSON 解析
  失败就不发，并说出来）；方法控制台走 `PluginRequest`，结果/拒绝原样显示在一个 `<pre>` 里。

- [x] **B7 — 权限清单（只读）**
  声明了什么、闸门里授了什么，文案来自 `Market.PermissionLabels`；「模拟未授权」开着时列表整列
  变成 ✗ 并写明"这个帧被告知 0 项，而闸门里的真实授权一个字节都没改"。

## C. 门禁

- [x] **C1 — render smoke（`cc/scripts/render-smoke.mjs`）+26 条**
  页面在 rail 上、默认选中、挂不上的点名、沙箱、URL、`panel/workshop` 握手、真实授权、日志有
  两个方向、请求被拒原样进日志、「模拟未授权」重挂且 `granted=[]`、拒绝写明是模拟的、
  浮层面按声明矩形挂（380×260）、常驻帧选项在、换插件换文档、`surface` 握手带 `rectIndex/rect`、
  手推事件进日志、日志里 4 种以上消息、重挂换了文档、清空日志、方法控制台拒绝可见。
  判据：`npm run smoke` → `smoke: 108/108`。

- [x] **C2 — 静态接线（`.dev/check-frontend.cjs`）+4 条**
  一份实现（不许 `createElement('iframe')`）、三种位置进 `init.surface`、模拟未授权的两半 +
  这一页不许 `Grant`/`SetEnabled`、没有 Wails runtime 也能挂帧。
  同时把主题那条断言的形状跟着 A2 的改动更新（`if (initialised) { post(…theme…) …}`），
  并把文档里的条数 138 → 142。
  判据：`node .dev/check-frontend.cjs` → `ALL FRONTEND CHECKS PASSED (142)`。

- [x] **C3 — `cc npm run check`** → `0 errors and 0 warnings`。

## D. 文档与产物

- [x] **D1 — `docs/plugin-market.md` §2.16**：这一页回答什么、三条边界（量不了排版 / 不改授权 /
  不是第二个宿主）、顺手修的 `EventsOn`、判据与构建标记。
- [x] **D2 — `README.md`**：控制中心那行改成七页并说明测试台；目录说明里 `cc/` 的页数同步。
- [x] **D3 — `AGENTS.md §3.3`**：写明这个窗口里有**三处**调用 `plugin-frame.ts`、只有一份实现，
  以及那三样新增参数为什么存在；§4 的静态接线条数 138 → 142。
- [x] **D4 — 重建控制中心**：`cc npm run build` → `cc/dist` 覆盖到
  `tools/plugin-market/frontend/dist` → `wails build -s -skipbindings` → 复制到根 `build\bin`。
  判据：exe 里搜得到 `扩展能力` / `测试台 · v1` / `推一条事件给插件` / `块声明矩形`；页面左上角的
  `测试台 · v1` 就是构建标记。
  一个现场情况：最后一次构建（改了位置下拉的文案 + 窄窗叠放）复制到根 `build\bin` 时，那里正跑着
  一个控制中心（pid 15108），Windows 锁着那个 exe，复制失败。`tools/plugin-market/build/bin/` 里
  那份是新的，且已核对字符串；关掉窗口后补一句即可：
  `Copy-Item -Force tools\plugin-market\build\bin\PetPCPluginMarket.exe build\bin\`。

## E. 未做（写清，避免误以为覆盖了）

- [ ] **排版越界检查**。这页量不了：沙箱帧是 opaque origin，读不到 DOM。仍然只有
  `market/docs/plugin-testkit/preview.mjs` 能做——它需要一台装着 Edge/Chrome 的机器和插件的
  源码目录，而装好的插件没有源码。
- [ ] **逐项授权模拟**（勾一半权限）。要么在前端复制一张"方法 → 权限"的表（AGENTS §3.1 明确
  不要的第七处那种副本），要么给 Go 加一个绑定；而"半授权"现在可以真在「插件」页改——那才是
  闸门看的那份。等真的有人需要再谈。
- [ ] **真机（WebView2）里的自动驱动**：本页的验收走的是 jsdom 冒烟 + 静态接线，窗口里的观感
  仍需人看一眼（与本仓库其它页面同一个约定）。
