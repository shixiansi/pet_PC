# tasks: 浮层面跟着「桌宠大小」走

## 1. 宿主（Go）

- [x] `plugins.Rect` 增加 `Anchor`（`""`/`"top"`/`"bottom"`），校验只接受这三个值
- [x] `computePluginSurfaceSized`：`anchor: bottom` 时 `dy` 从宠物脚底量
- [x] `PluginSurfaceRect` 增加 `Room`（宠物上方那一段的高度，CSS px）
- [x] 长高之后**再夹一次**窗口：块的高度不得超过"底边 - 0"
- [x] 测试：锚点（200/300 高的宠物）、`room` 随宠物变小、`maxH` 仍生效、带兜底的夹回
- [x] 更新 `sizedFixture()` 为真实形状（旧的 1.5.x 卡片顶边本来就在窗口外，长高永远不成立）

## 2. 前端

- [x] 新增协议消息 `surface`：某块矩形变化时发给**已挂载**的那一帧（按值比较，避免每次几何都发）
- [x] `PluginFrame.sendGeometry(rect)`

## 3. 插件（market/）

- [x] `anchor: "bottom", dy: 4` 用于输入框
- [x] `answerRoom()` 从 `rect.room` 读，缺省回退 160
- [x] 高度上限 `min(ANSWER_MAX, room - 4)`；提问条是否放得下按 `height + 48 <= room`
- [x] `surface` 消息 → 更新 `tailX` / `room`，重画气泡，忘掉上次申请并重新算
- [x] `resize` 同样重算（宿主改尺寸但没发消息时的兜底）
- [x] 测试：`room: 110` 时长回答申请 106、两行回答仍给提问条留位置；消息与 resize 都触发重算

## 4. 门禁与文档

- [x] `go vet ./...`、`go test ./... -count=1`
- [x] `cd market && npm test`（`plugin-app` 69 条）
- [x] `node .dev/check-frontend.cjs`
- [x] `.dev/verify-live.ps1`：`RectOf` 认锚点、"块在脚下"的断言认锚点、语法校验通过（需交互桌面，未实跑）
- [x] `docs/llm-chat.md` §8.0.2 / `README.md` 同步
- [ ] 真机：把「桌宠大小」调到 1.5 倍，确认回答气泡整体可见、输入框在脚下
