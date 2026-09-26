<script>
  import { get, post, del } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { bytes } from './lib/format.js';
  import { useEscape } from './lib/actions.js';
  import Empty from './Empty.svelte';
  import Icon from './Icon.svelte';
  import Notice from './ui/Notice.svelte';
  import Fill from './ui/Fill.svelte';
  import Donut from './ui/Donut.svelte';

  // The Storage tab, drawn as a map: every machine's disks on the left,
  // the clusters on the right, and a line from each member disk to the
  // cluster it is in — in that cluster's colour, dashed red while the
  // member is unreachable. A ring per cluster is its capacity split by
  // member; the ring at the top is the whole house's disk.
  let clusters = $state([]);
  let hosts = $state([]);
  let loadError = $state('');
  let error = $state('');
  let create = $state(null);
  let busy = $state(false);
  let newWs = $state(null);        // {cluster, name, hosts} while the workspace form is open

  async function load() {
    try { [clusters, hosts] = await Promise.all([get('/clusters'), get('/hosts')]); loadError = ''; } catch (e) { loadError = e.message; }
  }
  $effect(() => {
    return poll(load, 20_000);
  });

  // Every mountpoint every host reported, with why it can or can't be pooled.
  const disks = $derived(hosts.flatMap((h) => (h.facts?.mounts ?? []).map((m) => {
    const system = (h.facts.systemSources ?? []).includes(m.source) || ['/', '/boot', '/boot/efi', '/etc', '/usr', '/var', '/home'].includes(m.path);
    const member = clusters.find((c) => c.cluster.members.some((x) => x.host === h.name && x.path === m.path));
    const own = clusters.find((c) => c.cluster.gateway === h.name && c.cluster.path === m.path) || m.path.startsWith('/mnt/homedash/');
    return { host: h.name, ...m, system, member: member?.cluster.name, own: !!own, offline: h.status !== 'online' };
  })));
  const poolable = $derived(disks.filter((d) => !d.system && !d.member && !d.own));

  // Raw disks: whole-disk block devices with no filesystem of their own
  // and no partition that has one — nothing for df to see, so they never
  // show up in `mounts`, and nothing to pool until they're formatted
  // (S-1). Listed, not offered: they carry no path a cluster could use.
  const rawDisks = $derived(hosts.flatMap((h) => (h.facts?.disks?.blockdevices ?? [])
    .filter((d) => d.type === 'disk' && !d.fstype && !(d.children ?? []).some((c) => c.fstype || c.mountpoint))
    .map((d) => ({ host: h.name, dev: `/dev/${d.name}`, size: Number(d.size) || 0, offline: h.status !== 'online' }))));

  async function submit() {
    busy = true;
    try {
      const members = create.members.map((k) => { const [host, path] = k.split('|'); return { host, path }; });
      await post('/clusters', { name: create.name, gateway: create.gateway, path: create.path, members });
      create = null; error = ''; await load();
    } catch (e) { error = e.message; }
    busy = false;
  }
  async function remove(c) {
    if (!confirm(`Tear down ${c.cluster.name}? Every member keeps its files.`)) return;
    busy = true; try { await del(`/clusters/${c.cluster.id}`); error = ''; await load(); } catch (e) { error = e.message; } busy = false;
  }
  async function dropMember(c, m) {
    if (!confirm(`Remove ${m.host}:${m.path} from ${c.cluster.name}? Its files stay where they are.`)) return;
    busy = true; try { await del(`/clusters/${c.cluster.id}/members/${m.id}`); error = ''; await load(); } catch (e) { error = e.message; } busy = false;
  }
  async function addMember(c, key) {
    const [host, path] = key.split('|');
    busy = true; try { await post(`/clusters/${c.cluster.id}/members`, { host, path }); error = ''; await load(); } catch (e) { error = e.message; } busy = false;
  }

  // Workspaces: a directory on the cluster, shared to the remotes named.
  async function createWorkspace() {
    busy = true;
    try { await post('/workspaces', { name: newWs.name, cluster: newWs.cluster, hosts: newWs.hosts }); newWs = null; error = ''; await load(); }
    catch (e) { error = e.message; }
    busy = false;
  }
  async function dropWorkspace(w) {
    if (!confirm(`Unshare ${w.name}? Its files stay at ${w.path} on the cluster.`)) return;
    busy = true; try { await del(`/workspaces/${w.id}`); error = ''; await load(); } catch (e) { error = e.message; } busy = false;
  }
  async function shareTo(w, host) {
    busy = true; try { await post(`/workspaces/${w.id}/members`, { host }); error = ''; await load(); } catch (e) { error = e.message; } busy = false;
  }
  async function unshareFrom(w, host) {
    busy = true; try { await del(`/workspaces/${w.id}/members/${host}`); error = ''; await load(); } catch (e) { error = e.message; } busy = false;
  }
  const notIn = (w) => hosts.filter((h) => !w.members.some((m) => m.host === h.name));

  // A cluster's colour follows the cluster (its id order), never its rank.
  const colorOf = (id) => `var(--s${(clusters.map((c) => c.cluster.id).sort((a, b) => a - b).indexOf(id) % 8) + 1})`;
  const clusterOf = (name) => clusters.find((c) => c.cluster.name === name);
  const used = (size, free) => Math.max(0, (size || 0) - (free || 0));
  const statusOf = (d) => d.offline ? 'stale — the host is offline' : d.system ? 'system — off-limits' : d.own ? "a cluster's own mount" : d.member ? `in ${d.member}` : 'free to pool';

  // The house's disk, once: a cluster's own mount is its members over
  // again, so it is left out rather than counted twice.
  const house = $derived.by(() => {
    const real = disks.filter((d) => !d.own);
    const sum = (xs, k) => xs.reduce((n, d) => n + (d[k] || 0), 0);
    const segs = [];
    for (const c of clusters) {
      const size = c.size || 0, u = used(c.size, c.free);
      segs.push({ value: u, color: colorOf(c.cluster.id), label: `${c.cluster.name}: ${bytes(u)} used`, short: c.cluster.name });
      segs.push({ value: size - u, color: colorOf(c.cluster.id), faded: true, label: `${c.cluster.name}: ${bytes(size - u)} free`, short: c.cluster.name });
    }
    const pool = real.filter((d) => !d.system && !d.member), sys = real.filter((d) => d.system);
    segs.push({ value: used(sum(pool, 'size'), sum(pool, 'free')), color: 'var(--muted)', label: `free to pool: ${bytes(used(sum(pool, 'size'), sum(pool, 'free')))} used`, short: 'unpooled' });
    segs.push({ value: sum(pool, 'free'), color: 'var(--muted)', faded: true, label: `free to pool: ${bytes(sum(pool, 'free'))} free`, short: 'unpooled' });
    segs.push({ value: sum(sys, 'size'), color: 'var(--line)', label: `system disks: ${bytes(sum(sys, 'size'))}`, short: 'system' });
    const raw = rawDisks.reduce((n, d) => n + d.size, 0);
    segs.push({ value: raw, color: 'var(--line)', faded: true, label: `unformatted: ${bytes(raw)}`, short: 'raw' });
    const size = sum(real, 'size') + raw, free = sum(real, 'free');
    return { segs, size, pooled: clusters.reduce((n, c) => n + (c.size || 0), 0), unpooled: sum(pool, 'size'), system: sum(sys, 'size'), raw, pct: size ? Math.round((100 * used(size - raw, free)) / size) : 0 };
  });

  // One card per machine: its disks, system ones last and dimmed.
  const machines = $derived(hosts.map((h) => ({
    name: h.name, offline: h.status !== 'online',
    disks: disks.filter((d) => d.host === h.name && !d.own).sort((a, b) => a.system - b.system),
    raw: rawDisks.filter((d) => d.host === h.name),
  })).filter((m) => m.disks.length || m.raw.length));

  // The lines: from a disk's row on the left to its row in the cluster
  // on the right, measured off the laid-out page and redrawn whenever
  // either side moves. Hidden on a narrow screen, where the two columns
  // stack and a member row names its disk in words.
  let map = $state(null);
  let links = $state([]);
  function measure() {
    if (!map) return;
    const box = map.getBoundingClientRect();
    const out = [];
    for (const c of clusters) {
      for (const m of c.members) {
        const key = `${m.host}|${m.path}`;
        const a = map.querySelector(`[data-disk="${CSS.escape(key)}"]`), b = map.querySelector(`[data-member="${CSS.escape(key)}"]`);
        if (!a || !b) continue;
        const ra = a.getBoundingClientRect(), rb = b.getBoundingClientRect();
        const x1 = ra.right - box.left, y1 = ra.top + ra.height / 2 - box.top;
        const x2 = rb.left - box.left, y2 = rb.top + rb.height / 2 - box.top;
        if (x2 <= x1) continue; // stacked: no room for a line
        const mid = (x2 - x1) / 2;
        out.push({ key, x1, y1, d: `M${x1},${y1} C${x1 + mid},${y1} ${x2 - mid},${y2} ${x2},${y2}`, color: m.reachable ? colorOf(c.cluster.id) : 'var(--bad)', gone: !m.reachable, title: `${m.host}:${m.path} → ${c.cluster.name}` });
      }
    }
    links = out;
  }
  $effect(() => {
    void clusters; void hosts; void create; void newWs;
    if (!map) return;
    const ro = new ResizeObserver(() => measure());
    ro.observe(map);
    for (const el of map.querySelectorAll('.machine, .cluster')) ro.observe(el);
    requestAnimationFrame(measure);
    return () => ro.disconnect();
  });
