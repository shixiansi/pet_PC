#!/usr/bin/env node
'use strict';

// .dev/check-contrast.cjs —— 配色不是审美问题，是"读不出来"的问题。
//
// # 为什么需要它
//
// 2026-11-01 的界面评审里，两轮"已修复"的结论都是人手算的，而实算出来是这样：
//
//   控制中心 --text-faint #a6b1c0 on --bg #f6f7f9 ....... 2.03  （比任何 AA 线都低）
//   控制中心 --text-dim   #7a8798 on --bg #f6f7f9 ....... 3.41
//   桌宠页   --text-dim   #7a8798 on 面板白 ............ 3.41
//   控制中心 --warning    #d97706 on --warning-bg ....... 3.07
//   深色 --accent #7c3aed 当文字（导航当前项）压在 --bg-active 上 ... 2.89
//
// 这些数字没有一个是"设计得丑"，它们是**没人算**。审美可以讨论，4.5:1 不能讨论——
// 所以这件事必须由一条门禁来守，而不是写进文档让人记得。
//
// # 为什么词表是硬编码的
//
// 和 `.dev/check-frontend.cjs` 里那份权限词表同一个道理：它**故意**不自动跟随 CSS。
// 这个脚本的职责正是"发现词表被改了"——自动推导出来的词表只会跟着一起漂，最后什么也守不住。
// 所以下面列的是**角色**（哪些是文字、哪些是表面、有哪些主题包），**值一律从 CSS 文件里读**：
// 改色的人不必来改这个文件，改完跑一遍就知道有没有踩线。
//
// # 它检查什么
//
//   1. 每个"文字令牌" × 每个"表面令牌"，**每个主题包 × 两套配色** ≥ 4.5:1
//   2. 几条跑不进攻略的组合另外点名：填充上的白字、状态色压在自己的软底上
//      （`--on-accent` 也按包算——石墨的填充是浅灰白，它的字必须是深色）
//   3. 词表里点名的令牌必须**定义过**（每个包、两套配色都要）——
//      一个只写了一半的包不该静默通过
//
// 半透明的令牌（气泡面板、软底、内嵌块的 tint）先合成到它实际压着的那一层，
// 而"实际压着什么"不可知，所以按**最坏情况**合成：白。深色的半透明面板铺在浅色壁纸上
// 就是这么变灰的，那是真实场景，不是理论。
//
// # 用法
//
//   node .dev/check-contrast.cjs
//
// 退出码：0 = 全部达标；1 = 有不合格的组合（会逐条打印实测值）；2 = 文件缺失。
// 不联网、不依赖任何包。

const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..');

// WCAG 2.1 AA 对正文的最低比值。这条线不因字号、不因"它只是提示文字"而放宽：
// 占位符和行尾说明也是被人读的字。
const MIN_RATIO = 4.5;

// 半透明令牌按"压在最坏底色上"合成。白是深色面板最糟的邻居（浅色壁纸），
// 也是浅色面板唯一可能的邻居。
const WORST_BACKDROP = '#ffffff';

/*
 * 词表：三个明面里**在仓内**的两个（插件层住在独立的市场仓，见 design.md §5）。
 *
 * `packs` 是硬编码的包清单。加一个包就必须在这里加一行——包名和 stylesheet 里的块名对不上
 * 时，那条"令牌必须定义过"会替你说出来，而不是让一个半成品包在界面上露一手。
 * 空字符串代表"没有包属性"，也就是 tokens.css 的默认配色。
 */
