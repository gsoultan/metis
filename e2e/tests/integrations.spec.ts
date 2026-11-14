import type { Page } from '@playwright/test';

import { expect, test, uniqueName } from '../support/test';

/**
 * Whether the admin may see the installation-wide pages.
 *
 * With one organization its administrators run the installation. Once there
 * is a second, the server settings and upgrades are for platform
 * administrators only (METIS_PLATFORM_ADMINS), and the pages say so instead.
 * The administration spec adds organizations, so which of the two holds
 * depends on what ran before; each test asserts the one that applies.
 */
async function installationWide(api: import('@playwright/test').APIRequestContext): Promise<boolean> {
  const response = await api.get('/api/v1/organizations');
  expect(response.ok()).toBe(true);
  const { organizations } = (await response.json()) as { organizations: unknown[] };
  return organizations.length === 1;
}

/** Connectors, webhooks both ways, server settings, upgrades and the SDK sandbox. */

function catalogueCard(page: Page, connector: string) {
  return page
    .getByRole('main')
    .getByText(connector, { exact: true })
    .locator('xpath=ancestor::*[.//button[normalize-space()="Connect"]][1]');
}

test.describe('connectors', () => {
  test('the catalogue lists the built-in connectors and can be searched', async ({ page }) => {
    await page.goto('/connectors');
    for (const name of ['SMTP Email', 'Firebase Cloud Messaging', 'HTTP JSON Connector', 'Slack Connector']) {
      await expect(page.getByRole('main').getByText(name, { exact: true })).toBeVisible();
    }
    await page.getByRole('textbox', { name: 'Search the catalogue' }).fill('slack');
    await expect(page.getByRole('main').getByText('Slack Connector', { exact: true })).toBeVisible();
    await expect(page.getByRole('main').getByText('SMTP Email', { exact: true })).toBeHidden();
  });

  test('connecting a service walks through introduction, configuration and verification', async ({ page }) => {
    await page.goto('/connectors');
    await catalogueCard(page, 'SMTP Email').getByRole('button', { name: 'Connect' }).click();
    const setup = page.getByRole('dialog', { name: 'Set up SMTP Email' });
    await expect(setup.getByRole('button', { name: /1 Introduction/ })).toBeVisible();
    await expect(setup.getByRole('alert')).toContainText('Once saved they are not shown again');
    await setup.getByRole('button', { name: 'Start configuration' }).click();
    await expect(setup.getByRole('textbox', { name: 'Friendly name' })).toHaveValue('SMTP Email Instance');
    for (const field of ['SMTP Host', 'SMTP Port', 'Username', 'Password']) {
      await expect(setup.getByRole('textbox', { name: field })).toBeVisible();
    }
    await setup.getByRole('button', { name: 'Close' }).click();
    await expect(setup).toBeHidden();
  });

  test('Firebase push notifications can be set up from the catalogue', async ({ page }) => {
    await page.goto('/connectors');
    await catalogueCard(page, 'Firebase Cloud Messaging').getByRole('button', { name: 'Connect' }).click();
    const setup = page.getByRole('dialog', { name: 'Set up Firebase Cloud Messaging' });
    await expect(setup).toBeVisible();
    await setup.getByRole('button', { name: 'Start configuration' }).click();
    await expect(setup.getByRole('button', { name: /2 Configuration/ })).toBeVisible();
    await page.keyboard.press('Escape');
  });

  test('a connector manifest can be installed from the example', async ({ page }) => {
    await page.goto('/connectors');
    await page.getByRole('button', { name: 'Install' }).click();
    const dialog = page.getByRole('dialog', { name: 'Install a connector' });
    await expect(dialog.getByRole('button', { name: 'Install' })).toBeDisabled();
    await dialog.getByRole('button', { name: 'Start from an example' }).click();
    await expect(dialog.getByRole('textbox', { name: 'The document to install' })).not.toHaveValue('');
    await expect(dialog.getByRole('button', { name: 'Install' })).toBeEnabled();
    await dialog.getByRole('button', { name: 'Cancel' }).click();
  });
});

