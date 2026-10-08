package main

import (
	"os"
	"testing"

	"PetPC/internal/store"
)

// TestScanVoicesAgainstRealModelsDir is a manual probe, skipped unless
// PETPC_REAL_MODELS is set to a real models folder.
//
// It exists because the unit tests build their own folders, so they prove the
// scan logic but not that it matches the layout the install script actually
// produces on this machine. Run it with:
//
//	PETPC_REAL_MODELS='D:\GPT-SoVITS\models' go test -run RealModels -v .
func TestScanVoicesAgainstRealModelsDir(t *testing.T) {
	dir := os.Getenv("PETPC_REAL_MODELS")
	if dir == "" {
		t.Skip("set PETPC_REAL_MODELS to a real models folder to run this probe")
	}
	voices := scanVoices(dir)
	t.Logf("found %d voice(s) under %s", len(voices), dir)
	for _, v := range voices {
		status := "complete"
		if !v.Complete {
			status = "INCOMPLETE"
		}
		t.Logf("  [%s] id=%-9s name=%-10s", status, v.ID, v.Name)
		t.Logf("        gpt    = %s", v.GPTWeights)
		t.Logf("        sovits = %s", v.SovitsWeights)
		t.Logf("        ref    = %s", v.RefAudio)
		if len(v.Missing) > 0 {
			t.Logf("        missing= %v", v.Missing)
		}
	}
	if len(voices) == 0 {
		t.Errorf("no voices found: the scan does not match the layout under %s", dir)
	}
	for _, v := range voices {
		if !v.Complete {
			t.Errorf("%s is incomplete (%v) but the folder was produced by install.ps1", v.ID, v.Missing)
		}
	}

	// And the same list must come back through the App the panel talks to.
	a, _ := newLLMTestAppWithEvents(t)
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.TTSModelsDir = dir }); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if got := a.DiscoverVoices(); len(got) != len(voices) {
		t.Errorf("DiscoverVoices returned %d, scanVoices returned %d", len(got), len(voices))
	}
}
