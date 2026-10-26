package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"PetPC/internal/store"
)

// --- helpers ---------------------------------------------------------------

// planPendingApp builds an App whose store is parked on the plan question: the
// state a break that has just run out leaves behind (docs/reminder-pomodoro.md
// 4.5.2). The timer is stopped, so nothing advances on its own.
//
// It reuses newLLMTestApp (app_llm_test.go) rather than newTestApp so the event
// collector is wired up.
func planPendingApp(t *testing.T, nextTodoID, nextTodoTitle string) *App {
	t.Helper()
	a := newLLMTestApp(t)
	st := a.store.Pomodoro()
	st.Enabled = true
	st.Phase = store.PhaseFocus
	st.RunState = store.RunStopped
	st.PlanPending = true
	st.NextTodoID = nextTodoID
	st.NextTodoTitle = nextTodoTitle
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	return a
}

// planPendingSurfaceApp is planPendingApp over an app that also has a plugin
// declaring a floating surface, which is what the slot-contention tests need:
// the thing that may not eat the parked question is now that surface.
func planPendingSurfaceApp(t *testing.T) (*App, string) {
	t.Helper()
	a, id := surfaceApp(t)
	st := a.store.Pomodoro()
	st.Enabled = true
	st.Phase = store.PhaseFocus
	st.RunState = store.RunStopped
	st.PlanPending = true
	st.NextTodoID = "t1"
	st.NextTodoTitle = "写周报"
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	return a, id
}

// addRecord archives one finished session ending at endedAt.
func addRecord(t *testing.T, a *App, endedAt time.Time, plan string) store.PomodoroRecord {
	t.Helper()
	rec, err := a.store.AddPomodoroRecord(store.PomodoroRecord{
		StartedAt: endedAt.Add(-25 * time.Minute), EndedAt: endedAt, FocusMinutes: 25, Plan: plan,
	})
	if err != nil {
		t.Fatalf("AddPomodoroRecord: %v", err)
	}
	return rec
}

// --- T7a: the constant and its fixed size ----------------------------------

// TestBubblePlanHeightMapping pins the new fixed height without disturbing the
// three that were already there.
//
// The sizes are fixed rather than content-driven because the window region is
// cut from them: too large leaves glass-composited pixels behind, too small
// clips the card (docs/reminder-pomodoro.md 8.1).
func TestBubblePlanHeightMapping(t *testing.T) {
	cases := []struct {
		kind string
		want int
	}{
		{BubblePlan, 180},
		{BubblePick, 200},
		{BubbleSummary, 160},
		{BubbleReminder, 140},
	}
	for _, tc := range cases {
		if got := bubbleLogicalHeight(tc.kind); got != tc.want {
			t.Errorf("bubbleLogicalHeight(%q) = %d, want %d", tc.kind, got, tc.want)
		}
	}
	if BubblePlan != "plan" {
		t.Errorf("BubblePlan = %q, want the frozen literal %q", BubblePlan, "plan")
	}
}

// --- T7b: cleanup list and the Plan field ----------------------------------

// TestPlanBubbleIsClearedWithAllBubbles pins that a plan bubble is one of the
// bubbles clearAllBubblesLocked takes down. Starting a focus phase takes the
// slot from every bubble; a kind missing from the list would survive it and
// leave the frontend rendering a bubble the layout no longer describes.
func TestPlanBubbleIsClearedWithAllBubbles(t *testing.T) {
	a := newLLMTestApp(t)

	if _, err := a.showBubble(&BubbleView{Kind: BubblePlan, Title: "接下来想专注做什么？"}); err != nil {
		// No window: the region re-crop fails, which these tests tolerate - the
		// bubble/layout invariant is what is asserted.
		t.Logf("showBubble (no window): %v", err)
	}
	showing, advertised := a.bubbleState()
	if showing == nil || advertised == nil {
		t.Fatalf("showBubble did not register the plan bubble: showing=%+v advertised=%+v", showing, advertised)
	}

	a.clearAllBubblesLocked()

	showing, advertised = a.bubbleState()
	if showing != nil {
		t.Errorf("clearAllBubblesLocked left the plan bubble showing: %+v", showing)
	}
	if advertised != nil {
		t.Errorf("clearAllBubblesLocked left the layout advertising the plan bubble: %+v", advertised)
	}
}

