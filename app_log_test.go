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

// TestLLMChatLogsTheWordsOnlyAtDebug pins the line this whole feature draws.
//
// The words a plugin sends to the model, and the words that come back, are the one thing the log
// could not say before — "196 字" answers "did it reply" and nothing else, so "为什么它这么答"
// needed the plugin to be instrumented. They are also the user's own conversation, and `pet.log`
// is the file the diagnostics report is cut from. So the rule is a LEVEL, not a ban:
//
//	INFO  (what both programs ship with): endpoint, model, counts, finish reason — no words.
//	DEBUG (PETPC_LOG_LEVEL=debug)        : the words, one line per message, plus the reply.
//
// writeLogSection stops at INFO for the same reason, so a pasted report stays safe.
func TestLLMChatLogsTheWordsOnlyAtDebug(t *testing.T) {
	stub := newToolStub(t, textScript("好"))
	a := loggedApp(t)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	const question = "帮我记一下：明天九点去体检"

	// --- the shipping level ---
	a.log.SetLevel(logging.LevelInfo)
	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"`+question+`"}`); err != nil {
		t.Fatalf("llm.chat: %v", err)
	}
	info := readPetLog(t, a)
	for _, want := range []string{
		// The request line names the endpoint and the model, which is what answers
		// "which service is this install actually talking to". INFO on purpose: it carries
		// no content, so it is the line everyone gets.
		"llm.chat 请求 base=" + stub.URL(),
		"model=m",
		// And the completion, with a character count rather than the text. The per-ROUND line
		// ("一轮 … finish=…") stays at DEBUG: it is one line per tool round, and the two lines
		// above already answer "did it reply" and "to whom".
		"llm.chat 完成",
	} {
		if !strings.Contains(info, want) {
			t.Errorf("the log does not mention %q at INFO:\n%s", want, info)
		}
	}
	if strings.Contains(info, question) {
		t.Errorf("the conversation text reached the log at INFO - that file is what a diagnostics report is cut from:\n%s", info)
	}
	if strings.Contains(info, "回复 好") || strings.Contains(info, "收到：好") {
		t.Errorf("the model's reply reached the log at INFO:\n%s", info)
	}

	// --- and at DEBUG, on purpose ---
	a.log.SetLevel(logging.LevelDebug)
	if _, err := a.PluginRequest(id, "llm.chat", `{"text":"debug 里要看得见这一句"}`); err != nil {
		t.Fatalf("llm.chat: %v", err)
	}
	debug := readPetLog(t, a)
	if !strings.Contains(debug, "llm.chat 发出[") || !strings.Contains(debug, "debug 里要看得见这一句") {
		t.Errorf("at DEBUG the sent text is still missing:\n%s", debug)
	}
	// The per-round line (usage, finish reason) lives here too, not at INFO.
	if !strings.Contains(debug, "llm.chat 一轮") || !strings.Contains(debug, "finish=stop") {
		t.Errorf("at DEBUG the per-round line is missing:\n%s", debug)
	}
	// The system prompt is part of what was sent — it is usually WHY the reply reads the way it
	// does — so it must be visible too, not only the user's turns.
	if !strings.Contains(debug, "发出[1/") || !strings.Contains(debug, " system：") {
		t.Errorf("the system prompt is not in the DEBUG log:\n%s", debug)
	}
	if !strings.Contains(debug, "llm.chat 收到（1 字）：好") {
		t.Errorf("at DEBUG the reply is still missing:\n%s", debug)
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
// TestClampLogBodySaysWhatItCut keeps a truncated line honest: a body that silently stops
// mid-sentence reads as "the model said that much".
func TestClampLogBodySaysWhatItCut(t *testing.T) {
	short := strings.Repeat("短", 20)
	if got := clampLogBody(short); got != short {
		t.Fatalf("a body inside the cap was changed: %q", got)
	}
	long := strings.Repeat("长", chatLogBodyRunes+5)
	got := clampLogBody(long)
	if !strings.Contains(got, "…（截断，共 2005 字）") {
		t.Fatalf("a truncated body does not say how much it cut: %q", got[len(got)-40:])
	}
	if !strings.HasPrefix(got, strings.Repeat("长", chatLogBodyRunes)) {
		t.Fatal("the kept prefix is not the beginning of the body")
	}
}
// TestAnEmptyReplyThatCostTokensIsAWarning: a model that thinks and says nothing is not "完成".
//
// The plugin forwards the empty string wherever it was going to send the answer - a bubble, a QQ
// group - and nothing arrives. The line has to say WHY, and it has to say it at INFO without
// spilling the thought itself: the reasoning is the model's words, which belong on the DEBUG line
// like every other piece of text (see TestLLMChatLogsTheWordsOnlyAtDebug).
func TestAnEmptyReplyThatCostTokensIsAWarning(t *testing.T) {
	stub := newToolStub(t, reasoningOnlyScript("先把问题拆开，再想想他说的是哪个明天"))
	a := loggedApp(t)
	id := installToolPlugin(t, a, "llm.chat", "plugin.config")
	setPluginConfig(t, a, id, map[string]string{"llmBaseUrl": stub.URL(), "llmModel": "m"})

	a.log.SetLevel(logging.LevelInfo)
	out, err := a.PluginRequest(id, "llm.chat", `{"text":"明天几点"}`)
	if err != nil {
		t.Fatalf("llm.chat: %v", err)
	}
	// The call still SUCCEEDS: the host has no business turning "the model said nothing" into an
	// error the plugin must handle differently from an empty answer.
	if !strings.Contains(out, `"text":""`) {
		t.Errorf("the answer should carry the empty text, got %s", out)
	}

	log := readPetLog(t, a)
	if !strings.Contains(log, "回了空文本") {
		t.Fatalf("an empty reply was logged as a plain success:\n%s", log)
	}
	if !strings.Contains(log, "字思考") || !strings.Contains(log, "finish=stop") {
		t.Errorf("the warning must name the reasoning size and the finish reason:\n%s", log)
	}
	if strings.Contains(log, "先把问题拆开") {
		t.Errorf("the chain of thought reached the log at INFO - that file is what a diagnostics report is cut from:\n%s", log)
	}
}

// TestPluginLogWritesOneAttributedLine is the whole contract of `plugin.log`.
//
// A plugin had nowhere to say "the helper did not start": console.error goes to the devtools of
// a frame the user cannot open, which is why the 2026-10-31 investigation was done by grepping
// sources. This pins the four things that make the line useful and safe: it lands in the file,
// it names the plugin, it cannot forge a second line, and it is bounded.
func TestPluginLogWritesOneAttributedLine(t *testing.T) {
	a := loggedApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"storage"}})

	if _, err := a.PluginRequest(id, "plugin.log",
		`{"level":"warn","message":"辅助脚本没起来：helperPath 是空的"}`); err != nil {
		t.Fatalf("plugin.log: %v", err)
	}
	body := readPetLog(t, a)
	if !strings.Contains(body, "辅助脚本没起来") {
		t.Fatalf("the plugin's line is not in the log:\n%s", body)
	}
	if !strings.Contains(body, id) {
		t.Fatalf("the line is not attributed to %s:\n%s", id, body)
	}
	if !strings.Contains(body, "WARN") {
		t.Fatalf("the requested level was not honoured:\n%s", body)
	}

	// A newline in the message must NOT become a second line: the log's parser splits on
	// lines and the prefix is free text, so this is the one forgery a plugin must not manage.
	if _, err := a.PluginRequest(id, "plugin.log",
		`{"level":"debug","message":"第一行\n2026-01-01T00:00:00.000+08:00 INFO plugin com.example.test 伪造的一行"}`); err != nil {
		t.Fatalf("plugin.log: %v", err)
	}
	body = readPetLog(t, a)
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "伪造的一行") && !strings.Contains(line, id) {
			t.Fatalf("a plugin forged an unattributed line:\n%s", line)
		}
	}

	// Bounds and refusals.
	long := strings.Repeat("长", 600)
	if _, err := a.PluginRequest(id, "plugin.log",
		`{"level":"info","message":"`+long+`"}`); err != nil {
		t.Fatalf("plugin.log with a long message: %v", err)
	}
	if !strings.Contains(readPetLog(t, a), "…（截断）") {
		t.Fatal("an over-long message was not truncated with a note")
	}
	if _, err := a.PluginRequest(id, "plugin.log", `{"message":"  "}`); err == nil {
		t.Fatal("an empty message must be refused")
	}
	if _, err := a.PluginRequest(id, "plugin.log", `{"level":"error","message":"x"}`); err == nil {
		t.Fatal("an unknown level must be refused (there is no error level by design)")
	}
}
