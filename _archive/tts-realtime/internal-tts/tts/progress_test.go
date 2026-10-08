package tts

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// collectProgress returns an OnProgress hook plus a reader for what it saw.
func collectProgress() (func(string), func() []string) {
	var mu sync.Mutex
	var seen []string
	return func(s string) {
			mu.Lock()
			seen = append(seen, s)
			mu.Unlock()
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			out := make([]string, len(seen))
			copy(out, seen)
			return out
		}
}

// TestProgressReportsPrefixInPlaybackOrder covers the reveal contract: the hook
// reports the text whose audio is about to be heard, as a growing prefix.
//
// A prefix rather than an increment is what makes the receiver's job trivial - it
// only ever assigns - so this pins that shape: each report must be a superset of
// the last, and the sequence must end at the whole reply.
func TestProgressReportsPrefixInPlaybackOrder(t *testing.T) {
	onProgress, reports := collectProgress()

	p, _, spk := newTestPipeline(t, Options{
		MaxSentences: 5,
		ChunkRunes:   10,
		OnProgress:   onProgress,
	})
	p.Add("第一句话。第二句话。第三句话。")
	p.Flush()
	p.Wait()

	got := reports()
	if len(got) != 3 {
		t.Fatalf("got %d progress reports (%q), want 3", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if !strings.HasPrefix(got[i], got[i-1]) {
			t.Errorf("report %d (%q) does not extend report %d (%q)", i, got[i], i-1, got[i-1])
		}
	}
	if want := "第一句话。第二句话。第三句话。"; got[len(got)-1] != want {
		t.Errorf("final report = %q, want the whole reply %q", got[len(got)-1], want)
	}
	// Every report must correspond to something that actually played, so the count
	// must match the clips the speaker was handed.
	if n := spk.playedCount(); n != len(got) {
		t.Errorf("speaker played %d clips but %d reports were sent", n, len(got))
	}
}

// TestProgressPrecedesPlayback is the ordering guarantee the whole feature rests
// on: the text is revealed BEFORE its audio starts, so the line the user reads is
// the line being spoken.
//
// If this were reversed the text would trail the voice by one sentence - the same
// misalignment, in the other direction.
func TestProgressPrecedesPlayback(t *testing.T) {
	var mu sync.Mutex
	var order []string
	onProgress := func(s string) {
		mu.Lock()
		order = append(order, "reveal:"+s)
		mu.Unlock()
	}

	p, _, spk := newTestPipeline(t, Options{
		MaxSentences: 3,
		ChunkRunes:   10,
		OnProgress:   onProgress,
		Sleep: func(d time.Duration) {
			// Record that playback was waited out, after the reveal for this clip.
			mu.Lock()
			order = append(order, "played")
			mu.Unlock()
		},
	})
	p.Add("你好呀。今天过得怎么样。")
	p.Flush()
	p.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(order) == 0 {
		t.Fatal("no events recorded")
	}
	// The first event must be a reveal, and the clip count must match.
	if !strings.HasPrefix(order[0], "reveal:") {
		t.Errorf("first event was %q, want a reveal before any playback", order[0])
	}
	if spk.playedCount() != 2 {
		t.Errorf("played %d clips, want 2", spk.playedCount())
	}
}

// TestProgressSkipsUnspokenText covers the reason the prefix is assembled from
// played clips rather than from the splitter's input: text that is never
// synthesised must not appear in the reveal, or the user would read a sentence
// that is never spoken.
//
// The sentence budget is the everyday case - the pet answers at length and only
// the first few sentences are read out.
func TestProgressSkipsUnspokenText(t *testing.T) {
	onProgress, reports := collectProgress()

	p, _, _ := newTestPipeline(t, Options{
		MaxSentences: 2,
		ChunkRunes:   10,
		OnProgress:   onProgress,
	})
	p.Add("第一句。第二句。第三句会被截断。")
	p.Flush()
	p.Wait()

	got := reports()
	if len(got) == 0 {
		t.Fatal("no progress reports")
	}
	final := got[len(got)-1]
	if strings.Contains(final, "第三句") {
		t.Errorf("the revealed text %q includes a sentence that was never spoken", final)
	}
	if !strings.Contains(final, "第一句") {
		t.Errorf("the revealed text %q lost the first sentence", final)
	}
}

// TestProgressNilHookIsSafe pins that the feature is optional: a pipeline built
// without the hook (every existing test, and any caller that does not care) must
// behave exactly as before.
func TestProgressNilHookIsSafe(t *testing.T) {
	p, synth, spk := newTestPipeline(t, Options{MaxSentences: 3, ChunkRunes: 10})
	p.Add("一句话。两句话。")
	p.Flush()
	p.Wait()
	if synth.requestCount() != 2 {
		t.Errorf("synthesised %d chunks, want 2", synth.requestCount())
	}
	if spk.playedCount() != 2 {
		t.Errorf("played %d clips, want 2", spk.playedCount())
	}
}

// TestProgressStopsOnCancel covers a stopped reply. Nothing may panic or block,
// and no report may arrive after the pipeline was cancelled - the app layer has
// already released the text by then.
func TestProgressStopsOnCancel(t *testing.T) {
	onProgress, reports := collectProgress()

	p, _, _ := newTestPipeline(t, Options{
		MaxSentences: 5,
		ChunkRunes:   10,
		OnProgress:   onProgress,
	})
	p.Add("第一句。")
	p.Stop()
	p.Close()

	for _, r := range reports() {
		if strings.Contains(r, "第二句") {
			t.Errorf("revealed %q after the pipeline was stopped", r)
		}
	}
}

// TestChunkCapBoundsAChunk covers the online backend's hard input limit.
//
// The service refuses anything past ~100 characters with NO audio, so a chunk over
// the cap is a sentence the user silently never hears. The derived bound cannot be
// trusted for this because it scales with the target, so the cap has to win.
func TestChunkCapBoundsAChunk(t *testing.T) {
	const cap = 30
	// A target whose derived bound (4x) would far exceed the cap.
	s := NewSplitterCapped(20, cap)

	// Text with no terminator at all, so only the cap can cut it.
	long := strings.Repeat("啊", 200)
	chunks := append(s.Add(long), s.Flush()...)
	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}
	for i, c := range chunks {
		if n := len([]rune(c)); n > cap {
			t.Errorf("chunk %d has %d runes, over the %d cap", i, n, cap)
		}
	}
}

