package llm

// The transport's log lines, and - more importantly - what they must NOT contain.
//
// Two properties are asserted here because both are invisible on screen:
//
//  1. a retry is logged. "模型没回复" with no evidence is the report this whole
//     logging change exists to make answerable, and a 429 that was retried is the
//     commonest cause that looks like silence from the outside;
//  2. the user's own words and the API key never appear. The lines go to a file the
//     control center shows and the 诊断报告 copies, so a single careless `%s` on the
//     payload would turn every bug report into a leak.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogfRecordsAShapeAndNoContent(t *testing.T) {
	fs := newFakeServer(t, func(w http.ResponseWriter, r *http.Request, attempt int) {
		if attempt == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
			return
		}
		serveSSE(w, sseChunk("好")+finishChunk("stop", map[string]any{
			"prompt_tokens": 11, "completion_tokens": 3, "total_tokens": 14,
		}), 8)
	})

	var mu sync.Mutex
	var lines []string
	c := NewClient()
	c.RetryBackoff = []time.Duration{time.Millisecond}
	c.Logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}

	const secret = "sk-do-not-log-me"
	const question = "我下周三要去医院复查"
	res, err := c.Chat(context.Background(), Settings{
		BaseURL: fs.URL,
		Model:   "deepseek-chat",
		APIKey:  secret,
		Timeout: 5 * time.Second,
	}, []Message{{Role: "user", Content: question}}, Handlers{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Text != "好" {
		t.Fatalf("the retry lost the reply: %q", res.Text)
	}

	mu.Lock()
	joined := strings.Join(lines, "\n")
	mu.Unlock()

	for _, want := range []string{"POST", "deepseek-chat", "HTTP 429", "重试", "tokens in=11 out=3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the log does not mention %q:\n%s", want, joined)
		}
	}
	for _, forbidden := range []string{question, secret, "slow down"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("the log leaked %q:\n%s", forbidden, joined)
		}
	}
}
