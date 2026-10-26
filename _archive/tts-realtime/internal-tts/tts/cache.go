package tts

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cache stores synthesised audio on disk, keyed by content.
//
// Why a cache is not optional here: on the measured hardware a sentence costs
// roughly its own length in wall time (docs/genshin-tts.md 9), so re-synthesising
// a line the pet has said before is the difference between instant and a
// multi-second wait. The fixed lines - the pomodoro notices, the reminder
// openers, "好的" - repeat constantly and hit every time.
//
// It keys on the CONTENT (text plus the voice parameters) rather than on any
// notion of a "line id", because that is what makes it correct without the
// caller having to know which lines are stable: the same sentence in the same
// voice is byte-identical work, and anything else is a miss.
//
// It is safe for concurrent use.
type Cache struct {
	dir string
	// maxBytes is the soft limit. Eviction runs after a write pushes the total
	// over it, so the directory can transiently exceed maxBytes by one file.
	maxBytes int64

	mu sync.Mutex
	// size is the cached total, kept incrementally so eviction does not have to
	// walk the directory on every write. -1 means "not computed yet".
	size int64
}

// cacheEntrySuffix is the extension used for cached audio.
//
// ".wav" and not a private extension, because the file is handed straight to the
// player and to the user if they go looking in the directory.
const cacheEntrySuffix = ".wav"

