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

Svelte 5 runes，没有路由库、没有 UI 库：六个页面就是一个数组加一个 `$state`。