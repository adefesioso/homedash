<script>
  import { get, post } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import Icon from './Icon.svelte';
  import Agents from './Agents.svelte';
  import Jobs from './Jobs.svelte';
  import Hosts from './Hosts.svelte';
  import Network from './Network.svelte';
  import Models from './Models.svelte';
  import Tasks from './Tasks.svelte';
  import Apps from './Apps.svelte';
  import Catalog from './Catalog.svelte';
  import Storage from './Storage.svelte';
  import Auth from './Auth.svelte';
  import Peers from './Peers.svelte';
  import Empty from './Empty.svelte';
  import Health from './Health.svelte';
  import Events from './Events.svelte';
  import Settings from './Settings.svelte';
  import Cli from './Cli.svelte';

  // The tabs the README names, in its order, grouped the way the README
  // is: the house, the work done in it, the space beyond it, and the
  // hub itself. Each opens on an honest empty state until the feature
  // that fills it exists.
  const groups = [
    { name: 'House', tabs: [
      { id: 'hosts',    label: 'Hosts' },
      { id: 'network',  label: 'Network' },
      { id: 'storage',  label: 'Storage' },
      { id: 'apps',     label: 'Apps' },
      { id: 'catalog',  label: 'Catalog' },
      { id: 'models',   label: 'Models' },
    ] },
    { name: 'Work', tabs: [
      { id: 'agents',   label: 'Agents' },
      { id: 'jobs',     label: 'Jobs' },
      { id: 'tasks',    label: 'Tasks' },
    ] },
    { name: 'Space', tabs: [
      { id: 'peers',    label: 'Peers' },
    ] },
    { name: 'Hub', tabs: [
      { id: 'health',   label: 'Health' },
      { id: 'events',   label: 'Events' },
      { id: 'settings', label: 'Settings' },
    ] },
  ];
  const tabs = groups.flatMap((g) => g.tabs);

  // route: the hash to a tab id. Case and a trailing `/…` or `?…` don't
  // matter for an ordinary tab (`#Hosts`, `#hosts/3` all mean `hosts`),
  // but `cli?port=…&state=…` is kept whole — its own params, not a tab
  // (X-5).
  const route = (hash) => (hash.startsWith('cli?') ? hash : hash.toLowerCase().split(/[?/]/)[0]);
  let current = $state(route(location.hash.slice(1) || 'hosts'));
  let hub = $state(null);
  let auth = $state(null);     // {setupOpen, user}
  // A catalog entry the Catalog tab handed off to Apps to install; set
  // and switched to in the same tick, consumed once Apps mounts.
  let catalogInstall = $state(null);
  // What the rail shows beside each word: a count where one matters, an
  // LED where something needs a look. Read from the same API the tabs
  // read, so the rail never says more than the tab would.
  let signals = $state({});

  // #cli?port=…&state=… is the CLI asking to be signed in; it is a page
  // of its own, reached after sign-in, never a tab.
  const cliParams = $derived(current.startsWith('cli?') ? new URLSearchParams(current.slice(4)) : null);
  const known = $derived(!!cliParams || tabs.some((t) => t.id === current));
  const title = $derived(cliParams ? 'Command line' : tabs.find((t) => t.id === current)?.label ?? 'There is no such page');
  // The browser tab's own title, not just the in-page heading: the hub's
  // name while a real tab is open, same as always, but the empty state
  // used to leave whatever title was already there instead of saying
  // anything (X-5).
  $effect(() => { document.title = hub && known ? `${hub.name} · HomeDash` : `${title} · HomeDash`; });

  $effect(() => { location.hash = current; });
  // The address bar is a way in too: a link into the panel, or the CLI
  // opening #cli?… in a tab that already has it.
  $effect(() => {
    const onHash = () => { const h = route(location.hash.slice(1)); if (h && h !== current) current = h; };
    addEventListener('hashchange', onHash);
    return () => removeEventListener('hashchange', onHash);
  });
  // api.js dispatches this on any 401: the session is dead (deleted user,
  // revoked token) and the panel must not keep showing the old shell —
  // re-read /auth/state so the sign-in card returns (X-1). A role change
  // (Settings) dispatches the same kind of event so the header's role
  // and this session's own permissions stay current (C-5).
  $effect(() => {
    addEventListener('homedash:signed-out', loadAuth);
    addEventListener('homedash:auth-changed', loadAuth);
    addEventListener('homedash:signals-changed', loadSignals);
    return () => {
      removeEventListener('homedash:signed-out', loadAuth);
      removeEventListener('homedash:auth-changed', loadAuth);
      removeEventListener('homedash:signals-changed', loadSignals);
    };
  });
  async function loadAuth() {
    try { auth = await get('/auth/state'); } catch { auth = { setupOpen: false, user: null }; }
    if (auth.user) get('/hub').then((h) => { hub = h; }).catch(() => {});
  }
  loadAuth();
  async function signOut() { await post('/auth/logout'); await loadAuth(); }

  async function loadSignals() {
    // /hosts failing (a tunnel down, the hub mid-restart) is not "zero
    // hosts" — the rail used to say 0, which reads as 8 in the mono font
    // at that size and looks like a real count (X-14). null marks the
    // read as failed so the count can say so instead of guessing zero.
    const [hosts, jobs, tasks, peers, health] = await Promise.all([
      get('/hosts').catch(() => null), get('/jobs').catch(() => []), get('/tasks').catch(() => []), get('/peers').catch(() => null), get('/hub/health').catch(() => null),
    ]);
    signals = {
      hosts: hosts == null ? { count: '—', led: 'bad' }
        : { count: hosts.length, led: hosts.some((h) => h.status === 'offline' || h.status === 'mismatch') ? 'bad' : hosts.length ? 'ok' : '' },
      jobs:   { count: jobs.filter((j) => j.state === 'running').length || '', led: jobs.some((j) => j.state === 'needs_you') ? 'bad' : jobs.some((j) => j.state === 'running') ? 'busy' : '' },
      tasks:  { count: tasks.length || '', led: tasks.some((t) => t.enabled && t.failing) ? 'bad' : '' },
      peers:  { count: (peers?.peers ?? []).filter((p) => p.connected).length || '', led: peers?.status?.enabled ? 'ok' : '' },
      // The hub's own LED is its worst line: red for bad, amber for a
      // line that needs a look, green when every line is fine.
      health: { count: '', led: health == null ? 'bad' : { ok: 'ok', warn: 'busy', bad: 'bad' }[health.state] ?? '' },
    };
  }
  $effect(() => {
    if (!auth?.user) return;
    return poll(loadSignals, 15_000);
  });
