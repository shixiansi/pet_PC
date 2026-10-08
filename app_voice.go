package main

import (
	"fmt"
	"path/filepath"
	"sync"

	"PetPC/internal/desktop"
	"PetPC/internal/store"
	"PetPC/internal/voice"
)

// voiceState holds the fixed-line player.
//
// It replaces the whole real-time synthesis stack (client, splitter, pipeline,
// cache - some 3000 lines). That stack was retired because a FIXED line makes none
// of it necessary: the audio is already rendered and embedded, so there is nothing
// to synthesise, nothing to split, nothing to cache and nothing to wait for.
//
// The lock guards only the lazily-built player, so it is never held across I/O.
type voiceState struct {
	mu sync.Mutex
	v  *voice.Voice
}

// desktopPlayer is the production player, forwarding to the Windows one.
//
// A named type rather than a closure so the wiring stays obvious, and so this
// package never has to import internal/voice's interface by name.
type desktopPlayer struct{}

func (desktopPlayer) PlayWAVFile(path string) error { return desktop.PlayWAVFile(path) }
func (desktopPlayer) StopAudio()                    { desktop.StopAudio() }

// speechDirName is the subdirectory of the data directory holding the materialised
// clips.
//
// They live in the app's own data directory rather than the system temp folder so
// they are covered by the same permissions and cleanup as everything else the app
// owns, and so a user reclaiming space has one place to look.
const speechDirName = "voice"

// voicePlayer returns the player, creating it on first use.
//
// Lazy so a build with speech switched off never creates a directory it will not
// use - the same reasoning as the retired cache.
func (a *App) voicePlayer() *voice.Voice {
	a.voice.mu.Lock()
	defer a.voice.mu.Unlock()
	if a.voice.v == nil {
		// The studio's library is passed in so playback prefers the user's own
		// renders. It is read once here because the pointer is stable for the life
		// of the app: the studio may reload its index, never its identity.
		a.voice.v = voice.NewWithUser(desktopPlayer{}, filepath.Join(a.store.Dir(), speechDirName), a.studioStore())
	}
	return a.voice.v
}

// initVoice prepares the fixed-line player at startup.
//
// It cannot fail. The worst case is that the user hears nothing, which must never
// delay the pet appearing. The line inventory is logged because a missing clip is a
// BUILD problem (the go:embed pattern did not match), and finding it here beats
// discovering it when a timer fires and the pet says nothing.
func (a *App) initVoice() {
	dir := filepath.Join(a.store.Dir(), speechDirName)
	voice.Cleanup(dir)

	available := voice.Available()
	missing := 0
	for _, ok := range available {
		if !ok {
			missing++
		}
	}
	cfg := a.store.Config()
	a.DebugLog(fmt.Sprintf("voice: enabled=%v lines=%d missing=%d dir=%s",
		cfg.VoiceEnabled, len(available), missing, dir))
}

// speakFixed plays one embedded line, unless the caller's own switch has it off.
//
// It is the only way the app speaks now, and it is deliberately trivial: look up a
// clip that already exists and play it. There is no synthesis, no worker, no queue
// and no failure mode beyond "the audio device did not answer".
//
// A disabled switch is not an error and is not logged: it is the user's setting.
func (a *App) speakFixed(line voice.Line, enabled func(store.Config) bool) {
	cfg := a.store.Config()
	if !cfg.VoiceEnabled {
		return
	}
	if enabled != nil && !enabled(cfg) {
		return
	}
	if err := a.voicePlayer().Play(line); err != nil {
		// Logged rather than shown: a bubble about audio would interrupt the very
		// notice it was announcing, and the visual notice still works.
		a.DebugLog("voice: " + err.Error())
	}
}

// stopVoice silences whatever is playing.
//
// winmm plays one sound per process, so a new line implicitly stops the previous
// one; this exists for the explicit stop path and for shutdown.
func (a *App) stopVoice() {
	a.voice.mu.Lock()
	v := a.voice.v
	a.voice.mu.Unlock()
	if v != nil {
		v.Stop()
	}
}

