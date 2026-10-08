package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"PetPC/internal/llm"
	"PetPC/internal/store"
)

// --- helpers ---------------------------------------------------------------

// newLLMTestApp builds an App over a temporary data directory with the LLM
// runtime wired up.
//
// It differs from newTestApp (app_bubble_test.go) in calling initLLM, clearing
// the key environment variable (which would otherwise take precedence over the
// encrypted file and make the secret-store assertions pass for the wrong
// reason), and installing an event collector.
//
// The collector is not a convenience: Wails' runtime.EventsEmit rejects any
// context other than the one its lifecycle hooks supplied, and the chat path
// emits from a request goroutine. Without somewhere else for those events to go,
// none of the chat state machine could be tested outside a running window.
func newLLMTestApp(t *testing.T) *App {
	t.Helper()
	a, _ := newLLMTestAppWithEvents(t)
	return a
}

// newLLMTestAppWithEvents is newLLMTestApp plus the collector, for the tests
// that assert on what the frontend would have received.
func newLLMTestAppWithEvents(t *testing.T) (*App, *eventCollector) {
	t.Helper()
	os.Unsetenv("PETPC_LLM_API_KEY")

	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	a := &App{store: s}
	a.initLLM()
	// A context is still needed for the window calls these tests may reach.
	a.ctx = context.Background()
	collector := &eventCollector{}
	a.emitFn = collector.record
	return a, collector
}

// eventCollector records the events the app emits.
type eventCollector struct {
	mu   sync.Mutex
	seen []collectedEvent
}

type collectedEvent struct {
	name string
	data map[string]any
}

func (c *eventCollector) record(name string, data ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ev := collectedEvent{name: name}
	if len(data) > 0 {
		if m, ok := data[0].(map[string]any); ok {
			ev.data = m
		}
	}
	c.seen = append(c.seen, ev)
}

// names returns the event names recorded so far, in order.
func (c *eventCollector) names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.seen))
	for _, e := range c.seen {
		out = append(out, e.name)
	}
	return out
}

// textFor concatenates the "text" payloads of every delta for a request.
func (c *eventCollector) textFor(event, requestID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	for _, e := range c.seen {
		if e.name != event {
			continue
		}
		if id, _ := e.data["requestId"].(string); id != requestID {
			continue
		}
		if s, _ := e.data["text"].(string); s != "" {
			b.WriteString(s)
		}
	}
	return b.String()
}

// find returns the first recorded event with the given name.
func (c *eventCollector) find(name string) (collectedEvent, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.seen {
		if e.name == name {
			return e, true
		}
	}
	return collectedEvent{}, false
}

// has reports whether an event with this name has been recorded.
func (c *eventCollector) has(name string) bool {
	_, ok := c.find(name)
	return ok
}

// byName returns every recorded event with the given name, in order.
func (c *eventCollector) byName(name string) []collectedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []collectedEvent
	for _, e := range c.seen {
		if e.name == name {
			out = append(out, e)
		}
	}
	return out
}

// waitEvent blocks until an event with the given name arrives.
//
// Tests must wait on the EVENT rather than on the transcript: Go persists the
// reply before it emits the terminal event, so a test that waits for the
// transcript entry can observe a state where the write has landed but the event
// has not, and then fail looking for the event.
func (c *eventCollector) waitEvent(t *testing.T, name string, timeout time.Duration) collectedEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ev, ok := c.find(name); ok {
			return ev
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s (saw %v)", name, c.names())
	return collectedEvent{}
}

// enableLLM configures the app to talk to the given test server.
func enableLLM(t *testing.T, a *App, endpoint string) {
	t.Helper()
	if err := a.secretStore().Set("sk-test-key-0123456789abcdef"); err != nil {
		t.Fatalf("set key: %v", err)
	}
	if _, err := a.SaveLLMSettings(LLMSettingsInput{
		Enabled:      true,
		Provider:     store.ProviderCustom,
		BaseURL:      endpoint,
		Model:        "test-model",
		Temperature:  0.7,
		MaxTokens:    256,
		TimeoutSec:   10,
		SystemPrompt: "你是一只桌宠。",
		HistoryTurns: 6,
		ContextTodos: true,
		DailyBudget:  100,
	}); err != nil {
		t.Fatalf("SaveLLMSettings: %v", err)
	}
}

// sse builds one streaming frame.
func sse(content string) string {
	payload, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"content": content}}},
	})
	return "data: " + string(payload) + "\n\n"
}

// chatServer starts a scripted OpenAI-compatible endpoint.
func chatServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, attempt int)) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		handler(w, r, n)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// waitFor polls cond until it is true or the deadline passes.
//
// The chat path is asynchronous by design (SendChat returns before the request
// finishes), so an assertion has to wait for the terminal state rather than read
// it immediately.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// --- readiness -------------------------------------------------------------

