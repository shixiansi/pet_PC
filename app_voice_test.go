package main

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"PetPC/internal/store"
	"PetPC/internal/voice"
)

// recordingVoice captures plays instead of touching an audio device.
type recordingVoice struct {
	mu     sync.Mutex
	played []string
	stops  int
	fail   error
}

func (r *recordingVoice) PlayWAVFile(path string) error {
	r.mu.Lock()
	r.played = append(r.played, path)
	fail := r.fail
	r.mu.Unlock()
	return fail
}

func (r *recordingVoice) StopAudio() {
	r.mu.Lock()
	r.stops++
	r.mu.Unlock()
}

func (r *recordingVoice) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.played)
}

// last returns the most recently played path, or "" when nothing was played.
func (r *recordingVoice) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.played) == 0 {
		return ""
	}
	return r.played[len(r.played)-1]
}

// names returns which lines were played, by matching the hashed file name back to
// the line it came from.
func (r *recordingVoice) lines(t *testing.T) []voice.Line {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []voice.Line
	for _, p := range r.played {
		base := filepath.Base(p)
		found := false
		for _, l := range voice.AllLines {
			if strings.HasPrefix(base, "petpc-"+string(l)+"-") {
				out = append(out, l)
				found = true
				break
			}
		}
		if !found {
			t.Errorf("played %q, which matches no known line", base)
		}
	}
	return out
}

// newVoiceTestApp returns an app whose voice player is a recorder.
func newVoiceTestApp(t *testing.T, tune func(*store.Config)) (*App, *recordingVoice) {
	t.Helper()
	a, _ := newLLMTestAppWithEvents(t)

	rec := &recordingVoice{}
	a.voice.v = voice.New(rec, t.TempDir())

	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.VoiceEnabled = true
		c.VoicePomodoro = true
		c.VoiceReminder = true
		if tune != nil {
			tune(c)
		}
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	return a, rec
}

// TestPomodoroPhaseEndIsSpoken covers the feature: when a phase ends, the pet says
// so, using the pre-rendered clip for that direction.
//
// The two directions must be DIFFERENT clips: an announcement that sounds the same
// either way tells the user nothing, which defeats the point of speaking at all.
func TestPomodoroPhaseEndIsSpoken(t *testing.T) {
	a, rec := newVoiceTestApp(t, nil)

	a.speakPomodoroLine(store.PhaseFocus)
	if got := rec.lines(t); len(got) != 1 || got[0] != voice.LineFocusEnd {
		t.Fatalf("focus end played %v, want [%s]", got, voice.LineFocusEnd)
	}

	a.speakPomodoroLine(store.PhaseShortBreak)
	got := rec.lines(t)
	if len(got) != 2 || got[1] != voice.LineBreakEnd {
		t.Fatalf("break end played %v, want the break clip second", got)
	}
}

// TestPomodoroNoticeRespectsItsOwnSwitch is the point of a separate switch: a user
// may run the timer silently.
func TestPomodoroNoticeRespectsItsOwnSwitch(t *testing.T) {
	a, rec := newVoiceTestApp(t, func(c *store.Config) {
		c.VoicePomodoro = false
	})
	a.speakPomodoroLine(store.PhaseFocus)
	if n := rec.count(); n != 0 {
		t.Errorf("spoke the pomodoro notice with its switch off (%d plays)", n)
	}
}

// TestReminderNoticeRespectsItsOwnSwitch covers the reminder half, which defaults
// OFF: a reminder can fire mid-meeting and the pet talking unprompted is far more
// intrusive than a bubble appearing.
func TestReminderNoticeRespectsItsOwnSwitch(t *testing.T) {
	a, rec := newVoiceTestApp(t, func(c *store.Config) {
		c.VoiceReminder = false
	})
	a.speakReminderLine(3, 2, 9)
	if n := rec.count(); n != 0 {
		t.Errorf("spoke a reminder with its switch off (%d plays)", n)
	}
}

