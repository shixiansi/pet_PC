package main

import (
	"context"
	"testing"
	"time"

	"PetPC/internal/store"
)

// TestStartupSequenceDoesNotBlock drives the same calls App.startup makes, in the
// same order, against a real store.
//
// It exists because the built application reproducibly stopped logging after
// "boot: llm initialised" - i.e. inside logLLMStartup - leaving everything after
// it (speech init, the window reveal) never running. Guessing which call blocked
// wasted a lot of time; this pins it down in a test that either finishes or hangs
// with a stack trace.
//
// Each step is wrapped in its own timeout so the failure names the step rather
// than timing out the whole test.
func TestStartupSequenceDoesNotBlock(t *testing.T) {
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	a := &App{store: s}
	a.ctx = context.Background()
	// The settings the failing run had: the model on and configured. Speech is no
	// longer a setting of the pet's - the voice plugin owns its switches - so this
	// fixture no longer turns it on, and the startup path it exercises has one fewer
	// subsystem to initialise.
	if _, err := s.UpdateConfig(func(c *store.Config) {
		c.LLMEnabled = true
		c.LLMBaseURL = "https://example.invalid/v1"
		c.LLMModel = "m"
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	steps := []struct {
		name string
		fn   func()
	}{
		{"initLLM", func() { a.initLLM() }},
		{"logLLMStartup", func() { a.logLLMStartup() }},
		{"GetState", func() { _, _ = a.GetState() }},
	}

	for _, st := range steps {
		done := make(chan struct{})
		go func(fn func()) {
			defer close(done)
			fn()
		}(st.fn)

		select {
		case <-done:
			t.Logf("  OK   %s", st.name)
		case <-time.After(5 * time.Second):
			t.Fatalf("step %q did not finish in 5s: it is blocking, and every step after it in App.startup never runs", st.name)
		}
	}
}
