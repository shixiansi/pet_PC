package themepacks

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 主题包的宿主侧：色板表、注入形状、注入位置，以及"表与两份 CSS 不许分叉"。
//
// WHY THIS FILE. 主题包现在有三份色板（这张 Go 表、控制中心的 packs.css、桌宠的 style.css），
// 每份服务一个不同的运行时：Go 那份要能按请求拼进插件文档（沙箱帧里宿主碰不到 DOM，这是唯一
// 不需要插件配合的路），两份 CSS 服务两个窗口。三份里任意一份错了，症状都是"某个界面颜色不对"
// 而另外两处看起来完全正常——所以这里有一条会红的测试，而不是一条注释。

func TestBuiltinPackTokensAreInTheVocabularyAndAreColours(t *testing.T) {
	if len(Builtin) < 3 {
		t.Fatalf("只有 %d 个内建包，界面上的卡片会少", len(Builtin))
	}
	for id, palette := range Builtin {
		if normalizePackShape(id) != id {
			t.Fatalf("包 id %q 不是合法形状（要 [a-z0-9-]）", id)
		}
		for scheme, tokens := range map[string]map[string]string{"light": palette.Light, "dark": palette.Dark} {
			if len(tokens) == 0 {
				t.Fatalf("%s 的 %s 一套是空的：切到那一侧会退回默认配色", id, scheme)
			}
			// 两套都必须带齐"骨架"令牌：少了任何一颗，那一侧就会有整块界面还是上一个配色。
			// 状态色（success/warning/…）不在此列——包可以只压浅色那一侧的那几颗，深色沿用基线亮色
			// （基线深色的状态色本来就是为深底挑过的），这不是遗漏，是选择。
			for _, required := range []string{"bg", "bg-raised", "bg-sunken", "text", "text-dim", "accent", "accent-strong", "accent-soft"} {
				if _, ok := tokens[required]; !ok {
					t.Fatalf("%s 的 %s 缺少骨架令牌 %q", id, scheme, required)
				}
			}
			for name, value := range tokens {
				if err := ValidateToken(name, value); err != nil {
					t.Fatalf("%s/%s：%v", id, scheme, err)
				}
			}
		}
	}
	// 默认包刻意不在这张表里：它就是两份 CSS 的基线，写一份就是第二个真相。
	if _, ok := Builtin["violet"]; ok {
		t.Fatal("violet 不该出现在 Builtin 里（它是 stylesheet 的默认值）")
	}
}

// normalizePackShape keeps the shape check in one place; store.NormalizePack does the same thing
// for the persisted value, and this package must not import store (store imports nothing from here,
// but the dependency is the wrong way round for a rule about pack ids).
func normalizePackShape(id string) string {
	if id == "" || len(id) > 32 {
		return ""
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return ""
		}
	}
	return id
}

func TestCSSOnlyForRealPacks(t *testing.T) {
	if got := CSS("light", ""); got != "" {
		t.Fatalf("没有包时不该注入任何东西，得到 %q", got)
	}
	if got := CSS("light", "violet"); got != "" {
		t.Fatalf("默认包不该注入（它就是插件的基线），得到 %q", got)
	}
	if got := CSS("light", "nope"); got != "" {
		t.Fatalf("未知的包不该注入，得到 %q", got)
	}
	if got := StyleTag("dark", ""); got != "" {
		t.Fatalf("没有包时不该有 <style>，得到 %q", got)
	}

	light := CSS("light", "sakura")
	dark := CSS("dark", "sakura")
	if !strings.HasPrefix(light, ":root:root{") {
		t.Fatalf("选择器必须是 :root:root（要压过插件自己的 [data-theme] 块），得到 %q", light)
	}
	if !strings.Contains(light, "--accent:#be1857;") {
		t.Fatalf("浅色 accent 不对：%q", light)
	}
	if !strings.Contains(dark, "--accent:#f472b6;") {
		t.Fatalf("深色 accent 不对：%q", dark)
	}
	// 令牌按名字排序：同一次请求的输出必须逐字节稳定，否则一个"没改任何东西"的改动也会让
	// 缓存/快照类检查抖动。
	names := regexp.MustCompile(`--([a-z-]+):`).FindAllStringSubmatch(light, -1)
	var order []string
	for _, m := range names {
		order = append(order, m[1])
	}
	if !sort.StringsAreSorted(order) {
		t.Fatalf("令牌不是按名字排序的：%v", order)
	}
	// 别名：插件层把 --border-strong 写作 --line-strong，只发一个就是半套主题。
	if !strings.Contains(light, "--line-strong:#e3c3d0;") {
		t.Fatalf("缺少 --line-strong 别名：%q", light)
	}
	if !strings.Contains(CSS("dark", "graphite"), "--on-accent:#0d0e10;") {
		t.Fatal("石墨的深色必须翻转 --on-accent（填充是浅灰白）")
	}
}

