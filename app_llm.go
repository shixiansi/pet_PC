package main

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/llm"
	"PetPC/internal/store"
)

// eventChatOpen tells the frontend to switch to the conversation tab. It
// travels separately from the layout because the layout carries geometry,
// not which tab is showing.
const eventChatOpen = "pet:chat-open"

// llmState is the LLM-specific half of App's runtime state.
//
// It is a separate struct rather than more fields on App because its locking is
// different: App.mu guards the window geometry, and this is touched from a
// plugin's request goroutine, which must never be able to block the UI thread on
// a window operation (or the reverse - a slow emit must not stall a drag).
type llmState struct {
	mu sync.Mutex
	// key is the encrypted-file secret store the pet used for its own chat.
	//
	// The pet's conversation is a plugin now and the plugin keeps its own
	// credential, so nothing on a request path reads this. It survives for the
	// one-time migration (app_plugin_migrate.go), which re-encrypts the user's
	// existing key into the conversation plugin's private directory: reading it
	// is what makes that migration possible at all.
	key *desktop.SecretStore
	// client is reused across requests, which is what lets it remember that an
	// endpoint rejects `stream_options`.
	client *llm.Client
}

// initLLM wires up the LLM runtime. It cannot fail: a missing or undecryptable
// key is a state the settings panel explains, not a reason to refuse to start.
func (a *App) initLLM() {
	a.secretStore()
	a.llmClient()
}

// secretStore returns the key store, creating it on first use.
//
// The lazy creation is what lets an App be constructed as a bare literal (which
// the existing tests do) and still answer without a nil dereference. It also
// keeps the key file out of the picture entirely until something asks about it.
//
// Its only remaining reader is the one-time migration: the pet's own chat was
// retired, so nothing on a request path resolves a credential from here any more.
func (a *App) secretStore() *desktop.SecretStore {
	a.llm.mu.Lock()
	defer a.llm.mu.Unlock()
	if a.llm.key == nil {
		a.llm.key = desktop.NewSecretStore(a.store.Dir(), llmKeyFile)
	}
	return a.llm.key
}

// llmKeyFile is the encrypted key's file name inside the data directory.
const llmKeyFile = "llm-key.bin"

// --- settings --------------------------------------------------------------

// llmSettings assembles the client settings from a config and an explicit key.
//
// The key is a PARAMETER rather than a read of a.secretStore(): since the
// conversation became a plugin, the credential belongs to the plugin
// (plugin-data/<id>/secret-<key>.bin) and the pet's own key file is retired by
// the one-time migration. Passing it in makes "the credential on this request is
// the one the caller resolved" a structural fact instead of an override a future
// edit could forget - and the host's own store is no longer on any request path.
func (a *App) llmSettings(cfg store.Config, key string) llm.Settings {
	return llm.Settings{
		BaseURL:     cfg.LLMBaseURL,
		Model:       cfg.LLMModel,
		APIKey:      key,
		Temperature: cfg.LLMTemperature,
		MaxTokens:   cfg.LLMMaxTokens,
		Timeout:     time.Duration(cfg.LLMTimeoutSec) * time.Second,
	}
}

// llmClient returns the shared client.
func (a *App) llmClient() *llm.Client {
	a.llm.mu.Lock()
	defer a.llm.mu.Unlock()
	if a.llm.client == nil {
		a.llm.client = llm.NewClient()
	}
	return a.llm.client
}

// clientSnapshot renders the configured endpoint for the debug log, with no key.
//
// Settings.String never prints the key, and this function is the only thing the
// log ever sees, so a key cannot reach debug.log by accident. The key is passed
// as a placeholder rather than read: this is a LOG line, and resolving a
// credential to print "key=set" would put a decrypt on the startup path for
// nothing.
func (a *App) clientSnapshot() string {
	return a.llmSettings(a.store.Config(), "set").String()
}

// describeChatError turns any error into a displayable message and a kind.
func describeChatError(err error) (message, kind string) {
	var ce *llm.ChatError
	if errors.As(err, &ce) {
		return ce.Message, string(ce.Kind)
	}
	return err.Error(), string(llm.KindOther)
}

// --- prompt context --------------------------------------------------------

