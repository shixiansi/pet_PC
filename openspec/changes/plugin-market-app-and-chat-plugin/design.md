# design.md — plugin-market-app-and-chat-plugin

> 本文件把规格卡 `sc_3_mv0pt51q` 的 `impl_decisions` 收敛为**唯一路径**，不留未定分支。凡规格卡给了多个选项或不完整口径的地方，
> 见 §11 的收敛决定（C1–C9）与实际取值裁定（D1–D6）。
> 任务编号与 §10 依赖顺序、`tasks.md` 一一对应；`tasks.md` 是本设计唯一的执行清单。
> 全文事实均来自实读本仓文件（行号即出处）；凡未实读的路径一律不出现在本设计中。

## 1. 硬约束与设计边界（全部来自已实读的仓库事实）

| 约束 | 事实出处 | 本设计的遵守方式 |
|---|---|---|
| 窗口**永不 resize**，只改区域裁剪 | `app.go:510-531`（`totalW = petW + gap + panelW`）、`app.go:690-750`（region switch）、`README.md:475-567` | 对话浮层**只**进 `case pluginSurface != nil:` 分支（`app.go:716-740`），不调用任何 `SetBounds`/尺寸变更；`main.go:52-54` 的 `Width/Height/DisableResize` 一字不动 |
| 面板矩形固定 380×520 | `internal/store/store.go:225-235`、`README.md:276` | 「对话」tab 的 380×520 矩形**原样**交给插件 iframe（面板槽位），不新增窗口几何 |
| Wails v2 无多窗口 API；类名 `wailsWindow` 唯一 | `internal/desktop/desktop_windows.go:21`、`docs/llm-chat.md:203-222` | 市场程序是**另一个进程**（不是第二个窗口）；桌宠自身仍是唯一 `wailsWindow`；插件本体运行时 UI 不进市场程序 |
| 区域必须等于「页面真正绘制的东西」 | `README.md:541-567`、`app_plugin_surface.go:43-61` | 浮层矩形由 `computePluginSurface` 算出**一次**，同时用于 region 与 `PluginSurfaceView`；前端绝不自行推导 |
| 区域只支持矩形并集（可圆角）+ 椭圆 | `internal/desktop/desktop.go:38-48`（`RegionRect.Ellipse`）、`internal/desktop/desktop_windows.go:337-389`（椭圆裁剪实现） | 三个思考泡泡沿用 `RegionRect.Ellipse`；插件声明矩形新增 `ellipse` 标志（§11 C4） |
| 四态互斥（面板/气泡/浮层/插件浮层面） | `app.go:977-1001`、`app_pomodoro.go:1069-1071`、`app_plugin_surface.go:206-267` | 对话浮层走既有 `OpenPluginSurface`，其守卫（未回答提问时**拒绝且不改状态**）现有代码已有（`app_plugin_surface.go:221-224,355-364`） |
| 设置页控件白名单（7 个 `.sgroup`、id 集合全等） | `.dev/check-frontend.cjs:253-361` | 删两组 LLM（7→5）、删 25 个 `llm-*` id、增 1 个 `open-plugin-market` id（§6.4） |
| 权限词表 9 项固定，调度表与词表逐项对应 | `internal/plugins/capability.go:42-52`、`internal/plugins/manifest_test.go:155-174`、`app_plugins_test.go:863-881` | 增第 10 项后三处一并改（T21、T22、T23），**文档面的「九项/nine」也一并改**（`README.md:248`、`capability.go:8`/`:54`、`app_plugins_test.go:858` → T127、T127b） |
| `net.fetch` 必须声明白名单，`HostAllowed` 精确匹配、不做后缀通配 | `internal/plugins/manifest.go:177-188`、`capability.go:106-125` | 配置化白名单是**追加**来源，静态规则一字不改（T44、T45、T46）；`hostWhitelist` 声明的「至多一项」不要求声明 `net.fetch`（§3.2，T29） |
| Go 1.22 / wails v2.11.0，`internal/plugins` 只用标准库且不 import `internal/desktop` | `go.mod:1-10`、`internal/plugins/manifest.go:9-13` | 新增的密钥存储走**注入的 `SecretKeeper` 接口**，不新增 import、不新增第三方依赖 |
| 工具链须 dot-source；构建入口 `npm run build` → `wails build -s` | `.dev/env.ps1:1-41`、`wails.json:5-6`、`README.md:12-24`、`tools/build-all.ps1:18-35` | tasks.md 全部命令判据使用既有入口，不发明新命令 |
| 第二个 Wails 程序的模板形态 | `tools/voice-studio/go.mod:1-42`、`tools/voice-studio/wails.json:1-16`、`tools/voice-studio/main.go:42-68`、`tools/voice-studio/studio.go:100-102`、`tools/voice-studio/frontend/vite.config.js:1-19`、`tools/build-all.ps1:59-70` | 市场程序逐项照此（§2） |
| 启动独立程序的既有写法 | `app_voice_studio.go:91-160`（`studioCandidates` + detached + 记录 `launched`） | `OpenPluginMarket` 照此（§5） |

## 2. 交付一：独立插件市场程序 `tools/plugin-market/`

### 2.1 模块与构建形态（照搬 `tools/voice-studio`，零复制）

```
tools/plugin-market/
  go.mod          module PetPC/tools/plugin-market；go 1.22.0；toolchain go1.22.12；
                  require PetPC v0.0.0 + github.com/wailsapp/wails/v2 v2.11.0；replace PetPC => ../..
  go.sum          与 tools/voice-studio/go.sum 同字节（同一组依赖）
  wails.json      name/outputfilename = PetPCPluginMarket；productName「PetPC 插件市场」
  main.go         options.App{Title:"PetPC 插件市场", Width:980, Height:700,
                  MinWidth:720, MinHeight:520, DisableResize:false, Frameless:false,
                  AlwaysOnTop:false, BackgroundColour 不透明, AssetServer, Bind: market}
  market.go       绑定的宿主结构
  config.go       自己的配置文件（§2.3）
  frontend/       Vite 4.5.5、base './'、minify:false；index.html/src/main.js/src/style.css；wailsjs/**
```

- **零复制复用**：`market.go` 只 import `PetPC/internal/plugins`（`NewManager`、`Market`、`Install`、`Uninstall`、
  `SetEnabled`、`GrantPermissions`、`Pending`、`AllowedPermissions`、`PermissionLabels`、`LoadManifest`）
  与 `PetPC/internal/store`（`DefaultDir`）。安装/校验/授权/启停/卸载**没有第二份实现**。
- 数据目录 `store.DefaultDir()`（`%AppData%\PetPC`，支持 `PETPC_DATA_DIR`，`internal/store/store.go:454-491`），
  与桌宠**完全共享**；`Manager` 无内存缓存（`internal/plugins/manager.go:76-96`），因此每次读盘即最新。
- 构建：`tools/build-all.ps1` 增加一次前端构建 + 一次 `wails build -s`，随后把
  `tools/plugin-market/build/bin/PetPCPluginMarket.exe` 复制到 `build\bin`（与 `PetPCVoiceStudio.exe` 同级，
  顺序照既有「studio 先构建、再复制」的注释理由）。

### 2.2 绑定面（全部只做投影，不做插件逻辑）

| 绑定 | 返回值 | 说明 |
|---|---|---|
| `PluginMarketPath() (string,bool)` | exe 路径 | 供界面显示「桌宠从哪儿找它」 |
| `Installed() ([]plugins.Installed, error)` | 已装行 | 直接返回 manager 的行（含 `Manifest`/`Dir`/`Error`） |
| `Catalogue() (plugins.MarketView, error)` | 市场目录 | 基址由 `config.go` 的值经 `Manager.BaseURL` 函数提供 |
| `SaveMarketURL(raw string) (plugins.MarketView, error)` | 目录 | http/https 校验照 `app_plugins.go:324-336`，但写入**本程序自己的配置文件** |
| `Install(id string) error` / `Uninstall(id string) error` | — | 直通 `Manager.Install`（条目由 `Catalogue()` 查得）/`Manager.Uninstall` |
| `SetEnabled(id string, on bool) error` | — | 直通 |
| `Grant(id string, granted []string) error` | — | 直通（`Manager.GrantPermissions` 已拒绝未声明的权限） |
| `GetConfig(id string) (PluginConfig, error)` / `SaveConfig(id string, values map[string]string) error` | 配置视图 | 走 `internal/plugins` 的配置存储（§3.3/§3.4）；`secret` 项只回 `{set:true, mask:"sk-…cdef"}`，**明文绝不出 Go** |

