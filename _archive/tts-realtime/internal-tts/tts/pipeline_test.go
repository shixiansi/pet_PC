package tts

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// makeWAV builds a minimal valid PCM WAV whose stated length is seconds.
//
// The pipeline reads the duration from the header to decide how long to wait, so
// the tests need real headers rather than opaque bytes.
func makeWAV(seconds float64) []byte {
	const (
		sampleRate = 22050
		channels   = 1
		bits       = 16
	)
	byteRate := sampleRate * channels * bits / 8
	dataLen := int(float64(byteRate) * seconds)

	var b []byte
	le := binary.LittleEndian
	put32 := func(v uint32) {
		var t [4]byte
		le.PutUint32(t[:], v)
		b = append(b, t[:]...)
	}
	put16 := func(v uint16) {
		var t [2]byte
		le.PutUint16(t[:], v)
		b = append(b, t[:]...)
	}

	b = append(b, "RIFF"...)
	put32(uint32(36 + dataLen))
	b = append(b, "WAVE"...)
	b = append(b, "fmt "...)
	put32(16)
	put16(1) // PCM
	put16(channels)
	put32(sampleRate)
	put32(uint32(byteRate))
	put16(channels * bits / 8)
	put16(bits)
	b = append(b, "data"...)
	put32(uint32(dataLen))
	b = append(b, make([]byte, dataLen)...)
	return b
}

// --- test doubles ----------------------------------------------------------

// fakeSpeaker records playback without touching an audio device.
type fakeSpeaker struct {
	mu     sync.Mutex
	played []string
	stops  int
	// onPlay, when set, runs while "playing".
	onPlay func(path string)
	// failWith, when set, makes every play fail.
	failWith error
}

func (f *fakeSpeaker) PlayWAVFile(path string) error {
	f.mu.Lock()
	f.played = append(f.played, path)
	fail := f.failWith
	cb := f.onPlay
	f.mu.Unlock()
	if cb != nil {
		cb(path)
	}
	return fail
}

func (f *fakeSpeaker) StopAudio() {
	f.mu.Lock()
	f.stops++
	f.mu.Unlock()
}

func (f *fakeSpeaker) playedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.played)
}

func (f *fakeSpeaker) stopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stops
}

// fakeSynth synthesises on demand and records what it was asked for.
type fakeSynth struct {
	mu       sync.Mutex
	requests []string
	// delay simulates CPU-bound synthesis.
	delay time.Duration
	// failFor maps text to an error.
	failFor map[string]error
	// audio, when non-nil, is returned for every request.
	audio []byte
	// onRequest, when set, runs as each request is received. Used to observe the
	// order of synthesis against playback, which is what WholeReply is about.
	onRequest func(text string)
}

func (f *fakeSynth) Synthesize(ctx context.Context, _ Settings, text string) ([]byte, error) {
	f.mu.Lock()
	f.requests = append(f.requests, text)
	delay, err := f.delay, f.failFor[text]
	audio := f.audio
	cb := f.onRequest
	f.mu.Unlock()

	if cb != nil {
		cb(text)
	}

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	if audio != nil {
		return audio, nil
	}
	return makeWAV(0.5), nil
}

func (f *fakeSynth) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeSynth) gotRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// noSleep makes playback waits instant while still letting the loop observe
// cancellation.
func noSleep(time.Duration) {}

// newTestPipeline wires a pipeline with instant playback and a temp dir.
//
// It returns the pipeline plus the doubles, so a test can assert on what was
// synthesised and what was played.
func newTestPipeline(t *testing.T, opts Options) (*Pipeline, *fakeSynth, *fakeSpeaker) {
	t.Helper()
	synth := &fakeSynth{}
	if opts.Synth == nil {
		opts.Synth = synth
	}
	spk := &fakeSpeaker{}
	if opts.Speaker == nil {
		opts.Speaker = spk
	}
	opts.Sleep = noSleep
	opts.TempDir = t.TempDir()
	if opts.MaxSentences < 1 {
		opts.MaxSentences = 3
	}
	if opts.ChunkRunes < 1 {
		opts.ChunkRunes = 20
	}
	return NewPipeline(context.Background(), opts), synth, spk
}

