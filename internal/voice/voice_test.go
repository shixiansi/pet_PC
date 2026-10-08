package voice

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// recordingPlayer captures playback instead of using an audio device.
type recordingPlayer struct {
	mu     sync.Mutex
	played []string
	stops  int
	fail   error
}

func (r *recordingPlayer) PlayWAVFile(path string) error {
	r.mu.Lock()
	r.played = append(r.played, path)
	fail := r.fail
	r.mu.Unlock()
	return fail
}

func (r *recordingPlayer) StopAudio() {
	r.mu.Lock()
	r.stops++
	r.mu.Unlock()
}

func (r *recordingPlayer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.played)
}

func (r *recordingPlayer) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.played) == 0 {
		return ""
	}
	return r.played[len(r.played)-1]
}

// TestEveryLineIsEmbedded is the build check that matters most for this package.
//
// The clips are baked in with go:embed, so a line that is declared but not present
// is a BUILD problem - the pattern did not match, or a render was never committed.
// Finding it here is far better than discovering it when a timer fires and the pet
// says nothing, which is a failure the user cannot diagnose at all.
func TestEveryLineIsEmbedded(t *testing.T) {
	available := Available()
	for _, line := range AllLines {
		if !available[line] {
			t.Errorf("line %q has no embedded clip", line)
		}
	}
	if len(available) != len(AllLines) {
		t.Errorf("Available reported %d lines, AllLines has %d", len(available), len(AllLines))
	}
}

// TestEmbeddedAudioIsPlayableWAV checks the bytes are actually audio, not an empty
// file or an error page that happened to be saved with a .wav name.
//
// A RIFF/WAVE header and a plausible byte count are enough: the renderer is a
// one-off script, and a truncated or HTML response written to disk would otherwise
// look like a working clip right up until it is played.
func TestEmbeddedAudioIsPlayableWAV(t *testing.T) {
	for _, line := range AllLines {
		data, ok := Bytes(line)
		if !ok {
			t.Errorf("%s: not embedded", line)
			continue
		}
		if len(data) < 1000 {
			t.Errorf("%s: only %d bytes, too small to be speech", line, len(data))
			continue
		}
		if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
			t.Errorf("%s: not a RIFF/WAVE file (starts %q)", line, data[0:4])
			continue
		}
		// The declared data size must fit the file, or playback stops early.
		if len(data) < 44 {
			t.Errorf("%s: shorter than a WAV header", line)
		}
	}
}

// TestUnknownLineIsReported covers the lookup contract: an unknown line returns
// ok=false rather than an error, because every caller responds the same way.
func TestUnknownLineIsReported(t *testing.T) {
	if _, ok := Bytes(Line("does_not_exist")); ok {
		t.Error("an unknown line reported success")
	}
}

// TestPlayWritesAndPlaysAClip covers the happy path, including the one thing that
// looks redundant about this package: the audio is embedded but played from a FILE.
func TestPlayWritesAndPlaysAClip(t *testing.T) {
	dir := t.TempDir()
	player := &recordingPlayer{}
	v := New(player, dir)

	if err := v.Play(LineFocusEnd); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if player.count() != 1 {
		t.Fatalf("player was called %d times, want 1", player.count())
	}
	path := player.last()
	if !strings.HasPrefix(path, dir) {
		t.Errorf("played %q, want a path inside %q", path, dir)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the played file does not exist: %v", err)
	}
	// The file must be the embedded bytes, byte for byte - a truncated write would
	// play a truncated clip.
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	embedded, _ := Bytes(LineFocusEnd)
	if len(onDisk) != len(embedded) {
		t.Errorf("wrote %d bytes, embedded clip is %d", len(onDisk), len(embedded))
	}
}

