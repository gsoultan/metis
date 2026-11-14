import type { Page } from '@playwright/test';

import { expect, knownBug, test } from '../support/test';

/** Narrows the list to one process: the specs' own runs would otherwise push the sample's off the first page. */
async function showOnly(page: Page, process: string): Promise<void> {
  const filter = page.getByRole('combobox', { name: 'Show only one process' });
  await filter.click();
  await filter.fill(process);
  await page.getByRole('listbox', { name: 'Show only one process' }).getByRole('option', { name: process, exact: true }).first().click();
  await expect(page.getByRole('row', { name: new RegExp(`^${process} #`) }).first()).toBeVisible();
}

/** The instance list, the dashboard's view of the same runs, and opening one. */
test.describe('instances', () => {
  test('lists every run with its process, status and when it started', async ({ page }) => {
    await page.goto('/instances');
    await expect(page.getByText(/^Live/)).toBeVisible();
    await showOnly(page, 'Expense approval');
    // The sample starts four expenses and finishes the small one at once.
    await expect(page.getByRole('row', { name: /^Expense approval #.* Running/ })).toHaveCount(3);
    await expect(page.getByRole('row', { name: /^Expense approval #.* Completed/ })).toHaveCount(1);
  });

  test('the process filter offers each process once', async ({ page }) => {
    knownBug('"Show only one process" lists a process once per deployed version');
    await page.goto('/instances');
    await page.getByRole('combobox', { name: 'Show only one process' }).click();
    const options = await page.getByRole('listbox', { name: 'Show only one process' }).getByRole('option').allInnerTexts();
    expect(options.length).toBe(new Set(options).size);
  });

  test('filtering by state shows only that state', async ({ page }) => {
    await page.goto('/instances');
    const states = page.getByRole('radiogroup', { name: 'Filter instances by state' });

    await states.getByText('Completed', { exact: true }).click();
    await expect(page.getByRole('row', { name: / Completed / }).first()).toBeVisible();
    await expect(page.getByRole('row', { name: / Running / })).toHaveCount(0);

    await states.getByText('Running', { exact: true }).click();
    await expect(page.getByRole('row', { name: / Running / }).first()).toBeVisible();
    await expect(page.getByRole('row', { name: / Completed / })).toHaveCount(0);
  });

  test('filtering by process shows only its runs', async ({ page }) => {
    await page.goto('/instances');
    await showOnly(page, 'New supplier check');
    await expect(page.getByRole('row', { name: /^New supplier check #/ }).first()).toBeVisible();
    await expect(page.getByRole('row', { name: /^Expense approval #/ })).toHaveCount(0);
  });

  test('live updates can be paused and resumed', async ({ page }) => {
    await page.goto('/instances');
    await page.getByRole('button', { name: 'Pause live updates' }).click();
    await expect(page.getByRole('button', { name: /Resume live updates/ })).toBeVisible();
    await page.getByRole('button', { name: /Resume live updates/ }).click();
    await expect(page.getByRole('button', { name: 'Pause live updates' })).toBeVisible();
  });

  test('"Where it is" names the step as the designer does', async ({ page }) => {
    knownBug('UI/UX audit #5: "Where it is" shows a humanised step id ("Ask director"), not the step\'s name');
    await page.goto('/instances');
    await showOnly(page, 'Expense approval');
    await expect(page.getByRole('row', { name: /Director approves/ }).first()).toBeVisible({ timeout: 3_000 });
  });

  test('opening a run shows that run, read-only, not the process editor', async ({ page }) => {
    knownBug('UI/UX audit #2: an instance opens the full process editor, with Deploy');
    await page.goto('/instances');
    await showOnly(page, 'New supplier check');
    await page.getByRole('button', { name: /^New supplier check #/ }).first().click();

    await expect(page.getByRole('button', { name: 'Deploy', exact: true })).toHaveCount(0, { timeout: 3_000 });
    await expect(page.getByText('Running', { exact: true }).first()).toBeVisible({ timeout: 3_000 });
  });

  test('opening a run shows its process and where it is', async ({ page }) => {
    await page.goto('/instances');
    await showOnly(page, 'Expense approval');
    await page.getByRole('button', { name: /^Expense approval #/ }).first().click();
    await expect(page.getByRole('application')).toBeVisible();
    await expect(page.getByRole('application').getByText('Who approves?')).toBeVisible();
  });
});

test.describe('the dashboard', () => {
  test('counts what is running and what is deployed', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByText('Active Instances')).toBeVisible();
    await expect(page.getByText('Process Models')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Where work is waiting' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Business Timeline' })).toBeVisible();
  });

  test('"Where work is waiting" names steps as the designer does', async ({ page }) => {
    knownBug('UI/UX audit #5: the dashboard says "Ask director" for the step named "Director approves"');
    await page.goto('/');
    await expect(page.getByRole('progressbar', { name: /waiting on Director approves/ })).toBeVisible({ timeout: 3_000 });
  });

  test('the getting-started card can be hidden', async ({ page }) => {
    await page.goto('/');
    const card = page.getByRole('region', { name: 'Getting started' });
    await expect(card).toBeVisible();
    await expect(card.getByRole('progressbar')).toBeVisible();
  });

  test('the waiting list can be exported as CSV', async ({ page }) => {
    await page.goto('/');
    const section = page.getByRole('heading', { name: 'Where work is waiting' }).locator('xpath=ancestor::*[.//button[normalize-space()="Export CSV"]][1]');
    const download = page.waitForEvent('download');
    await section.getByRole('button', { name: 'Export CSV' }).click();
    expect((await download).suggestedFilename()).toMatch(/\.csv$/);
  });
});

test.describe('history', () => {
  test('the archive opens', async ({ page }) => {
    await page.goto('/archive');
    await expect(page.getByRole('main').getByRole('heading', { level: 1 })).toContainText('Archive');
  });
});
