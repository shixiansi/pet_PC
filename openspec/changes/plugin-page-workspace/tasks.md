# tasks: 插件页把插件界面接回来

## 阶段 1｜宿主层（前端，Go 一行不改）

- [x] `cc/src/lib/plugin-frame.ts`：从 `frontend/src/plugin-ui.js` 移植沙箱帧宿主
      （`sandbox="allow-scripts"`、`postMessage` 只认自己那个 `contentWindow`、
      `ready → init`、`request → Market.PluginRequest`、`?theme=` + `theme` 消息双通道、
      `surface: { kind:'panel', slot:'workshop' }`）
- [x] `cc/src/lib/bridge.ts`：补 `PluginRequest` / `SelectedID` / `RememberSelection`
      （含 DEV 假实现，且假实现只回答本程序真的有的方法）
- [x] `cc/src/components/PluginFrame.svelte`：稳定挂载点 + 字符串键的 `$effect`，
      **不让 Svelte 拥有 iframe**
- [x] `cc/src/pages/Plugins.svelte`：列表 + 详情主从布局，挂载门控照抄旧语义
      （已装 && 已启用 && 有 entry），四种不挂的原因各说一句话

**验收**：`cd tools/plugin-market/cc && npm run check && npm run build && npm run smoke`；
真机装 voice 插件后工坊出现、停用即消失。

> **真机第一次跑就红了一条，记在这里当第 5 条约定**：`postMessage` 的 `init` 载荷里带了
> `$state` 代理过的数组（`Manifest.permissions` / `Granted`），抛
> `Uncaught DataCloneError: … could not be cloned.`——异常在**消息监听器里**，所以页面看起来
> 完全正常、插件停在「等待宿主初始化…」。修法：`mountPlugin` 入口复制成纯数据 + `post()` 里
> 说明性的 `try/catch`；桩数据摊平成纯 JSON（原来是 `createFrom` 造的**类实例**，而 Svelte
> 不代理类实例，所以浏览器与 jsdom 都复现不出来）。`render-smoke.mjs` 新增 6 条握手断言，
> 其中「init 载荷能过 `structuredClone`」**做过反向验证**：把复制拿掉它就红。

> **本轮实测**：`npm run check` 0 errors / 0 warnings；`npm run build` 通过；
> `npm run smoke` **36/36 PASS**（含 sandbox / 资产 URL / 展开不重挂帧 / 停用不挂帧 /
> 坏 manifest 说原因）。
>
> **真机没验成**：本会话里 `PetPCPluginMarket.exe` 创建 WebView2 控制器一律
> `800700aa`（"resource in use"，`docs/plugin-market.md` §7 记过同一个错误码是环境限制）。
> 所以「真实的 WebView2 里选中语音插件、看到两栏工坊」这一条**仍然没有被任何真实浏览器
> 驱动验证过**，和 README 里那条已知限制是同一条。下次在有交互桌面的会话里补。

## 阶段 2｜布局

- [x] 详情区：身份行（名字 / id·版本 / 启用开关 / 权限 / 展开）+ 界面占满剩余高度
- [x] 权限改成 `<details>` + 绝对定位面板（不占布局、内容在 DOM 里，见 design 第四节）
- [x] 「展开」收起左栏，界面回到 764px（> 语音工坊 720px 的单列断点），`Esc` 收起
- [x] 换插件收起展开与权限面板；勾权限用整份 `Grant`（不发明「增量授权」）
- [x] **配置编辑器**（用户报「插件的设置没了」）：弹窗 + 五种控件
      （`text`/`number`/`bool`/`secret`/`select[+multiple]`），**只有声明了 `config[]` 的插件
      才有入口**；`secret` 永远不回填（留空＝保持不变）、布尔总是发送、空字段落回默认值；
      保存成功关窗并刷新，失败保持打开
- [ ] 目录与安装 / 更新 / 卸载——阶段 3

**验收**：980×700 下展开后语音工坊两列；`smoke` 全绿。

