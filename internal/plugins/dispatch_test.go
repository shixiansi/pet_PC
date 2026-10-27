package plugins

import "testing"

// TestHasGranted covers the one access-check primitive both programs share.
//
// It is a small function, but it is the thing every gate calls, and the pet and the
// market ask it about the same rows - which is why there is now one implementation
// instead of three. The cases below are the ones a "contains" implementation gets
// wrong: a permission whose name is a PREFIX of a granted one, the empty grant list,
// and the exact-match requirement (a substring check would make net.fetch look like
// net.credentials and vice versa).
func TestHasGranted(t *testing.T) {
	granted := []string{PermPluginShare, PermNetWS, PermAudio}

	for _, perm := range granted {
		if !HasGranted(granted, perm) {
			t.Errorf("%s must be reported as granted", perm)
		}
	}

	// PermNetCredentials is NOT in the list while PermNetWS is; a prefix or substring
	// match would confuse "the network" with "your saved credentials".
	for _, perm := range []string{
		PermStorage, PermNetFetch, PermNetCredentials, PermBubble,
		"plugin", "net.ws ", "", " ",
	} {
		if HasGranted(granted, perm) {
			t.Errorf("%q must not be reported as granted", perm)
		}
	}

	if HasGranted(nil, PermPluginShare) {
		t.Error("an empty grant list must grant nothing")
	}
	if HasGranted([]string{}, PermPluginShare) {
		t.Error("an empty grant list must grant nothing")
	}
}
