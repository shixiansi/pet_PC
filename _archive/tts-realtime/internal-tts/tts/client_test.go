package tts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer is a stand-in for GPT-SoVITS' api_v2.py.
//
// It records what it was asked so the tests can assert on the exact request
// shape - which matters here because the field names are the server's, not ours,
// and a typo in a JSON tag would only show up as a "tts failed" at runtime.
type fakeServer struct {
	mu sync.Mutex
	// synthBodies holds every decoded /tts body, in order.
	synthBodies []map[string]any
	// weightCalls records "path=weights" for each character-loading request.
	weightCalls []string
	// audio is what /tts returns on success.
	audio []byte
	// status/errBody override the /tts response when status is non-zero.
	status  int
	errBody string
	// synthDelay simulates CPU-bound synthesis.
	synthDelay time.Duration
	// weightStatus overrides the weight endpoints' status when non-zero.
	weightStatus int
}

func (f *fakeServer) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/tts", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		json.Unmarshal(body, &decoded)

		f.mu.Lock()
		f.synthBodies = append(f.synthBodies, decoded)
		status, errBody, delay, audio := f.status, f.errBody, f.synthDelay, f.audio
		f.mu.Unlock()

		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		if status != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			io.WriteString(w, errBody)
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(audio)
	})

	setWeights := func(kind string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			weights := r.URL.Query().Get("weights_path")
			f.mu.Lock()
			f.weightCalls = append(f.weightCalls, kind+"="+weights)
			status := f.weightStatus
			f.mu.Unlock()

			if status != 0 {
				w.WriteHeader(status)
				io.WriteString(w, `{"message":"change weight failed"}`)
				return
			}
			io.WriteString(w, `{"message":"success"}`)
		}
	}
	mux.HandleFunc("/set_gpt_weights", setWeights("gpt"))
	mux.HandleFunc("/set_sovits_weights", setWeights("sovits"))
	return mux
}

// newFake starts the fake server and returns it with a Settings pointing at it.
func newFake(t *testing.T) (*fakeServer, Settings) {
	t.Helper()
	f := &fakeServer{audio: []byte("RIFF....WAVEfake-audio-bytes")}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	return f, Settings{
		BaseURL:    srv.URL,
		RefAudio:   `D:\voices\ref.wav`,
		PromptText: "今天的天气非常好。",
		Lang:       "zh",
		Timeout:    5 * time.Second,
	}
}

// TestSynthesizeSendsServerFieldNames pins the wire contract.
//
// The names here are api_v2.py's, and the mismatch is the classic failure: our
// own code would happily use whatever names we chose, and the server would answer
// 400 "text_lang is required" only once a user tried it.
func TestSynthesizeSendsServerFieldNames(t *testing.T) {
	f, s := newFake(t)
	c := NewClient()

	if _, err := c.Synthesize(context.Background(), s, "你好。"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.synthBodies) != 1 {
		t.Fatalf("server saw %d /tts requests, want 1", len(f.synthBodies))
	}
	body := f.synthBodies[0]

	want := map[string]any{
		"text":           "你好。",
		"text_lang":      "zh",
		"ref_audio_path": `D:\voices\ref.wav`,
		"prompt_text":    "今天的天气非常好。",
		"prompt_lang":    "zh",
		"media_type":     "wav",
		"streaming_mode": false,
	}
	for k, v := range want {
		got, ok := body[k]
		if !ok {
			t.Errorf("request is missing %q (server would reject this)", k)
			continue
		}
		if got != v {
			t.Errorf("%s = %v, want %v", k, got, v)
		}
	}
	// streaming_mode must be present and false: the server treats a MISSING key
	// as its own default, and a wrong type as a 400.
	if v, ok := body["streaming_mode"].(bool); !ok || v {
		t.Errorf("streaming_mode = %#v, want boolean false", body["streaming_mode"])
	}
}

// TestSynthesizeReturnsAudio covers the happy path.
func TestSynthesizeReturnsAudio(t *testing.T) {
	f, s := newFake(t)
	audio, err := NewClient().Synthesize(context.Background(), s, "测试。")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if string(audio) != string(f.audio) {
		t.Fatalf("audio = %q, want %q", audio, f.audio)
	}
}

