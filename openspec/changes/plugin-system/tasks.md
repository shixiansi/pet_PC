# tasks.md — plugin-system

> 每条任务是**单一、可独立核对**的终态：一个任务只断言一个可观察结果，一个判据只描述实施完成后的状态（RED/GREEN/REFACTOR、"先失败再通过"属 D 阶段执行协议，此处不写）。
> **粒度规则（本文件唯一口径）**：一个任务 = 一条独立规则 / 一个权限 / 一个接线点 / 一个门禁改动面 / 一条构建-验收结论。「拒绝非法输入」与「接受合法输入」属同一条规则的两面，共处一个任务；两个独立规则（如「矩形映射」与「夹回窗口」、`storage.set/get` 与 `storage.remove`）一律拆开。
> `[前置]` 列出必须已完成的编号；依赖顺序见 design.md §12。编号后缀（a/b/c）只表示同组内的独立条目，不表示顺序依赖之外的任何含义。
> 本文件即后续 D 阶段的执行清单与交付报告镜像（条目标题与勾选状态可逐条核对）。
> 命令判据中的 `go`/`npm`/`wails`/`pwsh` 一律先在 `.dev/env.ps1` dot-source 后的环境里执行（`pwsh -File .dev\check-all.ps1` 是汇总入口）。

## A. 主仓上云准备与分支（T1–T6，含 T5b）

- [x] **T1 — 主仓配置本地 `origin` 远端（不推送）**
  文件：`.git/config`
  内容：执行 `git remote add origin https://github.com/shixiansi/pet_PC.git`（私有仓）；**不执行任何 `git push`**。
  完成判据：`git remote get-url origin` 输出恰为 `https://github.com/shixiansi/pet_PC.git`，且 `git remote` 只列出 `origin` 一项；`git for-each-ref --format='%(refname)' refs/remotes` 输出为空（证明没有任何远端跟踪引用、未推送过）。

- [x] **T2 — `main` 分支指向当前工作提交**
  [前置] T1
  文件：`refs/heads/main`
  内容：以当前工作提交创建 `main`（`git branch main <当前 HEAD>`，不切换工作分支以免打断后续开发）。
  完成判据：`git rev-parse main` 等于创建之时的 `git rev-parse feat/pomodoro-prefocus-plan`。

- [x] **T3 — 本地变更已提交，工作区干净**
  [前置] T2
  文件：（无新增文件；提交动作）
  内容：把本轮计划**两个 change 共 6 个 openspec 文件**（`plugin-system` 与 `plugin-market` 各 `proposal.md`/`design.md`/`tasks.md`）提交到当前分支。
  完成判据：`git status --porcelain` 输出为空。

- [x] **T4 — 插件化开发分支 `feat/plugin-system` 就绪**
  [前置] T3
  文件：`refs/heads/feat/plugin-system`
  内容：从 `main` 创建并切换到 `feat/plugin-system`，本 change 的全部代码改动都在该分支上。
  完成判据：`git rev-parse --abbrev-ref HEAD` 输出 `feat/plugin-system`，且 `git merge-base --is-ancestor main feat/plugin-system` 退出码为 0。

- [x] **T5 — `.gitignore` 忽略市场仓目录**
  [前置] T4
  文件：`.gitignore`
  内容：追加一行 `market/`（市场仓是独立 git 仓，嵌套在主仓目录内必须被主仓忽略）。
  完成判据：`git check-ignore -v market/index.json` 命中 `market/` 那条规则。

- [x] **T5b — `.gitignore` 放行本轮新增的浮层面像素门禁脚本**
  [前置] T5
  文件：`.gitignore`
  内容：追加一行 `!.dev/verify-plugin-surface.ps1`（T125 新建的门禁脚本）。`.gitignore:11-16` 的既有注释把三个前端门禁脚本作为**项目源**纳入版本库（「a change to a gate has to be reviewable alongside the code it guards」），第四个门禁脚本必须遵守同一约定，否则它对主仓不可见、T6 的负面判据也覆盖不到它。既有三行白名单一字不改。
  完成判据：`git check-ignore -v .dev/verify-plugin-surface.ps1` **无输出**（未被忽略）。

- [x] **T6 — `.gitignore` 既有全部忽略规则与白名单逐条未变**
  [前置] T5、T5b
  文件：`.gitignore`
  内容：只做 T5/T5b 的两处追加，既有**全部 10 条忽略规则**（`build/bin/`、`frontend/dist/`、`node_modules/`、`.dev/*`、`__pycache__/`、`*.pyc`、`.memsearch/`、`.DS_Store`、`Thumbs.db`、`*.swp`）与既有 **3 条白名单**（`!.dev/check-frontend.cjs`、`!.dev/run-frontend-harness.cjs`、`!.dev/layout-report.cjs`）逐条保留；`.gitignore:1-16` 的既有注释文本一字不改。
  完成判据：`git check-ignore -v frontend/dist/index.html` 命中 `frontend/dist/`、`git check-ignore -v build/bin/PetPC.exe` 命中 `build/bin/`、`git check-ignore -v frontend/node_modules/x.js` 命中 `node_modules/`、`git check-ignore -v .memsearch/memory/x.md` 命中 `.memsearch/`、`git check-ignore -v .dev/x.swp` 命中 `*.swp`、`git check-ignore -v a/__pycache__/b.pyc` 命中 `__pycache__/`（负面防回归覆盖到全部 10 条 pattern 中的 6 条代表项），而 `git check-ignore -v .dev/check-frontend.cjs`、`.dev/run-frontend-harness.cjs`、`.dev/layout-report.cjs`、`.dev/verify-plugin-surface.ps1` 四者均**无输出**（4 条白名单全部生效）。

## B. 配置与已装索引持久化（T7–T9）

- [x] **T7 — `Config.MarketBaseURL` 的默认值与规范化**
  [前置] T4
  文件：`internal/store/store.go`、`internal/store/store_test.go`
  内容：`Config` 新增 `MarketBaseURL string \`json:"marketBaseUrl"\``；常量 `DefaultMarketBaseURL = "https://gh-proxy.com/https://raw.githubusercontent.com/shixiansi/pet-pc-plugins/main"`；`DefaultConfig()` 置该默认值；`Config.Normalise()` 末尾追加：去首尾空白、去掉结尾多余的 `/`、**空串保持空串**（空 = 强制内置源，不是「恢复默认」）。
  完成判据：`store.DefaultConfig().MarketBaseURL == DefaultMarketBaseURL`；`Config{MarketBaseURL:"  https://example.com/market//  "}` 经 `Normalise()` 后等于 `"https://example.com/market"`；`Config{MarketBaseURL:"   "}` 经 `Normalise()` 后等于 `""`；不含 `marketBaseUrl` 键的旧 `config.json` 经 `store.New` 后 `Config().MarketBaseURL == DefaultMarketBaseURL`。

- [x] **T8 — `internal/plugins` 已装索引的持久化**
  [前置] T4
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`（新建）
  内容：`Index` 结构（`version` + `plugins[]{id,version,enabled,granted,installedAt}`）；`NewManager(dataDir string) (*Manager, error)`；索引用临时文件 + `os.Rename` 原子落盘（与 `internal/store/store.go:525-539` 的 `writeJSON` 同构）。
  完成判据：临时目录上 `NewManager` + 写入一条条目后重新 `NewManager`，读回的条目 `id`/`version`/`enabled`/`granted`/`installedAt` 逐字段相同；`<dataDir>` 下不残留 `.tmp-*` 文件。

- [x] **T9 — 索引损坏时隔离并继续（不阻断启动）**
  [前置] T8
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`
  内容：`plugins.json` 无法解析时重命名为 `plugins.json.bak`（与 `store.quarantine` 同构）并以空索引继续，`Warnings()` 给出提示。
  完成判据：把 `plugins.json` 写成 `{not json` 后 `NewManager` 返回 `err == nil`，`plugins.json.bak` 存在、`Plugins()` 为空且 `Warnings()` 非空。

## C. manifest 解析与校验（T10–T15）

- [x] **T10 — manifest 文本字段与 `id` 格式校验**
  [前置] T9
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`（新建）
  内容：`ParseManifest([]byte) (Manifest, error)` 纯函数；校验 `id`（`^[a-z0-9]+(\.[a-z0-9][a-z0-9-]*)+$`，≤96）、`name`（非空，≤64）、`author`（非空）、`description`（非空，≤512）；错误文案含字段名。
  完成判据：一份合法 manifest 解析成功且四个字段值逐项相等；逐一构造「缺 `id`」「`id` 含大写」「`id` 含空格」「缺 `name`」「缺 `author`」「缺 `description`」六种输入时 `ParseManifest` 均返回非 nil 错误，且错误文案包含对应字段名。

- [x] **T11 — `entry` 路径与「至少一种界面落点」校验**
  [前置] T10
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  内容：`entry` 必填、相对路径、`.html` 结尾、不含 `\` 与 `..`、不以 `/` 开头；`panel` 与 `surface` 至少声明其一。
  完成判据：`entry="panel.html"` + `panel=true` 的组合解析成功；逐一构造「`entry` 为 `../x.html`」「`entry` 为 `/abs.html`」「`entry` 为 `a\\b.html`」「`entry` 非 `.html`」「`entry` 为空」「`panel` 与 `surface` 都未声明」六种输入时均返回非 nil 错误，且错误文案包含 `entry`（前五种）或 `panel`/`surface`（第六种）。

- [x] **T12 — `version` 语义化版本校验**
  [前置] T10
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  内容：`version` 必须匹配 `^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`。
  完成判据：表驱动断言 `"1.0.0"`、`"10.20.30"`、`"1.0.0-beta.1"`、`"1.0.0-beta.1.2"` 四种输入解析成功，而 `"1.0"`、`"v1.0.0"`、`"1.0.0.0"`、`""`、`"1.0.0-"` 五种输入解析失败，且失败项的错误文案包含 `version`。

- [x] **T13 — `permissions` 白名单与去重校验**
  [前置] T10
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  内容：`AllowedPermissions` 固定为 9 项（`todos.read`、`todos.write`、`pet.bubble`、`timer.read`、`events`、`net.fetch`、`storage`、`pet.surface`、`llm.chat`）；元素必须属于该集合且不得重复。
  完成判据：`AllowedPermissions` 恰含上述 9 项（集合相等）；`permissions: []` 解析成功；含非法名 `fs.read` 的 manifest 解析失败且错误文案包含 `fs.read` 与 `permissions`；含重复项的 manifest 解析失败且错误文案包含该重复名。

- [x] **T14 — `surface` 声明的数量与尺寸校验**
  [前置] T11
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  内容：`surface` 选填、长度 ≤ `maxSurfaceRects = 4`，每项 `w>0 && h>0 && w<=2000 && h<=2000`、`0<=radius<=200`、`|dx|<=4000 && |dy|<=4000`。
  完成判据：表驱动断言 —— `surface` 5 块、某块 `w=0`、某块 `h=-1`、某块 `radius=201`、某块 `dx=4001` 五种输入均解析失败且错误文案含 `surface`；`surface` 恰 4 块且各值在界内的 input 解析成功。

- [x] **T15 — `netHosts` 与 `net.fetch` 的配对校验**
  [前置] T13
  文件：`internal/plugins/manifest.go`、`internal/plugins/manifest_test.go`
  内容：`netHosts` 每项匹配 `^[A-Za-z0-9.-]+(:\d+)?$`；声明了 `net.fetch` 但 `netHosts` 为空视为非法；未声明 `net.fetch` 时 `netHosts` 可缺席。
  完成判据：`netHosts:["bad host"]` 解析失败且错误文案含 `netHosts`；`permissions:["net.fetch"]` 且 `netHosts:[]` 解析失败且错误文案含 `netHosts`；`permissions:["net.fetch"]` 配 `netHosts:["api.deepseek.com","example.com:8443"]` 解析成功；不含 `net.fetch` 且不含 `netHosts` 的 manifest 解析成功。

## D. 安装、启停、授权、卸载（T16–T25）

- [x] **T16 — sha256 不匹配即拒绝安装，索引不变**
  [前置] T9、T10
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`
  内容：`Manager.Install(entry MarketEntry) (PluginView, error)`：校验 `sha256` 为小写 64 位十六进制 → 取 zip 字节（远端下载或内置源）→ 流式计算 sha256 并与期望比较；不相等即拒绝、删除临时文件、**索引不变**。
  完成判据：用一个 zip 的正确 sha256 调用 `Install` 返回 `err == nil`；把该 sha256 的任一位十六进制字符改掉后调用 `Install`，返回错误文案包含 `sha256`，且 `Manager.Plugins()` 与调用前逐条相同、`<dataDir>/plugins/<id>` 不存在、`<dataDir>/plugins-tmp` 无残留文件。

- [x] **T17 — 安装成功：落盘到 `plugins/<id>` 并写入索引（未启用）**
  [前置] T16
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`
  内容：校验通过后解压产物移到 `<dataDir>/plugins/<id>`；写索引条目 `enabled=false`、`granted=[]`；清空 `plugins-tmp`。
  完成判据：一次成功安装后 `<dataDir>/plugins/<id>/manifest.json` 存在且内容与 zip 内逐字节相同、`Manager.Plugins()` 恰有 1 条且其 `Enabled == false` 与 `Granted` 长度为 0、`<dataDir>/plugins-tmp` 为空。

- [x] **T18 — zip 条目路径穿越拒绝**
  [前置] T17
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`
  内容：解压前逐条校验 zip 条目：拒绝绝对路径（`/` 开头、含盘符）、含 `..` 段、`filepath.Rel` 后以 `..` 开头、以及非普通文件条目（symlink 等）。
  完成判据：构造三个仅含单一恶意条目的 zip（`../escape.txt`、`/abs.txt`、`a/../../escape.txt`，各自 sha256 正确）分别调用 `Install`，每次返回错误文案包含 `路径` 或 `非法条目`，且 `<dataDir>` 下不存在 `escape.txt`/`abs.txt`、`<dataDir>/plugins` 下无新目录、`Manager.Plugins()` 与调用前相同。