const SURFACES = [
  {
    label: '控制中心',
    files: ['tools/plugin-market/cc/src/tokens.css', 'tools/plugin-market/cc/src/packs.css'],
    packs: ['', 'sakura', 'forest', 'graphite'],
    // --accent 在词表里是"当文字/图标"的那一档；填充是 --accent-strong，白字压上去单列在 extra。
    text: ['text', 'text-dim', 'text-faint', 'accent', 'success', 'warning', 'danger', 'info'],
    backgrounds: ['bg', 'bg-raised', 'bg-sunken', 'bg-active'],
    extra: [
      { fg: 'on-accent', bg: 'accent-strong', note: '填充按钮上的字' },
      // 深色下这四颗软底是半透明的（rgba(...) 压一层绿/黄/红），而它们压着的是**卡片**，
      // 不是桌面——所以这里点名 `over: 'bg-raised'`。不点名就会按最坏情况合成到白，
      // 把"深色卡片上的浅绿标签"算成淡绿底配浅绿字（1.5:1），那是门禁自己看错了。
      { fg: 'success', bg: 'success-bg', over: 'bg-raised', note: '成功标签' },
      { fg: 'warning', bg: 'warning-bg', over: 'bg-raised', note: '警告标签' },
      { fg: 'danger', bg: 'danger-bg', over: 'bg-raised', note: '危险标签' },
      { fg: 'info', bg: 'info-bg', over: 'bg-raised', note: '信息标签' },
    ],
  },
  {
    label: '桌宠页',
    files: ['frontend/src/style.css'],
    packs: ['', 'sakura', 'forest', 'graphite'],
    text: ['text', 'text-dim', 'accent', 'ok', 'warn', 'danger'],
    backgrounds: ['panel-bg', 'chip-bg', 'chip-hover'],
    extra: [
      { fg: 'text', bg: 'tint', over: 'panel-bg', note: '气泡里的内嵌块' },
      { fg: 'text-dim', bg: 'tint', over: 'panel-bg', note: '内嵌块里的说明行' },
      // 压在被填充的底色上的字：徽章的底是 --accent-strong，toast 的底是 --danger。
      // 石墨把填充换成了浅灰白、深色的 --danger 本身就是浅红，这两条正是"白字默认"会踩空的地方。
      { fg: 'on-accent', bg: 'accent-strong', note: '徽章上的字' },
      { fg: 'on-danger', bg: 'danger', note: 'toast 上的字' },
    ],
  },
];

// --- 读 CSS -------------------------------------------------------------------

