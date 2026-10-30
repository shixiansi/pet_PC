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

// TestToolLoopRunsTheToolAndReturnsTheFinalText is the whole feature in one test: the
// model asks for pet_say, the host runs it for real (the pet is saying something
// afterwards), the result goes back to the model, and the user gets the second round's
// text.
//
// The tool used to be `todo_add`, which made the effect trivially observable ("the todo
// exists"). With the three todo tools gone (the list is another plugin's data now), the
// surviving pair is pet_say and memory_*; pet_say is the one with a visible effect that
// needs no file to be read back.
func TestToolLoopRunsTheToolAndReturnsTheFinalText(t *testing.T) {
	stub := newToolStub(t,
		toolCallScript("call_1", "pet_say", `{"text":"写完了"}`),
		textScript("说过了"),
	)
	a := pluginApp(t)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config", "pet.bubble")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	out, err := a.PluginRequest(id, "llm.chat", `{"text":"跟他说一句","tools":["pet_say"]}`)
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
	if name := tools[0].(map[string]any)["function"].(map[string]any)["name"]; name != "pet_say" {
		t.Errorf("the offered tool is %v, want pet_say", name)
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

	// The tool really ran: the pet is saying the line the model asked for.
	a.mu.Lock()
	bubble := a.bubble
	a.mu.Unlock()
	if bubble == nil {
		t.Fatal("pet_say left no bubble: the tool did not run")
	}
	if bubble.Title != "写完了" {
		t.Errorf("bubble title = %q, want the text the model passed", bubble.Title)
	}

	var reply struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("decode the reply: %v", err)
	}
	if reply.Text != "说过了" {
		t.Errorf("text = %q, want 说过了", reply.Text)
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
	id := installToolPlugin(t, a, "llm.chat", "plugin.config") // no pet.bubble
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"帮我说一句","tools":["pet_say"]}`); err != nil {
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
		// pet_say with no text is refused by the capability itself, so the failure comes
		// from the tool rather than from a missing permission - which is the case this
		// test is about.
		toolCallScript("call_1", "pet_say", `{}`),
		textScript("没说出什么"),
	)
	a := pluginApp(t)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config", "pet.bubble")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	out, err := a.PluginRequest(id, "llm.chat", `{"text":"说点什么","tools":["pet_say"]}`)
	if err != nil {
		t.Fatalf("a failing tool must not fail the request: %v", err)
	}
	var reply struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Text != "没说出什么" {
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
// The arguments can hold the user's own words - the line the pet is asked to say, or a
// memory note - and receiving an event does not imply permission to read them. A frame
// with the arguments in it would be a way around that.
func TestToolFrameCarriesNoArguments(t *testing.T) {
	stub := newToolStub(t,
		toolCallScript("call_1", "pet_say", `{"text":"秘密要说的话"}`),
		textScript("好"),
	)
	a := pluginApp(t)
	collected := collectPluginEvents(a)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config", "pet.bubble")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"说一句","tools":["pet_say"]}`); err != nil {
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
	if payload["name"] != "pet_say" {
		t.Errorf("name = %v, want pet_say", payload["name"])
	}
	if payload["ok"] != true {
		t.Errorf("ok = %v, want true", payload["ok"])
	}
	// The assertion that matters: nothing resembling the arguments.
	encoded, _ := json.Marshal(payload)
	for _, forbidden := range []string{"秘密要说的话", "arguments", "args", "text"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("the frame leaks %q: %s", forbidden, encoded)
		}
	}
}

// TestUnknownToolIsSkippedNotFatal covers the version skew: a plugin newer than the
// host asks for a tool name the host has never heard of.
//
// Not hypothetical - it is what happened the day memory_read was added. The old host
// failed the WHOLE request with "未知工具 memory_read", which from the user's side was
// indistinguishable from "granting the permission did nothing".
func TestUnknownToolIsSkippedNotFatal(t *testing.T) {
	stub := newToolStub(t, textScript("好"))
	a := pluginApp(t)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config", "storage", "pet.bubble")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	out, err := a.PluginRequest(id, "llm.chat",
		`{"text":"随便说点","tools":["tool_from_the_future","pet_say"]}`)
	if err != nil {
		t.Fatalf("an unknown tool name must not fail the request: %v", err)
	}
	if out == "" {
		t.Error("the request produced no reply")
	}

	// The known tool still reaches the model; the unknown one does not.
	tools, ok := stub.Request(0)["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("the request offers %+v, want exactly pet_say", stub.Request(0)["tools"])
	}
	name := tools[0].(map[string]any)["function"].(map[string]any)["name"]
	if name != "pet_say" {
		t.Errorf("the offered tool is %v, want pet_say", name)
	}
}

// TestToolLoopStopsAtTheRoundLimit covers the cap. The model that keeps asking for
// tools instead of answering must not be able to keep the request alive forever.
func TestToolLoopStopsAtTheRoundLimit(t *testing.T) {
	stub := newToolStub(t, toolCallScript("call_1", "memory_read", `{}`))
	a := pluginApp(t)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config", "storage")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"一直调","tools":["memory_read"]}`); err == nil {
		t.Fatal("a model that only ever asks for tools must eventually fail")
	}
	if stub.Hits() != maxToolRounds {
		t.Errorf("the endpoint saw %d requests, want exactly maxToolRounds (%d)", stub.Hits(), maxToolRounds)
	}
}
// reasoningOnlyScript is a model that spent its whole output budget thinking: the stream carries
// `reasoning_content` and never a `content` delta, and it still finishes with "stop".
//
// This is not a hypothetical shape - it is what a reasoning model does when max_tokens is smaller
// than its chain of thought, and the real machine logged one of these as "完成，回复 0 字"
// (tokens out=1353). See the empty-reply branch in app_plugins.go's `finish`.
func reasoningOnlyScript(thought string) []string {
	body, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{"delta": map[string]any{"reasoning_content": thought}}},
	})
	end := map[string]any{"choices": []map[string]any{{"delta": map[string]any{}, "finish_reason": "stop"}}}
	b, _ := json.Marshal(end)
	return []string{string(body), string(b)}
}
