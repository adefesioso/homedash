<script>
  import Notice from './ui/Notice.svelte';
  import { get, put, del } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { ago, bytes } from './lib/format.js';
  import Empty from './Empty.svelte';

  // The Peers tab: every hub discovered in the space, the controls per
  // row, what each has been served, and the two totals at the foot.
  let status = $state(null);
  let peers = $state([]);
  let services = $state([]);
  let loadError = $state('');
  let error = $state('');

  async function load() {
    try { const d = await get('/peers'); status = d.status; peers = d.peers; services = d.services ?? []; loadError = ''; } catch (e) { loadError = e.message; }
  }
  $effect(() => {
    return poll(load, 10_000);
  });
  async function save(p) {
    try { await put(`/peers/${p.id}`, { approved: p.approved, maxConcurrent: +p.maxConcurrent, perHour: +p.perHour }); error = ''; await load(); } catch (e) { error = e.message; }
  }
  // A front: approve it, and give it a hostname to make it an ingress.
  async function saveFront(p, f) {
    try { await put(`/peers/${p.id}/fronts/${f.service}`, { approved: f.approved, hostname: f.hostname }); error = ''; await load(); } catch (e) { error = e.message; }
  }
  async function forget(p) {
    try { await del(`/peers/${p.id}`); error = ''; await load(); } catch (e) { error = e.message; }
  }
  async function unpublish(sv) {
    if (!confirm(`Unpublish ${sv.name}? It leaves every peer's next offer.`)) return;
    try { await del(`/services/${sv.name}`); error = ''; await load(); } catch (e) { error = e.message; }
  }
  const sum = (m, i) => Object.values(m ?? {}).reduce((a, v) => a + v[i], 0);
  const taken = $derived(peers.reduce((a, p) => a + sum(p.counts.sent, 2), 0));
  const given = $derived(peers.reduce((a, p) => a + sum(p.counts.served, 2), 0));
  const carriedForUs = $derived(peers.reduce((a, p) => a + (p.counts.origin?.[2] ?? 0), 0));
  const carriedForThem = $derived(peers.reduce((a, p) => a + (p.counts.fronted?.[2] ?? 0), 0));
  const short = (id) => '…' + id.slice(-8);
  const b3 = (v) => (v ?? [0, 0, 0]).map(bytes).join(' · ');
</script>

