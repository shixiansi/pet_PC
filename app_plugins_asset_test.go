package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- T68/T69: the asset route ----------------------------------------------

// TestBridgeRoute serves the host's own bridge script.
//
// The three headers are not decoration: the iframe is a sandboxed OPAQUE origin
// (allow-scripts without allow-same-origin), so even a same-URL subresource fetch
// is cross-origin from its point of view, and without these it is refused before
// a single line of the plugin's script runs.
func TestBridgeRoute(t *testing.T) {
	a := pluginApp(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, pluginHostBridgePath, nil)
	assetHandler{app: a}.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/javascript") {
		t.Errorf("Content-Type = %q, want text/javascript", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if acao := rec.Header().Get("Access-Control-Allow-Origin"); acao != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", acao)
	}
	if body := rec.Body.String(); body != pluginBridgeJS {
		t.Errorf("the response is not the compiled-in bridge script: %d bytes vs %d", len(body), len(pluginBridgeJS))
	}
}

// TestPluginAssetRouteServesEnabledPlugins covers T69.
func TestPluginAssetRouteServesEnabledPlugins(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, pluginAssetPrefix+id+"/panel.html", nil)
	assetHandler{app: a}.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if acao := rec.Header().Get("Access-Control-Allow-Origin"); acao != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", acao)
	}
	dir, _ := a.pluginManager().PluginDir(id)
	onDisk, err := os.ReadFile(filepath.Join(dir, "panel.html"))
	if err != nil {
		t.Fatalf("read the plugin file: %v", err)
	}
	if rec.Body.String() != string(onDisk) {
		t.Error("the served bytes differ from the file on disk")
	}
}

// TestPluginAssetRouteRefusesDisabledAndMissing covers T71's first half: a
// disabled plugin's files stop being reachable at the same moment its surfaces
// close, so "off" is one state rather than two that can disagree.
func TestPluginAssetRouteRefusesDisabledAndMissing(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, nil)
	managerSetEnabled(t, a, id, false)

	for _, target := range []string{id, "com.example.never-installed"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, pluginAssetPrefix+target+"/panel.html", nil)
		assetHandler{app: a}.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "<!doctype") {
			t.Errorf("%s: the response leaked the plugin's file", target)
		}
	}
}

// TestPluginAssetRouteRefusesTraversal covers T71's second half.
//
// Both the percent-encoded and the literal form are tried: an encoded separator
// arrives as a single path segment, and a check that only looked at the raw
// string would miss it.
func TestPluginAssetRouteRefusesTraversal(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, nil)

	// A file outside the plugin's directory, to prove nothing leaks it.
	if err := os.WriteFile(filepath.Join(a.pluginManager().DataDir(), "config.json"), []byte("SECRET"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	for _, path := range []string{
		pluginAssetPrefix + id + "/../../config.json",
		pluginAssetPrefix + id + "/..%2fconfig.json",
		pluginAssetPrefix + id + "/../../../../../../config.json",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		assetHandler{app: a}.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Errorf("%s: status = 200, must be refused", path)
		}
		if strings.Contains(rec.Body.String(), "SECRET") {
			t.Errorf("%s: the response leaked a file outside the plugin", path)
		}
	}
}

// TestPluginAssetRouteRejectsBadIDs keeps a malformed id from reaching a
// filepath.Join at all.
func TestPluginAssetRouteRejectsBadIDs(t *testing.T) {
	a := pluginApp(t)
	for _, path := range []string{
		pluginAssetPrefix + "Not.An.Id/panel.html",
		pluginAssetPrefix + "nodot/panel.html",
		pluginAssetPrefix + "com.example.demo",
		pluginAssetPrefix + "com.example.demo/",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		assetHandler{app: a}.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Errorf("%s: status = 200, must be refused", path)
		}
	}
}

// --- T70: the existing pet image route is untouched -------------------------

// TestPetImageRouteUnchanged covers T70. The plugin route was added beside this
// one, and the whole check is that it did not disturb it.
func TestPetImageRouteUnchanged(t *testing.T) {
	a := pluginApp(t)

	rec := httptest.NewRecorder()
	assetHandler{app: a}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pet-image", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("with no custom picture the route must 404, got %d", rec.Code)
	}

	// A custom picture: any bytes with a recognised extension will do, because
	// what is under test is the route rather than the image.
	src := filepath.Join(t.TempDir(), "pet.png")
	if err := os.WriteFile(src, []byte("PNGDATA"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := a.store.SetPetImage(src); err != nil {
		t.Fatalf("SetPetImage: %v", err)
	}
	rec = httptest.NewRecorder()
	assetHandler{app: a}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pet-image", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("with a custom picture the route must 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Errorf("Content-Type = %q, want an image/png type", ct)
	}
}
