package desktop

import (
	"fmt"
	"strings"
)

// Global hot keys: system-wide shortcuts that work while another application owns
// the keyboard.
//
// They exist for one job - getting the pet back when it is not on screen. A
// desktop pet that can be hidden with no way to bring it back is a trap, and the
// tray icon is not enough on its own: it can be folded into the overflow area,
// and the user who just hid the pet is exactly the user who cannot find it.
//
// THE SAFETY RULE, which everything below follows from: the hide key must never
// be registered unless the show key is. RegisterHotKey can fail per key - another
// program may already own the combination - so a half-applied pair would leave a
// pet that can be hidden but not shown. That is the one unrecoverable failure
// this feature could have, so it is designed out rather than handled: the keys
// are registered IN ORDER, show first, and the first failure stops the chain.
// Whatever was registered before the failure stays, which means a failed hide key
// leaves a working show key, and a failed show key leaves nothing at all.

// HotkeyAction identifies what a hot key does.
type HotkeyAction int

const (
	// HotkeyShow brings the pet back and puts it in front.
	HotkeyShow HotkeyAction = iota
	// HotkeyHide hides the pet, folding away whatever is beside it.
	HotkeyHide
)

// HotkeyModifier is one modifier key.
type HotkeyModifier string

const (
	ModCtrl  HotkeyModifier = "Ctrl"
	ModShift HotkeyModifier = "Shift"
	ModAlt   HotkeyModifier = "Alt"
	ModWin   HotkeyModifier = "Win"
)

// HotkeySpec is one global shortcut.
//
// The modifier and key parts are kept as data rather than as a pre-built Win32
// bitmask so the same value can be formatted for the panel, translated for the
// platform call, and asserted on in tests without a window or a keyboard.
type HotkeySpec struct {
	Action HotkeyAction
	// Modifiers are the required modifier keys.
	Modifiers []HotkeyModifier
	// Key is the main key, e.g. "S" or "H".
	Key string
}

// Label renders a shortcut the way the panel shows it, e.g. "Ctrl+Alt+S".
func (s HotkeySpec) Label() string {
	parts := make([]string, 0, len(s.Modifiers)+1)
	for _, m := range s.Modifiers {
		parts = append(parts, string(m))
	}
	parts = append(parts, s.Key)
	return strings.Join(parts, "+")
}

// Valid reports whether the spec could name a real shortcut.
//
// A bare key with no modifier is rejected on purpose: a global hot key takes the
// combination away from EVERY application, so "S" alone would make the letter S
// unusable everywhere. Requiring a modifier is the smallest thing that stops this
// feature from breaking the user's whole desktop.
func (s HotkeySpec) Valid() bool {
	if strings.TrimSpace(s.Key) == "" || len(s.Modifiers) == 0 {
		return false
	}
	for _, m := range s.Modifiers {
		switch m {
		case ModCtrl, ModShift, ModAlt, ModWin:
		default:
			return false
		}
	}
	return true
}

// Default hot keys: Ctrl+Alt+S shows the pet, Ctrl+Alt+H hides it.
//
// S for show and H for hide, rather than one toggle key, and that is deliberate.
// A toggle is a state the user cannot see the value of, so a single missed
// keypress turns "bring it back" into "hide it again" - the worst possible
// outcome for the one feature whose whole purpose is recovery.
//
// Why Ctrl+Alt and not Ctrl+Shift: a global hot key is taken away from EVERY
// application, so the modifier pair decides how much damage a collision can do.
// Ctrl+Shift+S is "Save As" in most editors and Ctrl+Shift+H is find-and-replace
// in several, so claiming those turns a convenience into a broken shortcut in
// whatever the user actually works in - and when the other program wins the race,
// the pet's own shortcut silently fails instead. Ctrl+Alt is used by far less
// common software, which is exactly the property to optimise for here.
var (
	// DefaultShowHotkey brings the pet back.
	DefaultShowHotkey = HotkeySpec{Action: HotkeyShow, Modifiers: []HotkeyModifier{ModCtrl, ModAlt}, Key: "S"}
	// DefaultHideHotkey hides the pet.
	DefaultHideHotkey = HotkeySpec{Action: HotkeyHide, Modifiers: []HotkeyModifier{ModCtrl, ModAlt}, Key: "H"}
)

// DefaultHotkeys is the pair in registration order: show BEFORE hide.
//
// The order is load-bearing, not cosmetic - see the safety rule at the top of
// this file.
func DefaultHotkeys() []HotkeySpec {
	return []HotkeySpec{DefaultShowHotkey, DefaultHideHotkey}
}

// HotkeyBinding is the outcome for one requested shortcut.
type HotkeyBinding struct {
	Spec HotkeySpec
	// OK is true when the shortcut is registered and will fire.
	OK bool
	// Reason explains a failure in one line, for the settings panel. It is the
	// answer to "I pressed it and nothing happened".
	Reason string
}