func TestLLMViewNotReadyStates(t *testing.T) {
	a := newLLMTestApp(t)

	// 1. Switched off.
	view := a.llmView()
	if view.Ready {
		t.Error("the LLM reports itself ready while it is switched off")
	}
	if !strings.Contains(view.NotReadyReason, "未开启") {
		t.Errorf("reason = %q", view.NotReadyReason)
	}

	// 2. Switched on, no key.
	if _, err := a.SaveLLMSettings(LLMSettingsInput{
		Enabled: true, Provider: store.ProviderDeepSeek,
		BaseURL: store.DefaultLLMBaseURL, Model: store.DefaultLLMModel,
		Temperature: 0.8, MaxTokens: 800, TimeoutSec: 90, DailyBudget: 100,
	}); err != nil {
		t.Fatalf("SaveLLMSettings: %v", err)
	}
	view = a.llmView()
	if view.Ready {
		t.Error("ready without an API key")
	}
	if !strings.Contains(view.NotReadyReason, "API Key") {
		t.Errorf("reason = %q, want a missing-key explanation", view.NotReadyReason)
	}

	// 3. Key present: now it is ready.
	if err := a.secretStore().Set("sk-test-key-0123456789abcdef"); err != nil {
		t.Fatalf("set key: %v", err)
	}
	if view = a.llmView(); !view.Ready {
		t.Errorf("still not ready with a key: %s", view.NotReadyReason)
	}
	if view.NotReadyReason != "" {
		t.Errorf("a ready view carries a reason: %q", view.NotReadyReason)
	}
}

// TestLocalProviderNeedsNoKey: demanding a key for Ollama would send a user off
// to buy one for a server running on their own machine.
func TestLocalProviderNeedsNoKey(t *testing.T) {
	a := newLLMTestApp(t)
	if _, err := a.SaveLLMSettings(LLMSettingsInput{
		Enabled: true, Provider: "ollama",
		BaseURL: "http://127.0.0.1:11434/v1", Model: "qwen2.5:7b",
		Temperature: 0.8, MaxTokens: 800, TimeoutSec: 90, DailyBudget: 100,
	}); err != nil {
		t.Fatalf("SaveLLMSettings: %v", err)
	}
	view := a.llmView()
	if view.NeedsKey {
		t.Error("the Ollama preset is marked as needing a key")
	}
	if !view.HasKey {
		// No key file either, so this is the real "no key anywhere" case.
		if view.Ready != true {
			t.Errorf("a local endpoint should be ready without a key: %s", view.NotReadyReason)
		}
	}
}

func TestLLMViewMasksTheKeyAndNeverRevealsIt(t *testing.T) {
	a := newLLMTestApp(t)
	const key = "sk-abcdefghijklmnopqrstuvwxyz"
	if err := a.secretStore().Set(key); err != nil {
		t.Fatalf("set key: %v", err)
	}
	view := a.llmView()

	if !view.HasKey || view.KeySource != "file" {
		t.Errorf("hasKey=%v source=%q", view.HasKey, view.KeySource)
	}
	if view.KeyMask == key {
		t.Fatal("the view returned the key itself")
	}
	if !strings.Contains(view.KeyMask, "…") {
		t.Errorf("mask = %q, want an elided form", view.KeyMask)
	}

	// The decisive assertion: serialising the whole view - which is what happens
	// on every GetState - must not carry the key.
	blob, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), key) {
		t.Fatalf("the API key is in the serialised view: %s", blob)
	}
}

// TestStateNeverCarriesTheKey is the end-to-end version: GetState is exactly what
// the webview receives.
func TestStateNeverCarriesTheKey(t *testing.T) {
	a := newLLMTestApp(t)
	const key = "sk-ZZZZZZZZZZZZZZZZZZZZZZZZ"
	if err := a.secretStore().Set(key); err != nil {
		t.Fatalf("set key: %v", err)
	}
	state, err := a.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	blob, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if strings.Contains(string(blob), key) {
		t.Fatalf("GetState leaked the API key: %s", blob)
	}
	// And the config file on disk must be clean too. A first-run app has not
	// written one yet, so it is written here to make the check meaningful rather
	// than skipped.
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.LLMEnabled = true }); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(a.store.Dir(), "config.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), key) {
		t.Fatalf("config.json contains the API key: %s", raw)
	}
}

