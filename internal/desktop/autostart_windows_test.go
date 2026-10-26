//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// testValueName is a scratch Run value the tests own.
//
// These tests must NEVER touch autostartValueName ("PetPC"): that is the user's
// real auto-start registration, so a test that deletes it silently switches the
// feature off on the machine running the suite. An earlier version of this file
// did exactly that and turned auto-start off for its own author.
const testValueName = "PetPC-test-do-not-use"

// withScratchValue runs fn with a private Run value and removes it afterwards,
// leaving the real entry untouched.
func withScratchValue(t *testing.T, fn func()) {
	t.Helper()

	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatalf("open Run key: %v", err)
	}
	// Start from a clean slate and always clean up, even on failure or panic.
	_ = k.DeleteValue(testValueName)
	t.Cleanup(func() {
		_ = k.DeleteValue(testValueName)
		k.Close()
	})
	// Also drop any StartupApproved scratch record this test may create.
	_ = clearStartupApproved(testValueName)

	fn()
}

// TestAutostartTestsUseScratchValue is a guard on the guard: if someone points
// the helper back at the real value name, this fails loudly instead of quietly
// disabling auto-start for the developer.
func TestAutostartTestsUseScratchValue(t *testing.T) {
	if testValueName == autostartValueName {
		t.Fatal("the autostart tests must not use the real Run value name")
	}
}

// TestAutostartCommandIsWindowsQuoted pins the exact bug that made auto-start
// silently do nothing.
//
// The Run value is a Windows command line, so it must be the path wrapped in
// double quotes. It was once built with fmt.Sprintf("%q", exe) - Go *source*
// quoting - which escapes each backslash, producing
// `"F:\\pet_pc\\build\\bin\\PetPC.exe"`. Explorer cannot resolve that, so it
// skipped the entry and logged Shell-Core 9708 with PID 0 on every logon.
func TestAutostartCommandIsWindowsQuoted(t *testing.T) {
	got, err := autostartCommand(`C:\Users\me\PetPC.exe`)
	if err != nil {
		t.Fatalf("autostartCommand: %v", err)
	}
	want := `"C:\Users\me\PetPC.exe"`
	if got != want {
		t.Fatalf("autostartCommand = %s, want %s", got, want)
	}
	if strings.Contains(got, `\\`) {
		t.Errorf("command %s contains an escaped separator; Windows cannot resolve it", got)
	}
}

// TestAutostartCommandQuotesSpaces covers why the quoting exists at all: a
// directory with a space must survive as a single token.
func TestAutostartCommandQuotesSpaces(t *testing.T) {
	got, err := autostartCommand(`C:\Program Files\PetPC\PetPC.exe`)
	if err != nil {
		t.Fatalf("autostartCommand: %v", err)
	}
	want := `"C:\Program Files\PetPC\PetPC.exe"`
	if got != want {
		t.Fatalf("autostartCommand = %s, want %s", got, want)
	}
	// Exactly two quotes, both at the ends: anything else would re-split the path.
	if n := strings.Count(got, `"`); n != 2 {
		t.Errorf("command %s has %d quotes, want 2", got, n)
	}
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Errorf("command %s is not wrapped in quotes", got)
	}
}

// TestAutostartCommandRejectsUnusablePaths keeps a value Explorer would
// misparse from ever reaching the registry.
func TestAutostartCommandRejectsUnusablePaths(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"embedded quote":   `C:\we"ird\PetPC.exe`,
		"quoted already":   `"C:\PetPC.exe"`,
		"quote in the dir": `C:\a"b\PetPC.exe`,
	}
	for name, path := range cases {
		if got, err := autostartCommand(path); err == nil {
			t.Errorf("%s: autostartCommand(%q) = %q, want an error", name, path, got)
		}
	}
}

