package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/llm"
	"PetPC/internal/plugins"
	"PetPC/internal/store"
)

// eventPluginEvent is the ONE Wails event name plugin traffic travels under.
//
// Every host->plugin message - state changes from events.subscribe and the
// llm.chat stream - goes out as this event with a PluginEventView payload, and
// the frontend routes it to the right sandboxed iframe by pluginId. A single
// name is what keeps the frontend's subscription list one line long and makes
// the "every subscribed event is emitted by Go" check meaningful: the check
// scans Go source for the literal, so the name is written with double quotes at
// every emit site rather than through a constant.
const eventPluginEvent = "pet:plugin-event"

// pluginDataDirEnv is the environment variable a pixel gate uses to point the
// pet at a scratch data directory. It is the existing PETPC_DATA_DIR, read here
// only so the plugin subsystem honours the same override as the store.

// --- views -----------------------------------------------------------------

// PluginView is one installed plugin as the pet needs to see it.
//
// The pet no longer renders a plugin list - that is the plugin market's job now
// - so this is not a UI projection any more. It is the answer to the two
// questions the PAGE asks: who owns a panel slot, and where are the rectangles a
// floating surface must place its iframes. Every field here is read by
// frontend/src/plugin-host.js or by the slot resolution in app_chat_slot.go.
type PluginView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	// Permissions is everything the manifest declares, Granted what the user
	// allowed, and Pending the difference - so the row can offer "authorise the
	// rest" without the frontend having to subtract two lists.
	Permissions []string `json:"permissions"`
	Granted     []string `json:"granted"`
	Pending     []string `json:"pending"`
	NetHosts    []string `json:"netHosts,omitempty"`
	Panel       bool     `json:"panel"`
	// PanelSlot is which SLOT the manifest claims, "" for none: `chat`, `todos` or
	// `pomodoro`, and each names one cell of the pet's right-click menu now that the panel
	// is gone. It travels with the row because the frontend resolves the menu's target from
	// it: fetching a manifest per cell would make "who opens this" a second lookup.
	//
	// The JSON name still says "panel" because manifests are read FROM DISK: renaming the
	// field would turn every plugin already installed on a user's machine into an illegal
	// manifest on the next launch.
	PanelSlot string `json:"panelSlot,omitempty"`
	Surface   bool   `json:"surface"`
	// Background is the headless-frame declaration. The page reads it to decide
	// whether this plugin gets a permanently mounted hidden frame, so it has to
	// travel with the row: a page that had to fetch a manifest per plugin to learn
	// this would make "which plugin runs in the background" a second lookup that
	// can disagree with the list it is rendering.
	Background bool `json:"background"`
	// Entry is the plugin's HTML entry point, so the tab can build the sandboxed
	// iframe's URL without a second lookup.
	Entry string `json:"entry,omitempty"`
	// Dir is where the plugin's files live, for the diagnostics line.
	Dir string `json:"dir"`
	// Error explains a manifest that no longer parses. Such a row cannot be
	// enabled, but it can be uninstalled, which is the point of showing it.
	Error string `json:"error,omitempty"`
}

// PluginEventView is the payload of every pet:plugin-event.
type PluginEventView struct {
	PluginID string `json:"pluginId"`
	Name     string `json:"name"`
	// At is when the HOST emitted this event, as RFC 3339 carrying the LOCAL
	// offset ("2026-10-26T14:32:00+08:00").
	//
	// It exists because the event stream is what a plugin shows a MODEL, and
	// delivery order alone cannot answer "when": a frame that reaches a plugin
	// after a stall still describes the moment it happened, not the moment it
	// arrived. Without this field the receiving side has to stamp the event with
	// its own clock, which quietly turns "the focus ended at 14:32" into "the
	// focus ended when I noticed".
	//
	// RFC 3339 WITH an offset rather than epoch milliseconds, deliberately: the
	// string then says which zone it is in, so nobody has to re-derive the zone
	// from whichever machine they happen to run on - the same class of bug as
	// assuming everything is UTC.
	At      string `json:"at"`
	Payload any    `json:"payload"`
}

// The market catalogue types (MarketEntry, PluginsView, MarketView) are gone with
// the plugin tab: the catalogue is fetched, rendered and acted on inside
// PetPCPluginMarket.exe, which calls internal/plugins directly. Keeping a second
// projection of the same catalogue here would have been a copy that only the
// deleted tab could read.

// --- host state ------------------------------------------------------------

// pluginHost is the plugin subsystem's mutable state that is NOT window
// geometry.
//
// It has its own mutex rather than sharing App.mu because a plugin request can
// block on the network (net.fetch, llm.chat), and holding the geometry lock for
// that long would stall a drag.
type pluginHost struct {
	mu      sync.Mutex
	manager *plugins.Manager
	// subs maps a plugin id to the set of events it subscribed to.
	subs map[string]map[string]bool
	// lastPhase is the last (phase, runState) pair published to the frontend. It
	// is what turns the once-a-second countdown tick into a phase-change event,
	// so a subscribed plugin is told about transitions rather than about every
	// number the countdown passed through.
	lastPhase string
	// keeper encrypts the plugin settings the manifest marks as secret. It is
	// created lazily with the manager so that a machine without a usable DPAPI
	// still starts the pet; without it, secret writes are REFUSED rather than
	// silently stored as plaintext.
	keeper plugins.SecretKeeper
	// ws owns the long-lived WebSocket connections plugins open through net.ws.
	// It lives here rather than in internal/plugins because it is the only part
	// of the plugin system that imports a third-party package (gorilla), and that
	// package's promise is standard-library-only.
	ws *pluginWSHub
	// childMu guards children: the programs plugins started, by pid. It is its own
	// mutex because process.state is a fast lookup that must not queue behind a
	// network call.
	childMu sync.Mutex
	// children maps a pid to the plugin that started it. Bounded by maxTrackedChildren.
	children map[int]*pluginChildProc
}

// managerLocked opens the plugin manager on first use.
//
// It is lazy so that neither NewApp nor the startup path can fail because of a
// plugin problem: a broken plugin index must never be able to stop the pet from
// appearing.
func (a *App) pluginManager() *plugins.Manager {
	a.pluginHost.mu.Lock()
	defer a.pluginHost.mu.Unlock()
	if a.pluginHost.manager == nil {
		m, err := plugins.NewManager(a.store.Dir())
		if err != nil {
			// A manager that cannot be opened still has to be returned, or
			// every caller would need a second error path. An empty manager
			// reports no plugins, which is the honest answer here.
			m = &plugins.Manager{}
		}
		// The base address is read through a function so the manager always
		// sees the CURRENT configuration: the user can change it from the
		// plugin tab, and the next catalogue fetch has to use the new value
		// without the manager being rebuilt.
		m.BaseURL = func() string { return a.store.Config().MarketBaseURL }
		a.pluginHost.manager = m
	}
	a.keeperLocked()
	return a.pluginHost.manager
}

// keeperLocked returns the host's secret keeper, creating it on first use. The
// caller must hold pluginHost.mu.
//
// It is split out of pluginManager because the keeper is needed by a caller that
// must NOT open the manager: the one-time migration re-encrypts the user's API
// key into the conversation plugin's private directory (app_plugin_migrate.go),
// and reading the plugin index is not part of that. Opening the manager there
// would also create plugins/ and plugins-tmp/ on a machine that may never
// install anything, which is the "the plugin subsystem costs nothing until it is
// used" property pluginManager's own comment is about.
func (a *App) keeperLocked() plugins.SecretKeeper {
	if a.pluginHost.keeper == nil {
		a.pluginHost.keeper = NewSecretKeeper()
	}
	return a.pluginHost.keeper
}

// pluginConfigStore opens the settings store for one installed plugin.
//
// The private directory and the manifest both come from the manager, so the pet
// and the plugin market are guaranteed to be looking at the SAME file: neither
// program computes the path itself. The keeper is always non-nil here, which is
// what lets a secret actually be stored; a nil keeper would make ConfigStore
// refuse the write, and that refusal is a bug in this wiring rather than
// something the user did.
func (a *App) pluginConfigStore(id string) (*plugins.ConfigStore, error) {
	row, ok := a.pluginManager().Get(id)
	if !ok {
		return nil, fmt.Errorf("插件 %s 未安装", id)
	}
	if row.Manifest == nil {
		return nil, fmt.Errorf("插件 %s 的 manifest 无法解析", id)
	}
	dir, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		return nil, err
	}
	return plugins.NewConfigStore(dir, *row.Manifest, a.pluginHost.keeper), nil
}

// pluginConfigValues returns one plugin's non-secret settings.
//
// It is used by the net.fetch allow-list, and it is deliberately the cheap,
// non-erroring form: a plugin whose config cannot be read simply has no
// configured hosts, which narrows what it may reach rather than widening it.
func (a *App) pluginConfigValues(id string) map[string]string {
	store, err := a.pluginConfigStore(id)
	if err != nil {
		return nil
	}
	return store.Values()
}

// --- bindings --------------------------------------------------------------

// The host exposes exactly ONE plugin binding: PluginsInstalled.
//
// Installing, uninstalling, enabling, granting and choosing a market address all
// live in PetPCPluginMarket.exe, which calls internal/plugins directly - so the
// seven bindings that used to do those things here (PluginsView, MarketView,
// InstallPlugin, UninstallPlugin, SetPluginEnabled, GrantPluginPermissions,
// SaveMarketURL) and the views only they could read are gone. What is left is
// the one thing the pet's own page genuinely needs and cannot get anywhere else.

// PluginsInstalled returns just the installed plugins, in plugins.json order.
//
// It is the binding the in-page surfaces resolve their plugin from: the chat
// slot asks who owns it, and the floating surface asks for the rectangles it
// must place its iframes at. Both need the CURRENT on-disk state, and both need
// the ORDER - "the first enabled and granted plugin claiming this slot" is the
// deterministic tie-break, so a call that reordered or cached the list would
// make slot ownership depend on when the page happened to ask.
//
// It is deliberately a fresh read rather than a projection of cached state:
// the plugin market is a separate process that can install, enable or grant at
// any moment, and the pet has to see that without being restarted.
func (a *App) PluginsInstalled() ([]PluginView, error) {
	m := a.pluginManager()
	rows := m.Plugins()
	out := make([]PluginView, 0, len(rows))
	live := make(map[string]bool, len(rows))
	for _, r := range rows {
		live[r.ID] = true
		out = append(out, pluginViewOf(r))
	}
	a.pruneSubscriptions(live)
	return out, nil
}

// pluginViewOf projects one manager row.
func pluginViewOf(r plugins.Installed) PluginView {
	v := PluginView{
		ID:          r.ID,
		Version:     r.Version,
		Enabled:     r.Enabled,
		Granted:     append([]string{}, r.Granted...),
		Permissions: []string{},
		Pending:     []string{},
		Dir:         r.Dir,
		Error:       r.Error,
	}
	if r.Manifest != nil {
		v.Name = r.Manifest.Name
		v.Author = r.Manifest.Author
		v.Description = r.Manifest.Description
		v.Permissions = append([]string{}, r.Manifest.Permissions...)
		v.NetHosts = append([]string{}, r.Manifest.NetHosts...)
		v.Panel = r.Manifest.Panel
		v.PanelSlot = r.Manifest.PanelSlot
		v.Surface = len(r.Manifest.Surface) > 0
		v.Background = r.Manifest.Background
		v.Entry = r.Manifest.Entry
		v.Pending = plugins.Pending(*r.Manifest, r.Granted)
	} else {
		// A row whose manifest cannot be read still has to name itself
		// something; the id is the only fact left.
		v.Name = r.ID
	}
	return v
}

// --- capability dispatch ---------------------------------------------------

// pluginMethod is one entry of the capability table.
type pluginMethod struct {
	// perm is the permission the caller must hold.
	perm string
	// also lists permissions the IMPLEMENTATION consults as a second gate. They
	// are not required to call the method - net.fetch works fine without them -
	// which is why they cannot live in perm; but they are still real gates the
	// user was asked about, so the completeness check counts them.
	//
	// Without this field `net.credentials` would be a vocabulary entry with no
	// method behind it, i.e. exactly the "switch that does nothing" the check
	// exists to forbid.
	also []string
	// run performs the call. payload is the raw JSON the plugin sent; a method
	// that takes no arguments ignores it.
	run func(a *App, id string, payload []byte) (any, error)
}

