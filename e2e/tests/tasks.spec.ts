import type { Page } from '@playwright/test';

import { deployReviewProcess, sampleProjectId, shortReference, startProcess } from '../support/api';
import { expect, knownBug, test, uniqueName } from '../support/test';

/**
 * The inbox and All Tasks, on a process each test starts for itself.
 *
 * The sample's approvals are offered to groups that are not People groups, so
 * nobody can take them (All Tasks lists them under "Nobody can act on these",
 * which is checked below). Each test here starts its own instance of a
 * one-task process instead, and finds its task by the instance's reference.
 */
const KEY = 'e2e-review';
const PROCESS = 'E2E review';
const TASK = 'Review the request';

test.describe.configure({ mode: 'serial' });

let deployed = false;
let reference = '';
// What the task is about, unique per test: both task lists show it, so it is
// how a test finds its own task among the ones earlier tests left.
let about = '';

test.beforeEach(async ({ api }) => {
  const project = await sampleProjectId(api);
  // Once per run: every deploy is a new version, and versions add up in the
  // lists that show them.
  if (!deployed) await deployReviewProcess(api, project, KEY, PROCESS, TASK, [
    { id: 'approved', label: 'Approved', type: 'boolean' },
    { id: 'note', label: 'Note', type: 'textarea', required: true },
  ]);
  deployed = true;
  about = uniqueName('Printer paper');
  reference = shortReference(await startProcess(api, project, KEY, { amount: 120, description: about }));
});

function inboxRow(page: Page) {
  return page.getByRole('row', { name: new RegExp(about) });
}

async function claimInInbox(page: Page) {
  await page.goto('/inbox');
  await page.getByRole('tab', { name: /Available to Claim/ }).click();
  await inboxRow(page).getByRole('button', { name: 'Claim Task' }).click();
  await expect(page.getByText("It's yours now.")).toBeVisible();
  await page.getByRole('tab', { name: /Assigned to Me/ }).click();
  await expect(inboxRow(page)).toBeVisible();
}

test.describe('my inbox', () => {
  test('a task nobody holds waits under "Available to Claim", and claiming moves it to me', async ({ page }) => {
    await page.goto('/inbox');
    await page.getByRole('tab', { name: /Available to Claim/ }).click();
    await expect(inboxRow(page)).toContainText(TASK);
    await expect(inboxRow(page)).toContainText('Unclaimed');

    await claimInInbox(page);
  });

  test('the claim is written to the audit log', async ({ page }) => {
    await claimInInbox(page);
    await page.goto('/audit');
    await expect(page.getByRole('main').getByRole('row').nth(1)).toBeVisible();
  });

  test('completing asks for the form, refuses a missing required answer, then finishes the instance', async ({ page }) => {
    await claimInInbox(page);
    await inboxRow(page).getByRole('button', { name: 'Complete', exact: true }).click();

    const form = page.getByRole('dialog').filter({ hasText: `Complete Task: ${TASK}` });
    await expect(form).toBeVisible();
    await expect(form.getByText('Approved')).toBeVisible();

    const note = form.getByRole('textbox', { name: /Note/ });
    const submit = form.getByRole('button', { name: 'Complete Task' });
    await submit.click();
    // The browser holds the form back: the note is required and empty.
    expect(await note.evaluate((el) => (el as HTMLTextAreaElement).validity.valueMissing)).toBe(true);
    await expect(form).toBeVisible();

    await form.getByText('Approved', { exact: true }).click();
    await note.fill('Fine, within budget');
    await submit.click();
    await expect(form).toBeHidden();
    await expect(inboxRow(page)).toContainText(/completed/i);

    await page.goto('/instances');
    await expect(page.getByRole('row', { name: new RegExp(`#${reference}`) })).toContainText('Completed');
  });

  test('releasing a claimed task offers it again', async ({ page }) => {
    await claimInInbox(page);
    await inboxRow(page).getByRole('button', { name: /^Release|^Hand back|^Unclaim/ }).click();
    await page.getByRole('tab', { name: /Available to Claim/ }).click();
    await expect(inboxRow(page)).toBeVisible();
  });

  test('search finds a task by its name', async ({ page }) => {
    await page.goto('/inbox');
    await page.getByRole('tab', { name: /Available to Claim/ }).click();
    await page.getByRole('textbox', { name: 'Search tasks' }).fill('no task is called this');
    await expect(inboxRow(page)).toHaveCount(0);
    await page.getByRole('textbox', { name: 'Search tasks' }).fill(TASK);
    await expect(inboxRow(page)).toBeVisible();
  });

  test('the kanban view shows the same tasks', async ({ page }) => {
    await page.goto('/inbox');
    await page.getByRole('tab', { name: /Available to Claim/ }).click();
    await page.getByText('Kanban', { exact: true }).click();
    await expect(page.getByRole('radio', { name: 'Kanban' })).toBeChecked();
    await expect(page.getByRole('main').getByText(TASK).first()).toBeVisible();
  });

  test('"Open task" opens the task, not the process diagram', async ({ page }) => {
    knownBug('The inbox\'s "Open task" button goes to the designer (it is the process-path link, mislabelled)');
    await page.goto('/inbox');
    await page.getByRole('tab', { name: /Available to Claim/ }).click();
    await inboxRow(page).getByRole('button', { name: 'Open task' }).click();
    await expect(page).not.toHaveURL(/\/designer/);
  });
});

test.describe('all tasks', () => {
  test('lists every task in the project with who holds it', async ({ page }) => {
    await page.goto('/tasks');
    const row = page.getByRole('row', { name: new RegExp(about) });
    await expect(row).toContainText(TASK);
    await expect(row).toContainText('Unassigned');
    await expect(row).toContainText('Available');
  });

  test('tasks nobody can take are listed apart, with a way to hand them over', async ({ page }) => {
    await page.goto('/tasks');
    await expect(page.getByText('Nobody can act on these')).toBeVisible();
    const stranded = page.getByRole('row', { name: /Manager approves .* Nobody left to take it/ }).first();
    await expect(stranded).toBeVisible();
    await stranded.getByRole('button', { name: 'Hand over' }).click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await page.keyboard.press('Escape');
  });

  test('"Complete" on a task with a form asks for the form instead of finishing it', async ({ page }) => {
    await page.goto('/tasks');
    const row = page.getByRole('row', { name: new RegExp(about) });
    await row.getByRole('button', { name: 'Complete', exact: true }).click();
    await expect(page.getByRole('dialog').filter({ hasText: TASK })).toBeVisible({ timeout: 3_000 });
  });
});
