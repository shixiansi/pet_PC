package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Pomodoro phases.
//
// The zero value of the phase field is "" which is normalised to PhaseIdle, so a
// config file or state file written by an older build cannot leave the pet in a
// phase the state machine does not understand.
const (
	PhaseIdle       = "idle"
	PhaseFocus      = "focus"
	PhaseShortBreak = "short_break"
	PhaseLongBreak  = "long_break"
)

// Run states of a phase that is not idle.
//
// "paused" and "stopped" are deliberately distinct in the type but identical to
// every consumer: both freeze the countdown, both count as NOT focusing (so the
// reminder scheduler may interrupt, see docs/reminder-pomodoro.md 4.3 and 13.1),
// and both can be resumed. The distinction exists only so the UI can label the
// button that got the pet there.
const (
	RunRunning = "running"
	RunPaused  = "paused"
	RunStopped = "stopped"
)

// Pomodoro defaults and the ranges the settings panel is allowed to write.
//
// Every minimum is non-zero, which is what lets Normalise treat a zero as
// "absent" and fill in the default: a config.json written before this feature
// existed has no pomodoro keys at all, and unmarshalling over DefaultConfig
// leaves these values in place.
const (
	DefaultFocusMinutes      = 25
	DefaultShortBreakMinutes = 5
	DefaultLongBreakMinutes  = 15
	DefaultLongBreakEvery    = 4

	MinFocusMinutes   = 1
	MaxFocusMinutes   = 120
	MinBreakMinutes   = 1
	MaxBreakMinutes   = 60
	MinLongBreakEvery = 2
	MaxLongBreakEvery = 10
)

// IsFocusPhase reports whether a phase is a focus session (as opposed to a rest
// or the resting state).
func IsFocusPhase(phase string) bool { return phase == PhaseFocus }

// IsBreakPhase reports whether a phase is a rest period.
func IsBreakPhase(phase string) bool {
	return phase == PhaseShortBreak || phase == PhaseLongBreak
}

// IsPhase reports whether phase is one of the four known phases.
func IsPhase(phase string) bool {
	switch phase {
	case PhaseIdle, PhaseFocus, PhaseShortBreak, PhaseLongBreak:
		return true
	}
	return false
}

// PhaseDuration returns the configured length of the given phase.
//
// The idle phase has no duration. An unknown phase also returns 0, which callers
// treat as "nothing is running".
func (c Config) PhaseDuration(phase string) time.Duration {
	switch phase {
	case PhaseFocus:
		return time.Duration(c.FocusMinutes) * time.Minute
	case PhaseShortBreak:
		return time.Duration(c.ShortBreakMinutes) * time.Minute
	case PhaseLongBreak:
		return time.Duration(c.LongBreakMinutes) * time.Minute
	}
	return 0
}

// PomodoroState is the running state of the pomodoro timer.
//
// It lives in its own file (pomodoro.json) because it is runtime state rather
// than a user preference, and it is therefore handled differently on corruption:
// a broken state file is reset to idle silently, whereas a broken config or todo
// file is quarantined and reported. Losing "which phase was running" is
// forgettable; losing a todo is not.
type PomodoroState struct {
	// Enabled is whether the user has switched the pomodoro feature on. It is
	// part of the runtime state rather than the config so that turning the pet
	// off and on again does not silently restart focusing, while the separate
	// config flag PomodoroOnStartup decides what a fresh launch does.
	Enabled bool `json:"enabled"`

	// Phase is one of the Phase* constants.
	Phase string `json:"phase"`
	// RunState is one of the Run* constants. Meaningless while Phase is idle.
	RunState string `json:"runState"`

	// EndsAt is when the current phase is due to finish. Only set while running.
	EndsAt *time.Time `json:"endsAt,omitempty"`
	// PhaseStartedAt is when the current phase began, used for the record's
	// start time and for detecting an implausible clock jump.
	PhaseStartedAt *time.Time `json:"phaseStartedAt,omitempty"`

	// RemainingOnHalt holds the seconds left at the moment the phase was paused
	// or stopped. Pausing stores the remainder and clears EndsAt so that time
	// spent paused does not count against the session (a paused pomodoro is not
	// a focusing pomodoro).
	RemainingOnHalt int `json:"remainingOnHalt"`

	// CompletedCount is how many focus phases have finished, ever. It drives the
	// long-break cadence and survives stopping, restarting and relaunching.
	CompletedCount int `json:"completedCount"`

	// CurrentTodoID/Title are the todo this session is about. The title is a
	// snapshot so that the record and the badge keep working after the todo is
	// renamed or deleted. An empty ID is legitimate: with no open todos the user
	// may focus without associating one (docs/reminder-pomodoro.md 13.2).
	CurrentTodoID    string `json:"currentTodoId,omitempty"`
	CurrentTodoTitle string `json:"currentTodoTitle,omitempty"`

	// PendingRecordID names the record waiting for a summary. A focus phase
	// writes its record the moment it ends (so "skip" cannot dodge the
	// permanent record), and this field is what the summary bubble edits.
	//
	// While it is set the timer is PARKED: the next phase is in place but not
	// counting down, and it starts when the summary is answered. That is a
	// deliberate reversal of the earlier design, which started the break behind the
	// bubble - the user asked for the record to gate the rest, so that a session
	// left undescribed cannot silently bank a break nobody asked for.
	PendingRecordID string `json:"pendingRecordId,omitempty"`
}

