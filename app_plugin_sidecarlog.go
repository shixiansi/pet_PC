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
	"strings"
	"sync"

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
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		w.emit(line)
	}
	if len(w.buf) >= sidecarBufferMax {
		w.emit(string(w.buf))
		w.buf = w.buf[:0]
	}
	return len(p), nil
}

// Flush logs whatever is left without a trailing newline. Called when the child exits: the last
// thing a dying helper printed is usually the interesting one, and it has no newline after it.
func (w *sidecarLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
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
