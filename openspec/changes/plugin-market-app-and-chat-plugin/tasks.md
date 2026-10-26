# tasks.md — plugin-market-app-and-chat-plugin

> 每条任务是**单一、可独立核对**的终态：一个任务只断言一个可观察结果。RED/GREEN/REFACTOR、
> 「先失败再通过」属 D 阶段执行协议，此处不写；判据一律描述「实施完成后是什么样」。
> **粒度规则（本文件唯一口径）**：一个任务 = 一条规则 / 一个权限 / 一个接线点 / 一个校验项 / 一个门禁改动面 /
> 一条构建-验收结论。「拒绝非法输入」与「接受合法输入」属同一条规则的两面，共处一个任务；
> 两个独立规则（如「矩形映射」与「夹回窗口」、「history 搬移」与「usage 搬移」）一律拆开。
> `[前置]` 列出必须已完成的编号（**内容依赖**，不表示执行时序）；依赖顺序与**排期约束**见 design.md §10。
> 注意：T56→T57→T87b→T87c 是一段**连续批次**，必须整体排在 F/G/H 组与 I 组前段之后（该批次内根包在链尾 T87c
> 之前不可编译，而 F/G/H 组的判据要跑根包测试）；批次内只有 T87c 断言整包 `go build`/`go vet`/`go test`。
> 编号后缀（a/b/c）只表示同组内的独立条目。
> 命令判据中的 `go`/`npm`/`wails`/`pwsh` 一律先在 `.dev/env.ps1` dot-source 后的环境里执行
> （`pwsh -File .dev\check-all.ps1` 是汇总入口）。
> 本文件即后续 D 阶段的执行清单与交付报告镜像（条目标题与勾选状态可逐条核对）。
> 末尾 §N 是「澄清清单 / 规格卡任务项 ↔ 本计划任务」的双向映射表。

## A. 独立插件市场程序：骨架与绑定（T1–T20）

- [x] **T1 — `tools/plugin-market/go.mod` 是复用主仓包的嵌套 module**
  文件：`tools/plugin-market/go.mod`
  内容：`module PetPC/tools/plugin-market`、`go 1.22.0`、`toolchain go1.22.12`、`require (PetPC v0.0.0; github.com/wailsapp/wails/v2 v2.11.0)`、`replace PetPC => ../..`；形态与 `tools/voice-studio/go.mod:1-42` 逐项对应。
  完成判据：`grep -c '^module PetPC/tools/plugin-market$'` 为 1、`grep -c '^replace PetPC => \.\./\.\.$'` 为 1；在该目录下 `go list -m all` 输出含 `PetPC => ../../`。

- [x] **T2 — `tools/plugin-market/go.sum` 与配音工坊同字节**
  文件：`tools/plugin-market/go.sum`
  完成判据：`Compare-Object (Get-Content tools\plugin-market\go.sum) (Get-Content tools\voice-studio\go.sum)` 无差异输出（两程序依赖集完全相同）。

- [x] **T3 — `tools/plugin-market/wails.json` 声明自己的程序名**
  [前置] T1
  文件：`tools/plugin-market/wails.json`
  内容：`$schema`、`name`/`outputfilename` 均为 `PetPCPluginMarket`、`frontend:install`=`npm install`、`frontend:build`=`npm run build`、`frontend:dev:watcher`=`npm run dev`、`frontend:dev:serverUrl`=`auto`、`info.productName`=`PetPC 插件市场`。
  完成判据：`ConvertFrom-Json` 后 `.outputfilename -eq 'PetPCPluginMarket'` 且 `.info.productName -eq 'PetPC 插件市场'`；`tools/voice-studio/wails.json` 未出现在 diff 中。

- [x] **T4 — 市场程序窗口是普通带框可缩放窗口（980×700）**
  [前置] T1
  文件：`tools/plugin-market/main.go`
  内容：`//go:embed all:frontend/dist` + `wails.Run(&options.App{Title:"PetPC 插件市场", Width:980, Height:700, MinWidth:720, MinHeight:520, DisableResize:false, Frameless:false, AlwaysOnTop:false, BackgroundColour:&options.RGBA{R:24,G:26,B:32,A:255}, AssetServer:&assetserver.Options{Assets:assets}, OnStartup/OnShutdown, Bind:[]interface{}{app}})`。
  完成判据：`main.go` 中 `Width: 980`、`Height: 700`、`MinWidth`、`MinHeight`、`DisableResize: false`、`Frameless: false`、`AlwaysOnTop: false` 七项全部出现，且**不含** `DisableResize: true`。

- [x] **T5 — 市场程序的偏好写自己的文件，绝不写桌宠 `config.json`**
  [前置] T1
  文件：`tools/plugin-market/config.go`、`tools/plugin-market/config_test.go`
  内容：类型 `marketConfig{MarketBaseURL string json:"marketBaseUrl"; LastSelectedID string json:"lastSelectedId"; WindowW int json:"windowW"; WindowH int json:"windowH"}`；路径 `filepath.Join(dataDir, "plugin-market.json")`；首次运行把基址播种为「桌宠 `config.json` 的 `marketBaseUrl`（非空时）否则 `plugins.DefaultMarketBaseURL`」；`Save` 只写自己的文件。
  完成判据：在临时数据目录上跑一遍保存后，`plugin-market.json` 含写入值，而**同一目录的 `config.json` 字节与运行前完全相同**（测试用 `os.ReadFile` 前后比对）。

- [x] **T6 — 市场程序配置文件损坏时隔离并回落默认值**
  [前置] T5
  文件：`tools/plugin-market/config.go`、`tools/plugin-market/config_test.go`
  内容：`plugin-market.json` 无法解析时改名为 `plugin-market.json.bak`（照 `internal/plugins/manager.go:220-229` 的 `quarantine` 手法），并以默认值继续。
  完成判据：把该文件写成 `{not json` 后打开配置返回 `err == nil`、`plugin-market.json.bak` 存在、生效基址等于 `DefaultMarketBaseURL`。

- [x] **T7 — 绑定 `Installed` 只做投影**
  [前置] T1
  文件：`tools/plugin-market/market.go`、`tools/plugin-market/market_test.go`
  内容：`Installed() ([]plugins.Installed, error)` 直通 `plugins.NewManager(dataDir).Plugins()`，不复制任何插件逻辑。
  完成判据：临时数据目录里手工写入一条 `plugins.json` 条目与对应 `plugins/<id>/manifest.json` 后，`Installed()` 返回该行的 `id`/`version`/`enabled` 与磁盘一致。

- [x] **T7b — 绑定 `PluginMarketPath` 报出自己的 exe 位置**
  [前置] T4
  文件：`tools/plugin-market/market.go`、`tools/plugin-market/market_test.go`
  内容：`PluginMarketPath() (string, bool)` 返回本进程 exe 的绝对路径与「是否存在」（为界面显示「桌宠从哪儿找它」）。
  完成判据：返回值等于 `os.Executable()` 解析出的路径且 `ok==true`（测试环境下如 `os.Executable` 不可用则以注入的假解析器断言被调用一次并返回其值）。

- [x] **T7c — 绑定 `Catalogue` 只做投影，基址由本程序配置提供**
  [前置] T5、T7
  文件：`tools/plugin-market/market.go`、`tools/plugin-market/market_test.go`
  内容：`Catalogue() (plugins.MarketView, error)` 直通 `Manager.Market()`，`Manager.BaseURL` 由本程序配置文件的值提供。
  完成判据：基址指向内置样例源时 `Catalogue()` 返回 `Fallback == true` 且 `Entries` 长度 ≥ 1，条目的 `id` 与样例源清单逐字一致。

- [x] **T8 — 绑定 `Install` 直通既有管理器**
  [前置] T7c
  文件：`tools/plugin-market/market.go`、`tools/plugin-market/market_test.go`
  内容：`Install(id string) error` 从 `Catalogue()` 查得 `plugins.MarketEntry` 后调 `Manager.Install`。
  完成判据：对内置样例源安装 `com.shixiansi.plugin-chat` 后 `plugins/<id>/manifest.json` 存在且 `plugins.json` 出现该条目。

- [x] **T8b — 绑定 `Uninstall` 直通既有管理器**
  [前置] T8
  文件：`tools/plugin-market/market.go`、`tools/plugin-market/market_test.go`
  内容：`Uninstall(id string) error` 调 `Manager.Uninstall`。
  完成判据：对已装插件调用 `Uninstall` 后 `plugins.json` 不再有该条目、`plugins/<id>/` 与 `plugin-data/<id>/` 均不存在。

- [x] **T9 — 绑定 `Grant` 直通既有管理器**
  [前置] T7c
  文件：`tools/plugin-market/market.go`、`tools/plugin-market/market_test.go`
  完成判据：对已装插件 `Grant` 其声明的全部权限后，`Installed()` 中该行的 `Granted` 与传入逐项相等；传入未声明的权限名时 `Grant` 返回错误且 `plugins.json` 的 `granted` 不变。

- [x] **T9b — 绑定 `SetEnabled` 直通既有管理器**
  [前置] T9
  文件：`tools/plugin-market/market.go`、`tools/plugin-market/market_test.go`
  完成判据：对已授权插件 `SetEnabled(id,true)` 后 `Installed()` 中该行 `enabled == true`；`SetEnabled(id,false)` 后该行 `enabled == false`。

- [x] **T10 — 绑定 `SaveMarketURL` 校验后写入本程序配置文件**
  [前置] T5、T7c
  文件：`tools/plugin-market/market.go`、`tools/plugin-market/market_test.go`
  完成判据：传 `ftp://x/y` 时返回错误且 `plugin-market.json` 与 `config.json` 均未被修改；传 `https://example.com/m/` 后配置里存的是去掉尾斜杠的 `https://example.com/m`。

- [x] **T11 — 绑定 `GetConfig` 返回非密钥值与密钥的存在性/掩码**
  [前置] T24、T34、T41
  文件：`tools/plugin-market/market.go`、`tools/plugin-market/market_test.go`
  内容：返回 `{values: map[string]string, secrets: map[string]{set:bool, mask:string}}`，密钥明文**绝不出现在返回值里**。
  完成判据：写入一个 secret 项后再读取，返回的 `values` 不含该 key、`secrets[key].set == true`、`mask` 形如前 3 后 4 且不等于写入值；把整个返回值 `fmt.Sprint` 后**不包含**写入的明文。

- [x] **T12 — 绑定 `SaveConfig` 原子写入并区分 secret 项**
  [前置] T11
  文件：`tools/plugin-market/market.go`、`tools/plugin-market/market_test.go`
  完成判据：保存两个非密钥项与一个密钥项后，`plugin-data/<id>/config.json` 只含两个非密钥项、密钥值只存在于 `secret-<key>.bin`；写入过程中目录内不残留 `.tmp-*` 文件。

- [x] **T14 — 另一个进程改写 `plugins.json` 后市场程序读到新值**
  [前置] T7
  文件：`tools/plugin-market/market_test.go`
  内容：断言 `plugins.Manager` 无内存缓存的既有性质在市场程序的绑定路径上成立（照 `internal/voice/user.go:272-309` 的 reloadIfChanged 立场，但不新增机制）。
  完成判据：先 `Installed()` 取一次快照，然后在同一进程内用**另一次** `plugins.NewManager` 写入一条新条目，再次 `Installed()` 的条目数比第一次多 1 且包含新 id。

- [x] **T15 — 市场程序前端工程骨架可构建**
  [前置] T3
  文件：`tools/plugin-market/frontend/{package.json,vite.config.js,vitest.config.js,index.html,src/main.js,src/style.css}`
  内容：`package.json` 的 `vite` 固定 `4.5.5`、含 `build`/`dev`/`test` 脚本与 `vitest`+`jsdom`；`vite.config.js` 照 `tools/voice-studio/frontend/vite.config.js:1-19`（`base:'./'`、`minify:false`、`target:'esnext'`）；`index.html` 只有一个挂载根；`main.js` 首行 `import './style.css'`。
  完成判据：在 `tools/plugin-market/frontend` 下 `npm run build` 退出码为 0 且 `dist/index.html` 中的资源引用是相对路径（不含以 `/` 开头的 src/href）。

- [x] **T16 — 市场程序左列表与右详情各自独立滚动，互不挤压**
  [前置] T15
  文件：`tools/plugin-market/frontend/{index.html,src/main.js,src/style.css}`、`tools/plugin-market/frontend/src/main.test.js`
  完成判据：vitest（jsdom）断言列表容器与详情容器是**两个不同的**、各自带 `overflow-y:auto` 的滚动容器，且详情容器不是列表容器的后代。

- [x] **T17 — 权限勾选行完整可见且可换行（不截断）**
  [前置] T15
  文件：`tools/plugin-market/frontend/{src/main.js,src/style.css}`、`tools/plugin-market/frontend/src/main.test.js`
  完成判据：一条长权限文本在 vitest 中被渲染进详情区后，其所在元素的计算样式满足「标签与说明同容器、容器允许换行」（`white-space` 非 `nowrap` 且 `word-break`/`overflow-wrap` 允许断行），且该元素无固定像素宽度。

- [x] **T18 — 市场地址输入框占满可用宽度**
  [前置] T15
  文件：`tools/plugin-market/frontend/{index.html,src/style.css}`、`tools/plugin-market/frontend/src/main.test.js`
  完成判据：地址输入框与其保存按钮同一行（同一父容器），输入框无内联 `style.width`、其 CSS 规则含 `flex: 1`（或等效的 `width:100%`），且按钮不因输入框而换行。

- [x] **T18b — 市场程序顶部有刷新控件：点击重新拉取 `Catalogue()` 并更新列表**
  [前置] T15、T7c
  文件：`tools/plugin-market/frontend/{index.html,src/main.js,src/style.css}`、`tools/plugin-market/frontend/src/main.test.js`
  内容：顶部地址框同区放一个刷新按钮（`id="market-refresh"`），点击调 `Catalogue()` 并按返回值重绘左列表；拉取失败时在提示区显示可见文案，**不清空**已有列表。
  完成判据：在假绑定上点一次刷新后 `Catalogue` 调用计数由 1 变 2，且列表条目文本随第二次响应变化；令假绑定返回 `Notice` 非空时点刷新，提示区出现该文案且列表条目数与刷新前相同。

- [x] **T19 — `tools/build-all.ps1` 构建市场程序并复制到 `build\bin`**
  [前置] T3、T15
  文件：`tools/build-all.ps1`
  内容：在既有 `Build-Frontend`/`Build-One` 调用序列中增加市场程序的前端构建与 `wails build -s`，随后把 `tools\plugin-market\build\bin\PetPCPluginMarket.exe` 复制到 `build\bin`（照 `:66-70` 对配音工坊的同一手法，exe 缺失时 `throw`）。
  完成判据：`pwsh -File tools\build-all.ps1` 退出码为 0，`build\bin` 下同时存在 `PetPC.exe`、`PetPCVoiceStudio.exe`、`PetPCPluginMarket.exe` 三个文件。

