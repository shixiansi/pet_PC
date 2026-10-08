package store

import "strings"

// Speech defaults and ranges (docs/genshin-tts.md 7).
//
// The "zero means absent" convention used everywhere else in this package
// applies here too, with the usual exception: the three character paths have a
// meaningful empty value ("nothing configured yet"), so they are trimmed but
// never replaced by a default. There is no default character to fall back to -
// shipping one would mean shipping a third-party voice model, which is exactly
// what the design avoids (docs/genshin-tts.md 14).
const (
	// DefaultTTSBaseURL is GPT-SoVITS' api_v2.py default bind address. It is
	// loopback, not a placeholder: the whole point of this feature is that the
	// audio is generated on this machine.
	DefaultTTSBaseURL = "http://127.0.0.1:9880"

	// TTS language codes, as api_v2.py spells them.
	//
	// They are validated against the server's own list at request time, so an
	// unsupported value is rejected before a request is spent on it.
	TTSLangChinese  = "zh"
	TTSLangJapanese = "ja"
	TTSLangEnglish  = "en"

	// Speech backends.
	//
	// Both speak the SAME wire protocol (GPT-SoVITS' /tts, /set_gpt_weights), so
	// nothing in the client changes between them - only where the requests go and
	// where the voice LIST comes from. See docs/genshin-tts.md 17.
	//
	//   TTSBackendLocal  a local api_v2.py; voices are folders on disk.
	//   TTSBackendOnline a bridge process that proxies a public service; voices
	//                    come from the bridge's /voices endpoint.
	TTSBackendLocal  = "local"
	TTSBackendOnline = "online"

	// DefaultTTSOnlineURL is where tools/tts-online/bridge.py listens by default.
	//
	// 9881 rather than 9880 so the online bridge and a local api_v2.py can run at
	// the same time, which is what makes switching between them a one-click
	// operation instead of a restart.
	DefaultTTSOnlineURL = "http://127.0.0.1:9881"

	// DefaultTTSLang is Chinese: the pet's persona and the reference models this
	// feature targets are Chinese, and it is the one language the reply text is
	// guaranteed to be in.
	DefaultTTSLang = TTSLangChinese

	// DefaultTTSMaxSentences is how much of a reply is spoken.
	//
	// Eight rather than three, raised together with whole-reply speech. The two
	// interact: waiting for the whole reply before speaking anything means the user
	// has already paid the synthesis cost, so stopping after three sentences throws
	// away work that was already done AND leaves the visible text growing after the
	// voice has stopped. Eight covers an ordinary reply end to end.
	//
	// It is still a cap rather than unlimited: a 500-character answer read aloud in
	// full takes minutes, which is worse than not speaking it, and this is the
	// setting that prevents that.
	DefaultTTSMaxSentences = 8
	MinTTSMaxSentences     = 1
	MaxTTSMaxSentences     = 50

	// DefaultTTSChunkRunes is the target sentence length in runes.
	//
	// It is now a FALLBACK rather than a split point. A reply is cut at 。！？ only,
	// so this value no longer decides where most chunks end - it only bounds a
	// sentence that is too long for the backend to accept in one request. The
	// panel still exposes it, and it still matters for unpunctuated text, but
	// lowering it no longer fragments normal speech (which is what a low value used
	// to do, and what made replies sound chopped up).
	DefaultTTSChunkRunes = 20
	MinTTSChunkRunes     = 6
	MaxTTSChunkRunes     = 200

	// TTSOnlineMaxChunkRunes is the online service's hard input limit.
	//
	// At 110 characters it answers "输入文字过长！110>100" and produces NO audio, so
	// an over-long request is not a degraded sentence but a silent one. 90 leaves
	// room for the splitter to keep a trailing punctuation mark, so a chunk can
	// never land on the boundary and be refused by one character.
	//
	// This was briefly set to 15, from a report that longer chunks were being cut
	// off. That was a MISDIAGNOSIS, and the evidence is worth keeping:
	//
	//   - three separate measurements could not confirm any truncation at 15, 25 or
	//     55 characters; the shortest grouping came out with LESS audio per
	//     character, which cannot be truncation
	//   - the actual cause was that the splitter cut at COMMAS, so every chunk was
	//     an incomplete sentence and every clip sounded interrupted (see
	//     tts.softTerminators)
	//
	// A low cap made that worse rather than better: it produced MORE fragments. The
	// lesson is that the cap governs length, while the split POINTS govern whether
	// each request is a sentence - and only the second one was broken.
	TTSOnlineMaxChunkRunes = 90

	// DefaultTTSTimeoutSec bounds one sentence. It is deliberately far below the
	// 90s chat timeout: a sentence that has not finished synthesising by now is
	// competing with the sentences queued behind it, and dropping it keeps the
	// queue moving instead of stalling the whole reply behind one bad chunk.
	DefaultTTSTimeoutSec = 30
	MinTTSTimeoutSec     = 5
	MaxTTSTimeoutSec     = 300

	// DefaultTTSCacheMaxMB caps the audio cache. 500MB is a few thousand
	// sentences, which covers every fixed line the app says plus a long tail of
	// replies, while staying small next to the model files themselves.
	DefaultTTSCacheMaxMB = 500
	MinTTSCacheMaxMB     = 0
	MaxTTSCacheMaxMB     = 10240
)

