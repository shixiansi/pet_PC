package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Message is one turn sent to the API.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Images are absolute http(s) URLs of pictures the model may look at, in
	// addition to the text. Empty for every text-only request, which is what keeps
	// the wire format byte-identical for everything that existed before vision.
	//
	// The URLs are NOT downloaded by us: a vision endpoint fetches them itself, so
	// routing the bytes through this process would cost a round trip and a base64
	// blow-up for nothing.
	Images []string `json:"-"`
	// ToolCalls makes this an ASSISTANT turn that asked to run tools. Set by the
	// caller replaying what the model said in a previous round.
	//
	// It is `json:"-"` because the wire shape differs by role and MarshalJSON is the
	// one place that decides it (see below); a `tool_calls` key on a tool RESULT
	// message would be rejected by every gateway.
	ToolCalls []ToolCall `json:"-"`
	// ToolCallID makes this a TOOL RESULT: the answer to one specific call, which
	// the model matches by id. Required whenever Role is "tool".
	ToolCallID string `json:"-"`
}

// imagePart is the OpenAI-compatible content part for one picture.
type imagePart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

// MarshalJSON emits the shape the ROLE needs, and nothing else.
//
// Three shapes, in this order:
//
//  1. an assistant turn that asked for tools: role, optional content, tool_calls;
//  2. a tool result: role, content, tool_call_id;
//  3. plain text, or the content-part array when pictures are attached.
//
// A custom marshaller rather than extra fields: the request body must keep exactly
// the shape every existing gateway already accepts. In particular branch 3 emits the
// same two keys it always has, so a request that carries no tools and no images is
// byte-identical to what this app sent before tools existed - which is what lets the
// feature ship without asking every plugin's endpoint to re-accept anything.
func (m Message) MarshalJSON() ([]byte, error) {
	if len(m.ToolCalls) > 0 {
		return json.Marshal(struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		}{m.Role, m.Content, m.ToolCalls})
	}
	if m.ToolCallID != "" {
		return json.Marshal(struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
		}{m.Role, m.Content, m.ToolCallID})
	}
	if len(m.Images) == 0 {
		return json.Marshal(struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{m.Role, m.Content})
	}
	parts := make([]imagePart, 0, len(m.Images)+1)
	if m.Content != "" {
		parts = append(parts, imagePart{Type: "text", Text: m.Content})
	}
	for _, url := range m.Images {
		part := imagePart{Type: "image_url"}
		part.ImageURL = &struct {
			URL string `json:"url"`
		}{URL: url}
		parts = append(parts, part)
	}
	return json.Marshal(struct {
		Role    string      `json:"role"`
		Content []imagePart `json:"content"`
	}{m.Role, parts})
}

// ChatRequest is the body of POST /chat/completions, limited to the fields this
// app sets. Anything left out is a server default, which is what we want: fewer
// knobs means fewer ways for a custom gateway to disagree with us.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	// Tools lists the functions the model may call. omitempty is load-bearing: a
	// request without tools must not grow a `"tools":null` key, because the whole
	// point is that this feature is invisible to every endpoint that does not use it.
	Tools []ToolSpec `json:"tools,omitempty"`
	// ToolChoice is left nil in every call this app makes, so the server default
	// ("auto": call a tool if you want) applies. It is declared so a caller that
	// needs "required" does not have to reach into the raw body.
	ToolChoice any `json:"tool_choice,omitempty"`
}

