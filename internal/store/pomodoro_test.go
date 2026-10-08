package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// TestDefaultConfigPomodoro asserts the documented defaults, because the whole
// feature is opt-in and a wrong default here would switch it on unasked.
func TestDefaultConfigPomodoro(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PomodoroOnStartup || cfg.ReminderOnStartup || cfg.ReminderEnabled {
		t.Error("pomodoro and reminders must be off by default")
	}
	if !cfg.AutoStartNextPhase {
		t.Error("AutoStartNextPhase should default to true")
	}
	if !cfg.RemindDuringBreak || !cfg.PomodoroSound {
		t.Error("RemindDuringBreak and PomodoroSound should default to true")
	}
	if cfg.FocusMinutes != 25 || cfg.ShortBreakMinutes != 5 || cfg.LongBreakMinutes != 15 || cfg.LongBreakEvery != 4 {
		t.Errorf("unexpected pomodoro timings: %+v", cfg)
	}
	if cfg.ReminderIntervalMin != 30 || cfg.ReminderJitterMaxMin != 3 || cfg.ReminderMaxItems != 3 || cfg.ReminderAutoHideSec != 12 {
		t.Errorf("unexpected reminder settings: %+v", cfg)
	}
	if cfg.ReminderFrom != "09:00" || cfg.ReminderTo != "22:00" {
		t.Errorf("unexpected active window: %s-%s", cfg.ReminderFrom, cfg.ReminderTo)
	}
	if cfg.RemindWhenEmpty {
		t.Error("RemindWhenEmpty should default to false")
	}
}

