package plugins

// Which capability calls are "data plumbing" rather than "something the plugin did".
//
// # WHY THIS IS A RULE AND NOT A GUESS AT EACH CALL SITE
//
// Both hosts write one log line per plugin call, and at INFO that rule drowned the log: a
// plugin that polls (the pomodoro rereads its state every second; the todo list rereads its
// storage every two) produced 9729 `share.get` + 2842 `storage.get` + 580 `config.get` lines
// against 32 `llm.chat` lines on a real machine. The log a user opens to answer "why did that
// not work" was 99.5% the plugins reading their own files.
//
// So the line's LEVEL is derived from the method:
//
//   - **data** (`storage.*`, `share.*`, `config.*`, `memory.*`, `plugin.log`, `llm.usage`) is DEBUG.
//     It is the plugin's own file I/O - interesting only when you are debugging "my setting did
//     not stick", and uninteresting every second of a running timer. `llm.usage` is here for the
//     same reason even though its subject is money: it reads a counter, it spends nothing.
//   - **actions** (bubble, surface, audio, llm, net, ws, process, events) are INFO. These are
//     the things a human can see or that cost money.
//   - **failures** are WARN regardless (the callers do that).
//
// A method this table does not know is treated as an action (INFO): a new capability should be
// visible by default, and "quietly DEBUG" is the wrong default for something nobody has
// classified yet.
//
// The remaining flood - a plugin calling a real action in a loop - is handled by the caller's
// throttle (internal/logging.Throttle), not by this table.
func DataMethod(method string) bool {
	switch method {
	case "storage.get", "storage.set", "storage.remove", "storage.stat",
		"storage.dir", "storage.list", "storage.move", "storage.usage",
		"share.get", "share.set", "share.remove",
		"config.get", "config.set",
		"memory.read", "memory.append",
		// The plugin's own line is logged at the level IT asked for; the CALL itself is
		// plumbing, and a plugin that logs inside a poll loop would otherwise turn the call
		// log into a mirror of its own noise.
		"plugin.log",
		// Today's model usage for the caller. It READS the counter the host keeps for it:
		// nothing is spent, nothing changes, and the natural caller is a status line that
		// pulls it on a fixed rhythm. That is the same shape as share.get, one level up -
		// and the reason it is not an action is the incident in this file's header: a
		// status bar polling every five seconds at INFO is exactly how 9729 lines of
		// share.get drowned 32 lines of llm.chat. What DOES cost money (llm.chat) stays an
		// action, and it already carries its own "tokens in=… out=…" line.
		"llm.usage":
		return true
	case "pet.badge":
		// Not data plumbing, but the same kind of noise: a countdown pushes this once per second
		// for as long as it runs, and nothing about "the clock is still showing 24:59" belongs in
		// the story a user reads at INFO. The interesting part of a badge - that it was refused,
		// or that a plugin tried to draw on the pet - still reaches WARN through the gate.
		return true
	default:
		return false
	}
}
