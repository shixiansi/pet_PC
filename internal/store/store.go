// Package store provides JSON-file persistence for the desktop pet's
// configuration, todo list and schedule.
//
// Everything lives in a single directory (see DefaultDir) so the app is fully
// portable: copy the folder and the pet keeps its settings, picture and todos.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Todo is a single todo / schedule entry.
//
// Due is a pointer so that an entry may legitimately have no due date;
// it marshals as RFC3339 (or is omitted entirely).
type Todo struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Notes     string     `json:"notes,omitempty"`
	Due       *time.Time `json:"due,omitempty"`
	Done      bool       `json:"done"`
	CreatedAt time.Time  `json:"createdAt"`
	DoneAt    *time.Time `json:"doneAt,omitempty"`
}

// Overdue reports whether the entry is still open and its due date has passed.
func (t Todo) Overdue(now time.Time) bool {
	return !t.Done && t.Due != nil && t.Due.Before(now)
}

// Position remembers where the pet was last placed, in logical (DPI
// independent) pixels, matching what the Wails runtime uses.
type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Config holds user preferences. Zero values are normalised by DefaultConfig.
type Config struct {
	// PetImageFile is the file name (not path) of the user's picture inside the
	// data directory. Empty means "use the bundled default pet".
	PetImageFile string `json:"petImageFile,omitempty"`
	// OriginalPath is kept only so the settings panel can show where the
	// picture came from.
	OriginalPath string `json:"originalPath,omitempty"`

	// PetScale multiplies the base pet size.
	PetScale float64 `json:"petScale"`
	// Position is the last known window position.
	Position *Position `json:"position,omitempty"`
	// SnapToEdge enables magnetic snapping to screen edges after a drag.
	SnapToEdge bool `json:"snapToEdge"`
	// EdgeMargin is the gap left between the pet and the screen edge.
	EdgeMargin int `json:"edgeMargin"`

	// Autostart mirrors the Windows registry state; it is persisted so the
	// settings panel can render before the registry is read.
	Autostart bool `json:"autostart"`

	// MarketBaseURL is the plugin market's base address.
	//
	// It lives in the config rather than in the code because the market is a
	// separate repository that may move, be mirrored, or be forked; the whole
	// catalogue follows one edit here. The DEFAULT points at
	// raw.githubusercontent.com through the gh-proxy.com mirror, because that
	// host is not reachable from mainland China and this project already routes
	// around it elsewhere.
	//
	// An explicit empty string is a legitimate value meaning "use the built-in
	// sample catalogue", which is why Normalise leaves an empty value alone
	// rather than restoring the default. The default is applied by
	// DefaultConfig, so an older config.json with no such key still gets it.
	MarketBaseURL string `json:"marketBaseUrl"`

	// GlobalHotkeys enables the system-wide show/hide shortcuts
	// (Ctrl+Alt+S / Ctrl+Alt+H).
	//
	// It is OFF by default, and that matters more here than for the other
	// switches: a global hot key is taken away from every other application, so
	// claiming Ctrl+Alt+S without being asked could break a shortcut in an
	// editor or an IDE. Opting in is the only polite default.
	GlobalHotkeys bool `json:"globalHotkeys"`

	// --- todo reminders (docs/reminder-pomodoro.md 5.5) ---

	// ReminderOnStartup decides whether a launch enables reminders.
	ReminderOnStartup bool `json:"reminderOnStartup"`
	// ReminderEnabled is the live switch, toggled from the panel and the tray.
	// It is a config value (unlike the pomodoro's) because the scheduler is pure
	// Go: there is no runtime state file for it to live in.
	ReminderEnabled bool `json:"reminderEnabled"`
	// ReminderIntervalMin is the base gap between reminders, in minutes.
	ReminderIntervalMin int `json:"reminderIntervalMin"`
	// ReminderJitterMaxMin is how much randomness is added to each gap, so the
	// pet does not turn into a metronome (docs/reminder-pomodoro.md 5.3).
	ReminderJitterMaxMin int `json:"reminderJitterMaxMin"`
	// ReminderFrom/To are the active hours, "HH:MM".
	ReminderFrom string `json:"reminderFrom"`
	ReminderTo   string `json:"reminderTo"`
	// ReminderMaxItems caps how many todos a bubble lists.
	ReminderMaxItems int `json:"reminderMaxItems"`
	// ReminderAutoHideSec is how long a bubble stays up before fading.
	ReminderAutoHideSec int `json:"reminderAutoHideSec"`
	// RemindWhenEmpty allows a bubble even with nothing due.
	RemindWhenEmpty bool `json:"remindWhenEmpty"`

	// --- LLM chat (docs/llm-chat.md 6.1) ---
	//
	// The API key is deliberately NOT here. GetState hands this whole struct to
	// the webview on every refresh, so a credential stored in it would be one
	// `JSON.stringify(state)` away from leaking into the page. It lives in an
	// encrypted file instead; see desktop.SecretStore.

	// LLMEnabled is the master switch. Off by default, which means the app
	// behaves exactly as it did before this feature existed.
	LLMEnabled bool `json:"llmEnabled"`
	// LLMProvider is the name of the preset the settings UI selected. It only
	// drives that dropdown's display; the URL and model below are what is
	// actually used, so a hand-edited config never disagrees with a request.
	LLMProvider string `json:"llmProvider"`
	// LLMBaseURL is the OpenAI-compatible endpoint root, e.g.
	// "https://api.deepseek.com/v1". The client appends "/chat/completions".
	LLMBaseURL string `json:"llmBaseUrl"`
	// LLMModel is the model name sent in the request body.
	LLMModel string `json:"llmModel"`
	// LLMTemperature is the sampling temperature. 0 is a legitimate value
	// ("be deterministic"), so it is not treated as "unset" here: readJSON
	// decodes on top of DefaultConfig, which means an absent key keeps the
	// default and an explicit 0 stays 0.
	LLMTemperature float64 `json:"llmTemperature"`
	// LLMMaxTokens caps the reply length, in tokens.
	LLMMaxTokens int `json:"llmMaxTokens"`
	// LLMTimeoutSec bounds the whole request, streaming included.
	LLMTimeoutSec int `json:"llmTimeoutSec"`
	// LLMSystemPrompt is the pet's persona and behaviour rules.
	LLMSystemPrompt string `json:"llmSystemPrompt"`
	// LLMHistoryTurns is how many past messages are replayed as context.
	LLMHistoryTurns int `json:"llmHistoryTurns"`
	// LLMContextTodos injects the todo list into the prompt.
	// This is a privacy-relevant switch, so it is visible in the settings UI.
	LLMContextTodos bool `json:"llmContextTodos"`
	// LLMProactive allows the pet to speak up on its own (docs 9). Off by
	// default: an assistant that interrupts without being asked is worse than
	// one that waits to be addressed.
	LLMProactive bool `json:"llmProactive"`
	// LLMProactiveMinGapMin is the minimum gap between two unprompted remarks.
	LLMProactiveMinGapMin int `json:"llmProactiveMinGapMin"`
	// LLMDailyBudget caps requests per local day. A runaway loop here costs
	// real money, so the cap is enforced in code rather than trusted to the
	// user watching the panel.
	LLMDailyBudget int `json:"llmDailyBudget"`

	// There are deliberately NO speech settings here any more.
	//
	// Speech is a PLUGIN (com.shixiansi.plugin-voice). The lines, the character
	// library, the switches and the rendered clips all belong to it, and they live
	// in its own private directory - so the pet has no voice fields to persist and no
	// view to push to its webview.
	//
	// What the pet kept is the part that is genuinely the pet's: it emits
	// timer.phaseEnded and reminder.fired, and it serves the audio.* capability the
	// plugin stores and plays through. Which line to say, in which voice, and whether
	// to speak at all are the plugin's decisions now.
	//
	// The three fields that used to live here (voiceEnabled, voicePomodoro,
	// voiceReminder) are read ONCE by plugins.MigrateVoiceData, which copies their
	// values into the plugin's own settings. They are read from the raw JSON rather
	// than through this struct precisely so they can be deleted: a migration that
	// depended on a field still being declared here could not survive the field being
	// removed. See voice_migrate.go.
}

