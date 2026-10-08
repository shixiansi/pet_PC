package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/llm"
	"PetPC/internal/store"
)

// LLM event names. The frontend subscribes to these three; nothing else about a
// request crosses the bridge.
const (
	eventChatDelta = "pet:chat-delta"
	eventChatDone  = "pet:chat-done"
	eventChatError = "pet:chat-error"
	// eventChatOpen tells the frontend to switch to the conversation tab. It
	// travels separately from the layout because the layout carries geometry,
	// not which tab is showing.
	eventChatOpen = "pet:chat-open"
)

// maxChatInput caps one user message.
//
// The limit is character-based and generous (a pasted document is the realistic
// failure mode) but finite: the message is replayed as context on every later
// turn, so an unbounded paste would be paid for repeatedly.
const maxChatInput = 8000

// maxProactiveRunes caps an unprompted remark.
//
// The live overlay's card is a fixed height, and a remark that overflowed it
// would be scrolled out of sight the moment it arrived - the pet would appear to
// have said nothing. 80 runes is roughly three lines at the card's width.
const maxProactiveRunes = 80

// llmState is the LLM-specific half of App's runtime state.
//
// It is a separate struct rather than more fields on App because its locking is
// different: App.mu guards the window geometry, and this is touched from the
// request goroutine, which must never be able to block the UI thread on a window
// operation (or the reverse - a slow emit must not stall a drag).
type llmState struct {
	mu sync.Mutex
	// cancel aborts the in-flight request. Nil when nothing is running.
	cancel context.CancelFunc
	// requestID identifies the in-flight request, so events from a superseded
	// one can be dropped rather than appended to the new reply.
	requestID string
	// generation increments on every new request. It is what distinguishes "this
	// request was replaced by a newer question" from "the user pressed stop":
	// cancelChat clears requestID in both cases, but only SendChat bumps the
	// generation, and the difference decides whether a partial reply is worth
	// keeping (see runChat).
	generation int64
	// key is the encrypted-file secret store.
	key *desktop.SecretStore
	// client is reused across requests, which is what lets it remember that an
	// endpoint rejects `stream_options`.
	client *llm.Client
	// reason holds the chain of thought of the reply currently arriving, so the
	// panel can show "thinking" without a second event stream.
	reason string
	// proactive records whether the in-flight request was started by the pet
	// rather than by the user, so the events it emits can say so.
	proactive bool
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
// the existing tests do) and still answer llmView without a nil dereference. It
// also keeps the key file out of the picture entirely until something asks about
// it, which is why a build with the feature switched off never touches the disk
// for it.
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

// --- state -----------------------------------------------------------------

// LLMView is the LLM state the frontend renders.
//
// It is assembled here rather than reusing store.Config because the parts that
// matter to the UI are not all config: whether a key exists, where it came from,
// what it is masked as, and today's usage all live outside the config file - and
// the key itself must never be in this struct at all.
type LLMView struct {
	Enabled bool `json:"enabled"`
	// Ready is true when a request could be sent right now.
	Ready bool `json:"ready"`
	// NotReadyReason explains a false Ready in one line.
	NotReadyReason string `json:"notReadyReason,omitempty"`

	// HasKey, KeySource and KeyMask describe the credential without revealing
	// it. KeySource is "none", "env" or "file".
	HasKey    bool   `json:"hasKey"`
	KeySource string `json:"keySource"`
	KeyMask   string `json:"keyMask,omitempty"`
	// KeyFile is where the encrypted key lives, shown so the user can find (or
	// delete) it.
	KeyFile string `json:"keyFile"`
	// NeedsKey is false for a local endpoint, so the panel does not demand a key
	// for a server that has none.
	NeedsKey bool `json:"needsKey"`

	// Providers is the preset list for the dropdown.
	Providers []store.ProviderPreset `json:"providers"`

	Usage      store.LLMUsage `json:"usage"`
	UsageLine  string         `json:"usageLine"`
	OverBudget bool           `json:"overBudget"`

	// Messages is the persisted transcript, oldest first.
	Messages []ChatMessageView `json:"messages"`
	// Busy is true while a request is in flight.
	Busy bool `json:"busy"`
	// Proactive is the unprompted-speech state, for the settings tab.
	Proactive ProactiveView `json:"proactive"`
}

// ChatMessageView is one transcript entry as the frontend sees it.
type ChatMessageView struct {
	Role string `json:"role"`
	Text string `json:"text"`
	// At is RFC3339, so the frontend formats it in the user's locale.
	At string `json:"at"`
	// Error marks a message that reports a failure rather than a reply.
	Error bool `json:"error,omitempty"`
	// Proactive marks a reply the pet started on its own.
	Proactive bool `json:"proactive,omitempty"`
}

// llmReadiness is the part of llmView that other views need: whether a request
// could be sent right now, why not, and whether the budget is spent.
//
// It exists to BREAK A CYCLE. proactiveView needs these three facts, and it used
// to get them by calling llmView - which in turn calls proactiveView to fill in
// view.Proactive. That is infinite recursion, and it is not theoretical: with
// LLMProactive switched on, every GetState recursed until the goroutine stack
// overflowed and the process died. The symptom was the app vanishing on startup
// with no error, which is why it took a stack dump to find.
//
// Returning a small struct rather than "llmView without the proactive field"
// keeps the dependency one-way: llmView depends on proactiveView, never the
// reverse.
type llmReadiness struct {
	Ready          bool
	NotReadyReason string
	OverBudget     bool
}

// llmReadinessNow computes the readiness triple from the live config.
func (a *App) llmReadinessNow() llmReadiness {
	cfg := a.store.Config()
	key := a.secretStore()
	needsKey := true
	if preset, ok := store.LLMProviderByID(cfg.LLMProvider); ok {
		needsKey = preset.NeedsKey
	}
	hasKey := key.Source() != desktop.SourceNone

	var r llmReadiness
	// The first reason found wins, so the panel explains the most fundamental
	// problem rather than the last one checked - the same rule llmView uses.
	switch {
	case !cfg.LLMEnabled:
		r.NotReadyReason = "大模型对话未开启"
	case cfg.LLMBaseURL == "":
		r.NotReadyReason = "还没有填写接口地址"
	case cfg.LLMModel == "":
		r.NotReadyReason = "还没有选择模型"
	case needsKey && !hasKey:
		r.NotReadyReason = "还没有填写 API Key"
	}

	usage := a.store.LLMUsageToday(time.Now())
	r.OverBudget = usage.OverBudget(cfg.LLMDailyBudget)
	if r.OverBudget && r.NotReadyReason == "" {
		r.NotReadyReason = fmt.Sprintf("已达今日请求上限（%d 次）", cfg.LLMDailyBudget)
	}
	r.Ready = cfg.LLMEnabled && r.NotReadyReason == ""
	return r
}

// llmView assembles the LLM snapshot.
func (a *App) llmView() LLMView {
	cfg := a.store.Config()
	now := time.Now()
	key := a.secretStore()

	source := key.Source()
	needsKey := true
	if preset, ok := store.LLMProviderByID(cfg.LLMProvider); ok {
		needsKey = preset.NeedsKey
	}

	view := LLMView{
		Enabled:   cfg.LLMEnabled,
		HasKey:    source != desktop.SourceNone,
		KeySource: string(source),
		KeyMask:   key.Mask(),
		KeyFile:   key.Path(),
		NeedsKey:  needsKey,
		Providers: store.LLMProviders(),
	}

	usage := a.store.LLMUsageToday(now)
	view.Usage = usage
	view.UsageLine = usage.Summary(cfg.LLMDailyBudget)
	view.OverBudget = usage.OverBudget(cfg.LLMDailyBudget)

	// One source of truth for the reasons and the ready flag.
	r := a.llmReadinessNow()
	view.NotReadyReason = r.NotReadyReason
	view.Ready = r.Ready

	for _, m := range a.store.ChatHistory() {
		view.Messages = append(view.Messages, ChatMessageView{
			Role:      m.Role,
			Text:      m.Text,
			At:        m.At.Format(time.RFC3339),
			Error:     m.Error,
			Proactive: m.Proactive,
		})
	}
	if view.Messages == nil {
		view.Messages = []ChatMessageView{}
	}

	a.llm.mu.Lock()
	view.Busy = a.llm.requestID != ""
	a.llm.mu.Unlock()
	view.Proactive = a.proactiveView()
	return view
}

// --- settings --------------------------------------------------------------

// LLMSettingsInput is the settings form's payload.
//
// There is deliberately no key field: the key has its own method (SetLLMKey), so
// it can never be written as part of a generic settings save, and so a failed
// settings save cannot leave a half-stored credential behind.
type LLMSettingsInput struct {
	Enabled            bool    `json:"enabled"`
	Provider           string  `json:"provider"`
	BaseURL            string  `json:"baseUrl"`
	Model              string  `json:"model"`
	Temperature        float64 `json:"temperature"`
	MaxTokens          int     `json:"maxTokens"`
	TimeoutSec         int     `json:"timeoutSec"`
	SystemPrompt       string  `json:"systemPrompt"`
	HistoryTurns       int     `json:"historyTurns"`
	ContextTodos       bool    `json:"contextTodos"`
	Proactive          bool    `json:"proactive"`
	ProactiveMinGapMin int     `json:"proactiveMinGapMin"`
	DailyBudget        int     `json:"dailyBudget"`
}

// SaveLLMSettings persists the form and returns the new state.
func (a *App) SaveLLMSettings(in LLMSettingsInput) (State, error) {
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.LLMEnabled = in.Enabled
		c.LLMProvider = in.Provider
		c.LLMBaseURL = in.BaseURL
		c.LLMModel = in.Model
		c.LLMTemperature = in.Temperature
		c.LLMMaxTokens = in.MaxTokens
		c.LLMTimeoutSec = in.TimeoutSec
		c.LLMSystemPrompt = in.SystemPrompt
		c.LLMHistoryTurns = in.HistoryTurns
		c.LLMContextTodos = in.ContextTodos
		c.LLMProactive = in.Proactive
		c.LLMProactiveMinGapMin = in.ProactiveMinGapMin
		c.LLMDailyBudget = in.DailyBudget
	}); err != nil {
		return State{}, err
	}
	// The endpoint may have changed, so the client's memory of "this server
	// rejects stream_options" is about a different server now. A fresh client is
	// cheaper than tracking which fields invalidate what.
	a.llm.mu.Lock()
	a.llm.client = llm.NewClient()
	a.llm.mu.Unlock()

	// Switching proactive speech on has to schedule the first remark, and
	// switching it off has to cancel the pending one - otherwise a remark left
	// over from a previous configuration would still fire.
	a.rescheduleProactive()

	a.DebugLog("llm: settings saved -> " + a.clientSnapshot())
	return a.GetState()
}

