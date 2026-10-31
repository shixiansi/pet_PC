package main

// The manual, the fake host and the code are three copies of ONE contract (AGENTS.md §3.3).
// Nothing keeps them in step by itself: the count was written as 39, then 41, then 42, and each
// time it stayed behind for a while - the last time by two whole methods (plugin.log and
// storage.usage), which is enough for a reader to believe two capabilities do not exist.
//
// This guards the NUMBER only. It is the cheapest part of the contract and the one that goes
// stale silently; the payloads are guarded by the manual's own examples and by plugin-check.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestDocumentedMethodCountMatchesTheTable(t *testing.T) {
	want := len(pluginMethods)
	if want < 40 {
		t.Fatalf("pluginMethods has %d entries; the table looks truncated, so everything below would pass vacuously", want)
	}

	// "N 个方法" / "N 个能力方法" in prose. Written as a pattern so this test does not have to be
	// edited every time a sentence is reworded.
	countRe := regexp.MustCompile(`(\d+) 个(?:能力)?方法`)
	docs := []string{
		"README.md",
		"AGENTS.md",
		"docs/plugin-dev.md",
		filepath.Join("market", "README.md"),
		filepath.Join("market", "docs", "plugin-dev.md"),
		filepath.Join("market", "docs", "plugin-testkit", "README.md"),
	}
	found := 0
	for _, name := range docs {
		raw, err := os.ReadFile(name)
		if err != nil {
			// market/ is a separate checkout; the CI `contract` job checks it out.
			t.Logf("SKIP %s: %v", name, err)
			continue
		}
		for i, line := range strings.Split(string(raw), "\n") {
			for _, m := range countRe.FindAllStringSubmatch(line, -1) {
				found++
				if m[1] != strconv.Itoa(want) {
					t.Errorf("%s:%d says %s methods, the table has %d:\n  %s",
						name, i+1, m[1], want, strings.TrimSpace(line))
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no \"N 个方法\" line was found in any of the documents: this guard is looking at the wrong places")
	}

	// The fake host is the copy that drifts silently, because nothing type-checks it.
	spec := filepath.Join("market", "docs", "plugin-testkit", "fake-host.mjs")
	raw, err := os.ReadFile(spec)
	if err != nil {
		t.Logf("SKIP %s: %v", spec, err)
		return
	}
	block := regexp.MustCompile(`(?s)export const METHODS = \{(.*?)\n\};`).FindStringSubmatch(string(raw))
	if block == nil {
		t.Fatalf("%s: the METHODS block is not where this guard expects it", spec)
	}
	entries := regexp.MustCompile(`(?m)^\s{2}'[a-z]+\.[a-z.]+':`).FindAllString(block[1], -1)
	if len(entries) != want {
		t.Fatalf("fake-host.mjs lists %d methods, the host has %d - the two copies of the contract disagree",
			len(entries), want)
	}
}
