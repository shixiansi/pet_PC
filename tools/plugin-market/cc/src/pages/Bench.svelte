<script lang="ts">
  /*
   * 扩展能力 —— 插件测试台：把已装的插件挂到**它真正会待的那几种位置**上，看它画出什么、
   * 拿到哪些授权、以及和宿主之间到底交换了哪些消息。
   *
   * 为什么要有这一页（而不是"去看插件页"）：插件页回答的是「这个插件长什么样」，
   * 而这一页回答的是「它到了别处会怎样」。三件事在插件页上做不到：
   *
   *   1. **位置**。同一份文档在桌宠里是浮层面（有矩形、被原生区域裁）、是常驻帧（隐藏、
   *      没有矩形）、在控制中心是铺满的工作区。`init.surface` 就是它用来判断"我在哪"的
   *      那一句，而插件页永远只说 `panel/workshop` 一种。
   *   2. **授权被拒之后**。用户什么都不勾时界面是否还立得住，是插件作者最该看一眼的一屏，
   *      而插件页只有"真的改授权"一条路——那会改动闸门对这个世界生效的东西。
   *   3. **看不见的那一半**。握手、请求、响应、宿主推下来的事件，在插件页上一行都看不到。
   *
   * 它刻意**不是**第二个宿主：帧仍然由 `lib/plugin-frame.ts` 建（沙箱恰为 `allow-scripts`，
   * 请求仍然走 `Market.PluginRequest` 那道闸门）。这一页只多看两样东西——每一条过边界的消息，
   * 和一个"模拟未授权"的开关。
   *
   * 它也不假装能替掉 `plugin-testkit/preview.mjs`：沙箱帧是 opaque origin，本页**读不到**
   * 文档的 `getBoundingClientRect()`，"按钮被下边缘切掉"这种事在这里看不见。排版检查仍然只有
   * 那份真浏览器工具做得到（market/docs/plugin-testkit/），这一页量的是**框**，不是内容。
   */
  import { untrack } from 'svelte';
  import { Installed, PermissionLabels, PluginHostInfo, PluginRequest } from '../lib/bridge';
  import type { PluginRow } from '../lib/bridge';
  import {
    mountPlugin,
    windowTheme,
    type BoundaryEvent,
    type MountSurface,
    type PluginFrame,
  } from '../lib/plugin-frame';

  type Place = 'workshop' | 'background' | 'surface' | 'body';

  let rows = $state<PluginRow[]>([]);
  let labels = $state<Record<string, string>>({});
  let host = $state<{ program: string; methods: string[] } | null>(null);
  let loaded = $state(false);
  let loadErr = $state('');

  let pluginId = $state('');
  let place = $state<Place>('workshop');
  let rectIndex = $state(0);
  // The box the frame is drawn at. They are also what `init.surface.rect` reports, which is why
  // they are part of the mount key: a document told "you are 380 wide" while sitting in a 500px
  // box is being lied to, and a bench that lies is worse than no bench.
  let width = $state(380);
  let height = $state(260);
  let scale = $state(1);
  let deny = $state(false);
  let tick = $state(0);

  let logRows = $state<BoundaryEvent[]>([]);
  let mount = $state<HTMLDivElement | null>(null);
  // Not `$state`: the handle is replaced on every remount, and nothing in the markup depends on
  // its identity (the effect owns its lifetime).
  let frame: PluginFrame | null = null;

  let eventName = $state('share.changed');
  let eventPayload = $state('{"key":"command","owner":"com.shixiansi.plugin-todo","name":"todos"}');
  let eventNote = $state('');

  let methodName = $state('storage.get');
  let methodPayload = $state('{"key":"state"}');
  let methodResult = $state('');

  void boot();

  async function boot(): Promise<void> {
    try {
      const [list, pairs] = await Promise.all([Installed(), PermissionLabels()]);
      rows = list;
      labels = Object.fromEntries(pairs.map((p) => [p.ID, p.Label]));
      const first = mountable(list)[0];
      if (first) pick(first.ID);
      // Best effort, and deliberately not fatal: `init.host` is optional, and in the browser
      // there is no Go-side table to read (bridge.ts rejects on purpose rather than inventing a
      // copy that would drift from the gate's answer).
      void PluginHostInfo()
        .then((info) => {
          host = { program: info.program, methods: [...info.methods] };
        })
        .catch(() => {
          host = null;
        });
    } catch (e) {
      loadErr = '读不到已装插件：' + why(e);
    } finally {
      loaded = true;
    }
  }

  /** Plugins that can be mounted at all: enabled, manifest parsed, ships a document. */
  function mountable(list: PluginRow[]): PluginRow[] {
    return list.filter((r) => r.Enabled && Boolean(r.Manifest) && Boolean(r.Manifest?.entry));
  }

  /** Everything that cannot be mounted, with the reason the HOST would give, not a guess. */
  function skipped(list: PluginRow[]): Array<{ row: PluginRow; why: string }> {
    const out: Array<{ row: PluginRow; why: string }> = [];
    for (const r of list) {
      if (mountable([r]).length > 0) continue;
      if (!r.Manifest) out.push({ row: r, why: 'manifest 读不出来：' + (r.Error || '原因没写') });
      else if (!r.Enabled) out.push({ row: r, why: '已停用——停用的插件连文件都不发（/plugin-asset 回 403），挂不上' });
      else out.push({ row: r, why: '清单里没有 entry，没有文档可挂' });
    }
    return out;
  }

  const candidates = $derived(mountable(rows));
  const skippedRows = $derived(skipped(rows));
  const selected = $derived(rows.find((r) => r.ID === pluginId) ?? null);
  const rects = $derived(selected?.Manifest?.surface ?? []);
  const hasBackground = $derived(Boolean(selected?.Manifest?.background));
  // 画桌宠本体（manifest 的 body）。它只在自己的那一格里被挂一次，值不值得单列一个位置，
  // 理由见下面的 choosePlace。
  const hasBody = $derived(Boolean(selected?.Manifest?.body));
  const grantedCount = $derived((selected?.Granted ?? []).length);
  const permCount = $derived((selected?.Manifest?.permissions ?? []).length);

  /**
   * What decides a mount, as ONE string - the same trick PluginFrame.svelte uses, for the same
   * reason: `refresh()`-style reloads hand the page a new row object each time, and an effect that
   * read the row directly would tear the frame down for no visible change.
   *
   * `deny` and the box are part of it because both are carried by `init`: flipping either one has
   * to re-handshake, and a plugin that kept its old grant list would make the switch a lie.
   */
  const mountKey = $derived.by(() => {
    const row = selected;
    const entry = row?.Manifest?.entry ?? '';
    if (!row || !entry) return '';
    const where = place === 'surface' ? 'surface:' + rectIndex : place;
    const box = place === 'surface' || place === 'body' ? width + 'x' + height : 'fill';
    return [row.ID, row.Version || '', entry, where, box, deny ? 'deny' : 'grant', tick].join('|');
  });

  $effect(() => {
    const target = mount;
    const which = mountKey;
    if (!target || !which) return;
    const handle = untrack(() => {
      const row = selected;
      if (!row) return null;
      return mountPlugin(target, requestFor(row), windowTheme());
    });
    if (!handle) return;
    frame = handle;
    return () => {
      frame = null;
      handle.destroy();
    };
  });

  /** The installed row, turned into a mount request. Called inside `untrack` only. */
  function requestFor(row: PluginRow) {
    return {
      pluginId: row.ID,
      version: row.Version || '',
      entry: row.Manifest?.entry ?? '',
      permissions: row.Manifest?.permissions ?? [],
      granted: row.Granted ?? [],
      surface: surfaceNow(),
      denyRequests: deny,
      onMessage: push,
    };
  }

  /** Where the document is told it landed. Plain primitives only - this crosses `postMessage`. */
  function surfaceNow(): MountSurface {
    if (place === 'body') {
      // The pet's own rectangle: ONE box, no declaration to index into, and `x`/`y` are 0,0
      // because the frame IS the rectangle (see docs/plugin-dev.md §6.5).
      return {
        kind: 'body',
        slot: 'body',
        rectIndex: 0,
        rectCount: 1,
        rect: { dx: 0, dy: 0, w: width, h: height },
      };
    }
    if (place === 'surface') {
      const rect = rects[rectIndex];
      return {
        kind: 'surface',
        rectIndex,
        rectCount: rects.length,
        rect: {
          dx: Number(rect?.dx ?? 0) || 0,
          dy: Number(rect?.dy ?? 0) || 0,
          w: width,
          h: height,
          ...(rect?.radius === undefined ? {} : { radius: Number(rect.radius) || 0 }),
        },
      };
    }
    return { kind: 'panel', slot: place === 'background' ? 'background' : 'workshop' };
  }

  /** Newest first, capped: a plugin that requests in a loop must not grow this page forever. */
  function push(ev: BoundaryEvent): void {
    logRows = [ev, ...logRows].slice(0, 200);
  }

  function pick(id: string): void {
    pluginId = id;
    const row = rows.find((r) => r.ID === id) ?? null;
    choosePlace(row);
  }

  /** Resets the position to the most useful one this plugin actually has (see below). */
  function choosePlace(row: PluginRow | null): void {
    const list = row?.Manifest?.surface ?? [];
    if (list.length > 0) {
      // A SURFACE plugin is mounted as a rectangle in the pet, and the rectangle is the thing
      // worth looking at - it is where "被下边缘切掉" happens. Defaulting to the workshop instead
      // would show the same document in a box it was never measured against.
      place = 'surface';
      chooseRect(0);
      return;
    }
    // 画本体的插件：它唯一的那个落点就是宠物那一格，而且尺寸随用户设置（桌宠大小）变。默认
    // 按 1.0 倍宠物的 150×200 摆，再让作者自己改。
    if (row?.Manifest?.body) {
      place = 'body';
      width = 150;
      height = 200;
      return;
    }
    // No rectangles: the hidden常驻帧 is the honest place for a plugin that declares one, and
    // the workshop for everything else. Both are "fill", i.e. nothing to measure.
    place = row?.Manifest?.background ? 'background' : 'workshop';
  }

  function chooseRect(i: number): void {
    rectIndex = i;
    const rect = rects[i];
    if (!rect) return;
    width = clampSize(rect.w);
    height = clampSize(rect.h);
  }

  /** A 0-wide frame renders nothing and a 100000-wide one hangs the window; both are typos. */
  function clampSize(n: unknown): number {
    const v = Math.round(Number(n));
    if (!Number.isFinite(v)) return 80;
    return Math.max(80, Math.min(2000, v));
  }

  function commitSize(axis: 'w' | 'h', e: Event): void {
    const input = e.currentTarget as HTMLInputElement;
    const next = clampSize(input.value);
    input.value = String(next);
    if (axis === 'w') width = next;
    else height = next;
  }

  function remount(): void {
    tick += 1;
  }

  function clearLog(): void {
    logRows = [];
  }

  function sendEvent(): void {
    const name = eventName.trim();
    if (!name) {
      eventNote = '先写一个事件名';
      return;
    }
    if (!frame) {
      eventNote = '现在没有挂上的帧';
      return;
    }
    let payload: unknown = {};
    const raw = eventPayload.trim();
    if (raw !== '') {
      try {
        payload = JSON.parse(raw);
      } catch {
        eventNote = '载荷不是合法 JSON，没有发送';
        return;
      }
    }
    frame.sendEvent(name, payload);
    eventNote = '已推送 ' + name;
  }

  function useKnownEvent(name: string, payload: string): void {
    eventName = name;
    eventPayload = payload;
    eventNote = '';
  }

  /**
   * Calls one method AS the plugin, through the same gate the frame uses.
   *
   * It is not a back door: `Market.PluginRequest` refuses exactly what it would refuse for the
   * document (unknown method here, not granted, plugin off). What it buys is the one thing a
   * plugin's own UI cannot give you - "call `share.set` and show me the raw answer" without
   * hunting for the button that does it.
   */
  async function runMethod(): Promise<void> {
    if (!selected) return;
    const method = methodName.trim();
    if (!method) {
      methodResult = '先写一个方法名';
      return;
    }
    methodResult = '调用中…';
    try {
      const raw = await PluginRequest(selected.ID, method, methodPayload.trim() || '{}');
      methodResult = raw === '' || raw == null ? '（这个方法没有返回值）' : raw;
    } catch (e) {
      methodResult = '失败：' + why(e);
    }
  }

  function why(e: unknown): string {
    if (e instanceof Error) return e.message;
    if (typeof e === 'string') return e;
    try {
      return JSON.stringify(e);
    } catch {
      return String(e);
    }
  }

  const label = (perm: string): string => labels[perm] || perm;
  const displayName = (r: PluginRow): string => r.Manifest?.name || r.ID;

  const placeOptions = $derived.by(() => {
    const out: Array<[Place, string]> = [['workshop', '控制中心 · 工作区（铺满）']];
    if (hasBackground) out.push(['background', '桌宠 · 常驻帧（隐藏，没有矩形）']);
    if (rects.length > 0) {
      out.push(['surface', '桌宠 · 浮层面（' + rects.length + ' 块声明矩形）']);
    }
    if (hasBody) out.push(['body', '桌宠 · 本体（宠物那一格）']);
    return out;
  });