// HotkeyView is the whole hot-key state as the frontend sees it.
//
// It is a view rather than the raw specs because the panel has to answer three
// different questions - is the feature on, what are the keys, and did they
// actually take - and only the last one needs the OS to have been asked.
type HotkeyView struct {
	Enabled bool `json:"enabled"`
	// Show/Hide are the display labels, e.g. "Ctrl+Alt+S".
	Show string `json:"show"`
	Hide string `json:"hide"`
	// Active is true when BOTH keys are registered and will fire.
	Active bool `json:"active"`
	// Problems lists the shortcuts that could not be registered, with the reason.
	// A non-empty list with Enabled true is the "you pressed it and nothing
	// happened" case, surfaced in the panel instead of being silent.
	Problems []string `json:"problems,omitempty"`
	// Note explains a whole-feature problem (switched off, or unavailable on this
	// platform).
	Note string `json:"note,omitempty"`
}

// PlanHotkeys decides which shortcuts may be registered, without touching the
// OS.
//
// It exists so the ordering and validation policy is a pure function the tests
// can pin, and so the platform manager only has to deal with the OS call itself.
// The returned specs are in registration order.
//
// Validation is ALL-OR-NOTHING, which is a different rule from the OS-level one
// in the manager, and the difference is deliberate:
//
//   - A malformed spec is a bug in this program, not a hostile environment. The
//     shortcuts are fixed constants, so there is nothing the user could have done
//     to cause it, and registering the half that happens to be well-formed would
//     only hide the bug behind a feature that appears to work.
//   - A registration the OS REFUSES is a real, expected condition (another
//     program may already own the combination). There, keeping the keys that did
//     take is right, and the ordering rules in the manager make the kept subset
//     safe.
//
// Every requested spec is reported in bindings, including any skipped after a
// failure: the panel renders one line per shortcut, and leaving a spec out of the
// list would make it look like it was never asked for.
func PlanHotkeys(enabled bool, specs []HotkeySpec) ([]HotkeySpec, []HotkeyBinding) {
	if !enabled {
		return nil, nil
	}
	bindings := make([]HotkeyBinding, 0, len(specs))
	bad := false
	for _, s := range specs {
		b := HotkeyBinding{Spec: s}
		if s.Valid() {
			b.OK = true
		} else {
			b.Reason = "快捷键需要一个修饰键（如 Ctrl、Shift），并且要有主键"
			bad = true
		}
		bindings = append(bindings, b)
	}
	if bad {
		for i := range bindings {
			bindings[i].OK = false
			if bindings[i].Reason == "" {
				bindings[i].Reason = "另一个快捷键不可用，本组未注册"
			}
		}
		return nil, bindings
	}
	out := make([]HotkeySpec, 0, len(specs))
	out = append(out, specs...)
	return out, bindings
}

// HotkeyManager owns the registered shortcuts.
//
// Register returns the view so a partial failure is reported rather than hidden:
// the caller stores it and the panel renders it. Registering an empty slice
// releases everything, which is how the switch turns the shortcuts off without a
// restart.
type HotkeyManager interface {
	// Register applies exactly these specs, replacing anything registered before.
	Register(specs []HotkeySpec) HotkeyView
	// Close unregisters everything and stops the manager's thread.
	Close()
}

// NoteDisabled is the explanation shown when the shortcuts are switched off.
const NoteDisabled = "全局快捷键已关闭"

// ConfiguredHotkeyView is the view when the OS has not been asked yet.
//
// It reports the switch position with the documented labels, so the settings
// panel has something honest to render before the manager has started (and on the
// platforms where it never will).
func ConfiguredHotkeyView(enabled bool) HotkeyView {
	view := HotkeyView{
		Enabled: enabled,
		Show:    DefaultShowHotkey.Label(),
		Hide:    DefaultHideHotkey.Label(),
	}
	if !enabled {
		view.Note = NoteDisabled
	}
	return view
}

// DisabledHotkeyView is the answer when the feature is switched off.
func DisabledHotkeyView() HotkeyView { return ConfiguredHotkeyView(false) }

// unavailableHotkeyView is the honest answer where there are no global hot keys.
//
// Enabled is true because the user DID ask for the shortcuts - saying false here
// would render their switch as off and make it look like the click failed, when
// the real answer is the Note.
func unavailableHotkeyView() HotkeyView {
	return HotkeyView{
		Enabled: true,
		Show:    DefaultShowHotkey.Label(),
		Hide:    DefaultHideHotkey.Label(),
		Note:    "这个平台不支持全局快捷键",
	}
}

// HotkeySpecs is a convenience for the app: the pair in registration order.
func HotkeySpecs(show, hide HotkeySpec) []HotkeySpec { return []HotkeySpec{show, hide} }

// fmtHotkeyProblem formats one registration failure for the panel.
func fmtHotkeyProblem(s HotkeySpec, reason string) string {
	return fmt.Sprintf("%s：%s", s.Label(), reason)
}

// virtualKey maps a spec's key name to a Win32 virtual-key code.
//
// It is pure arithmetic with no Windows API in it, so it lives here rather than
// behind the build tag: its test then compiles and runs everywhere, including on
// the platforms where NewHotkeys is a stub.
//
// Exactly ONE character is accepted: a letter or a digit. Multi-character names
// ("F5", "Esc") are deliberately rejected rather than guessed at, because their
// virtual-key codes come from a separate table, and deriving them from the first
// byte would silently register the wrong key - the user would press their
// shortcut and something else would happen.
func virtualKey(name string) uint16 {
	if len(name) != 1 {
		return 0
	}
	c := name[0]
	if c >= 'a' && c <= 'z' {
		c -= 'a' - 'A'
	}
	switch {
	case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return uint16(c)
	}
	return 0
}
