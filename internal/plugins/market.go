package plugins

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"PetPC/internal/plugins/sample"
)

// DefaultMarketBaseURL is the market base address used when the user has not
// chosen one.
//
// It points at raw.githubusercontent.com THROUGH gh-proxy.com, which is the
// mirror form this project already uses elsewhere: raw.githubusercontent.com is
// not reachable from mainland China, and the market is useless if its catalogue
// cannot be fetched. The address is a configuration value rather than a
// constant in the code path, so a different mirror or a different repository
// name needs one edit here and nothing else.
const DefaultMarketBaseURL = "https://gh-proxy.com/https://raw.githubusercontent.com/shixiansi/PetPC-plugin/main"

// embeddedPrefix marks a download that is served from the built-in sample
// source instead of the network. It is a scheme rather than a bare file name so
// a built-in entry can never be confused with a relative path that would be
// resolved against the market base.
const embeddedPrefix = "embedded://"

// marketNoticeUnconfigured and marketNoticeUnreachable are two DIFFERENT
// messages on purpose: "you never set an address" and "the address you set did
// not answer" are different problems with different fixes, and the fallback
// looks identical otherwise.
const (
	marketNoticeUnconfigured = "未配置市场地址，正在使用内置示例源"
	marketNoticeUnreachable  = "市场不可达，正在使用内置示例源"
)

// catalogueTimeout bounds the catalogue request. It is short because the
// catalogue is a small JSON file and the UI is waiting on it.
const catalogueTimeout = 8 * time.Second

// MarketEntry is one row of the market catalogue.
type MarketEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	NetHosts    []string `json:"netHosts,omitempty"`
	// Download is a path relative to the market base, or an absolute http(s)
	// URL. A relative path is what lets the whole catalogue follow a mirror
	// change with one edit to the base.
	Download string `json:"download"`
	SHA256   string `json:"sha256"`
	// Installed / InstalledVersion are filled in by the host from the index,
	// never by the catalogue: what is installed is a local fact.
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installedVersion,omitempty"`
}

// MarketView is the "插件" tab's market half.
//
// Fallback and Notice travel with the entries rather than arriving as a
// separate error, because a fallback is not a failure: the built-in source is a
// working catalogue, and the tab renders it while telling the user why.
type MarketView struct {
	// Base is the address the tab's input box should show.
	Base string `json:"base"`
	// Fallback is true when the entries came from the built-in source.
	Fallback bool `json:"fallback"`
	// Notice explains a fallback, or accumulates the entries that had to be
	// dropped. Empty when everything came from the configured address intact.
	Notice  string        `json:"notice"`
	Entries []MarketEntry `json:"entries"`
}

// catalogue is the on-the-wire shape of index.json.
type catalogue struct {
	Version   int             `json:"version"`
	UpdatedAt string          `json:"updatedAt"`
	Plugins   []catalogueItem `json:"plugins"`
}

// catalogueItem mirrors MarketEntry but keeps every field optional, so a row
// missing a required key can be reported by id rather than failing the whole
// decode.
type catalogueItem struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	NetHosts    []string `json:"netHosts,omitempty"`
	Download    string   `json:"download"`
	SHA256      string   `json:"sha256"`
}

// Market fetches the catalogue, falling back to the built-in sample source.
//
// The fallback is not an error path: an unreachable mirror or a blank address
// still yields a usable list, flagged with Fallback and explained in Notice.
// Only a base address with a scheme that cannot be fetched at all is an error,
// because that is a value the user typed and has to fix.
func (m *Manager) Market() (MarketView, error) {
	base := m.baseURL()
	if base != "" {
		if err := checkBaseScheme(base); err != nil {
			return MarketView{}, err
		}
	}

	view := MarketView{Base: base}
	var raw catalogue
	notice := ""

	if base == "" {
		raw = builtinCatalogue()
		view.Fallback = true
		notice = marketNoticeUnconfigured
	} else {
		got, err := m.fetchCatalogue(base)
		if err != nil {
			raw = builtinCatalogue()
			view.Fallback = true
			notice = marketNoticeUnreachable
		} else {
			raw = got
		}
	}

	entries, dropped := m.validateEntries(raw.Plugins, view.Fallback)
	view.Entries = entries
	view.Notice = joinNotices(notice, dropped)
	return view, nil
}

// baseURL reads the current market address.
func (m *Manager) baseURL() string {
	if m.BaseURL == nil {
		return ""
	}
	return strings.TrimSpace(m.BaseURL())
}

// checkBaseScheme rejects anything that is not http or https.
//
// A file:// or ftp:// base is refused rather than treated as unreachable,
// because it is a typo the user can fix, and silently swapping in the built-in
// source would hide it behind a list that looks fine.
func checkBaseScheme(base string) error {
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("市场地址只支持 http/https：%s", base)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("市场地址只支持 http/https：%s", base)
	}
	if u.Host == "" {
		return fmt.Errorf("市场地址只支持 http/https：%s", base)
	}
	return nil
}

