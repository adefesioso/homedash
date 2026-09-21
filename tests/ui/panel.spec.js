// README: "The panel shows what a remote last reported about itself" —
// the signed-in panel is the API, drawn. Every tab the README names
// opens, and the Hosts tab lists exactly what GET /api/hosts returns.
import { test, expect } from './fixtures.js';
import { HUB } from './lab.js';

test('the panel says which hub it is', async ({ page, account }) => {
  // docs/running/README.md: "The panel is titled with the hub's hostname"
  // — both lab hubs reach this machine as localhost:7433, and this is
  // what tells them apart.
  await expect(page.locator('header').first().locator('.hubname')).toHaveText(HUB);
  await expect(page).toHaveTitle(`${HUB} · HomeDash`);
});

const tabs = ['Hosts', 'Network', 'Storage', 'Apps', 'Models', 'Agents', 'Tasks', 'Peers', 'Health', 'Events', 'Settings'];

test('every tab opens once signed in', async ({ page, account }) => {
  // G-2: a build that throws on render (xterm's ReferenceError) would
  // still pass the assertions below if nothing checks the console.
  const errors = [];
  page.on('pageerror', (e) => errors.push(e));
  for (const label of tabs) {
    await page.getByRole('navigation').getByRole('button', { name: label, exact: true }).click();
    await expect(page.getByRole('navigation').getByRole('button', { name: label, exact: true })).toHaveClass(/active/);
    await expect(page).toHaveURL(new RegExp(`#${label.toLowerCase()}$`));
    await expect(page.locator('main')).toBeVisible();
  }
  expect(errors, 'no uncaught error opening any tab').toEqual([]);
});

test('the Hosts tab lists exactly the hosts the API reports', async ({ page, hub, account }) => {
  const { body: hosts } = await hub.api('GET', '/api/hosts');
  await page.getByRole('navigation').getByRole('button', { name: 'Hosts', exact: true }).click();
  if (hosts.length === 0) {
    await expect(page.getByText('No machines enrolled')).toBeVisible();
    return;
  }
  const cards = page.locator('section.card strong');
  await expect(cards).toHaveCount(hosts.length);
  const shown = (await cards.allTextContents()).map((s) => s.trim()).sort();
  expect(shown).toEqual(hosts.map((h) => h.name).sort());
});

test('the Health tab draws exactly what GET /api/hub/health reports', async ({ page, hub, account }) => {
  // docs/running/README.md#is-the-hub-well: the hub read the way a
  // remote is — its hostname on the plate, one line per check with its
  // state and reason, and the rail's LED the worst of them.
  const { body: h } = await hub.api('GET', '/api/hub/health');
  await page.getByRole('navigation').getByRole('button', { name: 'Health', exact: true }).click();
  const plate = page.locator('section.card.plate');
  await expect(plate.locator('strong')).toHaveText(h.machine.hostname);
  const rows = plate.locator('.checks .check');
  await expect(rows).toHaveCount(h.checks.length);
  for (const c of h.checks) {
    const row = rows.filter({ hasText: c.name }).first();
    await expect(row).toHaveClass(new RegExp(`\\b${c.state}\\b`));
    await expect(row.locator('.detail')).toContainText(c.detail.slice(0, 20));
  }
  const led = page.getByRole('navigation').getByRole('button', { name: 'Health', exact: true }).locator('.led');
  await expect(led).toHaveClass(new RegExp({ ok: 'ok', warn: 'busy', bad: 'bad' }[h.state]));
});

test('a 502 with an HTML body reads as one sentence, not raw markup', async ({ page, account }) => {
  // X-2: a proxy's error page rendered verbatim in the error span
  // ("<html><head><title>502 Bad Gateway…") — api.js now turns any body
  // starting with "<" into "the hub answered <status>" (PR 2, FIXES.md).
  // account already landed on Hosts (the default tab) before this route
  // is installed; hop off and back so Hosts.svelte's load() runs fresh
  // against the mocked response instead of data fetched on mount.
  await page.getByRole('navigation').getByRole('button', { name: 'Events', exact: true }).click();
  await page.route('**/api/hosts', (route) => route.fulfill({
    status: 502,
    contentType: 'text/html',
    body: '<html><head><title>502 Bad Gateway</title></head><body>bad gateway</body></html>',
  }));
  await page.getByRole('navigation').getByRole('button', { name: 'Hosts', exact: true }).click();
  const err = page.locator('.bar .error');
  await expect(err).toBeVisible();
  await expect(err).not.toContainText('<');
  await expect(err).toContainText('502');
});

test('a session that no longer exists returns to the sign-in card', async ({ page, hub, account }) => {
  // X-1: the panel used to keep its shell (header, tabs, buttons) after
  // the account behind it was deleted, showing a dead "sign in" inline
  // instead of ever offering to actually sign in again. api.js's 401
  // handler (PR 2) dispatches homedash:signed-out, which App.svelte now
  // reacts to by re-reading /auth/state.
  await hub.api('DELETE', `/api/users/${account.name}`);
  const card = page.locator('section.card');
  await expect(card.getByRole('button', { name: 'Sign in' })).toBeVisible({ timeout: 20_000 });
});

