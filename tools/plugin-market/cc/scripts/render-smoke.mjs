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

// ── the rail's two retirements ───────────────────────────────────────────────────────
//
// 「待办」 read the pet's `todos.json`, which stopped being the list when it moved into the
// todo plugin's own storage. The list is drawn by the plugin's document, on the 插件 page, so
// the absence here is the assertion - a page reading a file nobody writes is worse than none.
//
// 「专注」 and 「记录」 went the other way earlier, with the host timer (their settings and
// archive belong to the pomodoro plugin). Same treatment: the rail must not grow them back.
check(
  'rail 上没有「待办」「专注」「记录」了（列表/计时器/档案都归插件自己的界面）',
  ![...w.document.querySelectorAll('nav button')].some(
    (b) => ['待办', '专注', '记录'].includes(b.textContent.trim()),
  ),
);

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
check('插件：权限文案来自 []LabelPair', text().includes('调用你已配置的大模型（流式返回）'));
check('插件：权限计数来自 Granted（4/5）', text().includes('4/5'));

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
check('插件：init 的权限表也是纯数组', Array.isArray(init?.permissions) && init.permissions.includes('pet.surface'));
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

// A plugin that declares SURFACE blocks must be mounted here too. This is the regression
// guard for the whole round: `mountable()` used to refuse these, so the todo / pomodoro /
// chat pages showed no interface at all -- and the smoke test stayed green because the fake
// rows had no `surface`. The refusal's real cause was the MARKET not serving `plugin.share`.
await pick('番茄钟插件');
const pomoFrame = frame();
check('插件：声明了浮层面的插件也在这里挂界面（这是上一轮拒绝挂载的那个形状）',
  Boolean(pomoFrame));
check('插件：它的帧也走同一个沙箱与 URL 约定',
  pomoFrame?.getAttribute('sandbox') === 'allow-scripts' &&
    (pomoFrame?.getAttribute('src') || '') ===
      '/plugin-asset/com.shixiansi.plugin-pomodoro/panel.html?theme=dark');
check('插件：番茄钟现在有声明出来的设置项，所以有「设置」', Boolean(chip('设置')));
chip('设置')?.click();
await settle();
check('插件：番茄钟的设置里能看到「专注时长」并带出宿主的默认值',
  text().includes('专注时长') && field('focusMinutes')?.value === '25');
w.dispatchEvent(new w.KeyboardEvent('keydown', { key: 'Escape' }));
await settle();

await pick('com.example.broken');
check('插件：manifest 坏掉的行点开后说出原因', text().includes('manifest.json: 第 4 行'));

// --- the update chip ------------------------------------------------------------------
//
// A plugin's INTERFACE ships with the plugin, so an installed package that is behind shows the
// OLD behaviour while the page around it looks finished. The stub's catalogue offers 1.7.5 for a
// row installed at 1.7.4, and the voice row is current - so "an update is offered for everything"
// fails as loudly as "none is offered at all".
await pick('对话插件');
const updateChip = [...w.document.querySelectorAll('button.chip')].find((b) =>
  b.textContent.includes('更新到 v'));
check('插件：装着的版本落后时会出现「更新到 v…」（界面跟着插件走）', Boolean(updateChip));
check('插件：它指名的是目录里的那个版本', (updateChip?.textContent || '').includes('v1.7.5'));

await pick('语音插件');
check('插件：已是最新时没有「更新」按钮（一个永远不动的按钮等于装饰）',
  ![...w.document.querySelectorAll('button.chip')].some((b) => b.textContent.includes('更新到 v')));

await pick('对话插件');
[...w.document.querySelectorAll('button.chip')].find((b) => b.textContent.includes('更新到 v'))?.click();
await settle();
check('插件：点更新后给出确认，并且那一行换成了新版本', text().includes('已更新到 v1.7.5'));
check('插件：更新之后按钮自己消失了', ![...w.document.querySelectorAll('button.chip')].some((b) => b.textContent.includes('更新到 v')));

// --- the 市场 page --------------------------------------------------------------------
//
// Until now this program KNEW the catalogue but only as an update chip, so installing something
// it did not have meant opening the pet's older market window (`docs/plugin-market.md` §2.8).
// This page is that half, and a row can be in three states. By the time this runs the update
// above has already moved the chat row to current, so the stub holds two current rows and one
// package this machine does not have - which is what makes "install does nothing" distinguishable
// from "install works".
await open('市场');
const installBtn = () => [...w.document.querySelectorAll('button')].find((b) => b.textContent.trim() === '安装');
check('市场：列出源上的插件（名字与 id 都在）',
  text().includes('待办插件') && text().includes('com.shixiansi.plugin-todo'));