- **本程序只 import `internal/plugins` 与 `internal/store` 两个包**，与规格卡 `impl_decisions` 第 2 条的「零复制复用」口径一致；不 import `internal/llm`、不自带第二份 LLM 路径。
  「连接测试」属**范围外**（见 proposal §4 第 11 条）：对话插件的端点与密钥是否正确，由它自己在面板槽位里以一次 `llm.chat` 给出真实反馈（该路径正是用户真正要用的那条），市场程序不为此再建一条探测路径。
- **刷新**（澄清 Q2 / 规格卡「顶部市场地址输入框与刷新」）不需要新绑定：它是同一进程内对 `Catalogue()` 的再调用 + 列表重绘（T18b），基址变更走 `SaveMarketURL`。绑定面因此没有单独的 `Refresh` 方法。

### 2.3 自己的配置文件（与桌宠 `config.json` 分离）

`<dataDir>/plugin-market.json`：

```json
{ "marketBaseUrl": "", "lastSelectedId": "", "windowW": 980, "windowH": 700 }
```

- 缺省 `marketBaseUrl` 从桌宠 `config.json` 的 `marketBaseUrl`（`internal/store/store.go:28` 字段）**播种一次**，
  取不到则用 `plugins.DefaultMarketBaseURL`（`internal/plugins/market.go:25`）。
- 播种后**只读**桌宠 `config.json`，**绝不写**它一个字节（澄清 Q3/Q4 的硬要求）。
- 损坏时按本仓既有 `quarantine` 手法改名 `.bak` 并回落默认值（`internal/plugins/manager.go:220-229` 同构）。

## 3. 交付三（规则层）：权限、manifest、配置存储、白名单、浮层上限

先在 `internal/plugins` 把规则落地，再让两个程序共用——这是「不存在两套实现」的落地方式。

### 3.1 第 10 项权限

`internal/plugins/capability.go`：

```go
// PermPluginConfig lets a plugin keep its own settings: the host stores them in
// the plugin's private directory and never lets one plugin read another's.
PermPluginConfig = "plugin.config"
```

- 追加到 `AllowedPermissions` 末尾（第 10 项）、`PermissionLabels` 加「保存你自己的插件配置」。
- 同步三处硬断言：`internal/plugins/manifest_test.go:155-174`（改名与清单）、
  `app_plugins_test.go:863-881`（调度表完整性自动跟随 `AllowedPermissions`）、
  `frontend/src/main.js` 侧标签映射（迁移到市场程序前端，见 §6.2）。

### 3.2 manifest 扩展

```go
// ConfigItem is one setting a plugin declares. The host validates it, shows it in
// the market program's editor, stores the value in the plugin's private
// directory, and - for HostWhitelist items - consults it when net.fetch decides
// whether a host is reachable.
type ConfigItem struct {
    Key   string `json:"key"`
    Label string `json:"label"`
    // Type is one of text/number/bool/secret. It decides which control the
    // editor draws and how the value is stored; it is NOT a coercion promise -
    // a value the plugin cannot parse is the plugin's problem, not the host's.
    Type    string `json:"type"`
    Default string `json:"default,omitempty"`
    // Secret means the value never leaves the plugin: it is encrypted at rest and
    // never returned to any UI in plaintext.
    Secret bool `json:"secret,omitempty"`
    // HostWhitelist marks the item whose CURRENT VALUE supplies additional
    // net.fetch hostnames. At most one item may carry it.
    HostWhitelist bool `json:"hostWhitelist,omitempty"`
}
```

- `Manifest` 新增 `Config []ConfigItem json:"config,omitempty"` 与
  `PanelSlot string json:"panelSlot,omitempty"`（取 `""`/`"chat"`）。
- 校验（全部在 `Manifest.Validate()` 的表内，逐字段报错，风格照 `manifest.go:115-156`）：
  - `config[].key` 必须匹配 `^[A-Za-z0-9_-]{1,64}$`（**与 `app_plugins.go:690` 的 `storageKeyPattern` 同形**，避免两套 key 规则）；
  - `config[].type` ∈ `{text,number,bool,secret}`；`key` 不重复；数量上限 `maxConfigItems = 16`；
  - `config[].label` 非空且 ≤ 64 字符；
  - `secret=true` 要求 `type=="secret"`，`type=="secret"` 要求 `secret=true`（消除两种写法）；
  - `hostWhitelist=true` 的项**至多一个**；**不要求该插件声明 `net.fetch`**（收敛裁定，与 T29 同步）：
    「白名单来源是否被使用」由调用路径决定，不由声明层决定。对话插件的请求走宿主 `llm.chat`
    （`app_plugins.go:829` 的 `a.llmClient().Chat`），**不经过 `net.fetch`**，因此它的 1.1.0 manifest
    不声明 `net.fetch`（§8.1）；若此处要求配对，`hostWhitelist` 声明与「不声明 `net.fetch`」将互斥。
    对声明了 `net.fetch` 的插件，`validateNetHosts`（`manifest.go:177-188`）的配对规则一字不改；
  - `panelSlot` 只接受 `""`/`"chat"`；`panelSlot=="chat"` 要求 `panel==true`；
  - `panelSlot=="chat"` 的 id 若与内置对话插件 id 相同则由宿主优先；**不限制第三方**（不做「保留槽位」例外，见 §11 C6）。
- 新增 `func (m Manifest) ConfigItem(key string) (ConfigItem, bool)`。

### 3.3 配置存储（`internal/plugins/config.go`，新文件）

```go
// ConfigStore persists one plugin's declared settings.
//
// Values live in <dataDir>/plugin-data/<id>/config.json and secrets in
// <dataDir>/plugin-data/<id>/secret-<key>.bin, so a plugin's settings are inside
// the directory that is deleted when the plugin is uninstalled - leaving no state
// nothing can ever read.
type ConfigStore struct {
    dir      string
    manifest Manifest
    keeper   SecretKeeper
}

// SecretKeeper is the host's encrypted-at-rest secret storage, injected so that
// internal/plugins keeps its "standard library only, never imports
// internal/desktop" boundary (manifest.go:9-13).
type SecretKeeper interface {
    Get(path string) (string, error)
    Set(path, value string) error
    Clear(path string) error
}
```

- 路径：`filepath.Join(pluginDataDir, "config.json")`。写入沿用 `manager.go:186-218` 的**临时文件 + `os.Rename`** 手法
  （同一包内，抽出包级 helper `atomicWriteFile`，`saveIndex` 与新代码共用一份，不改语义）。
- 值一律以字符串存 JSON（`map[string]string`）；`type` 只决定编辑器控件与 `bool` 的 `"true"/"false"` 归一。
- **`secret` 项不写 `config.json`**：值走 `keeper.Set(filepath.Join(dir,"secret-"+key+".bin"), v)`，
  `config.json` 里对应键存哨兵 `""`。读取时 `keeper.Get` 解密；解密失败视作「未配置」（照 `secret.go:59-79` 的既有立场）。
- `Read(id)`：只返回非 secret 项 + secret 项的 `{set, mask}`（mask 由前 3 后 4 生成，照 `secret.go:158` 的 `Mask`）。
- `Write(id, values)`：拒绝未声明的 key（照 `GrantPermissions` 拒绝未声明权限的立场）；secret 项只接受非空写入（空值=清除，见 D3）。
- 未提供 `keeper` 时 secret 项**拒绝写入并报错**（绝不回落明文落盘）。

### 3.4 `config.get` / `config.set` 能力（`app_plugins.go`）

```go
"config.get": {plugins.PermPluginConfig, pluginConfigGet},
"config.set": {plugins.PermPluginConfig, pluginConfigSet},
```

- `config.get`（无参数或 `{}`）返回 `{values:{...}, secrets:{key:{set,mask}}}`，**不含任何明文密钥**。
- `config.set` 接受 `{values:{key:value}}`，只写声明过的 key；**权限未授予时 `PluginRequest` 在触碰参数之前就拒绝，
  状态一点不变**（`app_plugins.go:373-410` 的既有顺序保证）。
- 两者都走 `ConfigStore`，因此**市场程序与插件看到的是同一份文件**。

### 3.5 配置化域名白名单

- 新增 `func ConfigHosts(man Manifest, values map[string]string) []string`：
  对每个 `HostWhitelist==true` 的项取当前值，按 `,`/空白切分，对每段：
  - 去掉首尾空白；空段丢弃；
  - 含 `://` 时 `url.Parse` 并要求 scheme ∈ `{http,https}`，取 `u.Host`；**非法 scheme 或解析失败 → 该段不贡献任何放行**；
  - 不含 `://` 时要求匹配 `netHostPattern`（`manifest.go:84`），否则丢弃；
  - **含通配符号（`*`）的段一律丢弃**（精确匹配立场，`capability.go:106-113`）。
- `pluginNetFetch`（`app_plugins.go:636-683`）的判定改为：