// TestBubbleViewPlanJSONTag pins the wire name and its omitempty behaviour: an
// empty plan must NOT travel as a key (the frontend distinguishes "no plan to
// show" from "an empty plan line"), while a real one must.
func TestBubbleViewPlanJSONTag(t *testing.T) {
	empty, err := json.Marshal(BubbleView{Kind: BubblePlan, Title: "t"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	// The key itself, not the substring: the kind is also "plan", and
	// {"kind":"plan"} must not be mistaken for the field.
	if strings.Contains(string(empty), `"plan":`) {
		t.Errorf("an empty plan still serialises a key: %s", empty)
	}

	full, err := json.Marshal(BubbleView{Kind: BubblePlan, Title: "t", Plan: "整理接口"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(full), `"plan":"整理接口"`) {
		t.Errorf("the plan is not on the wire under its frozen name: %s", full)
	}
}

// --- T7c: showPlanBubble ---------------------------------------------------

// TestShowPlanBubbleNeedsPlanPending is the precondition: with nothing waiting
// there is no question to ask. Showing one anyway would put a bubble on screen
// that answering cannot release, and would disturb a healthy state to do it.
func TestShowPlanBubbleNeedsPlanPending(t *testing.T) {
	a := newLLMTestApp(t)
	before := a.store.Pomodoro()

	a.showPlanBubble()

	showing, advertised := a.bubbleState()
	if showing != nil {
		t.Errorf("showPlanBubble raised a bubble with nothing pending: %+v", showing)
	}
	if advertised != nil {
		t.Errorf("the layout advertises a bubble with nothing pending: %+v", advertised)
	}
	if after := a.store.Pomodoro(); after != before {
		t.Errorf("showPlanBubble changed the state it should have left alone: %+v != %+v", after, before)
	}
}

// TestShowPlanBubbleAssemblesContent pins the three fields the frontend draws:
// the fixed title, the carried todo, and the previous plan as the input box's
// initial value.
//
// The last one is the answer to "what should the box start with after a silent
// path (full screen, catch-up) wrote no plan": the most recent record is the
// best guess at what the user is still working on, and an empty box would make
// them retype it.
func TestShowPlanBubbleAssemblesContent(t *testing.T) {
	a := planPendingApp(t, "t1", "写周报")

	now := time.Now()
	addRecord(t, a, now.Add(-2*time.Hour), "更早的计划")
	addRecord(t, a, now.Add(-time.Hour), "整理接口")

	a.showPlanBubble()

	showing, advertised := a.bubbleState()
	if showing == nil {
		t.Fatal("showPlanBubble raised nothing while the plan question was waiting")
	}
	if advertised == nil || advertised.Kind != BubblePlan {
		t.Fatalf("the layout does not advertise the plan bubble: %+v", advertised)
	}
	if showing.Kind != BubblePlan {
		t.Errorf("kind = %q, want %q", showing.Kind, BubblePlan)
	}
	if showing.Title != "接下来想专注做什么？" {
		t.Errorf("title = %q, want %q", showing.Title, "接下来想专注做什么？")
	}
	if showing.TodoTitle != "写周报" {
		t.Errorf("todoTitle = %q, want the carried todo %q", showing.TodoTitle, "写周报")
	}
	if showing.Plan != "整理接口" {
		t.Errorf("plan = %q, want the most recent record's plan %q", showing.Plan, "整理接口")
	}
}

// TestShowPlanBubbleWithNoRecordsStartsWithAnEmptyBox: a first-ever session has
// no previous plan to offer, and the box must simply be empty rather than the
// bubble failing to appear.
func TestShowPlanBubbleWithNoRecordsStartsWithAnEmptyBox(t *testing.T) {
	a := planPendingApp(t, "", "")

	a.showPlanBubble()

	showing, _ := a.bubbleState()
	if showing == nil {
		t.Fatal("showPlanBubble raised nothing for a first-ever session")
	}
	if showing.Plan != "" {
		t.Errorf("plan = %q, want empty with no records", showing.Plan)
	}
	if showing.TodoTitle != "" {
		t.Errorf("todoTitle = %q, want empty with no carried todo", showing.TodoTitle)
	}
}

// --- T9: the plan bubble cannot be waved away ------------------------------

// TestPendingPlanWaitingFollowsPlanPending pins the accessor the guard is built
// on, including its self-acquired lock: it is the reverse of the a.mu -> pomoMu
// order the scheduler uses, and DismissBubble depends on reading it first
// (design.md 2.2).
func TestPendingPlanWaitingFollowsPlanPending(t *testing.T) {
	a := newLLMTestApp(t)
	if a.pendingPlanWaiting() {
		t.Error("a fresh app reports a plan question waiting")
	}

	st := a.store.Pomodoro()
	st.Phase = store.PhaseFocus
	st.PlanPending = true
	if err := a.store.SavePomodoro(st); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	if !a.pendingPlanWaiting() {
		t.Error("pendingPlanWaiting missed a parked plan question")
	}
}

// TestPlanBubbleCannotBeDismissed is the property that makes the gate real: the
// focus session does not start until the question is answered, so a prompt that
// Escape, a drag or a stray click could close would leave a stopped timer with
// nothing on screen explaining why.
func TestPlanBubbleCannotBeDismissed(t *testing.T) {
	a := planPendingApp(t, "t1", "写周报")
	a.showPlanBubble()
	before := a.store.Pomodoro()

	// The refusal finishes by re-reading the layout, which needs a real window;
	// the guard runs before that, so the STATE is the assertion.
	_, _ = a.DismissBubble()

	after := a.store.Pomodoro()
	if !after.PlanPending {
		t.Error("the dismissal answered the plan question, so the gate can be bypassed")
	}
	if after.NextTodoID != before.NextTodoID || after.NextTodoTitle != before.NextTodoTitle {
		t.Errorf("the dismissal consumed the carried todo: %+v != %+v", after, before)
	}
	if after.RunState != store.RunStopped {
		t.Errorf("runState = %q, want the parked %q", after.RunState, store.RunStopped)
	}
	showing, advertised := a.bubbleState()
	if showing == nil || showing.Kind != BubblePlan {
		t.Fatalf("the question disappeared without being answered: %+v", showing)
	}
	if advertised == nil || advertised.Kind != BubblePlan {
		t.Errorf("the layout no longer advertises the question: %+v", advertised)
	}
}

// TestLingeringPlanBubbleIsStillDismissable is the other half: the refusal is
// tied to the PENDING question, not to the bubble kind. Once the question has
// been answered (or the session given up), a plan bubble left on screen is just
// a bubble and must close as before.
func TestLingeringPlanBubbleIsStillDismissable(t *testing.T) {
	a := planPendingApp(t, "t1", "写周报")
	a.showPlanBubble()

	// Answered by another path: PlanPending is cleared but the bubble lingers.
	a.pomoMu.Lock()
	st := a.store.Pomodoro()
	st.PlanPending = false
	err := a.store.SavePomodoro(st)
	a.pomoMu.Unlock()
	if err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}

	_, _ = a.DismissBubble()

	showing, advertised := a.bubbleState()
	if showing != nil {
		t.Errorf("a plan bubble with nothing pending could not be dismissed: %+v", showing)
	}
	if advertised != nil {
		t.Errorf("the layout still advertises the dismissed bubble: %+v", advertised)
	}
}

// --- T10: the plan bubble takes a question's slot --------------------------

// TestPlanBubbleCountsAsAnUnansweredQuestion pins the mapping every surface that
// is refused the slot depends on. Leaving BubblePlan out would let the chat
// popup - which TAKES the slot rather than sharing it - destroy a question the
// timer is parked on.
func TestPlanBubbleCountsAsAnUnansweredQuestion(t *testing.T) {
	a := newLLMTestApp(t)

	a.setBubbleLocked(&BubbleView{Kind: BubblePlan, Title: "接下来想专注做什么？"})

	if got := a.unansweredQuestionKind(); got != BubblePlan {
		t.Errorf("unansweredQuestionKind = %q, want %q", got, BubblePlan)
	}
	if !a.bubbleAwaitingAnswer() {
		t.Error("the plan bubble is not treated as a question")
	}
}

// TestQuestionOwnsTheSlotHasThreeDistinctReasons: "select a todo", "write the
// summary" and "write the plan" are not the same sentence, and all three have to
// explain themselves because each names a different consequence.
func TestQuestionOwnsTheSlotHasThreeDistinctReasons(t *testing.T) {
	msgs := map[string]string{
		BubblePick:    questionOwnsTheSlot(BubblePick),
		BubbleSummary: questionOwnsTheSlot(BubbleSummary),
		BubblePlan:    questionOwnsTheSlot(BubblePlan),
	}
	for kind, msg := range msgs {
		if msg == "" {
			t.Errorf("%s has no explanation for the refusal", kind)
		}
	}
	if msgs[BubblePick] == msgs[BubbleSummary] {
		t.Error("the picker and the summary share one refusal message")
	}
	if msgs[BubblePlan] == msgs[BubblePick] {
		t.Error("the plan question and the picker share one refusal message")
	}
	if msgs[BubblePlan] == msgs[BubbleSummary] {
		t.Error("the plan question and the summary share one refusal message")
	}
}

// TestChatPopupCannotEatThePlanBubble is the direction the user's report was
// about: a double click on the pet must not delete the parked question. The
// refusal has to name the plan question and leave it exactly where it was.
//
// The caller is now a plugin's floating surface rather than the pet's own
// overlay: the slot and the rule are the same, only the thing asking for it
// changed.
func TestChatPopupCannotEatThePlanBubble(t *testing.T) {
	a, id := planPendingSurfaceApp(t)
	a.showPlanBubble()

	_, err := a.OpenPluginSurface(id)
	if err == nil {
		t.Fatal("the chat popup opened over the plan question and destroyed it")
	}
	if err.Error() != pluginSurfaceBlocked(BubblePlan) {
		t.Errorf("the refusal does not explain itself: %v", err)
	}
	a.mu.Lock()
	surface := a.pluginSurface
	a.mu.Unlock()
	if surface != nil {
		t.Error("the surface is showing over a question that is waiting for an answer")
	}
	showing, advertised := a.bubbleState()
	if showing == nil || showing.Kind != BubblePlan {
		t.Fatalf("the refusal took the question with it: %+v", showing)
	}
	if advertised == nil || advertised.Kind != BubblePlan {
		t.Errorf("the refusal disturbed the layout: %+v", advertised)
	}
	// And the timer is still parked, so nothing was answered behind the user's back.
	if got := a.store.Pomodoro(); !got.PlanPending || got.RunState != store.RunStopped {
		t.Errorf("the refusal disturbed the parked phase: %+v", got)
	}
}

// TestChatPopupStillTakesTheSlotFromAReminder is the other half of the T10
// change: counting the plan bubble as a question must not make the surface defer
// to everything. A reminder asks nothing, so it is still replaced.
func TestChatPopupStillTakesTheSlotFromAReminder(t *testing.T) {
	a, id := surfaceApp(t)

	a.setBubbleLocked(&BubbleView{Kind: BubbleReminder, Title: "今天要做的事"})
	// The geometry call needs a real window and fails here, which is why the
	// assertion is the STATE: a run that reached the re-crop has passed the guard.
	_, _ = a.OpenPluginSurface(id)

	a.mu.Lock()
	surface := a.pluginSurface
	a.mu.Unlock()
	if surface == nil {
		t.Error("the surface did not open, so a notification can block the conversation")
	}
	showing, _ := a.bubbleState()
	if showing != nil {
		t.Errorf("the reminder survived the surface opening: %+v", showing)
	}
}

// TestUnansweredQuestionKindStillNamesTheOldBubbles guards the extension against
// a regression in the two kinds that already worked.
func TestUnansweredQuestionKindStillNamesTheOldBubbles(t *testing.T) {
	a := newLLMTestApp(t)

	for _, kind := range []string{BubblePick, BubbleSummary, BubblePlan} {
		a.setBubbleLocked(&BubbleView{Kind: kind})
		if got := a.unansweredQuestionKind(); got != kind {
			t.Errorf("unansweredQuestionKind = %q, want %q", got, kind)
		}
		if !a.bubbleAwaitingAnswer() {
			t.Errorf("%s is not treated as a question", kind)
		}
	}
	for _, kind := range []string{BubbleReminder, BubbleNone} {
		var b *BubbleView
		if kind != BubbleNone {
			b = &BubbleView{Kind: kind}
		}
		a.setBubbleLocked(b)
		if got := a.unansweredQuestionKind(); got != BubbleNone {
			t.Errorf("%s reported as a question: %q", kind, got)
		}
	}
}