// speakPomodoroLine announces the end of a phase.
//
// The wording was written as something the PET says, not as a timer caption: the
// whole point of speaking it is that the user is looking at something else, so it
// has to make sense with no visual context at all.
func (a *App) speakPomodoroLine(endedPhase string) {
	if store.IsFocusPhase(endedPhase) {
		a.speakFixed(voice.LineFocusEnd, func(c store.Config) bool { return c.VoicePomodoro })
		return
	}
	a.speakFixed(voice.LineBreakEnd, func(c store.Config) bool { return c.VoicePomodoro })
}

// speakReminderLine announces a todo reminder.
//
// Three lines rather than one because the urgent case should sound different from
// the routine one - but there are no NUMBERS in them, and that is a deliberate
// limit of the fixed-line approach: a count cannot be spliced into a pre-rendered
// clip, and pre-rendering 1..99 would be hundreds of files for information the
// bubble already shows on screen.
func (a *App) speakReminderLine(due, overdue, total int) {
	switch {
	case total == 0:
		// Only reachable when the user switched on reminders over an empty list,
		// where "nothing to do" is itself the message.
		a.speakFixed(voice.LineReminderNone, func(c store.Config) bool { return c.VoiceReminder })
	case overdue > 0:
		a.speakFixed(voice.LineReminderOverdue, func(c store.Config) bool { return c.VoiceReminder })
	case due > 0:
		a.speakFixed(voice.LineReminderDue, func(c store.Config) bool { return c.VoiceReminder })
	default:
		// Something is open but nothing is due; the routine line is still right.
		a.speakFixed(voice.LineReminderDue, func(c store.Config) bool { return c.VoiceReminder })
	}
}

// VoiceView is the fixed-voice state the settings panel renders.
type VoiceView struct {
	// Enabled is the master switch.
	Enabled bool `json:"enabled"`
	// Pomodoro and Reminder are the two per-notice switches.
	Pomodoro bool `json:"pomodoro"`
	Reminder bool `json:"reminder"`
	// Lines is how many clips are embedded, so the panel can show that the feature
	// is intact rather than silently absent.
	Lines int `json:"lines"`
	// Dir is where the clips are materialised, shown so the user can find them.
	Dir string `json:"dir"`
	// Speaking is true while a line is playing, so the panel can offer a stop
	// button only when it would do something.
	Speaking bool `json:"speaking"`
	// BuiltInRole names the character the EMBEDDED clips were rendered with.
	//
	// The settings panel shows it so "试听" is not a mystery. It is a fact about the
	// BUILD and does not change: the built-in clips are baked into the executable
	// and only a rebuild replaces them.
	BuiltInRole string `json:"builtInRole"`
	// ActiveRole is the character currently in effect, and ActiveRoleName its
	// display name. When no character has been chosen this is the built-in sentinel
	// and ActiveRoleName is the built-in character.
	//
	// The panel needs BOTH facts, because they answer different questions: what the
	// executable carries, and what the pet will actually say. Showing only the
	// former is what made "当前：内置 可莉" appear to contradict the studio's
	// selection.
	ActiveRole     string `json:"activeRole"`
	ActiveRoleName string `json:"activeRoleName"`
	// ActiveRoleIsBuiltIn distinguishes "the built-in clips are in effect" from "a
	// user role that happens to be NAMED like the built-in character is in effect".
	// The panel needs the distinction and cannot derive it from the names, which is
	// exactly why the sentinel exists.
	ActiveRoleIsBuiltIn bool `json:"activeRoleIsBuiltIn"`
	// RoleError is non-empty when the stored character no longer exists. The panel
	// must surface it rather than silently showing the built-in voice, which is what
	// let a broken configuration look like a working one.
	RoleError string `json:"roleError,omitempty"`
	// Replaced is how many lines will NOW speak in the current character's voice,
	// i.e. lines with a take rendered AND adopted by that character. Counted against
	// the active role, so it cannot claim a line plays a voice it does not.
	Replaced int `json:"replaced"`
}

