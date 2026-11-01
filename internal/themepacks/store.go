package themepacks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

/*
 * 主题包：内建的那几个（下面的 Builtin）+ 装在 dataDir/themes/<id>/theme.json 里的那些。
 *
 * 这一层被两个程序共用（桌宠注入插件文档、控制中心列出可选包），所以它不依赖任何一个程序，
 * 也不依赖 internal/plugins —— 依赖方向是 plugins → themepacks，反过来会让主题包拖进整个
 * 插件管理器。
 *
 * 三条规则：
 *  1. 名字与值都过白名单/形状校验。值会被拼进 `<style>`，那是宿主自己文档里的 CSS；
 *     "是不是颜色"在这里不是格式问题，而是"一个主题包有没有能力往宿主界面里塞东西"。
 *  2. 两套（light/dark）都要有，而且要带齐骨架令牌。只写浅色的包切到深色就是浅底浅字。
 *  3. 加载失败**不影响程序启动**：坏掉的包被跳过并带上原因（界面上要说出来），
 *     而不是让一个主题包把整个设置页弄成空白。
 */

// Scheme tokens every pack must provide, in both schemes.
//
// The full vocabulary is much larger (`TokenNames`); this is the minimum that decides whether a
// window is readable at all. A pack that sets only `--accent` is not a theme, it is a tint.
var requiredTokens = []string{"bg", "bg-raised", "bg-sunken", "text", "text-dim", "accent", "accent-strong", "accent-soft"}

// Entry is one pack, built-in or installed.
type Entry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Author  string `json:"author"`
	Version string `json:"version"`
	// Builtin packs are compiled in; they cannot be removed and they need no file on disk.
	Builtin bool `json:"builtin"`
	// Problems are readability failures measured on this pack's own tokens. A pack with problems is
	// still usable - the user chose it - but the card says so instead of pretending it is fine.
	Problems []string `json:"problems,omitempty"`
	// Tokens is the palette itself, per scheme. The control center draws a pack's preview card from
	// these values, so it does not have to keep a second copy of anyone's colours.
	Tokens map[string]map[string]string `json:"tokens,omitempty"`
	// Error is set when the file could not be used at all; such an entry is listed so the user can
	// see WHY their pack is not in effect, and it is never applied.
	Error string `json:"error,omitempty"`
}

// manifest is the on-disk shape: themes/<id>/theme.json.
//
// `tokens` is per scheme rather than flat, because a pack that only writes one scheme cannot be
// applied to the other one - the two are not tints of each other, they are different values.
type manifest struct {
	ID      string                       `json:"id"`
	Name    string                       `json:"name"`
	Author  string                       `json:"author"`
	Version string                       `json:"version"`
	Tokens  map[string]map[string]string `json:"tokens"`
}

// ValidID reports whether an id may be used as a pack id, i.e. as a directory name.
//
// Same character class as store.NormalizePack, and for the same reason: the value arrives from a
// file (and, later, from a download), and the next step in the plan is reading packs from
// `themes/<id>/`. An id that could escape that directory is the one bug this function exists for.
func ValidID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// Set is the loaded catalogue: built-ins plus whatever the themes directory currently holds.
//
// It is cached against the directory's modification time and reloaded when that changes: a settings
// page that stat'ed every file on every render would be a settings page that reads the disk in a
// render loop, and one that never reloaded would not see a pack the user just installed.
type Set struct {
	dir string

	mu      sync.Mutex
	loaded  time.Time
	entries map[string]Entry
	tokens  map[string]map[string]map[string]string // id -> scheme -> token -> value
	err     string
}

// NewSet returns a Set reading `themes/` under dataDir. A missing directory is not an error: it is
// what every install looks like before the first pack is installed.
func NewSet(dataDir string) *Set {
	return &Set{dir: filepath.Join(dataDir, "themes")}
}

// BuiltinEntries are the compiled-in packs, always present, never removable.
func BuiltinEntries() []Entry {
	ids := make([]string, 0, len(Builtin))
	for id := range Builtin {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Entry, 0, len(ids))
	for _, id := range ids {
		out = append(out, Entry{
			ID: id, Name: builtinName(id), Builtin: true,
			Tokens:   map[string]map[string]string{"light": Builtin[id].Light, "dark": Builtin[id].Dark},
			Problems: audit(Builtin[id]),
		})
	}
	return out
}

// builtinName is the display name; the ids are stable identifiers, not labels.
func builtinName(id string) string {
	switch id {
	case "sakura":
		return "樱花粉"
	case "forest":
		return "森野绿"
	case "graphite":
		return "极简石墨"
	}
	return id
}

// Entries returns every pack the user may pick, built-ins first, then the installed ones.
func (s *Set) Entries() []Entry {
	s.reload()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := BuiltinEntries()
	ids := make([]string, 0, len(s.entries))
	for id := range s.entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, s.entries[id])
	}
	return out
}

