//go:build ignore

// Command wav_check reads a real WAV produced by the synthesis server and reports
// what PetPC's own duration parser makes of it, next to the truth read from the
// header by hand.
//
// This exists because the playback pipeline decides how long to wait for a clip
// from that number. If the parser is wrong the pet overlaps its own sentences (too
// short) or leaves a gap after every one (too long), and both are the kind of bug
// that is easy to misread as "the server is slow".
//
// The reason to check at all: api_v2.py writes 32kHz mono, not the 44.1kHz the
// benchmark script had assumed, so an assumption baked in anywhere has to be
// re-verified against a real file rather than reasoned about.
//
// Usage: go run .dev/tts-probe/wav_check.go <file.wav> [...]
package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"PetPC/internal/tts"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: wav_check <file.wav> [...]")
		os.Exit(1)
	}

	exitCode := 0
	for _, path := range os.Args[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("FAIL read %s: %v\n", path, err)
			exitCode = 1
			continue
		}

		// Ground truth, straight from the fields.
		sampleRate := binary.LittleEndian.Uint32(data[24:28])
		byteRate := binary.LittleEndian.Uint32(data[28:32])
		dataSize := binary.LittleEndian.Uint32(data[40:44])
		expected := float64(dataSize) / float64(byteRate)

		got, err := tts.WAVDuration(data)
		if err != nil {
			fmt.Printf("FAIL %s: parser error: %v\n", path, err)
			exitCode = 1
			continue
		}

		match := "OK  "
		diff := got - expected
		if diff > 0.01 || diff < -0.01 {
			match = "FAIL"
			exitCode = 1
		}
		fmt.Printf("%s %-28s bytes=%7d  sampleRate=%5d  parser=%.3fs  header=%.3fs  diff=%+.3fs\n",
			match, base(path), len(data), sampleRate, got, expected, diff)
	}
	os.Exit(exitCode)
}

func base(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '\\' || p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
