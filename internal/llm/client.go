package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Settings is everything the client needs to talk to one endpoint. It is
// deliberately a plain struct rather than store.Config, so this package stays
// independent of the app's persistence and can be tested with a literal.
type Settings struct {
	BaseURL     string
	Model       string
	APIKey      string
	Temperature float64
	MaxTokens   int
	Timeout     time.Duration
}

// Handlers receive the streaming output.
//
// Both callbacks get already-batched text (see Coalescer) and may be nil. They
// are called from the goroutine that owns the request, one at a time, so they do
// not need to be reentrant - but they must be quick, because the response body
// is not being read while they run.
type Handlers struct {
	// OnText receives the visible reply, in batches.
	OnText func(string)
	// OnReasoning receives a thinking model's chain of thought, in batches.
	// Keeping it separate means the transcript never shows reasoning as if the
	// pet had said it.
	OnReasoning func(string)
}

// Result is what a finished (or cancelled) request produced.
type Result struct {
	// Text is the complete visible reply, including anything already delivered
	// through OnText. Having the whole thing in the result is what lets the
	// frontend repair itself if an event was dropped.
	Text string
	// Reasoning is the chain of thought, when the model emitted one.
	Reasoning string
	// Usage is the server's token accounting, or nil when it did not report it.
	Usage *Usage
	// FinishReason is "stop", "length", "content_filter" or "" if the stream
	// ended without one.
	FinishReason string
	// Model is the model name the server reported.
	Model string
	// ToolCalls is what the model asked to run, merged from the stream fragments and
	// in index order. Empty for every reply that is just text.
	//
	// Non-empty ToolCalls with EMPTY Text is the ordinary shape of a tool round: the
	// model says nothing and asks for a function instead. A caller that reads empty
	// text as "the model had nothing to say" will silently drop the request - which
	// is why FinishReason is "tool_calls" on exactly those rounds.
	ToolCalls []ToolCall
	// Attempts is how many HTTP requests were made (1 unless a retry happened).
	Attempts int
	// ElapsedMS is the wall-clock duration of the whole call.
	ElapsedMS int64
}

// maxErrorBody caps how much of a failed response is read. An error page can be
// megabytes, and none of it is useful.
const maxErrorBody = 8 << 10

// maxStreamBody caps the raw copy kept for the non-streaming fallback.
const maxStreamBody = 1 << 20

// DefaultRetryBackoff is the delay before retry n (0-indexed).
//
// Two attempts, with a long second wait: 500ms covers a transient blip, and
// 1.5s covers a gateway that is briefly overloaded. A third attempt is not worth
// it - by then the user is staring at a stalled reply and would rather have the
// error and a retry button.
var DefaultRetryBackoff = []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond}

// Client performs chat completions against an OpenAI-compatible endpoint.
//
// It is safe for concurrent use. The only mutable state is the note about
// whether the endpoint accepts `stream_options`, which is learned from a
// rejected request and then remembered so the user pays for that discovery once
// rather than on every message.
type Client struct {
	// HTTP is the underlying client. It is exported so tests can substitute a
	// transport, and so the app can install one with a proxy if it ever needs to.
	HTTP *http.Client

	// RetryBackoff overrides DefaultRetryBackoff. It exists so tests can assert
	// the retry POLICY without spending two seconds per case sleeping; leaving it
	// nil uses the real delays.
	RetryBackoff []time.Duration

	// Logf, when set, receives one diagnostic line per interesting step of a call:
	// the endpoint and model being asked, the HTTP status, a retry and its reason,
	// the finished stream's frame count and token usage.
	//
	// It NEVER receives the request messages or the reply text, and it must not:
	// this package is the one place that holds both, and the log it feeds is a file
	// the user may paste into a bug report (the control center's 诊断报告). What is
	// logged is SHAPE - which endpoint, which status, how many frames, how many
	// tokens - because that is what separates "the key is wrong" from "the gateway
	// answered in a dialect nobody can parse".
	//
	// Nil is silent and is the default, so a test or a second caller costs nothing.
	Logf func(format string, args ...any)

	mu              sync.Mutex
	noStreamOptions bool
}

// logf writes one line, if anyone is listening.
func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// NewClient returns a client with sensible transport timeouts.
//
// The transport's dial timeout is separate from the per-request deadline: a
// machine with no route to the internet should fail in ten seconds with "network
// unreachable", not sit for the full request timeout pretending to connect.
func NewClient() *Client {
	return &Client{
		HTTP: &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 60 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
	}
}

