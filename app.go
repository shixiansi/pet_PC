package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/logging"
	"PetPC/internal/plugins"
	"PetPC/internal/themepacks"
	"PetPC/internal/store"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// panelGap is the space between the pet and the todo panel in logical pixels.
const panelGap = 10

// App is the struct bound to the frontend. Every exported method is callable
// from JavaScript.
type App struct {
	ctx   context.Context
	store *store.Store

	// log is this program's log, in <dataDir>\logs\pet.log.
	//
	// It is ALWAYS on, which is the difference between this and the debug.log it
	// replaces: "请把日志发我" used to be followed by "你怎么没开 PETPC_DEBUG" and then
	// nothing, because reproducing the bug meant asking the user to relaunch a program
	// they had already relaunched once. Every method tolerates a nil receiver, so a
	// test that builds App as a bare literal keeps working (see internal/logging).
	log *logging.Logger

	// calls collapses repeated plugin-call lines (one per plugin+method+level per five
	// seconds, with a count). Built on first use so a bare App literal keeps working.
	//
	// It exists because "one line per call" drowned the log: on a real machine 9729
	// `share.get` + 2842 `storage.get` + 580 `config.get` lines against 32 `llm.chat`
	// lines. See internal/logging.Throttle.
	calls *logging.Throttle
	// callsMu guards the lazy construction above (PluginRequest runs on plugin goroutines).
	callsMu sync.Mutex

	mu      sync.Mutex
	hwnd    desktop.Window // native window handle, resolved after startup
	scale   float64        // DPI scale factor of the window's monitor
	petRect desktop.Bounds // the pet's own rect in physical px
	// petOffsetX/Y is the pet's position inside the window, and winSizeW/H is the
	// window's size. Both are constant for a given pet size and side, so they can
	// be reused while dragging to convert a pet position into a window position
	// without disturbing the layout.
	petOffsetX, petOffsetY int
	winSizeW, winSizeH     int
	// layout is the most recent layout handed to the frontend. Every value in it
	// is constant for a given pet size and side, so it only changes when the
	// window is first placed or the pet is resized.
	layout Layout
	// shown records whether the window has been revealed yet, so the startup
	// reveal stays idempotent across a frontend reload.
	shown bool
	// geomReady is closed once the window has been placed AND cropped at least once.
	//
	// The frontend asks to be revealed as soon as it has painted, and the placement runs in a
	// goroutine that first has to find the native window (WaitForWindow, up to 5s). Those two race,
	// and the losing order is the bad one: the window is revealed with NO region and NO placement,
	// so the user gets an uncropped rectangle at Wails' default size with a 0x0 pet in it, and
	// nothing looks right until something re-crops - which is why "drag the pet once" appeared to
	// fix it. Ready waits for this instead of revealing into that state.
	geomReady chan struct{}
	// geomOnce guards the single close above: every geometry pass after the first is a re-crop,
	// not a new readiness event.
	geomOnce sync.Once
	// tray is the notification-area icon. It owns its own thread and message
	// loop, so it is only touched at startup and shutdown.
	tray *desktop.Tray

	// bubble is the speech bubble currently drawn beside the pet, or nil. It is
	// mutually exclusive with the panel: both occupy the same slot, and the
	// window region must describe exactly one of them or the page's unpainted
	// margins reappear as a glass band (docs/reminder-pomodoro.md 8.2).
	bubble *BubbleView

	// pluginSurface is the plugin floating surface currently drawn beside the
	// pet, or nil. It is the third occupant of that same slot, so opening it
	// folds both the panel and any bubble away; exactly one of
	// {panel, bubble, pluginSurface} is ever visible.
	//
	// Like the bubble, it is geometry as much as content: the rectangles go into
	// the window region, and showing one without cropping to it would leave the
	// area the page does not paint visible as a glass band.
	pluginSurface *PluginSurfaceView

	// surfaceHiddenBlocks is the set of DECLARATION INDICES the running surface's
	// plugin asked the host to hide (`surface.blocks`). A hidden block contributes
	// neither a region rectangle nor a view entry, so the page does not mount a
	// frame for it - which is the point: a declared-but-idle block would keep
	// swallowing clicks, because pointer-events lives on the parent document's
	// frame element and a plugin cannot reach that.
	//
	// It carries its own mutex rather than using App.mu: applying the geometry
	// happens with App.mu held, and the reader is called from inside that, so
	// sharing the mutex would deadlock.
	//
	// Empty means "show everything the manifest declared", which is the behaviour
	// of every plugin that never calls the method.
	surfaceHiddenBlocks map[int]bool
	surfaceHiddenMu     sync.Mutex

	// surfaceBlockSizes is the height each declaration index asked for (`surface.blocks` with
	// `sizes`). Empty means "as declared", which is what every plugin but the chat one gets, and
	// what the chat one gets until its content actually needs more room.
	//
	// Shares surfaceHiddenMu: the two travel in the same request and are always applied together,
	// and one lock for one request is one fewer ordering to reason about.
	surfaceBlockSizes map[int]int

	// Throttle for re-cropping the window. A reply being revealed 20 times a second would
	// otherwise recompute the region and push a layout on every character.
	surfaceSizesAt   time.Time
	surfaceSizesNext *time.Timer

	// pluginHost holds the plugin subsystem's non-geometry state: the installed
	// plugins on disk and the per-plugin event subscriptions. It carries its own
	// mutex because a plugin call can block on the network, and App.mu guards
	// window geometry that a drag must never have to queue behind.
	pluginHost pluginHost

	// packs is the theme-pack catalogue: the built-ins plus whatever is installed in `themes/`.
	//
	// It is a POINTER SHARED BY THE WHOLE PROCESS so its cache is the process's cache. The asset
	// route asks it for a `<style>` on every plugin document, and re-reading the directory there
	// would put a stat per frame mount on the hot path.
	packs *themepacks.Set

	// audioMu guards the audio sink plugins play through.
	//
	// It is a separate mutex from App.mu for the same reason pluginHost's is:
	// audio.play runs on a plugin's request goroutine, and a drag must never
	// queue behind it. The sink itself is an interface so a test can observe
	// playback without an audio device - the same seam the retired voice package
	// exposed, kept because it is what makes "the plugin asked to play THIS file"
	// an assertable fact rather than a side effect.
	audioMu  sync.Mutex
	audioOut AudioSink

	// shutdownOnce makes stopping the loops idempotent: closing a channel twice
	// panics, and Quit can be reached from both the tray menu and the panel.
	shutdownOnce sync.Once
	// remMu guards the reminder bubble's own fields.
	remMu sync.Mutex
	// remHovered is true while the pointer is over a reminder bubble, which
	// pauses its auto-hide countdown.
	remHovered bool

	// llm holds the LLM chat runtime: the in-flight request, the encrypted API
	// key and the shared HTTP client. It has its own mutex because it is touched
	// from the request goroutine, which must not contend with window geometry.
	llm llmState

	// pluginMarket holds the launcher's state for the STANDALONE plugin market:
	// the process this pet started, and the three external inputs (its own
	// executable, the working directory, and the way a process is started) that
	// a test has to be able to substitute to observe the launch. It is separate
	// from pluginHost because it is process state, not plugin state: the market
	// is a different program, and the pet's plugin subsystem works whether or
	// not that program is running.
	pluginMarket pluginMarketState

	// hotkeyMu guards the global hot-key manager and its last reported view. The
	// manager's own thread calls back into the app while the settings panel reads
	// the view, so the two need separating.
	hotkeyMu sync.Mutex
	hotkeys  hotkeyState

	// hidden records that the window has been hidden with the hot key or the tray.
	//
	// It is runtime state and NOT a config value: a persisted "hidden" would have
	// to be un-hidden by a shortcut that might be unavailable, which is the one
	// trap this feature could set. Every launch therefore starts visible.
	//
	// Guarded by mu, like the rest of the window state.
	hidden bool

	// There is no question-reveal state here any more.
	//
	// `revealedForQuestion` and the `questionVisibility` seam belonged to the cycle
	// that brought the pet back when a pomodoro question arrived while it was
	// hidden. Both questions belong to the pomodoro plugin now, which reveals its
	// own floating surface, and the host's timer is gone with them - so nothing in
	// the host can raise a question, and the whole cycle (plus the seam that made it
	// testable) was deleted rather than left as a flag nothing sets
	// (openspec/changes/todo-pomodoro-as-plugins, design.md §4.1).

	// emitFn, when set, replaces runtime.EventsEmit. It exists because the chat
	// path emits from a request goroutine, and Wails refuses any context other
	// than the one its lifecycle hooks supplied - which makes the whole chat
	// state machine untestable without a running window unless the emit can be
	// swapped for a collector.
	emitFn func(name string, data ...any)
}