func TestSetAndClearLLMKey(t *testing.T) {
	a := newLLMTestApp(t)

	if _, err := a.SetLLMKey("sk-first-key-0123456789"); err != nil {
		t.Fatalf("SetLLMKey: %v", err)
	}
	if got := a.secretStore().Get(); got != "sk-first-key-0123456789" {
		t.Errorf("key = %q", got)
	}
	if view := a.llmView(); !view.HasKey {
		t.Error("the view does not see the key that was just stored")
	}

	if _, err := a.ClearLLMKey(); err != nil {
		t.Fatalf("ClearLLMKey: %v", err)
	}
	if view := a.llmView(); view.HasKey {
		t.Error("the view still reports a key after clearing")
	}
	// Clearing twice must be harmless.
	if _, err := a.ClearLLMKey(); err != nil {
		t.Fatalf("second ClearLLMKey: %v", err)
	}
}

// TestEnvKeyCannotBeClearedFromTheUI documents the one case the panel has to
// explain rather than silently accept.
func TestEnvKeyCannotBeClearedFromTheUI(t *testing.T) {
	a := newLLMTestApp(t)
	t.Setenv("PETPC_LLM_API_KEY", "sk-from-environment-0123456")

	view := a.llmView()
	if view.KeySource != "env" {
		t.Fatalf("source = %q, want env", view.KeySource)
	}
	if _, err := a.ClearLLMKey(); err != nil {
		t.Fatalf("ClearLLMKey: %v", err)
	}
	if _, err := a.ClearLLMKey(); err != nil {
		t.Fatalf("second ClearLLMKey: %v", err)
	}
	// The key is still there, because it is not ours to delete.
	if view := a.llmView(); !view.HasKey || view.KeySource != "env" {
		t.Errorf("after clearing: hasKey=%v source=%q", view.HasKey, view.KeySource)
	}
}

// --- sending ---------------------------------------------------------------

func TestSendChatStreamsAndPersists(t *testing.T) {
	var received []string
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		received = append(received, string(body[:n]))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, part := range []string{"你", "好", "呀"} {
			fmt.Fprint(w, sse(part))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	})

	a, events := newLLMTestAppWithEvents(t)
	enableLLM(t, a, srv.URL)

	start, err := a.SendChat("在吗")
	if err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	if start.RequestID == "" {
		t.Fatal("SendChat returned no request id")
	}
	// SendChat must return BEFORE the request finishes: a Wails binding call runs
	// on the bridge's thread, so doing the HTTP work inline would freeze the pet.
	if busy := a.llmView().Busy; !busy {
		t.Error("SendChat returned without a request in flight; the call is not asynchronous")
	}

	waitFor(t, 5*time.Second, "the reply to be stored", func() bool {
		msgs := a.store.ChatHistory()
		return len(msgs) == 2 && msgs[1].Role == store.RoleAssistant
	})

	// The streamed deltas must add up to exactly the stored reply.
	done := events.waitEvent(t, eventChatDone, 5*time.Second)
	streamed := events.textFor(eventChatDelta, start.RequestID)
	if streamed != "你好呀" {
		t.Errorf("streamed %q, want 你好呀", streamed)
	}
	if done.data["text"] != "你好呀" {
		t.Errorf("done event text = %v", done.data["text"])
	}
	if done.data["finishReason"] != "stop" {
		t.Errorf("finishReason = %v", done.data["finishReason"])
	}

	msgs := a.store.ChatHistory()
	if msgs[0].Role != store.RoleUser || msgs[0].Text != "在吗" {
		t.Errorf("the question was not stored first: %+v", msgs[0])
	}
	if msgs[1].Text != "你好呀" {
		t.Errorf("reply = %q, want 你好呀", msgs[1].Text)
	}
	if msgs[1].Error {
		t.Error("a successful reply was marked as an error")
	}

	// The request must have carried the persona and the world snapshot.
	if len(received) == 0 {
		t.Fatal("the server saw no request")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(received[0]), &body); err != nil {
		t.Fatalf("request body: %v", err)
	}
	msgsField, _ := body["messages"].([]any)
	if len(msgsField) < 2 {
		t.Fatalf("messages = %v", body["messages"])
	}
	first, _ := msgsField[0].(map[string]any)
	if first["role"] != "system" {
		t.Errorf("the first message is %v, want the system prompt", first["role"])
	}
	if !strings.Contains(fmt.Sprint(first["content"]), "桌宠") {
		t.Error("the persona is missing from the system prompt")
	}
	last, _ := msgsField[len(msgsField)-1].(map[string]any)
	if last["content"] != "在吗" {
		t.Errorf("last message = %v, want the user's question", last["content"])
	}
}

