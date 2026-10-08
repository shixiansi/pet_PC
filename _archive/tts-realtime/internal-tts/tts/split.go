package tts

import (
	"strings"
	"unicode"
)

// Sentence terminators.
//
// The set is deliberately wider than the ASCII period. A Chinese reply ends
// sentences with 。！？and separates clauses with ，、；：; a splitter that only
// knew about "." would hand the server a whole paragraph as one chunk, which
// defeats the entire point of the pipeline (the first sound would not arrive
// until the whole paragraph had been synthesised).
const (
	// hardTerminators end a sentence outright, and are the ONLY normal split
	// points. Text up to and including one of these is never joined with what
	// follows.
	hardTerminators = "。！？!?…\n"
	// softTerminators are commas and their kin. They are NOT split points.
	//
	// They used to be: a chunk was cut at a comma once it passed its target length,
	// which kept requests short. That was wrong, and the user heard exactly why -
	// "would rather not split at commas, the 5-piece splice sounds odd". A chunk
	// ending at a comma is an INCOMPLETE SENTENCE, and a TTS model given a fragment
	// cannot produce sentence-final prosody, so every clip sounds like it was
	// interrupted. Splicing five of those together sounds like five interrupted
	// fragments no matter how cleanly they are joined.
	//
	// Measured afterwards: the trailing silence at a seam is only 0.07s and the
	// clips are cut at real commas, so nothing was wrong with the JOINING. The
	// problem was the units being joined.
	//
	// They are still recognised, for one narrow purpose: when a single sentence is
	// longer than a backend allows, they mark a better cut than mid-word.
	softTerminators = "，、；：,;:"
	// closers are trailing punctuation that belongs to the sentence it follows
	// rather than starting a new one: "……」。", "好的！」". Splitting between a
	// terminator and its closing quote produces a chunk that is punctuation
	// only, which the server renders as noise or silence.
	closers = "」』】》）)』\"'”’"
)

// Splitter accumulates streamed text and emits speakable chunks.
//
// The reason this is a stateful type rather than a function over a whole string
// is that its input arrives in arbitrary pieces: a language model streams token
// by token, so the caller cannot wait for a complete reply before starting to
// synthesise (that would make the user wait for the entire reply before hearing
// anything - see docs/genshin-tts.md 5). Feeding deltas into a Splitter lets
// synthesis begin as soon as the first sentence is complete.
//
// It is NOT safe for concurrent use. There is exactly one Splitter per reply,
// driven by the single goroutine that owns that request.
type Splitter struct {
	buf strings.Builder
	// target is the soft-split threshold in runes. Clauses shorter than this are
	// allowed to accumulate so that "嗯，好的。" does not become three chunks.
	target int
	// max caps a single chunk when no terminator appears at all. Without it a
	// reply that never punctuates (or a pasted paragraph with no full stops)
	// would be buffered whole and synthesised as one enormous request.
	max int
}

// Bounds on a chunk that contains no terminator at all.
const (
	// chunkMultiple is how many times the target length the buffer may reach
	// before it is cut mid-sentence. Four rather than two because a forced cut is
	// the worst outcome - it can land mid-word and sounds like a stumble - so it
	// is worth waiting for punctuation; low enough that unpunctuated text still
	// starts playing promptly.
	chunkMultiple = 4
	// minMaxChunkRunes floors that bound so a very small target cannot ask for
	// pointlessly short forced cuts.
	minMaxChunkRunes = 24
	// absoluteMaxChunkRunes ceilings it. One chunk becomes one HTTP request with
	// one body, so an unbounded chunk is an unbounded request.
	absoluteMaxChunkRunes = 400
)