// emitEvent sends an event to the frontend.
func (a *App) emitEvent(name string, data ...any) {
	if a.emitFn != nil {
		a.emitFn(name, data...)
		return
	}
	runtime.EventsEmit(a.ctx, name, data...)
}

// NewApp creates the App and opens its data store.
func NewApp() (*App, error) {
	dir, err := store.DefaultDir()
	if err != nil {
		return nil, err
	}
	st, err := store.New(dir)
	if err != nil {
		return nil, err
	}
	lg := logging.Open(dir, logging.PetLog, logging.Options{Level: logLevelFromEnv()})
	return &App{
		store:     st,
		log:       lg,
		scale:     1,
		geomReady: make(chan struct{}),
		// One Set for the process: it caches the themes directory against its mtime, so building a
		// new one per request would stat the directory on every plugin document.
		packs: themepacks.NewSet(dir),
	}, nil
}

// logLevelFromEnv resolves how loud the log is.
//
// The default is INFO rather than DEBUG, and that choice is the feature: a shipping
// install writes the story (started, plugin called, model answered, what failed) without
// anyone setting anything. DEBUG is for a support session and is reached two ways -
// PETPC_DEBUG=1, which is the switch every existing documented workflow already names,
// and PETPC_LOG_LEVEL=debug / info / warn / error, which also wins over PETPC_DEBUG so
// one can quieten a machine without unsetting the other.
func logLevelFromEnv() logging.Level {
	if raw := strings.TrimSpace(os.Getenv("PETPC_LOG_LEVEL")); raw != "" {
		return logging.LevelOfOrDefault(raw, logging.LevelInfo)
	}
	if os.Getenv("PETPC_DEBUG") != "" {
		return logging.LevelDebug
	}
	return logging.LevelInfo
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// The first line of every session says which log is being written and how loud it
	// is. Without it, a report that looks empty cannot be told apart from one taken at
	// INFO while the interesting events were DEBUG.
	a.log.Infof("pet", "启动：数据目录 %s，日志级别 %s，日志文件 %s",
		a.store.Dir(), a.log.Level(), a.log.Path())
	// Preferences can be changed by the control center while this runs. The store
	// re-reads settings.json on demand, which covers everything a render reads - but the
	// pet's SIZE is baked into the geometry below, so it needs the other half: something
	// that notices the change and applies it. See app_settingswatch.go.
	a.startSettingsWatch() // The Wails window only exists once startup runs, so the native handle is
	// resolved here. A goroutine keeps the UI from waiting on the retry loop.
	go func() {
		hwnd, err := desktop.WaitForWindow(5 * time.Second)
		if err != nil {
			a.log.Warnf("window", "找不到主窗口（WaitForWindow 超时）：%v", err)
			return
		}
		a.mu.Lock()
		a.hwnd = hwnd
		a.scale = desktop.Scale(hwnd)
		a.mu.Unlock()
		// Turn the window into glass so the desktop shows through. This has to
		// happen after the window exists, and it must be re-applied after any
		// operation that recreates the DWM frame.
		if err := desktop.MakeTransparent(hwnd); err != nil {
			a.log.Warnf("window", "开启透明失败：%v", err)
		}
		// Stop Windows painting the class background brush (solid black, since
		// Wails ignores the alpha) when the window is resized. Without this the
		// pet flashes a black rectangle on every panel toggle and drag.
		if err := desktop.SuppressBackgroundErasure(hwnd); err != nil {
			a.log.Warnf("window", "抑制背景擦除失败：%v", err)
		}
		// The pet lives in the notification area, not the taskbar. Wails creates
		// the window with WS_EX_APPWINDOW, which forces a taskbar button.
		if err := desktop.HideFromTaskbar(hwnd); err != nil {
			a.log.Warnf("window", "从任务栏隐藏失败：%v", err)
		}
		a.startTray()
		// Settings are edited in the control center; this is how this process finds out.
		// See app_plugin_configwatch.go for why the plugin must not poll for it.
		a.startPluginConfigWatch()
	// The programs plugins started do not notice a disable or an update on their own,
	// and a plugin's frame cannot reap them (it is torn down with the update). The host
	// is the only party that outlives both.
	a.startPluginChildReaper()
	// The private-directory watch: a plugin's helper script writes files, and the plugin has no
	// way to notice except polling. See app_plugin_storagewatch.go.
	a.startPluginStorageWatch()
		a.restorePosition()
		// Voice moved to a plugin, so the pet no longer prepares a player or opens
		// the user's library - it emits facts and the plugin decides what to say.
		//
		// The two init calls that used to sit here (initVoice, initVoiceStudio) are
		// gone with the subsystem, and with them the ordering hazard they existed to
		// avoid: the player no longer caches a library it might have opened too
		// early, because the pet no longer has a library.
		//
		// The legacy library is COPIED into the voice plugin's private directory
		// before the loops start, so the very first phase completion after this
		// upgrade is announced with the character the user chose rather than with a
		// silent plugin. It is a no-op when the plugin is absent, when there is no
		// legacy library, and when it has already run.
		if err := plugins.MigrateVoiceData(a.store.Dir(), a.pluginHost.keeper); err != nil {
			// Not fatal: the pet works, the plugin just has no clips yet, and the
			// next launch retries because no marker was written.
			a.log.Warnf("plugin", "迁移语音插件的数据失败（下次启动会重试）：%v", err)
		}
		// The LIST moved the same way, one step later: `todos.*` retired, the todo
		// plugin owns its own `storage` now, and this copies the host's todos.json into
		// it once. Same three properties as the voice migration - source only read,
		// data written before the marker, marker ends it - and the same two no-op
		// cases: the plugin is not installed, or it already has a list of its own.
		if err := plugins.MigrateTodoData(a.store.Dir()); err != nil {
			// Not fatal for the same reason: no marker was written, so the next launch
			// retries, and a pet that refused to appear over a plugin's data would be
			// the worse bug. It IS worth a log line, because the visible symptom is an
			// empty list in the plugin while todos.json still has the entries.
			a.log.Warnf("plugin", "迁移待办插件的数据失败（下次启动会重试）：%v", err)
		}
		// There is no timer loop to start: the pomodoro plugin counts its own phases
		// (market/plugins/com.shixiansi.plugin-pomodoro), so the host has neither a
		// state machine to tick nor a startup preference to apply.
		//
		// The reminder SCHEDULE belongs to the todo plugin now (see the retired block in
		// app_reminder.go): the host keeps only the manual RemindNow action, so there is no loop to
		// start here either.
		// The chat runtime is set up last: it touches no window state, and a
		// failure to open the key file must never delay the pet appearing.
		a.initLLM()
		a.logLLMStartup()
		// The user's AI settings now belong to the conversation plugin, so they
		// are carried across ONCE, here, before anything can read them through
		// the plugin path. It is idempotent (a marker in the plugin's private
		// directory ends it) and it is a no-op when that plugin is not installed,
		// which is the normal state of a machine that never used the feature. A
		// failure is logged and swallowed: the migration renames nothing until
		// every target is written, so the next launch retries, and a pet that
		// refused to appear over a plugin's settings would be the worse bug.
		a.migrateChatData()
		// The plugin market is a SEPARATE PROGRAM and the pet never manages
		// plugins itself, so nothing is started here in a normal run. This call
		// only reads the gate-only environment variable declared in
		// app_plugin_market.go, and when it is set it launches the market once:
		// it is the gate-driveable equivalent of the settings button, and with
		// the variable unset (the default) it does nothing at all.
		a.startPluginMarketFromEnv()
		// Global shortcuts are started after the window exists: showing the pet
		// from a shortcut is a window call, and registering before there is a
		// window to reveal would make the first press do nothing.
		a.initHotkeys()
	}()
}

