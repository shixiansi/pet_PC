//go:build windows

package desktop

import (
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows implementation of the global hot keys.
//
// RegisterHotKey is thread-affine in a way that is easy to get wrong: the key is
// owned by the THREAD that registered it, and WM_HOTKEY is posted to that same
// thread's queue. Registering from an arbitrary goroutine and then having no loop
// on that thread means the key either never fires, or stops firing as soon as the
// goroutine is rescheduled onto another OS thread.
//
// So this follows the shape the tray icon already proved (tray_windows.go): one
// dedicated OS thread, locked for the lifetime of the manager, owning a
// message-only window and running its own GetMessage loop. The window exists only
// so there is an HWND to hang the registration on and a queue to receive
// WM_HOTKEY; it is never shown.
//
// Commands are handed to that thread over a channel and applied ON it, so
// RegisterHotKey and UnregisterHotKey are always called from their owner.

var (
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
	procPeekMessageW     = user32.NewProc("PeekMessageW")
)

const (
	// wmHotkey is WM_HOTKEY.
	wmHotkey = 0x0312
	// wmAppHotkeyQuit asks the manager's loop to exit. WM_APP + 2 is free: this
	// window is ours alone (the tray uses WM_APP + 1).
	wmAppHotkeyQuit = 0x8000 + 2

	// pmRemove is PM_REMOVE for PeekMessage.
	pmRemove = 0x0001

	// hotkeyPollEvery is how long the loop sleeps when there is neither a message
	// nor a command. It is the worst-case delay for a settings change; the hot
	// key itself is dispatched the moment the message is peeked, so this does not
	// affect how promptly a shortcut fires.
	hotkeyPollEvery = 10 * time.Millisecond

	// RegisterHotKey modifier flags.
	modAlt      = 0x0001
	modControl  = 0x0002
	modShiftKey = 0x0004
	modWinKey   = 0x0008
	// modNoRepeat stops a held key from firing at the keyboard repeat rate.
	modNoRepeat = 0x4000

	// hotkeyIdBase is the first Win32 hot-key id. Ids are per-window and must be
	// below 0xBFFF, so a small base plus the action is plenty.
	hotkeyIdBase = 0xA0
)

// hotkeyCmd is one instruction for the manager thread.
type hotkeyCmd struct {
	// specs is the set to register; nil together with quit means "stop".
	specs []HotkeySpec
	quit  bool
	// done receives the resulting view. Buffered by the sender so the manager
	// never blocks on a caller that has given up.
	done chan HotkeyView
}

// hotkeyReady reports the outcome of starting the thread.
type hotkeyReady struct {
	view HotkeyView
	err  error
}

// hotkeyManager is the live manager. It mirrors trayRuntime's shape: the window
// procedure reaches it through a package variable, because a Win32 callback
// cannot carry Go context.
type hotkeyManager struct {
	hwnd   uintptr
	onFire func(HotkeyAction)
	// registered maps a Win32 id back to the action that owns it.
	//
	// It is touched only from the manager's own thread - apply() runs there, and
	// so does the window procedure that reads it - so it needs no lock. The
	// hotkeyMu below guards the package-level pointer, not this map.
	registered map[int]HotkeyAction
}

var (
	hotkeyMu      sync.Mutex
	hotkeyCurrent *hotkeyManager
)

// hotkeyThread is a running manager thread.
type hotkeyThread struct {
	cmds chan hotkeyCmd
	// stopped is closed when the thread's loop exits, so Close cannot block on a
	// thread that is already gone.
	stopped chan struct{}
	once    sync.Once
}

// NewHotkeys starts the manager and registers the given shortcuts.
//
// onFire is called on the manager's thread each time a shortcut is pressed. The
// returned view reports what actually took effect; the error is only for "there
// is no hot-key facility at all here", which the caller treats as "leave the
// feature switched off" rather than as a crash.
func NewHotkeys(specs []HotkeySpec, onFire func(HotkeyAction)) (HotkeyManager, HotkeyView, error) {
	t := &hotkeyThread{
		cmds:    make(chan hotkeyCmd),
		stopped: make(chan struct{}),
	}
	ready := make(chan hotkeyReady, 1)
	go t.run(specs, onFire, ready)
	res := <-ready
	if res.err != nil {
		return nil, res.view, res.err
	}
	return t, res.view, nil
}

// Register replaces the registered set. Safe to call from any goroutine.
func (t *hotkeyThread) Register(specs []HotkeySpec) HotkeyView {
	done := make(chan HotkeyView, 1)
	select {
	case t.cmds <- hotkeyCmd{specs: specs, done: done}:
		return <-done
	case <-t.stopped:
		// The manager is gone; report it as a whole-feature problem rather than
		// pretending the keys took.
		return unavailableHotkeyView()
	}
}

// Close unregisters everything and stops the thread. Safe to call more than once.
func (t *hotkeyThread) Close() {
	t.once.Do(func() {
		done := make(chan HotkeyView, 1)
		select {
		case t.cmds <- hotkeyCmd{quit: true, done: done}:
			<-done
		case <-t.stopped:
			// Already stopped; nothing to unregister.
		}
	})
}

func (t *hotkeyThread) run(specs []HotkeySpec, onFire func(HotkeyAction), ready chan hotkeyReady) {
	// The window, the registrations and the message loop must all stay on one OS
	// thread for the lifetime of the manager.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.stopped)

	hInst, _, _ := procModuleHndl.Call(0)
	className := utf16Ptr("PetPCHotkeyWindow")
	wc := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   windows.NewCallback(hotkeyWndProc),
		HInstance:     windows.Handle(hInst),
		LpszClassName: className,
	}
	// A failure here means the class already exists, which is harmless.
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// HWND_MESSAGE (-3): a message-only window, never visible.
	const hwndMessage = ^uintptr(2)
	hwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)),
		0, 0, 0, 0, 0, 0,
		hwndMessage, 0, hInst, 0,
	)
	if hwnd == 0 {
		view := unavailableHotkeyView()
		ready <- hotkeyReady{view: view, err: fmt.Errorf("CreateWindowEx for the hot-key window failed")}
		return
	}

	rt := &hotkeyManager{
		hwnd:       hwnd,
		onFire:     onFire,
		registered: map[int]HotkeyAction{},
	}
	hotkeyMu.Lock()
	hotkeyCurrent = rt
	hotkeyMu.Unlock()

	// The first registration, applied on this thread like every later one. Its
	// result is what the caller is waiting on.
	ready <- hotkeyReady{view: rt.apply(specs)}

	// THE MESSAGE PUMP. This is the part that makes the hot keys fire at all:
	// RegisterHotKey makes Windows POST WM_HOTKEY to this thread's queue, and a
	// queue nobody drains means the key is reserved but the handler never runs -
	// the shortcut silently does nothing while looking perfectly registered.
	//
	// The loop cannot simply block on GetMessage, because it also has to service
	// the command channel (settings changes). So it pumps eagerly and sleeps only
	// when there is genuinely nothing to do, which is what PeekMessage is for.
	// A hot key is dispatched the moment it is peeked, so the sleep never delays
	// a shortcut - only the next settings change, by at most hotkeyPollEvery.
	var m msg
	for {
		// Drain every pending message first: a burst of WM_HOTKEY presses must
		// not be throttled by the command channel.
		for {
			ret, _, _ := procPeekMessageW.Call(
				uintptr(unsafe.Pointer(&m)), hwnd, 0, 0, pmRemove,
			)
			if ret == 0 {
				break
			}
			if m.Message == wmQuit {
				rt.shutdown(hwnd, className, hInst)
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}

		select {
		case cmd, ok := <-t.cmds:
			if !ok || cmd.quit {
				rt.shutdown(hwnd, className, hInst)
				if cmd.done != nil {
					cmd.done <- HotkeyView{}
				}
				return
			}
			cmd.done <- rt.apply(cmd.specs)
		default:
			// Nothing to do. Sleep briefly so this is not a spin loop; PeekMessage
			// above is what keeps hot keys immediate.
			time.Sleep(hotkeyPollEvery)
		}
	}
}

