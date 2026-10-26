package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// voiceManifestForMigration is the declaration the migration requires on disk
// before it will run.
//
// The `Config` list is the three engine paths the migration seeds, and it is here rather
// than omitted because the config store REFUSES an undeclared key: a fixture without
// them made the migration's config step fail with "插件未声明配置项 gptPath" - which is the
// store working correctly and the fixture being wrong. Keeping the real keys here means
// the test exercises the same seam the plugin does.
func voiceManifestForMigration() Manifest {
	return Manifest{
		ID:          VoicePluginID,
		Name:        "语音插件",
		Version:     "1.0.0",
		Author:      "shixiansi",
		Description: "语音插件",
		Permissions: []string{"audio", "events", "storage", "plugin.config", "net.fetch"},
		Background:  true,
		NetHosts:    []string{"127.0.0.1:9880"},
		Assets:      []string{"lines/focus_end.wav"},
		Entry:       "panel.html",
		Config: []ConfigItem{
			{Key: "ttsBaseUrl", Label: "语音服务地址", Type: "text", Default: "http://127.0.0.1:9880", HostWhitelist: true},
			{Key: "gptPath", Label: "GPT 权重路径", Type: "text"},
			{Key: "sovitsPath", Label: "SoVITS 权重路径", Type: "text"},
			{Key: "refPath", Label: "参考音频路径", Type: "text"},
		},
	}
}

// seedVoiceInstall writes the voice plugin's directory, manifest and private
// directory: the state the migration requires before it will run.
func seedVoiceInstall(t *testing.T, dataDir string) string {
	t.Helper()
	pluginDir := filepath.Join(dataDir, "plugins", VoicePluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	data, err := json.Marshal(voiceManifestForMigration())
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, ManifestName), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	privateDir := filepath.Join(dataDir, "plugin-data", VoicePluginID)
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatalf("mkdir private: %v", err)
	}
	return privateDir
}

// voiceLegacyFixture builds a legacy library shaped like the real one: five roles,
// several takes per line across DIFFERENT characters, and a 1-based `chosen` index.
//
// It is built from a struct rather than a string so the test cannot drift from the
// format: this is the file the studio wrote, and the migration must read it as-is.
type voiceLegacyFixture struct {
	dir     string
	clips   map[string][]byte
	library legacyVoiceLibrary
}

