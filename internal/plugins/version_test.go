package plugins

import "testing"

// Version comparison, and the update path that depends on it.
//
// These matter more than their size suggests: the comparison decides whether the
// market offers an upgrade at all, and the update path is what lets a user move a
// plugin forward WITHOUT uninstalling it - which would delete the plugin's private
// directory, taking its saved settings and API key with it.

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		why  string
	}{
		{"1.0.0", "1.1.0", -1, "a patch/minor bump is newer"},
		{"1.1.0", "1.0.0", 1, "and the reverse"},
		{"1.1.0", "1.1.0", 0, "identical"},
		{"1.1", "1.1.0", 0, "a missing part is zero, so a trailing .0 is not newer"},
		{"1.1.0", "1.1", 0, "same, either way round"},
		{"1.2", "1.10", -1, "numeric, not lexicographic: 2 < 10"},
		{"2.0.0", "10.0.0", -1, "and not string order either"},
		{"v1.2.0", "1.2.0", 0, "a leading v is tolerated"},
		{"1.0.0", "", 1, "empty parses as no parts, so anything is newer"},
		{"", "1.0.0", -1, "and the reverse"},
		{"1.0.0", "v", 1, "a bare v is empty"},
		// The prerelease rule, which is the one worth stating explicitly: the
		// suffix TERMINATES the version rather than being parsed, so a beta is
		// EQUAL to its release and never offered as an upgrade. Treating it as
		// newer would let a beta silently replace a release; treating it as older
		// would nag forever about an upgrade that never counts as applied.
		{"1.2.0-beta.1", "1.2.0", 0, "a prerelease does not compare as newer"},
		{"1.2.0", "1.2.0-beta.1", 0, "nor as older"},
		{"1.2.0-beta", "1.1.0", 1, "but its numeric prefix still orders correctly"},
		{"1.2.0-beta", "1.3.0", -1, "in both directions"},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d (%s)", tc.a, tc.b, got, tc.want, tc.why)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	if !IsNewerVersion("1.1.0", "1.0.0") {
		t.Fatal("1.1.0 should be newer than 1.0.0")
	}
	if IsNewerVersion("1.0.0", "1.1.0") {
		t.Fatal("1.0.0 should not be newer than 1.1.0")
	}
	if IsNewerVersion("1.1.0", "1.1.0") {
		t.Fatal("a version should not be newer than itself")
	}
	if IsNewerVersion("1.1.0", "1.1") {
		t.Fatal("1.1.0 should not be newer than 1.1 - they are the same version")
	}
}
