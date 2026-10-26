# 检查项清单（结构分析，不是关键词猜测）

每一项都是 `.dev/check-frontend.cjs` 里一个真实的 `check(` 块（共 **169** 个，与运行时
报告的 169 项一致）。分类看的是**块体引用了哪些变量**，不是标签里的词。

## 统计

| bucket | 项数 |
| --- | --- |
| `1-keep` | **87** |
| `2-rewrite` | **80** |
| `3-market` | **2** |
| `? 待人工` | **0** |
| 合计 | **169** |

本轮判的是上一版剩下的 73 项（70 个 `? 待人工` + 3 个 `? 混合`）：40 项落 `1-keep`、
33 项落 `2-rewrite`、0 项留在 `?`。另外更正了 3 处上一版的分类：

| # | 原标 | 改成 | 为什么 |
| --- | --- | --- | --- |
| 119 | `1-keep` | `2-rewrite` | 块体读 `mainJs` 的 `renderRecordDay` / `className = 'record-day'`，不是只读 CSS |
| 149 | `1-keep` | `3-market` | 块体**只**读 `tools/plugin-market/frontend/src/plugin-ui.js` |
| 164 | `1-keep` | `3-market` | 块体读 `plugin-ui.js` 的 `theme=` 参数与 `setFrameTheme`（另读插件包与市场样式表） |

## 判据（本轮实际使用的操作定义）

| bucket | 判据 |
| --- | --- |
| `1-keep` | 只读 Go 源码（`*.go`）或**生成的**绑定类型（`App.js`/`App.d.ts`/`models.ts`）；或只读 CSS 且属窗口区域规则（不许 `box-shadow`/`filter`/`text-shadow`/`transform`，椭圆必须 `border-radius: 50%`）；或读的是**别的东西**（`market/plugins/**` 的插件包、`market/index.json`、`tools/plugin-market/**` 这个独立程序的源码、`.dev/run-frontend-harness.cjs`、`frontend/src/test-harness.js`、文件系统存在性） |
| `2-rewrite` | 读 `frontend/src/main.js`、`frontend/index.html`、`frontend/src/plugin-host.js` 的元素 id / 调用点 / 文本；或读**旧样式表**（`frontend/src/style.css`）的分节与选择器结构并把它当断言对象 |
| `3-market` | 检查体读 `tools/plugin-market/frontend/src/plugin-ui.js` —— 控制中心那条工作线唯一会撞到门禁的地方 |
| `? 待人工` | 判不了，且备注里写清块体到底在读什么 |

## 三条边界说明（容易误判的地方）

1. **旧样式表的结构类检查归 `2-rewrite`，不归 `1-keep`。** `1-keep` 的 CSS 豁免只给窗口
   区域规则（`box-shadow`/`filter`/椭圆）。`/* --- chat tab` 这种分节标题、`.icon-btn` 的
   padding、滚动条选择器合并组，钉的都是旧样式表的组织方式：Svelte 用 scoped CSS 重画之后
   它们要么消失要么换写法，所以是"语义留、形式换"。
2. **市场程序的前端检查归 `1-keep`，不归 `3-market`。** 它们读的是
   `tools/plugin-market/frontend/{index.html,src/main.js,src/style.css}`，那是**另一个程序**，
   不是桌宠前端的入口。`3-market` 只按"读 `plugin-ui.js`"这一条判，见文末清单。
3. **把 `plugin-host.js` 当直接断言对象的检查归 `2-rewrite`。** 这个文件本次**不重写**
   （gates.md 第 1 类明说沙箱不变量原样有效），但它的断言绑在该文件的实现文本上，而它的装载点
   在前端重写范围内。这类共 6 项（37/38/49/50/51/109），另有 4 项（2/3/9/10）把它并进
   `scannedJs` 一起扫。其中 #50「椭圆画成真椭圆」是窗口区域规则，语义**必须**
   保留，只是形式要从扫 `plugin-host.js` 换成对新装载点的断言——已在备注里标出，别当垃圾删掉。

