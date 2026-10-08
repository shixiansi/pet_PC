package voice

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wav builds a minimal but valid mono 16-bit WAV with the given frame count, so
// tests exercise the real duration parser instead of a stub.
func wav(frames int, rate int) []byte {
	dataLen := frames * 2
	buf := make([]byte, 44+dataLen)
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataLen))
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:], 1) // mono
	binary.LittleEndian.PutUint32(buf[24:], uint32(rate))
	binary.LittleEndian.PutUint32(buf[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataLen))
	return buf
}

func testRole() Role {
	return Role{ID: "klee", Name: "可莉", GPT: "g.ckpt", SoVITS: "s.pth", Ref: "r.wav"}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

// withRole returns a store whose CURRENT character is testRole()'s klee.
//
// Nearly every playback assertion needs this, because a take only plays when a
// character is in effect: with no character chosen the store resolves to the
// built-in clips by design, so a test that forgot to select one would be asserting
// against the built-in branch while believing it was asserting the user's take.
func withRole(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	if err := s.UpsertRole(testRole()); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := s.SetActiveRole(testRole().ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	return s
}

// resolveUserClip resolves a line the way the PLAYER does and reports whether it is
// the user's take, so assertions keep reading as "does this line play my clip".
//
// A resolution error is reported as ok=false: these tests are about which clip wins,
// and the error cases have their own tests below.
func resolveUserClip(t *testing.T, s *Store, line Line) (string, bool) {
	t.Helper()
	res, err := s.ResolveClip(line)
	if err != nil {
		return "", false
	}
	if res.Source != SourceUser {
		return "", false
	}
	return res.Path, true
}

// TestTakeIsRecordedAndChosen covers the basic render flow: a take is stored, its
// metadata describes the clip, and the FIRST take for a line becomes the chosen one
// so that rendering the whole set makes the new voice audible without a second step.
func TestTakeIsRecordedAndChosen(t *testing.T) {
	s := withRole(t)
	take, err := s.AddTake(LineFocusEnd, wav(22050, 22050), testRole(), "台词")
	if err != nil {
		t.Fatalf("AddTake: %v", err)
	}
	if take.Version != 1 {
		t.Errorf("first take is version %d, want 1", take.Version)
	}
	if take.DurationMS != 1000 {
		t.Errorf("duration %dms, want 1000", take.DurationMS)
	}
	if take.RoleName != "可莉" {
		t.Errorf("role name %q, want 可莉", take.RoleName)
	}

	slot := s.Settings().Lines[LineFocusEnd]
	if slot.Chosen != 1 {
		t.Errorf("chosen %d, want 1 (the first take should be adopted)", slot.Chosen)
	}
	if _, ok := resolveUserClip(t, s, LineFocusEnd); !ok {
		t.Error("the chosen take does not resolve for the current character")
	}
}

// TestRegenerateAppendsAndKeepsTheOldTake is the property the whole feature rests
// on: re-rendering a line that sounds wrong must NOT destroy the take already
// there, because the user is comparing them.
func TestRegenerateAppendsAndKeepsTheOldTake(t *testing.T) {
	s := newTestStore(t)
	first, err := s.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词")
	if err != nil {
		t.Fatalf("AddTake: %v", err)
	}
	second, err := s.AddTake(LineFocusEnd, wav(2000, 22050), testRole(), "台词")
	if err != nil {
		t.Fatalf("AddTake: %v", err)
	}
	if second.Version != 2 {
		t.Errorf("second take is version %d, want 2", second.Version)
	}

	slot := s.Settings().Lines[LineFocusEnd]
	if len(slot.Takes) != 2 {
		t.Fatalf("%d takes kept, want 2", len(slot.Takes))
	}
	// The already-chosen take must not be silently swapped by a regeneration.
	if slot.Chosen != first.Version {
		t.Errorf("chosen %d after regenerating, want it unchanged at %d", slot.Chosen, first.Version)
	}
	// Both files must exist: an appended take that overwrote the first would make
	// the comparison impossible.
	if _, err := os.Stat(filepath.Join(s.lineDir(LineFocusEnd), first.File)); err != nil {
		t.Errorf("the first take's audio is gone: %v", err)
	}
}

// TestChooseIsWhatChangesPlayback pins the separation between auditioning and
// committing: rendering must never change what the pet says, and Choose must.
func TestChooseIsWhatChangesPlayback(t *testing.T) {
	s := withRole(t)
	s.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词") //nolint:errcheck // v1
	s.AddTake(LineFocusEnd, wav(9000, 22050), testRole(), "台词") //nolint:errcheck // v2
	s.ClearChoice(LineFocusEnd)                                 //nolint:errcheck // start with none

	// With a character selected, a line that has no adopted take must NOT resolve to
	// a clip: the built-in audio belongs to the built-in voice, not to klee.
	if _, ok := resolveUserClip(t, s, LineFocusEnd); ok {
		t.Fatal("a line with no choice must not resolve to a user clip")
	}
	if _, err := s.ResolveClip(LineFocusEnd); err == nil {
		t.Fatal("a line with no choice under a selected character must report why")
	}
	if err := s.Choose(LineFocusEnd, 2); err != nil {
		t.Fatalf("Choose: %v", err)
	}
	path, ok := resolveUserClip(t, s, LineFocusEnd)
	if !ok {
		t.Fatal("the chosen take does not resolve after Choose")
	}
	// The chosen clip must be the one chosen, not merely some take.
	states := s.LineStates(map[Line]bool{LineFocusEnd: true})
	var focus LineState
	for _, st := range states {
		if st.Line == LineFocusEnd {
			focus = st
		}
	}
	if focus.DurationMS != 9000*1000/22050 {
		t.Errorf("chosen take duration %dms, want the v2 take", focus.DurationMS)
	}
	if filepath.Base(path) != "v002.wav" {
		t.Errorf("resolution returned %q, want v002.wav", filepath.Base(path))
	}
}

// TestDeleteChosenTakeFallsBackRatherThanPromoting is a deliberate design choice:
// silently switching the pet to a take the user did NOT pick is worse than falling
// back to the built-in clip, which is at least a known quantity.
func TestDeleteChosenTakeFallsBackRatherThanPromoting(t *testing.T) {
	s := withRole(t)
	s.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词") //nolint:errcheck
	s.AddTake(LineFocusEnd, wav(2000, 22050), testRole(), "台词") //nolint:errcheck
	if err := s.Choose(LineFocusEnd, 2); err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if err := s.DeleteTake(LineFocusEnd, 2); err != nil {
		t.Fatalf("DeleteTake: %v", err)
	}
	if _, ok := resolveUserClip(t, s, LineFocusEnd); ok {
		t.Error("deleting the chosen take must clear the choice, not promote another")
	}
	if got := len(s.Settings().Lines[LineFocusEnd].Takes); got != 1 {
		t.Errorf("%d takes left, want 1", got)
	}
}

// TestMissingFileIsReportedNotReplaced covers the case where the user deletes the
// audio by hand.
//
// It must NOT quietly become the embedded clip: that would play a DIFFERENT
// character than the one the UI shows, with nothing anywhere saying so. Silence plus
// a message naming the missing file is diagnosable; the wrong voice is not.
func TestMissingFileIsReportedNotReplaced(t *testing.T) {
	s := withRole(t)
	take, err := s.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词")
	if err != nil {
		t.Fatalf("AddTake: %v", err)
	}
	if _, ok := resolveUserClip(t, s, LineFocusEnd); !ok {
		t.Fatal("precondition: the take should resolve")
	}
	if err := os.Remove(filepath.Join(s.lineDir(LineFocusEnd), take.File)); err != nil {
		t.Fatalf("remove clip: %v", err)
	}
	// A fresh Store so the in-memory path cache cannot mask the missing file.
	fresh, err := Open(filepath.Dir(s.Dir()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	res, err := fresh.ResolveClip(LineFocusEnd)
	if err == nil {
		t.Fatalf("a chosen take whose file is gone must be reported, got source %q path %q", res.Source, res.Path)
	}
	if res.Source == SourceBuiltIn {
		t.Error("a missing user clip must not resolve to the built-in voice")
	}
}

// TestSettingsSurviveReopen is the durability requirement: a render is minutes of
// CPU time and a judgement call, so it must outlive the process.
func TestSettingsSurviveReopen(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertRole(Role{ID: "klee", Name: "可莉", GPT: "g", SoVITS: "v", Ref: "r"}); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	// Explicitly selected: adding a role no longer implies choosing it, which is the
	// whole point of the fix - "a character exists" and "this character is in use"
	// are different facts.
	if err := s.SetActiveRole("klee"); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	if _, err := s.AddTake(LineBreakEnd, wav(4410, 22050), testRole(), "休息结束了"); err != nil {
		t.Fatalf("AddTake: %v", err)
	}
	base := filepath.Dir(s.Dir())

	reopened, err := Open(base)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	set := reopened.Settings()
	if len(set.Roles) != 1 || set.Roles[0].Name != "可莉" {
		t.Fatalf("roles did not survive: %+v", set.Roles)
	}
	if set.Roles[0].TopK != 15 || set.Roles[0].Speed != 1 {
		t.Errorf("role defaults were not applied on load: %+v", set.Roles[0])
	}
	if got := reopened.Settings().Lines[LineBreakEnd].Chosen; got != 1 {
		t.Errorf("chosen take did not survive: %d", got)
	}
	// The current character must survive the reopen too, or the pet would come back
	// speaking a different voice than the one the user left selected.
	role, err := reopened.ResolveCurrentRole()
	if err != nil {
		t.Fatalf("the current character did not survive the reopen: %v", err)
	}
	if role.ID != "klee" {
		t.Errorf("current character %q after reopen, want klee", role.ID)
	}
	if _, ok := resolveUserClip(t, reopened, LineBreakEnd); !ok {
		t.Error("the clip does not resolve after reopening")
	}
}

// TestResetKeepsAudio is the escape hatch: one click returns the pet to the
// embedded voice, and must not throw away renders the user may want back.
func TestResetKeepsAudio(t *testing.T) {
	s := withRole(t)
	take, err := s.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词")
	if err != nil {
		t.Fatalf("AddTake: %v", err)
	}
	if err := s.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := resolveUserClip(t, s, LineFocusEnd); ok {
		t.Error("Reset must stop the pet using the user clip")
	}
	if got := len(s.Settings().Lines[LineFocusEnd].Takes); got != 1 {
		t.Errorf("Reset deleted takes: %d left", got)
	}
	if _, err := os.Stat(filepath.Join(s.lineDir(LineFocusEnd), take.File)); err != nil {
		t.Errorf("Reset deleted the audio file: %v", err)
	}
}

// TestSwitchingCharacterChangesWhatPlays is the regression for the reported bug and
// the heart of the fix.
//
// Before this, selecting a character changed only the persisted ActiveRole, which
// playback never consulted: the pet kept speaking whichever take each line had
// adopted - so switching characters from 可莉 to 纳西妲 changed the UI and not a
// single sound.
func TestSwitchingCharacterChangesWhatPlays(t *testing.T) {
	s := newTestStore(t)
	klee := testRole()
	nahida := Role{ID: "nahida", Name: "纳西妲", GPT: "g2", SoVITS: "v2", Ref: "r2"}
	nahida.Normalise()
	for _, r := range []Role{klee, nahida} {
		if err := s.UpsertRole(r); err != nil {
			t.Fatalf("UpsertRole %s: %v", r.ID, err)
		}
	}

	// Both characters record the same line.
	if err := s.SetActiveRole(klee.ID); err != nil {
		t.Fatalf("SetActiveRole klee: %v", err)
	}
	kleeTake, err := s.AddTake(LineFocusEnd, wav(1000, 22050), klee, "台词")
	if err != nil {
		t.Fatalf("AddTake klee: %v", err)
	}
	if err := s.SetActiveRole(nahida.ID); err != nil {
		t.Fatalf("SetActiveRole nahida: %v", err)
	}
	// Nahida has recorded nothing yet: this is the case that used to play 可莉.
	if _, err := s.ResolveClip(LineFocusEnd); err == nil {
		t.Fatal("with nahida selected and no take of hers, playback must be refused rather than falling back")
	}

	nahidaTake, err := s.AddTake(LineFocusEnd, wav(3000, 22050), nahida, "台词")
	if err != nil {
		t.Fatalf("AddTake nahida: %v", err)
	}
	// Adopted explicitly: rendering alone must never commit (生成 ≠ 采用), and there
	// is ONE chosen version per line, so the newly rendered take has to be adopted
	// for it to be what plays.
	if err := s.Choose(LineFocusEnd, nahidaTake.Version); err != nil {
		t.Fatalf("Choose: %v", err)
	}

	// Nahida is selected, so her take must play - not klee's, which is the one that
	// had been adopted all along.
	res, err := s.ResolveClip(LineFocusEnd)
	if err != nil {
		t.Fatalf("ResolveClip: %v", err)
	}
	if got, want := filepath.Base(res.Path), nahidaTake.File; got != want {
		t.Errorf("with 纳西妲 selected, resolved %q; want her take %q", got, want)
	}

	// Switching back must follow immediately, from the same long-lived store. Klee's
	// take has to be re-adopted first, because the line's single Chosen pointer now
	// points at nahida's version.
	if err := s.Choose(LineFocusEnd, kleeTake.Version); err != nil {
		t.Fatalf("Choose klee: %v", err)
	}
	if err := s.SetActiveRole(klee.ID); err != nil {
		t.Fatalf("SetActiveRole klee again: %v", err)
	}
	res, err = s.ResolveClip(LineFocusEnd)
	if err != nil {
		t.Fatalf("ResolveClip after switching back: %v", err)
	}
	if got, want := filepath.Base(res.Path), kleeTake.File; got != want {
		t.Errorf("after switching back to 可莉, resolved %q; want %q", got, want)
	}
}

// TestAdoptedTakeFromAnotherCharacterIsRefused covers the second half of the bug:
// after switching character, a line whose ADOPTED take belongs to the old character
// must not keep playing that take.
//
// This is the case the "chosen take always wins" rule got wrong - it never asked
// whose voice the chosen take was.
func TestAdoptedTakeFromAnotherCharacterIsRefused(t *testing.T) {
	s := newTestStore(t)
	klee, nahida := testRole(), Role{ID: "nahida", Name: "纳西妲", GPT: "g2", SoVITS: "v2", Ref: "r2"}
	nahida.Normalise()
	for _, r := range []Role{klee, nahida} {
		if err := s.UpsertRole(r); err != nil {
			t.Fatalf("UpsertRole: %v", err)
		}
	}
	if err := s.SetActiveRole(klee.ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	// klee's take is adopted automatically as the first take.
	if _, err := s.AddTake(LineFocusEnd, wav(1000, 22050), klee, "台词"); err != nil {
		t.Fatalf("AddTake: %v", err)
	}
	// A take of nahida's exists but is NOT adopted - a plain generate, which never
	// commits.
	if _, err := s.AddTake(LineFocusEnd, wav(3000, 22050), nahida, "台词"); err != nil {
		t.Fatalf("AddTake nahida: %v", err)
	}

	if err := s.SetActiveRole(nahida.ID); err != nil {
		t.Fatalf("SetActiveRole nahida: %v", err)
	}
	if _, err := s.ResolveClip(LineFocusEnd); err == nil {
		t.Error("an adopted take belonging to a different character must not play for the current one")
	}

	// The panel must describe the same thing playback does: the row is NOT in
	// effect for the current character.
	var focus LineState
	for _, st := range s.LineStates(map[Line]bool{LineFocusEnd: true}) {
		if st.Line == LineFocusEnd {
			focus = st
		}
	}
	if focus.ChosenForActiveRole {
		t.Error("the panel claims this line plays as the current character")
	}
	if focus.EffectSource == "user" {
		t.Errorf("effect source %q, want the not-in-effect state", focus.EffectSource)
	}
}

// TestLineStatesNeverClaimsTheWrongCharacter is the "the badge cannot lie" rule,
// checked against playback itself rather than against a second copy of the rule.
func TestLineStatesNeverClaimsTheWrongCharacter(t *testing.T) {
	s := newTestStore(t)
	klee, nahida := testRole(), Role{ID: "nahida", Name: "纳西妲", GPT: "g2", SoVITS: "v2", Ref: "r2"}
	nahida.Normalise()
	for _, r := range []Role{klee, nahida} {
		if err := s.UpsertRole(r); err != nil {
			t.Fatalf("UpsertRole: %v", err)
		}
	}
	if err := s.SetActiveRole(klee.ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	if _, err := s.AddTake(LineFocusEnd, wav(1000, 22050), klee, "台词"); err != nil {
		t.Fatalf("AddTake: %v", err)
	}
	if err := s.SetActiveRole(nahida.ID); err != nil {
		t.Fatalf("SetActiveRole nahida: %v", err)
	}
	if _, err := s.AddTake(LineBreakEnd, wav(1000, 22050), nahida, "台词"); err != nil {
		t.Fatalf("AddTake: %v", err)
	}

	for _, st := range s.LineStates(map[Line]bool{}) {
		_, playErr := s.ResolveClip(st.Line)
		playsAsUser := playErr == nil && st.EffectSource == "user"
		if playsAsUser && !st.ChosenForActiveRole {
			t.Errorf("%s claims to play the user's clip but resolution disagrees", st.Line)
		}
		if st.EffectSource == "user" && playErr != nil {
			t.Errorf("%s claims to play the user's clip but playback refuses: %v", st.Line, playErr)
		}
	}
}
// TestSeedRolesIsAdditive is what makes "rescan" safe to press: a role the user has
// already tuned must never be overwritten by what is on disk.
func TestSeedRolesIsAdditive(t *testing.T) {
	s := newTestStore(t)
	tuned := Role{ID: "klee", Name: "可莉", GPT: "g", SoVITS: "v", Ref: "r", Speed: 1.7}
	if err := s.UpsertRole(tuned); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	added, err := s.SeedRoles([]Role{
		{ID: "klee", Name: "可莉", GPT: "other", SoVITS: "v", Ref: "r"}, // existing
		{ID: "nahida", Name: "纳西妲", GPT: "g2", SoVITS: "v2", Ref: "r2"},
	})
	if err != nil {
		t.Fatalf("SeedRoles: %v", err)
	}
	if added != 1 {
		t.Errorf("added %d roles, want 1", added)
	}
	got, ok := s.Role("klee")
	if !ok {
		t.Fatal("klee disappeared")
	}
	if got.Speed != 1.7 || got.GPT != "g" {
		t.Errorf("rescan overwrote a tuned role: %+v", got)
	}
	if _, ok := s.Role("nahida"); !ok {
		t.Error("the new role was not added")
	}
}

// TestDeleteRoleKeepsTakes makes the same guarantee for deletion: tidying the role
// list must not destroy audio that is already the pet's voice.
func TestDeleteRoleKeepsTakes(t *testing.T) {
	s := withRole(t)
	if _, err := s.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词"); err != nil {
		t.Fatalf("AddTake: %v", err)
	}
	if err := s.DeleteRole("klee"); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	if _, ok := s.Role("klee"); ok {
		t.Error("the role was not removed")
	}
	// The AUDIO must survive: deleting a role is tidying the character list, not a
	// request to destroy renders that took minutes of CPU.
	if _, err := os.Stat(filepath.Join(s.lineDir(LineFocusEnd), "v001.wav")); err != nil {
		t.Errorf("the audio file was deleted with the role: %v", err)
	}
	// Playback, however, must fall back to the built-in voice - the character the
	// user chose is gone, and speaking as a DIFFERENT configured role would be a
	// switch they never made.
	res, err := s.ResolveClip(LineFocusEnd)
	if err != nil {
		t.Fatalf("after deleting the character, resolution failed: %v", err)
	}
	if res.Source != SourceBuiltIn {
		t.Errorf("after deleting the character, source %q; want the built-in voice", res.Source)
	}
}

// TestSelectionAndVoiceSurviveRestart is acceptance criterion §六.3: after a full
// restart, both the selected character AND the voice the pet speaks must still be
// that character.
//
// Reopening the store IS the restart, as far as this layer is concerned: the pet
// reads the index at startup, and nothing about the selection lives in memory.
func TestSelectionAndVoiceSurviveRestart(t *testing.T) {
	base := t.TempDir()
	s, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	klee, nahida := testRole(), Role{ID: "nahida", Name: "纳西妲", GPT: "g2", SoVITS: "v2", Ref: "r2"}
	nahida.Normalise()
	for _, r := range []Role{klee, nahida} {
		if err := s.UpsertRole(r); err != nil {
			t.Fatalf("UpsertRole: %v", err)
		}
	}
	// Record one line with each character, then leave 纳西妲 selected with HER take
	// adopted - the state a user is in when they close the app.
	if err := s.SetActiveRole(klee.ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	if _, err := s.AddTake(LineFocusEnd, wav(1000, 22050), klee, "台词"); err != nil {
		t.Fatalf("AddTake klee: %v", err)
	}
	if err := s.SetActiveRole(nahida.ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	nahidaTake, err := s.AddTake(LineFocusEnd, wav(3000, 22050), nahida, "台词")
	if err != nil {
		t.Fatalf("AddTake nahida: %v", err)
	}
	if err := s.Choose(LineFocusEnd, nahidaTake.Version); err != nil {
		t.Fatalf("Choose: %v", err)
	}

	// --- restart ---
	restarted, err := Open(base)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	role, err := restarted.ResolveCurrentRole()
	if err != nil {
		t.Fatalf("the selected character did not survive: %v", err)
	}
	if role.ID != nahida.ID {
		t.Errorf("after restart the character is %q, want %q", role.ID, nahida.ID)
	}
	res, err := restarted.ResolveClip(LineFocusEnd)
	if err != nil {
		t.Fatalf("after restart the line does not resolve: %v", err)
	}
	if got, want := filepath.Base(res.Path), nahidaTake.File; got != want {
		t.Errorf("after restart the pet would play %q, want 纳西妲's %q", got, want)
	}
	if res.Role.ID != nahida.ID {
		t.Errorf("the resolved clip belongs to %q, want the selected character", res.Role.ID)
	}
}

// TestDeletedActiveRoleFallsBackToBuiltInNotAnotherRole is the deliberate change
// from "degrade to Roles[0]".
//
// Auto-selecting a surviving role kept the panel populated while silently changing
// which character the pet speaks as - a switch the user never made and could not
// see. Emptying the selection is the honest outcome, and it resolves to the
// built-in clips, which is the one state that legitimately means them.
func TestDeletedActiveRoleFallsBackToBuiltInNotAnotherRole(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.ResolveCurrentRole(); err != nil {
		t.Fatalf("a fresh store must resolve to the built-in voice, not fail: %v", err)
	}
	s.UpsertRole(Role{ID: "a", Name: "A", GPT: "g", SoVITS: "v", Ref: "r"}) //nolint:errcheck
	s.UpsertRole(Role{ID: "b", Name: "B", GPT: "g", SoVITS: "v", Ref: "r"}) //nolint:errcheck
	if err := s.SetActiveRole("b"); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	if r, err := s.ResolveCurrentRole(); err != nil || r.ID != "b" {
		t.Fatalf("current character %q err=%v, want b", r.ID, err)
	}

	s.DeleteRole("b") //nolint:errcheck

	r, err := s.ResolveCurrentRole()
	if err != nil {
		t.Fatalf("after deleting the current character, resolution failed: %v", err)
	}
	if !IsBuiltInRole(r.ID) {
		t.Errorf("after deleting the current character, resolved %q; want the built-in voice, NOT another role", r.ID)
	}
	if r.ID == "a" {
		t.Error("the store silently switched to another configured role")
	}
}

// TestResolveCurrentRoleRejectsADanglingID is the other half: an ActiveRole that
// names nothing is an ERROR, never a quiet substitution.
func TestResolveCurrentRoleRejectsADanglingID(t *testing.T) {
	s := newTestStore(t)
	s.UpsertRole(Role{ID: "a", Name: "A", GPT: "g", SoVITS: "v", Ref: "r"}) //nolint:errcheck
	// Written straight into the file, as a hand-edit or an older build would.
	if err := s.SetActiveRole("a"); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	s.mu.Lock()
	s.set.ActiveRole = "ghost"
	s.mu.Unlock()

	if _, err := s.ResolveCurrentRole(); err == nil {
		t.Fatal("an active role naming no configured role must be an error")
	} else if !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("error %v does not wrap ErrRoleNotFound", err)
	}
}

// TestSetActiveRoleRefusesAnUnknownID pins the write-side half: a bad id is refused
// at the point of the mistake rather than stored and discovered during playback.
func TestSetActiveRoleRefusesAnUnknownID(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetActiveRole("nobody"); err == nil {
		t.Fatal("selecting a character that does not exist must be refused")
	}
	// And nothing was written, so the store still resolves to the built-in voice
	// rather than to a dangling id.
	if r, err := s.ResolveCurrentRole(); err != nil || !IsBuiltInRole(r.ID) {
		t.Errorf("after a refused switch, resolved %q err=%v; want the built-in voice", r.ID, err)
	}
}

// TestAddingARoleDoesNotSelectIt is the "a character existing is not a choice" rule.
func TestAddingARoleDoesNotSelectIt(t *testing.T) {
	s := newTestStore(t)
	s.UpsertRole(Role{ID: "a", Name: "A", GPT: "g", SoVITS: "v", Ref: "r"}) //nolint:errcheck
	r, err := s.ResolveCurrentRole()
	if err != nil {
		t.Fatalf("ResolveCurrentRole: %v", err)
	}
	if !IsBuiltInRole(r.ID) {
		t.Errorf("adding the first role auto-selected %q; it must stay on the built-in voice", r.ID)
	}

	added, err := s.SeedRoles([]Role{{ID: "b", Name: "B", GPT: "g", SoVITS: "v", Ref: "r"}})
	if err != nil || added != 1 {
		t.Fatalf("SeedRoles added %d err=%v, want 1", added, err)
	}
	if r, err := s.ResolveCurrentRole(); err != nil || !IsBuiltInRole(r.ID) {
		t.Errorf("seeding a role auto-selected %q err=%v; it must stay on the built-in voice", r.ID, err)
	}
}

// TestWavDuration covers the parser against the shapes real engines emit, including
// a LIST chunk before the data, which is what ffmpeg-written files carry.
func TestWavDuration(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want int
	}{
		{"one second", wav(22050, 22050), 1000},
		{"half second", wav(11025, 22050), 500},
		{"48k", wav(48000, 48000), 1000},
		{"garbage", []byte("not a wav at all"), 0},
		{"empty", nil, 0},
		{"header only", wav(0, 22050), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wavDurationMS(tc.data); got != tc.want {
				t.Errorf("wavDurationMS = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestWavDurationSurvivesTrailingChunk guards the loop against a truncation that a
// naive reader turns into a hang.
func TestWavDurationSurvivesTrailingChunk(t *testing.T) {
	data := wav(22050, 22050)
	// A trailing chunk whose declared size runs past the end of the buffer.
	trailer := make([]byte, 12)
	copy(trailer[0:], "LIST")
	binary.LittleEndian.PutUint32(trailer[4:], 0xFFFFFF)
	if got := wavDurationMS(append(data, trailer...)); got != 1000 {
		t.Errorf("wavDurationMS = %d, want 1000 (it must still find the data chunk)", got)
	}
}

// TestSlugKeepsCJK is load-bearing: the character names ARE non-ASCII, so stripping
// them would leave every role with an empty ID and collide them all together.
//
// ASCII punctuation that would confuse a file system or a URL is replaced, while
// CJK - including its full-width punctuation, which is safe in a file name and keeps
// the ID readable - is preserved.
func TestSlugKeepsCJK(t *testing.T) {
	cases := map[string]string{
		"klee":          "klee",
		"可莉":            "可莉",
		"珊瑚宫心海（中/日/英）":  "珊瑚宫心海（中-日-英）",
		"  Nahida  ":    "nahida",
		"a b":           "a-b",
		"../etc/passwd": "etc-passwd",
		"":              "",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSlugProducesNoPathSeparators is the security-relevant half: a role ID becomes
// a directory name, so a crafted name must not escape the voice directory.
func TestSlugProducesNoPathSeparators(t *testing.T) {
	for _, in := range []string{"../evil", `..\evil`, "a/b", `a\b`, "..", "/", `C:\Windows`} {
		got := slug(in)
		if strings.ContainsAny(got, `/\:`) {
			t.Errorf("slug(%q) = %q, which still contains a path separator", in, got)
		}
		if got == ".." || got == "." {
			t.Errorf("slug(%q) = %q, which is a directory traversal", in, got)
		}
	}
}

// TestRoleValidate names the fields a render actually needs, so the panel can
// explain a failure instead of the engine returning something opaque.
func TestRoleValidate(t *testing.T) {
	good := testRole()
	if err := good.Validate(); err != nil {
		t.Fatalf("a complete role must validate: %v", err)
	}
	for _, broken := range []Role{
		{ID: "", Name: "x", GPT: "g", SoVITS: "v", Ref: "r"},
		{ID: "x", Name: "  ", GPT: "g", SoVITS: "v", Ref: "r"},
		{ID: "x", Name: "n", GPT: "", SoVITS: "v", Ref: "r"},
		{ID: "x", Name: "n", GPT: "g", SoVITS: "", Ref: "r"},
		{ID: "x", Name: "n", GPT: "g", SoVITS: "v", Ref: ""},
	} {
		if err := broken.Validate(); err == nil {
			t.Errorf("role %+v should not validate", broken)
		}
	}
}

// TestAddTakeRejectsEmptyAudio catches the engine's silent-failure mode, where a
// character that cannot be loaded answers with no bytes at all.
func TestAddTakeRejectsEmptyAudio(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.AddTake(LineFocusEnd, nil, testRole(), "台词"); err == nil {
		t.Error("empty audio must be refused rather than stored as a silent clip")
	}
	if len(s.Settings().Lines) != 0 {
		t.Error("a rejected take must not be recorded")
	}
}

// TestDiscoverRoles reads the layout a real GPT-SoVITS install uses, and skips a
// half-copied character rather than offering a role that cannot render.
func TestDiscoverRoles(t *testing.T) {
	dir := t.TempDir()
	models := filepath.Join(dir, "models")

	full := filepath.Join(models, "klee")
	os.MkdirAll(full, 0o755)
	for _, f := range []string{"gpt.ckpt", "sovits.pth", "ref.wav"} {
		os.WriteFile(filepath.Join(full, f), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(full, "petpc-config.json"),
		[]byte(`{"ttsCharacter":"可莉","ttsPromptText":"参考文本","ttsLang":"zh"}`), 0o644)

	partial := filepath.Join(models, "half")
	os.MkdirAll(partial, 0o755)
	os.WriteFile(filepath.Join(partial, "gpt.ckpt"), []byte("x"), 0o644)

	os.MkdirAll(filepath.Join(models, "not-a-character"), 0o755)

	roles := DiscoverRoles(dir)
	if len(roles) != 1 {
		t.Fatalf("found %d roles, want 1: %+v", len(roles), roles)
	}
	r := roles[0]
	if r.Name != "可莉" {
		t.Errorf("name %q, want 可莉 (from petpc-config.json)", r.Name)
	}
	if r.PromptText != "参考文本" {
		t.Errorf("prompt text %q was not imported", r.PromptText)
	}
	if r.Lang != "zh" {
		t.Errorf("lang %q, want zh", r.Lang)
	}
	if r.TopK == 0 || r.Speed == 0 {
		t.Errorf("discovered role was not normalised: %+v", r)
	}
}

// TestLineStatesReportsMissingClip keeps the panel honest: it must not claim a
// replaced line is in use when playback will actually fail.
//
// Chosen deliberately KEEPS its value here rather than being zeroed: it is the line's
// adopted version, which is still true - the row can say which take went missing
// instead of losing that fact. What must change is the EFFECT, which is what the
// panel shows as "what plays now".
func TestLineStatesReportsMissingClip(t *testing.T) {
	s := withRole(t)
	take, err := s.AddTake(LineFocusEnd, wav(1000, 22050), testRole(), "台词")
	if err != nil {
		t.Fatalf("AddTake: %v", err)
	}
	os.Remove(filepath.Join(s.lineDir(LineFocusEnd), take.File)) //nolint:errcheck

	states := s.LineStates(map[Line]bool{LineFocusEnd: true})
	var found bool
	for _, st := range states {
		if st.Line == LineFocusEnd {
			found = true
			if st.Chosen != take.Version {
				t.Errorf("Chosen = %d, want %d (the adoption is still recorded)", st.Chosen, take.Version)
			}
			if !st.Missing {
				t.Error("Missing should be set so the panel can explain it")
			}
			if st.ChosenForActiveRole {
				t.Error("a clip whose file is gone must not count as playing for the current character")
			}
			if st.EffectSource != "missing" {
				t.Errorf("effect source %q, want missing", st.EffectSource)
			}
			if len(st.Takes) != 1 {
				t.Errorf("%d takes reported, want 1 (the entry is still there)", len(st.Takes))
			}
		}
	}
	if !found {
		t.Fatal("the line is not in LineStates at all")
	}
}

// TestLineStatesCoversEveryLine means the studio always shows a row per line, so a
// line that was never rendered is still visible and can be rendered.
func TestLineStatesCoversEveryLine(t *testing.T) {
	s := newTestStore(t)
	states := s.LineStates(map[Line]bool{LineFocusEnd: true})
	if len(states) != len(AllLines) {
		t.Fatalf("%d rows, want one per declared line (%d)", len(states), len(AllLines))
	}
	for _, st := range states {
		if st.Text == "" {
			t.Errorf("line %s has no text to render or display", st.Line)
		}
		if st.Label == "" {
			t.Errorf("line %s has no label for the panel", st.Line)
		}
		if st.Embedded != (st.Line == LineFocusEnd) {
			t.Errorf("line %s reports embedded=%v", st.Line, st.Embedded)
		}
	}
}

// TestTextsCoverEveryLine is the check that keeps a new line from being added to
// AllLines without a sentence to say - which would render as silence.
func TestTextsCoverEveryLine(t *testing.T) {
	for _, line := range AllLines {
		if Text(line) == "" {
			t.Errorf("line %q has no text", line)
		}
		if Labels[line] == "" {
			t.Errorf("line %q has no label", line)
		}
		if !Known(line) {
			t.Errorf("line %q is not known to Known()", line)
		}
	}
	if Known("nonsense") {
		t.Error("Known accepted a line that does not exist")
	}
	if Text("nonsense") != "" {
		t.Error("Text returned something for a line that does not exist")
	}
}
