package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- helpers ---------------------------------------------------------------

// sseChunk builds one OpenAI-style streaming frame.
func sseChunk(content string) string {
	frame := map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"content": content}}},
	}
	b, _ := json.Marshal(frame)
	return "data: " + string(b) + "\n\n"
}

// reasonChunk builds a frame carrying reasoning_content instead of content.
func reasonChunk(text string) string {
	frame := map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"reasoning_content": text}}},
	}
	b, _ := json.Marshal(frame)
	return "data: " + string(b) + "\n\n"
}

// finishChunk builds the terminal frame with a finish reason and usage.
func finishChunk(reason string, usage map[string]any) string {
	frame := map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": reason}},
	}
	if usage != nil {
		frame["usage"] = usage
	}
	b, _ := json.Marshal(frame)
	return "data: " + string(b) + "\n\n"
}

// fakeServer is a scripted SSE endpoint.
type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests int
	// bodies keeps each request body for assertion.
	bodies []string
	// auth keeps each Authorization header.
	auth []string
}

// newFakeServer starts a server that runs handler for each request.
func newFakeServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, attempt int)) *fakeServer {
	t.Helper()
	fs := &fakeServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fs.mu.Lock()
		fs.requests++
		attempt := fs.requests
		fs.bodies = append(fs.bodies, string(body))
		fs.auth = append(fs.auth, r.Header.Get("Authorization"))
		fs.mu.Unlock()
		handler(w, r, attempt)
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (f *fakeServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakeServer) lastBody() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		return ""
	}
	return f.bodies[len(f.bodies)-1]
}

// serveSSE writes the given stream in chunks of the given size, flushing between
// them, which is how a real server behaves.
func serveSSE(w http.ResponseWriter, stream string, chunk int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	for i := 0; i < len(stream); i += chunk {
		end := i + chunk
		if end > len(stream) {
			end = len(stream)
		}
		fmt.Fprint(w, stream[i:end])
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func testSettings(base string) Settings {
	return Settings{
		BaseURL:     base,
		Model:       "test-model",
		APIKey:      "sk-test-0123456789abcdef",
		Temperature: 0.5,
		MaxTokens:   128,
		Timeout:     5 * time.Second,
	}
}

// fastClient returns a client whose retries are effectively instant, so a test
// can assert the retry POLICY without spending the real 500ms/1500ms waiting.
//
// The real delays are pinned separately by TestDefaultRetryBackoffIsNotInstant,
// and one test asserts an actual wait rather than a skipped one.
func fastClient() *Client {
	c := NewClient()
	c.RetryBackoff = []time.Duration{time.Millisecond, 2 * time.Millisecond}
	return c
}

// --- streaming happy path --------------------------------------------------

func TestChatStreamsInOrder(t *testing.T) {
	stream := sseChunk("你") + sseChunk("好") + sseChunk("呀") +
		finishChunk("stop", map[string]any{"prompt_tokens": 11, "completion_tokens": 3, "total_tokens": 14}) +
		"data: [DONE]\n\n"

	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		serveSSE(w, stream, 1024)
	})

	var got []string
	res, err := fastClient().Chat(context.Background(), testSettings(fs.URL), []Message{{Role: "user", Content: "hi"}}, Handlers{
		OnText: func(s string) { got = append(got, s) },
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Text != "你好呀" {
		t.Errorf("Text = %q, want %q", res.Text, "你好呀")
	}
	if strings.Join(got, "") != res.Text {
		t.Errorf("streamed %q but result is %q", strings.Join(got, ""), res.Text)
	}
	if res.FinishReason != "stop" {
		t.Errorf("FinishReason = %q", res.FinishReason)
	}
	if res.Usage == nil || res.Usage.PromptTokens != 11 || res.Usage.CompletionTokens != 3 {
		t.Errorf("usage = %+v", res.Usage)
	}
	if res.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", res.Attempts)
	}
	if fs.count() != 1 {
		t.Errorf("server saw %d requests, want 1", fs.count())
	}
}

// TestChatSurvivesEveryChunkBoundary is the end-to-end version of the decoder
// test: the network's segmentation must not change the reply.
func TestChatSurvivesEveryChunkBoundary(t *testing.T) {
	var stream strings.Builder
	for _, s := range []string{"第一段", "第二段，长一点", "第三段"} {
		stream.WriteString(sseChunk(s))
	}
	stream.WriteString(finishChunk("stop", nil))
	stream.WriteString("data: [DONE]\n\n")
	full := stream.String()
	want := "第一段第二段，长一点第三段"

	for chunk := 1; chunk <= 16; chunk++ {
		t.Run(fmt.Sprintf("chunk%d", chunk), func(t *testing.T) {
			fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
				serveSSE(w, full, chunk)
			})
			var got []string
			res, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{
				OnText: func(s string) { got = append(got, s) },
			})
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if res.Text != want {
				t.Fatalf("Text = %q, want %q", res.Text, want)
			}
			if strings.Join(got, "") != want {
				t.Fatalf("streamed %q, want %q", strings.Join(got, ""), want)
			}
		})
	}
}

