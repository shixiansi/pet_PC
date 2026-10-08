package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PetPC/internal/tts"
	"PetPC/internal/voice"
)

// jsonUnmarshal and jsonMarshalIndent keep the encoding/json import in one place
// per file, so the rest of the package reads as policy rather than plumbing.
func jsonUnmarshal(data []byte, v any) error {
	// A BOM is stripped because this file is meant to be hand-editable and Notepad
	// writes one; encoding/json rejects it.
	data = []byte(strings.TrimPrefix(string(data), "\ufeff"))
	return json.Unmarshal(data, v)
}

func jsonMarshalIndent(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// emitRuntime forwards an event to the webview.
func emitRuntime(ctx context.Context, name string, data ...any) {
	runtime.EventsEmit(ctx, name, data...)
}

// EngineView describes the local engine to the UI.
type EngineView struct {
	URL     string `json:"url"`
	Dir     string `json:"dir"`
	Venv    string `json:"venv"`
	Ready   bool   `json:"ready"`
	Managed bool   `json:"managed"`
	// Note explains a non-ready state in terms the user can act on.
	Note string `json:"note,omitempty"`
}

// Counts summarises the library.
type Counts struct {
	Replaced int   `json:"replaced"`
	Rendered int   `json:"rendered"`
	Takes    int   `json:"takes"`
	Bytes    int64 `json:"bytes"`
}

// View is the whole studio state.
type View struct {
	Engine EngineView `json:"engine"`
	// BuiltInRole names the character the pet's EMBEDDED clips were rendered with.
	// Shown so "试听内置" is never a mystery.
	BuiltInRole string `json:"builtInRole"`
	// Roles is every configured character, ActiveRole the one in effect. When no
	// character has been chosen ActiveRole is the built-in sentinel and
	// ActiveRoleName names the built-in character, so the UI always has something
	// honest to show for "what plays now".
	Roles          []voice.Role `json:"roles"`
	ActiveRole     string       `json:"activeRole"`
	ActiveRoleName string       `json:"activeRoleName"`
	// RoleError is non-empty when the stored character no longer exists. The
	// selection must then be shown as unset rather than silently snapping to
	// another role, which is what made a broken configuration look chosen.
	RoleError string `json:"roleError,omitempty"`
	// Available is how many characters were found on disk.
	Available int `json:"available"`
	// Lines is every line with its takes and what plays now.
	Lines  []voice.LineState `json:"lines"`
	Counts Counts            `json:"counts"`
	// Dir is the shared library directory, shown so the user can find their audio.
	Dir string `json:"dir"`
	// Busy/Progress/Done/Total describe a running batch render.
	Busy     bool   `json:"busy"`
	Progress string `json:"progress,omitempty"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
}

// clientLocked returns the engine client, rebuilding it if the address changed.
//
// The caller MUST hold s.mu, which is why this is a separate method: calling it
// without the lock, or calling it while already holding it from a function that
// then takes it again, deadlocks. That is not hypothetical - the first version of
// engineReady locked s.mu and then called this, and every test hung until the
// timeout dumped the goroutine stacks.
func (s *Studio) clientLocked() *tts.Client {
	if s.client == nil || s.client.BaseURL() != strings.TrimRight(s.cfg.TTSBaseURL, "/") {
		s.client = tts.New(s.cfg.TTSBaseURL)
	}
	return s.client
}

// engineReady probes the engine, caching a negative answer briefly.
//
// Only a negative answer is cached: re-probing a healthy engine on every state read
// would put an HTTP request in the path of the UI, while a positive answer that has
// gone stale is exactly the failure worth catching (the engine exited since).
func (s *Studio) engineReady(force bool) bool {
	s.mu.Lock()
	cached := s.ready
	fresh := time.Since(s.readyAt) < 5*time.Second
	s.mu.Unlock()
	if !force && fresh && !cached {
		return false
	}

	s.mu.Lock()
	client := s.clientLocked()
	s.mu.Unlock()

	ready := client.HealthQuick()
	s.mu.Lock()
	s.ready, s.readyAt = ready, time.Now()
	s.mu.Unlock()
	return ready
}

// view assembles the snapshot. probe forces a health check.
func (s *Studio) view(probe bool) View {
	s.mu.Lock()
	cfg := s.cfg
	managed := s.managed != nil
	busy, progress := s.busy, s.progress
	done, total := s.batchDone, s.batchTotal
	s.mu.Unlock()

	v := View{
		Engine: EngineView{
			URL: cfg.TTSBaseURL, Dir: cfg.TTSDir, Venv: cfg.TTSVenv,
			Managed: managed,
		},
		BuiltInRole: voice.BuiltInRole,
		Dir:         s.lib.Dir(),
		Busy:        busy, Progress: progress, Done: done, Total: total,
	}
	if s.engineReady(probe) {
		v.Engine.Ready = true
	} else {
		v.Engine.Note = "未连接。点「启动语音服务」即可（首次加载模型较慢）"
	}

	set := s.lib.Settings()
	v.Roles = set.Roles
	if v.Roles == nil {
		v.Roles = []voice.Role{}
	}
	// The character in effect and the line rows come from ONE LineStates snapshot,
	// rather than from a separate ResolveCurrentRole call. Two reads would let
	// another process commit a switch in between, and the header would then name a
	// character the rows below it do not agree with - the drift this whole change
	// removes, and the studio is the one place the user can see both at once.
	v.Available = len(voice.DiscoverRoles(cfg.TTSDir))

	v.Lines = s.lib.LineStates(voice.Available())
	for _, l := range v.Lines {
		// Counted against the character in effect, so "已替换" means "the pet will
		// really speak this line in the selected voice" - not merely "some take was
		// adopted at some point by someone".
		if l.ChosenForActiveRole {
			v.Counts.Replaced++
		}
		if len(l.Takes) > 0 {
			v.Counts.Rendered++
		}
		v.Counts.Takes += len(l.Takes)
		for _, t := range l.Takes {
			v.Counts.Bytes += t.Bytes
		}
	}
	// The role in effect comes from the resolver's answer carried on every row, not
	// from the persisted field directly: the field alone cannot say whether it names
	// a role that still exists, and showing a dangling id as the selection is how the
	// UI came to disagree with what actually played.
	if len(v.Lines) > 0 {
		row := v.Lines[0]
		v.ActiveRole = row.ActiveRole
		v.ActiveRoleName = row.ActiveRoleName
		v.RoleError = row.RoleError
		if v.RoleError != "" {
			// No usable selection: the UI must show none rather than highlight a card
			// the user never chose.
			v.ActiveRole = ""
			v.ActiveRoleName = ""
		}
	}
	return v
}

// --- bindings -------------------------------------------------------------

// State returns the studio snapshot, probing the engine.
func (s *Studio) State() (View, error) {
	s.seedRolesIfEmpty()
	return s.view(true), nil
}

// seedRolesIfEmpty imports characters found on disk on first use, so a fresh
// install offers what the machine already has instead of an empty list.
func (s *Studio) seedRolesIfEmpty() {
	if len(s.lib.Roles()) > 0 {
		return
	}
	s.mu.Lock()
	dir := s.cfg.TTSDir
	s.mu.Unlock()
	found := voice.DiscoverRoles(dir)
	if len(found) == 0 {
		return
	}
	if n, err := s.lib.SeedRoles(found); err == nil && n > 0 {
		s.emit("studio:roles", n)
	}
}

// SaveEngine persists the engine settings.
//
// Saving them does not start or stop anything: the address can be edited while the
// engine runs, and starting is an explicit action.
func (s *Studio) SaveEngine(url, dir, venv string) (View, error) {
	s.mu.Lock()
	if strings.TrimSpace(url) != "" {
		s.cfg.TTSBaseURL = strings.TrimSpace(url)
	}
	if strings.TrimSpace(dir) != "" {
		s.cfg.TTSDir = strings.TrimSpace(dir)
	}
	s.cfg.TTSVenv = strings.TrimSpace(venv)
	// The client is rebuilt on next use because its address no longer matches.
	s.client = nil
	s.ready, s.readyAt = false, time.Time{}
	err := s.saveConfig()
	s.mu.Unlock()
	if err != nil {
		return s.view(false), fmt.Errorf("保存设置失败：%w", err)
	}
	return s.view(true), nil
}

// EngineStart launches the engine and waits until it can synthesise.
//
// It reports progress through "studio:progress" because loading the models takes
// tens of seconds on CPU, and a UI that simply froze for a minute would look broken.
func (s *Studio) EngineStart() (View, error) {
	s.mu.Lock()
	if s.managed != nil && s.managed.Running() {
		s.mu.Unlock()
		return s.view(true), nil
	}
	s.mu.Unlock()
	if s.engineReady(true) {
		// Something already listens - possibly the pet started it, or the user did.
		return s.view(true), nil
	}

	s.mu.Lock()
	cfg := tts.EngineConfig{
		BaseURL: s.cfg.TTSBaseURL, Dir: s.cfg.TTSDir, Venv: s.cfg.TTSVenv, DataDir: s.dir,
	}
	s.mu.Unlock()

	proc, err := tts.StartEngine(cfg)
	if err != nil {
		return s.view(true), err
	}
	s.mu.Lock()
	s.managed = proc
	s.client = tts.New(cfg.BaseURL)
	client := s.client
	s.mu.Unlock()

	err = proc.Ready(client, 4*time.Minute, func(d time.Duration) {
		s.setProgress(fmt.Sprintf("正在加载模型…（已等待 %d 秒）", int(d.Seconds())))
	})
	s.setProgress("")
	if err != nil {
		return s.view(true), err
	}
	s.mu.Lock()
	s.ready, s.readyAt = true, time.Now()
	s.mu.Unlock()
	return s.view(true), nil
}

// EngineStop stops the engine, but only if this program started it.
func (s *Studio) EngineStop() (View, error) {
	s.mu.Lock()
	proc := s.managed
	s.managed = nil
	s.ready, s.readyAt = false, time.Time{}
	s.mu.Unlock()
	if proc != nil {
		proc.Stop()
	}
	return s.view(true), nil
}

// EngineLog returns the tail of the engine log, for a failed start.
func (s *Studio) EngineLog() string {
	s.mu.Lock()
	proc := s.managed
	s.mu.Unlock()
	if proc == nil {
		return ""
	}
	return proc.TailLog(6000)
}

// RescanRoles re-imports characters from the models directory.
//
// Additive: a role the user has already tuned is never overwritten, so rescanning
// after dropping in a new character cannot undo previous work.
func (s *Studio) RescanRoles() (View, error) {
	s.mu.Lock()
	dir := s.cfg.TTSDir
	s.mu.Unlock()
	found := voice.DiscoverRoles(dir)
	if len(found) == 0 {
		return s.view(true), fmt.Errorf("在 %s\\models 下没有找到角色（每个角色需要 gpt.ckpt、sovits.pth、ref.wav 三个文件）", dir)
	}
	added, err := s.lib.SeedRoles(found)
	if err != nil {
		return s.view(true), err
	}
	if added == 0 {
		return s.view(true), fmt.Errorf("没有新角色（已有的角色不会被覆盖）")
	}
	return s.view(true), nil
}

// SaveRole adds or updates a character.
func (s *Studio) SaveRole(role voice.Role) (View, error) {
	if err := s.lib.UpsertRole(role); err != nil {
		return s.view(false), err
	}
	return s.view(false), nil
}

// DeleteRole removes a character, keeping every take rendered with it.
func (s *Studio) DeleteRole(id string) (View, error) {
	if err := s.lib.DeleteRole(id); err != nil {
		return s.view(false), err
	}
	return s.view(false), nil
}

// SetActiveRole makes a character the current one, for BOTH rendering and playback.
//
// This is the single write that determines which voice the pet speaks as, and it is
// deliberately the same value the player resolves - the two used to be unrelated, so
// selecting a character changed nothing audible.
//
// A failure is returned rather than swallowed, and the returned view is built AFTER
// the failure, so the selection shown is the one that is actually persisted. A UI
// that keeps the new card highlighted after a failed write is claiming a switch that
// did not happen.
//
// Nothing here stops audio: changing the character must not cut off a line that is
// already being spoken.
func (s *Studio) SetActiveRole(id string) (View, error) {
	if err := s.lib.SetActiveRole(id); err != nil {
		return s.view(false), fmt.Errorf("切换角色失败：%w", err)
	}
	return s.view(false), nil
}

// Generate renders one line, synchronously.
func (s *Studio) Generate(line, roleID string) (View, error) {
	if s.isBusy() {
		return s.view(false), fmt.Errorf("正在批量生成，请先等它结束或点停止")
	}
	if _, err := s.renderLine(voice.Line(line), roleID); err != nil {
		return s.view(true), err
	}
	return s.view(false), nil
}

// GenerateAll renders every line in the background, reporting progress.
func (s *Studio) GenerateAll(roleID string) (View, error) {
	if s.isBusy() {
		return s.view(false), fmt.Errorf("已经在生成中")
	}
	role, err := s.requireRole(roleID)
	if err != nil {
		return s.view(false), err
	}
	if err := role.Validate(); err != nil {
		return s.view(false), err
	}
	if !s.engineReady(true) {
		return s.view(true), fmt.Errorf("语音服务没有运行，请先点「启动语音服务」")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.busy = true
	s.cancel = cancel
	s.progress = "准备中…"
	s.batchDone, s.batchTotal = 0, len(voice.AllLines)
	s.mu.Unlock()

	go s.runBatch(ctx, role)
	return s.view(false), nil
}

// CancelGenerate stops a running batch after the current line.
func (s *Studio) CancelGenerate() (View, error) {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return s.view(false), nil
}

// runBatch renders the whole set, reporting progress.
//
// A failed line does NOT abort the batch: on CPU the engine occasionally fails one
// synthesis, and discarding the others because of it would waste minutes already
// spent. The failure is reported and that one line can be retried.
func (s *Studio) runBatch(ctx context.Context, role voice.Role) {
	failed := 0
	done := 0
	total := len(voice.AllLines)
	cancelled := false

	defer func() {
		s.mu.Lock()
		s.busy = false
		s.cancel = nil
		s.progress = ""
		s.batchDone, s.batchTotal = done, total
		s.mu.Unlock()
		s.emit("studio:progress", map[string]any{
			"done": done, "total": total, "finished": true,
			"failed": failed, "cancelled": cancelled,
		})
	}()

	for _, line := range voice.AllLines {
		if ctx.Err() != nil {
			cancelled = true
			return
		}
		s.setProgress(fmt.Sprintf("%s（%d/%d）…", voice.Labels[line], done+1, total))
		s.emit("studio:progress", map[string]any{
			"done": done, "total": total, "line": string(line), "label": voice.Labels[line],
		})

		if _, err := s.renderLine(line, role.ID); err != nil {
			failed++
			s.emit("studio:progress", map[string]any{
				"done": done, "total": total, "line": string(line),
				"label": voice.Labels[line], "error": err.Error(),
			})
			continue
		}
		done++
		s.mu.Lock()
		s.batchDone = done
		s.mu.Unlock()
	}
}

// renderLine renders one take and stores it.
//
// It never CHOOSES the take. Deciding what the pet says is a separate, explicit
// action, so a bad render can never become live just because it was generated.
func (s *Studio) renderLine(line voice.Line, roleID string) (voice.Take, error) {
	if !voice.Known(line) {
		return voice.Take{}, fmt.Errorf("未知台词：%s", line)
	}
	role, err := s.requireRole(roleID)
	if err != nil {
		return voice.Take{}, err
	}
	if err := role.Validate(); err != nil {
		return voice.Take{}, err
	}
	for _, p := range []string{role.GPT, role.SoVITS, role.Ref} {
		if _, err := os.Stat(p); err != nil {
			return voice.Take{}, fmt.Errorf("找不到文件：%s", p)
		}
	}
	if !s.engineReady(true) {
		s.mu.Lock()
		url := s.cfg.TTSBaseURL
		s.mu.Unlock()
		return voice.Take{}, fmt.Errorf("语音服务没有运行（%s）", url)
	}

	text := voice.Text(line)
	if strings.TrimSpace(text) == "" {
		return voice.Take{}, fmt.Errorf("台词 %s 没有文本", line)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	s.mu.Lock()
	client := s.clientLocked()
	s.mu.Unlock()

	audio, err := client.Render(ctx, tts.RoleRef{GPT: role.GPT, SoVITS: role.SoVITS}, tts.Request{
		Text:              text,
		TextLang:          role.Lang,
		RefAudioPath:      role.Ref,
		PromptText:        role.PromptText,
		PromptLang:        role.PromptLang,
		SpeedFactor:       role.Speed,
		Temperature:       role.Temperature,
		TopK:              role.TopK,
		TopP:              role.TopP,
		RepetitionPenalty: role.RepetitionPenalty,
		Seed:              role.Seed,
	})
	if err != nil {
		return voice.Take{}, err
	}
	return s.lib.AddTake(line, audio, role, text)
}

// resolveRole returns the named role, or the one in effect when the name is empty.
//
// The empty case goes through ResolveCurrentRole rather than reading the persisted
// field, so the studio renders as exactly the character the pet will play - the two
// used to be resolved by different code and could disagree.
//
// A character that does not exist is an ERROR, not a silent fallback to another
// one: rendering a whole set as the wrong character is worse than refusing, because
// it is invisible until the user hears the result.
func (s *Studio) resolveRole(roleID string) (voice.Role, bool) {
	if roleID != "" {
		return s.lib.Role(roleID)
	}
	role, err := s.lib.ResolveCurrentRole()
	if err != nil || voice.IsBuiltInRole(role.ID) {
		// The built-in sentinel is not a renderable role: it stands for the clips
		// baked into the executable, which have no weights to synthesise with.
		return voice.Role{}, false
	}
	return role, true
}

// requireRole resolves a role for a render, explaining precisely what is missing.
//
// Separate from resolveRole because "no character is selected" and "no characters
// exist" need different instructions - "重新扫描" does not help a user who simply
// has not picked a card yet, and telling them to rescan would send them the wrong way.
func (s *Studio) requireRole(roleID string) (voice.Role, error) {
	if role, ok := s.resolveRole(roleID); ok {
		return role, nil
	}
	if roleID == "" || voice.IsBuiltInRole(roleID) {
		if _, err := s.lib.ResolveCurrentRole(); err != nil {
			return voice.Role{}, fmt.Errorf("当前角色无效（%w），请在左栏重新选一个角色", err)
		}
		if len(s.lib.Roles()) == 0 {
			return voice.Role{}, fmt.Errorf("没有可用的角色，请先「重新扫描」")
		}
		return voice.Role{}, fmt.Errorf("当前是内置音色，请先在左栏点一个角色")
	}
	return voice.Role{}, fmt.Errorf("找不到角色：%s", roleID)
}

func (s *Studio) isBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy
}

func (s *Studio) setProgress(msg string) {
	s.mu.Lock()
	s.progress = msg
	s.mu.Unlock()
}

// Choose commits one take: from now on the pet says this one.
//
// This is the ONLY write that changes what the pet plays, and the pet notices it
// without restarting because the library re-reads its index when another process has
// written it.
func (s *Studio) Choose(line string, version int) (View, error) {
	if err := s.lib.Choose(voice.Line(line), version); err != nil {
		return s.view(false), err
	}
	return s.view(false), nil
}

// ClearChoice returns one line to the built-in voice, keeping its takes.
func (s *Studio) ClearChoice(line string) (View, error) {
	if err := s.lib.ClearChoice(voice.Line(line)); err != nil {
		return s.view(false), err
	}
	return s.view(false), nil
}

// DeleteTake removes one take.
func (s *Studio) DeleteTake(line string, version int) (View, error) {
	if err := s.lib.DeleteTake(voice.Line(line), version); err != nil {
		return s.view(false), err
	}
	return s.view(false), nil
}

// ResetAll makes every line fall back to the built-in voice at once, deleting
// nothing.
func (s *Studio) ResetAll() (View, error) {
	if err := s.lib.Reset(); err != nil {
		return s.view(false), err
	}
	return s.view(false), nil
}

// AdoptAllAsBuiltIn is the shortcut for "replace the built-in voice with this
// character": it renders every line as the active role and commits each one.
//
// It exists because the long way round - generate five lines, then press "用这版"
// five times - is what a user actually wants the first time they choose a
// character, and making them do it by hand invites the belief that selecting a
// character should have done it already.
func (s *Studio) AdoptAllAsBuiltIn(roleID string) (View, error) {
	if s.isBusy() {
		return s.view(false), fmt.Errorf("已经在生成中")
	}
	role, err := s.requireRole(roleID)
	if err != nil {
		return s.view(false), err
	}
	if err := role.Validate(); err != nil {
		return s.view(false), err
	}
	if !s.engineReady(true) {
		return s.view(true), fmt.Errorf("语音服务没有运行，请先点「启动语音服务」")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.busy = true
	s.cancel = cancel
	s.progress = "准备中…"
	s.batchDone, s.batchTotal = 0, len(voice.AllLines)
	s.mu.Unlock()

	go s.runAdoptAll(ctx, role)
	return s.view(false), nil
}

// Audition plays one take WITHOUT choosing it.
//
// This is what makes the workflow safe to experiment in: several takes can be
// rendered and compared, and nothing the pet will actually say changes until Choose
// is called.
func (s *Studio) Audition(line string, version int) (View, error) {
	l := voice.Line(line)
	if !voice.Known(l) {
		return s.view(false), fmt.Errorf("未知台词：%s", line)
	}
	path, ok := s.lib.TakePath(l, version)
	if !ok {
		return s.view(false), fmt.Errorf("这一版音频不存在")
	}
	if err := playFile(path); err != nil {
		return s.view(false), err
	}
	return s.view(false), nil
}

// AuditionOriginal plays a line's BUILT-IN clip.
//
// The studio is a separate program, so it cannot ask the pet's player to do this;
// it reads the embedded bytes and plays them itself. Having it here is the point of
// offering the choice at all: comparing the built-in voice against a take is how the
// user decides, and a button that silently played the wrong thing - or nothing -
// would be worse than no button.
//
// The clip is materialised to a temp file rather than played from memory because
// winmm's SND_MEMORY form requires the caller's buffer to outlive the asynchronous
// playback, and Go's collector does not know that. See internal/voice/play.go, which
// documents the same problem for the pet.
func (s *Studio) AuditionOriginal(line string) (View, error) {
	l := voice.Line(line)
	if !voice.Known(l) {
		return s.view(false), fmt.Errorf("未知台词：%s", line)
	}
	data, ok := voice.Bytes(l)
	if !ok {
		return s.view(false), fmt.Errorf("这一句没有内置音频")
	}
	path, err := writeTempClip(s.dir, string(l), data)
	if err != nil {
		return s.view(false), err
	}
	if err := playFile(path); err != nil {
		return s.view(false), err
	}
	return s.view(false), nil
}

// runAdoptAll renders and commits every line.
//
// A line that fails is skipped rather than committed, and the rest still go
// through: a half-applied voice is recoverable (the unaffected lines keep the
// built-in clip) whereas abandoning everything because of one failure would waste
// the renders that did work.
func (s *Studio) runAdoptAll(ctx context.Context, role voice.Role) {
	failed := 0
	done := 0
	total := len(voice.AllLines)
	cancelled := false

	defer func() {
		s.mu.Lock()
		s.busy = false
		s.cancel = nil
		s.progress = ""
		s.batchDone, s.batchTotal = done, total
		s.mu.Unlock()
		s.emit("studio:progress", map[string]any{
			"done": done, "total": total, "finished": true,
			"failed": failed, "cancelled": cancelled, "adopted": true,
		})
	}()

	for _, line := range voice.AllLines {
		if ctx.Err() != nil {
			cancelled = true
			return
		}
		s.setProgress(fmt.Sprintf("正在替换 %s（%d/%d）…", voice.Labels[line], done+1, total))
		s.emit("studio:progress", map[string]any{
			"done": done, "total": total, "line": string(line), "label": voice.Labels[line],
		})

		take, err := s.renderLine(line, role.ID)
		if err != nil {
			failed++
			s.emit("studio:progress", map[string]any{
				"done": done, "total": total, "line": string(line),
				"label": voice.Labels[line], "error": err.Error(),
			})
			continue
		}
		if err := s.lib.Choose(line, take.Version); err != nil {
			failed++
			s.emit("studio:progress", map[string]any{
				"done": done, "total": total, "line": string(line),
				"label": voice.Labels[line], "error": err.Error(),
			})
			continue
		}
		done++
		s.mu.Lock()
		s.batchDone = done
		s.mu.Unlock()
	}
}
