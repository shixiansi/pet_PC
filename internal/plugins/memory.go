package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The plugin's long-term memory: one markdown file the MODEL maintains, plus a
// bounded history of its previous versions.
//
// WHY A FILE AND NOT A VECTOR STORE: the whole value is that the model can read it,
// so it has to fit in a prompt. A few hundred lines of "the user is working on X,
// prefers Y, does not want Z" is what survives a conversation, and it is also what a
// human can open and correct. Embeddings would answer "which old message mentioned
// this", which is a different question with a much larger bill.
//
// WHY APPEND-ONLY: a model that could REWRITE this file could delete a fact the user
// depends on - silently, and with no way back. Appending a correction ("actually it is
// Y now") is always available and never loses what was there.
//
// WHY EVERY CHANGE IS VERSIONED: the model WILL write wrong things into it. The user
// needs to be able to see what changed and when, and to put back what was there.
//
// WHY IT IS BOUNDED: this file is meant to be read on every conversation. An
// unbounded one eventually costs more context than the conversation itself, and the
// first symptom is the model answering from a wall of stale notes.
const (
	// MemoryFileName is the memory document itself, inside the plugin's private dir.
	MemoryFileName = "memory.md"
	// memoryHistoryDir holds the snapshot taken BEFORE each append.
	memoryHistoryDir = "memory-history"
	// maxMemoryBytes caps the document. 8 KiB is roughly 2000 Chinese characters:
	// enough for the standing facts of a person's week, small enough to send on every
	// request without thinking about it.
	maxMemoryBytes = 8 << 10
	// maxMemoryAppendRunes caps ONE entry, so a runaway model cannot fill the file in
	// a single call.
	maxMemoryAppendRunes = 500
	// maxMemoryHistory is how many previous versions are kept. Twenty is enough to
	// notice "it started saying nonsense on Tuesday" and still bounded.
	maxMemoryHistory = 20
)

// ReadMemory returns the plugin's memory document, or "" when it has none yet.
//
// Empty is not an error: a plugin with the permission and no notes is the normal
// state of a fresh install, and making the caller distinguish "no file" from "empty
// file" would be a distinction with no consequence.
func ReadMemory(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, MemoryFileName))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// AppendMemory adds one entry and returns the new document.
//
// The previous version is snapshotted first, so a bad entry is always recoverable by
// hand. The snapshot is taken BEFORE the write rather than after: a version that
// already contains the mistake is not a version anyone wants.
func AppendMemory(dir, text string, now time.Time) (string, error) {
	entry := strings.TrimSpace(text)
	if entry == "" {
		return "", errors.New("memory.append 需要 text")
	}
	if runes := []rune(entry); len(runes) > maxMemoryAppendRunes {
		entry = string(runes[:maxMemoryAppendRunes]) + "…"
	}
	// One entry is one line. A model that writes a paragraph would otherwise break the
	// "one bullet per fact" shape that makes this file readable, and newlines inside an
	// entry would let it forge the version header below.
	entry = strings.Join(strings.Fields(entry), " ")

	current, err := ReadMemory(dir)
	if err != nil {
		return "", err
	}

	stamp := now.Format("2006-01-02 15:04")
	line := fmt.Sprintf("- %s  %s\n", stamp, entry)
	next := current + line
	if len(next) > maxMemoryBytes {
		return "", fmt.Errorf("记忆已满（上限 %d KiB），先把旧的整理掉再记新的", maxMemoryBytes>>10)
	}

	if strings.TrimSpace(current) != "" {
		if err := snapshotMemory(dir, current, now); err != nil {
			// A snapshot that cannot be written must NOT be swallowed: appending
			// without it would overwrite history the user can no longer get back.
			return "", fmt.Errorf("记忆快照失败，未写入：%w", err)
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, MemoryFileName)
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return "", err
	}
	return next, nil
}

// snapshotMemory writes the current document into the history directory and prunes old
// versions.
//
// Filenames are timestamps to the second, with a numeric suffix on collision: two
// appends inside one second are normal (the model can call the tool twice in a round),
// and silently overwriting the first would lose exactly the version a user would want
// to see when something went wrong.
func snapshotMemory(dir, body string, now time.Time) error {
	historyDir := filepath.Join(dir, memoryHistoryDir)
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		return err
	}
	base := now.Format("2006-01-02T15-04-05")
	name := base + ".md"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(historyDir, name)); errors.Is(err, os.ErrNotExist) {
			break
		}
		name = fmt.Sprintf("%s-%d.md", base, i)
	}
	if err := os.WriteFile(filepath.Join(historyDir, name), []byte(body), 0o644); err != nil {
		return err
	}
	return pruneMemoryHistory(historyDir)
}

// pruneMemoryHistory keeps the newest maxMemoryHistory versions.
//
// Oldest-first by NAME, which sorts correctly because the filenames are ISO-ish
// timestamps. Pruning is best-effort on the deletion of an already-listed file: a
// failure there leaves one extra version, which is harmless, while failing the append
// over it would be a write the user cannot explain.
func pruneMemoryHistory(historyDir string) error {
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	if len(names) <= maxMemoryHistory {
		return nil
	}
	sort.Strings(names)
	for _, name := range names[:len(names)-maxMemoryHistory] {
		_ = os.Remove(filepath.Join(historyDir, name))
	}
	return nil
}
