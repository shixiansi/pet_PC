<script lang="ts">
  import Start from './pages/Start.svelte';
  import Todos from './pages/Todos.svelte';
  import Plugins from './pages/Plugins.svelte';
  import Advanced from './pages/Advanced.svelte';

  // Four pages, one level. Coopanion splits its settings into a normal mode and an
  // advanced mode; the pet is small enough that a single rail is honest.
  //
  // 「专注」 and 「记录」 were the fifth and sixth, and they left with the host timer: both
  // read and wrote config.json fields that no longer exist (focusMinutes, shortBreakMinutes,
  // pomodoroSound, …) and the pomodoro-records.json archive. The timer's own settings and its
  // archive belong to the pomodoro plugin now (com.shixiansi.plugin-pomodoro), and a plugin's
  // declared settings are edited on the 插件 page from its manifest - so there is nothing left
  // for these two pages to show, and a page that silently writes keys nobody reads is worse
  // than no page.
  const pages = [
    { id: 'start', label: '开始', component: Start },
    { id: 'todos', label: '待办', component: Todos },
    { id: 'plugins', label: '插件', component: Plugins },
    { id: 'advanced', label: '高级', component: Advanced },
  ] as const;

  let current = $state<string>('start');
  const view = $derived(pages.find((p) => p.id === current) ?? pages[0]);
</script>

<div class="shell">
  <nav>
    <h1>PetPC</h1>
    {#each pages as page (page.id)}
      <button
        class="rail-item"
        class:active={page.id === current}
        onclick={() => (current = page.id)}
      >{page.label}</button>
    {/each}
  </nav>
  <main>
    <view.component />
  </main>
</div>

<style>
  .shell { display: grid; grid-template-columns: 168px 1fr; height: 100vh; }

  nav {
    background: var(--bg-sunken);
    border-right: 1px solid var(--line);
    padding: var(--space-4) var(--space-2);
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  h1 {
    font-size: 13px;
    letter-spacing: .08em;
    color: var(--text-dim);
    margin: 0 0 var(--space-3) var(--space-2);
  }

  .rail-item {
    appearance: none;
    border: 0;
    background: transparent;
    color: var(--text-dim);
    text-align: left;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
    font: inherit;
    cursor: pointer;
  }
  .rail-item:hover { color: var(--text); background: var(--bg-raised); }
  .rail-item.active { color: var(--text); background: var(--bg-raised); }

  main { padding: var(--space-5); overflow-y: auto; }
</style>