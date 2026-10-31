<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import {
    MainSettings,
    DataDir,
    Diagnostics,
    SetReminderEnabled,
    SetReminderOnStartup,
    SetRemindWhenEmpty,
    SetReminderCadence,
    SetReminderWindow,
    SetReminderDisplay,
  } from '../lib/bridge';
  import type { Config } from '../lib/bridge';
  // Copying is the only thing this page wants from the runtime, and the generated wrapper is
  // the supported way in. Outside Wails it throws ("window.runtime is undefined") and the
  // page says so rather than pretending the clipboard worked.
  import { ClipboardSetText } from '../../../frontend/wailsjs/runtime/runtime';

  // 高级：剩下的是三件"这个文件/这份报告只有这里能看"的事——提醒的偏好、诊断报告、数据目录。
  //
  // 它曾经还有两张卡片：**密钥**（读 llm-key.bin 报"已设置"）和**模型**（显示宿主 Config 里
  // 那 13 个 llm* 字段）。两者都随对话变成插件而退休：密钥按插件隔离存在
  // plugin-data/<id>/secret-*.bin，模型的每一项现在都是插件自己的配置——它们的编辑入口是
  // 「插件」页（选中插件，右栏就是它自己声明的设置项），而那张 llm-key.bin 只留给一次性迁移
  // 读。留着它们比删掉更糟：一行"未设置"会让人去找一个不存在的开关。
  let cfg = $state<Config | null>(null);
  let dir = $state('');
  let report = $state('');
  let busy = $state(false);
  let notice = $state('');
  let error = $state('');

  onMount(async () => {
    try {
      cfg = await MainSettings();
      dir = await DataDir();
    } catch (e) {
      error = String(e);
    }
  });

  // One writer for every switch and number on this page: each setter answers with the config
  // the store SAVED (Normalise included), so the page shows what was actually stored rather
  // than what was typed - typing 999 into an interval leaves the clamped value on screen.
  async function save(run: () => Promise<Config>) {
    try {
      cfg = await run();
      error = '';
    } catch (e) {
      error = String(e);
    }
  }

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

<Card title="对话提醒">
  {#if cfg}
    <!-- A `const` copy, not the state variable: Svelte narrows `cfg` for the markup but not
         inside the event closures, and a handler that reads `c.reminderTo` would have to
         pretend it cannot be null. `c` IS the narrowed value. -->
    {@const c = cfg}
    <label class="switch">
      <input type="checkbox" checked={c.reminderEnabled} onchange={(e) => save(() => SetReminderEnabled((e.currentTarget as HTMLInputElement).checked))} />
      <span>开启对话提醒</span>
    </label>
    <label class="switch">
      <input type="checkbox" checked={c.reminderOnStartup} onchange={(e) => save(() => SetReminderOnStartup((e.currentTarget as HTMLInputElement).checked))} />
      <span>启动时自动开启提醒</span>
    </label>
    <label class="switch">
      <input type="checkbox" checked={cfg.remindWhenEmpty} onchange={(e) => save(() => SetRemindWhenEmpty((e.currentTarget as HTMLInputElement).checked))} />
      <span>没有待办时也提醒</span>
    </label>

    <div class="field">
      <span>提醒间隔</span>
      <div class="inline">
        <input
          type="number"
          min="5"
          max="240"
          step="5"
          value={c.reminderIntervalMin}
          onchange={(e) =>
            save(() => SetReminderCadence(Number((e.currentTarget as HTMLInputElement).value), c.reminderJitterMaxMin))}
        />
        <span class="unit">分钟，另加随机</span>
        <input
          type="number"
          min="0"
          max="60"
          step="1"
          value={c.reminderJitterMaxMin}
          onchange={(e) => save(() => SetReminderCadence(c.reminderIntervalMin, Number((e.currentTarget as HTMLInputElement).value)))}
        />
        <span class="unit">分钟</span>
      </div>
    </div>

    <div class="field">
      <span>活跃时段</span>
      <div class="inline">
        <input type="time" value={c.reminderFrom} onchange={(e) => save(() => SetReminderWindow((e.currentTarget as HTMLInputElement).value, c.reminderTo))} />
        <span class="unit">到</span>
        <input type="time" value={c.reminderTo} onchange={(e) => save(() => SetReminderWindow(c.reminderFrom, (e.currentTarget as HTMLInputElement).value))} />
      </div>
    </div>

    <div class="field">
      <span>每次显示</span>
      <div class="inline">
        <input
          type="number"
          min="1"
          max="5"
          step="1"
          value={c.reminderMaxItems}
          onchange={(e) => save(() => SetReminderDisplay(Number((e.currentTarget as HTMLInputElement).value), c.reminderAutoHideSec))}
        />
        <span class="unit">条，停留</span>
        <input
          type="number"
          min="5"
          max="60"
          step="1"
          value={c.reminderAutoHideSec}
          onchange={(e) => save(() => SetReminderDisplay(c.reminderMaxItems, Number((e.currentTarget as HTMLInputElement).value)))}
        />
        <span class="unit">秒</span>
      </div>
    </div>

    <p class="hint">
      这些是**待办插件的**提醒参数：循环跑在它的常驻帧里，读的是同一份
      <code>settings.json</code>，所以改完立刻按新值走，不需要重启。桌宠这边只剩「立刻提醒一次」——
      那是你按下去的动作，任何插件都没法替你按。
    </p>
  {:else}
    <p class="hint">读取中…</p>
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
      读取索引时的警告、日志尾。日志一直在写（<code>logs\pet.log</code> 与
      <code>logs\market.log</code>，默认 INFO 级），报告里只有最后 40 行——想看更多，用左栏的「日志」页。
    </p>
  {/if}
</Card>

<Card title="数据">
  <dl>
    <dt>数据目录</dt><dd class="mono">{dir || '读取中…'}</dd>
    <dt>设置</dt><dd class="mono">settings.json（本程序写）</dd>
    <dt>运行时状态</dt><dd class="mono">config.json（桌宠写：位置/开机启动/提醒开关）</dd>
    <dt>插件</dt><dd class="mono">plugins\&lt;id&gt;\ 与 plugin-data\&lt;id&gt;\（插件自己写）</dd>
  </dl>
  <p class="hint">
    一个文件只有一个写者 —— 这是这两个程序能同时跑而不互相覆盖的前提。待办列表已经不在这里了：
    它和番茄钟的记录一样，住在自己插件的那份 <code>plugin-data</code> 里。
  </p>
</Card>

{#if error}<p class="err">{error}</p>{/if}

<style>
  dl { display: grid; grid-template-columns: 140px 1fr; gap: var(--space-2) var(--space-3); margin: 0; }
  dt { color: var(--text-dim); }
  dd { margin: 0; color: var(--text); }
  .mono { font: var(--font-mono); }
  .row { display: flex; align-items: center; gap: var(--space-2); }
  .switch {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) 0;
    border-bottom: 1px solid var(--line);
    color: var(--text);
  }
  .switch:last-of-type { border-bottom: 0; }
  input[type='checkbox'] { accent-color: var(--accent); }
  .field {
    display: grid;
    grid-template-columns: 88px 1fr;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) 0;
    border-bottom: 1px solid var(--line);
  }
  .field:last-of-type { border-bottom: 0; }
  .inline { display: flex; align-items: center; gap: var(--space-2); flex-wrap: wrap; }
  .inline input[type='number'] { width: 72px; }
  .inline input[type='time'] { width: 110px; }
  .field input {
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: var(--font-mono);
    padding: var(--space-1) var(--space-2);
  }
  .unit { color: var(--text-dim); font-size: 12px; }
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
