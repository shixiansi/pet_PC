# AGENTS.md

给在本仓库里干活的 AI agent 的规则。**动手之前先读完这一页。**

这一页只写「不读就会做错」的东西：边界、不变量、踩过的坑。功能细节看
[README.md](README.md)，专题看 `docs/`，为什么这样设计看 `openspec/changes/`。

---

## 1. 这是什么

Windows-only 的效率型桌宠：**待办 / 番茄钟 / 提醒是主角，AI 是沙箱插件**。
Go + Wails v2 + 原生 JS（没有前端框架、没有 TypeScript）。没有 Web 服务、
没有跨平台、没有遥测。

写代码前请记住这个立场，它决定了大量设计：

> **桌宠只是入口，三件事都在插件里；宿主不认识任何具体的插件。**
> 待办、番茄钟、对话各自是插件，界面是它们自己声明的浮层面。宿主剩下的是：窗口、区域裁剪、
> 一个气泡卡、一个右键菜单、以及一套权限闸门。别在宿主里"顺手加个 AI 功能"——工具调用、
> 长期记忆这类东西要先想清楚它属于宿主还是属于插件（现在的答案是插件）。
>
> **没有一个"面板"可以往里加东西了。** 那个 380x520 的 tab 面板已经退休：它有过的两个标签
> （待办 / 对话）各自变成插件的浮层面，`Layout` 里只剩它那块**矩形**（宠物旁边的槽位）。
> 所以"给桌宠加个界面"只有三条路：加一格右键菜单、加一种气泡、或者写一个插件。
> manifest 的 `panelSlot` 现在只回答"右键菜单的哪一格找我"
> （[app_chat_slot.go](app_chat_slot.go)），不再是"占哪个 tab"。

## 2. 目录边界

放错地方的代价不是整洁问题，是**门禁会失败或者东西会丢**。

| 路径 | 是什么 | 必须知道的 |
| --- | --- | --- |
| 仓库根 | 宿主程序（`package main`） | `App` 上有 155+ 个导出方法，它们就是 Wails 生成的 JS 绑定面 |
| `internal/` | 可分单元测试的子系统 | `desktop`（Win32/DPAPI/注册表）、`llm`（协议与 SSE）、`plugins`（manifest/闸门/私有存储）、`store`（持久化） |
| `tools/plugin-market/` | **独立 Go module** | 有自己的 `go.mod`；根目录的 `go vet ./...` **不会**下潜进去 |
| `market/` | **独立 git 仓库**（被 `.gitignore` 排除） | 插件源码、`dist/` 打包产物、插件自己的测试都在这里。在本仓库里动它、又等 CI 去发现，是白动 |
| `_archive/` | 归档的旧实现（含被放弃的实时 TTS） | 下划线开头 → Go 在匹配 `./...` 时**会跳过**。归档 Go 代码放这里 |
| `scratch/` | 临时试验、截图、日志 | 被 gitignore，**但在 module 内**（不以 `_`/`.` 开头）。在这里放任何 `.go` 文件，会让 `go vet ./...` 和 `go test ./...` 整体编译失败 |
| `.dev/` | 门禁脚本 + 本机工具链 | 整体被 gitignore，但**门禁脚本逐个白名单放回**（见 `.gitignore` 里的 `!` 行）。新增一道门禁脚本，必须同时加白名单，否则换台机器就没了 |
| `openspec/changes/<change>/` | 非平凡改动的 proposal / design / tasks | 已有 11 个先例，沿用 |

### 立刻会咬人的两个坑

1. **`scratch/` 里不许放 `.go`。** 曾经有一个探针引用已删除的 `NewApp`，报错
   `undefined: NewApp` 指向一个没人编辑过的包，把全部门禁挡住。要么改名成
   `*.go.txt`，要么放 `_archive/`。
