package plugins

// The cross-plugin tray: one file per published value, inside the PUBLISHER's private
// directory, with the read side gated by the caller's manifest (`shareRead`).
//
// # WHY THIS LIVES HERE NOW
//
// It was written in the pet's package main, and that was correct while the pet was the
// only program hosting plugin documents. It is not any more: the plugin market draws a
// plugin's own interface in ITS window (the 插件 page's workspace), and the four bundled
// plugins that do real work there - todo, pomodoro, chat, voice - all publish or read a
// shared key through it. The pet's registry served it and the market's did not, so the
// market's answer was "本程序不支持该方法" in the middle of a plugin's own page: the
// reported failure was 「插件 com.shixiansi.plugin-pomodoro 调用了本程序不支持的方法
// share.set」, and the fix at the time was to stop mounting such documents at all.
//
// Refusing to mount a plugin's page because the HOST lacks a capability the plugin was
// granted is the wrong direction: the tray is a directory the market already owns (it is
// the same `plugin-data/<id>/` tree it writes settings into), and "which plugin may read
// whose key" is a rule about manifests, not about pets. So the rule moved here, beside
// manifest.go's DeclaredShareRead, and both capability tables call the same three
// functions.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// shareNamePattern is the only shape a shared key may take.
//
// A flat name, not a path: the value is one blob that another plugin reads whole, and a
// separator would let a publisher name a file outside the `share/` folder it owns.
var shareNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidShareName reports whether name is usable as a shared key.
func ValidShareName(name string) bool { return shareNamePattern.MatchString(name) }

// MaxShareValue bounds one published value.
//
// Only text that travels BETWEEN plugins: an unbounded channel would be a way to move
// arbitrary blobs through the host.
const MaxShareValue = 64 << 10

// shareFileName is where one published value lives: in the `share` FOLDER of the
// PUBLISHER's private directory, so "may write" is a directory that plugin already owns.
//
// The folder rather than the directory root is 1.11.0's change: these files are the
// HOST's business, not the plugin's, and mixing them into the root made a plugin's own
// data ("stickers/", "history/") and its tray look like the same pile. A value written
// before that change is still read, so an upgrade mid-conversation loses nothing.
func shareFileName(name string) string { return filepath.Join("share", name) }

// legacyShareFileName is where a published value lived before the share/ folder.
func legacyShareFileName(name string) string { return "share-" + name }

// ShareSet publishes one value under the caller's own name.
//
// Writing your own values needs no declaration, and writing anyone else's is impossible:
// the directory is the caller's, so the "only your own prefix" rule is enforced by the
// filesystem rather than by a string check that could be got wrong.
func ShareSet(gate Gate, id string, payload []byte) (any, error) {
	var in struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	if !ValidShareName(in.Name) {
		return nil, fmt.Errorf("share 的 name 非法：%q", in.Name)
	}
	if len(in.Value) > MaxShareValue {
		return nil, fmt.Errorf("share 的值超过 %d 字节：%d", MaxShareValue, len(in.Value))
	}
	dir, err := gate.PrivateDir(id)
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
	return map[string]any{"ok": true, "key": SharedKey(id, in.Name)}, nil
}

