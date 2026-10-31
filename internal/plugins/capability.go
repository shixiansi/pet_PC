package plugins

import (
	"sort"
	"strings"
)

// The fourteen permissions, fixed. A plugin can ask for nothing else, and the
// dispatcher refuses any call whose permission the user has not granted.
//
// The count is deliberately NOT asserted by this comment alone: manifest_test.go's
// TestAllowedPermissionsIsTheFourteen pins the exact list, and app_plugins_test.go's
// TestCapabilityTableIsComplete requires every permission here to be used by at
// least one method. A permission with no method behind it would be a switch the user
// is asked about that does nothing - which is why there is no separate `background`
// permission (a headless frame is gated on `events` / `net.ws` / `net.fetch` / `plugin.call`; see
// validateBackground).
const (
	// PermBubble lets a plugin make the pet say a line in a bubble.
	PermBubble = "pet.bubble"
	// There are no timer.* permissions any more. timer.read and timer.control gated
	// the host's plugin-facing timer surface (timer.state / start / pause / resume /
	// stop), and the timer those five drove is the pomodoro plugin's now: it keeps its
	// own state machine, counts its own phases and writes its own records, so it calls
	// nothing here. A permission whose methods are gone is exactly the "switch the user
	// is asked about that does nothing" this list exists to prevent, and
	// TestCapabilityTableIsComplete refuses it.
	//
	// Nor are there todos.read / todos.write any more, and for the same reason one step
	// further along: the LIST is the todo plugin's own now (its `storage`), and the two
	// other readers moved to `plugin.share` - the pomodoro reads the snapshot the todo
	// plugin publishes, and the chat plugin stopped reading the list at all. With no
	// method left in the table, both permission words are gone (17 → 15 → 13).
	//
	// PermEvents lets a plugin subscribe to the host's state-change events. What is
	// left of them is in SubscribableEvents: the two edge events the host still
	// produces itself (`reminder.fired`) or keeps for compatibility
	// (`timer.phaseEnded`). The two broadcasts, timer.changed and todos.changed, went
	// with the host's timer and the host's todo store.
	PermEvents = "events"
	// PermNetFetch lets a plugin make HTTP requests, but only to the hosts its
	// manifest declares in netHosts.
	PermNetFetch = "net.fetch"
	// PermStorage lets a plugin keep small values in its own private directory.
	PermStorage = "storage"
	// PermPetSurface lets a plugin show a floating surface beside the pet.
	PermPetSurface = "pet.surface"
	// PermLLMChat lets a plugin send a message to the model the user has
	// already configured in the pet. The key stays in the host.
	PermLLMChat = "llm.chat"
	// PermPluginConfig lets a plugin keep its own settings: the host stores them
	// in the plugin's private directory and never lets one plugin read another's.
	PermPluginConfig = "plugin.config"
	// PermAudio lets a plugin keep audio files in its private directory and ask
	// the host to play them.
	//
	// It is a separate permission from storage rather than a widening of it. The
	// two carry different data - storage is small TEXT values (the plugin's own
	// JSON), audio is binary measured in hundreds of kilobytes - and a plugin that
	// only wants to remember a setting should not have to be trusted with an audio
	// sink it never uses.
	PermAudio = "audio"
	// PermNetWS lets a plugin keep ONE long-lived WebSocket connection to a host
	// its manifest declares.
	//
	// net.fetch cannot stand in for this. The chat-protocol sides a plugin talks
	// to (Milky/Yogurt, OneBot, NapCat) push their events over a connection the
	// client holds open, so a one-shot request has nothing to receive. The
	// connection is owned by the HOST and not by the plugin's page for three
	// reasons, in the order that decided it:
	//
	//  1. an access token has to travel as an HTTP header, and a browser
	//     WebSocket cannot set one;
	//  2. the page can be torn down and rebuilt (a plugin update remounts its
	//     frames) while the connection must survive;
	//  3. an unmanaged socket in the sandbox would make net.fetch's allow-list
	//     decorative.
	//
	// The allow-list is the same one net.fetch uses: the manifest's netHosts plus
	// any host-whitelist setting's current value.
	PermNetWS = "net.ws"
	// PermLLMBorrow lets a plugin use the model settings and credential of
	// ANOTHER installed plugin that declares it lends them (`shareLLM`), instead
	// of asking the user to paste the same key a second time.
	//
	// It is a permission rather than an implicit fallback because the borrower is
	// consuming a credential the user entered for a different plugin: that is a
	// cross-plugin read, and the install dialog has to be able to say so. The
	// key itself never reaches the borrower - only the host uses it.
	//
	// It gates TWO methods, which answer two different questions: `llm.profile` says
	// "which settings would I use right now" (one), `llm.providers` says "which channels
	// exist, and which is the default" (a list - still without endpoints or keys). It is
	// also what authorises `llm.chat`'s `provider` field, which NAMES one of those
	// channels: choosing which of the user's configured endpoints to spend is a routing
	// decision INSIDE this consent, not a wider one, so it needs no permission of its own
	// (openspec/changes/llm-channels).
	PermLLMBorrow = "llm.borrow"
	// PermProcess lets a plugin START A PROGRAM ON THIS MACHINE: `process.launch`
	// takes an absolute path to an .exe and an optional argument list, starts it and
	// returns its pid, without waiting for it.
	//
	// It exists because a plugin can legitimately need a program that is not the pet:
	// the QQ plugin talks to a chat-protocol side (Yogurt) that is a separate
	// executable, and "the bot cannot connect because nothing is listening" is a
	// problem no amount of net.ws can solve. The alternative - telling the user to
	// start it by hand before every session - is what this replaces.
	//
	// It is by far the widest permission in this list, so it is bounded rather than
	// trusted: the path must be an ABSOLUTE path the USER typed into the plugin's own
	// settings (a plugin cannot invent one that the user never saw), it must name an
	// existing regular file with a `.exe` extension, the file is capped at 512 MiB and
	// the argument list at 32 arguments of 512 characters. Nothing here reads, writes
	// or deletes anything, and the child is decoupled from the pet (no wait, no
	// inherited stdio), so the worst a plugin can do with it is start a program the
	// user pointed it at.
	PermProcess = "process"
	// PermNetCredentials lets a plugin speak AS THE USER on a host it may already
	// reach, WITHOUT ever being handed the credential.
	// It is a separate permission from net.fetch - and not a widening of it -
	// because "reach this API" and "speak as me on this API" are different
	// questions. A plugin that only reads public data should not have to ask for
	// the second, and an install dialog that merged them would be asking for
	// consent it cannot describe: there is nothing in "访问网络" that tells a user
	// their account could be used to post something.
	//
	// It is the SECOND gate of net.fetch rather than a method of its own: the
	// payload names a DECLARED SECRET config item and the host fills the Cookie
	// header from encrypted storage, because secret plaintext never crosses the
	// bridge (ConfigStore.Read answers {set, mask}). Which is also why a manifest
	// declaring this without net.fetch is refused (validateCredentials): a switch
	// that unlocks nothing is exactly what this list exists to prevent.
	PermNetCredentials = "net.credentials"
	// PermPluginShare lets a plugin publish small values for OTHER plugins to read,
	// and read the ones its manifest asks for by name.
	//
	// WHY A HOST CHANNEL AND NOT "one plugin calls another": plugins share nothing -
	// separate private directories, separate settings, separate secrets, no IPC. That
	// separation is the whole reason a plugin can be reviewed in isolation, so the fix
	// is not to open a door between them but to give the HOST one narrow key/value tray.
	//
	// The directions are deliberately asymmetric:
	//
	//   - WRITING is always allowed, but only under the caller's own prefix
	//     ("<own-id>/<name>"), so no plugin can forge another's value;
	//   - READING requires the exact "<owner>/<name>" pair to be listed in the caller's
	//     manifest `shareRead`, which the install dialog shows. No wildcards, because a
	//     wildcard is how an allow-list quietly becomes "everything".
	//
	// Values are bounded (64 KiB, 32 keys) and carry their write time, so a consumer can
	// ignore stale work instead of acting on a job from three days ago.
	PermPluginShare = "plugin.share"
	// PermPluginCall lets a plugin ASK ANOTHER PLUGIN A QUESTION and get its answer
	// back, through one `plugin.call` (openspec/changes/plugin-call).
	//
	// It is the answer to a shape the tray cannot serve. `plugin.share` has no push and
	// no subscription: a consumer polls, so "one plugin asks another and waits" costs two
	// polls per exchange, carries a mailbox of exactly one key, and has no acknowledgement
	// to distinguish "still thinking" from "never arrived". The one bridge the repo
	// already has (the QQ plugin draining bilibili's results on a 2s timer) is the proof:
	// it works for jobs, and it is the wrong thing for a conversation turn.
	//
	// BOTH SIDES HOLD THIS PERMISSION, and that is the design rather than an oversight:
	// the caller must list the exact "<targetId>/<method>" pairs it may call
	// (`callTargets`), and the callee must list the method names it answers (`serves`),
	// so one switch means "takes part in plugin-to-plugin calls" and the two manifest
	// lists - both shown in the install dialog - say which. A second permission for the
	// serving half would be a switch with no method behind it, which is exactly what the
	// permission list below refuses to contain.
	//
	// A CALL TRANSFERS NO PERMISSIONS. The callee runs with its OWN granted set, so a
	// caller cannot make it do what it may not do, and the caller receives only what the
	// callee chose to return. That is why this needs no per-capability allow-list beyond
	// the two manifest lists.
	PermPluginCall = "plugin.call"
)

