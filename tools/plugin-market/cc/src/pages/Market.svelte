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

  /** Up to three permission labels, then "等 N 项" — a row is not the place for a wall of chips. */
  const SHOWN = 3;
  function perms(entry: MarketEntry): string[] {
    return Array.isArray(entry.permissions) ? entry.permissions : [];
  }
  function permLabel(id: string): string {
    const label = labels[id];
    return label ? label : id;
  }

  function stateOf(entry: MarketEntry): 'new' | 'old' | 'current' {
    if (!entry.installed) return 'new';
    return (entry.installedVersion ?? '') === entry.version ? 'current' : 'old';
  }
</script>

<div class="page">
  <header class="head">
    <h2>插件市场</h2>
    <p class="hint">
      插件源上有什么。<strong>安装不会启用它</strong>：装完去「插件」页授权它要用的能力，
      那一步才是你同意它做什么。
    </p>
  </header>

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
    <button class="btn ghost" type="button" onclick={() => void refresh()} disabled={loading}>
      {loading ? '刷新中…' : '重新读取'}
    </button>
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
  {:else}
    <p class="count">共 {view.entries.length} 个插件</p>
    <ul class="list">
      {#each view.entries as entry (entry.id)}
        <li class="row" class:busy={busyId === entry.id}>
          <div class="who">
            <strong>{entry.name}</strong>
            <span class="mono id">{entry.id}</span>
            <span class="ver">v{entry.version}</span>
            {#if entry.author}<span class="author">{entry.author}</span>{/if}
          </div>
          {#if entry.description}<p class="desc">{entry.description}</p>{/if}
          <div class="meta">
            {#each perms(entry).slice(0, SHOWN) as p (p)}
              <span class="chip" title={p}>{permLabel(p)}</span>
            {/each}
            {#if perms(entry).length > SHOWN}
              <span class="chip">等 {perms(entry).length} 项</span>
            {/if}
          </div>
          <div class="act">
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
  .page { display: flex; flex-direction: column; gap: var(--space-3); max-width: 880px; }
  .head h2 { margin: 0 0 var(--space-1); font-size: 15px; }
  .hint { margin: 0; color: var(--text-dim); font-size: 12px; line-height: 1.6; }
  .mono { font-family: ui-monospace, Consolas, monospace; }

  .source {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2);
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
  }
  .source label { font-size: 12px; color: var(--text-dim); }
  .source input {
    flex: 1 1 auto;
    min-width: 0;
    font: inherit;
    font-size: 12px;
    font-family: ui-monospace, Consolas, monospace;
    color: var(--text);
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-2);
  }

  .banner {
    margin: 0;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
    font-size: 12px;
    line-height: 1.6;
    border: 1px solid var(--line);
  }
  .banner.warn { color: var(--warn, #e0b341); border-color: var(--warn, #e0b341); }
  .banner.bad { color: var(--danger, #e05a5a); border-color: var(--danger, #e05a5a); }
  .banner.ok { color: var(--ok, #57b57a); border-color: var(--ok, #57b57a); }

  .count { margin: 0; font-size: 12px; color: var(--text-dim); }

  .list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--space-2); }
  .row {
    display: grid;
    grid-template-columns: 1fr auto;
    grid-template-areas: 'who act' 'desc act' 'meta act';
    gap: var(--space-1) var(--space-3);
    padding: var(--space-3);
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
  }
  .row.busy { opacity: .7; }
  .who { grid-area: who; display: flex; align-items: baseline; gap: var(--space-2); flex-wrap: wrap; }
  .who strong { font-size: 13px; }
  .id { font-size: 11px; color: var(--text-dim); }
  .ver { font-size: 11px; color: var(--text-dim); }
  .author { font-size: 11px; color: var(--text-dim); }
  .desc { grid-area: desc; margin: 0; font-size: 12px; color: var(--text-dim); line-height: 1.6; }
  .meta { grid-area: meta; display: flex; flex-wrap: wrap; gap: var(--space-1); }
  .chip {
    font-size: 11px;
    color: var(--text-dim);
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 0 var(--space-2);
  }
  .act { grid-area: act; display: flex; align-items: center; }
  .installed { font-size: 12px; color: var(--text-dim); }

  .btn {
    font: inherit;
    font-size: 12px;
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-sm);
    border: 1px solid var(--line);
    background: var(--bg);
    color: var(--text);
    cursor: pointer;
  }
  .btn:disabled { opacity: .5; cursor: default; }
  .btn.primary { border-color: var(--accent, #4a7dff); color: var(--accent, #4a7dff); }
  .btn.ghost { background: transparent; }
</style>