func TestChatSendsExpectedRequest(t *testing.T) {
	var gotPath string
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		gotPath = r.URL.Path
		serveSSE(w, "data: [DONE]\n\n", 1024)
	})
	if _, err := fastClient().Chat(context.Background(), testSettings(fs.URL), []Message{
		{Role: "system", Content: "persona"},
		{Role: "user", Content: "你好"},
	}, Handlers{}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(fs.lastBody()), &body); err != nil {
		t.Fatalf("request body is not JSON: %v (%s)", err, fs.lastBody())
	}
	if body["model"] != "test-model" {
		t.Errorf("model = %v", body["model"])
	}
	if body["stream"] != true {
		t.Errorf("stream = %v, want true", body["stream"])
	}
	if body["temperature"] != 0.5 {
		t.Errorf("temperature = %v", body["temperature"])
	}
	if body["max_tokens"] != float64(128) {
		t.Errorf("max_tokens = %v", body["max_tokens"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v", body["messages"])
	}
	// The usage report is requested in the documented way.
	so, _ := body["stream_options"].(map[string]any)
	if so == nil || so["include_usage"] != true {
		t.Errorf("stream_options = %v, want include_usage", body["stream_options"])
	}
	// The key goes in the header, never the body.
	if strings.Contains(fs.lastBody(), "sk-test") {
		t.Error("the API key was serialised into the request body")
	}
	fs.mu.Lock()
	auth := fs.auth[0]
	fs.mu.Unlock()
	if auth != "Bearer sk-test-0123456789abcdef" {
		t.Errorf("Authorization = %q", auth)
	}
}

// TestChatOmitsMaxTokensWhenZero: a zero cap would ask for an empty reply, so it
// must be left out of the body entirely rather than sent as 0.
func TestChatOmitsMaxTokensWhenZero(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		serveSSE(w, "data: [DONE]\n\n", 1024)
	})
	s := testSettings(fs.URL)
	s.MaxTokens = 0
	if _, err := fastClient().Chat(context.Background(), s, nil, Handlers{}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if strings.Contains(fs.lastBody(), "max_tokens") {
		t.Errorf("max_tokens was sent as zero: %s", fs.lastBody())
	}
}

// TestChatDropsStreamOptionsWhenRejected covers the self-healing path for a
// gateway that does not know the field.
func TestChatDropsStreamOptionsWhenRejected(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		if attempt == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"Unrecognized request argument supplied: stream_options"}}`)
			return
		}
		serveSSE(w, sseChunk("好")+"data: [DONE]\n\n", 1024)
	})

	res, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Text != "好" {
		t.Errorf("Text = %q, want the reply from the retry", res.Text)
	}
	if fs.count() != 2 {
		t.Fatalf("server saw %d requests, want 2", fs.count())
	}
	if strings.Contains(fs.lastBody(), "stream_options") {
		t.Error("the retry still sent stream_options")
	}
}

// TestChatRemembersRejectedStreamOptionsForTheSameClient checks the lesson is
// kept on the client, which is what the app holds for its lifetime: the user
// should pay for that discovery once, not on every message.
func TestChatRemembersRejectedStreamOptionsForTheSameClient(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		if attempt == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"unknown field stream_options"}}`)
			return
		}
		serveSSE(w, sseChunk("ok")+"data: [DONE]\n\n", 1024)
	})
	c := fastClient()
	if _, err := c.Chat(context.Background(), testSettings(fs.URL), nil, Handlers{}); err != nil {
		t.Fatalf("first Chat: %v", err)
	}
	if _, err := c.Chat(context.Background(), testSettings(fs.URL), nil, Handlers{}); err != nil {
		t.Fatalf("second Chat: %v", err)
	}
	fs.mu.Lock()
	bodies := append([]string(nil), fs.bodies...)
	fs.mu.Unlock()
	if len(bodies) != 3 {
		t.Fatalf("server saw %d requests, want 3 (2 on the first call, 1 on the second)", len(bodies))
	}
	if strings.Contains(bodies[2], "stream_options") {
		t.Error("the third request still carried the rejected field")
	}
}