// TestReminderNoticePicksTheUrgentClip pins the choice of line: an overdue list
// should not sound like a routine one, because the user is not looking at the
// screen when this fires.
func TestReminderNoticePicksTheUrgentClip(t *testing.T) {
	a, rec := newVoiceTestApp(t, nil)

	a.speakReminderLine(0, 2, 5) // overdue
	if got := rec.lines(t); len(got) != 1 || got[0] != voice.LineReminderOverdue {
		t.Fatalf("overdue reminder played %v, want [%s]", got, voice.LineReminderOverdue)
	}

	a.speakReminderLine(3, 0, 5) // due today, not overdue
	got := rec.lines(t)
	if len(got) != 2 || got[1] != voice.LineReminderDue {
		t.Fatalf("due reminder played %v, want [%s] second", got, voice.LineReminderDue)
	}
}

// TestReminderNoticeWithNothingDueUsesItsOwnClip covers the state a user can switch
// on deliberately (RemindWhenEmpty), where "nothing to do" is itself the message.
func TestReminderNoticeWithNothingDueUsesItsOwnClip(t *testing.T) {
	a, rec := newVoiceTestApp(t, nil)
	a.speakReminderLine(0, 0, 0)
	if got := rec.lines(t); len(got) != 1 || got[0] != voice.LineReminderNone {
		t.Fatalf("empty reminder played %v, want [%s]", got, voice.LineReminderNone)
	}
}

// TestVoiceIsInertWhenDisabled is the degradation guarantee: with the master switch
// off, nothing may be played at all.
func TestVoiceIsInertWhenDisabled(t *testing.T) {
	a, rec := newVoiceTestApp(t, func(c *store.Config) {
		c.VoiceEnabled = false
	})
	a.speakPomodoroLine(store.PhaseFocus)
	a.speakReminderLine(1, 1, 1)
	if n := rec.count(); n != 0 {
		t.Errorf("played %d clip(s) with the master switch off", n)
	}
}

// TestVoiceFailureIsNotFatal pins that a playback failure is logged and swallowed.
// The visual notice still works, so refusing to show it because the speakers are
// busy would be strictly worse.
func TestVoiceFailureIsNotFatal(t *testing.T) {
	a, rec := newVoiceTestApp(t, nil)
	rec.mu.Lock()
	rec.fail = os.ErrPermission
	rec.mu.Unlock()

	// Must not panic, and must not propagate: the signature has no error to return.
	a.speakPomodoroLine(store.PhaseFocus)
	if n := rec.count(); n != 1 {
		t.Errorf("play was attempted %d times, want 1", n)
	}
}

// TestStopVoiceReachesThePlayer covers the panel's stop button.
func TestStopVoiceReachesThePlayer(t *testing.T) {
	a, rec := newVoiceTestApp(t, nil)
	a.speakPomodoroLine(store.PhaseFocus)
	a.stopVoice()
	a.stopVoice()
	if rec.stops != 2 {
		t.Errorf("stops = %d, want 2", rec.stops)
	}
}

// TestVoiceViewDefaultsAndSwitches pins the panel's starting state and that the two
// notice defaults are DELIBERATELY different.
func TestVoiceViewDefaultsAndSwitches(t *testing.T) {
	a, _ := newVoiceTestApp(t, func(c *store.Config) {
		// Back to the shipped defaults, so this asserts what a new user sees.
		c.VoiceEnabled = false
		c.VoicePomodoro = true
		c.VoiceReminder = false
	})

	v := a.voiceView()
	if v.Enabled {
		t.Error("voice defaults to on")
	}
	if !v.Pomodoro {
		t.Error("the pomodoro announcement defaults OFF; the user is waiting for that timer")
	}
	if v.Reminder {
		t.Error("the reminder announcement defaults ON; it can fire mid-meeting")
	}
	if v.Lines != len(voice.AllLines) {
		t.Errorf("view reports %d lines, %d are declared", v.Lines, len(voice.AllLines))
	}
	if v.Dir == "" {
		t.Error("the view does not report where the clips live")
	}
}

