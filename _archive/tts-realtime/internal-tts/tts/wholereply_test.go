package tts

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestWholeReplyPlaysEverythingAfterSynthesisingEverything is the contract of the
// mode: no clip may reach the speaker until the last one has been synthesised.
//
// That ordering is the entire point. Playing as each sentence finishes makes the
// reply sound like a list - each clip is a separate utterance with its own
// intonation and a synthesis pause before the next - which is what the user
// described as strange. So the assertion is on the ORDER of the two events, not
// merely on the totals.
func TestWholeReplyPlaysEverythingAfterSynthesisingEverything(t *testing.T) {
	var mu sync.Mutex
	var order []string

	synth := &fakeSynth{}
	spk := &fakeSpeaker{}
	spk.onPlay = func(string) {
		mu.Lock()
		order = append(order, "play")
		mu.Unlock()
	}
	synth.onRequest = func(string) {
		mu.Lock()
		order = append(order, "synth")
		mu.Unlock()
	}

	p := NewPipeline(context.Background(), Options{
		MaxSentences: 5,
		ChunkRunes:   10,
		WholeReply:   true,
		Synth:        synth,
		Speaker:      spk,
		Sleep:        noSleep,
	})
	p.Add("第一句话。第二句话。第三句话。")
	p.Flush()
	p.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 6 {
		t.Fatalf("expected 3 synth + 3 play events, got %v", order)
	}
	// All three syntheses must precede the first playback.
	firstPlay := -1
	for i, e := range order {
		if e == "play" {
			firstPlay = i
			break
		}
	}
	if firstPlay < 3 {
		t.Errorf("playback began before every sentence was synthesised: %v", order)
	}
	// And no synthesis may happen after playback starts - that would mean the old
	// interleaved behaviour had come back.
	for i := firstPlay; i < len(order); i++ {
		if order[i] == "synth" {
			t.Errorf("synthesis ran during playback: %v", order)
		}
	}
}

// TestWholeReplyRevealsOnce covers the text side of the same change. One report,
// carrying the whole reply, rather than one per sentence.
func TestWholeReplyRevealsOnce(t *testing.T) {
	onProgress, reports := collectProgress()

	p, _, spk := newTestPipeline(t, Options{
		MaxSentences: 5,
		ChunkRunes:   10,
		WholeReply:   true,
		OnProgress:   onProgress,
	})
	p.Add("第一句话。第二句话。第三句话。")
	p.Flush()
	p.Wait()

	got := reports()
	if len(got) != 1 {
		t.Fatalf("got %d progress reports (%q), want exactly 1", len(got), got)
	}
	if want := "第一句话。第二句话。第三句话。"; got[0] != want {
		t.Errorf("reveal = %q, want the whole reply %q", got[0], want)
	}
	// Every clip must still have been played.
	if n := spk.playedCount(); n != 3 {
		t.Errorf("played %d clips, want 3", n)
	}
}

// TestWholeReplyLosesNothing guards the mode against the obvious way to get it
// wrong: buffering clips and dropping some. The audio played must account for every
// sentence the splitter produced.
func TestWholeReplyLosesNothing(t *testing.T) {
	p, synth, spk := newTestPipeline(t, Options{
		MaxSentences: 10,
		ChunkRunes:   8,
		WholeReply:   true,
	})
	text := "第一句话。第二句话。第三句话。第四句话。"
	p.Add(text)
	p.Flush()
	p.Wait()

	// One clip per synthesised sentence, and one synthesis per chunk.
	if synth.requestCount() != spk.playedCount() {
		t.Errorf("synthesised %d chunks but played %d clips", synth.requestCount(), spk.playedCount())
	}
	// Rejoining the requested text must reproduce the original, so no text was
	// dropped between the splitter and the synthesiser.
	if joined := strings.Join(synth.gotRequests(), ""); joined != text {
		t.Errorf("text lost:\n got %q\nwant %q", joined, text)
	}
}

