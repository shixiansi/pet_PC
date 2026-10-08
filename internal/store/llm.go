package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// LLM defaults and ranges (docs/llm-chat.md 6.1).
//
// As with the reminder settings, most minimums are non-zero so that Normalise
// can read a zero as "this key was absent from an older config.json" and
// substitute the default. The exceptions are called out where they occur,
// because a zero that is a legitimate value must not be silently replaced.
const (
	// ProviderDeepSeek is the default preset. Nothing in the client depends on
	// it: the URL and model strings are what decide where a request goes, so a
	// user who edits config.json by hand gets exactly what they wrote.
	ProviderDeepSeek = "deepseek"
	ProviderCustom   = "custom"

	DefaultLLMBaseURL = "https://api.deepseek.com/v1"
	DefaultLLMModel   = "deepseek-chat"

	DefaultLLMSystemPrompt = "你是一只住在用户桌面上的小猫桌宠，名字叫「桌宠」。\n" +
		"说话简短、口语化、有点活泼，像朋友而不是客服。默认一到三句话，不要长篇大论，不要用 Markdown 标题和列表。\n" +
		"你不知道的事情就直说不知道，绝不编造用户的待办、日程或已经完成的事。\n" +
		"如果用户想让你修改待办或番茄钟，就告诉他这个版本还做不到，让他自己在面板里改。"

	DefaultLLMTemperature = 0.8
	// MinLLMTemperature..MaxLLMTemperature bracket the values the OpenAI schema
	// accepts. 0 is inside the range and means "as deterministic as possible".
	MinLLMTemperature = 0.0
	MaxLLMTemperature = 2.0

	DefaultLLMMaxTokens = 800
	MinLLMMaxTokens     = 1
	MaxLLMMaxTokens     = 8192

	DefaultLLMTimeoutSec = 90
	MinLLMTimeoutSec     = 10
	MaxLLMTimeoutSec     = 600

	DefaultLLMHistoryTurns = 12
	MinLLMHistoryTurns     = 0
	MaxLLMHistoryTurns     = 50

	DefaultLLMProactiveMinGapMin = 60
	MinLLMProactiveMinGapMin     = 0
	MaxLLMProactiveMinGapMin     = 1440

	DefaultLLMDailyBudget = 200
	MinLLMDailyBudget     = 1
	MaxLLMDailyBudget     = 10000

	// llmHistoryLimit caps the persisted transcript. A rolling window is used
	// rather than a growing file because the file is rewritten on every turn:
	// an unbounded one would make each save slower than the request it is
	// recording.
	llmHistoryLimit = 200
)

// ProviderPreset is one entry in the settings panel's service dropdown.
//
// Presets only fill in the URL and model fields; they are not consulted when a
// request is built. That keeps the "custom" case from being a special case in
// the client: it is the same two strings either way.
type ProviderPreset struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
	// Hint is shown under the dropdown: what the user needs to know before
	// picking this one (where to get a key, whether it is local).
	Hint string `json:"hint"`
	// NeedsKey is false for a local server, where any placeholder will do.
	NeedsKey bool `json:"needsKey"`
}

// LLMProviders returns the built-in presets, in menu order.
func LLMProviders() []ProviderPreset {
	return []ProviderPreset{
		{
			ID: ProviderDeepSeek, Label: "DeepSeek（默认）",
			BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat",
			Hint:     "需要在 platform.deepseek.com 申请 API Key。价格便宜，中文好。",
			NeedsKey: true,
		},
		{
			ID: "moonshot", Label: "Kimi（月之暗面）",
			BaseURL: "https://api.moonshot.cn/v1", Model: "moonshot-v1-8k",
			Hint:     "在 platform.moonshot.cn 申请 Key。",
			NeedsKey: true,
		},
		{
			ID: "zhipu", Label: "智谱 GLM",
			BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4-flash",
			Hint:     "在 open.bigmodel.cn 申请 Key。glm-4-flash 有免费额度。",
			NeedsKey: true,
		},
		{
			ID: "dashscope", Label: "通义千问（阿里云）",
			BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus",
			Hint:     "在阿里云百炼控制台申请 Key，使用「兼容模式」地址。",
			NeedsKey: true,
		},
		{
			ID: "openai", Label: "OpenAI",
			BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini",
			Hint:     "需要能直连 api.openai.com 的网络环境。",
			NeedsKey: true,
		},
		{
			ID: "ollama", Label: "本机 Ollama（数据不出本机）",
			BaseURL: "http://127.0.0.1:11434/v1", Model: "qwen2.5:7b",
			Hint:     "需要先在本机装好 Ollama 并 pull 对应模型。密钥随便填，不会用到。",
			NeedsKey: false,
		},
		{
			ID: ProviderCustom, Label: "自定义（OpenAI 兼容）",
			BaseURL: "", Model: "",
			Hint:     "任何兼容 /chat/completions 的服务，包括 one-api、new-api 等网关。",
			NeedsKey: true,
		},
	}
}

