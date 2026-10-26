package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/store"
	"PetPC/internal/tts"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// speechState is the speech runtime: the pipeline currently speaking, plus the
// long-lived client and cache.
//
// It has its own mutex, separate from App.mu (window geometry) and App.llm.mu
// (chat requests), for the reason llmState documents: the speech workers run on
// their own goroutines and must never be able to block a drag on a lock the UI
// thread also wants.
type speechState struct {
	mu sync.Mutex
	// current is the pipeline for the utterance being spoken, or nil.
	current *tts.Pipeline
	// client is shared across utterances, which is what lets it remember which
	// character is loaded (a several-second cost it must pay only once).
	client *tts.Client
	// cache stores synthesised audio across utterances.
	cache *tts.Cache
	// cacheMB is the budget the cached instance was built with, so a settings
	// change can be detected and the cache rebuilt.
	cacheMB int
	// speaker plays the clips. It is a field rather than a direct call to the
	// desktop package so tests can substitute a recorder - the alternative is a
	// suite that plays audio out loud, or one that cannot run without a sound
	// device at all.
	speaker tts.Speaker

	// pending is the reply text that has been generated but not yet revealed,
	// and reqID is the request it belongs to. See holdText/onSpeechProgress.
	//
	// Guarded by mu together with the fields above rather than by its own lock,
	// because the reveal path reads the request id and the pipeline together and
	// they must agree about which round is in flight.
	pending string
	reqID   string
	// revealed is the text already shown for this round, so a late report cannot
	// shorten what the user is reading.
	revealed string
	// full is the complete reply for this round. It is stored rather than passed
	// around because the final reveal happens on the drain goroutine, which
	// outlives the request handler that knows the text.
	full string
}

// speechProgress reports what has been revealed for a round, and whether the round
// is being revealed in step with the voice at all.
//
// The boolean is needed because "revealed is empty" is ambiguous - it is equally
// true of a speech round whose first sentence is still being synthesised and of a
// round with speech switched off - and the frontend must choose between showing
// the partial text and showing the whole reply. Guessing that would either hide a
// finished reply behind a silent service or dump a spoken one all at once.
func (a *App) speechProgress(id string) (string, bool) {
	a.speech.mu.Lock()
	defer a.speech.mu.Unlock()
	if id == "" || a.speech.reqID != id {
		// Not a round this mechanism is tracking, so the caller must show the
		// text itself.
		return "", false
	}
	return a.speech.revealed, true
}

// beginSpeechRound resets the text-reveal bookkeeping for a new reply.
//
// The request id is stored so the pipeline's progress reports can be tagged with
// the round they belong to: a report that arrives after the user has asked
// something else must not be appended to the new answer.
func (a *App) beginSpeechRound(id string) {
	a.speech.mu.Lock()
	a.speech.reqID = id
	a.speech.pending = ""
	a.speech.revealed = ""
	a.speech.full = ""
	a.speech.mu.Unlock()
}

// setSpeechFullText records the complete reply for the round.
//
// It is stored rather than passed to finishSpeech because the final reveal runs on
// the drain goroutine, which outlives the request handler that knows the text - and
// because the authoritative text is the TRIMMED one, which only exists after the
// proactive cap has been applied.
func (a *App) setSpeechFullText(id, text string) {
	a.speech.mu.Lock()
	if a.speech.reqID == id {
		a.speech.full = text
	}
	a.speech.mu.Unlock()
}

// holdText records streamed reply text WITHOUT showing it yet.
//
// This is the difference the user asked for: with speech on, the reply is not
// streamed to the screen as the model produces it. It is held here and released
// sentence by sentence by onSpeechProgress, so each line appears at the moment it
// is spoken.
//
// The reason it is worth holding back: synthesis on this machine is SLOWER than
// generation (docs §5, and measured), so streaming the text would put the whole
// answer on screen seconds before the pet had said the first clause. The two would
// look unrelated - the voice reading something the user finished reading a while
// ago.
func (a *App) holdText(id, text string) {
	if text == "" {
		return
	}
	a.speech.mu.Lock()
	if a.speech.reqID == id {
		a.speech.pending += text
	}
	a.speech.mu.Unlock()
}

// onSpeechProgress reveals the text whose audio is about to be heard.
//
// It is called from the pipeline's playback goroutine, so it must not block; it
// only takes the speech mutex (which is never held across I/O) and emits an event.
//
// It reveals only what is being SPOKEN, and deliberately does not try to show the
// remainder of the reply early. That was tried and is not merely unnecessary but
// IMPOSSIBLE here: when the sentence budget runs out the pipeline closes its input
// and starts playing at once (see Pipeline.enqueue), and that can happen while the
// model is still streaming - so at that moment the rest of the reply does not exist
// yet. A held-back tail is delivered by the final reveal in revealRemaining.
func (a *App) onSpeechProgress(revealed string) {
	a.speech.mu.Lock()
	id := a.speech.reqID
	// Never shorten what is already on screen. The reports arrive in playback
	// order, but a late or duplicated one must not make the text jump backwards -
	// the user would see a sentence they had already read disappear.
	if runeLen(revealed) < runeLen(a.speech.revealed) {
		revealed = a.speech.revealed
	}
	a.speech.revealed = revealed
	a.speech.mu.Unlock()

	if id == "" {
		return
	}
	a.emitEvent(eventChatReveal, map[string]any{
		"requestId": id,
		"text":      revealed,
		"final":     false,
	})
}

