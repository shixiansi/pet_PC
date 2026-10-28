# tasks: 插件界面预览与自动排版检查

> 执行位置：`F:\dev\pet_pc\market\`（插件仓；`docs/plugin-testkit/` 已有 `plugin-check.mjs`）。
> 全部改动都在插件仓，**不需要**动宿主、不需要重建 exe。

## A. 假宿主与预览页（零依赖，先做）

- [x] **A1 — 抽出假宿主**
  把 `plugin-check.mjs` 里与"假宿主"有关的部分抽成 `fake-host.mjs` 的可复用导出：
  `createFakeHost({ manifest, granted, surface, mode, answers, deliver, autoRespond })`，
  返回 `{ onMessage, respond, send, takePending, log, requests, errors, badIds, unknownMethods }`；
  `okResult` / `METHODS` / `PERMISSIONS` 与 `previewAnswers` / `createMemoryStore` / `sampleState`
  都在这一份里，两个环境 import 同一份（`plugin-check.mjs` 在 jsdom、`preview.html` 在真浏览器）。
  判据：`plugin-check.mjs` 的协议组行为不变。**已核对**：6 个插件、81 条检查的 `--json` 输出与
  改动前**逐字节相同**（`Compare-Object` 无差异，`identical=True`）。
  踩到的坑记在这里：第一版把 `granted=[]` / `tolerant` 两轮映射成"一条都不答"，观测到的请求数
  从 13/15 掉到 7——原来旧实现在 `ready` 到达时就启动了一次 `flush()`，那两轮其实收到的是**拒绝**。
  为了"行为不变"，`flush()` 现在也在 `onMessage` 返回 `'ready'` 时启动，且只有 `ok` 一轮答应。

- [x] **A2 — `preview.html`**
  一个文件：URL 参数 `plugin` / `rect` / `slot` / `kind` / `granted` / `deny` / `state` / `click`；
  左侧 iframe 尺寸 = 清单里第 `rect` 块的 `w×h`，外圈虚线标边界；右侧消息流水 + 排版检查结果；
  顶部一行控件（rect / slot / kind / deny all / 重载 / 发 `config.changed`）。
  假宿主**不再内联**在这个文件里（A1 之前它有一份自己的 `resultFor`，那正是"第二套假宿主"）。
  判据：`?plugin=…&rect=0&slot=pomodoro&kind=surface` 能起，`ready→init→request→response` 全通，
  消息栏看得到每一条。

- [x] **A3 — 实时排版检查**
  每次消息之后 + `ResizeObserver` + 500ms 定时：对每个可见元素检查 `bottom > h`、`right > w`、
  `documentElement.scrollHeight > h`，越界标红并列出 `#id` 与数值。
  判据：把番茄钟 2.3.4（`git show 50632a3` 出来的旧版本）放进预览，**红**；2.3.9 **绿**。

## B. `preview.mjs`（服务器 + 系统 Chromium，已实现，无 npm 依赖）

- [x] **B1 — 静态服务器**
  `node:http` + `node:fs`，零依赖；`/plugin/*` 映射到插件目录，`/testkit/*` 映射到本工具目录
  （`preview.html` 要 import `fake-host.mjs`），`/` 返回 `preview.html`；
  `--port`（默认 7801，被占则 +1）、`--open`（`start` 打开默认浏览器）。
  判据：`node preview.mjs <dir> --open` 后人工能点、能切 rect、能看排版栏。