// shutdown releases the registrations and tears the window down.
func (rt *hotkeyManager) shutdown(hwnd uintptr, className *uint16, hInst uintptr) {
	rt.clear()
	procDestroyWindow.Call(hwnd)
	procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), hInst)
	hotkeyMu.Lock()
	hotkeyCurrent = nil
	hotkeyMu.Unlock()
}

// apply unregisters everything and registers the given specs, in order.
//
// The ORDER is the safety rule: show first, then hide. If the show key is taken
// by another program, the chain stops and nothing is left registered - so the pet
// can never be hidden by a shortcut that has no counterpart. If the HIDE key is
// the one that fails, the show key stays registered, which is precisely the state
// that keeps the pet recoverable.
func (rt *hotkeyManager) apply(specs []HotkeySpec) HotkeyView {
	rt.clear()

	view := HotkeyView{
		// The user asked for these specs, so the feature IS enabled - this view
		// must never report otherwise, or the panel renders the switch as off and
		// the user's click looks like it bounced back.
		Enabled: true,
		Show:    DefaultShowHotkey.Label(),
		Hide:    DefaultHideHotkey.Label(),
	}
	taken := 0
	for _, s := range specs {
		id := hotkeyIdBase + int(s.Action)
		if err := rt.registerOne(id, s); err != nil {
			view.Problems = append(view.Problems, fmtHotkeyProblem(s, "已被其他程序占用，或系统不允许"))
			// Stop the chain: registering a later key after an earlier one failed
			// would break the ordering this whole file depends on.
			break
		}
		rt.registered[id] = s.Action
		taken++
	}
	// Active only when the WHOLE pair took: a lone show key is harmless, but the
	// panel must not claim the shortcuts work when half of them do not.
	view.Active = taken > 0 && taken == len(specs)
	// Nothing to register (the switch was turned off): that is not "enabled".
	if len(specs) == 0 {
		return DisabledHotkeyView()
	}
	return view
}

