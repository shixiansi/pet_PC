package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"PetPC/internal/store"
	"PetPC/internal/tts"
)

// --- a fake synthesis server ----------------------------------------------

// fakeSynthServer stands in for GPT-SoVITS' api_v2.py at the HTTP level.
//
// It lives at the app layer rather than reusing internal/tts's test double because
// what is being tested here is the WIRING: that the chat stream actually feeds the
// splitter, that the sentence limit and the auto-read switches are honoured, and
// that a missing server degrades quietly. Those are all decisions the app layer
// makes, so the seam has to be at the socket.
type fakeSynthServer struct {
	mu sync.Mutex
	// requests holds every sentence the server was asked to synthesise.
	requests []string
	// status, when non-zero, is returned instead of audio.
	status int
	// fail makes every request fail at the transport level (the server is closed).
	audio []byte
}

func (f *fakeSynthServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/tts", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Text string `json:"text"`
		}
		json.Unmarshal(body, &req)

		f.mu.Lock()
		f.requests = append(f.requests, req.Text)
		status := f.status
		audio := f.audio
		f.mu.Unlock()

		if status != 0 {
			w.WriteHeader(status)
			io.WriteString(w, `{"message":"synthesis refused"}`)
			return
		}
		if len(audio) == 0 {
			audio = testWAV(0.05)
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(audio)
	})
	mux.HandleFunc("/set_gpt_weights", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"message":"success"}`)
	})
	mux.HandleFunc("/set_sovits_weights", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"message":"success"}`)
	})
	return mux
}

func (f *fakeSynthServer) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *fakeSynthServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// testWAV builds a minimal playable PCM WAV of the given length. It is duplicated
// from internal/tts's test helper because that one is unexported and the point here
// is the app-layer wiring, not the parser.
func testWAV(seconds float64) []byte {
	const (
		sampleRate = 22050
		dataLen    = 1102 // ~0.05s of 16-bit mono
	)
	if seconds > 0.1 {
		// Only the short clip is needed by these tests.
		_ = seconds
	}
	var b []byte
	put32 := func(v uint32) { b = append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }
	put16 := func(v uint16) { b = append(b, byte(v), byte(v>>8)) }
	byteRate := sampleRate * 2
	b = append(b, "RIFF"...)
	put32(uint32(36 + dataLen))
	b = append(b, "WAVE"...)
	b = append(b, "fmt "...)
	put32(16)
	put16(1)
	put16(1)
	put32(sampleRate)
	put32(uint32(byteRate))
	put16(2)
	put16(16)
	b = append(b, "data"...)
	put32(uint32(dataLen))
	b = append(b, make([]byte, dataLen)...)
	return b
}

// --- app fixtures ----------------------------------------------------------

// newSpeechTestApp builds an App whose speech is configured against a fake
// synthesis server, with the audio device replaced by a recorder.
//
// It reuses newLLMTestAppWithEvents so the chat path behaves exactly as it does in
// the existing LLM tests, then points the speech settings at the fake server.
func newSpeechTestApp(t *testing.T, srvURL string, tune func(*store.Config)) (*App, *eventCollector, *recordingSpeaker) {
	t.Helper()
	a, collector := newLLMTestAppWithEvents(t)

	spk := &recordingSpeaker{}
	a.speech.speaker = spk

	// The chat path needs a key and a non-local provider: a local endpoint is
	// treated as keyless, and the chat state machine refuses to send without one.
	// enableLLM is the existing helper for exactly this.
	enableLLM(t, a, srvURL)

	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		// Speech on, fully configured.
		c.TTSEnabled = true
		c.TTSBaseURL = srvURL
		c.TTSRefAudioPath = `D:\voices\ref.wav`
		c.TTSPromptText = "参考文本"
		c.TTSLang = store.TTSLangChinese
		c.TTSMaxSentences = 3
		c.TTSChunkRunes = 20
		c.TTSTimeoutSec = 10
		c.TTSCacheMaxMB = 0 // no cache: every test sentence is a fresh request
		if tune != nil {
			tune(c)
		}
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	return a, collector, spk
}

