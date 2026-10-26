/**
 * Prints the settings tab's measured layout as TEXT.
 *
 * Why this exists: the behaviour harness proves the invariants (one shared
 * control column, nothing past the panel edge, seven groups), and a screenshot
 * shows the result - but a screenshot can only be looked at by something that can
 * see. This dumps the same geometry as numbers, so the layout can be reviewed
 * from the transcript: every row's label, its x, its control's x and width, and
 * whether it wraps.
 *
 * It renders the REAL index.html and the BUILT stylesheet through headless Edge,
 * which is the same engine class WebView2 uses, and reports in CSS pixels at
 * devicePixelRatio 1, i.e. the numbers the layout actually resolves to.
 *
 * Run:  node .dev/layout-report.cjs
 */

const fs = require('fs');
const path = require('path');
const http = require('http');
const { spawnSync } = require('child_process');

const root = path.resolve(__dirname, '..');
const frontend = path.join(root, 'frontend');

const html = fs.readFileSync(path.join(frontend, 'index.html'), 'utf8');
const body = html.slice(html.indexOf('<body>') + '<body>'.length, html.indexOf('</body>'));
const markup = body.replace(/<script[\s\S]*?<\/script>/g, '');

const assetsDir = path.join(frontend, 'dist', 'assets');
const bundleCss = fs.readdirSync(assetsDir).find((f) => /^index-.*\.css$/.test(f));

const page = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <link rel="stylesheet" href="/dist/assets/${bundleCss}" />
  <style>
    #stage { width: 540px; height: 520px; }
    #panel { left: 0; top: 0; width: 380px; height: 520px; display: flex !important;
             position: absolute; }
    #tab-todos, #tab-chat, #tab-records { display: none !important; }
    #tab-settings { display: flex !important; }
  </style>
</head>
<body>
  <div id="stage">
${markup}
  </div>
  <script type="module">
   try {
    const P = (n) => Math.round(n * 10) / 10;
    const out = [];
    const panel = document.getElementById('panel');
    const tab = document.getElementById('tab-settings');
    const pRect = panel.getBoundingClientRect();

    out.push('PANEL box: ' + P(pRect.width) + 'x' + P(pRect.height) +
      '  content x ' + P(pRect.left) + '..' + P(pRect.right));
    out.push('TAB   box: ' + P(tab.clientWidth) + 'x' + P(tab.clientHeight) +
      '  scrollHeight ' + tab.scrollHeight +
      '  (scrolls: ' + (tab.scrollHeight > tab.clientHeight) + ')');
    out.push('');

    let y0 = null;
    for (const group of tab.querySelectorAll('.sgroup')) {
      const g = group.getBoundingClientRect();
      if (y0 === null) y0 = g.top;
      const head = group.querySelector('.sgroup-head');
      out.push('=== GROUP "' + head.textContent.trim() + '"  y ' + P(g.top - y0) +
        '..' + P(g.bottom - y0) + '  x ' + P(g.left) + '..' + P(g.right) +
        '  w ' + P(g.width) + (g.right > pRect.right + 0.5 ? '   *** OVERFLOWS PANEL ***' : ''));

      for (const child of group.children) {
        const r = child.getBoundingClientRect();
        const desc = child.className;
        if (child.classList.contains('sgroup-head') || child.classList.contains('sgroup-desc')) {
          out.push('    ' + desc.padEnd(14) + ' "' + child.textContent.trim().slice(0, 34) + '"');
          continue;
        }
        if (child.classList.contains('ssub')) {
          out.push('    ' + desc.padEnd(14) + ' "' + child.textContent.trim() + '"');
          continue;
        }
        if (child.classList.contains('row')) {
          const cb = child.querySelector('input');
          const txt = child.querySelector('span');
          out.push('    row            box y' + P(r.top - y0) + ' x' + P(r.left) +
            '..' + P(r.right) +
            '  cb@' + P(cb.getBoundingClientRect().left) +
            ' text@' + P(txt.getBoundingClientRect().left) +
            (r.right > pRect.right + 0.5 ? '  *** OVERFLOWS ***' : '') +
            '  "' + txt.textContent.trim() + '"');
          continue;
        }
        if (child.classList.contains('field')) {
          const label = child.querySelector('.field-label');
          const val = child.querySelector('.field-input');
          if (!label || !val) {
            out.push('    field          *** no .field-label or .field-input: ' + desc);
            continue;
          }
          const lr = label.getBoundingClientRect();
          const vr = val.getBoundingClientRect();
          const ctrl = val.querySelector('input, select, textarea, button');
          // A row "wraps" when its value box is taller than one control line.
          const oneLine = ctrl ? ctrl.getBoundingClientRect().height : 0;
          out.push('    field          box y' + P(r.top - y0) + ' x' + P(r.left) +
            '..' + P(r.right) +
            '  label@' + P(lr.left) + ' w' + P(lr.width) +
            '  value@' + P(vr.left) + ' w' + P(vr.width) +
            (vr.height > oneLine + 12 ? ' [stacked ' + P(vr.height) + 'px]' : '') +
            (r.right > pRect.right + 0.5 ? '  *** OVERFLOWS ***' : ''));
          out.push('                   label="' + label.textContent.trim() + '"' +
            '  controls: ' + [...val.querySelectorAll('input,select,textarea,button,span')]
              .map((e) => {
                const rr = e.getBoundingClientRect();
                const tag = e.tagName.toLowerCase();
                const name = e.id || (e.textContent || '').trim().slice(0, 12);
                return tag + (name ? ':' + name : '') + '(x' + P(rr.left) + ',w' + P(rr.width) +
                  ',h' + P(rr.height) + ')';
              }).join(' '));
          continue;
        }
        out.push('    ' + (desc || child.tagName).padEnd(14) + ' y' + P(r.top - y0) +
          ' h' + P(r.height) + ' x' + P(r.left) + '..' + P(r.right) +
          (r.right > pRect.right + 0.5 ? '  *** OVERFLOWS ***' : ''));
      }
      out.push('');
    }

    out.push('TOTAL form height: ' + P(tab.scrollHeight) + 'px in a ' +
      P(tab.clientHeight) + 'px viewport (' +
      Math.round((tab.scrollHeight / tab.clientHeight) * 10) / 10 + ' screens)');

    const el = document.createElement('pre');
    el.id = 'layout-report';
    el.textContent = out.join('\\n');
    document.body.appendChild(el);
    document.title = 'report ready';
    // Posted back over HTTP rather than printed, because a headless browser's
    // stdout cannot be captured by the parent in this environment (the spawn is
    // blocked, and --dump-dom needs the process to exit cleanly). Same channel the
    // behaviour harness uses.
    await fetch('/result', { method: 'POST', body: out.join('\\n') });
   } catch (err) {
    // A thrown error inside a module would otherwise be silent and the harness
    // would just time out with no explanation.
    await fetch('/result', { method: 'POST', body: 'REPORT FAILED: ' + (err && err.stack || err) });
   }
  </script>