// Chat sends one completion request and streams the answer to h.
//
// The returned error is always a *ChatError, already translated for display.
// On a transport failure the Result may still carry a partial reply: a stream
// that dies halfway is a real case (a laptop changing Wi-Fi), and throwing away
// the text the user already watched appear would be worse than showing it with a
// note.
func (c *Client) Chat(ctx context.Context, s Settings, msgs []Message, h Handlers) (Result, error) {
	return c.chat(ctx, s, msgs, nil, h)
}

// ChatWithTools is Chat with a list of callable functions.
//
// A separate entry point rather than a parameter on Chat, because every existing
// caller means "just talk to me": adding an argument would have made them all spell
// out `nil` for a feature they do not use, and the byte-identical-body guarantee
// below is easiest to keep when the no-tools path is a different function.
//
// A reply may come back with EMPTY text and a non-empty Result.ToolCalls: the model
// asked for a function instead of answering. The caller decides whether to run them
// and send another round - this layer does not loop.
func (c *Client) ChatWithTools(ctx context.Context, s Settings, msgs []Message, tools []ToolSpec, h Handlers) (Result, error) {
	return c.chat(ctx, s, msgs, tools, h)
}

func (c *Client) chat(ctx context.Context, s Settings, msgs []Message, tools []ToolSpec, h Handlers) (Result, error) {
	start := time.Now()
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr *ChatError
	backoff := c.RetryBackoff
	if backoff == nil {
		backoff = DefaultRetryBackoff
	}
	for attempt := 1; attempt <= len(backoff)+1; attempt++ {
		if attempt > 1 {
			c.logf("第 %d 次尝试（上一次：%s）", attempt, lastErr.Message)
		}
		res, err := c.attempt(ctx, s, msgs, tools, h)
		res.Attempts = attempt

		if err == nil {
			res.ElapsedMS = time.Since(start).Milliseconds()
			c.logf("完成：%d 字，%d 个工具调用，finish=%s，耗时 %dms", len([]rune(res.Text)), len(res.ToolCalls), res.FinishReason, res.ElapsedMS)
			return res, nil
		}
		lastErr = err
		lastErr.Attempts = attempt
		c.logf("失败（可重试=%v，已收到 %d 字）：%s", err.Retryable, len([]rune(res.Text)), err.Message)

		// A retry is only safe while nothing has been shown. Once text has
		// reached the transcript, sending the request again would make the reply
		// restart in the middle - visible, confusing, and paid for twice.
		if !err.Retryable || res.Text != "" || attempt > len(backoff) {
			res.Attempts = attempt
			res.ElapsedMS = time.Since(start).Milliseconds()
			return res, err
		}
		c.logf("等 %s 后重试", backoff[attempt-1])
		if !sleepCtx(ctx, backoff[attempt-1]) {
			res.Attempts = attempt
			res.ElapsedMS = time.Since(start).Milliseconds()
			return res, &ChatError{Kind: KindCanceled, Message: "已取消。", Attempts: attempt, Err: ctx.Err()}
		}
	}
	return Result{ElapsedMS: time.Since(start).Milliseconds()}, lastErr
}

// attempt performs exactly one HTTP request.
func (c *Client) attempt(ctx context.Context, s Settings, msgs []Message, tools []ToolSpec, h Handlers) (Result, *ChatError) {
	var res Result

	endpoint := strings.TrimRight(s.BaseURL, "/") + "/chat/completions"
	if s.BaseURL == "" || s.Model == "" {
		return res, newChatError(KindConfig, 0, "还没有配置模型服务，请先到设置里填写接口地址和模型名。", nil)
	}

	body := ChatRequest{
		Model:       s.Model,
		Messages:    msgs,
		Stream:      true,
		Temperature: s.Temperature,
		MaxTokens:   s.MaxTokens,
		Tools:       tools,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return res, newChatError(KindOther, 0, "构造请求失败。", err)
	}
	// Ask for the usage report in the final chunk. DeepSeek sends it anyway, but
	// OpenAI-compatible servers generally only include it when asked. If the
	// endpoint rejects the field, the next attempt drops it (see below), so the
	// cost of asking is one retry against an unusual gateway and never a
	// permanently broken configuration.
	if !c.streamOptionsDisabled() {
		payload = withStreamOptions(payload)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return res, newChatError(KindOther, 0, "构造请求失败。", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if s.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.APIKey)
	}

	// The shape of the request, never its content: the endpoint and model answer
	// "which service is this install actually talking to", the message count
	// answers "was the history attached", and neither can leak the user's words.
	c.logf("POST %s model=%s 消息 %d 条 工具 %d 个 温度 %g max_tokens=%d",
		endpoint, s.Model, len(msgs), len(tools), s.Temperature, s.MaxTokens)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return res, classifyTransport(ctx, s.BaseURL, err)
	}
	defer resp.Body.Close()
	c.logf("HTTP %d（Content-Type: %s）", resp.StatusCode, resp.Header.Get("Content-Type"))

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		detail := extractDetail(string(raw))

		// An endpoint that does not know `stream_options` says so explicitly.
		// Learning that here keeps the setting invisible to the user. The error
		// is marked retryable because the retry is the fix: the next attempt
		// omits the field.
		if resp.StatusCode == http.StatusBadRequest && strings.Contains(detail, "stream_options") {
			c.disableStreamOptions()
			c.logf("这个端点不认 stream_options，下一次尝试会去掉它")
			return res, retryableError(KindRequest, resp.StatusCode,
				"该服务不支持 stream_options，已自动重试。", nil)
		}
		ce := classifyResponse(resp.StatusCode, string(raw))
		if ce.Message != "" {
			ce.Detail = detail
		}
		return res, ce
	}

	return c.consume(ctx, resp.Body, h)
}