// speakAll feeds text, drains it, and returns. The Flush/Wait pair is the normal
// end-of-reply sequence: Flush marks the input complete, Wait lets the queued
// sentences actually play out.
func speakAll(p *Pipeline, text string) {
	p.Add(text)
	p.Flush()
	p.Wait()
}

// --- sentence order and splitting ------------------------------------------

// TestPipelineSpeaksSentencesInOrder is the basic contract: sentences reach the
// speaker in the order they were written, not in completion order.
func TestPipelineSpeaksSentencesInOrder(t *testing.T) {
	p, synth, spk := newTestPipeline(t, Options{})

	speakAll(p, "第一句。第二句。第三句。")
	p.Close()

	got := synth.gotRequests()
	want := []string{"第一句。", "第二句。", "第三句。"}
	if !equalStrings(got, want) {
		t.Fatalf("synthesised %q, want %q", got, want)
	}
	if spk.playedCount() != 3 {
		t.Errorf("played %d clips, want 3", spk.playedCount())
	}
}

// TestPipelineSpeaksWhenChunkArrivesSplitAcrossAdds covers the streaming case end
// to end: a sentence delivered in pieces must still be spoken once, whole.
func TestPipelineSpeaksWhenChunkArrivesSplitAcrossAdds(t *testing.T) {
	p, synth, spk := newTestPipeline(t, Options{})

	for _, piece := range []string{"今天有", "三件事", "到期。", "先做", "哪个？"} {
		p.Add(piece)
	}
	p.Flush()
	p.Wait()
	p.Close()

	got := synth.gotRequests()
	want := []string{"今天有三件事到期。", "先做哪个？"}
	if !equalStrings(got, want) {
		t.Fatalf("synthesised %q, want %q", got, want)
	}
	if spk.playedCount() != 2 {
		t.Errorf("played %d clips, want 2", spk.playedCount())
	}
}

// TestPipelineSpeaksTrailingPartialSentence pins the unconditional flush. A reply
// that does not end in punctuation must still have its tail spoken, or the pet
// stops mid-sentence.
func TestPipelineSpeaksTrailingPartialSentence(t *testing.T) {
	p, synth, _ := newTestPipeline(t, Options{})

	speakAll(p, "这是一句没有结束标点的话")
	p.Close()

	got := synth.gotRequests()
	if len(got) != 1 || got[0] != "这是一句没有结束标点的话" {
		t.Fatalf("synthesised %q, want the unterminated sentence", got)
	}
}

// TestPipelineSkipsUnspeakableChunks covers a model that emits a blank line: a
// punctuation-only chunk must not spend a synthesis request.
func TestPipelineSkipsUnspeakableChunks(t *testing.T) {
	p, synth, spk := newTestPipeline(t, Options{})

	speakAll(p, "第一句。\n\n。。。\n第二句。")
	p.Close()

	for _, r := range synth.gotRequests() {
		if !Speakable(r) {
			t.Errorf("synthesised unspeakable text %q", r)
		}
	}
	if spk.playedCount() != 2 {
		t.Errorf("played %d clips, want 2", spk.playedCount())
	}
}

// TestPipelineSpeakableOnlyTextIsNotSynthesised is a guard against a regression
// where punctuation became a request.
func TestPipelineSpeakableOnlyTextIsNotSynthesised(t *testing.T) {
	p, synth, _ := newTestPipeline(t, Options{})
	speakAll(p, "……！？　\n")
	p.Close()
	if n := synth.requestCount(); n != 0 {
		t.Errorf("synthesised %d requests for punctuation-only text: %q", n, synth.gotRequests())
	}
}