// --- reasoning separation --------------------------------------------------

func TestChatKeepsReasoningOutOfTheReply(t *testing.T) {
	stream := reasonChunk("让我想想…") + sseChunk("答案") + finishChunk("stop", nil) + "data: [DONE]\n\n"
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) { serveSSE(w, stream, 1024) })

	var text, reasoning []string
	res, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{
		OnText:      func(s string) { text = append(text, s) },
		OnReasoning: func(s string) { reasoning = append(reasoning, s) },
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Text != "答案" {
		t.Errorf("Text = %q, want only the visible reply", res.Text)
	}
	if res.Reasoning != "让我想想…" {
		t.Errorf("Reasoning = %q", res.Reasoning)
	}
	if strings.Contains(res.Text, "让我想想") {
		t.Error("the chain of thought leaked into the reply")
	}
	if strings.Join(reasoning, "") != "让我想想…" {
		t.Errorf("reasoning was not streamed: %v", reasoning)
	}
	if strings.Join(text, "") != "答案" {
		t.Errorf("text stream = %v", text)
	}
}

// --- failures --------------------------------------------------------------

func TestChatErrorStatuses(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		wantKind ErrorKind
		contains string
		retry    bool
	}{
		{401, `{"error":{"message":"Authentication Fails"}}`, KindAuth, "API Key", false},
		{403, `{"error":{"message":"Forbidden"}}`, KindAuth, "API Key", false},
		{402, `{"error":{"message":"Insufficient Balance"}}`, KindRateLimit, "额度", true},
		{404, `{"error":{"message":"Model Not Exist"}}`, KindModel, "不存在", false},
		{429, `{"error":{"message":"Rate limit reached"}}`, KindRateLimit, "频繁", true},
		{400, `{"error":{"message":"temperature must be <= 2"}}`, KindRequest, "temperature", false},
		{500, `internal error`, KindServer, "服务端错误", true},
		{503, ``, KindServer, "服务端错误", true},
		{418, `I am a teapot`, KindOther, "418", false},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%d", c.status), func(t *testing.T) {
			fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
				w.WriteHeader(c.status)
				fmt.Fprint(w, c.body)
			})
			_, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{})
			if err == nil {
				t.Fatal("expected an error")
			}
			var ce *ChatError
			if !errors.As(err, &ce) {
				t.Fatalf("error is %T, want *ChatError", err)
			}
			if ce.Kind != c.wantKind {
				t.Errorf("kind = %q, want %q", ce.Kind, c.wantKind)
			}
			if !strings.Contains(ce.Message, c.contains) {
				t.Errorf("message %q does not mention %q", ce.Message, c.contains)
			}
			if ce.Retryable != c.retry {
				t.Errorf("retryable = %v, want %v", ce.Retryable, c.retry)
			}
			if ce.Status != c.status {
				t.Errorf("status = %d, want %d", ce.Status, c.status)
			}
		})
	}
}

