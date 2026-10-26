// Package llm implements the OpenAI-compatible chat client the pet talks to:
// Server-Sent-Events parsing, delta coalescing, prompt assembly and error
// classification.
//
// Nothing in here touches the network on its own and nothing imports the app or
// the store, so every decision that can be got wrong - how a stream is split
// across reads, when to flush a partial reply, what to tell the user about a
// 429 - is a pure function with a test next to it.
package llm

import (
	"strings"
)

// sseDataDone is the sentinel OpenAI-compatible servers send as the last data
// line.
const sseDataDone = "[DONE]"

// Event is one dispatched Server-Sent Event.
type Event struct {
	// Data is the event's payload, with multiple `data:` lines joined by "\n".
	Data string
	// Done is true for the terminal "[DONE]" sentinel, which carries no JSON.
	Done bool
}

// Decoder turns an arbitrarily-chunked byte stream into SSE events.
//
// The chunking is the whole reason this type exists rather than a bufio.Scanner
// over the response body. A single `data: {...}` line routinely arrives split
// across two or three TCP reads, and a single read routinely delivers several
// complete events. Feeding READ BYTES into a stateful decoder is the only shape
// that gets both cases right, and it is exactly the part that is invisible until
// it breaks on a slow connection.
//
// The line handling follows the WHATWG event-stream rules that matter here:
// lines end with LF, CRLF or a lone CR; a line starting with ':' is a comment;
// the field name is everything before the first ':'; one leading space in the
// value is stripped; fields other than `data` are ignored; and an empty line
// dispatches the accumulated data.
type Decoder struct {
	// buf holds bytes of a line that has not been terminated yet.
	buf []byte
	// data accumulates the `data` fields of the event being built.
	data []string
	// pendingCR records that the last byte was a CR. A CRLF pair is one line
	// break, so the following LF must not be read as a second (empty) line,
	// which would dispatch a half-built event.
	pendingCR bool
}

// NewDecoder returns an empty decoder.
func NewDecoder() *Decoder { return &Decoder{} }

// Feed consumes a chunk of bytes and returns every event that is now complete.
//
// It never blocks and never keeps a reference to chunk, so the caller may reuse
// the buffer it read into.
func (d *Decoder) Feed(chunk []byte) []Event {
	var events []Event
	for _, b := range chunk {
		if d.pendingCR {
			d.pendingCR = false
			if b == '\n' {
				continue // the LF half of a CRLF: the line already ended
			}
		}
		switch b {
		case '\n':
			events = d.endLine(events)
		case '\r':
			events = d.endLine(events)
			d.pendingCR = true
		default:
			d.buf = append(d.buf, b)
		}
	}
	return events
}

// Finish flushes state left over when the stream ends.
//
// It must be called after the body is exhausted: a server that closes the
// connection immediately after its last event, with no trailing blank line,
// would otherwise lose that event.
func (d *Decoder) Finish() []Event {
	var events []Event
	if len(d.buf) > 0 {
		events = d.endLine(events)
	}
	// An event whose blank line never arrived still counts.
	if len(d.data) > 0 {
		events = append(events, d.dispatch())
	}
	return events
}

// endLine processes the completed line sitting in buf, appending any event it
// completes.
func (d *Decoder) endLine(events []Event) []Event {
	line := string(d.buf)
	d.buf = d.buf[:0]

	if line == "" {
		// Blank line: dispatch, if anything was accumulated.
		if len(d.data) > 0 {
			events = append(events, d.dispatch())
		}
		return events
	}
	if strings.HasPrefix(line, ":") {
		return events // comment, often a keep-alive
	}
	name, value, found := strings.Cut(line, ":")
	if !found {
		return events // a bare field name carries no data
	}
	value = strings.TrimPrefix(value, " ") // exactly one leading space
	if name != "data" {
		return events // event:, id: and retry: are unused by this protocol
	}
	if value == sseDataDone {
		// The sentinel stands alone; flush anything that preceded it without a
		// blank line, then report the end.
		if len(d.data) > 0 {
			events = append(events, d.dispatch())
		}
		return append(events, Event{Done: true})
	}
	d.data = append(d.data, value)
	return events
}

// dispatch builds the event from the accumulated data fields and resets state.
func (d *Decoder) dispatch() Event {
	ev := Event{Data: strings.Join(d.data, "\n")}
	d.data = d.data[:0]
	return ev
}