// ReminderSettings bundles the reminder preferences so the frontend and the
// scheduler can pass them around as one value.
type ReminderSettings struct {
	Enabled     bool   `json:"enabled"`
	IntervalMin int    `json:"intervalMin"`
	JitterMax   int    `json:"jitterMax"`
	From        string `json:"from"`
	To          string `json:"to"`
	MaxItems    int    `json:"maxItems"`
	AutoHideSec int    `json:"autoHideSec"`
	WhenEmpty   bool   `json:"whenEmpty"`
}

// Base (unscaled) window sizes in logical pixels.
const (
	BasePetWidth    = 150
	BasePetHeight   = 200
	PanelWidth      = 380
	PanelHeight     = 520
	DefaultScale    = 1.0
	DefaultEdgeGap  = 8
	MinScale        = 0.5
	MaxScale        = 2.5
	defaultDataName = "PetPC"
)

// DefaultMarketBaseURL is the plugin market address used when the user has not
// chosen one.
//
// It points at raw.githubusercontent.com THROUGH gh-proxy.com, the mirror this
// project already uses: the raw host is unreachable from mainland China, and a
// plugin market nobody can load is not a market. The repository is public, so
// the address needs no credential.
const DefaultMarketBaseURL = "https://gh-proxy.com/https://raw.githubusercontent.com/shixiansi/pet-pc-plugins/main"