- [x] **T20 — `.gitignore` 追加市场程序所需规则且既有规则逐条保留**
  [前置] T15
  文件：`.gitignore`
  内容：只追加市场程序构建产物所需的一行（`tools/plugin-market/frontend/dist/`，与既有 `frontend/dist/` 口径一致）；`.gitignore:1-44` 既有全部规则与 4 条白名单（`check-frontend.cjs`、`run-frontend-harness.cjs`、`layout-report.cjs`、`verify-plugin-surface.ps1`）**一字不改**。
  完成判据：`git check-ignore -v tools/plugin-market/frontend/dist/index.html` 命中该条；`git check-ignore -v .dev/verify-plugin-surface.ps1` 无输出；`.gitignore` 中 `market/`、`build/bin/`、`frontend/dist/` 三行与改动前的文本逐字相同。

## B. 规则层：第 10 项权限、manifest 扩展、浮层上限（T21–T33）

- [x] **T21 — 新增第 10 项权限 `plugin.config`**
  文件：`internal/plugins/capability.go`
  内容：常量 `PermPluginConfig = "plugin.config"`（含注释「让插件保存自己的设置；宿主存在插件私有目录里，插件之间互相读不到」）；追加到 `AllowedPermissions` 末尾使其长度为 10。
  完成判据：`len(plugins.AllowedPermissions) == 10` 且最后一项等于 `plugin.config`；`plugins.IsAllowedPermission("plugin.config")` 为真，`IsAllowedPermission("fs.read")` 仍为假。

- [x] **T22 — `plugin.config` 的中文标签进入权限词表**
  [前置] T21
  文件：`internal/plugins/capability.go`
  完成判据：`plugins.PermissionLabels[plugins.PermPluginConfig]` 等于 `"保存你自己的插件配置"`，且 `PermissionLabels` 的键集合与 `AllowedPermissions` 完全相等（无缺失、无多余）。

- [x] **T23 — 权限数量硬断言由九项改为十项**
  [前置] T21
  文件：`internal/plugins/manifest_test.go`
  内容：`TestAllowedPermissionsIsTheNine` 的 `want` 列表加入 `plugin.config` 并改名为 `TestAllowedPermissionsIsTheTen`（注释同步「fixed」数量与理由）。
  完成判据：`go test ./internal/plugins/ -run TestAllowedPermissionsIsTheTen -count=1` 通过；`grep -n 'TestAllowedPermissionsIsTheNine' internal/plugins/*_test.go` 无命中。

- [x] **T24 — manifest 新增 `config` 声明与 `ConfigItem` 类型**
  [前置] T21
  文件：`internal/plugins/manifest.go`
  内容：`ConfigItem{Key,Label,Type,Default,Secret,HostWhitelist}`（json tag 为 `key/label/type/default/secret/hostWhitelist`，后两个 `omitempty`）+ `Manifest.Config []ConfigItem json:"config,omitempty"` + 常量 `maxConfigItems = 16`。
  完成判据：含 `"config":[{"key":"llmBaseUrl","label":"接口地址","type":"text","hostWhitelist":true}]` 且声明了 `net.fetch` 的 manifest 经 `ParseManifest` 后 `len(Config)==1` 且 `Config[0].HostWhitelist == true`。

- [x] **T25 — `config[].key` 格式校验（含拒绝路径）**
  [前置] T24
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  内容：key 必须匹配 `^[A-Za-z0-9_-]{1,64}$`（**与 `app_plugins.go:690` 的 `storageKeyPattern` 同形**，不引入第二套 key 规则）。
  完成判据：`a b`、`../x`、`k` + 65 个字符、空串四种输入各被拒绝且错误消息同时含字段名 `config` 与该 key；`llmBaseUrl`、`a-b_c` 被接受。

- [x] **T26 — `config[].type` 枚举与 `secret` 标志的一致性校验**
  [前置] T24
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  完成判据：`type:"date"` 被拒绝；`type:"secret"` 但 `secret:false` 被拒绝；`secret:true` 但 `type:"text"` 被拒绝；`type:"secret",secret:true` 与 `type:"text"` 被接受。

- [x] **T27 — `config` 内 key 不得重复**
  [前置] T24
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  完成判据：两个同 `key` 项被拒绝，错误消息含该重复 key；`config` 为 `null` 或缺失时解析成功且 `len(Config)==0`。

- [x] **T28 — `config` 项数量上限 16**
  [前置] T24
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  完成判据：16 项被接受、17 项被拒绝且错误消息含 `16` 与 `config`。

- [x] **T29 — `hostWhitelist` 至多一项（不要求声明 `net.fetch`）**
  [前置] T24
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  内容：**只约束「至多一项」**，不把 `net.fetch` 设为前置条件——白名单来源是否被使用由调用路径决定，不由声明层决定。理由（与 design §3.2 同步）：对话插件的请求走宿主 `llm.chat`（`app_plugins.go:829` 的 `a.llmClient().Chat`），**不经过 `net.fetch`**，因此它的 1.1.0 manifest 只声明 `llm.chat`/`pet.bubble`/`pet.surface`/`plugin.config`/`storage`、不声明 `net.fetch`（design §8.1）。若此处要求配对，T65 的 manifest 将无法同时满足「声明合法的 `hostWhitelist` 项」与「不声明 `net.fetch`」，两条任务互斥。
  完成判据：两个 `hostWhitelist:true` 项被拒绝（消息含 `hostWhitelist` 与「至多一个」）；**单个 `hostWhitelist:true` 且权限里没有 `net.fetch` 时该项被接受**（`len(Config)==1` 且 `Config[0].HostWhitelist==true`），只是在 `net.fetch` 层面不产生任何放行（该语义由 T44 断言：未声明 `net.fetch` 的插件根本没有 `net.fetch` 调用路径，`ConfigHosts` 的返回值无处生效）；单 `hostWhitelist:true` 且有 `net.fetch` + `netHosts` 同样被接受。

- [x] **T30 — manifest 新增 `panelSlot` 声明与校验**
  [前置] T24
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  内容：`PanelSlot string json:"panelSlot,omitempty"`，只接受 `""`/`"chat"`；`panelSlot=="chat"` 要求 `panel==true`。
  完成判据：`panelSlot:"todo"` 被拒绝（消息含 `panelSlot`）；`panelSlot:"chat"` 且 `panel:false` 被拒绝；`panelSlot:"chat"` + `panel:true` 被接受；缺省时为空串。

- [x] **T31 — 浮层面矩形上限由 4 提升到 8**
  [前置] T21
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  完成判据：声明 8 块矩形被接受、9 块被拒绝且错误消息含 `8`；`maxSurfaceRects` 常量的值为 8。

- [x] **T32 — `Rect` 新增 `ellipse` 标志（供思考泡泡）**
  [前置] T31
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  内容：`Ellipse bool json:"ellipse,omitempty"`；`ellipse:true` 的矩形仍须满足既有的宽高为正与上限规则（不新增限制，也不因 radius 同时存在而报错）。
  完成判据：`{"dx":10,"dy":10,"w":34,"h":20,"ellipse":true}` 解析后 `Surface[0].Ellipse == true`；`{"dx":10,"dy":10,"w":0,"h":20,"ellipse":true}` 仍被拒绝且消息含 `宽高必须为正`。

- [x] **T33 — `Manifest.ConfigItem(key)` 访问器**
  [前置] T24
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  完成判据：对声明项 `llmBaseUrl` 返回该 `ConfigItem` 且 `ok==true`；对未声明的 `x` 返回零值且 `ok==false`。

## C. 插件配置存储（T34–T43c）

- [x] **T34 — `ConfigStore` 与 `SecretKeeper` 接口，且不破坏 `internal/plugins` 的依赖边界**
  [前置] T24
  文件：`internal/plugins/config.go`（新建）、`internal/plugins/config_test.go`（新建）
  内容：`SecretKeeper interface { Get(path string) (string, error); Set(path, value string) error; Clear(path string) error }`；`ConfigStore{pluginDataDir, manifest, keeper}`；`NewConfigStore(privateDir string, man Manifest, keeper SecretKeeper) *ConfigStore`。
  完成判据：`go build ./internal/plugins/` 通过；`grep -n 'internal/desktop' internal/plugins/*.go` 无命中（保持 `manifest.go:9-13` 声明的「不 import internal/desktop」边界）。

- [x] **T35 — 原子写手法的包级 helper 被索引写入与新配置写入共用**
  [前置] T34
  文件：`internal/plugins/manager.go`、`internal/plugins/config.go`、`internal/plugins/manager_test.go`
  内容：把 `saveIndex`（`manager.go:186-218`）里的「`os.CreateTemp` + `Write` + `Sync` + `Close` + `os.Rename`」抽为包级 `atomicWriteFile(path string, data []byte) error`，两处共用；语义与失败行为一字不变。
  完成判据：`go test ./internal/plugins/ -count=1` 全绿（既有 `manager_test.go` 断言未改）；写文件失败路径下目标文件保持原内容且不残留 `.tmp-*`。

- [x] **T36 — 非密钥配置项读写往返**
  [前置] T34、T35
  文件：`internal/plugins/config.go`、`internal/plugins/config_test.go`
  完成判据：写入 `{llmBaseUrl:"https://x/v1", llmMaxTokens:"123"}` 后新建一个 `ConfigStore` 读回，两值逐字相同；`plugin-data/<id>/config.json` 存在且是合法 JSON。

- [x] **T37 — 写入未声明的 key 被拒绝且不改变已存值**
  [前置] T36
  文件：`internal/plugins/config.go`、`internal/plugins/config_test.go`
  完成判据：先写 `llmBaseUrl=A`，再写 `{llmBaseUrl:"B","x":"1"}` 返回错误、`config.json` 中 `llmBaseUrl` 仍为 `A` 且不含 `x`。

- [x] **T38 — `secret` 项的值不写进 `config.json`**
  [前置] T36
  文件：`internal/plugins/config.go`、`internal/plugins/config_test.go`
  内容：secret 项经 `keeper.Set(<私目录>/secret-<key>.bin, v)` 落盘；`config.json` 对应键存空串。
  完成判据：写入 `llmApiKey="sk-abcdefgh"` 后 `config.json` 的字节**不包含** `sk-`；`secret-llmApiKey.bin` 存在且其内容不等于明文（由假 keeper 断言 `Set` 收到的路径与值各一次）。

- [x] **T39 — 没有注入 `SecretKeeper` 时 secret 写入被拒绝，绝不回落明文**
  [前置] T38
  文件：`internal/plugins/config.go`、`internal/plugins/config_test.go`
  完成判据：`NewConfigStore(dir, man, nil)` 上写 secret 项返回错误、`config.json` 不存在、该值不出现在私目录的任何文件中。

- [x] **T40 — secret 解密失败视作「未配置」而不是错误**
  [前置] T38
  文件：`internal/plugins/config.go`、`internal/plugins/config_test.go`
  内容：立场与 `internal/desktop/secret.go:59-79` 一致（换用户/换机器后旧密文不可解不应报栈）。
  完成判据：假 keeper 的 `Get` 返回错误时 `Read` 的 `err == nil` 且该项 `set == false`。

- [x] **T41 — 读取只返回密钥的存在性与掩码，明文不出 Go 层**
  [前置] T38
  文件：`internal/plugins/config.go`、`internal/plugins/config_test.go`
  完成判据：`Read` 返回的结构里含 `{set:true, mask:"sk-…cdef"}` 形态的一项，且把整个返回结构 `fmt.Sprintf("%#v", ...)` 后不包含注入的明文密钥。

- [x] **T42 — secret 项写入空串等于清除该密钥**
  [前置] T38
  文件：`internal/plugins/config.go`、`internal/plugins/config_test.go`
  内容：settled in design §11 D3。
  完成判据：写入非空后 `set==true`；再写空串后 `set==false` 且 `secret-<key>.bin` 不存在；`config.json` 仍可解析。

- [x] **T43 — 卸载插件同时删除其配置与密钥**
  [前置] T36、T38
  文件：`internal/plugins/manager_test.go`
  内容：沿用既有 `Manager.Uninstall` 已删除 `plugin-data/<id>/` 的行为（`manager.go:394-423`），为「配置与密钥随之消失」补断言。
  完成判据：写入一个普通项与一个 secret 项后 `Uninstall(id)`，`plugin-data/<id>/` 目录不存在、`config.json` 与 `secret-*.bin` 均不可读。

## D. 配置化域名白名单与配置能力（T43b–T46）

- [x] **T43b — `config.get` / `config.set` 进入能力表并受 `plugin.config` 门控**
  [前置] T36、T43
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`pluginMethods` 新增 `"config.get": {plugins.PermPluginConfig, pluginConfigGet}` 与 `"config.set": {plugins.PermPluginConfig, pluginConfigSet}`；两者走注入到 `plugins.Manager` 的配置存储（与市场程序同一份文件）。
  完成判据：`TestCapabilityTableIsComplete`（`app_plugins_test.go:863-881`）通过，即能力表用到的权限集合与 `AllowedPermissions` 逐项相等（新增 `plugin.config` 后自动覆盖）；未授予 `plugin.config` 的插件调用两者各自返回「未获权限 plugin.config」且配置文件的修改时间不变。

- [x] **T43c — `config.get` 面向插件只返回非密钥值**
  [前置] T43b
  文件：`app_plugins.go`、`app_plugins_test.go`
  完成判据：插件先 `config.set` 一个普通项与一个 secret 项，再 `config.get` 的返回 JSON 中含普通项的值、secret 项只含 `{set:true, mask}`，且返回的 JSON 文本**不包含**写入的明文密钥。

- [x] **T44 — `ConfigHosts` 从配置值解析出可放行主机名**
  [前置] T24、T36
  文件：`internal/plugins/config.go`、`internal/plugins/config_test.go`
  内容：`func ConfigHosts(man Manifest, values map[string]string) []string`：取唯一 `HostWhitelist==true` 项的值，按 `,`/空白切分；含 `://` 时要求 scheme ∈ `{http,https}` 并取 `u.Host`；不含 `://` 时要求匹配 `netHostPattern`（`manifest.go:84`）；含 `*` 的段丢弃；空段丢弃。
  完成判据：值 `https://a.example.com/v1, b.example.com:8443` 解析出 `["a.example.com","b.example.com:8443"]`；值 `""`、`"ftp://a/b"`、`"*.example.com"` 三种各自解析出空列表。

- [x] **T45 — `net.fetch` 的判定合并静态 `netHosts` 与配置化白名单**
  [前置] T44
  文件：`app_plugins.go`、`app_plugins_test.go`
  完成判据：`netHosts:["static.example.com"]` + 配置项值 `https://dynamic.example.com/v1` 时，对 `https://dynamic.example.com/x` 的 `net.fetch` 到达假服务并返回其响应体，对 `https://static.example.com/x` 同样到达；请求用 `GET` 之外的方法（如 `PUT`）仍被拒绝且不触网。

- [x] **T46 — 配置化白名单不会成为任意域名放行**
  [前置] T45
  文件：`app_plugins_test.go`
  完成判据：配置项值为空、为 `ftp://evil.example.com/x`、为 `*.example.com` 三种情形下，对 `https://evil.example.com/x` 与 `https://sub.example.com/x` 的 `net.fetch` 均返回错误且**假服务零请求**（用一个记录请求数的 `httptest.Server` 断言）。

## E. 宿主：面板槽位、浮层面、双击入口（T47–T57）

- [x] **T47 — 新增 `PluginsInstalled()` 绑定供槽位与浮层面解析**
  [前置] T21
  文件：`app_plugins.go`、`app_plugins_test.go`
  完成判据：已装一条启用插件时 `PluginsInstalled()` 返回一行且其 `id`/`enabled`/`permissions`/`granted`/`panelSlot` 字段与磁盘 manifest 一致。

