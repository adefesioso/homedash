<script>
  import Notice from './ui/Notice.svelte';
  import { get, post, put, del } from './lib/api.js';
  import { poll } from './lib/poll.js';
  import { ago } from './lib/format.js';
  import { useEscape } from './lib/actions.js';
  import Empty from './Empty.svelte';
  import Icon from './Icon.svelte';

  // The Tasks tab: everything scheduled in the house, with its last runs
  // on the row, because "did that actually work" is the only question
  // anyone asks of a scheduled job.
  let tasks = $state([]);
  let hosts = $state([]);
  let runs = $state({});
  let open = $state(null);
  let edit = $state(null);
  let loadError = $state('');
  let error = $state('');

  async function load() {
    try {
      [tasks, hosts] = await Promise.all([get('/tasks'), get('/hosts')]);
      if (open) runs = { ...runs, [open]: await get(`/tasks/${open}/runs`) };
      loadError = '';
    } catch (e) { loadError = e.message; }
  }
  $effect(() => {
    return poll(load, 10_000);
  });

  const blank = () => ({ name: '', hostId: 0, command: '', schedule: '0 3 * * *', timeoutSeconds: 600, enabled: true });

  // The common cases, by cron line, so most tasks never need to know cron
  // syntax; "Custom…" drops to the raw field for anything else.
  const presets = [
    { value: '@hourly', label: 'Every hour' },
    { value: '0 3 * * *', label: 'Every day, 3am' },
    { value: '@daily', label: 'Every day, midnight' },
    { value: '@weekly', label: 'Every week (Sunday, midnight)' },
    { value: '@monthly', label: 'Every month (1st, midnight)' },
  ];
  const isPreset = (schedule) => presets.some((p) => p.value === schedule);
  function pickSchedule(e) {
    const v = e.target.value;
    edit.schedule = v === 'custom' ? '' : v;
  }

  async function save() {
    try {
      const ts = edit.timeoutSeconds;
      const body = { ...edit, hostId: Number(edit.hostId), timeoutSeconds: ts === '' || ts == null ? null : Number(ts) };
      await (edit.id ? put(`/tasks/${edit.id}`, body) : post('/tasks', body));
      edit = null; error = ''; await load();
      dispatchEvent(new CustomEvent('homedash:signals-changed'));
    } catch (e) { error = e.message; }
  }
  async function run(t) { try { await post(`/tasks/${t.id}/run`); error = ''; open = t.id; setTimeout(load, 1500); } catch (e) { error = e.message; } }
  async function remove(t) {
    if (!confirm(`Delete task ${t.name}?`)) return;
    try {
      await del(`/tasks/${t.id}`); error = ''; await load();
      dispatchEvent(new CustomEvent('homedash:signals-changed'));
    } catch (e) { error = e.message; }
  }
  const lastRuns = (t) => runs[t.id] ?? [];
</script>

<div class="bar">
  <button class="primary" onclick={() => (edit = blank())}><Icon name="plus" size={14} /> New task</button>
  {#if loadError}<Notice>{loadError}</Notice>{/if}
  {#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}
</div>

{#if edit}
  <section class="card form" use:useEscape={() => (edit = null)}>
    <label>Name <input bind:value={edit.name} /></label>
    <label>Host
      <select bind:value={edit.hostId}>
        <option value={0}>every enrolled remote, one at a time</option>
        {#each hosts as h}<option value={h.id}>{h.name}</option>{/each}
      </select>
    </label>
    <label>Command <input class="mono" bind:value={edit.command} placeholder="docker system prune -af" /></label>
    <label>Schedule
      <select value={isPreset(edit.schedule) ? edit.schedule : 'custom'} onchange={pickSchedule}>
        {#each presets as p}<option value={p.value}>{p.label}</option>{/each}
        <option value="custom">Custom…</option>
      </select>
    </label>
    {#if !isPreset(edit.schedule)}
      <label>Custom schedule <input class="mono" bind:value={edit.schedule} placeholder="0 3 * * *   or   @daily" /></label>
    {/if}
    <label>Timeout, seconds <input type="number" bind:value={edit.timeoutSeconds} /></label>
    <label class="row"><input type="checkbox" bind:checked={edit.enabled} /> Enabled</label>
    <div class="row"><button class="primary" onclick={save} disabled={!edit.name || !edit.command || !edit.schedule}>Save</button><button class="quiet" onclick={() => (edit = null)}>Cancel</button></div>
  </section>
{/if}

{#if !loadError && tasks.length === 0}
  <Empty text="Nothing scheduled in the house." action="New task" onaction={() => (edit = blank())} />
{:else if !loadError}
  <table>
    <tbody>
      {#each tasks as t (t.id)}
        <tr class:off={!t.enabled}>
          <td><span class="led {!t.enabled ? '' : t.failing ? 'bad' : 'ok'}"></span> <strong>{t.name}</strong><br /><code class="small muted">{t.command}</code></td>
          <td class="mono">{t.host || 'every remote'}</td>
          <td><code>{t.schedule}</code></td>
          <td><span class="pill {!t.enabled ? '' : t.failing ? 'bad' : 'ok'}">{t.enabled ? (t.failing ? 'failing' : 'ok') : 'off'}</span></td>
          <td class="actions">
            <button class="small" onclick={() => run(t)}>Run now</button>
            <button class="small quiet" onclick={() => { open = open === t.id ? null : t.id; load(); }} aria-expanded={open === t.id}>Runs</button>
            <button class="small quiet" onclick={() => (edit = { ...t })}>Edit</button>
            <button class="small danger quiet" onclick={() => remove(t)}>Delete</button>
          </td>
        </tr>
        {#if open === t.id}
          <tr class="detail"><td colspan="5">
            {#if lastRuns(t).length === 0}<span class="muted">no runs yet</span>{/if}
            {#each lastRuns(t) as r (r.id)}
              <details>
                <summary>
                  <span class="muted">{ago(r.started)}</span> <strong>{r.host || '—'}</strong>
                  {#if r.skipped}<span class="muted">skipped: {r.skipped}</span>
                  {:else if r.exitCode == null}<span>running…</span>
                  {:else}<span class="pill {r.exitCode === 0 ? 'ok' : 'bad'}">exit {r.exitCode}</span>{/if}
                </summary>
                {#if r.output}<pre>{r.output}</pre>{/if}
              </details>
            {/each}
          </td></tr>
        {/if}
      {/each}
    </tbody>
  </table>
{/if}

<style>
  tr.off td { opacity: 0.55; }
  details { margin: 0.2rem 0; }
  summary { display: flex; gap: 0.5rem; align-items: center; font-size: 0.9em; }
  pre { max-height: 12rem; margin-top: 0.3rem; }
</style>
