package plugins

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// validManifest is the field-complete example from the plan. Every rejection test
// starts from this and breaks exactly one thing, so a failure names the rule that
// was actually violated rather than an unrelated missing field.
func validManifestMap() map[string]any {
	return map[string]any{
		"id":          "com.example.plugin",
		"name":        "示例插件",
		"version":     "1.0.0",
		"author":      "tester",
		"description": "一个用于测试的插件",
		"permissions": []string{"todos.read"},
		"panel":       true,
		"entry":       "panel.html",
	}
}

func manifestJSON(t *testing.T, over map[string]any) []byte {
	t.Helper()
	m := validManifestMap()
	for k, v := range over {
		if v == nil {
			delete(m, k)
			continue
		}
		m[k] = v
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// TestValidManifestParses pins the accepting half: the field-complete example
// must parse and every value must survive unchanged. A parser that accepted the
// manifest but dropped a field would pass the rejection tests below and still
// lose the plugin's declarations.
func TestValidManifestParses(t *testing.T) {
	m, err := ParseManifest(manifestJSON(t, nil))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.ID != "com.example.plugin" || m.Name != "示例插件" || m.Version != "1.0.0" ||
		m.Author != "tester" || m.Description != "一个用于测试的插件" {
		t.Fatalf("fields not preserved: %+v", m)
	}
	if !m.Panel || m.Entry != "panel.html" {
		t.Fatalf("panel/entry not preserved: %+v", m)
	}
	if len(m.Permissions) != 1 || m.Permissions[0] != PermTodosRead {
		t.Fatalf("permissions not preserved: %v", m.Permissions)
	}
}

// TestManifestRequiredFields covers T10: every required text field missing or
// malformed, one at a time, and each failure must name the offending field.
func TestManifestRequiredFields(t *testing.T) {
	cases := []struct {
		name  string
		over  map[string]any
		field string
	}{
		{"missing id", map[string]any{"id": nil}, "id"},
		{"uppercase id", map[string]any{"id": "Com.Example"}, "id"},
		{"id with space", map[string]any{"id": "com example.plugin"}, "id"},
		{"id without a dot", map[string]any{"id": "plainslug"}, "id"},
		{"missing name", map[string]any{"name": nil}, "name"},
		{"missing author", map[string]any{"author": nil}, "author"},
		{"missing description", map[string]any{"description": nil}, "description"},
		{"missing version", map[string]any{"version": nil}, "version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseManifest(manifestJSON(t, tc.over))
			if err == nil {
				t.Fatalf("expected a rejection")
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("error %q does not name the field %q", err, tc.field)
			}
		})
	}
}

// TestManifestEntryRules covers T11: the entry path rules and the requirement
// that a plugin declares at least one interface surface.
func TestManifestEntryRules(t *testing.T) {
	cases := []struct {
		name  string
		over  map[string]any
		wants string
	}{
		{"traversal", map[string]any{"entry": "../x.html"}, "entry"},
		{"absolute", map[string]any{"entry": "/abs.html"}, "entry"},
		{"backslash", map[string]any{"entry": `a\b.html`}, "entry"},
		{"not html", map[string]any{"entry": "panel.js"}, "entry"},
		{"empty", map[string]any{"entry": ""}, "entry"},
		{"no surface at all", map[string]any{"panel": false}, "surface"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseManifest(manifestJSON(t, tc.over))
			if err == nil {
				t.Fatalf("expected a rejection")
			}
			// The last case is refused for having no interface at all, which the
			// message reports by naming panel and surface together.
			if !strings.Contains(err.Error(), tc.wants) {
				t.Fatalf("error %q does not mention %q", err, tc.wants)
			}
		})
	}
	// A surface alone (no panel) is a legitimate plugin: the two are alternatives.
	if _, err := ParseManifest(manifestJSON(t, map[string]any{
		"panel":   false,
		"surface": []map[string]any{{"dx": 10, "dy": 10, "w": 100, "h": 100, "radius": 4}},
	})); err != nil {
		t.Fatalf("a surface-only plugin must parse: %v", err)
	}
}

// TestManifestVersionFormat covers T12 with the four accepted and five rejected
// shapes the plan names.
func TestManifestVersionFormat(t *testing.T) {
	good := []string{"1.0.0", "10.20.30", "1.0.0-beta.1", "1.0.0-beta.1.2"}
	for _, v := range good {
		t.Run("accept "+v, func(t *testing.T) {
			if _, err := ParseManifest(manifestJSON(t, map[string]any{"version": v})); err != nil {
				t.Fatalf("must parse: %v", err)
			}
		})
	}
	bad := []string{"1.0", "v1.0.0", "1.0.0.0", "", "1.0.0-"}
	for _, v := range bad {
		t.Run("reject "+v, func(t *testing.T) {
			_, err := ParseManifest(manifestJSON(t, map[string]any{"version": v}))
			if err == nil {
				t.Fatalf("%q must be rejected", v)
			}
			if !strings.Contains(err.Error(), "version") {
				t.Fatalf("error %q does not name version", err)
			}
		})
	}
}

