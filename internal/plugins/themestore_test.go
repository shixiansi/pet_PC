package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 主题商店：装上去的必须是"目录里登记的那一份"，而且必须是合法的包。
//
// The two checks are different questions and both are load-bearing: sha256 answers "are these the
// bytes the catalogue promised", and the manifest validation answers "is a pack at all". A store
// that checked only the hash would happily install a file whose `tokens.light` is missing - and the
// user would get a pack that silently does nothing.

const goodThemeBody = `{"id":"mint","name":"薄荷气泡","author":"Yue","version":"0.1.0","tokens":{` +
	`"light":{"bg":"#f2fdfb","bg-raised":"#ffffff","bg-sunken":"#e6fffb","bg-active":"#d5f7f2",` +
	`"text":"#0c201d","text-dim":"#4b6b67","accent":"#0f766e","accent-strong":"#0f766e","accent-soft":"#e6fffb"},` +
	`"dark":{"bg":"#0b1a18","bg-raised":"#112925","bg-sunken":"#081513","bg-active":"#123a34",` +
	`"text":"#d7f5ef","text-dim":"#8fc4bb","accent":"#5eead4","accent-strong":"#0f766e","accent-soft":"#112925"}}}`

func themeServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	mux := http.NewServeMux()
	mux.HandleFunc("/themes/index.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": 1,
			"themes": []map[string]string{{
				"id": "mint", "name": "薄荷气泡", "version": "0.1.0", "author": "Yue",
				"download": "themes/mint.json",
				"sha256":   hex.EncodeToString(sum[:]),
			}},
		})
	})
	mux.HandleFunc("/themes/mint.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newThemeManager(t *testing.T, base string) *Manager {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.BaseURL = func() string { return base }
	m.Client = http.DefaultClient
	return m
}

func TestThemeCatalogueAndInstall(t *testing.T) {
	body := strings.ReplaceAll(goodThemeBody, `"id":"mint"`, `"id":"mint"`)
	srv := themeServer(t, body)
	m := newThemeManager(t, srv.URL)

	entries, err := m.ThemesCatalogue()
	if err != nil {
		t.Fatalf("拉目录失败：%v", err)
	}
	if entries.Note != "" {
		t.Fatalf("不该有 note：%s", entries.Note)
	}
	if len(entries.Entries) != 1 || entries.Entries[0].ID != "mint" {
		t.Fatalf("目录不对：%+v", entries.Entries)
	}

	if err := m.InstallTheme("mint"); err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	written, err := os.ReadFile(filepath.Join(m.dataDir, "themes", "mint", "theme.json"))
	if err != nil {
		t.Fatalf("没有写到 themes/mint/theme.json：%v", err)
	}
	if string(written) != body {
		t.Fatal("写下去的不是下载到的字节")
	}

	// 卸载
	if err := m.RemoveTheme("mint"); err != nil {
		t.Fatalf("卸载失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(m.dataDir, "themes", "mint")); !os.IsNotExist(err) {
		t.Fatal("目录还在")
	}
}

func TestThemeInstallRejectsABadHash(t *testing.T) {
	// 目录登记的是好内容，实际给的却是另一份：sha256 必须拦住它。
	good := goodThemeBody
	bad := strings.Replace(good, `"#0f766e"`, `"#123456"`, 1)
	sum := sha256.Sum256([]byte(good))
	mux := http.NewServeMux()
	mux.HandleFunc("/themes/index.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": 1, "themes": []map[string]string{{
			"id": "mint", "download": "themes/mint.json", "sha256": hex.EncodeToString(sum[:]),
		}}})
	})
	mux.HandleFunc("/themes/mint.json", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(bad)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	m := newThemeManager(t, srv.URL)
	if err := m.InstallTheme("mint"); err == nil {
		t.Fatal("sha256 对不上却装成功了")
	}
	if _, err := os.Stat(filepath.Join(m.dataDir, "themes", "mint")); !os.IsNotExist(err) {
		t.Fatal("校验失败不该留下任何目录")
	}
}

func TestThemeInstallRejectsAnInvalidManifest(t *testing.T) {
	// 哈希是对的，但内容不是一个合法的包（缺 dark 那一套）。
	body := `{"id":"half","tokens":{"light":{"bg":"#ffffff"}}}`
	sum := sha256.Sum256([]byte(body))
	mux := http.NewServeMux()
	mux.HandleFunc("/themes/index.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": 1, "themes": []map[string]string{{
			"id": "half", "download": "themes/half.json", "sha256": hex.EncodeToString(sum[:]),
		}}})
	})
	mux.HandleFunc("/themes/half.json", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	m := newThemeManager(t, srv.URL)
	err := m.InstallTheme("half")
	// 先撞上的是"缺骨架令牌"，而不是"缺 dark"——顺序不重要，重要的是它被拒并且说了原因。
	if err == nil || !strings.Contains(err.Error(), "骨架令牌") {
		t.Fatalf("应该因为 manifest 不合法被拒并说明原因，得到 %v", err)
	}
}

func TestThemeStoreRefusesBuiltinsAndMissingShelves(t *testing.T) {
	m := newThemeManager(t, "https://example.invalid")
	if err := m.RemoveTheme("sakura"); err == nil {
		t.Fatal("内建包不该能删")
	}
	if err := m.InstallTheme("sakura"); err == nil {
		t.Fatal("内建包不该能从商店装（它会覆盖不了任何东西）")
	}

	// 一个还没有 themes/ 的市场：空列表 + 一句能显示的话，而不是一个错误。
	mux := http.NewServeMux()
	mux.HandleFunc("/themes/index.json", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	view, err := newThemeManager(t, srv.URL).ThemesCatalogue()
	if err != nil {
		t.Fatalf("404 不该是错误：%v", err)
	}
	if len(view.Entries) != 0 || view.Note == "" {
		t.Fatalf("应该是空列表 + 说明，得到 %v / %q", view.Entries, view.Note)
	}
}
