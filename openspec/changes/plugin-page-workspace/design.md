# design: 插件页的宿主层（iframe 归谁、什么时候重挂、宽度从哪来）

## 一、帧归前端模块，不归 Svelte

`cc/src/lib/plugin-frame.ts` 是从 `tools/plugin-market/frontend/src/plugin-ui.js` 移植的，
**不是重写**：那份文件里每一行注释都是花钱买来的，尤其是三处：

1. `granted`: `inst.Granted` 是**大写 G**（Go 字段名，`plugins.Installed` 没有 json tag）。
   写成 `inst.granted` 会静默变成 `undefined`，`|| []` 再把它变成空数组——插件被告知
   「用户什么都没授权」，界面照常渲染、每个控件都变灰，什么都不报错。
2. `theme` 有**两条通道**：`?theme=` 挂在帧 URL 上（首帧之前就生效，不闪），加一条
   `theme` 消息（帧已经在跑时切换，不重挂）。重挂会丢掉输入到一半的表单与滚动位置。
3. 复用比较必须含 **version**：更新一个插件保留 id、只换文件，只比 id 会让上一个版本的
   文档继续跑着——用户于是「更新了却还是旧界面」。

**关键约束：iframe 元素不能由 Svelte 的模板拥有。** 模板里的条件块、`{#key}`、列表 diff
都会重建 DOM 节点，而重建一个 iframe 就是销毁那个正在按用户权限运行的文档。所以：

- Svelte 只提供一个**稳定的挂载点** `<div bind:this>`；
- `plugin-frame.ts` 在 `$effect` 里**命令式**创建/销毁 iframe；
- 帧的样式写在创建它的那个模块里（`display:block; width:100%; height:100%; border:0`），
  因为元素不是 Svelte 画的，Svelte 的 scoped CSS 管不到它。

## 二、什么时候重挂：一个字符串键

`$effect` 的依赖必须是**一个字符串**，不是 `inst` 对象。原因：`refresh()` 每次都给出新的
行对象，任何直接读 `inst.*` 的 effect 都会在「勾了一个权限」之后重跑——也就是把工坊拆了。

```ts
const key = $derived.by(() => {
  const id = inst?.ID ?? '';
  const entry = inst?.Manifest?.entry ?? '';
  if (!id || !entry || !inst?.Enabled) return '';      // 不挂
  return [id, inst.Version, entry, grantedSorted.join(',')].join('|');
});
```

`$derived` 只在**值**变化时通知下游，所以刷新列表、切标签、窗格重绘都不会重挂。
其余字段（permissions 列表、granted）在 effect 里用 `untrack(() => …)` 读，只作为 `init`
的载荷。

**granted 进键是有意的，代价写在明处**：换授权会重启插件界面。理由有两条——

- 不发第二次 `init`：四个随包插件的 `onInit` 都会 `applyView` + 重新拉配置，其中
  qq/bilibili 的顶层还建了 interval 与轮询，多一次 init 是「不知道会不会重复注册」的赌；
  而 remount 是插件本来就支持的路径（换插件、停用再启用都会走它）。
- 授权是一个**人对插件的决定**，让插件带着新权限重新开始，比让它继续跑着旧授权诚实。
  界面上一句话说明这件事（权限浮层里「改完界面会按新权限重启」），不靠用户猜。

## 三、宽度：展开是**收起左栏**，不是开窗口

980×700 的窗口，减去 168 的侧栏与 48 的内边距，内容区 764px。主从布局里左栏再吃掉
240px，详情只剩 ~510px——而语音工坊的两列布局在 720px 就塌成单列
（`market/plugins/com.shixiansi.plugin-voice/style.css` 的 `@media (max-width: 720px)`）。

所以「展开」的作用是把左栏收起来，让内容区回到 764px（> 720）。**侧栏不收起**：再省下
168px 也能过断点，而那需要控制一次跨组件的布局（`App.svelte` 的 rail 由页面反向控制），
换来的是 168px 与一个全局状态。值不值：不值。这条写在这里，免得下一轮有人「顺手」加。

展开只改 CSS 网格列，iframe 原地不动（第二遍：重挂会销毁文档）。

## 四、权限是 `<details>` 上挂的绝对定位面板

旧结论是「权限＝贴按钮的浮层，不占布局」（`docs/plugin-market.md` §2.3）——一个展开就把
工坊往下挤的面板是要不得的。这里用 `<details>` + `position: absolute`：

- 关闭时**不占布局**（`<details>` 的关闭内容是 UA 隐藏的），打开时是浮层；
- 内容**始终在 DOM 里**，所以 `render-smoke.mjs` 能对着 `textContent` 断言「权限文案来自
  宿主」——不需要模拟点击（jsdom 也没有 `details` 的点击开关行为）；
- 不做 `position: fixed` + `getBoundingClientRect()` 定位：那要测量、要夹窗口、要在 jsdom
  （rect 全是 0）里退化。这一层浮在详情头部下方，离 `main` 的溢出边还远。

## 五、门禁怎么跟着走

- `cc/scripts/render-smoke.mjs`：插件页的断言从「一整页平铺」改成「列表 → 选中 → 详情」。
  新增三条真正在守不变量：帧的 `sandbox` 恰好是 `allow-scripts`（且不含
  `allow-same-origin`）、`src` 是 `/plugin-asset/<id>/<entry>?theme=…`、停用与 manifest
  坏掉时**不挂帧**且说出原因。
- `.dev/check-frontend.cjs`：它有一大片断言钉在**已不出货**的
  `tools/plugin-market/frontend/{index.html,src/main.js,src/style.css}` 上（`panel-interface`、
  `perm-list`、`ico` sprite、`app-dot`、`PERMISSION_LABELS`…）。给死代码发证书比没有门禁更糟，
  所以按 `ui-rebuild/design.md` 第五节的分诊表重新指向 `cc/src/**` 或退役。
- `AGENTS.md §3.1` 第 7 处跟着搬：权限文案的权威位置从旧市场的 `main.js` 挪到
  `cc/src/lib/bridge.ts` 的开发桩 + 真实的 `Market.PermissionLabels()`（生产界面只读后者）。
