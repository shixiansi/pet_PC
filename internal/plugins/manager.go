package plugins

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// sha256Pattern is the only shape an expected digest may take. It is lowercase
// on purpose: Get-FileHash and sha256.Sum256 both produce lowercase, and
// accepting uppercase would let a package's digest be written two ways.
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Extraction limits. A plugin package is front-end resources plus a manifest,
// so these are far above anything legitimate while still bounding what a
// hostile or broken archive can do to the disk.
const (
	maxEntrySize = 32 << 20  // 32 MiB
	maxTotalSize = 128 << 20 // 128 MiB
)

// indexFileName is the installed-plugin index inside the data directory.
const indexFileName = "plugins.json"

// Index is the persisted record of what is installed.
type Index struct {
	Version int          `json:"version"`
	Plugins []IndexEntry `json:"plugins"`
}

// IndexEntry is one installed plugin: what it is, whether it is switched on,
// and what the user has authorised.
//
// Granted is kept when a plugin is disabled, so switching it back on does not
// ask again for permissions the user already granted.
type IndexEntry struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	Enabled     bool      `json:"enabled"`
	Granted     []string  `json:"granted"`
	InstalledAt time.Time `json:"installedAt"`
}

// Installed is one row of the plugin manager's view: the index entry joined
// with the manifest on disk.
//
// A manifest that no longer parses does not remove the row - the row carries
// the reason instead, so the user can uninstall the broken plugin rather than
// being left with a directory they cannot see or remove from the UI.
type Installed struct {
	ID          string
	Version     string
	Enabled     bool
	Granted     []string
	InstalledAt time.Time
	// Manifest is nil when the on-disk manifest could not be loaded or failed
	// validation; Error then says why.
	Manifest *Manifest
	// Dir is the plugin's directory under the data directory.
	Dir string
	// Error is the manifest problem, empty when the row is usable.
	Error string
}

// Manager owns the installed plugins on disk.
//
// All state lives in the data directory: the index in plugins.json, the plugin
// files in plugins/<id>/ and each plugin's private values in
// plugin-data/<id>/. Nothing is cached in memory, so a Manager can be built
// again at any time and sees exactly what the last one wrote.
type Manager struct {
	dataDir  string
	warnings []string
	// BaseURL resolves the market base address. It is a function rather than a
	// field so the manager always reads the CURRENT configuration: the user can
	// change the address from the plugin tab, and the next Market() call has to
	// see the new value without the manager being rebuilt.
	//
	// Nil means "not configured", which falls back to the built-in sample
	// source.
	BaseURL func() string
	// Client is the HTTP client used for the catalogue and for packages. Nil
	// builds one per call with a bounded timeout.
	Client *http.Client
}

// NewManager opens (creating if needed) the plugin storage under dataDir.
//
// A corrupt index is quarantined rather than fatal - the same treatment
// config.json and todos.json get - because a plugin index must never be able to
// stop the pet from starting.
func NewManager(dataDir string) (*Manager, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("插件管理器缺少数据目录")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录 %s 失败：%w", dataDir, err)
	}
	m := &Manager{dataDir: dataDir}
	for _, d := range []string{m.pluginsDir(), m.tmpDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("创建插件目录 %s 失败：%w", d, err)
		}
	}
	if _, err := m.loadIndex(); err != nil {
		return nil, err
	}
	return m, nil
}

// DataDir returns the directory the manager is rooted at.
func (m *Manager) DataDir() string { return m.dataDir }

func (m *Manager) indexPath() string           { return filepath.Join(m.dataDir, indexFileName) }
func (m *Manager) pluginsDir() string          { return filepath.Join(m.dataDir, "plugins") }
func (m *Manager) tmpDir() string              { return filepath.Join(m.dataDir, "plugins-tmp") }
func (m *Manager) dataDirFor(id string) string { return filepath.Join(m.dataDir, "plugin-data", id) }

// PluginDir returns the directory an installed plugin's files live in. The id
// is validated first, so the join cannot escape the plugins directory.
func (m *Manager) PluginDir(id string) (string, error) {
	if !IsValidID(id) {
		return "", fmt.Errorf("非法插件 id：%q", id)
	}
	return filepath.Join(m.pluginsDir(), id), nil
}

