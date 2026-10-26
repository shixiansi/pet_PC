# 插件市场仓库 `pet-pc-plugins`（公开）：市场目录 + 官方示例对话插件 + 打包与发布规范

- change_name: `plugin-market`
- 目标仓库: `F:\pet_pc\market\`（**独立 git 仓**，在主仓目录内、被主仓 `.gitignore` 的 `market/` 忽略；远端 `https://github.com/shixiansi/pet-pc-plugins.git`，公开）
- 上游规格卡: 已批准规格卡（PetPC 插件化 + 插件市场，三块交付落在两个 GitHub 仓库）
- 上游澄清清单: `wiki/queries/checklists/petpc-github-muzfq8ul.md`（Q2/Q5/Q6/Q8/Q12/Q15/Q16、doubts-1/doubts-3、risks-4、placeholders-1/placeholders-2）
- 姊妹 change: `openspec/changes/plugin-system`（主仓的插件宿主；本 change 的产物是它的内置示例源与市场契约来源）

## 1. 问题（现状实证）

| 事实 | 出处（已实读） |
|---|---|
| 主仓**无任何远端**，工作区干净，唯一分支 `feat/pomodoro-prefocus-plan` | `.git/config`（仅 `[core]`）、`.git/HEAD` |
| `market/` 在主仓内**不存在**，且被同一份 impl_decisions 要求由主仓忽略（独立 git 仓） | `glob market/**` 无结果；主仓 `.gitignore:1-35` 当前无 `market/` 行 |
| 桌宠侧的市场目录契约已由姊妹 change 钉死：条目必须有 `id`/`version`/`download`/`sha256`，`permissions` 取 9 项之一 | `openspec/changes/plugin-system/design.md` §2.1、§5、§10 |
| 插件包格式（manifest 字段与校验规则）已由姊妹 change 钉死 | `openspec/changes/plugin-system/design.md` §3.1、`tasks.md` T10–T15 |
| 安装产物落 `<dataDir>/plugins/<id>/`（`%AppData%\PetPC\plugins\<id>\`） | `internal/store/store.go:410-445`（`DefaultDir`）、姊妹 change tasks.md T17 |
| 主仓需要一份**已提交**的内置示例源（`internal/plugins/sample/`），其 zip 必须与本仓 `dist/` 产物**同字节** | 姊妹 change design.md §13 C1、tasks.md T32 |
| `raw.githubusercontent.com` 国内不通，用 `gh-proxy.com` 前缀绕行；市场基址默认即该镜像 | `docs/genshin-tts.md:479`、`:688-689`；姊妹 change design.md §2.1 |
| 本轮**不**开 GitHub Pages，市场页随仓库分发；**不**由 agent 执行线上 push | 规格卡范围外第 7、8 项；清单 Q8 补充项 |

## 2. 变更目标（逐条落任务）

1. **市场仓落地**：`market/` 独立 git 仓、远端 `origin` 指向 `https://github.com/shixiansi/pet-pc-plugins.git`（公开）、默认分支 `main`；**线上 push 由用户手动执行**。
2. **市场目录**：`index.json` 按姊妹 change 的契约写条目（`id`/`name`/`version`/`author`/`description`/`permissions`/`download`/`sha256`；示例插件未声明 `net.fetch`，故无 `netHosts` 键）。
3. **官方示例对话插件**：`plugins/com.shixiansi.plugin-chat/`（`manifest.json` + `panel.html` + `app.js` + `style.css`），真实验证「插件面板视图 + 流式渲染 + 弹气泡 + 调用宿主已配置的大模型」，**不触碰主仓既有 `#tab-chat`**。
4. **打包产物**：`dist/com.shixiansi.plugin-chat-1.0.0.zip` + 由脚本计算真实 sha256 并回写 `index.json`；产物与主仓 `internal/plugins/sample/` 的副本同字节。
5. **静态市场页与提交规范**：`index.html`（人看的列表页，本轮不托管）、`README.md`（插件提交规范、打包与发版步骤、安装位置说明）。

