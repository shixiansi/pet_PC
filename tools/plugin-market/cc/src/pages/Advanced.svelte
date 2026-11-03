<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import { DataDir, Diagnostics } from '../lib/bridge';
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
  let dir = $state('');
  let report = $state('');
  let busy = $state(false);
  let notice = $state('');
  let error = $state('');

  onMount(async () => {
    try {
      dir = await DataDir();
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

<Card title="提醒">
  <p class="hint">
    定时提醒住在**待办插件**里：在「插件」页选中待办，右边那一栏就是它自己的设置（开关、间隔、
    活跃时段、一次提几件）。改完由它自己的常驻帧按新值走——2026-11-03 之前它只在帧重挂时才
    重读一次，所以"关了再开"要退出桌宠重开才生效，现在收到 <code>config.changed</code> 就重读。
  </p>
  <p class="hint">
    桌宠这边只剩「立刻提醒一次」（托盘菜单 / 右键菜单那一格）：那是你按下去的动作，任何插件都
    没法替你按。这一页原来还有一组「对话提醒」开关（开启 / 启动时自动开启 / 间隔 / 活跃时段…），
    它们写的是宿主 <code>settings.json</code> 里的 <code>reminder*</code> 字段，而宿主的调度器
    随「提醒搬进待办插件」一起删掉了——那些字段**没有任何读者**。一组点了没反应的开关比没有
    这组开关更糟，所以整组删掉了。
  </p>
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