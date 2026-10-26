package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The tool loop's tests. They drive the REAL request path - llm.chat through the
// dispatcher, a scripted SSE endpoint, the host's tool catalog - because the parts
// that can go wrong here are the round trip and the ordering, not the parsing
// (which internal/llm tests on its own).

// toolStub answers each request with the next script, so a two-round conversation can
// be expressed as two scripts. A request past the end of the list repeats the last
// script, which is how the round-limit test keeps the model asking forever.
type toolStub struct {
	srv *httptest.Server

	mu    sync.Mutex
	asked []map[string]any
}

func newToolStub(t *testing.T, scripts ...[]string) *toolStub {
	t.Helper()
	st := &toolStub{}
	st.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		st.mu.Lock()
		st.asked = append(st.asked, req)
		round := len(st.asked) - 1
		st.mu.Unlock()

		if round >= len(scripts) {
			round = len(scripts) - 1
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		write := func(s string) {
			_, _ = w.Write([]byte(s))
			if flusher != nil {
				flusher.Flush()
			}
		}
		for _, payload := range scripts[round] {
			write("data: " + payload + "\n\n")
		}
		write("data: [DONE]\n\n")
	}))
	t.Cleanup(st.srv.Close)
	return st
}

func (s *toolStub) URL() string { return s.srv.URL }

// installToolPlugin installs the chat fixture with exactly these permissions.
//
// chatTestManifest is the base rather than a bare map because the config items have to
// be DECLARED for setPluginConfig to write them - which is exactly what a real plugin
// does, and the declaration is part of what the request path reads.
func installToolPlugin(t *testing.T, a *App, perms ...string) string {
	t.Helper()
	man := chatTestManifest()
	man["permissions"] = perms
	return installSampleWith(t, a, man)
}

// Hits is how many requests the endpoint has received.
func (s *toolStub) Hits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.asked)
}

// Request returns the n-th request the endpoint received.
func (s *toolStub) Request(n int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 0 || n >= len(s.asked) {
		return nil
	}
	return s.asked[n]
}

// toolCallScript is one assistant round that asks for one tool.
func toolCallScript(id, tool, args string) []string {
	call := map[string]any{
		"choices": []map[string]any{{"delta": map[string]any{
			"tool_calls": []map[string]any{{
				"index": 0, "id": id, "type": "function",
				"function": map[string]any{"name": tool, "arguments": args},
			}},
		}}},
	}
	end := map[string]any{"choices": []map[string]any{{"delta": map[string]any{}, "finish_reason": "tool_calls"}}}
	a, _ := json.Marshal(call)
	b, _ := json.Marshal(end)
	return []string{string(a), string(b)}
}

// textScript is one ordinary reply.
func textScript(text string) []string {
	body, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{"delta": map[string]any{"content": text}}},
	})
	end := map[string]any{"choices": []map[string]any{{"delta": map[string]any{}, "finish_reason": "stop"}}}
	b, _ := json.Marshal(end)
	return []string{string(body), string(b)}
}

// TestToolLoopRunsTheToolAndReturnsTheFinalText is the whole feature in one test:
// the model asks for todo_add, the host runs it for real (the todo exists afterwards),
// the result goes back to the model, and the user gets the second round's text.
func TestToolLoopRunsTheToolAndReturnsTheFinalText(t *testing.T) {
	stub := newToolStub(t,
		toolCallScript("call_1", "todo_add", `{"title":"写周报"}`),
		textScript("加好了"),
	)
	a := pluginApp(t)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config", "todos.read", "todos.write")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	out, err := a.PluginRequest(id, "llm.chat", `{"text":"帮我加一条","tools":["todo_add"]}`)
	if err != nil {
		t.Fatalf("llm.chat: %v", err)
	}

	// Two rounds reached the endpoint, and the first one carried the schema.
	if stub.Hits() != 2 {
		t.Fatalf("the endpoint saw %d requests, want 2 (tool round + answer)", stub.Hits())
	}
	first := stub.Request(0)
	tools, ok := first["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("the first request carries no tools: %+v", first["tools"])
	}
	if name := tools[0].(map[string]any)["function"].(map[string]any)["name"]; name != "todo_add" {
		t.Errorf("the offered tool is %v, want todo_add", name)
	}

	// The second round replays the assistant turn and the tool result.
	second := stub.Request(1)
	msgs, _ := second["messages"].([]any)
	var sawAssistantCall, sawToolResult bool
	for _, item := range msgs {
		m := item.(map[string]any)
		if _, ok := m["tool_calls"]; ok {
			sawAssistantCall = true
		}
		if m["role"] == "tool" {
			sawToolResult = true
			if m["tool_call_id"] != "call_1" {
				t.Errorf("the tool result names %v, want call_1", m["tool_call_id"])
			}
		}
	}
	if !sawAssistantCall {
		t.Error("the second round does not replay the assistant's tool_calls")
	}
	if !sawToolResult {
		t.Error("the second round does not carry the tool result")
	}

	// The tool really ran.
	found := false
	for _, todo := range a.store.Todos() {
		if todo.Title == "写周报" {
			found = true
		}
	}
	if !found {
		t.Errorf("the todo was not created: %+v", a.store.Todos())
	}

	var reply struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("decode the reply: %v", err)
	}
	if reply.Text != "加好了" {
		t.Errorf("text = %q, want 加好了", reply.Text)
	}
}

