<script>
  import { get } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { count, usd, ago, sparkPath } from './lib/format.js';
  import Notice from './ui/Notice.svelte';
  import Fill from './ui/Fill.svelte';
  import Seg from './ui/Seg.svelte';
  import Donut from './ui/Donut.svelte';
  import Empty from './Empty.svelte';

  // The Usage tab, both sides of every token: what the fleet's agents
  // spent — the hub agent's windows and each remote's jobs — and what
  // the router's machines served, to whom. A reply from the house's own
  // pool shows on both sides: spent by the agent that asked, served by
  // the machine that answered. The point is dispatch, not cost: a remote
  // with nothing here while others carry the fleet is being skipped.
  const windows = [{ key: 24, label: '24h' }, { key: 24 * 7, label: '7d' }, { key: 24 * 30, label: '30d' }];
  let hours = $state(24 * 7);
  let totals = $state(null);   // HostUsage[] from /api/usage, one row per host, zero included
  let series = $state({});     // hostId -> Usage[] from /api/hosts/{id}/usage
  let hubRows = $state([]);    // Usage[] per model per hour from /api/usage/hub
  let served = $state([]);     // Served[] per server, caller, model per hour from /api/usage/served
  let error = $state('');

  async function load() {
    try {
      [totals, hubRows, served] = await Promise.all([get(`/usage?hours=${hours}`), get(`/usage/hub?hours=${hours}`), get(`/usage/served?hours=${hours}`)]);
      error = '';
    } catch (e) { error = e.message; }
  }
  async function loadSeries() {
    if (!totals?.length) return;
    const got = await Promise.all(totals.map((t) => get(`/hosts/${t.hostId}/usage?hours=${hours}`).then((u) => [t.hostId, u]).catch(() => null)));
    series = Object.fromEntries(got.filter(Boolean));
  }
  $effect(() => poll(load, 30_000));
  $effect(() => { hours; load(); });
  $effect(() => { if (totals) loadSeries(); });

  const windowLabel = $derived(windows.find((w) => w.key === hours)?.label);
  const sumBy = (rows, key, fields) => {
    const m = new Map();
    for (const r of rows) {
      const k = key(r), a = m.get(k) ?? Object.fromEntries([['key', k], ...fields.map((f) => [f, 0])]);
      for (const f of fields) a[f] += r[f] ?? 0;
      m.set(k, a);
    }
    return [...m.values()];
  };
  const tok = (u) => (u?.input ?? 0) + (u?.output ?? 0);
  const F = ['input', 'output', 'cacheRead', 'cacheWrite', 'cost', 'calls'];

  // Consumers: the hub agent first, then every remote, each in its own
  // colour for as long as the tab is open — the colour follows the
  // agent, not its rank. Past the seventh remote they share "others".
  const hub = $derived.by(() => {
    const [t] = sumBy(hubRows, () => 'hub', F);
    return {
      ...(t ?? Object.fromEntries(F.map((f) => [f, 0]))),
      models: sumBy(hubRows, (r) => r.model, F).sort((a, b) => tok(b) - tok(a)),
      spark: sumBy(hubRows, (r) => r.at, ['input', 'output']).map(tok),
      last: hubRows.at(-1)?.at,
    };
  });
  const colorOf = (id) => {
    const i = (totals ?? []).map((t) => t.hostId).sort((a, b) => a - b).indexOf(id);
    return i >= 0 && i < 7 ? `var(--s${i + 2})` : 'var(--muted)';
  };
  const consumers = $derived([
    { key: 'hub', name: 'hub agent', tokens: tok(hub), cost: hub.cost, color: 'var(--s1)' },
    ...(totals ?? []).map((t) => ({ key: t.hostId, name: t.host, tokens: tok(t), cost: t.cost, color: colorOf(t.hostId) })),
  ]);
  const spent = $derived(consumers.reduce((n, c) => n + c.tokens, 0));
  const spentCost = $derived(consumers.reduce((n, c) => n + (c.cost || 0), 0));
  const pctOf = (n, of) => (of ? Math.round((100 * n) / of) : 0);
  const tokenSeries = (id) => (series[id] ?? []).map(tok);
  const lastAt = (id) => (series[id] ?? []).at(-1)?.at;

  // Servers: every machine or peer that answered through the router.
  const who = (c) => c === 'hub' ? 'hub agent' : c?.startsWith('remote:') ? `${c.slice(7)}'s agent` : c?.startsWith('peer:') ? `peer ${c.slice(5)}` : c?.startsWith('client:') ? c.slice(7) : 'unknown';
  const servers = $derived(sumBy(served, (r) => `${r.peer ? 'p' : 'l'}|${r.server}`, ['input', 'output', 'calls']).map((s) => {
    const rows = served.filter((r) => `${r.peer ? 'p' : 'l'}|${r.server}` === s.key);
    return {
      ...s, name: rows[0].server, peer: rows[0].peer,
      models: sumBy(rows, (r) => r.model, ['input', 'output', 'calls']).sort((a, b) => tok(b) - tok(a)),
      callers: sumBy(rows, (r) => r.caller, ['input', 'output', 'calls']).sort((a, b) => tok(b) - tok(a)),
      spark: sumBy(rows, (r) => r.at, ['input', 'output']).map(tok),
      last: rows.at(-1)?.at,
    };
  }).sort((a, b) => tok(b) - tok(a)));
  const servedTokens = $derived(servers.reduce((n, s) => n + tok(s), 0));
  const servedCalls = $derived(servers.reduce((n, s) => n + s.calls, 0));
  const byPeers = $derived(servers.filter((s) => s.peer).reduce((n, s) => n + tok(s), 0));
  const forPeers = $derived(served.filter((r) => r.caller?.startsWith('peer:')).reduce((n, r) => n + tok(r), 0));
  // Who asked, across every server, in the consumers' colours where the
  // asker is one of them.
  const askers = $derived(sumBy(served, (r) => r.caller, ['input', 'output', 'calls']).sort((a, b) => tok(b) - tok(a)));
  const askerColor = (c) => {
    if (c === 'hub') return 'var(--s1)';
    const t = c?.startsWith('remote:') && (totals ?? []).find((x) => x.host === c.slice(7));
    return t ? colorOf(t.hostId) : 'var(--muted)';
  };