| # | 起始行 | 检查项 | bucket | 备注 |
| --- | --- | --- | --- | --- |
| 1 | 126 | every element id referenced from JS exists in index.html | 2-rewrite | 读 `index.html` 的 id 集合，对照 `main.js`+`plugin-host.js` 里所有 `$('id')` 调用点；id 接线就是旧 DOM 的形状。 |
| 2 | 145 | every App.\* call is an exported binding | 2-rewrite | 读 `main.js`/`plugin-host.js` 的 `App.Xxx(` 调用点，对照生成的 `App.js` 导出表；调用写法会从全局对象变成 import。 |
| 3 | 160 | every App.\* call is declared in App.d.ts | 2-rewrite | 对照 `App.d.ts` 的声明表，但左半边 `called` 取自前端源码，所以不是纯生成物检查。 |
| 4 | 169 | every window call the frontend applies as a layout is typed as one | 1-keep | 只读生成的 `App.d.ts`：七个布局类绑定必须声明为返回 `main.Layout`；与新前端实现无关。 |
| 5 | 178 | the plugin surface types exist in models.ts | 1-keep | 只读生成的 `models.ts`（`PluginSurfaceView`/`PluginSurfaceRect`）。 |
| 6 | 187 | the Layout model carries the plugin surface geometry | 1-keep | 只读 `models.ts` 的 `Layout.pluginSurface` 成员。 |
| 7 | 197 | the surface rectangle model carries the ellipse flag | 1-keep | 只读 `models.ts` 的 `PluginSurfaceRect.ellipse`；椭圆标志必须随几何一起生成。 |
| 8 | 206 | the host transcript model is gone with the host conversation | 1-keep | 只读 `models.ts`，断言 `ChatMessageView` 不再生成（生成物上的缺席断言）。 |
| 9 | 261 | every subscribed event is emitted by Go | 2-rewrite | 读 `main.js`+`plugin-host.js` 的 `EventsOn('…')`，对照 9 个硬编码 Go 文件的 `emitEvent`；订阅点随前端重写而变。 |
| 10 | 275 | the plugin and layout events are both emitted and subscribed | 2-rewrite | 同一组集合，钉 `pet:plugin-event`/`pet:layout`/`pet:chat-open` 两边都在。 |
| 11 | 317 | all N checked CSS sections are present | 2-rewrite | 读桌宠与市场两份 `style.css` 里 `/* --- …` 分节标题是否还在；钉的是旧样式表的组织方式。 |
| 12 | 328 | no box-shadow/filter/text-shadow/transform in the new CSS | 1-keep | 窗口区域规则：这些属性会画到桌面上；新前端的 CSS 必须继续被这条扫到。 |
| 13 | 336 | each new section banner appears in exactly one stylesheet | 2-rewrite | 读两份样式表里分节标题的出现次数（结构唯一性），随样式表重组而失真。 |
| 14 | 345 | the deleted plugin tab has no banner left behind | 2-rewrite | 读桌宠 `style.css` 里 `/* --- plugins tab` 的缺席；仍是旧样式表的分节。 |
| 15 | 372 | the settings page is four named groups | 2-rewrite | 读 `index.html` 的 `#tab-settings` 区块，数 `<section class="sgroup">`。 |
| 16 | 380 | every group has a heading | 2-rewrite | 读同一段里的 `<h3 class="sgroup-head">`。 |
| 17 | 397 | the settings sub-headings are exactly the pet's own | 2-rewrite | 读该段 `<h4 class="ssub">` 的文案集合。 |
| 18 | 403 | the pet settings carry no AI configuration | 2-rewrite | 读该段是否出现 `llm-` 前缀的 id（在 html 上的缺席断言）。 |
| 19 | 412 | the flat layout left nothing behind | 2-rewrite | 读 `index.html` 的旧 class（`setting`/`num-row`/`setting-label`）与 `style.css` 里同名规则，两条都属旧实现。 |
| 20 | 429 | every settings row uses a shared shape | 2-rewrite | 读该段每个设置行的 class（`field`/`pairs`/`float`），要求只有 `field`。 |
| 21 | 434 | the field rows build on one shared label column | 2-rewrite | 只读 `style.css` 的 `--label-col` 变量与 `.field` 的 grid 定义；旧样式表结构，新前端要重新立这条规则。 |
| 22 | 445 | switch rows keep the checkbox beside its own text | 2-rewrite | 只读 `style.css` 的 `#tab-settings .row`（必须 flex、不能 grid）。 |
| 23 | 456 | the settings tab still scrolls inside the panel and sizes nothing itself | 2-rewrite | 读 `style.css` 的 `#tab-settings` overflow，加 `main.js` 里是否直接设 width/height。 |
| 24 | 488 | the settings page still carries every control | 2-rewrite | 读该段的 34 个 id 白名单（含 `data-dir`/`quit`）；"控件还在不在"要在新组件上重新钉。 |
| 25 | 495 | index.html has a conversation tab body | 2-rewrite | 读 `index.html` 的 `#tab-chat`。 |
| 26 | 500 | the tabs list includes the conversation tab | 2-rewrite | 读 `index.html` 的 `data-tab="chat"`。 |
| 27 | 508 | the tabs are todos/chat/records/settings | 2-rewrite | 读 `index.html` 里 `data-tab="` 的出现次数（=4）。 |
| 28 | 517 | index.html has a conversation slot for the plugin | 2-rewrite | 读 `index.html` 的 `#chat-slot` 挂载点。 |
| 29 | 522 | switchTab toggles the conversation tab with the others | 2-rewrite | 读 `main.js` 里 `switchTab` 的 `classList.toggle` 行。 |
| 30 | 534 | the conversation tab button has its own id, separate from its body | 2-rewrite | 读 `index.html` 的 `#tab-chat-btn`/`#tab-chat` 与 `main.js` 的 `els` 绑定。 |
| 31 | 539 | the conversation tab is hidden when no plugin fills the slot | 2-rewrite | 读 `main.js` 的 `setChatTabAvailable` 函数体三处。 |
| 32 | 549 | switchTab re-resolves the slot, which is what decides the tab exists | 2-rewrite | 读 `main.js` 里 `name === 'chat'` 的分支，并钉那句不该出现的守卫的缺席。 |
| 33 | 567 | the conversation tab is watched while the panel is open | 2-rewrite | 读 `main.js` 的 `setChatTabWatch`/`setInterval`/`CHAT_TAB_WATCH_MS` 等五处。 |
| 34 | 580 | the watch is NOT bound to the tab being hidden | 2-rewrite | 读 `main.js`，断言 `setChatTabWatch(!available)` 不存在（仍在旧前端源码上）。 |
| 35 | 588 | opening the panel re-reads the installed list at once | 2-rewrite | 读 `main.js` 的 `panelWasOpen`/`panelOpenState` 三处。 |
| 36 | 603 | the plugin slot frame is rebuilt when the plugin VERSION changes | 2-rewrite | 读 `main.js` 的 `pluginHost.slotFrame.version === plugin.version`。 |
| 37 | 608 | the floating surface frames are rebuilt when the version changes | 2-rewrite | 读 `plugin-host.js` 的 `surfaceVersion` 比较；文件不重写但属旧前端模块，断言绑其实现文本。 |
| 38 | 613 | the iframe carries the version it was loaded from | 2-rewrite | 读 `plugin-host.js` 的 `frame.dataset.pluginVersion`。 |
| 39 | 620 | no leftover conversation card element | 2-rewrite | 读 `index.html` 的 `#chat` 与 `main.js` 的 `applyChatCard`（双双缺席）。 |
| 40 | 631 | the "待办" jump is still available to the conversation plugin | 1-keep | 只读生成的 `App.js` 导出表与 `App.d.ts`：`OpenTodoPanelFromChat` 必须存在且返回 `main.Layout`。 |
| 41 | 636 | Go reports which tab the frontend should show | 1-keep | 只读 `app_llm.go` 的 `eventChatOpen` 发送点（带 `"open"` 字段）。 |
| 42 | 649 | the double click is detected from the pointer stream | 2-rewrite | 读 `main.js` 的 `now - lastClickAt < DOUBLE_CLICK_MS`。 |
| 43 | 654 | a single click is deferred so a double click cannot flash the panel | 2-rewrite | 读 `main.js` 的 `pendingClick`/`setTimeout`/`clearTimeout`。 |
| 44 | 672 | the double-click branch was found | 2-rewrite | 断言的非空变量 `dbl` 是从 `main.js` 里切出来的双击分支切片——来源仍是旧前端源码。 |
| 45 | 677 | the second click cancels the pending single click before opening the surface | 2-rewrite | 读上一条切出的分支，比较 `clearTimeout` 与 `openChatPluginSurface()` 的先后。 |
| 46 | 683 | the double-click branch returns rather than scheduling a toggle | 2-rewrite | 同一切片：必须有 `return`、不能出现 `TogglePanel`。 |
| 47 | 688 | the deferred single click is what toggles the panel | 2-rewrite | 读 `main.js` 里 `setTimeout` → `App.TogglePanel()` 的间隔。 |
| 48 | 698 | a double click asks the conversation plugin for its surface | 2-rewrite | 读 `main.js` 的 `openChatPluginSurface` 函数体。 |
| 49 | 708 | plugin-host.js positions every surface frame from the layout | 2-rewrite | 读 `plugin-host.js` 的 `pluginSurface`/`rects` 与四个 `style.left/top/width/height` 赋值。 |
| 50 | 719 | an elliptical surface block is drawn as a true ellipse | 2-rewrite | 读 `plugin-host.js` 把 layout 的 `ellipse` 变成 `border-radius: 50%` 的三行。**属窗口区域规则，语义必须保留**，只是断言形式绑在该文件实现上。 |
| 51 | 731 | the plugin frames are sandboxed to scripts only | 2-rewrite | 读 `plugin-host.js` 的 `setAttribute('sandbox','allow-scripts')` 及 `allow-same-origin` 的缺席。 |
| 52 | 745 | the API key field is never populated from state | 1-keep | 读的是**市场程序** `tools/plugin-market/frontend/src/main.js` 的 `case 'secret'` 分支，不是桌宠前端。 |
| 53 | 757 | an untouched key field is omitted rather than sent empty | 1-keep | 同一文件里 secret 的提交分支：空值不上送，否则每次无关保存都会擦掉密钥。 |
| 54 | 771 | a finished focus does not auto-start the rest | 1-keep | 只读 `internal/pomodoro/machine.go` 的状态机。 |
| 55 | 776 | answering the record is what starts the rest | 1-keep | 只读 `app_pomodoro.go` 的 `startParkedPhaseLocked` 调用。 |
| 56 | 781 | the summary prompt cannot be dismissed | 1-keep | 只读 `app_pomodoro.go` 的 `BubbleSummary` + `pending` 守卫。 |
| 57 | 788 | the plan prompt cannot be dismissed either | 1-keep | 只读 `app_pomodoro.go` 的 `BubblePlan`/`planPending`/`pendingPlanWaiting`。 |
| 58 | 802 | the pending flags are read before the window lock is taken | 1-keep | 只读 `app_pomodoro.go` 的加锁顺序（`a.mu.Lock` 之前读 pending）。 |
| 59 | 812 | the frontend does not dismiss the summary prompt either | 2-rewrite | 读 `main.js` 里两处 `bubble.kind !== 'summary'` 守卫。 |
| 60 | 822 | the frontend does not dismiss the plan prompt either | 2-rewrite | 读 `main.js` 三处 kind 守卫（含 Escape 链）。 |
| 61 | 834 | a reminder bubble is still dismissable | 2-rewrite | 读 `main.js` 的 `if (b.kind !== 'reminder') return;`。 |
| 62 | 849 | the backend refuses to open the surface over an unanswered question | 1-keep | 只读 `app_plugin_surface.go` 的 `OpenPluginSurface` 守卫。 |
| 63 | 856 | the refusal names which question is in the way | 1-keep | 只读 `app_plugin_surface.go` 的 `pluginSurfaceBlocked` + `app_pomodoro.go` 的 `questionOwnsTheSlot`。 |
| 64 | 869 | the frontend turns the refused gesture into "go answer it" | 2-rewrite | 读 `main.js` 的 `bubbleAwaitingAnswer`/`focusAnswer`/`els.bubbleInput.focus`。 |
| 65 | 879 | the surface still takes the slot from a notification and the panel | 1-keep | 只读 `app_plugin_surface.go` + `app_pomodoro.go` 的 `unansweredQuestionKind`（提醒不在其中）。 |
| 66 | 894 | the picker cannot be eaten by the popup either | 1-keep | 只读 `app_pomodoro.go`：Pick/Summary/Plan 三种都被保护。 |
| 67 | 905 | the refusal names all three questions differently | 1-keep | 只读 `app_pomodoro.go` 的 `questionOwnsTheSlot` 里有没有 `case BubblePlan`。 |
| 68 | 920 | Enter confirms the summary | 2-rewrite | 读 `main.js` 的 `bubbleInput.onkeydown`、`isComposing` 与 `saveSummary` 调用。 |
| 69 | 927 | Enter confirms the plan too | 2-rewrite | 读 `main.js` 的 `startWithPlan(...)` 与 `App.StartPomodoroWithPlan`。 |
| 70 | 939 | the plan prompt advertises Enter | 2-rewrite | 读 `main.js` 的 placeholder 赋值与 `index.html` 里 textarea 的 placeholder。 |
| 71 | 948 | the summary prompt gets its own placeholder back | 2-rewrite | 读 `main.js` 里把摘要 placeholder 还原的那次赋值。 |
| 72 | 955 | the summary prompt advertises Enter | 2-rewrite | 只读 `index.html` 里 `（Enter 保存）` 这段文案。 |
| 73 | 960 | the Enter handler cannot leak into another bubble kind | 2-rewrite | 读 `main.js` 的 `els.bubbleInput.onkeydown = null;`（复位即缺席断言）。 |
| 74 | 978 | the shortcuts are off by default | 1-keep | 只读 `internal/store/store.go` 的 `GlobalHotkeys: false` 默认值。 |
| 75 | 985 | show is registered before hide | 1-keep | 只读 `internal/desktop/hotkey.go` 的 `DefaultHotkeys` 顺序。 |
| 76 | 992 | the shortcuts avoid the Ctrl+Shift combinations applications already use | 1-keep | 只读 `hotkey.go` 里两个默认 `HotkeySpec`（必须 Ctrl+Alt + S/H）。 |
| 77 | 1023 | a failed registration stops the chain rather than continuing | 1-keep | 只读 `internal/desktop/hotkey_windows.go` 里的 `break`。 |
| 78 | 1030 | the shortcut is released when the feature is switched off | 1-keep | 只读 `app_hotkey.go` 的 `mgr.Register(nil)`。 |
| 79 | 1036 | the shortcuts are released on shutdown | 1-keep | 只读 `app_reminder.go` 的 `closeHotkeys` 调用 + `app_hotkey.go` 的实现。 |
| 80 | 1043 | the tray offers both directions as a mouse fallback | 1-keep | 只读 `app.go` 的托盘菜单项文案与两个 `case` 分支。 |
| 81 | 1053 | a real window call hides the pet rather than a CSS trick | 1-keep | 只读 `internal/desktop/desktop_windows.go` 的 `Hide()`（`SW_HIDE`）。 |
| 82 | 1062 | hiding is refused while a summary is waiting | 1-keep | 只读 `app_hotkey.go` 的 `pendingSummaryWaiting` 守卫。 |
| 83 | 1069 | hiding is refused while the plan is waiting | 1-keep | 读 `app_hotkey.go` 的 `errPlanBlocksHiding` 及其守卫。 |
| 84 | 1078 | the hidden state is runtime-only, not persisted | 1-keep | 一正一负都在 Go：`app.go` 的 `hidden bool` + `store.go` 没有 `Hidden` 字段。 |
| 85 | 1088 | the settings switch is wired and its keys are shown | 2-rewrite | 读 `index.html` 的 `hotkey-enabled` 与 `main.js` 的 render/wire/SaveHotkeySettings 五处。 |
| 86 | 1098 | the key labels come from Go rather than being written in the page | 2-rewrite | 混合：`main.js` 把 `h.show`/`h.hide` 写进 textContent，`app.go` 声明 `Hotkeys` 字段。问的是"页面有没有把键位写死"，属前端渲染接线，重写后要重做。 |
| 87 | 1107 | a refused registration is surfaced rather than silently swallowed | 2-rewrite | 混合：`main.js` 读 `h.problems` 并显示「未能生效」，`hotkey.go` 有 `Problems` json 字段。渲染侧随重写变。 |
| 88 | 1115 | reminders are suppressed while the pet is hidden | 1-keep | 只读 `app_reminder.go` 的 `petHidden` 抑制分支 + 不该出现的 `revealForQuestion`。 |
| 89 | 1132 | a parked record is re-offered after a restart | 1-keep | 只读 `app_pomodoro.go` 的 `PendingRecordID` 重放日志。 |
| 90 | 1139 | a hidden pet is brought back for a question the timer is parked behind | 1-keep | 只读 `app_hotkey.go` 的 `revealForQuestion` + `app_pomodoro.go` 两处调用点。 |
| 91 | 1150 | the pet is revealed WITHOUT being activated | 1-keep | 只读 `desktop_windows.go` 的 `ShowNoActivate` + `app_hotkey.go` 的调用。 |
| 92 | 1162 | answering a question hides the pet again | 1-keep | 读 `app_hotkey.go` 的 `hideAfterQuestion` + `app_pomodoro.go` 里至少 4 个调用点。 |
| 93 | 1172 | an explicit show is not undone by answering a question | 1-keep | 读 `app_hotkey.go` 与 `app.go` 里 `hidden = false` 后紧跟 `revealedForQuestion = false`。 |
| 94 | 1181 | a failed reveal does not record a debt | 1-keep | 只读 `app_hotkey.go` 里 `setQuestionVisibility` 失败分支。 |
| 95 | 1200 | the pet settings carry no speech controls at all | 2-rewrite | 读 `index.html` 的 10 个 `voice-*` id、`main.js` 的 `App.*` 调用与 `els.voice*` 引用（三处缺席断言）。 |
| 96 | 1213 | no retired speech binding is exported | 1-keep | 只读生成的 `App.d.ts`/`App.js`：七个已退役绑定不得再导出。 |
| 97 | 1221 | the pet has no speech fields in its own config | 1-keep | 只读 `internal/store/store.go` 里 `VoiceEnabled` 等的缺席。 |
| 98 | 1233 | the audio capability exists and is permission-gated | 1-keep | 只读 `app_plugins.go` 的 `audio.save`/`audio.play`/`PermAudio`。 |
| 99 | 1239 | the permission vocabulary is the expected seventeen… | 1-keep | 只读 `internal/plugins/capability.go` 的 `AllowedPermissions`（17 项 + 七项具体权限 + 无 `PermBackground`）。 |
| 100 | 1272 | net.credentials is only ever declared together with net.fetch | 1-keep | 读 `market/plugins/*/manifest.json` 的权限配对；数据源是市场插件包，不是前端。 |
| 101 | 1295 | every permission a market plugin asks for exists in the host vocabulary | 1-keep | 读 `capability.go` 常量块 + `market/plugins/*/manifest.json` + `market/index.json` 求子集。 |
| 102 | 1343 | the headless frame is mounted and hidden, and only for an eligible plugin | 2-rewrite | 读 `index.html` 的 `#plugin-background` 与 `main.js` 的 `backgroundPlugins`/三处权限判断。 |
| 103 | 1364 | audio is played from a FILE, never from memory | 1-keep | 只读 `app_plugins.go` 的 `PlayWAVFile(path)` 与 `SND_MEMORY` 的缺席。 |
| 104 | 1373 | every audio file name is checked before it becomes a path | 1-keep | 只读 `app_plugins.go` 的 `flatNamePattern`/`audioNamePattern` + `privatestore.go` 的 `StorageKeyPath`。 |
| 105 | 1394 | the two edge events are emitted as double-quoted literals | 1-keep | 只读 `app_plugins.go` 里 `"timer.phaseEnded"`/`"reminder.fired"` 两个字面量（订阅扫描靠它）。 |
| 106 | 1401 | the edge events carry facts and no wording | 1-keep | 只读 `app_plugins.go` 的 `notifyPluginPhaseEnded` 不带 `text`。 |
| 107 | 1410 | a plugin can keep a headless frame, and only with a permission that fills one | 1-keep | 主体在 Go（`manifest.go` 的 `background` 字段、`app_plugins.go` 的 `PermEvents`/`PermNetWS`），另读 `plugin-host.js` 的 `openBackground`/`backgroundFrame` 两个标识符。 |
| 108 | 1422 | every eligible background plugin gets its own headless frame | 2-rewrite | 读 `plugin-host.js` 的 `backgroundFrames` Map/`syncBackground` 与 `main.js` 的 `syncBackground(backgroundPlugins())`。 |
| 109 | 1433 | the headless frame is hidden, so it takes no space and no clicks | 2-rewrite | 只读 `plugin-host.js` 的 `mount.classList.add('hidden')`——属前端模块，不是窗口区域规则。 |
| 110 | 1440 | the dispatcher gate is shared, not repeated per program | 1-keep | 读 `internal/plugins/dispatch.go` 的 `Dispatch`、`asset.go` 的 `ServePluginAsset`，加市场 `pluginhost.go` 对它的调用。 |
| 111 | 1460 | the legacy voice library is migrated into the plugin | 1-keep | 读 `internal/plugins/voice_migrate.go` 的 `MigrateVoiceData` + `app.go` 的调用。 |
| 112 | 1467 | the migration only READS its source | 1-keep | 只读 `voice_migrate.go` 里 `os.Remove`/`os.Rename`/`RemoveAll` 的缺席。 |
| 113 | 1475 | the migration never points a line at a file that is missing | 1-keep | 只读 `voice_migrate.go` 的 `copied[...]` 记录。 |
| 114 | 1482 | migration slot names satisfy the audio capability | 1-keep | 读 `voice_migrate.go` 的 `sanitiseSlot` + `audioNamePattern`（与 `app_plugins.go` 合读）。 |
| 115 | 1495 | no reply is read aloud | 1-keep | 只读 `app_llm.go` 里 `startSpeech`/`speechGated`/`eventChatReveal` 的缺席。 |
| 116 | 1500 | nothing on the playback path synthesises | 1-keep | 只读 `app_plugins.go`/`app_pomodoro.go`/`app_reminder.go` 三个 Go 文件的 import。 |
| 117 | 1515 | the archived implementation is out of the build | 2-rewrite | 混合：读 `_archive/tts-realtime` 目录是否存在（文件系统），又断言 `main.js` + `app_plugins.go` 都不提 `_archive`。前端那半边要随重写重指。 |
| 118 | 1523 | the settings carry no synthesis fields | 2-rewrite | 混合：`models.ts` + `main.js` 里 `tts*` 字段的缺席；前端半边随重写变。 |
| 119 | 1535 | the records list is grouped into collapsible days | 2-rewrite | ★原标 `1-keep` 有误：块体读 `main.js` 的 `renderRecordDay`/`className = 'record-day'` 与 `style.css` 的 `.record-day-*` 规则。 |
| 120 | 1545 | the newest day is the one that starts open | 2-rewrite | 读 `main.js` 的 `openRecordDays` 初始化。 |
| 121 | 1552 | a day the user collapsed stays collapsed across re-renders | 2-rewrite | 读 `main.js` 的模块级 `openRecordDays`/`seenRecordDays`（折叠加的是 JS 状态而非 DOM）。 |
| 122 | 1561 | a newly appeared day opens by itself | 2-rewrite | 读 `main.js` 的 `seenRecordDays` 判断。 |
| 123 | 1568 | the per-session row no longer repeats the date | 2-rewrite | 读 `main.js` 的 `formatClock` 使用与 `formatStamp` 的缺席。 |
| 124 | 1575 | the plan bubble has its own hint element | 2-rewrite | 读 `index.html` 里 `#bubble-plan`/`#bubble-list`/`#bubble-todo` 的相邻顺序 + `main.js` 的 `els` 绑定。 |
| 125 | 1587 | the plan hint line is reset on every render | 2-rewrite | 读 `main.js` 的 `els.bubblePlan` 复位与赋值。 |
| 126 | 1595 | the records row shows the plan above the actual result | 2-rewrite | 读 `main.js` 的 `planText`/`计划：`/`实际：` 三段模板字符串。 |
| 127 | 1606 | the records editor edits both lines in one write | 2-rewrite | 读 `main.js` 的单次 `App.EditPomodoroRecord` 调用与两个 `record-input`。 |
| 128 | 1616 | the two new bindings exist in the generated module and its types | 1-keep | 只读生成的 `App.js`/`App.d.ts`/`models.ts`（签名、`plan?` 与构造函数赋值）。 |
| 129 | 1639 | the records tab still scrolls inside the panel | 2-rewrite | 读 `style.css` 的 `.record-list` overflow + `main.js` 里是否设尺寸。 |
| 130 | 1652 | the scrollable surfaces share one scrollbar rule | 2-rewrite | 只读 `style.css` 的四选择器合并规则（`.todo-list,.record-list,.chat-log,#tab-settings`），旧样式表结构。 |
| 131 | 1662 | the records list and the chat log are on that rule | 2-rewrite | 只读 `style.css` 的 `.record-list`/`.chat-log` 的 `scrollbar-thumb`。 |
| 132 | 1669 | the private scrollbar copies are gone | 2-rewrite | 只读 `style.css`：每条 `-webkit-scrollbar` 规则的形状（必须 ≥4 个选择器）。 |
| 133 | 1683 | the scrollbar is translucent and trackless | 2-rewrite | 只读 `style.css` 的 `scrollbar-track`/`scrollbar-color` 取值，属视觉规格。 |
| 134 | 1697 | the close button centres its glyph with flex, not line metrics | 2-rewrite | 只读 `style.css` 的 `.icon-btn` flex/align/justify。 |
| 135 | 1704 | the close button carries the measured optical offset | 2-rewrite | 只读 `style.css` 的 `.icon-btn` `font-size:17px` 与 `padding:0 0 5px`。 |
| 136 | 1736 | both frontends actually load their stylesheet | 1-keep | 读两个程序入口的 `import './style.css'`（含桌宠 `main.js`）。钉的是"样式表没人 import 就一个字节都不进包"这个事实，与入口文件内部写了什么无关——入口若仍叫 `main.js` 就原样通过，换名了把路径指过去（gates.md 第 4 类）。 |
| 137 | 1742 | the voice plugin loads its own stylesheet and script | 1-keep | 只读插件包 `market/plugins/com.shixiansi.plugin-voice/panel.html` 的 link/script。 |
| 138 | 1748 | the pet has no voice studio tab and no speech tab | 2-rewrite | 读 `index.html` 的 `#tab-voice` 缺席 + `data-tab` 计数。 |
| 139 | 1756 | the pet has no in-panel way to open a voice studio | 2-rewrite | 读 `index.html` 的 `voice-open-studio` 与 `main.js` 的 `App.OpenVoiceStudio(`（双缺席）。 |
| 140 | 1764 | the standalone voice studio is gone | 1-keep | 只读文件系统：`tools/voice-studio`、`internal/voice`、`internal/tts` 三个目录不存在。 |
| 141 | 1773 | the voice plugin declares the capabilities it needs | 1-keep | 只读该插件的 `manifest.json`（`background` + 五个权限）。 |
| 142 | 1780 | the voice plugin carries its built-in clips as declared assets | 1-keep | 只读 `manifest.assets` 并在插件目录里逐个 `existsSync`。 |
| 143 | 1790 | the built-in clips are real audio, not empty placeholders | 1-keep | 读插件包里的 `.wav` 字节（长度 >1000 且头四字节是 `RIFF`）。 |
| 144 | 1806 | rendering a take and adopting it are separate steps | 1-keep | 只读该插件 `app.js` 的 `audio.save` / `settings.chosen` / `savingLine`。 |
| 145 | 1815 | a missing take falls back to the packaged clip rather than to silence | 1-keep | 只读该插件 `app.js` 的 `builtin-`/`resolveClipFor`。 |
| 146 | 1823 | the plugin asks for bytes when it renders, not text | 1-keep | 读该插件 `app.js` 的 `encoding:'base64'` + `app_plugins.go` 对 encoding 的支持。 |
| 147 | 1832 | the plugin never tries to start the engine itself | 1-keep | 只读该插件 `app.js` 里 `exec`/`spawn`/`StartEngine`/`python` 的缺席。 |
| 148 | 1842 | the market can host a plugin interface | 1-keep | 只读市场 Go 侧 `pluginhost.go`（`ServePluginAsset` + `PluginRequest`）；与控制中心接缝相邻，见文末清单。 |
| 149 | 1851 | the hosted interface is sandboxed to allow-scripts alone | 3-market | ★原标 `1-keep` 改判：块体**只**读 `tools/plugin-market/frontend/src/plugin-ui.js`（iframe 的 `sandbox` 只能 `allow-scripts`）。沙箱语义必须原样搬进新的 plugin-ui。 |
| 150 | 1862 | the market mounts an interface only for an installed and ENABLED plugin | 1-keep | 读市场程序前端 `main.js` 的 `hasInterface`/`mountPluginUI`/`clearPluginUI`；属控制中心接缝，但不是 `plugin-ui.js` 本身。 |
| 151 | 1917 | the plugin market is its own nested module reusing the pet package | 1-keep | 只读 `tools/plugin-market/go.mod` 的 module 行与 `replace`。 |
| 152 | 1923 | the plugin market builds to its own executable name | 1-keep | 只读市场 `wails.json` 的 `outputfilename`。 |
| 153 | 1928 | the market frontend imports its own stylesheet | 1-keep | 只读市场前端 `main.js` 的 `import './style.css'`。 |
| 154 | 1933 | the plugin market is a separate program keeping its own settings file | 1-keep | 只读市场产品代码（`config.go`/`market.go`/`main.go`，测试除外）里 `plugin-market.json` 的出现。 |
| 155 | 1940 | the theme is a scheme on html, remembered in the market settings file | 1-keep | 读市场 `style.css`/`index.html`/前端 `main.js` + 市场 `config.go`/`market.go`；全在市场程序内。 |
| 156 | 1958 | the market top bar is a store header: mark, search, three window controls | 1-keep | 只读市场 `index.html` 的 `<header class="top">` 段。 |
| 157 | 1996 | the market is two sibling views, with exactly one in the flow | 1-keep | 读市场 `index.html` 的 id 集合 + 前端 `main.js` 的 `goGrid`/`goDetail`/`show`。 |
| 158 | 2011 | the market address is a field in the settings sheet, not a strip on the page | 1-keep | 读市场 `index.html`（`market-sheet`/`market-url`）+ 前端 `main.js` 的 `openMarketSheet`。 |
| 159 | 2028 | the enable control is a switch whose wrapper is what gets hidden | 1-keep | 读市场 `index.html` 的 `detail-enabled-row`/`switch-track` + 前端 `main.js` 的 `show(els.detailEnabledRow…)`。 |
| 160 | 2036 | the market page lays the workspace beside a permission rail, and 设置 is a dialog | 1-keep | 读市场 `index.html` 的六个 id、`style.css` 的 `.rail` 滚动 + 前端 `main.js` 的 `renderSettingsState`。 |
| 161 | 2062 | the granted count lives in the permission panel header | 1-keep | 读市场 `index.html` 的 `perm-count`/`panel-note` + `style.css` + 前端 `main.js` 的 `renderPermCount`。 |
| 162 | 2075 | every icon in the market window is a symbol in its own sprite | 1-keep | 读市场 `index.html` 的 `<symbol>` 与 `href="#ico-…"`，外加前端 `main.js` 里运行时换的图标名。 |
| 163 | 2103 | the hero states no status chip, and the controls are one bar at its end | 1-keep | 读市场 `index.html` 的 hero 段（用 `panel-interface` 作位置边界）+ `style.css` 的 `.action-bar`。 |
| 164 | 2123 | each bundled plugin follows the window’s colour scheme | 3-market | ★原标 `1-keep` 改判：读 `market/plugins/**` 三件套 + **`plugin-ui.js`** 的 `?theme=` 与 `setFrameTheme` + 市场 `main.js`/`style.css`。含 `plugin-ui.js`，故进此桶。 |
| 165 | 2145 | the market states a plugin's state as a dot on its card icon | 1-keep | 读市场 `style.css` 的 `.app-dot` 与四个状态类 + 前端 `main.js` 里 `app-dot` 的使用。 |
| 166 | 2157 | the market never names the pet config, let alone writes it | 1-keep | 只读市场产品代码（三个 Go 文件）里 `config.json` 的缺席。 |
| 167 | 2173 | the market explains every permission the backend allows | 1-keep | 读 `capability.go` 常量块 + 市场前端 `main.js` 的 `PERMISSION_LABELS` 映射表，两边求相等。 |
| 168 | 2241 | the headless harness fakes exactly the bindings Go exports | 1-keep | 读 `.dev/run-frontend-harness.cjs` 的假桥接方法数组，与生成的 `App.js` 导出表逐名比较；这是"绑定名对齐"，新前端一样需要。 |
| 169 | 2247 | the jsdom harness fakes exactly the bindings Go exports | 1-keep | 读 `frontend/src/test-harness.js` 的 `methods` 数组（jsdom 假桥接）与 `App.js` 导出表；同上。 |

