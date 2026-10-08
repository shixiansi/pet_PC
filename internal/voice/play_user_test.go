package voice

import (
	"path/filepath"
	"testing"
)

// TestUserTakeWinsOverEmbedded is the guarantee the whole studio depends on: once a
// line has a chosen take FOR THE CURRENT CHARACTER, playing that line must use it -
// not the built-in clip.
//
// Without this the feature would look like it worked (renders stored, panel updated)
// while the pet kept saying the original voice, which is the worst possible failure
// because nothing reports an error.
func TestUserTakeWinsOverEmbedded(t *testing.T) {
	base := t.TempDir()
	user, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// A character must be selected first: with none, the built-in clips are the
	// CORRECT answer rather than a fallback, so the test would be asserting the
	// wrong branch.
	if err := user.UpsertRole(testRole()); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := user.SetActiveRole(testRole().ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	take, err := user.AddTake(LineFocusEnd, wav(22050, 22050), testRole(), "台词")
	if err != nil {
		t.Fatalf("AddTake: %v", err)
	}

	rec := &recordingPlayer{}
	v := NewWithUser(rec, filepath.Join(base, "clip"), user)
	if err := v.Play(LineFocusEnd); err != nil {
		t.Fatalf("Play: %v", err)
	}

	want := filepath.Join(user.lineDir(LineFocusEnd), take.File)
	if got := rec.last(); got != want {
		t.Errorf("played %q, want the user's take %q", got, want)
	}
}

// TestUnrenderedLineUsesEmbedded covers the ONE state that legitimately plays the
// built-in clips: no character has been chosen.
//
// This is the "never set up the studio" case, and it must keep working - the pet
// speaking its embedded voice out of the box is the shipped behaviour.
func TestUnrenderedLineUsesEmbedded(t *testing.T) {
	base := t.TempDir()
	user, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rec := &recordingPlayer{}
	v := NewWithUser(rec, filepath.Join(base, "clip"), user)

	if err := v.Play(LineBreakEnd); err != nil {
		t.Fatalf("Play: %v", err)
	}
	// The embedded path is the hashed materialisation, not the user store's.
	if got := rec.last(); filepath.Dir(got) != filepath.Join(base, "clip") {
		t.Errorf("played %q, which is not the materialised embedded clip", got)
	}
}

// TestSelectedCharacterWithoutATakeRefusesToPlay is the reported bug, expressed as a
// playback assertion.
//
// With a character selected but no adopted take of theirs for this line, playback
// must FAIL rather than quietly play the embedded 可莉 clip. The silent fallback was
// exactly the "界面选中态与运行时音色脱节" the fix removes: the user heard a
// character the UI did not show, and nothing anywhere reported it.
func TestSelectedCharacterWithoutATakeRefusesToPlay(t *testing.T) {
	base := t.TempDir()
	user, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := user.UpsertRole(testRole()); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := user.SetActiveRole(testRole().ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}

	rec := &recordingPlayer{}
	v := NewWithUser(rec, filepath.Join(base, "clip"), user)
	err = v.Play(LineFocusEnd)
	if err == nil {
		t.Fatal("a line with no take for the selected character must report an error")
	}
	if rec.count() != 0 {
		t.Errorf("nothing may be played, but the player was called %d time(s) with %q", rec.count(), rec.last())
	}
	// The message must be actionable, not merely present.
	if !contains(err.Error(), "还没有") {
		t.Errorf("error %q does not explain that this character has no take yet", err)
	}
}

// TestSwitchingCharacterChangesWhatPlaysThroughThePlayer is the end-to-end version of
// the fix at the layer the pet actually uses: the same Voice instance, two
// characters, and the audio following the selection without a restart.
func TestSwitchingCharacterChangesWhatPlaysThroughThePlayer(t *testing.T) {
	base := t.TempDir()
	user, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	klee, nahida := testRole(), Role{ID: "nahida", Name: "纳西妲", GPT: "g2", SoVITS: "v2", Ref: "r2"}
	nahida.Normalise()
	for _, r := range []Role{klee, nahida} {
		if err := user.UpsertRole(r); err != nil {
			t.Fatalf("UpsertRole: %v", err)
		}
	}

	if err := user.SetActiveRole(klee.ID); err != nil {
		t.Fatalf("SetActiveRole klee: %v", err)
	}
	kleeTake, err := user.AddTake(LineFocusEnd, wav(1000, 22050), klee, "台词")
	if err != nil {
		t.Fatalf("AddTake klee: %v", err)
	}
	if err := user.SetActiveRole(nahida.ID); err != nil {
		t.Fatalf("SetActiveRole nahida: %v", err)
	}
	nahidaTake, err := user.AddTake(LineFocusEnd, wav(3000, 22050), nahida, "台词")
	if err != nil {
		t.Fatalf("AddTake nahida: %v", err)
	}
	// Adopt it: there is one chosen version per line, and 生成 ≠ 采用 by design.
	if err := user.Choose(LineFocusEnd, nahidaTake.Version); err != nil {
		t.Fatalf("Choose nahida: %v", err)
	}

	rec := &recordingPlayer{}
	v := NewWithUser(rec, filepath.Join(base, "clip"), user)

	// Nahida is selected: her take must play.
	if err := v.Play(LineFocusEnd); err != nil {
		t.Fatalf("Play as nahida: %v", err)
	}
	if got, want := filepath.Base(rec.last()), nahidaTake.File; got != want {
		t.Fatalf("played %q as 纳西妲, want %q", got, want)
	}

	// Switch back on the SAME Voice: the next utterance must follow, with no restart
	// and no cache holding the previous character's clip.
	if err := user.Choose(LineFocusEnd, kleeTake.Version); err != nil {
		t.Fatalf("Choose klee: %v", err)
	}
	if err := user.SetActiveRole(klee.ID); err != nil {
		t.Fatalf("SetActiveRole klee again: %v", err)
	}
	if err := v.Play(LineFocusEnd); err != nil {
		t.Fatalf("Play as klee: %v", err)
	}
	if got, want := filepath.Base(rec.last()), kleeTake.File; got != want {
		t.Errorf("played %q after switching to 可莉, want %q", got, want)
	}
}

// TestChoiceChangeTakesEffectWithoutRestart pins the cache behaviour that would
// otherwise produce "I replaced it and nothing happened": the chosen clip is
// resolved on every play, so a change made in the panel applies to the very next
// utterance.
func TestChoiceChangeTakesEffectWithoutRestart(t *testing.T) {
	base := t.TempDir()
	user, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := user.UpsertRole(testRole()); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := user.SetActiveRole(testRole().ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	first, err := user.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词")
	if err != nil {
		t.Fatalf("AddTake: %v", err)
	}
	second, err := user.AddTake(LineFocusEnd, wav(9000, 22050), testRole(), "台词")
	if err != nil {
		t.Fatalf("AddTake: %v", err)
	}

	rec := &recordingPlayer{}
	v := NewWithUser(rec, filepath.Join(base, "clip"), user)
	v.Play(LineFocusEnd) //nolint:errcheck // warms any cache there might be

	if err := user.Choose(LineFocusEnd, second.Version); err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if err := v.Play(LineFocusEnd); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if got, want := filepath.Base(rec.last()), second.File; got != want {
		t.Errorf("played %q after choosing v%d, want %q (played v%d before)", got, second.Version, want, first.Version)
	}
}

// TestClearingChoiceRemovesTheLineFromPlayback covers the "清除采用" action.
//
// With a character still selected, clearing the choice means this line has no take
// for that character - so it must be reported, NOT silently reverted to the embedded
// clips, which belong to a different voice.
func TestClearingChoiceRemovesTheLineFromPlayback(t *testing.T) {
	base := t.TempDir()
	user, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := user.UpsertRole(testRole()); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := user.SetActiveRole(testRole().ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	user.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词") //nolint:errcheck
	rec := &recordingPlayer{}
	v := NewWithUser(rec, filepath.Join(base, "clip"), user)
	if err := v.Play(LineFocusEnd); err != nil {
		t.Fatalf("precondition: the take should play: %v", err)
	}

	if err := user.ClearChoice(LineFocusEnd); err != nil {
		t.Fatalf("ClearChoice: %v", err)
	}
	if err := v.Play(LineFocusEnd); err == nil {
		t.Fatal("after clearing the choice under a selected character, playback must report the gap")
	}
	if got := rec.last(); filepath.Dir(got) == filepath.Join(base, "clip") {
		t.Error("clearing the choice fell back to the embedded clips instead of reporting")
	}
}

// TestClearingTheCharacterRestoresEmbedded is the actual way back to the built-in
// voice: not by clearing per-line choices, but by clearing the CHARACTER.
//
// This is the distinction that made the old UI misleading - "改用内置" per line
// could not return the pet to the embedded clips while a character was selected.
func TestClearingTheCharacterRestoresEmbedded(t *testing.T) {
	base := t.TempDir()
	user, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := user.UpsertRole(testRole()); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := user.SetActiveRole(testRole().ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	user.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词") //nolint:errcheck

	rec := &recordingPlayer{}
	v := NewWithUser(rec, filepath.Join(base, "clip"), user)

	// Selecting the built-in sentinel is how a caller returns to the embedded voice.
	if err := user.SetActiveRole(BuiltInRoleID); err != nil {
		t.Fatalf("SetActiveRole built-in: %v", err)
	}
	if err := v.Play(LineFocusEnd); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if got := filepath.Dir(rec.last()); got != filepath.Join(base, "clip") {
		t.Errorf("played %q; want the embedded clip after clearing the character", got)
	}
}

// TestPlayEmbeddedIgnoresUserTake backs the panel's "listen to the built-in voice"
// button: comparing the two is the point of offering the choice, so the button must
// actually play the original even when a take is in use.
func TestPlayEmbeddedIgnoresUserTake(t *testing.T) {
	base := t.TempDir()
	user, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := user.UpsertRole(testRole()); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := user.SetActiveRole(testRole().ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	user.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词") //nolint:errcheck

	rec := &recordingPlayer{}
	v := NewWithUser(rec, filepath.Join(base, "clip"), user)
	if err := v.PlayEmbedded(LineFocusEnd); err != nil {
		t.Fatalf("PlayEmbedded: %v", err)
	}
	if got := filepath.Dir(rec.last()); got != filepath.Join(base, "clip") {
		t.Errorf("PlayEmbedded played %q, want the embedded clip", got)
	}
}

// TestNoUserStoreUsesEmbedded pins the unchanged behaviour of a build with no user
// library at all (the wiring in tests, or the feature unavailable): the embedded
// clips still play, because there is no character to be inconsistent with.
func TestNoUserStoreUsesEmbedded(t *testing.T) {
	rec := &recordingPlayer{}
	v := New(rec, t.TempDir())
	if err := v.Play(LineFocusEnd); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if rec.count() != 1 {
		t.Errorf("played %d clips, want 1", rec.count())
	}
}

// TestCleanupLeavesUserRendersAlone is a data-loss guard. Cleanup deletes files it
// does not recognise in the voice directory; the user's renders live in a
// subdirectory and must never be touched by it.
func TestCleanupLeavesUserRendersAlone(t *testing.T) {
	base := t.TempDir()
	user, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	take, err := user.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词")
	if err != nil {
		t.Fatalf("AddTake: %v", err)
	}

	Cleanup(base)

	path := filepath.Join(user.lineDir(LineFocusEnd), take.File)
	if !fileExists(path) {
		t.Errorf("Cleanup deleted a user render: %s", path)
	}
}

// TestCleanupStillRemovesStaleEmbeddedClips is the behaviour Cleanup exists for, and
// it must keep working now that a user directory lives inside the same tree.
func TestCleanupStillRemovesStaleEmbeddedClips(t *testing.T) {
	base := t.TempDir()
	stale := filepath.Join(base, "petpc-focus_end-deadbeef.wav")
	writeFile(t, stale, []byte("old"))
	other := filepath.Join(base, "not-ours.wav")
	writeFile(t, other, []byte("keep"))

	Cleanup(base)

	if fileExists(stale) {
		t.Error("a stale clip in our own naming scheme should have been removed")
	}
	if !fileExists(other) {
		t.Error("Cleanup removed a file that was not ours")
	}
}

// contains is a tiny substring helper, kept local so the test file does not pull in
// strings just for one assertion.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
