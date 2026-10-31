package main

// Hosting a plugin's own interface, in the market program.
//
// The market draws the voice plugin's workshop in the plugin's settings area, so
// it needs the two things any plugin host needs: the plugin's FILES, served under
// the same URL space the pet uses, and the plugin's REQUESTS, answered through the
// same gate the pet uses.
//
// Both are the shared implementations in internal/plugins. What is deliberately
// NOT shared is the capability SET, and the boundary is a real product fact rather
// than an oversight:
//
//	served here    audio.*, storage.*, config.*, net.fetch, plugin.share
//	                - they operate on the plugin's own directory, its own settings,
//	                  the network, and the cross-plugin tray (which is a folder inside
//	                  plugin-data either program can reach), all of which this program
//	                  has.
//
//	not served     todos.*, timer.*, bubble.say, surface.*, llm.chat, events.subscribe
//	                - each needs something only the PET has: the todo file, the
//	                  pomodoro state, a window to draw beside, the pet's surface
//	                  geometry, the pet's own LLM budget, and the timer that emits
//	                  the events.
//
// # WHY THE FLOATING-SURFACE PLUGINS ARE MOUNTED ANYWAY
//
// `plugin.share` used to be on the second list, and the consequence was that mounting a
// plugin's own document here broke: the todo, pomodoro and chat documents publish or read
// a shared key from inside their page, so the user saw 「插件 … 调用了本程序不支持的方法
// share.set」 in the middle of the plugin's own interface, and the fix at the time was to
// refuse to mount any plugin that declared a surface (`mountable()` in the control
// center). That refused the three plugins whose pages the user actually wanted, and it
// refused them for a reason that was not true: the tray is a folder in plugin-data, not a
// pet.
//
// So the tray moved to internal/plugins/share.go and is served by BOTH programs. What a
// document still cannot do here is the genuinely pet-shaped half - draw beside the pet,
// start its timer, spend the pet's model budget - and a document that is designed for a
// window (rather than for the pet's column) is expected to notice: `init.surface.slot` is
// "workshop" here, and the chat plugin uses that to say plainly that this window shows
// the record and the sending happens beside the pet (see its app.js).
//
// A plugin that calls one of the remaining methods from here gets "本程序不支持该方法".
// That is the honest answer, and it is why the registry is per program instead of one table
// shared by both: a shared table would advertise capabilities that could only fail
// later, deeper in, with a nil dereference instead of an explanation.
//
// `events.subscribe` is the one that is worth stating explicitly, because the voice
// plugin DOES subscribe - it just does so from the frame the PET mounts. A
// subscription from the market would be a promise this program cannot keep: it has
// no pomodoro and no reminder scheduler, so it would record a subscription and then
// never emit anything. Refusing it says so.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/logging"
	"PetPC/internal/plugins"
)

// audioNamePattern is the only shape an audio slot name may take.
//
// The same rule the pet applies, and for the same reason: the name becomes a file
// name inside the plugin's own directory, so a separator or ".." would be a way out
// of it.
var audioNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// maxAudioBytes bounds one saved clip, matching the pet's limit.
const maxAudioBytes = 16 << 20

// gateAdapter lets the shared dispatcher and asset route use this program's
// manager.
//
// The manager already has Get and PrivateDir, so the adapter is nearly empty - it
// exists because Go interfaces are satisfied structurally but the two programs hold
// their managers behind different concrete types, and because an adapter is where a
// future difference (say, a per-program view of what is installed) would go.
type gateAdapter struct{ m *plugins.Manager }

func (g gateAdapter) Get(id string) (plugins.Installed, bool) { return g.m.Get(id) }

func (g gateAdapter) PrivateDir(id string) (string, error) { return g.m.PrivateDir(id) }

// audioSink plays audio, as a seam so a test can observe what was asked for.
//
// It is the same seam the pet uses. Without it, "the workshop auditioned THIS take"
// is not an assertable fact, only a side effect.
type audioSink interface {
	PlayWAVFile(path string) error
	StopAudio()
}

