# tasks: 一份自包含的插件开发手册 + 一个可执行的自检工具

## A. 手册内容

- [x] **A1 — 运行模型与"至少一个界面落点"**
  判据：讲清 4 种挂载（surface 每块一个文档 / panel / background / workshop）、它们互不共享内存、
  以及 `panel:true` / 非空 `surface` / `background:true` 三者至少一项（manifest 校验的真实规则）。

- [x] **A2 — 一个能跑的最小插件（四文件全文）**
  判据：`manifest.json` + `panel.html` + `app.js` + `style.css` 各自完整；桥接客户端 40 行可复制；
  示例只声明 `pet.bubble` / `storage` / `pet.surface`。

- [x] **A3 — 本地开发与手工安装**
  判据：数据目录、`plugins/<id>/` 的平铺要求、`plugins.json` 的完整条目、`granted ⊆ permissions`、
  重启后生效、以及"日志页是排错第一站"。

- [x] **A4 — manifest 参考**
  判据：顶层字段表逐项含规则/上限；权限 13 项全表（含 `net.credentials` 是 `net.fetch` 的第二道闸、
  `background` 没有独立权限）；`config` 五种控件、`select` 约束、`secret` 的真实语义。

- [x] **A5 — 桥接协议**
  判据：消息总表（`ready`/`init`/`request`/`response`/`event`/`theme`/`surface`）；`init.surface`
  四种形状逐字段；六条铁律。

- [x] **A6 — 34 个能力方法**
  判据：每一行都有 payload / 返回 / 上限；含易错点：`storage.get` 缺失=空值、
  `storage.move` 的 `targetExists`、`share.get` 未发布=空值+`at`、`net.fetch` 的 `truncated`/
  `encoding:"base64"`/重定向重检、`ws.state` 四值、`llm.chat` 的流式帧与 `kind` 分类与 `tools`、
  `process.state` 的 `known:false`。

- [x] **A7 — 事件**
  判据：`reminder.fired` 的真实触发时机（只有"立刻提醒一次"走这条）；`timer.phaseEnded` 明确写
  "订阅会成功但永远收不到"，并给出番茄钟真正的发布方式；送达三条件。

- [x] **A8 — 几何与主题**
  判据：矩形是声明的（`height:auto` 不成立）、`anchor` 的存在理由、`maxH` 与 `surface.blocks`
  （索引、`sizes` 的字符串键、500 毫秒节流）、`tailX`/`room`、主题两通道、CSS 四条纪律。

- [x] **A9 — 两个宿主的差异与降级**
  判据：服务矩阵；`workshop` 下"藏起桌宠专属、保留只读、写一行说明"的三步；强调 `slot` 而非 `kind`。

- [x] **A10 — 安全边界 / 发布 / 自检 / 故障对照 / 方法总表**
  判据：拿不到的东西逐条；zip 形状与两条硬规则（不多包一层、包内 manifest 与条目逐项一致）；
  版本严格递增与更新保留授权；自检清单四组；故障对照 ≥10 行；方法总表覆盖全部 34 个方法。

- [x] **A11 — 删掉"已退休接口"那一节（用户指出第一版写错了）**
  内容：整节删除；还有用的三处搬到它该在的地方——「这张表就是全部 + 宿主不认识具体插件」
  写进 4.1 节的注、`panel`/`panelSlot`/`rectIndex` 留在字段表与 5.2 节、`llm.chat` 的上下文
  规则写进 6.6 节；头注改成"本文列出的就是全部"。`AGENTS.md §3.3` 同步改成"不许留退休表"。
  判据：全文 `grep` 不到 "退休"/"曾经的写法"/旧名字/旧数字，也没有指向已删内容的交叉引用。

## B. 自检工具（`docs/plugin-testkit/`）

- [x] **B1 — `plugin-check.mjs`：manifest 组**
  判据：与宿主校验逐条对应（id/version 正则、各长度上限、权限子集与去重、`netHosts` 形状与
  "声明 net.fetch 就必须有 hosts"、`net.credentials` 需 `net.fetch`、`shareLLM` 需 `llm.chat`、
  `shareRead` 形状/自我/重复/上限、`config` 五类与 `select` 约束、`panelSlot` 配对、
  `entry` 规则与"至少一个界面落点"、`background` 与三权限配对、`assets`/`surface`/`config` 上限）。

- [x] **B2 — `plugin-check.mjs`：package 组**
  判据：`entry` 文件存在、`assets` 全在、entry 里本地 `src`/`href` 全在（漏文件=装上也白屏）。

- [x] **B3 — `plugin-check.mjs`：source 组**
  判据：`request('x')` 里的每个方法都在契约表里、所需权限都已声明（多余权限给 WARN）；
  禁用 `localStorage`/`sessionStorage`/`indexedDB`/`document.cookie`/`fetch(`/`XMLHttpRequest`/
  `new WebSocket`；CSS 无 `box-shadow|filter|transform|text-shadow`、`:root` 有透明与
  `color-scheme: only light`。