// TestSynthesizeReportsServerMessages pins that the server's own validation text
// reaches the user. "ref_audio_path is required" tells them exactly what to fix;
// a generic "synthesis failed" does not.
func TestSynthesizeReportsServerMessages(t *testing.T) {
	f, s := newFake(t)
	f.status = 400
	f.errBody = `{"message": "text_lang: xx is not supported in version v2"}`

	_, err := NewClient().Synthesize(context.Background(), s, "你好。")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "text_lang") {
		t.Errorf("error %q does not carry the server's message", err)
	}
	var se *SynthError
	if !errors.As(err, &se) {
		t.Fatalf("error is %T, want *SynthError", err)
	}
	if se.Kind != KindRequest {
		t.Errorf("kind = %q, want %q", se.Kind, KindRequest)
	}
}

// TestSynthesizeSurfacesWrappedException covers the shape the server uses when
// the pipeline itself throws: {"message":"tts failed","Exception":"..."}. The
// exception is the only part that says why.
func TestSynthesizeSurfacesWrappedException(t *testing.T) {
	f, s := newFake(t)
	f.status = 400
	f.errBody = `{"message": "tts failed", "Exception": "the reference audio is too short"}`

	_, err := NewClient().Synthesize(context.Background(), s, "你好。")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("error %q does not carry the exception detail", err)
	}
}

// TestSynthesizeClassifiesUnreachable is the case a user actually hits: the
// synthesis server is not running. The message has to say so, because "connection
// refused" is not actionable to someone who did not know a server was involved.
func TestSynthesizeClassifiesUnreachable(t *testing.T) {
	// A server that is closed immediately: the port is almost certainly unused.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	s := Settings{BaseURL: url, RefAudio: "ref.wav", PromptText: "x", Lang: "zh", Timeout: 2 * time.Second}
	_, err := NewClient().Synthesize(context.Background(), s, "你好。")
	if err == nil {
		t.Fatal("expected an error")
	}
	var se *SynthError
	if !errors.As(err, &se) {
		t.Fatalf("error is %T, want *SynthError", err)
	}
	if se.Kind != KindUnreachable {
		t.Errorf("kind = %q, want %q", se.Kind, KindUnreachable)
	}
	if !strings.Contains(err.Error(), "GPT-SoVITS") {
		t.Errorf("error %q does not tell the user what to start", err)
	}
}

// TestSynthesizeTimeoutKeepsGoing is the queue-preserving property: one slow
// sentence must fail alone rather than stalling the ones behind it.
func TestSynthesizeTimeoutKeepsGoing(t *testing.T) {
	f, s := newFake(t)
	f.synthDelay = 400 * time.Millisecond
	s.Timeout = 80 * time.Millisecond

	start := time.Now()
	_, err := NewClient().Synthesize(context.Background(), s, "这一句很慢。")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("took %v, want the timeout to fire near 80ms", elapsed)
	}
	var se *SynthError
	if !errors.As(err, &se) || se.Kind != KindTimeout {
		t.Errorf("kind = %v, want %v", se, KindTimeout)
	}
}

// TestSynthesizeCancelIsRecognised pins that interrupting speech is not an error
// the user should ever see. It happens constantly: every new reply, every stop.
func TestSynthesizeCancelIsRecognised(t *testing.T) {
	f, s := newFake(t)
	f.synthDelay = 2 * time.Second
	s.Timeout = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := NewClient().Synthesize(ctx, s, "会被打断。")
	if err == nil {
		t.Fatal("expected an error from the cancelled request")
	}
	if !IsCanceled(ctx, err) {
		t.Errorf("IsCanceled(%v) = false; a user-initiated stop must not surface", err)
	}
}

// TestTimeoutIsNotCanceled pins the distinction that a timeout is a real failure
// while a cancellation is not, given the same context. Both propagate as context
// errors through http.Client, so this is exactly the case where a naive
// implementation reports a timeout as "已取消。" and hides it from the log.
func TestTimeoutIsNotCanceled(t *testing.T) {
	f, s := newFake(t)
	f.synthDelay = 400 * time.Millisecond
	s.Timeout = 60 * time.Millisecond

	// A live context: the request stops because of its own deadline, not because
	// anyone cancelled it.
	ctx := context.Background()
	_, err := NewClient().Synthesize(ctx, s, "这一句很慢。")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if IsCanceled(ctx, err) {
		t.Errorf("a timeout was classified as a cancellation: %v", err)
	}
	var se *SynthError
	if !errors.As(err, &se) || se.Kind != KindTimeout {
		t.Errorf("kind = %v, want %v", se, KindTimeout)
	}
}

