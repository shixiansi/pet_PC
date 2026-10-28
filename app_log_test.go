package main

// The log's own assertions: what a plugin call and a model call leave behind.
//
// Both are tested from the REQUEST path rather than by calling the logger, because the
// property being pinned is "this happened during a real call" - and because the two
// failures worth guarding against are silent:
//
//  1. a call that leaves no line at all. That is the state this change came from: a
//     plugin did nothing, the host said nothing, and the report was "它没反应";
//  2. a line that carries the user's TEXT. The log is meant to be pasted into a bug
//     report (README 「日志」, and the control center's 诊断报告 copies it), so a payload
//     that reached the log would turn every report into a leak. That is asserted here
//     the way internal/llm/log_test.go asserts it for the transport.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"PetPC/internal/logging"
	"PetPC/internal/store"
)

// loggedApp is pluginApp with a logger that writes to the SAME temp data directory, so
// the assertions read the file the program really wrote.
func loggedApp(t *testing.T) *App {
	t.Helper()
	a := pluginApp(t)
	a.log = logging.Open(a.store.Dir(), logging.PetLog, logging.Options{Level: logging.LevelDebug})
	return a
}

// readPetLog returns the log file's contents, failing the test if it is not there - an
// absent log IS the failure mode this file exists to catch.
func readPetLog(t *testing.T, a *App) string {
	t.Helper()
	path := filepath.Join(a.store.Dir(), logging.DirName, logging.PetLog+".log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("nothing was logged to %s: %v", path, err)
	}
	return string(data)
}

func TestPluginRequestsAreLoggedWithTheirOutcome(t *testing.T) {
	a := loggedApp(t)
	id := installToolPlugin(t, a, "pet.bubble")

	if _, err := a.PluginRequest(id, "bubble.say", `{"text":"该吃药了"}`); err != nil {
		t.Fatalf("bubble.say: %v", err)
	}
	// A permission the plugin does not hold: the gate refuses, and the refusal is the
	// interesting half - it is what the user's "它没反应" usually turns out to be.
	if _, err := a.PluginRequest(id, "storage.get", `{"key":"nope"}`); err == nil {
		t.Fatal("storage.get without the storage permission was allowed")
	}
	// A method this host does not serve at all: the version-skew case.
	if _, err := a.PluginRequest(id, "todos.list", `{}`); err == nil {
		t.Fatal("todos.list was allowed although the host serves no such method")
	}

	log := readPetLog(t, a)
	for _, want := range []string{
		"INFO plugin " + id + " bubble.say ok",
		"WARN plugin " + id + " storage.get 没有执行",
		"未获权限 storage",
		"todos.list 没有执行",
		"未知方法 todos.list",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log does not mention %q:\n%s", want, log)
		}
	}
	// The payload is NOT logged: it holds whatever the user typed (a todo, a message).
	if strings.Contains(log, "该吃药了") {
		t.Errorf("the plugin payload reached the log:\n%s", log)
	}
}

