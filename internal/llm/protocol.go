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
