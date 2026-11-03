package main

import (
	"os"
	"path/filepath"
	"testing"
)

// This file covers WHICH EXECUTABLE the autostart switch registers.
//
// That is the defect these tests exist for: 「开机自动启动」 lives in this program, but the
// Run entry must name the PET, and the old code let desktop.SetAutostart resolve
// os.Executable() - which is this program. Windows then started the control center at
// logon and the user got no pet, which reads exactly like auto-start being broken. So
// "the pet is where these candidates say it is" is not a detail: it is the feature.

// TestPetExecutableCandidatesPreferTheSibling pins the release layout: tools\build-all.ps1
// copies both programs into the same directory, so the pet is literally next to us.
func TestPetExecutableCandidatesPreferTheSibling(t *testing.T) {
	self := `F:\pet_pc\build\bin\PetPCPluginMarket.exe`
	got := petExecutableCandidates(self)
	if len(got) == 0 {
		t.Fatal("no candidates: the switch could never find the pet")
	}
	want := filepath.Join(`F:\pet_pc\build\bin`, petExeName)
	if got[0] != want {
		t.Errorf("first candidate = %s, want %s (beside the control center)", got[0], want)
	}

	// Every candidate must name the PET. A candidate that names this program would
	// register the control center again - the exact bug.
	for _, p := range got {
		if filepath.Base(p) != petExeName {
			t.Errorf("candidate %s does not name the pet (%s)", p, petExeName)
		}
	}
}

// TestPetExecutableCandidatesReachTheBuildTree covers the development layout: the control
// center built by `wails build` in tools\plugin-market\build\bin, the pet in build\bin.
func TestPetExecutableCandidatesReachTheBuildTree(t *testing.T) {
	self := `F:\pet_pc\tools\plugin-market\build\bin\PetPCPluginMarket.exe`
	want := filepath.Join(`F:\pet_pc`, "build", "bin", petExeName)

	for _, p := range petExecutableCandidates(self) {
		if filepath.Clean(p) == filepath.Clean(want) {
			return
		}
	}
	t.Fatalf("no candidate reaches %s: candidates were %v", want, petExecutableCandidates(self))
}

// TestFirstExistingFilePicksTheFirstHit is the resolution rule itself: first match in
// order, and "not found" rather than a guess. A wrong path would be written into the Run
// key and only show up as a missing pet after the next logon.
func TestFirstExistingFilePicksTheFirstHit(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first", "PetPC.exe")
	second := filepath.Join(dir, "second", "PetPC.exe")
	for _, p := range []string{first, second} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte("stub"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	got, ok := firstExistingFile([]string{first, second})
	if !ok || got != first {
		t.Errorf("firstExistingFile = (%s, %v), want (%s, true)", got, ok, first)
	}

	// Order is honoured: the second wins once the first is gone.
	if err := os.Remove(first); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, ok = firstExistingFile([]string{first, second})
	if !ok || got != second {
		t.Errorf("after removing the first, firstExistingFile = (%s, %v), want (%s, true)", got, ok, second)
	}

	// A directory is not an executable: choosing one would write a path Explorer cannot
	// start.
	subdir := filepath.Join(dir, "third")
	if err := os.MkdirAll(filepath.Join(subdir, petExeName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, ok := firstExistingFile([]string{filepath.Join(subdir, petExeName)}); ok {
		t.Error("firstExistingFile accepted a directory")
	}

	if _, ok := firstExistingFile([]string{filepath.Join(dir, "nope", petExeName)}); ok {
		t.Error("firstExistingFile reported a file that does not exist")
	}
}
