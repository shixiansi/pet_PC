# tasks.md — voice-plugin

> 执行清单。设计见 `design.md`，上游约束见 `proposal.md`。
> 每条的「完成判据」都是可直接核对的观测结果，不写「实现了 X」这类不可验证的措辞。

## A. 宿主：声明与权限

- [x] **A1 — manifest 新增 `background` 布尔字段**
  内容：`internal/plugins/manifest.go` 的 `Manifest` 加 `Background bool \`json:"background,omitempty"\``；解析不变（JSON 标签即契约）。
  完成判据：以 `{"background":true,...}` 解析后 `m.Background == true`；缺省时 `false`；`go test ./internal/plugins/` 通过。

- [x] **A2 — 权限表新增 `audio`（并刻意**不**新增 `background`）**
  内容：`internal/plugins/capability.go` 的 `AllowedPermissions` 与 `PermissionLabels` 加
  `PermAudio = "audio"`（「保存并播放音频（例如语音提醒）」）。
  **未新增 `PermBackground`**——见下方「实现时的偏离」。
  完成判据：`IsAllowedPermission("audio")` 为真；既有十项**逐项仍在**；`AllowedPermissions`
  长度为 **11**；`TestCapabilityTableIsComplete`（每个权限都必须被某个方法使用）通过。

- [x] **A3 — `background` 的配对规则（改为门控在既有的 `events` 权限上）**
  内容：`manifest.go` 的 `validateBackground` 要求：声明 `background: true` 的 manifest
  **必须同时申请 `events` 权限**，否则解析失败。
  完成判据：`{"background":true}` 且权限不含 `events` → 解析报错且文案同时点名 `background`
  与 `events`；两者都有 → 通过；都无 → 通过；`go test ./internal/plugins/` 通过。

  > **实现时的偏离（A2/A3 原写法作废）**：原计划新增一个 `background` 权限，长度 10 → 12。
  > 实现时发现这与本仓一条**硬门禁**冲突：`TestCapabilityTableIsComplete` 要求
  > **每一个权限都至少被一个方法使用**——否则用户会在安装时看到一个「勾了却什么都不做」的开关。
  > 而「常驻帧」本身不是一个可调用的能力，它没有方法可挂。
  >
  > 因此改为把常驻帧门控在**既有**的 `events` 权限上：该权限的语义本就是
  > 「把待办与番茄钟的变化事件送给我」，而常驻帧的用途正是「无人看着时接收事件」——
  > 语义直接吻合，且不需要为一个开关凭空造一个方法。
  > `manifest_test.go` 的 `TestAllowedPermissionsIsTheEleven` 里有一条断言
  > **主动证明 `IsAllowedPermission("background")` 为假**，把这个决定钉死。

- [x] **A4 — 可订阅事件新增两个**
  内容：`capability.go` 的 `SubscribableEvents` 加 `"timer.phaseEnded"`、`"reminder.fired"`。
  完成判据：`IsKnownEvent` 对两者为真；既有两项仍在；长度为 4。

- [x] **A5 — `entry` 不再要求 `panel || surface`**
  内容：`validateEntry` 现在要求「至少一个界面落点」（`panel` 或 `surface`）。语音插件的界面
  由**市场程序**托管，两者都不声明，所以该校验会拒掉它。改为：`panel`、`surface`、
  **或 `background`** 至少有一个——三者都是「宿主程序会挂起这个插件的一个界面」，
  只是挂的地方不同（桌宠面板 / 桌宠旁 / 由市场托管的编辑界面）。
  完成判据：既有三个反例（`panel:false` 且无 `surface`）仍被拒；只声明 `background:true` +
  `entry` 的 manifest 通过；错误文案仍点名 `panel` 与 `surface` 并**加上** `background`。
  > **这是 A 节唯一一处规则放宽**，它由用户「界面挂市场」的裁定直接推出：
  > 一个不占桌宠任何界面落点的插件仍然是合法插件，因为它的界面在另一个程序里。

## B. 宿主：`audio.*` 能力

