package tts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// This file launches and stops the local GPT-SoVITS server.
//
// It lives in the shared tts package rather than in either application, because
// BOTH need it: the studio starts an engine to render with, and the pet's own panel
// offers the same button. Duplicating it would mean two answers to "which process
// may I kill", which is exactly the question that must not be got wrong.
//
// Two hard-won details are encoded here:
//
//  1. The engine REWRITES its own tts_infer.yaml every time the character changes
//     (TTS_Config.save_configs). Its default path is inside the GPT-SoVITS
//     checkout, which may not be writable - and on this machine it is not. So the
//     caller always supplies a data directory, and the config is written there.
//
//  2. A process is only ever killed if THIS code started it. A server the user
//     launched themselves is used as-is and left running.

// EngineConfig describes how to reach and launch the engine.
//
// It is a plain struct rather than a store.Config so this package does not depend
// on the pet's config model: the studio reads its own settings, and both then call
// the same launcher.
type EngineConfig struct {
	// BaseURL is where the engine listens, e.g. http://127.0.0.1:9880.
	BaseURL string
	// Dir is the GPT-SoVITS checkout: models live under <Dir>/models and the
	// entry point is <Dir>/api_v2.py.
	Dir string
	// Venv overrides the interpreter. Empty means <Dir>/.venv/Scripts/python.exe.
	Venv string
	// DataDir is where the app may write. The engine's config and log go here.
	DataDir string
}

// configFileName is the engine config written into the data directory.
const configFileName = "tts_infer.yaml"

// logFileName is the engine's output, kept beside the config so a failed start has
// one obvious place to look.
const logFileName = "tts-engine.log"

// Process is an engine this code launched.
type Process struct {
	cmd     *exec.Cmd
	logPath string
	done    chan struct{}
	mu      sync.Mutex
	exited  bool
}

// FindPython locates the interpreter that can import the engine.
//
// The configured path wins, then the conventional venv inside the checkout. The
// engine's dependencies (torch, fastapi) are not in a system Python, so guessing
// wrong is worth a clear error rather than a confusing ImportError later.
func FindPython(cfg EngineConfig) (string, error) {
	if p := strings.TrimSpace(cfg.Venv); p != "" {
		if FileExists(p) {
			return p, nil
		}
		return "", fmt.Errorf("配置的 Python 解释器不存在：%s", p)
	}
	for _, c := range []string{
		filepath.Join(cfg.Dir, ".venv", "Scripts", "python.exe"),
		filepath.Join(cfg.Dir, ".venv", "bin", "python"),
	} {
		if FileExists(c) {
			return c, nil
		}
	}
	return "", fmt.Errorf("在 %s 下没有找到 .venv 里的 Python。请填写正确的 GPT-SoVITS 目录或解释器路径", cfg.Dir)
}

// StartEngine launches the engine and returns a handle to it.
//
// It returns as soon as the process is started; waiting for the models to load is
// the caller's job, because that is where progress can be reported. Use Ready for
// the wait.
func StartEngine(cfg EngineConfig) (*Process, error) {
	python, err := FindPython(cfg)
	if err != nil {
		return nil, err
	}
	entry := filepath.Join(cfg.Dir, "api_v2.py")
	if !FileExists(entry) {
		return nil, fmt.Errorf("找不到 %s，请确认 GPT-SoVITS 目录填写正确", entry)
	}

	// The engine resolves its model paths relative to its own checkout, so it must
	// be started there - but it rewrites its config on every character switch, so
	// the config path points back at a writable directory.
	engineCfg := filepath.Join(cfg.DataDir, configFileName)
	if err := seedEngineConfig(engineCfg, filepath.Join(cfg.Dir, "GPT_SoVITS", "configs", "tts_infer.yaml")); err != nil {
		return nil, err
	}

	logPath := filepath.Join(cfg.DataDir, logFileName)
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("无法创建日志文件：%w", err)
	}

	host, port := SplitBaseURL(cfg.BaseURL)
	cmd := exec.Command(python, entry, "-a", host, "-p", port, "-c", engineCfg)
	cmd.Dir = cfg.Dir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// No console window: this would otherwise flash a terminal on every start.
	cmd.SysProcAttr = hiddenProcessAttr()

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("启动语音服务失败：%w", err)
	}
	// The child owns the handle now; closing ours is what lets the file be removed
	// later instead of being pinned for the process's lifetime.
	logFile.Close()

	p := &Process{cmd: cmd, logPath: logPath, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.exited = true
		_ = err
		p.mu.Unlock()
		close(p.done)
	}()
	return p, nil
}

