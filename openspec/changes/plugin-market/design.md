# design.md — plugin-market

> 本文件把规格卡/澄清清单的 `impl_decisions` 收敛为**唯一路径**，不留未定分支。凡本仓（市场仓）与主仓（宿主）之间的契约，一律以姊妹 change `openspec/changes/plugin-system/design.md` 的 §2.1/§3/§5/§10 为准，本文件只规定市场仓**如何产出符合该契约的产物**。
> 任务编号与 tasks.md、§6 依赖顺序一一对应。

## 1. 仓库与目录形态（唯一结构）

```
F:\pet_pc\market\                     ← 独立 git 仓（主仓 .gitignore 忽略 market/）
├── .gitignore                        忽略 dist 之外的临时产物与 OS 噪声
├── README.md                         提交规范 / 打包发版步骤 / 安装位置 / 镜像基址
├── index.json                        市场目录（桌宠拉取的唯一入口）
├── index.html                        人看的静态市场页（本轮不托管）
├── tools/package.ps1                 打包 + 计算 sha256 + 回写 index.json + 复制进主仓内置源
├── plugins/
│   └── com.shixiansi.plugin-chat/
│       ├── manifest.json             插件声明（§3）
│       ├── panel.html                面板视图入口（同时是浮层面入口，见 §3.3）
│       ├── app.js                    插件前端逻辑（桥接、流式渲染、气泡）
│       └── style.css                 插件自己的样式（不使用 box-shadow/filter/transform）
└── dist/
    └── com.shixiansi.plugin-chat-1.0.0.zip   打包产物（含 manifest.json 与上述资源，位于 zip 根）
```

- `dist/` **提交进本仓**（规格卡 Q15 明确要求市场仓内含打包 zip；本仓是公开仓，zip 是分发产物）。
- 主仓 `internal/plugins/sample/` 里放的是**同一份 zip 的副本**与一份 `index.json` 副本（姊妹 change §5.1、T32）。
- 本仓远端 `origin = https://github.com/shixiansi/pet-pc-plugins.git`（**公开**）；默认分支 `main`。**不执行 push**。

## 2. `index.json`（唯一契约投影）

```json
{
  "version": 1,
  "updatedAt": "RFC3339",
  "plugins": [
    {
      "id": "com.shixiansi.plugin-chat",
      "name": "对话插件",
      "version": "1.0.0",
      "author": "shixiansi",
      "description": "在面板里与大模型对话的官方示例插件（验证插件框架）。",
      "permissions": ["llm.chat", "pet.bubble", "pet.surface"],
      "download": "dist/com.shixiansi.plugin-chat-1.0.0.zip",
      "sha256": "<64 位小写十六进制，由 package.ps1 实算回写>"
    }
  ]
}
```

固定规则：

- 每条必填 `id`/`name`/`version`/`author`/`description`/`permissions`/`download`/`sha256` 八个键。
- `netHosts` 只在声明了 `net.fetch` 时必填。**示例插件不声明 `net.fetch`**（联网全由宿主 `llm.chat` 承担，见 §3.2、§7 D6），因此它的条目里**不含** `netHosts` 键，也不含 `surface` 之外的任何可选键。这与姊妹 change design.md §3.1「声明 `net.fetch` 却无 `netHosts` 视为非法」的规则一致（未声明即无需该键）。
- `download` 是**相对路径**（相对基址），因此桌宠以 `base + "/" + download` 取包，基址换成镜像前缀即可整表切换。
- `permissions` 必须与包内 `manifest.json` 的 `permissions` **逐项相等**（由 `package.ps1` 从包内 manifest 读出后写回，禁止手工两处维护）。
- `sha256` 必须是小写 64 位十六进制；由 `package.ps1` 对最终 zip 实算。
- 字段名与取值域以姊妹 change `design.md` §5 的解析与校验为准：多一个键无害，**少一个必填键或取值越域即被桌宠丢弃该条**（并在界面的 `Notice` 里累计说明）。

## 3. 官方示例插件 `com.shixiansi.plugin-chat`

### 3.1 `manifest.json`

```json
{
  "id": "com.shixiansi.plugin-chat",
  "name": "对话插件",
  "version": "1.0.0",
  "author": "shixiansi",
  "description": "在面板里与大模型对话的官方示例插件（验证插件框架）。",
  "permissions": ["llm.chat", "pet.bubble", "pet.surface"],
  "panel": true,
  "surface": [{ "dx": 160, "dy": 10, "w": 300, "h": 260, "radius": 14 }],
  "entry": "panel.html"
}
```