```go
hosts := append(append([]string{}, row.Manifest.NetHosts...), plugins.ConfigHosts(*row.Manifest, a.pluginHost.configValues(id))...)
if !plugins.HostAllowed(hosts, u.Host) { return nil, fmt.Errorf("net.fetch 未声明的域名：%s", u.Host) }
```

- 其余一字不改：仍只 `GET/POST`、仍先校验再建请求（拒绝时不触网）、仍 `io.LimitReader(1<<20)`、仍 30s 超时。

### 3.6 浮层矩形上限 4 → 8，以及椭圆标志

- `internal/plugins/manifest.go:73`：`maxSurfaceRects = 8`；`Rect` 新增 `Ellipse bool json:"ellipse,omitempty"`。
- `PluginSurfaceRect`（`app_plugin_surface.go:20-31`）新增 `Ellipse bool json:"ellipse"`；
  `computePluginSurface` 在组装 `desktop.RegionRect` 时对 `r.Ellipse` 置 `Ellipse: true`（复用 `internal/desktop` 既有的
  `RegionRect.Ellipse` 字段与它的椭圆裁剪实现——字段定义在 `internal/desktop/desktop.go:38-48`、裁剪在
  `internal/desktop/desktop_windows.go:360`；`app_live.go:299` 是本仓至今唯一的使用者，该文件随 T56 删除后
  插件浮层面成为唯一使用者），并跳过圆角计算（椭圆的 radius 无意义，置 0）。
- 前端 `plugin-host.js` 对 `r.ellipse===true` 的 iframe 设 `border-radius: 50%`，其余沿用 `r.r`。
- 校验：`ellipse=true` 的矩形宽高必须为正（既有规则已覆盖），且**不限制与 radius 同时出现**（radius 被忽略，行为确定，不报错）。

## 4. 交付三（对话侧）：插件自持 AI 配置、面板槽位、浮层

### 4.1 参数来源唯一收敛：从插件配置构造 `llm.Settings`

`pluginLLMChat`（`app_plugins.go:793-845`）内部改为：

```
man := 该插件的 Manifest
values := ConfigStore.Read(id)                      // 非 secret 值
key := SecretKeeper.Get(<dir>/secret-llmApiKey.bin) // 仅当声明了该项
cfg := store.Config{}                                // 零值，不是桌宠 Config
cfg.LLMBaseURL/LLMModel/LLMTemperature/LLMMaxTokens/LLMTimeoutSec/LLMSystemPrompt/
cfg.LLMHistoryTurns/LLMContextTodos/LLMDailyBudget  <- 同名 config 项
```

- 然后**原样**调用既有的 `a.promptContext(cfg)` 与 `a.llmSettings(cfg)` 与 `a.llmClient().Chat(...)`：
  SSE 解码、重试、错误翻译、delta 合并、`res.Usage` 全部由 `internal/llm` 的既有实现完成，**一行不改**。
- 「未配置」的表述沿用 `llmView` 的 `NotReadyReason` **语义**（尚未配置时给出一句可显示的原因、绝不静默降级），
  但**类型与判定都换掉**：`LLMView`/`llmView()`/`llmReadinessNow()` 与 `State.LLM` 字段随宿主对话 UI 一起删除
  （T87b：`app.go:1158` 的字段、`:1218` 的赋值、`app_llm.go:103-257` 的视图与 `:175-205` 的 readiness）；
  判定条件改为直接读**插件配置**：
  `cfg.LLMBaseURL=="" || cfg.LLMModel==""` → `错误：「对话插件」还没配置端点与模型，请在插件市场里填好`；
  `needsKey && key==""` → `错误：还没有填写 API Key`。
- **`cfg.LLMEnabled` 不再参与**：插件的启用态就是它的开关（`PluginRequest` 第一步已要求 `row.Enabled`）。
- 宿主 `Config` 的 13 个 `llm*` 字段在请求路径上**不再被读取**（只被迁移逻辑读，见 §7）。

### 4.2 预算（宿主强制）与用量（单一写者）

- 宿主继续**强制**每日上限：`pluginLLMChat` 发送前读该插件的 `llmDailyBudget` 配置项与
  `plugin-data/<id>/llm-usage`（后者由宿主写），`OverBudget` 则拒绝（沿用 `store.LLMUsage.OverBudget`）。
- 文件与写者：
  - `plugin-data/<id>/llm-usage` —— **宿主唯一写者**（`pluginLLMChat` 开始时计数、结束时加 token，沿用
    `store.LLMUsage` 的语义：按**尝试**计数、token 只增不减、日期变更即翻转）。
  - `plugin-data/<id>/llm-history` —— **插件唯一写者**（经既有 `storage.get`/`storage.set`，key 为 `llm-history`），
    宿主**不写转写**（这正是 `app_plugins.go:789-792` 既有注释的立场）。
- 为此在 `internal/store/llm.go` 增加**两个导出纯函数**（既有方法与其行为一字不改）：

```go
// RolledTo returns the usage record for now, resetting it when the stored record
// belongs to an earlier local day.
func (u LLMUsage) RolledTo(now time.Time) LLMUsage
// PlusAttempt records one attempt and its token report on top of u.
func (u LLMUsage) PlusAttempt(promptTokens, completionTokens int) LLMUsage
```

  既有 `normalise`/`addTokens` 改为调用这两个导出函数（纯重构，`internal/store/llm_test.go` 现有断言保持绿）。
- 用量**展示**由市场程序的插件配置页与插件自身界面读取（`storage.get` key `llm-usage` 由插件读，字段与
  `store.LLMUsage` 同名）——因此「用量统计仍随对话累加」有一个可核对的落点。

### 4.3 面板槽位：`panelSlot: "chat"`

- 删除 `#tab-plugins` 后，「对话」tab 的 `#tab-chat` 体**收缩为挂载点**：保留 `id="tab-chat"` 容器（门禁与
  `switchTab` 都按它工作），内部只留 `<div id="chat-slot" class="hidden"></div>`，删掉 `#chat-bar`/`#chat-log`/
  `#chat-form` 及其全部控件（`frontend/index.html:78-99`）。
- 宿主解析槽位：`chatSlotPlugin()`（名字以 `tasks.md` T49 为准，本文件不另起一名）取「已装且启用、`granted` 含 `pet.surface`、`panelSlot=="chat"`」的插件；
  找不到则在 `#chat-slot` 内显示一句提示（不是空 iframe）：
  「对话插件还没装好：在设置里点「打开插件市场」装上并授权它。」
- 找到则 `PluginHost.openSlot('chat', plugin)`：iframe 填满 `#tab-chat` 的内容矩形，`kind` 仍为 `'panel'`
  （协议不变，`plugin-host.js:357-381` 的既有实现），`init` 消息的 `surface.kind` 保持 `'panel'`；
  新增 `surface.slot='chat'` 字段，让插件知道自己被放在哪个槽位。
- 「关掉对话插件后浮层不可用且面板对话槽位给出明确提示」= 上述提示 + 双击时改为弹一句气泡（§4.4）。

### 4.4 浮层：双面插件浮层面 + 双击入口

- **插件声明 5 块矩形**（`market/plugins/com.shixiansi.plugin-chat/manifest.json` 的 `surface`）：
  卡片（圆角矩形）、输入条（圆角矩形）、3 个思考泡泡（`ellipse: true`）。
  坐标语义沿用既有约定：相对**宠物左上角**的 CSS 像素（`manifest.go:44-48`），由 `computePluginSurface`
  夹回窗口并换算物理像素 —— 因此「5 块矩形各自的 iframe 位置逐字等于 `Layout` 下发矩形」由构造保证。
- **双击宠物**的新路径：

```
双击 → App.OpenChatPluginSurface()
        ├─ 找不到可用对话插件 → showBubble（kind=reminder）说明原因，返回当前 Layout
        └─ 找到 → 走既有 OpenPluginSurface(id)（含「未回答提问时拒绝且不改状态」的守卫）
```

- `OpenChatPluginSurface()` 是新绑定（`app_chat_slot.go`），只做「解析插件 → 调用既有 `OpenPluginSurface`」，
  不在浮层路径里新增第二种几何。
- 单击宠物仍打开面板并切到「对话」tab（面板槽位由此渲染），`App.OpenChat()` 与
  `OpenTodoPanelFromChat()`（`app_llm.go:1006-1119`）保留，仅去掉 `OpenChat` 对宿主 `chat.js` 的依赖。

### 4.5 被取代的宿主浮层实现（删除，避免两套几何）