// consume reads the SSE stream and fills in the result.
func (c *Client) consume(ctx context.Context, body io.Reader, h Handlers) (Result, *ChatError) {
	var res Result

	var textC, reasonC *Coalescer
	if h.OnText != nil {
		textC = NewCoalescer(h.OnText)
	}
	if h.OnReasoning != nil {
		reasonC = NewCoalescer(h.OnReasoning)
	}

	var (
		text    strings.Builder
		reason  strings.Builder
		raw     bytes.Buffer
		dec     = NewDecoder()
		readBuf = make([]byte, 4096)
		// frames counts what the decoder produced, for the one summary line at the
		// end: "0 frames from a 200 response" is the signature of a gateway that
		// answered in a dialect this client cannot read, and it is invisible from
		// the reply alone (it is empty either way).
		frames int
	)

	handle := func(events []Event) *ChatError {
		for _, ev := range events {
			frames++
			if ev.Done {
				res.FinishReason = orDefault(res.FinishReason, "stop")
				continue
			}
			f, err := ParseFrame(ev.Data)
			if err != nil {
				// A gateway that reports a failure inside a 200 response.
				var apiErr *APIError
				if asAPIError(err, &apiErr) {
					return newChatError(KindServer, 200, "服务端返回错误："+redact(apiErr.Message), nil)
				}
				return newChatError(KindProtocol, 200, "响应格式无法解析，该地址可能不是 OpenAI 兼容接口。", err)
			}
			if f.Usage != nil {
				res.Usage = f.Usage
			}
			if f.Model != "" {
				res.Model = f.Model
			}
			if f.FinishReason != "" {
				res.FinishReason = f.FinishReason
			}
			for _, d := range f.ToolCalls {
				res.addToolCallDelta(d)
			}
			if f.Reasoning != "" {
				reason.WriteString(f.Reasoning)
				if reasonC != nil {
					reasonC.Add(f.Reasoning)
				}
			}
			if f.Text != "" {
				text.WriteString(f.Text)
				if textC != nil {
					textC.Add(f.Text)
				}
			}
		}
		return nil
	}

	for {
		// The read has to be interrupted by the caller's cancellation, which
		// closing the response body does; checking the context here keeps the
		// error honest rather than surfacing as a bare "connection reset".
		if err := ctx.Err(); err != nil {
			return finishResult(res, text.String(), reason.String(), textC, reasonC), classifyTransport(ctx, "", err)
		}
		n, err := body.Read(readBuf)
		if n > 0 {
			chunk := readBuf[:n]
			if raw.Len() < maxStreamBody {
				raw.Write(chunk)
			}
			ce := handle(dec.Feed(chunk))
			if ce != nil {
				return finishResult(res, text.String(), reason.String(), textC, reasonC), ce
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			ce := handle(dec.Finish())
			if ce != nil {
				return finishResult(res, text.String(), reason.String(), textC, reasonC), ce
			}
			// A stream that dies mid-reply still yields a partial result.
			return finishResult(res, text.String(), reason.String(), textC, reasonC), classifyTransport(ctx, "", err)
		}
	}

	// Flush anything the decoder was still holding: a server that closes without
	// a trailing blank line would otherwise lose its last delta.
	if ce := handle(dec.Finish()); ce != nil {
		return finishResult(res, text.String(), reason.String(), textC, reasonC), ce
	}

	res = finishResult(res, text.String(), reason.String(), textC, reasonC)
	if res.Usage != nil {
		c.logf("流结束：%d 个 SSE 事件，%d 字，tokens in=%d out=%d", frames, len([]rune(res.Text)), res.Usage.PromptTokens, res.Usage.CompletionTokens)
	} else {
		c.logf("流结束：%d 个 SSE 事件，%d 字，服务端没报用量", frames, len([]rune(res.Text)))
	}

	// The non-streaming fallback. A server that ignores `stream: true` answers
	// with one ordinary JSON body, which produces no SSE events at all; without
	// this the user would see an empty reply from a service that actually
	// answered correctly.
	if res.Text == "" && res.Reasoning == "" {
		if text, usage, finish, model, ok := parseNonStreaming(raw.Bytes()); ok {
			c.logf("这不是 SSE：按一次性 JSON 回复处理（%d 字节）", raw.Len())
			res.Text = text
			res.Usage = usage
			res.FinishReason = orDefault(finish, "stop")
			if model != "" {
				res.Model = model
			}
			// The text has to be PUSHED, not just stored. Setting res.Text and
			// stopping there left the UI with an empty bubble next to a result
			// that had the whole reply in it - the frontend only ever renders
			// what the events carried.
			if text != "" {
				if textC != nil {
					textC.Add(text)
					textC.Flush()
				}
				if reasonC != nil {
					reasonC.Flush()
				}
			}
		}
	}
	return res, nil
}

// finishResult flushes the batchers and records the totals.
//
// Flushing here, on every exit path, is what guarantees the transcript matches
// the delivered events: a reply that ends one batch short of complete is the
// classic symptom of forgetting this.
// addToolCallDelta merges one streamed fragment into r.ToolCalls.
//
// The fragments are keyed by index and interleave when the model asks for several
// tools at once, so this genuinely merges rather than appends: id and name arrive
// once (on the first fragment for that index) and arguments accumulate as text.
//
// A fragment with no index decodes to 0, which is right for the common single-call
// case. It would be wrong for a multi-call stream from a gateway that omits the
// index - and there is no way to tell those apart from inside one frame, so the
// behaviour is documented here rather than guessed at.
func (r *Result) addToolCallDelta(d ToolCallDelta) {
	for len(r.ToolCalls) <= d.Index {
		r.ToolCalls = append(r.ToolCalls, ToolCall{Type: "function"})
	}
	cur := &r.ToolCalls[d.Index]
	if d.ID != "" {
		cur.ID = d.ID
	}
	if d.Name != "" {
		cur.Function.Name = d.Name
	}
	cur.Function.Arguments += d.Arguments
}

func finishResult(res Result, text, reason string, textC, reasonC *Coalescer) Result {
	if textC != nil {
		textC.Flush()
	}
	if reasonC != nil {
		reasonC.Flush()
	}
	res.Text = text
	res.Reasoning = reason
	return res
}

// withStreamOptions adds {"stream_options":{"include_usage":true}} to a marshalled
// request body.
//
// It is done by re-marshalling into a map rather than by adding a field to
// ChatRequest so that the field can be omitted entirely from the wire format
// when the endpoint does not support it. Sending `"stream_options":null` would
// be a different request, and some gateways reject it.
func withStreamOptions(payload []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return payload
	}
	m["stream_options"] = map[string]any{"include_usage": true}
	out, err := json.Marshal(m)
	if err != nil {
		return payload
	}
	return out
}