func TestSendChatRefusesWhenNotReady(t *testing.T) {
	a := newLLMTestApp(t)

	// Switched off.
	if _, err := a.SendChat("你好"); err == nil {
		t.Fatal("SendChat succeeded with the feature switched off")
	}

	// Switched on but with no key.
	if _, err := a.SaveLLMSettings(LLMSettingsInput{
		Enabled: true, Provider: store.ProviderDeepSeek,
		BaseURL: store.DefaultLLMBaseURL, Model: store.DefaultLLMModel,
		Temperature: 0.8, MaxTokens: 800, TimeoutSec: 90, DailyBudget: 100,
	}); err != nil {
		t.Fatalf("SaveLLMSettings: %v", err)
	}
	_, err := a.SendChat("你好")
	if err == nil {
		t.Fatal("SendChat succeeded without a key")
	}
	if !strings.Contains(err.Error(), "API Key") {
		t.Errorf("error = %q, want it to name the missing key", err)
	}
	// A refused send must not have written anything into the transcript.
	if n := len(a.store.ChatHistory()); n != 0 {
		t.Errorf("a refused message was persisted anyway (%d entries)", n)
	}
}

func TestSendChatRejectsEmptyInput(t *testing.T) {
	a := newLLMTestApp(t)
	enableLLM(t, a, "http://127.0.0.1:1/v1")
	for _, empty := range []string{"", "   ", "\n\t "} {
		if _, err := a.SendChat(empty); err == nil {
			t.Errorf("SendChat(%q) was accepted", empty)
		}
	}
	if n := len(a.store.ChatHistory()); n != 0 {
		t.Errorf("an empty message was persisted (%d entries)", n)
	}
}