// TestAllowedPermissionsIsTheSeventeen pins the vocabulary itself. The dispatcher's
// completeness check compares against this list, so a permission added or lost
// here would silently change what users are asked about.
//
// The count is deliberately asserted as well as the members: a permission added
// without a method behind it is a switch the user is asked about that does
// nothing, and the count is what makes that a visible two-place edit rather than
// a silent append.
//
// The count was thirteen until `process` was appended (the host gained the
// `process.launch` method, which is the thing that makes it a permission rather
// than a flag - see TestCapabilityTableIsComplete), fourteen until
// `net.credentials` was appended. That one is the second gate of net.fetch rather
// than a method of its own, so its "method behind it" is recorded in that table
// entry's `also` field; TestCapabilityTableIsComplete counts those too, and
// TestManifestCredentialsRequiresNetFetch keeps it from being declared alone.
// Fifteen until `plugin.share` arrived with share.set/get/remove, which is what
// lets two plugins exchange a job without either one calling the other.
// Sixteen until `timer.control` arrived with timer.start/pause/resume/stop: the
// timer could already be READ by a plugin, but not driven, and "you may look at my
// countdown" was never a sensible umbrella for "you may start a session in my name".
//
// Back down to fifteen when both timer permissions retired: the timer those five
// methods drove belongs to the pomodoro plugin now, so the host has no plugin-facing
// timer surface left to gate (openspec/changes/todo-pomodoro-as-plugins).
func TestAllowedPermissionsIsTheFifteen(t *testing.T) {
	want := []string{
		"todos.read", "todos.write", "pet.bubble",
		"events", "net.fetch", "net.credentials", "storage", "pet.surface", "llm.chat",
		"plugin.config", "audio", "net.ws", "llm.borrow", "process", "plugin.share",
	}
	if len(AllowedPermissions) != len(want) {
		t.Fatalf("AllowedPermissions has %d entries, want %d", len(AllowedPermissions), len(want))
	}
	// Order is what the install dialog draws, so it is part of the pinned list
	// rather than incidental: net.credentials belongs directly after the
	// permission it gates, or a user reads "访问网络" and "以你的登录身份发请求"
	// as two unrelated switches.
	for i, name := range AllowedPermissions {
		if want[i] != name {
			t.Fatalf("AllowedPermissions[%d] = %q, want %q (order is the dialog's order)", i, name, want[i])
		}
	}
	for _, w := range want {
		if !IsAllowedPermission(w) {
			t.Errorf("%s is not allowed", w)
		}
		if PermissionLabels[w] == "" {
			t.Errorf("%s has no label, so the install dialog would show a bare id", w)
		}
	}
	if IsAllowedPermission("fs.read") {
		t.Errorf("fs.read must not be an allowed permission")
	}
	// A headless frame is requested by the manifest's `background` field and
	// gated on an existing permission (`events`, `net.ws` or `net.fetch`), so
	// there must be NO background permission: one would have no method behind it,
	// which is the invariant this test protects.
	if IsAllowedPermission("background") {
		t.Error("a headless frame is gated on events/net.ws/net.fetch; a separate background permission would have no method behind it")
	}
}

// TestManifestPermissions covers T13: unknown names and duplicates are refused,
// and each message names the culprit.
func TestManifestPermissions(t *testing.T) {
	if _, err := ParseManifest(manifestJSON(t, map[string]any{"permissions": []string{}})); err != nil {
		t.Fatalf("an empty permission list is legal: %v", err)
	}
	_, err := ParseManifest(manifestJSON(t, map[string]any{"permissions": []string{"fs.read"}}))
	if err == nil {
		t.Fatal("fs.read must be rejected")
	}
	if !strings.Contains(err.Error(), "fs.read") || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("error %q must name both the permission and the field", err)
	}
	_, err = ParseManifest(manifestJSON(t, map[string]any{"permissions": []string{"todos.read", "todos.read"}}))
	if err == nil {
		t.Fatal("a duplicate permission must be rejected")
	}
	if !strings.Contains(err.Error(), "todos.read") {
		t.Fatalf("error %q must name the duplicate", err)
	}
}