// revealRemaining releases any text that was held but never spoken.
//
// It is the safety net for the whole scheme, and it is why holding text back can
// never lose a reply. Text goes unspoken for several ordinary reasons: the sentence
// budget cut the reply short, a chunk failed to synthesise, the user pressed stop,
// or the speech service is simply down. In every one of those cases the answer
// still belongs on screen, so the full text is emitted once the speech attempt is
// over.
//
// The hold state is only cleared when this round is STILL the current one. A
// superseded request's goroutine finishes after the new round has begun, and
// clearing unconditionally would wipe the new round's bookkeeping - so the new
// reply would lose its own sentence-by-sentence reveal and dump itself all at once.
// The event is emitted either way, tagged with this round's id, and the frontend
// decides from the id whether it still applies.
func (a *App) revealRemaining(id, full string) {
	if id == "" {
		return
	}
	a.speech.mu.Lock()
	if a.speech.reqID == id {
		a.speech.reqID = ""
		a.speech.pending = ""
		a.speech.revealed = ""
	}
	a.speech.mu.Unlock()

	a.emitEvent(eventChatReveal, map[string]any{
		"requestId": id,
		"text":      full,
		"final":     true,
	})
}

// runeLen counts runes, so the "never shorten" comparison is not fooled by
// multi-byte characters: Chinese text has one third the byte length would suggest.
func runeLen(s string) int { return len([]rune(s)) }

// SpeakLine speaks a short fixed sentence, replacing anything already speaking.
//
// It is for the app's OWN notices - the pomodoro phase ends, a todo reminder -
// not for chat replies, and the difference matters:
//
//   - The text is known in full up front, so there is no streaming and no
//     reveal: the bubble or the panel already shows the line, and holding it back
//     would only delay it. onSpeechProgress is therefore not involved, and the
//     speech round bookkeeping is deliberately NOT touched - a notice must not
//     capture the reveal state that a reply in flight owns.
//   - It returns immediately and speaks in the background. The callers are timer
//     callbacks and the layout path; blocking either for the seconds a sentence
//     takes to synthesise would freeze the countdown or the window.
//   - It is fire-and-forget by design: a notice that cannot be spoken is not an
//     error the user needs to see, and the bubble still delivers the message.
//
// Speech is skipped entirely when it is switched off, unconfigured, or when the
// caller's own preference has it off - so this can be called unconditionally from
// the event site and the decision lives in one place.
func (a *App) SpeakLine(text string, enabled func(store.Config) bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	cfg := a.store.Config()
	if !cfg.TTSReady() {
		return
	}
	if enabled != nil && !enabled(cfg) {
		return
	}

	// Interrupt whatever is speaking. The same rule as a chat reply: two voices at
	// once is never wanted, and a notification is the case where the old speech is
	// most likely to be stale (the user has moved on).
	a.stopSpeech()

	// A pipeline of its own rather than a chat round: the text is complete, so
	// there is nothing to stream and nothing to hold back. MaxSentences is set
	// high enough that the whole notice is spoken rather than truncated at the
	// chat budget - a two-clause reminder cut to one clause would be misleading.
	//
	// WholeReply is deliberately NOT set here, unlike a chat reply. A notice is a
	// single short sentence that is usually already in the cache, so gathering it
	// first would buy nothing and would delay the sound - and for a timer or a
	// reminder, arriving late is the same as not arriving.
	p := tts.NewPipeline(context.Background(), tts.Options{
		Settings:     a.ttsSettings(cfg),
		MaxSentences: maxNoticeSentences,
		ChunkRunes:   cfg.TTSChunkRunes,
		ChunkCap:     cfg.TTSChunkCap(),
		Cache:        a.ttsCache(cfg),
		Synth:        a.ttsClient(),
		Speaker:      a.speechSpeaker(),
		OnError: func(err error) {
			a.DebugLog("tts: notice: " + err.Error())
		},
	})

	a.speech.mu.Lock()
	a.speech.current = p
	a.speech.mu.Unlock()

	p.Add(text)
	p.Flush()

	go func() {
		p.Wait()
		a.speech.mu.Lock()
		if a.speech.current == p {
			a.speech.current = nil
		}
		a.speech.mu.Unlock()
		p.Close()
	}()
}

// maxNoticeSentences bounds one spoken notice.
//
// Larger than the chat default because a notice is short and complete: cutting it
// off mid-way would leave the user with a half-delivered instruction ("该休息了，
// 还有…"), which is worse than a slightly longer utterance.
const maxNoticeSentences = 8

// speakPomodoroLine announces the end of a phase.
//
// The wording is written as something the PET says, not as a timer caption: the
// whole point of reading it aloud is that the user is looking at something else,
// so it has to make sense with no visual context at all.
func (a *App) speakPomodoroLine(endedPhase string) {
	var line string
	if store.IsFocusPhase(endedPhase) {
		line = "专注时间结束了，起来活动一下吧。"
	} else {
		line = "休息结束了，我们继续吧。"
	}
	a.SpeakLine(line, func(c store.Config) bool { return c.TTSAutoReadPomodoro })
}

