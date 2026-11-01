# proposal: 扩展能力 —— 控制中心里的插件测试台（plugin-bench）

## 要解决什么

用户的原话：「在设置界面（= 桌宠右键「设置」打开的控制中心）新增扩展能力的菜单，里面可以直接展示
插件的测试平台」。

在这之前，"这个插件到了别的地方会怎样"这个问题在整条链路上**没有一屏能回答**：

| 现有手段 | 能看见 | 看不见 |
| --- | --- | --- |
| 「插件」页 | 插件长什么样、权限、设置 | 它只按一种位置挂：`init.surface = panel/workshop`，铺满页面 |
| `plugin-testkit/preview.mjs` | 声明矩形里的排版、越界元素、截图 | 要一台装着 Edge/Chrome 的机器、要 node、要插件**源码目录**；装好的插件没有源码 |
| 桌宠真机 | 一切 | 要装、要授权、要人在桌面上截屏，而且看不到消息 |
| `market.log` | 谁调了什么 | 握手、`init` 里到底写了什么、插件发出去的每一条请求 |

于是三件事只有"上真机"一条路，而它们恰恰是插件最容易做错的地方：

1. **位置**。同一份文档在桌宠里是浮层面（有矩形、被 `SetWindowRgn` 裁）、是常驻帧（隐藏、没有
   矩形）、在控制中心是铺满的工作区。而"按钮被下边缘切掉"只发生在那个矩形里。
2. **授权被拒之后**。用户什么都不勾时界面是否还立得住——插件作者最该看的一屏，而「插件」页只有
   "真的改授权"一条路（那会改动闸门对这个世界生效的东西）。
3. **看不见的那一半**。握手、请求、响应、宿主推下来的事件。

## 做什么

控制中心左栏新增第七页 **「扩展能力」**，页内是一个插件测试台：

1. **选一个已装插件**（启用 + manifest 能解析 + 有 `entry` 才算"挂得上"；其余在下面点名并给出
   宿主会给的理由——停用的插件连文件都不发，`/plugin-asset` 回 403）。
2. **选一个位置**，三条，都是它真的会待的地方：控制中心的工作区（`panel/workshop`，铺满）／桌宠的
   浮层面第 N 块（`surface` + `rectIndex`/`rectCount`/`rect`）／桌宠的隐藏常驻帧（`panel/background`）。
3. **按声明的矩形画出那条虚线框**，框可以按 100 / 75 / 50% 缩放显示；改宽高会**重新握手**
   （尺寸写在 `init.surface.rect` 里，改了不重挂就是在告诉插件一个假数字）。
4. **看每一条过边界的消息**：`ready` / `init` / `request` / `response` / `event` / `theme`，
   两个方向、带时间和字节数；拒绝（`ok:false`）有自己的颜色和错误原文。
5. **「模拟未授权」开关**：`init` 里 `granted` 清空，并且每个请求在宿主这一层就被拒绝——
   真实授权一个字节都不改。
6. **推一条事件**（`share.changed` / `storage.changed` / `config.changed` 三个名字是宿主真的会推的，
   载荷可以改）。
7. **以插件身份调一个方法**：走同一个 `Market.PluginRequest`，拒绝原样显示。

## 明确不做

- **不替代 `plugin-testkit/preview.mjs`。** 沙箱帧是 opaque origin，本页**读不到**它的 DOM，
  `getBoundingClientRect()` 一条都拿不到；"元素被下边缘切掉"仍然只有那份真浏览器工具能看见。
  这一页量的是**框**，不是内容——两句话都写在页面上，免得有人把它当排版检查。
- **不是第二个 iframe 宿主。** 帧仍然由 `cc/src/lib/plugin-frame.ts` 建（沙箱恰为
  `allow-scripts`，请求仍走那道闸门）。新增的是三个可选参数（位置、消息观察、模拟拒绝）和一个
  `sendEvent`，不是新机制。
- **不改真实授权。** 页面上没有 `Grant` / `SetEnabled`：授权是用户在「插件」页的决定。
- **不挂市场里没装的包、不读源码目录。** 它挂的是 `/plugin-asset/<id>/<entry>`，也就是这台机器上
  装着的那个版本——和用户看到的是同一个文档。

## 验收标准

1. 左栏出现「扩展能力」，选中一个声明了浮层面的插件时，框的宽高**逐字等于**清单里那一块
   （380×260），握手说得出 `kind:"surface"` + `rectIndex` + `rect`。
2. 声明了 `background` 的插件在位置下拉里也出现「常驻帧」；挂上去时它是隐藏的，但消息日志照样
   有 `ready` / `init`。
3. 打开「模拟未授权」之后，`init.granted` 是**空数组**，每个请求得到的是这一层写明的拒绝。
4. 关掉它、改宽高、「重挂」，三次都是新的握手（新文档，不是复用）。
5. 推一条 `share.changed` 后，日志里有一行「手动推送」，与宿主推送的那一行同形状。
6. 停用 / manifest 坏掉的插件**不出现在可选列表里**，而在下面说明原因。
7. `cc npm run check` 0/0；`cc npm run smoke` 全绿（含 26 条新断言）；`node .dev/check-frontend.cjs`
   全绿（+4 条）。

## 影响

| 位置 | 变化 |
| --- | --- |
| `tools/plugin-market/cc/src/lib/plugin-frame.ts` | `MountRequest` 多三样可选：`surface` / `onMessage` / `denyRequests`；句柄多 `sendEvent`。`EventsOn` 加 try/catch（浏览器与 smoke 没有 Wails runtime，之前它会让帧**根本挂不上**） |
| `tools/plugin-market/cc/src/pages/Bench.svelte` | 新增（测试台页） |
| `tools/plugin-market/cc/src/App.svelte` | 左栏第七页 + 为什么它不是"开发页"的那段理由 |
| `tools/plugin-market/cc/scripts/render-smoke.mjs` | 新增 26 条断言（位置、握手、拒绝、重挂、事件、清空、方法控制台） |
| `.dev/check-frontend.cjs` | +4 条静态接线（一份实现、三种位置、模拟未授的两半、无 runtime 也能挂），并把主题那条的形状跟着改动更新 |
| `README.md` | 控制中心那行改成七页并说明测试台 |
| `docs/plugin-market.md` | 新增 §2.16 |
| `AGENTS.md §3.3` | 写明这个窗口里有三处调用 `plugin-frame.ts`、只有一份实现 |
| `.github/workflows/ci.yml`、`AGENTS.md §4` | 静态接线条数 138 → 142 |
