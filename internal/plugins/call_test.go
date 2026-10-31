package plugins

// The authorising half of plugin.call, tested as the gate it is.
//
// What these cases are really pinning: a refusal must leave the world untouched, and the
// only thing that ever reaches the page is an envelope built from VALIDATED input. So the
// assertions are about which error came back and that no envelope did - plus one success
// case that checks the envelope names the pair the caller declared and the payload it sent
// (not more, not less).

import (
	"encoding/json"
	"strings"
	"testing"
)

// callGate is the two questions CallPlugin asks of a host, answered from a map.
//
// It exists rather than a *Manager because this file must be able to describe rows that a
// real install could not produce - a callee that is disabled, or one that was granted the
// permission but serves nothing - without building a data directory for each.
type callGate map[string]Installed

func (g callGate) Get(id string) (Installed, bool) {
	row, ok := g[id]
	return row, ok
}

func (g callGate) PrivateDir(id string) (string, error) { return id, nil }

// callerRow is a plugin that may call exactly `targets`.
func callerRow(id string, targets ...string) Installed {
	return Installed{ID: id, Enabled: true, Granted: []string{PermPluginCall},
		Manifest: &Manifest{ID: id, CallTargets: targets}}
}

// calleeRow is a plugin that answers exactly `serves`.
func calleeRow(id string, serves ...string) Installed {
	return Installed{ID: id, Enabled: true, Granted: []string{PermPluginCall},
		Manifest: &Manifest{ID: id, Serves: serves}}
}

func callPayload(t *testing.T, target, method, body string) []byte {
	t.Helper()
	raw := map[string]any{"target": target, "method": method}
	if body != "" {
		raw["payload"] = json.RawMessage(body)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return data
}

// TestCallPluginAuthorisesTheDeclaredPair is the one case that must SUCCEED: both halves
// declared, both switched on, and the envelope names exactly the pair and the bytes.
func TestCallPluginAuthorisesTheDeclaredPair(t *testing.T) {
	gate := callGate{
		"com.example.adapter": callerRow("com.example.adapter", "com.example.core/chat"),
		"com.example.core":    calleeRow("com.example.core", "chat"),
	}
	out, err := CallPlugin(gate, "com.example.adapter",
		callPayload(t, "com.example.core", "chat", `{"text":"在吗"}`))
	if err != nil {
		t.Fatalf("a declared pair must be authorised: %v", err)
	}
	env, ok := out.(CallEnvelope)
	if !ok {
		t.Fatalf("CallPlugin answered %T, want CallEnvelope", out)
	}
	if env.Target != "com.example.core" || env.Method != "chat" {
		t.Fatalf("envelope names %s/%s, want com.example.core/chat", env.Target, env.Method)
	}
	if string(env.Payload) != `{"text":"在吗"}` {
		t.Fatalf("envelope payload = %s, want the caller's bytes unchanged", env.Payload)
	}
	if len(env.Ticket) != 32 {
		t.Fatalf("ticket %q is %d chars, want 16 random bytes as hex", env.Ticket, len(env.Ticket))
	}
	// The ticket is correlation, not a counter: two calls must not share one.
	again, err := CallPlugin(gate, "com.example.adapter",
		callPayload(t, "com.example.core", "chat", `{"text":"还在吗"}`))
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if again.(CallEnvelope).Ticket == env.Ticket {
		t.Fatal("two calls share one ticket, so an answer could settle the wrong one")
	}
}

// TestCallPluginRefusals walks the gate in order. Each case also asserts that the error
// NAMES the thing that refused - a bare "forbidden" in a plugin log helps nobody.
func TestCallPluginRefusals(t *testing.T) {
	cases := []struct {
		name  string
		gate  callGate
		from  string
		body  []byte
		wants string
	}{
		{
			name: "target is not a plugin id",
			gate: callGate{
				"com.example.adapter": callerRow("com.example.adapter", "com.example.core/chat"),
				"com.example.core":    calleeRow("com.example.core", "chat"),
			},
			from:  "com.example.adapter",
			body:  callPayload(t, "../etc", "chat", ""),
			wants: "target",
		},
		{
			name: "calling yourself is a loop",
			gate: callGate{
				"com.example.adapter": callerRow("com.example.adapter", "com.example.adapter/chat"),
				"com.example.core":    calleeRow("com.example.core", "chat"),
			},
			from:  "com.example.adapter",
			body:  callPayload(t, "com.example.adapter", "chat", ""),
			wants: "不能调用自己",
		},
		{
			name: "method is not a name",
			gate: callGate{
				"com.example.adapter": callerRow("com.example.adapter", "com.example.core/chat"),
				"com.example.core":    calleeRow("com.example.core", "chat"),
			},
			from:  "com.example.adapter",
			body:  callPayload(t, "com.example.core", "chat/1", ""),
			wants: "method",
		},
		{
			name: "the pair is not in callTargets",
			gate: callGate{
				"com.example.adapter": callerRow("com.example.adapter", "com.example.core/chat"),
				"com.example.core":    calleeRow("com.example.core", "chat", "memory"),
			},
			from:  "com.example.adapter",
			body:  callPayload(t, "com.example.core", "memory", ""),
			wants: "callTargets",
		},
		{
			name: "the target is not installed",
			gate: callGate{
				"com.example.adapter": callerRow("com.example.adapter", "com.example.core/chat"),
			},
			from:  "com.example.adapter",
			body:  callPayload(t, "com.example.core", "chat", ""),
			wants: "未启用",
		},
		{
			name: "the target is disabled",
			gate: callGate{
				"com.example.adapter": callerRow("com.example.adapter", "com.example.core/chat"),
				"com.example.core": Installed{ID: "com.example.core", Enabled: false,
					Granted:  []string{PermPluginCall},
					Manifest: &Manifest{ID: "com.example.core", Serves: []string{"chat"}}},
			},
			from:  "com.example.adapter",
			body:  callPayload(t, "com.example.core", "chat", ""),
			wants: "未启用",
		},
		{
			name: "the target never got plugin.call",
			gate: callGate{
				"com.example.adapter": callerRow("com.example.adapter", "com.example.core/chat"),
				"com.example.core": Installed{ID: "com.example.core", Enabled: true,
					Granted:  []string{"storage"},
					Manifest: &Manifest{ID: "com.example.core", Serves: []string{"chat"}}},
			},
			from:  "com.example.adapter",
			body:  callPayload(t, "com.example.core", "chat", ""),
			wants: PermPluginCall,
		},
		{
			name: "the target does not serve that method",
			gate: callGate{
				"com.example.adapter": callerRow("com.example.adapter", "com.example.core/chat"),
				"com.example.core":    calleeRow("com.example.core", "memory"),
			},
			from:  "com.example.adapter",
			body:  callPayload(t, "com.example.core", "chat", ""),
			wants: "serves",
		},
		{
			name: "the payload is too big to carry",
			gate: callGate{
				"com.example.adapter": callerRow("com.example.adapter", "com.example.core/chat"),
				"com.example.core":    calleeRow("com.example.core", "chat"),
			},
			from:  "com.example.adapter",
			body:  callPayload(t, "com.example.core", "chat", `"`+strings.Repeat("x", MaxCallPayload)+`"`),
			wants: "payload",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := CallPlugin(tc.gate, tc.from, tc.body)
			if err == nil {
				t.Fatalf("refused call returned %#v, want an error", out)
			}
			if out != nil {
				t.Fatalf("a refused call answered %#v: a refusal must deliver nothing", out)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Fatalf("error %q does not mention %q", err, tc.wants)
			}
		})
	}
}