// TestConfigUpgradeFromOldFile is the compatibility test that matters: a
// config.json written before this feature existed has none of the new keys, and
// must come back as the defaults rather than as zeroes (a 0-minute focus phase
// would make the timer finish instantly).
func TestConfigUpgradeFromOldFile(t *testing.T) {
	dir := t.TempDir()
	old := `{"petScale":1.5,"snapToEdge":true,"edgeMargin":8,"autostart":false}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cfg := s.Config()
	if cfg.FocusMinutes != DefaultFocusMinutes {
		t.Errorf("focusMinutes = %d, want %d (zero must not survive as a duration)", cfg.FocusMinutes, DefaultFocusMinutes)
	}
	if cfg.ShortBreakMinutes != DefaultShortBreakMinutes || cfg.LongBreakMinutes != DefaultLongBreakMinutes {
		t.Errorf("breaks not defaulted: %+v", cfg)
	}
	if cfg.LongBreakEvery != DefaultLongBreakEvery {
		t.Errorf("longBreakEvery = %d, want %d", cfg.LongBreakEvery, DefaultLongBreakEvery)
	}
	if cfg.ReminderIntervalMin != DefaultReminderIntervalMin {
		t.Errorf("reminderIntervalMin = %d, want %d", cfg.ReminderIntervalMin, DefaultReminderIntervalMin)
	}
	if cfg.ReminderAutoHideSec != DefaultReminderAutoHideSec || cfg.ReminderMaxItems != DefaultReminderMaxItems {
		t.Errorf("reminder display settings not defaulted: %+v", cfg)
	}
	// Existing values must be preserved.
	if cfg.PetScale != 1.5 {
		t.Errorf("petScale = %v, want 1.5", cfg.PetScale)
	}
}

// TestConfigClampAndClock checks that out-of-range and malformed values are
// repaired rather than trusted.
func TestConfigClampAndClock(t *testing.T) {
	cases := []struct {
		name  string
		patch func(*Config)
		check func(*Config) bool
	}{
		{"focus too big", func(c *Config) { c.FocusMinutes = 999 }, func(c *Config) bool { return c.FocusMinutes == DefaultFocusMinutes }},
		{"focus negative", func(c *Config) { c.FocusMinutes = -5 }, func(c *Config) bool { return c.FocusMinutes == DefaultFocusMinutes }},
		{"long break every too small", func(c *Config) { c.LongBreakEvery = 1 }, func(c *Config) bool { return c.LongBreakEvery == DefaultLongBreakEvery }},
		{"interval too small", func(c *Config) { c.ReminderIntervalMin = 1 }, func(c *Config) bool { return c.ReminderIntervalMin == DefaultReminderIntervalMin }},
		{"auto hide too small", func(c *Config) { c.ReminderAutoHideSec = 1 }, func(c *Config) bool { return c.ReminderAutoHideSec == DefaultReminderAutoHideSec }},
		{"bad clock", func(c *Config) { c.ReminderFrom = "25:99" }, func(c *Config) bool { return c.ReminderFrom == DefaultReminderFrom }},
		{"clock not padded", func(c *Config) { c.ReminderTo = "7:5" }, func(c *Config) bool { return c.ReminderTo == "07:05" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			tc.patch(&c)
			c.Normalise()
			if !tc.check(&c) {
				t.Errorf("after Normalise: %+v", c)
			}
		})
	}
}

func TestWithinActiveWindow(t *testing.T) {
	at := func(h, m int) time.Time {
		return time.Date(2026, 3, 4, h, m, 0, 0, time.Local)
	}
	cases := []struct {
		name     string
		from, to string
		now      time.Time
		want     bool
	}{
		{"inside", "09:00", "22:00", at(12, 0), true},
		{"at start", "09:00", "22:00", at(9, 0), true},
		{"one minute before start", "09:00", "22:00", at(8, 59), false},
		{"at end", "09:00", "22:00", at(22, 0), false},
		{"one minute before end", "09:00", "22:00", at(21, 59), true},
		{"overnight after midnight", "22:00", "06:00", at(2, 0), true},
		{"overnight before start", "22:00", "06:00", at(12, 0), false},
		{"all day when equal", "09:00", "09:00", at(3, 0), true},
		{"unreadable window does not suppress", "nonsense", "22:00", at(3, 0), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WithinActiveWindow(tc.from, tc.to, tc.now); got != tc.want {
				t.Errorf("WithinActiveWindow(%q,%q,%s) = %v, want %v", tc.from, tc.to, tc.now.Format("15:04"), got, tc.want)
			}
		})
	}
}

// TestReminderItemPriority pins the documented ordering: overdue first (oldest
// first), then due today, then due within 24h, and never a completed or undated
// entry.
func TestReminderItemPriority(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.Local)
	due := func(d time.Duration) *time.Time { x := now.Add(d); return &x }

	todos := []Todo{
		{ID: "undated", Title: "no deadline", CreatedAt: now},
		{ID: "done", Title: "finished", Done: true, Due: due(-48 * time.Hour)},
		{ID: "soon", Title: "in 20h", Due: due(20 * time.Hour)},
		{ID: "today", Title: "tonight", Due: due(6 * time.Hour)},
		{ID: "overdue-old", Title: "very late", Due: due(-72 * time.Hour)},
		{ID: "overdue-recent", Title: "a bit late", Due: due(-1 * time.Hour)},
	}
	if err := s.SaveTodos(todos); err != nil {
		t.Fatal(err)
	}

	got := s.ReminderItems(4, now)
	ids := make([]string, len(got))
	for i, td := range got {
		ids[i] = td.ID
	}
	want := []string{"overdue-old", "overdue-recent", "today", "soon"}
	if len(ids) != len(want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("got %v, want %v", ids, want)
		}
	}
	for _, td := range got {
		if td.Done {
			t.Error("a completed todo was selected for a reminder")
		}
		if td.Due == nil {
			t.Error("an undated todo was selected for a reminder")
		}
	}

	// The cap is honoured, and the highest-priority entries survive it.
	if short := s.ReminderItems(2, now); len(short) != 2 || short[0].ID != "overdue-old" {
		t.Errorf("cap not honoured in priority order: %+v", short)
	}

	// Overdue entries stay candidates indefinitely - that is the point of the
	// priority order, not a bug.
	if later := s.ReminderItems(3, now.AddDate(1, 0, 0)); len(later) != 3 {
		t.Errorf("overdue todos should still be reminder candidates a year later, got %d", len(later))
	}

	// With nothing open (and nothing undated-but-open) there is nothing to
	// remind about, which the scheduler treats as "stay quiet" unless
	// RemindWhenEmpty is on.
	empty := newTestStore(t)
	if err := empty.SaveTodos([]Todo{
		{ID: "done", Title: "finished", Done: true, Due: due(-time.Hour)},
		{ID: "undated", Title: "no deadline", CreatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	if items := empty.ReminderItems(3, now); len(items) != 0 {
		t.Errorf("expected no candidates with nothing open and dated, got %+v", items)
	}
}

func TestPhaseDuration(t *testing.T) {
	c := DefaultConfig()
	c.FocusMinutes, c.ShortBreakMinutes, c.LongBreakMinutes = 25, 5, 15
	cases := map[string]time.Duration{
		PhaseFocus:      25 * time.Minute,
		PhaseShortBreak: 5 * time.Minute,
		PhaseLongBreak:  15 * time.Minute,
		PhaseIdle:       0,
		"bogus":         0,
	}
	for phase, want := range cases {
		if got := c.PhaseDuration(phase); got != want {
			t.Errorf("PhaseDuration(%q) = %v, want %v", phase, got, want)
		}
	}
}

func TestNormaliseStateRepairsGarbage(t *testing.T) {
	// An unknown phase resets to idle.
	if got := NormaliseState(PomodoroState{Phase: "banana", RunState: RunRunning}); got.Phase != PhaseIdle {
		t.Errorf("unknown phase survived: %+v", got)
	}
	// A running phase with no end time cannot count down.
	got := NormaliseState(PomodoroState{Phase: PhaseFocus, RunState: RunRunning})
	if got.IsRunning() {
		t.Errorf("running phase without EndsAt should be halted: %+v", got)
	}
	// A negative remainder is clamped.
	got = NormaliseState(PomodoroState{Phase: PhaseFocus, RunState: RunPaused, RemainingOnHalt: -30})
	if got.RemainingOnHalt != 0 {
		t.Errorf("negative remainder survived: %+v", got)
	}
	// Idle clears the transient phase fields.
	got = NormaliseState(PomodoroState{Phase: PhaseIdle, RunState: RunRunning, RemainingOnHalt: 120})
	if got.RunState != "" || got.RemainingOnHalt != 0 {
		t.Errorf("idle state not cleared: %+v", got)
	}
	// An unknown run state on a real phase becomes stopped, not running.
	got = NormaliseState(PomodoroState{Phase: PhaseFocus, RunState: "sideways"})
	if got.IsRunning() {
		t.Errorf("unknown run state became running: %+v", got)
	}
}

// TestFocusingExcludesPausedAndStopped is the type-level statement of the rule
// the whole reminder schedule rests on: only an actively running focus phase
// suppresses reminders.
func TestFocusingExcludesPausedAndStopped(t *testing.T) {
	end := time.Now().Add(time.Minute)
	cases := []struct {
		st   PomodoroState
		want bool
	}{
		{PomodoroState{Phase: PhaseFocus, RunState: RunRunning, EndsAt: &end}, true},
		{PomodoroState{Phase: PhaseFocus, RunState: RunPaused}, false},
		{PomodoroState{Phase: PhaseFocus, RunState: RunStopped}, false},
		{PomodoroState{Phase: PhaseShortBreak, RunState: RunRunning, EndsAt: &end}, false},
		{PomodoroState{Phase: PhaseIdle}, false},
	}
	for _, tc := range cases {
		if got := tc.st.IsFocusing(); got != tc.want {
			t.Errorf("%+v: IsFocusing = %v, want %v", tc.st, got, tc.want)
		}
	}
}

func TestPomodoroStatePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	end := time.Now().Add(10 * time.Minute).Truncate(time.Second)
	st := PomodoroState{
		Enabled:          true,
		Phase:            PhaseFocus,
		RunState:         RunRunning,
		EndsAt:           &end,
		CompletedCount:   3,
		CurrentTodoID:    "t1",
		CurrentTodoTitle: "写周报",
	}
	if err := s.SavePomodoro(st); err != nil {
		t.Fatal(err)
	}

	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Pomodoro()
	if !got.Enabled || got.Phase != PhaseFocus || !got.IsRunning() {
		t.Fatalf("state did not survive reopen: %+v", got)
	}
	if got.CompletedCount != 3 || got.CurrentTodoID != "t1" || got.CurrentTodoTitle != "写周报" {
		t.Errorf("fields lost across reopen: %+v", got)
	}
	if got.EndsAt == nil || !got.EndsAt.Equal(end) {
		t.Errorf("endsAt = %v, want %v", got.EndsAt, end)
	}
}

// TestCorruptPomodoroStateResetsSilently checks the deliberate difference in
// corruption policy: runtime state is dropped without a warning, while the
// permanent record archive is quarantined and reported.
func TestCorruptPomodoroStateResetsSilently(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pomodoro.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatalf("a corrupt state file must not stop the app: %v", err)
	}
	if got := s.Pomodoro(); got.Phase != PhaseIdle {
		t.Errorf("phase = %q, want idle", got.Phase)
	}
	if len(s.Warnings()) != 0 {
		t.Errorf("corrupt runtime state should be silent, got warnings %v", s.Warnings())
	}
}

func TestCorruptRecordsAreQuarantinedAndReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pomodoro-records.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(s.Warnings()) == 0 {
		t.Error("a corrupt record archive must be reported: it cannot be regenerated")
	}
	if _, err := os.Stat(filepath.Join(dir, "pomodoro-records.json.bak")); err != nil {
		t.Errorf("expected the bad archive to be kept as .bak: %v", err)
	}
	if recs := s.PomodoroRecords(); len(recs) != 0 {
		t.Errorf("expected an empty archive, got %d records", len(recs))
	}
}

func TestRecordSummaryEditableButNotDeletable(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-25 * time.Minute).Truncate(time.Second)
	rec, err := s.AddPomodoroRecord(PomodoroRecord{
		TodoID:       "t1",
		TodoTitle:    "写周报",
		StartedAt:    start,
		EndedAt:      time.Now().Truncate(time.Second),
		FocusMinutes: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID == "" {
		t.Fatal("record was stored without an ID")
	}

	// Editing the summary works and stamps EditedAt.
	updated, err := s.SetPomodoroSummary(rec.ID, "完成了数据部分")
	if err != nil {
		t.Fatalf("SetPomodoroSummary: %v", err)
	}
	if updated.Summary != "完成了数据部分" || updated.EditedAt == nil {
		t.Errorf("summary edit not applied: %+v", updated)
	}

	// Editing an unknown record is an error, not a silent success.
	if _, err := s.SetPomodoroSummary("nope", "x"); err == nil {
		t.Error("editing a missing record should fail")
	}

	// The archive still has exactly one record: there is no delete path.
	if recs := s.PomodoroRecords(); len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	count, minutes := s.PomodoroTotals()
	if count != 1 || minutes != 25 {
		t.Errorf("totals = (%d,%d), want (1,25)", count, minutes)
	}
}

// TestRecordTitleSnapshotSurvivesTodoDeletion is the reason the title is stored
// on the record at all.
func TestRecordTitleSnapshotSurvivesTodoDeletion(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTodos([]Todo{{ID: "t1", Title: "写周报", CreatedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPomodoroRecord(PomodoroRecord{
		TodoID: "t1", TodoTitle: "写周报",
		StartedAt: time.Now().Add(-time.Minute), EndedAt: time.Now(), FocusMinutes: 25,
	}); err != nil {
		t.Fatal(err)
	}
	// The todo is later deleted (or renamed).
	if err := s.SaveTodos(nil); err != nil {
		t.Fatal(err)
	}
	recs := s.PomodoroRecords()
	if len(recs) != 1 || recs[0].TodoTitle != "写周报" {
		t.Errorf("the record lost its title snapshot: %+v", recs)
	}
}

func TestRecordsAreNewestFirst(t *testing.T) {
	s := newTestStore(t)
	base := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := s.AddPomodoroRecord(PomodoroRecord{
			EndedAt:      base.Add(time.Duration(i) * time.Hour),
			StartedAt:    base.Add(time.Duration(i) * time.Hour).Add(-25 * time.Minute),
			FocusMinutes: 25,
		}); err != nil {
			t.Fatal(err)
		}
	}
	recs := s.PomodoroRecords()
	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}
	for i := 1; i < len(recs); i++ {
		if recs[i].EndedAt.After(recs[i-1].EndedAt) {
			t.Fatalf("records are not newest-first: %v", recs)
		}
	}
}

// TestRecordArchiveIsValidJSON keeps the on-disk shape honest, since users are
// invited to read these files.
func TestRecordArchiveIsValidJSON(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPomodoroRecord(PomodoroRecord{
		StartedAt: time.Now(), EndedAt: time.Now(), FocusMinutes: 25, Summary: "x",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "pomodoro-records.json"))
	if err != nil {
		t.Fatal(err)
	}
	var back []PomodoroRecord
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("archive is not valid JSON: %v", err)
	}
	if len(back) != 1 {
		t.Fatalf("expected 1 record on disk, got %d", len(back))
	}
}

func TestFormatAndParseClockRoundTrip(t *testing.T) {
	for _, s := range []string{"00:00", "09:05", "13:45", "23:59"} {
		m, ok := ParseClock(s)
		if !ok {
			t.Fatalf("ParseClock(%q) failed", s)
		}
		if got := FormatClock(m); got != s {
			t.Errorf("round trip %q -> %d -> %q", s, m, got)
		}
	}
	for _, bad := range []string{"", "9", "9:5:1", "24:00", "12:60", "aa:bb"} {
		if _, ok := ParseClock(bad); ok {
			t.Errorf("ParseClock(%q) should have failed", bad)
		}
	}
}
