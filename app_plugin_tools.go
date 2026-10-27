package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"PetPC/internal/llm"
	"PetPC/internal/plugins"
)

// The tools a plugin may let the MODEL call.
//
// A tool is a NAME from this catalog, never a schema the plugin supplies. That is the
// whole safety story, so it is worth stating plainly:
//
//   - the schema and the description live HERE, next to the permission they need, so
//     a tool cannot be described to the model as something other than what it does;
//   - a tool the user did not grant is never offered, so the model is not shown a
//     hand it cannot use and then made to try and fail;
//   - every call is checked again, in runPluginTool, before anything runs.
//
// NOT here yet, and why:
//   - reminder_set: the host derives reminders from todos and exposes no plugin
//     method to create one, so there is no permission to gate a tool on.
//   - pet_ask (a bubble with options): bubble.say is the only plugin-facing bubble
//     method; a question bubble belongs to the host's own plan/summary flow.
//
// GONE, and why: `pomodoro_start` let the model start a focus session, gated on
// timer.control. Both retired together with the host's plugin-facing timer - the
// pomodoro plugin counts its own phases now, and the host can no longer start one on
// a plugin's behalf. Removing the permission but keeping the tool would have left a
// tool whose gate does not exist; removing the tool but keeping the permission would
// have left a switch that unlocks nothing. They are one deletion.

// maxToolRounds bounds one llm.chat request.
//
// Four is enough for the shape this is for - look something up, act on it, tell the
// user - and it is a CEILING rather than a suggestion: an unbounded loop hands the
// bill for one question to the model.
const maxToolRounds = 4

// pluginTool is one entry of the catalog.
type pluginTool struct {
	// perm is what the caller must hold for this tool to be OFFERED, and it is
	// checked again before the call runs.
	perm string
	// description is what the model decides with: what it does, and when not to.
	description string
	// parameters is a JSON Schema object.
	parameters string
	// impl is the existing method IMPLEMENTATION this tool runs.
	//
	// It calls the implementation function, NOT PluginRequest, and that is forced
	// rather than chosen: PluginRequest reads pluginMethods, pluginMethods holds
	// llm.chat, and llm.chat reaches this table - a chain Go rejects as an
	// initialization cycle, because the edges run between function VALUES and not
	// only between calls. The gate is not lost by going direct: runPluginTool
	// performs exactly the two checks the dispatcher would (the plugin is enabled,
	// and the user granted t.perm) before impl is touched, using the same
	// plugins.HasGranted.
	impl func(a *App, id string, payload []byte) (any, error)
	// runTool implements a tool that is more than a forward, and takes precedence
	// over impl when set.
	runTool func(a *App, id string, args json.RawMessage) (string, error)
}

// pluginToolCatalog is the catalog, keyed by the name the model sees.
//
// Built on first use rather than declared as a package-level var: the entries hold
// function values that reach back into this package, and a var initializer that does
// so is rejected as an initialization cycle (see pluginTool.impl). A lazily built map
// has no initializer expression, so that edge does not exist.
var (
	toolCatalogOnce sync.Once
	toolCatalog     map[string]pluginTool
)

func pluginToolCatalog() map[string]pluginTool {
	toolCatalogOnce.Do(func() {
		toolCatalog = map[string]pluginTool{
			// The three todo tools (todo_list / todo_add / todo_complete) are gone.
			// They were thin forwards onto the host's todo store, and that store is the
			// todo plugin's own now: there is no host method left to forward to, and
			// re-pointing them at the other plugin's shared snapshot would have given
			// the model a READ of the list with no way to write it - the worse half of
			// both features. "The model chats; the list is edited where it lives" is
			// the cost of stage 1 that was accepted up front (see the change's §4.5).
			"pet_say": {
				perm:        plugins.PermBubble,
				description: "让桌宠弹出一句话气泡。用于用户该被主动告知的短句；长回复不要塞进来，那是对话面板的事。",
				parameters: `{"type":"object","properties":{` +
					`"text":{"type":"string","description":"要说的一句话"}` +
					`},"required":["text"]}`,
				impl: pluginBubbleSay,
			},
			"memory_read": {
				perm:        plugins.PermStorage,
				description: "读你自己记下的长期记忆（关于这位用户的稳定事实）。对话开始、或需要回忆以前的事时，先读它。",
				parameters:  `{"type":"object","properties":{},"additionalProperties":false}`,
				impl:        pluginMemoryRead,
			},
			"memory_append": {
				perm:        plugins.PermStorage,
				description: "往长期记忆里追加一条关于这位用户的稳定事实（一句话）。只记以后还用得上的事；一次性的对话内容不要记。",
				parameters: `{"type":"object","properties":{` +
					`"text":{"type":"string","description":"一条事实，一句话"}` +
					`},"required":["text"]}`,
				impl: pluginMemoryAppend,
			},
		}
	})
	return toolCatalog
}