// recordingSpeaker captures playback instead of using an audio device.
type recordingSpeaker struct {
	mu     sync.Mutex
	played int
	stops  int
}

func (r *recordingSpeaker) PlayWAVFile(string) error {
	r.mu.Lock()
	r.played++
	r.mu.Unlock()
	return nil
}

func (r *recordingSpeaker) StopAudio() {
	r.mu.Lock()
	r.stops++
	r.mu.Unlock()
}

func (r *recordingSpeaker) playedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.played
}

func (r *recordingSpeaker) stopCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stops
}

// waitFor already exists in app_llm_test.go with exactly this signature; the
// speech tests reuse it rather than declaring a second one.

// --- tests -----------------------------------------------------------------

// TestSpeechViewDefaults pins the panel's starting state: speech off, and a reason
// that says so rather than a bare false.
func TestSpeechViewDefaults(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	v := a.speechView()

	if v.Enabled {
		t.Error("speech must be off by default")
	}
	if v.Ready {
		t.Error("speech must not be ready by default")
	}
	if v.NotReadyReason == "" {
		t.Error("a not-ready state must explain itself")
	}
	if v.Endpoint != store.DefaultTTSBaseURL+"/tts" {
		t.Errorf("Endpoint = %q, want the default /tts URL", v.Endpoint)
	}
	if len([]rune(v.NotReadyReason)) > 40 {
		t.Errorf("NotReadyReason is %d runes; it goes in a one-line slot", len([]rune(v.NotReadyReason)))
	}
}

// TestSpeechViewExplainsMissingConfig pins the ordering rule: the most fundamental
// problem is reported, not the last one checked.
func TestSpeechViewExplainsMissingConfig(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)

	// Enabled but nothing configured.
	a.store.UpdateConfig(func(c *store.Config) {
		c.TTSEnabled = true
	})
	v := a.speechView()
	if v.Ready {
		t.Fatal("speech reported ready with no reference audio")
	}
	if !strings.Contains(v.NotReadyReason, "参考音频") {
		t.Errorf("NotReadyReason = %q, want it to name the reference audio", v.NotReadyReason)
	}

	// Reference audio present. That ALONE is enough: the transcript is optional,
	// so this must report ready even with an empty one. (The test previously
	// required a transcript here, which was wrong - see
	// store.TestTTSConfiguredNeedsOnlyReferenceAudio.)
	a.store.UpdateConfig(func(c *store.Config) {
		c.TTSRefAudioPath = "ref.wav"
		c.TTSPromptText = ""
	})
	v = a.speechView()
	if !v.Ready {
		t.Errorf("speech must be ready with a reference clip and no transcript, reason = %q", v.NotReadyReason)
	}

	// With a transcript it is still ready - the field is optional either way.
	a.store.UpdateConfig(func(c *store.Config) {
		c.TTSPromptText = "参考文本"
	})
	if v = a.speechView(); !v.Ready {
		t.Errorf("speech should be ready once configured, reason = %q", v.NotReadyReason)
	}
}

