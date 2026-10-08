package main

import (
	"strings"
	"testing"

	"PetPC/internal/store"
	"PetPC/internal/tts"
)

// TestOnlineChunksStayWithinTheLimit is the safety contract: on the online backend
// no single request may exceed the service's accepted length.
//
// The limit is a property of the SERVICE, so it must hold whatever 每句字数 is set
// to. The target here is deliberately far above the cap, which is the configuration
// that would otherwise produce over-long requests.
func TestOnlineChunksStayWithinTheLimit(t *testing.T) {
	limit := store.TTSOnlineMaxChunkRunes
	// One long comma-spliced sentence with no full stop until the end, which is the
	// shape that has to be force-split to fit the limit.
	text := strings.Repeat("今天天气不错我们去公园散步顺便买点水果，", 6) + "好。"

	s := tts.NewSplitterCapped(80, limit) // target far above the cap
	chunks := append(s.Add(text), s.Flush()...)

	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}
	for i, c := range chunks {
		if n := len([]rune(c)); n > limit {
			t.Errorf("chunk %d has %d runes, over the %d limit: %q", i, n, limit, c)
		}
	}
	// And nothing may be lost in the process: every character must survive.
	if joined := strings.Join(chunks, ""); joined != text {
		t.Errorf("splitting lost text:\n got %q\nwant %q", joined, text)
	}
}

// TestWholeSentencesAreNotSplit is the fix for the "chopped up" sound, stated at the
// app level where the user experienced it.
//
// A reply made of ordinary sentences must be cut at 。 only, one request per
// sentence, however long those sentences are. Cutting at commas made every chunk an
// incomplete sentence, and the user heard it immediately: "the 5-piece splice sounds
// odd". The commas here are inside the sentences and must NOT break them.
func TestWholeSentencesAreNotSplit(t *testing.T) {
	limit := store.TTSOnlineMaxChunkRunes
	reply := "今天天气不错，我们去公园散步。顺便买点水果，晚上回来做饭。明天要早起，记得定好闹钟。"

	s := tts.NewSplitterCapped(limit, limit)
	chunks := append(s.Add(reply), s.Flush()...)

	want := []string{
		"今天天气不错，我们去公园散步。",
		"顺便买点水果，晚上回来做饭。",
		"明天要早起，记得定好闹钟。",
	}
	if len(chunks) != len(want) {
		t.Fatalf("got %d chunk(s) %q, want %d (one per sentence)", len(chunks), chunks, len(want))
	}
	for i := range want {
		if chunks[i] != want[i] {
			t.Errorf("chunk %d = %q, want %q", i, chunks[i], want[i])
		}
	}
	// The sentences here happen to be under the cap, so every chunk must end at a
	// full stop - never at an inner comma.
	for i, c := range chunks {
		if !strings.HasSuffix(c, "。") {
			t.Errorf("chunk %d does not end at a full stop: %q", i, c)
		}
	}
}

// TestLocalBackendIsUnaffected is the degradation guarantee: the cap is the online
// service's, so the local backend must keep its own bounds. Applying it everywhere
// would multiply the request count for no reason, since api_v2.py has no such limit.
func TestLocalBackendIsUnaffected(t *testing.T) {
	cfg := store.DefaultConfig()
	if got := cfg.TTSChunkCap(); got != 0 {
		t.Errorf("the local backend reports a chunk cap of %d, want none", got)
	}

	online := store.DefaultConfig()
	online.TTSBackend = store.TTSBackendOnline
	if got := online.TTSChunkCap(); got != store.TTSOnlineMaxChunkRunes {
		t.Errorf("the online backend reports a cap of %d, want %d", got, store.TTSOnlineMaxChunkRunes)
	}
}

// TestUnpunctuatedRunIsStillBounded covers text with no terminator at all: the cap
// must still hold, because the service's limit does not care why the text is long.
func TestUnpunctuatedRunIsStillBounded(t *testing.T) {
	limit := store.TTSOnlineMaxChunkRunes
	text := strings.Repeat("好", limit*3) // no terminator anywhere

	s := tts.NewSplitterCapped(limit, limit)
	chunks := append(s.Add(text), s.Flush()...)
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	for i, c := range chunks {
		if n := len([]rune(c)); n > limit {
			t.Errorf("chunk %d = %d runes, over the %d limit", i, n, limit)
		}
	}
	if joined := strings.Join(chunks, ""); joined != text {
		t.Errorf("lost text: %q", joined)
	}
}

// TestShortTextIsNotOverSplit guards the other direction: a reply that already fits
// must go out as ONE request. Splitting it would add a request per fragment for no
// benefit and would break the sentence's prosody across two clips.
func TestShortTextIsNotOverSplit(t *testing.T) {
	limit := store.TTSOnlineMaxChunkRunes
	s := tts.NewSplitterCapped(limit, limit)

	chunks := append(s.Add("好的，我记下了。"), s.Flush()...)
	if len(chunks) != 1 {
		t.Fatalf("a short reply became %d chunks: %q", len(chunks), chunks)
	}
	if chunks[0] != "好的，我记下了。" {
		t.Errorf("chunk = %q", chunks[0])
	}
}

// TestOnlineCapIsTheServicesRealLimit pins the number. It was briefly 15, from a
// misdiagnosis: the chopping the user heard came from cutting at COMMAS, not from
// length. A low cap made it worse by producing more fragments, so the value must not
// drift back down without new evidence.
func TestOnlineCapIsTheServicesRealLimit(t *testing.T) {
	if store.TTSOnlineMaxChunkRunes < 50 {
		t.Errorf("the online cap is %d; a low cap fragments sentences and is what made "+
			"replies sound chopped up", store.TTSOnlineMaxChunkRunes)
	}
	if store.TTSOnlineMaxChunkRunes > 100 {
		t.Errorf("the online cap is %d, above the service's own 100-character refusal",
			store.TTSOnlineMaxChunkRunes)
	}
	// And it must actually be applied, not ignored when it exceeds the derived bound.
	s := tts.NewSplitterCapped(store.DefaultTTSChunkRunes, store.TTSOnlineMaxChunkRunes)
	long := strings.Repeat("好", store.TTSOnlineMaxChunkRunes*2)
	chunks := append(s.Add(long), s.Flush()...)
	for i, c := range chunks {
		if n := len([]rune(c)); n > store.TTSOnlineMaxChunkRunes {
			t.Errorf("chunk %d = %d runes, over the %d cap: the cap was ignored",
				i, n, store.TTSOnlineMaxChunkRunes)
		}
	}
}
