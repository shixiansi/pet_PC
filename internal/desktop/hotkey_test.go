package desktop

import "testing"

// The hot-key policy is a pure function so the whole thing - including the rule
// that keeps the pet recoverable - is testable without a window or a keyboard.

// TestShowIsRegisteredBeforeHide pins the ORDER, which is the safety rule rather
// than a detail.
//
// RegisterHotKey can fail per key. Registering show first means a failure stops
// the chain with the pet still on screen; registering hide first would leave a
// hide key that works and no way back.
func TestShowIsRegisteredBeforeHide(t *testing.T) {
	specs, bindings := PlanHotkeys(true, DefaultHotkeys())
	if len(specs) != 2 {
		t.Fatalf("got %d specs, want 2", len(specs))
	}
	if specs[0].Action != HotkeyShow {
		t.Errorf("the first registered key is %v, so a failure could leave the pet hidden", specs[0].Action)
	}
	if specs[1].Action != HotkeyHide {
		t.Errorf("the second registered key is %v, want hide", specs[1].Action)
	}
	if len(bindings) != 2 || !bindings[0].OK || !bindings[1].OK {
		t.Errorf("a default pair failed to plan: %+v", bindings)
	}
}

// TestDefaultHotkeysAreTheDocumentedOnes pins the actual shortcuts, because the
// panel prints them and the user is told them in the README.
//
// Ctrl+ALT specifically, and the modifier is the load-bearing part: a global hot
// key is taken from every application, and Ctrl+Shift+S is "Save As" in most
// editors while Ctrl+Shift+H is find-and-replace in several. Claiming those
// would break a shortcut in whatever the user actually works in.
func TestDefaultHotkeysAreTheDocumentedOnes(t *testing.T) {
	if got := DefaultShowHotkey.Label(); got != "Ctrl+Alt+S" {
		t.Errorf("show label = %q, want Ctrl+Alt+S", got)
	}
	if got := DefaultHideHotkey.Label(); got != "Ctrl+Alt+H" {
		t.Errorf("hide label = %q, want Ctrl+Alt+H", got)
	}
	if DefaultShowHotkey.Action != HotkeyShow || DefaultHideHotkey.Action != HotkeyHide {
		t.Error("the default specs carry the wrong actions")
	}
	// The pair must use the same modifiers, or the panel would be teaching the
	// user two different chords for one feature.
	if len(DefaultShowHotkey.Modifiers) != len(DefaultHideHotkey.Modifiers) {
		t.Fatal("the two shortcuts use different modifier sets")
	}
	for i := range DefaultShowHotkey.Modifiers {
		if DefaultShowHotkey.Modifiers[i] != DefaultHideHotkey.Modifiers[i] {
			t.Errorf("modifier %d differs: %q vs %q",
				i, DefaultShowHotkey.Modifiers[i], DefaultHideHotkey.Modifiers[i])
		}
	}
	// And nothing may claim a Ctrl+Shift combination, which is the collision this
	// moved away from.
	for _, s := range DefaultHotkeys() {
		hasShift := false
		for _, m := range s.Modifiers {
			if m == ModShift {
				hasShift = true
			}
		}
		if hasShift {
			t.Errorf("%s uses Shift, which collides with common application shortcuts", s.Label())
		}
	}
}

// TestDisabledPlansNothing covers the switch: off means no shortcuts are taken
// from the rest of the desktop.
func TestDisabledPlansNothing(t *testing.T) {
	specs, bindings := PlanHotkeys(false, DefaultHotkeys())
	if len(specs) != 0 {
		t.Errorf("a disabled feature planned %d shortcuts", len(specs))
	}
	if len(bindings) != 0 {
		t.Errorf("a disabled feature reported bindings: %+v", bindings)
	}
}

// TestModifierlessKeyIsRejected covers the range rule that keeps the feature from
// breaking the user's desktop: a global hot key is taken away from EVERY
// application, so a bare letter would make that letter unusable everywhere.
func TestModifierlessKeyIsRejected(t *testing.T) {
	bare := HotkeySpec{Action: HotkeyShow, Key: "S"}
	if bare.Valid() {
		t.Error("a bare key with no modifier was accepted")
	}
	// And it poisons the pair rather than being skipped: a lone hide key is the
	// trap this whole file exists to prevent.
	specs, bindings := PlanHotkeys(true, []HotkeySpec{bare, DefaultHideHotkey})
	if len(specs) != 0 {
		t.Errorf("a malformed show key still registered %d keys: %+v", len(specs), specs)
	}
	if len(bindings) != 2 {
		t.Fatalf("got %d bindings, want both reported", len(bindings))
	}
	if bindings[0].OK || bindings[0].Reason == "" {
		t.Errorf("the malformed key was not reported: %+v", bindings[0])
	}
	if bindings[1].OK || bindings[1].Reason == "" {
		t.Errorf("the innocent key was left looking registered: %+v", bindings[1])
	}
}