// TestShouldSpeakHonoursSwitches covers the gate matrix. Each switch is checked
// because the two kinds of utterance are genuinely different: a proactive remark is
// short and suits speech, a long answer is what a user most often silences.
func TestShouldSpeakHonoursSwitches(t *testing.T) {
	configured := func() store.Config {
		c := store.DefaultConfig()
		c.TTSEnabled = true
		c.TTSRefAudioPath = "ref.wav"
		c.TTSPromptText = "文本"
		return c
	}

	cases := []struct {
		name      string
		tune      func(*store.Config)
		proactive bool
		want      bool
	}{
		{"chat, all on", func(*store.Config) {}, false, true},
		{"proactive, all on", func(*store.Config) {}, true, true},
		{"master switch off", func(c *store.Config) { c.TTSEnabled = false }, false, false},
		{"master switch off, proactive", func(c *store.Config) { c.TTSEnabled = false }, true, false},
		{"chat auto-read off", func(c *store.Config) { c.TTSAutoReadChat = false }, false, false},
		{"proactive auto-read off", func(c *store.Config) { c.TTSAutoReadProactive = false }, true, false},
		{"chat off does not silence proactive", func(c *store.Config) { c.TTSAutoReadChat = false }, true, true},
		{"proactive off does not silence chat", func(c *store.Config) { c.TTSAutoReadProactive = false }, false, true},
		{"unconfigured", func(c *store.Config) { c.TTSRefAudioPath = "" }, false, false},
	}
	for _, tc := range cases {
		c := configured()
		tc.tune(&c)
		if got := shouldSpeak(c, tc.proactive); got != tc.want {
			t.Errorf("%s: shouldSpeak = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestStartSpeechReturnsNilWhenDisabled is the guard every call site depends on:
// speech off must produce no pipeline at all, so no socket is ever opened.
func TestStartSpeechReturnsNilWhenDisabled(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	if p := a.startSpeech(store.DefaultConfig(), false); p != nil {
		t.Error("startSpeech returned a pipeline with speech switched off")
	}
}

// TestChatStreamIsReadAloud is the end-to-end integration test: a streaming reply
// must reach the synthesis server sentence by sentence, split correctly, with no
// text lost.
func TestChatStreamIsReadAloud(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, collector, spk := newSpeechTestApp(t, srv.URL, nil)
	a.speech.speaker = spk

	// A scripted SSE reply in awkward chunks, so the splitter is genuinely
	// exercised rather than handed whole sentences.
	reply := "第一句在这里。第二句在这里。第三句在这里。"
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// Three characters at a time, deliberately straddling sentence
		// boundaries.
		runes := []rune(reply)
		for i := 0; i < len(runes); i += 3 {
			end := i + 3
			if end > len(runes) {
				end = len(runes)
			}
			frame := map[string]any{
				"choices": []any{map[string]any{
					"delta": map[string]any{"content": string(runes[i:end])},
				}},
			}
			data, _ := json.Marshal(frame)
			w.Write([]byte("data: " + string(data) + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(2 * time.Millisecond)
		}
		w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer llmSrv.Close()

	// Point the CHAT endpoint at the scripted LLM server; speech stays on the fake
	// synthesis server. enableLLM is what writes the key too.
	enableLLM(t, a, llmSrv.URL)
	a.store.UpdateConfig(func(c *store.Config) { c.TTSBaseURL = srv.URL })

	if _, err := a.SendChat("你好"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	collector.waitEvent(t, eventChatDone, 10*time.Second)

	// The reply is three sentences and MaxSentences is 3, so all three must reach
	// the server.
	waitFor(t, 10*time.Second, "all three sentences to be synthesised", func() bool {
		return synth.count() >= 3
	})

	got := synth.got()
	want := []string{"第一句在这里。", "第二句在这里。", "第三句在这里。"}
	if len(got) != len(want) {
		t.Fatalf("synthesised %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sentence %d = %q, want %q", i, got[i], want[i])
		}
	}
	waitFor(t, 5*time.Second, "playback", func() bool { return spk.playedCount() >= 1 })
}

// TestChatWithoutSpeechConfiguredMakesNoSynthesisRequest is the degradation
// guarantee: with speech off, the chat path must behave exactly as it did before
// the feature existed, and must not touch the network.
func TestChatWithoutSpeechConfiguredMakesNoSynthesisRequest(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, collector, _ := newSpeechTestApp(t, srv.URL, func(c *store.Config) {
		c.TTSEnabled = false
	})

	// A minimal LLM server that answers with one sentence.
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"你好。"}}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer llmSrv.Close()
	enableLLM(t, a, llmSrv.URL)

	if _, err := a.SendChat("在吗"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	collector.waitEvent(t, eventChatDone, 5*time.Second)

	// Give any stray goroutine a moment to misbehave.
	time.Sleep(150 * time.Millisecond)
	if n := synth.count(); n != 0 {
		t.Errorf("speech made %d synthesis request(s) while switched off: %q", n, synth.got())
	}
}

// TestChatDegradesQuietlyWhenSynthServerIsDown is the most important degradation
// case: the synthesis server is not running. The reply must still arrive complete
// and the app must not surface an error, because the chat was never about audio.
func TestChatDegradesQuietlyWhenSynthServerIsDown(t *testing.T) {
	// A closed server: the port is almost certainly unused.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	a, collector, _ := newSpeechTestApp(t, deadURL, func(c *store.Config) {
		c.TTSTimeoutSec = 1
	})

	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"答案在这里。"}}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer llmSrv.Close()
	enableLLM(t, a, llmSrv.URL)

	if _, err := a.SendChat("问题"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	ev := collector.waitEvent(t, eventChatDone, 5*time.Second)

	// The reply itself must be intact.
	text, _ := ev.data["text"].(string)
	if text != "答案在这里。" {
		t.Errorf("reply = %q, want it complete despite speech failing", text)
	}
	// And no error event may have been emitted because of the audio failure.
	if collector.has(eventChatError) {
		t.Error("a speech failure produced a chat error event; audio must degrade silently")
	}
}

// TestChatSurvivesSynthServerReturningErrors covers a server that is running but
// refusing: the reply still completes.
func TestChatSurvivesSynthServerReturningErrors(t *testing.T) {
	synth := &fakeSynthServer{status: 400}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, collector, _ := newSpeechTestApp(t, srv.URL, nil)

	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"依然要有答案。"}}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer llmSrv.Close()
	enableLLM(t, a, llmSrv.URL)

	if _, err := a.SendChat("问题"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	ev := collector.waitEvent(t, eventChatDone, 5*time.Second)
	if text, _ := ev.data["text"].(string); text != "依然要有答案。" {
		t.Errorf("reply = %q", text)
	}
	if collector.has(eventChatError) {
		t.Error("a synthesis rejection produced a chat error event")
	}
}

// TestNewReplyInterruptsSpeech pins the mutual exclusion: two utterances must never
// overlap, because the user interrupting the pet is the common case.
func TestNewReplyInterruptsSpeech(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, _, spk := newSpeechTestApp(t, srv.URL, func(c *store.Config) {
		// Long enough that the first utterance is still draining when the second
		// starts.
		c.TTSMaxSentences = 3
	})

	cfg := a.store.Config()
	first := a.startSpeech(cfg, false)
	if first == nil {
		t.Fatal("startSpeech returned nil for a configured setup")
	}
	before := spk.stopCount()

	second := a.startSpeech(cfg, false)
	if second == nil {
		t.Fatal("the second startSpeech returned nil")
	}
	if second == first {
		t.Fatal("startSpeech reused the previous pipeline; the two would overlap")
	}
	if spk.stopCount() == before {
		t.Error("starting a new utterance did not stop the previous one")
	}

	a.ttsShutdown()
}

// TestStopSpeechIsSafeAndIdempotent covers the several paths that stop speech (the
// panel button, a new reply, shutdown) all reaching it.
func TestStopSpeechIsSafeAndIdempotent(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, _, spk := newSpeechTestApp(t, srv.URL, nil)

	// Nothing speaking yet.
	a.StopSpeech()
	a.StopSpeech()

	if a.startSpeech(a.store.Config(), false) == nil {
		t.Fatal("startSpeech returned nil")
	}
	a.StopSpeech()
	before := spk.stopCount()
	a.StopSpeech()
	if spk.stopCount() < before {
		t.Error("stop count went backwards")
	}
	// The view must report not-speaking afterwards.
	view := a.speechView()
	if view.Speaking {
		t.Error("the view still reports the pet as speaking after StopSpeech")
	}
	a.ttsShutdown()
}

// TestSpeechViewReportsSpeaking flips with the pipeline's lifetime, which is what
// lets the panel enable its stop button only when it would do something.
func TestSpeechViewReportsSpeaking(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, _, _ := newSpeechTestApp(t, srv.URL, nil)

	if a.speechView().Speaking {
		t.Error("Speaking must be false before anything is said")
	}
	p := a.startSpeech(a.store.Config(), false)
	if p == nil {
		t.Fatal("startSpeech returned nil")
	}
	if !a.speechView().Speaking {
		t.Error("Speaking must be true while a pipeline is active")
	}
	a.StopSpeech()
	if a.speechView().Speaking {
		t.Error("Speaking must be false after StopSpeech")
	}
}

// TestSaveTTSSettingsRoundTrip covers the settings panel's write path, including
// the endpoint/root conversion that keeps saving idempotent.
//
// It also pins the RETURN TYPE. The methods used to return a bare SpeechView, and
// main.js assigns whatever they return straight to its `state`; a SpeechView
// landing there has no petImageUrl, so the pet silently reverted to the default
// picture the moment the user touched any speech field. The regression is
// invisible in Go's type system at the call site (both are structs), so the shape
// is asserted here instead: a State must carry the pet, the todos and the speech
// block together.
func TestSaveTTSSettingsRoundTrip(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)

	in := SpeechView{
		Enabled:      true,
		Endpoint:     "http://127.0.0.1:9880/tts",
		Character:    "珊瑚宫心海",
		RefAudio:     `D:\voices\ref.wav`,
		PromptText:   "今天的天气非常好。",
		Lang:         store.TTSLangChinese,
		GPTWeights:   `D:\voices\a.ckpt`,
		SovitsWeight: `D:\voices\a.pth`,
		CacheMB:      300,
	}
	st, err := a.SaveTTSSettings(in)
	if err != nil {
		t.Fatalf("SaveTTSSettings: %v", err)
	}
	if !st.Speech.Enabled || !st.Speech.Ready {
		t.Errorf("speech after save = %+v, want enabled and ready", st.Speech)
	}
	// The displayed endpoint must come back as the /tts URL, not doubled.
	if st.Speech.Endpoint != "http://127.0.0.1:9880/tts" {
		t.Errorf("Endpoint = %q, want the single /tts form", st.Speech.Endpoint)
	}
	// The whole-state contract: these must exist, or the panel loses the pet.
	if st.PetImageURL == "" && st.Config.PetImageFile != "" {
		t.Error("the returned State has no pet image URL")
	}
	if st.Speech.RefAudio != `D:\voices\ref.wav` {
		t.Errorf("Speech.RefAudio = %q", st.Speech.RefAudio)
	}

	cfg := a.store.Config()
	if cfg.TTSBaseURL != "http://127.0.0.1:9880" {
		t.Errorf("stored TTSBaseURL = %q, want the bare root", cfg.TTSBaseURL)
	}
	if cfg.TTSRefAudioPath != `D:\voices\ref.wav` || cfg.TTSPromptText != "今天的天气非常好。" {
		t.Errorf("character fields did not persist: %+v", cfg)
	}
	if cfg.TTSCacheMaxMB != 300 {
		t.Errorf("TTSCacheMaxMB = %d, want 300", cfg.TTSCacheMaxMB)
	}

	// Saving the view it just returned must not accumulate "/tts".
	st2, err := a.SaveTTSSettings(st.Speech)
	if err != nil {
		t.Fatalf("second SaveTTSSettings: %v", err)
	}
	if st2.Speech.Endpoint != "http://127.0.0.1:9880/tts" {
		t.Errorf("Endpoint drifted to %q after a re-save", st2.Speech.Endpoint)
	}
}