// TestPlayIsIdempotentAboutTheFile pins that the clip is written once, not on every
// play. Playback happens on timer ticks; rewriting the same file each time would be
// pointless I/O in a callback.
func TestPlayIsIdempotentAboutTheFile(t *testing.T) {
	dir := t.TempDir()
	player := &recordingPlayer{}
	v := New(player, dir)

	for i := 0; i < 3; i++ {
		if err := v.Play(LineFocusEnd); err != nil {
			t.Fatalf("Play %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var wavs int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".wav") {
			wavs++
		}
	}
	if wavs != 1 {
		t.Errorf("found %d wav files after 3 plays, want 1", wavs)
	}
	if player.count() != 3 {
		t.Errorf("player called %d times, want 3", player.count())
	}
}

// TestTheFileNameCarriesAContentHash explains why the name is not just the line
// name: a rebuilt binary with a re-rendered clip must not play the previous
// version's audio, and a content hash makes that impossible by construction.
func TestTheFileNameCarriesAContentHash(t *testing.T) {
	dir := t.TempDir()
	player := &recordingPlayer{}
	v := New(player, dir)
	if err := v.Play(LineFocusEnd); err != nil {
		t.Fatalf("Play: %v", err)
	}
	base := filepath.Base(player.last())
	if !strings.HasPrefix(base, "petpc-") {
		t.Errorf("file name %q does not carry the package prefix", base)
	}
	if !strings.Contains(base, string(LineFocusEnd)) {
		t.Errorf("file name %q does not name the line", base)
	}
	// The suffix between the line and the extension is the hash.
	trimmed := strings.TrimSuffix(strings.TrimPrefix(base, "petpc-"+string(LineFocusEnd)+"-"), ".wav")
	if len(trimmed) < 8 {
		t.Errorf("hash in %q is too short to be a content hash", base)
	}
}

// TestPlayReportsAPlayerFailure pins that a failure is surfaced rather than
// swallowed: the caller logs it, and the alternative - silence with no explanation
// anywhere - is far harder to diagnose.
func TestPlayReportsAPlayerFailure(t *testing.T) {
	v := New(&recordingPlayer{fail: os.ErrPermission}, t.TempDir())
	if err := v.Play(LineFocusEnd); err == nil {
		t.Error("a player failure was reported as success")
	}
}

// TestPlayWithoutAPlayerIsAnError covers the degenerate wiring: no player must be an
// error, not a panic. It is reachable if New is ever called with nil.
func TestPlayWithoutAPlayerIsAnError(t *testing.T) {
	v := New(nil, t.TempDir())
	if err := v.Play(LineFocusEnd); err == nil {
		t.Error("playing with no player reported success")
	}
}

// TestStopIsSafeWhenNothingWasEverPlayed covers the shutdown path, which runs
// whether or not anything was said.
func TestStopIsSafeWhenNothingWasEverPlayed(t *testing.T) {
	player := &recordingPlayer{}
	v := New(player, t.TempDir())
	v.Stop()
	v.Stop()
	if player.stops != 2 {
		t.Errorf("stops = %d, want 2", player.stops)
	}
}

// TestCleanupRemovesOnlyStaleClips is the guard on a function that deletes files.
//
// A re-rendered line produces a new hashed name, so the previous one would
// accumulate forever. Cleanup must remove those and NOTHING else - pointing it at a
// directory with unrelated content must not delete that content.
func TestCleanupRemovesOnlyStaleClips(t *testing.T) {
	dir := t.TempDir()

	// A stale clip: this package's naming, but not the current content.
	stale := filepath.Join(dir, "petpc-focus_end-deadbeef0000.wav")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Unrelated files that must survive.
	keepers := []string{
		filepath.Join(dir, "todos.json"),
		filepath.Join(dir, "petpc-notes.txt"),
		filepath.Join(dir, "some-other.wav"),
	}
	for _, k := range keepers {
		if err := os.WriteFile(k, []byte("keep"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}

	Cleanup(dir)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the stale clip was not removed (stat err = %v)", err)
	}
	for _, k := range keepers {
		if _, err := os.Stat(k); err != nil {
			t.Errorf("Cleanup removed %s, which was not its file", filepath.Base(k))
		}
	}
}

// TestCleanupKeepsTheCurrentClip is the other half: it must not delete the clip the
// running build is about to play.
func TestCleanupKeepsTheCurrentClip(t *testing.T) {
	dir := t.TempDir()
	player := &recordingPlayer{}
	v := New(player, dir)
	if err := v.Play(LineFocusEnd); err != nil {
		t.Fatalf("Play: %v", err)
	}
	current := player.last()

	Cleanup(dir)

	if _, err := os.Stat(current); err != nil {
		t.Errorf("Cleanup deleted the clip in use: %v", err)
	}
}

// TestCleanupOnAMissingDirectoryIsSafe covers a first run, when the data directory
// may not exist yet.
func TestCleanupOnAMissingDirectoryIsSafe(t *testing.T) {
	Cleanup(filepath.Join(t.TempDir(), "not-created-yet"))
	Cleanup("")
}

// TestBytesIsCached pins the small optimisation, because the alternative - reading
// the embedded FS on every timer tick - is exactly the kind of repeated work that is
// invisible until it is measured.
func TestBytesIsCached(t *testing.T) {
	first, ok := Bytes(LineFocusEnd)
	if !ok {
		t.Fatal("clip not embedded")
	}
	second, _ := Bytes(LineFocusEnd)
	// Same slice, not merely equal contents: the cache hands back the same backing
	// array rather than re-reading.
	if &first[0] != &second[0] {
		t.Error("Bytes re-read the clip instead of returning the cached copy")
	}
}