- [x] **T19 — zip 解压体积上限拒绝**
  [前置] T18
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`
  内容：单条目解压上限 32 MiB、总解压上限 128 MiB；超限即拒绝并回滚。
  完成判据：构造一个条目声明尺寸超 32 MiB 的 zip（sha256 正确）调用 `Install`，返回错误文案包含 `上限`，且 `<dataDir>/plugins/<id>` 不存在、`Manager.Plugins()` 与调用前相同。

- [x] **T20 — 市场条目权限与包内 manifest 不一致即拒绝安装**
  [前置] T17
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`
  内容：包内 manifest 是权威；市场条目的 `permissions`/`netHosts` 与包内不一致时拒绝安装（错误文案 `市场条目与插件包的权限声明不一致`）。
  完成判据：把市场条目的 `permissions` 相对包内多加一项后调用 `Install`，返回错误文案含 `权限声明不一致`，且 `Manager.Plugins()` 与调用前逐条相同、`<dataDir>/plugins/<id>` 不存在。

- [x] **T21 — 启停持久化与未知 id 拒绝**
  [前置] T17
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`
  内容：`Manager.SetEnabled(id string, on bool) error`，只改索引 `enabled` 并落盘；`id` 不存在时返回 `插件未安装` 且不改任何状态。
  完成判据：`SetEnabled(id, true)` 后重新 `NewManager` 读到的该条目 `Enabled == true` 且 `Granted` 与之前逐项相同；对不存在的 id 调用返回错误且 `Manager.Plugins()` 逐条不变。

- [x] **T22 — 逐项授权持久化**
  [前置] T17
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`
  内容：`Manager.GrantPermissions(id string, granted []string) (PluginView, error)` 写入索引并落盘；返回的视图里 `Granted` 为已授权子集、`Pending` 为声明但未授权的余下项。
  完成判据：对声明 `["llm.chat","pet.bubble"]` 的插件调用 `GrantPermissions(id, ["llm.chat"])` 后返回的 `PluginView.Granted == ["llm.chat"]`、`PluginView.Pending == ["pet.bubble"]`，且重新 `NewManager` 后 `Granted` 仍为 `["llm.chat"]`。

- [x] **T23 — 授权未声明的权限即拒绝**
  [前置] T22
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`
  内容：`GrantPermissions` 只接受该插件 manifest 声明过的权限名。
  完成判据：对声明 `["llm.chat","pet.bubble"]` 的插件传入 `["net.fetch"]`，返回错误且索引中该条目的 `granted` 与调用前逐项相同。

- [x] **T24 — 已装插件发现（坏 manifest 不阻断其他行）**
  [前置] T17
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`
  内容：`Manager.Plugins() []PluginView` 以索引为骨架、逐个 `LoadManifest(<dataDir>/plugins/<id>)`；解析失败时该行 `Error` 非空、`Enabled` 视为 false，其余行不受影响。
  完成判据：预置两个已装目录（一个合法、一个 `manifest.json` 内容为 `{}`）后 `Plugins()` 返回 2 条：合法行 `Error == ""` 且字段与 manifest 逐项相等，坏行 `Error != ""` 且 `Enabled == false`，且 `Plugins()` 调用前后磁盘文件未被修改。

- [x] **T25 — 卸载：目录与私有存储都清干净**
  [前置] T17
  文件：`internal/plugins/manager.go`、`internal/plugins/manager_test.go`
  内容：`Manager.Uninstall(id string) error` 删除 `<dataDir>/plugins/<id>` 与 `<dataDir>/plugin-data/<id>`，从索引移除并落盘。
  完成判据：对一个已装且 `plugin-data/<id>` 下有文件的插件调用 `Uninstall` 后 `<dataDir>/plugins/<id>` 与 `<dataDir>/plugin-data/<id>` 两个路径 `os.Stat` 均返回 `os.ErrNotExist`、`Manager.Plugins()` 不含该 id；对不存在的 id 调用返回错误且索引逐条不变。

## E. 市场源（T26–T32）

- [x] **T26 — 市场基址的 scheme 校验**
  [前置] T7、T8
  文件：`internal/plugins/market.go`、`internal/plugins/market_test.go`（新建）
  内容：`MarketBaseURL` 只接受 `http`/`https`，其他 scheme 一律拒绝，错误文案含 `只支持 http/https`。
  完成判据：`Config.MarketBaseURL` 置为 `file:///C:/x` 与 `ftp://h/x` 时 `Market()` 返回错误且错误文案含 `http/https`。

- [x] **T27 — 条目 `download` 的 scheme 校验**
  [前置] T26
  文件：`internal/plugins/market.go`、`internal/plugins/manager_test.go`
  内容：条目的 `download` 相对路径（拼接基址）或绝对 `http(s)`；其他 scheme 拒绝。
  完成判据：`download` 为 `file:///x.zip` 的条目在 `Install` 时被拒绝且索引不变；`download` 为相对路径 `dist/x.zip` 与绝对 `https://h/x.zip` 两种取值均被接受（以「能进入 sha256 校验步骤」为判据，即错误文案不再含 `http/https`）。

- [x] **T28 — 远端索引拉取成功**
  [前置] T26
  文件：`internal/plugins/market.go`、`internal/plugins/market_test.go`
  内容：`GET base + "/index.json"`（`Timeout = 8s`），解析后返回条目；单条字段非法的条目被丢弃并在 `Notice` 里累计说明；`Installed`/`InstalledVersion` 按索引比对。
  完成判据：用一个返回合法 `index.json`（3 条，其中 1 条与索引同 id）的 `httptest.Server` 调用 `Market()`，返回 `Fallback == false`、条目数为 3、该同 id 条目的 `Installed == true` 且 `InstalledVersion` 等于索引里的版本。

- [x] **T29 — 单条非法条目被丢弃且不影响其余条目**
  [前置] T28
  文件：`internal/plugins/market.go`、`internal/plugins/market_test.go`
  内容：`id`/`version`/`download`/`sha256` 缺任一或 `permissions` 含非法名 → 丢弃该条并累计 `Notice`，不整批失败。
  完成判据：用含 3 条（合法 / 缺 `sha256` / `permissions:["fs.read"]`）的 `index.json` 调用 `Market()`，返回 `err == nil`、条目数恰为 1、`Notice` 非空且同时提到被丢弃的两个 id。

- [x] **T30 — 不可达或空基址时回退内置源并给出提示**
  [前置] T28、T31（内置源文件由 T31 先落盘，本任务与 T31 共用同一条 `Market()` 链路）
  文件：`internal/plugins/market.go`、`internal/plugins/market_test.go`
  内容：非 2xx、超时、JSON 无法解析、基址为空 → 读内置源并置 `Fallback=true` 与对应 `Notice`。
  完成判据：把基址指向一个已关闭的端口调用 `Market()`，返回 `Fallback == true`、`Notice` 非空、条目数等于内置源条目数；把基址置为 `""` 再调用一次，同样得到 `Fallback == true` 且 `Notice` 的文案与前者不同（区分「未配置」与「不可达」）。

- [x] **T31 — 内置示例源嵌入且条目可读**
  [前置] T7、T9、T16、T17（内置源文件**先落盘**；`Install` 的完整安装链路已由 T16/T17 本体验证，本任务经 T16→T10–T15 传递依赖同一链路，T30 与 T32 共用该链路）
  文件：`internal/plugins/sample/embed.go`、`internal/plugins/sample/index.json`、`internal/plugins/sample/com.shixiansi.plugin-chat-1.0.0.zip`、`internal/plugins/market_test.go`
  内容：`//go:embed all:sample` 嵌入 `index.json` 与 zip；远端与内置共用同一 sha256 校验路径。
  完成判据：内置源模式下 `Market()` 恰返回 1 条（id 为 `com.shixiansi.plugin-chat`）且 `err == nil`；对该条调用 `Install` 返回 `err == nil` 且 `<dataDir>/plugins/com.shixiansi.plugin-chat/manifest.json` 存在。

- [x] **T32 — 内置源 zip 与市场仓 `dist/` 产物 sha256 一致**
  [前置] T31
  文件：`internal/plugins/market_test.go`
  内容：断言内置源 zip 的实算 sha256 == `sample/index.json` 的 `sha256`（**恒可验证，必过**）；并在市场仓工作目录存在时追加断言它等于 `../market/dist/com.shixiansi.plugin-chat-1.0.0.zip` 的 sha256（后者由 `plugin-market` M5/M6 产出）。`market/dist` 不存在时**对追加断言 `t.Skip`**（测试文件用 `os.Stat` 判断，路径写成相对 `../market/dist/...`，不绑定绝对盘符），恒可验证部分照常执行。
  完成判据：`go test ./internal/plugins -run TestBuiltinSampleMatchesIndex` 通过，且恒可验证断言必被求值：`sha256(sample/com.shixiansi.plugin-chat-1.0.0.zip)` 等于 `sample/index.json` 条目的 `sha256`；`../market/dist/com.shixiansi.plugin-chat-1.0.0.zip` 存在时该断言也通过（其 sha256 与之相同），不存在时输出中该追加断言记为 `SKIP` 而非 FAIL。

## F. 宿主调度与九项能力（T33–T52）

- [x] **T33 — `PluginRequest` 对未知方法返回错误**
  [前置] T8、T17
  文件：`app_plugins.go`、`app_plugins_test.go`（新建）
  内容：`func (a *App) PluginRequest(pluginID, method, payload string) (string, error)` 是唯一能力入口；方法名不在表内即返回 `插件 <id> 调用了未知方法 <method>`。
  完成判据：对一个已启用插件调用 `PluginRequest(id, "no.such.method", "{}")`，返回错误文案等于 `插件 <id> 调用了未知方法 no.such.method`，且 `a.layout`、`a.bubble`、`a.store.Todos()` 在调用前后逐项相同。

- [x] **T34 — `PluginRequest` 对未启用插件返回错误**
  [前置] T33
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：未安装或索引 `enabled=false` 的插件一律返回 `插件 <id> 未启用`，不进入方法分派。
  完成判据：对一个已安装但 `enabled=false` 的插件与一个未安装的 id 各调用一次 `todos.list`，两次均返回错误文案等于 `插件 <id> 未启用`，且 `a.store.Todos()` 在调用前后逐项相同。

- [x] **T35 — 未授权调用被拒绝且不改任何状态**
  [前置] T34
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：方法表逐项声明所需权限；未授予时返回 `插件 <id> 未获权限 <perm>` 后**立即返回**，不做任何副作用（不写待办、不弹气泡、不发请求、不写索引、不下发 Layout、不写私有存储）。
  完成判据：对 `granted=[]` 的插件逐一调用 `todos.list`、`todos.add`、`todos.update`、`todos.toggle`、`todos.remove`、`timer.state`、`events.subscribe`、`bubble.say`、`net.fetch`、`storage.get`、`storage.set`、`storage.remove`、`surface.show`、`surface.hide`、`llm.chat` 十五个方法，每次返回错误文案含 `未获权限`，且调用后 `a.store.Todos()` 逐条不变、`a.bubble == nil` 且 `a.layout.Bubble == nil`、`a.pluginSurface == nil`、`Manager.Plugins()` 逐条不变、`<dataDir>/plugin-data/<id>` 下无新文件。

- [x] **T36 — `todos.read` 能力**
  [前置] T35、T22
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`todos.list` 需 `todos.read`，返回 `a.store.Todos()` 的 JSON 投影。
  完成判据：store 内预置 2 条待办后，对已授予 `todos.read` 的插件调用 `PluginRequest(id, "todos.list", "")` 返回 `err == nil`，且返回体的条目数、顺序与 `Title` 逐项等于 `a.store.Todos()`。

- [x] **T37 — `todos.write` 之新增**
  [前置] T35、T22
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`todos.add` 需 `todos.write`，复用既有 store 层新增调用。
  完成判据：对已授予 `todos.write` 的插件调用 `todos.add`（`{title:"插件加的"}`）后 `a.store.Todos()` 比调用前多 1 条，且存在一条 `Title == "插件加的"`。

- [x] **T38 — `todos.write` 之修改**
  [前置] T37
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`todos.update` 需 `todos.write`，复用既有 `UpdateTodo` 的 store 层调用。
  完成判据：对已存在的一条待办调用 `todos.update` 后，`a.store.Todos()` 中该 id 的 `Title` 与 `Notes` 等于传入值，且条目总数不变。

- [x] **T39 — `todos.write` 之完成态翻转**
  [前置] T37
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`todos.toggle` 需 `todos.write`，复用既有 `ToggleTodo` 的 store 层调用。
  完成判据：对一条 `Done == false` 的待办调用 `todos.toggle` 后，`a.store.Todos()` 中该 id 的 `Done == true`。

- [x] **T40 — `todos.write` 之删除**
  [前置] T37
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`todos.remove` 需 `todos.write`，复用既有 `DeleteTodo` 的 store 层调用。
  完成判据：对一条待办调用 `todos.remove` 后，`a.store.Todos()` 中不再存在该 id 且总数比调用前少 1。

