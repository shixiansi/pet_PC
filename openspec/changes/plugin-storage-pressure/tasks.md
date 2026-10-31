# tasks.md — plugin-storage-pressure

- [x] `PluginDataPressureRatio`（0.8）/ `PluginDataPressureAt`
- [x] `noteStoragePressure`（latch + 一次跨越一次 + 回落重置），只在写成功后调用
- [x] `storage.set` 走 `pluginPrivateStoreWithPressure`；`audio.save` 成功后也提示
- [x] `TestStoragePressureFiresOncePerCrossing`
- [x] 手册 §7.3、AGENTS §3.9
- [x] clipask 0.1.18 响应它
- [x] proposal / tasks

## 后续

- [ ] 诊断报告里已经在报"占用 X（上限 Y）"，可以考虑把"接近上限"也标成问题行
- [ ] 别的会长的插件（qq 的 437 个状态文件、screen-vision 的历史）也响应它
