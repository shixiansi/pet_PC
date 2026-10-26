# 控制中心的 Svelte 应用（在建）

这个目录是 `openspec/changes/ui-rebuild` 阶段 2 的产物：设置界面从桌宠窗口里搬出来，
成为它自己的程序。

## 为什么先是新目录，而不是直接替换

替换 `../frontend/` 的那一刻，`.dev/check-frontend.cjs` 里读 `plugin-ui.js` 的 **7 项
断言**会红。分诊清单（`openspec/changes/ui-rebuild/check-inventory.md`，桶 `3-market`）
把处置定下来之后才做替换，所以在那之前新应用长在这里，一行旧文件不碰。

## 为什么保留 `PetPCPluginMarket.exe` 这个名字

改名要动 8 个代码/门禁文件，其中两个是按 exe 文件名启动的**真机像素门禁**——在本机
（起不来 WebView2 控制器）无法验证。改名的用户可见价值接近零，所以它推迟到前端落地
之后单独做，并且必须由人跑一遍那两道门禁。

## 命令

    npm ci && npm run build      # 产物在 dist/
    npm run check                # svelte-check，类型与可访问性
    npm run smoke                # 用 jsdom 真的把页面渲染一遍，断言它打印出了什么

`npm run smoke` 用**开发模式**的构建（`NODE_ENV=development`，即 `import.meta.env.DEV === true`），
所以回答的是「`npm run dev` 里那套假数据会渲染成什么」；`jsdom` 借的是 `../frontend/node_modules`
里 vitest 那份，借不到就明说退出。

## 为什么假数据必须用宿主的形状

`lib/bridge.ts` 的类型直接来自 `../frontend/wailsjs/go/models.ts`（`wails build` 生成），
不在这里手写一份。曾经手写过 camelCase 的那份：宿主发的 `plugins.Installed` 没有 json tag，
字段是 `ID` / `Enabled` / `Manifest.Name`，于是页面上四行的所有字段都是 `undefined`，
而浏览器里一切正常——**假数据同意那个错类型**。现在字段名写错是 `npm run check` 的错误，
`npm run smoke` 再确认一遍渲染结果。

Svelte 5 runes，没有路由库、没有 UI 库：六个页面就是一个数组加一个 `$state`。