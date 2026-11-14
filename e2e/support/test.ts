import { test as base, expect, type APIRequestContext, type Page } from '@playwright/test';

import { ADMIN_PASSWORD, ADMIN_USER } from './env';

/** What the API answers a sign-in with: enough to call it as that account. */
export interface Session {
  token: string;
  userId: string;
}

interface Fixtures {
  /** Fails the test on any uncaught exception in the page. */
  failOnPageError: void;
  /** The admin's session, for setting a test's scene up through the API. */
  adminSession: Session;
  /** An API client signed in as the admin. */
  api: APIRequestContext;
}

export const test = base.extend<Fixtures>({
  failOnPageError: [
    async ({ page }, use) => {
      const errors: string[] = [];
      page.on('pageerror', (error) => errors.push(error.stack ?? error.message));
      await use();
      expect(errors, 'uncaught exceptions in the page').toEqual([]);
    },
    { auto: true },
  ],

  adminSession: async ({ playwright, baseURL }, use) => {
    await use(await apiSignIn(playwright.request, baseURL!, ADMIN_USER, ADMIN_PASSWORD));
  },

  api: async ({ playwright, baseURL, adminSession }, use) => {
    const context = await playwright.request.newContext({
      baseURL,
      extraHTTPHeaders: { Authorization: `Bearer ${adminSession.token}` },
    });
    await use(context);
    await context.dispose();
  },
});

export { expect };

/** Signs in through the REST API, as scripts/seed-sample.sh does. */
export async function apiSignIn(
  request: { newContext: (options: { baseURL: string }) => Promise<APIRequestContext> },
  baseURL: string,
  username: string,
  password: string,
): Promise<Session> {
  const context = await request.newContext({ baseURL });
  try {
    const response = await context.post('/api/v1/login', { data: { username, password } });
    expect(response.ok(), `sign-in as ${username}: ${response.status()}`).toBe(true);
    const body = (await response.json()) as { token?: string; user?: { id?: string } };
    expect(body.token, `sign-in as ${username} returned no token`).toBeTruthy();
    return { token: body.token!, userId: body.user?.id ?? '' };
  } finally {
    await context.dispose();
  }
}

/** Signs in through the sign-in page, as a person would. */
export async function signInThroughPage(page: Page, username: string, password: string): Promise<void> {
  await page.goto('/login');
  await page.getByRole('textbox', { name: 'Username' }).fill(username);
  await page.getByRole('textbox', { name: 'Password' }).fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).not.toHaveURL(/\/login/);
}

/** A name nothing else in the database has, so a spec can find what it made. */
export function uniqueName(prefix: string): string {
  return `${prefix} ${Date.now().toString(36)}${Math.floor(Math.random() * 1296).toString(36)}`;
}

/**
 * Marks a test as covering behaviour the UI does not have yet.
 *
 * The test is written for what the product should do and still runs: it is
 * expected to fail, so the run stays green while the bug is open. When the fix
 * lands the test starts passing, Playwright reports that as a failure, and
 * whoever fixed it deletes this line. That is the point — a known bug that is
 * asserted as if it were the intended behaviour would turn the fix red instead.
 */
export function knownBug(finding: string): void {
  test.info().annotations.push({ type: 'known bug', description: finding });
  test.fail(true, finding);
}
