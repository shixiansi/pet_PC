package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"PetPC/internal/logging"
	"PetPC/internal/plugins"
	"PetPC/internal/store"
)

// Market is the struct bound to the frontend.
//
// Every method here is a PROJECTION over internal/plugins: this program decides
// nothing about what a plugin may do, what a package may contain or what an
// install means. It shows the manager's rows and forwards the user's decisions
// back to it. That is what keeps the pet and the market from disagreeing about
// what is installed - there is one implementation and both programs call it.
type Market struct {
	ctx context.Context

	// dataDir is the directory shared with the pet.
	dataDir string
	// cfgPath is this program's own settings file.
	cfgPath string
	// manager is the shared lifecycle implementation.
	manager *plugins.Manager
	// keeper encrypts declared secrets at rest.
	keeper plugins.SecretKeeper

	// log is this program's own log, in <dataDir>\logs\market.log.
	//
	// Its OWN file rather than a shared one with the pet: two processes appending to
	// one file cannot rotate it safely (each would rename a file the other still has
	// open), and the 日志 page merges the two by timestamp anyway - which is the only
	// thing a user wanted from "one log" in the first place.
	log *logging.Logger

	// registry is this program's capability table, built on first use.
	//
	// It is per program on purpose: see pluginhost.go for why the market serves
	// audio/storage/config/net.fetch and NOT todos/timer/bubble/surface/llm.
	registry *plugins.Registry

	// audioMu guards the audition sink, and audioOut is that sink. Separate from
	// mu because an audition runs on a plugin's request goroutine, and the
	// catalogue reads must not queue behind it.
	audioMu  sync.Mutex
	audioOut audioSink

	mu  sync.Mutex
	cfg marketConfig

	// callsMu guards the lazy throttle below; callsLog collapses repeated per-call log
	// lines (one per plugin+method+level per five seconds). See internal/logging.Throttle.
	callsMu  sync.Mutex
	callsLog *logging.Throttle
}

// NewMarket opens the shared data directory and this program's settings.
func NewMarket() (*Market, error) {
	dir, err := store.DefaultDir()
	if err != nil {
		return nil, fmt.Errorf("找不到数据目录：%w", err)
	}
	return newMarketAt(dir, "")
}

// newMarketAt opens a market rooted at dir, with an optional settings-file
// override.
//
// It is split out from NewMarket so the tests can drive the real code against a
// temp directory: NewMarket's only job is resolving the default location, which
// is the one part that cannot be tested without touching the user's real data.
func newMarketAt(dir, cfgPathOverride string) (*Market, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	m := &Market{
		dataDir: dir,
		cfgPath: filepath.Join(dir, "plugin-market.json"),
		keeper:  marketSecretKeeper{},
	}
	// Always on, at the same default as the pet, so a report about "控制中心打不开插件界面"
	// has something to read without anyone relaunching with a flag. The level override is
	// the pet's: PETPC_LOG_LEVEL, then PETPC_DEBUG, then INFO.
	m.log = logging.Open(dir, logging.MarketLog, logging.Options{Level: logLevelFromEnv()})
	if cfgPathOverride != "" {
		m.cfgPath = cfgPathOverride
	}
	m.cfg = loadMarketConfig(m.cfgPath, dir)

	manager, err := plugins.NewManager(dir)
	if err != nil {
		return nil, err
	}
	// The base address is read through a function so the manager always sees the
	// CURRENT value: the user can retype it in the address box, and the next
	// catalogue fetch has to use the new one without the manager being rebuilt.
	manager.BaseURL = func() string {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.cfg.MarketBaseURL
	}
	m.manager = manager

	// Either program may be the first one opened after the upgrade, so both run
	// the one-shot migration (T64c). It is idempotent and safe when there is
	// nothing to move.
	if err := plugins.MigrateChatData(dir, m.keeper); err != nil {
		// Not fatal: the plugin still works, it just has no settings yet, and
		// the pet will retry on its next start.
		fmt.Fprintf(os.Stderr, "plugin-market: 迁移对话插件数据失败：%v\n", err)
	}
	return m, nil
}

// startup records the Wails context.
func (m *Market) startup(ctx context.Context) {
	m.ctx = ctx
	// The first line of every session names the log file and the level, so an empty
	// 日志 page can be told apart from a quiet one.
	m.log.Infof("market", "启动：数据目录 %s，日志级别 %s，日志文件 %s",
		m.dataDir, m.log.Level(), m.log.Path())
}

// shutdown exists so the window's lifecycle is explicit; there is nothing to
// flush, because every binding writes through to disk.
func (m *Market) shutdown(context.Context) {}

// --- location ---------------------------------------------------------------