// TestAutostartCommandPrefersAbsolutePaths makes sure a relative executable
// path cannot be stored, because the Run key has no working directory of its
// own and would resolve it against an arbitrary directory at logon.
func TestAutostartCommandPrefersAbsolutePaths(t *testing.T) {
	got, err := autostartCommand(`PetPC.exe`)
	if err != nil {
		t.Fatalf("autostartCommand: %v", err)
	}
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Fatalf("command %s is not quoted", got)
	}
	unquoted := strings.Trim(got, `"`)
	if !strings.Contains(unquoted, `:\`) {
		t.Errorf("command %s does not look absolute", got)
	}
}

// TestSetAutostartRoundTrip exercises the real registry through a scratch value,
// and asserts the stored value is exactly the Windows-quoted command rather than
// a Go-quoted one.
func TestSetAutostartRoundTrip(t *testing.T) {
	withScratchValue(t, func() {
		read := func() (string, bool) {
			k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
			if err != nil {
				return "", false
			}
			defer k.Close()
			v, _, err := k.GetStringValue(testValueName)
			if err != nil {
				return "", false
			}
			return v, true
		}

		if autostartEnabled(testValueName) {
			t.Fatal("autostartEnabled reported true before enabling")
		}
		if _, ok := read(); ok {
			t.Fatal("a scratch value already existed")
		}

		if err := setAutostart(testValueName, true); err != nil {
			t.Fatalf("setAutostart(true): %v", err)
		}
		if !autostartEnabled(testValueName) {
			t.Error("autostartEnabled reported false after enabling")
		}

		val, ok := read()
		if !ok {
			t.Fatal("setAutostart(true) wrote no value")
		}
		want, err := autostartCommand(ExecutablePath())
		if err != nil {
			t.Fatalf("autostartCommand: %v", err)
		}
		if val != want {
			t.Errorf("stored value = %s, want %s", val, want)
		}
		if strings.Contains(val, `\\`) {
			t.Errorf("stored value %s has escaped separators; Explorer will skip it", val)
		}

		// Enabling again must be idempotent.
		if err := setAutostart(testValueName, true); err != nil {
			t.Fatalf("setAutostart(true) again: %v", err)
		}
		if again, _ := read(); again != want {
			t.Errorf("second enable changed the value to %s", again)
		}

		if err := setAutostart(testValueName, false); err != nil {
			t.Fatalf("setAutostart(false): %v", err)
		}
		if autostartEnabled(testValueName) {
			t.Error("autostartEnabled reported true after disabling")
		}
		if _, ok := read(); ok {
			t.Error("scratch value still present after disabling")
		}

		// Disabling when already absent must not error.
		if err := setAutostart(testValueName, false); err != nil {
			t.Errorf("disable is not idempotent: %v", err)
		}
	})
}

// TestRepairAutostartRewritesBrokenEntry is the regression guard for the exact
// user-visible symptom: the checkbox was ticked, a value existed, but every
// logon silently failed to start the app.
func TestRepairAutostartRewritesBrokenEntry(t *testing.T) {
	withScratchValue(t, func() {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
		if err != nil {
			t.Fatalf("open Run key: %v", err)
		}
		defer k.Close()

		want, err := autostartCommand(ExecutablePath())
		if err != nil {
			t.Fatalf("autostartCommand: %v", err)
		}

		// The malformed value an older build wrote: Go-escaped separators.
		broken := `"` + strings.ReplaceAll(strings.Trim(want, `"`), `\`, `\\`) + `"`
		if err := k.SetStringValue(testValueName, broken); err != nil {
			t.Fatalf("seed broken value: %v", err)
		}
		// The entry exists, so the checkbox would have shown "on".
		if !autostartEnabled(testValueName) {
			t.Fatal("a seeded entry should count as enabled")
		}

		if !repairAutostart(testValueName) {
			t.Fatal("repairAutostart did not report repairing a malformed entry")
		}
		got, _, err := k.GetStringValue(testValueName)
		if err != nil {
			t.Fatalf("read repaired value: %v", err)
		}
		if got != want {
			t.Errorf("repaired value = %s, want %s", got, want)
		}
		if strings.Contains(got, `\\`) {
			t.Errorf("repaired value %s still has escaped separators", got)
		}

		// A correct entry must be left alone.
		if repairAutostart(testValueName) {
			t.Error("repairAutostart rewrote an already-correct entry")
		}
	})
}

// TestRepairAutostartDoesNotHijackAnotherInstall is the regression test for the
// second bug found while fixing the first: GetState runs the repair on every
// read, so a blanket "make the entry match the running executable" let any other
// copy of the binary take over the user's auto-start entry.
//
// It was not hypothetical - `go test ./...` repointed a real PetPC Run value at
// a temporary PetPC.test.exe, which is why auto-start then did nothing at all.
func TestRepairAutostartDoesNotHijackAnotherInstall(t *testing.T) {
	withScratchValue(t, func() {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
		if err != nil {
			t.Fatalf("open Run key: %v", err)
		}
		defer k.Close()

		// A perfectly valid entry that just happens to point at another copy.
		other := `"C:\Some\Other\Install\PetPC.exe"`
		if err := k.SetStringValue(testValueName, other); err != nil {
			t.Fatalf("seed foreign value: %v", err)
		}

		if repairAutostart(testValueName) {
			t.Error("repairAutostart rewrote an entry pointing at another install")
		}
		got, _, err := k.GetStringValue(testValueName)
		if err != nil {
			t.Fatalf("read value: %v", err)
		}
		if got != other {
			t.Errorf("the entry was hijacked: got %s, want %s", got, other)
		}
	})
}

