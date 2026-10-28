# design: 插件界面预览与自动排版检查

## 1. 三个决定

| 决定 | 备选 | 为什么 |
| --- | --- | --- |
| **独立页面 + 静态服务器**（`preview.html` + `preview.mjs`） | 控制中心里的一个开发页 | 控制中心的界面是**编译期**嵌进 exe 的：换一次预览就得重建市场程序，而且 rect/granted 都得改代码。独立页面只换 URL 参数 |
| **假宿主抽成 `fake-host.mjs`，两个执行环境共用** | 各写一套 / 只让预览页 import `plugin-check.mjs` | 两套假宿主必然分叉，而分叉的假宿主比没有更坏：它会告诉你"没问题"。方法表、权限表、`okResult`、`createFakeHost` 都在这一个文件里；`plugin-check.mjs`（jsdom）与 `preview.html`（真浏览器）各只提供"怎么把消息送进插件"。直接 import `plugin-check.mjs` 不行——它是带 CLI 的脚本，import 就会跑起来 |
| **排版断言用真实浏览器的 `getBoundingClientRect`** | 继续用 jsdom | jsdom 的 `getBoundingClientRect()` 恒为 0，这就是五次翻车全都漏过的原因。没有别的替代品：裁剪是排版问题，必须有排版引擎 |
| **用系统已装的 Edge/Chrome，不装 Playwright**（实现时的修正） | `npm i -D playwright` + `npx playwright install chromium` | Playwright 首次要下载约 130 MB 浏览器，在这台机器上比它要抓的 bug 还慢（用户原话「npm 太慢了」）。Edge 就是产品 WebView2 的引擎，命令行自带 `--headless=new --screenshot` 与 `--dump-dom`，正好够用：截图给眼睛，`--dump-dom` 里 `data-report` 给断言。**零依赖、零下载**，且引擎与产品一致 |

## 2. `preview.html` 长什么样

```
┌───────────────────────────────┬──────────────────────────┐
│  ┌─────────────────────────┐  │ 收到的消息               │
│  │  iframe（插件的 entry）  │  │ ready  {v:1}             │
│  │  尺寸 = 声明的矩形       │  │ request storage.get      │
│  │  W×H，居中，边框标出     │  │ response ok              │
│  └─────────────────────────┘  │                          │
│                               │ ── 排版检查 ──           │
│  [rect 0 ▾] [slot pomodoro ▾] │ ✗ #ask-ok bottom 268 > 260│
│  [granted ▾] [deny all ☐]     │ ✓ 其余元素在框内          │
│  [重载] [发送 config.changed]  │                          │
└───────────────────────────────┴──────────────────────────┘
```

要点：

- **iframe 尺寸严格等于声明的矩形**，外面画一圈虚线标出"卡片边界"。真宿主就是这么裁的——
  只有把边界画出来，人才看得见"被裁掉"这件事。
- 宿主一侧实现与 `frontend/src/plugin-host.js` **同样的消息**：收 `ready` 回 `init`
  （`pluginId` / `permissions` / `granted` / `theme` / `surface`），收 `request` 回 `response`，
  能把 `event` / `theme` / `surface` 推给插件。**不判断权限**（和真宿主一样：拒绝由 granted 决定）。
- `surface.kind` / `slot` / `rectIndex` 由 URL 参数决定，所以同一份文档可以按三种挂载预览：
  `surface`（宠物旁）、`panel`（workshop）、`background`（常驻帧）。
- **"deny all" 开关**：把所有 `response` 变成 `{ok:false}`，用来看"权限被拒时界面是否崩"——
  这一条目前只有 `plugin-check.mjs` 的协议组能查，但看不到界面。
- **挂两个帧，不是一个**（实现时的第二处修正，用户提出「点确认和跳过没有反应」逼出来的）：
  清单声明 `background: true` 时，除了那张卡片，还挂一个隐藏的 `kind:'panel', slot:'background'`
  帧（`display:none`，和宿主一样：隐藏的 iframe 照样在跑），两个帧**共用**同一份存储与 share 托盘。
  番茄钟的计时器与状态机住在常驻帧里，卡片只是视图：点「确定」是 `share.set` 一条 command 出去、
  常驻帧下一秒读到并应用、写回 state、卡片下一秒读回重画。只挂一个帧的话，按钮点下去只会留下一条
  没人读的命令——**而工具会一本正经地说"布局没问题"**。那正是"假宿主撒谎"最坏的那种形态。
  例外：`--slot workshop` 不挂常驻帧，因为控制中心不跑常驻帧（它不服务 `events`）。
- 排版检查**实时**跑（`MutationObserver` + `ResizeObserver` + 每次消息之后 + 500ms 兜底）：
  列出越界的元素，红色；全在框内，绿色。**只量浮层面那个帧**——常驻帧是隐藏的，它没有"放不放得下"。

## 3. `preview.mjs` 的三个模式

```bash
# 1) 人工看：起服务器，打印地址（零依赖）
node docs/plugin-testkit/preview.mjs ../../plugins/com.shixiansi.plugin-pomodoro --open

# 2) 断言：无头 Chromium 里跑排版检查（系统已装的 Edge/Chrome）
node .../preview.mjs <dir> --check [--rect 0] [--slot pomodoro] [--state gate]

# 3) 截图：给人和 agent 看（系统已装的 Edge/Chrome）
node .../preview.mjs <dir> --shot gate.png --state gate [--click "#ask-ok"]
```

