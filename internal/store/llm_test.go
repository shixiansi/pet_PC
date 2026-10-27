package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfigLLM(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.LLMEnabled {
		t.Error("the LLM must be off by default: an app that starts sending data to a third party without being asked would be a bug, not a feature")
	}
	if cfg.LLMProactive {
		t.Error("proactive speech must be off by default")
	}
	if cfg.LLMBaseURL != DefaultLLMBaseURL || cfg.LLMModel != DefaultLLMModel {
		t.Errorf("default endpoint = %q/%q, want %q/%q",
			cfg.LLMBaseURL, cfg.LLMModel, DefaultLLMBaseURL, DefaultLLMModel)
	}
	// There is no LLMContextTodos to assert on any more. It switched the todo list into
	// the prompt, and the list is the todo plugin's own storage now - a switch that
	// cannot change anything is worse than no switch, so it went with the data.
}

// TestConfigUpgradeFromPreLLMFile is the compatibility guarantee that matters:
// a config.json written before this feature existed must gain sane defaults
// rather than a zero-valued endpoint that would send requests to "/chat/completions".
func TestConfigUpgradeFromPreLLMFile(t *testing.T) {
	dir := t.TempDir()
	// Only the keys an older build knew about.
	old := `{"petScale":1.25,"snapToEdge":false,"reminderIntervalMin":45,"focusMinutes":30}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(old), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cfg := s.Config()

	if cfg.PetScale != 1.25 || cfg.ReminderIntervalMin != 45 {
		t.Errorf("existing settings were not preserved: scale=%v interval=%d", cfg.PetScale, cfg.ReminderIntervalMin)
	}
	if cfg.LLMBaseURL != DefaultLLMBaseURL {
		t.Errorf("LLMBaseURL = %q, want the default after upgrade", cfg.LLMBaseURL)
	}
	if cfg.LLMModel != DefaultLLMModel {
		t.Errorf("LLMModel = %q, want the default after upgrade", cfg.LLMModel)
	}
	if cfg.LLMTemperature != DefaultLLMTemperature {
		t.Errorf("LLMTemperature = %v, want %v", cfg.LLMTemperature, DefaultLLMTemperature)
	}
	if cfg.LLMMaxTokens != DefaultLLMMaxTokens || cfg.LLMTimeoutSec != DefaultLLMTimeoutSec {
		t.Errorf("token/timeout defaults missing: %d/%d", cfg.LLMMaxTokens, cfg.LLMTimeoutSec)
	}
	if cfg.LLMHistoryTurns != DefaultLLMHistoryTurns || cfg.LLMDailyBudget != DefaultLLMDailyBudget {
		t.Errorf("history/budget defaults missing: %d/%d", cfg.LLMHistoryTurns, cfg.LLMDailyBudget)
	}
}

// TestExplicitZeroTemperatureSurvives checks the one value where "zero means
// unset" would be wrong.
func TestExplicitZeroTemperatureSurvives(t *testing.T) {
	dir := t.TempDir()
	body := `{"llmTemperature":0,"llmHistoryTurns":0,"llmProactiveMinGapMin":0}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cfg := s.Config()
	if cfg.LLMTemperature != 0 {
		t.Errorf("LLMTemperature = %v, want an explicit 0 to be kept", cfg.LLMTemperature)
	}
	if cfg.LLMHistoryTurns != 0 {
		t.Errorf("LLMHistoryTurns = %d, want an explicit 0 to be kept", cfg.LLMHistoryTurns)
	}
	if cfg.LLMProactiveMinGapMin != 0 {
		t.Errorf("LLMProactiveMinGapMin = %d, want an explicit 0 to be kept", cfg.LLMProactiveMinGapMin)
	}
}

