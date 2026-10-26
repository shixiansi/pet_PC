package llm

import (
	"fmt"
	"strings"
	"time"
)

// PromptContext is the snapshot of the pet's world that goes into the system
// message.
//
// It is a plain struct rather than the app's State so that this package does not
// depend on the store, and so the prompt can be assembled and asserted on in a
// test with a literal. The app fills it in.
type PromptContext struct {
	// Persona is the user-editable system prompt.
	Persona string
	// Now is the moment the request is being made.
	Now time.Time
	// Todos are the unfinished entries, already sorted the way the panel shows
	// them (overdue first, then due soonest).
	Todos []PromptTodo
	// OpenCount, OverdueCount and DueSoonCount mirror the badge on the pet.
	OpenCount    int
	OverdueCount int
	DueSoonCount int
	// Pomodoro describes the timer, when the feature is on.
	Pomodoro PromptPomodoro
	// RecentSessions are the most recent pomodoro summaries, newest first.
	RecentSessions []PromptSession
	// IncludeTodos is the LLMContextTodos switch. When false, none of the
	// todo/session detail is written into the prompt - the user asked for the
	// pet to know nothing about their day.
	IncludeTodos bool
}

// PromptTodo is one todo as it appears in the prompt.
type PromptTodo struct {
	Title string
	// Due is a human-readable deadline ("今天 18:00", "已逾期 2 小时"), already
	// rendered by the app so this package needs no locale rules.
	Due string
	// Overdue marks an entry whose deadline has passed.
	Overdue bool
}

// PromptPomodoro describes the timer for the prompt.
type PromptPomodoro struct {
	Enabled bool
	// Phase is the user-facing phase name ("专注", "短休息"...).
	Phase string
	// Remaining is the countdown, already formatted as MM:SS.
	Remaining string
	// TodoTitle is the session's associated todo, if any.
	TodoTitle string
	// CompletedToday is how many focus phases have finished today.
	CompletedToday int
}

// PromptSession is one finished pomodoro, for context.
type PromptSession struct {
	Title string
	// Summary is what the user wrote (may be empty when they skipped it).
	Summary string
	// When is a human-readable timestamp ("今天 14:30").
	When string
}

// maxPromptTodos caps how many todos reach the prompt.
//
// The list is context, not a data dump: ten entries are enough for the pet to
// answer "what is on my plate", and every extra entry is paid for on every
// single message.
const maxPromptTodos = 12

// maxPromptSessions caps the session history in the prompt.
const maxPromptSessions = 3

// MaxPromptTodos and MaxPromptSessions expose the caps so the caller can stop
// collecting data it would only throw away.
func MaxPromptTodos() int    { return maxPromptTodos }
func MaxPromptSessions() int { return maxPromptSessions }

// maxSummaryRunes caps one session summary's contribution.
const maxSummaryRunes = 120

// BuildMessages assembles the message list for one request.
//
// The order is: the system message (persona plus the world snapshot), then the
// replayed conversation, then nothing - the caller appends the new user message
// only after persisting it, so that a failure to persist cannot produce a reply
// to a question the transcript does not contain.
func BuildMessages(ctx PromptContext, history []Message) []Message {
	system := BuildSystemPrompt(ctx)
	msgs := make([]Message, 0, len(history)+1)
	if strings.TrimSpace(system) != "" {
		msgs = append(msgs, Message{Role: "system", Content: system})
	}
	msgs = append(msgs, history...)
	return msgs
}

// BuildSystemPrompt renders the persona and the world snapshot.
//
// Everything the pet "knows" about the user's day comes from here, which is why
// the wording is explicit about the data being real: a model given a bare list
// of todos will happily invent extra ones to sound helpful, and a model told
// "these are all of them, do not invent more" mostly does not.
func BuildSystemPrompt(ctx PromptContext) string {
	var b strings.Builder

	persona := strings.TrimSpace(ctx.Persona)
	if persona != "" {
		b.WriteString(persona)
		b.WriteString("\n\n")
	}

	b.WriteString("以下是当前的真实情况，只能依据这些内容回答，不要编造：\n")

	// Time. The weekday is included because "这周五" is otherwise unanswerable.
	now := ctx.Now
	if now.IsZero() {
		now = time.Now()
	}
	weekday := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[int(now.Weekday())]
	fmt.Fprintf(&b, "- 现在：%s %s %s\n",
		now.Format("2006-01-02"), weekday, now.Format("15:04"))

	if ctx.IncludeTodos {
		writeTodoSection(&b, ctx)
		writePomodoroSection(&b, ctx)
		writeSessionSection(&b, ctx)
	} else {
		b.WriteString("- 用户关闭了日程上下文：不知道他的待办和番茄钟，被问到就说明自己看不到。\n")
	}

	return b.String()
}

