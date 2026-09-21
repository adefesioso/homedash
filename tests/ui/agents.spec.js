// G-2: a session is a seat at the fleet (docs/pooling/agents/README.md) —
// esbuild's default target lowered xterm's `let r; f(r ||= {})` into
// `f(void 0||(s={}))` with no declaration for `s`, so the first DECRQM
// omp sent threw and the terminal never drew a pixel. Rebuilt for
// es2022 (hub/ui/vite.config.js), the same session should render the
// omp banner and throw nothing.
import { test, expect } from './fixtures.js';

test('an agent session attaches and renders omp with no uncaught error', async ({ page, hub, account }) => {
  const errors = [];
  page.on('pageerror', (e) => errors.push(e));

  await page.getByRole('navigation').getByRole('button', { name: 'Agents', exact: true }).click();
  const bar = page.locator('.bar');
  const newSession = bar.getByRole('button', { name: 'New session' });
  await expect(newSession).toBeEnabled({ timeout: 60_000 }); // omp may still be fetching itself in

  // The hub names the session (docs/pooling/agents/README.md: a
  // passphrase, adjective-noun); the row that appears is the one to
  // clean up.
  const before = new Set(((await hub.api('GET', '/api/agents/sessions')).body ?? []).map((x) => x.id));
  let name = '';
  try {
    await newSession.click();

    // Agents.svelte attaches the session it just opened, under the name
    // the hub drew.
    const open = page.locator('section.card.open');
    await expect(open.locator('strong')).toHaveText(/^[a-z]+-[a-z]+$/);
    name = await open.locator('strong').textContent();

    // Terminal.svelte exposes the live xterm instance as window.__term
    // for tests only — canvas rendering leaves nothing else to read the
    // banner from.
    await page.waitForFunction(() => {
      const term = window.__term;
      if (!term) return false;
      const buf = term.buffer.active;
      let text = '';
      for (let i = 0; i < buf.length; i++) text += buf.getLine(i)?.translateToString(true) ?? '';
      return text.includes('omp');
    }, { timeout: 20_000 });

    expect(errors, 'no uncaught error attaching a session').toEqual([]);
  } finally {
    // DELETE ends the session and drops it from the list, so a test run
    // leaves no history behind.
    const { body: sessions } = await hub.api('GET', '/api/agents/sessions');
    for (const s of sessions.filter((x) => !before.has(x.id) && (x.name === name || !name))) await hub.api('DELETE', `/api/agents/sessions/${s.id}`);
  }
});
