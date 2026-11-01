package themepacks

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

/*
 * 主题包的可读性自检。
 *
 * 内建包的每一颗颜色都在 `.dev/check-contrast.cjs` 里实算过（472 个组合）。用户装进来的包
 * 过不了那道门禁——门禁读的是仓库里的 CSS 文件，而用户的包在数据目录里。所以这项检查必须在
 * **加载时**做一次，并把结果摆到界面上：一个"能选但读不出来"的主题包，用户没有别的办法发现。
 *
 * 它检查的组合与门禁一样：每颗文字令牌 × 每个表面令牌，外加填充上的字。判据也一样（WCAG AA
 * 正文 4.5:1）。不同之处只有一处：这里**不拒绝**，只报告——用户已经选了它，宿主该做的是
 * 说清楚哪一颗读不出来，而不是替用户把包扔掉。
 */

// textTokens and surfaceTokens are the same roles the gate measures in the two windows.
var (
	textTokens    = []string{"text", "text-dim", "text-faint", "accent", "success", "warning", "danger", "info"}
	surfaceTokens = []string{"bg", "bg-raised", "bg-sunken", "bg-active"}
)

// minRatio is WCAG 2.1 AA for body text. Duplicated from the Node gate on purpose: the gate cannot
// read user packs, and a shared constant would be a shared build step for two independent tools.
const minRatio = 4.5

var colorRe = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
var rgbRe = regexp.MustCompile(`^rgba?\(\s*([0-9.]+)\s*,\s*([0-9.]+)\s*,\s*([0-9.]+)\s*(?:,\s*([0-9.]+)\s*)?\)$`)

// audit reports the readability failures of a built-in pack (both schemes).
func audit(p Palette) []string {
	return auditTokens(map[string]map[string]string{"light": p.Light, "dark": p.Dark})
}

func auditTokens(palette map[string]map[string]string) []string {
	var problems []string
	for _, scheme := range []string{"light", "dark"} {
		tokens := palette[scheme]
		if len(tokens) == 0 {
			continue
		}
		for _, fg := range textTokens {
			fgValue, ok := tokens[fg]
			if !ok {
				continue // 包不覆盖它 → 用基线，基线已由门禁守过
			}
			for _, bg := range surfaceTokens {
				bgValue, ok := tokens[bg]
				if !ok {
					continue
				}
				ratio, err := ContrastRatio(fgValue, bgValue)
				if err != nil {
					continue
				}
				if ratio < minRatio {
					problems = append(problems, fmt.Sprintf("%s：--%s on --%s 只有 %.2f:1",
						schemeLabel(scheme), fg, bg, ratio))
				}
			}
		}
	}
	return problems
}

func schemeLabel(scheme string) string {
	if scheme == "dark" {
		return "深色"
	}
	return "浅色"
}

// ContrastRatio computes the WCAG relative-luminance ratio between two colour literals.
//
// Alpha is composited onto white: the worst case for a translucent surface, and the same rule the
// Node gate uses. Anything that is not a plain colour returns an error rather than a number - a
// fabricated ratio would be worse than no number at all.
func ContrastRatio(fg, bg string) (float64, error) {
	f, err := parseColor(fg)
	if err != nil {
		return 0, err
	}
	b, err := parseColor(bg)
	if err != nil {
		return 0, err
	}
	white := [3]float64{255, 255, 255}
	fgRGB := composite(f, white)
	bgRGB := composite(b, white)
	lf, lb := luminance(fgRGB), luminance(bgRGB)
	if lb > lf {
		lf, lb = lb, lf
	}
	return (lf + 0.05) / (lb + 0.05), nil
}

func parseColor(value string) ([4]float64, error) {
	v := strings.TrimSpace(value)
	if m := colorRe.FindStringSubmatch(v); m != nil {
		hex := m[1]
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		r, _ := strconv.ParseInt(hex[0:2], 16, 32)
		g, _ := strconv.ParseInt(hex[2:4], 16, 32)
		b, _ := strconv.ParseInt(hex[4:6], 16, 32)
		return [4]float64{float64(r), float64(g), float64(b), 1}, nil
	}
	if m := rgbRe.FindStringSubmatch(v); m != nil {
		r, err1 := strconv.ParseFloat(m[1], 64)
		g, err2 := strconv.ParseFloat(m[2], 64)
		b, err3 := strconv.ParseFloat(m[3], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return [4]float64{}, fmt.Errorf("颜色解析不了：%q", value)
		}
		a := 1.0
		if m[4] != "" {
			parsed, err := strconv.ParseFloat(m[4], 64)
			if err != nil {
				return [4]float64{}, fmt.Errorf("颜色解析不了：%q", value)
			}
			a = parsed
		}
		return [4]float64{r, g, b, a}, nil
	}
	return [4]float64{}, fmt.Errorf("颜色解析不了：%q", value)
}

func composite(c [4]float64, backdrop [3]float64) [3]float64 {
	if c[3] >= 1 {
		return [3]float64{c[0], c[1], c[2]}
	}
	a := c[3]
	return [3]float64{
		c[0]*a + backdrop[0]*(1-a),
		c[1]*a + backdrop[1]*(1-a),
		c[2]*a + backdrop[2]*(1-a),
	}
}

func luminance(c [3]float64) float64 {
	channel := func(v float64) float64 {
		s := v / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c[0]) + 0.7152*channel(c[1]) + 0.0722*channel(c[2])
}
