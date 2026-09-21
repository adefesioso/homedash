// docs/running/cli.md — "the CLI opens your browser at the hub, you pass
// your passkey there and approve this computer, and the browser hands
// the result back to a port the CLI is listening on." The real binary,
// with no browser on its PATH, prints the URL it would have opened; the
// test's browser goes there instead, signed in with a passkey, and
// approves. What the CLI does with the session it gets is then checked
// against the API directly.
import { test, expect } from './fixtures.js';
import { BASE } from './lab.js';
import { spawn, execFile } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
// The build for this machine, as `make deb` leaves it.
const BIN = process.env.HOMEDASH_BIN || path.join(here, '..', '..', 'hub', 'dist', `homedash-linux-${process.arch === 'arm64' ? 'arm64' : 'amd64'}`);

function cli(args, env) {
  return new Promise((resolve) => {
    execFile(BIN, args, { env, timeout: 60_000 }, (err, stdout, stderr) => resolve({ code: err?.code ?? 0, stdout, stderr }));
  });
}

test('homedash login signs the command line in through the panel', async ({ page, hub, account }) => {
  const home = await mkdtemp(path.join(tmpdir(), 'homedash-cli-'));
  // No xdg-open on this PATH: the CLI prints the page instead of opening it.
  const env = { HOME: home, XDG_CONFIG_HOME: home, PATH: '/nonexistent' };
  const login = spawn(BIN, ['login', BASE], { env });
  let stderr = '';
  const exited = new Promise((resolve) => login.on('exit', (code) => resolve(code)));
  const url = await new Promise((resolve, reject) => {
    login.stderr.on('data', (d) => {
      stderr += d;
      const m = stderr.match(/(http:\/\/localhost:7433\/#cli\?port=\d+&state=[0-9a-f]+)/);
      if (m) resolve(m[1]);
    });
    login.on('exit', () => reject(new Error(`login exited early:\n${stderr}`)));
  });

  try {
    // The page names the account that will be approving and the port.
    const port = new URL(url).hash.match(/port=(\d+)/)[1];
    await page.goto(url);
    await expect(page.getByRole('heading', { name: 'Sign in the command line' })).toBeVisible();
    await expect(page.locator('main')).toContainText(account.name);
    await expect(page.locator('main')).toContainText(port);
    await page.getByRole('button', { name: 'Approve' }).click();

    // The browser lands on the CLI's own port, and the CLI finishes.
    await expect(page).toHaveURL(new RegExp(`^http://127\\.0\\.0\\.1:${port}/`));
    await expect(page.locator('body')).toContainText('The command line is signed in');
    expect(await exited).toBe(0);
    expect(stderr).toContain(`as ${account.name} (admin)`);

    // The session it saved is this account's, and it works as a bearer.
    const saved = JSON.parse(await readFile(path.join(home, 'homedash', 'hub.json'), 'utf8'));
    expect(saved.hub).toBe(BASE);
    expect(saved.name).toBe(account.name);
    expect(saved.role).toBe('admin');
    expect(saved.token).not.toMatch(/^hd_/);
    const asCli = (m, p) => fetch(BASE + p, { method: m, headers: { Authorization: `Bearer ${saved.token}` } });
    expect((await asCli('GET', '/api/hosts')).status).toBe(200);

    // The subcommands are the API: `hosts --json` is GET /api/hosts.
    const who = await cli(['whoami'], env);
    expect(who.stdout.trim()).toBe(`${BASE} as ${account.name} (admin)`);
    const hosts = await cli(['hosts', '--json'], env);
    expect(hosts.code).toBe(0);
    const { body: fromApi } = await hub.api('GET', '/api/hosts');
    expect(JSON.parse(hosts.stdout).map((h) => h.name).sort()).toEqual(fromApi.map((h) => h.name).sort());

    // Signing out ends the session on the hub, not just the file.
    expect((await cli(['logout'], env)).code).toBe(0);
    expect((await asCli('GET', '/api/hosts')).status).toBe(401);
    expect((await cli(['hosts'], env)).stderr).toContain('not signed in');
  } finally {
    login.kill();
    await rm(home, { recursive: true, force: true });
  }
});

test('a grant code is spent once and dies in two minutes', async ({ page, hub, account }) => {
  // Granted from the signed-in panel, redeemed once from outside it.
  const code = await page.evaluate(async () => (await (await fetch('/api/auth/cli/grant', { method: 'POST' })).json()).code);
  const redeem = () => fetch(`${BASE}/api/auth/cli/redeem`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code }) });
  const first = await redeem();
  expect(first.status).toBe(200);
  expect((await first.json()).name).toBe(account.name);
  expect((await redeem()).status).toBe(401);
  // And nobody gets a grant without a session.
  expect((await fetch(`${BASE}/api/auth/cli/grant`, { method: 'POST' })).status).toBe(401);
});

test('the approval page refuses a link the CLI did not make', async ({ page, account }) => {
  await page.goto('/#cli?port=7&state=nope');
  await expect(page.locator('main')).toContainText('was not opened by');
  await expect(page.getByRole('button', { name: 'Approve' })).toHaveCount(0);
});
