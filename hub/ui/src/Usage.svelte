<script>
  import { get } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { count, usd, ago, sparkPath } from './lib/format.js';
  import Notice from './ui/Notice.svelte';
  import Fill from './ui/Fill.svelte';
  import Seg from './ui/Seg.svelte';
  import Empty from './Empty.svelte';

  // The Usage tab: what every remote's omp actually spent, over the
  // window — a job round's `message_end` usage, summed per host. The
  // point is dispatch, not cost: a remote with nothing here while
  // others carry the fleet is either being skipped or can't take work,
  // and the share bar makes that visible without reading job history.
  const windows = [{ key: 24, label: '24h' }, { key: 24 * 7, label: '7d' }, { key: 24 * 30, label: '30d' }];
  let hours = $state(24 * 7);
  let totals = $state(null);   // HostUsage[] from /api/usage, one row per host, zero included
  let series = $state({});     // hostId -> Usage[] from /api/hosts/{id}/usage
  let error = $state('');

  async function load() {
    try { totals = await get(`/usage?hours=${hours}`); error = ''; }
    catch (e) { error = e.message; }
  }
  async function loadSeries() {
    if (!totals?.length) return;
    const got = await Promise.all(totals.map((t) => get(`/hosts/${t.hostId}/usage?hours=${hours}`).then((u) => [t.hostId, u]).catch(() => null)));
    series = Object.fromEntries(got.filter(Boolean));
  }
  $effect(() => poll(load, 30_000));
  $effect(() => { hours; load(); });
  $effect(() => { if (totals) loadSeries(); });

  const fleet = $derived.by(() => {
    if (!totals) return null;
    const sum = (k) => totals.reduce((n, t) => n + t[k], 0);
    return { input: sum('input'), output: sum('output'), cacheRead: sum('cacheRead'), cacheWrite: sum('cacheWrite'), cost: sum('cost'), calls: sum('calls') };
  });
  const share = (t) => (fleet && fleet.input + fleet.output ? Math.round((100 * (t.input + t.output)) / (fleet.input + fleet.output)) : 0);
  const tokenSeries = (id) => (series[id] ?? []).map((u) => u.input + u.output);
  const lastAt = (id) => (series[id] ?? []).at(-1)?.at;
</script>

{#if error}<Notice>{error}</Notice>{/if}

<div class="bar"><Seg options={windows} bind:value={hours} label="Window" /></div>

{#if totals && totals.length === 0}
  <Empty text="No machines enrolled yet. Usage appears once a job runs on one." />
{:else if fleet}
  <section class="card plate fleet">
    <header>
      <strong>Fleet total</strong>
      <span class="grow"></span>
      <span class="muted small">{windows.find((w) => w.key === hours)?.label} across {totals.length} remote{totals.length === 1 ? '' : 's'}</span>
    </header>
    <div class="stats">
      <div class="stat"><span class="v">{count(fleet.input + fleet.output)}</span><span class="k">tokens</span></div>
      <div class="stat"><span class="v">{count(fleet.input)}</span><span class="k">input</span></div>
      <div class="stat"><span class="v">{count(fleet.output)}</span><span class="k">output</span></div>
      <div class="stat"><span class="v">{count(fleet.cacheRead)}</span><span class="k">cache read</span></div>
      <div class="stat"><span class="v">{usd(fleet.cost)}</span><span class="k">cost</span></div>
      <div class="stat"><span class="v">{fleet.calls}</span><span class="k">replies</span></div>
    </div>
  </section>

  <div class="grid">
    {#each totals as t (t.hostId)}
      {@const busy = t.input + t.output > 0}
      <section class="card plate" class:idle={!busy}>
        <header>
          <strong>{t.host}</strong>
          {#if !busy}<span class="pill">idle</span>{/if}
          <span class="grow"></span>
          <span class="muted small">{lastAt(t.hostId) ? ago(lastAt(t.hostId)) : 'no jobs yet'}</span>
        </header>
        <div class="share">
          <div class="share-row">
            <span class="v">{count(t.input + t.output)} tokens<span class="of"> · {share(t)}% of fleet</span></span>
            {#if tokenSeries(t.hostId).length > 1}
              <svg viewBox="0 0 80 20" class="spark" preserveAspectRatio="none"><title>tokens, {windows.find((w) => w.key === hours)?.label}</title><path d={sparkPath(tokenSeries(t.hostId))} /></svg>
            {/if}
          </div>
          <Fill pct={share(t)} dim={!busy} />
        </div>
        <div class="stats">
          <div class="stat"><span class="v">{count(t.input)}</span><span class="k">input</span></div>
          <div class="stat"><span class="v">{count(t.output)}</span><span class="k">output</span></div>
          <div class="stat"><span class="v">{count(t.cacheRead)}</span><span class="k">cache read</span></div>
          <div class="stat"><span class="v">{usd(t.cost)}</span><span class="k">cost</span></div>
          <div class="stat"><span class="v">{t.calls}</span><span class="k">replies</span></div>
        </div>
      </section>
    {/each}
  </div>
{:else if !error}
  <p class="muted">…</p>
{/if}

<style>
  .bar { display: flex; align-items: center; gap: 0.75rem; margin-bottom: 1rem; }
  .fleet { max-width: 52rem; margin-bottom: 1rem; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 22rem), 1fr)); gap: 1rem; align-items: start; }
  .card.idle { opacity: 0.72; }
  .stats { display: flex; flex-wrap: wrap; gap: 0.5rem 1.5rem; padding: 0.7rem 1rem 0.8rem; }
  .stat { display: grid; gap: 0.1rem; }
  .stat .v { font-family: var(--mono); font-size: 0.95rem; font-weight: 500; }
  .stat .k { font-size: 0.72em; color: var(--muted); }
  .share { padding: 0.7rem 1rem 0; }
  .share-row { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; margin-bottom: 0.3rem; }
  .share-row .v { font-family: var(--mono); font-size: 0.95rem; font-weight: 500; }
  .share-row .of { color: var(--muted); font-weight: 400; font-family: var(--sans, inherit); }
  .spark { width: 64px; height: 16px; flex: none; }
  .spark path { fill: none; stroke: var(--muted); stroke-width: 1.5; }
</style>
