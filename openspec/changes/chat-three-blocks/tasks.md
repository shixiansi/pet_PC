# tasks: 对话浮层面拆成三块固定盒子

## 1. 插件（market/ 独立仓库）

- [x] `manifest.json`：`surface` 改成三块（48 / 100 / 56），去掉 `maxH`，版本 1.6.0
- [x] `panel.html`：根 class 默认 `view-boot`（握手前什么都不画）
- [x] `style.css`：新增 `view-question` / `view-answer`（SVG 气泡 + 朝下尖角）/ `view-input`
      （纯 CSS 气泡，**无尖角**，删掉 `view-bar` 的 padding 带与 `::after` 三角）
- [x] `app.js`：
  - [x] 角色由 `surface.rectIndex` 决定（0 提问 / 1 回复 / 2 输入），删掉
        `isCardBlock` / `isBarBlock` / `isReplyCard` / `visibleComposer`
  - [x] 删掉 `surface.blocks` 两处调用、`requestCardHeight` / `lastAskedHeight`
  - [x] 删掉结果区那套：`beginResult` / `finishResult` / `resultNode` / `#result` 模式
  - [x] 删掉替换那套：`cardSynced` / `cardPoll` / `afterTyping`
  - [x] `ownRequestId` 取代 holder 上的 `owner`；`llm.done` **当场**写转写
  - [x] 提问框：每轮读一次 `storage.get`，画最后一条 `user`
  - [x] 回复框：收 delta 直播（打字机保留为纯显示），`done` 用权威全文校正
  - [x] 同一时刻只允许一条请求在飞（发送键在 `busy` 时禁用）

## 2. 测试（market）

- [x] 三块视图各自的根 class 与 `#reply` 可见性
- [x] 提问框只画最后一条 user；回复框只画最后一条 assistant
- [x] 新问题在飞时回复框仍显示上一条回答（不被提问顶掉）
- [x] 回复框从**自己的** delta 直播，且不写转写
- [x] `llm.done` 到达即 `storage.set`（含"没有任何 delta、只有 done"这条路径）
- [x] manifest 三段几何：`dy + h` 关系、`dx` 居中、头顶合计 ≤ 160、没有 `maxH`
- [x] 删掉/改写旧断言：两块形状、`surface.blocks` 促成、`#result` 结果区
- [x] 追加：握手前是 `view-boot`、输入框不再有结果区、构建标记与 manifest 版本一致

## 3. 打包与仓库同步

- [x] `pwsh -File market/tools/package.ps1`（打 1.6.0 zip、回写 `market/index.json`、复制进
      `internal/plugins/sample/`）
- [x] 删掉 `internal/plugins/sample/` 与 `market/dist/` 里过期的 1.5.34 zip
- [x] `internal/plugins/sample/embed.go` 的 `go:embed` 清单换成 1.6.0

## 4. 文档

- [x] `docs/llm-chat.md` §8.0.2（三块几何表、头顶 156/160 预算、`maxH` 为何去掉、各块职责表）
- [x] `docs/llm-chat.md` §12.2 的"现在只剩两块"改成三块
- [x] `README.md`（用户可见的浮层面版式、manifest `surface` 字段说明、冒烟清单第 8 步、测试清单）
- [x] 插件 `app.js` 的构建标记 `BUILD = '1.6.0'`，并由测试钉住与 manifest 版本一致

## 5. 门禁

- [x] `cd market && npm test`（394 passed / 1 skipped）
- [x] `go vet ./...`
- [x] `go test ./... -count=1`（含 `TestBuiltinSampleAgreesWithTheMarketCatalogue`）
- [x] `cd frontend && npm run build && npm test`（78 passed）
- [x] `node .dev/check-frontend.cjs`（169 checks）
- [ ] 真机：装插件 → 重启桌宠 → 打开对话浮层面，确认三段盒子与尖角
      （`tools/install-fixed-chat-plugin.ps1` 复制四个资源文件；占位符上的 `(1.6.0)` 就是构建标记）
- [x] `.dev/verify-live.ps1`：原来按"最小 dy / 最大 dy"认块，现在改成按声明结构认
      （`dy + h = 0` 的是带尖角那块、`dy > 0` 的在脚下），并把三块都纳入"画出来了"的判据；
      解析器校验通过，但这条门禁需要交互桌面，本轮没有跑

## 已知遗留（写进 design §6，不属本次范围）

- 头顶预算跟 `PetScale` 反向：`PetScale > ~1.04` 时提问条顶边会被窗口顶边裁掉。
- 输入框的 `dy` 写死 208，只在 `PetScale = 1` 时正好贴脚；要通用得给 manifest 加"锚定宠物底边"
  的字段（宿主改动）。
