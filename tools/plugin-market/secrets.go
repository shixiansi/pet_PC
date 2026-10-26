package main

import (
	"fmt"
	"os"
	"path/filepath"

	"PetPC/internal/desktop"
	"PetPC/internal/plugins"
)

// marketSecretKeeper adapts internal/desktop's SecretStore (DPAPI on Windows) to
// the plugins.SecretKeeper interface.
//
// The interface exists so internal/plugins can store encrypted values without
// importing internal/desktop - it uses the standard library only. The pet has
// its own three-method adapter over the same type; each program owns its own
// dependency wiring while the storage rules (one file per key, encrypted at
// rest, atomic writes) are shared. That matters here because BOTH programs write
// plugin secrets: the market edits them, the pet reads them when a plugin makes
// a request. If the two disagreed about the format, a key saved here would be
// unusable there.
type marketSecretKeeper struct{}

var _ plugins.SecretKeeper = marketSecretKeeper{}

// Get returns the plaintext at path.
//
// A MISSING file is an empty value, not an error: a plugin that has not been
// configured yet is the normal first run. A file that exists but cannot be
// decrypted IS an error, because the two cases have opposite remedies - "fill
// the field in" versus "this credential is unusable on this account" - and the
// editor shows them differently.
func (marketSecretKeeper) Get(path string) (string, error) {
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
func (marketSecretKeeper) Set(path, value string) error {
	store := desktop.NewSecretStore(filepath.Dir(path), filepath.Base(path))
	return store.Set(value)
}

// Clear removes path, treating "already gone" as success.
func (marketSecretKeeper) Clear(path string) error {
	store := desktop.NewSecretStore(filepath.Dir(path), filepath.Base(path))
	return store.Clear()
}