// TestCallPluginRejectsMalformedJSON keeps the argument decoder honest: the payload is JSON
// the plugin built itself, so a decode failure is the plugin's bug and has to say so rather
// than arriving as a nil-dereference somewhere later.
func TestCallPluginRejectsMalformedJSON(t *testing.T) {
	_, err := CallPlugin(callGate{}, "com.example.adapter", []byte("{not json"))
	if err == nil {
		t.Fatal("malformed JSON must be refused")
	}
	if !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("error %q must mention JSON", err)
	}
}

// TestCallPluginHonoursTheDeclaredVersionFloor pins the answer to the 2026-10-31 mismatch.
//
// The dashboard called `todo/stats` for a week against a todo plugin that had never declared
// it. The host's refusal was precise and the caller caught it into a log line nobody read.
// A version floor lets the CALLER say "the shape I need arrived in 1.2.2", and the refusal
// now names both versions so the message is actionable rather than mysterious.
func TestCallPluginHonoursTheDeclaredVersionFloor(t *testing.T) {
	caller := callerRow("com.example.dash", "com.example.todo/stats@>=1.2.2")

	// Satisfied: the call is authorised.
	old := calleeRow("com.example.todo", "stats")
	old.Manifest.Version = "1.2.3"
	gate := callGate{"com.example.dash": caller, "com.example.todo": old}
	if _, err := CallPlugin(gate, "com.example.dash", callPayload(t, "com.example.todo", "stats", "")); err != nil {
		t.Fatalf("a satisfied floor must authorise the call: %v", err)
	}

	// Too old: refused, and the message names the versions on both sides.
	stale := calleeRow("com.example.todo", "stats")
	stale.Manifest.Version = "1.2.1"
	gate = callGate{"com.example.dash": caller, "com.example.todo": stale}
	_, err := CallPlugin(gate, "com.example.dash", callPayload(t, "com.example.todo", "stats", ""))
	if err == nil {
		t.Fatal("a callee below the declared floor must be refused")
	}
	for _, want := range []string{"1.2.1", "1.2.2", "com.example.todo/stats"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to mention %q", err, want)
		}
	}

	// No floor declared: an old callee is still fine (most calls do not care about versions).
	plain := callerRow("com.example.dash", "com.example.todo/stats")
	gate = callGate{"com.example.dash": plain, "com.example.todo": stale}
	if _, err := CallPlugin(gate, "com.example.dash", callPayload(t, "com.example.todo", "stats", "")); err != nil {
		t.Fatalf("an entry without a floor must not check the version: %v", err)
	}
}
