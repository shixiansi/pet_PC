# tasks.md — plugin-market

> 每条任务是**单一、可独立核对**的终态：一个任务只断言一个可观察结果，一个判据只描述实施完成后的状态（RED/GREEN/REFACTOR、"先失败再通过"属 D 阶段执行协议，此处不写）。
> `[前置]` 列出必须已完成的编号，依赖顺序见 design.md §6。
> 本文件即后续 D 阶段的执行清单与交付报告镜像（条目标题与勾选状态可逐条核对）。
> 全部路径相对 `F:\pet_pc\market\`（独立 git 仓，主仓 `.gitignore` 的 `market/` 忽略它）。

## A. 市场仓就绪（M1）

- [x] **M1 — `market/` 独立 git 仓与远端就绪（不推送）**
  [前置] 跨 change：`plugin-system` T5（主仓 `.gitignore` 的 `market/` 规则先就位；否则主仓会把本仓误当嵌套仓库跟踪）
  文件：`market/.git/`、`market/.gitignore`
  内容：在 `F:\pet_pc\market\` 执行 `git init`、`git branch -M main`、`git remote add origin https://github.com/shixiansi/pet-pc-plugins.git`（公开仓）；`.gitignore` 忽略 OS/编辑器噪声但**不**忽略 `dist/`。**不执行任何 `git push`**。
  完成判据：`git -C F:\pet_pc\market rev-parse --abbrev-ref HEAD` 输出 `main`；`git -C F:\pet_pc\market remote get-url origin` 输出恰为 `https://github.com/shixiansi/pet-pc-plugins.git`；`git -C F:\pet_pc\market for-each-ref --format='%(refname)' refs/remotes | wc -l` 为 0（证明没有任何远端跟踪引用、未推送过）；`git -C F:\pet_pc status --porcelain` 的输出中不出现 `market/`（证明主仓忽略规则生效）。

## B. 市场目录（M2）

- [x] **M2 — `index.json` 的条目字段完整且取值合法**
  [前置] M3
  文件：`market/index.json`
  内容：根对象含 `version`、`updatedAt`、`plugins`；`plugins` 恰 1 条，含 `id`/`name`/`version`/`author`/`description`/`permissions`/`download`/`sha256` 八个键。
  完成判据：`Get-Content market/index.json | ConvertFrom-Json` 可解析；该条目八个必填键全部存在且均非空字符串（`permissions` 为非空数组）；`id` 匹配 `^[a-z0-9]+(\.[a-z0-9][a-z0-9-]*)+$`、`version` 匹配 `^[0-9]+\.[0-9]+\.[0-9]+$`、`download` 不以 `http` 开头（相对路径）、`sha256` 匹配 `^[0-9a-f]{64}$`。

## C. 官方示例插件（M3–M4）

- [x] **M3 — `manifest.json` 逐字段符合宿主校验规则**
  [前置] M1；跨 change：`plugin-system` T10–T15（manifest 字段与校验规则先钉死，本任务判据要交给 `internal/plugins.ParseManifest` 解析）
  文件：`market/plugins/com.shixiansi.plugin-chat/manifest.json`
  内容：`id`/`name`/`version`/`author`/`description`/`permissions`/`panel`/`surface`/`entry` 九个键；`permissions` 恰为 `["llm.chat","pet.bubble","pet.surface"]`；`panel=true`；`surface` 恰 1 块 `{dx:160,dy:10,w:300,h:260,radius:14}`；`entry="panel.html"`；**不含** `net.fetch` 与 `netHosts`。
  完成判据：`ConvertFrom-Json` 后 `permissions` 数组计数为 3 且逐项等于上述三项；`panel` 为 `$true`；`surface` 数组计数为 1 且该对象的 `dx/dy/w/h/radius` 依次为 `160/10/300/260/14`；`entry` 等于 `panel.html`；对象不含 `netHosts` 属性；把该 manifest 交给主仓 `internal/plugins.ParseManifest` 解析（姊妹 change T10–T15 的实现）返回 `err == nil`。

- [x] **M4 — 插件前端资源存在且不含 Go 代码**
  [前置] M3
  文件：`market/plugins/com.shixiansi.plugin-chat/panel.html`、`app.js`、`style.css`
  内容：`panel.html` 声明 charset、引入 `./style.css` 与 `./app.js`（`type="module"`），并含输入区（textarea + 发送 + 停止 + 状态行 + 回复区 + 气泡按钮 + 浮层面按钮）；`app.js` 实现 design.md §3.3 的七条行为（**含握手**：启动即发 `{v:1,type:'ready'}`，收到 `init` 后才按 `init.granted` 决定 UI 可用性、才允许发 `request`）；`style.css` 无 `box-shadow`/`filter`/`text-shadow`/`transform`。插件目录内**不含**任何 `.go` 文件。
  完成判据：三个文件均存在；`Select-String -Path market/plugins/com.shixiansi.plugin-chat/*.css -Pattern '^\s*(box-shadow|filter|text-shadow|transform)\s*:'` 无匹配；`Get-ChildItem market/plugins/com.shixiansi.plugin-chat -Filter *.go` 返回空；`Select-String -Path market/plugins/com.shixiansi.plugin-chat/panel.html -Pattern 'type="module"','./style.css','./app.js'` 三项各至少 1 处匹配；`Select-String -Path market/plugins/com.shixiansi.plugin-chat/app.js -Pattern "type:\s*'ready'"` 至少 1 处匹配，且 `app.js` 中发出 `request` 的代码路径全部位于 `init` 处理分支之后（`init` 前的 `request` 发送点为 0）。

