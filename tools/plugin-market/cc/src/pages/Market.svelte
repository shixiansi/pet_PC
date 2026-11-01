<script lang="ts">
  import {
    Catalogue,
    Install,
    Update,
    SaveMarketURL,
    PermissionLabels,
    RememberSelection,
  } from '../lib/bridge';
  import type { MarketView, MarketEntry } from '../lib/bridge';

  // 插件市场：插件源上有什么。
  //
  // WHY THIS IS ITS OWN PAGE AND NOT A SECTION OF 插件. The 插件 page is a master-detail over the
  // plugins this machine HAS: its rows carry enabled / granted / an interface to mount. This page
  // is the list of what EXISTS, and most of it is not installed - no manifest on disk, no
  // permissions granted, nothing to mount. Putting both in one list would mean one of them lies
  // about what a row is.
  //
  // WHY IT CAN BE SEEN AT ALL: this program has had the catalogue for a while, but only as an
  // update chip on 插件 (`docs/plugin-market.md` §2.8 called browsing "the open half of stage 3").
  // So a user could install a plugin only if the pet's market window had it - and that window is
  // the older program. The catalogue is one file over HTTP; the page that shows it belongs here.
  //
  // WHAT IT NEVER DOES: enabling anything. `Install` lands a package DISABLED with nothing
  // granted, and this page says so out loud and points at 插件. An install that quietly switched a
  // plugin on would be the host granting authority on the user's behalf.
  //
  // A GRID OF CARDS, not a list of rows (2026-10-29, "插件市场改成这样的布局"): what a marketplace
  // sells is the SHAPE of each plugin - a name, a one-paragraph pitch, three facts - and a row of
  // that reads as a table. Cards also survive a wide window: `auto-fill` + a minimum width means a
  // maximised 1800px window shows four or five of them instead of one sentence stretched across it.
  let view = $state<MarketView | null>(null);
  let labels = $state<Record<string, string>>({});
  let loading = $state(false);
  let error = $state('');
  /** A one-off confirmation ("已安装 待办插件 v1.2.0"), cleared by the next action. */
  let notice = $state('');
  /** The entry whose package is being downloaded and unpacked, or ''. */
  let busyId = $state('');
  /** The editable copy of the source address; `view.base` is what the host actually used. */
  let base = $state('');
  let savingBase = $state(false);
  /** What the search box holds. Filtering is CLIENT side: the catalogue is one file already read. */
  let query = $state('');

  // Started here rather than in onMount, same as the 插件 page: a page that renders but never
  // starts a request is exactly the failure that page shipped with once.
  void boot();

  async function boot() {
    void loadLabels();
    await refresh();
  }

  async function loadLabels() {
    try {
      const pairs = await withTimeout('读取权限文案', PermissionLabels());
      labels = Array.isArray(pairs)
        ? Object.fromEntries(pairs.map((p) => [p.ID, p.Label]))
        : {};
    } catch {
      // Not fatal: the page falls back to the permission ids, which are ugly but true.
      labels = {};
    }
  }

  async function refresh() {
    loading = true;
    error = '';
    try {
      view = await withTimeout('读取插件目录', Catalogue());
      if (view) base = view.base ?? '';
    } catch (e) {
      error = '读不到插件目录：' + why(e);
    } finally {
      loading = false;
    }
  }

  async function saveSource() {
    const next = base.trim();
    if (!next || savingBase) return;
    savingBase = true;
    error = '';
    notice = '';
    try {
      view = await withTimeout('保存插件源', SaveMarketURL(next), 15000);
      if (view) base = view.base ?? next;
      notice = '已换成 ' + next;
    } catch (e) {
      error = '保存插件源失败：' + why(e);
    } finally {
      savingBase = false;
    }
  }

  async function install(entry: MarketEntry) {
    if (busyId) return;
    busyId = entry.id;
    error = '';
    notice = '';
    try {
      await withTimeout('安装 ' + entry.name, Install(entry.id), 120000);
      // The 插件 page opens on this plugin, so the next step (granting) is one click away.
      try {
        await RememberSelection(entry.id);
      } catch {
        /* not fatal: it only decides what is selected when 插件 opens */
      }
      notice = '已安装 ' + entry.name + ' v' + entry.version + ' —— 去「插件」页给它授权，它才跑得起来';
      await refresh();
    } catch (e) {
      error = '安装 ' + entry.name + ' 失败：' + why(e);
    } finally {
      busyId = '';
    }
  }

  async function upgrade(entry: MarketEntry) {
    if (busyId) return;
    busyId = entry.id;
    error = '';
    notice = '';
    try {
      await withTimeout('更新 ' + entry.name, Update(entry.id), 120000);
      notice = '已把 ' + entry.name + ' 更新到 v' + entry.version + '（启用状态与已授予的权限保持不变）';
      await refresh();
    } catch (e) {
      error = '更新 ' + entry.name + ' 失败：' + why(e);
    } finally {
      busyId = '';
    }
  }

  // A Wails binding that never settles (a panic on the Go side settles nothing) would leave the
  // page on 「读取中…」 for the life of the window. Name the call and refuse to wait for ever.
  function withTimeout<T>(what: string, p: Promise<T>, ms = 8000): Promise<T> {
    return Promise.race([
      p,
      new Promise<T>((_, reject) =>
        setTimeout(() => reject(new Error(what + ' 超过 ' + ms + 'ms 没有回应')), ms)),
    ]);
  }

  // A Wails rejection is not always an Error: Go can reject with a bare string, and an object
  // prints as "[object Object]". Print what is actually there.
  function why(e: unknown): string {
    if (e instanceof Error) return e.message;
    if (typeof e === 'string') return e;
    try {
      return JSON.stringify(e);
    } catch {
      return String(e);
    }
  }


  function perms(entry: MarketEntry): string[] {
    return Array.isArray(entry.permissions) ? entry.permissions : [];
  }
  function permLabel(id: string): string {
    const label = labels[id];
    return label ? label : id;
  }
  /** 权限的完整文案，给 tooltip 用（卡片上只放数量）。 */
  function permLabels(entry: MarketEntry): string {
    return perms(entry).map(permLabel).join('、');
  }

  /** What the search box matches: the name, the id, and the pitch. */
  const shown = $derived(
    (view?.entries ?? []).filter((e) => {
      const q = query.trim().toLowerCase();
      if (!q) return true;
      return (
        e.name.toLowerCase().includes(q) ||
        e.id.toLowerCase().includes(q) ||
        (e.description ?? '').toLowerCase().includes(q)
      );
    }),
  );

  /**
   * The icon tile's letter and hue.
   *
   * A package has no icon yet (the catalogue carries no artwork), and a grid with eighteen identical
   * placeholders is unreadable. Deriving both from the id is deterministic - the same plugin is the
   * same colour on every machine and after every refresh - so the tile is a landmark rather than
   * decoration. When packages grow an `icon` field this is the one thing that goes away.
   */
  function initial(name: string): string {
    const first = [...(name || '?').trim()][0] ?? '?';
    return first.toUpperCase();
  }
  function hue(id: string): number {
    let h = 0;
    for (let i = 0; i < id.length; i += 1) h = (h * 31 + id.charCodeAt(i)) % 360;
    return h;
  }

  function stateOf(entry: MarketEntry): 'new' | 'old' | 'current' {
    if (!entry.installed) return 'new';
    return (entry.installedVersion ?? '') === entry.version ? 'current' : 'old';
  }
