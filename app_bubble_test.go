package main

import (
	"os"
	"path/filepath"
	"testing"

	"PetPC/internal/store"
)

// newTestApp builds an App with a store in a temp directory.
//
// It has no window, so anything that needs the native handle (applyWindowGeometry)
// fails and is logged rather than applied - which is fine, and deliberately part
// of what these tests pin: the bubble/layout invariant must hold even when the
// window cannot be resolved.
func newTestApp(t *testing.T) *App {
	t.Helper()
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return &App{store: s}
}

// bubbleState reads the bubble and the layout's view of it together.
func (a *App) bubbleState() (showing *BubbleView, advertised *BubbleView) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bubble, a.layout.Bubble
}

// TestBubbleAndLayoutStayInStep is the regression test for "the prompt will not go
// away".
//
// The frontend renders the bubble from the LAYOUT (GetLayout -> applyLayout). The
// bug was that clearing a bubble updated a.bubble but left a.layout.Bubble
// pointing at the old bubble, so the very next GetLayout handed the frontend the
// prompt again and redrew it.
func TestBubbleAndLayoutStayInStep(t *testing.T) {
	a := newTestApp(t)

	// Nothing showing: both must be nil.
	showing, advertised := a.bubbleState()
	if showing != nil || advertised != nil {
		t.Fatalf("a fresh app advertises a bubble: showing=%+v advertised=%+v", showing, advertised)
	}

	// Showing a bubble must register it in BOTH places.
	if _, err := a.showBubble(&BubbleView{Kind: BubbleReminder, Title: "pick"}); err != nil {
		// A window error is expected and harmless here.
		t.Logf("showBubble (no window): %v", err)
	}
	showing, advertised = a.bubbleState()
	if showing == nil || advertised == nil {
		t.Fatalf("showBubble did not update both: showing=%+v advertised=%+v", showing, advertised)
	}
	if showing.Kind != BubbleReminder || advertised.Kind != BubbleReminder {
		t.Fatalf("wrong kind: showing=%q advertised=%q", showing.Kind, advertised.Kind)
	}

	// Closing it must clear BOTH. This is the assertion that fails on the buggy
	// version, where the layout kept the closed bubble.
	a.clearBubble(BubbleReminder)
	showing, advertised = a.bubbleState()
	if showing != nil {
		t.Errorf("clearBubble left the bubble showing: %+v", showing)
	}
	if advertised != nil {
		t.Errorf("clearBubble left the layout advertising the bubble: %+v", advertised)
	}

	// DismissBubble takes the same path, and closes anything - there is no kind the
	// host refuses to dismiss any more (the two pomodoro gates are the plugin's).
	if _, err := a.showBubble(&BubbleView{Kind: BubbleReminder, Title: "pick"}); err != nil {
		t.Logf("showBubble (no window): %v", err)
	}
	if _, err := a.DismissBubble(); err != nil {
		t.Logf("DismissBubble (no window): %v", err)
	}
	showing, advertised = a.bubbleState()
	if showing != nil || advertised != nil {
		t.Errorf("DismissBubble left something behind: showing=%+v advertised=%+v", showing, advertised)
	}

	// Opening the panel must fold the bubble away in both places too.
	if _, err := a.showBubble(&BubbleView{Kind: BubbleReminder, Title: "hi"}); err != nil {
		t.Logf("showBubble (no window): %v", err)
	}
	if _, err := a.SetPanelOpen(true); err != nil {
		t.Logf("SetPanelOpen (no window): %v", err)
	}
	showing, advertised = a.bubbleState()
	if showing != nil || advertised != nil {
		t.Errorf("opening the panel left a bubble: showing=%+v advertised=%+v", showing, advertised)
	}
	// ...and the panel itself must actually be open.
	a.mu.Lock()
	open := a.panelOpen
	a.mu.Unlock()
	if !open {
		t.Error("SetPanelOpen(true) did not open the panel")
	}
}

// TestRecordFileHasNoDeletePath is a structural check: the store must expose no
// way to delete a record, which is the whole "permanent record" guarantee.
//
// The records outlived the gates: a finished session is archived with an empty
// summary now, and the archive is still append-only and editable, never deletable.
func TestRecordFileHasNoDeletePath(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("internal", "store", "pomodoro.go"))
	if err != nil {
		t.Skipf("cannot read store source: %v", err)
	}
	for _, forbidden := range []string{"func (s *Store) DeletePomodoroRecord", "func (s *Store) ClearPomodoroRecords"} {
		if containsString(string(src), forbidden) {
			t.Errorf("the store exposes a way to delete records: %s", forbidden)
		}
	}
}

func containsString(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
