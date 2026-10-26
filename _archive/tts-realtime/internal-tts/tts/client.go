package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Settings is everything the client needs to reach one synthesis server.
//
// Like llm.Settings, it is a plain struct rather than store.Config so this
// package stays independent of the app's persistence and can be tested with a
// literal.
type Settings struct {
	// BaseURL is the server root, without a trailing slash.
	BaseURL string
	// RefAudio is the reference clip the voice is cloned from. Required.
	RefAudio string
	// PromptText is the transcript of RefAudio. Required by the server, and a
	// wrong value degrades quality rather than erroring.
	PromptText string
	// Lang is sent as both text_lang and prompt_lang.
	Lang string
	// GPTWeights and SovitsWeights are optional character checkpoints. Empty
	// means "keep whatever the server already loaded".
	GPTWeights    string
	SovitsWeights string
	// Timeout bounds ONE sentence, including the connection.
	Timeout time.Duration
}

// synthRequest is the JSON body for POST /tts.
//
// The field tags are the server's own names, taken from api_v2.py's
// TTS_Request. Only the fields this app sets are listed; everything else keeps
// the server's default, which is deliberate - the defaults are the maintainers'
// tuned values, and hard-coding copies of them here would silently drift.
type synthRequest struct {
	Text       string `json:"text"`
	TextLang   string `json:"text_lang"`
	RefAudio   string `json:"ref_audio_path"`
	PromptText string `json:"prompt_text"`
	PromptLang string `json:"prompt_lang"`
	// MediaType wav is what the player expects. It also makes the response a
	// single complete buffer rather than a stream, so no chunk assembly is
	// needed.
	MediaType string `json:"media_type"`
	// StreamingMode false: the pipeline is already overlapping synthesis with
	// playback (docs §5), so asking the server to stream as well would add a
	// second, redundant layer of chunking that the player cannot use - it plays
	// whole files.
	StreamingMode bool `json:"streaming_mode"`
}

// Client synthesises speech against a local GPT-SoVITS api_v2.py server.
//
// It is safe for concurrent use. The only mutable state is the record of which
// character is currently loaded, which exists so the weights are pushed once per
// session rather than on every sentence - re-loading them costs seconds, and a
// per-sentence reload would make the pipeline slower than the speech it is
// trying to keep up with.
type Client struct {
	// HTTP is the underlying client. Exported so tests can substitute a
	// transport, mirroring llm.Client.
	HTTP *http.Client

	mu sync.Mutex
	// loadedKey identifies the character currently pushed to the server. It is
	// the pair of weight paths, because either one changing means the voice has
	// changed.
	loadedKey string
}

// NewClient returns a client with timeouts suitable for a loopback server.
//
// The dial timeout is short - this endpoint is on 127.0.0.1, so a connection
// that has not completed in two seconds is a server that is not running - while
// the response header timeout is generous, because a CPU-bound synthesis of a
// long sentence legitimately takes seconds before the first byte comes back.
func NewClient() *Client {
	return &Client{
		HTTP: &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   2 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				// No proxy: a loopback endpoint must not be routed through one,
				// and inheriting the environment's proxy would break the very
				// case this feature exists for.
				Proxy:               nil,
				MaxIdleConns:        4,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     60 * time.Second,
			},
		},
	}
}

// maxAudioBytes caps one response. A pathological setting (a very long sentence
// times a high sample rate) should not be able to exhaust memory, and a real
// sentence is a few hundred kilobytes at most.
const maxAudioBytes = 32 << 20

// maxErrorBody caps how much of a failed response is read.
const maxErrorBody = 8 << 10