// MaxChunkRunes returns the longest chunk a splitter with this target emits when
// the text contains no terminator to cut on.
//
// It is DERIVED from the target rather than being a constant. A fixed bound
// would ignore the setting: a user who shortened the target to 8 runes to get
// faster first audio would still be handed 120-rune forced cuts, which is
// exactly the latency the setting exists to reduce.
func MaxChunkRunes(target int) int {
	if target < 1 {
		target = 1
	}
	n := target * chunkMultiple
	if n < minMaxChunkRunes {
		n = minMaxChunkRunes
	}
	// The ceiling is applied LAST and wins over the floor, because it is the
	// safety property: one chunk is one HTTP request with one body, so there must
	// be no way to ask for an unbounded one. A target larger than the ceiling is
	// degenerate (store.NormaliseTTS caps it well below), and in that case the
	// forced cut simply fires before the comma rule can - bounded and harmless,
	// where the opposite ordering would hand the server a whole document.
	if n > absoluteMaxChunkRunes {
		n = absoluteMaxChunkRunes
	}
	return n
}

// NewSplitter returns a Splitter that aims for target-rune chunks.
//
// A target below 1 is treated as 1, so a caller cannot accidentally ask for
// zero-length chunks (which would spin forever emitting punctuation).
func NewSplitter(target int) *Splitter {
	return NewSplitterCapped(target, 0)
}

// NewSplitterCapped is NewSplitter with an explicit upper bound on one chunk.
//
// maxChunk exists because a backend can refuse long input outright. The online
// service in tools/tts-online answers "输入文字过长！110>100" and returns NO audio,
// so an over-long request is not a degraded sentence but a silent one. Passing the
// service's own limit here makes the cap a property of the SERVER rather than a
// consequence of a UI slider.
//
// It applies whether it is above or below the derived bound. That matters: with the
// target at its default of 20 the derived bound is only 80, which is BELOW the
// online limit of 90, so a "lower only" rule would silently ignore the service's
// limit and let a 200-rune sentence through unrejected. The safety ceiling
// (absoluteMaxChunkRunes) remains the true upper bound on any request body.
//
// A maxChunk of 0 or less means "derive it from target", which is the local
// backend's behaviour: api_v2.py has no comparable limit.
func NewSplitterCapped(target, maxChunk int) *Splitter {
	if target < 1 {
		target = 1
	}
	max := MaxChunkRunes(target)
	if maxChunk > 0 {
		max = maxChunk
	}
	// The absolute ceiling wins over everything, including a caller asking for
	// more: it is what stops an unbounded request body.
	if max > absoluteMaxChunkRunes {
		max = absoluteMaxChunkRunes
	}
	if max < 1 {
		max = 1
	}
	// A target above the cap is meaningless, so it is lowered with it.
	//
	// This is DEFENSIVE rather than load-bearing, and saying so is the point: with
	// the backup rule below in place, a target above the cap produces the same
	// chunks anyway (the cap fires first and backs up to the same punctuation). It
	// is kept because "the comma threshold can never exceed the hard cap" is the
	// invariant a reader will assume, and a later change to the backup threshold
	// would make it load-bearing again.
	if target > max {
		target = max
	}
	return &Splitter{target: target, max: max}
}

// Add feeds streamed text and returns whatever chunks are now complete.
//
// It returns a slice rather than calling a callback so the caller can choose to
// apply its own limits (such as "at most N sentences") between chunks, and so
// the whole thing is trivially testable.
func (s *Splitter) Add(text string) []string {
	if text == "" {
		return nil
	}
	s.buf.WriteString(text)

	var out []string
	for {
		chunk, rest, ok := nextChunk(s.buf.String(), s.target, s.max)
		if !ok {
			break
		}
		s.buf.Reset()
		s.buf.WriteString(rest)
		if chunk != "" {
			out = append(out, chunk)
		}
	}
	return out
}

// Flush returns whatever is left, if it is speakable.
//
// It must be called on every terminal path (done, error, cancel), and it is
// UNCONDITIONAL - it does not go through the terminator search. That is the
// whole point: the tail of a reply that does not end in punctuation ("...所以就
// 先这样吧") has no terminator to find, and routing it through nextChunk would
// report "no complete chunk yet" and drop it silently. The user would watch the
// pet stop mid-sentence with no indication that anything was wrong.
//
// This mirrors llm.Coalescer.Flush, and for the same reason: a threshold that is
// right for the streaming case is wrong for the terminal one.
func (s *Splitter) Flush() []string {
	rest := strings.TrimSpace(s.buf.String())
	s.buf.Reset()
	if rest == "" {
		return nil
	}
	// A long unpunctuated tail is still cut into bounded pieces, because it
	// becomes a request like any other chunk.
	var out []string
	for len([]rune(rest)) > s.max {
		runes := []rune(rest)
		head := strings.TrimSpace(string(runes[:s.max]))
		if head != "" {
			out = append(out, head)
		}
		rest = string(runes[s.max:])
	}
	if tail := strings.TrimSpace(rest); tail != "" {
		out = append(out, tail)
	}
	return out
}

