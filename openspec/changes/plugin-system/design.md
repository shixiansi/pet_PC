# design.md — plugin-system

> 本文件把规格卡/澄清清单的 `impl_decisions` 收敛为**唯一路径**，不留未定分支。凡与规格卡字面口径不符处，见 §13 的收敛决定（C1–C4）与实际取值裁定（D1–D5）。
> 任务编号与 §12 依赖顺序、§11 边界表一一对应；tasks.md 是本设计唯一的执行清单。

## 1. 硬约束与设计边界（全部来自已实读的仓库事实）

| 约束 | 事实出处 | 本设计的遵守方式 |
|---|---|---|
| 窗口**永不 resize**，只改区域 | `app.go:510-531`（`totalW = petW + gap + panelW`）、`README.md:515-529` | 插件浮层面**只**进 region switch，不调用 `SetBounds`；`main.go` 的 `Width/Height` 与 `DisableResize` 不动 |
| 面板矩形固定 380×520 | `internal/store/store.go:225-235` | 插件面板视图**不新增任何窗口几何**：iframe 撑满槽位内容矩形，内部滚动。【已调整】槽位由 `#tab-plugins` 改为 `#chat-slot`（面板的「对话」tab），因为「插件」tab 已删除；本前提由 `openspec/changes/plugin-market-app-and-chat-plugin` 推翻（管理面搬进独立程序后宿主不再需要那个 tab） |
| Wails v2 无多窗口 API；类名 `wailsWindow` 唯一 | `internal/desktop/desktop_windows.go:21`、`docs/llm-chat.md:203-222` | 不创建第二个窗口、不新起程序；市场与插件界面都长在既有窗口内。【已调整】该结论**只对同一个进程内的第二个窗口成立**，对本 change 不成立：插件市场与配音工坊是**独立的 Wails 应用**（各有自己的 module、`wails.json` 与进程），不是同一进程的第二个窗口，因此不撞类名冲突；本前提由 `openspec/changes/plugin-market-app-and-chat-plugin` 推翻 |
| 区域必须等于「页面真正绘制的东西」 | `README.md:541-567` | 浮层面矩形由 Go 纯函数算出一次，同时用于 region 与 CSS 下发；前端**绝不**自行推导尺寸 |
| 区域只支持矩形并集（可圆角） | `internal/desktop/desktop_windows.go:337-389` | 声明矩形逐块进并集，圆角经 `RegionRect.Radius` |
| 四态互斥（面板/气泡/浮层） | `app.go:977-1001`、`app_pomodoro.go:1069-1071`、`app_llm.go:1033-1053` | 插件浮层面为第四态，四个开口互相清理（§7） |
| 门禁脚本硬编码清单 | `.dev/check-frontend.cjs:132/148-152/179/346/1297/1184-1188/105/117-121`、`.dev/run-frontend-harness.cjs:76-97`、`frontend/src/test-harness.js:47-65` | 门禁同步为必做任务（T100–T114，含 T107b） |
| 设置页控件白名单（7 个 `.sgroup`、id 集合全等） | `.dev/check-frontend.cjs:223-230`、`:309-331` | 市场基址输入框放**插件 tab**（T98）；设置页 markup 与 id 集合**逐字不改**（T73、T120 判据） |
| Go 1.22 / wails v2.11.0，只用标准库 | `go.mod:1-10` | `internal/plugins` 只用 `archive/zip`、`crypto/sha256`、`encoding/json`、`net/http`、`embed`；`go.mod` 不变 |
| 工具链须 dot-source；构建入口 `npm run build` → `wails build -s` | `.dev/env.ps1:1-41`、`wails.json:5-6`、`README.md:12-24` | tasks.md 的判据全部使用这些既有命令，不发明新入口 |
| `raw.githubusercontent.com` 不通，用 `gh-proxy.com` 绕行 | `docs/genshin-tts.md:479/688-689` | 市场基址默认值即该镜像前缀（§2.1） |

## 2. 数据模型

### 2.1 配置（`internal/store/store.go`）

| 字段 | 类型 / tag | 语义 |
|---|---|---|
| `MarketBaseURL` | `string` `json:"marketBaseUrl"` | 插件市场基址。缺省 = `DefaultMarketBaseURL`；用户显式清空（空串）表示「强制用内置示例源」 |

- `DefaultMarketBaseURL = "https://gh-proxy.com/https://raw.githubusercontent.com/shixiansi/pet-pc-plugins/main"`（镜像前缀指向 `raw.githubusercontent.com`，与 `docs/genshin-tts.md:688` 同一写法）。
- `Config.Normalise()` 末尾追加规范化：去首尾空白；去掉结尾多余的 `/`；**空串保持空串**（空 = 强制内置，不是「恢复默认」）。因此「缺省」由 `DefaultConfig()` 提供，而 `Normalise` 不把空串写回默认值（老 `config.json` 无该键 → `readJSON` 在默认值上解码 → 保持默认）。
- 索引地址 = `base + "/index.json"`；条目 `download` 为相对路径时 = `base + "/" + download`，为 `http(s)://` 绝对地址时直接使用（单基址，不做多源聚合，规格卡范围外第 9 项）。
- **不接受**非 `http`/`https` 基址与 download：`market.go` 校验 scheme，拒绝时返回 `fmt.Errorf("市场地址只支持 http/https：%s", u)`。

### 2.2 已装索引（`<dataDir>/plugins.json`）

```json
{
  "version": 1,
  "plugins": [
    { "id": "com.shixiansi.plugin-chat", "version": "1.0.0", "enabled": true,
      "granted": ["llm.chat", "pet.bubble", "pet.surface"], "installedAt": "RFC3339" }
  ]
}
```

