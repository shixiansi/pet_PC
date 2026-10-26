package tts

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"
)

// Speaker plays WAV files and can stop them. It is an interface so the pipeline
// can be tested without an audio device - the same reason llm.Client exposes its
// HTTP field.
type Speaker interface {
	// PlayWAVFile starts playback and returns once it has begun, not when it
	// ends.
	PlayWAVFile(path string) error
	// StopAudio stops anything currently playing.
	StopAudio()
}

// Synthesizer turns one chunk of text into WAV bytes.
//
// An interface so the pipeline's ordering, limiting and interruption logic can be
// tested without a server.
type Synthesizer interface {
	Synthesize(ctx context.Context, s Settings, text string) ([]byte, error)
}

// Options configures a Pipeline.
type Options struct {
	Settings Settings
	// MaxSentences caps how many chunks of one utterance are spoken.
	MaxSentences int
	// ChunkRunes is the splitter's target length.
	ChunkRunes int
	// ChunkCap is an absolute ceiling on one chunk, or 0 to derive it from
	// ChunkRunes. A backend that refuses long input (the online bridge caps at 100
	// characters) sets it so the splitter can never build a request the server
	// will reject outright.
	ChunkCap int
	// Cache stores synthesised audio. May be nil for no caching.
	Cache *Cache
	// Synth and Speaker are the injected dependencies.
	Synth   Synthesizer
	Speaker Speaker
	// TempDir is where scratch clips are written. Empty means the cache
	// directory, or the system temp directory when there is no cache.
	TempDir string
	// OnError receives non-cancellation failures, for the debug log and the
	// settings panel. It may be nil. It must not block.
	OnError func(error)
	// OnProgress reports the text whose audio is about to be heard, as the full
	// prefix of the reply revealed so far.
	//
	// It exists so the on-screen text can keep pace with the voice. The chat
	// stream would otherwise show a reply several seconds before it is spoken, and
	// on this machine synthesis is slower than generation - so the user reads the
	// whole answer while the pet is still saying the first clause, which reads as
	// the text and the voice being unrelated.
	//
	// A whole PREFIX rather than an increment, deliberately: the receiver then
	// only ever has to assign, so a reordered or duplicated report cannot corrupt
	// the text, and there is no need to pair increments with their request.
	//
	// It is called from the playback goroutine, immediately before each clip
	// starts, and it must not block - the caller emits a UI event. With
	// WholeReply it is called ONCE with the entire reply, just before playback
	// begins.
	OnProgress func(revealed string)
	// WholeReply synthesises every sentence before playing ANY of them, and
	// reveals the text in one go.
	//
	// It exists because playing sentence by sentence makes the pet sound like it is
	// reading a list: each clip is a separate utterance with its own intonation and
	// its own pause before the next, so a reply arrives as "一句。……一句。……" instead
	// of as one answer. Waiting for the whole reply first trades that for latency.
	//
	// The tradeoff is exact and worth stating: the first sound now arrives after
	// the WHOLE reply has been synthesised rather than after the first sentence.
	// On this machine that is seconds, not milliseconds, which is why it is a
	// choice and not simply the better default.
	//
	// It costs nothing in total time - synthesis is serial either way, and the
	// previous overlap only hid synthesis behind playback, which is not where the
	// time goes on a CPU-bound server.
	WholeReply bool
	// Sleep allows tests to make the playback wait deterministic. Nil means
	// time.Sleep.
	Sleep func(time.Duration)
}

