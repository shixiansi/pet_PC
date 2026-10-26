<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import { MainSettings, DataDir } from '../lib/bridge';
  import type { Config } from '../lib/bridge';

  let cfg = $state<Config | null>(null);
  let dir = $state('');
  let error = $state('');

  onMount(async () => {
    try {
      cfg = await MainSettings();
      dir = await DataDir();
    } catch (e) {
      error = String(e);
    }
  });
</script>

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
  .hint { margin: var(--space-3) 0 0; color: var(--text-dim); font-size: 12px; line-height: 1.6; }
  .err { margin: var(--space-2) 0 0; color: var(--danger); font-size: 12px; }
</style>