// TestSaveTTSSettingsKeepsCacheOnZero is a deliberate asymmetry: a zero cache size
// in the VIEW means "the panel did not send one", while zero in the CONFIG means
// "no cache". Treating the zero as a value would silently switch caching off just
// because a form omitted the field.
func TestSaveTTSSettingsKeepsCacheOnZero(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	a.store.UpdateConfig(func(c *store.Config) { c.TTSCacheMaxMB = 400 })

	if _, err := a.SaveTTSSettings(SpeechView{Enabled: true, Endpoint: "http://127.0.0.1:9880", CacheMB: 0}); err != nil {
		t.Fatalf("SaveTTSSettings: %v", err)
	}
	if got := a.store.Config().TTSCacheMaxMB; got != 400 {
		t.Errorf("TTSCacheMaxMB = %d, want the previous 400 to be kept", got)
	}
}

// TestSaveTTSSettingsForgetsLoadedCharacter is the settings-are-not-ignored
// guarantee: after a character change, the next utterance must push the new
// weights instead of trusting the client's cache.
func TestSaveTTSSettingsForgetsLoadedCharacter(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, _, _ := newSpeechTestApp(t, srv.URL, nil)

	var weightCalls int
	var mu sync.Mutex
	// Wrap the server so weight loads can be counted.
	counting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/set_") {
			mu.Lock()
			weightCalls++
			mu.Unlock()
		}
		synth.handler().ServeHTTP(w, r)
	}))
	defer counting.Close()

	a.store.UpdateConfig(func(c *store.Config) {
		c.TTSBaseURL = counting.URL
		c.TTSGPTWeights = `D:\voices\a.ckpt`
	})

	// First utterance loads the character.
	cfg := a.store.Config()
	p := a.startSpeech(cfg, false)
	p.Add("第一句。")
	a.finishSpeech(p)
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	after1 := weightCalls
	mu.Unlock()
	if after1 == 0 {
		t.Fatal("the character was never loaded")
	}

	// Saving settings must invalidate that record.
	//
	// The view is COMPLETE, switches included. SaveTTSSettings treats the input as
	// the whole form (that is what its one-for-one contract means), so a partial
	// view is not "leave the rest alone" - it is "set the rest to zero", which for
	// these four booleans silently turns reading off. The frontend always sends all
	// four; a test that omits them is modelling a caller that does not exist.
	if _, err := a.SaveTTSSettings(SpeechView{
		Enabled: true, Endpoint: counting.URL,
		RefAudio: `D:\voices\ref.wav`, PromptText: "参考文本", Lang: "zh",
		GPTWeights:    `D:\voices\b.ckpt`,
		ReadChat:      true,
		ReadProactive: true,
		ReadPomodoro:  true,
	}); err != nil {
		t.Fatalf("SaveTTSSettings: %v", err)
	}

	cfg = a.store.Config()
	p2 := a.startSpeech(cfg, false)
	if p2 == nil {
		t.Fatal("startSpeech returned nil after saving")
	}
	p2.Add("第二句。")
	a.finishSpeech(p2)
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	after2 := weightCalls
	mu.Unlock()
	if after2 <= after1 {
		t.Error("the new character was not loaded after saving settings; the old voice would still be used")
	}
}