- [x] **T41 — `timer.read` 能力**
  [前置] T35、T22
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`timer.state` 需 `timer.read`，返回与既有 `PomodoroView` 同源的状态。
  完成判据：开启番茄钟并启动一个专注后，对已授予 `timer.read` 的插件调用 `timer.state`，返回体的 `phase`/`runState`/`remainingSec` 与 `a.GetState().Pomodoro` 的同名字段逐项相同。

- [x] **T42 — `storage` 之写入与读取往返**
  [前置] T35、T22
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`storage.set`/`storage.get` 需 `storage`，只允许 `<dataDir>/plugin-data/<id>/`。
  完成判据：对已授予 `storage` 的插件 `storage.set("k1","v1")` 后 `<dataDir>/plugin-data/<id>/k1` 存在且内容为 `v1`，且 `storage.get("k1")` 返回 `v1`。

- [x] **T43 — `storage` 之删除**
  [前置] T42
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`storage.remove` 需 `storage`。
  完成判据：先 `storage.set("k1","v1")` 再 `storage.remove("k1")` 后，`<dataDir>/plugin-data/<id>/k1` 的 `os.Stat` 返回 `os.ErrNotExist`。

- [x] **T44 — `storage` 之 key 白名单拒绝**
  [前置] T42
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`key` 必须匹配 `^[A-Za-z0-9_-]{1,64}$`；不合法即拒绝，不落盘。
  完成判据：`storage.set("../escape","x")`、`storage.set("a/b","x")`、`storage.set("","x")` 三次均返回错误，且 `<dataDir>/plugin-data/<id>` 之外无任何新文件、该目录内文件数与调用前相同。

- [x] **T45 — `net.fetch` 对白名单域名放行**
  [前置] T35、T22
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`net.fetch` 需 `net.fetch`；`url` 必须 `http`/`https` 且其 `host[:port]` 命中该插件 manifest 的 `netHosts`；只接受 `GET`/`POST`。
  完成判据：插件 `netHosts: ["127.0.0.1:PORT"]` 时对 `httptest.Server` 的 `net.fetch` 返回 `err == nil`，且返回的响应体逐字节等于服务器发出的字节。

- [x] **T46 — `net.fetch` 对白名单外域名拒绝且不发请求**
  [前置] T45
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`host[:port]` 不在 `netHosts` 内即拒绝（不发请求）；非 `http(s)` 的 URL 同样拒绝。
  完成判据：对 `http://other.example/` 的调用返回错误文案含 `未声明的域名`，且同一测试用服务器记录的请求计数在处理该调用前后保持不变；对 `file:///x` 的调用返回错误且该计数同样不变。

- [x] **T47 — `events` 合法订阅**
  [前置] T35、T22
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`events.subscribe` 需 `events`，`names` 只能是 `timer.changed`、`todos.changed`；宿主在对应状态变化后向该插件推送对应 `event`。
  完成判据：订阅 `["timer.changed","todos.changed"]` 返回 `err == nil` 且宿主记录的该插件订阅集合恰等于这两项；随后触发一次待办变化，该插件收到 1 条 `name == "todos.changed"` 的 `event`。

- [x] **T48 — `events` 非法事件名拒绝**
  [前置] T47
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`names` 含表外事件名即拒绝，且不新增任何订阅。
  完成判据：订阅 `["todos.changed","no.such.event"]` 返回错误文案含 `no.such.event`，且订阅集合仍为调用前的值。

- [x] **T49 — `llm.chat` 流式返回（复用既有配置与用量）**
  [前置] T35、T22
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`llm.chat` 需 `llm.chat`；复用 `a.llmSettings(a.store.Config())`（`app_llm.go:415`）与 `a.llmClient()`（`:427`）；流式结果经 `event` 消息推给发起插件（`llm.delta`/`llm.done`，均带本次 `requestId`）。
  完成判据：用一个返回 SSE 分块响应的 `httptest.Server` 作为 LLM 端点，对已授予 `llm.chat` 的插件调用一次 `llm.chat`：返回 `err == nil`，宿主向该插件推送的事件序列为「≥2 条 `llm.delta`、末尾 1 条 `llm.done`」，所有事件的 `requestId` 相同且拼接起的文本等于端点返回的完整文本，且 `a.store.LLMUsageToday(time.Now())`（`internal/store/llm.go:231`，本仓无 `Store.LLMUsage()`）的 `CompletionTokens` 因该次调用增加。

- [x] **T50 — `llm.chat` 未就绪时返回错误且用量不变**
  [前置] T49
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：未配置/未就绪/超预算时直接返回错误（文案取既有 `NotReadyReason`），不做静默降级、不计用量。
  完成判据：在 `LLMEnabled=false` 时调用 `llm.chat` 返回非 nil 错误且错误文案非空，同时 `a.store.LLMUsageToday(time.Now())`（本仓无 `Store.LLMUsage()`）的 `Requests`/`PromptTokens`/`CompletionTokens` 与调用前逐项相同、该插件未收到任何 `llm.*` 事件。

- [x] **T50b — 状态变化与 LLM 流式回调经 `pet:plugin-event` 下行（Go 侧唯一事件名）**
  [前置] T49、T47
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：事件下行的**唯一** Wails 事件名钉死为 `pet:plugin-event`，且必须以**双引号字面量**发出（`a.emitEvent("pet:plugin-event", PluginEventView{PluginID:id, Name:name, Payload:payload})`——双引号是 `check-frontend.cjs:137` 的 `emitEvent\("([^"]+)"` 扫描口径，用常量会退化成标识符）；发出点两处：①`events.subscribe` 订阅的 `timer.changed`/`todos.changed` 状态变化处；②`llm.chat` 流式回调（`llm.delta`/`llm.done`/`llm.error`）。事件体固定为 `{pluginId,name,payload}`。
  完成判据：用既有 `emitFn` 收集器（`app.go:150-165`、`app_llm_test.go:31` 同一手法）收集全部 `pet:plugin-event`：对已订阅且已授权 `events` 的插件触发一次待办变化后，收集到的事件恰 1 条且其 `pluginId` 与 `name == "todos.changed"` 逐项相等；对已授予 `llm.chat` 的插件走一次 SSE 端点调用后，收集到的 `pet:plugin-event` 序列中 `name == "llm.delta"` 的条目 ≥2 条、`name == "llm.done"` 的条目恰 1 条，且所有条目的 `pluginId` 均为该插件 id。

- [x] **T51 — `pet.bubble` 能力**
  [前置] T35、T22
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`bubble.say` 需 `pet.bubble`，走既有 `a.showBubble(&BubbleView{Kind: BubbleReminder, Title: text})`，几何由既有常量决定。
  完成判据：对已授予 `pet.bubble` 的插件调用 `bubble.say`（text 非空）后 `a.bubble != nil`、`a.bubble.Kind == BubbleReminder`、`a.bubble.Title == text`，且 `a.layout.Bubble` 的 `Kind`/`Title` 与 `a.bubble` 相同、`a.layout.Bubble.H == float64(bubbleHeightReminder)`（即 140）。

- [x] **T52 — 九项权限与调度表逐项对应（无遗漏、无多余）**
  [前置] T36–T51（全部能力任务）
  文件：`internal/plugins/capability.go`、`app_plugins.go`、`app_plugins_test.go`
  内容：方法表里出现的权限名集合恰等于 `AllowedPermissions`，且每个权限至少被一个方法使用。
  完成判据：单测断言方法表用到的权限集合与 `AllowedPermissions` 集合相等（9 项），且对表中每个方法都能查到唯一一项所需权限（无重复归口、无空归口）。

## F2. Wails 绑定（`app_plugins.go` → `frontend/wailsjs/go/**`）（T52a–T52h）

> 本节七个绑定构成前端 `plugins.js`/`main.js` 的**唯一**调用面（`App.*` 调用被 `check-frontend.cjs:76-111` 与 `.dev/run-frontend-harness.cjs:76-97` 双向硬断言）。每条任务只断言**一个**绑定的导出与返回体；错误路径各归其位。
> 分工口径（**避免依赖成环**）：T52a–T52g 只断言 **Go 侧方法本体 + 返回体逐项相等**（此时 `App.js`/`App.d.ts` 尚未重新生成，故不在这些任务的判据里）；T52h（前置 = T52a–T52g、T57、T63）单独断言 `App.js` 的导出集合、`App.d.ts` 的返回类型与 `models.ts` 的类型定义；T120 的两条 `App.*` 门禁断言在 T52h 之后一次性核对全部十个绑定。
> 位置说明：T52h 在本文件中**按主题**列在 F2 节（连同七个绑定一起阅读），但其**执行位置**由 design.md §12 钉死为「T67 之后、T119 之前」——生成绑定必须在 `Layout.PluginSurface`（T57）与 `OpenPluginSurface`/`ClosePluginSurface`（T63）就位之后，否则生成的 `models.ts`/`App.d.ts` 会漏掉这两处产物。

- [x] **T52a — `InstallPlugin` 绑定**
  [前置] T52、T16
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`func (a *App) InstallPlugin(id string) (PluginView, error)`。
  完成判据：对一条合法市场条目调用后返回 `err == nil`，且返回体 `PluginView` 的 `ID`/`Version`/`Enabled`/`Permissions`/`Granted`/`Pending`/`Panel`/`Surface`/`Dir` 与 `Manager.Plugins()` 中该插件的条目逐项相等（`Enabled == false`、`Granted` 长度为 0）。

- [x] **T52b — `UninstallPlugin` 绑定**
  [前置] T52、T25
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`func (a *App) UninstallPlugin(id string) (PluginsView, error)`，返回卸载后的已装列表视图。
  完成判据：对已装插件调用后返回 `err == nil`，且返回体的 `plugins` 不含该 `id`、`<dataDir>/plugins/<id>` 的 `os.Stat` 返回 `os.ErrNotExist`。

- [x] **T52c — `SetPluginEnabled` 绑定**
  [前置] T52、T21
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`func (a *App) SetPluginEnabled(id string, on bool) (PluginView, error)`。
  完成判据：`SetPluginEnabled(id, true)` 返回 `err == nil` 且返回体的 `Enabled == true`、`Granted` 与调用前逐项相同（启用不重复授权）。

- [x] **T52d — `GrantPluginPermissions` 绑定**
  [前置] T52、T22
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`func (a *App) GrantPluginPermissions(id string, granted []string) (PluginView, error)`。
  完成判据：对声明 `["llm.chat","pet.bubble"]` 的插件传入 `["llm.chat"]` 后返回 `err == nil`，且返回体的 `Granted == ["llm.chat"]`、`Pending == ["pet.bubble"]`（逐项相等）。

- [x] **T52e — `SaveMarketURL` 绑定（含非 `http/https` 拒绝）**
  [前置] T52、T26
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`func (a *App) SaveMarketURL(url string) (MarketView, error)`：经 `Config.Normalise()` 落盘并返回重新拉取的 `MarketView`；基址非 `http`/`https` 时**拒绝**且**不写盘**。
  完成判据：传合法地址后返回 `err == nil` 且返回体的 `Base` 等于规范化后的地址；传 `file:///C:/x` 时返回错误且文案含 `http/https`，同时 `<dataDir>/config.json` 的字节内容与调用前逐字节相同。

- [x] **T52f — `PluginsView` 绑定**
  [前置] T52、T24
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`func (a *App) PluginsView() (PluginsView, error)`：返回已装列表视图（`plugins []PluginView` + `warnings []string`）与当前市场基址。
  完成判据：返回 `err == nil`，且返回体的 `Plugins` 与 `Manager.Plugins()` 逐条相等（条数与每条 `ID`/`Version`/`Enabled`/`Granted`/`Pending`/`Error` 逐项相同）、`Warnings` 与 `Manager.Warnings()` 逐项相等、`Base` 等于 `Config().MarketBaseURL`。

- [x] **T52g — `MarketView` 绑定**
  [前置] T52、T28
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`func (a *App) MarketView() (MarketView, error)`：返回 `{base, fallback, notice, entries}`。
  完成判据：返回 `err == nil`，且返回体的 `Base` 等于当前 `Config.MarketBaseURL`、`Fallback`/`Notice`/`Entries` 与 `Manager.Market()` 的同名字段逐项相等（`Entries` 条数与每条 `id`/`name`/`version`/`author`/`description`/`permissions`/`download`/`sha256`/`installed`/`installedVersion` 逐项相同）。

- [x] **T52h — 重新生成 `frontend/wailsjs/go/**` 绑定（排在 T67 之后、T119 之前）**
  [前置] T52a–T52g、T57、T63
  文件：`frontend/wailsjs/go/main/App.js`、`frontend/wailsjs/go/main/App.d.ts`、`frontend/wailsjs/go/models.ts`
  内容：跑 `wails generate module`（本仓 `.dev/bin/wails.exe`，先 dot-source `.dev/env.ps1`；绑定目录已提交，见 `.gitignore:22-23` 的说明），把本轮全部新绑定与 `PluginView`/`PluginsView`/`MarketView`/`MarketEntry`/`PluginSurfaceView`/`PluginSurfaceRect` 模型写进这三个文件。前置含 T57（`Layout.PluginSurface` 字段与 `PluginSurfaceView`/`PluginSurfaceRect` 类型定义）与 T63（`OpenPluginSurface`/`ClosePluginSurface` 的 `main.Layout` 返回体）：判据断言的就是这两处产物，先行生成会漏掉它们（设计 §12 已把 T52h 移到 T67 之后）。
  完成判据：`frontend/wailsjs/go/main/App.js` 的 `export function` 集合包含本轮全部新绑定（`PluginsView`/`MarketView`/`InstallPlugin`/`UninstallPlugin`/`SetPluginEnabled`/`GrantPluginPermissions`/`PluginRequest`/`SaveMarketURL`/`OpenPluginSurface`/`ClosePluginSurface`）；`App.d.ts` 中这十项的返回类型分别为 `main.PluginsView`/`main.MarketView`/`main.PluginView`/`main.PluginsView`/`main.PluginView`/`main.PluginView`/`string`（`PluginRequest` 返回 `Promise<string>`，故**不进** `layoutReturning`）/`main.MarketView`/`main.Layout`/`main.Layout`；`models.ts` 含 `class PluginView`、`class PluginsView`、`class MarketView`、`class MarketEntry`、`class PluginSurfaceView`、`class PluginSurfaceRect`，且 `class Layout` 含 `pluginSurface?: PluginSurfaceView`。

