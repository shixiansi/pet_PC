<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import { Todos } from '../lib/bridge';

  type Todo = {
    id: string;
    title: string;
    notes?: string;
    due?: string;
    done: boolean;
    createdAt: string;
  };

  let todos = $state<Todo[]>([]);
  let query = $state('');
  let loaded = $state(false);
  let error = $state('');

  onMount(async () => {
    try {
      todos = (await Todos()) ?? [];
    } catch (e) {
      error = String(e);
    } finally {
      loaded = true;
    }
  });

  // Filtered in the page rather than through the host: the list is bounded (the pet
  // asks the model to read at most 12) and a round trip per keystroke would be a
  // request per character for no benefit.
  const shown = $derived(
    query.trim() === ''
      ? todos
      : todos.filter((t) => (t.title + ' ' + (t.notes ?? '')).toLowerCase().includes(query.trim().toLowerCase())),
  );

  const open = $derived(shown.filter((t) => !t.done).length);

  function due(t: Todo): string {
    if (!t.due) return '无日期';
    const d = new Date(t.due);
    return Number.isNaN(d.getTime()) ? t.due : d.toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  }
</script>

<Card title="待办">
  {#if !loaded}
    <p class="hint">读取中…</p>
  {:else}
    <div class="head">
      <input class="search" type="search" placeholder="搜索标题或备注" bind:value={query} />
      <span class="count">{open} 项未完成 / 共 {todos.length} 项</span>
    </div>

    {#if shown.length === 0}
      <p class="hint">{todos.length === 0 ? '还没有待办。' : '没有匹配的条目。'}</p>
    {:else}
      <ul>
        {#each shown as t, i (t.id + ':' + i)}
          <li class:done={t.done}>
            <span class="mark">{t.done ? '✓' : '·'}</span>
            <span class="title">{t.title}</span>
            <span class="due">{due(t)}</span>
          </li>
        {/each}
      </ul>
    {/if}

    <p class="hint">
      这一页是**只读**的：todos.json 的写者是桌宠，这里再加一个写者就会出现"谁覆盖谁"。
      要改待办请用桌宠的面板；写权限要等一个明确的刷新协议。
    </p>
  {/if}
  {#if error}<p class="err">{error}</p>{/if}
</Card>

<style>
  .head { display: flex; align-items: center; gap: var(--space-3); margin-bottom: var(--space-3); }
  .search {
    flex: 1;
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    padding: var(--space-2) var(--space-3);
  }
  .count { color: var(--text-dim); font-size: 12px; white-space: nowrap; }
  ul { list-style: none; margin: 0; padding: 0; }
  li {
    display: grid;
    grid-template-columns: 18px 1fr auto;
    align-items: baseline;
    gap: var(--space-2);
    padding: var(--space-2) 0;
    border-bottom: 1px solid var(--line);
  }
  li:last-child { border-bottom: 0; }
  li.done .title { color: var(--text-dim); text-decoration: line-through; }
  .mark { color: var(--ok); }
  .due { color: var(--text-dim); font: var(--font-mono); font-size: 12px; white-space: nowrap; }
  .hint { margin: var(--space-3) 0 0; color: var(--text-dim); font-size: 12px; line-height: 1.6; }
  .err { margin: var(--space-2) 0 0; color: var(--danger); font-size: 12px; }
</style>