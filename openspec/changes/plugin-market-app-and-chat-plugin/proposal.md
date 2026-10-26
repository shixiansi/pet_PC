# proposal.md — plugin-market-app-and-chat-plugin

> 本 change 把三件事一次做完：① 插件管理面从桌宠面板搬进独立程序 `tools/plugin-market/`（`PetPCPluginMarket.exe`）；
> ② 桌宠面板删除「插件」tab，只在设置页留一个入口按钮；③ 对话与 AI 配置整体插件化（新增 `plugin.config` 权限与
> `config.get/set`、manifest 配置声明与配置化域名白名单、浮层面矩形上限 4→8、对话插件升版 1.1.0、宿主一次性幂等迁移）。
>
> 唯一约束来源是规格卡 `sc_3_mv0pt51q`（已批准）与澄清清单 `wiki/queries/checklists/petpc-ai-mv0ppr7n.md`（Q1–Q10）。
> 本文件只写「为什么要做、改变什么、影响哪些文件、遵守哪些上游协议」；技术细节在 `design.md`，执行清单在 `tasks.md`。

## 1. 变更说明（做什么、为什么）

### 1.1 现状（本仓实读事实，非推测）

| 事实 | 出处 |
|---|---|
| 面板有 5 个 tab：待办 / 对话 / 记录 / **插件** / 设置 | `frontend/index.html:65-69` |
| 插件 tab 体含已装列表、市场地址框、市场列表、面板视图容器 | `frontend/index.html:158-185` |
| 插件 tab 由 `frontend/src/plugins.js`（585 行）驱动，`main.js` 接线于 `els`/`switchTab`/`wire()` | `frontend/src/main.js:42,45-57,1316,1564-1602` |
| 对话 tab 由宿主前端 `chat.js`（488 行）+ `live.js`（465 行）渲染，浮层几何由 `computeLiveOverlay` 算出 | `frontend/src/chat.js`、`frontend/src/live.js`、`app_live.go:62-330` |
| AI 配置 = 设置页 25 个 `llm-*` 控件 + `Config` 13 个 `llm*` 字段 + `llm-key.bin`（DPAPI）+ `llm-history.json`/`llm-usage.json` | `frontend/index.html:390-529`、`internal/store/store.go:135-179`、`.dev/check-frontend.cjs:346-350`、`app_llm.go:98-99`、`internal/store/llm.go:347-348` |
| 权限固定 9 项；`net.fetch` 只认 manifest 的静态 `netHosts`；`HostAllowed` 精确匹配 | `internal/plugins/capability.go:8-125`、`app_plugins.go:636-662` |
| 浮层面矩形上限 4 | `internal/plugins/manifest.go:73`（`maxSurfaceRects`） |
| 第二个 Wails 程序的成熟形态：嵌套 module + `replace PetPC => ../..`、自有 `wails.json`/前端、自有配置文件、构建后复制到 `build/bin` | `tools/voice-studio/go.mod:1-42`、`wails.json`、`frontend/vite.config.js`、`studio.go:100-102`、`tools/build-all.ps1:59-70` |
| 桌宠启动独立程序的既有写法：三处查找顺序 + detached 启动、不等待不关闭 | `app_voice_studio.go:91-160` |

### 1.2 推翻的旧前提【已调整】

上一链 `openspec/changes/plugin-system/design.md:11-12` 把 **「Wails v2 无多窗口 API；类名 `wailsWindow` 唯一」
→「不创建第二个窗口、不新起程序；市场与插件界面都长在既有窗口内」** 写成了硬约束。同一前提也出现在
`openspec/changes/plugin-system/proposal.md:59`（「全程**不创建第二个窗口/独立程序**」）与 `:95`（「**不做独立「插件市场窗口」，不新起程序**」）。

本 change **正式推翻**该前提，理由：

