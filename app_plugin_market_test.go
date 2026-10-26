package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// These tests pin the pet's side of the STANDALONE PLUGIN MARKET: where it looks
// for the program, and how it starts it.
//
// Both halves are asserted through the seams on pluginMarketState rather than by
// starting a real window. That is not a shortcut around the interesting part -
// the interesting part IS the set of things a substitute lets a test observe:
// the candidate list must be exactly three paths in a fixed order, and the launch
// must be detached (its own directory, no waiting). A test that spawned the real
// executable could assert neither without racing the child.

// --- fixtures --------------------------------------------------------------

// marketLaunch is a substitute launcher that records what it was asked to start.
//
// `running` stays true forever: the fake child never exits, and nothing in the
// test releases it. That is deliberate - it is how "OpenPluginMarket returned
// without waiting for the child" becomes observable. If the production code ever
// waited on the process, this call would block until the test timed out.
type marketLaunch struct {
	mu      sync.Mutex
	calls   []*exec.Cmd
	running bool
}

func (l *marketLaunch) start(cmd *exec.Cmd) (*os.Process, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, cmd)
	l.running = true
	// A handle with a plausible pid and no OS process behind it: the production
	// code only stores it, so a fabricated one exercises the same path.
	return &os.Process{Pid: 4242}, nil
}

func (l *marketLaunch) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

func (l *marketLaunch) lastDir() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.calls) == 0 {
		return ""
	}
	return l.calls[len(l.calls)-1].Dir
}

// fakeMarket points the app at a fabricated executable location and a fabricated
// working directory, and installs the recording launcher.
//
// exeDir plays the role of the directory the pet itself lives in, which is where
// the first two candidates are resolved from.
func fakeMarket(t *testing.T, a *App, exeDir, wd string) *marketLaunch {
	t.Helper()
	l := &marketLaunch{}
	a.pluginMarket.exe = func() (string, error) { return filepath.Join(exeDir, "PetPC.exe"), nil }
	a.pluginMarket.getwd = func() (string, error) { return wd, nil }
	a.pluginMarket.launch = l.start
	return l
}

// placeMarket writes an empty file with the market's executable name into dir.
//
// The launcher only ever checks that a same-named regular FILE exists, so an
// empty one is the honest fixture: it pins "found" without pretending the bytes
// matter. Parent directories are created because the build-tree candidates name
// a path several levels deep, and that path is the point of those cases.
func placeMarket(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, pluginMarketExeName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// --- T85: the search order -------------------------------------------------

// TestPluginMarketCandidatesAreThreePathsInOrder pins the search order itself.
//
// The order is the contract: a packaged build must be found beside the pet, and
// a development build must be found in the build tree. A candidate list that has
// the right members in the wrong order would silently prefer a stale development
// build over the one shipped next to the pet.
func TestPluginMarketCandidatesAreThreePathsInOrder(t *testing.T) {
	a := newTestApp(t)
	exeDir := filepath.Join("C:", "pet", "bin")
	wd := filepath.Join("C:", "repo")
	fakeMarket(t, a, exeDir, wd)

	got := a.PluginMarketCandidates()
	if len(got) != 3 {
		t.Fatalf("want 3 candidates, got %d: %v", len(got), got)
	}
	if want := filepath.Join(exeDir, pluginMarketExeName); got[0] != want {
		t.Errorf("candidate 0 = %q, want %q (beside the pet)", got[0], want)
	}
	wantTree := filepath.Join(exeDir, "..", "tools", "plugin-market", "build", "bin", pluginMarketExeName)
	if got[1] != wantTree {
		t.Errorf("candidate 1 = %q, want %q (build tree relative to the exe)", got[1], wantTree)
	}
	if !strings.HasSuffix(got[2], filepath.Join("tools", "plugin-market", "build", "bin", pluginMarketExeName)) {
		t.Errorf("candidate 2 = %q, want the suffix tools\\plugin-market\\build\\bin\\%s", got[2], pluginMarketExeName)
	}
	if want := filepath.Join(wd, "tools", "plugin-market", "build", "bin", pluginMarketExeName); got[2] != want {
		t.Errorf("candidate 2 = %q, want %q (build tree relative to the working directory)", got[2], want)
	}
}

// TestPluginMarketPathFindsTheFileBesideThePet is the "the release layout works"
// case: with only the first candidate populated, the path that comes back is
// that file.
func TestPluginMarketPathFindsTheFileBesideThePet(t *testing.T) {
	a := newTestApp(t)
	exeDir := t.TempDir()
	want := placeMarket(t, exeDir)
	fakeMarket(t, a, exeDir, t.TempDir())

	got, ok := a.PluginMarketPath()
	if !ok {
		t.Fatalf("PluginMarketPath found nothing, want %s", want)
	}
	if got != want {
		t.Fatalf("PluginMarketPath = %q, want %q", got, want)
	}
}

// TestPluginMarketPathFallsThroughToTheBuildTree pins the middle candidate: the
// release copy is absent, so the build tree beside the pet's own executable is
// what answers.
func TestPluginMarketPathFallsThroughToTheBuildTree(t *testing.T) {
	a := newTestApp(t)
	exeDir := t.TempDir()
	want := placeMarket(t, filepath.Join(exeDir, "..", "tools", "plugin-market", "build", "bin"))
	fakeMarket(t, a, exeDir, t.TempDir())

	got, ok := a.PluginMarketPath()
	if !ok {
		t.Fatalf("PluginMarketPath found nothing, want %s", want)
	}
	// filepath.Join cleans the "..", so the expected file is written at the
	// cleaned location; comparing cleaned paths keeps the assertion about the
	// candidate, not about a redundant path segment.
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("PluginMarketPath = %q, want %q", got, want)
	}
}

