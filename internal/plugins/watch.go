package plugins

// The pure half of "tell a plugin when something of its own changed".
//
// # WHY THIS LIVES HERE AND NOT IN A HOST
//
// Both programs mount plugin frames: the pet's column and the control center's 插件 page are the
// same documents (AGENTS.md §3.3). So a plugin's own data changing is a fact both of them have to
// notice, and until 2026-11-01 only the pet did - `config.changed` and `storage.changed` were
// emitted by one of the two hosts. A workshop page could therefore only POLL, which is exactly
// what todo (2-second `storage.get`) and ast-coder (5-second `config.get`) did.
//
// Two watchers over the same idea must not disagree about what "changed" means, and the part
// worth agreeing on is a pure function of two snapshots. So the snapshots and the comparison live
// here, and each host keeps the loop that walks its own manager, plus its own emit channel
// (the pet's `emitPluginEvent`, the market's `pet:plugin-event`).

import (
	"os"
	"path/filepath"
	"sort"
)

// ConfigFileName is the one file in a plugin's private directory that belongs to the SETTINGS.
//
// It is excluded from the storage snapshot on purpose: `config.changed` already reports it, and
// two events for one save would run every plugin's settings handler twice.
const ConfigFileName = "config.json"

// FileStamp is what "did this file change" is decided on.
//
// (mtime, size) is enough for the writers this project has: a JSON encoder that rewrites the whole
// file. A rewrite that keeps both is possible in theory and not worth defending against - a lost
// event only means the plugin keeps its old view until the next write.
type FileStamp struct {
	Mod  int64 `json:"mod"`
	Size int64 `json:"size"`
}

// StampFile reads one file's stamp. ok=false when the file is absent (which callers treat as "no
// entry", so an APPEARING file counts as a change).
func StampFile(path string) (FileStamp, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return FileStamp{}, false
	}
	return FileStamp{Mod: info.ModTime().UnixNano(), Size: info.Size()}, true
}

// ConfigStamp reads one plugin's settings stamp.
func ConfigStamp(dir string) (FileStamp, bool) {
	return StampFile(filepath.Join(dir, ConfigFileName))
}

// PrivateDirStamps walks one private directory and keys every file by the STORAGE KEY the plugin
// would pass to `storage.get` (slash-separated, relative to the directory) - that is the string
// the plugin can act on, not the OS path.
//
// config.json is skipped (see ConfigFileName).
func PrivateDirStamps(dir string) map[string]FileStamp {
	stamps := map[string]FileStamp{}
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		if key == ConfigFileName {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		stamps[key] = FileStamp{Mod: info.ModTime().UnixNano(), Size: info.Size()}
		return nil
	})
	return stamps
}

// ChangedStamps reports which ids changed between two snapshots, sorted for a stable order (the
// log and the tests both benefit).
//
// An id that APPEARED counts as changed: the first write is exactly the case a polling plugin
// could not tell from "still empty".
func ChangedStamps(prev, cur map[string]FileStamp) []string {
	var out []string
	for id, now := range cur {
		if before, seen := prev[id]; !seen || before != now {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// ChangedStorageStamps reports which (plugin, key) pairs changed, per plugin, each key list
// sorted.
func ChangedStorageStamps(prev, cur map[string]map[string]FileStamp) map[string][]string {
	out := map[string][]string{}
	for id, now := range cur {
		before := prev[id]
		for key, stamp := range now {
			if old, seen := before[key]; !seen || old != stamp {
				out[id] = append(out[id], key)
			}
		}
		sort.Strings(out[id])
	}
	return out
}

// ShareTrayName reports whether a private-directory key is a published value (`share/<name>`), and
// which name it was published under.
//
// `share/` is the host's folder, not the plugin's: a key that lands there is a tray value, and the
// plugin's own `storage.get` cannot name it. Both hosts route it as `share.changed` rather than
// `storage.changed` for that reason - and the tray is also how a publish made by the OTHER program
// reaches a frame mounted in this one.
func ShareTrayName(key string) (string, bool) {
	const prefix = "share/"
	if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
		return "", false
	}
	name := key[len(prefix):]
	if name == "" || filepath.ToSlash(name) != name || containsSlash(name) {
		return "", false
	}
	return name, true
}

func containsSlash(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return true
		}
	}
	return false
}