// NormaliseTTS clamps the speech settings that came from disk or from the UI.
//
// The string handling mirrors NormaliseLLM: a blank base URL falls back to the
// default because an empty endpoint can never work, but a blank reference audio
// path is left alone because it is a legitimate "not configured yet" state that
// the settings panel explains.
func (c *Config) NormaliseTTS() {
	c.TTSBaseURL = strings.TrimSpace(c.TTSBaseURL)
	if c.TTSBaseURL == "" {
		c.TTSBaseURL = DefaultTTSBaseURL
	}
	// Same reasoning as the chat endpoint: a trailing slash would turn
	// "http://127.0.0.1:9880" + "/tts" into a double slash, and some servers
	// 404 on that.
	c.TTSBaseURL = strings.TrimRight(c.TTSBaseURL, "/")

	c.TTSCharacter = strings.TrimSpace(c.TTSCharacter)
	// The paths are stored as the user typed them. They are not made absolute
	// here: the synthesis server resolves them on its own filesystem, which is
	// a different process that may well run in a different working directory,
	// so silently rewriting them would be guessing on its behalf.
	c.TTSRefAudioPath = strings.TrimSpace(c.TTSRefAudioPath)
	c.TTSPromptText = strings.TrimSpace(c.TTSPromptText)
	c.TTSGPTWeights = strings.TrimSpace(c.TTSGPTWeights)
	c.TTSSovitsWeights = strings.TrimSpace(c.TTSSovitsWeights)
	// Trimmed but never defaulted: an empty models directory is a valid state (the
	// user configured the three paths by hand), and inventing a location would make
	// the panel list voices from a folder the user never chose.
	c.TTSModelsDir = strings.TrimRight(strings.TrimSpace(c.TTSModelsDir), `\/`)

	// An unknown backend falls back to local rather than being sent through: the
	// online bridge is an optional extra, and a typo in config.json must not leave
	// the feature pointed at nothing.
	switch strings.ToLower(strings.TrimSpace(c.TTSBackend)) {
	case TTSBackendOnline:
		c.TTSBackend = TTSBackendOnline
	default:
		c.TTSBackend = TTSBackendLocal
	}
	c.TTSOnlineURL = strings.TrimRight(strings.TrimSpace(c.TTSOnlineURL), "/")
	// A stored online URL equal to the local one is the fingerprint of an older
	// build's bug: the panel was handed the ACTIVE url for both address fields, so
	// selecting the local backend and touching any field wrote 9880 into the online
	// slot. The symptom was that switching to online asked the LOCAL port for a
	// voice list and reported "连不上在线服务" while a perfectly healthy bridge sat
	// on 9881. Rather than leave the user to work out that the two addresses must
	// differ, this treats the collision as "unset" and restores the default.
	if c.TTSOnlineURL == "" || c.TTSOnlineURL == strings.TrimRight(c.TTSBaseURL, "/") {
		c.TTSOnlineURL = DefaultTTSOnlineURL
	}

	// An unrecognised language falls back to the default rather than being sent
	// through: the server would answer 400 and the user would see a raw error
	// instead of the pet simply speaking in the language it knows.
	switch strings.ToLower(strings.TrimSpace(c.TTSLang)) {
	case TTSLangJapanese:
		c.TTSLang = TTSLangJapanese
	case TTSLangEnglish:
		c.TTSLang = TTSLangEnglish
	default:
		c.TTSLang = TTSLangChinese
	}

	c.TTSMaxSentences = clampOr(c.TTSMaxSentences, MinTTSMaxSentences, MaxTTSMaxSentences, DefaultTTSMaxSentences)
	c.TTSChunkRunes = clampOr(c.TTSChunkRunes, MinTTSChunkRunes, MaxTTSChunkRunes, DefaultTTSChunkRunes)
	c.TTSTimeoutSec = clampOr(c.TTSTimeoutSec, MinTTSTimeoutSec, MaxTTSTimeoutSec, DefaultTTSTimeoutSec)
	// 0 is inside the cache range and means "do not cache". It is documented as
	// such rather than special-cased, because a user with very little disk left
	// has a real reason to want it.
	if c.TTSCacheMaxMB < MinTTSCacheMaxMB || c.TTSCacheMaxMB > MaxTTSCacheMaxMB {
		c.TTSCacheMaxMB = DefaultTTSCacheMaxMB
	}
}

