<script lang="ts">
  let { title = '', children } = $props();
</script>

<section>
  {#if title}<h2>{title}</h2>{/if}
  {@render children?.()}
</section>

<style>
  section {
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    padding: var(--space-4);
    margin-bottom: var(--space-4);
  }
  h2 {
    font-size: 13px;
    margin: 0 0 var(--space-3);
    color: var(--text);
  }
</style>