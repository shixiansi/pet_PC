<script lang="ts">
  import Card from '../components/Card.svelte';
</script>

<Card title="Start">
  <p>桌宠大小与位置、开机启动、快捷键、暂停/恢复</p>
  <p class="todo">此页尚未接线。设计与接口见 openspec/changes/ui-rebuild/。</p>
</Card>

<style>
  p { margin: 0 0 var(--space-2); color: var(--text-dim); }
  .todo { font-size: 12px; }
</style>