// rescheduleProactive brings the proactive schedule in line with the config.
//
// Called whenever the settings that the gate reads may have changed. It mirrors
// SetReminderEnabled: turning the feature on starts the countdown from now (so
// the pet does not speak the moment it is switched on), and turning it off
// clears the schedule entirely.
func (a *App) rescheduleProactive() {
	cfg := a.store.Config()
	if !cfg.LLMProactive || !cfg.LLMEnabled {
		a.setNextProactive(time.Time{})
		if a.liveIsOpen() && !cfg.LLMEnabled {
			// The feature that owns the overlay is gone, so the overlay goes too.
			// Leaving it up would show an input box that cannot send anything.
			if _, err := a.CloseLive(); err != nil {
				a.DebugLog("llm: could not close the live overlay: " + err.Error())
			}
		}
		return
	}
	a.setNextProactive(time.Now().Add(a.proactiveGap(cfg)))
	a.emitLayout()
}

// SetLLMKey encrypts and stores the API key.
func (a *App) SetLLMKey(key string) (State, error) {
	if err := a.secretStore().Set(key); err != nil {
		return State{}, fmt.Errorf("保存密钥失败: %w", err)
	}
	a.DebugLog("llm: key stored, source now " + string(a.secretStore().Source()))
	return a.GetState()
}

