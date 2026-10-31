package plugins

// Plugin-to-plugin calls: the AUTHORISING half of `plugin.call`.
//
// # WHY THIS IS NOT THE WHOLE CALL
//
// It cannot be. Wails gives Go exactly one direction into a page - `EventsEmit`, fire and
// forget - and the answer to "another plugin answers this" only exists in the page that
// hosts the frames. So the primitive is two-phased, and this file is the first phase:
//
//	1. HERE: the gate decides whether the caller may ask this question of that plugin at
//	   all, and answers with an ENVELOPE. Nothing is touched, nothing is delivered, no
//	   frame is woken up - a refusal changes nothing, which is the invariant every other
//	   capability in this package keeps (see dispatch.go).
//	2. THE PAGE (frontend/src/plugin-host.js): it takes the envelope, finds the target's
//	   mounted frame, posts one `call`, waits, and settles the caller's promise with the
//	   callee's answer. Only the page has the frames, so only the page can deliver.
//
// The split has one consequence worth stating plainly: the ANSWER never passes through
// Go, so Go cannot log its content. That matches `llm.chat` (the reply's text only reaches
// the log at DEBUG, and never the user's own text at INFO) and it is why the log line for
// a call says which pair was authorised, not what came back.
//
// # WHY THE ENVELOPE CARRIES A TICKET
//
// The page needs to match an answer to a question, and two calls to the same target may
// be in flight at once. The ticket is minted here and is the only thing that gets a reply
// accepted; the page binds it to one waiting entry and forgets it on settlement, so a
// replayed ticket cannot produce a second delivery. Go keeps no record - a ticket is
// correlation, not authorisation, and pretending otherwise would mean a table that grows
// for the life of the process.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// MaxCallPayload bounds the JSON one plugin may hand another.
//
// 64 KiB, the same number and the same reasoning as MaxShareValue: this is text travelling
// between plugins THROUGH the host, and an unbounded channel would be a way to move
// arbitrary blobs through it. A conversation turn is small; a memory dump is what `share`
// (or the callee's own storage) is for.
const MaxCallPayload = 64 << 10

// CallEnvelope is what `plugin.call` returns once the gate has allowed the call.
//
// It is not a result: it is the authority to ask, in the exact shape the page needs to
// deliver the question. Every field is copied from VALIDATED input, never taken from the
// payload verbatim - that is what stops a caller from smuggling a different target or
// method past the two manifest lists.
type CallEnvelope struct {
	// Ticket correlates the answer with this question; minted here, forgotten here.
	Ticket string `json:"ticket"`
	// Target is the plugin id that may be asked (validated against callTargets).
	Target string `json:"target"`
	// Method is the name the target declared in `serves`.
	Method string `json:"method"`
	// Payload is passed through untouched: the host has no opinion about another
	// plugin's protocol, and validating it would be inventing a schema per plugin.
	Payload json.RawMessage `json:"payload"`
}

// CallPlugin authorises one plugin-to-plugin call.
//
// The order mirrors Dispatch's and exists for the same reason - a refusal must change
// NOTHING - so every check here is a pure read:
//
//  1. the target must be a plugin id, and not the caller itself;
//  2. the method name must be a name (not a path, not a pair);
//  3. the caller's manifest must list exactly "<target>/<method>" in callTargets;
//  4. the target must be installed, enabled, and must also hold plugin.call;
//  5. the target's manifest must list the method in `serves`
//     (the declaration that makes "no such question here" an install-time fact rather
//     than a message printed after the question was delivered);
//  6. the payload must fit.
func CallPlugin(gate Gate, id string, payload []byte) (any, error) {
	var in struct {
		Target  string          `json:"target"`
		Method  string          `json:"method"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	target := strings.TrimSpace(in.Target)
	method := strings.TrimSpace(in.Method)
	if !IsValidID(target) {
		return nil, fmt.Errorf("plugin.call 的 target 非法：%q", in.Target)
	}
	if target == id {
		return nil, fmt.Errorf("插件 %s 不能调用自己：那是循环，不是调用", id)
	}
	if !servedMethodPattern.MatchString(method) {
		return nil, fmt.Errorf("plugin.call 的 method 非法：%q", in.Method)
	}
	if len(in.Payload) > MaxCallPayload {
		return nil, fmt.Errorf("plugin.call 的 payload 超过 %d 字节：%d", MaxCallPayload, len(in.Payload))
	}

	caller, ok := gate.Get(id)
	if !ok || caller.Manifest == nil {
		return nil, fmt.Errorf("插件 %s 未启用", id)
	}
	if !DeclaredCall(caller.Manifest.CallTargets, target, method) {
		return nil, fmt.Errorf("插件 %s 没有声明要调用 %s：manifest 的 callTargets 里没有这一项",
			id, CallTargetKey(target, method))
	}

	callee, ok := gate.Get(target)
	if !ok || !callee.Enabled || callee.Manifest == nil {
		return nil, fmt.Errorf("plugin.call 的目标插件 %s 未启用", target)
	}
	if !HasGranted(callee.Granted, PermPluginCall) {
		return nil, fmt.Errorf("plugin.call 的目标插件 %s 没有获得 %s 权限", target, PermPluginCall)
	}
	if !ServesMethod(callee.Manifest.Serves, method) {
		return nil, fmt.Errorf("插件 %s 没有声明会回答 %s：它的 manifest 的 serves 里没有这一项",
			target, method)
	}
	// The version floor the CALLER declared, if any. Checked here rather than at install
	// because the target may not be installed yet when the caller is (and a plugin must be
	// installable before its dependencies are). The refusal names both versions: "the shape
	// I expect from 1.2.3" is only actionable if the message says what is actually there.
	if floor, ok := DeclaredCallFloor(caller.Manifest.CallTargets, target, method); ok && floor != "" {
		if CompareVersions(callee.Manifest.Version, floor) < 0 {
			return nil, fmt.Errorf("plugin.call 的目标插件 %s 是 %s，低于你声明的 %s@>=%s：先升级它，或把 callTargets 里的版本下限改掉",
				target, callee.Manifest.Version, CallTargetKey(target, method), floor)
		}
	}

	ticket, err := newCallTicket()
	if err != nil {
		return nil, err
	}
	return CallEnvelope{Ticket: ticket, Target: target, Method: method, Payload: in.Payload}, nil
}

// newCallTicket mints one correlation id.
//
// Random rather than a counter: the id crosses into two plugin documents, and a counter
// would let the second one predict the first one's ticket. 16 bytes is far past the point
// where guessing is a strategy.
func newCallTicket() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 plugin.call 的票据失败：%w", err)
	}
	return hex.EncodeToString(buf), nil
}
