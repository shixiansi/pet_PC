<script lang="ts">
  import Card from '../components/Card.svelte';
  import { Installed, Grant, SetEnabled, PermissionLabels } from '../lib/bridge';
  import type { PluginRow } from '../lib/bridge';

  // The row type is the GENERATED one (wailsjs/go/models.ts -> plugins.Installed), not a
  // hand-written description of it. That is the whole fix: this page used to declare
  // `{ id, version, enabled, granted, manifest }` in camelCase, the host sends Go field
  // names (`ID`, `Enabled`, `Manifest`), and both sides were wrong in a way that only
  // looked empty at runtime. Now a field the host does not send is a `npm run check`
  // error instead of four blank rows.
  let rows = $state<PluginRow[]>([]);
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

  // A Wails rejection is not always an Error: the Go side can reject with a bare string,
  // and a structured one prints as "[object Object]" through String(). Both of those have
  // already cost a round trip in this page's history, so print what is actually there.
  function why(e: unknown): string {
    if (e instanceof Error) return e.message;
    if (typeof e === 'string') return e;
    return peek(e);
  }

  // Print the payload, not the idea of it. Anything non-string goes through JSON, because
  // String() on an object gives "[object Object]" - the value was there and nothing showed
  // it, which is how a status code survived several rounds of guessing.
  function peek(v: unknown): string {
    try {
      const s = JSON.stringify(v);
      if (s === undefined) return String(v);
      return s.length > 400 ? s.slice(0, 400) + '…' : s;
    } catch {
      return String(v);
    }
  }

  async function refresh() {
    const problems: string[] = [];
    try {
      const list = await withTimeout('读取已装插件', Installed());
      if (list == null) rows = [];
      else if (!Array.isArray(list)) throw new Error('宿主返回的不是数组，原样：' + peek(list));
      else rows = list;
    } catch (e) {
      problems.push('已装列表：' + why(e));
    }
    try {
      // The labels come from the host, not from a map in this file: the install dialog is
      // the user's consent, and consent described two ways is consent to two things.
      // A slice of {ID, Label} rather than a map, because a map arrives as an anonymous
      // object that nothing can describe - see the comment on LabelPair in the Go side.
      const pairs = await withTimeout('读取权限文案', PermissionLabels());
      if (pairs == null) {
        labels = {};
      } else if (!Array.isArray(pairs)) {
        // Naming this case is the point: a map, an error object or a bare string all used
        // to reach the lookup below and fail as "x.map is not a function".
        throw new Error('宿主返回的不是 {ID, Label} 数组，原样：' + peek(pairs));
      } else {
        labels = Object.fromEntries(pairs.map((p) => [p.ID, p.Label]));
      }
    } catch (e) {
      // Not fatal: the page can still show ids, and saying which call failed is worth
      // more than a blank screen.
      problems.push('权限文案：' + why(e));
    }
    error = problems.join('；');
    loaded = true;
  }

  function name(r: PluginRow): string {
    return r.Manifest?.name || r.ID || '（这一行没有 ID）';
  }

  function label(perm: string): string {
    return labels[perm] || perm;
  }

  // The grant list is replaced WHOLE on every toggle: the host's Grant takes the complete
  // set, so sending a delta would be a second definition of what "granted" means.
  async function togglePerm(r: PluginRow, perm: string, on: boolean) {
    const granted = new Set(r.Granted ?? []);
    if (on) granted.add(perm);
    else granted.delete(perm);
    await act(() => Grant(r.ID, [...granted]));
  }

  async function toggleEnabled(r: PluginRow, on: boolean) {
    await act(() => SetEnabled(r.ID, on));
  }

  async function act(run: () => Promise<unknown>) {
    try {
      await run();
      await refresh();
    } catch (e) {
      error = why(e);
    }
  }
</script>

<Card title="已装插件 · v4">
  <p class="hint" style="font: var(--font-mono); opacity:.55">
    字段用宿主的名字：ID / Enabled / Manifest.Name / Granted（宿主没有 json tag）。类型来自生成的
    wailsjs/go/models.ts，手写一份 camelCase 的类型曾让四个字段全是 undefined。
  </p>
  <p class="hint" style="font: var(--font-mono); opacity:.55">
    key = 值 + 下标：数据不保证唯一，用值当 key 会在渲染时抛 each_key_duplicate
  </p>
  {#if !loaded}
    <p class="hint">读取中…</p>
  {:else if rows.length === 0}
    <p class="hint">还没有插件。插件市场仍在旧界面里，从这里安装要到下一批。</p>
  {:else}
    {#each rows as r, ri (r.ID + ':' + ri)}
      <section>
        {#if !r.ID}
          <!-- The 空对象 symptom, made self-describing: a row that arrives without an id
               is the shape this page got for five rounds, and a screenshot of "nothing" is
               not evidence. Print the object. -->
          <p class="err">
            第 {ri + 1} 行没有 ID，页面读的字段名和宿主返回的对不上。原样：
            <code>{JSON.stringify(r)}</code>
          </p>
        {/if}
        <header>
          <label class="name">
            <input type="checkbox" checked={r.Enabled} onchange={(e) => toggleEnabled(r, (e.currentTarget as HTMLInputElement).checked)} />
            <span>{name(r)}</span>
          </label>
          <span class="meta">{r.ID || '（无 id）'} · v{r.Version || '?'}</span>
        </header>

        {#if r.Error}<p class="err">{r.Error}</p>{/if}

        {#if (r.Manifest?.permissions?.length ?? 0) > 0}
          <div class="perms">
            {#each r.Manifest?.permissions ?? [] as perm, pi (perm + ':' + pi)}
              <label class="perm" title={perm}>
                <input
                  type="checkbox"
                  checked={(r.Granted ?? []).includes(perm)}
                  onchange={(e) => togglePerm(r, perm, (e.currentTarget as HTMLInputElement).checked)}
                />
                <span>{label(perm)}</span>
              </label>
            {/each}
          </div>
          <p class="warn">勾选即授权。取消勾选会让用到它的工具与方法立刻开始被拒绝。</p>
        {:else if !r.Error}
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
  .err code { font: var(--font-mono); font-size: 11px; word-break: break-all; }
</style>
