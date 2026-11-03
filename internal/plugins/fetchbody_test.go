package plugins

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// TestRequestBodyCarriesBytesIntact is the whole reason bodyBase64 exists.
//
// The bytes below deliberately are NOT valid UTF-8 (0x00 / 0xFF / 0x89PNG…). A text body
// would survive Go's JSON decoding as U+FFFD in those places - the request would still be
// sent, the far end would accept it, and what landed would be a corrupt file. That is the
// failure this helper has to make impossible, so the assertion is byte equality, not length.
func TestRequestBodyCarriesBytesIntact(t *testing.T) {
	want := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe, 0x80}
	got, err := RequestBody("", base64.StdEncoding.EncodeToString(want))
	if err != nil {
		t.Fatalf("RequestBody: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("RequestBody returned %v, want %v", got, want)
	}
}

// TestRequestBodyTextAndEmpty covers the ordinary half: a JSON body arrives unchanged,
// and "no body at all" stays distinguishable from "an empty body" (nil).
func TestRequestBodyTextAndEmpty(t *testing.T) {
	const text = `{"q":"今天 天气"}`
	got, err := RequestBody(text, "")
	if err != nil {
		t.Fatalf("RequestBody: %v", err)
	}
	if string(got) != text {
		t.Fatalf("RequestBody = %q, want %q", got, text)
	}
	if got, err := RequestBody("", ""); err != nil || got != nil {
		t.Fatalf("no body must be (nil, nil), got (%v, %v)", got, err)
	}
	// Whitespace-only is "no body", not a body of two spaces: a plugin that builds its
	// payload conditionally should not have to special-case the empty string.
	if got, err := RequestBody("   ", ""); err != nil || got != nil {
		t.Fatalf("blank body must be (nil, nil), got (%v, %v)", got, err)
	}
}

// TestRequestBodyRefusesBothBodies pins "never guess which one".
func TestRequestBodyRefusesBothBodies(t *testing.T) {
	_, err := RequestBody("abc", base64.StdEncoding.EncodeToString([]byte("xyz")))
	if err == nil {
		t.Fatal("both bodies at once must be refused")
	}
	if !strings.Contains(err.Error(), "只能给一个") {
		t.Fatalf("error %q must say the two are alternatives", err)
	}
}

// TestRequestBodyRejectsBadBase64 keeps a plugin bug from becoming a request that looks
// like it worked: the error has to name base64, because that is the thing to fix.
func TestRequestBodyRejectsBadBase64(t *testing.T) {
	for _, bad := range []string{"!!!!", "aGVsbG8*", "AAAA AAAA"} {
		if _, err := RequestBody("", bad); err == nil {
			t.Fatalf("bodyBase64 %q must be refused", bad)
		} else if !strings.Contains(err.Error(), "base64") {
			t.Fatalf("bodyBase64 %q: error %q must name base64", bad, err)
		}
	}
}

// TestRequestBodyAcceptsUnpaddedBase64 is the compatibility half: btoa pads, some clients
// do not, and a body refused only because of padding would send the author hunting for a
// bug in their encoder.
func TestRequestBodyAcceptsUnpaddedBase64(t *testing.T) {
	want := []byte("hello pet")
	encoded := base64.StdEncoding.EncodeToString(want)
	got, err := RequestBody("", strings.TrimRight(encoded, "="))
	if err != nil {
		t.Fatalf("unpadded base64 must be accepted: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("RequestBody returned %q, want %q", got, want)
	}
}

// TestRequestBodyIsCapped checks both halves of the ceiling: a body just under it goes
// through, one over it is refused with the limit in the message.
//
// The cap exists because the transport is JSON: without it a plugin could hand the host a
// half-gigabyte string and the host would allocate it (twice) before finding out.
func TestRequestBodyIsCapped(t *testing.T) {
	okBody := bytes.Repeat([]byte{0xAB}, MaxFetchUploadBytes)
	if _, err := RequestBody("", base64.StdEncoding.EncodeToString(okBody)); err != nil {
		t.Fatalf("a body exactly at the cap must be accepted: %v", err)
	}
	big := bytes.Repeat([]byte{0xAB}, MaxFetchUploadBytes+1)
	_, err := RequestBody("", base64.StdEncoding.EncodeToString(big))
	if err == nil {
		t.Fatal("a body over the cap must be refused")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Fatalf("error %q must name the limit", err)
	}
}

// TestRequestBodyRejectsEmptyBase64 makes "the plugin sent an empty file" an error rather
// than a request with no body: a far end that answers "no file" is a worse error message
// than the host saying what happened.
func TestRequestBodyRejectsEmptyBase64(t *testing.T) {
	if _, err := RequestBody("", "===="); err == nil {
		t.Fatal("an empty base64 body must be refused")
	}
}
