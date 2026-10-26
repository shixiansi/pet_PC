package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"PetPC/internal/llm"
	"PetPC/internal/plugins"
	"PetPC/internal/store"
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
// Both are additive later: add the method, then the tool.

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
			"todo_list": {
				perm:        plugins.PermTodosRead,
				description: "列出用户当前的待办（id、标题、截止时间、是否已完成）。要完成某条待办或谈论具体某条时，先用它拿到 id。",
				parameters:  `{"type":"object","properties":{},"additionalProperties":false}`,
				impl:        pluginTodosList,
			},
			"todo_add": {
				perm:        plugins.PermTodosWrite,
				description: "给用户新增一条待办。只在用户确实要记住一件事时调用；不要替他发明他没提过要做的事。",
				parameters: `{"type":"object","properties":{` +
					`"title":{"type":"string","description":"待办标题，一句话"},` +
					`"notes":{"type":"string","description":"补充说明，可选"},` +
					`"due":{"type":"string","description":"截止时间，可选，用界面上那种说法，例如「今天 18:00」「明天」「周五」"}` +
					`},"required":["title"]}`,
				impl: pluginTodosAdd,
			},
			"todo_complete": {
				perm:        plugins.PermTodosWrite,
				description: "把某条待办标记为已完成，需要它的 id（先调 todo_list）。已经是完成状态时什么也不会发生。",
				parameters: `{"type":"object","properties":{` +
					`"id":{"type":"string","description":"待办的 id，来自 todo_list"}` +
					`},"required":["id"]}`,
				runTool: completeTodoTool,
			},
			"pomodoro_start": {
				perm:        plugins.PermTimerControl,
				description: "开始一个番茄钟专注阶段，可选关联一条待办。用户没有明确说要开始专注时不要调用。",
				parameters: `{"type":"object","properties":{` +
					`"todoId":{"type":"string","description":"要关联的待办 id，可选；为空表示不关联"}` +
					`},"additionalProperties":false}`,
				impl: pluginTimerStart,
			},
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

// completeTodoTool implements todo_complete.
//
// It cannot simply forward: todos.toggle FLIPS the flag, so calling it on an entry
// that is already done would UN-complete it - the exact opposite of what the tool is
// named. So it looks the entry up first and answers "已经完成了" instead of flipping.
func completeTodoTool(a *App, id string, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("todo_complete 的参数不是合法 JSON：%w", err)
	}
	if strings.TrimSpace(in.ID) == "" {
		return "", errors.New("todo_complete 需要 id")
	}

	listed, err := pluginTodosList(a, id, nil)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(listed)
	if err != nil {
		return "", err
	}
	var todos []store.Todo
	if err := json.Unmarshal(encoded, &todos); err != nil {
		return "", fmt.Errorf("读取待办列表失败：%w", err)
	}
	for _, t := range todos {
		if t.ID != in.ID {
			continue
		}
		if t.Done {
			return `{"ok":true,"alreadyDone":true}`, nil
		}
		// Built with json.Marshal rather than string concatenation, so an id can
		// never break out of the JSON string it sits in.
		payload, mErr := json.Marshal(map[string]string{"id": in.ID})
		if mErr != nil {
			return "", mErr
		}
		out, tErr := pluginTodosToggle(a, id, payload)
		if tErr != nil {
			return "", tErr
		}
		done, mErr := json.Marshal(out)
		if mErr != nil {
			return "", mErr
		}
		return string(done), nil
	}
	return "", fmt.Errorf("没有 id 为 %s 的待办（先调 todo_list 确认）", in.ID)
}

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
// An unknown name is an error - the same way events.subscribe refuses an unknown
// event - because a plugin that asked for something this host cannot do has a bug,
// and quietly offering less would hide it behind "the model never used that tool".
//
// A name the user did NOT grant is skipped rather than refused: the plugin cannot
// know what the user allowed, and the model must not be shown a tool it cannot use.
// The skip is logged so the confusion is diagnosable from debug.log.
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
			return nil, fmt.Errorf("未知工具 %s", name)
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
