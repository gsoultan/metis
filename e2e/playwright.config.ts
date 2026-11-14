import { defineConfig, devices } from '@playwright/test';

import { ADMIN_STATE } from './support/env';

/**
 * End-to-end tests against a real, running Metis.
 *
 * Nothing here starts a server: scripts/e2e.sh builds the binary, starts it on
 * an empty PostgreSQL, seeds it with scripts/seed-sample.sh and then runs this.
 * Pointing E2E_BASE_URL at another seeded server works the same way.
 *
 * One worker. Every spec shares one database and the sample in it, and several
 * of them change it — complete a task, deploy a version — so running them side
 * by side would make each one's starting point depend on scheduling. Specs
 * create what they change under a name of their own, so the order between
 * files does not matter either.
 *
 * No retries. A test that passes on its second attempt is a flaky test, and a
 * retry turns that into a green run nobody looks at.
 */
const baseURL = process.env.E2E_BASE_URL ?? 'http://localhost:8373';
const onCI = Boolean(process.env.CI);

export default defineConfig({
  testDir: './tests',
  outputDir: './test-results',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: onCI,
  reporter: onCI
    ? [['github'], ['list'], ['html', { open: 'never', outputFolder: 'playwright-report' }]]
    : [['list'], ['html', { open: 'never', outputFolder: 'playwright-report' }]],
  use: {
    baseURL,
    viewport: { width: 1440, height: 900 },
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
    locale: 'en-GB',
    timezoneId: 'UTC',
  },
  projects: [
    { name: 'setup', testMatch: /auth\.setup\.ts/ },
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 }, storageState: ADMIN_STATE },
      dependencies: ['setup'],
    },
  ],
});
