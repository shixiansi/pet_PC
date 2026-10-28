package plugins

import (
	"strings"
	"testing"
)

// TestSurfaceSizeWarnings pins the three ways a `sizes` request is dropped, because all three are
// silent at runtime: the call succeeds and the log says `surface.blocks ok`. Both real-world
// occurrences were found from screenshots of a card with its last button cut off.
func TestSurfaceSizeWarnings(t *testing.T) {
	rects := []Rect{
		{W: 380, H: 260},            // no ceiling: cannot grow at all
		{W: 380, H: 260, MaxH: 480}, // may grow, up to 480
	}
	cases := []struct {
		name      string
		index     int
		requested int
		why       string
	}{
		{"no maxH", 0, 430, "没有 maxH"},
		{"above the ceiling", 1, 600, "超过声明的 maxH"},
		{"below the declared height", 1, 100, "低于声明高度"},
		{"an index that was never declared", 5, 430, "只声明了 2 块"},
	}
	for _, c := range cases {
		got := SurfaceSizeWarnings(rects, map[int]int{c.index: c.requested})
		if len(got) != 1 || !strings.Contains(got[0], c.why) {
			t.Errorf("%s: want one warning mentioning %q, got %v", c.name, c.why, got)
		}
	}

	// And the happy path must stay quiet: a request inside [h, maxH] is honoured, and a warning
	// there would teach people to ignore the column.
	if got := SurfaceSizeWarnings(rects, map[int]int{1: 400}); len(got) != 0 {
		t.Errorf("a valid request warned: %v", got)
	}
	if got := SurfaceSizeWarnings(rects, nil); len(got) != 0 {
		t.Errorf("an empty request warned: %v", got)
	}
	// A request exactly at the ceiling is valid, not "above" it - an off-by-one here would put a
	// WARN in the log for every well-behaved plugin that asks for its declared maximum.
	if got := SurfaceSizeWarnings(rects, map[int]int{1: 480}); len(got) != 0 {
		t.Errorf("the ceiling itself warned: %v", got)
	}
}
