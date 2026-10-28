# 插件开发手册搬到了插件仓

**这份文档现在只住在插件仓里**，因为它讲的是插件，而读它的人是从插件仓（或它的 GitHub 地址）
进来的：

- GitHub：<https://github.com/shixiansi/pet-pc-plugins/blob/main/docs/plugin-dev.md>
- 本机检出：`market/docs/plugin-dev.md`
- 配套的自检工具：<https://github.com/shixiansi/pet-pc-plugins/blob/main/docs/plugin-testkit/README.md>
  （本机：`market/docs/plugin-testkit/`）

## 为什么这里只剩一句话

这份契约（manifest 的每条规则、桥的每个消息、13 项权限与 34 个方法、几何与主题、打包与发布）
同时被三方引用：**插件作者**、**本仓库的实现**、**自检工具里的常量表**。

它原来放在本仓库的 `docs/plugin-dev.md`，而这就出了一件明显的事：插件作者从插件仓的 GitHub
地址进来，根本看不到它——那一份等于只对**拥有宿主源码的人**可见，也就是最不需要它的人。
留两份又会立刻腐坏（谁改哪一份？），所以：**挪到插件仓，本仓库只留这句指针。**

本仓库与它的一致性由 `AGENTS.md §3.3` 的约定守着：改动 manifest 的字段或规则、桥的消息、
任何能力方法的签名与上限、退休一个接口，都要同时改插件仓里的那份手册和工具里那张表。
