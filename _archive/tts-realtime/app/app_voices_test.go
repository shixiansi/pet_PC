package main

import (
	"os"
	"path/filepath"
	"testing"

	"PetPC/internal/store"
)

// makeVoiceDir lays out one character folder the way tools/tts-p0/install.ps1
// does, and returns its path.
func makeVoiceDir(t *testing.T, root, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for f, content := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}
	return dir
}

// TestScanVoicesFindsInstalledCharacters covers the dropdown's data source: the
// panel lists characters instead of making the user type three paths each.
func TestScanVoicesFindsInstalledCharacters(t *testing.T) {
	root := t.TempDir()
	makeVoiceDir(t, root, "klee", map[string]string{
		"gpt.ckpt": "x", "sovits.pth": "y", "ref.wav": "z",
	})
	makeVoiceDir(t, root, "nahida", map[string]string{
		"gpt.ckpt": "x", "sovits.pth": "y", "ref.wav": "z",
	})
	// Not a character: a stray file where a folder is expected.
	os.WriteFile(filepath.Join(root, "readme.txt"), []byte("hi"), 0o644)

	got := scanVoices(root)
	if len(got) != 2 {
		t.Fatalf("found %d voices (%+v), want 2", len(got), got)
	}
	for _, v := range got {
		if !v.Complete {
			t.Errorf("%s reported incomplete: missing %v", v.ID, v.Missing)
		}
		if !filepath.IsAbs(v.GPTWeights) || !filepath.IsAbs(v.SovitsWeights) || !filepath.IsAbs(v.RefAudio) {
			t.Errorf("%s returned a relative path: %+v", v.ID, v)
		}
	}
}

// TestScanVoicesMarksIncomplete covers a half-downloaded character. It must still
// be listed - so the user can see it is there and what is missing - but flagged,
// rather than offered as a working choice that then fails at synthesis time.
func TestScanVoicesMarksIncomplete(t *testing.T) {
	root := t.TempDir()
	makeVoiceDir(t, root, "broken", map[string]string{"gpt.ckpt": "x"})

	got := scanVoices(root)
	if len(got) != 1 {
		t.Fatalf("got %d voices, want 1 (the incomplete one must still be listed)", len(got))
	}
	if got[0].Complete {
		t.Error("a folder with only a gpt checkpoint was reported complete")
	}
	if len(got[0].Missing) == 0 {
		t.Error("Missing is empty, so the panel cannot say what is absent")
	}
}

// TestScanVoicesSortsCompleteFirst puts the usable voices at the top of the list,
// which is what the user is looking for.
func TestScanVoicesSortsCompleteFirst(t *testing.T) {
	root := t.TempDir()
	makeVoiceDir(t, root, "aaa_incomplete", map[string]string{"gpt.ckpt": "x"})
	makeVoiceDir(t, root, "zzz_complete", map[string]string{
		"gpt.ckpt": "x", "sovits.pth": "y", "ref.wav": "z",
	})

	got := scanVoices(root)
	if len(got) != 2 {
		t.Fatalf("got %d voices, want 2", len(got))
	}
	if !got[0].Complete {
		t.Errorf("the complete voice is not first: %+v", got)
	}
}

// TestScanVoicesAcceptsHandAssembledFolders covers a folder copied straight out of
// a model repo, where the file names carry an epoch suffix. Recognising those is
// the difference between "the panel found my models" and "the panel says I have no
// models even though the files are right there".
func TestScanVoicesAcceptsHandAssembledFolders(t *testing.T) {
	root := t.TempDir()
	makeVoiceDir(t, root, "Klee", map[string]string{
		"Klee-e10.ckpt":     "x",
		"Klee_e8_s656.pth":  "y",
		"Klee_ref.wav":      "z",
		"petpc-config.json": `{"ttsCharacter":"可莉"}`,
	})

	got := scanVoices(root)
	if len(got) != 1 {
		t.Fatalf("got %d voices, want 1", len(got))
	}
	v := got[0]
	if !v.Complete {
		t.Fatalf("epoch-suffixed files were not recognised: missing %v", v.Missing)
	}
	if filepath.Base(v.GPTWeights) != "Klee-e10.ckpt" {
		t.Errorf("GPTWeights = %q", v.GPTWeights)
	}
	if filepath.Base(v.SovitsWeights) != "Klee_e8_s656.pth" {
		t.Errorf("SovitsWeights = %q", v.SovitsWeights)
	}
	// The label from the folder's own config wins over the romaniased folder name.
	if v.Name != "可莉" {
		t.Errorf("Name = %q, want the label from petpc-config.json", v.Name)
	}
}

// TestScanVoicesEmptyAndMissingAreNotErrors pins the degradation: an unset or
// missing directory is a normal state, not a failure the panel has to apologise
// for.
func TestScanVoicesEmptyAndMissingAreNotErrors(t *testing.T) {
	if got := scanVoices(""); len(got) != 0 {
		t.Errorf("scanVoices(\"\") = %+v, want empty", got)
	}
	if got := scanVoices(filepath.Join(t.TempDir(), "does-not-exist")); len(got) != 0 {
		t.Errorf("a missing directory returned %+v, want empty", got)
	}
	// An empty directory is likewise just "no voices".
	if got := scanVoices(t.TempDir()); len(got) != 0 {
		t.Errorf("an empty directory returned %+v, want empty", got)
	}
}

// TestDiscoverVoicesUsesConfiguredDir is the wiring check: the list the panel sees
// must come from the directory the user configured, not from a guess.
func TestDiscoverVoicesUsesConfiguredDir(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	root := t.TempDir()
	makeVoiceDir(t, root, "klee", map[string]string{
		"gpt.ckpt": "x", "sovits.pth": "y", "ref.wav": "z",
	})

	// Unset: nothing listed, and no crash.
	if got := a.DiscoverVoices(); len(got) != 0 {
		t.Errorf("with no models dir configured, found %+v", got)
	}
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.TTSModelsDir = root }); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	got := a.DiscoverVoices()
	if len(got) != 1 || got[0].ID != "klee" {
		t.Fatalf("found %+v, want the klee folder", got)
	}
	// And the view carries the same list, so the dropdown and the paths beside it
	// come from one snapshot.
	v := a.speechView()
	if len(v.Voices) != 1 || v.ModelsDir != root {
		t.Errorf("speechView = %+v, want it to carry the voice list", v)
	}
}

// TestSaveTTSSettingsPersistsModelsDir keeps the folder choice across restarts.
func TestSaveTTSSettingsPersistsModelsDir(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	dir := t.TempDir()
	if _, err := a.SaveTTSSettings(SpeechView{
		Enabled: true, Endpoint: "http://127.0.0.1:9880", ModelsDir: dir,
	}); err != nil {
		t.Fatalf("SaveTTSSettings: %v", err)
	}
	if got := a.store.Config().TTSModelsDir; got != dir {
		t.Errorf("TTSModelsDir = %q, want %q", got, dir)
	}
}

// TestPickTTSFileRejectsUnknownKind covers the argument guard: an unknown kind must
// be an error rather than opening an unfiltered dialog.
func TestPickTTSFileRejectsUnknownKind(t *testing.T) {
	a, _ := newLLMTestAppWithEvents(t)
	if _, err := a.PickTTSFile("nonsense"); err == nil {
		t.Error("PickTTSFile accepted an unknown kind")
	}
}
