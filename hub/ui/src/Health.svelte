<script>
  import { get } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { bytes, ago } from './lib/format.js';
  import Notice from './ui/Notice.svelte';
  import Stat from './ui/Stat.svelte';

  // The Health tab: the hub drawn the way a host card draws a remote —
  // what the machine reports about itself now, and one line per thing
  // the hub has to keep running, each with its reason. Nothing here is a
  // control; the line says where the fix lives.
  let h = $state(null);
  let error = $state('');

  async function load() {
    try { h = await get('/hub/health'); error = ''; }
    catch (e) { error = e.message; }
  }
  $effect(() => poll(load, 15_000));

  const words = { ok: 'Well', warn: 'Needs a look', bad: 'Trouble' };
  const led = { ok: 'ok', warn: 'busy', bad: 'bad', off: '' };
  const memPct = (m) => (m.memTotal ? Math.round((100 * m.memUsed) / m.memTotal) : 0);
  const diskPct = (d) => (d.diskSize ? Math.round((100 * (d.diskSize - d.diskFree)) / d.diskSize) : 0);
  const loadPct = (m) => (m.cores ? Math.round((100 * m.load1) / m.cores) : 0);
  // The same band every meter reads by: past 70% a look, past 90% trouble.
  const level = (pct) => (pct >= 90 ? 'bad' : pct >= 70 ? 'warn' : 'ok');
  // uptime in the largest unit that reads: 3d 4h, 5h 12m, 40m.
  function uptime(s) {
    const d = Math.floor(s / 86400), hr = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
    if (d) return `${d}d ${hr}h`;
    if (hr) return `${hr}h ${m}m`;
    return `${m}m`;
  }
</script>

{#if error}<Notice>{error}</Notice>{/if}

{#if h}
  {@const m = h.machine}
  {@const d = h.stateDir}
  <section class="card plate" class:trouble={h.state === 'bad'}>
    <header>
      <span class="led {led[h.state]}" title={words[h.state]}></span>
      <strong>{m.hostname}</strong>
      <span class="pill {h.state === 'ok' ? 'ok' : h.state === 'bad' ? 'bad' : 'accent'}">{words[h.state]}</span>
      <span class="grow"></span>
      <span class="muted small">{m.os}{m.os && m.kernel ? ' · ' : ''}{m.kernel} {m.arch}</span>
      <span class="muted small">read {ago(h.at)}</span>
    </header>
    <div class="meters">
      <Stat label="memory of {bytes(m.memTotal)}" pct={memPct(m)} level={level(memPct(m))} min="11rem">{bytes(m.memUsed)}</Stat>
      <Stat label="load on {m.cores} cores" pct={loadPct(m)} level={level(loadPct(m))} min="11rem">{m.load1.toFixed(2)}</Stat>
      <Stat label="{d.path} free of {bytes(d.diskSize)}" pct={diskPct(d)} level={level(diskPct(d))} min="11rem">{bytes(d.diskFree)}</Stat>
    </div>
    <div class="stats">
      <Stat label="cores">{m.cores}</Stat>
      <Stat label="machine up">{uptime(m.uptime)}</Stat>
      <Stat label="hub up">{uptime((Date.now() - new Date(h.process.started).getTime()) / 1000)}</Stat>
      <Stat label="hub process">{bytes(h.process.rss)}</Stat>
      <Stat label="open sessions">{h.process.sessions}</Stat>
      <Stat label="state db">{bytes(d.dbSize)}</Stat>
    </div>
    <div class="summary">
      {#each ['ok', 'warn', 'bad'] as s}
        {@const n = h.checks.filter((c) => c.state === s).length}
        {#if n}<span class="tally {s}"><span class="led {led[s]}"></span>{n} {words[s].toLowerCase()}</span>{/if}
      {/each}
    </div>
    <ul class="checks">
      {#each h.checks as c (c.name)}
        <li class="check {c.state}">
          <span class="led {led[c.state]}" class:off={c.state === 'off'}></span>
          <span class="name">{c.name}</span>
          <span class="detail" class:muted={c.state === 'off'}>{c.detail}</span>
        </li>
      {/each}
    </ul>
  </section>
{:else if !error}
  <p class="muted">…</p>
{/if}

<style>
  .plate { max-width: 52rem; }
  /* The three headline ratios, as bigger meters than a plain Stat gets
     elsewhere — this card leads with them. */
  .meters { display: flex; flex-wrap: wrap; gap: 0.6rem 1.5rem; padding: 0.9rem 1rem 0.5rem; }
  .meters :global(.fill) { height: 0.5rem; border-radius: 3px; }
  .stats { display: flex; flex-wrap: wrap; gap: 0.5rem 1.5rem; padding: 0.3rem 1rem 0.6rem; }
  .summary { display: flex; gap: 0.5rem; padding: 0.2rem 1rem 0.7rem; }
  .tally { display: inline-flex; align-items: center; gap: 0.35rem; font-size: 0.8em; color: var(--muted); }
  .tally.warn { color: var(--accent); }
  .tally.bad { color: var(--bad); }
  .checks { list-style: none; margin: 0; padding: 0.4rem 0 0.6rem; border-top: 1px solid var(--line); }
  .check { display: grid; grid-template-columns: auto 9rem 1fr; gap: 0.6rem; align-items: baseline; padding: 0.45rem 1rem; font-size: 0.9em; }
  .check + .check { border-top: 1px solid color-mix(in srgb, var(--line) 50%, transparent); }
  .check .led { align-self: center; }
  .check .led.off { background: var(--sunk); box-shadow: none; }
  .check .name { font-weight: 550; }
  .check.warn { background: var(--accent-dim); }
  .check.bad { background: var(--bad-dim); }
  .check.warn .detail { color: var(--accent); }
  .check.bad .detail { color: var(--bad); }
  @media (max-width: 560px) {
    .check { grid-template-columns: auto 1fr; }
    .check .detail { grid-column: 2; }
  }
</style>
