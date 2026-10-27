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
check('开始：图片卡在，且说得出当前用的是默认图片', text().includes('更换图片') && text().includes('默认图片'));

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

// --- the plugin page, which is a LIST + a DETAIL AREA now -----------------------------
//
// Two things about this group. First, the page is master-detail, so "the page prints the
// host's permission labels" only becomes true after a row is picked - the permutation the
// page went through is what these checks follow. Second, three of them are not about
// cosmetics at all: the frame's sandbox, its URL, and "a disabled plugin gets no frame" are
// the invariants that a future edit to this page can silently break.
const frame = () => w.document.querySelector('iframe.plugin-frame');
async function pick(text_) {
  const row = [...w.document.querySelectorAll('.list button.row')].find((b) => b.textContent.includes(text_));
  if (!row) throw new Error('插件列表里没有「' + text_ + '」这一行');
  row.click();
  await settle();
}
const chip = (label) => [...w.document.querySelectorAll('button.chip')].find((b) => b.textContent.includes(label));

await open('插件');
check('插件：列表里的名字来自 plugins.Installed.Manifest.Name', text().includes('对话插件') && text().includes('语音插件'));
check('插件：每一行都说得出名字（字段名对得上）',
  [...w.document.querySelectorAll('.list .row-name')].every((el) => el.textContent.trim().length > 0));
check('插件：没选中时给出提示，而不是一块空框', text().includes('从左边选一个插件'));

await pick('对话插件');
// The id is read from `ID` (not `id`) on the identity line. Until this row is picked it is
// only in the row's `title`, which `textContent` does not carry - which is why this assertion
// lives AFTER the click rather than beside the list ones.
check('插件：选中的 id 来自 ID', text().includes('com.shixiansi.plugin-chat'));
const chatFrame = frame();
check('插件：选中后挂的是一个帧', Boolean(chatFrame));
check('插件：帧的 sandbox 恰好是 allow-scripts（没有 allow-same-origin）',
  chatFrame?.getAttribute('sandbox') === 'allow-scripts');
check('插件：帧的地址是 /plugin-asset/<id>/<entry>?theme=',
  (chatFrame?.getAttribute('src') || '') === '/plugin-asset/com.shixiansi.plugin-chat/index.html?theme=dark');
check('插件：权限文案来自 []LabelPair', text().includes('读取待办列表'));
check('插件：权限计数来自 Granted（6/7）', text().includes('6/7'));

// --- THE HANDSHAKE, simulated ---------------------------------------------------------
//
// This group exists because of a bug class that every other check here is blind to: the host
// answers the frame's `ready` with ONE `init`, and if that payload holds anything that cannot
// be structured-cloned the exception is thrown INSIDE the message listener. The page looks
// perfect, the plugin sits on 「等待宿主初始化…」, and the console has one line nobody reads.
//
// The real case: the rows live in a Svelte `$state` array, which DEEP-PROXIES what it holds, so
// `Manifest.permissions` / `Granted` are Proxies - and a Proxy cannot cross `postMessage`
// ("Uncaught DataCloneError: [object Object] could not be cloned"). Found on the real window,
// after this script had already reported 36/36 green.
//
// jsdom is enough to pin it: dispatch a `ready` with the frame's own `contentWindow` as its
// source (the host rejects anything else), capture what the host posts back, and require that
// Node's structuredClone can clone it.
const frameWin = chatFrame?.contentWindow;
const sent = [];
if (frameWin) {
  frameWin.postMessage = (msg) => { sent.push(msg); };
  w.dispatchEvent(new w.MessageEvent('message', { data: { v: 1, type: 'ready' }, source: frameWin }));
  await settle();
}
const init = sent.find((m) => m && m.type === 'init');
check('插件：收到 ready 后回了恰好一条 init', sent.filter((m) => m && m.type === 'init').length === 1);
check('插件：init 的载荷能过结构化克隆（$state 的 Proxy 过不去）', (() => {
  if (!init) return false;
  try {
    globalThis.structuredClone(init);
    return true;
  } catch {
    return false;
  }
})());
check('插件：init 带的是宿主的授权集合（Granted，大写 G），不是空数组',
  Array.isArray(init?.granted) && init.granted.includes('llm.chat'));