// normaliseBaseURL trims a market address and drops a trailing slash, while
// leaving an EMPTY value empty.
//
// The empty case is the reason this is not folded into a clamp: "" is not
// "unset", it is the user asking for the built-in sample source, and rewriting
// it to the default would silently undo that choice. The default only ever
// comes from DefaultConfig, which readJSON decodes on top of.
func normaliseBaseURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	return strings.TrimRight(u, "/")
}

// DefaultConfig returns the configuration used on first launch.
func DefaultConfig() Config {
	return Config{
		PetScale:   DefaultScale,
		SnapToEdge: true,
		EdgeMargin: DefaultEdgeGap,

		// The global shortcuts are off until asked for: see the field's comment.
		GlobalHotkeys: false,

		// Pomodoro settings are NOT here any more: the timer, its phase lengths,
		// its long-break rhythm and its sound all belong to the pomodoro plugin,
		// which keeps them in its own `plugin.config`. A host field left behind
		// would be a switch nothing reads.

		// Reminders: likewise off until asked for.
		ReminderOnStartup:    false,
		ReminderEnabled:      false,
		ReminderIntervalMin:  DefaultReminderIntervalMin,
		ReminderJitterMaxMin: DefaultReminderJitterMaxMin,
		ReminderFrom:         DefaultReminderFrom,
		ReminderTo:           DefaultReminderTo,
		ReminderMaxItems:     DefaultReminderMaxItems,
		ReminderAutoHideSec:  DefaultReminderAutoHideSec,
		RemindWhenEmpty:      false,

		// LLM: off until asked for. The default endpoint is DeepSeek because it
		// is the cheapest of the well-behaved Chinese-language options; nothing
		// else in the code depends on that choice, since only the two strings
		// below decide where a request goes.
		LLMEnabled:            false,
		LLMProvider:           ProviderDeepSeek,
		LLMBaseURL:            DefaultLLMBaseURL,
		LLMModel:              DefaultLLMModel,
		LLMTemperature:        DefaultLLMTemperature,
		LLMMaxTokens:          DefaultLLMMaxTokens,
		LLMTimeoutSec:         DefaultLLMTimeoutSec,
		LLMSystemPrompt:       DefaultLLMSystemPrompt,
		LLMHistoryTurns:       DefaultLLMHistoryTurns,
		LLMContextTodos:       true,
		LLMProactive:          false,
		LLMProactiveMinGapMin: DefaultLLMProactiveMinGapMin,
		LLMDailyBudget:        DefaultLLMDailyBudget,

		MarketBaseURL: DefaultMarketBaseURL,

		// Speech has no defaults here because it has no fields here any more: the
		// voice plugin owns its own switches and stores them in its own directory.
	}
}