// The pet's own menu lives in app_pet_menu.go (OpenPetMenu). It is a page-drawn
// bubble card there, and it used to be this tray menu popped on the tray thread -
// see that file for why the pet stopped borrowing the tray's list.

// startTray installs the notification-area icon.
//
// Wails v2 has no tray support, so desktop.NewTray provides one. It runs on its
// own thread with its own message loop, because Wails' OnStartup callback is on a
// goroutine and the shell requires the icon's messages to be dispatched by the
// thread that created the window.
//
// A failure here is logged and ignored: the pet still works without a tray icon,
// and refusing to start over a missing convenience would be worse.
func (a *App) startTray() {
	items := []desktop.TrayMenuItem{
		desktop.TrayMenuAction(desktop.TrayCmdTogglePanel, "打开待办插件"),
		desktop.TrayMenuAction(desktop.TrayCmdRemindNow, "立刻提醒一次"),
		desktop.TrayMenuSeparator(),
		// The mouse route to the same thing the global shortcuts do. It is here
		// because the shortcuts can FAIL (another program may own the
		// combination), and the tray is then the only way to bring the pet back.
		desktop.TrayMenuAction(desktop.TrayCmdShowPet, "显示桌宠"),
		desktop.TrayMenuAction(desktop.TrayCmdHidePet, "隐藏桌宠"),
		desktop.TrayMenuSeparator(),
		desktop.TrayMenuAction(desktop.TrayCmdResetPos, "归位到默认位置"),
		desktop.TrayMenuSeparator(),
		desktop.TrayMenuAction(desktop.TrayCmdOpenData, "打开数据目录"),
		desktop.TrayMenuAction(desktop.TrayCmdOpenMarket, "打开控制中心（插件与设置）"),
		desktop.TrayMenuAction(desktop.TrayCmdQuit, "退出"),
	}
	tray, err := desktop.NewTray(desktop.TrayOptions{
		Tooltip:    "PetPC 桌宠",
		Items:      items,
		OnActivate: func() { a.toggleTodoSurface() },
		OnCommand: func(id uintptr) {
			switch id {
			case desktop.TrayCmdTogglePanel:
				a.toggleTodoSurface()
			case desktop.TrayCmdRemindNow:
				if _, err := a.RemindNow(); err != nil {
					a.log.Warnf("reminder", "托盘「立刻提醒一次」失败：%v", err)
				}
			case desktop.TrayCmdShowPet:
				if err := a.ShowPet(); err != nil {
					a.log.Warnf("pet", "托盘「显示桌宠」失败：%v", err)
				}
			case desktop.TrayCmdHidePet:
				if err := a.HidePet(); err != nil {
					a.log.Warnf("pet", "托盘「隐藏桌宠」失败：%v", err)
				}
			case desktop.TrayCmdResetPos:
				a.ResetToDefaultPosition()
			case desktop.TrayCmdOpenData:
				if err := desktop.ShellOpen(a.store.Dir()); err != nil {
					a.log.Warnf("pet", "打开数据目录失败：%v", err)
				}
			case desktop.TrayCmdOpenMarket:
				if _, err := a.OpenPluginMarket(); err != nil {
					// Not fatal and not silent: the program is a sibling exe, so "not
					// found" is a real state the user has to be told about.
					a.log.Warnf("plugin", "打开控制中心失败：%v", err)
				}
			case desktop.TrayCmdQuit:
				a.Quit()
			}
		},
	})
	if err != nil {
		a.log.Errorf("tray", "托盘图标没装成功（托盘菜单因此不可用）：%v", err)
		return
	}
	a.log.Info("tray", "托盘图标已注册")
	a.mu.Lock()
	a.tray = tray
	a.mu.Unlock()
}

// Ready shows the window once the frontend has painted its first frame.
//
// The window is created hidden (see StartHidden in main.go) so the decorated
// frame Wails creates is never visible. The frontend calls this after applying
// the first layout, which makes the first thing on screen the finished pet
// rather than a title bar with an unpositioned picture.
//
// The window is revealed with ShowWindow directly instead of runtime.WindowShow:
// Wails' implementation is gated on an internal "hasBeenShown" flag and races
// WebView2 navigation, so calling it this early can leave the window hidden
// forever.
func (a *App) Ready() error {
	a.mu.Lock()
	shown := a.shown
	a.shown = true
	a.mu.Unlock()
	if shown {
		return nil // idempotent: a reload would call this again
	}
	hwnd, err := a.resolveWindow()
	if err != nil {
		return err
	}
	// WAIT FOR THE PLACEMENT AND THE CROP. `restorePosition` runs in the startup goroutine and
	// first has to find the native window, so on a cold start it loses the race against the
	// page's own "I have painted, reveal me". Revealing into that gap is what produced the
	// reported first-launch state: an uncropped rectangle at Wails' default size with a 0x0 pet
	// in it, which only looked right once something re-cropped (a drag).
	//
	// The timeout is a floor, not a target: if the window could never be resolved the pet still
	// has to appear, because an invisible pet is a worse bug than an uncropped one. It logs
	// instead of failing so the reveal is never silently skipped.
	select {
	case <-a.geomReady:
	case <-time.After(6 * time.Second):
		a.log.Warn("window", "窗口还没摆好就先显示了（首帧裁剪可能不准，拖动一次会重裁）")
	}
	// Re-assert transparency: the frame was stripped while the window was
	// hidden, and the first show is what DWM uses to build the composition.
	if err := desktop.MakeTransparent(hwnd); err != nil {
		a.log.Warnf("window", "重新应用透明失败：%v", err)
	}
	a.log.Info("window", "页面已画好，显示窗口")
	return desktop.Show(hwnd)
}

// WindowReady reports whether the startup reveal has happened yet.
func (a *App) WindowReady() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.shown
}

// --- window plumbing ------------------------------------------------------

// resolveWindow returns a live window handle, rediscovering it if necessary.
func (a *App) resolveWindow() (desktop.Window, error) {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd != 0 && desktop.IsWindow(hwnd) {
		return hwnd, nil
	}
	h, err := desktop.FindWindow()
	if err != nil {
		return 0, err
	}
	a.mu.Lock()
	a.hwnd = h
	a.scale = desktop.Scale(h)
	a.mu.Unlock()
	return h, nil
}

func (a *App) currentScale() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.scale <= 0 {
		return 1
	}
	return a.scale
}

// petPixelSize converts the configured pet size into physical pixels.
func (a *App) petPixelSize() (int, int) {
	lw, lh := a.store.Config().PetSize()
	s := a.currentScale()
	return int(float64(lw)*s + 0.5), int(float64(lh)*s + 0.5)
}

func clampToWork(x, y, w, h int, work desktop.Bounds) (int, int) {
	if maxX := work.X + work.Width - w; x > maxX {
		x = maxX
	}
	if maxY := work.Y + work.Height - h; y > maxY {
		y = maxY
	}
	if x < work.X {
		x = work.X
	}
	if y < work.Y {
		y = work.Y
	}
	return x, y
}

