# design: 主题包

## 1. 数据住在哪 —— 一个文件、两个字段、两个写者

```
%AppData%\PetPC\theme.json
{ "theme": "light", "pack": "sakura" }
```

| 字段 | 谁能改 | 谁读 |
| --- | --- | --- |
| `theme` | 桌宠右键菜单的「主题」、控制中心「偏好」页的浅/深 | 两个窗口（`data-theme`） |
| `pack` | 只有控制中心「偏好」页的主题包卡片 | 两个窗口（`data-theme-pack`） |

**两个字段为什么必须是读-改-写。** 两个写者各写自己那一个字段：桌宠的菜单调
`store.WritePanelTheme(dir, theme)`，控制中心调 `store.WritePanelThemePack(dir, pack)`。
如果任一实现按"我这一份结构体"整文件写回，另一次的点击就会被静默抹掉，症状是
「我选的主题包一会儿自己变回紫的」，而两个程序的日志里都没有一句话能解释它。所以两个写者
都先 `readThemeFile` 再写整份——`internal/store/theme_test.go` 有一条测试专门守这件事
（"scheme 与 pack 互不覆盖"，两个方向都测）。

**为什么放进同一个文件而不是各存各的。** 它们是同一件事的两半（"这个产品长什么样"），
而分文件的代价已经有先例：两个窗口各存一个明暗值，最后就是"面板是暗的、设置是亮的"。

## 2. 应用机制 —— 一个属性，三层读数

```
theme.json ──(每秒读一次)──> Layout.ThemePack ──> 桌宠 main.js ──> <html data-theme-pack>
             └─(event market:theme-pack)──> 控制中心 App.svelte ──> <html data-theme-pack>
```

- **控制中心**自己写、自己先应用（`setPack`：先改 DOM，再落盘，落盘失败翻回去）。
- **桌宠**只读：它没有"选包"的界面，包由另一个程序写。它走的是所有偏好共用的一条路——
  `startSettingsWatch` 每秒比较一次（`theme` 与 `pack` 在**同一个比较**里，因为它们在同一个
  layout 里，分两次推会画出"新明暗 + 旧包"的中间帧）。
- **反向通知**：`theme_watch.go` 盯着共享文件，变了就发 `market:theme-pack`。这是**第二条**
  事件，不是给 `market:theme` 加字段：明暗连桌宠菜单都能改，它不知道包存在，合成一条的话
  每次切明暗都得替对方编一个包值。

## 3. 包只覆盖令牌，而且必须两套

```css
[data-theme='light'][data-theme-pack='sakura'] { … }
[data-theme='dark'][data-theme-pack='sakura'] { … }
```

**为什么选择器要带 `[data-theme='light']`。** 裸的 `[data-theme-pack='sakura']` 在深色窗口里
也生效，于是浅色块里为了粉底而压深的状态色（比如 `--warning: #a34a08`）会漏进深色窗口——
实测 2.9:1。第一版就是那么写的，对比度门禁一次报了 11 处。写全两个维度之后每个块都是自足的。

**为什么不挂 `:root`。** 不挂 `:root` 的 `[data-theme='light'][data-theme-pack='x']` 既能作用于
`<html>`（应用），也能作用于**任意一个自带这两个属性的元素**——「偏好」页的预览卡就是这么画的：
卡片身上带 `data-theme` + `data-theme-pack`，卡里的底色、文字、按钮直接用 `var(--bg-raised)`
`var(--accent-strong)`，于是**预览不是一张示意图，它就是那个配色本身**，TS 里一个色值都没有。
抄一份色值进 TS 是最容易腐坏的做法（改了 CSS、忘了改预览，卡片永远说的是旧话）。

## 4. 四个包，以及它们各自踩到的坑

| 包 | 主色 | 坑 |
| --- | --- | --- |
| `violet`（默认） | `#7c3aed` | 没有 CSS 块：它就是 `tokens.css` 的默认值。给它写一块就是第二个真相 |
| `sakura` 樱花粉 | `#db2777` 填充 / `#be1857` 文字 | 粉底"吃"暖色：同一颗 `-700` 的警告/信息色压在 `--bg-active` 上只剩 4.26 / 4.38，包自己压深一档 |
| `forest` 森野绿 | `#047857` | 灰绿底比白底暗：`--text-faint` 与 `--warning` 都要压深 |
| `graphite` 极简石墨 | `#18181b` / 深色 `#e8e9ec` | **填充是浅色**，所以 `--on-accent` 必须翻成深色（`#0d0e10`）。这是唯一一个改 `--on-accent` 的包 |

桌宠那一页还有两颗包专用的小坑，都记在 `frontend/src/style.css` 的包段落里：

- **徽章的底是填充色**：`#fff` 写死在规则里时，石墨深色的徽章就是白字压浅灰底（1.2:1）。
  改成 `var(--on-accent)`，由包决定。
- **toast 的底是 `--danger`**：深色下 `--danger` 是浅红 `#f87171`，白字只有 2.4:1。
  新增 `--on-danger`（浅色白、深色 `#1f1113`）。这一条是这一轮**发现**的老 bug，不是包带来的。

## 5. 门禁

- **`.dev/check-contrast.cjs`**：词表里加了 `packs` 这一维，跑法是"每个明面 × 每套配色 ×
  每个包"，472 个组合。包的值必须自己达标，不能借基础配色的余量。
- **`.dev/check-frontend.cjs`**：`lib/packs.ts` 里每张卡片上的 id，都必须在**两个窗口、两套
  配色**里各有一块色板。这条盯的是"卡片点了没反应"——一个没有色板的 id 会渲染成上一个包的
  颜色，看起来就像点击被吞了。
- **`internal/store/theme_test.go`**：互不覆盖（两个方向）、非法 id 落空、大小写与空白归一。

## 6. 下一轮

1. 插件文档的令牌注入：`internal/plugins/asset.go` 服务 entry 文档时注入 `<style>`，
   必须排在插件自己的 `ui.css` **之后**（同特异性、后写者胜）。
2. 从 `themes/<id>/` 读包（`theme.json` 的 `pack` 已经是目录名形状的 id，`NormalizePack`
   就是按这个形状校验的），以及主题商店。
3. 圆角/字体主题化（需要先让每个页面的卡片认一个变量）。