// Pipeline speaks a stream of text as it arrives.
//
// The problem it solves is the one in docs/genshin-tts.md 5: on a CPU-only
// machine synthesis takes roughly as long as playback, so waiting for a complete
// reply before speaking would leave the user waiting ten or twenty seconds to
// hear anything. Text is instead fed in as the model streams it, split into
// sentences, and two workers hand work to each other:
//
//	synth worker: pulls text -> synthesises -> pushes audio
//	play  worker: pulls audio -> plays -> waits out the clip
//
// That split is what creates the overlap. While sentence N is playing (a pure
// wait - winmm gives no completion callback), the synth worker is already busy on
// sentence N+1, so the CPU never idles during playback. There is still only ever
// ONE synthesis call in flight: a second synth worker would buy one sentence of
// lookahead while making both calls contend for the same cores on a CPU-bound
// server.
//
// The observable result: the first sound arrives about one sentence's synthesis
// time after the first sentence completes, not after the whole reply.
//
// Concurrency contract: Add and Flush are called from ONE goroutine - the chat
// request goroutine. That single-writer discipline is what makes closing textCh
// safe without a lock around the send.
type Pipeline struct {
	opts Options

	ctx    context.Context
	cancel context.CancelFunc

	// textCh carries sentences from the caller to the synth worker.
	textCh chan string
	// audioCh carries synthesised clips from the synth worker to the player.
	audioCh chan clip

	// finishOnce closes textCh exactly once.
	finishOnce sync.Once
	// finished is closed alongside textCh, so workers can tell "input ended"
	// from "cancelled" and drain rather than abort.
	finished chan struct{}

	mu       sync.Mutex
	splitter *Splitter
	// spoken counts chunks emitted, for MaxSentences.
	spoken int
	// stopped is set when the sentence budget is exhausted, so later text is
	// dropped rather than synthesised and thrown away.
	stopped bool
	// revealed accumulates the text whose audio has been reached, for OnProgress.
	// Guarded by mu because playback runs on its own goroutine.
	revealed string

	wg sync.WaitGroup
}

// clip is one synthesised sentence on its way to the speaker.
type clip struct {
	text    string
	audio   []byte
	seconds float64
}

// queueDepth is how many sentences may be buffered at each stage.
//
// It bounds the work thrown away by a stop: deep enough to hide a slow sentence
// behind a fast one, shallow enough that cancelling does not leave the CPU busy
// for long after the user has moved on.
const queueDepth = 3

// NewPipeline returns a Pipeline ready to receive text.
//
// The caller must eventually call Flush (input is complete) and then Close (free
// the workers).
func NewPipeline(parent context.Context, opts Options) *Pipeline {
	if opts.MaxSentences < 1 {
		opts.MaxSentences = 1
	}
	if opts.ChunkRunes < 1 {
		opts.ChunkRunes = 20
	}
	if opts.Sleep == nil {
		opts.Sleep = time.Sleep
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)

	p := &Pipeline{
		opts:   opts,
		ctx:    ctx,
		cancel: cancel,
		// Buffered to the SENTENCE BUDGET, not to queueDepth. The synth queue is
		// the one the caller writes to, and Add must never block (it runs on the
		// chat stream's goroutine, so blocking there stalls the reply itself).
		// Since at most MaxSentences items are ever enqueued, sizing the buffer to
		// that bound makes blocking structurally impossible rather than merely
		// unlikely. A string header is 16 bytes, so even the 50-sentence maximum
		// costs under a kilobyte.
		textCh:   make(chan string, opts.MaxSentences),
		audioCh:  make(chan clip, queueDepth),
		finished: make(chan struct{}),
		splitter: NewSplitterCapped(opts.ChunkRunes, opts.ChunkCap),
	}
	p.wg.Add(2)
	go p.synthLoop()
	go p.playLoop()
	return p
}

// Add feeds freshly streamed text and queues any sentences it completes.
//
// It never blocks on synthesis or playback: the caller keeps reading the model's
// stream while speech happens behind it. Text beyond MaxSentences is dropped, so
// a long reply does not leave the CPU synthesising for a minute after the user
// has read the first lines and moved on.
func (p *Pipeline) Add(text string) {
	if text == "" {
		return
	}
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	chunks := p.splitter.Add(text)
	p.mu.Unlock()
	p.enqueue(chunks)
}

// Flush speaks the trailing partial sentence and marks the input complete.
//
// It must be called on every terminal path, and it is UNCONDITIONAL: a reply that
// does not end in punctuation still has a tail, and without this the pet would
// stop mid-sentence with no sign that anything was wrong. This mirrors the
// unconditional Flush in llm.Coalescer, for the same reason.
//
// Calling it does not stop playback - it lets the already-queued sentences drain
// and play out, which is what the user expects at the end of a reply.
func (p *Pipeline) Flush() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		p.finish()
		return
	}
	chunks := p.splitter.Flush()
	p.mu.Unlock()

	p.enqueue(chunks)
	p.finish()
}

