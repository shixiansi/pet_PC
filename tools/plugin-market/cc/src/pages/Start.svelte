<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import {
    MainSettings,
    SetPetScale,
    SetSnapToEdge,
    SetAutostart,
    SetGlobalHotkeys,
    HotkeyLabels,
    ChoosePetImage,
    ResetPetImage,
  } from '../lib/bridge';
  import type { Config, HotkeyView } from '../lib/bridge';

  // `cfg` is the generated store.Config: this page names the fields the host sends, and a
  // typo is a compile error rather than an `undefined` on screen. It used to read
  // `cfg.PetScale` (the host sends `petScale`), which pinned the slider at 1.0 forever.
  let cfg = $state<Config | null>(null);
  let scale = $state(1);
  let hotkeys = $state<HotkeyView | null>(null);
  let loaded = $state(false);
  let error = $state('');

  onMount(async () => {
    try {
      cfg = await MainSettings();
      scale = cfg.petScale;
      hotkeys = await HotkeyLabels();
    } catch (e) {
      error = String(e);
    } finally {
      loaded = true;
    }
  });

  // Written on `change`, not on `input`: dragging the slider fires input dozens of times
  // and each one is a whole file write plus a stat on the pet's side.
  async function commitScale(e: Event) {
    const value = Number((e.currentTarget as HTMLInputElement).value);
    scale = value;
    await save(() => SetPetScale(value)).then((next) => {
      // The setter answers with the config the store SAVED, so the number on screen is the
      // clamped one the pet will actually use, not the one the slider was dragging to.
      if (next) scale = next.petScale;
    });
  }

  async function save(run: () => Promise<Config>): Promise<Config | null> {
    try {
      const next = await run();
      cfg = next;
      error = '';
      return next;
    } catch (e) {
      error = String(e);
      return null;
    }
  }
</script>

<Card title="桌宠大小">
  {#if !loaded}
    <p>读取中…</p>
  {:else}
    <div class="row">
      <input type="range" min="0.5" max="3" step="0.1" value={scale} onchange={commitScale} />
      <output>{scale.toFixed(1)}×</output>
    </div>
    <p class="hint">改完桌宠会立刻变 —— 设置由本程序写入 settings.json，桌宠每 500ms 检查一次。</p>
  {/if}
</Card>

<Card title="桌宠图片">
  {#if !cfg}
    <p class="hint">读取中…</p>
  {:else}
    <dl>
      <dt>当前</dt>
      <dd class="mono">{cfg.petImageFile ? cfg.petImageFile : '默认图片'}</dd>
    </dl>
    <div class="row">
      <Button variant="primary" onclick={() => save(ChoosePetImage)}>更换图片…</Button>
      <Button onclick={() => save(ResetPetImage)}>恢复默认</Button>
    </div>
    <p class="hint">
      图片会**复制**进数据目录（再由宿主换个带时间戳的地址），所以原文件挪走也不影响。
      桌宠那边一秒内重画 —— 它盯着这个设置项，不是等你重启。
    </p>
  {/if}
</Card>

<Card title="外观与启动">
  {#if !cfg}
    <p class="hint">读取中…</p>
  {:else}
    <label class="switch">
      <input
        type="checkbox"
        checked={cfg.snapToEdge}
        onchange={(e) => save(() => SetSnapToEdge((e.currentTarget as HTMLInputElement).checked))}
      />
      <span>拖动后自动贴边</span>
    </label>

    <label class="switch">
      <input
        type="checkbox"
        checked={cfg.autostart}
        onchange={(e) => save(() => SetAutostart((e.currentTarget as HTMLInputElement).checked))}
      />
      <span>开机自动启动</span>
    </label>

    <label class="switch">
      <input
        type="checkbox"
        checked={cfg.globalHotkeys}
        onchange={(e) => save(() => SetGlobalHotkeys((e.currentTarget as HTMLInputElement).checked))}
      />
      <span>用全局快捷键隐藏 / 显示桌宠</span>
    </label>

    {#if hotkeys}
      <p class="hint">
        <strong>{hotkeys.show || '?'}</strong> 显示 · <strong>{hotkeys.hide || '?'}</strong> 隐藏 —— 全局生效，
        所以开启后会占用这两个组合键。键位定义在宿主里（internal/desktop），这一页只显示，不复制一份。
      </p>
    {/if}
    <p class="hint">
      开机启动会写 HKCU 的 Run 项。这三项都是即时生效的：桌宠每秒看一次设置文件，快捷键会当场重新登记。
    </p>
  {/if}
</Card>

<!-- One error line for the whole page: every card here writes the same preference file, so
     the reason a write failed belongs to the page, not to the card that happened to be
     clicked first. -->
{#if error}<p class="err">{error}</p>{/if}

<style>
  .row { display: flex; align-items: center; gap: var(--space-3); }
  input[type='range'] { width: 260px; accent-color: var(--accent-strong); }
  output { font: var(--font-mono); color: var(--text); min-width: 44px; }
  .switch {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) 0;
    border-bottom: 1px solid var(--line);
    color: var(--text);
  }
  .switch:last-of-type { border-bottom: 0; }
  input[type='checkbox'] { accent-color: var(--accent-strong); }
  .hint { margin: var(--space-3) 0 0; color: var(--text-dim); font-size: 12px; line-height: 1.6; }
  .err { margin: var(--space-2) 0 0; color: var(--danger); font-size: 12px; }
</style>
