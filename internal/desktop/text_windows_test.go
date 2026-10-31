//go:build windows

package desktop

import (
	"testing"

	"golang.org/x/sys/windows"
)

// TestDecodeChildOutputReadsTheCodePageAPowerShellWrites is the regression for the log that
// could not be read: a dying sidecar's stderr was captured correctly and decoded as UTF-8, so
// every Chinese line became "��������Ͷ�����ȱ���ҡ�}����" - at the one moment the line was
// supposed to explain what went wrong.
//
// The fixture is GBK bytes and the test SKIPS on a machine whose ANSI code page is not 936:
// the assertion is "the system code page is honoured", so a machine that cannot produce these
// bytes has nothing to say about it. Encoding the sample at run time would need the inverse
// conversion, which x/sys does not bind - and a test that drags a production helper in for its
// own convenience is the shape this repo keeps deleting.
func TestDecodeChildOutputReadsTheCodePageAPowerShellWrites(t *testing.T) {
	if cp := windows.GetACP(); cp != 936 {
		t.Skipf("this assertion needs the GBK (936) code page, this machine is %d", cp)
	}
	// "缺少" as GBK: C8B1 C9D9. Decoded as UTF-8 those four bytes are invalid, which is
	// exactly why the old `string(bytes)` produced mojibake rather than an error.
	raw := []byte{0xC8, 0xB1, 0xC9, 0xD9}
	if got := DecodeChildOutput(raw); got != "缺少" {
		t.Fatalf("GBK bytes decoded as %q, want %q", got, "缺少")
	}

	// UTF-8 is taken as it is. PowerShell 7 and every .exe we launch write UTF-8 here, and a
	// conversion they do not need would mangle them.
	if got := DecodeChildOutput([]byte("缺少")); got != "缺少" {
		t.Fatalf("valid UTF-8 was converted: %q", got)
	}

	// Empty and pure-ASCII inputs are the common case and must stay untouched.
	if got := DecodeChildOutput(nil); got != "" {
		t.Errorf("nil decoded to %q", got)
	}
	if got := DecodeChildOutput([]byte("while ($true) {")); got != "while ($true) {" {
		t.Errorf("ASCII was altered: %q", got)
	}
}