// TestPipelineFlushWithoutAddIsSilent covers an empty or fully-cancelled reply:
// nothing may reach the synth, or a request is spent on silence.
func TestPipelineFlushWithoutAddIsSilent(t *testing.T) {
	p, synth, spk := newTestPipeline(t, Options{})
	p.Flush()
	p.Wait()
	p.Close()

	if synth.requestCount() != 0 {
		t.Errorf("synthesised %q for an empty reply", synth.gotRequests())
	}
	if spk.playedCount() != 0 {
		t.Errorf("played %d clips for an empty reply", spk.playedCount())
	}
}

// --- the latency property --------------------------------------------------

// TestPipelineOverlapsSynthesisWithPlayback is THE property that makes this
// feature viable on a CPU-only machine (docs/genshin-tts.md 5).
//
// It asserts the two workers genuinely run concurrently: while the player is
// waiting out sentence 1, the synthesiser must already have started sentence 2.
// A single-goroutine implementation passes every other test in this file and
// fails only this one - and its symptom in production is that the user waits for
// the whole reply instead of one sentence.
func TestPipelineOverlapsSynthesisWithPlayback(t *testing.T) {
	synth := &fakeSynth{delay: 20 * time.Millisecond}
	spk := &fakeSpeaker{}

	firstPlaying := make(chan struct{})
	var once sync.Once
	spk.onPlay = func(string) { once.Do(func() { close(firstPlaying) }) }

	p := NewPipeline(context.Background(), Options{
		Synth:   synth,
		Speaker: spk,
		// A long playback wait makes the overlap window unambiguous: if synthesis
		// were serialised behind playback, the second request could not start
		// until this wait finished.
		Sleep:        func(time.Duration) { time.Sleep(120 * time.Millisecond) },
		TempDir:      t.TempDir(),
		MaxSentences: 3,
		ChunkRunes:   20,
	})

	p.Add("第一句。第二句。第三句。")
	p.Flush()

	select {
	case <-firstPlaying:
	case <-time.After(3 * time.Second):
		t.Fatal("playback never started")
	}

	// Playback of sentence 1 is underway. For the pipeline to be overlapping, the
	// synthesiser must already have moved on to sentence 2 (or be finished).
	deadline := time.Now().Add(2 * time.Second)
	var n int
	for time.Now().Before(deadline) {
		synth.mu.Lock()
		n = len(synth.requests)
		synth.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n < 2 {
		t.Fatalf("only %d synthesis request(s) while sentence 1 was still playing: "+
			"synthesis is NOT overlapping playback, so the user waits for the whole reply", n)
	}

	p.Close()
}

// TestPipelineAddIsNonBlocking is the property the chat stream depends on: Add
// runs on the request goroutine, so a slow synthesiser must never stall it. This
// is the bug the buffered textCh exists to prevent - an unbuffered or small queue
// makes Add block as soon as synthesis falls behind, which stalls the model stream
// itself.
func TestPipelineAddIsNonBlocking(t *testing.T) {
	synth := &fakeSynth{delay: 200 * time.Millisecond}
	p := NewPipeline(context.Background(), Options{
		Synth:        synth,
		Speaker:      &fakeSpeaker{},
		Sleep:        noSleep,
		TempDir:      t.TempDir(),
		MaxSentences: 50,
		ChunkRunes:   20,
	})
	defer p.Close()

	// Far more sentences than any queue depth, added as fast as possible.
	start := time.Now()
	for i := 0; i < 2000; i++ {
		p.Add("这一句会一直加下去。")
	}
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("Add blocked for %v; it must not wait for synthesis", elapsed)
	}
}

// --- sentence budget -------------------------------------------------------

// TestPipelineCapsSentences guards the setting that keeps a long reply from being
// read out for two minutes.
func TestPipelineCapsSentences(t *testing.T) {
	p, synth, _ := newTestPipeline(t, Options{MaxSentences: 2})

	speakAll(p, "第一句。第二句。第三句。第四句。第五句。")
	p.Close()

	got := synth.gotRequests()
	if len(got) != 2 {
		t.Fatalf("synthesised %d sentences (%q), want 2", len(got), got)
	}
	if !equalStrings(got, []string{"第一句。", "第二句。"}) {
		t.Errorf("got %q, want the first two sentences", got)
	}
}

