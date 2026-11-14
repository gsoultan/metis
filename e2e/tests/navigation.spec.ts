import { expect, test } from '../support/test';

/**
 * Every page the main navigation offers opens, names itself, and does not
 * throw. The feature specs go deeper; this is the floor under all of them, so a
 * page that stops rendering is caught even where nothing else looks at it.
 */
const pages: Array<{ link: string; path: RegExp; heading: string }> = [
  { link: 'Dashboard', path: /\/$/, heading: 'Dashboard' },
  { link: 'My Inbox', path: /\/inbox/, heading: 'Task Inbox' },
  { link: 'All Tasks', path: /\/tasks/, heading: 'All Tasks' },
  { link: 'People', path: /\/people/, heading: 'People' },
  { link: 'Processes', path: /\/models/, heading: 'Processes' },
  { link: 'Decisions', path: /tab=decisions/, heading: 'Decisions' },
  { link: 'Connectors', path: /\/connectors/, heading: 'Connectors' },
  { link: 'SDK Sandbox', path: /\/sdk/, heading: 'SDK Sandbox' },
  { link: 'Instances', path: /\/instances/, heading: 'Process Instances' },
  { link: 'Archive', path: /\/archive/, heading: 'Archive' },
  { link: 'Releases', path: /\/releases/, heading: 'Releases' },
  { link: 'Projects', path: /\/projects/, heading: 'Projects' },
  { link: 'Organizations', path: /\/organizations/, heading: 'Organizations' },
  { link: 'Groups', path: /\/groups/, heading: 'Groups' },
  { link: 'Users', path: /\/users/, heading: 'Users' },
  { link: 'Audit log', path: /\/audit/, heading: 'Audit' },
  { link: 'Upgrades', path: /\/upgrades/, heading: 'Upgrades' },
];

test.describe('main navigation', () => {
  for (const { link, path, heading } of pages) {
    test(`${link} opens`, async ({ page }) => {
      await page.goto('/');
      await page.getByRole('navigation', { name: 'Main navigation' }).getByRole('link', { name: link, exact: true }).click();

      await expect(page).toHaveURL(path);
      await expect(page.getByRole('main').getByRole('heading', { level: 1 })).toContainText(heading);
      await expect(page.getByRole('main').getByText(/something went wrong/i)).toHaveCount(0);
    });
  }

  test('the account menu reaches Profile and Settings', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Account menu' }).click();
    await page.getByRole('menuitem', { name: 'Profile', exact: true }).click();
    await expect(page).toHaveURL(/\/profile/);

    await page.getByRole('button', { name: 'Account menu' }).click();
    await page.getByRole('menuitem', { name: 'Settings', exact: true }).click();
    await expect(page).toHaveURL(/\/settings/);
    await expect(page.getByRole('heading', { level: 1, name: 'Application Settings' })).toBeVisible();
  });

  test('an address that does not exist says so instead of a blank page', async ({ page }) => {
    await page.goto('/no-such-page');
    await expect(page.getByText(/not found|doesn.t exist|does not exist/i).first()).toBeVisible();
  });

  test('the theme switch changes the colour scheme and remembers it', async ({ page }) => {
    await page.goto('/');
    const html = page.locator('html');
    const before = await html.getAttribute('data-mantine-color-scheme');
    await page.getByRole('button', { name: /Switch to (dark|light) theme/ }).click();
    await expect(html).not.toHaveAttribute('data-mantine-color-scheme', before ?? '');
    const after = await html.getAttribute('data-mantine-color-scheme');
    await page.reload();
    await expect(html).toHaveAttribute('data-mantine-color-scheme', after ?? '');
    // Put it back, so later screenshots are taken in the theme they always were.
    await page.getByRole('button', { name: /Switch to (dark|light) theme/ }).click();
  });

  test('the help drawer opens and closes', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Open help' }).click();
    const drawer = page.getByRole('dialog');
    await expect(drawer).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(drawer).toBeHidden();
  });
});