## D. 打包与一致性（M5–M6）

- [x] **M5 — `tools/package.ps1` 产出 zip（根目录含 manifest 与资源）**
  [前置] M4
  文件：`market/tools/package.ps1`、`market/dist/com.shixiansi.plugin-chat-1.0.0.zip`
  内容：脚本先删后建 `dist/<id>-<version>.zip`，zip 内为 `manifest.json` 与插件目录内的资源（**不带**外层目录），并在打包前断言 `entry` 指向的文件与三份资源都存在。
  完成判据：运行 `pwsh -File market\tools\package.ps1` 退出码为 0、`market/dist/com.shixiansi.plugin-chat-1.0.0.zip` 存在；用 `[System.IO.Compression.ZipFile]::OpenRead` 读该 zip 的条目名集合，该集合包含 `manifest.json`、`panel.html`、`app.js`、`style.css` 四个名字，且**没有任何**条目名以 `/` 结尾或包含 `/`（即全部位于 zip 根）。

- [x] **M6 — 实算 sha256、回写 `index.json`、复制进主仓内置源**
  [前置] M5；**排到姊妹 change 的 T31/T32 之前**（T31 落盘主仓内置源、T32 断言两侧 sha256 一致，消费的正是本任务的产物）
  文件：`market/tools/package.ps1`、`market/index.json`、`F:\pet_pc\internal\plugins\sample\index.json`、`F:\pet_pc\internal\plugins\sample\com.shixiansi.plugin-chat-1.0.0.zip`
  内容：脚本用 `Get-FileHash -Algorithm SHA256` 实算 zip 的 sha256（小写）写入条目的 `sha256`；用包内 manifest 的 `permissions`/`name`/`version`/`author`/`description` 重写条目的同名字段；把 zip 与 `index.json` 复制到主仓 `internal/plugins\sample\`（同名）。
  完成判据：`Get-FileHash market/dist/com.shixiansi.plugin-chat-1.0.0.zip -Algorithm SHA256` 的小写值与 `(Get-Content market/index.json | ConvertFrom-Json).plugins[0].sha256` 逐字符相同；该条目的 `permissions` 与包内 `manifest.json` 的 `permissions` 逐项相同；`Get-FileHash F:\pet_pc\internal\plugins\sample\com.shixiansi.plugin-chat-1.0.0.zip` 与市场仓 dist 的同一 zip 哈希相同。

## E. 静态市场页（M7）

- [x] **M7 — `index.html` 单文件市场页（无外部依赖、无脚本）**
  [前置] M6
  文件：`market/index.html`
  内容：单文件、纯静态，展示插件的名称/版本/作者/描述/权限清单/下载链接（相对 `dist/com.shixiansi.plugin-chat-1.0.0.zip`）与安装说明；不引用任何外部 URL、不含 `<script>` 标签；不配置任何 Pages 工作流。
  完成判据：文件存在且 `Select-String -Pattern '<script' market/index.html` 无匹配；`Select-String -Pattern 'https?://' market/index.html` 无匹配（无外部依赖）；`Select-String -Pattern 'dist/com.shixiansi.plugin-chat-1.0.0.zip' market/index.html` 至少 1 处匹配；`market/` 下不存在 `.github/workflows/` 目录。

## F. 文档（M8）

- [x] **M8 — `README.md`（提交规范 + 打包发版 + 安装位置 + 镜像基址）**
  [前置] M6、M7
  文件：`market/README.md`
  内容：写清 ①插件目录结构（`manifest.json` 字段与九项权限的含义）；②打包发版步骤（改 `plugins/<id>/` → 运行 `tools/package.ps1` → 提交 `dist/` 与新 `index.json`）；③桌宠侧安装位置 `%AppData%\PetPC\plugins\<id>\`；④桌宠侧市场基址的镜像形式 `https://gh-proxy.com/https://raw.githubusercontent.com/shixiansi/pet-pc-plugins/main`；⑤若实际远端仓名与此不同，只需 `git remote set-url` 与桌宠侧市场基址各改一处，产物与目录结构不变；⑥本仓不含 exe / 构建产物，桌宠由使用者自行 build。
  完成判据：`market/README.md` 中六个主题各自的标题或关键词均存在（`manifest`、`package.ps1`、`%AppData%\PetPC\plugins`、`gh-proxy.com`、`remote set-url`、`自行 build` 各至少 1 处匹配），且文件同时含「本仓为公开仓、主仓为私有仓」的说明 1 处以上。

