package plugins

import (
	"strconv"
	"strings"
)

// Version comparison for the plugin market.
//
// Plugins declare semantic versions in their manifests ("1.1.0"), and both programs
// have to answer "is the copy on disk behind the catalogue?".
//
// The comparison is deliberately NOT a full semver implementation. It splits on
// dots, parses each part as a number, and compares numeric parts left to right -
// which is what every version this project or its plugins uses needs.
//
// A PRE-RELEASE SUFFIX TERMINATES THE VERSION rather than being parsed, so
// "1.2.0-beta.1" compares EQUAL to "1.2.0" (not greater). That is the safe
// direction for the question actually being asked: treating a prerelease as newer
// would let a beta silently replace a release, and treating it as older would
// nag the user forever about an upgrade that never counts as applied. Equal means
// "no upgrade offered", which is the least surprising outcome for a version format
// this code does not claim to understand.
//
// It lives in internal/plugins rather than in the market program because the PET
// also needs it: the host reports when a plugin it is running was replaced on disk
// under it.

// CompareVersions orders two dotted numeric versions.
//
// Returns -1 when a < b, 0 when they are equal, and 1 when a > b. Missing parts are
// treated as zero, so "1.1" and "1.1.0" are equal and a longer version with trailing
// zeros is not spuriously "newer".
func CompareVersions(a, b string) int {
	as := versionParts(a)
	bs := versionParts(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

// IsNewerVersion reports whether candidate is strictly newer than current.
func IsNewerVersion(candidate, current string) bool {
	return CompareVersions(candidate, current) > 0
}

// versionParts splits a version into its leading numeric parts.
//
// A leading "v" is tolerated because manifests in the wild carry it. Parsing STOPS
// at the first part that is not a plain non-negative integer, which is what makes a
// pre-release suffix compare equal to its release: "1.2.0-beta.1" yields [1,2,0],
// the same as "1.2.0". Stopping rather than skipping is the whole point - a skipped
// part would be read as zero and the suffix's own digits would then make the
// prerelease look NEWER than the release.
func versionParts(v string) []int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	if v == "" {
		return nil
	}
	out := []int{}
	for _, f := range strings.Split(v, ".") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || n < 0 {
			break
		}
		out = append(out, n)
	}
	return out
}