func TestSendChatRecordsFailuresInTheTranscript(t *testing.T) {
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"bad key"}}`)
	})
	a, events := newLLMTestAppWithEvents(t)
	enableLLM(t, a, srv.URL)

	if _, err := a.SendChat("你好"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	waitFor(t, 5*time.Second, "the error note", func() bool {
		msgs := a.store.ChatHistory()
		return len(msgs) == 2 && msgs[1].Error
	})

	// The frontend is told what went wrong, with a kind it can act on.
	ev := events.waitEvent(t, eventChatError, 5*time.Second)
	if ev.data["kind"] != "auth" {
		t.Errorf("kind = %v, want auth", ev.data["kind"])
	}
	if msg, _ := ev.data["message"].(string); !strings.Contains(msg, "API Key") {
		t.Errorf("message = %v, want a readable explanation", ev.data["message"])
	}

	msgs := a.store.ChatHistory()
	if !msgs[1].Error {
		t.Error("the failure was not marked as an error")
	}
	if !strings.Contains(msgs[1].Text, "API Key") {
		t.Errorf("error note = %q, want a readable explanation", msgs[1].Text)
	}
	// Errors must not be replayed to the model as if it had said them.
	for _, m := range a.store.Replay(10) {
		if m.Text == msgs[1].Text {
			t.Error("the error note would be replayed as the pet's own words")
		}
	}
}

// TestSendChatCancelsThePreviousRequest pins the "先取消再发" decision: a second
// question must not interleave with the answer to the first.
//
// The second request is what is asserted on, so the guard is that only one
// server request is ever in flight at a time and the second one produces the
// transcript entry - not that the first one's goroutine has been reaped, which
// the transport decides.
func TestSendChatCancelsThePreviousRequest(t *testing.T) {
	var mu sync.Mutex
	live := 0
	maxLive := 0
	release := make(chan struct{})

	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		mu.Lock()
		live++
		if live > maxLive {
			maxLive = live
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			live--
			mu.Unlock()
		}()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if attempt == 1 {
			fmt.Fprint(w, sse("第一"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Hold the first request open until the test lets go, so the second
			// one genuinely overlaps it.
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		fmt.Fprint(w, sse("第二"), "data: [DONE]\n\n")
	})
	defer close(release)

	a := newLLMTestApp(t)
	enableLLM(t, a, srv.URL)

	if _, err := a.SendChat("第一问"); err != nil {
		t.Fatalf("first SendChat: %v", err)
	}
	// Wait until the first request is actually in flight.
	waitFor(t, 3*time.Second, "the first request to start", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return live > 0
	})

	if _, err := a.SendChat("第二问"); err != nil {
		t.Fatalf("second SendChat: %v", err)
	}
	waitFor(t, 5*time.Second, "the second reply", func() bool {
		for _, m := range a.store.ChatHistory() {
			if m.Text == "第二" {
				return true
			}
		}
		return false
	})

	// The cancelled first request must not have been retried, and its partial
	// text must not have been appended to the second reply's entry.
	for _, m := range a.store.ChatHistory() {
		if m.Role == store.RoleAssistant && strings.Contains(m.Text, "第一") {
			t.Errorf("the superseded reply was stored: %+v", m)
		}
	}
}

func TestCancelChat(t *testing.T) {
	release := make(chan struct{})
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sse("开始"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)

	a := newLLMTestApp(t)
	enableLLM(t, a, srv.URL)

	start, err := a.SendChat("慢慢来")
	if err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	waitFor(t, 3*time.Second, "the request to be in flight", func() bool {
		a.llm.mu.Lock()
		defer a.llm.mu.Unlock()
		return a.llm.requestID == start.RequestID
	})

	if !a.CancelChat(start.RequestID) {
		t.Fatal("CancelChat reported nothing to cancel")
	}
	// The partial reply is kept, but a cancellation must NOT be written into the
	// transcript as an error.
	waitFor(t, 5*time.Second, "the cancellation to settle", func() bool {
		a.llm.mu.Lock()
		defer a.llm.mu.Unlock()
		return a.llm.requestID == ""
	})
	time.Sleep(50 * time.Millisecond) // let the goroutine finish persisting

	for _, m := range a.store.ChatHistory() {
		if m.Error && strings.Contains(m.Text, "取消") {
			t.Errorf("a cancellation was persisted as an error: %+v", m)
		}
	}

	// Cancelling again, or with a stale id, is not an error.
	if a.CancelChat(start.RequestID) {
		t.Error("a second cancel of the same request reported success")
	}
	if a.CancelChat("c-stale-id") {
		t.Error("cancelling an unknown request reported success")
	}
}

// --- budget ----------------------------------------------------------------

func TestDailyBudgetStopsFurtherRequests(t *testing.T) {
	served := 0
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		served++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse("hi"), "data: [DONE]\n\n")
	})

	a := newLLMTestApp(t)
	enableLLM(t, a, srv.URL)
	// A budget of one request.
	if _, err := a.SaveLLMSettings(LLMSettingsInput{
		Enabled: true, Provider: store.ProviderCustom,
		BaseURL: srv.URL, Model: "test-model",
		Temperature: 0.7, MaxTokens: 256, TimeoutSec: 10,
		HistoryTurns: 6, ContextTodos: true, DailyBudget: 1,
	}); err != nil {
		t.Fatalf("SaveLLMSettings: %v", err)
	}

	if _, err := a.SendChat("第一次"); err != nil {
		t.Fatalf("first SendChat: %v", err)
	}
	waitFor(t, 5*time.Second, "the first request to finish", func() bool {
		a.llm.mu.Lock()
		defer a.llm.mu.Unlock()
		return a.llm.requestID == ""
	})

	view := a.llmView()
	if !view.OverBudget {
		t.Fatalf("usage %+v should be over a budget of 1", view.Usage)
	}
	if view.Ready {
		t.Error("the view still reports ready while over budget")
	}
	if !strings.Contains(view.NotReadyReason, "上限") {
		t.Errorf("reason = %q, want it to mention the cap", view.NotReadyReason)
	}

	// The second request is refused, and no second HTTP call is made.
	before := served
	if _, err := a.SendChat("第二次"); err == nil {
		t.Fatal("a message was accepted while over budget")
	}
	time.Sleep(100 * time.Millisecond)
	if served != before {
		t.Errorf("the server was called %d times while over budget", served-before)
	}
}

func TestResetLLMUsageClearsTheCap(t *testing.T) {
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse("hi"), "data: [DONE]\n\n")
	})
	a := newLLMTestApp(t)
	enableLLM(t, a, srv.URL)
	if _, err := a.store.AddLLMUsage(10, 10, time.Now()); err != nil {
		t.Fatalf("AddLLMUsage: %v", err)
	}
	if !a.llmView().OverBudget == false && a.store.Config().LLMDailyBudget == 100 {
		// One request against a budget of 100 is not over; just check the reset.
	}
	if _, err := a.ResetLLMUsage(); err != nil {
		t.Fatalf("ResetLLMUsage: %v", err)
	}
	view := a.llmView()
	if view.Usage.Requests != 0 || view.Usage.PromptTokens != 0 {
		t.Errorf("usage after reset = %+v", view.Usage)
	}
	if view.OverBudget {
		t.Error("still over budget after a reset")
	}
}

// TestOneRequestCountsOnce guards the counter against the double-counting that a
// naive "record usage twice" implementation produces.
func TestOneRequestCountsOnce(t *testing.T) {
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse("hi"))
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	a := newLLMTestApp(t)
	enableLLM(t, a, srv.URL)

	if _, err := a.SendChat("一次"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	waitFor(t, 5*time.Second, "the request to finish", func() bool {
		a.llm.mu.Lock()
		defer a.llm.mu.Unlock()
		return a.llm.requestID == ""
	})
	time.Sleep(50 * time.Millisecond)

	usage := a.store.LLMUsageToday(time.Now())
	if usage.Requests != 1 {
		t.Errorf("requests = %d, want exactly 1 for one message", usage.Requests)
	}
	if usage.PromptTokens != 12 || usage.CompletionTokens != 3 {
		t.Errorf("tokens = %+v, want 12/3 from the server's report", usage)
	}
}

// --- regenerate and clear --------------------------------------------------

func TestRegenerateResendsTheLastQuestion(t *testing.T) {
	attempt := 0
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		attempt++
		w.Header().Set("Content-Type", "text/event-stream")
		if attempt == 1 {
			fmt.Fprint(w, sse("第一版"))
		} else {
			fmt.Fprint(w, sse("第二版"))
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	a := newLLMTestApp(t)
	enableLLM(t, a, srv.URL)

	if _, err := a.SendChat("问题"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	waitFor(t, 5*time.Second, "the first reply", func() bool {
		msgs := a.store.ChatHistory()
		return len(msgs) == 2
	})

	if _, err := a.Regenerate(); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	waitFor(t, 5*time.Second, "the regenerated reply", func() bool {
		for _, m := range a.store.ChatHistory() {
			if m.Text == "第二版" {
				return true
			}
		}
		return false
	})

	msgs := a.store.ChatHistory()
	if len(msgs) != 2 {
		t.Fatalf("transcript has %d entries after regenerating, want 2: %+v", len(msgs), msgs)
	}
	if msgs[0].Text != "问题" {
		t.Errorf("the question changed: %q", msgs[0].Text)
	}
	if msgs[1].Text != "第二版" {
		t.Errorf("the old answer survived: %q", msgs[1].Text)
	}
}

func TestRegenerateWithoutAQuestion(t *testing.T) {
	a := newLLMTestApp(t)
	if _, err := a.Regenerate(); err == nil {
		t.Fatal("Regenerate succeeded with an empty transcript")
	}
}

func TestClearChat(t *testing.T) {
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse("hi"), "data: [DONE]\n\n")
	})
	a := newLLMTestApp(t)
	enableLLM(t, a, srv.URL)

	if _, err := a.SendChat("你好"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	waitFor(t, 5*time.Second, "the reply", func() bool {
		return len(a.store.ChatHistory()) == 2
	})

	state, err := a.ClearChat()
	if err != nil {
		t.Fatalf("ClearChat: %v", err)
	}
	if len(state.LLM.Messages) != 0 {
		t.Errorf("the state still shows %d messages", len(state.LLM.Messages))
	}
	if n := len(a.store.ChatHistory()); n != 0 {
		t.Errorf("the transcript still has %d entries", n)
	}
}

// --- prompt context --------------------------------------------------------

func TestPromptContextIncludesTodosAndPomodoro(t *testing.T) {
	a := newLLMTestApp(t)

	if _, err := a.AddTodo(TodoInput{Title: "交周报"}); err != nil {
		t.Fatalf("AddTodo: %v", err)
	}
	// Backdate it so it is overdue, which is the case the label logic exists for.
	todos := a.store.Todos()
	if len(todos) != 1 {
		t.Fatalf("expected one todo, got %d", len(todos))
	}
	overdue := time.Now().Add(-2 * time.Hour)
	todos[0].Due = &overdue
	if err := a.store.SaveTodos(todos); err != nil {
		t.Fatalf("SaveTodos: %v", err)
	}

	ctx := a.promptContext(a.store.Config())

	if ctx.OpenCount != 1 || ctx.OverdueCount != 1 {
		t.Errorf("counts = open %d overdue %d, want 1/1", ctx.OpenCount, ctx.OverdueCount)
	}
	if len(ctx.Todos) != 1 || ctx.Todos[0].Title != "交周报" {
		t.Fatalf("todos = %+v", ctx.Todos)
	}
	if !ctx.Todos[0].Overdue {
		t.Error("the overdue flag was not set")
	}
	if !strings.Contains(ctx.Todos[0].Due, "逾期") {
		t.Errorf("due label = %q, want it to read as overdue", ctx.Todos[0].Due)
	}

	// The assembled prompt must carry it.
	prompt := promptFor(t, ctx)
	if !strings.Contains(prompt, "交周报") {
		t.Errorf("prompt is missing the todo:\n%s", prompt)
	}
}

func TestPromptContextIncludesPomodoroState(t *testing.T) {
	a := newLLMTestApp(t)
	end := time.Now().Add(10 * time.Minute)
	if err := a.store.SavePomodoro(store.PomodoroState{
		Enabled: true, Phase: store.PhaseFocus, RunState: store.RunRunning,
		EndsAt: &end, CurrentTodoTitle: "写周报",
	}); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}

	ctx := a.promptContext(a.store.Config())
	if !ctx.Pomodoro.Enabled {
		t.Fatal("the pomodoro did not reach the prompt context")
	}
	if ctx.Pomodoro.Phase == "" {
		t.Error("the phase is empty for a running focus session")
	}
	if ctx.Pomodoro.Remaining == "" {
		t.Error("the countdown is empty for a running focus session")
	}
	if ctx.Pomodoro.TodoTitle != "写周报" {
		t.Errorf("todo title = %q", ctx.Pomodoro.TodoTitle)
	}
	prompt := promptFor(t, ctx)
	if !strings.Contains(prompt, "写周报") {
		t.Errorf("prompt is missing the running session:\n%s", prompt)
	}
}

// TestPromptContextReplaysRecentSummaries covers the "what did I do today"
// question, which is unanswerable without the summaries.
func TestPromptContextReplaysRecentSummaries(t *testing.T) {
	a := newLLMTestApp(t)
	if _, err := a.store.AddPomodoroRecord(store.PomodoroRecord{
		TodoTitle: "写文档", Summary: "把接口文档补完了",
		EndedAt: time.Now().Add(-3 * time.Hour), FocusMinutes: 25,
	}); err != nil {
		t.Fatalf("AddPomodoroRecord: %v", err)
	}

	ctx := a.promptContext(a.store.Config())
	if len(ctx.RecentSessions) == 0 {
		t.Fatal("no sessions reached the context")
	}
	prompt := promptFor(t, ctx)
	if !strings.Contains(prompt, "把接口文档补完了") {
		t.Errorf("the summary is missing from the prompt:\n%s", prompt)
	}
}

func TestPromptContextRespectsContextSwitch(t *testing.T) {
	a := newLLMTestApp(t)
	if _, err := a.AddTodo(TodoInput{Title: "秘密任务"}); err != nil {
		t.Fatalf("AddTodo: %v", err)
	}
	if _, err := a.SaveLLMSettings(LLMSettingsInput{
		Enabled: true, Provider: store.ProviderCustom, BaseURL: "http://x/v1", Model: "m",
		Temperature: 0.8, MaxTokens: 800, TimeoutSec: 90,
		ContextTodos: false, DailyBudget: 100,
	}); err != nil {
		t.Fatalf("SaveLLMSettings: %v", err)
	}

	ctx := a.promptContext(a.store.Config())
	if ctx.IncludeTodos {
		t.Fatal("the switch was not honoured")
	}
	if len(ctx.Todos) != 0 || ctx.OpenCount != 0 {
		t.Errorf("todos leaked into the context: %+v", ctx.Todos)
	}
	if strings.Contains(promptFor(t, ctx), "秘密任务") {
		t.Error("a todo reached the prompt with context injection switched off")
	}
}

// promptFor renders a context through the llm package's builder, so these tests
// assert on what the model actually receives rather than on the intermediate
// struct.
func promptFor(t *testing.T, ctx llm.PromptContext) string {
	t.Helper()
	return llm.BuildSystemPrompt(ctx)
}

func TestTrimToRunes(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"  hi  ", 10, "hi"},
		{"你好世界", 2, "你好"},
		{"你好世界", 10, "你好世界"},
		{"", 5, ""},
	}
	for _, c := range cases {
		if got := trimToRunes(c.in, c.n); got != c.want {
			t.Errorf("trimToRunes(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
	// A cut must never produce invalid UTF-8.
	got := trimToRunes(strings.Repeat("喵", 100), 3)
	if !utf8.ValidString(got) {
		t.Errorf("trimToRunes produced invalid UTF-8: %q", got)
	}
}

func TestPromptDueLabel(t *testing.T) {
	now := time.Date(2025, 3, 7, 14, 0, 0, 0, time.Local)
	mk := func(d time.Time) store.Todo {
		return store.Todo{Title: "t", Due: &d}
	}
	cases := []struct {
		name string
		todo store.Todo
		want string
	}{
		{"no due", store.Todo{Title: "t"}, ""},
		{"overdue", mk(now.Add(-2 * time.Hour)), "已逾期，03-07 12:00"},
		{"today", mk(now.Add(3 * time.Hour)), "今天 17:00"},
		{"tomorrow", mk(now.AddDate(0, 0, 1)), "明天 14:00"},
		{"later", mk(now.AddDate(0, 0, 5)), "03-12 14:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := promptDueLabel(c.todo, now); got != c.want {
				t.Errorf("promptDueLabel = %q, want %q", got, c.want)
			}
		})
	}
}

func TestChatMessagesConversion(t *testing.T) {
	in := []store.ChatMessage{
		{Role: store.RoleUser, Text: "问"},
		{Role: store.RoleAssistant, Text: "答"},
	}
	out := chatMessages(in)
	if len(out) != 2 || out[0].Role != "user" || out[1].Content != "答" {
		t.Fatalf("conversion wrong: %+v", out)
	}
	if got := chatMessages(nil); len(got) != 0 {
		t.Errorf("nil input produced %d messages", len(got))
	}
}

// --- test connection -------------------------------------------------------

func TestTestLLMConnectionSuccess(t *testing.T) {
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse("pong"), "data: [DONE]\n\n")
	})
	a := newLLMTestApp(t)
	enableLLM(t, a, srv.URL)

	res := a.TestLLMConnection()
	if !res.OK {
		t.Fatalf("test connection failed: %+v", res)
	}
	if res.Error != "" {
		t.Errorf("a successful test carries an error: %q", res.Error)
	}
	if res.ElapsedMS < 0 {
		t.Errorf("elapsed = %d", res.ElapsedMS)
	}
}

func TestTestLLMConnectionFailure(t *testing.T) {
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"message":"model not found"}}`)
	})
	a := newLLMTestApp(t)
	enableLLM(t, a, srv.URL)

	res := a.TestLLMConnection()
	if res.OK {
		t.Fatal("test connection reported success against a 404")
	}
	if res.Kind != "model" {
		t.Errorf("kind = %q, want model", res.Kind)
	}
	if !strings.Contains(res.Error, "不存在") {
		t.Errorf("error = %q, want it in Chinese", res.Error)
	}
}

