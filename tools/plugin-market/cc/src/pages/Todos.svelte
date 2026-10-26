<script lang="ts">
  import Card from '../components/Card.svelte';
</script>

<Card title="Todos">
  <p>完整待办列表与搜索（第一阶段只读：todos.json 的写者是桌宠）</p>
  <p class="todo">此页尚未接线。设计与接口见 openspec/changes/ui-rebuild/。</p>
</Card>

<style>
  p { margin: 0 0 var(--space-2); color: var(--text-dim); }
  .todo { font-size: 12px; }
</style>