<script>
  import { get, post, del } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { ago } from './lib/format.js';
  import Empty from './Empty.svelte';
  import Notice from './ui/Notice.svelte';

  // The Jobs tab: every job — started here, by a session on the Agents
  // tab, or from the API — with its report and stream. A job is readable
  // while it runs and after, with the remote off: the hub keeps every
  // event.
  let jobs = $state([]);
  let hosts = $state([]);
  let workspaces = $state([]);
  let open = $state(null);
  let events = $state([]);
  let correction = $state('');
  let loadError = $state('');
  let error = $state('');
  let draft = $state({ host: '', cwd: '', text: '' });
  // Running jobs are always in view; the finished ones fold to the most
  // recent, with the rest a click away.
  let showAll = $state(false);
  const running = $derived(jobs.filter((j) => j.state === 'running'));
  const history = $derived(jobs.filter((j) => j.state !== 'running'));
  // A job you have open stays in view even when a newer one folds it.
  const shown = $derived(showAll ? jobs : jobs.filter((j) => j.state === 'running' || j.id === history[0]?.id || j.id === open));

  async function load() {
    try {
      [jobs, hosts, workspaces] = await Promise.all([get('/jobs'), get('/hosts'), get('/workspaces').catch(() => [])]);
      if (open) await loadEvents();
      loadError = '';
    } catch (e) { loadError = e.message; }
  }
  // A job's stream only grows, so ask for what came after the last line
  // shown rather than the whole log every five seconds. The hub caps a
  // reply at 500 lines; keep asking until it is caught up.
  async function loadEvents() {
    const id = open;
    for (;;) {
      const after = events.length ? events[events.length - 1].id : 0;
      const more = await get(`/jobs/${id}/events?after=${after}`);
      if (open !== id) return;
      if (more.length) events = [...events, ...more];
      if (more.length < 500) return;
    }
  }
  $effect(() => {
    return poll(load, 5_000);
  });

  async function start() {
    try { const j = await post('/jobs', draft); draft = { ...draft, text: '' }; open = j.id; events = []; await load(); }
    catch (e) { error = e.message; }
  }
  async function rollback(id) {
    if (!confirm('Put the machine back to the snapshot taken before this job? On btrfs this restores live; on LVM it merges at the next boot.')) return;
    try { await post(`/jobs/${id}/rollback`); await load(); }
    catch (e) { error = e.message; }
  }
  async function correct(id) {
    try { await post(`/jobs/${id}/correct`, { text: correction }); correction = ''; await load(); }
    catch (e) { error = e.message; }
  }
  async function clearHistory() {
    if (!confirm(`Delete all ${history.length} finished jobs, with their events and snapshots? Running jobs stay.`)) return;
    try { await del('/jobs'); if (open && !running.some((j) => j.id === open)) open = null; showAll = false; error = ''; await load(); }
    catch (e) { error = e.message; }
  }
  const summary = (line) => {
    try {
      const e = JSON.parse(line);
      if (e.type === 'tool_execution_start') return `→ ${e.toolName} ${JSON.stringify(e.args).slice(0, 140)}`;
      if (e.type === 'tool_execution_end') return `← ${e.toolName}: ${(e.result?.content?.[0]?.text ?? '').slice(0, 160).replace(/\n/g, ' ')}`;
      if (e.type === 'auto_retry_start') return `retry ${e.attempt}/${e.maxAttempts}: ${e.errorMessage}`;
      if (e.type === 'message_end' && e.message?.role === 'assistant') {
        const text = (e.message.content?.map((c) => c.text ?? '').join(' ') ?? '').slice(0, 200).trim();
        return text ? `assistant: ${text}` : null;
      }
      if (e.type === 'stderr') return `stderr: ${e.text.slice(0, 200)}`;
      if (e.type === 'sudo') return `root (exit ${e.exit}): ${e.command.slice(0, 120)}${e.exit < 0 ? ' — ' + e.text.slice(0, 120) : ''}`;
      if (e.type === 'session') return `session ${e.id}`;
      return null;
    } catch { return line.slice(0, 160); }
  };