2. **`.dev/*` 的白名单是依赖闭包。** `check-all.ps1` 是入口，它 dot-source
   `env.ps1`、启动 `verify-*.ps1`。这四类文件任何一个漏了白名单，README 承诺
   的「一条命令跑全部门禁」就会在别的机器上消失。这个疏漏已经发生过三次。

## 3. 不许破坏的不变量

每条都有守门测试或静态检查。**改了它，就要同时改守着它的那个检查**——
这是本仓库最重要的协作约定。

### 3.1 权限闸门（这是全项目最不能动的东西）

[internal/plugins/dispatch.go](internal/plugins/dispatch.go) 里的检查顺序是硬契约：

1. 插件已安装且已启用 →
2. 方法**在本程序**里存在 →
3. 用户已授予该方法需要的权限 →
4. **只有到这里**才碰参数。

1–3 任何一步拒绝，都**什么都没发生**：没写待办、没弹气泡、没发请求。
这就是「你拒绝了这项权限」能成立的原因，而不是一句事后打印的话。

- 权限词表：[internal/plugins/capability.go](internal/plugins/capability.go) 的
  `AllowedPermissions`，**现在 13 项**，顺序就是安装对话框的绘制顺序。
- **加一项权限要同步七处**（`timer.control` 那次实测就是七处，漏一处就红）：
  1. `capability.go` 的常量——注释里写清它 gates 什么、**不** gates 什么
  2. `AllowedPermissions`（顺序就是安装对话框的绘制顺序）
  3. `PermissionLabels`（Go 侧的唯一权威文案）
  4. 至少一个注册了它的方法（否则 `TestCapabilityTableIsComplete` 红）
  5. `manifest_test.go` 的 `want` 列表 + 测试名里的数字（`…IsTheThirteen`）
  6. `.dev/check-frontend.cjs` 里那份**硬编码**的期望词表 —— 它故意不自动跟随
     `AllowedPermissions`，因为它的职责正是发现那张表被改了
  7. `tools/plugin-market/cc/src/lib/bridge.ts` 的 **dev 桩** `LABELS` —— 控制中心的
     `main.js`/`PERMISSION_LABELS` 已经不发货了（`cc/` 是唯一界面，见
     `docs/plugin-market.md` §2.8）；生产界面直接读 `Market.PermissionLabels()`，
     所以剩下的唯一副本就是浏览器里的假数据。桩落后了，`.dev/check-frontend.cjs` 会红

  外加 README 的权限表和标题里的数字。第 6、7 处是这一路最容易漏的：它们不在
  `internal/plugins` 里，改完 Go 侧全绿，静态接线检查才红。
  故意没有 `background` 权限——常驻帧由 `events` / `net.ws` / `net.fetch` 门控。
- 没有方法支撑的权限是一个"用户被问了一个什么也不做的开关"，
  `TestCapabilityTableIsComplete` 和 `TestAllowedPermissionsIsTheThirteen` 就是拦它的。
- **退休一项权限走的是同一张清单的镜像**：删常量、删 `AllowedPermissions`、
  删 `PermissionLabels`、删或改注册它的方法（否则 `TestCapabilityTableIsComplete` 红）、
  改 `manifest_test.go` 的 `want` 与测试名、改 `.dev/check-frontend.cjs` 的期望词表、
  改 `bridge.ts` 的 `LABELS`，外加 README 的表格行与标题里的数字。
  `timer.read`/`timer.control` 就是这么退的（17 → 15）；**多一步**：还要把已发布的插件里
  声明它的那份 manifest 一起改并重新打包，否则装好的插件下次启动就被判为非法 manifest。

### 3.2 窗口只有两个占位，且必须精确裁剪

气泡与插件浮层面**共用宠物旁边那个位置，同一时刻只能有一个可见**。窗口区域
（`SetWindowRgn`）必须恰好覆盖那一个的**包围盒**；多一块或少一块，页面未绘制的区域就会
露出桌面，变成一条"玻璃带"。这是本项目历史上反复出现的 bug，
见 [docs/reminder-pomodoro.md](docs/reminder-pomodoro.md) 8.2。