// TestChatErrorMessagesNeverLeakTheKey is the security half of error handling: a
// provider that echoes the request must not get the key into the transcript or
// the debug log.
func TestChatErrorMessagesNeverLeakTheKey(t *testing.T) {
	const key = "sk-verysecret-0123456789abcdef"
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"message":"invalid api key %s supplied in header"}}`, key)
	})

	s := testSettings(fs.URL)
	s.APIKey = key
	_, err := fastClient().Chat(context.Background(), s, nil, Handlers{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("the user-facing error contains the key: %q", err.Error())
	}
	var ce *ChatError
	if !errors.As(err, &ce) {
		t.Fatalf("error is %T", err)
	}
	if strings.Contains(ce.Detail, key) {
		t.Errorf("the error detail contains the key: %q", ce.Detail)
	}
	if !strings.Contains(ce.Detail, "invalid api key") {
		t.Errorf("detail = %q, want the provider's reason with the key masked", ce.Detail)
	}
	if ce.Err != nil && strings.Contains(ce.Err.Error(), key) {
		t.Errorf("the wrapped cause contains the key: %q", ce.Err)
	}
}

// TestChatRetriesServerErrorAndSucceeds is the whole point of the retry policy.
func TestChatRetriesServerErrorAndSucceeds(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		if attempt == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "boom")
			return
		}
		serveSSE(w, sseChunk("恢复了")+"data: [DONE]\n\n", 1024)
	})

	c := NewClient()
	// A short but non-zero backoff: this asserts the client actually waits
	// rather than hammering the server, without spending the real 500ms.
	c.RetryBackoff = []time.Duration{120 * time.Millisecond}
	start := time.Now()
	res, err := c.Chat(context.Background(), testSettings(fs.URL), nil, Handlers{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Text != "恢复了" {
		t.Errorf("Text = %q", res.Text)
	}
	if res.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", res.Attempts)
	}
	if waited := time.Since(start); waited < 100*time.Millisecond {
		t.Errorf("retried after only %v; the backoff was skipped", waited)
	}
}

// TestDefaultRetryBackoffIsNotInstant pins the shipped delays. A retry that fires
// immediately turns a rate limit into a tighter rate limit.
func TestDefaultRetryBackoffIsNotInstant(t *testing.T) {
	if len(DefaultRetryBackoff) == 0 {
		t.Fatal("no retries are configured")
	}
	for i, d := range DefaultRetryBackoff {
		if d < 100*time.Millisecond {
			t.Errorf("backoff %d is %v, which is too eager to help against a rate limit", i, d)
		}
		if d > 10*time.Second {
			t.Errorf("backoff %d is %v, which would look like a hang", i, d)
		}
	}
	if len(DefaultRetryBackoff) > 3 {
		t.Errorf("%d retries is more than a user will wait through", len(DefaultRetryBackoff))
	}
}

// TestChatGivesUpAfterTheConfiguredRetries checks the loop terminates.
func TestChatGivesUpAfterTheConfiguredRetries(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "always broken")
	})
	c := fastClient()
	_, err := c.Chat(context.Background(), testSettings(fs.URL), nil, Handlers{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if want := len(c.RetryBackoff) + 1; fs.count() != want {
		t.Errorf("server saw %d requests, want %d (initial plus retries)", fs.count(), want)
	}
}

// TestChatDoesNotRetryAfterTextArrived guards against restarting a reply the
// user has already seen half of.
func TestChatDoesNotRetryAfterTextArrived(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		// One delta, then a hard failure mid-stream.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseChunk("半句话"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Abort the connection so the client sees a transport error.
		panic(http.ErrAbortHandler)
	})

	res, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if fs.count() != 1 {
		t.Errorf("server saw %d requests; a retry after text was shown would duplicate the reply", fs.count())
	}
	if res.Text != "半句话" {
		t.Errorf("partial Text = %q, want the text that was already streamed", res.Text)
	}
}

func TestChatDoesNotRetryAuthErrors(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"bad key"}}`)
	})
	_, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if fs.count() != 1 {
		t.Errorf("server saw %d requests; a rejected key must not be retried", fs.count())
	}
}

func TestChatTimeout(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseChunk("开始"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Never finish. The client's deadline must end this.
		<-r.Context().Done()
	})

	s := testSettings(fs.URL)
	s.Timeout = 400 * time.Millisecond
	res, err := fastClient().Chat(context.Background(), s, nil, Handlers{})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	var ce *ChatError
	if !errors.As(err, &ce) || ce.Kind != KindTimeout {
		t.Fatalf("error = %v (kind %v), want a timeout", err, ce)
	}
	if res.Text != "开始" {
		t.Errorf("partial Text = %q, want what arrived before the timeout", res.Text)
	}
}