</script>

{#if error}<Notice>{error}</Notice>{/if}

<div class="bar"><Seg options={windows} bind:value={hours} label="Window" /></div>

{#if totals && totals.length === 0 && !hub.calls && !servers.length}
  <Empty text="No machines enrolled yet. Usage appears once an agent runs or a model answers." />
{:else if totals}
  <div class="overview">
    <section class="card plate">
      <header><strong>Spent</strong><span class="grow"></span><span class="muted small">{windowLabel}, by the fleet's agents</span></header>
      <div class="split">
        <Donut size={112} thickness={14} center={count(spent)} sub="tokens" label="Tokens spent by agent"
          segments={consumers.map((c) => ({ value: c.tokens, color: c.color, label: `${c.name}: ${count(c.tokens)} tokens (${pctOf(c.tokens, spent)}%)`, short: c.name }))} />
        <div class="legend">
          {#each consumers as c (c.key)}
            <div class="k" class:idle={!c.tokens}><span class="swatch" style:background={c.color}></span><span class="mono name">{c.name}</span><span class="grow"></span><span class="mono">{count(c.tokens)}</span><span class="muted small pct">{pctOf(c.tokens, spent)}%</span></div>
          {/each}
          <div class="k total"><span class="muted">cost</span><span class="grow"></span><span class="mono">{usd(spentCost)}</span><span class="pct"></span></div>
        </div>
      </div>
    </section>

    <section class="card plate">
      <header><strong>Served</strong><span class="grow"></span><span class="muted small">{windowLabel}, through the router</span></header>
      {#if servers.length}
        <div class="split">
          <Donut size={112} thickness={14} center={count(servedTokens)} sub="tokens" label="Tokens served, by who asked"
            segments={askers.map((a) => ({ value: tok(a), color: askerColor(a.key), label: `asked by ${who(a.key)}: ${count(tok(a))} tokens`, short: who(a.key) }))} />
          <div class="legend">
            <h4>Asked by</h4>
            {#each askers as a (a.key)}
              <div class="k"><span class="swatch" style:background={askerColor(a.key)}></span><span class="name">{who(a.key)}</span><span class="grow"></span><span class="mono">{count(tok(a))}</span><span class="muted small pct">{a.calls} req</span></div>
            {/each}
            <div class="k total"><span class="muted">{servedCalls} requests · {count(byPeers)} by peers · {count(forPeers)} for peers</span></div>
          </div>
        </div>
      {:else}
        <p class="muted small pad">Nothing served in this window. A request to the hub's Ollama endpoint — from an agent on the <code>homedash</code> provider, a peer, or a client — is counted here.</p>
      {/if}
    </section>
  </div>

  <h3>Agents</h3>
  <div class="grid">
    <section class="card plate" class:idle={!hub.calls} style:--c="var(--s1)">
      <header>
        <span class="swatch" style:background="var(--s1)"></span>
        <strong>hub agent</strong>
        {#if !hub.calls}<span class="pill">idle</span>{/if}
        <span class="grow"></span>
        <span class="muted small">{hub.last ? ago(hub.last) : 'no windows yet'}</span>
      </header>
      <div class="share">
        <div class="share-row">
          <span class="v">{count(tok(hub))} tokens<span class="of"> · {pctOf(tok(hub), spent)}% of spend</span></span>
          {#if hub.spark.length > 1}<svg viewBox="0 0 80 20" class="spark" preserveAspectRatio="none"><title>tokens, {windowLabel}</title><path d={sparkPath(hub.spark)} /></svg>{/if}
        </div>
        <Fill pct={pctOf(tok(hub), spent)} dim={!hub.calls} />
      </div>
      <div class="stats">
        <div class="stat"><span class="v">{count(hub.input)}</span><span class="k">input</span></div>
        <div class="stat"><span class="v">{count(hub.output)}</span><span class="k">output</span></div>
        <div class="stat"><span class="v">{count(hub.cacheRead)}</span><span class="k">cache read</span></div>
        <div class="stat"><span class="v">{usd(hub.cost)}</span><span class="k">cost</span></div>
        <div class="stat"><span class="v">{hub.calls}</span><span class="k">replies</span></div>
      </div>
      {#if hub.models.length}
        <div class="models">
          {#each hub.models as m (m.key)}
            <div class="model"><code>{m.key}</code><span class="grow"></span><span class="mono small">{count(tok(m))}</span><span class="muted small">{usd(m.cost)}</span></div>
          {/each}
        </div>
      {/if}
    </section>

    {#each totals as t (t.hostId)}
      {@const busy = tok(t) > 0}
      <section class="card plate" class:idle={!busy}>
        <header>
          <span class="swatch" style:background={colorOf(t.hostId)}></span>
          <strong>{t.host}</strong>
          {#if !busy}<span class="pill">idle</span>{/if}
          <span class="grow"></span>
          <span class="muted small">{lastAt(t.hostId) ? ago(lastAt(t.hostId)) : 'no jobs yet'}</span>
        </header>
        <div class="share">
          <div class="share-row">
            <span class="v">{count(tok(t))} tokens<span class="of"> · {pctOf(tok(t), spent)}% of spend</span></span>
            {#if tokenSeries(t.hostId).length > 1}
              <svg viewBox="0 0 80 20" class="spark" preserveAspectRatio="none"><title>tokens, {windowLabel}</title><path d={sparkPath(tokenSeries(t.hostId))} /></svg>
            {/if}
          </div>
          <Fill pct={pctOf(tok(t), spent)} dim={!busy} />
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

  {#if servers.length}
    <h3>Models served</h3>
    <div class="grid">
      {#each servers as s (s.key)}
        <section class="card plate">
          <header>
            <strong>{s.name}</strong>
            <span class="pill" class:accent={s.peer}>{s.peer ? 'peer' : 'this house'}</span>
            <span class="grow"></span>
            <span class="muted small">{s.last ? ago(s.last) : ''}</span>
          </header>
          <div class="share">
            <div class="share-row">
              <span class="v">{count(tok(s))} tokens<span class="of"> · {s.calls} request{s.calls === 1 ? '' : 's'} · {pctOf(tok(s), servedTokens)}% served</span></span>
              {#if s.spark.length > 1}<svg viewBox="0 0 80 20" class="spark" preserveAspectRatio="none"><title>tokens, {windowLabel}</title><path d={sparkPath(s.spark)} /></svg>{/if}
            </div>
            <Fill pct={pctOf(tok(s), servedTokens)} />
          </div>
          <div class="models">
            {#each s.models as m (m.key)}
              <div class="model">
                <code>{m.key}</code><span class="grow"></span>
                <span class="mono small">{count(m.input)} in · {count(m.output)} out</span>
                <span class="muted small">{m.calls} req</span>
              </div>
            {/each}
          </div>
          <footer>
            <span class="muted small">asked by</span>
            {#each s.callers as c (c.key)}<span class="pill"><span class="swatch" style:background={askerColor(c.key)}></span>{who(c.key)} · {count(tok(c))}</span>{/each}
          </footer>
        </section>
      {/each}
    </div>
  {/if}
{:else if !error}
  <p class="muted">…</p>
{/if}

<style>
  .bar { display: flex; align-items: center; gap: 0.75rem; margin-bottom: 1rem; }
  .overview { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 24rem), 1fr)); gap: 1rem; max-width: 64rem; }
  .split { display: flex; gap: 1.25rem; align-items: center; padding: 0.9rem 1rem; flex-wrap: wrap; }
  .legend { display: grid; gap: 0.25rem; flex: 1; min-width: 12rem; }
  .legend h4 { margin: 0 0 0.15rem; }
  .k { display: flex; gap: 0.5rem; align-items: center; font-size: 0.88em; }
  .k.idle { opacity: 0.6; }
  .k .pct { min-width: 3.2rem; text-align: right; }
  .k.total { border-top: 1px solid var(--line); padding-top: 0.3rem; margin-top: 0.15rem; }
  .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .pad { padding: 0.9rem 1rem; margin: 0; }
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
  .models { display: grid; gap: 0.25rem; padding: 0.6rem 1rem 0.8rem; border-top: 1px solid var(--line); }
  .model { display: flex; gap: 0.5rem; align-items: baseline; font-size: 0.88em; min-width: 0; }
  .model code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
  footer { font-size: 0.9em; }
</style>