1. 面板矩形固定 380×520、窗口永不 resize，这三条约束是关于**桌宠本体**的裁剪前提，而不是「本仓只能有一个程序」。
   本仓已有反例与结论：配音工坊正是因为「面板不能长大」而拆成独立程序（`docs/voice-studio.md:9`、`README.md:276/353`）。
2. 用户实拍截图实证：权限勾选行被右边界切断、市场地址框只剩半截、对话框与两个列表争抢同一块滚动区——
   380×520 内不可能同时满足「密度与可点击区域匹配」。
3. 被推翻的只是**管理层**的形态；「Wails v2 无多窗口 API」这条**技术事实**仍然成立，因此插件**本体的运行时 UI**
   依旧只长在桌宠的裁剪矩形内（面板槽位或浮层面），不进独立程序——两条约束处于不同层次，本 change 同时遵守。

同步动作（只读定位结论 → 写进 tasks）：`plugin-system/design.md:11-12`、`plugin-system/proposal.md:59`、`:95`
三处在原地标注【已调整】并指向本 change；`docs/llm-chat.md:203-222` §8.0 的「为什么是面板 tab，而不是独立窗口」
需补一段现状说明（对话仍是面板内的一个槽位，但其**内容**自本 change 起由插件渲染，AI 配置不再在设置页）。
`README.md:276`、`:353`、`:480` 的既有表述（「想要更大的界面只能像配音工坊那样另起一个独立程序」）与新形态一致，
**不改**。

### 1.3 三块交付

**【一】独立插件市场程序 `tools/plugin-market/`（`PetPCPluginMarket.exe`）**

- 完全照搬 `tools/voice-studio` 的成熟形态：嵌套 go module（`module PetPC/tools/plugin-market` + `replace PetPC => ../..`）、
  自有 `wails.json`、自有 `frontend/`（Vite 4.5.5、`base './'`、`minify:false`）、自有 wailsjs 绑定产物。
- 直接 `import` 既有 `PetPC/internal/plugins`（manifest / manager / market）与 `PetPC/internal/store`（`DefaultDir`），
  **零复制**：安装/校验/授权/启停/卸载/配置写入全部走同一份代码，不存在两套实现。
- 普通带框可缩放窗口（初始 980×700，Frameless=false、AlwaysOnTop=false、不透明背景），
  左侧列表（已装 + 市场）、右侧详情/权限勾选/配置编辑/安装按钮，顶部市场地址框 + 刷新。
- 数据目录与桌宠完全共享（`%AppData%\PetPC`，支持 `PETPC_DATA_DIR`）；**自己的偏好写自己的文件**
  `<dataDir>/plugin-market.json`（照 `tools/voice-studio/studio.go:100-102` 用 `voice-studio.json` 的先例），**绝不写桌宠 `config.json`**。
- 由 `tools/build-all.ps1` 构建并把 exe 复制到 `build/bin`，与 `PetPCVoiceStudio.exe` 同级（桌宠侧按同目录优先查找）。

**【二】桌宠侧收口**

- `frontend/index.html` 删除整个插件 tab（`data-tab="plugins"` 按钮 + `#tab-plugins` 整块），面板 tab 由 5 个回到 4 个。
- 删除 `frontend/src/plugins.js`；`main.js` 移除 `els.tabPlugins`、插件列表 els、`PluginsTab` 实例与市场地址接线。
- 设置页新增「打开插件市场」入口按钮（`id="open-plugin-market"`，放在「数据与退出」分组内，不新增分组），
  点击调用新绑定 `OpenPluginMarket()`，以独立进程方式启动 exe（查找顺序照 `studioCandidates`：
  exe 同目录 → `exe/../tools/plugin-market/build/bin` → `cwd/tools/plugin-market/build/bin`），桌宠**不等待、不关闭**它。
- 插件本体的运行时 UI 仍只长在桌宠的裁剪矩形内（面板槽位或浮层面），不受本次搬迁影响。

**【三】对话与 AI 配置插件化**

