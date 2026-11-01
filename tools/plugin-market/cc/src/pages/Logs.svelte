<script lang="ts">
  /*
   * 日志 —— 桌宠与控制中心写下的同一份故事，按等级、子系统与关键词筛出来。
   *
   * 为什么这一页存在：界面上的每一个失败都曾经只有一句 3.2 秒的 toast，而插件在沙箱
   * iframe 里、模型请求发生在另一个进程里，两边都没有留下任何可以事后读的证据。
   * 「对话插件不说话」这种报告以前无法回答：不知道请求有没有到宿主、被闸门拒了、
   * 还是模型那边 401 了。现在每一件都有一行。
   *
   * 这一页只读文件（`internal/logging.Read`），不和桌宠握手：两个进程共用数据目录，
   * 轮询文件是唯一不需要新协议、也不会因为哪边先启动就不一致的做法。2 秒一次，
   * 日志本身也限制在几 MB 之内。
   */
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import { LogTail } from '../lib/bridge';
  import type { LogQuery, LogView } from '../lib/bridge';
  // 复制只借用运行时的那一个接口，和高级页的诊断报告同一条路：在没有 Wails 的环境里
  // 它会抛，页面照实说「这个环境没有剪贴板」，而不是假装复制成功。
  import { ClipboardSetText } from '../../../frontend/wailsjs/runtime/runtime';

  let view = $state<LogView | null>(null);
  let loaded = $state(false);
  let busy = $state(false);
  let error = $state('');
  let notice = $state('');

  // 等级的默认值是「信息及以上」而不是「全部」：调试级的行是探针（每次指针移动、
  // 模型每一轮），默认显示它们会把真正想看的那一行挤到看不见的地方。
  let level = $state('info');
  let scope = $state('');
  let search = $state('');
  // 自动刷新默认开着，因为这一页的用途就是「一边操作一边看」。
  let live = $state(true);

  const levels: Array<[value: string, label: string]> = [
    ['debug', '全部（含调试）'],
    ['info', '信息及以上'],
    ['warn', '警告及以上'],
    ['error', '只看错误'],
  ];

  let timer: ReturnType<typeof setInterval> | null = null;

  async function load(): Promise<void> {
    busy = true;
    try {
      view = await LogTail({ level, scope, search, limit: 500 } as LogQuery);
      error = '';
    } catch (e) {
      error = String(e);
    } finally {
      busy = false;
      loaded = true;
    }
  }

  onMount(() => {
    load();
    timer = setInterval(() => {
      // 关掉自动刷新时仍然可以点「刷新」；把定时器本身停掉而不是在回调里判断，
      // 这样关掉之后一个请求也不会再发。
      if (live) load();
    }, 2000);
    return () => {
      if (timer) clearInterval(timer);
    };
  });

  // 换筛选条件立刻重读一次，不等到下一次轮询：手点一下要等两秒才动，
  // 会让人以为筛选没生效。
  async function refilter(): Promise<void> {
    await load();
  }

  async function copy(): Promise<void> {
    if (!view) return;
    const text = view.entries
      .map((e) => `${e.at} ${e.level.padEnd(5)} ${e.scope} ${e.plugin || '-'} ${e.message}`)
      .join('\n');
    try {
      await ClipboardSetText(`PetPC 日志（${view.entries.length} 行，来自 ${view.dir}）\n${text}`);
      notice = '已复制当前这些行';
    } catch {
      notice = '这个环境没有剪贴板接口，请手动选中文字复制';
    }
  }

  const levelClass = (name: string): string => 'lv-' + name.toLowerCase();
  const count = $derived(view?.entries.length ?? 0);
</script>

