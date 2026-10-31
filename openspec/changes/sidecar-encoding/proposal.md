# proposal: sidecar 脚本的编码，和一份能读的日志

## 要解决什么

2026-10-31 的 `pet.log` 里有一类报错，看了一分钟也没读出个所以然：

```
WARN sidecar com.shixiansi.plugin-media-sync ����ʽ������а�������ı�ǡ�)����
WARN sidecar com.shixiansi.plugin-pet-sense + FullyQualifiedErrorId : UnexpectedToken
WARN sidecar com.shixiansi.plugin-clipask    + while ($true) {
WARN plugin com.shixiansi.plugin-pet-sense outbox/system… 一直是空的：辅助脚本可能没起来
```

三件事叠在一起：

1. **乱码**：宿主的 sidecar 日志把子进程的字节当 UTF-8 读。Windows PowerShell 5.1 写的是
   本机 ANSI 代码页（这台机器是 GBK），于是每条中文都成了 `����`——而这正是日志存在的目的。
2. **一行就能说清的根因，当时没人说**：Windows PowerShell 5.1 用本机 ANSI 代码页解码**没有
   BOM** 的 `.ps1`。含中文注释的 UTF-8 脚本会被按 GBK 读，一个多字节字符能吃掉它后面的引号或
   花括号，脚本一启动就 `Missing closing '}'`，而报的行号指向一行完全正常的代码。
3. **插件侧的脚本真的坏了**：五个带 `sidecar` 的插件里，四个脚本没有 BOM（pet-sense /
   media-sync / privacy-guard / screen-vision），第五个（clipask）**少一个右花括号**——文件在
   `catch` 里就结束了。所以五个辅助脚本从来没有产出过一个字节，插件只能猜"可能没起来"。

## 做什么

**宿主（本仓）**

1. 启动 `sidecar` 之前先看一眼脚本：`.ps1` + 没有 BOM + 含非 ASCII → 写一行 WARN，把原因和修法
   写清楚（`sidecarScriptEncoding`）。它**不阻止**启动：宿主不该改写自己安装的别人的包，而一个
   碰巧还能解析的脚本仍然应当跑起来。
2. 子进程的 stdout/stderr 按"先认 UTF-8、否则按本机 ANSI 代码页转换"解码
   （`desktop.DecodeChildOutput`），失败时保留原字节。乱码不再出现在日志里。

**插件仓（`market/`）**

3. 四个脚本加 UTF-8 BOM（screen-vision 的脚本本来能解析，但它的中文**字符串值**在 GBK 解码下
   是错的，所以一并加）；clipask 的脚本补上缺的那个 `}`（`while` 循环的收尾）。
4. 契约补齐：`plugin-dev.md` §4 的 `sidecar` 表加一行"`.ps1` 必须带 BOM 或纯 ASCII"；
   `plugin-check.mjs` 终于开始校验 `sidecar`（四条与宿主 `validateSidecar` 一一对应：
   包内相对路径、必须写进 `assets`、必须申请 `process`、必须声明 `background`），并新增
   `sidecar-encoding`。
   顺手修掉一个**误报**：`checkPackage` 的"引用了但没写进 assets"读 `info.assets`，而
   `checkManifest` 从来没有返回它——于是**声明正确**的 `ui.css` 也会被判 FAIL。现在它返回了。

## 明确不做

- **不自动改写已装插件的文件**（给脚本补 BOM 这种"帮忙"会让包与磁盘上的字节不再相同，
  而更新链是按包走的）。宿主只负责说清楚，修不修是作者/用户的事。
- **不在安装时拒绝**：一个没 BOM 的脚本可能照样能解析（screen-vision 就是），拒绝它等于
  用"我猜它会坏"否掉一个能用的插件。安全检查放在启动那一刻，用日志说。
- **不动 `process.launch` 的 `stderr`**：那条路径故意是 `/dev/null`（插件自己的程序不能阻塞宿主），
  这次的解码与它无关。

## 影响的不变量

- **AGENTS.md §3.8**（日志一直在写、有等级、没有正文）：这条更结实了——以前"证据在，
  但读不出来"等于没有证据。解码顺序写死在 `DecodeChildOutput` 里，并有测试钉住。
- **AGENTS.md §3.9 第 2 条**（辅助脚本交给宿主托管）：sidecar 的契约多了一条
  （脚本编码），补进了 AGENTS.md 与插件手册。
- 门禁：`internal/desktop` 多一个 Windows-only 单测（GBK 字节 → 中文），`app_plugin_sidecarlog`
  多一个纯函数用例（四种脚本形状）。

## 验证

- `go vet ./... && go test ./... -count=1`（根 module）：全绿，含
  `TestDecodeChildOutputReadsTheCodePageAPowerShellWrites` 与
  `TestSidecarScriptEncodingNamesTheTrap`。
- `powershell.exe -NoProfile -Command "[Parser]::ParseFile(...)"` 对着五个脚本逐个跑：
  修之前三个 + clipask 报错，修之后五个全 `PARSE OK`。
- `node market/docs/plugin-testkit/plugin-check.mjs <36 个插件> --only=manifest,package`：
  0 FAIL；把一个脚本的 BOM 去掉后**立刻**报 `sidecar-encoding`（已实测）。
- `cd market && npx vitest run`：1468 passed（插件自己的用例不受 BOM/花括号影响）。
- `node .dev/check-frontend.cjs`、`cd frontend && npx vitest run`、`tools/build-all.ps1`。
