package themepacks

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

/*
 * Theme packs, host side: the tokens a pack overrides, per scheme.
 *
 * WHY THIS TABLE EXISTS AT ALL, when the two windows already have their palettes in CSS
 * (`tools/plugin-market/cc/src/packs.css`, `frontend/src/style.css`): the plugin's document is a
 * SANDBOXED OPAQUE ORIGIN iframe. The host cannot touch its DOM, so "make the plugin follow the
 * pack" has exactly one implementation that needs no plugin cooperation - serve the entry document
 * with a `<style>` injected into it. That means the values must exist in Go, at request time.
 *
 * THREE COPIES, ONE GUARD. The palettes now live in three places (this table, and one CSS file per
 * window) because each is consumed by a different runtime. That is a real cost, paid on purpose, and
 * it is paid down by `TestPalettesMatchTheStylesheets`: it parses both CSS files and fails if a
 * value here disagrees with either. A palette that is right in the window and wrong inside plugin
 * frames is otherwise invisible until someone opens a plugin on a pink desktop.
 *
 * The table holds ONLY what a pack overrides. Tokens it does not name keep whatever the plugin's own
 * `ui.css` says - which is the same violet baseline the two windows use, so a pack that overrides
 * `--accent` does not have to restate `--success`.
 */

// Palette is one pack's overrides, keyed by token name WITHOUT the leading `--`.
type Palette struct {
	Light map[string]string
	Dark  map[string]string
}

// Builtin are the packs compiled into the program. The default pack ("violet") is absent on
// purpose: it IS the stylesheets' baseline, and a block for it would be a second truth.
var Builtin = map[string]Palette{
	"sakura": {
		Light: map[string]string{
			"bg": "#fdf7f9", "bg-app": "#fdf7f9", "bg-raised": "#ffffff", "bg-surface": "#ffffff",
			"bg-sunken": "#fdf2f6", "bg-hover": "#fdf2f6", "bg-active": "#fce7ef",
			"line": "#f2dde5", "border": "#f2dde5", "border-strong": "#e3c3d0",
			"text": "#2b1120", "text-dim": "#7a5c6a", "text-faint": "#82626f",
			"accent": "#be1857", "accent-strong": "#db2777", "accent-hover": "#9d174d",
			"accent-soft": "#fdf2f6", "accent-dim": "#f9a8d4",
			"ring": "rgba(219, 39, 119, 0.28)",
			// 粉底吃暖色：同一颗 -700 的警告/信息色压不到 4.5，见 packs.css 的注释。
			"warning": "#a34a08", "info": "#1d4ed8",
		},
		Dark: map[string]string{
			"bg": "#180d14", "bg-app": "#180d14", "bg-raised": "#241620", "bg-surface": "#241620",
			"bg-sunken": "#120a0f", "bg-hover": "#2c1b26", "bg-active": "#3a1c2c",
			"line": "#35202d", "border": "#35202d", "border-strong": "#472b3a",
			"text": "#fbe9f1", "text-dim": "#d3a8bb", "text-faint": "#bb91a4",
			"accent": "#f472b6", "accent-strong": "#db2777", "accent-hover": "#be1857",
			"accent-soft": "#3a1c2c", "accent-dim": "#9d174d",
			"ring": "rgba(244, 114, 182, 0.32)",
		},
	},
	"forest": {
		Light: map[string]string{
			"bg": "#f5faf7", "bg-app": "#f5faf7", "bg-raised": "#ffffff", "bg-surface": "#ffffff",
			"bg-sunken": "#eef7f1", "bg-hover": "#eef7f1", "bg-active": "#e3f4e9",
			"line": "#dcede2", "border": "#dcede2", "border-strong": "#c2ddca",
			"text": "#0d2018", "text-dim": "#4e6b5a", "text-faint": "#526d5d",
			"accent": "#047857", "accent-strong": "#047857", "accent-hover": "#036249",
			"accent-soft": "#ecfdf5", "accent-dim": "#6ee7b7",
			"ring": "rgba(4, 120, 87, 0.28)",
			"warning": "#a34a08",
		},
		Dark: map[string]string{
			"bg": "#0a1510", "bg-app": "#0a1510", "bg-raised": "#111f18", "bg-surface": "#111f18",
			"bg-sunken": "#070f0b", "bg-hover": "#16281f", "bg-active": "#123528",
			"line": "#1d3128", "border": "#1d3128", "border-strong": "#274234",
			"text": "#e6f5ec", "text-dim": "#a8c6b6", "text-faint": "#8cae9c",
			"accent": "#34d399", "accent-strong": "#047857", "accent-hover": "#036249",
			"accent-soft": "#123528", "accent-dim": "#065f46",
			"ring": "rgba(52, 211, 153, 0.3)",
		},
	},
	"graphite": {
		Light: map[string]string{
			"bg": "#f4f4f5", "bg-app": "#f4f4f5", "bg-raised": "#ffffff", "bg-surface": "#ffffff",
			"bg-sunken": "#ececee", "bg-hover": "#ececee", "bg-active": "#e6e6e8",
			"line": "#e4e4e7", "border": "#e4e4e7", "border-strong": "#d4d4d8",
			"text": "#18181b", "text-dim": "#52525b", "text-faint": "#5f5f68",
			"accent": "#18181b", "accent-strong": "#18181b", "accent-hover": "#3f3f46",
			"accent-soft": "#ececee", "accent-dim": "#a1a1aa",
			"ring":    "rgba(24, 24, 27, 0.22)",
			"success": "#065f46", "warning": "#9a4507", "info": "#1d4ed8",
		},
		Dark: map[string]string{
			"bg": "#111214", "bg-app": "#111214", "bg-raised": "#191b1e", "bg-surface": "#191b1e",
			"bg-sunken": "#0d0e10", "bg-hover": "#22252a", "bg-active": "#2a2d32",
			"line": "#2a2d32", "border": "#2a2d32", "border-strong": "#3a3e45",
			"text": "#e8e9ec", "text-dim": "#a2a6ad", "text-faint": "#94989f",
			"accent": "#e8e9ec", "accent-strong": "#e8e9ec", "accent-hover": "#c9ccd2",
			"accent-soft": "#22252a", "accent-dim": "#4a4e56",
			// 填充是浅灰白，所以填充上的字必须是深色——这是唯一一个翻转它的包。
			"on-accent": "#0d0e10",
			"ring":      "rgba(232, 233, 236, 0.3)",
		},
	},
}