## G. 插件浮层面几何与窗口区域（T53–T67）

- [x] **T53 — 声明矩形 → 物理像素映射与圆角**
  [前置] T4
  文件：`app_plugin_surface.go`（新建，根包）、`app_plugin_surface_test.go`（新建，根包）
  内容：`func computePluginSurface(petX, petY, petW, petH, totalW, totalH int, s float64, rects []plugins.Rect) ([]desktop.RegionRect, *PluginSurfaceView)`（与既有 `computeLiveOverlay` 同构，`app_live.go:156-243`）的映射部分：`x = petX + round(dx*s)`、`y = petY + round(dy*s)`、`w = round(w*s)`、`h = round(h*s)`；`r = min(round(radius*s), min(w,h)/2)`；物理矩形转 CSS 视图（除以 `s`，与 `app.go:726-729` 同一手法）。
  完成判据：输入 `petX=0,petY=160,petW=150,petH=200,totalW=540,totalH=520,s=1` 与单块 `{dx:160,dy:10,w:300,h:220,radius:14}` 时返回的 region 矩形恰为 `{X:160,Y:170,Width:300,Height:220,Radius:14}` 且 `View.Rects[0] == {Index:0,X:160,Y:170,W:300,H:220,R:14}`；`s=2` 时同一输入返回 `{X:320,Y:340,Width:600,Height:440,Radius:28}` 且 `View.Rects[0]` 的 CSS 值仍为 `{160,170,300,220,14}`。

- [x] **T54 — 矩形夹回窗口边界**
  [前置] T53
  文件：`app_plugin_surface.go`、`app_plugin_surface_test.go`
  内容：逐块先钉尺寸（`w>totalW` → `w=totalW`；`h>totalH` → `h=totalH`），再把 `x` 夹到 `[0, totalW-w]`、`y` 夹到 `[0, totalH-h]`。
  完成判据：`{dx:400,dy:10,w:300,h:220}` → `X == 240` 且 `Width == 300`；`{dx:-4000,dy:-4000,w:300,h:220}` → `X == 0` 且 `Y == 0`；`{dx:0,dy:0,w:2000,h:2000}` → `Width == 540` 且 `Height == 520`（`X == 0`、`Y == 0`）。

- [x] **T55 — 夹回后无面积的矩形被丢弃，且保留块按声明序编号**
  [前置] T54
  文件：`app_plugin_surface.go`、`app_plugin_surface_test.go`
  内容：夹回后 `w<=0 || h<=0` 的块丢弃；保留块按下标顺序编号（不重排）。
  完成判据：输入两块「先 `{dx:0,dy:0,w:0,h:0}` 再 `{dx:160,dy:10,w:300,h:220}`」时返回块数为 1 且保留块的 `Index == 1`。

- [x] **T56 — 全部矩形被丢弃时返回 nil（不下发、不扩区域）**
  [前置] T55
  文件：`app_plugin_surface.go`、`app_plugin_surface_test.go`
  内容：全部丢弃时返回 `(nil, nil)`。
  完成判据：输入单块 `{dx:0,dy:0,w:0,h:0}` 时返回 `regions == nil` 且 `view == nil`。

- [x] **T57 — `Layout.PluginSurface` 字段与 JSON 序列化**
  [前置] T53、T8
  文件：`app.go`、`app_plugin_surface_test.go`
  内容：`Layout` 新增 `PluginSurface *PluginSurfaceView \`json:"pluginSurface,omitempty"\``；`PluginSurfaceView`/`PluginSurfaceRect` 类型定义（`pluginId` + `rects[]{index,x,y,w,h,r}`）。
  完成判据：`json.Marshal(Layout{})` 的输出不含 `pluginSurface` 键（`omitempty` 生效）；`json.Marshal(Layout{PluginSurface:&PluginSurfaceView{PluginID:"com.x.y",Rects:[]PluginSurfaceRect{{Index:0,X:160,Y:170,W:300,H:220,R:14}}}})` 的 JSON 含 `"pluginSurface"`、`"com.x.y"` 与 `"rects"`，反序列化回 `Layout` 后各字段逐项相等。

- [x] **T58 — `applyWindowGeometry` 的 region 新增浮层面分支**
  [前置] T57
  文件：`app.go`、`app_plugin_surface_test.go`
  内容：region switch 新增 `case pluginSurface != nil`：region 用 `computePluginSurface` 返回的物理矩形并集再加宠物矩形，`Layout.PluginSurface` 取**同一次**调用的视图。
  完成判据：`grep -n "case pluginSurface" app.go` 命中 1 处；以 `{dx:160,dy:10,w:300,h:220}` 的单块输入断言该分支产生 2 个 region（1 块 + 宠物）且 `Layout.PluginSurface.Rects` 长度为 1；`a.pluginSurface == nil` 时该分支不成立（region 回落为仅宠物）。

- [x] **T59 — `setPluginSurfaceLocked` 是唯一入口（状态与 layout 同步）**
  [前置] T58
  文件：`app.go`、`app_plugin_surface_test.go`
  内容：`setPluginSurfaceLocked(view *PluginSurfaceView)` 同时维护 `a.pluginSurface` 与 `a.layout.PluginSurface`（与 `setBubbleLocked`/`setLiveLocked` 同构，理由见 `app_bubble_test.go:34-41`）。
  完成判据：`setPluginSurfaceLocked(&PluginSurfaceView{PluginID:"com.x.y"})` 后 `a.pluginSurface != nil` 且 `a.layout.PluginSurface.PluginID == "com.x.y"`；再 `setPluginSurfaceLocked(nil)` 后两者同为 nil。

- [x] **T60 — `SetPanelOpen` 清理浮层面**
  [前置] T59
  文件：`app.go`、`app_plugin_surface_test.go`
  内容：`SetPanelOpen(true)` 内新增一次 `setPluginSurfaceLocked(nil)` 调用。
  完成判据：先 `setPluginSurfaceLocked(&PluginSurfaceView{PluginID:"com.x.y"})` 再调用 `SetPanelOpen(true)`，之后 `a.pluginSurface == nil` 且 `a.layout.PluginSurface == nil`。

- [x] **T61 — `OpenLive` 清理浮层面**
  [前置] T59
  文件：`app_llm.go`、`app_plugin_surface_test.go`
  内容：`OpenLive` 内新增一次 `setPluginSurfaceLocked(nil)` 调用（既有拒绝提问气泡的行为不变）。
  完成判据：先 `setPluginSurfaceLocked(&PluginSurfaceView{PluginID:"com.x.y"})` 再调用 `OpenLive()`，之后 `a.pluginSurface == nil` 且 `a.layout.PluginSurface == nil`，同时既有 `TestChatPopupCannotEatTheSummaryPrompt` 仍通过。

- [x] **T62 — `showBubble` 清理浮层面**
  [前置] T59
  文件：`app_pomodoro.go`、`app_plugin_surface_test.go`
  内容：`showBubble` 内新增一次 `setPluginSurfaceLocked(nil)` 调用。
  完成判据：先 `setPluginSurfaceLocked(&PluginSurfaceView{PluginID:"com.x.y"})` 再调用 `showBubble(&BubbleView{Kind:BubbleReminder,Title:"t"})`，之后 `a.pluginSurface == nil`、`a.layout.PluginSurface == nil` 且 `a.bubble.Kind == BubbleReminder`。

- [x] **T63 — `OpenPluginSurface` 打开终态（含 `surface.show` 同路径）**
  [前置] T59、T51
  文件：`app_plugins.go`、`app_plugin_surface_test.go`
  内容：`OpenPluginSurface(pluginID string) (Layout, error)` 要求插件已启用、已授权 `pet.surface`、`manifest.surface` 非空；`PluginRequest` 的 `surface.show` 复用**同一函数**（不新增第二条开关路径）；`ClosePluginSurface() (Layout, error)` 关闭。
  完成判据：对一个已启用且已授权 `pet.surface` 的插件，`OpenPluginSurface(id)` 返回 `err == nil` 且 `a.pluginSurface.PluginID == id`、`a.layout.PluginSurface.PluginID == id`；关闭后再经 `PluginRequest(id,"surface.show","")` 打开得到逐字段相同的终态；再 `ClosePluginSurface()` 后 `a.pluginSurface == nil` 且 `a.layout.PluginSurface == nil`。

- [x] **T64 — `OpenPluginSurface` 对未授权/未声明/越界插件的拒绝**
  [前置] T63
  文件：`app_plugins.go`、`app_plugin_surface_test.go`
  内容：未授权 `pet.surface`、`manifest.surface` 为空、或全部矩形夹回后被丢弃 → 返回错误且不改任何状态。
  完成判据：三种情形各调用一次 `OpenPluginSurface(id)`：均返回非 nil 错误（文案分别含 `未获权限`、`surface`、`窗口之外`），且每次调用后 `a.pluginSurface == nil`、`a.layout.PluginSurface == nil`、`a.panelOpen` 与调用前相同。

- [x] **T65 — 有提问气泡时拒绝打开浮层面且不改状态**
  [前置] T63
  文件：`app_plugins.go`、`app_plugin_surface_test.go`
  内容：存在未回答提问气泡时**拒绝**（与 `OpenLive` 同构，`app_llm.go:1033-1053`），不改任何状态。
  完成判据：在 `PlanPending=true` 且 `a.bubble.Kind == BubblePlan` 时调用 `OpenPluginSurface(id)` 返回非 nil 错误（文案含 `plan`），且 `a.pluginSurface == nil`、`a.bubble.Kind == BubblePlan`、`a.store.Pomodoro().PlanPending == true` 三项不变。

- [x] **T66 — 停用该插件时浮层面关闭**
  [前置] T63、T21
  文件：`app_plugins.go`、`app_plugin_surface_test.go`
  内容：`SetPluginEnabled(id,false)` 在浮层面正属于该插件时先关闭浮层面再回落区域。
  完成判据：先 `OpenPluginSurface(id)` 成功，再调用 `SetPluginEnabled(id,false)` 后 `a.pluginSurface == nil` 且 `a.layout.PluginSurface == nil`。

- [x] **T67 — 卸载该插件时浮层面关闭**
  [前置] T66、T25
  文件：`app_plugins.go`、`app_plugin_surface_test.go`
  内容：`UninstallPlugin(id)` 在浮层面正属于该插件时先关闭浮层面再回落区域。
  完成判据：重新启用该插件并 `OpenPluginSurface(id)` 成功后调用 `UninstallPlugin(id)`，`a.pluginSurface == nil`、`a.layout.PluginSurface == nil` 且 `Manager.Plugins()` 不含该 id。

## H. 资源路由（T68–T71）

- [x] **T68 — `/plugin-asset/@host/bridge.js` 路由**
  [前置] T17
  文件：`main.go`、`app_plugins.go`、`app_plugins_test.go`
  内容：`assetHandler.ServeHTTP` 新增该路径，返回**编译期常量** `pluginBridgeJS`（不是文件），`Content-Type: text/javascript`、`Cache-Control: no-store`、`Access-Control-Allow-Origin: *`（不透明源沙箱 iframe 下 `type="module"` 与子资源加载的前置，§8；响应头断言与「既有像素门禁一字不改」不冲突）。
  完成判据：用 `httptest.NewRecorder` 请求 `/plugin-asset/@host/bridge.js` 得到 200、`Content-Type` 含 `text/javascript`、`Cache-Control` 含 `no-store`、`Access-Control-Allow-Origin` 为 `*`，且响应体逐字节等于源码中的 `pluginBridgeJS` 常量。

- [x] **T69 — `/plugin-asset/<id>/<relpath>` 服务已启用插件的资源**
  [前置] T68
  文件：`main.go`、`app_plugins.go`、`app_plugins_test.go`
  内容：解析 `<dataDir>/plugins/<id>/<relpath>`，命中给 MIME、`Access-Control-Allow-Origin: *`（同 T68 的同一前置）并用 `http.ServeContent`（与 `app.go:1174-1205` 的 `servePetImage` 同构）。
  完成判据：`httptest.NewRecorder` 请求 `/plugin-asset/<已启用id>/<entry>` 得到 200、`Access-Control-Allow-Origin` 为 `*`，且响应体逐字节等于磁盘上该文件的内容。

- [x] **T70 — `/pet-image` 既有行为不变**
  [前置] T69
  文件：`main.go`、`app_plugins_test.go`
  内容：既有 `/pet-image` 分支逐字保留。
  完成判据：数据目录无自定义图片时请求 `/pet-image` 得到 404；把一张图片设为自定义图片后同一请求得到 200 且 MIME 与 `store.MimeForExt` 的结果一致。