// Problem reports why the themes directory could not be read, if it could not.
func (s *Set) Problem() string {
	s.reload()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// CSS answers with the `<style>` element for a pack in a scheme, or "" when nothing is injected.
//
// Built-in first: a user pack may not shadow a compiled-in id. That is not politeness - "violet"
// means "the stylesheet's default", and a directory named violet that silently replaced it would
// make the default pack unremovable and unaccountable.
func (s *Set) CSS(scheme, pack string) string {
	if pack == "" {
		return ""
	}
	if _, ok := Builtin[pack]; ok {
		return StyleTag(scheme, pack)
	}
	s.reload()
	s.mu.Lock()
	tokens, ok := s.tokens[pack]
	s.mu.Unlock()
	if !ok {
		return ""
	}
	return styleTagFrom(tokens[scheme])
}

// reload re-reads the directory when its modification time moved.
//
// A directory whose mtime cannot be read (it does not exist yet) counts as "nothing changed": the
// next successful write creates it and moves the mtime, so the reload happens then.
func (s *Set) reload() {
	info, err := os.Stat(s.dir)
	if err != nil {
		s.mu.Lock()
		if s.entries == nil {
			s.entries = map[string]Entry{}
			s.tokens = map[string]map[string]map[string]string{}
		}
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	fresh := s.entries != nil && info.ModTime().Equal(s.loaded)
	s.mu.Unlock()
	if fresh {
		return
	}

	entries := map[string]Entry{}
	tokens := map[string]map[string]map[string]string{}
	problem := ""
	dirs, err := os.ReadDir(s.dir)
	if err != nil {
		problem = "读不到主题包目录：" + err.Error()
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		id := d.Name()
		if !ValidID(id) {
			entries["?"+id] = Entry{ID: id, Error: "目录名不是合法的包 id（只能是 [a-z0-9-]，最长 32）"}
			continue
		}
		entry, palette, err := loadPack(filepath.Join(s.dir, id, "theme.json"), id)
		if err != nil {
			entries[id] = Entry{ID: id, Error: err.Error()}
			continue
		}
		if _, clash := Builtin[id]; clash {
			entries[id] = Entry{ID: id, Name: entry.Name, Error: "这个 id 是内建包，装不上（删掉 themes/" + id + "）"}
			continue
		}
		entries[id] = entry
		tokens[id] = palette
	}

	s.mu.Lock()
	s.entries, s.tokens, s.loaded, s.err = entries, tokens, info.ModTime(), problem
	s.mu.Unlock()
}

// loadPack validates one manifest. Every check returns a message the settings page can show: a pack
// that silently does nothing is worse than one that says why.
func loadPack(path, id string) (Entry, map[string]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, nil, fmt.Errorf("读不到 theme.json：%v", err)
	}
	return ParseManifest(data, id)
}

// ParseManifest validates one theme.json. Shared by the loader and by the store's install path:
// an install must not write bytes the loader would then refuse, and two validators would drift.
func ParseManifest(data []byte, id string) (Entry, map[string]map[string]string, error) {
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Entry{}, nil, fmt.Errorf("theme.json 不是合法 JSON：%v", err)
	}
	if m.ID != "" && m.ID != id {
		return Entry{}, nil, fmt.Errorf("manifest 的 id（%s）与目录名（%s）不一致", m.ID, id)
	}
	name := strings.TrimSpace(m.Name)
	if name == "" {
		name = id
	}
	if len(name) > 40 {
		return Entry{}, nil, fmt.Errorf("名字太长（%d 字，上限 40）", len(name))
	}
	palette := map[string]map[string]string{}
	for _, scheme := range []string{"light", "dark"} {
		tokens := m.Tokens[scheme]
		if len(tokens) == 0 {
			return Entry{}, nil, fmt.Errorf("缺少 tokens.%s（两套都要写：只写一套的话切到另一侧会读不出来）", scheme)
		}
		clean := map[string]string{}
		for name, value := range tokens {
			tokenName := strings.TrimPrefix(strings.TrimSpace(name), "--")
			if err := ValidateToken(tokenName, value); err != nil {
				return Entry{}, nil, err
			}
			clean[tokenName] = strings.TrimSpace(value)
		}
		for _, required := range requiredTokens {
			if _, ok := clean[required]; !ok {
				return Entry{}, nil, fmt.Errorf("tokens.%s 缺少 %s（骨架令牌必须给：%s）",
					scheme, required, strings.Join(requiredTokens, "、"))
			}
		}
		palette[scheme] = clean
	}
	entry := Entry{ID: id, Name: name, Author: strings.TrimSpace(m.Author), Version: strings.TrimSpace(m.Version), Tokens: palette}
	entry.Problems = auditTokens(palette)
	return entry, palette, nil
}

// styleTagFrom renders a pack's tokens as the element that goes into a plugin document.
//
// The selector is `:root:root` for the reason documented on StyleTag: a plugin's own palette is
// written twice (`:root` and `:root[data-theme="dark"]`), so a plain `:root` would lose to the dark
// block and a pack would work in light mode only.
func styleTagFrom(tokens map[string]string) string {
	if len(tokens) == 0 {
		return ""
	}
	expand := map[string]string{}
	for name, value := range tokens {
		expand[name] = value
		for _, alias := range tokenAliases[name] {
			expand[alias] = value
		}
	}
	names := make([]string, 0, len(expand))
	for name := range expand {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(`<style id="petpc-theme-pack">:root:root{`)
	for _, name := range names {
		b.WriteString("--")
		b.WriteString(name)
		b.WriteString(":")
		b.WriteString(expand[name])
		b.WriteString(";")
	}
	b.WriteString("}</style>")
	return b.String()
}

// SpliceInto puts the fragment at the END of `<head>`, or at the end of the document when there is no
// `</head>`.
//
// Position is the mechanism, not a detail: the injected rule is `:root:root`, which ties on
// specificity with a plugin's own `:root[data-theme="dark"]`, so the later rule wins. "Later" only
// holds if this lands after the plugin's `<link>`/`<style>` - hence the end of the head.
func SpliceInto(doc []byte, fragment string) []byte {
	if fragment == "" {
		return doc
	}
	lower := bytes.ToLower(doc)
	out := make([]byte, 0, len(doc)+len(fragment))
	if i := bytes.LastIndex(lower, []byte("</head>")); i >= 0 {
		out = append(out, doc[:i]...)
		out = append(out, []byte(fragment)...)
		out = append(out, doc[i:]...)
		return out
	}
	out = append(out, doc...)
	out = append(out, []byte(fragment)...)
	return out
}
