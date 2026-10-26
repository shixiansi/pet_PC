package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"PetPC/internal/store"
)

// scriptedLLM serves a fixed reply as streamed SSE frames, three runes at a time.
//
// The awkward chunking is deliberate: it exercises the splitter rather than handing
// it whole sentences, which is what the real model does.
func scriptedLLM(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
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
}

// TestSpeechOnHoldsTextUntilItIsSpoken is the user-requested behaviour: with speech
// on, the reply must NOT be streamed to the screen as the model produces it.
//
// The reason is the measured speed difference - synthesis is slower than
// generation, so streaming would show the whole answer seconds before the pet had
// said the first clause, and the two would read as unrelated.
func TestSpeechOnHoldsTextUntilItIsSpoken(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, collector, _ := newSpeechTestApp(t, srv.URL, nil)

	reply := "第一句在这里。第二句在这里。"
	llmSrv := scriptedLLM(t, reply)
	defer llmSrv.Close()
	enableLLM(t, a, llmSrv.URL)
	a.store.UpdateConfig(func(c *store.Config) { c.TTSBaseURL = srv.URL })

	if _, err := a.SendChat("你好"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}

	// Not one delta may be emitted while the reply is being spoken.
	collector.waitEvent(t, eventChatDone, 10*time.Second)
	if collector.has(eventChatDelta) {
		t.Error("delta events were sent with speech on: the text is being shown ahead of the voice")
	}

	// The text must still arrive, via the reveal events.
	ev := collector.waitEvent(t, eventChatReveal, 10*time.Second)
	if txt, _ := ev.data["text"].(string); txt == "" {
		t.Error("the first reveal carried no text")
	}
}

