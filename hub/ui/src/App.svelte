<script>
  import { get, post } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import Icon from './Icon.svelte';
  import Agents from './Agents.svelte';
  import Jobs from './Jobs.svelte';
  import Usage from './Usage.svelte';
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
  import Rules from './Rules.svelte';
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
      { id: 'usage',    label: 'Usage' },
      { id: 'tasks',    label: 'Tasks' },
    ] },
    { name: 'Space', tabs: [
      { id: 'peers',    label: 'Peers' },
    ] },
    { name: 'Hub', tabs: [
      { id: 'health',   label: 'Health' },
      { id: 'events',   label: 'Events' },
      { id: 'rules',    label: 'Rules' },
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
  // On a phone the rail is a strip that scrolls sideways; the tab that
  // just opened (a link into #settings, say) may sit past its edge.
  $effect(() => {
    void current; void auth; // the strip exists only once signed in
    document.querySelector('nav button.active, .taskbar .app.active')?.scrollIntoView({ inline: 'center', block: 'nearest' });
  });
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

  // On a wide screen the panel is a desktop: the tab open is a window
  // over a wallpaper, and the tabs live in a taskbar along the bottom
  // with a start menu for the words. Minimising the window shows the
  // desktop's icons; maximising it (remembered per browser) gives it the
  // whole screen. Under 860px it is the strip above, unchanged.
  const wide = matchMedia('(min-width: 861px)');
  let desktop = $state(wide.matches);
  $effect(() => {
    const on = () => (desktop = wide.matches);
    wide.addEventListener('change', on);
    return () => wide.removeEventListener('change', on);
  });
  const remembered = (k) => { try { return localStorage.getItem(k) === '1'; } catch { return false; } };
  let maximized = $state(remembered('homedash.maximized'));
  $effect(() => { try { localStorage.setItem('homedash.maximized', maximized ? '1' : '0'); } catch {} });
  let minimized = $state(false);
  let startOpen = $state(false);
  let now = $state(new Date());
  $effect(() => { const t = setInterval(() => (now = new Date()), 15_000); return () => clearInterval(t); });
  // A taskbar button opens its tab; the one already open minimises, the
  // way a taskbar does.
  function launch(id) {
    if (id === current && !minimized) minimized = true;
    else { current = id; minimized = false; }
    startOpen = false;
  }
  $effect(() => {
    if (!startOpen) return;
    const close = (e) => { if (e.type === 'keydown' ? e.key === 'Escape' : !e.target.closest('.startmenu, .start')) startOpen = false; };
    addEventListener('pointerdown', close);
    addEventListener('keydown', close);
    return () => { removeEventListener('pointerdown', close); removeEventListener('keydown', close); };
  });
</script>

<svelte:body class:viewer={auth?.user?.role != null && auth.user.role !== 'admin'} />

{#snippet page()}
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
    {:else if current === 'usage'}
      <Usage />
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
    {:else if current === 'rules'}
      <Rules />
    {:else if current === 'settings'}
      <Settings {hub} user={auth.user} onauth={loadAuth} />
    {:else}
      <Empty text="There is no such page." action="Back to Hosts" onaction={() => (current = 'hosts')} />
    {/if}
{/snippet}

{#snippet signal(s)}
  {#if s}<span class="signal" aria-hidden="true">{#if s.count !== '' && s.count != null}<span class="count">{s.count}</span>{/if}{#if s.led}<span class="led {s.led}"></span>{/if}</span>{/if}
{/snippet}

{#if !auth}
  <p class="loading">…</p>
{:else if !auth.user}
  <Auth {auth} onsignedin={loadAuth} />
{:else if desktop}
<div class="os">
  <div class="wallpaper">
    {#if minimized}
      <div class="icons" role="list">
        {#each tabs as t}
          <button class="icon" role="listitem" onclick={() => launch(t.id)}>
            <span class="tile"><Icon name={t.id} size={26} />{#if signals[t.id]?.led}<span class="led {signals[t.id].led}"></span>{/if}</span>
            <span>{t.label}</span>
          </button>
        {/each}
      </div>
    {:else}
      <section class="window" class:max={maximized} aria-label={title}>
        <div class="titlebar" role="presentation" ondblclick={() => (maximized = !maximized)}>
          <span class="appicon">{#if known && !cliParams}<Icon name={current} />{:else}<Icon name="mark" />{/if}</span>
          <span class="wtitle">{title}</span>
          {#if hub}<span class="whub">{hub.name}</span>{/if}
          <span class="grow"></span>
          <button class="wbtn" title="Minimise — show the desktop" aria-label="Minimise" onclick={() => (minimized = true)}><svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M2 9h8" stroke="currentColor" stroke-width="1.4" /></svg></button>
          <button class="wbtn" title={maximized ? 'Restore' : 'Maximise'} aria-label={maximized ? 'Restore' : 'Maximise'} onclick={() => (maximized = !maximized)}>
            <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">{#if maximized}<path d="M2.5 4.5h5v5h-5zM4.5 4.5v-2h5v5h-2" fill="none" stroke="currentColor" stroke-width="1.2" />{:else}<path d="M2.5 2.5h7v7h-7z" fill="none" stroke="currentColor" stroke-width="1.2" />{/if}</svg>
          </button>
        </div>
        <main class="content">{@render page()}</main>
      </section>
    {/if}
  </div>

  {#if startOpen}
    <div class="startmenu" role="menu">
      <header>
        <span class="mark"><Icon name="mark" size={18} /></span>
        <div><div class="wordmark">HomeDash</div>{#if hub}<div class="hubname mono">{hub.name}</div>{/if}</div>
      </header>
      <div class="sgroups">
        {#each groups as g}
          <div class="group">
            <span class="groupname" aria-hidden="true">{g.name}</span>
            {#each g.tabs as t}
              <button role="menuitem" class:active={current === t.id} onclick={() => launch(t.id)}>
                <Icon name={t.id} /><span class="label">{t.label}</span>{@render signal(signals[t.id])}
              </button>
            {/each}
          </div>
        {/each}
      </div>
      <footer>
        <span class="who">{auth.user.name} · {auth.user.role}</span>
        {#if hub}<span class="version" title="hub version">{hub.version}</span>{/if}
        <span class="grow"></span>
        <button class="quiet small" onclick={signOut}><Icon name="logout" size={14} /> Sign out</button>
      </footer>
    </div>
  {/if}

  <div class="taskbar" role="navigation" aria-label="Sections">
    <button class="start" class:open={startOpen} aria-label="Start" aria-expanded={startOpen} onclick={() => (startOpen = !startOpen)}><span class="mark"><Icon name="mark" size={18} /></span></button>
    <div class="apps">
      {#each groups as g, gi}
        {#if gi > 0}<span class="sep" aria-hidden="true"></span>{/if}
        {#each g.tabs as t}
          {@const s = signals[t.id]}
          <button class="app" class:active={current === t.id} class:shown={current === t.id && !minimized} title={t.label} aria-label={t.label} onclick={() => launch(t.id)}>
            <Icon name={t.id} size={18} />
            {#if s?.count !== '' && s?.count != null}<span class="badge">{s.count}</span>{/if}
            {#if s?.led}<span class="led {s.led}"></span>{/if}
          </button>
        {/each}
      {/each}
    </div>
    <div class="tray">
      {#if signals.health?.led}<button class="trayitem" title="Hub health" onclick={() => launch('health')}><span class="led {signals.health.led}"></span></button>{/if}
      {#if hub}<span class="trayhub mono">{hub.name}</span>{/if}
      <span class="clock"><span class="mono">{now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span><span class="date">{now.toLocaleDateString([], { day: 'numeric', month: 'short' })}</span></span>
    </div>
  </div>
</div>
{:else}
<div class="shell">
  <aside>
    <header>
      <div class="brand">
        <span class="mark"><Icon name="mark" size={18} /></span>
        <span class="wordmark">HomeDash</span>
        <button class="link signout" onclick={signOut}>sign out</button>
      </div>
      <h1>{#if hub}<span class="hubname">{hub.name}</span>{/if}</h1>
      <span class="who">{auth.user.name} · {auth.user.role}</span>
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
              {@render signal(s)}
            </button>
          {/each}
        </div>
      {/each}
    </nav>
    {#if hub}<span class="version" title="hub version">{hub.version}</span>{/if}
  </aside>

  <main>
    <h2 class="title">{title}</h2>
    {@render page()}
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
  .brand { display: flex; align-items: center; gap: 0.4rem; }
  .wordmark { font-weight: 650; letter-spacing: -0.02em; font-size: 1rem; }
  .signout { margin-left: auto; font-size: 0.78em; }
  h1 { margin: 0.35rem 0 0; font-size: 1.05rem; font-family: var(--mono); font-weight: 500; letter-spacing: 0; min-height: 1.3em; }
  .hubname { color: var(--fg); word-break: keep-all; }
  .who { color: var(--muted); font-size: 0.8em; }
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

  /* The desktop. */
  .os { position: fixed; inset: 0; display: grid; grid-template-rows: 1fr auto; overflow: hidden; }
  .wallpaper {
    position: relative; min-height: 0; padding: 1rem 1rem 0.75rem;
    background:
      radial-gradient(60rem 40rem at 85% 110%, color-mix(in srgb, var(--accent) 22%, transparent), transparent 70%),
      radial-gradient(50rem 30rem at 0% -10%, color-mix(in srgb, var(--s1) 14%, transparent), transparent 70%),
      radial-gradient(circle at 1px 1px, color-mix(in srgb, var(--fg) 9%, transparent) 1px, transparent 0) 0 0 / 22px 22px,
      var(--bg);
  }
  .window {
    height: 100%; max-width: 90rem; margin: 0 auto; display: grid; grid-template-rows: auto 1fr;
    background: var(--bg); border-radius: var(--r-lg); overflow: hidden;
    box-shadow: 0 0 0 1px var(--line), 0 18px 50px rgba(0, 0, 0, 0.22), 0 2px 6px rgba(0, 0, 0, 0.12);
    animation: open 0.14s ease-out;
  }
  .window.max { max-width: none; border-radius: 0; }
  .wallpaper:has(.window.max) { padding: 0; }
  @keyframes open { from { opacity: 0; transform: translateY(8px) scale(0.99); } }
  .titlebar {
    display: flex; align-items: center; gap: 0.55rem; padding: 0.35rem 0.4rem 0.35rem 0.85rem;
    background: var(--card); border-bottom: 1px solid var(--line); user-select: none;
  }
  .appicon { color: var(--accent); display: inline-flex; }
  .wtitle { font-weight: 650; letter-spacing: -0.01em; }
  .whub { color: var(--muted); font-family: var(--mono); font-size: 0.82em; }
  .whub::before { content: '— '; }
  .wbtn { background: none; border: 0; box-shadow: none; padding: 0.35rem 0.55rem; color: var(--muted); border-radius: var(--r); display: inline-grid; place-items: center; }
  .wbtn:hover:not(:disabled) { background: var(--sunk); color: var(--fg); }
  .content { overflow: auto; padding: 1.25rem 1.75rem 3rem; max-width: none; }

  .icons { display: grid; grid-template-columns: repeat(auto-fill, 6.5rem); grid-auto-rows: min-content; gap: 0.75rem; padding: 0.5rem; }
  .icon { display: grid; justify-items: center; gap: 0.35rem; background: none; border: 0; box-shadow: none; padding: 0.6rem 0.25rem; border-radius: var(--r-lg); color: var(--fg); font-size: 0.85em; }
  .icon:hover:not(:disabled) { background: color-mix(in srgb, var(--card) 70%, transparent); }
  .tile { position: relative; display: grid; place-items: center; width: 3.2rem; height: 3.2rem; border-radius: 12px; background: var(--card); box-shadow: var(--shadow); color: var(--accent); }
  .tile .led { position: absolute; top: 4px; right: 4px; }

  .taskbar {
    display: flex; align-items: center; gap: 0.5rem; height: 3.1rem; padding: 0 0.6rem;
    background: color-mix(in srgb, var(--card) 88%, transparent); backdrop-filter: blur(14px);
    border-top: 1px solid var(--line); z-index: 5;
  }
  .start { background: none; border: 0; box-shadow: none; padding: 0.3rem; border-radius: var(--r); }
  .start:hover:not(:disabled), .start.open { background: var(--sunk); }
  .apps { flex: 1; display: flex; align-items: center; justify-content: center; gap: 2px; min-width: 0; overflow-x: auto; scrollbar-width: none; }
  .sep { width: 1px; height: 1.4rem; background: var(--line); margin: 0 0.35rem; flex: none; }
  .app {
    position: relative; flex: none; display: grid; place-items: center; width: 2.6rem; height: 2.5rem;
    background: none; border: 0; box-shadow: none; padding: 0; border-radius: var(--r); color: var(--muted);
  }
  .app:hover:not(:disabled) { background: var(--sunk); color: var(--fg); }
  .app.active { color: var(--fg); }
  .app.active::after { content: ''; position: absolute; bottom: 2px; left: 50%; width: 0.5rem; height: 3px; border-radius: 2px; background: var(--muted); transform: translateX(-50%); transition: width 0.15s; }
  .app.shown { background: var(--sunk); }
  .app.shown::after { width: 1.1rem; background: var(--accent); }
  .app.shown :global(svg) { color: var(--accent); }
  .app .led { position: absolute; top: 5px; right: 6px; width: 0.42rem; height: 0.42rem; }
  .badge { position: absolute; bottom: 4px; right: 1px; font-family: var(--mono); font-size: 0.62rem; line-height: 1; padding: 0.12rem 0.25rem; border-radius: 999px; background: var(--card); box-shadow: 0 0 0 1px var(--line); color: var(--fg); }
  .tray { display: flex; align-items: center; gap: 0.65rem; flex: none; }
  .trayitem { background: none; border: 0; box-shadow: none; padding: 0.4rem; border-radius: var(--r); }
  .trayhub { color: var(--muted); font-size: 0.8em; max-width: 12rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .clock { display: grid; justify-items: end; line-height: 1.15; font-size: 0.8em; }
  .clock .date { color: var(--muted); font-size: 0.9em; }

  .startmenu {
    position: fixed; left: 0.6rem; bottom: 3.6rem; width: 21rem; max-height: calc(100vh - 5rem); overflow: auto; z-index: 6;
    background: var(--card); border-radius: var(--r-lg); box-shadow: 0 0 0 1px var(--line), 0 18px 50px rgba(0, 0, 0, 0.28);
    display: grid; grid-template-rows: auto 1fr auto; animation: open 0.12s ease-out;
  }
  .startmenu header { display: flex; gap: 0.6rem; align-items: center; padding: 0.9rem 1rem 0.7rem; border-bottom: 1px solid var(--line); }
  .startmenu .hubname { font-size: 0.85em; color: var(--muted); }
  .sgroups { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem 0.5rem; padding: 0.75rem; align-items: start; }
  .sgroups .group { align-content: start; }
  .startmenu footer { display: flex; align-items: center; gap: 0.6rem; padding: 0.5rem 0.5rem 0.5rem 1rem; border-top: 1px solid var(--line); background: color-mix(in srgb, var(--sunk) 50%, var(--card)); }
  .startmenu .version { padding: 0; }
  .startmenu button[role=menuitem] {
    display: flex; align-items: center; gap: 0.55rem; width: 100%; text-align: left;
    background: none; border: 0; box-shadow: none; padding: 0.4rem 0.55rem; border-radius: var(--r);
    color: var(--muted); font-size: 0.92em; font-weight: 500;
  }
  .startmenu button[role=menuitem]:hover { color: var(--fg); background: var(--sunk); }
  .startmenu button[role=menuitem].active { color: var(--fg); background: var(--sunk); }
  .startmenu button[role=menuitem].active :global(svg) { color: var(--accent); }

  @media (max-width: 860px) {
    .shell { grid-template-columns: 1fr; grid-template-rows: auto 1fr; }
    /* The strip stays pinned, so a tab is a thumb away from the bottom
       of a long page; z-index over the tables' shadows behind it. */
    aside {
      position: sticky; top: 0; z-index: 2; height: auto; min-width: 0; gap: 0;
      border-right: 0; border-bottom: 1px solid var(--line);
      padding: max(0.75rem, env(safe-area-inset-top)) 1rem 0.5rem;
    }
    aside header { grid-template-columns: auto 1fr auto; align-items: center; padding: 0; }
    h1 { margin: 0; font-size: 0.95rem; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
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
    main { padding: 1rem 1rem max(3rem, env(safe-area-inset-bottom)); }
  }
  @media (max-width: 560px) {
    .who { display: none; } /* name and role are in Settings */
  }
</style>
