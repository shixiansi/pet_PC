package plugins

import "sort"

// HostInfo is what a program tells a plugin about ITSELF: which program is mounting it, and
// which capability methods that program answers.
//
// # WHY THIS EXISTS
//
// The two hosts serve different method sets (see dispatch.go's "what is NOT shared"), and a
// plugin had no way to ASK which one it was in. It could infer the program from
// `init.surface.slot` and then consult a table it hardcoded - so "does this window serve
// `llm.profile`" was a fact every plugin author re-derived, in their own copy, with nothing
// checking it. The cost was measurable rather than theoretical: on 2026-10-30 the pet and the
// control center logged 571 lines of 「本程序不支持该方法」, half of all WARN lines, from two
// plugins that asked a question this struct answers for free.
//
// # WHY IT IS COMPUTED, NOT DECLARED
//
// `Methods` comes from the program's own registry - the same map `Dispatch` consults - so the
// answer cannot disagree with the gate. A hand-written list for the page would be a second
// thing to keep in sync, and this repository has paid for that mistake more than once (the
// permission count in three files, the background-permission list in main.js).
//
// The payload is OPTIONAL in the protocol: a plugin that ignores `host` behaves exactly as it
// did before, which is what lets this ship without republishing every installed plugin.
type HostInfo struct {
	// Program is "pet" or "workshop" - the same vocabulary `surface.slot` uses for the window,
	// so a plugin can log "running in the workshop" without translating between two names.
	Program string `json:"program"`
	// Methods is every method this program answers, sorted.
	Methods []string `json:"methods"`
}

// HostInfoOf builds a HostInfo from a method table, sorted for stability.
//
// Sorted rather than in registration order: the list crosses into a plugin's document and may
// be compared, logged or diffed, and map iteration order in Go is deliberately random.
//
// The parameter is generic over the table's VALUE because the two programs describe a method
// differently - the pet's table carries its own `pluginMethod` (a permission, a list of
// secondary gates and a run function), the market's carries `plugins.Method` - and neither
// should have to build a second map just to name its keys. Only the KEYS matter here, which is
// also the point: this list is a set of names, not a projection of anyone's struct.
func HostInfoOf[V any](program string, methods map[string]V) HostInfo {
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return HostInfo{Program: program, Methods: names}
}