// speakReminderLine announces a todo reminder.
//
// The counts are spoken rather than the titles: a list of todo titles read aloud
// is long, and the user cannot act on it without looking at the bubble anyway -
// what they need to know from across the room is simply that something is due.
func (a *App) speakReminderLine(due, overdue, total int) {
	if total == 0 {
		return
	}
	var line string
	switch {
	case overdue > 0:
		line = fmt.Sprintf("有 %d 件事情已经逾期了，记得看一下。", overdue)
	case due > 0:
		line = fmt.Sprintf("今天有 %d 件事情到期，别忘了。", due)
	default:
		line = fmt.Sprintf("你还有 %d 件待办没有完成。", total)
	}
	a.SpeakLine(line, func(c store.Config) bool { return c.TTSAutoReadReminder })
}

// speechSpeaker returns the audio player, defaulting to the real Windows one.
//
// Tests substitute a recorder by assigning a.speech.speaker directly - they live in
// this package, so no exported hook is needed. That matters: Wails binds EVERY
// exported method on App, so an exported test seam would appear in the generated
// frontend bindings and expose an internal type (tts.Speaker) to the webview, which
// is both noise in the public surface and a hole in the layering.
func (a *App) speechSpeaker() tts.Speaker {
	a.speech.mu.Lock()
	defer a.speech.mu.Unlock()
	if a.speech.speaker == nil {
		a.speech.speaker = desktopSpeaker{}
	}
	return a.speech.speaker
}

// speechDirName is the subdirectory of the data directory holding synthesised
// audio.
//
// It lives inside the app's own data directory rather than the system temp folder
// so the clips are covered by the same permissions and cleanup as everything else
// the app owns - and so a user who wants to reclaim the space has one place to
// look.
const speechDirName = "tts-cache"

// ttsClient returns the shared synthesis client, creating it on first use.
func (a *App) ttsClient() *tts.Client {
	a.speech.mu.Lock()
	defer a.speech.mu.Unlock()
	if a.speech.client == nil {
		a.speech.client = tts.NewClient()
	}
	return a.speech.client
}

// ttsCache returns the audio cache, creating it on first use.
//
// Lazy creation is what keeps a build with speech switched off from creating a
// directory it will never use - the same reasoning as secretStore.
//
// The cache is REBUILT when the configured budget changes. Caching the instance
// alone would be wrong in a way that is easy to miss: the budget is a live setting
// (the panel can change it, and config.json can be edited between runs), while the
// Cache instance captures it at construction. Keeping a stale instance would mean a
// user who switched caching off still had every line served from disk, and one who
// raised the limit still had the old, smaller one enforced.
func (a *App) ttsCache(cfg store.Config) *tts.Cache {
	a.speech.mu.Lock()
	defer a.speech.mu.Unlock()

	if a.speech.cache != nil && a.speech.cacheMB == cfg.TTSCacheMaxMB {
		return a.speech.cache
	}

	dir := filepath.Join(a.store.Dir(), speechDirName)
	cache, err := tts.NewCache(dir, cfg.TTSCacheMaxMB)
	if err != nil {
		// A cache that cannot be created is not a reason to refuse to speak: every
		// sentence is simply synthesised again.
		a.DebugLog("tts: cache unavailable: " + err.Error())
		cache, _ = tts.NewCache("", 0)
	}
	a.speech.cache = cache
	a.speech.cacheMB = cfg.TTSCacheMaxMB
	return cache
}

// ttsSettings assembles the client settings from the config.
//
// It is the ONE place that knows the two backends differ, and it hides that from
// everything downstream: the client, the cache, the splitter and the playback
// pipeline all see the same Settings either way.
//
// The two differences are both about where a voice comes from:
//
//   - URL: the local api_v2.py or the online bridge (different ports, so both can
//     be running and switching is instant).
//   - Identity: a local character is identified by the PATH of its reference clip
//     and its two checkpoints. The online bridge has none of those - the public
//     service owns the voice - so the character NAME is passed as both the
//     reference and the GPT weight, which is exactly what the bridge reads.
func (a *App) ttsSettings(cfg store.Config) tts.Settings {
	if cfg.TTSIsOnline() {
		voice := strings.TrimSpace(cfg.TTSCharacter)
		return tts.Settings{
			BaseURL: cfg.TTSActiveURL(),
			// The bridge takes the voice name from ref_audio_path for parity with
			// GPT-SoVITS, and from /set_gpt_weights when the character is
			// switched. Sending both means either path works.
			RefAudio:   voice,
			PromptText: "",
			Lang:       cfg.TTSLang,
			GPTWeights: voice,
			// No SoVITS weight: the bridge treats that call as a no-op, and
			// sending the voice name there would be meaningless.
			Timeout: time.Duration(cfg.TTSTimeoutSec) * time.Second,
		}
	}
	return tts.Settings{
		BaseURL:       cfg.TTSActiveURL(),
		RefAudio:      cfg.TTSRefAudioPath,
		PromptText:    cfg.TTSPromptText,
		Lang:          cfg.TTSLang,
		GPTWeights:    cfg.TTSGPTWeights,
		SovitsWeights: cfg.TTSSovitsWeights,
		Timeout:       time.Duration(cfg.TTSTimeoutSec) * time.Second,
	}
}

