<script>
  import { get, post, put, del } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { ago } from './lib/format.js';
  import { useEscape } from './lib/actions.js';
  import Empty from './Empty.svelte';
  import Notice from './ui/Notice.svelte';
  import Seg from './ui/Seg.svelte';

  // The Network tab: everything the remotes can see that isn't enrolled,
  // with the hub's guess and your word beside it. Rows are what remotes
  // last reported; the only hub-side state is the name and kind you set.
  const kinds = ['tv', 'speaker', 'printer', 'phone', 'computer', 'camera', 'light', 'appliance', 'router', 'console', 'input', 'network'];
  const kindLabels = { tv: 'television', lan: 'LAN', wifi: 'Wi-Fi', bt: 'Bluetooth' };
  let devices = $state([]);
  let scanners = $state([]);
  let filter = $state('lan');
  let edit = $state(null);
  let open = $state(null);
  let scanning = $state(false);
  let loadError = $state('');
  let error = $state('');

  async function load() {
    try {
      ({ devices, scanners } = await get('/devices'));
      loadError = '';
    } catch (e) { loadError = e.message; }
  }
  $effect(() => {
    return poll(load, 15_000);
  });

  async function scanNow() {
    scanning = true;
    try { ({ devices, scanners } = await post('/devices/scan')); error = ''; } catch (e) { error = e.message; }
    scanning = false;
  }
  async function save() {
    try { await put(`/devices/${edit.id}`, { name: edit.name, kind: edit.kind }); edit = null; error = ''; await load(); } catch (e) { error = e.message; }
  }
  async function forget(d) {
    if (!confirm(`Forget ${label(d)}? It comes back if a remote sees it again.`)) return;
    try { await del(`/devices/${d.id}`); error = ''; await load(); } catch (e) { error = e.message; }
  }

  const label = (d) => d.name || d.guessName || d.addr;
  const kindOf = (d) => d.userKind || d.guessKind;
  const stale = (d) => Date.now() - new Date(d.lastSeen).getTime() > 3 * 3600_000;
  const detail = (s) => s.detail ?? {};
  let shown = $derived(devices.filter((d) => d.kind === filter));
  const count = (k) => devices.filter((d) => d.kind === k).length;
  const sees = (sc) => [sc.lan && 'LAN', sc.wifi && 'Wi-Fi', sc.bt && 'Bluetooth'].filter(Boolean).join(', ') || 'nothing';
</script>

<div class="bar">
  <Seg bind:value={filter} label="Where it was seen" options={['lan', 'wifi', 'bt'].map((k) => ({ key: k, label: kindLabels[k], n: count(k) }))} />
  <button onclick={scanNow} disabled={scanning}>{scanning ? 'Scanning…' : 'Scan now'}</button>
  {#if loadError}<Notice>{loadError}</Notice>{/if}
  {#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}
  <span class="grow"></span>
  {#if scanners.length > 0}
    <span class="scanners">
      {#each scanners as sc (sc.hostId)}
        <span><span class="led {sc.status !== 'online' || sc.error ? 'bad' : 'ok'}"></span> <strong>{sc.host}</strong> {sc.status !== 'online' ? `${sc.status} · last scan` : sc.error ? `scan failed: ${sc.error}` : sees(sc)} · {ago(sc.at)}</span>
      {/each}
    </span>
  {/if}
</div>

{#if edit}
  <section class="card form" use:useEscape={() => (edit = null)}>
    <label>Name <input bind:value={edit.name} placeholder={edit.guessName || 'what you call it'} /></label>
    <label>Kind
      <input list="kinds" bind:value={edit.kind} placeholder={edit.guessKind || 'what it is'} />
      <datalist id="kinds">{#each kinds as k}<option value={k}></option>{/each}</datalist>
    </label>
    <div class="row"><button class="primary" onclick={save}>Save</button><button class="quiet" onclick={() => (edit = null)}>Cancel</button></div>
  </section>
{/if}

{#if !loadError && shown.length === 0}
  <Empty text={scanners.length === 0 ? 'No remote has scanned yet. Scans run on the online remotes every ten minutes.' : filter === 'lan' ? 'No neighbours seen yet.' : filter === 'wifi' ? 'No Wi-Fi network seen — no online remote has a wireless card.' : 'No Bluetooth device seen — no online remote has a radio.'} action={scanners.length === 0 ? 'Scan now' : undefined} onaction={scanNow} />
{:else if !loadError}
  <div class="scroll">
  <table class="stack">
    <tbody>
      {#each shown as d (d.id)}
        <tr class:stale={stale(d)}>
          <td>
            <strong>{label(d)}</strong>
            {#if d.name && d.guessName && d.name !== d.guessName}<span class="muted small"> · guessed {d.guessName}</span>{/if}
            <br /><code class="small muted">{d.addr}</code>
            {#if d.kind === 'lan'}{#each d.sightings.slice(0, 1) as s}<code class="small muted"> {detail(s).ip}</code>{/each}{/if}
          </td>
          <td>
            {kindLabels[kindOf(d)] ?? kindOf(d) ?? ''}{#if !d.userKind && d.guessKind}<span class="muted small"> (guess)</span>{/if}
            {#if d.vendor}<br /><span class="muted small">{d.vendor}</span>{/if}
          </td>
          <td>
            {#each d.sightings as s (s.hostId)}
              <span class="seen">{s.host} · {ago(s.seen)}</span>
            {/each}
            {#if stale(d)}<span class="muted small">{d.name ? 'missing' : 'not seen lately'}</span>{/if}
          </td>
          <td class="actions">
            <button class="small quiet" onclick={() => { open = open === d.id ? null : d.id; }}>Detail</button>
            <button class="small" onclick={() => (edit = { id: d.id, name: d.name, kind: d.userKind, guessName: d.guessName, guessKind: d.guessKind })}>Name</button>
            <button class="small danger quiet" onclick={() => forget(d)}>Forget</button>
          </td>
        </tr>
        {#if open === d.id}
          <tr class="detail"><td colspan="4">
            {#each d.sightings as s (s.hostId)}
              <details open>
                <summary><strong>{s.host}</strong> <span class="muted">{ago(s.seen)}</span></summary>
                <pre>{JSON.stringify(detail(s), null, 1)}</pre>
              </details>
            {/each}
          </td></tr>
        {/if}
      {/each}
    </tbody>
  </table>
  </div>
{/if}

<style>
  .scanners { display: flex; flex-wrap: wrap; gap: 0.25rem 1.25rem; font-size: 0.8em; color: var(--muted); }
  .scanners strong { color: var(--fg); font-weight: 500; }
  tr.stale td { opacity: 0.55; }
  .seen { display: block; font-size: 0.85em; }
  pre { max-height: 16rem; }
</style>