// Normalise clamps values that came from disk or from the UI into sane ranges.
//
// A zero is treated as "unset" for every setting whose valid range excludes
// zero, which is what upgrades an older config.json (written before these keys
// existed) into the defaults without a migration step. Booleans cannot be
// distinguished from an absent key this way; their defaults live in
// DefaultConfig, which readJSON decodes on top of, so an absent key keeps the
// default and a present `false` stays false.
func (c *Config) Normalise() {
	if c.PetScale < MinScale || c.PetScale > MaxScale {
		c.PetScale = DefaultScale
	}
	if c.EdgeMargin < 0 || c.EdgeMargin > 400 {
		c.EdgeMargin = DefaultEdgeGap
	}

	c.ReminderIntervalMin = clampOr(c.ReminderIntervalMin, MinReminderIntervalMin, MaxReminderIntervalMin, DefaultReminderIntervalMin)
	// The jitter floor is 0, so an absent key is indistinguishable from an
	// explicit "fire exactly on time". Falling back to 0 rather than to the
	// default keeps a deliberate choice from being silently changed.
	if c.ReminderJitterMaxMin < MinReminderJitterMaxMin || c.ReminderJitterMaxMin > MaxReminderJitterMaxMin {
		c.ReminderJitterMaxMin = MinReminderJitterMaxMin
	}
	c.ReminderMaxItems = clampOr(c.ReminderMaxItems, MinReminderMaxItems, MaxReminderMaxItems, DefaultReminderMaxItems)
	c.ReminderAutoHideSec = clampOr(c.ReminderAutoHideSec, MinReminderAutoHideSec, MaxReminderAutoHideSec, DefaultReminderAutoHideSec)
	c.ReminderFrom = normaliseClock(c.ReminderFrom, DefaultReminderFrom)
	c.ReminderTo = normaliseClock(c.ReminderTo, DefaultReminderTo)

	c.NormaliseLLM()

	// The market address is normalised (trimmed, trailing slash dropped) but an
	// EMPTY value stays empty: blank means "no remote market, use the built-in
	// sample source", which is a choice the user can make deliberately.
	c.MarketBaseURL = normaliseBaseURL(c.MarketBaseURL)
	// No NormaliseVoice: the fixed voice has three booleans and no path, range or
	// cross-field rule, so there is nothing to clamp or repair. The switch that used
	// to live here validated URLs, matched a language against a server's list and
	// clamped five numeric ranges - all of which belonged to the retired service.
	//
	// The synthesis settings that briefly lived here moved to the standalone studio,
	// which owns them in its own file; see docs/voice-studio.md.
}

// clampOr returns v when it lies within [min,max], and fallback otherwise. A
// zero is out of range for every setting that uses this helper, so an absent key
// falls back to its default.
func clampOr(v, min, max, fallback int) int {
	if v < min || v > max {
		return fallback
	}
	return v
}

// Reminder returns the reminder settings as one bundle.
//
// There is no DuringBreak field any more. It existed so the host's scheduler could
// keep quiet while the host's timer was resting; both the scheduler and the timer
// are gone (reminders belong to the todo plugin, the timer to the pomodoro plugin),
// and a field the host no longer consults would only invite a reader to think it
// still did.
func (c Config) Reminder() ReminderSettings {
	return ReminderSettings{
		Enabled:     c.ReminderEnabled,
		IntervalMin: c.ReminderIntervalMin,
		JitterMax:   c.ReminderJitterMaxMin,
		From:        c.ReminderFrom,
		To:          c.ReminderTo,
		MaxItems:    c.ReminderMaxItems,
		AutoHideSec: c.ReminderAutoHideSec,
		WhenEmpty:   c.RemindWhenEmpty,
	}
}

// PetSize returns the scaled pet window size in logical pixels.
func (c Config) PetSize() (int, int) {
	s := c.PetScale
	if s < MinScale || s > MaxScale {
		s = DefaultScale
	}
	w := int(float64(BasePetWidth)*s + 0.5)
	h := int(float64(BasePetHeight)*s + 0.5)
	if w < 60 {
		w = 60
	}
	if h < 60 {
		h = 60
	}
	return w, h
}

