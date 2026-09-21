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
  .notice {
    display: flex;
    align-items: flex-start;
    gap: 0.4rem;
    margin: 0 0 0.75rem;
    padding: 0.5rem 0.7rem;
    border-radius: 8px;
    background: var(--bad-dim);
    border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent);
    overflow-wrap: anywhere;
    animation: notice-in 0.18s ease-out;
  }
  .notice.error::before, .notice.warn::before { content: '⚠'; flex: 0 0 auto; line-height: 1.3; }
  .notice.muted { background: color-mix(in srgb, var(--muted) 10%, transparent); border-color: color-mix(in srgb, var(--muted) 26%, transparent); }
  .notice.muted::before { content: 'ℹ'; flex: 0 0 auto; line-height: 1.3; opacity: 0.75; }
  :global(.bar) > .notice, :global(.row) > .notice, :global(label) > .notice, :global(.form) > .notice { display: inline-flex; margin: 0; padding: 0.2rem 0.55rem; }
  .warn { color: var(--bad); }
  .muted { color: var(--muted); }
  .small { font-size: 0.85em; padding: 0.35rem 0.6rem; }
  .dismiss { flex: 0 0 auto; margin-left: 0.15rem; }
  @keyframes notice-in {
    from { opacity: 0; transform: translateY(-3px); }
    to { opacity: 1; transform: translateY(0); }
  }
  @media (prefers-reduced-motion: reduce) {
    .notice { animation: none; }
  }
</style>
