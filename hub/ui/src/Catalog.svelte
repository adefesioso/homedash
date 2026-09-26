<script>
  import Notice from './ui/Notice.svelte';
  import { get, post, del } from './lib/api.js';
  import { useEscape } from './lib/actions.js';
  import Icon from './Icon.svelte';
  import Empty from './Empty.svelte';
  import Stat from './ui/Stat.svelte';

  // The Catalog tab: saved compose files with the volumes they want and a
  // rough requirements line, so installing the same thing twice never
  // means writing the YAML twice. Installing hands the entry to the Apps
  // tab, which already knows placement and deploy.
  let { oninstall } = $props();
  let catalog = $state([]);
  let loadError = $state('');
  let error = $state('');
  let addEntry = $state(null); // {..., editing} — editing locks the name, so a save replaces that entry
  let loaded = $state(false);
  let filter = $state('');
  const blank = () => ({ name: '', title: '', description: '', compose: '', volumes: [], files: [], envTemplate: '', setupCommands: '', needs: { cores: 1, memoryMB: 256, diskGB: 1, gpu: false }, editing: false });
  const shown = $derived(filter.trim()
    ? catalog.filter((e) => `${e.name} ${e.title} ${e.description}`.toLowerCase().includes(filter.trim().toLowerCase()))
    : catalog);
  // A tile colour per entry, stable across loads: a hue from the name.
  const hue = (name) => [...name].reduce((h, c) => (h * 31 + c.charCodeAt(0)) % 360, 7);
  const memory = (mb) => (mb >= 1024 ? `${+(mb / 1024).toFixed(1)} GB` : `${mb} MB`);

  async function load() {
    try { catalog = await get('/apps/catalog'); loadError = ''; }
    catch (e) { loadError = e.message; }
    loaded = true;
  }
  load();

  async function saveEntry() {
    const { editing, ...e } = addEntry;
    const setupCommands = (e.setupCommands || '').split('\n').map((s) => s.trim()).filter(Boolean);
    const files = (e.files ?? []).filter((f) => f.path.trim());
    try {
      await post('/apps/catalog', { ...e, needs: { cores: +e.needs.cores, memoryMB: +e.needs.memoryMB, diskGB: +e.needs.diskGB, gpu: !!e.needs.gpu }, volumes: e.volumes ?? [], files, setupCommands });
      addEntry = null; error = ''; await load();
    }
    catch (e) { error = e.message; }
  }
  async function deleteEntry(e) {
    if (!confirm(`Delete ${e.title || e.name} from the catalog? Stacks already installed from it keep running.`)) return;
    const name = e.name;
    try { await del(`/apps/catalog/${name}`); error = ''; await load(); } catch (e) { error = e.message; }
  }
  function editEntry(e) {
    const c = structuredClone($state.snapshot(e));
    addEntry = { ...c, files: c.files ?? [], envTemplate: c.envTemplate ?? '', setupCommands: (c.setupCommands ?? []).join('\n'), editing: true };
  }
  function addFile() {
    addEntry.files = [...(addEntry.files ?? []), { path: '', content: '' }];
  }
  function removeFile(i) {
    addEntry.files = addEntry.files.filter((_, j) => j !== i);
  }
</script>