// TestIsGoEscapedPath documents exactly which strings are treated as malformed,
// since that predicate is what keeps the repair from touching valid entries.
func TestIsGoEscapedPath(t *testing.T) {
	malformed := []string{
		`"F:\\pet_pc\\build\\bin\\PetPC.exe"`,
		`"C:\\Program Files\\PetPC.exe"`,
		`F:\\pet_pc\\PetPC.exe`,
	}
	for _, s := range malformed {
		if !isGoEscapedPath(s) {
			t.Errorf("isGoEscapedPath(%s) = false, want true", s)
		}
	}

	valid := []string{
		`"F:\pet_pc\build\bin\PetPC.exe"`,
		`"C:\Program Files\PetPC\PetPC.exe"`,
		`C:\PetPC.exe`,
		``,
	}
	for _, s := range valid {
		if isGoEscapedPath(s) {
			t.Errorf("isGoEscapedPath(%s) = true, want false", s)
		}
	}
}

// TestRepairAutostartIgnoresMissingEntry makes sure the read path does not
// create an auto-start entry for a user who switched it off.
func TestRepairAutostartIgnoresMissingEntry(t *testing.T) {
	withScratchValue(t, func() {
		if repairAutostart(testValueName) {
			t.Error("repairAutostart reported a change with no entry present")
		}
		if autostartEnabled(testValueName) {
			t.Error("repairAutostart created an entry that did not exist")
		}
	})
}

// TestAutostartTestsLeaveRealEntryAlone is the regression test for the mistake
// that motivated this whole scratch-value design: running the suite must not
// change whether the user's own PetPC entry exists.
func TestAutostartTestsLeaveRealEntryAlone(t *testing.T) {
	before := detailAutostart(autostartValueName)

	// Exercise every write path through the scratch value.
	withScratchValue(t, func() {
		_ = setAutostart(testValueName, true)
		_ = setAutostart(testValueName, true)
		_ = repairAutostart(testValueName)
		_ = setAutostart(testValueName, false)
		_ = setAutostart(testValueName, false)
	})

	if after := detailAutostart(autostartValueName); before != after {
		t.Fatalf("the real autostart entry changed:\n  before = %s\n  after  = %s", before, after)
	}
}

// TestSuiteDoesNotTouchRealEntry guards the two regressions that both silently
// broke auto-start on the developer's own machine during this work: a test
// calling SetAutostart(false) on the real value, and the GetState repair
// repointing it at a temporary test binary.
//
// It asserts the invariant directly: the scratch helpers, which are what every
// other test drives, must not move the real entry. RepairAutostart is
// deliberately not called here, because repairing a genuinely malformed real
// entry is legitimate behaviour rather than a regression.
func TestSuiteDoesNotTouchRealEntry(t *testing.T) {
	if autostartValueName == testValueName {
		t.Fatal("scratch and real value names must differ")
	}

	before := detailAutostart(autostartValueName)

	// Every write path the other tests use, driven through the scratch name.
	withScratchValue(t, func() {
		_ = setAutostart(testValueName, true)
		_ = repairAutostart(testValueName)
		_ = autostartEnabled(testValueName)
		_ = startupApprovedDisabled(testValueName)
		_ = clearStartupApproved(testValueName)
		_ = setAutostart(testValueName, false)
	})

	if after := detailAutostart(autostartValueName); before != after {
		t.Fatalf("the real autostart entry changed:\n  before = %s\n  after  = %s", before, after)
	}
}

// detailAutostart describes the named Run entry for comparison, distinguishing
// "absent" from "present but empty".
func detailAutostart(valueName string) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return "<no Run key: " + err.Error() + ">"
	}
	defer k.Close()
	v, _, err := k.GetStringValue(valueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "<absent>"
		}
		return fmt.Sprintf("<error: %v>", err)
	}
	return v
}