</script>

<div class="page">
  <section class="bar">
    <p class="stamp">
      测试台 · v1
      <span class="dim">挂的是这台机器上真在跑的那份文档，不是另一份假宿主</span>
    </p>

    <label class="field">
      <span>插件</span>
      <select class="plugin-select" value={pluginId} onchange={(e) => pick(e.currentTarget.value)}>
        {#if !loaded}
          <option value="">读取中…</option>
        {:else if candidates.length === 0}
          <option value="">没有可挂的插件</option>
        {/if}
        {#each candidates as r (r.ID)}
          <option value={r.ID}>{displayName(r)} · v{r.Version || '?'}</option>
        {/each}
      </select>
    </label>

    <label class="field">
      <span>位置</span>
      <select class="place-select" value={place} onchange={(e) => {
        place = e.currentTarget.value as Place;
        if (place === 'surface') chooseRect(rectIndex);
      }}>
        {#each placeOptions as [value, text] (value)}
          <option {value}>{text}</option>
        {/each}
      </select>
    </label>

    {#if place === 'surface'}
      <label class="field">
        <span>矩形</span>
        <select class="rect-select" value={String(rectIndex)} onchange={(e) => chooseRect(Number(e.currentTarget.value))}>
          {#each rects as r, i (i)}
            <option value={String(i)}>第 {i + 1} 块 · {r.w}×{r.h}</option>
          {/each}
        </select>
      </label>
    {/if}

    {#if place === 'surface' || place === 'body'}
      <label class="field size">
        <span>宽 × 高</span>
        <span class="pair">
          <input
            type="number"
            class="w"
            value={width}
            min="80"
            max="2000"
            onchange={(e) => commitSize('w', e)}
          />
          <input
            type="number"
            class="h"
            value={height}
            min="80"
            max="2000"
            onchange={(e) => commitSize('h', e)}
          />
        </span>
      </label>

      <div class="field">
        <span>缩放</span>
        <div class="segmented" role="group" aria-label="缩放">
          {#each [1, 0.75, 0.5] as s (s)}
            <button
              type="button"
              class:is-active={scale === s}
              aria-pressed={scale === s}
              onclick={() => (scale = s)}
            >{Math.round(s * 100)}%</button>
          {/each}
        </div>
      </div>
    {/if}

    <label class="switch" title="把 init 的 granted 变成空的，并且每个请求都在这一层被拒绝——真实的授权没有被改动">
      <input type="checkbox" class="deny" bind:checked={deny} />
      <span>模拟未授权</span>
    </label>

    <button type="button" class="btn" onclick={remount} title="销毁这个文档再挂一次（新的握手）">重挂</button>
    <button type="button" class="btn" onclick={clearLog} title="清空消息日志">清空日志</button>
  </section>

  <div class="split">
    <section class="stage-col">
      {#if !loaded}
        <p class="hint">读取中…</p>
      {:else if !selected}
        <p class="hint">
          {#if loadErr}
            {loadErr}
          {:else}
            没有可挂的插件：装一个、并且启用它，测试台才有东西可以挂。
          {/if}
        </p>
      {:else}
        <header class="stage-head">
          <span class="who">{displayName(selected)} <span class="dim">v{selected.Version || '?'}</span></span>
          <span class="dim mono">{selected.ID}</span>
          <span class="pill" class:bad={deny}>
            授权 {deny ? '0' : grantedCount}/{permCount}{deny ? ' · 模拟未授权' : ''}
          </span>
        </header>

        {#if place === 'background'}
          <div class="hidden-note">
            常驻帧在桌宠里是 <code>display:none</code> 的：它没有可见区域，只负责在后台跑。
            这里也一样——你看不见它，旁边那条消息日志就是它在跑的证据。
          </div>
          <div class="mount hidden-mount" bind:this={mount}></div>
        {:else if place === 'surface' || place === 'body'}
          <div class="canvas">
            <div class="card" style="width:{width * scale}px;height:{height * scale}px">
              <div
                class="mount"
                bind:this={mount}
                style="width:{width}px;height:{height}px;transform:scale({scale});transform-origin:top left"
              ></div>
            </div>
            <p class="caption">
              {width} × {height}{scale === 1 ? '' : '（缩小到 ' + Math.round(scale * 100) + '% 显示）'} ·
              {place === 'body'
                ? '桌宠那一格：这儿画满、透明，而且这一层在桌宠上不接指针事件'
                : '虚线是宿主会交给 SetWindowRgn 的那个包围盒'}
            </p>
          </div>
        {:else}
          <div class="canvas fill">
            <div class="mount fill" bind:this={mount}></div>
          </div>
        {/if}

        <p class="hint">
          {#if place === 'surface'}
            清单里这一块声明在 {rects[rectIndex]?.dx ?? 0}, {rects[rectIndex]?.dy ?? 0}，圆角
            {rects[rectIndex]?.radius ?? 0}px。改成别的尺寸会重新握手——尺寸在 <code>init.surface.rect</code>
            里，改它而不重挂，插件就是在被告诉一个假数字。
          {:else if place === 'body'}
            桌宠只在插件声明 <code>body: true</code> 且持有 <code>pet.surface</code> 时才挂这一格；
            上面这块就是宠物的大小（用户能把桌宠调到 0.5×–3×）。两件事在这里看不见：它下面的桌面，
            以及**它不接任何指针**——拖动、右键菜单、悬停按钮都是宿主的。
          {:else if place === 'background'}
            桌宠只在插件声明 <code>background: true</code> 且持有 <code>events</code> / <code>net.ws</code> /
            <code>net.fetch</code> / <code>plugin.call</code> 之一时才挂这个帧；这里只按清单判断，不检查授权
            （那是闸门的事）。
          {:else}
            控制中心是普通窗口，帧铺满这块画布——这里没有"被裁掉"这件事，所以排版别在这儿量。
          {/if}
        </p>

        {#if skippedRows.length > 0}
          <ul class="skipped">
            {#each skippedRows as s (s.row.ID)}
              <li><b>{displayName(s.row)}</b> <span class="dim mono">v{s.row.Version || '?'}</span>：{s.why}</li>
            {/each}
          </ul>
        {/if}

        <!-- The grant list the frame is being told about. Labels come from the host
             (`Market.PermissionLabels`), not from a copy - a bench that invented its own wording
             would be testing its own vocabulary instead of the one the user was shown when they
             granted these. -->
        <details class="perms">
          <summary>
            声明的权限（闸门里已授 {grantedCount}/{permCount}{deny ? ' · 这个帧被告知 0 项' : ''}）
          </summary>
          {#if permCount === 0}
            <p class="hint">这个插件没有声明任何权限。</p>
          {:else}
            <ul>
              {#each selected.Manifest?.permissions ?? [] as perm (perm)}
                <li class:on={!deny && (selected.Granted ?? []).includes(perm)}>
                  <span class="mark">{!deny && (selected.Granted ?? []).includes(perm) ? '✓' : '✗'}</span>
                  <code>{perm}</code>
                  <span class="dim">{label(perm)}</span>
                </li>
              {/each}
            </ul>
            <p class="hint">
              {deny
                ? '「模拟未授权」开着：帧里拿到的 granted 是空的，所以下面每一项对它都不存在——而闸门里的真实授权一个字节都没改。'
                : '这份清单就是 init 里告诉插件的那一份。要改真实授权，去「插件」页——那才是闸门看的东西。'}
            </p>
          {/if}
        </details>
      {/if}
    </section>

    <aside class="side">
      <div class="side-head">
        <h2>过边界的消息</h2>
        <span class="count">{logRows.length}</span>
      </div>
      {#if logRows.length === 0}
        <p class="hint">还没有消息。插件一进来会先报 <code>ready</code>，宿主回一条 <code>init</code>。</p>
      {:else}
        <ol class="log" aria-label="插件与宿主之间的消息">
          {#each logRows as m, i (i + m.at + m.type)}
            <li class:in={m.dir === 'in'} class:out={m.dir === 'out'} class:bad={m.ok === false}>
              <span class="at">{m.at}</span>
              <span class="dir">{m.dir === 'in' ? '←' : '→'}</span>
              <span class="type">{m.type}</span>
              <span class="text">{m.text}</span>
            </li>
          {/each}
        </ol>
      {/if}
      <p class="hint">
        ← 是插件说的，→ 是宿主说的。请求走的是同一个 <code>Market.PluginRequest</code>，
        所以「本程序不支持该方法」「未获权限」这些拒绝会原样出现在这里。
      </p>

      <details class="tool">
        <summary>推一条事件给插件</summary>
        <label class="field">
          <span>事件名</span>
          <input type="text" bind:value={eventName} />
        </label>
        <label class="field">
          <span>载荷（JSON）</span>
          <textarea rows="3" bind:value={eventPayload}></textarea>
        </label>
        <div class="chips">
          <button type="button" class="chip" onclick={() => useKnownEvent('share.changed', '{"key":"command","owner":"com.shixiansi.plugin-todo","name":"todos"}')}>share.changed</button>
          <button type="button" class="chip" onclick={() => useKnownEvent('storage.changed', '{"key":"state"}')}>storage.changed</button>
          <button type="button" class="chip" onclick={() => useKnownEvent('config.changed', '{"pluginId":"' + pluginId + '"}')}>config.changed</button>
        </div>
        <button type="button" class="btn event-send" onclick={sendEvent}>发送</button>
        {#if eventNote}<span class="note">{eventNote}</span>{/if}
        <p class="hint">
          这三个名字是宿主真的会推的（手册第 7 节）。插件收到的形状与真机一致——
          它分不出这是你手推的还是桌宠推的，这正是这一栏的用处。
        </p>
      </details>

      <details class="tool">
        <summary>以插件身份调用一个方法</summary>
        <label class="field">
          <span>方法</span>
          <input type="text" bind:value={methodName} />
        </label>
        <label class="field">
          <span>载荷（JSON）</span>
          <textarea rows="3" bind:value={methodPayload}></textarea>
        </label>
        <button type="button" class="btn method-send" onclick={() => void runMethod()}>调用</button>
        {#if methodResult}<pre class="result">{methodResult}</pre>{/if}
        <p class="hint">
          走的是同一道闸门，不是后门：本程序不服务的方法、没授权的、插件停用了的，都会照样被拒。
        </p>
      </details>

      {#if host}
        <p class="hint">
          本窗口是 <code>{host.program}</code>，服务 {host.methods.length} 个方法（
          <code>init.host</code> 里给插件的就是这一份）。桌宠那一半——
          <code>bubble.say</code> / <code>surface.*</code> / <code>todos.*</code> / <code>timer.*</code> /
          <code>llm.chat</code> / <code>events.subscribe</code>——在这里一律是拒绝。
        </p>
      {/if}

      <p class="hint last">
        看不到"元素被下边缘切掉"？那件事在这里量不了：沙箱帧是 opaque origin，
        本页读不到它的 DOM。排版越界要 <code>node market/docs/plugin-testkit/preview.mjs &lt;插件目录&gt; --check</code>。
        这一页量的是**框**：它是不是清单声明的那一块、插件在这个框里握手时被告知了什么。
      </p>
    </aside>
  </div>
</div>

<style>
  .page {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    gap: var(--space-3);
    height: 100%;
    min-height: 0;
  }

  /* --- 控制条 --- */
  .bar {
    display: flex;
    align-items: flex-end;
    gap: var(--space-3);
    flex-wrap: wrap;
    padding: var(--space-3) var(--space-4);
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    box-shadow: var(--sh-xs);
  }
  .stamp {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 0 var(--space-2) 0 0;
    padding-right: var(--space-3);
    border-right: 1px solid var(--line);
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    color: var(--text-dim);
  }
  .stamp .dim { font-weight: 400; letter-spacing: 0; white-space: nowrap; }

  .field { display: flex; flex-direction: column; gap: var(--space-1); }
  .field > span { color: var(--text-dim); font-size: 12px; }
  .field.size .pair { display: inline-flex; align-items: center; gap: var(--space-1); }
  .field.size .pair::after { content: 'px'; color: var(--text-dim); font-size: 11px; }
  input[type='number'] { width: 68px; }

  input[type='text'],
  input[type='number'],
  textarea {
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    font-size: var(--fs-sm);
    padding: var(--space-1) var(--space-2);
    min-width: 0;
  }
  textarea { font: var(--font-mono); font-size: 11px; resize: vertical; }

  select {
    appearance: none;
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    font-size: var(--fs-sm);
    padding: var(--space-1) var(--space-5) var(--space-1) var(--space-2);
    background-image:
      linear-gradient(45deg, transparent 50%, var(--text-dim) 50%),
      linear-gradient(135deg, var(--text-dim) 50%, transparent 50%);
    background-position:
      calc(100% - 14px) calc(50% + 1px),
      calc(100% - 10px) calc(50% + 1px);
    background-size: 5px 5px, 5px 5px;
    background-repeat: no-repeat;
    cursor: pointer;
    max-width: 260px;
  }

  .segmented {
    display: inline-flex;
    gap: 2px;
    padding: 2px;
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
  }
  .segmented button {
    appearance: none;
    border: 0;
    background: transparent;
    color: var(--text-dim);
    font-size: var(--fs-xs);
    font-weight: 550;
    padding: 3px var(--space-2);
    border-radius: var(--radius-xs);
  }
  .segmented button.is-active { background: var(--bg-raised); color: var(--text); box-shadow: var(--sh-xs); }

  .switch {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-dim);
    font-size: 12px;
    padding-bottom: var(--space-1);
  }
  input[type='checkbox'] { accent-color: var(--accent-strong); }

  .btn {
    font: inherit;
    font-size: var(--fs-sm);
    font-weight: 550;
    height: 32px;
    padding: 0 var(--space-3);
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-strong);
    background: var(--bg-raised);
    color: var(--text);
    cursor: pointer;
  }
  .btn:hover { background: var(--bg-hover); border-color: var(--n-400); }

  .chip {
    appearance: none;
    font: inherit;
    font-size: 11px;
    font-family: var(--font-mono);
    padding: 3px var(--space-2);
    border-radius: var(--radius-full);
    border: 1px solid var(--line);
    background: var(--bg-sunken);
    color: var(--text-dim);
    cursor: pointer;
  }
  .chip:hover { color: var(--text); border-color: var(--accent-dim); }

  /* --- 两栏 --- */
  .split {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 348px;
    gap: var(--space-3);
    min-height: 0;
  }
  /* At the window's DEFAULT size (980 wide) two columns leave the stage ~320px, i.e. narrower
     than the 380px rectangle the pomodoro (and most floating surfaces) declares - the very box
     this page exists to show would have to be scrolled sideways to be seen at all. So below
     1120px the log moves UNDER the stage and the box gets the full width. Measured need:
     380 + padding + rail 236 + window padding 64. */
  @media (max-width: 1120px) {
    .split {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: minmax(0, 1fr) minmax(160px, 34vh);
    }
  }

  .stage-col {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
    min-height: 0;
    overflow: auto;
    padding: var(--space-3);
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    box-shadow: var(--sh-xs);
  }
  .stage-head {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    flex-wrap: wrap;
    font-size: var(--fs-sm);
  }
  .who { font-weight: 600; }
  .pill {
    margin-left: auto;
    font-size: 11px;
    font-family: var(--font-mono);
    padding: 1px var(--space-2);
    border-radius: var(--radius-full);
    background: var(--bg-hover);
    color: var(--text-dim);
  }
  .pill.bad { background: var(--warning-bg); color: var(--warning); }

  /* The dashed box IS the window region: the pet crops to exactly this rectangle, so drawing it
     is the whole difference between "the plugin renders" and "the plugin renders inside what the
     user will actually see". Only the box is dashed - its contents belong to the plugin. */
  .canvas { display: flex; flex-direction: column; align-items: center; gap: var(--space-2); padding: var(--space-3) 0; }
  .canvas.fill { flex: 1 1 auto; align-items: stretch; padding: 0; min-height: 240px; }
  .card {
    border: 1px dashed var(--accent-dim);
    border-radius: 4px;
    background: var(--bg-sunken);
    flex: none;
    overflow: hidden;
  }
  .mount { display: block; overflow: hidden; }
  .mount.fill { flex: 1 1 auto; min-height: 0; border: 1px dashed var(--line); border-radius: var(--radius-sm); }
  /* A background frame takes no layout space - the same rule the pet applies (`display:none`),
     because "hidden" has to mean one thing in both programs. */
  .hidden-mount { display: none; }
  .hidden-note {
    padding: var(--space-3);
    background: var(--bg-sunken);
    border: 1px dashed var(--line);
    border-radius: var(--radius-sm);
    font-size: var(--fs-sm);
    color: var(--text-dim);
    line-height: 1.7;
  }
  .caption { margin: 0; font-size: 11px; color: var(--text-dim); }

  .skipped {
    margin: 0;
    padding-left: var(--space-4);
    font-size: 12px;
    color: var(--text-dim);
    line-height: 1.7;
  }

  .perms {
    margin-top: var(--space-2);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
  }
  .perms > summary { cursor: pointer; font-size: 12px; font-weight: 600; color: var(--text-dim); list-style: none; }
  .perms > summary::before { content: '▸ '; }
  .perms[open] > summary::before { content: '▾ '; }
  .perms[open] > summary { margin-bottom: var(--space-2); }
  .perms ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .perms li {
    display: grid;
    grid-template-columns: 14px 190px minmax(0, 1fr);
    gap: var(--space-2);
    align-items: baseline;
    font-size: 12px;
    color: var(--text-faint);
  }
  .perms li.on { color: var(--text); }
  .perms .mark { color: var(--text-faint); }
  .perms li.on .mark { color: var(--ok); }

  /* --- 右栏 --- */
  .side {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-3);
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    box-shadow: var(--sh-xs);
  }
  .side-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }
  .side-head h2 { font-size: var(--fs-sm); font-weight: 650; }
  .count {
    font-family: var(--font-mono);
    font-size: 11px;
    background: var(--bg-hover);
    border-radius: var(--radius-full);
    padding: 1px var(--space-2);
    color: var(--text-dim);
  }

  .log {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    background: var(--bg-sunken);
    overflow: auto;
    max-height: 40vh;
    flex: 0 1 auto;
  }
  .log li {
    display: grid;
    grid-template-columns: 62px 12px 58px minmax(0, 1fr);
    gap: var(--space-1);
    align-items: baseline;
    padding: 2px var(--space-2);
    border-bottom: 1px solid var(--line);
    font: var(--font-mono);
    font-size: 11px;
    line-height: 1.5;
  }
  .log li:last-child { border-bottom: 0; }
  .at { color: var(--text-faint); }
  .dir { color: var(--text-dim); }
  .type { color: var(--accent); }
  .log li.in .type { color: var(--ok); }
  .text { white-space: pre-wrap; word-break: break-word; }
  /* A refusal carries the stripe, like the 日志 page: "ok:false" that looks like every other row
     is a refusal nobody notices. */
  .log li.bad { box-shadow: inset 3px 0 0 var(--warn); }
  .log li.bad .text { color: var(--warn); }

  .tool {
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    background: var(--bg-raised);
  }
  .tool > summary {
    cursor: pointer;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-dim);
    list-style: none;
  }
  .tool > summary::before { content: '▸ '; }
  .tool[open] > summary::before { content: '▾ '; }
  .tool[open] > summary { margin-bottom: var(--space-2); }
  .tool .field { margin-bottom: var(--space-2); }
  .tool .btn { margin-top: var(--space-1); }
  .chips { display: flex; gap: var(--space-1); flex-wrap: wrap; margin-bottom: var(--space-1); }
  .note { margin-left: var(--space-2); font-size: 11px; color: var(--ok); }
  .result {
    margin: var(--space-2) 0 0;
    padding: var(--space-2);
    background: var(--bg-sunken);
    border-radius: var(--radius-xs);
    font: var(--font-mono);
    font-size: 11px;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 24vh;
    overflow: auto;
  }

  .hint { margin: var(--space-2) 0 0; font-size: 11px; color: var(--text-dim); line-height: 1.7; }
  .hint.last { margin-top: auto; }
  code { font: var(--font-mono); font-size: 11px; background: var(--bg-sunken); border-radius: var(--radius-xs); padding: 1px 4px; }
  .dim { color: var(--text-dim); }
  .mono { font: var(--font-mono); font-size: 11px; }
</style>