// Layout tells the frontend where to draw, in CSS pixels relative to the
// window's top-left corner.
//
// Every value here is a CONSTANT for a given pet size and side. The window is
// always the full panel+pet rectangle and never resizes or moves when the panel
// is toggled; only a native region crop changes what is visible. That is
// deliberate, and it is what finally removes the flicker:
//
//   - Resizing the window made WebView2 recreate its viewport a frame or two
//     later. During that gap an element pinned to an edge was laid out against
//     the wrong viewport size, so the pet visibly moved when the panel opened.
//     Hiding the pet during the gap traded the jump for a one-frame blink.
//   - Cropping with SetWindowRgn changes no window geometry at all, so the page
//     keeps exactly the layout it already had. There is no gap to paper over,
//     and the panel is already rendered when it appears, so opening it is
//     instantaneous.
type Layout struct {
	WindowW float64 `json:"windowW"`
	WindowH float64 `json:"windowH"`
	PetX    float64 `json:"petX"`
	PetY    float64 `json:"petY"`
	PetW    float64 `json:"petW"`
	PetH    float64 `json:"petH"`
	// No PanelOpen member. It said whether the panel was showing, and the panel is gone; what
	// the four Panel* offsets below still are is the SLOT beside the pet - the rectangle a
	// plugin's floating surface may occupy - so they stay, and they stay named for the slot
	// they define rather than for a section that no longer exists.
	PanelX float64 `json:"panelX"`
	PanelY float64 `json:"panelY"`
	PanelW float64 `json:"panelW"`
	PanelH float64 `json:"panelH"`
	// PanelOnRight is true when that slot sits to the right of the pet.
	PanelOnRight bool    `json:"panelOnRight"`
	Scale        float64 `json:"scale"`

	// Theme is the colour scheme the pet's cards must be drawn in: "light" or
	// "dark" (store.Config.CardTheme).
	//
	// It travels WITH the layout rather than in an event of its own because the page
	// already re-applies the layout on every state push - so "the file says dark, the
	// cards are light" cannot happen: whatever the host last pushed is what the page
	// paints. The control center's theme is a different field in a different process;
	// this one is only the pet's own cards.
	Theme string `json:"theme"`

	// ThemePack is the palette pack the same cards must be drawn in ("sakura", …), or
	// "" when the user never chose one.
	//
	// It rides the layout for the same reason Theme does, and it is ADDITIVE on purpose:
	// a page that predates packs ignores the field, and an install that predates them
	// sends "". Neither case is an error, and neither needs a version bump.
	ThemePack string `json:"themePack"`

	// Bubble is the speech bubble when one is showing, or nil.
	//
	// Its content travels with the layout rather than in a separate event
	// because its rectangle is what the window region is cut to: if the frontend
	// learned the text and the geometry from two different messages, a frame
	// could be painted with a bubble drawn but not yet visible (or the reverse).
	Bubble *BubbleView `json:"bubble,omitempty"`

	// PluginSurface is a plugin's floating surface when one is showing, or nil.
	//
	// It travels with the layout for the same reason again - the rectangles ARE
	// the window region - and the frontend places the surface iframes at
	// exactly these coordinates rather than deriving a size of its own. That is
	// what makes "the region's rectangles" and "the placed iframes" the same
	// rectangles rather than two calculations kept in agreement by hand.
	PluginSurface *PluginSurfaceView `json:"pluginSurface,omitempty"`
}

// GetLayout returns the current layout.
func (a *App) GetLayout() (Layout, error) {
	if _, err := a.resolveWindow(); err != nil {
		return Layout{}, err
	}
	// The scheme is a SETTING, not geometry: it changes without the window moving, so it is stamped
	// here instead of trusted from the cached layout. Without this the cache still held the value
	// from the last geometry pass, and pushLayout re-sent the OLD scheme - which is exactly why
	// flipping 主题 in the menu did nothing until the menu was re-opened (that recomputes geometry,
	// and so the layout, with the new value).
	theme := a.panelTheme()
	a.mu.Lock()
	defer a.mu.Unlock()
	cached := a.layout
	cached.Theme = theme
	return cached, nil
}

// panelRGB is the panel's opaque background colour, matching --panel-bg in the
// stylesheet.
const (
	panelR, panelG, panelB = 28, 30, 36
	// panelRadius matches --radius in the stylesheet, in CSS pixels. The window
	// region uses it so the visible area follows the panel's rounded corners.
	panelRadius = 14
)