{#if loadError}<Notice>{loadError}</Notice>{/if}
{#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}

{#if catalog.length}
  <div class="bar">
    <button class="primary" onclick={() => (addEntry = blank())}><Icon name="plus" size={14} /> Add your own</button>
    <span class="summary muted small"><strong>{catalog.length}</strong> entr{catalog.length === 1 ? 'y' : 'ies'}</span>
    <label class="search"><Icon name="search" size={14} /><input type="search" placeholder="Search the catalog" aria-label="Search the catalog" bind:value={filter} /></label>
  </div>
{/if}

{#if addEntry}
  <section class="card plate form sheet" use:useEscape={() => (addEntry = null)}>
    <header>
      <h3>{addEntry.editing ? `Edit ${addEntry.name}` : 'Add your own'}</h3>
      <button class="quiet close" aria-label="Close" onclick={() => (addEntry = null)}><Icon name="close" /></button>
    </header>
    <div class="section">
      <div class="lead"><h4>About</h4><p class="hint">The name is how the stack is known on a remote; the title and description are what this page shows.</p></div>
      <div class="fields-grid">
        <label>Name <input bind:value={addEntry.name} readonly={addEntry.editing} placeholder="lowercase, digits, - _" /></label>
        <label>Title <input bind:value={addEntry.title} placeholder="Jellyfin" /></label>
        <label class="full">Description <input bind:value={addEntry.description} placeholder="What it is, in one line" /></label>
      </div>
    </div>
    <div class="section">
      <div class="lead"><h4>Needs</h4><p class="hint">What placement looks for when choosing a remote.</p></div>
      <div class="fields-grid needs">
        <label>Cores <input type="number" min="0" bind:value={addEntry.needs.cores} /></label>
        <label>Memory MB <input type="number" min="0" bind:value={addEntry.needs.memoryMB} /></label>
        <label>Disk GB <input type="number" min="0" bind:value={addEntry.needs.diskGB} /></label>
        <label class="check full"><input type="checkbox" bind:checked={addEntry.needs.gpu} /> Needs a GPU</label>
      </div>
    </div>
    <div class="section">
      <div class="lead"><h4>Compose file</h4></div>
      <label>compose.yml <textarea rows="12" bind:value={addEntry.compose} spellcheck="false" placeholder="services:&#10;  app:&#10;    image: …"></textarea></label>
    </div>
    <div class="section">
      <div class="lead"><h4>Setup files <span class="opt">optional</span></h4><p class="hint">Plain-text files the compose's bind mounts expect to exist — written under the stack directory before it comes up.</p></div>
      <div class="files">
        {#each addEntry.files ?? [] as f, i}
          <div class="setup-file">
            <div class="file-head">
              <input class="mono" placeholder="path, e.g. nginx/nginx.conf" aria-label="File path" bind:value={f.path} />
              <button class="small danger quiet" onclick={() => removeFile(i)}>Remove</button>
            </div>
            <textarea rows="4" placeholder="file content" aria-label="File content" bind:value={f.content} spellcheck="false"></textarea>
          </div>
        {/each}
        <div><button class="small" onclick={addFile}><Icon name="plus" size={13} /> Add file</button></div>
      </div>
    </div>
    <div class="section">
      <div class="lead"><h4>Setup commands <span class="opt">optional</span></h4><p class="hint">One shell command per line, run in the stack directory before it comes up (e.g. <code>mkdir -p data</code>). Gated the same as any other command run on a host.</p></div>
      <label>commands <textarea rows="4" bind:value={addEntry.setupCommands} spellcheck="false"></textarea></label>
    </div>
    <div class="section">
      <div class="lead"><h4>.env template <span class="opt">optional</span></h4><p class="hint">Carried into the install form instead of starting blank.</p></div>
      <label>.env <textarea rows="4" bind:value={addEntry.envTemplate} spellcheck="false"></textarea></label>
    </div>
    <footer>
      <button class="quiet" onclick={() => (addEntry = null)}>Cancel</button>
      <button class="primary" onclick={saveEntry} disabled={!addEntry.name || !addEntry.compose}>{addEntry.editing ? 'Save changes' : 'Save to catalog'}</button>
    </footer>
  </section>
{/if}

{#if loaded && !loadError && catalog.length === 0 && !addEntry}
  <Empty text="The catalog is empty. Save a compose file once and install it on any remote in one click." action="Add your own" onaction={() => (addEntry = blank())} />
{:else}
  {#if filter && shown.length === 0}<p class="muted">Nothing in the catalog matches “{filter}”.</p>{/if}
  <div class="catalog">
    {#each shown as e (e.name)}
      {@const extras = (e.files?.length ?? 0) + (e.setupCommands?.length ?? 0)}
      <section class="card plate entry">
        <div class="top">
          <span class="tile" style:--h={hue(e.name)} aria-hidden="true">{(e.title || e.name).charAt(0).toUpperCase()}</span>
          <div class="ident">
            <h3>{e.title || e.name}</h3>
            <span class="mono muted small">{e.name}</span>
          </div>
          {#if e.needs.gpu}<span class="pill accent">GPU</span>{/if}
        </div>
        {#if e.description}<p class="desc">{e.description}</p>{/if}
        <div class="specs">
          <Stat label="cores">{e.needs.cores}</Stat>
          <Stat label="memory">{memory(e.needs.memoryMB)}</Stat>
          <Stat label="disk">{e.needs.diskGB} GB</Stat>
        </div>
        <footer>
          <button class="small primary" onclick={() => oninstall?.(e)}>Install</button>
          {#if extras}<span class="muted small extras" title="Setup files and commands run before the stack comes up">+{extras} setup step{extras === 1 ? '' : 's'}</span>{/if}
          <span class="grow"></span>
          <button class="small quiet icon" aria-label="Edit {e.title || e.name}" title="Edit" onclick={() => editEntry(e)}><Icon name="edit" size={15} /></button>
          <button class="small quiet danger icon" aria-label="Delete {e.title || e.name}" title="Delete" onclick={() => deleteEntry(e)}><Icon name="trash" size={15} /></button>
        </footer>
      </section>
    {/each}
  </div>
{/if}

<style>
  .summary { margin-left: auto; }
  .summary strong { color: var(--fg); font-family: var(--mono); font-weight: 500; }
  .search { position: relative; display: flex; align-items: center; color: var(--muted); }
  .search :global(svg) { position: absolute; left: 0.6rem; pointer-events: none; }
  .search input { padding-left: 1.9rem; width: 15rem; background: var(--card); }

  /* The cards: a tile and a name, a line of what it is, what it needs, and Install */
  .catalog { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 19rem), 1fr)); gap: 1rem; }
  .entry { margin: 0; display: flex; flex-direction: column; transition: box-shadow 0.15s, transform 0.15s; }
  .entry:hover { box-shadow: 0 6px 18px rgba(0, 0, 0, 0.08), 0 0 0 1px color-mix(in srgb, var(--fg) 18%, var(--line)); }
  .top { display: flex; align-items: center; gap: 0.75rem; padding: 1rem 1rem 0; }
  .tile {
    display: grid; place-items: center; flex: none; width: 2.5rem; height: 2.5rem; border-radius: 10px;
    font-weight: 700; font-size: 1.1rem;
    background: oklch(0.93 0.045 var(--h)); color: oklch(0.42 0.11 var(--h));
    box-shadow: inset 0 0 0 1px oklch(0.85 0.05 var(--h));
  }
  .ident { display: grid; min-width: 0; flex: 1; }
  .ident h3 { margin: 0; font-size: 1rem; overflow-wrap: anywhere; }
  .desc {
    margin: 0.7rem 1rem 0.9rem; color: color-mix(in srgb, var(--fg) 78%, var(--muted)); font-size: 0.92em;
    display: -webkit-box; -webkit-line-clamp: 3; line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
  }
  .specs {
    display: grid; grid-template-columns: repeat(3, 1fr); margin: auto 1rem 1rem;
    border: 1px solid var(--line); border-radius: var(--r); background: color-mix(in srgb, var(--sunk) 45%, var(--card));
  }
  .top:has(+ .specs) { padding-bottom: 0.9rem; }
  .specs :global(.stat) { padding: 0.45rem 0.7rem; }
  .specs :global(.stat + .stat) { border-left: 1px solid var(--line); }
  .entry > footer { gap: 0.4rem; padding: 0.6rem 0.75rem 0.6rem 1rem; }
  .extras { white-space: nowrap; }
  button.icon { padding: 0.3rem 0.4rem; display: inline-flex; }

  /* The add/edit sheet: a label column beside each section's fields */
  .card.sheet { max-width: 60rem; margin-bottom: 1.25rem; gap: 0; }
  .sheet > header { justify-content: space-between; }
  .sheet h3 { margin: 0; font-size: 1rem; }
  .close { padding: 0.3rem; }
  .section { display: grid; grid-template-columns: 14rem 1fr; gap: 0.5rem 2rem; padding: 1.1rem 1.25rem; border-bottom: 1px solid var(--line); }
  .section > label { align-self: start; }
  .lead h4 { margin: 0 0 0.25rem; color: var(--fg); font-size: 0.88rem; }
  .opt { font-weight: 400; color: var(--muted); font-size: 0.85em; margin-left: 0.2rem; }
  .hint { margin: 0; font-size: 0.82em; color: var(--muted); line-height: 1.45; }
  .fields-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0.7rem; align-items: end; }
  .fields-grid.needs { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .fields-grid .full { grid-column: 1 / -1; }
  .files { display: grid; gap: 0.6rem; align-content: start; }
  .setup-file { display: grid; gap: 0; border: 1px solid var(--line); border-radius: var(--r); overflow: hidden; }
  .file-head { display: flex; gap: 0.4rem; align-items: center; padding: 0.35rem; background: color-mix(in srgb, var(--sunk) 50%, var(--card)); border-bottom: 1px solid var(--line); }
  .file-head input { flex: 1; background: var(--card); font-size: 0.85em; }
  .setup-file textarea { border: 0; border-radius: 0; }
  .sheet > footer { justify-content: flex-end; border-top: 0; background: color-mix(in srgb, var(--sunk) 35%, var(--card)); }

  @media (prefers-color-scheme: dark) {
    .tile { background: oklch(0.34 0.06 var(--h)); color: oklch(0.88 0.08 var(--h)); box-shadow: inset 0 0 0 1px oklch(0.42 0.06 var(--h)); }
    .entry:hover { box-shadow: 0 6px 18px rgba(0, 0, 0, 0.35), 0 0 0 1px color-mix(in srgb, var(--fg) 18%, var(--line)); }
  }
  @media (max-width: 760px) {
    .section { grid-template-columns: 1fr; }
    .summary { margin-left: 0; }
    .search, .search input { width: 100%; }
    .fields-grid, .fields-grid.needs { grid-template-columns: 1fr 1fr; }
  }
</style>