- [x] **T48 — 另一个进程改写 `plugins.json` 后桌宠读到新值**
  [前置] T47
  文件：`app_plugins_test.go`
  完成判据：先调一次 `PluginsInstalled()` 记下条目数，再用另一个 `plugins.NewManager(a.store.Dir())` 写入一条新条目，再次调用时条目数 +1 且包含新 id。

- [x] **T49 — 解析「对话」槽位的插件（确定性唯一来源）**
  [前置] T47
  文件：`app_chat_slot.go`（新建）、`app_chat_slot_test.go`（新建）
  内容：`func (a *App) chatSlotPlugin() (plugins.Installed, bool)`：从 `PluginsInstalled()` 顺序（= `plugins.json` 顺序）取**第一个**满足「`Enabled==true` 且 `Granted` 含 `pet.surface` 且 `Manifest.PanelSlot=="chat"`」的插件；无则 `ok==false`。
  完成判据：构造三个假插件（未启用、启用但未授权、启用且授权）时返回第三个；只有前两个时返回 `ok==false`。

- [x] **T50 — 无可用对话插件时槽位显示明确提示而不是空 iframe**
  [前置] T49
  文件：`frontend/index.html`、`frontend/src/main.js`、`frontend/src/style.css`
  完成判据：`#chat-slot` 内不出现 `iframe`，并出现文本「对话插件还没装好：在设置里点「打开插件市场」装上并授权它。」

- [x] **T51 — 插件槽位的 iframe 按槽位挂载，`init` 携带槽位名**
  [前置] T49
  文件：`frontend/src/plugin-host.js`、`frontend/src/main.js`
  完成判据：`PluginHost.openSlot('chat', plugin)` 在 `#chat-slot` 内产生一个 `iframe`，其 `sandbox` 恰为 `allow-scripts`、`src` 以 `/plugin-asset/<id>/` 开头；该帧收到的 `init` 消息的 `surface.kind === 'panel'` 且 `surface.slot === 'chat'`。

- [x] **T52 — `PluginSurfaceRect` 携带椭圆标志**
  [前置] T32
  文件：`app_plugin_surface.go`、`app_plugin_surface_test.go`
  完成判据：`computePluginSurface` 对声明里 `ellipse:true` 的矩形返回的 `PluginSurfaceRect.Ellipse == true`，对其余矩形为 `false`。

- [x] **T53 — 椭圆矩形的 region 用椭圆并由 `Layout` 同源下发**
  [前置] T52
  文件：`app_plugin_surface.go`、`app_plugin_surface_test.go`
  完成判据：对含一个 `ellipse:true` 矩形的声明，返回的 `desktop.RegionRect` 中对应块的 `Ellipse == true` 且 `Radius == 0`；同一块的 `PluginSurfaceRect` 与 region 的 `X/Y/W/H` 互为 `/s` 与 `*s` 的换算关系（沿用既有断言形态）。

- [x] **T54 — 前端对椭圆块画成真正的椭圆**
  [前置] T52
  文件：`frontend/src/plugin-host.js`
  完成判据：`applyGeometry` 对 `rect.ellipse===true` 的帧设置 `border-radius: 50%`（四个角都是 `50%`），非椭圆块仍按 `r.r` 设四角圆角。

- [x] **T55 — 双击宠物打开的是对话插件的浮层面（无插件时给出气泡提示）**
  [前置] T49、T51
  文件：`app_chat_slot.go`、`app_plugins_test.go`、`frontend/src/main.js`
  内容：新绑定 `OpenChatPluginSurface() (Layout, error)`：有可用插件时调既有 `OpenPluginSurface(id)`；无可用插件时用既有 `showBubble` 弹一句说明并返回当前 Layout。
  完成判据：有插件时返回的 `Layout.PluginSurface.PluginID` 等于该插件 id 且 region 含其声明矩形；无插件时 `Layout.PluginSurface == nil` 且当前气泡非 nil、文案含「插件市场」。

- [x] **T56 — 删除被取代的宿主实时浮层实现（几何与 UI 一并收敛到插件浮层面）**
  [前置] T55
  文件：`app_live.go`（删除）、`app_live_test.go`（删除）、`app.go`、`app_llm.go`、`app_reminder.go`、`app_pomodoro.go`、`app_hotkey.go`、`app_plugin_surface.go`、`app_bubble_plan_test.go`、`app_plugin_surface_state_test.go`、`frontend/src/live.js`（删除）、`frontend/src/main.js`
  内容（逐点写出终态）：
  ① `app_live.go`/`app_live_test.go` 整文件删除（含 `LiveRect`/`LiveView`/`computeLiveOverlay`/`setLiveLocked` 与其全部单测）；
  ② `app.go`：删 `case live:` 分支（`:707-715`）、`Layout.Live` 字段（`:496`）、`layout.Live = liveView`（`:788`）、`liveOpen` 字段（`:60`）、`StartDrag` 中的 `live := a.liveOpen` 与 `CloseLive` 调用（`:905-916`）、`SetPanelOpen` 中的 `wasLive`/`setLiveLocked(false)`/`emitLive(false)`（`:1040-1064`）与 `:1512` 的 `setLiveLocked(false)`；
  ③ `app_llm.go`：删 `OpenLive`/`CloseLive`/`liveIsOpen`/`emitLive` 四个函数与 `OpenLive` 的守卫注释块（`:1012-1106`）；**并删掉 `rescheduleProactive` 体内对这四个函数的唯一引用**——`:327-333` 的 `if a.liveIsOpen() && !cfg.LLMEnabled { ... a.CloseLive() ... }` 整块（否则删掉函数后本文件编译不过）。该块外面的主动说话调度行（`:325-326`、`:336-337`）**本次不动**，由 T87c 连同 `rescheduleProactive` 本体一起删除——两步各自有单一终态，边界在这里写死；
  ④ `app_reminder.go`：`showReminderBubble` 的 `liveIsOpen()` 抑制块**改写为**对同一槽位的插件浮层面的抑制——在 `a.mu` 下读 `a.pluginSurface`，非 nil 时按原句式 `reminder: suppressed（对话浮层面打开中）` 记录并返回 false；`OpenTodoPanelFromBubble` 中的 `wasLive`/`setLiveLocked(false)`/`emitLive(false)`（`:440-445`）删除；
  ⑤ `app_pomodoro.go`：`showBubble` 的 `wasLive`/`setLiveLocked(false)`/`emitLive(false)`（`:1072-1083`）删除；
  ⑥ `app_hotkey.go`：`HidePet` 的 `wasLive`/`setLiveLocked(false)`/`emitLive(false)`（`:254-267`）删除；
  ⑦ `app_plugin_surface.go`：`OpenPluginSurface` 的 `wasLive`/`setLiveLocked(false)`/`emitLive(false)`（`:240-252`）删除；
  ⑧ `app_bubble_plan_test.go`（本文件里的两条，**与 T57 处理的 `pomodoro_gate_test.go` 四条不是同一批**）：`TestChatPopupCannotEatThePlanBubble` 与 `TestChatPopupStillTakesTheSlotFromAReminder` 的调用与断言由 `OpenLive()`/`liveIsOpen()` 改为 `OpenPluginSurface(id)`/`a.pluginSurface`（断言内容逐条保留：拒绝时状态不变、提问仍在、计时器仍停、提醒仍被替换）。**该改写与 T57 的四条同属「测试收敛」，其源码级核对由 T57 的判据承接**（本任务的实现判据不含测试文件；整包运行由 T87c 承接）；
  ⑨ `app_plugin_surface_state_test.go`：`TestExistingOpeningsClearTheSurface` 的用例表删去 `{"OpenLive", ...}` 一项（槽位占据者由三个改为两个：面板、气泡）；
  ⑩ `main.js`：`toggleLive`/`closeLive` 与 `EventsOn('pet:live')` 订阅删除（其占位回归 T87 删除的 `ChatPopup` 接线）。
  完成判据（**只判实现文件**，测试改写的核对统一由 T57 承接——见下方口径）：`grep -rn 'liveIsOpen\|emitLive\|OpenLive\|CloseLive\|computeLiveOverlay\|setLiveLocked\|liveOpen\|[Ll]ayout\.Live\b\|\bLiveView\b\|\bLiveRect\b' --include=*.go --include=*.js --glob='!*_test.go' .` 零命中（`_archive/` 亦无命中）；本任务的判据**不含** `go build ./...`（理由见下方口径 (b)）。
  **口径（与 T57、T87b、T87c 的顺序，收敛为唯一路径）**：（a）与 T57：本任务的**完成判据不含** `go test ./...`——第 ⑧ 项的两条测试改写与 T57 的四条同属「测试收敛」，而 `pomodoro_gate_test.go` 四条此刻仍按 `OpenLive()`/`liveIsOpen()` 调用，**在同一任务内无法先改实现再让全仓测试同步全绿**。因此两件事按「实现收敛」与「测试收敛」分开：T56 的判据是**实现文件零命中**（第 ⑧ 项的改写是本任务的伴随改动，其源码级核对由 T57 的判据统一承接），T57 的判据是**对赌语义在新承载上逐条保留（源码级）**。(c) 见下：整包运行判据统一由 T87c 承接。
   （b）与 T87b/T87c：本任务的**完成判据同样不含** `go build ./...` 与 `go vet ./...`。原因写死在这里：`app_live.go` 随本任务整文件删除后，`app_llm.go` 的 `rescheduleProactive` 仍引用随之消失的 `setNextProactive`（`:326`/`:336`）与 `proactiveGap`（`:336`），而这一半由 T87c 删除；因此**在链尾 T87c 落地之前整包不可编译**，`go build ./...` 在本任务的前置位置上不可满足。T56 → T57 → T87b → T87c 是同一条「宿主实时浮层 + 宿主对话引擎 + 主动说话」删除链：**T56、T57、T87b 都只判源码级终态，整包的 `go build ./...`、`go vet ./...` 与 `go test ./... -count=1` 唯一由链尾 T87c 承接**（T87c 判据已含这三条）。这样每个任务在该位置上都有一个真能达成的单一终态，D 阶段不会在同一处再撞顺序矛盾。
    （c）**排期（唯一路径，执行顺序在此写死）**：`[前置]` 表达**内容依赖**（T56 需要 T55 的槽位机制、T57 需要 T56 的改写对象），不表示执行时序。实际执行顺序为：
    **E 组前段（T47–T55）→ F/G/H 组（T58–T77）→ I 组前段（T78–T87）→ 本批次（T56→T57→T87b→T87c）→ T87d/T87e/T87f → J/K/L/M 组**。
    理由：本批次一旦开始，根包在链尾 T87c 之前不可编译，而 F/G/H 组（T72–T77 的 `app_plugins_test.go`）与 I 组前段（T85/T86 的假启动器用例）的判据都要**运行根包测试**，T69/T70 要跑 `go build`；把这些判据排在批次之前，每个判据在其位置上才可满足。批次内部只有链尾 T87c 断言 `go build ./...`/`go vet ./...`/`go test ./... -count=1`。批次之后的 T87d 起（含 `go build ./...` 判据）与 J/K/L/M 组都能整包编译。

- [x] **T57 — 提问气泡对浮层的四条安全对赌在新承载上仍然成立**
  [前置] T56
  文件：`app_pomodoro_test.go` 或 `pomodoro_gate_test.go`、`app_bubble_plan_test.go`
  内容：把 `TestChatPopupCannotEatTheSummaryPrompt`、`TestChatPopupCannotEatTheTodoPicker`、`TestChatPopupStillTakesTheSlotFromANotification`、`TestSummaryPromptEvictsAnOpenChatPopup` 四条改为按 `OpenPluginSurface(id)` 调用，**断言内容逐条保留**（拒绝时状态不变、反向一半仍成立、提问出现时浮层被收掉且 layout 不再宣称有浮层）；并核对 T56 ⑧ 已改写的 `app_bubble_plan_test.go` 两条断言逐条保留。
  完成判据（**源码级终态**）：这四个测试名存在且其中「拒绝且不改状态」的断言仍逐项检查 `pluginSurface==nil`、气泡仍在、`pendingRecordId` 仍在、计时器停住；`app_bubble_plan_test.go` 的 `TestChatPopupCannotEatThePlanBubble`/`TestChatPopupStillTakesTheSlotFromAReminder` 两条同样逐条保留断言且不再引用 `OpenLive`/`liveIsOpen`（`grep -c 'OpenLive\|liveIsOpen' app_bubble_plan_test.go` 为 0）。
   **口径（本任务不判 `go test ./...` 的运行结果）**：这里只判「测试源码已在新承载上改写、断言未放宽」。**运行层面的 `go test ./... -count=1`（连同 `go build ./...`、`go vet ./...`）唯一由链尾 T87c 承接**——本任务前置 T56 删掉 `app_live.go` 后根包不可编译（见 T56 口径 (b)），此刻执行任何整包测试都会失败于编译，而不是失败于这四条对赌。这样 T57 的终态（四条对赌语义逐条保留在源码里）与 T87c 的终态（整包编译与测试全绿）各自唯一、互不重叠。

## F. 一次性幂等迁移（T58–T64c）

- [x] **T58 — 迁移的前置判定：对话插件未安装则不迁移、不写标记、不报错**
  [前置] T36
  文件：`internal/plugins/migrate.go`（新建）、`internal/plugins/migrate_test.go`（新建）
  内容：`MigrateChatData(dataDir string, keeper SecretKeeper) error` + 常量 `ChatPluginID = "com.shixiansi.plugin-chat"` + 标记名 `.migrated`。
  完成判据：数据目录里有 `llm-history.json` 但 `plugins.json` 无该插件条目时调用返回 `err == nil`，源文件未改名、`plugin-data/<id>/` 未创建、`.migrated` 不存在。

- [x] **T59 — 13 个 `llm*` 配置值搬进插件配置，且不改动 `config.json`**
  [前置] T58
  文件：`internal/plugins/migrate.go`、`internal/plugins/migrate_test.go`
  完成判据：`config.json` 写入 13 个 `llm*` 值（键名与 `Config` 的 json tag 逐字相同）的可解析 JSON 后执行迁移，读回的插件配置中含同名 13 个键且值一致；`config.json` 的字节前后完全相同；插件配置里**已存在**的键不被覆盖。

- [x] **T60 — `llm-history.json` 搬进插件私有存储并保留 `.bak`**
  [前置] T58
  文件：`internal/plugins/migrate.go`、`internal/plugins/migrate_test.go`
  完成判据：迁移后 `plugin-data/<id>/llm-history` 字节与原 `llm-history.json` 相同，且 `llm-history.json.bak` 存在、原 `llm-history.json` 不存在。

- [x] **T61 — `llm-usage.json` 搬进插件私有存储并保留 `.bak`**
  [前置] T58
  文件：`internal/plugins/migrate.go`、`internal/plugins/migrate_test.go`
  完成判据：迁移后 `plugin-data/<id>/llm-usage` 字节与原 `llm-usage.json` 相同，且 `llm-usage.json.bak` 存在、原 `llm-usage.json` 不存在。

