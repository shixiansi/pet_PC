# proposal: 开机自启指向桌宠，而不是指向控制中心

## 要解决什么

用户报的是「开机自启失效了」。查下来的状态是：`HKCU\...\Run` 里**没有** `PetPC` 这个值，也没有
`StartupApproved` 的禁用记录，`config.json` 里 `autostart:false`。也就是说先前的自启项要么被关
掉了，要么从来就没写对过。而代码里有一条确定的、会**稳定复现同一个症状**的缺陷：

1. 自启开关长在控制中心的「外观与启动」页（`cc/src/pages/Start.svelte`），桌宠自己的设置面板
   已经退休，所以这是唯一的入口；
2. 它调 `Market.SetAutostart` → `desktop.SetAutostart(on)`，而后者用 `os.Executable()` 取路径
   —— **谁调用它，它就把谁写进 Run 项**。控制中心调用它，写进去的就是
   `PetPCPluginMarket.exe`；
3. 于是登录时起来的是控制中心，桌宠永远不出现（开发期 `build\bin` 与
   `tools\plugin-market\build\bin` 一换，连那个也起不来，因为路径已经不存在了）。

这与 README §4 记录过的两种老症状**一模一样**（勾选着、值也在、开机就是不启动），只是原因第三
种：前两种是"资源管理器解析不了这个值"，这一种是"解析得了，但启动的是另一个程序"。

同一条路径上还有第二个缺陷，让开关连"记住"都做不到：`autostart` 是 `config.json` 的**运行时键**
（`internal/store/settings.go` 的 `runtimeKeys`），而控制中心保存偏好走
`store.SaveSettings`，它**故意把这些键从 `settings.json` 里删掉**。所以
`store.SaveSettings(..., c.Autostart = on)` 一个字节也没写——旧代码既写错了程序，也没有留下任何
持久痕迹。

## 做什么

**`internal/desktop`（唯一知道 Run 项是什么形状的地方）**

1. `SetAutostartFor(exe string, enable bool)`：要自启的程序由**调用方显式给出**。传进来的路径如果
   是控制中心（`PetPCPluginMarket.exe`），**拒绝**而不是写进注册表——一个启动错误程序的 Run 项与
   缺失的 Run 项在用户眼里没有任何区别，所以这个错误必须在这里就发生。
2. `SetAutostart(enable)` 只剩桌宠自己用（内部转成 `SetAutostartFor(ExecutablePath(), enable)`）。
3. `AutostartEnabled()` 从"值存在且没被标记禁用"扩成第三条：**值指向的不能是控制中心**。
   否则复选框继续显示"已开启"，而那正是这次要消掉的谎。
4. `RepairAutostart()` 多修一种值：指向控制中心的那种，改回桌宠。仍然**不**改写"指向另一份桌宠
   安装"的合法值（README §4 的劫持教训原样保留，两种情况的区别是刚性的：另一个桌宠是用户的
   选择，控制中心是这条路径自己的历史错误）。

**控制中心（`tools/plugin-market`）**

5. `SetAutostart` 先在自己旁边找桌宠（`petExecutableCandidates`：同目录 `PetPC.exe`，以及开发期从
   `tools\plugin-market\build\bin` 往上四层的 `build\bin\PetPC.exe`），找到才调
   `desktop.SetAutostartFor`；找不到就报一句中文原因，**不假装成功**。关闭方向不需要路径，永远
   可用（装了一半的用户也得能把这个开关关掉）。
6. `MainSettings()` 的 `autostart` 直接问 `desktop.AutostartEnabled()`：`config.json` 里那份是
   桌宠维护的镜像，桌宠不跑就是旧的，而这一页要回答的是"登录时会不会有桌宠"。
7. 不再 `SaveSettings` 一个运行时键（写了等于没写），`SetAutostart` 直接回读 `MainSettings()`。

## 明确不做

- **不把 `autostart` 挪进 `settings.json`**（即"控制中心只存偏好、桌宠去写注册表"）。这条看着更
  符合"一个文件一个写者"，但它要桌宠**正在运行**才生效：用户在桌宠没开时勾上，然后关掉一切重启
  电脑，什么都不会发生——正是 AGENTS.md §7 反复拦的那种"存下了但没生效"。注册表就是这项设置本身，
  写它的程序必须先知道桌宠在哪，而控制中心已经在磁盘上找得到它。
- **不因为"值和当前 exe 不一致"就改写**：`RepairAutostart` 每次 `GetState` 都跑，无条件对齐会
  让任何一个副本（测试二进制、第二份安装）劫持用户的自启项——这条以前真的踩过。
- **不动 `ReminderEnabled` 那个同类问题**（它也是运行时键，`SetReminderEnabled` 同样写不进去）。
  它是提醒面板和插件之间的事，与本次症状无关，单独一轮再看。

## 影响的不变量

- README §4 的"注册表为准"没变，但多一条判据：**值为真不等于功能为真**，还要看它指向谁。
  两个程序从此都读同一个谓词 `desktop.AutostartEnabled()`。
- 测试不许碰真实值 `PetPC` 的规矩原样保留；新增的拒绝用例**故意**跑真实值，因为它断言的是
  "什么都没写"。
- §3.1 的权限表无关（没有新方法、没有新权限、没有新绑定），Wails 绑定面一个方法都没变。

## 验证

- `go vet ./...` + `go test ./... -count=1`（根 module）：**全绿**，`internal/desktop` 也在内。
  注意这批用例要写 HKCU，受限沙箱里会报 `open Run key: Access is denied`（AGENTS.md §7 的已知项，
  不是代码问题）；放宽权限后 `internal/desktop ok 0.300s`。
- `cd tools/plugin-market && go vet ./... && go test ./... -count=1`：全绿。
- `node .dev\check-frontend.cjs`：`ALL FRONTEND CHECKS PASSED (153)`（没有绑定变化）。
- 新增用例：`TestSetAutostartStoresTheGivenExecutable`、`TestSetAutostartForRefusesTheControlCenter`、
  `TestIsControlCenterEntry`、`TestAutostartUsableRejectsControlCenterEntry`、
  `TestRepairAutostartRewritesControlCenterEntry`、`TestPetExecutableCandidates*`、
  `TestFirstExistingFilePicksTheFirstHit`。
- 真机：两个程序重建（`wails build -s -skipbindings`，前端未改动所以 `-s` 正确；控制中心那颗 exe
  也重新拷到 `build\bin`），扫字节确认新代码在两颗 exe 里（`开机自启必须指向桌宠` 命中），
  `reg query` 复核：`PetPC = "F:\dev\pet_pc\build\bin\PetPC.exe"`，`StartupApproved\Run` 里没有
  `PetPC` 的 `0x03` 记录。