- `granted` 是**已授权权限**的集合（安装时逐项授权后的结果），持久化于此文件；插件被停用时 `enabled=false` 但 `granted` 保留（再次启用不重复授权）。
- 写入用 `internal/plugins` 自带的 `atomicWriteFile`（临时文件 + `os.Rename`），与 `internal/store/store.go:525-539` 的 `writeJSON` 同构（store 的该函数未导出，故复制同一手法而非导出它）。
- 文件损坏/无法解析：重命名为 `plugins.json.bak`（与 `store.quarantine` 同构）并在 `PluginsView.Warnings` 里给出提示；**不**阻断启动。

### 2.3 目录布局（全部在既有数据目录下，`store.DefaultDir()` = `%AppData%\PetPC`）

| 路径 | 内容 |
|---|---|
| `<dataDir>/plugins.json` | 已装索引（§2.2） |
| `<dataDir>/plugins/<id>/` | 插件的解压产物（manifest.json + 前端资源） |
| `<dataDir>/plugin-data/<id>/` | 插件的私有存储（`storage.*` 能力唯一的可写落点） |
| `<dataDir>/plugins-tmp/` | 安装期的临时下载/解压目录，安装结束（成功或失败）后清空 |

`<id>` 必须是 §3.1 校验通过的合法 id（`^[a-z0-9]+(\.[a-z0-9][a-z0-9-]*)+$`），因此它不含路径分隔符，`filepath.Join` 不可能逃出上述前缀；`manager.go` 在任何 `Join` 前先跑 `validID`。

### 2.4 面向前端的视图（`app_plugins.go`）

```go
// PluginView 是「已装插件」一行。
type PluginView struct {
    ID, Name, Version, Author, Description string
    Enabled     bool
    Permissions []string `json:"permissions"`      // manifest 声明的全集
    Granted     []string `json:"granted"`          // 已授权的子集
    Pending     []string `json:"pending"`          // 声明但未授权（= Permissions - Granted），供「授权」按钮预填
    NetHosts    []string `json:"netHosts,omitempty"`
    Panel       bool     `json:"panel"`
    Surface     bool     `json:"surface"`
    Dir         string   `json:"dir"`
    Error       string   `json:"error,omitempty"`  // manifest 校验失败的说明（该行不可启用）
}

// MarketEntry 是市场列表一行（index.json 的投影）。
type MarketEntry struct {
    ID, Name, Version, Author, Description string
    Permissions []string
    NetHosts    []string `json:"netHosts,omitempty"`
    Download    string
    SHA256      string
    Installed   bool   `json:"installed"`
    InstalledVersion string `json:"installedVersion,omitempty"`
}

// PluginSurfaceRect 是浮层面一块矩形（CSS 像素，相对窗口左上角）。
type PluginSurfaceRect struct {
    Index int     `json:"index"`
    X, Y, W, H, R float64
}

// PluginSurfaceView 随 Layout 下发；nil 表示没有浮层面。
type PluginSurfaceView struct {
    PluginID string              `json:"pluginId"`
    Rects    []PluginSurfaceRect `json:"rects"`
}

// PluginsView 是「插件」tab 的整页快照（PluginsView / UninstallPlugin /
// SetPluginEnabled / GrantPluginPermissions 的返回体）。
type PluginsView struct {
    Plugins  []PluginView `json:"plugins"`
    Warnings []string     `json:"warnings,omitempty"`
    Base     string       `json:"base"`   // 当前 MarketBaseURL，供界面回填地址输入框
}

// PluginEventView 是 Go→插件事件下行的唯一载荷（Wails 事件名 pet:plugin-event）。
type PluginEventView struct {
    PluginID string `json:"pluginId"`
    Name     string `json:"name"`   // todos.changed / timer.changed / llm.delta / llm.done / llm.error
    Payload  any    `json:"payload"`
}
```

`Layout` 新增 `PluginSurface *PluginSurfaceView \`json:"pluginSurface,omitempty"\``（与 `Bubble`/`Live` 同构：几何与内容同一条消息，见 `app.go:465-481` 的理由）。上述类型全部要在 `frontend/wailsjs/go/models.ts` 中生成对应 class（T52h 判据），`Layout` 的 `pluginSurface?: PluginSurfaceView` 由 T120 的 PASS 清单断言。

## 3. 插件包格式与安装事务

### 3.1 manifest（`internal/plugins/manifest.go`）

```json
{
  "id": "com.shixiansi.plugin-chat",
  "name": "对话插件",
  "version": "1.0.0",
  "author": "shixiansi",
  "description": "…",
  "permissions": ["llm.chat", "pet.bubble", "pet.surface"],
  "panel": true,
  "surface": [{ "dx": 160, "dy": 10, "w": 300, "h": 220, "radius": 14 }],
  "entry": "panel.html"
}
```

> 上面这份是本仓库的**字段全集示例**（`netHosts` 未出现是合法的：只有声明 `net.fetch` 时才必须给出）。官方示例插件 `com.shixiansi.plugin-chat` 的 manifest 逐字取这份（`plugin-market` M3 判据）。

