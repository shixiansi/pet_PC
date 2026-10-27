# proposal: 插件页把「插件自己的界面」接回来

## 要解决什么

用户的原话：

> 现在界面改版了后，原来显示的插件界面没有了，应该如何做插件的界面，是接入设置面板的
> 插件页面，还是插件页面只显示插件列表，详情另开一个窗口？

第一句是事实，而且是**功能消失**，不是观感退步。核实到的现状：

- 出货的界面已经是控制中心（`tools/plugin-market/cc`，Svelte）。`tools/build-all.ps1`
  与 `.dev/check-all.ps1` 都是「先 `npm run build` cc，再把 `cc/dist` 覆盖到
  `frontend/dist`」，因为 Go 侧 `//go:embed all:frontend/dist` 读的是后者。
- 旧的市场前端（`frontend/index.html` 的 `#panel-interface` + `frontend/src/plugin-ui.js`）
  源码还在，但**已经不出货**。插件界面的宿主恰好只存在于它里面。
- 新的插件页（`cc/src/pages/Plugins.svelte`）只有三样：列表、启用开关、权限勾选。
  `ui-rebuild` 的阶段 3 任务本身就只写了「插件列表与权限勾选从市场窗口搬进控制中心的
  插件页」——界面、目录与安装、配置编辑器三项从未被列入，于是一起丢了。

丢的代价有多大，看清单就知道：`market/` 里四个插件中，**三个（voice / qq / bilibili）
是 `panel=false`、`surface=[]`、`background=true`**，它们的 `entry` 只可能由市场程序以
`surface.slot = "workshop"` 挂起来。控制中心不挂，它们就**一个可见界面都没有**，只剩
桌宠里那一个看不见的后台帧。

桥与文件服务这一侧没有坏：`tools/plugin-market/main.go` 把 `AssetServer.Handler` 指向
app，`pluginhost.go` 里有 `ServePluginAsset` / `Market.PluginRequest` / 本程序的能力表。
缺的只有前端那一层。

## 做什么

**插件页 = 主从布局：左边是插件列表，右边是这个插件自己的界面（沙箱 iframe）。**
另给一个「展开」把界面铺满整页内容区——它不是第二个窗口，只是把左栏收起来。

同步把另外三项补回来（否则「插件页」只是四分之一页）：市场目录与安装/更新/卸载、
配置编辑器、`row.Error` / `Warnings()`。

## 为什么不是「详情另开一个窗口」

1. **Wails v2 没有多窗口 API。** 在 `wails v2.11.0` 的模块里核过：`pkg/runtime/window.go`
   只操作唯一那个窗口，全模块没有 `NewWebviewWindow` 一类入口。要真开第二个 OS 窗口只有
   两条路：第三个 Wails 程序（`openspec/changes/ui-rebuild/design.md` 已明确否掉：第三条
   构建链、第三个 CI job、第三套门禁，收益为零），或者自己用 `go-webview2` 起第二个
   WebView2 窗口——那样注入的 runtime、`/plugin-asset` 路由与 `PluginRequest` 通道都得
   自己重做一遍，等于把宿主层抄第二份。
2. **插件协议本来就是按「哪个程序挂我」分支的**。`market/plugins/com.shixiansi.plugin-voice/`
   的 `style.css` 自己写着「这就是为什么工坊由市场的 980px 窗口托管，而不是桌宠的 380px
   面板」。控制中心就是那个 workshop 宿主。
3. **纯列表会把前面七轮打磨掉的东西扔了**。`docs/plugin-market.md` §2.3 的结论是
   「权限＝贴按钮的浮层、设置＝弹窗、**界面＝面板本身**」。只有主从布局能让「边看权限边看
   界面」成立；「列表只列表、详情另开窗」做不到，而且要付出上面第 1 条的全部代价。

## 明确不做

- **不新增权限，不改 Go 侧的闸门与能力表。** 这次的缺口在前端。
- **不做第二个窗口**（理由见上）。
- **不接受插件给坐标/尺寸**（与 `pet.surface` 的既有约定一致：几何归宿主，插件只声明
  要哪些块可见）。
- 不把桌宠那套「窗口区域 / 圆角 / 半透明」的纪律带进控制中心：这是一个普通带框窗口，
  `SetWindowRgn` 不参与，插件文档在这里可以正常用阴影。

## 影响的不变量

- `AGENTS.md §3.3`：帧仍然是 `<iframe sandbox="allow-scripts">`，**永不**加
  `allow-same-origin`；唯一通道是 `postMessage`，只接受本文件自己创建的 iframe；
  前端不判断权限，每个请求都经 `Market.PluginRequest`。
- `AGENTS.md §3.1` 的**第 7 处**（市场侧权限文案 `PERMISSION_LABELS`）现在指的是
  `tools/plugin-market/frontend/src/main.js`——那个文件已经不出货。这一处要跟着搬家，
  否则「加一项权限要同步七处」会指向一个死文件。

## 验证计划

- `cd tools/plugin-market/cc && npm run check && npm run build && npm run smoke`：
  smoke 里新增「选中插件后挂的是一个 `sandbox="allow-scripts"` 的帧」「停用的插件不挂帧
  且说出原因」「manifest 坏掉的行点开后说出原因」。
- 真机：装 voice 插件 → 插件页选中 → 工坊出现、可改开关；停用 → 帧消失；切主题 → 帧跟随
  而不重挂（`?theme=` 管首帧、`theme` 消息管切换）。
- 展开：980×700 窗口下内容区 ≥ 760px，语音工坊 `240px 1fr` 两列生效
  （它的单列断点是 720px）。

## 顺序

1. `openspec` 三件套（本文件）。
2. 前端：`cc/src/lib/plugin-frame.ts`（从 `frontend/src/plugin-ui.js` 移植）+ 插件页主从布局。
3. 补齐目录/安装/配置（另一次提交，见 tasks.md 阶段 3）。
4. 门禁与文档同步：`check-frontend.cjs` 里钉在旧市场 DOM 上的断言重新指向 `cc/src/**`
   或退役；`docs/plugin-market.md` 增一节；`AGENTS.md §3.1` 第 7 处改指向。
