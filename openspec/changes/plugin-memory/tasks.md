# tasks: 插件的长期记忆

## 已完成

- [x] `internal/plugins/memory.go`：`ReadMemory` / `AppendMemory` + 快照 + 剪枝
- [x] 三条硬规则各有对应实现：只追加、写前快照、三处上限
- [x] 插件方法 `memory.read` / `memory.append`，挂 `storage` 权限
- [x] 工具 `memory_read` / `memory_append`（工具目录现在 7 个）
- [x] `internal/plugins/memory_test.go`：8 个用例

### 实际验证

| 命令 | 结果 |
| --- | --- |
| `gofmt -l`（四个改动文件） | 无输出 |
| `go build ./...` | 退出码 0 |
| `go vet ./...` | 退出码 0 |
| `go test ./internal/plugins -run 'TestAppendMemory\|TestMemoryHistory\|TestMemorySnapshot'` | 8 个用例全 PASS |
| `go test ./...` | `PetPC` / `internal/llm` / `internal/plugins` / `internal/pomodoro` / `internal/store` 全绿 |
| `node .dev/check-frontend.cjs` | 169 项全过 |

8 个用例各自钉住一条规则，不是凑数：

- 空目录读出空串（不是错误）
- 快照是**写入前**的那一版（断言它不含后一条）
- 空条目被拒
- 带换行的条目不增加行数，也伪造不出回溯日期
- 超长条目被截断并标记 `…`
- 满了**拒绝**且没有丢内容
- 历史版本会被剪到上限，且剪掉的是**最旧**的
- 同一秒内的两次追加产生两个不同的快照文件名

## 下一步（未开始）

- [ ] 面板/设置页里让用户能看到并编辑 `memory.md`（现在是纯文件，只有插件自己知道它在哪）
- [ ] 一个"整理记忆"的工具：模型可以把重复的条目合并（目前只能追加，长期会变啰嗦）
- [ ] `check-frontend.cjs` 加一条断言：`memory.append` 不做路径拼接（键名是固定的）
- [ ] 对话插件接上这两个工具（属 market 仓）

## 明确不做

- 向量库 / 语义检索
- 宿主自动把记忆注入 system prompt（措辞是插件的事）
- 跨插件共享记忆
- 第 18 项权限（`storage` 已经覆盖）
