# design.md — voice-plugin

> 技术设计与裁定。上游约束见 `proposal.md` §1.2；执行清单见 `tasks.md`。
> 本文件里每条决定都写明**被否决的备选**，因为这些都是本仓反复踩过的坑。

## 1. 三条硬约束（回顾，带出处）

| # | 约束 | 出处 | 本 change 的应对 |
|---|---|---|---|
| 1 | 事件只投给**已挂载**的 iframe | `frontend/src/plugin-host.js:322` | 新增 `background` 权限 + 常驻帧 |
| 2 | 面板固定 380×520，窗口不 resize | `app.go:599`、`README.md` | 工坊界面重设计为**单栏** |
| 3 | 插件包不含二进制资源 | `market/tools/package.ps1:52,140-145` | 扩 `zipMembers` 支持声明式资源清单 |

## 2. `background`：给既有的「全程在线」一个名字

### 2.1 事实

对话插件的 iframe 从**启动**就挂在 `#chat-slot` 里（`main.js:1805` 调 `refreshChatSlot()`），
关面板只改**窗口区域**，iframe 不销毁 —— `closeSlot()` 只在「没有合格插件」时才被调用
（`main.js:1308-1309`）。所以「插件在没有可见界面时仍运行」**已经成立**，只是无人声明、也无人依赖。

### 2.2 决定

manifest 新增布尔 `background`，并以**权限**形式让用户可见（`background` 加入权限表，
标签「关闭面板后仍在后台运行」）。

- 声明了它且**已授权**的插件，宿主在启动/热更时挂一个**不可见常驻帧**。
- 常驻帧与面板帧**同一个 `PluginFrame` 类**、同一套握手、同一套权限门控 —— 不新造运行方式。
- 只允许**一个**常驻帧（每个插件至多一个），挂在 `#plugin-background` 容器里，`display:none`。

### 2.3 为什么是权限而不是纯声明

纯声明会让任一插件静默获得「后台常驻」这种能力，而用户装插件时看到的是权限清单。
放进权限表 = 用户在安装时**看见并同意**「这个插件会在后台运行」。这是本仓
「缺权限就明说」的一贯做法（`capability.go` 的注释）。

### 2.4 被否决的备选

- **让宿主自己朗读**：那就是现状，用户要的正是把它交出去。
- **让插件在宿主进程里跑 Go 代码**：插件是 HTML/JS，无此机制，且违背沙箱前提。
- **轮询**：插件没有界面就无法计时；而且宿主事件是**已经算出来的**事实，轮询只是把已知变未知。

## 3. `audio.*` 能力

### 3.1 为什么不能复用 `storage`

`storage.set` 的 value 是 **JSON 字符串**（`app_plugins.go:627-646` 直接 `os.WriteFile` 文本）。
300KB 的 WAV 经 base64 再套进 JSON 是 ~400KB 单次 `postMessage`，且 `storage.get` 会把整段
读回宿主内存再回传 —— 为了「播一段音频」传输两遍音频。**存**可以勉强复用，**播**不行。

### 3.2 方法表（新增 5 个，全部要求 `audio` 权限）

| 方法 | 载荷 | 行为 |
|---|---|---|
| `audio.save` | `{name, data}`（data 为 base64） | 写进插件私有目录 `audio/<name>.wav`，返回 `{name, bytes}` |
| `audio.list` | — | 返回 `[{name, bytes}]`，按 name 排序 |
| `audio.remove` | `{name}` | 删一个，不存在不算错 |
| `audio.play` | `{name}` | 播放；**可带 `{loop:false}`**，默认单次 |
| `audio.stop` | — | 停 |

### 3.3 名字校验

复用 `storageKeyPattern`（`^[A-Za-z0-9_-]{1,64}$`）：`name` 会成为文件名，
含分隔符或 `..` 就是逃出私有目录的路径。**与 storage 同一条规则**，不新造。

### 3.4 播放器复用

`audio.play` 经 `desktop.PlayWAVFile` —— **同一个** winmm 封装（`internal/desktop`），
不新写播放器。`internal/desktop` 不动。

### 3.5 被否决的备选

- **`audio.playBytes`（直接播音，不落盘）**：`SND_MEMORY` 要求缓冲区在整个异步播放期间存活，
>  而 Go 的 GC 不知道 —— 约 1% 概率静音或崩溃（`docs/voice.md` §实现要点已记录该结论）。
  必须先落盘再按路径播放，这正是既有 `internal/voice` 的做法。
- **让插件直接读宿主 `voice/user/`**：那是宿主知识，且插件不得访问自身私有目录以外的路径。

## 4. 两个新事件

| 事件名 | 抛出点 | 载荷 |
|---|---|---|
| `timer.phaseEnded` | `app_pomodoro.go` 完成阶段处（既有 `speakPomodoroLine` 的调用点） | `{phase: "focus"\|"break"}` |
| `reminder.fired` | `app_reminder.go:336` 处 | `{due, overdue, total}` |

