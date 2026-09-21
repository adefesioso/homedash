// Test fixtures. `hub` is an admin API client outside the browser, on a
// token named tests-ui that is deleted after the test; `account` is a
// signed-in panel — registered through the real ceremony with a fresh
// passkey and a one-time code — whose user is deleted afterwards.
import { test as base, expect } from '@playwright/test';
import { api, invite, token } from './lab.js';
import { attachPasskey } from './passkey.js';

export const test = base.extend({
  hub: async ({}, use) => {
    const tok = await token('tests-ui');
    await use({ api: (method, p, body) => api(tok, method, p, body) });
    await api(tok, 'DELETE', '/api/tokens/tests-ui');
  },

  // registerAccount(page, name, role) walks the register screen. With no
  // account on the hub yet the first passkey is the admin and needs no
  // code; otherwise the shell prints one for the role.
  registerAccount: async ({ hub }, use) => {
    await use(async (page, name, role = 'admin') => {
      await attachPasskey(page);
      await page.goto('/');
      const card = page.locator('section.card');
      await expect(card.getByRole('heading', { name: 'HomeDash' })).toBeVisible();
      const { body: state } = await hub.api('GET', '/api/auth/state');
      if (!state.setupOpen) {
        await card.getByRole('button', { name: 'I have a registration code' }).click();
        await card.getByLabel('Code').fill(await invite(role));
      }
      await card.getByLabel('Your name').fill(name);
      await card.getByRole('button', { name: 'Register a passkey' }).click();
      await expect(page.locator('header').first()).toContainText(`${name} · ${role}`);
    });
  },

  account: async ({ page, hub, registerAccount }, use) => {
    const name = 'tests-ui-' + Date.now().toString(36);
    try {
      await registerAccount(page, name);
      await use({ name });
    } finally {
      // Also on a failed registration: the row exists from `begin` on.
      await hub.api('DELETE', `/api/users/${name}`);
    }
  },
});

export { expect };
