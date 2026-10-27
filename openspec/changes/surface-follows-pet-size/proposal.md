# proposal: 浮层面跟着「桌宠大小」走

## 要解决什么

用户把**桌宠大小**调到 1.5 倍之后：

1. 回答气泡的**顶边被切掉**，最上面一行只露出半截；
2. **输入框压到了宠物腿上**。

两条都不是新 bug，而是"清单是按 1.0 倍写死的"这件事终于被用户改的设置暴露出来：

- 所有矩形的 `dy` 都从**宠物左上角**量。输入框写的是 `dy: 204`（＝ 200 高的宠物 + 4px 间隙），
  宠物变成 300 高之后这个偏移落在宠物身上。
- 宠物在窗口里**竖直居中**，所以它头顶的空间随大小变化：1.0 倍 160px，1.5 倍只剩 110px。
  回答框申请的高度按 160px 那档算（156），宿主照单接受并把顶边推到 y = -46——
  区域出不了窗口，页面却照画，于是气泡顶部被裁掉。
- 插件在自己的 iframe 里**量不到**这段空间，也不知道自己的块被摆到了哪里（`init` 只发一次）。

## 做什么

三件小事，各自独立，合起来让浮层面在任何桌宠大小下都正确：

1. **`anchor` 字段**：`Rect` 增加 `anchor: "top" | "bottom"`（省略＝ top）。`bottom` 表示 `dy`
   从宠物**脚底**往下量。输入框改用它。
2. **`room` 字段**：`PluginSurfaceRect` 增加 `room`（宠物上方那一段的高度，CSS px）。
3. **`surface` 消息**：宿主在某块的矩形**变化**时把它重新发给那一帧（`{v:1,type:'surface',rect}`），
   因为 `init` 只发一次，而帧要活过很多次几何变化（桌宠大小、回答长高、窗口换边）。
   插件据此更新 `tailX`（尖角指向）、`room`（上限）并重新量高、重新申请。

外加宿主侧一道**兜底夹回**：长高之后把块夹回窗口（`max <= 块的底边 - 0`），
所以任何尺寸下"申请得太多"都只会变成"装得下多少给多少"，而不是画到窗口外面去。

## 明确不做

- 不新增权限、不新增方法：`surface.blocks` 的 payload 形状不变。
- 不让插件改坐标或尺寸：`anchor` / `room` 都是宿主算的。
- 不动"三块各干一件事"和可见性规则（见 `surface-hidden-keeps-frames`）。

## 影响的不变量

`AGENTS.md §3.2`（区域必须恰好等于画出来的东西）**更强**：宿主现在保证"长大后的块仍在窗口内"，
而以前它会让区域与视图同时越界（视图画出去、区域被裁掉，两者不再一致）。

## 验证计划

- Go：`TestSurfaceAnchorBottomMeasuresFromThePetFeet`（200 与 300 高的宠物各测一次）、
  `TestSurfaceSizesAreClampedToTheBandAboveThePet`、`TestSurfaceRoomShrinksWithThePetAndClampsGrowth`、
  `TestSurfaceSizesAreClampedToTheDeclaredCeiling`（在新 fixture 上仍是 maxH 生效）。
- 插件：`room` 110 时长回答申请 106 而不是 156、两行回答仍给提问条留出位置；`surface` 消息与
  `resize` 都会触发重新申请（幂等）。
- 静态/实机：`go vet`、`gofmt`、`check-frontend.cjs`、`.dev/verify-live.ps1`（锚点与投影带的断言已同步）。
