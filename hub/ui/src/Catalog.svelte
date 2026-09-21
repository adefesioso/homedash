<script>
  import Notice from './ui/Notice.svelte';
  import { get, post, del } from './lib/api.js';
  import { useEscape } from './lib/actions.js';
  import Icon from './Icon.svelte';

  // The Catalog tab: saved compose files with the volumes they want and a
  // rough requirements line, so installing the same thing twice never
  // means writing the YAML twice. Installing hands the entry to the Apps
  // tab, which already knows placement and deploy.
  let { oninstall } = $props();
  let catalog = $state([]);
  let loadError = $state('');
  let error = $state('');
  let addEntry = $state(null); // {..., editing} — editing locks the name, so a save replaces that entry

  async function load() {
    try { catalog = await get('/apps/catalog'); loadError = ''; }
    catch (e) { loadError = e.message; }
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
  async function deleteEntry(name) {
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

{#if addEntry}
  <section class="card form wide entry-form" use:useEscape={() => (addEntry = null)}>
    <h3>{addEntry.editing ? `Edit ${addEntry.name}` : 'Add your own'}</h3>
    <div class="row fields identity-fields">
      <label>Name <input bind:value={addEntry.name} readonly={addEntry.editing} /></label>
      <label>Title <input bind:value={addEntry.title} /></label>
    </div>
    <label>Description <input bind:value={addEntry.description} /></label>
    <h4>Needs</h4>
    <div class="row fields needs-fields">
      <label>Cores <input type="number" bind:value={addEntry.needs.cores} /></label>
      <label>Memory, MB <input type="number" bind:value={addEntry.needs.memoryMB} /></label>
      <label>Disk, GB <input type="number" bind:value={addEntry.needs.diskGB} /></label>
      <label class="check"><input type="checkbox" bind:checked={addEntry.needs.gpu} /> GPU</label>
    </div>
    <h4>Compose file</h4>
    <label>compose.yml <textarea rows="10" bind:value={addEntry.compose} spellcheck="false"></textarea></label>
    <h4>Setup files</h4>
    <p class="hint">Plain-text files the compose's bind mounts expect to exist — written under the stack directory before it comes up.</p>
    {#each addEntry.files ?? [] as f, i}
      <div class="setup-file">
        <div class="row">
          <input placeholder="path, e.g. nginx/nginx.conf" bind:value={f.path} />
          <button class="small danger quiet" onclick={() => removeFile(i)}>Remove</button>
        </div>
        <textarea rows="4" placeholder="file content" bind:value={f.content} spellcheck="false"></textarea>
      </div>
    {/each}
    <button class="quiet small" onclick={addFile}><Icon name="plus" /> Add file</button>
    <h4>Setup commands (optional)</h4>
    <p class="hint">One shell command per line, run in the stack directory before it comes up (e.g. <code>mkdir -p data</code>). Gated the same as any other command run on a host.</p>
    <label>commands <textarea rows="4" bind:value={addEntry.setupCommands} spellcheck="false"></textarea></label>
    <h4>.env template (optional)</h4>
    <label>.env <textarea rows="4" bind:value={addEntry.envTemplate} spellcheck="false"></textarea></label>
    <div class="row"><button class="primary" onclick={saveEntry} disabled={!addEntry.name || !addEntry.compose}>Save</button><button class="quiet" onclick={() => (addEntry = null)}>Cancel</button></div>
  </section>
{/if}

<div class="catalog">
  {#each catalog as e (e.name)}
    <section class="card entry">
      <div class="row"><strong>{e.title}</strong></div>
      <p class="muted">{e.description}</p>
      <p class="needs">{e.needs.cores} cores · {e.needs.memoryMB} MB · {e.needs.diskGB} GB{e.needs.gpu ? ' · GPU' : ''}</p>
      <div class="row">
        <button class="small" onclick={() => oninstall?.(e)}>Install</button>
        <button class="small quiet" onclick={() => editEntry(e)}>Edit</button>
        <button class="small danger quiet" onclick={() => deleteEntry(e.name)}>Delete</button>
      </div>
    </section>
  {/each}
  <button class="entry add" onclick={() => (addEntry = { name: '', title: '', description: '', compose: '', volumes: [], files: [], envTemplate: '', setupCommands: '', needs: { cores: 1, memoryMB: 256, diskGB: 1, gpu: false }, editing: false })}><Icon name="plus" /> Add your own</button>
</div>

<style>
  .catalog { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 16rem), 1fr)); gap: 0.75rem; }
  .entry { margin: 0; display: grid; gap: 0.35rem; align-content: start; }
  .entry p { margin: 0; }
  .entry > .row:last-child { margin-top: auto; padding-top: 0.3rem; }
  .needs { font-family: var(--mono); font-size: 0.78em; color: var(--muted); }
  button.add { display: flex; align-items: center; justify-content: center; gap: 0.5rem; border: 1px dashed var(--line); background: none; box-shadow: none; color: var(--muted); min-height: 6rem; border-radius: var(--r-lg); }
  button.add:hover { color: var(--fg); border-color: var(--muted); }

  .entry-form { gap: 0.9rem; padding: 1.25rem 1.5rem; }
  .entry-form h3 { margin: 0; }
  .entry-form h4 { margin: 0.3rem 0 -0.5rem; font-size: 0.78em; font-weight: 600; text-transform: uppercase; letter-spacing: 0.03em; color: var(--muted); }
  .identity-fields label { flex: 1; }
  .needs-fields label { min-width: 6rem; }
  .hint { margin: -0.4rem 0 0; font-size: 0.85em; color: var(--muted); }
  .setup-file { display: grid; gap: 0.3rem; }
  .setup-file .row { align-items: center; }
  .setup-file input { flex: 1; }
</style>
