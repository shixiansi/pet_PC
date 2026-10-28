package main

// The 日志 page's half of the log: the query it sends and the rows it draws.
//
// It reads the FILES, not some in-memory ring buffer, and that is the whole design:
// the interesting lines come from the OTHER process. When the user says "对话插件不回话",
// the evidence is in the pet's log, while the window they are looking at belongs to this
// program. A buffer here could only ever show this program's own half.
//
// The reading itself is internal/logging.Read - the same package that writes the lines,
// so the format has one implementation and the page cannot silently show a shorter log
// than the file holds.

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"PetPC/internal/logging"
)

// logLevelFromEnv resolves this program's log level, with the pet's precedence.
//
// Duplicated in the two programs because there is no shared HOME for "how this install
// is configured" - the pet keeps its settings in settings.json and this program in
// plugin-market.json, and neither owns an environment policy. Two four-line functions
// that must agree is a smaller cost than a third file whose only job is to be imported
// by both; the names they read (PETPC_LOG_LEVEL, PETPC_DEBUG) are in README so a user
// sets one thing for both programs.
func logLevelFromEnv() logging.Level {
	if raw := strings.TrimSpace(os.Getenv("PETPC_LOG_LEVEL")); raw != "" {
		return logging.LevelOfOrDefault(raw, logging.LevelInfo)
	}
	if os.Getenv("PETPC_DEBUG") != "" {
		return logging.LevelDebug
	}
	return logging.LevelInfo
}

// sortedKeys lists a settings write's keys in a stable order, for a log line.
//
// Sorted rather than map order because a log line that reorders itself between two
// otherwise identical saves is a line nobody can diff.
func sortedKeys(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for k := range values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// LogQuery is what the page asks for.
//
// Plain fields rather than an opaque blob: Wails generates the TypeScript for a struct
// parameter, and a page that had to build a query string would be a second place the
// filter vocabulary lives.
type LogQuery struct {
	// Level is a MINIMUM ("info" keeps info, warn and error). "" or "debug" keeps
	// everything.
	Level string `json:"level"`
	// Scope keeps one subsystem ("plugin", "llm", "pet", …); "" keeps all.
	Scope string `json:"scope"`
	// Search is a case-insensitive substring of the message or the plugin id.
	Search string `json:"search"`
	// Limit is how many of the newest rows to return; 0 uses the default.
	Limit int `json:"limit"`
}

// LogLine is one row of the page, already formatted for display.
//
// The time is a STRING in the file's own layout rather than a time.Time: the page would
// otherwise re-derive a zone from the browser's clock, and these lines are evidence of
// what happened on the user's machine at the user's local time. The millisecond
// precision and the offset are already in it.
type LogLine struct {
	At      string `json:"at"`
	Level   string `json:"level"`
	Scope   string `json:"scope"`
	Plugin  string `json:"plugin"`
	Message string `json:"message"`
}

// LogFileView is one row of the page's file list.
type LogFileView struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Bytes is 0 for a file that is not there, which the page shows as an em dash.
	Bytes int64 `json:"bytes"`
	// Modified is the local time of the last write, "" when the file is absent.
	Modified string `json:"modified"`
	// Err explains an unreadable file.
	Err string `json:"err"`
}

// LogView is one answer from LogTail.
type LogView struct {
	// Entries are NEWEST FIRST, which is what a monitoring view wants: the row that
	// just appeared is at the top, and a poll that brings three more lines does not
	// move the one the user is reading.
	Entries []LogLine     `json:"entries"`
	Files   []LogFileView `json:"files"`
	// Scopes are the subsystems present in the read, for the filter dropdown.
	Scopes []string `json:"scopes"`
	// Dir is where the files live, so the page can show a path the user can open.
	Dir string `json:"dir"`
	// Note explains an empty list. Empty itself when there are entries.
	//
	// It exists because "nothing to see" has four different meanings here (the pet
	// has never run, the level filter is too high, the search matched nothing, the
	// file is not writable) and a blank panel answers none of them. This is the same
	// reason diagnostics.go prints a line for a file that is MISSING rather than
	// skipping it.
	Note string `json:"note"`
}

// LogTail returns the newest log lines from both programs.
//
// Both, always: the two programs share one data directory and one user, and a page that
// only showed this program's log would have been unable to answer every question that
// actually gets asked ("为什么插件不说话" is the pet's problem, not the market's).
func (m *Market) LogTail(q LogQuery) (LogView, error) {
	view := LogView{
		Entries: []LogLine{},
		Files:   []LogFileView{},
		Scopes:  []string{},
		Dir:     loggingDir(m.dataDir),
	}

	files := logging.Files(m.dataDir, logging.PetLog, logging.MarketLog)
	anyFile := false
	for _, f := range files {
		if f.Exists() {
			anyFile = true
		}
		row := LogFileView{Name: f.Name, Path: f.Path, Bytes: f.Bytes, Err: f.Err}
		if !f.Modified.IsZero() {
			row.Modified = f.Modified.Format("2006-01-02 15:04:05")
		}
		view.Files = append(view.Files, row)
	}

	entries, err := logging.Read(m.dataDir, []string{logging.PetLog, logging.MarketLog}, logging.Query{
		MinLevel: logging.LevelOfOrDefault(q.Level, logging.LevelDebug),
		Scope:    strings.TrimSpace(q.Scope),
		Search:   q.Search,
		Limit:    q.Limit,
	})
	if err != nil {
		// A read failure is not an empty log, and saying so is the difference between
		// "the pet is quiet" and "this program cannot open the file".
		view.Note = "读日志失败：" + err.Error()
		return view, nil
	}

	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		view.Entries = append(view.Entries, LogLine{
			At:      e.At.Format("2006-01-02 15:04:05.000"),
			Level:   e.Level.String(),
			Scope:   e.Scope,
			Plugin:  e.Plugin,
			Message: e.Message,
		})
	}
	view.Scopes = logging.Scopes(entries)

	if len(view.Entries) == 0 {
		switch {
		case !anyFile:
			view.Note = fmt.Sprintf("还没有日志文件。桌宠和控制中心都会把日志写到 %s，两个程序各跑一次之后这里就会有内容。", view.Dir)
		case q.Search != "" || q.Scope != "" || strings.TrimSpace(q.Level) != "":
			view.Note = "没有匹配的日志行：试试把等级调到「全部」，或者清空搜索与范围。"
		default:
			view.Note = "日志文件是空的。"
		}
	}
	// A write failure in THIS program is the one diagnostic the page cannot get from the
	// files: `market.log` would simply stop growing, which looks exactly like "nothing is
	// happening". Saying it here is the difference between a silent gap and a bug report.
	if werr := m.log.Err(); werr != nil {
		prefix := "这个程序写日志失败（market.log 可能停止增长）：" + werr.Error()
		if view.Note == "" {
			view.Note = prefix
		} else {
			view.Note = prefix + "；" + view.Note
		}
	}
	return view, nil
}

// loggingDir is where this install's logs live, for the page's path line.
//
// The same join logging.Files does, spelled once here so the page and the files cannot
// disagree about the directory they are describing.
func loggingDir(dataDir string) string {
	return strings.TrimRight(dataDir, `\/`) + string(os.PathSeparator) + logging.DirName
}