func TestChatCancellation(t *testing.T) {
	release := make(chan struct{})
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseChunk("一"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	s := testSettings(fs.URL)
	s.Timeout = 10 * time.Second
	res, err := fastClient().Chat(ctx, s, nil, Handlers{})
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	var ce *ChatError
	if !errors.As(err, &ce) {
		t.Fatalf("error is %T", err)
	}
	if ce.Kind != KindCanceled {
		t.Errorf("kind = %q, want %q", ce.Kind, KindCanceled)
	}
	if ce.Message != "已取消。" {
		t.Errorf("message = %q", ce.Message)
	}
	if res.Text != "一" {
		t.Errorf("partial reply lost on cancel: %q", res.Text)
	}
}

// clientWithTransport returns a client whose dial always fails with err, so a
// transport-failure test does not depend on the machine's DNS resolver, its
// routing table or how long either takes to give up.
func clientWithTransport(err error) *Client {
	c := fastClient()
	c.HTTP = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, err }),
	}
	return c
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChatNetworkError(t *testing.T) {
	// A connection refusal, synthesised rather than dialled, so the test is not
	// at the mercy of what happens to be listening on the machine.
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	s := testSettings("http://127.0.0.1:1/v1")
	_, err := clientWithTransport(refused).Chat(context.Background(), s, nil, Handlers{})
	if err == nil {
		t.Fatal("expected a network error")
	}
	var ce *ChatError
	if !errors.As(err, &ce) || ce.Kind != KindNetwork {
		t.Fatalf("error = %v (kind %v), want a network error", err, ce)
	}
	if !strings.Contains(ce.Message, "网络") {
		t.Errorf("message %q should explain the network problem", ce.Message)
	}
}

// TestChatBadHostName pins the DNS case: the message has to name the host that
// could not be resolved, because "网络不可达" alone gives the user nothing to fix.
func TestChatBadHostName(t *testing.T) {
	dnsErr := &net.DNSError{Err: "no such host", Name: "this-host-does-not-exist.invalid", IsNotFound: true}
	s := testSettings("http://this-host-does-not-exist.invalid/v1")
	_, err := clientWithTransport(dnsErr).Chat(context.Background(), s, nil, Handlers{})
	if err == nil {
		t.Fatal("expected a DNS error")
	}
	var ce *ChatError
	if !errors.As(err, &ce) || ce.Kind != KindNetwork {
		t.Fatalf("error = %v (kind %v)", err, ce)
	}
	if !strings.Contains(ce.Message, "this-host-does-not-exist.invalid") {
		t.Errorf("message %q should name the host that failed", ce.Message)
	}
	// And the unrecognised-host wording, not the generic one.
	if !strings.Contains(ce.Message, "解析") {
		t.Errorf("message %q should say the address could not be resolved", ce.Message)
	}
}

func TestChatUnconfigured(t *testing.T) {
	s := Settings{}
	_, err := fastClient().Chat(context.Background(), s, nil, Handlers{})
	if err == nil {
		t.Fatal("expected a config error")
	}
	var ce *ChatError
	if !errors.As(err, &ce) || ce.Kind != KindConfig {
		t.Fatalf("error = %v (kind %v)", err, ce)
	}
}

// TestChatErrorInside200Response covers a gateway that answers 200 and puts the
// failure in the body. A client that only looks at the status code shows an
// empty reply instead of the reason.
func TestChatErrorInside200Response(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		serveSSE(w, `data: {"error":{"message":"model overloaded","type":"server_error"}}`+"\n\n", 1024)
	})
	_, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{})
	if err == nil {
		t.Fatal("expected an error")
	}
	var ce *ChatError
	if !errors.As(err, &ce) {
		t.Fatalf("error is %T", err)
	}
	if !strings.Contains(ce.Message, "model overloaded") {
		t.Errorf("message = %q, want the server's reason", ce.Message)
	}
}

