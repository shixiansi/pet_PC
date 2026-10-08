package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
)

// ErrorKind classifies a failure so the UI can say something useful and the
// caller can decide whether retrying could help.
type ErrorKind string

const (
	// KindAuth: the key is wrong, missing or expired. Retrying is pointless.
	KindAuth ErrorKind = "auth"
	// KindRateLimit: quota or rate limit. Retrying after a wait can help.
	KindRateLimit ErrorKind = "rate_limit"
	// KindModel: the model name does not exist, or the account cannot use it.
	KindModel ErrorKind = "model"
	// KindRequest: the request itself was rejected (bad temperature, too long).
	KindRequest ErrorKind = "request"
	// KindNetwork: DNS, connection refused, TLS, connection reset.
	KindNetwork ErrorKind = "network"
	// KindTimeout: the request outlived its deadline.
	KindTimeout ErrorKind = "timeout"
	// KindCanceled: the user asked to stop.
	KindCanceled ErrorKind = "canceled"
	// KindServer: a 5xx from the gateway.
	KindServer ErrorKind = "server"
	// KindProtocol: the stream was not the shape it promised (bad JSON frame).
	KindProtocol ErrorKind = "protocol"
	// KindConfig: nothing is configured yet.
	KindConfig ErrorKind = "config"
	// KindBudget: today's request budget is used up.
	KindBudget ErrorKind = "budget"
	// KindOther: anything not otherwise classified.
	KindOther ErrorKind = "other"
)

// ChatError is a failure already translated for the user.
//
// It carries the kind (for control flow) and a Chinese message (for display).
// The conversion happens at the boundary rather than in the UI so that no
// component can accidentally show a raw provider payload, which frequently
// echoes parts of the request - including, on some gateways, the authorization
// header.
type ChatError struct {
	Kind    ErrorKind
	Message string
	// Retryable says whether trying the same request again could succeed.
	Retryable bool
	// Status is the HTTP status, 0 when there was no response.
	Status int
	// Detail is the redacted extract from the response body, when there was one.
	// It is kept separate from Message so the UI can show a short line and the
	// debug log can carry the rest.
	Detail string
	// Attempts is how many requests had been made when this error was produced.
	Attempts int
	// Err is the underlying error, kept for the debug log only.
	Err error
}

func (e *ChatError) Error() string { return e.Message }

// Unwrap exposes the cause to errors.Is/As without making it printable.
func (e *ChatError) Unwrap() error { return e.Err }

// newChatError builds a ChatError.
func newChatError(kind ErrorKind, status int, msg string, err error) *ChatError {
	return &ChatError{
		Kind:      kind,
		Status:    status,
		Message:   msg,
		Retryable: kind == KindRateLimit || kind == KindServer || kind == KindNetwork,
		Err:       err,
	}
}

// retryableError is newChatError for a failure that is worth retrying even
// though its kind is not normally so.
//
// The case it exists for: an endpoint that rejects `stream_options`. That is a
// request error by kind, but the retry is exactly what fixes it, and the client
// has already changed what it will send.
func retryableError(kind ErrorKind, status int, msg string, err error) *ChatError {
	e := newChatError(kind, status, msg, err)
	e.Retryable = true
	return e
}

// classifyResponse turns a non-2xx response into a ChatError.
//
// body is the (already size-limited) response body; it is used as a hint only,
// because providers put wildly different things there - DeepSeek returns
// {"error":{"message":...}}, some gateways return an HTML error page, and a few
// return a bare sentence.
func classifyResponse(status int, body string) *ChatError {
	detail := extractDetail(body)
	switch {
	case status == 401 || status == 403:
		return newChatError(KindAuth, status, "API Key 无效或已过期，请在设置里重新填写。", nil)
	case status == 402:
		return newChatError(KindRateLimit, status, "账户额度不足，请到服务商后台充值或更换 Key。", nil)
	case status == 404:
		msg := "模型名或接口地址不存在"
		if detail != "" {
			msg += "：" + detail
		} else {
			msg += "，请检查设置里的「模型」和「接口地址」。"
		}
		return newChatError(KindModel, status, msg, nil)
	case status == 429:
		return newChatError(KindRateLimit, status, "请求过于频繁或超出配额，请稍后再试。", nil)
	case status == 400 || status == 422:
		msg := "请求被拒绝"
		if detail != "" {
			msg += "：" + detail
		} else {
			msg += "，请检查模型名与参数。"
		}
		return newChatError(KindRequest, status, msg, nil)
	case status >= 500:
		return newChatError(KindServer, status, fmt.Sprintf("服务端错误（HTTP %d），请稍后重试。", status), nil)
	default:
		msg := fmt.Sprintf("请求失败（HTTP %d）", status)
		if detail != "" {
			msg += "：" + detail
		}
		return newChatError(KindOther, status, msg, nil)
	}
}