// applyWindowGeometry places the window and sets its region for the current
// panel state.
//
// The window is always the full pet+gap+panel rectangle anchored so that the pet
// lands where the user left it. Opening the panel only clears the region crop;
// closing it crops back down to the pet. Nothing is resized, so the webview's
// viewport never changes and the page never has to re-lay-out.
func (a *App) applyWindowGeometry() (Layout, error) {
	// Drop a surface whose plugin has stopped qualifying BEFORE anything else.
	// This is the guard that replaces the two per-plugin close calls the deleted
	// management bindings used to make, and unlike them it also sees a disable or
	// uninstall performed by the market program, which is a different process:
	// see syncPluginSurface.
	//
	// It runs before resolveWindow on purpose. A geometry pass that cannot reach
	// the window still has to reconcile the STATE, or a pet whose window call
	// failed would keep advertising a surface it is not cropped for.
	a.syncPluginSurface()

	hwnd, err := a.resolveWindow()
	if err != nil {
		return Layout{}, err
	}
	work, err := desktop.WorkArea(hwnd)
	if err != nil {
		return Layout{}, err
	}
	petW, petH := a.petPixelSize()
	s := a.currentScale()

	gap := int(float64(panelGap)*s + 0.5)
	panelW := int(float64(store.PanelWidth)*s + 0.5)
	panelH := int(float64(store.PanelHeight)*s + 0.5)

	totalW := petW + gap + panelW
	totalH := panelH
	if petH > totalH {
		totalH = petH
	}

	a.mu.Lock()
	pet := a.petRect
	hasBubble := a.bubble != nil
	pluginSurface := a.pluginSurface
	var bubble BubbleView
	if hasBubble {
		bubble = *a.bubble
	}
	a.mu.Unlock()
	if pet.Width <= 0 {
		pet = desktop.Bounds{Width: petW, Height: petH}
	}

	// Decide which side the panel takes from where the pet sits on screen: a pet
	// on the right half gets the panel on its left. This is stable for a given
	// pet position, so the pet's offset inside the window stays constant and the
	// frontend never has to move it.
	//
	// The bubble lives in the panel's slot on that same side, so it inherits the
	// same decision: switching sides mid-bubble would move the pet.
	panelOnRight := pet.X+pet.Width/2 <= work.X+work.Width/2

	var petX, panelX int
	if panelOnRight {
		petX = 0
		panelX = petW + gap
	} else {
		petX = panelW + gap
		panelX = 0
	}
	// Centre the pet vertically; the panel spans the full height.
	petY := (totalH - petH) / 2
	if petY < 0 {
		petY = 0
	}
	panelY := 0

	// The bubble is narrower than the panel and centred on the pet's horizontal
	// band, so it reads as coming from the pet.
	var bubblePx desktop.Bounds
	if hasBubble {
		bw := int(float64(bubbleWidth)*s + 0.5)
		bh := int(float64(bubbleLogicalHeight(bubble.Kind))*s + 0.5)
		bx := panelX
		if panelOnRight {
			// Keep the card hard against the pet's side of the window.
			bx = petX + petW + gap
		}
		by := petY + (petH-bh)/2
		if by < 0 {
			by = 0
		}
		if maxY := totalH - bh; by > maxY {
			by = maxY
		}
		bubblePx = desktop.Bounds{X: bx, Y: by, Width: bw, Height: bh}
	}

	// Place the window so the pet stays where the user put it, then pull the
	// WINDOW back inside the work area without moving the pet.
	//
	// Only the window is clamped. Clamping the pet instead would visibly drag it
	// away from where it was dropped whenever the window (which is much larger
	// than the pet) would otherwise overhang the screen edge - the pet would
	// drift toward the middle as the panel came and went.
	winX := pet.X - petX
	winY := pet.Y - petY
	if winX < work.X {
		winX = work.X
	}
	if maxX := work.X + work.Width - totalW; winX > maxX {
		winX = maxX
	}
	if winY < work.Y {
		winY = work.Y
	}
	if maxY := work.Y + work.Height - totalH; winY > maxY {
		winY = maxY
	}
	// The pet follows the window, but never leaves the work area.
	petScreenX := winX + petX
	petScreenY := winY + petY
	if maxX := work.X + work.Width - petW; petScreenX > maxX {
		petScreenX = maxX
	}
	if petScreenX < work.X {
		petScreenX = work.X
	}
	if maxY := work.Y + work.Height - petH; petScreenY > maxY {
		petScreenY = maxY
	}
	if petScreenY < work.Y {
		petScreenY = work.Y
	}
	// Re-derive the window origin from the (possibly clamped) pet position.
	winX = petScreenX - petX
	winY = petScreenY - petY

	target := desktop.Bounds{X: winX, Y: winY, Width: totalW, Height: totalH}

	// The background brush and framelessness are set once; re-assert them here
	// because this runs at startup before the window is shown.
	if err := desktop.SuppressBackgroundErasure(hwnd); err != nil {
		a.log.Warnf("window", "抑制背景擦除失败：%v", err)
	}
	if err := desktop.StripWindowFrame(hwnd); err != nil {
		a.log.Warnf("window", "去掉窗口边框失败：%v", err)
	}
	// Re-clear the window blur. Wails applies it for a translucent window, and
	// anything that re-runs the accent setup (activation, a resize) can bring it
	// back, which shows as a frosted wash behind the pet.
	if err := desktop.DisableWindowBlur(hwnd); err != nil {
		a.log.Warnf("window", "关闭窗口模糊失败：%v", err)
	}

	if err := desktop.SetBounds(hwnd, target); err != nil {
		return Layout{}, err
	}

	// Crop the window to the BOUNDING BOXES of what the page paints.
	//
	// While nothing is beside the pet that is just the pet. While a plugin's surface is up it
	// is the union of that surface's rectangles and the pet, and while a bubble is up it is the
	// union of that bubble and the pet. (The panel used to be the third case, and it is gone.)
	//
	// The union matters because the window is a fixed size larger than what is drawn (the pet
	// is shorter than the slot beside it, so there are unpainted margins above and below it).
	// Clearing the region exposed those margins, and with the DWM frame extended over the
	// client area they rendered as a translucent glass strip - the pale band along the top
	// edge of the pet's column. Cropping keeps every visible pixel one that the page drew.
	//
	// Boxes, NOT shapes: the bubble's radius and a plugin's rounded rectangles stay in the
	// CSS, and the region is deliberately square. Handing the region a radius used to be how
	// the corners were cut, and it is what made them jagged - a GDI region is a 1-bit mask, so
	// the OS drew a staircase over the browser's anti-aliased edge (see SetWindowRegion in
	// internal/desktop). The page's own alpha now defines the shape, and because the window
	// is translucent the corner pixels composite to the desktop.
	//
	// The trade, which the pixel gate records: a click in those corner pixels lands on this
	// window instead of falling through to the desktop. A few pixels per corner, and the
	// alternative is per-pixel hit testing.
	var regions []desktop.Bounds
	var surfaceView *PluginSurfaceView
	switch {
	case pluginSurface != nil:
		// A plugin floating surface: the plugin's declared rectangles, clamped
		// into the window, plus the pet.
		//
		// The rectangles are re-read from the plugin's manifest and put through
		// ONE call to computePluginSurface, whose two outputs are the cropped
		// region and the coordinates the page places the surface iframes at.
		// Deriving both here, from the declaration, is what makes them the same
		// rectangles rather than two calculations kept in agreement by hand.
		//
		// A surface whose every rectangle fell outside the window yields nothing
		// to crop to, so the pet keeps the window to itself and no surface is
		// advertised in the layout.
		//
		// ok is also false when the plugin has stopped QUALIFYING - disabled,
		// uninstalled, or with pet.surface revoked - which is how the guard
		// survives the plugin market being a separate process: see
		// pluginSurfaceRects. The else branch below is the same one a surface
		// that fell outside takes, and the state is cleared further down.
		declared, ok := a.pluginSurfaceRects(pluginSurface.PluginID)
		// Mirrored when the pet is on the right, for the reason
		// mirrorDeclaredRects documents: the declaration is written for the pet on
		// the left, and without the flip the blocks land outside the window and are
		// clamped back on top of the panel, where they swallow its clicks.
		if !panelOnRight {
			declared = mirrorDeclaredRects(declared, float64(petW)/s)
		}
		surfaceRegions, view := computePluginSurfaceSized(petX, petY, petW, petH, totalW, totalH, s, declared, a.hiddenSurfaceBlocks(), a.requestedSurfaceSizes())
		if ok && len(surfaceRegions) > 0 {
			// The plugin's identity is stamped on HERE, and this line is what makes
			// the surface usable at all.
			//
			// computePluginSurface works from the declared rectangles alone - it has
			// no plugin to name - so the view it returns carries an EMPTY PluginID.
			// The frontend builds every iframe's asset URL and routes every plugin
			// request by that field, so an empty one meant:
			//
			//   * `src="/plugin-asset//panel.html"` -> the asset route answered
			//     "bad plugin asset path" (an empty id fails splitPluginAssetPath),
			//     which is the error the plugin showed;
			//   * `App.PluginRequest("", ...)` -> every capability call was refused,
			//     so the plugin rendered as unauthorised;
			//   * the frames themselves still mounted and paint over the panel, so
			//     they swallowed the clicks meant for it.
			//
			// The stored state (a.pluginSurface) already carries the id, so this
			// takes it from the same source rather than re-deriving it.
			view.PluginID = pluginSurface.PluginID
			surfaceView = view
			regions = append(surfaceRegions, desktop.Bounds{X: petX, Y: petY, Width: petW, Height: petH})
		} else {
			regions = []desktop.Bounds{
				{X: petX, Y: petY, Width: petW, Height: petH},
			}
		}
	case hasBubble:
		regions = []desktop.Bounds{
			bubblePx,
			{X: petX, Y: petY, Width: petW, Height: petH},
		}
	default:
		regions = []desktop.Bounds{
			{X: petX, Y: petY, Width: petW, Height: petH},
		}
	}
	if err := desktop.SetWindowRegion(hwnd, regions...); err != nil {
		a.log.Warnf("window", "设置窗口区域失败（会露出一条没人画的桌面）：%v", err)
	}

	// Record where the pet actually ended up, so the next toggle starts from the
	// real position.
	petScreen := desktop.Bounds{X: petScreenX, Y: petScreenY, Width: petW, Height: petH}
	layout := Layout{
		WindowW:      float64(totalW) / s,
		WindowH:      float64(totalH) / s,
		PetX:         float64(petX) / s,
		PetY:         float64(petY) / s,
		PetW:         float64(petW) / s,
		PetH:         float64(petH) / s,
		PanelX:       float64(panelX) / s,
		PanelY:       float64(panelY) / s,
		PanelW:       float64(panelW) / s,
		PanelH:       float64(panelH) / s,
		PanelOnRight: panelOnRight,
		Scale:        s,
		// Read on every layout, not cached at startup: the menu flips it, and the next push
		// has to carry the new value or the cards would change only after a restart.
		Theme: a.panelTheme(),
		// The pack travels with the scheme for the same reason, and it is written by the OTHER
		// window (the control center's 主题包 cards). Empty means "no pack chosen": the pet page
		// then drops the attribute and its own stylesheet's default applies - reporting "violet"
		// here would be this program inventing a choice the user never made.
		ThemePack: a.panelThemePack(),
	}
	if hasBubble {
		// The bubble is reported in CSS pixels from the same physical rectangle
		// the region used, so the drawn card and the visible area are the same
		// shape by construction rather than by two matching calculations.
		bubble.X = float64(bubblePx.X) / s
		bubble.Y = float64(bubblePx.Y) / s
		bubble.W = float64(bubblePx.Width) / s
		bubble.H = float64(bubblePx.Height) / s
		bubble.OnRight = panelOnRight
		shown := bubble
		layout.Bubble = &shown
	}
	// The surface view comes from the same computePluginSurface call the region
	// did, so what the page is told to place and what the window is cropped to
	// are one shape by construction.
	layout.PluginSurface = surfaceView

	a.mu.Lock()
	a.petRect = petScreen
	a.petOffsetX, a.petOffsetY = petX, petY
	a.winSizeW, a.winSizeH = totalW, totalH
	a.layout = layout
	// A surface whose rectangles all fell outside the window must not keep
	// claiming to be showing: the state and the layout have to agree, or the
	// next geometry pass would try to crop to rectangles that do not exist.
	if pluginSurface != nil && surfaceView == nil {
		a.setPluginSurfaceLocked(nil)
	}
	// Write the resolved geometry back so GetLayout and the region crop agree
	// with what was just emitted.
	if hasBubble && a.bubble != nil {
		*a.bubble = bubble
	}
	a.mu.Unlock()

	a.savePosition(petScreenX, petScreenY)
	if a.ctx != nil {
		a.emitEvent("pet:layout", layout)
	}
	// The window now exists, is placed, and is cropped. This is the signal Ready() waits for:
	// revealing before it produced an uncropped rectangle with nothing drawn in it.
	a.geomOnce.Do(func() { close(a.geomReady) })
	return layout, nil
}

