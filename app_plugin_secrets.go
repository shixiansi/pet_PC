package main

import (
	"fmt"
	"os"
	"path/filepath"

	"PetPC/internal/desktop"
)

// pathSecretKeeper adapts internal/desktop's SecretStore (DPAPI on Windows) to
// the plugins.SecretKeeper interface.
//
// The interface exists so internal/plugins can store encrypted values without
// importing internal/desktop - it uses the standard library only. This adapter
// is the pet's side of that boundary, and the plugin market program has its own
// copy of the same three methods: each program owns its own dependency wiring,
// while the storage rules (one file per key, DPAPI at rest) are shared.
type pathSecretKeeper struct{}

// NewSecretKeeper returns the pet's secret storage backend.
func NewSecretKeeper() pathSecretKeeper { return pathSecretKeeper{} }

// Get returns the plaintext at path.
//
// A MISSING file is an empty value, not an error: a plugin that has not been
// configured yet is the normal first run. A file that exists but cannot be
// decrypted IS reported as an error, because the two cases have opposite
// remedies - "fill the field in" versus "the credential is unusable on this
// account" - and callers act on the difference.
func (pathSecretKeeper) Get(path string) (string, error) {
	store := desktop.NewSecretStore(filepath.Dir(path), filepath.Base(path))
	value := store.Get()
	if value == "" {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("密钥无法解密：%s", filepath.Base(path))
		}
		return "", nil
	}
	return value, nil
}

// Set encrypts value into path.
func (pathSecretKeeper) Set(path, value string) error {
	store := desktop.NewSecretStore(filepath.Dir(path), filepath.Base(path))
	return store.Set(value)
}

// Clear removes path, treating "already gone" as success.
func (pathSecretKeeper) Clear(path string) error {
	store := desktop.NewSecretStore(filepath.Dir(path), filepath.Base(path))
	return store.Clear()
}