// parseNonStreaming reads a single-response body.
//
// It reports false when the body is not a chat completion at all, which is the
// normal case for a stream that simply produced no content.
func parseNonStreaming(raw []byte) (text string, usage *Usage, finish, model string, ok bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", nil, "", "", false
	}
	var c chunk
	if err := json.Unmarshal(trimmed, &c); err != nil {
		return "", nil, "", "", false
	}
	if c.Error != nil {
		return "", nil, "", "", false
	}
	if len(c.Choices) == 0 {
		return "", nil, "", "", false
	}
	ch := c.Choices[0]
	t := ch.Message.Content
	if t == "" {
		t = ch.Delta.Content
	}
	if t == "" && c.Usage == nil {
		return "", nil, "", "", false
	}
	if ch.FinishReason != nil {
		finish = *ch.FinishReason
	}
	return t, c.Usage, finish, c.Model, true
}

// streamOptionsDisabled reports whether this endpoint has rejected
// `stream_options` before.
func (c *Client) streamOptionsDisabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.noStreamOptions
}

func (c *Client) disableStreamOptions() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.noStreamOptions = true
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// sleepCtx waits for d, reporting false if the context ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// asAPIError is errors.As for the concrete pointer type, kept local so the
// call site reads clearly.
func asAPIError(err error, target **APIError) bool {
	e, ok := err.(*APIError)
	if ok {
		*target = e
	}
	return ok
}

// String makes Settings printable for the debug log without exposing the key.
func (s Settings) String() string {
	key := "none"
	if s.APIKey != "" {
		key = "set"
	}
	return fmt.Sprintf("{base=%s model=%s key=%s temp=%g max=%d timeout=%s}",
		s.BaseURL, s.Model, key, s.Temperature, s.MaxTokens, s.Timeout)
}
