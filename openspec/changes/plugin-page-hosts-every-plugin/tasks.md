# tasks: 插件页挂载每个插件的界面

## A. 共用的一层：插件间托盘

- [x] **A1 — `internal/plugins/share.go`**
  内容：`ShareSet/ShareGet/ShareRemove`（`Gate` 参数）+ `RegisterShare(r, gate)` + `ValidShareName`
  / `MaxShareValue`；语义与原来逐字一致（`share/` 目录、`share-` 旧文件、64 KiB 上限、`shareRead`
  校验、未发布 = 空值）。
  判据：`go test ./internal/plugins/` 与宿主全绿（既有的 share 用例没有改一行）。

- [x] **A2 — 桌宠改成转发**
  内容：`app_plugins.go` 的 `pluginShareSet/Get/Remove` 变成三行转发（名字保留：能力表与测试
  指着它们），删掉搬走的常量与辅助函数。
  判据：`go vet ./...` 干净；`grep maxPluginShareValue app_plugins.go` 零命中。

- [x] **A3 — 控制中心注册托盘**
  内容：`pluginhost.go` 里 `plugins.RegisterShare(r, gateAdapter{m.manager})`；文件头的
  「served / not served」两段重写，并写明 `surface` 不再是拒绝挂载的理由。
  判据：`share_test.go` 三条通过（往返 + 磁盘位置、未声明就拒、只删自己的）。

## B. 控制中心：挂载与左栏

- [x] **B1 — `mountable()` 只留「已装 + 已启用 + 有 entry」**
  内容：删掉 `surface` 那一句；`noInterfaceWhy()` 的第四种说法（"只能在桌宠上打开"）随之删除。
  判据：`npm run smoke` 的「声明了浮层面的插件也在这里挂界面」。

- [x] **B2 — 删掉「待办」页**
  内容：`Todos.svelte` 删除、`App.svelte` 左栏改为 开始 / 插件 / 日志 / 高级、`bridge.ts` 去掉
  `Todos`/`Todo` 与 `FAKE_TODOS`、`settingsapi_read.go` 去掉 `Market.Todos()`。
  判据：`node .dev/check-frontend.cjs` 的「the todo list has one home」；`npm run check` 0 error。

- [x] **B3 — dev 桩补一行"声明了 surface 的插件"**
  内容：`FAKE_ROWS` 加番茄钟（`surface`、`background`、`panelSlot`、5 个 config），并让冒烟断言
  它挂了界面、设置弹窗里有「专注时长」= 25。
  判据：`npm run smoke` 63/63。

## C. 三个插件的文档（`market/`，独立仓）

- [x] **C1 — 番茄钟 2.2.0：设置与记录**
  内容：manifest 声明 `focusMinutes`/`shortBreakMinutes`/`longBreakMinutes`/`longBreakEvery`/
  `autoStartNextPhase`（默认 25/5/15/4/true）；`panel.html` 加「最近专注」区与 workshop 的一行
  说明；`app.js` 读 `pomodoro-records-v2`（只读、新在前、20 行）、每 5 秒重读、`workshop` 里
  隐藏倒计时按钮；`style.css` 加记录与 `view-workshop` 的规则。
  判据：`tests/plugin-pomodoro-frames.test.js` 新增 5 条（设置键与 machine 读的键**逐项相等**、
  渲染顺序、空列表说明、workshop 隐藏按钮、桌宠浮层面保留按钮）。

- [x] **C2 — 对话 1.7.5：workshop 只读**
  内容：`workshop` 标志（`surface.slot === 'workshop'`）、`canChat` 与 `surface` 按钮随之关闭、
  状态行写「这里只显示记录；发送请在桌宠旁的对话浮层面」。
  判据：`tests/plugin-app.test.js` 新增 2 条（workshop 只读 + 桌宠 panel 挂载仍有输入框）。

- [x] **C3 — 待办 1.2.0：列表轮询**
  内容：列表角色每 2 秒 `storage.get`；`writes` 记录在飞的写、轮询期间暂停。
  判据：`tests/plugin-todo.test.js` 新增 2 条（会重读、写未应答时暂停且之后恢复）。

## C4. 插件页能「更新到 vX」

- [x] **C4 — 装了旧包时给出升级入口**
  内容：`Catalogue()` 在页面启动后**后台**读（离线失败就当没有）；`updateFor()` 只在目录版本
  **更新**时成立（降级不给按钮）；按钮调 `Market.Update`（保持启用与已授权），成功后就地
  `refresh()` 并重读目录，按钮自己消失。
  判据：`npm run smoke` 的四条（落后时有按钮且指名版本、已最新时没有、点完给出确认、按钮消失）；
  `.dev/check-frontend.cjs` 的「an installed plugin can be UPDATED from its page」。

  为什么它是本轮的必需品而不是顺带：**插件的界面跟着插件走**。三个文档都改了行为，装着的旧包
  只会显示旧行为（番茄钟的按钮点了没反应、对话的输入框能点但发不出去），而页面看起来是新
  的——这比"没有界面"更难判断。

## D. 打包、重建与门禁

- [x] **D1 — 重新打包**
  内容：`pwsh -File market\tools\package.ps1`（6 个 zip + `index.json` + `internal/plugins/sample`），
  `internal/plugins/sample/embed.go` 指到新版本，删掉三个被取代的旧 zip。
  判据：`TestBuiltinSampleAgreesWithTheMarketCatalogue` 绿；`go build ./...` 干净。

- [x] **D2 — 重建两个程序**
  内容：`pwsh -File tools\build-all.ps1`。
  判据：两个 exe 的时间戳都是本轮。

- [x] **D3 — 静态门与文档**
  内容：`.dev/check-frontend.cjs` 里钉住"surface 不许挂载"的那条改成**负断言**（并保留"停用即
  不挂"），新增两条（挂载规则、待办页不许回来）；`docs/plugin-market.md` 加 §2.9；
  `market/README.md` 加 §4.5（三个插件在 workshop 里的差别与两条坑）；README 的左栏描述改成
  四页并说明待办页的去处；AGENTS.md §3.3 加一段"两个宿主各服务哪些方法、文档靠 slot 分辨"。
  判据：`node .dev/check-frontend.cjs` → 124 条全绿。

## E. 未做

- [ ] 控制中心里的对话**能发消息**（需要把 `llm.chat` 连同凭据、预算、流式事件搬进来——用户
      确认本轮不做）。
- [ ] 番茄钟在控制中心里**驱动计时**（计时帧属于桌宠；要做得先有一条跨进程命令通道，而现在的
      `command` 键只有桌宠的计时帧读）。
- [ ] `storage` 的并发写版本号（"最后写入者赢"仍在，见 design §7）。
- [ ] 语音 / QQ / B 站三个插件在控制中心里的界面**没有回归测试**（它们本轮没改，冒烟只覆盖了
      番茄钟这一行 surface 的形状）。
