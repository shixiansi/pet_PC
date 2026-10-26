// Command cpuid prints the CPU features that decide how fast CPU-side neural
// inference can be. It exists because docs/genshin-tts.md makes a claim about
// AVX-512 on this machine, and that claim decides whether CPU inference is
// viable - so it must be measured, not assumed.
//
// Tiger Lake (11th-gen mobile) is the specific case in question: unlike Ice Lake
// and Rocket Lake, Intel fused AVX-512 off on Tiger Lake client parts, so an
// "11th gen" label alone does not answer it.
package main

import (
	"fmt"

	"golang.org/x/sys/cpu"
)

func main() {
	x := cpu.X86

	type row struct {
		name string
		has  bool
	}
	rows := []row{
		{"SSE2", x.HasSSE2},
		{"SSE4.2", x.HasSSE42},
		{"AVX", x.HasAVX},
		{"AVX2", x.HasAVX2},
		{"FMA", x.HasFMA},
		{"AVX512F", x.HasAVX512F},
		{"AVX512BW", x.HasAVX512BW},
		{"AVX512CD", x.HasAVX512CD},
		{"AVX512DQ", x.HasAVX512DQ},
		{"AVX512VL", x.HasAVX512VL},
		{"AVX512VNNI", x.HasAVX512VNNI},
		{"AVX512VBMI", x.HasAVX512VBMI},
		{"AVX512BF16", x.HasAVX512BF16},
		{"AVXVNNI", x.HasAVXVNNI},
		{"AMXTILE", x.HasAMXTile},
		{"AMXBF16", x.HasAMXBF16},
		{"BMI2", x.HasBMI2},
		{"OSXSAVE", x.HasOSXSAVE},
	}
	for _, r := range rows {
		fmt.Printf("%-12s = %v\n", r.name, r.has)
	}

	switch {
	case x.HasAVX512F && x.HasAVX512VNNI:
		fmt.Println("verdict: AVX-512 + VNNI present (best case for CPU inference)")
	case x.HasAVX512F:
		fmt.Println("verdict: AVX-512 present, no VNNI")
	case x.HasAVX2 && x.HasFMA:
		fmt.Println("verdict: AVX2 + FMA only (AVX-512 unavailable on this part)")
	default:
		fmt.Println("verdict: no AVX2 - CPU inference will be very slow")
	}
}
