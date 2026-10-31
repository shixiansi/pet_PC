# design.md — plugin-process-and-index

> 决策与"为什么不是更自然的那个写法"。每条都对应 `proposal.md` 里的一处改动。

## D1 停止一个进程，按"谁启动的"授权，不按 pid

`process` 权限的含义是"启动**用户指给你的**程序"，而 pid 是可猜的数字。如果 `process.kill` 只看 pid，
任何有 `process` 的插件都能停掉别人的协议端——这不是权限大小的问题，是**权限含义被偷换**的问题。

所以授权来源是宿主自己的子进程表（`pluginHost.children`），不是 payload：

```go
child, ok := h.children[in.PID]
if !ok { /* 未知 pid：不是错误，见 D2 */ }
if child.pluginID != id { return nil, fmt.Errorf("进程 %d 不是本插件启动的，不能停它", in.PID) }
```

拒绝信息里写出 pid，但不写"它属于谁"：插件猜 pid 是它自己的 bug，它需要读到"这不是你的"，
不需要知道那是谁的。

## D2 "它已经自己退了"不是失败

三种结局的划分是按**调用方的意图**切的，不是按宿主的状态机切的：

| 情形 | 返回 | 理由 |
| --- | --- | --- |
| 跑着，是我们的 | `{killed:true, known:true}` | 意图达成 |
| 是我们的，已经退出 | `{killed:false, known:true, exitCode}` | 意图（"确认它不在跑"）**已经**达成 |
| 不在表里 | `{killed:false, known:false}` | 上一次运行的 pid 合法地不存在；与 `process.state` 同一条规则 |

把第二种变成错误，会让每个插件的每条清理路径都需要一个特例分支——而清理路径恰恰是最不该写
分支的地方。第三种不报错则是 `process.state` 已经定下的规矩（`{known:false}` 读作"它已经不在了"），
两个方法在同一个问题上不该给两种答案。

## D3 回收的判据是（插件，版本），不只是"还装没装"

只按"插件是否启用"回收会漏掉当天那起事故：插件是**升级**，升级后它仍然启用，可它启动的那个
helper 是按旧版本的文件名写的。升级会重挂帧（`frontend/src/main.js` 的 1.5 秒轮询），旧帧的
pid 表随之消失，所以那个 helper 已经不属于任何人。

于是 `trackChild` 记下启动时的版本，回收者比较：

```go
case row.Version != "" && child.version != "" && row.Version != child.version:
    // 插件已更新到 X：旧版本启动的进程不算谁的子进程了
```

两边都必须非空才比较：manifest 读失败时 `row.Version` 是空的，而**拿一个"不知道"去杀进程**
是这份代码里最不能有的行为。

## D4 桌宠退出时不杀子进程（这是既有契约，不是遗漏）

`process.launch` 的注释已经把理由写死了：QQ 插件启动的协议端（Yogurt）必须**跨桌宠重启**活着，
否则每次退出桌宠都等于把用户的机器人踢下线。所以退出时不做"全部回收"。

取而代之的是 `logSurvivingChildren()`：退出前把还活着的子进程逐个记一行 INFO（pid + 插件）。
理由是当天那个问题的另一半——**孤儿是看不见的**。用户在任务管理器里看到一个 `powershell.exe`，
既不知道它是谁启动的，也不知道它为什么还在。一行日志把这件事变成可查的。

`shutdown()` 里仍然跑一次 `reapPluginChildren()`：停用/卸载/升级的孤儿该收，健康的协议端留着。

## D5 不把子进程表落盘

落盘能让"重启后的桌宠"收掉上一次运行的孤儿，但代价是 **pid 复用**：重启之间 pid 可能被系统分给
一个完全无关的进程，而"按记录里的 pid 杀一个陌生进程"是本 change 唯一不能接受的失败模式。

要安全地落盘，必须同时记录**进程创建时间**（`OpenProcess` + `GetProcessTimes`，`golang.org/x/sys`
已经是直接依赖，技术上可行），再在回收前比对。但那套东西真正要解决的问题是"辅助脚本的生命周期"，
而那是 sidecar 的事（见 proposal 第 5 节）：宿主托管 sidecar 之后，孤儿根本不会产生，也就不需要
一份跨重启的 pid 表。

所以本 change 只收"同一次运行内"的孤儿，并把跨重启的那一半明确留给 sidecar。

## D6 `plugins.json` 用重试，不用锁

直觉解法是加锁（`LockFileEx` / 锁文件）。它**解决不了**这个失败：挡住 rename 的不是另一个写者，
而是一个**读者**——桌宠每 1.5 秒 `os.Open` 一次 `plugins.json`，Go 在 Windows 上打开文件时不带
`FILE_SHARE_DELETE`，于是 rename 落到一个"有人打开着"的目标上就是 `ERROR_ACCESS_DENIED`。锁只
约束写者，读者照样让 rename 失败。

所以：

- **重试**，8 次、退避 `4·(i+1)` ms，总计约 200 ms。挡住它的是一次读，长度是微秒级；再长就只是
  把真问题藏在一个"卡住"后面。
- **只对争用重试**（`ERROR_SHARING_VIOLATION` 32、`ERROR_ACCESS_DENIED` 5、POSIX `EACCES`/`EPERM`）。
  磁盘满、路径不存在这类错误第一次就返回——把"写不进去"伪装成"有人在读"是更坏的失败。
- **不改读路径**：原子写本来就保证读者看不到半个文件，这一半一直是对的。

`renameFile` 是一个包级变量，只为让测试能造出 Windows 那种失败而不必真的开第二个进程持文件。
这是本包里唯一的测试缝，注释里写明了理由。

## D7 锁序：manager → childMu

回收者要先知道"哪些插件还在、版本是多少"（`pluginManager().Plugins()`，manager 自己的锁），
再读子进程表（`childMu`）。启动路径本来就是这个顺序（`pluginProcessLaunch` 先取版本，再
`trackChild`），所以回收者必须一致，否则就是一个等慢盘的死锁。

实现上就是把两步**分开取**：先 `Plugins()` 拿到快照（不持 `childMu`），再进 `childMu` 扫表。

## D8 日志等级

- 回收成功：**INFO**（进程被停掉是人能看见的事，属于"动作面"，与 `process.launch` 同级）。
- 回收失败：**WARN**。
- 退出时的存活者：**INFO**。

不写 payload、不写命令行：`AGENTS.md` §3.8 的"没有正文"同样适用于这里（命令行里可能有令牌）。