- [x] **T62 — `llm-key.bin` 解密后按插件隔离重新加密存储**
  [前置] T58、T38
  文件：`internal/plugins/migrate.go`、`internal/plugins/migrate_test.go`
  完成判据：假 keeper 先以旧路径读到明文再写到 `secret-llmApiKey.bin` 时，`keeper.Set` 恰好被调用一次且路径以 `secret-llmApiKey.bin` 结尾；迁移后 `llm-key.bin.bak` 存在、明文不出现在任何文件里。

- [x] **T63 — 迁移幂等：第二次调用不读源文件、不重复搬动**
  [前置] T62
  文件：`internal/plugins/migrate.go`、`internal/plugins/migrate_test.go`
  完成判据：连续调用两次，第二次返回 `err == nil`；第二次调用期间 keeper 的 `Get`/`Set` 调用计数为 0（用计数假 keeper 断言），目标文件内容与第一次结束后逐字节相同。

- [x] **T64 — 迁移中途失败时原文件全部保持原名原样**
  [前置] T62
  文件：`internal/plugins/migrate.go`、`internal/plugins/migrate_test.go`
  完成判据：让 keeper 的 `Set` 返回错误（或在写第三份目标时注入失败）后调用迁移返回非 nil 错误；三个源文件仍以原名存在、无 `.bak`、`.migrated` 不存在；下一次（keeper 正常）调用仍能把三份数据完整搬过去。

- [x] **T64b — 桌宠启动序列调用迁移（与市场程序共用同一份实现）**
  [前置] T64
  文件：`app.go`（或启动路径所在文件）、`app_plugin_migrate_test.go`（新建）
  完成判据：以「数据目录含三个源文件 + 对话插件已安装」的状态构造 App 并跑启动序列后，`.migrated` 存在且 `llm-history.json` 已改名为 `.bak`；`grep -c 'plugins.MigrateChatData' app*.go` 为 1（**不复制**实现）。

- [x] **T64c — 市场程序启动时也调用迁移**
  [前置] T64
  文件：`tools/plugin-market/market.go`、`tools/plugin-market/market_test.go`
  完成判据：市场程序在数据目录含三个源文件且对话插件已安装时构造绑定对象后，`.migrated` 存在；其源码里调用的是 `plugins.MigrateChatData`（`grep -c 'MigrateChatData' tools/plugin-market/*.go` 为 1）。

## G. 对话插件 1.1.0（T65–T71）

- [x] **T65 — 对话插件 manifest 升版并声明配置、槽位与 5 块矩形**
  [前置] T24、T29、T30、T31、T32
  文件：`market/plugins/com.shixiansi.plugin-chat/manifest.json`
  内容：`version` 1.1.0、`permissions` 为 `["llm.chat","pet.bubble","pet.surface","plugin.config","storage"]`（**不含 `net.fetch`**、manifest 也**不含 `netHosts`**——该插件的请求走宿主 `llm.chat`，由宿主 `internal/llm` 直发，不经过 `net.fetch`；这是与 T29 一致的唯一路径，见 design §3.2 与 §8.1）、`panel:true` + `panelSlot:"chat"`、`config` **14 项**（13 个 `llm*` 一键一位 + `llmApiKey` 为唯一 secret；`llmBaseUrl` 为唯一 `hostWhitelist`）、`surface` 5 块（卡片 112 高、输入条 56 高、三个 `ellipse:true` 泡泡 34×20 / 24×14 / 15×9）。
  完成判据：该 manifest 经 `plugins.LoadManifest` 解析成功；`len(cfg)==14`、13 个 `llm*` 键名与 `Config` 的 13 个 json tag 逐字相等、`hostWhitelist` 项恰 1 个、`secret` 项恰 1 个、`len(Surface)==5` 且恰 3 块 `Ellipse`；且 `HasPermission("net.fetch")` 为假、`len(NetHosts)==0`（与 `hostWhitelist` 项共存不报错，正因为 T29 已把配对要求放宽为「至多一项」）。

- [x] **T66 — 插件前端按 `surface.kind` 渲染面板槽位与浮层块**
  [前置] T65
  文件：`market/plugins/com.shixiansi.plugin-chat/{panel.html,app.js}`
  完成判据：`init` 的 `surface.kind==='panel'` 时渲染会话视图（含消息列表与输入框），`'surface'` 时按 `surface.rectIndex` 分别渲染卡片 / 输入条 / 泡泡三种外观（三种外观的根元素 class 互不相同）。

- [x] **T67 — 面板槽位的对话走既有 `llm.chat` 流式帧**
  [前置] T66
  文件：`market/plugins/com.shixiansi.plugin-chat/app.js`
  完成判据：该插件前端订阅 `pet:event` 并对 `llm.delta`/`llm.done`/`llm.error` 三种 `name` 各有分支；`llm.done` 分支在文本与已累积内容不一致时以 `llm.done` 的全文替换显示。

- [x] **T68 — 插件样式不含越界装饰，泡泡画成椭圆**
  [前置] T66
  文件：`market/plugins/com.shixiansi.plugin-chat/style.css`
  完成判据：文件内 `box-shadow`/`filter`/`text-shadow`/`transform` 四种属性各自零命中；泡泡类含 `border-radius:50%`。

- [x] **T69 — 打包脚本产出 1.1.0 的 zip 与新目录**
  [前置] T65
  文件：`market/tools/package.ps1`（不改代码，执行）、`market/dist/com.shixiansi.plugin-chat-1.1.0.zip`、`market/index.json`、`internal/plugins/sample/{index.json,com.shixiansi.plugin-chat-1.1.0.zip}`
  完成判据：`pwsh -File market/tools/package.ps1` 退出码 0；`market/index.json` 中该条目的 `version` 为 `1.1.0`、`download` 指向 `dist/com.shixiansi.plugin-chat-1.1.0.zip`、`sha256` 匹配 `^[0-9a-f]{64}$` 且与实算值相等。

- [x] **T70 — 内置样例源升级到 1.1.0（清单、zip、旧包）**
  [前置] T69
  文件：`internal/plugins/sample/embed.go`、`internal/plugins/sample/index.json`、`internal/plugins/sample/com.shixiansi.plugin-chat-1.0.0.zip`（删除）
  完成判据：`embed.go` 的 `//go:embed` 清单列出 `com.shixiansi.plugin-chat-1.1.0.zip`、旧的 1.0.0 zip 不在仓内（`grep -c '1.0.0' internal/plugins/sample/embed.go` 为 0）；`go build ./internal/plugins/` 通过。

- [x] **T71 — 两处目录的条目与 zip 字节完全一致**
  [前置] T69、T70
  文件：`internal/plugins/market_test.go`
  完成判据：`market/index.json` 与 `internal/plugins/sample/index.json` 中该条目的 `version`/`download` 基名/`sha256` 三项逐字相同；`market/dist/com.shixiansi.plugin-chat-1.1.0.zip` 与 `internal/plugins/sample/` 下同名 zip 的 sha256 相等。

## H. `llm.chat` 的参数来源、预算与用量（T72–T77）

- [x] **T72 — `llm.chat` 的模型参数全部来自调用插件自己的配置**
  [前置] T36、T38、T65
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：请求参数从**零值** `store.Config` 起步，仅由该插件的 `config` 项（含 secret 密钥）填充后交给既有的 `a.promptContext(cfg)` / `a.llmSettings(cfg)` / `a.llmClient().Chat`。
  完成判据：给插件配置 `llmBaseUrl` 指向假服务 A、桌宠 `config.json` 的 `llmBaseUrl` 指向假服务 B，`llm.chat` 的请求只到达 A（B 的请求计数为 0），且请求体的 `model` 取自 A 配置的 `llmModel`。

- [x] **T73 — 未配置时的拒绝文案分两类且不改状态**
  [前置] T72
  文件：`app_plugins.go`、`app_plugins_test.go`
  完成判据：缺端点或缺模型时返回的错误含「还没配置端点与模型」且**没有发出任何 HTTP 请求**；端点模型齐全但缺密钥且该插件声明了 secret 项时返回的错误含「还没有填写 API Key」，同样零请求。

- [x] **T74 — 每日预算仍由宿主强制，用量文件由宿主唯一写**
  [前置] T72
  文件：`app_plugins.go`、`app_plugins_test.go`
  完成判据：插件配置 `llmDailyBudget=1` 时，第一次 `llm.chat` 到达假服务，第二次被拒绝且错误含「上限」；`plugin-data/<id>/llm-usage` 中 `requests` 随每次**尝试**累加（第二次被拒也 +1，沿用既有语义），`date` 为当日。

- [x] **T75 — 用量滚动与累加抽为可复用的导出纯函数**
  [前置] T74
  文件：`internal/store/llm.go`、`internal/store/llm_test.go`
  内容：新增 `func (u LLMUsage) RolledTo(now time.Time) LLMUsage` 与 `func (u LLMUsage) PlusAttempt(promptTokens, completionTokens int) LLMUsage`；既有 `normalise`/`addTokens` 改为调用它们（纯重构）。
  完成判据：`RolledTo` 对昨日记录返回 `{Date: 今日, Requests:0, ...}`、对当日记录原样返回；`PlusAttempt` 次数 +1 且两个 token 数按「零值不加」规则累加；`go test ./internal/store/ -count=1` 全绿（既有断言未改）。

- [x] **T76 — 对话历史由插件经 `storage.*` 读写，宿主不写转写**
  [前置] T72、T67
  文件：`app_plugins.go`、`app_plugins_test.go`
  完成判据：一次成功的 `llm.chat` 之后，宿主**没有**向 `plugin-data/<id>/llm-history` 写入任何内容（该文件不存在或字节未变）；而该插件用 `storage.set`/`storage.get` 读写 key `llm-history` 能成功往返。

- [x] **T77 — `llm.chat` 的流式帧协议与既有 SSE/重试/合并实现零改动**
  [前置] T72
  文件：`app_plugins_test.go`、`internal/llm/**`（只读核对）
  完成判据：`TestLLMChatStreamsToThePlugin`（或同名等价断言）通过并断言收到 `llm.delta` 若干帧后跟一帧 `llm.done`、`llm.done.text` 为全文；`git diff --stat internal/llm/` 输出为空。

## I. 宿主侧收口（T78–T87f）

- [x] **T78 — `frontend/index.html` 删除插件 tab 按钮**
  文件：`frontend/index.html`
  完成判据：`grep -c 'data-tab=' frontend/index.html` 为 4，且 `grep -c 'data-tab="plugins"'` 为 0；四者依次为 `todos`/`chat`/`records`/`settings`。

- [x] **T79 — `frontend/index.html` 删除 `#tab-plugins` 整块**
  [前置] T78
  文件：`frontend/index.html`
  完成判据：`grep -c 'id="tab-plugins"' frontend/index.html` 为 0；`plugin-manage`/`plugin-list`/`plugin-count`/`plugin-status`/`plugin-refresh`/`plugin-panel-view`/`plugin-panel-back`/`market-base-url`/`market-base-save`/`market-notice`/`market-list`/`plugin-empty` 十三个 id 在该文件中全部零命中。

- [x] **T80 — 删除 `frontend/src/plugins.js`**
  [前置] T79
  文件：`frontend/src/plugins.js`（删除）
  完成判据：该文件不存在；`grep -rn "plugins.js\|PluginsTab" frontend/src/ tools/plugin-market/frontend/src/ --include=*.js` 无命中。

- [x] **T81 — `main.js` 删除插件 tab 的 els 与组件实例**
  [前置] T80
  文件：`frontend/src/main.js`
  完成判据：`grep -c 'tabPlugins\|pluginManage\|pluginList\|pluginCount\|pluginStatus\|pluginRefresh\|marketBaseUrl\|marketBaseSave\|marketList\|new PluginsTab' frontend/src/main.js` 为 0。

- [x] **T82 — `switchTab` 只切换 4 个 tab，且不再触发市场拉取**
  [前置] T81
  文件：`frontend/src/main.js`
  完成判据：`switchTab` 体内含 `tabTodos`/`tabChat`/`tabRecords`/`tabSettings` 四条 `classList.toggle` 且**不含** `tabPlugins`；`grep -c "name === 'plugins'"` 为 0。

- [x] **T83 — 「对话」tab 体收缩为槽位挂载点**
  [前置] T79
  文件：`frontend/index.html`、`frontend/src/main.js`
  完成判据：`#tab-chat` 内只保留 `#chat-slot`（另有零个 `chat-bar`/`chat-log`/`chat-list`/`chat-empty`/`chat-form`/`chat-input`/`chat-send`/`chat-regen`/`chat-clear`/`chat-todos`/`chat-status` id）；`main.js` 里出现 `chatSlot: $('chat-slot')` 且 `applyState`/`switchTab` 路径按 `PluginsInstalled()` 解析槽位插件。

- [x] **T84 — 设置页新增「打开插件市场」入口按钮**
  [前置] T83
  文件：`frontend/index.html`、`frontend/src/main.js`
  内容：按钮 `id="open-plugin-market"` 放在「数据与退出」分组内（不新增 `.sgroup`），点击调 `App.OpenPluginMarket()`。
  完成判据：`grep -c 'id="open-plugin-market"' frontend/index.html` 为 1 且该行位于 `数据与退出` 分组的 `<section class="sgroup">` 与其 `</section>` 之间；`grep -c 'App.OpenPluginMarket()' frontend/src/main.js` 为 1。

- [x] **T85 — 市场程序的查找顺序与配音工坊逐项一致**
  [前置] T3
  文件：`app_plugin_market.go`（新建）、`app_plugin_market_test.go`（新建）
  内容：`PluginMarketCandidates()` 依次给出「exe 同目录 → `exe/../tools/plugin-market/build/bin` → `cwd/tools/plugin-market/build/bin`」三处路径（exe 名 `PetPCPluginMarket.exe`）；`PluginMarketPath()` 返回第一个存在的文件。
  完成判据：第三个候选路径的**后缀**为 `tools\plugin-market\build\bin\PetPCPluginMarket.exe`；在临时目录放置一个同名文件并把 exe 路径替换为该目录时，`PluginMarketPath()` 返回该文件。

- [x] **T86 — `OpenPluginMarket` 以独立进程启动且不等待、不关闭**
  [前置] T85
  文件：`app_plugin_market.go`、`app_plugin_market_test.go`、`app.go`
  内容：除绑定本体外，启动序列增加一个**进程层面**的触发钩子：环境变量 `PETPC_OPEN_PLUGIN_MARKET` 为 `1` 时调用一次 `OpenPluginMarket()`（照本仓既有的 `PETPC_DEBUG` 开门成例，`app.go:1467-1472` 同形；调用点放在托盘安装之后的启动序列里）。它只为**门禁可驱动性**存在（T123 需要一条不依赖页面控件的通道），不是用户功能：默认不设该变量时行为与现在完全一致，设置页按钮仍是唯一的产品入口。该钩子**不新增绑定、不新增菜单项**。
  完成判据：找不到 exe 时返回的错误含 `找不到插件市场程序（PetPCPluginMarket.exe）`；找到时调用 `OpenPluginMarket()` 后用假启动器断言启动了一次、`cmd.Dir` 等于 exe 所在目录、且函数返回**不等待**子进程结束（用假启动器记录返回时机）；`grep -c 'PETPC_OPEN_PLUGIN_MARKET' app*.go` 为 1，且该变量未设置时启动序列的假启动器调用计数为 0。