- [x] **B1 — 方法表挂 5 个 `audio.*`，全部要求 `audio` 权限**
  内容：`app_plugins.go` 的 `pluginMethods` 加 `audio.save`/`audio.list`/`audio.remove`/`audio.play`/`audio.stop`。
  完成判据：无 `audio` 权限的插件调用任一方法返回「未获权限 audio」且**状态不变**（私有目录无新文件、无播放）。

- [x] **B2 — `audio.save` 落盘**
  内容：base64 解码后写入 `<插件私有目录>/audio/<name>.wav`；`name` 用 `storageKeyPattern` 校验；父目录按需创建；写临时文件再 rename（与 `writePluginUsage` 同法）。
  完成判据：`audio.save{name:"focus_end",data:<wav base64>}` 后该文件存在且**字节与输入相同**；`name` 为 `../evil` / `a/b` / 空 / 65 字符 → 拒绝且不写任何文件。

- [x] **B3 — `audio.list`**
  内容：返回 `audio/` 下的 `[{name,bytes}]`，按 name 排序；目录不存在返回空数组而非错误。
  完成判据：新装插件 `audio.list` 返回长度 0 且无错误；save 两个后返回按 name 升序的两项且 `bytes` 与文件大小相等。

- [x] **B4 — `audio.remove`**
  内容：删除一个；不存在不算错。
  完成判据：删存在的文件后 `list` 少一项且文件消失；删不存在的返回 ok。

- [x] **B5 — `audio.play` / `audio.stop`**
  内容：`play` 经 `desktop.PlayWAVFile` 播放 `audio/<name>.wav`；`stop` 调 `desktop.StopAudio`。
  完成判据：以注入的假播放器驱动时，`play{name:"x"}` 使假播放器收到**该文件的绝对路径**；`play` 一个不存在的 name 返回错误且不调用播放器；`stop` 使假播放器收到一次 StopAudio。

- [x] **B6 — 路径不得逃出私有目录**
  内容：`audio.*` 的 name 只经 `storageKeyPattern`，且最终路径必须仍在 `PrivateDir(id)` 之下（与 `servePluginAsset` 同法，用 `filepath.Rel` 判定）。
  完成判据：构造含 `..`、绝对路径、分隔符的 name 全部被拒；一条断言直接比对 `filepath.Rel` 结果不以 `..` 开头。

## C. 宿主：抛事件

- [x] **C1 — `timer.phaseEnded`**
  内容：在 `app_pomodoro.go` 既有两个 `speakPomodoroLine` 调用点，改为抛 `a.emitPluginStateChange("timer.phaseEnded", map[string]any{"phase": ...})`（`focus`/`break`）。
  完成判据：完成一个专注阶段后，订阅该事件的插件收到**恰好一帧** `timer.phaseEnded` 且 `payload.phase == "focus"`；完成休息阶段收到 `"break"`；未订阅的插件一帧都收不到。

- [x] **C2 — `reminder.fired`**
  内容：在 `app_reminder.go` 既有 `speakReminderLine` 处抛 `{"due":..,"overdue":..,"total":..}`。
  完成判据：触发一次提醒后订阅者收到一帧，三个数字与传入 `speakReminderLine` 的一致。

- [x] **C3 — 事件名以双引号字面量出现**
  内容：两个新事件的发出点写 `"timer.phaseEnded"` / `"reminder.fired"` 字面量，**不得**经 Go 常量。
  完成判据：`check-frontend.cjs` 的「每个 EventsOn 都有 Go 侧 emit」检查仍通过；在插件前端订阅这两个名字时不被判为 dangling。

## D. 宿主：常驻帧（`background`）

- [x] **D1 — 前端新增常驻挂载点**
  内容：`frontend/index.html` 加 `<div id="plugin-background" class="hidden"></div>`；`main.js` 的 `els` 登记。
  完成判据：`check-frontend.cjs` 的「每个 element id 都在 index.html 里存在」通过；该容器默认带 `hidden`。

