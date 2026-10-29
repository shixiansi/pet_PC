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
    { id: 'start', label: '开始', hint: '桌宠的样子、外观与启动', component: Start },
    { id: 'plugins', label: '插件', hint: '已装插件的启停、权限、设置与界面', component: Plugins },
    { id: 'market', label: '市场', hint: '插件源上有什么，能装与能更新', component: Market },
    { id: 'logs', label: '日志', hint: '两个程序写下的日志，按等级与子系统筛', component: Logs },
    { id: 'advanced', label: '高级', hint: '模型配置、数据目录与诊断报告', component: Advanced },
  ] as const;

  let current = $state<string>('start');
  const view = $derived(pages.find((p) => p.id === current) ?? pages[0]);
</script>

<!--
  The shell: a rail on the left, a title bar on top, the page below it.

  WHY A TITLE BAR, when the rail already highlights where you are: the rail says "which page";
  the bar says "which page, in words, and what it is for". It also gives every page the same
  first line, which is why none of them repeats its own name in a big font at the top any more.
-->
<div class="shell">
  <aside class="rail">
    <div class="brand">
      <span class="brand-mark" aria-hidden="true">
        <!-- A pet head, drawn inline: an icon font or an asset would be one more file to ship. -->
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M5 9.5 4 4l5 2.2a9.4 9.4 0 0 1 6 0L20 4l-1 5.5a7.4 7.4 0 0 1 1 3.6c0 4-3.6 6.9-8 6.9s-8-2.9-8-6.9a7.4 7.4 0 0 1 1-3.6Z" />
          <path d="M9 12.5h.01M15 12.5h.01" />
          <path d="M12 15.5c.7 0 1.3-.4 1.6-1" />
        </svg>
      </span>
      <span class="brand-text">
        <span class="brand-name">PetPC</span>
        <span class="brand-sub">控制中心</span>
      </span>
    </div>

    <nav class="nav" aria-label="页面">
      {#each pages as page (page.id)}
        <button
          class="nav-item"
          class:is-active={page.id === current}
          aria-current={page.id === current ? 'page' : undefined}
          onclick={() => (current = page.id)}
        >{page.label}</button>
      {/each}
    </nav>
  </aside>

  <div class="main">
    <header class="topbar">
      <div class="titles">
        <h1>{view.label}</h1>
        <p class="sub">{view.hint}</p>
      </div>
    </header>
    <div class="content">
      <view.component />
    </div>
  </div>
</div>

<style>
  .shell {
    display: grid;
    grid-template-columns: var(--sidebar-w) minmax(0, 1fr);
    height: 100vh;
    overflow: hidden;
    background: var(--bg);
  }

  /* --- 侧栏 --- */
  .rail {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-4) var(--space-3);
    background: var(--bg-raised);
    border-right: 1px solid var(--line);
    min-height: 0;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-2) var(--space-5);
  }
  .brand-mark {
    width: 36px;
    height: 36px;
    flex: none;
    display: grid;
    place-items: center;
    border-radius: var(--radius);
    color: #fff;
    background: linear-gradient(135deg, var(--p-500), var(--p-700));
    box-shadow: 0 2px 8px rgba(124, 58, 237, 0.35);
  }
  .brand-mark svg { width: 21px; height: 21px; }
  .brand-text { display: flex; flex-direction: column; min-width: 0; }
  .brand-name {
    font-size: var(--fs-md);
    font-weight: 650;
    letter-spacing: -0.01em;
    color: var(--text);
  }
  .brand-sub {
    font-size: 11px;
    letter-spacing: 0.02em;
    color: var(--text-dim);
  }

  .nav { display: flex; flex-direction: column; gap: 2px; }
  .nav-item {
    appearance: none;
    border: 0;
    width: 100%;
    text-align: left;
    padding: 9px var(--space-3);
    border-radius: var(--radius);
    font-size: var(--fs-base);
    font-weight: 500;
    color: var(--text-dim);
    background: transparent;
    transition: background var(--dur-1) var(--ease), color var(--dur-1) var(--ease);
  }
  .nav-item:hover { background: var(--bg-hover); color: var(--text); }
  .nav-item.is-active {
    background: var(--bg-active);
    color: var(--accent);
    font-weight: 600;
  }

  /* --- 主区 --- */
  .main { display: flex; flex-direction: column; min-width: 0; min-height: 0; }

  .topbar {
    flex: none;
    height: var(--topbar-h);
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: 0 var(--space-6);
    background: var(--bg-raised);
    border-bottom: 1px solid var(--line);
  }
  .titles { display: flex; flex-direction: column; min-width: 0; }
  .topbar h1 {
    font-size: var(--fs-lg);
    font-weight: 650;
    letter-spacing: -0.015em;
    line-height: 1.2;
    color: var(--text);
  }
  .sub {
    font-size: var(--fs-xs);
    color: var(--text-dim);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* The content area owns the padding and the scroll; a page that fills the window (插件) sets
     `height: 100%` and scrolls its own columns instead, so the two never fight. */
  .content {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-6);
  }
</style>



