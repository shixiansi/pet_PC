package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DPAPI is per-user and machine-bound, which means the only honest way to test
// this code is to actually call it: there is no fixture that can stand in for
// Windows' own key material.

func TestProtectRoundTrip(t *testing.T) {
	for _, plain := range []string{"sk-1234567890abcdef", "短", strings.Repeat("x", 4096)} {
		blob, err := protectBytes([]byte(plain))
		if err != nil {
			t.Fatalf("protectBytes(%d bytes): %v", len(plain), err)
		}
		if strings.Contains(string(blob), plain) && len(plain) > 8 {
			t.Fatalf("ciphertext contains the plaintext: %q", blob)
		}
		back, err := unprotectBytes(blob)
		if err != nil {
			t.Fatalf("unprotectBytes: %v", err)
		}
		if string(back) != plain {
			t.Fatalf("round trip mismatch: got %q want %q", back, plain)
		}
	}
}

func TestUnprotectRejectsGarbage(t *testing.T) {
	if _, err := unprotectBytes([]byte("not a dpapi blob")); err == nil {
		t.Fatal("expected an error for a non-DPAPI blob")
	}
	if _, err := unprotectBytes(nil); err == nil {
		t.Fatal("expected an error for an empty blob")
	}
}

// clearKeyEnv removes the environment override for the duration of a test: it
// takes precedence over the file, so leaving it set would make the file tests
// pass for the wrong reason.
func clearKeyEnv(t *testing.T) {
	t.Helper()
	old, had := os.LookupEnv(APIKeyEnvVar)
	if err := os.Unsetenv(APIKeyEnvVar); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(APIKeyEnvVar, old)
		} else {
			os.Unsetenv(APIKeyEnvVar)
		}
	})
}

func TestSecretStoreFileRoundTrip(t *testing.T) {
	clearKeyEnv(t)
	s := NewSecretStore(t.TempDir(), "llm-key.bin")

	if s.Has() || s.Source() != SourceNone {
		t.Fatalf("a fresh store should have no key, got source=%q", s.Source())
	}
	if got := s.Get(); got != "" {
		t.Fatalf("Get on an empty store = %q, want \"\"", got)
	}

	const key = "sk-abcdefghijklmnop"
	if err := s.Set(key); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !s.Has() || s.Source() != SourceFile {
		t.Fatalf("after Set: has=%v source=%q", s.Has(), s.Source())
	}
	if got := s.Get(); got != key {
		t.Fatalf("Get = %q, want %q", got, key)
	}

	// The whole point of the exercise: the file must not carry the key in the
	// clear. This is the assertion that fails if someone "simplifies" the store
	// into a plain-text file.
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if strings.Contains(string(raw), key) {
		t.Fatalf("stored file contains the key in the clear: %q", raw)
	}
	if len(raw) == 0 {
		t.Fatal("stored file is empty")
	}

	// A second store over the same file must see the same key (this is what a
	// restart looks like).
	again := NewSecretStore(filepath.Dir(s.Path()), filepath.Base(s.Path()))
	if got := again.Get(); got != key {
		t.Fatalf("after reopen Get = %q, want %q", got, key)
	}

	if err := s.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if s.Has() {
		t.Fatal("Has is still true after Clear")
	}
	// Clearing twice must be harmless: the settings UI has no way to know
	// whether the file was there.
	if err := s.Clear(); err != nil {
		t.Fatalf("second Clear: %v", err)
	}
}