// completeTodoTool used to live here: todo_complete could not simply forward, because
// todos.toggle FLIPS the flag and calling it on an entry that was already done would
// un-complete it. The tool went with the rest of the todo tools (see the catalog above) -
// it is kept in this note only so the next person does not re-add a "complete" tool as a
// thin wrapper over a toggle.

// runPluginTool executes one tool call the model asked for, and tells the plugin
// that it happened.
//
// The two checks below are the dispatcher's, in its order: the plugin must be
// installed and enabled, and the user must have granted the permission. They are
// repeated here rather than inherited, because a grant can be revoked while a request
// is in flight (the user can untick a box in the market) and "it was in the list I
// offered" is not a licence to run it now.
//
// The llm.tool frame deliberately carries NO arguments. They can hold the user's todo
// text, and reading that requires todos.read - which merely receiving events does not
// imply. The frame carries the tool's NAME, whether it worked, and why not, which is
// exactly what a plugin needs to show "在加待办" and then "没加成".
func (a *App) runPluginTool(pluginID, requestID string, call llm.ToolCall) (string, error) {
	t, ok := pluginToolCatalog()[call.Function.Name]
	if !ok {
		return "", fmt.Errorf("未知工具 %s", call.Function.Name)
	}
	row, ok := a.pluginManager().Get(pluginID)
	if !ok || row.Manifest == nil {
		return "", fmt.Errorf("插件 %s 未启用", pluginID)
	}
	if !plugins.HasGranted(row.Granted, t.perm) {
		return "", fmt.Errorf("工具 %s 需要 %s，未授予", call.Function.Name, t.perm)
	}

	args := json.RawMessage(strings.TrimSpace(call.Function.Arguments))
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}

	out, err := a.invokePluginTool(pluginID, t, args)

	frame := map[string]any{"requestId": requestID, "name": call.Function.Name, "ok": err == nil}
	if err != nil {
		frame["error"] = err.Error()
	}
	a.emitPluginEvent(pluginID, "llm.tool", frame)
	return out, err
}

// invokePluginTool runs the tool body and renders its value as the result text.
//
// Arguments are passed to the implementation UNCHANGED. That implementation already
// validates them and already refuses what it does not accept, so a bad call comes
// back as a tool result the model can read and correct - which is what makes one
// wrong guess cheap. Re-validating here would be a second set of rules for one
// operation.
func (a *App) invokePluginTool(pluginID string, t pluginTool, args json.RawMessage) (string, error) {
	if t.runTool != nil {
		return t.runTool(a, pluginID, args)
	}
	value, err := t.impl(a, pluginID, args)
	if err != nil {
		return "", err
	}
	// json.Marshal, never string concatenation: the result is what the model reads,
	// and a hand-built string is one quoting bug away from undecodable output.
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// pluginToolSpecs turns requested tool NAMES into the list the model sees.
//
// A name this host does not know is SKIPPED, and that is deliberately different from
// how events.subscribe treats an unknown event. The difference is the direction of the
// version skew: a plugin ships on its own schedule, so a NEWER plugin talking to an
// older host is an ordinary state. It happened the day memory_read was added - the
// whole conversation failed with "未知工具 memory_read", which the user reasonably read
// as "granting the permission did nothing". An unknown EVENT is not the same thing: the
// event set is closed and stable, so asking for one means the plugin was written
// against a host that never existed.
//
// A name the user did NOT grant is skipped for the other reason: the plugin cannot know
// what the user allowed, and the model must not be shown a tool it cannot use. Neither
// skip is silent - both are logged, because "the model never used that tool" and "the
// tool was never offered" look identical from the outside, and only one of them is a
// bug.
func pluginToolSpecs(a *App, granted, want []string) ([]llm.ToolSpec, error) {
	specs := make([]llm.ToolSpec, 0, len(want))
	catalog := pluginToolCatalog()
	for _, name := range want {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		t, ok := catalog[name]
		if !ok {
			a.DebugLog(fmt.Sprintf("plugin llm: 工具 %s 是宿主不认识的工具名，已跳过（插件比宿主新？）", name))
			continue
		}
		if !plugins.HasGranted(granted, t.perm) {
			a.DebugLog(fmt.Sprintf("plugin llm: 工具 %s 需要 %s，未授予，不提供给模型", name, t.perm))
			continue
		}
		specs = append(specs, llm.ToolSpec{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        name,
				Description: t.description,
				Parameters:  json.RawMessage(t.parameters),
			},
		})
	}
	return specs, nil
}