// TestSaveVoiceSettingsRoundTrip covers the panel's write path AND the return type.
//
// The return type matters: main.js assigns whatever a settings method returns
// straight to its `state`, so a narrower struct there wipes petImageUrl and the pet
// silently reverts to its default picture. That bug was hit once already with the
// speech methods these replaced.
func TestSaveVoiceSettingsRoundTrip(t *testing.T) {
	a, _ := newVoiceTestApp(t, nil)

	st, err := a.SaveVoiceSettings(VoiceSettingsInput{Enabled: false, Pomodoro: false, Reminder: true})
	if err != nil {
		t.Fatalf("SaveVoiceSettings: %v", err)
	}

	cfg := a.store.Config()
	if cfg.VoiceEnabled {
		t.Error("VoiceEnabled did not persist")
	}
	if cfg.VoicePomodoro {
		t.Error("VoicePomodoro did not persist")
	}
	if !cfg.VoiceReminder {
		t.Error("VoiceReminder did not persist")
	}
	// The whole State must come back, with the fields the panel needs.
	if st.Voice.Reminder != true || st.Voice.Pomodoro != false {
		t.Errorf("the view did not echo the switches back: %+v", st.Voice)
	}
	// The State must still carry the pet, or the panel loses its picture.
	if st.PetImageURL == "" && st.Config.PetImageFile != "" {
		t.Error("the returned State has no pet image URL")
	}
}

// TestTestVoiceAuditionsWithMasterOnIgnoringNoticeSwitches covers the audition
// button: its purpose is to let the user HEAR the voice, so a notice switch that is
// off must not silence it.
func TestTestVoiceAuditionsWithMasterOnIgnoringNoticeSwitches(t *testing.T) {
	a, rec := newVoiceTestApp(t, func(c *store.Config) {
		c.VoicePomodoro = false
		c.VoiceReminder = false
	})
	if _, err := a.TestVoice(); err != nil {
		t.Fatalf("TestVoice: %v", err)
	}
	if n := rec.count(); n != 1 {
		t.Errorf("audition played %d clips, want 1", n)
	}
}

// TestTestVoiceRefusesWhenDisabled covers the other half: with nothing switched on
// there is nothing to audition, and reporting success would be a lie.
func TestTestVoiceRefusesWhenDisabled(t *testing.T) {
	a, rec := newVoiceTestApp(t, func(c *store.Config) { c.VoiceEnabled = false })
	if _, err := a.TestVoice(); err == nil {
		t.Error("TestVoice reported success with the master switch off")
	}
	if rec.count() != 0 {
		t.Error("TestVoice played something with the master switch off")
	}
}

// TestInitVoiceReportsMissingClips covers the startup self-check. It cannot fail the
// app, but it must be CALLED: a missing embedded clip is a build problem, and
// finding it at startup beats discovering it when a timer fires.
func TestInitVoiceReportsMissingClips(t *testing.T) {
	a, _ := newVoiceTestApp(t, nil)
	a.initVoice() // must not panic, and must log
}

// --- the current-character wiring -----------------------------------------
//
// The panel's "音色" line used to be a build fact only ("内置 可莉"), which is what
// made a studio selection look like it had not applied. These tests pin the panel
// view to the SAME resolution the player uses, so the two cannot drift apart again.

// withStudio gives a test app a real shared library, so voiceView() reports a
// resolved character rather than a nil store's defaults.
func withStudio(t *testing.T, a *App) *voice.Store {
	t.Helper()
	st, err := voice.Open(filepath.Join(a.store.Dir(), speechDirName))
	if err != nil {
		t.Fatalf("voice.Open: %v", err)
	}
	a.vstudio.mu.Lock()
	a.vstudio.store = st
	a.vstudio.mu.Unlock()
	return st
}

// testWAVBytes builds a minimal but valid mono 16-bit WAV with the given frame
// count, so a take added through the real AddTake carries a real duration.
func testWAVBytes(frames int) []byte {
	dataLen := frames * 2
	buf := make([]byte, 44+dataLen)
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataLen))
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:], 1) // mono
	binary.LittleEndian.PutUint32(buf[24:], 22050)
	binary.LittleEndian.PutUint32(buf[28:], 44100)
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataLen))
	return buf
}

