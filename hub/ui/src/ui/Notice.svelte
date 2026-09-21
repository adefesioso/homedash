<script>
  // One line the panel has to say about itself: an error from an action
  // or a load, a warning, a status. `ondismiss` adds the × that clears it;
  // a notice without one stays until whatever set it changes its mind.
  // The root keeps the kind as a class (`error`) so tests find it as before.
  // On its own it is a paragraph; inside a .bar or .row it sits in the line.
  let { kind = 'error', small = false, ondismiss, children } = $props();
</script>

<p class="notice {kind}" class:small>
  <span>{@render children()}</span>
  {#if ondismiss}<button class="dismiss" onclick={ondismiss} aria-label="Dismiss {kind === 'error' ? 'error' : 'notice'}">×</button>{/if}
</p>

<style>
  .notice { display: flex; align-items: baseline; gap: 0.25rem; margin: 0 0 0.75rem; overflow-wrap: anywhere; }
  :global(.bar) > .notice, :global(.row) > .notice, :global(label) > .notice, :global(.form) > .notice { display: inline-flex; margin: 0; }
  .warn { color: var(--bad); }
  .muted { color: var(--muted); }
  .small { font-size: 0.85em; }
</style>