// ClearLLMKey forgets the stored API key.
//
// A key supplied through the environment cannot be removed from here. The panel
// reads KeySource, so it can say so rather than appearing to succeed while
// requests keep working.
func (a *App) ClearLLMKey() (State, error) {
	if err := a.secretStore().Clear(); err != nil {
		return State{}, fmt.Errorf("删除密钥失败: %w", err)
	}
	a.DebugLog("llm: key cleared, source now " + string(a.secretStore().Source()))
	return a.GetState()
}

// TestLLMConnection sends a minimal request to check the configuration end to
// end: address, key, model name and network.
//
// The probe is a real (paid) request with max_tokens=1, which is the cheapest
// way to prove all four at once. Validating them separately would leave the
// commonest failure - a working address with a model name the account cannot use
// - undetected.
func (a *App) TestLLMConnection() LLMTestResult {
	cfg := a.store.Config()
	start := time.Now()
	result := LLMTestResult{BaseURL: cfg.LLMBaseURL, Model: cfg.LLMModel}

	settings := a.llmSettings(cfg)
	settings.MaxTokens = 1
	// The probe is deliberately short-tempered: the user is waiting on a button,
	// so an endpoint that is merely slow should report that quickly rather than
	// holding the panel for the full configured timeout.
	if settings.Timeout > 20*time.Second {
		settings.Timeout = 20 * time.Second
	}

	res, err := a.llmClient().Chat(context.Background(), settings,
		[]llm.Message{{Role: "user", Content: "ping"}}, llm.Handlers{})
	result.ElapsedMS = time.Since(start).Milliseconds()
	if res.Model != "" {
		result.Model = res.Model
	}
	if err != nil {
		result.Error, result.Kind = describeChatError(err)
		a.DebugLog(fmt.Sprintf("llm: test connection failed kind=%s in=%dms: %s",
			result.Kind, result.ElapsedMS, result.Error))
		return result
	}
	result.OK = true
	a.DebugLog(fmt.Sprintf("llm: test connection ok model=%s in=%dms", result.Model, result.ElapsedMS))
	return result
}