// pluginMethods is THE capability table: the only method names a plugin may
// call, and the only place a permission is attached to one.
//
// It is a map rather than a switch so the completeness check can walk it: a
// permission with no method behind it would be a switch the user is asked about
// that does nothing, and a method absent from here is simply not callable.
var pluginMethods = map[string]pluginMethod{
	// There are no todos.* methods any more either, and for the same reason as the
	// timer.* five below: the list is the todo plugin's own `storage`, and the two
	// other readers went to `plugin.share` (the pomodoro) or stopped reading it (the
	// chat plugin). A `todos.list` left here would gate a permission whose owner is
	// another plugin's file.
	//
	// There are no timer.* methods any more: timer.state / start / pause / resume /
	// stop drove the HOST's timer, and the pomodoro plugin counts its own phases in
	// its own state machine now (market/plugins/com.shixiansi.plugin-pomodoro, whose
	// manifest asks for storage + plugin.config + events instead). Keeping the five
	// would leave a permission gating methods no shipped plugin calls, which is what
	// TestCapabilityTableIsComplete exists to forbid.
	"events.subscribe": {perm: plugins.PermEvents, run: pluginEventsSubscribe},
	"bubble.say":       {perm: plugins.PermBubble, run: pluginBubbleSay},
	// net.fetch carries a SECOND gate: with `net.credentials` held it will fill
	// the Cookie header from one of the caller's declared secrets and accept a
	// checked Referer. The permission is listed here so the completeness check
	// sees it used (see pluginMethod.also).
	"net.fetch":      {perm: plugins.PermNetFetch, also: []string{plugins.PermNetCredentials}, run: pluginNetFetch},
	"storage.get":    {perm: plugins.PermStorage, run: pluginPrivateStore(plugins.StorageGet)},
	"storage.set":    {perm: plugins.PermStorage, run: pluginPrivateStore(plugins.StorageSet)},
	"storage.remove": {perm: plugins.PermStorage, run: pluginPrivateStore(plugins.StorageRemove)},
	// Where the caller's own private directory is. A plugin that starts a downloader has
	// to tell it where to write, and a chat protocol side has to be able to read the
	// result by path - neither is possible without knowing it. Own directory only, with
	// an optional `sub` folder inside it, so those products land in their own folder
	// instead of the root of the plugin's data.
	"storage.dir": {perm: plugins.PermStorage, run: pluginPrivateStore(plugins.StorageDir)},
	// How big one of the caller's own files is, in bytes.
	//
	// This exists because "did the download finish?" cannot be answered by a clock: a
	// 60 MB merge takes four minutes on one connection and forty seconds on another. The
	// size can be compared with the estimate the playurl response already gave, which is
	// how the reference implementation judges it (xunlu-core: `fs.statSync(...).size`).
	"storage.stat": {perm: plugins.PermStorage, run: pluginPrivateStore(plugins.StorageStat)},
	// What is actually in one of those folders.
	//
	// It exists because a folder a plugin owns can accumulate leftovers nothing references:
	// the QQ plugin's video ledger is cleared after ten minutes, and an older version
	// cleared it while leaving a 24 MB file behind on each run. "Clean up your own folder"
	// is impossible without being able to list it, short of deleting by a guessed pattern.
	"storage.list": {perm: plugins.PermStorage, run: pluginPrivateStore(plugins.StorageList)},
	// Renaming one of your own files.
	//
	// It exists because reorganising is not the same job as saving: a plugin that has to
	// move hundreds of megabytes of its own leftovers into a folder layout must be able
	// to rename them in place. Reading them back through the bridge as base64 and writing
	// them out again would be three times the bytes and twice the disk.
	"storage.move": {perm: plugins.PermStorage, run: pluginPrivateStore(plugins.StorageMove)},
	// The cross-plugin tray: writing is always your own name, reading needs the pair in
	// the manifest (see plugins.PermPluginShare and pluginShareGet).
	// The long-term memory document: one markdown file the MODEL maintains, inside the
	// caller's own private directory.
	//
	// Gated on `storage` rather than a permission of its own, because "I keep notes in
	// my own folder" is exactly what that permission already means. A separate switch
	// would be one the user cannot describe the difference of - they would be choosing
	// between two rows that both read "save data in your private directory".
	"memory.read":    {perm: plugins.PermStorage, run: pluginMemoryRead},
	"memory.append":  {perm: plugins.PermStorage, run: pluginMemoryAppend},
	"share.set":      {perm: plugins.PermPluginShare, run: pluginShareSet},
	"share.get":      {perm: plugins.PermPluginShare, run: pluginShareGet},
	"share.remove":   {perm: plugins.PermPluginShare, run: pluginShareRemove},
	"surface.show":   {perm: plugins.PermPetSurface, run: pluginSurfaceShow},
	"surface.hide":   {perm: plugins.PermPetSurface, run: pluginSurfaceHide},
	"surface.blocks": {perm: plugins.PermPetSurface, run: pluginSurfaceBlocks},
	"llm.chat":       {perm: plugins.PermLLMChat, run: pluginLLMChat},
	"llm.profile":    {perm: plugins.PermLLMBorrow, run: pluginLLMProfile},
	"config.get":     {perm: plugins.PermPluginConfig, run: pluginConfigGet},
	"config.set":     {perm: plugins.PermPluginConfig, run: pluginConfigSet},
	"audio.save":     {perm: plugins.PermAudio, run: pluginAudioSave},
	"audio.import":   {perm: plugins.PermAudio, run: pluginAudioImport},
	"audio.list":     {perm: plugins.PermAudio, run: pluginAudioList},
	"audio.remove":   {perm: plugins.PermAudio, run: pluginAudioRemove},
	"audio.play":     {perm: plugins.PermAudio, run: pluginAudioPlay},
	"audio.stop":     {perm: plugins.PermAudio, run: pluginAudioStop},
	"ws.open":        {perm: plugins.PermNetWS, run: pluginWSOpen},
	"ws.send":        {perm: plugins.PermNetWS, run: pluginWSSend},
	"ws.close":       {perm: plugins.PermNetWS, run: pluginWSClose},
	"ws.state":       {perm: plugins.PermNetWS, run: pluginWSState},
	"process.launch": {perm: plugins.PermProcess, run: pluginProcessLaunch},
	// Whether that program is still running, and how it ended. The pair is what lets a
	// plugin wait for a downloader without blocking its own frame or asking for a
	// filesystem capability.
	"process.state": {perm: plugins.PermProcess, run: pluginProcessState},
}

// PluginRequest is the ONE way a plugin reaches the host.
//
// Every capability is funnelled through here rather than through a binding per
// capability, so there is exactly one place that checks a permission and exactly
// one place that can be audited. The order of the checks is fixed and
// deliberate:
//
//  1. the plugin must be installed and enabled;
//  2. the method must exist;
//  3. the user must have granted the permission it needs;
//  4. only then are the arguments touched.
//
// A refusal at step 1-3 changes NOTHING: no todo is written, no bubble shown,
// no request sent, no index saved, no layout emitted and no private value
// stored. That is what makes "you declined this permission" mean something,
// rather than being a message printed after the work was already done.
func (a *App) PluginRequest(pluginID, method, payload string) (string, error) {
	row, ok := a.pluginManager().Get(pluginID)
	if !ok || !row.Enabled || row.Manifest == nil {
		return "", fmt.Errorf("插件 %s 未启用", pluginID)
	}
	spec, ok := pluginMethods[method]
	if !ok {
		return "", fmt.Errorf("插件 %s 调用了未知方法 %s", pluginID, method)
	}
	if !hasPermission(row.Granted, spec.perm) {
		return "", fmt.Errorf("插件 %s 未获权限 %s", pluginID, spec.perm)
	}
	out, err := spec.run(a, pluginID, []byte(payload))
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// hasPermission reports whether the user granted perm to that plugin.
//
// It is the ONE answer to that question in this package, and it forwards to
// plugins.HasGranted so that there is one answer in the codebase: internal/plugins
// uses it for the dispatcher's own gate, and this package uses it for two other
// places - the gate inside a method (the permissions pluginMethod.also lists, which
// the dispatcher must not require: net.fetch works without net.credentials, but the
// Cookie header is only filled for a caller that holds the second one), and the
// slot/surface resolvers, which ask "was this permission granted" before handing a
// plugin a piece of the pet.
//
// The forwarding is the point. There used to be three copies of this loop - one in
// dispatch.go, and two here (this one and pluginHoldsPermission) - and three copies
// of an access check is three answers waiting to disagree.
func hasPermission(granted []string, perm string) bool {
	return plugins.HasGranted(granted, perm)
}

// --- capability implementations --------------------------------------------

// No todo capabilities here any more. `todos.list/add/update/toggle/remove` were the
// host's todo store handed to plugins, and that store is the todo plugin's own now
// (its `storage`, key `todos-v1`): the last two readers moved off it - the pomodoro
// reads the snapshot published through `plugin.share`, and the chat plugin's three todo
// tools were deleted rather than re-pointed, because "the model may read the list but
// not change it" is the worse half of both features. With no method left, both
// permission words retired too (17 → 15 → 13); see capability.go.

// pluginTimerState and the four driving methods (timer.start/pause/resume/stop) used
// to live here. They are gone with the timer.* permissions: the pomodoro plugin owns
// its own state machine and records, so the host has no timer to hand out. Of the two
// conclusions the host used to publish, timer.changed is gone as well (nothing to
// broadcast); timer.phaseEnded stays as a name without an emitter - see
// notifyPluginPhaseEnded and openspec/changes/pomodoro-headless-timer.

// pluginMemoryRead returns the caller's memory document, or "" when it has none.
//
// Empty rather than an error: a plugin that holds the permission and has not been
// taught anything yet is the normal state of a fresh install.
func pluginMemoryRead(a *App, id string, _ []byte) (any, error) {
	dir, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		return nil, err
	}
	text, err := plugins.ReadMemory(dir)
	if err != nil {
		return nil, err
	}
	return map[string]any{"text": text}, nil
}

// pluginMemoryAppend adds one entry and returns the WHOLE document.
//
// The whole document, not an acknowledgement: it is what the model will see on its
// next turn, so handing it back saves a round trip that would only be spent reading
// what this call already had in hand.
func pluginMemoryAppend(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	dir, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		return nil, err
	}
	text, err := plugins.AppendMemory(dir, in.Text, time.Now())
	if err != nil {
		return nil, err
	}
	return map[string]any{"text": text}, nil
}

