<script lang="ts">
  import {
    DataDir,
    Themes,
    ThemePacksProblem,
    ThemeStore,
    InstallTheme,
    RemoveTheme,
    type PackEntry,
    type ThemeStoreRow,
  } from '../lib/bridge';
  import { themeState, setTheme, setPack } from '../lib/theme.svelte';
  import { PACKS, DEFAULT_PACK, packToStore } from '../lib/packs';

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

  // 已安装的主题包（内建的走 PACKS 那张表，它们的配色在 packs.css 里；装进来的包配色来自宿主，
  // 因为 CSS 里没有它们的块）。两边合起来才是用户能选的全部。
  let installed = $state<PackEntry[]>([]);
  let packsProblem = $state('');

  // 主题商店：市场源上的货架。`storeNote` 是"没有货架但不算错误"的那句话（市场源还没有
  // themes/index.json 时会是这样），它必须显示出来——空货架和加载失败在界面上长得一样。
  let storeRows = $state<ThemeStoreRow[]>([]);
  let storeNote = $state('');
  let storeBusy = $state('');
  let storeError = $state('');

  void boot();
  async function boot() {
    try {
      dir = await DataDir();
    } catch (e) {
      err = '读不到数据目录：' + why(e);
    }
    await loadPacks();
    await loadStore();
  }

  async function loadPacks() {
    try {
      installed = (await Themes()).filter((p) => !p.builtin);
      packsProblem = await ThemePacksProblem();
    } catch (e) {
      packsProblem = '读不到已安装的主题包：' + why(e);
    }
  }

  async function loadStore() {
    try {
      const view = await ThemeStore();
      storeRows = view.entries ?? [];
      storeNote = view.note ?? '';
    } catch (e) {
      storeError = '读不到主题商店：' + why(e);
    }
  }

  /** 商店里那一条现在该显示什么：没装 / 已装（可更新）/ 已是最新。 */
  function installedVersion(id: string): string {
    return installed.find((p) => p.id === id)?.version ?? '';
  }

  async function install(id: string) {
    storeBusy = id;
    storeError = '';
    try {
      await InstallTheme(id);
      await loadPacks();
      await loadStore();
    } catch (e) {
      storeError = '安装失败：' + why(e);
    } finally {
      storeBusy = '';
    }
  }

  async function remove(id: string) {
    storeBusy = id;
    storeError = '';
    try {
      await RemoveTheme(id);
      // 删掉的正是当前在用的包时，回到默认配色——否则窗口会停在一个已经不存在的包上。
      if (themeState.pack === id) await setPack('');
      await loadPacks();
      await loadStore();
    } catch (e) {
      storeError = '卸载失败：' + why(e);
    } finally {
      storeBusy = '';
    }
  }

  /**
   * 把一个已安装包的令牌摊成 CSS 变量，供卡片里的预览使用。
   *
   * 为什么不给卡片挂 `data-theme-pack` 了事：`packs.css` 里只有内建那三个块，装进来的包的配色
   * 从来不在任何样式表里——它在宿主的内存里。内联 `--pv-*` 让同一段预览标记对两类包都成立：
   * 内建包不内联，落回 `var(--bg)` 那一档（它们本来就有块），已安装包则由宿主送来的值画出来。
   * 于是"预览就是真实配色"这条对用户自己的包同样成立。
   */
  function previewVars(p: PackEntry): string {
    const t = p.tokens?.[themeState.current] ?? p.tokens?.light ?? {};
    const pick = (...names: string[]) => names.map((n) => t[n]).find(Boolean);
    const parts: [string, string | undefined][] = [
      ['--pv-bg', pick('bg')],
      ['--pv-raised', pick('bg-raised')],
      ['--pv-line', pick('line', 'border')],
      ['--pv-text', pick('text')],
      ['--pv-dim', pick('text-dim')],
      ['--pv-accent', pick('accent-strong', 'accent')],
      ['--pv-on-accent', pick('on-accent')],
    ];
    return parts
      .filter(([, v]) => v)
      .map(([k, v]) => `${k}:${v}`)
      .join(';');
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
      <span class="field-note">两个窗口共用这一个设置（<code>theme.json</code>）：桌宠右键菜单的「主题」改的是同一份。</span>
    </div>
  </section>

  <!--
    主题包。卡片自己带着 `data-theme` + `data-theme-pack`，所以卡片内部用的就是那个包的真实令牌
    （packs.css 的块不挂 `:root`，正是为了让它们能铺在一个小盒子上）——预览不是一张画，
    它就是那个配色本身。抄一份色值进 TS 是最容易腐坏的做法：改了 CSS、忘了改预览。
  -->
  <section class="card">
    <header class="card-head">
      <h2>主题包</h2>
      <p class="hint">点一下立即生效，两个窗口一起换（<code>theme.json</code> 的 <code>pack</code>）。桌宠的气泡、右键菜单、徽章用的是同一套。</p>
    </header>

    <div class="packs">
      {#each PACKS as p (p.id)}
        {@const id = packToStore(p.id)}
        <button
          type="button"
          class="pack"
          class:is-active={themeState.pack === id}
          aria-pressed={themeState.pack === id}
          data-theme={themeState.current}
          data-theme-pack={p.id === DEFAULT_PACK ? undefined : p.id}
          onclick={() => void setPack(id)}
        >
          <span class="pack-preview">
            <span class="pk-row"><i class="pk-dot"></i><i class="pk-dot"></i></span>
            <span class="pk-line w72"></span>
            <span class="pk-line w44"></span>
            <span class="pk-pill">按钮</span>
          </span>
          <span class="pack-name">{p.name}</span>
          <span class="pack-hint">{p.hint}</span>
        </button>
      {/each}

      <!--
        已安装的包（themes/<id>/theme.json）。它们的配色**不在** packs.css 里，所以预览用的是宿主
        送来的令牌（内联到 --pv-* 上）。这也意味着一个自己装进来的包同样看得到真实配色——预览
        仍然不是一张示意图。
      -->
      {#each installed as p (p.id)}
        <button
          type="button"
          class="pack"
          class:is-active={themeState.pack === p.id}
          class:has-error={!!p.error}
          aria-pressed={themeState.pack === p.id}
          style={previewVars(p)}
          disabled={!!p.error}
          onclick={() => void setPack(p.error ? '' : p.id)}
        >
          <span class="pack-preview">
            <span class="pk-row"><i class="pk-dot"></i><i class="pk-dot"></i></span>
            <span class="pk-line w72"></span>
            <span class="pk-line w44"></span>
            <span class="pk-pill">按钮</span>
          </span>
          <span class="pack-name">{p.name}</span>
          {#if p.error}
            <span class="pack-hint err-text">{p.error}</span>
          {:else}
            <span class="pack-hint">
              {p.author ? p.author : '已安装'}{p.version ? ' · v' + p.version : ''}
            </span>
            {#if p.problems && p.problems.length}
              <span class="pack-warn">对比度不足：{p.problems[0]}{p.problems.length > 1 ? ' 等 ' + p.problems.length + ' 处' : ''}</span>
            {/if}
          {/if}
        </button>
      {/each}
    </div>
    {#if packsProblem}<p class="field-note err-text">{packsProblem}</p>{/if}
    <p class="field-note">
      包只改令牌，不碰布局与圆角；每个包都写了浅色与深色两套，所以上面那个「浅色 / 深色」对它照样管用。
      内建的四个包每一颗文字色都经过对比度实算（<code>.dev/check-contrast.cjs</code>）；
      自己装进来的包由宿主在加载时实算一次，不合格的会在卡片上标出来。
    </p>
  </section>

  <!--
    主题商店：和插件市场同一条分发通道（同一个市场源、同样校验 sha256）。货架上的包是**一个
    theme.json**，不是 zip——主题包只有颜色，没有代码，装它也不需要授权。
  -->
  <section class="card">
    <header class="card-head">
      <h2>主题商店</h2>
      <p class="hint">
        装好的包会出现在上面的卡片里，点一下就能用。装主题包不需要授权：它只包含颜色，不包含代码。
      </p>
    </header>

    {#if storeNote}<p class="field-note">{storeNote}</p>{/if}
    {#if storeError}<p class="field-note err-text">{storeError}</p>{/if}

    {#if storeRows.length}
      <div class="store">
        {#each storeRows as row (row.id)}
          {@const have = installedVersion(row.id)}
          <div class="store-row">
            <div class="store-main">
              <span class="store-name">{row.name} <span class="store-ver">v{row.version}</span></span>
              {#if row.author}<span class="store-meta">{row.author}</span>{/if}
              {#if row.description}<span class="store-desc">{row.description}</span>{/if}
            </div>
            {#if !have}
              <button type="button" class="btn" disabled={storeBusy === row.id} onclick={() => void install(row.id)}>
                {storeBusy === row.id ? '安装中…' : '安装'}
              </button>
            {:else}
              <div class="store-actions">
                {#if have !== row.version}
                  <button type="button" class="btn" disabled={storeBusy === row.id} onclick={() => void install(row.id)}>
                    {storeBusy === row.id ? '更新中…' : `更新到 v${row.version}`}
                  </button>
                {:else}
                  <span class="store-flag">已装 v{have}</span>
                {/if}
                <button type="button" class="btn ghost" disabled={storeBusy === row.id} onclick={() => void remove(row.id)}>
                  卸载
                </button>
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
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

  /* 主题包卡片。卡片自己的底色/描边也来自它的 data-theme-pack —— 一行 `var()` 都不用改，
     因为令牌就在卡片身上生效。 */
  .packs {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(168px, 1fr));
    gap: var(--space-3);
  }
  .pack {
    appearance: none;
    text-align: left;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: var(--space-3);
    border-radius: var(--radius);
    border: 1.5px solid var(--pv-line, var(--line));
    background: var(--pv-raised, var(--bg-raised));
    color: var(--text);
    font: inherit;
    cursor: pointer;
    transition: border-color var(--dur-2) var(--ease), box-shadow var(--dur-2) var(--ease),
      transform var(--dur-2) var(--ease);
  }
  .pack:hover { transform: translateY(-1px); box-shadow: var(--sh-sm); }
  .pack.is-active { border-color: var(--accent); box-shadow: 0 0 0 3px var(--ring); }

  /* 预览：一个极小的"窗口"，用它自己的底、文字色和填充色画出来。 */
  .pack-preview {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 8px;
    margin-bottom: 2px;
    border-radius: var(--radius-sm);
    background: var(--pv-bg, var(--bg));
    border: 1px solid var(--pv-line, var(--line));
  }
  .pk-row { display: flex; gap: 4px; }
  .pk-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--pv-accent, var(--accent)); }
  .pk-line { height: 5px; border-radius: 3px; background: var(--pv-dim, var(--text-dim)); opacity: 0.45; }
  .pk-line.w72 { width: 72%; }
  .pk-line.w44 { width: 44%; }
  .pk-pill {
    align-self: flex-start;
    margin-top: 2px;
    padding: 2px 8px;
    border-radius: var(--radius-full);
    background: var(--pv-accent, var(--accent-strong));
    color: var(--pv-on-accent, var(--on-accent));
    font-size: 10px;
    font-weight: 650;
  }
  .pack-name { font-size: var(--fs-sm); font-weight: 650; color: var(--pv-text, var(--text)); }
  .pack-hint { font-size: 10.5px; color: var(--pv-dim, var(--text-dim)); line-height: 1.55; }
  .pack.has-error { border-style: dashed; cursor: default; }
  .pack.has-error .pack-name { color: var(--text-dim); }
  .pack-warn { font-size: 10.5px; color: var(--warning); line-height: 1.5; }
  .err-text { color: var(--danger); }

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
  /* 主题商店的一行：左边名字与说明，右边动作。 */
  .store { display: flex; flex-direction: column; gap: var(--space-2); }
  .store-row {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-3);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    background: var(--bg-sunken);
  }
  .store-main { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1 1 auto; }
  .store-name { font-size: var(--fs-sm); font-weight: 650; color: var(--text); }
  .store-ver { color: var(--text-dim); font-weight: 500; }
  .store-meta { font-size: 11px; color: var(--text-dim); }
  .store-desc { font-size: 12px; color: var(--text-dim); line-height: 1.55; }
  .store-actions { display: flex; align-items: center; gap: var(--space-2); flex: 0 0 auto; }
  .store-flag { font-size: 11.5px; color: var(--text-dim); }
  .btn.ghost { background: transparent; border-color: transparent; color: var(--text-dim); }
  .btn.ghost:hover { background: var(--bg-hover); color: var(--text); }</style>