// TestMalformedHideKeyStopsTheChain is the safety rule seen from the other side:
// a bad HIDE spec must not leave the show key silently unregistered either, and
// must certainly not register anything past it.
func TestMalformedHideKeyStopsTheChain(t *testing.T) {
	badHide := HotkeySpec{Action: HotkeyHide, Key: ""}
	specs, bindings := PlanHotkeys(true, []HotkeySpec{DefaultShowHotkey, badHide})
	if len(specs) != 0 {
		t.Errorf("registered %d keys with a malformed hide spec: %+v", len(specs), specs)
	}
	if len(bindings) != 2 {
		t.Fatalf("got %d bindings, want both reported", len(bindings))
	}
	for i, b := range bindings {
		if b.OK {
			t.Errorf("binding %d claims to be registered: %+v", i, b)
		}
	}
}

// TestSpecValidity walks the accepted and rejected shapes.
func TestSpecValidity(t *testing.T) {
	cases := []struct {
		name string
		spec HotkeySpec
		want bool
	}{
		{"default show", DefaultShowHotkey, true},
		{"default hide", DefaultHideHotkey, true},
		{"alt only", HotkeySpec{Modifiers: []HotkeyModifier{ModAlt}, Key: "A"}, true},
		{"win modifier", HotkeySpec{Modifiers: []HotkeyModifier{ModWin}, Key: "1"}, true},
		{"digit key", HotkeySpec{Modifiers: []HotkeyModifier{ModCtrl}, Key: "5"}, true},
		{"no key", HotkeySpec{Modifiers: []HotkeyModifier{ModCtrl}}, false},
		{"blank key", HotkeySpec{Modifiers: []HotkeyModifier{ModCtrl}, Key: "  "}, false},
		{"no modifiers", HotkeySpec{Key: "S"}, false},
		{"unknown modifier", HotkeySpec{Modifiers: []HotkeyModifier{"Hyper"}, Key: "S"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.spec.Valid(); got != tc.want {
				t.Errorf("Valid() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestVirtualKeyMapping pins the key translation, including the case folding and
// the explicit refusal of anything not a letter or digit.
func TestVirtualKeyMapping(t *testing.T) {
	cases := map[string]uint16{
		"S": 0x53, "s": 0x53,
		"H": 0x48, "h": 0x48,
		"A": 0x41, "Z": 0x5A,
		"0": 0x30, "9": 0x39,
		"": 0, "F5": 0, " ": 0, "-": 0,
	}
	for name, want := range cases {
		if got := virtualKey(name); got != want {
			t.Errorf("virtualKey(%q) = %#x, want %#x", name, got, want)
		}
	}
}

// TestUnavailableViewIsHonest: where there are no hot keys the view must SAY so,
// because the panel renders Active=false and a silent failure would look like a
// bug in the switch.
//
// Enabled is TRUE here on purpose: the user did ask for the shortcuts, and
// reporting false would render their switch as off - making the click look like
// it failed, when the real answer is the Note.
func TestUnavailableViewIsHonest(t *testing.T) {
	v := unavailableHotkeyView()
	if !v.Enabled {
		t.Error("an unsupported platform reports the switch as off, so the click looks broken")
	}
	if v.Active {
		t.Error("an unavailable platform claims the shortcuts are active")
	}
	if v.Note == "" {
		t.Error("no explanation for the user")
	}
	if v.Show != DefaultShowHotkey.Label() || v.Hide != DefaultHideHotkey.Label() {
		t.Error("the labels are not the documented shortcuts")
	}
}

// TestDisabledViewNamesTheSwitch: switched off is a different state from
// unavailable, and the panel has to tell the user which one they are looking at.
func TestDisabledViewNamesTheSwitch(t *testing.T) {
	off := DisabledHotkeyView()
	unavailable := unavailableHotkeyView()
	if off.Note == "" || unavailable.Note == "" {
		t.Fatal("a state has no explanation")
	}
	if off.Note == unavailable.Note {
		t.Error("switched-off and unsupported produce the same message")
	}
}