- [x] **B4 — `plugin-check.mjs`：protocol 组（假宿主 + jsdom）**
  判据：三种挂载各跑一遍（`panel/workshop`、`surface#0`、`background`）；断言 `ready` 是 `v:1`、
  `init` 后不崩、`request` 的 `id` 唯一非空且方法已知、全拒绝时不崩、**未授权时该方法的请求
  数必须为 0**、`theme`/`surface`/未知 type/v:2 不崩；有 page error 或 unhandled rejection 记 FAIL；
  没有 jsdom 时记 SKIP 并说明原因（不许静默算过）。

- [x] **B5 — 语料验证：六个官方插件**
  判据：`node plugin-check.mjs <六个插件目录>` 跑完，**4 个全绿**，bilibili 只剩 2 条 WARN
  （`granted=[]` 时仍发请求——先发再优雅处理拒绝是合法写法，所以规则定成 WARN）。

  **同时在两个官方插件里抓到真问题**（这两个不是工具误报，逐条回源确认过）：

  | 插件 | 工具报的 | 回源确认 |
  | --- | --- | --- |
  | chat `1.7.5` | `command-permission: bubble.say 需要 pet.bubble，但 manifest 没有申请` | `app.js:1193` 在浮层输入条的失败路径里调 `bubble.say`，而 manifest 只声明 `llm.chat/pet.surface/plugin.config/storage`。**那次调用必然被拒**，所以「对话失败：…」永远说不出来 |
  | chat `1.7.5` | `css-forbidden: style.css 里有 box-shadow` | `style.css:505` 是 `box-shadow: 0 2px 4px rgba(0,0,0,.5)`（非 inset）。按 `AGENTS.md §3.2`，它在矩形之外绘制 → 要么被区域裁掉（白写），要么在桌面上留一道影 |

  两条都留着让作者决定（加权限还是删调用 / 换成 border），本次不改那个仓的业务代码。

- [x] **B6 — 反向验证：故意写坏的夹具**
  判据：一个临时夹具同时带 18 处毛病（坏 id / 坏版本 / 重复权限 / 未知权限 / 声明 net.fetch 无
  hosts / `panelSlot:todos` 无 surface / `shareRead` 无权限、owner 非法、指向自己 / `config` 的
  secret 与 type 不一致、select 只有 1 个选项 / assets 文件缺失 / entry 引用缺失文件 / 调
  `todos.list` / 调 `bubble.say` 但没申请 `pet.bubble` / 用 `localStorage` / CSS 里有非 inset 的
  `box-shadow`）——工具**逐条报出 17 个 FAIL**，并且协议组把 `localStorage` 那一处**在运行期**
  也抓到了：`localStorage is not available for opaque origins`。
  另有两处**误报被这次反向验证逼出来并修掉**：注释里的 `no localStorage`（已改为先剥注释与
  字符串）、`function request(` 声明被当成动态方法名（已排除声明）。


## C. 同步

- [x] **C1 — README 的「自己写一个插件」**
  内容：指向手册（说明自包含、示例跑过）+ 指向自检工具；补一段"开发时不必经过市场"。

- [x] **C2 — `AGENTS.md §3.3`**
  内容：加一段"对外契约只有一份"：手册 + 工具的方法表；改协议/能力/上限/退休必须两边一起改；
  退休时从文档里删干净，不留退休表。

- [x] **C3 — 手册第 12 节**
  内容：自检清单开头指向工具（"这些里能自动查的，工具都查"），并写清工具**查不到**什么。

- [x] **C4 — README 的「自己写一个插件」**
  内容：在手册链接下面补一段工具链接与一句话说明（一个文件、不需要宿主、查四组）。

- [x] **C5 — 把手册与工具搬进插件仓（用户要求）**
  内容：`docs/plugin-dev.md` 与 `docs/plugin-testkit/` 从本仓移到 `market/docs/`（插件仓是**公开**
  仓，本仓是**私有**仓——契约放在这里，等于只对"已经有宿主源码的人"可见）；本仓根目录的
  `docs/plugin-dev.md` 改成**一句指针**（避免两份副本腐坏）；`market/README.md` 新增 §0「想写
  插件？这两份就是全部契约」并把 `docs/` 写进目录树；本仓 README 与 `AGENTS.md §3.3` 的链接
  改指插件仓（含 GitHub 地址）。
  判据：`market/docs/plugin-testkit/plugin-check.mjs` 从新位置跑得动（jsdom 由插件仓的
  `node_modules` 提供）；本仓 `grep -r "plugin-testkit"` 只剩指针文件与文档说明，没有第二份工具。


## D. 未做

- [ ] 英文版。
- [ ] 工具检查"内容是否溢出声明的矩形"——jsdom 不做布局，只能真机看（README 的 SKIP 项）。
- [ ] 工具直接读宿主仓库的能力表（那会让它只能在宿主仓里跑，违背"不用本地也能检查"）。
- [ ] 把工具接进 `check-all.ps1`（新增一道门禁要动白名单与 BOM 约定；现在它是**给插件作者的
      工具**，不是仓库的门禁。六个官方插件跑一遍是本轮的验证手段，不是常设门）。
