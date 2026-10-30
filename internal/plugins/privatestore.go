// Package plugins' private store: the files a plugin keeps in its OWN directory.
//
// # WHY THIS LIVES HERE RATHER THAN IN EACH PROGRAM
//
// Two programs host plugin documents: the pet (app_plugins.go) and the plugin market
// (tools/plugin-market/pluginhost.go). Both answer `storage.*`, and before this file
// each had its own copy of "validate the key, join it to the private directory, read
// or write". That was already two implementations of one security decision, and it
// meant a plugin could find a key it was allowed to write in one process and refused
// in the other - the QQ plugin's sticker workshop runs in the MARKET while the bot
// that fills the library runs in the PET, so "the same key works in both" is a
// requirement, not a nicety.
//
// # WHY A KEY IS NOW A PATH
//
// A key used to be a bare file name (`^[A-Za-z0-9_-]{1,64}$`), so everything a
// plugin saved landed in one flat directory. The QQ plugin made the cost visible:
// two hundred stickers, two 24 MB videos, a transcript per group and a status
// snapshot, 333 files in one folder with no way to tell a byte blob from a settings
// file. A key may now be a RELATIVE PATH (`stickers/bytes/790ed6cc`), which gives
// every kind of data its own folder while keeping the one property that matters:
//
//	the key is still resolved UNDER the caller's own private directory and can never
//	reach outside it.
//
// The validation is deliberately narrower than "a relative path": every segment must
// match storageKeySegment, so there is no `.`, no `..`, no drive letter, no backslash
// (a Windows separator would otherwise be a way to write the same key twice under two
// names) and no empty segment. Depth and total length are bounded so a key cannot
// become a place to hide a path.
package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The bounds below are part of the contract, not tuning knobs: a plugin that asks for
// a deeper or longer key gets a refusal naming the limit, rather than a path the host
// silently truncates.
const (
	// storageKeyMaxDepth is how many folders deep a key may go.
	storageKeyMaxDepth = 4
	// storageKeyMaxLen bounds the whole key.
	storageKeyMaxLen = 200
)

// StorageKeyPath validates one key and returns the relative path it names.
//
// The result uses the OS separator and is safe to Join to a private directory: every
// segment was checked against ValidStorageSegment, so no segment can be `.`, `..` or a
// separator in disguise.
func StorageKeyPath(key string) (string, error) {
	if key == "" {
		return "", errors.New("storage key 不能为空")
	}
	if len(key) > storageKeyMaxLen {
		return "", fmt.Errorf("storage key 太长：%d 字符（上限 %d）", len(key), storageKeyMaxLen)
	}
	segments := strings.Split(key, "/")
	if len(segments) > storageKeyMaxDepth {
		return "", fmt.Errorf("storage key 的目录太深：%d 层（上限 %d）", len(segments), storageKeyMaxDepth)
	}
	for _, segment := range segments {
		if !ValidStorageSegment(segment) {
			return "", fmt.Errorf("storage key 非法：%q（用 / 分隔，每一段只能是字母、数字、下划线或连字符，且不能是 . 或 ..）", key)
		}
	}
	return filepath.Join(segments...), nil
}

// ValidStorageSegment reports whether name is one legal segment of a storage key.
//
// It is exported because the FLAT names a plugin uses elsewhere are held to the same
// shape: an audio take's name, a share tray's name. Those are single segments by
// definition - a shared value is a file, not a tree - so they go through this check
// and not through StorageKeyPath.
func ValidStorageSegment(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// storagePath turns a key into an absolute path under dir.
func storagePath(dir, key string) (string, error) {
	rel, err := StorageKeyPath(key)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, rel), nil
}

