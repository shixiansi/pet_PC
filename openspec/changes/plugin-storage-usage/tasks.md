# tasks.md — plugin-storage-usage

- [x] `StorageUsage`（`{used, limit}`，`limit` 显式 int64）+ `RegisterPrivateStore` 注册
- [x] 桌宠方法表加一条（读路径不走 pressure latch）
- [x] `DataMethod` 归数据面
- [x] `TestStorageUsageReportsTheBudget`（含子目录、空目录为 0）
- [x] 手册 §6.3 方法表 + 用法段、附录一行；fake-host `METHODS` 43 + 结果桩
- [x] clipask 0.1.19：`storage.pressure` 里用 `storage.usage` 算"还能再用多少"
- [x] 六份文档的计数改到 43；`TestDocumentedMethodCountMatchesTheTable`
- [x] AGENTS §5 补两条教训（计数由测试绑定；别用 grep 判断能力死活）
- [x] proposal / tasks
