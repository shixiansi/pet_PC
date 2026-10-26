package tts

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// collect feeds text through a splitter in the given pieces and returns every
// chunk, including the flush.
func collect(target int, pieces ...string) []string {
	s := NewSplitter(target)
	var out []string
	for _, p := range pieces {
		out = append(out, s.Add(p)...)
	}
	out = append(out, s.Flush()...)
	return out
}

// TestSplitterSplitsOnFullStop is the basic contract: a multi-sentence reply
// becomes one chunk per sentence, rather than one chunk for the whole thing.
func TestSplitterSplitsOnFullStop(t *testing.T) {
	got := collect(20, "今天有三件事到期。第二件比较急。先做哪个？")
	want := []string{"今天有三件事到期。", "第二件比较急。", "先做哪个？"}
	if !equalStrings(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestSplitterEmitsAsSoonAsSentenceCompletes is the property the whole pipeline
// depends on: the first chunk must come out of Add, not out of Flush. If it did
// not, synthesis could not start until the reply finished, and on a CPU-bound
// server the user would wait for the entire answer before hearing anything
// (docs/genshin-tts.md 5).
func TestSplitterEmitsAsSoonAsSentenceCompletes(t *testing.T) {
	s := NewSplitter(20)
	chunks := s.Add("好的，")
	if len(chunks) != 0 {
		t.Fatalf("a bare comma should not close a chunk yet, got %q", chunks)
	}
	chunks = s.Add("我记下了。")
	if len(chunks) != 1 || chunks[0] != "好的，我记下了。" {
		t.Fatalf("expected the completed sentence to be emitted by Add, got %q", chunks)
	}
}

// TestSplitterHandlesSplitAcrossFeeds is the arbitrary-boundary case: a language
// model streams by token, so a terminator routinely arrives in its own frame and
// a multi-byte character can be split across frames if the transport chunks
// badly. The splitter must be indifferent to where the cuts fall.
func TestSplitterHandlesSplitAcrossFeeds(t *testing.T) {
	full := "第一句。第二句！第三句？"
	// Every possible two-way split of the string must produce the same chunks.
	runes := []rune(full)
	want := []string{"第一句。", "第二句！", "第三句？"}
	for i := 0; i <= len(runes); i++ {
		got := collect(20, string(runes[:i]), string(runes[i:]))
		if !equalStrings(got, want) {
			t.Fatalf("split at rune %d: got %q, want %q", i, got, want)
		}
	}
}

// TestSplitterDoesNotFragmentShortClauses guards the target-length rule: commas
// are only split points once the buffer is already long enough, so an ordinary
// short sentence with a comma in it stays whole.
func TestSplitterDoesNotFragmentShortClauses(t *testing.T) {
	got := collect(20, "嗯，好的。")
	want := []string{"嗯，好的。"}
	if !equalStrings(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestSplitterDoesNotSplitAtCommas is the fix for the "chopped up" sound the user
// reported, and it REPLACES a test that asserted the opposite.
//
// The splitter used to cut at a comma once the buffer passed its target, to keep
// requests short. That made every chunk an incomplete sentence, and a TTS model
// given a fragment cannot produce sentence-final prosody - so every clip sounded
// interrupted, however cleanly the clips were joined. The user's verdict on the two
// side by side was unambiguous: "the 5-piece splice sounds odd, the other is more
// normal".
//
// So a comma must NOT end a chunk while a sentence terminator may still be coming.
// The reply here is one long comma-spliced sentence with no full stop at all, which
// is exactly the case the old rule existed for - it must now stay whole and be
// emitted by Flush.
func TestSplitterDoesNotSplitAtCommas(t *testing.T) {
	long := "这是一个很长的从句，它一直都没有出现句号，"
	got := collect(10, long)
	if len(got) != 1 {
		t.Fatalf("a comma should not end a chunk; got %d chunks: %q", len(got), got)
	}
	if got[0] != long {
		t.Fatalf("chunk = %q, want the whole sentence %q", got[0], long)
	}
}

// TestSplitterSplitsAtFullStopsHoweverShort pins the other direction: every
// sentence terminator ends a chunk, whatever the length. Sentences are the unit the
// voice needs, so a short one must still be sent on its own rather than merged with
// the next for the sake of a target length - merging would put two sentences'
// intonation into one request.
func TestSplitterSplitsAtFullStopsHoweverShort(t *testing.T) {
	got := collect(50, "好。行。可以。")
	want := []string{"好。", "行。", "可以。"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

// TestSplitterBacksUpToPunctuationAtTheCap covers the last-resort path: a single
// sentence longer than the cap.
//
// This is NOT the normal case any more - commas no longer end chunks (see
// TestSplitterDoesNotSplitAtCommas), so this only runs when one sentence is too long
// for the backend to accept in one request. When it does run, the cut must fall at
// the last punctuation rather than at the cap, because a cut at the cap lands
// mid-word by construction.
func TestSplitterBacksUpToPunctuationAtTheCap(t *testing.T) {
	// 13 runes up to and including the comma, then 12 more with no terminator.
	// The cap is 15, so the cut lands two runes past the comma.
	text := "一二三四五六七八九十甲乙，丙丁戊己庚辛壬癸子丑。"
	const cap = 15

	s := NewSplitterCapped(cap, cap)
	chunks := append(s.Add(text), s.Flush()...)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunk(s): %q", len(chunks), chunks)
	}

	first := chunks[0]
	if got := utf8.RuneCountInString(first); got > cap {
		t.Errorf("first chunk is %d runes, over the %d cap", got, cap)
	}
	// The load-bearing assertion: the first chunk ends at the comma, not at the cap.
	if !strings.HasSuffix(first, "，") {
		t.Errorf("first chunk = %q, want it to end at the comma and not mid-word", first)
	}
	if got := utf8.RuneCountInString(first); got != 13 {
		t.Errorf("first chunk is %d runes, want 13 (up to the comma)", got)
	}
	// And nothing may be lost: backing up must not drop the runes it skipped over,
	// they belong to the next chunk.
	if joined := strings.Join(chunks, ""); joined != text {
		t.Errorf("splitting lost text:\n got %q\nwant %q", joined, text)
	}
}

// TestSplitterFallsBackToHardCutWhenPunctuationIsTooEarly is the guard on the backup.
//
// A comma very early in a long run would produce a two-rune request if the backup
// were unconditional, and a run of those is choppier than the mid-word cut it was
// avoiding. So the backup is only taken when it leaves a substantial chunk.
func TestSplitterFallsBackToHardCutWhenPunctuationIsTooEarly(t *testing.T) {
	// Comma at rune 3, then a long unpunctuated run. Under a 20 cap the comma is
	// far too early to back up to.
	text := "好，然后这是一个很长的从句一直都没有出现句号"
	const cap = 20

	s := NewSplitterCapped(cap, cap)
	chunks := append(s.Add(text), s.Flush()...)
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	if got := utf8.RuneCountInString(chunks[0]); got <= 3 {
		t.Errorf("first chunk is only %d runes: the backup was taken from an early comma", got)
	}
	if got := utf8.RuneCountInString(chunks[0]); got > cap {
		t.Errorf("first chunk is %d runes, over the %d cap", got, cap)
	}
	if joined := strings.Join(chunks, ""); joined != text {
		t.Errorf("splitting lost text:\n got %q\nwant %q", joined, text)
	}
}

// TestSplitterMaxChunkBoundsUnpunctuatedText covers the safety valve. Text with
// no terminator at all must still be emitted in bounded pieces, or one request
// would carry an unbounded amount of text.
func TestSplitterMaxChunkBoundsUnpunctuatedText(t *testing.T) {
	const target = 20
	cap := MaxChunkRunes(target)
	long := strings.Repeat("啊", cap*2+7)
	s := NewSplitter(target)
	chunks := s.Add(long)
	if len(chunks) == 0 {
		t.Fatal("unpunctuated text produced no chunks")
	}
	chunks = append(chunks, s.Flush()...)
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > cap {
			t.Errorf("chunk %d has %d runes, over the %d cap", i, n, cap)
		}
	}
	total := 0
	for _, c := range chunks {
		total += utf8.RuneCountInString(c)
	}
	if total != utf8.RuneCountInString(long) {
		t.Fatalf("lost text: %d of %d runes survived", total, utf8.RuneCountInString(long))
	}
}

// TestMaxChunkRunesIsBounded pins the derived bound's shape, including the floor
// and ceiling, because the splitter's behaviour at the extremes depends on it.
func TestMaxChunkRunesIsBounded(t *testing.T) {
	cases := []struct {
		target int
		want   int
	}{
		{1, minMaxChunkRunes},          // tiny target floors at the minimum
		{6, minMaxChunkRunes},          // still under the floor
		{10, 40},                       // 4x the target
		{50, 200},                      // 4x the target
		{200, absoluteMaxChunkRunes},   // hits the ceiling
		{10000, absoluteMaxChunkRunes}, // clamped hard
	}
	for _, c := range cases {
		if got := MaxChunkRunes(c.target); got != c.want {
			t.Errorf("MaxChunkRunes(%d) = %d, want %d", c.target, got, c.want)
		}
	}
}

// TestSplitterKeepsClosingPunctuationWithItsSentence pins the closer rule. A
// split between "。" and "」" would produce a punctuation-only chunk, which the
// server turns into a click or a wasted request.
func TestSplitterKeepsClosingPunctuationWithItsSentence(t *testing.T) {
	got := collect(20, "他说：「今天不行。」然后就走了。")
	for _, c := range got {
		if !Speakable(c) {
			t.Errorf("emitted a chunk with nothing speakable in it: %q", c)
		}
	}
	if !contains(got, "他说：「今天不行。」") {
		t.Fatalf("closing quote was separated from its sentence: %q", got)
	}
}

// TestSplitterDropsWhitespaceOnlyChunks covers a model that emits a blank line
// between paragraphs. A newline is a hard terminator, so without the emptiness
// check it would become its own chunk and spend a request on silence.
func TestSplitterDropsWhitespaceOnlyChunks(t *testing.T) {
	got := collect(20, "第一句。\n\n第二句。")
	want := []string{"第一句。", "第二句。"}
	if !equalStrings(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestSplitterFlushReturnsTail is the unconditional-flush property, mirroring
// llm.Coalescer.Flush: a reply that does not end in punctuation still has a
// tail, and dropping it would leave the pet silently truncating itself.
func TestSplitterFlushReturnsTail(t *testing.T) {
	s := NewSplitter(200)
	if got := s.Add("这是一个没有结束标点的句子"); len(got) != 0 {
		t.Fatalf("expected nothing yet, got %q", got)
	}
	got := s.Flush()
	if len(got) != 1 || got[0] != "这是一个没有结束标点的句子" {
		t.Fatalf("flush returned %q", got)
	}
	if s.Pending() {
		t.Error("flush left text buffered")
	}
	if again := s.Flush(); len(again) != 0 {
		t.Errorf("second flush returned %q, want nothing", again)
	}
}

// TestSplitterFlushOnEmptyIsSilent covers the terminal path of an empty reply
// (a cancelled request that produced nothing). It must not emit an empty chunk,
// because the caller would then spend a synthesis request on it.
func TestSplitterFlushOnEmptyIsSilent(t *testing.T) {
	s := NewSplitter(20)
	if got := s.Flush(); len(got) != 0 {
		t.Fatalf("empty flush returned %q", got)
	}
	if got := s.Add(""); len(got) != 0 {
		t.Fatalf("empty add returned %q", got)
	}
}

// TestSplitterPreservesTextExactly is the invariant that matters most: however
// the chunks are cut, concatenating them must reproduce the input. A splitter
// that drops or duplicates a character would make the pet say something the
// model never wrote.
func TestSplitterPreservesTextExactly(t *testing.T) {
	cases := []string{
		"今天有三件事到期。第二件比较急。先做哪个？",
		"好的，我记下了。",
		"第一句！第二句？第三句。",
		"没有标点的一句话就这样结束",
		"他说：「今天不行。」然后就走了。",
		"Mixed English and 中文。Second sentence here!第三句？",
		strings.Repeat("很长的一段文字，", 20),
	}
	for _, c := range cases {
		for _, target := range []int{6, 10, 20, 200} {
			got := collect(target, c)
			if joined := strings.Join(got, ""); joined != c {
				t.Errorf("target=%d: rejoined %q, want %q", target, joined, c)
			}
			for _, chunk := range got {
				if !Speakable(chunk) {
					t.Errorf("target=%d: unspeakable chunk %q", target, chunk)
				}
			}
		}
	}
}

// TestSplitterChunkBoundariesRespectRunes checks that no chunk can be cut
// mid-character, which would produce invalid UTF-8 for the server to reject.
func TestSplitterChunkBoundariesRespectRunes(t *testing.T) {
	got := collect(6, strings.Repeat("中文测试", 10))
	for _, c := range got {
		if !utf8.ValidString(c) {
			t.Fatalf("invalid UTF-8 chunk: %q", c)
		}
	}
}

// TestSplitterTargetBelowOneIsClamped covers the constructor guard: a caller
// asking for zero-length chunks would otherwise be able to spin the emitter.
func TestSplitterTargetBelowOneIsClamped(t *testing.T) {
	s := NewSplitter(0)
	got := s.Add("嗯。")
	if len(got) == 0 {
		t.Fatal("a clamped splitter produced nothing")
	}
}

// TestSpeakable pins the predicate that keeps punctuation-only text from
// spending a request.
func TestSpeakable(t *testing.T) {
	cases := map[string]bool{
		"你好":    true,
		"a":     true,
		"3":     true,
		"。。。":   false,
		"   ":   false,
		"":      false,
		"……！？":  false,
		"好的 ok": true,
		"\n\t":  false,
		"123":   true,
		"。，、；：": false,
		"a。":    true,
	}
	for in, want := range cases {
		if got := Speakable(in); got != want {
			t.Errorf("Speakable(%q) = %v, want %v", in, got, want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