// Store is a concurrency-safe JSON store rooted at a single directory.
type Store struct {
	mu  sync.Mutex
	dir string
	cfg Config
	// settingsStamp identifies the settings.json the in-memory cfg came from
	// (mtime ^ size); settingsChecked throttles the stat that reads it. Both are
	// guarded by mu - see refreshSettingsLocked.
	settingsStamp   int64
	settingsChecked time.Time
	todos           []Todo
	warnings        []string
	// chat is the LLM transcript (llm-history.json) and llmUsage today's request
	// counters (llm-usage.json). Both live under the same mutex as everything
	// else, so a turn that is being appended cannot race a read of the config.
	//
	// The API key is NOT kept here: see the note on Config's LLM fields.
	chat     []ChatMessage
	llmUsage LLMUsage
}

// DataDirEnvVar overrides the data directory when set. This gives the app a
// portable mode (point it at a USB stick) and makes the location easy to
// redirect for testing.
const DataDirEnvVar = "PETPC_DATA_DIR"

// DefaultDir returns the per-user data directory, creating it if needed.
//
// The location is %AppData%\PetPC on Windows, which is where per-user
// application data belongs: it is writable without elevation, survives
// upgrades, and is unique per user.
func DefaultDir() (string, error) {
	if custom := strings.TrimSpace(os.Getenv(DataDirEnvVar)); custom != "" {
		dir, err := filepath.Abs(custom)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", DataDirEnvVar, err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create data dir %s: %w", dir, err)
		}
		return dir, nil
	}

	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		// Fall back to the executable's folder so the app still works in
		// environments without a user profile.
		exe, exErr := os.Executable()
		if exErr != nil {
			return "", fmt.Errorf("locate config dir: %w", err)
		}
		base = filepath.Dir(exe)
	}
	dir := filepath.Join(base, defaultDataName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create data dir %s: %w", dir, err)
	}
	return dir, nil
}