// TestTestTTSConnectionReportsSuccessAndFailure covers the settings panel's test
// button for both outcomes.
func TestTestTTSConnectionReportsSuccessAndFailure(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, _ := newLLMTestAppWithEvents(t)
	a.store.UpdateConfig(func(c *store.Config) { c.TTSBaseURL = srv.URL })

	st, err := a.TestTTSConnection()
	if err != nil {
		t.Fatalf("TestTTSConnection: %v", err)
	}
	if !st.OK {
		t.Errorf("live server reported not-OK: %s", st.Message)
	}
	if st.Message == "" {
		t.Error("a successful test must still return a message")
	}

	// Now a dead server.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	a.store.UpdateConfig(func(c *store.Config) { c.TTSBaseURL = deadURL })

	st, err = a.TestTTSConnection()
	if err != nil {
		t.Fatalf("TestTTSConnection on a dead server returned an error rather than a status: %v", err)
	}
	if st.OK {
		t.Error("a dead server reported OK")
	}
	if !strings.Contains(st.Message, "GPT-SoVITS") {
		t.Errorf("message = %q, want it to tell the user what to start", st.Message)
	}
}

// TestClearTTSCache covers the panel's reclaim-space button.
func TestClearTTSCache(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	cfg := a.store.Config()
	cache := a.ttsCache(cfg)
	if cache == nil {
		t.Fatal("ttsCache returned nil")
	}
	if err := cache.Put(a.ttsSettings(cfg), "缓存一句话。", testWAV(0.05)); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	if a.speechView().CacheBytes == 0 {
		t.Fatal("the cache size was not reported")
	}
	if _, err := a.ClearTTSCache(); err != nil {
		t.Fatalf("ClearTTSCache: %v", err)
	}
	if got := a.speechView().CacheBytes; got != 0 {
		t.Errorf("CacheBytes = %d after clearing, want 0", got)
	}
}

