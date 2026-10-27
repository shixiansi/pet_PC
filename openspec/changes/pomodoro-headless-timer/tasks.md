# tasks: 番茄钟的计时循环搬进常驻帧

> 设计理由与三条候选路的取舍写在 [proposal.md](proposal.md)；这里只列可勾的步骤。
> 这一份**还没开工**——它是 `timer.phaseEnded` 那个已知缺口的正式收尾方案。

## 1. 让常驻帧成为唯一的计时者

- [ ] `manifest.json` 加 `"background": true`（它已经声明 `events`，那是常驻帧的闸门之一），
      版本升到 2.1.0；`shareRead` 不需要加（写的是自己的键）
- [ ] `app.js` 分两个入口：`surface.kind === 'background'` 走"计时者"分支
      （读 state/records/config、起 `setInterval(tick, 1000)`、写记录、发布事实、**不碰 DOM**）；
      `surface.kind === 'surface'` 走"视图"分支（只 `paint()`，命令经 `plugin.share` 提交）
- [ ] 命令通道：视图写 `command`（`{op, todoId, text, at}`），计时者每秒读一次并清空；
      最多 1 秒延迟，与现在的 tick 同量级
- [ ] 记录 id 由阶段结束时刻派生（`rec-<endedAtMs>`），让两个上下文写同一段结束时收敛成一条
- [ ] 视图每秒重读一次状态（`storage.get` on `state`）——它自己不再 tick

## 2. 发布"阶段结束"这个事实（路 b）

- [ ] 计时者在 `out.endedPhase` 时同时 `share.set {name:"phase-ended", value: JSON.stringify({phase, at})}`，
      `at` 用**阶段真实结束时刻**而不是"发现它的时刻"
- [ ] 语音插件：manifest 加 `plugin.share` 权限 + `shareRead: ["com.shixiansi.plugin-pomodoro/phase-ended"]`；
      常驻帧每 2 秒 `share.get {owner, name}`，`at` 变了就说那一句（版本升到 1.0.2）
- [ ] 语音插件保守起见**保留** `timer.phaseEnded` 订阅：宿主将来若重新有发射方，这条路径自动恢复

## 3. 收尾

- [ ] 两个插件打包（`market/tools/package.ps1`）+ 安装 + `index.json` 与内嵌 sample 同步
- [ ] `market/tests/` 补两条：视图分支不发状态写、计时者按 id 去重
- [ ] 门禁：`go build`/`vet`/`test`、两个前端的 build+test、`check-frontend`、`run-frontend-harness`、
      `tools/plugin-market` 的 vet+test、`market` 的 npm test
- [ ] 文档：`docs/voice.md` §2.1 的补记改成"已由 `pomodoro-headless-timer` 解决"，
      README 语音那一节的缺口说明同步
- [ ] 在此之前 `timer.phaseEnded` 保留在 `SubscribableEvents` 里（删掉会让已装插件 subscribe 报错）

## 验收

- 关注在跑：关掉番茄钟浮层面 → 到点后语音插件**不用重开界面**就说出「专注时间结束了」
- 同一次阶段结束只产生**一条**记录（打开浮层面再关掉、反复打开，都不新增）
- 两个帧同时存在时，`storage` 里的 state 不被丢更新（视图不写状态）
