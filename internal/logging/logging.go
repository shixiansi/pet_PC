// Package logging is the one log both PetPC programs write through.
//
// # WHY THIS EXISTS
//
// There WAS a log before this package, and it had three properties that made it
// useless exactly when it was needed:
//
//  1. it only existed when the user launched with PETPC_DEBUG=1 (App.DebugLog), so a
//     report of "the plugin said nothing" arrived with no evidence at all;
//  2. every line had the same weight. A tray icon failing to register and a
//     pointer-drag probe were indistinguishable, in the same file, in arrival order;
//  3. it was write-only. Nothing in either program could show it, so reading it meant
//     knowing that %AppData%\PetPC\debug.log exists.
//
// A log nobody can read is a log nobody writes carefully, and that is the state this
// package replaces: levels that mean something, always on, bounded on disk, and
// readable by the control center's 日志 page through Read.
//
// # WHAT IS NEVER LOGGED
//
// The message text of a conversation, the contents of a todo, the value of a plugin
// setting and any credential. Callers log SIZES, COUNTS, NAMES and OUTCOMES. That rule
// is not a style preference: these files are meant to be pasted into a bug report
// (see the market's Diagnostics), and a single careless `log.Debug("llm", payload)`
// would make every report unsafe to send. The one place a string from outside may
// appear is an error message, and those are already written on the assumption that the
// user will read them (internal/llm's redact, pluginError bodies).
//
// # THE LINE FORMAT
//
// One line per entry, five space-separated fields:
//
//	2026-10-28T14:32:00.123+08:00 INFO plugin com.shixiansi.plugin-chat llm.chat ok in 812ms
//	│                              │    │      │                        └─ the message
//	│                              │    │      └─ the plugin, or "-"
//	│                              │    └─ the subsystem (pet, plugin, llm, …)
//	│                              └─ the level
//	└─ local time WITH its offset
//
// The trailing field format is deliberate on this repo's terms: a file a user opens in
// Notepad is the first thing anyone does with it, so no JSON, no tabs that collapse,
// and no column padding that would make the split ambiguous. SplitLine is the one
// implementation of the read side, so the writer and the 日志 page cannot disagree.
//
// The zone offset is carried because "the reminder fired at 09:05" means nothing
// without it, and deriving the zone from the machine reading the file is the same class
// of bug as assuming everything is UTC.
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Level is how loud one entry is.
//
// The zero value is LevelDebug, so a Logger built as a bare literal is a debug logger
// rather than a silent one - the opposite of a gate, and the right default for a
// component that has no opinion.
type Level int8

const (
	// LevelDebug is the detail only interesting while chasing a bug: every request,
	// every frame of a stream, every pointer move the page reports.
	LevelDebug Level = iota
	// LevelInfo is the story of the program: started, plugin enabled, model answered.
	LevelInfo
	// LevelWarn is something that did not work but that the program survived
	// (a failed crop, a refused permission, a model that 429'd twice).
	LevelWarn
	// LevelError is a failure the user is going to notice.
	LevelError
)

// String is the token written into the file and shown on the 日志 page.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	}
	return "LEVEL" + strconv.Itoa(int(l))
}

// ParseLevel reads a level name, case-insensitively.
//
// It also accepts the Chinese words the 日志 page offers, because the level tokens are
// user-visible text there and a filter that only accepts English is a filter half the
// users cannot type.
func ParseLevel(s string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "调试":
		return LevelDebug, true
	case "info", "信息", "通知":
		return LevelInfo, true
	case "warn", "warning", "警告":
		return LevelWarn, true
	case "error", "err", "错误":
		return LevelError, true
	}
	return LevelInfo, false
}

// LevelOfOrDefault is ParseLevel's forgiving form, for environment variables and query
// strings: an unreadable name keeps the program running at the default level instead of
// refusing to start over a typo.
func LevelOfOrDefault(s string, fallback Level) Level {
	if lv, ok := ParseLevel(s); ok {
		return lv
	}
	return fallback
}

// TimeLayout is the timestamp at the head of every line: local clock, millisecond
// resolution, zone offset included.
const TimeLayout = "2006-01-02T15:04:05.000Z07:00"

// NoPlugin fills the plugin column of an entry that belongs to no plugin.
//
// A placeholder rather than an empty field: the format is positional, and a skipped
// column would shift the scope of every line that follows it.
const NoPlugin = "-"

// maxMessage caps one entry's message.
//
// A log line is read by a human in a scrolling list; a 200 KB error body in one line
// costs the page its layout and the reader their place. The cap is generous enough for
// a full Go error chain.
const maxMessage = 4000