// Synthesize renders one chunk of text to WAV bytes.
//
// It returns the audio and the character key that was in effect, so the caller
// can cache the result against the voice that produced it.
func (c *Client) Synthesize(ctx context.Context, s Settings, text string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("tts: 文本为空")
	}
	if strings.TrimSpace(s.RefAudio) == "" {
		return nil, fmt.Errorf("tts: 还没有配置参考音频")
	}
	if c.HTTP == nil {
		c.HTTP = NewClient().HTTP
	}

	// Make sure the configured character is the one loaded before speaking.
	// This is a no-op after the first sentence of a session.
	if err := c.ensureCharacter(ctx, s); err != nil {
		return nil, err
	}

	body, err := json.Marshal(synthRequest{
		Text:          text,
		TextLang:      langOrDefault(s.Lang),
		RefAudio:      s.RefAudio,
		PromptText:    s.PromptText,
		PromptLang:    langOrDefault(s.Lang),
		MediaType:     "wav",
		StreamingMode: false,
	})
	if err != nil {
		return nil, fmt.Errorf("tts: 编码请求失败：%w", err)
	}

	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := strings.TrimRight(s.BaseURL, "/") + "/tts"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tts: 构造请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, classifyTransport(ctx, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The server reports its own validation failures as 400 with a JSON
		// body, and a raw exception as 400 with {"message":"tts failed",...}.
		// Reading it back is what turns "合成失败" into a fixable message.
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, classifyResponse(resp.StatusCode, raw)
	}

	audio, err := io.ReadAll(io.LimitReader(resp.Body, maxAudioBytes))
	if err != nil {
		if ctx.Err() != nil {
			return nil, classifyTransport(ctx, err)
		}
		return nil, fmt.Errorf("tts: 读取音频失败：%w", err)
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("tts: 服务端返回了空音频")
	}
	return audio, nil
}

