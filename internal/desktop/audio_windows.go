//go:build windows

package desktop

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Audio playback flags for winmm's PlaySound.
//
// SND_FILENAME (not SND_MEMORY) is the load-bearing choice here, and it is the
// same class of problem the comment on PlaySystemSound describes: with
// SND_MEMORY the caller's byte slice must stay alive for the whole asynchronous
// playback, and Go's garbage collector has no idea the OS is still reading it. A
// collection mid-sentence produces silence or a crash that reproduces roughly
// once in a hundred runs. Reading from a file sidesteps the lifetime question
// entirely, and comes with a cache for free.
const (
	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndFilename  = 0x00020000
)

var (
	winmm          = windows.NewLazySystemDLL("winmm.dll")
	procPlaySoundW = winmm.NewProc("PlaySoundW")
)

// PlayWAVFile starts playing a WAV file asynchronously.
//
// It returns as soon as playback has started, not when it finishes: the caller
// is a pipeline that has the next sentence to synthesise, and blocking here would
// serialise synthesis behind playback. The duration is available separately (see
// tts.WAVDuration) because winmm offers no completion callback, so waiting for a
// clip to end means waiting out its length.
//
// Starting a new sound implicitly stops the previous one - winmm plays one sound
// per process - which is exactly the interruption behaviour this feature wants,
// but it means callers must not assume two calls can overlap.
func PlayWAVFile(path string) error {
	if path == "" {
		return fmt.Errorf("desktop: 音频路径为空")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("desktop: 音频文件不可用：%w", err)
	}
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("desktop: 路径无法转换：%w", err)
	}
	ret, _, callErr := procPlaySoundW.Call(
		uintptr(unsafe.Pointer(ptr)),
		0,
		uintptr(sndAsync|sndFilename|sndNoDefault),
	)
	if ret == 0 {
		// PlaySound's failure detail is not available through GetLastError in a
		// useful form; the boolean is all it gives.
		return fmt.Errorf("desktop: 无法播放音频（%v）", callErr)
	}
	return nil
}

// StopAudio stops any sound currently playing.
//
// Passing NULL with no flags is winmm's documented "stop everything" form. It
// returns immediately, which is what makes an audible interruption feel instant
// rather than merely fast (measured at ~4ms).
//
// A failure is not reported: stopping a sound that is not playing is not an
// error, and there is nothing the caller could do about a real failure anyway.
func StopAudio() {
	procPlaySoundW.Call(0, 0, 0)
}
