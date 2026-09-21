<script>
  // Restore: an export file and its passphrase go up as multipart; the
  // hub stages the file, checks the passphrase, and restarts itself to
  // apply it. We wait for /api/health to answer again, then reload —
  // the restored accounts are what sign in from here on.
  let { onerror } = $props();
  let file = $state(null);
  let passphrase = $state('');
  let busy = $state(false);
  let waiting = $state(false);

  async function restore() {
    if (!file) return;
    if (!confirm('Replace everything this hub knows with the export? Hosts, clusters, accounts, the vault — the current state is overwritten and the hub restarts.')) return;
    busy = true; onerror('');
    const form = new FormData();
    form.append('file', file);
    form.append('passphrase', passphrase);
    try {
      const r = await fetch('/api/backup/restore', { method: 'POST', body: form });
      if (!r.ok) throw new Error((await r.text()).trim() || `the hub answered ${r.status}`);
      waiting = true;
      await new Promise((res) => setTimeout(res, 3000));
      for (let i = 0; i < 60; i++) {
        try { if ((await fetch('/api/health')).ok) break; } catch {}
        await new Promise((res) => setTimeout(res, 1000));
      }
      location.reload();
    } catch (e) { onerror(e.message); busy = false; }
  }
</script>

{#if waiting}
  <p class="muted">Restored. Waiting for the hub to come back…</p>
{:else}
  <div class="row">
    <input type="file" accept=".age,.tar.age" onchange={(e) => (file = e.target.files[0] || null)} />
    <input type="password" placeholder="its passphrase" autocomplete="off" bind:value={passphrase} />
    <button type="button" class="danger" onclick={restore} disabled={busy || !file || !passphrase}>Restore</button>
  </div>
{/if}