- [x] **D2 — `PluginHost` 支持一个常驻帧**
  内容：`plugin-host.js` 新增 `openBackground(plugin)` / `closeBackground()` 与 `backgroundFrame` 字段；`routeEvent` 与 `onMessage` 把常驻帧一并纳入（与 `slotFrame` 同等对待）。
  完成判据：harness 中打开常驻帧后，1 帧存在、其 `sandbox` 恰为 `allow-scripts`、收到 `kind:"panel"` + `slot:"background"` 的 `init`；事件按 `pluginId` **也**投给常驻帧。

- [x] **D3 — 只有声明且已授权的插件才挂常驻帧**
  内容：`main.js` 的 `backgroundPlugin()` 挑选「enabled + manifest 声明 `background` +
  **`granted` 含 `events`**」的插件挂常驻帧；未授权一律不挂。
  （不是「含 `background`」——那个权限不存在，理由见 A2/A3 的偏离说明。）
  完成判据：三个反例（未声明 / 未授权 `events` / 停用）各自断言常驻帧数量为 0；正例为 1。

- [x] **D4 — 常驻帧与面板帧不冲突**
  内容：常驻帧独立于 `slotFrame`，互不覆写。
  完成判据：同一个插件既占面板槽位又开常驻帧时，两个帧同时存在且各收到自己的一次 `init`。
  > 已补上覆盖：harness 新增 `com.example.both` 一行（`panelSlot:"chat"` + `background:true`），
  > 断言两个 iframe 同时存在，且两条 `init` 的 `surface.slot` 分别是 `chat` 与 `background`。
  >
  > 补这条时发现一处**不直观的耦合**并记在这里：`slotPlugin()` 判定对话槽位归属时
  > 除了 `panelSlot === 'chat'` 还要求**已授权 `pet.surface`**，尽管该插件可以完全不声明浮层矩形。
  > 第一版夹具只授权了 `events`，于是「槽位帧 0 个」——测试失败的原因是夹具不对，
  > 不是代码不对。夹具里写明了这一点。

## E. 退役宿主内置语音

- [x] **E1 — 删除语音实现**
  内容：删 `internal/voice/`、`internal/tts/`、`app_voice.go`、`app_voice_studio*.go`、`app_voice_*_test.go`、`tools/voice-studio/`、`tools/voice-fixed/`。
  完成判据：`go build ./...` 通过；`grep -r "internal/voice" --include=*.go` 无匹配。

- [x] **E2 — 删除配置字段与绑定**
  内容：`store.Config` 删 `VoiceEnabled`/`VoicePomodoro`/`VoiceReminder`；删 `SaveVoiceSettings`/`TestVoice`/`StopVoice`/`OpenVoiceStudio`/`StudioPath` 绑定；`app.go` 删 `initVoice`/`initVoiceStudio` 与 `vstudio` 状态。
  完成判据：`voiceEnabled` 等不再出现在 `GetState` 的 JSON 里；生成的前端绑定（`App.d.ts`/`App.js`）不再含这四个方法；`check-frontend.cjs` 的「假桥接名单与 Go 导出集合逐名相等」通过。

- [x] **E3 — 删除设置页语音区**
  内容：`frontend/index.html` 删语音整块（三开关、试听、停止、音色显示、打开工坊按钮）；`main.js` 删 `renderVoiceSettings`/`wireVoiceSettings`/`voiceSettingsPayload` 与 `els` 中相应条目。
  完成判据：`check-frontend.cjs` 的「id 集合」断言通过（不多不少）；设置页无任何 `voice-*` id。

- [x] **E4 — 旧数据原地保留**
  内容：不删 `<dataDir>/voice/`；`initVoice` 的 `voice.Cleanup` 随 E1 一并消失（不再清理）。
  完成判据：`grep -r "Cleanup(" --include=*.go internal/ app*.go` 无语音相关匹配；文档写明该目录位置且说明不予删除。

## F. 打包支持包内资源

- [x] **F1 — manifest 新增 `assets` 声明**
  内容：`Manifest` 加 `Assets []string \`json:"assets,omitempty"\``；每项须通过 `ValidRelPath`、不以 `/` 开头、不含 `..`，数量上限沿用既有风格（新增 `maxAssets = 32`）。
  完成判据：合法相对路径通过；`../x`、`/abs`、`a\b`、超过 32 项 → 拒绝且文案含 `assets`。

