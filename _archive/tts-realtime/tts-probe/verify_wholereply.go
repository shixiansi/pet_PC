package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"PetPC/internal/store"
	"PetPC/internal/tts"
)

// Records the ORDER of synthesis and playback through the real Settings, so the
// whole-reply behaviour can be seen on the real online service rather than only
// against fakes.
type orderSynth struct {
	inner *tts.Client
	cfg   store.Config
	log   *[]string
}

func (o *orderSynth) Synthesize(ctx context.Context, s tts.Settings, text string) ([]byte, error) {
	*o.log = append(*o.log, fmt.Sprintf("synth  [%2d字] %s", len([]rune(text)), text))
	return o.inner.Synthesize(ctx, s, text)
}

type orderSpeaker struct{ log *[]string }

func (o *orderSpeaker) PlayWAVFile(path string) error {
	*o.log = append(*o.log, "PLAY   <-- 开始播放")
	return nil // do not actually make noise during the probe
}

func (o *orderSpeaker) StopAudio() {}

func main() {
	cfgPath := filepath.Join(os.Getenv("APPDATA"), "PetPC", "config.json")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		fmt.Println("FAIL read config:", err)
		os.Exit(1)
	}
	var c store.Config
	if err := json.Unmarshal(raw, &c); err != nil {
		fmt.Println("FAIL parse config:", err)
		os.Exit(1)
	}

	// A reply long enough to be split into several requests.
	reply := "今天是十月二十四日星期五，你一共有五件待办，其中两件今天到期。上午的复习计划还没有开始，下午三点有一个会议。"
	fmt.Printf("回复 %d 字，上限 %d 字/段\n\n", len([]rune(reply)), c.TTSChunkCap())

	settings := tts.Settings{
		BaseURL:    c.TTSActiveURL(),
		RefAudio:   c.TTSCharacter,
		GPTWeights: c.TTSCharacter,
		Lang:       c.TTSLang,
		Timeout:    60 * time.Second,
	}

	var log []string
	synth := &orderSynth{inner: tts.NewClient(), cfg: c, log: &log}
	spk := &orderSpeaker{log: &log}

	p := tts.NewPipeline(context.Background(), tts.Options{
		Settings:     settings,
		MaxSentences: c.TTSMaxSentences,
		ChunkRunes:   c.TTSChunkRunes,
		ChunkCap:     c.TTSChunkCap(),
		WholeReply:   true,
		Synth:        synth,
		Speaker:      spk,
		OnProgress: func(revealed string) {
			log = append(log, fmt.Sprintf("REVEAL (%d 字) %s", len([]rune(revealed)), revealed))
		},
		OnError: func(err error) { log = append(log, "ERR    "+err.Error()) },
	})

	start := time.Now()
	p.Add(reply)
	p.Flush()
	p.Wait()
	elapsed := time.Since(start)

	fmt.Println("事件顺序：")
	for _, l := range log {
		fmt.Println("  " + l)
	}
	fmt.Printf("\n总耗时 %.1fs\n", elapsed.Seconds())

	// The invariant: every synth must precede the first PLAY.
	firstPlay := -1
	lastSynth := -1
	for i, l := range log {
		if strings.HasPrefix(l, "PLAY") && firstPlay < 0 {
			firstPlay = i
		}
		if strings.HasPrefix(l, "synth") {
			lastSynth = i
		}
	}
	fmt.Println()
	if firstPlay >= 0 && lastSynth < firstPlay {
		fmt.Println("结论：全部合成完成后才开始播放 —— 整段等待生效 ✅")
	} else {
		fmt.Println("结论：播放时仍有合成未完成 ❌")
	}
	reveals := 0
	for _, l := range log {
		if strings.HasPrefix(l, "REVEAL") {
			reveals++
		}
	}
	fmt.Printf("文字 reveal 次数：%d（整段模式应为 1）\n", reveals)
}