- 删除 `frontend/src/live.js`（465 行）与 `frontend/src/chat.js`（488 行）：两份 UI 都由插件渲染。
- 删除 `app_live.go` 与其 UI 支撑（`OpenLive`/`CloseLive`/`liveIsOpen`/`emitLive`/`Layout.Live`/
  `app.go:707-715` 的 `case live:` 分支）与 `app_live_test.go`，并删净**同文件内的 proactive 家族**（`tickProactive`
  等，见 §11 C8：这些函数住在 `app_live.go`，所以「保留函数只断开调度」与「整文件删除」不能同时成立；本设计取后者）；
  规则不变、实现换了承载——「未回答提问时浮层被拒绝且不改状态」由 `OpenPluginSurface` 承担
  （`app_plugin_surface.go:221-224`），「提问出现时浮层被收掉」由 `showBubble` 清浮层面承担
  （`app.go:977-1001` 既有行为）。**实现收敛分成四个任务，整包的编译与测试判据只落在链尾 T87c**：
  `app_live.go` 删除（T56）→ 四条安全对赌用例改写（T57）→ 宿主对话引擎与其已无入口的绑定删除（T87b）→
  主动说话整套删除（T87c）。T56/T57/T87b 都只判**源码级终态**（T56 的 `grep --glob='!*_test.go'`；
  T57 的四条断言逐条保留；T87b 的 `grep` 四条），**不含 `go build ./...`/`go vet ./...`/`go test ./...`**——
  因为 T56 删掉 `app_live.go` 后，`app_llm.go` 的 `rescheduleProactive` 仍引用随之消失的
  `setNextProactive`/`proactiveGap`，而这一半由 T87c 删除，**链尾之前整包不可编译**。
  这三条判据（`go build ./...`、`go vet ./...`、`go test ./... -count=1`）**唯一**是 T87c 的完成判据。
  **排期约束**：这四步是一段连续批次，必须排在 A–H 组之后、I 组收尾（T87b/T87c 本就在 I 组末段）——
  批次内根包不可编译，而 F/G/H 组（T58–T77）的判据要跑根包测试或整包构建。
  `pomodoro_gate_test.go` 中四个浮层对赌用例**改名为按插件浮层面调用**，
  断言内容逐个保留（不删用例、不放宽）。
- `Layout.Live` 字段删除后，`.dev/check-frontend.cjs:127-140` 的 `pluginSurface` 断言保留、`live?: LiveView`
  相关断言删除，`models.ts` 重新生成。
- **`verify-live.ps1` 的判据全部保留、只换驱动面**：它不再双击后看 `#live`，而是双击后看对话插件声明的
  5 块矩形（卡片在上、输入条紧贴宠物下方、第一个泡泡中心被绘制且**包围盒角点露桌面**——
  后者由 `ellipse: true` 产生的椭圆区域满足，脚本原有的椭圆证明保持成立）；窗口不 resize、宠物不移动两条不变。

## 5. 交付二：桌宠侧收口

### 5.1 面板删 tab

- `frontend/index.html`：删 `data-tab="plugins"` 按钮（`:68`）与 `#tab-plugins` 整块（`:145-185`）；
  tab 计数 5→4。
- `frontend/src/plugins.js` 删除；`main.js` 删 `import { PluginsTab }`、`els.tabPlugins` 与全部插件 els、
  `switchTab` 的 `tabPlugins` 行（`:1316`）、`if (name === 'plugins' && pluginsTab) refresh()` 分支（`:1324-1329`）、
  `PluginsTab` 实例与市场地址接线（`:1573-1602`）。
- **宿主侧的插件管理绑定一并删除（最终态）**：`app_plugins.go` 的 `PluginsView`/`MarketView`/`InstallPlugin`/
  `UninstallPlugin`/`SetPluginEnabled`/`GrantPluginPermissions`/`SaveMarketURL` 七个绑定连同 `installedView`/
  `marketView`/`marketEntry` 三个私有助手与它们的视图类型整体删除（T87d）——管理面**只在** `PetPCPluginMarket.exe`，
  它直接调 `internal/plugins` 的 `Manager`，宿主不再保留一条无人调用的第二条管理路径。**保留**：`PluginRequest`
  （插件能力的唯一入口）、`OpenPluginSurface`/`ClosePluginSurface`、`PluginsInstalled`（槽位与浮层面解析用）、
  `PluginEventView` 与 `PluginView`（后者仍被 `PluginsInstalled` 使用）。
- 删除绑定带走的不变量按**新承载**保留（T87e）：`UninstallPlugin`/`SetPluginEnabled` 原本在卸载/停用后调
  `closeSurfaceFor(id)`。管理口子移走后，守卫改为**每次几何计算时判定**——`pluginSurfaceRects` 的可用性判据收紧为
  「已装 且 `Enabled==true` 且 `granted` 含 `pet.surface` 且 manifest 可读」，不可用即走 `app.go:799-804` 既有的
  「矩形归零就同时清掉状态与 layout」路径，`closeSurfaceFor` 随之删除。这比原调用点更严：市场程序（另一个进程）
  卸载/停用插件后的下一个几何 pass 就会收起浮层面，而原实现只在宿主自己调那两个绑定时才生效。因此
  「插件没了，它的矩形不能继续占着窗口裁剪区域」这条仍然成立、仍可核对。
- 测试夹具随之改用管理器直通（`plugins.NewManager(dir).Install/GrantPermissions/SetEnabled`），断言语义不变（T87d）。

- **保留**：`PluginHost` 实例（浮层面与对话槽位都要它）、`pluginViewFor` 改为读新的 `PluginsInstalled()` 快照
  （不再依赖被删的插件 tab 快照）。

### 5.2 跨进程可见性（不新增机制）

- `pluginHost.manager` 缓存的是 `*plugins.Manager`，它**无内存状态**（`manager.go:76-96`），因此
  `Plugins()`/`Get()` 每次调用都重新读 `plugins.json` —— 市场程序写完后桌宠下一次读即最新。
- 需要补的只有两处**可核对判据**（T14、T48）：
  - 桌宠侧：外部改写 `plugins.json`（模拟市场程序）后 `App.PluginsInstalled()` 返回新值；
  - 市场程序侧：外部改写后 `Installed()` 返回新值。
- 新增 `func (a *App) PluginsInstalled() ([]PluginView, error)`：只投影 `plugins.Installed`，供浮层面/槽位/`pluginViewFor` 使用。

### 5.3 设置页入口按钮

- `frontend/index.html` 在**「数据与退出」**分组（`:605`，`<h3 class="sgroup-head">数据与退出</h3>`）内新增一行：

```html
<div class="field">
  <span class="field-label">插件市场</span>
  <div class="field-input">
    <button id="open-plugin-market" class="primary" type="button">打开插件市场</button>
  </div>
</div>
```

  不新增 `.sgroup`（分组 7→5 但按钮不计入新组）；id 加入 `expectedSettingsIds`。
- `main.js` 接线：`App.OpenPluginMarket().then(applyState).catch(fail)`（照 `voice-open-studio` 的接线形态，
  `check-frontend.cjs:1342-1346` 已为配音工坊钉了同一形态）。
- Go 侧新增（放在 `app_plugin_surface.go` 之外的新文件 `app_plugin_market.go`，与 `app_voice_studio.go` 平行）：

```go
const pluginMarketExeName = "PetPCPluginMarket.exe"

func (a *App) PluginMarketPath() (string, bool)      // 第一个存在的候选，供设置页显示
func (a *App) PluginMarketCandidates() []string      // 三处查找顺序（可测的纯列举）
func (a *App) OpenPluginMarket() (State, error)      // detached 启动，不等待、不关闭
```

- 查找顺序逐字照 `app_voice_studio.go:101-118`：
  ① `filepath.Join(filepath.Dir(exe), pluginMarketExeName)`；
  ② `filepath.Join(filepath.Dir(exe), "..", "tools", "plugin-market", "build", "bin", pluginMarketExeName)`；
  ③ `filepath.Join(wd, "tools", "plugin-market", "build", "bin", pluginMarketExeName)`。
- 启动沿用 `cmd.Dir = filepath.Dir(path)` + `detachedProcessAttr()` + `go cmd.Wait()` + 记录 `launched`
  （`app_voice_studio.go:145-157`）；找不到时返回 `找不到插件市场程序（PetPCPluginMarket.exe）…` 的同一句式。

## 6. 门禁同步（硬性交付内容）

### 6.1 `.dev/check-frontend.cjs`