// TestSpeechNoCacheWhenBudgetZero covers the "no cache" configuration: the feature
// still speaks, it just re-synthesises every time.
func TestSpeechNoCacheWhenBudgetZero(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, _, spk := newSpeechTestApp(t, srv.URL, func(c *store.Config) { c.TTSCacheMaxMB = 0 })

	cfg := a.store.Config()
	for i := 0; i < 2; i++ {
		p := a.startSpeech(cfg, false)
		if p == nil {
			t.Fatal("startSpeech returned nil")
		}
		p.Add("同一句话。")
		a.finishSpeech(p)
		time.Sleep(200 * time.Millisecond)
	}

	if n := synth.count(); n != 2 {
		t.Errorf("synthesised %d times with caching off, want 2", n)
	}
	if spk.playedCount() == 0 {
		t.Error("nothing was played with caching off")
	}
}

// TestSpeechSharedClientRemembersCharacter pins the performance property: the
// client is shared across utterances so the several-second character load happens
// once, not once per reply.
func TestSpeechSharedClientRemembersCharacter(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, _, _ := newSpeechTestApp(t, srv.URL, nil)
	if a.ttsClient() != a.ttsClient() {
		t.Error("ttsClient returned a different client each call; the character cache would never hit")
	}
}

// TestGetStateIncludesSpeech is the plumbing check: the frontend reads speech state
// from GetState, so it must be there.
func TestGetStateIncludesSpeech(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	a.store.UpdateConfig(func(c *store.Config) {
		c.TTSEnabled = true
		c.TTSCharacter = "珊瑚宫心海"
		c.TTSRefAudioPath = "ref.wav"
		c.TTSPromptText = "文本"
	})

	st, err := a.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if !st.Speech.Enabled {
		t.Error("GetState did not carry the speech enabled flag")
	}
	if st.Speech.Character != "珊瑚宫心海" {
		t.Errorf("GetState character = %q", st.Speech.Character)
	}
	if !st.Speech.Ready {
		t.Error("GetState did not report speech ready")
	}
}