// finish closes the input channel exactly once.
func (p *Pipeline) finish() {
	p.finishOnce.Do(func() {
		close(p.finished)
		close(p.textCh)
	})
}

// enqueue sends the chunks that fit within the sentence budget.
func (p *Pipeline) enqueue(chunks []string) {
	for _, c := range chunks {
		if !Speakable(c) {
			// Punctuation- or whitespace-only: nothing to say, and asking the
			// server would spend a request and a queue slot on silence.
			continue
		}
		p.mu.Lock()
		if p.stopped {
			p.mu.Unlock()
			return
		}
		if p.spoken >= p.opts.MaxSentences {
			// Budget exhausted. Mark stopped so later text is dropped, and clear
			// the splitter so a subsequent Flush cannot resurrect a tail that
			// will never be spoken.
			p.stopped = true
			p.splitter.Flush()
			p.mu.Unlock()
			p.finish()
			return
		}
		p.spoken++
		p.mu.Unlock()

		select {
		case p.textCh <- c:
		case <-p.ctx.Done():
			return
		case <-p.finished:
			// Input was closed by a concurrent terminal path; dropping is right,
			// because everything already queued still plays.
			return
		}
	}
}

// Wait blocks until the queued speech has drained and played out.
//
// It is the normal end-of-reply step: Flush marks the input complete, and Wait
// lets the pet finish talking. The two are separate from Close because they mean
// opposite things - draining is "say the rest", Close is "stop now" - and
// conflating them would necessarily make one of the two wrong.
//
// It returns when the workers exit, so it is bounded by the audio already queued
// (at most MaxSentences clips), not by anything open-ended.
func (p *Pipeline) Wait() { p.wg.Wait() }

// Close stops speech immediately and waits for the workers to exit.
//
// It is an INTERRUPTION, not an end-of-input: it silences the pet mid-sentence.
// Draining a finished reply is what Flush followed by Wait does.
//
// It is idempotent: it is reached from several terminal paths (a new reply,
// shutdown, an error) and a second call must neither panic nor block.
func (p *Pipeline) Close() {
	p.cancel()
	p.wg.Wait()
	if p.opts.Speaker != nil {
		p.opts.Speaker.StopAudio()
	}
}

// Stop silences the pet without waiting for the workers.
//
// Separate from Close so a user-initiated stop is audible immediately - the
// StopAudio call is synchronous and takes about 4ms - while the goroutines wind
// down in the background. Waiting for an in-flight synthesis request to notice
// its cancellation would otherwise leave the pet talking for a second after the
// user asked it to stop.
func (p *Pipeline) Stop() {
	p.cancel()
	if p.opts.Speaker != nil {
		p.opts.Speaker.StopAudio()
	}
}

// synthLoop pulls sentences, synthesises them, and pushes the audio on.
//
// It also consults the cache first, which is what makes the fixed lines (pomodoro
// notices, the reminder openers) instant on every repeat.
//
// Closing audioCh on the way out is what lets playLoop tell "no more sentences are
// coming" from "nothing has arrived yet" - without it, the player would block
// forever after the last sentence and Wait would never return.
func (p *Pipeline) synthLoop() {
	defer p.wg.Done()
	defer close(p.audioCh)

	for {
		select {
		case <-p.ctx.Done():
			return
		case text, ok := <-p.textCh:
			if !ok {
				return // input complete and drained
			}
			c, ok := p.synthesise(text)
			if !ok {
				return
			}
			select {
			case p.audioCh <- c:
			case <-p.ctx.Done():
				return
			}
		}
	}
}

// synthesise produces one clip, reporting false when the pipeline should stop.
func (p *Pipeline) synthesise(text string) (clip, bool) {
	var audio []byte
	if p.opts.Cache != nil {
		audio = p.opts.Cache.Get(p.opts.Settings, text)
	}
	if audio == nil {
		if p.opts.Synth == nil {
			return clip{}, false
		}
		var err error
		audio, err = p.opts.Synth.Synthesize(p.ctx, p.opts.Settings, text)
		if err != nil {
			if IsCanceled(p.ctx, err) {
				return clip{}, false
			}
			// A failure speaks nothing and moves on. Speech decorates an answer
			// the user already has on screen, so one bad sentence must not abort
			// the rest of the reply.
			p.reportError(err)
			// Signal "skip" by returning an empty clip with ok=true via the
			// caller's loop; using a zero clip keeps the loop simple.
			return clip{}, true
		}
		if p.opts.Cache != nil {
			if cerr := p.opts.Cache.Put(p.opts.Settings, text, audio); cerr != nil {
				// Not fatal: the audio is already in hand.
				p.reportError(cerr)
			}
		}
	}
	if len(audio) == 0 {
		return clip{}, true
	}
	seconds, derr := wavDuration(audio)
	if derr != nil {
		// Playable but unmeasurable: estimate rather than skip. A slightly wrong
		// wait is better than dropping a sentence the user can hear.
		seconds = float64(len([]rune(text))) * 0.18
	}
	return clip{text: text, audio: audio, seconds: seconds}, true
}