| 字段 | 规则（唯一口径） |
|---|---|
| `id` | 必填；`^[a-z0-9]+(\.[a-z0-9][a-z0-9-]*)+$`；长度 ≤ 96 |
| `name`/`author`/`description` | 必填（非空，`name` ≤ 64，`description` ≤ 512） |
| `version` | 必填；语义化 `^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$` |
| `permissions` | 必填（可为空数组）；每个元素必须是 §4.1 的 9 项之一，重复项拒绝 |
| `netHosts` | 选填；每个元素是 `host` 或 `host:port`（`^[A-Za-z0-9.-]+(:\d+)?$`）；**声明了 `net.fetch` 却无 `netHosts` 视为非法** |
| `panel` | 选填 bool |
| `surface` | 选填；数组长度 ≤ `maxSurfaceRects = 4`；每项 `w>0 && h>0 && w<=2000 && h<=2000`、`radius>=0 && radius<=200`、`|dx|<=4000 && |dy|<=4000` |
| `entry` | 必填；相对路径、`.html` 结尾、不含 `\` 与 `..`、不以 `/` 开头；`panel` 与 `surface` 均未声明时拒绝（插件至少要有一种界面落点） |

解析入口：`ParseManifest(data []byte) (Manifest, error)`（纯函数，无 I/O）与 `LoadManifest(dir string) (Manifest, error)`（读 `<dir>/manifest.json`）。所有校验失败都返回带字段名的中文错误，不做静默修正。

### 3.2 安装事务（`Manager.Install`）

固定顺序，任一步失败即整体回滚（删除本次创建的临时目录与目标目录，**已装索引不变**）：

1. 拉取/查找市场条目（§5），取 `download` 与 `sha256`；
2. **校验**：`sha256` 必须是小写 64 位十六进制，否则拒绝（`"市场条目的 sha256 格式非法"`）；
3. 下载（或读本地）zip 到 `<dataDir>/plugins-tmp/<id>.zip`，同时流式计算 `sha256`；与期望值**不相等即拒绝**，删除临时文件，返回 `fmt.Errorf("sha256 校验失败：期望 %s，实际 %s", want, got)`；
4. 解压到 `<dataDir>/plugins-tmp/<id>/extract`：**逐条**遍历 zip 条目，拒绝绝对路径（`/` 开头、盘符）、`..` 段、以及解析后不在目标前缀内的路径（`filepath.Rel` 后以 `..` 开头即拒绝），拒绝 symlink/非普通文件条目；单条目大小上限 32 MiB、总解压上限 128 MiB；
5. 读 `<extract>/manifest.json` 并 `ParseManifest`；**包内 manifest 是权威**——若市场条目的 `permissions`/`netHosts` 与包内 manifest 不一致，拒绝安装（`"市场条目与插件包的权限声明不一致"`），避免「展示的权限」与「实际申请的权限」脱钩；
6. `os.Rename` 把 `<extract>` 移到 `<dataDir>/plugins/<id>`（目标已存在则先拒绝：`"插件已安装，请先卸载"`）；
7. 写入索引条目（`enabled=false`、`granted=[]`——**安装完成但未启用**，启用与授权由用户在「插件」tab 完成）；
8. 清空 `plugins-tmp`。

`Manager.Uninstall(id)`：删除 `<dataDir>/plugins/<id>` **与** `<dataDir>/plugin-data/<id>`，从索引移除条目并落盘；`id` 不存在时返回 `"插件未安装"` 且不改任何状态。删除私有存储是 D5 的收敛结论（§13）。

`Manager.SetEnabled(id, on)`：只改索引条目的 `enabled` 并落盘；`on=true` 且索引条目的 `granted` 为空时允许（启用≠授权：未授权的能力调用照样被 §4 拒绝）。

## 4. 权限模型与门控调度

### 4.1 九项权限（固定，`internal/plugins/capability.go`）

`todos.read`、`todos.write`、`pet.bubble`、`timer.read`、`events`、`net.fetch`、`storage`、`pet.surface`、`llm.chat`。

### 4.2 方法表（唯一映射；方法名不在此表内即为未知方法）

| 方法 | 需要权限 | 行为 |
|---|---|---|
| `todos.list` | `todos.read` | 返回 `[]store.Todo` 的投影（复用 `a.store.Todos()`） |
| `todos.add` / `todos.update` / `todos.toggle` / `todos.remove` | `todos.write` | 复用既有 `AddTodo`/`UpdateTodo`/`ToggleTodo`/`DeleteTodo` 的 store 层调用 |
| `timer.state` | `timer.read` | 返回与 `PomodoroView` 同源的状态（复用 `a.pomodoroView()`） |
| `events.subscribe` | `events` | `names` 只能是 `timer.changed`、`todos.changed`；宿主在状态变化后以 `event` 消息推送 |
| `bubble.say` | `pet.bubble` | 走既有 `a.showBubble(&BubbleView{Kind: BubbleReminder, Title: text})`（§13 D1） |
| `net.fetch` | `net.fetch` | `url` 必须 `http`/`https` 且 `host[:port]` 命中该插件 manifest 的 `netHosts`；否则拒绝。**返回**响应，不落盘 |
| `storage.get` / `storage.set` / `storage.remove` | `storage` | 只能在 `<dataDir>/plugin-data/<id>/` 下；`key` 必须匹配 `^[A-Za-z0-9_-]{1,64}$`（防穿越与超长名） |
| `storage.dir` / `storage.stat` | `storage` | 前者给调用者自己的私有目录（启动下载器、让协议端按路径读文件都要它）；后者给某个 key 的**真实字节数**（`{exists,bytes}`，文件还没出现时是 `exists:false` 而不是报错）。`storage.stat` 存在的理由：**「下载完了没有」不能靠时钟判断** —— 同样 60 MB，一条宽带四分钟、一条千兆四十秒；体积可以和 playurl 给的预估值比，这也正是参考实现的做法（xunlu-core 的 `fs.statSync(resultPath).size` 对比上限） |
| `surface.show` / `surface.hide` | `pet.surface` | 走 §6/§7 的浮层面开关：`surface.show` 复用 `OpenPluginSurface`、`surface.hide` 复用 `ClosePluginSurface`（**不新增第二条开关路径**） |
| `llm.chat` | `llm.chat` | 复用既有 LLM 运行时的设置/密钥/用量统计与流式事件（§4.4） |

### 4.3 单一调度入口与拒绝语义

`PluginRequest(pluginID, method string, payload string) (string, error)`（`app_plugins.go`）是**唯一**绑定入口；宿主侧所有能力都收口于此，不为每个能力开绑定。

拒绝顺序（固定，先鉴权后执行）：
1. 插件未安装/已停用 → `fmt.Errorf("插件 %s 未启用", id)`；
2. 方法未在表中 → `fmt.Errorf("插件 %s 调用了未知方法 %s", id, method)`；
3. 权限未授予 → `fmt.Errorf("插件 %s 未获权限 %s", id, perm)`；
4. 参数非法（URL/域名/key/JSON）→ 具体错误。

**未授权的调用在被拒时不得改变任何状态**（不改待办、不弹气泡、不发网络请求、不写索引、不下发 Layout）——由 T35 的判据逐项断言。

### 4.4 `llm.chat` 的实现（绝不新起一条 LLM 路径）

- 复用 `a.llmSettings(a.store.Config())`（`app_llm.go:415`）与 `a.llmClient()`（`:427`），因此密钥、基址、模型、历史与**每日用量上限**全部沿用既有配置；插件不保存任何密钥。
- 流式：宿主以 `event` 消息推 `llm.delta`（`{requestId, text}`）、`llm.done`（`{requestId, text}`）、`llm.error`（`{requestId, message}`）给发起方那个插件的 iframe；每次 `llm.chat` 生成新的 `requestId`（`crypto/rand` 十六进制 16 字节），旧 `requestId` 的迟到事件在插件侧被忽略（前端按 `requestId` 过滤）。
- 未配置 LLM / 未就绪 / 超预算：直接返回错误（文案取既有 `view.NotReadyReason`），**不**做静默降级；示例插件据此显示「宿主未配置大模型」。
- `llm.chat` 的回复**不写入主转写**（既有 `#tab-chat` 的会话保持原样），因此不触碰 `app_llm.go` 的会话状态机。

