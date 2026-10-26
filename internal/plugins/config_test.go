package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKeeper is an in-memory SecretKeeper.
//
// It records every call so the tests can assert on the PATH a secret was written
// to and on how many times crypto was asked to do anything - the two facts that
// are invisible from the returned values.
type fakeKeeper struct {
	values map[string]string
	gets   int
	sets   int
	clears int
	// failSet makes the next Set calls fail, for the "nothing was written"
	// assertions.
	failSet bool
	// failGet makes Get report an undecryptable value.
	failGet bool
}

func newFakeKeeper() *fakeKeeper { return &fakeKeeper{values: map[string]string{}} }

func (f *fakeKeeper) Get(path string) (string, error) {
	f.gets++
	if f.failGet {
		return "", fmt.Errorf("cannot decrypt")
	}
	return f.values[path], nil
}

func (f *fakeKeeper) Set(path, value string) error {
	f.sets++
	if f.failSet {
		return fmt.Errorf("cannot encrypt")
	}
	f.values[path] = value
	return nil
}

func (f *fakeKeeper) Clear(path string) error {
	f.clears++
	delete(f.values, path)
	return nil
}

// configManifest is a manifest with one of each control type plus a secret and a
// host-whitelist item.
func configManifest() Manifest {
	return Manifest{
		ID:      "com.example.plugin",
		Config: []ConfigItem{
			{Key: "llmBaseUrl", Label: "接口地址", Type: "text", HostWhitelist: true},
			{Key: "llmMaxTokens", Label: "回复上限", Type: "number"},
			{Key: "llmContextTodos", Label: "知道待办", Type: "bool"},
			{Key: "llmApiKey", Label: "API Key", Type: "secret", Secret: true},
		},
	}
}

