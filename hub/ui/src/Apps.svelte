<script>
  import Notice from './ui/Notice.svelte';
  import { get, post } from './lib/api.js';
  import { useEscape } from './lib/actions.js';
  import Empty from './Empty.svelte';
  import Publish from './Publish.svelte';
  import Icon from './Icon.svelte';

  // The Apps tab: every stack on every remote, live, on each open. Paste
  // a compose file or install one handed over from the Catalog tab;
  // placement proposes where.
  let { installEntry = null, onconsumed } = $props();
  let stacks = $state([]);
  let errors = $state([]);
  let hosts = $state([]);
  let loadError = $state('');
  let error = $state('');
  let loading = $state(true);
  let install = $state(null);     // {name, compose, env, needs, host, verdicts}
  let open = $state(null);         // "host/name"
  let detail = $state({ file: null, logs: '' });
  let busy = $state(false);
  let publish = $state(null);      // {host, name, port} while the dialog is open
  let warning = $state('');        // a deploy that went through with a compose-gate warning (A-1)

  // A-4: deploying a name already running on the chosen host replaces
  // it — the API does this silently (it's the same call as Update), so
  // the form says so before you press Deploy rather than after.
  const clashes = $derived(!!install?.host && stacks.some((s) => s.host === install.host && s.name === install.name));

  // The first published host port docker reports for a stack, e.g. 0.0.0.0:8096->8096/tcp.
  const firstPort = (s) => { for (const c of s.containers) { const m = /:(\d+)->/.exec(c.ports || ''); if (m) return m[1]; } return ''; };

  async function load() {
    loading = true;
    try {
      const [a, h] = await Promise.all([get('/apps'), get('/hosts')]);
      stacks = a.stacks; errors = a.errors ?? []; hosts = h; loadError = '';
    } catch (e) { loadError = e.message; }
    loading = false;
  }
  $effect(() => { load(); });
  // An entry handed over from the Catalog tab opens the install dialog
  // pre-filled, once — consumed immediately so returning to this tab on
  // its own doesn't reopen it.
  $effect(() => { if (installEntry) { fromCatalog(installEntry); onconsumed?.(); } });

  async function fromCatalog(e) {
    install = { name: e.name, compose: e.compose, env: e.envTemplate ?? '', files: e.files ?? [], setupCommands: e.setupCommands ?? [], needs: e.needs, host: '', verdicts: null };
    await place();
  }
  async function place() {
    try { install.verdicts = await post('/apps/placement', install.needs); install.host = install.verdicts.find((v) => v.fits)?.host ?? ''; error = ''; }
    catch (e) { error = e.message; }
  }
  async function deploy() {
    busy = true;
    try {
      const r = await post(`/hosts/${install.host}/apps`, { name: install.name, compose: install.compose, env: install.env, files: install.files ?? [], setupCommands: install.setupCommands ?? [] });
      warning = r?.warning || '';
      install = null; error = '';
      await load();
    }
    catch (e) { error = e.message; }
    busy = false;
  }
  async function action(s, act, volumes = false) {
    if (act === 'remove' && !confirm(`Remove ${s.name} from ${s.host}${volumes ? ' with its volumes' : ''}?`)) return;
    busy = true;
    try { await post(`/hosts/${s.host}/apps/${s.name}`, { action: act, volumes }); error = ''; await load(); }
    catch (e) { error = e.message; }
    busy = false;
  }
  async function toggle(s) {
    const key = `${s.host}/${s.name}`;
    if (open === key) { open = null; return; }
    open = key; detail = { file: null, logs: '' };
    try {
      const [f, l] = await Promise.all([s.managed ? get(`/hosts/${s.host}/apps/${s.name}/file`) : null, fetch(`/api/hosts/${s.host}/apps/${s.name}/logs?lines=100`).then((r) => r.text())]);
      detail = { file: f, logs: l };
      error = '';
    } catch (e) { error = e.message; }
  }
  async function updateFile(s) {
    busy = true;
    try { await post(`/hosts/${s.host}/apps`, { name: s.name, compose: detail.file.compose, env: detail.file.env }); error = ''; await load(); }
    catch (e) { error = e.message; }
    busy = false;
  }
</script>