// 注释先删掉：这个仓库的注释里写着 "#7c3aed"、"2.89:1" 这样的字，留着会把它们当成声明。
function stripComments(css) {
  return css.replace(/\/\*[\s\S]*?\*\//g, '');
}

/*
 * 把若干份样式表拆成"有条件的声明块"，条件和顺序都留着。
 *
 * 一个块的两个维度：
 *   - theme：选择器里有 `[data-theme='dark']` / `'light']` 就是那一侧，没有就是两边都生效；
 *   - pack：选择器里有 `[data-theme-pack='x']` 就是那个包，没有就是所有包都生效。
 *
 * 保留顺序（而不是立刻合并）是因为层叠规则：`packs.css` 的值要压过 `tokens.css` 的默认值。
 * 跑某一种组合时，按文档顺序把匹配的块摊上去，后写者胜——这就是浏览器的做法。
 */
function loadBlocks(files, where) {
  const blocks = [];
  for (const rel of files) {
    const file = path.join(root, rel);
    if (!fs.existsSync(file)) {
      console.error(`缺少 ${rel}`);
      process.exit(2);
    }
    const css = stripComments(fs.readFileSync(file, 'utf8'));
    const blockRe = /([^{}]+)\{([^{}]*)\}/g;
    let m;
    while ((m = blockRe.exec(css)) !== null) {
      const selector = m[1];
      // `:root` 是 tokens.css 的写法；包的块**故意不带 `:root`**（`[data-theme-pack='x']`），
      // 这样同一份令牌也能铺在"主题包预览卡"那个小盒子上——预览卡不需要抄一份色值，
      // 它只要自带 data-theme + data-theme-pack，两行 CSS 就把它画成真实配色。
      if (!selector.includes(':root') && !selector.includes('[data-theme-pack')) continue;
      const theme = /data-theme\s*=\s*['"]dark['"]/.test(selector)
        ? 'dark'
        : /data-theme\s*=\s*['"]light['"]/.test(selector)
          ? 'light'
          : null;
      const packMatch = /data-theme-pack\s*=\s*['"]([a-z0-9-]+)['"]/.exec(selector);
      const decls = {};
      const declRe = /(--[a-z0-9-]+)\s*:\s*([^;]+);/gi;
      let d;
      while ((d = declRe.exec(m[2])) !== null) {
        // 键去掉前导 `--`：下面点名词表时写的是 `text` 而不是 `--text`，一处归一化比两处约定好。
        decls[d[1].toLowerCase().replace(/^--/, '')] = d[2].trim();
      }
      blocks.push({ theme, pack: packMatch ? packMatch[1] : null, decls });
    }
  }
  if (blocks.length === 0) {
    console.error(`${where}：一份样式表里都没找到 :root 块`);
    process.exit(2);
  }
  return blocks;
}

function mapFor(blocks, scheme, pack) {
  const map = {};
  for (const b of blocks) {
    if (b.theme && b.theme !== scheme) continue;
    if (b.pack && b.pack !== pack) continue;
    Object.assign(map, b.decls);
  }
  return map;
}

// --- 颜色 ---------------------------------------------------------------------

function parseColor(raw, where) {
  const v = raw.trim().toLowerCase();
  let m = /^#([0-9a-f]{6})$/.exec(v);
  if (m) {
    const n = m[1];
    return [parseInt(n.slice(0, 2), 16), parseInt(n.slice(2, 4), 16), parseInt(n.slice(4, 6), 16), 1];
  }
  m = /^#([0-9a-f]{3})$/.exec(v);
  if (m) {
    const [r, g, b] = m[1].split('');
    return [parseInt(r + r, 16), parseInt(g + g, 16), parseInt(b + b, 16), 1];
  }
  m = /^rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)\s*(?:,\s*([\d.]+)\s*)?\)$/.exec(v);
  if (m) {
    return [Number(m[1]), Number(m[2]), Number(m[3]), m[4] === undefined ? 1 : Number(m[4])];
  }
  throw new Error(`${where}：看不懂的颜色 ${JSON.stringify(raw)}（这个脚本只认 hex / rgb() / rgba()）`);
}

// 把半透明色压到不透明的底上，得到"眼睛看到的那个颜色"。
function composite(fg, backdrop) {
  const a = fg[3];
  if (a >= 1) return fg;
  return [fg[0] * a + backdrop[0] * (1 - a), fg[1] * a + backdrop[1] * (1 - a), fg[2] * a + backdrop[2] * (1 - a), 1];
}

function relativeLuminance([r, g, b]) {
  const channel = (c) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

function contrast(fg, bg) {
  const a = relativeLuminance(fg);
  const b = relativeLuminance(bg);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

// --- 令牌 → 颜色 --------------------------------------------------------------

const VAR_RE = /^var\(\s*(--[a-z0-9-]+)\s*\)$/i;

/*
 * 把一颗令牌解析成不透明颜色。
 *
 * `overToken` 是它压着的那一层（令牌名）；不给就是 WORST_BACKDROP。递归最多五跳——
 * 仓里现在最深的一层是 tint → panel-bg → 白，两跳。五跳是给"以后有人加一层"留的余量，
 * 超过就报错而不是默默当成白：一个解析不了的令牌不该看起来像通过。
 */
function resolve(token, map, where, overToken = null, depth = 0) {
  if (depth > 5) throw new Error(`${where}：${token} 的 var() 链太深（可能自我引用）`);
  let raw = map[token];
  if (raw === undefined) throw new Error(`${where}：令牌 ${token} 没有定义`);
  const varMatch = VAR_RE.exec(raw.trim());
  if (varMatch) {
    const target = varMatch[1].toLowerCase().replace(/^--/, '');
    raw = map[target];
    if (raw === undefined) throw new Error(`${where}：${token} 指向了未定义的 ${varMatch[1]}`);
  }
  const color = parseColor(raw, `${where} 的 ${token}`);
  if (color[3] >= 1) return color;
  const backdrop = overToken
    ? resolve(overToken, map, where, null, depth + 1)
    : parseColor(WORST_BACKDROP, 'backdrop');
  return composite(color, backdrop);
}

// --- 跑 -----------------------------------------------------------------------

const failures = [];
let checked = 0;

const fmt = (n) => n.toFixed(2);

function runSurface(surface, blocks, scheme, pack) {
  const map = mapFor(blocks, scheme, pack);
  const where = `${surface.label} · ${scheme === 'dark' ? '深色' : '浅色'}${pack ? ' · 主题包 ' + pack : ''}`;

  console.log(`\n=== ${where} ===`);
  console.log('  文字令牌 → 表面令牌（最右两列：实测比值 / 结论）');

  const rows = [];
  for (const fgToken of surface.text) {
    for (const bgToken of surface.backgrounds) {
      let ratio;
      try {
        const fg = resolve(fgToken, map, where);
        const bg = resolve(bgToken, map, where);
        // 前景自己也可能是半透明的（几乎不会，但错的时候要说清楚）
        const flatFg = fg[3] >= 1 ? fg : composite(fg, bg);
        ratio = contrast(flatFg, bg);
      } catch (err) {
        failures.push(err.message);
        rows.push({ fgToken, bgToken, ratio: null, ok: false });
        continue;
      }
      const ok = ratio >= MIN_RATIO;
      checked += 1;
      if (!ok) failures.push(`${where}：--${fgToken} on --${bgToken} 只有 ${fmt(ratio)}:1（要 ≥ ${MIN_RATIO}）`);
      rows.push({ fgToken, bgToken, ratio, ok });
    }
  }
  for (const extra of surface.extra) {
    let ratio;
    try {
      const bg = resolve(extra.bg, map, where, extra.over || null);
      const fgRaw = resolve(extra.fg, map, where);
      ratio = contrast(fgRaw, bg);
    } catch (err) {
      failures.push(err.message);
      rows.push({ fgToken: extra.fg, bgToken: extra.bg, ratio: null, ok: false, note: extra.note });
      continue;
    }
    const ok = ratio >= MIN_RATIO;
    checked += 1;
    if (!ok) {
      failures.push(
        `${where}：--${extra.fg} on --${extra.bg}（${extra.note}）只有 ${fmt(ratio)}:1（要 ≥ ${MIN_RATIO}）`,
      );
    }
    rows.push({ fgToken: extra.fg, bgToken: extra.bg, ratio, ok, note: extra.note });
  }
  for (const row of rows) {
    const label = `--${row.fgToken}`;
    const bgLabel = `--${row.bgToken}${row.note ? ` (${row.note})` : ''}`;
    const verdict = row.ratio === null ? '解析不了' : row.ok ? 'PASS' : 'FAIL';
    console.log(
      `  ${label.padEnd(18)}${bgLabel.padEnd(34)}${row.ratio === null ? '   -  ' : fmt(row.ratio).padStart(7)}  ${verdict}`,
    );
  }
}

for (const surface of SURFACES) {
  const blocks = loadBlocks(surface.files, surface.label);
  for (const pack of surface.packs) {
    for (const scheme of ['light', 'dark']) runSurface(surface, blocks, scheme, pack);
  }
}

console.log('');
if (failures.length > 0) {
  console.error(`对比度检查：${checked} 个组合里 ${failures.length} 处不合格`);
  for (const f of failures) console.error('  FAIL  ' + f);
  console.error('');
  console.error('这些值不是"设计得丑"，是算出来读不出来。改色，别改这条门禁的阈值——');
  console.error('4.5:1 是 WCAG AA 对正文的线，放宽它等于把这条检查关掉。');
  process.exit(1);
}
console.log(`对比度检查：${checked} 个组合全部达 AA（≥ ${MIN_RATIO}:1），两套配色 × 每个主题包都跑过`);