check('市场：插件源就是宿主给的那一条，并且可以改',
  /^https:\/\//.test(w.document.querySelector('#base')?.value || ''));
check('市场：装好的行写「已安装 v…」，三行里只有一个能装',
  text().includes('已安装 v') &&
    [...w.document.querySelectorAll('button')].filter((b) => b.textContent.trim() === '安装').length === 1);
check('市场：没有「更新到 v…」——两行都已经是目录里的版本',
  ![...w.document.querySelectorAll('button')].some((b) => b.textContent.includes('更新到 v')));

installBtn()?.click();
await settle();
check('市场：点「安装」之后那一行变成已安装', !installBtn() && text().includes('已安装 v1.2.0'));
check('市场：装完明说要去「插件」页授权（安装不等于启用）',
  text().includes('去「插件」页给它授权'));

// --- uninstalling, and the question it has to carry -----------------------------------
//
// The dialog exists because "remove the plugin" and "delete its settings, keys and data" are not
// the same size: the first is undone by installing the package again, the second is not. So the
// checks are about what a screenshot of a dialog cannot tell you: the box starts UNTICKED, and
// the wording actually changes with it. A dialog that deleted everything whatever the box said
// would pass a check that only looked for the dialog.
await open('插件');
await pick('语音插件');
const uninstallChip = () =>
  [...w.document.querySelectorAll('button.chip')].find((b) => b.textContent.trim() === '卸载');
const uninstallDialog = () => w.document.querySelector('[role="dialog"][aria-label="卸载插件"]');
const inDialog = (sel, label) =>
  [...(uninstallDialog()?.querySelectorAll(sel) ?? [])].find((el) => el.textContent.includes(label));

check('插件：有一个卸载入口', Boolean(uninstallChip()));
uninstallChip()?.click();
await settle();
check('插件：点卸载只是先问一次（插件还在）',
  Boolean(uninstallDialog()) && text().includes('同时删除它的配置与数据'));
check('插件：默认不勾，并明说只删插件本体',
  text().includes('没勾：只删插件本体，它的配置与数据留下来'));
const purgeBox = [...(uninstallDialog()?.querySelectorAll('input[type="checkbox"]') ?? [])][0];
check('插件：那个勾选框默认是空的（安全的那一半要自己选）', purgeBox?.checked === false);

purgeBox.checked = true;
purgeBox.dispatchEvent(new w.Event('change', { bubbles: true }));
await settle();
check('插件：勾上之后文案改成「不可恢复」', text().includes('不可恢复'));

inDialog('button.chip', '取消')?.click();
await settle();
check('插件：取消之后什么都没发生', !uninstallDialog() && Boolean(uninstallChip()));

// For real this time, with the box left EMPTY: the package goes, the data stays.
uninstallChip()?.click();
await settle();
inDialog('button.chip', '卸载')?.click();
await settle();
check('插件：确认后那一行没了',
  ![...w.document.querySelectorAll('nav.list .row-name')].some((el) => el.textContent.includes('语音插件')));
check('插件：并且说清配置与数据是保留的', text().includes('已卸载 语音插件（配置与数据保留）'));

// --- the 日志 page -------------------------------------------------------------------
//
// This page's whole value is that it shows the HOST's lines, so the checks are about the
// three things a screenshot cannot tell you: the levels are rendered as levels (a WARN row
// is distinguishable from an ERROR row), the filters actually reach the query, and the file
// list names the two files the two programs write. The stub APPLIES the query on purpose -
// a fake that ignored it would make a filter wired to nothing look perfect here.
await open('日志');
check('日志：行来自宿主的日志（等级 + 子系统 + 插件 + 正文四段都在）',
  text().includes('llm.chat') && text().includes('com.shixiansi.plugin-chat'));
check('日志：等级是按等级画的，不是普通文字',
  Boolean(w.document.querySelector('li.lv-warn')) && Boolean(w.document.querySelector('li.lv-error')));
check('日志：默认是「信息及以上」，调试行不占地方（但可以切到全部）',
  w.document.querySelectorAll('ol.log li').length > 0 &&
    ![...w.document.querySelectorAll('li.lv-debug')].length);
check('日志：文件列表列出两个程序各两个文件', text().includes('pet.log') && text().includes('market.log'));
check('日志：说明里写清了哪两个程序在写、以及里面没有什么',
  text().includes('PETPC_LOG_LEVEL') && text().includes('没有') && text().includes('密钥'));

// Switching the level to 全部 must bring the DEBUG row in: that is the difference between a
// <select> that is bound and one that merely exists.
const levelSelect = w.document.querySelectorAll('select')[0];
levelSelect.value = 'debug';
levelSelect.dispatchEvent(new w.Event('change', { bubbles: true }));
await settle();
check('日志：等级切到「全部」后调试行出现了', Boolean(w.document.querySelector('li.lv-debug')));

// A search that matches nothing must SAY so rather than showing an empty box.
const searchBox = w.document.querySelector('input[type="search"]');
searchBox.value = '没有这个字符串';
searchBox.dispatchEvent(new w.Event('input', { bubbles: true }));
await settle();
check('日志：搜索无结果时给出原因，而不是一块空白', text().includes('没有匹配的日志行'));
searchBox.value = '';
searchBox.dispatchEvent(new w.Event('input', { bubbles: true }));
await settle();

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
  check('高级：诊断报告带上了日志区与两个日志文件', after.includes('== 日志 ==') && after.includes('logs/pet.log'));
}

check('访问过的每一页都没有报错行', !/已装列表：|权限文案：|读取中…/.test(text()));
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