// LLMProviderByID returns the preset with the given id.
func LLMProviderByID(id string) (ProviderPreset, bool) {
	for _, p := range LLMProviders() {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderPreset{}, false
}

// NormaliseLLM clamps the LLM settings that came from disk or from the UI.
//
// The "absent key means default" trick works differently here than for the
// integers: readJSON decodes on top of DefaultConfig, so a key that is missing
// from an older config.json simply keeps its default value and never reaches
// this function as a zero. The string checks below are for the UI, which can
// legitimately send an empty field, and for a file that was edited by hand.
func (c *Config) NormaliseLLM() {
	if _, ok := LLMProviderByID(c.LLMProvider); !ok {
		c.LLMProvider = ProviderDeepSeek
	}
	c.LLMBaseURL = strings.TrimSpace(c.LLMBaseURL)
	if c.LLMBaseURL == "" {
		c.LLMBaseURL = DefaultLLMBaseURL
	}
	// Trailing slashes turn "…/v1" + "/chat/completions" into a double slash.
	// Some gateways 404 on that, so it is normalised away here rather than in
	// the client, where it would have to be remembered at every call site.
	c.LLMBaseURL = strings.TrimRight(c.LLMBaseURL, "/")

	c.LLMModel = strings.TrimSpace(c.LLMModel)
	if c.LLMModel == "" {
		c.LLMModel = DefaultLLMModel
	}
	// A prompt the user has emptied out is not the same as "unset": an empty
	// system message is a legitimate (if terse) choice, so it is left alone
	// rather than being overwritten with the persona on the next launch.

	c.LLMTemperature = clampFloatOr(c.LLMTemperature, MinLLMTemperature, MaxLLMTemperature, DefaultLLMTemperature)
	c.LLMMaxTokens = clampOr(c.LLMMaxTokens, MinLLMMaxTokens, MaxLLMMaxTokens, DefaultLLMMaxTokens)
	c.LLMTimeoutSec = clampOr(c.LLMTimeoutSec, MinLLMTimeoutSec, MaxLLMTimeoutSec, DefaultLLMTimeoutSec)
	// 0 history turns ("stateless") and 0 gap ("no minimum") are both legitimate.
	c.LLMHistoryTurns = clampOr(c.LLMHistoryTurns, MinLLMHistoryTurns, MaxLLMHistoryTurns, DefaultLLMHistoryTurns)
	c.LLMProactiveMinGapMin = clampOr(c.LLMProactiveMinGapMin, MinLLMProactiveMinGapMin, MaxLLMProactiveMinGapMin, DefaultLLMProactiveMinGapMin)
	c.LLMDailyBudget = clampOr(c.LLMDailyBudget, MinLLMDailyBudget, MaxLLMDailyBudget, DefaultLLMDailyBudget)
}

// clampFloatOr returns v when it lies within [min,max], and fallback otherwise.
//
// This is the float twin of clampOr, needed because temperature's valid range
// includes 0 and the integer helper's callers all assume it does not.
func clampFloatOr(v, min, max, fallback float64) float64 {
	if v < min || v > max {
		return fallback
	}
	return v
}

// ChatEndpoint joins the configured base URL with the chat path.
//
// It is a method rather than a bare string concat in the client so that the one
// place that knows the path also owns the trailing-slash question.
func (c Config) ChatEndpoint() string {
	return strings.TrimRight(c.LLMBaseURL, "/") + "/chat/completions"
}

// --- daily usage ----------------------------------------------------------

// LLMUsage counts requests per local day, so the budget is enforced on a clock
// the user recognises rather than on a rolling 24 hours.
type LLMUsage struct {
	// Date is the local day the counters below belong to, "2006-01-02".
	Date string `json:"date"`
	// Requests is how many chat completions have been started today. It counts
	// attempts, not successes: a failing request that is retried still costs a
	// round trip, and the budget exists to bound spending.
	Requests int `json:"requests"`
	// PromptTokens and CompletionTokens are the totals reported by the server.
	// They are informational (the panel shows them); the budget is enforced on
	// Requests, because a provider that does not report usage would otherwise
	// silently disable the cap.
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
}

// UsageDate renders the local day key for now.
func UsageDate(now time.Time) string { return now.Format("2006-01-02") }

// normaliseUsage rolls a record over when the day has changed.
//
// Rolling over here rather than on a timer means there is no midnight job to
// miss: the first request after midnight sees a stale date and resets.
func (u LLMUsage) normalise(now time.Time) LLMUsage {
	today := UsageDate(now)
	if u.Date != today {
		return LLMUsage{Date: today}
	}
	return u
}

// LLMUsageToday returns today's counters, already rolled over if the stored
// record belongs to an earlier day.
func (s *Store) LLMUsageToday(now time.Time) LLMUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.llmUsage.normalise(now)
}

