package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"PetPC/internal/desktop"
	"PetPC/internal/plugins"
	"PetPC/internal/store"
)

// These tests pin the PET's end of the one-time migration: that its startup
// sequence runs it, that it runs against the pet's own data directory with the
// pet's own secret keeper, and that a failure can never stop the pet appearing.
//
// The move itself - what is carried, in what order, and what a failure leaves
// behind - is tested in internal/plugins/migrate_test.go. Testing it again here
// would only re-assert the shared implementation; what is worth pinning here is
// the wiring, which is the part that exists only in this program.

// --- fixtures --------------------------------------------------------------

// chatPluginManifestForMigration is the conversation plugin's declaration, with
// every legacy key in it.
//
// The migration carries a config value only when the target manifest DECLARES
// it, so a fixture that omitted them would make the config half of this test pass
// vacuously.
func chatPluginManifestForMigration() plugins.Manifest {
	return plugins.Manifest{
		ID:          plugins.ChatPluginID,
		Name:        "对话插件",
		Version:     "1.1.0",
		Author:      "shixiansi",
		Description: "把对话搬到插件里的测试清单",
		Permissions: []string{"llm.chat", "pet.bubble", "pet.surface", "plugin.config", "storage"},
		Panel:       true,
		PanelSlot:   plugins.PanelSlotChat,
		Entry:       "panel.html",
		Config: []plugins.ConfigItem{
			{Key: "llmEnabled", Label: "开启对话", Type: "bool"},
			{Key: "llmProvider", Label: "服务商", Type: "text"},
			{Key: "llmBaseUrl", Label: "接口地址", Type: "text", HostWhitelist: true},
			{Key: "llmModel", Label: "模型", Type: "text"},
			{Key: "llmTemperature", Label: "随机性", Type: "number"},
			{Key: "llmMaxTokens", Label: "回复上限", Type: "number"},
			{Key: "llmTimeoutSec", Label: "超时", Type: "number"},
			{Key: "llmSystemPrompt", Label: "人设", Type: "text"},
			{Key: "llmHistoryTurns", Label: "上下文", Type: "number"},
			{Key: "llmContextTodos", Label: "知道待办", Type: "bool"},
			{Key: "llmProactive", Label: "主动说话", Type: "bool"},
			{Key: "llmProactiveMinGapMin", Label: "最小间隔", Type: "number"},
			{Key: "llmDailyBudget", Label: "每日上限", Type: "number"},
			{Key: "llmApiKey", Label: "API Key", Type: "secret", Secret: true},
		},
	}
}

// seedInstalledChatPlugin puts the conversation plugin on disk: its directory
// and manifest, its private directory, and its index entry.
//
// All three are what "installed" means to the rest of the app, and the migration
// reads the manifest directly (it must know WHICH keys to carry), so the fixture
// writes the same shape the installer does rather than going through an install
// over the network.
func seedInstalledChatPlugin(t *testing.T, dataDir string) plugins.Manifest {
	t.Helper()
	man := chatPluginManifestForMigration()

	pluginDir := filepath.Join(dataDir, "plugins", man.ID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	raw, err := json.Marshal(man)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, plugins.ManifestName), raw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "plugin-data", man.ID), 0o755); err != nil {
		t.Fatalf("mkdir private dir: %v", err)
	}

	idx := map[string]any{
		"version": 1,
		"plugins": []map[string]any{{
			"id":          man.ID,
			"version":     man.Version,
			"enabled":     true,
			"granted":     man.Permissions,
			"installedAt": "2026-01-01T00:00:00Z",
		}},
	}
	idxRaw, err := json.Marshal(idx)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "plugins.json"), idxRaw, 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	return man
}

// legacyChatSeed is what the fixture put on disk, so the test can compare the
// bytes afterwards instead of re-deriving them.
type legacyChatSeed struct {
	config  []byte
	history []byte
	usage   []byte
	apiKey  string
}