> **本轮实测**（`scratch/layout-cc.cjs`：把开发态包喂给无头 Edge——与 WebView2 同一引擎类，
> 手法同 `.dev/layout-report.cjs`——然后点开插件页、点选插件、开权限浮层、展开，读回几何）：
>
> | | 980×700 窗口 | 720×520（最小） |
> |---|---|---|
> | 列表 / 详情 | 232 / 496 | 180 / 288（`@media` 到 860px 以下收窄） |
> | 帧（列表在时） | 494×394 | 286×214 |
> | 帧 = 详情高度 − 边框 | 是 | 是 |
> | 权限浮层把帧推走 | 否（y141..537 前后一致） | 否 |
> | 展开后帧宽 | **740**（> 语音工坊 720 断点） | 480 |
> | 展开是否重挂帧 | 否 | 否 |
> | 有元素越过窗口右边缘 | 没有 | 没有 |
>
> **探针当场抓到一个真 bug**：`min-width: auto` 让 iframe 的默认 300×150 变成它的
> min-content 宽度，于是在 720 的窗口里帧**伸出自己的列 110px**、一直顶到窗口边——
> 默认尺寸下完全看不见。修法是三层都给 `min-width: 0`（`.detail` / `.frame-area` /
> 帧元素的内联样式），并把「帧不许伸出自己的列」写成探针里的一条与尺寸无关的断言。

## 阶段 3｜把插件管理另外三块补回来

- [ ] 市场目录 + 安装 / 更新 / 卸载（`Catalogue` / `Install` / `Update` / `Uninstall` /
      `SaveMarketURL` 都在 `Market.d.ts` 里，只是 cc 没用）
- [x] 配置编辑器（`GetConfig` / `SaveConfig`）——已随阶段 2 交付
- [ ] `Warnings()` 与安装来源徽标

**验收**：全新数据目录里从市场装一个插件、改配置、卸载，走一遍。

## 阶段 4｜门禁与文档

- [x] `cc/scripts/render-smoke.mjs`：插件页断言改成「列表 → 选中 → 详情」，新增
      sandbox / 资产 URL / 停用与坏 manifest 不挂帧三条
- [x] `.dev/check-frontend.cjs`：托管插件界面的三条断言 + 权限文案那条 + 主题那条
      **重新指向 `cc/src/**`**（对着不再构建的文件报 PASS 比没有门禁更糟）。
      剩下那批钉在旧市场 DOM 上的（`panel-interface` / `ico` sprite / `app-dot` /
      地址行…）属于 `ui-rebuild` 第五节的分诊表，本次没动
- [ ] `.dev/verify-plugin-market.ps1`：给控制中心加一个深链接（`PETPC_CC_PAGE=plugins`
      或 `#/plugins`），让像素门禁能落到插件页而不是永远停在开始页
- [ ] **把布局探针收进门禁**：`scratch/layout-cc.cjs` 已经能量 15 条（含那条
      `min-width` 的回归），但它现在在 gitignore 的 `scratch/` 里，换台机器就没了。
      该搬去 `.dev/`（同时加 `.gitignore` 白名单、`.ps1` 那条 BOM 规则不适用但
      `check-all.ps1` 要挂上去）。**没有本次就做**，因为 `check-all.ps1` 此刻正被另一处
      改动编辑着，两边同时改同一个文件只会互相打架
- [x] `docs/plugin-market.md` §2.8 + README（入口那行 / 工坊那节 / 文件表）
- [x] **交付这一步不能靠记性**：插件页左上角留 `已装插件 · v5` 这个**构建标记**
      （旧 JS 页面当年的 `· v4` 就是这个用途，Svelte 重写时丢了）；
      `docs/plugin-market.md` §6.1 与 `AGENTS.md` §7 各补一条——
      **重建桌宠不会重建市场程序**，而控制中心的界面是编译期嵌进它自己的 exe 的。
      2026-10-27 的「插件页面又没有了」就是这一条：源码与 `frontend/dist` 都换了，
      用户打开的仍是 0:00 的二进制
- [x] `AGENTS.md §3.3` 补控制中心那一份帧宿主与「iframe 归模块不归 Svelte」，
      §3.1 第 7 处改指向 `cc/src/lib/bridge.ts` 的 dev 桩 `LABELS`