// TestPipelineTextBeyondBudgetIsDroppedWithoutSynthesis asserts the cap saves work
// rather than just discarding output: sentences past the limit must never be sent
// to the server.
func TestPipelineTextBeyondBudgetIsDroppedWithoutSynthesis(t *testing.T) {
	p, synth, _ := newTestPipeline(t, Options{MaxSentences: 1})

	p.Add("第一句。")
	p.Add("第二句。")
	p.Add("第三句。")
	p.Flush()
	p.Wait()
	p.Close()

	if got := synth.gotRequests(); len(got) != 1 {
		t.Fatalf("synthesised %q, want only the first sentence", got)
	}
}

// TestPipelineLongReplyIsBounded pins the interaction of the two limits: a long
// reply must produce at most MaxSentences requests even though the splitter would
// happily cut it into many more.
func TestPipelineLongReplyIsBounded(t *testing.T) {
	p, synth, _ := newTestPipeline(t, Options{MaxSentences: 3, ChunkRunes: 10})
	for i := 0; i < 50; i++ {
		p.Add("这是第很多句话了。")
	}
	p.Flush()
	p.Wait()
	p.Close()
	if n := synth.requestCount(); n > 3 {
		t.Errorf("synthesised %d sentences, want at most 3", n)
	}
}

// --- interruption ----------------------------------------------------------

// TestPipelineStopIsImmediateAndSilent pins the interruption contract: Stop must
// silence the pet synchronously, not merely schedule it, and must not block.
func TestPipelineStopIsImmediateAndSilent(t *testing.T) {
	synth := &fakeSynth{delay: 20 * time.Millisecond}
	spk := &fakeSpeaker{}
	p := NewPipeline(context.Background(), Options{
		Synth:        synth,
		Speaker:      spk,
		Sleep:        func(time.Duration) { time.Sleep(300 * time.Millisecond) },
		TempDir:      t.TempDir(),
		MaxSentences: 5,
		ChunkRunes:   20,
	})

	p.Add("第一句。第二句。第三句。")
	p.Flush()

	// Let playback begin.
	time.Sleep(150 * time.Millisecond)
	start := time.Now()
	p.Stop()
	stopElapsed := time.Since(start)
	if stopElapsed > 100*time.Millisecond {
		t.Errorf("Stop took %v; it must not wait for the workers", stopElapsed)
	}
	if spk.stopCount() == 0 {
		t.Error("Stop did not tell the speaker to stop")
	}

	// Close must return promptly: the wait loop is sliced so it notices quickly.
	closeStart := time.Now()
	p.Close()
	if closeElapsed := time.Since(closeStart); closeElapsed > time.Second {
		t.Errorf("Close took %v after Stop; the playback wait is not interruptible", closeElapsed)
	}
}

// TestPipelineCloseIsIdempotent guards the several terminal paths that can all
// reach Close (a new reply, shutdown, an error).
func TestPipelineCloseIsIdempotent(t *testing.T) {
	p, _, _ := newTestPipeline(t, Options{})
	p.Add("一句。")
	p.Flush()
	p.Close()
	p.Close()
	p.Close()
}

// TestPipelineContextCancellationFromParent covers the app quitting mid-speech:
// the parent context dies and the workers must exit rather than hold the process
// open.
func TestPipelineContextCancellationFromParent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	synth := &fakeSynth{delay: 100 * time.Millisecond}
	p := NewPipeline(ctx, Options{
		Synth:        synth,
		Speaker:      &fakeSpeaker{},
		Sleep:        func(d time.Duration) { time.Sleep(d) },
		TempDir:      t.TempDir(),
		MaxSentences: 5,
		ChunkRunes:   20,
	})
	p.Add("第一句。第二句。第三句。")
	p.Flush()
	cancel()

	done := make(chan struct{})
	go func() {
		p.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return after the parent context was cancelled")
	}
}

