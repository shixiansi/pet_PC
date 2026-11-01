# tasks: 主题包

## 1. 存储（`internal/store/theme.go`）

- [x] `themeFile` 加 `pack`；`NormalizePack` 校验 `[a-z0-9-]{1,32}`（小写、去空白，非法 → 空）
- [x] `ReadPanelThemePack` / `WritePanelThemePack`
- [x] **两个写者都改成读-改-写**：`WritePanelTheme` 保住 pack，`WritePanelThemePack` 保住明暗
- [x] 共用 `readThemeFile` / `writeThemeFile`（原子写仍是临时文件 + rename）
- [x] 测试：两个方向互不覆盖、非法 id 落空、大小写/空白归一

## 2. 两个程序

- [x] 控制中心 `Market.ThemePack()` / `Market.SetThemePack()`（INFO 记一行，只记 id）
- [x] `theme_watch.go`：第二条事件 `market:theme-pack`；一次 tick 可以同时报两件事
- [x] 桌宠 `Layout.ThemePack` + `panelThemePack()`；`startSettingsWatch` 把明暗与包放进**同一个**
      比较（两次推会画出"新明暗 + 旧包"的中间帧）
- [x] `frontend/src/main.js`：`layout.themePack` → `data-theme-pack`，空值**删属性**（不是写空串）

## 3. 色板

- [x] `cc/src/packs.css`：樱花粉 / 森野绿 / 极简石墨 × 浅深两套；`app.css` 在 `tokens.css` 之后导入
- [x] `frontend/src/style.css`：同名同值的三个包 × 两套（面板、文字、强调、chip 边框、`--on-accent`）
- [x] 每块的坑都写在注释里：粉底吃暖色、灰绿底更暗、石墨的填充是浅色（`--on-accent` 翻深）
- [x] 顺手修掉一个老 bug：桌宠 toast 是"白字压 `--danger`"，深色下 2.4:1 → 新增 `--on-danger`

## 4. 界面

- [x] `lib/packs.ts`：只有 id / 名字 / 一句话，**没有色值**
- [x] `lib/theme.svelte.ts`：`themeState.pack` + `setPack`（先 DOM、再落盘、失败翻回）
- [x] `main.ts`：挂载前应用（带 1.5s 超时，读不到就是默认配色）
- [x] `pages/Preferences.svelte`：4 张卡片网格，卡片自带 `data-theme` + `data-theme-pack`
      （预览就是真实配色，不是示意图）
- [x] `App.svelte`：订阅 `market:theme-pack` + 窗口获得焦点时补读

## 5. 门禁

- [x] `check-contrast.cjs`：词表加 `packs` 维 → 472 个组合；包块的选择器不带 `:root` 也认
- [x] `check-frontend.cjs`：三张表必须同时存在（清单 / 两个窗口的色板 / Go 绑定与页面）
- [x] `README.md`：「偏好」页那一行加上主题包

## 6. 验证记录

```
node .dev/check-contrast.cjs
  → 472 个组合全部达 AA（两套配色 × 每个主题包 × 两个明面）  [exit 0]
node .dev/check-frontend.cjs
  → ALL FRONTEND CHECKS PASSED (150)  [exit 0]
     （新增三条：色板齐全 / 绑定与页面这条链 / 桌宠页真的应用 themePack）
go build ./... ; go vet ./...                      → 0
cd tools/plugin-market && go vet ./... && go test ./...   → ok
go test ./internal/store/                          → ok（互不覆盖那条测试在内）
cd tools/plugin-market/cc && npm run check          → 0 errors
cd tools/plugin-market/cc && npm run build          → ✓
cd tools/plugin-market/cc && npm run smoke          → 109/109
cd frontend && npm run build && npm test            → ✓ / 71 passed
wails generate module（市场程序，为了新的两个绑定）
pwsh -File tools/build-all.ps1                      → exit 0
```

## 7. 紧接着做完的三件（原计划里"下一轮"，本轮一并落地）

- [x] **插件帧的令牌注入**：`internal/plugins/asset.go` 服务 entry 文档时把
      `themepacks.StyleTag()` 拼在 `</head>` 之前；选择器 `:root:root` 压过插件自己的
      `:root[data-theme]`；两个宿主的帧地址都带 `&pack=`。测试：`internal/plugins/asset_test.go`
- [x] **从 `themes/<id>/` 读包**：`internal/themepacks/store.go`（manifest 校验、值消毒、
      两套令牌与骨架令牌、坏包带原因列出）+ `audit.go`（加载时实算对比度，不合格只报告不拒绝）；
      控制中心「偏好」页把已安装的包画成卡片，预览用宿主送来的令牌。测试：`store_test.go`
- [x] **主题商店**：`internal/plugins/themestore.go`（`themes/index.json` → 下载 → sha256 →
      校验 → 写入；卸载；拒绝内建 id；404 不算错误）+ market 仓 `themes/`（2 个包 +
      `check-catalogue.mjs` 的货架校验）+ 「偏好」页的商店区（安装/更新/卸载）。

## 8. 仍然留着的

- [ ] 圆角、字体、间距的主题化（需要页面先认变量）
- [ ] 主题包的"预览图"（现在卡片是令牌直出，没有截图）
- [ ] 已挂着的插件帧在包切换后的热更新（那段 CSS 是服务端拼进文档的，重挂才换；重挂会丢表单）