// ShareGet reads one value another plugin published, if the caller declared it.
//
// Reading your own values needs no declaration (they are your files). Reading someone
// else's needs the exact pair in the caller's manifest, and the refusal names the pair
// and the field: a bare "403" in a plugin log helps nobody.
func ShareGet(gate Gate, id string, payload []byte) (any, error) {
	var in struct {
		Owner string `json:"owner"`
		Name  string `json:"name"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	owner := strings.TrimSpace(in.Owner)
	if owner == "" {
		owner = id
	}
	if !ValidShareName(in.Name) {
		return nil, fmt.Errorf("share 的 name 非法：%q", in.Name)
	}
	if !IsValidID(owner) {
		return nil, fmt.Errorf("share 的 owner 非法：%q", owner)
	}
	if owner != id {
		row, ok := gate.Get(id)
		if !ok || row.Manifest == nil {
			return nil, fmt.Errorf("插件 %s 未启用", id)
		}
		if !DeclaredShareRead(row.Manifest.ShareRead, owner, in.Name) {
			return nil, fmt.Errorf("插件 %s 没有声明要读 %s：manifest 的 shareRead 里没有这一项",
				id, SharedKey(owner, in.Name))
		}
	}
	dir, err := gate.PrivateDir(owner)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, shareFileName(in.Name))
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// A value published before share/ existed. Reading it where it is costs one stat
		// and keeps an upgrade invisible; moving it is best effort, because the only
		// thing that matters here is the bytes the caller asked for.
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
			return map[string]any{"value": "", "at": 0, "key": SharedKey(owner, in.Name)}, nil
		}
		return nil, err
	}
	if len(data) > MaxShareValue {
		return nil, fmt.Errorf("%s 的值超过 %d 字节，拒绝读取", SharedKey(owner, in.Name), MaxShareValue)
	}
	var at int64
	if info, statErr := os.Stat(path); statErr == nil {
		at = info.ModTime().UnixMilli()
	}
	return map[string]any{"value": string(data), "at": at, "key": SharedKey(owner, in.Name)}, nil
}

// ShareRemove deletes one of the caller's OWN published values.
//
// Only its own: a consumer clearing someone else's job would make a shared key whose
// content depends on which plugin ran last, which is the coupling this tray exists to
// avoid.
func ShareRemove(gate Gate, id string, payload []byte) (any, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	if !ValidShareName(in.Name) {
		return nil, fmt.Errorf("share 的 name 非法：%q", in.Name)
	}
	dir, err := gate.PrivateDir(id)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(filepath.Join(dir, shareFileName(in.Name))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// A leftover from before share/: removing "my value" has to remove it too, or a clear
	// would appear to succeed while a stale copy stays readable.
	if err := os.Remove(filepath.Join(dir, legacyShareFileName(in.Name))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// RegisterShare adds the three tray methods to a program's capability table.
//
// The gate is a PARAMETER rather than something recovered from the registry's host: the
// host object is the program's own state (the market's *Market), and the tray needs the
// installer index and the private directories (the market's gateAdapter). Passing it here
// keeps the three closures honest about what they touch, and leaves no type assertion that
// could panic in a method the user just called.
//
// The pet builds its table by hand (it needs the `also` field and the tool catalog), so it
// calls the three functions directly instead.
// onSet is called AFTER a publish lands, with the publisher's id and the key's name. It is how a
// program tells the interested plugins that the value changed (see `share.changed` in
// docs/plugin-dev.md §7.1); nil means "this program mounts no plugin documents", which is the only
// honest reason to pass nothing.
//
// The callback exists because the two programs emit through different runtimes (the pet's
// emitPluginEvent, the market's runtime.EventsEmit) while the RULE - who may be told - must stay
// one implementation. Duplicating the rule in the market program is the drift this package exists
// to prevent.
func RegisterShare(r *Registry, gate Gate, onSet func(pluginID, name string)) {
	r.Register("share.set", Method{
		Perm: PermPluginShare,
		Run: func(_ any, call Call, payload []byte) (any, error) {
			out, err := ShareSet(gate, call.PluginID, payload)
			if err != nil {
				return nil, err
			}
			if onSet != nil {
				var in struct {
					Name string `json:"name"`
				}
				// The payload was already validated by ShareSet; this parse only recovers the
				// name for the notification, so a failure here is "no notification", not an error
				// for the publisher.
				if json.Unmarshal(payload, &in) == nil && in.Name != "" {
					onSet(call.PluginID, in.Name)
				}
			}
			return out, nil
		},
	})
	r.Register("share.get", Method{
		Perm: PermPluginShare,
		Run:  func(_ any, call Call, payload []byte) (any, error) { return ShareGet(gate, call.PluginID, payload) },
	})
	r.Register("share.remove", Method{
		Perm: PermPluginShare,
		Run:  func(_ any, call Call, payload []byte) (any, error) { return ShareRemove(gate, call.PluginID, payload) },
	})
}
