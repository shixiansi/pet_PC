package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if !strings.Contains(report, "todos.json") || !strings.Contains(report, "不存在") {
		t.Errorf("a missing file was skipped instead of reported:\n%s", report)
	}
	if !strings.Contains(report, "（没有已装插件）") {
		t.Errorf("an empty plugin list was not stated:\n%s", report)
	}
}

func TestDiagnosticsCarriesTheLogTailAndSaysWhyItMayBeEmpty(t *testing.T) {
	m, dir := diagMarket(t)

	// No log at all: the report has to say WHY, because "no log" is the normal state - the
	// pet only writes one when PETPC_DEBUG is set.
	report, err := m.Diagnostics()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "PETPC_DEBUG") {
		t.Errorf("a missing debug.log was not explained:\n%s", report)
	}

	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, "line "+fmt.Sprintf("%02d", i))
	}
	if err := os.WriteFile(filepath.Join(dir, "debug.log"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
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

func TestDiagnosticsNeverContainsTheSecret(t *testing.T) {
	m, dir := diagMarket(t)
	const secret = "sk-THIS-MUST-NOT-LEAVE-THE-PROCESS"
	if err := os.WriteFile(filepath.Join(dir, hostKeyFile), []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := m.Diagnostics()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(report, secret) {
		t.Fatalf("the report carries the key file's contents:\n%s", report)
	}
	// It still has to be USEFUL: the file's presence and size, and that it was not decrypted.
	if !strings.Contains(report, hostKeyFile) || !strings.Contains(report, "密文") {
		t.Errorf("the key section does not say what it did and did not read:\n%s", report)
	}
}

func TestHostKeyReportsPresenceWithoutReadingIt(t *testing.T) {
	m, dir := diagMarket(t)

	key := m.HostKey()
	if key.Present {
		t.Fatalf("HostKey reports a key with an empty data directory: %+v", key)
	}
	if filepath.Base(key.Path) != hostKeyFile {
		t.Errorf("HostKey looked at %q, want ...%s", key.Path, hostKeyFile)
	}

	// An EMPTY file is not a key: the pet writes the encrypted blob, so zero bytes means a
	// half-finished write, and reporting that as "已设置" would send the user hunting in the
	// wrong place.
	if err := os.WriteFile(key.Path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if m.HostKey().Present {
		t.Error("an empty key file was reported as present")
	}
	if err := os.WriteFile(filepath.Join(dir, hostKeyFile), []byte("ciphertext"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !m.HostKey().Present {
		t.Error("a non-empty key file was reported as absent")
	}
}