test('a viewer sees the panel without the keyboard', async ({ page, hub, registerAccount }) => {
  // docs/running/accounts.md: viewer is "the panel, read-only". The
  // header says the role; the split itself is enforced at the API,
  // which accounts_test.sh covers. This test walks a viewer through
  // three destructive controls and checks the 403 the API sends back
  // ("viewers read; this needs an admin") stays on screen — X-7 used to
  // have load()'s next poll wipe it within a few seconds.
  const { body: state } = await hub.api('GET', '/api/auth/state');
  test.skip(state.setupOpen, 'no admin yet: the first account cannot be a viewer');
  const name = 'tests-ui-viewer-' + Date.now().toString(36);
  const taskName = 'tests-ui-viewer-task-' + Date.now().toString(36);
  const errors = [];
  page.on('pageerror', (e) => errors.push(e));
  page.on('dialog', (d) => d.accept());
  const { body: task } = await hub.api('POST', '/api/tasks', { name: taskName, hostId: 0, command: 'true', schedule: '0 3 * * *', timeoutSeconds: 60, enabled: true });
  try {
    await registerAccount(page, name, 'viewer');
    await page.getByRole('navigation').getByRole('button', { name: 'Hosts', exact: true }).click();
    await expect(page.locator('main')).toBeVisible();

    // Hosts: Revoke never checks host status, so it is always clickable —
    // the API refuses before anything on the host is touched.
    const { body: hosts } = await hub.api('GET', '/api/hosts');
    if (hosts.length > 0) {
      const card = page.locator('section.card.plate').filter({ hasText: hosts[0].name }).first();
      await card.getByRole('button', { name: 'Revoke' }).click();
      const err = page.locator('.error', { hasText: 'viewers read' });
      await expect(err).toBeVisible();
      await page.waitForTimeout(16_000); // past Hosts.svelte's 15s poll
      await expect(err).toBeVisible();
    }

    // Tasks: Delete, on a throwaway task made through the API above.
    await page.getByRole('navigation').getByRole('button', { name: 'Tasks', exact: true }).click();
    const row = page.locator('tr').filter({ hasText: taskName });
    await expect(row).toBeVisible();
    await row.getByRole('button', { name: 'Delete' }).click();
    const taskErr = page.locator('.error', { hasText: 'viewers read' });
    await expect(taskErr).toBeVisible();
    await page.waitForTimeout(16_000); // past Tasks.svelte's 10s poll
    await expect(taskErr).toBeVisible();

    // Settings: Make a token.
    await page.getByRole('navigation').getByRole('button', { name: 'Settings', exact: true }).click();
    await page.getByPlaceholder('token name, e.g. backup-script').fill('tests-ui-viewer-token');
    await page.getByRole('button', { name: 'Make a token' }).click();
    const tokenErr = page.locator('.error', { hasText: 'viewers read' });
    await expect(tokenErr).toBeVisible();
    await page.waitForTimeout(16_000);
    await expect(tokenErr).toBeVisible();
  } finally {
    await hub.api('DELETE', `/api/users/${name}`);
    await hub.api('DELETE', `/api/tasks/${task.id}`);
  }
  expect(errors, 'no uncaught error across the viewer sweep').toEqual([]);
});

test('a 502 on /api/hosts shows the error, not the empty state', async ({ page, account }) => {
  // X-2: every list tab's {#if list.length === 0} used to ignore a failed
  // read, so a 502 showed "No machines enrolled" right beside the error —
  // Hosts.svelte now only reaches the empty state when loadError is unset.
  await page.getByRole('navigation').getByRole('button', { name: 'Events', exact: true }).click();
  await page.route('**/api/hosts', (route) => route.fulfill({
    status: 502,
    contentType: 'text/plain',
    body: 'dial tcp: connection refused',
  }));
  await page.getByRole('navigation').getByRole('button', { name: 'Hosts', exact: true }).click();
  await expect(page.locator('.error', { hasText: 'connection refused' })).toBeVisible();
  await expect(page.getByText('No machines enrolled')).toHaveCount(0);
});

