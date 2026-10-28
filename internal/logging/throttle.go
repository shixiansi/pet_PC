package logging

// Throttle collapses a repeated line into one line plus a count.
//
// # WHY THIS EXISTS
//
// The rule the two hosts follow is "one line per plugin call". Taken literally that rule has a
// failure mode the user found before I did: a plugin that POLLS - the pomodoro rereads its
// state every second, the todo list rereads its storage every two - writes thousands of
// identical lines an hour. On a real machine the log held 9729 `share.get` lines, 2842
// `storage.get` lines and 580 `config.get` lines, against 32 `llm.chat` lines: 99.5% of the
// log was the plugins talking to themselves, and the 0.5% anybody opens a log for was buried.
//
// A level change alone does not fix that (the reads moved to DEBUG, but a plugin's periodic
// WRITE still looks like an interesting call), so the repetition itself is collapsed: the first
// occurrence in a window is written, later ones are counted, and the next line that gets
// through says how many were folded into it.
//
// # WHAT IT MUST NOT DO
//
// It must never lose the FACT that something keeps happening: a heartbeat that prints once and
// then nothing forever reads as a hang. That is why every emitted line after a suppressed run
// carries the count.

import (
	"sync"
	"time"
)

// DefaultThrottleWindow is how long one key stays quiet after a line.
//
// Five seconds is chosen against the traffic that caused this: a one-second poll produces one
// line per five polls (still visible as "it is running"), while a real action - a bubble, a
// model call, an install - is far enough apart that it is never collapsed.
const DefaultThrottleWindow = 5 * time.Second

// Throttle is a per-key rate limiter. It is safe for concurrent use and tolerates a nil
// receiver, so a program can leave the field unset.
type Throttle struct {
	mu     sync.Mutex
	window time.Duration
	// now is a seam: the tests assert the window without sleeping.
	now func() time.Time

	last    map[string]time.Time
	dropped map[string]int
}

// NewThrottle builds a throttle with the given window. now may be nil (time.Now).
func NewThrottle(window time.Duration, now func() time.Time) *Throttle {
	if window <= 0 {
		window = DefaultThrottleWindow
	}
	if now == nil {
		now = time.Now
	}
	return &Throttle{window: window, now: now, last: map[string]time.Time{}, dropped: map[string]int{}}
}

// Allow reports whether a line for key may be written now, and how many lines for that key were
// swallowed since the last one that got through.
//
// A caller that gets allowed=false writes nothing; the count is carried forward and reported by
// the next line that IS allowed. That keeps the "still alive" signal without one line per tick.
func (t *Throttle) Allow(key string) (allowed bool, swallowed int) {
	if t == nil {
		return true, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	if at, seen := t.last[key]; seen && now.Sub(at) < t.window {
		t.dropped[key] += 1
		return false, 0
	}
	swallowed = t.dropped[key]
	t.dropped[key] = 0
	t.last[key] = now
	return true, swallowed
}