| 位置（现存） | 改动 |
|---|---|
| `:23-27` 读 `plugins.js`/`plugin-host.js` 与 `scannedJs` | 删 `plugins.js`、`chat.js`、`live.js` 三项；`scannedJs` 变为 `[mainJs, pluginHostJs]` + 市场程序前端（`tools/plugin-market/frontend/src/main.js`） |
| `:207-225` `newSections` | 删 `/* --- plugins tab`；保留 `/* --- plugin surface`；新增 `/* --- plugin market`（市场程序自己的 style.css 段） |
| `:253-279` 分组与 LLM 子块 | 分组 7→5；删「LLM 分组三个子块」与「主动说话是独立分组」两条断言（两组已删），新增「设置页有打开插件市场按钮」断言 |
| `:339-361` `expectedSettingsIds` | 删 25 个 `llm-*`；增 `open-plugin-market` |
| `:374-378` tab 计数 | 5→4，断言文案改为 `todos/chat/records/settings` |
| `:379-388` `#tab-plugins` 两条 | 删除；替换为「`#tab-chat` 内有 `#chat-slot` 挂载点」与「`switchTab` 仍切换 `#tab-chat`」 |
| `:115` `layoutReturning` | 删 `OpenLive`/`CloseLive`；增 `OpenPluginSurface`/`ClosePluginSurface`/`OpenChatPluginSurface` |
| `:127-145` `models.ts` 断言 | 保留 `pluginSurface?: PluginSurfaceView;`；删 `live?: LiveView`；`OpenPluginSurface` 仍须是 `main.Layout` |
| `:151` `goFiles` | 保持含 `app_plugins.go`；**删 `app_live.go`**（文件已随 T56 删除，留在数组里会让该 `readFileSync` 抛错）；新增 `app_plugin_market.go`、`app_chat_slot.go`、`app_plugin_migrate.go`（否则它们发出的 `pet:*` 字面量事件会被读成「订阅没有发出者」） |
| `:1224-1228` 滚动条规则组 | 去掉 `#tab-plugins`（组内仍 ≥4 个选择器，`:1246` 阈值不变） |
| `:1333-1341` 「宠物没有配音工坊 tab，且 tab 计数为 5」 | 计数改 4，注释同步 |
| `:554-559` 「key 输入不回写」 | 断言对象改为市场程序前端的密钥控件（`tools/plugin-market/frontend/src/main.js` 含 `llmApiKey` 输入且从不回填） |
| `:571-587` 主动说话三条 | 删前两条（`llm-proactive`/`llm-proactive-gap`，UI 已移除）；第三条 `App.SpeakNow()` 连同 T87c 一起删（"主动说话"整套随 `app_live.go` 回调家族删净，不留半条路径） |
| `:75-84` `liveParts`/`usedParts` | 删（`live.js` 已随 T56 删除，两个集合失去来源） |
| `:141-145` `ChatMessageView` 的 `proactive` 断言 | 删（该模型随 T87b 删除） |
| `:187-198` `pet:chat-*` 四条与 `pet:live` 一条「既发出又被订阅」断言 | 删；改写为 `pet:plugin-event`/`pet:layout`/`pet:chat-open` 三者的等价检查 |
| `:471-552` live.js 断言块 | 整段删（`place(card/bar/puffs)`、不自行设尺寸、`ChatPopup` 组件、`#live` 挂载点、卡片无 chrome、`.chat-puff` 椭圆的 CSS 断言、输入框不抢焦点、`maxProactiveRunes`、`computeLiveOverlay` 锚定）；`.chat-puff` 的椭圆语义改由 T108（harness 三个 `ellipse` 帧 `border-radius:50%`）承接 |
| `:564-568` 「`chat.js` 不自行 resize」 | 删（`chat.js` 随 T87 删除） |
| `:673-677`、`:697-704` 引用被删函数的守卫断言 | 删；「浮层面拒绝占据提问槽位」改指向 `OpenPluginSurface`；`:678-696` 三条保留 |
| `:935-941` 「宠物隐藏时不自言自语」 | 删（读 `app_live.go` 的 `petHidden` 分支，该文件已删） |
| `:1064` 归档代码断言 | 保持（`startSpeech|speechGated|eventChatReveal` 仍不得出现） |
| `:1280-1611` §16 配音工坊段 | **一字不改**（本轮不动 `tools/voice-studio`）；新增 §17「插件市场是独立程序」段，形态照 §16：`go.mod` 的 module/replace、`wails.json` 的 outputfilename、`frontend/src/main.js` 导入 style.css、`config.go` 出现 `plugin-market.json`、**`/plugin-market\.json/` 命中且 `config.json` 在产品代码里零命中**。零命中的扫描集合**只含非测试源码**（`config.go`/`market.go`/`main.go`），因为 `config_test.go` 必须以 `config.json` 为断言对象来证明「桌宠配置一个字节没动」（T5、T10）——把测试也扫进去会让 T102 与 T5 互相排斥。市场前端 `main.js` 单独读取（形如 §16 的 `studioJS` 变量），**不**进入 `scannedJs`（见 T89） |

### 6.2 市场程序前端

- `tools/plugin-market/frontend/src/style.css` 必须非空、必须被 `main.js` **import**，且段标记
  `/* --- plugin market */` 存在（照 `:1312-1331` 对配音工坊的两条断言形态）。
- 新增 `tools/plugin-market/frontend/src/main.test.js`（jsdom）与 `vitest.config.js`：断言
  「权限清单每项都渲染为完整可见的行（不截断）」「市场地址框 DOM 无内联宽度、占满其容器」
  「列表与详情是两个独立的滚动容器」「点刷新按钮后 `Catalogue` 被再调一次、列表随响应更新、拉取失败时提示可见且不清空列表」。
  **这些就是规格卡「截图实证的界面优化作为明确判据」与澄清 Q2「顶部地址框 + 刷新」的可执行形式。**

### 6.3 `.dev/run-frontend-harness.cjs`

| 位置（现存） | 改动 |
|---|---|
| `:76-101` `methods` 白名单 | 删 `PluginsView`/`MarketView`/`InstallPlugin`/`UninstallPlugin`/`SetPluginEnabled`/`GrantPluginPermissions`/`SaveMarketURL`/`CloseLive`/`OpenLive`（七个管理绑定由 T87d 从 Go 侧删除，因此重新生成的 `App.js` 不再导出它们）；保留 `PluginRequest`/`OpenPluginSurface`/`ClosePluginSurface`；增 `PluginsInstalled`/`OpenPluginMarket`/`OpenChatPluginSurface` |
| `:336-418` 插件假数据 | 删（插件 tab 已无）；保留 `pluginDir` 供浮层面用 |
| `:448-...` `__handlers` | 删插件管理类绑定与 `liveView()`；保留 `PluginRequest`/`OpenPluginSurface`/`ClosePluginSurface`/`GetLayout`（Layout 含 `pluginSurface`） |
| 第 18 节（`:1594-1760`）插件 tab 用例 | **整节删除** |
| 第 19 节（`:1890-2035`）插件浮层面 | 保留并适配为**对话插件的 5 块矩形**：一个 `pluginSurface` 支付 5 个 `rects` → 5 个 iframe；逐个断言 `left/top/width/height` 逐字等于矩形；`ellipse:true` 的两个/三个泡泡帧 `borderTopLeftRadius === '50%'`；每个帧各收到一次 `init` 且 `surface.rectIndex/rectCount/rect` 正确；沙箱恰为 `allow-scripts`；按 `pluginId` 投递事件 |
| 新增节 | 设置页入口按钮：点击后 `__calls` 出现 `OpenPluginMarket` 且恰好一次 |
| 对话 tab 相关（chat 用例、popup 用例、`ClearChat`/`SendChat`/`Regenerate` 等） | 删除：宿主不再渲染对话，其能力改由 `PluginRequest` 驱动；**保留**「切到 `#tab-chat`」这一步（`data-tab="chat"` 的导航语义不变，只是 tab 体里只有 `#chat-slot`）；保留 `PluginRequest` 的流式帧断言，并把「未获权限时插件看到宿主原文」这条移到 `plugin.config`（`:1869-1875` 的断言对象改为 `未获权限 plugin.config`） |

### 6.4 `frontend/src/test-harness.js`

- `methods` 白名单：删 `ClearLLMKey`/`SaveLLMSettings`/`SetLLMKey`/`TestLLMConnection`/`ClearChat`/`CloseLive`/`OpenLive`/`DebugLLMState`
  （最后一项随 T87b 删除——它是宿主对话设置的诊断绑定，`llmView` 消失后无实现）；
  增 `PluginsInstalled`/`OpenPluginMarket`/`OpenChatPluginSurface`。
- **宿主 `main.js` 的 AI 接线必须一并删净（T87f）**：删 25 个 `llm-*` 的 els 条目、`renderProactiveStatus()`、
  `llmSettingsPayload()`/`saveLLMSettings()`/`wireLLMSettings()` 及其调用、`renderSettings` 的 LLM 段赋值与
  `state.llm?.keyFile` 引用，以及 `App.SaveLLMSettings`/`SetLLMKey`/`ClearLLMKey`/`ResetLLMUsage`/`SpeakNow`/
  `TestLLMConnection` 六处调用。T92 只改门禁脚本的 id 白名单，**改不到**这些读取与绑定：少了 T87f，
  `els.llmEnabled` 等会取到 `null` 并在 `renderSettings` 里抛错，harness 与 vitest 都到不了全绿。
