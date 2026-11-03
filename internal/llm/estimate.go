package llm

// Token accounting for the rounds where the SERVER reported nothing.
//
// # Why this exists at all
//
// A gateway that omits `usage` (some proxies and self-hosted front ends do, and one
// branch of the client actively drops `stream_options` when an endpoint rejects it)
// used to make a whole round count as zero tokens. The plugin's daily usage counter
// then read "12 requests, 0 tokens", which is worse than an estimate: it looks like
// a measurement.
//
// # Why it is an estimate and not a tokenizer
//
// A real BPE table changes per MODEL, not per endpoint - two models behind one base
// URL disagree by 20% - so "精确到个位" would need a vocabulary that ships and goes
// stale with upstream. Nothing in the host needs that precision: the counter answers
// "which plugin is burning money" (a RANKING) and "has this crossed the user's own
// line" (a RULER). When precision matters the server reports it, and then this file
// is not consulted at all.
//
// # The coefficients are deliberately HIGH
//
// An underestimated cap silently stops working; an overestimated one only makes the
// user see "今日用完了" slightly early, on a number he chose himself. Every constant
// below therefore rounds in the expensive direction, and the measured ratios are
// documented on each one.
const (
	// estimateTokensPerCJK is tokens per Han character / fullwidth punctuation.
	//
	// Real value is about 0.6 (one character is often one token, sometimes less);
	// counting 1.0 overestimates Chinese text by roughly 1.6x, which is the
	// deliberate direction. There is no fractional constant because a token count is
	// an integer everywhere it is used.
	estimateTokensPerCJK = 1

	// estimateCharsPerOther is how many Latin/ASCII characters make one token.
	//
	// English prose is really ~4 chars/token, code and JSON ~2.5-3. Taking 3 makes
	// prose ~1.3x high and code/JSON about right - code is the case that matters,
	// because tool arguments and tool results are JSON.
	estimateCharsPerOther = 3

	// estimateTokensPerMessage is the per-message overhead: the role, the separators
	// and the end-of-turn marker. Every chat format bills these, and a conversation
	// of 20 short turns costs more than 20 short strings.
	estimateTokensPerMessage = 4

	// estimateTokensPerImage is FLAT, and that is the whole point.
	//
	// A `data:image/…;base64,…` URI is 100-400 KB of text for one picture. Counting
	// its characters would bill ~100k tokens for something a vision model charges
	// about a thousand for, and a vision plugin would hit its own daily cap after one
	// screenshot. So one picture counts as one flat number, whatever its length.
	//
	// 1024 is a mid-range tile cost and is the noisiest number here: the real value
	// depends on the model's resolution tier (0.3x-3x). That is why the manual says a
	// request carrying pictures is a MAGNITUDE, not a bill.
	estimateTokensPerImage = 1024

	// estimateMinTokens is the floor for any non-empty text: one character is never
	// free. The ceiling division below already guarantees it today; it is stated as a
	// constant so a future change to estimateCharsPerOther cannot silently make short
	// text cost zero.
	estimateMinTokens = 1
)

// EstimateTokens estimates how many tokens a piece of text costs.
//
// Chinese (and Japanese/Korean Han) characters, fullwidth punctuation and kana count
// as one token each; everything else counts as one token per 3 characters, rounding
// up. An empty string is 0 - "no message" is not a message.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	var cjk, other int
	for _, r := range text {
		if isCJKRune(r) {
			cjk++
		} else {
			other++
		}
	}
	n := cjk * estimateTokensPerCJK
	n += other / estimateCharsPerOther
	if other%estimateCharsPerOther != 0 {
		n++ // 不足则进一: a partial group of characters still crosses a token boundary
	}
	if n < estimateMinTokens {
		n = estimateMinTokens
	}
	return n
}

// isCJKRune reports whether r is a character that is billed roughly one-per-token.
//
// The ranges are the ones that actually appear in Chinese and Japanese text, not the
// whole CJK blocks of Unicode: an emoji or a Cyrillic letter is billed like Latin
// text (several characters per token possibly) and moving them into the 1:1 bucket
// would overestimate them by 3x for no reason.
func isCJKRune(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303F: // CJK punctuation: 、。「」《》
		return true
	case r >= 0x3040 && r <= 0x30FF: // hiragana + katakana
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK unified ideographs extension A
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // CJK unified ideographs
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK compatibility ideographs
		return true
	case r >= 0xFF00 && r <= 0xFFEF: // fullwidth/halfwidth forms: ，！？（）：；
		return true
	}
	return false
}

// EstimatePromptTokens estimates the INPUT side of one round.
//
// It is called with the message list the round actually sends, which is not "the
// user's question": a system prompt, the replayed history, and from round 2 onward
// the assistant's tool calls plus the tool results are all in there. That is the
// point - a four-round tool conversation really does cost four inputs, and that is
// what makes it expensive.
//
// The three per-message parts:
//
//   - overhead (every message pays it);
//   - the text;
//   - each picture at the flat per-image cost, and each tool call by its function
//     name plus its JSON argument string. Leaving the arguments out would make every
//     tool conversation look cheaper than it is, because the model wrote that JSON
//     and it travels back in the assistant turn.
func EstimatePromptTokens(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		n := estimateTokensPerMessage + EstimateTokens(m.Content)
		n += len(m.Images) * estimateTokensPerImage
		for _, call := range m.ToolCalls {
			n += EstimateTokens(call.Function.Name) + EstimateTokens(call.Function.Arguments)
		}
		total += n
	}
	return total
}

// EstimateCompletionTokens estimates the OUTPUT side of one round.
//
// Reasoning is included on purpose: thinking models bill the chain of thought against
// the same budget, and the real machine produced a round that reported `tokens out=1353`
// with a reply of ZERO characters - it was all reasoning. Counting only Result.Text
// would record the most expensive kind of answer as nothing.
func EstimateCompletionTokens(r Result) int {
	return EstimateTokens(r.Text) + EstimateTokens(r.Reasoning)
}
