package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixedClock makes the timestamps in a test assertable.
func fixedClock(at time.Time) func() time.Time { return func() time.Time { return at } }

// TestLineRoundTrips is the contract between the writer and the 日志 page: whatever Line
// produces, ParseLine must read back exactly. Everything else in this package is a
// convenience around these two functions.
func TestLineRoundTrips(t *testing.T) {
	at := time.Date(2026, 10, 28, 14, 32, 0, 123000000, time.FixedZone("CST", 8*3600))
	in := Entry{At: at, Level: LevelWarn, Scope: "plugin", Plugin: "com.example.p", Message: "llm.chat 失败：未获权限 llm.chat"}

	out, ok := ParseLine(in.Line())
	if !ok {
		t.Fatalf("ParseLine refused a line this package wrote: %q", in.Line())
	}
	if !out.At.Equal(at) {
		t.Errorf("time: got %v, want %v", out.At, at)
	}
	if out.Level != in.Level || out.Scope != in.Scope || out.Plugin != in.Plugin || out.Message != in.Message {
		t.Errorf("field mismatch:\n got %+v\nwant %+v", out, in)
	}
}

// TestLevelTokensAreTheOnesUsersType guards the filter vocabulary: the 日志 page sends
// these words back in its query, so a renamed token would empty the page for every level
// but the default.
func TestLevelTokensAreTheOnesUsersType(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Level
	}{
		{"DEBUG", LevelDebug}, {"debug", LevelDebug},
		{"info", LevelInfo}, {"INFO", LevelInfo}, {"信息", LevelInfo},
		{"warn", LevelWarn}, {"WARNING", LevelWarn}, {"警告", LevelWarn},
		{"error", LevelError}, {"错误", LevelError},
	} {
		got, ok := ParseLevel(c.in)
		if !ok || got != c.want {
			t.Errorf("ParseLevel(%q) = %v/%v, want %v", c.in, got, ok, c.want)
		}
	}
	if _, ok := ParseLevel("verbose"); ok {
		t.Error("ParseLevel accepted a level that does not exist")
	}
	if got := LevelOfOrDefault("nonsense", LevelInfo); got != LevelInfo {
		t.Errorf("an unreadable level must fall back rather than fail: got %v", got)
	}
}

// TestNilLoggerIsSilent is not trivia: every call site in both programs is written
// WITHOUT a nil check, so a logger that panics or refuses to compile into a nil field is
// the difference between a log line and a crash inside an error path.
func TestNilLoggerIsSilent(t *testing.T) {
	var l *Logger
	l.Log(LevelError, "pet", "x", "y")
	l.Error("pet", "y")
	l.Errorf("pet", "y %d", 1)
	l.Pluginf(LevelInfo, "plugin", "id", "y")
	l.SetLevel(LevelDebug)
	if l.Level() != LevelError {
		t.Errorf("a nil logger's level must read as the quietest, got %v", l.Level())
	}
	if l.Path() != "" || l.Err() != nil {
		t.Error("a nil logger must answer harmlessly")
	}
}

// TestLevelGateDropsQuieterEntries is the property that makes the levels worth having:
// at the shipping default the file holds the story, not the drag probes.
func TestLevelGateDropsQuieterEntries(t *testing.T) {
	dir := t.TempDir()
	l := Open(dir, PetLog, Options{Level: LevelInfo})
	l.Debug("pet", "拖动探针")
	l.Info("pet", "启动")
	l.Warn("plugin", "重连")
	l.Error("llm", "401")

	entries := mustRead(t, dir, []string{PetLog}, Query{})
	if len(entries) != 3 {
		t.Fatalf("want 3 entries above DEBUG, got %d: %+v", len(entries), entries)
	}
	if entries[0].Message != "启动" || entries[2].Level != LevelError {
		t.Errorf("entries are not in file order: %+v", entries)
	}
}

// TestRotationKeepsOneGeneration pins the retention policy: bounded on disk, and the
// previous generation is still readable so "what happened just before the restart" is
// answerable.
func TestRotationKeepsOneGeneration(t *testing.T) {
	dir := t.TempDir()
	l := Open(dir, PetLog, Options{MaxBytes: 200, Level: LevelDebug})
	for i := 0; i < 40; i++ {
		l.Info("pet", "a line long enough that forty of them certainly rotate the file")
	}
	files := Files(dir, PetLog)
	if len(files) != 2 {
		t.Fatalf("want the live file and one generation, got %d", len(files))
	}
	for _, f := range files {
		if f.Bytes > 400 {
			t.Errorf("%s grew past the rotation budget: %d bytes", f.Name, f.Bytes)
		}
	}
	// The merged read spans BOTH generations - that is the point of keeping one. It is
	// deliberately a small number: retention is "the live file plus one generation of a
	// 200-byte budget", i.e. two short lines each. So the assertion is "both generations
	// are represented AND the newest line is still there", not "nothing was lost" -
	// losing the oldest is what a bounded log is for.
	entries := mustRead(t, dir, []string{PetLog}, Query{Limit: MaxLimit})
	if len(entries) < 2 {
		t.Fatalf("the merged read lost the rotated generation: %d entries", len(entries))
	}
	if last := entries[len(entries)-1]; last.Message != "a line long enough that forty of them certainly rotate the file" {
		t.Errorf("the newest line went missing after rotation: %+v", last)
	}
}

