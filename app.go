package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/plugins"
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

	mu        sync.Mutex
	hwnd      desktop.Window // native window handle, resolved after startup
	scale     float64        // DPI scale factor of the window's monitor
	panelOpen bool
	petRect   desktop.Bounds // the pet's own rect in physical px
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

	// pluginHost holds the plugin subsystem's non-geometry state: the installed
	// plugins on disk and the per-plugin event subscriptions. It carries its own
	// mutex because a plugin call can block on the network, and App.mu guards
	// window geometry that a drag must never have to queue behind.
	pluginHost pluginHost

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

	// pomoMu serialises the pomodoro state machine. The scheduler and the
	// frontend can both drive it, and every transition is read-modify-write on
	// the persisted state.
	pomoMu sync.Mutex
	// focusPlan is the plan text of the focus session in flight. Guarded by pomoMu;
	// it only lives from StartPomodoroWithPlan until the record is written.
	//
	// It is deliberately memory-only rather than a persisted field: the plan is
	// answered and consumed inside one session, and the permanent copy is the
	// record's Plan. A restart mid-focus therefore loses it, which degrades to
	// an empty Plan on that session's record rather than to a failed write
	// (design.md 1.4).
	focusPlan string

	// foregroundFullscreen is the single place the app reads "is the foreground
	// window full screen?" from. initApp/NewApp set it to
	// desktop.IsForegroundFullscreen; tests swap in a stub. Nil falls back to
	// the production function.
	//
	// It exists because the plan question is deliberately silent under a
	// full-screen window (docs/reminder-pomodoro.md 4.7), and that branch has to
	// be driveable in a windowless unit test - the same shape as the jitter
	// generator and the fullscreen argument the reminder decision already take.
	foregroundFullscreen func(desktop.Window) bool

	// pomoStop ends the timer goroutine at shutdown, remStop the reminder
	// scheduler's. Both are closed exactly once, via shutdownOnce.
	pomoStop chan struct{}
	remStop  chan struct{}
	// shutdownOnce makes stopping the loops idempotent: closing a channel twice
	// panics, and Quit can be reached from both the tray menu and the panel.
	shutdownOnce sync.Once
	// remMu guards the reminder scheduler's own fields.
	remMu sync.Mutex
	// remNextAt is when the next automatic reminder is due, already including
	// the random jitter for that interval. Zero means "not scheduled".
	remNextAt time.Time
	// remMissed records that a reminder came due while it was suppressed by a
	// focus phase, so the pet can make it up once the phase ends.
	remMissed bool
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

	// revealedForQuestion records that the window is visible ONLY because a
	// question arrived while the user had the pet hidden.
	//
	// The two gates are the reason it exists. The summary prompt ends a focus
	// phase and the rest does not start until it is answered; the plan prompt
	// ends a rest and the focus does not start until it is answered. Both park
	// the timer, so raising one into a hidden window is not "a notification
	// nobody can see" - it is a dead end, because nothing on screen says the
	// timer is waiting. The pet therefore comes back for as long as the question
	// is up, and hideAfterQuestion puts it away again the moment it is answered.
	//
	// It is a second flag rather than a third value of `hidden` because the two
	// answer different questions: `hidden` is read everywhere as "is the window
	// hidden right now" (the reminder and proactive gates, the panel's show-on-
	// open path), while this one is the memory of what the USER asked for. A
	// show they performed themselves - the shortcut, the tray, opening the panel
	// - clears it, because answering a question must never undo an explicit
	// "bring it back".
	//
	// Guarded by mu, like the rest of the window state.
	revealedForQuestion bool

	// questionVisibility, when set, replaces the whole native step the question
	// reveal makes: making the pet's window visible WITHOUT activating it, or
	// hiding it again (desktop.ShowNoActivate / desktop.Hide). It exists for the
	// same reason foregroundFullscreen does: the reveal-and-hide-again cycle is a
	// state machine with real gates on it, and this harness has no window, so
	// without a seam the whole cycle would be unreachable in a unit test and only
	// ever exercised by hand. Nil means the production calls.
	questionVisibility func(desktop.Window, bool) error

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
	return &App{
		store:    st,
		scale:    1,
		pomoStop: make(chan struct{}),
		remStop:  make(chan struct{}),
		// The production answer for "is something full screen in front of us?".
		// It is set here rather than read inline so the silent full-screen path
		// can be driven from a test.
		foregroundFullscreen: desktop.IsForegroundFullscreen,
	}, nil
}

