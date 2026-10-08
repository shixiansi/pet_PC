// Package voice plays the pet's fixed spoken lines.
//
// The lines are FIXED: every one the app can say is known ahead of time, so each is
// synthesised ONCE (tools/voice-fixed/make_lines.py) and embedded in the binary.
// That removes the entire real-time synthesis path, and with it the three problems
// that made it not worth keeping:
//
//   - latency: a 54-character reply took 11 seconds to speak, cold start 36.7s
//   - footprint: the local model held 3.3GB resident for a desktop pet
//   - stability: the upstream service documents its own output as random
//
// A fixed line has none of those, because playing a WAV is instant, costs no memory
// beyond the file, and produces the same audio every time.
//
// This is a deliberate reduction in scope, not a limitation worked around. Speech
// was only ever wanted for the app's own notices - the timer and the todo reminder -
// and those are a handful of sentences.
package voice

import (
	"embed"
	"sync"
)

// linesFS holds the rendered clips. They are embedded rather than loaded from disk
// so the executable stays self-contained: there is no data directory to find, no
// missing-file state, and nothing for the user to configure.
//
//go:embed lines/*.wav
var linesFS embed.FS

// Line identifies one fixed utterance.
//
// A string type rather than an int enum so the value in a debug log or a test
// failure is readable on its own, and so the embedded file name is the value.
type Line string

const (
	// LineFocusEnd announces that a focus phase finished.
	LineFocusEnd Line = "focus_end"
	// LineBreakEnd announces that a rest finished.
	LineBreakEnd Line = "break_end"
	// LineReminderDue announces that something is due.
	LineReminderDue Line = "reminder_due"
	// LineReminderOverdue announces that something is past its due date.
	LineReminderOverdue Line = "reminder_overdue"
	// LineReminderNone is used when a reminder fires with nothing to report, which
	// is a state the user can switch on deliberately (RemindWhenEmpty).
	LineReminderNone Line = "reminder_none"
)

// AllLines is every line the app can play, for tests and for a startup check.
var AllLines = []Line{
	LineFocusEnd,
	LineBreakEnd,
	LineReminderDue,
	LineReminderOverdue,
	LineReminderNone,
}

// Texts is the sentence each line speaks.
//
// It belongs here, beside the line inventory, because it is the same fact: a line
// key names a clip and this is what that clip says. Keeping one copy means the
// embedded renderer, the studio UI and the online generator cannot disagree about
// what a line is - and a second copy is exactly what used to drift, since
// tools/voice-fixed/make_lines.py held one of its own.
var Texts = map[Line]string{
	LineFocusEnd:        "专注时间结束了，起来活动一下吧。",
	LineBreakEnd:        "休息结束了，我们继续吧。",
	LineReminderDue:     "有些事情到期了，记得看一下。",
	LineReminderOverdue: "有事情已经逾期了，记得处理一下。",
	LineReminderNone:    "现在没有待办，好好休息吧。",
}

// Text returns the sentence for a line, or "" for a line that does not exist.
func Text(line Line) string { return Texts[line] }

// Known reports whether a line is one of the declared lines.
//
// Used to reject a line name arriving from the frontend, which is the one place a
// line identifier is not compile-time checked.
func Known(line Line) bool {
	_, ok := Texts[line]
	return ok
}

// BuiltInRole names the character the EMBEDDED clips were rendered with.
//
// It is a fact about the build, not a setting, and it is recorded because the UI
// must be able to say it: a user who switches 使用角色 to 纳西妲 and then presses
// "试听内置" hears 可莉 and reasonably concludes the switch did nothing. The switch
// only affects NEW renders - the built-in clips are baked into the executable and
// cannot change without a rebuild - so naming them is the honest answer.
//
// Kept in sync with tools/voice-fixed/make_lines.py, which renders these clips.
const BuiltInRole = "可莉"

// BuiltInRoleID is the SENTINEL role id standing for the embedded clips.
//
// It exists so "the built-in voice" is a value the role resolver can RETURN rather
// than a special case every caller re-derives: the one legitimate form of it is an
// empty ActiveRole ("never set"), and ResolveCurrentRole reports it as this id.
//
// It is deliberately SEPARATE from BuiltInRole, which is a display name: a user is
// free to configure their own role NAMED 可莉 (DiscoverRoles reads the name out of
// petpc-config.json, so it happens without anyone trying), and the two must stay
// distinguishable - otherwise "playing the built-in clips" and "playing that user's
// 可莉 renders" become the same string and neither the code nor the panel can tell
// the user which one is in effect.
//
// Deliberately not a slug-able character name: the surrounding underscores keep it
// from colliding with any id DiscoverRoles can produce from a directory or a name.
const BuiltInRoleID = "__builtin__"

// BuiltInRoleSentinel is the role value ResolveCurrentRole returns for "never set".
//
// A Role rather than a bare id so every caller handles it with the same type, and
// so the display name travels with it: the UI has something to show without a
// second lookup that could disagree.
func BuiltInRoleSentinel() Role {
	return Role{ID: BuiltInRoleID, Name: BuiltInRole}
}

// IsBuiltInRole reports whether a role id is the built-in sentinel.
//
// The one predicate the whole package asks, so "is this the embedded voice?" cannot
// be answered two different ways in two places - which is how the panel and the
// player came to disagree in the first place.
func IsBuiltInRole(id string) bool { return id == BuiltInRoleID }

// Labels is a short human name for each line, for the voice studio's list.
//
// Separate from Texts because the studio needs to label a row before the user has
// read the sentence - "番茄钟结束" identifies the row at a glance where the full
// sentence does not.
var Labels = map[Line]string{
	LineFocusEnd:        "番茄钟 · 专注结束",
	LineBreakEnd:        "番茄钟 · 休息结束",
	LineReminderDue:     "待办提醒 · 有到期",
	LineReminderOverdue: "待办提醒 · 已逾期",
	LineReminderNone:    "待办提醒 · 无待办",
}

// audio caches the decoded file contents.
//
// Read once and kept, because the clips are a few hundred kilobytes in total and
// the alternative - re-reading the embedded FS for every timer tick - would touch
// the same bytes repeatedly for no benefit.
var (
	mu    sync.Mutex
	cache = map[Line][]byte{}
)

// Bytes returns the WAV data for a line.
//
// It reports ok=false for an unknown line rather than returning an error, because
// every caller responds the same way (say nothing) and an error would have to be
// discarded at each of them.
func Bytes(line Line) ([]byte, bool) {
	mu.Lock()
	if data, hit := cache[line]; hit {
		mu.Unlock()
		return data, true
	}
	mu.Unlock()

	data, err := linesFS.ReadFile("lines/" + string(line) + ".wav")
	if err != nil {
		return nil, false
	}

	mu.Lock()
	cache[line] = data
	mu.Unlock()
	return data, true
}

// Available reports which lines are present in the binary.
//
// It exists for a startup self-check: a missing embedded file is a BUILD problem
// (the go:embed pattern did not match), and finding it at startup is far better than
// discovering it when a timer fires and the pet says nothing.
func Available() map[Line]bool {
	out := make(map[Line]bool, len(AllLines))
	for _, l := range AllLines {
		_, ok := Bytes(l)
		out[l] = ok
	}
	return out
}
