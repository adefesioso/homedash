<script>
  import Fill from './Fill.svelte';
  // One figure a machine reported, over the word for it: 12 / cores.
  // `of` is the whole the figure is part of (used / total); `pct` draws
  // a Fill under it.
  let { label, of, pct, hot = false, min, children } = $props();
</script>

<div class="stat" style:min-width={min}>
  <span class="v">{@render children()}{#if of != null}<span class="of"> / {of}</span>{/if}</span>
  <span class="k">{label}</span>
  {#if pct != null}<Fill {pct} {hot} />{/if}
</div>

<style>
  .stat { display: grid; gap: 0.1rem; }
  .v { font-family: var(--mono); font-size: 0.95rem; font-weight: 500; display: flex; align-items: center; gap: 0.35rem; }
  .of { color: var(--muted); font-weight: 400; }
  .k { font-size: 0.72em; color: var(--muted); }
  .stat :global(.fill) { margin-top: 0.25rem; }
</style>