// PrivateDir returns a plugin's private storage directory, creating it.
func (m *Manager) PrivateDir(id string) (string, error) {
	if !IsValidID(id) {
		return "", fmt.Errorf("非法插件 id：%q", id)
	}
	dir := m.dataDirFor(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建插件私有目录失败：%w", err)
	}
	return dir, nil
}

// Warnings returns non-fatal problems found while loading the index.
func (m *Manager) Warnings() []string {
	out := make([]string, len(m.warnings))
	copy(out, m.warnings)
	return out
}

// loadIndex reads plugins.json, quarantining it when it cannot be parsed.
func (m *Manager) loadIndex() (Index, error) {
	var idx Index
	data, err := os.ReadFile(m.indexPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Index{Version: 1}, nil
		}
		return Index{}, fmt.Errorf("读取 %s 失败：%w", indexFileName, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return Index{Version: 1}, nil
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		m.warnings = append(m.warnings, fmt.Sprintf("%s 无法解析（%v），已从空索引开始", indexFileName, err))
		if backup := quarantine(m.indexPath()); backup != "" {
			m.warnings[len(m.warnings)-1] += "，原文件已备份为 " + filepath.Base(backup)
		}
		return Index{Version: 1}, nil
	}
	if idx.Version == 0 {
		idx.Version = 1
	}
	return idx, nil
}

