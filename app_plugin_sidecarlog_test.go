package main

// The sidecar's output, and the one difference from `process.launch` that matters.
//
// 真机背景（2026-10-31）：两轮启动共 10 个 sidecar，8 个在几秒内退出，而日志里只有「已启动」。
// PowerShell 的异常走 stderr，而 stderr 被丢进 /dev/null——插件那条"辅助脚本可能没起来"是猜测，
// 不是证据。这两个用例钉住"从我这里能看到它说了什么"。

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"PetPC/internal/logging"
)

func TestSidecarLogWriterSplitsTruncatesAndFlushes(t *testing.T) {
	dir := t.TempDir()
	log := logging.Open(dir, "sidecarprobe", logging.Options{Level: logging.LevelDebug})
	w := newSidecarLogWriter(log, logging.LevelWarn, "com.example.p", "stderr")

	// Two lines in one write, no trailing newline on the second.
	if _, err := w.Write([]byte("第一行\n第二行(no newline)")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// A partial line must NOT be logged yet - it may be half of a longer one.
	if body := readLogFile(t, dir); strings.Contains(body, "第二行") {
		t.Fatalf("a line without a newline was logged before Flush:\n%s", body)
	}
	w.Flush()
	body := readLogFile(t, dir)
	if !strings.Contains(body, "第一行") {
		t.Fatalf("the first line is missing:\n%s", body)
	}
	if !strings.Contains(body, "第二行") {
		t.Fatalf("Flush did not emit the trailing partial line:\n%s", body)
	}
	if !strings.Contains(body, "WARN") || !strings.Contains(body, "sidecar") {
		t.Fatalf("the line is not WARN/sidecar-scoped:\n%s", body)
	}

	// Long lines are truncated with a note: a stack trace is not a log entry.
	long := strings.Repeat("长", sidecarLineMaxRunes+50)
	w2 := newSidecarLogWriter(log, logging.LevelWarn, "com.example.q", "stderr")
	if _, err := w2.Write([]byte(long + "\n")); err != nil {
		t.Fatalf("Write(long): %v", err)
	}
	body = readLogFile(t, dir)
	if !strings.Contains(body, "…（截断）") {
		t.Fatalf("a long line was not truncated:\n%s", body)
	}
}

// TestSidecarStderrReachesTheLog runs a real sidecar that writes to stderr.
func TestSidecarStderrReachesTheLog(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("sidecars are .ps1 or .exe; this is Windows-only")
	}
	a := loggedApp(t)
	installSidecarPlugin(t, a, sidecarManifest("com.example.noisy", "noisy.ps1"),
		"noisy.ps1", "[Console]::Error.WriteLine('sidecar boom: 找不到端口 3210')\nStart-Sleep -Seconds 30\n")

	a.ensurePluginSidecars()
	defer killSidecarChildren(t, a)

	deadline := time.Now().Add(15 * time.Second)
	for {
		if strings.Contains(readPetLog(t, a), "sidecar boom") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the sidecar's stderr never reached the log:\n%s", readPetLog(t, a))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestSidecarScriptEncodingNamesTheTrap covers the sentence that turns "脚本一启动就 ParserError"
// into something the user can act on.
//
// The bug it exists for is a byte-level one, so the cases are bytes rather than scripts: a
// BOM-less UTF-8 file with Chinese is the broken shape, and the FIX (add a BOM) has to be
// recognised as healthy - otherwise the warning would fire on every correct plugin too, which
// is how a warning becomes noise nobody reads.
func TestSidecarScriptEncodingNamesTheTrap(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	cases := []struct {
		name string
		path string
		body []byte
		want bool
	}{
		{
			name: "ASCII only, no BOM: Windows PowerShell reads it fine",
			path: "helper.ps1",
			body: []byte("while ($true) { Start-Sleep -Seconds 1 }\n"),
		},
		{
			name: "UTF-8 with BOM: the fix",
			path: "helper.ps1",
			body: append(append([]byte{}, bom...), []byte("# 中文注释\nwhile ($true) { }\n")...),
		},
		{
			name: "BOM-less UTF-8 with Chinese: mis-parsed as the ANSI code page",
			path: "helper.ps1",
			body: []byte("# 采样失败不退出\nwhile ($true) { }\n"),
			want: true,
		},
		{
			name: "an .exe sidecar is a binary, not a text question",
			path: "helper.exe",
			body: []byte{0x4D, 0x5A, 0x90, 0x00, 0xC3, 0xE4},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			why := sidecarScriptEncoding(c.path, c.body)
			if c.want && why == "" {
				t.Fatal("a script that cannot be parsed was not flagged")
			}
			if !c.want && why != "" {
				t.Fatalf("a healthy script was flagged: %s", why)
			}
			if c.want && (!strings.Contains(why, "BOM") || !strings.Contains(why, "PowerShell")) {
				t.Fatalf("the warning does not name the cause and the fix: %s", why)
			}
		})
	}
}

func readLogFile(t *testing.T, dir string) string {
	t.Helper()
	path := dir + string(os.PathSeparator) + "logs" + string(os.PathSeparator) + "sidecarprobe.log"
	raw, err := os.ReadFile(path)
	if err != nil {
		raw, err = os.ReadFile(dir + string(os.PathSeparator) + "sidecarprobe.log")
	}
	if err != nil {
		t.Fatalf("read probe log: %v", err)
	}
	return string(raw)
}

// killSidecarChildren stops whatever this test started: a test must not leave a 30-second script
// behind (that leak is item two of the same goal, and a test has no excuse for it).
func killSidecarChildren(t *testing.T, a *App) {
	t.Helper()
	a.pluginHost.childMu.Lock()
	pids := make([]int, 0, len(a.pluginHost.children))
	for pid := range a.pluginHost.children {
		pids = append(pids, pid)
	}
	a.pluginHost.childMu.Unlock()
	for _, pid := range pids {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	}
}