func TestLLMChatIsLoggedWithoutTheConversationText(t *testing.T) {
	stub := newToolStub(t, textScript("好"))
	a := loggedApp(t)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	const question = "帮我记一下：明天九点去体检"
	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"`+question+`"}`); err != nil {
		t.Fatalf("llm.chat: %v", err)
	}

	log := readPetLog(t, a)
	for _, want := range []string{
		// The request line names the endpoint and the model, which is what answers
		// "which service is this install actually talking to".
		"llm.chat 请求 base=" + stub.URL(),
		"model=m",
		// One line per round with the usage the server reported, and the finish reason.
		"llm.chat 一轮",
		"finish=stop",
		// And the completion, with a character count rather than the text.
		"llm.chat 完成",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log does not mention %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, question) {
		t.Errorf("the conversation text reached the log:\n%s", log)
	}
	// The reply's own text ("好") must not be recorded either - a character count is
	// the most a log may say about it.
	if strings.Contains(log, "回复 好") {
		t.Errorf("the model's reply reached the log:\n%s", log)
	}
}

// TestStartupLineNamesTheLogAndItsLevel: a report that looks empty has to be
// distinguishable from one taken while the interesting level was off.
func TestStartupLineNamesTheLogAndItsLevel(t *testing.T) {
	a := loggedApp(t)
	a.log.Infof("pet", "启动：数据目录 %s，日志级别 %s，日志文件 %s",
		a.store.Dir(), a.log.Level(), a.log.Path())

	log := readPetLog(t, a)
	if !strings.Contains(log, "日志级别 DEBUG") || !strings.Contains(log, "logs") {
		t.Errorf("the first line does not say which log and which level:\n%s", log)
	}
}

// TestDataCallsAreDebugAndActionsAreInfo pins the level rule that was WRONG in the first
// cut of the logging system: every successful call was INFO, and on a real machine that made
// 99.5% of the log a plugin's own polling (9729 share.get + 2842 storage.get + 580 config.get
// against 32 llm.chat). Reading a plugin's own file is plumbing; what it DID is the story.
func TestDataCallsAreDebugAndActionsAreInfo(t *testing.T) {
	// INFO level, i.e. the shipping default: the plumbing must be absent, the action present.
	dir := t.TempDir()
	s, err := store.New(dir)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	a := pluginApp(t)
	a.store = s
	a.log = logging.Open(dir, logging.PetLog, logging.Options{Level: logging.LevelInfo})
	id := installToolPlugin(t, a, "storage", "pet.bubble")

	if _, err := a.PluginRequest(id, "storage.get", `{"key":"k"}`); err != nil {
		t.Fatalf("storage.get: %v", err)
	}
	if _, err := a.PluginRequest(id, "bubble.say", `{"text":"你好"}`); err != nil {
		t.Fatalf("bubble.say: %v", err)
	}

	log := readPetLogAt(t, dir)
	if strings.Contains(log, "storage.get") {
		t.Errorf("a read of the plugin's own file reached INFO:\n%s", log)
	}
	if !strings.Contains(log, "bubble.say ok") {
		t.Errorf("an action did not reach INFO:\n%s", log)
	}
	// A refusal is WARN, whatever the method: that is the line a user opens the log for.
	if _, err := a.PluginRequest(id, "surface.show", `{}`); err == nil {
		t.Fatal("surface.show without pet.surface was allowed")
	}
	if !strings.Contains(readPetLogAt(t, dir), "WARN") || !strings.Contains(readPetLogAt(t, dir), "surface.show") {
		t.Errorf("a refusal did not reach WARN:\n%s", readPetLogAt(t, dir))
	}
}

// TestRepeatedCallsAreFoldedWithACount pins the other half: a plugin that polls must not bury
// the log at ANY level, and the fold must not lose the fact that it keeps happening.
func TestRepeatedCallsAreFoldedWithACount(t *testing.T) {
	a := loggedApp(t)
	id := installToolPlugin(t, a, "storage")

	// A controllable clock, so the window is asserted without sleeping through it.
	now := time.Unix(1_700_000_000, 0)
	a.callsMu.Lock()
	a.calls = logging.NewThrottle(5*time.Second, func() time.Time { return now })
	a.callsMu.Unlock()

	for i := 0; i < 5; i++ {
		if _, err := a.PluginRequest(id, "storage.set", `{"key":"k","value":"v"}`); err != nil {
			t.Fatalf("storage.set #%d: %v", i, err)
		}
	}
	log := readPetLog(t, a)
	if got := strings.Count(log, "storage.set ok"); got != 1 {
		t.Fatalf("five identical calls in one window produced %d lines, want 1:\n%s", got, log)
	}

	now = now.Add(6 * time.Second)
	if _, err := a.PluginRequest(id, "storage.set", `{"key":"k","value":"v"}`); err != nil {
		t.Fatalf("storage.set: %v", err)
	}
	log = readPetLog(t, a)
	if got := strings.Count(log, "storage.set ok"); got != 2 {
		t.Fatalf("the window did not reopen: %d lines, want 2:\n%s", got, log)
	}
	if !strings.Contains(log, "同类另有 4 次已合并") {
		t.Errorf("the folded calls are not accounted for:\n%s", log)
	}
}

// readPetLogAt reads a log written under an arbitrary data directory.
func readPetLogAt(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, logging.DirName, logging.PetLog+".log"))
	if err != nil {
		t.Fatalf("nothing was logged under %s: %v", dir, err)
	}
	return string(data)
}