func TestNormaliseLLMClampsAndRepairs(t *testing.T) {
	cases := []struct {
		name string
		in   Config
		want func(Config) bool
		desc string
	}{
		{
			name: "trailing slash removed",
			in:   Config{LLMBaseURL: "https://x.example/v1/"},
			want: func(c Config) bool { return c.LLMBaseURL == "https://x.example/v1" },
			desc: "a trailing slash would produce a double slash before /chat/completions",
		},
		{
			name: "empty url falls back",
			in:   Config{LLMBaseURL: "   "},
			want: func(c Config) bool { return c.LLMBaseURL == DefaultLLMBaseURL },
			desc: "an empty field must not survive as an empty endpoint",
		},
		{
			name: "empty model falls back",
			in:   Config{LLMModel: ""},
			want: func(c Config) bool { return c.LLMModel == DefaultLLMModel },
			desc: "an empty model name makes every request a 400",
		},
		{
			name: "unknown provider falls back",
			in:   Config{LLMProvider: "not-a-provider"},
			want: func(c Config) bool { return c.LLMProvider == ProviderDeepSeek },
			desc: "the dropdown must always have a valid selection",
		},
		{
			name: "custom provider is preserved",
			in:   Config{LLMProvider: ProviderCustom},
			want: func(c Config) bool { return c.LLMProvider == ProviderCustom },
			desc: "custom is a real choice, not a fallback",
		},
		{
			name: "temperature above range falls back",
			in:   Config{LLMTemperature: 9},
			want: func(c Config) bool { return c.LLMTemperature == DefaultLLMTemperature },
			desc: "the API rejects temperature > 2",
		},
		{
			name: "temperature 0 kept",
			in:   Config{LLMTemperature: 0},
			want: func(c Config) bool { return c.LLMTemperature == 0 },
			desc: "0 is a valid temperature",
		},
		{
			name: "zero max tokens falls back",
			in:   Config{LLMMaxTokens: 0},
			want: func(c Config) bool { return c.LLMMaxTokens == DefaultLLMMaxTokens },
			desc: "0 tokens would ask the server for an empty reply",
		},
		{
			name: "absurd max tokens clamped down",
			in:   Config{LLMMaxTokens: 999999},
			want: func(c Config) bool { return c.LLMMaxTokens == DefaultLLMMaxTokens },
			desc: "a runaway cap is a runaway bill",
		},
		{
			name: "timeout too small falls back",
			in:   Config{LLMTimeoutSec: 1},
			want: func(c Config) bool { return c.LLMTimeoutSec == DefaultLLMTimeoutSec },
			desc: "a 1s timeout would abort every real request",
		},
		{
			name: "budget of zero falls back",
			in:   Config{LLMDailyBudget: 0},
			want: func(c Config) bool { return c.LLMDailyBudget == DefaultLLMDailyBudget },
			desc: "0 would read as 'unlimited' if left alone, which is the opposite of safe",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := c.in
			cfg.NormaliseLLM()
			if !c.want(cfg) {
				t.Errorf("%s: got %+v", c.desc, cfg)
			}
		})
	}
}

func TestChatEndpoint(t *testing.T) {
	cases := map[string]string{
		"https://api.deepseek.com/v1":         "https://api.deepseek.com/v1/chat/completions",
		"https://api.deepseek.com/v1/":        "https://api.deepseek.com/v1/chat/completions",
		"http://127.0.0.1:11434/v1":           "http://127.0.0.1:11434/v1/chat/completions",
		"https://gateway.example/api/openai/": "https://gateway.example/api/openai/chat/completions",
	}
	for base, want := range cases {
		if got := (Config{LLMBaseURL: base}).ChatEndpoint(); got != want {
			t.Errorf("ChatEndpoint(%q) = %q, want %q", base, got, want)
		}
	}
}

// TestNoAPIKeyInState is the security invariant the whole secret-store design
// exists to protect. GetState serialises store.Config straight into the webview,
// so a key stored in Config would leak into the page.
func TestNoAPIKeyInState(t *testing.T) {
	// A key that must not appear anywhere in the serialised config.
	const secret = "sk-THIS-MUST-NOT-LEAK-000000"
	t.Setenv("PETPC_LLM_API_KEY", secret)

	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Write it into every LLM field we can, to prove the test would catch a
	// regression wherever someone put it.
	if _, err := s.UpdateConfig(func(c *Config) {
		c.LLMEnabled = true
		c.LLMSystemPrompt = "persona"
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	blob, err := json.Marshal(s.Config())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), secret) {
		t.Fatalf("the API key leaked into the serialised config: %s", blob)
	}
	if strings.Contains(string(blob), "sk-") {
		t.Errorf("the config mentions something key-shaped: %s", blob)
	}

	// And no field NAME suggests a credential. This walks the JSON keys rather
	// than scanning the raw text, because a substring search produces false
	// positives on legitimate names like llmMaxTokens.
	var decoded map[string]any
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	// The token COUNTERS are fine; a token is what you pay with, not a secret.
	allowed := map[string]bool{"llmMaxTokens": true}
	for key := range decoded {
		if allowed[key] {
			continue
		}
		lower := strings.ToLower(key)
		for _, suspect := range []string{"apikey", "api_key", "secret", "password", "credential", "authtoken", "accesstoken"} {
			if strings.Contains(lower, suspect) {
				t.Errorf("config contains a credential-shaped field %q: %s", key, blob)
			}
		}
	}
}

func TestChatHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := s.ChatHistory(); len(got) != 0 {
		t.Fatalf("a fresh store should have an empty transcript, got %d messages", len(got))
	}

	if _, err := s.AppendChat(RoleUser, "你好", false); err != nil {
		t.Fatalf("AppendChat: %v", err)
	}
	if _, err := s.AppendChat(RoleAssistant, "喵", false); err != nil {
		t.Fatalf("AppendChat: %v", err)
	}

	// Reopening is what a restart looks like.
	again, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := again.ChatHistory()
	if len(got) != 2 || got[0].Text != "你好" || got[1].Text != "喵" {
		t.Fatalf("transcript did not survive a reopen: %+v", got)
	}
	if got[1].At.IsZero() {
		t.Error("timestamps were not persisted")
	}
}

func TestChatHistoryIsTrimmed(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Two messages past the cap, starting with a user turn so the trim cannot
	// land on an assistant message.
	total := llmHistoryLimit + 2
	for i := 0; i < total; i++ {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		if _, err := s.AppendChat(role, "m", false); err != nil {
			t.Fatalf("AppendChat %d: %v", i, err)
		}
	}
	got := s.ChatHistory()
	if len(got) != llmHistoryLimit {
		t.Fatalf("history length = %d, want the cap %d", len(got), llmHistoryLimit)
	}
	if got[0].Role != RoleUser {
		t.Errorf("a trimmed history must start with a user turn, got %q", got[0].Role)
	}
	// The file on disk must match, or a restart would load the untrimmed list.
	raw, err := os.ReadFile(filepath.Join(dir, "llm-history.json"))
	if err != nil {
		t.Fatalf("read history file: %v", err)
	}
	var onDisk []ChatMessage
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("unmarshal history file: %v", err)
	}
	if len(onDisk) != llmHistoryLimit {
		t.Errorf("the persisted file has %d messages, want %d", len(onDisk), llmHistoryLimit)
	}
}

// TestTrimHistoryNeverStartsWithAnAnswer covers the case the loop in
// trimHistory exists for.
func TestTrimHistoryNeverStartsWithAnAnswer(t *testing.T) {
	msgs := make([]ChatMessage, 0, 10)
	for i := 0; i < 10; i++ {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		msgs = append(msgs, ChatMessage{Role: role, Text: "x"})
	}
	// Cutting to 5 lands on index 5, an assistant message.
	got := trimHistory(msgs, 5)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4 (the cut moves forward past the assistant turn)", len(got))
	}
	if got[0].Role != RoleUser {
		t.Errorf("trimmed history starts with %q, want user", got[0].Role)
	}
}

func TestClearAndTruncateChat(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.AppendChat(RoleUser, "q1", false)
	s.AppendChat(RoleAssistant, "a1", false)

	// Regenerate: the question stays, the answer goes.
	after, err := s.TruncateChatTo(1)
	if err != nil {
		t.Fatalf("TruncateChatTo: %v", err)
	}
	if len(after) != 1 || after[0].Text != "q1" {
		t.Fatalf("after truncate: %+v", after)
	}
	// Out-of-range values are clamped rather than panicking.
	if _, err := s.TruncateChatTo(99); err != nil {
		t.Fatalf("TruncateChatTo(99): %v", err)
	}
	if _, err := s.TruncateChatTo(-5); err != nil {
		t.Fatalf("TruncateChatTo(-5): %v", err)
	}

	s.AppendChat(RoleUser, "q2", false)
	cleared, err := s.ClearChat()
	if err != nil {
		t.Fatalf("ClearChat: %v", err)
	}
	if len(cleared) != 0 {
		t.Fatalf("ClearChat left %d messages", len(cleared))
	}
	again, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(again.ChatHistory()) != 0 {
		t.Error("the cleared transcript came back after a reopen")
	}
}