// seedLegacyChatData writes exactly the state an upgraded install is in: the
// pet's own config.json holding the 13 llm* values, the transcript, the usage
// counter, and the DPAPI-encrypted API key - all in the DATA DIRECTORY, which is
// where the pre-plugin build kept them.
//
// The key is written through the real desktop.SecretStore rather than a fake, so
// the test drives the same decryption the user's machine will.
func seedLegacyChatData(t *testing.T, dataDir string) legacyChatSeed {
	t.Helper()
	cfg := map[string]any{
		"llmEnabled":            true,
		"llmProvider":           "deepseek",
		"llmBaseUrl":            "https://api.deepseek.com/v1",
		"llmModel":              "deepseek-chat",
		"llmTemperature":        0.7,
		"llmMaxTokens":          800,
		"llmTimeoutSec":         90,
		"llmSystemPrompt":       "你是一只猫",
		"llmHistoryTurns":       12,
		"llmContextTodos":       true,
		"llmProactive":          false,
		"llmProactiveMinGapMin": 60,
		"llmDailyBudget":        200,
		// A setting that is NOT part of the migration, so "carry the AI settings"
		// cannot be satisfied by copying the whole file.
		"petScale": 1.25,
	}
	cfgRaw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), cfgRaw, 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	history := []byte(`[{"role":"user","text":"你好","at":"2026-01-01T00:00:00Z"}]`)
	usage := []byte(`{"date":"2026-01-01","requests":3,"promptTokens":120}`)
	for name, body := range map[string][]byte{
		"llm-history.json": history,
		"llm-usage.json":   usage,
	} {
		if err := os.WriteFile(filepath.Join(dataDir, name), body, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	const apiKey = "sk-legacy-key-that-must-survive-the-move"
	if err := desktop.NewSecretStore(dataDir, "llm-key.bin").Set(apiKey); err != nil {
		t.Fatalf("seed the encrypted API key: %v", err)
	}
	return legacyChatSeed{config: cfgRaw, history: history, usage: usage, apiKey: apiKey}
}

// petAppOn builds an App over an existing data directory.
//
// newTestApp creates its own directory, which is the wrong order here: the
// legacy files have to be in place BEFORE the store opens, because the store
// loads the transcript and the usage counter during construction.
func petAppOn(t *testing.T, dir string) *App {
	t.Helper()
	s, err := store.New(dir)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return &App{store: s}
}

// --- T64b: the startup sequence runs it ------------------------------------

// TestStartupMigrationMovesThePetsAIChatDataIntoThePlugin is T64b's completion
// criterion, asserted end to end.
//
// It drives the step the startup sequence calls rather than the sequence itself,
// for the same reason TestStartupHookLaunchesOnlyWhenAsked does: the real
// startup waits for a Wails window, and a unit test has none. The clause "the
// startup sequence calls it" is pinned separately, by source, in
// TestOnlyOneCallSiteRunsTheSharedMigration.
func TestStartupMigrationMovesThePetsAIChatDataIntoThePlugin(t *testing.T) {
	dir := t.TempDir()
	man := seedInstalledChatPlugin(t, dir)
	seed := seedLegacyChatData(t, dir)

	a := petAppOn(t, dir)
	a.migrateChatData()

	// The marker ends the migration, and its presence is what makes the second
	// launch skip the whole thing.
	marker := plugins.MigrateMarkerPath(dir)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the migration marker %s must exist: %v", marker, err)
	}

	// The pet's own files are RENAMED, not deleted: the migration is reversible
	// by hand, and the pet's config.json is not touched at all.
	for _, name := range []string{"llm-history.json", "llm-usage.json", "llm-key.bin"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s must have been retired to .bak, stat err = %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dir, name+".bak")); err != nil {
			t.Errorf("%s.bak must exist: %v", name, err)
		}
	}
	after, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if string(after) != string(seed.config) {
		t.Errorf("config.json changed:\n before %s\n after  %s", seed.config, after)
	}

	// The plugin's private directory now holds the data, byte for byte.
	private := filepath.Join(dir, "plugin-data", man.ID)
	gotHistory, err := os.ReadFile(filepath.Join(private, "llm-history"))
	if err != nil {
		t.Fatalf("the transcript must have been carried: %v", err)
	}
	if string(gotHistory) != string(seed.history) {
		t.Errorf("llm-history = %s, want %s", gotHistory, seed.history)
	}
	gotUsage, err := os.ReadFile(filepath.Join(private, "llm-usage"))
	if err != nil {
		t.Fatalf("the usage counter must have been carried: %v", err)
	}
	if string(gotUsage) != string(seed.usage) {
		t.Errorf("llm-usage = %s, want %s", gotUsage, seed.usage)
	}

	// The settings are the plugin's now, and the pet's own key must be readable
	// through the plugin's declared secret item.
	values := plugins.NewConfigStore(private, man, a.pluginKeeper()).Values()
	for _, key := range []string{"llmBaseUrl", "llmModel", "llmSystemPrompt", "llmEnabled"} {
		if values[key] == "" {
			t.Errorf("%s was not carried into the plugin's settings: %+v", key, values)
		}
	}
	if values["llmBaseUrl"] != "https://api.deepseek.com/v1" {
		t.Errorf("llmBaseUrl = %q, want the pet's own endpoint", values["llmBaseUrl"])
	}
	if values["petScale"] != "" {
		t.Errorf("a non-AI setting leaked into the plugin's settings: %q", values["petScale"])
	}

	// The credential was re-encrypted under the plugin's own file name, and the
	// plaintext is nowhere on disk.
	gotKey, err := a.pluginKeeper().Get(filepath.Join(private, "secret-llmApiKey.bin"))
	if err != nil {
		t.Fatalf("the API key must be readable through the plugin's secret item: %v", err)
	}
	if gotKey != seed.apiKey {
		t.Errorf("the carried API key = %q, want the seeded one", gotKey)
	}
	leakCheck(t, private, seed.apiKey)
}