- [x] **B2 — `--check`（系统 Edge/Chrome）**
  无头 Chromium，逐 rect 打开预览、等 `init` 完成、跑与 A3 相同的断言，输出
  `PASS/FAIL + 越界元素清单`，退出码 1/0；缺浏览器时 `SKIP` 并说明服务器模式仍可用。
  判据：对 2.3.4 退出码 1 且点名 `#ask-ok`；对 2.3.9 退出码 0。
  **修掉的三个真问题**：① 原来结尾是 `process.exitCode = opts.check ? 0 : 0`，越界也返回 0——
  "FAIL 但退出码 0"正是最坏的那种工具；② 原来只查 `--rect` 给的那一块（默认 0），
  现在不给 `--rect` 就逐块矩形都查（`chat` 三块 300×40 / 308×60 / 308×64 都跑到了），
  而没有声明 `surface` 的插件（只有常驻帧或铺满窗口的面板）报 **SKIP** 而不是编一个 380×260；
  ③ **它会时好时坏**：同一个 2.3.4 六次里两次报"全部在框内"。根因不在浏览器，在预览页——
  测量是用 `requestAnimationFrame` 排队的，而无头 + `--virtual-time-budget` 下 rAF 可能一帧都
  不再派发，`reportQueued` 于是永远卡在 true，整页只量了最初那一次（那一次往往在插件画完之前）。
  改成 `setTimeout(…, 0)` + 用 MutationObserver 盯住 iframe（插件一重画就重量），定时器退成兜底。
  改完：2.3.4 六次全 FAIL、2.3.9 六次全 PASS。**一个会抖的检查器比没有更坏**，这条比功能本身重要。
  另外把浏览器的输出改成**落临时文件**而不是管道：受限沙箱里 `execFile` 抓 stdout 会
  `spawn EPERM`，报错还指向浏览器。调试用 `PETPC_PREVIEW_DUMP=<file>` 留下 `--check` 读到的那份 DOM。

- [x] **B3 — `--shot` / `--state` / `--click`**
  `--state gate` 让假宿主回一个停住的 state；`--click "#ask-ok"` 先点再截（页面按 URL 参数自己点，
  不需要 CDP）；`--shot` 出一张卡片 + 右栏的图。
  判据：`--shot gate.png --state gate` 出一张 380×260 的卡片图，肉眼能看清"问题 + 输入框 + 确定/跳过"。
  判据（补的）：`--shot after.png --state gate --click "#ask-ok"` 出的图里**门关掉了、休息在跑**。

- [x] **B4 — 挂两个帧，于是"点一下有没有用"能被验证**（B3 的第一版做不到这件事）
  第一版只挂 `entry` 一个帧：点「确定」只看到一条 `share.set` 出去、屏幕不变，因为番茄钟的
  状态机在**常驻帧**里。这不是"预览的边界"，是预览少了一半——用户当场指出「番茄钟要提交时点
  确认和跳过没有反应」，而在那个工具里根本看不出这是不是 bug。
  现在：清单声明 `background: true` 就再挂一个隐藏的 `kind:'panel', slot:'background'` 帧
  （`display:none`，和宿主一样：隐藏的 iframe 照样在跑），两个帧共用同一个存储与同一个 share 托盘
  （`createMemoryStore()` 各一个，由 `previewAnswers` 的 `store` / `share` 传下去）。
  一次点击因此走完整一圈：按钮 → `share.set command` → 常驻帧下一秒读到并应用 → 写回 state →
  卡片下一秒读回并重画。`--click` 点完等两拍再量、再截。
  例外：`--slot workshop` 不挂常驻帧——那是控制中心的挂载方式，而控制中心不跑常驻帧
  （它不服务 `events`，见 `tools/plugin-market/cc/src/lib/plugin-frame.ts`）。
  判据：`--shot --state gate --click "#ask-ok"` 与 `--click "#ask-skip"` 两张图都是"门关掉、
  04:56 休息在跑"；`--check` 仍然只量浮层面那一块（2.3.9 PASS / 2.3.4 FAIL，各 3 次稳定）。

## C. 文档与接线

- [x] **C1 — `plugin-testkit/README.md`**
  新增「预览：装之前先看一眼」一节：三种用法、`--state` 的取值、排版检查看什么、
  缺浏览器时怎么办、`--click` 的边界、常设反向样本；另加"这个目录里有四个文件"一节，
  并把"它查不到什么"表里的"排版"一行改成指向 `preview.mjs`。

- [x] **C2 — `plugin-dev.md` §12**
  自检清单开头加一句："装之前先 `preview.mjs` 看一眼最满的状态（门开着 / 回复最长）"。

- [x] **C3 — `AGENTS.md §3.3`（宿主仓）**
  契约与工具清单里加上这个预览器（四个文件各自是什么），并写清它的定位：开发工具，不是门禁
  ——需要浏览器，服务对象是插件作者，所以不进 `check-all.ps1`、本仓 CI 也不跑它。

## D. 回归证据（已取得）

