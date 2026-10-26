<script lang="ts">
  import Card from '../components/Card.svelte';
  import { Installed, Grant, SetEnabled, PermissionLabels } from '../lib/bridge';

  type Row = {
    id: string;
    version?: string;
    enabled: boolean;
    granted?: string[];
    error?: string;
    manifest?: { name?: string; permissions?: string[] };
  };

  let rows = $state<Row[]>([]);
  let labels = $state<Record<string, string>>({});
  let loaded = $state(false);
  let error = $state('');

  // Started here rather than in onMount. onMount is one more thing between "the page
  // rendered" and "the request started", and a page that renders but never starts is
  // exactly the failure this page had: a permanent 读取中 with no error and no console
  // message. Assigning $state during init is allowed, so there is nothing to defer.
  void refresh();

  // Every binding call is awaited SEPARATELY, behind a timeout, and the reason is the bug
  // this page shipped with: it sat on "读取中…" forever.
  //
  // A Wails binding that never settles (a panic on the Go side settles nothing) leaves an
  // await pending for the life of the window, and `finally` never runs. A page whose only
  // two states are "reading" and "ready" then lies: it reports progress it is not making.
  // So: name what failed, and refuse to be silent for longer than a couple of seconds.
  function withTimeout<T>(what: string, p: Promise<T>, ms = 4000): Promise<T> {
    return Promise.race([
      p,
      new Promise<T>((_, reject) => setTimeout(() => reject(new Error(what + ' 超过 ' + ms + 'ms 没有回应')), ms)),
    ]);
  }

  async function refresh() {
    const problems: string[] = [];
    try {
      const list = await withTimeout('读取已装插件', Installed());
      rows = list ?? [];
    } catch (e) {
      problems.push('已装列表：' + String(e));
    }
    try {
      // The labels come from the host, not from a map in this file: the install dialog is
      // the user's consent, and consent described two ways is consent to two things.
      const names = await withTimeout('读取权限文案', PermissionLabels());
      labels = names ?? {};
    } catch (e) {
      // Not fatal: the page can still show ids, and saying which call failed is worth
      // more than a blank screen.
      problems.push('权限文案：' + String(e));
    }
    error = problems.join('；');
    loaded = true;
  }

  function name(r: Row): string {
    return r.manifest?.name || r.id;
  }

  function label(perm: string): string {
    return labels[perm] || perm;
  }

  // The grant list is replaced WHOLE on every toggle: the host's Grant takes the complete
  // set, so sending a delta would be a second definition of what "granted" means.
  async function togglePerm(r: Row, perm: string, on: boolean) {
    const granted = new Set(r.granted ?? []);
    if (on) granted.add(perm);
    else granted.delete(perm);
    await act(() => Grant(r.id, [...granted]));
  }

  async function toggleEnabled(r: Row, on: boolean) {
    await act(() => SetEnabled(r.id, on));
  }

  async function act(run: () => Promise<unknown>) {
    try {
      await run();
      await refresh();
    } catch (e) {
      error = String(e);
    }
  }
</script>

<Card title="已装插件 · v3">
  <p class="hint" style="font: var(--font-mono); opacity:.55">
    key = 值 + 下标：数据不保证唯一，用值当 key 会在渲染时抛 each_key_duplicate
  </p>
  {#if !loaded}
    <p class="hint">读取中…</p>
  {:else if rows.length === 0}
    <p class="hint">还没有插件。插件市场仍在旧界面里，从这里安装要到下一批。</p>
  {:else}
    {#each rows as r, ri (r.id + ':' + ri)}
      <section>
        <header>
          <label class="name">
            <input type="checkbox" checked={r.enabled} onchange={(e) => toggleEnabled(r, (e.currentTarget as HTMLInputElement).checked)} />
            <span>{name(r)}</span>
          </label>
          <span class="meta">{r.id} · v{r.version ?? '?'}</span>
        </header>

        {#if r.error}<p class="err">{r.error}</p>{/if}

        {#if (r.manifest?.permissions?.length ?? 0) > 0}
          <div class="perms">
            {#each r.manifest?.permissions ?? [] as perm, pi (perm + ':' + pi)}
              <label class="perm" title={perm}>
                <input
                  type="checkbox"
                  checked={(r.granted ?? []).includes(perm)}
                  onchange={(e) => togglePerm(r, perm, (e.currentTarget as HTMLInputElement).checked)}
                />
                <span>{label(perm)}</span>
              </label>
            {/each}
          </div>
          <p class="warn">勾选即授权。取消勾选会让用到它的工具与方法立刻开始被拒绝。</p>
        {:else}
          <p class="hint">这个插件没有声明任何权限。</p>
        {/if}
      </section>
    {/each}
  {/if}
  {#if error}<p class="err">{error}</p>{/if}
</Card>

<style>
  section { border-bottom: 1px solid var(--line); padding: var(--space-3) 0; }
  section:last-of-type { border-bottom: 0; }
  header { display: flex; justify-content: space-between; align-items: baseline; gap: var(--space-3); }
  .name { display: flex; align-items: center; gap: var(--space-2); color: var(--text); }
  .meta { color: var(--text-dim); font: var(--font-mono); font-size: 11px; }
  .perms { display: flex; flex-direction: column; gap: var(--space-1); margin-top: var(--space-2); }
  .perm { display: flex; align-items: center; gap: var(--space-2); color: var(--text-dim); font-size: 12px; }
  .hint { margin: var(--space-2) 0 0; color: var(--text-dim); font-size: 12px; line-height: 1.6; }
  .warn { margin: var(--space-2) 0 0; color: var(--danger); font-size: 11px; }
  .err { margin: var(--space-2) 0 0; color: var(--danger); font-size: 12px; }
</style>