// --- failure handling ------------------------------------------------------

// TestPipelineContinuesAfterSynthesisFailure is the degradation property: one bad
// sentence must not abort the rest of the reply. Speech decorates an answer the
// user already has on screen.
func TestPipelineContinuesAfterSynthesisFailure(t *testing.T) {
	synth := &fakeSynth{failFor: map[string]error{
		"第二句。": errors.New("boom"),
	}}
	spk := &fakeSpeaker{}
	var reported []error
	var mu sync.Mutex

	p := NewPipeline(context.Background(), Options{
		Synth:        synth,
		Speaker:      spk,
		Sleep:        noSleep,
		TempDir:      t.TempDir(),
		MaxSentences: 5,
		ChunkRunes:   20,
		OnError: func(err error) {
			mu.Lock()
			reported = append(reported, err)
			mu.Unlock()
		},
	})

	speakAll(p, "第一句。第二句。第三句。")
	p.Close()

	if spk.playedCount() != 2 {
		t.Errorf("played %d clips, want 2 (the sentences that did not fail)", spk.playedCount())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 {
		t.Errorf("reported %d errors, want 1", len(reported))
	}
}

// TestPipelineReportsCancellationNotAsError is the quiet-failure contract. A stop
// happens constantly (every new reply), so treating it as an error would fill the
// log and make a working feature look broken.
func TestPipelineReportsCancellationNotAsError(t *testing.T) {
	synth := &fakeSynth{delay: 300 * time.Millisecond}
	var mu sync.Mutex
	var reported []error

	p := NewPipeline(context.Background(), Options{
		Synth:        synth,
		Speaker:      &fakeSpeaker{},
		Sleep:        noSleep,
		TempDir:      t.TempDir(),
		MaxSentences: 5,
		ChunkRunes:   20,
		OnError: func(err error) {
			mu.Lock()
			reported = append(reported, err)
			mu.Unlock()
		},
	})

	p.Add("第一句。第二句。")
	go func() {
		time.Sleep(50 * time.Millisecond)
		p.Stop()
	}()
	p.Flush()
	p.Close()

	mu.Lock()
	defer mu.Unlock()
	// A cancellation must never be surfaced. (Other errors would be acceptable
	// here; the assertion is one-directional on purpose.)
	for _, err := range reported {
		t.Errorf("cancellation was reported as an error: %v", err)
	}
}

// TestPipelineSpeakerFailureDoesNotAbort covers an audio device that refuses to
// play (an exclusive-mode app holding it, or no device at all). The reply's other
// sentences are still worth attempting.
func TestPipelineSpeakerFailureDoesNotAbort(t *testing.T) {
	spk := &fakeSpeaker{failWith: errors.New("no device")}
	synth := &fakeSynth{}
	var mu sync.Mutex
	var reported int

	p := NewPipeline(context.Background(), Options{
		Synth:        synth,
		Speaker:      spk,
		Sleep:        noSleep,
		TempDir:      t.TempDir(),
		MaxSentences: 5,
		ChunkRunes:   20,
		OnError: func(error) {
			mu.Lock()
			reported++
			mu.Unlock()
		},
	})
	speakAll(p, "第一句。第二句。")
	p.Close()

	if n := synth.requestCount(); n != 2 {
		t.Errorf("synthesised %d sentences, want 2 despite playback failing", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if reported == 0 {
		t.Error("a playback failure was not reported")
	}
}

// TestPipelineEmptyAudioIsSkipped covers a server that returns a zero-length clip
// for one sentence: it must be skipped rather than played as silence.
func TestPipelineEmptyAudioIsSkipped(t *testing.T) {
	synth := &fakeSynth{audio: []byte{}}
	spk := &fakeSpeaker{}
	p := NewPipeline(context.Background(), Options{
		Synth:        synth,
		Speaker:      spk,
		Sleep:        noSleep,
		TempDir:      t.TempDir(),
		MaxSentences: 3,
		ChunkRunes:   20,
	})
	speakAll(p, "第一句。")
	p.Close()
	if spk.playedCount() != 0 {
		t.Errorf("played %d clips for empty audio", spk.playedCount())
	}
}

// TestPipelineUnmeasurableAudioStillPlays covers a WAV the duration parser cannot
// read. The sentence must still be spoken - a wrong wait is better than silence.
func TestPipelineUnmeasurableAudioStillPlays(t *testing.T) {
	synth := &fakeSynth{audio: []byte("not a wav at all")}
	spk := &fakeSpeaker{}
	p := NewPipeline(context.Background(), Options{
		Synth:        synth,
		Speaker:      spk,
		Sleep:        noSleep,
		TempDir:      t.TempDir(),
		MaxSentences: 3,
		ChunkRunes:   20,
	})
	speakAll(p, "这一句的音频无法测量。")
	p.Close()
	if spk.playedCount() != 1 {
		t.Errorf("played %d clips, want 1 (unmeasurable audio must still play)", spk.playedCount())
	}
}

// TestPipelineNoSynthConfiguredDoesNotPanic covers a nil synthesizer, which the
// app layer can produce when speech is enabled but the client failed to build.
func TestPipelineNoSynthConfiguredDoesNotPanic(t *testing.T) {
	p := NewPipeline(context.Background(), Options{
		Speaker:      &fakeSpeaker{},
		Sleep:        noSleep,
		TempDir:      t.TempDir(),
		MaxSentences: 3,
		ChunkRunes:   20,
	})
	speakAll(p, "第一句。")
	p.Close()
}

// --- cache integration -----------------------------------------------------

// TestPipelineUsesCacheOnRepeat is the payoff of caching: a line the pet says
// twice is synthesised once. The fixed phrases repeat constantly, so this is what
// makes them instant instead of a multi-second wait.
func TestPipelineUsesCacheOnRepeat(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewCache(filepath.Join(dir, "cache"), 50)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	synth := &fakeSynth{audio: makeWAV(0.1)}
	settings := Settings{BaseURL: "http://x", RefAudio: "r.wav", PromptText: "p", Lang: "zh"}

	speak := func() {
		p := NewPipeline(context.Background(), Options{
			Settings:     settings,
			Synth:        synth,
			Speaker:      &fakeSpeaker{},
			Sleep:        noSleep,
			TempDir:      dir,
			Cache:        cache,
			MaxSentences: 2,
			ChunkRunes:   20,
		})
		speakAll(p, "番茄钟结束了。")
		p.Close()
	}

	speak()
	speak()

	if n := synth.requestCount(); n != 1 {
		t.Errorf("synthesised %d times for the same line, want 1 (the second must hit the cache)", n)
	}
}

// TestPipelineCachedAudioIsActuallyPlayed guards a subtle failure: a cache hit
// that skips synthesis must still reach the speaker. Otherwise the second time a
// line is said the pet is silent, which looks exactly like a bug in the cache.
func TestPipelineCachedAudioIsActuallyPlayed(t *testing.T) {
	dir := t.TempDir()
	cache, _ := NewCache(filepath.Join(dir, "cache"), 50)
	synth := &fakeSynth{audio: makeWAV(0.1)}
	settings := Settings{BaseURL: "http://x", RefAudio: "r.wav", PromptText: "p", Lang: "zh"}

	// Prime the cache, then assert synthesis is skipped AND audio still plays.
	if err := cache.Put(settings, "第一句。", makeWAV(0.1)); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	spk := &fakeSpeaker{}
	p := NewPipeline(context.Background(), Options{
		Settings:     settings,
		Synth:        synth,
		Speaker:      spk,
		Sleep:        noSleep,
		TempDir:      dir,
		Cache:        cache,
		MaxSentences: 2,
		ChunkRunes:   20,
	})
	speakAll(p, "第一句。")
	p.Close()

	if n := synth.requestCount(); n != 0 {
		t.Errorf("synthesised %d times on a cache hit, want 0", n)
	}
	if spk.playedCount() != 1 {
		t.Fatalf("played %d clips on a cache hit, want 1", spk.playedCount())
	}
}

// --- temp files ------------------------------------------------------------

// TestPipelineCleansUpTempFiles asserts no scratch clip is left behind, which
// matters because a leak here grows by one file per sentence forever.
func TestPipelineCleansUpTempFiles(t *testing.T) {
	dir := t.TempDir()
	p := NewPipeline(context.Background(), Options{
		Synth:        &fakeSynth{},
		Speaker:      &fakeSpeaker{},
		Sleep:        noSleep,
		TempDir:      dir,
		MaxSentences: 5,
		ChunkRunes:   20,
	})

	speakAll(p, "第一句。第二句。第三句。")
	p.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var leftovers []string
	for _, e := range entries {
		leftovers = append(leftovers, e.Name())
	}
	if len(leftovers) != 0 {
		t.Errorf("scratch files left behind: %v", leftovers)
	}
}

// TestCleanupTempDir removes orphans from a previous crash and leaves everything
// else alone.
func TestCleanupTempDir(t *testing.T) {
	dir := t.TempDir()
	orphan := filepath.Join(dir, tempPrefix+"123.wav")
	other := filepath.Join(dir, "keep-me.wav")
	os.WriteFile(orphan, []byte("x"), 0o644)
	os.WriteFile(other, []byte("x"), 0o644)

	CleanupTempDir(dir)

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("orphaned scratch file survived the sweep")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("the sweep deleted a file that was not a scratch clip")
	}
	// A missing directory must not panic.
	CleanupTempDir(filepath.Join(dir, "does-not-exist"))
	CleanupTempDir("")
}

// TestTempPrefixIsNotAccidentallyMatched guards the startup sweep: it must not
// delete the cache's own entries. Cached files are named by content hash and
// scratch clips by tempPrefix, so this asserts the two schemes cannot collide.
func TestTempPrefixIsNotAccidentallyMatched(t *testing.T) {
	dir := t.TempDir()
	cache, _ := NewCache(dir, 50)
	s := Settings{BaseURL: "b", RefAudio: "r", PromptText: "p", Lang: "zh"}
	cache.Put(s, "一句话。", makeWAV(0.1))

	CleanupTempDir(dir)

	entries, _ := os.ReadDir(dir)
	var found int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), cacheEntrySuffix) {
			found++
		}
	}
	if found != 1 {
		t.Errorf("the sweep deleted a cached entry: %d left, want 1", found)
	}
}

// --- stress ----------------------------------------------------------------

// TestPipelineStressNoDeadlock hammers the producer and consumer together with
// cancellation to shake out a deadlock the simpler tests would miss.
func TestPipelineStressNoDeadlock(t *testing.T) {
	for i := 0; i < 20; i++ {
		synth := &fakeSynth{delay: time.Duration(i%3) * time.Millisecond}
		ctx, cancel := context.WithCancel(context.Background())
		p := NewPipeline(ctx, Options{
			Synth:        synth,
			Speaker:      &fakeSpeaker{},
			Sleep:        noSleep,
			TempDir:      t.TempDir(),
			MaxSentences: 4,
			ChunkRunes:   20,
		})
		done := make(chan struct{})
		go func() {
			for j := 0; j < 20; j++ {
				p.Add("一句话。")
			}
			p.Flush()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatal("Add/Flush deadlocked")
		}
		if i%4 == 0 {
			cancel()
		}
		// Close cancels internally too; cancel() is called on every path so the
		// context is always released.
		p.Close()
		cancel()
	}
}
