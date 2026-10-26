<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import { MainSettings, SetPetScale } from '../../../frontend/wailsjs/go/main/Market';

  let scale = $state(1);
  let loaded = $state(false);
  let error = $state('');

  onMount(async () => {
    try {
      const cfg = await MainSettings();
      scale = cfg.PetScale ?? 1;
    } catch (e) {
      error = String(e);
    } finally {
      loaded = true;
    }
  });

  // Written on `change`, not on `input`: dragging the slider fires input dozens of times
  // and each one is a whole file write plus a stat on the pet's side.
  async function commit(e: Event) {
    const value = Number((e.currentTarget as HTMLInputElement).value);
    scale = value;
    try {
      const cfg = await SetPetScale(value);
      scale = cfg.PetScale;
      error = '';
    } catch (err) {
      error = String(err);
    }
  }
</script>

<Card title="桌宠大小">
  {#if !loaded}
    <p>读取中…</p>
  {:else}
    <div class="row">
      <input type="range" min="0.5" max="3" step="0.1" value={scale} onchange={commit} />
      <output>{scale.toFixed(1)}×</output>
    </div>
    <p class="hint">改完桌宠会立刻变 —— 设置由本程序写入 settings.json，桌宠每 500ms 检查一次。</p>
  {/if}
  {#if error}<p class="err">{error}</p>{/if}
</Card>

<style>
  .row { display: flex; align-items: center; gap: var(--space-3); }
  input[type='range'] { width: 260px; accent-color: var(--accent); }
  output { font: var(--font-mono); color: var(--text); min-width: 44px; }
  .hint { margin: var(--space-3) 0 0; color: var(--text-dim); font-size: 12px; }
  .err { margin: var(--space-2) 0 0; color: var(--danger); font-size: 12px; }
</style>