# tasks.md — plugin-share-subscribe

## 1. 宿主

- [x] `pluginShareSet`：写成功后调 `notifyShareReaders`（发在写之后，收件人 `share.get` 才能读到新值）
- [x] `notifyShareReaders(publisherID, payload)`：遍历已装插件，三条同时满足才发
      （`shareRead` 点名了 `<publisher>/<name>`、持有 `plugin.share`、已启用且 manifest 可读）
- [x] payload `{key, owner, name, at}`，`key` 与 `share.get` 的 `key` 同一个字符串
- [x] 测试：`TestShareSetNotifiesDeclaredReaders`（只唤醒声明过的读者；未声明的键谁也不唤醒）

## 2. 文档

- [x] 插件仓 `docs/plugin-dev.md` §7.1：`share.changed` 的 payload、授权来源（不经过
      `events.subscribe`）、"不是投递保证，轮询留着"
- [x] 顺手修掉 §7 表里 `timer.phaseEnded` 那行被粘到上一行的排版错误
- [x] 本 change 的 proposal / design / tasks

## 3. 验证

```powershell
go vet ./...
go test -count=1 .
node .dev\check-frontend.cjs
cd market; npm test
cd market; node docs/plugin-testkit/plugin-check.mjs plugins/*
```

## 4. 后续（不在本 change）

- [ ] 让几个高频轮询的插件改用它：`media-sync`（每秒）、`pet-sense`（每秒）、`privacy-guard`
      （每两秒）——它们都是"等自己辅助脚本写的文件变了"，收益最大；改完日志里那 94% 的 DEBUG
      才会真的降下来。
- [ ] 控制中心要不要转发（今天明确不做，见 design D5）
- [ ] 如果以后需要"订阅任意键"，那要重新讨论授权模型——不要顺手放开 `shareRead`
