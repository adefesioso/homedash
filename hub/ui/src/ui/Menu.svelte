<script>
  import Icon from '../Icon.svelte';
  // An overflow menu: the actions a row carries without showing, behind
  // a ⋯. Choosing an item, clicking elsewhere or Escape closes it; it
  // opens upward when there is no room below. Items are plain buttons;
  // an <hr> separates the destructive ones.
  let { label = 'More actions', disabled = false, children } = $props();
  let open = $state(false);
  let up = $state(false);
  let root;

  function toggle() {
    if (!open) up = root.getBoundingClientRect().bottom + 240 > innerHeight;
    open = !open;
  }
  $effect(() => {
    if (!open) return;
    const away = (e) => { if (!root.contains(e.target)) open = false; };
    const key = (e) => { if (e.key === 'Escape') { e.stopPropagation(); open = false; } };
    addEventListener('pointerdown', away);
    addEventListener('keydown', key, true);
    return () => { removeEventListener('pointerdown', away); removeEventListener('keydown', key, true); };
  });
</script>

<div class="menu" bind:this={root}>
  <button class="quiet more" {disabled} aria-label={label} title={label} aria-haspopup="menu" aria-expanded={open} onclick={toggle}><Icon name="more" /></button>
  {#if open}
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div class="list" class:up role="menu" tabindex="-1" onclick={(e) => { if (e.target.closest('button')) open = false; }}>{@render children()}</div>
  {/if}
</div>

<style>
  .menu { position: relative; display: inline-flex; }
  .more { padding: 0.42rem 0.5rem; }
  .list {
    position: absolute; right: 0; top: calc(100% + 4px); z-index: 20; min-width: 12rem;
    display: grid; padding: 0.3rem; border-radius: var(--r-lg);
    background: var(--card); box-shadow: 0 8px 24px rgba(0, 0, 0, 0.14), 0 0 0 1px var(--line);
    animation: menu-in 0.12s ease-out;
  }
  .list.up { top: auto; bottom: calc(100% + 4px); }
  .list :global(button) {
    display: flex; align-items: center; gap: 0.55rem; width: 100%; text-align: left;
    background: none; border: 0; box-shadow: none; padding: 0.45rem 0.6rem; font-weight: 450; color: var(--fg); white-space: nowrap;
  }
  .list :global(button:hover:not(:disabled)) { background: var(--sunk); }
  .list :global(button.danger) { color: var(--bad); }
  .list :global(button.danger:hover:not(:disabled)) { background: var(--bad-dim); }
  .list :global(button svg) { color: var(--muted); flex: none; }
  .list :global(button.danger svg) { color: inherit; }
  .list :global(hr) { border: 0; border-top: 1px solid var(--line); margin: 0.3rem 0.2rem; }
  @keyframes menu-in { from { opacity: 0; transform: translateY(-2px); } to { opacity: 1; transform: none; } }
</style>
