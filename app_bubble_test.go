package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

// TestBubbleAndLayoutStayInStep is the regression test for "clicking 保存 does
// nothing and the summary prompt will not go away".
//
// The frontend renders the bubble from the LAYOUT (GetLayout -> applyLayout). The
// bug was that clearing a bubble updated a.bubble but left a.layout.Bubble
// pointing at the old bubble, so the very next GetLayout handed the frontend the
// prompt again and redrew it. The record was written correctly the whole time;
// only the UI was stuck.
func TestBubbleAndLayoutStayInStep(t *testing.T) {
	a := newTestApp(t)

	// Nothing showing: both must be nil.
	showing, advertised := a.bubbleState()
	if showing != nil || advertised != nil {
		t.Fatalf("a fresh app advertises a bubble: showing=%+v advertised=%+v", showing, advertised)
	}

	// Showing a bubble must register it in BOTH places.
	if _, err := a.showBubble(&BubbleView{Kind: BubbleSummary, Title: "x", RecordID: "r1"}); err != nil {
		// A window error is expected and harmless here.
		t.Logf("showBubble (no window): %v", err)
	}
	showing, advertised = a.bubbleState()
	if showing == nil || advertised == nil {
		t.Fatalf("showBubble did not update both: showing=%+v advertised=%+v", showing, advertised)
	}
	if showing.Kind != BubbleSummary || advertised.Kind != BubbleSummary {
		t.Fatalf("wrong kind: showing=%q advertised=%q", showing.Kind, advertised.Kind)
	}

	// Closing it must clear BOTH. This is the assertion that fails on the buggy
	// version, where the layout kept the summary bubble.
	a.clearBubble(BubbleSummary)
	showing, advertised = a.bubbleState()
	if showing != nil {
		t.Errorf("clearBubble left the bubble showing: %+v", showing)
	}
	if advertised != nil {
		t.Errorf("clearBubble left the layout advertising the bubble: %+v", advertised)
	}

	// DismissBubble takes the same path.
	if _, err := a.showBubble(&BubbleView{Kind: BubblePick, Title: "pick"}); err != nil {
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

// TestSaveSummaryClearsBubble walks the user's reported flow: a finished focus
// session leaves a summary prompt, the user saves, and the prompt must go.
func TestSaveSummaryClearsBubble(t *testing.T) {
	dir := t.TempDir()
	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: s}

	// Archive a finished session the way the timer does, and leave its prompt up.
	rec, err := s.AddPomodoroRecord(store.PomodoroRecord{
		TodoID: "t1", TodoTitle: "写周报",
		StartedAt: time.Now().Add(-25 * time.Minute), EndedAt: time.Now(), FocusMinutes: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	st := s.Pomodoro()
	st.Enabled = true
	st.Phase = store.PhaseShortBreak
	st.RunState = store.RunRunning
	end := time.Now().Add(5 * time.Minute)
	st.EndsAt = &end
	st.PendingRecordID = rec.ID
	if err := s.SavePomodoro(st); err != nil {
		t.Fatal(err)
	}
	a.showBubble(&BubbleView{Kind: BubbleSummary, Title: "这一个番茄做了什么？", RecordID: rec.ID})

	// The user types a summary and saves.
	next, err := a.SavePomodoroSummary(rec.ID, "完成了数据部分")
	if err != nil {
		t.Fatalf("SavePomodoroSummary: %v", err)
	}

	// The prompt must be gone from BOTH the bubble field and the layout, or the
	// frontend redraws it and the click looks like it did nothing.
	showing, advertised := a.bubbleState()
	if showing != nil {
		t.Errorf("the summary prompt is still showing after saving: %+v", showing)
	}
	if advertised != nil {
		t.Errorf("the layout still advertises the summary prompt after saving: %+v", advertised)
	}

	// The edit itself must have landed, and the pending flag must be cleared.
	if next.Pomodoro.PendingSummary {
		t.Error("pendingSummary is still true after saving the summary")
	}
	recs := s.PomodoroRecords()
	if len(recs) != 1 || recs[0].Summary != "完成了数据部分" {
		t.Errorf("the summary was not stored: %+v", recs)
	}
	if stored := s.Pomodoro(); stored.PendingRecordID != "" {
		t.Errorf("pendingRecordId was not cleared: %q", stored.PendingRecordID)
	}
}

// TestSkippingSummaryAlsoClosesThePrompt: "跳过" takes the same route with an
// empty summary, and the record must survive it.
func TestSkippingSummaryAlsoClosesThePrompt(t *testing.T) {
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: s}

	rec, err := s.AddPomodoroRecord(store.PomodoroRecord{
		StartedAt: time.Now().Add(-time.Minute), EndedAt: time.Now(), FocusMinutes: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	st := s.Pomodoro()
	st.Enabled = true
	st.Phase = store.PhaseShortBreak
	st.PendingRecordID = rec.ID
	if err := s.SavePomodoro(st); err != nil {
		t.Fatal(err)
	}
	a.showBubble(&BubbleView{Kind: BubbleSummary, Title: "x", RecordID: rec.ID})

	if _, err := a.SavePomodoroSummary(rec.ID, ""); err != nil {
		t.Fatalf("skipping the summary failed: %v", err)
	}
	showing, advertised := a.bubbleState()
	if showing != nil || advertised != nil {
		t.Errorf("skipping did not close the prompt: showing=%+v advertised=%+v", showing, advertised)
	}
	// Skipping must NOT delete the record: the archive is permanent.
	if recs := s.PomodoroRecords(); len(recs) != 1 {
		t.Errorf("skipping the summary removed the record: %+v", recs)
	}
}

// TestSummaryBubbleSurvivesRestart is the "record is permanent" guarantee seen
// from the UI side: a prompt left unanswered before a restart must reappear,
// because the record and its pending flag are both persisted.
func TestSummaryBubbleSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.AddPomodoroRecord(store.PomodoroRecord{
		StartedAt: time.Now().Add(-time.Minute), EndedAt: time.Now(), FocusMinutes: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	st := s.Pomodoro()
	st.Enabled = true
	st.Phase = store.PhaseShortBreak
	st.RunState = store.RunRunning
	end := time.Now().Add(5 * time.Minute)
	st.EndsAt = &end
	st.PendingRecordID = rec.ID
	if err := s.SavePomodoro(st); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Pomodoro()
	if got.PendingRecordID != rec.ID {
		t.Errorf("the pending summary was lost across a restart: %q != %q", got.PendingRecordID, rec.ID)
	}
	if recs := reopened.PomodoroRecords(); len(recs) != 1 || recs[0].ID != rec.ID {
		t.Errorf("the archived session was lost across a restart: %+v", recs)
	}
}

// TestSaveSummaryUnknownRecordIsAnError guards the "edit, never delete" contract
// against a bad record id.
func TestSaveSummaryUnknownRecordIsAnError(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.SavePomodoroSummary("does-not-exist", "x"); err == nil {
		t.Error("saving a summary for a missing record should fail")
	}
}

// TestRecordFileHasNoDeletePath is a structural check: the store must expose no
// way to delete a record, which is the whole "permanent record" guarantee.
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
