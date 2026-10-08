package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"PetPC/internal/store"
	"PetPC/internal/tts"
	"PetPC/internal/voice"
)

// The studio's own settings.
//
// They are a SEPARATE file from the pet's config.json, and that separation is
// deliberate: the pet's config is pushed wholesale to its webview on every state
// read, so anything stored there is a potential leak surface, and an engine address
// is not a pet setting. The two programs share the voice LIBRARY, not their
// configuration.
//
// The defaults still come from internal/store so the engine's conventional location
// is described in exactly one place.
type config struct {
	// TTSBaseURL is where the engine listens.
	TTSBaseURL string `json:"ttsBaseUrl"`
	// TTSDir is the GPT-SoVITS checkout.
	TTSDir string `json:"ttsDir"`
	// TTSVenv overrides the interpreter; empty means <TTSDir>/.venv.
	TTSVenv string `json:"ttsVenv"`
	// DataDir overrides where the shared library lives. Empty means the pet's own
	// data directory, which is what makes the two programs agree by default.
	DataDir string `json:"dataDir"`
}

func defaultConfig(dataDir string) config {
	return config{
		TTSBaseURL: store.DefaultVoiceTTSBaseURL,
		TTSDir:     store.DefaultVoiceTTSDir,
		DataDir:    dataDir,
	}
}

// Studio is the struct bound to the frontend.
type Studio struct {
	ctx context.Context

	// dir is the data directory holding voice/user - the SAME one the pet reads.
	dir string
	// cfgPath is this program's own settings file.
	cfgPath string

	mu  sync.Mutex
	cfg config

	// lib is the shared rendered library.
	lib *voice.Store
	// client talks to the engine.
	client *tts.Client
	// managed is the engine this program started, so Stop only kills our own.
	managed *tts.Process
	// ready/readyAt cache the last health probe.
	ready   bool
	readyAt time.Time

	// busy/progress describe a running batch render.
	busy     bool
	progress string
	// batchDone and batchTotal back the progress bar.
	batchDone, batchTotal int
	cancel                context.CancelFunc

	// emitFn is swappable so the batch renderer can be tested without a window.
	emitFn func(name string, data ...any)
}

// NewStudio opens the shared library and this program's settings.
func NewStudio() (*Studio, error) {
	// The pet's data directory is the default, so both programs find the same
	// library with no configuration at all.
	dir, err := store.DefaultDir()
	if err != nil {
		return nil, fmt.Errorf("找不到数据目录：%w", err)
	}
	return newStudioAt(dir, "")
}

// newStudioAt opens a studio rooted at dir, with an optional override for the
// engine settings.
//
// Split out from NewStudio so the tests can drive the real code against a temp
// directory and a fake engine: NewStudio's only job is to resolve the default
// location, which is the one part that cannot be tested without touching the user's
// real data.
func newStudioAt(dir, cfgPathOverride string) (*Studio, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Studio{dir: dir, cfg: defaultConfig(dir), cfgPath: filepath.Join(dir, "voice-studio.json")}
	if cfgPathOverride != "" {
		s.cfgPath = cfgPathOverride
	}
	s.loadConfig()

	lib, err := voice.Open(filepath.Join(dir, "voice"))
	if err != nil {
		return nil, err
	}
	s.lib = lib
	return s, nil
}

// loadConfig reads this program's settings, tolerating anything wrong with them.
//
// A broken settings file must not stop the studio from opening: the library is the
// valuable part and the settings are a handful of paths.
func (s *Studio) loadConfig() {
	data, err := os.ReadFile(s.cfgPath)
	if err != nil {
		return
	}
	var c config
	if err := jsonUnmarshal(data, &c); err != nil {
		return
	}
	if strings.TrimSpace(c.TTSBaseURL) != "" {
		s.cfg.TTSBaseURL = c.TTSBaseURL
	}
	if strings.TrimSpace(c.TTSDir) != "" {
		s.cfg.TTSDir = c.TTSDir
	}
	s.cfg.TTSVenv = c.TTSVenv
	// DataDir from the file is intentionally ignored: it is the directory we
	// already resolved, and honouring a stale path here would point the studio at a
	// library the pet is not using.
}

// saveConfig persists this program's settings atomically.
func (s *Studio) saveConfig() error {
	data, err := jsonMarshalIndent(s.cfg)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".tmp-studio-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.cfgPath)
}

func (s *Studio) startup(ctx context.Context) {
	s.ctx = ctx
	// No health probe and no engine start here: opening the window must never wait
	// on a server, and the user may only want to look at what they already have.
}

func (s *Studio) shutdown(ctx context.Context) {
	s.mu.Lock()
	proc := s.managed
	s.mu.Unlock()
	// Leaving a several-hundred-megabyte process behind after the window closes
	// would be rude, so an engine WE started goes with us. One the user started is
	// left alone.
	if proc != nil {
		proc.Stop()
	}
}

// emit sends an event to the frontend, or to the test collector.
func (s *Studio) emit(name string, data ...any) {
	if s.emitFn != nil {
		s.emitFn(name, data...)
		return
	}
	emitRuntime(s.ctx, name, data...)
}
