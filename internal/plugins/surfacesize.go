package plugins

import "fmt"

// SurfaceSizeWarnings explains every requested block height that the host will NOT honour.
//
// # WHY THIS EXISTS
//
// `surface.blocks` with `sizes` is easy to call and easy to get wrong, and getting it wrong is
// SILENT: the call succeeds, the log line says `surface.blocks ok`, and the card keeps its old
// height with its last button cut off. That happened twice in a row on the pomodoro plugin - first
// because the declaration had no `maxH` (every request is ignored without it), then because the
// request was larger than `maxH` and got clamped. Both were diagnosed from screenshots, because
// nothing anywhere said "your request was dropped".
//
// So the host answers the question the plugin cannot answer itself: given these rectangles and
// these requests, which ones will not be honoured, and why. Pure, so it is testable without an open
// surface or a log file; the caller writes each string at WARN.
//
// The wording names the CAUSE, because the two causes have different fixes:
//   - no `maxH`      -> the declaration has to allow growth at all;
//   - above `maxH`   -> the content is genuinely taller than the declared ceiling (shrink it, or
//     raise `maxH`);
//   - bad index      -> the plugin is addressing a block it never declared.
func SurfaceSizeWarnings(rects []Rect, sizes map[int]int) []string {
	if len(sizes) == 0 {
		return nil
	}
	// Sorted-by-index output is not needed for correctness, but a stable order keeps the log
	// readable and the test simple.
	indexes := make([]int, 0, len(sizes))
	for i := range sizes {
		indexes = append(indexes, i)
	}
	for i := 1; i < len(indexes); i++ {
		for j := i; j > 0 && indexes[j] < indexes[j-1]; j-- {
			indexes[j], indexes[j-1] = indexes[j-1], indexes[j]
		}
	}

	var out []string
	for _, index := range indexes {
		want := sizes[index]
		if index < 0 || index >= len(rects) {
			out = append(out, fmt.Sprintf("surface.blocks 请求第 %d 块高 %dpx，但只声明了 %d 块：这个请求被忽略",
				index, want, len(rects)))
			continue
		}
		r := rects[index]
		declared := int(r.H)
		if r.MaxH <= 0 {
			out = append(out, fmt.Sprintf("surface.blocks 请求第 %d 块高 %dpx，但声明里没有 maxH：请求被忽略，仍是 %dpx（要长高必须在 manifest 里声明 maxH）",
				index, want, declared))
			continue
		}
		ceiling := int(r.MaxH)
		if want > ceiling {
			out = append(out, fmt.Sprintf("surface.blocks 请求第 %d 块高 %dpx，超过声明的 maxH=%dpx：被夹到 %dpx（内容比声明允许的更高）",
				index, want, ceiling, ceiling))
			continue
		}
		if want < declared {
			out = append(out, fmt.Sprintf("surface.blocks 请求第 %d 块高 %dpx，低于声明高度 %dpx：被夹到 %dpx",
				index, want, declared, declared))
		}
	}
	return out
}