- [x] **T71 — 未安装/已停用插件的资源返回 403，且路径穿越被拒**
  [前置] T69
  文件：`app_plugins.go`、`app_plugins_test.go`
  内容：`<id>` 未安装或 `enabled=false` → 403；`<id>` 与 `<relpath>` 逐段拒绝 `..`、`\`、绝对路径与越过插件根的情况。
  完成判据：请求 `/plugin-asset/<已停用id>/panel.html` 与 `/plugin-asset/<未安装id>/panel.html` 均得 403 且响应体不含插件文件内容；请求 `/plugin-asset/<id>/../../config.json` 与 `/plugin-asset/<id>/..%2fconfig.json` 均得 400 或 404 且响应体不含 `config.json` 的内容。

## I. 前端：第 5 个 tab 与沙箱桥（T72–T99）

- [x] **T72 — `index.html` 新增插件 tab 按钮与 tab 体**
  [前置] T4
  文件：`frontend/index.html`
  内容：tab 栏新增 `<button class="tab" data-tab="plugins">插件</button>`；新增 `#tab-plugins` 体（`class="tab-body hidden"`），内含已装列表容器、市场列表容器、市场地址输入框 `#market-base-url` 与保存按钮 `#market-base-save`、「返回」按钮 `#plugin-panel-back`、面板视图容器 `#plugin-panel-view`；`#stage` 内新增浮层面挂载点 `<div id="plugin-surface" class="hidden"></div>`。
  完成判据：`frontend/index.html` 中 `data-tab="` 出现次数为 5；`id="tab-plugins"`、`id="plugin-panel-view"`、`id="plugin-panel-back"`、`id="market-base-url"`、`id="market-base-save"`、`id="plugin-surface"` 六个 id 均存在。

- [x] **T73 — 既有 4 个 tab 与 `#tab-chat` 体逐字未变**
  [前置] T72
  文件：`frontend/index.html`
  内容：`#tab-todos`/`#tab-chat`/`#tab-records`/`#tab-settings` 四个 tab 按钮与四个 tab 体的 markup 一字不改；设置页 section 的 id 集合不增不减。
  完成判据：`id="tab-settings"` 到 `id="toast"` 之间的 `id="` 集合与改动前完全相同；`frontend/src/chat.js` 的 `git diff` 为空（该文件未被本 change 触碰）。

- [x] **T74 — 插件 CSS 两段存在且无禁装饰**
  [前置] T72
  文件：`frontend/src/style.css`
  内容：新增两段带段标记的 CSS（`/* --- plugins tab`、`/* --- plugin surface`），段内不得出现 `box-shadow`/`filter`/`text-shadow`/`transform`。
  完成判据：`grep -c "^/\* --- plugins tab" frontend/src/style.css` 与 `"^/\* --- plugin surface"` 各为 1；两段标记之间的文本中 `box-shadow|filter|text-shadow|transform` 作为属性名的匹配数为 0。

- [x] **T75 — `#tab-plugins` 纳入共享滚动条规则（全部 5 条）**
  [前置] T74
  文件：`frontend/src/style.css`
  内容：把 `#tab-plugins` 加入既有共享滚动条规则的**全部 5 条** `-webkit-scrollbar` 规则的选择器列表（`style.css:467-470` 的 `-webkit-scrollbar`、`:475-479` 的 `-track`、`:483-487` 的 `-thumb`、`:492-496` 的 `-thumb:hover`、`:503-507` 的 `-corner`），并使 `:458-465` 的 `scrollbar-width`/`scrollbar-color` 组选择器列表也含 `#tab-plugins`；使自己滚动的插件视图不自带第二套滚动条样式。
  完成判据：`#tab-plugins` 出现在上述 5 条 `-webkit-scrollbar` 规则的每一条选择器列表中（逐一核对 `-webkit-scrollbar`、`-webkit-scrollbar-track`、`-webkit-scrollbar-thumb`、`-webkit-scrollbar-thumb:hover`、`-webkit-scrollbar-corner` 五条），且每条规则 `sel.split(',').length >= 5`（与门禁 `check-frontend.cjs:1203-1207` 的判定同源、更严）；`:458-465` 的组选择器列表含 `#tab-plugins`。

- [x] **T76 — `plugins.js` 已装列表渲染**
  [前置] T72
  文件：`frontend/src/plugins.js`（新建）
  内容：`PluginsTab` 组件（`constructor(els, {onState, fail, debug})` + `wire()`，写法沿用 `frontend/src/chat.js`）；已装列表按 `PluginView` 渲染（名称/版本/作者/权限摘要/启停/卸载）。
  完成判据：给 `PluginsTab` 喂一份含 2 条已装插件的假视图后，已装列表容器内出现 2 个 `.plugin-row`，且每行文本含对应 `name` 与 `version`。

- [x] **T77 — `plugins.js` 市场列表渲染**
  [前置] T76
  文件：`frontend/src/plugins.js`
  内容：市场列表按 `MarketEntry` 渲染（名称/版本/作者/描述/权限摘要/安装按钮/已装标记）。
  完成判据：喂一份含 2 条市场条目（1 条 `installed=true`）的假 `MarketView` 后，市场列表容器内出现 2 个 `.market-row`，已装项文本含 `已安装`、未装项按钮文案为 `安装`。

- [x] **T78 — `plugins.js` 回退提示渲染**
  [前置] T77
  文件：`frontend/src/plugins.js`
  内容：`MarketView.Fallback=true` 时把 `Notice` 显示在提示区。
  完成判据：喂 `Fallback:true, Notice:"市场不可达，正在使用内置示例源"` 后，提示容器的文本含 `市场不可达`，且市场列表仍按同一份假数据渲染（条目数与 `entries` 相同）。

- [x] **T79 — `plugins.js` 权限对话框渲染声明清单**
  [前置] T72
  文件：`frontend/src/plugins.js`
  内容：安装前的权限对话框按 manifest 声明的权限逐项渲染勾选框（默认全不勾），并显示 `netHosts` 白名单。
  完成判据：打开一个声明 `["llm.chat","pet.bubble","net.fetch"]` 且 `netHosts:["api.deepseek.com"]` 的条目后，对话框内勾选框数量为 3、每项文本为对应权限名、白名单区文本含 `api.deepseek.com`。

- [x] **T80 — 未勾选任何权限时「安装」按钮 disabled**
  [前置] T79
  文件：`frontend/src/plugins.js`
  内容：权限对话框里勾选数为 0 时安装按钮 disabled，勾选 ≥1 项后 enabled。
  完成判据：打开对话框的初始状态下安装按钮的 `disabled` 为 true，勾选其中一项后变为 false。

- [x] **T81 — 安装流程的调用序列**
  [前置] T80
  文件：`frontend/src/plugins.js`
  内容：安装按钮依次调用 `App.InstallPlugin(id)` → `App.GrantPluginPermissions(id, granted)` → `App.SetPluginEnabled(id, true)`。
  完成判据：在假桥接下点击未授权项的「安装」并勾选全部权限，桥接收到的调用序列恰为 `["InstallPlugin","GrantPluginPermissions","SetPluginEnabled"]`（顺序与名字逐项相等）。

- [x] **T82 — 安装任一步失败即停止并提示**
  [前置] T81
  文件：`frontend/src/plugins.js`
  内容：任一步 reject 时状态行显示该步与错误文案，且不继续后续步骤；状态行同时显示 `下载 → 校验 → 解压 → 授权 → 启用` 中当前所处的那一步。
  完成判据：让 `InstallPlugin` reject（错误文案 `sha256 校验失败`）后，调用序列只有 `InstallPlugin` 一项，且状态行文本含 `sha256 校验失败`；在全部成功的路径上，`GrantPluginPermissions` 被调用时状态行文本含 `授权`。

- [x] **T83 — `plugin-host.js` 沙箱 iframe 的 `sandbox` 属性**
  [前置] T72
  文件：`frontend/src/plugin-host.js`（新建）
  内容：创建 `<iframe>` 时 `sandbox` 属性恰为 `allow-scripts`（不设 `allow-same-origin`/`allow-popups`/`allow-top-navigation`/`allow-forms`），且不设置 `allow` 属性。
  完成判据：调用创建面板视图 iframe 后 `document.querySelector('#plugin-panel-view iframe').getAttribute('sandbox') === 'allow-scripts'` 且 `getAttribute('allow') === null`。

- [x] **T84 — 沙箱 iframe 的资源 URL**
  [前置] T83、T69
  文件：`frontend/src/plugin-host.js`
  内容：`src` 为 `/plugin-asset/<id>/<entry>`（同源 URL，使子资源在沙箱内仍可加载；不设 `allow-same-origin` 形成不透明源）。
  完成判据：创建面板视图 iframe 后该元素的 `getAttribute('src')` 等于 `/plugin-asset/<id>/<entry>`（`<id>`/`entry` 取自传入视图），且 `getAttribute('srcdoc') === null`。

- [x] **T84b — 握手：收到本 iframe 的 `ready` 后回投 1 条 `init`**
  [前置] T84
  文件：`frontend/src/plugin-host.js`
  内容：`plugin-host.js` 在 iframe 加载完成后监听**仅来自该 iframe** 的 `{v:1,type:'ready'}`，随即按其所在落点回投**恰好一条** `{v:1,type:'init',pluginId,permissions,granted,surface}`（§9.2 消息表字段全集）：面板视图 `surface.kind='panel'`；浮层面 `surface.kind='surface'` 且带 `rectIndex`/`rectCount`/`rect`。非本 iframe 的 `ready` 不回投。
  完成判据：在一个面板视图 iframe 上从其 `contentWindow` 发一条 `{v:1,type:'ready'}` 后，该 iframe **恰好收到 1 条** `type=='init'` 的消息，且该消息的 `v`/`pluginId`/`permissions`/`granted`/`surface.kind`/`surface.rectIndex`/`surface.rectCount`/`surface.rect` 八项与宿主侧传入的该插件视图及落点**逐项相等**（`surface.kind=='panel'`）；从别的 window 发同名 `ready` 后该 iframe 收到的 `init` 条数仍为 1。

- [x] **T84c — 多矩形时第 i 个 iframe 的 `init.surface.rectIndex == i`**
  [前置] T84b、T89
  文件：`frontend/src/plugin-host.js`
  内容：浮层面第 i 块矩形对应第 i 个 iframe，其 `init.surface` 的 `rectIndex` 为该 iframe 的下标 `i`、`rectCount` 为本次下发的矩形总数、`rect` 为该块矩形自身。
  完成判据：给 `applyGeometry({pluginId:'com.x.y',rects:[A,B]})` 后，第 0 个 iframe 收到的 `init.surface` 为 `{kind:'surface',rectIndex:0,rectCount:2,rect:A}`、第 1 个 iframe 收到的为 `{kind:'surface',rectIndex:1,rectCount:2,rect:B}`（`rectIndex` 与下标逐项相等，且两个 iframe 各自的 `rect` 等于对应块）。

- [x] **T85 — 桥接只接受自己那个 iframe 的消息**
  [前置] T84
  文件：`frontend/src/plugin-host.js`
  内容：只处理 `event.source === iframe.contentWindow` 且 `data.v === 1` 的消息；`method`/`id` 非字符串或为空时丢弃且不回消息。
  完成判据：向该 iframe 的 `contentWindow` 发一条合法 `request` 后 `App.PluginRequest` 被调用 1 次；从别的 window 发同样消息后调用次数仍为 1；从该 iframe 发一条缺 `id` 的消息后调用次数仍为 1。

- [x] **T86 — `request` → `PluginRequest` → `response` 往返（成功路径）**
  [前置] T85
  文件：`frontend/src/plugin-host.js`
  内容：收到合法 `request` 时调用 `App.PluginRequest(pluginId, method, JSON.stringify(payload))`，把结果包成 `{v:1,type:'response',id,ok:true,result}` 回投。
  完成判据：发 `{v:1,type:'request',id:'r1',method:'todos.list',payload:{}}`（假桥接返回 `[{"id":"t1"}]`）后桥接收到 `PluginRequest(<id>,'todos.list','{}')`，且该 iframe 收到恰好 1 条 `{v:1,type:'response',id:'r1',ok:true,result:[{"id":"t1"}]}`。

- [x] **T87 — `response` 的错误路径（`ok:false` + 文案透传）**
  [前置] T86
  文件：`frontend/src/plugin-host.js`
  内容：`App.PluginRequest` reject 时回投 `{v:1,type:'response',id,ok:false,error}`，错误文案原样透传。
  完成判据：让假桥接 reject（错误文案 `未获权限 llm.chat`）后发一条 `request`，收到的 response 为 `{id:'r2',ok:false,error:'未获权限 llm.chat'}`。

- [x] **T88 — `sendEvent` 事件下行**
  [前置] T86
  文件：`frontend/src/plugin-host.js`
  内容：提供 `sendEvent(name, payload)`，向对应 iframe 发 `{v:1,type:'event',name,payload}`。
  完成判据：调用 `sendEvent('llm.delta',{requestId:'r1',text:'你'})` 后该 iframe 收到恰好 1 条 `{v:1,type:'event',name:'llm.delta',payload:{requestId:'r1',text:'你'}}`。