// TestSpeechOffStillStreamsText is the other half of the contract: with speech off
// nothing about the old behaviour may change, because most installs run this way
// and the streaming UI is what makes the pet feel responsive.
func TestSpeechOffStillStreamsText(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, collector, _ := newSpeechTestApp(t, srv.URL, func(c *store.Config) {
		c.TTSEnabled = false
	})

	reply := "第一句在这里。第二句在这里。"
	llmSrv := scriptedLLM(t, reply)
	defer llmSrv.Close()
	enableLLM(t, a, llmSrv.URL)

	if _, err := a.SendChat("你好"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	collector.waitEvent(t, eventChatDone, 10*time.Second)

	if !collector.has(eventChatDelta) {
		t.Error("no delta events with speech off: the reply would not stream")
	}
	if collector.has(eventChatReveal) {
		t.Error("reveal events were sent with speech off, where they mean nothing")
	}
	if synth.count() != 0 {
		t.Errorf("synthesised %d chunks with speech off", synth.count())
	}
}

// TestRevealEndsWithTheWholeReply is the guarantee that holding text back cannot
// lose it. The reply has three sentences but only two are spoken, so the third is
// never revealed by playback - and it must still reach the screen.
func TestRevealEndsWithTheWholeReply(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	// Two sentences may be spoken; the reply has three.
	a, collector, _ := newSpeechTestApp(t, srv.URL, func(c *store.Config) {
		c.TTSMaxSentences = 2
	})

	reply := "第一句在这里。第二句在这里。第三句在这里。"
	llmSrv := scriptedLLM(t, reply)
	defer llmSrv.Close()
	enableLLM(t, a, llmSrv.URL)
	a.store.UpdateConfig(func(c *store.Config) { c.TTSBaseURL = srv.URL })

	if _, err := a.SendChat("你好"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	collector.waitEvent(t, eventChatDone, 10*time.Second)
	collector.waitEvent(t, eventChatReveal, 10*time.Second)

	// The final reveal must carry the complete reply, including the sentence the
	// budget dropped.
	waitFor(t, 15*time.Second, "the final reveal", func() bool {
		for _, ev := range collector.byName(eventChatReveal) {
			if final, _ := ev.data["final"].(bool); final {
				txt, _ := ev.data["text"].(string)
				return strings.Contains(txt, "第三句")
			}
		}
		return false
	})
}

// TestRevealIsPrefixedByWhatWasSpoken pins the alignment itself: every reveal must
// be a prefix of the reply, and what it contains must be text that was actually
// synthesised.
//
// With WholeReply the chat path now reveals ONCE, so the "reveals grow" loop below
// is trivially satisfied on one sample. It is kept because the pipeline still
// supports per-sentence mode (see internal/tts's progress tests, which cover the
// growing-prefix property directly), and because a regression that reintroduced
// per-sentence reveals here would then be checked rather than silently accepted.
func TestRevealIsPrefixedByWhatWasSpoken(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, collector, _ := newSpeechTestApp(t, srv.URL, nil)

	reply := "第一句在这里。第二句在这里。"
	llmSrv := scriptedLLM(t, reply)
	defer llmSrv.Close()
	enableLLM(t, a, llmSrv.URL)
	a.store.UpdateConfig(func(c *store.Config) { c.TTSBaseURL = srv.URL })

	if _, err := a.SendChat("你好"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	collector.waitEvent(t, eventChatDone, 10*time.Second)
	collector.waitEvent(t, eventChatReveal, 10*time.Second)
	// Let the speech drain so every reveal has been sent.
	waitFor(t, 15*time.Second, "all reveals", func() bool {
		for _, ev := range collector.byName(eventChatReveal) {
			if final, _ := ev.data["final"].(bool); final {
				return true
			}
		}
		return false
	})

	reveals := collector.byName(eventChatReveal)
	if len(reveals) == 0 {
		t.Fatal("no reveal events")
	}
	// Every reveal must be a prefix of the reply, and none may shrink - a shrinking
	// one would mean text disappearing from the screen.
	prev := ""
	for i, ev := range reveals {
		txt, _ := ev.data["text"].(string)
		if !strings.HasPrefix(reply, txt) {
			t.Errorf("reveal %d = %q is not a prefix of the reply %q", i, txt, reply)
		}
		if len([]rune(txt)) < len([]rune(prev)) {
			t.Errorf("reveal %d (%q) is shorter than the previous one (%q)", i, txt, prev)
		}
		prev = txt
	}
	// And the whole reply must have been revealed by the end.
	if !strings.Contains(prev, "第二句") {
		t.Errorf("the reply was not fully revealed: last reveal %q", prev)
	}
}

// TestUnspokenTailArrivesWithTheFinalReveal covers the interaction between
// whole-reply speech and the sentence budget.
//
// The budget can leave sentences unspoken - that is its purpose, so a long answer is
// not read out for two minutes. Those sentences are still part of the answer and must
// reach the screen, which happens in the FINAL reveal once the speech is over.
//
// An earlier attempt made the very first reveal carry the whole reply instead, so the
// text would never appear to grow. That is not possible: when the budget runs out the
// pipeline closes its input and starts playing immediately, which can happen while
// the model is still streaming - at that moment the rest of the reply does not exist
// yet. This test pins the behaviour that IS achievable, so a future change does not
// silently drop the tail.
func TestUnspokenTailArrivesWithTheFinalReveal(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	// Two sentences may be spoken; the reply has three.
	a, collector, _ := newSpeechTestApp(t, srv.URL, func(c *store.Config) {
		c.TTSMaxSentences = 2
	})

	reply := "第一句在这里。第二句在这里。第三句在这里。"
	llmSrv := scriptedLLM(t, reply)
	defer llmSrv.Close()
	enableLLM(t, a, llmSrv.URL)
	a.store.UpdateConfig(func(c *store.Config) { c.TTSBaseURL = srv.URL })

	if _, err := a.SendChat("你好"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	collector.waitEvent(t, eventChatDone, 10*time.Second)

	// The final reveal must carry the unspoken sentence, or it would never appear.
	waitFor(t, 15*time.Second, "the final reveal", func() bool {
		for _, ev := range collector.byName(eventChatReveal) {
			if final, _ := ev.data["final"].(bool); final {
				return true
			}
		}
		return false
	})
	var finalText string
	for _, ev := range collector.byName(eventChatReveal) {
		if final, _ := ev.data["final"].(bool); final {
			finalText, _ = ev.data["text"].(string)
		}
	}
	if !strings.Contains(finalText, "第三句") {
		t.Errorf("the final reveal %q omits the sentence the budget left unspoken", finalText)
	}
	if !strings.Contains(finalText, "第一句") {
		t.Errorf("the final reveal %q is missing the reply's opening", finalText)
	}
	// And the spoken part must have been revealed BEFORE the final one, so the text
	// keeps pace with the voice rather than appearing all at the end.
	if len(collector.byName(eventChatReveal)) < 2 {
		t.Error("only the final reveal was sent: the text did not appear with the voice")
	}
}

// TestRevealIsTaggedWithTheRequest covers the guard that keeps a late report from
// landing on the next answer: every reveal carries the id of the round it belongs
// to, and "done" says whether the round is speech-gated at all.
func TestRevealIsTaggedWithTheRequest(t *testing.T) {
	synth := &fakeSynthServer{}
	srv := httptest.NewServer(synth.handler())
	defer srv.Close()

	a, collector, _ := newSpeechTestApp(t, srv.URL, nil)

	reply := "一句话就够了。"
	llmSrv := scriptedLLM(t, reply)
	defer llmSrv.Close()
	enableLLM(t, a, llmSrv.URL)
	a.store.UpdateConfig(func(c *store.Config) { c.TTSBaseURL = srv.URL })

	if _, err := a.SendChat("你好"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}

	done := collector.waitEvent(t, eventChatDone, 10*time.Second)
	// Done must announce that the round is speech-gated, because it can arrive
	// before the first reveal - and the frontend cannot tell "nothing yet" from
	// "speech is off" without this flag.
	if gated, _ := done.data["speechGated"].(bool); !gated {
		t.Error("done did not report speechGated, so the frontend would show the full text too early")
	}

	ev := collector.waitEvent(t, eventChatReveal, 10*time.Second)
	if id, _ := ev.data["requestId"].(string); id == "" {
		t.Error("a reveal event carried no requestId")
	}
}