// TestTestLLMConnectionProbeIsCheap: the probe costs the user real money, so it
// must ask for a single token.
func TestTestLLMConnectionProbeIsCheap(t *testing.T) {
	var body string
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		buf := make([]byte, 8192)
		n, _ := r.Body.Read(buf)
		body = string(buf[:n])
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse("p"), "data: [DONE]\n\n")
	})
	a := newLLMTestApp(t)
	enableLLM(t, a, srv.URL)

	if res := a.TestLLMConnection(); !res.OK {
		t.Fatalf("test connection failed: %+v", res)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("body: %v", err)
	}
	if decoded["max_tokens"] != float64(1) {
		t.Errorf("probe max_tokens = %v, want 1", decoded["max_tokens"])
	}
}

// --- window ----------------------------------------------------------------

// TestOpenChatOpensThePanel covers the shape of the conversation surface: it is a
// tab inside the panel, so OpenChat opens the panel and tells the frontend which
// tab to show.
func TestOpenChatOpensThePanel(t *testing.T) {
	a, events := newLLMTestAppWithEvents(t)

	if _, err := a.OpenChat(); err != nil {
		// Without a real window the geometry call fails; that is an environment
		// limitation, not a logic error, so the state is asserted anyway.
		t.Logf("OpenChat geometry: %v", err)
	}

	a.mu.Lock()
	panelOpen := a.panelOpen
	bubble := a.bubble
	a.mu.Unlock()

	if !panelOpen {
		t.Error("OpenChat did not open the panel")
	}
	// The conversation is a tab, so nothing should be occupying the bubble slot.
	if bubble != nil {
		t.Errorf("OpenChat raised a bubble: %+v", bubble)
	}

	// The frontend needs the event to switch to the conversation tab.
	if !events.has(eventChatOpen) {
		t.Errorf("OpenChat did not emit %s (saw %v)", eventChatOpen, events.names())
	}
}