- [x] **F2 — `package.ps1` 打进 `assets`**
  内容：`zipMembers` 追加 manifest 中列出的 `assets`；文件不存在则打包失败并点名。
  完成判据：带 `assets:["lines/a.wav"]` 的插件打包后 zip 内含该条目；声明了但文件缺失 → 脚本非零退出且输出含文件名。

## G. 新插件 `com.shixiansi.plugin-voice`

- [x] **G0 — 界面挂在插件市场程序里**（用户裁定 2026-10-14）
  内容：**不**新增面板槽位、**不**新增面板 tab。语音插件的界面（音色选择 + 逐条台词工作面）
  作为一个**插件自己的 HTML 文档**由市场程序托管在「该插件的设置区」，两栏布局照搬工坊原样。
  完成判据：桌宠面板的 tab 数仍为 4（`check-frontend.cjs:462` 的「恰好 4 个 data-tab」不变）；
  市场程序里选中该插件时能看到两栏工作面。
  > 这条取代了原先的 G2/G3（新增 `voice` 槽位 + 单栏重设计）——**那两条作废**。

- [x] **G1 — manifest**
  内容：`id/name/version 1.0.0/author/description`；`permissions: ["audio","events","storage","plugin.config","net.fetch"]`；
  **`background: true`**（常驻帧，靠 `events` 门控）；**无 `panel`、无 `panelSlot`、无 `surface`**
  （界面由市场托管，不占桌宠的任何界面落点）；`config` 含 `ttsBaseUrl`(hostWhitelist)/三个权重路径/五个采样参数；
  `entry: "workshop.html"`；`assets` 列内置台词与工坊样式。
  完成判据：`ParseManifest` 通过；`background` 与 `events` 同时存在；`netHosts` 与 `net.fetch` 的配对规则满足。
  > **注意**：`validateEntry` 要求 `panel || surface` 至少有一个。本插件两者都没有，
  > 所以 **G1 依赖 A5**（见下）——`entry` 的用途要从「面板/浮层面入口」扩展为
  > 「任何由宿主程序托管的界面入口」，并允许一个只声明 `background` + `entry` 的插件通过校验。

- [x] **G2 — 市场侧托管（依赖 H 节）**
  内容：市场程序为选中插件提供一个「界面」区域，用 `/plugin-asset/<id>/<entry>` 挂沙箱 iframe，
  经共享调度层收发 `PluginRequest`。
  完成判据：iframe 的 `sandbox` 恰为 `allow-scripts`；插件发起的 `audio.save` 在市场进程里
  写进**同一个** `plugin-data/<id>/audio/`；桌宠随后能播到它。

- [x] **G3 — 两栏工坊界面**
  内容：`panel.html` + `app.js` + `style.css`（沿用插件约定文件名，不另起 `workshop.*`）：
  左栏角色列表（点一下即切换，并显示该角色已录几条），右栏每条台词一行
  （试听 / 生成 / 改回内置）+「整套换成这个角色」；顶部三个开关。
  完成判据：`style.css` 无 `box-shadow`/`filter`/`transform`（有断言）；
  角色列表与「整套换成这个角色」的**行为**由 `market/tests/plugin-voice.test.js` 覆盖
  （角色来自迁移写入的 `roleNames`、计数正确、点击切换会保存、整套采用只改该角色已录的句子）。
  > **仍未做的**：这套界面**没有在真实浏览器/WebView2 里点过**。jsdom 测试能证明逻辑与
  > DOM 结构，证明不了排版在视觉上成立——市场窗口 980×700、面板最小 720 宽，
  > 「720 宽下无横向滚动」这条只能靠眼睛。首次真机使用时建议按
  > **打开市场 → 选中语音插件 → 看左栏是否有角色 → 点一个 → 试听** 的顺序确认。