- 新增第 10 项权限 `plugin.config`（标签「保存你自己的插件配置」）与 `config.get` / `config.set` 能力。
- manifest 新增 `config` 声明（`{key,label,type,default,secret,hostWhitelist}`）与 `panelSlot`（取值 `""`/`"chat"`）字段，含完整校验。
- 插件配置持久化到插件私有目录 `plugin-data/<id>/config.json`（原子写），`secret=true` 的项经注入的 `SecretKeeper`
  复用 `internal/desktop.SecretStore`（DPAPI）按插件隔离加密，**明文绝不落盘、绝不过桥**。
- `net.fetch` 的域名校验改为「静态 `netHosts` ∪ 配置项（`hostWhitelist=true`）当前值解析出的主机名」，
  仍限 http/https、仍精确匹配（沿用 `HostAllowed` 语义，不做后缀通配）。
- 浮层面矩形上限 `maxSurfaceRects` 4→8，容纳对话浮层（卡片 + 输入条 + 3 个思考泡泡）。
- 对话插件升版 `1.0.0 → 1.1.0`：manifest 声明自己的 AI 配置项（端点/模型/温度/上限/超时/上下文/人设/每日上限/密钥）
  与 `panelSlot: "chat"`，渲染「对话」tab 的面板槽位与宠物旁浮层面；两处市场目录与内置样例源同步真实 sha256。
- 宿主一次性幂等迁移：把 13 个 `llm*` 值、`llm-history.json`、`llm-usage.json` 与 `llm-key.bin` 内容搬进对话插件私有存储，
  原文件改名 `.bak` 保留；第二次启动不重复搬动；任一环节失败则原文件不动并报错。
- `internal/llm` 传输层与 `llm.chat` 能力**保留不动**：SSE 解码、重试、错误翻译、50ms delta 合并、用量与预算统计一律不重写；
  变的只是参数的**来源**（从宿主 `Config` 改为**调用方插件自己的配置**）。

## 2. 上游协议遵循说明（上游协议引用 → 本计划如何遵守）

本节逐条列出**规格卡 / 澄清清单声明的**工程协议引用（都能定位到本仓文件或明确协议名），并说明计划如何遵守。
每条引用均已实读原始文件；**未引入规格卡之外的任何协议标准**（不引用外部规范、不引用二手摘要）。

