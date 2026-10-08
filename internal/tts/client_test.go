package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests use httptest rather than the real engine: they pin the CONTRACT the
// studio depends on (which endpoints, in which order, with which parameters), which
// is exactly what a local server upgrade would break quietly.

// fakeEngine records what the client asked for and answers like api_v2.py.
type fakeEngine struct {
	requests []string
	// selects records the weight switches, in order.
	selects []string
	// order is every call in the order it arrived, so the select-then-synthesise
	// sequence can be asserted rather than assumed.
	order []string
	// failSynthesis makes /tts answer 400 with the engine's own error shape.
	failSynthesis string
}

func (f *fakeEngine) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/tts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			f.requests = append(f.requests, "GET /tts")
			f.order = append(f.order, "GET /tts")
			// The engine answers 422 when required query params are missing, which
			// is what Health treats as "up and loaded".
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		var req Request
		json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck // test fixture
		body, _ := json.Marshal(req)
		f.requests = append(f.requests, "POST /tts "+string(body))
		f.order = append(f.order, "synth")
		if f.failSynthesis != "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"message": f.failSynthesis}) //nolint:errcheck
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(wavBytes()) //nolint:errcheck
	})
	for _, p := range []string{"/set_gpt_weights", "/set_sovits_weights"} {
		path := p
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			f.selects = append(f.selects, path+"?"+r.URL.RawQuery)
			f.order = append(f.order, "select")
			json.NewEncoder(w).Encode(map[string]string{"message": "success"}) //nolint:errcheck
		})
	}
	return mux
}

// wavBytes is a minimal valid WAV, so isWAV accepts it.
func wavBytes() []byte {
	b := make([]byte, 44+16)
	copy(b[0:], "RIFF")
	copy(b[8:], "WAVE")
	copy(b[12:], "fmt ")
	return b
}

func newFake(t *testing.T) (*Client, *fakeEngine, func()) {
	t.Helper()
	f := &fakeEngine{}
	srv := httptest.NewServer(f.handler())
	return New(srv.URL), f, srv.Close
}

// TestRenderSelectsBothWeightsThenSynthesises pins the ordering that the studio
// depends on and that is easy to get wrong: the engine's selected character is
// SERVER-SIDE STATE, so synthesising before (or without) the switch returns audio
// in whatever voice was selected last.
func TestRenderSelectsBothWeightsThenSynthesises(t *testing.T) {
	c, f, done := newFake(t)
	defer done()

	audio, err := c.Render(context.Background(),
		RoleRef{GPT: `D:\m\klee\gpt.ckpt`, SoVITS: `D:\m\klee\sovits.pth`},
		Request{Text: "专注时间结束了", TextLang: "zh", RefAudioPath: `D:\m\klee\ref.wav`},
	)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(audio) == 0 {
		t.Fatal("Render returned no audio")
	}
	if len(f.selects) != 2 {
		t.Fatalf("%d weight switches, want 2 (GPT and SoVITS)", len(f.selects))
	}
	if !strings.Contains(f.selects[0], "set_gpt_weights") {
		t.Errorf("first switch was %q, want the GPT weights", f.selects[0])
	}
	if !strings.Contains(f.selects[1], "set_sovits_weights") {
		t.Errorf("second switch was %q, want the SoVITS weights", f.selects[1])
	}
	// The synthesis must happen AFTER both switches: the engine's selected
	// character is server-side state, so synthesising first would return audio in
	// whatever voice was selected previously.
	want := []string{"select", "select", "synth"}
	if len(f.order) != len(want) {
		t.Fatalf("call order %v, want %v", f.order, want)
	}
	for i := range want {
		if f.order[i] != want[i] {
			t.Fatalf("call order %v, want %v", f.order, want)
		}
	}
	// Paths must survive as backslashes in the query string.
	if !strings.Contains(f.selects[0], "gpt.ckpt") {
		t.Errorf("the GPT path did not reach the engine: %q", f.selects[0])
	}
}