- [x] **T88b — `plugin-host.js` 用 `EventsOn('pet:plugin-event')` 按 `pluginId` 路由到对应插件的 `sendEvent`**
  [前置] T88、T50b
  文件：`frontend/src/plugin-host.js`
  内容：`plugin-host.js` 以 `EventsOn('pet:plugin-event', …)` 订阅宿主事件（`import { EventsOn } from '../wailsjs/runtime/runtime'`，与 `main.js:21` 同一入口），按事件体的 `pluginId` 找到该插件的 iframe，并以 `sendEvent(name, payload)` 投递一条 `{v:1,type:'event',name,payload}`；未安装/未打开界面的 `pluginId` 直接丢弃、不投给任何 iframe。
  完成判据：在假桥接下用 harness 的 `window.runtime.EventsEmit('pet:plugin-event', {pluginId:'com.x.y',name:'llm.delta',payload:{requestId:'r1',text:'你'}})` 投一条假事件后，该插件（`com.x.y`）的 iframe **恰好收到 1 条** `{v:1,type:'event',name:'llm.delta',payload:{requestId:'r1',text:'你'}}`；`pluginId` 改为一个无 iframe 的 id 再投一次后，已存在的 iframe 收到的 event 条数不变。

- [x] **T89 — `applyGeometry` 按 Layout 矩形摆放浮层面 iframe**
  [前置] T83、T57
  文件：`frontend/src/plugin-host.js`
  内容：非 nil 的 `surface` → 为每个 `rects[i]` 建/复用第 i 个 iframe，`left/top/width/height` 逐字取该 rect 的 `x/y/w/h`（**不在此处做任何换算**），圆角取 `r`。
  完成判据：给 `applyGeometry({pluginId:'com.x.y',rects:[{index:0,x:160,y:170,w:300,h:220,r:14}]})` 后 `#plugin-surface` 不含 `hidden`，且其内 iframe 的 `style.left/top/width/height` 依次为 `160px/170px/300px/220px`、`style.borderTopLeftRadius` 为 `14px`。

- [x] **T90 — `applyGeometry(null)` 隐藏并清空浮层面**
  [前置] T89
  文件：`frontend/src/plugin-host.js`
  内容：`surface` 为 nil → 给挂载点加 `hidden` 并移除全部 iframe。
  完成判据：在 T89 的终态之后调用 `applyGeometry(null)`，`#plugin-surface` 含 `hidden` 且其内 iframe 数量为 0。

- [x] **T91 — 插件 tab 内的面板视图打开**
  [前置] T83、T76
  文件：`frontend/src/plugin-host.js`、`frontend/src/plugins.js`
  内容：插件 `panel=true` 时管理列表提供「打开」按钮：显示 `#plugin-panel-view`、隐藏管理列表、把该插件的 iframe 挂入 `#plugin-panel-view`。
  完成判据：点击一个 `panel=true` 条目的「打开」后 `#plugin-panel-view` 不含 `hidden`、其内 iframe 数量为 1，且管理列表容器含 `hidden`。

- [x] **T92 — 面板视图的「返回」**
  [前置] T91
  文件：`frontend/src/plugins.js`
  内容：`#plugin-panel-back` 返回管理列表并移除 iframe。
  完成判据：在 T91 的终态之后点击 `#plugin-panel-back`，`#plugin-panel-view` 含 `hidden`、其内 iframe 数量为 0、管理列表容器不含 `hidden`。

- [x] **T93 — `main.js` 的模块导入与 `els` 表登记**
  [前置] T72、T76、T83
  文件：`frontend/src/main.js`
  内容：`import { PluginsTab } from './plugins';`、`import { PluginHost } from './plugin-host';`；`els` 表登记 `tabPlugins: $('tab-plugins')` 与新增的六个插件相关 id。
  完成判据：`grep -n "tabPlugins: \$('tab-plugins')" frontend/src/main.js` 命中 1 处，且两条 import 语句各命中 1 处。

- [x] **T94 — `main.js` 的 `switchTab` 新增一行 toggle**
  [前置] T93
  文件：`frontend/src/main.js`
  内容：`switchTab` 内新增 `els.tabPlugins.classList.toggle('hidden', name !== 'plugins');`。
  完成判据：`frontend/src/main.js` 中 `tabPlugins.classList.toggle('hidden', name !== 'plugins')` 字面出现 1 次。

- [x] **T95 — `switchTab` 既有四行 toggle 逐字保留**
  [前置] T94
  文件：`frontend/src/main.js`
  内容：既有四行 toggle 一字不改（门禁正则要求 `tabChat.classList.toggle('hidden', name !== 'chat')` 字面存在）。
  完成判据：`tabTodos.classList.toggle('hidden', name !== 'todos')`、`tabChat.classList.toggle('hidden', name !== 'chat')`、`tabRecords.classList.toggle('hidden', name !== 'records')`、`tabSettings.classList.toggle('hidden', name !== 'settings')` 四条字面各出现 1 次。

- [x] **T96 — `main.js` 的 `applyLayout` 接线浮层面**
  [前置] T89、T94
  文件：`frontend/src/main.js`
  内容：`applyLayout` 末尾新增一行把 `layout.pluginSurface || null` 交给 `pluginHost.applyGeometry(...)`（在既有 `live?.applyGeometry(...)` 之后）。
  完成判据：`grep -n "pluginSurface" frontend/src/main.js` 恰命中 1 行且该行所在函数为 `applyLayout`；行为上，注入含 `pluginSurface` 的 layout 后 `#plugin-surface` 内 iframe 被摆放，注入缺省该字段的 layout 后该挂载点被隐藏。

- [x] **T97 — `main.js` 的 `wire()` 接线插件组件**
  [前置] T93、T96
  文件：`frontend/src/main.js`
  内容：`wire()` 内实例化并接线 `PluginsTab`/`PluginHost`（`pluginsTab.wire()`、把两者串起来）；tab 点击仍走既有 `els.tabs.forEach` 那一段不改。
  完成判据：切到插件 tab 后管理视图容器不含 `hidden`（证明本体已接上），且 `grep -n "els.tabs.forEach" frontend/src/main.js` 仍命中 1 处。

- [x] **T98 — 市场地址保存接线**
  [前置] T97
  文件：`frontend/src/main.js`
  内容：`#market-base-save` 点击调用 `App.SaveMarketURL(els.marketBaseUrl.value)` 并把返回的 `MarketView` 交给 `pluginsTab.applyMarket(...)`；失败走既有 `fail(err)`。
  完成判据：点击 `#market-base-save` 后桥接收到的 `SaveMarketURL` 调用恰好 1 次且实参等于输入框的值，随后市场列表容器按返回的假 `MarketView` 重渲染（`.market-row` 数量等于假数据条目数）。

- [x] **T99 — 市场地址非法时的保存失败提示**
  [前置] T98
  文件：`frontend/src/main.js`、`frontend/src/plugins.js`
  内容：`SaveMarketURL` reject 时走 `fail(err)` 的既有提示路径，输入框保留用户输入。
  完成判据：让 `SaveMarketURL` reject（错误文案 `市场地址只支持 http/https`）后，提示区文本含 `http/https`，且 `#market-base-url` 的 `value` 仍等于用户输入的那个字符串。

## J. 门禁脚本同步与单测（T100–T114，含 T107b）

- [x] **T100 — `check-frontend.cjs` 的 Go 事件文件清单**
  [前置] T33、T50b
  文件：`.dev/check-frontend.cjs`
  内容：`goFiles` 数组（`:132-135`）新增 `'app_plugins.go'`——`app_plugins.go` 发出的确定性事件名 `pet:plugin-event` 必须有发出者（该文件被排除时，`plugin-host.js` 对它的 `EventsOn` 订阅——由 T106 纳入第 3 节扫描——会被判成「订阅了没人发的事件」，见 `:153`）。事件名一律为双引号字面量 `pet:plugin-event`（不是 `pet:plugin-*`）。本任务必须先于 T106 落地：T106 一旦把 `plugin-host.js` 纳入订阅扫描，若 `goFiles` 尚无 `app_plugins.go`，该 PASS 行会转 FAIL。
  完成判据：该数组中含 `'app_plugins.go'` 一项，且元素计数由 5 变为 6。

- [x] **T101 — `check-frontend.cjs` 的新增 CSS 段清单**
  [前置] T74
  文件：`.dev/check-frontend.cjs`
  内容：`newSections` 数组新增 `'/* --- plugins tab'` 与 `'/* --- plugin surface'`。
  完成判据：该数组中含上述两个字符串各 1 项，且元素计数由 4 变为 6。

- [x] **T102 — 第一处 tab 计数断言 4→5**
  [前置] T72
  文件：`.dev/check-frontend.cjs`
  内容：`:346` 附近的 `'the tabs are todos/chat/records/settings'` 断言改为 `todos/chat/records/plugins/settings` 且计数 `=== 5`。
  完成判据：`grep -n "the tabs are todos/chat/records/plugins/settings" .dev/check-frontend.cjs` 命中 1 处，且该断言中的 `data-tab` 计数为 `=== 5`。

- [x] **T103 — 第二处 tab 计数断言 4→5**
  [前置] T72
  文件：`.dev/check-frontend.cjs`
  内容：`:1297` 附近的 `!htmlIds.has('tab-voice') && (…data-tab…).length === 4` 改为 `=== 5`。
  完成判据：该表达式内的 `data-tab` 计数为 `=== 5`，且文件中 `=== 4` 的出现次数比改动前少 2。

- [x] **T104 — `#tab-plugins` 存在性断言**
  [前置] T72
  文件：`.dev/check-frontend.cjs`
  内容：新增一条 `htmlIds.has('tab-plugins')` 的断言（与既有 `htmlIds.has('tab-chat')` 同构）。
  完成判据：`grep -n "htmlIds.has('tab-plugins')" .dev/check-frontend.cjs` 命中 1 处。

- [x] **T105 — 共享滚动条规则断言纳入 `#tab-plugins`**
  [前置] T75
  文件：`.dev/check-frontend.cjs`
  内容：两处定点改动 —— ①`:1186` 的「the scrollable surfaces share one scrollbar rule」正则（`scrollbar-width`/`scrollbar-color` 组选择器列表 `.todo-list,\n.record-list,\n.chat-log,\n.voice-scroll,\n#tab-settings`）加 `#tab-plugins`；②`:1206` 的「the private scrollbar copies are gone」判定阈值**保持 `>= 4`**（不提高，避免把既有 4 条规则判成私有副本），仅因新选择器加入而自然覆盖到 `#tab-plugins`。既有四条滚动条检查名逐条保留。
  完成判据：`:1186` 处正则的选择器列表含 `#tab-plugins`（`grep -c "#tab-plugins" .dev/check-frontend.cjs` 比改动前增加 ≥1），且 `:1206` 的 `sel.split(',').length >= 4` 阈值字面未变（`grep -n "sel.split(',').length >= 4" .dev/check-frontend.cjs` 命中 1 处），脚本内既有的 `the records list and the chat log are on that rule`、`the private scrollbar copies are gone`、`the scrollbar is translucent and trackless` 三条断言名逐条仍存在。

- [x] **T106 — id/绑定/事件订阅三处扫描均纳入两个新组件模块**
  [前置] T97、T100
  文件：`.dev/check-frontend.cjs`
  内容：三处定点改动 —— ①第 1 节（`:53-57`）与第 2 节（`:77-81`）的源码扫描列表 `[mainJs, chatJs, liveJs]` 加入 `pluginsJs`（`frontend/src/plugins.js`）与 `pluginHostJs`（`frontend/src/plugin-host.js`），使新组件里的 `$('id')` 与 `App.*(` 被一并核对；②**第 3 节的事件订阅扫描**（`:148-152`，现为 `[...mainJs.matchAll(...)]`、`chatJs`、`liveJs` 三处硬编码）同样把这两个文件加入 `EventsOn\('([^']+)'` 的扫描（在 `:17` 旁新增两个 `fs.readFileSync`，并在 `:148-152` 后按同构写法各追加一次 `matchAll` 合并进 `subscribed`）。前置 T100 的理由：`plugin-host.js` 的 `EventsOn('pet:plugin-event', …)` 一旦被扫描到，`goFiles` 就必须已含 `app_plugins.go`，否则 `:153` 的 dangling 判定会把该订阅算成「订阅了没人发的事件」。
  完成判据：脚本中 `readFileSync` 读取 `frontend/src/plugins.js` 与 `frontend/src/plugin-host.js` 的语句各 1 处，且两者各被用于**三处**扫描（id 扫描、`App.*` 扫描、`EventsOn` 扫描）；第 3 节 `subscribed` 集合的基数比改动前增加（新增来自 `plugin-host.js` 的 `pet:plugin-event`），报告行 `every subscribed event is emitted by Go (N subscriptions)` 的 N 比改动前大，且该行仍为 PASS。

- [x] **T107 — `layoutReturning` 增两个浮层面绑定**
  [前置] T63
  文件：`.dev/check-frontend.cjs`
  内容：`layoutReturning` 数组（`:105`）新增 `'OpenPluginSurface'` 与 `'ClosePluginSurface'`。
  完成判据：该数组中含上述两项，且元素计数比改动前多 2。

- [x] **T107b — `check-frontend.cjs` 新增 `models.ts` 的 `pluginSurface` 断言（T120 点名的 PASS 行由此产出）**
  [前置] T107、T52h（T52h 已把 `pluginSurface?: PluginSurfaceView` 写进 `models.ts`，断言才有可断言之物）
  文件：`.dev/check-frontend.cjs`
  内容：在既有 `:117-121` 的 `'the Layout model carries the live overlay geometry'` 断言旁，按**同构**写法新增一条 `'the Layout model carries the plugin surface geometry'`，判据正则 `/pluginSurface\?: PluginSurfaceView;/` 作用于 `modelsTs`（与 `'Layout has no live member'` 同一失败文案风格）。没有本任务，T120 判据中点名的这条 PASS 行在 `check-frontend.cjs` 里**不存在**，T120 必然 FAIL。
  完成判据：`grep -n "the Layout model carries the plugin surface geometry" .dev/check-frontend.cjs` 命中 1 处，且 `node .dev/check-frontend.cjs` 输出中出现该断言名并以 `PASS` 结尾（前置是 T52h 已把 `pluginSurface?: PluginSurfaceView` 写进 `models.ts`）。

