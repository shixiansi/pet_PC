package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/logging"
)

// hostKeyFile is the file the PET keeps its LLM API key in, DPAPI-encrypted
// (app_llm.go's llmKeyFile). This program never reads it, never writes it and never
// decrypts it - it only says whether the file is there, because "密钥没配" and "密钥配了
// 但插件还是没人回答" are two very different bug reports and the user cannot tell them
// apart from the outside.
const hostKeyFile = "llm-key.bin"

// dataFiles is what the 高级 page reports on: every file the two programs put in the shared
// data directory, minus the ones whose CONTENT is the user's business (todos, settings).
// Sizes and timestamps answer "is the pet actually writing anything" without copying
// someone's todo list into a bug report.
//
// No pomodoro*.json: those two were the pet's timer state and its archive, and both moved into
// the pomodoro plugin's private directory (plugin-data/com.shixiansi.plugin-pomodoro/) when the
// host timer retired. A file this page reports on has to be a file one of these two programs
// still writes.
//
// The two log files are listed here (rather than only in the 日志 section below) so the file
// table answers "has anything ever been logged at all" without parsing a line: a pet.log that
// keeps growing and a pet.log that stopped an hour ago are different diagnoses.
var dataFiles = []string{
	"settings.json",
	"config.json",
	"plugins.json",
	"plugin-market.json",
	"todos.json",
	hostKeyFile,
	"logs/pet.log",
	"logs/market.log",
}

// logTailLines bounds the log tail a report may contain. A report that has to be scrolled is
// a report nobody reads, and the full log is one click away in the 日志 page.
const logTailLines = 40

// HostKey is what this program may honestly say about the pet's LLM key.
//
// A struct rather than a bare bool: the page has to be able to say WHICH file it looked at,
// and "没有密钥" said about the wrong path is worse than saying nothing.
type HostKey struct {
	Path string
	// Present is "a non-empty encrypted file exists". It is NOT "the key works": only the
	// pet can judge that, because only the pet decrypts it.
	Present bool
}

// HostKey reports the pet's encrypted key file, without opening it.
func (m *Market) HostKey() HostKey {
	path := desktop.NewSecretStore(m.dataDir, hostKeyFile).Path()
	info, err := os.Stat(path)
	return HostKey{Path: path, Present: err == nil && info.Size() > 0}
}