// ensureCharacter pushes the configured weights to the server if they differ
// from what it last confirmed.
//
// A failure here is returned rather than ignored: if the requested voice cannot
// be loaded, synthesising anyway would produce audio in whatever voice happened
// to be loaded - the user would hear the wrong character and have no idea why.
//
// The whole check-and-load runs under the lock, and the HTTP calls happen inside
// it. That is deliberate. Releasing the lock around the network calls would let N
// concurrent sentences all observe "not loaded yet" and each push the same
// weights, which on a CPU-bound server means N multi-second model loads racing
// each other - the exact cost this cache exists to avoid. Serialising here is
// cheap because it happens once per session; every later call takes the mutex
// only long enough to compare two strings.
func (c *Client) ensureCharacter(ctx context.Context, s Settings) error {
	key := strings.TrimSpace(s.GPTWeights) + "\x00" + strings.TrimSpace(s.SovitsWeights)
	if key == "\x00" {
		// Nothing configured: the server's own startup voice is used.
		return nil
	}
	if c.HTTP == nil {
		c.HTTP = NewClient().HTTP
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loadedKey == key {
		return nil
	}

	if p := strings.TrimSpace(s.GPTWeights); p != "" {
		if err := c.callSetWeights(ctx, s.BaseURL, "/set_gpt_weights", p); err != nil {
			return fmt.Errorf("tts: 加载 GPT 权重失败：%w", err)
		}
	}
	if p := strings.TrimSpace(s.SovitsWeights); p != "" {
		if err := c.callSetWeights(ctx, s.BaseURL, "/set_sovits_weights", p); err != nil {
			return fmt.Errorf("tts: 加载 SoVITS 权重失败：%w", err)
		}
	}

	// Only recorded after both halves succeeded: a partially-loaded character is
	// not the requested voice, so it must not be remembered as one.
	c.loadedKey = key
	return nil
}

// ForgetCharacter drops the record of the loaded voice.
//
// It exists for the settings panel's "test" button: a user who has just fixed a
// wrong weight path expects the next attempt to push it again, and the cache
// would otherwise make the correction appear to do nothing.
func (c *Client) ForgetCharacter() {
	c.mu.Lock()
	c.loadedKey = ""
	c.mu.Unlock()
}

// callSetWeights hits one of the two weight-setting endpoints.
//
// They are GET routes taking a query parameter in api_v2.py, which is worth
// noting because the natural guess (POST with a body) gets a 405 and looks like
// a broken server.
func (c *Client) callSetWeights(ctx context.Context, baseURL, path, weights string) error {
	endpoint := strings.TrimRight(baseURL, "/") + path + "?weights_path=" + url.QueryEscape(weights)
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return classifyTransport(ctx, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return classifyResponse(resp.StatusCode, raw)
	}
	return nil
}

// Ping checks whether a server is answering at all, without synthesising.
//
// It is used by the settings panel's connection test. The request is expected to
// fail validation (no reference audio), which still proves the server is up: a
// 400 with a JSON body is a success for this purpose, while a connection refusal
// is not.
func (c *Client) Ping(ctx context.Context, baseURL string) error {
	if c.HTTP == nil {
		c.HTTP = NewClient().HTTP
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/tts"
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return classifyTransport(ctx, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	// Any HTTP answer at all means something is listening and speaking HTTP.
	return nil
}

// classifyTransport turns a Go network error into a message that says what to do
// about it.
//
// The distinction that matters most is WHY the request ended. This feature stops
// speech constantly (a new reply, a new sentence, the user leaving), and those
// must never surface as errors. A deadline is different - nobody asked for it -
// so it gets its own kind even though it is equally non-fatal.
//
// The order of the checks is load-bearing. A per-sentence deadline that fires
// propagates as a context error, so asking "is any context error set?" first
// would classify every timeout as a cancellation and swallow the one failure that
// says the server may be too slow to be usable.
func classifyTransport(ctx context.Context, err error) error {
	// An explicit cancellation of the PARENT context is a user/app action.
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return &SynthError{Kind: KindCanceled, Message: "已取消。", Err: err}
	}
	// A deadline - the per-sentence timeout, or a parent carrying one - is a real
	// (if non-fatal) failure and is reported as such.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &SynthError{Kind: KindTimeout, Message: "合成超时，这一句就不读了。", Err: err}
	}
	var netErr net.Error
	if asNetError(err, &netErr) && netErr.Timeout() {
		return &SynthError{Kind: KindTimeout, Message: "合成超时，这一句就不读了。", Err: err}
	}
	return &SynthError{
		Kind: KindUnreachable,
		Message: "连不上语音服务，请确认 GPT-SoVITS 已经在运行" +
			"（默认 127.0.0.1:9880）。",
		Err: err,
	}
}

// classifyResponse turns a non-2xx response into a SynthError.
//
// The server's own messages are surfaced rather than replaced, because they name
// the offending field ("ref_audio_path is required", "text_lang: xx is not
// supported") and that is far more useful than a generic failure line.
func classifyResponse(status int, body []byte) error {
	detail := serverMessage(body)
	switch {
	case status == 400 || status == 422:
		msg := "语音服务拒绝了这一句"
		if detail != "" {
			msg += "：" + detail
		}
		return &SynthError{Kind: KindRequest, Message: msg, Status: status}
	case status == 404:
		return &SynthError{
			Kind:    KindRequest,
			Status:  status,
			Message: "语音服务没有 /tts 接口：地址可能填成了别的服务。",
		}
	case status >= 500:
		return &SynthError{
			Kind:    KindServer,
			Status:  status,
			Message: fmt.Sprintf("语音服务内部错误（HTTP %d）。", status),
		}
	default:
		msg := fmt.Sprintf("语音服务返回 HTTP %d", status)
		if detail != "" {
			msg += "：" + detail
		}
		return &SynthError{Kind: KindOther, Status: status, Message: msg}
	}
}

// serverMessage extracts "message" (and, when present, the "Exception" the
// server attaches to a wrapped failure) from an error body.
func serverMessage(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	var payload struct {
		Message   string `json:"message"`
		Exception string `json:"Exception"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		// Not JSON: an HTML error page or a bare sentence. Tags are stripped and
		// the result truncated, so neither a wall of markup nor a whole page can
		// end up in a 300px bubble.
		return truncateRunes(collapseSpace(stripTags(string(body))), 160)
	}
	out := strings.TrimSpace(payload.Message)
	if ex := strings.TrimSpace(payload.Exception); ex != "" {
		if out != "" {
			out += "（" + ex + "）"
		} else {
			out = ex
		}
	}
	return truncateRunes(out, 200)
}

// langOrDefault substitutes the default language for an empty one, so a config
// that predates the field still produces a servable request.
func langOrDefault(lang string) string {
	if strings.TrimSpace(lang) == "" {
		return "zh"
	}
	return lang
}