// restorePosition places the pet at its remembered spot (or the default
// bottom-right on first run), clamped so it is always reachable.
func (a *App) restorePosition() {
	hwnd, err := a.resolveWindow()
	if err != nil {
		return
	}
	work, err := desktop.WorkArea(hwnd)
	if err != nil {
		return
	}
	pw, ph := a.petPixelSize()
	cfg := a.store.Config()

	var x, y int
	firstRun := cfg.Position == nil
	if firstRun {
		// Park the pet against the right edge, vertically centered.
		s := a.currentScale()
		x = work.X + work.Width - pw - int(float64(cfg.EdgeMargin)*s+0.5)
		y = work.Y + (work.Height-ph)/2
	} else {
		// Positions are persisted in logical pixels; convert to physical.
		s := a.currentScale()
		x = int(float64(cfg.Position.X) * s)
		y = int(float64(cfg.Position.Y) * s)
	}
	x, y = clampToWork(x, y, pw, ph, work)
	a.mu.Lock()
	a.petRect = desktop.Bounds{X: x, Y: y, Width: pw, Height: ph}
	a.mu.Unlock()
	if _, err := a.applyWindowGeometry(); err != nil {
		a.log.Warnf("window", "恢复上次位置失败：%v", err)
		return
	}
	if firstRun {
		a.savePosition(x, y)
	}
	a.emitLayout()
}

// savePosition persists a physical-pixel position as logical pixels.
func (a *App) savePosition(x, y int) {
	s := a.currentScale()
	if err := a.store.SetPosition(int(float64(x)/s), int(float64(y)/s)); err != nil {
		a.log.Warnf("window", "保存位置失败：%v", err)
	}
}

// emitLayout pushes the current layout to the frontend.
func (a *App) emitLayout() {
	if a.ctx == nil {
		return
	}
	l, err := a.GetLayout()
	if err != nil {
		return
	}
	a.emitEvent("pet:layout", l)
}

// --- dragging -------------------------------------------------------------

// DragState is returned when a drag starts: the pet's physical-pixel origin on
// screen, which the frontend adds its pointer delta to.
type DragState struct {
	X     int     `json:"x"`
	Y     int     `json:"y"`
	Scale float64 `json:"scale"`
}

// StartDrag closes the panel and returns the pet's current screen origin.
//
// Dragging is driven from the frontend rather than by Wails' built-in
// --wails-draggable mechanism. The built-in version hands the move to Windows as
// a modal loop: the webview never sees the matching mouseup, so a click and a
// drag cannot be told apart, and the window cannot be repositioned from Go until
// the loop ends. Driving it ourselves gives exact disambiguation, which matters
// because a click on the pet must toggle the panel.
//
// The reported origin is the PET's, not the window's. The window is larger than
// the pet (it always has room for the panel), so the frontend must move the pet
// by the pointer delta and let Go place the window around it.
func (a *App) StartDrag() (DragState, error) {
	// Dragging folds the slot away, because a card that stays put while the pet moves is a card
	// pointing at nothing. The panel used to be folded here; it is gone, so what remains is the
	// bubble and any floating surface - and both are folded by the same helper the rest of the
	// window code uses.
	a.mu.Lock()
	hadBubble := a.bubble != nil
	hadSurface := a.pluginSurface != nil
	if hadBubble {
		a.setBubbleLocked(nil)
	}
	if hadSurface {
		a.setPluginSurfaceLocked(nil)
	}
	pet := a.petRect
	a.mu.Unlock()
	if hadBubble {
		a.emitBubble(BubbleNone, false)
	}
	if hadSurface {
		// No surface event exists to emit: the frontend renders the surface from the LAYOUT,
		// and this state change is already reflected there by setPluginSurfaceLocked. The
		// re-crop happens on the caller's next window pass.
	}
	if pet.Width <= 0 {
		pet.Width, pet.Height = a.petPixelSize()
	}
	return DragState{X: pet.X, Y: pet.Y, Scale: a.currentScale()}, nil
}

// DragTo moves the pet to an absolute physical screen position.
//
// The window is re-placed so the pet lands there, which also keeps the panel's
// future position and the cropped region consistent. The window itself is not
// resized, so the webview is untouched and the drag stays smooth.
func (a *App) DragTo(x, y int) error {
	a.mu.Lock()
	pet := a.petRect
	offX, offY := a.petOffsetX, a.petOffsetY
	winW, winH := a.winSizeW, a.winSizeH
	a.mu.Unlock()
	if pet.Width <= 0 {
		pet.Width, pet.Height = a.petPixelSize()
	}
	if winW <= 0 || winH <= 0 {
		return nil
	}
	pet.X, pet.Y = x, y
	a.mu.Lock()
	a.petRect = pet
	a.mu.Unlock()
	// Place the window so the pet's offset inside it lands on (x, y).
	return desktop.SetBounds(a.windowHandle(), desktop.Bounds{
		X: x - offX, Y: y - offY, Width: winW, Height: winH,
	})
}

// windowHandle returns the resolved window, or 0 when it is not available yet.
func (a *App) windowHandle() desktop.Window {
	hwnd, err := a.resolveWindow()
	if err != nil {
		return 0
	}
	return hwnd
}

// EndDrag snaps the pet to the nearest edge, remembers the position and
// returns the resulting layout.
//
// The pet's rect is already up to date (DragTo maintains it), so this only has
// to snap and re-place.
func (a *App) EndDrag() (Layout, error) {
	return a.SnapNow()
}

// SnapNow pulls the pet flush against the closest vertical edge and remembers
// the position. The window is re-placed (and re-cropped) around the pet.
func (a *App) SnapNow() (Layout, error) {
	a.mu.Lock()
	pet := a.petRect
	a.mu.Unlock()
	// Snapping used to be refused while the panel was open, because re-placing the window
	// would move the panel with it. There is no panel to move: the slot beside the pet is
	// empty unless a plugin's surface is up, and a surface is placed FROM the new pet
	// position by the same geometry pass, so snapping with one open is coherent.

	hwnd, err := a.resolveWindow()
	if err != nil {
		return Layout{}, err
	}
	work, err := desktop.WorkArea(hwnd)
	if err != nil {
		return Layout{}, err
	}
	if pet.Width <= 0 {
		w, h := a.petPixelSize()
		pet = desktop.Bounds{X: work.X, Y: work.Y, Width: w, Height: h}
	}

	cfg := a.store.Config()
	if cfg.SnapToEdge {
		gap := int(float64(cfg.EdgeMargin)*a.currentScale() + 0.5)
		distLeft := pet.X - work.X
		distRight := (work.X + work.Width) - (pet.X + pet.Width)
		if distLeft <= distRight {
			pet.X = work.X + gap
		} else {
			pet.X = work.X + work.Width - pet.Width - gap
		}
	}
	pet.Y = clampY(pet.Y, pet.Height, work)

	a.mu.Lock()
	a.petRect = pet
	a.mu.Unlock()
	return a.applyWindowGeometry()
}

func clampY(y, h int, work desktop.Bounds) int {
	if maxY := work.Y + work.Height - h; y > maxY {
		y = maxY
	}
	if y < work.Y {
		y = work.Y
	}
	return y
}

// --- panel ----------------------------------------------------------------

// No SetPanelOpen and no TogglePanel any more: the panel they opened is gone from
// index.html, and the two bindings existed only to show and hide it.
//
// WHAT THEY ALSO DID moved to the things that still need it, which is why this is a deletion
// rather than a rename:
//
//   - "opening the panel folds the other occupants away" is still the rule for every other
//     slot occupant, and showBubble / setPluginSurfaceLocked already implement it in both
//     directions. There is no third occupant left to fold for.
//   - "opening the panel on a hidden pet shows the pet" was the tray's way back from a hidden
//     pet. The tray entry that called it (打开待办插件) now opens the plugin's SURFACE,
//     through OpenPluginSurface - which already shows a hidden pet (surfaceOpensOnAHiddenPet).
//   - the window is still re-cropped; every remaining path calls applyWindowGeometry.