// AllowedPermissions is the complete permission vocabulary, in the order the
// UI presents it.
//
// It is a slice rather than a set so the order is stable; callers that need
// membership use IsAllowedPermission. T52 asserts that this set and the set the
// dispatcher actually consults are equal - a permission listed here but used by
// no method would be a switch that does nothing, and a method naming a
// permission absent here would be a capability the user is never asked about.
//
// That equivalence is why there is NO "background" permission: a headless frame
// is requested by the manifest's `background` field, and it is gated on the
// EXISTING events permission (see validateBackground) - or, for a plugin whose
// only traffic is a socket it holds open, on net.ws. A permission with no method
// behind it would be exactly the "switch the user is asked about that does
// nothing" this invariant exists to prevent - and both of those name the thing a
// background plugin actually wants: something delivered to it while nobody is
// looking.
var AllowedPermissions = []string{
	PermBubble,
	PermEvents,
	PermNetFetch,
	PermNetCredentials,
	PermStorage,
	PermPetSurface,
	PermLLMChat,
	PermPluginConfig,
	PermAudio,
	PermNetWS,
	PermLLMBorrow,
	PermProcess,
	PermPluginShare,
	PermPluginCall,
}

// IsAllowedPermission reports whether name is one of the permitted set.
func IsAllowedPermission(name string) bool {
	for _, p := range AllowedPermissions {
		if p == name {
			return true
		}
	}
	return false
}

