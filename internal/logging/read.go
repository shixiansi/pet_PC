package logging

// The READING half of the log: what the control center's 日志 page shows.
//
// It lives here, beside the writer, for the reason dispatch.go gives about permission
// checks: two implementations of one format is two answers waiting to disagree - and
// here the disagreement is silent, because a page that parses the file slightly
// differently from the way it is written shows a shorter log rather than an error.
//
// It is also the only place that touches the file from OUTSIDE its writer. Two
// processes share the data directory (the pet and the control center), and the pet may
// rotate a file at the exact moment the page reads it. Every function here therefore
// treats "the file vanished between listing and reading" as an ordinary, reportable
// state rather than an error to propagate: the next poll two seconds later is the fix.

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Names of the two logs, in the order a merged view wants them.
//
// They are constants rather than string literals at each call site because THREE places
// have to agree on them: each program's Open, the 日志 page's reader, and the
// diagnostics report. A typo in one of the three produces an empty section, which reads
// as "nothing happened" rather than as a bug.
const (
	PetLog    = "pet"
	MarketLog = "market"
)

// DefaultLimit is how many entries a read returns when the caller does not say.
//
// A page, not an archive: 500 lines is several screens of scrolling and one poll's worth
// of text to serialize. Anything older is what the FILE is for, and the page says where
// it is.
const DefaultLimit = 500

// MaxLimit caps what a caller may ask for, so a page cannot ask a 40 MB file to become
// one string.
const MaxLimit = 2000

// maxReadBytes bounds how much of one file is read.
//
// The tail, not the whole file: the newest entries are the interesting ones, and a log
// that has just been rotated can be 2 MB - parsing all of it on every two-second poll
// would make the 日志 page compete with the pet for the disk.
const maxReadBytes = 512 << 10

// File describes one log file on disk, for the page's file list.
type File struct {
	// Name is the file's base name ("pet.log", "pet.log.1").
	Name string
	Path string
	// Bytes is 0 for a file that does not exist, which is the normal state of the
	// previous generation on a fresh install.
	Bytes int64
	// Modified is the local zero time when the file does not exist.
	Modified time.Time
	// Err explains an unreadable file ("Access is denied"), and is empty otherwise.
	Err string
}

// Exists reports whether the file is there.
func (f File) Exists() bool { return f.Bytes > 0 && f.Err == "" }

// Files lists name.log and name.log.1 for each of names, newest generation first.
//
// Missing files are LISTED with zero bytes rather than omitted: "pet.log 不存在" is
// information (the pet has never run on this machine, or its log directory is not
// writable), whereas a list that silently omits it says nothing at all. That is the same
// decision diagnostics.go makes about the data files.
func Files(dir string, names ...string) []File {
	out := make([]File, 0, len(names)*2)
	for _, name := range names {
		for _, suffix := range []string{".log", ".log.1"} {
			path := filepath.Join(dir, DirName, name+suffix)
			f := File{Name: name + suffix, Path: path}
			info, err := os.Stat(path)
			switch {
			case err == nil:
				f.Bytes = info.Size()
				f.Modified = info.ModTime()
			case !os.IsNotExist(err):
				f.Err = err.Error()
			}
			out = append(out, f)
		}
	}
	return out
}

// Query narrows a read.
type Query struct {
	// MinLevel drops entries quieter than this. LevelDebug keeps everything, which is
	// what the page's 「全部」 means.
	MinLevel Level
	// Scope keeps one subsystem; "" keeps them all.
	Scope string
	// Search keeps entries whose message or plugin id contains it, case-insensitively.
	// It is a substring over the TWO fields a user would type: an error word or a
	// plugin id.
	Search string
	// Limit is how many of the newest entries to return; 0 means DefaultLimit and
	// anything above MaxLimit is clamped to it.
	Limit int
}

// Read returns the newest entries from the named logs, oldest first.
//
// Oldest first is the file's own order and the order a reader expects a log to be
// story-shaped in; the page reverses it for display, because a monitoring view wants the
// latest at the top. Doing the reversal in the page rather than here keeps this function
// composable - "the last 20 warnings" and "the whole tail" differ only by Limit.
func Read(dir string, names []string, q Query) ([]Entry, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	search := strings.ToLower(strings.TrimSpace(q.Search))

	// Rotated generations first, so the merged stream is chronological: `.1` holds what
	// came BEFORE the live file, and reading them the other way would put a restart in
	// the middle of the file it restarted from.
	var all []Entry
	for _, name := range names {
		for _, generation := range []string{".log.1", ".log"} {
			path := filepath.Join(dir, DirName, name+generation)
			entries, err := readFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			all = append(all, entries...)
		}
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].At.Before(all[j].At) })

	out := make([]Entry, 0, limit)
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		e := all[i]
		if e.Level < q.MinLevel {
			continue
		}
		if q.Scope != "" && e.Scope != q.Scope {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(e.Message), search) &&
			!strings.Contains(strings.ToLower(e.Plugin), search) {
			continue
		}
		out = append(out, e)
	}
	// Collected newest-first for the Limit, restored to file order for the caller.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// readFile parses one log file's tail.
//
// The first line after a truncation is usually a fragment, and a line that does not
// parse is dropped by ParseLine rather than repaired - a log reader that invents entries
// is worse than one that loses half a line.
func readFile(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var data []byte
	if info.Size() > maxReadBytes {
		// Seek rather than read-and-slice: the point of the cap is not to touch the
		// bytes at the front at all.
		if _, err := f.Seek(info.Size()-maxReadBytes, io.SeekStart); err != nil {
			return nil, err
		}
		data, err = io.ReadAll(io.LimitReader(f, maxReadBytes))
	} else {
		data, err = io.ReadAll(io.LimitReader(f, maxReadBytes))
	}
	if err != nil {
		return nil, err
	}

	text := string(data)
	if info.Size() > maxReadBytes {
		// Cutting at a byte boundary can split the first rune AND the first line; both
		// are handled by dropping everything before the first newline, which is also
		// what makes the truncated case produce no half entry.
		if at := strings.IndexByte(text, '\n'); at >= 0 {
			text = text[at+1:]
		}
	}
	lines := strings.Split(text, "\n")
	out := make([]Entry, 0, len(lines))
	for _, line := range lines {
		if e, ok := ParseLine(line); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// Scopes lists the scopes present in entries, sorted, for a filter dropdown.
//
// Derived from what was READ rather than from a table of names: the set of scopes grows
// with every feature, and a hand-kept list would silently stop offering the new one -
// which is exactly the "filter cannot reach the thing I need" failure that makes people
// stop using a log viewer.
func Scopes(entries []Entry) []string {
	seen := make(map[string]bool, 8)
	for _, e := range entries {
		if e.Scope != "" {
			seen[e.Scope] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
