# tasks: 番茄钟状态机下沉

## B4 —— 已完成（逻辑搬迁）

- [x] 通读 `pomodoro.go`，确认它不引用 App / 窗口 / Wails
- [x] 量化搬迁半径：8 个文件、约 110 处引用
- [x] 确认只有 `.finished()` 需要升为导出（7 处调用）
- [x] 建 `internal/pomodoro/machine.go`（含包文档）
- [x] `pomodoro.go`（main）换成接缝（395 → 96 行）
- [x] 7 处 `.finished()` → `.Finished()`；14 处文档注释首词改导出名

## B4b —— 已完成（测试分家）

- [x] `external_test.go`（`package pomodoro_test`）从包外钉住导出面
- [x] `carry_test.go`（4 个用例）搬进新包
- [x] `machine_test.go`（20 个纯状态迁移 + `TestPhaseLabel`）搬进新包
- [x] main 的 `pomodoro_test.go`：582 → 76 行，只剩 `TestPomodoroViewCountdownMatchesSeconds`
- [x] 两个 fixture 在新包留副本（仍被 4 个留在 main 的测试文件使用）

## B4c —— 已完成（改调用点、删接缝）

- [x] 6 个文件、46 处调用点改成 `pomodoro.X`，并补 import
- [x] 删除接缝文件 `pomodoro.go`（main）
- [x] `package main` 里不再有状态迁移逻辑
- [x] 修掉因搬迁而失效的**按路径读文件**的门禁：`.dev/check-frontend.cjs` 里
      `goPomodoroEngine` 原本读 `pomodoro.go`，文件一搬就抛 ENOENT（整脚本无法运行，
      169 项检查全都不再执行）。改读 `internal/pomodoro/machine.go`，断言本身没变。
- [x] 修文档里两处指向已删除文件的说法：`README.md` 的项目结构清单、
      `docs/reminder-pomodoro.md` §15.1 的文件表（同时把小写函数名改成导出名）

### 这一批踩到并修掉的四个坑（都是机械替换的副产品）

1. **PowerShell 的 `-replace` 不区分大小写**：`\bskipPhase\(` 命中了 App 上的
   Wails 绑定方法 `SkipPhase`，`\bstop\(` 命中了 `ticker.Stop()`。前者会打断
   前端 `App.SkipPhase()`，后者让 `app_pomodoro.go` 编译不过。
   **教训：脚本化改名必须显式用 `[regex]::Replace` 或加 `(?-i)`。**
2. 我那段"插入 import"的写法把 `"PetPC/internal/store"` 覆盖掉了（6 个文件）
3. 导入组间空行丢失，`gofmt` 想把项目包排到标准库前面
4. 一个原本单行的函数因为类型名变长而超出 gofmt 的单行上限

### 实际验证

| 命令 | 结果 |
| --- | --- |
| `gofmt -l`（改过的 7 个路径） | 无输出 |
| `go vet ./...` | 退出码 0 |
| `go test ./...` | `PetPC` / `internal/llm` / `internal/plugins` / `internal/pomodoro` / `internal/store` 全 ok |
| `go test ./internal/pomodoro -v` | 25 个用例全 PASS |
| `node .dev/check-frontend.cjs` | ALL FRONTEND CHECKS PASSED (169) —— 含"两个前端的假桥接名单与 Go 的导出集合逐名相等"，所以 59 个 Wails 绑定一个没丢 |

`internal/desktop` 仍是那 9 个 `open Run key: Access is denied`（沙箱限制）。

### 遗留（本批不处理）

- `gofmt -l .` 会列出 6 个 `internal/plugins/*.go`：它们**不是** CRLF，是真的没格式化过。
  这是历史遗留，不属于本次范围；要做的话单独一批，且提交信息里要说明这是纯格式化。
- `.gitattributes` 没有给 Go 文件指定 `eol=lf`，而 `core.autocrlf=true`：新检出可能得到
  CRLF，那时 `gofmt -l` 会把所有 Go 文件标成未格式化。
