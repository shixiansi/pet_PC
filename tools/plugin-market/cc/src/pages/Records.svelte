<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import { PomodoroRecords } from '../lib/bridge';
  import type { PomodoroRecord } from '../lib/bridge';

  let records = $state<PomodoroRecord[]>([]);
  let loaded = $state(false);
  let error = $state('');

  onMount(async () => {
    try {
      records = (await PomodoroRecords()) ?? [];
    } catch (e) {
      error = String(e);
    } finally {
      loaded = true;
    }
  });

  // Newest first, and grouped by day the way the pet's own records page does it.
  const days = $derived.by(() => {
    const by = new Map<string, PomodoroRecord[]>();
    for (const r of [...records].reverse()) {
      const key = r.endedAt ? new Date(r.endedAt).toLocaleDateString('zh-CN') : '未知日期';
      const list = by.get(key) ?? [];
      list.push(r);
      by.set(key, list);
    }
    return [...by.entries()];
  });

  const total = $derived(records.reduce((n, r) => n + (r.focusMinutes ?? 0), 0));
</script>

<Card title="番茄记录">
  {#if !loaded}
    <p class="hint">读取中…</p>
  {:else if records.length === 0}
    <p class="hint">还没有完成的番茄。</p>
  {:else}
    <p class="total">共 {records.length} 个番茄，累计专注 {total} 分钟</p>
    {#each days as [day, list] (day)}
      <section>
        <h3>{day}</h3>
        {#each list as r, i (day + i)}
          <article>
            <header>
              <span class="title">{r.todoTitle || '（未关联待办）'}</span>
              <span class="mins">{r.focusMinutes ?? '?'} 分钟</span>
            </header>
            {#if r.plan}<p class="plan">计划：{r.plan}</p>{/if}
            {#if r.summary}<p class="sum">实际：{r.summary}</p>{/if}
          </article>
        {/each}
      </section>
    {/each}
    <p class="hint">
      只读。记录是**档案**：它是番茄钟唯一不可重写的东西，能改它的只有你（在桌宠里）。
    </p>
  {/if}
  {#if error}<p class="err">{error}</p>{/if}
</Card>

<style>
  .total { margin: 0 0 var(--space-3); color: var(--text-dim); font-size: 12px; }
  section { margin-bottom: var(--space-4); }
  h3 { font-size: 12px; color: var(--text-dim); margin: 0 0 var(--space-2); font-weight: 500; }
  article {
    border-left: 2px solid var(--line);
    padding: var(--space-2) 0 var(--space-2) var(--space-3);
    margin-bottom: var(--space-2);
  }
  header { display: flex; justify-content: space-between; gap: var(--space-3); }
  .title { color: var(--text); }
  .mins { color: var(--text-dim); font: var(--font-mono); font-size: 12px; white-space: nowrap; }
  .plan, .sum { margin: var(--space-1) 0 0; color: var(--text-dim); font-size: 12px; }
  .hint { margin: var(--space-3) 0 0; color: var(--text-dim); font-size: 12px; line-height: 1.6; }
  .err { margin: var(--space-2) 0 0; color: var(--danger); font-size: 12px; }
</style>