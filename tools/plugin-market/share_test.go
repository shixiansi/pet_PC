package main

// The market's half of the cross-plugin tray.
//
// This is not "a nice extra capability". The 插件 page mounts each plugin's OWN document,
// and four of the six bundled plugins publish or read a shared key from inside that
// document: the todo plugin publishes the list the pomodoro reads, the pomodoro publishes
// the phase-end fact the voice plugin reads. Until this program served the tray, mounting
// those documents put 「插件 … 调用了本程序不支持的方法 share.set」 in the middle of the
// plugin's own page - and the workaround was to refuse to mount any plugin that declared a
// surface, i.e. exactly the three the user wanted to see.
//
// So the rule moved to internal/plugins/share.go (one implementation, two hosts) and these
// tests pin the two things a second host could get wrong: it must honour the caller's
// `shareRead` declaration, and it must write into the right plugin's directory.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"PetPC/internal/plugins"
)

// installShareFixture installs a plugin that declares `plugin.share` plus whatever the
// caller adds to its manifest.
func installShareFixture(t *testing.T, m *Market, id string, extra map[string]any) string {
	t.Helper()
	man := testManifest(t, id, extra)
	man = strings.Replace(man, `"permissions":["pet.bubble","storage"]`,
		`"permissions":["pet.bubble","storage","plugin.share"]`, 1)
	data, sum := makeZip(t, map[string]string{
		"manifest.json": man,
		"panel.html":    "<!doctype html><p>hi</p>",
	})
	url := serveZip(t, data)
	entry := plugins.MarketEntry{
		ID:          id,
		Name:        "共享夹具",
		Version:     "1.0.0",
		Author:      "tester",
		Description: "用于测试",
		Permissions: []string{"pet.bubble", "storage", "plugin.share"},
		Download:    url,
		SHA256:      sum,
	}
	if _, err := m.manager.Install(entry); err != nil {
		t.Fatalf("install %s: %v", id, err)
	}
	if _, err := m.manager.GrantPermissions(id, []string{"plugin.share"}); err != nil {
		t.Fatalf("grant %s: %v", id, err)
	}
	// Installing is not enabling (that is the whole point of the two steps), and the gate
	// refuses a disabled plugin before it looks at anything else.
	if err := m.manager.SetEnabled(id, true); err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
	return id
}

func TestShareRoundTripsThroughTheMarket(t *testing.T) {
	m := newTestMarket(t, "")
	publisher := installShareFixture(t, m, "com.example.publisher", nil)
	// The reader DECLARED the publisher's keys. That declaration is the whole read gate -
	// and it is why the second key below is readable-but-unpublished, which is the normal
	// first-poll state every consumer hits.
	reader := installShareFixture(t, m, "com.example.reader", map[string]any{
		"shareRead": []string{"com.example.publisher/todo-list", "com.example.publisher/missing"},
	})

	out, err := m.PluginRequest(publisher, "share.set", `{"name":"todo-list","value":"[{\"id\":\"a\"}]"}`)
	if err != nil {
		t.Fatalf("share.set: %v", err)
	}
	if !strings.Contains(out, "com.example.publisher/todo-list") {
		t.Errorf("share.set did not name the key it wrote: %s", out)
	}

	// On DISK, in the publisher's own share/ folder: a value written anywhere else would
	// be a tray nobody can find.
	path := filepath.Join(m.dataDir, "plugin-data", publisher, "share", "todo-list")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the value is not in the publisher's share folder: %v", err)
	}

	got, err := m.PluginRequest(reader, "share.get", `{"owner":"com.example.publisher","name":"todo-list"}`)
	if err != nil {
		t.Fatalf("share.get: %v", err)
	}
	if !strings.Contains(got, `id\":\"a`) {
		t.Errorf("the reader did not receive the published value: %s", got)
	}

	// A value nobody published is an EMPTY value, not an error: every consumer hits that
	// on its first poll.
	empty, err := m.PluginRequest(reader, "share.get", `{"owner":"com.example.publisher","name":"missing"}`)
	if err != nil {
		t.Fatalf("share.get of an unpublished key: %v", err)
	}
	if !strings.Contains(empty, `"value":""`) {
		t.Errorf("an unpublished key is not reported as empty: %s", empty)
	}
}

func TestShareReadNeedsTheDeclaration(t *testing.T) {
	m := newTestMarket(t, "")
	publisher := installShareFixture(t, m, "com.example.publisher", nil)
	// NOT declared: this plugin must not be able to read the other one's key, and the
	// refusal has to name what is missing.
	snoop := installShareFixture(t, m, "com.example.snoop", nil)

	if _, err := m.PluginRequest(publisher, "share.set", `{"name":"secret-plan","value":"x"}`); err != nil {
		t.Fatalf("share.set: %v", err)
	}
	_, err := m.PluginRequest(snoop, "share.get", `{"owner":"com.example.publisher","name":"secret-plan"}`)
	if err == nil {
		t.Fatal("a plugin read another plugin's key without declaring it")
	}
	if !strings.Contains(err.Error(), "shareRead") || !strings.Contains(err.Error(), "com.example.publisher/secret-plan") {
		t.Errorf("the refusal does not name the missing declaration and the key: %v", err)
	}
}

func TestShareRemoveOnlyTouchesTheCallersOwnValues(t *testing.T) {
	m := newTestMarket(t, "")
	publisher := installShareFixture(t, m, "com.example.publisher", nil)
	other := installShareFixture(t, m, "com.example.other", map[string]any{
		"shareRead": []string{"com.example.publisher/todo-list"},
	})

	if _, err := m.PluginRequest(publisher, "share.set", `{"name":"todo-list","value":"v"}`); err != nil {
		t.Fatalf("share.set: %v", err)
	}
	// `share.remove` takes no owner: it always means "my own value", so a consumer cannot
	// clear the producer's job.
	if _, err := m.PluginRequest(other, "share.remove", `{"name":"todo-list"}`); err != nil {
		t.Fatalf("share.remove: %v", err)
	}
	got, err := m.PluginRequest(other, "share.get", `{"owner":"com.example.publisher","name":"todo-list"}`)
	if err != nil {
		t.Fatalf("share.get: %v", err)
	}
	if !strings.Contains(got, `"value":"v"`) {
		t.Errorf("another plugin removed the publisher's value: %s", got)
	}
}