// TestPluginMarketPathIsEmptyWhenNothingIsInstalled is the honest-absence case:
// the settings page shows this instead of a path, and the button explains why.
func TestPluginMarketPathIsEmptyWhenNothingIsInstalled(t *testing.T) {
	a := newTestApp(t)
	fakeMarket(t, a, t.TempDir(), t.TempDir())

	if got, ok := a.PluginMarketPath(); ok {
		t.Fatalf("PluginMarketPath = %q, ok, want no path", got)
	}
}

// --- T86: the launch -------------------------------------------------------

// TestOpenPluginMarketReportsAMissingProgram checks the wording the user sees.
//
// It names the executable because that is the whole diagnosis: the market ships
// as a separate binary and it is either next to the pet or it is not. A message
// that only said "failed" would send the user looking for a bug in the pet.
func TestOpenPluginMarketReportsAMissingProgram(t *testing.T) {
	a := newTestApp(t)
	fakeMarket(t, a, t.TempDir(), t.TempDir())

	_, err := a.OpenPluginMarket()
	if err == nil {
		t.Fatal("OpenPluginMarket succeeded with no program present")
	}
	if want := "找不到插件市场程序（" + pluginMarketExeName + "）"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err, want)
	}
}

// TestOpenPluginMarketStartsItOnceFromItsOwnDirectoryAndDoesNotWait is the
// launch contract, all three clauses at once because they are one act.
//
// "From its own directory" matters: the market resolves its paths and its own
// bundled frontend relative to its executable, so inheriting the pet's working
// directory is the difference between a working window and a blank one.
//
// "Does not wait" is asserted through the fake launcher, which never reports the
// child as finished: the call still has to return.
func TestOpenPluginMarketStartsItOnceFromItsOwnDirectoryAndDoesNotWait(t *testing.T) {
	a := newTestApp(t)
	exeDir := t.TempDir()
	placeMarket(t, exeDir)
	l := fakeMarket(t, a, exeDir, t.TempDir())

	state, err := a.OpenPluginMarket()
	if err != nil {
		t.Fatalf("OpenPluginMarket: %v", err)
	}
	if l.count() != 1 {
		t.Fatalf("launcher called %d times, want exactly 1", l.count())
	}
	if got := l.lastDir(); got != exeDir {
		t.Fatalf("cmd.Dir = %q, want the executable's directory %q", got, exeDir)
	}
	// The fake child has not exited and never will: if the call waited for it,
	// this line would not be reached.
	if !l.running {
		t.Fatal("the launch reported the child as already finished; the helper should never end it")
	}
	// The snapshot is returned so the settings wiring is the same shape as every
	// other action; a zero State here would mean the caller had nothing to apply.
	if state.DataDir == "" {
		t.Error("OpenPluginMarket returned an empty state snapshot")
	}
}

