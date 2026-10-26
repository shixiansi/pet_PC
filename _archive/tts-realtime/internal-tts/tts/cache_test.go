package tts

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// baseSettings is a fully-populated Settings for cache-key tests.
func baseSettings() Settings {
	return Settings{
		BaseURL:    "http://127.0.0.1:9880",
		RefAudio:   `D:\voices\ref.wav`,
		PromptText: "参考文本",
		Lang:       "zh",
	}
}

// TestCacheRoundTrip is the basic contract: what goes in comes back byte-identical.
func TestCacheRoundTrip(t *testing.T) {
	c, err := NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	s := baseSettings()
	audio := makeWAV(0.2)

	if got := c.Get(s, "你好。"); got != nil {
		t.Fatalf("a fresh cache returned %d bytes", len(got))
	}
	if err := c.Put(s, "你好。", audio); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got := c.Get(s, "你好。")
	if string(got) != string(audio) {
		t.Fatalf("Get returned %d bytes, want %d", len(got), len(audio))
	}
}

// TestCacheKeyIncludesEveryAudioParameter is the correctness property that matters
// most: a character switch, a new reference clip, or a language change must NOT
// serve the previous voice's audio. Getting this wrong is especially bad because
// it looks like the settings are ignored.
func TestCacheKeyIncludesEveryAudioParameter(t *testing.T) {
	c, _ := NewCache(t.TempDir(), 10)
	base := baseSettings()
	audio := makeWAV(0.1)
	const text = "同一句话。"

	if err := c.Put(base, text, audio); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if c.Get(base, text) == nil {
		t.Fatal("the baseline entry is missing, so the variations below prove nothing")
	}

	variations := map[string]Settings{
		"a different base URL": func() Settings {
			v := base
			v.BaseURL = "http://127.0.0.1:9999"
			return v
		}(),
		"a different reference clip": func() Settings {
			v := base
			v.RefAudio = `D:\voices\other.wav`
			return v
		}(),
		"a different prompt text": func() Settings {
			v := base
			v.PromptText = "别的参考文本"
			return v
		}(),
		"a different language": func() Settings {
			v := base
			v.Lang = "ja"
			return v
		}(),
		"a different GPT checkpoint": func() Settings {
			v := base
			v.GPTWeights = "kokomi.ckpt"
			return v
		}(),
		"a different SoVITS checkpoint": func() Settings {
			v := base
			v.SovitsWeights = "kokomi.pth"
			return v
		}(),
	}
	for name, v := range variations {
		if got := c.Get(v, text); got != nil {
			t.Errorf("%s reused the cached audio; the user would hear the wrong voice", name)
		}
	}
}

// TestCacheKeyIsNotAmbiguous guards the field-separator choice. Without a
// separator between fields, ("ab","c") and ("a","bc") hash identically and one
// voice's audio would be served for another configuration.
func TestCacheKeyIsNotAmbiguous(t *testing.T) {
	a := Settings{BaseURL: "ab", RefAudio: "c", PromptText: "p", Lang: "zh"}
	b := Settings{BaseURL: "a", RefAudio: "bc", PromptText: "p", Lang: "zh"}
	same := baseSettings()

	c, _ := NewCache(t.TempDir(), 10)
	if err := c.Put(a, "文本。", makeWAV(0.05)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := c.Get(b, "文本。"); got != nil {
		t.Error("two different configurations collided onto one cache entry")
	}
	_ = same
}

// TestCacheKeyIsStable pins that the same request always maps to the same file,
// which is what makes the cache hit at all.
func TestCacheKeyIsStable(t *testing.T) {
	s := baseSettings()
	first := key(s, "你好。")
	for i := 0; i < 5; i++ {
		if got := key(s, "你好。"); got != first {
			t.Fatalf("key changed between calls: %q then %q", first, got)
		}
	}
	// Whitespace-only differences in the stored paths must not change the key,
	// because store.NormaliseTTS trims them; otherwise a config reload would
	// silently invalidate the whole cache.
	padded := baseSettings()
	padded.RefAudio = "  " + padded.RefAudio + "  "
	if key(padded, "你好。") != first {
		t.Error("untidy whitespace in the config changed the cache key")
	}
}

// TestCacheEvictsLeastRecentlyUsed is the policy that keeps the fixed phrases.
// The pet's daily lines are re-read constantly (each hit touches them); a one-off
// reply fragment is written once and never read again.
//
// The test models that asymmetry directly: the "frequent" line is read on every
// iteration, so it must outlive the one-off lines that were written later but
// never used again.
func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir, 1)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	c.maxBytes = 200 * 1024
	s := baseSettings()
	audio := makeWAV(0.9) // ~80KB, so only ~2 entries fit

	const frequent = "常用的一句话。"
	if err := c.Put(s, frequent, audio); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if c.Get(s, frequent) == nil {
		t.Fatal("the frequently used entry vanished immediately")
	}

	// Each iteration writes a fresh one-off line and re-reads the frequent one,
	// which is what a real repeat does.
	for i := 0; i < 6; i++ {
		time.Sleep(5 * time.Millisecond)
		text := "一次性的句子" + string(rune('A'+i)) + "。"
		if err := c.Put(s, text, audio); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
		if c.Get(s, frequent) == nil {
			t.Fatalf("the frequently used entry was evicted after write %d", i)
		}
	}

	if c.Size() > c.maxBytes {
		t.Errorf("cache is %d bytes, over its %d limit", c.Size(), c.maxBytes)
	}
	if c.Get(s, frequent) == nil {
		t.Error("the repeatedly used entry was evicted instead of a one-off one")
	}
}

