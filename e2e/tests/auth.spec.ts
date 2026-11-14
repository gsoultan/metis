import { ADMIN_USER, AUTH_STORAGE_KEY, SAMPLE_PASSWORD } from '../support/env';
import { expect, signInThroughPage, test } from '../support/test';

// Each test here signs in on its own.
test.use({ storageState: { cookies: [], origins: [] } });

// Signing out ends every session the account has, on every device, so the
// tests that sign out do it as a sample account nothing else signs in as.
// Done as the admin, it would sign every later spec out with it.
const SIGN_OUT_USER = 'reviewer';

// Sign-in opens the first organization the account belongs to, by name. Other
// specs create organizations the admin joins, so the admin can land in an empty
// one; the sample's manager belongs to Example Co alone, wherever it runs.
const SIGN_IN_USER = 'manager';

test.describe('signing in', () => {
  test('a wrong password is refused and stays on the sign-in page', async ({ page }) => {
    await page.goto('/login');
    await page.getByRole('textbox', { name: 'Username' }).fill(ADMIN_USER);
    await page.getByRole('textbox', { name: 'Password' }).fill('not-the-password');
    await page.getByRole('button', { name: 'Sign in' }).click();

    await expect(page.getByRole('alert')).toBeVisible();
    await expect(page).toHaveURL(/\/login/);
  });

  test('the right password opens the dashboard as that account', async ({ page }) => {
    await signInThroughPage(page, SIGN_IN_USER, SAMPLE_PASSWORD);

    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Account menu' })).toContainText(SIGN_IN_USER);
  });

  test('a page asked for while signed out is where sign-in returns to', async ({ page }) => {
    await page.goto('/instances');
    await expect(page).toHaveURL(/\/login\?redirect=/);

    await page.getByRole('textbox', { name: 'Username' }).fill(SIGN_IN_USER);
    await page.getByRole('textbox', { name: 'Password' }).fill(SAMPLE_PASSWORD);
    await page.getByRole('button', { name: 'Sign in' }).click();

    await expect(page).toHaveURL(/\/instances/);
    await expect(page.getByRole('heading', { level: 1, name: 'Process Instances' })).toBeVisible();
  });

  test('the password can be shown and hidden', async ({ page }) => {
    await page.goto('/login');
    const password = page.getByRole('textbox', { name: 'Password' });
    await expect(password).toHaveAttribute('type', 'password');
    await page.getByRole('button', { name: 'Toggle password visibility' }).click();
    await expect(password).toHaveAttribute('type', 'text');
  });
});

test.describe('ending a session', () => {
  test('signing out returns to sign-in, and the old token no longer works', async ({ page, request }) => {
    await signInThroughPage(page, SIGN_OUT_USER, SAMPLE_PASSWORD);
    const token = await storedToken(page);

    await page.getByRole('button', { name: 'Account menu' }).click();
    await page.getByRole('menuitem', { name: 'Sign out' }).click();
    await expect(page).toHaveURL(/\/login/);

    // The server was told, so the token is dead everywhere, not just in this tab.
    await expect
      .poll(async () => (await request.get('/api/v1/organizations', { headers: { Authorization: `Bearer ${token}` } })).status())
      .toBe(401);

    await page.goto('/instances');
    await expect(page).toHaveURL(/\/login/);
  });

  test('a token the server stops accepting sends the person to sign in', async ({ page }) => {
    await signInThroughPage(page, SIGN_OUT_USER, SAMPLE_PASSWORD);
    // Once the app has settled: it writes the same storage while it chooses an
    // organization, and would put the good token back.
    await expect(page.getByRole('button', { name: 'Organization: Example Co' })).toBeVisible();

    // An expired token and a revoked one look the same to the client: a 401.
    await page.evaluate((key) => {
      const raw = localStorage.getItem(key);
      if (!raw) throw new Error('no stored session');
      const stored = JSON.parse(raw) as { state: { token: string } };
      stored.state.token = `${stored.state.token.slice(0, -4)}AAAA`;
      localStorage.setItem(key, JSON.stringify(stored));
    }, AUTH_STORAGE_KEY);
    await page.reload();

    // reason=expired is what makes the sign-in page say why the person is there.
    await expect(page).toHaveURL(/\/login\?redirect=.*reason=expired/);
    await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
  });

  test('signing out in another tab signs this one out on its next request', async ({ page }) => {
    await signInThroughPage(page, SIGN_OUT_USER, SAMPLE_PASSWORD);
    await page.getByRole('link', { name: 'Organizations' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Organizations' })).toBeVisible();

    const other = await page.context().newPage();
    await other.goto('/');
    await other.getByRole('button', { name: 'Account menu' }).click();
    await other.getByRole('menuitem', { name: 'Sign out' }).click();
    await expect(other).toHaveURL(/\/login/);

    await page.getByRole('link', { name: 'Dashboard' }).click();
    await expect(page).toHaveURL(/\/login/);
  });
});

async function storedToken(page: import('@playwright/test').Page): Promise<string> {
  const token = await page.evaluate((key) => {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as { state: { token: string | null } }).state.token : null;
  }, AUTH_STORAGE_KEY);
  expect(token).toBeTruthy();
  return token!;
}
