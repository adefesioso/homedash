<script>
  import { get, post, put, del } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { bytes, ago, sparkPath } from './lib/format.js';
  import { useEscape } from './lib/actions.js';
  import Empty from './Empty.svelte';
  import Publish from './Publish.svelte';
  import Icon from './Icon.svelte';
  import ModelPick from './ModelPick.svelte';
  import Notice from './ui/Notice.svelte';
  import Stat from './ui/Stat.svelte';
  import Fill from './ui/Fill.svelte';
  import { splitModel, joinModel, halfPicked, badModel } from './lib/model.js';

  // The Hosts tab: one card per enrolled machine, showing what it last
  // reported about itself — never what the hub asked for.
  let hosts = $state([]);
  let metrics = $state({});
  let loadError = $state('');     // set/cleared by load() only
  let error = $state('');         // set by an action; cleared by the next action or the × (X-7)
  let enroll = $state(null);      // {name, rebuildFrom} while the dialog is open; the line once minted
  let open = $state(null);        // host id whose detail is open
  let busy = $state({});
  let fit = $state({});
  let script = $state('');
  let publish = $state(null);     // host id whose publish dialog is open
  let note = $state({});          // host id -> a status line while a fire-and-forget action runs
  let ollamaBusy = $state({});    // host id -> 'installing' | 'removing' while that switch's own action runs (H-7)
  // The agent model override: Change on a card opens the same picker
  // Settings has, fed by the same GET /agents/models, with the fleet
  // default as the blank choice. One card edits at a time.
  let modelEdit = $state(null);   // {id, pick: {provider, model}, err} while a card's picker is open
  let providers = $state([]);
  let providersError = $state('');
  // The Address section under More: one form per open card, prefilled
  // with what the machine reports on the chosen interface, so "hold what
  // it has now" is the default.
  let addr = $state(null);        // {iface, cidr, gateway, dns} for the open card

  async function load() {
    try {
      hosts = await get('/hosts');
      loadError = '';
    } catch (e) { loadError = e.message; }
  }
  // The sparklines: two days of sweeps per host, which the heartbeat adds
  // to once a minute — so asked for once a minute, not with every card
  // refresh, and set in one assignment so the cards redraw once.
  async function loadMetrics() {
    const got = await Promise.all(hosts.map((h) => get(`/hosts/${h.id}/metrics?hours=48`).then((m) => [h.id, m]).catch(() => null)));
    metrics = Object.fromEntries(got.filter(Boolean));
  }
  $effect(() => poll(load, 15_000));
  $effect(() => { get('/agents/models').then((p) => { providers = p; providersError = ''; }).catch((e) => { providersError = e.message; }); });
  $effect(() => poll(async () => { if (!hosts.length) await load(); await loadMetrics(); }, 60_000));

  async function act(h, fn) {
    busy = { ...busy, [h.id]: true };
    try { await fn(); error = ''; } catch (e) { error = `${h.name}: ${e.message}`; }
    busy = { ...busy, [h.id]: false };
    await load();
  }
  // The Ollama switch can take minutes either way; act() alone leaves it
  // checked-and-disabled with nothing to say why (H-7).
  async function toggleOllama(h, installed) {
    ollamaBusy = { ...ollamaBusy, [h.id]: installed ? 'installing' : 'removing' };
    await act(h, () => post(`/hosts/${h.id}/ollama`, { installed }));
    ollamaBusy = { ...ollamaBusy, [h.id]: '' };
  }
  // Sync credentials everywhere: one host's success must not erase
  // another's failure (H-5), so failures are collected across the whole
  // sweep and shown together rather than through act()'s per-call clear.
  async function updateAllCredentials() {
    const failed = [];
    for (const h of hosts) {
      if (h.status !== 'online') continue;
      busy = { ...busy, [h.id]: true };
      try { await post(`/hosts/${h.id}/credentials`); } catch (e) { failed.push(`${h.name}: ${e.message}`); }
      busy = { ...busy, [h.id]: false };
    }
    error = failed.join(' · ');
    await load();
  }
  const mint = async () => {
    try { enroll = { ...enroll, ...(await post('/hosts/enroll', { name: enroll.name, rebuildFrom: enroll.rebuildFrom || '' })) }; }
    catch (e) { error = e.message; }
  };
  // Re-provision answers as soon as the SSH run is kicked off in the
  // background (204, no body) — the card would otherwise go quiet for
  // minutes with the button re-enabled and nothing to show for the
  // click (H-15). Events carries the actual result; this is just "it
  // started" until the next load clears it.
  async function reprovision(h) {
    if (!confirm(`Re-provision ${h.name}? The enrollment layout runs again over SSH: accounts, key, cage, agent. Takes a few minutes; ends as an event.`)) return;
    note = { ...note, [h.id]: 're-provisioning… ends as an event' };
    await act(h, () => post(`/hosts/${h.id}/reprovision`));
    setTimeout(() => { note = { ...note, [h.id]: '' }; }, 180_000);
  }
  async function saveModel(h) {
    const v = joinModel(modelEdit.pick);
    if (halfPicked(modelEdit.pick)) { modelEdit.err = 'pick a provider and type a model, or leave both blank for the fleet default'; return; }
    if (badModel(v)) { modelEdit.err = 'provider/model such as anthropic/claude-sonnet-5 or homedash/qwen2.5:3b'; return; }
    modelEdit = null;
    await act(h, () => put(`/hosts/${h.id}/agent`, { model: v }));
    // The write to config.yml lands synchronously, but the card shows
    // facts.agent.model, which only catches up once the background Sweep
    // the save kicks off finishes its own SSH round trip — the load()
    // act() just did almost always beats it back. Patch the just-loaded
    // facts so the card doesn't flash back to the old value; the next
    // poll (or heartbeat) confirms it for real.
    hosts = hosts.map((x) => (x.id === h.id && x.facts?.agent ? { ...x, facts: { ...x.facts, agent: { ...x.facts.agent, model: v } } } : x));
  }
  const rootFree = (h) => (metrics[h.id] ?? []).map((m) => { try { return (typeof m.mounts === 'string' ? JSON.parse(m.mounts) : m.mounts)['/']; } catch { return NaN; } });
  const facts = (h) => h.facts ?? {};
  // Interfaces worth an address: not Docker's bridges and veths.
  const ifaces = (h) => (facts(h).interfaces ?? []).filter((i) => !/^(docker|br-|veth|virbr)/.test(i.name));
  function pickIface(h, name) {
    const i = ifaces(h).find((x) => x.name === name) ?? ifaces(h)[0];
    const n = facts(h).network ?? {};
    addr = { iface: i?.name ?? '', cidr: i?.addrs?.[0]?.cidr ?? '', gateway: n.via === i?.name ? n.gateway ?? '' : '', dns: (n.dns ?? []).join(' ') };
  }
  // The change answers only once the machine confirmed at the new
  // address, up to a minute and a half; the note says what is going on
  // while nothing else on the card does. A refusal is act()'s error.
  async function setAddress(h, dhcp) {
    const body = dhcp ? { interface: addr.iface, address: '' } : { interface: addr.iface, address: addr.cidr, gateway: addr.gateway, dns: addr.dns.split(/[\s,]+/).filter(Boolean) };
    if (!dhcp && !confirm(`Hold ${h.name}'s ${addr.iface} at ${addr.cidr}? The hub must reach it there within 90 seconds or the machine reverts on its own.`)) return;
    if (dhcp && !confirm(`Put ${h.name}'s ${addr.iface} back on DHCP? Confirmed at ${h.addr}: a lease that hands out a different address is reverted.`)) return;
    note = { ...note, [h.id]: dhcp ? `${addr.iface} going back to DHCP… waiting for ${h.name} to answer at ${h.addr}` : `holding ${addr.cidr} on ${addr.iface}… waiting for ${h.name} to answer there` };
    await act(h, () => put(`/hosts/${h.id}/address`, body));
    note = { ...note, [h.id]: '' };
  }
  const memPct = (f) => (f.memTotal ? Math.round((100 * f.memUsed) / f.memTotal) : 0);
  const status = (h) => ({ online: 'Online', offline: 'Offline', mismatch: 'Key mismatch', unknown: 'Unknown' }[h.status] ?? h.status);