// TestManifestSurfaceBounds covers T14.
func TestManifestSurfaceBounds(t *testing.T) {
	rect := map[string]any{"dx": 10, "dy": 10, "w": 100, "h": 100, "radius": 4}
	bad := []struct {
		name string
		rect map[string]any
	}{
		{"w=0", map[string]any{"dx": 0, "dy": 0, "w": 0, "h": 100, "radius": 0}},
		{"h=-1", map[string]any{"dx": 0, "dy": 0, "w": 100, "h": -1, "radius": 0}},
		{"radius=201", map[string]any{"dx": 0, "dy": 0, "w": 100, "h": 100, "radius": 201}},
		{"dx=4001", map[string]any{"dx": 4001, "dy": 0, "w": 100, "h": 100, "radius": 0}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseManifest(manifestJSON(t, map[string]any{"surface": []map[string]any{tc.rect}}))
			if err == nil {
				t.Fatal("expected a rejection")
			}
			if !strings.Contains(err.Error(), "surface") {
				t.Fatalf("error %q must name surface", err)
			}
		})
	}

	eight := []map[string]any{rect, rect, rect, rect, rect, rect, rect, rect}
	if _, err := ParseManifest(manifestJSON(t, map[string]any{"surface": eight})); err != nil {
		t.Fatalf("eight rectangles must parse: %v", err)
	}

	nine := []map[string]any{rect, rect, rect, rect, rect, rect, rect, rect, rect}
	_, err := ParseManifest(manifestJSON(t, map[string]any{"surface": nine}))
	if err == nil {
		t.Fatal("nine rectangles must be rejected")
	}
	if !strings.Contains(err.Error(), "8") || !strings.Contains(err.Error(), "surface") {
		t.Fatalf("error %q must name surface and the limit 8", err)
	}
}

// TestManifestNetHosts covers T15: the format rule and the net.fetch pairing.
func TestManifestNetHosts(t *testing.T) {
	_, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"net.fetch"},
		"netHosts":    []string{"bad host"},
	}))
	if err == nil {
		t.Fatal("a host with a space must be rejected")
	}
	if !strings.Contains(err.Error(), "netHosts") {
		t.Fatalf("error %q must name netHosts", err)
	}

	_, err = ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"net.fetch"},
		"netHosts":    []string{},
	}))
	if err == nil {
		t.Fatal("net.fetch without an allow-list must be rejected")
	}
	if !strings.Contains(err.Error(), "netHosts") {
		t.Fatalf("error %q must name netHosts", err)
	}

	if _, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"net.fetch"},
		"netHosts":    []string{"api.deepseek.com", "example.com:8443"},
	})); err != nil {
		t.Fatalf("a well-formed allow-list must parse: %v", err)
	}

	// Without net.fetch the allow-list may be absent entirely.
	if _, err := ParseManifest(manifestJSON(t, nil)); err != nil {
		t.Fatalf("no netHosts and no net.fetch must parse: %v", err)
	}
}

