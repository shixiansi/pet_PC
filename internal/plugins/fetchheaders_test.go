package plugins

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

// TestFetchHeadersIsExactlyTheDocumentedFour pins the closed list.
//
// The list is small on purpose, and each entry is a decision rather than a permission
// (see the comment on FetchHeaders): Host can pick a virtual host, Cookie/Origin carry
// ambient credentials, Content-Length/Transfer-Encoding can contradict the body we are
// about to send. Both programs apply this list, and the plugin manual documents it, so a
// fifth entry is a change to the contract - this test makes it one.
func TestFetchHeadersIsExactlyTheDocumentedFour(t *testing.T) {
	got := make([]string, 0, len(FetchHeaders))
	for name := range FetchHeaders {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{"accept", "authorization", "content-type", "user-agent"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("FetchHeaders = %v, want %v (and a doc update in docs/plugin-dev.md §6.10)", got, want)
	}
}

// TestApplyFetchHeaderRefusesWhatIsNotOnTheList covers the names a plugin might reach for
// and must not have. The error names the four that ARE allowed, because that is the fact
// the author needs to fix their request.
func TestApplyFetchHeaderRefusesWhatIsNotOnTheList(t *testing.T) {
	for _, name := range []string{"cookie", "host", "origin", "referer", "content-length", "x-custom"} {
		header := http.Header{}
		err := ApplyFetchHeader(header, name, "v")
		if err == nil {
			t.Fatalf("header %q must be refused", name)
		}
		if !strings.Contains(err.Error(), "content-type") {
			t.Fatalf("%q: error %q must name the allowed headers", name, err)
		}
		if len(header) != 0 {
			t.Fatalf("%q: a refused header was still set: %v", name, header)
		}
	}
}

// TestApplyFetchHeaderNormalisesAndBounds pins the two mechanical rules: the name is
// lower-cased (so "Content-Type" is the same header as "content-type"), and one value is
// capped, with no newlines allowed at all - a header that can hold a CRLF is a way to write
// a second header the list never saw.
func TestApplyFetchHeaderNormalisesAndBounds(t *testing.T) {
	header := http.Header{}
	if err := ApplyFetchHeader(header, "Content-Type", "multipart/form-data; boundary=x"); err != nil {
		t.Fatalf("mixed case must be accepted: %v", err)
	}
	if got := header.Get("content-type"); got != "multipart/form-data; boundary=x" {
		t.Fatalf("content-type = %q", got)
	}

	if err := ApplyFetchHeader(http.Header{}, "user-agent", "a\r\nx-injected: 1"); err == nil {
		t.Fatal("a header value containing CRLF must be refused")
	} else if !strings.Contains(err.Error(), "换行") {
		t.Fatalf("error %q must say the value has a newline", err)
	}

	if err := ApplyFetchHeader(http.Header{}, "accept", strings.Repeat("a", MaxFetchHeaderValue+1)); err == nil {
		t.Fatal("an over-long header value must be refused")
	} else if !strings.Contains(err.Error(), "上限") && !strings.Contains(err.Error(), "超过") {
		t.Fatalf("error %q must name the limit", err)
	}
}

// TestApplyFetchHeadersIsAllOrNothing is the same rule as the permission gate: a request
// that half-happened is a state a plugin cannot reason about. The map is walked in Go's
// random order, so the assertion is on the OUTCOME (an error, and the caller is expected to
// throw the request away), not on which header was refused first.
func TestApplyFetchHeadersIsAllOrNothing(t *testing.T) {
	header := http.Header{}
	err := ApplyFetchHeaders(header, map[string]string{
		"accept": "application/json",
		"cookie": "session=1",
	})
	if err == nil {
		t.Fatal("a map containing one forbidden header must be refused")
	}
	if err := ApplyFetchHeaders(http.Header{}, map[string]string{}); err != nil {
		t.Fatalf("an empty map is not an error: %v", err)
	}
}
