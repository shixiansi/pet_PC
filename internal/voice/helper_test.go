package voice

import "os"

// writeFile writes a file for tests, failing the test if it cannot.
func writeFile(t testingT, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// testingT is the slice of *testing.T this helper needs.
type testingT interface {
	Helper()
	Fatalf(format string, args ...any)
}