- 加进 `SubscribableEvents`（`capability.go:99`）。
- 载荷**只含事实**，不含任何文案 —— 说什么由插件决定，这是本 change 的要点。
- 仍经 `pet:plugin-event` 下行，**双引号字面量**（门禁扫描口径，`app_plugins.go:33` 的注释已钉死）。

> **注意 `timer.changed` 不能替代 `timer.phaseEnded`**：前者是「每秒可能变」的状态广播
> （`lastPhase` 去重后才成事件），插件要靠它判断「刚刚结束」就得自己存上一次的状态并做边沿检测 ——
> 把宿主已有的知识推给每个插件各实现一遍。`phaseEnded` 是宿主的**结论**。

## 5. 插件：`com.shixiansi.plugin-voice`

### 5.1 界面挂在**插件市场程序**里（用户裁定，2026-10-14）

原设计把工坊界面放进桌宠面板并压成单栏。用户裁定改为：**界面挂在插件市场程序中该插件的设置区**。

这条裁定**取消了约束 2**，而且明显更好：

- 市场是 **980×700、可缩放、带边框**的普通窗口（`tools/plugin-market/main.go:48-58`），
  所以工坊**原来的两栏布局（左栏角色 260px + 右栏台词）可以原样保留**，
  不必为 380×520 重设计。
- 面板那个矩形根本没有空间同时放「角色列表」和「逐条台词的工作面」——
  这正是当初把工坊拆成独立程序的理由（`docs/voice-studio.md:9`）。
  把界面放进市场，等于**工坊退役而工作面保留**，而不是把工作面砍小。

因此 `design.md` 的其余部分（单栏、折叠版本历史）**作废**，改按第 9 节执行。

### 5.2 台词与音色从哪来

- 台词文本写在插件自己的 `panel.html`/`app.js` 里（不再是宿主的 `voice.Texts`）。
- 音色库与渲染结果存在**插件自己的私有目录**（`audio/` 存 WAV，`storage` 记采用哪一版）。
- 生成经 `net.fetch` 调用户**已运行**的 GPT-SoVITS 服务；插件配置里有三个权重路径与采样参数。

### 5.3 **引擎启动不由插件负责**（用户裁定）

插件没有、也不会获得启动进程的能力。界面在服务未运行时显示
「语音服务未运行」并给出启动命令，由用户自己启动。
理由：新增「启动进程」等于把任意代码执行交给任何已安装插件，
而这个收益（省一次手动启动）远小于代价。

### 5.4 配置项（manifest `config`）

`ttsBaseUrl`（`hostWhitelist: true`，默认 `http://127.0.0.1:9880`）、
`gptPath`/`sovitsPath`/`refPath`、`promptText`、
`speed`/`temperature`/`topK`/`topP`/`repetitionPenalty`/`seed`。
本地引擎无鉴权，故无 secret 项。

## 6. 落地：市场程序托管插件界面（方案 B）

### 6.1 为什么市场需要这套能力

`tools/plugin-market/` 目前**只有** `assets` 嵌入，**没有**插件资源路由、
**没有** `PluginRequest` 调度。插件界面要在市场里跑，市场必须有：

1. **资源路由**：`/plugin-asset/<id>/<path>` —— 与桌宠的 `servePluginAsset` 同一套语义
   （同一个 `ValidRelPath`、同一个按扩展名给 MIME、同样拒绝逃逸）。
2. **能力调度**：`PluginRequest(id, method, payload)` —— 同一张方法表、同一套权限门控。

### 6.2 做法：抽到 `internal/plugins`，两程序共用（零复制）

用户裁定方案 B。**不复制**，把两处能力抽成共享实现：

| 抽出的东西 | 从哪来 | 归属 |
|---|---|---|
| 方法表与权限门控（`Dispatch`） | `app_plugins.go` 的 `pluginMethods` + `PluginRequest` | `internal/plugins/dispatch.go` |
| 插件资源解析（路径校验 + MIME） | `app_plugins_asset.go` | `internal/plugins/asset.go` |
| 私有目录存取、`audio.*` 的文件部分 | `app_plugins.go` 的 storage/audio | `internal/plugins/private.go` |

桌面专属的能力（`todos.*`、`timer.*`、`bubble.say`、`surface.*`、`llm.chat`）
**留在桌宠侧**：它们操作的是桌宠的窗口、番茄钟与待办，市场程序里没有这些东西。
共享层提供**能力注册**：每个程序把自己实现得了的方法注册进去，调度器只认注册过的。

> 这一点是本节的要点，也是方案 B 唯一需要裁定的地方：
> **"共享调度"不等于"两边能调一样的东西"**。市场程序里的插件界面调
> `todos.list` 会得到「本程序不支持该方法」，因为市场没有待办列表——
> 这不是缺陷，而是"能力由宿主程序决定"的正确表现。