// AddLLMUsage records one request and its token counts, returning the updated
// totals.
//
// A failure to persist is returned but is NOT fatal at the call site: the usage
// file is regenerable state, and refusing to answer the user because a counter
// could not be written would be the wrong trade (the same reasoning as
// pomodoro.json, as opposed to the records archive).
func (s *Store) AddLLMUsage(promptTokens, completionTokens int, now time.Time) (LLMUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.llmUsage.normalise(now)
	u.Requests++
	u.addTokens(promptTokens, completionTokens)
	s.llmUsage = u
	if err := writeJSON(s.llmUsagePath(), u); err != nil {
		return u, err
	}
	return u, nil
}

// AddLLMUsageTokens records token counts WITHOUT counting another request.
//
// It exists because a request is counted when it is sent (the budget must cover
// attempts, not successes) and its tokens only arrive at the end. Using
// AddLLMUsage for both would make one message count as two against the daily
// cap.
func (s *Store) AddLLMUsageTokens(promptTokens, completionTokens int, now time.Time) (LLMUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.llmUsage.normalise(now)
	u.addTokens(promptTokens, completionTokens)
	s.llmUsage = u
	if err := writeJSON(s.llmUsagePath(), u); err != nil {
		return u, err
	}
	return u, nil
}

// addTokens folds in a token report, ignoring the zero values a server that does
// not report usage produces.
func (u *LLMUsage) addTokens(promptTokens, completionTokens int) {
	if promptTokens > 0 {
		u.PromptTokens += promptTokens
	}
	if completionTokens > 0 {
		u.CompletionTokens += completionTokens
	}
}

// ResetLLMUsage zeroes today's counters. The panel offers this so that a user
// who hits the budget on purpose can get going again without editing JSON.
func (s *Store) ResetLLMUsage(now time.Time) (LLMUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := LLMUsage{Date: UsageDate(now)}
	s.llmUsage = u
	if err := writeJSON(s.llmUsagePath(), u); err != nil {
		return u, err
	}
	return u, nil
}

// LLMOverBudget reports whether today's request count has reached the cap.
func (u LLMUsage) OverBudget(budget int) bool {
	if budget <= 0 {
		return false
	}
	return u.Requests >= budget
}

// LLMUsageSummary is the one-line description the settings panel shows.
func (u LLMUsage) Summary(budget int) string {
	if budget <= 0 {
		return fmt.Sprintf("今日 %d 次请求", u.Requests)
	}
	return fmt.Sprintf("今日 %d / %d 次请求", u.Requests, budget)
}

// --- chat history ---------------------------------------------------------

// ChatRole values, matching the OpenAI schema.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	// RoleSystem is used for the pet's own error notes ("网络不可达"), so that a
	// failure is visible in the transcript without being mistaken for something
	// the model said.
	RoleSystem = "system"
)