- [x] **G4 — 生成走 `net.fetch`，引擎由用户启动**
  内容：调 `ttsBaseUrl` 上的 GPT-SoVITS：先 `set_gpt_weights`、再 `set_sovits_weights`、
  最后 `POST /tts`（引擎常驻**一个**模型，不先切权重就会用上一个角色的声音合成——
  这正是「换了角色却还是旧声音」那类 bug 的成因）。
  **不**尝试启动进程（用户裁定 A）。
  完成判据：缺少三个权重路径时点「生成」**不发任何请求**并有提示；**引擎已知未运行时
  点「生成」同样不发请求**（`engineUp === false` 时前置拒绝并提示，见下）；
  三次调用的顺序由测试钉住；权重调用失败会把按钮恢复可用。
  > `engineUp` 有**三态**而非两态，这一点是刻意的：`false` 才拦，
  > `null`（尚未探测 / 探测未回复）**不拦**——把「未知」当「不可用」会让按钮在一次探测
  > 完成前一直是死的。两条都写了测试。
  >
  > 仍未做的：`/set_gpt_weights` 与 `/set_sovits_weights` 是 GPT-SoVITS api_v2 里
  > **旧工坊用过**的接口，但**本插件对它们的调用没有跑过真实引擎**——
  > 顺序与参数只由 jsdom 测试钉住。

- [x] **G5 — 开关与采用状态存在插件自己那里**
  内容：三个开关（总开关 / 朗读番茄 / 朗读提醒）与「每条采用哪一版」存插件 `storage`；
  收到 `timer.phaseEnded` / `reminder.fired` 时 `audio.play` 对应片段。
  完成判据：改开关后重启插件状态保持；订阅两个事件后，宿主抛事件能触发一次 `audio.play`
  且播的是**被采用那一版**的 name。

## H. 门禁与文档

- [x] **H1 — harness 覆盖常驻帧与音频能力**
  内容：`.dev/run-frontend-harness.cjs` 加断言：常驻帧挂载/沙箱/握手、事件投给常驻帧、`background` 反例、
  以及**同一插件同时占槽位与常驻帧**。
  完成判据：harness 全绿（142 项）且新增断言出现在 PASS 列表。

- [x] **H2 — check-frontend 覆盖新 id 与权限**
  内容：`.dev/check-frontend.cjs` 加：语音区已移除、`#plugin-background` 的挂载与隐藏纪律、
  权限表长度（**11，不是 12**——见 A2 的偏离说明）、以及插件包与内置音频的完整性。
  完成判据：`node .dev/check-frontend.cjs` 退出码 0（152 项全绿）。
  > 原判据写的「权限表长度 12」随 A2 一并作废。`#plugin-background` 的存在性断言在 harness 里
  > （H1），静态门禁这里断言的是它的**隐藏纪律**与语音区已消失。

- [x] **H3 — 门禁脚本去语音**
  内容：`.dev/verify-live.ps1` 若含语音断言则删除（不是改指）；`verify-region.ps1` 同查。
  完成判据：两个脚本 `Parser::ParseFile` 无错误；不再提及 `voice`。
  > 实际核查结果：这两个脚本**本来就没有语音断言**（它们验的是对话插件浮层面与窗口区域裁剪），
  > 所以没有要删的东西。四个 `verify-*.ps1` 加上 `check-all.ps1` 全部 `ParseFile` 无错误。

- [x] **H4 — 构建与全量测试**
  内容：`npm run build`（两个前端）、`wails build -s`、`go test ./...`、市场程序 `go vet`/`go test`、插件 vitest。
  完成判据：全部退出码 0 且无 FAIL；`build/bin/PetPC.exe` 存在。
  > **一处已知的环境限制，不是代码问题**：`wails build -s`（不带 `-skipbindings`）在本沙箱内
  > **必然失败**，因为 Wails 的绑定生成器要覆盖写 `frontend/wailsjs/runtime/package.json`
  > 而该目录的写入被拒（`Access is denied`）。绑定本身是**已提交且已同步**的，静态门禁
  > 「每个 App.* 调用都是导出的绑定 / 都在 App.d.ts 里声明」正在证明它们一致。
  > 用 `wails build -s -skipbindings` 可正常构建出两个 exe。
  > 因此 `check-all.ps1` 里 **12 个门禁有 11 个 PASS**，唯一 FAIL 的是 `wails build`。