> **这里原来是三个占位，第三个是面板**，而且它是全项目裁剪 bug 的老家：那个 380x520 的
> 矩形随开关反复裁来裁去，10(c)/10(d) 两节的跳位与淡蓝色带都是它。面板退休之后（阶 3），
> "同一位置只放一个"的规则**没有变、也不需要变**——只是它的成员从三个变成了两个，
> 而 `app.go` 的 region switch 因此少了一个 `case`。窗口尺寸也没变：`Layout.PanelX/Y/W/H`
> 仍是宠物旁边那块**矩形**，插件浮层面就摆在那里。

**区域只给矩形，形状归页面。** 2026-10 之前这里给的是圆角矩形和椭圆
（`CreateRoundRectRgn` / `CreateEllipticRgn`），结果每个圆角都有锯齿：GDI region 是
**1-bit 掩码**，没有 alpha，系统会把曲线量化成台阶，再叠在浏览器已经抗锯齿的边上。
现在 `SetWindowRegion` 只接受 `desktop.Bounds`（普通矩形），圆角、椭圆、以后的气泡尖角
都由 CSS 的 `border-radius` / 伪元素画；窗口是半透明的（`WindowIsTranslucent`），页面
留空的像素合成到桌面。
**代价是明写着的**：圆角外那几像素不再把点击透给桌面，而是落在本窗口上。要拿回它们需要
逐像素命中测试（`WM_NCHITTEST` + 缓存 alpha 掩码），那是另一个量级的机制。

**页面上的新控件必须先回答"它在哪个矩形里"。** 桌宠左上角的对话按钮（`#pet-chat`，
悬停才出现的那个）就是这条的样板：它长在 `#pet` 的框**之内**（绝对定位在 4px 处），因为
浮层面和气泡都不在时窗口恰好被裁成宠物那一块。挂在宠物边上的按钮会被原生区域切掉；把区域
放大来容它，就会在宠物旁留下一条页面从不绘制、却会吞掉桌面点击的窗口。所以"给桌宠加个按钮"
不是加一个 `div`，而是先决定它住在谁的盒子里。

**右键菜单是气泡，不是第四个占位。** 2026-10 之前桌宠右键借的是托盘的原生弹出菜单
（`Tray.ShowMenuAtCursor`），理由是"页面画的菜单得挤进裁剪区域，那就是第四个占位"。这条推理
错在前提：菜单要的东西气泡全都有——固定矩形、按矩形裁剪、以及"同一位置只放一个"的折叠规则。
所以它是 `BubbleMenu`（[app_pet_menu.go](app_pet_menu.go)），`app.go` 的 region switch 与
`Layout` 一行未改，而开面板 / 开浮层面 / 隐藏 / 拖动 / 相位切换全都自动把它收掉。下次想做
一个"要自己一块矩形"的界面，先问一遍：**它能不能就是一种气泡？**

**插件浮层面的 iframe 必须透明**（`#plugin-surface .plugin-frame { background: transparent }`）。
它曾经用 `var(--panel-bg)` 铺底板——那是区域还是圆角形状的时代留下的，那时的底板就是"被裁出来的
形状"。区域改成包围盒之后，底板变成插件圆角外面的深色方块，以及插件自己留白处（比如气泡尾巴）
的深色带：一个 `#1d2027` 的三角形压在 `rgba(28,30,36,.94)` 上，肉眼就是"没有尾巴"，为此查了
五轮。约定是：插件声明矩形并负责画满它，宿主只出位置。

**浮层面留白要透明，还得管住两条别人碰不到的地方。** 插件文档里写着 `:root { color-scheme: dark }`
（配色需要），而 Chromium 在 `color-scheme: dark` 下会给**画布**铺 `#121212`——根元素的
`background: transparent` 也压不住它，于是帧里"没画东西"的那一条 10px 带就是黑的。开关是
`:root { color-scheme: only light }`，并且**必须写在 `:root` 上**：`:root` 是伪类，特异性高于
`html`，写在 `html` 上的那条规则会被上面的 `:root { color-scheme: dark }` 直接吃掉（这一条也
白白多花了一轮）。想确认某一层到底透不透明，别猜——在那一层里画一个亮色方块，看得见就说明那层
可画、黑在它后面。

