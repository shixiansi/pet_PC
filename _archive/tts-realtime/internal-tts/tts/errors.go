package tts

import (
	"context"
	"errors"
	"net"
	"strings"
)

// ErrorKind classifies a synthesis failure.
//
// The classification exists for control flow and for the settings panel, not for
// the user's ear: speech is a decoration on top of the feature the user actually
// asked for, so almost every failure here ends the same way - say nothing and
// carry on. The kind is what lets the caller tell apart the one case that is
// worth surfacing (the server is not configured or not running, which the user
// can fix) from the ones that are not (this sentence timed out; the user
// interrupted).
type ErrorKind string

const (
	// KindCanceled: the app itself stopped the request (a new reply arrived, the
	// user pressed stop, or the app is quitting). This is the normal path, not
	// a failure, and must never be shown.
	KindCanceled ErrorKind = "canceled"
	// KindUnreachable: nothing is listening, DNS failed, or the connection was
	// refused. The actionable case: the user has probably not started the
	// synthesis server.
	KindUnreachable ErrorKind = "unreachable"
	// KindTimeout: the server accepted the request but did not finish in time.
	KindTimeout ErrorKind = "timeout"
	// KindRequest: the server rejected the request (missing reference audio, an
	// unsupported language, an unreadable weight path).
	KindRequest ErrorKind = "request"
	// KindServer: a 5xx from the server.
	KindServer ErrorKind = "server"
	// KindOther: anything else.
	KindOther ErrorKind = "other"
)

// SynthError is a synthesis failure already translated for the user.
//
// Mirrors llm.ChatError: it carries the kind for control flow and a Chinese
// message for display, and the conversion happens at the boundary so no
// component can accidentally render a raw server payload.
type SynthError struct {
	Kind    ErrorKind
	Message string
	// Status is the HTTP status, 0 when there was no response.
	Status int
	// Err is the underlying cause, kept for the debug log only.
	Err error
}

func (e *SynthError) Error() string { return e.Message }

// Unwrap exposes the cause to errors.Is/As without making it printable.
func (e *SynthError) Unwrap() error { return e.Err }

// IsCanceled reports whether err is the app's own interruption rather than a
// real failure.
//
// ctx is consulted because the error alone is not always enough to tell the two
// apart: a cancelled request and a timed-out one can surface as the same
// *url.Error wrapping the same sentinel, and only the context knows which one the
// caller asked for. Passing it in is what lets the pipeline answer the question it
// actually cares about - "did we stop this, or did it break?" - because a stop
// must be silent and a break is worth a log line.
//
// A timeout is deliberately NOT a cancellation: nobody asked for it, so it is
// worth reporting even though it is still not worth interrupting the user.
func IsCanceled(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	// An explicit cancellation on the context is authoritative: the pipeline is
	// shutting down, so nothing that failed during the wind-down is interesting.
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return true
	}
	var se *SynthError
	if errors.As(err, &se) {
		return se.Kind == KindCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return errors.Is(err, context.Canceled)
}

// asNetError is errors.As for net.Error, kept as a helper so the call site reads
// as one expression.
func asNetError(err error, target *net.Error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		*target = ne
		return true
	}
	return false
}

// collapseSpace turns runs of whitespace (including the newlines an HTML error
// page is full of) into single spaces.
func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// stripTags removes anything that looks like an HTML tag.
//
// The synthesis server is a local FastAPI app, so it normally answers with JSON -
// but a misconfigured port can easily be pointing at some other local service
// (another dev server, a router's admin page), and that answers with HTML. Without
// this, a 404 page would arrive in the pet's bubble as a wall of markup.
func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// truncateRunes cuts s to at most n runes, appending an ellipsis when it had to.
//
// Runes, not bytes: slicing a Chinese message at a byte boundary produces
// invalid UTF-8, which the webview renders as replacement characters.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}
