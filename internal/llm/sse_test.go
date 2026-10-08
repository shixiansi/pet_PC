package llm

import (
	"strings"
	"testing"
)

// --- decoder ---------------------------------------------------------------

// feedAll pushes the whole stream in one chunk and returns every event.
func feedAll(t *testing.T, stream string) []Event {
	t.Helper()
	d := NewDecoder()
	events := d.Feed([]byte(stream))
	return append(events, d.Finish()...)
}

// feedSplit pushes the stream in chunks of exactly n bytes, which is how an
// arbitrary network segmentation is simulated.
func feedSplit(t *testing.T, stream string, n int) []Event {
	t.Helper()
	d := NewDecoder()
	var events []Event
	for i := 0; i < len(stream); i += n {
		end := i + n
		if end > len(stream) {
			end = len(stream)
		}
		events = append(events, d.Feed([]byte(stream[i:end]))...)
	}
	return append(events, d.Finish()...)
}

const sampleStream = "data: {\"a\":1}\n\ndata: {\"b\":2}\n\ndata: [DONE]\n\n"

func TestDecoderBasic(t *testing.T) {
	events := feedAll(t, sampleStream)
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(events), events)
	}
	if events[0].Data != `{"a":1}` || events[1].Data != `{"b":2}` {
		t.Errorf("payloads wrong: %+v", events)
	}
	if !events[2].Done {
		t.Error("the [DONE] sentinel was not reported as Done")
	}
	if events[2].Data != "" {
		t.Errorf("[DONE] should carry no data, got %q", events[2].Data)
	}
}

// TestDecoderSurvivesAnyChunking is the test that justifies the whole design:
// no matter how the bytes are cut up, the events must come out identical.
func TestDecoderSurvivesAnyChunking(t *testing.T) {
	want := feedAll(t, sampleStream)
	if len(want) != 3 {
		t.Fatalf("baseline parse failed: %+v", want)
	}
	for n := 1; n <= len(sampleStream)+2; n++ {
		got := feedSplit(t, sampleStream, n)
		if len(got) != len(want) {
			t.Fatalf("chunk size %d: got %d events, want %d: %+v", n, len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("chunk size %d: event %d = %+v, want %+v", n, i, got[i], want[i])
			}
		}
	}
}

func TestDecoderHandlesCRLFAndLoneCR(t *testing.T) {
	for name, stream := range map[string]string{
		"lf":   "data: x\n\n",
		"crlf": "data: x\r\n\r\n",
		"cr":   "data: x\r\r",
	} {
		t.Run(name, func(t *testing.T) {
			events := feedAll(t, stream)
			if len(events) != 1 || events[0].Data != "x" {
				t.Fatalf("got %+v, want one event with data \"x\"", events)
			}
		})
	}
	// And the same with every possible split, since CRLF straddling a chunk
	// boundary is where a naive implementation dispatches twice.
	stream := "data: one\r\n\r\ndata: two\r\n\r\n"
	for n := 1; n <= len(stream); n++ {
		events := feedSplit(t, stream, n)
		if len(events) != 2 {
			t.Fatalf("crlf chunk size %d: got %d events (%+v), want 2", n, len(events), events)
		}
		if events[0].Data != "one" || events[1].Data != "two" {
			t.Fatalf("crlf chunk size %d: payloads wrong: %+v", n, events)
		}
	}
}

func TestDecoderIgnoresCommentsAndOtherFields(t *testing.T) {
	stream := ": keep-alive\nevent: message\nid: 42\nretry: 1000\ndata: hello\n\n"
	events := feedAll(t, stream)
	if len(events) != 1 || events[0].Data != "hello" {
		t.Fatalf("got %+v, want just the data field", events)
	}
}

func TestDecoderJoinsMultipleDataLines(t *testing.T) {
	// Per the SSE spec, several data fields in one event join with newlines.
	events := feedAll(t, "data: line1\ndata: line2\n\n")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Data != "line1\nline2" {
		t.Errorf("data = %q, want %q", events[0].Data, "line1\nline2")
	}
}

func TestDecoderStripsExactlyOneLeadingSpace(t *testing.T) {
	events := feedAll(t, "data:  two spaces\n\n")
	if len(events) != 1 || events[0].Data != " two spaces" {
		t.Fatalf("got %+v, want a single leading space preserved", events)
	}
	// No space at all is legal: "data:{...}".
	events = feedAll(t, "data:{\"x\":1}\n\n")
	if len(events) != 1 || events[0].Data != `{"x":1}` {
		t.Fatalf("got %+v, want the payload without a space", events)
	}
}

func TestDecoderFinishFlushesUnterminatedEvent(t *testing.T) {
	// A server that closes the connection right after the payload, with no
	// trailing blank line. Losing this event would truncate the reply.
	d := NewDecoder()
	if got := d.Feed([]byte("data: {\"x\":1}")); len(got) != 0 {
		t.Fatalf("an unterminated line should not dispatch yet, got %+v", got)
	}
	events := d.Finish()
	if len(events) != 1 || events[0].Data != `{"x":1}` {
		t.Fatalf("Finish() = %+v, want the pending event", events)
	}
	// Finish twice must not duplicate anything.
	if got := d.Finish(); len(got) != 0 {
		t.Fatalf("second Finish() = %+v, want nothing", got)
	}
}

func TestDecoderDoneWithoutBlankLine(t *testing.T) {
	// Some gateways end with "data: [DONE]" and no trailing newline at all.
	events := feedAll(t, "data: [DONE]")
	if len(events) != 1 || !events[0].Done {
		t.Fatalf("got %+v, want a single Done event", events)
	}
}

func TestDecoderEmptyStream(t *testing.T) {
	if got := feedAll(t, ""); len(got) != 0 {
		t.Fatalf("empty stream produced %+v", got)
	}
	d := NewDecoder()
	if got := d.Feed(nil); len(got) != 0 {
		t.Fatalf("nil chunk produced %+v", got)
	}
	if got := d.Finish(); len(got) != 0 {
		t.Fatalf("Finish on an empty stream produced %+v", got)
	}
}

func TestDecoderDoesNotDispatchOnBlankLinesAlone(t *testing.T) {
	// Keep-alive blank lines must not emit empty events, which downstream code
	// would try to JSON-parse.
	events := feedAll(t, "\n\n\ndata: x\n\n\n\n")
	if len(events) != 1 || events[0].Data != "x" {
		t.Fatalf("got %+v, want exactly one event", events)
	}
}

func TestDecoderLargePayloadAcrossManyChunks(t *testing.T) {
	// A realistic reply: JSON with escaped quotes and newlines, cut into 7-byte
	// pieces.
	payload := `{"choices":[{"delta":{"content":"他说：\"你好\"\n第二行，还有一段比较长的中文内容用来把这一行撑得足够长。"}}]}`
	stream := "data: " + payload + "\n\ndata: [DONE]\n\n"
	events := feedSplit(t, stream, 7)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}
	if events[0].Data != payload {
		t.Errorf("payload = %q, want %q", events[0].Data, payload)
	}
	if !events[1].Done {
		t.Error("the sentinel was lost")
	}
	if !strings.HasPrefix(events[0].Data, "{") {
		t.Error("payload was mangled")
	}
}
