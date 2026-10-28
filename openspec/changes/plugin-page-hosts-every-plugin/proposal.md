# proposal: 插件页挂载每个插件的界面，并删掉控制中心的待办页

## 要解决什么

用户的原话：

> 设置面板的待办界面应该放在插件的面板里，同时番茄钟没有设置，番茄钟的记录插件页面也没有展示，
> 对话插件同样也是没有界面。

四句话，两个根因：

1. **插件页拒绝挂载声明了 `surface` 的插件。** 上一轮加这条的理由是真的（番茄钟的文档在页面
   中间报「本程序不支持的方法 share.set」），但结论反了：`plugin.share` 是
   `plugin-data/<id>/share/` 里的一叠文件，**控制中心同样拥有那棵树**，从来不是"宠物才有的
   能力"。拒绝挂载的后果是待办 / 番茄钟 / 对话三个插件**在唯一有地方放界面的窗口里都没有界面**。
2. **待办页在读一个没人写的文件。** 列表搬进待办插件的 `storage` 之后，`todos.json` 只剩
   一次性迁移在用；控制中心的「待办」页还照着它渲染，数据是旧的，而插件自己的文档早就支持
   "panel 挂载按列表渲染"。

外加两处**插件缺东西**：番茄钟的 manifest 一个 `config` 都没声明（`machine.js` 其实一直在读
`focusMinutes` 等五个键），所以没有「设置」；它的文档只有倒计时，**记录写了没人读**。

## 做什么

1. **`plugin.share` 变成两个宿主共用的规则**：`internal/plugins/share.go`（`ShareSet/Get/Remove`
   + `RegisterShare`），桌宠的三个方法变成转发，控制中心注册同一套。
2. **插件页挂载所有"已装 + 已启用 + 有 entry"的插件**；`surface` 不再是拒绝理由——坐标是从宠物
   量出来的，但文档自己知道自己在哪（`surface.slot`）。
3. **删掉控制中心的「待办」页**（组件、左栏项、`Market.Todos()` 绑定、dev 桩、冒烟断言），
   列表由待办插件的文档在插件页里画。
4. **番茄钟 2.2.0**：manifest 声明那 5 个设置键（默认值与 `machine.js` 的兜底一致）；文档加
   「最近专注」列表（只读、新在前、最多 20 行），`workshop` 里隐藏倒计时按钮并说明计时在桌宠上。
5. **对话 1.7.5**：`workshop` 里历史照常渲染、输入框只读并写明"发送请在桌宠旁的对话浮层面"。
6. **待办 1.2.0**：列表角色每 2 秒重读一次（写请求在飞时暂停），因为同一个列表可能同时开在
   两个窗口。
7. **插件页给出「更新到 vX」**：界面跟着插件走，装了旧包就看不到上面这些；目录读不到时按钮
   不出现（离线是正常状态），只有更新方向、没有降级。

## 明确不做

- **不把 `llm.chat` 搬进控制中心**（用户确认选"只读历史"）。那会是第二个 LLM 路径：第二份凭据
  解析、第二份预算计数、第二个写 `llm-usage` 的进程，还要给插件帧补一条事件下行通道。
- **不服务 `bubble.say` / `surface.*` / `events.subscribe` / `todos.*` / `timer.*`**：它们要的是
  宠物窗口、宠物计时器、宠物预算。"本程序不支持该方法"仍然是诚实的回答。
- **不重写对话插件**：只加一条 `workshop` 分支；桌宠那边的 `chat` 槽位一行未动。
- **不给控制中心加"清空记录"之类的写入口**：计时器与记录的唯一写者仍是那个常驻帧。

## 影响的不变量

| 不变量 | 这次的关系 |
|---|---|
| `AGENTS.md §3.1` 权限闸门 | 顺序未改；`plugin.share` 本来就在 13 项里，**没有新增权限**，所以七处清单一项都不用动 |
| `AGENTS.md §3.3` 插件帧 | 沙箱属性、`postMessage`、opaque origin 一条未改；新增的一句是"两个宿主各服务哪些方法，文档靠 `surface.slot` 分辨自己在哪" |
| 「一个文件只有一个写者」 | `llm-usage` 的写者仍然只有桌宠（这也是本轮拒绝在控制中心实现 `llm.chat` 的理由之一）；插件的 `storage` 键本来就只有一个写者——**列表角色现在多了一个读者**，不是第二个写者 |
| `§3.7` 内置 sample 与市场一致 | `tools/package.ps1` 重新打包 6 个插件，`embed.go` 指到新版本，`TestBuiltinSampleAgreesWithTheMarketCatalogue` 绿 |
| `.dev/check-frontend.cjs` | 一条旧断言（钉住"surface 不许挂载"）改成负断言 + 新增两条（挂载只看已装/已启用/entry；待办页不许回来） |

## 验证计划

- `market/`：`npx vitest run` → 471 通过 / 1 跳过（新增 9 条：番茄钟的记录与 workshop、待办的轮询、对话的只读）；
- `tools/plugin-market`：`go test ./...` → 新增 `share_test.go` 三条（往返、未声明就拒、只能删自己的）；
- `cc`：`npm run check` 0 error、`npm run smoke` 63/63（新增：surface 插件也挂、番茄钟设置里有「专注时长」）；
- 宿主：`go vet ./... && go test ./... -count=1`；
- `node .dev/check-frontend.cjs` → 124 条；
- `pwsh -File tools\build-all.ps1` 重建两个程序；`TestBuiltinSampleAgreesWithTheMarketCatalogue` 绿。
