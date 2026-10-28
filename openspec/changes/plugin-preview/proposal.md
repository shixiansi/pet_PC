# proposal: 插件界面预览与自动排版检查（plugin-preview）

## 要解决什么

2026-10-28 一天之内，番茄钟的浮层面卡片连翻五次车，症状依次是「按钮被切」→「只剩数字」→
「全黑」。五次都是同一个类别的错误：**元素在声明的矩形里放不下，或者被父元素的 `hidden`
连坐**。五次都不是靠测试发现的，而是靠用户截图。

原因很清楚，而且是结构性的：

| 现有手段 | 能看见什么 | 看不见什么 |
| --- | --- | --- |
| `market/tests/*.test.js`（jsdom） | DOM 结构、`hidden` 属性、桥接消息 | **排版**：jsdom 没有布局引擎，`getBoundingClientRect()` 全是 0 |
| `docs/plugin-testkit/plugin-check.mjs` | manifest、包、静态红线、协议行为 | 同上 |
| 真机 | 一切 | 需要用户装插件、点按钮、截图、再描述给我 |
| 宿主日志 | 调用与拒绝 | 页面长什么样 |

所以"这张卡片现在长什么样、按钮在不在可视区里"这个问题，在整条链路上**没有任何一处能回答**。

## 做什么

给插件作者（以及我）一个**能在浏览器里跑起来、能点、能截图、能断言排版**的平台：

1. **`preview.html`** —— 一个本地页面，把插件的 `entry` 装进 `iframe`，尺寸**正好**是它声明的
   矩形；清单声明 `background: true` 时**再挂一个隐藏的常驻帧**（番茄钟的状态机在那里，只挂一个
   帧就会让"点按钮"看起来毫无反应）；实现与真宿主**同一套桥接协议**的假宿主（与 `plugin-check.mjs`
   共用 `fake-host.mjs`）；旁边一栏实时显示两个帧的每条消息。
2. **`preview.mjs`** —— 一个 Node 脚本：起静态服务器（零依赖）、打印地址；用**系统已装的**
   Edge/Chrome 无头跑一遍做**排版断言**与**截图**（不下载任何浏览器，见 design 第 1 节）。
3. **排版断言**（这是真正的价值）：任何可见元素的
   `getBoundingClientRect().bottom > 卡片高度` 或 `scrollHeight > clientHeight + 1`，都算失败——
   也就是"被裁掉了"。上面五次翻车里，前四次会被这一条直接抓住。
4. **状态直达**：`--state gate` 一条命令把插件推到"等你回答"那一屏（假宿主按协议回一个停住的
   state），不必手点五分钟等它自然发生。

## 明确不做

- **不做成控制中心里的一个页面。** 那需要重建市场 exe、把 Svelte 的 `plugin-frame` 拖进来，
  而且换个 rect / 换 granted 都得改代码。独立页面一行 URL 就能换。
- **不替代 `plugin-check.mjs` 的协议检查。** 那个跑在 jsdom 里、无需浏览器；这个补的是"看得见"
  的那一半。两者共用同一张能力表与假宿主逻辑。
- **不做真机自动化**（Wails + WebView2 的驱动）。真机仍需人看一眼，但"卡片放不放得下"这类
  问题不再需要人看。
- **不引入前端框架**，`preview.html` 就是一个文件。

## 验收标准

1. `node preview.mjs <pomodoro 2.3.4 的源码目录> --check` **失败**，并指出是哪个元素的
   `bottom` 超出了卡片（已知坏样本）。
2. 同一条命令对 **2.3.9** **通过**。
3. `--shot out.png --state gate` 出一张能看清"问题 + 输入框 + 确定/跳过"的图——人和 agent
   都能直接看（agent 用 `read_image`）。
4. 全流程**不需要 Wails、不需要构建宿主**：只有 Node + 一个已安装的浏览器（Edge 或 Chrome）。
5. 没有 Edge/Chrome 时，`preview.mjs` 仍然能起服务器并打印地址（人工用浏览器打开），
   `--check` / `--shot` 报 **SKIP 并说明原因**，绝不假装通过。
6. **点一下能看见效果**：`--shot after.png --state gate --click "#ask-ok"` 出的图里，那一屏已经从
   「等你回答」变成「休息在跑」。这条是用户提出「点确认和跳过没有反应」之后补的——第一版只挂一个
   帧，点下去屏幕上什么都不会变，工具却报"布局没问题"。

## 影响

| 位置 | 变化 |
| --- | --- |
| `market/docs/plugin-testkit/` | 新增 `preview.html` + `preview.mjs` + `fake-host.mjs`（假宿主，`plugin-check.mjs` 也改成用它）；README 加一节 |
| `market/tests/fixtures/overflow-card/` | 一个故意越界的最小插件，作常设反向样本（tasks D2） |
| `market/package.json` | **不动**：不需要任何新依赖（design 第 1 节的修正） |
| `market/docs/plugin-dev.md` §12 | 自检清单加一句"先预览，再装" |
| `AGENTS.md §3.3` | 契约文档与工具的清单里加这个预览器，并写明它是开发工具、不是门禁 |
