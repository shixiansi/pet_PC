<script lang="ts">
  let {
    variant = 'default',
    onclick,
    children,
  }: { variant?: 'default' | 'primary'; onclick?: () => void; children?: any } = $props();
</script>

<button class={variant} {onclick}>{@render children?.()}</button>

<style>
  button {
    appearance: none;
    border: 1px solid var(--line);
    background: var(--bg-raised);
    color: var(--text);
    font: inherit;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  button:hover { border-color: var(--accent-dim); }
  button.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
</style>