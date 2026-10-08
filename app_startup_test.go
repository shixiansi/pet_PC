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
// it (speech init, the proactive loop, the window reveal) never running. Guessing
// which call blocked wasted a lot of time; this pins it down in a test that either
// finishes or hangs with a stack trace.
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
	// The settings the failing run had: LLM on, speech on and configured.
	if _, err := s.UpdateConfig(func(c *store.Config) {
		c.LLMEnabled = true
		c.LLMBaseURL = "https://example.invalid/v1"
		c.LLMModel = "m"
		c.VoiceEnabled = true
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	steps := []struct {
		name string
		fn   func()
	}{
		{"initLLM", func() { a.initLLM() }},
		{"logLLMStartup", func() { a.logLLMStartup() }},
		{"llmView", func() { _ = a.llmView() }},
		{"initVoice", func() { a.initVoice() }},
		{"voiceView", func() { _ = a.voiceView() }},
		{"initProactive", func() { a.initProactive() }},
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

// TestViewsWithProactiveOnDoNotRecurse is the regression test for a stack
// overflow that killed the process on every GetState.
//
// llmView filled its Proactive field by calling proactiveView, and proactiveView
// asked for the LLM's readiness by calling llmView - a cycle. With proactive
// speech OFF the second call returned early and nothing happened, so the bug lay
// dormant; switching the feature on in the settings panel made the app vanish at
// startup with no message, no log and no panic the user could see.
//
// A plain call is enough to catch it: on the old code this test does not fail an
// assertion, it dies with "goroutine stack exceeds 1000000000-byte limit". The
// timeout is therefore a backstop for a slower failure mode, and the call is made
// in a goroutine so a regression is reported as a test failure rather than taking
// the whole test binary down with it.
func TestViewsWithProactiveOnDoNotRecurse(t *testing.T) {
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	a := &App{store: s}
	a.ctx = context.Background()
	// The exact configuration that triggered it: proactive ON, which is when
	// proactiveView stops returning early.
	if _, err := s.UpdateConfig(func(c *store.Config) {
		c.LLMEnabled = true
		c.LLMBaseURL = "https://example.invalid/v1"
		c.LLMModel = "m"
		c.LLMProactive = true
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	for _, tc := range []struct {
		name string
		fn   func()
	}{
		{"proactiveView", func() { _ = a.proactiveView() }},
		{"llmView", func() { _ = a.llmView() }},
		{"GetState", func() { _, _ = a.GetState() }},
		{"llmReadinessNow", func() { _ = a.llmReadinessNow() }},
	} {
		done := make(chan struct{})
		go func(fn func()) {
			defer close(done)
			fn()
		}(tc.fn)
		select {
		case <-done:
			t.Logf("  OK   %s (proactive on)", tc.name)
		case <-time.After(10 * time.Second):
			t.Fatalf("%s did not return with proactive speech on: this is the infinite-recursion bug returning", tc.name)
		}
	}

	// The two views must also agree about readiness, since one now derives it
	// instead of asking the other.
	lv := a.llmView()
	pv := a.proactiveView()
	if lv.Ready != (pv.Suppressed == "") || !lv.Ready {
		// Not ready here (no API key), so proactive must explain why rather than
		// claiming everything is fine.
		if lv.Ready {
			t.Fatalf("llmView says ready but proactiveView suppressed with %q", pv.Suppressed)
		}
		if pv.Suppressed != lv.NotReadyReason {
			t.Errorf("readiness disagrees: llmView reason %q vs proactiveView %q",
				lv.NotReadyReason, pv.Suppressed)
		}
	}
}
