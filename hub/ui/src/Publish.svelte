<script>
  import Notice from './ui/Notice.svelte';
  import { get, post } from './lib/api.js';

  // Publish one port on one machine this hub owns to the peers named.
  // Used from a stack's row on the Apps tab and from a host card; the
  // service itself lives on the Peers tab afterwards.
  let { host, port = '', name = '', onclose } = $props();
  // svelte-ignore state_referenced_locally
  let form = $state({ name, port, peers: [] });
  let peers = $state([]);
  let error = $state('');
  let busy = $state(false);
  // A name already published is a 409, not a silent replace (H-12): show
  // it and offer the replace the error names, rather than resubmitting
  // blind.
  let conflict = $state(false);

  $effect(() => { get('/peers').then((d) => (peers = d.peers)).catch((e) => (error = e.message)); });

  async function publish(replace = false) {
    busy = true;
    try {
      await post('/services', { name: form.name, host, port: +form.port, peers: form.peers, replace });
      onclose(true);
    } catch (e) {
      error = e.message;
      conflict = !replace && /already published/.test(e.message);
    }
    busy = false;
  }
  const short = (id) => '…' + id.slice(-8);
</script>

<section class="card form">
  <h4>Publish a port on {host}</h4>
  <p class="help">One port on one machine you own, offered to the peers you name. Nothing changes on the machine; the hub copies to the port over the SSH connection it already holds.</p>
  <div class="row fields">
    <label>Service name <input bind:value={form.name} placeholder="wiki, photos" /></label>
    <label>Port <input type="number" min="1" max="65535" bind:value={form.port} /></label>
  </div>
  <fieldset>
    <legend>Peers that may front it</legend>
    {#if peers.length === 0}<p class="muted">No peers discovered yet. <a href="#settings">Join a space in Settings</a> first.</p>{/if}
    {#each peers as p (p.id)}
      <label class="check"><input type="checkbox" value={p.id} bind:group={form.peers} /> {p.name || short(p.id)} <code class="small muted">{short(p.id)}</code></label>
    {/each}
  </fieldset>
  <div class="row">
    <button class="primary" onclick={() => publish(false)} disabled={busy || !form.name || !form.port || form.peers.length === 0}>Publish</button>
    {#if conflict}<button class="danger" onclick={() => publish(true)} disabled={busy}>Replace it</button>{/if}
    <button class="quiet" onclick={() => onclose(false)}>Cancel</button>
    {#if error}<Notice>{error}</Notice>{/if}
  </div>
</section>

<style>
  .card { margin: 0.5rem 0 1rem; }
  h4 { margin: 0; color: var(--fg); font-size: 0.95em; }
  .fields label:first-child { min-width: 14rem; }
  p { margin: 0; }
</style>