// TestHostAllowedIsExact is the allow-list's own rule: a suffix match would turn
// "api.example.com" into a wildcard for the whole domain, which is the classic
// way an allow-list quietly stops restricting anything.
func TestHostAllowedIsExact(t *testing.T) {
	hosts := []string{"api.example.com", "127.0.0.1:8080"}
	cases := []struct {
		host string
		want bool
	}{
		{"api.example.com", true},
		{"API.Example.COM", true},
		{"example.com", false},
		{"notapi.example.com", false},
		{"evil.api.example.com", false},
		{"127.0.0.1:8080", true},
		{"127.0.0.1", false},
		{"127.0.0.1:9090", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := HostAllowed(hosts, tc.host); got != tc.want {
			t.Errorf("HostAllowed(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

// TestValidRelPath covers the URL-side traversal guard.
func TestValidRelPath(t *testing.T) {
	good := []string{"panel.html", "assets/app.js", "a/b/c.css"}
	for _, p := range good {
		if !ValidRelPath(p) {
			t.Errorf("%q must be accepted", p)
		}
	}
	bad := []string{"", "../x", "a/../b", "/abs", `a\b`, "a//b", "./x", ".."}
	for _, p := range bad {
		if ValidRelPath(p) {
			t.Errorf("%q must be rejected", p)
		}
	}
}

// TestValidID pins the id shape, which the manager relies on to keep every
// filepath.Join inside the plugins directory.
func TestValidID(t *testing.T) {
	for _, id := range []string{"com.shixiansi.plugin-chat", "a.b", "com.a1.b2"} {
		if !IsValidID(id) {
			t.Errorf("%q must be a valid id", id)
		}
	}
	for _, id := range []string{"", "..", "com/evil", `com\evil`, "Com.Evil", "com..evil", "com. evil"} {
		if IsValidID(id) {
			t.Errorf("%q must not be a valid id", id)
		}
	}
}

// TestParseManifestRejectsNonJSON keeps a mangled file from being read as an
// empty (and therefore permissive) manifest.
func TestParseManifestRejectsNonJSON(t *testing.T) {
	if _, err := ParseManifest([]byte("{not json")); err == nil {
		t.Fatal("malformed JSON must be rejected")
	}
}

// TestManifestEllipseFlag pins the ellipse flag's parsing and that it does not
// exempt a block from the width/height rule.
func TestManifestEllipseFlag(t *testing.T) {
	m, err := ParseManifest(manifestJSON(t, map[string]any{
		"panel":   false,
		"surface": []map[string]any{{"dx": 10, "dy": 10, "w": 34, "h": 20, "ellipse": true}},
	}))
	if err != nil {
		t.Fatalf("an ellipse block must parse: %v", err)
	}
	if len(m.Surface) != 1 || !m.Surface[0].Ellipse {
		t.Fatalf("ellipse not preserved: %+v", m.Surface)
	}

	_, err = ParseManifest(manifestJSON(t, map[string]any{
		"panel":   false,
		"surface": []map[string]any{{"dx": 10, "dy": 10, "w": 0, "h": 20, "ellipse": true}},
	}))
	if err == nil {
		t.Fatal("an ellipse with w=0 must be rejected")
	}
	if !strings.Contains(err.Error(), "宽高必须为正") {
		t.Fatalf("error %q must name the size rule", err)
	}
}

// TestManifestBackground pins the headless-frame declaration and its pairing
// rule: asking for a background frame without either permission that fills it is
// refused, because the frame's whole purpose is receiving something while no
// interface is open, and the user has to have been shown that capability.
func TestManifestBackground(t *testing.T) {
	// The honest declaration: a background frame AND the permission it runs on.
	m, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"events"},
		"background":  true,
	}))
	if err != nil {
		t.Fatalf("background with events must parse: %v", err)
	}
	if !m.Background {
		t.Fatal("background was not preserved")
	}

	// The second honest declaration: a frame whose traffic arrives over a socket
	// the plugin holds open (the QQ bridge). `events` would be the wrong thing to
	// demand - it subscribes to nothing on the host side - and demanding it anyway
	// would ask the user to grant a switch the plugin never reads.
	ws, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"net.ws"},
		"background":  true,
	}))
	if err != nil {
		t.Fatalf("background with net.ws must parse: %v", err)
	}
	if !ws.Background {
		t.Fatal("background was not preserved for a net.ws plugin")
	}

	// The third honest declaration: a POLLING plugin. It has nothing to be pushed
	// and decides when to ask (the Bilibili plugin's schedule), so neither events
	// nor a socket describes it - what it needs from the host is permission to
	// make a request at a time of its own choosing, which net.fetch already is.
	// This is the case that made the entry rule's sibling three-way.
	poll, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"net.fetch"},
		"netHosts":    []string{"api.example.com"},
		"background":  true,
	}))
	if err != nil {
		t.Fatalf("background with net.fetch must parse: %v", err)
	}
	if !poll.Background {
		t.Fatal("background was not preserved for a polling plugin")
	}

	// Absent by default, so every existing manifest is unaffected.
	plain, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"events"},
	}))
	if err != nil {
		t.Fatalf("a manifest without background must parse: %v", err)
	}
	if plain.Background {
		t.Error("background defaults to false")
	}

	// Asking for a permanent frame while requesting no way to receive anything in
	// it: a silently useless configuration, refused where the author can see it.
	_, err = ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"storage"},
		"background":  true,
	}))
	if err == nil {
		t.Fatal("background without events, net.ws or net.fetch must be rejected")
	}
	if !strings.Contains(err.Error(), "background") ||
		!strings.Contains(err.Error(), "events") || !strings.Contains(err.Error(), "net.ws") ||
		!strings.Contains(err.Error(), "net.fetch") {
		t.Fatalf("error %q must name background, events, net.ws and net.fetch", err)
	}

	// A background-only plugin is LEGAL, and that matters: its interface is hosted
	// by another program (the plugin market draws the voice plugin's workshop), so
	// it has no mount point in the pet while still needing the frame to hear the
	// timer. This is the case that made the entry rule three-way.
	only, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"events"},
		"background":  true,
		"panel":       false,
	}))
	if err != nil {
		t.Fatalf("a background-only plugin must parse: %v", err)
	}
	if !only.Background || only.Panel || len(only.Surface) != 0 {
		t.Fatalf("a background-only plugin parsed as %+v", only)
	}

	// But SOMETHING must load the entry: declaring none of the three means the
	// entry names a file nothing would ever run.
	_, err = ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"events"},
		"panel":       false,
	}))
	if err == nil {
		t.Fatal("a plugin with no panel, no surface and no background must be rejected")
	}
	if !strings.Contains(err.Error(), "background") {
		t.Fatalf("error %q must name background as one of the three mount points", err)
	}
}

