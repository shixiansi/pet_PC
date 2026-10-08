package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"PetPC/internal/tts"
	"PetPC/internal/voice"
)

// These tests exercise the real local GPT-SoVITS engine. They are skipped unless
// PETPC_TTS_E2E=1, because they need a running server and take minutes of CPU -
// exactly the kind of test that must never gate a normal build, but that is
// invaluable when the engine or its API changes.
//
//	PETPC_TTS_E2E=1 PETPC_TTS_DIR=D:\GPT-SoVITS go test -run TestE2E -v .
//
// What they prove that the unit tests cannot: that the parameters the studio sends
// are the ones this engine accepts, and that a rendered take is genuinely playable
// audio of the expected shape.

func e2eConfig(t *testing.T) (string, string) {
	t.Helper()
	if os.Getenv("PETPC_TTS_E2E") != "1" {
		t.Skip("set PETPC_TTS_E2E=1 to run the engine end-to-end tests")
	}
	dir := os.Getenv("PETPC_TTS_DIR")
	if dir == "" {
		dir = `D:\GPT-SoVITS`
	}
	base := os.Getenv("PETPC_TTS_URL")
	if base == "" {
		base = tts.DefaultBaseURL
	}
	return dir, base
}

// sharedEngine is an engine this test binary started, so the suite is
// self-contained: running it does not require the user to have started a server by
// hand, which is what made these tests fail for the wrong reason before.
var sharedEngine struct {
	once sync.Once
	// proc is the engine WE started, and is nil when one was already running. It is
	// only used for cleanup, because a server we did not start must not be killed.
	proc *tts.Process
	// available records whether an engine can be reached at all, which is the
	// question the tests actually care about. Conflating it with proc was a bug:
	// an already-running server left proc nil and every dependent test skipped.
	available bool
}

// ensureEngine makes sure something is listening on base, starting an engine if
// necessary and stopping it when the test binary exits.
//
// One engine serves every test in the run because loading the models takes ~40s and
// several hundred megabytes; starting one per test would make the suite unusable.
func ensureEngine(t *testing.T, ttsDir, base string) {
	t.Helper()
	sharedEngine.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tts.New(base).Health(ctx); err == nil {
			// Somebody else's engine - the user's, or a previous run's. Use it and
			// leave it alone.
			sharedEngine.available = true
			return
		}
		// A temp data dir, so the shared engine's config and log do not land in the
		// working directory: StartEngine writes both into the dir it is given.
		dataDir, derr := os.MkdirTemp("", "petpc-e2e-engine-")
		if derr != nil {
			t.Logf("could not create a data dir for the shared engine: %v", derr)
			return
		}
		proc, err := tts.StartEngine(tts.EngineConfig{
			BaseURL: base, Dir: ttsDir, DataDir: dataDir,
		})
		if err != nil {
			t.Logf("could not start a shared engine: %v", err)
			return
		}
		// Wait for it to be able to synthesise, not merely to accept connections.
		if err := proc.Ready(tts.New(base), 4*time.Minute, nil); err != nil {
			t.Logf("the shared engine never became ready: %v\nlog:\n%s", err, proc.TailLog(3000))
			proc.Stop()
			return
		}
		sharedEngine.proc = proc
		sharedEngine.available = true
	})
	if !sharedEngine.available {
		// No engine available: skip rather than fail, because "the machine has no
		// engine" is a missing prerequisite, not a defect in the code under test.
		t.Skip("no GPT-SoVITS engine is available; start one or let the launcher start it")
	}
}

// TestMain gives the suite one shared engine and cleans it up afterwards.
//
// Without this the engine-dependent tests were order-sensitive: they only passed if
// something happened to be listening, which is exactly the kind of test that fails
// for an unrelated reason and trains people to ignore it.
func TestMain(m *testing.M) {
	code := m.Run()
	if sharedEngine.proc != nil {
		sharedEngine.proc.Stop()
	}
	os.Exit(code)
}

// e2eRole builds a role from a real model directory.
func e2eRole(t *testing.T, ttsDir, id string) voice.Role {
	t.Helper()
	dir := filepath.Join(ttsDir, "models", id)
	r := voice.Role{
		ID: id, Name: id,
		GPT:    filepath.Join(dir, "gpt.ckpt"),
		SoVITS: filepath.Join(dir, "sovits.pth"),
		Ref:    filepath.Join(dir, "ref.wav"),
	}
	r.Normalise()
	for _, p := range []string{r.GPT, r.SoVITS, r.Ref} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("model %s is not present: %v", id, err)
		}
	}
	return r
}

// TestE2EHealth checks the running engine answers.
func TestE2EHealth(t *testing.T) {
	ttsDir, base := e2eConfig(t)
	ensureEngine(t, ttsDir, base)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tts.New(base).Health(ctx); err != nil {
		t.Fatalf("the engine is not reachable at %s: %v", base, err)
	}
}

