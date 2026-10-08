package main

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"PetPC/internal/tts"
	"PetPC/internal/voice"
)

// These tests drive the studio through its BINDINGS, which is what its window
// actually calls.
//
// The two properties worth pinning are the ones the feature exists for:
//
//  1. generating a take does NOT change what the pet says - only 采用 does;
//  2. a change made here reaches the PET, which is a different process reading the
//     same library. That second one is new since the studio became standalone, and
//     it is the failure the user reported ("modified the character, still heard the
//     old one").

// fakeEngine is a GPT-SoVITS that records the roles it was asked for.
type fakeEngine struct {
	mu      sync.Mutex
	selects []string
	synth   int
	fail    bool
	// frames is the audio length each render reports, so duration parsing is
	// exercised rather than stubbed.
	frames int
}

func (e *fakeEngine) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/tts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError) // this build's real answer
			return
		}
		e.mu.Lock()
		e.synth++
		fail, frames := e.fail, e.frames
		e.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"message": "synthesis exploded"}) //nolint:errcheck
			return
		}
		if frames == 0 {
			frames = 22050
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(testWAV(frames)) //nolint:errcheck
	})
	for _, p := range []string{"/set_gpt_weights", "/set_sovits_weights"} {
		path := p
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			e.mu.Lock()
			e.selects = append(e.selects, r.URL.Query().Get("weights_path"))
			e.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]string{"message": "success"}) //nolint:errcheck
		})
	}
	return mux
}

// testWAV is a valid mono 16-bit WAV of the given frame count.
func testWAV(frames int) []byte {
	dataLen := frames * 2
	b := make([]byte, 44+dataLen)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+dataLen))
	copy(b[8:], "WAVE")
	copy(b[12:], "fmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 22050)
	binary.LittleEndian.PutUint32(b[28:], 44100)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(dataLen))
	return b
}