// pluginEventsSubscribe records which events the plugin wants.
//
// An unknown name is refused outright rather than filtered: a plugin that asked
// for something the host cannot send has a bug, and silently subscribing it to
// less than it asked for would hide the bug behind an interface that just never
// updates.
func pluginEventsSubscribe(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Names []string `json:"names"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	for _, n := range in.Names {
		if !plugins.IsKnownEvent(n) {
			return nil, fmt.Errorf("未知事件名 %s", n)
		}
	}
	a.pluginHost.mu.Lock()
	if a.pluginHost.subs == nil {
		a.pluginHost.subs = map[string]map[string]bool{}
	}
	set := a.pluginHost.subs[id]
	if set == nil {
		set = map[string]bool{}
		a.pluginHost.subs[id] = set
	}
	for _, n := range in.Names {
		set[n] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	a.pluginHost.mu.Unlock()
	return out, nil
}

// pluginBubbleSay makes the pet say a line.
//
// It goes through the ordinary showBubble path - the same call a reminder uses -
// so the bubble's geometry, its slot and its mutual exclusion with the panel and
// the overlay are the existing, already-verified behaviour rather than a plugin
// special case.
func pluginBubbleSay(a *App, _ string, payload []byte) (any, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, errors.New("bubble.say 需要 text")
	}
	// The bubble's STATE is set before the window is touched, so a geometry
	// failure (no native window, as in a windowless test) leaves the bubble
	// showing and correctly recorded in the layout - the only thing that failed
	// is the crop, and reporting that as "bubble.say failed" would tell the
	// plugin the opposite of what happened.
	if _, err := a.showBubble(&BubbleView{Kind: BubbleReminder, Title: text}); err != nil {
		a.DebugLog("plugin bubble: re-crop failed: " + err.Error())
	}
	return map[string]any{"ok": true}, nil
}

// netFetchHeaders is the closed set of request headers a plugin may set.
//
// It is a list rather than "anything the plugin wants" because a header is a way
// to say things the allow-list does not: Host picks a virtual host, Cookie and
// Origin carry ambient credentials, and Content-Length/Transfer-Encoding can
// contradict the body we are about to send. The four here are what a JSON API
// behind a token actually needs - content-type and authorization are the pair the
// Milky and OneBot APIs require - and nothing in the list can redirect the
// request away from the URL that was checked.
var netFetchHeaders = map[string]bool{
	"accept":        true,
	"authorization": true,
	"content-type":  true,
	"user-agent":    true,
}

// maxPluginHeaderValue bounds one header value.
const maxPluginHeaderValue = 1024

// applyPluginRequestHeader validates and sets one header.
//
// Refusal rather than dropping: a plugin that asked for a header it may not have
// and silently did not get it would build a request that fails at the far end for
// a reason nothing in this process knows about.
func applyPluginRequestHeader(header http.Header, name, value string) error {
	key := strings.ToLower(strings.TrimSpace(name))
	if !netFetchHeaders[key] {
		return fmt.Errorf("net.fetch 不允许设置请求头 %q（只支持 accept / authorization / content-type / user-agent）", name)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("net.fetch 的请求头 %s 含换行", key)
	}
	if len(value) > maxPluginHeaderValue {
		return fmt.Errorf("net.fetch 的请求头 %s 超过 %d 字节", key, maxPluginHeaderValue)
	}
	header.Set(key, value)
	return nil
}

// pluginNetFetch performs an HTTP request against the plugin's own allow-list.
//
// The host is checked against the manifest BEFORE the request is built, so a
// refused call never reaches the network at all: a plugin cannot use a rejected
// host as a probe that reveals whether something is listening.
//
// The reply body is text by default. `encoding: "base64"` asks for the bytes
// instead, and it exists because the text form is LOSSY for anything that is not
// valid UTF-8: the body travels to the plugin as a JSON string, and Go's JSON
// encoder replaces every invalid byte with U+FFFD. A 120 KB WAV sent that way comes
// back as the same length of replacement characters - it looks like it worked and
// plays as noise. Synthesis engines return exactly that (a local TTS service
// answers with audio/wav), so the plugin has to be able to ask for bytes.
// maxPluginFetchBytes bounds ONE response body.
//
// It is a per-request ceiling rather than a total, so it can be generous without becoming a
// place to hide unbounded work. It was 1 MiB until the QQ plugin's sticker library showed
// what that costs: every image larger than the cap was cut silently, and 156 of its 317
// stickers landed on exactly 1 MiB of base64 - i.e. half the library was a truncated
// animation that only looked fine. 8 MiB covers a phone photo and a long GIF, which is the
// widest thing a plugin here legitimately downloads, and `truncated` in the reply means the
// next ceiling cannot be invisible either.
const maxPluginFetchBytes = 8 << 20

func pluginNetFetch(a *App, id string, payload []byte) (any, error) {
	row, ok := a.pluginManager().Get(id)
	if !ok || row.Manifest == nil {
		return nil, fmt.Errorf("插件 %s 未启用", id)
	}
	var in struct {
		Method string `json:"method"`
		URL    string `json:"url"`
		Body   string `json:"body"`
		// Encoding is "" or "text" for the decoded body, "base64" for raw bytes.
		Encoding string `json:"encoding"`
		// Headers are the request headers a plugin may set. It exists because a
		// JSON API behind a bearer token cannot be called without both
		// content-type and authorization, and the Milky/OneBot APIs are exactly
		// that. The names are checked against a closed list (netFetchHeaders); a
		// plugin cannot invent one - and `cookie`/`referer` are NOT on that list,
		// because those are filled by the host (see Credentials below).
		Headers map[string]string `json:"headers"`
		// Credentials names a DECLARED SECRET config item of the caller whose
		// plaintext the host sends as the Cookie header. The value never enters
		// the payload, the reply, an error or a log line: a secret's plaintext
		// does not cross the bridge (ConfigStore.Read answers {set, mask}), so
		// naming a key is the only way a plugin can "use" one.
		Credentials string `json:"credentials"`
		// Referer is the page the request claims to come from. Bilibili's comment
		// endpoint checks it, and its host has to pass the SAME allow-list as the
		// URL - otherwise "only these hosts" would be a claim about one field of
		// the request rather than about where the request goes.
		Referer string `json:"referer"`
		// ReadCookies asks for the response's Set-Cookie values.
		//
		// It exists for exactly one flow that cannot be done any other way: B 站's QR
		// login answers "success" and hands the session over as cookies, with nothing
		// usable in the body. It needs net.credentials because a cookie is the same kind
		// of secret that permission lets the caller SEND; the reply carries only
		// `name=value` pairs - attributes (Path, Domain, HttpOnly, Max-Age) are dropped
		// here rather than handed to a page that has no use for them.
		ReadCookies bool `json:"readCookies"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	enc := strings.ToLower(strings.TrimSpace(in.Encoding))
	if enc != "" && enc != "text" && enc != "base64" {
		return nil, fmt.Errorf("net.fetch 的 encoding 只支持 text/base64：%s", in.Encoding)
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodPost {
		return nil, fmt.Errorf("net.fetch 只支持 GET/POST：%s", method)
	}
	// The allow-list is the manifest's static netHosts PLUS whatever the user
	// typed into the declared host-whitelist setting. Merging them here rather
	// than writing the configured hosts back into the manifest keeps the manifest
	// a pure declaration: the setting can change at any moment and the very next
	// request has to see the new value.
	//
	// It is computed ONCE, before the URL is even checked, because three checks
	// share it (the URL, the Referer and every redirect hop) and three copies are
	// three chances to disagree.
	hosts := append(append([]string{}, row.Manifest.NetHosts...),
		plugins.ConfigHosts(*row.Manifest, a.pluginConfigValues(id))...)
	if u, err := url.Parse(in.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("net.fetch 只支持 http/https：%s", in.URL)
	} else if !plugins.HostAllowed(hosts, u.Host) {
		return nil, fmt.Errorf("net.fetch 未声明的域名：%s", u.Host)
	}
	// Checked BEFORE the request, like the credential header above: a caller that may not
	// read cookies should not get to ask the far end for them either.
	if in.ReadCookies && !hasPermission(row.Granted, plugins.PermNetCredentials) {
		return nil, fmt.Errorf("net.fetch 读取 Set-Cookie 需要 %s 权限", plugins.PermNetCredentials)
	}

	var body io.Reader
	if in.Body != "" {
		body = strings.NewReader(in.Body)
	}
	req, err := http.NewRequest(method, in.URL, body)
	if err != nil {
		return nil, err
	}
	// Applied BEFORE the request is sent and AFTER the host check, so a refused
	// host never gets the credential a header may carry.
	for name, value := range in.Headers {
		if err := applyPluginRequestHeader(req.Header, name, value); err != nil {
			return nil, err
		}
	}
	if err := applyPluginCredentials(a, row, req, in.Credentials, in.Referer, hosts); err != nil {
		return nil, err
	}
	// The allow-list is re-checked on every redirect hop.
	//
	// Without this the check would only cover the FIRST URL: Go follows up to ten
	// redirects by default, so a listed host answering 302 to anywhere is a way to
	// reach an unlisted one - and to carry the Authorization header added above, or
	// the Cookie header applyPluginCredentials added, to it. That was already true
	// before headers existed, but a one-shot GET leaks nothing while a credential
	// does.
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("net.fetch 重定向次数过多")
			}
			if !plugins.HostAllowed(hosts, next.URL.Host) {
				return fmt.Errorf("net.fetch 的重定向指向未声明的域名：%s", next.URL.Host)
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// Read one byte past the cap so a response that fits exactly is not reported as
	// truncated, and one that does not is reported rather than silently cut.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPluginFetchBytes+1))
	if err != nil {
		return nil, err
	}
	truncated := len(data) > maxPluginFetchBytes
	if truncated {
		data = data[:maxPluginFetchBytes]
	}
	out := map[string]any{"status": resp.StatusCode}
	if truncated {
		// The caller has to KNOW. Before this flag existed a 1 MiB cap quietly cut every
		// larger picture: the QQ plugin's sticker library ended up with 156 of 317 images
		// at exactly 1 MiB of base64, i.e. half the library was a broken animation, and
		// nothing anywhere said so.
		out["truncated"] = true
	}
	if enc == "base64" {
		// base64 of the RAW bytes, which is the only form a JSON string can carry
		// without loss. The plugin decodes it and hands the bytes to audio.save.
		out["body"] = base64.StdEncoding.EncodeToString(data)
		out["encoding"] = "base64"
	} else {
		out["body"] = string(data)
	}
	if in.ReadCookies {
		out["cookies"] = setCookiePairs(resp.Header.Values("Set-Cookie"))
	}
	return out, nil
}

// setCookiePairs reduces Set-Cookie lines to `name=value`.
//
// Bounded on both axes because the caller is a plugin: at most 20 cookies of at most 512
// bytes, which is far more than a login flow needs and far less than a header a hostile
// server could make expensive.
func setCookiePairs(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if i := strings.IndexByte(line, ';'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=") {
			continue
		}
		if len(line) > 512 {
			line = line[:512]
		}
		out = append(out, line)
		if len(out) >= 20 {
			break
		}
	}
	return out
}

// applyPluginCredentials fills the Cookie header from one of the caller's own
// declared secrets, and sets a checked Referer.
//
// # WHY THE HOST DOES THIS AND THE PLUGIN DOES NOT
//
// A secret's plaintext never crosses the bridge: config.get answers {set, mask}
// and MaskSecret hides short values outright (internal/plugins/config.go). So
// "let net.fetch carry a cookie header" would not have let a plugin use its own
// login - it would have pushed every author towards the one workaround that DOES
// copy the plaintext into a sandboxed page. The payload therefore names a CONFIG
// KEY and the host does the reading, which is the same split llm.chat already
// uses: the plugin supplies the message, the host supplies the credential.
//
// It needs its own permission because "reach this API" and "speak as me on this
// API" are different things to consent to. Both headers are set AFTER the
// allow-list has been checked and BEFORE the request is sent, so a refused host
// never sees a credential, and CheckRedirect keeps it from following a 302 to a
// host the manifest never declared.
func applyPluginCredentials(a *App, row plugins.Installed, req *http.Request, credentials, referer string, hosts []string) error {
	if credentials == "" && referer == "" {
		return nil
	}
	if !hasPermission(row.Granted, plugins.PermNetCredentials) {
		// Refusal, not silent dropping: a request that quietly went out without
		// its cookie would fail at the far end for a reason nothing here knows.
		return fmt.Errorf("net.fetch 携带凭据或 referer 需要 %s 权限", plugins.PermNetCredentials)
	}
	if credentials != "" {
		if row.Manifest == nil {
			return fmt.Errorf("插件 %s 的 manifest 无法解析", row.ID)
		}
		item, ok := row.Manifest.ConfigItem(credentials)
		if !ok || !item.Secret {
			// Names the KEY, never a value. The key is what the author wrote and
			// what the user saw in the settings sheet; a value appearing here
			// would mean a credential in a message that ends up in debug.log.
			return fmt.Errorf("net.fetch 的凭据必须是插件声明过的 secret 配置项：%s", credentials)
		}
		store, err := a.pluginConfigStore(row.ID)
		if err != nil {
			return err
		}
		secret := store.PlainSecret(credentials)
		if secret == "" {
			return fmt.Errorf("net.fetch 的凭据 %s 还没有保存", credentials)
		}
		// A stored secret is user-typed text that becomes a header value, so it
		// gets the same newline refusal as every plugin-supplied header: a bare
		// CR/LF in it would otherwise let a second header ride along.
		if strings.ContainsAny(secret, "\r\n") {
			return fmt.Errorf("net.fetch 的凭据 %s 含换行", credentials)
		}
		req.Header.Set("Cookie", secret)
	}
	if referer != "" {
		u, err := url.Parse(referer)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("net.fetch 的 referer 必须是 http(s) 地址：%s", referer)
		}
		if !plugins.HostAllowed(hosts, u.Host) {
			return fmt.Errorf("net.fetch 的 referer 指向未声明的域名：%s", u.Host)
		}
		req.Header.Set("Referer", referer)
	}
	return nil
}

