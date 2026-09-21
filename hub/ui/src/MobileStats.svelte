<script>
  import { bytes } from './lib/format.js';
  import Stat from './ui/Stat.svelte';

  // What a paired Android phone's own status push last said. Puppeteering
  // is stats-only for now (see docs/pooling/mobile.md) — there is nothing
  // here to change on the phone, only to read.
  let { f } = $props();
  const storagePct = (f) => (f.storageTotal ? Math.round((100 * (f.storageTotal - f.storageFree)) / f.storageTotal) : null);
</script>

{#if f.battery == null && !f.network && !f.screen}
  <p class="muted none">Nothing reported yet.</p>
{:else}
  <div class="gauges">
    {#if f.battery != null}
      <Stat label="battery{f.charging ? ', charging' : ''}" pct={f.battery} hot={f.battery < 15 && !f.charging}>{f.battery}%</Stat>
    {/if}
    {#if f.storageTotal}
      <Stat label="storage free of {bytes(f.storageTotal)}" pct={storagePct(f)} hot={storagePct(f) > 90}>{bytes(f.storageFree)}</Stat>
    {/if}
  </div>
  <div class="chips">
    {#if f.network}<span class="pill">{f.network}</span>{/if}
    {#if f.screen}<span class="pill">screen {f.screen}</span>{/if}
    {#if f.foregroundApp}<span class="pill">foreground <b>{f.foregroundApp}</b></span>{/if}
    {#if f.model}<span class="pill muted">{f.model}{f.androidVersion ? ` · Android ${f.androidVersion}` : ''}</span>{/if}
  </div>
{/if}

<style>
  .gauges { display: grid; grid-template-columns: repeat(auto-fill, minmax(7.5rem, 1fr)); gap: 0.6rem 1rem; padding: 0.7rem 1rem 0.5rem; }
  .none { padding: 0.7rem 1rem 0.3rem; margin: 0; }
  .chips { display: flex; gap: 0.35rem 0.5rem; flex-wrap: wrap; align-items: center; padding: 0.3rem 1rem 0.7rem; font-size: 0.85em; }
  .chips .pill b { font-weight: 600; }
</style>