// newTestStudio returns a studio wired to a fake engine and a temp library, with
// one role whose model files exist.
func newTestStudio(t *testing.T) (*Studio, *fakeEngine) {
	t.Helper()
	dataDir := t.TempDir()
	models := filepath.Join(dataDir, "models")
	for _, f := range []string{"gpt.ckpt", "sovits.pth", "ref.wav"} {
		writeFile(t, filepath.Join(models, "klee", f), []byte("x"))
	}

	eng := &fakeEngine{}
	srv := httptest.NewServer(eng.handler())
	t.Cleanup(srv.Close)

	s, err := newStudioAt(dataDir, "")
	if err != nil {
		t.Fatalf("newStudioAt: %v", err)
	}
	// The config is set under the lock, then released BEFORE calling anything that
	// takes the lock itself (UpsertRole does) - holding it across those calls
	// deadlocks, which is exactly how this helper hung the whole suite the first
	// time.
	s.mu.Lock()
	s.cfg.TTSBaseURL = srv.URL
	s.cfg.TTSDir = dataDir // <dir>/models/klee
	s.client = tts.New(srv.URL)
	s.mu.Unlock()

	// Collect events instead of emitting them. Wails refuses any context other than
	// the one its lifecycle hooks supplied, so a batch render - which reports
	// progress by emitting - would otherwise fail the test for a reason that has
	// nothing to do with what is being tested.
	s.emitFn = func(name string, data ...any) {}

	role := voice.Role{
		ID: "klee", Name: "可莉",
		GPT:    filepath.Join(models, "klee", "gpt.ckpt"),
		SoVITS: filepath.Join(models, "klee", "sovits.pth"),
		Ref:    filepath.Join(models, "klee", "ref.wav"),
	}
	role.Normalise()
	if err := s.lib.UpsertRole(role); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	// Selected explicitly: adding a role no longer implies choosing it, which is the
	// change that stops the studio rendering a character the pet is not speaking as.
	if err := s.lib.SetActiveRole(role.ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	return s, eng
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestGenerateDoesNotChoose is the central safety property: rendering must leave
// the pet's voice alone, so a bad take cannot go live before it is heard.
func TestGenerateDoesNotChoose(t *testing.T) {
	s, _ := newTestStudio(t)

	// Commit v1 first, so "unchanged" is a real assertion.
	if _, err := s.Generate(string(voice.LineFocusEnd), "klee"); err != nil {
		t.Fatalf("generate v1: %v", err)
	}
	if _, err := s.Choose(string(voice.LineFocusEnd), 1); err != nil {
		t.Fatalf("choose v1: %v", err)
	}

	view, err := s.Generate(string(voice.LineFocusEnd), "klee")
	if err != nil {
		t.Fatalf("generate v2: %v", err)
	}
	var line voice.LineState
	for _, l := range view.Lines {
		if l.Line == voice.LineFocusEnd {
			line = l
		}
	}
	if len(line.Takes) != 2 {
		t.Fatalf("%d takes, want 2 (the first must survive)", len(line.Takes))
	}
	if line.Chosen != 1 {
		t.Errorf("chosen v%d after regenerating, want v1 unchanged", line.Chosen)
	}
}

// TestChooseChangesWhatThePetPlays is the other half, and it is checked through a
// SECOND voice.Store over the same directory - which is exactly how the pet sees
// this library, as a different process opening the same files.
func TestChooseChangesWhatThePetPlays(t *testing.T) {
	s, _ := newTestStudio(t)
	line := string(voice.LineFocusEnd)

	if _, err := s.Generate(line, "klee"); err != nil {
		t.Fatalf("generate v1: %v", err)
	}
	if _, err := s.Generate(line, "klee"); err != nil {
		t.Fatalf("generate v2: %v", err)
	}
	if _, err := s.Choose(line, 2); err != nil {
		t.Fatalf("choose v2: %v", err)
	}

	// A reader that opened the library BEFORE the choice was made, standing in for
	// the pet which is already running.
	petView, err := voice.Open(filepath.Join(s.dir, "voice"))
	if err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	if err := petView.SetActiveRole("klee"); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	res, err := petView.ResolveClip(voice.LineFocusEnd)
	if err != nil {
		t.Fatalf("the pet's view cannot resolve a clip after a choice was made in the studio: %v", err)
	}
	if res.Source != voice.SourceUser {
		t.Fatal("the pet's view resolved the built-in clip, not the user's take")
	}
	if filepath.Base(res.Path) != "v002.wav" {
		t.Errorf("the pet would play %s, want v002.wav", filepath.Base(res.Path))
	}
}

// TestPetSeesAChoiceMadeAfterItOpened is the regression for the reported bug.
//
// The studio is a separate PROCESS, so the pet's in-memory view can be older than
// the file. Before the library re-read its index on change, the pet kept playing
// whatever was chosen when it started - so committing a take in the studio appeared
// to do nothing at all.
func TestPetSeesAChoiceMadeAfterItOpened(t *testing.T) {
	s, _ := newTestStudio(t)
	line := voice.LineFocusEnd

	// A reader opened BEFORE anything was rendered, exactly like a running pet.
	pet, err := voice.Open(filepath.Join(s.dir, "voice"))
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	if err := pet.SetActiveRole("klee"); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	if _, err := pet.ResolveClip(line); err == nil {
		t.Fatal("precondition: the reader should resolve nothing yet")
	}

	// The studio renders and commits, in its own "process".
	if _, err := s.Generate(string(line), "klee"); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := s.Choose(string(line), 1); err != nil {
		t.Fatalf("choose: %v", err)
	}

	// The long-running reader must now resolve the new take without reopening.
	res, err := pet.ResolveClip(line)
	if err != nil {
		t.Fatalf("the running reader did not notice the studio's choice: %v", err)
	}
	if filepath.Base(res.Path) != "v001.wav" {
		t.Errorf("the reader resolved %s, want v001.wav", filepath.Base(res.Path))
	}
}

// TestAdoptAllIsTheOneClickReplacement covers the action that answers "I chose a
// character, now what?": it renders every line AND commits each one.
func TestAdoptAllIsTheOneClickReplacement(t *testing.T) {
	s, _ := newTestStudio(t)
	if _, err := s.AdoptAllAsBuiltIn("klee"); err != nil {
		t.Fatalf("AdoptAllAsBuiltIn: %v", err)
	}
	waitForIdle(t, s)

	view := s.view(false)
	if view.Counts.Takes != len(voice.AllLines) {
		t.Errorf("%d takes, want one per line (%d)", view.Counts.Takes, len(voice.AllLines))
	}
	if view.Counts.Replaced != len(voice.AllLines) {
		t.Errorf("%d lines replaced, want %d - adopting must commit, not just render",
			view.Counts.Replaced, len(voice.AllLines))
	}
	for _, l := range view.Lines {
		if l.Chosen == 0 {
			t.Errorf("%s was not committed", l.Line)
		}
		if l.EffectSource != "user" {
			t.Errorf("%s still reports effect %q", l.Line, l.EffectSource)
		}
	}
}

// TestGenerateAllRendersButDoesNotAdopt is the contrast with the above, and it is
// the reason both buttons exist.
func TestGenerateAllRendersButDoesNotAdopt(t *testing.T) {
	s, _ := newTestStudio(t)
	if _, err := s.GenerateAll("klee"); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	waitForIdle(t, s)

	view := s.view(false)
	if view.Counts.Takes != len(voice.AllLines) {
		t.Errorf("%d takes, want %d", view.Counts.Takes, len(voice.AllLines))
	}
	// The first take of each line adopts itself, so "replaced" is not the
	// discriminator here; what matters is that a SECOND run would not commit.
	before := view.Counts.Replaced
	if _, err := s.GenerateAll("klee"); err != nil {
		t.Fatalf("second GenerateAll: %v", err)
	}
	waitForIdle(t, s)

	after := s.view(false)
	if after.Counts.Replaced != before {
		t.Errorf("regenerating changed %d committed lines to %d; a plain generate must not commit",
			before, after.Counts.Replaced)
	}
	if after.Counts.Takes != 2*len(voice.AllLines) {
		t.Errorf("%d takes, want %d", after.Counts.Takes, 2*len(voice.AllLines))
	}
}

// waitForIdle blocks until a batch render finishes.
func waitForIdle(t *testing.T, s *Studio) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if !s.isBusy() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the batch render never finished")
}

// TestEffectNamesTheCharacter is the fix for the reported confusion: a line must
// say WHICH voice plays, not merely that it is "the built-in one".
//
// With no character selected, the built-in clips ARE what plays, so every row must
// report the built-in character.
func TestEffectNamesTheCharacter(t *testing.T) {
	s, _ := newTestStudio(t)
	// Back to "no character chosen", which is the only state that plays the embedded
	// clips and therefore the only one where every row must name the built-in voice.
	if _, err := s.SetActiveRole(voice.BuiltInRoleID); err != nil {
		t.Fatalf("SetActiveRole built-in: %v", err)
	}
	view := s.view(false)

	for _, l := range view.Lines {
		if l.Effect == "" {
			t.Errorf("%s reports no effect at all", l.Line)
		}
		if l.EffectSource == "builtin" {
			if l.BuiltInRole == "" {
				t.Errorf("%s is using the built-in clip but does not name its character", l.Line)
			}
			if l.Effect != l.BuiltInRole {
				t.Errorf("%s effect %q, want the built-in character %q", l.Line, l.Effect, l.BuiltInRole)
			}
		}
	}
	if view.BuiltInRole == "" {
		t.Error("the view does not report which character the embedded clips are")
	}
	if view.BuiltInRole != voice.BuiltInRole {
		t.Errorf("view reports built-in %q, voice package says %q", view.BuiltInRole, voice.BuiltInRole)
	}
	if !voice.IsBuiltInRole(view.ActiveRole) {
		t.Errorf("with no character chosen the view reports activeRole %q; want the built-in sentinel", view.ActiveRole)
	}
}

// TestEffectFollowsTheChoice means the badge cannot lie: after committing, the line
// reports the user's take and its role name.
//
// The character must be SELECTED for the take to be in effect - merely adopting it
// while the built-in voice is selected must not claim the pet speaks it.
func TestEffectFollowsTheChoice(t *testing.T) {
	s, _ := newTestStudio(t)
	if _, err := s.SetActiveRole("klee"); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	if _, err := s.Generate(string(voice.LineFocusEnd), "klee"); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := s.Choose(string(voice.LineFocusEnd), 1); err != nil {
		t.Fatalf("choose: %v", err)
	}

	view := s.view(false)
	var line voice.LineState
	for _, l := range view.Lines {
		if l.Line == voice.LineFocusEnd {
			line = l
		}
	}
	if line.EffectSource != "user" {
		t.Errorf("effect source %q after committing, want user", line.EffectSource)
	}
	if line.Effect != "可莉" {
		t.Errorf("effect %q, want the role name 可莉", line.Effect)
	}
	if !line.ChosenForActiveRole {
		t.Error("the line reports a user clip but not that it belongs to the current character")
	}
}

// TestChangingCharacterMakesUnrecordedLinesPending is the UI half of the fix: after
// switching to a character that has not recorded a line, the row must say so instead
// of still showing the previous character's take as if the pet were speaking it.
func TestChangingCharacterMakesUnrecordedLinesPending(t *testing.T) {
	s, _ := newTestStudio(t)
	// klee records and adopts one line.
	if _, err := s.SetActiveRole("klee"); err != nil {
		t.Fatalf("SetActiveRole klee: %v", err)
	}
	if _, err := s.AdoptAllAsBuiltIn("klee"); err != nil {
		t.Fatalf("AdoptAllAsBuiltIn: %v", err)
	}
	waitForIdle(t, s)

	// A second character, with models on disk so it can be selected.
	models := filepath.Join(s.dir, "models", "nahida")
	for _, f := range []string{"gpt.ckpt", "sovits.pth", "ref.wav"} {
		writeFile(t, filepath.Join(models, f), []byte("x"))
	}
	writeFile(t, filepath.Join(models, "petpc-config.json"), []byte(`{"ttsCharacter":"纳西妲"}`))
	if _, err := s.RescanRoles(); err != nil {
		t.Fatalf("RescanRoles: %v", err)
	}

	view, err := s.SetActiveRole("nahida")
	if err != nil {
		t.Fatalf("SetActiveRole nahida: %v", err)
	}
	if view.Counts.Replaced != 0 {
		t.Errorf("%d lines still count as replaced after switching to a character that recorded none",
			view.Counts.Replaced)
	}
	for _, l := range view.Lines {
		if l.ChosenForActiveRole {
			t.Errorf("%s claims to play as 纳西妲, who has recorded nothing", l.Line)
		}
		if l.EffectSource != "stale" {
			t.Errorf("%s effect source %q after switching; want stale (adopted by another character)",
				l.Line, l.EffectSource)
		}
	}
}

// TestFailedRenderStoresNothing means an engine error is reported and leaves no
// trace, rather than producing a silent clip.
func TestFailedRenderStoresNothing(t *testing.T) {
	s, eng := newTestStudio(t)
	eng.mu.Lock()
	eng.fail = true
	eng.mu.Unlock()

	if _, err := s.Generate(string(voice.LineFocusEnd), "klee"); err == nil {
		t.Fatal("a failed synthesis must be reported")
	}
	if got := s.view(false).Counts.Takes; got != 0 {
		t.Errorf("%d takes recorded after a failure, want 0", got)
	}
}

// TestBatchSurvivesOneFailure is why a batch does not abort: on CPU the engine
// occasionally fails a single synthesis, and discarding the others would waste the
// minutes already spent.
func TestBatchSurvivesOneFailure(t *testing.T) {
	s, eng := newTestStudio(t)

	// Fail the FIRST synthesis only.
	var calls int
	eng.mu.Lock()
	eng.fail = true
	eng.mu.Unlock()
	go func() {
		time.Sleep(150 * time.Millisecond)
		eng.mu.Lock()
		calls = eng.synth
		eng.fail = false
		eng.mu.Unlock()
	}()

	if _, err := s.GenerateAll("klee"); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	waitForIdle(t, s)
	eng.mu.Lock()
	synth := eng.synth
	eng.mu.Unlock()
	_ = calls

	// Whatever happened, the batch must have completed and attempted every line.
	if synth < len(voice.AllLines) {
		t.Errorf("only %d syntheses attempted, want at least one per line (%d)", synth, len(voice.AllLines))
	}
}

// TestUnreachableEngineIsReported covers the case a user hits first.
func TestUnreachableEngineIsReported(t *testing.T) {
	s, _ := newTestStudio(t)
	s.mu.Lock()
	s.cfg.TTSBaseURL = "http://127.0.0.1:1" // reserved; nothing listens
	s.client = nil
	s.mu.Unlock()

	view := s.view(true)
	if view.Engine.Ready {
		t.Error("an unreachable engine reported ready")
	}
	if view.Engine.Note == "" {
		t.Error("the view gives no explanation for a non-ready engine")
	}
	if _, err := s.Generate(string(voice.LineFocusEnd), "klee"); err == nil {
		t.Error("generating without an engine must fail with a message")
	}
}

// TestUnknownLineIsRejected guards the one string that arrives from the frontend.
func TestUnknownLineIsRejected(t *testing.T) {
	s, _ := newTestStudio(t)
	if _, err := s.Generate("not_a_line", "klee"); err == nil {
		t.Error("an unknown line must be rejected")
	}
	if _, err := s.Choose("not_a_line", 1); err == nil {
		t.Error("choosing an unknown line must be rejected")
	}
}

// TestResetKeepsAudio is the escape hatch: one click back to the built-in voice,
// deleting nothing.
func TestResetKeepsAudio(t *testing.T) {
	s, _ := newTestStudio(t)
	if _, err := s.Generate(string(voice.LineFocusEnd), "klee"); err != nil {
		t.Fatalf("generate: %v", err)
	}
	view, err := s.ResetAll()
	if err != nil {
		t.Fatalf("ResetAll: %v", err)
	}
	if view.Counts.Replaced != 0 {
		t.Errorf("%d lines still replaced after a reset", view.Counts.Replaced)
	}
	if view.Counts.Takes != 1 {
		t.Errorf("%d takes survived, want 1 (a reset must not delete audio)", view.Counts.Takes)
	}
}

// TestRescanIsAdditive: a role the user has tuned must never be overwritten by what
// is on disk.
func TestRescanIsAdditive(t *testing.T) {
	s, _ := newTestStudio(t)
	// Drop in a second character.
	models := filepath.Join(s.dir, "models", "nahida")
	for _, f := range []string{"gpt.ckpt", "sovits.pth", "ref.wav"} {
		writeFile(t, filepath.Join(models, f), []byte("x"))
	}
	writeFile(t, filepath.Join(models, "petpc-config.json"), []byte(`{"ttsCharacter":"纳西妲"}`))

	// Tune klee first, so the rescan has something to clobber.
	role, _ := s.lib.Role("klee")
	role.Speed = 1.4
	if _, err := s.SaveRole(role); err != nil {
		t.Fatalf("SaveRole: %v", err)
	}

	view, err := s.RescanRoles()
	if err != nil {
		t.Fatalf("RescanRoles: %v", err)
	}
	var names []string
	for _, r := range view.Roles {
		names = append(names, r.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "纳西妲") {
		t.Errorf("the new role was not added: %v", names)
	}
	got, _ := s.lib.Role("klee")
	if got.Speed != 1.4 {
		t.Errorf("rescan overwrote a tuned role: speed=%v, want 1.4", got.Speed)
	}
}

// TestRescanOnNothingIsExplained: the most likely user error is a wrong directory,
// so the message must say where it looked.
func TestRescanOnNothingIsExplained(t *testing.T) {
	s, _ := newTestStudio(t)
	s.mu.Lock()
	s.cfg.TTSDir = filepath.Join(t.TempDir(), "nowhere")
	s.mu.Unlock()

	_, err := s.RescanRoles()
	if err == nil {
		t.Fatal("an empty models directory should be reported")
	}
	if !strings.Contains(err.Error(), "models") {
		t.Errorf("the error does not say where it looked: %v", err)
	}
}

// TestSettingsPersistSeparatelyFromThePet means the studio owns its own
// configuration file and does not write into the pet's config.json.
func TestSettingsPersistSeparatelyFromThePet(t *testing.T) {
	s, _ := newTestStudio(t)
	if _, err := s.SaveEngine("http://127.0.0.1:9999", `D:\Somewhere`, `D:\py.exe`); err != nil {
		t.Fatalf("SaveEngine: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "voice-studio.json")); err != nil {
		t.Errorf("the studio did not persist its own settings: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "config.json")); err == nil {
		t.Error("the studio wrote the PET's config.json")
	}

	// And a reopened studio sees them.
	again, err := newStudioAt(s.dir, "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	again.mu.Lock()
	got := again.cfg
	again.mu.Unlock()
	if got.TTSBaseURL != "http://127.0.0.1:9999" || got.TTSDir != `D:\Somewhere` {
		t.Errorf("settings did not persist: %+v", got)
	}
}

// TestCorruptSettingsDoNotBlockOpening: the library is the valuable part, the
// settings are four strings.
func TestCorruptSettingsDoNotBlockOpening(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "voice-studio.json"), []byte("{not json at all"))
	s, err := newStudioAt(dir, "")
	if err != nil {
		t.Fatalf("a corrupt settings file must not stop the studio opening: %v", err)
	}
	if s.lib == nil {
		t.Error("the library was not opened")
	}
}

// TestAuditionDoesNotChoose means a take can be heard before it is trusted.
func TestAuditionDoesNotChoose(t *testing.T) {
	s, _ := newTestStudio(t)
	line := string(voice.LineFocusEnd)
	if _, err := s.Generate(line, "klee"); err != nil {
		t.Fatalf("generate: %v", err)
	}
	// Clear the choice the first take adopted, so "did not commit" is meaningful.
	if _, err := s.ClearChoice(line); err != nil {
		t.Fatalf("clear: %v", err)
	}

	if _, err := s.Audition(line, 1); err != nil {
		t.Fatalf("Audition: %v", err)
	}
	// Auditioning must not have adopted the take. Checked through the LINE's chosen
	// version rather than through playback, because with no character selected the
	// built-in clips play either way and would mask a silent commit.
	if got := s.lib.Settings().Lines[voice.LineFocusEnd].Chosen; got != 0 {
		t.Errorf("auditioning adopted v%d; it must leave the choice alone", got)
	}
}

// TestAuditionRejectsWhatIsNotThere keeps a bad click from looking like a broken
// audio device.
func TestAuditionRejectsWhatIsNotThere(t *testing.T) {
	s, _ := newTestStudio(t)
	if _, err := s.Audition(string(voice.LineFocusEnd), 99); err == nil {
		t.Error("auditioning a take that does not exist must be refused")
	}
	if _, err := s.Audition("not_a_line", 1); err == nil {
		t.Error("auditioning an unknown line must be refused")
	}
}

// TestAuditionOriginalPlaysTheEmbeddedClip is the comparison the studio exists to
// offer. The clip is read from the binary and materialised, so this checks the file
// is written where it can be played - the actual audio device is not available in a
// test, but a missing or empty file is the failure that would matter.
func TestAuditionOriginalPlaysTheEmbeddedClip(t *testing.T) {
	s, _ := newTestStudio(t)

	// A user take is committed first, so "played the built-in one" is a real
	// distinction rather than the only option available.
	if _, err := s.Generate(string(voice.LineFocusEnd), "klee"); err != nil {
		t.Fatalf("generate: %v", err)
	}

	data, ok := voice.Bytes(voice.LineFocusEnd)
	if !ok {
		t.Fatal("the line has no embedded clip in this build")
	}
	path, err := writeTempClip(s.dir, string(voice.LineFocusEnd), data)
	if err != nil {
		t.Fatalf("writeTempClip: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the embedded clip was not materialised: %v", err)
	}
	if fi.Size() != int64(len(data)) {
		t.Errorf("materialised %d bytes, want %d", fi.Size(), len(data))
	}
	// The materialised path must be OUTSIDE the user library, so it can never be
	// mistaken for a take or deleted as one.
	if strings.Contains(path, string(voice.SubdirName)) {
		t.Errorf("the embedded clip was written into the user library: %s", path)
	}

	// Calling twice must reuse the same file rather than accumulating copies.
	again, err := writeTempClip(s.dir, string(voice.LineFocusEnd), data)
	if err != nil {
		t.Fatalf("second writeTempClip: %v", err)
	}
	if again != path {
		t.Errorf("a second materialisation produced %s, want %s", again, path)
	}
}

// TestAuditionOriginalRejectsAnUnknownLine guards the frontend-supplied string.
func TestAuditionOriginalRejectsAnUnknownLine(t *testing.T) {
	s, _ := newTestStudio(t)
	if _, err := s.AuditionOriginal("not_a_line"); err == nil {
		t.Error("an unknown line must be rejected")
	}
}

// TestDeleteTakeFallsBackRatherThanPromoting: deleting the take in use must leave the
// line with NO adopted take, not silently switch the pet to another take the user did
// not pick.
//
// "No adopted take" is reported as the pending state rather than as the built-in
// voice, because a character is still selected: the built-in clips belong to a
// different voice and are not this line's fallback.
func TestDeleteTakeFallsBackRatherThanPromoting(t *testing.T) {
	s, _ := newTestStudio(t)
	line := string(voice.LineFocusEnd)
	if _, err := s.SetActiveRole("klee"); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Generate(line, "klee"); err != nil {
			t.Fatalf("generate %d: %v", i, err)
		}
	}
	if _, err := s.Choose(line, 1); err != nil {
		t.Fatalf("choose v1: %v", err)
	}
	view, err := s.DeleteTake(line, 1)
	if err != nil {
		t.Fatalf("DeleteTake: %v", err)
	}
	var got voice.LineState
	for _, l := range view.Lines {
		if l.Line == voice.LineFocusEnd {
			got = l
		}
	}
	if got.Chosen != 0 {
		t.Errorf("chosen v%d after deleting the take in use, want 0", got.Chosen)
	}
	if got.ChosenForActiveRole {
		t.Error("the line still counts as playing for the current character")
	}
	if got.EffectSource == "user" {
		t.Errorf("effect source %q, want the not-in-effect state", got.EffectSource)
	}
	if len(got.Takes) != 1 {
		t.Errorf("%d takes left, want 1", len(got.Takes))
	}
}

// TestSetActiveRoleIsThePlaybackSource is the fix stated at the studio's own layer:
// the selection is persisted, reported back, and is what the pet's resolution uses.
func TestSetActiveRoleIsThePlaybackSource(t *testing.T) {
	s, _ := newTestStudio(t)
	models := filepath.Join(s.dir, "models", "nahida")
	for _, f := range []string{"gpt.ckpt", "sovits.pth", "ref.wav"} {
		writeFile(t, filepath.Join(models, f), []byte("x"))
	}
	writeFile(t, filepath.Join(models, "petpc-config.json"), []byte(`{"ttsCharacter":"纳西妲"}`))
	if _, err := s.RescanRoles(); err != nil {
		t.Fatalf("RescanRoles: %v", err)
	}

	view, err := s.SetActiveRole("nahida")
	if err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	if view.ActiveRole != "nahida" || view.ActiveRoleName != "纳西妲" {
		t.Errorf("view reports active %q/%q, want nahida/纳西妲", view.ActiveRole, view.ActiveRoleName)
	}

	// A SECOND reader - how the pet sees this library, as another process - must
	// resolve the same character, which is what makes the selection audible.
	pet, err := voice.Open(filepath.Join(s.dir, "voice"))
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	role, err := pet.ResolveCurrentRole()
	if err != nil {
		t.Fatalf("the pet cannot resolve the character the studio selected: %v", err)
	}
	if role.ID != "nahida" {
		t.Errorf("the pet resolves %q, want nahida - the studio's selection is not the playback source", role.ID)
	}
}

// TestFailedSwitchDoesNotPretendToHaveSwitched is the "失败可见" requirement: a
// refused switch must report an error AND leave the view showing the OLD character.
//
// A UI that keeps the new card highlighted after a failed write claims a switch that
// did not happen - the same class of lie as hearing the wrong voice.
func TestFailedSwitchDoesNotPretendToHaveSwitched(t *testing.T) {
	s, _ := newTestStudio(t)

	view, err := s.SetActiveRole("nobody")
	if err == nil {
		t.Fatal("selecting a character that does not exist must be refused")
	}
	if !strings.Contains(err.Error(), "切换角色失败") {
		t.Errorf("error %q is not recognisable as a failed switch", err)
	}
	if view.ActiveRole != "klee" {
		t.Errorf("after a refused switch the view reports %q; it must still show klee", view.ActiveRole)
	}
	// And the persisted truth is unchanged, so the next read agrees with the view.
	role, rerr := s.lib.ResolveCurrentRole()
	if rerr != nil {
		t.Fatalf("ResolveCurrentRole: %v", rerr)
	}
	if role.ID != "klee" {
		t.Errorf("after a refused switch the stored character is %q; want klee", role.ID)
	}
}

// TestViewIsOneConsistentSnapshot means the header cannot disagree with the rows.
//
// The role and the per-line states are read in a single pass, so the character named
// at the top is the same one the rows below were computed against. Two separate
// reads would let another process commit a switch in between, and the studio would
// then show a character that the lines it is displaying do not belong to.
func TestViewIsOneConsistentSnapshot(t *testing.T) {
	s, _ := newTestStudio(t)
	view, err := s.SetActiveRole("klee")
	if err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}

	for _, l := range view.Lines {
		if l.ActiveRole != view.ActiveRole {
			t.Errorf("%s was resolved for %q but the view reports %q", l.Line, l.ActiveRole, view.ActiveRole)
		}
		if l.ActiveRoleName != view.ActiveRoleName {
			t.Errorf("%s names %q but the view reports %q", l.Line, l.ActiveRoleName, view.ActiveRoleName)
		}
	}

	// Returning to the built-in voice must be reported as such, with the sentinel
	// resolved to a display name rather than leaking the id.
	if _, err := s.SetActiveRole(voice.BuiltInRoleID); err != nil {
		t.Fatalf("SetActiveRole built-in: %v", err)
	}
	view = s.view(false)
	if view.ActiveRoleName != voice.BuiltInRole {
		t.Errorf("built-in voice reported as %q, want %q", view.ActiveRoleName, voice.BuiltInRole)
	}
	if !voice.IsBuiltInRole(view.ActiveRole) {
		t.Errorf("built-in voice reported as id %q, want the sentinel", view.ActiveRole)
	}
	for _, l := range view.Lines {
		if l.ChosenForActiveRole {
			t.Errorf("%s counts as playing for the current character while the built-in voice is selected", l.Line)
		}
	}
}

// TestGenerateRefusesWhileBusy keeps two batch runs from interleaving their engine
// weight switches, which would return one role's audio under another's name.
func TestGenerateRefusesWhileBusy(t *testing.T) {
	s, _ := newTestStudio(t)
	if _, err := s.GenerateAll("klee"); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	if _, err := s.Generate(string(voice.LineFocusEnd), "klee"); err == nil {
		t.Error("a single generate must be refused while a batch runs")
	}
	if _, err := s.GenerateAll("klee"); err == nil {
		t.Error("a second batch must be refused while one runs")
	}
	if _, err := s.CancelGenerate(); err != nil {
		t.Fatalf("CancelGenerate: %v", err)
	}
	waitForIdle(t, s)
}