**事件下行（唯一路径，钉死事件名）**：插件事件一律经 Wails 事件 `pet:plugin-event` 从 Go 侧下发，载荷固定为 `PluginEventView{pluginId, name, payload}`，**必须以双引号字面量**发出（`a.emitEvent("pet:plugin-event", …)`；双引号是 `check-frontend.cjs:137` 的 `emitEvent\("([^"]+)"` 扫描口径，用 Go 常量会退化成标识符而让门禁判成「订阅了没人发的事件」）。发出点两处：① `events.subscribe` 订阅的 `timer.changed`/`todos.changed` 状态变化处；② `llm.chat` 流式回调的 `llm.delta`/`llm.done`/`llm.error`。前端 `plugin-host.js` 用 `EventsOn('pet:plugin-event', …)`（`frontend/wailsjs/runtime/runtime`，与 `main.js:21` 同一入口）按 `pluginId` 路由到该插件的 iframe 并以 `sendEvent` 投递 `{v:1,type:'event',name,payload}`（T50b、T88b）。

## 5. 市场源（`internal/plugins/market.go`）

`Manager.Market() (MarketView, error)` 的固定顺序：

1. 读 `cfg.MarketBaseURL`；为空 → 走内置源（第 4 步），并置 `Fallback=true`、`Notice="未配置市场地址，正在使用内置示例源"`；
2. `GET base + "/index.json"`，`Timeout = 8s`，`http.Client` 只允许 `http`/`https`（拒绝其他 scheme）；非 2xx → 视为不可达；
3. 解析并**校验每条**：`id`/`version`/`download`/`sha256` 必填，`permissions` 元素必须是 9 项之一，`download` scheme 合法；单条非法 → 丢弃该条并在 `Notice` 中累计说明（不整批失败）；整个 JSON 无法解析 → 视为不可达；
4. 不可达或空基址 → 读内置源（§5.1），仍置 `Fallback=true` 与对应 `Notice`（`"市场不可达，正在使用内置示例源"`）；
5. `Installed` 标记按索引比对（`id` 相同即 `Installed=true`，`InstalledVersion` 取索引里的版本）。

`MarketView = { base string; fallback bool; notice string; entries []MarketEntry }`。

### 5.1 内置示例源（C1 的收敛）

- 位置：`internal/plugins/sample/index.json` 与 `internal/plugins/sample/com.shixiansi.plugin-chat-1.0.0.zip`，**两个文件都提交进主仓**，用 `//go:embed all:sample` 嵌入（`sample/embed.go`）。
- `download` 写**相对文件名**；解析到内置源时把相对路径解析为 `embedded://` 前缀并由 `Manager` 从 `embed.FS` 读字节（与远端下载共用第 3 步的 sha256 校验路径，因此内置源同样过校验）。
- 该 zip 与市场仓 `market/dist/com.shixiansi.plugin-chat-1.0.0.zip` **同字节**（由 `plugin-market` M5 的打包脚本产出后复制过来），`index.json` 的 `sha256` 取真实计算结果；两侧一致性由 `plugin-system` T32 与 `plugin-market` M6 各钉一侧。

## 6. 插件浮层面几何（Go 侧纯函数，唯一算法）

```go
// app_plugin_surface.go（根包，与既有 computeLiveOverlay 同构）
// computePluginSurface 输入宠物矩形（物理像素）、缩放、窗口尺寸、声明矩形（CSS 像素），
// 输出 region 用的物理像素矩形与随 Layout 下发的 CSS 视图。
func computePluginSurface(petX, petY, petW, petH, totalW, totalH int, s float64, rects []plugins.Rect) ([]desktop.RegionRect, *PluginSurfaceView)
```