- `baseState()`：删 `llm` 段（宿主不再有 AI 状态）与其 `config.llmEnabled`；
- `layout()`：删 `live` 形参，增 `pluginSurface` 形参（默认 `null`）；
- `bubbleView()` 保留（气泡机制未变）。

### 6.5 `frontend/src/*.test.js`

- 删 `plugins.test.js`；新增 `settings-market.test.js`：① 设置页存在 `#open-plugin-market` 且点击调用
  `OpenPluginMarket`（恰好一次）；② `#chat-slot` 在无可用的已启用对话插件时显示提示文案而非 iframe；
  ③ 有可用插件（`PluginsInstalled` 假数据）时 `#chat-slot` 内出现一个 `iframe[data-kind="panel"]` 且
  `src` 以 `/plugin-asset/<id>/` 开头，沙箱属性恰为 `allow-scripts`。
- `bubble-plan.test.js`/`records-plan.test.js` 不动。

### 6.6 `.dev/check-all.ps1`

- `go vet`/`go test` 两条之外新增「market program tests」门：在 `tools/plugin-market` 下
  `go vet ./...` 与 `go test ./... -count=1`；
- 「frontend build」门扩为两次（`frontend` 与 `tools/plugin-market/frontend`）；
- 其余门顺序不变（静态门禁 → harness → `wails build -s` → 三个真实窗口像素门禁）。
  市场程序的 `wails build` 不进 `check-all.ps1`（它在 `tools/build-all.ps1` 里，理由同配音工坊：产物复制顺序由那个脚本负责）。

### 6.7 其他门禁

- `.dev/verify-region.ps1`：**一字不改**（面板/宠物/气泡裁剪未变）。
- `.dev/verify-live.ps1`：保留全部判据，只把「双击后读 `#live` 的三个面」改为「双击后读对话插件的 5 块声明矩形」；
  卡片/输入条/泡泡的几何常量改从插件 manifest 读（脚本现有做法就是从常量复现，改成从 `manifest.json` 读更诚实）。
- `.dev/verify-plugin-surface.ps1`：保留；`panel.html` 探针的 `surface` 声明由 1 块改为 5 块（含一个 `ellipse:true`），
  并把原来「点第 4 个 tab 的坐标」这一驱动步骤删除——改为**不依赖页面控件**的触发通道：脚本设 `PETPC_OPEN_PLUGIN_MARKET=1`
  后启动桌宠，由启动序列在该变量存在时调用一次 `OpenPluginMarket()`（该函数与钩子由 T86 落地），并断言
  「3 秒内存在名为 `PetPCPluginMarket` 的进程」。这不新增菜单项、不新增绑定，断言对象与产品路径（设置页入口按钮
  调用同一函数）完全一致；脚本中**不得**再出现设置页入口按钮的 id 或坐标这类页面控件引用（T123 判据：该项文本零命中）。
  **为什么不用「设置页入口按钮坐标 + 点击」**：本环境已记录「合成点击能到达桌宠窗口但到不了页面里的控件」
  （`README.md:977`、`.dev/verify-plugin-surface.ps1:29`），且按钮坐标依赖设置页的滚动位置，是一条会在
  门禁里测量自己瞄准误差的驱动方式。因此本脚本**不是**「唯一能驱动真实点击的门禁」，也不依赖任何页面控件坐标。
- `.dev/verify-plugin-market.ps1`（**新建**）：真实桌面上启动市场程序，断言 ①窗口带框可缩放（`WS_THICKFRAME` 且
  `WS_MAXIMIZEBOX` 存在）、初始 980×700；②权限勾选行的文字**未超出其容器右边界**（用像素列扫描而非查 DOM，
  因为 WebView2 无法外部取值）；③市场地址框可见宽度 ≥ 容器的 90%。无交互桌面时按既有约定报告并 `exit 0`。

## 7. 交付三（迁移）：一次性、幂等、可回滚

### 7.1 触发与标记

- 触发点：`initApp` 之后、窗口显示之前的启动路径（`app.go` 的既有启动序列末尾），以及**市场程序启动时**
  （`NewMarket()` 里）——两个程序都可能先被打开，因此迁移必须幂等且两处共用一份实现
  （实现放 `internal/plugins/migrate.go`，两个 module 都 import 它，**不复制**）。
- 标记：`<dataDir>/plugin-data/<id>/.migrated`（内容为 RFC3339 + 源文件名清单）。存在即跳过，**整段不读源文件**。
- 目标插件 id 由常量 `chatPluginID = "com.shixiansi.plugin-chat"` 固定（迁移必须知道搬给谁）。
  **若该插件尚未安装**（没有 `plugin-data/<id>/` 且 `plugins.json` 无该条目）→ **不迁移、不写标记**，
  下次启动再看；这样「装了插件才搬」是确定的，不会把用户数据搬进一个不存在的目录。

### 7.2 搬什么、搬到哪

| 源（`<dataDir>/`） | 目标（`<dataDir>/plugin-data/com.shixiansi.plugin-chat/`） | 原文件 |
|---|---|---|
| 13 个 `llm*` 值（`config.json`） | `config.json` 的 `values`（键名与 `Config` 的 json tag 同名：`llmBaseUrl`/`llmModel`/…） | **不改**（`config.json` 不是迁移文件，只读取） |
| `llm-history.json` | `llm-history`（原字节） | 改名 `llm-history.json.bak` |
| `llm-usage.json` | `llm-usage`（原字节） | 改名 `llm-usage.json.bak` |
| `llm-key.bin`（DPAPI 密文） | `secret-llmApiKey.bin`（**解出明文后重新加密**，同一 Windows 用户下 DPAPI 密文可直接复制，但重新加密更诚实也更可验证） | 改名 `llm-key.bin.bak` |

- **secret 项落点**：对话插件在 manifest 里声明 `{"key":"llmApiKey","type":"secret","secret":true}`，
  因此目标文件名由配置声明决定（`secret-<key>.bin`），迁移代码不写死文件名以外的知识。
- `config.json` 的 13 个 `llm*` 字段**保留**（见 §11 C7）：迁移只读它们，不删字段、不删文件。
  因此「原文件改名 `.bak`」只适用于三个数据文件，不适用于 `config.json`——这一点必须在
  `app_plugin_migrate_test.go` 里有断言（迁移前后 `config.json` 字节相同）。

### 7.3 幂等与失败语义

- 写入顺序（每一步都失败即止）：
  1. 读三个源文件（缺失即视为「无需迁移」，**不写标记**，直接成功返回）；
  2. 解密 `llm-key.bin`；解密失败 → 报错并**保持所有原文件不动**（`.bak` 都不建）；
  3. 写目标（`atomicWriteFile` 临时文件 + rename）：`config.json` 的 values（与既有 `config.json` 合并，插件已改过的键**不覆盖**）、
     `llm-history`、`llm-usage`、`secret-llmApiKey.bin`；
  4. 全部写成功后，才把三个源文件 `os.Rename` 成 `.bak`（`llm-history.json.bak` 已存在时先删旧备份，照
     `manager.go:222-229` 的 `quarantine` 手法）；
  5. 最后写 `.migrated` 标记。
- **任一步失败 → 返回 error，源文件全部保持原名原样**（因为改名是第 4 步且在第 1–3 步全部成功之后），
  下一次启动重试。这就是「任一环节失败则保持原文件不动并报错」的实现方式。
- 第二次启动：`.migrated` 存在 → 直接返回「已迁移」，不读源文件、不碰磁盘。
- 回滚：三个 `.bak` 与 `config.json` 都在，用户可以自行改回文件名；`config.json` 一个字节没动，
  因此「回滚」在配置层面是零操作。

## 8. 交付三（插件侧）：对话插件 1.1.0

### 8.1 manifest（`market/plugins/com.shixiansi.plugin-chat/manifest.json`）