func TestSecretStoreTrimsInput(t *testing.T) {
	clearKeyEnv(t)
	s := NewSecretStore(t.TempDir(), "llm-key.bin")
	// A key pasted into the settings field very often arrives with a trailing
	// newline, which would otherwise be sent as part of the Authorization
	// header and rejected by the server with a confusing 401.
	if err := s.Set("  sk-padded  \n"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Get(); got != "sk-padded" {
		t.Fatalf("Get = %q, want %q", got, "sk-padded")
	}
	// Setting an empty value is a clear, not a store of "".
	if err := s.Set("   "); err != nil {
		t.Fatalf("Set(blank): %v", err)
	}
	if s.Has() {
		t.Fatal("a blank Set should have cleared the stored key")
	}
}

func TestSecretStoreEnvOverridesFile(t *testing.T) {
	clearKeyEnv(t)
	s := NewSecretStore(t.TempDir(), "llm-key.bin")
	if err := s.Set("sk-from-file"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Setenv(APIKeyEnvVar, "sk-from-env")

	if s.Source() != SourceEnv {
		t.Fatalf("source = %q, want %q", s.Source(), SourceEnv)
	}
	if got := s.Get(); got != "sk-from-env" {
		t.Fatalf("Get = %q, want the env value", got)
	}
	// Clearing the file must not be able to remove the env key.
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got := s.Get(); got != "sk-from-env" {
		t.Fatalf("after Clear, Get = %q, want the env value to survive", got)
	}
}

func TestSecretStoreUndecryptableFileLooksUnconfigured(t *testing.T) {
	clearKeyEnv(t)
	dir := t.TempDir()
	s := NewSecretStore(dir, "llm-key.bin")
	// A blob written by a different user (or a truncated one) cannot be
	// decrypted. The UI must read that as "no key", not as a crash.
	if err := os.WriteFile(s.Path(), []byte("garbage that is not dpapi"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if s.Has() {
		t.Fatal("Has should be false for an undecryptable file")
	}
	if s.Source() != SourceNone {
		t.Fatalf("source = %q, want %q", s.Source(), SourceNone)
	}
	if got := s.Get(); got != "" {
		t.Fatalf("Get = %q, want \"\"", got)
	}
}

func TestSecretStoreMask(t *testing.T) {
	clearKeyEnv(t)
	s := NewSecretStore(t.TempDir(), "llm-key.bin")
	if got := s.Mask(); got != "" {
		t.Fatalf("Mask with no key = %q, want \"\"", got)
	}

	cases := []struct {
		key  string
		want string
	}{
		{"sk-abcdefghijklmnop", "sk-…mnop"},
		{"short", "••••"},
		{"12345678", "••••"},
		{"123456789", "••••"},
		{"sk-1234567890abcdef", "sk-…cdef"},
	}
	for _, c := range cases {
		if err := s.Set(c.key); err != nil {
			t.Fatalf("Set(%q): %v", c.key, err)
		}
		got := s.Mask()
		if got != c.want {
			t.Errorf("Mask(%q) = %q, want %q", c.key, got, c.want)
		}
		// The mask must never reveal more than a three-character prefix plus the
		// last four characters. Counting runes, because the ellipsis is one
		// character but three bytes.
		if got != "••••" {
			head, tail, found := strings.Cut(got, "…")
			if !found {
				t.Errorf("Mask(%q) = %q has no ellipsis", c.key, got)
				continue
			}
			if n := len([]rune(head)); n > 3 {
				t.Errorf("Mask(%q) = %q reveals a %d-character prefix", c.key, got, n)
			}
			if n := len([]rune(tail)); n != 4 {
				t.Errorf("Mask(%q) = %q reveals a %d-character suffix", c.key, got, n)
			}
		}
	}
}

// TestMaskedKeyIsNeverTheKey guards the invariant the settings panel depends on:
// whatever the UI is handed must not be usable as a credential.
func TestMaskedKeyIsNeverTheKey(t *testing.T) {
	clearKeyEnv(t)
	s := NewSecretStore(t.TempDir(), "llm-key.bin")
	for _, key := range []string{"sk-abcdefghijklmnop", "123456789", "abcdefghijklmnopqrstuvwxyz"} {
		if err := s.Set(key); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if masked := s.Mask(); masked == key {
			t.Errorf("Mask returned the key itself for %q", key)
		}
	}
}