// initSpeech wires up the speech runtime at startup.
//
// It cannot fail. Speech is an optional local service, so the worst case is that
// the user hears nothing - which must never delay the pet appearing.
func (a *App) initSpeech() {
	cfg := a.store.Config()
	// Reclaim scratch clips orphaned by a hard kill during playback. Nothing else
	// would ever collect them.
	tts.CleanupTempDir(filepath.Join(a.store.Dir(), speechDirName))
	a.DebugLog(fmt.Sprintf("tts: enabled=%v backend=%s configured=%v -> %s",
		cfg.TTSEnabled, cfg.TTSBackend, cfg.TTSConfigured(), cfg.TTSEndpoint()))
}

// shouldSpeak reports whether this reply should be read aloud.
//
// The two auto-read switches are checked separately because the two kinds of
// utterance are genuinely different: a proactive remark is capped at 80 runes and
// suits speech best, while a long answer is the case a user is most likely to want
// silenced.
func shouldSpeak(cfg store.Config, proactive bool) bool {
	if !cfg.TTSReady() {
		return false
	}
	if proactive {
		return cfg.TTSAutoReadProactive
	}
	return cfg.TTSAutoReadChat
}

// startSpeech begins speaking a new utterance, replacing any speech in progress.
//
// It returns nil when speech is switched off or unconfigured, which is the signal
// to the caller that there is nothing to feed. A nil return is the NORMAL case in
// a default install, so every call site treats it as "carry on silently" rather
// than as an error.
func (a *App) startSpeech(cfg store.Config, proactive bool) *tts.Pipeline {
	if !shouldSpeak(cfg, proactive) {
		return nil
	}

	// Interrupt whatever is still speaking BEFORE starting the new pipeline. Two
	// voices at once is never what the user wants, and a reply that arrives while
	// the previous one is still being read out is the common case (the user
	// interrupted the pet by asking something else).
	a.stopSpeech()

	p := tts.NewPipeline(context.Background(), tts.Options{
		Settings:     a.ttsSettings(cfg),
		MaxSentences: cfg.TTSMaxSentences,
		ChunkRunes:   cfg.TTSChunkRunes,
		// The backend's own input limit. The online service refuses anything past
		// ~100 characters with NO audio, so a chunk over it is a sentence the user
		// simply never hears - see store.TTSChunkCap.
		ChunkCap: cfg.TTSChunkCap(),
		// Wait for the WHOLE reply before speaking any of it.
		//
		// Playing sentence by sentence makes the reply land as a list: each clip is
		// a separate utterance with its own intonation and a synthesis pause before
		// the next, which the user described as strange. Gathering first makes it
		// one continuous answer, at the cost of the first sound arriving later.
		WholeReply: true,
		Cache:      a.ttsCache(cfg),
		Synth:      a.ttsClient(),
		Speaker:    a.speechSpeaker(),
		OnError: func(err error) {
			// Every failure here is non-fatal by design: speech decorates an
			// answer the user already has on screen. It is logged rather than
			// shown, because a bubble about audio would interrupt the reply the
			// user is reading.
			a.DebugLog("tts: " + err.Error())
		},
		OnProgress: a.onSpeechProgress,
	})

	a.speech.mu.Lock()
	a.speech.current = p
	a.speech.mu.Unlock()
	return p
}

// finishSpeech marks an utterance complete and lets the queued sentences play out
// in the background.
//
// It is called (deferred) on every terminal path of a chat request. The reason it
// does NOT simply wait is the same reason SendChat returns immediately: this runs
// on the request goroutine, and the remaining audio can take seconds. Blocking here
// would delay the request's own cleanup - and, worse, the next request would have
// to wait for the previous reply to finish being spoken before it could even
// start.
//
// Flush closes the input and hands over the trailing sentence; the goroutine then
// waits for the speech to drain, reveals any text that was never spoken, and only
// then releases the pipeline.
func (a *App) finishSpeech(p *tts.Pipeline) {
	p.Flush()

	// The round this pipeline belongs to, captured now: by the time the drain
	// finishes, a newer request may own the speech state.
	a.speech.mu.Lock()
	id := a.speech.reqID
	full := a.speech.full
	a.speech.mu.Unlock()

	go func() {
		// Wait for the queued sentences to finish playing, not merely to be
		// synthesised. This is bounded by the audio already accepted (at most
		// TTSMaxSentences clips), so it cannot run away.
		p.Wait()

		// Only clear the field if this pipeline is still the current one. A new
		// reply may have replaced it while this one was draining, and clobbering
		// that newer pipeline would silence the wrong utterance - or lose the
		// handle to it entirely, leaving speech that nothing can stop.
		a.speech.mu.Lock()
		if a.speech.current == p {
			a.speech.current = nil
		}
		a.speech.mu.Unlock()

		// Release whatever was held but never spoken, and only AFTER the drain:
		// revealing earlier would put the tail of the reply on screen while the
		// voice was still working through the earlier sentences, which is the lag
		// this whole mechanism exists to remove.
		a.revealRemaining(id, full)

		p.Close()
	}()
}