// DefaultMaxBytes is when one file is rotated.
//
// Two files per program (the live one and one generation back) is the whole retention
// policy: enough to see what happened just before a restart, bounded enough that a
// plugin in a reconnect loop cannot fill the user's disk. It is deliberately NOT
// configurable - a retention knob nobody can describe the failing case of is a knob
// that only produces support questions.
const DefaultMaxBytes = 2 << 20

// DirName is the subdirectory of the data directory the logs live in.
//
// A directory rather than files at the top: the data directory is the user's, its top
// level is already a list of six files, and "logs" is the one entry they can delete
// without losing anything. It also keeps a rotated generation out of the way of
// anything that scans the top level.
const DirName = "logs"

// Entry is one log line, structured.
type Entry struct {
	At      time.Time
	Level   Level
	Scope   string
	Plugin  string
	Message string
}

// Line renders the entry in the on-disk format.
//
// The message is flattened first: a newline inside it would split one entry into two
// lines, and the second half would then be unparseable garbage in the 日志 page.
func (e Entry) Line() string {
	scope := e.Scope
	if scope == "" {
		scope = "app"
	}
	plugin := e.Plugin
	if plugin == "" {
		plugin = NoPlugin
	}
	return strings.Join([]string{
		e.At.Format(TimeLayout),
		e.Level.String(),
		flatten(scope),
		flatten(plugin),
		truncate(flatten(e.Message)),
	}, " ")
}

// ParseLine is the inverse of Line, and the ONLY reader of the format.
//
// It reports false for a line it cannot read rather than guessing: a hand-edited file,
// a half-written last line after a power cut, and a message that itself contains the
// separator all have to be survivable, and dropping the line is the survivable answer.
func ParseLine(line string) (Entry, bool) {
	parts := strings.SplitN(strings.TrimRight(line, "\r\n"), " ", 5)
	if len(parts) < 5 {
		return Entry{}, false
	}
	at, err := time.Parse(TimeLayout, parts[0])
	if err != nil {
		return Entry{}, false
	}
	lv, ok := ParseLevel(parts[1])
	if !ok {
		return Entry{}, false
	}
	plugin := parts[3]
	if plugin == NoPlugin {
		plugin = ""
	}
	return Entry{At: at, Level: lv, Scope: parts[2], Plugin: plugin, Message: parts[4]}, true
}

// Options configures one program's logger.
type Options struct {
	// MaxBytes overrides DefaultMaxBytes. Tests use a tiny value to exercise rotation.
	MaxBytes int64
	// Level is the minimum level that reaches the file. The zero value is LevelDebug;
	// callers that want the shipping default pass LevelInfo explicitly.
	Level Level
	// Now is the clock, for tests. Nil means time.Now.
	Now func() time.Time
}

// Logger writes one program's log.
//
// It is safe for concurrent use and every method tolerates a nil receiver, because the
// alternative - an `if a.log != nil` at each of eighty call sites, one of which will be
// forgotten - turns a log line into a crash in exactly the code path that is already
// going wrong.
type Logger struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	level    Level
	now      func() time.Time
	// err is the last failure to write. It is kept rather than returned because a
	// caller cannot do anything about it, and the 日志 page reads it to explain why
	// the file it is showing has stopped growing.
	err error
}

// Open starts (or continues) the log called name, inside dir.
//
// It never fails. A program that cannot create its log directory has a problem, but not
// one worth refusing to start over: the pet's whole job is to sit on the desktop, and
// the market's is to show a window. The failure is recorded and surfaces in Err and in
// the 日志 page's note; every Log call becomes a no-op until then.
func Open(dir, name string, opts Options) *Logger {
	l := &Logger{
		maxBytes: opts.MaxBytes,
		level:    opts.Level,
		now:      opts.Now,
	}
	if l.maxBytes <= 0 {
		l.maxBytes = DefaultMaxBytes
	}
	if l.now == nil {
		l.now = time.Now
	}
	l.path = filepath.Join(dir, DirName, name+".log")
	return l
}