// leakCheck fails when the plaintext appears in any file under dir.
//
// It walks the whole directory rather than the files the migration is known to
// write, so a future change that started writing the key somewhere else is
// caught by this test rather than by a user.
func leakCheck(t *testing.T, dir, plaintext string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), plaintext) {
			t.Errorf("the plaintext API key is readable in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
}

// TestStartupMigrationWithoutThePluginChangesNothing is the "an upgraded install
// that never used the feature" case.
//
// It must be silent: no marker (so the next launch looks again), no private
// directory conjured up for a plugin that is not installed, and every source
// file still under its own name.
func TestStartupMigrationWithoutThePluginChangesNothing(t *testing.T) {
	dir := t.TempDir()
	seed := seedLegacyChatData(t, dir)

	a := petAppOn(t, dir)
	a.migrateChatData()

	if _, err := os.Stat(plugins.MigrateMarkerPath(dir)); !os.IsNotExist(err) {
		t.Errorf("no marker may be written without the plugin, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugin-data", plugins.ChatPluginID)); !os.IsNotExist(err) {
		t.Errorf("no private directory may be created without the plugin, stat err = %v", err)
	}
	for _, name := range []string{"llm-history.json", "llm-usage.json", "llm-key.bin"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s must be untouched: %v", name, err)
		}
	}
	after, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if string(after) != string(seed.config) {
		t.Error("config.json changed even though there was nothing to migrate")
	}
}

// failingKeeper is a secret keeper whose writes always fail.
//
// It stands in for the realistic failure this wiring has to survive: a machine
// whose DPAPI refuses to encrypt for the current user. Nothing else in the
// migration is made to fail, so the test also pins the ORDER - the failure lands
// while writing the plugin's settings, and everything before it is either
// harmless (config.json is only read) or not yet done.
type failingKeeper struct{}

func (failingKeeper) Get(string) (string, error) { return "sk-from-the-failing-keeper", nil }
func (failingKeeper) Set(string, string) error   { return errors.New("DPAPI 拒绝加密") }
func (failingKeeper) Clear(string) error         { return nil }

// TestStartupMigrationFailureCannotStopThePet is the "a plugin's settings are
// never worth a pet that will not start" clause.
//
// The step is called exactly as the startup sequence calls it, and it must
// return. Afterwards the user's data must be where they left it - the migration
// renames nothing until every target is written, so a failure is a retry, not a
// loss.
func TestStartupMigrationFailureCannotStopThePet(t *testing.T) {
	dir := t.TempDir()
	seedInstalledChatPlugin(t, dir)
	seedLegacyChatData(t, dir)

	a := petAppOn(t, dir)
	a.pluginHost.keeper = failingKeeper{}

	a.migrateChatData() // must return, must not panic

	if _, err := os.Stat(plugins.MigrateMarkerPath(dir)); !os.IsNotExist(err) {
		t.Errorf("a failed migration must not leave a marker behind, stat err = %v", err)
	}
	for _, name := range []string{"llm-history.json", "llm-usage.json", "llm-key.bin"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s must still be under its own name after a failure: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dir, name+".bak")); !os.IsNotExist(err) {
			t.Errorf("%s.bak must not exist after a failure, stat err = %v", name, err)
		}
	}
}

// --- T64b: one implementation, one call site -------------------------------

// TestOnlyOneCallSiteRunsTheSharedMigration pins both halves of "不复制实现".
//
// The migration rules live in internal/plugins/migrate.go and BOTH programs call
// them: the pet from its startup path, the market program from its constructor.
// A second copy in the pet would be the version that silently stops matching
// when the plugin's manifest changes, so the count is asserted rather than
// trusted.
//
// The needle is assembled from two pieces on purpose: this file is itself one of
// the app*.go files the count covers, and spelling the call here would make the
// assertion depend on the test's own text.
func TestOnlyOneCallSiteRunsTheSharedMigration(t *testing.T) {
	needle := "plugins." + "MigrateChatData"

	files, err := filepath.Glob("app*.go")
	if err != nil {
		t.Fatalf("glob app sources: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no app*.go files found; the assertion below would be vacuous")
	}
	total := 0
	where := ""
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if n := strings.Count(string(data), needle); n > 0 {
			total += n
			where = f
		}
	}
	if total != 1 {
		t.Fatalf("the shared migration is called %d times across app*.go, want exactly 1 (found in %q)", total, where)
	}

	// And the startup sequence is the caller: a call site that nothing invokes
	// would satisfy the count above while migrating nothing.
	src, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatalf("read app.go: %v", err)
	}
	if !strings.Contains(string(src), "a.migrateChatData()") {
		t.Error("app.go's startup sequence does not call a.migrateChatData()")
	}
}