// seedEngineConfig copies the engine's stock config into the writable data dir.
//
// A missing source is not fatal: the engine creates a default one when the path it
// is given does not exist, so an unusual checkout still works - it simply starts on
// whichever character its own defaults name.
func seedEngineConfig(dst, src string) error {
	if FileExists(dst) {
		return nil // keep whatever the engine last wrote; it knows the current voice
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return nil //nolint:nilerr // the engine will create its own default
	}
	return os.WriteFile(dst, data, 0o644)
}

// Ready waits for the engine to be able to synthesise, returning nil once it can.
//
// It waits for a HEALTH PROBE rather than for the port, and that distinction is
// load-bearing: the engine binds its port only after constructing the pipeline, but
// on a slow first load the process is up and unresponsive for a while, and a render
// attempted in that window fails in a way that looks like a broken character.
//
// progress, when non-nil, is called roughly once a second so the caller can show
// something during what can be a minute of loading.
func (p *Process) Ready(client *Client, timeout time.Duration, progress func(elapsed time.Duration)) error {
	deadline := time.Now().Add(timeout)
	start := time.Now()
	for time.Now().Before(deadline) {
		if !p.Running() {
			return fmt.Errorf("语音服务启动失败（进程已退出）。日志：%s", p.LogPath())
		}
		if client.HealthQuick() {
			return nil
		}
		if progress != nil {
			progress(time.Since(start))
		}
		time.Sleep(1500 * time.Millisecond)
	}
	return fmt.Errorf("语音服务启动超时（%s）。首次加载模型较慢，可稍后重试。日志：%s",
		timeout.Round(time.Second), p.LogPath())
}

// Running reports whether the process is still alive.
func (p *Process) Running() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.exited
}

// LogPath returns the file the engine's output is written to.
func (p *Process) LogPath() string {
	if p == nil {
		return ""
	}
	return p.logPath
}

// Stop terminates the engine and waits briefly for it to go.
//
// Kill rather than a graceful shutdown request: the engine has no such endpoint that
// is safe to call (its /control exit handler kills the process by signal anyway), and
// nothing here is worth flushing - renders are written to disk as they finish.
func (p *Process) Stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Kill()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		// Already dead or wedged; blocking the caller would be worse.
	}
}

// TailLog returns the last n bytes of the engine log, for an error message.
func (p *Process) TailLog(n int) string {
	if p == nil {
		return ""
	}
	b, err := os.ReadFile(p.logPath)
	if err != nil {
		return "(无日志)"
	}
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}

// SplitBaseURL turns a base URL into the host and port the engine's CLI wants.
//
// 0.0.0.0 is deliberately rewritten to 127.0.0.1: a wildcard bind would expose a
// model server on every interface, and the app only ever talks to it over loopback.
func SplitBaseURL(base string) (string, string) {
	base = strings.TrimSpace(base)
	base = strings.TrimPrefix(base, "http://")
	base = strings.TrimPrefix(base, "https://")
	base = strings.TrimSuffix(base, "/")
	host, port := base, "9880"
	if i := strings.LastIndex(base, ":"); i >= 0 {
		host, port = base[:i], base[i+1:]
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return host, port
}

// FileExists reports whether path is an existing regular file.
func FileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