// stopSpeech silences the pet immediately.
//
// It is safe to call when nothing is speaking, and it is called from several paths
// at once (a new reply, the stop button, shutdown), so it takes the pipeline out of
// the field first and stops it after releasing the lock - the stop itself can touch
// the audio device, and holding the speech mutex across that would let the UI block
// on it.
func (a *App) stopSpeech() {
	a.speech.mu.Lock()
	p := a.speech.current
	a.speech.current = nil
	a.speech.mu.Unlock()
	if p != nil {
		p.Stop()
	}
}

// desktopSpeaker is the production Speaker, forwarding to the Windows player.
//
// It is a named type rather than a closure so the wiring stays obvious, and so the
// tts package never has to import desktop.
type desktopSpeaker struct{}

func (desktopSpeaker) PlayWAVFile(path string) error { return desktop.PlayWAVFile(path) }
func (desktopSpeaker) StopAudio()                    { desktop.StopAudio() }

// --- bindings --------------------------------------------------------------

// SpeechView is the speech state the settings panel renders.
type SpeechView struct {
	Enabled bool `json:"enabled"`
	// Ready is true when an utterance could be synthesised right now.
	Ready bool `json:"ready"`
	// NotReadyReason explains a false Ready in one line.
	NotReadyReason string `json:"notReadyReason,omitempty"`
	// Speaking is true while the pet is talking, so the panel can offer a stop
	// button only when it would do something.
	Speaking bool `json:"speaking"`
	// Endpoint and the character fields are echoed so the panel can show what is
	// configured without re-deriving it.
	Endpoint     string `json:"endpoint"`
	Character    string `json:"character"`
	RefAudio     string `json:"refAudio"`
	PromptText   string `json:"promptText"`
	Lang         string `json:"lang"`
	GPTWeights   string `json:"gptWeights"`
	SovitsWeight string `json:"sovitsWeights"`
	// ModelsDir is the folder the panel scans for characters, and Voices is what
	// it found. Shipping the list with the state means the dropdown is populated
	// from the same snapshot as everything else, so it cannot disagree with the
	// paths shown beside it.
	ModelsDir string      `json:"modelsDir"`
	Voices    []VoiceInfo `json:"voices"`
	// Backend is "local" or "online". Endpoint (above) is the address of the
	// ACTIVE backend; LocalURL and OnlineURL are the two stored addresses, so the
	// panel can show the right one without either overwriting the other.
	Backend   string `json:"backend"`
	LocalURL  string `json:"localUrl"`
	OnlineURL string `json:"onlineUrl"`
	// The four "read this aloud" switches, echoed so the panel renders them from
	// the same snapshot as everything else.
	ReadChat      bool `json:"readChat"`
	ReadProactive bool `json:"readProactive"`
	ReadPomodoro  bool `json:"readPomodoro"`
	ReadReminder  bool `json:"readReminder"`
	// BackendReady distinguishes "this backend is usable" from the overall
	// readiness, so the panel can say which side is at fault when both are
	// configured.
	BackendReady    bool   `json:"backendReady"`
	BackendNotReady string `json:"backendNotReady,omitempty"`
	// CacheMB and CacheBytes let the panel show and reclaim the disk usage.
	CacheMB    int   `json:"cacheMB"`
	CacheBytes int64 `json:"cacheBytes"`
	// CacheDir is shown so the user can find (or delete) the audio.
	CacheDir string `json:"cacheDir"`
}

// speechView assembles the speech snapshot.
func (a *App) speechView() SpeechView {
	cfg := a.store.Config()

	a.speech.mu.Lock()
	speaking := a.speech.current != nil
	a.speech.mu.Unlock()

	view := SpeechView{
		Enabled:      cfg.TTSEnabled,
		Speaking:     speaking,
		Endpoint:     cfg.TTSEndpoint(),
		Character:    cfg.TTSCharacter,
		RefAudio:     cfg.TTSRefAudioPath,
		PromptText:   cfg.TTSPromptText,
		Lang:         cfg.TTSLang,
		GPTWeights:   cfg.TTSGPTWeights,
		SovitsWeight: cfg.TTSSovitsWeights,
		CacheMB:      cfg.TTSCacheMaxMB,
		CacheDir:     filepath.Join(a.store.Dir(), speechDirName),
		Ready:        cfg.TTSReady(),
		Backend:      cfg.TTSBackend,
		// The four auto-read switches, so the panel renders them from the same
		// snapshot it saves.
		ReadChat:      cfg.TTSAutoReadChat,
		ReadProactive: cfg.TTSAutoReadProactive,
		ReadPomodoro:  cfg.TTSAutoReadPomodoro,
		ReadReminder:  cfg.TTSAutoReadReminder,
		// Both stored addresses, NOT the active one twice. Filling OnlineURL from
		// TTSActiveURL() was a real bug: while the local backend was selected it
		// returned the 9880 address, the panel wrote that into its (single) address
		// field, and saving then stored 9880 as the ONLINE address - so switching
		// to online asked 9880 for a voice list and reported "连不上在线服务".
		LocalURL:  cfg.TTSBaseURL,
		OnlineURL: cfg.TTSOnlineURL,
		ModelsDir: cfg.TTSModelsDir,
		Voices:    a.DiscoverVoices(),
	}

	// The first reason found wins, so the panel explains the most fundamental
	// problem rather than the last one checked - the same rule llmView uses.
	//
	// There is deliberately no case for an empty TTSPromptText: it is a valid
	// configuration (clone the timbre without text alignment), not a missing
	// field. Treating it as unfinished is what pushed users into typing a
	// transcript they had to guess, which is worse than leaving it blank.
	switch {
	case !cfg.TTSEnabled:
		view.NotReadyReason = "语音朗读未开启"
		view.BackendNotReady = view.NotReadyReason
	case cfg.TTSIsOnline():
		// The online backend needs a voice name, not a file. Saying "还没有填写参考
		// 音频" here would send the user looking for a file that does not exist.
		if strings.TrimSpace(cfg.TTSCharacter) == "" {
			view.NotReadyReason = "还没有选择在线音色"
			view.BackendNotReady = view.NotReadyReason
		}
	case strings.TrimSpace(cfg.TTSRefAudioPath) == "":
		view.NotReadyReason = "还没有填写参考音频"
		view.BackendNotReady = view.NotReadyReason
	}
	view.BackendReady = view.BackendNotReady == ""

	if cache := a.ttsCache(cfg); cache != nil {
		view.CacheBytes = cache.Size()
	}
	return view
}

