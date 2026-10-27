package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"PetPC/internal/desktop"
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
var dataFiles = []string{
	"settings.json",
	"config.json",
	"plugins.json",
	"plugin-market.json",
	"todos.json",
	hostKeyFile,
	"debug.log",
}

// logTailLines and logTailBytes bound what a report may contain. A debug.log that grew to a
// megabyte must not make the page unreadable, and a report that has to be scrolled is a
// report nobody reads.
const (
	logTailLines = 40
	logTailBytes = 8 << 10
)

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

	b.WriteString("\n== 日志尾 ==\n")
	tail, note := readLogTail(filepath.Join(m.dataDir, "debug.log"))
	if note != "" {
		b.WriteString(note + "\n")
	}
	b.WriteString(tail)

	return b.String(), nil
}

// writeFileLine prints one line per file: size and mtime, or why it is not there.
//
// Missing files are listed rather than skipped. "todos.json 不存在" is information - it says
// the pet has never written one - whereas a list that silently omits it says nothing at all.
func writeFileLine(b *strings.Builder, dir, name string) {
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		fmt.Fprintf(b, "%-24s 不存在\n", name)
		return
	}
	fmt.Fprintf(b, "%-24s %9d B  %s\n", name, info.Size(), info.ModTime().Format("2006-01-02 15:04:05"))
}

// readLogTail returns the last lines of debug.log, and a note when there is nothing to show.
//
// The note is not an excuse, it is the second half of the diagnosis: debug.log only exists
// at all when the pet runs with PETPC_DEBUG set (see App.DebugLog), so an empty log usually
// means "the flag was off", not "nothing happened". Saying that here is the difference
// between a report somebody can act on and a section that looks broken.
func readLogTail(path string) (tail, note string) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "debug.log 不存在：它只在桌宠以 PETPC_DEBUG=1 启动时才被写，平时是正常的。"
	}
	if info.Size() == 0 {
		return "", "debug.log 存在但是空的：设了 PETPC_DEBUG 之后才会有内容。"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "debug.log 读不出来：" + err.Error()
	}
	if len(data) > logTailBytes {
		// Cutting at a byte boundary can split a UTF-8 rune; the replacement character that
		// costs is cheaper than a report that fails to render.
		data = data[len(data)-logTailBytes:]
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > logTailLines {
		lines = lines[len(lines)-logTailLines:]
		note = fmt.Sprintf("（前面还有内容，只显示最后 %d 行）", logTailLines)
	}
	return strings.Join(lines, "\n") + "\n", note
}