// writeTodoSection renders the todo block.
func writeTodoSection(b *strings.Builder, ctx PromptContext) {
	if ctx.OpenCount == 0 && len(ctx.Todos) == 0 {
		b.WriteString("- 待办：目前没有未完成的事项。\n")
		return
	}

	parts := []string{fmt.Sprintf("共 %d 项未完成", ctx.OpenCount)}
	if ctx.OverdueCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 项已逾期", ctx.OverdueCount))
	}
	if ctx.DueSoonCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 项 24 小时内到期", ctx.DueSoonCount))
	}
	fmt.Fprintf(b, "- 待办：%s", strings.Join(parts, "，"))
	if len(ctx.Todos) > maxPromptTodos {
		fmt.Fprintf(b, "（下面只列出前 %d 项）", maxPromptTodos)
	}
	b.WriteString("\n")

	for i, t := range ctx.Todos {
		if i == maxPromptTodos {
			break
		}
		title := strings.TrimSpace(t.Title)
		if title == "" {
			continue
		}
		b.WriteString("  ")
		if t.Overdue {
			b.WriteString("[逾期] ")
		}
		b.WriteString(title)
		if due := strings.TrimSpace(t.Due); due != "" {
			fmt.Fprintf(b, "（%s）", due)
		}
		b.WriteString("\n")
	}
}

// writePomodoroSection renders the timer block.
func writePomodoroSection(b *strings.Builder, ctx PromptContext) {
	p := ctx.Pomodoro
	if !p.Enabled {
		b.WriteString("- 番茄钟：未开启。\n")
		return
	}
	if p.Phase == "" {
		b.WriteString("- 番茄钟：已开启，当前空闲。\n")
		return
	}
	fmt.Fprintf(b, "- 番茄钟：%s", p.Phase)
	if p.Remaining != "" {
		fmt.Fprintf(b, "，还剩 %s", p.Remaining)
	}
	if t := strings.TrimSpace(p.TodoTitle); t != "" {
		fmt.Fprintf(b, "，正在做「%s」", t)
	}
	b.WriteString("\n")
	if p.CompletedToday > 0 {
		fmt.Fprintf(b, "- 今天已完成 %d 个番茄。\n", p.CompletedToday)
	}
}

// writeSessionSection renders the recent-summaries block.
func writeSessionSection(b *strings.Builder, ctx PromptContext) {
	if len(ctx.RecentSessions) == 0 {
		return
	}
	b.WriteString("- 最近的番茄总结：\n")
	n := 0
	for _, s := range ctx.RecentSessions {
		if n == maxPromptSessions {
			break
		}
		summary := strings.TrimSpace(s.Summary)
		if summary == "" {
			continue // a skipped summary says nothing; an empty bullet invites invention
		}
		n++
		fmt.Fprintf(b, "  %s《%s》：%s\n", s.When, strings.TrimSpace(s.Title), truncate(summary, maxSummaryRunes))
	}
	if n == 0 {
		// Undo the header: it promised content that does not exist.
		trimLastLine(b)
	}
}

// trimLastLine removes the most recent line from b. It backs out a header that
// turned out to have nothing under it, which is the alternative to buffering the
// section first.
func trimLastLine(b *strings.Builder) {
	s := b.String()
	if i := strings.LastIndex(strings.TrimRight(s, "\n"), "\n"); i >= 0 {
		b.Reset()
		b.WriteString(s[:i+1])
	} else {
		b.Reset()
	}
}