### 3.3 插件帧只能这样长

[frontend/src/plugin-host.js](frontend/src/plugin-host.js) 是安全边界：

- `<iframe sandbox="allow-scripts">`，**故意不含** `allow-same-origin`
  （帧是 opaque origin，够不到父文档和存储；`document.domain` 也爬不出来）；
- 唯一通道是 `postMessage`，且只接受**本文件自己创建的** iframe 的 `contentWindow`；
- **前端永远不判断权限**，每次请求都经 `App.PluginRequest` → 上面那个闸门。

**控制中心里是同一件事的第二份实现**：[tools/plugin-market/cc/src/lib/plugin-frame.ts](tools/plugin-market/cc/src/lib/plugin-frame.ts)
（请求走 `Market.PluginRequest`，闸门与能力表是本程序那一份）。三条规则一字不改地照用。
它还多一条**只属于它的**约束：**iframe 归这个模块，不归 Svelte**——模板里的条件块、
`{#key}`、列表 diff 都会重建节点，而重建一个 iframe 就是销毁那个正在按用户权限运行的文档
（正在填的表单、滚动位置、已经答过的握手）。所以页面只给一个稳定挂载点，帧由 `$effect`
命令式建、命令式销毁。

还有一条**过边界前必须复制成纯数据**的规则：Svelte 5 的 `$state` 会把**纯对象与数组**
深度代理，而 Proxy **过不了** `postMessage` 的结构化克隆——`init` 里带一个被代理的数组，
抛的是 `Uncaught DataCloneError: … could not be cloned.`，而这个异常发生在**消息监听器
里面**：页面看起来完全正常，插件永远收不到它那唯一一条 `init`，界面上就停在
「等待宿主初始化…」。2026-10-27 真机上踩的就是这个（`inst.Granted` / `Manifest.permissions`），
所以 `plugin-frame.ts` 在入口处把要重复使用的值读成局部副本。**注意它为什么在浏览器里
复现不出来**：Wails 的绑定返回**纯 JSON**（会被代理），而 dev 桩里的行是 `createFrom`
建的**类实例**（Svelte 不代理类实例）——桩必须摊平成纯数据，否则它和真宿主不是同一个世界。

**两个宿主各服务哪些方法，以及文档靠什么知道自己在哪。** 桌宠服务全部 13 项权限；控制中心
服务 `storage.*` / `config.*` / `audio.*` / `net.fetch` / **`plugin.share.*`**，不服务
`bubble.say` / `surface.*` / `llm.chat` / `events.subscribe` / `todos.*` / `timer.*`——那些要的
是宠物窗口、宠物的计时器、宠物的模型预算。**别再把"挂不了"当成解法**：这三个插件的界面
（待办列表、番茄钟记录、对话历史）只在控制中心的插件页里有地方放，而上一轮因为 `share.set`
不支持就干脆不挂它们，等于把界面藏起来；正确做法是把**规则**搬到 `internal/plugins/share.go`
让两个宿主共用（`RegisterShare`）。

文档判断"我在哪个窗口"**只用 `init.surface.slot`**：桌宠给 `chat` / `todos` / `pomodoro` /
`background`，控制中心给 `workshop`。`kind` 不能用来判断——常驻帧的 `kind` 也是 `panel`。
在 `workshop` 里缺的能力要**说出口**（对话插件把输入框变成只读并写明去哪发，番茄钟隐藏计时
按钮并写明计时在桌宠上），一个点了没反应的控件比没有控件更糟。

