//go:build !windows

package desktop

// Sound names, mirrored from the Windows implementation.
const (
	SoundAsterisk    = "asterisk"
	SoundExclamation = "exclamation"
)

// PlaySystemSound is a no-op off Windows: there is no cross-platform equivalent
// worth pulling in a dependency for, and the pet is a Windows-first app.
func PlaySystemSound(string) error { return nil }
