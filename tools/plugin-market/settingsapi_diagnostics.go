package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"PetPC/internal/logging"
	"PetPC/internal/plugins"
)

// dataFiles is what the 高级 page reports on: every file the two programs put in the shared
// data directory, minus the ones whose CONTENT is the user's business (settings, plugin data).
// Sizes and timestamps answer "is the pet actually writing anything" without copying someone's
// todo list into a bug report.
//
// NO pomodoro*.json, NO todos.json, NO llm-key.bin. The first two moved into their plugins'
// private directories when the host timer and the host todo store retired, and the third is the
// pet's OLD model key: the one-time migration re-encrypts it into the conversation plugin's
// private directory and renames it to `.bak` on the way out (internal/plugins/migrate.go).
// Reporting them would be reporting on files nothing writes any more - and a table that says
// "todos.json 不存在" sends its reader looking for a file whose absence is the intended state.
//
// The two log files are listed here (rather than only in the 日志 section below) so the file
// table answers "has anything ever been logged at all" without parsing a line: a pet.log that
// keeps growing and a pet.log that stopped an hour ago are different diagnoses.
var dataFiles = []string{
	"settings.json",
	"config.json",
	"plugins.json",
	"plugin-market.json",
	"logs/pet.log",
	"logs/market.log",
}

// logTailLines bounds the log tail a report may contain. A report that has to be scrolled is
// a report nobody reads, and the full log is one click away in the 日志 page.
const logTailLines = 40

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
	installed := make(map[string]bool, len(rows))
	for _, r := range rows {
		installed[r.ID] = true
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
		for _, problem := range pluginProblems(m, r, installed) {
			b.WriteString("    " + problem + "\n")
		}
	}

	b.WriteString("\n== 警告 ==\n")
	if warnings := m.manager.Warnings(); len(warnings) == 0 {
		b.WriteString("（无）\n")
	} else {
		for _, w := range warnings {
			b.WriteString(w + "\n")
		}
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
		// INFO and above, NOT the level the files happen to be at. At DEBUG the pet logs the
		// words that went to the model and came back (app_plugins.go's logChatRequest), and this
		// report is copied into a chat window - so this section stays on the side of "safe to
		// paste", which is the only reason it exists. Someone actually debugging reads the 日志
		// page, where the level is theirs to choose.
		MinLevel: logging.LevelInfo,
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
// Missing files are listed rather than skipped. "config.json 不存在" is information - it says
// the pet has never run against this data directory - whereas a list that silently omits it
// says nothing at all.
func writeFileLine(b *strings.Builder, dir, name string) {
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		fmt.Fprintf(b, "%-24s 不存在\n", name)
		return
	}
	fmt.Fprintf(b, "%-24s %9d B  %s\n", name, info.Size(), info.ModTime().Format("2006-01-02 15:04:05"))
}

// pluginProblems lists what is WRONG with one installed plugin, in the words a bug report needs.
//
// # WHY THESE, AND WHY HERE
//
// Every line below is a failure that looks like "the plugin does nothing", produces no error at
// install time, and is invisible from the outside:
//
//   - a permission the user never ticked ("我都授权了" is the report that turns out to be false);
//   - a helper script the package does not carry (the host would log 找不到脚本 every 5 seconds);
//   - a callTargets entry pointing at a plugin nobody installed (the call can only fail);
//   - the plugin being switched off, which stops its resident frame AND every call to it.
//
// One line for the disk it occupies, because "why is %AppData% 400 MB" is the other question
// this report gets asked. There is no ceiling any more (2026-11-01), so the number stands
// alone instead of being read as a percentage of something.
func pluginProblems(m *Market, row plugins.Installed, installed map[string]bool) []string {
	var out []string
	if !row.Enabled {
		out = append(out, "停用中：它的常驻帧不会跑，别的插件也调用不到它")
	}
	if row.Manifest == nil {
		return out
	}
	granted := make(map[string]bool, len(row.Granted))
	for _, perm := range row.Granted {
		granted[perm] = true
	}
	var missing []string
	for _, perm := range row.Manifest.Permissions {
		if !granted[perm] {
			missing = append(missing, perm)
		}
	}
	if len(missing) > 0 {
		out = append(out, "缺授权："+strings.Join(missing, "、")+"（装的时候没勾，或后来取消了）")
	}
	if spec := row.Manifest.Sidecar; spec != nil {
		script := filepath.Join(row.Dir, filepath.FromSlash(spec.Script))
		if _, err := os.Stat(script); err != nil {
			out = append(out, "辅助脚本不在："+spec.Script+"（包里没有它，宿主会一直启动失败）")
		}
	}
	var absent []string
	for _, entry := range row.Manifest.CallTargets {
		target, method, ok := plugins.SplitCallTarget(entry)
		if !ok || installed[target] {
			continue
		}
		absent = append(absent, target+"/"+method)
	}
	if len(absent) > 0 {
		out = append(out, "callTargets 指向没装的插件："+strings.Join(absent, "、")+"（调用会失败）")
	}
	if row.ID != "" {
		if dir, err := m.manager.PrivateDir(row.ID); err == nil {
			if used := plugins.PluginDataBytes(dir); used > 0 {
				out = append(out, fmt.Sprintf("私有目录占用 %s（宿主不设上限；在控制中心的「插件」页可以看明细）", humanBytes(used)))
			}
		}
	}
	return out
}

// humanBytes renders a size the way a person reading a bug report wants it.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}