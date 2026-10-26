<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import { Installed, Grant, SetEnabled } from '../../../frontend/wailsjs/go/main/Market';

  type Row = {
    id: string;
    version?: string;
    enabled: boolean;
    granted?: string[];
    error?: string;
    manifest?: { name?: string; permissions?: string[] };
  };

  let rows = $state<Row[]>([]);
  let loaded = $state(false);
  let error = $state('');

  onMount(refresh);

  async function refresh() {
    try {
      rows = (await Installed()) ?? [];
      error = '';
    } catch (e) {
      error = String(e);
    } finally {
      loaded = true;
    }
  }

  function name(r: Row): string {
    return r.manifest?.name || r.id;
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

<Card title="已装插件">
  {#if !loaded}
    <p class="hint">读取中…</p>
  {:else if rows.length === 0}
    <p class="hint">还没有插件。插件市场仍在旧界面里，从这里安装要到下一批。</p>
  {:else}
    {#each rows as r (r.id)}
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
            {#each r.manifest?.permissions ?? [] as perm (perm)}
              <label class="perm">
                <input
                  type="checkbox"
                  checked={(r.granted ?? []).includes(perm)}
                  onchange={(e) => togglePerm(r, perm, (e.currentTarget as HTMLInputElement).checked)}
                />
                <code>{perm}</code>
              </label>
            {/each}
          </div>
          <p class="warn">勾选即授权。取消勾选会让用到它的工具/方法立刻开始被拒绝。</p>
        {:else}
          <p class="hint">这个插件没有声明任何权限。</p>
        {/if}
      </section>
    {/each}
    <p class="hint">
      权限名显示的是 id 而不是中文说明 —— 那份文案在宿主的 Go 侧，这一页还没有读到它的
      通道。加一个只读绑定就能显示，是下一批的事。
    </p>
  {/if}
  {#if error}<p class="err">{error}</p>{/if}
</Card>

<style>
  section { border-bottom: 1px solid var(--line); padding: var(--space-3) 0; }
  section:last-of-type { border-bottom: 0; }
  header { display: flex; justify-content: space-between; align-items: baseline; gap: var(--space-3); }
  .name { display: flex; align-items: center; gap: var(--space-2); color: var(--text); }
  .meta { color: var(--text-dim); font: var(--font-mono); font-size: 11px; }
  .perms { display: flex; flex-wrap: wrap; gap: var(--space-2) var(--space-4); margin-top: var(--space-2); }
  .perm { display: flex; align-items: center; gap: var(--space-1); color: var(--text-dim); }
  code { font: var(--font-mono); font-size: 11px; }
  .hint { margin: var(--space-2) 0 0; color: var(--text-dim); font-size: 12px; line-height: 1.6; }
  .warn { margin: var(--space-2) 0 0; color: var(--danger); font-size: 11px; }
  .err { margin: var(--space-2) 0 0; color: var(--danger); font-size: 12px; }
</style>