// registerOne performs the Win32 registration for one spec.
func (rt *hotkeyManager) registerOne(id int, s HotkeySpec) error {
	var mods uintptr
	for _, m := range s.Modifiers {
		switch m {
		case ModAlt:
			mods |= modAlt
		case ModCtrl:
			mods |= modControl
		case ModShift:
			mods |= modShiftKey
		case ModWin:
			mods |= modWinKey
		}
	}
	// A held shortcut must fire once, not at the keyboard repeat rate.
	mods |= modNoRepeat

	vk := virtualKey(s.Key)
	if vk == 0 {
		return fmt.Errorf("unknown key %q", s.Key)
	}
	ret, _, err := procRegisterHotKey.Call(rt.hwnd, uintptr(id), mods, uintptr(vk))
	if ret == 0 {
		return fmt.Errorf("RegisterHotKey(%d): %w", id, err)
	}
	return nil
}

// clear unregisters everything currently held.
func (rt *hotkeyManager) clear() {
	for id := range rt.registered {
		procUnregisterHotKey.Call(rt.hwnd, uintptr(id))
		delete(rt.registered, id)
	}
}

// hotkeyWndProc receives WM_HOTKEY for this window.
func hotkeyWndProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	if message == wmHotkey {
		hotkeyMu.Lock()
		rt := hotkeyCurrent
		hotkeyMu.Unlock()
		if rt != nil && rt.onFire != nil {
			// wParam carries the id that was registered. This runs on the
			// manager's own thread, which is the only thread that mutates the
			// map, so no lock is needed for the lookup.
			if action, ok := rt.registered[int(wparam)]; ok {
				rt.onFire(action)
			}
		}
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lparam)
	return ret
}