- [x] **T87 — 删除被插件取代的宿主对话前端模块**
  [前置] T83
  文件：`frontend/src/chat.js`（删除）、`frontend/src/live.js`（**本任务不动**，其删除归 T56——本任务在批次之前执行，此处只删 `chat.js`）、`frontend/src/main.js`
  完成判据：`frontend/src/chat.js` 不存在；`main.js` 中 `import { ChatTab }`/`import { ChatPopup }`/`new ChatTab`/`new ChatPopup` 零命中；`applyLayout` 仍保留 `pluginHost?.applyGeometry(layout.pluginSurface || null, ...)` 一行。

- [x] **T87b — 宿主侧已无 UI 入口的对话绑定一并移除**
  [前置] T56、T73、T87（`app_live.go` 必须先随 T56 删除——`app_llm.go:507/:616` 之外的 `llmView`/`llmReadinessNow` 调用点都住在那里；`pluginLLMChat` 的未就绪判定先由 T73 收敛为插件配置判定式，本任务才能删掉它依赖的 `LLMView`）
  文件：`app_llm.go`、`app.go`、`app_reminder.go`、`app_plugins.go`、`app_llm_test.go`、`app_llm_e2e_test.go`、`app_startup_test.go`、`app_plugins_test.go`
  内容：删除只服务于宿主对话 UI 的绑定与其测试：`SendChat`/`Regenerate`/`ClearChat`/`CancelChat`/`SaveLLMSettings`/`SetLLMKey`/`ClearLLMKey`/`TestLLMConnection`/`ResetLLMUsage`/`ChatIsOpen`/`ToggleChat`/`CloseChat`/`DebugLLMState` 及其视图构造（`LLMView`/`LLMSettingsInput`/`LLMTestResult`/`ChatMessageView`）。
  **`State.LLM` 与 `llmView` 的终态（本任务的改动面必须覆盖，否则其自身判据不可满足）**：`LLMView` 类型既然删除，产出它的 `llmView()`（`app_llm.go:208-257`）与只被 `llmView` 与 `app_live.go:616` 使用的 `llmReadinessNow()`（`:175-205`）**一并删除**（后者在 `app_live.go` 随 T56 整文件删除后已无第二个调用者）；随之删 `app.go:1158` 的 `State.LLM LLMView json:"llm"` 字段与 `:1218` 的 `LLM: a.llmView()` 赋值；`baseState()`/`GetState` 不再输出 `llm` 段。`pluginLLMChat` 的「未就绪」判定**改用 T73 的插件配置判定式**（缺端点/模型、缺密钥两类文案），不再依赖 `LLMView.Ready`/`NotReadyReason`。
  测试侧随之收敛：`app_llm_test.go` 中 `TestLLMViewNotReadyStates`/`TestLLMViewMasksTheKeyAndNeverRevealsIt` 等一切引用 `llmView`/`LLMView` 的用例删除（其断言的「掩码不出明文」「未就绪原因」两条语义由 T41/T43c/T73 在新承载上承接）；`app_llm_e2e_test.go:109`、`app_plugins_test.go:718`、`app_startup_test.go:45/:107` 的 `llmView`/`llmReadinessNow` 引用行一并删除。
   **`llmView`/`llmReadinessNow` 的其余调用点必须逐点写死终态**（否则删掉类型后本文件与 `app_plugins.go` 都编译不过，本任务判据不可满足）：
   - `app_llm.go:1126` 的 `DebugLLMState()`：它以 `v := a.llmView()` 组装诊断串，`llmView` 消失后**整族删除**——`DebugLLMState` 绑定与其测试一并删除（它只服务于「宿主对话设置对不对」这条已随之消失的诊断路径；插件侧诊断走 `PluginRequest` 的 `llm.error` 帧），并同步 T88/T103 的绑定清单（`App.js`/`App.d.ts` 不再导出 `DebugLLMState`，harness 与 `test-harness.js` 的 `methods` 白名单同步删掉该名）。
   - `app_llm.go:1141` 的 `logLLMStartup()`：**保留**（启动日志仍是用户报「对话不工作」时的第一手信息），但去掉 `view := a.llmView()` 与格式串里的 `view.KeySource`/`view.Ready` 两个参数，只留 `cfg` 侧字段（`cfg.LLMEnabled`/`cfg.LLMProvider`/`cfg.LLMBaseURL`/`cfg.LLMModel`/`cfg.LLMProactive`），并把 `if cfg.LLMEnabled && !view.Ready` 那条 `log.Printf` 一并删除，同处改注说明「就绪判定的唯一来源已是插件配置」。这样它与 T87c④ 的措辞一致：`logLLMStartup` 保留但**不再引用 `llmView`**。
   - `app_llm.go:496` 的 `startChat` 与 `:501` 的 `if view := a.llmView(); !view.Ready`：`startChat`/`runChat` 是**宿主对话引擎**（`SendChat`/`Regenerate`/`SpeakNow` 的唯一执行体），本任务已删掉它的全部宿主入口，因此这两族**整体删除**（`startChat`、`runChat`、`chatRequest`/`ChatStart` 类型与其 `Proactive` 字段，以及只被它们使用的 `supersede`/`requestCurrent`/`cancelChat` 与 `llmState` 的 `cancel`/`requestID`/`generation`/`reason`/`proactive` 字段）；`a.store.AppendChatMessage`/`AppendChat` 的**调用点**随之消失，但 `internal/store` 的转写存储**保留**（迁移的源与插件的历史落点仍需它，见 T58–T64、T76）。`pluginLLMChat` 不依赖这两族（它自建 `llm.Handlers` 与 `a.llmClient().Chat`），删掉后仍完整。
   - `app_llm_e2e_test.go` **整文件删除**：它的每一段都建立在 `SaveLLMSettings`/`llmView`/`SendChat`/`ChatHistory` 之上，宿主对话引擎删净后无可保留的对象；其「整条请求路径（配置→密钥→提示组装→流式→转写→用量）」的语义由 T77 的插件帧断言（`llm.delta` 若干 + `llm.done` 全文）与 T72/T74 的插件配置/用量断言在新承载上承接。
   - `app_llm.go:756` 的 `CancelChat` 与 `:821` 的 `ClearChat`、`:833` 的 `Regenerate` 同样随宿主对话引擎删除（三者在本条首段的绑定清单内）。
   - `app_plugins.go:809` 的 `view := a.llmView()`：由 T73 的插件配置判定式替换（本任务前置已含 T73）。
   - `app_reminder.go:464` 的 `a.cancelChat()`：`cancelChat` 随宿主对话引擎删除，该调用行同步删除（`shutdown` 余下步骤不变）。
  **保留**（与 T72/T77 的「不重写传输层」一致）：`internal/llm` 客户端（`app_llm.go` 的 `llmClient`）、`llmSettings`（`app_llm.go:415`）、`describeChatError`（`app_llm.go:445`）、`pluginLLMChat`（`app_plugins.go:790-870`）、`OpenChat`、`OpenTodoPanelFromChat`、`secretStore`（`app_llm.go:211` 的构造点随 `llmView` 删除后由 `pluginLLMChat` 侧的密钥读取路径承接）。（`SpeakNow` 不在此列：它由 T87c 删除，两条任务合起来才是「宿主侧不再有主动说话入口」。）
  完成判据：`grep -cE 'func \(a \*App\) (SendChat|Regenerate|ClearChat|CancelChat|SaveLLMSettings|SetLLMKey|ClearLLMKey|TestLLMConnection|ResetLLMUsage|DebugLLMState)\(' app_llm.go` 为 0；`grep -cE 'func \(a \*App\) (startChat|runChat|supersede|requestCurrent|cancelChat)\(' app_llm.go` 为 0；`grep -rn 'llmView\|LLMView\|llmReadinessNow' --include=*.go app*.go` 零命中；`grep -c 'State\.LLM\|json:"llm"' app.go` 为 0；`grep -c 'func pluginLLMChat' app_plugins.go` 为 1；`grep -c 'a\.cancelChat()' app_reminder.go` 为 0。
  **口径（与 T56/T87c 同一链，本任务不断言整包编译）**：本任务与 T56 同为链中环节，整包在链尾 T87c 落地前不可编译（理由见 T56 口径 (b)），因此本任务**不含** `go build ./...` 与 `go vet ./...`：这两条连同 `go test ./... -count=1` 唯一由 T87c 承接。

- [x] **T87c — 「主动说话」整套（调度 + 手动入口）随 `app_live.go` 删除**
  [前置] T87b、T56
  文件：`app_llm.go`、`app_llm_test.go`、`app.go`、`app_startup_test.go`、`app_proactive_test.go`（删除）
  内容（本任务与 T56/T87b 是同一终态的三段，T56 已删 `app_live.go` 整文件，T87b 已删宿主对话引擎与 `llmView` 一族，本任务删掉最后住在 `app_llm.go`/`app.go` 的主动说话那一段并与 T87b 合并收口）：
  ① 删 `app_llm.go` 的 `SpeakNow`、`speakProactive`、`rescheduleProactive`、`proactivePrompt`、`maxProactiveRunes`、`ProactiveView`、`proactiveView`。（`rescheduleProactive` 的唯一调用者 `SaveLLMSettings` 已由 T87b 整族删除，故此处不再有「删调用行」这一项——见 T87c③ 与 T87b 的绑定清单。）
  ② **已由 T87b 完成，此处只核对**：`startChat`/`runChat` 与 `chatRequest.Proactive`/`ChatStart.Proactive`/`a.llm.proactive`、事件载荷里的 `"proactive"` 键（`:646`、`:736`、`:751`）已随宿主对话引擎整族删除（见 T87b 的调用点清单）；本任务不再重复声明这一段的文件改动面——`app_llm.go` 的两条删除（对话引擎、主动说话）同属一个文件，T87b 与本任务**在同一文件上相邻落地**，编号保持既有后缀约定不变；
  ③ 删 `app.go` 的 `proMu`/`proLastSpoke` 字段（`:77-79`）与启动序列的 `a.initProactive()`/`a.startProactiveLoop()` 两行（`:276-277`）；
  ④ 删 `app_llm.go` 剩余的 `LLMProactive` 引用：`logLLMStartup` 的格式串与参数里的 `cfg.LLMProactive`（`:1144-1145`）。（`:70-72` 的 `llm` 运行时结构体 `proactive bool` 字段已随 T87b 的 `llmState` 字段收敛删除；`:155-167` 的「llmReadiness 打断递归」注释块随 `llmReadiness`/`llmReadinessNow` 整套由 T87b 删除——那份注释与 `proactiveView` 同生共死，不在本任务里重复处理。`logLLMStartup` 本身由 T87b 保留但已去掉 `llmView` 参数，本任务只再去掉其中的 proactive 字段。）
  ⑤ 删测试：`app_proactive_test.go` 整文件；`app_startup_test.go` 中 `{"initProactive", ...}` 用例（`:48`）与 `{"proactiveView", ...}` 用例（`:104`）及 `TestViewsWithProactiveOnDoNotRecurse`（`:68-140`，其断言对象 `proactiveView` 已不存在）；`app_llm_test.go` 中一切引用被删标识符的用例。
  立场（与 design §11 C8 同）：两个配置取值 `LLMProactive`/`LLMProactiveMinGapMin` 按 design §11 C3 **保留为只读迁移源**（随 T59 搬进对话插件配置，插件日后可用 `llm.chat` + `pet.bubble` 自行实现），但宿主侧不再有任何调度、手动入口或日志引用。
  完成判据（**本任务同时是整包编译与测试的唯一收敛点**）：`grep -rn 'tickProactive\|decideProactive\|proactiveView\|proactiveGap\|setNextProactive\|noteSpoke\|speakProactive\|startProactiveLoop\|initProactive\|maxProactiveRunes\|proactivePrompt\|ProactiveView\|SpeakNow\|proMu\|proLastSpoke\|req\.Proactive' --include=*.go .` 零命中（`_archive/` 亦无命中）；`grep -rn 'LLMProactive' --include=*.go .` 的命中只剩 `internal/store/{store.go,llm.go,llm_test.go}`（宿主侧 `app*.go` 为 0）；**`go build ./...`、`go vet ./...` 与 `go test ./... -count=1` 三条全部通过**。
  **口径（本任务是「宿主实时浮层 + 宿主对话引擎 + 主动说话」删除链的链尾，整包编译与测试唯一在此收敛）**：T56 删除 `app_live.go` 后留在 `app_llm.go` 的 proactive 引用（`rescheduleProactive` 对 `setNextProactive`/`proactiveGap` 的引用、`SpeakNow`/`speakProactive`、`chatRequest.Proactive` 家族）由本任务删除，因此**链尾之前的 T56/T57/T87b 都不断言 `go build`**（见 T56 口径 (b) 与 T87b 的同类口径）；**整包的 `go build ./...`、`go vet ./...` 与 `go test ./... -count=1` 三条判据唯一由本任务承接**。

- [x] **T87d — 删除宿主侧七个插件管理绑定及其测试**
  [前置] T47、T83、T87c
  文件：`app_plugins.go`、`app_plugins_test.go`、`app_plugin_surface_state_test.go`
  内容：删除 `PluginsView`/`MarketView`/`InstallPlugin`/`UninstallPlugin`/`SetPluginEnabled`/`GrantPluginPermissions`/`SaveMarketURL` 七个绑定与它们专属的视图类型（`PluginsView`/`MarketView`/`MarketEntry`），以及 `installedView`/`marketView`/`marketEntry` 三个仅供它们使用的私有助手；保留 `PluginRequest`/`OpenPluginSurface`/`ClosePluginSurface`/`PluginEventView`/`PluginsInstalled`/`pluginViewOf`/`PluginView`（`PluginView` 仍被 `PluginsInstalled` 使用）。测试侧：`app_plugins_test.go` 的 `installSample`/`readyPlugin`/`installSampleWith` 三个夹具改用 `plugins.NewManager(dir)` 直通调用（`Install`/`GrantPermissions`/`SetEnabled`）而非被删绑定；`app_plugin_surface_state_test.go` 中 `surfaceApp`/`TestDisableClosesTheSurface`/`TestUninstallClosesTheSurface` 同理改用管理器直通，断言的「关表面」语义逐条保留（改由 T87e 的几何守卫承接）。
  **最终态声明（与 design §5.1 同步）**：宿主侧不再暴露任何插件管理绑定——管理面只在 `PetPCPluginMarket.exe`，它直接调 `internal/plugins`。
  完成判据：`grep -cE 'func \(a \*App\) (PluginsView|MarketView|InstallPlugin|UninstallPlugin|SetPluginEnabled|GrantPluginPermissions|SaveMarketURL)\(' app_plugins.go` 为 0；`grep -c 'func (a \*App) PluginsInstalled(' app_plugins.go` 为 1（本任务排在删除链之后，故可断言整包编译：`go build ./...` 与 `go vet ./...` 通过）。