// classifyTransport turns a Go network error into a ChatError.
//
// The order of the checks matters: context.DeadlineExceeded also satisfies
// net.Error.Timeout(), and the two deserve different messages ("timed out" versus
// "you stopped it").
func classifyTransport(ctx context.Context, baseURL string, err error) *ChatError {
	switch {
	case errors.Is(err, context.Canceled):
		return newChatError(KindCanceled, 0, "已取消。", err)
	case errors.Is(err, context.DeadlineExceeded):
		return newChatError(KindTimeout, 0, "等待响应超时，已中断。可以在设置里调大超时时间。", err)
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		host := hostOf(baseURL)
		return newChatError(KindNetwork, 0, fmt.Sprintf("无法解析服务器地址 %s，请检查网络或接口地址。", host), err)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return newChatError(KindTimeout, 0, "连接超时，请检查网络。", err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return newChatError(KindTimeout, 0, "连接超时，请检查网络。", err)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return newChatError(KindNetwork, 0, "连接被中断，回复可能不完整。", err)
	}
	return newChatError(KindNetwork, 0, "网络不可达，请检查网络连接或接口地址。", err)
}

// hostOf extracts just the host from a URL, for error messages. A malformed URL
// yields the raw string, which is the most useful thing to show in that case.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

// maxDetail is how much of an error body is shown. A gateway that returns a
// full HTML page must not paste it into a 300px bubble.
const maxDetail = 200

// extractDetail pulls a human-readable sentence out of an error body.
//
// It tries the OpenAI error shape first, then anything JSON-ish, then falls back
// to the raw text truncated. Every path runs through redact, so a body that
// echoes the request cannot leak the API key into the transcript or the log.
func extractDetail(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	// The common shape: {"error":{"message":"...","type":"..."}}
	if i := strings.Index(body, `"message"`); i >= 0 {
		if msg := firstJSONString(body[i:]); msg != "" {
			return redact(truncate(msg, maxDetail))
		}
	}
	// Some gateways return {"detail":"..."} or a bare string.
	if i := strings.Index(body, `"detail"`); i >= 0 {
		if msg := firstJSONString(body[i:]); msg != "" {
			return redact(truncate(msg, maxDetail))
		}
	}
	// HTML or plain text: strip tags and collapse whitespace so an error page
	// does not arrive as a wall of markup.
	cleaned := collapseSpace(stripTags(body))
	return redact(truncate(cleaned, maxDetail))
}

// firstJSONString returns the value of the first JSON string in s, unescaped.
func firstJSONString(s string) string {
	_, rest, found := strings.Cut(s, ":")
	if !found {
		return ""
	}
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	rest = rest[1:]
	var b strings.Builder
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '\\':
			if i+1 < len(rest) {
				i++
				switch rest[i] {
				case 'n', 't', 'r':
					b.WriteByte(' ')
				default:
					b.WriteByte(rest[i])
				}
			}
		case '"':
			return b.String()
		default:
			b.WriteByte(rest[i])
		}
	}
	return b.String()
}

// stripTags removes anything that looks like an HTML tag.
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

// collapseSpace turns runs of whitespace into single spaces.
func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// truncate cuts s to at most n runes, appending an ellipsis when it had to.
//
// The cut counts runes, not bytes: slicing a Chinese error message at a byte
// boundary produces invalid UTF-8, which the webview renders as replacement
// characters.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

// redact removes anything key-shaped from a string.
//
// This is the last line of defence. Provider error bodies sometimes echo the
// request that failed, and some gateways include the auth header in a debug
// field. Anything that looks like a credential is replaced before the text can
// reach the transcript, the settings panel or the debug log.
func redact(s string) string {
	if s == "" {
		return s
	}
	fields := strings.Fields(s)
	for i, f := range fields {
		if looksSecret(f) {
			fields[i] = "sk-***"
		}
	}
	return strings.Join(fields, " ")
}

// looksSecret reports whether a whitespace-delimited token looks like an API
// key.
//
// A run of 20+ characters from the URL-safe alphabet with no dots or slashes is
// treated as a credential. That deliberately errs towards redacting: replacing a
// harmless long word with dots costs the user nothing, while printing a key once
// into a log file that then gets pasted into a bug report cannot be undone.
func looksSecret(tok string) bool {
	t := strings.Trim(tok, "\"'`,.;:)")
	if len(t) < 20 {
		return false
	}
	if strings.ContainsAny(t, "./@:\\") {
		return false
	}
	for _, r := range t {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	// A long lowercase hex or a long word could be innocent, but the cheap
	// heuristic is worth the false positives.
	return true
}

// RedactForLog is the exported form used by the app layer before writing
// anything user- or server-supplied into the debug log.
func RedactForLog(s string) string { return redact(s) }