// promptContext snapshots the pet's world for the system message.
//
// includeTodos is a PARAMETER rather than cfg.LLMContextTodos because the two callers
// answer it differently, and reading it off `cfg` gave the wrong answer for one of
// them. A plugin's config starts from zero, so cfg.LLMContextTodos was always false -
// which made EVERY plugin conversation tell the model "用户关闭了日程上下文". The model
// therefore never saw a todo in a plugin chat, and once tools existed it held todo_add
// in one hand and a sentence saying it could not add anything in the other. That is the
// reply the user got.
//
// So the caller decides: the pet's own settings for a pet-side request, and
// "the user's global switch AND this plugin may read todos" for a plugin's.
func (a *App) promptContext(cfg store.Config, includeTodos bool) llm.PromptContext {
	now := time.Now()
	ctx := llm.PromptContext{
		Persona:      cfg.LLMSystemPrompt,
		Now:          now,
		IncludeTodos: includeTodos,
	}
	if !includeTodos {
		return ctx
	}

	stats := a.store.Stats()
	ctx.OpenCount = stats.Open
	ctx.OverdueCount = stats.Overdue
	ctx.DueSoonCount = stats.DueSoon

	// OpenTodos already returns the panel's order: soonest due first, undated
	// last.
	for i, t := range a.store.OpenTodos() {
		if i == llm.MaxPromptTodos() {
			break
		}
		ctx.Todos = append(ctx.Todos, llm.PromptTodo{
			Title:   t.Title,
			Due:     promptDueLabel(t, now),
			Overdue: t.Overdue(now),
		})
	}

	// The timer is deliberately absent from this snapshot.
	//
	// It used to carry the phase, the countdown, the attached todo and the day's
	// finished-session count. All four belong to the pomodoro plugin now, which
	// keeps its own state machine and its own archive - the host has no timer to
	// describe, and inventing an empty one would tell the model "nothing is running"
	// with the authority of a fact it cannot check.
	return ctx
}

// promptDueLabel renders a deadline the way a person would say it.
//
// It is computed here rather than in the llm package so that package needs no
// locale rules; the model only ever sees the finished phrase.
func promptDueLabel(t store.Todo, now time.Time) string {
	if t.Due == nil {
		return ""
	}
	due := t.Due.In(now.Location())
	label := due.Format("01-02 15:04")
	switch {
	case due.Before(now):
		return "已逾期，" + label
	case due.Format("2006-01-02") == now.Format("2006-01-02"):
		return "今天 " + due.Format("15:04")
	case due.Format("2006-01-02") == now.AddDate(0, 0, 1).Format("2006-01-02"):
		return "明天 " + due.Format("15:04")
	default:
		return label
	}
}

// promptStamp renders a past moment relative to today.
func promptStamp(at, now time.Time) string {
	at = at.In(now.Location())
	if at.Format("2006-01-02") == now.Format("2006-01-02") {
		return "今天 " + at.Format("15:04")
	}
	return at.Format("01-02 15:04")
}

// --- window ----------------------------------------------------------------

// OpenChat opens the panel on the conversation tab.
//
// The panel geometry is untouched: the conversation is a tab inside the same
// 380x520 rectangle as the others, so the native region crop is the one that is
// already verified (docs/llm-chat.md 8). Only which tab is showing changes, and
// that is a frontend concern announced by eventChatOpen.
//
// The event is emitted even when the geometry call fails. Landing on the
// conversation must not depend on the window: the tab is the user's request, and
// the window position is an implementation detail that can fail without
// invalidating it.
func (a *App) OpenChat() (Layout, error) {
	layout, err := a.SetPanelOpen(true)
	a.emitEvent(eventChatOpen, map[string]any{"open": true})
	return layout, err
}

// OpenTodoPanelFromChat opens the todo tab, leaving the panel where it is.
//
// It exists so the conversation's own "待办" affordance has a single named entry
// point on the Go side rather than the frontend calling SetPanelOpen and
// switchTab separately.
func (a *App) OpenTodoPanelFromChat() (Layout, error) {
	layout, err := a.SetPanelOpen(true)
	a.emitEvent(eventChatOpen, map[string]any{"open": false})
	return layout, err
}

// --- diagnostics -----------------------------------------------------------

// logLLMStartup writes one line about the LLM configuration at startup, so a log
// from a user reporting "the chat does not work" already answers the first three
// questions.
//
// It reports the pet's stored llm* values and NOTHING about readiness. Readiness
// used to be resolved here through llmView, but the conversation is a plugin now
// and it keeps its own endpoint, model and credential: the pet's config is only
// the one-time migration source, so a "not ready" verdict computed from it would
// describe a configuration no request uses. The single source of readiness is
// the plugin's own settings, and a plugin that cannot answer says so in an
// llm.error frame rather than in this log.
func (a *App) logLLMStartup() {
	cfg := a.store.Config()
	a.DebugLog(fmt.Sprintf("llm: startup enabled=%v provider=%s base=%s model=%s",
		cfg.LLMEnabled, cfg.LLMProvider, cfg.LLMBaseURL, cfg.LLMModel))
}