// flatNamePattern is the only shape a FLAT plugin-chosen name may take: an audio
// take's name, a share tray's name.
//
// It is deliberately narrow: such a name becomes a file name directly under the
// plugin's own directory, so a name containing a separator or ".." would be a way out
// of it, and there is no legitimate reason for one that needs a separator. Storage
// keys are the exception and are validated by internal/plugins (see
// plugins.StorageKeyPath): those may name folders, because a plugin that keeps a
// sticker library and a video cache needs somewhere to put them.
var flatNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// pluginPrivateStore adapts one shared private-store method to this table's shape.
//
// The implementations themselves live in internal/plugins because the market program
// answers the same `storage.*` calls over the same directory - one implementation is
// what stops a key being legal in one process and refused in the other, which the QQ
// plugin would hit immediately (its bot runs in the pet, its sticker workshop runs in
// the market).
func pluginPrivateStore(run func(dir string, payload []byte) (any, error)) func(*App, string, []byte) (any, error) {
	return func(a *App, id string, payload []byte) (any, error) {
		dir, err := a.pluginManager().PrivateDir(id)
		if err != nil {
			return nil, err
		}
		return run(dir, payload)
	}
}

// pluginSurfaceShow opens the plugin's floating surface.
//
// It calls the SAME function the OpenPluginSurface binding does rather than
// growing a second switch for the capability: two ways to open one surface is
// two ways for the state and the window region to disagree.
func pluginSurfaceShow(a *App, id string, _ []byte) (any, error) {
	if _, err := a.OpenPluginSurface(id); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
} // pluginSurfaceHide closes the floating surface.
func pluginSurfaceHide(a *App, id string, _ []byte) (any, error) {
	if _, err := a.ClosePluginSurface(); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// pluginSurfaceBlocks shows or hides blocks of the OPEN floating surface.
//
// The payload carries DECLARATION INDICES only - never coordinates, never sizes. A plugin
// that could submit geometry could cover the whole screen and the taskbar, which is not
// something "the user granted pet.surface" can be stretched to mean: the rectangles stay a
// manifest decision, and this only picks a subset of them to show.
//
// It exists because a surface block cannot appear on demand otherwise. The window region is
// cropped to the declared rectangles the moment the surface opens, so a block that should
// only exist after the user sends a message has to be declared up front - and then it is
// there while idle, transparent but still swallowing every click in its area.
func pluginSurfaceBlocks(a *App, id string, payload []byte) (any, error) {
	var req struct {
		Hidden []int `json:"hidden"`
		// JSON object keys are strings, so the declaration index arrives as one. Converted below
		// rather than declared as a map[int]int, which encoding/json refuses to fill.
		Sizes map[string]int `json:"sizes"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, err
		}
	}
	sizes := make(map[int]int, len(req.Sizes))
	for key, h := range req.Sizes {
		index, convErr := strconv.Atoi(key)
		if convErr != nil || index < 0 || h <= 0 {
			continue // junk keys are ignored, not fatal: the declaration is the authority
		}
		sizes[index] = h
	}
	if _, err := a.SetPluginSurfaceBlocks(id, req.Hidden, sizes); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// SetPluginSurfaceBlocks replaces the hidden-block set of the surface open for pluginID and
// re-applies the window geometry, then pushes the new layout to the page so the frames for
// the hidden blocks are unmounted (a frame nobody asked for is a frame that eats clicks).
//
// Indices outside the declaration are simply never matched by computePluginSurface, so an
// out-of-range value is a no-op rather than an error: the declaration is the authority.
func (a *App) SetPluginSurfaceBlocks(pluginID string, hidden []int, sizes map[int]int) (Layout, error) {
	a.mu.Lock()
	cur := a.pluginSurface
	a.mu.Unlock()
	if cur == nil || cur.PluginID != pluginID {
		return Layout{}, fmt.Errorf("surface is not open for %s", pluginID)
	}
	set := make(map[int]bool, len(hidden))
	for _, i := range hidden {
		if i < 0 {
			continue
		}
		set[i] = true
	}
	a.surfaceHiddenMu.Lock()
	if len(set) == 0 {
		a.surfaceHiddenBlocks = nil
	} else {
		a.surfaceHiddenBlocks = set
	}
	if len(sizes) == 0 {
		a.surfaceBlockSizes = nil
	} else {
		a.surfaceBlockSizes = sizes
	}
	a.surfaceHiddenMu.Unlock()
	return a.applySurfaceBlocksThrottled()
}

// applySurfaceBlocksThrottled re-crops the window at most twice a second.
//
// The caller is a plugin revealing a reply one character at a time: without a floor on the
// interval, every character would recompute the region, re-crop the window with SetWindowRgn and
// push a new layout to the page. The LAST request wins, which is what makes coalescing safe -
// heights only ever describe the current content.
func (a *App) applySurfaceBlocksThrottled() (Layout, error) {
	const minInterval = 500 * time.Millisecond
	a.surfaceHiddenMu.Lock()
	since := time.Since(a.surfaceSizesAt)
	if since >= minInterval {
		a.surfaceSizesAt = time.Now()
		a.surfaceHiddenMu.Unlock()
		return a.applySurfaceBlocksNow()
	}
	wait := minInterval - since
	alreadyQueued := a.surfaceSizesNext != nil
	a.surfaceHiddenMu.Unlock()
	if alreadyQueued {
		return a.currentLayout(), nil
	}
	a.surfaceHiddenMu.Lock()
	a.surfaceSizesNext = time.AfterFunc(wait, func() {
		a.surfaceHiddenMu.Lock()
		a.surfaceSizesNext = nil
		a.surfaceSizesAt = time.Now()
		a.surfaceHiddenMu.Unlock()
		if _, err := a.applySurfaceBlocksNow(); err != nil {
			log.Printf("pet: apply surface blocks: %v", err)
		}
	})
	a.surfaceHiddenMu.Unlock()
	return a.currentLayout(), nil
}

func (a *App) applySurfaceBlocksNow() (Layout, error) {
	layout, err := a.applyWindowGeometry()
	if err != nil {
		return layout, err
	}
	a.emitLayout()
	return layout, nil
}

// requestedSurfaceSizes hands the geometry function a copy of the requested heights.
func (a *App) requestedSurfaceSizes() map[int]int {
	a.surfaceHiddenMu.Lock()
	defer a.surfaceHiddenMu.Unlock()
	if len(a.surfaceBlockSizes) == 0 {
		return nil
	}
	out := make(map[int]int, len(a.surfaceBlockSizes))
	for k, v := range a.surfaceBlockSizes {
		out[k] = v
	}
	return out
}

// hiddenSurfaceBlocks hands computePluginSurface a copy of the hidden set: that function is
// pure, and reading App state from inside it would make the geometry un-testable.
func (a *App) hiddenSurfaceBlocks() map[int]bool {
	a.surfaceHiddenMu.Lock()
	defer a.surfaceHiddenMu.Unlock()
	if len(a.surfaceHiddenBlocks) == 0 {
		return nil
	}
	out := make(map[int]bool, len(a.surfaceHiddenBlocks))
	for k, v := range a.surfaceHiddenBlocks {
		out[k] = v
	}
	return out
}

// clearHiddenSurfaceBlocks resets the set. Called when a surface OPENS, so a plugin that
// hid a block last time cannot come back with it missing: "nothing hidden" is the default
// every plugin that never calls surface.blocks gets.
func (a *App) clearHiddenSurfaceBlocks() {
	a.surfaceHiddenMu.Lock()
	a.surfaceHiddenBlocks = nil
	a.surfaceBlockSizes = nil
	if a.surfaceSizesNext != nil {
		a.surfaceSizesNext.Stop()
		a.surfaceSizesNext = nil
	}
	a.surfaceHiddenMu.Unlock()
}

// --- audio ------------------------------------------------------------------

// AudioSink plays and stops audio files.
//
// It is the seam between the plugin capability and the operating system. The
// production implementation forwards to internal/desktop - the SAME winmm wrapper
// the pet has always used, so there is no second audio path - while a test
// substitutes a recorder to observe what a plugin asked for without making a
// sound.
type AudioSink interface {
	PlayWAVFile(path string) error
	StopAudio()
}

// desktopAudio is the production sink.
type desktopAudio struct{}

func (desktopAudio) PlayWAVFile(path string) error { return desktop.PlayWAVFile(path) }
func (desktopAudio) StopAudio()                    { desktop.StopAudio() }

// audioPlayer returns the sink plugins play through, creating the default one on
// first use.
//
// It is separate from the retired voiceState.player on purpose: the voice
// subsystem is going away, and the capability must not depend on it. A plugin
// plays a file it stored itself; nothing here knows what a line is.
func (a *App) audioPlayer() AudioSink {
	a.audioMu.Lock()
	defer a.audioMu.Unlock()
	if a.audioOut == nil {
		a.audioOut = desktopAudio{}
	}
	return a.audioOut
}

//
// A plugin's audio is its OWN material, kept beside its own settings: the host
// knows how to store bytes and how to play a file, and nothing else. It has no
// idea what a line is, which character speaks it, or when it should be said -
// those are the plugin's business, which is the whole point of moving speech out
// of the host.
//
// It is a separate area from storage rather than a mode of it. storage.set takes
// a JSON STRING (that is what a plugin's own small values are), so putting a WAV
// through it would mean base64 in both directions - the audio crossing
// postMessage twice, at 4/3 size - to accomplish "write these bytes, later play
// them". Different data, different shape, different permission.

// audioNamePattern is the only shape an audio slot name may take.
//
// The SAME pattern as a flat storage name, and for the same reason: the name becomes a
// file name inside the plugin's private directory, so a separator or a ".." would be a
// way out of it. Reusing the rule rather than writing a second one keeps "what a legal
// private name looks like" a single fact. Storage KEYS are the exception - they may
// name folders, and plugins.StorageKeyPath is their validator.
var audioNamePattern = flatNamePattern

// pluginAudioDir returns the plugin's audio directory, creating it on demand.
func (a *App) pluginAudioDir(id string) (string, error) {
	dir, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		return "", err
	}
	audioDir := filepath.Join(dir, "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		return "", err
	}
	return audioDir, nil
}

// pluginAudioImport copies one file from the plugin's OWN PACKAGE into its audio
// directory.
//
// It is a convenience with a measurable benefit rather than a workaround for a
// blocked path: the plugin COULD fetch its own package (`/plugin-asset/<id>/...`
// sets Access-Control-Allow-Origin, so the opaque-origin frame can read it) and
// then hand the bytes to audio.save. That route pushes the whole clip through the
// bridge twice - once as an ArrayBuffer, once as base64 in a request - which for
// the 569 KB of built-in clips means roughly 1.5 MB of postMessage traffic per
// install to accomplish "copy these five files". Here the host reads and writes
// them directly and the plugin only names them.
//
// It grants no new access. The plugin may already read every file in its package
// through the asset route, and it may already write its own audio directory through
// audio.save; this is exactly those two abilities composed, with the source
// confined to the plugin's own directory by ValidRelPath and by the same
// containment check the asset route applies - plus a rule the asset route does NOT
// have, which is the manifest declaration checked below.
func pluginAudioImport(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Name string `json:"name"`
		// From is a path RELATIVE TO THE PLUGIN'S OWN PACKAGE.
		From string `json:"from"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	if !audioNamePattern.MatchString(in.Name) {
		return nil, fmt.Errorf("audio 名称非法：%q", in.Name)
	}
	if !plugins.ValidRelPath(in.From) {
		return nil, fmt.Errorf("audio.import 的 from 不是合法的相对路径：%q", in.From)
	}
	pkgDir, err := a.pluginManager().PluginDir(id)
	if err != nil {
		return nil, err
	}
	// The manifest must have DECLARED the file. Without this a plugin could import
	// its own source files into the audio directory and play them, which is not a
	// privilege escalation but is certainly not "the package's audio assets" - and
	// the declaration is what makes the set of importable files auditable.
	row, ok := a.pluginManager().Get(id)
	if !ok || row.Manifest == nil {
		return nil, fmt.Errorf("插件 %s 未启用", id)
	}
	if !declaresAsset(*row.Manifest, in.From) {
		return nil, fmt.Errorf("manifest 的 assets 未声明 %q", in.From)
	}

	src := filepath.Join(pkgDir, filepath.FromSlash(in.From))
	absPkg, err := filepath.Abs(pkgDir)
	if err != nil {
		return nil, err
	}
	absSrc, err := filepath.Abs(src)
	if err != nil || !strings.HasPrefix(absSrc, absPkg+string(filepath.Separator)) {
		return nil, fmt.Errorf("audio.import 的 from 越出了插件目录：%q", in.From)
	}
	raw, err := os.ReadFile(absSrc)
	if err != nil {
		return nil, fmt.Errorf("读不到包内文件 %q：%w", in.From, err)
	}
	if len(raw) > maxAudioBytes {
		return nil, fmt.Errorf("audio.import 的文件超过上限 %d 字节", maxAudioBytes)
	}
	dir, err := a.pluginAudioDir(id)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, in.Name+".wav")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	return map[string]any{"name": in.Name, "bytes": len(raw)}, nil
}

// declaresAsset reports whether the manifest listed rel among its assets.
func declaresAsset(man plugins.Manifest, rel string) bool {
	for _, a := range man.Assets {
		if a == rel {
			return true
		}
	}
	return false
}

// pluginAudioSave writes one audio file.
func pluginAudioSave(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Name string `json:"name"`
		// Data is base64 because the payload is JSON and a WAV is binary.
		Data string `json:"data"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	if !audioNamePattern.MatchString(in.Name) {
		return nil, fmt.Errorf("audio 名称非法：%q", in.Name)
	}
	if in.Data == "" {
		return nil, errors.New("audio.save 需要 data")
	}
	raw, err := base64.StdEncoding.DecodeString(in.Data)
	if err != nil {
		return nil, fmt.Errorf("audio.save 的 data 不是合法 base64：%w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("audio.save 解出了空音频")
	}
	if len(raw) > maxAudioBytes {
		return nil, fmt.Errorf("audio.save 的音频超过上限 %d 字节", maxAudioBytes)
	}
	dir, err := a.pluginAudioDir(id)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, in.Name+".wav")
	// Written via a temporary file and renamed, so a crash mid-write cannot leave
	// a truncated clip that plays as noise - the same reasoning plugin usage and
	// the store's own saves use.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	return map[string]any{"name": in.Name, "bytes": len(raw)}, nil
}

// pluginAudioList lists the plugin's audio files, by name.
func pluginAudioList(a *App, id string, _ []byte) (any, error) {
	dir, err := a.pluginAudioDir(id)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".wav") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, map[string]any{
			"name":  strings.TrimSuffix(e.Name(), ".wav"),
			"bytes": info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["name"].(string) < out[j]["name"].(string)
	})
	return out, nil
}

// pluginAudioRemove deletes one audio file.
func pluginAudioRemove(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	if !audioNamePattern.MatchString(in.Name) {
		return nil, fmt.Errorf("audio 名称非法：%q", in.Name)
	}
	dir, err := a.pluginAudioDir(id)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(filepath.Join(dir, in.Name+".wav")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// pluginAudioPlay plays one audio file.
//
// It goes through internal/desktop - the SAME winmm wrapper the pet has always
// used - rather than growing a second audio path. The file is played BY PATH
// because that is what the wrapper takes, and because the in-memory form has a
// documented failure mode (the buffer must outlive asynchronous playback and the
// GC does not know that).
func pluginAudioPlay(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	if !audioNamePattern.MatchString(in.Name) {
		return nil, fmt.Errorf("audio 名称非法：%q", in.Name)
	}
	dir, err := a.pluginAudioDir(id)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, in.Name+".wav")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("没有这段音频：%s", in.Name)
	}
	if err := a.audioPlayer().PlayWAVFile(path); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// pluginAudioStop stops playback.
func pluginAudioStop(a *App, _ string, _ []byte) (any, error) {
	a.audioPlayer().StopAudio()
	return map[string]any{"ok": true}, nil
}

// maxAudioBytes bounds one saved clip. It is generous for a spoken line (the
// embedded ones are ~120 KB) while still refusing a plugin that tries to fill the
// user's disk through one call.
const maxAudioBytes = 16 << 20 // 16 MiB

// pluginConfigGet returns the plugin's own declared settings.
//
// The shape is exactly ConfigValues: non-secret values verbatim, secrets as
// {set, mask} and nothing else. That is not a convention this function follows,
// it is the only shape the type has - there is no field on ConfigValues that
// could carry a plaintext secret, so "a plugin cannot read back the API key it
// stored" is a property of the data, not of this code being careful.
//
// The plugin can therefore tell WHETHER a secret is configured (so it can say
// "fill in your key") without ever being able to send it anywhere.
func pluginConfigGet(a *App, id string, _ []byte) (any, error) {
	store, err := a.pluginConfigStore(id)
	if err != nil {
		return nil, err
	}
	return store.Read()
}

// pluginConfigSet writes the plugin's own declared settings.
//
// Only keys the manifest declares are accepted, and the check happens inside
// ConfigStore before anything is written, so a payload carrying one undeclared
// key stores NOTHING - the same all-or-nothing rule GrantPermissions follows for
// permissions it was not asked about.
//
// The most important line here is the one that is absent: the permission check
// already happened in PluginRequest, BEFORE this function was called and before
// the payload was even parsed. A plugin without plugin.config cannot reach this
// code, so it cannot store a setting through a rejected call.
func pluginConfigSet(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Values map[string]string `json:"values"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	if len(in.Values) == 0 {
		return map[string]any{"ok": true}, nil
	}
	store, err := a.pluginConfigStore(id)
	if err != nil {
		return nil, err
	}
	if err := store.Write(in.Values); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// --- cross-plugin sharing ---------------------------------------------------

// maxPluginShareValue bounds one shared value.
//
// 64 KiB is far more than a job list or a play-URL answer needs and far less than
// something a plugin could hide behind. The bound exists because these values are the
// only text that travels BETWEEN plugins: an unbounded channel would be a way to move
// arbitrary blobs through the host.
const maxPluginShareValue = 64 << 10

// shareFileName is where one published value lives: in the `share` FOLDER of the
// PUBLISHER's private directory, so "may write" is a directory that plugin already
// owns.
//
// The folder rather than the directory root is 1.11.0's change: these files are the
// HOST's business, not the plugin's, and mixing them into the root made a plugin's own
// data ("stickers/", "history/") and its tray look like the same pile. A value written
// before that change is still read - see pluginShareGet - so an upgrade mid-conversation
// loses nothing.
func shareFileName(name string) string { return filepath.Join("share", name) }

// legacyShareFileName is where a published value lived before the share/ folder.
func legacyShareFileName(name string) string { return "share-" + name }

// pluginShareSet publishes one value under the caller's own name.
//
// Writing your own values needs no declaration, and writing anyone else's is impossible:
// the directory is the caller's, so the "only your own prefix" rule is enforced by the
// filesystem rather than by a string check that could be got wrong.
func pluginShareSet(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	if !flatNamePattern.MatchString(in.Name) {
		return nil, fmt.Errorf("share 的 name 非法：%q", in.Name)
	}
	if len(in.Value) > maxPluginShareValue {
		return nil, fmt.Errorf("share 的值超过 %d 字节：%d", maxPluginShareValue, len(in.Value))
	}
	dir, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, shareFileName(in.Name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(in.Value), 0o644); err != nil {
		return nil, err
	}
	// The value now lives in share/; the pre-1.11 file, if any, is a duplicate of it.
	_ = os.Remove(filepath.Join(dir, legacyShareFileName(in.Name)))
	return map[string]any{"ok": true, "key": plugins.SharedKey(id, in.Name)}, nil
}

// pluginShareGet reads one value another plugin published, if the caller declared it.
//
// Reading your own values needs no declaration (they are your files). Reading someone
// else's needs the exact pair in the caller's manifest, and the refusal names the pair
// and the field: a bare "403" in a plugin log helps nobody.
func pluginShareGet(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Owner string `json:"owner"`
		Name  string `json:"name"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	owner := strings.TrimSpace(in.Owner)
	if owner == "" {
		owner = id
	}
	if !flatNamePattern.MatchString(in.Name) {
		return nil, fmt.Errorf("share 的 name 非法：%q", in.Name)
	}
	if !plugins.IsValidID(owner) {
		return nil, fmt.Errorf("share 的 owner 非法：%q", owner)
	}
	if owner != id {
		row, ok := a.pluginManager().Get(id)
		if !ok || row.Manifest == nil {
			return nil, fmt.Errorf("插件 %s 未启用", id)
		}
		if !plugins.DeclaredShareRead(row.Manifest.ShareRead, owner, in.Name) {
			return nil, fmt.Errorf("插件 %s 没有声明要读 %s：manifest 的 shareRead 里没有这一项",
				id, plugins.SharedKey(owner, in.Name))
		}
	}
	dir, err := a.pluginManager().PrivateDir(owner)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, shareFileName(in.Name))
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// A value published before share/ existed. Reading it where it is costs one
		// stat and keeps an upgrade invisible; moving it is best effort, because the
		// only thing that matters here is the bytes the caller asked for.
		legacy := filepath.Join(dir, legacyShareFileName(in.Name))
		if raw, legacyErr := os.ReadFile(legacy); legacyErr == nil {
			data, err = raw, nil
			if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
				_ = os.Rename(legacy, path)
			}
		}
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// "Nobody published that yet" is an EMPTY value, not an error: every consumer
			// hits it on its first poll, and treating it as a failure would fill the log
			// with noise from the normal case.
			return map[string]any{"value": "", "at": 0, "key": plugins.SharedKey(owner, in.Name)}, nil
		}
		return nil, err
	}
	if len(data) > maxPluginShareValue {
		return nil, fmt.Errorf("%s 的值超过 %d 字节，拒绝读取", plugins.SharedKey(owner, in.Name), maxPluginShareValue)
	}
	var at int64
	if info, statErr := os.Stat(path); statErr == nil {
		at = info.ModTime().UnixMilli()
	}
	return map[string]any{"value": string(data), "at": at, "key": plugins.SharedKey(owner, in.Name)}, nil
}

// pluginShareRemove deletes one of the caller's OWN published values.
//
// Only its own: a consumer clearing someone else's job would make a shared key whose
// content depends on which plugin ran last, which is the coupling this tray exists to
// avoid.
func pluginShareRemove(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	if !flatNamePattern.MatchString(in.Name) {
		return nil, fmt.Errorf("share 的 name 非法：%q", in.Name)
	}
	dir, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(filepath.Join(dir, shareFileName(in.Name))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// A leftover from before share/: removing "my value" has to remove it too, or a
	// clear would appear to succeed while a stale copy stays readable.
	if err := os.Remove(filepath.Join(dir, legacyShareFileName(in.Name))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// pluginLLMChat sends one message to the model the CALLING PLUGIN configured -
// or, when it declares the llm.borrow permission and its own settings are
// incomplete, to the model another plugin lent it (see resolvePluginChatCredential).
//
// Every parameter comes from that plugin's own declared settings: the endpoint,
// the model, the sampling knobs and the credential. The host supplies only the
// things a plugin must not be trusted with - the daily budget and the usage
// counter - and the transport itself (internal/llm, unchanged).
//
// `history` and `system` were added for the group-chat bridge, which needs two
// things a single user message cannot express: several turns of the conversation
// so far, and a prompt that says who it is talking to. See buildPluginChatMessages
// for why an explicit system prompt deliberately DROPS the pet's world snapshot.
//
// The reply is streamed to the requesting plugin as pet:plugin-event frames
// carrying llm.delta / llm.done / llm.error, and the returned value is the
// finished text.
//
// The reply is deliberately NOT written to any transcript: the plugin owns its
// own history (through storage.get/set on the key "llm-history"), and the pet
// writing one too would be two writers for one conversation - with the pet's
// copy invisible to the only UI that shows it.
func pluginLLMChat(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Text      string       `json:"text"`
		RequestID string       `json:"requestId"`
		System    string       `json:"system"`
		History   []pluginTurn `json:"history"`
		// Model overrides the configured model for THIS call only. It exists for
		// vision: a plugin sends pictures to a vision model and plain text to the
		// chat model without maintaining two endpoints, two keys and two budgets.
		Model string `json:"model"`
		// Images are absolute http(s) URLs for the model to look at.
		Images []string `json:"images"`
		// Tools are NAMES from the host's catalog (app_plugin_tools.go), not schemas.
		// The host builds what the model sees, so a tool cannot be described as
		// something other than what it does, and one the user did not grant is never
		// offered at all.
		Tools []string `json:"tools"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	text := clampPluginText(strings.TrimSpace(in.Text), maxPluginChatText)
	if text == "" {
		return nil, errors.New("llm.chat 需要 text")
	}

	row, ok := a.pluginManager().Get(id)
	if !ok || row.Manifest == nil {
		return nil, fmt.Errorf("插件 %s 未启用", id)
	}
	dir, err := a.pluginManager().PrivateDir(id)
	if err != nil {
		return nil, err
	}
	cfgStore, err := a.pluginConfigStore(id)
	if err != nil {
		return nil, err
	}

	cred, err := a.resolvePluginChatCredential(id, row, cfgStore)
	if err != nil {
		return nil, err
	}
	cfg, key := cred.cfg, cred.key
	if model := strings.TrimSpace(in.Model); model != "" {
		// No separate credential or budget: the model name is the ONLY thing a
		// vision call changes, so it cannot become a way around the endpoint, the
		// key or the daily cap.
		cfg.LLMModel = clampPluginText(model, 96)
	}
	images, err := a.pluginChatImages(id, *row.Manifest, in.Images)
	if err != nil {
		return nil, err
	}
	if cred.lender != "" {
		// One line in debug.log, because "which endpoint did that reply come from"
		// is otherwise unanswerable when two plugins can supply it.
		a.DebugLog(fmt.Sprintf("plugin llm: %s 借用 %s 的模型配置（%s）", id, cred.lender, cred.lenderName))
	}

	// The budget is the host's, not the plugin's: a plugin that could raise its
	// own cap would not have one. The counter lives in the plugin's private
	// directory so it travels with the plugin's settings, but the HOST is its
	// only writer.
	now := time.Now()
	usage := readPluginUsage(dir, now)
	if usage.OverBudget(cfg.LLMDailyBudget) {
		// The refused attempt is still counted. The budget bounds ATTEMPTS, and
		// a caller sitting on the boundary must not be able to keep asking for
		// free - the same rule the pet's own counter has always followed.
		refused := usage.PlusAttempt(0, 0)
		if err := writePluginUsage(dir, refused); err != nil {
			a.DebugLog("plugin llm: could not record usage: " + err.Error())
		}
		return nil, fmt.Errorf("已达今日请求上限（%d 次）", cfg.LLMDailyBudget)
	}
	// Counted when the request is SENT, not when it succeeds: a request that
	// fails still cost a round trip.
	sent := usage.PlusAttempt(0, 0)
	if err := writePluginUsage(dir, sent); err != nil {
		a.DebugLog("plugin llm: could not record usage: " + err.Error())
	}

	requestID := in.RequestID
	if requestID == "" {
		requestID = newID()
	}

	// There is no todo snapshot to offer any more, so there is no second gate here.
	//
	// It used to be `cfg.LLMContextTodos && hasPermission(row.Granted, todos.read)`: the
	// user's global switch AND this plugin's permission, because the snapshot was the
	// user's own list and "any plugin with llm.chat may see your tasks" is not a
	// permission model. Both halves are gone - the list moved into the todo plugin, and
	// with no method behind `todos.read` the permission itself retired. A plugin's
	// system prompt is now the only context a plugin conversation gets, and the world
	// snapshot says plainly that the list and the timer are not visible from here.
	msgs := buildPluginChatMessages(a, cfg, in.System, in.History, text)
	if len(images) > 0 {
		// The pictures belong to the question, i.e. the LAST message. Attaching them
		// to an earlier turn would ask the model to look at them "again" and some
		// gateways reject an image part on a non-final user message outright.
		last := len(msgs) - 1
		if last >= 0 {
			msgs[last].Images = images
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handlers := llm.Handlers{
		OnText: func(s string) {
			a.emitPluginEvent(id, "llm.delta", map[string]any{"requestId": requestID, "text": s})
		},
	}
	settings := a.llmSettings(cfg, key)

	// send performs ONE round and accounts for it.
	//
	// The budget counts ROUNDS rather than questions, deliberately: a round that ran
	// tools really did cost a round trip, and a request that silently spends four of
	// them while the counter moves by one would be a cap that does not bound what it
	// claims to. Tokens are added on top of a FRESH read for the reason the counter
	// already had: another attempt may have been counted while this one was in flight.
	send := func(round []llm.Message, tools []llm.ToolSpec) (llm.Result, error) {
		res, err := a.llmClient().ChatWithTools(ctx, settings, round, tools, handlers)
		if res.Usage != nil {
			latest := readPluginUsage(dir, time.Now())
			addReportedTokens(&latest, res.Usage.PromptTokens, res.Usage.CompletionTokens)
			if werr := writePluginUsage(dir, latest); werr != nil {
				a.DebugLog("plugin llm: could not record tokens: " + werr.Error())
			}
		}
		return res, err
	}
	fail := func(err error) (any, error) {
		message, kind := describeChatError(err)
		a.emitPluginEvent(id, "llm.error", map[string]any{"requestId": requestID, "message": message, "kind": kind})
		return nil, errors.New(message)
	}
	finish := func(text string) (any, error) {
		// The delta frames carried the text as it arrived; the done frame carries the
		// WHOLE reply, so a plugin that dropped a frame can repair itself instead of
		// leaving a hole in the middle of a sentence.
		a.emitPluginEvent(id, "llm.done", map[string]any{"requestId": requestID, "text": text})
		return map[string]any{"requestId": requestID, "text": text}, nil
	}

	specs, err := pluginToolSpecs(a, row.Granted, in.Tools)
	if err != nil {
		return nil, err
	}

	// No usable tools: exactly the request this method has always made.
	if len(specs) == 0 {
		res, err := send(msgs, nil)
		if err != nil {
			return fail(err)
		}
		return finish(res.Text)
	}

	// With tools the model may ASK instead of answering, and the loop that runs them
	// lives here rather than in the plugin: the plugin asked one question and gets
	// one stream, and it never sees a tool round at all.
	for round := 0; round < maxToolRounds; round++ {
		res, err := send(msgs, specs)
		if err != nil {
			return fail(err)
		}
		if len(res.ToolCalls) == 0 {
			return finish(res.Text)
		}

		// The assistant turn has to carry the tool_calls VERBATIM: the ids in the
		// results below refer to them, and a gateway that cannot match the two
		// rejects the whole request.
		msgs = append(msgs, llm.Message{Role: "assistant", Content: res.Text, ToolCalls: res.ToolCalls})
		for _, call := range res.ToolCalls {
			out, runErr := a.runPluginTool(id, requestID, call)
			if runErr != nil {
				// A failed tool goes BACK TO THE MODEL instead of failing the request:
				// a wrong argument or an unknown id is something it can correct, and
				// one bad guess should not cost the user their whole question.
				failed, _ := json.Marshal(map[string]any{"ok": false, "error": runErr.Error()})
				out = string(failed)
			}
			msgs = append(msgs, llm.Message{Role: "tool", ToolCallID: call.ID, Content: out})
		}
	}

	// Out of rounds. Not a protocol error: the model kept asking for tools instead of
	// answering, and saying so is better than paying for another round forever.
	limit := fmt.Sprintf("模型连续调用工具超过 %d 轮，已停止。", maxToolRounds)
	a.emitPluginEvent(id, "llm.error", map[string]any{
		"requestId": requestID, "message": limit, "kind": "tool_rounds",
	})
	return nil, errors.New(limit)
}

// pluginChatConfig builds the model settings for one request from the calling
// plugin's OWN declared settings.
//
// It starts from the ZERO store.Config, never from the pet's. The pet's own
// llm* values are a migration source and nothing else: reading them here would
// silently resurrect the retired behaviour the first time a user edited one, and
// would make the pet's settings file a second, invisible configuration for a
// plugin that believes it owns its endpoint.
//
// An item with no stored value falls back to the default its manifest declares,
// which is the value the market program's editor would have saved had the user
// opened the form. Without that fallback a plugin installed and used straight
// away would refuse with "not configured" while its own manifest already says
// where to send the request.
func pluginChatConfig(man plugins.Manifest, values map[string]string) store.Config {
	var cfg store.Config

	raw := func(key string) string {
		if v, ok := values[key]; ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		if item, ok := man.ConfigItem(key); ok {
			return strings.TrimSpace(item.Default)
		}
		return ""
	}
	num := func(key string) int {
		n, _ := strconv.Atoi(raw(key))
		return n
	}
	flt := func(key string) float64 {
		f, _ := strconv.ParseFloat(raw(key), 64)
		return f
	}

	// Exactly the fields the request path reads. llmEnabled, llmProactive and
	// llmProactiveMinGapMin are still DECLARED by the manifest - the one-time
	// migration carries the user's old values into them - but they have no
	// host-side meaning any more: the plugin's own enabled state is the switch,
	// and the proactive schedule was removed with the rest of the host's chat UI.
	cfg.LLMProvider = raw("llmProvider")
	cfg.LLMBaseURL = raw("llmBaseUrl")
	cfg.LLMModel = raw("llmModel")
	cfg.LLMTemperature = flt("llmTemperature")
	cfg.LLMMaxTokens = num("llmMaxTokens")
	cfg.LLMTimeoutSec = num("llmTimeoutSec")
	cfg.LLMSystemPrompt = raw("llmSystemPrompt")
	cfg.LLMHistoryTurns = num("llmHistoryTurns")
	cfg.LLMDailyBudget = num("llmDailyBudget")
	return cfg
}

// --- conversation shape -----------------------------------------------------

// pluginTurn is one replayed message in an llm.chat call.
//
// The text field is `text` rather than `content` because every other plugin-facing
// payload in this file calls the string "text" (bubble.say, audio names, storage
// values), and a second name for the same thing is a second thing to remember.
type pluginTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// The caps on one call's conversation.
//
// They exist because the caller is a plugin: `history` is whatever it kept on
// disk, and a bridge that saved a week of group chat would otherwise turn every
// reply into a full-context bill. The numbers are per-call, not per-day - the
// daily budget is a separate mechanism and stays the host's.
const (
	// maxPluginChatText bounds the new user message.
	maxPluginChatText = 4000
	// maxPluginChatSystem bounds an explicit system prompt.
	maxPluginChatSystem = 4000
	// maxPluginChatTurns bounds how many replayed messages are accepted. The
	// OLDEST are dropped, so the turns nearest the new message survive.
	maxPluginChatTurns = 32
	// maxPluginChatHistoryRunes bounds the total size of the replayed turns, for
	// the same reason turns are bounded: 32 entries of 4000 characters is still
	// too much to send by accident.
	maxPluginChatHistoryRunes = 12000
)

// clampPluginText trims a string to at most n runes.
//
// Runes rather than bytes: the truncation is shown to a model and to users, and
// cutting a multi-byte character in half produces mojibake that looks like a
// protocol bug.
func clampPluginText(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// sanitizePluginHistory validates and truncates the replayed conversation.
//
// Unknown roles are DROPPED rather than coerced: `system` from a plugin would be a
// second system prompt after the real one (and the honest way to send one is the
// `system` field, which is clamped and placed first), and anything else is a
// provider error waiting to happen. Empty turns are dropped for the same reason
// the pet's own history skips them - an empty assistant message reads to the model
// as "it said nothing", which it then tries to repair.
func sanitizePluginHistory(turns []pluginTurn) []llm.Message {
	if len(turns) == 0 {
		return nil
	}
	start := 0
	if len(turns) > maxPluginChatTurns {
		start = len(turns) - maxPluginChatTurns
	}
	out := make([]llm.Message, 0, len(turns)-start)
	budget := maxPluginChatHistoryRunes
	for i := len(turns) - 1; i >= start; i-- {
		role := strings.ToLower(strings.TrimSpace(turns[i].Role))
		if role != "user" && role != "assistant" {
			continue
		}
		text := strings.TrimSpace(turns[i].Text)
		if text == "" {
			continue
		}
		text = clampPluginText(text, maxPluginChatText)
		if len([]rune(text)) > budget {
			continue
		}
		budget -= len([]rune(text))
		out = append(out, llm.Message{Role: role, Content: text})
	}
	// Built backwards (newest first) so the budget is spent on what is nearest the
	// new message; reversed here so the provider sees chronological order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// buildPluginChatMessages assembles the message list for one plugin request.
//
// TWO MODES, and the difference is a privacy decision rather than a formatting
// one:
//
//   - No `system`: the pet's usual prompt path, which means the persona from the
//     plugin's settings PLUS the world snapshot (time, todos, pomodoro). That is
//     what the conversation panel wants.
//   - `system` given: that prompt and nothing else. The snapshot is deliberately
//     NOT added, because the caller is answering people who are not the user - a
//     group chat - and "以下是当前的真实情况" would send the user's todo list and
//     pomodoro state to everyone in the room. A plugin that asks for its own
//     system prompt is saying "I know what context belongs here"; the host does
//     not second-guess that, it just refuses to add the user's day to it.
func buildPluginChatMessages(a *App, cfg store.Config, system string, history []pluginTurn, text string) []llm.Message {
	turns := sanitizePluginHistory(history)
	system = clampPluginText(strings.TrimSpace(system), maxPluginChatSystem)
	if system != "" {
		msgs := make([]llm.Message, 0, len(turns)+2)
		msgs = append(msgs, llm.Message{Role: "system", Content: system})
		msgs = append(msgs, turns...)
		msgs = append(msgs, llm.Message{Role: "user", Content: text})
		return msgs
	}
	return llm.BuildMessages(a.promptContext(cfg), append(turns, llm.Message{Role: "user", Content: text}))
}

// pluginChatCredential is the model settings ONE llm.chat call runs with, plus
// where they came from.
type pluginChatCredential struct {
	cfg store.Config
	// key is the plaintext credential. It never leaves this process: it is handed
	// to the transport and to nothing else.
	key string
	// lender is the id of the plugin whose settings were borrowed, "" for own.
	lender string
	// lenderName is that plugin's display name, for the debug log and the UI.
	lenderName string
}

// pluginOwnChatCredential builds a plugin's own settings and reports whether they
// are complete enough to send a request.
//
// "Complete" is checked here rather than at the call site because it is the same
// question the borrow path asks about the LENDER: one predicate for "this plugin
// could answer right now", so a lender that would fail is never chosen.
func pluginOwnChatCredential(man plugins.Manifest, cfgStore *plugins.ConfigStore) (pluginChatCredential, bool) {
	cfg := pluginChatConfig(man, cfgStore.Values())
	key := ""
	secretName, hasSecret := pluginSecretItem(man)
	if hasSecret {
		key = cfgStore.PlainSecret(secretName)
	}
	// Refusals name the two different problems separately at the call site; here we
	// only answer "ready or not".
	if cfg.LLMBaseURL == "" || cfg.LLMModel == "" {
		return pluginChatCredential{cfg: cfg, key: key}, false
	}
	if hasSecret && key == "" && pluginNeedsKey(cfg.LLMProvider) {
		return pluginChatCredential{cfg: cfg, key: key}, false
	}
	return pluginChatCredential{cfg: cfg, key: key}, true
}

// resolvePluginChatCredential decides which model settings one call uses.
//
// The rule is OWN FIRST, then borrow, and it is written that way so a user can
// always opt out of borrowing by filling in the borrower's own settings. Borrowing
// additionally requires the caller to hold llm.borrow - the host does not grant
// itself the right to read one plugin's credential on another plugin's behalf.
//
// A lender must be installed, ENABLED, and declare `shareLLM`; among several, the
// lowest id wins, so the answer does not depend on install order (which is not
// visible anywhere the user can see it).
func (a *App) resolvePluginChatCredential(id string, row plugins.Installed, cfgStore *plugins.ConfigStore) (pluginChatCredential, error) {
	own, ready := pluginOwnChatCredential(*row.Manifest, cfgStore)
	if ready {
		return own, nil
	}
	if !hasPermission(row.Granted, plugins.PermLLMBorrow) {
		// The wording names the two different problems separately, because "the
		// address is blank" and "the key is missing" need different actions from
		// the user and a single message would leave them guessing which one it was.
		if own.cfg.LLMBaseURL == "" || own.cfg.LLMModel == "" {
			return pluginChatCredential{}, fmt.Errorf("「%s」还没配置端点与模型，请在插件市场里填好", row.Manifest.Name)
		}
		return pluginChatCredential{}, errors.New("还没有填写 API Key")
	}
	lender := a.findLLMLender(id)
	if lender == nil {
		return pluginChatCredential{}, fmt.Errorf("「%s」还没有可用的大模型配置：它自己没填，也没有其它插件声明可以借出（shareLLM）", row.Manifest.Name)
	}
	return *lender, nil
}

// findLLMLender returns the first plugin that lends its model settings and can
// actually answer right now.
func (a *App) findLLMLender(exclude string) *pluginChatCredential {
	rows := a.pluginManager().Plugins()
	ids := make([]string, 0, len(rows))
	byID := make(map[string]plugins.Installed, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		byID[r.ID] = r
	}
	sort.Strings(ids)
	for _, id := range ids {
		if id == exclude {
			continue
		}
		r := byID[id]
		if !r.Enabled || r.Manifest == nil || !r.Manifest.ShareLLM {
			continue
		}
		store, err := a.pluginConfigStore(id)
		if err != nil {
			continue
		}
		cred, ready := pluginOwnChatCredential(*r.Manifest, store)
		if !ready {
			continue
		}
		cred.lender = id
		cred.lenderName = r.Manifest.Name
		return &cred
	}
	return nil
}

// pluginLLMProfile reports where this plugin's model settings would come from.
//
// It exists so a plugin's own UI can say "用「对话插件」的配置（deepseek-chat）"
// instead of guessing, and it is the method behind the llm.borrow permission: a
// permission with no method behind it would be a switch the user is asked about
// that does nothing. Nothing here reveals the endpoint or the credential of the
// lender - only that it exists, what it is called, and which model it names.
func pluginLLMProfile(a *App, id string, _ []byte) (any, error) {
	row, ok := a.pluginManager().Get(id)
	if !ok || row.Manifest == nil {
		return nil, fmt.Errorf("插件 %s 未启用", id)
	}
	cfgStore, err := a.pluginConfigStore(id)
	if err != nil {
		return nil, err
	}
	own, ownReady := pluginOwnChatCredential(*row.Manifest, cfgStore)
	allowed := hasPermission(row.Granted, plugins.PermLLMBorrow)

	out := map[string]any{
		"ownReady":      ownReady,
		"borrowAllowed": allowed,
		"ready":         ownReady,
		"source":        "none",
		"model":         own.cfg.LLMModel,
	}
	if ownReady {
		out["source"] = "own"
		return out, nil
	}
	if !allowed {
		return out, nil
	}
	if lender := a.findLLMLender(id); lender != nil {
		out["ready"] = true
		out["source"] = "borrowed"
		out["model"] = lender.cfg.LLMModel
		out["lender"] = map[string]any{
			"id":   lender.lender,
			"name": lender.lenderName,
		}
	}
	return out, nil
}

// pluginChatImages validates the picture URLs one llm.chat call may send.
//
// The rule is the net.fetch allow-list (manifest netHosts plus the host-whitelist
// setting), NOT a separate one: "where may this plugin reach" is one question, and a
// second answer to it would eventually disagree - with the disagreement being a plugin
// that can download a picture but not upload it, or vice versa.
//
// The count is capped because every picture is billed as tokens on every turn: four is
// already a lot of image, and a plugin that wanted more is describing a screenshot
// gallery rather than a group-chat question.
func (a *App) pluginChatImages(id string, man plugins.Manifest, raw []string) ([]string, error) {
	const maxChatImages = 4
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > maxChatImages {
		return nil, fmt.Errorf("llm.chat 一次最多 %d 张图片，收到 %d 张", maxChatImages, len(raw))
	}
	hosts := append(append([]string{}, man.NetHosts...),
		plugins.ConfigHosts(man, a.pluginConfigValues(id))...)
	out := make([]string, 0, len(raw))
	for _, candidate := range raw {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		u, err := url.Parse(trimmed)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("llm.chat 的图片地址必须是 http/https：%s", trimmed)
		}
		if !plugins.HostAllowed(hosts, u.Host) {
			return nil, fmt.Errorf("llm.chat 的图片来自未声明的域名：%s", u.Host)
		}
		out = append(out, trimmed)
	}
	return out, nil
}

// pluginSecretItem returns the key of the first secret setting a plugin
// declares, which is the one its credential is stored under.
//
// It is discovered from the manifest rather than hard-coded to "llmApiKey": the
// host must not know which plugin is asking (see the design's C6), and a plugin
// that names its credential differently is describing the same thing.
func pluginSecretItem(man plugins.Manifest) (string, bool) {
	for _, item := range man.Config {
		if item.Secret {
			return item.Key, true
		}
	}
	return "", false
}

// pluginNeedsKey reports whether an endpoint identified by a provider preset
// requires a credential.
//
// The answer comes from the preset the PLUGIN configured, so a local server that
// accepts any placeholder is not forced to store a key; an unknown or absent
// provider is treated as needing one, which is the safe direction - it asks for
// a credential rather than sending an unauthenticated request.
func pluginNeedsKey(provider string) bool {
	if preset, ok := store.LLMProviderByID(provider); ok {
		return preset.NeedsKey
	}
	return true
}

// pluginUsageFile is the host-owned usage counter's name inside a plugin's
// private directory.
//
// It is a bare name rather than a ".json" file because it is not part of any
// contract with the plugin: the plugin reads it, if it wants to, through
// storage.get, and only the host ever writes it.
const pluginUsageFile = "llm-usage"

// readPluginUsage loads a plugin's usage counter, already rolled over to now.
//
// An absent or unparseable file is a fresh counter rather than an error: it is
// regenerable state, and refusing to answer the user because a counter could not
// be read would be the wrong trade - the same position the pet's own counter
// takes.
func readPluginUsage(dir string, now time.Time) store.LLMUsage {
	var u store.LLMUsage
	raw, err := os.ReadFile(filepath.Join(dir, pluginUsageFile))
	if err == nil {
		if uerr := json.Unmarshal(raw, &u); uerr != nil {
			u = store.LLMUsage{}
		}
	}
	// The day-rollover rule is shared with the pet's counter through this one
	// exported function (T75): two implementations would eventually disagree
	// about when a day starts, and the disagreement would show up as a budget
	// that resets at the wrong moment.
	return u.RolledTo(now)
}

// writePluginUsage replaces the counter in one step (temp file in the same
// directory, then rename), so the market program - a second process that reads
// this directory - never sees a half-written file.
func writePluginUsage(dir string, u store.LLMUsage) error {
	data, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(dir, pluginUsageFile)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// addReportedTokens folds a server's token report into u.
//
// The "a zero count means the server did not report usage" rule is the one
// store.LLMUsage.PlusAttempt applies (T75). It is repeated as two lines here
// rather than exported a second time because the shared export exists for the
// DAY ROLLOVER - the rule two implementations can silently disagree about - and
// a token count of zero is unambiguous. The attempt counter is deliberately
// untouched: this request was counted when it was sent.
func addReportedTokens(u *store.LLMUsage, promptTokens, completionTokens int) {
	if promptTokens > 0 {
		u.PromptTokens += promptTokens
	}
	if completionTokens > 0 {
		u.CompletionTokens += completionTokens
	}
}

// --- process ----------------------------------------------------------------

// The bounds process.launch enforces. Every one of them is refused rather than
// clamped: a plugin that asked to start something slightly too big has a bug, and
// starting a DIFFERENT program than the one it named would be worse than a refusal.
const (
	// maxLaunchArgs bounds the argument list. A protocol side takes a handful of
	// flags; 32 is far past any real invocation while still being a number the host
	// can state in its refusal.
	maxLaunchArgs = 32
	// maxLaunchArgLen bounds ONE argument, in characters.
	//
	// 512 was the first number, chosen for an access token and a file path. The real
	// machine showed why that is too small: a signed B 站 CDN URL is 600-700 characters,
	// so a downloader could be launched but never pointed at anything. 4096 covers a
	// signed URL with room to spare and is still a number the host can state.
	maxLaunchArgLen = 4096
	// maxLaunchArgsTotal bounds the WHOLE command line, because Windows caps it at 32767
	// and 32 arguments x 4096 would sail past that. Raising the per-argument limit without
	// this would trade one refusal for a mysterious "the program started and did nothing".
	maxLaunchArgsTotal = 16384
	// maxLaunchFileBytes bounds the executable itself. The pet refuses to start a
	// file it cannot plausibly be pointing at by hand - a 512 MiB "exe" is not a
	// program the user chose from a file dialog, and reading its size is the only
	// inspection done here at all.
	maxLaunchFileBytes = 512 << 20 // 512 MiB
)

// pluginProcessLaunch starts one program on this machine and returns its pid.
//
// It does NOT wait for the child: cmd.Start() and a Process.Release() is the whole
// lifecycle, so the program is decoupled from the pet and keeps running when the
// pet exits. That is the point - the QQ plugin starts a chat-protocol side it then
// talks to over a WebSocket, and a child that died with the pet (or blocked it)
// would be useless. Nothing here reads the child's output, forwards a signal to it
// or records it, so there is no second `exec` path to keep in step with anything.
//
// The validation is deliberately by-the-book Chinese and deliberately BEFORE
// cmd.Start(): every refusal names the field that was wrong, because the plugin
// shows this sentence to the user in its own log ("启动客户端失败：…") and "invalid
// argument" would tell them nothing about which of a path and four arguments is at
// fault.
func pluginProcessLaunch(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Path string `json:"path"`
		// Args is optional. A nil list and an empty list are the same thing here.
		Args []string `json:"args"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}

	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, errors.New("process.launch 需要 path")
	}
	// An absolute path is required and is not a formality: a relative one would be
	// resolved against the pet's own working directory, so the same manifest setting
	// would start a different program depending on how the pet was launched.
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("process.launch 的 path 必须是绝对路径：%q", in.Path)
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("process.launch 找不到文件：%s", path)
		}
		return nil, fmt.Errorf("process.launch 读不到文件 %s：%v", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("process.launch 的 path 是目录而不是文件：%s", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("process.launch 的 path 不是普通文件：%s", path)
	}
	// EqualFold rather than a lower-cased comparison on the whole path: Windows
	// uppercases extensions as readily as it lowercases them, and only the EXTENSION
	// is case-insensitive in the sense that matters here.
	if !strings.EqualFold(filepath.Ext(path), ".exe") {
		return nil, fmt.Errorf("process.launch 只允许启动 .exe：%s", path)
	}
	if info.Size() > maxLaunchFileBytes {
		return nil, fmt.Errorf("process.launch 的文件超过上限 %d 字节：%s", maxLaunchFileBytes, path)
	}
	if len(in.Args) > maxLaunchArgs {
		return nil, fmt.Errorf("process.launch 的 args 最多 %d 个，收到 %d 个", maxLaunchArgs, len(in.Args))
	}
	argChars := 0
	for i, arg := range in.Args {
		// A newline in an argument is refused rather than escaped. exec builds a
		// command line, not a shell script, so a newline cannot be quoted safely -
		// and there is no legitimate flag that contains one.
		if strings.ContainsAny(arg, "\r\n") {
			return nil, fmt.Errorf("process.launch 的第 %d 个参数含换行", i+1)
		}
		argChars += len([]rune(arg))
		if len([]rune(arg)) > maxLaunchArgLen {
			return nil, fmt.Errorf("process.launch 的第 %d 个参数超过 %d 字符", i+1, maxLaunchArgLen)
		}
	}
	if argChars > maxLaunchArgsTotal {
		return nil, fmt.Errorf("process.launch 的参数总长度 %d 超过上限 %d 字符（Windows 命令行有硬上限）",
			argChars, maxLaunchArgsTotal)
	}

	cmd := exec.Command(path, in.Args...)
	// The child's working directory is its own folder, not the pet's.
	//
	// This is not a nicety: a protocol side (Yogurt, and most of its peers) loads its
	// account files, config and logs RELATIVE to the current directory. Inheriting the
	// pet's cwd made such a program start and exit within a second, which looked exactly
	// like "the launch did nothing" from the plugin's side.
	cmd.Dir = filepath.Dir(path)
	// HideWindow on Windows (see pluginLaunchAttr): these are console programs, so
	// without it every launch paints a black box over the user's desktop.
	cmd.SysProcAttr = pluginLaunchAttr()
	// No stdin/stdout/stderr: nil means /dev/null for the child, so a program that
	// waits for input or floods its log cannot hold the pet, and no pipe of ours can
	// keep the child alive after the pet exits.
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 %s 失败：%v", path, err)
	}
	pid := cmd.Process.Pid
	// Released, not Waited: releasing the handle is what makes the child independent.
	// Waited in a goroutine rather than released.
	//
	// `Release` was the first version, and it was right about what it protected: the
	// plugin request must not block until the program exits, and the child must survive
	// the pet. A goroutine gets both AND keeps the one fact `Release` threw away -
	// whether and how it ended. That fact is what lets a plugin tell "still working" from
	// "finished" without a filesystem capability it does not have (the video bridge's
	// downloader is the motivating case).
	a.pluginHost.trackChild(id, pid)
	go func() {
		a.pluginHost.finishChild(pid, cmd.Wait())
	}()
	return map[string]any{"pid": pid}, nil
}

// --- child bookkeeping for process.state -------------------------------------

// childProc is one program a plugin started, as far as the host needs to remember it.
type childProc struct {
	startedAt time.Time
	done      bool
	exitCode  int
}

// pluginChildProc pairs a child with its owner: the pid is guessable, and "which process
// is that" is not information to hand to whoever asks.
type pluginChildProc struct {
	proc     *childProc
	pluginID string
}

// maxTrackedChildren bounds the table. A plugin that starts a hundred programs is not a
// use case, and forgetting the oldest entry keeps a stuck one from growing this forever.
const maxTrackedChildren = 32

func (h *pluginHost) trackChild(pluginID string, pid int) *childProc {
	h.childMu.Lock()
	defer h.childMu.Unlock()
	if h.children == nil {
		h.children = map[int]*pluginChildProc{}
	}
	if len(h.children) >= maxTrackedChildren {
		oldest := 0
		var oldestAt time.Time
		for key, child := range h.children {
			if oldest == 0 || child.proc.startedAt.Before(oldestAt) {
				oldest = key
				oldestAt = child.proc.startedAt
			}
		}
		delete(h.children, oldest)
	}
	proc := &childProc{startedAt: time.Now(), exitCode: -1}
	h.children[pid] = &pluginChildProc{proc: proc, pluginID: pluginID}
	return proc
}

func (h *pluginHost) finishChild(pid int, err error) {
	h.childMu.Lock()
	defer h.childMu.Unlock()
	child, ok := h.children[pid]
	if !ok {
		return
	}
	child.proc.done = true
	child.proc.exitCode = 0
	if err != nil {
		child.proc.exitCode = -1
		if exitErr, isExit := err.(*exec.ExitError); isExit {
			child.proc.exitCode = exitErr.ExitCode()
		}
	}
}

// pluginProcessState reports whether a program this plugin started is still running.
//
// It exists so a plugin can wait for a downloader without a filesystem capability and
// without blocking its own frame: it asks every couple of seconds instead of holding a
// request open for a minute.
func pluginProcessState(a *App, id string, payload []byte) (any, error) {
	var in struct {
		PID int `json:"pid"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	if in.PID <= 0 {
		return nil, errors.New("process.state 需要 pid")
	}
	a.pluginHost.childMu.Lock()
	defer a.pluginHost.childMu.Unlock()
	child, ok := a.pluginHost.children[in.PID]
	if !ok || child.pluginID != id {
		// Not an error: a pid from a previous run of the pet is simply unknown, and the
		// caller should read that as "that process is gone".
		return map[string]any{"known": false, "running": false, "exitCode": -1}, nil
	}
	if !child.proc.done {
		return map[string]any{"known": true, "running": true, "exitCode": -1}, nil
	}
	return map[string]any{
		"known":    true,
		"running":  false,
		"exitCode": child.proc.exitCode,
		"ranMs":    time.Since(child.proc.startedAt).Milliseconds(),
	}, nil
}

// decodePayload unmarshals a plugin payload. An empty payload is an empty
// object rather than an error, because several methods take no arguments.
func decodePayload(payload []byte, v any) error {
	if len(strings.TrimSpace(string(payload))) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, v); err != nil {
		return fmt.Errorf("参数不是合法的 JSON：%w", err)
	}
	return nil
}

// --- event down-link -------------------------------------------------------

// emitPluginEvent sends ONE pet:plugin-event carrying a PluginEventView.
//
// The event name is written as a double-quoted literal at every call site - it
// is deliberately NOT behind a constant - because the frontend's gate scans Go
// source for the literal string. An event emitted through a constant reads to
// that check as an identifier, i.e. as an event nobody ever sends, and the
// frontend's subscription to it would be reported as dangling.
func (a *App) emitPluginEvent(pluginID, name string, payload any) {
	if a.ctx == nil && a.emitFn == nil {
		return
	}
	// Stamped HERE rather than by the caller: every event goes through this one
	// function, so one line covers the whole stream. A caller that had to pass
	// the time would be a caller that can forget to.
	a.emitEvent("pet:plugin-event", PluginEventView{
		PluginID: pluginID,
		Name:     name,
		At:       time.Now().Format(time.RFC3339),
		Payload:  payload,
	})
}

// emitPluginStateChange delivers a state-change event to every plugin that
// subscribed to it.
func (a *App) emitPluginStateChange(name string, payload any) {
	a.pluginHost.mu.Lock()
	targets := make([]string, 0, len(a.pluginHost.subs))
	for id, set := range a.pluginHost.subs {
		if set[name] {
			targets = append(targets, id)
		}
	}
	a.pluginHost.mu.Unlock()
	for _, id := range targets {
		a.emitPluginEvent(id, name, payload)
	}
}

// notifyPluginPhaseEnded tells subscribers a pomodoro phase just FINISHED.
//
// NOTHING IN THE HOST CALLS IT ANY MORE, and that is a known gap rather than an
// oversight. The only emitter was the host's own timer, which is gone: the pomodoro
// plugin counts its own phases, and a plugin cannot raise a host event. So a plugin
// that subscribes to timer.phaseEnded today will never hear it - the voice plugin's
// "番茄钟结束" line is silent.
//
// It is kept, with its event still in SubscribableEvents, because the real fix is a
// headless timer inside the pomodoro plugin (then `plugin.share` carries the fact and the
// voice plugin polls it) - see openspec/changes/pomodoro-headless-timer. Deleting the
// pair now would also break every installed voice plugin's subscribe call, which
// validates the name.
//
// The payload is a bare fact and no wording: which line to say, in which voice,
// is the plugin's business.
//
// The event name is a double-quoted LITERAL because .dev/check-frontend.cjs scans Go
// source for it to prove every subscription has a matching emitter; a constant would
// read as an identifier and the subscription would be reported as dangling.
func (a *App) notifyPluginPhaseEnded(kind string) {
	a.emitPluginStateChange("timer.phaseEnded", map[string]any{"phase": kind})
}

// notifyPluginReminderFired tells subscribers a reminder just came due.
//
// Same reasoning as the phase event: the host has already decided that a reminder
// is due and counted what it covers, so the numbers travel and the wording does
// not. A plugin that speaks or plays a sound reads them; one that does nothing
// simply never subscribes.
func (a *App) notifyPluginReminderFired(due, overdue, total int) {
	a.emitPluginStateChange("reminder.fired", map[string]any{
		"due":     due,
		"overdue": overdue,
		"total":   total,
	})
}

// pruneSubscriptions drops the subscription sets of plugins that are no longer
// installed.
//
// It used to be a targeted forget, called by UninstallPlugin - which is exactly
// the kind of bookkeeping that breaks once the mutation moves to another process.
// The market program can uninstall a plugin while the pet is running, and the
// pet's subscription map would then hold an entry for an id that no longer
// exists, growing without bound across a long session. Pruning on the read path
// instead makes the map a projection of the install list, which is what it
// always should have been.
//
// Callers must NOT hold pluginHost.mu.
func (a *App) pruneSubscriptions(live map[string]bool) {
	a.pluginHost.mu.Lock()
	for id := range a.pluginHost.subs {
		if !live[id] {
			delete(a.pluginHost.subs, id)
		}
	}
	a.pluginHost.mu.Unlock()
}