- `permissions` 恰为这三项（§3.2）；声明 `surface` 就必须同时授权 `pet.surface`，否则插件自己打不开浮层面。
- 不声明 `net.fetch`，因此不需要 `netHosts`（§3.2、§7 D6）。
- `entry` 同时用于面板视图与浮层面（§3.3 的 `init.surface.kind` 决定渲染布局）。
- `surface[0]` 的 `dx=160` 正好落在「宠物 150 + 间隙 10」之后，即面板列左侧；`dy=10/w=300/h=260/radius=14` 在窗口 540×520 内，逐块夹回后不会被丢弃（姊妹 change T53/T54 的算式）。

### 3.2 权限集合（唯一取值）

| 权限 | 用途 | 是否声明 |
|---|---|---|
| `llm.chat` | 调用宿主已配置的大模型（流式） | ✅ |
| `pet.bubble` | 让桌宠弹一个气泡 | ✅ |
| `pet.surface` | 显示插件自定义浮层面 | ✅ |
| 其余 6 项 | 不需要 | ❌ |

### 3.3 `panel.html` / `app.js` 的行为（固定顺序）

1. `panel.html` 只做三件事：`<meta charset>`、`<link rel="stylesheet" href="./style.css">`、`<script type="module" src="./app.js"></script>`，并放一个空的挂载点与一个输入区（`textarea` + 发送按钮 + `停止` 按钮 + 状态行 + 回复区）。
2. `app.js` 启动即向 `parent` 发 `{v:1,type:'ready'}`（**握手第一步**，与姊妹 change design.md §9.2 的消息表一致）；收到宿主回投的 `init` 后按 `init.granted` 决定 UI 可用性：缺 `llm.chat` → 输入区 disabled 且状态行显示「未获授权：llm.chat」；缺 `pet.bubble` → 气泡按钮 disabled；缺 `pet.surface` → 浮层面按钮 disabled。**不静默降级**。`init` 之前不发起任何 `request`（`init` 未到不猜权限）。
3. 发送：`{v:1,type:'request',id:<自增>,method:'llm.chat',payload:{text, requestId}}`；收到 `event` 的 `llm.delta` 时把 `text` **追加到同一个文本节点**（沿用主仓 `frontend/src/chat.js` 的流式写法：一个节点活到整条回复结束，避免每帧重建列表）；`llm.done` 时清 `requestId` 与光标；`llm.error` 时把错误文案渲染为一条系统行（含「宿主未配置大模型」这类原因）。
4. 气泡：一个「让桌宠说一句」按钮，发 `{type:'request',method:'bubble.say',payload:{text: <回复的首行>}}`。
5. 浮层面：一个「浮在桌宠旁」按钮，发 `method:'surface.show'`；再点一次发 `surface.hide`。
6. 迟到的 `llm.delta`（`requestId` 不等于当前）一律丢弃——与主仓 `live.js` 对过期 requestId 的处理同一原则。
7. 本插件**不**读写任何文件、**不**发起任何网络请求（联网全由宿主 `llm.chat` 承担），因此它的能力面恰好等于它声明的三项权限。

### 3.4 样式纪律（承主仓硬规矩）

- `style.css` 中**不得**出现 `box-shadow`/`filter`/`text-shadow`/`transform`（这些装饰会画到盒子之外；插件的浮层面与面板视图都活在被裁剪的窗口区域内）。
- 面板视图在 380×520 内**内部滚动**，不撑高任何容器。
- 浮层面内容同样不越出宿主下发的矩形。

## 4. 打包与一致性（`tools/package.ps1`，唯一产出路径）

固定步骤（脚本幂等，可重复运行）：

