# tasks: 番茄钟的计时循环搬进常驻帧

> 设计理由与三条候选路的取舍写在 [proposal.md](proposal.md)。
> **已完成（2026-10-28）**，插件版本 `plugin-pomodoro` **2.1.0** / `plugin-voice` **1.0.2**。

## 1. 让常驻帧成为唯一的计时者

- [x] `manifest.json` 加 `"background": true`（它已声明 `events`，那是常驻帧的闸门之一），
      版本升到 2.1.0；`shareRead` 不需要加（写的是自己的键）
- [x] `app.js` 分两个入口。判据是 `surface.slot === 'background'` 而**不是** `kind`：常驻帧的
      `kind` 也是 `panel`（它是一个没有矩形的文档），只看 `kind` 会让计时器跑进视图的文档里。
      - 计时者：读 state/records/config、`setInterval(tick, 1000)`、写记录、发布事实、**不碰 DOM**
        （`paint()` 在 `role === 'timer'` 时直接 return —— 那里的 `els` 全是 null）
      - 视图：每秒 `storage.get` 读 state、`paint()`，命令经 `plugin.share` 提交
- [x] 命令通道：视图写 `command = {op, todoId, text, skip}`，计时者每秒读一次、应用、**清空**。
      最多 1 秒延迟，与 tick 同量级。清空是必须的：不清就会每个 tick 重放同一条命令
- [x] 记录 id 由阶段结束时刻派生（`rec-<endedAtMs>`），并用 `records.some(r => r.id === id)` 去重，
      所以"同一段结束被 reconcile 两次"收敛成一条
- [x] 视图每秒重读一次状态——它自己不再 tick

**一处设计修正**：原计划写"`{op, todoId, text, at}`"。`at` 是多余的——命令由计时者在读到它的那一 tick
执行，带一个客户端时间戳只会引入"客户端钟和宿主钟不一致"这个没人需要的自由度。实现里没有它。

## 2. 发布"阶段结束"这个事实（路 b）

- [x] 计时者在 `out.endedPhase` 时 `share.set {name:"phase-ended", value: {phase, at}}`，
      `at` 用**阶段真实结束时刻**（`out.record.endedAt`，即 `endsAt`）而不是"发现它的时刻"。
      发布在**抬门控之前**：语音那句说的是刚结束的那一段，不该等一个用户还没回答的问题
- [x] 语音插件：manifest 加 `plugin.share` + `shareRead: ["com.shixiansi.plugin-pomodoro/phase-ended"]`；
      常驻帧每 2 秒 `share.get`，`at` 比上次大才说那一句（版本升到 1.0.2）
- [x] 语音插件**保留** `timer.phaseEnded` 订阅，两条路径并存：宿主将来若重新有发射方，
      这条路径自动恢复，两侧都不用改

**一处实现细节**：`phasePollInFlight` 必须在**任何**提前返回之前清掉。一次被拒的读取
（番茄钟插件没装）是正常答案，若把标志留在 true，轮询就永久停了——而这正是"插件后装上来、
桌宠不重启"那个场景要能工作的前提。

## 3. 收尾

- [x] 两个插件打包（`market/tools/package.ps1`）+ 安装 + `index.json` 与内嵌 sample 同步
      （旧的 2.0.1 / 1.0.1 两个 zip 从 sample 里删掉，`embed.go` 的清单同步）
- [x] `market/tests/` 补了 16 条：`plugin-pomodoro-frames.test.js`（10 条：两个角色、发布事实、
      记录去重、门控从 state 读出）与 `plugin-voice.test.js` 的 6 条（轮询、只响一次、
      新截止时刻再响、空值与坏值不说话、被拒后不卡死）
- [x] 门禁：`go build`/`vet`/`test`、`tools/plugin-market` 的 vet+test、`check-frontend`（117）、
      `run-frontend-harness`（53）、`frontend` npm test（34）、`market` npm test（**461**）
- [x] 文档：`docs/voice.md` §2.1 的补记改成"已由 `pomodoro-headless-timer` 解决"，
      能力表里的 `todos.*` 换成 `plugin.share`；README 语音那一节的缺口说明同步
- [x] `timer.phaseEnded` 保留在 `SubscribableEvents` 里（删掉会让 1.0.1 及更早的插件 subscribe 报错），
      `notifyPluginPhaseEnded` 的注释改成"这是刻意退休而不是缺口"

## 验收

- 关照在跑：关掉番茄钟浮层面 → 到点后语音插件**不用重开界面**就说出「专注时间结束了」
  （由 `plugin-pomodoro-frames.test.js` 的"a finished focus publishes the fact" 与
  `plugin-voice.test.js` 的"a new phase ending speaks the line" 两侧共同钉住）
- 同一次阶段结束只产生**一条**记录（`rec-<endedAt>` + 去重，有测试）
- 两个帧同时存在时 `storage` 里的 state 不被丢更新（视图不写状态，有测试）

**未在真机上验证**：这一轮的门禁都是单元与浏览器层面，没有交互桌面跑 `verify-live.ps1`。
首次真机使用时按 proposal 的验收清单确认一次——尤其是"关掉浮层面后到点是否出声"。
