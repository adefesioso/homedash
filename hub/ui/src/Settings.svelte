<script>
  import Notice from './ui/Notice.svelte';
  import { get, post, put, del } from './lib/api.js';
  import { useEscape } from './lib/actions.js';
  import { splitModel, joinModel, halfPicked, badModel } from './lib/model.js';
  import ModelPick from './ModelPick.svelte';
  import Restore from './Restore.svelte';
  let { hub, user, onauth } = $props();
  let users = $state([]);
  let invites = $state([]);
  let tokens = $state([]);
  let newToken = $state('');
  let tokenName = $state('');

  // Settings: the few values the hub takes from a person. Everything
  // else on this page is what the hub reports about itself.
  let s = $state({});
  let saved = $state('');
  let agent = $state(null);
  let gate = $state([]);
  let secrets = $state([]);
  let hosts = $state([]);
  let newSecret = $state({ name: '', value: '', hosts: [] });
  let backup = $state(null);
  let passphrase = $state('');
  let exporting = $state(false);
  let loadError = $state('');
  let error = $state('');
  let modelError = $state('');
  let remoteModelError = $state('');
  let ompBusy = $state(false);
  // providers: what the vault can run, from omp (GET /agents/models);
  // ModelPick edits each setting as {provider, model} (lib/model.js).
  let providers = $state([]);
  let providersError = $state('');
  let hubPick = $state({ provider: '', model: '' });
  let remotePick = $state({ provider: '', model: '' });

  // on: the boolean vocabulary a setting may arrive in — the current
  // "true", or "1"/"on" from an older row (mirrors settingBool in
  // internal/peers). A save always writes back "true"/"" (C-11).
  const on = (v) => v === '1' || v === 'true' || v === 'on';
  async function load() {
    try {
      [s, agent, gate, secrets, hosts, backup, users, invites, tokens] = await Promise.all([get('/settings'), get('/agents'), get('/gate'), get('/secrets'), get('/hosts'), get('/backup'), get('/users'), get('/invites'), get('/tokens')]);
      loadError = '';
      hubPick = splitModel(s['agent.default_model']);
      remotePick = splitModel(s['agent.remote_model']);
    } catch (e) { loadError = e.message; }
    try { providers = await get('/agents/models'); providersError = ''; }
    catch (e) { providersError = e.message; }
  }
  $effect(() => { load(); });

  // put('/settings') is a map[string]string on the wire; type="number"
  // inputs bind their value as a JS number (Svelte's own coercion), so
  // without this the body carries a bare number and the hub's JSON
  // decode into map[string]string 400s.
  async function save() {
    try {
      const payload = Object.fromEntries(Object.entries(s).map(([k, v]) => [k, v == null ? '' : String(v)]));
      s = await put('/settings', payload); saved = 'Saved'; setTimeout(() => (saved = ''), 2000); error = '';
    } catch (e) { error = e.message; }
  }
  // saveAgents: native min/max blocks an out-of-range number silently —
  // no submit, no message a headless run can see — so ask the form
  // directly instead of trusting the click to have gotten this far, and
  // check the model's shape ourselves since there is no HTML pattern
  // for "provider/model" (C-1).
  async function saveAgents(e) {
    e.preventDefault();
    modelError = ''; remoteModelError = '';
    if (!e.target.reportValidity()) { error = 'fix the highlighted field before saving'; return; }
    if (halfPicked(hubPick)) { modelError = 'pick a provider and type a model, or leave both blank'; return; }
    if (halfPicked(remotePick)) { remoteModelError = 'pick a provider and type a model, or leave both blank'; return; }
    s['agent.default_model'] = joinModel(hubPick);
    s['agent.remote_model'] = joinModel(remotePick);
    if (badModel(s['agent.default_model'])) {
      modelError = 'blank, or provider/model such as anthropic/claude-sonnet-5 or homedash/qwen2.5:3b';
      return;
    }
    if (badModel(s['agent.remote_model'])) {
      remoteModelError = 'blank, or provider/model such as anthropic/claude-sonnet-5 or homedash/qwen2.5:3b';
      return;
    }
    await save();
  }
  // updateOmp: the hub re-fetches its own pinned omp right away, and
  // every online remote catches up the only way a remote ever does — its
  // own Re-provision — so this just walks the same endpoint that button
  // hits, one host at a time, collecting failures the way "Update
  // credentials everywhere" on Hosts does (one host's failure must not
  // hide another's).
  async function updateOmp() {
    if (!confirm(`Update oh-my-pi? The hub re-fetches its pinned build now. Every online remote re-runs its enrollment layout over SSH — accounts, key, cage and the pinned agent, not just omp — which is the only way a remote picks up a newer version; each takes a few minutes and finishes as an event.`)) return;
    ompBusy = true;
    const failed = [];
    try { await post('/agents/update'); } catch (e) { failed.push(`this hub: ${e.message}`); }
    for (const h of hosts) {
      if (h.status !== 'online') continue;
      try { await post(`/hosts/${h.id}/reprovision`); } catch (e) { failed.push(`${h.name}: ${e.message}`); }
    }
    error = failed.join(' · ');
    ompBusy = false;
    await load();
  }
  async function addSecret() {
    try {
      await put(`/secrets/${encodeURIComponent(newSecret.name.trim())}`, { value: newSecret.value, hosts: newSecret.hosts.map(Number) });
      newSecret = { name: '', value: '', hosts: [] };
      error = '';
      await load();
    } catch (e) { error = e.message; }
  }
  async function removeSecret(name) {
    if (!confirm(`Delete secret ${name}?`)) return;
    try { await del(`/secrets/${encodeURIComponent(name)}`); error = ''; await load(); } catch (e) { error = e.message; }
  }
  const hostName = (id) => hosts.find((h) => h.id === id)?.name ?? `#${id}`;
  async function addPasskey() {
    try {
      const { ceremony, options } = await post('/auth/register/begin', {});
      const cred = await navigator.credentials.create({ publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(options.publicKey) });
      const r = await fetch(`/api/auth/register/finish?ceremony=${ceremony}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(cred.toJSON()) });
      if (!r.ok) throw new Error(await r.text());
      onauth?.(); error = ''; await load();
    } catch (e) { error = e.message; }
  }
  // A role change can be this session's own (an admin demoting itself
  // down to the last other admin) or another admin's header going
  // stale; either way the panel's idea of `auth.user` can be wrong until
  // /auth/state is read again (C-5). App.svelte does that on this event.
  async function setRole(name, role) {
    try { await put(`/users/${encodeURIComponent(name)}/role`, { role }); error = ''; await load(); dispatchEvent(new CustomEvent('homedash:auth-changed')); }
    catch (e) { error = e.message; }
  }
  async function mintToken() {
    try { newToken = (await post('/tokens', { name: tokenName, role: 'admin' })).token; tokenName = ''; error = ''; await load(); } catch (e) { error = e.message; }
  }
  async function removeUser(name) {
    if (!confirm(`Remove ${name}?`)) return;
    try { await del(`/users/${encodeURIComponent(name)}`); error = ''; await load(); } catch (e) { error = e.message; }
  }
  async function invite(body) {
    try { await post('/invites', body); error = ''; await load(); } catch (e) { error = e.message; }
  }
  async function revokeInvite(code) {
    try { await del(`/invites/${encodeURIComponent(code)}`); error = ''; await load(); } catch (e) { error = e.message; }
  }
  async function revokeToken(name) {
    try { await del(`/tokens/${encodeURIComponent(name)}`); error = ''; await load(); } catch (e) { error = e.message; }
  }
  // The export is a download, so it is a fetch of bytes rather than the
  // JSON wrapper: the passphrase goes in the body, the file name comes
  // back in Content-Disposition.
  async function exportNow() {
    exporting = true;
    try {
      const r = await fetch('/api/backup/export', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ passphrase }) });
      if (!r.ok) throw new Error((await r.text()).trim() || `the hub answered ${r.status}`);
      const name = (r.headers.get('content-disposition') || '').match(/filename="([^"]+)"/)?.[1] || 'homedash.tar.age';
      const url = URL.createObjectURL(await r.blob());
      const a = document.createElement('a');
      a.href = url; a.download = name; a.click();
      URL.revokeObjectURL(url);
      passphrase = ''; error = '';
      backup = await get('/backup');
    } catch (e) { error = e.message; }
    exporting = false;
  }
</script>

{#if loadError}<Notice>{loadError}</Notice>{/if}
{#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}
<div class="layout">
<nav class="toc" aria-label="Settings sections">
  <a href="#settings" onclick={(e) => { e.preventDefault(); document.getElementById('s-hub')?.scrollIntoView({ behavior: 'smooth', block: 'start' }); }}>This hub</a><a href="#settings" onclick={(e) => { e.preventDefault(); document.getElementById('s-agents')?.scrollIntoView({ behavior: 'smooth', block: 'start' }); }}>Agents</a><a href="#settings" onclick={(e) => { e.preventDefault(); document.getElementById('s-space')?.scrollIntoView({ behavior: 'smooth', block: 'start' }); }}>Space</a><a href="#settings" onclick={(e) => { e.preventDefault(); document.getElementById('s-accounts')?.scrollIntoView({ behavior: 'smooth', block: 'start' }); }}>Accounts</a><a href="#settings" onclick={(e) => { e.preventDefault(); document.getElementById('s-backup')?.scrollIntoView({ behavior: 'smooth', block: 'start' }); }}>Backup</a><a href="#settings" onclick={(e) => { e.preventDefault(); document.getElementById('s-secrets')?.scrollIntoView({ behavior: 'smooth', block: 'start' }); }}>Secrets</a><a href="#settings" onclick={(e) => { e.preventDefault(); document.getElementById('s-refusals')?.scrollIntoView({ behavior: 'smooth', block: 'start' }); }}>Refusals</a>
</nav>
<div class="sections">

<section id="s-hub" class="card">
  <h2>This hub</h2>
  {#if hub}
    <dl>
      <dt>Version</dt><dd>{hub.version}</dd>
      <dt>Running since</dt><dd>{new Date(hub.started).toLocaleString()}</dd>
      <dt>State file</dt><dd><code>{hub.statePath}</code></dd>
      <dt>Public key</dt><dd><code class="key">{hub.publicKey}</code></dd>
      <dt>Ollama endpoint</dt><dd><code>{location.origin}</code> <span class="muted">— the pool, one address; existing clients work unchanged</span></dd>
      <dt>Command line</dt><dd><code>homedash login {location.origin}</code> <span class="muted">— on your workstation; signs in with your passkey through this browser</span></dd>
    </dl>
  {/if}
</section>

<section id="s-agents" class="card">
  <h2>Agents</h2>
  {#if agent}
    <dl>
      <dt>omp</dt><dd>{agent.ompVersion} — {agent.ready ? 'installed' : (agent.installError ? `not installed: ${agent.installError}` : 'fetching…')}</dd>
      <dt>Credential vault</dt><dd>{agent.vaultRunning ? 'running' : 'not running'}</dd>
    </dl>
    <div class="row"><button onclick={updateOmp} disabled={ompBusy}>Update omp globally</button> {#if ompBusy}<span class="muted">starting…</span>{/if}</div>
  {/if}
  <form class="form" onsubmit={saveAgents}>
    <label>Hub model
      <ModelPick bind:pick={hubPick} {providers} listId="hub-models" />
      {#if modelError}<Notice>{modelError}</Notice>{/if}
    </label>
    <label>Remote model (no model: same as the hub)
      <ModelPick bind:pick={remotePick} {providers} listId="remote-models" />
      {#if remoteModelError}<Notice>{remoteModelError}</Notice>{/if}
    </label>
    {#if providersError}<Notice small>Providers could not be listed from omp: {providersError}</Notice>{/if}
    <label>Rounds before a job needs you <input type="number" min="1" max="10" placeholder="3" bind:value={s['agent.rounds']} /></label>
    <label>Per-job time, seconds <input type="number" min="60" placeholder="1200" bind:value={s['jobs.timeout']} /></label>
    <label>Jobs kept per host <input type="number" min="5" placeholder="20" bind:value={s['jobs.retention']} /></label>
    <label class="check"><input type="checkbox" checked={s['jobs.sudo'] !== 'off'} onchange={(e) => (s['jobs.sudo'] = e.target.checked ? '' : 'off')} /> Jobs may ask for root through homedash-sudo (one checked, logged command at a time; off means a job has no route to root)</label>
    <label>Per-job memory cap, e.g. 2G (blank for none) <input placeholder="none" bind:value={s['jobs.memory_max']} /></label>
    <label>Per-job CPU quota, e.g. 200% (blank for none) <input placeholder="none" bind:value={s['jobs.cpu_quota']} /></label>
    <label>Mountpoint fullness threshold, % <input type="number" min="50" max="99" placeholder="90" bind:value={s['notify.disk_percent']} /></label>
    <label>Network scan interval, minutes (0 is off) <input type="number" min="0" placeholder="10" bind:value={s['network.scan_minutes']} /></label>
    <label>Forget unnamed devices unseen for, days <input type="number" min="1" placeholder="30" bind:value={s['network.forget_days']} /></label>
    <label>Notification target <input placeholder="https://ntfy.sh/your-topic, or any URL to POST at" bind:value={s['notify.target']} /></label>
    <label>Router queue depth <input type="number" min="1" placeholder="8" bind:value={s['router.queue']} /></label>
    <label>Hub address on the LAN <input placeholder="worked out from the default route if blank" bind:value={s['hub.lan_addr']} /></label>
    <div><button type="submit" class="primary">Save</button> <span class="muted">{saved}</span></div>
  </form>
</section>

<section id="s-space" class="card">
  <h2>Space</h2>
  <p class="help">A shared name is the rendezvous: every hub that typed the same one finds the others over a DHT, with no server in the middle. The name is a password — on the public DHT anyone who guesses it finds the space — so make it one. Work leaves the house one way, and it is not a default.</p>
  <form class="form" onsubmit={(e) => { e.preventDefault(); save(); }}>
    <label>Space name <input placeholder="e.g. elm-street-labs-7f3k9q" bind:value={s['space.name']} /></label>
    <label class="check"><input type="checkbox" checked={on(s['space.enabled'])} onchange={(e) => (s['space.enabled'] = e.target.checked ? 'true' : '')} /> Joined (on)</label>
    <label>Network <select bind:value={s['space.network']}><option value="">public — the IPFS DHT, nothing to run</option><option value="private">private — a DHT of your hubs only</option></select></label>
    {#if s['space.network'] === 'private'}
      <label>Bootstrap hubs <textarea rows="3" placeholder="one address per line, copied from a reachable hub's Peers tab: /ip4/…/tcp/…/p2p/12D3…" bind:value={s['space.bootstrap']} spellcheck="false"></textarea></label>
      <label>Network key (optional) <input placeholder="64 hex characters, the same on every hub; blank is none" bind:value={s['space.psk']} spellcheck="false" /> <button type="button" class="small" onclick={() => (s['space.psk'] = Array.from(crypto.getRandomValues(new Uint8Array(32)), (b) => b.toString(16).padStart(2, '0')).join(''))}>generate</button></label>
      <p class="help">A keyed network runs on TCP only, and needs the key pasted into every member before it can see them.</p>
    {/if}
    <label class="check"><input type="checkbox" checked={on(s['space.reachable'])} onchange={(e) => (s['space.reachable'] = e.target.checked ? 'true' : '')} /> This hub is reachable from the internet (a public address, or the listen port forwarded) — on a private network it serves the DHT and relays for the rest</label>
    <label>Listen port <input type="number" min="0" max="65535" placeholder="picked at random if blank" bind:value={s['space.port']} /></label>
    <label class="check"><input type="checkbox" checked={on(s['space.serve'])} onchange={(e) => (s['space.serve'] = e.target.checked ? 'true' : '')} /> Run peers' jobs on my machines (off by default)</label>
    <label>This hub's name to peers <input placeholder="hostname if blank" bind:value={s['space.hub_name']} /></label>
    <label>Unknown peers <select bind:value={s['space.unknown']}><option value="">refuse until approved</option><option value="accept">accept under the default quota</option></select></label>
    <label>Default max concurrent per peer <input type="number" min="0" placeholder="1" bind:value={s['space.default_concurrent']} /></label>
    <label>Default per hour per peer <input type="number" min="0" placeholder="20" bind:value={s['space.default_per_hour']} /></label>
    <label>Hub-wide ceiling on peers' jobs at once <input type="number" min="0" placeholder="2" bind:value={s['space.ceiling']} /></label>
    <label>Hub-wide ceiling on peers' jobs per hour <input type="number" min="0" placeholder="100" bind:value={s['space.per_hour']} /></label>
    <label>Hub-wide ceiling on connections to published services at once <input type="number" min="0" placeholder="64" bind:value={s['space.connections']} /></label>
    <label>Connections any one peer may hold of those <input type="number" min="0" placeholder="16" bind:value={s['space.peer_connections']} /></label>
    <label>Bandwidth per peer for published services, KB/s <input type="number" min="0" placeholder="unlimited if blank" bind:value={s['space.peer_kbps']} /></label>
    <div><button type="submit" class="primary">Save</button> <span class="muted">{saved}</span></div>
  </form>
</section>

<section id="s-accounts" class="card">
  <h2>Accounts</h2>
  {#if user && user.passkeys < 2}
    <Notice>You have one passkey. One passkey on one phone is a lockout waiting to happen — <button class="link" onclick={addPasskey}>add a second passkey</button> on another device (or make an invite for yourself below and open it there).</Notice>
  {/if}
  <div class="scroll">
  <table>
    <tbody>
      {#each users as u (u.name)}
        <tr>
          <td><strong>{u.name}</strong></td>
          <td>
            <select value={u.role} onchange={(e) => setRole(u.name, e.target.value)} disabled={u.name === user?.name}>
              <option value="admin">admin</option><option value="viewer">viewer</option>
            </select>
          </td>
          <td class="muted">{u.passkeys} passkey{u.passkeys === 1 ? '' : 's'}</td>
          <td class="actions">{#if u.name !== user?.name}<button class="small danger quiet" onclick={() => removeUser(u.name)}>Remove</button>{/if}</td>
        </tr>
      {/each}
    </tbody>
  </table>
  </div>
  <div class="row">
    <button onclick={() => invite({ role: 'viewer' })}>Invite a viewer</button>
    <button onclick={() => invite({ role: 'admin' })}>Invite an admin</button>
    <button onclick={() => invite({ self: true })}>A code for my second passkey</button>
  </div>
  {#if invites.length > 0}
    <div class="scroll">
    <table>
      <tbody>
        {#each invites as i (i.code)}
          <tr><td><code>{i.code}</code></td><td class="muted">{i.user ? `a passkey for ${i.user}` : `a new ${i.role}`}</td><td class="muted">good until {new Date(i.expires).toLocaleString()}</td><td class="actions"><button class="small danger quiet" onclick={() => revokeInvite(i.code)}>Revoke</button></td></tr>
        {/each}
      </tbody>
    </table>
    </div>
  {/if}
  <h3>API tokens</h3>
  <p class="help">For a script that can't open a browser: <code>Authorization: Bearer …</code> on the API, or <code>HOMEDASH_TOKEN</code> for the command line. Shown once.</p>
  {#if newToken}<pre use:useEscape={() => (newToken = '')}>{newToken}</pre>{/if}
  {#if tokens.length > 0}
    <div class="scroll">
    <table><tbody>{#each tokens as t (t.name)}<tr><td><code>{t.name}</code></td><td class="muted">{t.role}</td><td>{#if t.name !== 'hub-agent'}<button class="small danger quiet" onclick={() => revokeToken(t.name)}>Revoke</button>{:else}<span class="muted">the hub's own sessions</span>{/if}</td></tr>{/each}</tbody></table>
    </div>
  {/if}
  <div class="row"><input placeholder="token name, e.g. backup-script" bind:value={tokenName} /><button class="primary" onclick={mintToken} disabled={!tokenName.trim()}>Make a token</button></div>
</section>

<section id="s-backup" class="card">
  <h2>Backup</h2>
  <p class="help">The whole state directory — hosts and their keys, clusters, tasks, secrets, accounts, the vault — as one file, encrypted under a passphrase before it leaves, downloaded by your browser. Keep it wherever you keep things. Restoring it puts all of that back and restarts the hub; a fresh hub takes it on its setup page.</p>
  {#if backup}
    <dl>
      <dt>Last export</dt><dd>{backup.last ? new Date(backup.last).toLocaleString() : 'never'}</dd>
    </dl>
  {/if}
  <form class="form" onsubmit={(e) => { e.preventDefault(); exportNow(); }}>
    <div class="row"><input type="password" placeholder="a passphrase for the file" autocomplete="new-password" bind:value={passphrase} /><button type="submit" class="primary" disabled={exporting || !passphrase}>{exporting ? 'Exporting…' : 'Export'}</button></div>
  </form>
  <p class="help">Restore from an export made on this hub or another:</p>
  <Restore onerror={(m) => (error = m)} />
</section>

<section id="s-secrets" class="card">
  <h2>Secrets</h2>
  <p class="help">Named values kept encrypted on the hub. A job's remote reads one with <code>homedash-secret NAME</code> while the job runs; values are never shown again here.</p>
  {#if secrets.length > 0}
    <div class="scroll">
    <table>
      <tbody>
        {#each secrets as sec (sec.name)}
          <tr>
            <td><code>{sec.name}</code></td>
            <td class="muted">{sec.hosts.length === 0 ? 'every host' : sec.hosts.map(hostName).join(', ')}</td>
            <td class="muted">updated {new Date(sec.updated).toLocaleString()}</td>
            <td class="actions"><button class="small danger quiet" onclick={() => removeSecret(sec.name)}>Delete</button></td>
          </tr>
        {/each}
      </tbody>
    </table>
    </div>
  {/if}
  <form class="form secret" onsubmit={(e) => { e.preventDefault(); addSecret(); }}>
    <input placeholder="NAME" bind:value={newSecret.name} />
    <input type="password" placeholder="value (replaces an existing name)" bind:value={newSecret.value} autocomplete="off" />
    <select multiple bind:value={newSecret.hosts} size="3">
      {#each hosts as h}<option value={String(h.id)}>{h.name}</option>{/each}
    </select>
    <button type="submit" class="primary" disabled={!newSecret.name.trim() || !newSecret.value}>Set</button>
    <span class="help">No host selected means every host.</span>
  </form>
</section>

<section id="s-refusals" class="card">
  <h2>What the hub refuses</h2>
  <p class="help">However it is asked — from the panel, a session, an outside assistant or a task — and on every remote's own agent as a hook.</p>
  <ul>{#each gate as g}<li>{g}</li>{/each}</ul>
</section>
</div>
</div>

<style>
  .layout { display: grid; grid-template-columns: 9rem 1fr; gap: 1.5rem; align-items: start; }
  .toc { position: sticky; top: 1.5rem; display: grid; gap: 0.1rem; }
  .toc a { color: var(--muted); font-size: 0.9em; padding: 0.3rem 0.6rem; border-radius: var(--r); }
  .toc a:hover { color: var(--fg); background: var(--sunk); text-decoration: none; }
  .sections { display: grid; gap: 1rem; min-width: 0; }
  section { padding: 1.1rem 1.25rem; scroll-margin-top: 1rem; min-width: 0; }
  h2 { margin: 0 0 0.75rem; }
  .key { font-size: 0.85em; }
  form { max-width: 44rem; }
  .row { margin: 0.5rem 0; }
  .row input { width: auto; flex: 1; min-width: 10rem; }
  h3 { margin: 1.25rem 0 0.25rem; }
  form.secret { grid-template-columns: 1fr 2fr auto auto; align-items: start; max-width: none; }
  form.secret .help { grid-column: 1 / -1; }
  table { margin-bottom: 0.75rem; box-shadow: none; background: transparent; }
  td { padding: 0.35rem 0.5rem; }
  ul { margin: 0; padding-left: 1.2rem; }
  pre { user-select: all; }
  @media (max-width: 860px) { .layout { grid-template-columns: 1fr; } .toc { position: static; grid-auto-flow: column; overflow-x: auto; min-width: 0; } form.secret { grid-template-columns: 1fr; } }
</style>
