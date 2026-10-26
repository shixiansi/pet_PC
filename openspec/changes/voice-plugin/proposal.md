# proposal.md — voice-plugin

> 本 change 把**语音提醒**整体插件化：宿主删掉内置语音（`internal/voice`、`internal/tts`、`app_voice*.go`、
> 设置页语音区、独立程序 `tools/voice-studio`），改为只提供两条**通用机制** —— ① `audio.*` 能力（插件把
> 音频存进自己的私有目录并请求播放）② `timer.phaseEnded` / `reminder.fired` 事件 —— 以及一条让插件能在
> **没有界面时也活着**的 `background` 声明。台词、音色、开关、生成界面全部归新插件
> `com.shixiansi.plugin-voice`。
>
> 本文件只写「为什么、改变什么、影响哪些文件、遵守哪些上游协议」；技术细节在 `design.md`，执行清单在 `tasks.md`。
> 与上一链的一致做法见 `openspec/changes/plugin-market-app-and-chat-plugin/proposal.md`。

## 1. 变更说明（做什么、为什么）

### 1.1 现状（本仓实读事实）

| 事实 | 出处 |
|---|---|
| 语音是**宿主整族**：内嵌 5 条台词 WAV + 用户语音库解析 | `internal/voice/voice.go`、`internal/voice/user.go`、`internal/voice/play.go` |
| 合成客户端与引擎启动 | `internal/tts/client.go`、`internal/tts/engine.go` |
| 宿主绑定：开关、试听、停止、打开工坊、语音视图 | `app_voice.go`、`app_voice_studio.go` |
| 设置页语音区：三开关 + 试听/停止 + 音色显示 + 打开工坊 | `frontend/index.html`、`frontend/src/main.js:2149-2270` |
| 配置字段 `voiceEnabled`/`voicePomodoro`/`voiceReminder` | `internal/store/store.go:196-209`、`:336-338` |
| 第三个独立程序：配音工坊（两栏工作面，左栏角色 260px + 右栏台词） | `tools/voice-studio/`、`docs/voice-studio.md`、`style.css:183` |
| 播放发生在番茄阶段结束与提醒触发两处 | `app_pomodoro.go:385,393`、`app_reminder.go:336` |
| 插件能力 = 10 项固定权限 + 方法表 | `internal/plugins/capability.go`、`app_plugins.go:280-298` |
| 插件事件下行唯一通道 `pet:plugin-event`（**必须双引号字面量**） | `app_plugins.go:33,1065-1070` |
| 可订阅事件仅 `timer.changed` / `todos.changed` | `internal/plugins/capability.go:99` |
| 插件包只打包 manifest + `panel.html`/`app.js`/`style.css` + entry | `market/tools/package.ps1:52,140-145` |
| 插件私有目录支持任意文件写入（`storage.set/get/remove`） | `app_plugins.go:626-694` |
| 插件资源路由按扩展名给 MIME（`.wav` 由系统 MIME 表覆盖） | `app_plugins_asset.go:94` |

### 1.2 本 change 面对的硬约束【决定形态】

**约束 1 — 事件只投给「已挂载的 iframe」。** `frontend/src/plugin-host.js:322` 明确写在事件无人可投时
`plugin event for <id> dropped: no interface on screen`，`routeEvent` 只遍历 `surfaceFrames` 与 `slotFrame`。
而语音要响的两个时刻（番茄到点、提醒触发）恰恰是**用户没打开面板**的时候。

**先例让这条约束可解**：面板槽位的 iframe 在启动时（`main.js:1805` 的 `refreshChatSlot()`）就挂上，
**关面板只是把窗口区域裁掉，iframe 继续运行** —— `closeSlot()` 只在「没有合格插件」时被调用
（`main.js:1308-1309`）。也就是说「插件全程在线」这件事在对话插件上**已经成立**，只是没有名字。

本 change 把它显式化为 manifest 的 `background: true`：声明它的插件由宿主挂一个**常驻帧**，
从而在没有可见界面时也能收事件。这不是新造一套运行方式，而是给既有事实一个受声明约束的名字。

**约束 2 — 面板是固定 380×520。** 窗口区域按它裁剪，不能长大（`README.md` 多处）。
配音工坊现在是 920×660 两栏工作面，**塞不进**。用户已确认工坊并入插件，因此其界面必须**重设计为单栏**。

**约束 3 — 插件包不携带二进制资源。** `package.ps1` 的 `zipMembers` 只有 4 个固定文件 + entry。
音频要随包分发就得扩这个清单。

### 1.3 交付

**【一】宿主侧：两条通用机制 + 一条声明**

- **`audio.*` 能力**（新权限 `audio`）：把 WAV 存进插件私有目录 / 列出 / 删除 / 播放 / 停止。
  宿主**不知道**什么是台词、什么是音色 —— 它只会「把这段字节存起来」和「播这个文件」。
- **`timer.phaseEnded` / `reminder.fired` 事件**：在宿主**已经**计算出的那两个时刻抛出，
  载荷只含事实（结束了哪个阶段、到期/逾期条数），不含任何文案。
- **`background` 声明**：manifest 布尔字段；为真时宿主挂常驻帧，使插件无界面也能收事件。

**【二】插件侧：`com.shixiansi.plugin-voice`**

- 面板槽位承载**单栏**工作界面：音色选择 + 每条台词一行（试听 / 生成 / 采用）+ 三个开关。
  单栏是约束 2 的直接结果，见 `design.md` 的界面章节。
- 插件自带台词文本与开关；音色库与渲染结果存在**插件自己的私有目录**。
- 生成走 `net.fetch` 调本地 GPT-SoVITS 服务（工坊原来的做法），或由插件自己的配置决定。

**【三】退役**

- 删除 `internal/voice`、`internal/tts`、`app_voice.go`、`app_voice_studio.go`、`tools/voice-studio/`、
  设置页语音区与 `voice*` 配置字段。
- 既有用户数据（`voice/user/`）**保留在磁盘上不删**，并在迁移时告知位置；迁移本身**不做猜测性搬运**。

## 2. 影响面（文件级）

| 层 | 文件 |
|---|---|
| 能力与事件 | `internal/plugins/capability.go`、`app_plugins.go` |
| 声明 | `internal/plugins/manifest.go`（`background` 字段 + 校验） |
| 常驻帧 | `frontend/src/plugin-host.js`、`frontend/src/main.js`、`frontend/index.html` |
| 退役 | `internal/voice/*`、`internal/tts/*`、`app_voice*.go`、`tools/voice-studio/*`、`frontend/src/main.js` 语音区、`internal/store/store.go` |
| 打包 | `market/tools/package.ps1`（允许包内音频资源） |
| 插件 | `market/plugins/com.shixiansi.plugin-voice/*` |
| 门禁与测试 | `.dev/check-frontend.cjs`、`.dev/run-frontend-harness.cjs`、`.dev/verify-live.ps1`、各 `*_test.go` |
| 文档 | `README.md`、`docs/voice.md`、`docs/voice-studio.md`、`docs/genshin-tts.md` |

## 3. 不做的事

- **不**把语音做成宿主「可配置的若干音频槽位」——那仍是宿主懂语音，只是换了存储位置。
- **不**让插件直接读宿主 `config.json` 或用户的绝对路径 —— 插件只经自己的能力访问自己的私有目录。
- **不**保留工坊独立程序：用户已确认并入插件（`voice-plugin` 的问答记录）。
- **不**在本 change 里改变番茄钟/提醒**何时**触发；只改变「谁来说、说什么」。