// SpeechStatus is what TestTTSConnection reports.
type SpeechStatus struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latencyMs"`
	Message   string `json:"message"`
}

// TestTTSConnection probes the synthesis server, so the settings panel can tell a
// user whether their setup works before they rely on it.
//
// It deliberately does NOT synthesise: a real sentence would cost seconds on CPU
// and would speak out loud as a side effect of pressing "test". A probe that gets
// any HTTP answer at all proves the server is running and reachable, which is the
// question being asked.
func (a *App) TestTTSConnection() (SpeechStatus, error) {
	cfg := a.store.Config()
	start := time.Now()
	err := a.ttsClient().Ping(context.Background(), cfg.TTSBaseURL)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return SpeechStatus{OK: false, LatencyMS: latency, Message: err.Error()}, nil
	}
	return SpeechStatus{
		OK:        true,
		LatencyMS: latency,
		Message:   fmt.Sprintf("语音服务已就绪（%dms）。", latency),
	}, nil
}

// StopSpeech silences the pet right now. It backs the panel's stop button and is
// safe to call when nothing is speaking.
//
// It returns the WHOLE State, not just the speech part. Every other bound method
// that the panel calls through applyState does, and returning a narrower struct
// was a real bug: main.js assigns the result straight to its `state`, so a
// SpeechView landing there wiped petImageUrl and the pet silently reverted to the
// default picture on the next render. The type of the return value is the
// contract, and it has to match the other settings methods.
func (a *App) StopSpeech() (State, error) {
	a.stopSpeech()
	return a.GetState()
}

// SaveTTSSettings writes the speech configuration.
//
// The input mirrors the config fields one-for-one; there is no secret involved
// (the server is local), so unlike the LLM path there is no separate key call.
// It returns the whole State for the reason StopSpeech documents.
//
// "One-for-one" is a real contract, not a description: the input is the WHOLE
// form, so a field left at its zero value is written as zero. That is why the four
// auto-read booleans are assigned unconditionally - a caller that wants to change
// one path must send the switches too, and the frontend always does. Treating an
// absent boolean as "leave it alone" would make it impossible to turn a switch OFF,
// which is the more common direction.
func (a *App) SaveTTSSettings(in SpeechView) (State, error) {
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.TTSEnabled = in.Enabled
		// Which backend: an unknown value falls back to local in NormaliseTTS.
		if strings.TrimSpace(in.Backend) != "" {
			c.TTSBackend = in.Backend
		}
		// The two backends have separate addresses, so switching never loses
		// either one. The panel shows whichever is active, so the field it sends
		// is routed to the matching config key.
		if strings.TrimSpace(in.Endpoint) != "" {
			trimmed := trimTTSPath(in.Endpoint)
			if strings.EqualFold(strings.TrimSpace(in.Backend), store.TTSBackendOnline) {
				c.TTSOnlineURL = trimmed
			} else {
				// The panel shows the full /tts URL, but the config stores the root.
				c.TTSBaseURL = trimmed
			}
		}
		c.TTSCharacter = in.Character
		c.TTSRefAudioPath = in.RefAudio
		c.TTSPromptText = in.PromptText
		c.TTSLang = in.Lang
		c.TTSGPTWeights = in.GPTWeights
		c.TTSSovitsWeights = in.SovitsWeight
		c.TTSModelsDir = in.ModelsDir
		// The four "read this aloud" switches. They were previously rendered by the
		// panel and never sent, so all four checkboxes did nothing at all - the
		// config kept whatever it had and the UI silently disagreed with it.
		c.TTSAutoReadChat = in.ReadChat
		c.TTSAutoReadProactive = in.ReadProactive
		c.TTSAutoReadPomodoro = in.ReadPomodoro
		c.TTSAutoReadReminder = in.ReadReminder
		if in.CacheMB != 0 {
			c.TTSCacheMaxMB = in.CacheMB
		}
	}); err != nil {
		return State{}, err
	}

	// The character may have changed, so the client's record of what the server has
	// loaded is no longer trustworthy. Forgetting it makes the next utterance push
	// the new weights instead of silently keeping the old voice - which would look
	// exactly like the setting being ignored.
	a.ttsClient().ForgetCharacter()

	// No cache reset is needed here: ttsCache rebuilds itself when the configured
	// budget differs from the one its instance was built with.
	return a.GetState()
}

