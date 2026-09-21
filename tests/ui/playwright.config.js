import { defineConfig } from '@playwright/test';
import { PORT } from './lab.js';

// One hub, one browser, one test at a time: the tests share the hub's
// account table and the tunnel, and a passkey ceremony is not something
// to race. globalSetup opens the tunnel; the port is fixed because the
// hub only accepts passkeys from http://localhost:7433.
export default defineConfig({
  testDir: '.',
  testMatch: /.*\.spec\.js/,
  workers: 1,
  fullyParallel: false,
  timeout: 90_000,
  expect: { timeout: 15_000 },
  reporter: 'list',
  globalSetup: './lab.js',
  use: {
    baseURL: `http://localhost:${PORT}`,
    browserName: 'chromium',
    trace: 'retain-on-failure',
  },
});