</script>

<div class="bar">
  <button class="primary" onclick={() => (enroll = { name: '', rebuildFrom: '' })}><Icon name="plus" size={14} /> New remote</button>
  <button disabled={hosts.length === 0} onclick={updateAllCredentials}>Sync omp credentials</button>
  {#if loadError}<Notice>{loadError}</Notice>{/if}
  {#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}
</div>

{#if enroll}
  <section class="card dialog" use:useEscape={() => (enroll = null)}>
    {#if enroll.line}
      <p>Paste this into the machine's terminal. Good once, for fifteen minutes.</p>
      <pre>{enroll.line}</pre>
      <div class="row"><button class="primary" onclick={() => (enroll = null)}>Done</button></div>
    {:else}
      <div class="row fields">
        <label>Name <input bind:value={enroll.name} placeholder="nas, pi, gpu-box" /></label>
        <label>Rebuild from
          <select bind:value={enroll.rebuildFrom}>
            <option value="">a fresh machine</option>
            {#each hosts as h}<option value={h.name}>{h.name}'s rebuild script</option>{/each}
          </select>
        </label>
      </div>
      <div class="row"><button class="primary" onclick={mint} disabled={!enroll.name.trim()}>Make the line</button><button class="quiet" onclick={() => (enroll = null)}>Cancel</button></div>
    {/if}
  </section>
{/if}

{#if !loadError && hosts.length === 0}
  <Empty text="No machines enrolled. One line pasted into a terminal joins one." action="New remote" onaction={() => (enroll = { name: '', rebuildFrom: '' })} />
{:else if !loadError}
  <div class="grid">
    {#each hosts as h (h.id)}
      {@const f = facts(h)}
      <section class="card plate" class:trouble={h.status === 'offline' || h.status === 'mismatch'}>
        <header>
          <span class="led {h.status}" title={status(h)}></span>
          <strong>{h.name}</strong>
          {#if h.status !== 'online'}<span class="pill bad">{status(h)}</span>{/if}
          <span class="grow"></span>
          <span class="addr">{h.addr}</span>
          <span class="muted small">{ago(h.lastSeen)}</span>
        </header>
        {#if f.cores}
          <Stat label="memory" of={bytes(f.memTotal)} pct={memPct(f)} hot={memPct(f) > 85} min="11rem">{bytes(f.memUsed)}</Stat>
          <div class="stats">
            <Stat label="cores">{f.cores}</Stat>
            <Stat label="load">{f.load1}</Stat>
            {#if f.docker}<Stat label="docker">{f.docker}</Stat>{/if}
            {#if f.gpu}<Stat label={f.gpu.busy ? 'gpu busy' : 'gpu idle'}><span class="led {f.gpu.busy ? 'busy' : 'ok'}"></span> {f.gpu.name || 'GPU'}</Stat>{/if}
          </div>
          <div class="mounts">
            {#each f.mounts ?? [] as m}
              {@const pct = m.size ? Math.round((100 * (m.size - m.free)) / m.size) : 0}
              <div class="mount">
                <code>{m.path}</code>
                <span class="muted small">{bytes(m.free)} free of {bytes(m.size)}</span>
                {#if m.path === '/' && rootFree(h).length > 1}
                  <svg viewBox="0 0 80 20" class="spark" preserveAspectRatio="none"><path d={sparkPath(rootFree(h))} /></svg>
                {:else}<span></span>{/if}
                <Fill {pct} hot={pct > 85} />
              </div>
            {/each}
          </div>
        {:else}
          <p class="muted">Nothing reported yet.</p>
        {/if}
        <div class="switches">
          <label class="check" title={f.lock ? '' : 'The machine has not reported its SSH config yet'}>
            <input type="checkbox" checked={f.lock?.locked ?? false} disabled={h.status !== 'online' || busy[h.id]}
              onchange={(e) => act(h, () => post(`/hosts/${h.id}/lock`, { locked: e.target.checked }))} />
            Locked to the hub
          </label>
          <label class="check">
            <input type="checkbox" checked={f.ollama?.installed ?? false} disabled={h.status !== 'online' || busy[h.id]}
              onchange={(e) => toggleOllama(h, e.target.checked)} />
            Ollama {#if ollamaBusy[h.id]}<span class="muted">{ollamaBusy[h.id]}…</span>{:else if f.ollama?.installed}<span class="muted">{f.ollama.running ? 'running' : 'installed, not running'}</span>{/if}
          </label>
        </div>
        <div class="agent">
          {#if f.agent}
            <span>Agent <span class="muted">omp {f.agent.version}</span></span>
            <span class="muted">{f.agent.model || 'fleet default'}</span>
            <button class="small" disabled={busy[h.id] || h.status !== 'online'} aria-expanded={modelEdit?.id === h.id} onclick={() => (modelEdit = modelEdit?.id === h.id ? null : { id: h.id, pick: splitModel(f.agent.model), err: '' })}>Change</button>
            <span class="muted">{h.credentialsRevokedAt ? 'credentials revoked' : f.agent.snapshotAge == null ? 'no credentials yet' : `credentials ${Math.round(f.agent.snapshotAge / 60)}m old`}</span>
            {#if f.memTotal && f.memTotal < 1400 * 1048576}<span class="warn">too little memory for the agent to run</span>{/if}
            {#if !f.agent.account}<span class="warn">no agent account: re-provision to run jobs</span>{:else if !f.agent.cage}<span class="warn">the agent's network cage is not up</span>{/if}
          {:else}
            <span class="muted">No agent reported.</span>
          {/if}
        </div>
        {#if modelEdit?.id === h.id}
          <form class="form model-edit" use:useEscape={() => (modelEdit = null)} onsubmit={(e) => { e.preventDefault(); saveModel(h); }}>
            <label>Model for {h.name}'s agent
              <ModelPick bind:pick={modelEdit.pick} {providers} blank="fleet default" listId="host-{h.id}-models" />
            </label>
            {#if modelEdit.err}<Notice>{modelEdit.err}</Notice>{/if}
            {#if providersError}<Notice>Providers could not be listed from omp: {providersError}</Notice>{/if}
            <div class="row"><button type="submit" class="primary small">Save</button><button type="button" class="quiet small" onclick={() => (modelEdit = null)}>Cancel</button></div>
          </form>
        {/if}
        {#if note[h.id]}<p class="muted small">{note[h.id]}</p>{/if}
        <footer>
          <button disabled={busy[h.id] || h.status !== 'online'} onclick={() => act(h, () => post(`/hosts/${h.id}/credentials`))}>Update credentials</button>
          <button disabled={busy[h.id]} onclick={() => confirm(`Revoke ${h.name}'s vault access? Its jobs run on the encrypted snapshot only until you update credentials again.`) && act(h, () => post(`/hosts/${h.id}/credentials/revoke`))}>Revoke</button>
          <button disabled={busy[h.id] || h.status !== 'online'} onclick={() => reprovision(h)}>Re-provision</button>
          <button disabled={h.status !== 'online'} onclick={() => (publish = publish === h.id ? null : h.id)}>Publish a port</button>
          <button class="quiet" onclick={() => { open = open === h.id ? null : h.id; script = h.rebuildScript; fit = {}; pickIface(h); }} aria-expanded={open === h.id}><span class="chev" class:open={open === h.id}><Icon name="chevron" size={14} /></span> More</button>
          <span class="grow"></span>
          <button class="danger quiet" onclick={() => confirm(`Remove ${h.name} from the hub? Nothing on the machine changes.`) && act(h, () => del(`/hosts/${h.id}`))}>Remove</button>
        </footer>
        {#if publish === h.id}
          <Publish host={h.name} onclose={() => (publish = null)} />
        {/if}
        {#if open === h.id}
          <div class="more">
            <h4>Rebuild script</h4>
            <p class="help">What takes a fresh Debian to this machine's state. The hub's agent keeps it after every job; you can edit it.</p>
            <textarea bind:value={script} rows="10" spellcheck="false"></textarea>
            <div class="row"><button onclick={() => act(h, () => put(`/hosts/${h.id}/rebuild-script`, { script }))}>Save script</button></div>
            <h4>What fits</h4>
            <div class="row"><button disabled={busy[h.id] || h.status !== 'online'} onclick={() => act(h, async () => { fit = { ...fit, [h.id]: await get(`/hosts/${h.id}/fit?n=8`) }; })}>Ask llmfit on {h.name}</button></div>
            {#if fit[h.id]}
              <div class="scroll">
              <table class="stack">
                <tbody>
                  {#each fit[h.id].models as m}
                    <tr><td><code>{m.ollama}</code></td><td>{m.fit}</td><td>{m.tokensPerSec ? `${m.tokensPerSec.toFixed(1)} tok/s` : ''}</td><td>{m.memoryGB.toFixed(1)} GB</td><td class="muted">{m.capabilities.join(', ')}</td></tr>
                  {/each}
                </tbody>
              </table>
              </div>
              {#if fit[h.id].models.length === 0}<p class="muted">llmfit found nothing pullable that fits.</p>{/if}
            {/if}
            <h4>Address</h4>
            <p class="help">What each interface reports. The hub reaches this machine at <code>{h.addr}</code>; a held address survives a new lease.</p>
            {#if ifaces(h).length === 0}
              <p class="muted">No interfaces reported yet.</p>
            {:else}
              <div class="ifaces">
                {#each ifaces(h) as i (i.name)}
                  <div class="iface"><code>{i.name}</code><span class="muted small">{i.mac}</span>
                    {#each i.addrs ?? [] as a}<span>{a.cidr} <span class="pill {a.dynamic ? '' : 'ok'}">{a.dynamic ? 'DHCP' : 'held'}</span></span>{:else}<span class="muted">no address</span>{/each}
                  </div>
                {/each}
                {#if f.network?.gateway}<div class="iface muted small">gateway {f.network.gateway} via {f.network.via}{#if f.network.dns?.length} · DNS {f.network.dns.join(', ')}{/if}{#if f.network.manager} · {f.network.manager}{/if}</div>{/if}
              </div>
              {#if f.network?.pending}<Notice>An address change is still waiting to be confirmed or reverted on {h.name}.</Notice>{/if}
              {#if addr && open === h.id}
                <form class="form addr-edit" onsubmit={(e) => { e.preventDefault(); setAddress(h, false); }}>
                  <div class="row fields">
                    <label>Interface
                      <select bind:value={addr.iface} onchange={(e) => pickIface(h, e.target.value)}>
                        {#each ifaces(h) as i}<option value={i.name}>{i.name}</option>{/each}
                      </select>
                    </label>
                    <label>Address <input bind:value={addr.cidr} placeholder="192.168.1.20/24" required /></label>
                    <label>Gateway <input bind:value={addr.gateway} placeholder="192.168.1.1" /></label>
                    <label>DNS <input bind:value={addr.dns} placeholder="192.168.1.1 1.1.1.1" /></label>
                  </div>
                  <div class="row">
                    <button type="submit" class="primary small" disabled={busy[h.id] || h.status !== 'online' || f.network?.pending}>Hold this address</button>
                    <button type="button" class="quiet small" disabled={busy[h.id] || h.status !== 'online' || f.network?.pending} onclick={() => setAddress(h, true)}>Back to DHCP</button>
                  </div>
                </form>
              {/if}
            {/if}
            <h4>Host key</h4>
            <code class="key">{h.hostKey}</code>
          </div>
        {/if}
      </section>
    {/each}
  </div>
{/if}

<style>
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 26rem), 1fr)); gap: 1rem; }
  .addr { font-family: var(--mono); font-size: 0.8em; color: var(--muted); }
  .spark { width: 64px; height: 18px; }
  .spark path { fill: none; stroke: var(--muted); stroke-width: 1.5; }
  .switches { display: flex; gap: 1.2rem; flex-wrap: wrap; padding: 0.2rem 1rem; }
  .agent { display: flex; gap: 0.35rem 0.9rem; flex-wrap: wrap; align-items: center; font-size: 0.85em; padding: 0.5rem 1rem 0.8rem; }
  .model-edit { gap: 0.4rem; padding: 0 1rem 0.8rem; }
  .more { border-top: 1px solid var(--line); padding: 0.25rem 1rem 1rem; background: color-mix(in srgb, var(--sunk) 40%, var(--card)); }
  .more h4 { margin-top: 1rem; }
  .plate :global(.card) { margin: 0 1rem 1rem; }
  .key { font-size: 0.75em; overflow-wrap: anywhere; color: var(--muted); }
  .ifaces { display: flex; flex-direction: column; gap: 0.3rem; font-size: 0.85em; margin-bottom: 0.6rem; }
  .iface { display: flex; gap: 0.6rem; flex-wrap: wrap; align-items: center; }
  .addr-edit { gap: 0.4rem; }
</style>