- 服务器用 `node:http` + `node:fs`，**零依赖**；MIME 只需 `.html/.js/.css/.json/.wav/.png`。
  它还服务 `/testkit/*`（`preview.html` 要 `import` 那份 `fake-host.mjs`——`file://` 下模块
  脚本会被 CORS 拦掉）。
- `--state gate|running|idle` 是**假宿主的行为开关**：它决定 `storage.get` / `config.get` 回什么。
  `gate` 回一个停住的 state（`asking:'summary'`, `endsAt:0`, `remainingOnHalt:300`），
  于是"等你回答"那一屏一条命令就能复现。
- `--click` 把点击真的送进插件再重新量一遍（页面自己按 URL 参数点，不需要 CDP）。
- **浏览器是可选的**：`--check`/`--shot` 在找不到 Edge/Chrome 时报 `SKIP` 并说明服务器模式仍可用，
  退出码 0（与 `plugin-check.mjs` 里 jsdom 缺失时同一套规矩）。
- `--check` 不给 `--rect` 时**逐块矩形都查**：380×260 放得下的卡片，在 320×120 那块里可能就放不下。
  没有声明 `surface` 的插件（常驻帧 / 铺满窗口的面板）报 **SKIP**：没有"放不放得下"这回事，
  编一个 380×260 去量它就是在报告一个插件从没答应过的尺寸。
- **测量必须由"变了"驱动，不能靠定时器猜。** 第一版用 `requestAnimationFrame` 排队测量，而无头 +
  `--virtual-time-budget` 下 rAF 可能一帧都不再派发，队列标志永远卡住 → 整页只量了最初那一次，
  于是同一个插件六次里两次报"全在框内"。现在：MutationObserver 盯住 iframe（插件一重画就重量）
  + `setTimeout(…, 0)` 排队 + 500ms 定时器兜底。**一个会抖的检查器比没有更坏。**

## 4. 排版断言的具体规则

对**每一个可见元素**（`offsetParent !== null` 且 `getClientRects().length > 0`）：

1. `rect.bottom > cardH + 0.5` → 越界（被下边缘裁掉）。这是五次翻车的共同症状。
2. `rect.right > cardW + 0.5` → 越界（被右边缘裁掉，长文本常见）。
3. `document.documentElement.scrollHeight > cardH + 1` → 内容比卡片高（即使元素自身没越界）。
4. 允许白名单：`position: fixed` 的元素、`.visually-hidden` 之类（本仓库暂无，留接口）。

**为什么不用 `elementFromPoint` 判断"是否可点"**：真机里点击会落在窗口区域之外（原生裁剪），
那是宿主的事，不是排版。这里只回答"画得出来吗"。

## 5. 与现有工具的关系

```
fake-host.mjs           协议 + 权限表 + 方法表 + 结果表（一份实现，两个环境都 import 它）
        │
        ├── plugin-check.mjs        manifest / package / source / protocol（jsdom，零依赖）
        │
        └── preview.html            ← 新增：看得见 + 排版断言 + 截图（真浏览器）
                 ▲
            preview.mjs            ← 服务器 / --check / --shot / --click（系统 Edge/Chrome）
```

一份协议知识、两个执行环境。`plugin-check.mjs` 负责"不装也能查的"，`preview` 负责"必须看见的"。

## 6. 风险与对策

| 风险 | 对策 |
| --- | --- |
| 机器上没有 Edge/Chrome（或装在别处） | `--check`/`--shot` 是可选路径；没有就 `SKIP`，服务器模式照样能人工看。查的顺序写死在 `BROWSERS` 里 |
| 假宿主与真宿主分叉 | 假宿主**只**有一个实现（`fake-host.mjs`），`plugin-check.mjs` 与 `preview.html` 都 import 它；真宿主改动时那份表是唯一要改的地方（`AGENTS.md §3.3` 已约定） |
| 假宿主"没有记忆"，点完「确定」界面弹回原样 | 预览页给 `previewAnswers` 一个 `createMemoryStore()`：`storage.set` 写进去，`storage.get` 先读它。一个会撒谎的预览工具比没有更坏 |
| iframe 的 `sandbox="allow-scripts"` + `file://` 下 module script 不执行 | 一律走 `http://127.0.0.1:<port>`，不用 `file://`。预览页的 iframe **故意不加 sandbox**：量排版要读 `contentDocument`，而真宿主的 opaque origin 差异由 `plugin-check.mjs` 的静态红线那组兜住 |
| 预览通过、真机不通过 | 真机差异只有：窗口区域（原生裁剪）、`theme` 通道、跨进程设置、以及控制中心不跑常驻帧这一条（`--slot workshop` 已按此处理）。前两条预览里已模拟（画边界、发 theme）；跨进程那条靠 `config.changed` 事件推 |
| 预览说"点了没反应"，其实是预览少挂了一个帧 | 常驻帧按清单挂上，两个帧共用存储与 share 托盘（B4）。这条的教训写在 tasks 里：一个挂不全帧的预览，比没有预览更容易让人得出错的结论 |
| 有人把它当门禁 | 明确：它是**开发工具**，不进 `check-all.ps1`（需要浏览器，且它是给作者用的） |
