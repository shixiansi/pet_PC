//go:build ignore

// Command check_gate reproduces shouldSpeak's decision against the LIVE config file
// that the running app reads.
//
// The running app logged "tts: enabled=true configured=true" at startup and then
// never logged anything from the speech path, which means startSpeech returned nil
// on every request. That function's only early exit is shouldSpeak, so this walks
// the same predicate against the same file to find which clause fails.
//
// It deliberately does not import app_tts.go (package main cannot be imported);
// instead it re-derives the gate from store.Config, which is where the predicate
// actually lives.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"PetPC/internal/store"
)

func main() {
	path := filepath.Join(os.Getenv("APPDATA"), "PetPC", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("read:", err)
		os.Exit(1)
	}

	// Decode exactly the way the app does: on top of DefaultConfig, then Normalise.
	cfg := store.DefaultConfig()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		fmt.Println("unmarshal:", err)
		os.Exit(1)
	}
	cfg.Normalise()

	fmt.Println("=== 生效后的相关字段 ===")
	fmt.Printf("  TTSEnabled          = %v\n", cfg.TTSEnabled)
	fmt.Printf("  TTSRefAudioPath     = %q\n", cfg.TTSRefAudioPath)
	fmt.Printf("  TTSPromptText       = %q\n", cfg.TTSPromptText)
	fmt.Printf("  TTSAutoReadChat     = %v\n", cfg.TTSAutoReadChat)
	fmt.Printf("  TTSAutoReadProactive= %v\n", cfg.TTSAutoReadProactive)

	fmt.Println("=== shouldSpeak 的判定链（chat）===")
	ready := cfg.TTSReady()
	fmt.Printf("  TTSConfigured()     = %v\n", cfg.TTSConfigured())
	fmt.Printf("  TTSReady()          = %v\n", ready)
	speak := ready && cfg.TTSAutoReadChat
	fmt.Printf("  shouldSpeak(chat)   = %v\n", speak)

	// Also confirm the reference file really exists at the configured path.
	if cfg.TTSRefAudioPath != "" {
		if _, err := os.Stat(cfg.TTSRefAudioPath); err != nil {
			fmt.Printf("  ⚠️  参考音频不存在: %v\n", err)
		} else {
			fmt.Println("  参考音频存在 ✓")
		}
	}

	fmt.Println()
	if speak {
		fmt.Println("判定：应该朗读。若仍无声，问题在 startSpeech 之后。")
	} else {
		fmt.Println("判定：不该朗读 —— 上面第一条为 false 的即是原因。")
	}

	// Print the raw JSON keys too, in case the file has a key the struct does not
	// map (a typo in the key name would silently keep the default value).
	var m map[string]any
	json.Unmarshal(raw, &m)
	var found []string
	for k := range m {
		if strings.HasPrefix(k, "tts") {
			found = append(found, k)
		}
	}
	fmt.Printf("\nconfig.json 里的 tts* 键: %v\n", found)
}