// Pending reports whether anything is buffered.
func (s *Splitter) Pending() bool { return s.buf.Len() > 0 }

// nextChunk finds the first speakable chunk in buf.
//
// It returns the chunk and the unconsumed remainder. ok is false when no
// complete chunk can be cut yet, which is the normal case while a sentence is
// still arriving.
//
// The scan is rune-based rather than byte-based because every terminator here is
// multi-byte in UTF-8; slicing at a byte offset would produce invalid UTF-8 that
// the synthesis server would reject or render as replacement characters.
//
// A chunk ends at a SENTENCE terminator (。！？), or at the cap when a sentence is
// too long to send whole. Commas are NOT split points, and that is the whole
// design of this function - see the comment on softTerminators for what went wrong
// when they were.
func nextChunk(buf string, target, max int) (chunk, rest string, ok bool) {
	if buf == "" {
		return "", "", false
	}
	runes := []rune(buf)

	// Leading whitespace is dropped from the chunk but kept in the buffer's
	// accounting: a chunk of pure whitespace has nothing to say and would be a
	// wasted request.
	start := 0
	for start < len(runes) && unicode.IsSpace(runes[start]) {
		start++
	}

	cut := -1 // index one past the last rune of the chunk
	// lastPause is one past the most recent punctuation of ANY kind, used only by
	// the cap rule below as a better place to cut than mid-word.
	lastPause := -1
	seen := 0
	for i := start; i < len(runes); i++ {
		r := runes[i]
		seen = i - start + 1
		if isHardTerminator(r) {
			// Absorb any closing punctuation that belongs to this sentence.
			j := i + 1
			for j < len(runes) && isCloser(runes[j]) {
				j++
			}
			cut = j
			break
		}
		if isSoftTerminator(r) {
			lastPause = i + 1
		}
		if seen >= max {
			// The sentence is longer than one request may carry, so it has to be
			// cut. Fall back to the last punctuation rather than cutting here: a
			// cut at the cap lands mid-word by construction.
			//
			// This is a LAST RESORT, not a normal path - it only runs when a single
			// sentence exceeds the cap, which the online service's own limit makes
			// possible for a long clause chain. The backup is refused when the
			// punctuation is too early (half the cap) because a chunk of two or
			// three runes is worse than the mid-word cut it avoids.
			//
			// Note that "punctuation" here includes commas, which are NOT normal
			// split points. Inside an over-long sentence a comma is still a better
			// place to break than the middle of a word.
			if lastPause-start >= (max+1)/2 {
				cut = lastPause
			} else {
				cut = i + 1
			}
			break
		}
	}

	if cut < 0 {
		// No terminator yet. The only way to emit something is the max-length
		// valve, which is handled above; otherwise wait for more text.
		return "", buf, false
	}

	chunkText := strings.TrimSpace(string(runes[start:cut]))
	restText := string(runes[cut:])
	// Re-check: a chunk can be whitespace-only after trimming (for example a
	// stray newline), in which case it is reported as complete-but-empty so the
	// caller's loop makes progress instead of rescanning the same buffer.
	if chunkText == "" {
		return "", restText, true
	}
	return chunkText, restText, true
}

// Speakable reports whether text has anything worth synthesising.
//
// Punctuation-only and whitespace-only strings are rejected: the server spends a
// request and a second of compute to produce a click or silence, and the queue
// slot it occupies is one a real sentence could have used.
func Speakable(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func isHardTerminator(r rune) bool { return strings.ContainsRune(hardTerminators, r) }
func isSoftTerminator(r rune) bool { return strings.ContainsRune(softTerminators, r) }
func isCloser(r rune) bool         { return strings.ContainsRune(closers, r) }
