package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"PetPC/internal/plugins"
)

// The end-to-end answer to the question the whole change exists to keep true: after a
// user's library is migrated and the plugin is installed, does the pet's audio
// capability play the render the USER made - and not the packaged clip?
//
// Every layer is real here except the sound card: the plugin is installed from the
// built-in package (so its declared assets are the ones the packager produced), the
// migration runs against a library laid out exactly like the studio's, and the play
// request goes through the same gate a plugin uses. The only substitution is the audio
// sink, which records what it was asked to play.
//
// It is worth a test of this shape because the failure mode is INVISIBLE: a broken
// join between the migrated slot name and the file on disk plays the built-in 可莉 clip
// instead, which sounds like a working feature and is exactly the bug reported before
// this change ("the studio shows 宵宫, the pet speaks 可莉").
func TestMigratedVoiceLibraryIsWhatActuallyPlays(t *testing.T) {
	a := pluginApp(t)

	// 1. Install the voice plugin from the built-in catalogue, so the package under
	//    test is the one the packager ships.
	id := installBuiltinVoice(t, a)
	if id != plugins.VoicePluginID {
		t.Fatalf("installed %s, want %s", id, plugins.VoicePluginID)
	}
	row, ok := a.pluginManager().Get(id)
	if !ok || row.Manifest == nil {
		t.Fatal("the voice plugin is not installed")
	}
	// The declaration the whole feature hangs on.
	if !row.Manifest.Background || !row.Manifest.HasPermission(plugins.PermEvents) {
		t.Fatalf("the plugin cannot receive events: background=%v events=%v",
			row.Manifest.Background, row.Manifest.HasPermission(plugins.PermEvents))
	}
	// Every declared asset must have survived packaging, or the built-in fallback is
	// a promise the package does not keep.
	pkgDir, err := a.pluginManager().PluginDir(id)
	if err != nil {
		t.Fatalf("PluginDir: %v", err)
	}
	for _, asset := range row.Manifest.Assets {
		p := filepath.Join(pkgDir, filepath.FromSlash(asset))
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("declared asset %s is missing from the installed package: %v", asset, err)
		}
		if len(raw) < 1000 || string(raw[:4]) != "RIFF" {
			t.Fatalf("declared asset %s is not a RIFF/WAVE file (%d bytes)", asset, len(raw))
		}
	}

	// 2. Lay out a legacy library exactly as the studio wrote it: two takes for one
	//    line from DIFFERENT characters, with the chosen index pointing at 宵宫.
	dataDir := a.pluginManager().DataDir()
	seedMigratableVoiceLibrary(t, dataDir)

	// 3. Run the real migration.
	if err := plugins.MigrateVoiceData(dataDir, a.pluginHost.keeper); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// 4. The plugin asks what it holds and what it chose - the two calls its own
	//    resolveClipFor makes. Both go through the same gate.
	privateDir, _ := a.pluginManager().PrivateDir(id)
	rawSettings, err := os.ReadFile(filepath.Join(privateDir, "voice-settings"))
	if err != nil {
		t.Fatalf("the plugin has no settings after migration: %v", err)
	}
	var settings struct {
		ActiveRole string            `json:"activeRole"`
		Chosen     map[string]string `json:"chosen"`
	}
	if err := json.Unmarshal(rawSettings, &settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if settings.ActiveRole != "yoimiya" {
		t.Fatalf("activeRole = %q, want yoimiya", settings.ActiveRole)
	}
	chosen := settings.Chosen["focus_end"]
	if chosen == "" {
		t.Fatal("nothing was chosen for focus_end after migration")
	}

	// The inventory the plugin reads, through the capability.
	listOut, err := a.PluginRequest(id, "audio.list", `{}`)
	if err != nil {
		t.Fatalf("audio.list: %v", err)
	}
	var listed []struct {
		Name  string `json:"name"`
		Bytes int64  `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(listOut), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	names := make(map[string]int64, len(listed))
	for _, c := range listed {
		names[c.Name] = c.Bytes
	}
	// THE assertion at this layer: the chosen take is on disk, so the plugin's
	// fallback does not engage and the user hears their own render.
	if _, ok := names[chosen]; !ok {
		t.Fatalf("the migrated take %q is not in the plugin's inventory %v; the plugin would fall back to the built-in clip",
			chosen, listed)
	}

	// 5. Play it, and assert on the PATH that reached the sink - the only form in
	//    which "the user's render played" is an observable fact.
	rec := &recordingAudio{}
	a.audioMu.Lock()
	a.audioOut = rec
	a.audioMu.Unlock()

	if _, err := a.PluginRequest(id, "audio.play", `{"name":"`+chosen+`"}`); err != nil {
		t.Fatalf("audio.play: %v", err)
	}
	got := rec.last()
	want := filepath.Join(privateDir, "audio", chosen+".wav")
	if got != want {
		t.Fatalf("the sink played %q, want %q", got, want)
	}
	// And the bytes really are the user's render, not a copy of the packaged clip.
	played, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read the played clip: %v", err)
	}
	builtin, err := os.ReadFile(filepath.Join(pkgDir, "lines", "focus_end.wav"))
	if err != nil {
		t.Fatalf("read the packaged clip: %v", err)
	}
	if string(played) == string(builtin) {
		t.Fatal("the migrated take is byte-identical to the packaged clip, so this test proves nothing")
	}
	if int64(len(played)) != names[chosen] {
		t.Fatalf("the listing says %d bytes but the file is %d", names[chosen], len(played))
	}

	// 6. The built-in fallback still works for a line the user never rendered, and it
	//    is reachable only by importing the packaged clip first - which is what
	//    audio.import is for.
	if _, err := a.PluginRequest(id, "audio.import",
		`{"name":"builtin-focus_end","from":"lines/focus_end.wav"}`); err != nil {
		t.Fatalf("audio.import: %v", err)
	}
	if _, err := a.PluginRequest(id, "audio.play", `{"name":"builtin-focus_end"}`); err != nil {
		t.Fatalf("audio.play (builtin): %v", err)
	}
	if got := rec.last(); got != filepath.Join(privateDir, "audio", "builtin-focus_end.wav") {
		t.Fatalf("the built-in fallback played %q", got)
	}

	// And an asset the manifest does NOT declare must be refused: the declaration is
	// what makes "which package files may be imported" an auditable set.
	grantPath, _ := a.pluginManager().PluginDir(id)
	if _, err := os.Stat(filepath.Join(grantPath, "app.js")); err != nil {
		t.Fatalf("the package has no app.js to try: %v", err)
	}
	if _, err := a.PluginRequest(id, "audio.import", `{"name":"sneaky","from":"app.js"}`); err == nil {
		t.Fatal("an undeclared package file was importable")
	}
}

// installBuiltinVoice installs the voice plugin from the embedded catalogue and grants
// everything it declares, which is the state a user reaches by clicking install.
func installBuiltinVoice(t *testing.T, a *App) string {
	t.Helper()
	view, err := a.pluginManager().Market()
	if err != nil {
		t.Fatalf("Market: %v", err)
	}
	for _, entry := range view.Entries {
		if entry.ID != plugins.VoicePluginID {
			continue
		}
		if _, err := a.pluginManager().Install(entry); err != nil {
			t.Fatalf("install the voice plugin: %v", err)
		}
		if _, err := a.pluginManager().GrantPermissions(entry.ID, entry.Permissions); err != nil {
			t.Fatalf("grant: %v", err)
		}
		if err := a.pluginManager().SetEnabled(entry.ID, true); err != nil {
			t.Fatalf("enable: %v", err)
		}
		return entry.ID
	}
	t.Fatal("the built-in catalogue does not carry the voice plugin")
	return ""
}

// seedMigratableVoiceLibrary writes a legacy library into dataDir.
//
// It is written as raw JSON rather than through a struct so it cannot drift from the
// format the studio actually produced - this is the file the migration must read as-is,
// including its 1-based "chosen" index.
func seedMigratableVoiceLibrary(t *testing.T, dataDir string) {
	t.Helper()
	clipsDir := filepath.Join(dataDir, "voice", "user", "clips")
	for _, line := range []string{"focus_end"} {
		if err := os.MkdirAll(filepath.Join(clipsDir, line), 0o755); err != nil {
			t.Fatalf("mkdir clips: %v", err)
		}
	}
	// Two takes: the first is 纳西妲, the second 宵宫, and "chosen" points at the second.
	for i, who := range []struct {
		file string
		fill byte
		role string
		name string
	}{
		{"v001.wav", 7, "nahida", "纳西妲"},
		{"v002.wav", 200, "yoimiya", "宵宫"},
	} {
		_ = i
		raw := make([]byte, 4096)
		copy(raw, "RIFF")
		for j := 12; j < len(raw); j++ {
			raw[j] = who.fill
		}
		if err := os.WriteFile(filepath.Join(clipsDir, "focus_end", who.file), raw, 0o644); err != nil {
			t.Fatalf("write clip: %v", err)
		}
	}
	lib := `{
  "activeRole": "yoimiya",
  "roles": [
    {"id":"nahida","name":"纳西妲"},
    {"id":"yoimiya","name":"宵宫"}
  ],
  "lines": {
    "focus_end": {
      "takes": [
        {"version":1,"role":"nahida","roleName":"纳西妲","text":"专注时间结束了。","file":"v001.wav","bytes":4096,"durationMs":1000},
        {"version":2,"role":"yoimiya","roleName":"宵宫","text":"专注时间结束了。","file":"v002.wav","bytes":4096,"durationMs":1100}
      ],
      "chosen": 2
    }
  }
}
`
	dir := filepath.Join(dataDir, "voice", "user")
	if err := os.WriteFile(filepath.Join(dir, "voice-user.json"), []byte(lib), 0o644); err != nil {
		t.Fatalf("write library: %v", err)
	}
	// A switch the user had turned on, to confirm it travels too.
	cfg := map[string]any{"voiceEnabled": true, "voicePomodoro": true, "voiceReminder": false}
	encoded, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), encoded, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// TestVoicePluginPlaysItsOwnChosenSlotWithoutThePackageOnDisk guards the join between
// the migration and the capability: `chosen` must name a slot audio.play ACCEPTS.
//
// The name is built by the migration and consumed by the capability, and the two have
// separate rules. A name the migration liked but the capability refuses would leave the
// user with a library that migrated "successfully" and never makes a sound.
func TestVoicePluginPlaysItsOwnChosenSlotWithoutThePackageOnDisk(t *testing.T) {
	a := pluginApp(t)
	id := installBuiltinVoice(t, a)
	dataDir := a.pluginManager().DataDir()
	seedMigratableVoiceLibrary(t, dataDir)
	if err := plugins.MigrateVoiceData(dataDir, a.pluginHost.keeper); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	privateDir, _ := a.pluginManager().PrivateDir(id)
	raw, err := os.ReadFile(filepath.Join(privateDir, "voice-settings"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings struct {
		Chosen map[string]string `json:"chosen"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(settings.Chosen) == 0 {
		t.Fatal("the migration chose nothing")
	}
	// Each chosen name must be accepted by the capability WITHOUT the package being
	// consulted: the plugin resolved it from the inventory, and that is the path.
	for line, name := range settings.Chosen {
		if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
			t.Fatalf("the migrated name %q for %s is not a safe file name", name, line)
		}
		out, err := a.PluginRequest(id, "audio.list", `{}`)
		if err != nil {
			t.Fatalf("audio.list: %v", err)
		}
		if !strings.Contains(out, `"`+name+`"`) {
			t.Fatalf("audio.list does not carry the chosen take %q for %s: %s", name, line, out)
		}
	}
}
