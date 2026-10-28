<script lang="ts">
  import Start from './pages/Start.svelte';
  import Plugins from './pages/Plugins.svelte';
  import Market from './pages/Market.svelte';
  import Logs from './pages/Logs.svelte';
  import Advanced from './pages/Advanced.svelte';

  // Five pages, one level. Coopanion splits its settings into a normal mode and an
  // advanced mode; the pet is small enough that a single rail is honest.
  //
  // 「待办」 was the second page, and it is GONE rather than moved: it read the pet's
  // `todos.json`, which stopped being the list the moment the list moved into the todo
  // plugin's own storage (one writer - the plugin). A page reading a file nobody writes is
  // worse than no page, and the list already has a home: the plugin's own document, which
  // the 插件 page mounts in its workspace (so does the pomodoro's records view and the chat
  // plugin's transcript). "Which program draws the plugin's interface" has one answer now:
  // the plugin decides, both programs host it.
  //
  // 「市场」 is next to 插件 on purpose: one page lists what this machine HAS (and mounts its
  // interface), the other lists what the SOURCE has. They were one page's worth of confusion
  // for a while - the catalogue only existed as an update chip, so installing something new
  // meant opening the pet's older market window. The catalogue is one HTTP GET; the page that
  // shows it belongs to the program that is already open.
  //
  // 「日志」 is the third, and it exists because a bug report had no evidence to carry: the
  // pet writes one (logs/pet.log, always on, INFO by default), this program writes its own,
  // and this page is where the two are read side by side with levels. It sits before 高级
  // because that is when it is used: while looking at a plugin that misbehaves.
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
    { id: 'plugins', label: '插件', component: Plugins },
    { id: 'market', label: '市场', component: Market },
    { id: 'logs', label: '日志', component: Logs },
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