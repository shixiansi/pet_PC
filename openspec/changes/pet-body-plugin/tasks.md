# tasks: 宠物本体也拆成插件

## 宿主（Go）

- [x] `internal/plugins/manifest.go`：新增 `Body bool`（`body`），`validateBody` 要求
      `pet.surface`，`validateEntry` 的"界面落点"从三种变四种。
- [x] `internal/plugins/manifest_test.go`：`TestManifestBodyPinsThePetAndItsPermission`
      （合法 / 缺省 / 只声明 body / 缺权限被拒），并把"三种落点"的注释与断言改成四种。
- [x] `internal/plugins/capability.go`：`PermPetSurface` 的注释与 `PermissionLabels` 文案改成
      "在桌宠身上或旁边显示你自己的界面（浮层面、头顶的一行字、宠物本体）"。
- [x] `app_plugins.go`：`PluginView.Body`（JSON `body`）+ `pluginViewOf` 填它。
- [x] `app_pet_body_test.go`：`TestPluginViewCarriesTheBodyDeclaration`（字段真的离开 Go、
      旁边只有浮层面的插件不会被报成画宠物）。

## 宿主（前端）

- [x] `frontend/index.html`：`#pet-body` 挂载点（在 `#pet` 内、`#pet-image` 之前）。
- [x] `frontend/src/style.css`：`.pet-body` 整格铺开、**无 `z-index`**；
      `#pet-body .plugin-frame` 透明 + `pointer-events: none`；
      `#pet.body-from-plugin #pet-image { visibility: hidden }`。
- [x] `frontend/src/plugin-host.js`：`kind: 'body'` / `slot: 'body'`、`syncBody` / `placeBody` /
      `closeBody` / `bodyPluginId`、`allFrames` / `onMessage` / `setTheme` / `routeEvent` /
      `frameForCall` 全部带上本体帧；`initMessage` 让 `slot` 对所有种类都发。
- [x] `frontend/src/main.js`：`els.petBody`、`petBodyPlugin()`（三道门 + 只取第一个 + 点名其余）、
      `syncPetBody()`，在 `applyLayout` 与 `refreshInstalled` 两处调用；列表还没读到时触发一次读。
- [x] `frontend/src/pet-body.test.js`：五个用例（无声明回退、挂载与沙箱、停用/撤权不挂、
      **改尺寸不重建文档**、卸载后收回）。

## 门禁与文档

- [x] `.dev/check-frontend.cjs`：新增「6c. 宠物本体是它自己的挂载点」五条。
- [x] `market/docs/plugin-dev.md`：§1 挂载表与"四种界面落点"、§4 顶层字段、§4.1 权限表、
      §5.2 `init.surface`、§6.5 末尾新增 `body` 一节、§8.5 的 slot 列表。
- [x] `market/docs/plugin-testkit/plugin-check.mjs`：`no-mount-point` 认出 `body`，新增
      `body-without-permission`。
- [x] `README.md`：新增「宠物本体也是插件的」一节；`pet.surface` 权限行与"没有 `pet.body` 权限"。
- [x] `AGENTS.md`：§1 的立场、§3.2 的本体帧约束、§3.3 的 slot 列表。
- [x] `tools/plugin-market/cc/src/lib/bridge.ts`：dev 桩的 `LABELS` 跟随新文案。
- [x] 「扩展能力」测试台加第四种落点（`body`）：`Bench.svelte` 的 `Place` / `choosePlace` /
      `surfaceNow` / 尺寸与缩放那一行 / 说明文字，`plugin-frame.ts` 的 `MountSurface.kind` 与
      `plainSurface`，两个生成的 `models.ts` 补上 `Manifest.body` / `PluginView.body`。
      `.dev/check-frontend.cjs` 那条"测试台能挂到桌宠的位置上"一并盯 `kind: 'body'`。

## 没做（有意）

- 官方本体插件本身：`market/` 是独立仓，插件要走它自己的打包与发布流程。
- `pet.badge` / 角标搬进本体插件：它们是两个宿主能力，番茄钟与待办依赖它们，
  不该被"用户装了一个会画宠物的插件"影响（见 design §8）。
