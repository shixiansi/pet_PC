<script lang="ts">
  import { untrack } from 'svelte';
  import type { PluginRow } from '../lib/bridge';
  import { mountPlugin, windowTheme, type PluginFrame } from '../lib/plugin-frame';

  // The installed row for the plugin whose interface is being hosted.
  let { inst }: { inst: PluginRow } = $props();

  // The stable mount point. The iframe is created INSIDE this node by plugin-frame.ts and is
  // never a template child: a re-created iframe is a destroyed document (half-filled form,
  // scroll position, an already-answered handshake). See the header of plugin-frame.ts.
  let mount = $state<HTMLDivElement | null>(null);

  /**
   * What decides a mount, as ONE string.
   *
   * It is a string, and not `inst`, on purpose: `refresh()` hands the page a NEW row object
   * every time it runs, so an effect that read `inst.*` directly would tear the frame down
   * after every permission toggle or enable flip - i.e. exactly when the user is working in
   * the plugin's own interface. `$derived` only notifies downstream when its VALUE changes.
   *
   * `granted` IS part of the key, i.e. changing permissions restarts the interface. The
   * alternative - posting a second `init` at the live document - was rejected: every bundled
   * plugin's `onInit` re-runs its view setup and re-reads its config, and two of them also
   * start timers at load time. A restart is a path the plugins already support (switching
   * plugins does it), and a plugin running with the permissions the user just ticked is more
   * honest than one still holding the old set. The permission panel says so in words.
   *
   * An empty key means "nothing to mount": not installed/enabled, or no entry.
   */
  const key = $derived.by(() => {
    const id = inst?.ID ?? '';
    const entry = inst?.Manifest?.entry ?? '';
    if (!id || !entry || !inst?.Enabled) return '';
    const granted = [...(inst?.Granted ?? [])].sort().join(',');
    return [id, inst?.Version ?? '', entry, granted].join('|');
  });

  $effect(() => {
    const target = mount;
    const mountKey = key;
    if (!target || !mountKey) return;
    const frame: PluginFrame = untrack(() =>
      mountPlugin(
        target,
        {
          pluginId: inst.ID,
          version: inst.Version || '',
          entry: inst.Manifest?.entry ?? '',
          permissions: inst.Manifest?.permissions ?? [],
          granted: inst.Granted ?? [],
        },
        windowTheme(),
      ),
    );
    return () => frame.destroy();
  });
</script>

<div class="frame-host" bind:this={mount}></div>

<style>
  .frame-host {
    flex: 1 1 auto;
    min-height: 0;
    /* An iframe's default size is 300x150 and `min-width: auto` would turn that into its
       min-content width, i.e. the frame would decide the column's width instead of the
       other way round. Zero here and in plugin-frame.ts's inline styles: measured, not
       guessed (scratch/layout-cc.cjs at 720x520). */
    min-width: 0;
    display: flex;
  }
  /* The child is not Svelte's (plugin-frame.ts appends it), so the rule has to be :global.
     Its box rules also live there; this only makes it fill the host. */
  .frame-host > :global(iframe) {
    flex: 1 1 auto;
  }
</style>