<Card title="日志">
  <div class="bar">
    <label class="field">
      <span>等级</span>
      <select bind:value={level} onchange={refilter}>
        {#each levels as [value, label] (value)}
          <option {value}>{label}</option>
        {/each}
      </select>
    </label>

    <label class="field">
      <span>子系统</span>
      <select bind:value={scope} onchange={refilter}>
        <option value="">全部</option>
        {#each view?.scopes ?? [] as name (name)}
          <option value={name}>{name}</option>
        {/each}
      </select>
    </label>

    <label class="field grow">
      <span>搜索</span>
      <input type="search" placeholder="错误文字、插件 id 或方法名" bind:value={search} oninput={refilter} />
    </label>

    <label class="switch">
      <input type="checkbox" bind:checked={live} />
      <span>自动刷新（2 秒）</span>
    </label>

    <Button onclick={load}>{busy ? '读取中…' : '刷新'}</Button>
    <Button onclick={copy}>复制</Button>
    {#if notice}<span class="ok">{notice}</span>{/if}
  </div>

  {#if !loaded}
    <p class="hint">读取中…</p>
  {:else if view}
    {#if view.note}
      <p class="hint">{view.note}</p>
    {/if}

    {#if count > 0}
      <ol class="log">
        {#each view.entries as e, i (e.at + ':' + i)}
          <li class={levelClass(e.level)}>
            <span class="at">{e.at}</span>
            <span class="level">{e.level}</span>
            <span class="scope">{e.scope}</span>
            <span class="plugin" class:empty={!e.plugin}>{e.plugin || '—'}</span>
            <span class="msg">{e.message}</span>
          </li>
        {/each}
      </ol>
      <p class="hint">
        共 {count} 行，最新在最上面。这一页只读最近 500 行；完整日志在下面这些文件里。
      </p>
    {/if}

    <dl class="files">
      {#each view.files as f (f.name)}
        <dt>{f.name}</dt>
        <dd>
          {#if f.err}
            <span class="bad">读不出来：{f.err}</span>
          {:else if f.bytes === 0}
            <span class="dim">还不存在</span>
          {:else}
            <span class="mono">{f.bytes.toLocaleString()} B</span>
            <span class="dim">{f.modified}</span>
          {/if}
        </dd>
      {/each}
      <dt>目录</dt>
      <dd class="mono">{view.dir}</dd>
    </dl>

    <p class="hint">
      桌宠写 <code>pet.log</code>、控制中心写 <code>market.log</code>，两个文件各自超过 2 MB 就轮转一次、
      只保留上一个版本。日志里**没有**对话正文、待办内容、插件设置的值，也没有任何密钥：
      记的是「谁调了什么、花了多久、结果如何」，所以可以直接复制给别人。
      默认级别是「信息」，插件调用失败、模型报错都会在里面；要更细的每一轮模型请求与指针事件，
      给两个程序都加 <code>PETPC_LOG_LEVEL=debug</code>（或 <code>PETPC_DEBUG=1</code>）。
    </p>
  {/if}

  {#if error}<p class="err">{error}</p>{/if}
</Card>

<style>
  .bar {
    display: flex;
    align-items: flex-end;
    gap: var(--space-3);
    flex-wrap: wrap;
    margin-bottom: var(--space-3);
  }
  .field { display: flex; flex-direction: column; gap: var(--space-1); }
  .field > span { color: var(--text-dim); font-size: 12px; }
  .field.grow { flex: 1 1 220px; }
  input[type='search'] {
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    padding: var(--space-1) var(--space-2);
  }

  /* select 与搜索框分开写：箭头只属于下拉框，搜索框带一个 V 会很怪。
     原生箭头被 appearance:none 拿掉了，得自己画（跟着 --text-dim 变色）。 */
  select {
    appearance: none;
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    padding: var(--space-1) var(--space-5) var(--space-1) var(--space-2);
    background-image:
      linear-gradient(45deg, transparent 50%, var(--text-dim) 50%),
      linear-gradient(135deg, var(--text-dim) 50%, transparent 50%);
    background-position:
      calc(100% - 14px) calc(50% + 1px),
      calc(100% - 10px) calc(50% + 1px);
    background-size: 5px 5px, 5px 5px;
    background-repeat: no-repeat;
    cursor: pointer;
  }
  .switch {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-dim);
    font-size: 12px;
    padding-bottom: var(--space-1);
  }
  input[type='checkbox'] { accent-color: var(--accent-strong); }

  .log {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 46vh;
    overflow: auto;
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    background: var(--bg-sunken);
  }
  .log li {
    display: grid;
    /* 时间 | 等级 | 子系统 | 插件 | 正文 —— 定宽的前四列让行与行能对上，
       正文自己折行，所以一条长错误不会把表格挤歪。 */
    grid-template-columns: 168px 52px 64px 200px 1fr;
    gap: var(--space-2);
    align-items: baseline;
    padding: 3px var(--space-2);
    border-bottom: 1px solid var(--line);
    font: var(--font-mono);
    font-size: 11px;
    line-height: 1.5;
  }
  .log li:last-child { border-bottom: 0; }
  .at { color: var(--text-dim); }
  .level { font-weight: 600; }
  .scope { color: var(--text-dim); }
  .plugin { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .plugin.empty { color: var(--text-dim); }
  .msg { white-space: pre-wrap; word-break: break-word; }

  /* 等级的颜色只给「等级」那一列和一条左边框，正文保持可读的正文色：
     整行染色会让 200 行里的一行错误和旁边一样刺眼，等于没有分级。 */
  .lv-debug .level { color: var(--text-dim); }
  .lv-info .level { color: var(--accent); }
  .lv-warn { box-shadow: inset 3px 0 0 var(--warn); }
  .lv-warn .level { color: var(--warn); }
  .lv-error { box-shadow: inset 3px 0 0 var(--danger); }
  .lv-error .level { color: var(--danger); }

  .files {
    display: grid;
    grid-template-columns: 140px 1fr;
    gap: var(--space-1) var(--space-3);
    margin: var(--space-3) 0 0;
    font-size: 12px;
  }
  .files dt { color: var(--text-dim); }
  .files dd { margin: 0; color: var(--text); display: flex; gap: var(--space-2); }
  .mono { font: var(--font-mono); font-size: 11px; }
  .dim { color: var(--text-dim); }
  .bad { color: var(--danger); }
  .ok { color: var(--ok); font-size: 12px; }
  .hint { margin: var(--space-3) 0 0; color: var(--text-dim); font-size: 12px; line-height: 1.6; }
  .hint code { font: var(--font-mono); font-size: 11px; }
  .err { margin: var(--space-2) 0 0; color: var(--danger); font-size: 12px; }
</style>