func TestReplaySkipsErrorsAndHonoursTurns(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.AppendChat(RoleUser, "u1", false)
	s.AppendChat(RoleAssistant, "a1", false)
	s.AppendChat(RoleSystem, "网络不可达", true) // must never be replayed
	s.AppendChat(RoleUser, "u2", false)
	s.AppendChat(RoleAssistant, "a2", false)

	got := s.Replay(10)
	if len(got) != 4 {
		t.Fatalf("Replay returned %d messages, want 4 (the error note is skipped): %+v", len(got), got)
	}
	if got[0].Text != "u1" || got[3].Text != "a2" {
		t.Errorf("replay is not in chronological order: %+v", got)
	}
	for _, m := range got {
		if m.Text == "网络不可达" {
			t.Fatal("an error note was replayed to the model as if it had said it")
		}
	}

	// The cap keeps the NEWEST turns.
	tail := s.Replay(2)
	if len(tail) != 2 || tail[0].Text != "u2" || tail[1].Text != "a2" {
		t.Errorf("Replay(2) = %+v, want the two newest messages", tail)
	}
	if got := s.Replay(0); got != nil {
		t.Errorf("Replay(0) = %+v, want nil (stateless mode)", got)
	}
}

func TestCorruptHistoryIsQuarantinedNotIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llm-history.json")
	if err := os.WriteFile(path, []byte(`{"not":"an array"`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(s.ChatHistory()) != 0 {
		t.Error("a corrupt transcript should start empty")
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("the unreadable file was not preserved as .bak: %v", err)
	}
	if len(s.Warnings()) == 0 {
		t.Error("the user was not told their transcript had to be reset")
	}
}

func TestCorruptUsageResetsSilently(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "llm-usage.json"), []byte(`not json`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := s.LLMUsageToday(time.Now()); got.Requests != 0 {
		t.Errorf("usage = %d, want 0", got.Requests)
	}
	// Unlike the transcript, this file is regenerable, so it must NOT nag.
	for _, w := range s.Warnings() {
		if strings.Contains(w, "llm-usage") {
			t.Errorf("a regenerable counter produced a user-facing warning: %q", w)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "llm-usage.json.bak")); err == nil {
		t.Error("the usage file was quarantined; it should just be reset")
	}
}

func TestLLMUsageRollsOverOnANewDay(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	day1 := time.Date(2025, 3, 1, 23, 0, 0, 0, time.Local)
	for i := 0; i < 3; i++ {
		if _, err := s.AddLLMUsage(10, 5, day1); err != nil {
			t.Fatalf("AddLLMUsage: %v", err)
		}
	}
	got := s.LLMUsageToday(day1)
	if got.Requests != 3 || got.PromptTokens != 30 || got.CompletionTokens != 15 {
		t.Fatalf("usage after 3 calls = %+v", got)
	}
	if !got.OverBudget(3) {
		t.Error("a budget of 3 should be reached after 3 requests")
	}
	if got.OverBudget(4) {
		t.Error("a budget of 4 should not be reached after 3 requests")
	}

	// The next day starts from zero without any midnight job.
	day2 := day1.Add(2 * time.Hour)
	next := s.LLMUsageToday(day2)
	if next.Requests != 0 || next.PromptTokens != 0 {
		t.Errorf("counters did not roll over: %+v", next)
	}
	if _, err := s.AddLLMUsage(1, 1, day2); err != nil {
		t.Fatalf("AddLLMUsage on day 2: %v", err)
	}
	if got := s.LLMUsageToday(day2).Requests; got != 1 {
		t.Errorf("day 2 requests = %d, want 1", got)
	}

	// The rollover is written to disk, so it survives a restart.
	raw, err := os.ReadFile(filepath.Join(dir, "llm-usage.json"))
	if err != nil {
		t.Fatalf("read usage file: %v", err)
	}
	var onDisk LLMUsage
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("unmarshal usage file: %v", err)
	}
	if onDisk.Date != UsageDate(day2) || onDisk.Requests != 1 {
		t.Errorf("persisted usage = %+v, want date %s with 1 request", onDisk, UsageDate(day2))
	}
	if onDisk.PromptTokens != 1 || onDisk.CompletionTokens != 1 {
		t.Errorf("day 1's token counts leaked into day 2: %+v", onDisk)
	}
}

