//go:build ignore

// Command e2e_real probes a REAL GPT-SoVITS server with the exact client code that
// ships in PetPC.
//
// It exists because everything in internal/tts has so far been tested against a
// fake server and against hand-written curl commands. Running the real client
// against the real api_v2.py is the only way to confirm the two actually agree:
// the field names, the /tts contract, the weight-switching GET endpoints, and the
// WAV the server returns being parseable by our own duration reader.
//
// Usage (from the repo root):
//
//	go run .dev/tts-probe/e2e_real.go
//
// It is excluded from the normal build by the ignore tag, so it can never end up
// in the product binary.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"PetPC/internal/tts"
)

func main() {
	baseURL := os.Getenv("TTS_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:9880"
	}

	settings := tts.Settings{
		BaseURL:       baseURL,
		RefAudio:      `D:\GPT-SoVITS\models\kokomi\ref.wav`,
		PromptText:    "今天的天气非常好，阳光明媚，非常适合出去郊游。",
		Lang:          "zh",
		GPTWeights:    `D:\GPT-SoVITS\models\kokomi\gpt.ckpt`,
		SovitsWeights: `D:\GPT-SoVITS\models\kokomi\sovits.pth`,
		Timeout:       120 * time.Second,
	}

	client := tts.NewClient()
	ctx := context.Background()

	fmt.Println("=== 1. Ping (PetPC 的 TestTTSConnection 用的就是这个) ===")
	start := time.Now()
	if err := client.Ping(ctx, baseURL); err != nil {
		fmt.Printf("  FAIL Ping: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  OK   %d ms\n", time.Since(start).Milliseconds())

	fmt.Println("=== 2. Synthesize（会先推送角色权重，再合成）===")
	texts := []string{
		"番茄钟结束了。",
		"你好，我是你的桌宠，现在有三件事情今天到期，先从哪一件开始呢？",
	}
	for _, text := range texts {
		start := time.Now()
		audio, err := client.Synthesize(ctx, settings, text)
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("  FAIL %q: %v\n", text, err)
			os.Exit(1)
		}
		// Parse the duration with our OWN reader: that confirms the bytes the
		// server produced are a WAV the playback path can actually schedule.
		seconds, err := tts.WAVDuration(audio)
		if err != nil {
			fmt.Printf("  FAIL duration for %q: %v\n", text, err)
			os.Exit(1)
		}
		rtf := elapsed.Seconds() / seconds
		fmt.Printf("  OK   %d bytes, %.2fs audio, %.0fms synth, RTF %.3f  %q\n",
			len(audio), seconds, elapsed.Seconds()*1000, rtf, text)
	}

	fmt.Println("=== 3. 缓存：同一句话第二次不应再请求服务端 ===")
	cache, err := tts.NewCache(os.TempDir()+`\petpc-tts-e2e`, 50)
	if err != nil {
		fmt.Printf("  FAIL NewCache: %v\n", err)
		os.Exit(1)
	}
	defer cache.Clear()

	if got := cache.Get(settings, texts[0]); got != nil {
		fmt.Println("  (缓存里已有上一轮的数据，先清空)")
		cache.Clear()
	}
	audio, err := client.Synthesize(ctx, settings, texts[0])
	if err != nil {
		fmt.Printf("  FAIL: %v\n", err)
		os.Exit(1)
	}
	if err := cache.Put(settings, texts[0], audio); err != nil {
		fmt.Printf("  FAIL Put: %v\n", err)
		os.Exit(1)
	}
	if got := cache.Get(settings, texts[0]); got == nil {
		fmt.Println("  FAIL 缓存未命中")
		os.Exit(1)
	}
	fmt.Printf("  OK   缓存命中，%d 字节\n", len(audio))

	fmt.Println("=== 4. 整条流水线（Pipeline：切句 → 合成 → 播放）===")
	// A speaker that reports instead of touching an audio device, so this can run
	// headless. The point is the plumbing, not the sound.
	var played int
	speaker := &countingSpeaker{count: &played}
	pipeline := tts.NewPipeline(ctx, tts.Options{
		Settings:     settings,
		Synth:        client,
		Speaker:      speaker,
		Cache:        cache,
		TempDir:      os.TempDir(),
		MaxSentences: 3,
		ChunkRunes:   20,
		OnError: func(err error) {
			fmt.Printf("  pipeline error: %v\n", err)
		},
	})
	pipeline.Add(texts[1])
	pipeline.Flush()
	pipeline.Wait()
	pipeline.Close()

	if played == 0 {
		fmt.Println("  FAIL 没有任何音频被播放")
		os.Exit(1)
	}
	fmt.Printf("  OK   播放了 %d 段\n", played)

	fmt.Println()
	fmt.Println("全部通过：PetPC 的客户端与真实 api_v2.py 契约一致。")
}

type countingSpeaker struct{ count *int }

func (s *countingSpeaker) PlayWAVFile(string) error { *s.count++; return nil }
func (s *countingSpeaker) StopAudio()               {}
