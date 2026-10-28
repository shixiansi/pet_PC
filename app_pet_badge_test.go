package main

// The host side of `pet.badge`: the line a plugin draws on the pet itself.
//
// What is worth pinning here, and why each one is not obvious from the code:
//
//  1. the GRANT. The method borrows `pet.surface` instead of a 14th permission (see
//     app_pet_badge.go for why). A borrow that is not enforced is not a grant at all, so a plugin
//     without `pet.surface` must be refused - and refused by the gate, i.e. nothing drawn.
//  2. the EVENT is emitted only when the visible text changed. A timer pushes once a second; if
//     every push repainted, the box would be rewritten 3600 times an hour for 60 distinct values.
//  3. the READ is filtered. A plugin that is disabled or uninstalled stops calling, and a string it
//     left behind must not sit on the pet forever - that is the whole reason the text is derived
//     at read time rather than stored as "the current line".

import (
	"strings"
	"sync"
	"testing"
)

// badgeEvents collects the `pet:badge` emissions, and ignores everything else the app sends.
func badgeEvents(a *App) func() []string {
	var mu sync.Mutex
	var all []string
	a.emitFn = func(event string, data ...any) {
		if event != "pet:badge" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, d := range data {
			if m, ok := d.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					all = append(all, text)
				}
			}
		}
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), all...)
	}
}

func TestPetBadgeNeedsTheSurfaceGrant(t *testing.T) {
	a := newTestApp(t)
	// Declares pet.bubble only: no pet.surface, so no badge.
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"pet.bubble"}})

	if _, err := a.PluginRequest(id, "pet.badge", `{"text":"24:59"}`); err == nil {
		t.Fatal("a plugin without pet.surface was allowed to draw on the pet")
	} else if !strings.Contains(err.Error(), "pet.surface") {
		t.Errorf("the refusal does not name the permission it needs: %v", err)
	}
	if got := a.PetBadge(); got != "" {
		t.Errorf("a refused call still drew %q", got)
	}
}

func TestPetBadgeShowsTheTextAndEmitsOnlyOnChange(t *testing.T) {
	a := newTestApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"pet.surface"}})
	seen := badgeEvents(a)

	if _, err := a.PluginRequest(id, "pet.badge", `{"text":"24:59"}`); err != nil {
		t.Fatalf("pet.badge: %v", err)
	}
	if got := a.PetBadge(); got != "24:59" {
		t.Fatalf("PetBadge() = %q, want 24:59", got)
	}
	// The same text again is the once-a-second push of an unchanged second: no event.
	if _, err := a.PluginRequest(id, "pet.badge", `{"text":"24:59"}`); err != nil {
		t.Fatalf("pet.badge: %v", err)
	}
	if got := seen(); len(got) != 1 {
		t.Errorf("unchanged text re-emitted: %v", got)
	}

	// Empty clears it, and that IS a change.
	if _, err := a.PluginRequest(id, "pet.badge", `{"text":""}`); err != nil {
		t.Fatalf("pet.badge clear: %v", err)
	}
	if got := a.PetBadge(); got != "" {
		t.Errorf("clearing left %q on the pet", got)
	}
	if got := seen(); len(got) != 2 || got[1] != "" {
		t.Errorf("clearing did not emit an empty text: %v", got)
	}
}

func TestPetBadgeDropsTextFromADisabledPlugin(t *testing.T) {
	a := newTestApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"pet.surface"}})
	// The collector is not decoration here: without it emitEvent reaches the real Wails runtime,
	// which refuses a context outside the lifecycle hook and fails the test for an unrelated
	// reason.
	badgeEvents(a)

	if _, err := a.PluginRequest(id, "pet.badge", `{"text":"05:00"}`); err != nil {
		t.Fatalf("pet.badge: %v", err)
	}
	// The plugin stops calling when it is switched off - so the string it left behind has to be
	// dropped by whoever reads it. Reading is the hook, because watching the plugin list is the
	// polling pattern this codebase just removed.
	managerSetEnabled(t, a, id, false)
	if got := a.PetBadge(); got != "" {
		t.Errorf("a disabled plugin's line is still on the pet: %q", got)
	}
}

func TestPetBadgeRejectsOverlongText(t *testing.T) {
	a := newTestApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"pet.surface"}})

	long := strings.Repeat("9", MaxBadgeRunes+1)
	if _, err := a.PluginRequest(id, "pet.badge", `{"text":"`+long+`"}`); err == nil {
		t.Fatal("an overlong badge was accepted")
	}
	if got := a.PetBadge(); got != "" {
		t.Errorf("the rejected text was drawn anyway: %q", got)
	}
}