放在根包而非 `internal/plugins`：它要返回 `desktop.RegionRect` 与 `PluginSurfaceView`（前端视图类型），与 `computeLiveOverlay`（`app_live.go:156-243`）同一层次；`internal/plugins` 保持只依赖标准库、不依赖 `internal/desktop`。声明矩形的类型 `plugins.Rect`（`dx/dy/w/h/radius`）由 `internal/plugins/manifest.go` 提供，因此两边无循环依赖。

固定算式（`round(v) = int(v*… + 0.5)`，与 `app.go:522-524` 同一取整口径）：

1. 逐块：`x = petX + round(dx*s)`，`y = petY + round(dy*s)`，`w = round(w*s)`，`h = round(h*s)`；
2. 夹回窗口：若 `w > totalW` → `w = totalW`；`x` 夹到 `[0, totalW-w]`；`y`/`h` 同理（`totalH`）；
3. 夹回后 `w <= 0 || h <= 0` → **丢弃该块**；
4. 圆角：`r = round(radius*s)`，再 `r = min(r, min(w,h)/2)`；
5. 保留的块按下标顺序编号（`Index` 为声明序，不重排），`regions` 为物理矩形（`Radius: r`），`view.Rects` 为同样的矩形除以 `s` 得到的 CSS 值（与 `app.go:726-729` 把 `bubblePx` 转 CSS 的做法同构）；
6. **全部被丢弃 → 返回 `(nil, nil)`**：不下发 `Layout.PluginSurface`、不扩区域，且 `OpenPluginSurface` 返回 `fmt.Errorf("插件 %s 的浮层面完全在窗口之外", id)`（§11）。

因此「区域用到的矩形集合」与「前端摆放用的矩形」**同源**，二者不可能漂移。

## 7. 窗口区域与四态互斥

### 7.1 region 分支（`app.go` 的 `applyWindowGeometry`）

```go
switch {
case open:            // 面板（含插件 tab 的面板视图）
    regions = []desktop.RegionRect{ panelRect, petRect }
case live:            // 实时交流浮层（既有三块 + 宠物）
    regions = append(liveRegions, petRect)
case pluginSurface != nil: // 插件浮层面（1..4 块 + 宠物）—— 新增分支
    regions = append(surfaceRegions, petRect)
case hasBubble:
    regions = []desktop.RegionRect{ bubbleRect, petRect }
default:
    regions = []desktop.RegionRect{ petRect }
}
```

- 分支顺序只是兜底；四态本已互斥（§7.2），同时为真时以 `open > live > pluginSurface > bubble` 优先，且只有被选中的那一态进 region/layout。
- 浮层面的矩形集合与 `Layout.PluginSurface` 由同一次 `computePluginSurface` 调用的返回值提供（`app.go:734-737` 对 `Live` 的同一手法）。

### 7.2 互斥开关（四个入口，各自清理其余三态）

| 入口 | 面板 | 实时浮层 | 插件浮层面 | 气泡 |
|---|---|---|---|---|
| `SetPanelOpen(true)`（`app.go:977`） | 开 | 关 | **关（新增）** | 关 |
| `OpenLive`（`app_llm.go:1033`） | 关 | 开 | **关（新增）** | 关 |
| `showBubble`（`app_pomodoro.go:1069`） | 关 | 关 | **关（新增）** | 开 |
| `OpenPluginSurface`（新增） | 关 | 关 | 开 | 关 |

清理由一个私有方法 `setPluginSurfaceLocked(view *PluginSurfaceView)` 承担（与 `setBubbleLocked`/`setLiveLocked` 同构：**唯一**改 `a.pluginSurface` 与 `a.layout.PluginSurface` 的入口，避免「清了状态但 layout 还宣称有」的既有那类 bug——见 `app_bubble_test.go:34-41` 的回归注释）。

`OpenPluginSurface` 在存在未回答提问气泡时**拒绝**且不改任何状态（与 `app_llm.go:1033-1040` 同构）：

```go
if kind := a.unansweredQuestionKind(); kind != BubbleNone {
    return layout, errors.New(pluginSurfaceBlocked(kind)) // 文案含 kind 字面量
}
```

## 8. 资源路由（`main.go` 的 `assetHandler`）

`assetHandler.ServeHTTP` 在既有 `/pet-image` 分支旁新增：

| 路径 | 行为 |
|---|---|
| `/plugin-asset/@host/bridge.js` | 返回**编译期常量**的桥接脚本（`const pluginBridgeJS = "…"`，不是文件），`Content-Type: text/javascript`，`Cache-Control: no-store`，`Access-Control-Allow-Origin: *`；由 T68 断言其内容与 `plugin-host.js` 的消息协议一致（同一份常量在两端各有一份字面校验） |
| `/plugin-asset/<id>/<relpath>` | 解析 `<dataDir>/plugins/<id>/<relpath>`；`validID` 与逐段路径校验（拒绝 `..`、`\`、绝对路径、解析后越过插件根）；`<id>` 未安装或 `enabled=false` → `403`；未找到 → `404`；命中 → 按扩展名给 MIME、`Access-Control-Allow-Origin: *` 并用 `http.ServeContent`（与 `servePetImage` 同构，`app.go:1174-1205`） |

- 该路由服务的是**已授权且已启用**的插件文件，且只读；写路径不存在。
- `bridge.js` 与 `plugins/<id>/` 同源（`http://wails.localhost` 或资产服务源），因此 sandbox iframe 的 `src` 与子资源（`app.js`/`style.css`）能加载，而因为**不设 `allow-same-origin`**，iframe 的 document 源是不透明的：父页面 DOM 不可达（C3、§13）。

## 9. 前端结构

### 9.1 模块

