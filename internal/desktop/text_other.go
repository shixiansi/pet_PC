//go:build !windows

package desktop

// DecodeChildOutput has nothing to convert off Windows: the sidecars here are the same
// .ps1/.exe the pet cannot start on this platform at all, and a child would write UTF-8.
// It exists so the caller compiles without a build tag of its own - the alternative was a
// windows-only file in package main, which is how a "Windows-first" app stops building
// anywhere else and nobody notices.
func DecodeChildOutput(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return string(b)
}
