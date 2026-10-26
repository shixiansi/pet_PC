package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// This file is the ONE-TIME, IDEMPOTENT move of the pet's own AI settings into
// the conversation plugin's private storage.
//
// Before the conversation became a plugin, the pet held the model endpoint, the
// model name, the endpoint credentials, the transcript and the usage counter
// itself. They now belong to the plugin - its manifest declares each of them -
// so the values have to be carried across once, without losing anything and
// without ever being able to run twice.
//
// Three properties are load-bearing:
//
//  1. The pet's config.json is READ, never written. It remains the rollback: a
//     user who wants the old build back finds every value where it was.
//  2. Nothing is renamed until every target file has been written, so a failure
//     part-way through leaves all three source files exactly as they were and
//     the next launch retries.
//  3. A marker in the plugin's private directory ends it. Its presence means
//     the source files are not even read.
//
// BOTH programs call this (the pet at startup, the market program when it
// starts), because either may be the first one opened after the upgrade. They
// share this implementation; there is no second copy of the rules.

// ChatPluginID is the conversation plugin's id.
//
// It is fixed because the migration has to know WHO to hand the data to, and
// its own manifest is what declares the keys - so the id is the only name this
// file needs to know. The declaration is read from disk.
const ChatPluginID = "com.shixiansi.plugin-chat"

// migrateMarkerName marks a completed migration inside the plugin's directory.
const migrateMarkerName = ".migrated"

// Migration source file names inside the data directory.
const (
	migrateHistoryFile = "llm-history.json"
	migrateUsageFile   = "llm-usage.json"
	migrateKeyFile     = "llm-key.bin"
)

// legacyConfigKeys are the pet Config fields the migration carries over, in the
// json-tag spelling the plugin's manifest also uses. The order is the order they
// are written, so the resulting file reads like the settings form.
var legacyConfigKeys = []string{
	"llmEnabled",
	"llmProvider",
	"llmBaseUrl",
	"llmModel",
	"llmTemperature",
	"llmMaxTokens",
	"llmTimeoutSec",
	"llmSystemPrompt",
	"llmHistoryTurns",
	"llmContextTodos",
	"llmProactive",
	"llmProactiveMinGapMin",
	"llmDailyBudget",
}

// MigrateChatData moves the pet's legacy AI settings into the conversation
// plugin's private storage.
//
// It reports no error when there is nothing to do: "the plugin is not installed
// yet", "the files are already gone" and "this ran before" are all ordinary
// states, not failures. An error means the data was there and could not be
// moved, and in that case nothing was changed.
func MigrateChatData(dataDir string, keeper SecretKeeper) error {
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("迁移缺少数据目录")
	}
	manifest, privateDir, ok := chatPluginOnDisk(dataDir)
	if !ok {
		// The plugin is not installed, so there is nowhere to put its settings.
		// No marker is written: the next launch looks again.
		return nil
	}
	if markerExists(privateDir) {
		return nil
	}

	// 1. Read the sources. A source that is absent is simply not part of this
	//    migration; when none of the three exist there is nothing to carry.
	history, historyErr := os.ReadFile(filepath.Join(dataDir, migrateHistoryFile))
	usage, usageErr := os.ReadFile(filepath.Join(dataDir, migrateUsageFile))
	keyBlob, keyErr := os.ReadFile(filepath.Join(dataDir, migrateKeyFile))
	if historyErr != nil && usageErr != nil && keyErr != nil {
		return nil
	}
	values, err := legacyConfigValues(dataDir)
	if err != nil {
		return err
	}

	// 2. Decrypt the old key BEFORE anything is written. A credential that can
	//    no longer be decrypted (a different Windows user) must not stop the
	//    transcript and the usage counter from moving, but it must also not be
	//    silently dropped: the user is told, and the files stay put.
	plainKey := ""
	if keyErr == nil && len(keyBlob) > 0 {
		if keeper == nil {
			return errors.New("迁移密钥失败：本进程没有注入密钥存储")
		}
		got, err := keeper.Get(filepath.Join(dataDir, migrateKeyFile))
		if err != nil {
			return fmt.Errorf("迁移密钥失败：%w", err)
		}
		plainKey = got
	}

	store := NewConfigStore(privateDir, manifest, keeper)

	// 3. Write the targets. config.json is MERGED, so a value the plugin has
	//    already stored (it may have been used before the migration ran) wins.
	pending := map[string]string{}
	for k, v := range values {
		if _, declared := manifest.ConfigItem(k); !declared {
			// A manifest that does not declare a legacy key simply does not
			// receive it; the value stays in the pet's config.json.
			continue
		}
		if _, isSecret := secretKey(manifest, k); isSecret {
			continue
		}
		pending[k] = v
	}
	existing := store.Values()
	for k := range existing {
		delete(pending, k)
	}
	if len(pending) > 0 {
		if err := store.Write(pending); err != nil {
			return fmt.Errorf("迁移插件配置失败：%w", err)
		}
	}
	if plainKey != "" {
		if item, ok := secretKey(manifest, "llmApiKey"); ok && item.Secret {
			if err := store.Write(map[string]string{"llmApiKey": plainKey}); err != nil {
				return fmt.Errorf("迁移密钥失败：%w", err)
			}
		}
	}

	if historyErr == nil {
		if err := atomicWriteFile(filepath.Join(privateDir, "llm-history"), history); err != nil {
			return fmt.Errorf("迁移对话记录失败：%w", err)
		}
	}
	if usageErr == nil {
		if err := atomicWriteFile(filepath.Join(privateDir, "llm-usage"), usage); err != nil {
			return fmt.Errorf("迁移用量失败：%w", err)
		}
	}

	// 4. Everything is in place, so the sources are retired. Renaming is the
	//    LAST step precisely so a failure above leaves them untouched.
	retired := make([]string, 0, 3)
	for _, name := range []string{migrateHistoryFile, migrateUsageFile, migrateKeyFile} {
		source := filepath.Join(dataDir, name)
		if _, err := os.Stat(source); err != nil {
			continue
		}
		backup := source + ".bak"
		// Keep only the most recent backup, exactly as the index quarantine does.
		os.Remove(backup)
		if err := os.Rename(source, backup); err != nil {
			return fmt.Errorf("备份 %s 失败：%w", name, err)
		}
		retired = append(retired, name)
	}

	// 5. The marker ends it. Its content is a timestamp and the files that were
	//    moved, so the file answers "did this run, and what did it take?".
	marker := time.Now().Format(time.RFC3339) + "\n" + strings.Join(retired, "\n") + "\n"
	return atomicWriteFile(filepath.Join(privateDir, migrateMarkerName), []byte(marker))
}