// TTSActiveURL is the address of whichever backend is selected.
//
// Every endpoint below goes through this, so "which backend" is decided in ONE
// place. That is what lets the online mode be a configuration change rather than
// a second code path through the client, the cache and the playback pipeline.
func (c Config) TTSActiveURL() string {
	if c.TTSBackend == TTSBackendOnline {
		u := strings.TrimRight(strings.TrimSpace(c.TTSOnlineURL), "/")
		if u != "" {
			return u
		}
		return DefaultTTSOnlineURL
	}
	return strings.TrimRight(c.TTSBaseURL, "/")
}

// TTSIsOnline reports whether the online backend is selected.
func (c Config) TTSIsOnline() bool { return c.TTSBackend == TTSBackendOnline }

// TTSEndpoint joins the active base URL with the synthesis path.
//
// Like ChatEndpoint, it lives here so the one place that knows the path also
// owns the trailing-slash question.
func (c Config) TTSEndpoint() string {
	return c.TTSActiveURL() + "/tts"
}

// TTSVoicesEndpoint lists the voices the active backend offers.
//
// Only the online bridge implements it; a local api_v2.py has no such route, so
// the caller must treat a failure here as "fall back to scanning the models
// directory" rather than as an error.
func (c Config) TTSVoicesEndpoint() string {
	return c.TTSActiveURL() + "/voices"
}

// TTSSetGPTWeightsEndpoint and TTSSetSovitsWeightsEndpoint are the two
// character-loading routes on the same server.
//
// They are separate endpoints rather than one because that is the shape
// api_v2.py exposes; keeping them here means the switch-character call site
// never has to spell a path. The online bridge accepts both, treating the GPT
// one as the voice selector and the SoVITS one as a no-op, so the client's
// existing two-step switch works against either backend unchanged.
func (c Config) TTSSetGPTWeightsEndpoint() string {
	return c.TTSActiveURL() + "/set_gpt_weights"
}

// TTSSetSovitsWeightsEndpoint is the SoVITS half of the character switch.
func (c Config) TTSSetSovitsWeightsEndpoint() string {
	return c.TTSActiveURL() + "/set_sovits_weights"
}

// TTSConfigured reports whether enough is filled in for a synthesis attempt to
// be worth making.
//
// What counts as "enough" depends on the backend:
//
//   - Local (api_v2.py): the reference audio. The server rejects a request
//     without it ("ref_audio_path is required").
//   - Online (the bridge): a selected voice NAME, which this feature stores in
//     TTSCharacter. There is no reference clip and no weight file - the public
//     service owns all of that - so demanding a ref path here would refuse a
//     working setup.
//
// The transcript is NOT required by either. This function used to demand one,
// which was wrong and expensively so: GPT-SoVITS aligns the reference using that
// text, so a transcript that does not match the clip makes the model latch onto
// the wrong acoustic pattern. On this machine a made-up transcript produced audio
// that was mostly laughter instead of speech. An empty one is a supported mode.
//
// The weights stay optional for the local backend because the server may already
// have a voice loaded from its own config file.
func (c Config) TTSConfigured() bool {
	if c.TTSIsOnline() {
		return strings.TrimSpace(c.TTSCharacter) != ""
	}
	return strings.TrimSpace(c.TTSRefAudioPath) != ""
}

// TTSChunkCap is the absolute ceiling on one synthesis request for the active
// backend, or 0 when the backend has no limit of its own.
//
// It is a property of the SERVER rather than a preference: the online service
// refuses anything past its limit with no audio, so the splitter has to respect it
// even when the user has asked for longer sentences. The local api_v2.py has no
// comparable limit, so it returns 0 and the splitter falls back to its own derived
// bound.
func (c Config) TTSChunkCap() int {
	if c.TTSIsOnline() {
		return TTSOnlineMaxChunkRunes
	}
	return 0
}

// TTSReady reports whether speech should be attempted at all: switched on, and
// pointed at something usable.
func (c Config) TTSReady() bool {
	return c.TTSEnabled && c.TTSConfigured()
}