// TestManifestCredentialsRequiresNetFetch covers net.credentials' pairing rule:
// it is the second gate of net.fetch, so declaring it alone is a switch that
// unlocks nothing.
func TestManifestCredentialsRequiresNetFetch(t *testing.T) {
	// Paired with the permission it gates: legal.
	if _, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"net.fetch", "net.credentials"},
		"netHosts":    []string{"api.example.com"},
	})); err != nil {
		t.Fatalf("net.credentials with net.fetch must parse: %v", err)
	}

	// Alone: refused, naming both permissions so the author knows what to add.
	_, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"net.credentials"},
	}))
	if err == nil {
		t.Fatal("net.credentials without net.fetch must be rejected")
	}
	if !strings.Contains(err.Error(), "net.credentials") || !strings.Contains(err.Error(), "net.fetch") {
		t.Fatalf("error %q must name net.credentials and net.fetch", err)
	}
}

// TestManifestShareRead covers the read half of the cross-plugin channel: the pairs are
// "<id>/<name>", the permission must be held, and a plugin cannot declare itself.
func TestManifestShareRead(t *testing.T) {
	// Declared pair with the permission: legal.
	if _, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"plugin.share"},
		"shareRead":   []string{"com.example.qq/bili-jobs"},
	})); err != nil {
		t.Fatalf("a declared pair with plugin.share must parse: %v", err)
	}
	// Without the permission it is a promise nothing enforces.
	_, err := ParseManifest(manifestJSON(t, map[string]any{
		"shareRead": []string{"com.example.qq/bili-jobs"},
	}))
	if err == nil {
		t.Fatal("shareRead without plugin.share must be refused")
	}
	if !strings.Contains(err.Error(), "plugin.share") {
		t.Fatalf("error %q must name the permission", err)
	}
	// Malformed entries are refused: the two halves become a directory and a file name.
	for _, bad := range []string{
		"no-separator",
		"com.example.qq",
		"com.example.qq/a/b",
		"../etc/passwd",
		"com.example.qq/",
	} {
		if _, err := ParseManifest(manifestJSON(t, map[string]any{
			"permissions": []string{"plugin.share"},
			"shareRead":   []string{bad},
		})); err == nil {
			t.Fatalf("shareRead entry %q must be refused", bad)
		}
	}
	// Listing yourself is noise: reading your own values is implicit.
	if _, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"plugin.share"},
		"shareRead":   []string{"com.example.plugin/jobs"},
	})); err == nil {
		t.Fatal("a manifest must not list itself in shareRead")
	}
}

// TestManifestSelectOptions covers the picker control: options are required to be usable,
// unique, labelled, and only on a select.
//
// The motivating case is a plugin that used to ask for "分区（逗号分隔 rid，例如 36=知识）":
// a settings field that demanded the user memorise a table. A picker is only an improvement
// if the declaration behind it cannot be half-written, which is what these cases pin.
func TestManifestSelectOptions(t *testing.T) {
	base := func(item map[string]any) map[string]any {
		return map[string]any{"config": []any{item}}
	}
	options := []any{
		map[string]any{"value": "36", "label": "知识"},
		map[string]any{"value": "188", "label": "科技"},
	}

	// The shape the Bilibili plugin ships: a multi-select whose value is comma-joined.
	m, err := ParseManifest(manifestJSON(t, base(map[string]any{
		"key": "rids", "label": "分区", "type": "select", "multiple": true,
		"default": "36,188", "options": options,
	})))
	if err != nil {
		t.Fatalf("a select with options must parse: %v", err)
	}
	item, _ := m.ConfigItem("rids")
	if !item.Multiple || len(item.Options) != 2 || item.Options[0].Label != "知识" {
		t.Fatalf("the declaration lost its options: %+v", item)
	}

	bad := []struct {
		name  string
		item  map[string]any
		wants string
	}{
		{"one option", map[string]any{
			"key": "a", "label": "A", "type": "select",
			"options": []any{map[string]any{"value": "1", "label": "一"}},
		}, "2–"},
		{"duplicate value", map[string]any{
			"key": "a", "label": "A", "type": "select",
			"options": []any{
				map[string]any{"value": "1", "label": "一"},
				map[string]any{"value": "1", "label": "壹"},
			},
		}, "重复选项"},
		{"empty label", map[string]any{
			"key": "a", "label": "A", "type": "select",
			"options": []any{
				map[string]any{"value": "1", "label": "一"},
				map[string]any{"value": "2", "label": "  "},
			},
		}, "缺少 label"},
		{"secret select", map[string]any{
			"key": "a", "label": "A", "type": "select", "secret": true, "options": options,
		}, "不能同时是 secret"},
		{"options on text", map[string]any{
			"key": "a", "label": "A", "type": "text", "options": options,
		}, "type 不是 select"},
		{"multiple on text", map[string]any{
			"key": "a", "label": "A", "type": "text", "multiple": true,
		}, "type 不是 select"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseManifest(manifestJSON(t, base(tc.item)))
			if err == nil {
				t.Fatal("this declaration must be refused")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Fatalf("error %q must mention %q", err, tc.wants)
			}
		})
	}
}

