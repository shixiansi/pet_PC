package plugins

import "testing"

// TestHostInfoOfSortsAndKeepsEveryKey pins the two properties a plugin's document depends on.
//
// Sorted, because the list crosses into a plugin and may be logged, diffed or compared: Go's
// map order is deliberately random, so an unsorted answer would make two identical programs
// look different from run to run. Complete, because a missing name is a capability the plugin
// will decide it cannot use - the failure this field exists to prevent, in the other direction.
func TestHostInfoOfSortsAndKeepsEveryKey(t *testing.T) {
	table := map[string]Method{
		"storage.get": {Perm: PermStorage, Run: func(any, Call, []byte) (any, error) { return nil, nil }},
		"bubble.say":  {Perm: PermBubble, Run: func(any, Call, []byte) (any, error) { return nil, nil }},
		"net.fetch":   {Perm: PermNetFetch, Run: func(any, Call, []byte) (any, error) { return nil, nil }},
	}
	info := HostInfoOf("pet", table)
	if info.Program != "pet" {
		t.Errorf("Program = %q", info.Program)
	}
	want := []string{"bubble.say", "net.fetch", "storage.get"}
	if len(info.Methods) != len(want) {
		t.Fatalf("Methods = %v, want %v", info.Methods, want)
	}
	for i, name := range want {
		if info.Methods[i] != name {
			t.Fatalf("Methods = %v, want %v", info.Methods, want)
		}
	}
	// The table itself must come back untouched: HostInfoOf is a read.
	if len(table) != 3 {
		t.Errorf("the input table changed: %d entries", len(table))
	}
}

// TestHostInfoAgreesWithTheRegistryGate is the assertion that makes the field trustworthy.
//
// `init.host.methods` is only useful if it answers the same question `Dispatch` answers. A list
// that said "yes" for a method the gate refuses would be worse than no list at all - it would
// turn "this window cannot do that" into "it can, try it", which is the 571-line mistake.
func TestHostInfoAgreesWithTheRegistryGate(t *testing.T) {
	r := NewRegistry(nil, gateStub{})
	r.Register("storage.get", Method{Perm: PermStorage, Run: func(any, Call, []byte) (any, error) { return nil, nil }})
	r.Register("bubble.say", Method{Perm: PermBubble, Run: func(any, Call, []byte) (any, error) { return nil, nil }})

	info := HostInfoOf("pet", r.Methods())
	if len(info.Methods) != 2 {
		t.Fatalf("Methods = %v", info.Methods)
	}
	for _, name := range info.Methods {
		if !r.Has(name) {
			t.Errorf("%q is advertised but the gate does not accept it", name)
		}
	}
	// And the other direction: a name the gate accepts must be advertised.
	for name := range r.Methods() {
		found := false
		for _, advertised := range info.Methods {
			if advertised == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q is accepted by the gate but not advertised", name)
		}
	}
}

// gateStub satisfies Gate without a manager: nothing here dispatches.
type gateStub struct{}

func (gateStub) Get(string) (Installed, bool)      { return Installed{}, false }
func (gateStub) PrivateDir(string) (string, error) { return "", nil }