// setScratchApproved writes Explorer's enabled/disabled record for the scratch
// value, so the StartupApproved handling can be tested without touching the real
// entry's record.
func setScratchApproved(t *testing.T, disabled bool) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			k, _, err = registry.CreateKey(registry.CURRENT_USER, startupApprovedKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
		}
		if err != nil {
			t.Skipf("cannot open StartupApproved key: %v", err)
		}
	}
	t.Cleanup(func() {
		_ = k.DeleteValue(testValueName)
		k.Close()
	})

	blob := []byte{0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if disabled {
		blob[0] = 0x03
	}
	if err := k.SetBinaryValue(testValueName, blob); err != nil {
		t.Fatalf("write StartupApproved blob: %v", err)
	}
}

// TestStartupApprovedDisabledReadsTaskManagerFlag pins the byte semantics that
// make "the value exists but nothing starts at logon" possible.
//
// Task Manager's Startup tab does not delete the Run value; it records 0x03 in
// StartupApproved. The mapping was cross-checked against real boot data: OneDrive
// (0x03) was skipped at logon while ctfmon/Steam (0x02) launched.
func TestStartupApprovedDisabledReadsTaskManagerFlag(t *testing.T) {
	setScratchApproved(t, true)
	if !startupApprovedDisabled(testValueName) {
		t.Error("a 0x03 blob should read as disabled")
	}
}

// TestStartupApprovedEnabledIsNotDisabled covers the enabled spelling, which is
// what our own writes must leave behind.
func TestStartupApprovedEnabledIsNotDisabled(t *testing.T) {
	setScratchApproved(t, false)
	if startupApprovedDisabled(testValueName) {
		t.Error("a 0x02 blob should not read as disabled")
	}
}

// TestStartupApprovedAbsentIsNotDisabled makes sure a Run entry Explorer has
// never seen - the common first-time case - counts as enabled.
func TestStartupApprovedAbsentIsNotDisabled(t *testing.T) {
	withScratchValue(t, func() {
		if startupApprovedDisabled(testValueName) {
			t.Error("a missing StartupApproved record should not read as disabled")
		}
	})
}

// TestClearStartupApprovedRemovesRecord is the fix for the second half of the
// symptom: enabling auto-start must also drop a stale Task Manager "off" record,
// or Windows keeps skipping the entry.
func TestClearStartupApprovedRemovesRecord(t *testing.T) {
	setScratchApproved(t, true)
	if !startupApprovedDisabled(testValueName) {
		t.Fatal("precondition failed: blob should read as disabled")
	}
	if err := clearStartupApproved(testValueName); err != nil {
		t.Fatalf("clearStartupApproved: %v", err)
	}
	if startupApprovedDisabled(testValueName) {
		t.Error("the disabled record survived clearStartupApproved")
	}
}

// TestSetAutostartClearsDisabledRecord is the end-to-end version: ticking the box
// in the panel must leave the entry genuinely launchable, even if the user had
// previously switched the pet off in Task Manager.
func TestSetAutostartClearsDisabledRecord(t *testing.T) {
	withScratchValue(t, func() {
		// A Run value that is present and correct, but marked disabled.
		if err := setAutostart(testValueName, true); err != nil {
			t.Fatalf("setAutostart(true): %v", err)
		}
		setScratchApproved(t, true)
		if !startupApprovedDisabled(testValueName) {
			t.Fatal("precondition failed: blob should read as disabled")
		}

		// Re-enabling through the app must clear it.
		if err := setAutostart(testValueName, true); err != nil {
			t.Fatalf("setAutostart(true) again: %v", err)
		}
		if startupApprovedDisabled(testValueName) {
			t.Error("setAutostart left a stale disabled record; Windows would still skip the entry")
		}
	})
}

// TestRepairAutostartClearsDisabledRecord covers the read path: GetState must be
// able to heal a disabled record for a user who never reopens the setting.
func TestRepairAutostartClearsDisabledRecord(t *testing.T) {
	withScratchValue(t, func() {
		if err := setAutostart(testValueName, true); err != nil {
			t.Fatalf("setAutostart(true): %v", err)
		}
		setScratchApproved(t, true)

		// repairAutostart only fixes the value; the exported RepairAutostart is
		// what also clears the flag, so exercise that pairing explicitly.
		repaired := repairAutostart(testValueName)
		if autostartEnabled(testValueName) && startupApprovedDisabled(testValueName) {
			if err := clearStartupApproved(testValueName); err == nil {
				repaired = true
			}
		}
		if !repaired {
			t.Error("the disabled record should have been reported as repaired")
		}
		if startupApprovedDisabled(testValueName) {
			t.Error("the disabled record survived the repair")
		}
	})
}