// TestOpenTodoPanelFromChatReportsTheOtherTab: the conversation's "待办" button
// has to move the frontend back, which it does by event because the layout
// carries geometry rather than which tab is showing.
func TestOpenTodoPanelFromChatReportsTheOtherTab(t *testing.T) {
	a, events := newLLMTestAppWithEvents(t)

	if _, err := a.OpenTodoPanelFromChat(); err != nil {
		t.Logf("OpenTodoPanelFromChat geometry: %v", err)
	}
	a.mu.Lock()
	panelOpen := a.panelOpen
	a.mu.Unlock()
	if !panelOpen {
		t.Error("the panel did not open")
	}

	// The last chat-open event must be the "false" (leave the chat) one.
	all := events.byName(eventChatOpen)
	if len(all) == 0 {
		t.Fatalf("no %s event (saw %v)", eventChatOpen, events.names())
	}
	if v, ok := all[len(all)-1].data["open"].(bool); !ok || v {
		t.Errorf("the last %s event was %+v, want open=false", eventChatOpen, all[len(all)-1].data)
	}
}

func TestShutdownCancelsChat(t *testing.T) {
	release := make(chan struct{})
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sse("一"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)

	a := newLLMTestApp(t)
	enableLLM(t, a, srv.URL)
	// The loops are not running in this test, so the channels still need closing.
	a.pomoStop = make(chan struct{})
	a.remStop = make(chan struct{})

	if _, err := a.SendChat("占用着"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	waitFor(t, 3*time.Second, "the request to be in flight", func() bool {
		a.llm.mu.Lock()
		defer a.llm.mu.Unlock()
		return a.llm.requestID != ""
	})

	a.shutdown()

	a.llm.mu.Lock()
	id := a.llm.requestID
	a.llm.mu.Unlock()
	if id != "" {
		t.Errorf("shutdown left request %q marked as running", id)
	}
	// Shutting down twice must not panic on the closed channels.
	a.shutdown()
}