// NormaliseState repairs a state loaded from disk into something the state
// machine can always act on.
//
// This is deliberately forgiving and never returns an error: the state file is
// runtime bookkeeping, so the correct response to anything unexpected is to fall
// back to a resting state rather than to interrupt the user.
func NormaliseState(st PomodoroState) PomodoroState {
	if !IsPhase(st.Phase) {
		st.Phase = PhaseIdle
	}
	switch st.RunState {
	case RunRunning:
		// A running phase without an end time cannot be counted down; treat it
		// as halted with whatever remainder was recorded.
		if st.EndsAt == nil {
			st.RunState = RunStopped
		}
	case RunPaused, RunStopped:
		if st.RemainingOnHalt < 0 {
			st.RemainingOnHalt = 0
		}
	default:
		st.RunState = RunStopped
	}
	if st.Phase == PhaseIdle {
		st.RunState = ""
		st.EndsAt = nil
		st.PhaseStartedAt = nil
		st.RemainingOnHalt = 0
	}
	if st.CompletedCount < 0 {
		st.CompletedCount = 0
	}
	return st
}

// IsRunning reports whether a phase is actively counting down.
func (st PomodoroState) IsRunning() bool {
	return st.Phase != PhaseIdle && st.RunState == RunRunning
}

// IsFocusing reports whether the pet is in a focus phase that is actually
// running. Pausing or stopping a focus phase makes this false, which is what
// allows the reminder scheduler to interrupt (docs/reminder-pomodoro.md 4.3).
func (st PomodoroState) IsFocusing() bool {
	return IsFocusPhase(st.Phase) && st.IsRunning()
}

// PomodoroRecord is one finished focus session.
//
// Records are permanent by design: the store deliberately offers no way to
// delete one, so "skip the summary" cannot be used to make a session disappear.
// The summary text itself is editable forever.
type PomodoroRecord struct {
	ID        string `json:"id"`
	TodoID    string `json:"todoId,omitempty"`
	TodoTitle string `json:"todoTitle,omitempty"`
	// StartedAt/EndedAt bound the session.
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	// FocusMinutes is the length the focus phase was configured for, not the
	// wall-clock elapsed time, so a paused session is still credited with the
	// session length the user planned.
	FocusMinutes int `json:"focusMinutes"`
	// Summary is empty when the user skipped the summary bubble.
	Summary string `json:"summary"`
	// EditedAt is set when the summary text is changed after the fact.
	EditedAt *time.Time `json:"editedAt,omitempty"`
}

func (s *Store) pomodoroPath() string        { return filepath.Join(s.dir, "pomodoro.json") }
func (s *Store) pomodoroRecordsPath() string { return filepath.Join(s.dir, "pomodoro-records.json") }

// loadPomodoro reads pomodoro.json. A missing or unreadable file silently resets
// the timer to idle: this is runtime state, and refusing to start (or nagging)
// over it would be worse than losing it.
func (s *Store) loadPomodoro() error {
	var st PomodoroState
	if err := readJSON(s.pomodoroPath(), &st); err != nil {
		st = PomodoroState{}
	}
	s.pomodoro = NormaliseState(st)
	return nil
}

// loadPomodoroRecords reads pomodoro-records.json. Unlike the state file, these
// records are user data and cannot be regenerated, so a corrupt file is
// quarantined and reported exactly like todos.json.
func (s *Store) loadPomodoroRecords() error {
	var recs []PomodoroRecord
	if err := readJSON(s.pomodoroRecordsPath(), &recs); err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("pomodoro-records.json 无法解析（%v），已从空记录开始", err))
		if backup := quarantine(s.pomodoroRecordsPath()); backup != "" {
			s.warnings[len(s.warnings)-1] += "，原文件已备份为 " + filepath.Base(backup)
		}
		recs = nil
	}
	if recs == nil {
		recs = []PomodoroRecord{}
	}
	s.records = recs
	return nil
}

// Pomodoro returns a copy of the running state.
func (s *Store) Pomodoro() PomodoroState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pomodoro
}

// SavePomodoro replaces the running state and persists it.
func (s *Store) SavePomodoro(st PomodoroState) error {
	st = NormaliseState(st)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pomodoro = st
	return writeJSON(s.pomodoroPath(), s.pomodoro)
}

// PomodoroRecords returns every record, newest first.
func (s *Store) PomodoroRecords() []PomodoroRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PomodoroRecord, len(s.records))
	copy(out, s.records)
	sort.SliceStable(out, func(i, j int) bool { return out[i].EndedAt.After(out[j].EndedAt) })
	return out
}

// AddPomodoroRecord appends a finished session and returns it with its ID filled
// in. There is intentionally no DeletePomodoroRecord: the type is the guarantee.
func (s *Store) AddPomodoroRecord(rec PomodoroRecord) (PomodoroRecord, error) {
	if rec.ID == "" {
		rec.ID = newRecordID()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	if err := writeJSON(s.pomodoroRecordsPath(), s.records); err != nil {
		return rec, err
	}
	return rec, nil
}

// SetPomodoroSummary rewrites the summary of an existing record. Records cannot
// be deleted but they can always be corrected.
func (s *Store) SetPomodoroSummary(id, summary string) (PomodoroRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.records {
		if s.records[i].ID != id {
			continue
		}
		now := time.Now()
		s.records[i].Summary = summary
		s.records[i].EditedAt = &now
		if err := writeJSON(s.pomodoroRecordsPath(), s.records); err != nil {
			return s.records[i], err
		}
		return s.records[i], nil
	}
	return PomodoroRecord{}, errors.New("未找到该记录")
}

// PomodoroTotals summarises the archive for the records page: how many sessions
// have been recorded and how many focus minutes they add up to.
func (s *Store) PomodoroTotals() (count, focusMinutes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.records {
		count++
		focusMinutes += r.FocusMinutes
	}
	return count, focusMinutes
}

// newRecordID makes a sortable, collision-resistant record identifier.
func newRecordID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
}