// toggleTodoSurface is what the tray icon's click and 打开待办 do.
//
// It closes the todo plugin's surface if that one is up, and otherwise opens it - the same
// one-gesture-two-directions shape the panel had, now against the surface that replaced it.
// The plugin is found by the slot it CLAIMS rather than by id, for the same reason the pet's
// menu resolves it that way: the host must not know which plugin is "the todo one".
//
// With no claimant, or with a claimant that cannot open (disabled, unrevoked), OpenPluginSurface
// says why in a bubble - a tray click that did nothing would be indistinguishable from a
// broken icon.
func (a *App) toggleTodoSurface() {
	a.mu.Lock()
	current := a.pluginSurface
	a.mu.Unlock()
	if current != nil && current.PluginID == a.todoSlotPluginID() {
		if _, err := a.ClosePluginSurface(); err != nil {
			a.log.Warnf("plugin", "收起待办浮层面失败：%v", err)
		}
		return
	}
	id := a.todoSlotPluginID()
	if id == "" {
		// Nothing claims the slot: the honest answer is the same bubble the chat slot's own
		// resolver produces, pointing at the control center.
		if _, err := a.showBubble(&BubbleView{
			Kind:  BubbleReminder,
			Title: "待办插件还没装好：打开控制中心装上并授权它。",
		}); err != nil {
			a.log.Warnf("plugin", "待办浮层面：说明气泡之后重新裁剪失败：%v", err)
		}
		return
	}
	if _, err := a.OpenPluginSurface(id); err != nil {
		// OpenPluginSurface refuses in its own words (not enabled, no grant, no rectangles).
		// The refusal travels as a bubble so the user can read it: the tray is outside the
		// window, and a log line there is invisible.
		a.log.Warnf("plugin", "待办浮层面打不开：%v", err)
		if _, berr := a.showBubble(&BubbleView{Kind: BubbleReminder, Title: err.Error()}); berr != nil {
			a.log.Warnf("plugin", "待办浮层面：拒绝气泡之后重新裁剪失败：%v", berr)
		}
	}
}

// todoSlotPluginID returns the id of the plugin that claims the 日程 slot, or "".
//
// It mirrors chatSlotPlugin's rule exactly (installed, enabled, pet.surface granted, claims
// the slot) because a slot is a named mount point with ONE resolution rule - two rules for
// two slots would drift, and the drift would show up as a menu cell that opens nothing for
// one plugin and something for another.
func (a *App) todoSlotPluginID() string {
	rows, err := a.PluginsInstalled()
	if err != nil {
		return ""
	}
	for _, row := range rows {
		if !row.Enabled || row.PanelSlot != plugins.PanelSlotTodos {
			continue
		}
		if !hasPermission(row.Granted, plugins.PermPetSurface) {
			continue
		}
		return row.ID
	}
	return ""
}

// SetScale changes the pet size, keeping the pet's bottom-right corner in place
// so it appears to grow from there.
//
// This is the one operation that genuinely changes the window size, because the
// window has to be large enough for a plugin's surface beside the pet. It happens
// from the control center, where a single viewport change is not noticeable, and the
// pet is re-placed immediately afterwards.
func (a *App) SetScale(scale float64) (Layout, error) {
	if scale < store.MinScale || scale > store.MaxScale {
		return Layout{}, fmt.Errorf("缩放比例需在 %.1f 到 %.1f 之间", store.MinScale, store.MaxScale)
	}
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.PetScale = scale }); err != nil {
		return Layout{}, err
	}
	return a.applyScale(scale)
}

// --- state ----------------------------------------------------------------

// State is the full snapshot the frontend renders from.
type State struct {
	Config         store.Config `json:"config"`
	PetImageURL    string       `json:"petImageUrl"`
	HasCustomImage bool         `json:"hasCustomImage"`
	DataDir        string       `json:"dataDir"`
	Autostart      bool         `json:"autostart"`
	Now            string       `json:"now"`
	// No Todos and no Stats member: the list (and the open / overdue / dueSoon counters
	// derived from it) is the todo plugin's own `storage` now. Sending the host's copy would
	// put a SECOND list on screen - and after the migration the host's copy is frozen at
	// whatever it looked like on the last launch, which is worse than empty.
	// No Pomodoro, Records or RecordTotals member: the timer, its archive and the
	// "N sessions / M minutes" counters all belong to the pomodoro plugin, which
	// keeps them in its own storage.
	// No Reminder member: it existed to draw the "next reminder at" strip, and the
	// host stopped computing a next reminder when the todo plugin took reminders
	// over. Sending it would be sending a time nobody can honour.
	// Hotkeys carries the global shortcut state: the switches, the labels and
	// whether the OS actually accepted the registration. It never contains a
	// credential, and it is a view rather than the raw specs because the control
	// center has to answer "did it take?" - which only the OS knows.
	Hotkeys desktop.HotkeyView `json:"hotkeys"`
	// PetVisible is false while the pet is hidden with the shortcut or the tray.
	PetVisible bool `json:"petVisible"`
	// TodoBadge is what the pet's corner badge draws: how many entries are open and whether
	// any of them is overdue or due soon.
	//
	// It is NOT the old `Stats`: that counted the host's own list, which is a snapshot frozen
	// at migration time now. This is computed from the list the TODO PLUGIN publishes (see
	// sharedTodoList), because the badge is the one thing drawn on the pet itself that has to
	// know what is on the user's list.
	//
	// It rides on State rather than on a binding of its own for the reason the old counters
	// did: it changes when the list changes, and every list change already produces a new
	// State. A second call would be a second thing to keep in step.
	TodoBadge store.Stats `json:"todoBadge"`
	// Warnings reports non-fatal problems found while loading data files (for
	// example a corrupt todos.json that had to be reset).
	Warnings []string `json:"warnings,omitempty"`
}

// GetState returns the current configuration, todos and derived counters.
func (a *App) GetState() (State, error) {
	cfg := a.store.Config()
	// Heal the registry entry before reading it, because GetState treats the
	// registry as the source of truth and would otherwise persist a wrong answer.
	//
	// Two ways an entry can exist yet still be unusable, both of which made the
	// checkbox read "on" while nothing started at logon: an older build wrote the
	// path with Go-escaped backslashes, which Explorer cannot resolve, and Task
	// Manager records a separate "disabled" flag that Windows honours over the
	// Run value itself.
	if desktop.RepairAutostart() {
		a.log.Info("autostart", "开机启动的注册表项指向的是旧路径，已修好")
	}
	// The registry is the source of truth for auto-start because the user can
	// also change it outside the app (Task Manager, another tool).
	if actual := desktop.AutostartEnabled(); actual != cfg.Autostart {
		if updated, err := a.store.UpdateConfig(func(c *store.Config) { c.Autostart = actual }); err == nil {
			cfg = updated
		}
	}
	// The badge's numbers come from the todo plugin's own published list, computed here so
	// the page never has to parse another plugin's data.
	dueSoon, overdue, openCount := reminderCounts(a.sharedTodoList(), time.Now())
	return State{
		Config:         cfg,
		TodoBadge:      store.Stats{Open: openCount, Overdue: overdue, DueSoon: dueSoon},
		PetImageURL:    a.PetImageURL(),
		HasCustomImage: a.store.ImagePath() != "",
		DataDir:        a.store.Dir(),
		Autostart:      cfg.Autostart,
		Now:            time.Now().Format(time.RFC3339),
		Hotkeys:        a.hotkeyView(),
		PetVisible:     !a.petHidden(),
		Warnings:       a.store.Warnings(),
	}, nil
}

// PetImageURL returns a URL for the pet picture with a cache-busting token, so
// replacing the file takes effect immediately even though the path is stable.
func (a *App) PetImageURL() string {
	p := a.store.ImagePath()
	if p == "" {
		return ""
	}
	v := "0"
	if fi, err := os.Stat(p); err == nil {
		v = fmt.Sprintf("%d", fi.ModTime().UnixNano())
	}
	return "/pet-image?v=" + v
}

// servePetImage serves the user's pet picture. It is wired into the Wails
// AssetServer, which is how a file outside the embedded bundle is exposed.
//
// It is deliberately unexported: the Wails binding generator only exposes
// exported methods, and there is no reason for JavaScript to call this
// directly (it would have to construct an http.ResponseWriter).
func (a *App) servePetImage(w http.ResponseWriter, r *http.Request) {
	p := a.store.ImagePath()
	if p == "" {
		http.NotFound(w, r)
		return
	}
	mime, ok := store.MimeForExt(filepath.Ext(p))
	if !ok {
		mime = "application/octet-stream"
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.Error(w, "stat failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", mime)
	// The URL carries a version token, so the bytes can be cached hard.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, filepath.Base(p), fi.ModTime(), f)
}

// --- todos ----------------------------------------------------------------

// TodoInput carries an add/edit request from the frontend.
type TodoInput struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Notes string `json:"notes"`
	Due   string `json:"due"` // RFC3339 or "YYYY-MM-DD", "" for none
}