### 6.3 安全边界不变

- 同一个 `sandbox="allow-scripts"`，同一个 `PluginRequest` 门控，同一张权限表。
- 市场侧的资源路由**必须**复用 `ValidRelPath` 与 `PrivateDir(id)`，
  不得自己拼路径——这是唯一的穿越防线，两处各写一遍必然漂移。

## 7. 打包

`package.ps1` 的 `zipMembers` 现在只有 manifest + 三个固定资源 + entry。新增可选
**`assets` 声明**：manifest 里列出的相对路径一并打进包，使插件能自带内置音频。

- 只允许**相对路径**，复用 `plugins.ValidRelPath`（与安装解压同一校验，`manager.go:678`）。
- 体积上限沿用既有 `maxEntrySize`/`maxTotalSize`（32 MiB / 128 MiB），不新增常量。
- **安装侧不用改**：`extractZip` 本来就按 zip 内容解压任意路径。

## 7. 退役清单与数据处置

删除：`internal/voice/`、`internal/tts/`、`app_voice.go`、`app_voice_studio*.go`、
`app_voice_*_test.go`、`tools/voice-studio/`、`tools/voice-fixed/`、设置页语音区、
`Config` 的 `VoiceEnabled`/`VoicePomodoro`/`VoiceReminder`、
`app.go` 的 `initVoice`/`initVoiceStudio` 与 `vstudio` 状态、`StudioPath`/`OpenVoiceStudio` 绑定。

**数据处置：一次性迁移**（用户裁定 2026-10-14）。

`<dataDir>/voice/user/` 里的东西搬进插件私有目录，而不是丢下：

| 旧位置 | 新位置 | 说明 |
|---|---|---|
| `voice/user/voice-user.json` | `plugin-data/com.shixiansi.plugin-voice/library.json` | 角色表、每条的 takes 与 chosen |
| `voice/user/clips/<line>/vNNN.wav` | `plugin-data/com.shixiansi.plugin-voice/audio/<line>-vNNN.wav` | 12 个 WAV |

**照着 `internal/plugins/migrate.go` 的既有先例做**，它已经为对话插件解决过同一个问题
（把宿主的 `llm*` 设置与三个数据文件搬进插件私有目录），三条性质直接沿用：

1. **只读源，绝不改** —— 旧文件原地保留，这就是回滚路径；
2. **先全部写好再改名/写标记** —— 中途失败则下次启动重试，不会留下半个库；
3. **插件私有目录里放一个标记文件** —— 它的存在就是「搬过了」，源文件不再被读。

**为什么不像对话插件那样"只读不搬"**：那次的目标文件是配置与历史，插件重填一遍成本很低；
这次是**12 段已经渲染好的音频**，重建要几十分钟 CPU 与一个运行中的 GPT-SoVITS 服务，
用户刚刚才把宵宫调好。不搬等于让那些工作作废，所以搬。

**迁移的归属**：迁移代码放在**桌宠侧**（它有数据目录与 `plugins.Manager`），
和 `migrateChatData` 并列，在启动时执行一次。市场程序不参与——
它可能在桌宠从未运行的机器上被打开，而那时源数据同样在场；
但把同一条迁移写两遍会让"搬没搬过"有两个答案，所以**只有桌宠做**。

## 8. 收敛的裁定

| 问题 | 裁定 | 理由 |
|---|---|---|
| 谁决定「说什么台词」 | 插件 | 用户要求；宿主只抛事实 |
| `background` 是权限还是纯声明 | **复用 `events` 权限**，不新增 | `TestCapabilityTableIsComplete` 要求每个权限都被方法使用；一个没有方法的后台权限会让用户勾一个什么都不做的开关 |
| 音频要不要落盘 | **要** | `SND_MEMORY` + GC 的 1% 崩溃是已记录的坑 |
| `audio` 与 `storage` 是否合一 | **分开** | storage 是文本 JSON；音频是二进制，混用会传两遍 |
| 工坊界面 | **挂插件市场程序，两栏保留** | 用户裁定；980×700 放得下，面板 380×520 放不下 |
| 引擎启动 | 用户手动 | 插件无进程能力，且不新增 |
| 内置 5 条音频 | **随插件包分发**（`assets`） | 否则插件装好后一声不响，体验断崖 |
| `voice/user/` 旧数据 | **一次性迁移进插件** | 12 段已渲染音频，重建代价高；照 `migrate.go` 先例 |
| 共享调度层的边界 | 每个程序**注册自己实现得了的**能力 | 市场没有待办/番茄钟，共享的是「调度」，不是「能力集合」 |
| 迁移代码放哪 | **只在桌宠侧** | 同一迁移写两遍会让「搬没搬过」有两个答案 |
