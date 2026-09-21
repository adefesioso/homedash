<script>
  import Notice from './ui/Notice.svelte';
  import { post } from './lib/api.js';
  import Icon from './Icon.svelte';
  import Restore from './Restore.svelte';

  // Sign-in. Passkeys only: the first one registered is the admin; after
  // that, registration takes a single-use code from an admin or from a
  // shell on the hub.
  let { auth, onsignedin } = $props();
  // 'restore' is the third door, open only while there is no account:
  // an export from an earlier hub brings its accounts with it.
  let mode = $state(auth.setupOpen ? 'register' : 'login');
  let name = $state('');
  let invite = $state('');
  let error = $state('');
  let busy = $state(false);

  const supported = typeof PublicKeyCredential !== 'undefined' && !!PublicKeyCredential.parseCreationOptionsFromJSON;

  async function register() {
    busy = true; error = '';
    try {
      const { ceremony, options } = await post('/auth/register/begin', { name, invite });
      const cred = await navigator.credentials.create({ publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(options.publicKey) });
      const r = await fetch(`/api/auth/register/finish?ceremony=${ceremony}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(cred.toJSON()) });
      if (!r.ok) throw new Error(await r.text());
      onsignedin(await r.json());
    } catch (e) { error = e.message; }
    busy = false;
  }
  async function login() {
    busy = true; error = '';
    try {
      const { ceremony, options } = await post('/auth/login/begin');
      const cred = await navigator.credentials.get({ publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(options.publicKey) });
      const r = await fetch(`/api/auth/login/finish?ceremony=${ceremony}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(cred.toJSON()) });
      if (!r.ok) throw new Error(await r.text());
      onsignedin(await r.json());
    } catch (e) { error = e.message; }
    busy = false;
  }
</script>

<div class="wrap">
  <section class="card form">
    <div class="brand"><span class="mark"><Icon name="mark" size={20} /></span><h1>HomeDash</h1></div>
    {#if !supported}
      <Notice>This browser cannot do passkeys. Open the panel in a current Chromium or Firefox on <code>localhost</code>.</Notice>
    {:else if mode === 'register'}
      {#if auth.setupOpen}
        <p>No account yet. The first passkey registered becomes the admin.</p>
      {:else}
        <p class="muted">Paste the one-time code an admin gave you, or the one <code>homedash recover</code> printed on the hub.</p>
        <label>Code <input bind:value={invite} autocomplete="off" /></label>
      {/if}
      <label>Your name <input bind:value={name} autocomplete="username webauthn" /></label>
      <button class="primary" onclick={register} disabled={busy || (!name && !invite)}>Register a passkey</button>
      {#if !auth.setupOpen}<button class="link" onclick={() => (mode = 'login')}>I have a passkey</button>{/if}
      {#if auth.setupOpen}<button class="link" onclick={() => (mode = 'restore')}>Rebuilding? Restore an export</button>{/if}
    {:else if mode === 'restore'}
      <p class="muted">An export made in Settings on the hub this one replaces. Its accounts, hosts and vault come back with it, and the hub restarts.</p>
      <Restore onerror={(m) => (error = m)} />
      <button class="link" onclick={() => (mode = 'register')}>Start fresh instead</button>
    {:else}
      <p class="muted">Your passkey is the key to the house.</p>
      <button class="primary" onclick={login} disabled={busy}>Sign in</button>
      <button class="link" onclick={() => (mode = 'register')}>I have a registration code</button>
    {/if}
    {#if error}<Notice>{error}</Notice>{/if}
  </section>
</div>

<style>
  .wrap { min-height: 100vh; display: grid; place-items: center; padding: 1rem; }
  .card { width: min(100%, 24rem); max-width: none; margin: 0; gap: 0.8rem; padding: 1.75rem 1.75rem 1.5rem; }
  .brand { margin-bottom: 0.5rem; }
  .mark { width: 2rem; height: 2rem; border-radius: 8px; }
  h1 { margin: 0; font-size: 1.25rem; letter-spacing: -0.02em; }
  button.primary { padding: 0.6rem 0.9rem; font-size: 0.95em; }
  button.link { text-align: left; font-size: 0.85em; }
  p { margin: 0; }
</style>