// isForegroundFullscreen reports whether the foreground window covers the screen,
// which is what silences the pet during a video or a game.
//
// Every caller goes through this wrapper rather than the desktop helper, so the
// injected probe is the ONLY answer the app can get: a branch that read
// desktop.IsForegroundFullscreen directly would be unreachable in a windowless
// test. The nil fallback covers an App built without NewApp.
func (a *App) isForegroundFullscreen() bool {
	probe := a.foregroundFullscreen
	if probe == nil {
		probe = desktop.IsForegroundFullscreen
	}
	return probe(a.windowHandle())
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Preferences can be changed by the control center while this runs. The store
	// re-reads settings.json on demand, which covers everything a render reads - but the
	// pet's SIZE is baked into the geometry below, so it needs the other half: something
	// that notices the change and applies it. See app_settingswatch.go.
	a.startSettingsWatch() // The Wails window only exists once startup runs, so the native handle is
	// resolved here. A goroutine keeps the UI from waiting on the retry loop.
	go func() {
		hwnd, err := desktop.WaitForWindow(5 * time.Second)
		if err != nil {
			log.Printf("pet: could not locate main window: %v", err)
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
			log.Printf("pet: enable transparency: %v", err)
		}
		// Stop Windows painting the class background brush (solid black, since
		// Wails ignores the alpha) when the window is resized. Without this the
		// pet flashes a black rectangle on every panel toggle and drag.
		if err := desktop.SuppressBackgroundErasure(hwnd); err != nil {
			log.Printf("pet: suppress background erase: %v", err)
		}
		// The pet lives in the notification area, not the taskbar. Wails creates
		// the window with WS_EX_APPWINDOW, which forces a taskbar button.
		if err := desktop.HideFromTaskbar(hwnd); err != nil {
			log.Printf("pet: hide from taskbar: %v", err)
		}
		a.startTray()
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
			log.Printf("pet: 迁移语音数据失败: %v", err)
		}
		// Both background loops start only once the window exists, so their
		// first emit has somewhere to go.
		a.initPomodoro()
		a.startPomodoroLoop()
		a.initReminders()
		a.startReminderLoop()
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
		desktop.TrayMenuAction(desktop.TrayCmdTogglePanel, "打开待办面板"),
		desktop.TrayMenuAction(desktop.TrayCmdRemindNow, "立刻提醒一次"),
		desktop.TrayMenuSeparator(),
		// The mouse route to the same thing the global shortcuts do. It is here
		// because the shortcuts can FAIL (another program may own the
		// combination), and the tray is then the only way to bring the pet back.
		desktop.TrayMenuAction(desktop.TrayCmdShowPet, "显示桌宠"),
		desktop.TrayMenuAction(desktop.TrayCmdHidePet, "隐藏桌宠"),
		desktop.TrayMenuSeparator(),
		desktop.TrayMenuAction(desktop.TrayCmdTogglePomodoro, "开启/关闭番茄钟"),
		desktop.TrayMenuAction(desktop.TrayCmdResetPos, "归位到默认位置"),
		desktop.TrayMenuSeparator(),
		desktop.TrayMenuAction(desktop.TrayCmdOpenData, "打开数据目录"),
		desktop.TrayMenuAction(desktop.TrayCmdOpenMarket, "打开控制中心（插件与设置）"),
		desktop.TrayMenuAction(desktop.TrayCmdQuit, "退出"),
	}
	tray, err := desktop.NewTray(desktop.TrayOptions{
		Tooltip:    "PetPC 桌宠",
		Items:      items,
		OnActivate: func() { a.TogglePanel() },
		OnCommand: func(id uintptr) {
			switch id {
			case desktop.TrayCmdTogglePanel:
				a.TogglePanel()
			case desktop.TrayCmdRemindNow:
				if _, err := a.RemindNow(); err != nil {
					log.Printf("pet: remind now: %v", err)
				}
			case desktop.TrayCmdTogglePomodoro:
				// The tray has no checkable menu, so the click flips the state
				// and the new state is announced by the pet itself (badge, panel).
				enabled := a.store.Pomodoro().Enabled
				if _, err := a.SetPomodoroEnabled(!enabled); err != nil {
					log.Printf("pet: toggle pomodoro: %v", err)
				}
			case desktop.TrayCmdShowPet:
				if err := a.ShowPet(); err != nil {
					log.Printf("pet: show pet: %v", err)
				}
			case desktop.TrayCmdHidePet:
				if err := a.HidePet(); err != nil {
					log.Printf("pet: hide pet: %v", err)
				}
			case desktop.TrayCmdResetPos:
				a.ResetToDefaultPosition()
			case desktop.TrayCmdOpenData:
				if err := desktop.ShellOpen(a.store.Dir()); err != nil {
					log.Printf("pet: open data dir: %v", err)
				}
			case desktop.TrayCmdOpenMarket:
				if _, err := a.OpenPluginMarket(); err != nil {
					// Not fatal and not silent: the program is a sibling exe, so "not
					// found" is a real state the user has to be told about.
					log.Printf("pet: open plugin market: %v", err)
				}
			case desktop.TrayCmdQuit:
				a.Quit()
			}
		},
	})
	if err != nil {
		log.Printf("pet: tray icon unavailable: %v", err)
		a.DebugLog("tray: FAILED: " + err.Error())
		return
	}
	a.DebugLog("tray: icon registered")
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
	// Re-assert transparency: the frame was stripped while the window was
	// hidden, and the first show is what DWM uses to build the composition.
	if err := desktop.MakeTransparent(hwnd); err != nil {
		log.Printf("pet: re-apply transparency: %v", err)
	}
	a.DebugLog("ready: revealing window")
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
	PanelOpen bool    `json:"panelOpen"`
	WindowW   float64 `json:"windowW"`
	WindowH   float64 `json:"windowH"`
	PetX      float64 `json:"petX"`
	PetY      float64 `json:"petY"`
	PetW      float64 `json:"petW"`
	PetH      float64 `json:"petH"`
	PanelX    float64 `json:"panelX"`
	PanelY    float64 `json:"panelY"`
	PanelW    float64 `json:"panelW"`
	PanelH    float64 `json:"panelH"`
	// PanelOnRight is true when the panel sits to the right of the pet.
	PanelOnRight bool    `json:"panelOnRight"`
	Scale        float64 `json:"scale"`

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
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.layout, nil
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
	open := a.panelOpen
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
		log.Printf("pet: suppress background erase: %v", err)
	}
	if err := desktop.StripWindowFrame(hwnd); err != nil {
		log.Printf("pet: strip window frame: %v", err)
	}
	// Re-clear the window blur. Wails applies it for a translucent window, and
	// anything that re-runs the accent setup (activation, a resize) can bring it
	// back, which shows as a frosted wash behind the pet.
	if err := desktop.DisableWindowBlur(hwnd); err != nil {
		log.Printf("pet: disable window blur: %v", err)
	}

	if err := desktop.SetBounds(hwnd, target); err != nil {
		return Layout{}, err
	}

	// Crop the window to the BOUNDING BOXES of what the page paints.
	//
	// While nothing is beside the pet that is just the pet. While the panel is open it is
	// the union of the panel and the pet, and while a bubble is up it is the union of that
	// bubble and the pet.
	//
	// The union matters because the window is a fixed size larger than what is drawn (the
	// pet is shorter than the panel, so there are unpainted margins above and below it
	// beside the panel). Clearing the region exposed those margins, and with the DWM frame
	// extended over the client area they rendered as a translucent glass strip - the pale
	// band along the top edge of the pet's column. Cropping keeps every visible pixel one
	// that the page actually drew.
	//
	// Boxes, NOT shapes: the panel's rounded corners and the bubble's radius stay in the CSS,
	// and the region is deliberately square. Handing the region a radius used to be how the
	// corners were cut, and it is what made them jagged - a GDI region is a 1-bit mask, so
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
	case open:
		regions = []desktop.Bounds{
			{X: panelX, Y: panelY, Width: panelW, Height: panelH},
			{X: petX, Y: petY, Width: petW, Height: petH},
		}
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
		surfaceRegions, view := computePluginSurface(petX, petY, petW, petH, totalW, totalH, s, declared)
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
		log.Printf("pet: set window region: %v", err)
	}

	// Record where the pet actually ended up, so the next toggle starts from the
	// real position.
	petScreen := desktop.Bounds{X: petScreenX, Y: petScreenY, Width: petW, Height: petH}
	layout := Layout{
		PanelOpen:    open,
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
		log.Printf("pet: restore position: %v", err)
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
		log.Printf("pet: save position: %v", err)
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
	a.mu.Lock()
	open := a.panelOpen
	a.mu.Unlock()
	// Dragging happens with the panel folded away.
	if open {
		if _, err := a.SetPanelOpen(false); err != nil {
			return DragState{}, err
		}
	}
	a.mu.Lock()
	pet := a.petRect
	a.mu.Unlock()
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
	open := a.panelOpen
	pet := a.petRect
	a.mu.Unlock()
	// Re-placing the window while the panel is open would move the panel too, so
	// the pet is only snapped when it is on its own.
	if open {
		return a.GetLayout()
	}

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

// SetPanelOpen shows or hides the todo panel by cropping the window.
//
// The window itself never changes size or position here, so the webview's
// viewport is untouched and the pet cannot shift. Only the visible region
// changes, which Windows applies atomically.
//
// Opening the panel folds both other occupants away: all three share the same
// slot, and the region can only describe one of them
// (docs/reminder-pomodoro.md 8.2).
func (a *App) SetPanelOpen(open bool) (Layout, error) {
	a.mu.Lock()
	wasBubble := a.bubble != nil
	wasHidden := a.hidden
	if open {
		// Fold any bubble away through the helper that keeps the layout in step:
		// the frontend renders the bubble from the layout, so a stale one would be
		// drawn on top of the panel.
		a.setBubbleLocked(nil)
		// The plugin floating surface is the third occupant of this slot, so
		// opening the panel folds it away too - the region can only describe one
		// of them, and a surface left in the layout would crop the window around
		// rectangles the page is no longer placing an iframe into.
		a.setPluginSurfaceLocked(nil)
		// Opening the panel implies showing the pet. A hidden window has no
		// visible panel, so the tray's "打开待办面板" on a hidden pet would
		// otherwise do nothing at all - and the menu entry is one of the ways back,
		// so it must not be a dead end.
		//
		// It is also a show the USER asked for, so it settles any debt a question
		// reveal left behind: answering that question later must not put away a
		// pet the user deliberately brought back (see forgetQuestionReveal).
		a.hidden = false
		a.revealedForQuestion = false
	}
	a.panelOpen = open
	a.mu.Unlock()
	if open && wasBubble {
		a.emitBubble(BubbleNone, false)
	}
	if open && wasHidden {
		if hwnd, err := a.resolveWindow(); err == nil {
			if err := desktop.Show(hwnd); err != nil {
				log.Printf("pet: show on panel open: %v", err)
			} else {
				a.emitVisibility()
			}
		}
	}
	// Closing the panel brings back a question that the fold took away, and the
	// plan question needs it most: unlike the summary, which the records tab can
	// edit, NOTHING inside the panel can answer "what is the next focus about" -
	// so losing it leaves the timer parked with nothing on screen explaining why
	// (design.md 2.1; tasks.md T23).
	//
	// The hook runs AFTER a.mu is released, because showPlanBubble reaches
	// showBubble, which takes a.mu itself. It is also gated on there being no
	// bubble already: a reminder that arrived while the panel was open is a
	// legitimate occupant, and the question is asked again on the next fold rather
	// than over the top of it.
	if !open && a.store.Pomodoro().PlanPending {
		a.mu.Lock()
		bubbleUp := a.bubble != nil
		a.mu.Unlock()
		if !bubbleUp {
			a.showPlanBubble()
		}
	}
	return a.applyWindowGeometry()
}

// TogglePanel flips the panel and returns the new layout.
func (a *App) TogglePanel() (Layout, error) {
	a.mu.Lock()
	open := a.panelOpen
	a.mu.Unlock()
	return a.SetPanelOpen(!open)
}

// SetScale changes the pet size, keeping the pet's bottom-right corner in place
// so it appears to grow from there.
//
// This is the one operation that genuinely changes the window size, because the
// window has to be large enough for the panel. It happens from the settings
// panel, where a single viewport change is not noticeable, and the pet is
// re-placed immediately afterwards.
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
	Todos          []store.Todo `json:"todos"`
	Stats          store.Stats  `json:"stats"`
	PetImageURL    string       `json:"petImageUrl"`
	HasCustomImage bool         `json:"hasCustomImage"`
	DataDir        string       `json:"dataDir"`
	Autostart      bool         `json:"autostart"`
	Now            string       `json:"now"`
	// Pomodoro is the timer's current snapshot, recomputed on every read so the
	// countdown is never stale.
	Pomodoro PomodoroView `json:"pomodoro"`
	// Records and RecordTotals back the records tab.
	Records      []store.PomodoroRecord `json:"records"`
	RecordTotals RecordTotals           `json:"recordTotals"`
	// Reminder mirrors the scheduler's settings and its next fire time.
	Reminder ReminderView `json:"reminder"`
	// Hotkeys carries the global shortcut state: the switches, the labels and
	// whether the OS actually accepted the registration. It never contains a
	// credential, and it is a view rather than the raw specs because the panel has
	// to answer "did it take?" - which only the OS knows.
	Hotkeys desktop.HotkeyView `json:"hotkeys"`
	// PetVisible is false while the pet is hidden with the shortcut or the tray.
	// It is sent so the panel can say so; there is no way to hide the pet from the
	// panel itself.
	PetVisible bool `json:"petVisible"`
	// Warnings reports non-fatal problems found while loading data files (for
	// example a corrupt todos.json that had to be reset).
	Warnings []string `json:"warnings,omitempty"`
}

