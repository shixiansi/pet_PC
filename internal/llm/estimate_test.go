package llm

// The estimate exists for ONE case: a round where the server reported no usage, so
// these tests pin the two properties that make it usable rather than accurate -
// it is in the documented ballpark, and it never comes out as zero for text that
// really was sent.

import (
	"strings"
	"testing"
)

// TestEstimateTokensIsInTheDocumentedBallpark pins the ratios design.md §4 promises
// for the three kinds of text a plugin actually sends: estimate / real must land in
// the band the design accepts, because a coefficient that drifts out of it would make
// the user's own cap fire at a number he cannot explain.
//
// The "real" baselines below are the design's (0.6 token per Han character, 4 ASCII
// characters per token, ~3 for code/JSON), so a failure here means the ESTIMATOR moved
// - not that the server changed its answer.
func TestEstimateTokensIsInTheDocumentedBallpark(t *testing.T) {
	ratio := func(text string, realPerChar float64) float64 {
		real := float64(len([]rune(text))) * realPerChar
		return float64(EstimateTokens(text)) / real
	}

	// 中文：约 1.6 倍（高估），design §4 accepts 1.0-1.8.
	chinese := strings.Repeat("今天天气很好我们去公园散步吧", 285)
	if got := ratio(chinese, 0.6); got < 1.0 || got > 1.8 {
		t.Errorf("%d 汉字的估算/真实 = %.2f, want 1.0-1.8", len([]rune(chinese)), got)
	}

	// 英文散文：约 1.3 倍（高估），design §4 accepts 0.8-1.5.
	english := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 91)
	if got := ratio(english, 0.25); got < 0.8 || got > 1.5 {
		t.Errorf("%d chars of English estimate/real = %.2f, want 0.8-1.5", len(english), got)
	}

	// 代码 / JSON：约 1.0 倍（贴合）。This is the case that matters, because the
	// replayed tool arguments are JSON.
	code := strings.Repeat(`{"key":"value","n":1},`, 200)
	if got := ratio(code, 1.0/3.0); got < 0.9 || got > 1.2 {
		t.Errorf("%d chars of JSON estimate/real = %.2f, want 0.9-1.2", len(code), got)
	}
}

// TestEstimateTokensOfNothing pins the two boundaries: nothing costs nothing, and a
// single character costs something.
func TestEstimateTokensOfNothing(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Fatalf(`EstimateTokens("") = %d, want 0: an absent message has no token cost`, got)
	}
	if got := EstimateTokens("a"); got != 1 {
		t.Fatalf(`EstimateTokens("a") = %d, want 1`, got)
	}
	if got := EstimateTokens("中"); got != 1 {
		t.Fatalf(`EstimateTokens("中") = %d, want 1`, got)
	}
}

// TestEstimatePromptTokensOfWhitespaceOnlyMessage covers the "空串算 0，但一条消息至少
// 算 overhead" rule: a message whose text carries no information still pays for its
// role and separators, because the provider bills the envelope.
func TestEstimatePromptTokensOfWhitespaceOnlyMessage(t *testing.T) {
	got := EstimatePromptTokens([]Message{{Role: "user", Content: "   "}})
	if want := estimateTokensPerMessage + EstimateTokens("   "); got != want {
		t.Fatalf("a whitespace-only message estimated %d, want %d (overhead + the spaces)", got, want)
	}
	if got < estimateTokensPerMessage {
		t.Fatalf("a message estimated %d, less than the per-message overhead %d", got, estimateTokensPerMessage)
	}
}

// TestEstimatePromptTokensCountsPicturesFlat is the load-bearing one for vision.
//
// A data URI is 100-400 KB of base64 for ONE picture. If the estimate looked at its
// length, a vision plugin would be billed ~100k tokens per screenshot and would hit
// its own daily cap after a single question - a cap that fires on a guess is worse
// than no cap, because the user cannot see why.
func TestEstimatePromptTokensCountsPicturesFlat(t *testing.T) {
	small := Message{Role: "user", Content: "这是什么", Images: []string{"data:image/png;base64,AAAA"}}
	huge := Message{Role: "user", Content: "这是什么", Images: []string{
		"data:image/png;base64," + strings.Repeat("A", 300_000),
	}}
	a := EstimatePromptTokens([]Message{small})
	b := EstimatePromptTokens([]Message{huge})
	if a != b {
		t.Fatalf("one picture estimated %d for a 4-byte URI and %d for a 300 KB one: the estimate is reading the base64", a, b)
	}
	want := estimateTokensPerMessage + EstimateTokens("这是什么") + estimateTokensPerImage
	if a != want {
		t.Fatalf("one picture estimated %d, want %d (overhead + text + one flat image)", a, want)
	}
}

// TestEstimatePromptTokensCountsToolArguments covers the half that is easy to miss:
// the model WROTE the argument JSON, and it travels back inside the assistant turn
// it is replayed in. Ignoring it makes every tool conversation look cheaper.
func TestEstimatePromptTokensCountsToolArguments(t *testing.T) {
	args := `{"text":"` + strings.Repeat("一", 200) + `"}`
	var call ToolCall
	call.Function.Name = "pet_say"
	call.Function.Arguments = args

	withCall := Message{Role: "assistant", ToolCalls: []ToolCall{call}}
	textOnly := Message{Role: "assistant"}

	gotWith := EstimatePromptTokens([]Message{withCall})
	gotWithout := EstimatePromptTokens([]Message{textOnly})
	want := EstimateTokens("pet_say") + EstimateTokens(args)
	if diff := gotWith - gotWithout; diff != want {
		t.Fatalf("a tool call added %d tokens to the estimate, want %d (name + argument string)", diff, want)
	}
	if got := EstimateTokens(args); got < 200 {
		t.Fatalf("the 200-character argument string estimated %d tokens, want at least 200", got)
	}
}

// TestEstimateCompletionTokensIncludesReasoning is the "tokens out=1353、回复 0 字" case
// from the real machine: the whole output was the chain of thought, and an estimate
// that only read Result.Text would record the most expensive answer as zero.
func TestEstimateCompletionTokensIncludesReasoning(t *testing.T) {
	r := Result{Text: "", Reasoning: strings.Repeat("思考", 100)}
	if got := EstimateCompletionTokens(r); got < 200 {
		t.Fatalf("a reply that was all reasoning estimated %d tokens, want at least 200", got)
	}
	if got := EstimateCompletionTokens(Result{}); got != 0 {
		t.Fatalf("an empty result estimated %d tokens, want 0", got)
	}
}
