# proposal: 日志系统 —— 有等级、一直在写、并且在控制中心里能读

## 要解决什么

用户的原话：

> 现在插件行为以及宠物行为模型调用都没有日志，也没有日志系统，每次报错都不清楚是什么问题，
> 添加日志系统，在通知面板中添加日志，以及插件行为都要进行日志记录，日志有等级区分，并写入本地。

（"通知面板"在确认后落成了**控制中心新增的「日志」页**：桌宠那块 380×520 的 tab 面板已经
退休，宿主里没有任何叫「通知面板」的界面。）

现状是 `App.DebugLog`：只在 `PETPC_DEBUG=1` 时往 `debug.log` 追加一行 `15:04:05.000 消息`。
它有三个性质，正好对应三类无法回答的报告：

| 报告 | 为什么当时无法回答 |
| --- | --- |
| 「插件点了没反应」 | 没有开关就没日志；插件在沙箱 iframe 里，宿主在另一个进程，`log.Printf` 在 Windows 上没有人读 |
| 「模型不回话」 | 端点/模型/密钥住在**插件**的设置里，请求由**宿主**发出，两边都不留痕；只有一行启动快照 |
| 「看一下日志」 | 要用户重新启动一次程序（带上环境变量）再复现一次，而他那次已经复现过了 |

还有第四个更隐蔽的：**所有行一样重**。托盘注册失败与一次指针拖动在同一份文件里，按到达顺序
排，没有等级，也就没有"先看这一行"。

## 做什么

1. **`internal/logging`**：一个两程序共用的写者/读者。
   - 四个等级 `DEBUG < INFO < WARN < ERROR`；
   - 写 `<dataDir>\logs\<程序>.log`，**一直在写**，默认 `INFO`；
   - 单文件 2 MB 轮转，保留一代 `.1`；
   - `Entry.Line()` / `ParseLine()` 是格式的唯一实现，`Read` 是唯一的读者（按时间归并两个程序）；
   - 所有方法对 nil 接收者安全。
2. **桌宠**：`App.log` 接上；新增绑定 `App.PageLog(level, message)`，页面的每个失败与 toast
   同句进 ERROR；`DebugLog` 变成 DEBUG 级的转发（不再是"没有开关就没有日志"）；
   30 处 `log.Printf` / `DebugLog` 全部按语义改成 INFO/WARN/ERROR 并带子系统名。
3. **插件行为**：`PluginRequest` 的闸门对**每一次**调用写一行（方法、结果、耗时、字节数，
   payload 只报字节数）；`llm.chat` 另写请求（端点/模型/工具/图片数）、每轮（tokens/finish）、
   完成（字数/耗时）、失败（kind）；每个工具调用一行；`internal/llm` 增加 `Logf` 钩子，
   记录 POST/状态码/重试/流摘要（**不含正文与密钥**）。
4. **控制中心**：`Market.log` 接上（启动、安装/更新/卸装/启用/授权、插件请求、设置保存）；
   新增绑定 `Market.LogTail(query)`；左栏新增「日志」页（等级 / 子系统 / 搜索 / 2 秒自动刷新 /
   复制 / 文件与目录）；「高级 → 诊断报告」的日志尾换成同一份真实日志。

## 明确不做

- **不做遥测/上报**（README「明确不做」）。日志只写本地，只有用户自己复制才会离开这台机器。
- **不做可配置的保留策略**：2 MB × 两代，不做旋钮。一个说不清失败场景的旋钮只会变成支持问题。
- **不做内存环形缓冲当数据源**：两进程共享数据目录，读文件是唯一不会因"谁先启动"而不一致的
  做法（`LogTail` 轮询，2 秒）。
- **不记录正文**：对话、待办、设置值、密钥一律不进日志（见 design §4）。
- **不改插件帧协议**：`postMessage` 那条路一行不动。
- **不给桌宠加"日志页"**：桌宠的窗口是裁剪到宠物 + 一个槽位的，塞不进一个可读的日志视图；
  用户已经选了控制中心。

## 影响的不变量

| 不变量 | 这次的关系 |
| --- | --- |
| `AGENTS.md §3.1` 权限闸门 | **顺序一字未改**（`PluginRequest` 拆出 `dispatchPluginRequest`，检查顺序原样）；日志只在结果之后写 |
| `AGENTS.md §3.2` 窗口区域 | 不涉及：桌宠没有新增任何界面 |
| `AGENTS.md §3.3` 插件帧 | 不涉及（帧协议、沙箱属性、桥接都没动） |
| 「口令与密钥不出宿主进程」 | **加强**：日志里只有 `key=set` 与字节数，`TestLLMChatIsLoggedWithoutTheConversationText` 与 `internal/llm` 的 `TestLogfRecordsAShapeAndNoContent` 守着 |
| `.dev/check-frontend.cjs` | 新增 4 条检查（日志页存在且读宿主日志 / 四个等级各有色与警告竖线 / dev 桩真的会筛选 / 两个程序都无条件开日志） |

## 验证计划

- `go test ./internal/logging/`（格式往返、等级门、轮转、过滤、伪造行、缺失文件）；
- `go test ./internal/llm/ -run TestLogf`（重试被记录；正文/密钥/服务端返回体不在里面）；
- `go test ./ -run 'Logged'`（插件的成功/拒绝/未知方法各一行；`llm.chat` 的请求与完成各一行；
  payload 与回复正文不在里面）；
- `go test ./tools/plugin-market -run TestLogTail`（两个文件按时间归并、新在前、三个筛选真的生效、
  空结果给出原因）；
- `node .dev/check-frontend.cjs`（122 条）；
- `cd tools/plugin-market/cc && npm run check && npm run smoke`（61 条，含新加的 7 条日志页断言）；
- **真机**：`PETPC_DATA_DIR=<临时目录> PETPC_LOG_LEVEL=debug` 启动 `build\bin\PetPC.exe` 9 秒，
  读 `<临时目录>\logs\pet.log` —— 启动/托盘/模型配置/页面 DEBUG/显示窗口五行按格式落盘
  （2026-10-28 实测，见 tasks.md）。
- `pwsh -File tools\build-all.ps1` 重建两个程序；扫 exe 字节确认「自动刷新（2 秒）」已进
  `PetPCPluginMarket.exe`（编译期 embed，只改 cc/ 不重建等于没改）。