// isCacheEntry reports whether a filename is a cached clip rather than something
// else in the same directory.
//
// The suffix alone is NOT enough: the pipeline writes its scratch clips as
// "pettts-*.wav" into this same directory, and those also end in ".wav". Counting
// them would inflate the apparent cache size and trigger needless eviction, and
// evicting them while the player still has one open is a race the player cannot
// see. Cached entries are named by a 40-character SHA-1 hex digest, so requiring
// that shape separates the two schemes exactly.
func isCacheEntry(name string) bool {
	if !strings.HasSuffix(name, cacheEntrySuffix) {
		return false
	}
	if strings.HasPrefix(name, tempPrefix) {
		return false
	}
	stem := strings.TrimSuffix(name, cacheEntrySuffix)
	if len(stem) != sha1HexLen {
		return false
	}
	for _, r := range stem {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

// sha1HexLen is the length of a hex-encoded SHA-1 digest, which is how cache
// entries are named.
const sha1HexLen = 40

// NewCache opens (creating if needed) a cache in dir.
//
// maxMB of zero or less disables the cache entirely rather than erroring: a user
// with very little disk left has a real reason to want that, and the feature
// still works without it (every sentence is simply synthesised again).
func NewCache(dir string, maxMB int) (*Cache, error) {
	c := &Cache{dir: dir, maxBytes: int64(maxMB) * 1024 * 1024, size: -1}
	if c.maxBytes <= 0 {
		return c, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("tts: 创建音频缓存目录失败：%w", err)
	}
	return c, nil
}

// Enabled reports whether the cache will store anything.
func (c *Cache) Enabled() bool { return c != nil && c.maxBytes > 0 && c.dir != "" }

// key derives the cache key for one synthesis request.
//
// Every parameter that changes the audio is folded in, including the reference
// audio and the language. Omitting any of them would let a character switch or a
// reference-clip change serve the previous voice's audio under the new
// configuration - the user would hear the wrong character and the settings would
// look broken.
//
// The key is a hash rather than the text itself so that the filename is a fixed,
// filesystem-safe length: reply text can contain any character at all, and
// Windows rejects several of them in paths.
func key(s Settings, text string) string {
	h := sha1.New()
	// A NUL separates the fields so that concatenation cannot be ambiguous
	// ("ab"+"c" must not collide with "a"+"bc").
	fields := []string{
		strings.TrimSpace(text),
		strings.TrimSpace(s.BaseURL),
		strings.TrimSpace(s.RefAudio),
		strings.TrimSpace(s.PromptText),
		strings.TrimSpace(s.Lang),
		strings.TrimSpace(s.GPTWeights),
		strings.TrimSpace(s.SovitsWeights),
	}
	for _, f := range fields {
		h.Write([]byte(f))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Path returns the file a key maps to.
func (c *Cache) Path(s Settings, text string) string {
	return filepath.Join(c.dir, key(s, text)+cacheEntrySuffix)
}

// Get returns the cached audio for a request, or nil when there is none.
//
// A read error is reported as a miss rather than as a failure: the audio is
// regenerable, so the right response to an unreadable cache file is to synthesise
// again, not to refuse to speak. The unreadable file is removed so it cannot keep
// costing a failed stat on every later request.
func (c *Cache) Get(s Settings, text string) []byte {
	if !c.Enabled() {
		return nil
	}
	path := c.Path(s, text)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			os.Remove(path)
			c.invalidateSize()
		}
		return nil
	}
	if len(data) == 0 {
		os.Remove(path)
		c.invalidateSize()
		return nil
	}
	// Touch the file so the LRU ordering reflects this use. The mtime is what
	// Put's eviction sorts on, so without this a line that is used constantly but
	// written once would look like the oldest thing in the directory.
	now := time.Now()
	os.Chtimes(path, now, now)
	return data
}

// Put stores audio for a request, then evicts if the cache is over its limit.
//
// A write failure is returned but is never fatal at the call site: the audio is
// already in hand and about to be played, so a full or read-only disk should cost
// the user a slower next sentence, not this one.
func (c *Cache) Put(s Settings, text string, audio []byte) error {
	if !c.Enabled() || len(audio) == 0 {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	path := c.Path(s, text)

	// Written to a temp file and renamed so a crash mid-write cannot leave a
	// truncated file that would later be served as valid audio (a click or a
	// truncated sentence). Same reasoning as store.writeJSON.
	tmp, err := os.CreateTemp(c.dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if _, err := tmp.Write(audio); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}

	// The rename above already put the file on disk, so the running total has to
	// be advanced carefully:
	//
	//   - On the first write the total is unknown, and computeSizeLocked now
	//     already COUNTS the file just written, so adding len(audio) would
	//     double-count it.
	//   - Afterwards the total is known, so the new file has to be added
	//     explicitly - otherwise `over` stays false forever and the directory
	//     grows without bound.
	//
	// Overwriting an existing key can drift the total by one file's size (the
	// audio is regenerated, so its length may differ). That is self-correcting:
	// evict recomputes the total from disk, and it runs as soon as the estimate
	// crosses the limit.
	c.mu.Lock()
	if c.size < 0 {
		c.size = c.computeSizeLocked()
	} else {
		c.size += int64(len(audio))
	}
	over := c.size > c.maxBytes
	c.mu.Unlock()

	if over {
		c.evict()
	}
	return nil
}

// evict removes the least recently used entries until the cache is back under
// its limit.
//
// LRU by modification time is the right policy for this data because the value of
// a cached line is exactly how often it recurs: the fixed phrases are touched
// constantly and stay, while one-off reply fragments age out. The alternative - a
// size-based or random policy - would just as happily discard the twelve lines
// the pet says every day.
func (c *Cache) evict() {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type item struct {
		path  string
		size  int64
		mtime time.Time
	}
	var items []item
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Leftover temp files from a crash are not cache entries; drop them.
		if strings.HasPrefix(name, ".tmp-") {
			os.Remove(filepath.Join(c.dir, name))
			continue
		}
		if !isCacheEntry(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{
			path:  filepath.Join(c.dir, name),
			size:  info.Size(),
			mtime: info.ModTime(),
		})
		total += info.Size()
	}

	c.size = total
	if total <= c.maxBytes {
		return
	}

	// Oldest first.
	sort.Slice(items, func(i, j int) bool { return items[i].mtime.Before(items[j].mtime) })
	for _, it := range items {
		if total <= c.maxBytes {
			break
		}
		if err := os.Remove(it.path); err != nil {
			continue
		}
		total -= it.size
	}
	c.size = total
}

// Clear removes every cached entry. The settings panel exposes it so a user can
// reclaim the space without hunting for the directory.
func (c *Cache) Clear() error {
	if c == nil || c.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var firstErr error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(c.dir, e.Name())); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	c.invalidateSize()
	return firstErr
}

// Size returns the current cached byte total, recomputing it from disk the first
// time.
func (c *Cache) Size() int64 {
	if !c.Enabled() {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.size < 0 {
		c.size = c.computeSizeLocked()
	}
	return c.size
}

// invalidateSize marks the cached total as unknown so the next Size or Put
// recomputes it.
func (c *Cache) invalidateSize() {
	c.mu.Lock()
	c.size = -1
	c.mu.Unlock()
}

// computeSizeLocked sums the cache directory. The caller must hold c.mu.
func (c *Cache) computeSizeLocked() int64 {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() || !isCacheEntry(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}