{#if loadError}<Notice>{loadError}</Notice>{/if}
{#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}
{#if status}
  <div class="bar status">
    {#if status.enabled}
      <span class="led ok"></span>
      <span>Space <strong>{status.space}</strong></span>
      <span class="muted">as <code>{short(status.id)}</code> · {status.private ? 'private' : 'public'} network · {status.connected} connection{status.connected === 1 ? '' : 's'}</span>
      <span class="pill {status.serving ? 'accent' : ''}">{status.serving ? 'running peers’ jobs' : 'not running peers’ jobs'}</span>
    {:else}
      <span class="led"></span><span class="muted">Not in a space.</span> <a href="#settings">Name one in Settings</a>
    {/if}
  </div>
  {#if status.enabled && status.reachable}
    <details class="addrs">
      <summary class="muted small">This hub's addresses — what another hub pastes as a bootstrap hub</summary>
      <pre>{(status.addrs || []).filter((a) => !a.includes('/p2p-circuit') && !a.startsWith('/ip4/127.') && !a.startsWith('/ip6/::1')).map((a) => `${a}/p2p/${status.id}`).join('\n')}</pre>
    </details>
  {/if}
{/if}

{#if !loadError && peers.length === 0}
  <Empty text="No hubs discovered yet. Discovery fills this list; approval is yours." />
{:else if !loadError}
  <div class="scroll">
  <table>
    <thead><tr><th>Hub</th><th>Offer</th><th>Approved</th><th>Max concurrent</th><th>Per hour</th><th>Record</th><th>Sent / served (day · week · all)</th><th>Fronts</th><th></th></tr></thead>
    <tbody>
      {#each peers as p (p.id)}
        <tr class:off={!p.connected}>
          <td><span class="led {p.connected ? 'ok' : ''}"></span> <strong>{p.name || short(p.id)}</strong><br /><code class="small muted">{p.id}</code><br /><span class="muted small">{p.connected ? 'connected' : `last seen ${ago(p.lastSeen)}`}</span></td>
          <td class="small">{#if p.offer}{p.offer.free ? 'free' : 'busy'} · {p.offer.models.join(', ') || 'no models'}{:else}<span class="muted">—</span>{/if}</td>
          <td><input type="checkbox" bind:checked={p.approved} onchange={() => save(p)} /></td>
          <td><input type="number" min="0" bind:value={p.maxConcurrent} onchange={() => save(p)} /></td>
          <td><input type="number" min="0" bind:value={p.perHour} onchange={() => save(p)} /></td>
          <td class="small">{#if p.record.samples || p.record.accepted}accepts {Math.round(p.record.accepted * 100)}% · finishes {Math.round(p.record.finished * 100)}% · first token {Math.round(p.record.medianTTFT)} ms{:else}<span class="muted">no record yet</span>{/if}</td>
          <td class="small mono">
            {#each Object.entries(p.counts.sent ?? {}) as [m, v]}<div>→ {m}: {v.join(' · ')}</div>{/each}
            {#each Object.entries(p.counts.served ?? {}) as [m, v]}<div>← {m}: {v.join(' · ')}</div>{/each}
            {#if p.counts.fronted?.[2]}<div>← carried for its services: {b3(p.counts.fronted)}</div>{/if}
            {#if p.counts.origin?.[2]}<div>→ it carried for yours: {b3(p.counts.origin)}</div>{/if}
          </td>
          <td class="small">
            {#if p.fronts.length === 0}<span class="muted">nothing published to you</span>{/if}
            {#each p.fronts as f (f.service)}
              <div class="front" class:gone={!f.offered}>
                <label class="check"><input type="checkbox" bind:checked={f.approved} onchange={() => saveFront(p, f)} /> <strong>{f.service}</strong>{#if !f.offered} <span class="muted">(no longer offered)</span>{/if}</label>
                {#if f.approved}
                  <a href={`/~${p.id}/${f.service}/`} target="_blank" rel="noopener">open as a link</a>
                  <input placeholder="hostname for an ingress (optional)" bind:value={f.hostname} onchange={() => saveFront(p, f)} />
                {/if}
              </div>
            {/each}
          </td>
          <td class="actions"><button class="small quiet" onclick={() => forget(p)}>forget</button></td>
        </tr>
      {/each}
    </tbody>
    <tfoot><tr><td colspan="9">Taken from the space: <strong>{taken}</strong> jobs and <strong>{bytes(carriedForUs)}</strong> carried for your services · given back: <strong>{given}</strong> jobs and <strong>{bytes(carriedForThem)}</strong> carried for theirs. A number, not a currency.</td></tr></tfoot>
  </table>
  </div>
{/if}

<h3>Published by this hub</h3>
{#if services.length === 0}
  <p class="help">Nothing published. <strong>Publish</strong> on a stack in Apps, or <strong>Publish a port</strong> on a host card, names one port on one machine you own and the peers that may front it.</p>
{:else}
  <table>
    <tbody>
      {#each services as sv (sv.name)}
        <tr>
          <td><strong>{sv.name}</strong></td>
          <td><code>{sv.host}:{sv.port}</code></td>
          <td class="small">to {sv.peers.map((id) => peers.find((p) => p.id === id)?.name || short(id)).join(', ') || 'nobody'}</td>
          <td class="actions"><button class="small danger quiet" onclick={() => unpublish(sv)}>unpublish</button></td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

<style>
  .status { gap: 0.6rem; margin-bottom: 0.75rem; }
  .addrs { margin-bottom: 1rem; }
  .addrs pre { margin-top: 0.4rem; font-size: 0.78em; }
  .front { display: grid; gap: 0.2rem; margin-bottom: 0.4rem; }
  .front.gone { opacity: 0.6; }
  .front input:not([type]) { width: 14rem; padding: 0.25rem 0.4rem; }
  tr.off td { opacity: 0.6; }
  tfoot td { color: var(--muted); border-bottom: 0; }
  input[type=number] { width: 4.5rem; padding: 0.3rem; }
</style>