// ChatMessage is one turn in the persisted transcript.
//
// Only what the UI and the next request need is stored: the role, the text and
// when it happened. In particular there is no per-message token accounting,
// because a provider that does not report it would leave half the records at
// zero and the number would then be read as a fact.
type ChatMessage struct {
	Role string    `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
	// Error marks a message that reports a failure rather than a reply. It is
	// kept so the transcript can style it differently and so Replay can skip it.
	Error bool `json:"error,omitempty"`
	// Proactive marks a reply the pet produced on its own initiative rather than
	// in answer to something the user said. The panel labels those rounds, so a
	// conversation that opens with the pet talking does not read as a reply to a
	// question that is missing from the log.
	Proactive bool `json:"proactive,omitempty"`
}

func (s *Store) llmHistoryPath() string { return filepath.Join(s.dir, "llm-history.json") }
func (s *Store) llmUsagePath() string   { return filepath.Join(s.dir, "llm-usage.json") }

// loadLLMHistory reads llm-history.json.
//
// The transcript is user data in the sense that it cannot be regenerated, but it
// is also chat trivia rather than a work record, so a corrupt file is
// quarantined and reported (like todos.json) instead of being silently dropped -
// the user should get the chance to look at the .bak if a long conversation
// mattered to them.
func (s *Store) loadLLMHistory() error {
	var msgs []ChatMessage
	if err := readJSON(s.llmHistoryPath(), &msgs); err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("llm-history.json 无法解析（%v），已从空对话开始", err))
		if backup := quarantine(s.llmHistoryPath()); backup != "" {
			s.warnings[len(s.warnings)-1] += "，原文件已备份为 " + filepath.Base(backup)
		}
		msgs = nil
	}
	if msgs == nil {
		msgs = []ChatMessage{}
	}
	s.chat = trimHistory(msgs, llmHistoryLimit)
	return nil
}

// loadLLMUsage reads llm-usage.json.
//
// This file is pure bookkeeping and is fully regenerable, so a broken one resets
// silently, exactly like pomodoro.json. Rolling the day over is deferred to the
// next read so that there is no midnight job to miss.
func (s *Store) loadLLMUsage() error {
	var u LLMUsage
	if err := readJSON(s.llmUsagePath(), &u); err != nil {
		u = LLMUsage{}
	}
	s.llmUsage = u.normalise(time.Now())
	return nil
}

// trimHistory keeps the newest limit messages.
//
// The cut is moved forward if it would land on an assistant message, so the
// window always starts at something the user said: a transcript that begins with
// a reply to a question nobody asked reads as a bug when it reaches the model.
func trimHistory(msgs []ChatMessage, limit int) []ChatMessage {
	if limit <= 0 || len(msgs) <= limit {
		return msgs
	}
	cut := len(msgs) - limit
	for cut < len(msgs) && msgs[cut].Role == RoleAssistant {
		cut++
	}
	out := make([]ChatMessage, len(msgs)-cut)
	copy(out, msgs[cut:])
	return out
}

// ChatHistory returns a copy of the persisted transcript.
func (s *Store) ChatHistory() []ChatMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ChatMessage, len(s.chat))
	copy(out, s.chat)
	return out
}

// AppendChat adds a message to the transcript and persists it, returning the
// stored message (with its timestamp filled in).
func (s *Store) AppendChat(role, text string, isError bool) (ChatMessage, error) {
	return s.AppendChatMessage(ChatMessage{Role: role, Text: text, Error: isError})
}

// AppendChatMessage adds a fully-formed message to the transcript.
//
// It exists for the proactive replies, which carry a flag the role-and-text
// shorthand cannot express: AppendChat would silently drop it, and the panel
// would then show the pet opening a conversation for no visible reason.
func (s *Store) AppendChatMessage(m ChatMessage) (ChatMessage, error) {
	if m.At.IsZero() {
		m.At = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chat = trimHistory(append(s.chat, m), llmHistoryLimit)
	return m, writeJSON(s.llmHistoryPath(), s.chat)
}

// TruncateChatTo drops every message after index n. It backs "regenerate": the
// question stays, the answer it produced goes away.
func (s *Store) TruncateChatTo(n int) ([]ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 0 {
		n = 0
	}
	if n > len(s.chat) {
		n = len(s.chat)
	}
	s.chat = append([]ChatMessage(nil), s.chat[:n]...)
	return append([]ChatMessage(nil), s.chat...), writeJSON(s.llmHistoryPath(), s.chat)
}

// ClearChat empties the transcript and persists the empty list.
func (s *Store) ClearChat() ([]ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chat = []ChatMessage{}
	return s.chat, writeJSON(s.llmHistoryPath(), s.chat)
}

// Replay returns the last n messages in the shape the API expects, skipping the
// error notes: those are for the user's eyes, and feeding "网络不可达" back to the
// model as its own past statement would have it apologise for something it never
// said.
func (s *Store) Replay(turns int) []ChatMessage {
	if turns <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ChatMessage, 0, turns)
	for i := len(s.chat) - 1; i >= 0 && len(out) < turns; i-- {
		if s.chat[i].Error || s.chat[i].Role == RoleSystem {
			continue
		}
		out = append(out, s.chat[i])
	}
	// Collected newest-first, so reverse into chronological order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
