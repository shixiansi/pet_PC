package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"PetPC/internal/llm"
	"PetPC/internal/store"
)

// This file used to hold the host's own conversation tests: readiness, sending,
// cancellation, regeneration, budget and the settings panel. All of that lived on
// bindings the pet no longer exposes - the conversation is a plugin, its
// configuration lives in the plugin market, and its request path is asserted in
// app_plugins_test.go (T72-T77). What survives here is the machinery the rest of
// the suite shares (an App wired for events, an endpoint stub, an SSE writer) and
// the two things the pet still owns: the prompt context it builds for a plugin,
// and the window entry points that open the conversation tab.
//
// Keeping the helpers here rather than moving them is deliberate: half a dozen
// test files call newLLMTestApp, and a rename would have been churn with no
// behavioural content.

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
// context other than the one its lifecycle hooks supplied, and a plugin request
// emits from its own goroutine. Without somewhere else for those events to go,
// none of the plugin protocol could be tested outside a running window.
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
// Tests must wait on the EVENT rather than on a side effect the handler also
// performs: the host emits the terminal frame after it has done its bookkeeping,
// so a test that waits for the side effect can observe a state where the write
// has landed but the frame has not, and then fail looking for the frame.
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

// waitFor polls cond until it is true or the deadline passes.
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

// --- prompt context --------------------------------------------------------

// The snapshot is the persona and the clock, and the interesting half is what is NOT in it:
// the host no longer owns the todo list or the timer, so nothing about the user's day can
// reach the model through this path. Asserted from here rather than from internal/llm
// because that package can only see the struct it is given - the leak this guards against is
// a caller filling it back in.
func TestPromptContextCarriesNoUserData(t *testing.T) {
	a := newLLMTestApp(t)

	prompt := promptFor(t, a.promptContext(a.store.Config()))
	for _, leaked := range []string{"待办：", "番茄钟未开启", "专注中"} {
		if strings.Contains(prompt, leaked) {
			t.Errorf("the prompt still describes the user's day (%q): the host does not own that data any more:\n%s", leaked, prompt)
		}
	}
	if !strings.Contains(prompt, "被问到就说自己看不到") {
		t.Errorf("the prompt should say the list and the timer are not visible:\n%s", prompt)
	}
	if !strings.Contains(prompt, "现在：") {
		t.Errorf("the clock is not private data and should still be there:\n%s", prompt)
	}
}

// promptFor renders a context through the llm package's builder, so these tests
// assert on what the model actually receives rather than on the intermediate
// struct.
func promptFor(t *testing.T, ctx llm.PromptContext) string {
	t.Helper()
	return llm.BuildSystemPrompt(ctx)
}

// TestPromptContextIncludesTodosAndPomodoro, TestPromptContextRespectsContextSwitch,
// TestPromptDueLabel, TestOpenChatOpensThePanel and TestOpenTodoPanelFromChatReportsTheOtherTab
// used to live here. The first three described data the host no longer has (the list is the
// todo plugin's storage and the timer is its own plugin), and the last two described the
// panel's tabs - OpenChat and OpenTodoPanelFromChat are gone with the panel, and the
// conversation is the chat plugin's floating surface now.
// TestShutdownIsIdempotentWithoutAChatRequest: the conversation no longer runs
// inside the pet, so there is no in-flight request for shutdown to cancel - but
// shutdown still releases the global shortcuts and the plugin websockets, and doing
// that twice would double-close. The double call is what pins that down.
//
// The pomodoro ticker's stop channel used to be set up here too; it went with the
// timer, so shutdown now has two things to release instead of three.
func TestShutdownIsIdempotentWithoutAChatRequest(t *testing.T) {
	a := newLLMTestApp(t)

	a.shutdown()
	a.shutdown()
}