- [x] **T87e — 浮层面的「插件还有效才允许占据区域」守卫，替代被删绑定的关表面调用**
  [前置] T87d
  文件：`app_plugin_surface.go`、`app_plugin_surface_test.go`、`app_plugin_surface_state_test.go`
  内容：`closeSurfaceFor` 原本只被 `SetPluginEnabled(false)`/`UninstallPlugin` 调用；两个绑定删除后，守卫改为**每次几何计算时判定**（这正是 `app.go:799-804` 已有的手法：几何发现矩形全被裁掉时即 `setPluginSurfaceLocked(nil)`，让状态与 layout 一起归零）：把 `pluginSurfaceRects` 的可用性判据由「插件已装且 manifest 可读」收紧为「**已装 且 `Enabled==true` 且 `granted` 含 `pet.surface` 且 manifest 可读**」，`ok==false` 时几何分支走既有的 else 路径（只保留宠物矩形）并清掉 `pluginSurface` 与 `layout.PluginSurface`；`closeSurfaceFor` 随之删除（无调用者）。
  这条比原调用点更严：市场程序卸载/停用插件的下一个几何 pass 就会收起浮层面，**跨进程**同样成立，而原实现只在宿主自己调那两个绑定时才生效。
  完成判据：`TestDisableClosesTheSurface`/`TestUninstallClosesTheSurface` 改名后（如 `TestGeometryDropsTheSurfaceOfADisabledPlugin`/`TestGeometryDropsTheSurfaceOfAnUninstalledPlugin`）通过，且断言用「`plugins.NewManager(dir).SetEnabled(id,false)` / `.Uninstall(id)` 之后跑一次几何计算」驱动，`a.pluginSurface` 与 `a.layout.PluginSurface` 均为 nil；`grep -c 'closeSurfaceFor' app*.go` 为 0。

- [x] **T87f — `main.js` 删净设置页 AI 配置段的全部前端接线**
  [前置] T87b（**不前置 T92**：T92 属 K 组、按 §10 依赖 J 组，若反过来要求本任务等待 T92 就会形成 J→K→I→J 的环；本任务的终态用 `grep` 单独可核对，静态门禁全绿由 T133 在最后统一收口）
  文件：`frontend/src/main.js`
  内容（逐点写出终态，行号为改动前现文）：① 删 `:138-159` 的 21 个 LLM els 条目（`llmEnabled`/`llmProvider`/`llmProviderHint`/`llmBase`/`llmModel`/`llmKey`/`llmKeySave`/`llmKeyClear`/`llmKeyStatus`/`llmKeyFile`/`llmTest`/`llmTestStatus`/`llmTemp`/`llmMax`/`llmTimeout`/`llmHistory`/`llmBudget`/`llmBudgetReset`/`llmUsage`/`llmContext`/`llmPrompt`）与 `:191-195` 的 4 个主动说话 els 条目（`llmProactive`/`llmProactiveGap`/`llmProactiveStatus`/`llmSpeakNow`）；② 删 `:1174-1176` 的主动说话赋值与 `renderProactiveStatus()` 调用、`:1186-1187` 的 `state.llm?.keyFile` 引用、`renderProactiveStatus()` 函数本体（`:1212-1229`）；③ 删 `:1530-1543` 的 LLM 段 `renderSettings` 赋值；④ 删 `:1955` 的 `wireLLMSettings()` 调用、`llmSettingsPayload()`（`:1968-1984`）、`saveLLMSettings()`（`:1987-1989`）与 `wireLLMSettings()` 本体（`:1992-2099`，含 `App.SaveLLMSettings`/`App.SetLLMKey`/`App.ClearLLMKey`/`App.ResetLLMUsage`/`App.SpeakNow`/`App.TestLLMConnection` 六处绑定调用）。这不与 T92 重复：T92 改的是门禁脚本的 id 白名单，本任务改的是宿主前端对这些 id 的**读取与绑定**——少了本任务，T92 删除白名单后 `els.llmEnabled` 等仍会取到 `null` 并在 `renderSettings` 里抛错，harness 与 vitest 都无法全绿。
  完成判据：`grep -cE 'llmEnabled|llmProvider|llmBase|llmModel|llmKey|llmTemp|llmMax|llmTimeout|llmHistory|llmBudget|llmContext|llmPrompt|llmProactive|llmSpeakNow|llmTest|wireLLMSettings|llmSettingsPayload|saveLLMSettings|renderProactiveStatus' frontend/src/main.js` 为 0；`grep -cE 'App\.(SaveLLMSettings|SetLLMKey|ClearLLMKey|ResetLLMUsage|SpeakNow|TestLLMConnection)\(' frontend/src/main.js` 为 0。

## J. 生成产物（T88）

- [x] **T88 — Wails 生成产物与最终绑定/模型一致**
  [前置] T55、T84、T87b、T87c、T87d、T87f
  文件：`frontend/wailsjs/go/main/App.js`、`frontend/wailsjs/go/main/App.d.ts`、`frontend/wailsjs/go/models.ts`
  完成判据：`App.js` 导出 `OpenPluginMarket`/`PluginMarketPath`/`PluginsInstalled`/`OpenChatPluginSurface`，且不再导出 `OpenLive`/`CloseLive`/`PluginsView`/`MarketView`/`InstallPlugin`/`UninstallPlugin`/`SetPluginEnabled`/`GrantPluginPermissions`/`SaveMarketURL`/`SendChat`/`ClearChat`/`SaveLLMSettings`/`SetLLMKey`/`ClearLLMKey`/`TestLLMConnection`/`SpeakNow`/`ResetLLMUsage`/`DebugLLMState`（最后一项随 T87b 删除——它是宿主对话设置的诊断绑定，`llmView` 消失后无实现）；`App.d.ts` 中 `OpenPluginSurface`、`ClosePluginSurface`、`OpenChatPluginSurface` 均返回 `Promise<main.Layout>`；`models.ts` 含 `pluginSurface?: PluginSurfaceView;` 与 `ellipse` 字段、不含 `Layout.Live`。

## K. 门禁同步（T89–T126）

- [x] **T89 — 静态门禁的扫描文件列表与模块集合一致**
  [前置] T80、T87
  文件：`.dev/check-frontend.cjs`
  完成判据：该脚本的扫描集合 `scannedJs` **恰为 `[main.js, plugin-host.js]`**（两个宿主模块）；市场程序前端 `main.js` 由**另一个独立变量**读取（形如既有 `:1300` 的 `studioJS`），**不进入 `scannedJs`**；`grep -c "frontend/src/plugins.js\|frontend/src/chat.js\|frontend/src/live.js" .dev/check-frontend.cjs` 为 0。

- [x] **T89b — 删除 `check-frontend.cjs` 中依赖被删模块/被删事件的既有断言**
  [前置] T89、T56、T87、T87b
  文件：`.dev/check-frontend.cjs`
  内容（逐项写出终态；行号为改动前现文）：
  ① 删 `:75-84` 的 `liveParts`/`usedParts` 与「每个 data-part 都存在」检查（`live.js` 已随 T56 删除，两个集合都失去来源）；
  ② 删 `:141-145` 的 `ChatMessageView` 的 `proactive` 断言（该模型随 T87b 删除；`LiveRect`/`LiveView`/`ProactiveView` 的联合断言由 T96 处理）；
  ③ 删 `:187-193` 的 `pet:chat-delta/done/error/open` 四条「既发出又被订阅」断言与 `:194-198` 的 `pet:live` 断言，改为一条等价检查——`pet:plugin-event`、`pet:layout`、`pet:chat-open` 三个名字仍然「既发出又被订阅」（前两者由 `plugin-host.js` 与 `main.js` 订阅，`pet:chat-open` 由 `main.js` 订阅、由 `app_llm.go` 的 `OpenChat` 发出）；
  ④ 删 `:471-552` 整段 live.js 断言块（`:475-481` 的 `place(card/bar/puffs)`、`:482-493` 的「live.js 不自行设尺寸」、`:497-503` 的 `ChatPopup` 组件断言、`:504-511` 的 `#live` 挂载点断言、`:512-523` 的卡片无 chrome 断言、`:524-528` 的 `.chat-puff` 椭圆 CSS 断言、`:529-533` 的输入框不抢焦点断言、`:534-539` 的 `maxProactiveRunes` 断言——最后一条随 T87c 删除，`:542-552` 的 `computeLiveOverlay` 锚定断言随 T56 删除）；`.chat-puff` 的椭圆语义改由 T108（harness 的 3 个 `ellipse` 帧 `border-radius:50%`）承接；
  ⑤ 删 `:564-568` 的「`chat.js` 不自行 resize」断言（`chat.js` 随 T87 删除）；
  ⑥ 删 `:570-587` 的「主动说话」三条断言中依赖被删标识符的两条（`:573-577` 的 `llm-proactive`、`:578-582` 的 `llm-proactive-gap`；`:583-587` 的 `App.SpeakNow()` 由 T101 承接）；
  ⑦ 删 `:664-704` 中引用被删函数的四条（`:673-677` 的 `OpenLive` 守卫断言、`:697-704` 的 `a.bubbleAwaitingAnswer()` 读 `app_live.go` 断言），其中「浮层面拒绝占据提问槽位」的守卫断言改指向 `OpenPluginSurface`；`:678-696` 三条（拒绝文案、前端引导、Enter 处理器）保留；
  ⑧ 删 `:935-941` 的「宠物隐藏时不自言自语」断言（读 `app_live.go` 的 `petHidden` 分支，该文件已删）；
  ⑨ 删 `:151` 的 `goFiles` 数组中 `'app_live.go'` 一项（该文件已随 T56 删除；数组其余成员保留）；
  ⑩ 删 `:23` 的 `pluginsJs` 读取行（`frontend/src/plugins.js` 随 T80 删除，该 `readFileSync` 会直接抛错）与 `:27` 的 `scannedJs` 数组中的 `pluginsJs` 成员（数组收敛为 T89 的 `[mainJs, pluginHostJs]`），以及 `:177-178` 依赖 `pluginsJs` 的 `subscribedInPlugins` 集合与它的使用点（插件 tab 的 `EventsOn` 订阅随 tab 删除而失去来源；改用 `:177-178` 之外的既有订阅者变量，即 `mainJs`/`pluginHostJs` 的订阅集合）。
  完成判据：`node .dev/check-frontend.cjs` 退出码为 0 且无 FAIL 行；脚本中 `liveJs`/`liveParts`/`chatJs`/`pluginsJs`/`subscribedInPlugins`/`chat-puff`/`pet:live`/`pet:chat-delta`/`OpenLive`/`computeLiveOverlay`/`maxProactiveRunes`/`app_live.go` 十二类文本零命中。

- [x] **T90 — `newSections` 增删与最终 CSS 段标记一致**
  [前置] T87
  文件：`.dev/check-frontend.cjs`、`frontend/src/style.css`、`tools/plugin-market/frontend/src/style.css`
  完成判据：`newSections` 含 `/* --- plugin surface` 与 `/* --- plugin market`、不含 `/* --- plugins tab`；三处标记在对应 CSS 文件中各命中一次；既有「无 `box-shadow`/`filter`/`text-shadow`/`transform`」检查对两个前端各自的 CSS 均通过。

- [x] **T91 — 设置页分组数改为 5，LLM 子块断言删除**
  [前置] T79
  文件：`.dev/check-frontend.cjs`、`frontend/index.html`
  完成判据：脚本断言 `.sgroup` 恰为 5 且 `index.html` 中恰有 5 个 `<section class="sgroup">`；「LLM 分组三个子块」与「主动说话是独立分组」两条断言从脚本中删除（`grep -c "API Key\|主动说话" .dev/check-frontend.cjs` 为 0）。

- [x] **T92 — 设置页 id 白名单删 25 个 `llm-*`、增入口按钮 id**
  [前置] T84
  文件：`.dev/check-frontend.cjs`
  完成判据：`expectedSettingsIds` 中 `llm-` 前缀条目数为 0、含 `open-plugin-market`；条目总数等于 `index.html` 设置页实际 id 集合大小（脚本的 missing/extra 两条差集均为空）。

- [x] **T93 — tab 计数断言由 5 改为 4**
  [前置] T78
  文件：`.dev/check-frontend.cjs`
  完成判据：脚本断言 `(html.match(/data-tab="/g)||[]).length === 4` 且断言文案为 `todos/chat/records/settings`；`:1339` 处的计数断言同样为 4。

- [x] **T94 — 删除 `#tab-plugins` 系列断言，新增 `#chat-slot` 断言**
  [前置] T83
  文件：`.dev/check-frontend.cjs`
  完成判据：脚本中 `tab-plugins` 与 `tabPlugins` 文本零命中；新增两条断言——`htmlIds.has('chat-slot')` 与 `switchTab` 仍切换 `#tab-chat`。

- [x] **T95 — `layoutReturning` 集合与最终绑定一致**
  [前置] T88
  文件：`.dev/check-frontend.cjs`
  完成判据：集合含 `OpenPluginSurface`/`ClosePluginSurface`/`OpenChatPluginSurface`/`OpenChat`/`SetPanelOpen`/`TogglePanel`/`DismissBubble`，不含 `OpenLive`/`CloseLive`。

- [x] **T96 — `models.ts` 断言跟随模型变化**
  [前置] T88
  文件：`.dev/check-frontend.cjs`
  完成判据：保留 `pluginSurface?: PluginSurfaceView;` 断言、删除 `live?: LiveView` 断言；脚本中 `LiveView` 文本零命中（`LiveRect`/`LiveView`/`ProactiveView` 的旧联合断言一并改为只保留仍存在的模型名）。

- [x] **T97 — 事件发出者扫描的文件清单覆盖新增 Go 文件**
  [前置] T55、T85
  文件：`.dev/check-frontend.cjs`
  完成判据：`goFiles` 含 `app_plugins.go`、`app_plugin_market.go`、`app_chat_slot.go`、`app_plugin_migrate.go`、`app.go`、`app_llm.go`；「每个订阅事件都有 Go 发出者」检查通过（`pet:plugin-event`/`pet:layout`/`pet:chat-open` 三者都在）。

- [x] **T98 — 共享滚动条规则组去掉 `#tab-plugins` 且仍是共享定义**
  [前置] T79
  文件：`.dev/check-frontend.cjs`、`frontend/src/style.css`
  完成判据：规则组正则不再含 `#tab-plugins`（组内仍是 `.todo-list`/`.record-list`/`.chat-log`/`.voice-scroll`/`#tab-settings` 五个选择器）；「每条 `-webkit-scrollbar` 规则的选择器数 ≥ 4」检查通过。

- [x] **T99 — 「宠物没有配音工坊 tab」断言的计数与注释同步**
  [前置] T93
  文件：`.dev/check-frontend.cjs`
  完成判据：该断言为 `!htmlIds.has('tab-voice') && (html.match(/data-tab="/g)||[]).length === 4`，其注释不再出现「the fifth is the plugin tab」。

- [x] **T100 — 「密钥输入不回写」断言改指向市场程序前端**
  [前置] T17
  文件：`.dev/check-frontend.cjs`
  完成判据：断言读取 `tools/plugin-market/frontend/src/main.js`，要求其中密钥输入在保存后被清空且**没有**把状态里的密钥回填进输入的赋值；`frontend/src/main.js` 不再是该断言的读取对象。

- [x] **T101 — 「主动说话」视图相关断言移除**
  [前置] T91
  文件：`.dev/check-frontend.cjs`
  完成判据：脚本中 `llm-proactive`、`proactiveMinGapMin`、`SpeakNow()` 三条断言零命中。