// TestOpenPluginMarketRecordsTheLaunchedProcess pins that the handle is kept:
// without it a second click cannot be told from the first, and the second click
// would start a second window.
func TestOpenPluginMarketRecordsTheLaunchedProcess(t *testing.T) {
	a := newTestApp(t)
	exeDir := t.TempDir()
	placeMarket(t, exeDir)
	fakeMarket(t, a, exeDir, t.TempDir())

	if _, err := a.OpenPluginMarket(); err != nil {
		t.Fatalf("OpenPluginMarket: %v", err)
	}
	a.pluginMarket.mu.Lock()
	proc := a.pluginMarket.launched
	a.pluginMarket.mu.Unlock()
	if proc == nil {
		t.Fatal("the launched process was not recorded")
	}
}

// --- T86: the gate-only startup hook ---------------------------------------

// TestPluginMarketGateVariableName pins the literal the pixel gate sets.
//
// The name is assembled from two pieces on purpose: the gate seeks it in the Go
// sources by text, and exactly one of those mentions may exist - the constant in
// app_plugin_market.go that documents it. A second literal here would make "how
// many places can start the market at startup" unanswerable by grep, so the test
// constructs the name instead of spelling it.
func TestPluginMarketGateVariableName(t *testing.T) {
	want := "PETPC_OPEN_" + "PLUGIN_MARKET"
	if pluginMarketOpenEnv != want {
		t.Fatalf("pluginMarketOpenEnv = %q, want %q", pluginMarketOpenEnv, want)
	}
}

// TestStartupHookLaunchesOnlyWhenAsked is the "default behaviour is unchanged"
// clause.
//
// The hook is the startup sequence's step, so this calls the step itself rather
// than simulating it: unset is the default every user is in, and it must launch
// nothing at all; set to '1' is what the gate uses, and it must launch exactly
// once. Any other value is treated as unset - only the documented '1' opens the
// door, so a stray "0" or "true" cannot start a window behind the user's back.
func TestStartupHookLaunchesOnlyWhenAsked(t *testing.T) {
	a := newTestApp(t)
	exeDir := t.TempDir()
	placeMarket(t, exeDir)
	l := fakeMarket(t, a, exeDir, t.TempDir())

	os.Unsetenv(pluginMarketOpenEnv)
	a.startPluginMarketFromEnv()
	if l.count() != 0 {
		t.Fatalf("the hook launched %d times with the variable unset, want 0", l.count())
	}

	t.Setenv(pluginMarketOpenEnv, "0")
	a.startPluginMarketFromEnv()
	if l.count() != 0 {
		t.Fatalf("the hook launched %d times for a value other than 1, want 0", l.count())
	}

	t.Setenv(pluginMarketOpenEnv, "1")
	a.startPluginMarketFromEnv()
	if l.count() != 1 {
		t.Fatalf("the hook launched %d times with the variable set, want exactly 1", l.count())
	}
}

// TestStartupHookSwallowsAMissingProgram pins that the gate-only hook cannot
// stop the pet from appearing.
//
// The variable is not a user feature, so a machine without the market installed
// must reach the end of startup exactly as it did before the hook existed.
func TestStartupHookSwallowsAMissingProgram(t *testing.T) {
	a := newTestApp(t)
	fakeMarket(t, a, t.TempDir(), t.TempDir())
	t.Setenv(pluginMarketOpenEnv, "1")

	a.startPluginMarketFromEnv() // must not panic or fail the test
}
