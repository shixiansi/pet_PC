# PetPC 插件化（插件宿主 + 市场客户端 + 内置示例源）＋主仓上云准备

- change_name: `plugin-system`
- 目标仓库: `F:\pet_pc`（非 greenfield：仓库已存在，`.git` 已在，无任何 `[remote]` 段）
- 上游规格卡: 已批准规格卡（PetPC 插件化 + 插件市场，三块交付落在两个 GitHub 仓库）
- 上游澄清清单: `wiki/queries/checklists/petpc-github-muzfq8ul.md`（Q1–Q16、doubts 3 条、risks 4 条、placeholders 3 条、manifest 文件基线）
- 姊妹 change: `openspec/changes/plugin-market`（市场仓 `market/` 内容与打包脚本；本 change 的内置示例源依赖它先完成，见 design.md §10）

## 1. 问题（现状实证，逐处复读源码/门禁脚本确认）

| 事实 | 出处（已实读） |
|---|---|
| 仓库**无任何远端**，只有本地分支 `feat/pomodoro-prefocus-plan`，工作区干净 | `.git/config`（仅 `[core]`，无 `[remote]`）；`.git/HEAD` = `ref: refs/heads/feat/pomodoro-prefocus-plan` |
| 窗口**永不 resize**，只改区域；宽 = 宠物宽 + `panelGap`(10) + `store.PanelWidth`(380)，高 = `max(store.PanelHeight=520, 宠物高)` | `app.go:20-21`、`app.go:510-531`（`totalW := petW + gap + panelW`；`totalH` 取 `panelH` 与 `petH` 较大者） |
| 可见区域 = `desktop.SetWindowRegion` 矩形并集（可圆角），内部 `CombineRgn(RGN_OR)` | `app.go:652-702`（region switch 三态）、`internal/desktop/desktop_windows.go:337-389`、`panelRadius = 14`（`app.go:500`） |
| 面板矩形固定 380×520 | `internal/store/store.go:225-235`（`PanelWidth=380`、`PanelHeight=520`） |
| 气泡／实时浮层与面板**共用同一槽位且互斥**；提问气泡不可被顶掉 | `app.go:977-1001`（`SetPanelOpen` 折叠气泡与浮层）、`app_pomodoro.go:1069-1071`（`showBubble` 关面板）、`app_llm.go:1033-1053`（`OpenLive` 遇未答提问**拒绝且不改状态**）、`docs/reminder-pomodoro.md:487-512` |
| 几何「只算一次」：浮层矩形由 Go 纯函数算出，同时用于区域裁剪与前端定位 | `app_live.go:156-243`（`computeLiveOverlay`）、`app.go:734-737` |
| Wails v2 **无多窗口 API**，窗口靠类名 `"wailsWindow"` 找回 | `internal/desktop/desktop_windows.go:21`（`appWindowClass`）、`docs/llm-chat.md:203-222`、`README.md:177` |
| 面板现有 4 个 tab，对话是面板内 tab（不是独立窗口） | `frontend/index.html:51-56`（4 个 `data-tab`）、`frontend/src/main.js:1268-1280`（`switchTab`） |
| 设置页控件被门禁**白名单硬断言**（恰好 7 个 `.sgroup`、id 集合完全相等） | `.dev/check-frontend.cjs:223-230`、`:309-331` |
| 静态门禁硬编码：Go 事件文件清单、新增 CSS 段清单、两处 tab 计数 `=== 4`、共享滚动条正则、`layoutReturning` 清单 | `.dev/check-frontend.cjs:132-135`、`:179-206`、`:346`、`:1297`、`:1184-1188`、`:105` |
| 行为 harness 硬编码绑定白名单；缺一个方法会在 `wire()` 中途抛错 | `.dev/run-frontend-harness.cjs:76-97`；jsdom 侧同名清单 `frontend/src/test-harness.js:47-65` |
| 资源路由先例：AssetServer 只服务 `/pet-image`（服务数据目录里的文件） | `main.go:24-32`、`app.go:1174-1205` |
| 两 exe 构建顺序 / 工具链需 dot-source | `tools/build-all.ps1:18-70`、`.dev/env.ps1:1-41`、`.dev/check-all.ps1:51-78` |
| `raw.githubusercontent.com` 国内不通，既有实现用 `gh-proxy.com` 绕行 | `docs/genshin-tts.md:479`、`:688-689`；`_archive/tts-realtime/tools/tts-p0/fetch.ps1:64-72` |
| Go 1.22.0 / wails v2.11.0，无 fsnotify 类新依赖 | `go.mod:1-10`；`wails.json:5-6`（`frontend:build = npm run build`） |

