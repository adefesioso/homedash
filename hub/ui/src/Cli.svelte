<script>
  import Notice from './ui/Notice.svelte';
  import { post } from './lib/api.js';

  // A command line on this computer asked to sign in: the CLI opened the
  // browser here with the loopback port it listens on and a state it
  // minted. The person is already signed in (App shows Auth first), so
  // approving is one click: the hub hands out a one-time code and the
  // browser carries it to the port. Only the code crosses; the session
  // it becomes is minted when the CLI redeems it.
  let { user, params, oncancel } = $props();
  const port = $derived(Number(params.get('port')));
  const nonce = $derived(params.get('state') ?? '');
  const valid = $derived(Number.isInteger(port) && port > 0 && port < 65536 && /^[0-9a-f]{32}$/.test(nonce));
  let error = $state('');
  let busy = $state(false);

  async function approve() {
    busy = true; error = '';
    try {
      const { code } = await post('/auth/cli/grant');
      location.href = `http://127.0.0.1:${port}/?code=${encodeURIComponent(code)}&state=${encodeURIComponent(nonce)}`;
    } catch (e) { error = e.message; busy = false; }
  }
</script>

<section class="card">
  <h2>Sign in the command line</h2>
  {#if !valid}
    <Notice>This page was not opened by <code>homedash login</code>. Run it again in your terminal.</Notice>
  {:else}
    <p>A <code>homedash</code> command line on this computer, listening on port <code>{port}</code>, asks to sign in as <strong>{user.name}</strong> ({user.role}).</p>
    <p class="muted">It gets a session like this panel's, for thirty days or until <code>homedash logout</code>. Approve only if you just ran <code>homedash login</code> here.</p>
    <div class="row">
      <button class="primary" onclick={approve} disabled={busy}>Approve</button>
      <button class="link" onclick={oncancel}>Cancel</button>
    </div>
    {#if error}<Notice>{error}</Notice>{/if}
  {/if}
</section>

<style>
  .card { max-width: 32rem; margin: 2rem 0; display: grid; gap: 0.6rem; }
  h2 { margin: 0; }
  .row { gap: 1rem; }
</style>
