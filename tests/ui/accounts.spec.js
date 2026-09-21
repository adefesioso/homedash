// docs/running/accounts.md — "Sign-in is passkeys": the first passkey is
// the admin, after that a one-time code; a shell on the hub prints one.
import { test, expect } from './fixtures.js';
import { BASE, invite } from './lab.js';
import { attachPasskey } from './passkey.js';

test('register with a one-time code, sign out, sign back in with the passkey', async ({ page, hub, registerAccount }) => {
  const name = 'tests-ui-' + Date.now().toString(36);
  try {
    await registerAccount(page, name);

    // The account exists on the hub, with its one passkey.
    const { body: users } = await hub.api('GET', '/api/users');
    const me = users.find((u) => u.name === name);
    expect(me, 'the registered account is in /api/users').toBeTruthy();
    expect(me.role).toBe('admin');
    expect(me.passkeys).toBe(1);

    // Sign out lands on the login card, not the panel.
    await page.getByRole('button', { name: 'sign out' }).click();
    const card = page.locator('section.card');
    await expect(card.getByRole('button', { name: 'Sign in' })).toBeVisible();

    // Sign in with the same (virtual) passkey: a discoverable-credential
    // login, no name typed.
    await card.getByRole('button', { name: 'Sign in' }).click();
    await expect(page.locator('header').first()).toContainText(`${name} · admin`);
  } finally {
    await hub.api('DELETE', `/api/users/${name}`);
  }
});

test('a wrong code is refused on the register screen', async ({ page, hub }) => {
  const { body: state } = await hub.api('GET', '/api/auth/state');
  test.skip(state.setupOpen, 'no account on the hub yet: registration is open, no code is asked for');
  await page.goto('/');
  const card = page.locator('section.card');
  await card.getByRole('button', { name: 'I have a registration code' }).click();
  await card.getByLabel('Code').fill('not-a-code');
  await card.getByLabel('Your name').fill('tests-ui-nobody');
  await card.getByRole('button', { name: 'Register a passkey' }).click();
  await expect(card.locator('.error')).toBeVisible();
  await expect(page.locator('header').first()).toHaveCount(0);
});

test('a duplicate token name is refused with a visible message, not a crash', async ({ page, hub, account }) => {
  // C-6: `{#each tokens as t (t.name)}` used to crash the whole Settings
  // tab for every admin (each_key_duplicate) once a name was minted
  // twice; now the API refuses the second one and the tab still renders.
  const errors = [];
  page.on('pageerror', (e) => errors.push(e));
  const name = 'tests-ui-dup';
  try {
    await page.getByRole('navigation').getByRole('button', { name: 'Settings', exact: true }).click();
    const input = page.getByPlaceholder('token name, e.g. backup-script');
    const make = page.getByRole('button', { name: 'Make a token' });
    await input.fill(name);
    await make.click();
    await expect(page.locator('pre').first()).toBeVisible();
    await input.fill(name);
    await make.click();
    // A fresh test account has one passkey, so the "add a second
    // passkey" warning is also a `.error` — scope to the one that names
    // this token.
    await expect(page.locator('.error').filter({ hasText: name })).toContainText(`${name} exists`);
    await expect(page.getByRole('heading', { name: 'Accounts' })).toBeVisible();
    expect(errors, 'no uncaught error from the duplicate name').toEqual([]);
  } finally {
    await hub.api('DELETE', `/api/tokens/${name}`);
  }
});

test('a used code does not work twice', async ({ page, hub }) => {
  const { body: state } = await hub.api('GET', '/api/auth/state');
  test.skip(state.setupOpen, 'no account on the hub yet: registration is open, no code is asked for');
  const name = 'tests-ui-once-' + Date.now().toString(36);
  const code = await invite('viewer');
  try {
    // Spend it the real way: a passkey registered against it.
    await attachPasskey(page);
    await page.goto('/');
    const card = page.locator('section.card');
    await card.getByRole('button', { name: 'I have a registration code' }).click();
    await card.getByLabel('Code').fill(code);
    await card.getByLabel('Your name').fill(name);
    await card.getByRole('button', { name: 'Register a passkey' }).click();
    await expect(page.locator('header').first()).toContainText(`${name} · viewer`);
    // A second account on the same code is refused before any ceremony.
    const again = await fetch(`${BASE}/api/auth/register/begin`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: name + '-twice', invite: code }),
    });
    expect(again.status).toBe(400);
  } finally {
    await hub.api('DELETE', `/api/users/${name}`);
  }
});
