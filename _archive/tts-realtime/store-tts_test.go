package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigTTS(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.TTSEnabled {
		t.Error("speech must be off by default: the default build should never open a socket or make a sound")
	}
	if cfg.TTSBaseURL != DefaultTTSBaseURL {
		t.Errorf("default endpoint = %q, want %q", cfg.TTSBaseURL, DefaultTTSBaseURL)
	}
	if cfg.TTSLang != DefaultTTSLang {
		t.Errorf("default language = %q, want %q", cfg.TTSLang, DefaultTTSLang)
	}
	// There is no shippable default voice: the character models are third-party
	// downloads with their own licences, so shipping a path would be wrong.
	if cfg.TTSCharacter != "" || cfg.TTSRefAudioPath != "" || cfg.TTSPromptText != "" ||
		cfg.TTSGPTWeights != "" || cfg.TTSSovitsWeights != "" {
		t.Errorf("character fields must start empty, got %+v", cfg)
	}
	if !cfg.TTSAutoReadChat || !cfg.TTSAutoReadProactive {
		t.Error("read-aloud should default on for a user who has switched speech on")
	}
	if cfg.TTSConfigured() {
		t.Error("TTSConfigured must be false on a fresh config, or speech would be attempted with no reference audio")
	}
	if cfg.TTSReady() {
		t.Error("TTSReady must be false on a fresh config")
	}
}

