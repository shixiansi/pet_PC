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
		Pomodoro: PromptPomodoro{
			Enabled:        true,
			Phase:          "专注",
			Remaining:      "12:34",
			TodoTitle:      "交周报",
			CompletedToday: 3,
		},
		RecentSessions: []PromptSession{
			{Title: "写文档", Summary: "把接口文档补完了", When: "今天 11:00"},
		},
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
		"专注",
		"12:34",
		"今天已完成 3 个番茄",
		"写文档",
		"把接口文档补完了",
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

func TestBuildSystemPromptNoSessionHeaderWhenNothingToShow(t *testing.T) {
	ctx := testContext()
	// A summary the user skipped carries no information, so the whole section
	// (header included) must disappear rather than appear empty.
	ctx.RecentSessions = []PromptSession{{Title: "写文档", Summary: "  ", When: "今天 11:00"}}
	got := BuildSystemPrompt(ctx)

	if strings.Contains(got, "最近的番茄总结") {
		t.Errorf("an empty summary left a dangling header:\n%s", got)
	}
}

func TestBuildSystemPromptCapsSessionSummaries(t *testing.T) {
	ctx := testContext()
	ctx.RecentSessions = nil
	for i := 0; i < maxPromptSessions+3; i++ {
		ctx.RecentSessions = append(ctx.RecentSessions, PromptSession{
			Title: "会话", Summary: "总结内容", When: "今天 10:00",
		})
	}
	got := BuildSystemPrompt(ctx)
	if n := strings.Count(got, "总结内容"); n != maxPromptSessions {
		t.Errorf("%d summaries in the prompt, want %d", n, maxPromptSessions)
	}
}

func TestBuildSystemPromptTruncatesLongSummaries(t *testing.T) {
	ctx := testContext()
	long := strings.Repeat("很", 500)
	ctx.RecentSessions = []PromptSession{{Title: "T", Summary: long, When: "今天"}}
	got := BuildSystemPrompt(ctx)

	if strings.Contains(got, long) {
		t.Error("a 500-character summary went into the prompt verbatim")
	}
	if !strings.Contains(got, "…") {
		t.Error("the truncation was not marked")
	}
}

func TestBuildSystemPromptPomodoroStates(t *testing.T) {
	cases := []struct {
		name string
		p    PromptPomodoro
		want string
	}{
		{"off", PromptPomodoro{Enabled: false}, "未开启"},
		{"idle", PromptPomodoro{Enabled: true}, "已开启，当前空闲"},
		{"running", PromptPomodoro{Enabled: true, Phase: "专注", Remaining: "05:00"}, "还剩 05:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := testContext()
			ctx.Pomodoro = c.p
			got := BuildSystemPrompt(ctx)
			if !strings.Contains(got, c.want) {
				t.Errorf("prompt missing %q:\n%s", c.want, got)
			}
		})
	}
}

func TestBuildSystemPromptHandlesZeroTime(t *testing.T) {
	ctx := testContext()
	ctx.Now = time.Time{} // an unset clock must not render as year 1
	got := BuildSystemPrompt(ctx)
	if strings.Contains(got, "0001-01-01") {
		t.Errorf("an unset clock reached the prompt:\n%s", got)
	}
	if !strings.Contains(got, "现在：") {
		t.Errorf("the time line is missing entirely:\n%s", got)
	}
}

func TestBuildSystemPromptWithoutPersona(t *testing.T) {
	ctx := testContext()
	ctx.Persona = "   "
	got := BuildSystemPrompt(ctx)
	if strings.HasPrefix(got, "\n") {
		t.Error("an empty persona left a leading blank line")
	}
	if !strings.Contains(got, "现在：") {
		t.Error("the world snapshot was lost")
	}
}

func TestBuildMessagesOrder(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "第一问"},
		{Role: "assistant", Content: "第一答"},
	}
	msgs := BuildMessages(testContext(), history)

	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("message 0 role = %q, want system", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "2025-03-07") {
		t.Error("the system message is not the assembled prompt")
	}
	if msgs[1].Content != "第一问" || msgs[2].Content != "第一答" {
		t.Errorf("history order changed: %+v", msgs[1:])
	}
}

func TestBuildMessagesOmitsEmptySystem(t *testing.T) {
	// An empty persona and no context still produces the world snapshot, so the
	// system message survives; but with nothing at all it must not be sent as an
	// empty string, which some gateways reject.
	msgs := BuildMessages(PromptContext{IncludeTodos: false}, nil)
	if len(msgs) != 1 || msgs[0].Role != "system" {
		t.Fatalf("expected just a system message, got %+v", msgs)
	}
	if strings.TrimSpace(msgs[0].Content) == "" {
		t.Error("an empty system message was sent")
	}
}

func TestBuildMessagesWithNoHistory(t *testing.T) {
	msgs := BuildMessages(testContext(), nil)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want just the system one", len(msgs))
	}
}
