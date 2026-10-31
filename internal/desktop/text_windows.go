//go:build windows

package desktop

import (
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

// DecodeChildOutput turns the bytes a child process wrote into text.
//
// # WHY THIS EXISTS
//
// The host captures a sidecar's stderr (a dying PowerShell script's exception is the only
// evidence there is), logs it at WARN - and then decoded those bytes as UTF-8. Windows
// PowerShell writes them in the machine's ANSI code page instead, so the 2026-10-31 log
// looked like this:
//
//	WARN sidecar com.shixiansi.plugin-media-sync ����ʽ������а�������ı�ǡ�)����
//
// The bytes were correct; the decoding was a guess, and the guess was wrong at exactly the
// moment the log was supposed to explain itself. AGENTS.md §3.8's "the log always has the
// evidence" is worth nothing if the evidence is unreadable.
//
// The order is deliberate:
//
//  1. valid UTF-8 is taken as it is - a script that prints UTF-8 (or PowerShell 7, or any
//     .exe we launch) must not be mangled by a conversion it does not need;
//  2. anything else is converted from the system ANSI code page, which is what a console
//     program writes on this machine;
//  3. if the conversion itself fails, the raw bytes are kept. A half-readable line beats a
//     dropped one, and "this line is garbage" is still more useful than silence.
func DecodeChildOutput(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return decodeCodePage(b, windows.GetACP())
}

// decodeCodePage converts b from cp to UTF-8, or returns it unchanged when that fails.
//
// MultiByteToWideChar with dwFlags 0 is MB_PRECOMPOSED: for the ANSI code pages a console
// child writes, that is the only sensible request - the alternative (MB_COMPOSITE) changes
// which characters the same bytes mean, which is a different bug.
func decodeCodePage(b []byte, cp uint32) string {
	if len(b) == 0 || cp == 0 {
		return string(b)
	}
	n, err := windows.MultiByteToWideChar(cp, 0, &b[0], int32(len(b)), nil, 0)
	if err != nil || n <= 0 {
		return string(b)
	}
	buf := make([]uint16, n)
	got, err := windows.MultiByteToWideChar(cp, 0, &b[0], int32(len(b)), &buf[0], n)
	if err != nil || got <= 0 {
		return string(b)
	}
	return windows.UTF16ToString(buf[:got])
}
