//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"PetPC/internal/desktop"
	"PetPC/internal/tts"
)

type sp struct{}

func (sp) PlayWAVFile(p string) error { return desktop.PlayWAVFile(p) }
func (sp) StopAudio()                 { desktop.StopAudio() }

// Mirrors what app_tts.go reads out of the config.
type cfg struct {
	Enabled  bool   `json:"ttsEnabled"`
	BaseURL  string `json:"ttsBaseUrl"`
	Char     string `json:"ttsCharacter"`
	GPT      string `json:"ttsGptWeights"`
	Sovits   string `json:"ttsSovitsWeights"`
	RefAudio string `json:"ttsRefAudioPath"`
	Prompt   string `json:"ttsPromptText"`
	Lang     string `json:"ttsLang"`
}

func main() {
	path := filepath.Join(os.Getenv("APPDATA"), "PetPC", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("read config:", err)
		os.Exit(1)
	}
	var c cfg
	if err := json.Unmarshal(raw, &c); err != nil {
		fmt.Println("parse config:", err)
		os.Exit(1)
	}

	fmt.Printf("配置: 角色=%q 启用=%v\n", c.Char, c.Enabled)
	if !c.Enabled {
		fmt.Println("语音未启用")
		os.Exit(1)
	}

	settings := tts.Settings{
		BaseURL: c.BaseURL, RefAudio: c.RefAudio, PromptText: c.Prompt,
		Lang: c.Lang, GPTWeights: c.GPT, SovitsWeights: c.Sovits,
		Timeout: 120 * time.Second,
	}

	ctx := context.Background()
	client := tts.NewClient()
	cache, _ := tts.NewCache(filepath.Join(os.TempDir(), "petpc-tts-demo"), 200)

	line := "番茄钟结束了，休息一下吧。"
	fmt.Printf("说: %s\n", line)

	p := tts.NewPipeline(ctx, tts.Options{
		Settings: settings, Synth: client, Speaker: sp{}, Cache: cache,
		TempDir:      filepath.Join(os.TempDir(), "petpc-tts-demo"),
		MaxSentences: 3, ChunkRunes: 20,
		OnError: func(err error) { fmt.Println("  错误:", err) },
	})
	start := time.Now()
	p.Add(line)
	p.Flush()
	p.Wait()
	p.Close()
	fmt.Printf("用时 %.1fs（含播放）\n", time.Since(start).Seconds())
	fmt.Println("OK")
}