- [x] **T108 — `run-frontend-harness.cjs` 的绑定白名单**
  [前置] T97、T52h（绑定已导出）
  文件：`.dev/run-frontend-harness.cjs`
  内容：`methods` 数组（`:76-94`）新增本轮全部新绑定：`PluginsView`、`MarketView`、`InstallPlugin`、`UninstallPlugin`、`SetPluginEnabled`、`GrantPluginPermissions`、`PluginRequest`、`SaveMarketURL`、`OpenPluginSurface`、`ClosePluginSurface`。
  完成判据：`methods` 数组内含上述十个字符串且元素计数比改动前多 10。

- [x] **T109 — harness：插件 tab 可见且市场列表按假数据渲染**
  [前置] T108、T77
  文件：`.dev/run-frontend-harness.cjs`
  内容：新增判据 —— 点击 `data-tab="plugins"` 后管理视图可见、市场列表按假 `MarketView` 渲染。
  完成判据：`node .dev/run-frontend-harness.cjs` 输出中该判据名出现在 PASS 列表（其断言名含 `插件` 或 `plugin`），且 `#tab-plugins` 不含 `hidden`、`.market-row` 数量等于假数据条目数。

- [x] **T110 — harness：权限对话框展示声明清单**
  [前置] T109、T79
  文件：`.dev/run-frontend-harness.cjs`
  内容：新增判据 —— 权限对话框逐项列出插件声明的权限。
  完成判据：`node .dev/run-frontend-harness.cjs` 输出中该判据名出现在 PASS 列表，且对话框内勾选框文本集合等于假 `permissions` 数组。

- [x] **T111 — harness：安装按钮走完五步调用序列**
  [前置] T110、T81
  文件：`.dev/run-frontend-harness.cjs`
  内容：新增判据 —— 安装按钮依次调用 `InstallPlugin`/`GrantPluginPermissions`/`SetPluginEnabled`（假 market 响应驱动），并在途中显示 `下载`/`校验`/`解压`/`授权`/`启用` 五步状态。
  完成判据：`node .dev/run-frontend-harness.cjs` 输出中该判据名出现在 PASS 列表，且桥接收到的三个调用名按序相等、状态行出现过含 `下载` 与含 `启用` 的文本各一次。

- [x] **T112 — harness：浮层面 iframe 位置等于 layout 矩形**
  [前置] T111、T89
  文件：`.dev/run-frontend-harness.cjs`
  内容：新增判据 —— 下发含 `pluginSurface` 的假 layout 后，`#plugin-surface` 内 iframe 的位置尺寸逐项等于该 layout 的矩形。
  完成判据：`node .dev/run-frontend-harness.cjs` 输出中该判据名出现在 PASS 列表，且 iframe 的 `style.left/top/width/height` 与假 layout 的 `x/y/w/h` 逐项相等。

- [x] **T113 — `test-harness.js` 的绑定白名单**
  [前置] T97、T52h
  文件：`frontend/src/test-harness.js`
  内容：`methods` 数组补入与 T108 相同的十项绑定。
  完成判据：该数组中含上述十个字符串，且 `cd frontend && npx vitest run src/bubble-plan.test.js src/records-plan.test.js` 仍全部通过（既有 jsdom 用例不受影响）。

- [x] **T114 — jsdom 单测：`plugins.test.js` 四个行为**
  [前置] T113、T79、T82
  文件：`frontend/src/plugins.test.js`（新建）
  内容：四个独立用例 —— ①市场列表渲染；②权限清单渲染；③未授权时启用按钮 disabled；④安装失败提示路径。
  完成判据：`cd frontend && npx vitest run src/plugins.test.js` 退出码为 0，且报告中恰好 4 个用例通过、用例名分别对应上述四个行为。

## K. 文档（T115）

- [x] **T115 — `README.md` 同步（插件化章节 + 端到端冒烟步骤）**
  [前置] T98、T108
  文件：`README.md`
  内容：新增「插件化与插件市场」章节，写清插件包格式（manifest 字段与九项权限）、安装流程（下载→sha256→解压→授权→启用）、两种界面落点、数据目录新增的 `plugins/`＋`plugin-data/` 与 `plugins.json`、市场基址配置位置（「插件」tab）；补入端到端冒烟的人工复现步骤（启动 → 插件 tab → 市场列表加载 → 安装示例对话插件 → 授权 → 启用 → 打开面板视图 → 发一条消息看到流式渲染 → 触发一个气泡 → 禁用/卸载后只剩宠物可见）；并写明「本地提交与 remote 已就绪，线上 push 由用户执行」及其命令。
  完成判据：`README.md` 中新增章节标题含 `插件` 1 处、含 `sha256` 与 `gh-proxy.com` 各至少 1 处、冒烟步骤的五个关键词（`市场列表`、`授权`、`启用`、`流式`、`卸载`）在同一章节内均出现，且既有章节标题「快速开始」「番茄钟与对话提醒」「消除闪动」逐条仍存在。

## L. 构建与全量验收（T116–T126）

- [x] **T116 — `go vet ./...` 无告警**
  [前置] T35、T52、T58、T71、T32、T114
  文件：（无）
  内容：`go vet ./...`。
  完成判据：退出码 0。

- [x] **T117 — `go test ./... -count=1` 全绿**
  [前置] T116
  文件：`internal/plugins/*_test.go`、`app_plugins_test.go`、`app_plugin_surface_test.go`
  内容：`go test ./... -count=1`。
  完成判据：退出码 0 且输出中无 FAIL。

- [x] **T118 — 未新增任何第三方依赖**
  [前置] T116
  文件：`go.mod`、`go.sum`
  内容：`internal/plugins` 只用标准库（`archive/zip`、`crypto/sha256`、`encoding/json`、`net/http`、`embed`）。
  完成判据：`git diff --stat go.mod go.sum` 输出为空。

- [x] **T119 — 前端构建**
  [前置] T98、T52h（`frontend/wailsjs/go/**` 已重新生成，`main.js`/`plugins.js` 的 `App.*` 调用才有目标）
  文件：`frontend/dist/`（构建产物，`.gitignore` 忽略）
  内容：`cd frontend && npm run build`。
  完成判据：退出码 0 且 `frontend/dist/index.html` 存在。

- [x] **T120 — 静态接线门禁 PASS**
  [前置] T100–T107b、T119
  文件：`.dev/check-frontend.cjs`
  内容：`node .dev/check-frontend.cjs`。
  完成判据：退出码 0 且输出中 FAIL 计数为 0；其中必须逐条出现 PASS：「every element id referenced from JS exists in index.html」「every App.* call is an exported binding」「every App.* call is declared in App.d.ts」「every window call the frontend applies as a layout is typed as one」「every subscribed event is emitted by Go」「all N checked CSS sections are present」「the settings page is seven named groups」「the scrollable surfaces share one scrollbar rule」「the tabs are todos/chat/records/plugins/settings」以及 T107b 新增的「the Layout model carries the plugin surface geometry」（`models.ts` 含 `pluginSurface?: PluginSurfaceView`，与既有 `the Layout model carries the live overlay geometry` 同构），且设置页 id 白名单断言（`the settings page still carries every control`）同样 PASS（既有控件一条不减）。

- [x] **T121 — 行为 harness PASS**
  [前置] T108–T112、T119
  文件：`.dev/run-frontend-harness.cjs`
  内容：`node .dev/run-frontend-harness.cjs`。
  完成判据：退出码 0，输出中无 FAIL，且下列判据名全部出现在 PASS 列表中：T109–T112 四条新增判据，加上 T88b 的插件事件路由判据（假 `pet:plugin-event` 事件后该插件 iframe 恰好收到 1 条对应 `event` 消息）与 T84b 的握手判据（本 iframe 的 `ready` 后恰好收到 1 条字段逐项相等的 `init`）。

- [x] **T122 — `wails build -s` 链接成功**
  [前置] T117、T119
  文件：`build/bin/PetPC.exe`（构建产物，`.gitignore` 忽略）
  内容：`wails build -s`（前端已构建，跳过 Wails 自带前端构建）。
  完成判据：退出码 0 且 `build/bin/PetPC.exe` 存在。

- [ ] **T123 — 真实窗口区域像素门禁回归**
  > **实跑结果**：`powershell -File .dev\verify-region.ps1` 能跑、能定位窗口与宠物，12 条判据里 11 条 PASS；唯一未过的是「pet left edge / pet size unchanged (exact)」，差 1px（抗锯齿边缘阈值抖动）。**已用同机同一脚本对改动前基线提交 `bd5bb02` 编译的 exe 复跑 3 次，得到完全相同的 1px 失败**，故判定为环境既有噪声而非本 change 的回归。另：本机未装 `pwsh`（只有 Windows PowerShell 5.1），按 `powershell -File` 执行。
  [前置] T122
  文件：`.dev/verify-region.ps1`（不改）
  内容：`pwsh -File .dev\verify-region.ps1`。
  完成判据：脚本退出码 0，且输出中报告宠物位置在开关面板前后完全一致、面板裁剪区域正确、裁剪区域外点击穿透到桌面；若环境无交互桌面，则输出「无交互桌面」并在交付报告中记录该情形原文（按既有约定不计为失败）。

- [ ] **T124 — 实时浮层像素门禁回归**
  > **实跑结果**：`powershell -File .dev\verify-live.ps1` 能跑，9 条判据里 8 条 PASS；未过的是「the puff centre is painted (it is inside the ellipse)」。**同样已对基线 exe 复跑 2 次得到完全相同的失败**，属环境既有问题（该判据依赖椭圆内部单点取样）。
  [前置] T122
  文件：`.dev/verify-live.ps1`（不改）
  内容：`pwsh -File .dev\verify-live.ps1`。
  完成判据：脚本退出码 0，且输出中报告开关浮层时窗口不 resize 不移动、卡片与输入条画在 layout 给出的位置、椭圆中心是强调色而包围盒角落是纯桌面、宠物像素完全不变；无交互桌面时按 T123 的同一约定记录。

- [ ] **T125 — 浮层面真实窗口像素判据（独立脚本）**
  > **实跑结果**：`.dev/verify-plugin-surface.ps1` 已新建（522 行，T5b 已放行）并能跑出**关闭态**判据：窗口矩形 540×520、宠物偏移 390,160、声明矩形 1922,435 300×260，两条断言 PASS（未开浮层面时该矩形不属于窗口；宠物按预期绘制在窗口内）。**打开态未能跑通**：本环境的合成点击能到达桌宠的**窗口**（面板开得了、插件 tab 切得过），但到不了页面内的**控件**，因此无法点开插件面板视图、也就无法触达它持有的「浮层面」按钮。脚本按既有约定报告「NO INTERACTIVE DESKTOP（surface could not be driven open）」并 exit 0，**未伪报 PASS**。浮层面的几何由 `app_plugin_surface_test.go` 的纯函数单测（夹回/圆角/缩放换算/区域与视图同源）与行为 harness（iframe 摆放坐标与布局矩形逐项相等）覆盖。
  [前置] T122、T63
  文件：`.dev/verify-plugin-surface.ps1`（新建；**不改动** `.dev/verify-region.ps1` 与 `.dev/verify-live.ps1`；本文件由 T5b 放行纳入版本库）
  内容：以 `PETPC_DATA_DIR` 指向临时数据目录启动新构建的 exe → 预置「已装且已启用并已授权 `pet.surface`」的示例插件 → 打开浮层面 → 截屏断言「浮层面矩形内是插件绘制的内容、矩形外是纯桌面」→ 关闭浮层面 → 再截屏断言只剩宠物可见（沿用 `.dev/verify-region.ps1:32-60` 的 `/wailsWindow` 枚举与截图手法）。
  完成判据：`pwsh -File .dev\verify-plugin-surface.ps1` 退出码为 0，且输出中报告两条结论（浮层面矩形内已绘制、关闭后仅宠物可见）；无交互桌面时按 T123 的同一约定报告并记录。

- [ ] **T126 — 端到端冒烟（人工可复现）**
  > **实跑结果**：端到端冒烟步骤已写入 `README.md`「插件与插件市场 → 端到端冒烟（人工可复现）」共 11 步表；**未在真实桌面上逐步走完**（第 7 步起的点击无法在本环境驱动，同 T125）。报告如实记为 leftover，未伪报。
  [前置] T125、T115
  文件：`README.md` 的冒烟步骤（T115 写入）
  内容：按 README 步骤在真实桌面上走一遍完整序列；该序列覆盖本轮两条桥接判据的端到端形态 —— **握手**（打开插件面板视图后，插件脚本发出的 `ready` 使宿主回投 1 条 `init`，插件的发送按钮据 `init.granted` 解禁，见 T84b）与**事件下行**（发一条消息后 `llm.chat` 的流式增量经 `pet:plugin-event` 路由到该插件 iframe 并逐帧渲染，见 T50b/T88b）。
  完成判据：交付报告中记录该序列每一步的实际观察结果（每步一行：步骤 + 结果），含「面板视图打开后输入区从 disabled 变为可用（即收到 `init`）」与「回复逐字出现（即 `llm.delta` 逐帧到达）」两条观察；且最后一步观察到「窗口裁剪区域恢复为仅有宠物」（截图或 `PETPC_DEBUG=1` 的 debug.log 中浮层面关闭记录，二者之一作为证据）；无交互桌面时在报告中标注「无法执行（无交互桌面）」而非判定为代码失败。