// TestCacheEvictsWhenFullEvenWithoutReuse is the complement: with no reuse at all,
// the oldest entry must still be the one to go, and the newest must survive. This
// is what stops the directory growing without bound.
func TestCacheEvictsWhenFullEvenWithoutReuse(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewCache(dir, 1)
	c.maxBytes = 200 * 1024
	s := baseSettings()
	audio := makeWAV(0.9)

	var written []string
	for i := 0; i < 8; i++ {
		time.Sleep(5 * time.Millisecond)
		text := "句子" + string(rune('A'+i)) + "。"
		written = append(written, text)
		if err := c.Put(s, text, audio); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	if c.Size() > c.maxBytes {
		t.Errorf("cache is %d bytes, over its %d limit", c.Size(), c.maxBytes)
	}
	// The newest entry must always be present: a cache that evicted what was just
	// written would miss on every request and be pure overhead.
	newest := written[len(written)-1]
	if c.Get(s, newest) == nil {
		t.Error("the entry just written was already evicted")
	}
	// The oldest must be gone, or nothing was ever reclaimed.
	if c.Get(s, written[0]) != nil {
		t.Error("the oldest entry survived despite the cache being over budget")
	}
}

// TestCacheDisabledStoresNothing covers the maxMB=0 case, which a user with little
// disk left may choose. The feature still works, just without caching.
func TestCacheDisabledStoresNothing(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(filepath.Join(dir, "cache"), 0)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	if c.Enabled() {
		t.Fatal("a zero budget should disable the cache")
	}
	s := baseSettings()
	if err := c.Put(s, "你好。", makeWAV(0.1)); err != nil {
		t.Errorf("Put on a disabled cache returned %v, want nil", err)
	}
	if got := c.Get(s, "你好。"); got != nil {
		t.Error("a disabled cache returned audio")
	}
	if c.Size() != 0 {
		t.Errorf("Size = %d, want 0", c.Size())
	}
	// Clear must be safe too.
	if err := c.Clear(); err != nil {
		t.Errorf("Clear on a disabled cache returned %v", err)
	}
}

// TestCacheIgnoresEmptyAudio guards against caching a zero-byte file, which would
// later be "hit" and played as silence.
func TestCacheIgnoresEmptyAudio(t *testing.T) {
	c, _ := NewCache(t.TempDir(), 10)
	s := baseSettings()
	if err := c.Put(s, "你好。", nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := c.Get(s, "你好。"); got != nil {
		t.Error("a nil clip was cached and returned")
	}
}

// TestCacheTreatsCorruptEntryAsMiss covers a truncated file left by a crash. It
// must be treated as a miss and removed, so it cannot keep costing a failed read.
func TestCacheTreatsCorruptEntryAsMiss(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewCache(dir, 10)
	s := baseSettings()

	// An empty file is the shape a crash mid-write would leave behind.
	path := c.Path(s, "你好。")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := c.Get(s, "你好。"); got != nil {
		t.Error("an empty cache file was returned as audio")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the unusable cache file was not removed")
	}
}

// TestCacheClear removes everything the cache owns.
func TestCacheClear(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewCache(dir, 10)
	s := baseSettings()
	for i := 0; i < 5; i++ {
		if err := c.Put(s, "句子"+string(rune('A'+i))+"。", makeWAV(0.05)); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if c.Size() == 0 {
		t.Fatal("nothing was cached")
	}
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if c.Size() != 0 {
		t.Errorf("Size after Clear = %d, want 0", c.Size())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), cacheEntrySuffix) {
			t.Errorf("Clear left %s behind", e.Name())
		}
	}
}

// TestCacheIsSafeForConcurrentUse exercises the mutex. The pipeline writes from
// the synth goroutine while a settings panel may read the size.
func TestCacheIsSafeForConcurrentUse(t *testing.T) {
	c, _ := NewCache(t.TempDir(), 5)
	s := baseSettings()
	audio := makeWAV(0.05)

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := "并发句子" + string(rune('A'+i%8)) + "。"
			c.Put(s, text, audio)
			c.Get(s, text)
			c.Size()
		}(i)
	}
	wg.Wait()
	if c.Size() < 0 {
		t.Error("Size went negative")
	}
}

// TestCacheSizeCountsOnlyCacheEntries makes sure scratch files and stray files do
// not count against the budget.
func TestCacheSizeCountsOnlyCacheEntries(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewCache(dir, 10)
	s := baseSettings()
	c.Put(s, "你好。", makeWAV(0.05))
	before := c.Size()

	// A scratch clip and an unrelated file must not be counted.
	os.WriteFile(filepath.Join(dir, tempPrefix+"x.wav"), makeWAV(0.5), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644)

	c.invalidateSize()
	after := c.Size()
	if after != before {
		t.Errorf("Size changed from %d to %d after adding non-cache files", before, after)
	}
}

