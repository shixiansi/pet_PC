// Package tts is a client for a local GPT-SoVITS inference server.
//
// It exists only to RENDER the pet's fixed lines into the user's own voice, from
// the settings panel. Nothing here runs while the pet is speaking: playback plays a
// WAV file. The previous real-time synthesis path was retired because speaking a
// reply took 11 seconds on a 54-character sentence, and this package keeps that
// lesson by never being on the playback path at all - a render happens when the
// user asks for one, and its result is stored.
//
// The one behaviour worth knowing about is that a character switch is STATEFUL on
// the server: /set_gpt_weights and /set_sovits_weights change which voice every
// subsequent /tts call uses. Rendering a take therefore means "select the role,
// then synthesise", and the two cannot be reordered or interleaved with another
// role's render. Client.Serialise exists to enforce exactly that.
package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is where a local GPT-SoVITS api_v2.py listens by default.
const DefaultBaseURL = "http://127.0.0.1:9880"

// Request is one synthesis job.
//
// The fields mirror the server's own request model, but only the ones that decide
// the result. Everything omitted (batch size, split bucket, the streaming knobs)
// is left at the server's default, because a fixed line is a single short sentence
// and none of those knobs do anything useful for one.
type Request struct {
	// Text is the sentence to speak. The server caps this per request, so long text
	// is the caller's problem; every line in this app is one short sentence.
	Text string `json:"text"`
	// TextLang is the language of Text, e.g. "zh".
	TextLang string `json:"text_lang"`
	// RefAudioPath is the reference clip whose timbre is cloned.
	RefAudioPath string `json:"ref_audio_path"`
	// PromptText is the reference clip's transcript. May be empty.
	PromptText string `json:"prompt_text"`
	// PromptLang is the language of the reference clip.
	PromptLang string `json:"prompt_lang"`
	// TextSplitMethod decides how the server splits long text. "cut5" splits on
	// punctuation and is the server's own default.
	TextSplitMethod string `json:"text_split_method"`
	// BatchSize is 1: the studio renders one line at a time so progress can be
	// reported per line.
	BatchSize int `json:"batch_size"`
	// MediaType is always "wav" - the pet plays WAV files.
	MediaType string `json:"media_type"`
	// SpeedFactor scales the reading rate (>1 is faster).
	SpeedFactor float64 `json:"speed_factor"`
	// Temperature, TopK and TopP control sampling variety. Lower is more stable.
	Temperature float64 `json:"temperature"`
	TopK        int     `json:"top_k"`
	TopP        float64 `json:"top_p"`
	// RepetitionPenalty suppresses the repeated-syllable failure mode.
	RepetitionPenalty float64 `json:"repetition_penalty"`
	// Seed makes a render reproducible; -1 lets the server choose.
	Seed int `json:"seed"`
	// Streaming is off: a whole-file render is what gets stored.
	StreamingMode bool `json:"streaming_mode"`
}

// Client talks to one GPT-SoVITS server.
type Client struct {
	baseURL string
	http    *http.Client

	// render serialises synthesis.
	//
	// This is not an optimisation, it is a correctness requirement: the selected
	// character is server-side state, so two concurrent renders for different roles
	// would interleave their weight switches and could return one role's audio
	// under another role's name. The server is also single-threaded in practice on
	// CPU, so serialising costs nothing real.
	render sync.Mutex
}

// New returns a client for baseURL, defaulting to the local server.
func New(baseURL string) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		// CPU synthesis of one line takes seconds, so the timeout is generous and
		// per-request; a shorter one would turn a slow but working render into a
		// spurious failure.
		http: &http.Client{Timeout: 10 * time.Minute},
	}
}

// BaseURL returns the server this client talks to.
func (c *Client) BaseURL() string { return c.baseURL }

// Health reports whether the server is up and able to synthesise.
//
// The server has no dedicated health route, so this asks /tts, which is the
// cheapest endpoint that proves it is running.
//
// ANY HTTP status counts as healthy, and that is deliberate rather than lax. The
// engine constructs its TTS pipeline at module import - loading the models - and
// only then binds the port, so "something answered on this port" already implies
// the models are loaded. The status codes confirm it: this build answers 500 when
// /tts is called without its required query parameters (it dereferences text_lang
// before validating), and 422 on other builds. Rejecting anything but 200/422 made
// a perfectly healthy engine look absent.
//
// A render still reports the engine's real errors, so nothing is hidden by being
// generous here.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/tts", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接语音服务 %s：%w", c.baseURL, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16)) //nolint:errcheck // draining only
	return nil
}

