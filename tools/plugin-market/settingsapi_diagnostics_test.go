package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"PetPC/internal/logging"
	"PetPC/internal/plugins"
)

// These cover the two things a diagnostic report can get wrong and never notice: claiming a
// file is there when it is not, and carrying something that should not leave the machine.

func diagMarket(t *testing.T) (*Market, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := newMarketAt(dir, "")
	if err != nil {
		t.Fatalf("newMarketAt: %v", err)
	}
	return m, dir
}

func TestDiagnosticsNamesTheFilesThatAreNotThere(t *testing.T) {
	m, dir := diagMarket(t)
	settings := []byte(`{"focusMinutes":25}`)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), settings, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := m.Diagnostics()
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if !strings.Contains(report, dir) {
		t.Errorf("report does not name the data directory %q", dir)
	}
	// A file that IS there must show its size, and a file that is not must be SAID to be
	// missing: an omitted line reads as "nothing to see", which is the opposite of the fact.
	if !strings.Contains(report, "settings.json") || !strings.Contains(report, fmt.Sprintf("%d B", len(settings))) {
		t.Errorf("settings.json line has no size:\n%s", report)
	}
	if !strings.Contains(report, "plugins.json") || !strings.Contains(report, "不存在") {
		t.Errorf("a missing file was skipped instead of reported:\n%s", report)
	}
	// The files that RETIRED must not be reported at all. A line saying "todos.json 不存在"
	// is not neutral: it reads as a problem to fix, while the file's absence is the intended
	// state (the list lives in the todo plugin's private storage now), and llm-key.bin is the
	// pet's old model key, moved into the conversation plugin by the one-time migration.
	for _, gone := range []string{"todos.json", "llm-key.bin", "pomodoro.json"} {
		if strings.Contains(report, gone) {
			t.Errorf("the report still names the retired file %s:\n%s", gone, report)
		}
	}
	if !strings.Contains(report, "（没有已装插件）") {
		t.Errorf("an empty plugin list was not stated:\n%s", report)
	}
}

func TestDiagnosticsCarriesTheLogTailAndSaysWhyItMayBeEmpty(t *testing.T) {
	m, dir := diagMarket(t)

	// No log at all: the report has to say WHY, and "why" is no longer "you forgot a
	// flag" - the logs are always written now. An empty one means neither program has
	// run against this data directory.
	report, err := m.Diagnostics()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "还没有日志") {
		t.Errorf("an absent log was not explained:\n%s", report)
	}
	if !strings.Contains(report, "logs/pet.log") || !strings.Contains(report, "logs/market.log") {
		t.Errorf("the file table does not name both logs:\n%s", report)
	}

	// 60 real lines through the SHARED writer, so this also pins the format the 日志
	// page parses: a report built from a hand-written string would pass while the page
	// showed nothing.
	lg := logging.Open(dir, logging.PetLog, logging.Options{Level: logging.LevelDebug})
	for i := 0; i < 60; i++ {
		lg.Infof("pet", "line %02d", i)
	}

	report, err = m.Diagnostics()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "line 59") || !strings.Contains(report, "只显示最后") {
		t.Errorf("the tail does not end with the newest line, or is not summarised:\n%s", report)
	}
	if strings.Contains(report, "line 00") {
		t.Errorf("the tail was not bounded to the last %d lines:\n%s", logTailLines, report)
	}
}

// TestDiagnosticsNeverContainsTheSecret pins the half of a report that must stay true no matter
// what the report says: nothing secret leaves the machine.
//
// It used to be about llm-key.bin, which this program reported the PRESENCE of without ever
// opening. That file is retired now (密钥按插件隔离存在 plugin-data/<id>/secret-*.bin, and the
// old one is re-encrypted into the conversation plugin's directory by the one-time migration),
// so the report no longer names it at all - which is the stronger statement, and the one worth
// asserting: a report that cannot see a file cannot leak it.
func TestDiagnosticsNeverContainsTheSecret(t *testing.T) {
	m, dir := diagMarket(t)
	const secret = "sk-THIS-MUST-NOT-LEAVE-THE-PROCESS"
	// Both shapes: the pet's retired key file, and a plugin's own secret inside its private
	// directory. Neither is on the report's path any more.
	if err := os.WriteFile(filepath.Join(dir, "llm-key.bin"), []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(dir, "plugin-data", "com.example.chat")
	if err := os.MkdirAll(private, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "secret-llmApiKey.bin"), []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := m.Diagnostics()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(report, secret) {
		t.Fatalf("the report carries secret bytes:\n%s", report)
	}
	if strings.Contains(report, "llm-key.bin") {
		t.Errorf("the report still names the retired key file:\n%s", report)
	}
}

// TestDiagnosticsNamesWhatIsMissingPerPlugin is the "装好了但什么也不动" half of a report.
//
// Every line here is a failure with no install-time error and no visible symptom from the
// outside: a permission nobody ticked, a helper script the package does not carry, a call
// target nobody installed, a plugin that is switched off. `pluginProblems` is pure enough to
// drive directly, which is why this test builds the row rather than installing one.
func TestDiagnosticsNamesWhatIsMissingPerPlugin(t *testing.T) {
	m, _ := diagMarket(t)
	row := plugins.Installed{
		ID:      "com.example.thing",
		Version: "1.0.0",
		Enabled: true,
		Granted: []string{"storage"},
		Dir:     t.TempDir(),
		Manifest: &plugins.Manifest{
			ID:          "com.example.thing",
			Permissions: []string{"storage", "plugin.call", "pet.bubble"},
			Sidecar:     &plugins.SidecarSpec{Script: "helper.ps1"},
			CallTargets: []string{"com.example.absent/stats"},
		},
	}
	lines := strings.Join(pluginProblems(m, row, map[string]bool{"com.example.thing": true}), "\n")
	for _, want := range []string{
		"缺授权：plugin.call、pet.bubble",   // the two nobody ticked, named
		"辅助脚本不在：helper.ps1",           // the sidecar the package does not carry
		"callTargets 指向没装的插件：com.example.absent/stats",
	} {
		if !strings.Contains(lines, want) {
			t.Fatalf("problem lines:\n%s\nwant them to mention %q", lines, want)
		}
	}
	if strings.Contains(lines, "停用中") {
		t.Fatalf("an enabled plugin was reported as switched off:\n%s", lines)
	}

	// Switched off: said out loud, because a resident frame that never runs looks exactly like
	// a plugin whose feature is broken.
	row.Enabled = false
	if lines := strings.Join(pluginProblems(m, row, map[string]bool{"com.example.thing": true}), "\n"); !strings.Contains(lines, "停用中") {
		t.Fatalf("a disabled plugin was not reported:\n%s", lines)
	}
}

// TestDiagnosticsCarriesThePerPluginProblems proves the lines actually reach the report the user
// copies - a helper that nothing calls is not a diagnosis.
func TestDiagnosticsCarriesThePerPluginProblems(t *testing.T) {
	m, _ := diagMarket(t)
	// A plugin that declares plugin.share but was granted only that (not storage / pet.bubble):
	// installShareFixture grants plugin.share only.
	installShareFixture(t, m, "com.example.half", nil)

	report, err := m.Diagnostics()
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if !strings.Contains(report, "com.example.half") {
		t.Fatalf("the plugin is not in the report:\n%s", report)
	}
	if !strings.Contains(report, "缺授权：") {
		t.Fatalf("the missing grants were not reported:\n%s", report)
	}
}