func TestChatGarbageFrame(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		serveSSE(w, "data: this is not json\n\n", 1024)
	})
	_, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{})
	if err == nil {
		t.Fatal("expected a protocol error")
	}
	var ce *ChatError
	if !errors.As(err, &ce) || ce.Kind != KindProtocol {
		t.Fatalf("error = %v (kind %v), want a protocol error", err, ce)
	}
	if !strings.Contains(ce.Message, "OpenAI 兼容") {
		t.Errorf("message %q should hint at the likely cause", ce.Message)
	}
}

// TestChatNonStreamingFallback covers a server that ignores `stream: true`.
func TestChatNonStreamingFallback(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"fallback-model","choices":[{"message":{"content":"整段回答"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":4}}`)
	})
	var streamed []string
	res, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{
		OnText: func(s string) { streamed = append(streamed, s) },
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Text != "整段回答" {
		t.Errorf("Text = %q, want the whole reply", res.Text)
	}
	if res.Model != "fallback-model" {
		t.Errorf("Model = %q", res.Model)
	}
	if res.Usage == nil || res.Usage.PromptTokens != 5 {
		t.Errorf("usage = %+v", res.Usage)
	}
	if res.FinishReason != "stop" {
		t.Errorf("FinishReason = %q", res.FinishReason)
	}
	// The fallback reply must still reach the UI, not just the result.
	if strings.Join(streamed, "") != "整段回答" {
		t.Errorf("fallback text was not streamed to the UI: %v", streamed)
	}
}

// TestChatEmptyReplyIsNotAnError: a model that legitimately produces nothing must
// not look like a failure.
func TestChatEmptyReplyIsNotAnError(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		serveSSE(w, "data: [DONE]\n\n", 1024)
	})
	res, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Text != "" {
		t.Errorf("Text = %q, want empty", res.Text)
	}
}

// TestChatWithoutTrailingBlankLine covers a stream that ends immediately after
// its last payload.
func TestChatWithoutTrailingBlankLine(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		serveSSE(w, strings.TrimSuffix(sseChunk("最后一句"), "\n"), 1024)
	})
	res, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Text != "最后一句" {
		t.Errorf("Text = %q, the final frame was dropped", res.Text)
	}
}

// TestChatKeepAliveComments: many gateways send `:` keep-alives while a slow
// model thinks. They must not become empty deltas.
func TestChatKeepAliveComments(t *testing.T) {
	stream := ": ping\n\n" + ": ping\n\n" + sseChunk("想好了") + ": keep-alive\n\n" + "data: [DONE]\n\n"
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) { serveSSE(w, stream, 1024) })

	var pieces []string
	res, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{
		OnText: func(s string) { pieces = append(pieces, s) },
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Text != "想好了" {
		t.Errorf("Text = %q", res.Text)
	}
	for _, p := range pieces {
		if p == "" {
			t.Error("a keep-alive produced an empty delta event")
		}
	}
}

