<script>
  import { get } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { bytes, ago } from './lib/format.js';
  import Notice from './ui/Notice.svelte';
  import Stat from './ui/Stat.svelte';
  import Fill from './ui/Fill.svelte';

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
    <div class="stats">
      <Stat label="cores">{m.cores}</Stat>
      <Stat label="load">{m.load1.toFixed(2)}</Stat>
      <Stat label="memory" of={bytes(m.memTotal)} pct={memPct(m)} hot={memPct(m) >= 90} min="11rem">{bytes(m.memUsed)}</Stat>
      <Stat label="machine up">{uptime(m.uptime)}</Stat>
      <Stat label="hub up">{uptime((Date.now() - new Date(h.process.started).getTime()) / 1000)}</Stat>
      <Stat label="hub process">{bytes(h.process.rss)}</Stat>
      <Stat label="open sessions">{h.process.sessions}</Stat>
    </div>
    <div class="mounts">
      <div class="mount">
        <code>{d.path}</code>
        <span class="muted small">state {bytes(d.dbSize)} · {bytes(d.diskFree)} free of {bytes(d.diskSize)}</span>
        <span></span>
        <Fill pct={diskPct(d)} hot={h.checks.find((c) => c.name === 'State disk')?.state === 'bad'} />
      </div>
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
    <footer class="muted small">What the hub reports about itself, read every 15 seconds. The fix for a line lives where the thing does — Settings, Hosts, Tasks, Peers.</footer>
  </section>
{:else if !error}
  <p class="muted">…</p>
{/if}

<style>
  .plate { max-width: 52rem; }
  .checks { list-style: none; margin: 0; padding: 0.4rem 0 0.6rem; border-top: 1px solid var(--line); }
  .check { display: grid; grid-template-columns: auto 9rem 1fr; gap: 0.6rem; align-items: baseline; padding: 0.45rem 1rem; font-size: 0.9em; }
  .check + .check { border-top: 1px solid color-mix(in srgb, var(--line) 50%, transparent); }
  .check .led { align-self: center; }
  .check .led.off { background: var(--sunk); box-shadow: none; }
  .check .name { font-weight: 550; }
  .check.warn .detail { color: var(--accent); }
  .check.bad .detail { color: var(--bad); }
  @media (max-width: 560px) {
    .check { grid-template-columns: auto 1fr; }
    .check .detail { grid-column: 2; }
  }
</style>
