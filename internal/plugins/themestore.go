package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"PetPC/internal/themepacks"
)

/*
 * 主题商店：和插件市场同一条分发通道（同一个 base URL、同一份 http 客户端、同样校验 sha256），
 * 但装的东西不一样——主题包是一个 theme.json 文件，不是 zip。
 *
 * WHY A FILE AND NOT A ZIP: a theme pack is a palette. Zipping one adds a container, a manifest
 * inside a manifest, and an unzip step that has to defend against paths - for a file whose whole
 * content is "these colours". The marketplace catalogue still carries `sha256`, so the bytes are
 * pinned exactly as a package would be.
 *
 * The install path deliberately does NOT reuse the plugin installer: that one writes into
 * plugins/<id>/ with a manifest, a version table and a grant list. A theme has none of those, and
 * folding it in would make "installed" mean two different things in one registry.
 */

// ThemeStoreEntry is one row of themes/index.json on the market.
type ThemeStoreEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	Download    string `json:"download"`
	SHA256      string `json:"sha256"`
}

// ThemeStoreView is what the settings page needs in one shape.
//
// A STRUCT RATHER THAN MULTIPLE RETURN VALUES: Wails v2 maps at most (result, error) onto a JS
// promise, so a second string would be silently dropped - the page would get the rows and never the
// sentence explaining an empty shelf.
type ThemeStoreView struct {
	Entries []ThemeStoreEntry `json:"entries"`
	Note    string            `json:"note"`
}

type themeCatalogue struct {
	Version int               `json:"version"`
	Themes  []ThemeStoreEntry `json:"themes"`
}

// ThemesCatalogue fetches `<base>/themes/index.json`.
//
// A missing file is not an error to shout about: the theme shelf is new, and a market that predates
// it answers 404. That case comes back as an empty list plus a sentence the page can show.
func (m *Manager) ThemesCatalogue() (ThemeStoreView, error) {
	base := m.baseURL()
	if base == "" {
		return ThemeStoreView{}, fmt.Errorf("还没有设置市场源")
	}
	if err := checkBaseScheme(base); err != nil {
		return ThemeStoreView{}, err
	}
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: catalogueTimeout}
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/themes/index.json", nil)
	if err != nil {
		return ThemeStoreView{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return ThemeStoreView{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ThemeStoreView{Note: "市场源上没有 themes/index.json（商店还没开张）"}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ThemeStoreView{}, fmt.Errorf("市场返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ThemeStoreView{}, err
	}
	var cat themeCatalogue
	if err := json.Unmarshal(body, &cat); err != nil {
		return ThemeStoreView{}, fmt.Errorf("themes/index.json 不是合法 JSON：%v", err)
	}
	out := make([]ThemeStoreEntry, 0, len(cat.Themes))
	for _, e := range cat.Themes {
		cleaned, err := validateThemeStoreEntry(e)
		if err != nil {
			// 一条坏条目不该让整张货架消失：跳过它，其余照常。
			continue
		}
		out = append(out, cleaned)
	}
	return ThemeStoreView{Entries: out}, nil
}

// validateThemeStoreEntry rejects rows that cannot be installed safely.
func validateThemeStoreEntry(e ThemeStoreEntry) (ThemeStoreEntry, error) {
	if !themepacks.ValidID(e.ID) {
		return ThemeStoreEntry{}, fmt.Errorf("主题包 id 不合法：%q", e.ID)
	}
	if _, clash := themepacks.Builtin[e.ID]; clash {
		return ThemeStoreEntry{}, fmt.Errorf("%s 是内建包，商店不该再卖一次", e.ID)
	}
	if err := checkDownloadScheme(e.Download); err != nil {
		return ThemeStoreEntry{}, err
	}
	if !sha256Pattern.MatchString(strings.ToLower(e.SHA256)) {
		return ThemeStoreEntry{}, fmt.Errorf("%s 缺少合法的 sha256", e.ID)
	}
	e.SHA256 = strings.ToLower(e.SHA256)
	if strings.TrimSpace(e.Name) == "" {
		e.Name = e.ID
	}
	return e, nil
}

// InstallTheme downloads one pack and writes it to themes/<id>/theme.json.
//
// The order is the contract: download → verify sha256 → validate the manifest (names, values,
// both schemes, the skeleton tokens) → write. Nothing is written before the last step, so a
// half-verified file can never sit in the themes directory looking installed.
func (m *Manager) InstallTheme(id string) error {
	if !themepacks.ValidID(id) {
		return fmt.Errorf("主题包 id 不合法：%q", id)
	}
	view, err := m.ThemesCatalogue()
	if err != nil {
		return err
	}
	var chosen *ThemeStoreEntry
	for i := range view.Entries {
		if view.Entries[i].ID == id {
			chosen = &view.Entries[i]
			break
		}
	}
	if chosen == nil {
		return fmt.Errorf("商店里没有 %s", id)
	}
	body, err := m.downloadTheme(chosen.Download)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != chosen.SHA256 {
		return fmt.Errorf("%s 的 sha256 对不上（下载到的内容与目录里登记的不是同一份）", id)
	}
	// 用同一个校验器把内容读一遍：sha256 只证明"是目录里那一份"，不证明"那一份是合法的包"。
	if _, _, err := themepacks.ParseManifest(body, id); err != nil {
		return err
	}
	dir := filepath.Join(m.dataDir, "themes", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "theme.json.tmp")
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "theme.json"))
}

// RemoveTheme deletes an installed pack's directory. Built-in ids are refused (there is nothing to
// delete, and "removing" one would only confuse the picker).
func (m *Manager) RemoveTheme(id string) error {
	if !themepacks.ValidID(id) {
		return fmt.Errorf("主题包 id 不合法：%q", id)
	}
	if _, clash := themepacks.Builtin[id]; clash {
		return fmt.Errorf("%s 是内建包，删不掉", id)
	}
	dir := filepath.Join(m.dataDir, "themes", id)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return nil
}

// downloadTheme resolves a download path and returns its bytes.
//
// The resolved URL must still be http(s) even when the catalogue gave a relative path: the check in
// validateThemeStoreEntry ran on the RELATIVE string, and a base URL is what turns it absolute.
func (m *Manager) downloadTheme(download string) ([]byte, error) {
	target := download
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = strings.TrimRight(m.baseURL(), "/") + "/" + strings.TrimLeft(target, "/")
	}
	if err := checkBaseScheme(target); err != nil {
		return nil, err
	}
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: catalogueTimeout}
	}
	resp, err := client.Get(target)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载主题包失败：HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<10))
}