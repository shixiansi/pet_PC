package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The plan gate and the plan carried to the next focus session.
//
// These two fields exist only so the app layer can ask "what is the next focus
// session about?" and remember the todo in the meantime. They are runtime
// bookkeeping, so they must not appear in the state file when they are unset:
// pomodoro.json is a file users are invited to read, and three extra zero-valued
// keys in every idle state would make it look like something is pending when
// nothing is.

// TestPlanGateFieldsAreAbsentWhenUnset is the omitempty contract on PomodoroState.
func TestPlanGateFieldsAreAbsentWhenUnset(t *testing.T) {
	raw, err := json.Marshal(PomodoroState{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range []string{"planPending", "nextTodoId", "nextTodoTitle"} {
		if strings.Contains(string(raw), `"`+key+`"`) {
			t.Errorf("an unset %s still appears in the state JSON: %s", key, raw)
		}
	}

	// A round trip of the zero value keeps all three at their zero values.
	var back PomodoroState
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.PlanPending || back.NextTodoID != "" || back.NextTodoTitle != "" {
		t.Errorf("zero value did not round-trip: %+v", back)
	}

	// When they ARE set, the exact keys the contract froze are the ones written.
	set := PomodoroState{PlanPending: true, NextTodoID: "t1", NextTodoTitle: "写周报"}
	raw, err = json.Marshal(set)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var kv map[string]any
	if err := json.Unmarshal(raw, &kv); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if kv["planPending"] != true {
		t.Errorf("planPending = %v, want true (json %s)", kv["planPending"], raw)
	}
	if kv["nextTodoId"] != "t1" || kv["nextTodoTitle"] != "写周报" {
		t.Errorf("carried todo not written under its frozen key: %s", raw)
	}
}

// TestPlanGateFieldsStayOutOfThePersistedState covers the same contract at the
// store boundary rather than at the encoder: a running focus written by an
// ordinary start must not pick up plan keys on disk.
func TestPlanGateFieldsStayOutOfThePersistedState(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	end := time.Now().Add(10 * time.Minute).Truncate(time.Second)
	if err := s.SavePomodoro(PomodoroState{
		Enabled: true, Phase: PhaseFocus, RunState: RunRunning, EndsAt: &end,
	}); err != nil {
		t.Fatalf("SavePomodoro: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "pomodoro.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"planPending", "nextTodoId", "nextTodoTitle"} {
		if strings.Contains(string(raw), `"`+key+`"`) {
			t.Errorf("an unset %s reached the state file: %s", key, raw)
		}
	}
}

// TestNormaliseStateClearsThePlanGateWhenIdle closes the one hole the plan gate
// could otherwise leave open: an idle timer that is still "waiting for a plan"
// would stop the pet from ever asking again (and from starting anything).
func TestNormaliseStateClearsThePlanGateWhenIdle(t *testing.T) {
	got := NormaliseState(PomodoroState{
		Phase:         PhaseIdle,
		PlanPending:   true,
		NextTodoID:    "x",
		NextTodoTitle: "y",
	})
	if got.PlanPending {
		t.Errorf("an idle state stayed plan-pending: %+v", got)
	}
	if got.NextTodoID != "" || got.NextTodoTitle != "" {
		t.Errorf("idle state kept a carried todo: %+v", got)
	}

	// The clearing is specific to idle: a stopped focus phase legitimately waits
	// for the plan, and that must survive the normaliser or the gate would be
	// answered by every restart.
	kept := NormaliseState(PomodoroState{
		Phase:         PhaseFocus,
		RunState:      RunStopped,
		PlanPending:   true,
		NextTodoID:    "x",
		NextTodoTitle: "y",
	})
	if !kept.PlanPending || kept.NextTodoID != "x" || kept.NextTodoTitle != "y" {
		t.Errorf("a waiting focus phase lost its gate: %+v", kept)
	}
}

// legacyRecordsJSON is a pomodoro-records.json written before this feature
// existed: it has no plan and no planEditedAt key anywhere.
const legacyRecordsJSON = `[
  {
    "id": "r1",
    "todoId": "t1",
    "todoTitle": "写周报",
    "startedAt": "2026-03-04T10:00:00+08:00",
    "endedAt": "2026-03-04T10:25:00+08:00",
    "focusMinutes": 25,
    "summary": "写完了数据部分",
    "editedAt": "2026-03-04T11:00:00+08:00"
  },
  {
    "id": "r2",
    "startedAt": "2026-03-04T08:00:00+08:00",
    "endedAt": "2026-03-04T08:25:00+08:00",
    "focusMinutes": 25,
    "summary": ""
  }
]`

// TestLegacyRecordsWithoutPlanReadBackEmpty is the compatibility promise: there
// is no migration script (spec card out_of_scope 4), so an old archive must read
// back with a zero plan rather than as an error or a shrunken list.
func TestLegacyRecordsWithoutPlanReadBackEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pomodoro-records.json"), []byte(legacyRecordsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if w := s.Warnings(); len(w) != 0 {
		t.Errorf("a legacy archive must not be quarantined or warned about: %v", w)
	}

	recs := s.PomodoroRecords()
	if len(recs) != 2 {
		t.Fatalf("got %d records, want the 2 in the file", len(recs))
	}
	for _, rec := range recs {
		if rec.Plan != "" {
			t.Errorf("record %q came back with a plan it never had: %q", rec.ID, rec.Plan)
		}
		if rec.PlanEditedAt != nil {
			t.Errorf("record %q came back with a planEditedAt it never had: %v", rec.ID, rec.PlanEditedAt)
		}
	}
	// The fields that were in the file are still intact.
	if recs[0].ID != "r1" || recs[0].Summary != "写完了数据部分" || recs[0].TodoTitle != "写周报" {
		t.Errorf("the legacy record lost data: %+v", recs[0])
	}
}

// TestEditingALegacyRecordAddsThePlanFields is the upgrade path in one step: the
// record that had no plan keys gains them on the first edit, the record count is
// unchanged, and the new keys appear in the file.
func TestEditingALegacyRecordAddsThePlanFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pomodoro-records.json"), []byte(legacyRecordsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	updated, err := s.SetPomodoroPlanAndSummary("r1", "", "改后的实际")
	if err != nil {
		t.Fatalf("SetPomodoroPlanAndSummary: %v", err)
	}
	if updated.Plan != "" {
		t.Errorf("plan = %q, want an empty plan", updated.Plan)
	}
	if updated.PlanEditedAt == nil {
		t.Error("PlanEditedAt was not stamped on a legacy record")
	}
	if updated.EditedAt == nil {
		t.Error("EditedAt was not stamped on a legacy record")
	}

	raw, err := os.ReadFile(filepath.Join(dir, "pomodoro-records.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"planEditedAt"`) {
		t.Errorf("the edit did not reach the file: %s", raw)
	}
	if recs := s.PomodoroRecords(); len(recs) != 2 {
		t.Errorf("editing changed the record count: %d", len(recs))
	}
}

// TestRecordPlanFieldsUseTheFrozenJSONTags pins the on-disk shape of the two new
// record fields, on the same principle as the state fields above: unset means
// absent, and set means exactly the contract's key.
func TestRecordPlanFieldsUseTheFrozenJSONTags(t *testing.T) {
	raw, err := json.Marshal(PomodoroRecord{ID: "r1"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range []string{"plan", "planEditedAt"} {
		if strings.Contains(string(raw), `"`+key+`"`) {
			t.Errorf("an unset %s still appears in the record JSON: %s", key, raw)
		}
	}

	when := time.Date(2026, 3, 4, 11, 0, 0, 0, time.Local)
	raw, err = json.Marshal(PomodoroRecord{ID: "r1", Plan: "整理接口", PlanEditedAt: &when})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back PomodoroRecord
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.Plan != "整理接口" {
		t.Errorf("plan round-trip = %q, want 整理接口 (json %s)", back.Plan, raw)
	}
	if back.PlanEditedAt == nil || !back.PlanEditedAt.Equal(when) {
		t.Errorf("planEditedAt round-trip = %v, want %v", back.PlanEditedAt, when)
	}
}

// TestSetPomodoroPlanAndSummaryWritesBothFields is the record page's one write
// path: the plan and the summary are edited together, and both timestamps move
// even when the new plan is empty (clearing a plan deliberately is a real edit,
// not a no-op).
func TestSetPomodoroPlanAndSummaryWritesBothFields(t *testing.T) {
	s := newTestStore(t)
	start := time.Now().Add(-25 * time.Minute).Truncate(time.Second)
	rec, err := s.AddPomodoroRecord(PomodoroRecord{
		TodoID: "t1", TodoTitle: "写周报",
		StartedAt: start, EndedAt: time.Now().Truncate(time.Second), FocusMinutes: 25,
		Summary: "原来的实际",
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := s.SetPomodoroPlanAndSummary(rec.ID, "", "改后的实际")
	if err != nil {
		t.Fatalf("SetPomodoroPlanAndSummary: %v", err)
	}
	if updated.Plan != "" {
		t.Errorf("plan = %q, want the empty plan that was written", updated.Plan)
	}
	if updated.Summary != "改后的实际" {
		t.Errorf("summary = %q, want 改后的实际", updated.Summary)
	}
	if updated.PlanEditedAt == nil {
		t.Error("an empty plan is still an edit, so PlanEditedAt must be stamped")
	}
	if updated.EditedAt == nil {
		t.Error("EditedAt was not stamped")
	}

	// Both edits must be on disk, not merely in the returned copy: a reopen is
	// what the record page actually sees.
	reopened, err := New(s.dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	recs := reopened.PomodoroRecords()
	if len(recs) != 1 {
		t.Fatalf("got %d records after reopen, want 1", len(recs))
	}
	if recs[0].Plan != "" || recs[0].Summary != "改后的实际" || recs[0].PlanEditedAt == nil {
		t.Errorf("the combined edit did not persist: %+v", recs[0])
	}

	// Setting a real plan works the same way, and the record count never moves.
	updated, err = s.SetPomodoroPlanAndSummary(rec.ID, "整理接口", "改后的实际")
	if err != nil {
		t.Fatalf("SetPomodoroPlanAndSummary: %v", err)
	}
	if updated.Plan != "整理接口" || updated.PlanEditedAt == nil {
		t.Errorf("plan edit not applied: %+v", updated)
	}
	if recs := s.PomodoroRecords(); len(recs) != 1 {
		t.Errorf("editing changed the record count: %d", len(recs))
	}
}

// TestSetPomodoroPlanAndSummaryRejectsAnUnknownID keeps the new write path honest
// about a missing record instead of silently succeeding.
func TestSetPomodoroPlanAndSummaryRejectsAnUnknownID(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SetPomodoroPlanAndSummary("不存在", "a", "b"); err == nil {
		t.Fatal("editing a missing record should fail")
	} else if err.Error() != "未找到该记录" {
		t.Errorf("error = %q, want the store's existing wording", err.Error())
	}
}

// TestSetPomodoroSummaryKeepsItsSignature is the do-not-break half: the summary
// bubble still calls SetPomodoroSummary, and it must keep touching only the
// summary.
func TestSetPomodoroSummaryKeepsItsSignature(t *testing.T) {
	s := newTestStore(t)
	rec, err := s.AddPomodoroRecord(PomodoroRecord{
		StartedAt: time.Now().Add(-time.Minute), EndedAt: time.Now(), FocusMinutes: 25,
		Plan: "整理接口",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.SetPomodoroSummary(rec.ID, "只改实际")
	if err != nil {
		t.Fatalf("SetPomodoroSummary: %v", err)
	}
	if updated.Plan != "整理接口" {
		t.Errorf("the summary path touched the plan: %q", updated.Plan)
	}
	if updated.Summary != "只改实际" || updated.EditedAt == nil {
		t.Errorf("the summary path did not do its own job: %+v", updated)
	}
	if updated.PlanEditedAt != nil {
		t.Errorf("the summary path stamped PlanEditedAt: %v", updated.PlanEditedAt)
	}
}

// TestNoRecordDeletePathExists is the negative half of "records are permanent":
// the spec card forbids any delete or clear entry point, so the exported method
// set itself is asserted rather than only the call sites.
func TestNoRecordDeletePathExists(t *testing.T) {
	forbidden := map[string]bool{
		"DeletePomodoroRecord": true,
		"ClearPomodoroRecords": true,
		"DeleteRecord":         true,
	}
	destructive := []string{"delete", "clear", "remove", "purge", "erase"}
	typ := reflect.TypeOf(&Store{})
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		if forbidden[name] {
			t.Errorf("the store grew a record delete path: %s", name)
			continue
		}
		lower := strings.ToLower(name)
		if !strings.Contains(lower, "record") {
			continue
		}
		for _, verb := range destructive {
			if strings.Contains(lower, verb) {
				t.Errorf("the store grew a destructive record method: %s", name)
			}
		}
	}
}

// TestPlanEditIsVisibleInTheTotalsAlongsideTheSummary is a small guard that the
// new write path is a record edit and nothing more: it must not disturb the
// counts the records page footer is built from.
func TestPlanEditIsVisibleInTheTotalsAlongsideTheSummary(t *testing.T) {
	s := newTestStore(t)
	var ids []string
	for i := 0; i < 2; i++ {
		rec, err := s.AddPomodoroRecord(PomodoroRecord{
			StartedAt:    time.Now().Add(-time.Duration(i+1) * time.Hour),
			EndedAt:      time.Now().Add(-time.Duration(i) * time.Minute),
			FocusMinutes: 25,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rec.ID)
	}
	if _, err := s.SetPomodoroPlanAndSummary(ids[0], fmt.Sprintf("计划 %d", 1), "实际"); err != nil {
		t.Fatalf("SetPomodoroPlanAndSummary: %v", err)
	}
	count, minutes := s.PomodoroTotals()
	if count != 2 || minutes != 50 {
		t.Errorf("totals = (%d,%d), want (2,50) - editing must not add or drop records", count, minutes)
	}
}
