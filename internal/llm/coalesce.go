package llm

import (
	"strings"
	"sync"
	"time"
)

// Coalescer batches text deltas before they cross the Wails bridge.
//
// Why this exists: a language model streams token by token, and a 500-character
// reply arrives as several hundred separate frames. Forwarding each one to the
// frontend means several hundred cross-process calls, each with its own JSON
// serialisation on both sides, which is enough to make the UI stutter and the
// event queue back up while the reply is still being generated.
//
// The batcher flushes on whichever comes first - a short interval or a decent
// amount of text - so the typewriter effect is unchanged (an eye cannot resolve
// 20 updates a second as separate steps) while the number of bridge crossings
// drops by roughly an order of magnitude.
//
// It is safe for concurrent use and, importantly, Flush is idempotent between
// appends: the caller flushes from its own goroutine while appending from the
// network goroutine.
type Coalescer struct {
	mu      sync.Mutex
	buf     strings.Builder
	last    time.Time
	flushMs int
	flushAt int
	// emit receives the batched text. It is called with the mutex held, so it
	// must not call back into the Coalescer.
	emit func(string)
}

// Default batching thresholds. 50ms is under three frames at 60Hz, and 24
// characters is about one short clause: both are small enough that the text
// still appears to flow, and large enough that a fast model produces a handful
// of events per reply instead of hundreds.
const (
	DefaultFlushInterval = 50 * time.Millisecond
	DefaultFlushChars    = 24
)

// NewCoalescer returns a Coalescer that hands batched text to emit.
//
// emit is called with the internal lock held, so it must be quick and must not
// re-enter the Coalescer. In this app it is a runtime.EventsEmit call.
func NewCoalescer(emit func(string)) *Coalescer {
	return &Coalescer{
		emit:    emit,
		last:    time.Now(),
		flushMs: int(DefaultFlushInterval / time.Millisecond),
		flushAt: DefaultFlushChars,
	}
}

// Add appends text and flushes if either threshold is now met. It reports
// whether a flush happened, which is what lets the caller reset its own
// bookkeeping.
func (c *Coalescer) Add(text string) bool {
	if text == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.WriteString(text)
	return c.flushLocked(time.Now(), false)
}

// Flush emits whatever is buffered, if anything.
//
// It is UNCONDITIONAL, unlike Add: it is called on every terminal path (done,
// error, cancel) to guarantee the transcript matches what the user watched
// appear. Letting the thresholds apply here instead silently dropped the tail of
// every reply shorter than the size threshold - the text was in the buffer, the
// result carried it, and the events never did.
func (c *Coalescer) Flush() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.flushLocked(time.Now(), true)
}

// Pending reports whether anything is buffered. Used by tests and by the
// terminal handler to avoid emitting an empty final event.
func (c *Coalescer) Pending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Len() > 0
}

func (c *Coalescer) flushLocked(now time.Time, force bool) bool {
	if c.buf.Len() == 0 {
		return false
	}
	// The size threshold or the time threshold. Time is measured from the last
	// flush rather than from the last byte, so a stuttering stream cannot hold a
	// batch indefinitely: whatever arrived in the window goes out together.
	if !force {
		elapsed := now.Sub(c.last)
		if c.buf.Len() < c.flushAt && elapsed < time.Duration(c.flushMs)*time.Millisecond {
			return false
		}
	}
	text := c.buf.String()
	c.buf.Reset()
	c.last = now
	c.emit(text)
	return true
}

// SetThresholds overrides the batching policy. It exists for tests, which need
// deterministic flushing rather than a wall-clock race.
func (c *Coalescer) SetThresholds(chars int, interval time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushAt = chars
	c.flushMs = int(interval / time.Millisecond)
}