// PluginMarketPath reports where this program's executable is.
//
// The pet launches this program by looking for it beside its own executable
// first, so showing the path answers "which copy is the pet actually going to
// start?" - a question that only comes up when there are two.
func (m *Market) PluginMarketPath() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	path, err := filepath.Abs(exe)
	if err != nil {
		return exe, true
	}
	return path, true
}

// --- catalogue and installed list -------------------------------------------

// Installed returns every installed plugin, in index order.
//
// The manager holds no in-memory state (it re-reads plugins.json on every call),
// so a change the PET made - or a second market window - is visible here
// immediately, with no refresh protocol between the two processes.
func (m *Market) Installed() ([]plugins.Installed, error) {
	return m.manager.Plugins(), nil
}

// Warnings returns non-fatal problems found while reading the index, so a
// quarantined index is reported rather than looking like "no plugins".
func (m *Market) Warnings() []string { return m.manager.Warnings() }

// Catalogue fetches the market catalogue.
func (m *Market) Catalogue() (plugins.MarketView, error) {
	return m.manager.Market()
}

// SaveMarketURL stores a new base address and returns the catalogue fetched from
// it.
//
// A bad address is refused BEFORE anything is written, so a typo cannot leave
// the market pointing at a source it cannot use - the previous, working address
// stays in place and the user sees why.
func (m *Market) SaveMarketURL(raw string) (plugins.MarketView, error) {
	trimmed := strings.TrimSpace(raw)
	if err := checkMarketURL(trimmed); err != nil {
		m.log.Warnf("market", "拒绝这个市场地址（%s）：%v", trimmed, err)
		return plugins.MarketView{}, err
	}
	m.mu.Lock()
	m.cfg.MarketBaseURL = strings.TrimRight(trimmed, "/")
	cfg := m.cfg
	m.mu.Unlock()
	if err := saveMarketConfig(m.cfgPath, cfg); err != nil {
		return plugins.MarketView{}, err
	}
	m.log.Infof("market", "市场地址已保存：%s", cfg.MarketBaseURL)
	return m.manager.Market()
}

// --- lifecycle --------------------------------------------------------------

// Install downloads, verifies and unpacks a plugin. It does NOT enable it:
// installing and enabling are separate steps so the user can look at what was
// installed before letting it run.
func (m *Market) Install(id string) error {
	entry, err := m.catalogueEntry(id)
	if err != nil {
		return err
	}
	row, err := m.manager.Install(entry)
	if err != nil {
		m.log.Pluginf(logging.LevelWarn, "market", id, "安装失败：%v", err)
		return err
	}
	m.log.Pluginf(logging.LevelInfo, "market", id, "已安装 v%s（未启用，权限都还没授予）", row.Version)
	return nil
}

// Update replaces an installed plugin with the newer package the catalogue offers,
// keeping it enabled and keeping the permissions the user already granted.
//
// This is a separate binding from Install rather than a flag on it, because the two
// do different things to state the user cares about: Install adds a DISABLED plugin
// with nothing granted, while Update must not touch either. Folding them together
// would mean either an update that silently disables the plugin or an install that
// silently enables one the user has not looked at yet.
//
// It exists because without it an outdated plugin had no way forward: the market
// hides the install button once a plugin is installed, and the manager refuses a
// second install of a present id, so the only route was uninstall-then-install -
// which deletes the plugin's private directory, taking its saved settings and API
// key with it.
func (m *Market) Update(id string) error {
	entry, err := m.catalogueEntry(id)
	if err != nil {
		return err
	}
	row, err := m.manager.Update(entry)
	if err != nil {
		m.log.Pluginf(logging.LevelWarn, "market", id, "更新失败：%v", err)
		return err
	}
	m.log.Pluginf(logging.LevelInfo, "market", id, "已更新到 v%s（启用状态与已授予的权限保持不变）", row.Version)
	return nil
}

// catalogueEntry finds one catalogue row by id.
func (m *Market) catalogueEntry(id string) (plugins.MarketEntry, error) {
	got, err := m.manager.Market()
	if err != nil {
		return plugins.MarketEntry{}, err
	}
	for _, e := range got.Entries {
		if e.ID == id {
			return e, nil
		}
	}
	return plugins.MarketEntry{}, fmt.Errorf("市场里没有插件 %s", id)
}