// trimTTSPath reduces a full endpoint URL back to the server root.
//
// The panel displays "…/tts" (that is the address a user recognises from the
// GPT-SoVITS docs), but the config stores the root because the client appends the
// path itself. Stripping it here keeps the round trip idempotent: saving what the
// panel showed must not accumulate "/tts/tts".
//
// Trailing slashes are removed FIRST. Otherwise "…/tts/" fails the suffix match
// and is stored as a base URL of "…/tts", which the client then extends to
// "…/tts/tts" - a 404 on a setup that looked correct in the panel.
func trimTTSPath(endpoint string) string {
	s := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	for _, suffix := range []string{"/tts", "/set_gpt_weights", "/set_sovits_weights"} {
		s = strings.TrimSuffix(s, suffix)
	}
	// A suffix removal can expose another slash ("…/tts//" -> "…/tts" -> "…/").
	return strings.TrimRight(s, "/")
}

// ClearTTSCache removes every cached clip and reports the freed space.
//
// It returns the whole State for the reason StopSpeech documents.
func (a *App) ClearTTSCache() (State, error) {
	cfg := a.store.Config()
	cache := a.ttsCache(cfg)
	if cache != nil {
		if err := cache.Clear(); err != nil {
			return State{}, err
		}
	}
	return a.GetState()
}

// VoiceInfo is one character the panel can offer.
type VoiceInfo struct {
	// ID is the option value: the sub-folder name for a local voice, or the
	// service's own voice name for an online one.
	ID string `json:"id"`
	// Name is what the panel shows. For a local voice it prefers a label from the
	// character's own petpc-config.json, because the folder name is usually a
	// romanised slug ("klee") while the label is the name the user recognises
	// ("可莉"). For an online voice the service's name is already the label.
	Name string `json:"name"`
	// Paths are absolute and ready to use as-is. They are empty for an online
	// voice, which has no files on this machine.
	GPTWeights    string `json:"gptWeights"`
	SovitsWeights string `json:"sovitsWeights"`
	RefAudio      string `json:"refAudio"`
	// Complete is false when a required file is missing, so the panel can grey the
	// entry out instead of letting the user pick a voice that cannot load. Always
	// true for an online voice: the service either has it or it does not.
	Complete bool `json:"complete"`
	// Missing lists what is absent, for the tooltip.
	Missing []string `json:"missing,omitempty"`
	// Online marks a voice served by the bridge rather than found on disk, so the
	// panel can group the two lists when both are shown.
	Online bool `json:"online,omitempty"`
}

// voiceFolderNames are the file names a character folder may use.
//
// Several spellings are accepted because the two things that produce these
// folders disagree: tools/tts-p0/fetch.ps1 writes gpt.ckpt/sovits.pth/ref.wav,
// while a folder copied straight out of a model repo is full of
// "<Name>-e10.ckpt" and "<Name>_e8_s656.pth". Recognising both means a user who
// downloaded models by hand still gets a working dropdown.
var (
	gptNamePatterns    = []string{"gpt.ckpt"}
	sovitsNamePatterns = []string{"sovits.pth"}
	refNamePatterns    = []string{"ref.wav", "ref_original.wav"}
)

// DiscoverVoices lists the characters the active backend offers.
//
// For the local backend that means scanning the models directory; for the online
// one it means asking the bridge. It returns an empty slice (never an error) when
// nothing can be found: "no voices" is a normal state the panel explains, not a
// failure the user needs to see a stack trace for.
func (a *App) DiscoverVoices() []VoiceInfo {
	cfg := a.store.Config()
	if cfg.TTSIsOnline() {
		return a.discoverOnlineVoices(cfg)
	}
	return scanVoices(cfg.TTSModelsDir)
}