- [x] **T102 — 新增「插件市场是独立程序」静态断言段**
  [前置] T3、T15、T19
  文件：`.dev/check-frontend.cjs`
  内容：照 §16 对配音工坊的断言形态新增 §17：`tools/plugin-market/go.mod` 的 module 与 replace、`wails.json` 的 `outputfilename`、市场前端 `main.js` 首行 import style.css、市场程序**产品代码**（`config.go`/`market.go`/`main.go`，显式排除 `*_test.go`）出现 `plugin-market.json`、且同一份产品代码里 `config.json` 零命中。
  完成判据：新增断言的 name 可在脚本中检索到（如 `the plugin market is a separate program keeping its own settings file`），且 `node .dev/check-frontend.cjs` 退出码为 0、输出出现该 PASS 行。
  **口径（与 T5 不冲突）**：`config.json` 的零命中**只**针对非测试的产品源码；`config_test.go` 里允许出现 `config.json` 作为「桌宠配置未被改写」的断言对象（T5、T10 的判据正是拿它做前后比对），因此扫描集合必须显式排除 `*_test.go`。

- [x] **T103 — harness 假绑定白名单与最终绑定集合一致**
  [前置] T88、T87d、T87c
  文件：`.dev/run-frontend-harness.cjs`
  完成判据：`methods` 数组含 `PluginRequest`/`OpenPluginSurface`/`ClosePluginSurface`/`PluginsInstalled`/`OpenPluginMarket`/`OpenChatPluginSurface`；不含 `PluginsView`/`MarketView`/`InstallPlugin`/`UninstallPlugin`/`SetPluginEnabled`/`GrantPluginPermissions`/`SaveMarketURL`/`OpenLive`/`CloseLive`/`SendChat`/`Regenerate`/`ClearChat`/`CancelChat`/`SaveLLMSettings`/`SetLLMKey`/`ClearLLMKey`/`TestLLMConnection`/`ResetLLMUsage`/`SpeakNow`/`DebugLLMState`（后十一项由 T87b/T87c 从 Go 侧删除，假桥接保留它们会让「多余方法」这一半的断言与真实绑定集合脱节）。

- [x] **T104 — harness 删除插件 tab 用例节**
  [前置] T103
  文件：`.dev/run-frontend-harness.cjs`
  完成判据：脚本中 `data-tab="plugins"`、`#tab-plugins`、`plugin-dialog`、`market-list`、`plugin-manage` 文本零命中；`node .dev/run-frontend-harness.cjs` 退出码为 0 且 PASS 行数比改动前少不超过被删节的用例数（无新增失败）。

- [x] **T105 — harness 断言 5 块矩形各得一个 iframe 且位置逐字相等**
  [前置] T103
  文件：`.dev/run-frontend-harness.cjs`
  完成判据：以含 5 块 `rects` 的 `pluginSurface` 驱动后，`#plugin-surface` 内 iframe 数为 5，且第 i 个 iframe 的 `style.left/top/width/height` 分别等于第 i 块矩形的 `x/y/w/h` 加 `px`（逐项字符串相等）。

- [x] **T106 — harness 断言每个浮层帧各收到一次带自身几何的 `init`**
  [前置] T105
  文件：`.dev/run-frontend-harness.cjs`
  完成判据：探针报告里恰好 5 条 `init`（每个帧一条），且第 i 条的 `surface.rectIndex === i`、`surface.rectCount === 5`、`surface.rect.x === 第 i 块矩形的 x`。

- [x] **T107 — harness 断言浮层帧的沙箱恰为 `allow-scripts`**
  [前置] T105
  文件：`.dev/run-frontend-harness.cjs`
  完成判据：5 个 iframe 的 `sandbox` 属性值逐字等于 `allow-scripts`（断言同时检查不含 `allow-same-origin`）。

- [x] **T108 — harness 断言椭圆块被画成圆形圆角**
  [前置] T105
  文件：`.dev/run-frontend-harness.cjs`
  完成判据：`ellipse:true` 的 3 个 iframe 的 `style.borderTopLeftRadius` 与 `borderBottomRightRadius` 均为 `50%`，非椭圆的 2 个 iframe 的对应值为矩形声明的 `r` 加 `px`。

- [x] **T109 — harness 断言事件按 `pluginId` 投递**
  [前置] T105
  文件：`.dev/run-frontend-harness.cjs`
  完成判据：投递一条 `pet:plugin-event` 给该浮层插件时，5 个帧各自收到一条同名事件；投递给另一个 id 时 5 个帧都**没有**收到（探针报告条数不变）。

- [x] **T110 — harness 新增设置页入口按钮用例**
  [前置] T84、T103
  文件：`.dev/run-frontend-harness.cjs`
  完成判据：点击 `#open-plugin-market` 后 `window.__calls` 中 `OpenPluginMarket` 恰好出现 1 次，且该次调用参数为空数组。

- [x] **T111 — harness 删除宿主对话 UI 用例**
  [前置] T87、T103
  文件：`.dev/run-frontend-harness.cjs`
  完成判据：脚本中 `#chat-log`、`#chat-form`、`#chat-input`、`ClearChat`、`SendChat`、`Regenerate`、`#live`、`chat-puff`、`.chat-cursor` 九类文本零命中；仍保留的对话入口是「切到 `#tab-chat`」本身（`data-tab="chat"` 四处在切换 tab 的导航语义下保留，`#chat-slot` 的断言保留）。

- [x] **T112 — harness 的「未获权限」断言改指 `plugin.config`**
  [前置] T47
  文件：`.dev/run-frontend-harness.cjs`
  完成判据：该用例让假 `PluginRequest` 抛出 `插件 com.example.beta 未获权限 plugin.config`，并断言插件侧看到的就是宿主原文（含「未获权限」）。

- [x] **T113 — `frontend/src/test-harness.js` 假桥接白名单一致**
  [前置] T103
  文件：`frontend/src/test-harness.js`
  完成判据：其 `methods` 列表与 `.dev/run-frontend-harness.cjs` 的 `methods` 列表**集合相等**（用脚本或测试直接比对两个列表）。

- [x] **T114 — `test-harness.js` 的状态与布局辅助跟随模型变化**
  [前置] T96
  文件：`frontend/src/test-harness.js`
  完成判据：`baseState()` 不含 `llm` 段与 `config.llmEnabled`；`layout()` 接受 `pluginSurface` 形参且返回对象含 `pluginSurface` 键、不含 `live` 键。

- [x] **T115 — 删除 `frontend/src/plugins.test.js`**
  [前置] T80
  文件：`frontend/src/plugins.test.js`（删除）
  完成判据：该文件不存在；在 `frontend` 下 `npx vitest run` 退出码为 0（剩余两个测试文件全绿）。

- [x] **T116 — 新增入口按钮的单测**
  [前置] T84、T114
  文件：`frontend/src/settings-market.test.js`（新建）
  完成判据：点击 `#open-plugin-market` 后调用日志里 `OpenPluginMarket` 恰好一次。

- [x] **T117 — 新增「无可用对话插件时槽位给提示」的单测**
  [前置] T50、T114
  文件：`frontend/src/settings-market.test.js`
  完成判据：`PluginsInstalled` 返回空数组时 `#chat-slot` 内无 `iframe` 且文本含「对话插件还没装好」。

- [x] **T118 — 新增「有可用对话插件时槽位挂 iframe」的单测**
  [前置] T51、T114
  文件：`frontend/src/settings-market.test.js`
  完成判据：`PluginsInstalled` 返回一行「启用 + 已授权 `pet.surface` + `panelSlot==='chat'`」时 `#chat-slot` 内出现一个 `iframe`，其 `sandbox` 为 `allow-scripts` 且 `src` 以 `/plugin-asset/<id>/` 开头。

- [x] **T119 — `check-all.ps1` 增加市场程序的 Go 门**
  [前置] T1
  文件：`.dev/check-all.ps1`
  完成判据：脚本中新增一个门，在 `tools/plugin-market` 目录下执行 `go vet ./...` 与 `go test ./... -count=1`；`pwsh -File .dev\check-all.ps1` 的 summary 中该门显示 PASS。

- [x] **T120 — `check-all.ps1` 的前端构建门覆盖两个前端**
  [前置] T15
  文件：`.dev/check-all.ps1`
  完成判据：前端构建门依次在 `frontend` 与 `tools/plugin-market/frontend` 下各跑一次 `npm run build`；两个前端任一失败时该门为 FAIL。

- [x] **T121 — `.dev/verify-region.ps1` 一字不改**
  [前置] T79
  文件：`.dev/verify-region.ps1`
  完成判据：`git diff --stat .dev/verify-region.ps1` 输出为空；该脚本在有交互桌面时仍 PASS。

- [x] **T122 — `.dev/verify-live.ps1` 的判据改由对话插件的声明矩形驱动**
  [前置] T55、T105
  文件：`.dev/verify-live.ps1`
  内容：保留原有全部判据形态（窗口不 resize、窗口不移动、宠物不移动、卡片在宠物上方、输入条紧贴宠物下方、第一个泡泡中心被绘制且**包围盒角点露桌面**），几何来源改为读 `market/plugins/com.shixiansi.plugin-chat/manifest.json` 的 `surface` 前 3 块。
  完成判据：脚本在有交互桌面时 PASS；其中「角点露桌面」判据在 `ellipse:true` 的泡泡上成立；无交互桌面时按既有约定输出说明并 `exit 0`。

- [x] **T123 — `.dev/verify-plugin-surface.ps1` 的探针与服务面适配新形态**
  [前置] T55、T86
  文件：`.dev/verify-plugin-surface.ps1`
  内容：种子 manifest 的 `surface` 由 1 块改为 5 块（含一个 `ellipse:true`）；驱动方式由「点第 4 个 tab 的坐标」改为**不依赖页面控件**的路径——本环境已记录「合成点击能到达桌宠窗口但到不了页面里的控件」（`README.md:977`、`.dev/verify-plugin-surface.ps1:29`），因此「打开设置页 → 点 `#open-plugin-market`」这一步不作为门禁的驱动方式。
  **驱动机制（本任务已定死，不留待执行环节猜）**：脚本以种子数据目录（`$env:PETPC_DATA_DIR`）启动桌宠，并用一个**进程层面**的触发通道调起市场程序——本仓既有「用环境变量开门」的成例（`PETPC_DATA_DIR`：`internal/store/store.go:457`；`PETPC_DEBUG`：`app.go:1467-1472`；`PETPC_TTS_E2E`：`app_voice_e2e_test.go:29`），因此脚本设 `PETPC_OPEN_PLUGIN_MARKET=1` 后启动桌宠，由启动序列在**该变量存在时**调用一次 `OpenPluginMarket()`。这不新增菜单项、不新增绑定（`OpenPluginMarket` 由 T86 建立），只把「入口按钮被点击」这一动作换成同一函数在进程启动时的等价调用，因此断言的对象（`PetPCPluginMarket.exe` 进程出现）与产品路径完全一致。
  **该环境变量本身由 T86 落地为可核对终态**（见 T86 的内容与判据），本任务只消费它。另：脚本**不得**改用托盘菜单或全局快捷键作为驱动（两者都需要新增产品代码，超出本 change 范围）。
  完成判据：脚本在有交互桌面时 PASS，其中新增断言要求「设置 `PETPC_OPEN_PLUGIN_MARKET=1` 启动桌宠后 3 秒内存在名为 `PetPCPluginMarket` 的进程」；无交互桌面时按既有约定输出说明并 `exit 0`；脚本中不再出现「点设置页控件的页面坐标」这一驱动步骤（`grep -c 'open-plugin-market' .dev/verify-plugin-surface.ps1` 为 0）。

- [x] **T124 — 新建 `.dev/verify-plugin-market.ps1`：窗口是带框可缩放且初始 980×700**
  [前置] T4、T19
  文件：`.dev/verify-plugin-market.ps1`（新建）
  完成判据：启动 `build\bin\PetPCPluginMarket.exe` 后，其窗口样式含 `WS_THICKFRAME` 与 `WS_MAXIMIZEBOX`（可缩放）、且客户区尺寸约为 980×700（±16px 容差）；无交互桌面时输出说明并 `exit 0`。

- [x] **T125 — 市场程序的权限勾选行在真实窗口里不越界**
  [前置] T124
  文件：`.dev/verify-plugin-market.ps1`
  完成判据：脚本指出权限勾选行所在的详情列矩形，并在该矩形**右边界内侧 2px 的竖条**上采样像素，断言那里是窗口背景色而不是文字像素（即文字未越过右边界被裁）；无交互桌面时按约定 `exit 0`。

- [x] **T126 — 市场地址输入框的可见宽度不小于容器的 90%**
  [前置] T124
  文件：`.dev/verify-plugin-market.ps1`
  完成判据：脚本用与 `T125` 同一套像素扫描找出顶部输入框的可视边框，断言其宽度 ≥ 容器宽度的 90% 且其右侧与保存按钮在同一水平带内（按钮未换行）。

## L. 文档与端到端冒烟（T127–T132）

- [x] **T127 — README 的插件章节改写为「管理面在独立程序里」**
  [前置] T84
  文件：`README.md`
  内容：除改写入口说明外，**权限文档面同步为十项**：`README.md:248` 的小标题「### 九项权限」改为「### 十项权限」，`:250-260` 的权限表新增一行 `| \`plugin.config\` | 保存你自己的插件配置（宿主存在插件私有目录里，插件之间互相读不到） |`，`:269-270` 的浮层面说明把「矩形上限」口径同步（4→8）；同文件 `:267` 「占满「插件」tab 里那块…」一句必须改写（该 tab 已随 T79 删除，插件面板视图改挂 `#chat-slot` 槽位）。
  完成判据：`grep -c '九项权限' README.md` 为 0；该权限表恰含 10 行权限且其中一行以 `plugin.config` 开头；`grep -c '插件」tab' README.md` 为 0；新增段落说明「插件管理在 `PetPCPluginMarket.exe`，桌宠设置页的『打开插件市场』是唯一入口」，并列出 `build/bin` 下三个 exe。

- [x] **T127b — `capability.go` 的「nine / 九项」措辞同步为十项**
  [前置] T21
  文件：`internal/plugins/capability.go`、`app_plugins_test.go`
  内容：`capability.go:8` 的 `// The nine permissions, fixed.` 与 `:54` 的 `// IsAllowedPermission reports whether name is one of the nine.` 两处措辞随第 10 项权限同步；`app_plugins_test.go:858` 的 `// uses must be EXACTLY the nine the manifest validator accepts.` 同样同步。注释与断言数量都指向「固定十项」，留一处「nine」会让后来者以为清单仍是九项。
  完成判据：`grep -c 'nine' internal/plugins/capability.go app_plugins_test.go` 为 0；`len(plugins.AllowedPermissions) == 10`；`go test ./internal/plugins/ -count=1` 与 `go test . -run TestCapabilityTable -count=1` 通过。

- [x] **T128 — README 补端到端冒烟清单（人工可复现）**
  [前置] T127、T65
  文件：`README.md`
  完成判据：清单含七步且顺序为「设置页点打开插件市场 → 独立程序启动并读到已装插件 → 浏览市场列表 → 给对话插件填 AI 配置并保存 → 桌宠侧双击宠物发一条消息走通流式 → 关掉对话插件后浮层不可用且面板槽位给出提示 → 确认旧对话记录与用量已被迁移」；每步都有可观察结果一句话。