// TestWholeReplyRevealMatchesWhatIsPlayed is the alignment guarantee, and the
// reason the reveal is built from the CLIPS rather than from the splitter's input:
// a sentence that fails to synthesise is dropped from the audio, so it must not
// appear in the text either.
func TestWholeReplyRevealMatchesWhatIsPlayed(t *testing.T) {
	onProgress, reports := collectProgress()

	synth := &fakeSynth{failFor: map[string]error{"第二句话。": errors.New("boom")}}
	p := NewPipeline(context.Background(), Options{
		MaxSentences: 5,
		ChunkRunes:   10,
		WholeReply:   true,
		OnProgress:   onProgress,
		Synth:        synth,
		Speaker:      &fakeSpeaker{},
		Sleep:        noSleep,
	})
	p.Add("第一句话。第二句话。第三句话。")
	p.Flush()
	p.Wait()

	got := reports()
	if len(got) != 1 {
		t.Fatalf("got %d reports, want 1: %q", len(got), got)
	}
	if strings.Contains(got[0], "第二句话") {
		t.Errorf("the reveal %q includes a sentence that was never synthesised", got[0])
	}
	if !strings.Contains(got[0], "第一句话") || !strings.Contains(got[0], "第三句话") {
		t.Errorf("the reveal %q lost sentences that DID play", got[0])
	}
}

// TestWholeReplyStopsBeforePlayingOnCancel covers a stop that lands during
// synthesis. Playing a partial reply after the user asked it to stop is the one
// outcome worse than silence, so nothing may be played.
func TestWholeReplyStopsBeforePlayingOnCancel(t *testing.T) {
	synth := &fakeSynth{delay: 40 * time.Millisecond}
	spk := &fakeSpeaker{}
	p := NewPipeline(context.Background(), Options{
		MaxSentences: 5,
		ChunkRunes:   10,
		WholeReply:   true,
		Synth:        synth,
		Speaker:      spk,
		Sleep:        noSleep,
	})
	p.Add("第一句话。第二句话。第三句话。")
	p.Flush()

	// Cancel while the synthesiser is still working through the sentences.
	time.Sleep(50 * time.Millisecond)
	p.Stop()
	p.Wait()

	if n := spk.playedCount(); n != 0 {
		t.Errorf("played %d clip(s) after being stopped mid-synthesis", n)
	}
}

// TestPerSentenceModeIsUnchanged is the other half of the contract: the new mode
// must be opt-in. Everything the pipeline did before must still hold when the flag
// is off, because that overlap is what the local backend's latency budget is built
// on.
//
// The synthesiser is given a DELAY, and that is load-bearing rather than realistic
// flavour. With an instant one the synthesis worker fills the whole 3-deep audio
// buffer before the player can pick up the first clip, so even the per-sentence
// path comes out as "synth synth synth play play play" - which made the first
// version of this test fail for a reason that had nothing to do with the mode.
// The delay is what makes the overlap observable at all.
func TestPerSentenceModeIsUnchanged(t *testing.T) {
	var mu sync.Mutex
	var order []string
	synth := &fakeSynth{delay: 15 * time.Millisecond}
	spk := &fakeSpeaker{}
	spk.onPlay = func(string) {
		mu.Lock()
		order = append(order, "play")
		mu.Unlock()
	}
	synth.onRequest = func(string) {
		mu.Lock()
		order = append(order, "synth")
		mu.Unlock()
	}

	p := NewPipeline(context.Background(), Options{
		MaxSentences: 5,
		ChunkRunes:   10,
		Synth:        synth,
		Speaker:      spk,
		Sleep:        noSleep,
	})
	p.Add("第一句话。第二句话。第三句话。")
	p.Flush()
	p.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 6 {
		t.Fatalf("expected 6 events, got %v", order)
	}
	// Interleaved: a playback must appear before the LAST synthesis. Asserting on
	// the last rather than the first keeps this robust to the player starting a
	// moment late, while still failing outright if the worker ran to completion
	// before anything played.
	lastSynth := -1
	for i, e := range order {
		if e == "synth" {
			lastSynth = i
		}
	}
	firstPlay := -1
	for i, e := range order {
		if e == "play" {
			firstPlay = i
			break
		}
	}
	if firstPlay < 0 || firstPlay > lastSynth {
		t.Errorf("per-sentence mode no longer overlaps synthesis with playback: %v", order)
	}
}

// TestWholeReplyEmptyIsHarmless covers the degenerate inputs, which are ordinary
// here: a reply that is only punctuation, or an immediate stop.
func TestWholeReplyEmptyIsHarmless(t *testing.T) {
	onProgress, reports := collectProgress()
	p, _, spk := newTestPipeline(t, Options{
		MaxSentences: 3,
		ChunkRunes:   10,
		WholeReply:   true,
		OnProgress:   onProgress,
	})
	p.Add("。。。")
	p.Flush()
	p.Wait()

	if n := spk.playedCount(); n != 0 {
		t.Errorf("played %d clip(s) for punctuation-only input", n)
	}
	for _, r := range reports() {
		if r != "" {
			t.Errorf("revealed %q for punctuation-only input", r)
		}
	}
}