// tokenAliases maps a token name to the extra names a plugin document may use for it.
//
// `--line-strong` is petpc-ui.css's spelling of `--border-strong`; emitting both costs one line and
// stops a pack from producing a half-themed frame whose borders are still violet.
var tokenAliases = map[string][]string{
	"border-strong": {"line-strong"},
}

// TokenNames is the vocabulary a pack may override - the shape a theme.json may carry.
//
// A WHITELIST, not a blacklist, and it is the security boundary of user packs: these values end up
// inside a `<style>` element in the host's own document. Anything not named here is ignored, so a
// manifest cannot invent `--x: url(http://…)` and have the host paint it.
var TokenNames = func() map[string]bool {
	names := map[string]bool{}
	for _, p := range Builtin {
		for n := range p.Light {
			names[n] = true
		}
		for n := range p.Dark {
			names[n] = true
		}
	}
	for _, extra := range tokenAliases {
		for _, n := range extra {
			names[n] = true
		}
	}
	return names
}()

// tokenValueRe is the ONLY shape a token value may have: a hex colour or an rgb()/rgba() literal.
//
// Strict on purpose. These strings are concatenated into CSS that runs in a document the host then
// puts inside its own window, so "is it a colour?" is not a formatting question - it is the reason a
// pack cannot smuggle `}` or `url(` into the host's stylesheet.
var tokenValueRe = regexp.MustCompile(`^(#[0-9a-fA-F]{3,8}|rgba?\(\s*[0-9.]+%?\s*,\s*[0-9.]+%?\s*,\s*[0-9.]+%?\s*(,\s*[0-9.]+\s*)?\))$`)

// ValidateToken reports whether a pack may set this token to this value.
//
// Both halves are checked: the NAME must be in the vocabulary, and the VALUE must be a colour
// literal. Callers that load a theme.json from disk report the error to the user; callers that read
// the built-in table never fail (a test asserts that instead).
func ValidateToken(name, value string) error {
	trimmedName := strings.TrimPrefix(strings.TrimSpace(name), "--")
	if !TokenNames[trimmedName] {
		return fmt.Errorf("主题包不能设置 --%s（不在白名单里）", trimmedName)
	}
	trimmedValue := strings.TrimSpace(value)
	if err := ValidateTokenValue(trimmedValue); err != nil {
		return fmt.Errorf("--%s：%v", trimmedName, err)
	}
	return nil
}

// ValidateTokenValue is the value half, exported because the loader validates a whole manifest
// before it knows which tokens it will keep.
func ValidateTokenValue(value string) error {
	if len(value) > 64 {
		return fmt.Errorf("值太长（%d 字节）", len(value))
	}
	if !tokenValueRe.MatchString(value) {
		return fmt.Errorf("只能写颜色字面量（#rgb / #rrggbb / rgb() / rgba()），不能是 %q", value)
	}
	return nil
}

// PackCSS builds the `<style>` body a plugin document needs to follow a pack, or "" when there is
// nothing to inject.
//
// Empty string means "emit nothing at all", and that is the common case: the default pack, an
// unknown id, or a frame that asked for no pack. Injecting an empty rule would be harmless in CSS
// terms and harmful in review terms - an empty `<style>` in every plugin document makes it
// impossible to tell "the host did not theme this frame" from "the host themed it with nothing".
//
// The selector is `:root:root`, not `:root`. A plugin's own ui.css writes its palette as
// `:root { … }` AND `:root[data-theme="dark"] { … }`; the doubled pseudo-class gives this rule
// specificity (0,2,0), which ties with the dark block and beats the plain one - and it is injected
// at the END of `<head>`, so on a tie the later rule wins. `:root` alone would lose to every
// plugin's dark block and the pack would work in light mode only.
func CSS(scheme, pack string) string {
	palette, ok := Builtin[pack]
	if !ok {
		return ""
	}
	tokens := palette.Light
	if scheme == "dark" {
		tokens = palette.Dark
	}
	if len(tokens) == 0 {
		return ""
	}
	names := make([]string, 0, len(tokens))
	for n := range tokens {
		names = append(names, n)
	}
	// Fold the aliases into the same map BEFORE sorting: emitting them inline next to their token
	// would leave the output unsorted, and "same input, same bytes" is what makes an injected
	// document diffable and a snapshot test possible.
	expand := map[string]string{}
	for _, n := range names {
		expand[n] = tokens[n]
		for _, alias := range tokenAliases[n] {
			expand[alias] = tokens[n]
		}
	}
	names = names[:0]
	for n := range expand {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString(":root:root{")
	for _, n := range names {
		b.WriteString("--")
		b.WriteString(n)
		b.WriteString(":")
		b.WriteString(expand[n])
		b.WriteString(";")
	}
	b.WriteString("}")
	return b.String()
}

// PackStyleTag wraps PackCSS in the element that goes into the document, or "" for nothing.
func StyleTag(scheme, pack string) string {
	css := CSS(scheme, pack)
	if css == "" {
		return ""
	}
	return `<style id="petpc-theme-pack">` + css + `</style>`
}
