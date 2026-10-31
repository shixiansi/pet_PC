package main

// A sidecar's own words, in the host's log.
//
// # WHY THIS EXISTS
//
// `process.launch` gives the child no pipes on purpose: a program the PLUGIN started must not be
// able to block the pet by not reading its stdin, and no file descriptor of ours may keep it alive
// after the pet exits. That reasoning does not transfer to a SIDECAR - a script the HOST started,
// with the host's environment, dying for reasons only it knows.
//
// 2026-10-31, the day after sidecars shipped: ten of them were started across two launches and
// EIGHT died within seconds (pet-sense / media-sync / privacy-guard / clipask produced no files at
// all). The log said "已启动辅助脚本" and then nothing, ever - a PowerShell script's exception goes
// to stderr, and stderr was /dev/null. The only symptom was a plugin warning "辅助脚本可能没起来",
// which is a guess, not evidence.
//
// So a sidecar's output is captured: stderr at WARN (failures), stdout at DEBUG (chatter). The
// `process.launch` path keeps its /dev/null, and there is a test for that difference.

import (
	"bytes"
	"path/filepath"
	"strings"
	"sync"

	"PetPC/internal/desktop"
	"PetPC/internal/logging"
)

const (
	// sidecarLineMaxRunes bounds one logged line. A PowerShell stack trace is hundreds of
	// characters and the useful part is at the start; the log is not a debugger.
	sidecarLineMaxRunes = 300
	// sidecarBufferMax flushes a stream that never emits a newline, so a helper printing dots
	// cannot grow this buffer without bound.
	sidecarBufferMax = 4000
)

// sidecarLogWriter turns one output stream of one sidecar into log lines.
//
// It is an io.Writer, which is what `exec.Cmd` wants: the runtime then creates the pipe and copies
// from it on its own goroutine, so nothing here can deadlock the child.
type sidecarLogWriter struct {
	log      *logging.Logger
	level    logging.Level
	plugin   string
	stream   string
	throttle *logging.Throttle

	mu  sync.Mutex
	buf []byte
}

func newSidecarLogWriter(log *logging.Logger, level logging.Level, plugin, stream string) *sidecarLogWriter {
	return &sidecarLogWriter{
		log:    log,
		level:  level,
		plugin: plugin,
		stream: stream,
		// The same five-second window the plugin call log uses: one line per (plugin, stream)
		// per window, with the swallowed count attached. A helper that logs in a loop must not
		// own the file.
		throttle: logging.NewThrottle(logging.DefaultThrottleWindow, nil),
	}
}

func (w *sidecarLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := w.buf[:i]
		w.buf = w.buf[i+1:]
		w.emit(decodeSidecarLine(line))
	}
	if len(w.buf) >= sidecarBufferMax {
		w.emit(decodeSidecarLine(w.buf))
		w.buf = w.buf[:0]
	}
	return len(p), nil
}

// decodeSidecarLine turns one raw line into text, dropping the CR of a CRLF pair.
//
// The DECODING is the load-bearing half and it is not UTF-8: PowerShell writes the machine's
// ANSI code page, so the old `string(bytes)` produced the mojibake that made these very log
// lines useless (see desktop.DecodeChildOutput). Splitting the CR off BEFORE decoding is
// deliberate: a stray 0x0D is ASCII either way, and trimming it inside the decoder would make
// that function locale-dependent.
func decodeSidecarLine(raw []byte) string {
	if n := len(raw); n > 0 && raw[n-1] == '\r' {
		raw = raw[:n-1]
	}
	return desktop.DecodeChildOutput(raw)
}

// Flush logs whatever is left without a trailing newline. Called when the child exits: the last
// thing a dying helper printed is usually the interesting one, and it has no newline after it.
func (w *sidecarLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(decodeSidecarLine(w.buf))
		w.buf = w.buf[:0]
	}
}

func (w *sidecarLogWriter) emit(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if runes := []rune(line); len(runes) > sidecarLineMaxRunes {
		line = string(runes[:sidecarLineMaxRunes]) + "…（截断）"
	}
	// Keyed by the LINE, not by the stream: a stack trace is several DISTINCT lines and all of them
	// are the evidence, while a helper stuck in a retry loop prints the SAME line forever. So
	// identical repeats collapse (with a count) and distinct ones all get through.
	allowed, swallowed := w.throttle.Allow("sidecar|" + w.plugin + "|" + w.stream + "|" + line)
	if !allowed {
		return
	}
	if swallowed > 0 {
		w.log.Pluginf(w.level, "sidecar", w.plugin, "%s（同类另有 %d 行已合并）", line, swallowed)
		return
	}
	w.log.Pluginf(w.level, "sidecar", w.plugin, "%s", line)
}

// sidecarScriptEncoding explains why a .ps1 will not even PARSE, or returns "" when it will.
//
// # THE TRAP THIS NAMES
//
// Windows PowerShell 5.1 decodes a BOM-less script with the machine's ANSI code page. A file
// saved as UTF-8 without a BOM therefore has its Chinese comments read as GBK, and a
// multi-byte character can eat the quote or brace that follows it - the script then dies at
// startup with `Missing closing '}'` pointing at a perfectly good line, and the real cause is
// invisible. AGENTS.md §7 records the same trap for this repo's own `.ps1` files; the sidecar
// launcher hits it for every plugin's script, and on 2026-10-31 it is why four of them never
// produced a single byte (the plugins' own warning - "辅助脚本可能没起来" - was a guess; this
// line is the diagnosis).
//
// It is deliberately a WARNING and not a refusal: the host cannot re-encode somebody's file
// (a re-encoded copy would change `$PSScriptRoot`-relative behaviour, and silently rewriting
// an installed package is worse than a bad script), and a script whose non-ASCII bytes happen
// to survive mis-decoding does still run. What the host owes the user is the sentence, in
// front of the mojibake.
func sidecarScriptEncoding(path string, body []byte) string {
	if !strings.EqualFold(filepath.Ext(path), ".ps1") {
		// .exe sidecars are binaries; their encoding is not a text question.
		return ""
	}
	if bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		return ""
	}
	nonASCII := false
	for _, b := range body {
		if b >= 0x80 {
			nonASCII = true
			break
		}
	}
	if !nonASCII {
		return ""
	}
	return "脚本没有 UTF-8 BOM 而含非 ASCII 字符：Windows PowerShell 5.1 会按本机 ANSI 代码页解析它，" +
		"一句中文注释就能吃掉后面的引号或花括号，脚本会在启动时抛 ParserError（行号指向的位置往往看不出问题）。" +
		"给它加上 UTF-8 BOM，或把脚本写成纯 ASCII（AGENTS.md §7）"
}