// fetchCatalogue GETs base + "/index.json".
func (m *Manager) fetchCatalogue(base string) (catalogue, error) {
	var out catalogue
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: catalogueTimeout}
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/index.json", nil)
	if err != nil {
		return out, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("市场返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

// builtinCatalogue reads the catalogue embedded in the binary.
//
// It is the offline floor: it always parses, always yields at least the sample
// plugin, and is the source the two fallback paths above land on.
func builtinCatalogue() catalogue {
	var out catalogue
	data, err := sample.FS.ReadFile("index.json")
	if err != nil {
		return catalogue{Version: 1}
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return catalogue{Version: 1}
	}
	return out
}

// validateEntries checks each catalogue row and drops the bad ones.
//
// One malformed row must not cost the user the rest of the catalogue, so rows
// are filtered individually and every drop is named in the notice: a plugin
// that quietly vanished from the list would look like it was never published.
func (m *Manager) validateEntries(items []catalogueItem, builtin bool) ([]MarketEntry, string) {
	installed := make(map[string]string)
	if idx, err := m.loadIndex(); err == nil {
		for _, e := range idx.Plugins {
			installed[e.ID] = e.Version
		}
	}

	out := make([]MarketEntry, 0, len(items))
	var dropped []string
	for _, it := range items {
		entry, err := validateEntry(it, builtin)
		if err != nil {
			dropped = append(dropped, fmt.Sprintf("%s（%v）", fallbackID(it.ID), err))
			continue
		}
		if v, ok := installed[entry.ID]; ok {
			entry.Installed = true
			entry.InstalledVersion = v
		}
		out = append(out, entry)
	}
	if len(dropped) == 0 {
		return out, ""
	}
	return out, "已忽略 " + fmt.Sprint(len(dropped)) + " 条非法市场条目：" + strings.Join(dropped, "；")
}

// fallbackID names a row that has no usable id of its own.
func fallbackID(id string) string {
	if strings.TrimSpace(id) == "" {
		return "(缺少 id)"
	}
	return id
}

// validateEntry turns one catalogue row into a market entry, or explains why it
// cannot be one.
func validateEntry(it catalogueItem, builtin bool) (MarketEntry, error) {
	if !IsValidID(it.ID) {
		return MarketEntry{}, fmt.Errorf("id 非法")
	}
	if strings.TrimSpace(it.Name) == "" {
		return MarketEntry{}, fmt.Errorf("缺少 name")
	}
	if !versionPattern.MatchString(it.Version) {
		return MarketEntry{}, fmt.Errorf("version 非法")
	}
	if strings.TrimSpace(it.Download) == "" {
		return MarketEntry{}, fmt.Errorf("缺少 download")
	}
	if !sha256Pattern.MatchString(it.SHA256) {
		return MarketEntry{}, fmt.Errorf("缺少合法的 sha256")
	}
	for _, p := range it.Permissions {
		if !IsAllowedPermission(p) {
			return MarketEntry{}, fmt.Errorf("permissions 含非法权限名 %s", p)
		}
	}
	entry := MarketEntry{
		ID:          it.ID,
		Name:        it.Name,
		Version:     it.Version,
		Author:      it.Author,
		Description: it.Description,
		Permissions: append([]string(nil), it.Permissions...),
		NetHosts:    append([]string(nil), it.NetHosts...),
		Download:    it.Download,
		SHA256:      it.SHA256,
	}
	if entry.Permissions == nil {
		entry.Permissions = []string{}
	}

	// A built-in entry's download names a file inside the embedded source,
	// reached through the catalogued relative path's last segment; a remote
	// entry's is a relative path against the base or an absolute URL.
	if builtin {
		if !validRelSegments(strings.TrimPrefix(it.Download, "./")) {
			return MarketEntry{}, fmt.Errorf("download 路径非法")
		}
		name := path.Base(it.Download)
		if _, err := sample.FS.ReadFile(name); err != nil {
			return MarketEntry{}, fmt.Errorf("内置源缺少 %s", name)
		}
		entry.Download = embeddedPrefix + name
		return entry, nil
	}
	if err := checkDownloadScheme(it.Download); err != nil {
		return MarketEntry{}, err
	}
	return entry, nil
}

// validRelSegments accepts a multi-segment relative path like "dist/x.zip".
func validRelSegments(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// checkDownloadScheme accepts an absolute http(s) URL or a relative path.
//
// Anything else is refused: the relative form is resolved against a base that
// has already been checked, and the absolute form is the only other thing a
// catalogue may legitimately carry.
func checkDownloadScheme(download string) error {
	if strings.Contains(download, "://") {
		u, err := url.Parse(download)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("插件包地址只支持 http/https：%s", download)
		}
		return nil
	}
	if !validRelSegments(strings.TrimPrefix(download, "./")) {
		return fmt.Errorf("插件包地址只支持 http/https：%s", download)
	}
	return nil
}

// joinNotices merges the fallback notice with the dropped-entry notice.
func joinNotices(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "；" + b
	}
}

// resolveDownload opens the package bytes for a download value.
//
// It is the single place the three forms are distinguished, so the install
// path never has to know whether it is talking to the network, to the
// configured mirror or to the binary's own sample source.
func (m *Manager) resolveDownload(download string) (io.ReadCloser, error) {
	if strings.HasPrefix(download, embeddedPrefix) {
		f, err := sample.FS.Open(strings.TrimPrefix(download, embeddedPrefix))
		if err != nil {
			return nil, fmt.Errorf("内置源缺少插件包：%w", err)
		}
		return f, nil
	}
	abs := download
	if !strings.Contains(download, "://") {
		base := m.baseURL()
		if base == "" {
			return nil, fmt.Errorf("未配置市场地址，无法下载 %s", download)
		}
		abs = strings.TrimRight(base, "/") + "/" + strings.TrimPrefix(download, "./")
	}
	if err := checkDownloadScheme(abs); err != nil {
		return nil, err
	}
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Get(abs)
	if err != nil {
		return nil, fmt.Errorf("下载插件包失败：%w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("下载插件包失败：HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// SampleIndexBytes returns the built-in catalogue's bytes, so a caller can show
// which sample is embedded in this build.
func SampleIndexBytes() ([]byte, error) {
	return sample.FS.ReadFile("index.json")
}

// readSample reads one file out of the built-in source.
func readSample(name string) ([]byte, error) {
	return sample.FS.ReadFile(name)
}