```json
{
  "id": "com.shixiansi.plugin-chat",
  "name": "对话插件",
  "version": "1.1.0",
  "author": "shixiansi",
  "description": "在面板里与桌宠旁和大模型对话的官方插件；自己的 AI 配置与密钥由自己保存。",
  "permissions": ["llm.chat", "pet.bubble", "pet.surface", "plugin.config", "storage"],
  "panel": true,
  "panelSlot": "chat",
  "surface": [
    { "dx": 160, "dy": 10, "w": 300, "h": 112, "radius": 14 },
    { "dx": 160, "dy": 226, "w": 300, "h": 56, "radius": 14 },
    { "dx": 160, "dy": 138, "w": 34, "h": 20, "ellipse": true },
    { "dx": 160, "dy": 163, "w": 24, "h": 14, "ellipse": true },
    { "dx": 160, "dy": 184, "w": 15, "h": 9, "ellipse": true }
  ],
  "config": [
    { "key": "llmEnabled", "label": "开启对话（插件是否启用由宿主的启停开关决定；此值仅随迁移保留）", "type": "bool", "default": "false" },
    { "key": "llmProvider", "label": "服务商预设", "type": "text", "default": "deepseek" },
    { "key": "llmBaseUrl", "label": "接口地址", "type": "text", "default": "https://api.deepseek.com/v1", "hostWhitelist": true },
    { "key": "llmModel", "label": "模型", "type": "text", "default": "deepseek-chat" },
    { "key": "llmTemperature", "label": "随机性", "type": "number", "default": "0.8" },
    { "key": "llmMaxTokens", "label": "回复上限", "type": "number", "default": "800" },
    { "key": "llmTimeoutSec", "label": "超时（秒）", "type": "number", "default": "90" },
    { "key": "llmSystemPrompt", "label": "人设", "type": "text", "default": "" },
    { "key": "llmHistoryTurns", "label": "上下文条数", "type": "number", "default": "12" },
    { "key": "llmContextTodos", "label": "让它知道我的待办和番茄钟", "type": "bool", "default": "true" },
    { "key": "llmProactive", "label": "允许主动说话（保留值；宿主的主动说话已整体移除，见 §11 C8）", "type": "bool", "default": "false" },
    { "key": "llmProactiveMinGapMin", "label": "主动说话最小间隔（分钟）", "type": "number", "default": "60" },
    { "key": "llmDailyBudget", "label": "每日上限（次）", "type": "number", "default": "200" },
    { "key": "llmApiKey", "label": "API Key", "type": "secret", "secret": true }
  ],
  "entry": "panel.html"
}
```

- **`config` 必须是 13 + 1 项**：`llm*` 的 13 项与 `Config` 的 13 个字段一键一位（json tag 同名），这样迁移才是**直搬**
  而不是「挑着搬」——`ConfigStore.Write` 会拒绝未声明的 key（§3.3），所以声明必须覆盖迁移全集；
  另有 1 个 secret 项 `llmApiKey`（对应旧的 `llm-key.bin`）。`llmEnabled` 与 `llmProactive*` 随之搬入，
  但它们的**行为**由宿主侧决定（启用态由宿主的启停开关决定；主动说话在宿主侧已整体移除，见 §11 C8），
  这一点在标签里写明，避免用户以为改它就能生效。
- 坐标与 `app_live.go:62-101` 的常量同源（卡片 112 高、输入条 56 高、三个泡泡 34×20 / 24×14 / 15×9，
  纵向链 = 宠物上方重叠 20、trail 间距 4/5/7），因此迁移后浮层的**观感与位置与迁移前一致**
  （`app_live.go` 随 T56 删除，这些常量以 manifest 的声明形式存活下来，是它们的唯一归宿）。
- `llmApiKey` 是唯一的 secret 项；`llmBaseUrl` 是唯一的 `hostWhitelist` 项 —— 若该插件日后改用 `net.fetch`
  直连端点，「配置项放行新域名」就解决「用户自填端点无法被静态 `netHosts` 覆盖」的问题（沿用 §3.4 的合并判定，
  仍精确匹配、仍限 http/https）。
- **本版 manifest 不声明 `net.fetch`、也不给 `netHosts`**：对话请求经宿主 `llm.chat` 由 `internal/llm` 直发
  （§4.4/§8.3），不经过 `net.fetch`，声明一个用不上的权限只会让用户在授权页多面对一项无意义的勾选。
  相应地 `hostWhitelist` 声明**在此仍被接受**（T29 的「至多一项」规则不把 `net.fetch` 设为前置条件），
  只是它的值在本插件当前路径下不产生放行效果。

### 8.2 前端（`market/plugins/com.shixiansi.plugin-chat/`）

- `panel.html`：同一份 `entry` 同时服务面板槽位与 5 个浮层块；`init` 消息的 `surface.kind`
  （`'panel'`/`'surface'`）与新增的 `surface.slot`/`surface.rectIndex` 决定渲染哪一块。
- `app.js`：面板槽位渲染会话（历史来自 `storage.get('llm-history')`，流式经 `llm.chat` 的
  `llm.delta`/`llm.done`/`llm.error` 帧）；浮层按 `rectIndex` 渲染卡片/输入条/泡泡；
  设置面（在市场程序里）由宿主的配置声明驱动，插件自身不需要额外 HTML。
- `style.css`：三个泡泡块用 `border-radius: 50%`（与 `region.ellipse` 一致）；**不许**出现
  `box-shadow`/`filter`/`text-shadow`/`transform`（门禁 `check-frontend.cjs:226-236` 的同一规则）。

### 8.3 打包与目录同步

- 跑 `pwsh -File market/tools/package.ps1`：产出 `dist/com.shixiansi.plugin-chat-1.1.0.zip`、重写 `market/index.json`、
  并复制到 `internal/plugins/sample/`。
- `internal/plugins/sample/embed.go:31` 的显式清单由 `com.shixiansi.plugin-chat-1.0.0.zip` 改为 `...-1.1.0.zip`；
  旧 zip 删除。
- `market/index.json` 与 `internal/plugins/sample/index.json` 两处的 `version`/`download`/`sha256` 必须
  由脚本产出、**不使用占位值**（新增测试断言两处 sha256 相等且两处 zip 字节一致，照既有
  `internal/plugins/market_test.go` 的「两处同字节」判据）。

## 9. 数据流总览（改后的唯一路径）

```
                    ┌────────────────────── 共享数据目录 %AppData%\PetPC ──────────────────────┐
                    │ config.json(只读,迁移源) plugins.json  plugins/<id>/  plugin-data/<id>/  │
                    │                                                      ├ config.json      │
                    │                                                      ├ secret-<key>.bin │
                    │                                                      ├ llm-history      │
                    │                                                      ├ llm-usage        │
                    │                                                      └ .migrated        │
                    └───────────▲──────────────────────────────▲──────────────────────────────┘
                                │ internal/plugins             │
        ┌───────────────────────┴───────────┐       ┌──────────┴────────────────────┐
        │ PetPC.exe（桌宠）                  │       │ PetPCPluginMarket.exe（市场）   │
        │ 面板 380×520：4 tab                │       │ 普通窗口 980×700（可缩放）      │
        │  待办/记录/设置 = 宿主渲染          │       │ 左列表 + 右详情/权限/配置        │
        │  对话 = #chat-slot ← 插件 iframe   │       │ 自己的 plugin-market.json       │
        │ 浮层面 = Layout.pluginSurface      │       │ 安装/校验/授权/启停/卸载          │
        │        ← 插件声明 5 块矩形          │       │ = internal/plugins 同一份实现    │
        │ 双击宠物 → OpenChatPluginSurface() │       │ llm.llm.Client 仅用于「测试连接」 │
        │ llm.chat → internal/llm（不改）    │       └─────────────────────────────────┘
        └───────────────────────────────────┘
```

## 10. 任务分组与依赖顺序（`tasks.md` 的骨架）

| 组 | 内容 | 任务号 | 前置 |
|---|---|---|---|
| A | 独立市场程序骨架与绑定（module/wails.json/前端/config/构建脚本/绑定面） | T1–T20 | — |
| B | 规则层：权限第 10 项、`Rect.Ellipse` 与上限 8、manifest `config`/`panelSlot` 与校验 | T21–T33 | — |
| C | 插件配置存储（`ConfigStore`/`SecretKeeper`/原子写）与 `config.get`/`config.set` 能力 | T34–T43c | B |
| D | 配置化白名单 | T44–T46 | B、C |
| E | 宿主槽位与浮层（`PluginsInstalled`/`chatSlotPlugin`/`OpenChatPluginSurface`/删 live 路径） | T47–T57 | A、B |
| F | 迁移（幂等/失败语义/标记/密钥重加密） | T58–T64 | C |
| G | 对话插件 1.1.0（manifest/前端/打包/目录同步） | T65–T71 | B、C、D |
| H | `llm.chat` 参数来源收敛 + 预算/用量 | T72–T77 | C、F、G |
| I | 宿主侧收口（删插件 tab / 删 3 个前端模块 / 删已无入口的对话绑定与 `State.LLM`/`llmView` / 删主动说话整套 / 删七个管理绑定与关表面守卫改写 / 删净 `main.js` 的 AI 接线 / 设置页入口按钮 / `OpenPluginMarket` 与 `PETPC_OPEN_PLUGIN_MARKET` 钩子） | T78–T87f | A、E、H |
| J | 生成产物重生成（`wailsjs`、`models.ts`） | T88 | E、I |
| K | 门禁同步（`check-frontend.cjs`/harness/`test-harness.js`/vitest/`check-all.ps1`/三个像素门禁 + 新建市场门禁） | T89–T126 | I、J |
| L | 文档（README/docs/旧前提标注/权限 9→10 措辞）与端到端冒烟清单 | T127–T132 | 全部 |
| M | 全量门禁收尾（`check-all` + `build-all` + 传输层零改动核对） | T133–T135 | K、L |