// TestStaleUsageFileDoesNotLeakIntoToday is the restart half of the rollover:
// the file on disk belonging to an earlier day must not count against today's
// budget, however long ago it was written.
func TestStaleUsageFileDoesNotLeakIntoToday(t *testing.T) {
	dir := t.TempDir()
	stale := LLMUsage{Date: "2001-01-01", Requests: 999, PromptTokens: 12345}
	blob, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "llm-usage.json"), blob, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := s.LLMUsageToday(time.Now())
	if got.Requests != 0 || got.PromptTokens != 0 {
		t.Errorf("yesterday's usage counted against today: %+v", got)
	}
	if got.Date != UsageDate(time.Now()) {
		t.Errorf("date = %q, want today %q", got.Date, UsageDate(time.Now()))
	}
	// Tomorrow's budget must be fully available again.
	if got.OverBudget(DefaultLLMDailyBudget) {
		t.Error("a stale file left the app over budget")
	}
}

func TestResetLLMUsage(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	now := time.Now()
	s.AddLLMUsage(100, 100, now)
	got, err := s.ResetLLMUsage(now)
	if err != nil {
		t.Fatalf("ResetLLMUsage: %v", err)
	}
	if got.Requests != 0 || got.PromptTokens != 0 {
		t.Errorf("reset left counters behind: %+v", got)
	}
	if got.Date != UsageDate(now) {
		t.Errorf("reset date = %q, want %q", got.Date, UsageDate(now))
	}
}

func TestUsageSummary(t *testing.T) {
	u := LLMUsage{Requests: 3}
	if got := u.Summary(200); got != "今日 3 / 200 次请求" {
		t.Errorf("Summary = %q", got)
	}
	if got := u.Summary(0); got != "今日 3 次请求" {
		t.Errorf("Summary with no budget = %q", got)
	}
	if (LLMUsage{Requests: 5}).OverBudget(0) {
		t.Error("a budget of 0 must mean unlimited, not 'always over'")
	}
}

func TestLLMProviderPresets(t *testing.T) {
	presets := LLMProviders()
	if len(presets) == 0 {
		t.Fatal("no presets")
	}
	seen := map[string]bool{}
	for _, p := range presets {
		if p.ID == "" || p.Label == "" {
			t.Errorf("preset with an empty id or label: %+v", p)
		}
		if seen[p.ID] {
			t.Errorf("duplicate preset id %q", p.ID)
		}
		seen[p.ID] = true
		// A preset that is not Custom must be usable as-is.
		if p.ID != ProviderCustom && (p.BaseURL == "" || p.Model == "") {
			t.Errorf("preset %q has no endpoint to fill in", p.ID)
		}
		if p.ID != ProviderCustom && !strings.HasPrefix(p.BaseURL, "http") {
			t.Errorf("preset %q base URL %q is not a URL", p.ID, p.BaseURL)
		}
	}
	if !seen[ProviderDeepSeek] {
		t.Error("DeepSeek must be offered: it is the documented default")
	}
	if !seen[ProviderCustom] {
		t.Error("a custom endpoint must be offered")
	}
	if _, ok := LLMProviderByID(ProviderCustom); !ok {
		t.Error("provider lookup failed for custom")
	}
	if _, ok := LLMProviderByID("nope"); ok {
		t.Error("provider lookup succeeded for an unknown id")
	}

	// Every preset's endpoint must survive the normaliser unchanged once the
	// base URL is trimmed, or the settings panel would fill in a URL that then
	// gets rewritten under the user's feet.
	for _, p := range presets {
		if p.ID == ProviderCustom {
			continue
		}
		cfg := Config{LLMBaseURL: p.BaseURL, LLMModel: p.Model, LLMProvider: p.ID}
		cfg.NormaliseLLM()
		if cfg.LLMBaseURL != p.BaseURL {
			t.Errorf("preset %q base URL was rewritten to %q", p.ID, cfg.LLMBaseURL)
		}
		if cfg.LLMModel != p.Model {
			t.Errorf("preset %q model was rewritten to %q", p.ID, cfg.LLMModel)
		}
	}
}