## `? 待人工` 为什么归零

上一版 70 项标 `?` 的理由是"块体没有引用任何已知变量"——多数是被正则抓出来的局部变量
（`settingsBody`、`groups`、`subHeads`、`dbl`、`newSections`、`typed`、`called`、`emittedNames`、
`harnessMethods`、`marketHtmlIds`），它们的来源都在上面的变量表里，读一遍赋值行就能定位。逐个
追到源头之后没有一项是"看不出来在读什么"的，所以全部判定完毕。

三条判定不是零成本的地方，写在这里备查：

- **#44/#45/#46（双击分支）** 读的是 `mainJs` 上切出来的一个切片 `dbl`，不是独立文件。它问的
  "第二击会不会让待定的单击也触发"是**行为**，改写成组件测试更合适，所以归 `2-rewrite`。
- **#52/#53（API key）** 变量名叫 `marketMainJs`，容易误当成桌宠的 `main.js`。它是市场程序的前端，
  与 Svelte 迁移无关，归 `1-keep`。
- **#168/#169（两套假桥接）** 读的是 `.dev/run-frontend-harness.cjs` 与
  `frontend/src/test-harness.js`，两者都不在 `2-rewrite` 的文件清单里，且 gates.md 明确把
  "绑定名对齐"列为第 1 类：保留，只建议把期望集合改成从 `App.d.ts` 生成。