// --- WAV duration ----------------------------------------------------------

// TestWAVDurationReadsHeader is the basis for pacing playback, since winmm offers
// no completion callback.
func TestWAVDurationReadsHeader(t *testing.T) {
	for _, want := range []float64{0.1, 0.5, 2.0} {
		got, err := wavDuration(makeWAV(want))
		if err != nil {
			t.Fatalf("wavDuration(%v): %v", want, err)
		}
		// The header is written from an integer sample count, so allow one
		// sample's worth of slack.
		if diff := got - want; diff > 0.01 || diff < -0.01 {
			t.Errorf("wavDuration = %v, want ~%v", got, want)
		}
	}
}

// TestWAVDurationSkipsUnknownChunks covers the real-world file shape: an encoder
// (or Windows itself) often inserts a LIST/INFO chunk before the data chunk, so a
// parser that assumed a fixed 44-byte header would misread the duration.
func TestWAVDurationSkipsUnknownChunks(t *testing.T) {
	base := makeWAV(0.5)

	// Splice a LIST chunk in immediately after the "WAVE" tag.
	var out []byte
	out = append(out, base[:12]...)
	listBody := []byte("INFOIART\x04\x00\x00\x00test")
	var hdr [8]byte
	copy(hdr[0:4], "LIST")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(listBody)))
	out = append(out, hdr[:]...)
	out = append(out, listBody...)
	out = append(out, base[12:]...)
	// The RIFF size field is now stale, which a chunk-walking parser tolerates.

	got, err := wavDuration(out)
	if err != nil {
		t.Fatalf("wavDuration with a LIST chunk: %v", err)
	}
	if diff := got - 0.5; diff > 0.01 || diff < -0.01 {
		t.Errorf("duration = %v, want ~0.5", got)
	}
}

// TestWAVDurationRejectsGarbage pins the error path: a non-WAV must not report a
// zero duration, because the pipeline would then treat a real sentence as
// instantaneous and play the whole reply on top of itself.
func TestWAVDurationRejectsGarbage(t *testing.T) {
	cases := map[string][]byte{
		"empty":         {},
		"too short":     []byte("RIFF"),
		"wrong magic":   []byte("XXXX....WAVE"),
		"no fmt":        []byte("RIFF\x24\x00\x00\x00WAVEdata\x00\x00\x00\x00"),
		"no data chunk": func() []byte { return makeWAV(0.1)[:36] }(),
	}
	for name, data := range cases {
		if _, err := wavDuration(data); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestWAVDurationRecoversZeroByteRate covers a header some encoders emit with a
// zero byte rate. It is derivable from the other fields, so recovering is strictly
// better than rejecting an otherwise playable file.
func TestWAVDurationRecoversZeroByteRate(t *testing.T) {
	base := makeWAV(0.5)
	// Zero the byte-rate field (offset 28 in a canonical 44-byte header).
	broken := append([]byte(nil), base...)
	binary.LittleEndian.PutUint32(broken[28:32], 0)

	got, err := wavDuration(broken)
	if err != nil {
		t.Fatalf("wavDuration: %v", err)
	}
	if diff := got - 0.5; diff > 0.01 || diff < -0.01 {
		t.Errorf("duration = %v, want ~0.5", got)
	}
}

// TestWAVDurationToleratesTruncatedData covers a clip whose data chunk is shorter
// than its header claims, which a killed encoder can produce. The duration is
// computed from the DECLARED size, so this is mainly an assertion that the parser
// does not panic.
func TestWAVDurationToleratesTruncatedData(t *testing.T) {
	full := makeWAV(0.5)
	truncated := full[:len(full)-100]
	if _, err := wavDuration(truncated); err != nil {
		t.Logf("truncated data returned %v (acceptable: the file is genuinely short)", err)
	}
}

// TestWAVDurationExportedWrapper covers the exported entry point.
func TestWAVDurationExportedWrapper(t *testing.T) {
	if _, err := WAVDuration(makeWAV(0.25)); err != nil {
		t.Fatalf("WAVDuration: %v", err)
	}
}

// TestCachePathIsFilesystemSafe is a guard against using the reply text as a
// filename: reply text can contain any character, and Windows rejects several.
func TestCachePathIsFilesystemSafe(t *testing.T) {
	c, _ := NewCache(t.TempDir(), 10)
	s := baseSettings()
	hostile := `句子里有/斜杠\和:冒号*星号?问号"引号<尖括号>以及很长很长很长很长的文字。`
	path := c.Path(s, hostile)
	base := filepath.Base(path)
	for _, bad := range []string{"/", "\\", ":", "*", "?", `"`, "<", ">"} {
		if strings.Contains(base, bad) {
			t.Errorf("filename %q contains the illegal character %q", base, bad)
		}
	}
	if len(base) > 64 {
		t.Errorf("filename is %d chars; a hash name should be fixed-length", len(base))
	}
}
