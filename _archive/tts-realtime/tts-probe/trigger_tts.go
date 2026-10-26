//go:build ignore

// Command trigger_tts exercises PetPC's real speech path against the real
// synthesis server, end to end, using the same config the running app reads.
//
// Why this exists: the previous rounds verified the pipeline, the server and the
// config separately, and still shipped a build that made no sound - because the
// binary on disk predated the code. So this checks the three things together in
// ONE process, in the same order the app does them:
//
//	config.json (what the user set)  ->  Settings
//	Settings                         ->  client.Synthesize (weight switch + /tts)
//	the audio                        ->  played through the real WinMM player
//
// If this prints OK and audio comes out of the speakers, then the only remaining
// variable in the running app is the chat path itself.
//
// Excluded from the build by the ignore tag.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/tts"
)

type appConfig struct {
	Enabled   bool   `json:"ttsEnabled"`
	BaseURL   string `json:"ttsBaseUrl"`
	Backend   string `json:"ttsBackend"`
	OnlineURL string `json:"ttsOnlineUrl"`
	Char      string `json:"ttsCharacter"`
	GPT       string `json:"ttsGptWeights"`
	Sovits    string `json:"ttsSovitsWeights"`
	RefAudio  string `json:"ttsRefAudioPath"`
	Prompt    string `json:"ttsPromptText"`
	Lang      string `json:"ttsLang"`
	MaxSent   int    `json:"ttsMaxSentences"`
	Chunk     int    `json:"ttsChunkRunes"`
	Timeout   int    `json:"ttsTimeoutSec"`
}

// activeURL mirrors store.Config.TTSActiveURL: the online backend and the local
// one keep separate addresses, so a probe that always read ttsBaseUrl would test
// the wrong port whenever the online mode is selected.
func (c appConfig) activeURL() string {
	if c.Backend == "online" {
		if c.OnlineURL != "" {
			return strings.TrimRight(c.OnlineURL, "/")
		}
		return "http://127.0.0.1:9881"
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func main() {
	cfgPath := filepath.Join(os.Getenv("APPDATA"), "PetPC", "config.json")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		fmt.Println("FAIL read config:", err)
		os.Exit(1)
	}
	var c appConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		fmt.Println("FAIL parse config:", err)
		os.Exit(1)
	}

	fmt.Println("=== 1. 配置（与运行中的 app 同一份）===")
	fmt.Printf("  enabled=%v  backend=%s  character=%q  lang=%s\n",
		c.Enabled, c.Backend, c.Char, c.Lang)
	fmt.Printf("  active url=%s\n", c.activeURL())
	if c.Backend != "online" {
		fmt.Printf("  ref=%s\n", c.RefAudio)
		fmt.Printf("  promptText=%q  (空是正常的)\n", c.Prompt)
	}
	if !c.Enabled {
		fmt.Println("FAIL 语音未启用")
		os.Exit(1)
	}
	if c.MaxSent == 0 {
		c.MaxSent = 3
	}
	if c.Chunk == 0 {
		c.Chunk = 20
	}
	if c.Timeout == 0 {
		c.Timeout = 30
	}

	settings := tts.Settings{Timeout: time.Duration(c.Timeout) * time.Second}
	if c.Backend == "online" {
		// Mirrors app_tts.go: the online bridge takes the voice NAME as both the
		// reference and the GPT weight, and has no reference clip of its own.
		settings.BaseURL = c.activeURL()
		settings.RefAudio = c.Char
		settings.GPTWeights = c.Char
		settings.Lang = c.Lang
	} else {
		settings.BaseURL = c.activeURL()
		settings.RefAudio = c.RefAudio
		settings.PromptText = c.Prompt
		settings.Lang = c.Lang
		settings.GPTWeights = c.GPT
		settings.SovitsWeights = c.Sovits
	}

	ctx := context.Background()
	client := tts.NewClient()

	fmt.Println("=== 2. Ping ===")
	start := time.Now()
	if err := client.Ping(ctx, settings.BaseURL); err != nil {
		fmt.Println("FAIL Ping:", err)
		os.Exit(1)
	}
	fmt.Printf("  OK %dms\n", time.Since(start).Milliseconds())

	fmt.Println("=== 3. 合成 + 播放（走 PetPC 的 Pipeline）===")
	tmp := filepath.Join(os.TempDir(), "petpc-tts-e2e")
	cache, err := tts.NewCache(tmp, 200)
	if err != nil {
		fmt.Println("  (cache unavailable, continuing)")
		cache = nil
	}

	line := "你好，我是你的桌宠。现在有三件事情今天到期。"
	var played int
	p := tts.NewPipeline(ctx, tts.Options{
		Settings:     settings,
		Synth:        client,
		Speaker:      realSpeaker{count: &played},
		Cache:        cache,
		TempDir:      tmp,
		MaxSentences: c.MaxSent,
		ChunkRunes:   c.Chunk,
		OnError: func(err error) {
			fmt.Println("  pipeline error:", err)
		},
	})

	start = time.Now()
	p.Add(line)
	p.Flush()
	p.Wait()
	p.Close()
	elapsed := time.Since(start)

	if played == 0 {
		fmt.Println("FAIL 没有任何音频被播放")
		os.Exit(1)
	}
	fmt.Printf("  OK 播放了 %d 段，共 %.1fs（含播放时间）\n", played, elapsed.Seconds())
	fmt.Println()
	fmt.Println("结论：配置 -> 合成 -> 播放 三段全部打通。")
}

// realSpeaker counts calls and lets the production WinMM player actually sound.
type realSpeaker struct{ count *int }

func (s realSpeaker) PlayWAVFile(path string) error {
	if err := desktop.PlayWAVFile(path); err != nil {
		return err
	}
	*s.count++
	return nil
}

func (realSpeaker) StopAudio() { desktop.StopAudio() }
