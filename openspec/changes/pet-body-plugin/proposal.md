# proposal: 宠物本体也拆成插件

## 要解决什么

宿主手里最后一件"关于宠物本身"的东西，是那张图：`#pet-image` 指向 `store.Config.PetImageFile`
（或内置的 `default-pet.svg`），由页面 `<img>` 画出来。除此之外待办、番茄钟、对话、语音全在插件里。

这留下一个说不通的边界：

- 想换**形象**（不只是换一张 png，而是动画、表情、随状态变化）只能改宿主、重新编译；
- 插件能往宠物身上画一行字（`pet.badge`）、能在旁边放浮层面（`surface`），唯独**画不了宠物自己**；
- 「开始」页的桌宠图片是宿主的设置，而"宠物长什么样"按 §1 的立场本来就不该是宿主的判断。

## 做什么

1. **manifest 新增 `body: true`**：`entry` 那个文档就画在宠物那一格里。要求同时申请
   `pet.surface`（"这个插件会在桌宠身上画东西"就是那一条授权）。
2. **宿主侧新增一个挂载种类**：`frontend/src/plugin-host.js` 的 `syncBody` / `closeBody`，
   `kind: 'body'`、`slot: 'body'`，帧整格铺在 `#pet` 里，**透明**且 **`pointer-events: none`**。
3. **`PluginView` 带上 `body`**，页面据它决定挂谁（已装 + 已启用 + 仍持有 `pet.surface`，
   安装顺序取第一个）。
4. **回退**：没有合格插件时，宿主照旧画那张图；有本体插件时 `<img>` 只是不显示（`visibility`），
   不做销毁——卸载之后要立刻回到那张图。
5. **事件、主题、`plugin.call`、几何**都接上：本体帧是常驻的，和常驻帧一样收 `event`、
   收 `theme`、能回答 `plugin.call`；宠物尺寸变了用 `surface` 消息告诉它。

## 明确不做

- **不新增权限**（没有 `pet.body`）：权限词表要求每一项背后至少有一个方法，而 `body` 是
  manifest 声明、由既有的 `pet.surface` 门控——和 `background` 同形。
- **不新增方法**：本体插件不需要"申请显示"这种动作，它在装好、启用、授权之后就是那只宠物。
- **不让本体帧接手任何手势**：拖动、右键菜单、悬停按钮仍然是宿主的（见 design 为什么不）。
- **不动窗口区域**：宠物那一格本来就在区域里，所以 `app.go` 的 region switch 一行不改。
- **不内置一个官方本体插件**：先把机制做出来；`body` 之前已经有两个插件在画"宠物身上的东西"
  （pet-sense / pet-evolution），它们可以自己升级。

## 影响的不变量

- **AGENTS.md §3.2**（新控件先回答"它在哪个矩形里"）：本体帧的答案是"宠物那一格，整格"，
  而且它带来一条新的硬约束——**必须透明 + 必须不接指针**，否则要么宠物身后一块方板，
  要么一只拖不走的宠物。这条写进了 AGENTS.md §3.2 与 `#pet-body .plugin-frame` 的注释。
- **AGENTS.md §3.1**（权限闸门）：不新增权限，但 **`pet.surface` 的文案改了**
  （"在桌宠身上或旁边显示你自己的界面"），也就是说 §3.1 那份"七处"里的第 3、6、7 处都动了：
  `PermissionLabels`、`cc/src/lib/bridge.ts` 的 `LABELS`、README 的权限表；`.dev/check-frontend.cjs`
  里那份**名字**表不变（它比的是词表而不是文案）。
- **`validateEntry` 的"界面落点"从三种变四种**：`body` 是第四个合法的落点。

## 验证计划

- Go：`TestManifestBodyPinsThePetAndItsPermission`（合法声明 / 缺省 / 只声明 body / 缺
  `pet.surface` 被拒）、`TestPluginViewCarriesTheBodyDeclaration`（那一字段真的离开 Go 了）。
- 前端：`frontend/src/pet-body.test.js`（挂载、停用与撤权不挂、**改尺寸不重建文档**、
  卸载后回到自带图片）。
- 静态接线：`.dev/check-frontend.cjs` 新增五条（挂载点在 `#pet` 内、帧透明且不接指针、
  三道资格门、`kind/slot` 与回收、`body` 与 `pet.surface` 的配对）。
- 文档：`market/docs/plugin-dev.md`（字段、`init.surface`、`body` 一节、权限表）与
  `market/docs/plugin-testkit/plugin-check.mjs`（`no-mount-point` 认出 `body`，新增
  `body-without-permission`）、README「宠物本体也是插件的」。
