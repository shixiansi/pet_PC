# design: 浮层面跟着「桌宠大小」走

## 1. 锚点：`dy` 从哪条边量

```go
type Rect struct {
    ...
    Anchor string `json:"anchor,omitempty"` // ""/"top"（默认）或 "bottom"
}
```

`computePluginSurfaceSized` 里：

```go
y0 := petY
if r.Anchor == plugins.RectAnchorBottom {
    y0 = petY + petH
}
y := y0 + round(r.DY*s)
```

**为什么不是"把 dy 写大一点"**：宠物的逻辑尺寸是 `150×PetScale`（`PetBaseWidth/Height`），
用户在设置里能改到 2.5 倍；任何从宠物**顶部**量的固定偏移都只在某一个尺寸下正确。
从**脚底**量是唯一与尺寸无关的写法，而这恰好是"宠物下方"这件事的定义。

校验里拒绝任何其它取值（否则一个拼写错误会静默地把块锚到另一条边，
而症状看起来像宿主的 bug）。

## 2. 空间：`room`

`PluginSurfaceRect.Room` = 宠物上方那一段的高度（CSS px，`petY/s`）。它随每个矩形一起下发，
所以 `init` 里就有。

**为什么必须由宿主给**：宠物竖直居中在 520 高的窗口里，`room = (520 - 200×PetScale)/2`。
插件在自己的 iframe 里既看不到窗口、也看不到宠物，量不出来。

**回退**：旧宿主不发 `room` 时插件按 160 算（默认尺寸那档）——只是保守，不会更糟。

## 3. 消息：几何变了要告诉帧

`init` 发一次，而一帧要活过很多次几何变化：

- 用户改「桌宠大小」→ `applyWindowGeometry` → layout 变化；
- 回答长高 → `SetPluginSurfaceBlocks` → 重新裁剪 + 推 layout；
- 窗口换边（宠物贴左/贴右）→ `tailX` 变化。

这些都不会重建帧（pluginId/version/块数没变），于是一个只用 `init` 里那份矩形的插件会：
把尖角画在宠物**曾经**的中心、按**旧的** `room` 申请高度。

所以 `PluginHost.applyGeometry` 在把矩形套到帧上时比较新旧值（按值比较：layout 对象每次都重建），
变了就 `postMessage({v:1,type:'surface',rect})`。插件收到后更新 `tailX` / `room`、重画气泡、
**忘掉上一次的申请**并重新计算——幂等，因为宿主不会因为同一个尺寸再发一次 resize。

这条消息与 `theme` 是同一类：**宿主 → 已挂载的帧，关于它自己的处境**。

## 4. 兜底夹回：窗口对高度有最后一句

原来长高是"先夹进窗口，再往上加，加完不再夹"：

```go
if y < 0 { y = 0 }        // 声明位置夹回
...
regions[ri].Y -= grew     // 往上长，可能越过 0
regions[ri].Height = max
```

于是区域出现负坐标（`SetWindowRgn` 会把它裁掉），而视图仍把 iframe 摆在那里——
**区域和视图不再一致**，表现就是气泡顶边被切掉半行。

现在长高之后再夹一次：

```go
if room := regions[ri].Y + regions[ri].Height; max > room {
    max = room            // 块的底边就是可用高度的上限（窗口顶边是 0）
}
```

它与插件侧的 `min(maxH, room - 4)` 是两道独立的闸：插件先算到装得下（避免无谓的请求与裁剪），
宿主兜底（任何越界都变成"装得下多少给多少"）。

## 5. 结果

以 1.5 倍（宠物 225×300、`room` 110）为例：

| 元素 | 之前 | 现在 |
|---|---|---|
| 提问条 | 落在 y<0 被裁，与回答重叠 | 装不下就隐藏（`height + 48 > room`） |
| 回答气泡 | 申请 156，顶边 -46 → 顶部切掉 | 申请 ≤ 106，整体在窗口内，多余滚动 |
| 输入框 | `dy 204` → 压在宠物腿上 | `anchor: bottom, dy: 4` → 脚下 4px，随尺寸走动 |