// TestSynthesizeEmptyAudioIsAnError covers a server that answers 200 with nothing.
// Treated as a failure rather than played, because writing a zero-byte file and
// handing it to the player would produce silence with no explanation.
func TestSynthesizeEmptyAudioIsAnError(t *testing.T) {
	f, s := newFake(t)
	f.audio = nil

	_, err := NewClient().Synthesize(context.Background(), s, "你好。")
	if err == nil {
		t.Fatal("expected an error for empty audio")
	}
	if !strings.Contains(err.Error(), "空音频") {
		t.Errorf("error = %q, want it to mention empty audio", err)
	}
}

// TestSynthesizeRejectsBlankInput saves a round trip on text that cannot produce
// speech.
func TestSynthesizeRejectsBlankInput(t *testing.T) {
	_, s := newFake(t)
	if _, err := NewClient().Synthesize(context.Background(), s, "   "); err == nil {
		t.Error("expected an error for blank text")
	}
	if _, err := NewClient().Synthesize(context.Background(), Settings{BaseURL: s.BaseURL}, "你好。"); err == nil {
		t.Error("expected an error when no reference audio is configured")
	}
}

// TestCharacterWeightsAreLoadedOnce is the performance property that makes the
// pipeline viable on CPU: loading a character costs seconds, so it must happen
// once per session rather than once per sentence. Ten sentences must not produce
// ten reloads.
func TestCharacterWeightsAreLoadedOnce(t *testing.T) {
	f, s := newFake(t)
	s.GPTWeights = `D:\voices\kokomi-e10.ckpt`
	s.SovitsWeights = `D:\voices\kokomi_e10_s1683.pth`
	c := NewClient()

	for i := 0; i < 10; i++ {
		if _, err := c.Synthesize(context.Background(), s, "第几句都一样。"); err != nil {
			t.Fatalf("Synthesize %d: %v", i, err)
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.weightCalls) != 2 {
		t.Fatalf("weight endpoints called %d times (%v), want exactly 2 for 10 sentences",
			len(f.weightCalls), f.weightCalls)
	}
	if f.weightCalls[0] != `gpt=D:\voices\kokomi-e10.ckpt` {
		t.Errorf("first weight call = %q", f.weightCalls[0])
	}
	if f.weightCalls[1] != `sovits=D:\voices\kokomi_e10_s1683.pth` {
		t.Errorf("second weight call = %q", f.weightCalls[1])
	}
}

// TestCharacterWeightsReloadOnChange is the other half: switching character must
// actually push the new files. Without this the cache would silently keep the old
// voice, and the user would change the setting and hear no difference.
func TestCharacterWeightsReloadOnChange(t *testing.T) {
	f, s := newFake(t)
	s.GPTWeights = "a.ckpt"
	s.SovitsWeights = "a.pth"
	c := NewClient()

	c.Synthesize(context.Background(), s, "第一句。")
	s.GPTWeights = "b.ckpt"
	c.Synthesize(context.Background(), s, "第二句。")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.weightCalls) != 4 {
		t.Fatalf("weight calls = %v, want 4 (two pairs)", f.weightCalls)
	}
	if f.weightCalls[2] != "gpt=b.ckpt" {
		t.Errorf("third call = %q, want the new checkpoint", f.weightCalls[2])
	}
}

// TestForgetCharacterForcesReload backs the settings panel's test button: after
// the user fixes a wrong path, the next attempt must push it again instead of
// trusting the cache.
func TestForgetCharacterForcesReload(t *testing.T) {
	f, s := newFake(t)
	s.GPTWeights = "a.ckpt"
	c := NewClient()

	c.Synthesize(context.Background(), s, "第一句。")
	c.ForgetCharacter()
	c.Synthesize(context.Background(), s, "第二句。")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.weightCalls) != 2 {
		t.Fatalf("weight calls = %v, want 2 after ForgetCharacter", f.weightCalls)
	}
}

