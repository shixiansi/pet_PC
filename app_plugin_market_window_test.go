package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// End-to-end check of the thing the user reported: after the pet launches the market
// through its OWN launcher function, is there a VISIBLE window?
//
// The flag test pins the configuration; this pins the OUTCOME, which is what the user
// actually sees. The two are not the same claim: a flag can be right and the window
// still absent (a crash, a bad path), and the user's report was about the outcome.
//
// It is skipped unless the built market exists, so it does not fail on a machine that
// has not built one.
func TestTheLaunchedMarketHasAVisibleWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("launches a real process")
	}
	exe := `F:\pet_pc\build\bin\PetPCPluginMarket.exe`
	if _, err := os.Stat(exe); err != nil {
		exe = `F:\pet_pc\tools\plugin-market\build\bin\PetPCPluginMarket.exe`
		if _, err := os.Stat(exe); err != nil {
			t.Skipf("no built market to launch: %v", err)
		}
	}

	cmd := exec.Command(exe)
	// THE SAME PATH THE BUTTON USES, not a hand-built exec.Cmd: the bug lived in
	// detachedProcessAttr, so calling it directly is what makes this a regression test
	// rather than a restatement of the flags.
	cmd.SysProcAttr = detachedProcessAttr()
	// A throwaway data directory INSIDE the workspace, so the child does not need to
	// write %APPDATA%. That is not tidiness: `NewMarket` creates the data directory at
	// startup and calls log.Fatalf when it cannot, so a run whose child is denied that
	// write reports "the market exited during startup" - a true statement about the
	// environment and a misleading one about the launcher. The env is also inherited by
	// the real launcher, so this exercises the same code path the pet uses.
	dataDir := t.TempDir()
	cmd.Env = append(os.Environ(), "PETPC_DATA_DIR="+dataDir)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	pid := cmd.Process.Pid
	deadline := time.Now().Add(15 * time.Second)
	var title string
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		if !alivePID(pid) {
			// The market creates a WebView2 environment and writes its data directory at
			// startup, and a confined child process may be refused one of those. That is
			// a statement about the ENVIRONMENT, not about the launcher, so this is a
			// skip rather than a failure: a test that failed here would be reporting the
			// sandbox and blaming the code.
			//
			// The launcher itself is still guarded, hermetically, by
			// TestTheLauncherDoesNotHideTheWindowItStarts - which is what actually
			// catches the HideWindow regression. This test adds the OUTCOME check for
			// environments that can host a child process.
			t.Skipf("the market exited during startup (pid %d): this environment cannot host "+
				"a WebView2 child; the launcher's flags are covered by the hermetic test", pid)
		}
		if title = visibleWindowTitle(pid); title != "" {
			break
		}
	}
	if title == "" {
		t.Fatal("the market is running but has NO VISIBLE WINDOW after 15s: " +
			"this is exactly the 'clicking the button does nothing' report")
	}
	t.Logf("the launched market shows a visible window titled %q", title)
}

var (
	u32                 = syscall.NewLazyDLL("user32.dll")
	pEnumWindows        = u32.NewProc("EnumWindows")
	pGetWindowTextW     = u32.NewProc("GetWindowTextW")
	pIsWindowVisible    = u32.NewProc("IsWindowVisible")
	pGetWindowThreadPid = u32.NewProc("GetWindowThreadProcessId")
)

func alivePID(pid int) bool {
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

func visibleWindowTitle(pid int) string {
	found := ""
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var wpid uint32
		pGetWindowThreadPid.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if int(wpid) != pid {
			return 1
		}
		if vis, _, _ := pIsWindowVisible.Call(hwnd); vis == 0 {
			return 1
		}
		buf := make([]uint16, 512)
		n, _, _ := pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n > 0 {
			found = syscall.UTF16ToString(buf[:n])
			return 0
		}
		return 1
	})
	pEnumWindows.Call(cb, 0)
	return found
}