// HealthQuick is Health with a short, fixed timeout, for a polling loop.
//
// Separate from Health because the two callers want opposite things: an interactive
// probe should be able to wait a couple of seconds for a busy engine, while a
// readiness poll wants to fail fast and try again.
func (c *Client) HealthQuick() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.Health(ctx) == nil
}

// SelectRole switches the server to a role's weights.
//
// Both calls are required and neither is optional: the GPT weights carry the
// prosody model and the SoVITS weights the tone, and a stale half of the pair
// produces audio in a blend of two characters rather than an error.
func (c *Client) SelectRole(ctx context.Context, gptWeights, sovitsWeights string) error {
	if err := c.get(ctx, "/set_gpt_weights", gptWeights); err != nil {
		return fmt.Errorf("切换 GPT 权重失败：%w", err)
	}
	if err := c.get(ctx, "/set_sovits_weights", sovitsWeights); err != nil {
		return fmt.Errorf("切换 SoVITS 权重失败：%w", err)
	}
	return nil
}

// get calls a GET endpoint that takes one path parameter.
func (c *Client) get(ctx context.Context, path, value string) error {
	u := c.baseURL + path + "?weights_path=" + url.QueryEscape(value)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s：%s", resp.Status, messageOf(body))
	}
	return nil
}

// Synthesize renders one request to WAV bytes.
//
// The caller must already have selected the role; Synthesize does not do it, so
// that a batch render selects once and then renders many lines instead of paying
// the weight-loading cost per line.
func (c *Client) Synthesize(ctx context.Context, req Request) ([]byte, error) {
	if strings.TrimSpace(req.Text) == "" {
		return nil, fmt.Errorf("文本为空")
	}
	if strings.TrimSpace(req.RefAudioPath) == "" {
		return nil, fmt.Errorf("缺少参考音频路径")
	}
	if req.TextLang == "" {
		req.TextLang = "zh"
	}
	if req.PromptLang == "" {
		req.PromptLang = req.TextLang
	}
	if req.TextSplitMethod == "" {
		req.TextSplitMethod = "cut5"
	}
	if req.BatchSize == 0 {
		req.BatchSize = 1
	}
	if req.MediaType == "" {
		req.MediaType = "wav"
	}
	if req.SpeedFactor == 0 {
		req.SpeedFactor = 1
	}
	if req.Temperature == 0 {
		req.Temperature = 1
	}
	if req.TopK == 0 {
		req.TopK = 15
	}
	if req.TopP == 0 {
		req.TopP = 1
	}
	if req.RepetitionPenalty == 0 {
		req.RepetitionPenalty = 1.35
	}
	if req.Seed == 0 {
		req.Seed = -1
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tts", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("合成请求失败：%w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取合成结果失败：%w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("合成失败（%s）：%s", resp.Status, messageOf(data))
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("合成返回了空音频")
	}
	if !isWAV(data) {
		return nil, fmt.Errorf("合成返回的不是 WAV 音频")
	}
	return data, nil
}

// Render renders one line as one role, selecting the role first.
//
// It takes the render lock for both steps together, which is the only way to keep
// the server's selected-voice state from being changed underneath a synthesis.
func (c *Client) Render(ctx context.Context, role RoleRef, req Request) ([]byte, error) {
	c.render.Lock()
	defer c.render.Unlock()
	if err := c.SelectRole(ctx, role.GPT, role.SoVITS); err != nil {
		return nil, err
	}
	return c.Synthesize(ctx, req)
}

// RoleRef is the pair of weight files that identifies a voice on the server. It is
// a local type so this package does not depend on the app's role model.
type RoleRef struct {
	GPT    string
	SoVITS string
}

// isWAV reports whether data starts with a RIFF/WAVE header.
//
// Checked because the server answers 200 with an empty or HTML body in some
// failure modes, and storing that as a take would produce a clip that plays as
// silence with nothing to explain why.
func isWAV(data []byte) bool {
	return len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE"
}

// messageOf extracts the server's own error message, which is far more useful than
// the status code alone: it is where "text_lang is not supported" and friends live.
func messageOf(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "（服务未返回内容）"
	}
	var payload struct {
		Message   string `json:"message"`
		Exception string `json:"Exception"`
		Detail    any    `json:"detail"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		parts := []string{}
		if payload.Message != "" {
			parts = append(parts, payload.Message)
		}
		if payload.Exception != "" {
			parts = append(parts, payload.Exception)
		}
		if payload.Detail != nil {
			if b, err := json.Marshal(payload.Detail); err == nil {
				parts = append(parts, string(b))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " / ")
		}
	}
	if len(text) > 300 {
		text = text[:300] + "…"
	}
	return text
}