## 2. 变更目标（承规格卡三块交付，逐条落任务）

1. **主仓上云（仅本地）**：`main` 分支指向当前工作提交、`origin` 指向 `https://github.com/shixiansi/pet_PC.git`（私有）、插件化开发在 `feat/plugin-system`；**线上 push 由用户手动执行**，本 change 不含任何 push 动作。→ T1–T4
2. **插件宿主**：`internal/plugins`（manifest 解析、已装发现、zip 安装 + sha256、卸载、启停、权限授予与持久化、市场源解析、宿主能力）；`internal/plugins/sample/` 内置示例源（`go:embed`）；根包 `app_plugins.go` 绑定与 `app_plugin_surface.go` 几何；`app.go` 的 `Layout`/region 新增插件浮层面；`main.go` 新增 `/plugin-asset/` 资源路由；前端第 5 个 tab「插件」＋ `plugin-host.js` 沙箱桥 ＋ 两种界面落点。→ T8–T114（任务总表见 tasks.md）
3. **插件市场（桌宠侧）**：可配置市场基址（`Config.MarketBaseURL`），默认走 `gh-proxy.com` 镜像指向 `raw.githubusercontent.com`；不可达时回退内置示例目录并给出提示；安装流程 下载 → sha256 → 解压 → 授权 → 启用。市场仓内容本身在 `plugin-market` change。→ T7、T16–T20、T26–T32、T81、T98
4. **框架验证**：官方示例对话插件（在 `plugin-market`）真实验证「插件 tab 内面板视图 + 流式渲染 + 弹气泡 + 调用宿主已配置的大模型」，**完全不改动既有 `#tab-chat`**（`frontend/src/chat.js` 与 `app_llm.go` 的会话状态机保持原样）。→ T31、T32、T49、T51、T73、T91、T115、T126，`plugin-market` M2–M8

## 3. 与规格卡/清单的四处口径冲突及收敛结论（不留未定分支）

| # | 规格卡/清单原话 | 仓库事实 | 收敛结论（唯一路径） |
|---|---|---|---|
| C1 | 「缺省内嵌一份随程序分发的本地示例目录（**market/index.json**）作为兜底」 | `market/` 按同一份 impl_decisions 被主仓 `.gitignore` 忽略（该目录是**独立 git 仓**），而 `go:embed` 只能嵌入主仓内已提交的路径；忽略路径一旦缺失会让主仓构建直接失败 | 内置示例源改放**主仓内已提交**的 `internal/plugins/sample/index.json`（＋同名 zip），`//go:embed all:sample` 嵌入；`market/index.json` 是市场仓的远端目录，两者由同一个打包脚本产出**同字节**的 zip 与相同 sha256（T32 与 `plugin-market` M6 各钉一侧）。规格卡的意图（离线兜底目录）与「不引入会失效的嵌入路径」同时满足，无第二分支 |
| C2 | 「插件**注册面板 tab**」＋框架验证「注册面板 tab + 流式渲染…」 | `data-tab` 计数被门禁两处硬断言，且本 change 已把它由 4 改为 5；插件**不可能**再动态增加顶级 tab（会破坏已批准的门禁与区域裁剪前提） | 插件的「面板 tab」= **插件 tab 内的面板视图**：插件在 `manifest.panel = true` 时提供一份面板内视图，宿主把它的 iframe 挂满 `#tab-plugins` 的内容矩形，并提供「返回」回到管理列表。**不新增、不改名任何顶级 tab**（T72、T91、T92） |
| C3 | 清单 doubts-3：沙箱 iframe 若在 WebView2 受限，降级为「blob/内联注入」 | 全仓无 iframe/动态加载先例，无法离线证伪 | 计划**只保留一条路径**：`sandbox="allow-scripts"`（**不设 `allow-same-origin`**）＋ `src="/plugin-asset/<id>/<entry>"`（同源 URL 子资源加载不受源限制）＋ 宿主提供的 `/plugin-asset/@host/bridge.js`。该路径由 harness（真实浏览器引擎、真实构建产物）＋ 真实 exe 门禁验证；**若实测受限，本 change 判定为 blocked（记录实测结论）而不是当场切换分支**——降级方案不写进任务（design.md §13 C3、§11） |
| C4 | 清单 Q9/Q14：「一个或多个矩形（可圆角）」 | 区域裁剪只能表达矩形并集；一个 entry HTML 无法分成多块 | 浮层面按声明矩形的**顺序编号**逐块渲染：第 i 个矩形 → 第 i 个同源 sandbox iframe，`init` 消息带 `{surface:"surface", rectIndex, rectCount, pluginId, permissions}`；示例插件只声明 1 个矩形，多矩形路径由 Go 纯函数单测（T53、T55）与 harness 假桥接（T89、T112）覆盖 |