// TestChatFlushesShortReplies is a regression test for a real defect: the
// terminal flush used to be subject to the batching thresholds, so a reply
// shorter than 24 characters stayed in the buffer and reached the result but
// never the UI.
func TestChatFlushesShortReplies(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		serveSSE(w, sseChunk("喵")+"data: [DONE]\n\n", 1024)
	})
	var streamed []string
	res, err := fastClient().Chat(context.Background(), testSettings(fs.URL), nil, Handlers{
		OnText: func(s string) { streamed = append(streamed, s) },
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Text != "喵" {
		t.Fatalf("Text = %q", res.Text)
	}
	if strings.Join(streamed, "") != "喵" {
		t.Fatalf("a one-character reply never reached the UI: streamed %v", streamed)
	}
}

// --- coalescer -------------------------------------------------------------

func TestCoalescerFlushesOnSize(t *testing.T) {
	var got []string
	c := NewCoalescer(func(s string) { got = append(got, s) })
	c.SetThresholds(10, time.Hour) // only the size threshold can fire

	c.Add("12345")
	if len(got) != 0 {
		t.Fatalf("flushed too early: %v", got)
	}
	c.Add("67890")
	if len(got) != 1 || got[0] != "1234567890" {
		t.Fatalf("got %v, want one batch of 10", got)
	}
	if c.Pending() {
		t.Error("Pending is true after a flush")
	}
}

func TestCoalescerFlushIsExplicitAndIdempotent(t *testing.T) {
	var got []string
	c := NewCoalescer(func(s string) { got = append(got, s) })
	c.SetThresholds(1000, time.Hour)

	c.Add("尾")
	if !c.Flush() {
		t.Fatal("Flush reported nothing to flush")
	}
	if len(got) != 1 || got[0] != "尾" {
		t.Fatalf("got %v", got)
	}
	if c.Flush() {
		t.Error("a second Flush reported work it did not do")
	}
	if len(got) != 1 {
		t.Errorf("a second Flush emitted again: %v", got)
	}
}

func TestCoalescerIgnoresEmptyAndKeepsOrder(t *testing.T) {
	var got []string
	c := NewCoalescer(func(s string) { got = append(got, s) })
	c.SetThresholds(1000, time.Hour)
	if c.Add("") {
		t.Error("adding an empty string reported a flush")
	}
	c.Add("a")
	c.Add("b")
	c.Add("c")
	c.Flush()
	if len(got) != 1 || got[0] != "abc" {
		t.Fatalf("got %v, want one ordered batch", got)
	}
}

func TestCoalescerFlushesOnTime(t *testing.T) {
	var got []string
	c := NewCoalescer(func(s string) { got = append(got, s) })
	c.SetThresholds(10000, 10*time.Millisecond)

	c.Add("慢")
	time.Sleep(20 * time.Millisecond)
	// The next Add sees that the interval has elapsed and flushes.
	c.Add("吞吞")
	if len(got) == 0 {
		t.Fatal("the time threshold never fired")
	}
	if strings.Join(got, "") != "慢吞吞" {
		t.Fatalf("got %v", got)
	}
}

// TestCoalescerBatchesManyDeltas is the property the whole type exists for: a
// token-per-frame stream must not become one event per token.
//
// The batching policy is tightened to a 5ms window so the test is not a
// wall-clock race: the tight loop below finishes in well under 5ms, so every add
// lands in the same batch no matter how loaded the machine is.
func TestCoalescerBatchesManyDeltas(t *testing.T) {
	batches := 0
	var total strings.Builder
	c := NewCoalescer(func(s string) {
		batches++
		total.WriteString(s)
	})
	c.SetThresholds(10000, 5*time.Millisecond)
	for i := 0; i < 500; i++ {
		c.Add("x")
	}
	c.Flush()
	if total.Len() != 500 {
		t.Fatalf("lost text: got %d characters, want 500", total.Len())
	}
	if batches == 0 {
		t.Fatal("nothing was flushed at all")
	}
	if batches > 100 {
		t.Errorf("%d bridge crossings for 500 deltas: the batching is not working", batches)
	}
}

// TestCoalescerDefaultThresholdBatches asserts the shipped policy rather than a
// test-tuned one: with a 24-character threshold, a run of single characters must
// be grouped into batches of 24.
func TestCoalescerDefaultThresholdBatches(t *testing.T) {
	var sizes []int
	c := NewCoalescer(func(s string) { sizes = append(sizes, len(s)) })
	for i := 0; i < 72; i++ {
		c.Add("x")
	}
	c.Flush()
	if len(sizes) != 3 {
		t.Fatalf("got %d batches (%v), want 3 for 72 characters at a 24-character threshold", len(sizes), sizes)
	}
	for _, n := range sizes {
		if n != 24 {
			t.Errorf("batch of %d characters, want 24", n)
		}
	}
}

// TestCoalescerConcurrentAddAndFlush exercises the lock: the network goroutine
// appends while the request owner flushes.
func TestCoalescerConcurrentAddAndFlush(t *testing.T) {
	var mu sync.Mutex
	var total strings.Builder
	c := NewCoalescer(func(s string) {
		mu.Lock()
		total.WriteString(s)
		mu.Unlock()
	})
	c.SetThresholds(8, time.Millisecond)

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				c.Add("ab")
				c.Flush()
			}
		}()
	}
	wg.Wait()
	c.Flush()

	mu.Lock()
	defer mu.Unlock()
	if total.Len() != 4*200*2 {
		t.Errorf("got %d characters, want %d", total.Len(), 4*200*2)
	}
}