// playLoop writes clips to disk, plays them, and waits each one out.
func (p *Pipeline) playLoop() {
	defer p.wg.Done()

	if p.opts.WholeReply {
		p.playWholeReply()
		return
	}

	for {
		select {
		case <-p.ctx.Done():
			return
		case c, ok := <-p.audioCh:
			if !ok {
				return
			}
			if len(c.audio) == 0 {
				continue // a sentence that failed to synthesise
			}
			if !p.playOne(c) {
				return
			}
		}
	}
}

// playWholeReply gathers every clip before playing any of them.
//
// Draining first is what makes the reply sound like one utterance: the clips are
// then handed to the player back to back with no synthesis pause between them, and
// the text is revealed once rather than sentence by sentence.
//
// Draining cannot deadlock against the synth worker. The worker pushes into a
// buffered channel and this loop consumes continuously until that channel closes,
// so the worker is never left waiting on a full buffer while nobody reads.
func (p *Pipeline) playWholeReply() {
	clips := p.gatherClips()
	// A cancel during synthesis means the user moved on; playing a partial reply
	// after they asked it to stop is the one thing worse than silence.
	if len(clips) == 0 || p.ctx.Err() != nil {
		return
	}

	// The text appears with the voice, in one piece. This is the whole point of the
	// mode: revealing per sentence is what made a reply land as a list of separate
	// utterances.
	p.revealAll(clips)

	for _, c := range clips {
		if p.ctx.Err() != nil {
			return
		}
		if !p.playClip(c) {
			return
		}
	}
}

// gatherClips drains the synthesis channel into a slice.
//
// It returns on cancellation as well as on a clean close, and the caller must
// check the context: a run cut short by a stop must not be played.
func (p *Pipeline) gatherClips() []clip {
	var clips []clip
	for {
		select {
		case <-p.ctx.Done():
			return clips
		case c, ok := <-p.audioCh:
			if !ok {
				return clips
			}
			if len(c.audio) == 0 {
				continue // a sentence that failed to synthesise
			}
			clips = append(clips, c)
		}
	}
}

// playOne reveals one sentence's text and then plays it.
//
// Used by the per-sentence mode only; the whole-reply mode reveals everything up
// front and calls playClip directly.
func (p *Pipeline) playOne(c clip) bool {
	// Reveal the text BEFORE the sound starts.
	//
	// Before, not after: the point is that the sentence appears at the moment it
	// is spoken. Reporting after playback would put the text one sentence behind
	// the voice, and reporting at synthesis time (further upstream) would put it
	// seconds ahead - the exact lag being fixed.
	p.reveal(c.text)
	return p.playClip(c)
}

// playClip writes one clip to disk, plays it, and waits it out.
func (p *Pipeline) playClip(c clip) bool {
	path, err := p.writeTemp(c.audio)
	if err != nil {
		p.reportError(err)
		// The audio never plays, so its text must not be revealed either: showing
		// a sentence the user cannot hear would leave the text ahead of the voice
		// again, which is the whole thing this reporting exists to prevent.
		return true // skip this sentence, keep the rest
	}
	defer p.removeTemp(path)

	if p.opts.Speaker != nil {
		if err := p.opts.Speaker.PlayWAVFile(path); err != nil {
			p.reportError(err)
			return true
		}
	}

	// Wait out the clip. winmm reports no completion, so the clip's own stated
	// duration is the only signal available; the file must therefore stay on disk
	// for the whole wait, which is why removal is deferred above.
	return p.waitOrDone(time.Duration(c.seconds * float64(time.Second)))
}

