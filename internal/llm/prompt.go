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
//
// WHAT IS NO LONGER HERE, and why it is not coming back: the todos, their overdue
// counts, the timer's phase and the recent session summaries. Each of those became a
// plugin's own data - the list went to com.shixiansi.plugin-todo (`storage`, plus a
// `todo-list` snapshot published through `plugin.share`) and the timer to
// com.shixiansi.plugin-pomodoro. The host cannot read another plugin's private storage,
// and it must not read the snapshot on a third plugin's behalf: that would be exactly the
// "here is the user's todo list" leak the `todos.read` permission used to gate, now with
// nobody to ask. So the model is told the truth instead - see the last line of
// BuildSystemPrompt.
type PromptContext struct {
	// Persona is the user-editable system prompt.
	Persona string
	// Now is the moment the request is being made.
	Now time.Time
}

// PromptPomodoro, PromptSession, PromptTodo and MaxPromptTodos used to live here: the
// timer's phase and countdown, the last few session summaries, and the todo list with
// its badge counts. All of that is plugins' data now (see the comment above), so this
// package has nothing to render and the caller has nothing to fill in.

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
// The snapshot is now shorter than the persona's preamble, and the honest sentence at
// the end is the important part of it: a model told nothing about the user's day will
// invent plausible errands to sound helpful, and one told explicitly that it cannot see
// them mostly will not. "只能依据这些内容回答" alone was not enough when the list was
// sometimes present and sometimes not; saying which of the two it is removes the guess.
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

	b.WriteString("- 待办与番茄钟：由插件各自管理，你这里没有它们的数据。" +
		"被问到就说自己看不到，不要编造条目或进度。\n")

	return b.String()
}
