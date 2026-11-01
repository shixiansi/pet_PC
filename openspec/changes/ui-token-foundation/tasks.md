# tasks: 令牌地基

## 1. 词表与文档

- [x] 实算现状，确认"配色已全绿"只对插件层成立（控制中心 2.03 / 3.41 / 3.58 / 3.07 / 3.16 / 2.89）
- [x] 定命名裁决：向改不动的 `petpc-ui.css` 收敛（`--accent` 文字 / `--accent-strong` 填充），
      否掉设计稿的 `--accent-ink`（理由见 design.md §2）
- [x] `openspec/changes/ui-token-foundation/{proposal,design,tasks}.md`

## 2. 控制中心（`tools/plugin-market/cc/src/tokens.css`）

- [x] 浅色：`--text-dim #7a8798 → #5b6472`、`--text-faint #a6b1c0 → #64707d`
- [x] 浅色：`--success/#warning/#danger` 取 -700（`#047857 / #b45309 / #b91c1c`）
- [x] 强调色拆成两个名字：`--accent #7c3aed`（文字）+ `--accent-strong #6d28d9`（填充），
      `--accent-hover → #5b21b6`
- [x] 深色：`--text-dim → #a8b4c6`、`--text-faint #5a6a7e → #8e9bb0`
- [x] 深色：`--accent → #a78bfa`（文字）、`--accent-strong #7c3aed`（填充）、`--accent-hover → #6d28d9`
- [x] 文件头补第 3 条约束：名字有角色，文字不是填充

## 3. 控制中心的组件

- [x] `components/Button.svelte`：`.primary` 用 `--accent-strong`，补 `:hover`
- [x] `pages/Market.svelte`：`.btn.primary` 用 `--accent-strong`
- [x] `pages/Plugins.svelte`：`.chip.primary` 用 `--accent-strong`；选中行的紫字注释改对
- [x] `accent-color: var(--accent)` × 4 页 5 处 → `--accent-strong`（`Advanced` / `Bench` / `Logs` / `Start`×2）
- [x] `App.svelte` / `Bench.svelte` / `Logs.svelte` / `Plugins.svelte` 的文字用色统一回 `--accent`
      （它现在就是"当文字"的那一档）

## 4. 桌宠页（`frontend/src/style.css`）

- [x] `--text-dim → #5b6472`（与控制中心同值，`check-frontend.cjs` 有配对断言）
- [x] `--ok/--warn/--danger` 取 -700
- [x] 新增 `--accent-strong`；`--accent` 两套配色分开（浅 `#7c3aed` / 深 `#a78bfa`）
- [x] 徽章 `background: var(--accent) → var(--accent-strong)`（白字压在填充色上）

## 5. 门禁

- [x] `.dev/check-contrast.cjs`：文字令牌 × 表面令牌，深浅两套，114 个组合，< 4.5:1 即红
- [x] 半透明合成规则：面板按最坏情况压白；状态软底点名 `over: 'bg-raised'`（否则门禁自己算错）
- [x] 负向验证：把 `--text-faint` 改回 `#a6b1c0` → 立刻红并打印 `2.03:1`（试完已还原）
- [x] `.gitignore` 白名单 `!.dev/check-contrast.cjs`
- [x] `.dev/check-all.ps1`：新增 `Gate 'contrast checks'` + 文件头门禁清单（BOM 已确认还在）
- [x] `.github/workflows/ci.yml`：契约 job 里加一步
- [x] `README.md`：门禁链与"对比度门禁"一节

## 6. 验证记录

```
node .dev/check-contrast.cjs
  → 对比度检查：114 个组合全部达 AA（≥ 4.5:1），深浅两套都跑过   [exit 0]

node .dev/check-frontend.cjs
  → ALL FRONTEND CHECKS PASSED (147)   [exit 0]
     （其中 "the pet page and the control center name the same colours" 仍然 PASS）

cd tools/plugin-market/cc && npm run check     → 0 errors, 0 warnings
cd tools/plugin-market/cc && npm run build     → built in 519ms
cd tools/plugin-market/cc && npm run smoke     → smoke: 109/109

cd frontend && npm run build                   → built in 206ms
cd frontend && npm test                        → 71 passed (8 files)

pwsh -File tools/build-all.ps1                 → exit 0
  PetPC.exe                11,612,672 bytes  11:53:37
  PetPCPluginMarket.exe    11,049,984 bytes  11:53:26
  两个 exe 里都能扫到 --accent-strong / #8e9bb0 / #5b6472（前端产物确实进了二进制，
  不是只改了源码——AGENTS.md §7 那条"改了却看起来没变"）
```

## 7. 留给下一轮（不在本次范围）

- [ ] 市场仓 `petpc-ui.css`：深色 `--text-faint → #8e9bb0`；781～788 行两处 soft 标签改用
      `--accent-on-soft`。要动 27 份副本 + 重新打包 27 个插件
- [ ] 市场两页（`index.html` / `manage.html`）由 `package.ps1` 从 `index.json` 生成，
      并改掉 `tests/ui-market.test.js` 里那几个把 18 写死的断言
- [ ] 空状态 / 骨架进 `petpc-ui.css`
- [ ] 主题包本体（`{scheme, pack}`、令牌白名单、注入插件帧、商店）
