//go:build windows

package desktop

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// cryptProtectUIForbidden keeps DPAPI from raising a prompt. The app encrypts
// and decrypts on its own behalf with no user present, so any UI would be an
// unattended hang rather than a question.
const cryptProtectUIForbidden = 0x1

// protectBytes encrypts data with DPAPI, bound to the current Windows user.
//
// The ciphertext is only decryptable by the same user on the same machine (or,
// for a roaming profile, by that user elsewhere — which is the behaviour we
// want). Another account on the same PC cannot read it, and neither can a copy
// of the file taken to a different machine. That is the whole reason to use this
// instead of a plaintext file or a hard-coded obfuscation key.
func protectBytes(plain []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(plain))}
	if len(plain) > 0 {
		in.Data = &plain[0]
	}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, cryptProtectUIForbidden, &out); err != nil {
		return nil, fmt.Errorf("dpapi encrypt: %w", err)
	}
	// DPAPI hands back a block allocated with LocalAlloc, so it is ours to free
	// once copied. Leaking it would leak one block per key change, and more
	// importantly the plaintext-equivalent ciphertext would sit in the process
	// heap.
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobToBytes(out), nil
}

// unprotectBytes reverses protectBytes. It fails (rather than returning garbage)
// when the blob was produced by another user or is corrupt.
func unprotectBytes(cipher []byte) ([]byte, error) {
	if len(cipher) == 0 {
		return nil, fmt.Errorf("dpapi decrypt: empty blob")
	}
	in := windows.DataBlob{Size: uint32(len(cipher)), Data: &cipher[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, cryptProtectUIForbidden, &out); err != nil {
		return nil, fmt.Errorf("dpapi decrypt: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobToBytes(out), nil
}

// blobToBytes copies a DPAPI output blob into a Go slice.
//
// The copy is mandatory, not defensive: the blob lives in memory allocated with
// LocalAlloc that the caller frees as soon as this returns. `unsafe.Slice` would
// hand back a view into that block, so encrypting and then freeing before the
// bytes were written out produced a file of freed, garbage memory - which
// decrypted as "The data is invalid", i.e. a key that could never be read back.
func blobToBytes(b windows.DataBlob) []byte {
	if b.Data == nil || b.Size == 0 {
		return nil
	}
	out := make([]byte, b.Size)
	copy(out, unsafe.Slice(b.Data, b.Size))
	return out
}