// TestReadFiltersAndOrders covers the three knobs the 日志 page exposes, because a filter
// that silently does nothing is indistinguishable from "there is nothing to see".
func TestReadFiltersAndOrders(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 28, 9, 0, 0, 0, time.Local)
	pet := Open(dir, PetLog, Options{Now: fixedClock(base)})
	pet.Log(LevelInfo, "plugin", "com.a", "todos.list ok")
	pet.Log(LevelError, "llm", "com.a", "401 unauthorized")
	pet.Log(LevelWarn, "plugin", "com.b", "surface.show 被拒绝")

	// Market lines interleave into the merged view by timestamp, not by file.
	market := Open(dir, MarketLog, Options{Now: fixedClock(base.Add(time.Minute))})
	market.Log(LevelInfo, "market", "", "安装完成")

	all := mustRead(t, dir, []string{PetLog, MarketLog}, Query{})
	if len(all) != 4 {
		t.Fatalf("want 4 merged entries, got %d: %+v", len(all), all)
	}
	if all[3].Scope != "market" {
		t.Errorf("merged view is not chronological: last is %+v", all[3])
	}

	if got := mustRead(t, dir, []string{PetLog}, Query{MinLevel: LevelWarn}); len(got) != 2 {
		t.Errorf("MinLevel=warn should keep 2 entries, got %d", len(got))
	}
	if got := mustRead(t, dir, []string{PetLog}, Query{Scope: "plugin"}); len(got) != 2 {
		t.Errorf("scope filter should keep 2 entries, got %d", len(got))
	}
	// Search covers the plugin id as well as the message, because "which plugin" is
	// what a user usually has when they open this page.
	if got := mustRead(t, dir, []string{PetLog}, Query{Search: "com.a"}); len(got) != 2 {
		t.Errorf("search over the plugin column should keep 2 entries, got %d", len(got))
	}
	if got := mustRead(t, dir, []string{PetLog}, Query{Search: "UNAUTHORIZED"}); len(got) != 1 {
		t.Errorf("search must be case-insensitive, got %d entries", len(got))
	}
	if got := mustRead(t, dir, []string{PetLog}, Query{Limit: 1}); len(got) != 1 || got[0].Level != LevelWarn {
		t.Errorf("Limit must keep the NEWEST entry: %+v", got)
	}
	if scopes := Scopes(all); strings.Join(scopes, ",") != "llm,market,plugin" {
		t.Errorf("Scopes should offer every subsystem that appeared, got %v", scopes)
	}
}

// TestUnreadableLinesAreDropped is the power-cut case and the hand-edit case: a line that
// does not parse must cost that line and nothing else.
func TestUnreadableLinesAreDropped(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	good := Entry{At: time.Now(), Level: LevelInfo, Scope: "pet", Message: "ok"}.Line()
	content := "garbage from a half-written line\n" + good + "\n2026-10-28T09:00:00.000+08:00 TRACE pet - nope\n"
	if err := os.WriteFile(filepath.Join(dir, DirName, PetLog+".log"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := mustRead(t, dir, []string{PetLog}, Query{})
	if len(entries) != 1 || entries[0].Message != "ok" {
		t.Fatalf("want exactly the readable line, got %+v", entries)
	}
}

// TestNewlinesInAMessageCannotForgeAnEntry is a small security property rather than
// cosmetics: a message carrying a newline would otherwise let a plugin's error text (or a
// page's) write a second line that LOOKS like the host logged it.
func TestNewlinesInAMessageCannotForgeAnEntry(t *testing.T) {
	dir := t.TempDir()
	l := Open(dir, PetLog, Options{})
	l.Info("plugin", "说了一句\n2026-10-28T09:00:00.000+08:00 ERROR llm - 伪造的一行")
	entries := mustRead(t, dir, []string{PetLog}, Query{})
	if len(entries) != 1 {
		t.Fatalf("a message with a newline produced %d entries", len(entries))
	}
	if entries[0].Level != LevelInfo {
		t.Errorf("the forged line took effect: %+v", entries[0])
	}
}

// TestFilesReportsMissingOnes: an absent file is information, and the page has to be able
// to tell "the pet never ran" from "the pet is broken".
func TestFilesReportsMissingOnes(t *testing.T) {
	dir := t.TempDir()
	files := Files(dir, PetLog)
	if len(files) != 2 {
		t.Fatalf("want two rows per log (live + previous), got %d", len(files))
	}
	for _, f := range files {
		if f.Bytes != 0 || f.Exists() || f.Err != "" {
			t.Errorf("a missing file must be reported as absent, not as an error: %+v", f)
		}
	}
}

func mustRead(t *testing.T, dir string, names []string, q Query) []Entry {
	t.Helper()
	entries, err := Read(dir, names, q)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return entries
}