| 协议引用（可定位出处） | 本计划的遵守方式 |
|---|---|
| 规格卡 body「三条硬约束」：① 窗口永不 resize，只改矩形裁剪区域；② 面板矩形固定 380×520；③ Wails v2 无多窗口 API。出处 `app.go:510-531/690-750`、`internal/store/store.go:225-235`、`internal/desktop/desktop_windows.go:21`、`docs/llm-chat.md:203-222` | ① 插件浮层面与对话浮层**只**走 `applyWindowGeometry` 的 region 分支，不新增任何尺寸/位置变更（T53、T55、T56）；② 面板槽位把 `#tab-chat` 的既有矩形交给插件 iframe，前端不自行推导尺寸（T49、T51、T83）；③ 市场程序是**另一个进程**（不是第二个窗口），桌宠自身仍是唯一窗口（T4、T85、T86）；插件本体运行时 UI 不进市场程序（T49、T51、T55） |
| 规格卡/澄清 Q2/Q10 点名 `tools/voice-studio` 作为模板协议：`tools/voice-studio/go.mod:1-42`（`module …` + `replace PetPC => ../..`）、`tools/voice-studio/wails.json:1-16`、`tools/voice-studio/frontend/vite.config.js:1-19`（Vite 4.5.5、`base:'./'`、`minify:false`）、`tools/voice-studio/main.go:42-68`（普通带框可缩放窗口） | 市场程序**逐项**照此形态（T1、T2、T3、T4、T15）；不发明新的构建/窗口写法 |
| `app_voice_studio.go:91-118`（`StudioPath`/`studioCandidates` 三处查找顺序）、`:130-160`（`OpenVoiceStudio` detached 启动、不等待不关闭、记录 `launched`） | 新绑定 `OpenPluginMarket` / `PluginMarketPath` 沿用同一查找顺序与启动语义（T85、T86） |
| `internal/voice/user.go:272-309`（`reloadIfChanged`：跨进程重读）+ `internal/plugins/manager.go:76-96`（「Nothing is cached in memory」） | 跨进程可见性写成**可核对判据**而非新机制：`plugins.Manager` 本身无内存缓存，每次读盘即最新；桌宠侧与管理侧各加一条「另一进程改写 `plugins.json` 后本进程读到新值」的单测（T14、T48） |
| `internal/desktop/secret.go:33-158`（`SecretStore`：`NewSecretStore(dir,name)`、`Set` 原子写、`Get`/`Has`/`Source`/`Clear`）+ `internal/desktop/crypto_windows.go`（DPAPI） | 插件密钥按插件隔离：每个 secret 项一个 `plugin-data/<id>/secret-<key>.bin`，由**注入的 `SecretKeeper` 接口**复用 `SecretStore`；`internal/plugins` 仍不 import `internal/desktop`（保持其文件头声明的依赖边界，T34、T38、T41） |
| `internal/plugins/capability.go:114-125`（`HostAllowed` 精确匹配、绝不做后缀通配）+ `internal/plugins/manifest.go:177-188`（`net.fetch` 必须有 `netHosts`，否则拒绝「不可执行的允许清单」） | 配置化白名单是**追加**来源，静态 `netHosts` 的强制配对规则**逐字不改**；仍精确匹配、仍限 http/https（T44、T45、T46） |
| `internal/plugins/manifest.go:69-77/92-156/225-244`（校验表与常量：`maxSurfaceRects`、正则、逐字段报错） | 新增 `config`/`panelSlot` 校验走同一张表与同一报错风格；`maxSurfaceRects` 4→8 只改常量与其单测边界（T24–T33） |
| `.dev/check-frontend.cjs` 既有工程协议：`scannedJs` 三处扫描（id / `App.*` / `EventsOn`）、`newSections` 段标记与禁 `box-shadow/filter/text-shadow/transform`、`sgroup` 恰 7 个、设置页 id 白名单全等、tab 计数与 `#tab-plugins` 断言、共享滚动条规则、`layoutReturning` 必须是 `main.Layout`、`models.ts` 必须有 `Layout.pluginSurface` | 「门禁同步」写成逐条独立任务（T89、**T89b**、T90–T102）：tab 计数 5→4、删 `#tab-plugins`/`tabPlugins` 断言、`scannedJs` 去掉三个被删模块并新增市场程序前端、`newSections` 删插件 tab 段保留 plugin surface 段、滚动条规则组去 `#tab-plugins`、设置页白名单删 25 个 `llm-*` 增 `open-plugin-market`、分组 7→5、`goFiles` 增三个新 Go 文件并**删 `app_live.go`**；**T89b 专门承接「既有断言/扫描清单依赖被删模块或被删事件」的那批**（`liveJs`/`liveParts`/`chatJs`/`pet:chat-*`/`pet:live`/`ChatMessageView.proactive`/`OpenLive` 守卫/`computeLiveOverlay`/`petHidden`/`goFiles` 里的 `app_live.go` 共十处），否则 T89 删掉 `readFileSync` 行后这些断言会因变量不存在让整个脚本抛错 |
| `.dev/run-frontend-harness.cjs` 协议：跑**真实构建产物** + 假 Wails 桥接；`methods` 白名单缺方法会让 `wire()` 中途抛错、失败点漂移 | T103–T112：删第 18 节插件 tab 用例、删宿主对话 UI 用例、第 19 节按 5 块矩形适配、假绑定白名单增删、假 `Layout` 补 `pluginSurface`、新增入口按钮用例 |
| `frontend/src/test-harness.js:47-72`（假桥接方法白名单）与 `frontend/vitest.config.js:13-20`（jsdom 单测入口） | T113–T118：白名单同步、`baseState`/`layout()` 辅助同步、`plugins.test.js` 删除并新增设置页入口按钮与槽位渲染用例，`npx vitest run` 全绿 |
| `.dev/check-all.ps1:53-75`（门禁顺序：vet → test → 前端构建 → 静态门禁 → harness → wails build → 两个真实窗口像素门禁）、`.dev/env.ps1`（任何 `go`/`npm`/`wails` 前须 dot-source）、`README.md:12-24`（`npm run build` → `wails build -s`） | tasks.md 的全部命令判据按此写；新增市场程序 Go 测试门与第二个前端构建门（T119、T120）；真实窗口门禁在构建之后运行，无交互桌面时按既有约定报告而非代码失败（T121–T124） |
| `market/tools/package.ps1:1-256`（打包 → `Get-FileHash` → 回写 `index.json` → 复制到内置样例源；条目权限取自包内 manifest）+ `internal/plugins/sample/embed.go:22-32`（显式 `//go:embed` 清单） | 对话插件升版后**必须**重跑打包并同步两处目录与真实 sha256；`embed.go` 的显式文件名清单随之替换 1.1.0（T69、T70、T71） |
| `.gitignore:1-44` 既有约定（`.dev/*` + 四条白名单、`build/bin/`、`frontend/dist/`、`node_modules/`、`market/`） | 只**追加**市场程序构建产物所需的规则（口径与 voice-studio 现状对齐），既有规则一条不改（T20） |
| `go.mod:1-10` / `tools/voice-studio/go.mod:1-42`：Go 1.22、wails v2.11.0，`internal/plugins` 只用标准库 | 市场程序是**嵌套 module**，`internal/plugins` 不新增第三方依赖；其新增的 `SecretKeeper` 是接口而非 import（T1、T34） |
| `docs/genshin-tts.md:479,688-689`：`raw.githubusercontent.com` 不通，用 `gh-proxy.com` 前缀绕行（`internal/plugins/market.go:25` 已有默认基址常量） | 市场程序的地址框默认值沿用 `plugins.DefaultMarketBaseURL`；首次运行从桌宠 `config.json` 的 `marketBaseUrl`（若用户改过）或该常量播种到自己的配置文件（T5、T10） |
| `tools/voice-studio/studio.go:17-45,100-102`「两个程序共享数据目录，但配置分离」的先例 + 澄清 Q3/Q4 的「绝不写桌宠 config.json」 | 市场程序的偏好只写 `<dataDir>/plugin-market.json`；迁移只读桌宠 `config.json` 的 13 个 `llm*` 值，**不改** `config.json` 一个字节（T5、T59、T64） |
| 规格卡 testing 第 8 条「沿用上一链已建立的 geometry_gate 与 sandbox_gate」：区域 = 「宠物矩形 ∪ 插件声明矩形（夹回后）」、iframe 位置逐字等于 Layout 下发矩形、沙箱恰为 `allow-scripts` | T53、T105、T106、T107 与 `app_plugin_surface_test.go` 的纯函数判据一并守住；`.dev/verify-live.ps1`、`.dev/verify-plugin-surface.ps1` 的真实窗口判据按 T122、T123 适配 |

