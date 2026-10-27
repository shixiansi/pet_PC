# proposal: 隐藏一块浮层面，不再销毁它

## 要解决什么

`surface.blocks {hidden:[i]}` 现在的语义是**删块**：被隐藏的块既不出现在窗口区域里，也
不出现在 `layout.PluginSurface.rects` 里。于是"隐藏"和"这块没声明过"对页面来说是同一件事，
而页面按**数组位置**建 iframe，所以：

1. 隐藏第 0 块之后，原来的第 1 块落到位置 0，于是它拿到的 `rectIndex` 变成了 0
   ——历史上就是这样让输入胶囊认为自己是"结果条"，输入框直接消失；
2. 块数一变，`plugin-host.js` 用 `surfaceFrames.length !== rects.length` 判定"浮层面换了"，
   把**所有**帧 `destroy()` 再重建。重建一个 iframe 就是销毁它里面的文档：正在打的消息、
   正在流式进场的回答一起丢，重建期间宿主投出去的 `llm.*` 事件也没人接。

结果是：任何"按需显示/隐藏一块"的设计都不可用——而用户对对话浮层面的原始想法恰恰是
"三块盒子，整个过程只改变隐藏和显示"。1.6.0 的折中是**三块全部常显**，代价是空闲时
头顶挂着两块空的深色条。

## 做什么

把"隐藏"拆成两件互不牵连的事：

- **区域**（窗口裁到哪）：隐藏的块**不进区域**——这一条不变。不画的地方不能吃点击，
  这是 AGENTS.md §3.2 的不变量。
- **视图**（页面往哪摆 iframe）：隐藏的块**保留条目**，带 `hidden: true`，几何与可见时完全
  一样。页面因此可以**保留帧、只停止显示**（`visibility: hidden` + `pointer-events: none`）。

前端同时把帧的对应关系从"数组位置"改成**声明下标**（`rect.index`）。这两件事必须一起做：
只改前端，块仍然会从 `rects` 里消失，下标映射救不了它；只改宿主，前端仍会按位置错位。

## 明确不做

- 不新增权限（仍是 `pet.surface`），不新增方法（仍是 `surface.blocks`），payload 形状不变。
- 不接受插件给的坐标或尺寸。
- 不做"每块各自切换"：`hidden` 仍然是一次给全的量，插件侧要靠"所有调用者算出同一个集合"
  来用（见 design §3）。

## 影响的不变量

- AGENTS.md §3.2（区域必须恰好等于画出来的东西）：**更强**了——隐藏块既不画也不进区域，
  而它的 iframe 还在，所以"帧还活着但用户点不到"第一次成为可能。
- 区域与视图仍然来自**同一次** `computePluginSurfaceSized`，只是视图多了一个 `hidden` 字段。

## 验证计划

- Go：`TestComputePluginSurfaceHidesBlocksWithoutDeletingThem`（隐藏块无区域、有视图条目、
  保留下标与尺寸、幸存块不位移）；`TestComputePluginSurfaceAllHiddenReturnsNil`（全隐藏＝没有浮层面）。
- 前端：`npm run build && npm test`（帧池改成 Map 后所有既有断言不变）、`node .dev/check-frontend.cjs`、
  `.dev/run-frontend-harness.cjs`（真实浏览器里的 iframe 数量/位置/逐帧 init 不受影响）。
- 插件：新增 vitest 用例钉住"什么时候切换"（见 design §3）。
- 真机：空闲时应该**只剩输入框**（头顶两块空盒子被隐藏），一发消息两块就回来。