另有两处清单未规定的**取值裁定**（非仓库事实冲突，均收敛为唯一路径）：

- **插件气泡与插件视图的关系**：`bubble.say` 直接走既有 `showBubble` 路径（既有语义：弹气泡会收起面板与浮层，`app_pomodoro.go:1069-1071`），因此插件面板视图会随面板收起而隐藏、插件浮层面被清除。不做特例分支（T51、T62）。
- **插件浮层面与提问气泡**：`OpenPluginSurface` 在存在未回答提问气泡时**拒绝**并返回明确错误（与 `OpenLive` 同构，`app_llm.go:1033-1053`），不改任何状态（T65）。

## 4. 上游协议遵循说明（上游协议引用 → 本计划如何遵守）

下列协议**全部来自规格卡/澄清清单声明的可定位出处**（路径或明确协议名），每条均已实读原始文件，未引入规格卡之外的协议标准；未声明任何上述清单之外的协议标准。

| 协议引用（可定位出处） | 本计划的遵守方式 |
|---|---|
| `README.md:475-567` §10(c)(d)：窗口永不 resize，只改区域；**区域必须等于「页面真正绘制的东西」** | 插件浮层面**只**扩展 `applyWindowGeometry` 的 region switch，不新增任何 `SetBounds`/尺寸变更；矩形由 Go 纯函数算出后**同时**用于区域与前端摆放（T53、T58、T89） |
| `README.md:568-591` §11：气泡三种形态、一块区域、三态互斥 | 插件浮层面成为同一槽位的**第四态**，`OpenPluginSurface`/`SetPanelOpen`/`showBubble`/`OpenLive` 四个入口互为清理（T59–T63）；不新开窗口、不新开槽位 |
| `docs/reminder-pomodoro.md:487-512` §8.2 区域裁剪与「提问气泡不可被顶掉」 | 插件浮层面在未回答提问气泡存在时**拒绝打开**且不改状态（T65），文案沿用既有 `questionOwnsTheSlot` 的说明风格；插件气泡一律走既有 `showBubble`（T51、T62） |
| `docs/llm-chat.md:203-222` §8.0：Wails v2 无多窗口 API、类名 `wailsWindow` 冲突、只有一个槽位 | 全程**不创建第二个窗口/独立程序**；市场与插件界面都长在既有窗口内（T72、T83、T91）；市场页 `index.html` 只是市场仓里的静态文件（`plugin-market` M7）。**【已调整】** 后半段（「不新起程序」）**本前提由 `openspec/changes/plugin-market-app-and-chat-plugin` 推翻**：该 change 另起了 `PetPCPluginMarket.exe` 与既有的 `PetPCVoiceStudio.exe` 同级，因为面板 380×520 装不下插件管理这个工作面（权限行被右边界切断、地址框只剩半截、两个列表争抢一条滚动条）。注意 §8.0 的三条理由**本身仍然成立**——它们说的是「同一个进程里的第二个窗口」，而独立程序是**独立的进程、独立的 module、独立的 `wails.json`**，不撞类名、不抢槽位。**保留前半段**：「插件宿主必须在桌宠进程内」至今成立，所以插件浮层面仍然复用窗口裁剪，市场程序只通过共享文件与桌宠协作 |
| `docs/voice-studio.md` / `tools/voice-studio/wails.json`：同仓「第二个 Wails 程序」的边界先例 | 本 change **不**照此另起程序：插件宿主必须在桌宠进程内，因为插件浮层面要复用窗口裁剪（C1/C4 的收敛理由） |
| `.dev/check-frontend.cjs` 既有工程协议：JS 引用的 id 必须存在于 HTML、前端调用的绑定必须被 Go 导出、订阅的事件必须有 Go 发出者（`:137` 只认 `emitEvent("字面量")`）、新增 CSS 段必须挂段标记且无 `box-shadow/filter/text-shadow/transform`、设置页控件白名单（7 个 `.sgroup`）、共享滚动条规则（`:1203-1207` 逐条 `-webkit-scrollbar` 规则的选择器数 `>= 4`） | 「门禁同步」写成显式任务（T100–T107b）：`goFiles` 增 `app_plugins.go`（新增事件钉死为确定名 `pet:plugin-event`、双引号字面量发出）、`newSections` 增两段插件标记、两处 tab 计数 4→5 且断言 `#tab-plugins`、`#tab-plugins` 加进 `:1186` 的 `scrollbar-width` 组与全部 5 条 `-webkit-scrollbar*` 规则（含 `-corner`；`:1206` 阈值保持 `>= 4`）、id/绑定/**事件订阅**三处扫描均纳入两个新组件模块（T106，须排在 T100 的 `goFiles` 增项之后）、`layoutReturning` 增两个浮层面绑定（T107）、新增 `models.ts` 的 `Layout.pluginSurface` 断言（T107b，T120 点名的 PASS 行）；**设置页白名单与「恰好 7 个 `.sgroup`」逐字不改**（市场地址输入框放在插件 tab，T72、T98） |
| `.dev/run-frontend-harness.cjs` 协议：真实构建产物 + 假 Wails 桥接；`methods` 白名单缺方法会让 `wire()` 中途抛错 | T108：`methods` 增本轮全部新绑定（含 `PluginsView`/`MarketView`/`SaveMarketURL` 等十项）；T109–T112：四条新增判据（插件 tab 行为、权限清单、安装状态流转、iframe 按 layout 矩形摆放）；T84b/T88b 两条桥接与事件判据由 T121 的 PASS 清单点名 |
| `frontend/vitest.config.js` + `frontend/src/test-harness.js`（jsdom 单测入口与假桥接白名单） | T113（白名单）＋ T114（`frontend/src/plugins.test.js` 的四个行为用例）；判据为 vitest 全绿 |
| `.dev/check-all.ps1` 门禁顺序与 `.dev/verify-region.ps1`/`.dev/verify-live.ps1` 真实窗口像素门禁 | T116–T126 各为独立任务；像素门禁在 `wails build -s`（T122）之后运行，无交互桌面时按既有约定报告「无交互桌面」而非代码失败（T123、T124）；浮层面的像素判据写成**独立脚本** `.dev/verify-plugin-surface.ps1`（T125），既有两个像素门禁一字不改 |
| `.dev/env.ps1`、`tools/build-all.ps1`、`wails.json`、`README.md:12-24`：任何 `go`/`npm`/`wails` 命令前须 dot-source 工具链；`npm run build` → `wails build -s` | tasks.md 的全部命令判据按此写（`go vet ./...`、`go test ./... -count=1`、`npm run build`、`wails build -s`、`pwsh -File .dev\check-all.ps1`），不发明新命令 |
| `go.mod:1-10`：Go 1.22 / wails v2.11.0 | `internal/plugins` 只用标准库（`archive/zip`、`crypto/sha256`、`encoding/json`、`net/http`、`embed`），**不新增任何第三方依赖**，T118 判据含 `go.mod`/`go.sum` 未变 |
| `.gitignore:1-35`：构建产物与 `.dev/*` 白名单的既有约定 | T5 只**追加** `market/` 一行，T5b 再追加 `!.dev/verify-plugin-surface.ps1`（把第 4 个门禁脚本按 `.gitignore:11-16` 的既有约定纳入版本库），T6 逐条核对**既有全部 10 条忽略 pattern 与 3 条既有白名单**仍生效 |
| `docs/genshin-tts.md:479`、`:688-689`＋`_archive/tts-realtime/tools/tts-p0/fetch.ps1:64-72`：`raw.githubusercontent.com` 不通，用 `gh-proxy.com` 前缀绕行 | 市场基址默认值取该镜像前缀（T7），且基址可配置（T72、T98）；不引入第二个镜像服务、不做多源聚合（规格卡范围外第 9 项） |

## 5. 影响面（文件级）

**新增（主仓）**：`internal/plugins/manifest.go`、`manager.go`、`market.go`、`capability.go`（＋各自 `_test.go`）、`internal/plugins/sample/embed.go`、`internal/plugins/sample/index.json`、`internal/plugins/sample/com.shixiansi.plugin-chat-1.0.0.zip`（由 `plugin-market` M5/M6 产出并与本仓副本同字节）、`app_plugins.go`、`app_plugins_test.go`、`app_plugin_surface.go`、`app_plugin_surface_test.go`、`frontend/src/plugins.js`、`frontend/src/plugin-host.js`、`frontend/src/plugins.test.js`。

**修改（主仓）**：`app.go`（`Layout.PluginSurface`、region 分支、`SetPanelOpen` 清浮层面）、`app_llm.go`（`OpenLive` 清浮层面，仅一行）、`app_pomodoro.go`（`showBubble` 清浮层面，仅一行）、`main.go`（assetHandler 路由）、`internal/store/store.go`（`Config.MarketBaseURL` + `Normalise`）、`.gitignore`（`market/` 与 `!.dev/verify-plugin-surface.ps1` 两行追加）、`frontend/index.html`、`frontend/src/main.js`、`frontend/src/style.css`、`frontend/wailsjs/go/main/App.js`、`App.d.ts`、`models.ts`（重新生成）、`.dev/check-frontend.cjs`、`.dev/run-frontend-harness.cjs`、`frontend/src/test-harness.js`、`README.md`。

**新增（工程断言）**：`.dev/verify-plugin-surface.ps1`（T125）。

**不动（判据守住）**：`frontend/src/chat.js`、`frontend/src/live.js`、`app_llm.go` 的会话状态机与转写、`frontend/index.html` 的 `#tab-chat` 体与 4 个既有 tab、`.dev/verify-region.ps1`、`.dev/verify-live.ps1`、`.dev/check-all.ps1`、`.dev/layout-report.cjs`、`docs/*`。

**构建产物（`.gitignore` 忽略，不提交）**：`frontend/dist/`、`build/bin/PetPC.exe`。

## 6. 风险与缓解（承自澄清清单，逐条落到任务）

| 风险 | 缓解（任务映射） |
|---|---|
| 浮层面的像素与 Go 计算的矩形不一致 → 桌面上留下未绘制像素/色带（本仓历史上出现过宠物列顶部淡蓝玻璃条） | T53（纯函数唯一算一次）、T58（区域取自同一集合）、T89（前端只按矩形摆放）、T112（harness 断言 iframe 位置 == layout 矩形）、T123/T124/T125（真实窗口像素回归） |
| 第 5 个 tab 撞上两处 `=== 4` 断言、`goFiles`/`newSections` 硬编码清单，漏改直接 FAIL | T100–T107b 为必做任务（其中 T106 的 `[前置]` 含 T100：`plugin-host.js` 的订阅扫描必须先有 `goFiles` 里的 `app_plugins.go` 才能通过 dangling 判定），T120 的判据逐条点名要出现在 PASS 列表，且设置页白名单断言不减 |
| 沙箱 iframe 在 WebView2 的可行性未在本仓验证过（全仓无 iframe 先例） | T83（属性）、T84（URL）、T85–T88（桥接）、T109–T112（真实浏览器引擎 + 真实构建产物）、T125（真实 exe）；**实测受限即 blocked，不静默切换分支**（C3） |
| 私有主仓在用户手动 push 前无法被他人克隆，交付物只存在于本地 | T1（判据含「无远端跟踪引用」）、T115（README 写清 push 命令与分支名）；本 change 不含任何 push |
| 插件能力越权（读待办、联网、显示浮层面） | T22（授权持久化）、T35（未授权拒绝且无副作用）、T45/T46（域名白名单 + 仅 http/https）、T42/T44（私有存储与 key 白名单）、T83（不透明源沙箱） |
| zip 解压穿越（`../`、绝对路径）写坏用户磁盘 | T18（解压前逐条校验条目路径并拒绝）、T25（卸载只删 `<dataDir>/plugins/<id>` 与 `plugin-data/<id>`） |

## 7. 范围外（承规格卡 10 项，逐条不做）

不做评分/评论/下载量/自动更新/版本回滚（只列表/详情/安装/卸载/启停/授权）；不做插件签名与非对称校验（只 sha256 完整性）；不做任意曲线外形/逐像素 alpha 掩码（只矩形并集可圆角）；**不做独立「插件市场窗口」，不新起程序**（Wails v2 无多窗口 API）。**【已调整】** 「不新起程序」这一条**本前提由 `openspec/changes/plugin-market-app-and-chat-plugin` 推翻**：插件管理最终就是一个独立程序（`PetPCPluginMarket.exe`），理由是面板 380×520 装不下它；「不做独立**窗口**」仍然成立——没有在桌宠进程里开第二个窗口。「不改动、不迁移、不删除既有 `#tab-chat`（`frontend/src/chat.js` 与 `app_llm.go` 会话状态机原样保留）」这一条也**由同一个 change 推翻**：`#tab-chat` 的**内容**改由对话插件渲染（宿主只留 `#chat-slot` 挂载点），`chat.js` 与宿主对话引擎整族被删除，配置迁给插件。其余各条不变：不做 exe 的 Release 附件分发；不开 GitHub Pages；**不由 agent 执行线上 push**；不做多市场/私有源聚合；不做插件间依赖解析。本 change 亦不改提醒调度规则、不改窗口尺寸机制、不引入阴影/滤镜/transform。
