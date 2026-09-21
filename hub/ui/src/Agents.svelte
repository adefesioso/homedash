<script>
  import { get, post, del } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import Empty from './Empty.svelte';
  import Terminal from './Terminal.svelte';
  import Icon from './Icon.svelte';
  import Notice from './ui/Notice.svelte';

  // The Agents tab: every omp session on the hub, live or finished, and
  // the one currently attached. Sessions stay on the hub, so the list is
  // also the log — a finished one stays until it is deleted. The jobs a
  // session starts live on the Jobs tab.
  let { role } = $props();
  let status = $state(null);
  let sessions = $state([]);
  let open = $state(null);
  // history: the finished session whose kept screen is shown under its
  // row — the History toggle; one at a time.
  let history_ = $state(null);
  let loadError = $state('');
  let error = $state('');
  // The finished sessions fold to the most recent; the rest are a click
  // away. Live ones are never folded.
  let showAll = $state(false);
  const live = $derived(sessions.filter((s) => s.live));
  const history = $derived(sessions.filter((s) => !s.live));
  const shown = $derived(showAll ? sessions : [...live, ...history.slice(0, 1)]);

  async function load() {
    try {
      [status, sessions] = await Promise.all([get('/agents'), get('/agents/sessions')]);
      loadError = '';
    } catch (e) { loadError = String(e.message ?? e); }
  }
  $effect(() => {
    return poll(load, 10_000);
  });

  // The shell takes a moment to spawn; show the (blank) frame the instant
  // the button is clicked rather than leaving the tab looking inert, then
  // swap in the real terminal once the session exists. The hub names the
  // session (a passphrase, bounty-koala); nothing is sent.
  async function openSession() {
    open = 'pending';
    try {
      const s = await post('/agents/sessions', {});
      await load();
      open = s.id;
    } catch (e) {
      error = String(e.message ?? e);
      if (open === 'pending') open = null;
    }
  }
  // close ends the process and keeps the row; remove drops the row too.
  async function closeSession(id) {
    try {
      await post(`/agents/sessions/${id}/close`);
      if (open === id) open = null;
      error = '';
      await load();
    } catch (e) { error = String(e.message ?? e); }
  }
  async function removeSession(s) {
    if (s.live && !confirm(`End and delete ${s.name}? Its jobs stay on the Jobs tab.`)) return;
    try {
      await del(`/agents/sessions/${s.id}`);
      if (open === s.id) open = null;
      if (history_ === s.id) history_ = null;
      error = '';
      await load();
    } catch (e) { error = String(e.message ?? e); }
  }
  async function clearHistory() {
    if (!confirm(`Delete all ${history.length} finished sessions? Live ones stay, and so do their jobs.`)) return;
    try { await del('/agents/sessions'); showAll = false; history_ = null; error = ''; await load(); }
    catch (e) { error = String(e.message ?? e); }
  }
  const when = (s) => new Date(s).toLocaleString();
</script>

{#if loadError}<Notice>{loadError}</Notice>{/if}
{#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}

{#if status && !status.ready}
  <p class="notice dashed">
    Fetching <code>omp {status.ompVersion}</code> into the hub's state directory…
    {#if status.installError}<br /><span class="error">{status.installError}</span> — retrying every minute.{/if}
  </p>
{/if}

<div class="bar">
  <button class="primary" onclick={openSession} disabled={!status?.ready || open === 'pending'}><Icon name="plus" size={14} /> New session</button>
  <span class="grow"></span>
  {#if status?.ready}<span class="muted small mono">omp {status.ompVersion} · <span class="led {status.vaultRunning ? 'ok' : 'bad'}"></span> vault</span>{/if}
</div>

{#if open}
  {@const s = open === 'pending' ? null : sessions.find((x) => x.id === open)}
  <section class="card open">
    <header>
      <span class="led busy"></span>
      <strong class="sname">{s?.name ?? (open === 'pending' ? 'opening…' : `Session ${open}`)}</strong>
      <span class="grow"></span>
      <button class="small quiet" onclick={() => (open = null)}>Detach</button>
      <button class="small danger quiet" onclick={() => closeSession(open)} disabled={open === 'pending'}>Close session</button>
    </header>
    {#if open === 'pending'}
      <div class="term-placeholder"></div>
      <p class="status">opening…</p>
    {:else}
      {#key open}<Terminal id={open} {role} onended={load} />{/key}
    {/if}
  </section>
{/if}

{#if !loadError && sessions.length === 0}
  <Empty text="No sessions yet. A session is a seat at the fleet; open one and ask." action={status?.ready ? 'New session' : undefined} onaction={openSession} />
{:else if !loadError}
  <div class="scroll">
  <table class="stack">
    <tbody>
      {#each shown as s (s.id)}
        <tr class:live={s.live}>
          <td><span class="led {s.live ? 'ok' : ''}"></span> <strong class="sname">{s.name}</strong></td>
          <td class="muted">{s.live ? 'live' : `ended ${when(s.ended)}`}</td>
          <td class="muted">last activity {when(s.lastActivity)}</td>
          <td class="actions">
            {#if s.live}
              <button class="small" onclick={() => (open = s.id)} disabled={open === s.id}>Attach</button>
              <button class="small quiet" onclick={() => closeSession(s.id)}>Close</button>
            {:else}
              <button class="small quiet" aria-pressed={history_ === s.id} onclick={() => (history_ = history_ === s.id ? null : s.id)}>{history_ === s.id ? 'Hide history' : 'History'}</button>
            {/if}
            <button class="small danger quiet" onclick={() => removeSession(s)}>Delete</button>
          </td>
        </tr>
        {#if history_ === s.id}
          <tr class="history detail"><td colspan="4">{#key s.id}<Terminal id={s.id} {role} live={false} />{/key}</td></tr>
        {/if}
      {/each}
    </tbody>
  </table>
  </div>
  {#if history.length > 0}
    <div class="fold">
      {#if history.length > 1}
        <button class="small quiet" onclick={() => (showAll = !showAll)}>{showAll ? 'Show less' : `Show all ${history.length} finished`}</button>
      {/if}
      <span class="grow"></span>
      <button class="small danger quiet" onclick={clearHistory}>Clear history</button>
    </div>
  {/if}
{/if}

<style>
  .open { padding: 0.6rem; margin-bottom: 1rem; }
  .open header { display: flex; gap: 0.5rem; align-items: center; margin-bottom: 0.5rem; padding: 0 0.25rem; }
  tr:not(.live) td { opacity: 0.6; }
  tr:not(.live) td.actions { opacity: 1; }
  tr.history td { opacity: 1; padding: 0.4rem 0 0.8rem; }
  .notice { color: var(--muted); padding: 0.75rem 1rem; }
  .sname { word-break: break-word; }
  .term-placeholder { height: min(70dvh, 40rem); background: var(--sunk); border-radius: var(--r); padding: 0.5rem; }
  .status { color: var(--muted); font-size: 0.8em; font-family: var(--mono); margin: 0.4rem 0 0; }
</style>
