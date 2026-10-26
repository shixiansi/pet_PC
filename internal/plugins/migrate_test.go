package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chatManifestForMigration is the declaration the migration reads - it drives
// which keys are carried and which one is a secret.
func chatManifestForMigration() Manifest {
	return Manifest{
		ID:          ChatPluginID,
		Name:        "对话插件",
		Version:     "1.1.0",
		Author:      "shixiansi",
		Description: "对话插件",
		Permissions: []string{"llm.chat", "pet.bubble", "pet.surface", "plugin.config", "storage"},
		Panel:       true,
		PanelSlot:   "chat",
		Entry:       "panel.html",
		Config: []ConfigItem{
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

// seedChatInstall writes the plugin's directory, manifest, index entry and
// private directory, i.e. the state the migration requires before it will run.
func seedChatInstall(t *testing.T, dataDir string, man Manifest) string {
	t.Helper()
	pluginDir := filepath.Join(dataDir, "plugins", ChatPluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	data, err := json.Marshal(man)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, ManifestName), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	privateDir := filepath.Join(dataDir, "plugin-data", ChatPluginID)
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatalf("mkdir private: %v", err)
	}
	return privateDir
}

// seedLegacyState writes the three legacy data files and a config.json holding
// the 13 llm* values, returning the config.json bytes.
func seedLegacyState(t *testing.T, dataDir string) []byte {
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
		"someOtherSetting":      "untouched",
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write(migrateHistoryFile, `[{"role":"user","text":"你好"}]`)
	write(migrateUsageFile, `{"date":"2026-01-01","requests":3}`)
	return raw
}

// TestMigrateDoesNothingWithoutThePlugin covers T58.
func TestMigrateDoesNothingWithoutThePlugin(t *testing.T) {
	dataDir := t.TempDir()
	seedLegacyState(t, dataDir)
	keeper := newFakeKeeper()

	if err := MigrateChatData(dataDir, keeper); err != nil {
		t.Fatalf("MigrateChatData: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, migrateHistoryFile)); err != nil {
		t.Fatal("the source file must not be renamed when the plugin is absent")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "plugin-data", ChatPluginID)); !os.IsNotExist(err) {
		t.Fatal("no private directory may be created when the plugin is absent")
	}
	if _, err := os.Stat(MigrateMarkerPath(dataDir)); !os.IsNotExist(err) {
		t.Fatal("no marker may be written when the plugin is absent")
	}
}

// TestMigrateCarriesConfigValuesAndLeavesConfigAlone covers T59.
func TestMigrateCarriesConfigValuesAndLeavesConfigAlone(t *testing.T) {
	dataDir := t.TempDir()
	man := chatManifestForMigration()
	privateDir := seedChatInstall(t, dataDir, man)
	before := seedLegacyState(t, dataDir)
	keeper := newFakeKeeper()

	if err := MigrateChatData(dataDir, keeper); err != nil {
		t.Fatalf("MigrateChatData: %v", err)
	}

	store := NewConfigStore(privateDir, man, keeper)
	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for _, key := range legacyConfigKeys {
		if _, ok := got.Values[key]; !ok {
			t.Errorf("config key %s was not carried over", key)
		}
	}
	if got.Values["llmBaseUrl"] != "https://api.deepseek.com/v1" {
		t.Errorf("llmBaseUrl = %q", got.Values["llmBaseUrl"])
	}
	if got.Values["llmMaxTokens"] != "800" {
		t.Errorf("a number must be carried without a fractional part, got %q", got.Values["llmMaxTokens"])
	}
	if got.Values["llmContextTodos"] != "true" {
		t.Errorf("a bool must be carried as true/false, got %q", got.Values["llmContextTodos"])
	}

	after, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("the pet's config.json must not be modified")
	}
}

// TestMigrateDoesNotOverwriteExistingValues is the other half of T59.
func TestMigrateDoesNotOverwriteExistingValues(t *testing.T) {
	dataDir := t.TempDir()
	man := chatManifestForMigration()
	privateDir := seedChatInstall(t, dataDir, man)
	seedLegacyState(t, dataDir)
	keeper := newFakeKeeper()

	store := NewConfigStore(privateDir, man, keeper)
	if err := store.Write(map[string]string{"llmModel": "already-chosen"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := MigrateChatData(dataDir, keeper); err != nil {
		t.Fatalf("MigrateChatData: %v", err)
	}
	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Values["llmModel"] != "already-chosen" {
		t.Fatalf("an existing value must win, got %q", got.Values["llmModel"])
	}
}

// TestMigrateCarriesDataFiles covers T60 and T61.
func TestMigrateCarriesDataFiles(t *testing.T) {
	dataDir := t.TempDir()
	man := chatManifestForMigration()
	privateDir := seedChatInstall(t, dataDir, man)
	seedLegacyState(t, dataDir)
	keeper := newFakeKeeper()

	history, _ := os.ReadFile(filepath.Join(dataDir, migrateHistoryFile))
	usage, _ := os.ReadFile(filepath.Join(dataDir, migrateUsageFile))

	if err := MigrateChatData(dataDir, keeper); err != nil {
		t.Fatalf("MigrateChatData: %v", err)
	}

	gotHistory, err := os.ReadFile(filepath.Join(privateDir, "llm-history"))
	if err != nil || string(gotHistory) != string(history) {
		t.Fatalf("history not carried byte for byte: %q %v", gotHistory, err)
	}
	gotUsage, err := os.ReadFile(filepath.Join(privateDir, "llm-usage"))
	if err != nil || string(gotUsage) != string(usage) {
		t.Fatalf("usage not carried byte for byte: %q %v", gotUsage, err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, migrateHistoryFile+".bak")); err != nil {
		t.Errorf("llm-history.json.bak must exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, migrateUsageFile+".bak")); err != nil {
		t.Errorf("llm-usage.json.bak must exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, migrateHistoryFile)); !os.IsNotExist(err) {
		t.Error("the original llm-history.json must be gone")
	}
	if _, err := os.Stat(filepath.Join(dataDir, migrateUsageFile)); !os.IsNotExist(err) {
		t.Error("the original llm-usage.json must be gone")
	}
}

// TestMigrateReEncryptsTheKey covers T62.
func TestMigrateReEncryptsTheKey(t *testing.T) {
	dataDir := t.TempDir()
	man := chatManifestForMigration()
	privateDir := seedChatInstall(t, dataDir, man)
	seedLegacyState(t, dataDir)
	const plain = "sk-legacykeyvalue1234"
	if err := os.WriteFile(filepath.Join(dataDir, migrateKeyFile), []byte("ciphertext"), 0o644); err != nil {
		t.Fatalf("write key: %v", err)
	}

	keeper := newFakeKeeper()
	// The old key is read through the keeper at its legacy path.
	keeper.values[filepath.Join(dataDir, migrateKeyFile)] = plain

	if err := MigrateChatData(dataDir, keeper); err != nil {
		t.Fatalf("MigrateChatData: %v", err)
	}

	// The only Set calls are the migration's re-encryption of the key.
	setPaths := []string{}
	for path := range keeper.values {
		setPaths = append(setPaths, path)
	}
	want := filepath.Join(privateDir, "secret-llmApiKey.bin")
	found := false
	for _, p := range setPaths {
		if p == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("the key must be re-encrypted at %s, got %v", want, setPaths)
	}
	if keeper.values[want] != plain {
		t.Fatalf("the re-encrypted value must be the legacy plaintext")
	}
	if _, err := os.Stat(filepath.Join(dataDir, migrateKeyFile+".bak")); err != nil {
		t.Errorf("llm-key.bin.bak must exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, migrateKeyFile)); !os.IsNotExist(err) {
		t.Error("the original llm-key.bin must be gone")
	}
}

// TestMigrateIsIdempotent covers T63: a second call does not read the sources
// and does not ask crypto for anything.
func TestMigrateIsIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	man := chatManifestForMigration()
	privateDir := seedChatInstall(t, dataDir, man)
	seedLegacyState(t, dataDir)
	keeper := newFakeKeeper()

	if err := MigrateChatData(dataDir, keeper); err != nil {
		t.Fatalf("first MigrateChatData: %v", err)
	}
	historyAfterFirst, err := os.ReadFile(filepath.Join(privateDir, "llm-history"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}

	keeper.gets, keeper.sets, keeper.clears = 0, 0, 0
	if err := MigrateChatData(dataDir, keeper); err != nil {
		t.Fatalf("second MigrateChatData: %v", err)
	}
	if keeper.gets != 0 || keeper.sets != 0 || keeper.clears != 0 {
		t.Fatalf("a second run must not touch crypto: gets=%d sets=%d clears=%d",
			keeper.gets, keeper.sets, keeper.clears)
	}
	historyAfterSecond, err := os.ReadFile(filepath.Join(privateDir, "llm-history"))
	if err != nil || string(historyAfterFirst) != string(historyAfterSecond) {
		t.Fatal("a second run must not change the target files")
	}
}

// TestMigrateFailureLeavesEverythingPut covers T64: a failing key write stops
// the run before any file is renamed, and a later run still completes.
func TestMigrateFailureLeavesEverythingPut(t *testing.T) {
	dataDir := t.TempDir()
	man := chatManifestForMigration()
	seedChatInstall(t, dataDir, man)
	seedLegacyState(t, dataDir)
	if err := os.WriteFile(filepath.Join(dataDir, migrateKeyFile), []byte("ciphertext"), 0o644); err != nil {
		t.Fatalf("write key: %v", err)
	}

	keeper := newFakeKeeper()
	keeper.values[filepath.Join(dataDir, migrateKeyFile)] = "sk-legacykeyvalue1234"
	keeper.failSet = true

	if err := MigrateChatData(dataDir, keeper); err == nil {
		t.Fatal("a failing key write must be reported")
	}
	for _, name := range []string{migrateHistoryFile, migrateUsageFile, migrateKeyFile} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); err != nil {
			t.Errorf("%s must still exist under its original name", name)
		}
		if _, err := os.Stat(filepath.Join(dataDir, name+".bak")); !os.IsNotExist(err) {
			t.Errorf("%s.bak must not exist after a failure", name)
		}
	}
	if _, err := os.Stat(MigrateMarkerPath(dataDir)); !os.IsNotExist(err) {
		t.Error("no marker may be written after a failure")
	}

	// The retry succeeds and carries everything.
	keeper.failSet = false
	if err := MigrateChatData(dataDir, keeper); err != nil {
		t.Fatalf("the retry must succeed: %v", err)
	}
	if _, err := os.Stat(MigrateMarkerPath(dataDir)); err != nil {
		t.Fatalf("the marker must exist after the retry: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dataDir, "plugin-data", ChatPluginID, "llm-history"))
	if err != nil || !strings.Contains(string(got), "你好") {
		t.Fatalf("the retry must carry the history: %q %v", got, err)
	}
}