// chatPluginOnDisk finds the conversation plugin's installed directory and
// private directory.
//
// Both must exist: a plugin directory without a private directory is not an
// install this migration can complete, and creating one would leave state behind
// for a plugin that is not there.
func chatPluginOnDisk(dataDir string) (Manifest, string, bool) {
	pluginDir := filepath.Join(dataDir, "plugins", ChatPluginID)
	man, err := LoadManifest(pluginDir)
	if err != nil {
		return Manifest{}, "", false
	}
	privateDir := filepath.Join(dataDir, "plugin-data", ChatPluginID)
	if fi, err := os.Stat(privateDir); err != nil || !fi.IsDir() {
		if err := os.MkdirAll(privateDir, 0o755); err != nil {
			return Manifest{}, "", false
		}
	}
	return man, privateDir, true
}

// markerExists reports whether the migration already ran.
func markerExists(privateDir string) bool {
	_, err := os.Stat(filepath.Join(privateDir, migrateMarkerName))
	return err == nil
}

// legacyConfigValues reads the pet's config.json and returns the AI settings it
// holds, keyed by the json tag the plugin's manifest also uses.
//
// The file is only READ. A config.json that cannot be parsed is not an error
// here: the migration's job is to move whatever is legible, and the pet itself
// reports a broken config through its own quarantine path.
func legacyConfigValues(dataDir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("读取 config.json 失败：%w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]string{}, nil
	}
	out := map[string]string{}
	for _, key := range legacyConfigKeys {
		v, ok := raw[key]
		if !ok {
			continue
		}
		out[key] = renderConfigValue(v)
	}
	return out, nil
}

// renderConfigValue renders one JSON value in the string form the config store
// uses. Only the three shapes the pet's config actually contains are handled;
// anything else is skipped rather than stringified arbitrarily.
func renderConfigValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		// json.Unmarshal gives every number a float64. Integers are rendered
		// without a fractional part so "800" does not become "800.000000".
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", t), "0"), ".")
	default:
		return ""
	}
}

// secretKey reports whether key is a declared secret item.
func secretKey(man Manifest, key string) (ConfigItem, bool) {
	item, ok := man.ConfigItem(key)
	if !ok || !item.Secret {
		return ConfigItem{}, false
	}
	return item, true
}

// MigrateMarkerPath returns where the marker for the conversation plugin lives,
// for diagnostics and tests.
func MigrateMarkerPath(dataDir string) string {
	return filepath.Join(dataDir, "plugin-data", ChatPluginID, migrateMarkerName)
}