// ToolSpec is one function the model may call.
type ToolSpec struct {
	// Type is always "function" today; the field exists because the wire format has
	// it and a gateway that validates the schema will reject a tool without it.
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes one callable function.
type ToolFunction struct {
	Name string `json:"name"`
	// Description is the only thing the model has to decide with, so it is written
	// for the model rather than for the user: what the function does, and when NOT
	// to call it.
	Description string `json:"description"`
	// Parameters is a JSON Schema object. RawMessage rather than a struct because
	// the schema is data the host builds per tool, and re-encoding it through a
	// struct would only be a second place for it to be wrong.
	Parameters json.RawMessage `json:"parameters"`
}

// ToolCall is one tool invocation, in the shape it travels in BOTH directions: the
// model emits it, and the caller replays it in the assistant message it sends back.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
		// Arguments is a JSON-encoded STRING, not an object. That is the wire format
		// (and the reason it is not `json.RawMessage` here): the model may emit it in
		// pieces, and pieces can only be concatenated as text.
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ToolCallDelta is one FRAGMENT of a tool call from a streaming frame.
//
// Streaming tool calls arrive in pieces: the first fragment for an index carries the
// id and the function name, and later fragments carry only more of Arguments. Index
// is what ties them together - a model may ask for several tools in one reply, and
// their fragments interleave.
type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// Usage is the token accounting a server may report. It is omitted by some
// gateways and only ever treated as informational.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// chunk is one decoded SSE payload: the subset of the OpenAI streaming schema
// this app reads.
//
// Every field is a pointer or has an omitempty distinction because the same
// struct is used for the terminal frames, where `choices` is an empty array and
// only `usage` is present. A decoder that cannot tell "absent" from "empty"
// either loses a usage report or invents an empty reply.
type chunk struct {
	Choices []struct {
		Delta struct {
			// Content is the visible text. It is a plain string in every
			// OpenAI-compatible server this app targets.
			Content string `json:"content"`
			// ReasoningContent carries a thinking model's chain of thought
			// (DeepSeek-R1 and friends). It is collected separately and never
			// mixed into the reply: showing it as the pet's words would be
			// wrong, and dropping it silently would make the "thinking" pause
			// look like a hang.
			ReasoningContent string `json:"reasoning_content"`
			// Role is set on the first frame of some servers.
			Role string `json:"role"`
			// ToolCalls are the tool-call FRAGMENTS this frame carries. They arrive in
			// pieces keyed by index - see ToolCallDelta - and `arguments` is a string
			// that grows across frames, not an object.
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
	// Error is how a gateway reports a failure that arrives with HTTP 200 and a
	// JSON body - a real and easily-missed case, since a client that only looks
	// at the status code shows an empty reply instead of the reason.
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
	// Model is echoed back by most servers; the settings panel shows what
	// actually answered, which can differ from what was requested.
	Model string `json:"model"`
}

// Frame is one parsed step of a stream.
type Frame struct {
	// Text is the visible delta carried by this frame.
	Text string
	// Reasoning is the chain-of-thought delta, if the model emits one.
	Reasoning string
	// Done is true once the server has reported a finish reason or sent the
	// sentinel.
	Done bool
	// FinishReason is "" while the stream is running, then "stop", "length",
	// "content_filter" etc.
	FinishReason string
	// Usage is the final accounting, when the server reports it. Some only send
	// it if the request asked for it.
	Usage *Usage
	// Model is the model that actually answered.
	Model string
	// ToolCalls are the fragments this frame contributed, already flattened out of
	// the choice. The caller accumulates them by Index; an empty slice means this
	// frame said nothing about tools.
	ToolCalls []ToolCallDelta
}

// ParseFrame decodes one SSE payload.
//
// It tolerates the frames real gateways send but the schema does not promise:
// keep-alive comment events (already filtered by the decoder), `{}` heartbeats,
// frames with an empty choices array, and a non-streaming reply delivered in a
// single frame (which is how a server that ignored `stream:true` responds, and
// which has to be read from `message.content` rather than `delta.content`).
func ParseFrame(data string) (Frame, error) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" || trimmed == "{}" {
		return Frame{}, nil
	}

	var c chunk
	if err := json.Unmarshal([]byte(trimmed), &c); err != nil {
		// A frame that is not JSON at all is a protocol error worth surfacing:
		// silently skipping it would turn a broken gateway into "the model
		// replied nothing".
		return Frame{}, fmt.Errorf("解析响应帧失败: %w", err)
	}
	if c.Error != nil {
		return Frame{}, &APIError{Message: c.Error.Message, Type: c.Error.Type, Code: c.Error.Code}
	}

	f := Frame{Usage: c.Usage, Model: c.Model}
	for i, ch := range c.Choices {
		// Only the first choice is used: this app sends no `n`, so a second
		// choice would be a server-side surprise, and concatenating them would
		// produce gibberish.
		if i == 0 {
			f.Text = ch.Delta.Content
			f.Reasoning = ch.Delta.ReasoningContent
			if f.Text == "" && ch.Delta.Content == "" {
				// Non-streaming shape.
				f.Text = ch.Message.Content
			}
			// Tool-call fragments are passed through as they arrive, in the order the
			// server sent them. They are NOT merged here: accumulating by index is the
			// caller's job, because a fragment with no index (some gateways omit it
			// when there is only one call) has to be resolved against the calls that
			// already exist - which is state this stateless parser does not have.
			for _, tc := range ch.Delta.ToolCalls {
				f.ToolCalls = append(f.ToolCalls, ToolCallDelta{
					Index:     tc.Index,
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				})
			}
		}
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			f.Done = true
			f.FinishReason = *ch.FinishReason
		}
	}
	return f, nil
}

// APIError is an error the server itself reported, either as a non-2xx response
// or as a 200 response carrying an `error` object.
type APIError struct {
	Status  int
	Message string
	Type    string
	Code    any
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("服务端返回错误（HTTP %d）", e.Status)
}
