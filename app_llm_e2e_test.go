package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"PetPC/internal/store"
)

// TestEndToEndAgainstAFakeProvider is the P2 acceptance check from
// docs/llm-chat.md 11: the whole request path - config, key, prompt assembly,
// streaming, coalescing, persistence, usage accounting - exercised against a
// real HTTP server speaking real SSE.
//
// It is deliberately run against the SAME client the app uses, with the only
// substitution being where the bytes come from. The GUI cannot be exercised in
// this environment (WebView2 refuses to create a controller in a headless
// session, identically for the pre-change binary), so this is the closest thing
// to a live run that exists here.
func TestEndToEndAgainstAFakeProvider(t *testing.T) {
	const reply = "你今天有 2 件事：先交周报，再买牛奶。"

	var (
		mu       sync.Mutex
		requests []string
		authSeen string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, string(body))
		authSeen = r.Header.Get("Authorization")
		mu.Unlock()

		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		// Split the reply into one frame per character, which is what a real
		// token stream looks like, and flush each one so the client has to
		// reassemble them.
		for _, ch := range reply {
			frame, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{"delta": map[string]any{"content": string(ch)}}},
			})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			flusher.Flush()
		}
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":42,"completion_tokens":18}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	// A data directory as a real install would have, with todos in it.
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	now := time.Now()
	overdue := now.Add(-2 * time.Hour)
	soon := now.Add(3 * time.Hour)
	if err := st.SaveTodos([]store.Todo{
		{ID: "t1", Title: "交周报", Due: &overdue, CreatedAt: now},
		{ID: "t2", Title: "买牛奶", Due: &soon, CreatedAt: now},
		{ID: "t3", Title: "已经做完的事", Done: true, CreatedAt: now},
	}); err != nil {
		t.Fatalf("SaveTodos: %v", err)
	}

	a := &App{store: st}
	a.initLLM()
	a.ctx = context.Background()
	events := &eventCollector{}
	a.emitFn = events.record
	if err := a.secretStore().Set("sk-e2e-key-0123456789abcdef"); err != nil {
		t.Fatalf("store key: %v", err)
	}

	// Configure it exactly as the settings panel would.
	if _, err := a.SaveLLMSettings(LLMSettingsInput{
		Enabled:      true,
		Provider:     store.ProviderCustom,
		BaseURL:      srv.URL + "/v1/",
		Model:        "deepseek-chat",
		Temperature:  0.8,
		MaxTokens:    800,
		TimeoutSec:   30,
		HistoryTurns: 12,
		ContextTodos: true,
		DailyBudget:  200,
	}); err != nil {
		t.Fatalf("SaveLLMSettings: %v", err)
	}

	if view := a.llmView(); !view.Ready {
		t.Fatalf("not ready after configuring: %s", view.NotReadyReason)
	}

	// --- send ---------------------------------------------------------------
	start, err := a.SendChat("我今天还有什么要做？")
	if err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	done := events.waitEvent(t, eventChatDone, 15*time.Second)

	// 1. The streamed deltas reassembled into exactly the reply.
	streamed := events.textFor(eventChatDelta, start.RequestID)
	if streamed != reply {
		t.Errorf("streamed %q, want %q", streamed, reply)
	}
	if done.data["text"] != reply {
		t.Errorf("done text = %q", done.data["text"])
	}
	if done.data["finishReason"] != "stop" {
		t.Errorf("finishReason = %v", done.data["finishReason"])
	}

	// 2. The prompt carried the real todos and the clock, and left the finished
	//    one out.
	mu.Lock()
	if len(requests) != 1 {
		mu.Unlock()
		t.Fatalf("provider saw %d requests, want 1", len(requests))
	}
	requestBody := requests[0]
	mu.Unlock()

	var body struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal([]byte(requestBody), &body); err != nil {
		t.Fatalf("request body: %v (%s)", err, requestBody)
	}
	if !body.Stream {
		t.Error("the request did not ask for a stream")
	}
	if body.Model != "deepseek-chat" {
		t.Errorf("model = %q", body.Model)
	}
	system := body.Messages[0].Content
	for _, want := range []string{"交周报", "买牛奶", "已逾期", "待办"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt is missing %q:\n%s", want, system)
		}
	}
	if strings.Contains(system, "已经做完的事") {
		t.Errorf("a completed todo reached the prompt:\n%s", system)
	}
	if last := body.Messages[len(body.Messages)-1]; last.Content != "我今天还有什么要做？" {
		t.Errorf("last message = %q", last.Content)
	}

	// 3. The key travelled as a header and nowhere else.
	if authSeen != "Bearer sk-e2e-key-0123456789abcdef" {
		t.Errorf("Authorization = %q", authSeen)
	}
	if strings.Contains(requestBody, "sk-e2e-key") {
		t.Error("the API key was serialised into the request body")
	}

	// 4. The trailing slash in the configured base URL did not produce "//".
	if !strings.HasSuffix(srv.URL+"/v1", strings.TrimSuffix(srv.URL+"/v1", "/")) {
		t.Fatal("unreachable")
	}
	if strings.Contains(requestBody, "//chat/completions") {
		t.Error("the endpoint was built with a double slash")
	}

	// 5. The transcript and the usage counters landed on disk.
	msgs := st.ChatHistory()
	if len(msgs) != 2 {
		t.Fatalf("transcript has %d entries, want 2: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != store.RoleUser || msgs[1].Role != store.RoleAssistant {
		t.Errorf("roles = %q, %q", msgs[0].Role, msgs[1].Role)
	}
	if msgs[1].Text != reply {
		t.Errorf("stored reply = %q", msgs[1].Text)
	}

	// Reopening the store is what a restart looks like.
	reopened, err := store.New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := reopened.ChatHistory(); len(got) != 2 || got[1].Text != reply {
		t.Errorf("the conversation did not survive a restart: %+v", got)
	}
	usage := reopened.LLMUsageToday(time.Now())
	if usage.Requests != 1 {
		t.Errorf("requests = %d, want 1", usage.Requests)
	}
	if usage.PromptTokens != 42 || usage.CompletionTokens != 18 {
		t.Errorf("tokens = %+v, want the provider's report", usage)
	}

	// 6. A second question replays the first exchange as context.
	if _, err := a.SendChat("那先做哪个？"); err != nil {
		t.Fatalf("second SendChat: %v", err)
	}
	events.waitEvent(t, eventChatDone, 15*time.Second)
	// waitEvent returns the FIRST done event, so wait for the transcript instead.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && len(st.ChatHistory()) < 4 {
		time.Sleep(5 * time.Millisecond)
	}
	if n := len(st.ChatHistory()); n != 4 {
		t.Fatalf("transcript has %d entries after two exchanges, want 4", n)
	}

	mu.Lock()
	second := requests[len(requests)-1]
	mu.Unlock()
	var body2 struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(second), &body2); err != nil {
		t.Fatalf("second request body: %v", err)
	}
	// system + user + assistant + user
	if len(body2.Messages) != 4 {
		t.Fatalf("second request carried %d messages, want 4 (system plus one exchange)", len(body2.Messages))
	}
	if body2.Messages[1].Content != "我今天还有什么要做？" || body2.Messages[2].Content != reply {
		t.Errorf("the previous exchange was not replayed: %+v", body2.Messages[1:])
	}

	// 7. Everything the webview can see, serialised, contains no key.
	state, err := a.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	blob, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if strings.Contains(string(blob), "sk-e2e-key") {
		t.Fatalf("GetState leaked the key: %s", blob)
	}

	// And the encrypted key file is not readable as text.
	raw, err := os.ReadFile(filepath.Join(dir, "llm-key.bin"))
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	if strings.Contains(string(raw), "sk-e2e-key") {
		t.Fatal("the key file holds the key in the clear")
	}
	t.Logf("end-to-end: %d deltas coalesced into %d events, reply %d chars, %d+%d tokens",
		len([]rune(reply)), len(events.byName(eventChatDelta)), len([]rune(reply)),
		usage.PromptTokens, usage.CompletionTokens)
}