// StorageGet reads one private value.
//
// A missing value is an EMPTY value, not an error: a plugin reading its own storage
// before writing anything is a normal first run, and a plugin created in a folder it
// has not used yet is the same situation one level down.
func StorageGet(dir string, payload []byte) (any, error) {
	var in struct {
		Key string `json:"key"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	path, err := storagePath(dir, in.Key)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"value": ""}, nil
		}
		return nil, err
	}
	return map[string]any{"value": string(data)}, nil
}

// StorageSet writes one private value, creating the folders its key names.
//
// The MkdirAll is what makes a folder layout usable at all: a plugin that wants
// `stickers/bytes/<hash>` should not have to create `stickers` and `stickers/bytes`
// first with a second capability it does not have.
func StorageSet(dir string, payload []byte) (any, error) {
	var in struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	path, err := storagePath(dir, in.Key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		// Translate the ONE cause a plugin can actually fix, and it is the one that looks
		// like anything but a plugin bug: a key that is a FILE in one call and a FOLDER in
		// another (`s/<hash>` and `s/<hash>/meta`) makes MkdirAll fail with Windows'
		// "The system cannot find the path specified", which reads as a disk or permission
		// problem. The QQ-bot plugin spent six of those on 2026-10-29 before anyone looked at
		// the key names. Anything else keeps the original error, because guessing at other
		// causes would replace a precise message with a vague one.
		if blocker := firstFileInPath(dir, filepath.Dir(path)); blocker != "" {
			return nil, fmt.Errorf(
				"storage key %q 用不了：%q 已经是一个文件，不能同时当目录（一个键只能有一种身份）",
				in.Key, blocker)
		}
		return nil, err
	}
	if err := os.WriteFile(path, []byte(in.Value), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// firstFileInPath reports the first component of path (relative to base) that exists as a FILE,
// or "" when the whole prefix is folders or absent.
//
// It is only called after a failed MkdirAll, so the cost of walking a handful of segments is
// paid on an error path and never on the happy one.
func firstFileInPath(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	current := base
	for _, segment := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, err := os.Stat(current)
		if err != nil {
			return "" // absent: MkdirAll could have created it, so this is not the cause
		}
		if !info.IsDir() {
			return current
		}
	}
	return ""
}

// StorageRemove deletes one private value.
//
// The folders a key named are LEFT IN PLACE. Removing them would be a second write
// that can fail after the first succeeded, and an empty folder costs nothing while a
// half-removed tree is a state nothing reports.
func StorageRemove(dir string, payload []byte) (any, error) {
	var in struct {
		Key string `json:"key"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	path, err := storagePath(dir, in.Key)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// StorageStat reports the size of one of the caller's own files.
//
// It returns `exists:false` rather than an error for a missing key: "not there yet" is
// the normal answer while a download is running, and a plugin polling it should not
// have to treat that as a failure.
func StorageStat(dir string, payload []byte) (any, error) {
	var in struct {
		Key string `json:"key"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	path, err := storagePath(dir, in.Key)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"exists": false, "bytes": 0}, nil
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("storage.stat 只接受文件：%s 是目录", in.Key)
	}
	return map[string]any{"exists": true, "bytes": info.Size()}, nil
}

// StorageDir reports a directory the caller may hand to a program it starts.
//
// WHY A PLUGIN NEEDS THIS: it can pass the path to a downloader writing its output
// there, and to the chat protocol side (which reads the file by path). Without it,
// "run ffmpeg and send the result" has no place to put the result.
//
// `sub` names a FOLDER inside that directory (same segment rules, zero or more of
// them) and is created on demand. That is what lets those products land next to each
// other instead of in the root of the plugin's data: the plugin's own writes can name
// a folder, but a program writing to a path it was handed cannot.
func StorageDir(dir string, payload []byte) (any, error) {
	var in struct {
		Sub string `json:"sub"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	out := dir
	if strings.TrimSpace(in.Sub) != "" {
		rel, err := StorageKeyPath(in.Sub)
		if err != nil {
			return nil, err
		}
		out = filepath.Join(dir, rel)
		if err := os.MkdirAll(out, 0o755); err != nil {
			return nil, err
		}
	}
	return map[string]any{"dir": out}, nil
}

// StorageList reports what is in one of the caller's own folders.
//
// WHY THIS EXISTS: a plugin that keeps hundreds of megabytes in its own folder needs to
// be able to SEE that folder. The QQ plugin's video products are recorded in a ledger
// file, and the ledger can lose entries - it is cleared after ten minutes, and an older
// version cleared it while leaving the products behind. The result on the real machine
// was two 24 MB files that nothing would ever delete again: the plugin has no way to ask
// "what is in here?", so the only two candidates were "leak forever" or "delete by a
// name pattern guessed in the dark", and both are wrong.
//
// Read-only, names and sizes, `sub` validated exactly like `storage.dir` (so a listing
// cannot be aimed outside the private directory). A folder that does not exist yet is an
// EMPTY listing rather than an error, for the same reason a missing value is empty.
func StorageList(dir string, payload []byte) (any, error) {
	var in struct {
		Sub string `json:"sub"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	base := dir
	if strings.TrimSpace(in.Sub) != "" {
		rel, err := StorageKeyPath(in.Sub)
		if err != nil {
			return nil, err
		}
		base = filepath.Join(dir, rel)
	}
	files := []map[string]any{}
	dirs := []string{}
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"dir": base, "files": files, "dirs": dirs}, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
			continue
		}
		info, err := entry.Info()
		if err != nil {
			// A file that vanished between ReadDir and Info is simply not listed; the
			// caller is looking for leftovers, and a race is not worth an error.
			continue
		}
		files = append(files, map[string]any{"name": entry.Name(), "bytes": info.Size()})
	}
	return map[string]any{"dir": base, "files": files, "dirs": dirs}, nil
}

// StorageMove renames one of the caller's own files.
//
// It exists for REORGANISING, not for saving bytes through the bridge: a plugin that
// has to move 300 MB of its own leftovers to a new layout can rename them in place
// instead of reading them back as base64 and writing them out again. Both ends are
// keys, so both are validated the same way and the move cannot leave the directory.
//
// A missing source and an occupied target are both reported as moved:false rather
// than as errors, because the one caller this method was added for is a MIGRATION:
// running it twice, or on a value it already moved, must be a no-op. The source is
// left untouched when the target is occupied - "rename over an existing file" is a
// data-loss shape nobody asked for.
func StorageMove(dir string, payload []byte) (any, error) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	from, err := storagePath(dir, in.From)
	if err != nil {
		return nil, err
	}
	to, err := storagePath(dir, in.To)
	if err != nil {
		return nil, err
	}
	if from == to {
		return map[string]any{"ok": true, "moved": false}, nil
	}
	if _, err := os.Stat(from); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"ok": true, "moved": false}, nil
		}
		return nil, err
	}
	if _, err := os.Stat(to); err == nil {
		return map[string]any{"ok": true, "moved": false, "targetExists": true}, nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(from, to); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "moved": true}, nil
}

// RegisterPrivateStore registers the private-store methods into a program's table.
//
// A host that offers `storage` calls this instead of hand-writing one entry per
// method: the key validation, the "only ever under YOUR directory" rule and the
// missing-value-is-empty convention then have exactly one implementation, and a
// plugin behaves the same in the pet and in the market.
func RegisterPrivateStore(r *Registry) {
	register := func(name string, run func(dir string, payload []byte) (any, error)) {
		r.Register(name, Method{
			Perm: PermStorage,
			Run: func(_ any, call Call, payload []byte) (any, error) {
				return run(call.PrivateDir, payload)
			},
		})
	}
	register("storage.get", StorageGet)
	register("storage.set", StorageSet)
	register("storage.remove", StorageRemove)
	register("storage.stat", StorageStat)
	register("storage.dir", StorageDir)
	register("storage.list", StorageList)
	register("storage.move", StorageMove)
}