// TestManifestShareLLM covers the lending flag: it is opt-in, defaults to false,
// and is refused unless the plugin could actually serve the request it is
// advertising.
func TestManifestShareLLM(t *testing.T) {
	m, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"llm.chat"},
		"shareLLM":    true,
	}))
	if err != nil {
		t.Fatalf("shareLLM with llm.chat must parse: %v", err)
	}
	if !m.ShareLLM {
		t.Fatal("shareLLM was not preserved")
	}

	plain, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"llm.chat"},
	}))
	if err != nil {
		t.Fatalf("a manifest without shareLLM must parse: %v", err)
	}
	if plain.ShareLLM {
		t.Error("shareLLM defaults to false: lending must be opt-in")
	}

	// Lending without the capability that reads the settings would advertise a
	// configured model no code path ever consults.
	if _, err := ParseManifest(manifestJSON(t, map[string]any{
		"permissions": []string{"storage"},
		"shareLLM":    true,
	})); err == nil {
		t.Fatal("shareLLM without llm.chat must be rejected")
	} else if !strings.Contains(err.Error(), "shareLLM") || !strings.Contains(err.Error(), "llm.chat") {
		t.Fatalf("error %q must name shareLLM and llm.chat", err)
	}
}

// TestManifestAssets covers the extra files a package may carry.
//
// The path rule is the point: the packaging script joins these names onto the
// plugin directory, so a ".." segment or an absolute path would let a manifest
// pull a file from outside the plugin into a published package.
func TestManifestAssets(t *testing.T) {
	m, err := ParseManifest(manifestJSON(t, map[string]any{
		"assets": []string{"lines/focus_end.wav", "lines/break_end.wav"},
	}))
	if err != nil {
		t.Fatalf("legal asset paths must parse: %v", err)
	}
	if len(m.Assets) != 2 {
		t.Fatalf("assets not preserved: %+v", m.Assets)
	}

	bad := []struct {
		name string
		path string
	}{
		{"traversal", "../secret.wav"},
		{"absolute", "/etc/passwd"},
		{"backslash", `lines\a.wav`},
		{"empty", ""},
		{"dot", "."},
		{"interior traversal", "lines/../../x.wav"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseManifest(manifestJSON(t, map[string]any{
				"assets": []string{tc.path},
			}))
			if err == nil {
				t.Fatalf("asset path %q must be rejected", tc.path)
			}
			if !strings.Contains(err.Error(), "assets") {
				t.Fatalf("error %q must name assets", err)
			}
		})
	}

	// A duplicate would make the packager add the same member twice.
	_, err = ParseManifest(manifestJSON(t, map[string]any{
		"assets": []string{"a.wav", "a.wav"},
	}))
	if err == nil {
		t.Fatal("a duplicate asset path must be rejected")
	}
	if !strings.Contains(err.Error(), "a.wav") {
		t.Fatalf("error %q must name the duplicate", err)
	}

	// The bound, on both sides of the edge.
	atLimit := make([]string, maxAssets)
	for i := range atLimit {
		atLimit[i] = fmt.Sprintf("l/%d.wav", i)
	}
	if _, err := ParseManifest(manifestJSON(t, map[string]any{"assets": atLimit})); err != nil {
		t.Fatalf("%d assets must parse: %v", maxAssets, err)
	}
	over := append(append([]string(nil), atLimit...), "l/extra.wav")
	_, err = ParseManifest(manifestJSON(t, map[string]any{"assets": over}))
	if err == nil {
		t.Fatalf("%d assets must be rejected", maxAssets+1)
	}
	if !strings.Contains(err.Error(), "assets") || !strings.Contains(err.Error(), "32") {
		t.Fatalf("error %q must name assets and the limit 32", err)
	}
}

