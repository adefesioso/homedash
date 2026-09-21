<script>
  // A gauge: how full something is, as one bar. Red past the caller's
  // threshold (hot), greyed when the thing it measures is off-limits (dim).
  let { pct = 0, hot = false, dim = false } = $props();
  const width = $derived(Math.max(0, Math.min(100, Math.round(pct || 0))));
</script>

<span class="fill" class:dim role="meter" aria-valuenow={width} aria-valuemin="0" aria-valuemax="100">
  <span class="bar" class:hot style="width:{width}%"></span>
</span>

<style>
  .fill { display: block; height: 0.3rem; border-radius: 2px; background: var(--sunk); overflow: hidden; }
  .bar { display: block; height: 100%; background: var(--accent); border-radius: 2px; transition: width 0.4s; }
  .bar.hot { background: var(--bad); }
  .dim .bar { background: var(--muted); opacity: 0.5; }
</style>