## 3. 影响面（文件级）

**新增（主仓）**
`openspec/changes/plugin-market-app-and-chat-plugin/{proposal,design,tasks}.md`；
`tools/plugin-market/{go.mod,go.sum,main.go,market.go,config.go,config_test.go,market_test.go,wails.json}`；
`tools/plugin-market/frontend/{package.json,vite.config.js,vitest.config.js,index.html,src/main.js,src/style.css,src/main.test.js}` 与生成的
`tools/plugin-market/frontend/wailsjs/**`；
`internal/plugins/{config.go,config_test.go,migrate.go,migrate_test.go}`；
`app_plugin_migrate.go`、`app_plugin_migrate_test.go`、`app_chat_slot.go`（对话槽位解析）、`app_chat_slot_test.go`、
`app_plugin_market.go`（`OpenPluginMarket`/`PluginMarketPath`/`PluginMarketCandidates`）、`app_plugin_market_test.go`；
`frontend/src/settings-market.test.js`；`.dev/verify-plugin-market.ps1`（新建）；`docs/plugin-market.md`（市场程序使用与前后对比结论）。

**修改（主仓）**
`internal/plugins/{capability.go,manifest.go,manager.go,manifest_test.go,manager_test.go}`；
`internal/store/llm.go`（新增两个导出纯函数用于按插件计数与滚动，既有行为不改）；
`app_plugins.go`（`config.get/set`、配置化白名单、`PluginsInstalled()`；删除七个插件管理绑定与 `installedView`/`marketView`/`marketEntry`，见 §5.2）、`app_plugins_test.go`；
`app_plugin_surface.go`（浮层盒子映射；删 `wasLive` 分支）、`app_voice_studio.go` 或新增同族文件（`OpenPluginMarket`/`PluginMarketCandidate`；`app.go` 的启动序列加 `PETPC_OPEN_PLUGIN_MARKET` 门禁钩子，T86）；
`app_llm.go`（`pluginLLMChat` 参数来源改为插件配置；删宿主对话 UI 绑定与 proactive 家族）、
`app_live.go`（删除：浮层几何与其 UI 支撑、proactive 家族）、`app.go`、`app_pomodoro.go`、`app_reminder.go`、`app_hotkey.go`；
`frontend/index.html`、`frontend/src/main.js`、`frontend/src/plugin-host.js`、`frontend/src/style.css`；
`frontend/wailsjs/go/main/{App.js,App.d.ts}`、`frontend/wailsjs/go/models.ts`（重新生成）；
`tools/build-all.ps1`、`market/tools/package.ps1`（重跑产出）、`market/plugins/com.shixiansi.plugin-chat/**`（升版与功能对等）、
`market/index.json`、`internal/plugins/sample/{index.json,embed.go}` 与内置样例 zip；
`.dev/{check-frontend.cjs,run-frontend-harness.cjs,check-all.ps1,verify-live.ps1,verify-plugin-surface.ps1}`；
`frontend/src/test-harness.js`、`README.md`、`docs/llm-chat.md`、`openspec/changes/plugin-system/{design.md,proposal.md}`（旧前提标注）。

