package main

import (
	"fmt"

	"PetPC/internal/store"
)

// PomodoroRecords returns the finished-session archive, newest last (the store's order).
//
// Read-only, like Todos: pomodoro-records.json belongs to the pet. The record is the one
// thing in this app that is meant to be permanent, so a second program able to rewrite it
// would be able to erase work that really happened.
func (m *Market) PomodoroRecords() ([]store.PomodoroRecord, error) {
	s, err := store.New(m.dataDir)
	if err != nil {
		return nil, fmt.Errorf("读取番茄记录失败：%w", err)
	}
	return s.PomodoroRecords(), nil
}
