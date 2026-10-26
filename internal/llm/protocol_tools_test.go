package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMessagesWithoutToolsAreByteIdenticalToBefore covers the promise the custom
// marshaller makes: adding tools must be INVISIBLE to every request that does not
// use them.
//
// This is the assertion that lets the feature ship without renegotiating anything
// with the endpoints plugins already point at: a gateway that has never heard of
// `tools`, `tool_calls` or the `tool` role must see exactly the body it saw before
// tools existed - same keys, same order, no null.
func TestMessagesWithoutToolsAreByteIdenticalToBefore(t *testing.T) {
	got, err := json.Marshal(Message{Role: "user", Content: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"role":"user","content":"你好"}`; string(got) != want {
		t.Errorf("a plain message marshals to %s, want %s", got, want)
	}

	body, err := json.Marshal(ChatRequest{
		Model:    "m",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// `"tools":null` would be a key the old body never had. omitempty is the whole
	// mechanism, so it is asserted rather than assumed.
	for _, key := range []string{"tools", "tool_choice", "tool_calls", "tool_call_id"} {
		if strings.Contains(string(body), `"`+key+`"`) {
			t.Errorf("a tool-free request carries %q: %s", key, body)
		}
	}
}

// TestToolSpecsReachTheWire checks that a schema arrives as an OBJECT.
//
// Parameters is json.RawMessage on purpose; if it were ever changed to a string
// field the schema would be sent double-encoded and every gateway would reject the
// tool without saying why.
func TestToolSpecsReachTheWire(t *testing.T) {
	schema := `{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}`
	body, err := json.Marshal(ChatRequest{
		Model: "m",
		Tools: []ToolSpec{{
			Type:     "function",
			Function: ToolFunction{Name: "todo_add", Description: "新增一条待办", Parameters: json.RawMessage(schema)},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var back struct {
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name       string          `json:"name"`
				Parameters json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("the body does not decode: %v (%s)", err, body)
	}
	if len(back.Tools) != 1 {
		t.Fatalf("tools = %d, want 1 (%s)", len(back.Tools), body)
	}
	if back.Tools[0].Type != "function" {
		t.Errorf("type = %q, want function", back.Tools[0].Type)
	}
	if back.Tools[0].Function.Name != "todo_add" {
		t.Errorf("name = %q", back.Tools[0].Function.Name)
	}
	// The schema must come back as the same JSON OBJECT, not as a quoted string.
	var schemaBack map[string]any
	if err := json.Unmarshal(back.Tools[0].Function.Parameters, &schemaBack); err != nil {
		t.Fatalf("parameters are not a JSON object: %v (%s)", err, back.Tools[0].Function.Parameters)
	}
	if schemaBack["type"] != "object" {
		t.Errorf("parameters.type = %v, want object", schemaBack["type"])
	}
}

// TestStreamingToolCallsAccumulateByIndex pins the shape of a streamed tool call.
//
// Three properties, each of which a naive "decode the frame" implementation gets
// wrong:
//
//   - `arguments` arrives as a STRING IN PIECES, so it has to be concatenated and
//     decoded once at the end - decoding a fragment fails on the first frame;
//   - the id and the function name appear only on the FIRST fragment for an index;
//   - the round ends with finish_reason "tool_calls", which is the host's signal to
//     run them rather than to return the (empty) text.
func TestStreamingToolCallsAccumulateByIndex(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"todo_add","arguments":"{\"ti"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"tle\":\"写周报\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}

	var args strings.Builder
	var id, name string
	var last Frame
	for i, raw := range frames {
		f, err := ParseFrame(raw)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		for _, d := range f.ToolCalls {
			if d.Index != 0 {
				t.Errorf("frame %d: index = %d, want 0", i, d.Index)
			}
			if d.ID != "" {
				id = d.ID
			}
			if d.Name != "" {
				name = d.Name
			}
			args.WriteString(d.Arguments)
		}
		last = f
	}

	if id != "call_a" {
		t.Errorf("id = %q, want call_a (it is sent once, on the first fragment)", id)
	}
	if name != "todo_add" {
		t.Errorf("name = %q, want todo_add (also first-fragment only)", name)
	}
	if last.FinishReason != "tool_calls" {
		t.Errorf("finish reason = %q, want tool_calls", last.FinishReason)
	}
	var decoded struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(args.String()), &decoded); err != nil {
		t.Fatalf("the concatenated arguments are not JSON: %v (%q)", err, args.String())
	}
	if decoded.Title != "写周报" {
		t.Errorf("title = %q, want 写周报", decoded.Title)
	}
}

// TestResultMergesInterleavedToolCallFragments covers the merge itself, rather than
// the frame parsing above it.
//
// Two calls, four fragments, interleaved and with the argument text split at awkward
// points. The result must be two complete calls, each with its own id and its own
// decoded arguments - which is the property that makes it safe to run them.
func TestResultMergesInterleavedToolCallFragments(t *testing.T) {
	var res Result
	for _, d := range []ToolCallDelta{
		{Index: 0, ID: "call_a", Name: "todo_add", Arguments: `{"ti`},
		{Index: 1, ID: "call_b", Name: "pet_say", Arguments: `{"text":"好`},
		{Index: 0, Arguments: `tle":"写周报"}`},
		{Index: 1, Arguments: `的"}`},
	} {
		res.addToolCallDelta(d)
	}

	if len(res.ToolCalls) != 2 {
		t.Fatalf("merged %d calls, want 2", len(res.ToolCalls))
	}
	if res.ToolCalls[0].ID != "call_a" || res.ToolCalls[0].Function.Name != "todo_add" {
		t.Errorf("call 0 = %+v", res.ToolCalls[0])
	}
	if res.ToolCalls[1].ID != "call_b" || res.ToolCalls[1].Function.Name != "pet_say" {
		t.Errorf("call 1 = %+v", res.ToolCalls[1])
	}
	for i, c := range res.ToolCalls {
		if c.Type != "function" {
			t.Errorf("call %d type = %q, want function", i, c.Type)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(c.Function.Arguments), &decoded); err != nil {
			t.Errorf("call %d arguments are not JSON: %v (%q)", i, err, c.Function.Arguments)
		}
	}
}

// TestToolTurnsMarshalWithTheirOwnShape pins the two new message shapes: the
// assistant turn that ASKED, and the tool turn that ANSWERS.
//
// A `tool_calls` key on the answer, or a `tool_call_id` on the question, is rejected
// by every gateway - so each shape is asserted whole rather than by substring.
func TestToolTurnsMarshalWithTheirOwnShape(t *testing.T) {
	var call ToolCall
	call.ID = "call_a"
	call.Type = "function"
	call.Function.Name = "todo_add"
	call.Function.Arguments = `{"title":"写周报"}`

	asked, err := json.Marshal(Message{Role: "assistant", ToolCalls: []ToolCall{call}})
	if err != nil {
		t.Fatal(err)
	}
	var askedBack map[string]any
	if err := json.Unmarshal(asked, &askedBack); err != nil {
		t.Fatalf("the assistant turn does not decode: %v (%s)", err, asked)
	}
	if _, ok := askedBack["tool_calls"]; !ok {
		t.Errorf("the assistant turn has no tool_calls: %s", asked)
	}
	if _, ok := askedBack["tool_call_id"]; ok {
		t.Errorf("the assistant turn must not carry tool_call_id: %s", asked)
	}

	answered, err := json.Marshal(Message{Role: "tool", ToolCallID: "call_a", Content: `{"ok":true}`})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"role":"tool","content":"{\"ok\":true}","tool_call_id":"call_a"}`
	if string(answered) != want {
		t.Errorf("a tool result marshals to %s, want %s", answered, want)
	}
}
