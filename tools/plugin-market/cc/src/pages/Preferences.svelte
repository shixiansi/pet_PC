<script lang="ts">
  import { DataDir } from '../lib/bridge';
  import { themeState, setTheme } from '../lib/theme.svelte';

  // 偏好：这个程序自己的两件事——它长什么样（配色），以及它的数据在哪。
  //
  // WHY A PAGE OF ITS OWN rather than a section of 开始: 开始 是**桌宠**的样子（大小、图片、贴边、
  // 开机启动、快捷键），而这一页是**这个窗口**的样子与它自己的一点点状态。混在一起的话，
  // "改的是桌宠还是这个程序"就成了每次都要想一遍的问题。
  //
  // 配色只有一个开关，刻意没有"跟随系统"：多一个状态就要多一条"文件里存的是什么、现在生效的
  // 是什么、系统改了怎么办"的推理，而这个程序没有读取系统主题的绑定。
  let dir = $state('');
  let err = $state('');
  let copied = $state(false);

  void boot();
  async function boot() {
    try {
      dir = await DataDir();
    } catch (e) {
      err = '读不到数据目录：' + why(e);
    }
  }

  async function copyDir() {
    if (!dir) return;
    try {
      await navigator.clipboard.writeText(dir);
      copied = true;
      setTimeout(() => (copied = false), 1600);
    } catch (e) {
      err = '复制失败：' + why(e);
    }
  }

  function why(e: unknown): string {
    if (e instanceof Error) return e.message;
    if (typeof e === 'string') return e;
    try {
      return JSON.stringify(e);
    } catch {
      return String(e);
    }
  }
</script>

<div class="page">
  <section class="card">
    <header class="card-head">
      <h2>外观</h2>
      <p class="hint">这个窗口的配色。插件自己的界面会跟着它走（宿主动手，插件不用管）。</p>
    </header>

    <div class="field-row">
      <span class="field-label">主题</span>
      <div class="segmented" role="group" aria-label="主题">
        <button
          type="button"
          class:is-active={themeState.current === 'light'}
          aria-pressed={themeState.current === 'light'}
          onclick={() => void setTheme('light')}
        >浅色</button>
        <button
          type="button"
          class:is-active={themeState.current === 'dark'}
          aria-pressed={themeState.current === 'dark'}
          onclick={() => void setTheme('dark')}
        >深色</button>
      </div>
      <span class="field-note">存在 <code>plugin-market.json</code> 里，下次打开还是它。</span>
    </div>
  </section>

  <section class="card">
    <header class="card-head">
      <h2>数据</h2>
      <p class="hint">插件、它们的数据、以及两个程序的日志都在这个目录下面。</p>
    </header>

    <div class="field-row">
      <span class="field-label">数据目录</span>
      <code class="path">{dir || '读取中…'}</code>
      <button type="button" class="btn" disabled={!dir} onclick={() => void copyDir()}>
        {copied ? '已复制' : '复制路径'}
      </button>
    </div>
    <p class="field-note">
      下面这几个子目录各自归谁：<code>plugins</code> 是装好的插件，<code>plugin-data/&lt;id&gt;</code>
      是每个插件的私有目录（设置、密钥、数据），<code>logs</code> 是两个程序各写一份的日志。
      卸载插件时「同时删除它的配置与数据」删的就是 <code>plugin-data/&lt;id&gt;</code>。
    </p>
    {#if err}<p class="err">{err}</p>{/if}
  </section>
</div>

<style>
  .page { display: flex; flex-direction: column; gap: var(--space-4); max-width: 760px; }

  .card {
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    box-shadow: var(--sh-xs);
    padding: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .card-head h2 {
    font-size: var(--fs-md);
    font-weight: 650;
    letter-spacing: -0.01em;
  }
  .hint { margin: 2px 0 0; font-size: var(--fs-sm); color: var(--text-dim); line-height: 1.6; }

  .field-row { display: flex; align-items: center; gap: var(--space-3); flex-wrap: wrap; }
  .field-label { font-size: var(--fs-sm); color: var(--text-dim); min-width: 68px; }
  .field-note { font-size: 11px; color: var(--text-dim); line-height: 1.7; }
  code { font: var(--font-mono); background: var(--bg-sunken); border-radius: var(--radius-xs); padding: 1px 5px; }
  .path { flex: 1 1 240px; min-width: 0; padding: 6px var(--space-2); overflow: hidden; text-overflow: ellipsis; }

  /* 分段控件：与参考稿同一形状——一个浅底槽 + 选中的那块浮起来。 */
  .segmented {
    display: inline-flex;
    gap: 2px;
    padding: 3px;
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius);
  }
  .segmented button {
    appearance: none;
    border: 0;
    background: transparent;
    color: var(--text-dim);
    font-size: var(--fs-sm);
    font-weight: 550;
    padding: 5px var(--space-3);
    border-radius: var(--radius-sm);
    transition: background var(--dur-1) var(--ease), color var(--dur-1) var(--ease);
  }
  .segmented button:hover { color: var(--text); }
  .segmented button.is-active {
    background: var(--bg-raised);
    color: var(--text);
    font-weight: 600;
    box-shadow: var(--sh-xs);
  }

  .btn {
    font: inherit;
    font-size: var(--fs-sm);
    font-weight: 550;
    height: 32px;
    padding: 0 var(--space-3);
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-strong);
    background: var(--bg-raised);
    color: var(--text);
    cursor: pointer;
    transition: background var(--dur-1) var(--ease), border-color var(--dur-1) var(--ease);
  }
  .btn:hover { background: var(--bg-hover); border-color: var(--n-400); }
  .btn:disabled { opacity: 0.5; cursor: default; }

  .err { margin: 0; font-size: var(--fs-sm); color: var(--danger); }
</style>