// TestConfigUpgradeFromPreTTSFile is the compatibility guarantee: a config.json
// written before speech existed must gain a usable default endpoint rather than a
// zero value that would try to POST to "/tts" on a relative URL.
func TestConfigUpgradeFromPreTTSFile(t *testing.T) {
	dir := t.TempDir()
	old := `{"petScale":1.25,"llmEnabled":true,"llmBaseUrl":"https://api.deepseek.com/v1","llmModel":"deepseek-chat"}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(old), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cfg := s.Config()

	if cfg.TTSBaseURL != DefaultTTSBaseURL {
		t.Errorf("TTSBaseURL = %q after upgrade, want %q", cfg.TTSBaseURL, DefaultTTSBaseURL)
	}
	if cfg.TTSLang != DefaultTTSLang {
		t.Errorf("TTSLang = %q after upgrade, want %q", cfg.TTSLang, DefaultTTSLang)
	}
	if cfg.TTSEnabled {
		t.Error("speech was switched on by an upgrade; it must stay off")
	}
	if cfg.TTSMaxSentences != DefaultTTSMaxSentences {
		t.Errorf("TTSMaxSentences = %d, want %d", cfg.TTSMaxSentences, DefaultTTSMaxSentences)
	}
	if cfg.TTSChunkRunes != DefaultTTSChunkRunes {
		t.Errorf("TTSChunkRunes = %d, want %d", cfg.TTSChunkRunes, DefaultTTSChunkRunes)
	}
	if cfg.TTSTimeoutSec != DefaultTTSTimeoutSec {
		t.Errorf("TTSTimeoutSec = %d, want %d", cfg.TTSTimeoutSec, DefaultTTSTimeoutSec)
	}
	if cfg.TTSCacheMaxMB != DefaultTTSCacheMaxMB {
		t.Errorf("TTSCacheMaxMB = %d, want %d", cfg.TTSCacheMaxMB, DefaultTTSCacheMaxMB)
	}
	// The upgrade must not have disturbed the LLM settings it did know about.
	if !cfg.LLMEnabled || cfg.LLMBaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("the upgrade disturbed existing settings: %+v", cfg)
	}
}

// TestTTSNormaliseClampsOutOfRange covers values a hand-edited file could contain.
func TestTTSNormaliseClampsOutOfRange(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TTSMaxSentences = 0
	cfg.TTSChunkRunes = 0
	cfg.TTSTimeoutSec = 0
	cfg.TTSCacheMaxMB = -1
	cfg.Normalise()

	if cfg.TTSMaxSentences != DefaultTTSMaxSentences {
		t.Errorf("TTSMaxSentences = %d, want the default", cfg.TTSMaxSentences)
	}
	if cfg.TTSChunkRunes != DefaultTTSChunkRunes {
		t.Errorf("TTSChunkRunes = %d, want the default", cfg.TTSChunkRunes)
	}
	if cfg.TTSTimeoutSec != DefaultTTSTimeoutSec {
		t.Errorf("TTSTimeoutSec = %d, want the default", cfg.TTSTimeoutSec)
	}
	if cfg.TTSCacheMaxMB != DefaultTTSCacheMaxMB {
		t.Errorf("TTSCacheMaxMB = %d, want the default", cfg.TTSCacheMaxMB)
	}

	// Absurdly large values are pulled back into range too.
	cfg2 := DefaultConfig()
	cfg2.TTSMaxSentences = 1 << 20
	cfg2.TTSChunkRunes = 1 << 20
	cfg2.TTSTimeoutSec = 1 << 20
	cfg2.TTSCacheMaxMB = 1 << 20
	cfg2.Normalise()
	if cfg2.TTSMaxSentences != DefaultTTSMaxSentences {
		t.Errorf("TTSMaxSentences = %d, want the default", cfg2.TTSMaxSentences)
	}
	if cfg2.TTSChunkRunes != DefaultTTSChunkRunes {
		t.Errorf("TTSChunkRunes = %d, want the default", cfg2.TTSChunkRunes)
	}
	if cfg2.TTSTimeoutSec != DefaultTTSTimeoutSec {
		t.Errorf("TTSTimeoutSec = %d, want the default", cfg2.TTSTimeoutSec)
	}
	if cfg2.TTSCacheMaxMB != DefaultTTSCacheMaxMB {
		t.Errorf("TTSCacheMaxMB = %d, want the default", cfg2.TTSCacheMaxMB)
	}
}

// TestTTSCacheZeroIsHonoured is the one setting whose zero is meaningful: it means
// "do not cache", which a user with almost no disk left may genuinely want. It must
// not be silently replaced by the default.
func TestTTSCacheZeroIsHonoured(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TTSCacheMaxMB = 0
	cfg.Normalise()
	if cfg.TTSCacheMaxMB != 0 {
		t.Errorf("TTSCacheMaxMB = %d, want 0 to be preserved (zero means 'no cache')", cfg.TTSCacheMaxMB)
	}
}

// TestTTSNormaliseTrimsPaths checks that the paths are tidied without being
// rewritten to absolute. They are resolved by the SYNTHESIS SERVER, a different
// process that may run in a different working directory, so guessing on its behalf
// would be wrong.
func TestTTSNormaliseTrimsPaths(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TTSRefAudioPath = "  D:\\voices\\ref.wav  "
	cfg.TTSPromptText = "  参考文本  "
	cfg.TTSGPTWeights = "  D:\\voices\\a.ckpt\n"
	cfg.TTSSovitsWeights = "\tD:\\voices\\a.pth "
	cfg.TTSCharacter = "  珊瑚宫心海  "
	cfg.Normalise()

	if cfg.TTSRefAudioPath != `D:\voices\ref.wav` {
		t.Errorf("TTSRefAudioPath = %q", cfg.TTSRefAudioPath)
	}
	if cfg.TTSPromptText != "参考文本" {
		t.Errorf("TTSPromptText = %q", cfg.TTSPromptText)
	}
	if cfg.TTSGPTWeights != `D:\voices\a.ckpt` {
		t.Errorf("TTSGPTWeights = %q", cfg.TTSGPTWeights)
	}
	if cfg.TTSSovitsWeights != `D:\voices\a.pth` {
		t.Errorf("TTSSovitsWeights = %q", cfg.TTSSovitsWeights)
	}
	if cfg.TTSCharacter != "珊瑚宫心海" {
		t.Errorf("TTSCharacter = %q", cfg.TTSCharacter)
	}
	// The value is NOT made absolute: it stays exactly as typed.
	if strings.HasPrefix(cfg.TTSRefAudioPath, string(filepath.Separator)+string(filepath.Separator)) {
		t.Error("the path was rewritten; the server resolves it, not us")
	}
}

// TestTTSEndpointTrimsTrailingSlash guards the "double slash" bug: a base URL with
// a trailing slash would produce "http://127.0.0.1:9880//tts", which some servers
// 404 on.
func TestTTSEndpointTrimsTrailingSlash(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:9880":    "http://127.0.0.1:9880/tts",
		"http://127.0.0.1:9880/":   "http://127.0.0.1:9880/tts",
		"http://127.0.0.1:9880//":  "http://127.0.0.1:9880/tts",
		"http://127.0.0.1:9880///": "http://127.0.0.1:9880/tts",
	}
	for base, want := range cases {
		cfg := DefaultConfig()
		cfg.TTSBaseURL = base
		cfg.Normalise()
		if got := cfg.TTSEndpoint(); got != want {
			t.Errorf("TTSEndpoint(%q) = %q, want %q", base, got, want)
		}
		if got := cfg.TTSSetGPTWeightsEndpoint(); got != strings.TrimRight(base, "/")+"/set_gpt_weights" {
			t.Errorf("TTSSetGPTWeightsEndpoint(%q) = %q", base, got)
		}
		if got := cfg.TTSSetSovitsWeightsEndpoint(); got != strings.TrimRight(base, "/")+"/set_sovits_weights" {
			t.Errorf("TTSSetSovitsWeightsEndpoint(%q) = %q", base, got)
		}
	}
}

// TestTTSBaseURLEmptyFallsBack covers a UI that submits a cleared field: an empty
// endpoint can never work, so it is replaced rather than left broken.
func TestTTSBaseURLEmptyFallsBack(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TTSBaseURL = "   "
	cfg.Normalise()
	if cfg.TTSBaseURL != DefaultTTSBaseURL {
		t.Errorf("TTSBaseURL = %q, want the default", cfg.TTSBaseURL)
	}
}

// TestTTSLangValidation pins the language fallback. The server rejects an
// unsupported code with a 400, so sending one through would surface a raw error
// where the pet could simply have spoken in the language it knows.
func TestTTSLangValidation(t *testing.T) {
	cases := map[string]string{
		"zh":      TTSLangChinese,
		"ZH":      TTSLangChinese,
		" ja ":    TTSLangJapanese,
		"en":      TTSLangEnglish,
		"klingon": TTSLangChinese, // unknown falls back
		"":        TTSLangChinese, // absent key
		"zh-CN":   TTSLangChinese,
	}
	for in, want := range cases {
		cfg := DefaultConfig()
		cfg.TTSLang = in
		cfg.Normalise()
		if cfg.TTSLang != want {
			t.Errorf("TTSLang(%q) = %q, want %q", in, cfg.TTSLang, want)
		}
	}
}

// TestTTSConfiguredNeedsOnlyReferenceAudio documents exactly what "configured"
// means: the reference clip and nothing else.
//
// This test previously asserted the opposite - that a transcript was also required
// - and that assertion was WRONG, in a way that cost real debugging time. An empty
// prompt_text is a supported mode (clone the timbre without aligning a reference
// transcript). Demanding one pushed users into inventing a transcript, and an
// invented transcript makes GPT-SoVITS align the reference to the wrong text; on
// this machine that produced laughter instead of speech.
func TestTTSConfiguredNeedsOnlyReferenceAudio(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.TTSConfigured() {
		t.Error("a fresh config must not count as configured")
	}

	cfg.TTSRefAudioPath = "ref.wav"
	if !cfg.TTSConfigured() {
		t.Error("a reference clip alone IS enough: the transcript is optional")
	}

	// And an empty transcript must not un-configure it.
	cfg.TTSPromptText = ""
	if !cfg.TTSConfigured() {
		t.Error("an empty transcript must remain a valid configuration")
	}

	// The weights stay optional: the server may already have a voice loaded from
	// its own config file.
	if cfg.TTSGPTWeights != "" {
		t.Error("weights should be optional")
	}
}

// TestTTSReadyRequiresEnabled is the gate the app checks before every utterance:
// configured AND switched on.
func TestTTSReadyRequiresEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TTSRefAudioPath = "ref.wav"
	cfg.TTSPromptText = "参考文本"
	if cfg.TTSReady() {
		t.Error("speech must not be ready while it is switched off")
	}
	cfg.TTSEnabled = true
	if !cfg.TTSReady() {
		t.Error("speech should be ready once it is switched on and configured")
	}
}

// TestTTSConfigSurvivesRoundTrip is the persistence guarantee: what the settings
// panel writes must come back identical, or the user's character choice would
// silently reset on the next launch.
func TestTTSConfigSurvivesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := s.UpdateConfig(func(c *Config) {
		c.TTSEnabled = true
		c.TTSBaseURL = "http://127.0.0.1:9880"
		c.TTSCharacter = "珊瑚宫心海"
		c.TTSRefAudioPath = `D:\voices\kokomi\ref.wav`
		c.TTSPromptText = "今天的天气非常好。"
		c.TTSLang = TTSLangChinese
		c.TTSGPTWeights = `D:\voices\kokomi\kokomi-e10.ckpt`
		c.TTSSovitsWeights = `D:\voices\kokomi\kokomi_e10_s1683.pth`
		c.TTSMaxSentences = 4
		c.TTSChunkRunes = 24
		c.TTSTimeoutSec = 20
		c.TTSCacheMaxMB = 300
		c.TTSAutoReadChat = false
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	// Reopen from disk.
	s2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := s2.Config()

	if !got.TTSEnabled || got.TTSCharacter != "珊瑚宫心海" {
		t.Errorf("speech settings did not survive the round trip: %+v", got)
	}
	if got.TTSRefAudioPath != `D:\voices\kokomi\ref.wav` {
		t.Errorf("TTSRefAudioPath = %q", got.TTSRefAudioPath)
	}
	if got.TTSGPTWeights != `D:\voices\kokomi\kokomi-e10.ckpt` {
		t.Errorf("TTSGPTWeights = %q", got.TTSGPTWeights)
	}
	if got.TTSMaxSentences != 4 || got.TTSChunkRunes != 24 || got.TTSTimeoutSec != 20 || got.TTSCacheMaxMB != 300 {
		t.Errorf("numeric settings did not survive: %+v", got)
	}
	if got.TTSAutoReadChat {
		t.Error("TTSAutoReadChat was reset to true")
	}
	if !got.TTSReady() {
		t.Error("speech should be ready after a reopen")
	}
}

// TestTTSConfigJSONKeys pins the on-disk names, since a user may edit config.json
// by hand and the settings panel writes these keys.
func TestTTSConfigJSONKeys(t *testing.T) {
	data, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, k := range []string{
		"ttsEnabled", "ttsBaseUrl", "ttsCharacter", "ttsRefAudioPath",
		"ttsPromptText", "ttsLang", "ttsGptWeights", "ttsSovitsWeights",
		"ttsAutoReadChat", "ttsAutoReadProactive", "ttsMaxSentences",
		"ttsChunkRunes", "ttsTimeoutSec", "ttsCacheMaxMB",
	} {
		if _, ok := m[k]; !ok {
			t.Errorf("config.json is missing the key %q", k)
		}
	}
}

// TestTTSConfigHasNoSecretField is a structural guarantee, mirroring the LLM
// key test: GetState hands the whole Config to the webview, so nothing
// credential-shaped may ever be added to it. Speech has no credential by design
// (it is a local server), and this test exists to keep it that way.
//
// It inspects the KEYS rather than the serialised string, because a substring
// search has a false positive built in: "token" appears inside llmMaxTokens,
// which is a length cap and not a credential. A key-level check is both stricter
// about what it looks for and immune to that noise.
func TestTTSConfigHasNoSecretField(t *testing.T) {
	data, _ := json.Marshal(DefaultConfig())
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// Documented exceptions: these count length, they do not hold a secret.
	allowed := map[string]bool{
		"llmmaxtokens":    true,
		"llmhistoryturns": true,
	}
	forbidden := []string{"apikey", "api_key", "secret", "password", "credential", "authorization", "bearer"}
	for k := range m {
		lower := strings.ToLower(k)
		if allowed[lower] {
			continue
		}
		for _, f := range forbidden {
			if strings.Contains(lower, f) {
				t.Errorf("config.json has the key %q, which looks like a credential; "+
					"no secret may live in Config because GetState serialises it to the page", k)
			}
		}
	}
}
