# tasks: 隐藏一块浮层面，不再销毁它

## 1. 宿主（Go）

- [x] `PluginSurfaceRect` 增加 `Hidden bool`（json `hidden`），并写清"为什么条目要留着"
- [x] `computePluginSurfaceSized`：隐藏块照常算几何、照常进 `view.Rects`，只是不进 `regions`
- [x] 用 `regionOf[k]` 影子数组替代"两片同序"的隐式约定（长高那条路径按它取矩形）
- [x] 长高的请求跳过隐藏块
- [x] 全隐藏仍然返回 `(nil, nil)`（＝没有浮层面可裁）
- [x] 重写 `app_plugin_surface_blocks_test.go`：无区域 / 有视图条目 / 保留下标与尺寸 / 幸存块不位移

## 2. 前端

- [x] `surfaceFrames` 改成 `Map<声明下标, PluginFrame>`
- [x] 建帧用 `rectIndex: index`；重建判据改成键集合比较（不再用长度）
- [x] 隐藏块的元素：`visibility: hidden` + `pointer-events: none` + `dataset.hidden='1'`
      （用 `visibility` 而不是 `display:none`：后者会让 iframe 视口塌成 0，重排整篇文档）
- [x] `isOpen()` / `onMessage` / `routeEvent` / `clearSurface` 跟着改成 `.size` / `.values()`

## 3. 插件（market/）

- [x] `hiddenSet()`：三个状态（空闲无回答 → `[0,1]`；空闲有回答 → `[0]`；提问中 → `[]`）
- [x] `syncSurfaceBlocks()` 在所有状态变化处调用：读回转写后、发出的那一刻、本轮第一条事件、
      `llm.done`、`llm.error`；面板永不调用（浮层面没开时该方法会被拒绝）
- [x] `asking` / `hasAnswer`（粘性）两个变量，三帧从同一批事件到达同样的值 → 调用幂等
- [x] 提问条在 `asking` 为假时不渲染，避免下一次提问闪出上一句
- [x] 版本 1.6.2（构建标记同步，占位符可见）
- [x] 提问条 48 → 40px、文字垂直居中（两行 16px 正好等于内容高度）
- [x] 回答气泡改为顶部对齐；长回答在气泡内滚动并逐字跟随最新一行（滚动条隐藏）
- [x] vitest：空转写只有输入框 / 有关闭提问有回答 / 有问无答只有输入框 /
      轮内三态（`[0,1]`→`[]`→`[0]`）/ 出错保留错误行 / 发送那一刻 `[]` / 面板不调用

## 4. 文档

- [x] 新增本变更的 proposal / design / tasks
- [x] `chat-three-blocks/design.md` §5 标注"常显"的理由已被本变更修掉
- [x] `docs/llm-chat.md` §8.0.2 补一节可见性规则（什么时候藏、为什么只有两个时刻）

## 5. 门禁

- [x] `cd market && npm test`（402 passed / 1 skipped）
- [x] `go vet ./...`、`go test ./... -count=1`
- [x] `cd frontend && npm run build && npm test`（78 passed）
- [x] `node .dev/check-frontend.cjs`
- [x] `pwsh -File market/tools/package.ps1`（1.6.1 zip、目录、内嵌 sample 同步）
- [x] 重建桌宠（`wails build -s -skipbindings`）：前端产物是编译期嵌进 exe 的，
      **不重建就等于没改**
- [ ] 真机：空闲时只剩输入框；发一条消息，提问条与回答气泡出现并保持
- [ ] `.dev/run-frontend-harness.cjs`：本轮没跑（需要真实 Chromium + 假 Wails 桥接）；
      它的 fixture 没有 `hidden` 块，帧数/位置/逐帧 init 都不受本次改动影响