## 3. 边界（与主仓、与用户职责的界线）

- 本 change **不改动主仓任何源码**；唯一跨仓动作是把打包产物**复制**进主仓 `internal/plugins/sample/`（该复制在姊妹 change 的 T32 判据中被核对 sha256 一致，本 change 的 M6 从市场侧核对同一件事）。
- 本 change **不执行 `git push`**。M1 的判据刻意包含「不存在任何远端跟踪引用」，使「未推送」这件事可被机械核对。
- 插件**不保存任何密钥**：它只通过宿主 `llm.chat` 转发（`app_plugins.go` 的 `PluginRequest`），因此本仓无需任何凭据或环境变量。
- 不引入任何构建依赖：打包脚本只用 PowerShell 内置的 `Compress-Archive` 与 `Get-FileHash`（本仓工具链环境已由 `.dev/env.ps1` 提供，见姊妹 change 的同一约定）。

## 4. 上游协议遵循说明（上游协议引用 → 本计划如何遵守）

下列协议**全部来自规格卡/澄清清单声明的可定位出处**（路径或明确协议名），每条均已实读原始文件，未引入规格卡之外的协议标准；未声明任何上述清单之外的协议标准。

| 协议引用（可定位出处） | 本计划的遵守方式 |
|---|---|
| `openspec/changes/plugin-system/design.md` §5、§10（市场条目字段：`id`/`version`/`download`/`sha256` 必填，`permissions` 取 9 项之一，`download` 可相对或 `http(s)` 绝对） | M2 逐字段按该契约写 `index.json`；M7 与 M8 的判据逐项核对字段名与取值域 |
| 同文件 §3.1（manifest 字段与校验规则：`id` 反向域名式、`version` 语义化、`permissions` 9 项、`netHosts`、`panel`、`surface`、`entry`） | M3 的示例插件 `manifest.json` 逐字段符合；判据含「`permissions` 恰为 `llm.chat`/`pet.bubble`/`pet.surface` 三项、`panel=true`、`surface` 恰 1 块且各值在界内、`entry=panel.html`、未声明 `net.fetch` 故无 `netHosts`」 |
| 同文件 §3.2（安装流程与包内 manifest 为权威：市场条目权限必须与包内一致） | M2 与 M3 的权限集合由 M6 的判据强制一致（打包脚本从包内 manifest 读出权限写回 `index.json`，不允许手工两处维护） |
| 同文件 §5.1（内置源 zip 与市场 `dist/` 产物必须同字节，sha256 取真实计算结果） | M5 产出 `dist/*.zip`，M6 计算真实 sha256、回写 `index.json`、并把该 zip 复制进主仓 `internal/plugins/sample/`；判据两侧各钉一次 |
| `docs/genshin-tts.md:479`、`:688-689`＋`_archive/tts-realtime/tools/tts-p0/fetch.ps1:64-72`：`raw.githubusercontent.com` 不通，用 `gh-proxy.com` 前缀绕行 | M8 的 README 写明镜像访问形式 `https://gh-proxy.com/https://raw.githubusercontent.com/shixiansi/pet-pc-plugins/main/index.json`，与桌宠侧默认基址一致 |
| 规格卡范围外第 7 项（不开 GitHub Pages）、第 6 项（exe 不作为 Release 附件） | M1 的判据含「仓库内不含 `build/`、不含 exe」；M7 的市场页只是仓库内静态文件，不配置任何 Pages 工作流 |
| 规格卡范围外第 8 项（由 agent 执行线上 push 属范围外） | M1 的判据含「`git for-each-ref refs/remotes` 输出为空」，即明确「仅本地」 |
| 清单 Q15（市场仓内每插件一目录 + 打包 zip；`index.json` 记 id/名称/版本/作者/描述/权限/下载地址/sha256；安装到 `%AppData%\PetPC\plugins\<id>\`） | M2、M3、M5、M8 逐项覆盖；M8 的 README 写明安装位置 |
| 清单 doubts-3（市场仓目录建在主仓内、主仓忽略 `market/`） | M1 的判据含「主仓 `git status --porcelain` 不出现 `market/`」 |
| 清单 doubts-1（市场仓名字未逐字确认，暂定 `shixiansi/pet-pc-plugins`） | M1 的判据只要求 `origin` 为该地址；M8 的 README 写明「若实际仓名不同，改一次 `git remote set-url` 与桌宠侧市场基址即可，产物与目录结构不变」 |
| 规格卡「插件 = 一份 manifest.json 加一些 HTML/JS/CSS 的小包，只需懂前端即可扩展」 | M3 的示例插件只用三份前端文件 + 一份 manifest，不含任何 Go 代码与构建步骤；M4 的判据含「目录内不含 `.go` 文件」 |

## 5. 影响面（文件级）

**新增（`F:\pet_pc\market\`，独立 git 仓）**：`README.md`、`index.json`、`index.html`、`.gitignore`、`tools/package.ps1`、`plugins/com.shixiansi.plugin-chat/manifest.json`、`plugins/com.shixiansi.plugin-chat/panel.html`、`plugins/com.shixiansi.plugin-chat/app.js`、`plugins/com.shixiansi.plugin-chat/style.css`、`dist/com.shixiansi.plugin-chat-1.0.0.zip`。

**跨仓复制（不是本 change 的新增物，而是它的产物）**：`F:\pet_pc\internal\plugins\sample\`（`index.json` 与同名 zip 的副本）。

**不动**：主仓任何源码、`frontend/`、`docs/`、主仓 `README.md`（主仓文档由姊妹 change T59 负责）、`openspec/`（本 change 的规格文档除外）。

## 6. 风险与缓解（承自澄清清单，逐条落到任务）

| 风险 | 缓解（任务映射） |
|---|---|
| 市场仓尚未推送上线，桌宠拉取远端会 404 | M2 与 M5 的产物同时进主仓内置源（M6），桌宠在远端不可达时回退内置源（姊妹 change T30/T31）；M8 的 README 写明「填入基址即可切换到远端」 |
| `index.json` 的 `sha256` 与 zip 实际内容不一致（手工维护两处必然漂移） | M6 由打包脚本计算并回写，判据为「回写值等于实算值」；姊妹 change T32 从主仓侧再核一次 |
| 市场条目声明的权限与包内 manifest 不一致（展示的权限与实际申请的不同） | M6 从包内 manifest 读出权限写回条目；判据为「条目 `permissions` 等于包内 manifest 的 `permissions`」 |
| 市场仓的 zip 与主仓内置源副本漂移 | M6 的判据含「两处 zip 的 sha256 相同」 |
| 示例插件依赖用户本机 LLM 配置 | M4 的判据含「宿主未配置大模型时显示提示而非静默失败」；本仓不保存任何密钥 |
| 私有/公开可见性搞错（主仓公开会泄露源码） | M1 的判据含「本仓 `origin` 为 `pet-pc-plugins`（公开）」，主仓 `origin` 为 `pet_PC`（私有）由姊妹 change T1 钉住，两处地址字面不同、不可能互换 |

## 7. 范围外（承规格卡 10 项与清单补充，逐条不做）

不做评分/评论/下载量统计/自动更新/版本回滚；不做插件签名与非对称校验（只 sha256 完整性）；不做多市场/私有源聚合；不做插件间依赖解析；不做 exe 的 Release 附件；**不开 GitHub Pages，不写任何 Pages 工作流**；**不由 agent 执行线上 push**；不做插件的自动构建 CI（本轮由 `tools/package.ps1` 本地产出并提交）；不改动主仓的 `#tab-chat`；不为插件提供任何后端（插件只有 manifest + 前端资源）。