// New opens (or creates) a store in dir.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir %s: %w", dir, err)
	}
	s := &Store{dir: dir, cfg: DefaultConfig()}
	if err := s.loadConfig(); err != nil {
		return nil, err
	}
	// Preferences come from settings.json, layered ON TOP of config.json. This is also
	// where an installation that predates the split is migrated - the old file is
	// copied, never consumed.
	if err := s.loadSettings(); err != nil {
		return nil, err
	}
	if err := s.loadTodos(); err != nil {
		return nil, err
	}
	// A broken transcript is quarantined and a broken usage file resets. Neither
	// can stop the pet from starting.
	if err := s.loadLLMHistory(); err != nil {
		return nil, err
	}
	if err := s.loadLLMUsage(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir returns the directory backing the store.
func (s *Store) Dir() string { return s.dir }

// ReadMarketBaseURL returns the market address stored in the config file at dir,
// or "" when there is none to read.
//
// This is a READ-ONLY view of the pet's configuration, and it exists because a
// SEPARATE PROGRAM (the plugin market, tools/plugin-market) seeds its own
// settings file from it once, so a user who chose a mirror keeps it. The name of
// the file belongs here, with the rest of the config code: the market program
// must not learn it, because the moment it can name config.json is the moment it
// can be edited to write it, and "the pet's config is never modified by another
// program" is what keeps a rollback a zero-step operation.
//
// Every failure is "" rather than an error. The caller is seeding a DEFAULT: a
// missing file, an unreadable one, a corrupt one, or one with no marketBaseUrl
// all mean the same thing to it - fall back to the built-in address - and the pet
// reports a broken config through its own quarantine path when IT opens.
func ReadMarketBaseURL(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return ""
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	var raw struct {
		MarketBaseURL string `json:"marketBaseUrl"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ""
	}
	return normaliseBaseURL(raw.MarketBaseURL)
}

func (s *Store) configPath() string { return filepath.Join(s.dir, "config.json") }
func (s *Store) todosPath() string  { return filepath.Join(s.dir, "todos.json") }

// readJSON decodes path into v. A missing file is not an error.
//
// The file is read whole rather than streamed so that a leading UTF-8 byte
// order mark can be stripped: editors such as Notepad write one, and
// encoding/json rejects it. Since users are expected to poke at these files,
// tolerating a BOM avoids an otherwise baffling startup failure.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	if len(bytes.TrimSpace(data)) == 0 {
		return nil // empty file: keep defaults
	}
	if err := json.Unmarshal(data, v); err != nil {
		return err
	}
	return nil
}

// utf8BOM is the UTF-8 byte order mark.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// quarantine renames a file the app could not parse to "<name>.bak" so that the
// unreadable content is preserved for the user instead of being silently
// overwritten on the next save.
func quarantine(path string) string {
	backup := path + ".bak"
	os.Remove(backup) // keep only the most recent bad copy
	if err := os.Rename(path, backup); err != nil {
		return ""
	}
	return backup
}

// writeJSON writes v atomically (temp file + rename) so a crash mid-write
// cannot corrupt the user's data.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// loadConfig reads config.json, falling back to defaults if it is unreadable.
//
// A corrupt or unparseable config must never prevent the pet from starting; the
// bad file is set aside as config.json.bak so nothing is lost.
func (s *Store) loadConfig() error {
	cfg := DefaultConfig()
	if err := readJSON(s.configPath(), &cfg); err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("config.json 无法解析（%v），已重置为默认设置", err))
		if backup := quarantine(s.configPath()); backup != "" {
			s.warnings[len(s.warnings)-1] += "，原文件已备份为 " + filepath.Base(backup)
		}
		cfg = DefaultConfig()
	}
	cfg.Normalise()
	s.cfg = cfg
	return nil
}

// loadTodos reads todos.json, falling back to an empty list if it is unreadable.
func (s *Store) loadTodos() error {
	var todos []Todo
	if err := readJSON(s.todosPath(), &todos); err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("todos.json 无法解析（%v），已从空列表开始", err))
		if backup := quarantine(s.todosPath()); backup != "" {
			s.warnings[len(s.warnings)-1] += "，原文件已备份为 " + filepath.Base(backup)
		}
		todos = nil
	}
	if todos == nil {
		todos = []Todo{}
	}
	s.todos = todos
	return nil
}

// Warnings returns any non-fatal problems found while loading, so the UI can
// tell the user that something was reset.
func (s *Store) Warnings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.warnings))
	copy(out, s.warnings)
	return out
}

// Config returns a copy of the current configuration.
//
// It first gives the settings file a chance to be re-read, which is what makes a
// change made in the control center visible to the running pet without a restart.
// Every reader benefits from this one call site rather than each of the hundred
// places that read a setting doing its own reload.
func (s *Store) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshSettingsLocked()
	return s.cfg
}

// UpdateConfig applies fn to the configuration and persists the result.
func (s *Store) UpdateConfig(fn func(*Config)) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.cfg
	fn(&s.cfg)
	s.cfg.Normalise()
	// Runtime keys only. Writing the whole struct here would make the pet a second
	// writer of the preferences, and would resurrect the legacy keys the migration
	// deliberately left in the old config.json.
	if err := s.writeRuntime(s.cfg); err != nil {
		return s.cfg, err
	}
	// ...but the pet still HAS a settings UI until it moves to the control center, so a
	// preference this call changed has to be persisted somewhere. It goes to
	// settings.json, and ONLY when a preference actually changed: dragging the pet
	// writes the position many times an hour and must never touch the settings file.
	//
	// This is transitional. When the settings UI leaves the pet (ui-rebuild phase 3),
	// no caller changes a preference through here and the branch goes quiet.
	if s.preferencesChanged(before) {
		if err := s.writeSettings(s.cfg); err != nil {
			return s.cfg, err
		}
	}
	return s.cfg, nil
}

// SetPosition records the window position.
func (s *Store) SetPosition(x, y int) error {
	_, err := s.UpdateConfig(func(c *Config) {
		c.Position = &Position{X: x, Y: y}
	})
	return err
}

// Todos returns a copy of the todo list, sorted for display: open entries
// first (soonest due first, undated last), then completed entries newest first.
func (s *Store) Todos() []Todo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Todo, len(s.todos))
	copy(out, s.todos)
	sortTodos(out)
	return out
}

func sortTodos(todos []Todo) {
	sort.SliceStable(todos, func(i, j int) bool {
		a, b := todos[i], todos[j]
		if a.Done != b.Done {
			return !a.Done // open entries first
		}
		if a.Done && b.Done { // both done: most recently completed first
			return doneTime(a).After(doneTime(b))
		}
		switch {
		case a.Due != nil && b.Due != nil:
			return a.Due.Before(*b.Due)
		case a.Due != nil:
			return true // dated before undated
		case b.Due != nil:
			return false
		default:
			return a.CreatedAt.Before(b.CreatedAt)
		}
	})
}

func doneTime(t Todo) time.Time {
	if t.DoneAt != nil {
		return *t.DoneAt
	}
	return t.CreatedAt
}

// SaveTodos replaces the whole list and persists it.
func (s *Store) SaveTodos(todos []Todo) error {
	if todos == nil {
		todos = []Todo{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos = todos
	return writeJSON(s.todosPath(), s.todos)
}

// Stats summarises the list for the on-pet badge.
type Stats struct {
	Total   int `json:"total"`
	Open    int `json:"open"`
	Overdue int `json:"overdue"`
	DueSoon int `json:"dueSoon"`
}

// Stats computes badge counters. "Due soon" means due within the next 24h.
func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var st Stats
	st.Total = len(s.todos)
	for _, t := range s.todos {
		if t.Done {
			continue
		}
		st.Open++
		if t.Due == nil {
			continue
		}
		if t.Due.Before(now) {
			st.Overdue++
		} else if t.Due.Sub(now) <= 24*time.Hour {
			st.DueSoon++
		}
	}
	return st
}

// --- pet picture handling -------------------------------------------------

// allowedImageExts limits what we are willing to copy in and serve back.
var allowedImageExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".svg":  "image/svg+xml",
	".avif": "image/avif",
	".ico":  "image/x-icon",
}

// MimeForExt returns the content type for a supported image extension.
func MimeForExt(ext string) (string, bool) {
	m, ok := allowedImageExts[strings.ToLower(ext)]
	return m, ok
}

// ImagePath returns the absolute path of the user's pet picture, or "" when
// the bundled default should be used.
func (s *Store) ImagePath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.PetImageFile == "" {
		return ""
	}
	p := filepath.Join(s.dir, s.cfg.PetImageFile)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// SetPetImage copies src into the data directory and remembers it.
//
// Copying (rather than referencing the original path) means the pet keeps
// working if the user later moves or deletes the file they picked, and it
// keeps the served path inside a directory we control.
func (s *Store) SetPetImage(src string) (string, error) {
	ext := strings.ToLower(filepath.Ext(src))
	if _, ok := MimeForExt(ext); !ok {
		return "", fmt.Errorf("unsupported image type %q", ext)
	}
	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	// Store under a stable name; the AssetsHandler cache-busts with the
	// original path + file mtime, so overwriting in place is fine.
	name := "pet-image" + ext
	dst := filepath.Join(s.dir, name)
	tmp, err := os.CreateTemp(s.dir, ".img-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return "", fmt.Errorf("copy image: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	// Remove a previously stored picture that used a different extension so
	// the data directory does not accumulate stale files.
	s.mu.Lock()
	old := s.cfg.PetImageFile
	s.mu.Unlock()
	if old != "" && old != name {
		os.Remove(filepath.Join(s.dir, old))
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return "", err
	}
	if _, err := s.UpdateConfig(func(c *Config) {
		c.PetImageFile = name
		c.OriginalPath = src
	}); err != nil {
		return "", err
	}
	return dst, nil
}

// ClearPetImage reverts to the bundled default pet.
func (s *Store) ClearPetImage() error {
	s.mu.Lock()
	old := s.cfg.PetImageFile
	s.mu.Unlock()
	if old != "" {
		os.Remove(filepath.Join(s.dir, old))
	}
	_, err := s.UpdateConfig(func(c *Config) {
		c.PetImageFile = ""
		c.OriginalPath = ""
	})
	return err
}