test.describe('webhooks', () => {
  test('an outgoing webhook is created for the events chosen', async ({ page }) => {
    const name = uniqueName('E2E ledger');
    await page.goto('/connectors');
    await page.getByRole('button', { name: 'New outgoing webhook' }).click();
    const dialog = page.getByRole('dialog', { name: 'New outgoing webhook' });
    await dialog.getByRole('textbox', { name: 'Name' }).fill(name);
    await dialog.getByRole('textbox', { name: 'Where to send it' }).fill('https://ledger.example.com/hooks/metis');
    await dialog.getByRole('checkbox', { name: 'A process finishes' }).check();
    await dialog.getByRole('button', { name: 'Create webhook' }).click();

    // The signing secret is shown once, on creation.
    const created = page.getByRole('dialog').filter({ hasText: /secret/i });
    if (await created.isVisible().catch(() => false)) {
      await created.getByRole('button').last().click();
    }
    await expect(page.getByRole('main').getByText(name)).toBeVisible();
  });

  test('an outgoing webhook to a plain http address is refused', async ({ page }) => {
    await page.goto('/connectors');
    await page.getByRole('button', { name: 'New outgoing webhook' }).click();
    const dialog = page.getByRole('dialog', { name: 'New outgoing webhook' });
    await dialog.getByRole('textbox', { name: 'Name' }).fill('Not secure');
    await dialog.getByRole('textbox', { name: 'Where to send it' }).fill('http://ledger.example.com/hooks');
    await dialog.getByRole('checkbox', { name: 'A process starts' }).check();
    const create = dialog.getByRole('button', { name: 'Create webhook' });
    if (await create.isEnabled()) await create.click();
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(/https/).first()).toBeVisible();
  });

  test('an incoming webhook gets an address a partner can post to', async ({ page }) => {
    const name = uniqueName('E2E payments');
    await page.goto('/connectors');
    await page.getByRole('button', { name: 'Add a webhook' }).click();
    const dialog = page.getByRole('dialog', { name: 'Add a webhook' });
    await dialog.getByRole('textbox', { name: 'Name' }).fill(name);
    await dialog.getByRole('textbox', { name: 'Becomes the message' }).fill('payment.received');
    await dialog.getByRole('textbox', { name: 'Which process is it about?' }).fill('order.id');
    await dialog.getByRole('button', { name: 'Create' }).click();

    const created = page.getByRole('dialog').filter({ hasText: /secret/i });
    if (await created.isVisible().catch(() => false)) {
      await created.getByRole('button').last().click();
    }
    await expect(page.getByRole('main').getByText(name)).toBeVisible();
  });
});

test.describe('settings', () => {
  test('every server-settings section opens and says where each value comes from', async ({ page, api }) => {
    await page.goto('/settings');
    if (!(await installationWide(api))) {
      await expect(page.getByRole('alert', { name: 'Server settings' })).toContainText('METIS_PLATFORM_ADMINS');
      return;
    }
    for (const section of ['Email and notifications', 'Sign-in', 'Retention', 'Workers', 'Limits', 'Caches', 'Observability']) {
      await page.getByRole('button', { name: section, exact: true }).click();
      const region = page.getByRole('region', { name: section });
      await expect(region).toBeVisible();
      await expect(region.getByRole('button', { name: /^Change / }).first()).toBeVisible();
    }
  });

  test('expert mode is a switch that is remembered', async ({ page }) => {
    await page.goto('/settings');
    const expert = page.getByRole('switch', { name: 'Expert mode' });
    const before = await expert.isChecked();
    await expert.click({ force: true });
    await expect(expert).toBeChecked({ checked: !before });
    await page.reload();
    await expect(page.getByRole('switch', { name: 'Expert mode' })).toBeChecked({ checked: !before });
    await page.getByRole('switch', { name: 'Expert mode' }).click({ force: true });
    await expect(page.getByRole('switch', { name: 'Expert mode' })).toBeChecked({ checked: before });
  });

  test('environments are offered, and the project runs on the main database until one is added', async ({ page }) => {
    await page.goto('/settings');
    await expect(page.getByText('No environments yet.', { exact: false })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Add environment' })).toBeEnabled();
  });
});

test.describe('upgrades', () => {
  test('says what is running and checks for a newer release on request', async ({ page, api }) => {
    await page.goto('/upgrades');
    if (!(await installationWide(api))) {
      await expect(page.getByRole('alert', { name: 'The upgrade status could not be read' })).toContainText('METIS_PLATFORM_ADMINS');
      return;
    }
    await expect(page.getByText('Running', { exact: true })).toBeVisible();
    await expect(page.getByText(/Database schema \d+/)).toBeVisible();
    await page.getByRole('button', { name: 'Check now' }).click();
    await expect(page.getByText('Last checked')).toBeVisible();
    // A build that is not from a release tag is never offered an upgrade.
    await expect(page.getByText('This build is not from a release tag, so it is never offered an upgrade.')).toBeVisible();
  });
});

test.describe('the SDK sandbox', () => {
  test('opens with something to try', async ({ page }) => {
    await page.goto('/sdk');
    await expect(page.getByRole('main').getByRole('heading', { level: 1 })).toContainText('SDK');
    await expect(page.getByRole('main').getByRole('button').first()).toBeVisible();
  });
});