func TestInjectPackGoesAfterThePluginsOwnStylesheet(t *testing.T) {
	doc := `<!doctype html><html><head><meta charset="utf-8">` +
		`<link rel="stylesheet" href="ui.css"><script src="/plugin-asset/@host/bridge.js"></script>` +
		`</head><body><div id="app"></div></body></html>`
	out := string(SpliceInto([]byte(doc), StyleTag("light", "sakura")))

	linkAt := strings.Index(out, `href="ui.css"`)
	styleAt := strings.Index(out, `<style id="petpc-theme-pack">`)
	headEnd := strings.Index(out, "</head>")
	if styleAt < 0 {
		t.Fatalf("没有注入：%s", out)
	}
	if !(linkAt < styleAt && styleAt < headEnd) {
		t.Fatalf("注入的位置不对（link %d, style %d, </head> %d）：必须排在插件自己的样式表之后、</head> 之前", linkAt, styleAt, headEnd)
	}

	// 没有 </head> 的文档：排在最后，理由同上（后写者胜）。
	fragment := `<div id="app"></div>`
	out = string(SpliceInto([]byte(fragment), StyleTag("dark", "forest")))
	if !strings.HasSuffix(out, `</style>`) {
		t.Fatalf("无 head 的文档应该把样式追加在最后：%s", out)
	}

	// 没有包 = 一个字节都不改。这条比"注入了"更重要：空 <style> 会让"宿主没给它主题"和
	// "宿主给了它一个空主题"在 DOM 里长得一样。
	if got := string(SpliceInto([]byte(doc), "")); got != doc {
		t.Fatalf("没有包时文档被改动了：%s", got)
	}
}

// TestPackPalettesMatchTheStylesheets is the three-copy guard.
//
// It reads the two CSS files out of the repository. That is a coupling this package otherwise does
// not have, and it is deliberate: the alternative is trusting that whoever edits a palette edits it
// three times, and "the window is pink but the plugin panel is still violet" is exactly the class of
// bug that survives review because no single screen shows it.
func TestPackPalettesMatchTheStylesheets(t *testing.T) {
	ccCSS := readRepoFile(t, "../../tools/plugin-market/cc/src/packs.css")
	petCSS := readRepoFile(t, "../../frontend/src/style.css")

	for id, palette := range Builtin {
		for _, scheme := range []string{"light", "dark"} {
			want := palette.Light
			if scheme == "dark" {
				want = palette.Dark
			}
			// 控制中心：词表逐项相同，一个名字都不许漏。
			block := cssBlock(t, ccCSS, `[data-theme='`+scheme+`'][data-theme-pack='`+id+`']`)
			for name, value := range want {
				if !strings.Contains(block, "--"+name+": "+value+";") {
					t.Fatalf("cc/packs.css 的 %s/%s 里 --%s 不是 %s：\n%s", id, scheme, name, value, block)
				}
			}
			// 桌宠那一页的词表更小（面板色是 --panel-bg 这种半透明名字），所以按映射比它认识的那几颗，
			// 并且要求它至少真的比到了骨架里的四颗——否则"映射为空"会让这条断言静默变成永真。
			petBlock := cssBlock(t, petCSS, `:root[data-theme='`+scheme+`'][data-theme-pack='`+id+`']`)
			compared := 0
			for name, value := range want {
				petName, ok := petTokenFor(name)
				if !ok {
					continue
				}
				compared++
				if !strings.Contains(petBlock, "--"+petName+": "+value+";") {
					t.Fatalf("frontend/style.css 的 %s/%s 里 --%s（=cc 的 --%s）不是 %s：\n%s",
						id, scheme, petName, name, value, petBlock)
				}
			}
			if compared < 4 {
				t.Fatalf("%s/%s 只比到 %d 颗令牌：映射表或样式表有一个漏了", id, scheme, compared)
			}
		}
	}
}

// petTokenFor maps the control center's token names onto the vocabulary the pet's stylesheet uses
// for the same role. The pet draws a floating card (`--panel-bg`, translucent) rather than a page
// background, so most surface names simply do not exist there - and those it does share are the ones
// that decide whether the two windows look like one product.
//
// STATE COLOURS ARE DELIBERATELY NOT MAPPED. --warning/--success are tuned per SURFACE (a pink
// `--bg-active` eats amber, a grey `--chip-hover` eats it differently), so a pack may legitimately
// press amber darker in the control center and leave the pet's card alone. `.dev/check-contrast.cjs`
// is what holds each surface to 4.5:1; demanding identical values here would force one of the two
// windows to be less readable than it can be.
func petTokenFor(name string) (string, bool) {
	switch name {
	case "text", "text-dim", "accent", "accent-strong", "accent-hover", "on-accent":
		return name, true
	}
	return "", false
}

func cssBlock(t *testing.T, css, selector string) string {
	t.Helper()
	at := strings.Index(css, selector)
	if at < 0 {
		t.Fatalf("找不到选择器 %s", selector)
	}
	open := strings.Index(css[at:], "{")
	closeAt := strings.Index(css[at:], "}")
	if open < 0 || closeAt < 0 || closeAt < open {
		t.Fatalf("选择器 %s 的块不完整", selector)
	}
	return css[at+open+1 : at+closeAt]
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读不到 %s：%v", rel, err)
	}
	return string(data)
}
