<script>
  import Notice from './ui/Notice.svelte';
  import { get, post } from './lib/api.js';
  import { useEscape } from './lib/actions.js';
  import Empty from './Empty.svelte';
  import Publish from './Publish.svelte';
  import Icon from './Icon.svelte';
  import Menu from './ui/Menu.svelte';

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

  const blank = () => ({ name: '', compose: '', env: '', files: [], setupCommands: [], needs: { cores: 1, memoryMB: 256, diskGB: 1, gpu: false }, host: '', verdicts: null });
  let filter = $state('');

  // A stack's state from its containers, not docker's "running(4)": all
  // up, some up, or none.
  const upCount = (s) => s.containers.filter((c) => c.state === 'running').length;
  function health(s) {
    const up = upCount(s), n = s.containers.length;
    if (n && up === n) return { led: 'ok', pill: 'ok', text: 'Running' };
    if (up) return { led: 'busy', pill: 'accent', text: `${up} of ${n} up` };
    return { led: '', pill: '', text: /exited/i.test(s.status) || n ? 'Stopped' : s.status || 'Unknown' };
  }
  // The host ports a container publishes, once each: 0.0.0.0:53->53/udp, :::53->53/udp → 53/udp.
  function ports(c) {
    const seen = new Set();
    for (const m of (c.ports || '').matchAll(/:(\d+)->\d+\/(\w+)/g)) seen.add(m[2] === 'tcp' ? m[1] : `${m[1]}/${m[2]}`);
    return [...seen];
  }
  // An image without its registry: ghcr.io/immich-app/immich-server:release → immich-server:release.
  const image = (i) => (i || '').split('/').pop();

  const shown = $derived(filter.trim()
    ? stacks.filter((s) => `${s.name} ${s.host} ${s.containers.map((c) => c.name + ' ' + c.image).join(' ')}`.toLowerCase().includes(filter.trim().toLowerCase()))
    : stacks);
  // Stacks under the remote they run on, in the order the hub lists its hosts.
  const groups = $derived.by(() => {
    const order = hosts.map((h) => h.name);
    const by = new Map();
    for (const s of shown) { if (!by.has(s.host)) by.set(s.host, []); by.get(s.host).push(s); }
    return [...by].sort(([a], [b]) => (order.indexOf(a) + 1 || 1e9) - (order.indexOf(b) + 1 || 1e9) || a.localeCompare(b))
      .map(([host, list]) => ({ host, list, status: hosts.find((h) => h.name === host)?.status }));
  });
  const running = $derived(stacks.filter((s) => s.containers.length && upCount(s) === s.containers.length).length);

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
  <button class="primary" onclick={() => (install = blank())}><Icon name="plus" size={14} /> Install a stack</button>
  <button class="quiet" onclick={load} disabled={loading}><Icon name="restart" size={14} /> {loading ? 'Asking every remote…' : 'Refresh'}</button>
  {#if stacks.length}
    <span class="summary muted small"><strong>{stacks.length}</strong> stack{stacks.length === 1 ? '' : 's'} · <strong>{running}</strong> running · <strong>{new Set(stacks.map((s) => s.host)).size}</strong> remote{new Set(stacks.map((s) => s.host)).size === 1 ? '' : 's'}</span>
    <label class="search"><Icon name="search" size={14} /><input type="search" placeholder="Filter stacks" aria-label="Filter stacks" bind:value={filter} /></label>
  {/if}
</div>
{#if loadError}<Notice>{loadError}</Notice>{/if}
{#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}
{#each errors as e}<Notice>{e}</Notice>{/each}
{#if warning}<Notice kind="muted" small ondismiss={() => (warning = '')}>Warning: {warning}</Notice>{/if}

{#if install}
  <section class="card plate form sheet" use:useEscape={() => (install = null)}>
    <header>
      <h3>{install.name ? `Install ${install.name}` : 'Install a stack'}</h3>
      <button class="quiet close" aria-label="Close" onclick={() => (install = null)}><Icon name="close" /></button>
    </header>
    <div class="sheet-body">
      <div class="col">
        <label>Name <input bind:value={install.name} placeholder="lowercase, digits, - _" /></label>
        <label>compose.yml <textarea rows="12" bind:value={install.compose} spellcheck="false" placeholder="services:&#10;  app:&#10;    image: …"></textarea></label>
        <label><span>.env <span class="opt">optional</span></span> <textarea rows="3" bind:value={install.env} spellcheck="false"></textarea></label>
        {#if install.files?.length || install.setupCommands?.length}
          <details class="setup-summary">
            <summary class="muted small">From the catalog: {install.files?.length ?? 0} setup file{install.files?.length === 1 ? '' : 's'}, {install.setupCommands?.length ?? 0} setup command{install.setupCommands?.length === 1 ? '' : 's'}</summary>
            {#each install.files ?? [] as f}<p class="mono small">{f.path}</p>{/each}
            {#each install.setupCommands ?? [] as c}<p class="mono small">$ {c}</p>{/each}
          </details>
        {/if}
      </div>
      <div class="col">
        <div>
          <h4>Needs</h4>
          <div class="needs">
            <label>Cores <input type="number" min="0" bind:value={install.needs.cores} onchange={place} /></label>
            <label>Memory MB <input type="number" min="0" bind:value={install.needs.memoryMB} onchange={place} /></label>
            <label>Disk GB <input type="number" min="0" bind:value={install.needs.diskGB} onchange={place} /></label>
            <label class="check"><input type="checkbox" bind:checked={install.needs.gpu} onchange={place} /> Needs a GPU</label>
          </div>
        </div>
        <div>
          <h4>Where</h4>
          {#if install.verdicts}
            <div class="verdicts" role="radiogroup" aria-label="Where">
              {#each install.verdicts as v}
                <label class="verdict" class:no={!v.fits} class:on={install.host === v.host}>
                  <input type="radio" name="host" value={v.host} bind:group={install.host} disabled={!v.fits} />
                  <span class="led {v.fits ? 'ok' : 'bad'}"></span>
                  <span class="vh"><strong class="mono">{v.host}</strong><span class="muted small">{v.why}</span></span>
                </label>
              {/each}
            </div>
          {:else}
            <button onclick={place}>Ask placement</button>
          {/if}
          {#if clashes}<Notice kind="muted" small>{install.name} is already running on {install.host}; this replaces the running stack.</Notice>{/if}
        </div>
      </div>
    </div>
    <footer>
      <button class="quiet" onclick={() => (install = null)}>Cancel</button>
      <button class="primary" onclick={deploy} disabled={busy || !install.host || !install.name || !install.compose}>Deploy on {install.host || '…'}</button>
    </footer>
  </section>
{/if}

{#if !loadError && stacks.length === 0 && !loading}
  <Empty text="No stacks on any remote. Pick one from the Catalog tab, or paste a compose file." action="Install a stack" onaction={() => (install = blank())} />
{:else if loading && stacks.length === 0}
  <div class="card waiting muted">Asking every remote for its stacks…</div>
{:else if !loadError || loading}
  {#if filter && shown.length === 0}<p class="muted">No stack matches “{filter}”.</p>{/if}
  <div class="groups">
    {#each groups as g (g.host)}
      <section class="card group">
        <header>
          <span class="led {g.status ?? ''}"></span>
          <strong class="mono">{g.host}</strong>
          <span class="muted small">{g.list.length} stack{g.list.length === 1 ? '' : 's'}</span>
        </header>
        {#each g.list as s (s.host + '/' + s.name)}
          {@const h = health(s)}
          {@const key = `${s.host}/${s.name}`}
          <div class="app" class:open={open === key}>
            <div class="who">
              <span class="led {h.led}"></span>
              <div>
                <strong class="name">{s.name}</strong>
                <span class="muted small">{s.containers.length} container{s.containers.length === 1 ? '' : 's'}{#if !s.managed}{' · deployed by hand'}{/if}</span>
              </div>
            </div>
            <ul class="containers">
              {#each s.containers as c}
                <li title="{c.name} — {c.image} ({c.state})">
                  <span class="dot" class:up={c.state === 'running'}></span>
                  <span class="cn">{c.name}</span>
                  {#each ports(c) as p}<span class="port">:{p}</span>{/each}
                </li>
              {/each}
            </ul>
            <span class="pill state {h.pill}">{h.text}</span>
            <div class="acts">
              {#if upCount(s) === s.containers.length && s.containers.length}
                <button class="small" disabled={busy} onclick={() => action(s, 'stop')}><Icon name="stop" size={13} /> Stop</button>
              {:else}
                <button class="small" disabled={busy} onclick={() => action(s, 'start')}><Icon name="play" size={13} /> Start</button>
              {/if}
              <button class="small quiet" onclick={() => toggle(s)} aria-expanded={open === key}>{open === key ? 'Close' : 'Details'} <span class="chev" class:open={open === key}><Icon name="chevron" size={13} /></span></button>
              <Menu label="More actions for {s.name}" disabled={busy}>
                <button disabled={busy} onclick={() => action(s, 'restart')}><Icon name="restart" size={15} /> Restart</button>
                {#if upCount(s) > 0 && upCount(s) < s.containers.length}<button disabled={busy} onclick={() => action(s, 'stop')}><Icon name="stop" size={15} /> Stop</button>{/if}
                <button disabled={busy} onclick={() => action(s, 'pull')}><Icon name="pull" size={15} /> Pull images</button>
                <button disabled={busy} onclick={() => (publish = { host: s.host, name: s.name, port: firstPort(s) })}><Icon name="publish" size={15} /> Publish a port…</button>
                <hr />
                <button class="danger" disabled={busy} onclick={() => action(s, 'remove', false)}><Icon name="trash" size={15} /> Remove</button>
                <button class="danger" disabled={busy} onclick={() => action(s, 'remove', true)}><Icon name="trash" size={15} /> Remove with volumes</button>
              </Menu>
            </div>
          </div>
          {#if publish && publish.host === s.host && publish.name === s.name}
            <div class="inset"><Publish host={publish.host} name={publish.name} port={publish.port} onclose={() => (publish = null)} /></div>
          {/if}
          {#if open === key}
            <div class="inset detail">
              <div class="scroll">
                <table class="ctab">
                  <thead><tr><th>Container</th><th>Image</th><th>State</th><th>Ports</th></tr></thead>
                  <tbody>
                    {#each s.containers as c}
                      <tr><td class="mono">{c.name}</td><td class="mono muted" title={c.image}>{image(c.image)}</td><td><span class="dot" class:up={c.state === 'running'}></span> {c.state}</td><td class="mono">{ports(c).join(', ') || '—'}</td></tr>
                    {/each}
                  </tbody>
                </table>
              </div>
              <div class="panes" class:single={!detail.file}>
                {#if detail.file}
                  <div class="pane">
                    <h4>compose.yml</h4>
                    <textarea rows="12" bind:value={detail.file.compose} spellcheck="false"></textarea>
                    <div class="row"><button class="small primary" disabled={busy} onclick={() => updateFile(s)}>Update stack</button></div>
                  </div>
                {:else if !s.managed}
                  <p class="muted small">Deployed by hand outside ~/stacks, so its compose file isn't editable here.</p>
                {/if}
                <div class="pane">
                  <h4>Logs <span class="muted small">last 100 lines</span></h4>
                  <pre class="logs">{detail.logs || '…'}</pre>
                </div>
              </div>
            </div>
          {/if}
        {/each}
      </section>
    {/each}
  </div>
{/if}

<style>
  /* Toolbar: actions left, the count and the filter right */
  .summary { margin-left: auto; }
  .summary strong { color: var(--fg); font-family: var(--mono); font-weight: 500; }
  .search { position: relative; display: flex; align-items: center; color: var(--muted); }
  .search :global(svg) { position: absolute; left: 0.6rem; pointer-events: none; }
  .search input { padding-left: 1.9rem; width: 13rem; background: var(--card); }

  .waiting { padding: 2rem; text-align: center; }

  /* One card per remote, a row per stack */
  .groups { display: grid; gap: 1rem; }
  .group { padding: 0; overflow: visible; }
  .group > header {
    display: flex; align-items: center; gap: 0.55rem; padding: 0.6rem 1rem;
    border-bottom: 1px solid var(--line); border-radius: var(--r-lg) var(--r-lg) 0 0;
    background: color-mix(in srgb, var(--sunk) 50%, var(--card));
  }
  .group > header strong { font-size: 0.95rem; }
  .app {
    display: grid; grid-template-columns: minmax(11rem, 14rem) 1fr auto auto; gap: 0.5rem 1.25rem;
    align-items: center; padding: 0.75rem 1rem; border-bottom: 1px solid var(--line);
  }
  .group > :last-child { border-bottom: 0; }
  .app:hover { background: color-mix(in srgb, var(--fg) 2.5%, transparent); }
  .app.open { background: color-mix(in srgb, var(--accent) 5%, transparent); }
  .who { display: flex; gap: 0.6rem; align-items: center; min-width: 0; }
  .who > div { display: grid; min-width: 0; }
  .name { font-size: 0.98rem; overflow-wrap: anywhere; }
  .containers { list-style: none; margin: 0; padding: 0; display: flex; flex-wrap: wrap; gap: 0.35rem; min-width: 0; }
  .containers li {
    display: inline-flex; align-items: center; gap: 0.35rem; max-width: 100%;
    font-family: var(--mono); font-size: 0.76em; padding: 0.16rem 0.5rem; border-radius: 999px;
    background: var(--sunk); border: 1px solid var(--line);
  }
  .cn { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .port { color: var(--accent); font-weight: 500; }
  .dot { display: inline-block; width: 0.4rem; height: 0.4rem; border-radius: 50%; background: var(--muted); flex: none; }
  .dot.up { background: var(--ok); }
  .state { justify-self: end; white-space: nowrap; }
  .acts { display: flex; align-items: center; gap: 0.3rem; justify-content: flex-end; }
  .acts button { display: inline-flex; align-items: center; gap: 0.3rem; }

  /* What opens under a row: the containers, the file, the logs */
  .inset { padding: 0.25rem 1rem 1rem; border-bottom: 1px solid var(--line); }
  .detail { display: grid; gap: 1rem; padding-top: 0.75rem; background: color-mix(in srgb, var(--accent) 5%, transparent); }
  .ctab { box-shadow: 0 0 0 1px var(--line); font-size: 0.85em; }
  .ctab th, .ctab td { padding: 0.4rem 0.75rem; }
  .ctab th { background: var(--sunk); }
  .panes { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
  .panes.single { grid-template-columns: 1fr; }
  .pane { display: grid; gap: 0.5rem; align-content: start; min-width: 0; }
  .pane h4 { margin: 0; }
  .logs { background: #111418; color: #d7dae0; max-height: 18rem; min-height: 8rem; font-size: 0.78em; border: 1px solid var(--line); }

  /* The install sheet: the stack left, where it goes right */
  .card.sheet { max-width: 64rem; margin-bottom: 1.25rem; }
  .sheet > header { justify-content: space-between; }
  .sheet h3 { margin: 0; font-size: 1rem; }
  .close { padding: 0.3rem; }
  .sheet-body { display: grid; grid-template-columns: 1.4fr 1fr; gap: 1.25rem 1.75rem; padding: 1.1rem 1.25rem; }
  .col { display: grid; gap: 0.9rem; align-content: start; min-width: 0; }
  .col h4 { margin: 0 0 0.5rem; font-size: 0.72rem; text-transform: uppercase; letter-spacing: 0.06em; }
  .opt { font-size: 0.9em; opacity: 0.7; }
  .needs { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0.6rem; align-items: end; }
  .needs .check { grid-column: 1 / -1; }
  .verdicts { display: grid; gap: 0.4rem; margin-bottom: 0.6rem; }
  .verdict {
    display: flex !important; align-items: center; gap: 0.6rem; padding: 0.55rem 0.7rem; cursor: pointer;
    border: 1px solid var(--line); border-radius: var(--r); background: var(--card); color: var(--fg) !important;
  }
  .verdict:hover:not(.no) { border-color: color-mix(in srgb, var(--fg) 25%, var(--line)); }
  .verdict.on { border-color: var(--accent); box-shadow: inset 0 0 0 1px var(--accent); background: color-mix(in srgb, var(--accent) 6%, var(--card)); }
  .verdict.no { opacity: 0.6; cursor: not-allowed; }
  .vh { display: grid; min-width: 0; }
  .sheet > footer { justify-content: flex-end; background: color-mix(in srgb, var(--sunk) 35%, var(--card)); }
  .setup-summary { font-size: 0.9em; }
  .setup-summary p { margin: 0.2rem 0 0 1rem; }

  @media (max-width: 1000px) {
    .app { grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: 'who state' 'containers containers' 'acts acts'; }
    .who { grid-area: who; } .containers { grid-area: containers; } .state { grid-area: state; } .acts { grid-area: acts; justify-content: flex-start; }
    .panes { grid-template-columns: 1fr; }
  }
  @media (max-width: 760px) {
    .sheet-body { grid-template-columns: 1fr; }
    .summary { margin-left: 0; }
    .search, .search input { width: 100%; }
    .needs { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
</style>