// RecordTotals summarises the permanent archive.
type RecordTotals struct {
	Count        int `json:"count"`
	FocusMinutes int `json:"focusMinutes"`
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
		a.DebugLog("autostart: repaired the Run entry")
	}
	// The registry is the source of truth for auto-start because the user can
	// also change it outside the app (Task Manager, another tool).
	if actual := desktop.AutostartEnabled(); actual != cfg.Autostart {
		if updated, err := a.store.UpdateConfig(func(c *store.Config) { c.Autostart = actual }); err == nil {
			cfg = updated
		}
	}
	count, minutes := a.store.PomodoroTotals()
	return State{
		Config:         cfg,
		Todos:          a.store.Todos(),
		Stats:          a.store.Stats(),
		PetImageURL:    a.PetImageURL(),
		HasCustomImage: a.store.ImagePath() != "",
		DataDir:        a.store.Dir(),
		Autostart:      cfg.Autostart,
		Now:            time.Now().Format(time.RFC3339),
		Pomodoro:       a.pomodoroView(),
		Records:        a.store.PomodoroRecords(),
		RecordTotals:   RecordTotals{Count: count, FocusMinutes: minutes},
		Reminder:       a.reminderView(),
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

// AddTodo appends a new entry and returns the refreshed state.
func (a *App) AddTodo(in TodoInput) (State, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return State{}, fmt.Errorf("标题不能为空")
	}
	due, err := parseDue(in.Due)
	if err != nil {
		return State{}, err
	}
	todos := a.store.Todos()
	todos = append(todos, store.Todo{
		ID:        newID(),
		Title:     title,
		Notes:     strings.TrimSpace(in.Notes),
		Due:       due,
		CreatedAt: time.Now(),
	})
	if err := a.store.SaveTodos(todos); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// UpdateTodo edits an existing entry.
func (a *App) UpdateTodo(in TodoInput) (State, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return State{}, fmt.Errorf("标题不能为空")
	}
	due, err := parseDue(in.Due)
	if err != nil {
		return State{}, err
	}
	todos := a.store.Todos()
	found := false
	for i := range todos {
		if todos[i].ID != in.ID {
			continue
		}
		todos[i].Title = title
		todos[i].Notes = strings.TrimSpace(in.Notes)
		todos[i].Due = due
		found = true
		break
	}
	if !found {
		return State{}, fmt.Errorf("未找到该待办")
	}
	if err := a.store.SaveTodos(todos); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// ToggleTodo flips the done flag.
func (a *App) ToggleTodo(id string) (State, error) {
	todos := a.store.Todos()
	found := false
	for i := range todos {
		if todos[i].ID != id {
			continue
		}
		todos[i].Done = !todos[i].Done
		if todos[i].Done {
			now := time.Now()
			todos[i].DoneAt = &now
		} else {
			todos[i].DoneAt = nil
		}
		found = true
		break
	}
	if !found {
		return State{}, fmt.Errorf("未找到该待办")
	}
	if err := a.store.SaveTodos(todos); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// DeleteTodo removes an entry.
func (a *App) DeleteTodo(id string) (State, error) {
	todos := a.store.Todos()
	out := make([]store.Todo, 0, len(todos))
	for _, t := range todos {
		if t.ID != id {
			out = append(out, t)
		}
	}
	if err := a.store.SaveTodos(out); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// ClearDone removes every completed entry.
func (a *App) ClearDone() (State, error) {
	todos := a.store.Todos()
	out := make([]store.Todo, 0, len(todos))
	for _, t := range todos {
		if !t.Done {
			out = append(out, t)
		}
	}
	if err := a.store.SaveTodos(out); err != nil {
		return State{}, err
	}
	return a.GetState()
}

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

// DebugLog appends a diagnostic line to debug.log in the data directory, but
// only when PETPC_DEBUG is set.
//
// Pointer handling happens entirely in the webview, so this is the only way to
// see drag events from the Go side when diagnosing drag/snap problems.
func (a *App) DebugLog(msg string) {
	if os.Getenv("PETPC_DEBUG") == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(a.store.Dir(), "debug.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), msg)
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
	a.panelOpen = false
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