// TestToolNotGrantedIsNeverOffered pins the design's central promise: the model is not
// shown a hand it cannot use.
//
// A plugin that asks for a tool without holding the permission is not an error - it
// cannot know what the user allowed - but the request must go out WITHOUT tools, which
// also means it stays the single-round request this method has always made.
func TestToolNotGrantedIsNeverOffered(t *testing.T) {
	stub := newToolStub(t, textScript("好"))
	a := pluginApp(t)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config") // no todos.write
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"帮我加一条","tools":["todo_add"]}`); err != nil {
		t.Fatalf("llm.chat: %v", err)
	}
	if stub.Hits() != 1 {
		t.Errorf("the endpoint saw %d requests, want 1", stub.Hits())
	}
	if _, ok := stub.Request(0)["tools"]; ok {
		t.Error("an ungranted tool was offered to the model")
	}
}

// TestFailingToolGoesBackToTheModel covers the recovery path: a tool that fails does
// not fail the request. The model gets the error as the tool result and can answer.
func TestFailingToolGoesBackToTheModel(t *testing.T) {
	stub := newToolStub(t,
		toolCallScript("call_1", "todo_complete", `{"id":"nope"}`),
		textScript("没找到那条待办"),
	)
	a := pluginApp(t)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config", "todos.read", "todos.write")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	out, err := a.PluginRequest(id, "llm.chat", `{"text":"把那条划掉","tools":["todo_complete"]}`)
	if err != nil {
		t.Fatalf("a failing tool must not fail the request: %v", err)
	}
	var reply struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Text != "没找到那条待办" {
		t.Errorf("text = %q", reply.Text)
	}

	// The tool result that went back carries the failure, not a success.
	msgs, _ := stub.Request(1)["messages"].([]any)
	var toolContent string
	for _, item := range msgs {
		m := item.(map[string]any)
		if m["role"] == "tool" {
			toolContent, _ = m["content"].(string)
		}
	}
	if toolContent == "" {
		t.Fatal("no tool result reached the model")
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(toolContent), &res); err != nil {
		t.Fatalf("the tool result is not JSON: %v (%q)", err, toolContent)
	}
	if res.OK || res.Error == "" {
		t.Errorf("the tool result reports success: %q", toolContent)
	}
}

// TestToolFrameCarriesNoArguments pins the boundary: the plugin is told WHICH tool ran,
// not what it was given.
//
// The arguments can hold the user's todo text, and reading that requires todos.read -
// which receiving events does not imply. A frame with the arguments in it would be a
// way around the permission.
func TestToolFrameCarriesNoArguments(t *testing.T) {
	stub := newToolStub(t,
		toolCallScript("call_1", "todo_add", `{"title":"秘密的待办标题"}`),
		textScript("好"),
	)
	a := pluginApp(t)
	collected := collectPluginEvents(a)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config", "todos.read", "todos.write")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"加一条","tools":["todo_add"]}`); err != nil {
		t.Fatalf("llm.chat: %v", err)
	}

	frames := collected("llm.tool")
	if len(frames) != 1 {
		t.Fatalf("got %d llm.tool frames, want 1", len(frames))
	}
	payload, ok := frames[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("the payload is %T", frames[0].Payload)
	}
	if payload["name"] != "todo_add" {
		t.Errorf("name = %v, want todo_add", payload["name"])
	}
	if payload["ok"] != true {
		t.Errorf("ok = %v, want true", payload["ok"])
	}
	// The assertion that matters: nothing resembling the arguments.
	encoded, _ := json.Marshal(payload)
	for _, forbidden := range []string{"秘密的待办标题", "arguments", "args", "title"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("the frame leaks %q: %s", forbidden, encoded)
		}
	}
}

// TestToolLoopStopsAtTheRoundLimit covers the cap. The model that keeps asking for
// tools instead of answering must not be able to keep the request alive forever.
func TestToolLoopStopsAtTheRoundLimit(t *testing.T) {
	stub := newToolStub(t, toolCallScript("call_1", "todo_list", `{}`))
	a := pluginApp(t)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config", "todos.read")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"一直调","tools":["todo_list"]}`); err == nil {
		t.Fatal("a model that only ever asks for tools must eventually fail")
	}
	if stub.Hits() != maxToolRounds {
		t.Errorf("the endpoint saw %d requests, want exactly maxToolRounds (%d)", stub.Hits(), maxToolRounds)
	}
}
