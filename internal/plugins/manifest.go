// Package plugins implements the pet's plugin host.
//
// It owns five things and nothing else: manifest parsing and validation
// (manifest.go), the installed-plugin index and the whole install /
// enable / grant / uninstall lifecycle (manager.go), the market catalogue
// (market.go), the fixed capability table (capability.go) and the built-in
// sample source (sample/).
//
// The package deliberately uses the standard library only - archive/zip,
// crypto/sha256, encoding/json, net/http and embed - so the module keeps its
// exact dependency set and the app still builds offline. It also does NOT
// import internal/desktop: the window-facing geometry lives in the root
// package, which keeps the two free of a cycle.
package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Manifest is one plugin's declaration, as found in manifest.json inside the
// zip and in the installed directory.
//
// Every field is validated by ParseManifest before it is trusted: the file
// comes from the network, so nothing in it may be assumed.
type Manifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	// NetHosts is the allow-list for net.fetch. It is only meaningful together
	// with PermNetFetch, and ParseManifest rejects a net.fetch plugin that
	// declares no hosts: an unenforceable allow-list is worse than none, since
	// the permission would read as "restricted" while permitting everything.
	NetHosts []string `json:"netHosts,omitempty"`
	// Panel is GONE from the pet, and kept only where it still means something.
	//
	// It used to allow a plugin an in-panel view (the panel tab's 380x520 scroll area). The
	// panel itself has retired (openspec/changes/todo-pomodoro-as-plugins, stage 3), so no
	// host reads this field any more - see PanelSlot for what replaced it, and note that the
	// field is still parsed because an installed manifest from an older build carries it and
	// a parse that rejected unknown keys would make every such plugin uninstallable.
	Panel bool `json:"panel"`
	// PanelSlot names WHICH slot the plugin's view occupies, and it is the pet's MENU that
	// resolves the name now - not a panel tab.
	//
	// The panel is gone, so `chat` no longer names a tab body; what survives is the half that
	// was always about finding an owner: "who do I ask when the user picks this menu cell".
	// `todos` and `pomodoro` are the menu's own slots - see PanelSlotTodos / PanelSlotPomodoro.
	// Kept as `panelSlot` in JSON rather than renamed to `menuCell`: an installed manifest is
	// read from disk and a rename would make every plugin already on a user's machine an
	// illegal manifest on the next launch.
	PanelSlot string `json:"panelSlot,omitempty"`
	// Surface is the list of rectangles the plugin wants the window region to
	// be extended by. Coordinates are CSS pixels relative to the pet's
	// top-left corner.
	Surface []Rect `json:"surface,omitempty"`
	// Config is what the plugin wants to configure. The host validates it, the
	// market program draws an editor for it, and the values live in the plugin's
	// private directory - so a plugin's settings are inside the directory that
	// disappears when it is uninstalled.
	Config []ConfigItem `json:"config,omitempty"`
	// Background asks the host for a HEADLESS frame that stays mounted for the
	// life of the process.
	//
	// It exists because a plugin's events are delivered only to a frame the host
	// has mounted (the frontend drops an event with "no interface on screen"), and
	// some plugins must act at moments when the user has no interface open at all.
	// A voice plugin is the motivating case: the pomodoro ends whether or not the
	// panel is showing, and the plugin has to hear about it to speak.
	//
	// It is deliberately NOT a free-standing flag: ParseManifest requires the plugin to
	// also declare one of the permissions that can actually DELIVER something to a frame
	// (`events` / `net.ws` / `net.fetch` / `plugin.call`, see validateBackground). There is no
	// `background` permission - a permission with no method behind it is the switch this
	// vocabulary refuses to contain.
	Background bool `json:"background,omitempty"`
	// ShareLLM lets OTHER plugins use this plugin's model settings and credential
	// through `llm.borrow`, instead of each of them asking the user to paste the
	// same key again.
	//
	// It is opt-in by the LENDER's manifest and not a host-wide setting: the
	// declaration says "the author of this plugin is willing to be the one place
	// the model is configured", and a plugin that would rather keep its endpoint
	// to itself simply omits the field. The host still requires the BORROWER to
	// hold the llm.borrow permission, so both ends of the transfer are declared
	// and the user is asked about exactly one of them (the one being installed).
	ShareLLM bool `json:"shareLLM,omitempty"`
	// ShareRead lists the OTHER plugins' shared values this plugin may read, as
	// "<ownerId>/<name>" pairs.
	//
	// The pair list is the whole authorisation model for `share.get`: writing is
	// free but restricted to the caller's own prefix, reading needs an entry here
	// and the plugin.share permission. Exact pairs, never wildcards - the install
	// dialog has to be able to say "它会读 QQ 插件的 bili-jobs" and mean it.
	//
	// Reading one's OWN values is implicit and needs no entry.
	ShareRead []string `json:"shareRead,omitempty"`
	// Serves lists the METHOD NAMES this plugin answers when another plugin asks it
	// something through `plugin.call` (openspec/changes/plugin-call).
	//
	// It is the publishing half of the call primitive, and it is deliberately a list of
	// bare names rather than "<id>/<method>" pairs: the id is the manifest's own, so a
	// pair here would be a declaration that could disagree with itself.
	//
	// Declaring it requires the plugin.call permission. Without that rule a manifest could
	// promise to answer calls it was never given the means to answer, and the install
	// dialog's promise would be enforced by nothing.
	Serves []string `json:"serves,omitempty"`
	// CallTargets lists the other plugins' served methods this plugin may call, as
	// "<targetId>/<method>" pairs.
	//
	// Same authorisation shape as ShareRead, for the same reason: the pair list is what
	// the install dialog can READ OUT LOUD ("它会调用 QQ 插件的 chat"), and an exact pair
	// cannot quietly become a wildcard. Exact pairs, never a bare id, never a prefix.
	CallTargets []string `json:"callTargets,omitempty"`
	// Assets are extra files the package carries beyond the manifest, the entry
	// and the three fixed front-end resources. A plugin that ships audio needs
	// them: the built-in clips are part of what makes the feature work offline,
	// and a package that could only carry HTML/JS/CSS would leave the plugin
	// silent until the user generated everything themselves.
	//
	// Each path is validated with ValidRelPath - the SAME rule the asset route and
	// the installer use - so a package cannot name a file outside itself.
	Assets []string `json:"assets,omitempty"`
	// Sidecar is the optional helper PROGRAM a plugin ships alongside its document
	// (openspec/changes/plugin-process-and-index, step 3).
	//
	// # WHY THE HOST LAUNCHES IT
	//
	// Five plugins had grown the same pattern by hand: ship a PowerShell script, expose
	// `helperPath` + `helperScript` as two ABSOLUTE PATH settings, launch it through
	// process.launch, then poll a file it writes. On 2026-10-31 four of the five had never
	// run at all - the user had not filled the two paths - and the plugins looked
	// installed and broken at the same time. Every one of those steps is something the
	// host can do for all of them, and the child bookkeeping it needs already exists
	// (process.kill / the reaper).
	//
	// The declaration is deliberately small: WHERE the script is, and what to pass it.
	// The host supplies the rest - the interpreter, the plugin's private directory
	// (`PETPC_PLUGIN_DATA`), the plugin id, and the lifetime (started when the plugin is
	// enabled, stopped when it is disabled, uninstalled or updated).
	Sidecar *SidecarSpec `json:"sidecar,omitempty"`
	// Entry is the HTML file that renders both the panel view and the surface
	// view. It is a path relative to the plugin directory.
	Entry string `json:"entry"`
}