</body>
</html>`;

const built = path.join(__dirname, 'layout-report.html');
fs.writeFileSync(built, page, 'utf8');

const MIME = { '.html': 'text/html', '.css': 'text/css', '.js': 'text/javascript', '.svg': 'image/svg+xml' };
let resolveResult;
const resultArrived = new Promise((r) => { resolveResult = r; });

const server = http.createServer((req, res) => {
  if (req.method === 'POST' && req.url === '/result') {
    let body = '';
    req.on('data', (d) => { body += d; });
    req.on('end', () => {
      res.writeHead(200);
      res.end('ok');
      resolveResult(body);
    });
    return;
  }
  const url = req.url.split('?')[0];
  const file = url === '/' ? built : path.join(frontend, url);
  if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) {
    res.writeHead(404);
    res.end('not found');
    return;
  }
  res.writeHead(200, { 'Content-Type': MIME[path.extname(file)] || 'application/octet-stream' });
  res.end(fs.readFileSync(file));
});

const edge = [
  'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
  'C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe',
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
].find((p) => fs.existsSync(p));
if (!edge) {
  console.error('no headless browser found');
  process.exit(2);
}

server.listen(0, () => {
  const url = `http://127.0.0.1:${server.address().port}/`;
  const child = require('child_process').spawn(
    edge,
    [
      '--headless=old', '--disable-gpu', '--no-sandbox', '--no-first-run', '--disable-sync',
      `--user-data-dir=${path.join(__dirname, 'report-profile')}`,
      '--virtual-time-budget=8000',
      url,
    ],
    { stdio: 'ignore' },
  );

  const timeout = new Promise((r) => setTimeout(() => r(null), 60000));
  Promise.race([resultArrived, timeout]).then((report) => {
    try { child.kill(); } catch { /* already gone */ }
    server.close();
    if (report === null) {
      console.error('the page never reported back');
      process.exit(2);
    }
    console.log(report);
  });
});