// Path is where this logger writes.
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// Level is the minimum level currently reaching the file.
func (l *Logger) Level() Level {
	if l == nil {
		return LevelError
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

// SetLevel changes the minimum level. It exists for the PETPC_LOG_LEVEL override, which
// has to be applied before anything is written.
func (l *Logger) SetLevel(lv Level) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = lv
}

// Err reports why writes stopped, or nil.
func (l *Logger) Err() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// Log is the one entry point every convenience method funnels through.
func (l *Logger) Log(lv Level, scope, plugin, msg string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if lv < l.level {
		return
	}
	if err := l.writeLocked(Entry{
		At:      l.now(),
		Level:   lv,
		Scope:   scope,
		Plugin:  plugin,
		Message: msg,
	}); err != nil {
		l.err = err
	}
}

// Debug / Info / Warn / Error are the four scopes' worth of shorthand, in the order the
// level list declares them.
func (l *Logger) Debug(scope, msg string) { l.Log(LevelDebug, scope, "", msg) }
func (l *Logger) Info(scope, msg string)  { l.Log(LevelInfo, scope, "", msg) }
func (l *Logger) Warn(scope, msg string)  { l.Log(LevelWarn, scope, "", msg) }
func (l *Logger) Error(scope, msg string) { l.Log(LevelError, scope, "", msg) }

// Debugf / Infof / Warnf / Errorf are the printf forms. They exist because most call
// sites are building a sentence out of two or three values, and `a.log.Warn("x", "y: "+
// err.Error())` reads worse than the format string it replaces.
func (l *Logger) Debugf(scope, format string, args ...any) {
	l.Log(LevelDebug, scope, "", fmt.Sprintf(format, args...))
}
func (l *Logger) Infof(scope, format string, args ...any) {
	l.Log(LevelInfo, scope, "", fmt.Sprintf(format, args...))
}
func (l *Logger) Warnf(scope, format string, args ...any) {
	l.Log(LevelWarn, scope, "", fmt.Sprintf(format, args...))
}
func (l *Logger) Errorf(scope, format string, args ...any) {
	l.Log(LevelError, scope, "", fmt.Sprintf(format, args...))
}

// Pluginf logs on behalf of one installed plugin, so the 日志 page can filter by it.
//
// Separate from the scope-taking methods because the plugin id is the single most
// useful filter on that page: "the chat plugin never answers" is answered by one id and
// a level, and a sentence that merely mentions the id cannot be filtered on.
func (l *Logger) Pluginf(lv Level, scope, plugin, format string, args ...any) {
	l.Log(lv, scope, plugin, fmt.Sprintf(format, args...))
}

// writeLocked appends one entry, rotating first when the file is full.
//
// THE FILE IS NOT HELD OPEN BETWEEN LINES, and that is a deliberate choice with three
// separate payoffs - it was found the hard way, when every test that built a Market failed
// its temp-directory cleanup with "the file is being used by another process":
//
//  1. rotation is a rename, and on Windows a rename of a file some process has open FAILS.
//     Holding the handle meant the rotation silently fell back to truncating, i.e. the
//     "previous generation" the 日志 page advertises did not exist;
//  2. the 日志 page (a different process) opens the same file to read it, and Notepad does
//     too when a user double-clicks it - an open writer makes both fail;
//  3. nothing has to be closed on exit, so a hard kill cannot leave a half-flushed line.
//
// The cost is one open/close per line, which at this program's volume (a few lines a
// second in the worst case) is nothing.
func (l *Logger) writeLocked(e Entry) error {
	line := e.Line() + "\n"
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	// Stat before opening, so the rotation decision is made from the file's real size:
	// two programs never share a file here, but a user can delete it while the pet runs,
	// and a tracked counter would then grow without ever rotating.
	if info, err := os.Stat(l.path); err == nil && info.Size()+int64(len(line)) > l.maxBytes {
		l.rotateLocked()
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

// rotateLocked moves the live file aside, keeping exactly one generation.
//
// No handle is open at this point (see writeLocked), so the rename is the ordinary case.
// What remains possible is ANOTHER program holding it - the 日志 page reading, or the user
// with the file open in an editor - and then the rename fails. Truncating is the fallback:
// the alternative is a log that grows without bound, which is the one thing the size cap
// exists to prevent.
func (l *Logger) rotateLocked() {
	prev := l.path + ".1"
	_ = os.Remove(prev)
	if err := os.Rename(l.path, prev); err != nil {
		_ = os.Truncate(l.path, 0)
	}
}

// flatten removes line breaks and collapses the whitespace a multi-line message would
// otherwise put into the middle of a one-line format.
func flatten(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " / ")
	s = strings.ReplaceAll(s, "\n", " / ")
	s = strings.ReplaceAll(s, "\r", " / ")
	return s
}

func truncate(s string) string {
	if len(s) <= maxMessage {
		return s
	}
	// Cut on a rune boundary, so the file stays valid UTF-8 for every reader.
	cut := maxMessage
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + " …(已截断)"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
