<script>
  import Notice from './ui/Notice.svelte';
  import { get } from './lib/api.js';
  import { poll } from './lib/poll.js';
  // The audit log: 200 rows unless there's more to page through — the
  // "Older" button asks for what came before the oldest row on screen,
  // never skipping or repeating a row as new ones land (E-1).
  const PAGE = 200;
  let events = $state([]);
  let error = $state('');
  let hasOlder = $state(true);
  let loadingOlder = $state(false);
  // Once paged past the first screen, the 10 s poll stops replacing the
  // list — refreshing it out from under someone reading further back
  // would be worse than a page that's a few seconds stale.
  let paged = $state(false);

  async function load() {
    if (paged) return;
    try { events = await get(`/events?limit=${PAGE}`); hasOlder = events.length === PAGE; error = ''; }
    catch (e) { error = e.message; }
  }
  async function older() {
    if (events.length === 0) return;
    loadingOlder = true;
    try {
      const before = events[events.length - 1].id;
      const more = await get(`/events?before=${before}&limit=${PAGE}`);
      events = [...events, ...more];
      hasOlder = more.length === PAGE;
      paged = true;
      error = '';
    } catch (e) { error = e.message; }
    loadingOlder = false;
  }
  $effect(() => {
    return poll(load, 10_000);
  });
</script>

{#if error}
  <Notice>{error}</Notice>
{:else if events.length === 0}
  <p class="muted">Nothing has happened yet.</p>
{:else}
  <p class="muted small">showing the newest {events.length}</p>
  <div class="scroll">
  <table class="stack">
    <tbody>
      {#each events as e (e.id)}
        <tr>
          <td class="when">{new Date(e.at).toLocaleString()}</td>
          <td><span class="pill">{e.kind}</span></td>
          <td class="mono">{e.subject}</td>
          <td class="message">{e.message}</td>
        </tr>
      {/each}
    </tbody>
  </table>
  </div>
  {#if hasOlder}<button class="quiet" onclick={older} disabled={loadingOlder}>{loadingOlder ? 'Loading…' : 'Older'}</button>{/if}
{/if}

<style>
  table { table-layout: fixed; }
  .when { color: var(--muted); white-space: nowrap; font-family: var(--mono); font-size: 0.82em; width: 11rem; }
  .pill { font-family: var(--mono); margin-left: 0.5rem; }
  .message { max-width: 30rem; overflow-wrap: break-word; }
  @media (max-width: 560px) { table { table-layout: auto; } .when { width: auto; } .message { max-width: none; } }
</style>
