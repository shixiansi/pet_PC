// Renders the plugin page in a headless DOM and asserts what it PRINTS.
//
// WHY THIS EXISTS: the plugin page spent five rounds stuck on 「读取中」 and one round
// showing four empty rows, and every round was diagnosed from a screenshot of the .exe.
// Both failures were invisible in the browser for the same reason - the dev stub and the
// page agreed with each other and disagreed with the host - and a screenshot of "nothing"
// carries no information about which of the three was wrong.
//
// So this builds the app the same way `npm run dev` serves it (NODE_ENV=development, so
// `import.meta.env.DEV` is true and the stub answers), mounts it in jsdom, clicks 插件, and
// fails if the rendered text does not contain what the host actually sends: names from
// `Manifest.Name`, ids from `Installed.ID`, wording from the `[]LabelPair` slice.
//
// Run:  npm run smoke
//
// jsdom is borrowed from ../frontend/node_modules (the market's own vitest dependency)
// rather than added here: this program ships two devDependencies, and a check that only
// ever runs on a developer's machine does not justify a third. If that copy is missing the
// script says so and exits, instead of pretending the page rendered.
import { execFileSync } from 'node:child_process';
import { existsSync, mkdtempSync, readdirSync, rmSync } from 'node:fs';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const ccDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const frontendDir = resolve(ccDir, '../frontend');
const require = createRequire(join(frontendDir, 'package.json'));

let JSDOM;
try {
  ({ JSDOM } = require('jsdom'));
} catch {
  console.error('smoke: 找不到 jsdom（期望它在 ' + join(frontendDir, 'node_modules', 'jsdom') + '）');
  console.error('smoke: 先在那边的目录里 npm ci，或者把 jsdom 装到这个程序自己的 devDependencies 里。');
  process.exit(2);
}

// Same code `npm run dev` serves: NODE_ENV is what import.meta.env.DEV follows, and
// `vite build` forces it to production unless it is set here.
const outDir = mkdtempSync(join(tmpdir(), 'petpc-cc-smoke-'));
const vite = join(ccDir, 'node_modules', 'vite', 'bin', 'vite.js');
if (!existsSync(vite)) {
  console.error('smoke: 找不到 ' + vite + '，先 npm ci');
  process.exit(2);
}
try {
  execFileSync(process.execPath, [vite, 'build', '--mode', 'development', '--outDir', outDir, '--emptyOutDir'], {
    cwd: ccDir,
    stdio: 'inherit', // piped stdio is blocked in some sandboxes; inherit works everywhere
    env: { ...process.env, NODE_ENV: 'development' },
  });
} catch {
  console.error('smoke: 构建失败，页面没有渲染即无从验证');
  process.exit(1);
}

const dom = new JSDOM('<!doctype html><html><body><div id="app"></div></body></html>', { url: 'http://localhost/' });
const w = dom.window;

// Svelte 5 reaches for Text/Comment/Element/Node by name, so the window's constructors have
// to exist as globals; anything Node already provides stays untouched.
for (const k of ['window', 'document', 'navigator', 'getComputedStyle']) {
  try { Object.defineProperty(globalThis, k, { value: k === 'window' ? w : w[k], configurable: true, writable: true }); } catch { /* keep node's own */ }
}
for (const k of Object.getOwnPropertyNames(w)) {
  if (!/^[A-Z]/.test(k) || k in globalThis) continue;
  try { Object.defineProperty(globalThis, k, { value: w[k], configurable: true, writable: true }); } catch { /* keep node's own */ }
}
globalThis.requestAnimationFrame = (cb) => setTimeout(cb, 0);

const entry = readdirSync(join(outDir, 'assets')).find((f) => f.endsWith('.js'));
await import(pathToFileURL(join(outDir, 'assets', entry)).href);
// The stub answers 120ms late on purpose, so "读取中" is observable; wait past that.
await new Promise((r) => setTimeout(r, 400));

const rail = [...w.document.querySelectorAll('nav button')].find((b) => b.textContent.trim() === '插件');
if (!rail) {
  console.error('smoke: 找不到「插件」导航按钮，rail 上的页面名字变了？');
  process.exit(1);
}
rail.click();
await new Promise((r) => setTimeout(r, 600));

const text = w.document.body.textContent.replace(/\s+/g, ' ');
const banner = (w.document.getElementById('boot-error')?.textContent || '').trim();
const checks = [
  ['页面挂上了（v4 标记）', text.includes('已装插件 · v4')],
  ['加载完了，不是永久的「读取中」', !text.includes('读取中')],
  ['名字来自 Manifest.Name', text.includes('对话插件') && text.includes('语音插件')],
  ['id 来自 Installed.ID', text.includes('com.shixiansi.plugin-chat')],
  ['权限文案来自 []LabelPair', text.includes('读取待办列表') && text.includes('保存并播放音频')],
  ['manifest 坏掉的那一行说出了原因', text.includes('manifest.json: 第 4 行')],
  ['没有任何一行缺 ID（字段名对得上）', !text.includes('没有 ID')],
  ['两次调用都没报错', !text.includes('已装列表：') && !text.includes('权限文案：')],
  ['没有未捕获错误横幅', banner === ''],
];
let bad = 0;
for (const [name, ok] of checks) {
  console.log((ok ? 'PASS  ' : 'FAIL  ') + name);
  if (!ok) bad++;
}
rmSync(outDir, { recursive: true, force: true });
if (bad) {
  console.log('\n--- 页面文本 ---\n' + text + '\n--- 横幅 ---\n' + banner);
  process.exit(1);
}
console.log('smoke: ' + checks.length + '/' + checks.length);
