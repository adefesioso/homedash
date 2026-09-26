<script>
  // A ring of parts of one whole: each segment a value and a colour, a
  // 2px gap between them, the figure that matters in the middle. A
  // segment marked `faded` is the same entity's unused share (a disk's
  // free space), drawn in its colour at low strength. Hovering a segment
  // puts its label in the middle in place of the caller's.
  let { segments = [], size = 120, thickness = 14, center = '', sub = '', label = '' } = $props();
  let hover = $state(null);

  const r = $derived((size - thickness) / 2);
  const total = $derived(segments.reduce((n, s) => n + Math.max(0, s.value || 0), 0));
  const arcs = $derived.by(() => {
    if (!total) return [];
    const gap = segments.filter((s) => s.value > 0).length > 1 ? 2 / r : 0; // 2px, as an angle
    let a = -Math.PI / 2;
    return segments.filter((s) => s.value > 0).map((s) => {
      const span = (s.value / total) * 2 * Math.PI;
      const from = a + gap / 2, to = a + Math.max(span - gap / 2, gap / 2 + 0.001);
      a += span;
      return { ...s, d: arc(from, to) };
    });
  });
  function arc(from, to) {
    const c = size / 2;
    if (to - from >= 2 * Math.PI - 0.0001) to = from + 2 * Math.PI - 0.0001;
    const x1 = c + r * Math.cos(from), y1 = c + r * Math.sin(from);
    const x2 = c + r * Math.cos(to), y2 = c + r * Math.sin(to);
    return `M${x1},${y1} A${r},${r} 0 ${to - from > Math.PI ? 1 : 0} 1 ${x2},${y2}`;
  }
</script>

<svg class="donut" width={size} height={size} viewBox="0 0 {size} {size}" role="img" aria-label={label}>
  <circle cx={size / 2} cy={size / 2} {r} fill="none" stroke="var(--sunk)" stroke-width={thickness} />
  {#each arcs as s}
    <path d={s.d} fill="none" stroke={s.color} stroke-width={thickness} opacity={s.faded ? 0.28 : 1}
      class:on={hover === s} role="presentation"
      onmouseenter={() => (hover = s)} onmouseleave={() => (hover = null)}><title>{s.title ?? s.label}</title></path>
  {/each}
  <text x="50%" y="50%" class="c" dy={sub && !hover ? '-0.15em' : '0.35em'}>{hover ? hover.short ?? hover.label : center}</text>
  {#if sub && !hover}<text x="50%" y="50%" class="s" dy="1.25em">{sub}</text>{/if}
</svg>

<style>
  .donut { flex: none; overflow: visible; }
  path { transition: opacity 0.12s; cursor: default; }
  path.on { filter: brightness(1.12); }
  text { text-anchor: middle; fill: var(--fg); }
  .c { font-family: var(--mono); font-size: 0.95rem; font-weight: 600; }
  .s { font-size: 0.68rem; fill: var(--muted); }
</style>