// Diagnostics returns a plain-text report, for a bug report or for the user's own "是什么在
// 占地方" question.
//
// WHY A TEXT BLOB AND NOT A STRUCT: what this is FOR is being pasted somewhere. A struct
// would be re-assembled by the page and then copied anyway, and the one thing that must be
// true - that no secret and no todo text ends up in it - is easiest to hold when the whole
// report is generated in one function and a test can assert on the exact bytes.
func (m *Market) Diagnostics() (string, error) {
	var b strings.Builder

	b.WriteString("PetPC 诊断报告\n")
	fmt.Fprintf(&b, "生成时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "数据目录: %s\n", m.dataDir)

	b.WriteString("\n== 文件 ==\n")
	for _, name := range dataFiles {
		writeFileLine(&b, m.dataDir, name)
	}

	b.WriteString("\n== 已装插件 ==\n")
	rows := m.manager.Plugins()
	if len(rows) == 0 {
		b.WriteString("（没有已装插件）\n")
	}
	for _, r := range rows {
		state := "启用"
		if !r.Enabled {
			state = "停用"
		}
		declared := 0
		name := ""
		if r.Manifest != nil {
			name = r.Manifest.Name
			declared = len(r.Manifest.Permissions)
		}
		line := fmt.Sprintf("%s  v%s  %s  权限 %d（已授 %d）", r.ID, r.Version, state, declared, len(r.Granted))
		if name != "" {
			line += "  " + name
		}
		if r.Error != "" {
			// The reason matters more than the row: a plugin whose manifest no longer
			// parses is exactly the case where a screenshot says "empty" and nothing else.
			line += "\n    manifest: " + r.Error
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n== 警告 ==\n")
	if warnings := m.manager.Warnings(); len(warnings) == 0 {
		b.WriteString("（无）\n")
	} else {
		for _, w := range warnings {
			b.WriteString(w + "\n")
		}
	}

	b.WriteString("\n== 密钥 ==\n")
	if key := m.HostKey(); key.Present {
		fmt.Fprintf(&b, "%s 存在（DPAPI 密文，本程序不解密）\n", filepath.Base(key.Path))
	} else {
		fmt.Fprintf(&b, "%s 不存在（宿主里还没有配置大模型密钥）\n", filepath.Base(key.Path))
	}

	b.WriteString("\n== 日志 ==\n")
	writeLogSection(&b, m.dataDir)

	return b.String(), nil
}

// writeLogSection prints where the logs are, how big they are, and their tail.
//
// The tail is REAL log lines (internal/logging.Read parses the same file each program
// writes), formatted the way the 日志 page shows them, and bounded by logTailLines. A
// report is copied into a chat window, so the section has to be short enough to read and
// honest about what it left out.
//
// The level note is not an excuse, it is the second half of the diagnosis: the logs are
// always written now (INFO and above), so an empty one means "neither program has run
// yet" or "the log directory is not writable" - never "you forgot a flag".
func writeLogSection(b *strings.Builder, dir string) {
	fmt.Fprintf(b, "目录: %s（轮转上限 %d KB，每个程序两个文件）\n", loggingDir(dir), logging.DefaultMaxBytes/1024)
	files := logging.Files(dir, logging.PetLog, logging.MarketLog)
	any := false
	for _, f := range files {
		switch {
		case f.Err != "":
			fmt.Fprintf(b, "%-24s 读不出来：%s\n", f.Name, f.Err)
		case !f.Exists():
			fmt.Fprintf(b, "%-24s 不存在\n", f.Name)
		default:
			any = true
			fmt.Fprintf(b, "%-24s %9d B  %s\n", f.Name, f.Bytes, f.Modified.Format("2006-01-02 15:04:05"))
		}
	}

	entries, err := logging.Read(dir, []string{logging.PetLog, logging.MarketLog}, logging.Query{
		MinLevel: logging.LevelDebug,
		Limit:    logTailLines,
	})
	if err != nil {
		b.WriteString("读日志失败：" + err.Error() + "\n")
		return
	}
	if len(entries) == 0 {
		if any {
			b.WriteString("（日志文件存在，但里面没有一行可读的记录）\n")
		} else {
			b.WriteString("（还没有日志：桌宠和控制中心都没在这个数据目录里跑过。默认级别 INFO 一直在写，不需要任何环境变量。）\n")
		}
		return
	}
	b.WriteString("\n")
	if len(entries) >= logTailLines {
		fmt.Fprintf(b, "（只显示最后 %d 行；完整日志在控制中心的「日志」页，或上面的文件里）\n", logTailLines)
	}
	for _, e := range entries {
		plugin := e.Plugin
		if plugin == "" {
			plugin = "-"
		}
		fmt.Fprintf(b, "%s %-5s %s %s %s\n",
			e.At.Format("2006-01-02 15:04:05.000"), e.Level.String(), e.Scope, plugin, e.Message)
	}
}

// writeFileLine prints one line per file: size and mtime, or why it is not there.
//
// Missing files are listed rather than skipped. "todos.json 不存在" is information - it says
// the pet has never written one - whereas a list that silently omits it says nothing at all.
func writeFileLine(b *strings.Builder, dir, name string) {
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		fmt.Fprintf(b, "%-24s 不存在\n", name)
		return
	}
	fmt.Fprintf(b, "%-24s %9d B  %s\n", name, info.Size(), info.ModTime().Format("2006-01-02 15:04:05"))
}