## M. 需求对齐映射（双向，逐条不遗漏）

### 规格卡三块交付 → 任务

| 规格卡交付 | 任务 |
|---|---|
| 一、主仓上云（本地提交 + remote add + 分支就绪，线上 push 由用户执行） | T1、T2、T3、T4、T115（README 写清 push 命令）、T126 |
| 二、插件宿主：`internal/plugins`（manifest/发现/zip+sha256/卸载/启停/授权持久化） | T8、T9、T10–T15、T16–T25 |
| 二、插件宿主：根包 `app_plugins.go` Wails 绑定 | T33–T52、T52a–T52h（五个宿主方法绑定 + `PluginsView`/`MarketView` 两个视图绑定 + 重新生成 `frontend/wailsjs/go/**`）、T50b（`pet:plugin-event` 下行）、T63–T67、T68–T71 |
| 二、插件宿主：前端第 5 个 tab「插件」 | T72–T82、T93–T99 |
| 二、插件宿主：sandbox iframe（仅 `allow-scripts`）+ postMessage（含 ready→init 握手）+ 受权限门控统一调度 | T83–T88、T84b、T84c、T88b、T33–T35、T71 |
| 二、插件宿主：两种界面落点（面板内 380×520 滚动区；浮层面矩形并集 + 夹回） | T91、T92、T53–T67、T69、T75 |
| 三、插件市场（桌宠侧）：可配置基址 + gh-proxy 默认 + 离线兜底 | T7、T26–T32、T98 |
| 三、插件市场：安装流程 下载→sha256→解压→授权→启用 | T16–T20、T22、T81 |
| 【框架验证】官方示例对话插件真实验证（面板视图 + 流式 + 气泡 + 宿主大模型），**不改 `#tab-chat`** | T49、T50b、T51、T84b、T88b、T91、T31、T32、T73、T115、T126（插件内容在 `plugin-market` M2–M8） |
| 三条硬约束（永不 resize／面板固定 380×520／无多窗口 & `wailsWindow` 唯一） | design.md §1 逐条；判据见 T54、T58、T60–T62、T72、T83、T91、T123、T124 |

### 规格卡用户故事 → 任务

| 用户故事 | 任务 |
|---|---|
| 插件 tab 里浏览并安装插件 | T77、T81、T98 |
| 安装前看到权限清单并自己决定是否授权 | T79、T80、T22、T81 |
| 随时禁用或卸载，干净回到原状 | T21、T25、T66、T67 |
| 插件小界面（面板内视图或浮层面）贴合桌宠显示 | T89、T91、T92、T53、T58 |
| 浮层面位置大小由插件声明，宿主保证不越界 | T54、T55、T56、T57、T89、T112 |
| 只写 manifest + HTML/JS/CSS 即可做插件 | T10–T15、T68–T71、T83（字段与资源路由；包在 `plugin-market`） |
| 通过宿主接口访问待办/番茄钟/气泡/事件/大模型 | T36–T52 |
| 插件能联网但只限声明域名 | T45、T46、T15 |
| 主仓与市场仓彼此独立（一私有一公开） | T1、T5（市场仓在 `plugin-market` M1） |
| 本地提交与远端配置由工具完成、线上推送我自己执行 | T1、T2、T3、T115 |
| 既有门禁在插件化之后依然全绿 | T100–T114（含 T107b）、T116–T126 |

### 澄清清单 Q1–Q16 → 任务

| 澄清项 | 结论 | 任务 |
|---|---|---|
| Q1/Q11/Q16 主仓地址与分支策略 | 本地提交 + `origin`，推送由用户执行；`main` 与 `feat/plugin-system` | T1、T2、T4、T115 |
| Q2 两个仓库 | 主仓 + 市场仓 | T1、T5、`plugin-market` M1–M8 |
| Q3 插件能力受控（manifest + 前端资源；不能读写任意文件） | 单一调度入口 + 能力表 | T33–T52、T44、T71 |
| Q4 默认不信任：逐项授权 + 沙箱 + 网络白名单 | 授权持久化 + `allow-scripts` iframe + `netHosts` | T22、T79、T83、T45、T46、T15、T85 |
| Q5 本轮范围 = 宿主 + 加载器 + 最小市场 + 双仓 | 不做评分/评论/自动更新 | 范围外（无任务） |
| Q6/Q12 主仓私有、市场公开 | 两仓可见性写进 README 与市场仓 README | T115、`plugin-market` M8 |
| Q7 线上推送由用户手动执行 | 本 change 不含 push | T1（判据含「无远端跟踪引用」）、T115 |
| Q8 市场形态与基址可配置、默认镜像、不开 Pages | `MarketBaseURL` + 默认镜像 + `plugin-market` M7 | T7、T26–T30、T98 |
| Q9/Q14 两种界面落点；每个面必须是矩形（可圆角） | 面板视图 + 浮层面；`computePluginSurface` | T53–T67、T89–T92 |
| Q10 sandbox iframe + 白名单 API + 只放行声明域名；9 项权限最终版 | `allow-scripts` + `PluginRequest` + 9 项权限 | T83、T84b、T84c、T33、T13、T35–T52 |
| Q13 本轮保留 `#tab-chat`，对话先作为官方插件验证框架 | 不改 `chat.js` 与会话状态机；示例插件走 `llm.chat` | T49、T50b、T88b、T73、T91、T115、T126；范围外第 5 项 |
| Q15 市场仓内每插件一目录 + zip；index.json 记八项字段；安装到 `%AppData%\PetPC\plugins\<id>\` | 市场契约 + 安装目录 | T17、T27、T31、T32、`plugin-market` M2–M6 |
| Q16 两个仓地址、分支、忽略构建产物 | `origin` 地址、分支、`.gitignore` | T1、T4、T5、T6 |

### 澄清 doubts / risks / placeholders → 任务

| 条目 | 任务 |
|---|---|
| doubts-1 市场仓名字未逐字确认 | T7（基址是**可配置**的单一字段，`remote set-url` 或改这一行即可切换，无需改代码）、T98 |
| doubts-2 主仓线上默认分支是否 main | T2、T115（README 写明用户可在 GitHub 改默认分支） |
| doubts-3 市场仓本地目录在主仓内、主仓忽略 `market/` | T5、T6、`plugin-market` M1 |
| doubts-3（iframe 可行性）→ 见 design.md §13 C3 | T83、T84、T85、T84b、T84c、T109–T112、T121、T125（实测受限即 blocked，不切分支） |
| risks-1 浮层面像素/矩形不一致 | T53、T54、T58、T89、T112、T123、T124、T125 |
| risks-2 第 5 个 tab 撞两处 `=== 4` 与设置页白名单 | T72、T73、T100、T101、T102、T103、T104、T105、T106、T107b、T120 |
| risks-3 沙箱 iframe 在 WebView2 未验证 | T83、T84b、T88b、T112、T121、T125 |
| risks-4 用户未手动 push 前交付物只在本地 | T1、T115 |
| placeholders-1 市场远端真实地址（市场仓未上线会 404） | T30（回退 + `Notice`）、T31（内置源兜底）、T98（用户填地址即可切换） |
| placeholders-2 zip 的 sha256 与下载地址（依赖真实打包产物） | T16、T31、T32、`plugin-market` M5、M6 |
| placeholders-3 示例插件依赖用户本机 LLM 配置 | T50（未就绪即返回错误）、T115（README 写明提示）、T126 |
| `models.ts` 的 `Layout.pluginSurface` 断言（`check-frontend.cjs`） | T107b、T120 |
| prdCollection | 无 PRD 链接（清单为空）→ 无对应任务 |
| `.gitignore` 白名单（第 4 个门禁脚本） | T5b、T6、T125 |

### 超纲任务的溯源说明

| 任务 | 溯源 |
|---|---|
| T53–T56（映射、夹回、丢弃、全丢弃） | 规格卡「宿主把这些矩形并集接进窗口裁剪区域，并强制夹回窗口边界」＋ Q14 的硬边界「插件面不能超出既有固定窗口范围」；不夹回会在桌面留下未绘制像素（README 10(d) 同类故障） |
| T65（有提问气泡时拒绝打开浮层面） | 清单 Q4/规格卡「未授权的 API 调用被拒绝并返回明确错误，不做静默降级」的同构要求 + `docs/reminder-pomodoro.md:487-512`「提问气泡不可被顶掉」：浮层面若要槽位就会**删掉**提问而不是遮住它（与 `OpenLive` 的既有拒绝同因） |
| T66/T67（停用/卸载关闭浮层面） | 规格卡「卸载即删除目录…」与「区域必须等于页面真正绘制的东西」的必然推论：插件消失后浮层面矩形不能再留在裁剪区域里 |
| T68（`bridge.js` 由宿主提供） | Q4/Q10 的 sandbox 前提：不设 `allow-same-origin` 时插件无法 import 宿主脚本，桥接脚本必须由宿主以子资源形式提供；`docs/llm-chat.md:203-222` 要求界面留在既有窗口内 |
| T85（`source === iframe.contentWindow` 校验） | Q4「默认不信任」的直接落地：不校验来源就等于任意页面都能代插件发起请求；同源子资源可达而不透明源不可达是 C3 收敛的同一前提 |
| T107（`layoutReturning` 增两项） | `.dev/check-frontend.cjs:103-111` 的既有工程协议（窗口调用若被前端喂给 `applyLayout` 就必须类型为 `main.Layout`）；本变更新增两个此类绑定 |
| T100/T106（`goFiles` 增 `app_plugins.go`、订阅扫描纳入两个新组件） | `.dev/check-frontend.cjs:132-135` 的 `goFiles` 清单与 `:148-152` 的 `EventsOn` 扫描列表互为对偶：`:153` 的 dangling 判定同时依赖两者；规格卡 testing 第 3 条点名的「`plugin-host.js` 订阅 `pet:plugin-event`」要求这两处一起改 |
| T125（新增独立像素门禁脚本） | 规格卡测试计划第 7 条要求浮层面几何有判据、第 6 条要求既有像素门禁仍 PASS；把新判据写成独立脚本才能同时满足「既有门禁不改」与「新能力有真实像素覆盖」 |
| T115（README 冒烟步骤） | 规格卡测试计划第 8 条「端到端冒烟（人工可复现步骤写进 README）」 |
| T5/T6（`.gitignore` 增 `market/` 且既有规则不变） | 清单 doubts-3 的确认结论（市场仓目录建在主仓内、须忽略）；T6 是防回归的负面判据（覆盖既有全部 10 条 pattern 与 4 条白名单） |
| T5b（`.gitignore` 放行 `.dev/verify-plugin-surface.ps1`） | `.gitignore:11-16` 的既有约定：三个前端门禁脚本是**项目源**而非 scratch（「a change to a gate has to be reviewable alongside the code it guards」）；T125 新建的第 4 个门禁脚本按同一约定纳入版本库 |
| T107b（`check-frontend.cjs` 的 `pluginSurface` 断言） | 规格卡 implementation/testing 第 3 条「门禁脚本同步是硬性交付内容」＋`.dev/check-frontend.cjs:117-121` 的既有 `'the Layout model carries the live overlay geometry'`（同构先例：`Layout` 每新增一个几何成员就该有一条对应断言）＋ T120 的 PASS 清单点名该断言名：没有创建该断言的任务，T120 的点名断言无对应语句，T120 必然 FAIL |
| T52（权限与调度表逐项对应） | 清单 Q10 的「9 项权限最终版」需要一个**完备性**判据，否则漏接一项在功能上不可见 |
| T52a–T52g（五个绑定与两个视图绑定） | 规格卡【二】「新增根包 `app_plugins.go` 暴露 Wails 绑定」＋规格卡 testing 第 3/4 条的硬约束（前端调用的绑定必须被 Go 导出、harness 假桥接 `methods` 白名单缺方法会让 `wire()` 抛错）；每条绑定单列才满足「一个任务一个可观察结果」 |
| T52h（重新生成 `frontend/wailsjs/go/**`） | `.gitignore:22-23` 与 `README.md:731` 的既有约定（绑定已提交、供前端类型检查）＋`check-frontend.cjs:89-111` 对 `App.d.ts`/`models.ts` 的断言；`wails build -s` 不重新生成绑定，故不能并入 T122 |
| T50b/T88b（`pet:plugin-event` 事件下行） | 规格卡 Q10「白名单 API」与【框架验证】「流式渲染」要求 Go→插件的下行链路存在；`check-frontend.cjs:136-137` 只认 `emitEvent(<标识符>)` 或 `emitEvent("字面量")`，事件名必须钉死为确定名才不会被判成「订阅了没人发的事件」 |
| T84b/T84c（ready→init 握手） | 规格卡 Q10「sandbox iframe + postMessage 桥接」的必要第一步：规格卡 impl_decisions 与 `plugin-market` §3.3 第 2 条都要求插件按宿主下发的 `granted` 决定 UI 可用性，没有 `init` 这一条，示例插件的框架验证没有实现路径 |

### 范围外（规格卡 10 项，逐条不做，故无任务）

不做评分/评论/下载量/自动更新/版本回滚；不做插件签名与非对称校验；不做任意曲线外形与逐像素 alpha 掩码；**不做独立市场窗口或第二个 Wails 程序**；不改动/迁移/删除既有 `#tab-chat`（T73 与 T120 的判据要求其 markup 与会话状态机逐字保留）；不做 exe 的 Release 附件；不开 GitHub Pages；不由 agent 执行线上 push（T1 的判据即为「没有任何远端交互」）；不做多市场/私有源聚合（`MarketBaseURL` 是单一基址）；不做插件间依赖解析。