// LLMTestResult reports the outcome of TestLLMConnection.
type LLMTestResult struct {
	OK bool `json:"ok"`
	// Model is what the server said it used, which can differ from what was
	// requested.
	Model     string `json:"model"`
	BaseURL   string `json:"baseUrl"`
	ElapsedMS int64  `json:"elapsedMs"`
	// Error is a user-facing message; Kind is the classification.
	Error string `json:"error,omitempty"`
	Kind  string `json:"kind,omitempty"`
}

// llmSettings assembles the client settings from the config and the secret
// store. It is the only place the key is read.
func (a *App) llmSettings(cfg store.Config) llm.Settings {
	return llm.Settings{
		BaseURL:     cfg.LLMBaseURL,
		Model:       cfg.LLMModel,
		APIKey:      a.secretStore().Get(),
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
// log ever sees, so a key cannot reach debug.log by accident.
func (a *App) clientSnapshot() string {
	return a.llmSettings(a.store.Config()).String()
}

// describeChatError turns any error into a displayable message and a kind.
func describeChatError(err error) (message, kind string) {
	var ce *llm.ChatError
	if errors.As(err, &ce) {
		return ce.Message, string(ce.Kind)
	}
	return err.Error(), string(llm.KindOther)
}

// --- chat ------------------------------------------------------------------

// ChatStart is what SendChat, Regenerate and SpeakNow return: the id of the
// request that was started.
type ChatStart struct {
	RequestID string `json:"requestId"`
	// Proactive is true when the pet started this round itself. The frontend
	// uses it to open the live overlay and to label the round, since in that case
	// there is no question of the user's above the reply.
	Proactive bool `json:"proactive,omitempty"`
}

// SendChat starts a request and returns immediately.
//
// The immediate return is not a nicety, it is the design: a Wails binding call
// runs on the bridge's thread, so doing the HTTP work here would freeze the
// panel - and the pet - until the model finished. The reply arrives as events.
func (a *App) SendChat(text string) (ChatStart, error) {
	text = trimToRunes(text, maxChatInput)
	if text == "" {
		return ChatStart{}, fmt.Errorf("说点什么吧")
	}
	return a.startChat(chatRequest{Text: text})
}

// chatRequest is one turn to run: the user's text, plus the two things that
// differ when the pet is the one starting the conversation.
type chatRequest struct {
	// Text is the message the model sees.
	Text string
	// Proactive marks an unprompted remark: it is not persisted as a user
	// message, because the user did not say it.
	Proactive bool
	// MaxRunes caps the reply. Zero means the configured max_tokens is the only
	// limit.
	MaxRunes int
}

// startChat validates, persists and launches one request.
//
// SendChat and the proactive path share this so there is exactly one place that
// talks to the model, exactly one place that persists the question and exactly
// one way the request-id bookkeeping can go wrong.
func (a *App) startChat(req chatRequest) (ChatStart, error) {
	cfg := a.store.Config()
	if !cfg.LLMEnabled {
		return ChatStart{}, fmt.Errorf("大模型对话未开启，请先到设置里开启")
	}
	if view := a.llmView(); !view.Ready {
		return ChatStart{}, errors.New(view.NotReadyReason)
	}
	if req.Proactive {
		a.noteSpoke(time.Now())
	}

	// Only one request at a time. Starting a new one supersedes the old: the
	// user asked a new question, so the half-finished previous answer is not
	// what they want (docs/llm-chat.md 10, "生成中再次发送").
	//
	// Cancelling and marking the old request stale happen in ONE locked step. Doing
	// them separately left a window in which the old request could finish after
	// its id was cleared but before the generation moved, so it looked like a
	// user-initiated stop and its stale fragment was appended after the new
	// question.
	a.supersede()

	// Persist the question BEFORE sending it, so a crash or a failed request
	// cannot leave a reply in the transcript with no question above it.
	//
	// A proactive remark has no question: the instruction that produced it is a
	// system prompt, not something the user typed, and writing it into the
	// transcript would make the panel's record of the conversation a lie.
	if !req.Proactive {
		if _, err := a.store.AppendChat(store.RoleUser, req.Text, false); err != nil {
			return ChatStart{}, fmt.Errorf("保存消息失败: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	id := fmt.Sprintf("c%d", time.Now().UnixNano())

	a.llm.mu.Lock()
	gen := a.llm.generation
	a.llm.cancel = cancel
	a.llm.requestID = id
	a.llm.reason = ""
	a.llm.proactive = req.Proactive
	a.llm.mu.Unlock()

	// The request is counted when it is SENT, not when it succeeds: the budget
	// exists to bound spending, and a request that fails still cost a round trip.
	if _, err := a.store.AddLLMUsage(0, 0, time.Now()); err != nil {
		// The counter is best-effort: failing to record it must not stop the
		// user from talking to the pet.
		a.DebugLog("llm: could not record usage: " + err.Error())
	}

	go a.runChat(ctx, id, gen, cfg, req)
	return ChatStart{RequestID: id, Proactive: req.Proactive}, nil
}

// SpeakNow makes the pet say something unprompted, right now.
//
// It is the manual escape hatch for the proactive gate, exactly as RemindNow is
// for the reminder schedule: the user asking the pet to speak is not something
// any of the gates should be able to refuse (except the content check, which is
// what "the feature is even usable" depends on).
func (a *App) SpeakNow() (State, error) {
	cfg := a.store.Config()
	if !cfg.LLMEnabled {
		return State{}, fmt.Errorf("大模型对话未开启")
	}
	if view := a.llmView(); !view.Ready {
		return State{}, errors.New(view.NotReadyReason)
	}
	if _, err := a.startChat(chatRequest{Text: proactivePrompt, Proactive: true, MaxRunes: maxProactiveRunes}); err != nil {
		return State{}, err
	}
	// The next automatic remark is measured from this one, so a manual nudge does
	// not immediately bring an automatic one with it.
	a.setNextProactive(time.Now().Add(a.proactiveGap(cfg)))
	return a.GetState()
}

// speakProactive starts the scheduled unprompted remark and opens the overlay.
//
// The slot is checked BEFORE the request is sent. The gate in tickProactive
// already refuses when a question is waiting, but that check and this call are
// not atomic: a focus phase can end in between and raise the summary prompt. If
// the request were sent first, that race would spend a paid round trip whose
// reply has nowhere to appear - it would be recorded in the transcript with no
// visible reason, which is exactly the "the pet said something for no reason"
// confusion the proactive flag exists to avoid.
func (a *App) speakProactive() (State, error) {
	if kind := a.unansweredQuestionKind(); kind != BubbleNone {
		return State{}, errors.New(questionOwnsTheSlot(kind))
	}
	cfg := a.store.Config()
	if _, err := a.startChat(chatRequest{Text: proactivePrompt, Proactive: true, MaxRunes: maxProactiveRunes}); err != nil {
		return State{}, err
	}
	a.setNextProactive(time.Now().Add(a.proactiveGap(cfg)))
	// The pet speaking on its own is exactly the case the overlay exists for, so
	// it comes up without the user having to ask for it.
	layout, err := a.OpenLive()
	if err != nil {
		return State{}, err
	}
	_ = layout
	return a.GetState()
}

// runChat performs the request and streams it to the frontend.
func (a *App) runChat(ctx context.Context, id string, gen int64, cfg store.Config, req chatRequest) {
	start := time.Now()

	history := a.store.Replay(cfg.LLMHistoryTurns)
	msgs := llm.BuildMessages(a.promptContext(cfg), chatMessages(history))
	// The proactive instruction is appended AFTER the replay rather than
	// persisted, so it steers this turn without ever appearing in the transcript
	// the user reads.
	if req.Proactive {
		msgs = append(msgs, llm.Message{Role: "user", Content: req.Text})
	}
	settings := a.llmSettings(cfg)

	a.DebugLog(fmt.Sprintf("llm: request %s start proactive=%v -> %s", id, req.Proactive, settings.String()))

	// Replies are NOT read aloud any more.
	//
	// A real-time synthesis pipeline used to speak them, streaming sentence by
	// sentence as the model produced text. It was retired because it was slow (11
	// seconds for a 54-character reply), heavy (3.3GB resident for the local model)
	// and unstable (the upstream service documents its own output as random). The
	// pet now speaks only its own fixed notices - the timer and the reminder - which
	// are pre-rendered and embedded.
	//
	// That removal is why this function no longer holds text back or reveals it in
	// step with a voice: the reply is simply streamed as it arrives, exactly as it
	// was before any of the speech work began.
	handlers := llm.Handlers{
		OnText: func(s string) {
			if !a.requestCurrent(id) {
				return // a newer request replaced this one
			}
			a.emitEvent(eventChatDelta, map[string]any{
				"requestId": id,
				"text":      s,
				// The flag travels with EVERY event of the round, not just the
				// terminal one. The overlay has to know from the very first delta
				// that this is an unprompted remark: it decides whether to render
				// a question line above the reply, and by the time "done" arrives
				// the reply has already been shown under the previous question.
				"proactive": req.Proactive,
			})
		},
		OnReasoning: func(s string) {
			a.llm.mu.Lock()
			if a.llm.requestID == id {
				a.llm.reason += s
			}
			a.llm.mu.Unlock()
		},
	}

	res, err := a.llmClient().Chat(ctx, settings, msgs, handlers)
	elapsed := time.Since(start).Milliseconds()

	// An unprompted remark is capped AFTER it arrives, and the cap is applied to
	// the same text the frontend was streamed: the live overlay's card is a fixed
	// height, so a reply that ran past it would be scrolled out of sight on the
	// very frame it finished - the pet would appear to have said nothing. Capping
	// only the stored copy would leave the two disagreeing.
	if req.MaxRunes > 0 && err == nil {
		res.Text = trimToRunes(res.Text, req.MaxRunes)
	}

	// The token report arrives without the request having to be counted again.
	if res.Usage != nil {
		if _, uerr := a.store.AddLLMUsageTokens(res.Usage.PromptTokens, res.Usage.CompletionTokens, time.Now()); uerr != nil {
			a.DebugLog("llm: could not record tokens: " + uerr.Error())
		}
	}

	var reasoning string
	a.llm.mu.Lock()
	reasoning = a.llm.reason
	current := a.llm.requestID == id
	if current {
		a.llm.requestID = ""
		a.llm.cancel = nil
		a.llm.reason = ""
	}
	superseded := a.llm.generation != gen
	a.llm.mu.Unlock()

	// Whether this answer is still wanted.
	//
	// A superseded request is one the user replaced with a new question: its
	// partial text belongs to a question that is no longer last in the
	// transcript, so saving it would put a stale fragment between the new
	// question and its answer. A stopped request is different - the user ended
	// it deliberately, the question is still the last one, and the text they
	// watched arrive should be kept.
	if !superseded {
		if text := res.Text; text != "" {
			// The proactive flag is carried into the transcript so the panel can
			// label a round the pet started.
			if _, aerr := a.store.AppendChatMessage(store.ChatMessage{
				Role:      store.RoleAssistant,
				Text:      text,
				Proactive: req.Proactive,
			}); aerr != nil {
				a.DebugLog("llm: could not save reply: " + aerr.Error())
			}
		}
	}

	// The authoritative reply text for this round, stored so the drain goroutine
	// can release anything that was held but never spoken. It is the TRIMMED text
	// (below), which is why it is recorded after the proactive cap rather than
	// here.
	if err != nil {
		message, kind := describeChatError(err)
		// A cancellation is not a failure worth writing into the transcript: the
		// user did it on purpose (or replaced the question), and a permanent
		// "已取消。" line would clutter the history they scroll back through.
		if kind != string(llm.KindCanceled) && !superseded {
			if _, aerr := a.store.AppendChat(store.RoleSystem, message, true); aerr != nil {
				a.DebugLog("llm: could not save error note: " + aerr.Error())
			}
		}
		a.DebugLog(fmt.Sprintf("llm: request %s failed kind=%s attempts=%d superseded=%v in=%dms: %s",
			id, kind, res.Attempts, superseded, elapsed, message))
		// A superseded request says nothing to the UI either: the new request
		// already told it what is happening, and its "stopped" note would appear
		// under the wrong question.
		if !superseded {
			a.emitEvent(eventChatError, map[string]any{
				"requestId": id,
				"kind":      kind,
				"message":   message,
				"partial":   res.Text,
				"proactive": req.Proactive,
			})
		}
		return
	}

	a.DebugLog(fmt.Sprintf("llm: request %s done in=%dms attempts=%d chars=%d finish=%s",
		id, elapsed, res.Attempts, len(res.Text), res.FinishReason))
	a.emitEvent(eventChatDone, map[string]any{
		"requestId":    id,
		"text":         res.Text,
		"reasoning":    reasoning,
		"finishReason": res.FinishReason,
		"model":        res.Model,
		"elapsedMs":    elapsed,
		"proactive":    req.Proactive,
	})
}

// CancelChat aborts the in-flight request. Cancelling nothing is not an error.
func (a *App) CancelChat(requestID string) bool {
	a.llm.mu.Lock()
	cancel, current := a.llm.cancel, a.llm.requestID
	a.llm.mu.Unlock()

	// An empty requestID means "whatever is running", which is what the stop
	// button sends if it has lost track of the id.
	if requestID != "" && requestID != current {
		return false
	}
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// cancelChat stops any in-flight request without marking it stale: the user
// pressed stop, so whatever arrived is theirs to keep.
//
// It does not wait for the goroutine: the request's own cleanup clears the state
// under the same lock, and blocking here would stall the UI thread for as long
// as the transport takes to notice the cancellation.
func (a *App) cancelChat() {
	a.llm.mu.Lock()
	cancel := a.llm.cancel
	// Clear the id immediately so the cancelled request's late events are dropped
	// even before its goroutine notices.
	a.llm.requestID = ""
	a.llm.cancel = nil
	a.llm.reason = ""
	a.llm.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// supersede cancels any in-flight request AND marks it stale.
//
// The two have to happen under one lock. Split apart, the old request could
// finish in the gap: it would see its own generation as current, treat itself as
// a user-initiated stop, and append a partial answer that belongs to a question
// the user has already replaced - landing it between the new question and its
// reply.
func (a *App) supersede() {
	a.llm.mu.Lock()
	cancel := a.llm.cancel
	a.llm.generation++
	a.llm.requestID = ""
	a.llm.cancel = nil
	a.llm.reason = ""
	a.llm.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// requestCurrent reports whether id is still the request the UI is waiting for.
func (a *App) requestCurrent(id string) bool {
	a.llm.mu.Lock()
	defer a.llm.mu.Unlock()
	return a.llm.requestID == id
}

// ClearChat empties the transcript.
func (a *App) ClearChat() (State, error) {
	a.cancelChat()
	if _, err := a.store.ClearChat(); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// Regenerate drops the last reply and re-sends the question before it.
//
// It truncates the transcript and calls SendChat with the same text, so there is
// exactly one code path that talks to the model.
func (a *App) Regenerate() (ChatStart, error) {
	msgs := a.store.ChatHistory()
	last := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == store.RoleUser {
			last = i
			break
		}
	}
	if last < 0 {
		return ChatStart{}, fmt.Errorf("没有可以重新生成的问题")
	}
	text := msgs[last].Text
	if _, err := a.store.TruncateChatTo(last); err != nil {
		return ChatStart{}, err
	}
	return a.SendChat(text)
}

// ResetLLMUsage zeroes today's counters, so a user who hit the cap on purpose
// can carry on without editing JSON.
func (a *App) ResetLLMUsage() (State, error) {
	if _, err := a.store.ResetLLMUsage(time.Now()); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// --- prompt context --------------------------------------------------------

// promptContext snapshots the pet's world for the system message.
func (a *App) promptContext(cfg store.Config) llm.PromptContext {
	now := time.Now()
	ctx := llm.PromptContext{
		Persona:      cfg.LLMSystemPrompt,
		Now:          now,
		IncludeTodos: cfg.LLMContextTodos,
	}
	if !cfg.LLMContextTodos {
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

	if pv := a.pomodoroView(); pv.Enabled {
		ctx.Pomodoro = llm.PromptPomodoro{
			Enabled:        true,
			Phase:          promptPhase(pv),
			Remaining:      pv.Countdown,
			TodoTitle:      pv.TodoTitle,
			CompletedToday: a.todayFocusCount(now),
		}
	}

	// Collected newest first; the prompt builder caps what it renders, and the
	// two extra entries here cover the case where the newest summaries are ones
	// the user skipped (they carry no information and are dropped).
	for _, r := range a.store.PomodoroRecords() {
		if len(ctx.RecentSessions) >= llm.MaxPromptSessions()+2 {
			break
		}
		ctx.RecentSessions = append(ctx.RecentSessions, llm.PromptSession{
			Title:   r.TodoTitle,
			Summary: r.Summary,
			When:    promptStamp(r.EndedAt, now),
		})
	}
	return ctx
}

// promptPhase renders the timer's state for the model, or "" when there is no
// phase to report.
func promptPhase(v PomodoroView) string {
	if !v.Enabled || (!v.Running && !v.Focusing) {
		return ""
	}
	return v.Label
}

// todayFocusCount counts the focus sessions that ended today.
func (a *App) todayFocusCount(now time.Time) int {
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	n := 0
	for _, r := range a.store.PomodoroRecords() {
		if !r.EndedAt.Before(startOfDay) {
			n++
		}
	}
	return n
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

// chatMessages converts stored messages into the API shape.
func chatMessages(msgs []store.ChatMessage) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, llm.Message{Role: m.Role, Content: m.Text})
	}
	return out
}

// trimToRunes trims whitespace and caps a string at n runes.
//
// The cap counts runes so a Chinese message is not cut mid-character, which
// would put invalid UTF-8 in the request body.
func trimToRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n]))
}

// --- window ----------------------------------------------------------------

// OpenChat opens the panel on the conversation tab.
//
// The panel geometry is untouched: the conversation is a tab inside the same
// 380x520 rectangle as the others, so the native region crop is the one that is
// already verified (docs/llm-chat.md 8). Only which tab is showing changes, and
// that is a frontend concern announced by eventChatOpen.
//
// The event is emitted even when the geometry call fails. Double-clicking the pet
// must land on the conversation either way: the tab is the user's request, and
// the window position is an implementation detail that can fail without
// invalidating it.
func (a *App) OpenChat() (Layout, error) {
	layout, err := a.SetPanelOpen(true)
	a.emitEvent(eventChatOpen, map[string]any{"open": true})
	return layout, err
}

// OpenLive shows the live conversation overlay.
//
// The overlay is the pet's real-time surface: the current round at the top, the
// input box at the bottom, and a trail of thought-puffs between them pointing at
// the pet. It occupies the same slot beside the pet as the panel and the speech
// bubble, so opening it folds both away - the window region can only describe
// one of them (docs/reminder-pomodoro.md 8.2).
//
// Nothing is resized: the window is already the full panel+pet rectangle and the
// overlay is drawn inside it, so this only changes the region crop. That is the
// whole reason the pet does not move when the overlay appears.
//
// THE ONE THING IT MAY NOT DISPLACE is a bubble that is waiting for the user's
// answer. The overlay takes the slot rather than sharing it, so opening it over
// the todo picker or the summary prompt does not cover the question - it deletes
// it. For the summary that is not a cosmetic loss: the prompt is the gate on the
// break, so the timer stays parked with nothing on screen saying why, and the
// user has no way to discover that a double click on the pet is what ate it. The
// panel is allowed to win that slot because the panel can answer the question
// itself (the records tab edits the summary); the overlay cannot, so it refuses
// and says which question is in the way.
func (a *App) OpenLive() (Layout, error) {
	if kind := a.unansweredQuestionKind(); kind != BubbleNone {
		a.DebugLog("live: refused, a " + kind + " bubble is waiting for an answer")
		a.mu.Lock()
		layout := a.layout
		a.mu.Unlock()
		return layout, errors.New(questionOwnsTheSlot(kind))
	}

	a.mu.Lock()
	wasBubble := a.bubble != nil
	a.setBubbleLocked(nil)
	a.panelOpen = false
	a.setLiveLocked(true)
	a.mu.Unlock()
	if wasBubble {
		a.emitBubble(BubbleNone, false)
	}
	a.emitLive(true)
	return a.applyWindowGeometry()
}

// CloseLive hides the live conversation overlay.
//
// Closing is not "stop talking": an in-flight reply keeps arriving and is still
// recorded, exactly as it is when the panel is collapsed mid-answer. The overlay
// is a view of the conversation, not the conversation.
//
// It returns the new layout rather than nothing, because closing changes the
// window region: the frontend has to re-place what is left, and re-reading the
// layout separately would be a second round trip during which the page is still
// laid out for the overlay. DismissBubble returns its layout for the same reason.
func (a *App) CloseLive() (Layout, error) {
	a.mu.Lock()
	was := a.liveOpen
	a.setLiveLocked(false)
	// A close that had nothing to do must not touch the window: GetLayout would
	// resolve the native handle to answer, and re-cropping an unchanged window is
	// work with no effect. The last known layout is the right answer.
	layout := a.layout
	a.mu.Unlock()
	if !was {
		return layout, nil
	}
	a.emitLive(false)
	layout, err := a.applyWindowGeometry()
	// The slot is free again, so a reminder that was suppressed while the
	// overlay was up can be delivered now.
	a.makeUpMissedReminder()
	return layout, err
}

// liveIsOpen reports whether the overlay is showing.
func (a *App) liveIsOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.liveOpen
}

// emitLive tells the frontend that the overlay appeared or went away.
//
// The geometry arrives with the layout; this event exists so the frontend can
// run its fade, place the caret and decide about keyboard focus without having
// to diff the layout on every frame.
func (a *App) emitLive(shown bool) {
	if a.ctx == nil {
		return
	}
	a.emitEvent("pet:live", map[string]any{"shown": shown})
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

// DebugLLMState writes the LLM's view to the debug log and returns it.
//
// This is what step P2 of the implementation plan is verified with: the whole
// request path can be checked from the log before any UI is trusted
// (docs/llm-chat.md 11).
func (a *App) DebugLLMState() string {
	v := a.llmView()
	cfg := a.store.Config()
	summary := fmt.Sprintf("llm: enabled=%v ready=%v hasKey=%v source=%s needsKey=%v endpoint=%s model=%s usage=%s",
		v.Enabled, v.Ready, v.HasKey, v.KeySource, v.NeedsKey, cfg.LLMBaseURL, cfg.LLMModel, v.UsageLine)
	if v.NotReadyReason != "" {
		summary += " notReady=" + v.NotReadyReason
	}
	a.DebugLog(summary)
	return summary
}

// logLLMStartup writes one line about the LLM configuration at startup, so a log
// from a user reporting "the chat does not work" already answers the first three
// questions.
func (a *App) logLLMStartup() {
	cfg := a.store.Config()
	view := a.llmView()
	a.DebugLog(fmt.Sprintf("llm: startup enabled=%v provider=%s base=%s model=%s keySource=%s proactive=%v ready=%v",
		cfg.LLMEnabled, cfg.LLMProvider, cfg.LLMBaseURL, cfg.LLMModel, view.KeySource, cfg.LLMProactive, view.Ready))
	if cfg.LLMEnabled && !view.Ready {
		log.Printf("pet: LLM is enabled but not ready: %s", view.NotReadyReason)
	}
}