// TestChunkCapIsClampedToTheSafetyCeiling pins that a caller cannot raise the
// absolute ceiling: absoluteMaxChunkRunes is what stops one unbounded request body.
//
// Note what changed and why. The cap used to only ever LOWER the derived bound, so
// a caller asking for more got the smaller of the two. That rule was wrong: at the
// default target of 20 the derived bound is only 80, which is BELOW the online
// service's actual limit of 90 - so the service's own limit was being ignored, and a
// 200-rune sentence could be sent to a server that rejects anything past 100. The
// cap now applies directly, with the safety ceiling as the only thing above it.
func TestChunkCapIsClampedToTheSafetyCeiling(t *testing.T) {
	// A huge cap is clamped to the ceiling, not ignored.
	if s := NewSplitterCapped(20, absoluteMaxChunkRunes*10); s.max != absoluteMaxChunkRunes {
		t.Errorf("max = %d, want the safety ceiling %d", s.max, absoluteMaxChunkRunes)
	}
	// A cap between the derived bound and the ceiling is honoured. This is the case
	// that used to be silently dropped.
	if s := NewSplitterCapped(20, 90); s.max != 90 {
		t.Errorf("max = %d, want the cap 90 even though the derived bound is %d",
			s.max, MaxChunkRunes(20))
	}
	// A cap below the derived bound is honoured too.
	if s := NewSplitterCapped(20, 30); s.max != 30 {
		t.Errorf("max = %d, want the cap 30", s.max)
	}
	// Zero means "derive it", which is the local backend's behaviour.
	if z := NewSplitterCapped(20, 0); z.max != MaxChunkRunes(20) {
		t.Errorf("max = %d with cap 0, want %d", z.max, MaxChunkRunes(20))
	}
}

// TestChunkCapSplitsIntoTwo pins the user-visible effect: a sentence longer than
// the cap is read out as two requests rather than one refused request.
//
// This is the reported symptom - "长句有时候会不全" - so the check is that the text
// arrives in more than one piece AND that no text is lost between them.
func TestChunkCapSplitsIntoTwo(t *testing.T) {
	// 40 runes, one sentence, no interior punctuation to split on.
	text := strings.Repeat("测", 40) + "。"
	s := NewSplitterCapped(20, 24)

	chunks := append(s.Add(text), s.Flush()...)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunk(s) for a %d-rune sentence with a 24 cap: %q",
			len(chunks), len([]rune(text)), chunks)
	}
	var joined strings.Builder
	for _, c := range chunks {
		joined.WriteString(c)
	}
	if joined.String() != text {
		t.Errorf("splitting lost or altered text:\n got %q\nwant %q", joined.String(), text)
	}
	for i, c := range chunks {
		if n := len([]rune(c)); n > 24 {
			t.Errorf("chunk %d has %d runes, over the cap", i, n)
		}
	}
}

// TestPipelineAppliesChunkCap is the wiring check: Options.ChunkCap must reach the
// splitter, because the setting is useless if the pipeline ignores it.
func TestPipelineAppliesChunkCap(t *testing.T) {
	p, synth, _ := newTestPipeline(t, Options{
		MaxSentences: 10,
		ChunkRunes:   20,
		ChunkCap:     24,
	})
	// One long unpunctuated sentence, so only the cap can bound it.
	p.Add(strings.Repeat("好", 100) + "。")
	p.Flush()
	p.Wait()

	texts := synth.gotRequests()
	if len(texts) < 3 {
		t.Fatalf("got %d chunks for 100 runes with a 24 cap: %q", len(texts), texts)
	}
	for i, s := range texts {
		if n := len([]rune(s)); n > 24 {
			t.Errorf("chunk %d = %d runes, over the cap: %q", i, n, s)
		}
	}
}

// TestNewPipelineKeepsZeroCapBehaviour pins that omitting the cap leaves the
// previous behaviour intact for the local backend.
func TestNewPipelineKeepsZeroCapBehaviour(t *testing.T) {
	p := NewPipeline(context.Background(), Options{
		MaxSentences: 1,
		ChunkRunes:   20,
		Speaker:      &fakeSpeaker{},
	})
	defer p.Close()
	if got := p.splitter.max; got != MaxChunkRunes(20) {
		t.Errorf("splitter max = %d, want the derived bound %d", got, MaxChunkRunes(20))
	}
}