// ConfigItem is one setting a plugin declares.
//
// The host validates it, shows it in the market program's editor, stores the
// value in the plugin's private directory, and - for HostWhitelist items -
// consults it when net.fetch decides whether a host is reachable.
type ConfigItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Type is one of text/number/bool/secret/select. It decides which control the
	// editor draws and how the value is stored; it is NOT a coercion promise -
	// a value the plugin cannot parse is the plugin's problem, not the host's.
	Type    string `json:"type"`
	Default string `json:"default,omitempty"`
	// Secret means the value never leaves the plugin: it is encrypted at rest and
	// never returned to any UI in plaintext.
	Secret bool `json:"secret,omitempty"`
	// HostWhitelist marks an item whose CURRENT VALUE supplies additional net.fetch
	// hostnames. Up to maxHostWhitelists items may carry it, and they are MERGED: a
	// plugin that reaches one set of hosts for its API and another for media needs two
	// fields, and one combined text box would be a field the user cannot fill correctly
	// (a URL in a host list, or a host in a URL field).
	HostWhitelist bool `json:"hostWhitelist,omitempty"`
	// Options are the choices of a `select` item.
	//
	// WHY THIS TYPE EXISTS: a plugin that takes an id ("rid 36 is 知识") was asking the
	// user to memorise a table. The declaration can name the choices, so the editor can
	// list them, and the author stops shipping a legend in the label.
	Options []ConfigOption `json:"options,omitempty"`
	// Multiple turns a `select` into a checkbox group whose value is the chosen
	// values joined by commas - the shape plugins already parse with a split.
	Multiple bool `json:"multiple,omitempty"`
}

// ConfigOption is one choice of a select item: the value stored, the label shown.
type ConfigOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ConfigValueTypes is the closed set of control types a config item may declare.
var ConfigValueTypes = []string{"text", "number", "bool", "secret", "select"}

// maxConfigOptions bounds one select's choices. A picker longer than this is a
// search box, not a settings item.
const maxConfigOptions = 64

// PanelSlotChat is the one panel slot a manifest may claim today: the pet's
// 对话 tab.
//
// It is exported because the HOST has to name it too - the slot resolution in
// app_chat_slot.go compares against it - and two string literals for one
// agreement is exactly how a slot silently stops matching its declaration.
// Adding a slot means adding it here, to validatePanelSlot's switch and to the
// host's resolver; nothing else in the repo spells these names out.
const PanelSlotChat = "chat"

// PanelSlotTodos is the 日程 slot: whoever claims it is what the pet's menu opens.
//
// A SLOT is a named mount point, not necessarily a panel tab. `chat` fills the panel's 对话 tab;
// `todos` fills the column beside the pet with the claimant's own FLOATING SURFACE, because the
// panel is on its way out (see openspec/changes/todo-pomodoro-as-plugins). The host resolves the
// name and opens whatever surface the claimant declared - it never learns which plugin that is.
const PanelSlotTodos = "todos"

// PanelSlotPomodoro is the 番茄钟 slot: whoever claims it is what the pet's menu opens there.
//
// Same shape as PanelSlotTodos - a name the host resolves, filled by the claimant's own surface.
const PanelSlotPomodoro = "pomodoro"