- [x] **D1 — 用五次翻车当样本**
  `git show 50632a3:…/` 与当前 2.3.9 各跑一次 `--check`，两次输出贴在这里：
  ```
  FAIL  com.shixiansi.plugin-pomodoro rect 0（380×260，state=gate）：6 处越界     # 2.3.4，exit=1
    ✗ 内容比卡片高：scrollHeight=277 > 260
    ✗ #compose 底边 277 > 260（被下边缘裁掉 17px）
    ✗ #ask 底边 277 > 260（被下边缘裁掉 17px）
    ✗ div.ask-actions 底边 277 > 260（被下边缘裁掉 17px）
    ✗ #ask-ok 底边 277 > 260（被下边缘裁掉 17px）
    ✗ #ask-skip 底边 277 > 260（被下边缘裁掉 17px）

  PASS  com.shixiansi.plugin-pomodoro rect 0（380×260，state=gate）：全部元素在卡片之内   # 2.3.9，exit=0
  ```
  截图也对：2.3.4 那张图上「确定/跳过」被虚线边界切掉，2.3.9 那张完整。
  这是这个平台"真的能抓住这类 bug"的唯一证明。

- [x] **D2 — 一个 fixture**
  `market/tests/fixtures/overflow-card/`：声明 200×120 的矩形，里面 15 行 × 20px = 300 高的列表。
  `--check` 报 14 处越界、退出码 1；`plugin-check.mjs` 对它 0 FAIL（只有"固定高度却从不请求
  `surface.blocks`"那条 WARN）——也就是"只有一处错"，工具说 PASS 时先用它确认工具还看得见东西。

- [x] **D3 — 用它量到了"点了没反应"的那 0.8 秒**
  用户的第二份报告是「点确认和跳过没有反应」「要点好几次才有效」。把两个帧都挂上之后，这个平台
  第一次能量出中间那段：从按下到那一屏真的变，**0.824s**（视图发 command → 常驻帧下一秒读到并应用
  → 视图下一秒读回重画）。屏幕上那 0.8 秒什么都没变，人就会继续点，而第二下是没有意义的。
  插件 2.3.10 因此让按钮自己认下这一下：按下即禁用 + 写「已提交…」、状态栏说明在等计时器确认、
  再补四次 250ms 的连读把确认提前（实测 **0.583s**）；3 秒还不确认就把按钮还回来并明写
  「计时器没有回应（看日志）」——一次落空的点击不该看起来像一次被忽略的点击。
  三条都写进了 `market/tests/plugin-pomodoro-frames.test.js`（+3 条，全仓 480 条通过）。
  这是"平台补上了它该补的那一半"的第二个证明：它量出来的东西，前面五次翻车一次都没量到。
  用户的第三份报告（截图：卡片上写着「计时器没有回应（看日志）」）说明**看门狗本身有用**——它把
  一次沉默失败变成了一句能查的话。顺着它查到的根因不是延迟，是**状态不同步**：保存的状态里
  `asking:"plan"` 旁边跑着一个专注，而常驻帧自己的门标记是空的；`answerGate` 只看后者，于是把
  每条 answer 命令**静默吞掉**（清空命令键、什么都不写）。为什么不同步：回答一个门会连写两次状态
  （先写带 `asking` 的，再写清掉 `asking` 的），两次重叠，后一次不一定落盘。插件 2.3.11 三处修：
  ① `answerGate` 以**保存的状态**为准（`state.asking || asking`）；② 帧认不出门时先重读状态、
  命令留在通道里下一秒重试（只重试一次，不会死循环）；③ 同一 key 的写在同一拍内合并成一次，
  落盘的是后一次。+3 条测试（28 条全过，全仓 483）。
  这条教训值得记下来：**"两个副本 + 一次静默丢弃"就是这类 bug 的配方**——先写下"谁是权威"，
  再让不可应用的东西**说出来**而不是消失。

## E. 未做（写清，避免误以为覆盖了）

- [ ] 真机（Wails + WebView2）的自动化驱动：窗口区域裁剪、`theme` 通道的**真实**首帧、
      跨进程 `config.changed` 的时序，仍需人工看一眼。
- [ ] 视觉回归（截图逐像素比对）：现在只判"放不放得下"，不判"好不好看"。等需求真的出现再说。
- [ ] 进 `check-all.ps1`：需要浏览器，且它是给插件作者用的工具，不是仓库门禁。
