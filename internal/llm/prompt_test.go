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
		Todos: []PromptTodo{
			{Title: "交周报", Due: "已逾期 2 小时", Overdue: true},
			{Title: "买牛奶", Due: "今天 18:00"},
		},
		OpenCount:    2,
		OverdueCount: 1,
		DueSoonCount: 1,
		IncludeTodos: true,
	}
}

func TestBuildSystemPromptIncludesTheWorld(t *testing.T) {
	got := BuildSystemPrompt(testContext())

	for _, want := range []string{
		"你是一只桌宠。",
		"2025-03-07",
		"周五",
		"14:30",
		"2 项未完成",
		"1 项已逾期",
		"交周报",
		"已逾期 2 小时",
		"买牛奶",
		"今天 18:00",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt is missing %q\n---\n%s", want, got)
		}
	}
}

// TestBuildSystemPromptForbidsInvention pins the instruction that keeps the pet
// from making up todos to sound helpful.
func TestBuildSystemPromptForbidsInvention(t *testing.T) {
	got := BuildSystemPrompt(testContext())
	if !strings.Contains(got, "不要编造") {
		t.Errorf("the prompt does not forbid invention:\n%s", got)
	}
	if !strings.Contains(got, "只能依据这些内容回答") {
		t.Errorf("the prompt does not constrain the model to the given facts:\n%s", got)
	}
}

func TestBuildSystemPromptWithoutTodos(t *testing.T) {
	ctx := testContext()
	ctx.Todos = nil
	ctx.OpenCount, ctx.OverdueCount, ctx.DueSoonCount = 0, 0, 0
	got := BuildSystemPrompt(ctx)

	if !strings.Contains(got, "没有未完成的事项") {
		t.Errorf("an empty todo list should be stated explicitly so the pet does not guess:\n%s", got)
	}
	if strings.Contains(got, "共 0 项未完成") {
		t.Errorf("the zero-count form reads oddly:\n%s", got)
	}
}

func TestBuildSystemPromptRespectsContextSwitch(t *testing.T) {
	ctx := testContext()
	ctx.IncludeTodos = false
	got := BuildSystemPrompt(ctx)

	for _, leaked := range []string{"交周报", "买牛奶", "12:34", "把接口文档补完了"} {
		if strings.Contains(got, leaked) {
			t.Errorf("context injection is off but %q is in the prompt:\n%s", leaked, got)
		}
	}
	if !strings.Contains(got, "关闭了日程上下文") {
		t.Errorf("the prompt should say the data is unavailable, not silently omit it:\n%s", got)
	}
	// The clock is not private data, so it stays.
	if !strings.Contains(got, "14:30") {
		t.Errorf("the current time should still be present:\n%s", got)
	}
}

func TestBuildSystemPromptCapsTodoList(t *testing.T) {
	ctx := testContext()
	ctx.Todos = nil
	for i := 0; i < maxPromptTodos+8; i++ {
		ctx.Todos = append(ctx.Todos, PromptTodo{Title: "任务" + string(rune('A'+i))})
	}
	ctx.OpenCount = len(ctx.Todos)
	got := BuildSystemPrompt(ctx)

	// The section says it is truncated...
	if !strings.Contains(got, "只列出前 12 项") {
		t.Errorf("the prompt does not say the list is truncated:\n%s", got)
	}
	// ...and it really is.
	count := strings.Count(got, "任务")
	if count != maxPromptTodos {
		t.Errorf("prompt contains %d todos, want %d", count, maxPromptTodos)
	}
	// The true total is still stated, so "how many do I have" stays correct.
	if !strings.Contains(got, "共 20 项未完成") {
		t.Errorf("the real total was lost when truncating:\n%s", got)
	}
}

func TestBuildSystemPromptSkipsEmptyTitles(t *testing.T) {
	ctx := testContext()
	ctx.Todos = []PromptTodo{{Title: "   "}, {Title: "有内容"}}
	ctx.OpenCount = 2
	got := BuildSystemPrompt(ctx)
	if strings.Count(got, "（") > 1 && strings.Contains(got, "- 待办：共 2 项") {
		// Just assert the blank entry produced no bullet.
		if strings.Contains(got, "  （") {
			t.Errorf("a blank todo title produced a bullet:\n%s", got)
		}
	}
	if !strings.Contains(got, "有内容") {
		t.Errorf("the real todo was dropped:\n%s", got)
	}
}

// The three session-summary tests and the pomodoro-state test that used to follow
// this point described a snapshot the host no longer builds: the recent pomodoro
// summaries and the timer's phase. Both belong to the pomodoro plugin now, so there
// is no summary list to cap, no header to back out and no phase to render.

// TestBuildSystemPromptPomodoroStates used to pin the four ways the timer could
// appear in the snapshot (off, idle, running, with a completed count). The timer is
// the pomodoro plugin's now, so the host's prompt has no pomodoro block at all.
