package voice

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Player plays a WAV file and can stop it.
//
// An interface so the package can be tested without an audio device - the same
// reason the retired tts package exposed its speaker. It is deliberately the same
// two methods the Windows player already has, so no adapter is needed.
type Player interface {
	// PlayWAVFile starts playback and returns once it has begun, not when it ends.
	PlayWAVFile(path string) error
	StopAudio()
}

// Voice plays the embedded lines.
//
// The clips are EMBEDDED but played from FILES, which looks redundant and is not:
// winmm's SND_MEMORY form requires the caller's buffer to stay alive for the whole
// asynchronous playback with the garbage collector unaware of it, and a collection
// mid-sentence produces silence or a crash that reproduces about once in a hundred
// runs (see desktop.PlayWAVFile). Materialising the bytes once and playing them by
// path sidesteps that entirely, and the cost is a few hundred kilobytes written on
// first use.
type Voice struct {
	player Player
	// dir is where the clips are materialised. Created on demand.
	dir string
	// user holds the lines the user rendered themselves, or nil when there is no
	// user-voice store (tests, or a build with the feature unavailable). When a
	// line HAS been rendered and chosen there, that clip wins over the embedded one.
	user *Store

	mu sync.Mutex
	// files maps a line to the path written for it, so the write happens once per
	// run rather than once per play.
	files map[Line]string
}

// New returns a Voice that writes its clips into dir.
//
// An empty dir means the system temp directory, which keeps the package usable in
// tests without one.
func New(player Player, dir string) *Voice {
	return NewWithUser(player, dir, nil)
}

// NewWithUser returns a Voice that prefers the user's own renders when a line has
// one, falling back to the embedded clip otherwise.
//
// The precedence lives here rather than in the caller so that every playback path -
// a timer, the reminder scheduler, the panel's audition button - resolves a line to
// audio the same way, and none of them can accidentally play the built-in voice
// after the user has replaced it.
func NewWithUser(player Player, dir string, user *Store) *Voice {
	if dir == "" {
		dir = os.TempDir()
	}
	return &Voice{player: player, dir: dir, user: user, files: map[Line]string{}}
}

// User returns the user-voice store, or nil when there is none.
func (v *Voice) User() *Store { return v.user }

// Play starts a line, replacing anything already playing.
//
// It returns as soon as playback begins. Every failure is reported rather than
// swallowed, because the one caller is a timer callback whose alternative is
// silence - but the caller is expected to log it and carry on, not to surface it:
// a bubble about audio would interrupt the notice it was announcing.
func (v *Voice) Play(line Line) error {
	path, err := v.file(line)
	if err != nil {
		return err
	}
	if v.player == nil {
		return fmt.Errorf("voice: 没有可用的播放器")
	}
	return v.player.PlayWAVFile(path)
}

// PlayEmbedded plays a line's BUILT-IN clip, ignoring the user's own render.
//
// The studio needs this to make "use the built-in voice" an option the user can
// actually hear, rather than a label they have to take on faith - comparing the two
// is the whole point of offering the choice.
func (v *Voice) PlayEmbedded(line Line) error {
	data, ok := Bytes(line)
	if !ok {
		return fmt.Errorf("voice: 没有内嵌音频 %q", line)
	}
	path, err := v.write(line, data)
	if err != nil {
		return err
	}
	if v.player == nil {
		return fmt.Errorf("voice: 没有可用的播放器")
	}
	return v.player.PlayWAVFile(path)
}

// PlayPath plays an arbitrary clip, for auditioning one take.
//
// It exists so the studio can play a take that is NOT the chosen one without that
// distinction leaking into the Player interface, which the app already implements
// for the Windows audio device.
func (v *Voice) PlayPath(path string) error {
	if path == "" {
		return fmt.Errorf("voice: 没有可播放的音频")
	}
	if v.player == nil {
		return fmt.Errorf("voice: 没有可用的播放器")
	}
	return v.player.PlayWAVFile(path)
}

// Stop silences whatever is playing.
//
// Safe to call with nothing playing, and safe on a nil receiver's player: it is
// reached from shutdown paths that run whether or not anything was said.
func (v *Voice) Stop() {
	if v.player != nil {
		v.player.StopAudio()
	}
}

// file returns the on-disk path for a line, writing the embedded clip if needed.
//
// Only the EMBEDDED path is cached here. A user take is resolved on every call,
// because the choice AND the current character can both change while the app is
// running: caching either would leave the pet speaking the previously chosen take,
// or the previous character, until the next restart - precisely the kind of
// "I changed it and nothing happened" bug the studio must not have.
//
// The embedded clip is played ONLY when the store says no character has been
// chosen. It used to be an unconditional fallback whenever a line had no adopted
// take, which is what made the pet keep speaking the built-in 可莉 while the UI
// showed a different character: "this character has not recorded this line" was
// silently answered with somebody else's voice. That case is now an error the
// caller can report.
func (v *Voice) file(line Line) (string, error) {
	if v.user != nil {
		res, err := v.user.ResolveClip(line)
		if err != nil {
			return "", err
		}
		if res.Source == SourceUser {
			return res.Path, nil
		}
		// SourceBuiltIn: no character is chosen, so the embedded clip IS the
		// correct answer rather than a fallback.
	}

	v.mu.Lock()
	if path, ok := v.files[line]; ok {
		v.mu.Unlock()
		return path, nil
	}
	v.mu.Unlock()

	data, ok := Bytes(line)
	if !ok {
		return "", fmt.Errorf("voice: 没有内嵌音频 %q", line)
	}
	path, err := v.write(line, data)
	if err != nil {
		return "", err
	}
	v.mu.Lock()
	v.files[line] = path
	v.mu.Unlock()
	return path, nil
}

// write materialises clip data into the voice directory and returns its path.
//
// The name carries a content hash so a rebuilt binary with a re-rendered clip
// cannot accidentally play the previous version's audio: different bytes, different
// name. That also makes the write idempotent across runs without any bookkeeping.
func (v *Voice) write(line Line, data []byte) (string, error) {
	sum := sha1.Sum(data)
	name := fmt.Sprintf("petpc-%s-%s.wav", line, hex.EncodeToString(sum[:6]))
	path := filepath.Join(v.dir, name)

	// Skip the write when it is already there (a previous run, or another play).
	if fi, err := os.Stat(path); err == nil && fi.Size() == int64(len(data)) {
		return path, nil
	}
	if err := os.MkdirAll(v.dir, 0o755); err != nil {
		return "", fmt.Errorf("voice: 无法创建目录：%w", err)
	}
	// Write to a temporary name and rename, so a concurrent reader (or a crash)
	// never sees a half-written clip.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", fmt.Errorf("voice: 无法写入音频：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("voice: 无法保存音频：%w", err)
	}
	return path, nil
}

// Cleanup removes clips this version does not use.
//
// It runs at startup because a re-rendered line produces a new hashed file name, and
// nothing else would ever collect the previous one - a handful of kilobytes per
// rebuild, accumulating forever in a directory the user cannot easily reason about.
//
// Only files matching this package's own naming are touched, so pointing it at a
// directory with other content cannot delete anything that was not ours.
func Cleanup(dir string) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	current := map[string]bool{}
	for _, line := range AllLines {
		if data, ok := Bytes(line); ok {
			sum := sha1.Sum(data)
			current[fmt.Sprintf("petpc-%s-%s.wav", line, hex.EncodeToString(sum[:6]))] = true
		}
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "petpc-") || !strings.HasSuffix(name, ".wav") {
			continue
		}
		if !current[name] {
			os.Remove(filepath.Join(dir, name))
		}
	}
}
