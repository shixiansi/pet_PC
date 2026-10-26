package plugins

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipServers maps a package's bytes to the test server serving them.
//
// install_test.go's helpers serve packages over HTTP rather than from disk, so
// the install path exercised is the one that ships: there is no file:// branch in
// the download resolver, and a test-only one would be a second way in that
// production never takes.
var zipServerFor = map[string]*httptest.Server{}

// testZipURL publishes files as a zip on a test server and returns a download
// URL plus a cleanup registration.
func testZipURL(t *testing.T, files map[string]string) string {
	t.Helper()
	data, _ := makeZip(t, files)
	return testZipBytesURL(t, data)
}

func testZipBytesURL(t *testing.T, data []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/pkg.zip"
}

// fileURL is only used by the oversized-entry test, whose point is the size
// check rather than the transport; it still has to be an http(s) URL because a
// file:// download is refused by design.
func fileURL(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	// The server is never closed: the test process exits with it, and closing it
	// here would race the request the install is about to make.
	return srv.URL + "/big.zip"
}

// testEntryFromFiles builds a catalogue entry that points at files served over
// HTTP, computing the digest from the same bytes the server will send.
func testEntryFromFiles(t *testing.T, files map[string]string, mutate func(*MarketEntry)) (MarketEntry, map[string]string) {
	t.Helper()
	data, sum := makeZip(t, files)
	entry := MarketEntry{
		ID:          "com.example.demo",
		Name:        "演示插件",
		Version:     "1.0.0",
		Author:      "tester",
		Description: "用于测试的插件",
		Permissions: []string{"todos.read", "pet.bubble"},
		Download:    testZipBytesURL(t, data),
		SHA256:      sum,
	}
	if mutate != nil {
		mutate(&entry)
	}
	return entry, files
}

// patchZipSize rewrites one entry's declared uncompressed size in the zip's
// central directory and local header, which is what the installer's pre-write
// size check reads.
//
// It is deliberately a byte-level patch rather than a fabricated archive: the
// point of the test is that the CHECK works, so the archive has to otherwise be
// a valid zip the reader will parse.
func patchZipEntrySize(t *testing.T, data []byte, name string, size uint64) []byte {
	t.Helper()
	out := append([]byte(nil), data...)
	target := []byte(name)

	// Local file header signature 0x04034b50: uncompressed size at offset 22.
	for i := 0; i+30 < len(out); i++ {
		if binary.LittleEndian.Uint32(out[i:]) != 0x04034b50 {
			continue
		}
		nameLen := int(binary.LittleEndian.Uint16(out[i+26:]))
		if i+30+nameLen > len(out) {
			continue
		}
		if !bytes.Equal(out[i+30:i+30+nameLen], target) {
			continue
		}
		binary.LittleEndian.PutUint32(out[i+22:], uint32(size))
	}
	// Central directory header signature 0x02014b50: uncompressed size at offset 24.
	for i := 0; i+46 < len(out); i++ {
		if binary.LittleEndian.Uint32(out[i:]) != 0x02014b50 {
			continue
		}
		nameLen := int(binary.LittleEndian.Uint16(out[i+28:]))
		if i+46+nameLen > len(out) {
			continue
		}
		if !bytes.Equal(out[i+46:i+46+nameLen], target) {
			continue
		}
		binary.LittleEndian.PutUint32(out[i+24:], uint32(size))
	}
	return out
}

// cachedIndex writes an index file for tests that need a pre-existing one.
func cachedIndex(t *testing.T, dir string, idx Index) {
	t.Helper()
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, indexFileName), data, 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
}

// strings.Contains is used across these tests; keeping the import here rather
// than in each file would be wrong, so this file does not re-export it.
var _ = strings.Contains