// TestManifestPanelSlot covers T30: the closed set of slot names and the one
// pairing rule.
func TestManifestPanelSlot(t *testing.T) {
	_, err := ParseManifest(manifestJSON(t, map[string]any{"panelSlot": "todo"}))
	if err == nil {
		t.Fatal("an unknown panelSlot must be rejected")
	}
	if !strings.Contains(err.Error(), "panelSlot") {
		t.Fatalf("error %q must name panelSlot", err)
	}

	_, err = ParseManifest(manifestJSON(t, map[string]any{"panelSlot": "chat", "panel": false}))
	if err == nil {
		t.Fatal("panelSlot=chat without panel must be rejected")
	}

	m, err := ParseManifest(manifestJSON(t, map[string]any{"panelSlot": "chat", "panel": true}))
	if err != nil {
		t.Fatalf("panelSlot=chat with panel must parse: %v", err)
	}
	if m.PanelSlot != "chat" {
		t.Fatalf("panelSlot not preserved: %q", m.PanelSlot)
	}

	plain, err := ParseManifest(manifestJSON(t, nil))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if plain.PanelSlot != "" {
		t.Fatalf("default panelSlot must be empty, got %q", plain.PanelSlot)
	}
}

// configItems is a helper for the config-declaration tests.
func configItems(items ...map[string]any) []map[string]any { return items }

// TestManifestConfigKeyRules covers T25: the key shape, both halves.
func TestManifestConfigKeyRules(t *testing.T) {
	good := []string{"llmBaseUrl", "a-b_c", "K1"}
	for _, key := range good {
		t.Run("accept "+key, func(t *testing.T) {
			if _, err := ParseManifest(manifestJSON(t, map[string]any{
				"config": configItems(map[string]any{"key": key, "label": "标签", "type": "text"}),
			})); err != nil {
				t.Fatalf("%q must be accepted: %v", key, err)
			}
		})
	}
	bad := []string{"a b", "../x", strings.Repeat("k", 65), ""}
	for _, key := range bad {
		t.Run("reject "+key, func(t *testing.T) {
			_, err := ParseManifest(manifestJSON(t, map[string]any{
				"config": configItems(map[string]any{"key": key, "label": "标签", "type": "text"}),
			}))
			if err == nil {
				t.Fatalf("%q must be rejected", key)
			}
			if !strings.Contains(err.Error(), "config") || !strings.Contains(err.Error(), key) {
				t.Fatalf("error %q must name config and %q", err, key)
			}
		})
	}
}

// TestManifestConfigTypeRules covers T26: the type enum and its agreement with
// the secret flag.
func TestManifestConfigTypeRules(t *testing.T) {
	_, err := ParseManifest(manifestJSON(t, map[string]any{
		"config": configItems(map[string]any{"key": "k", "label": "标签", "type": "date"}),
	}))
	if err == nil {
		t.Fatal("type=date must be rejected")
	}
	_, err = ParseManifest(manifestJSON(t, map[string]any{
		"config": configItems(map[string]any{"key": "k", "label": "标签", "type": "secret"}),
	}))
	if err == nil {
		t.Fatal("type=secret without secret=true must be rejected")
	}
	_, err = ParseManifest(manifestJSON(t, map[string]any{
		"config": configItems(map[string]any{"key": "k", "label": "标签", "type": "text", "secret": true}),
	}))
	if err == nil {
		t.Fatal("secret=true with type=text must be rejected")
	}
	if _, err := ParseManifest(manifestJSON(t, map[string]any{
		"config": configItems(map[string]any{"key": "k", "label": "标签", "type": "secret", "secret": true}),
	})); err != nil {
		t.Fatalf("type=secret + secret=true must be accepted: %v", err)
	}
	if _, err := ParseManifest(manifestJSON(t, map[string]any{
		"config": configItems(map[string]any{"key": "k", "label": "标签", "type": "text"}),
	})); err != nil {
		t.Fatalf("type=text must be accepted: %v", err)
	}
}