依赖顺序可执行：B 是纯规则层，无前置；A 与 B 并行；C 需要 B（manifest 的 `config` 声明）；
D 需要 B、C；E 需要 A（exe 名与绑定）与 B（`panelSlot`/`ellipse`）；F 需要 C；G 需要 B、C、D（插件声明的项与白名单标记要先能校验）；
H 需要 C、F、G（参数来源与用量落点都在插件配置上）；I 需要 A、E；J 需要 I（生成产物反映最终绑定）；
K 需要 I、J（门禁断言的是最终源码与最终模型）；L、M 收尾。

**排期约束（唯一路径，§4.5 的推论）**：`[前置]` 表达的是**内容依赖**，不表示执行时序。实际执行顺序为
**E 组前段（T47–T55）→ F/G/H 组（T58–T77）→ I 组前段（T78–T87）→ 连续批次（T56→T57→T87b→T87c）→
T87d/T87e/T87f → J/K/L/M 组**。理由：该批次一旦开始，根包在链尾 T87c 之前不可编译，而 F/G/H 组
（T72–T77 的 `app_plugins_test.go`）、I 组前段（T85/T86 的假启动器用例）的判据都要运行根包测试，
T69/T70 要跑 `go build`；把这些判据排在批次之前，每个判据在其位置上才可满足。
批次内只有链尾 T87c 断言 `go build ./...`/`go vet ./...`/`go test ./... -count=1`；
批次之后（T87d 起，含其 `go build ./...` 判据）与 J/K/L/M 组都能整包编译。

## 11. 收敛决定（规格卡给了选项或不完整口径的地方，一律在此定死）

| 编号 | 规格卡原文（有分歧或多解） | 收敛后的**唯一路径** | 理由 |
|---|---|---|---|
| C1 | 「对话浮层 = 宿主计算几何」 vs 「浮层面矩形上限 4→8 以容纳对话浮层的 5 块矩形」 | **浮层几何只有 `computePluginSurface` 一条**：对话插件在 manifest 声明 5 块矩形，双击经 `OpenPluginSurface` 打开；`computeLiveOverlay` 与其 UI 路径删除 | 两条几何来源必然漂移（本仓反复修过同类问题）；且规格卡 testing 第 8 条明确要求「并集 = 宠物矩形 ∪ 插件声明矩形」 |
| C2 | 「宿主不再在对话 tab 内直接渲染自己的对话 UI」但未说旧浮层怎么办 | 旧浮层**删除**（`live.js`/`chat.js`/`app_live.go` 及其 UI 支撑），其四条安全对赌用例**改名保留**到 `OpenPluginSurface` | 保留死代码会让门禁断言的对象与实际渲染脱节；`OpenPluginSurface` 已有同样的守卫语义 |
| C3 | 「Config 中 13 个 `llm*` 字段的去留」 | **只读迁移源**：字段保留、`DefaultConfig`/`NormaliseLLM` 保留，设置页无 UI，请求路径不读 | 迁移需要它们；删除会丢「用户手改过 config.json」这类事实，且回滚变成不可能 |
| C4 | 「3 个思考泡泡」是矩形还是椭圆 | `plugins.Rect` 新增 `ellipse`；泡泡声明 `ellipse:true`，region 用既有 `RegionRect.Ellipse` | `verify-live.ps1` 的判据是「包围盒角点露桌面」才能证明椭圆区域，圆角矩形会削弱/歪曲该结论 |
| C5 | 「宿主做一次性幂等迁移」但未说迁移由谁触发 | **两个程序都触发、共用 `internal/plugins/migrate.go` 一份实现**；标记为 `<插件私有目录>/.migrated` | 用户可能先开市场程序；两处实现=两套幂等逻辑 |
| C6 | 「插件声明占面板槽位」的槽位归属 | 槽位由 `panelSlot:"chat"` 声明，**不保留内置 id**；多个插件声明同一槽位时取「已装顺序中第一个已启用且已授权 `pet.surface`」的，`PluginsInstalled()` 顺序即 `plugins.json` 顺序 | 「保留官方 id」会成为插件架构里的暗门；已装顺序是确定性来源 |
| C7 | 「设置页 25 个 `llm-*` 控件 id 全部移除」是否包含「主动说话」组 | **包含**：两组（大模型对话 21 个 id + 主动说话 4 个 id = 25 个）整体移除 | 规格卡写的是 25 个，而两组合计恰为 25（`.dev/check-frontend.cjs:346-350` 逐个数得） |
| C8 | 「主动说话」功能的去处 | **整套删除（调度与手动入口一并）**：`app_live.go` 的 proactive 家族（`tickProactive`/`decideProactive`/`proactiveMaterial`/`proactiveGap`/`setNextProactive`/`noteSpoke`/`initProactive`/`startProactiveLoop`/`proactiveView`/`ProactiveView`/`proactivePrompt`）随 T56 的整文件删除消失；`app_llm.go` 的 `SpeakNow`/`speakProactive`/`rescheduleProactive` 与 `app.go:276-277` 的两个调用点、`proMu`/`proLastSpoke` 字段由 T87c 删除；`chatRequest.Proactive`/`ChatStart.Proactive`/`a.llm.proactive` 与 `proactiveView` 的调用点**随宿主对话引擎一族在 T87b 一并消失**（`app_live.go:507` 的 `llmView()` 与 `:616` 的 `llmReadinessNow()` 也随 T56 消失），T87c 只**核对**这一段已删、不再重复声明改动面；`app_proactive_test.go` 整文件与 `app_startup_test.go` 的 proactive 用例一并删；`Config.LLMProactive`/`LLMProactiveMinGapMin` 按 C3 保留为**只读迁移源**（随 T59 搬进对话插件配置，插件日后可用 `llm.chat` + `pet.bubble` 自行实现）。**整包 `go build ./...`/`go vet ./...`/`go test ./... -count=1` 三条判据唯一落在链尾 T87c**（T56 与 T87b 只判源码级零命中，理由见 §4.5） | 见 proposal §5.2：无配置源；不应把宿主与某个第三方插件 id 硬耦合；「保留函数但断开调用」会产生一个全仓无调用点、却仍被门禁与测试引用的死家族，是三条任务互相冲突的根源（上一轮评审 #2） |
| C9 | 「对话历史与用量」的存储与读写者 | 历史 `llm-history` 由**插件**经 `storage.*` 读写；用量 `llm-usage` 由**宿主**唯一写、插件只读 | 从 `pluginLLMChat` 既有注释（宿主不写转写）与「预算必须由宿主强制」两条推出 |

| 编号 | 裁定项 | 取值 |
|---|---|---|
| D1 | 市场程序窗口尺寸 | 980×700，Min 720×520；`DisableResize:false`、`Frameless:false`、`AlwaysOnTop:false`、不透明底色 `RGBA{24,26,32,255}`（与配音工坊同色） |
| D2 | 市场程序自有配置文件 | `<dataDir>/plugin-market.json`（键见 §2.3） |
| D3 | `secret` 项的「清空」语义 | `SaveConfig` 收到 secret 项**空字符串** = 删除该密钥文件（`keeper.Clear`）；非空 = 覆写。空字符串绝不被当成合法密钥写入 |
| D4 | `config` 项数量上限 | 16（`maxConfigItems`） |
| D5 | `onPanel` 槽位渲染的 iframe `kind` | 仍为 `'panel'`（协议不变）；新增 `surface.slot` 字段承载「哪个槽位」 |
| D6 | 迁移的目标插件未安装时 | 不迁移、不写标记、不报错（下次启动再试）；错误只用于「已确定要搬但搬失败」 |

## 12. 不变量与回归锚点（每条都有任务）

1. 窗口宽高与位置在浮层面/面板/气泡/槽位四个状态下都不变（`verify-region.ps1`：T121；`verify-live.ps1`：T122）。
2. `Layout.pluginSurface` 的矩形集合 == `desktop.SetWindowRegion` 收到的矩形集合（`computePluginSurface` 单测 + T53）。
3. 插件 iframe 的 `sandbox` 恰为 `allow-scripts`（harness 第 19 节：T107；单测：T118）。
4. `internal/llm/**` 未被改动（T135 的 `git diff --stat` 判据：该目录零改动）。
5. 未授予权限的调用**状态一点不变**（既有 `TestUnauthorisedCallsChangeNothing`，新增 `config.set` 一例：T37、T46）。
6. 迁移幂等（T63）与失败不动原文件（T64）。
7. `plugin.config` 项的密钥**从不出现在任何返回值、也从不以明文落盘**（T38、T41、T42）。
8. 配置化白名单不能放行任意域名（T46：空值、非法 scheme、含 `*` 三类拒绝路径各一条断言）。
