<script>
  // A segmented switch: one of a few views, with a count on each. The
  // buttons are real buttons, so keyboard and tests reach them by name.
  let { options, value = $bindable(), label } = $props();
</script>

<div class="seg" role="group" aria-label={label}>
  {#each options as o (o.key)}
    <button class:active={value === o.key} aria-pressed={value === o.key} onclick={() => (value = o.key)}>{o.label}{#if o.n != null} <span class="n">{o.n}</span>{/if}</button>
  {/each}
</div>

<style>
  .seg { display: inline-flex; background: var(--sunk); border-radius: var(--r); padding: 2px; }
  .seg button { background: none; border: 0; box-shadow: none; color: var(--muted); padding: 0.3rem 0.7rem; }
  .seg button.active { background: var(--card); color: var(--fg); box-shadow: var(--shadow); }
  .n { font-family: var(--mono); font-size: 0.8em; opacity: 0.8; margin-left: 0.2rem; }
</style>
