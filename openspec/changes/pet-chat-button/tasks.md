# tasks.md — pet-chat-button

> 每一步的「完成判据」都是**实施完成后的单一终态**，可以直接复跑。

## A. 页面侧（T1–T4）

- [x] **T1 — 按钮的标记与样式**
  文件：`frontend/index.html`、`frontend/src/style.css`
  内容：`#pet` 内新增 `<button id="pet-chat" type="button" title="对话" aria-label="对话">` 与内联 SVG 气泡图标；
  `.pet-chat` 绝对定位在宠物框左上角（24×24 圆），默认 `opacity:0` + `visibility:hidden` + `pointer-events:none`；
  `#pet:hover .pet-chat, .pet-chat:focus-visible` 才 `opacity:1` + `visibility:visible` + `pointer-events:auto`；
  `#pet.anchored .pet-chat` 隐藏；过渡只有 `opacity`。
  完成判据：`#pet-chat` 在 `#pet` 的 DOM 子树内（`pet.contains(button) === true`）；未悬停时 `getComputedStyle` 的
  `visibility === 'hidden'` 且 `pointer-events === 'none'`（真浏览器 harness 断言）；`.pet-chat` 规则里没有 `transform`。

- [x] **T2 — 接线：点按钮 = 原来的双击动作**
  文件：`frontend/src/main.js`
  内容：`els.chatButton = $('pet-chat')`；`pointerdown` 上 `stopPropagation()`（不让 `#pet` 登记拖动、不让 `preventDefault`
  压掉 `click`）；`click` 调 `openChatPluginSurface()`。
  完成判据：点一次按钮 → `OpenChatPluginSurface` 恰好 1 次、`TogglePanel` 0 次、`StartDrag` 0 次。

- [x] **T3 — 删掉双击裁决，单击立即生效**
  文件：`frontend/src/main.js`
  内容：删除 `pendingClick` / `lastClickAt` / `DOUBLE_CLICK_MS` 及 `endDrag` 里的双击分支；单击分支改为
  `applyLayout(await App.TogglePanel())`，`editingId === null` 时聚焦标题。
  完成判据：`grep -n "DOUBLE_CLICK_MS\|pendingClick\|lastClickAt" frontend/src/main.js` 无匹配；`pointerdown`+`pointerup`
  之后**一个 tick** 内 `TogglePanel` 已调用；连点两次 = `TogglePanel` 两次、`OpenChatPluginSurface` 零次。

- [x] **T4 — 提问待答时的老规则保留**
  文件：`frontend/src/main.js`（`openChatPluginSurface` / `focusAnswer` 的注释）
  内容：逻辑不动，只把注释里的"双击"改成"对话按钮"，说清它防的是什么（浮层和气泡共用一块矩形）。
  完成判据：气泡是 `plan` / `summary` / `pick` 时点按钮 → `OpenChatPluginSurface` 0 次、焦点落在输入框 / 主按钮上。

## B. 门禁（T5–T7）

- [x] **T5 — 静态接线检查改钉新行为**
  文件：`.dev/check-frontend.cjs`
  内容：第 6 节整节重写：按钮在 `#pet` 标记之内（`id="pet"` < `id="pet-chat"` < `id="bubble"`）、`chatButton` 与
  `click` 接线、`pointerdown` 的 `stopPropagation`、`.pet-chat` 的 `visibility:hidden`+`pointer-events:none`、
  `#pet:hover` / `#pet.anchored` 两条规则、`applyLayout(await App.TogglePanel())` 且无双击常量。
  完成判据：`node .dev/check-frontend.cjs` 全 PASS（本次 165 项）。

- [x] **T6 — 真浏览器行为 harness 跟上**
  文件：`.dev/run-frontend-harness.cjs`
  内容：`pressPet(2)` 的两处调用改为新的 `clickChatButton()`；场景 1 改为"单击**立即**生效"；场景 2 改为"点按钮开浮层面"；
  场景 2c 改为"点按钮不顶掉总结气泡"；新增两条断言（都在真引擎里实测，不是读源码）：未悬停时按钮
  `visibility:hidden` + `pointer-events:none`，以及按钮的 `getBoundingClientRect()` 落在宠物框之内。
  完成判据：`node .dev/run-frontend-harness.cjs` → `FRONTEND HARNESS PASSED (110 checks)`。

- [x] **T7 — jsdom 单测**
  文件：`frontend/src/pet-chat-button.test.js`（新增）、`frontend/src/bubble-plan.test.js`
  内容：新增文件覆盖 T1–T4 的每一条；`bubble-plan.test.js` 的 `doubleClickPet()` 换成 `clickChatButton()`。
  完成判据：`npm test` → 3 个文件、56 个用例全绿。

## C. 文档（T8）

- [x] **T8 — 行为变了，文档跟着改**
  文件：`README.md`（操作表、对话浮层面一节、验收清单、验证一节）、`docs/llm-chat.md`（§0 表、§8.2、§8.3 重写、
  §12.3/§12.5/§15 的口述）、`docs/reminder-pomodoro.md`（§4.5、§8.2 的两条实现约束、边界表、验收清单）
  内容：入口一律写成"悬停宠物 → 点左上角对话按钮"；`docs/llm-chat.md` §8.3 记下**为什么放弃双击**；
  顺手修掉几处指向已删除标识符的引用（`OpenLive` / `openLive` → `OpenPluginSurface` / `openChatPluginSurface`）。
  完成判据：`Select-String -Path README.md,docs\*.md -Pattern 'OpenLive|openLive'` 只剩一处**明确标注为历史**的说明。

## D. 验收（人工）

- [ ] 悬停宠物 → 左上角出现对话按钮；移开 → 消失，且那个位置点下去仍然是开关面板
- [ ] 点按钮 → 对话插件的浮层面打开；再点一次关闭
- [ ] 面板开着时悬停宠物 → 不出现按钮
- [ ] 总结气泡在时点按钮 → 浮层面不打开，光标落进总结输入框
- [ ] 单击桌宠 → 面板**立刻**开关（没有 200ms 的迟滞感）
- [ ] 按钮出现/消失过程中，宠物框外**没有**任何一帧被画到桌面上（区域未变）