type desktopAudio struct{}

func (desktopAudio) PlayWAVFile(path string) error { return desktop.PlayWAVFile(path) }
func (desktopAudio) StopAudio()                    { desktop.StopAudio() }

// marketRegistry builds the capability table for THIS program.
func (m *Market) marketRegistry() *plugins.Registry {
	if m.registry != nil {
		return m.registry
	}
	r := plugins.NewRegistry(m, gateAdapter{m.manager})

	// Audio: the workshop's audition and its takes.
	r.Register("audio.save", plugins.Method{Perm: plugins.PermAudio, Run: marketAudioSave})
	r.Register("audio.list", plugins.Method{Perm: plugins.PermAudio, Run: marketAudioList})
	r.Register("audio.remove", plugins.Method{Perm: plugins.PermAudio, Run: marketAudioRemove})
	r.Register("audio.play", plugins.Method{Perm: plugins.PermAudio, Run: marketAudioPlay})
	r.Register("audio.stop", plugins.Method{Perm: plugins.PermAudio, Run: marketAudioStop})
	r.Register("audio.import", plugins.Method{Perm: plugins.PermAudio, Run: marketAudioImport})

	// The plugin's own small values and files, and its declared settings.
	//
	// storage.* comes from internal/plugins rather than being written here, because the
	// QQ plugin keeps its sticker library in this program (the workshop tab) while the
	// bot that fills it runs in the pet. One implementation is what makes a storage key
	// legal in both: keys may name folders now, and a second copy of that rule is a
	// second chance to get it wrong.
	plugins.RegisterPrivateStore(r)
	// The cross-plugin tray, served from the shared rule in internal/plugins. The 插件
	// page mounts the bundled plugins' own documents, and three of them publish or read a
	// key from inside their page - so without this the page shows a plugin's interface
	// with a host error in the middle of it (see this file's header).
	plugins.RegisterShare(r, gateAdapter{m.manager})
	r.Register("config.get", plugins.Method{Perm: plugins.PermPluginConfig, Run: marketConfigGet})
	r.Register("config.set", plugins.Method{Perm: plugins.PermPluginConfig, Run: marketConfigSet})
	// The channel plugin's editor lives on this program's 插件 page, so the table's two
	// methods have to be served here as well - they are the plugin's OWN settings file plus
	// DPAPI-encrypted keys, with no model call and no pet window involved. Same shared
	// implementation the pet uses (internal/plugins/llmchannels.go).
	plugins.RegisterChannels(r, gateAdapter{m.manager}, m.keeper)

	// Talking to a synthesis engine the user is running.
	r.Register("net.fetch", plugins.Method{Perm: plugins.PermNetFetch, Run: marketNetFetch})

	m.registry = r
	return r
}

// PluginHostInfo answers "what does THIS program serve", for `init.host`.
//
// The list is this program's own registry - the same map `PluginRequest` dispatches through -
// so it cannot claim a capability the gate would refuse. That matters more here than in the pet:
// the difference between the two hosts is the whole reason this field exists, and a plugin that
// had to infer it from `surface.slot` kept getting it wrong in the one direction that costs a
// WARN line per poll (see plugins.HostInfo).
func (m *Market) PluginHostInfo() plugins.HostInfo {
	return plugins.HostInfoOf("workshop", m.marketRegistry().Methods())
}

