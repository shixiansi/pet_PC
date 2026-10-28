package main

// The 日志 page's binding, driven through the real files.
//
// What is worth testing here is not "the struct is filled in" but three behaviours the
// page depends on and a screenshot cannot show: the two programs' lines are MERGED by
// time (the pet's half is the interesting one), the filters reach the read, and an empty
// result carries a reason.

import (
	"strings"
	"testing"

	"PetPC/internal/logging"
)

// seedLogs writes one pet line, one refused plugin call and one market line.
func seedLogs(t *testing.T, dir string) {
	t.Helper()
	pet := logging.Open(dir, logging.PetLog, logging.Options{Level: logging.LevelDebug})
	pet.Info("pet", "启动：测试")
	pet.Pluginf(logging.LevelWarn, "plugin", "com.example.p", "llm.chat 没有执行（payload 84 B）")
	pet.Debug("llm", "POST https://api.example/v1/chat/completions model=m")

	market := logging.Open(dir, logging.MarketLog, logging.Options{})
	market.Pluginf(logging.LevelInfo, "market", "com.example.p", "授权已保存：[llm.chat]")
}

func TestLogTailMergesBothProgramsAndIsNewestFirst(t *testing.T) {
	m, dir := diagMarket(t)
	seedLogs(t, dir)

	view, err := m.LogTail(LogQuery{})
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}
	if len(view.Entries) != 4 {
		t.Fatalf("want 4 merged entries (2 pet + 1 llm + 1 market), got %d: %+v", len(view.Entries), view.Entries)
	}
	// Newest first, whatever file it came from: the market line was written last.
	if view.Entries[0].Scope != "market" {
		t.Errorf("the first row is not the newest: %+v", view.Entries[0])
	}
	// Both files are listed - including the previous generations, which do not exist yet
	// and are therefore reported as absent rather than omitted.
	if len(view.Files) != 4 {
		t.Errorf("want a row for each of the four log files, got %d", len(view.Files))
	}
	if view.Dir == "" || !strings.Contains(view.Dir, logging.DirName) {
		t.Errorf("the view does not say where the logs are: %q", view.Dir)
	}
	if view.Note != "" {
		t.Errorf("a non-empty view carries a note: %q", view.Note)
	}
	// The plugin column survives the projection: it is the filter a user reaches for.
	if view.Entries[2].Plugin != "com.example.p" || view.Entries[2].Level != "WARN" {
		t.Errorf("the refused call lost its plugin or level: %+v", view.Entries[2])
	}
	if view.Entries[0].At == "" {
		t.Error("a row has no timestamp, so the page cannot show when it happened")
	}
}

func TestLogTailFiltersReachTheRead(t *testing.T) {
	m, dir := diagMarket(t)
	seedLogs(t, dir)

	cases := []struct {
		name string
		q    LogQuery
		want int
	}{
		{"level=warn keeps warn only", LogQuery{Level: "warn"}, 1},
		{"level=debug keeps everything", LogQuery{Level: "debug"}, 4},
		{"scope=market keeps its line", LogQuery{Scope: "market"}, 1},
		{"search by plugin id", LogQuery{Search: "com.example.p"}, 2},
		{"search is case-insensitive", LogQuery{Search: "API.EXAMPLE"}, 1},
		{"limit keeps the newest", LogQuery{Limit: 1}, 1},
	}
	for _, c := range cases {
		view, err := m.LogTail(c.q)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(view.Entries) != c.want {
			t.Errorf("%s: got %d entries, want %d (%+v)", c.name, len(view.Entries), c.want, view.Entries)
		}
	}
}

// TestLogTailExplainsAnEmptyAnswer is the difference between "nothing happened" and
// "nothing has run yet", which is the whole reason the note exists.
func TestLogTailExplainsAnEmptyAnswer(t *testing.T) {
	m, dir := diagMarket(t)

	view, err := m.LogTail(LogQuery{})
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}
	if len(view.Entries) != 0 || !strings.Contains(view.Note, "还没有日志") {
		t.Errorf("an install with no logs was not explained: %+v", view)
	}

	seedLogs(t, dir)
	filtered, err := m.LogTail(LogQuery{Search: "没有这个字符串"})
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}
	if len(filtered.Entries) != 0 || !strings.Contains(filtered.Note, "没有匹配") {
		t.Errorf("a filter that matched nothing was not explained: %+v", filtered)
	}
}
