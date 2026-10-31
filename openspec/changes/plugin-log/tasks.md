# tasks.md — plugin-log

- [x] 方法表加 `plugin.log`（借 `storage`）；`pluginLog`（一行、≤500、三级、带 id、5 秒节流）
- [x] `DataMethod` 归类为数据面（DEBUG）
- [x] 市场程序的服务端副本（同样的边界）
- [x] `TestPluginLogWritesOneAttributedLine`（落盘 / 归属 / 等级 / 防伪造 / 截断 / 两种拒绝）
- [x] 手册 §6.13 + 附录 + 权限表；fake-host METHODS（42 项）
- [x] README「日志」一节
- [x] proposal / design / tasks

## 后续（不在本 change）

- [ ] 让插件真的用它：`media-sync` 的 SMTC 读不到时、`screen-vision` 的截图失败时、
      `clipask` 的辅助脚本没起来时——今天这些都只能静默
- [ ] 是否把 `swallowed` 暴露到工坊页（"你的日志被合并了 N 次"）