| 模块 | 职责 | 参照范式 |
|---|---|---|
| `frontend/src/plugins.js` | 插件 tab 的管理视图（已装列表/启停/卸载/权限查看/市场列表/安装/权限对话框/市场地址输入）+ 面板视图容器（iframe 挂载与「返回」） | `frontend/src/chat.js`（自包含组件、`constructor(els, {onState, fail, debug})` + `wire()`） |
| `frontend/src/plugin-host.js` | 沙箱 iframe 的创建、`postMessage` 桥接、**按 Layout 矩形摆放**、面板视图与浮层面的承载 | `frontend/src/live.js`（`applyGeometry(live)` 里那句 `place(el, r)` 的写法照搬，`live.js:203-243`） |

`main.js` 只做四件事（每件都是独立任务）：`els` 表登记与 `import`、`switchTab` 新增一行字面 toggle（既有 4 行**逐字保留**，门禁正则要求 `tabChat.classList.toggle('hidden', name !== 'chat')` 字面存在）、`applyLayout` 里把 `layout.pluginSurface` 交给 `pluginHost.applyGeometry(...)`（与 `live?.applyGeometry(layout.live || null)` 同一行风格）、`wire()` 里接线。

### 9.2 沙箱与桥接协议（唯一路径，C3）

- 每个插件界面 = **一个** `<iframe sandbox="allow-scripts" src="/plugin-asset/<id>/<entry>">`；**不设** `allow-same-origin`（形成不透明源）、不设 `allow-popups`/`allow-top-navigation`/`allow-forms`。
- 面板视图：iframe 撑满 `#plugin-panel-view`（无窗口几何，§1）；浮层面：第 i 块矩形 → 第 i 个 iframe，位置尺寸**逐字取自** `Layout.PluginSurface.Rects[i]`。
- 消息（`postMessage(targetOrigin = '*')`，消息体 `v: 1` 版本号）：

| 方向 | `type` | 载荷 |
|---|---|---|
| 插件→宿主 | `ready` | `{}`（插件脚本启动即发；宿主据此回投 `init`，握手顺序固定为 ready→init） |
| 宿主→插件 | `init` | `{pluginId, permissions, granted, surface:{kind:'panel'|'surface', rectIndex, rectCount, rect}}` |
| 插件→宿主 | `request` | `{id, method, payload}` |
| 宿主→插件 | `response` | `{id, ok, result?, error?}` |
| 宿主→插件 | `event` | `{name, payload}`（`timer.changed`/`todos.changed`/`llm.delta`/`llm.done`/`llm.error`） |

- **握手**：宿主在 iframe 加载完成后等待**仅来自该 iframe** 的 `{v:1,type:'ready'}`，随即回投**恰好一条** `init`（字段全集见上表）；面板视图 `surface.kind='panel'`（`rectIndex`/`rectCount`/`rect` 缺席），浮层面 `surface.kind='surface'` 且第 i 个 iframe 的 `rectIndex == i`、`rectCount` 为本次矩形总数、`rect` 为该块矩形自身（C4）。非本 iframe 的 `ready` 不回投（T84b、T84c）。

- 宿主只接受来自**自己创建的那个 iframe 的 `contentWindow`** 且 `event.source === iframe.contentWindow` 的消息；`method` 必须是非空字符串、`id` 必须是非空字符串，否则丢弃且不回消息。
- 宿主统一调用 `App.PluginRequest(pluginId, method, JSON.stringify(payload))`，把结果包成 `response`；`error` 非空即 `ok:false`（错误文案原样透传，便于插件显示「未获权限 X」）。
- 真实的插件脚本写得再糟也拿不到主页面：它只能通过这四类消息与宿主对话，宿主侧能力再经 §4 门控。

## 10. 与 `plugin-market`（市场仓）的接口契约

| 契约 | `plugin-system` 侧的钉法 | `plugin-market` 侧的钉法 |
|---|---|---|
| `market/index.json` 的条目字段 | T28、T29（解析与校验：`id`/`version`/`download`/`sha256` 必填、`permissions` 合法、单条非法只丢该条） | M2（按同一字段集写 `index.json`） |
| zip 与 sha256 同字节一致 | T32 断言内置源 zip 的实算 sha256 == `sample/index.json` 的 `sha256` | M6 断言 `market/dist/*.zip` 的实算 sha256 == `market/index.json` 的 `sha256` |
| 主仓内置源与市场仓 dist 同字节 | T32 断言 `internal/plugins/sample/*.zip` 的 sha256 == `market/dist/*.zip` 的 sha256 | M6 断言两者 sha256 相同 |
| manifest 字段集与校验规则 | T10–T15 | M3（示例插件 manifest 逐字段符合 §3.1） |
| 市场条目权限与包内 manifest 一致 | T20（不一致即拒绝安装） | M6（从包内 manifest 回写条目字段） |
| 安装目录位置 `<dataDir>/plugins/<id>/` | T17 | M8（README 写明该位置） |

## 11. 边界与错误处理（每条都有任务映射）