check('插件：init 说明自己在 workshop 槽位', init?.surface?.kind === 'panel' && init?.surface?.slot === 'workshop');
check('插件：init 的权限表也是纯数组', Array.isArray(init?.permissions) && init.permissions.includes('todos.write'));
// A second `ready` must not produce a second init (the grant list would be applied twice).
if (frameWin) {
  w.dispatchEvent(new w.MessageEvent('message', { data: { v: 1, type: 'ready' }, source: frameWin }));
  await settle();
}
check('插件：第二条 ready 不会再回一条 init', sent.filter((m) => m && m.type === 'init').length === 1);

// --- the settings dialog (manifest.config) --------------------------------------------
//
// The marketplace window's oldest feature after the list itself, and the one this page lost
// when it was rewritten: a plugin's DECLARED settings (a model name, a temperature, an API
// key) had no editor at all. What is worth asserting is not "a dialog appears" but the rules:
// every control type draws from the declaration, a secret starts blank and is never backfilled
// with its mask, and a value that was saved comes back on the next open.
const dialog = () => w.document.querySelector('.sheet');
const field = (key) => w.document.getElementById('cfg-lab-' + key)?.parentElement?.querySelector('input, select, textarea');
const sheetButton = (label) =>
  [...w.document.querySelectorAll('.sheet-foot button')].find((b) => b.textContent.includes(label));

check('插件：声明了设置项的插件才有「设置」按钮', Boolean(chip('设置')));
chip('设置')?.click();
await settle();
check('插件：设置弹窗打开了', Boolean(dialog()));
check('插件：文本项带出宿主存的值', field('model')?.value === 'deepseek-chat');
check('插件：数字项带出宿主存的值', field('temperature')?.value === '0.8');
check('插件：布尔项按已存值勾上', field('stream')?.checked === true);
// The state is read off the PROPERTY, not the attribute: Svelte sets `checked`/`value` as
// properties, so a `:checked` selector (and `innerHTML`) can disagree with what the user sees.
check('插件：多选按逗号串复原勾选',
  [...w.document.querySelectorAll('.checks input')].filter((i) => i.checked).map((i) => i.value).join(',') === 'deepseek');
check('插件：密钥项永远是空的，占位是宿主的掩码',
  field('apiKey')?.value === '' && field('apiKey')?.placeholder === '••••a1b2');
check('插件：密钥项下面写明「留空保持不变」', text().includes('留空保持不变'));
check('插件：用法行来自宿主', text().includes('今天已用 3 / 200 次'));
check('插件：打开设置不会重挂插件帧', frame() === chatFrame);

const modelField = field('model');
modelField.value = 'deepseek-reasoner';
modelField.dispatchEvent(new w.Event('input', { bubbles: true }));
sheetButton('保存')?.click();
await settle();
check('插件：保存后弹窗关闭', !dialog());
check('插件：保存后给出确认', text().includes('设置已保存'));

chip('设置')?.click();
await settle();
check('插件：重新打开时读到刚保存的值', field('model')?.value === 'deepseek-reasoner');
w.dispatchEvent(new w.KeyboardEvent('keydown', { key: 'Escape' }));
await settle();
check('插件：Esc 关掉设置弹窗', !dialog());

// Expanding folds the LIST away so the plugin's document gets the page. The assertion is not
// "the class changed" - it is that the SAME iframe node survived: a remount would destroy the
// document the user is working in (see openspec/changes/plugin-page-workspace/design.md).
chip('展开界面')?.click();
await settle();
check('插件：展开把列表收起来', Boolean(w.document.querySelector('.page.expanded')));
check('插件：展开不重挂帧（还是同一个 iframe 节点）', frame() === chatFrame);
chip('收起列表')?.click();
await settle();

await pick('语音插件');
check('插件：换插件后权限文案仍来自宿主', text().includes('保存并播放音频'));
check('插件：停用的插件不挂帧，并说出原因', !frame() && text().includes('插件已停用'));
check('插件：停用那一行的权限计数是 0/3', text().includes('0/3'));
// No declared settings, no button: a 设置 that opens an empty form is a control that lies.
check('插件：没有声明设置项的插件不出现「设置」', !chip('设置'));

await pick('com.example.broken');
check('插件：manifest 坏掉的行点开后说出原因', text().includes('manifest.json: 第 4 行'));

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
