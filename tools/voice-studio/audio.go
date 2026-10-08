package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"PetPC/internal/desktop"
)

// Audio playback for the studio.
//
// The studio needs its own player rather than borrowing the pet's: they are separate
// processes, and winmm plays one sound per PROCESS, so the pet's player cannot be
// reached from here at all. Sharing internal/desktop's implementation is what keeps
// the two consistent.

// playFile plays a WAV file, replacing anything already playing.
//
// It returns once playback has STARTED, not when it ends - winmm offers no
// completion callback, so "is it still talking" is not answerable and the UI treats
// the indicator as a hint rather than a fact.
func playFile(path string) error {
	if path == "" {
		return fmt.Errorf("没有可播放的音频")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("音频文件不存在：%w", err)
	}
	return desktop.PlayWAVFile(path)
}

// stopAudio silences whatever is playing.
func stopAudio() { desktop.StopAudio() }

// writeTempClip materialises embedded clip data to a file and returns its path.
//
// Played from a FILE rather than from memory because winmm's SND_MEMORY form
// requires the caller's buffer to stay alive for the whole asynchronous playback
// with the garbage collector unaware of it - a collection mid-sentence produces
// silence or a crash roughly one run in a hundred. internal/voice/play.go documents
// the same reasoning for the pet's own clips.
//
// The name carries a content hash so a rebuild with re-rendered clips cannot play a
// stale file, and the write is skipped when it is already there.
func writeTempClip(dir, name string, data []byte) (string, error) {
	sum := sha1.Sum(data)
	clipDir := filepath.Join(dir, "voice", "embedded")
	path := filepath.Join(clipDir, fmt.Sprintf("%s-%s.wav", name, hex.EncodeToString(sum[:6])))

	if fi, err := os.Stat(path); err == nil && fi.Size() == int64(len(data)) {
		return path, nil
	}
	if err := os.MkdirAll(clipDir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建目录：%w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", fmt.Errorf("无法写入音频：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("无法保存音频：%w", err)
	}
	return path, nil
}