**删除（主仓）**
`frontend/src/plugins.js`、`frontend/src/plugins.test.js`、`frontend/src/chat.js`、`frontend/src/live.js`、
`app_live.go`、`app_live_test.go`、`app_proactive_test.go`。

**不动（判据守住）**
`internal/llm/**`（SSE / 重试 / 错误翻译 / delta 合并 / 理由字段）、`llm.chat` 能力的**方法名与流式帧协议**（`llm.delta`/`llm.done`/`llm.error`）、
`internal/desktop/secret.go` 与 DPAPI 实现、`internal/plugins/market.go` 的市场抓取与兜底源、`plugin-host.js` 的 `ready→init` 协议与沙箱属性、
`frontend/src/plugin-host.js` 的 `postMessage` 路由、`.dev/verify-region.ps1`、`.dev/env.ps1`、`.gitignore` 既有规则、`tools/voice-studio/**`（只当模板）。

## 4. 范围外（与规格卡逐条一致，不放宽）

1) 不重写宿主传输层：不删不改 `internal/llm/**` 与 `llm.chat` 的流式帧协议；不在插件内实现 SSE 解码/重试/delta 合并/预算统计。
2) 不做市场的评分、评论、下载量、自动更新与版本回滚。
3) 不做插件签名体系（仍只有 sha256 完整性 + 安装时逐项授权 + 沙箱隔离）。
4) 不做任意曲线外形（裁剪仍只支持矩形并集，可圆角）。
5) 不做第二个桌宠窗口；插件本体的运行时界面不进市场程序。
6) 不做多市场/私有源聚合，也不做插件间依赖解析。
7) 不由 agent 执行线上 `git push`（两仓推送仍由用户手动执行）。
8) 不把 exe 作为 GitHub Release 附件分发。
9) 不给插件后台常驻进程或 Go 动态库能力（插件仍是 manifest + 前端资源）。
10) 不动 `tools/voice-studio`（只当模板）。
11) 不做「连接测试」：市场程序**不** import `internal/llm`、不为填好的端点另建一条探测路径（违反规格卡 `impl_decisions` 第 2 条「只 import `internal/plugins` 与 `internal/store`」的口径，且宿主的 `TestLLMConnection` 本轮已随宿主对话 UI 移除）。端点与密钥是否正确由对话插件自己在面板槽位里以一次真实的 `llm.chat` 反馈——那正是用户真正要用的路径。