test('saving a host\'s rebuild script from the card is byte-exact', async ({ page, hub, account }) => {
  // H-10 (PR 5): Hosts.svelte's Save script button used to `put(path,
  // script)`, which api.js JSON.stringifies — the store ended up
  // holding the script quoted, with every newline escaped. It now sends
  // { script }; hosts.md: "a save is byte-exact".
  const { body: hosts } = await hub.api('GET', '/api/hosts');
  test.skip(hosts.length === 0, 'no hosts enrolled');
  const h = hosts[0];
  const before = h.rebuildScript;
  // A browser textarea normalizes \r\n to \n on the way into `.value`, so
  // CRLF is out of scope here — tests/hosts_test.sh covers that over the
  // raw API. A real newline, a quote and a literal \n are what a
  // textarea actually hands back.
  const text = 'edited from the panel\nwith a "quote" and a literal \\n\nand a real newline';
  try {
    await page.getByRole('navigation').getByRole('button', { name: 'Hosts', exact: true }).click();
    const card = page.locator('section.card.plate').filter({ hasText: h.name }).first();
    await card.getByRole('button', { name: 'More' }).click();
    const textarea = card.locator('textarea');
    await textarea.fill(text);
    await card.getByRole('button', { name: 'Save script' }).click();
    await expect(textarea).toHaveValue(text);
    await expect.poll(async () => {
      const { body } = await hub.api('GET', '/api/hosts');
      return body.find((x) => x.id === h.id)?.rebuildScript;
    }).toBe(text);
  } finally {
    await hub.api('PUT', `/api/hosts/${h.id}/rebuild-script`, { script: before });
  }
});

test('at 390px no tab widens the page itself', async ({ page, account }) => {
  // X-9: Network, Storage, Apps, Agents and Events overflowed the page at
  // a phone width — every table now sits in the same .scroll wrapper
  // Peers/Models already used, so the table scrolls sideways and the
  // document does not.
  await page.setViewportSize({ width: 390, height: 844 });
  for (const label of tabs) {
    await page.getByRole('navigation').getByRole('button', { name: label, exact: true }).click();
    await expect(page.locator('main')).toBeVisible();
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth === window.innerWidth),
      { message: `${label} overflows the page at 390px` }).toBe(true);
  }
});

test('Escape closes the New remote card', async ({ page, account }) => {
  // X-11: inline "dialogs" are cards, not <dialog>s, and Escape used to
  // dismiss none of them.
  await page.getByRole('navigation').getByRole('button', { name: 'Hosts', exact: true }).click();
  await page.getByRole('button', { name: 'New remote' }).click();
  const card = page.locator('section.card.dialog');
  await expect(card).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(card).toBeHidden();
});

test('#Hosts, capitalised, still opens Hosts', async ({ page, account }) => {
  // X-5: routing used to match the hash exactly, so anything but the
  // lowercase tab id landed on "There is no such page".
  await page.goto('/#Hosts');
  await expect(page.getByRole('navigation').getByRole('button', { name: 'Hosts', exact: true })).toHaveClass(/active/);
  await expect(page.getByText('There is no such page')).toHaveCount(0);
});

test('the "Run peers\' jobs" checkbox is checked when the stored value is true', async ({ page, hub, account }) => {
  // C-11: the checkbox used to test `=== '1'`, so the canonical "true"
  // the API stores (and this same PUT now always writes) showed as off.
  const before = (await hub.api('GET', '/api/settings')).body['space.serve'];
  await hub.api('PUT', '/api/settings', { 'space.serve': 'true' });
  try {
    await page.getByRole('navigation').getByRole('button', { name: 'Settings', exact: true }).click();
    const box = page.getByLabel(/Run peers' jobs on my machines/);
    await expect(box).toBeChecked();
  } finally {
    await hub.api('PUT', '/api/settings', { 'space.serve': before ?? '' });
  }
});

test('the model pickers save provider and id as one provider/id setting', async ({ page, hub, account }) => {
  // hub/internal/agent/README.md, "Providers and models": an id alone
  // does not say who runs it (openai/gpt-4o-mini is OpenRouter's tag),
  // so the picker is a provider from omp's list plus the id, saved as
  // "provider/id". The providers come from GET /api/agents/models.
  const before = (await hub.api('GET', '/api/settings')).body['agent.remote_model'];
  const providers = (await hub.api('GET', '/api/agents/models')).body;
  test.skip(!Array.isArray(providers) || !providers.some((p) => p.name === 'openrouter'), 'the vault has no openrouter key');
  try {
    await page.getByRole('navigation').getByRole('button', { name: 'Settings', exact: true }).click();
    const remote = page.locator('label', { hasText: 'Remote model' });
    await remote.locator('select').selectOption('openrouter');
    await remote.locator('input').fill('openai/gpt-4o-mini');
    await page.locator('#s-agents button[type=submit]').click();
    await expect(page.locator('#s-agents').getByText('Saved')).toBeVisible();
    const after = (await hub.api('GET', '/api/settings')).body['agent.remote_model'];
    expect(after).toBe('openrouter/openai/gpt-4o-mini');
    await page.reload();
    await page.getByRole('navigation').getByRole('button', { name: 'Settings', exact: true }).click();
    await expect(remote.locator('select')).toHaveValue('openrouter');
    await expect(remote.locator('input')).toHaveValue('openai/gpt-4o-mini');
  } finally {
    await hub.api('PUT', '/api/settings', { 'agent.remote_model': before ?? '' });
  }
});
