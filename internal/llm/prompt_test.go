package llm

import (
	"strings"
	"testing"
	"time"
)

func testContext() PromptContext {
	now := time.Date(2025, 3, 7, 14, 30, 0, 0, time.Local) // a Friday
	return PromptContext{
		Persona: "你是一只桌宠。",
		Now:     now,
	}
}

func TestBuildSystemPromptIncludesTheWorld(t *testing.T) {
	got := BuildSystemPrompt(testContext())

	for _, want := range []string{
		"你是一只桌宠。",
		"2025-03-07",
		"周五",
		"14:30",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt is missing %q\n---\n%s", want, got)
		}
	}
}

// TestBuildSystemPromptForbidsInvention pins the instruction that keeps the pet from
// making things up to sound helpful - and the sentence that says the todo list and the
// timer are NOT here. The second half matters as much as the first: the data used to be
// in the snapshot, so a model may still be asked about it, and "I cannot see that" is the
// answer that must be available to it.
func TestBuildSystemPromptForbidsInvention(t *testing.T) {
	got := BuildSystemPrompt(testContext())
	for _, want := range []string{
		"不要编造",
		"只能依据这些内容回答",
		"待办与番茄钟",
		"被问到就说自己看不到",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt is missing %q:\n%s", want, got)
		}
	}
}

// TestBuildSystemPromptOmitsPersonaWhenEmpty covers the one branch that changes shape:
// with no persona the prompt must still be a usable system message, and it must not lead
// with the blank lines the persona block would have left behind.
func TestBuildSystemPromptOmitsPersonaWhenEmpty(t *testing.T) {
	ctx := testContext()
	ctx.Persona = "   "
	got := BuildSystemPrompt(ctx)

	if strings.HasPrefix(got, "\n") {
		t.Errorf("prompt starts with a blank line:\n%q", got)
	}
	if !strings.Contains(got, "以下是当前的真实情况") {
		t.Errorf("the snapshot line is missing:\n%s", got)
	}
}

// The todo-list and session-summary tests that used to follow this point are gone with
// the data they described. `PromptTodo`, `MaxPromptTodos`, the truncation cap and the
// "用户关闭了日程上下文" switch all existed to render a snapshot the host can no longer
// build: the list is the todo plugin's own storage, the timer and its archive are the
// pomodoro plugin's, and neither is readable from here. What replaced them is the single
// sentence asserted above - see PromptContext's comment.