</script>

<section class="card form full">
  <div class="row">
    <select bind:value={draft.host}>
      <option value="">on which machine</option>
      {#each hosts as h}<option value={h.name} disabled={h.status !== 'online'}>{h.name}{h.status !== 'online' ? ` (${h.status})` : ''}</option>{/each}
    </select>
    <input class="cwd" placeholder="working directory (default: the hub account's home)" bind:value={draft.cwd} list="workspaces" />
    <datalist id="workspaces">
      {#each workspaces.filter((w) => !draft.host || w.members.some((m) => m.host === draft.host)) as w}<option value={w.path}>shared workspace {w.name}</option>{/each}
    </datalist>
  </div>
  <textarea rows="3" placeholder="What should be true when it is done? The machine's own agent works alone and ends with a report." bind:value={draft.text}></textarea>
  <div class="row">
    <button class="primary" onclick={start} disabled={!draft.host || !draft.text.trim()}>Start job</button>
    {#if loadError}<Notice>{loadError}</Notice>{/if}
    {#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}
  </div>
</section>

{#if !loadError && jobs.length === 0}
  <Empty text="No jobs yet. A job is one machine's own agent working on that machine; pick a host above and say what should be true when it is done." />
{:else if jobs.length > 0}
  <div class="scroll">
  <table>
    <tbody>
      {#each shown as j (j.id)}
        <tr class="job" class:open={open === j.id} onclick={() => { open = open === j.id ? null : j.id; events = []; load(); }}>
          <td class="mono muted">#{j.id}</td>
          <td class="mono"><strong>{j.host}</strong></td>
          <td><span class="pill {j.state === 'done' ? 'ok' : j.state === 'running' ? 'accent' : j.state === 'failed' || j.state === 'needs_you' ? 'bad' : ''}">{j.state.replace('_', ' ')}</span></td>
          <td class="muted small">round {j.rounds}</td>
          <td class="text">{j.text.slice(0, 90)}</td>
          <td class="muted small nowrap">{ago(j.started)}</td>
        </tr>
        {#if open === j.id}
          <tr class="detail"><td colspan="6">
            {#if j.reason}<Notice>{j.reason}</Notice>{/if}
            <p class="muted small">snapshot: {j.snapshot || 'none'} {#if j.snapshot === 'none' || !j.snapshot}— no way back but the rebuild script{:else if j.snapshot === 'merge-at-boot'}— reboot the machine to complete the rollback{/if}
              {#if j.state !== 'running' && (j.snapshot === 'btrfs' || j.snapshot === 'lvm')}<button class="small" onclick={() => rollback(j.id)}>Roll back</button>{/if}</p>
            {#if j.report}<h4>Report</h4><pre>{j.report}</pre>{/if}
            <h4>Stream</h4>
            <div class="stream">
              {#each events as e (e.id)}
                {@const s = summary(e.line)}
                {#if s}<div>{s}</div>{/if}
              {/each}
              {#if events.length === 0}<span class="muted">nothing yet</span>{/if}
            </div>
            {#if j.state !== 'running' && j.session}
              <div class="row correct">
                <input placeholder="a correction, on the same remote session" bind:value={correction} onkeydown={(e) => e.key === 'Enter' && correct(j.id)} />
                <button onclick={() => correct(j.id)} disabled={!correction.trim()}>Correct</button>
              </div>
            {/if}
          </td></tr>
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
  .full { max-width: none; }
  .cwd { flex: 1; min-width: 14rem; }
  tr.job { cursor: pointer; }
  tr.open td { border-bottom: 0; }
  .text { max-width: 30rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  pre { max-height: 20rem; }
  .stream { font: 11px/1.5 var(--mono); background: var(--sunk); padding: 0.5rem 0.75rem; border-radius: var(--r); max-height: 16rem; overflow: auto; }
  .correct { margin-top: 0.6rem; }
  .correct input { flex: 1; min-width: 12rem; }
</style>
