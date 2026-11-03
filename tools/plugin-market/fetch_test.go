package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"PetPC/internal/plugins"
)

// This file covers the workshop's net.fetch, and only the part that the two programs must
// agree on: the REQUEST BODY.
//
// Why it deserves its own file: the pet and the control center each own their copy of this
// handler (they are separate programs with separate registries), and the rule for "what a
// plugin may send" lives in internal/plugins so that there is exactly one answer. The thing
// a test can still catch is the wiring - this program's handler forgetting to CALL the
// shared rule, which would be invisible until a plugin tried to upload a picture from the
// 插件 page and the far end received U+FFFD instead of a PNG.

// TestMarketNetFetchSendsTheBinaryBodyIntact drives the real binding: install, grant, call.
//
// The body is deliberately invalid UTF-8 in the middle, because that is the failure mode
// `bodyBase64` exists for - a byte-for-byte comparison is the only assertion that fails when
// someone "simplifies" this back to a string.
func TestMarketNetFetchSendsTheBinaryBodyIntact(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0x80}
	body := append([]byte("--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\n\r\n"), png...)
	body = append(body, []byte("\r\n--b--\r\n")...)

	var got []byte
	var gotType string
	var gotLen int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		gotType = r.Header.Get("Content-Type")
		gotLen = r.ContentLength
		_, _ = w.Write([]byte(`{"src":"https://img.example/a.png"}`))
	}))
	defer srv.Close()

	m := newTestMarket(t, "")
	id := "com.example.uploader"
	data, sum := makeZip(t, map[string]string{
		"manifest.json": testManifest(t, id, map[string]any{
			"permissions": []string{"net.fetch"},
			"netHosts":    []string{strings.TrimPrefix(srv.URL, "http://")},
		}),
		"panel.html": "<!doctype html><p>hi</p>",
	})
	if _, err := m.manager.Install(plugins.MarketEntry{
		ID: id, Name: "示例插件", Version: "1.0.0", Author: "tester", Description: "用于测试",
		Permissions: []string{"net.fetch"},
		NetHosts:    []string{strings.TrimPrefix(srv.URL, "http://")},
		Download:    serveZip(t, data),
		SHA256:      sum,
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := m.SetEnabled(id, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := m.Grant(id, []string{"net.fetch"}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	payload, err := json.Marshal(map[string]any{
		"method":     "POST",
		"url":        srv.URL + "/upload",
		"bodyBase64": base64.StdEncoding.EncodeToString(body),
		"headers":    map[string]string{"content-type": "multipart/form-data; boundary=b"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := m.PluginRequest(id, "net.fetch", string(payload))
	if err != nil {
		t.Fatalf("net.fetch with a binary body: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("the body arrived as %q, want %q", got, body)
	}
	if gotType != "multipart/form-data; boundary=b" {
		t.Fatalf("content-type = %q", gotType)
	}
	if gotLen != int64(len(body)) {
		t.Fatalf("content-length = %d, want %d", gotLen, len(body))
	}
	if !strings.Contains(out, "img.example/a.png") {
		t.Fatalf("reply = %s", out)
	}
}

// TestMarketNetFetchRefusesABadBodyBeforeSending is the same refusal the pet makes, checked
// here because "the two programs behave the same" is the whole point of the shared rule.
func TestMarketNetFetchRefusesABadBodyBeforeSending(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	m := newTestMarket(t, "")
	id := "com.example.uploader"
	host := strings.TrimPrefix(srv.URL, "http://")
	data, sum := makeZip(t, map[string]string{
		"manifest.json": testManifest(t, id, map[string]any{
			"permissions": []string{"net.fetch"},
			"netHosts":    []string{host},
		}),
		"panel.html": "<!doctype html><p>hi</p>",
	})
	if _, err := m.manager.Install(plugins.MarketEntry{
		ID: id, Name: "示例插件", Version: "1.0.0", Author: "tester", Description: "用于测试",
		Permissions: []string{"net.fetch"}, NetHosts: []string{host},
		Download: serveZip(t, data), SHA256: sum,
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := m.SetEnabled(id, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := m.Grant(id, []string{"net.fetch"}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if _, err := m.PluginRequest(id, "net.fetch",
		`{"method":"POST","url":"`+srv.URL+`/x","body":"abc","bodyBase64":"YWJj"}`); err == nil {
		t.Fatal("two bodies at once must be refused")
	} else if !strings.Contains(err.Error(), "只能给一个") {
		t.Fatalf("error %q must say the two are alternatives", err)
	}
	if hits != 0 {
		t.Fatalf("a refused body reached the network: hits=%d", hits)
	}
}

// TestMarketNetFetchSaysWhenItTruncated covers the response cap, which this handler used to
// enforce at 1 MiB WITHOUT a flag.
//
// The pet raised its ceiling to 8 MiB when the QQ sticker library showed what a silent cut costs
// (156 of 317 images landed on exactly 1 MiB of base64 - a broken animation that looked fine).
// This program kept the old number and said nothing, so the same plugin downloading the same
// picture from the 插件 page got half of it and had no way to notice. The assertion is therefore
// on BOTH halves: the size of the answer and the `truncated` flag that makes it visible.
func TestMarketNetFetchSaysWhenItTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size := plugins.MaxFetchResponseBytes
		if strings.Contains(r.URL.Path, "huge") {
			size = plugins.MaxFetchResponseBytes + (1 << 10)
		}
		_, _ = w.Write(bytes.Repeat([]byte("A"), size))
	}))
	defer srv.Close()

	m := newTestMarket(t, "")
	id := "com.example.downloader"
	host := strings.TrimPrefix(srv.URL, "http://")
	data, sum := makeZip(t, map[string]string{
		"manifest.json": testManifest(t, id, map[string]any{
			"permissions": []string{"net.fetch"},
			"netHosts":    []string{host},
		}),
		"panel.html": "<!doctype html><p>hi</p>",
	})
	if _, err := m.manager.Install(plugins.MarketEntry{
		ID: id, Name: "示例插件", Version: "1.0.0", Author: "tester", Description: "用于测试",
		Permissions: []string{"net.fetch"}, NetHosts: []string{host},
		Download: serveZip(t, data), SHA256: sum,
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := m.SetEnabled(id, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := m.Grant(id, []string{"net.fetch"}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	read := func(path string) (int, bool) {
		raw, err := m.PluginRequest(id, "net.fetch",
			`{"method":"GET","encoding":"base64","url":"`+srv.URL+path+`"}`)
		if err != nil {
			t.Fatalf("net.fetch %s: %v", path, err)
		}
		var out struct {
			Body      string `json:"body"`
			Truncated bool   `json:"truncated"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		decoded, err := base64.StdEncoding.DecodeString(out.Body)
		if err != nil {
			t.Fatalf("body is not base64: %v", err)
		}
		return len(decoded), out.Truncated
	}

	if size, truncated := read("/exact"); size != plugins.MaxFetchResponseBytes || truncated {
		t.Fatalf("at-cap body = %d bytes, truncated=%v; want %d, false", size, truncated, plugins.MaxFetchResponseBytes)
	}
	if size, truncated := read("/huge"); size != plugins.MaxFetchResponseBytes || !truncated {
		t.Fatalf("over-cap body = %d bytes, truncated=%v; want %d, true", size, truncated, plugins.MaxFetchResponseBytes)
	}
}
