package store

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Reminder defaults and ranges (docs/reminder-pomodoro.md 5.5).
//
// As with the pomodoro settings, every minimum is non-zero so that Normalise can
// treat a zero as "this key was absent" and substitute the default.
const (
	DefaultReminderIntervalMin  = 30
	MinReminderIntervalMin      = 5
	MaxReminderIntervalMin      = 240
	DefaultReminderJitterMaxMin = 3
	MinReminderJitterMaxMin     = 0
	MaxReminderJitterMaxMin     = 60
	DefaultReminderFrom         = "09:00"
	DefaultReminderTo           = "22:00"
	DefaultReminderMaxItems     = 3
	MinReminderMaxItems         = 1
	MaxReminderMaxItems         = 5
	DefaultReminderAutoHideSec  = 12
	MinReminderAutoHideSec      = 5
	MaxReminderAutoHideSec      = 60
)

// ParseClock parses an "HH:MM" wall-clock string into minutes past midnight.
func ParseClock(s string) (int, bool) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// FormatClock renders minutes past midnight as "HH:MM".
func FormatClock(minutes int) string {
	minutes = ((minutes % 1440) + 1440) % 1440
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// normaliseClock returns s in canonical "HH:MM" form, or fallback when s is not
// a readable time.
func normaliseClock(s, fallback string) string {
	if m, ok := ParseClock(s); ok {
		return FormatClock(m)
	}
	return fallback
}

// WithinActiveWindow reports whether now falls inside the reminder's active
// hours.
//
// A window whose end is not after its start is treated as wrapping past
// midnight (22:00 -> 06:00), and an equal pair is treated as "all day" rather
// than as a zero-length window, because that is the only reading that is not
// obviously a mistake.
func WithinActiveWindow(from, to string, now time.Time) bool {
	startMin, okFrom := ParseClock(from)
	endMin, okTo := ParseClock(to)
	if !okFrom || !okTo {
		return true // unreadable window: do not suppress reminders
	}
	if startMin == endMin {
		return true
	}
	cur := now.Hour()*60 + now.Minute()
	if startMin < endMin {
		return cur >= startMin && cur < endMin
	}
	return cur >= startMin || cur < endMin
}

// OpenTodos returns every unfinished entry, in the same priority order the
// reminder bubble uses.
//
// Unlike ReminderItems this does NOT filter out undated entries: the "which todo
// is this pomodoro for?" list must offer everything that is still open, or a todo
// without a deadline could never be focused on.
func (s *Store) OpenTodos() []Todo {
	todos := s.Todos()
	out := make([]Todo, 0, len(todos))
	for _, t := range todos {
		if !t.Done {
			out = append(out, t)
		}
	}
	return out
}

// ReminderItems picks the todos a reminder bubble should show, in priority
// order, capped at max (docs/reminder-pomodoro.md 5.4):
//
//  1. overdue, the longest overdue first
//  2. due later today, soonest first
//  3. due within the next 24 hours, soonest first
//
// Completed entries are never included. Todos with no due date are not reminder
// candidates: the bubble is about what is due, and an undated entry has no
// deadline to be reminded of.
func (s *Store) ReminderItems(max int, now time.Time) []Todo {
	if max <= 0 {
		return nil
	}
	todos := s.Todos() // already ordered: open first, soonest due first
	endOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)

	var overdue, today, soon []Todo
	for _, t := range todos {
		if t.Done || t.Due == nil {
			continue
		}
		switch {
		case t.Due.Before(now):
			overdue = append(overdue, t)
		case t.Due.Before(endOfToday):
			today = append(today, t)
		case t.Due.Sub(now) <= 24*time.Hour:
			soon = append(soon, t)
		}
	}

	out := make([]Todo, 0, max)
	for _, group := range [][]Todo{overdue, today, soon} {
		for _, t := range group {
			if len(out) == max {
				return out
			}
			out = append(out, t)
		}
	}
	return out
}