// PermissionLabels are the human-readable descriptions shown in the install
// dialog, so the user is deciding about "读取待办列表" rather than about
// "todos.read".
var PermissionLabels = map[string]string{
	PermBubble:         "让桌宠弹出一句话的气泡",
	PermEvents:         "订阅宿主的状态变化事件（提醒触发、计时阶段结束、配置变更）",
	PermNetFetch:       "访问网络（仅限插件声明的域名）",
	PermNetCredentials: "以你保存的网站登录凭据发请求（凭据本身不会显示给插件）",
	PermStorage:        "在自己的私有目录里保存数据",
	PermPetSurface:     "在桌宠旁显示自定义浮层面",
	PermLLMChat:        "调用你已配置的大模型（流式返回）",
	PermPluginConfig:   "保存你自己的插件配置",
	PermAudio:          "保存并播放音频（例如语音提醒）",
	PermNetWS:          "保持一条到插件所声明地址的 WebSocket 长连接（收发消息）",
	PermLLMBorrow:      "使用其它插件已配置的大模型与密钥（密钥不会显示给你）",
	PermProcess:        "启动本机程序",
	PermPluginShare:    "与其它插件交换消息（只能读清单里点名的键，只能写自己名下的键）",
	PermPluginCall:     "与其它插件互相调用（只能调用清单里点名的目标，也只能回答清单里声明的方法）",
}

// sortedPermissions returns a copy of names sorted, for stable comparisons and
// for views that must not depend on manifest ordering.
func sortedPermissions(names []string) []string {
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out
}

// EventConfigChanged is "this plugin's own settings file changed on disk".
//
// It exists because settings are edited in ANOTHER PROCESS (the control center's 插件 page) and
// the frame that has to obey them lives here, in the pet's headless frame for the whole session.
// The two programs share no IPC - only the data directory - so "the setting changed" can only be
// noticed by watching the file. Before this event existed, the plugin did the watching itself:
// the pomodoro polled `config.get` every three seconds, which is a per-plugin timer doing the
// host's job (and it showed up as "设置了 5 分钟，下一个番茄钟还是 25" for as long as the poll
// window was).
//
// The payload is `{pluginId}` because the event is BROADCAST to subscribers: a plugin compares it
// against its own id. Same file also covers a plugin writing its own settings through
// `config.set`, so both editors take one path.
const EventConfigChanged = "config.changed"

// SubscribableEvents is the complete set of state-change events a plugin may
// subscribe to.
//
// The set is closed on purpose: a subscription is a promise the host makes, so
// any name outside this list would be a promise the host cannot keep. The
// llm.* frames are not here - they belong to the llm.chat call that produced
// them and are delivered to the caller, not to subscribers.
//
// These are EDGE events ("this just happened"), not state broadcasts. The two broadcasts that
// used to sit here - timer.changed and todos.changed - are gone with the state they described:
// the timer is the pomodoro plugin's and the list is the todo plugin's, so the host has nothing
// to broadcast. A plugin that needs one of those now watches its neighbour through
// `plugin.share`.
//
// timer.phaseEnded is kept WITHOUT an emitter on purpose: it is a promise the host
// currently cannot keep, and the honest fix is a plugin-side timer that keeps running
// headless (openspec/changes/pomodoro-headless-timer). Deleting the name would break
// every installed voice plugin's subscribe call, which validates against this list -
// and a plugin whose subscribe throws is in a worse state than one that hears nothing.
var SubscribableEvents = []string{
	"timer.phaseEnded",
	"reminder.fired",
	EventConfigChanged,
}

// IsKnownEvent reports whether name is a subscribable host event.
func IsKnownEvent(name string) bool {
	for _, n := range SubscribableEvents {
		if n == name {
			return true
		}
	}
	return false
}

// HostAllowed reports whether host ("example.com" or "example.com:8443") is
// covered by the plugin's netHosts allow-list.
//
// The comparison is exact rather than suffix-based: "api.example.com" being
// allowed must NOT make "example.com" (or "notapi.example.com") reachable, and
// a suffix rule is the classic way an allow-list quietly becomes a wildcard.
// An entry without a port matches only the bare host, and one with a port
// matches only that port.
func HostAllowed(hosts []string, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, h := range hosts {
		if strings.ToLower(strings.TrimSpace(h)) == host {
			return true
		}
	}
	return false
}