// voiceView assembles the fixed-voice snapshot.
func (a *App) voiceView() VoiceView {
	cfg := a.store.Config()
	a.voice.mu.Lock()
	v := a.voice.v
	a.voice.mu.Unlock()
	// Speaking is reported from the player's own view of the world, which is
	// necessarily approximate: winmm offers no completion callback, so "is it still
	// talking" is not something it can answer. A pipeline used to track this; with
	// fixed clips there is nothing to track, so the flag is false and the panel
	// simply does not show a stop button.
	_ = v
	view := VoiceView{
		Enabled:     cfg.VoiceEnabled,
		Pomodoro:    cfg.VoicePomodoro,
		Reminder:    cfg.VoiceReminder,
		Lines:       len(voice.AllLines),
		Dir:         filepath.Join(a.store.Dir(), speechDirName),
		BuiltInRole: voice.BuiltInRole,
		// "Never set" and "configured" are different, but the panel has something
		// honest to show in both cases.
		ActiveRole:          voice.BuiltInRoleID,
		ActiveRoleName:      voice.BuiltInRole,
		ActiveRoleIsBuiltIn: true,
	}
	// What is in effect, and how much of it the current character has actually
	// replaced. Read from the shared library, which means the pet picks up a change
	// made in the studio without a restart.
	//
	// The role and the count come from ONE LineStates snapshot rather than from a
	// separate ResolveCurrentRole call. Two reads would let the studio commit a
	// switch in between, and the panel would then describe a character that the rows
	// it just counted do not agree with - the same divergence this change removes.
	if st := a.studioStore(); st != nil {
		states := st.LineStates(voice.Available())
		for _, l := range states {
			if l.ChosenForActiveRole {
				view.Replaced++
			}
		}
		if len(states) > 0 {
			// Every row carries the character resolved for the whole snapshot, so
			// any row answers it and no two rows can disagree.
			row := states[0]
			if row.RoleError != "" {
				// Reported, not hidden: a stored character that does not exist is a
				// configuration fault, and the panel is where it can be explained.
				//
				// The built-in flag is cleared too. In this state NOTHING plays, so
				// leaving it set would have the panel claim the embedded voice is in
				// effect - a working-sounding answer to a broken configuration.
				view.RoleError = row.RoleError
				view.ActiveRole = ""
				view.ActiveRoleName = ""
				view.ActiveRoleIsBuiltIn = false
			} else if row.ActiveRoleName != "" {
				view.ActiveRole = row.ActiveRole
				view.ActiveRoleName = row.ActiveRoleName
				view.ActiveRoleIsBuiltIn = voice.IsBuiltInRole(row.ActiveRole)
			}
		}
	}
	return view
}

// VoiceSettingsInput carries the voice switches from the settings tab.
type VoiceSettingsInput struct {
	Enabled  bool `json:"enabled"`
	Pomodoro bool `json:"pomodoro"`
	Reminder bool `json:"reminder"`
}

// SaveVoiceSettings persists the fixed-voice switches.
//
// It returns the whole State, like every other settings method, because main.js
// assigns the result straight to its `state`.
func (a *App) SaveVoiceSettings(in VoiceSettingsInput) (State, error) {
	if _, err := a.store.UpdateConfig(func(c *store.Config) {
		c.VoiceEnabled = in.Enabled
		c.VoicePomodoro = in.Pomodoro
		c.VoiceReminder = in.Reminder
	}); err != nil {
		return State{}, err
	}
	return a.GetState()
}

// StopVoice silences whatever is playing, for the panel's stop button.
//
// It returns the whole State for the reason SaveVoiceSettings documents.
func (a *App) StopVoice() (State, error) {
	a.stopVoice()
	return a.GetState()
}

// TestVoice plays one line so the user can hear the voice before relying on it.
//
// It backs a "试听" button, and it deliberately ignores the per-notice switches: the
// point is to hear the voice, so a user who has the pomodoro announcement switched
// off must still be able to audition it. The master switch IS respected, because
// with speech off there is nothing to audition.
func (a *App) TestVoice() (State, error) {
	if !a.store.Config().VoiceEnabled {
		return State{}, fmt.Errorf("语音朗读未开启")
	}
	if err := a.voicePlayer().Play(voice.LineFocusEnd); err != nil {
		a.DebugLog("voice: audition: " + err.Error())
		return State{}, err
	}
	return a.GetState()
}