// TestCharacterLoadFailureStopsSynthesis is a deliberate choice worth pinning:
// if the requested voice cannot be loaded, speaking anyway would produce audio in
// the WRONG voice with no indication. Failing is the lesser evil.
func TestCharacterLoadFailureStopsSynthesis(t *testing.T) {
	f, s := newFake(t)
	s.GPTWeights = "missing.ckpt"
	f.weightStatus = 400

	_, err := NewClient().Synthesize(context.Background(), s, "你好。")
	if err == nil {
		t.Fatal("expected an error when the character cannot be loaded")
	}
	if !strings.Contains(err.Error(), "权重") {
		t.Errorf("error = %q, want it to mention the weights", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.synthBodies) != 0 {
		t.Errorf("synthesis was attempted despite the character failing to load")
	}
}

// TestNoWeightsConfiguredSkipsLoading covers the server-managed voice case: a user
// who configured the character in the server's own config file must not be forced
// to duplicate those paths in PetPC.
func TestNoWeightsConfiguredSkipsLoading(t *testing.T) {
	f, s := newFake(t)
	if _, err := NewClient().Synthesize(context.Background(), s, "你好。"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.weightCalls) != 0 {
		t.Errorf("weight endpoints were called with nothing configured: %v", f.weightCalls)
	}
}

// TestLangDefaultsToChinese covers a config written before the field existed.
func TestLangDefaultsToChinese(t *testing.T) {
	f, s := newFake(t)
	s.Lang = ""

	if _, err := NewClient().Synthesize(context.Background(), s, "你好。"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := f.synthBodies[0]["text_lang"]; got != "zh" {
		t.Errorf("text_lang = %v, want zh", got)
	}
}

// TestPingTreats400AsAlive is the settings-panel test button's contract: the
// probe deliberately sends no reference audio, so the server answers 400. That
// still proves it is running, and reporting it as a failure would make a working
// setup look broken.
func TestPingTreats400AsAlive(t *testing.T) {
	f, s := newFake(t)
	f.status = 400
	f.errBody = `{"message":"ref_audio_path is required"}`

	if err := NewClient().Ping(context.Background(), s.BaseURL); err != nil {
		t.Errorf("Ping against a live server returned %v, want nil", err)
	}
}

// TestPingFailsWhenNothingListens is the other half: a dead server must be
// reported.
func TestPingFailsWhenNothingListens(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	if err := NewClient().Ping(context.Background(), url); err == nil {
		t.Error("Ping against a dead server returned nil")
	}
}

// TestServerMessageHandlesNonJSON makes sure an HTML error page cannot end up
// pasted whole into the UI.
func TestServerMessageHandlesNonJSON(t *testing.T) {
	long := "<html><body>" + strings.Repeat("error ", 200) + "</body></html>"
	got := serverMessage([]byte(long))
	if len([]rune(got)) > 201 {
		t.Errorf("serverMessage returned %d runes, want it truncated", len([]rune(got)))
	}
	if strings.Contains(got, "<") {
		t.Errorf("serverMessage leaked markup: %q", got)
	}
}

// TestServerMessageOnGarbage covers a body that is neither JSON nor readable.
func TestServerMessageOnGarbage(t *testing.T) {
	if got := serverMessage([]byte("   ")); got != "" {
		t.Errorf("serverMessage(blank) = %q, want empty", got)
	}
	if got := serverMessage([]byte("{not json")); got == "" {
		t.Error("serverMessage should fall back to the raw text")
	}
}

// TestClientIsSafeForConcurrentUse is a race-detector test. The pipeline runs
// synthesis from one goroutine per sentence, so the character cache and the
// shared HTTP client are both genuinely concurrent.
func TestClientIsSafeForConcurrentUse(t *testing.T) {
	f, s := newFake(t)
	s.GPTWeights = "a.ckpt"
	s.SovitsWeights = "a.pth"
	c := NewClient()

	var wg sync.WaitGroup
	var failures int64
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Synthesize(context.Background(), s, "并发测试。"); err != nil {
				atomic.AddInt64(&failures, 1)
			}
		}()
	}
	wg.Wait()

	if failures != 0 {
		t.Errorf("%d concurrent syntheses failed", failures)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// However the goroutines interleaved, the character must not have been
	// loaded more than once per weight file.
	if len(f.weightCalls) > 2 {
		t.Errorf("character loaded %d times under concurrency, want at most 2", len(f.weightCalls))
	}
}
