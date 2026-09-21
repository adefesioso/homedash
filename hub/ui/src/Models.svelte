<script>
  import Notice from './ui/Notice.svelte';
  import { get, post } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { bytes } from './lib/format.js';
  import { pulls } from './lib/pulls.svelte.js';
  import Empty from './Empty.svelte';

  // The Models tab: every Ollama machine down one side, every model any
  // of them holds across the other. A gap in the grid is the answer to
  // "why did that queue" or "why was that refused".
  let grid = $state([]);
  let loadError = $state('');
  let error = $state('');
  let name = $state('');
  let fit = $state({});

  async function load() {
    try { grid = await get('/models'); loadError = ''; } catch (e) { loadError = e.message; }
  }
  $effect(() => {
    return poll(load, 15_000);
  });

  const models = $derived([...new Set(grid.flatMap((m) => m.models.map((x) => x.name)))].sort());
  const cell = (m, model) => m.models.find((x) => x.name === model);
  const total = (m) => m.models.reduce((a, x) => a + x.size, 0);

  async function pull(host, model) {
    const key = `${host}/${model}`;
    pulls[key] = 'starting';
    let failed = false;
    try {
      const r = await fetch('/api/models/pull', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ host, model }) });
      const reader = r.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      readLoop: while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const lines = buf.split('\n'); buf = lines.pop();
        for (const l of lines) {
          if (!l) continue;
          let j;
          try { j = JSON.parse(l); } catch { continue; }
          if (j.error) {
            // A stream line with "error" is terminal: Ollama answers 200
            // for a bogus model and puts the reason in the stream rather
            // than the status, so this is the only place the failure
            // shows (M-1). Stop reading; don't let a later line paper
            // over it.
            error = `${host}: ${j.error}`;
            failed = true;
            await reader.cancel().catch(() => {});
            break readLoop;
          }
          pulls[key] = j.total ? `${j.status} ${Math.round((100 * (j.completed ?? 0)) / j.total)}%` : j.status;
        }
      }
      if (!failed) { if (!r.ok) { error = `${host}: pull failed`; failed = true; } else { error = ''; } }
    } catch (e) { error = e.message; failed = true; }
    if (failed) {
      // Keep the row up with the error text a few seconds — long enough
      // to read — instead of it just vanishing (M-1).
      pulls[key] = error;
      setTimeout(() => { delete pulls[key]; }, 5000);
    } else {
      delete pulls[key];
    }
    await load();
  }
  async function remove(host, model) {
    if (!confirm(`Remove ${model} from ${host}?`)) return;
    try { await post('/models/delete', { host, model }); error = ''; } catch (e) { error = e.message; }
    await load();
  }
  async function askFit(m) {
    try { fit = { ...fit, [m.host]: await get(`/hosts/${m.hostId}/fit?n=8`) }; error = ''; } catch (e) { error = `${m.host}: ${e.message}`; }
  }
  async function enable(m, on) {
    try { await post(`/hosts/${m.hostId}/pool`, { enabled: on }); error = ''; } catch (e) { error = e.message; }
    await load();
  }
</script>

{#if loadError}<Notice>{loadError}</Notice>{/if}
{#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}

{#if !loadError && grid.length === 0}
  <Empty text="No Ollama machines. Switch Ollama on from a host card to start the pool." />
{:else if !loadError}
  <div class="bar">
    <input class="name" placeholder="model to pull, e.g. qwen2.5:3b" bind:value={name} />
    {#each grid.filter((m) => !m.peer) as m}
      <button disabled={!name.trim() || !m.online} onclick={() => pull(m.host, name.trim())}>Pull to {m.host}</button>
    {/each}
    <span class="grow"></span>
    <span class="muted small">Ollama endpoint <code>{location.origin}</code></span>
  </div>
  <div class="scroll">
    <table class="grid">
      <thead>
        <tr>
          <th></th>
          {#each grid as m}
            <th>
              <div class="host"><span class="led {m.online ? (m.busy ? 'busy' : 'ok') : 'bad'}"></span> {m.host}{#if m.peer}<span class="pill small">peer</span>{/if}</div>
              {#if m.peer}
                <div class="muted small">{m.online ? (m.busy ? 'busy' : 'free') : 'offline'} · from the space, read-only</div>
              {:else}
                <div class="muted small">{m.online ? (m.busy ? 'busy' : 'idle') : 'offline'} · {bytes(total(m))} of models · {bytes(m.free)} free</div>
                <label class="check small"><input type="checkbox" checked={m.enabled} onchange={(e) => enable(m, e.target.checked)} /> in the pool</label>
                <div><button class="small quiet" onclick={() => askFit(m)}>What fits</button></div>
              {/if}
              {#if m.error}<Notice small>{m.error}</Notice>{/if}
            </th>
          {/each}
        </tr>
      </thead>
      <tbody>
        {#each models as model}
          <tr>
            <td><code>{model}</code></td>
            {#each grid as m}
              {@const c = cell(m, model)}
              <td class:has={c}>
                {#if m.peer}
                  {#if c}<span class="muted small">served by the space</span>{/if}
                {:else if pulls[`${m.host}/${model}`]}
                  <span class="muted small">{pulls[`${m.host}/${model}`]}</span>
                {:else if c}
                  <span class="mono">{bytes(c.size)}</span>
                  <button class="small quiet" onclick={() => remove(m.host, model)}>remove</button>
                {:else}
                  <button class="small quiet" disabled={!m.online} onclick={() => pull(m.host, model)}>pull</button>
                {/if}
              </td>
            {/each}
          </tr>
        {/each}
        {#each Object.entries(pulls) as [key, p]}
          {#if !models.includes(key.split('/').slice(1).join('/'))}
            <tr><td colspan={grid.length + 1} class="muted small">{key}: {p}</td></tr>
          {/if}
        {/each}
      </tbody>
    </table>
  </div>
  {#each grid as m}
    {#if fit[m.host]}
      <section class="card fit">
        <h3>What fits on {m.host}</h3>
        <table>
          <tbody>
            {#each fit[m.host].models as f}
              <tr>
                <td><code>{f.ollama}</code></td><td>{f.fit}</td><td>{f.tokensPerSec ? `~${f.tokensPerSec.toFixed(1)} tok/s` : ''}</td>
                <td>{f.memoryGB.toFixed(1)} GB</td><td class="muted">{f.capabilities.join(', ')}</td>
                <td class="actions"><button class="small" disabled={!!cell(m, f.ollama) || !!cell(m, f.ollama + ':latest')} onclick={() => pull(m.host, f.ollama)}>pull</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
        {#if fit[m.host].models.length === 0}<p class="muted">llmfit found nothing pullable that fits.</p>{/if}
      </section>
    {/if}
  {/each}
{/if}

<style>
  .name { min-width: 16rem; }
  table.grid th { font-size: 0.9em; color: var(--fg); }
  table.grid th .host { font-family: var(--mono); font-weight: 600; display: flex; align-items: center; gap: 0.4rem; }
  table.grid th label { margin: 0.3rem 0; }
  table.grid td { min-width: 8rem; }
  td.has { background: var(--accent-dim); }
  tbody tr:hover td.has { background: color-mix(in srgb, var(--accent) 22%, transparent); }
  .fit { margin-top: 1rem; }
  .fit h3 { margin: 0 0 0.6rem; }
  .fit table { box-shadow: none; }
</style>
