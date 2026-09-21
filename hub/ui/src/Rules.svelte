<script>
  import Notice from './ui/Notice.svelte';
  import { get, put } from './lib/api.js';
  // Rules: free-text lines the hub agent must respect, on top of the
  // standing instructions every window starts with (agent/context.go).
  // What the hub refuses (Settings' Refusals section) is a hard gate,
  // enforced whether or not a rule asks around it, and always wins —
  // said here so the precedence is visible next to what it bounds.
  let rules = $state('');
  let gate = $state([]);
  let saved = $state('');
  let loadError = $state('');
  let error = $state('');

  async function load() {
    try {
      const [s, g] = await Promise.all([get('/settings'), get('/gate')]);
      rules = s['agent.rules'] ?? '';
      gate = g;
      loadError = '';
    } catch (e) { loadError = e.message; }
  }
  load();

  async function save() {
    try {
      await put('/settings', { 'agent.rules': rules });
      saved = 'Saved'; setTimeout(() => (saved = ''), 2000); error = '';
    } catch (e) { error = e.message; }
  }
</script>

{#if loadError}<Notice>{loadError}</Notice>{/if}
{#if error}<Notice ondismiss={() => (error = '')}>{error}</Notice>{/if}

<div class="sections">
  <section class="card">
    <p class="help">One per line. The hub agent reads these alongside its standing instructions, on every window. What the hub refuses always takes precedence — a rule that would need a refused action is refused, not followed.</p>
    <form class="form" onsubmit={(e) => { e.preventDefault(); save(); }}>
      <label>
        <textarea rows="10" placeholder="e.g. Ask before installing anything new.&#10;Prefer the smallest change that solves the problem." bind:value={rules} spellcheck="false"></textarea>
      </label>
      <div><button type="submit" class="primary">Save</button> <span class="muted">{saved}</span></div>
    </form>
  </section>

  {#if gate.length > 0}
    <section class="card">
      <h2>What the hub refuses</h2>
      <p class="help">Always, no rule above can override this.</p>
      <ul>{#each gate as g}<li>{g}</li>{/each}</ul>
    </section>
  {/if}
</div>

<style>
  .sections { display: grid; gap: 1rem; }
</style>