// saveIndex writes the index atomically (temp file + rename), so a crash
// mid-write cannot leave a half-written index behind.
func (m *Manager) saveIndex(idx Index) error {
	if idx.Version == 0 {
		idx.Version = 1
	}
	for i := range idx.Plugins {
		if idx.Plugins[i].Granted == nil {
			idx.Plugins[i].Granted = []string{}
		}
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(m.indexPath(), data)
}

// atomicWriteFile replaces path's contents in one step: a temp file in the same
// directory, flushed and closed, then renamed over the target.
//
// It is the package's ONE atomic-write helper, shared by the index and the
// plugin config store. A rename within one directory is atomic on every
// platform this ships to, so a reader (the pet, the market program) never sees a
// half-written file - which matters more here than elsewhere, because two
// processes read these files.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// quarantine renames an unreadable file to "<name>.bak" so the bad content is
// preserved for the user instead of being overwritten by the next save.
func quarantine(path string) string {
	backup := path + ".bak"
	os.Remove(backup) // keep only the most recent bad copy
	if err := os.Rename(path, backup); err != nil {
		return ""
	}
	return backup
}

// --- discovery -------------------------------------------------------------

// Plugins returns every installed plugin, in index order.
//
// The index is the skeleton - a plugin directory without an index entry is not
// a plugin the user installed - and the manifest is read per row. One row's
// broken manifest is reported on that row and nowhere else: the others stay
// fully usable.
func (m *Manager) Plugins() []Installed {
	idx, err := m.loadIndex()
	if err != nil {
		return nil
	}
	out := make([]Installed, 0, len(idx.Plugins))
	for _, e := range idx.Plugins {
		out = append(out, m.installedFrom(e))
	}
	return out
}

// installedFrom joins one index entry with its manifest.
func (m *Manager) installedFrom(e IndexEntry) Installed {
	row := Installed{
		ID:          e.ID,
		Version:     e.Version,
		Enabled:     e.Enabled,
		Granted:     append([]string(nil), e.Granted...),
		InstalledAt: e.InstalledAt,
	}
	if row.Granted == nil {
		row.Granted = []string{}
	}
	dir, err := m.PluginDir(e.ID)
	if err != nil {
		row.Error = err.Error()
		row.Enabled = false
		return row
	}
	row.Dir = dir
	man, err := LoadManifest(dir)
	if err != nil {
		// A row whose manifest cannot be read is not a usable plugin: it must
		// not stay enabled, or the dispatcher would look up capabilities in a
		// declaration that does not exist.
		row.Error = err.Error()
		row.Enabled = false
		return row
	}
	row.Manifest = &man
	return row
}

// Get returns one installed plugin by id.
func (m *Manager) Get(id string) (Installed, bool) {
	for _, p := range m.Plugins() {
		if p.ID == id {
			return p, true
		}
	}
	return Installed{}, false
}

// Pending returns the permissions a plugin declares but the user has not
// granted, preserving the manifest's order so the dialog reads the way the
// author wrote it.
func Pending(man Manifest, granted []string) []string {
	have := make(map[string]bool, len(granted))
	for _, g := range granted {
		have[g] = true
	}
	out := []string{}
	for _, p := range man.Permissions {
		if !have[p] {
			out = append(out, p)
		}
	}
	return out
}

// --- lifecycle -------------------------------------------------------------

// SetEnabled switches a plugin on or off and persists the change.
//
// Enabling is not granting: a plugin with no granted permissions can be enabled
// and will still be refused by every capability it calls. That separation is
// what lets the user turn a plugin off and back on without being asked to
// re-authorise it.
func (m *Manager) SetEnabled(id string, on bool) error {
	idx, err := m.loadIndex()
	if err != nil {
		return err
	}
	found := false
	for i := range idx.Plugins {
		if idx.Plugins[i].ID != id {
			continue
		}
		idx.Plugins[i].Enabled = on
		found = true
		break
	}
	if !found {
		return fmt.Errorf("插件 %s 未安装", id)
	}
	return m.saveIndex(idx)
}

// GrantPermissions records the permissions the user ticked.
//
// Only permissions the plugin actually declared may be granted: accepting an
// undeclared name would put a permission in the index that no manifest
// justifies, and the next manifest would silently inherit it.
func (m *Manager) GrantPermissions(id string, granted []string) (Installed, error) {
	row, ok := m.Get(id)
	if !ok {
		return Installed{}, fmt.Errorf("插件 %s 未安装", id)
	}
	if row.Manifest == nil {
		return Installed{}, fmt.Errorf("插件 %s 的 manifest 无法解析，无法授权", id)
	}
	declared := make(map[string]bool, len(row.Manifest.Permissions))
	for _, p := range row.Manifest.Permissions {
		declared[p] = true
	}
	for _, g := range granted {
		if !declared[g] {
			return Installed{}, fmt.Errorf("插件 %s 未声明权限 %s，不能授权", id, g)
		}
	}
	idx, err := m.loadIndex()
	if err != nil {
		return Installed{}, err
	}
	applied := false
	for i := range idx.Plugins {
		if idx.Plugins[i].ID != id {
			continue
		}
		idx.Plugins[i].Granted = append([]string(nil), granted...)
		if idx.Plugins[i].Granted == nil {
			idx.Plugins[i].Granted = []string{}
		}
		applied = true
		break
	}
	if !applied {
		return Installed{}, fmt.Errorf("插件 %s 未安装", id)
	}
	if err := m.saveIndex(idx); err != nil {
		return Installed{}, err
	}
	saved, ok := m.Get(id)
	if !ok {
		return Installed{}, fmt.Errorf("插件 %s 未安装", id)
	}
	return saved, nil
}

// Uninstall deletes a plugin's files, its private storage and its index entry.
//
// The private storage is removed as well: the user asked to be rid of the
// plugin, and a leftover directory under plugin-data/ would be state nothing
// can ever read or clean up again.
// Uninstall removes the plugin, and — only when asked — the data it left behind.
//
// WHY `purge` IS A PARAMETER AND DEFAULTS TO FALSE. The two halves are different decisions:
// deleting `plugins/<id>` is a ROLLBACK (install the same version again and it picks up where
// it left off, because `plugin-data/<id>` is still there), while deleting `plugin-data/<id>`
// destroys the settings, the stored secrets and whatever the plugin was in the middle of. The
// first is undoable by installing again; the second is not recoverable at all. So it is the
// user's call, made in the confirmation dialog, and the safe answer is the one that has to be
// asked for. (Before this flag the private directory was always deleted, which made "卸载"
// quietly the most destructive button in the program.)
func (m *Manager) Uninstall(id string, purge bool) error {
	idx, err := m.loadIndex()
	if err != nil {
		return err
	}
	out := make([]IndexEntry, 0, len(idx.Plugins))
	found := false
	for _, e := range idx.Plugins {
		if e.ID == id {
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		return fmt.Errorf("插件 %s 未安装", id)
	}
	dir, err := m.PluginDir(id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("删除插件目录失败：%w", err)
	}
	if purge {
		if err := os.RemoveAll(m.dataDirFor(id)); err != nil {
			return fmt.Errorf("删除插件私有目录失败：%w", err)
		}
	}
	idx.Plugins = out
	return m.saveIndex(idx)
}

// --- install ---------------------------------------------------------------

// Install downloads (or reads) a package, verifies it and unpacks it.
//
// The order is fixed and every step is all-or-nothing: the index is written
// only after the files are in their final place, and any failure removes
// whatever this call created. So a failed install leaves the installed set
// exactly as it found it - which is what makes it safe to retry.
func (m *Manager) Install(entry MarketEntry) (Installed, error) {
	return m.install(entry, false)
}

// Update replaces an installed plugin with a NEWER package of the same id.
//
// It exists because Install alone left the user with no way forward. Install
// refuses an id that is already present ("已安装，请先卸载"), and the market hides the
// install button once a plugin is installed - so a plugin whose manifest gained a
// field in a later version could only be updated by uninstalling it first, which
// silently throws away its private data: the conversation plugin's own AI settings
// and API key live in `plugin-data/<id>/`, and `Uninstall` deletes that directory.
// That is a terrible price for a bug fix, and it is exactly the trap a user hits
// when the catalogue moves ahead of what they have.
//
// What the update PRESERVES is the point of having it at all: the enabled flag and
// the granted permissions carry over, so an update is not a re-authorisation. The
// files are replaced wholesale from the new package, which is what makes it an
// update rather than a merge.
//
// It refuses to move BACKWARDS: an entry that is not newer than what is installed
// is rejected, so a stale mirror cannot silently downgrade a working plugin and
// reintroduce the very bug the newer version fixed.
func (m *Manager) Update(entry MarketEntry) (Installed, error) {
	current, ok := m.Get(entry.ID)
	if !ok {
		return Installed{}, fmt.Errorf("插件 %s 未安装，无法更新", entry.ID)
	}
	if !IsNewerVersion(entry.Version, current.Version) {
		return Installed{}, fmt.Errorf("插件 %s 已是最新版本（%s），市场条目为 %s",
			entry.ID, current.Version, entry.Version)
	}
	return m.install(entry, true)
}

// install is the shared body of Install and Update.
//
// `replacing` is true for an update. It changes three things and nothing else: the
// already-installed guard becomes an assertion instead of a refusal, the index entry
// carries the previous enabled/granted state forward, and the private directory is
// left alone.
func (m *Manager) install(entry MarketEntry, replacing bool) (Installed, error) {
	if !IsValidID(entry.ID) {
		return Installed{}, fmt.Errorf("市场条目的 id 非法：%q", entry.ID)
	}
	if !sha256Pattern.MatchString(entry.SHA256) {
		return Installed{}, fmt.Errorf("市场条目的 sha256 格式非法：%q", entry.SHA256)
	}
	previous, existed := m.Get(entry.ID)
	// The state an UPDATE must carry forward. An install has none to carry.
	carryEnabled := false
	carryGranted := []string{}
	if replacing {
		if !existed {
			return Installed{}, fmt.Errorf("插件 %s 未安装，无法更新", entry.ID)
		}
		carryEnabled = previous.Enabled
		carryGranted = append([]string(nil), previous.Granted...)
	} else if existed {
		return Installed{}, fmt.Errorf("插件 %s 已安装，请先卸载", entry.ID)
	}
	target, err := m.PluginDir(entry.ID)
	if err != nil {
		return Installed{}, err
	}
	if !replacing {
		if _, statErr := os.Stat(target); statErr == nil {
			return Installed{}, fmt.Errorf("插件 %s 已安装，请先卸载", entry.ID)
		}
	}

	// 1. Fetch the archive and hash it as it arrives, so a package is never
	//    written to the plugin directory before its digest has been checked.
	m.clearTmp()
	defer m.clearTmp()

	zipPath := filepath.Join(m.tmpDir(), entry.ID+".zip")
	got, err := m.fetchTo(entry.Download, zipPath)
	if err != nil {
		return Installed{}, err
	}
	if got != entry.SHA256 {
		os.Remove(zipPath)
		return Installed{}, fmt.Errorf("sha256 校验失败：期望 %s，实际 %s", entry.SHA256, got)
	}

	// 2. Unpack into a staging directory, checking every entry on the way.
	staging := filepath.Join(m.tmpDir(), entry.ID)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return Installed{}, fmt.Errorf("创建临时目录失败：%w", err)
	}
	if err := extractZip(zipPath, staging); err != nil {
		return Installed{}, err
	}

	// 3. The manifest INSIDE the package is authoritative. If the catalogue
	//    advertises different permissions from the ones the package asks for,
	//    what the user was shown is not what they would be authorising, so the
	//    install is refused rather than guessed at.
	man, err := LoadManifest(staging)
	if err != nil {
		return Installed{}, err
	}
	// The two comparisons report SEPARATELY, and that is not cosmetic: the
	// combined version printed the permission lists for a netHosts mismatch, so
	// the message showed two identical lists and read as a broken comparison.
	// It cost a real diagnosis - the actual cause was the catalogue not carrying
	// netHosts at all - so each field now names itself and carries its own values.
	if !sameStrings(man.Permissions, entry.Permissions) {
		return Installed{}, fmt.Errorf("市场条目与插件包的权限声明不一致：条目 %v，插件包 %v",
			entry.Permissions, man.Permissions)
	}
	if !sameStrings(man.NetHosts, entry.NetHosts) {
		return Installed{}, fmt.Errorf("市场条目与插件包的域名白名单不一致：条目 %v，插件包 %v",
			entry.NetHosts, man.NetHosts)
	}

	// 4. Move the staging directory into place, then record it.
	if err := os.RemoveAll(target); err != nil {
		return Installed{}, fmt.Errorf("清理目标目录失败：%w", err)
	}
	if err := os.Rename(staging, target); err != nil {
		return Installed{}, fmt.Errorf("安装插件失败：%w", err)
	}

	idx, err := m.loadIndex()
	if err != nil {
		os.RemoveAll(target)
		return Installed{}, err
	}
	// The entry is REPLACED in place when this is an update, not appended. Appending
	// would leave two rows for one id, and `Plugins()` walks the index in order - so
	// the list would show the plugin twice and the slot resolver, which takes the
	// first match, could hand back the OLD version's row.
	next := IndexEntry{
		ID:      man.ID,
		Version: man.Version,
		// On a fresh install: installed but NOT enabled, with nothing granted -
		// the user has seen what it asks for but has not yet said yes to any of it.
		//
		// On an UPDATE: the enabled flag and the granted permissions are carried
		// over, because an update is not a re-authorisation. Resetting them would
		// both disable a working plugin and re-prompt for permissions the user
		// already granted, which is a worse experience than not offering updates
		// at all.
		Enabled:     carryEnabled,
		Granted:     carryGranted,
		InstalledAt: time.Now(),
	}
	replaced := false
	for i := range idx.Plugins {
		if idx.Plugins[i].ID == man.ID {
			idx.Plugins[i] = next
			replaced = true
			break
		}
	}
	if !replaced {
		idx.Plugins = append(idx.Plugins, next)
	}
	if err := m.saveIndex(idx); err != nil {
		os.RemoveAll(target)
		return Installed{}, err
	}
	row, _ := m.Get(man.ID)
	return row, nil
}

// fetchTo downloads (or copies) the package at download into dest, returning
// the lowercase hex sha256 of the bytes written.
func (m *Manager) fetchTo(download, dest string) (string, error) {
	src, err := m.resolveDownload(download)
	if err != nil {
		return "", err
	}
	defer src.Close()

	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败：%w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hasher), src); err != nil {
		return "", fmt.Errorf("下载插件包失败：%w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// clearTmp empties the staging directory, keeping the directory itself so its
// absence can never be the thing that breaks an install.
func (m *Manager) clearTmp() {
	dir := m.tmpDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

// sameStrings reports whether two string lists hold the same values, ignoring
// order (a manifest and a catalogue entry written by different tools may list
// the same permissions in a different order).
func sameStrings(a, b []string) bool {
	x, y := sortedPermissions(a), sortedPermissions(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// extractZip unpacks zipPath into dest, refusing anything that would land
// outside dest.
//
// Three separate checks because they catch different mistakes: an absolute
// path, a ".." segment, and a relative path that resolves outside the root.
// Non-regular entries are refused too - a symlink in an archive is a way to
// make a later entry write somewhere else.
func extractZip(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开插件包失败：%w", err)
	}
	defer zr.Close()

	root, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	var total uint64
	for _, f := range zr.File {
		name := f.Name
		// A DIRECTORY entry's name ends in "/" - that is what the zip format says a
		// directory entry looks like, and every common packer writes them (PowerShell's
		// Compress-Archive emits one per folder, even for folders that also have files in
		// them). ValidRelPath splits on "/" and refuses an empty segment, so the trailing
		// slash had to be trimmed BEFORE the check or this branch was unreachable and a
		// perfectly ordinary package was refused with "非法条目路径" - which is exactly what
		// happened to the QQ-bot package on 2026-10-29 (three failed installs, and the
		// message named a folder as if it were a traversal attempt).
		//
		// The trim is confined to directory entries on purpose: for a FILE the trailing
		// slash is a mistake, and ValidRelPath should keep refusing it.
		if f.FileInfo().IsDir() {
			name = strings.TrimSuffix(name, "/")
		}
		if !ValidRelPath(name) {
			return fmt.Errorf("插件包含非法条目路径：%q", f.Name)
		}
		if filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
			return fmt.Errorf("插件包含非法条目路径：%q", f.Name)
		}
		full := filepath.Join(root, filepath.FromSlash(name))
		rel, err := filepath.Rel(root, full)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("插件包含非法条目路径：%q", f.Name)
		}
		if !f.FileInfo().Mode().IsRegular() && !f.FileInfo().IsDir() {
			return fmt.Errorf("插件包含非普通文件条目：%q", name)
		}
		// The declared size is checked before writing anything, so an archive
		// that claims to expand to gigabytes is refused without ever reaching
		// the disk. The copy below is additionally bounded, because a header
		// can lie.
		if f.UncompressedSize64 > maxEntrySize {
			return fmt.Errorf("插件包条目 %q 超过单条目解压上限 %d 字节", name, maxEntrySize)
		}
		total += f.UncompressedSize64
		if total > maxTotalSize {
			return fmt.Errorf("插件包解压总大小超过上限 %d 字节", maxTotalSize)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(full, 0o755); err != nil {
				return fmt.Errorf("创建目录失败：%w", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("创建目录失败：%w", err)
		}
		if err := writeZipEntry(f, full); err != nil {
			return err
		}
	}
	return nil
}

// writeZipEntry writes one regular file, bounded by maxEntrySize.
func writeZipEntry(f *zip.File, full string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("读取插件包条目 %q 失败：%w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("写入插件文件失败：%w", err)
	}
	defer out.Close()

	// One byte past the limit, so "exactly at the limit" passes and anything
	// above it is detected rather than silently truncated.
	if _, err := io.Copy(out, io.LimitReader(rc, maxEntrySize+1)); err != nil {
		return fmt.Errorf("解压插件包条目 %q 失败：%w", f.Name, err)
	}
	if fi, err := out.Stat(); err == nil && fi.Size() > maxEntrySize {
		return fmt.Errorf("插件包条目 %q 超过单条目解压上限 %d 字节", f.Name, maxEntrySize)
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

// PluginIDs returns the installed ids, sorted. Used by tests and diagnostics.
func (m *Manager) PluginIDs() []string {
	rows := m.Plugins()
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	sort.Strings(out)
	return out
}