// TestSynthesizeSendsTheRoleParameters is the other half of "the take is
// reproducible": every knob that shapes the audio must actually be transmitted.
func TestSynthesizeSendsTheRoleParameters(t *testing.T) {
	c, f, done := newFake(t)
	defer done()

	_, err := c.Synthesize(context.Background(), Request{
		Text:              "测试",
		TextLang:          "zh",
		RefAudioPath:      "r.wav",
		SpeedFactor:       1.25,
		Temperature:       0.7,
		TopK:              9,
		TopP:              0.85,
		RepetitionPenalty: 1.5,
		Seed:              42,
	})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	var sent Request
	for _, r := range f.requests {
		if strings.HasPrefix(r, "POST /tts ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(r, "POST /tts ")), &sent); err != nil {
				t.Fatalf("bad request body: %v", err)
			}
		}
	}
	if sent.SpeedFactor != 1.25 || sent.Temperature != 0.7 || sent.TopK != 9 ||
		sent.TopP != 0.85 || sent.RepetitionPenalty != 1.5 || sent.Seed != 42 {
		t.Errorf("parameters were dropped in transit: %+v", sent)
	}
	if sent.MediaType != "wav" {
		t.Errorf("media type %q, want wav (the pet plays WAV files)", sent.MediaType)
	}
	if sent.StreamingMode {
		t.Error("streaming must be off: a whole-file render is what gets stored")
	}
}

// TestDefaultsAreFilledIn guards the case where a role predates a new field: the
// engine rejects or mis-handles zeroes, and a zero here would mean "unset".
func TestDefaultsAreFilledIn(t *testing.T) {
	c, f, done := newFake(t)
	defer done()

	_, err := c.Synthesize(context.Background(), Request{Text: "x", RefAudioPath: "r.wav"})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	var sent Request
	for _, r := range f.requests {
		if strings.HasPrefix(r, "POST /tts ") {
			json.Unmarshal([]byte(strings.TrimPrefix(r, "POST /tts ")), &sent) //nolint:errcheck
		}
	}
	if sent.TextLang != "zh" || sent.PromptLang != "zh" {
		t.Errorf("languages defaulted wrong: %+v", sent)
	}
	if sent.SpeedFactor != 1 || sent.Temperature != 1 || sent.TopK != 15 ||
		sent.TopP != 1 || sent.RepetitionPenalty != 1.35 || sent.Seed != -1 {
		t.Errorf("defaults were not applied: %+v", sent)
	}
	if sent.BatchSize != 1 || sent.TextSplitMethod != "cut5" {
		t.Errorf("batch defaults wrong: %+v", sent)
	}
}

// TestEmptyTextIsRefusedBeforeAnyRequest keeps a pointless round trip (and a
// confusing server error) out of the common "empty line" mistake.
func TestEmptyTextIsRefusedBeforeAnyRequest(t *testing.T) {
	c, f, done := newFake(t)
	defer done()

	if _, err := c.Synthesize(context.Background(), Request{Text: "   ", RefAudioPath: "r.wav"}); err == nil {
		t.Error("empty text must be refused")
	}
	if len(f.requests) != 0 {
		t.Errorf("a request was sent anyway: %v", f.requests)
	}
}

// TestMissingRefAudioIsRefused covers the other required field.
func TestMissingRefAudioIsRefused(t *testing.T) {
	c, f, done := newFake(t)
	defer done()

	if _, err := c.Synthesize(context.Background(), Request{Text: "x"}); err == nil {
		t.Error("a missing reference audio path must be refused")
	}
	if len(f.requests) != 0 {
		t.Errorf("a request was sent anyway: %v", f.requests)
	}
}

