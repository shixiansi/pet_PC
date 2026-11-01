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
  /* 填充色是 --accent-strong，不是 --accent：后者是"当文字"的那一档（深色下是浅紫
     #a78bfa，白字压上去只有 2.6:1）。名字见 tokens.css 的"两个角色，两个名字"。 */
  button.primary { background: var(--accent-strong); border-color: var(--accent-strong); color: var(--on-accent); }
  button.primary:hover { background: var(--accent-hover); border-color: var(--accent-hover); }
</style>