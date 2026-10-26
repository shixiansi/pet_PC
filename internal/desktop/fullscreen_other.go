//go:build !windows

package desktop

// IsForegroundFullscreen always reports false off Windows: there is no portable
// notion of "the foreground window", and the pet is a Windows-first app.
func IsForegroundFullscreen(Window) bool { return false }
