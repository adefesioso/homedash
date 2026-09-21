<script>
  import { get, post, del } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { bytes } from './lib/format.js';
  import { useEscape } from './lib/actions.js';
  import Empty from './Empty.svelte';
  import Icon from './Icon.svelte';
  import Notice from './ui/Notice.svelte';
  import Fill from './ui/Fill.svelte';

  // The Storage tab: clusters above the per-machine disk view, with the
  // disks that are off-limits and the ones that belong to a cluster marked.
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

{#if !loadError && clusters.length === 0}
  <Empty text="No storage clusters. Two or more disks below, presented as one path." />
{:else if !loadError}
  {#each clusters as c (c.cluster.id)}
    {@const pct = c.size ? Math.round((100 * ((c.size || 0) - (c.free || 0))) / c.size) : 0}
    <section class="card cluster" class:trouble={c.degraded || c.error}>
      <header>
        <span class="led {c.error ? 'bad' : c.degraded ? 'bad' : c.mounted ? 'ok' : ''}"></span>
        <strong>{c.cluster.name}</strong>
        <code class="muted">{c.cluster.gateway}:{c.cluster.path}</code>
        {#if c.error || c.degraded || !c.mounted}<span class="pill bad">{c.error ? c.error : c.degraded ? 'Degraded' : 'Not mounted'}</span>{/if}
        <span class="grow"></span>
        <span class="cap"><span class="mono">{bytes(c.free)}</span> <span class="muted">free of {bytes(c.size)}</span></span>
        <button class="small danger quiet" disabled={busy} onclick={() => remove(c)}>Tear down</button>
      </header>
      <Fill {pct} hot={pct > 85} />
      <div class="members">
        {#each c.members as m (m.id)}
          <div class="member" class:gone={!m.reachable}>
            <span class="led {m.reachable ? 'ok' : 'bad'}"></span>
            <code>{m.host}:{m.path}</code>
            <span class="muted small">{m.reachable ? `${bytes(m.free)} free of ${bytes(m.size)}` : 'unreachable — its files are missing until it returns'}</span>
            <span class="grow"></span>
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
{/if}

<h3>Disks</h3>
<div class="scroll">
<table class="stack">
  <tbody>
    {#each disks as d}
      {@const pct = d.size ? Math.round((100 * (d.size - d.free)) / d.size) : 0}
      <tr class:dim={d.system || d.own}>
        <td class="mono">{d.host}{#if d.offline}<br /><span class="error small">offline</span>{/if}</td>
        <td><code>{d.path}</code> <span class="muted small">{d.source} · {d.fs}</span></td>
        <td class="gauge"><Fill {pct} hot={pct > 85} dim={d.system || d.own} /></td>
        <td class="nowrap">{bytes(d.free)} <span class="muted">free of {bytes(d.size)}</span></td>
        <td class="muted">{d.offline ? 'stale — the host is offline' : d.system ? 'system — off-limits' : d.own ? "a cluster's own mount" : d.member ? `in ${d.member}` : 'free to pool'}</td>
      </tr>
    {/each}
    {#each rawDisks as d}
      <tr class="dim">
        <td class="mono">{d.host}{#if d.offline}<br /><span class="error small">offline</span>{/if}</td>
        <td><code>{d.dev}</code> <span class="muted small">raw</span></td>
        <td class="gauge"></td>
        <td class="nowrap">{bytes(d.size)}</td>
        <td class="muted">{d.offline ? 'stale — the host is offline' : 'unformatted — give this host a job to format and mount it'}</td>
      </tr>
    {/each}
  </tbody>
</table>
</div>

<style>
  .cluster { margin-bottom: 1rem; display: grid; gap: 0.6rem; }
  header { display: flex; gap: 0.6rem; align-items: center; flex-wrap: wrap; }
  header strong { font-family: var(--mono); font-size: 1rem; }
  .cap { font-size: 0.9em; }
  .members { display: grid; gap: 0.3rem; }
  .member { display: flex; gap: 0.6rem; align-items: center; flex-wrap: wrap; font-size: 0.9em; }
  .member.gone code { color: var(--bad); }
  .add { width: max-content; font-size: 0.85em; padding: 0.25rem 1.6rem 0.25rem 0.5rem; }
  .workspaces { border-top: 1px solid var(--line); padding-top: 0.2rem; display: grid; gap: 0.4rem; }
  .workspaces h4 { margin: 0.5rem 0 0; }
  .ws { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; font-size: 0.9em; }
  .ws.form { display: grid; }
  .gauge { width: 10rem; vertical-align: middle; }
  tr.dim td { color: var(--muted); }
  @media (max-width: 560px) { .gauge { width: auto; flex-basis: 100%; } }
</style>