func seedVoiceLegacy(t *testing.T, dataDir string, activeRole string) voiceLegacyFixture {
	t.Helper()
	fx := voiceLegacyFixture{clips: map[string][]byte{}}

	role := func(id, name string) legacyVoiceRole {
		return legacyVoiceRole{
			ID: id, Name: name,
			GPT:    `D:\GPT-SoVITS\models\` + id + `\gpt.ckpt`,
			SoVITS: `D:\GPT-SoVITS\models\` + id + `\sovits.pth`,
			Ref:    `D:\GPT-SoVITS\models\` + id + `\ref.wav`,
		}
	}
	fx.library = legacyVoiceLibrary{
		ActiveRole: activeRole,
		Roles: []legacyVoiceRole{
			role("klee", "可莉"),
			role("yoimiya", "宵宫"),
			role("nahida", "纳西妲"),
		},
		Lines: map[string]legacyVoiceSlot{},
	}

	// Two takes for focus_end: the FIRST is 纳西妲 and the chosen index points at the
	// SECOND, which is 宵宫. That mixture is the case worth testing - a migration
	// that ignored `chosen` would select the wrong character, and one that ignored
	// the role would too.
	fx.addTake(t, dataDir, "focus_end", 1, "nahida", "纳西妲", 11)
	fx.addTake(t, dataDir, "focus_end", 2, "yoimiya", "宵宫", 22)
	// `chosen` is set on the slot the adds produced. Assigning a fresh slot here
	// would DISCARD the takes just added, and the migration would then have nothing
	// to choose between - which is exactly the mistake this comment prevents.
	fx.setChosen("focus_end", 2)

	// One take for break_end, already 宵宫 and chosen.
	fx.addTake(t, dataDir, "break_end", 1, "yoimiya", "宵宫", 33)
	fx.setChosen("break_end", 1)

	// reminder_due: the chosen index points at a take of a DIFFERENT character than
	// the active one, which the studio permitted. The migration must fall back to the
	// active character's take rather than copying a voice the user is not using.
	fx.addTake(t, dataDir, "reminder_due", 1, "nahida", "纳西妲", 44)
	fx.addTake(t, dataDir, "reminder_due", 2, "yoimiya", "宵宫", 55)
	fx.setChosen("reminder_due", 1)

	// A line with NO takes at all: the user never generated it. It must not appear in
	// the migrated settings, and must not stop the rest.
	fx.library.Lines["reminder_none"] = legacyVoiceSlot{}

	writeLegacyVoiceLibrary(t, dataDir, fx.library)
	fx.dir = filepath.Join(dataDir, voiceMigrateDir, "user")
	return fx
}

// setChosen records which take is in effect for a line, WITHOUT disturbing the
// takes already registered for it.
func (fx *voiceLegacyFixture) setChosen(lineID string, index int) {
	slot := fx.library.Lines[lineID]
	slot.Chosen = index
	fx.library.Lines[lineID] = slot
}

// addTake registers one take in the fixture and writes its clip to disk.
func (fx *voiceLegacyFixture) addTake(t *testing.T, dataDir, lineID string, version int, roleID, roleName string, fill byte) {
	t.Helper()
	file := voiceSlotName(lineID, legacyVoiceTake{Version: version}) + ".wav"
	// The file name on disk is the studio's ("v001.wav"), NOT the migration's slot
	// name - that difference is the whole point of testing the copy.
	diskName := vName(version)
	_ = file
	raw := make([]byte, 2048)
	copy(raw, "RIFF")
	for i := 12; i < len(raw); i++ {
		raw[i] = fill
	}
	fx.clips[lineID+"/"+diskName] = raw

	slot := fx.library.Lines[lineID]
	slot.Takes = append(slot.Takes, legacyVoiceTake{
		Version:    version,
		Role:       roleID,
		RoleName:   roleName,
		Text:       "第 " + lineID + " 句",
		File:       diskName,
		Bytes:      int64(len(raw)),
		DurationMS: 1234,
	})
	fx.library.Lines[lineID] = slot

	dir := filepath.Join(dataDir, voiceMigrateDir, "user", "clips", lineID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir clips: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, diskName), raw, 0o644); err != nil {
		t.Fatalf("write clip: %v", err)
	}
}

// vName is the studio's file naming for one take version.
func vName(version int) string {
	return "v" + pad3(version) + ".wav"
}

func pad3(n int) string {
	s := "000"
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	if digits == "" {
		return s
	}
	return s[:len(s)-len(digits)] + digits
}

func writeLegacyVoiceLibrary(t *testing.T, dataDir string, lib legacyVoiceLibrary) {
	t.Helper()
	dir := filepath.Join(dataDir, voiceMigrateDir, "user")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		t.Fatalf("marshal library: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "voice-user.json"), raw, 0o644); err != nil {
		t.Fatalf("write library: %v", err)
	}
}

// TestVoiceMigrationCarriesTheEnginePaths covers the gap that made the workshop's
// 生成 form blank right after a migration.
//
// The clips are what PLAYS; the three weight paths are what lets the user render MORE.
// Carrying only the first left a plugin holding 12 playable clips and no record of which
// gpt/sovits/ref produced them - and since every take in the old library carried its
// `params`, those three paths were the last copy of that fact. Losing them meant
// retyping three paths per character from memory to render one more line.
//
// The paths are stored through ConfigStore, so this also asserts the SEAM: the manifest
// the migration reads must be the plugin's real one, because the store drops every key a
// manifest does not declare - a mismatch would store nothing at all and look like a
// silent no-op.
func TestVoiceMigrationCarriesTheEnginePaths(t *testing.T) {
	dataDir := t.TempDir()
	privateDir := seedVoiceInstall(t, dataDir)
	seedVoiceLegacy(t, dataDir, "yoimiya")

	if err := MigrateVoiceData(dataDir, newFakeKeeper()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Read it back the way the MARKET does, through the same store: a hand-rolled JSON
	// read here would not prove the editor can see these values.
	man, err := LoadManifest(filepath.Join(dataDir, "plugins", VoicePluginID))
	if err != nil {
		t.Fatalf("load the installed manifest: %v", err)
	}
	store := NewConfigStore(privateDir, man, newFakeKeeper())
	values := store.Values()

	want := map[string]string{
		"gptPath":    `D:\GPT-SoVITS\models\yoimiya\gpt.ckpt`,
		"sovitsPath": `D:\GPT-SoVITS\models\yoimiya\sovits.pth`,
		"refPath":    `D:\GPT-SoVITS\models\yoimiya\ref.wav`,
	}
	for key, expected := range want {
		if got := values[key]; got != expected {
			t.Errorf("%s = %q, want %q (the ACTIVE character's path)", key, got, expected)
		}
	}
	// The active character is 宵宫; a migration that took Roles[0] would write 可莉's
	// paths, and the rendered audio would then come out in the wrong voice.
	if values["gptPath"] == `D:\GPT-SoVITS\models\klee\gpt.ckpt` {
		t.Error("the migration seeded the FIRST character's paths instead of the active one's")
	}
}

// TestVoiceMigrationDoesNotOverwriteTheUsersEnginePaths pins the merge rule.
//
// The migration runs at startup, so a user who already filled the form in must not have
// their values replaced by whatever the old library happened to say.
func TestVoiceMigrationDoesNotOverwriteTheUsersEnginePaths(t *testing.T) {
	dataDir := t.TempDir()
	privateDir := seedVoiceInstall(t, dataDir)
	seedVoiceLegacy(t, dataDir, "yoimiya")

	// The user edited the form before the migration ran.
	man, err := LoadManifest(filepath.Join(dataDir, "plugins", VoicePluginID))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	const mine = `E:\my-own\gpt.ckpt`
	if err := NewConfigStore(privateDir, man, newFakeKeeper()).
		Write(map[string]string{"gptPath": mine}); err != nil {
		t.Fatalf("seed the user's config: %v", err)
	}

	if err := MigrateVoiceData(dataDir, newFakeKeeper()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	values := NewConfigStore(privateDir, man, newFakeKeeper()).Values()
	if values["gptPath"] != mine {
		t.Errorf("gptPath = %q, want the user's own value %q (migration overwrote it)",
			values["gptPath"], mine)
	}
	// The fields the user had NOT set are still filled in: the merge is per-key.
	if values["sovitsPath"] == "" {
		t.Error("the migration skipped the keys the user had not set")
	}
}

// TestVoiceMigrationCarriesNoEnginePathsWhenTheRoleHasNone pins the empty case.
//
// Writing blanks would be indistinguishable from the user clearing the form, and the
// plugin would show three empty boxes as if nothing had been carried.
func TestVoiceMigrationCarriesNoEnginePathsWhenTheRoleHasNone(t *testing.T) {
	dataDir := t.TempDir()
	privateDir := seedVoiceInstall(t, dataDir)
	seedVoiceLegacy(t, dataDir, "yoimiya")

	// Strip the paths from every role, as a library that never recorded them.
	fx := readLegacyVoiceLibraryForTest(t, dataDir)
	for i := range fx.Roles {
		fx.Roles[i].GPT, fx.Roles[i].SoVITS, fx.Roles[i].Ref = "", "", ""
	}
	writeLegacyVoiceLibrary(t, dataDir, *fx)

	if err := MigrateVoiceData(dataDir, newFakeKeeper()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	man, err := LoadManifest(filepath.Join(dataDir, "plugins", VoicePluginID))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	values := NewConfigStore(privateDir, man, newFakeKeeper()).Values()
	for _, key := range []string{"gptPath", "sovitsPath", "refPath"} {
		if _, present := values[key]; present {
			t.Errorf("%s was written as %q although the old library had no such path",
				key, values[key])
		}
	}
}

func readLegacyVoiceLibraryForTest(t *testing.T, dataDir string) *legacyVoiceLibrary {
	t.Helper()
	lib, err := readLegacyVoiceLibrary(dataDir)
	if err != nil {
		t.Fatalf("read the legacy library: %v", err)
	}
	if lib == nil {
		t.Fatal("the fixture wrote no legacy library")
	}
	return lib
}

// TestVoiceMigrationCarriesTheActiveCharactersRenders is the core guarantee: the
// clips the user is HEARING today are the ones that arrive, under the names the
// plugin will look for.
func TestVoiceMigrationCarriesTheActiveCharactersRenders(t *testing.T) {
	dataDir := t.TempDir()
	privateDir := seedVoiceInstall(t, dataDir)
	fx := seedVoiceLegacy(t, dataDir, "yoimiya")

	if err := MigrateVoiceData(dataDir, newFakeKeeper()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	settings := readVoiceSettings(t, privateDir)

	// The switches came from the pet's defaults (no config.json in this fixture).
	if settings.Enabled || !settings.Pomodoro || settings.Reminder {
		t.Fatalf("the switches did not survive: %+v", settings)
	}
	if settings.ActiveRole != "yoimiya" {
		t.Fatalf("activeRole = %q, want yoimiya", settings.ActiveRole)
	}
	if settings.RoleNames["yoimiya"] != "宵宫" {
		t.Fatalf("the role names did not survive: %+v", settings.RoleNames)
	}

	// THE assertion: every line the user has a 宵宫 render for is pointed at that
	// render, and the file is byte-for-byte the source.
	for _, lineID := range []string{"focus_end", "break_end", "reminder_due"} {
		name := settings.Chosen[lineID]
		if name == "" {
			t.Fatalf("%s has no chosen take after migration", lineID)
		}
		takes := settings.Takes[lineID]
		if len(takes) == 0 {
			t.Fatalf("%s has no take metadata after migration", lineID)
		}
		var found *voicePluginTake
		for i := range takes {
			if takes[i].Name == name {
				found = &takes[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("%s chose %q, which is not among its takes %v", lineID, name, takes)
		}
		if found.Role != "yoimiya" {
			t.Fatalf("%s chose a %s take; the active character is yoimiya", lineID, found.Role)
		}

		got, err := os.ReadFile(filepath.Join(privateDir, "audio", name+".wav"))
		if err != nil {
			t.Fatalf("%s: the chosen clip was not copied: %v", lineID, err)
		}
		srcName := takeDiskName(fx.library, lineID, name)
		want, err := os.ReadFile(filepath.Join(fx.dir, "clips", lineID, srcName))
		if err != nil {
			t.Fatalf("%s: read the source clip: %v", lineID, err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s: the copied clip differs from the source (%d vs %d bytes)", lineID, len(got), len(want))
		}
	}

	// A line with no takes must be absent rather than pointed at nothing.
	if _, ok := settings.Chosen["reminder_none"]; ok {
		t.Error("a line with no renders was given a chosen take")
	}

	// EVERY take is carried, not just the chosen ones: the workshop switches between
	// characters, so the 纳西妲 render is as much a part of the library as the 宵宫 one.
	if len(settings.Takes["focus_end"]) != 2 {
		t.Fatalf("focus_end carried %d takes, want both", len(settings.Takes["focus_end"]))
	}

	// The source is untouched, which is the rollback path.
	if _, err := os.Stat(filepath.Join(fx.dir, "voice-user.json")); err != nil {
		t.Fatalf("the source library was disturbed: %v", err)
	}
	for rel := range fx.clips {
		if _, err := os.Stat(filepath.Join(fx.dir, "clips", filepath.FromSlash(rel))); err != nil {
			t.Fatalf("the source clip %s was disturbed: %v", rel, err)
		}
	}
}

// takeDiskName maps a migrated slot name back to the studio's file name, so the
// comparison above is against the real source bytes.
func takeDiskName(lib legacyVoiceLibrary, lineID, slot string) string {
	for _, take := range lib.Lines[lineID].Takes {
		if voiceSlotName(lineID, take) == slot {
			return take.File
		}
	}
	return ""
}

func readVoiceSettings(t *testing.T, privateDir string) voicePluginSettings {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(privateDir, voiceStorageKey))
	if err != nil {
		t.Fatalf("read the migrated settings: %v", err)
	}
	var out voicePluginSettings
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse the migrated settings: %v", err)
	}
	return out
}

// TestVoiceMigrationIsIdempotentAndMarksItself pins the marker: a second run must
// change nothing, and must not re-read the source.
func TestVoiceMigrationIsIdempotentAndMarksItself(t *testing.T) {
	dataDir := t.TempDir()
	privateDir := seedVoiceInstall(t, dataDir)
	seedVoiceLegacy(t, dataDir, "yoimiya")

	if err := MigrateVoiceData(dataDir, newFakeKeeper()); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	markerPath := filepath.Join(privateDir, migrateMarkerName)
	first, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("the marker was not written: %v", err)
	}
	settingsPath := filepath.Join(privateDir, voiceStorageKey)
	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	// Delete a copied clip, then run again: a migration that re-ran would put it back.
	victim := filepath.Join(privateDir, "audio", "break_end-v001.wav")
	if err := os.Remove(victim); err != nil {
		t.Fatalf("remove a copied clip: %v", err)
	}
	if err := MigrateVoiceData(dataDir, newFakeKeeper()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Fatal("the second run repeated the migration despite the marker")
	}
	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings again: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("the second run rewrote the settings")
	}
	again, _ := os.ReadFile(markerPath)
	if string(first) != string(again) {
		t.Fatal("the marker was rewritten")
	}
}

// TestVoiceMigrationNeedsThePluginInstalled pins that nothing is written when the
// plugin is absent, so the data is picked up by a later launch - which is what makes
// "install the plugin, then restart" work.
func TestVoiceMigrationNeedsThePluginInstalled(t *testing.T) {
	dataDir := t.TempDir()
	seedVoiceLegacy(t, dataDir, "yoimiya")
	// Deliberately NOT installed.

	if err := MigrateVoiceData(dataDir, newFakeKeeper()); err != nil {
		t.Fatalf("migrate without the plugin must be a no-op: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "plugin-data", VoicePluginID)); !os.IsNotExist(err) {
		t.Fatal("the migration created the plugin's private directory")
	}

	// Now install it and run again: the data must arrive.
	seedVoiceInstall(t, dataDir)
	if err := MigrateVoiceData(dataDir, newFakeKeeper()); err != nil {
		t.Fatalf("migrate after installing: %v", err)
	}
	if _, err := os.Stat(VoiceMigrateMarkerPath(dataDir)); err != nil {
		t.Fatalf("the migration did not run after the plugin was installed: %v", err)
	}
}

// TestVoiceMigrationWithNoLegacyLibraryIsANoOp pins the fresh-install path: a user
// who never used the host voice must not get an empty library written over their
// plugin, and must not be marked as migrated.
func TestVoiceMigrationWithNoLegacyLibraryIsANoOp(t *testing.T) {
	dataDir := t.TempDir()
	privateDir := seedVoiceInstall(t, dataDir)

	if err := MigrateVoiceData(dataDir, newFakeKeeper()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(privateDir, voiceStorageKey)); !os.IsNotExist(err) {
		t.Fatal("a settings file was written with no source data")
	}
	if _, err := os.Stat(VoiceMigrateMarkerPath(dataDir)); !os.IsNotExist(err) {
		t.Fatal("a marker was written with no source data, which would skip a later real migration")
	}
}

// TestVoiceMigrationCarriesTheSwitches pins that the pet's three switches become the
// plugin's three switches. Without it the user's "speak on pomodoro" choice would be
// silently reset to the default.
func TestVoiceMigrationCarriesTheSwitches(t *testing.T) {
	dataDir := t.TempDir()
	privateDir := seedVoiceInstall(t, dataDir)
	seedVoiceLegacy(t, dataDir, "yoimiya")

	cfg := map[string]any{
		"voiceEnabled":  true,
		"voicePomodoro": false,
		"voiceReminder": true,
	}
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := MigrateVoiceData(dataDir, newFakeKeeper()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	settings := readVoiceSettings(t, privateDir)
	if !settings.Enabled || settings.Pomodoro || !settings.Reminder {
		t.Fatalf("the switches did not carry: %+v", settings)
	}
}

// TestVoiceSlotNamesAreLegal pins the join between the file name and the capability
// that plays it: audio.play refuses a name outside [A-Za-z0-9_-]. A migration that
// produced a name the host rejects would succeed and never make a sound.
func TestVoiceSlotNamesAreLegal(t *testing.T) {
	for _, take := range []legacyVoiceTake{
		{Version: 1, File: "v001.wav"},
		{Version: 0, File: "weird name (1).wav"},
		{Version: 12345, File: "v12345.wav"},
	} {
		name := voiceSlotName("focus_end", take)
		if name == "" {
			t.Fatalf("an empty slot name for %+v", take)
		}
		for _, r := range name {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '-' || r == '_'
			if !ok {
				t.Fatalf("the slot name %q contains %q, which the audio capability refuses", name, r)
			}
		}
		if len(name) > 64 {
			t.Fatalf("the slot name %q is longer than 64 characters", name)
		}
	}
}

// TestVoiceMigrationSkipsAMissingClipWithoutLosingTheRest pins the per-file
// tolerance: a library that names a file which is gone must still migrate everything
// else. Failing the whole run would leave the user with nothing over one bad entry.
func TestVoiceMigrationSkipsAMissingClipWithoutLosingTheRest(t *testing.T) {
	dataDir := t.TempDir()
	privateDir := seedVoiceInstall(t, dataDir)
	fx := seedVoiceLegacy(t, dataDir, "yoimiya")

	// Remove one source clip that the library still names.
	if err := os.Remove(filepath.Join(fx.dir, "clips", "break_end", "v001.wav")); err != nil {
		t.Fatalf("remove a source clip: %v", err)
	}

	if err := MigrateVoiceData(dataDir, newFakeKeeper()); err != nil {
		t.Fatalf("a missing clip must not fail the migration: %v", err)
	}
	settings := readVoiceSettings(t, privateDir)

	// The line whose clip is gone has no chosen take, and no take metadata...
	if settings.Chosen["break_end"] != "" {
		t.Fatalf("break_end chose %q, whose file is missing", settings.Chosen["break_end"])
	}
	// ...while every other line arrived.
	for _, lineID := range []string{"focus_end", "reminder_due"} {
		if settings.Chosen[lineID] == "" {
			t.Fatalf("%s was lost because of an unrelated missing clip", lineID)
		}
		if _, err := os.Stat(filepath.Join(privateDir, "audio", settings.Chosen[lineID]+".wav")); err != nil {
			t.Fatalf("%s's clip was not copied: %v", lineID, err)
		}
	}
	// And the migration still completed, so it does not retry forever.
	if _, err := os.Stat(VoiceMigrateMarkerPath(dataDir)); err != nil {
		t.Fatalf("the migration did not mark itself complete: %v", err)
	}
}

// TestVoiceMigrationReportsAnUnparseableLibrary pins that a corrupt source is an
// ERROR rather than a silent empty result: an empty plugin looks exactly like "the
// migration worked and your voice is gone".
func TestVoiceMigrationReportsAnUnparseableLibrary(t *testing.T) {
	dataDir := t.TempDir()
	seedVoiceInstall(t, dataDir)
	dir := filepath.Join(dataDir, voiceMigrateDir, "user")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "voice-user.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := MigrateVoiceData(dataDir, newFakeKeeper())
	if err == nil {
		t.Fatal("a corrupt library must be reported")
	}
	if !strings.Contains(err.Error(), "解析") {
		t.Fatalf("the error %q does not say the library could not be parsed", err)
	}
	if _, statErr := os.Stat(VoiceMigrateMarkerPath(dataDir)); !os.IsNotExist(statErr) {
		t.Fatal("a failed migration marked itself complete")
	}
}
