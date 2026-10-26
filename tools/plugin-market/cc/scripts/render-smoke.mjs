// Renders the six pages in a headless DOM and asserts what they PRINT and what their
// inputs are set to.
//
// WHY THIS EXISTS: the plugin page spent five rounds stuck on 「读取中」 and one round
// showing four empty rows, and every round was diagnosed from a screenshot of the .exe.
// Both failures were invisible in the browser for the same reason - the dev stub and the
// page agreed with each other and disagreed with the host - and a screenshot of "nothing"
// carries no information about which of the three was wrong.
//
// Then the start page turned out to have the same defect in the other direction: it read
// `cfg.PetScale` while the host sends `petScale`, so the slider was pinned at 1.0 and a
// drag made it throw. It was announced as "确认可用" because a slider at 1.0 looks fine.
// An assertion on the VALUE, against a fake that is 1.2, is the difference between "the
// page rendered" and "the page rendered the host's number".
//
// So: this builds the app the same way `npm run dev` serves it (NODE_ENV=development, so
// `import.meta.env.DEV` is true and the stub answers), mounts it in jsdom, walks all six
// rail pages, and fails on any empty field, any error banner, any input holding the wrong
// number.
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
const settle = () => new Promise((r) => setTimeout(r, 600));
await settle();

const text = () => w.document.body.textContent.replace(/\s+/g, ' ');
const banner = () => (w.document.getElementById('boot-error')?.textContent || '').trim();

async function open(label) {
  const button = [...w.document.querySelectorAll('nav button')].find((b) => b.textContent.trim() === label);
  if (!button) throw new Error('找不到「' + label + '」导航按钮，rail 上的页面名字变了？');
  button.click();
  await settle();
}

const checks = [];
const check = (name, ok) => checks.push([name, ok]);

await open('开始');
const range = w.document.querySelector('input[type="range"]');
check('开始：滑块拿到了宿主的 petScale（1.2）', range?.value === '1.2');
check('开始：读数也是 1.2×', text().includes('1.2×'));
check('开始：没有停在读取中', !text().includes('读取中'));
check(
  '开始：贴边 / 开机启动 / 快捷键三个开关在，键位来自宿主',
  text().includes('拖动后自动贴边') && text().includes('开机自动启动') && text().includes('Ctrl+Alt+S'),
);

await open('待办');
check('待办：标题来自 store.Todo.title', text().includes('写周报') && text().includes('整理纸箱'));
check('待办：完成数来自 store.Todo.done', text().includes('1 项未完成 / 共 2 项'));

await open('专注');
const number = w.document.querySelector('input[type="number"]');
check('专注：第一个框拿到了 focusMinutes（25）', number?.value === '25');
check('专注：自动接续读到了 autoStartNextPhase', text().includes('自动接续下一段'));
check(
  '专注：两个番茄钟开关在（提示音 / 启动时开启）',
  text().includes('阶段结束提示音') && text().includes('启动时自动开启番茄钟') && text().includes('下次启动生效'),
);

await open('记录');
check('记录：来自 store.PomodoroRecord', text().includes('写周报') && text().includes('共 1 个番茄'));

await open('插件');
check('插件：四/三行的名字来自 plugins.Installed.Manifest.Name', text().includes('对话插件') && text().includes('语音插件'));
check('插件：id 来自 ID', text().includes('com.shixiansi.plugin-chat'));
check('插件：文案来自 []LabelPair', text().includes('读取待办列表') && text().includes('保存并播放音频'));
check('插件：manifest 坏掉的那一行说出了原因', text().includes('manifest.json: 第 4 行'));
check('插件：没有任何一行缺 ID（字段名对得上）', !text().includes('没有 ID'));

await open('高级');
check('高级：模型配置来自 store.Config', text().includes('deepseek') && text().includes('deepseek-chat'));
check('高级：密钥状态来自 HostKey()', text().includes('已设置（DPAPI 密文）') && text().includes('llm-key.bin'));
check('高级：数据目录来自 DataDir()', text().includes('C:\\fake\\PetPC'));
check('高级：整组对话提醒搬过来了', text().includes('开启对话提醒') && text().includes('活跃时段') && text().includes('没有待办时也提醒'));
const numberValues = [...w.document.querySelectorAll('input[type="number"]')].map((i) => i.value);
check('高级：提醒间隔来自 reminderIntervalMin（30）', numberValues.includes('30'));

// The report is generated on demand, so this also checks that the button is wired and that
// the text arrives - the two things a "有按钮" screenshot cannot tell you.
const diagButton = [...w.document.querySelectorAll('button')].find((b) => b.textContent.trim().startsWith('生成诊断报告'));
check('高级：诊断按钮在', Boolean(diagButton));
if (diagButton) {
  diagButton.click();
  await settle();
  const after = text();
  check('高级：诊断报告列出了文件与插件', after.includes('== 文件 ==') && after.includes('== 已装插件 =='));
  check('高级：诊断报告说明了日志为什么不在', after.includes('PETPC_DEBUG'));
}

check('六个页面都没有报错行', !/已装列表：|权限文案：|读取中…/.test(text()));
check('没有未捕获错误横幅', banner() === '');

let bad = 0;
for (const [name, ok] of checks) {
  console.log((ok ? 'PASS  ' : 'FAIL  ') + name);
  if (!ok) bad++;
}
rmSync(outDir, { recursive: true, force: true });
if (bad) {
  console.log('\n--- 页面文本 ---\n' + text() + '\n--- 横幅 ---\n' + banner());
  process.exit(1);
}
console.log('smoke: ' + checks.length + '/' + checks.length);