**对外的契约只有一份，而且它住在插件仓**：[`market/docs/plugin-dev.md`](market/docs/plugin-dev.md)
（GitHub：<https://github.com/shixiansi/pet-pc-plugins/blob/main/docs/plugin-dev.md>），配套的自检
工具是 `market/docs/plugin-testkit/`（一个小目录，插件作者整个拷进自己的仓就能跑，不需要本仓库、
也不需要宿主代码）：

| 文件 | 是什么 |
| --- | --- |
| `plugin-check.mjs` | 四组静态/协议检查（jsdom）。CI 与作者都用它 |
| `fake-host.mjs` | **假宿主**：协议、13 项权限表、35 个方法表、结果表。上面那个与下面的预览页共用这一份——两套假宿主必然分叉，而分叉的假宿主比没有更坏 |
| `preview.mjs` / `preview.html` | **排版预览器**：把 entry 装进声明的矩形，用系统 Edge/Chrome 无头跑，`--check` 越界就 FAIL、`--shot` 出图、`--state gate` 直达「等你回答」那一屏、`--click "#ask-ok"` 点一下再截。清单声明 `background` 时**连隐藏的常驻帧一起挂**——插件的行为有一半住在那儿，只挂一个帧会让"点了没反应"变成工具看不见的事 |

**预览器的定位是开发工具，不是门禁**：它需要一台装着 Edge/Chrome 的机器，而且它服务的对象是
插件作者（`market/` 那个公开仓），所以**不进 `.dev/check-all.ps1`**，本仓的 CI 也不跑它。它补的
是 jsdom 永远补不上的那一半——`getBoundingClientRect()` 在 jsdom 里恒为 0，于是"按钮被卡片下边缘
切掉"这类问题在整条链路上没有一处看得见（2026-10-28 番茄钟为此连翻五次车）。

**它为什么不在这里**：本仓是**私有**的，插件仓是**公开**的。契约放在本仓，就等于只对"已经有
宿主源码的人"可见——而他们正是最不需要它的人。这也意味着**本仓不许再放第二份**：
根目录的 `docs/plugin-dev.md` 只是一句指针，谁想"顺手复制一份过来"就是又开了一个腐坏源。

手册是写给别人（以及别人的模型）看的自包含说明：manifest 字段与校验、桥的每条消息、
13 项权限与 35 个方法的 payload/返回/上限、几何与主题、打包与发布。所以：**改动 manifest 的
字段或规则、桥的消息、任何能力方法的签名或上限、退休一个接口，都要同时改插件仓里那份手册和
工具里那张方法表**。退休一个接口时**从文档里删干净**，不要留一张"已退休接口"的表：那份文档的
读者是没看过旧资料的人（以及模型），死名字只会被当成真 API 记住——这条是用户当场指出来的，
第一版就写错了。文档第 2 节的示例插件是逐字跑过的：一个 jsdom 脚本把它当真实插件驱动过一遍
（握手 → storage → bubble.say → 拒绝路径）。

### 3.4 音频按文件播放，绝不用内存

`PlayWAVFile(path)`。`SND_MEMORY` 需要调用方缓冲区活过调用、而 GC 不知道——
句子播到一半被回收就是静音或崩溃。静态检查会拦 `SND_MEMORY`。

### 3.5 名字从 payload 来，就不能直接拼路径

- 音频槽名是**扁平**的：`flatNamePattern`（`^[A-Za-z0-9_-]{1,64}$`）；
- 存储键**允许目录**，所以用 path-aware 的
  [`StorageKeyPath`](internal/plugins/privatestore.go) 校验；
- 插件目录由宿主决定，方法**从不**从 payload 取路径。

### 3.6 密钥不出宿主进程

DPAPI（[internal/desktop/crypto_windows.go](internal/desktop/crypto_windows.go)）。
插件只拿到 `{set, mask}`；`llm.borrow` 借的是"用别人的配置发请求"，密钥本身
也不会交给借入方。

### 3.7 内置 sample 包的字节必须和市场一致