// TestEngineErrorMessageIsSurfaced is what makes a failure actionable: the engine
// explains itself in the body, and the status code alone would not.
func TestEngineErrorMessageIsSurfaced(t *testing.T) {
	c, f, done := newFake(t)
	defer done()
	f.failSynthesis = "text_lang: ja is not supported in version v2"

	_, err := c.Synthesize(context.Background(), Request{Text: "x", RefAudioPath: "r.wav"})
	if err == nil {
		t.Fatal("a 400 must be reported as an error")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("the engine's own message was lost: %v", err)
	}
}

// TestNonWAVResponseIsRejected covers the engine's silent-failure modes, where it
// answers 200 with something that is not audio. Storing that as a take would
// produce a clip that plays as silence with nothing to explain why.
func TestNonWAVResponseIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>not audio</html>")) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := New(srv.URL).Synthesize(context.Background(), Request{Text: "x", RefAudioPath: "r.wav"})
	if err == nil {
		t.Error("a non-WAV 200 response must be rejected")
	}
}

// TestHealthReportsAnUnreachableServer means the panel can say "not running"
// instead of failing a render with a confusing error.
func TestHealthReportsAnUnreachableServer(t *testing.T) {
	// Port 1 is reserved and nothing listens there.
	if err := New("http://127.0.0.1:1").Health(context.Background()); err == nil {
		t.Error("an unreachable server must fail the health check")
	}
	c, _, done := newFake(t)
	defer done()
	if err := c.Health(context.Background()); err != nil {
		t.Errorf("a running engine must pass the health check: %v", err)
	}
}

// TestConcurrentRendersAreSerialised is the correctness requirement behind the
// render lock: the selected character is server-side state, so two renders for
// different roles must not interleave their weight switches.
func TestConcurrentRendersAreSerialised(t *testing.T) {
	var order []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/set_gpt_weights"):
			name := r.URL.Query().Get("weights_path")
			order = append(order, "select:"+name)
		case r.URL.Path == "/tts" && r.Method == http.MethodPost:
			order = append(order, "synth")
		}
		json.NewEncoder(w).Encode(map[string]string{"message": "success"}) //nolint:errcheck
	}))
	defer srv.Close()

	c := New(srv.URL)
	done := make(chan struct{}, 2)
	for _, role := range []string{"klee", "nahida"} {
		go func(r string) {
			c.Render(context.Background(), RoleRef{GPT: r, SoVITS: r}, Request{Text: "x", RefAudioPath: "r.wav"}) //nolint:errcheck
			done <- struct{}{}
		}(role)
	}
	<-done
	<-done

	// Each selection must be immediately followed by its own synthesis: an
	// interleaving would mean one role's audio was rendered under another's name.
	for i := 0; i+1 < len(order); i += 2 {
		if !strings.HasPrefix(order[i], "select:") || order[i+1] != "synth" {
			t.Fatalf("renders interleaved: %v", order)
		}
	}
}

// TestBaseURLDefault pins the address a fresh install points at.
func TestBaseURLDefault(t *testing.T) {
	if got := New("").BaseURL(); got != DefaultBaseURL {
		t.Errorf("default base URL %q, want %q", got, DefaultBaseURL)
	}
	if got := New("http://127.0.0.1:9881/").BaseURL(); got != "http://127.0.0.1:9881" {
		t.Errorf("trailing slash not trimmed: %q", got)
	}
}

// TestMessageOfHandlesTheEngineShapes covers the response bodies actually seen:
// api_v2's {"message","Exception"} and FastAPI's {"detail"}.
func TestMessageOfHandlesTheEngineShapes(t *testing.T) {
	cases := map[string]string{
		`{"message":"bad"}`:                     "bad",
		`{"message":"bad","Exception":"why"}`:   "bad / why",
		`{"detail":[{"msg":"field required"}]}`: `[{"msg":"field required"}]`,
		`plain text`:                            "plain text",
		``:                                      "（服务未返回内容）",
	}
	for body, want := range cases {
		if got := messageOf([]byte(body)); got != want {
			t.Errorf("messageOf(%q) = %q, want %q", body, got, want)
		}
	}
}