| 场景 | 终态 | 任务 |
|---|---|---|
| manifest 缺必填字段 / `id` 格式非法 | `ParseManifest` 返回含字段名的错误，安装停在解析步，索引不变 | T10 |
| `entry` 非法 / 无任何界面落点 | 同上 | T11 |
| `version` 非语义化 | 同上 | T12 |
| `permissions` 含非法名或重复 | 同上 | T13 |
| `surface` 超 4 块 / 尺寸越界 | 同上 | T14 |
| `netHosts` 格式非法 / 声明 `net.fetch` 却无白名单 | 同上 | T15 |
| sha256 不匹配 | 拒绝安装，索引不变，临时文件被删 | T16 |
| zip 解压条目为绝对路径或含 `..` | 拒绝安装，`plugins/<id>` 不创建，索引不变 | T18 |
| zip 条目超出体积上限 | 拒绝安装并回滚 | T19 |
| 市场条目权限与包内 manifest 不一致 | 拒绝安装 | T20 |
| 市场不可达 / 基址为空 | 返回内置源条目 + `Fallback=true` + `Notice`，界面给出提示；**不报错中断** | T30 |
| 单条市场条目字段非法 | 丢弃该条 + 累计 `Notice`，其余条目照常返回 | T29 |
| 基址或 download 非 http(s) | 拒绝，错误文案含 `http/https` | T26、T27 |
| 索引文件损坏 | 隔离为 `.bak` + `Warnings()` 提示，以空索引继续 | T9 |
| 调未知方法 | 错误 `…调用了未知方法…`，无副作用 | T33 |
| 未启用插件调用任何方法 | 错误 `…未启用…` | T34 |
| 调未授权方法 | 错误 `…未获权限 <perm>…`，**不改任何状态** | T35 |
| `net.fetch` 域名不在白名单 | 拒绝，且**不发出**请求 | T46 |
| `storage` key 非法 | 拒绝，不落盘 | T44 |
| 插件停用 | 索引 `enabled=false`，`/plugin-asset` 对该 id 返回 403，浮层面关闭、区域回落 | T21、T66、T71 |
| 卸载 | `plugins/<id>` 与 `plugin-data/<id>` 均删除，索引移除，区域回落 | T25、T67 |
| 浮层面全部越界 | 不下发、不扩区域、`OpenPluginSurface` 返回错误 | T56、T64 |
| 有提问气泡时打开浮层面 | 拒绝且不改状态（面板/气泡/计时器全不变） | T65 |
| 市场地址输入非法（非 http/https） | 保存被拒绝并给出提示，`config.json` 不变 | T26、T99 |
| `llm.chat` 未配置/未就绪 | 返回错误、不计用量、不发事件 | T50 |
| Go→插件状态/流式下行（事件名漂移或漏发） | 事件名钉死 `pet:plugin-event` 且以双引号字面量发出；`pluginId` 不符的帧被前端丢弃；`plugin-host.js` 的订阅扫描与 `goFiles` 必须同时覆盖（否则订阅被判为无人发出） | T50b、T88b、T100、T106 |
| 插件界面在 `init` 之前猜权限 | 插件在收到 `init` 前不发 `request`；宿主对未授权调用照常拒绝（双保险） | T84b（宿主侧握手）、`plugin-market` M4（插件侧） |
| 新增绑定未导出 / 未进 `models.ts` | `check-frontend.cjs` 三条断言（含 T107b 新增的 `pluginSurface` 断言）+ harness `methods` 白名单在 T120/T121 一次性核对 | T52a–T52h、T107b、T108、T113、T120、T121 |
| 门禁脚本自身未纳入版本库（被 `.gitignore` 忽略） | 第 4 个门禁脚本按既有约定放行：`.gitignore` 追加 `!.dev/verify-plugin-surface.ps1` | T5b、T6、T125 |
| 无交互桌面的像素门禁 | 报告「无交互桌面」而非失败 | T123、T124、T125、T126 |
| 浮层面的像素判据 | 新增独立脚本 `.dev/verify-plugin-surface.ps1`，既有两个像素门禁不改 | T125、T122 |

## 12. 任务依赖顺序（可执行性）

```
T1 origin → T2 main → T3 提交（两个 change 共 6 个 openspec 文件）→ T4 feat/plugin-system → T5 .gitignore 市场仓 → T5b 放行 verify-plugin-surface.ps1 → T6 既有规则逐条未变
T7 Config.MarketBaseURL → T26–T28（市场源解析）
T31 内置示例源落盘（前置 T7、T9、T16、T17；只依赖配置、索引与既有安装链路，不依赖 T30）→ T30（不可达/空基址回退，读 T31 落盘的内置源）→ T32（sha256 一致性）
T8 索引持久化 → T9 索引隔离 → T16/T17/T21/T22/T24/T25（安装与生命周期）
T10–T15 manifest 校验 → T16
T8/T17 → T33（调度入口）→ T34 → T35 → T36–T51（各项能力）
T47/T49 → T50b（pet:plugin-event 下行，Go 侧）→ T52（权限与调度表收口）
T52 → T52a–T52g（七个绑定：五个宿主方法 + PluginsView/MarketView 两个视图）
T16 → T68–T71（资源路由：浮层面与面板视图都需要 iframe 源）
T53–T56（纯函数几何）→ T57（Layout 字段）→ T58（region 分支）→ T59（唯一入口）
T59 → T60/T61/T62（三个既有开口清理）→ T63（打开/关闭）→ T64（拒绝路径）→ T65 → T66 → T67
T52a–T52g、T57、T63 → T52h（重新生成 frontend/wailsjs/go/**；排在 T67 之后、T119 之前）
T72 → T73/T74/T76/T79/T83 → T75/T77/T78/T80/T84/T85 → T84b（握手 ready→init）/T86/T87/T88 → T89 → T84c（多矩形 rectIndex）/T88b（前端事件路由）→ T81/T90/T91 → T82/T92/T93 → T94 → T95/T96 → T97/T98 → T99
T72–T99 → T100–T114（门禁脚本同步与单测，含 T107b）→ T115（README）
T35/T52/T58/T71/T32/T114 → T116 → T117 → T118
T52h → T98 → T119 → T120/T121 → T122
T122 → T123/T124/T125 → T126
```