`internal/plugins/sample/*.zip` 是离线兜底源，`.gitattributes` 把这些 zip 标成
`binary` 就是为了保住摘要（`core.autocrlf` 会改写文本型 zip 的字节）。
**不要用文本工具编辑它们**；两处字节不同时，`TestBuiltinSampleAgreesWithTheMarketCatalogue`
会红——它是真失败，不是噪音。

### 3.8 日志：一直在写、有等级、没有正文

`internal/logging`。两个程序各写 `<dataDir>\logs\<程序>.log`（`pet.log` / `market.log`），
默认 INFO，超过 2 MB 轮转并保留一代 `.1`。三条不许破：

- **不许再给日志加开关。** 旧的 `debug.log` 只在 `PETPC_DEBUG=1` 时存在，于是每份报告都
  缺证据、每次排查都要用户重启一次程序。降噪用等级（`PETPC_LOG_LEVEL=warn`），不加门。
- **一次插件调用一行，但等级由方法决定、重复会被合并。** 数据面（`storage.*` / `share.*` /
  `config.*` / `memory.*`）是 **DEBUG**——插件读写自己的文件是管道，不是事件；动作面（气泡、
  浮层、音频、模型、网络、进程、订阅）是 **INFO**——那是人能看见或要花钱的事；失败一律 **WARN**。
  **纯显示**的 `pet.badge` 也记 DEBUG：倒计时每秒推一次，INFO 里只会是噪音（它被拒时仍然是 WARN）。
  这条规则的第一版是"全都 INFO"，在真机上把日志写成了 99.5% 的轮询（9729 行 `share.get` +
  2842 行 `storage.get` + 580 行 `config.get`，对 32 行 `llm.chat`），用户直接问"这个等级放这些
  能看个啥"。另外同一 `(插件, 方法, 等级)` 5 秒内只写一行、合并数附在下一行上
  （`internal/logging.Throttle`），因为轮询既可能读也可能写。分类表在
  `internal/plugins.DataMethod`，两个宿主共用；新方法忘了分类就默认按"动作"记 INFO。
- **正文与密钥绝不进日志。** payload 只记字节数，回复只记字数，`Settings.String()` 只写
  `key=set`。日志是给 issue 贴的（`Market.Diagnostics` 直接复制它），一处
  `log.Debug("llm", payload)` 就让每份报告都不能外发。

守它的检查：`internal/logging`（格式往返/等级/轮转/伪造行）、`internal/llm/log_test.go`、
`app_log_test.go`、`tools/plugin-market/logapi_test.go`，以及 `.dev/check-frontend.cjs` 的
「the log is always on」那一条（它盯着 `PETPC_DEBUG` 的门不许回来）。
格式与取舍见 [docs/logging.md](docs/logging.md)；用户可见的说法在 README「日志」。

## 4. 怎么验证

**全部门禁**（12 道，需要交互桌面与 WebView2）：

```powershell
pwsh -File .dev\check-all.ps1
```

**CI 跑的子集**（`.github/workflows/ci.yml`，不需要桌面）：

```powershell
go vet ./...                       # 根 module
go test ./... -count=1
go vet ./...                       # tools/plugin-market（独立 module，要单独跑）
go test ./... -count=1
cd frontend                       && npm ci && npm run build && npm test
cd tools/plugin-market/frontend    && npm ci && npm run build && npm test
```

**静态接线检查**（纯 Node，无依赖，但需要 `market/` 检出）：

```powershell
node .dev\check-frontend.cjs
```

CI 刻意没覆盖的东西和原因，写在 `ci.yml` 的文件头——**别以为 CI 绿了就是全绿**。

## 5. 提交与文档

- **Conventional Commits**，正文用中文，必须写清「为什么改」和「验证了什么」。
  提交信息里的验证命令要能被人复跑。
- **代码注释写「为什么」，不写「是什么」。** 本仓库的注释密度是刻意的（很多
  注释在记录"为什么不是更自然的那个写法"），别删，也别写复述代码的注释。
