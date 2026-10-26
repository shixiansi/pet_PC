//go:build windows

package desktop

import "golang.org/x/sys/windows"

// Sound names the system sounds the pet may play.
//
// They are Windows' own notification sounds, so the feature needs no audio files
// and automatically follows whatever scheme the user has chosen in Sound
// settings (including "no sounds").
const (
	SoundAsterisk    = "asterisk"
	SoundExclamation = "exclamation"
)

// MessageBeep's system-sound constants.
const (
	mbIconAsterisk    = 0x00000040
	mbIconExclamation = 0x00000030
)

// GetForegroundWindow is needed by IsForegroundFullscreen.
var procGetForegroundWindow = user32.NewProc("GetForegroundWindow")

var (
	user32Beep      = windows.NewLazySystemDLL("user32.dll")
	procMessageBeep = user32Beep.NewProc("MessageBeep")
)

// PlaySystemSound plays one of the system notification sounds.
//
// MessageBeep is used rather than PlaySound(SND_ALIAS) because the alias form
// hands the API a string that must stay alive for an asynchronous playback,
// whereas MessageBeep takes a constant and cannot be raced by the garbage
// collector. It also returns immediately, so a finishing phase never blocks the
// timer goroutine on audio.
//
// Failures are ignored: a phase ending is more important than the beep that
// announces it, and a machine with no sound scheme is not an error.
func PlaySystemSound(name string) error {
	var kind uintptr
	switch name {
	case SoundExclamation:
		kind = mbIconExclamation
	default:
		kind = mbIconAsterisk
	}
	procMessageBeep.Call(kind)
	return nil
}
