# proposal.md — plugin-share-subscribe

> `plugin.share` 是**牌盘**，不是总线：读者只能轮询。2026-10-31 的桌宠日志里 94% 是 DEBUG，
> 绝大部分就是这件事——`media-sync` 每秒、`pet-sense` 每秒、`privacy-guard` 每两秒各一次
> `share.get`，每次都读到同一个值。
>
> 本 change 给"某个键变了"补一条**推送**：`share.set` 落盘之后，只发给**在 `shareRead` 里点名了
> 这一对 `<owner>/<name>`** 的插件。授权完全复用既有的那一条，不新增权限、不新增方法。

## 1. 现状（本仓实读事实）

| 事实 | 出处 |
| --- | --- |
| `share.set/get/remove` 的规则在 `internal/plugins/share.go`，两个宿主共用 | `share.go:65,100,170`、`RegisterShare` |
| 读别人的键要先在 `shareRead` 里写 `<owner>/<name>`，并持有 `plugin.share` | `share.go` 的 `DeclaredShareRead` |
| 宿主 → 插件的唯一事件通道是 `pet:plugin-event`（`PluginEventView{PluginID,Name,At,Payload}`） | `app_plugins.go` 的 `emitPluginEvent` |
| 前端按 `pluginId` 路由到浮层面帧与常驻帧，没有帧就丢掉 | `frontend/src/plugin-host.js` 的 `routeEvent` |
| 插件之间的"等一个值变化"只能靠轮询 | 各插件的 `setInterval(..., 1000/2000)` |

## 2. 变更说明

- `share.set` 成功之后，宿主遍历已装插件，给**同时满足**下面三条的每一个发一条
  `share.changed`：`shareRead` 里点名了 `<publisher>/<name>`、持有 `plugin.share`、插件已启用且
  manifest 读得出来。
- payload：`{key, owner, name, at}`。`key` 与 `share.get` 返回的 `key` 是同一个字符串，可以直接拿去读。
- **不要求 `events.subscribe`**：授权就是 `shareRead` 那一对 + `plugin.share`，与 `share.get` 同一份。
- **不是投递保证**：只有被挂载的帧收得到（前端丢弃没有界面的插件的事件），所以轮询保留为兜底。

## 3. 明确不改

- 不新增权限、不新增方法、不改 `share.*` 的语义与上限。
- 不做"订阅任意键"：能收到什么，完全由 `shareRead` 决定——那是安装对话框里能给用户念出来的东西。
- 不改控制中心的 `share.set`（那里同时只挂一个插件页，读者通常不在场）；文档里写清"这条事件在桌宠里生效"。

## 4. 影响面

| 文件 | 改什么 |
| --- | --- |
| `app_plugins.go` | `pluginShareSet` 在写成功后调 `notifyShareReaders`；新增 `notifyShareReaders` |
| `app_plugins_test.go` | `TestShareSetNotifiesDeclaredReaders`：只有声明过的读者收到、未声明的键不唤醒任何人 |
| `market/docs/plugin-dev.md` | §7 加 `share.changed` 一节（含"为什么它是推送、为什么轮询还得留着"），顺手修掉 §7 表里 `timer.phaseEnded` 那行被粘到上一行的排版错误 |

## 5. 验证

```powershell
go vet ./...
go test -count=1 .                    # TestShareSetNotifiesDeclaredReaders
node .dev\check-frontend.cjs
cd market; npm test; node docs/plugin-testkit/plugin-check.mjs plugins/*
```