## G. 需求对齐映射（双向，逐条不遗漏）

### 规格卡与清单 → 任务

| 规格卡/清单条目 | 任务 |
|---|---|
| 【三】市场仓 `index.json`（市场目录） | M2 |
| 【三】市场仓 `plugins/<插件id>/`（插件源码与资源） | M3、M4 |
| 【三】市场仓 `dist/<id>-<version>.zip`（打包产物，带 sha256） | M5、M6 |
| 【三】市场仓 `index.html`（人看的静态市场页） | M7 |
| 【三】桌宠侧通过可配置基址拉取 index.json、默认走 gh-proxy 镜像 | M8（写明基址形式）；拉取实现属姊妹 change T7/T26–T30 |
| 【三】安装流程 下载→sha256→解压→授权→启用 | M6（产出可校验的 zip 与 sha256）；流程实现属姊妹 change T16–T22、T81 |
| 【框架验证】官方示例对话插件（注册面板视图 + 流式渲染 + 弹气泡 + 调用宿主大模型），不改 `#tab-chat` | M3、M4（插件本体 + ready/init 握手与按 `granted` 的降级）+ 姊妹 change T31/T32/T49/T50b/T51/T84b/T88b/T91（宿主侧验证） |
| 姊妹 change 的跨 change 前置（本仓必须先行） | M1 ← plugin-system T5；M3 ← T10–T15；M2–M6 ← T31/T32（见 design.md §6） |
| Q2/Q6/Q12 市场仓为独立公开仓 | M1、M8 |
| Q5 本轮最小市场（不做评分/评论/自动更新） | 本 change 仅 M2/M5/M7 三个产物，无额外任务（范围外） |
| Q8 市场形态为 index.json + 插件包 + 静态页；本轮不开 Pages | M2、M5、M7（M7 判据含「无 workflows 目录」） |
| Q15 每插件一目录 + 打包 zip；`index.json` 记八项字段；安装到 `%AppData%\PetPC\plugins\<id>\` | M2、M3、M5、M8 |
| Q16 两仓地址；构建产物不进版本库 | M1（远端地址）；M8（写明不含 exe/构建产物） |
| 清单 doubts-1 市场仓名未逐字确认 | M1、M8（写清改一处即可切换） |
| 清单 doubts-3 市场仓目录在主仓内、主仓忽略 `market/` | M1 判据 |
| 清单 risks-4 用户未手动 push 前交付物只在本地 | M1 判据（远端跟踪引用为 0）、M8 |
| 清单 placeholders-1 远端地址暂不可访问 | M6（主仓内置源副本作为可用兜底）、M8（写明填入基址即可切远端） |
| 清单 placeholders-2 zip 的 sha256 与下载地址依赖真实打包产物 | M5、M6（实算回写，禁用假值） |
| 清单 placeholders-3 示例插件依赖用户本机 LLM 配置 | M4（`app.js` 在缺授权/未就绪时显示提示）、M8 |

### 超纲任务的溯源说明

| 任务 | 溯源 |
|---|---|
| M1 的 `.gitignore` | 规格卡 Q16「两个仓库均忽略构建产物」与清单 doubts-3「市场仓单独 git init」的必要实现：本仓的 `dist/` 必须提交，故忽略清单要与主仓不同 |
| M6 的「回写 `index.json`」 | 清单 placeholders-2「占位值一律用真实计算结果，不使用假值」——手工维护 sha256 必然漂移，故由脚本回写 |
| M8 的第 ⑤ 条（改一处即可切换仓名） | 清单 doubts-1（仓名未逐字确认）的落地口径：把不确定性收敛为两处可改配置，而不是留一个未定分支 |
| M4 的「不含任何 `.go` 文件」 | 规格卡用户故事「第三方插件作者只写一份 manifest + HTML/JS/CSS」，负面判据防止示例插件引入后端 |

### 范围外（规格卡 10 项 + 清单补充，逐条不做，故无任务）

不做评分/评论/下载量/自动更新/版本回滚；不做插件签名与非对称校验；不做多市场/私有源聚合；不做插件间依赖解析；不做 exe 的 Release 附件；**不开 GitHub Pages、不写任何 Pages 工作流**；**不由 agent 执行线上 push**；不做插件的自动构建 CI（由 `tools/package.ps1` 本地产出并提交）；不改动主仓 `#tab-chat`；不为插件提供任何后端（只有 manifest + 前端资源）。