// PluginRequest is the ONE way a plugin reaches this program.
//
// The signature matches the pet's binding exactly, and deliberately: the plugin's
// frame posts the same protocol message to whichever program mounted it, so the
// binding it calls has to look the same from the page's side. The GATE is the
// shared one; only the table differs.
//
// Every request leaves one line, for the reason the pet's own gate does: a plugin
// whose workshop stays empty produces no evidence anywhere else, and "它在工坊里
// 什么也做不了" is unanswerable without knowing whether the request arrived, was
// refused, or failed inside the method.
func (m *Market) PluginRequest(pluginID, method, payload string) (string, error) {
	start := time.Now()
	out, err := m.marketRegistry().Dispatch(pluginID, method, payload)
	elapsed := time.Since(start)

	// Same level rule as the pet's gate (see internal/plugins.DataMethod): reading the
	// plugin's own file is plumbing (DEBUG), an ACTION is the story (INFO), a refusal is WARN.
	lv := logging.LevelInfo
	if plugins.DataMethod(method) {
		lv = logging.LevelDebug
	}
	if err != nil {
		// WARN rather than ERROR: "本程序不支持该方法" is the normal answer for a
		// capability only the pet serves, and the line says so in the error text.
		lv = logging.LevelWarn
	}
	if allow, swallowed := m.pluginCallLog().Allow(pluginID + "|" + method + "|" + lv.String()); !allow {
		return out, err
	} else if swallowed > 0 {
		if err != nil {
			m.log.Pluginf(lv, "plugin", pluginID, "%s 没有执行（payload %d B，in %dms；同类另有 %d 次已合并）：%v",
				method, len(payload), elapsed.Milliseconds(), swallowed, err)
			return "", err
		}
		m.log.Pluginf(lv, "plugin", pluginID, "%s ok（payload %d B → %d B，in %dms；同类另有 %d 次已合并）",
			method, len(payload), len(out), elapsed.Milliseconds(), swallowed)
		return out, nil
	}
	if err != nil {
		m.log.Pluginf(lv, "plugin", pluginID, "%s 没有执行（payload %d B，in %dms）：%v",
			method, len(payload), elapsed.Milliseconds(), err)
		return "", err
	}
	m.log.Pluginf(lv, "plugin", pluginID, "%s ok（payload %d B → %d B，in %dms）",
		method, len(payload), len(out), elapsed.Milliseconds())
	return out, nil
}

// pluginCallLog returns the throttle for the per-call lines, creating it on first use.
func (m *Market) pluginCallLog() *logging.Throttle {
	m.callsMu.Lock()
	defer m.callsMu.Unlock()
	if m.callsLog == nil {
		m.callsLog = logging.NewThrottle(logging.DefaultThrottleWindow, nil)
	}
	return m.callsLog
}

// ServePluginAsset serves a plugin's own files, and the bridge script.
//
// Wired into this program's AssetServer. It is the same handler the pet uses, so
// the traversal defence and the bridge protocol have exactly one implementation.
func (m *Market) ServePluginAsset(w http.ResponseWriter, r *http.Request) {
	plugins.ServePluginAsset(gateAdapter{m.manager}, w, r)
}

// ServeHTTP is this program's AssetServer fallback.
//
// Wails calls it for any GET the embedded front-end bundle cannot satisfy, which
// is exactly the plugin asset space: those files live on disk in the plugin's
// installed directory, not in this program's bundle.
//
// Everything else 404s rather than falling through to a default page. A mistyped
// URL should say so, and silently serving the market's own index for it would make
// a broken plugin entry look like a plugin that rendered nothing.
func (m *Market) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if plugins.ServePluginAsset(gateAdapter{m.manager}, w, r) {
		return
	}
	http.NotFound(w, r)
}

// audioPlayer returns the sink used for auditions.
func (m *Market) audioPlayer() audioSink {
	m.audioMu.Lock()
	defer m.audioMu.Unlock()
	if m.audioOut == nil {
		m.audioOut = desktopAudio{}
	}
	return m.audioOut
}

// pluginAudioDir returns the plugin's audio directory, creating it on demand.
func (m *Market) pluginAudioDir(id string) (string, error) {
	dir, err := m.manager.PrivateDir(id)
	if err != nil {
		return "", err
	}
	audioDir := filepath.Join(dir, "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		return "", err
	}
	return audioDir, nil
}