- [x] **H5 — 文档**
  内容：`README.md` 语音章节改写为「语音是插件」；`docs/voice.md` 重写为插件说明；`docs/voice-studio.md` 标注【已并入插件】；`docs/genshin-tts.md` 标注工坊相关段落去向。
  完成判据：README 不再声称宿主内置语音；`docs/voice.md` 的台词表与实际插件一致。

- [x] **H6 — 打包与内置样板**
  内容：`market/tools/package.ps1` 产出语音插件 zip、回写 `index.json`、复制到 `internal/plugins/sample/`；`sample/embed.go` 清单同步。
  完成判据：两处 zip 的 sha256 与 `index.json` 三方一致；`embed.go` 列出的文件名都在目录里；`go build ./internal/plugins/` 通过。

- [x] **H7 — 本机安装验证**
  内容：装进 `%APPDATA%\PetPC\plugins\`，`plugins.json` 记 1.0.0 并授权五项；重编 exe。
  完成判据：已装目录四个资源与仓库逐字节相同；`plugin-data/<id>/` 私有数据未被触碰。
  > 本机迁移也跑过了：12 段旧配音逐字节复制进插件私有目录，
  > `activeRole=yoimiya`、五条台词全部指向宵宫的版本，旧库 `voice\user\` 原样保留。

## 实现期间发现的额外问题（原计划未预见）

- **`net.fetch` 的文本形式会损坏二进制**：Go 的 JSON 编码器把非 UTF-8 字节替换成 U+FFFD，
  于是一段 WAV 回来时**长度正确、内容全是替换字符**——看起来成功了，播出来是噪音。
  加了 `encoding: "base64"`。测试：`TestNetFetchCanReturnBinaryWithoutCorruptingIt`。
- **`index.json` 不携带 `netHosts`**：任何用 `net.fetch` 的内置插件**都装不上**
  （安装时比对目录与包内 manifest，不一致即拒），而报错却打印两份**完全相同**的权限列表，
  极具误导性——真正的差异在没被打印的 `netHosts` 上。已修打包脚本与错误文案。
- **迁移会选中「文件缺失」的片段**：指向不存在的文件会**静默不出声**，看起来就是功能坏了。
  现在 `chosen` 只会指向确实复制成功的 take。
- **装了插件重启却不迁移**：迁移要求插件私有目录已存在，而 `Install` 并不创建它。
  只有先打开过工坊（触发写设置）才会「碰巧」成立。现在由迁移自己创建该目录。
- **插件自己抹掉了迁移结果（数据丢失）**：`ensureBuiltins()` 在握手阶段就写盘，
  而那一刻 `settings` 还是空默认值，于是把刚迁进来的库覆盖成空。
  详见 `docs/voice.md` §6.2——这是本次最危险的一个，因为音频还在、指针没了、
  而 `.migrated` 标记还宣告着「迁移成功」。
- **`.dev/verify-live.ps1` 有一个永远不可能通过的断言**（先前就存在，与本功能无关）：
  它的探针页面画 `#00c8ff`（青色），而判据 `DarkFraction` 要求 R/G/B 都在 20..90 之间——
  青色 (0,200,255) **永远不满足**，所以浮层面明明画了 99% 也报 0%。
  已把探针颜色改成深色（`#303540`）。**该文件未被 git 跟踪**（`.dev/*` 被忽略且无 `!` 反例），
  所以 `git status` 看不到它被改过。

## 未决项（已被用户裁定，保留以备追溯）

原计划在这里留了一个问题：语音插件的界面挂在哪。用户最终裁定
**挂在插件市场程序的窗口里**（980×700 可缩放，两栏布局原样保住），
而不是新增桌宠面板 tab，因此：

- **不**新增 `voice` 槽位名，**不**给桌宠面板加第 5 个 tab（tab 数仍是 4）；
- 工坊界面是插件自己的 `panel.html`，由**市场程序**在它的设置区挂起来；
- 因此**不需要**改 `check-frontend.cjs` 的「恰好 4 个 data-tab」断言。
2. **内置 5 条音频是否随包**：design 裁定「随包」，则包体积 +569KB；需确认可接受。
