package main

import (
	"fmt"

	"PetPC/internal/store"
)

// Read-only views of the pet's data.
//
// These exist so a page can SHOW what the pet knows without this program becoming a
// second writer of those files. todos.json belongs to the pet (openspec/changes/
// ui-rebuild, design: one writer per file), so the control center reads it and nothing
// more until a reload protocol is designed for the other direction.

// Todos returns the todo list in the panel's order.
func (m *Market) Todos() ([]store.Todo, error) {
	s, err := store.New(m.dataDir)
	if err != nil {
		return nil, fmt.Errorf("读取待办失败：%w", err)
	}
	return s.Todos(), nil
}

// DataDir is where everything lives, so the advanced page can show it rather than
// describe it - "your data is in %APPDATA%\PetPC" is the one sentence users need and
// never find.
func (m *Market) DataDir() string { return m.dataDir }