// TestManifestConfigDuplicatesAndLimit covers T27 and T28.
func TestManifestConfigDuplicatesAndLimit(t *testing.T) {
	_, err := ParseManifest(manifestJSON(t, map[string]any{
		"config": configItems(
			map[string]any{"key": "dup", "label": "标签", "type": "text"},
			map[string]any{"key": "dup", "label": "标签", "type": "text"},
		),
	}))
	if err == nil {
		t.Fatal("a duplicate config key must be rejected")
	}
	if !strings.Contains(err.Error(), "dup") {
		t.Fatalf("error %q must name the duplicate key", err)
	}

	if _, err := ParseManifest(manifestJSON(t, map[string]any{"config": nil})); err != nil {
		t.Fatalf("an absent config must parse: %v", err)
	}
	m, err := ParseManifest(manifestJSON(t, map[string]any{"config": nil}))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(m.Config) != 0 {
		t.Fatalf("absent config must yield no items, got %d", len(m.Config))
	}

	// The cap is asserted AT the boundary rather than as "the number I remember":
	// a test that hardcoded 16 would have to be edited (and could quietly stop
	// testing anything) the moment a plugin legitimately needed more settings -
	// which is exactly what happened when the QQ plugin gained vision settings.
	atCap := make([]map[string]any, 0, maxConfigItems)
	for i := 0; i < maxConfigItems; i++ {
		atCap = append(atCap, map[string]any{
			// Numeric suffixes, not letters: the key pattern allows digits, and
			// 'a'+i past 26 produces a character the pattern refuses - which is a
			// test bug that would look like a cap failure.
			"key":   "k" + fmt.Sprint(i),
			"label": "标签",
			"type":  "text",
		})
	}
	if _, err := ParseManifest(manifestJSON(t, map[string]any{"config": atCap})); err != nil {
		t.Fatalf("%d items must be accepted: %v", maxConfigItems, err)
	}
	overCap := append(atCap, map[string]any{"key": "extra", "label": "标签", "type": "text"})
	_, err = ParseManifest(manifestJSON(t, map[string]any{"config": overCap}))
	if err == nil {
		t.Fatalf("%d items must be rejected", maxConfigItems+1)
	}
	if !strings.Contains(err.Error(), fmt.Sprint(maxConfigItems)) || !strings.Contains(err.Error(), "config") {
		t.Fatalf("error %q must name config and %d", err, maxConfigItems)
	}
}

// TestManifestConfigHostWhitelist covers T29: at most one hostWhitelist item,
// and it does NOT require the plugin to declare net.fetch.
func TestManifestConfigHostWhitelist(t *testing.T) {
	// One hostWhitelist item with no net.fetch at all is accepted: whether the
	// list is consulted is a property of the calling path, not of the manifest.
	m, err := ParseManifest(manifestJSON(t, map[string]any{
		"config": configItems(map[string]any{
			"key": "llmBaseUrl", "label": "接口地址", "type": "text", "hostWhitelist": true,
		}),
	}))
	if err != nil {
		t.Fatalf("a single hostWhitelist item without net.fetch must be accepted: %v", err)
	}
	if len(m.Config) != 1 || !m.Config[0].HostWhitelist {
		t.Fatalf("hostWhitelist not preserved: %+v", m.Config)
	}

	_, err = ParseManifest(manifestJSON(t, map[string]any{
		"config": configItems(
			map[string]any{"key": "a", "label": "标签", "type": "text", "hostWhitelist": true},
			map[string]any{"key": "b", "label": "标签", "type": "text", "hostWhitelist": true},
		),
	}))
	if err == nil {
		t.Fatal("two hostWhitelist items must be rejected")
	}
	if !strings.Contains(err.Error(), "hostWhitelist") || !strings.Contains(err.Error(), "至多") {
		t.Fatalf("error %q must name hostWhitelist and 至多", err)
	}
}

// TestManifestConfigItemAccessor covers T33.
func TestManifestConfigItemAccessor(t *testing.T) {
	m, err := ParseManifest(manifestJSON(t, map[string]any{
		"config": configItems(map[string]any{
			"key": "llmBaseUrl", "label": "接口地址", "type": "text", "hostWhitelist": true,
		}),
	}))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	got, ok := m.ConfigItem("llmBaseUrl")
	if !ok || got.Key != "llmBaseUrl" || !got.HostWhitelist {
		t.Fatalf("ConfigItem(llmBaseUrl) = %+v, %v", got, ok)
	}
	if _, ok := m.ConfigItem("x"); ok {
		t.Fatal("an undeclared key must not be found")
	}
}

// TestPermissionLabelsCoverTheVocabulary keeps the two lists from drifting: a
// permission with no label would be shown as its raw name in the install dialog.
func TestPermissionLabelsCoverTheVocabulary(t *testing.T) {
	if len(PermissionLabels) != len(AllowedPermissions) {
		t.Fatalf("PermissionLabels has %d entries, AllowedPermissions has %d",
			len(PermissionLabels), len(AllowedPermissions))
	}
	for _, p := range AllowedPermissions {
		if PermissionLabels[p] == "" {
			t.Errorf("permission %s has no label", p)
		}
	}
	if PermissionLabels[PermPluginConfig] != "保存你自己的插件配置" {
		t.Errorf("PermPluginConfig label = %q", PermissionLabels[PermPluginConfig])
	}
}