- **非平凡改动走 openspec**：`openspec/changes/<change>/{proposal,design,tasks}.md`。
- **README = 用户能看到的行为**；专题细节进 `docs/`；历史留在 git 与 openspec 里。
- **改了行为就同步文档。** 本仓库最容易腐坏的就是"文档说 X、代码是 Y"，
  已经踩过的例子：
  - 权限数在 README、代码注释、测试名里各写了一个值（真实是 15）；
  - README 说工具调用"留了 `internal/llm/tools.go` 的位置"——该文件从未存在；
  - README 说"`Provider` 接口已留位"——`internal/llm` 里没有这个接口，
    `Provider` 只是配置字段 `LLMProvider`（服务商预设名）。

  写文档时**去代码里确认标识符真的存在**，比写得好更重要。

## 6. 许可证红线

Coopanion 一类项目是 **AGPL-3.0**。可以读设计、可以理解协议格式，
**不要复制代码、素材或字段命名**——否则本仓库也得按 AGPL 开源。形象贴图的授权
范围另算，尤其不能拿。

## 7. 本机环境已知坑（不是代码问题）

- **`.dev/` 与 `tools/` 下的 `.ps1` 必须带 UTF-8 BOM。** Windows PowerShell 5.1 把无 BOM 的
  文件按系统 ANSI 代码页（这里 GBK）解码，中文一多就会吃掉一个引号或 here-string 的结束符：
  同一个文件在 `pwsh` 下 0 个解析错误、在 5.1 下 43 个，而且错误指向的行完全正常
  （`check-all.ps1` 表现为整段 C# 被当成 PowerShell 解析，报「`using` 语句必须出现在最前面」）。
  `check-all.ps1` 的 `script encodings` 门禁会抓这件事；**别用会去 BOM 的编辑器保存它们**。
- 系统 Go 是 **1.21**，而 `go.mod` 要 1.22，`env.ps1` 又设了 `GOTOOLCHAIN=local`。
  本地 1.22.12 工具链在 `pet_pc\.dev\gotool\go`（**不在** `.dev\gotool\go`）；
  `env.ps1` 已做两处回退，别再写死一处。
  **不想走 `env.ps1` 也行**：模块缓存里已经有 `toolchain@v0.0.1-go1.22.12`，
  用 `GOTOOLCHAIN=auto`（别设 `local`）就会自动切过去——`go env GOROOT` 会指向
  `C:\Users\fan\go\pkg\mod\golang.org\toolchain@…`。此时只要把 `GOCACHE` 指到 `%TEMP%`
  下面（`.dev\gocache` 对 pwsh 进程只读）。2026-10-27 用这条路径跑通了全部 Go 门禁。
- 受限沙箱下 `.dev` 的子目录不可写 → Go 的 `GOCACHE`/`GOMODCACHE` 要指到工作区
  **外部**的临时目录。**别指到 `scratch/` 下面**：那在里面会让 `./...` 把依赖缓存
  当成被测包（会看到 `FAIL github.com/pkg/errors` 这种莫名其妙的输出）。
- `.git` 与 `.dev` 在受限模式下不可写：`git commit` 和删改文件需要一次性提权。
  **2026-10-27 复核过这条的准确边界**：对 pwsh 进程，**工作区根与 `scratch/` 可写**，而
  `.dev` / `internal` / `frontend/src` / `tools` / `market` 这些**子目录是只读的**
  （`Access to the path … is denied`，不是沙箱策略拒绝）。所以：
  - **文件编辑工具不受这条限制**，这些位置都能改——"三处只读所以不能委派"这个结论是错的；
  - **打包与安装必须提权**：`market/tools/package.ps1` 要写 `market/dist`、
    `market/index.json` 和 `internal/plugins/sample`；
  - **`npm test` 必须提权**：vitest 要 esbuild 起子进程，受限下是 `spawn EPERM`（沙箱不许开
    命名管道），报错会指向 `vitest.config.js` 加载失败，看着像配置问题，其实是同一道边界。
