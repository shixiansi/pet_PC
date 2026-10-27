# design — pet-chat-button

## 1. 按钮长在哪：宠物框之内，不是框外

这是这次唯一有架构含量的决定。

窗口在"面板、气泡、浮层面都不在"时被 `SetWindowRegion` **恰好裁成宠物那一块矩形**
（`app.go` 的 `default` 分支）。因此浮在宠物边上的按钮有两种坏结局：

| 方案 | 结果 |
|---|---|
| 按钮画在宠物框外，区域不变 | 框外部分**直接被原生区域切掉**，用户只能看到半个按钮 |
| 按钮画在宠物框外，区域跟着放大 | 那条矩形是窗口却没人绘制，点击落在本窗口上而**不会透给桌面**——就是 `AGENTS.md §3.2` 反复出现的那类问题 |
| 悬停时让 Go 重算区域 | 要一次往返；区域没到之前按钮**画了也点不到**，而且"悬停"成了一个会改窗口几何的事件 |

所以按钮是 `#pet` 的**子元素**（`<button id="pet-chat">`），用绝对定位摆在宠物框的左上角
（右上角已经被番茄钟徽标与待办计数占用）。这是纯页面侧的控件：**Go 的几何一个字节都没改**。

代价，明写在这里：按钮占着宠物框左上角那一小块，所以那一小块的点击在悬停时归按钮。
把它做成 24×24 的圆并贴在角上（而不是浮在宠物脸中央），就是为了让误点概率足够低。

## 2. 「隐藏」必须同时是 `visibility` 与 `pointer-events`

````css
.pet-chat { opacity: 0; visibility: hidden; pointer-events: none; }
#pet:hover .pet-chat, .pet-chat:focus-visible { opacity: 1; visibility: visible; pointer-events: auto; }
````

只做 `opacity: 0` 的话，按钮**仍然是一个可点的元素**，于是宠物左上角那一下会去开对话，
而不是开面板——一个"什么都没改"的改动却改变了单击的语义。`pointer-events: none` 是这条
不变量，`visibility: hidden` 让它在辅助技术与命中测试里也不存在。

`#pet.anchored`（面板开着）时按钮不显示：此时宠物只是面板的锚点，而面板自己就有「对话」tab。

过渡**只有透明度**。`transform` 会让像素在动画中途伸出宠物框，而框外就是裁剪边界，
动画会被切一刀；`box-shadow` / `filter` 在这个仓库里本来就被禁（会重合成宠物背后的桌面）。

## 3. 按下不启动拖动：`pointerdown` 停在按钮上

`#pet` 的 `pointerdown` 会（a）记下拖动原点、（b）`preventDefault()`、（c）`setPointerCapture`。
按钮是它的后代，事件会冒泡上去，后果是：按一下按钮就登记了一次"可能的拖动"，而
`preventDefault` 在某些引擎里会连带压掉随后的兼容鼠标事件（`click`）。

所以按钮自己 `pointerdown` 就 `stopPropagation()`。这样：

- 不会出现"按按钮 → 松手 → 触发 `EndDrag`/吸附"；
- `click` 照常合成（没有 `preventDefault` 参与）；
- 按下按钮再拖出去松手 = 什么也不发生（`click` 需要按下与松开在同一元素上）。

## 4. 单击不再延迟

原来 `pointerup(moved=false)` 里有：

````
if (now - lastClickAt < DOUBLE_CLICK_MS) { …openChatPluginSurface(); return }
pendingClick = setTimeout(() => TogglePanel(), DOUBLE_CLICK_MS)
````

这整段删除，`TogglePanel` 直接在 `pointerup` 里 `await`。删除是**结论**而不是顺手：
这段代码存在的唯一理由是"两击必须区分开"，而两击已经不存在了。留着它，就是让每一次单击
继续为一件没人做的手势付 200ms。

`endDrag` 仍然是 `async` 的（拖动分支本来就要 `await startPromise`），单击分支改成
`applyLayout(await App.TogglePanel())`，`then(...focus)` 合成一句。

## 5. 提问待答时的行为照旧

`openChatPluginSurface()` 里的那道判断一条都没改：

````js
if (bubbleAwaitingAnswer()) { focusAnswer(); return; }
````

它原来防的是"双击把番茄钟的问题顶掉"，现在防的是"点按钮把问题顶掉"，理由完全一样：
浮层面和气泡共用一块矩形，打开浮层不是盖住问题而是**删掉**问题，而总结气泡是休息的门控、
计划气泡是专注的门控。报错的 toast 长在面板里，而气泡占着槽位时面板必然关着，所以不能靠报错。

## 6. 守护这条行为的检查怎么改

按 `AGENTS.md §3` 的规矩，改不变量就要改守着它的检查：

| 检查 | 原来钉住 | 现在钉住 |
|---|---|---|
| `.dev/check-frontend.cjs` §6 | 双击在指针流里裁决、第二击先 `clearTimeout` 再开浮层、单击延迟 200ms | `#pet-chat` 在 `#pet` 标记之内、`chatButton` 接线、`pointerdown` 被 `stopPropagation`、CSS 默认 `visibility:hidden`+`pointer-events:none`、`#pet:hover` 才放出来、`main.js` 里**没有** `DOUBLE_CLICK_MS`/`pendingClick`/`lastClickAt` |
| `.dev/run-frontend-harness.cjs` | 单击"不立即生效"、双击开浮层、双击被拒时光标进输入框 | 单击**立即**生效、按钮未悬停时实测 `visibility:hidden`+`pointer-events:none`、点按钮开浮层、点按钮被拒时光标进输入框 |
| `frontend/src/bubble-plan.test.js` | `doubleClickPet()` 助手 | `clickChatButton()` 助手 |
| `frontend/src/pet-chat-button.test.js` | —（新增） | 上面两条的 jsdom 版本 + "双击 = 两次 TogglePanel，再也不开浮层" |

真浏览器里**悬停本身无法用脚本触发**（`:hover` 不是由事件决定的状态），所以分工是：
`check-frontend.cjs` 静态钉住 `:hover` 规则，harness 在真引擎里钉住"没悬停时它确实是隐藏且不可点的"。

## 7. 没有走的两条路

- **常显按钮。** 桌宠的价值是"不占地方"，常显的小按钮会一直压在图上，而且会与徽标抢同一只角。
- **右键菜单里加一条「对话」。** 菜单已经很长，而且"说话"是桌宠最常用的入口之一，
  藏进二级不可见列表里等于把它删掉。
