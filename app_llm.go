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

// eventChatOpen used to tell the frontend to switch the panel to its 对话 tab.
//
// It is GONE rather than merely unemitted, and the difference matters to the static gate: a
// subscription with no emitter is exactly what "every subscribed event is emitted by Go"
// exists to catch, and allowing this one name through would teach that check to ignore its
// own failure mode. Nothing outside this page ever subscribed to it - the conversation is a
// plugin, and a plugin's own "open the conversation" ask goes through
// OpenChatPluginSurface, which is a binding rather than an event.

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
// That is the persona and the clock, and nothing else - and the empty half is the point.
// The todo list and the timer used to be filled in here; both are plugins' data now (the
// todo plugin's `storage`, the pomodoro plugin's own state), and the host must not read
// another plugin's data on a third plugin's behalf. Doing so would be exactly the
// "here is the user's todo list" leak that `todos.read` used to gate, with nobody left to
// ask. So llm.BuildSystemPrompt states plainly that the list and the timer are not
// visible, which is true and is what stops the model inventing them.
//
// `promptDueLabel` and `promptStamp` went with the snapshot: the first rendered a todo's
// deadline, the second a session's timestamp, and there are no deadlines or sessions left
// on this side to render.
func (a *App) promptContext(cfg store.Config) llm.PromptContext {
	return llm.PromptContext{
		Persona: cfg.LLMSystemPrompt,
		Now:     time.Now(),
	}
}

// --- window ----------------------------------------------------------------

// OpenChat and OpenTodoPanelFromChat used to live here. Both opened the PANEL on one of its
// tabs, and both are gone with it:
//
//   - the conversation is the chat plugin's floating surface now. A plugin's own script asks
//     for it (`surface.show`, or the host's OpenChatPluginSurface on the hover button), so
//     there is no host tab to select;
//   - the 待办 tab went the same way, and the todo plugin's surface is opened the same way -
//     from the pet's own menu cell, through OpenPluginSurface.
//
// `eventChatOpen` went with OpenChat: it named the panel tab to switch to, and neither the
// tab nor the emitter exists. A subscription with no emitter is what the static gate calls a
// dangling event, so the page's listener is deleted too rather than left as a no-op.


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
