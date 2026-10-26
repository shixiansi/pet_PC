<script lang="ts">
  import Card from '../components/Card.svelte';
</script>

<Card title="Records">
  <p>已完成项、番茄记录、按天统计</p>
  <p class="todo">此页尚未接线。设计与接口见 openspec/changes/ui-rebuild/。</p>
</Card>

<style>
  p { margin: 0 0 var(--space-2); color: var(--text-dim); }
  .todo { font-size: 12px; }
</style>