</script>

<div class="bar">
  <button class="primary" onclick={() => (create = { name: '', gateway: hosts[0]?.name ?? '', path: '/srv/pool', members: [] })} disabled={poolable.length < 2}><Icon name="plus" size={14} /> New cluster</button>
  {#if poolable.length < 2}<span class="muted small">Two free disks make a cluster.</span>{/if}
  {#if loadError}<Notice>{loadError}</Notice>{/if}
  {#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}
</div>

{#if create}
  <section class="card form" use:useEscape={() => (create = null)}>
    <label>Name <input bind:value={create.name} placeholder="pool" /></label>
    <label>Gateway <select bind:value={create.gateway}>{#each hosts as h}<option value={h.name}>{h.name}</option>{/each}</select></label>
    <label>Path on the gateway <input bind:value={create.path} /></label>
    <fieldset>
      <legend>Members (two or more)</legend>
      {#each poolable as d}
        <label class="row"><input type="checkbox" value={`${d.host}|${d.path}`} bind:group={create.members} /> <code>{d.host}:{d.path}</code> <span class="muted">{bytes(d.free)} free of {bytes(d.size)}</span></label>
      {/each}
    </fieldset>
    <p class="help">Capacity, not safety: a file is never split, an offline member is a hole, a dead disk takes its files with it. Pool what you can re-download.</p>
    <div class="row"><button class="primary" onclick={submit} disabled={busy || !create.name || create.members.length < 2}>Create</button><button class="quiet" onclick={() => (create = null)}>Cancel</button></div>
  </section>
{/if}

{#if !loadError && house.size > 0}
  <section class="card overview">
    <Donut segments={house.segs} size={132} thickness={16} center={bytes(house.size)} sub={`${house.pct}% used`} label="The house's disk capacity" />
    <div class="key">
      <h4>The house's disk</h4>
      {#each clusters as c (c.cluster.id)}
        <div class="k"><span class="swatch" style:background={colorOf(c.cluster.id)}></span><span class="mono">{c.cluster.name}</span><span class="grow"></span><span class="mono">{bytes(c.size)}</span><span class="muted small">{bytes(c.free)} free</span></div>
      {/each}
      <div class="k"><span class="swatch" style:background="var(--muted)"></span>free to pool<span class="grow"></span><span class="mono">{bytes(house.unpooled)}</span><span class="muted small">{poolable.length} disk{poolable.length === 1 ? '' : 's'}</span></div>
      <div class="k"><span class="swatch" style:background="var(--line)"></span>system<span class="grow"></span><span class="mono">{bytes(house.system)}</span><span class="muted small">off-limits</span></div>
      {#if house.raw}<div class="k"><span class="swatch raw"></span>unformatted<span class="grow"></span><span class="mono">{bytes(house.raw)}</span><span class="muted small">{rawDisks.length} raw</span></div>{/if}
    </div>
  </section>
{/if}

{#if !loadError}
<div class="map" bind:this={map}>
  <svg class="links" aria-hidden="true">
    {#each links as l (l.key)}
      <path d={l.d} stroke={l.color} class:gone={l.gone}><title>{l.title}</title></path>
      <circle cx={l.x1} cy={l.y1} r="3" fill={l.color} />
    {/each}
  </svg>

  <div class="col">
    <h3>Machines</h3>
    {#each machines as h (h.name)}
      <section class="card plate machine">
        <header><span class="led {h.offline ? 'bad' : 'ok'}"></span><strong>{h.name}</strong>{#if h.offline}<span class="pill bad">offline</span>{/if}</header>
        <div class="disks">
          {#each h.disks as d (d.path)}
            {@const pct = d.size ? Math.round((100 * (d.size - d.free)) / d.size) : 0}
            {@const c = d.member ? clusterOf(d.member) : null}
            <div class="disk" class:dim={d.system} data-disk={`${d.host}|${d.path}`} title={statusOf(d)}>
              <span class="swatch" style:background={c ? colorOf(c.cluster.id) : 'transparent'} class:hollow={!c}></span>
              <code>{d.path}</code>
              <span class="fs muted small">{d.fs}</span>
              <span class="cap small nowrap"><span class="mono">{bytes(d.free)}</span> <span class="muted">/ {bytes(d.size)}</span></span>
              <span class="gauge"><Fill {pct} hot={pct > 85} dim={d.system} /></span>
              <span class="why muted small">{statusOf(d)}</span>
            </div>
          {/each}
          {#each h.raw as d (d.dev)}
            <div class="disk dim" title="unformatted — give this host a job to format and mount it">
              <span class="swatch raw"></span>
              <code>{d.dev}</code><span class="fs muted small">raw</span>
              <span class="cap small mono">{bytes(d.size)}</span>
              <span class="why muted small">unformatted — give this host a job to format and mount it</span>
            </div>
          {/each}
        </div>
      </section>
    {:else}
      <p class="muted small">No machine has reported a disk yet.</p>
    {/each}
  </div>

  <div class="col">
    <h3>Clusters</h3>
    {#if clusters.length === 0}
      <Empty text="No storage clusters. Two or more disks on the left, presented as one path." />
    {/if}
    {#each clusters as c (c.cluster.id)}
      {@const pct = c.size ? Math.round((100 * used(c.size, c.free)) / c.size) : 0}
      {@const color = colorOf(c.cluster.id)}
      <section class="card plate cluster" class:trouble={c.degraded || c.error} style:--c={color}>
        <header>
          <span class="led {c.error ? 'bad' : c.degraded ? 'bad' : c.mounted ? 'ok' : ''}"></span>
          <strong>{c.cluster.name}</strong>
          <code class="muted">{c.cluster.gateway}:{c.cluster.path}</code>
          {#if c.error || c.degraded || !c.mounted}<span class="pill bad">{c.error ? c.error : c.degraded ? 'Degraded' : 'Not mounted'}</span>{/if}
          <span class="grow"></span>
          <button class="small danger quiet" disabled={busy} onclick={() => remove(c)}>Tear down</button>
        </header>
        <div class="body">
          <div class="members">
            {#each c.members as m (m.id)}
              <div class="member" class:gone={!m.reachable} data-member={`${m.host}|${m.path}`}>
                <span class="led {m.reachable ? 'ok' : 'bad'}"></span>
                <code>{m.host}:{m.path}</code>
                <span class="grow"></span>
                <span class="muted small">{m.reachable ? `${bytes(m.free)} free of ${bytes(m.size)}` : 'unreachable — its files are missing until it returns'}</span>
                <button class="small quiet" disabled={busy || c.cluster.members.length <= 2} onclick={() => dropMember(c, m)}>remove</button>
              </div>
            {/each}
            {#if poolable.length > 0}
              <select class="add" disabled={busy} onchange={(e) => { if (e.target.value) addMember(c, e.target.value); e.target.value = ''; }}>
                <option value="">add a member…</option>
                {#each poolable as d}<option value={`${d.host}|${d.path}`}>{d.host}:{d.path} ({bytes(d.free)} free)</option>{/each}
              </select>
            {/if}
          </div>
          <Donut size={104} thickness={13} center={`${pct}%`} sub={`of ${bytes(c.size)}`} label={`${c.cluster.name}: ${pct}% used`}
            segments={c.members.flatMap((m) => [
              { value: m.reachable ? used(m.size, m.free) : 0, color, label: `${m.host}:${m.path} — ${bytes(used(m.size, m.free))} used`, short: m.host },
              { value: m.reachable ? m.free : 0, color, faded: true, label: `${m.host}:${m.path} — ${bytes(m.free)} free`, short: m.host },
            ])} />
        </div>
        <div class="workspaces">
          <h4>Shared workspaces</h4>
          {#if (c.workspaces ?? []).length === 0}
            <p class="help">A directory on this cluster that appears at the same path on every remote you name.</p>
          {/if}
          {#each c.workspaces ?? [] as w (w.id)}
            <div class="ws">
              <strong>{w.name}</strong> <code class="muted">{w.path}</code>
              {#each w.members as m (m.hostId)}
                <span class="pill" class:bad={!w.mounted[m.host]}>{m.host}{w.mounted[m.host] ? '' : ' (not mounted)'} <button class="dismiss" disabled={busy} onclick={() => unshareFrom(w, m.host)} aria-label={`Unshare from ${m.host}`}>×</button></span>
              {/each}
              {#if notIn(w).length > 0}
                <select class="add small" disabled={busy} onchange={(e) => { if (e.target.value) shareTo(w, e.target.value); e.target.value = ''; }}>
                  <option value="">share to…</option>
                  {#each notIn(w) as h}<option value={h.name}>{h.name}</option>{/each}
                </select>
              {/if}
              <span class="grow"></span>
              <button class="small danger quiet" disabled={busy} onclick={() => dropWorkspace(w)}>unshare</button>
            </div>
          {/each}
          {#if newWs && newWs.cluster === c.cluster.name}
            <div class="ws form" use:useEscape={() => (newWs = null)}>
              <label>Name <input bind:value={newWs.name} placeholder="build" /></label>
              <fieldset><legend>Appears on, at <code>{c.cluster.path}/{newWs.name || '…'}</code></legend>
                {#each hosts as h}<label class="row"><input type="checkbox" value={h.name} bind:group={newWs.hosts} /> {h.name}</label>{/each}
              </fieldset>
              <div class="row"><button class="primary" onclick={createWorkspace} disabled={busy || !newWs.name || newWs.hosts.length === 0}>Share</button><button class="quiet" onclick={() => (newWs = null)}>Cancel</button></div>
            </div>
          {:else}
            <div><button class="small" disabled={busy} onclick={() => (newWs = { cluster: c.cluster.name, name: '', hosts: [] })}><Icon name="plus" size={12} /> Workspace</button></div>
          {/if}
        </div>
      </section>
    {/each}
  </div>
</div>
{/if}

<style>
  .overview { display: flex; gap: 1.5rem; align-items: center; flex-wrap: wrap; max-width: 44rem; margin-bottom: 1.25rem; }
  .key { display: grid; gap: 0.3rem; flex: 1; min-width: 16rem; }
  .key h4 { margin: 0 0 0.2rem; }
  .k { display: flex; gap: 0.5rem; align-items: center; font-size: 0.9em; }
  .k .small { min-width: 5.5rem; text-align: right; }
  .swatch.raw { background: repeating-linear-gradient(135deg, var(--line) 0 2px, transparent 2px 4px); box-shadow: inset 0 0 0 1px var(--line); }
  .swatch.hollow { box-shadow: inset 0 0 0 1px var(--line); }

  .map { position: relative; display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.25fr); gap: 0 5rem; align-items: start; }
  .col { display: grid; gap: 0.9rem; min-width: 0; position: relative; z-index: 1; }
  .col h3 { margin: 0; }
  .links { position: absolute; inset: 0; width: 100%; height: 100%; pointer-events: none; overflow: visible; z-index: 2; }
  .links path { fill: none; stroke-width: 2; stroke-linecap: round; opacity: 0.85; }
  .links path.gone { stroke-dasharray: 4 4; }

  .disks { display: grid; }
  .disk {
    display: grid; grid-template-columns: auto auto 1fr auto; grid-template-areas: 'sw path fs cap' 'sw gauge gauge gauge' 'sw why why why';
    gap: 0.15rem 0.5rem; align-items: center; padding: 0.55rem 1rem; font-size: 0.9em; border-bottom: 1px solid var(--line);
  }
  .disk:last-child { border-bottom: 0; }
  .disk > .swatch { grid-area: sw; align-self: start; margin-top: 0.3rem; }
  .disk > code { grid-area: path; }
  .disk > .fs { grid-area: fs; }
  .disk > .cap { grid-area: cap; }
  .disk > .gauge { grid-area: gauge; }
  .disk > .why { grid-area: why; }
  .disk.dim code, .disk.dim .mono { color: var(--muted); }

  .cluster { border-left: 3px solid var(--c); }
  .cluster .body { display: flex; gap: 1rem; align-items: center; padding: 0.8rem 1rem; flex-wrap: wrap; }
  .members { display: grid; gap: 0.35rem; flex: 1; min-width: 14rem; }
  .member { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; font-size: 0.9em; padding: 0.15rem 0.35rem; margin-left: -0.35rem; border-radius: var(--r); }
  .member:hover { background: var(--sunk); }
  .member.gone code { color: var(--bad); }
  .add { width: max-content; font-size: 0.85em; padding: 0.25rem 1.6rem 0.25rem 0.5rem; }
  .workspaces { border-top: 1px solid var(--line); padding: 0.2rem 1rem 0.8rem; display: grid; gap: 0.4rem; }
  .workspaces h4 { margin: 0.5rem 0 0; }
  .ws { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; font-size: 0.9em; }
  .ws.form { display: grid; }

  @media (max-width: 1100px) { .map { grid-template-columns: 1fr; gap: 1.25rem; } .links { display: none; } }
</style>
