# 任务与验收记录（2026-11-03）

每条都写"跑什么、看到什么"。**没跑的不打勾**——这一列是给人复跑用的，不是给完成度打分用的。

## A. 宿主机制

- [x] `gofmt -w` 全仓（30 个文件）→ `gofmt -l .` 在排除 `.dev/` 后为空
- [x] `go vet ./...` 与 `go test ./... -count=1` → 全 ok（`PetPC` / `internal/*` 八个包）
- [x] `cd tools/plugin-market && go vet ./... && go test ./...` → ok
- [x] `validateSurface` 要求 `pet.surface`：`go test ./internal/plugins/ -run TestManifestSurface -count=1`
- [x] `TestEveryPublishedManifestParses`（它会读 `../../market/plugins/*`）→ 抓到了两个真问题：
      `weekly-review` 声明了 surface 没申请权限、`finance-board` 的 config label 超过 64 字符
- [x] `TestOpenPluginSurfaceRefusedWithoutPermission` 改成"先装（带 pet.surface）再撤授权"——
      它测的是**打开时**的授权闸门，而不是安装期校验
- [x] `node .dev/check-frontend.cjs` → `ALL FRONTEND CHECKS PASSED (153)`
- [x] `AGENTS.md` §3.9 的 `storage.pressure` 漂移；`app_plugins.go` 的三个死字段与两处过期注释

## B. 市场侧：12 个新插件 + 1 个存量

每个插件的 Definition of Done（九条里的前六条）：

- [x] `node docs/plugin-testkit/plugin-check.mjs plugins/com.shixiansi.plugin-<id>` → **12/12 都是 0 FAIL**
      （个别 1 WARN `surface-fixed-height`，是"卡片刻意定高"的诚实标记）
- [x] `npx vitest run` 全仓 → **61 files / 1933 tests passed, 1 skipped**
- [x] `preview.mjs --check`：7 个声明浮层面的全部 PASS（"全部元素在卡片之内"）；
      5 个只有插件页的 SKIP（排版断言对可滚动的整页没有意义，这是工具的既定口径）
- [x] `preview.mjs --shot`：12 张卡片 / 页面截图 + 一张 49 格的画廊图（`scratch/sprint/shots/`）
- [x] manifest 申请的权限 == 实际调用的集合：四个泳道在返工时各补了一条断言钉住它
- [x] `todo` 1.2.3 → 1.3.0：`serves: ["stats", "add_task", "toggle", "list"]`，
      原有 12 条断言一条未改，新增 11 条（23 passed）

## C. 发布

- [x] `pwsh -File market/tools/package.ps1` → 49 个包 + `index.json` 回写 + 内置样例子集刷新（11 个包）
- [x] `node market/tools/check-catalogue.mjs` → `OK：49 个插件、49 个条目、主题包 2 个、包与摘要全部对得上`
- [x] `node market/tools/render-pages.mjs` → `index.html` / `manage.html` 各 49 个插件
- [x] `go test ./internal/plugins/ -run TestBuiltinSample -count=1` → ok（兜底源与市场逐字节一致）

## D. 截图阶段抓到的四个真问题（这一轮最值钱的部分）

1. **三张卡片是空白的。** `calendar` / `anniversary` / `finance-board` 都用
   `mountInfo.slot === 'surface'` 判断"我是不是浮层面"。而浮层面的 `slot` 可能根本没有，
   也可能带着**一个面板槽名**（例如 `pomodoro`）——于是整张卡片停在 `display:none` 里，
   矩形裁出来了但里面什么都没有。jsdom 全绿，因为它们的假 `init` 多带了一个 `slot`。
   改成 `kind === 'surface'`，并在 `plugin-dev.md` §5.2 写清这条。
2. **`anniversary` 的 `maxH: 176` 超过窗口只给得起的 172px**（`dy` + `h` + 宠物偏移算出来的天花板），
   多出来的 4px 永远不会生效 → 改成 172。`quicksave` 同样的问题（180 → 152）。
3. **`github-notify` 声明了 `pet.badge` / `pet.bubble` 却没有桌宠侧的帧**（只有 `panel:true`）——
   徽标与气泡在真机上永不执行。加 `background: true`，并让**只有常驻帧**排轮询定时器
   （两个帧各排一个会让同一份 GitHub 配额被问两次、同一个 storage 键两个写者）。
4. **`window.postMessage` 而不是 `window.parent.postMessage`** —— `weekly-review` 的卡片是空白，
   页面日志里连一条 `→ request` 都没有：出站消息的收件人写成了"被调用的那个 window"，
   也就是它自己，宿主一条都收不到、`init` 永远不来。**jsdom 抓不到**（假宿主与插件文档住在同一个
   window，那个写法在测试里正好是对的）。同一行模板在**三个已经发布的插件**里也存在：
   `diary-writer` / `knowledge-base` / `agent-worker`——它们的界面在真机上一直停在
   「等待宿主初始化…」。四个都改了，`weekly-review` 1.0.0 → 1.0.1、`diary-writer` 0.1.1 → 0.1.2、
   `knowledge-base` 0.1.1 → 0.1.2、`agent-worker` 0.1.2 → 0.1.3 之后重新打包；
   `plugin-check.mjs` 加静态检查 `host-channel-target`（对着旧代码验证过会 FAIL），
   手册 §5.3 铁律加了一条（原第 2–6 条顺延）。

## D2. 一次"全绿但真机是坏的"的账

四条里没有一条能让 jsdom 变红。它们的共同点是**只有真浏览器 + 真拓扑才存在的差别**：
布局（`getBoundingClientRect` 恒 0）、`slot` 的真实取值、帧与父窗口是两个 window。
所以 `preview.mjs` 不是"锦上添花的开发工具"——这一轮它抓到的四个问题里，**三个是发布级别的**
（两张卡片打不开、三个已发布插件的界面从来没初始化过）。这也是"截图验证"必须紧跟打包、
而不是可选项的原因。

## C2. 版本与重新打包（截图修复之后）

- `weekly-review` 1.0.0 → **1.0.1**、`diary-writer` 0.1.1 → **0.1.2**、
  `knowledge-base` 0.1.1 → **0.1.2**、`agent-worker` 0.1.2 → **0.1.3**
- 重跑 `package.ps1` → `check-catalogue` 再绿（49 条目、摘要全对）；
  删掉 `dist/com.shixiansi.plugin-weekly-review-1.0.0.zip`（当天自己产生、从未发布过的孤儿）
- 兜底源（`internal/plugins/sample`）不受影响：它只刷新已有同名同版本的包，不新增文件

## E. 复跑全部验收

```powershell
# 宿主
cd F:\dev\pet_pc
gofmt -l .                                   # 应为空（.dev/ 里的模块缓存不算）
go vet ./... ; go test ./... -count=1
cd tools\plugin-market ; go vet ./... ; go test ./... -count=1 ; cd ..\..

# 市场
cd market
node docs/plugin-testkit/plugin-check.mjs plugins/*   # 每个都 0 FAIL
npx vitest run                                        # 1933 passed
pwsh -File tools/package.ps1                          # 需要 pwsh（5.1 的 param 默认值没有 $PSScriptRoot）
node tools/check-catalogue.mjs
node tools/render-pages.mjs

# 排版与截图（需要一台装着 Edge/Chrome 的机器）
pwsh -File F:\dev\pet_pc\scratch\sprint\verify-all.ps1
```
