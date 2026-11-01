package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

/*
 * Occupancy: how much space each plugin takes, and what the user can do about it.
 *
 * # WHY THIS EXISTS INSTEAD OF A CEILING
 *
 * The host used to cap every plugin's private directory at 64 MiB and refuse writes past it
 * (MaxPluginDataBytes, deleted 2026-11-01). The cap did not prevent anything; it created a
 * problem. A plugin that legitimately needed room - the qq bot's sticker library - was refused
 * the write that would have saved its own bookkeeping, so it had to invent an eviction policy.
 * Every policy it invented was wrong in the same way: it decided which of the user's pictures
 * were "cache". The user lost 318 stickers (458 MiB) and then, worse, 318 recognition records.
 *
 * So the ceiling is gone and this replaces it: **store what you need, and let the user SEE what
 * is stored.** A number a person can look at and act on is worth more than a threshold the
 * program enforces behind their back.
 *
 * # THE THREE GRADES, AND WHY THE HOST CANNOT DECIDE THEM ITSELF
 *
 * The host can measure bytes; it cannot know whether a given folder is a user's data or a
 * rebuildable cache - that is knowledge the PLUGIN has (it knows its media/bytes are原图 that
 * expired, and its video/tmp is a下载中间产物). So:
 *
 *   - the plugin reports its own grading through `storage.usage`'s `folders` plus the
 *     `occupancy` hint it may write once (see OccupancyHintKey);
 *   - the host fills in everything else as `unknown`, which is honest: "there are 12 MiB here
 *     and I do not know what they are" is a better sentence than a wrong guess;
 *   - **only `evictable` folders are offered with a clean button**, and even then the click is
 *     the user's (see App.CleanPluginFolder). `user-data` never gets one.
 */

// Occupancy grades. `unknown` is the default and the honest one: the host does not know.
const (
	// GradeEvictable is a folder the plugin itself produces and can rebuild.
	GradeEvictable = "evictable"
	// GradeUserData is something the user would lose permanently.
	GradeUserData = "user-data"
	// GradeUnknown is "measured, not classified".
	GradeUnknown = "unknown"
)

// OccupancyHintKey is where a plugin may store its OWN grading of its folders.
//
// A key rather than a new capability: the grading is plugin knowledge that changes with the
// plugin's design, and adding a method for it would mean a permission, a manifest field and a
// version negotiation for what is ultimately one small JSON object. The shape is
// `[{"key":"media/video/tmp","grade":"evictable","label":"下载中间产物"}, …]`.
const OccupancyHintKey = "occupancy/hints"

// FolderOccupancy is one folder of one plugin's private directory.
type FolderOccupancy struct {
	// Key is the folder's relative path ("" for files at the root).
	Key string `json:"key"`
	// Label is the plugin's own words for it, when it supplied any.
	Label string `json:"label,omitempty"`
	// Grade is one of GradeEvictable / GradeUserData / GradeUnknown.
	Grade string `json:"grade"`
	// Bytes is what that folder holds right now.
	Bytes int64 `json:"bytes"`
	// Files is how many files it holds.
	Files int `json:"files"`
}

// PluginOccupancy is one plugin's whole private directory, measured and graded.
type PluginOccupancy struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
	// Folders is sorted by bytes descending: the biggest thing is the first thing a user needs
	// to see.
	Folders []FolderOccupancy `json:"folders"`
	// EvictableBytes is the part a "clean" button may remove. Zero means no button.
	EvictableBytes int64 `json:"evictableBytes"`
}

// MeasureOccupancy walks one plugin's private directory and grades it.
//
// `hints` is the plugin's own grading (already decoded from OccupancyHintKey); anything it does
// not name comes back `unknown`. A grader that guessed would be worse than none: marking a user's
// sticker folder `evictable` is exactly the mistake this file exists to prevent.
func MeasureOccupancy(dir, id, name string, hints map[string]folderHint) PluginOccupancy {
	out := PluginOccupancy{ID: id, Name: name, Folders: []FolderOccupancy{}}
	byKey := map[string]*FolderOccupancy{}
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		// Group by the folder the file lives in (its parent), not by every level: a user wants
		// "media/stickers/bytes 23 MiB", not one row per sticker.
		folder := filepath.ToSlash(filepath.Dir(rel))
		if folder == "." {
			folder = ""
		}
		slot, ok := byKey[folder]
		if !ok {
			slot = &FolderOccupancy{Key: folder, Grade: GradeUnknown}
			byKey[folder] = slot
		}
		slot.Bytes += info.Size()
		slot.Files++
		out.Bytes += info.Size()
		out.Files++
		return nil
	})

	// Apply the plugin's own grading. A folder it called evictable is evictable; anything else
	// stays unknown unless it explicitly says user-data.
	for key, hint := range hints {
		normalized := filepath.ToSlash(strings.TrimSuffix(key, "/"))
		if slot, ok := byKey[normalized]; ok {
			slot.Label = hint.Label
			switch hint.Grade {
			case GradeEvictable, GradeUserData:
				slot.Grade = hint.Grade
			}
		}
	}

	for _, slot := range byKey {
		out.Folders = append(out.Folders, *slot)
		if slot.Grade == GradeEvictable {
			out.EvictableBytes += slot.Bytes
		}
	}
	sort.Slice(out.Folders, func(i, j int) bool {
		if out.Folders[i].Bytes != out.Folders[j].Bytes {
			return out.Folders[i].Bytes > out.Folders[j].Bytes
		}
		return out.Folders[i].Key < out.Folders[j].Key
	})
	return out
}

// folderHint is one row of the plugin's self-grading.
type folderHint struct {
	Label string `json:"label"`
	Grade string `json:"grade"`
}

// ReadOccupancyHints decodes the plugin's self-grading, tolerating every failure.
//
// A missing hint file is the normal case (most plugins do not grade yet), and a corrupt one must
// not cost the page its occupancy view - both come back as an empty map, i.e. "everything unknown".
func ReadOccupancyHints(dir string) map[string]folderHint {
	out := map[string]folderHint{}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(OccupancyHintKey)))
	if err != nil || len(data) == 0 {
		return out
	}
	rows := []struct {
		Key   string `json:"key"`
		Label string `json:"label"`
		Grade string `json:"grade"`
	}{}
	if err := json.Unmarshal(data, &rows); err != nil {
		return out
	}
	for _, row := range rows {
		if row.Grade != GradeEvictable && row.Grade != GradeUserData {
			continue // an unknown grade is not a grade
		}
		out[strings.TrimSuffix(strings.TrimSpace(row.Key), "/")] = folderHint{Label: row.Label, Grade: row.Grade}
	}
	return out
}