<div class="bar">
  <button class="primary" onclick={() => (install = { name: '', compose: '', env: '', files: [], setupCommands: [], needs: { cores: 1, memoryMB: 256, diskGB: 1, gpu: false }, host: '', verdicts: null })}><Icon name="plus" size={14} /> Install a stack</button>
  <button class="quiet" onclick={load} disabled={loading}>{loading ? 'Asking every remote…' : 'Refresh'}</button>
  {#if loadError}<Notice>{loadError}</Notice>{/if}
  {#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}
  {#each errors as e}<Notice>{e}</Notice>{/each}
  {#if warning}<Notice kind="muted" small ondismiss={() => (warning = '')}>Warning: {warning}</Notice>{/if}
</div>

{#if install}
  <section class="card form wide" use:useEscape={() => (install = null)}>
    <h3>{install.name ? `Install ${install.name}` : 'Install a stack'}</h3>
    <div class="row fields">
      <label>Name <input bind:value={install.name} placeholder="lowercase, digits, - _" /></label>
      <label>Cores <input type="number" bind:value={install.needs.cores} onchange={place} /></label>
      <label>Memory, MB <input type="number" bind:value={install.needs.memoryMB} onchange={place} /></label>
      <label>Disk, GB <input type="number" bind:value={install.needs.diskGB} onchange={place} /></label>
      <label class="check"><input type="checkbox" bind:checked={install.needs.gpu} onchange={place} /> GPU</label>
    </div>
    <label>compose.yml <textarea rows="10" bind:value={install.compose} spellcheck="false"></textarea></label>
    <label>.env (optional) <textarea rows="2" bind:value={install.env} spellcheck="false"></textarea></label>
    {#if install.files?.length || install.setupCommands?.length}
      <details class="setup-summary">
        <summary class="muted small">From the catalog: {install.files?.length ?? 0} setup file{install.files?.length === 1 ? '' : 's'}, {install.setupCommands?.length ?? 0} setup command{install.setupCommands?.length === 1 ? '' : 's'}</summary>
        {#each install.files ?? [] as f}<p class="mono small">{f.path}</p>{/each}
        {#each install.setupCommands ?? [] as c}<p class="mono small">{c}</p>{/each}
      </details>
    {/if}
    <h4>Where</h4>
    {#if install.verdicts}
      <div class="verdicts">
        {#each install.verdicts as v}
          <label class="verdict" class:no={!v.fits}><input type="radio" name="host" value={v.host} bind:group={install.host} disabled={!v.fits} /> <span class="led {v.fits ? 'ok' : 'bad'}"></span> <strong>{v.host}</strong> <span class="muted">{v.why}</span></label>
        {/each}
      </div>
    {:else}
      <div><button onclick={place}>Ask placement</button></div>
    {/if}
    {#if clashes}<p class="muted small">{install.name} is already running on {install.host}; this replaces the running stack.</p>{/if}
    <div class="row">
      <button class="primary" onclick={deploy} disabled={busy || !install.host || !install.name || !install.compose}>Deploy on {install.host || '…'}</button>
      <button class="quiet" onclick={() => (install = null)}>Cancel</button>
    </div>
  </section>
{/if}

{#if !loadError && stacks.length === 0 && !loading}
  <Empty text="No stacks on any remote. Pick one from the Catalog tab, or paste a compose file." action="Install a stack" onaction={() => (install = { name: '', compose: '', env: '', files: [], setupCommands: [], needs: { cores: 1, memoryMB: 256, diskGB: 1, gpu: false }, host: '', verdicts: null })} />
{:else if !loadError || loading}
  <div class="scroll">
  <table class="stack">
    <tbody>
      {#each stacks as s (s.host + '/' + s.name)}
        {@const up = /running|up/i.test(s.status)}
        <tr>
          <td><span class="led {up ? 'ok' : ''}"></span> <strong>{s.name}</strong> {#if !s.managed}<span class="muted small">(deployed by hand)</span>{/if}</td>
          <td class="mono">{s.host}</td>
          <td class="muted">{s.status}</td>
          <td class="small wide">{#each s.containers as c}<div>{c.name} <span class="muted">{c.state} · {c.ports || c.image}</span></div>{/each}</td>
          <td class="actions">
            <button class="small quiet" onclick={() => toggle(s)} aria-expanded={open === `${s.host}/${s.name}`}>{open === `${s.host}/${s.name}` ? 'Close' : 'Details'}</button>
            <button class="small" disabled={busy} onclick={() => action(s, 'start')}>Start</button>
            <button class="small" disabled={busy} onclick={() => action(s, 'stop')}>Stop</button>
            <button class="small" disabled={busy} onclick={() => action(s, 'restart')}>Restart</button>
            <button class="small" disabled={busy} onclick={() => action(s, 'pull')}>Pull</button>
            <button class="small" disabled={busy} onclick={() => (publish = { host: s.host, name: s.name, port: firstPort(s) })}>Publish</button>
            <button class="small danger quiet" disabled={busy} onclick={() => action(s, 'remove', false)}>Remove</button>
            <button class="small danger quiet" disabled={busy} onclick={() => action(s, 'remove', true)}>Remove + volumes</button>
          </td>
        </tr>
        {#if publish && publish.host === s.host && publish.name === s.name}
          <tr class="detail"><td colspan="5"><Publish host={publish.host} name={publish.name} port={publish.port} onclose={() => (publish = null)} /></td></tr>
        {/if}
        {#if open === `${s.host}/${s.name}`}
          <tr class="detail"><td colspan="5">
            {#if detail.file}
              <h4>compose.yml</h4>
              <textarea rows="8" bind:value={detail.file.compose} spellcheck="false"></textarea>
              <div class="row"><button class="small" disabled={busy} onclick={() => updateFile(s)}>Update</button></div>
            {/if}
            <h4>Logs</h4>
            <pre>{detail.logs || '…'}</pre>
          </td></tr>
        {/if}
      {/each}
    </tbody>
  </table>
  </div>
{/if}

<style>
  .fields label { min-width: 6rem; }
  .fields label:first-child { min-width: 14rem; }
  .verdicts { display: grid; gap: 0.3rem; }
  .verdict { display: flex; align-items: center; gap: 0.4rem; color: var(--fg); }
  .verdict.no { color: var(--muted); }
  pre { max-height: 16rem; }
  .setup-summary { font-size: 0.9em; }
  .setup-summary summary { cursor: pointer; }

  /* The name was losing its width to the actions column's eight buttons
     forcing themselves onto one line; letting actions wrap onto more
     than one line gives that room back to the name. */
  table.stack td:first-child { min-width: 16rem; overflow-wrap: anywhere; }
  table.stack td.mono { white-space: nowrap; }
  table.stack td.actions { white-space: normal; }
  table.stack td.actions button + button { margin-left: 0; }
  table.stack td.actions button { margin: 0.15rem 0 0 0.4rem; }
</style>
