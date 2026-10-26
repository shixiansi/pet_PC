# design: 控制中心 / 数据所有权 / Svelte 结构

## 一、程序形状：把插件市场长成控制中心

**决定：扩展现有的 `tools/plugin-market`，不新建第三个程序。**

它已经是一个独立 Wails 程序，有自己的 module、构建链、frontend、门禁，还已经在宿主
插件界面（插件的工坊就挂在它的窗口里）。控制中心需要的正是这些。新建第三个程序会
带来第三条构建链、第三个 CI job、第三套 gates 和第三份"从哪找 exe"的逻辑，而收益为零。

它今天的名字（`PetPCPluginMarket.exe`）会名不副实，所以：

- exe 改名为 `PetPCControlCenter.exe`，桌宠的"打开插件市场"按钮文案改成"设置"；
- 旧 exe 名保留一个版本的重定向（`check-all.ps1` 已经在处理"把市场 exe 拷到 build\bin"
  这件事，改名要同步这里和 `tools/build-all.ps1`）；
- **这是本方案唯一一处需要动构建脚本的地方**，也是唯一会让"点按钮没反应"的风险点。

## 二、数据所有权（本设计的核心）

现状的事实（已核实）：`Store` 把 `cfg`/`todos`/`pomodoro`/`records`/`chat` **全部缓存在
内存**，`Config()` 返回的是内存副本。所以「控制中心改设置 → 运行中的桌宠立刻看到」
并不成立，而且更糟：桌宠会拿内存里的旧配置把控制中心的改动**覆盖掉**。

双写同一个文件是这一整块唯一真正的危险，所以按文件划清写者：

| 文件 | 唯一写者 | 另一方 |
| --- | --- | --- |
| `settings.json`（新） | **控制中心** | 桌宠只读 + 热重载 |
| `config.json`（收窄） | **桌宠**（位置、大小、窗口吸附等运行时状态） | 控制中心不碰 |
| `plugins.json`、`plugins/<id>/`、`plugin-data/<id>/` | **控制中心**（现状就是市场在写） | 桌宠只读（每次调用都重读，本来就不缓存） |
| `todos.json`、`pomodoro.json`、`pomodoro-records.json`、`llm-history.json`、`llm-usage.json` | **桌宠** | 控制中心第一阶段只读 |
| `debug.log` | 桌宠 | 控制中心可读（诊断页） |

### 为什么拆成两个文件，而不是一个文件加锁

`Config` 这个结构体不动（它的 19 个键散落在上百处调用里，改名是纯赔钱），但**持久化
分成两个文件**：控制中心写的偏好项进 `settings.json`，桌宠写的运行时状态留
`config.json`。`Store.Config()` 把两份在内存里合并，`store` 在首次加载时做一次迁移
（把老 `config.json` 里的偏好键搬进 `settings.json`，原文件只留运行时键）。

这样一个文件只有一个写者，**不存在"谁覆盖谁"这类问题**——不需要锁、不需要版本号、
不需要"改完要重启"。加锁方案（两边都 read-modify-write）永远留着一个几十毫秒的丢失
窗口，而这类 bug 的表现是"我明明改了，它没生效"，正是这次要消灭的那类观感。

### 热重载怎么落到桌宠

`Store.Config()` 里加一次 mtime 检查（节流 500ms）：`settings.json` 变了就重读并合并。
一个入口，所有调用点自动受益（番茄钟 tick、提醒调度、每次绑定调用）。一次 `stat`
约 5µs，每日请求量下可以忽略。

**不做文件监视**：`fsnotify` 是一整个依赖，而 500ms 的延迟对一个设置项完全够用。

## 三、信息架构

侧栏六个页面，参考 Coopanion 的"常规/高级"分法——但只分两级，不做它的十一页：

| 页面 | 内容 | 数据 |
| --- | --- | --- |
| 开始 | 桌宠大小与位置、开机启动、快捷键、暂停/恢复、运行状态 | settings.json 写 + 读 |
| 待办 | 完整列表、搜索、批量勾选（**第一阶段只读**，见下） | todos.json 读 |
| 专注 | 番茄钟时长、长休息间隔、自动接续、专注前计划开关 | settings.json 写 |
| 记录 | 已完成项、番茄记录、按天统计 | 只读 |
| 插件 | 已装列表、权限勾选、插件的工坊（现在市场里那套搬过来） | plugins.json 写 |
| 高级 | 模型与密钥、数据目录、诊断导出、日志尾 | settings.json 写 |

**待办页第一阶段只读**，理由写在上面那张表里：`todos.json` 的写者是桌宠。第二阶段
再决定是给它写权限（需要和桌宠的 reload 协议一起设计），还是投递到桌宠面板里操作。
先交付一个只读的、能搜索、能看清 13 条里第 13 条是什么的页面，也比现在"面板里滚不到底"
强，而且零风险。

