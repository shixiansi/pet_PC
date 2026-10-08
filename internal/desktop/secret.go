package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// APIKeyEnvVar lets the LLM API key come from the environment instead of the
// encrypted file. It wins over the file when both are present, which is what
// makes CI runs and a read-only portable stick workable: the key never has to
// touch disk in those setups.
const APIKeyEnvVar = "PETPC_LLM_API_KEY"

// SecretStore persists one small secret (the LLM API key) on disk, encrypted
// with the platform's own per-user facility.
//
// Two properties matter here and both are load-bearing:
//
//  1. The plaintext never lives in a file that is readable by another user or by
//     a synced folder. On Windows that is DPAPI (see protectBytes), which binds
//     the ciphertext to the current user account.
//  2. The secret must NOT end up in store.Config. GetState hands the whole
//     config to the webview on every refresh, so a key stored there would be one
//     `JSON.stringify(state)` away from leaking into the page. This type exists
//     precisely so the key has somewhere else to live.
//
// The value is deliberately not cached in memory: it is read from disk on each
// use. Decrypting a few hundred bytes costs microseconds, and a long-lived field
// holding a plaintext credential is the kind of thing that later gets logged by
// accident.
type SecretStore struct {
	path string
	mu   sync.Mutex
}

// NewSecretStore returns a secret store backed by dir/name.
func NewSecretStore(dir, name string) *SecretStore {
	return &SecretStore{path: filepath.Join(dir, name)}
}

// Path returns the file the secret is stored in. It is exposed for the settings
// UI ("密钥保存在这个文件里") and for tests.
func (s *SecretStore) Path() string { return s.path }

// Source describes where the effective secret comes from, for the settings UI.
type Source string

const (
	// SourceNone means no key is configured anywhere.
	SourceNone Source = "none"
	// SourceEnv means PETPC_LLM_API_KEY is set and takes precedence.
	SourceEnv Source = "env"
	// SourceFile means the encrypted file holds the key.
	SourceFile Source = "file"
)

// Source reports which source currently supplies the key.
//
// An encrypted file that cannot be decrypted (a different Windows user, a
// restored backup, a truncated file) reports SourceNone rather than an error:
// from the user's point of view that key simply is not usable, and the settings
// panel should offer to re-enter it instead of showing a stack trace.
func (s *SecretStore) Source() Source {
	if strings.TrimSpace(os.Getenv(APIKeyEnvVar)) != "" {
		return SourceEnv
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil || len(data) == 0 {
		return SourceNone
	}
	if _, err := unprotectBytes(data); err != nil {
		return SourceNone
	}
	return SourceFile
}

// Has reports whether a usable key exists.
func (s *SecretStore) Has() bool { return s.Source() != SourceNone }

// Get returns the plaintext secret, or "" when none is configured. A file that
// exists but cannot be decrypted yields "" and no error, for the reason given on
// Source.
func (s *SecretStore) Get() string {
	if env := strings.TrimSpace(os.Getenv(APIKeyEnvVar)); env != "" {
		return env
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil || len(data) == 0 {
		return ""
	}
	plain, err := unprotectBytes(data)
	if err != nil {
		return ""
	}
	return string(plain)
}

// Set encrypts and stores the secret.
//
// The write is atomic (temp file + rename) for the same reason every other file
// in this app is: a crash halfway through must not leave a half-written
// credential that then decrypts to garbage.
func (s *SecretStore) Set(secret string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return s.Clear()
	}
	blob, err := protectBytes([]byte(secret))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".key-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if _, err := tmp.Write(blob); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// Clear removes the stored secret. Removing a file that is already gone is not
// an error, so the settings UI can call this unconditionally.
func (s *SecretStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Mask renders the configured key for display, revealing at most the last four
// characters.
//
// The UI must never show more than this: a mask is a reminder of which key is in
// use, not a way to read it back. An env-supplied key is masked the same way but
// labelled differently by the caller.
func (s *SecretStore) Mask() string {
	key := s.Get()
	if key == "" {
		return ""
	}
	// Anything short is hidden completely. The threshold is 16 rather than 8
	// because revealing 7 of a 9-character key (as "123…6789") gives most of it
	// away; a mask has to be a reminder, not a partial disclosure.
	if len(key) < 16 {
		return "••••"
	}
	return key[:3] + "…" + key[len(key)-4:]
}