// Rect is one declared plugin surface rectangle, in CSS pixels relative to the
// pet's top-left corner.
type Rect struct {
	DX     float64 `json:"dx"`
	DY     float64 `json:"dy"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	// MaxH is how tall this block may GROW; 0 means "cannot grow".
	//
	// A surface block is sized by the manifest, because the window region is cropped to the
	// declared rectangles when the surface opens - there is no CSS `height: auto` for a window
	// region. A block whose content is a variable-length reply therefore needs a declared
	// CEILING it may request up to (`surface.blocks` carrying `sizes`), and the ceiling is the
	// only thing that stops a plugin asking for a window that covers half the screen. Blocks
	// that leave this at 0 keep exactly the geometry they declare - which is every plugin in
	// the market today except the chat one.
	MaxH   float64 `json:"maxH,omitempty"`
	Radius float64 `json:"radius"`
	// Anchor says which edge of the PET `dy` is measured from, because the pet's size is the
	// user's (`petScale`): "top" (or empty) means the pet's top-left corner, "bottom" means
	// `dy` grows DOWNWARD from the pet's feet.
	//
	// It exists because a block UNDER the pet cannot be expressed as an offset from the pet's
	// top: `dy: 204` was right for the default 200-tall pet and landed on the pet's legs at
	// 1.5x, where the pet is 300 tall. Blocks above the pet do not need it - their offsets are
	// from the top edge, which does not move.
	Anchor string `json:"anchor,omitempty"`
	// Ellipse makes the cropped region a true ellipse instead of a rounded
	// rectangle, so the region matches the border-radius: 50% the page draws.
	//
	// The official conversation plugin no longer uses it - its two floating
	// blocks are the rounded card above the pet and the rounded input bar below
	// it - but the capability stays: an ellipse is the one shape a
	// rounded-rectangle region cannot approximate, and a plugin that wants a
	// round badge or a speech puff needs it.
	Ellipse bool `json:"ellipse,omitempty"`
}

// Manifest file name, used by both the loader and the installer.
const ManifestName = "manifest.json"

// The two values `Rect.Anchor` accepts. `""` behaves like top.
const (
	RectAnchorTop    = "top"
	RectAnchorBottom = "bottom"
)

// Validation limits. They are constants rather than inline literals because the
// packaging script and the docs quote the same numbers.
const (
	maxIDLen          = 96
	maxNameLen        = 64
	maxDescriptionLen = 512
	// maxSurfaceRects bounds how many blocks one plugin may declare. 8 is
	// generous for what a surface actually is - a few cards beside the pet - and
	// the official conversation plugin uses 2 of them (a reply card above the pet
	// and an input bar below it), so the limit is nowhere near a shipping
	// plugin's needs while still refusing a manifest that turns the window
	// region into an unbounded union.
	maxSurfaceRects = 8
	maxRectEdge     = 2000
	maxRectRadius   = 200
	maxRectOffset   = 4000
	// maxConfigItems bounds the declared settings. It exists so a manifest cannot
	// turn the editor into an unbounded form, NOT to ration settings: the market
	// draws the list in a scrolling pane, so the real cost of a large manifest is
	// noise, not breakage. 16 was the cap while the official conversation plugin
	// needed 14 and the QQ plugin 16; adding the vision-model settings is a
	// legitimate 19, and merging two unrelated knobs to stay under a round number
	// would be worse for the user than raising it. 32 kept the original intent
	// (a form, not a config language) with room for the next feature.
	//
	// Raised to 40 when the QQ plugin grew memory/energy/meme settings (31 -> 35):
	// same reasoning as before - the alternative was hard-coding four knobs the
	// user's own group bot needs to tune (how much it remembers, how chatty it is,
	// how many sticker candidates the model sees, whether it collects pictures).
	maxConfigItems = 40
	// maxHostWhitelists caps how many items may widen the net.fetch allow-list. See the
	// HostWhitelist field comment: the runtime merges them, so this is a shape limit, not a
	// security one.
	maxHostWhitelists = 4
	maxConfigLabel = 64
	// maxAssets bounds the extra files a package may carry. The voice plugin is
	// the motivating case: five built-in clips. 32 leaves room for a plugin that
	// ships a whole set per language without letting a manifest turn the package
	// into an arbitrary file dump.
	maxAssets = 32
)

// configKeyPattern is the only shape a config key may take.
//
// It is the SAME shape the storage capability's keys use (StorageKeyPath in
// privatestore.go, which is the path-aware validator that replaced app_plugins.go's
// storageKeyPattern when the private store moved here): a key becomes part of a file
// name in the plugin's private directory, so a separator or ".." would be a way out
// of it, and there is no legitimate reason for a key that needs one.
var configKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Field patterns. Each is anchored, because a partial match on an id or a
// version is a different (and wrong) value.
var (
	idPattern      = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9][a-z0-9-]*)+$`)
	versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)
	netHostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+(:\d+)?$`)
)

// ParseManifest validates raw manifest.json bytes and returns the declaration.
//
// It is a pure function with no I/O so the whole validation table can be driven
// from a test. Every failure names the offending field, because the message is
// shown to whoever packaged the plugin.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("%s 不是合法的 JSON：%w", ManifestName, err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// LoadManifest reads a plugin directory's manifest.json and validates it.
func LoadManifest(dir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return Manifest{}, fmt.Errorf("读取 %s 失败：%w", ManifestName, err)
	}
	return ParseManifest(data)
}

// Validate checks the declaration against every rule in the plugin package
// format. It is exported so the packaging side and the installer can share one
// definition of "a package the host will accept".
func (m Manifest) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("%s 缺少必填字段 id", ManifestName)
	}
	if len(m.ID) > maxIDLen {
		return fmt.Errorf("%s 字段 id 长度超过 %d", ManifestName, maxIDLen)
	}
	if !idPattern.MatchString(m.ID) {
		return fmt.Errorf("%s 字段 id 非法（应为反向域名式的小写标识，如 com.example.plugin）：%q", ManifestName, m.ID)
	}
	if m.Name == "" {
		return fmt.Errorf("%s 缺少必填字段 name", ManifestName)
	}
	if len([]rune(m.Name)) > maxNameLen {
		return fmt.Errorf("%s 字段 name 长度超过 %d", ManifestName, maxNameLen)
	}
	if m.Author == "" {
		return fmt.Errorf("%s 缺少必填字段 author", ManifestName)
	}
	if m.Description == "" {
		return fmt.Errorf("%s 缺少必填字段 description", ManifestName)
	}
	if len([]rune(m.Description)) > maxDescriptionLen {
		return fmt.Errorf("%s 字段 description 长度超过 %d", ManifestName, maxDescriptionLen)
	}
	if m.Version == "" {
		return fmt.Errorf("%s 缺少必填字段 version", ManifestName)
	}
	if !versionPattern.MatchString(m.Version) {
		return fmt.Errorf("%s 字段 version 非法（应为语义化版本，如 1.0.0）：%q", ManifestName, m.Version)
	}
	if err := m.validatePermissions(); err != nil {
		return err
	}
	if err := m.validateNetHosts(); err != nil {
		return err
	}
	if err := m.validateCredentials(); err != nil {
		return err
	}
	if err := m.validateShareRead(); err != nil {
		return err
	}
	if err := m.validateCalls(); err != nil {
		return err
	}
	if err := m.validateEntry(); err != nil {
		return err
	}
	if err := m.validatePanelSlot(); err != nil {
		return err
	}
	if err := m.validateConfig(); err != nil {
		return err
	}
	if err := m.validateBackground(); err != nil {
		return err
	}
	if err := m.validateShareLLM(); err != nil {
		return err
	}
	if err := m.validateAssets(); err != nil {
		return err
	}
	if err := m.validateSidecar(); err != nil {
		return err
	}
	return m.validateSurface()
}

// SidecarSpec names the helper program a plugin ships.
type SidecarSpec struct {
	// Script is a path RELATIVE to the plugin's own directory, ending in .ps1 or .exe.
	//
	// Relative rather than absolute for the same reason process.launch insists on
	// absolute: the meaning must not depend on where the pet was started from. It is the
	// package's own file, so the installer's ValidRelPath rule is the right one - a
	// manifest cannot name a file outside itself.
	Script string `json:"script"`
	// Args are passed after the script. Small and bounded: this is a helper, not a
	// command line the manifest gets to compose.
	Args []string `json:"args,omitempty"`
}

// The bounds on sidecar.args. Same shape as process.launch's, one order smaller: a
// helper script takes a directory and a flag or two, and a manifest that needs 32
// arguments is describing something else.
const (
	maxSidecarArgs    = 8
	maxSidecarArgLen  = 256
	maxSidecarArgsSum = 1024
)

// validateSidecar keeps the declaration honest.
//
// Four rules, each of which was a real failure mode before it existed:
//
//  1. the script must be a path inside the package (ValidRelPath), .ps1 or .exe;
//  2. it must ALSO be listed in `assets` - the packagers ship exactly manifest +
//     entry + the three fixed resources + assets, so a script that is not in assets
//     would not be in the package and the host would fail to start a file that exists
//     only in the repository;
//  3. the manifest must request `process`: a sidecar IS "start a program on this
//     machine", and the install dialog has to be able to say so. Launching it without
//     that consent would make the permission meaningless;
//  4. it must declare `background`, because the resident frame is the half that talks
//     to the helper - a sidecar with no reader is a process nobody asked for.
func (m Manifest) validateSidecar() error {
	if m.Sidecar == nil {
		return nil
	}
	script := strings.TrimSpace(m.Sidecar.Script)
	if script == "" {
		return errors.New("manifest 的 sidecar.script 不能为空")
	}
	if !ValidRelPath(script) {
		return fmt.Errorf("manifest 的 sidecar.script 必须是插件目录内的相对路径：%q", m.Sidecar.Script)
	}
	switch strings.ToLower(filepath.Ext(script)) {
	case ".ps1", ".exe":
	default:
		return fmt.Errorf("manifest 的 sidecar.script 只支持 .ps1 或 .exe：%q", m.Sidecar.Script)
	}
	declared := false
	for _, asset := range m.Assets {
		if asset == script {
			declared = true
			break
		}
	}
	if !declared {
		return fmt.Errorf("manifest 的 sidecar.script 必须同时写进 assets（否则不会打进包里）：%q", script)
	}
	hasProcess := false
	for _, perm := range m.Permissions {
		if perm == PermProcess {
			hasProcess = true
			break
		}
	}
	if !hasProcess {
		return fmt.Errorf("manifest 声明了 sidecar 就必须申请 %s（它要启动一个本机程序）", PermProcess)
	}
	if !m.Background {
		return errors.New("manifest 声明了 sidecar 就必须同时声明 background：常驻帧才是读它输出的那一半")
	}
	if len(m.Sidecar.Args) > maxSidecarArgs {
		return fmt.Errorf("manifest 的 sidecar.args 最多 %d 个，收到 %d 个", maxSidecarArgs, len(m.Sidecar.Args))
	}
	total := 0
	for i, arg := range m.Sidecar.Args {
		if strings.ContainsAny(arg, "\r\n") {
			return fmt.Errorf("manifest 的 sidecar.args 第 %d 个含换行", i+1)
		}
		runes := len([]rune(arg))
		if runes > maxSidecarArgLen {
			return fmt.Errorf("manifest 的 sidecar.args 第 %d 个超过 %d 字符", i+1, maxSidecarArgLen)
		}
		total += runes
	}
	if total > maxSidecarArgsSum {
		return fmt.Errorf("manifest 的 sidecar.args 总长度 %d 超过 %d 字符", total, maxSidecarArgsSum)
	}
	return nil
}

// validateBackground pairs the headless-frame request with the permission that
// makes it observable.
//
// A background frame is what lets a plugin act while the user has no interface
// open, and the host mounts one only for a plugin that both DECLARES the field
// and HOLDS a permission that says what the frame is FOR. Requiring one here is
// what keeps the pairing honest: without it, a manifest could ask for a
// permanently running frame while the install dialog showed a permission list
// that did not mention background operation at all - the user would be
// authorising something they were never shown.
//
// There are FOUR acceptable permissions because there are four ways to be useful
// with nobody watching, and they are different in kind:
//
//   - `events` - the host tells the plugin about the pomodoro, the todos and the
//     reminders. That is what the voice plugin needs.
//   - `net.ws` - the plugin hears from a service it holds a connection to (a
//     chat protocol side pushing group messages). `events` would be the wrong
//     thing to demand here: subscribing to host events is not what such a plugin
//     does, and asking the user to grant a switch the plugin never uses is the
//     "switch that does nothing" this list exists to avoid.
//   - `net.fetch` - a POLLING plugin: it has nothing to be pushed and knows when
//     to ask (the Bilibili plugin's schedule). Demanding `events` would force it
//     to subscribe to the pomodoro it never reads - the same useless switch - and
//     `net.ws` would be a lie about the transport. What such a plugin needs from
//     the host is not a channel, it is the permission to make a request at a time
//     of its own choosing, which is exactly what net.fetch already is.
//   - `plugin.call` - a plugin that is ASKED (openspec/changes/plugin-call): its
//     inbound traffic is another plugin's `call`, and it must be resident or the
//     caller finds no frame and fails. This is the case the list did not have
//     until the memory core existed: a persona-and-memory plugin has no host
//     events to hear, no socket of its own and no polling schedule, so any of the
//     other three would have been a switch the user is asked about and the plugin
//     never uses - which is the thing this whole vocabulary refuses.
//
// Adding the third one does NOT loosen the rule: a headless frame still requires
// a permission the user granted in the install dialog, and disabling or
// uninstalling the plugin still tears the frame down.
//
// There is deliberately NO "background needs a panel or surface too" rule. That
// was the first version of this function, and it was wrong: a plugin whose
// interface is hosted by ANOTHER program (the plugin market draws the voice
// plugin's workshop) legitimately has no mount point in the pet at all, while
// still needing the frame to hear the timer. The requirement is that the plugin
// appears SOMEWHERE - which validateEntry already enforces across all three
// kinds of mount point.
func (m Manifest) validateBackground() error {
	if !m.Background {
		return nil
	}
	if !m.HasPermission(PermEvents) && !m.HasPermission(PermNetWS) &&
		!m.HasPermission(PermNetFetch) && !m.HasPermission(PermPluginCall) {
		return fmt.Errorf("%s 声明了 background 却既未申请 events、net.ws、net.fetch，也未申请 plugin.call 权限：后台帧的用途就是无人看着时收发消息",
			ManifestName)
	}
	return nil
}

// validateCredentials keeps net.credentials attached to the capability it gates.
//
// net.credentials is the SECOND gate of net.fetch, not a method of its own: the
// payload names one of the plugin's declared secrets and the host fills the
// Cookie header from encrypted storage. Declared alone it would be a switch in
// the install dialog that unlocks nothing - the exact "switch the user is asked
// about that does nothing" the vocabulary invariant forbids - and it would also
// be untrue: there is no way to "be logged in" here except by making a request.
func (m Manifest) validateCredentials() error {
	if !m.HasPermission(PermNetCredentials) || m.HasPermission(PermNetFetch) {
		return nil
	}
	return fmt.Errorf("%s 声明了 %s 却未声明 %s：前者只是后者携带凭据的第二道开关",
		ManifestName, PermNetCredentials, PermNetFetch)
}

// validateShareRead checks the read half of the cross-plugin channel.
//
// Three rules, each of which would otherwise be a way for the declaration to lie:
//
//  1. every entry is "<ownerId>/<name>" and both halves pass the same validators the
//     ids and storage keys already pass - a ".." or a separator here would be a path;
//  2. the plugin must hold plugin.share, or the list is a promise nothing enforces;
//  3. it must not name ITSELF - reading your own values is implicit, and letting a
//     manifest list itself would make the dialog say "reads itself", which is noise.
func (m Manifest) validateShareRead() error {
	if len(m.ShareRead) == 0 {
		return nil
	}
	if !m.HasPermission(PermPluginShare) {
		return fmt.Errorf("%s 声明了 shareRead 却未申请 %s 权限：读别人的共享值要有这把开关",
			ManifestName, PermPluginShare)
	}
	if len(m.ShareRead) > maxShareRead {
		return fmt.Errorf("%s 的 shareRead 最多 %d 项，收到 %d", ManifestName, maxShareRead, len(m.ShareRead))
	}
	seen := make(map[string]bool, len(m.ShareRead))
	for _, entry := range m.ShareRead {
		owner, _, ok := SplitSharedKey(entry)
		if !ok {
			return fmt.Errorf("%s 的 shareRead 项 %q 不是 \"<插件id>/<名字>\"", ManifestName, entry)
		}
		if owner == m.ID {
			return fmt.Errorf("%s 的 shareRead 不该列出自己（读自己的值是默认允许的）", ManifestName)
		}
		if seen[entry] {
			return fmt.Errorf("%s 的 shareRead 有重复项 %q", ManifestName, entry)
		}
		seen[entry] = true
	}
	return nil
}

// maxShareRead bounds the read declarations. Eight is enough for a bridge plugin and
// small enough that the install dialog still reads like a list rather than a policy.
const maxShareRead = 8

// SharedKey renders the canonical "<owner>/<name>" form.
func SharedKey(owner, name string) string { return owner + "/" + name }

// DeclaredShareRead reports whether the manifest listed owner/name in shareRead.
//
// Exact match only. A prefix or suffix rule here would be the classic way an allow-list
// quietly becomes a wildcard - and this one guards data crossing between plugins.
func DeclaredShareRead(declared []string, owner, name string) bool {
	want := SharedKey(owner, name)
	for _, entry := range declared {
		if strings.TrimSpace(entry) == want {
			return true
		}
	}
	return false
}

// SplitSharedKey parses "<owner>/<name>", validating BOTH halves.
//
// The validation is the point: the name becomes a file name inside the owner's private
// directory and the owner becomes a directory name, so a ".." or an extra separator here
// would be a path out of it. Same rules the plugin ids and storage keys already follow.
func SplitSharedKey(entry string) (owner, name string, ok bool) {
	parts := strings.Split(strings.TrimSpace(entry), "/")
	if len(parts) != 2 {
		return "", "", false
	}
	owner = strings.TrimSpace(parts[0])
	name = strings.TrimSpace(parts[1])
	if !IsValidID(owner) || !configKeyPattern.MatchString(name) {
		return "", "", false
	}
	return owner, name, true
}

// validateCalls checks both halves of the plugin-to-plugin call declarations.
//
// The rules mirror validateShareRead's, because they guard the same kind of thing: a
// promise made in a manifest that the host must be able to hold the plugin to.
//
//  1. either list requires plugin.call, or it is a promise nothing backs;
//  2. every callTargets entry is a "<targetId>/<method>" pair whose halves pass the same
//     validators ids and method names already pass - a separator or a ".." here would be
//     a name that means something else;
//  3. neither list may name the plugin ITSELF. A self-call is not a call, it is a loop
//     with a hop in it;
//  4. no duplicates, and no list longer than an install dialog can read out.
//
// What is deliberately NOT checked here: whether the target plugin is installed, and
// whether it really serves that method. Neither is knowable at packaging time - the
// target may be installed later, or removed - so both are answered at CALL time, where
// "目标没有声明会回答 X" names the real situation instead of an install-time guess.
func (m Manifest) validateCalls() error {
	if len(m.Serves) == 0 && len(m.CallTargets) == 0 {
		return nil
	}
	if !m.HasPermission(PermPluginCall) {
		return fmt.Errorf("%s 声明了 serves / callTargets 却未申请 %s 权限：插件间调用要有这把开关",
			ManifestName, PermPluginCall)
	}
	if len(m.Serves) > maxCallList {
		return fmt.Errorf("%s 的 serves 最多 %d 项，收到 %d", ManifestName, maxCallList, len(m.Serves))
	}
	served := make(map[string]bool, len(m.Serves))
	for _, name := range m.Serves {
		method := strings.TrimSpace(name)
		if !servedMethodPattern.MatchString(method) {
			return fmt.Errorf("%s 的 serves 项 %q 不是合法的方法名（%s）",
				ManifestName, name, servedMethodPattern.String())
		}
		if served[method] {
			return fmt.Errorf("%s 的 serves 有重复项 %q", ManifestName, method)
		}
		served[method] = true
	}
	if len(m.CallTargets) > maxCallList {
		return fmt.Errorf("%s 的 callTargets 最多 %d 项，收到 %d", ManifestName, maxCallList, len(m.CallTargets))
	}
	seen := make(map[string]bool, len(m.CallTargets))
	for _, entry := range m.CallTargets {
		target, method, ok := SplitCallTarget(entry)
		if !ok {
			return fmt.Errorf("%s 的 callTargets 项 %q 不是 \"<插件id>/<方法名>\"（版本下限写成 \"<id>/<方法名>@>=1.2.3\"）",
				ManifestName, entry)
		}
		if target == m.ID {
			return fmt.Errorf("%s 的 callTargets 不该列出自己（那是循环，不是调用）", ManifestName)
		}
		key := CallTargetKey(target, method)
		if seen[key] {
			return fmt.Errorf("%s 的 callTargets 有重复项 %q", ManifestName, key)
		}
		seen[key] = true
	}
	return nil
}

// maxCallList bounds each half of the call declarations. Same reasoning as maxShareRead:
// eight still reads like a list of questions a plugin asks, and a longer one is a policy
// the dialog cannot honestly summarise.
const maxCallList = 8

// servedMethodPattern is the shape of one name in `serves`.
//
// Dots are allowed (unlike configKeyPattern) because a served method is a NAME the plugin
// author invents for its own protocol - "chat", "memory.get" - and forbidding the dot
// would push authors towards underscores for no benefit. It is not a path: the name never
// reaches the filesystem.
var servedMethodPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// CallTargetKey renders the canonical "<targetId>/<method>" form.
func CallTargetKey(target, method string) string { return target + "/" + method }

// callFloorPattern is the optional version floor a callTargets entry may carry:
// "<id>/<method>@>=1.2.3".
//
// # WHY A FLOOR AND NOT A SHAPE
//
// 2026-10-31: the dashboard plugin called `todo/stats` for a week against a todo plugin that
// had never declared `stats`. The host's refusal was precise ("目标插件 … 没有声明会回答
// stats") and the caller caught it - into a log line nobody read, once every 30 seconds.
// What the caller needed was a way to say WHICH todo plugin it can work with, and a way to
// find out before shipping.
//
// A floor is the smallest honest form of that: the target's version is already in its
// manifest, versions are already required to be x.y.z, and "this method answers the shape I
// expect from 1.2.3 on" is a claim an author can actually keep. A declared RETURN SHAPE
// (a JSON schema per method) was the alternative and it is a much larger promise: the host
// would have to validate every answer, and a plugin that returned an extra field would
// break.
//
// The floor is checked when the call is made, not at install: the target may not be
// installed yet, and a plugin must be installable before its dependencies are.
var callFloorPattern = regexp.MustCompile(`^@>=(\d+)\.(\d+)\.(\d+)$`)

// SplitCallTargetVersion parses "<targetId>/<method>" with an optional "@>=x.y.z" floor.
//
// floor is "" when the entry carries none, which is the common case and stays allowed:
// most callers do not care about a version, and forcing them to write one would be noise
// that trains authors to paste a number they never checked.
func SplitCallTargetVersion(entry string) (target, method, floor string, ok bool) {
	raw := strings.TrimSpace(entry)
	if at := strings.IndexByte(raw, '@'); at >= 0 {
		m := callFloorPattern.FindStringSubmatch(raw[at:])
		if m == nil {
			return "", "", "", false
		}
		floor = m[1] + "." + m[2] + "." + m[3]
		raw = raw[:at]
	}
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return "", "", "", false
	}
	target = strings.TrimSpace(parts[0])
	method = strings.TrimSpace(parts[1])
	if !IsValidID(target) || !servedMethodPattern.MatchString(method) {
		return "", "", "", false
	}
	return target, method, floor, true
}

// SplitCallTarget parses "<targetId>/<method>", validating BOTH halves. A version floor, if
// present, is parsed and DROPPED - callers that only care about the pair (validation, the
// install dialog's summary) use this one.
func SplitCallTarget(entry string) (target, method string, ok bool) {
	target, method, _, ok = SplitCallTargetVersion(entry)
	return target, method, ok
}

// DeclaredCall reports whether the manifest listed target/method in callTargets.
//
// Exact match on the PAIR, for the reason DeclaredShareRead gives: a prefix rule here is how
// an allow-list quietly becomes "everything another plugin does". A version floor on the
// entry does not change which pair it authorises.
func DeclaredCall(declared []string, target, method string) bool {
	_, ok := DeclaredCallFloor(declared, target, method)
	return ok
}

// DeclaredCallFloor returns the version floor declared for target/method, "" when the entry
// carries none, and ok=false when the pair is not declared at all.
func DeclaredCallFloor(declared []string, target, method string) (floor string, ok bool) {
	for _, entry := range declared {
		t, m, f, valid := SplitCallTargetVersion(entry)
		if !valid || t != target || m != method {
			continue
		}
		return f, true
	}
	return "", false
}

// The comparison itself is CompareVersions in version.go: one implementation, because the
// market program already asks the same question ("is the copy on disk behind?") and two
// implementations of "which version is newer" is how a floor and an update check end up
// disagreeing.

// ServesMethod reports whether the manifest promised to answer method.
func ServesMethod(serves []string, method string) bool {
	for _, name := range serves {
		if strings.TrimSpace(name) == method {
			return true
		}
	}
	return false
}

// validateShareLLM keeps the lending flag attached to a capability that can
// actually consume it.
//
// A plugin that declares `shareLLM` but not `llm.chat` would be advertising a
// configured model that no code path ever reads: the host resolves a lender by
// looking for exactly that pair, so the flag alone is a half-written declaration
// that would silently make a borrower report "no model configured" while the
// settings screen showed a filled-in endpoint.
func (m Manifest) validateShareLLM() error {
	if !m.ShareLLM {
		return nil
	}
	if !m.HasPermission(PermLLMChat) {
		return fmt.Errorf("%s 声明了 shareLLM 却未申请 llm.chat 权限：借出的是它自己调模型的那套配置",
			ManifestName)
	}
	return nil
}

// validateAssets bounds the extra files a package may carry.
//
// Each entry is checked with ValidRelPath - the same rule the asset route and
// the installer apply - because the packager joins these names onto the plugin
// directory when it builds the zip. A ".." segment or an absolute path here
// would make the packaging script read a file outside the plugin, i.e. the
// manifest could exfiltrate anything the packager can reach into a published
// package.
func (m Manifest) validateAssets() error {
	if len(m.Assets) > maxAssets {
		return fmt.Errorf("%s 字段 assets 最多 %d 项，收到 %d", ManifestName, maxAssets, len(m.Assets))
	}
	seen := make(map[string]bool, len(m.Assets))
	for i, a := range m.Assets {
		if !ValidRelPath(a) {
			return fmt.Errorf("%s 字段 assets 第 %d 项不是合法的相对路径：%q", ManifestName, i, a)
		}
		if seen[a] {
			return fmt.Errorf("%s 字段 assets 含重复路径 %q", ManifestName, a)
		}
		seen[a] = true
	}
	return nil
}

// validatePanelSlot restricts the slot name and its one pairing rule.
//
// A slot is a place in the pet's own panel, so the set of names is closed: an
// unknown name would be a plugin asking to be mounted somewhere that does not
// exist, and it would silently never be shown.
func (m Manifest) validatePanelSlot() error {
	switch m.PanelSlot {
	case "":
		return nil
	case PanelSlotChat:
		if !m.Panel {
			return fmt.Errorf("%s 字段 panelSlot=chat 要求 panel 为 true", ManifestName)
		}
		return nil
	case PanelSlotTodos, PanelSlotPomodoro:
		// The todo slot is filled by the claimant's own surface, so a manifest that claims it
		// without declaring one would claim a slot it can never fill - the menu would open
		// nothing at all, and nothing would say why.
		if len(m.Surface) == 0 {
			return fmt.Errorf("%s 字段 panelSlot=todos 要求声明 surface 矩形", ManifestName)
		}
		return nil
	default:
		return fmt.Errorf("%s 字段 panelSlot 只接受 %q、%q 或 %q：%q", ManifestName, PanelSlotChat, PanelSlotTodos, PanelSlotPomodoro, m.PanelSlot)
	}
}

// validateConfig checks every declared setting.
//
// The rules are deliberately narrow, because each one is a promise the market
// program's editor and the config store rely on: a key that is a safe file name,
// a type that is one of four controls, and exactly one item allowed to feed the
// net.fetch allow-list.
func (m Manifest) validateConfig() error {
	if len(m.Config) > maxConfigItems {
		return fmt.Errorf("%s 字段 config 最多 %d 项，收到 %d", ManifestName, maxConfigItems, len(m.Config))
	}
	seen := make(map[string]bool, len(m.Config))
	whitelists := 0
	for i, c := range m.Config {
		if !configKeyPattern.MatchString(c.Key) {
			return fmt.Errorf("%s 字段 config 第 %d 项的 key 非法：%q", ManifestName, i, c.Key)
		}
		if seen[c.Key] {
			return fmt.Errorf("%s 字段 config 含重复 key %s", ManifestName, c.Key)
		}
		seen[c.Key] = true
		if strings.TrimSpace(c.Label) == "" {
			return fmt.Errorf("%s 字段 config 的 %s 缺少 label", ManifestName, c.Key)
		}
		if len([]rune(c.Label)) > maxConfigLabel {
			return fmt.Errorf("%s 字段 config 的 %s label 超过 %d 字符", ManifestName, c.Key, maxConfigLabel)
		}
		if !isConfigValueType(c.Type) {
			return fmt.Errorf("%s 字段 config 的 %s type 非法：%q", ManifestName, c.Key, c.Type)
		}
		// Checked BEFORE the secret/type agreement below, so a `select` that also says
		// secret is told "a picker cannot hold a credential" instead of the generic
		// "type 与 secret 必须一致" - the author's mistake is the picker, not the flag.
		if err := validateConfigOptions(c); err != nil {
			return err
		}
		// Two ways of writing "a secret" would be two ways of getting the
		// encryption wrong, so the flag and the type must agree.
		if c.Secret != (c.Type == "secret") {
			return fmt.Errorf("%s 字段 config 的 %s 的 type 与 secret 必须一致（type=secret 时 secret 为 true）", ManifestName, c.Key)
		}
		if c.HostWhitelist {
			whitelists++
		}
	}
	// 多于一个是可以的：ConfigHosts 会把每个带这个标记的项的值都并进白名单，而"接口域名"
	// 与"图片域名"本来就该是两个字段（一个填 URL、一个填域名），挤在一个框里只能填错。
	if whitelists > maxHostWhitelists {
		return fmt.Errorf("%s 字段 config 最多允许 %d 个 hostWhitelist 项，收到 %d 个", ManifestName, maxHostWhitelists, whitelists)
	}
	return nil
}

// validateConfigOptions checks the picker half of a config item.
//
// A `select` without choices would draw an empty dropdown - a control the user cannot
// use - and a choice without a value stores nothing. Both are refused where the author
// can still see them, rather than becoming a blank row in the market.
func validateConfigOptions(c ConfigItem) error {
	if c.Type != "select" {
		if len(c.Options) > 0 || c.Multiple {
			return fmt.Errorf("%s 字段 config 的 %s 用了 options/multiple，但 type 不是 select",
				ManifestName, c.Key)
		}
		return nil
	}
	if c.Secret || c.HostWhitelist {
		// A picker cannot hold a credential, and the net.fetch allow-list is not a
		// choice: both would be a category error the editor cannot render honestly.
		return fmt.Errorf("%s 字段 config 的 %s 是 select，不能同时是 secret 或 hostWhitelist",
			ManifestName, c.Key)
	}
	if len(c.Options) < 2 || len(c.Options) > maxConfigOptions {
		return fmt.Errorf("%s 字段 config 的 %s 有 %d 个选项，select 需要 2–%d 个",
			ManifestName, c.Key, len(c.Options), maxConfigOptions)
	}
	seen := make(map[string]bool, len(c.Options))
	for _, option := range c.Options {
		if strings.TrimSpace(option.Value) == "" {
			return fmt.Errorf("%s 字段 config 的 %s 有选项缺少 value", ManifestName, c.Key)
		}
		if strings.TrimSpace(option.Label) == "" {
			return fmt.Errorf("%s 字段 config 的 %s 的选项 %q 缺少 label", ManifestName, c.Key, option.Value)
		}
		if len([]rune(option.Label)) > maxConfigLabel {
			return fmt.Errorf("%s 字段 config 的 %s 的选项 %q label 超过 %d 字符",
				ManifestName, c.Key, option.Value, maxConfigLabel)
		}
		if seen[option.Value] {
			return fmt.Errorf("%s 字段 config 的 %s 有重复选项 value %q", ManifestName, c.Key, option.Value)
		}
		seen[option.Value] = true
	}
	return nil
}

// isConfigValueType reports whether t names one of the editor controls.
func isConfigValueType(t string) bool {
	for _, v := range ConfigValueTypes {
		if v == t {
			return true
		}
	}
	return false
}

// ConfigItem returns the declaration for key, and whether it was declared.
//
// The store and the encoder both refuse an undeclared key, so this is the one
// lookup that decides what "declared" means.
func (m Manifest) ConfigItem(key string) (ConfigItem, bool) {
	for _, c := range m.Config {
		if c.Key == key {
			return c, true
		}
	}
	return ConfigItem{}, false
}

// validatePermissions rejects unknown names and duplicates.
//
// Duplicates are rejected rather than folded because a permission list is what
// the user is shown and ticks: the same name twice makes the dialog ask for one
// thing twice, and the granted set (a set) would silently report one.
func (m Manifest) validatePermissions() error {
	seen := make(map[string]bool, len(m.Permissions))
	for _, p := range m.Permissions {
		if !IsAllowedPermission(p) {
			return fmt.Errorf("%s 字段 permissions 含非法权限名 %s", ManifestName, p)
		}
		if seen[p] {
			return fmt.Errorf("%s 字段 permissions 含重复项 %s", ManifestName, p)
		}
		seen[p] = true
	}
	return nil
}

// validateNetHosts enforces the net.fetch/netHosts pairing.
func (m Manifest) validateNetHosts() error {
	for _, h := range m.NetHosts {
		if !netHostPattern.MatchString(h) {
			return fmt.Errorf("%s 字段 netHosts 含非法主机名 %q", ManifestName, h)
		}
	}
	if m.HasPermission(PermNetFetch) && len(m.NetHosts) == 0 {
		return fmt.Errorf("%s 声明了 net.fetch 却未给出 netHosts 白名单", ManifestName)
	}
	return nil
}

// validateEntry checks the entry path and that the plugin has somewhere to appear.
//
// The path rules are the first half of the path-traversal defence: the entry is
// joined onto the plugin directory to build an asset URL, so an absolute path
// or a ".." segment would let a plugin point the host at files outside its own
// directory.
//
// "Somewhere to appear" is one of THREE things, not two. `panel` and `surface` are
// the pet's own mount points; `background` is a plugin whose interface is hosted by
// another program (the plugin market) while it keeps a headless frame in the pet so
// it can still receive events. All three mean "some host program will load this
// file and run it", which is the property the rule exists to guarantee - a manifest
// that declares none of them names an entry nothing would ever load.
func (m Manifest) validateEntry() error {
	if m.Entry == "" {
		return fmt.Errorf("%s 缺少必填字段 entry", ManifestName)
	}
	if strings.HasPrefix(m.Entry, "/") {
		return fmt.Errorf("%s 字段 entry 不能是绝对路径：%q", ManifestName, m.Entry)
	}
	if strings.Contains(m.Entry, `\`) {
		return fmt.Errorf("%s 字段 entry 不能包含反斜杠：%q", ManifestName, m.Entry)
	}
	if strings.Contains(m.Entry, "..") {
		return fmt.Errorf("%s 字段 entry 不能包含 .. 路径段：%q", ManifestName, m.Entry)
	}
	if !strings.HasSuffix(m.Entry, ".html") {
		return fmt.Errorf("%s 字段 entry 必须以 .html 结尾：%q", ManifestName, m.Entry)
	}
	if !m.Panel && len(m.Surface) == 0 && !m.Background {
		return fmt.Errorf("%s 必须声明 panel、surface 或 background（插件至少要有一个界面落点）", ManifestName)
	}
	return nil
}

// validateSurface bounds the declared rectangles.
//
// The bounds are wide but finite on purpose: the geometry function clamps them
// into the window anyway, but a manifest claiming a 10-metre rectangle is a
// packaging bug, and refusing it here says so where the author can see it
// rather than silently drawing a full-window panel.
func (m Manifest) validateSurface() error {
	if len(m.Surface) > maxSurfaceRects {
		return fmt.Errorf("%s 字段 surface 最多 %d 块矩形，收到 %d", ManifestName, maxSurfaceRects, len(m.Surface))
	}
	for i, r := range m.Surface {
		if r.W <= 0 || r.H <= 0 {
			return fmt.Errorf("%s 字段 surface 第 %d 块的宽高必须为正：w=%g h=%g", ManifestName, i, r.W, r.H)
		}
		if r.W > maxRectEdge || r.H > maxRectEdge {
			return fmt.Errorf("%s 字段 surface 第 %d 块的宽高上限为 %d：w=%g h=%g", ManifestName, i, maxRectEdge, r.W, r.H)
		}
		if r.Radius < 0 || r.Radius > maxRectRadius {
			return fmt.Errorf("%s 字段 surface 第 %d 块的 radius 需在 0..%d：%g", ManifestName, i, maxRectRadius, r.Radius)
		}
		if r.DX < -maxRectOffset || r.DX > maxRectOffset || r.DY < -maxRectOffset || r.DY > maxRectOffset {
			return fmt.Errorf("%s 字段 surface 第 %d 块的坐标偏移上限为 %d：dx=%g dy=%g", ManifestName, i, maxRectOffset, r.DX, r.DY)
		}
		// A typo here would silently anchor the block to the wrong edge of the pet, and the
		// symptom (a composer sitting on the pet's legs) looks like a bug in the host.
		switch r.Anchor {
		case "", RectAnchorTop, RectAnchorBottom:
		default:
			return fmt.Errorf("%s 字段 surface 第 %d 块的 anchor 只能是 %q 或 %q：%q",
				ManifestName, i, RectAnchorTop, RectAnchorBottom, r.Anchor)
		}
	}
	return nil
}

// HasPermission reports whether the declaration includes p.
func (m Manifest) HasPermission(p string) bool {
	for _, got := range m.Permissions {
		if got == p {
			return true
		}
	}
	return false
}

// IsValidID reports whether id is a well-formed plugin id.
//
// It exists separately from Validate because the manager joins the id onto the
// data directory to build filesystem paths, and every one of those joins has to
// be preceded by this check: a valid id contains no separator, so Join cannot
// escape the prefix.
func IsValidID(id string) bool {
	return len(id) > 0 && len(id) <= maxIDLen && idPattern.MatchString(id)
}

// ValidRelPath reports whether a plugin-relative asset path is safe to join
// onto the plugin's directory.
//
// This is the second half of the traversal defence: it guards the URL form
// ("/plugin-asset/<id>/<relpath>"), where the id is already validated but the
// remaining segments come straight from the request.
func ValidRelPath(rel string) bool {
	if rel == "" {
		return false
	}
	if strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