// TestE2ERenderEveryLine is the test that actually matters: it renders the real
// lines with a real character and stores them as takes, proving the whole path -
// role selection, synthesis, storage, choice, and playback resolution - works
// against the engine rather than against a fake.
func TestE2ERenderEveryLine(t *testing.T) {
	ttsDir, base := e2eConfig(t)
	ensureEngine(t, ttsDir, base)
	role := e2eRole(t, ttsDir, "klee")

	// A throwaway library, so the test cannot disturb the user's own renders.
	store, err := voice.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	client := tts.New(base)

	// Switch to the role once, then render every line without re-selecting: the
	// batch path in the app does the same, and it is what keeps a full render to
	// one weight load instead of five.
	ctx := context.Background()
	if err := client.SelectRole(ctx, role.GPT, role.SoVITS); err != nil {
		t.Fatalf("SelectRole: %v", err)
	}

	for _, line := range voice.AllLines {
		text := voice.Text(line)
		t.Run(string(line), func(t *testing.T) {
			start := time.Now()
			audio, err := client.Synthesize(ctx, tts.Request{
				Text:              text,
				TextLang:          role.Lang,
				RefAudioPath:      role.Ref,
				PromptText:        role.PromptText,
				PromptLang:        role.PromptLang,
				SpeedFactor:       role.Speed,
				Temperature:       role.Temperature,
				TopK:              role.TopK,
				TopP:              role.TopP,
				RepetitionPenalty: role.RepetitionPenalty,
				Seed:              role.Seed,
			})
			if err != nil {
				t.Fatalf("Synthesize %q: %v", text, err)
			}
			take, err := store.AddTake(line, audio, role, text)
			if err != nil {
				t.Fatalf("AddTake: %v", err)
			}
			t.Logf("v%d  %d bytes  %dms  in %s",
				take.Version, take.Bytes, take.DurationMS, time.Since(start).Round(time.Millisecond))

			if take.DurationMS < 1000 {
				t.Errorf("clip is only %dms; a whole sentence should be longer", take.DurationMS)
			}
			if take.Bytes < 20000 {
				t.Errorf("clip is only %d bytes, which is too small to be speech", take.Bytes)
			}
		})
	}

	// Every line must now resolve to its user take, which is the guarantee that
	// makes the studio's output actually heard. A character has to be selected for
	// this to mean anything: with none selected, the built-in clips are the correct
	// answer rather than a fallback.
	if err := store.SetActiveRole(role.ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	for _, line := range voice.AllLines {
		res, err := store.ResolveClip(line)
		if err != nil {
			t.Errorf("%s does not resolve after rendering: %v", line, err)
			continue
		}
		if res.Source != voice.SourceUser {
			t.Errorf("%s resolved to %q, want the user's own render", line, res.Source)
		}
	}
	states := store.LineStates(voice.Available())
	for _, st := range states {
		if st.Chosen == 0 {
			t.Errorf("%s reports no chosen take", st.Line)
		}
		if len(st.Takes) != 1 {
			t.Errorf("%s has %d takes, want 1", st.Line, len(st.Takes))
		}
	}
}

// TestE2ERegenerateThenChoose drives the actual fix-the-bad-line workflow against
// the engine: render v1, render v2, confirm v1 is still on disk and still chosen
// (so nothing changed behind the user's back), then choose v2 and confirm playback
// follows.
func TestE2ERegenerateThenChoose(t *testing.T) {
	ttsDir, base := e2eConfig(t)
	ensureEngine(t, ttsDir, base)
	role := e2eRole(t, ttsDir, "klee")

	store, err := voice.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	client := tts.New(base)
	ctx := context.Background()
	line := voice.LineFocusEnd
	text := voice.Text(line)

	render := func() voice.Take {
		t.Helper()
		audio, err := client.Render(ctx, tts.RoleRef{GPT: role.GPT, SoVITS: role.SoVITS}, tts.Request{
			Text: text, TextLang: role.Lang, RefAudioPath: role.Ref,
			PromptText: role.PromptText, PromptLang: role.PromptLang,
			SpeedFactor: role.Speed, Temperature: role.Temperature,
			TopK: role.TopK, TopP: role.TopP,
			RepetitionPenalty: role.RepetitionPenalty, Seed: role.Seed,
		})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		take, err := store.AddTake(line, audio, role, text)
		if err != nil {
			t.Fatalf("AddTake: %v", err)
		}
		return take
	}

	v1 := render()
	v2 := render()
	if v2.Version != 2 {
		t.Fatalf("second render is v%d, want v2", v2.Version)
	}

	// Rendering a new take must NOT silently change the pet's voice.
	if got := store.Settings().Lines[line].Chosen; got != v1.Version {
		t.Errorf("chosen v%d after regenerating, want it unchanged at v1", got)
	}
	if _, err := os.Stat(store.TakeFilePath(line, v1.File)); err != nil {
		t.Errorf("v1's audio is gone after a regeneration: %v", err)
	}

	// Committing v2 must change it, immediately.
	if err := store.Choose(line, v2.Version); err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if err := store.SetActiveRole(role.ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	res, err := store.ResolveClip(line)
	if err != nil {
		t.Fatalf("ResolveClip after Choose: %v", err)
	}
	if filepath.Base(res.Path) != v2.File {
		t.Errorf("resolved %s, want %s", filepath.Base(res.Path), v2.File)
	}
}

// TestE2EDiscoveryFindsTheRealModels checks the on-disk layout assumption against
// the actual installation, which is how the panel offers characters with no
// configuration at all.
func TestE2EDiscoveryFindsTheRealModels(t *testing.T) {
	ttsDir, _ := e2eConfig(t)
	roles := voice.DiscoverRoles(ttsDir)
	if len(roles) == 0 {
		t.Fatalf("no roles discovered under %s\\models", ttsDir)
	}
	seen := map[string]bool{}
	for _, r := range roles {
		if r.Name == "" || r.ID == "" {
			t.Errorf("a discovered role is unnamed: %+v", r)
		}
		if err := r.Validate(); err != nil {
			t.Errorf("discovered role %s does not validate: %v", r.ID, err)
		}
		if seen[r.ID] {
			t.Errorf("duplicate role id %s", r.ID)
		}
		seen[r.ID] = true
		t.Logf("%-10s %s", r.ID, r.Name)
	}
	fmt.Fprintf(os.Stderr, "discovered %d roles\n", len(roles))
}