## `3-market` 清单（控制中心这条线的合并条件）

判据：检查体读 `tools/plugin-market/frontend/src/plugin-ui.js`。

**先纠一个量：`plugin-ui.js` 在检查体里只出现在 2 个 `check` 块中**（行 1857–1859 与
2138–2140，共 6 处断言），不是 7 项。按判据只有 2 项进 `3-market`。为了让这条线的合并条件
不漏东西，下表把**同一接缝**上的另外 5 项也列出来（它们按判据仍是 `1-keep`，但
`plugin-ui.js` 一改就要跟着复核），合计 7 行。

| # | 起始行 | 检查项 | bucket | 与这条线的关系 |
| --- | --- | --- | --- | --- |
| 149 | 1851 | the hosted interface is sandboxed to allow-scripts alone | `3-market` | **必改**。只读 `plugin-ui.js`：iframe 只能 `sandbox="allow-scripts"`，不许 `allow-same-origin`。重写时必须把这条断言原样搬过去——它是插件沙箱的边界，不是样式。 |
| 164 | 2123 | each bundled plugin follows the window’s colour scheme | `3-market` | **必改**。读 `plugin-ui.js` 的 `plugin-asset/…?theme=`、`theme:` 消息与 `setFrameTheme`。`plugin-ui.js` 一改这条立刻红。 |
| 148 | 1842 | the market can host a plugin interface | `1-keep` | 接缝的 Go 侧：市场 `pluginhost.go` 必须复用 `plugins.ServePluginAsset` / `PluginRequest`，不许另写一份。 |
| 150 | 1862 | the market mounts an interface only for an installed and ENABLED plugin | `1-keep` | 接缝的调用方：市场前端 `main.js` 的 `hasInterface`/`mountPluginUI`/`clearPluginUI`。`plugin-ui.js` 的入口签名变了，这里也要改。 |
| 160 | 2036 | the market page lays the workspace beside a permission rail, and 设置 is a dialog | `1-keep` | 钉 `#panel-interface` 这个静态挂载点存在，且不许被 `replaceChildren` 重建——它承载的正是 `plugin-ui.js` 装进去的文档。 |
| 163 | 2103 | the hero states no status chip, and the controls are one bar at its end | `1-keep` | 用 `#panel-interface` 当位置边界（动作条必须在它之前），宿主布局动了它也会红。 |
| 110 | 1440 | the dispatcher gate is shared, not repeated per program | `1-keep` | 接缝的权限闸门：市场 `pluginhost.go` 必须调共享的 `ServePluginAsset`，插件的每次请求都走同一个闸门。 |

**给这条线的合并条件**：`149` / `164` 的断言不许随 `plugin-ui.js` 一起消失，只许跟着搬到新文件
的对应位置；另外 5 项在 `plugin-ui.js` 改动后必须重跑一遍门禁确认还是绿的。
