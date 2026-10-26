// Package plugins' dispatcher: the one gate every plugin capability passes
// through, shared by every program that hosts a plugin.
//
// WHY THIS IS SHARED RATHER THAN COPIED
//
// Two programs host plugin interfaces: the pet (the 对话 panel slot, the floating
// surface, and the headless background frame) and the plugin market (the workshop
// it draws in a plugin's settings area). Both mount the SAME kind of sandboxed
// iframe and both receive the same `request` messages, so both need to answer the
// question "may this plugin call this method, and with what?".
//
// That question is a security decision, and two implementations of a security
// decision is two places for it to drift. The order of the checks below is the
// contract:
//
//  1. the plugin must be installed and enabled;
//  2. the method must exist IN THIS PROGRAM;
//  3. the user must have granted the permission the method needs;
//  4. only then are the arguments touched.
//
// A refusal at 1-3 changes NOTHING: no todo written, no bubble shown, no request
// sent, no index saved, no private value stored. That is what makes "you declined
// this permission" mean something rather than being a message printed after the
// work was done.
//
// WHAT IS *NOT* SHARED
//
// The capability SET is per program, and that is deliberate rather than a gap.
// `todos.list` reads the pet's todo file; the market has no todo list. `bubble.say`
// shows a bubble next to the pet; the market has no pet. A plugin that calls one
// of those from the market gets "本程序不支持该方法" - an honest answer that names
// the real situation, instead of a shared table that pretended every capability
// worked everywhere and failed deeper down with a nil dereference.
//
// So this package owns the MECHANISM (registry, gating, encoding, the private-file
// operations every host can serve) and each program owns its own METHOD SET.
package plugins

import (
	"encoding/json"
	"fmt"
)

// Gate is what the dispatcher needs to know about the installed plugins.
//
// It is an interface rather than a *Manager parameter so the dispatcher can be
// exercised with a fixture, and so a program that keeps its rows somewhere else
// can still use the same gate. Both current hosts pass their Manager.
type Gate interface {
	// Get returns the installed row for id, and whether it exists.
	Get(id string) (Installed, bool)
	// PrivateDir returns the plugin's own directory, creating it if needed.
	PrivateDir(id string) (string, error)
}

// Method is one entry of a program's capability table.
type Method struct {
	// Perm is the permission the caller must hold. It must be a member of
	// AllowedPermissions, and every member of that list must be used by at least
	// one registered method in at least one program - the completeness test lives
	// with the pet, which registers the whole vocabulary.
	Perm string
	// Run performs the call. payload is the raw JSON the plugin sent; a method
	// that takes no arguments ignores it. host is whatever the registering
	// program passed to NewRegistry, so a method can reach the program's own
	// state without this package knowing what that state is.
	Run func(host any, call Call, payload []byte) (any, error)
}

// Call is the context of one accepted request.
//
// It carries only what a capability needs and nothing about the host's internals:
// the id of the CALLER (never taken from the payload - that is how one plugin
// would otherwise read another's data), its manifest, its private directory and
// its own configured values.
type Call struct {
	PluginID string
	Manifest Manifest
	// PrivateDir is the caller's own directory. Capabilities that touch files
	// build paths UNDER it and never take a path from the payload.
	PrivateDir string
}

// Registry is one program's capability table.
type Registry struct {
	host    any
	gate    Gate
	methods map[string]Method
}

// NewRegistry builds an empty registry bound to a host object and a plugin gate.
//
// host is opaque here and handed back to each Method.Run, which is what lets the
// pet's `bubble.say` reach the pet while this package stays ignorant of windows,
// timers and todo files.
func NewRegistry(host any, gate Gate) *Registry {
	return &Registry{host: host, gate: gate, methods: map[string]Method{}}
}

// Register adds one method.
//
// It panics on a duplicate name or an unknown permission, and both are deliberate:
// a table is built once at package scope, so a mistake here is a programming error
// that would otherwise surface as a plugin silently unable to call something. A
// duplicate means two implementations of one method and no way to say which wins.
func (r *Registry) Register(name string, m Method) {
	if name == "" {
		panic("plugins: a method needs a name")
	}
	if m.Run == nil {
		panic("plugins: method " + name + " has no implementation")
	}
	if !IsAllowedPermission(m.Perm) {
		panic("plugins: method " + name + " names the unknown permission " + m.Perm)
	}
	if _, dup := r.methods[name]; dup {
		panic("plugins: method " + name + " registered twice")
	}
	r.methods[name] = m
}

// Methods exposes the registered table for the completeness test.
func (r *Registry) Methods() map[string]Method {
	out := make(map[string]Method, len(r.methods))
	for k, v := range r.methods {
		out[k] = v
	}
	return out
}

// Has reports whether this program implements method.
//
// It exists so a host can tell a plugin which methods are available here, rather
// than letting it discover the difference by collecting refusals.
func (r *Registry) Has(method string) bool {
	_, ok := r.methods[method]
	return ok
}

// Dispatch runs one plugin request. This is THE gate.
//
// The returned string is the JSON encoding of the method's value, which is what
// the bridge hands back to the plugin as a `response`.
func (r *Registry) Dispatch(pluginID, method, payload string) (string, error) {
	row, ok := r.gate.Get(pluginID)
	if !ok || !row.Enabled || row.Manifest == nil {
		return "", fmt.Errorf("插件 %s 未启用", pluginID)
	}
	spec, ok := r.methods[method]
	// Names the PROGRAM, not just the method: from the plugin's side "todos.list
	// failed" and "this host has no todos" are very different messages, and only
	// the second one tells the author the plugin is running somewhere it was not
	// designed for.
	if !ok {
		return "", fmt.Errorf("插件 %s 调用了本程序不支持的方法 %s", pluginID, method)
	}
	if !hasGranted(row.Granted, spec.Perm) {
		return "", fmt.Errorf("插件 %s 未获权限 %s", pluginID, spec.Perm)
	}
	dir, err := r.gate.PrivateDir(pluginID)
	if err != nil {
		return "", err
	}
	out, err := spec.Run(r.host, Call{
		PluginID:   pluginID,
		Manifest:   *row.Manifest,
		PrivateDir: dir,
	}, []byte(payload))
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// hasGranted reports whether perm is in granted.
func hasGranted(granted []string, perm string) bool {
	for _, g := range granted {
		if g == perm {
			return true
		}
	}
	return false
}

// DecodePayload unmarshals a plugin payload into v.
//
// It is exported because every capability implementation needs it, and because
// the error wording is part of the contract: a plugin sends JSON it built itself,
// so a decode failure is the plugin's bug and should say so.
func DecodePayload(payload []byte, v any) error {
	if len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, v); err != nil {
		return fmt.Errorf("请求参数不是合法 JSON：%w", err)
	}
	return nil
}