1. 读 `plugins/com.shixiansi.plugin-chat/manifest.json`，校验 `id`/`version` 存在（脚本不做全套字段校验——那是宿主 `ParseManifest` 的职责；脚本只保证「打出来的包能被宿主接受」这一点由 M3/M4 的判据覆盖）。
2. 校验 `entry` 指向的文件与 `panel.html`/`app.js`/`style.css` 都在插件目录内；把 `<id>-<version>.zip` 先删后建，zip **根目录**放 `manifest.json` 与这些资源（不带外层目录）。
3. `Get-FileHash -Algorithm SHA256` 计算 zip 的 sha256（小写）。
4. 用包内 manifest 的 `permissions` 与元数据**重写** `index.json` 中该条目的字段（`name`/`version`/`author`/`description`/`permissions`/`download`/`sha256`），并更新 `updatedAt`。
5. 把 zip 与 `index.json` 复制到 `F:\pet_pc\internal\plugins\sample\`（同名），使主仓内置源与市场仓产物**同字节**。

脚本**不**执行任何 git 命令、**不**联网、**不**调用 npm/go/wails。

## 5. 静态市场页 `index.html`（本轮不托管）

- 单文件、无外部依赖（无 CDN、无字体外链），内容：插件名、版本、作者、描述、权限清单、下载链接（相对 `dist/...`）与「如何安装」的说明（指向 README）。
- 不配置任何 Pages 工作流；`index.html` 只是仓库内的静态文件，按需通过 `raw`/镜像访问。
- 页面本身**不**执行任何脚本（纯静态 HTML + 内联样式），因此不存在「市场页被插件脚本影响」的可能。

## 6. 任务依赖顺序

```
M1 仓库与远端就绪（跨 change 前置：plugin-system T5 —— 主仓 .gitignore 的 market/ 必须先就位，
   否则主仓会把本仓当嵌套仓跟踪）→ M2（index.json 骨架：字段集与取值域）与 M3（manifest）
M3 manifest（跨 change 前置：plugin-system T10–T15 —— manifest 校验规则先钉死，M3 判据要交给
   主仓 internal/plugins.ParseManifest 解析）→ M2 的 permissions/netHosts 取值必须与之一致（M6 再强制一次）
M3 → M4 插件前端资源（entry 与资源必须先存在）
M4 → M5 打包（zip 根目录含 manifest + 资源）
M5 → M6 实算 sha256 + 回写 index.json + 复制进主仓内置源
M6 → M7 静态市场页（下载链接指向 dist/ 的真实文件名）
M6 → M8 README（权限集合、安装位置、镜像基址、提交规范）
```

- **M2–M6 必须在姊妹 change 的 T31/T32 之前完成**：T31 落盘主仓内置源（`internal/plugins/sample/index.json` 与同名 zip），T32 断言内置源 zip 的实算 sha256 == `sample/index.json` 的 `sha256`，并在 `../market/dist/*.zip` 存在时断言两侧同哈希——这两处判据读的正是本 change M2/M5/M6 的产物。
- M2 的条目内容依赖 M3 的 manifest（权限集合必须相同），故 M2 的前置是 M3；M6 会再强制一次一致性。
- 跨 change 前置汇总：**M1 ← plugin-system T5**（主仓忽略 `market/`）；**M3 ← plugin-system T10–T15**（manifest 字段与校验规则）；**M2–M6 ← plugin-system T31/T32**（内置源落盘与 sha256 断言消费本仓产物）。反向亦有依赖：plugin-system T31 的文件清单含 `internal/plugins/sample/` 的两份产物，由本 change M6 复制过去。

## 7. 收敛的设计决定（不留未定分支）

| # | 决定 | 理由 |
|---|---|---|
| D1 | `dist/` 提交进本仓 | 规格卡 Q15「市场仓内每插件一目录 + 打包 zip」；桌宠需要可直接下载的产物，本轮不做 CI／Release 工作流 |
| D2 | `index.json` 的 `download` 用**相对路径** | 桌宠以「基址 + 相对路径」取包，换镜像或换仓名只需改一处基址（姊妹 change design.md §2.1） |
| D3 | `permissions` 由 `package.ps1` 从包内 manifest 回写 | 消除「展示的权限」与「实际申请的权限」漂移；宿主侧也会拒绝不一致的条目（姊妹 change §3.2 第 5 步） |
| D4 | 示例插件的三类权限为 `llm.chat`/`pet.bubble`/`pet.surface` | 框架验证要求「流式渲染 + 弹气泡」；浮层面是第二种界面落点，不声明就无法验证它 |
| D5 | 浮层面只声明**一块**矩形 | 示例插件只需证明落点可行；多矩形路径由宿主侧纯函数单测与 harness 覆盖（姊妹 change T53/T55、T89/T112） |
| D6 | 插件不保存密钥、不自己联网 | 密钥留在宿主（既有 DPAPI 加密），联网统一走宿主 `llm.chat` 与 `net.fetch` 白名单；插件因此无需 `net.fetch` 权限 |
| D7 | 本仓不做 CI、不做 Release、不开 Pages | 规格卡范围外第 6、7 项 |