// TestVoiceViewReportsTheBuiltInVoiceWhenNothingIsChosen covers a fresh install: no
// character has been selected, so the panel must name the built-in character and say
// it IS the built-in voice (the flag, not the name, is what distinguishes them).
func TestVoiceViewReportsTheBuiltInVoiceWhenNothingIsChosen(t *testing.T) {
	a, _ := newVoiceTestApp(t, nil)
	withStudio(t, a)

	v := a.voiceView()
	if !v.ActiveRoleIsBuiltIn {
		t.Error("with no character chosen the panel must report the built-in voice")
	}
	if v.ActiveRoleName != voice.BuiltInRole {
		t.Errorf("panel names %q, want the built-in character %q", v.ActiveRoleName, voice.BuiltInRole)
	}
	if v.RoleError != "" {
		t.Errorf("a fresh install reported a role error: %q", v.RoleError)
	}
}

// TestVoiceViewFollowsTheStudioSelection is the acceptance criterion "界面选中态 =
// 运行时音色": after the studio selects a character, the pet's panel must report
// THAT character, without a restart.
func TestVoiceViewFollowsTheStudioSelection(t *testing.T) {
	a, _ := newVoiceTestApp(t, nil)
	st := withStudio(t, a)

	nahida := voice.Role{ID: "nahida", Name: "纳西妲", GPT: "g", SoVITS: "v", Ref: "r"}
	if err := st.UpsertRole(nahida); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	// Before selecting: still the built-in voice.
	if v := a.voiceView(); !v.ActiveRoleIsBuiltIn {
		t.Error("adding a character must not change the panel's current voice")
	}

	if err := st.SetActiveRole(nahida.ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	v := a.voiceView()
	if v.ActiveRoleIsBuiltIn {
		t.Error("after selecting 纳西妲 the panel still reports the built-in voice")
	}
	if v.ActiveRoleName != "纳西妲" {
		t.Errorf("panel names %q, want 纳西妲", v.ActiveRoleName)
	}
	if v.ActiveRole != nahida.ID {
		t.Errorf("panel reports role id %q, want %q", v.ActiveRole, nahida.ID)
	}
}

// TestVoiceViewSurfacesAStoredCharacterThatVanished is the "失败可见" requirement: a
// character that no longer exists must be REPORTED, not silently replaced by the
// built-in voice or by another role.
func TestVoiceViewSurfacesAStoredCharacterThatVanished(t *testing.T) {
	a, _ := newVoiceTestApp(t, nil)
	st := withStudio(t, a)

	role := voice.Role{ID: "gone", Name: "已删除的角色", GPT: "g", SoVITS: "v", Ref: "r"}
	if err := st.UpsertRole(role); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := st.SetActiveRole(role.ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	if err := st.DeleteRole(role.ID); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}

	// Deleting the current character leaves NO selection, which resolves to the
	// built-in voice - a working state, not an error.
	v := a.voiceView()
	if v.RoleError != "" {
		t.Errorf("deleting the current character produced a role error: %q", v.RoleError)
	}
	if !v.ActiveRoleIsBuiltIn {
		t.Error("after deleting the current character the panel must fall back to the built-in voice")
	}

	// A DANGLING id - what an older build or a hand edit leaves behind - is a real
	// fault and must be reported. Written straight into the index, because that is
	// the only way this state can arise: SetActiveRole refuses to create it.
	path := filepath.Join(st.Dir(), "voice-user.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse index: %v", err)
	}
	raw["activeRole"] = "ghost"
	out, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	v = a.voiceView()
	if v.RoleError == "" {
		t.Error("a stored character that does not exist must be reported to the panel")
	}
	// And the panel must NOT simultaneously claim the built-in voice is in effect:
	// in this state nothing plays, so a working-sounding answer would hide the fault.
	if v.ActiveRoleIsBuiltIn {
		t.Error("the panel claims the built-in voice is in effect while the stored character is broken")
	}
	if v.ActiveRoleName != "" {
		t.Errorf("the panel names %q while the stored character is broken; want no name", v.ActiveRoleName)
	}
}