// discoverOnlineVoices asks the bridge which voices the upstream service has.
//
// A failure returns an empty list rather than an error. The usual cause is that
// the bridge is not running, which the panel already reports through the
// readiness line and the connection test - surfacing it twice would be noise.
func (a *App) discoverOnlineVoices(cfg store.Config) []VoiceInfo {
	url := cfg.TTSVoicesEndpoint()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		a.DebugLog("tts: online voice list request: " + err.Error())
		return nil
	}
	resp, err := onlineVoicesClient().Do(req)
	if err != nil {
		a.DebugLog("tts: online voice list unavailable: " + err.Error())
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		a.DebugLog(fmt.Sprintf("tts: online voice list returned HTTP %d", resp.StatusCode))
		return nil
	}

	var payload struct {
		Voices []string `json:"voices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		a.DebugLog("tts: online voice list was not JSON: " + err.Error())
		return nil
	}

	out := make([]VoiceInfo, 0, len(payload.Voices))
	for _, name := range payload.Voices {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, VoiceInfo{
			ID:       name,
			Name:     name,
			Complete: true,
			Online:   true,
		})
	}
	// Sorted here rather than trusting the service's order, so the dropdown does
	// not reshuffle between runs and the Genshin voices are findable by name.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// onlineVoicesClient is a small client for the bridge's metadata endpoints.
//
// It is separate from tts.Client because that one is built for loopback synthesis
// with a 2-second dial timeout; this one fetches a list that the bridge may
// itself be building on the fly, and must survive that taking a moment.
func onlineVoicesClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:               nil, // loopback must not be proxied
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			MaxIdleConnsPerHost: 2,
		},
	}
}

// scanVoices is the pure part of DiscoverVoices, so it can be tested against a
// temporary directory without an App.
func scanVoices(dir string) []VoiceInfo {
	out := []VoiceInfo{}
	if strings.TrimSpace(dir) == "" {
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		v := VoiceInfo{ID: e.Name(), Name: e.Name()}

		v.GPTWeights, v.Missing = pickVoiceFile(sub, gptNamePatterns, "gpt.ckpt", v.Missing)
		v.SovitsWeights, v.Missing = pickVoiceFile(sub, sovitsNamePatterns, "sovits.pth", v.Missing)
		v.RefAudio, v.Missing = pickVoiceFile(sub, refNamePatterns, "ref.wav", v.Missing)
		v.Complete = len(v.Missing) == 0

		// A label from the character's own config wins over the folder name.
		if label := labelFromFolderConfig(sub); label != "" {
			v.Name = label
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		// Complete voices first, then alphabetical: the usable ones are what the
		// user is looking for.
		if out[i].Complete != out[j].Complete {
			return out[i].Complete
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// pickVoiceFile finds the first candidate name that exists, appending a
// human-readable note to missing when none does.
//
// It also accepts the first matching checkpoint of a hand-assembled folder (for
// example "Klee-e10.ckpt" or "Klee_e8_s656.pth"), because the epoch suffix varies
// per model and demanding one exact name would reject a perfectly usable voice.
func pickVoiceFile(dir string, exact []string, fallbackLabel string, missing []string) (string, []string) {
	for _, name := range exact {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, missing
		}
	}
	// Fall back to a pattern match on the extension.
	ext := filepath.Ext(fallbackLabel)
	entries, err := os.ReadDir(dir)
	if err == nil {
		var matches []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			n := e.Name()
			if strings.EqualFold(filepath.Ext(n), ext) {
				matches = append(matches, n)
			}
		}
		if len(matches) > 0 {
			sort.Strings(matches)
			return filepath.Join(dir, matches[0]), missing
		}
	}
	return "", append(missing, fallbackLabel)
}

// labelFromFolderConfig reads a display name from the character's own
// petpc-config.json, if one was written there.
func labelFromFolderConfig(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "petpc-config.json"))
	if err != nil {
		return ""
	}
	var v struct {
		Character string `json:"ttsCharacter"`
	}
	if json.Unmarshal(data, &v) != nil {
		return ""
	}
	return strings.TrimSpace(v.Character)
}

// PickTTSFile opens a file dialog for one of the speech settings' path fields.
//
// which is one of "gpt", "sovits" or "ref" and only decides the dialog's title and
// filter. It returns the chosen path, or "" when the user cancels - the frontend
// leaves the field alone in that case, so a cancel cannot blank a working path.
//
// This exists because typing three long Windows paths by hand is the single most
// error-prone part of setting speech up, and a mistyped path fails at synthesis
// time with a server-side "file not found" that does not say which field was
// wrong.
func (a *App) PickTTSFile(which string) (string, error) {
	var title string
	var pattern string
	switch which {
	case "gpt":
		title, pattern = "选择 GPT 权重 (.ckpt)", "*.ckpt"
	case "sovits":
		title, pattern = "选择 SoVITS 权重 (.pth)", "*.pth"
	case "ref":
		title, pattern = "选择参考音频 (.wav)", "*.wav"
	default:
		return "", fmt.Errorf("未知的文件类型：%s", which)
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: title,
		Filters: []runtime.FileFilter{
			{DisplayName: pattern, Pattern: pattern},
			{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(path), nil
}

// PickTTSModelsDir opens a folder dialog for the models directory, so the user can
// point the panel at the folder install.ps1 produced without typing it.
func (a *App) PickTTSModelsDir() (string, error) {
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择语音角色所在的文件夹（每个角色一个子文件夹）",
	})
	if err != nil {
		return "", err
	}
	return strings.TrimRight(strings.TrimSpace(path), `\/`), nil
}

// ttsShutdown stops speech when the app quits.
//
// It is separate from stopSpeech because shutdown must also wait for the workers:
// leaving them running would mean a goroutine writing a scratch file into a
// directory the process is about to abandon, and a WAV still playing after the pet
// has gone.
func (a *App) ttsShutdown() {
	a.speech.mu.Lock()
	p := a.speech.current
	a.speech.current = nil
	a.speech.mu.Unlock()
	if p != nil {
		p.Close()
	}
}
