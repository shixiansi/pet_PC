# tasks.md — plugin-storage-quota

- [x] `MaxStorageValue`（1 MiB）、`MaxPluginDataBytes`（64 MiB）
- [x] `checkStorageBudget`（写前、扣旧值、两种拒绝两种文案）、`privateDirBytes`（早退）
- [x] `PluginDataBytes` 导出给 `audio.save` / `audio.import`
- [x] 测试：两种拒绝 + 覆盖大值放行 + 小目录放行；`StorageSet` 上的越界拒绝且**没写文件**
- [x] README「插件数据放在哪」、手册 §6.3 顶部
- [x] proposal / design / tasks

## 后续（不在本 change）

- [ ] 控制中心能不能显示"这个插件占了 X MiB"（`storage.stat` 只给单个键，整棵树要靠宿主）
- [ ] 接近上限时给插件一条事件（`storage.pressure`？）——现在只有拒绝