## 四、Svelte 结构

```
tools/plugin-market/frontend/        （改名后就是控制中心）
  src/
    main.ts
    App.svelte
    pages/        Start / Todos / Focus / Records / Plugins / Advanced
    components/   Button, Card, ListRow, Switch, Field, Dialog, EmptyState, Toast
    lib/          bindings.ts（wailsjs 的薄封装）, format.ts, theme.ts
```

**共享层**：`frontend/ui/` 放 `tokens.css` + 上面那套 `components/`，两个前端都用
Vite 的 `resolve.alias` 指过去（`fs.allow` 打开仓库根）。不建 npm workspace——这个
仓库没有 pnpm，两个前端各自 `npm ci`，加一个 workspace 会牵动两套 lockfile 和 CI。

**token 从哪来**：现有两份 `style.css` 里已经有成型的取值（面板圆角、玻璃感、深色
配色）。重构的动作是把这些**抽出来**再统一，不是重新发明一套——用户已经接受了现在的
观感，问题在结构不在颜色。

**桌宠前端（`frontend/`）第二阶段再迁**。它小、耦合窗口几何，而且 4 道像素门禁钉在
它上面。控制中心是全新代码，先拿它验证 Svelte + token + 组件这套东西是可行的。

## 五、门禁怎么跟着演进（最容易被低估的成本）

`.dev/check-frontend.cjs` 的 169 项断言里，一大半是**钉在旧 DOM 上**的：元素 id 存在
于 `index.html`、`main.js` 里调用了这个 id、CSS 里没有 `box-shadow` 等等。

Svelte 编译产物里没有那些 id，所以重构期这些断言会成片变红。必须**同时**做分诊，
否则会陷入"关掉门禁让它绿"的滑坡：

| 断言类别 | 处理 |
| --- | --- |
| 绑定名对齐（"假桥接名单 == Go 的导出集合"） | **保留并强化**：改成从 `wailsjs/go/main/App.d.ts` 生成期望集合，新前端也用它 |
| 权限词表 / 市场解释每一项权限 | **保留**：与界面结构无关 |
| 窗口区域规则（无 `box-shadow`/`filter`） | **保留**：这是裁剪窗口的硬约束，Svelte 一样要守 |
| 元素 id 与 `main.js` 调用点对齐 | **重写**为组件测试（计划引入 vitest + @testing-library/svelte），断言"点了这个按钮会调用那个绑定" |
| 具体 DOM 层级/文本 | **删除**，或下沉到组件测试 |

四道像素门禁（`verify-region` / `verify-live` / `verify-plugin-surface` /
`verify-plugin-market`）只看真实窗口的裁剪与几何，**不受前端框架影响**，原样保留——
它们是这次重构最可靠的网。

## 六、迁移顺序（每阶段可独立验收）

1. **阶段 1｜地基**：`settings.json` 拆分 + 迁移 + `Store.Config()` 热重载 + 桌宠不再写
   偏好键。**验收**：手工改 `settings.json`，运行中的桌宠在 1 秒内改变行为（例如番茄
   时长）。
2. **阶段 2｜控制中心骨架**：市场程序改名 + Svelte 6 页骨架 + token + 组件；先只接
   "读"和"开始页的写"。**验收**：侧栏六页能切换，开始页改桌宠大小，桌宠立刻变。
3. **阶段 3｜搬设置**：把桌宠面板里的设置/记录/插件 tab 删掉，功能搬进控制中心。
   **验收**：面板只剩待办与番茄，`check-all.ps1` 全绿。
4. **阶段 4｜桌宠前端迁 Svelte** + 活动气泡。**验收**：三道像素门禁仍绿。
5. **阶段 5｜形象层**（`figure.json` + 分件动画），另立提案。

## 七、风险

| 风险 | 对策 |
| --- | --- |
| 改名 exe 打断"打开插件市场"按钮 | 阶段 2 的验收项里显式包含它；`build-all.ps1` 与 `check-all.ps1` 同步改，旧名留一个版本 |
| 169 项断言成片变红，导致"先关掉再说" | 第五节的分诊表**先做**再动前端；绑定名对齐那条改成从 `App.d.ts` 生成，不手工维护 |
| `settings.json` 迁移把用户配置弄丢 | 迁移是**复制**不是搬移：老 `config.json` 原样保留一个版本，读失败时回退到它 |
| Svelte 5 是新版本，生态资料少 | 只用 runes + 内置 transition，不引路由库、不引 UI 库；组件自己写，一共 8 个 |
| 双写 `todos.json` | 第一阶段不做，写权限后置 |