- `internal/desktop` 里有一批用例要访问 HKCU 的开机自启项，沙箱下必然失败
  （`open Run key: Access is denied`）。托管 runner 上它们是能跑的。
- **改了 `.go` 就必须重建，否则用户看到的永远是上一个二进制。** 「好像没变化」的第一个
  怀疑对象不是代码，是时间戳：`Get-Item build\bin\*.exe` 和刚改过的 `.go` 对一下。
  2026-10-26 一天之内这件事发生了三次（桌宠两次、市场程序一次），每次都被当成"功能没做"。
- **`wails build -s` 会跳过前端构建，exe 里嵌的还是旧 `frontend/dist`。** 2026-10-28 就这么
  踩了一次：`index.html`/`main.js`/`style.css` 都改完、`go test` 与 `check-frontend` 全绿、
  exe 也重建了，但加的元素就是不出现——因为 `-s` 只编译 Go，`dist` 还停在改动之前，exe 里那份
  页面根本没有那个元素。**改了 `frontend/` 就必须 `wails build`（不带 `-s`）**；`-s` 只在
  "我只动了 Go、前端确实没变"时用。验证办法：`Select-String frontend\dist\index.html -Pattern
  <新元素的 id>`，或直接扫 exe 的字节串。静态门禁读的是 `frontend/src`，它**看不见**这件事，
  能看见的是 `.dev/check-all.ps1` 里那几道真机门禁。
- **两个程序是分开的：重建桌宠不会重建市场程序（控制中心）。** 而控制中心的界面是**编译期**
  嵌进它自己的 exe 的（`//go:embed all:frontend/dist`，而 `frontend/dist` 是 `cc/dist` 的副本）。
  所以只改了 `cc/` 之后，`wails build` 桌宠、甚至跑一遍桌宠的前端构建，都不会让用户看到任何变化——
  2026-10-27 就这样又踩了一次：插件页的主从布局写完了、`frontend/dist` 也换了，但用户打开的还是
  0:00 那个二进制，于是「插件页面又没有了」。要做的是**重建市场程序**并按 `tools/build-all.ps1`
  的位置复制（`tools/build-all.ps1` 一条命令就够；只重建它时注意 `wails build` **不要**省 `-s`，
  否则它会去构建 `tools/plugin-market/frontend`（旧 JS 界面）并覆盖 embed 的那棵树）。
  验证用**页面上的构建标记**：控制中心插件页左上角 `已装插件 · v5`——这一行就是为这件事存在的；
  或者按 `docs/plugin-market.md` §6.1 的办法扫 exe 的字节串。
- **「数据热了」不等于「改动被应用了」。** `Store.Config()` 会懒重读 `settings.json`，
  但只有"用到时才读一次"的设置因此生效；**启动时算进窗口几何的东西不会**——它需要一个
  重新应用的人（`app_settingswatch.go` 里的缩放监视就是这么来的）。写热重载的验收条件时，
  写「改完立刻生效」，不要写「重启后生效」。

## 8. 明确不做（YAGNI）

迁移 Electron、做跨平台、11 种语言、遥测、Steam / OBS 集成、CUA（让 AI 直接操作
电脑）。这些与"效率桌宠"的定位无关，投入大、风险高。
Wails 比 Electron 轻是优势，不是待还的技术债。

## 9. 改动前自检

1. 我改的东西属于哪个边界？（宿主 / `internal` / 独立 module / 独立仓库 / `scratch`）
2. 我碰到 §3 的某个不变量了吗？碰了就要改守着它的那个检查。
3. 我加权限了吗？§3.1 的七处 + README 的表格与标题都改了吗？
4. 我改行为了吗？README 里对应的说法跟着改了吗？
5. 我跑了什么？把命令和结果写进提交信息（"应该能过"不算验证）。