func parseDue(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	// Accept a full timestamp or the date-only value from <input type="date">.
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("无法识别的时间格式 %q", s)
}

func newID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
}

// No todo bindings here any more: AddTodo / UpdateTodo / ToggleTodo / DeleteTodo /
// ClearDone were the panel's CRUD, and the panel is gone because the LIST is the todo
// plugin's own `storage` now (openspec/changes/todo-pomodoro-as-plugins, stages 1 and 3).
//
// They were kept one stage longer than the data moved, on purpose: while the panel still
// existed it needed a way to read something, and the migration is what fills the plugin's
// storage on the first launch. Keeping them past the panel would be two writable copies of
// one list - the one thing `todos.*`'s retirement was supposed to end.
//
// `parseDue` and `newID` above go with them? No - `newID` is still used by the plugin
// request ids, and `parseDue` went with the last caller. See the note on each.
// --- settings -------------------------------------------------------------

// ChoosePetImage opens a file dialog, copies the chosen picture into the data
// directory and returns the refreshed state.
func (a *App) ChoosePetImage() (State, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择桌宠图片",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "图片 (*.png;*.jpg;*.jpeg;*.gif;*.webp;*.bmp;*.svg)",
				Pattern:     "*.png;*.jpg;*.jpeg;*.gif;*.webp;*.bmp;*.svg",
			},
		},
	})
	if err != nil {
		return State{}, err
	}
	if strings.TrimSpace(path) == "" {
		return a.GetState() // dialog cancelled
	}
	if _, err := a.store.SetPetImage(path); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// ResetPetImage goes back to the bundled default pet.
func (a *App) ResetPetImage() (State, error) {
	if err := a.store.ClearPetImage(); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// SetSnapToEdge toggles magnetic edge snapping.
func (a *App) SetSnapToEdge(enabled bool) (State, error) {
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.SnapToEdge = enabled }); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// panelTheme is the scheme BOTH windows draw in, read from the file the two of them share
// (internal/store/theme.go). Nothing is cached here: the value can be changed by the control
// center at any moment, and a cached copy is how the menu and the settings page end up disagreeing.
func (a *App) panelTheme() string {
	if theme := store.ReadPanelTheme(a.store.Dir()); theme != "" {
		return theme
	}
	// Nothing stored yet: light, which is the scheme the control center's window is designed
	// around and therefore the one the pet's cards match on a fresh install.
	return store.ThemeLight
}

// panelThemePack is the palette pack the same file carries, or "" when none was chosen.
//
// The pet never writes this: the control center's 主题包 cards do, and the pet picks it up on its
// next push (like the scheme). Empty is passed through rather than defaulted here, so the
// stylesheet stays the only place that says what "no pack" looks like.
func (a *App) panelThemePack() string {
	return store.ReadPanelThemePack(a.store.Dir())
}

// SetCardTheme switches the scheme for BOTH windows (see panelTheme).
//
// It answers with the whole State like the other setters: the theme travels in Layout.Theme, so
// the caller only has to re-apply the layout it already knows how to apply.
func (a *App) SetCardTheme(theme string) (State, error) {
	next := store.NormalizeTheme(theme)
	if err := store.WritePanelTheme(a.store.Dir(), next); err != nil {
		return State{}, err
	}
	a.log.Infof("ui", "配色切成 %s（桌宠与控制中心共用）", next)
	a.pushLayout()
	return a.GetState()
}

// pushLayout re-sends the current layout, which is how the page learns about a change that is not a
// geometry change.
//
// WHY THE SETTER PUSHES AND THE PAGE DOES NOT ASK. The scheme travels in Layout.Theme, and the page
// paints from whatever layout it last received. `ToggleCardTheme` answers with a bare string, so a
// page that wanted to repaint had to fetch the layout itself - and the version of that which shipped
// passed the THEME STRING to `applyLayout`, which then read `layout.petX` off a string: the menu
// vanished (the pet got positioned at "undefinedpx") and came back on the next push still in the old
// colours. Pushing from here makes every caller correct by construction.
func (a *App) pushLayout() {
	if a.ctx == nil {
		return
	}
	if l, err := a.GetLayout(); err == nil {
		a.emitEvent("pet:layout", l)
	}
}

// ToggleCardTheme flips the scheme and answers with the NEW value.
//
// The menu cell's label is rendered FROM the theme ("深色" while the cards are light, i.e. it names
// the action), so the page needs to know the result without asking again. The repaint does not
// depend on that answer: SetCardTheme pushes a layout.
func (a *App) ToggleCardTheme() (string, error) {
	next := store.ThemeDark
	if a.panelTheme() == store.ThemeDark {
		next = store.ThemeLight
	}
	if _, err := a.SetCardTheme(next); err != nil {
		return "", err
	}
	return next, nil
}

// SetAutostart writes or removes the per-user Run registry entry.
func (a *App) SetAutostart(enabled bool) (State, error) {
	if err := desktop.SetAutostart(enabled); err != nil {
		return State{}, err
	}
	if _, err := a.store.UpdateConfig(func(c *store.Config) { c.Autostart = enabled }); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// DebugLog records one line from the page at DEBUG level.
//
// Pointer handling happens entirely in the webview, so this binding is the only way to
// see drag events from the Go side when diagnosing drag/snap problems - which is why it
// stays a binding even though it now just forwards to the log.
//
// It is DEBUG because it is a probe, not an event: at the shipping level (INFO) it is
// dropped, so the page can keep calling it unconditionally without turning the log into
// a pointer trace. PETPC_DEBUG=1 or PETPC_LOG_LEVEL=debug brings it back.
func (a *App) DebugLog(msg string) {
	a.log.Debug("page", msg)
}

// PageLog records one line from the page at a level the page chooses.
//
// It exists because `fail(err)` in main.js had exactly one output: a toast that vanished
// after 3.2 s. A binding that threw for a user who was not watching at that moment left
// no evidence anywhere - and "点了一下，什么也没发生" is the report that costs the most
// to diagnose. The page now reports the same failure to the log, where the control
// center's 日志 page shows it with the host's own lines around it.
//
// The level string is parsed forgivingly (an unknown word logs at ERROR): a page that
// asked to be recorded and got silence would be the worse outcome.
func (a *App) PageLog(level, message string) {
	lv, ok := logging.ParseLevel(level)
	if !ok {
		lv = logging.LevelError
	}
	a.log.Log(lv, "page", "", message)
}

// ResetToDefaultPosition moves the pet back to its first-run resting place: the
// right edge of the work area, vertically centred, with the configured margin.
//
// The tray menu offers this because a pet dragged to an awkward spot (or left on
// a monitor that is no longer attached) can otherwise be hard to retrieve.
func (a *App) ResetToDefaultPosition() (Layout, error) {
	hwnd, err := a.resolveWindow()
	if err != nil {
		return Layout{}, err
	}
	work, err := desktop.WorkArea(hwnd)
	if err != nil {
		return Layout{}, err
	}
	pw, ph := a.petPixelSize()
	cfg := a.store.Config()
	s := a.currentScale()
	gap := int(float64(cfg.EdgeMargin)*s + 0.5)

	x := work.X + work.Width - pw - gap
	y := work.Y + (work.Height-ph)/2
	x, y = clampToWork(x, y, pw, ph, work)

	// Fold the panel away so the pet is placed on its own.
	a.mu.Lock()
	// The floating surface occupies the same slot, so it goes with it; leaving
	// it up would put a card beside a pet that has just been moved somewhere
	// else.
	a.setPluginSurfaceLocked(nil)
	a.setBubbleLocked(nil)
	a.petRect = desktop.Bounds{X: x, Y: y, Width: pw, Height: ph}
	a.mu.Unlock()
	return a.applyWindowGeometry()
}

// Quit closes the application.
func (a *App) Quit() {
	// Stop the background loops first: they touch the store and emit events, and
	// letting them run while the window is torn down would race the shutdown.
	a.shutdown()
	// Remove the tray icon first: once the message loop exits the window is gone
	// and the icon would linger as a ghost in the notification area until the
	// user hovers over it.
	a.mu.Lock()
	tray := a.tray
	a.tray = nil
	a.mu.Unlock()
	if tray != nil {
		tray.Close()
	}
	runtime.Quit(a.ctx)
}
