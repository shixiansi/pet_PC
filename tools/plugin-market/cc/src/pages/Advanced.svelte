<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import { MainSettings, DataDir, HostKey, Diagnostics } from '../lib/bridge';
  import type { Config, HostKeyState } from '../lib/bridge';
  // Copying is the only thing this page wants from the runtime, and the generated wrapper is
  // the supported way in. Outside Wails it throws ("window.runtime is undefined") and the
  // page says so rather than pretending the clipboard worked.
  import { ClipboardSetText } from '../../../frontend/wailsjs/runtime/runtime';

  let cfg = $state<Config | null>(null);
  let dir = $state('');
  let key = $state<HostKeyState | null>(null);
  let report = $state('');
  let busy = $state(false);
  let notice = $state('');
  let error = $state('');

  onMount(async () => {
    try {
      cfg = await MainSettings();
      dir = await DataDir();
      key = await HostKey();
    } catch (e) {
      error = String(e);
    }
  });

  // Built on demand, not on mount: it stats every file in the data directory and reads a log
  // tail, which is wasted work for the 99 visits out of 100 that are just "what is my data
  // directory again". It is also the one thing on this page that can show STALE state, so it
  // should be something the user asked for a moment ago.
  async function generate() {
    busy = true;
    notice = '';
    try {
      report = await Diagnostics();
      error = '';
    } catch (e) {
      error = String(e);
    } finally {
      busy = false;
    }
  }

  async function copy() {
    try {
      await ClipboardSetText(report);
      notice = '已复制到剪贴板';
    } catch {
      notice = '这个环境没有剪贴板接口，请手动选中上面的文字复制';
    }
  }
</script>

<Card title="密钥">
  {#if !key}
    <p class="hint">读取中…</p>
  {:else}
    <dl>
      <dt>大模型密钥</dt>
      <dd>{key.Present ? '已设置（DPAPI 密文）' : '未设置'}</dd>
      <dt>文件</dt>
      <dd class="mono">{key.Path}</dd>
    </dl>
    <p class="hint">
      本程序只**看这个文件在不在**，不读、不解密、也不写。密钥只在宿主进程内解密——把它搬到这里，
      就等于多了一处能泄漏它的地方。要改密钥，用桌宠那边的设置，或者用插件自己的设置页。
    </p>
  {/if}
</Card>

<Card title="模型">
  {#if !cfg}
    <p class="hint">读取中…</p>
  {:else}
    <dl>
      <dt>开关</dt><dd>{cfg.llmEnabled ? '已启用' : '已关闭'}</dd>
      <dt>服务商预设</dt><dd>{cfg.llmProvider || '（未选）'}</dd>
      <dt>接口地址</dt><dd class="mono">{cfg.llmBaseUrl || '（未填）'}</dd>
      <dt>模型</dt><dd class="mono">{cfg.llmModel || '（未填）'}</dd>
      <dt>每日上限</dt><dd>{cfg.llmDailyBudget} 次</dd>
    </dl>
    <p class="hint">
      模型配置目前由**对话插件**自己保存，这一页只显示；密钥不在任何 JSON 里
      （DPAPI 加密，且只在宿主进程内解密）。
    </p>
  {/if}
</Card>

<Card title="诊断">
  <div class="row">
    <Button variant="primary" onclick={generate}>{busy ? '生成中…' : '生成诊断报告'}</Button>
    {#if report}<Button onclick={copy}>复制</Button>{/if}
    {#if notice}<span class="ok">{notice}</span>{/if}
  </div>
  {#if report}
    <pre class="report">{report}</pre>
    <p class="hint">
      报告里只有文件大小、时间戳、插件清单和日志尾：**没有任何密钥，也没有待办与记录的正文**。
      贴给别人时不用再删东西。
    </p>
  {:else}
    <p class="hint">
      一份可以直接贴出来的文本：数据目录里每个文件的大小与时间、已装插件与它们的 manifest 状态、
      读取索引时的警告、日志尾。<code>debug.log</code> 只在桌宠以 <code>PETPC_DEBUG=1</code>
      启动时才会被写，报告会说明它为什么不在。
    </p>
  {/if}
</Card>

<Card title="数据">
  <dl>
    <dt>数据目录</dt><dd class="mono">{dir || '读取中…'}</dd>
    <dt>设置</dt><dd class="mono">settings.json（本程序写）</dd>
    <dt>运行时状态</dt><dd class="mono">config.json（桌宠写：位置/开机启动/提醒开关）</dd>
    <dt>数据</dt><dd class="mono">todos.json · pomodoro.json · pomodoro-records.json</dd>
  </dl>
  <p class="hint">一个文件只有一个写者 —— 这是这两个程序能同时跑而不互相覆盖的前提。</p>
</Card>

{#if error}<p class="err">{error}</p>{/if}

<style>
  dl { display: grid; grid-template-columns: 140px 1fr; gap: var(--space-2) var(--space-3); margin: 0; }
  dt { color: var(--text-dim); }
  dd { margin: 0; color: var(--text); }
  .mono { font: var(--font-mono); }
  .row { display: flex; align-items: center; gap: var(--space-2); }
  .ok { color: var(--ok); font-size: 12px; }
  .report {
    margin: var(--space-3) 0 0;
    padding: var(--space-3);
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: var(--font-mono);
    font-size: 11px;
    line-height: 1.6;
    max-height: 320px;
    overflow: auto;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .hint { margin: var(--space-3) 0 0; color: var(--text-dim); font-size: 12px; line-height: 1.6; }
  .hint code { font: var(--font-mono); font-size: 11px; }
  .err { margin: var(--space-2) 0 0; color: var(--danger); font-size: 12px; }
</style>