- [x] **T129 — 新建 `docs/plugin-market.md` 并记录搬迁前后的对比结论**
  [前置] T126
  文件：`docs/plugin-market.md`（新建）
  完成判据：文档含「为什么搬出去」一节，逐条记录截图四项问题（权限行被右边界切断、地址框只剩半截、对话框与两个列表争抢同一块滚动区、可点击区域与提示文字尺寸不匹配）与搬迁后的结论（权限行完整可见可换行、地址框占满可用宽度、列表与详情分区滚动、对话框不溢出被裁）。

- [x] **T130 — `docs/llm-chat.md` 补现状说明（配置归插件、传输留宿主）**
  [前置] T72
  文件：`docs/llm-chat.md`
  完成判据：§8.0 的表格后新增一段，说明「对话仍是面板里的一个槽位，但其内容自本 change 起由对话插件渲染；AI 配置与密钥归插件、编辑入口在插件市场；宿主的 `internal/llm` 与 `llm.chat` 帧协议未变」；§6.1 描述的设置页控件位置标注为已迁出。

- [x] **T131 — `plugin-system/design.md` 的旧前提标注【已调整】**
  文件：`openspec/changes/plugin-system/design.md`
  完成判据：第 11–12 行所在表格行的「本设计的遵守方式」一列含【已调整】字样与「本前提由 `openspec/changes/plugin-market-app-and-chat-plugin` 推翻」的指向；其余行文本未变。

- [x] **T132 — `plugin-system/proposal.md` 的旧前提标注【已调整】**
  [前置] T131
  文件：`openspec/changes/plugin-system/proposal.md`
  完成判据：`:59` 与 `:95` 两处含【已调整】字样并指向本 change；`README.md` 的模块清单（`:830-874`）中 `frontend/src/plugins.js` 一行改为指向市场程序、`app_plugins.go`/`app_plugin_surface.go` 两行描述更新为「含配置能力与配置化白名单」/「浮层矩形上限 8」。

## M. 收尾门禁（T133–T135）

- [ ] **T133 — 全套门禁在最终源码上全绿**
  [前置] T126、T132
  文件：（无新增；执行）
  完成判据：`pwsh -File .dev\check-all.ps1` 退出码为 0，summary 中 `go vet`、`go test`、市场程序 Go 门、两次前端构建、静态接线检查、行为 harness、`wails build`、两个真实窗口像素门禁全部 PASS（无交互桌面时两个像素门禁按既有约定报「无交互桌面」而不计失败）。

- [x] **T134 — 两个程序的构建产物都在 `build\bin`**
  [前置] T133
  文件：（无新增；执行）
  完成判据：`pwsh -File tools\build-all.ps1` 退出码 0；`build\bin` 下 `PetPC.exe`、`PetPCVoiceStudio.exe`、`PetPCPluginMarket.exe` 三个文件都存在且修改时间晚于本次构建开始时间。

- [x] **T135 — 宿主传输层与配音工坊零改动**
  [前置] T133
  文件：（只读核对）
  完成判据：`git diff --stat -- internal/llm/ tools/voice-studio/` 输出为空；`go.mod` 与 `go.sum` 也未被修改（`git diff --stat -- go.mod go.sum` 为空）。

## N. 需求对齐映射（澄清清单 / 规格卡任务项 ↔ 本计划任务）

### N.1 澄清问答（Q1–Q10 的每个要求 → 任务）

| 澄清要求（原文要点） | 计划任务 |
|---|---|
| Q1：面板彻底删整个插件 tab；tab 由 5 回到 4（待办/对话/记录/设置）；桌宠只留一个入口按钮且放在设置页内（不在 tab 栏、不新增顶级 tab）；点击启动 `PetPCPluginMarket.exe` | T78、T79、T80、T81、T82、T84、T86 |
| Q2：程序名 `PetPCPluginMarket.exe`、窗口标题「PetPC 插件市场」；产物复制到 `build/bin`（与 `PetPCVoiceStudio.exe` 同级）；普通带框可缩放窗口 980×700 + 合理 MinWidth/MinHeight；`Frameless=false`、`AlwaysOnTop=false`、不透明背景；布局＝左列表（已装 + 市场）+ 右详情/权限/配置/安装；顶部地址框 + 刷新；独立程序保留「已装插件」区块 | T3、T4、T7、T7c、T8、T8b、T9、T9b、T10、T15、T16、**T18b（刷新控件）**、T19、T124 |
| Q3：插件在 manifest 声明配置项（key/标签/类型/默认值），由市场程序提供编辑界面，落插件私有目录；对话插件自己的 LLM 参数与密钥都在市场程序里编辑；桌宠设置页不再提供 AI 配置入口 | T24–T29、T34–T42、T43b、T43c、T11、T12、T92 |
| Q4：两个程序读写同一份数据目录（`%AppData%\PetPC`，支持 `PETPC_DATA_DIR`）；市场程序写同一套文件（`plugins.json`、`plugins/<id>/`、`plugin-data/<id>/`）；桌宠检测文件变化自动重读（沿用 reloadIfChanged 做法）；两进程互不阻塞；插件本体运行时界面仍只长在桌宠裁剪矩形内 | T5、T7、T14、T48、T86、T55、T51 |
| Q5 / Q10：交付边界＝①新建市场程序 ②面板删插件 tab + 设置页入口 ③新增插件配置能力（manifest 声明 + get/set + 第 10 项权限）④对话插件功能对等 ⑤宿主 AI 配置迁移 + 设置页 AI 段移除 ⑥门禁同步 ⑦市场 zip 升版并重算 sha256 | ①A 组、②T78–T84、③T21–T46、④G 组、⑤F 组 + T92、⑥K 组、⑦T69–T71 |
| Q6：插件自己声明 LLM 配置项（端点/模型/温度/系统提示词/自己的密钥），密钥按插件隔离存储；请求仍经宿主 `llm.chat`；宿主只当带信封，不再持有 AI 配置；设置页删掉整个 AI 配置段；保留 `internal/llm`，不重写 SSE/重试/错误翻译/delta 合并/预算统计 | T65、T72、T73、T74、T75、T77、T91、T92、T135 |
| Q7：面板保留「对话」tab 且内容改由对话插件渲染（宿主提供面板槽位机制）；双击宠物的浮层面也由对话插件渲染；`maxSurfaceRects` 由 4 提到 8（3 泡泡 + 卡片 + 输入条 = 5，留余量），并同步单测与文档 | T30、T31、T32、T49、T50、T51、T52、T53、T54、T55、T65 |
| Q8：首次启动迁移一次——13 个 `llm*` 值 + `llm-history.json` + `llm-usage.json` + `llm-key.bin` 搬进 `plugin-data/com.shixiansi.plugin-chat/`；原文件改名 `.bak` 保留；幂等（第二次不重复搬） | T58、T59、T60、T61、T62、T63 |
| Q9：给 `net.fetch` 加「配置化白名单」——白名单来自插件声明的某个配置项；用户填端点后宿主只放行该配置项中的域名；仍限 http/https、仍精确匹配、不做后缀通配 | T44、T45、T46、T29（T29 只约束「至多一项」；对话插件走宿主 `llm.chat` 不经 `net.fetch`，故其 manifest 不声明 `net.fetch`——见 T65） |
| Q10 的附加项：保留 `internal/llm` 作传输层供 `llm.chat` 使用；不删除宿主对话历史与用量模块（随配置迁给插件使用） | T72、T76、T135 |
| 截图四项判据：①权限勾选行溢出被裁 ②地址框只剩半截、保存按钮另起一行 ③对话框与两个列表争抢同一滚动区 ④可点击区域与提示文字尺寸不匹配 | T17、T18、T16、T125、T126、T129 |

### N.2 规格卡 testing 第 1–10 条 → 任务

| testing 条目 | 计划任务 |
|---|---|
| 1) `go vet`/`go test` + `internal/plugins` 新增单测（config 校验、读写往返与原子写、secret 不明文、`config.get/set` 权限门控、配置化白名单四类、上限 8 边界、迁移幂等/失败/密钥隔离）＋ `app_plugins.go` 绑定层测试 | T23、T25–T33、T35–T43c、T46、T58–T65、T133 |
| 2) 市场程序独立单测（配置文件与桌宠 `config.json` 分离、跨进程重读、启动路径查找顺序） | T5、T6、T14、T85 |
| 3) 桌宠前端与市场前端分别 `npm run build` | T15、T120 |
| 4) `node .dev/check-frontend.cjs` PASS 且硬编码清单同步 | T89、**T89b（既有断言删改）**、T90–T102 |
| 5) `node .dev/run-frontend-harness.cjs` PASS（删第 18 节、第 19 节适配 5 块、假绑定与假 Layout、入口按钮用例） | T103–T112 |
| 6) 前端单测（vitest/jsdom）删旧用例、增入口按钮与槽位渲染用例 | T114–T118 |
| 7) 两个程序的构建链接；`verify-region.ps1`/`verify-live.ps1` 仍 PASS；`verify-plugin-surface.ps1` 适配 | T19、T121、T122、T123、T124 || 8) geometry_gate 与 sandbox_gate（并集＝宠物 ∪ 声明矩形；iframe 位置逐字等于 Layout；沙箱恰 `allow-scripts`） | T53、T105、T106、T107、T122 |
| 9) 对话不可回归（双击弹浮层、流式返回、历史来自插件私有存储、用量仍累加） | T55、T67、T72、T74、T76、T77、T122 |
| 10) 端到端冒烟写进 README | T128 |

### N.3 澄清清单 `doubts` / `risks` / `placeholders` → 任务

| 条目 | 计划任务 |
|---|---|
| doubt 1（配置化白名单不改「请求仍由宿主发起」的立场，不存在 CORS 风险） | T45、T46（断言均通过宿主能力发请求，插件侧无 `fetch`） || doubt 2（宿主保留历史/用量/预算/传输层模块，但配置数据归插件；宿主不再自动向旧路径写新对话） | T74、T76、T135 |
| doubt 3（新增第 10 项权限后，权限数量断言与两处标签映射同步） | T21、T22、T23、T91、T92、**T127（README「九项权限」文档面）、T127b（`capability.go` 的 nine 措辞）** |
| risk 1（对话搬迁回归面极大） | T72、T77、T111、T122、T128、T135 |
| risk 2（浮层上限 4 不足） | T31、T65 |
| risk 3（配置化白名单可能被利用为任意域名放行） | T44、T46 |
| risk 4（迁移数据丢失风险） | T58–T64 |
| risk 5（多处硬门禁同时被撞击） | T89、T89b、T90–T126（逐条独立成任务） |
| risk 6（旧前提「不新起独立程序」需显式推翻） | T131、T132 + design.md §1 表、proposal.md §1.2 |
| placeholder 1（远端市场不可达回退内置样例源） | T7c |
| placeholder 2（升版包与两处目录的 sha256 必须实算、两处 zip 字节一致） | T69、T71 |
| placeholder 3（旧密钥经 SecretStore 读出后按插件隔离重加密；插件不存明文；未配置时明确提示） | T62、T73 |

### N.4 反向核对（本计划任务 → 依据）

本文件每一条任务都已在 §A–§M 的「文件/内容/完成判据」里写出依据，均可回溯到：
① 规格卡 `impl_decisions` 的 16 条（逐条落在 A–I 组）；② 规格卡 `testing` 第 1–10 条（N.2）；
③ 澄清清单 Q1–Q10 与截图四项判据（N.1）；④ risks/doubts/placeholders（N.3）。
无任何任务超出上述来源：**A 组**全部来自 Q2/Q5/Q10 与 `impl_decisions` 第 1–4 条（其中 **T18b 的「刷新控件」直接来自 Q2 原文「顶部市场地址输入框 + 刷新按钮」与规格卡 solution【一】的「顶部市场地址输入框与刷新」**；T7c/T8b/T9b 是同一绑定面的独立接线点，由本文件第 5–7 行的粒度规则拆出）；
**B/C/D 组**来自 `impl_decisions` 第 7–10 条；
**E 组**来自第 12–14 条与 Q7；**F 组**来自第 11 条与 Q8；**G 组**来自第 12 条与 Q6/Q10；**H 组**来自第 15 条与 Q6；
**I 组**来自第 5–6 条、第 15 条与 Q1（其中 T87b/T87c 来自 design §11 C7/C8 对「25 个 `llm-*` 控件整体移除」与「主动说话失去配置来源」的裁定：删掉没有入口的绑定、删净没有配置源的调度；**T87b 的改动面在本轮扩到「宿主对话引擎整族」（`startChat`/`runChat`/`chatRequest`/`ChatStart`/`supersede`/`requestCurrent`/`cancelChat`/`CancelChat`/`ClearChat`/`Regenerate`/`DebugLLMState`/`llmState` 的对话字段与 `State.LLM`/`llmView`/`llmReadinessNow`）并连带删除 `app_llm_e2e_test.go` 与 `app_reminder.go:464` 的 `cancelChat()` 调用，理由是这些标识符的唯一使用者就是被删的宿主对话 UI/引擎，不删净则 T87b 自身的 `grep` 零命中判据不可满足（对齐 design §4.1 的「类型与判定都换掉」）；**T56/T57/T87b/T87c 的整包编译与测试判据统一落在链尾 T87c**，因为它们构成一段不可编译的连续批次（见 T56 口径 (b)(c) 与 design §4.5）；**T87d/T87e** 来自第 5 条「面板删除插件 tab」与第 6 条「管理面只在独立程序」的必然推论——管理口子搬走后宿主不再需要七个管理绑定，但「插件没了它的浮层面不能继续占区域」这条既有不变量按新承载保留；**T87f** 是同一裁定在宿主前端的另一半——规格卡 `impl_decisions` 第 15 条「设置页 AI 配置段随之移除」要求的是**界面真的没有这段**，而门禁脚本的 id 白名单（T92）删完 25 个 id 之后，`main.js` 里对这些 id 的读取与绑定仍在，`renderSettings` 会因 `els.llmEnabled === null` 抛错，T92 与 T133 的全绿判据都到不了；**T86 的 `PETPC_OPEN_PLUGIN_MARKET` 钩子**来自规格卡 `testing` 第 7 条「真实窗口门禁必须仍然 PASS」与 `.dev/verify-plugin-surface.ps1:24-37` 已记录的环境限制（合成点击到不了页面控件），它为 T123 提供一条**不新增产品入口**的驱动通道，形态照本仓既有的 `PETPC_DEBUG`/`PETPC_LLM_API_KEY` 开门成例）；**J 组**是生成产物的必然伴随项（`impl_decisions` 第 1 条要求自有 wailsjs、第 6 条要求新增绑定）；
**K 组**来自第 16 条（门禁同步；其中 **T89b** 是「既有断言依赖被删模块/事件」的定点修复：T89 删掉 `readFileSync` 行之后，这些断言会因变量不存在而让整个脚本抛错，所以它们必须有一条自己的终态；**T102 的零命中只扫非测试产品代码**，因为 `config_test.go` 必须以 `config.json` 为断言对象来证明桌宠配置未被改写——见 T5、T10）；**L/M 组**来自 `testing` 第 7/10 条与 risk 6（其中 **T127/T127b** 来自 doubt 3「权限数量断言与标签映射同步」的文档面：README:248 与 `capability.go:8/:54` 至今写着「九项/nine」，权限表改到十项后这些措辞必须一起改，否则文档与代码互相矛盾）。

**本计划不包含任何「连接测试」任务**：T13（市场程序 `TestEndpoint`）已删除，理由与范围外第 11 条一致，反查在此落号。
