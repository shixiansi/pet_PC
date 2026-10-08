package main

import (
	"fmt"

	"PetPC/internal/store"
	"PetPC/internal/tts"
)

// Shows the actual chunks the online backend would send, so the effect of the cap
// can be eyeballed rather than inferred from tests.
func main() {
	limit := store.TTSOnlineMaxChunkRunes
	fmt.Printf("online cap = %d runes\n\n", limit)

	samples := []string{
		"好的，我记下了。",
		"今天天气不错，我们去公园散步，顺便买点水果，晚上回来做饭。",
		"这是一个很长的从句它一直都没有出现任何标点符号所以只能靠长度来切分",
		"专注时间结束了，起来活动一下吧。",
		"你一共有五件待办，其中两件今天到期，上午的复习计划还没有开始，下午三点有一个会议。",
	}

	for _, text := range samples {
		s := tts.NewSplitterCapped(limit, limit)
		chunks := append(s.Add(text), s.Flush()...)
		fmt.Printf("原文 (%d 字): %s\n", len([]rune(text)), text)
		for i, c := range chunks {
			mark := " "
			if i == len(chunks)-1 {
				mark = "└"
			}
			fmt.Printf("  %s [%2d 字] %s\n", mark, len([]rune(c)), c)
		}
		fmt.Println()
	}
}
