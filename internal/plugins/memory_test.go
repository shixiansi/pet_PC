package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The memory document's rules: append-only, versioned, bounded.
//
// These are asserted on the store rather than through a plugin request because the
// rules ARE the store's: the capability in front of it only trims a string and reads
// the caller's directory.

func fixedNow(hour, minute int) time.Time {
	return time.Date(2026, 3, 4, hour, minute, 0, 0, time.Local)
}

func TestAppendMemoryCreatesTheFile(t *testing.T) {
	dir := t.TempDir()

	got, err := ReadMemory(dir)
	if err != nil {
		t.Fatalf("ReadMemory on an empty dir: %v", err)
	}
	// Empty, not an error: a fresh install with the permission and no notes is the
	// normal state, not a fault.
	if got != "" {
		t.Errorf("a plugin with no memory reads %q, want empty", got)
	}

	next, err := AppendMemory(dir, "用户在做 PetPC，一个效率桌宠", fixedNow(14, 32))
	if err != nil {
		t.Fatalf("AppendMemory: %v", err)
	}
	if !strings.Contains(next, "2026-03-04 14:32") {
		t.Errorf("the entry carries no timestamp: %q", next)
	}
	if !strings.Contains(next, "PetPC") {
		t.Errorf("the entry lost its text: %q", next)
	}

	back, err := ReadMemory(dir)
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	if back != next {
		t.Errorf("what was written is not what is read:\n wrote %q\n read  %q", next, back)
	}
}

// TestAppendMemorySnapshotsThePREVIOUSVersion pins which side of the write a snapshot
// belongs on. A version that already contains the mistake is not a version anyone
// wants.
func TestAppendMemorySnapshotsThePREVIOUSVersion(t *testing.T) {
	dir := t.TempDir()
	if _, err := AppendMemory(dir, "第一条", fixedNow(10, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendMemory(dir, "第二条", fixedNow(10, 1)); err != nil {
		t.Fatal(err)
	}

	history := filepath.Join(dir, memoryHistoryDir)
	entries, err := os.ReadDir(history)
	if err != nil {
		t.Fatalf("no history directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("history holds %d versions, want exactly 1 (only the first append creates one)", len(entries))
	}
	body, err := os.ReadFile(filepath.Join(history, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "第二条") {
		t.Errorf("the snapshot contains the entry that came after it: %q", body)
	}
	if !strings.Contains(string(body), "第一条") {
		t.Errorf("the snapshot lost the previous version: %q", body)
	}
}

func TestAppendMemoryRejectsBlankEntries(t *testing.T) {
	dir := t.TempDir()
	for _, blank := range []string{"", "   ", "\n\t"} {
		if _, err := AppendMemory(dir, blank, fixedNow(10, 0)); err == nil {
			t.Errorf("%q was accepted as an entry", blank)
		}
	}
}

// TestAppendMemoryCollapsesAnEntryToOneLine keeps the file's shape: one bullet per
// fact. It also stops an entry from forging the "date  text" header of a later line -
// a model writing "x\n- 2026-01-01  earlier" would otherwise look like two entries,
// one of them backdated.
func TestAppendMemoryCollapsesAnEntryToOneLine(t *testing.T) {
	dir := t.TempDir()
	next, err := AppendMemory(dir, "第一行\n- 2026-01-01  伪造的一条", fixedNow(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(next, "\n"); got != 1 {
		t.Errorf("the document has %d newlines, want 1: %q", got, next)
	}
}

func TestAppendMemoryTruncatesOneLongEntry(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("很", maxMemoryAppendRunes+100)
	next, err := AppendMemory(dir, long, fixedNow(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if runes := []rune(next); len(runes) > maxMemoryAppendRunes+40 {
		t.Errorf("the entry was not truncated: %d runes", len(runes))
	}
	if !strings.HasSuffix(strings.TrimSpace(next), "…") {
		t.Errorf("a truncated entry is not marked as truncated: %q", next)
	}
}

// TestAppendMemoryRefusesWhenFull pins that the bound is a REFUSAL, not a silent
// overwrite of the oldest entry. Losing what the user taught the pet without saying so
// would be the worst possible behaviour here.
func TestAppendMemoryRefusesWhenFull(t *testing.T) {
	dir := t.TempDir()
	chunk := strings.Repeat("记", 400)
	var err error
	for i := 0; i < 100; i++ {
		if _, err = AppendMemory(dir, chunk, fixedNow(10, i%60)); err != nil {
			break
		}
	}
	if err == nil {
		t.Fatal("the document accepted entries past its cap")
	}
	if !strings.Contains(err.Error(), "记忆已满") {
		t.Errorf("the error does not explain the cap: %v", err)
	}
	body, readErr := ReadMemory(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	// Nothing was lost by the refused append.
	if len(body) > maxMemoryBytes {
		t.Errorf("the document grew past the cap anyway: %d bytes", len(body))
	}
}

func TestMemoryHistoryIsPruned(t *testing.T) {
	dir := t.TempDir()
	// One more than the cap, one second apart so filenames are distinct.
	for i := 0; i <= maxMemoryHistory; i++ {
		at := time.Date(2026, 3, 4, 10, 0, i, 0, time.Local)
		if _, err := AppendMemory(dir, "记一笔", at); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, memoryHistoryDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > maxMemoryHistory {
		t.Errorf("history holds %d versions, want at most %d", len(entries), maxMemoryHistory)
	}
	// The NEWEST versions survive: the oldest filename is gone.
	oldest := "2026-03-04T10-00-00.md"
	if _, err := os.Stat(filepath.Join(dir, memoryHistoryDir, oldest)); err == nil {
		t.Errorf("the oldest version %s survived pruning", oldest)
	}
}

// TestMemorySnapshotNamesAreUniqueWithinASecond covers two tool calls in one round,
// which is exactly what a model does when it decides to record two facts at once.
func TestMemorySnapshotNamesAreUniqueWithinASecond(t *testing.T) {
	dir := t.TempDir()
	at := fixedNow(10, 0)
	if _, err := AppendMemory(dir, "第一条", at); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendMemory(dir, "第二条", at); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendMemory(dir, "第三条", at); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, memoryHistoryDir))
	if err != nil {
		t.Fatal(err)
	}
	// Two snapshots exist by now (the third append's snapshot included), and none was
	// overwritten by another at the same second.
	if len(entries) != 2 {
		t.Errorf("history holds %d versions, want 2: %v", len(entries), entries)
	}
}