- T100–T114（含 T107b）（门禁同步与单测）必须在 T120/T121 之前完成；T120/T121 又必须在 T119（`npm run build`）之后（两门禁都依赖真实构建产物）。
- **T52h（重新生成 `frontend/wailsjs/go/**`）必须排在 T119 之前，且在 T57（`Layout.PluginSurface` 字段）与 T63（`OpenPluginSurface`/`ClosePluginSurface`，返回 `main.Layout`）之后**：`main.js`/`plugins.js` 的 `App.*` 调用与 `models.ts` 的类型都依赖它，且 T120 的「Layout 模型带 pluginSurface」断言读的就是 `models.ts`（绑定目录已提交，见 `.gitignore:22-23`；`wails build -s` 不重新生成绑定，故不能改由 T122 承担）。T52h 的判据断言 `App.d.ts` 中两个 `main.Layout` 返回类型与 `models.ts` 的 `pluginSurface?: PluginSurfaceView`，这两处产物分别由 T63 与 T57 定义；排在 T67 之后（即全部 `app_plugins.go` 方法就位后）一次生成。
- **T31 的 `[前置]` 含 T16/T17**：T31 的判据对该条内置条目调用 `Install`，而 `Install` 的完整实现与成功落盘由 T16（sha256 校验路径）与 T17（成功安装落盘与索引）给出，T16 经 T10–T15 传递依赖 manifest 校验。
- T84c（多矩形 `rectIndex`）排在 T89 之后：它断言的是 `applyGeometry` 为第 i 块矩形建的 iframe 收到 `rectIndex == i` 的 `init`。
- T88b（前端事件路由）同时依赖 T88（`sendEvent`）与 T50b（Go 侧事件发出者），故排在两者之后。
- **T100 必须先于 T106**：T100 往 `goFiles` 加 `app_plugins.go`，T106 把 `plugin-host.js` 的 `EventsOn('pet:plugin-event', …)` 纳入第 3 节（`check-frontend.cjs:148-152`）的订阅扫描。两者共同保证 `:153` 的 dangling 判定不把该订阅算成「订阅了没人发的事件」；顺序颠倒（先 T106 后 T100）会让 T120 在该中间态 FAIL，为此 T106 的 `[前置]` 显式含 T100。
- **T107b（`models.ts` 的 `pluginSurface` 断言）排在 T107 之后、T119/T120 之前**：T120 的 PASS 清单点名该断言名，而断言本体由 T107b 创建；T107b 的 `[前置]` 为 T107 与 T52h（T52h 已把 `pluginSurface?: PluginSurfaceView` 写进 `models.ts`，断言才有可断言之物），执行顺序上 T52h（T67 之后）早于 T100–T114，故可执行。
- **T5b（`.gitignore` 放行 `.dev/verify-plugin-surface.ps1`）紧随 T5**：T125 新建的门禁脚本要与既有三个门禁脚本同为项目源（`.gitignore:11-16` 的既有约定），T6 的负面判据据此覆盖到第 4 条白名单。
- T123/T124/T125 必须排在 T122（`wails build -s`）之后：它们驱动的是新构建的 exe。
- T126 必须排在 T125 与 T115 之后（既要有可运行的浮层面，也要有 README 里的步骤）。
- `plugin-market` 的 **M2–M6** 必须在本 change 的 **T31/T32** 之前完成：M2/M3 决定内置源 `index.json` 与 `sample/*.zip` 的内容，M5/M6 产出并复制该 zip 与 `index.json`（T31 落盘、T32 断言 sha256 一致）。M1 依赖本 change 的 T5（主仓 `.gitignore` 忽略 `market/`），M3 依赖 T10–T15（manifest 校验规则），M6 依赖 M5。

## 13. 收敛的设计决定（不留未定分支）

| # | 决定 | 理由 |
|---|---|---|
| C1 | 内置示例源放**主仓已提交**的 `internal/plugins/sample/`（`go:embed`），而非清单字面写的 `market/index.json` | `market/` 被主仓 `.gitignore` 忽略（独立 git 仓），`go:embed` 对未提交路径会让主仓构建失败；两侧 zip 同字节 + 两处 sha256 判据保住了「离线兜底」与「同一份产物」两个意图 |
| C2 | 插件的「面板 tab」= **插件 tab 内的面板视图**，不新增/不改名任何顶级 tab | `data-tab` 计数被两处门禁硬断言，且本 change 已把它 4→5；动态增加顶级 tab 会破坏已批准的门禁与「一块槽位」前提 |
| C3 | 沙箱 iframe **只保留** `sandbox="allow-scripts"` + 同源 URL 加载这一条路径；实测受限即 blocked，不就地切降级分支 | 清单 doubts-3 的降级方案（blob/内联注入）会把协议变成两条且在 `allow-scripts`（无 `allow-same-origin`）下 blob 源同样是不透明源、收益不明；由 harness + 真实 exe 门禁一次性验证，结论写进交付报告 |
| C4 | 浮层面第 i 块矩形 → 第 i 个 iframe，`init` 带 `rectIndex/rectCount` | 区域裁剪只能表达矩形并集，单个 entry HTML 无法切成多块；多矩形路径由纯函数单测 + 假桥接覆盖 |
| D1 | 插件气泡 = 既有 `showBubble(BubbleReminder)`（`pet.bubble`） | 复用既有气泡/区域/互斥机制，不新增窗口几何 |
| D2 | `llm.chat` 回复**不写入主转写** | 保持 `#tab-chat` 会话状态机原样（规格卡范围外第 5 项） |
| D3 | `plugins.json` 是「已装 + 授权 + 启停」的**唯一**持久化位置 | 与既有 `config.json`/`todos.json` 同属数据目录，无新增外部依赖 |
| D4 | 安装完成时 `enabled=false`，需用户显式启用 | 用户故事「装之前看到权限并自己决定」要求安装与启用分离 |
| D5 | 卸载**同时删除** `<dataDir>/plugin-data/<id>` | 清单字面「清理该插件私有存储目录之外的任何状态」可两读；取用户故事「干净地回到原状」的更严一读 |