// TestConfigValuesRoundTrip covers T36.
func TestConfigValuesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewConfigStore(dir, configManifest(), newFakeKeeper())
	if err := store.Write(map[string]string{
		"llmBaseUrl":   "https://x/v1",
		"llmMaxTokens": "123",
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	reopened := NewConfigStore(dir, configManifest(), newFakeKeeper())
	got, err := reopened.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Values["llmBaseUrl"] != "https://x/v1" || got.Values["llmMaxTokens"] != "123" {
		t.Fatalf("values not preserved: %+v", got.Values)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("config.json must exist: %v", err)
	}
}

// TestConfigRejectsUndeclaredKey covers T37.
func TestConfigRejectsUndeclaredKey(t *testing.T) {
	dir := t.TempDir()
	store := NewConfigStore(dir, configManifest(), newFakeKeeper())
	if err := store.Write(map[string]string{"llmBaseUrl": "A"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := store.Write(map[string]string{"llmBaseUrl": "B", "x": "1"}); err == nil {
		t.Fatal("an undeclared key must be refused")
	}
	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Values["llmBaseUrl"] != "A" {
		t.Fatalf("a refused write must change nothing, got %q", got.Values["llmBaseUrl"])
	}
	if _, ok := got.Values["x"]; ok {
		t.Fatal("the undeclared key must not be stored")
	}
}

// TestConfigSecretNeverHitsThePlainFile covers T38.
func TestConfigSecretNeverHitsThePlainFile(t *testing.T) {
	dir := t.TempDir()
	keeper := newFakeKeeper()
	store := NewConfigStore(dir, configManifest(), keeper)
	if err := store.Write(map[string]string{"llmApiKey": "sk-abcdefgh"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if strings.Contains(string(data), "sk-") {
		t.Fatalf("config.json must not contain the secret: %s", data)
	}
	if keeper.sets != 1 {
		t.Fatalf("keeper.Set called %d times, want 1", keeper.sets)
	}
	wantPath := filepath.Join(dir, "secret-llmApiKey.bin")
	if _, ok := keeper.values[wantPath]; !ok {
		t.Fatalf("secret not written to %s: %v", wantPath, keeper.values)
	}
}

// TestConfigSecretRefusedWithoutKeeper covers T39: no keeper means no secret,
// never a plaintext fallback.
func TestConfigSecretRefusedWithoutKeeper(t *testing.T) {
	dir := t.TempDir()
	store := NewConfigStore(dir, configManifest(), nil)
	if err := store.Write(map[string]string{"llmApiKey": "sk-abcdefgh"}); err == nil {
		t.Fatal("a secret write without a keeper must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
		t.Fatal("a refused secret write must not create config.json")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(string(data), "sk-") {
			t.Fatalf("%s contains the plaintext secret", e.Name())
		}
	}
}

// TestConfigUndecryptableSecretIsUnset covers T40.
func TestConfigUndecryptableSecretIsUnset(t *testing.T) {
	dir := t.TempDir()
	keeper := newFakeKeeper()
	store := NewConfigStore(dir, configManifest(), keeper)
	if err := store.Write(map[string]string{"llmApiKey": "sk-abcdefghijklmnop"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	keeper.failGet = true
	got, err := store.Read()
	if err != nil {
		t.Fatalf("a secret that cannot be decrypted must not be an error: %v", err)
	}
	if got.Secrets["llmApiKey"].Set {
		t.Fatal("an undecryptable secret must read as unset")
	}
}

// TestConfigReadNeverReturnsPlaintext covers T41.
func TestConfigReadNeverReturnsPlaintext(t *testing.T) {
	dir := t.TempDir()
	keeper := newFakeKeeper()
	store := NewConfigStore(dir, configManifest(), keeper)
	const secret = "sk-abcdefghijklmnop"
	if err := store.Write(map[string]string{"llmApiKey": secret}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, ok := got.Values["llmApiKey"]; ok {
		t.Fatal("Read must not return a secret in Values")
	}
	state := got.Secrets["llmApiKey"]
	if !state.Set {
		t.Fatal("the secret must read as set")
	}
	if state.Mask == secret || !strings.Contains(state.Mask, "…") {
		t.Fatalf("mask %q must be a mask, not the value", state.Mask)
	}
	if strings.Contains(fmt.Sprintf("%#v", got), secret) {
		t.Fatal("the plaintext secret must not appear anywhere in the returned struct")
	}
}

// TestConfigEmptySecretClearsIt covers T42.
func TestConfigEmptySecretClearsIt(t *testing.T) {
	dir := t.TempDir()
	keeper := newFakeKeeper()
	store := NewConfigStore(dir, configManifest(), keeper)
	if err := store.Write(map[string]string{"llmApiKey": "sk-abcdefghijklmnop"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if state := mustRead(t, store).Secrets["llmApiKey"]; !state.Set {
		t.Fatal("the secret must be set after a non-empty write")
	}
	if err := store.Write(map[string]string{"llmApiKey": ""}); err != nil {
		t.Fatalf("clearing must succeed: %v", err)
	}
	if state := mustRead(t, store).Secrets["llmApiKey"]; state.Set {
		t.Fatal("an empty write must clear the secret")
	}
	if _, ok := keeper.values[filepath.Join(dir, "secret-llmApiKey.bin")]; ok {
		t.Fatal("the secret file must be gone")
	}
	if _, err := os.ReadFile(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("config.json must still be readable: %v", err)
	}
}

func mustRead(t *testing.T, store *ConfigStore) ConfigValues {
	t.Helper()
	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return got
}

// TestConfigBoolNormalisation pins the one canonical form the store enforces.
func TestConfigBoolNormalisation(t *testing.T) {
	dir := t.TempDir()
	store := NewConfigStore(dir, configManifest(), newFakeKeeper())
	for in, want := range map[string]string{"true": "true", "1": "true", "on": "true", "no": "false", "": "false"} {
		if err := store.Write(map[string]string{"llmContextTodos": in}); err != nil {
			t.Fatalf("Write(%q): %v", in, err)
		}
		if got := mustRead(t, store).Values["llmContextTodos"]; got != want {
			t.Errorf("bool %q stored as %q, want %q", in, got, want)
		}
	}
}

// TestUninstallRemovesConfigAndSecrets covers T43.
func TestUninstallRemovesConfigAndSecrets(t *testing.T) {
	dir := t.TempDir()
	man := configManifest()
	man.Name = "示例插件"
	man.Version = "1.0.0"
	man.Author = "tester"
	man.Description = "用于测试的插件"
	man.Panel = true
	man.Entry = "panel.html"

	pluginDir := filepath.Join(dir, "plugins", man.ID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(man)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, ManifestName), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	cachedIndex(t, dir, Index{Version: 1, Plugins: []IndexEntry{{
		ID: man.ID, Version: man.Version, Enabled: true, Granted: []string{PermPluginConfig},
	}}})

	manager, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	keeper := newFakeKeeper()
	private, err := manager.PrivateDir(man.ID)
	if err != nil {
		t.Fatalf("PrivateDir: %v", err)
	}
	store := NewConfigStore(private, man, keeper)
	if err := store.Write(map[string]string{"llmBaseUrl": "x", "llmApiKey": "sk-abcdefghijklmnop"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(private, "config.json")); err != nil {
		t.Fatalf("config.json must exist before uninstall: %v", err)
	}
	if err := manager.Uninstall(man.ID); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatalf("the private directory must be gone: %v", err)
	}
}

// TestConfigHostsParsing covers T44: the accepting and the three refusing shapes.
func TestConfigHostsParsing(t *testing.T) {
	man := configManifest()
	got := ConfigHosts(man, map[string]string{"llmBaseUrl": "https://a.example.com/v1, b.example.com:8443"})
	want := []string{"a.example.com", "b.example.com:8443"}
	if len(got) != len(want) {
		t.Fatalf("ConfigHosts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ConfigHosts = %v, want %v", got, want)
		}
	}

	for _, bad := range []string{"", "ftp://a/b", "*.example.com", "  ,  "} {
		if out := ConfigHosts(man, map[string]string{"llmBaseUrl": bad}); len(out) != 0 {
			t.Errorf("ConfigHosts(%q) = %v, want nothing", bad, out)
		}
	}

	// A bare host with no scheme is accepted too - that is the commonest thing
	// a user types when the field is not a URL.
	if out := ConfigHosts(man, map[string]string{"llmBaseUrl": "plain.example.com"}); len(out) != 1 || out[0] != "plain.example.com" {
		t.Errorf("a bare host must be accepted, got %v", out)
	}

	// ws/wss contribute their host, because the SAME setting is what points a
	// net.ws plugin at its service. Refusing them here would leave the socket
	// capability unable to reach the very address the form asks for.
	if out := ConfigHosts(man, map[string]string{"llmBaseUrl": "ws://127.0.0.1:3000/event"}); len(out) != 1 || out[0] != "127.0.0.1:3000" {
		t.Errorf("a ws:// value must contribute its host, got %v", out)
	}
	if out := ConfigHosts(man, map[string]string{"llmBaseUrl": "wss://bot.example.com/event"}); len(out) != 1 || out[0] != "bot.example.com" {
		t.Errorf("a wss:// value must contribute its host, got %v", out)
	}
}

// TestAtomicWriteLeavesNoTempFiles pins the atomic-write helper's cleanliness.
func TestAtomicWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	if err := atomicWriteFile(target, []byte("{}")); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("a temp file was left behind: %s", e.Name())
		}
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "{}" {
		t.Fatalf("target not written: %q %v", data, err)
	}
}