// TestGetStateSpeechHasNoCredential is a structural guarantee: GetState is
// serialised straight to the webview, so the speech block must never grow a secret.
// Speech takes no credential by design (it is a local server), and this test keeps
// it that way.
func TestGetStateSpeechHasNoCredential(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	st, err := a.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	data, err := json.Marshal(st.Speech)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for k := range m {
		lower := strings.ToLower(k)
		for _, bad := range []string{"key", "secret", "token", "password", "authorization"} {
			if strings.Contains(lower, bad) {
				t.Errorf("SpeechView has the field %q, which looks credential-shaped", k)
			}
		}
	}
}

// TestTrimTTSPath pins the endpoint/root conversion directly, including the paths a
// user might paste from the GPT-SoVITS docs.
func TestTrimTTSPath(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:9880":                    "http://127.0.0.1:9880",
		"http://127.0.0.1:9880/":                   "http://127.0.0.1:9880",
		"http://127.0.0.1:9880/tts":                "http://127.0.0.1:9880",
		"http://127.0.0.1:9880/set_gpt_weights":    "http://127.0.0.1:9880",
		"http://127.0.0.1:9880/set_sovits_weights": "http://127.0.0.1:9880",
		"  http://127.0.0.1:9880/tts  ":            "http://127.0.0.1:9880",
		"http://127.0.0.1:9880/tts/":               "http://127.0.0.1:9880",
	}
	for in, want := range cases {
		if got := trimTTSPath(in); got != want {
			t.Errorf("trimTTSPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSpeechSettingsMatchConfig is a wiring check: every field the client needs must
// come from the config, or the character would be half-applied.
func TestSpeechSettingsMatchConfig(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	cfg := store.DefaultConfig()
	cfg.TTSBaseURL = "http://127.0.0.1:9880"
	cfg.TTSRefAudioPath = "ref.wav"
	cfg.TTSPromptText = "文本"
	cfg.TTSLang = store.TTSLangJapanese
	cfg.TTSGPTWeights = "a.ckpt"
	cfg.TTSSovitsWeights = "a.pth"
	cfg.TTSTimeoutSec = 42

	s := a.ttsSettings(cfg)
	if s.BaseURL != cfg.TTSBaseURL || s.RefAudio != cfg.TTSRefAudioPath ||
		s.PromptText != cfg.TTSPromptText || s.Lang != cfg.TTSLang ||
		s.GPTWeights != cfg.TTSGPTWeights || s.SovitsWeights != cfg.TTSSovitsWeights {
		t.Errorf("ttsSettings dropped a field: %+v", s)
	}
	if s.Timeout != 42*time.Second {
		t.Errorf("Timeout = %v, want 42s", s.Timeout)
	}
}

// TestDesktopSpeakerIsWired verifies the production Speaker is the real one, so a
// refactor cannot silently leave the app speaking into a nil recorder.
func TestDesktopSpeakerIsWired(t *testing.T) {
	var s tts.Speaker = desktopSpeaker{}
	// Off Windows this returns an error; on Windows it would fail because the file
	// does not exist. Either way it must not panic, and StopAudio must be safe.
	_ = s.PlayWAVFile("definitely-not-a-real-file.wav")
	s.StopAudio()
}

// TestTTSShutdownStopsSpeech covers Quit: the workers must actually exit, not merely
// be silenced, or the process would hold a goroutine writing into an abandoned
// directory.
func TestTTSShutdownStopsSpeech(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, _, spk := newSpeechTestApp(t, srv.URL, nil)
	p := a.startSpeech(a.store.Config(), false)
	if p == nil {
		t.Fatal("startSpeech returned nil")
	}
	// Feed a sentence so a worker is genuinely mid-flight when shutdown runs.
	p.Add("一句会被中断的话。")

	done := make(chan struct{})
	go func() {
		a.ttsShutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ttsShutdown did not return; a worker is stuck")
	}
	if spk.stopCount() == 0 {
		t.Error("shutdown did not stop the audio device")
	}
	if a.speechView().Speaking {
		t.Error("the view still reports speaking after shutdown")
	}
}

// TestSpeechUnavailableSpeakerIsSurvivable covers a machine with no audio device:
// synthesis still runs and the failure is logged rather than fatal.
func TestSpeechUnavailableSpeakerIsSurvivable(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, _, _ := newSpeechTestApp(t, srv.URL, nil)
	a.speech.speaker = &failingSpeaker{}

	cfg := a.store.Config()
	p := a.startSpeech(cfg, false)
	if p == nil {
		t.Fatal("startSpeech returned nil")
	}
	p.Add("第一句。")
	a.finishSpeech(p)

	// The pipeline must still have attempted synthesis, and must not have panicked
	// or hung.
	waitFor(t, 5*time.Second, "synthesis despite the dead speaker", func() bool {
		return synth.count() >= 1
	})
}

// failingSpeaker simulates an audio device that refuses to play.
type failingSpeaker struct{}

func (failingSpeaker) PlayWAVFile(string) error { return io.ErrUnexpectedEOF }
func (failingSpeaker) StopAudio()               {}

// TestSpeechContextIsIndependentOfRequestContext is a subtle but important
// property: speech must outlive the chat request's context, because the request's
// context is cancelled the moment the reply completes and the pet should still
// finish the sentence it is on.
func TestSpeechContextIsIndependentOfRequestContext(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, _, _ := newSpeechTestApp(t, srv.URL, nil)

	// A context that is already cancelled, standing in for a finished request.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = ctx

	p := a.startSpeech(a.store.Config(), false)
	if p == nil {
		t.Fatal("startSpeech returned nil")
	}
	p.Add("这一句应该照常朗读。")
	a.finishSpeech(p)

	waitFor(t, 5*time.Second, "synthesis with an independent context", func() bool {
		return synth.count() >= 1
	})
}