// Uninstall removes a plugin, and — only when asked — its private storage with it.
//
// `purgeData` is the checkbox in the confirmation dialog, and it is the user's answer to a
// question only they can answer: "reinstall later and continue" (keep `plugin-data/<id>`:
// settings, stored keys, and whatever the plugin was in the middle of) versus "be gone"
// (delete it). The package itself always goes; the data goes only when they ticked the box.
func (m *Market) Uninstall(id string, purgeData bool) error {
	if err := m.manager.Uninstall(id, purgeData); err != nil {
		m.log.Pluginf(logging.LevelWarn, "market", id, "卸载失败：%v", err)
		return err
	}
	if purgeData {
		// WARN and not INFO: this deleted the plugin's private directory — its settings, its
		// stored key and its data — which is not recoverable. That belongs above the ordinary
		// lifecycle lines.
		m.log.Pluginf(logging.LevelWarn, "market", id, "已卸载，并按用户的选择删除了私有目录（设置、密钥、数据）")
		return nil
	}
	// INFO: the user asked to keep it, and saying so is what makes "install it again and nothing
	// is lost" a promise the log backs up.
	m.log.Pluginf(logging.LevelInfo, "market", id, "已卸载，私有目录保留（重新安装接着用）")
	return nil
}

// SetEnabled switches a plugin on or off.
func (m *Market) SetEnabled(id string, on bool) error {
	if err := m.manager.SetEnabled(id, on); err != nil {
		m.log.Pluginf(logging.LevelWarn, "market", id, "切换启用状态失败：%v", err)
		return err
	}
	state := "停用"
	if on {
		state = "启用"
	}
	m.log.Pluginf(logging.LevelInfo, "market", id, "已%s", state)
	return nil
}

// Grant records the permissions the user ticked. The manager refuses any name
// the plugin did not declare.
func (m *Market) Grant(id string, granted []string) error {
	row, err := m.manager.GrantPermissions(id, granted)
	if err != nil {
		m.log.Pluginf(logging.LevelWarn, "market", id, "保存授权失败：%v", err)
		return err
	}
	// The permission NAMES are logged; nothing else about them is. A user asking "为什么
	// 那个插件什么也做不了" needs to see exactly what is ticked, and a permission name is
	// not a secret.
	m.log.Pluginf(logging.LevelInfo, "market", id, "授权已保存：%v", row.Granted)
	return nil
}

// --- plugin settings --------------------------------------------------------

// ConfigEditor is one declared setting, ready to draw.
//
// It is the manifest's declaration JOINED with the stored value, so the frontend
// never has to correlate two structures - and, for a secret, carries only
// whether one is stored and how it masks. The plaintext is never in this type.
type ConfigEditor struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
	// Value is the current value for a non-secret item, "" for a secret one.
	Value string `json:"value"`
	// Default is shown as a placeholder when Value is empty.
	Default string `json:"default,omitempty"`
	// Secret marks the item whose value is write-only.
	Secret bool `json:"secret,omitempty"`
	// HostWhitelist marks the item that feeds the net.fetch allow-list.
	HostWhitelist bool `json:"hostWhitelist,omitempty"`
	// SecretSet / SecretMask describe a stored secret without revealing it.
	SecretSet  bool   `json:"secretSet,omitempty"`
	SecretMask string `json:"secretMask,omitempty"`
	// Options / Multiple describe a `select` item's choices, straight from the
	// manifest: the editor draws them, it does not invent them.
	Options  []plugins.ConfigOption `json:"options,omitempty"`
	Multiple bool                   `json:"multiple,omitempty"`
}

// PluginConfig is one plugin's settings as the editor sees them.
type PluginConfig struct {
	PluginID string         `json:"pluginId"`
	Name     string         `json:"name"`
	Dir      string         `json:"dir"`
	Items    []ConfigEditor `json:"items"`
	Usage    string         `json:"usage,omitempty"`
	Error    string         `json:"error,omitempty"`
}

// GetConfig returns the editor for one plugin's declared settings.
func (m *Market) GetConfig(id string) (PluginConfig, error) {
	row, ok := m.manager.Get(id)
	if !ok {
		return PluginConfig{}, fmt.Errorf("插件 %s 未安装", id)
	}
	if row.Manifest == nil {
		return PluginConfig{}, fmt.Errorf("插件 %s 的 manifest 无法解析", id)
	}
	dir, err := m.manager.PrivateDir(id)
	if err != nil {
		return PluginConfig{}, err
	}
	store := plugins.NewConfigStore(dir, *row.Manifest, m.keeper)
	values, err := store.Read()
	if err != nil {
		return PluginConfig{}, err
	}

	out := PluginConfig{
		PluginID: id,
		Name:     row.Manifest.Name,
		Dir:      dir,
		Items:    make([]ConfigEditor, 0, len(row.Manifest.Config)),
	}
	for _, item := range row.Manifest.Config {
		editor := ConfigEditor{
			Key:           item.Key,
			Label:         item.Label,
			Type:          item.Type,
			Default:       item.Default,
			Secret:        item.Secret,
			HostWhitelist: item.HostWhitelist,
			Options:       item.Options,
			Multiple:      item.Multiple,
		}
		if item.Secret {
			state := values.Secrets[item.Key]
			editor.SecretSet = state.Set
			editor.SecretMask = state.Mask
		} else {
			editor.Value = values.Values[item.Key]
		}
		out.Items = append(out.Items, editor)
	}
	out.Usage = m.usageLine(dir)
	return out, nil
}

