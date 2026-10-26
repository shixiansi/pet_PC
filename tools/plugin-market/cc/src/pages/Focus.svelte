<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import {
    MainSettings,
    SetFocusMinutes,
    SetShortBreakMinutes,
    SetLongBreakMinutes,
    SetLongBreakEvery,
    SetAutoStartNextPhase,
  } from '../../../frontend/wailsjs/go/main/Market';

  type Cfg = {
    focusMinutes: number;
    shortBreakMinutes: number;
    longBreakMinutes: number;
    longBreakEvery: number;
    autoStartNextPhase: boolean;
  };

  let cfg = $state<Cfg | null>(null);
  let error = $state('');

  onMount(async () => {
    try {
      cfg = await MainSettings();
    } catch (e) {
      error = String(e);
    }
  });

  // Every setter returns the config the store actually SAVED, which is Normalise'd. So
  // the page shows the clamped value - typing 999 into the focus box leaves 180 on
  // screen, and the user learns the limit instead of silently keeping an impossible one.
  async function apply(run: () => Promise<Cfg>) {
    try {
      cfg = await run();
      error = '';
    } catch (e) {
      error = String(e);
    }
  }
</script>

{#snippet field(label: string, value: number, unit: string, min: number, max: number, onCommit: (v: number) => void)}
  <label class="field">
    <span>{label}</span>
    <input
      type="number"
      {min}
      {max}
      {value}
      onchange={(e) => onCommit(Number((e.currentTarget as HTMLInputElement).value))}
    />
    <em>{unit}</em>
  </label>
{/snippet}

<Card title="专注">
  {#if !cfg}
    <p class="hint">读取中…</p>
  {:else}
    {@render field('专注时长', cfg.focusMinutes, '分钟', 1, 180, (v) => apply(() => SetFocusMinutes(v)))}
    {@render field('短休息', cfg.shortBreakMinutes, '分钟', 1, 60, (v) => apply(() => SetShortBreakMinutes(v)))}
    {@render field('长休息', cfg.longBreakMinutes, '分钟', 1, 120, (v) => apply(() => SetLongBreakMinutes(v)))}
    {@render field('每几个番茄进入长休息', cfg.longBreakEvery, '个', 2, 12, (v) => apply(() => SetLongBreakEvery(v)))}

    <label class="field">
      <span>自动接续下一段</span>
      <input
        type="checkbox"
        checked={cfg.autoStartNextPhase}
        onchange={(e) => apply(() => SetAutoStartNextPhase((e.currentTarget as HTMLInputElement).checked))}
      />
      <em></em>
    </label>

    <p class="hint">
      改动只影响**下一段**：正在跑的番茄不会被改写，否则一段专注会在中途被改短。
      专注结束后仍然要等记录写下，休息才会开始。
    </p>
  {/if}
  {#if error}<p class="err">{error}</p>{/if}
</Card>

<style>
  .field {
    display: grid;
    grid-template-columns: 1fr 88px 44px;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) 0;
    border-bottom: 1px solid var(--line);
  }
  .field:last-of-type { border-bottom: 0; }
  .field span { color: var(--text); }
  .field em { color: var(--text-dim); font-style: normal; font-size: 12px; }
  input[type='number'] {
    width: 100%;
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: var(--font-mono);
    padding: var(--space-1) var(--space-2);
  }
  input[type='checkbox'] { justify-self: start; accent-color: var(--accent); }
  .hint { margin: var(--space-3) 0 0; color: var(--text-dim); font-size: 12px; line-height: 1.6; }
  .err { margin: var(--space-2) 0 0; color: var(--danger); font-size: 12px; }
</style>