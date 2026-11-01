package plugins

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"PetPC/internal/themepacks"
)

// The asset route with a theme pack: a document gets the `<style>`, a stylesheet does not, and an
// unthemed request comes back byte for byte.
//
// WHY THIS IS A TEST AND NOT A COMMENT IN serveDocument: the injection is the ONLY mechanism by which
// a pack reaches a plugin interface (the frame is an opaque origin). If this route stops splicing, the
// symptom is "the plugin panel is still violet on a pink desktop" - and nothing else in the repo looks
// wrong: the windows are pink, the gate is green, the plugin "works".

type fakeAssetGate struct {
	dir     string
	enabled bool
}

func (g fakeAssetGate) Get(id string) (Installed, bool) {
	return Installed{ID: id, Dir: g.dir, Enabled: g.enabled}, true
}

func (g fakeAssetGate) PrivateDir(string) (string, error) { return g.dir, nil }

// writePack drops a valid user pack into <dataDir>/themes/<id>/theme.json.
func writePack(t *testing.T, dataDir, id string, light, dark map[string]string) {
	t.Helper()
	dir := filepath.Join(dataDir, "themes", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"id":"` + id + `","name":"测试包","tokens":{` +
		`"light":` + jsonMap(light) + `,"dark":` + jsonMap(dark) + `}}`
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Stable order so a failure message is readable.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	var b strings.Builder
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"` + k + `":"` + m[k] + `"`)
	}
	b.WriteString("}")
	return b.String()
}

func minimalPack(bg, accent string) map[string]string {
	return map[string]string{
		"bg": bg, "bg-raised": "#ffffff", "bg-sunken": "#eeeeee", "bg-active": "#dddddd",
		"text": "#111111", "text-dim": "#444444", "accent": accent, "accent-strong": accent,
		"accent-soft": "#eeeeee",
	}
}

func TestAssetRouteInjectsThePackAndLeavesOtherFilesAlone(t *testing.T) {
	dir := t.TempDir()
	dataDir := t.TempDir()
	packs := themepacks.NewSet(dataDir)
	const id = "com.example.plugin-packer"

	entry := `<!doctype html><html><head><link rel="stylesheet" href="ui.css"></head><body>hi</body></html>`
	if err := os.WriteFile(filepath.Join(dir, "panel.html"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ui.css"), []byte(`:root{--accent:#7c3aed}`), 0o644); err != nil {
		t.Fatal(err)
	}
	gate := fakeAssetGate{dir: dir, enabled: true}

	// Built-in pack: injected, and the accent is that pack's.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, AssetPrefix+id+"/panel.html?theme=dark&pack=forest", nil)
	if !ServePluginAsset(gate, packs, rec, req) {
		t.Fatal("asset route 没接过这个请求")
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<style id="petpc-theme-pack">:root:root{`) {
		t.Fatalf("entry 文档里没有注入：%s", body)
	}
	if !strings.Contains(body, "--accent:#34d399;") {
		t.Fatalf("注入的不是森林的深色：%s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type 是 %q，应该是 text/html", ct)
	}

	// An INSTALLED pack works the same way - that is the whole point of the loader.
	writePack(t, dataDir, "mint", minimalPack("#f2fdfb", "#0f766e"), minimalPack("#0b1a18", "#5eead4"))
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, AssetPrefix+id+"/panel.html?theme=light&pack=mint", nil)
	ServePluginAsset(gate, packs, rec, req)
	if !strings.Contains(rec.Body.String(), "--accent:#0f766e;") {
		t.Fatalf("装进来的包没有生效：%s", rec.Body.String())
	}

	// A stylesheet is bytes: a pack inside ui.css would be a CSS file that violates its own contract.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, AssetPrefix+id+"/ui.css?theme=dark&pack=forest", nil)
	ServePluginAsset(gate, packs, rec, req)
	if got := rec.Body.String(); got != `:root{--accent:#7c3aed}` {
		t.Fatalf(".css 被改动了：%s", got)
	}

	// No pack: the document comes back byte for byte.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, AssetPrefix+id+"/panel.html?theme=light", nil)
	ServePluginAsset(gate, packs, rec, req)
	if got := rec.Body.String(); got != entry {
		t.Fatalf("没有包时文档被改动了：%s", got)
	}

	// A disabled plugin's document is not served at all, injected or otherwise.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, AssetPrefix+id+"/panel.html?theme=light&pack=sakura", nil)
	ServePluginAsset(fakeAssetGate{dir: dir, enabled: false}, packs, rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("停用的插件应该 403，得到 %d", rec.Code)
	}
}

func TestAssetRouteWithoutAPackSourceStillServesBuiltIns(t *testing.T) {
	dir := t.TempDir()
	const id = "com.example.plugin-plain"
	entry := `<html><head></head><body>x</body></html>`
	if err := os.WriteFile(filepath.Join(dir, "panel.html"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, AssetPrefix+id+"/panel.html?theme=light&pack=sakura", nil)
	ServePluginAsset(fakeAssetGate{dir: dir, enabled: true}, nil, rec, req)
	if !strings.Contains(rec.Body.String(), "--accent:#be1857;") {
		t.Fatalf("nil PackSource 应该退到内建表：%s", rec.Body.String())
	}
}