// SaveConfig writes the plugin's settings.
//
// A secret arrives only when the user actually typed a new one - the editor
// sends the mask back untouched otherwise - so an absent secret key means "leave
// it alone" rather than "clear it".
func (m *Market) SaveConfig(id string, values map[string]string) error {
	row, ok := m.manager.Get(id)
	if !ok {
		return fmt.Errorf("插件 %s 未安装", id)
	}
	if row.Manifest == nil {
		return fmt.Errorf("插件 %s 的 manifest 无法解析", id)
	}
	dir, err := m.manager.PrivateDir(id)
	if err != nil {
		return err
	}
	store := plugins.NewConfigStore(dir, *row.Manifest, m.keeper)
	if err := store.Write(values); err != nil {
		m.log.Pluginf(logging.LevelWarn, "market", id, "保存设置失败：%v", err)
		return err
	}
	// Which KEYS were written, never their values: a plugin's settings hold model
	// names and folder paths (fine) as well as secrets (never).
	m.log.Pluginf(logging.LevelInfo, "market", id, "设置已保存（%d 项，键：%v）", len(values), sortedKeys(values))
	return nil
}

// RememberSelection stores which plugin the user last had open.
func (m *Market) RememberSelection(id string) error {
	m.mu.Lock()
	m.cfg.LastSelectedID = id
	cfg := m.cfg
	m.mu.Unlock()
	return saveMarketConfig(m.cfgPath, cfg)
}

// SelectedID returns the remembered selection, so reopening the window lands
// where the user left off.
func (m *Market) SelectedID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.LastSelectedID
}

// Theme answers with the scheme BOTH windows draw in.
//
// It is read from the file the two programs share (internal/store/theme.go), not from this
// program's own config, because the pet's right-click menu can change it as well: one value with
// two owners in two files is how the menu ended up dark while this window was light (reported
// 2026-10-29 as 一个亮一个暗的，像是独立的). m.cfg.Theme is only the fallback for an install that
// predates the shared file; the first flip from either window writes it and this field stops
// being consulted.
func (m *Market) Theme() string {
	m.mu.Lock()
	legacy := strings.TrimSpace(m.cfg.Theme)
	dir := m.dataDir
	m.mu.Unlock()
	if theme := store.ReadPanelTheme(dir); theme != "" {
		return theme
	}
	// Nothing shared yet. An install that predates the shared file keeps whatever its own config
	// said; one with no opinion at all gets the default, which is LIGHT - the same answer the pet's
	// layout gives - so a fresh install cannot start with the two windows disagreeing.
	if legacy == "" {
		return store.ThemeLight
	}
	return store.NormalizeTheme(legacy)
}

// SetTheme stores the colour scheme for both windows.
//
// An unknown value is normalised rather than refused: a look must never be able to fail a save.
// It writes the SHARED file - the pet picks the change up on its next layout push - and it does
// not rewrite either program's config, so nothing else in them can be clobbered by a colour.
func (m *Market) SetTheme(name string) error {
	theme := store.NormalizeTheme(name)
	m.mu.Lock()
	dir := m.dataDir
	m.cfg.Theme = theme
	m.mu.Unlock()
	return store.WritePanelTheme(dir, theme)
}

// normalizeTheme maps anything that is not "light" onto "dark" - the rule this program's own
// field used before the scheme became shared. Only m.cfg.Theme reads are left on it (see Theme),
// and every write goes through store.NormalizeTheme so an unknown value cannot mean "dark" here
// and "light" in the pet.
func normalizeTheme(name string) string {
	if strings.EqualFold(strings.TrimSpace(name), themeLight) {
		return themeLight
	}
	return themeDark
}


// usageLine reads the usage counter the pet keeps for this plugin.
//
// Only this program and the pet read it; the PET is the only writer, because the
// budget has to be enforced where the request is made.
func (m *Market) usageLine(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "llm-usage"))
	if err != nil {
		return ""
	}
	var raw struct {
		Date     string `json:"date"`
		Requests int    `json:"requests"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || raw.Date == "" {
		return ""
	}
	return fmt.Sprintf("%s：%d 次请求", raw.Date, raw.Requests)
}
