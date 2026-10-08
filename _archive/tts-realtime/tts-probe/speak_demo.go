//go:build ignore

// Command speak_demo synthesises a few lines through PetPC's own speech pipeline
// and actually plays them out loud.
//
// It is the "can I hear it" check: everything else in this project verifies
// contracts, timings and file formats, but none of that answers whether the voice
// sounds right. This runs the real path end to end - the real client against the
// real GPT-SoVITS server, the real splitter, the real WinMM player - so what comes
// out of the speakers is exactly what the pet would say.
//
// Excluded from the build by the ignore tag.
//
// Usage (from the repo root, with the synthesis server already running):
//
//	go run .dev/tts-probe/speak_demo.go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/tts"
)

// desktopSpeaker is the production player, wired the same way app_tts.go wires it.
type desktopSpeaker struct{}

func (desktopSpeaker) PlayWAVFile(path string) error { return desktop.PlayWAVFile(path) }
func (desktopSpeaker) StopAudio()                    { desktop.StopAudio() }

func main() {
	settings := tts.Settings{
		BaseURL:       "http://127.0.0.1:9880",
		RefAudio:      `D:\GPT-SoVITS\models\kokomi\ref.wav`,
		PromptText:    "今天的天气非常好，阳光明媚，非常适合出去郊游。",
		Lang:          "zh",
		GPTWeights:    `D:\GPT-SoVITS\models\kokomi\gpt.ckpt`,
		SovitsWeights: `D:\GPT-SoVITS\models\kokomi\sovits.pth`,
		Timeout:       180 * time.Second,
	}

	ctx := context.Background()
	client := tts.NewClient()

	// A scratch cache so repeated demos are instant after the first run, and so
	// the user can hear the same lines again without paying the synthesis cost.
	cacheDir := filepath.Join(os.TempDir(), "petpc-tts-demo")
	cache, err := tts.NewCache(cacheDir, 200)
	if err != nil {
		fmt.Printf("cache unavailable (continuing without it): %v\n", err)
		cache = nil
	}

	lines := []string{
		"番茄钟结束了，休息一下吧。",
		"你好，我是你的桌宠。现在有三件事情今天到期，先从哪一件开始呢？",
	}

	for i, line := range lines {
		fmt.Printf("\n[%d/%d] %s\n", i+1, len(lines), line)

		pipeline := tts.NewPipeline(ctx, tts.Options{
			Settings:     settings,
			Synth:        client,
			Speaker:      desktopSpeaker{},
			Cache:        cache,
			TempDir:      cacheDir,
			MaxSentences: 3,
			ChunkRunes:   20,
			OnError: func(err error) {
				fmt.Printf("    (skipped: %v)\n", err)
			},
		})

		start := time.Now()
		pipeline.Add(line)
		pipeline.Flush()
		// Wait lets the queued sentences play out rather than cutting them off.
		pipeline.Wait()
		pipeline.Close()
		fmt.Printf("    用时 %.1fs\n", time.Since(start).Seconds())
	}

	fmt.Println("\n播放完毕。")
}