// revealAll reports the entire reply in one report.
//
// It joins the text of the clips that will actually be played, so the revealed
// text still equals the audible words: a sentence that failed to synthesise is not
// in the list, and neither is anything the sentence budget dropped.
func (p *Pipeline) revealAll(clips []clip) {
	if p.opts.OnProgress == nil {
		return
	}
	var b strings.Builder
	for _, c := range clips {
		b.WriteString(c.text)
	}
	out := b.String()
	p.mu.Lock()
	p.revealed = out
	p.mu.Unlock()
	p.opts.OnProgress(out)
}

// reveal reports the text spoken so far, as a prefix of the reply.
//
// The prefix is assembled from the clips that have actually been reached, in
// order, rather than from the splitter's input. The two differ in exactly the
// cases that matter: a sentence that failed to synthesise is dropped from the
// audio, and text beyond the sentence budget is never spoken at all - so building
// the prefix here is what keeps the visible text equal to the audible words rather
// than merely similar to them.
func (p *Pipeline) reveal(text string) {
	if p.opts.OnProgress == nil {
		return
	}
	p.mu.Lock()
	if p.revealed == "" {
		p.revealed = text
	} else {
		// A space between sentences: Chinese replies are not space-separated, but
		// the clips already carry their own trailing punctuation, so joining with
		// nothing reproduces the original text exactly.
		p.revealed += text
	}
	out := p.revealed
	p.mu.Unlock()
	p.opts.OnProgress(out)
}

// waitOrDone sleeps for d, returning false if the pipeline was cancelled first.
//
// The wait is sliced rather than being one long sleep so that a stop is audible
// immediately instead of up to a sentence later. The slices go through
// opts.Sleep so tests can drive playback pacing without real delays - otherwise
// every pipeline test would take as long as the audio it is pretending to play.
func (p *Pipeline) waitOrDone(d time.Duration) bool {
	if d <= 0 {
		return p.ctx.Err() == nil
	}
	const slice = 50 * time.Millisecond
	remaining := d
	for remaining > 0 {
		step := remaining
		if step > slice {
			step = slice
		}
		// The cancellation check comes first so a stop is not delayed by even
		// one slice whenever the context is already done.
		select {
		case <-p.ctx.Done():
			return false
		default:
		}
		p.opts.Sleep(step)
		remaining -= step
	}
	return p.ctx.Err() == nil
}

// writeTemp writes audio to a scratch file for the player.
//
// A file rather than memory because PlaySound's SND_MEMORY form requires the
// buffer to outlive the call with the garbage collector unaware of it - see the
// comment on desktop.PlayWAVFile. The cache cannot serve here: it is keyed by
// content and may be disabled entirely, while the player needs a path for every
// clip.
func (p *Pipeline) writeTemp(audio []byte) (string, error) {
	dir := p.tempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, tempPrefix+"*.wav")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.Write(audio); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// tempPrefix identifies scratch clips, so a startup sweep can reclaim the ones a
// crash orphaned.
const tempPrefix = "pettts-"

// removeTemp deletes a scratch file, ignoring failure: a leftover file is
// collected by the next startup sweep, and failing to delete one must not
// interrupt speech.
func (p *Pipeline) removeTemp(path string) {
	if path != "" {
		os.Remove(path)
	}
}

// tempDir is where scratch clips are written.
func (p *Pipeline) tempDir() string {
	if p.opts.TempDir != "" {
		return p.opts.TempDir
	}
	if p.opts.Cache != nil && p.opts.Cache.dir != "" {
		return p.opts.Cache.dir
	}
	return os.TempDir()
}

// reportError passes a failure to the OnError hook.
//
// Deliberately not logged here: the app layer owns the debug log and knows
// whether logging is enabled at all.
func (p *Pipeline) reportError(err error) {
	if err == nil || p.opts.OnError == nil {
		return
	}
	p.opts.OnError(err)
}

// CleanupTempDir removes scratch clips left behind by a crash.
//
// It runs at startup because a hard kill during playback orphans a file, and
// nothing else would ever collect it.
func CleanupTempDir(dir string) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, tempPrefix) || strings.HasPrefix(name, ".tmp-") {
			os.Remove(dir + string(os.PathSeparator) + name)
		}
	}
}