// TestVoiceViewReplacedCountUsesTheCurrentCharacter means the panel's "已换成该音色的
// N 条" cannot claim lines the pet will not speak. A take adopted by a DIFFERENT
// character must not be counted.
func TestVoiceViewReplacedCountUsesTheCurrentCharacter(t *testing.T) {
	a, rec := newVoiceTestApp(t, nil)
	st := withStudio(t, a)
	// The player must use the SAME shared library, or playback and the panel would
	// be reading different stores and this test would prove nothing.
	a.voice.v = voice.NewWithUser(rec, filepath.Join(a.store.Dir(), speechDirName), st)

	klee := voice.Role{ID: "klee", Name: "可莉", GPT: "g", SoVITS: "v", Ref: "r"}
	nahida := voice.Role{ID: "nahida", Name: "纳西妲", GPT: "g2", SoVITS: "v2", Ref: "r2"}
	for _, r := range []voice.Role{klee, nahida} {
		if err := st.UpsertRole(r); err != nil {
			t.Fatalf("UpsertRole: %v", err)
		}
	}
	if err := st.SetActiveRole(klee.ID); err != nil {
		t.Fatalf("SetActiveRole: %v", err)
	}
	if _, err := st.AddTake(voice.LineFocusEnd, testWAVBytes(22050), klee, voice.Text(voice.LineFocusEnd)); err != nil {
		t.Fatalf("AddTake: %v", err)
	}

	// 可莉 has one line; the panel must say so.
	if v := a.voiceView(); v.Replaced != 1 {
		t.Errorf("with 可莉 selected, %d lines counted as replaced; want 1", v.Replaced)
	}

	// Switching to 纳西妲, who has recorded nothing, must drop the count to zero
	// rather than inheriting 可莉's take.
	if err := st.SetActiveRole(nahida.ID); err != nil {
		t.Fatalf("SetActiveRole nahida: %v", err)
	}
	if v := a.voiceView(); v.Replaced != 0 {
		t.Errorf("with 纳西妲 selected, %d lines still counted as replaced; want 0", v.Replaced)
	}
}

// TestSpeakFixedUsesTheSelectedCharacter is the acceptance criterion stated as
// behaviour rather than as a view field: what the pet PLAYS must follow the selection.
func TestSpeakFixedUsesTheSelectedCharacter(t *testing.T) {
	a, rec := newVoiceTestApp(t, nil)
	st := withStudio(t, a)
	a.voice.v = voice.NewWithUser(rec, filepath.Join(a.store.Dir(), speechDirName), st)

	klee := voice.Role{ID: "klee", Name: "可莉", GPT: "g", SoVITS: "v", Ref: "r"}
	nahida := voice.Role{ID: "nahida", Name: "纳西妲", GPT: "g2", SoVITS: "v2", Ref: "r2"}
	for _, r := range []voice.Role{klee, nahida} {
		if err := st.UpsertRole(r); err != nil {
			t.Fatalf("UpsertRole: %v", err)
		}
	}
	if err := st.SetActiveRole(klee.ID); err != nil {
		t.Fatalf("SetActiveRole klee: %v", err)
	}
	kleeTake, err := st.AddTake(voice.LineFocusEnd, testWAVBytes(22050), klee, voice.Text(voice.LineFocusEnd))
	if err != nil {
		t.Fatalf("AddTake klee: %v", err)
	}

	a.speakPomodoroLine(store.PhaseFocus)
	if got := filepath.Base(rec.last()); got != kleeTake.File {
		t.Fatalf("spoke %q as 可莉, want %q", got, kleeTake.File)
	}

	// A character with no take for this line must NOT fall back to 可莉's clip. The
	// failure is logged (a bubble about audio would interrupt the notice), so the
	// assertion is that nothing new was played.
	if err := st.SetActiveRole(nahida.ID); err != nil {
		t.Fatalf("SetActiveRole nahida: %v", err)
	}
	before := rec.count()
	a.speakPomodoroLine(store.PhaseFocus)
	if rec.count() != before {
		t.Errorf("spoke %q while 纳西妲 has no take for this line; it must stay silent",
			filepath.Base(rec.last()))
	}
}

// TestVoiceViewNeverCarriesACredential is a shape check on the public view: the
// fixed voice has no endpoint and no key, and a future field that looks like one
// should be caught here rather than shipped to the frontend.
func TestVoiceViewNeverCarriesACredential(t *testing.T) {
	a, _ := newVoiceTestApp(t, nil)
	v := a.voiceView()

	typ := reflect.TypeOf(v)
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		lower := strings.ToLower(name)
		for _, banned := range []string{"key", "token", "secret", "password"} {
			if strings.Contains(lower, banned) {
				t.Errorf("VoiceView carries %q, which looks credential-shaped", name)
			}
		}
	}
}
