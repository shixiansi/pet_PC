/*
 * 内建主题包清单。
 *
 * 这张表里**只有名字**，没有一个色值：颜色全部住在 `packs.css` 的
 * `[data-theme-pack='…']` 块里，卡片本身就是"把那个包铺在一个小盒子上"——
 * 它自带 `data-theme` 与 `data-theme-pack`，于是这两行 CSS 就把它画成真实配色。
 * 把色值抄进 TS 才是这类界面最常见的腐坏：改了 CSS、忘了改预览，卡片永远说的是旧话。
 *
 * `violet` 没有对应的 CSS 块：它就是 tokens.css 的默认值。清单里留着它是因为用户要能
 * **选回默认**，而"没有包"在数据里是空字符串（见 store.NormalizePack）。
 */
export type Pack = {
  /** 与 packs.css 的块名、theme.json 里的值一致；violet 例外，它是"没有包"。 */
  id: string;
  name: string;
  hint: string;
};

export const PACKS: Pack[] = [
  { id: 'violet', name: '紫罗兰', hint: '默认。中性底 + 紫色主色，两个窗口出厂就是这个。' },
  { id: 'sakura', name: '樱花粉', hint: '粉底 + 玫红主色。文字改用更深的粉，填充色仍是明亮的。' },
  { id: 'forest', name: '森野绿', hint: '灰绿底 + 森林绿。绿色在这里同时当文字和填充都够亮。' },
  { id: 'graphite', name: '极简石墨', hint: '无彩色。填充是浅灰白，所以按钮上的字翻成深色。' },
];

/** 清单里那个"没有包"的 id。它**不**写进 theme.json——写的是空字符串。 */
export const DEFAULT_PACK = 'violet';

/** 界面上的 id → 存储用的值（violet 存空字符串）。 */
export function packToStore(id: string): string {
  return id === DEFAULT_PACK ? '' : id;
}

/** 存储里的值 → 界面上的 id。 */
export function packFromStore(value: string): string {
  return value === '' ? DEFAULT_PACK : value;
}