</script>

<div class="page">
  <!-- 这一页的名字与一句话说明在顶栏上（App.svelte 的 topbar），这里不再重复一个 h2：
       顶栏说"是哪一页"，这一段只说这一页最要紧的那条规矩。 -->
  <p class="hint">
    <strong>安装不会启用它</strong>：装完去「插件」页授权它要用的能力，那一步才是你同意它做什么。
  </p>

  <div class="toolbar">
    <label class="search">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" aria-hidden="true">
        <circle cx="11" cy="11" r="6.5" />
        <path d="m16 16 4.5 4.5" />
      </svg>
      <input type="search" bind:value={query} placeholder="搜索插件名称、功能…" spellcheck="false" />
    </label>
    <span class="count">
      {#if view}{query.trim() ? shown.length + ' / ' + view.entries.length : view.entries.length}{:else}—{/if} 个插件
    </span>
    <button class="btn ghost" type="button" onclick={() => void refresh()} disabled={loading}>
      {loading ? '刷新中…' : '重新读取'}
    </button>
  </div>

  <form
    class="source"
    onsubmit={(e) => {
      e.preventDefault();
      void saveSource();
    }}
  >
    <label for="base">插件源</label>
    <input id="base" type="text" bind:value={base} spellcheck="false" placeholder="https://…" />
    <button class="btn" type="submit" disabled={savingBase}>{savingBase ? '保存中…' : '保存并刷新'}</button>
  </form>

  {#if view?.fallback}
    <p class="banner warn">
      取不到插件源（离线或镜像不通），下面是<strong>程序内置的样本</strong>：装得上，但版本可能与源上的不同。
      {view.notice ? '（' + view.notice + '）' : ''}
    </p>
  {:else if view?.notice}
    <p class="banner warn">{view.notice}</p>
  {/if}
  {#if error}<p class="banner bad">{error}</p>{/if}
  {#if notice}<p class="banner ok">{notice}</p>{/if}

  {#if loading && !view}
    <p class="hint">读取中…</p>
  {:else if !view}
    <p class="hint">还没有目录。上面的地址填对之后点「保存并刷新」。</p>
  {:else if view.entries.length === 0}
    <p class="hint">这个源上没有插件。</p>
  {:else if shown.length === 0}
    <p class="hint">没有匹配「{query.trim()}」的插件。清空搜索再看看。</p>
  {:else}
    <ul class="grid">
      {#each shown as entry (entry.id)}
        <li class="card" class:busy={busyId === entry.id}>
          <div class="head">
            <span class="icon" style={`--hue:${hue(entry.id)}`} aria-hidden="true">{initial(entry.name)}</span>
            <div class="ident">
              <h3 title={entry.id}>{entry.name}</h3>
              <p class="id mono" title={entry.id}>{entry.id}</p>
            </div>
            {#if stateOf(entry) === 'current'}
              <span class="state">已安装</span>
            {:else if stateOf(entry) === 'old'}
              <span class="state update">可更新</span>
            {/if}
          </div>

          {#if entry.description}<p class="desc">{entry.description}</p>{/if}

          <!-- 三枚短 pill，和参考稿的节奏一致：版本、要几项权限、归谁。权限的**文案**放进
               title（读者要看的时候鼠标一停就有），而不是铺两行 chip —— 一句「调用你已配置的
               大模型（流式返回）」会把卡片撑成三级台阶，行高不齐。 -->
          <div class="meta">
            <span class="pill mono">v{entry.version}</span>
            {#if perms(entry).length > 0}
              <span class="pill" title={permLabels(entry)}>{perms(entry).length} 项权限</span>
            {/if}
          </div>

          <div class="foot">
            <span class="author">{entry.author ? entry.author : '未署名'}</span>
            {#if stateOf(entry) === 'new'}
              <button class="btn primary" disabled={Boolean(busyId)} onclick={() => void install(entry)}>
                {busyId === entry.id ? '安装中…' : '安装'}
              </button>
            {:else if stateOf(entry) === 'old'}
              <button class="btn primary" disabled={Boolean(busyId)} onclick={() => void upgrade(entry)}>
                {busyId === entry.id ? '更新中…' : '更新到 v' + entry.version}
              </button>
            {:else}
              <span class="installed">已安装 v{entry.installedVersion}</span>
            {/if}
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .page { display: flex; flex-direction: column; gap: var(--space-3); }
  .hint { margin: 0; color: var(--text-dim); font-size: var(--fs-sm); line-height: 1.6; }
  .mono { font: var(--font-mono); }

  /* 工具条：搜索在左，计数与「重新读取」在右。搜索是这一页最常用的动作，所以它占满剩下的宽度。 */
  .toolbar { display: flex; align-items: center; gap: var(--space-3); }
  .search {
    flex: 1 1 auto;
    min-width: 0;
    position: relative;
    display: flex;
    align-items: center;
  }
  .search svg {
    position: absolute;
    left: 11px;
    width: 16px;
    height: 16px;
    color: var(--text-faint);
    pointer-events: none;
  }
  .search input {
    width: 100%;
    height: 36px;
    padding: 0 var(--space-3) 0 34px;
    font: inherit;
    font-size: var(--fs-sm);
    color: var(--text);
    background: var(--bg-raised);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    transition: border-color var(--dur-1) var(--ease), box-shadow var(--dur-1) var(--ease);
  }
  .search input::placeholder { color: var(--text-faint); }
  .search input:focus { outline: none; border-color: var(--p-500); box-shadow: var(--sh-ring); }
  .count { font-size: var(--fs-xs); color: var(--text-dim); white-space: nowrap; }

  .source {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2);
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius);
  }
  .source label { font-size: var(--fs-xs); color: var(--text-dim); }
  .source input {
    flex: 1 1 auto;
    min-width: 0;
    font: var(--font-mono);
    color: var(--text);
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-2);
  }

  .banner {
    margin: 0;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
    font-size: var(--fs-sm);
    line-height: 1.6;
    border: 1px solid var(--line);
  }
  .banner.warn { color: var(--warn); border-color: var(--warn); }
  .banner.bad { color: var(--danger); border-color: var(--danger); }
  .banner.ok { color: var(--ok); border-color: var(--ok); }

  /* auto-fill + a minimum: a narrow window gets one column, a maximised one gets four or five.
     `minmax(276px, 1fr)` is the width at which a two-line description still reads. */
  .grid {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(276px, 1fr));
    gap: var(--space-3);
    align-items: stretch;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-4);
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    box-shadow: var(--sh-xs);
    transition: box-shadow var(--dur-2) var(--ease), border-color var(--dur-2) var(--ease);
  }
  .card:hover { box-shadow: var(--sh-md); border-color: var(--border-strong); }
  .card.busy { opacity: .7; }

  .head { display: flex; align-items: flex-start; gap: var(--space-3); }
  /* 图标位：包里没有图，所以取 id 的一个稳定色相 + 名字首字。同一插件在哪台机器上都是同一个
     颜色，于是它是个"地标"而不是装饰。包里长出色图之后，这里唯一要删的就是它。 */
  .icon {
    width: 40px;
    height: 40px;
    flex: none;
    display: grid;
    place-items: center;
    border-radius: var(--radius);
    color: #fff;
    font-size: var(--fs-lg);
    font-weight: 700;
    background: linear-gradient(140deg, hsl(var(--hue) 72% 58%), hsl(calc(var(--hue) + 26) 68% 44%));
  }
  .ident { min-width: 0; flex: 1 1 auto; }
  .ident h3 {
    margin: 0;
    font-size: var(--fs-md);
    font-weight: 650;
    letter-spacing: -0.01em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .id {
    margin: 1px 0 0;
    font-size: 11px;
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .state {
    flex: none;
    font-size: 11px;
    font-weight: 600;
    padding: 2px var(--space-2);
    border-radius: var(--radius-full);
    color: var(--success);
    background: var(--success-bg);
    white-space: nowrap;
  }
  .state.update { color: var(--warning); background: var(--warning-bg); }

  /* Two lines, then an ellipsis: a card's job is "is this for me", and four lines of prose in a
     276px column is neither. */
  .desc {
    margin: 0;
    font-size: var(--fs-sm);
    color: var(--text-dim);
    line-height: 1.6;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .meta { display: flex; flex-wrap: wrap; gap: var(--space-1); }
  .pill {
    font-size: 11px;
    color: var(--text-dim);
    background: var(--bg-hover);
    border: 1px solid transparent;
    border-radius: var(--radius-full);
    padding: 1px var(--space-2);
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* `margin-top: auto` keeps the action row on the floor of every card in a row, whatever the
     description's height: a grid where the buttons float at different heights looks unfinished. */
  .foot {
    margin-top: auto;
    padding-top: var(--space-2);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    border-top: 1px solid var(--line);
  }
  .author { font-size: 11px; color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .installed { font-size: var(--fs-sm); color: var(--text-dim); white-space: nowrap; }

  .btn {
    font: inherit;
    font-size: var(--fs-sm);
    font-weight: 550;
    height: 32px;
    padding: 0 var(--space-4);
    border-radius: var(--radius);
    border: 1px solid var(--border-strong);
    background: var(--bg-raised);
    color: var(--text);
    cursor: pointer;
    white-space: nowrap;
    transition: background var(--dur-1) var(--ease), border-color var(--dur-1) var(--ease),
      box-shadow var(--dur-1) var(--ease);
  }
  .btn:hover { background: var(--bg-hover); border-color: var(--n-400); }
  .btn:disabled { opacity: .5; cursor: default; }
  /* The one action a card has is FILLED: on a page whose job is "install this", an outline the same
     weight as the pills reads as decoration. */
  .btn.primary {
    background: var(--accent-strong);
    border-color: var(--accent-strong);
    color: var(--on-accent);
    box-shadow: 0 1px 2px rgba(124, 58, 237, 0.3);
  }
  .btn.primary:hover { background: var(--accent-hover); border-color: var(--accent-hover); }
  .btn.ghost { background: transparent; }
  .btn.ghost:hover { background: var(--bg-hover); }
</style>
