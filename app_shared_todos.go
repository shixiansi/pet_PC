package main

// The host's read-only view of the todo plugin's published list.
//
// The LIST moved into com.shixiansi.plugin-todo (its `storage`, key `todos-v1`) when
// `todos.*` retired, and the plugin republishes a snapshot to `share/todo-list` after every
// write. That snapshot is the ONLY way a host-side feature can still see the user's todos,
// and exactly one host-side feature still needs to: the manual «立刻提醒» bubble.
//
// Why the host may read it at all, given that it refuses to read ANOTHER plugin's data on a
// third plugin's behalf: `share` is published data. The writing plugin chose to make it
// readable - that is what `share.set` means - and the pomodoro reads it for the same
// reason. What the host must not do is read a plugin's PRIVATE storage, which is why
// nothing here touches `plugin-data/com.shixiansi.plugin-todo/todos-v1`.
//
// The shape is deliberately narrow (title / due / done): the bubble shows a title and a
// deadline, and reading more of the user's data than the feature renders would be a small
// privacy leak dressed as convenience.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"PetPC/internal/store"
)

// The plugin that owns the list, and the name it publishes under. Both are spelled once
// here; the plugin's own app.js carries the same two strings (SHARE_LIST and its id).
const (
	todoOwnerPluginID = "com.shixiansi.plugin-todo"
	todoShareName     = "todo-list"
)

// sharedTodo is one entry of the published snapshot.
//
// It is declared HERE rather than reusing store.Todo because the JSON on disk is the
// plugin's, not the store's: the plugin writes a flat array with a STRING due date, and a
// struct claiming otherwise would decode to zero values and silently show an empty bubble.
type sharedTodo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
	Due   string `json:"due"`
}

// sharedTodoList reads and parses the snapshot the todo plugin publishes.
//
// Every failure is an EMPTY list rather than an error, and that is the honest answer for
// all of them: the plugin may not be installed, may be disabled, or may not have written
// anything yet - and in each case "there is nothing to remind you about" is true. An error
// here would turn a normal first run into a toast.
func (a *App) sharedTodoList() []store.Todo {
	mgr := a.pluginManager()
	if mgr == nil {
		return nil
	}
	row, ok := mgr.Get(todoOwnerPluginID)
	if !ok || !row.Enabled || row.Manifest == nil {
		return nil
	}
	// Reading the shared value needs the pair declared by the READER; the host is not a
	// plugin and has no manifest, so it reads the file the same way pluginShareGet does -
	// through the owner's private directory, under share/.
	dir, err := mgr.PrivateDir(todoOwnerPluginID)
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, "share", todoShareName))
	if err != nil {
		// A pre-11.0 layout kept it beside the directory root; the capability still reads
		// that one, so this does too, and otherwise a machine that upgraded mid-use would
		// show an empty reminder until the plugin next wrote.
		if legacy, lerr := os.ReadFile(filepath.Join(dir, "share-"+todoShareName)); lerr == nil {
			raw = legacy
		} else {
			return nil
		}
	}

	var parsed []sharedTodo
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}

	out := make([]store.Todo, 0, len(parsed))
	for _, t := range parsed {
		if strings.TrimSpace(t.Title) == "" {
			continue
		}
		item := store.Todo{ID: t.ID, Title: t.Title, Done: t.Done}
		if due, err := parseSharedDue(t.Due); err == nil {
			item.Due = due
		}
		out = append(out, item)
	}
	// The bubble's priority order is the store's, and the snapshot's order is the plugin's
	// (append order). Sorting here keeps the reminder reading like the list the user sees
	// rather than like the order they happened to add things in.
	sortTodosForReminder(out)
	return out
}

// parseSharedDue accepts the plugin's RFC3339 due stamp and ignores anything else.
//
// A due date that cannot be parsed becomes "no deadline" rather than "now": the first is a
// slightly less precise reminder, the second is an item reported as overdue because of a
// format mismatch.
func parseSharedDue(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("no due date")
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// sortTodosForReminder orders entries the way the reminder bubble wants them: open first,
// then by deadline, undated last. It mirrors store.sortTodos, which is unexported and now
// only used for the host's own (migrated-away) copy.
func sortTodosForReminder(todos []store.Todo) {
	sort.SliceStable(todos, func(i, j int) bool {
		x, y := todos[i], todos[j]
		if x.Done != y.Done {
			return !x.Done
		}
		switch {
		case x.Due != nil && y.Due != nil:
			return x.Due.Before(*y.Due)
		case x.Due != nil:
			return true
		case y.Due != nil:
			return false
		default:
			return false
		}
	})
}

// reminderItems applies the bubble's priority rule to a snapshot list.
//
// The rule is the store's ReminderItems, which is now unused by anything the pet ships:
// overdue first (longest overdue first), then due later today, then due within 24 hours,
// completed entries never, and undated entries not at all - the bubble is about what is
// DUE, and an entry without a deadline has nothing to be due.
func reminderItems(todos []store.Todo, max int, now time.Time) []store.Todo {
	if max <= 0 {
		return nil
	}
	endOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)

	var overdue, today, soon []store.Todo
	for _, t := range todos {
		if t.Done || t.Due == nil {
			continue
		}
		switch {
		case t.Due.Before(now):
			overdue = append(overdue, t)
		case t.Due.Before(endOfToday):
			today = append(today, t)
		case t.Due.Sub(now) <= 24*time.Hour:
			soon = append(soon, t)
		}
	}

	out := make([]store.Todo, 0, max)
	for _, group := range [][]store.Todo{overdue, today, soon} {
		for _, t := range group {
			if len(out) == max {
				return out
			}
			out = append(out, t)
		}
	}
	return out
}

// openTodoCount is how many entries in the snapshot are unfinished.
//
// It drives the bubble's "还有 N 项" tail, and it comes from the snapshot rather than from
// a store counter because the store no longer holds the user's list.
func openTodoCount(todos []store.Todo) int {
	n := 0
	for _, t := range todos {
		if !t.Done {
			n++
		}
	}
	return n
}

// reminderCounts renders the three numbers the reminder event carries.
//
// It replaces store.Stats(), which counted the host's own list: dueSoon is "due within 24
// hours and not yet overdue", overdue is "deadline already passed", and open is everything
// unfinished. Keeping the definitions identical to the store's matters - a plugin that
// speaks "N things are due today" reads these directly.
func reminderCounts(todos []store.Todo, now time.Time) (dueSoon, overdue, open int) {
	for _, t := range todos {
		if t.Done {
			continue
		}
		open++
		if t.Due == nil {
			continue
		}
		switch {
		case t.Due.Before(now):
			overdue++
		case t.Due.Sub(now) <= 24*time.Hour:
			dueSoon++
		}
	}
	return dueSoon, overdue, open
}
