// Package sample embeds the built-in plugin source.
//
// This is the offline floor of the plugin market. The market's real home is the
// pet-pc-plugins repository, but it is fetched over the network, and a plugin
// tab that renders nothing when the mirror is unreachable would make the whole
// feature look broken. So one sample plugin ships inside the binary as an
// embedded catalogue plus its package.
//
// It lives here, and not in market/, because market/ is a SEPARATE git
// repository that this one ignores: //go:embed can only embed paths that are
// committed to THIS repo, and an ignored path would break the build outright.
// The packaging script writes the same bytes to both places, and a test asserts
// the two digests agree.
//
// The data files are committed on purpose. Removing any of them turns the
// fallback into an empty list - or, for a package named by index.json but absent
// here, into a download that 404s - and the pattern below then fails the build
// rather than the first fallback at runtime.
package sample

import "embed"

// FS holds the sample catalogue and its packages.
//
// The files are named explicitly rather than with a directory pattern because
// this file lives INSIDE the directory being embedded, so the design's shorthand
// "all:sample" would resolve to a nonexistent nested "sample/sample". Naming them
// also makes adding a file to this package a deliberate act: a stray download or
// a screenshot cannot silently ship inside the binary.
//
// Every package index.json lists must appear here. A missing one is not a compile
// error - embed only fails for a pattern that matches nothing - but it IS a
// runtime failure for the user who clicks install on the offline catalogue, so
// the list is kept in step by hand and checked by a test.
//
//go:embed index.json com.shixiansi.plugin-bilibili-1.0.0.zip com.shixiansi.plugin-chat-1.5.12.zip com.shixiansi.plugin-voice-1.0.1.zip com.shixiansi.plugin-qq-1.12.0.zip
var FS embed.FS