</script>

<svelte:body class:viewer={auth?.user?.role != null && auth.user.role !== 'admin'} />

{#if !auth}
  <p class="loading">…</p>
{:else if !auth.user}
  <Auth {auth} onsignedin={loadAuth} />
{:else}
<div class="shell">
  <aside>
    <header>
      <div class="brand">
        <span class="mark"><Icon name="mark" size={18} /></span>
        <span class="wordmark">HomeDash</span>
      </div>
      <h1>{#if hub}<span class="hubname">{hub.name}</span>{/if}</h1>
      <span class="who">{auth.user.name} · {auth.user.role} <button class="link" onclick={signOut}>sign out</button></span>
    </header>
    <nav aria-label="Sections">
      {#each groups as g}
        <div class="group">
          <span class="groupname" aria-hidden="true">{g.name}</span>
          {#each g.tabs as t}
            {@const s = signals[t.id]}
            <button class:active={current === t.id} onclick={() => (current = t.id)}>
              <Icon name={t.id} />
              <span class="label">{t.label}</span>
              {#if s}<span class="signal" aria-hidden="true">{#if s.count !== '' && s.count != null}<span class="count">{s.count}</span>{/if}{#if s.led}<span class="led {s.led}"></span>{/if}</span>{/if}
            </button>
          {/each}
        </div>
      {/each}
    </nav>
    {#if hub}<span class="version" title="hub version">{hub.version}</span>{/if}
  </aside>

  <main>
    <h2 class="title">{title}</h2>
    {#if cliParams}
      <Cli user={auth.user} params={cliParams} oncancel={() => (current = 'hosts')} />
    {:else if current === 'hosts'}
      <Hosts />
    {:else if current === 'network'}
      <Network />
    {:else if current === 'models'}
      <Models />
    {:else if current === 'agents'}
      <Agents role={auth.user.role} />
    {:else if current === 'jobs'}
      <Jobs />
    {:else if current === 'tasks'}
      <Tasks />
    {:else if current === 'apps'}
      <Apps installEntry={catalogInstall} onconsumed={() => (catalogInstall = null)} />
    {:else if current === 'catalog'}
      <Catalog oninstall={(e) => { catalogInstall = e; current = 'apps'; }} />
    {:else if current === 'storage'}
      <Storage />
    {:else if current === 'peers'}
      <Peers />
    {:else if current === 'health'}
      <Health />
    {:else if current === 'events'}
      <Events />
    {:else if current === 'settings'}
      <Settings {hub} user={auth.user} onauth={loadAuth} />
    {:else}
      <Empty text="There is no such page." action="Back to Hosts" onaction={() => (current = 'hosts')} />
    {/if}
  </main>
</div>
{/if}

<style>
  .loading { text-align: center; color: var(--muted); padding: 3rem; }
  .shell { display: grid; grid-template-columns: 14rem 1fr; min-height: 100vh; }
  aside {
    position: sticky; top: 0; height: 100vh; overflow-y: auto;
    display: flex; flex-direction: column; gap: 0.5rem;
    padding: 1rem 0.75rem; border-right: 1px solid var(--line); background: var(--card);
  }
  aside header { display: grid; gap: 0.15rem; padding: 0.25rem 0.5rem 0.75rem; }
  .wordmark { font-weight: 650; letter-spacing: -0.02em; font-size: 1rem; }
  h1 { margin: 0.35rem 0 0; font-size: 1.05rem; font-family: var(--mono); font-weight: 500; letter-spacing: 0; min-height: 1.3em; }
  .hubname { color: var(--fg); word-break: keep-all; }
  .who { color: var(--muted); font-size: 0.8em; }
  .who .link { font-size: 1em; }
  nav { display: flex; flex-direction: column; gap: 0.75rem; flex: 1; }
  .group { display: grid; gap: 1px; }
  .groupname { font-size: 0.7em; font-weight: 600; color: var(--muted); padding: 0 0.6rem 0.3rem; letter-spacing: 0.02em; }
  nav button {
    display: flex; align-items: center; gap: 0.6rem; width: 100%; text-align: left;
    background: none; border: 0; box-shadow: none; padding: 0.45rem 0.6rem; border-radius: var(--r);
    color: var(--muted); font-size: 0.92em; font-weight: 500;
  }
  nav button:hover { color: var(--fg); background: var(--sunk); }
  nav button.active { color: var(--fg); background: var(--sunk); box-shadow: inset 2px 0 0 var(--accent); }
  nav button.active :global(svg) { color: var(--accent); }
  .label { flex: 1; }
  .signal { display: inline-flex; align-items: center; gap: 0.4rem; }
  .count { font-family: var(--mono); font-size: 0.78em; color: var(--muted); }
  .version { color: var(--muted); font-size: 0.72em; font-family: var(--mono); padding: 0 0.6rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  main { padding: 1.5rem 2rem 3rem; max-width: 80rem; min-width: 0; }
  .title { margin: 0 0 1rem; font-size: 1.3rem; font-weight: 650; letter-spacing: -0.02em; }

  @media (max-width: 860px) {
    .shell { grid-template-columns: 1fr; }
    aside { position: static; height: auto; border-right: 0; border-bottom: 1px solid var(--line); padding: 0.75rem 1rem; }
    aside header { grid-template-columns: auto 1fr auto; align-items: center; padding: 0; }
    h1 { margin: 0; }
    .who { text-align: right; }
    nav {
      flex-direction: row; overflow-x: auto; gap: 0.25rem; margin: 0.5rem -1rem 0; padding: 0 1rem; scrollbar-width: none;
      /* A hint that the strip scrolls sideways: the edges fade rather
         than cut a button off clean, on top of whatever's behind (X-9). */
      mask-image: linear-gradient(to right, transparent, black 1rem, black calc(100% - 1rem), transparent);
      -webkit-mask-image: linear-gradient(to right, transparent, black 1rem, black calc(100% - 1rem), transparent);
    }
    .group { display: contents; }
    .groupname { display: none; }
    nav button { width: auto; white-space: nowrap; padding: 0.4rem 0.6rem; }
    nav button.active { box-shadow: inset 0 -2px 0 var(--accent); }
    .version { display: none; }
    main { padding: 1rem 1rem 3rem; }
  }
</style>