## 5. 已知后果（明确记录，避免读者误判为遗漏）

1. **对话必须有插件才可用**：对话 tab 的面板槽位与宠物旁浮层层都改由对话插件渲染，因此「对话插件未安装/未启用」时
   面板槽位显示明确提示、双击宠物给出气泡提示而不是打开空浮层（空浮层正是本仓反复修掉的「玻璃带」故障）。
   这是「把整个对话以及 AI 配置都迁成插件」的直接结果，端到端冒烟步骤与 README 都会写清。
2. **设置页 AI 配置段整体消失**：25 个 `llm-*` 控件（含「大模型对话」与「主动说话」两组）全部移除，
   `Config` 的 13 个 `llm*` 字段保留为**只读迁移源**（仍解析、仍规范化，但不再被请求路径读取、也无 UI 可改）。
   移除分两处落地、两处都必须有终态：门禁脚本的设置页 id 白名单（T92）与宿主前端对这些 id 的**读取与绑定**（T87f，
   删 25 个 els 条目、`wireLLMSettings()`/`llmSettingsPayload()`/`saveLLMSettings()`/`renderProactiveStatus()`
   与六处 `App.*` 调用）——只做前者的结果是 `renderSettings` 里 `els.llmEnabled === null` 抛错，
   静态门禁与 harness 都到不了全绿。
   随配置源一起失去入口的还有宿主的「主动说话」功能：它没有配置来源，且若改为读某个第三方插件的私有配置，
   就把宿主与一个插件 id 硬耦合，违反插件架构——因此它**整套删除**（`app_live.go` 的 proactive 家族随 T56 的整文件删除消失，
   `SpeakNow`/`speakProactive`/调度调用点/`proMu` 状态与 `app_proactive_test.go` 由 T87c 删除），
   两个取值照规格迁移进插件配置，插件日后可自行实现（`llm.chat` + `pet.bubble`）。对用户可见的结果：
   设置页不再有「主动说话」开关与「现在说点什么」，托盘与快捷键也无该入口。
3. **面板 tab 从 5 个回到 4 个**：插件管理不再占用桌宠界面；入口只有一个设置页按钮。
4. **宿主侧不再暴露插件管理绑定**：`PluginsView`/`MarketView`/`InstallPlugin`/`UninstallPlugin`/`SetPluginEnabled`/
   `GrantPluginPermissions`/`SaveMarketURL` 七个绑定从 `app_plugins.go` 删除（管理面只在 `PetPCPluginMarket.exe`，
   它直接调 `internal/plugins`）。宿主内仍保留「插件失效后它的浮层面不能继续占着窗口裁剪区域」这条不变量，
   改由几何计算时的有效性守卫承接（T87e），因此跨进程卸载/停用同样成立。
