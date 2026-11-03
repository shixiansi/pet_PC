# tasks: 开机自启指向桌宠，而不是指向控制中心

## `internal/desktop`（Run 项的唯一作者）

- [x] `desktop_windows.go`：`controlCenterExeName` 常量 + `isControlCenterEntry(command)`
      （Run 值是命令行，可能带引号、可能带参数，所以先取路径再比 basename）。
- [x] `desktop_windows.go`：`setAutostart(valueName, exe, enable)` 收 exe；
      `SetAutostartFor(exe, enable)` 是新的公开入口，`exe` 是控制中心时**报错且不写**；
      `SetAutostart(enable)` 变成 `SetAutostartFor(ExecutablePath(), enable)`（只剩桌宠用）。
- [x] `desktop_windows.go`：`autostartEntryCommand` / `autostartUsable`，`AutostartEnabled` 改为
      "值存在 ∧ 未被禁用 ∧ 不指向控制中心"。
- [x] `desktop_windows.go`：`repairAutostart` 的修复集合从「`\\` 畸形值」扩到「`\\` 畸形值
      **或** 指向控制中心」，并注明为什么"指向另一份桌宠"仍然不动。
- [x] `desktop_other.go`：`SetAutostartFor` 桩（非 Windows 一律 unsupported）。

## 控制中心（`tools/plugin-market`，独立 module）

- [x] `settingsapi_pet.go`：`petExeName` / `petExecutableCandidates(self)` /
      `petExecutablePath()` / `firstExistingFile` —— 与桌宠找控制中心的那张候选表互为镜像。
- [x] `settingsapi_pet.go`：`SetAutostart` 关闭方向不找路径（半装的用户也要能关），开启方向先找
      桌宠再 `desktop.SetAutostartFor(pet, true)`，找不到就报中文原因；不再 `SaveSettings` 一个
      运行时键。
- [x] `settingsapi.go`：`MainSettings()` 的 `autostart` 取自 `desktop.AutostartEnabled()`。
- [x] 绑定面未变（方法名/签名/返回值形状不变），`wailsjs` 不需要重新生成。

## 测试

- [x] `internal/desktop/autostart_windows_test.go`：五个新用例（写入方给的可执行文件、拒绝控制
      中心、`isControlCenterEntry` 的表格、`autostartUsable` 的读取侧、修复路径），并把既有用例
      改成三参数 `setAutostart`。
- [x] `tools/plugin-market/settingsapi_pet_test.go`：三个新用例（候选表首选同目录、开发期候选能
      到达 `build\bin`、`firstExistingFile` 的顺序/目录不算/找不到）。
- [x] 这些用例都走 scratch 值名 `PetPC-test-do-not-use`；唯一碰真实值名字的是"拒绝"用例，它断言
      的正是"什么都没写"。

## 文档

- [x] `README.md` §4「开机自启」：补第三种症状（值指向控制中心）、`SetAutostartFor` 的规矩、
      "两个程序都从注册表读"；测试清单里 17 → 23 并把新用例点名。
- [x] `openspec/changes/autostart-points-at-the-pet/`（本文件与 proposal）。

## 收尾（本机）

- [x] 两个程序重建（`tools/build-all.ps1`：控制中心的界面是编译期嵌进它自己的 exe 的，只重建
      桌宠看不到任何变化）。
- [x] 本机注册项按新规矩写好并复核：`reg query` 的 `PetPC` 指向 `build\bin\PetPC.exe`，
      `StartupApproved\Run` 里没有 `PetPC` 的 `0x03` 记录。

## 没做（有意）

- 没有把 `autostart` 从 `config.json` 搬进 `settings.json`（理由见 proposal「明确不做」）。
- 没有顺手修 `ReminderEnabled` 的同型问题（同样是运行时键、同样被 `SaveSettings` 删掉）。