// writeAudioFile writes one clip atomically.
//
// A half-written WAV is worse than a missing one: it plays as noise, and the
// workshop would show it as a take that exists. The temporary-then-rename dance is
// the same one the pet's side does.
func (m *Market) writeAudioFile(id, name string, raw []byte) (int, error) {
	dir, err := m.pluginAudioDir(id)
	if err != nil {
		return 0, err
	}
	path := filepath.Join(dir, name+".wav")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	return len(raw), nil
}

func marketAudioSave(host any, call plugins.Call, payload []byte) (any, error) {
	m := host.(*Market)
	var in struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if err := plugins.DecodePayload(payload, &in); err != nil {
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
	n, err := m.writeAudioFile(call.PluginID, in.Name, raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": in.Name, "bytes": n}, nil
}

func marketAudioList(host any, call plugins.Call, _ []byte) (any, error) {
	m := host.(*Market)
	dir, err := m.pluginAudioDir(call.PluginID)
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

func marketAudioRemove(host any, call plugins.Call, payload []byte) (any, error) {
	m := host.(*Market)
	var in struct {
		Name string `json:"name"`
	}
	if err := plugins.DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	if !audioNamePattern.MatchString(in.Name) {
		return nil, fmt.Errorf("audio 名称非法：%q", in.Name)
	}
	dir, err := m.pluginAudioDir(call.PluginID)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(filepath.Join(dir, in.Name+".wav")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func marketAudioPlay(host any, call plugins.Call, payload []byte) (any, error) {
	m := host.(*Market)
	var in struct {
		Name string `json:"name"`
	}
	if err := plugins.DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	if !audioNamePattern.MatchString(in.Name) {
		return nil, fmt.Errorf("audio 名称非法：%q", in.Name)
	}
	dir, err := m.pluginAudioDir(call.PluginID)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, in.Name+".wav")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("没有这段音频：%s", in.Name)
	}
	if err := m.audioPlayer().PlayWAVFile(path); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func marketAudioStop(host any, _ plugins.Call, _ []byte) (any, error) {
	host.(*Market).audioPlayer().StopAudio()
	return map[string]any{"ok": true}, nil
}

// marketAudioImport copies a declared package file into the plugin's audio
// directory, for the same reason the pet has it: the packaged built-in clips have
// to reach the player, which works on the private directory.
func marketAudioImport(host any, call plugins.Call, payload []byte) (any, error) {
	m := host.(*Market)
	var in struct {
		Name string `json:"name"`
		From string `json:"from"`
	}
	if err := plugins.DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	if !audioNamePattern.MatchString(in.Name) {
		return nil, fmt.Errorf("audio 名称非法：%q", in.Name)
	}
	if !plugins.ValidRelPath(in.From) {
		return nil, fmt.Errorf("audio.import 的 from 不是合法的相对路径：%q", in.From)
	}
	declared := false
	for _, a := range call.Manifest.Assets {
		if a == in.From {
			declared = true
			break
		}
	}
	if !declared {
		return nil, fmt.Errorf("manifest 的 assets 未声明 %q", in.From)
	}
	pkgDir, err := m.manager.PluginDir(call.PluginID)
	if err != nil {
		return nil, err
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
	n, err := m.writeAudioFile(call.PluginID, in.Name, raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": in.Name, "bytes": n}, nil
}

func marketConfigGet(host any, call plugins.Call, _ []byte) (any, error) {
	m := host.(*Market)
	st := plugins.NewConfigStore(call.PrivateDir, call.Manifest, m.keeper)
	return st.Read()
}

func marketConfigSet(host any, call plugins.Call, payload []byte) (any, error) {
	m := host.(*Market)
	var in struct {
		Values map[string]string `json:"values"`
	}
	if err := plugins.DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	if len(in.Values) == 0 {
		return map[string]any{"ok": true}, nil
	}
	st := plugins.NewConfigStore(call.PrivateDir, call.Manifest, m.keeper)
	if err := st.Write(in.Values); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}
