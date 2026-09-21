<script>
  // One session's PTY, over the WebSocket the hub keeps behind it. Binary
  // frames are the terminal in both directions; a text frame is a resize.
  // A viewer attaches as a reader: the hub drops its keystrokes, so the
  // status line says so instead of pretending the keyboard works (X-8).
  // With live=false it is the History view of a finished session: the
  // bytes the hub kept when the process ended, written once into the
  // same xterm, no socket and no keyboard.
  //
  // xterm is loaded here, on the first session opened, not with the
  // page: it is most of the panel's script and most visits never open
  // a terminal.
  let { id, role, onended, live = true } = $props();
  let host;
  let status = $state('connecting');
  const readOnly = $derived(role != null && role !== 'admin');

  $effect(() => {
    let gone = false;
    let cleanup = null;
    (async () => {
      const [{ Terminal }, { FitAddon }] = await Promise.all([
        import('@xterm/xterm'), import('@xterm/addon-fit'), import('@xterm/xterm/css/xterm.css'),
      ]);
      if (gone) return;
      const term = new Terminal({ cursorBlink: live, disableStdin: !live, fontSize: 13, scrollback: 5000,
        fontFamily: "'JetBrains Mono Variable', ui-monospace, Menlo, monospace",
        theme: { background: '#15181c', foreground: '#e6e4de', cursor: '#e3a83c' } });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(host);
      fit.fit();
      if (!live) {
        const ro = new ResizeObserver(() => fit.fit());
        ro.observe(host);
        cleanup = () => { ro.disconnect(); term.dispose(); };
        try {
          const r = await fetch(`/api/agents/sessions/${id}/history`);
          if (!r.ok) throw new Error(await r.text());
          const b = new Uint8Array(await r.arrayBuffer());
          if (gone) return;
          if (b.length) { term.write(b); status = 'ended · history'; }
          else status = 'nothing kept — the hub restarted while this session was live';
        } catch (e) { status = `history unavailable: ${e.message}`; }
        return;
      }
      window.__term = term; // tests only: read the buffer without a screenshot

      const proto = location.protocol === 'https:' ? 'wss' : 'ws';
      const ws = new WebSocket(`${proto}://${location.host}/api/agents/sessions/${id}/pty`);
      ws.binaryType = 'arraybuffer';
      const enc = new TextEncoder();
      const resize = () => {
        fit.fit();
        if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ resize: { cols: term.cols, rows: term.rows } }));
      };
      ws.onopen = () => { status = 'attached'; resize(); term.focus(); };
      ws.onmessage = (e) => term.write(new Uint8Array(e.data));
      ws.onclose = (e) => { status = e.reason === 'session ended' ? 'ended' : 'detached'; if (status === 'ended') onended?.(); };
      ws.onerror = () => { status = 'detached'; };
      const sub = term.onData((d) => { if (ws.readyState === WebSocket.OPEN) ws.send(enc.encode(d)); });
      const ro = new ResizeObserver(resize);
      ro.observe(host);
      cleanup = () => { ro.disconnect(); sub.dispose(); ws.close(); term.dispose(); if (window.__term === term) delete window.__term; };
    })();
    return () => { gone = true; cleanup?.(); };
  });
</script>

<div class="term" bind:this={host}></div>
<p class="status">{status}{#if readOnly && live} · read only — viewers watch{/if}</p>

<style>
  .term { height: min(70dvh, 40rem); background: #15181c; border-radius: var(--r); padding: 0.5rem; }
  .status { color: var(--muted); font-size: 0.8em; font-family: var(--mono); margin: 0.4rem 0 0; }
</style>
