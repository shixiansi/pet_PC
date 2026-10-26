//go:build !windows

package desktop

import "fmt"

// PlayWAVFile is a stub off Windows: there is no cross-platform equivalent worth
// pulling in a dependency for, and the pet is a Windows-first app (see
// sound_other.go, which makes the same choice for the notification beep).
//
// It returns an error rather than succeeding silently so that the speech layer
// fails loudly on a platform it does not support, instead of appearing to speak
// and producing nothing.
func PlayWAVFile(string) error {
	return fmt.Errorf("desktop: 音频播放只在 Windows 上实现")
}

// StopAudio is a no-op off Windows.
func StopAudio() {}
