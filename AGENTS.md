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

> **宿主不认识任何具体的插件，也不自带对话界面。**
> 对话面板是插件用 manifest 的 `panelSlot: "chat"` 声明的槽位
> （[app_chat_slot.go](app_chat_slot.go)）。别在宿主里"顺手加个 AI 功能"——
> 工具调用、长期记忆这类东西要先想清楚它属于宿主还是属于插件。

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
  `AllowedPermissions`，**现在 17 项**，顺序就是安装对话框的绘制顺序。
- **加一项权限要同步七处**（`timer.control` 那次实测就是七处，漏一处就红）：
  1. `capability.go` 的常量——注释里写清它 gates 什么、**不** gates 什么
  2. `AllowedPermissions`（顺序就是安装对话框的绘制顺序）
  3. `PermissionLabels`（Go 侧的唯一权威文案）
  4. 至少一个注册了它的方法（否则 `TestCapabilityTableIsComplete` 红）
  5. `manifest_test.go` 的 `want` 列表 + 测试名里的数字（`…IsTheSeventeen`）
  6. `.dev/check-frontend.cjs` 里那份**硬编码**的期望词表 —— 它故意不自动跟随
     `AllowedPermissions`，因为它的职责正是发现那张表被改了
  7. `tools/plugin-market/frontend/src/main.js` 的 `PERMISSION_LABELS` —— 市场是
     独立程序，后端允许而市场没解释的权限，会让安装对话框出现一个光秃秃的 id

  外加 README 的权限表和标题里的数字。第 6、7 处是这一路最容易漏的：它们不在
  `internal/plugins` 里，改完 Go 侧全绿，静态接线检查才红。
  故意没有 `background` 权限——常驻帧由 `events` / `net.ws` / `net.fetch` 门控。
- 没有方法支撑的权限是一个"用户被问了一个什么也不做的开关"，
  `TestCapabilityTableIsComplete` 和 `TestAllowedPermissionsIsTheSeventeen` 就是拦它的。

### 3.2 窗口只有三个占位，且必须精确裁剪

面板、气泡、插件浮层面**共用同一个位置，同一时刻只能有一个可见**。窗口区域
（`SetWindowRgn`）必须恰好描述那一个；多一块或少一块，页面未绘制的区域就会
露出桌面，变成一条"玻璃带"。这是本项目历史上反复出现的 bug，
见 [docs/reminder-pomodoro.md](docs/reminder-pomodoro.md) 8.2。

### 3.3 插件帧只能这样长

[frontend/src/plugin-host.js](frontend/src/plugin-host.js) 是安全边界：

- `<iframe sandbox="allow-scripts">`，**故意不含** `allow-same-origin`
  （帧是 opaque origin，够不到父文档和存储；`document.domain` 也爬不出来）；
- 唯一通道是 `postMessage`，且只接受**本文件自己创建的** iframe 的 `contentWindow`；
- **前端永远不判断权限**，每次请求都经 `App.PluginRequest` → 上面那个闸门。

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
  - 权限数在 README、代码注释、测试名里各写了一个值（真实是 17）；
  - README 说工具调用"留了 `internal/llm/tools.go` 的位置"——该文件从未存在；
  - README 说"`Provider` 接口已留位"——`internal/llm` 里没有这个接口，
    `Provider` 只是配置字段 `LLMProvider`（服务商预设名）。

  写文档时**去代码里确认标识符真的存在**，比写得好更重要。

## 6. 许可证红线

Coopanion 一类项目是 **AGPL-3.0**。可以读设计、可以理解协议格式，
**不要复制代码、素材或字段命名**——否则本仓库也得按 AGPL 开源。形象贴图的授权
范围另算，尤其不能拿。

## 7. 本机环境已知坑（不是代码问题）

- 系统 Go 是 **1.21**，而 `go.mod` 要 1.22，`env.ps1` 又设了 `GOTOOLCHAIN=local`。
  本地 1.22.12 工具链在 `pet_pc\.dev\gotool\go`（**不在** `.dev\gotool\go`）；
  `env.ps1` 已做两处回退，别再写死一处。
- 受限沙箱下 `.dev` 的子目录不可写 → Go 的 `GOCACHE`/`GOMODCACHE` 要指到工作区
  **外部**的临时目录。**别指到 `scratch/` 下面**：那在里面会让 `./...` 把依赖缓存
  当成被测包（会看到 `FAIL github.com/pkg/errors` 这种莫名其妙的输出）。
- `.git` 与 `.dev` 在受限模式下不可写：`git commit` 和删改文件需要一次性提权。
- `internal/desktop` 里有一批用例要访问 HKCU 的开机自启项，沙箱下必然失败
  （`open Run key: Access is denied`）。托管 runner 上它们是能跑的。
- **改了 `.go` 就必须重建，否则用户看到的永远是上一个二进制。** 「好像没变化」的第一个
  怀疑对象不是代码，是时间戳：`Get-Item build\bin\*.exe` 和刚改过的 `.go` 对一下。
  2026-10-26 一天之内这件事发生了三次（桌宠两次、市场程序一次），每次都被当成"功能没做"。
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
