import type { Page } from '@playwright/test';

import { expect, test, uniqueName } from '../support/test';

async function openDecision(page: Page, name: string): Promise<void> {
  await page.goto('/models?tab=decisions');
  await page.getByRole('button', { name: `Edit ${name}` }).click();
  await expect(page.getByRole('textbox', { name: 'What this decides' })).toHaveValue(name);
}

test.describe('the decisions list', () => {
  test('lists the sample tables with their version and how they choose', async ({ page }) => {
    await page.goto('/models?tab=decisions');
    const row = page.getByRole('row', { name: /Expense approval level/ });
    await expect(row).toContainText('expense-approval-level');
    await expect(row).toContainText('v1 live');
    await expect(row).toContainText('The first line that matches wins');
    await expect(page.getByRole('row', { name: /Supplier risk band/ })).toBeVisible();
  });

  test('search narrows the list', async ({ page }) => {
    await page.goto('/models?tab=decisions');
    await page.getByRole('textbox', { name: 'Search decisions' }).fill('supplier');
    await expect(page.getByRole('row', { name: /Supplier risk band/ })).toBeVisible();
    await expect(page.getByRole('row', { name: /Expense approval level/ })).toBeHidden();
  });

  test('version history lists the live version', async ({ page }) => {
    await page.goto('/models?tab=decisions');
    await page.getByRole('button', { name: 'Version history of Expense approval level' }).click();
    const history = page.getByRole('dialog', { name: 'Versions of Expense approval level' });
    await expect(history.getByRole('row', { name: /v1 Live/ })).toBeVisible();
  });
});

test.describe('the decision editor', () => {
  test('describes the table in words and shows every line', async ({ page }) => {
    await openDecision(page, 'Expense approval level');

    await expect(page.getByText('Looks at Amount, decides Approval level and Approver.')).toBeVisible();
    for (const line of [1, 2, 3]) {
      await expect(page.getByRole('textbox', { name: `Note on line ${line}` })).toBeVisible();
    }
    await expect(page.getByRole('radio', { name: /The first line that matches wins/ })).toBeChecked();
    await expect(page.getByText('Every case has a line that decides it.')).toBeVisible();
    // Nothing has changed, so there is nothing to save.
    await expect(page.getByRole('button', { name: 'Save' })).toBeDisabled();
  });

  test('a case tried in the table says which line matched and what it decided', async ({ page }) => {
    await openDecision(page, 'Expense approval level');

    for (const [amount, line, level] of [['40', 1, 'auto'], ['500', 2, 'manager'], ['2400', 3, 'director']] as const) {
      await page.getByRole('textbox', { name: 'Try a value for Amount' }).fill(amount);
      await page.getByRole('button', { name: 'Run this case' }).click();
      const result = page.getByRole('row', { name: new RegExp(`Line ${line} matched`) });
      await expect(result).toBeVisible();
      await expect(result).toContainText(level);
    }
  });

  test('"Try it" runs the saved version', async ({ page }) => {
    await openDecision(page, 'Expense approval level');
    await page.getByRole('tab', { name: 'Try it' }).click();
    const panel = page.getByRole('tabpanel', { name: 'Try it' });

    await panel.getByRole('textbox', { name: 'Amount' }).fill('750');
    await panel.getByRole('button', { name: 'Run', exact: true }).first().click();
    await expect(panel.getByText('Line 2 matched')).toBeVisible();
    await expect(panel.getByText('manager', { exact: true })).toBeVisible();
  });

  test('an edit is saved as a new version, staged without changing the live one', async ({ page }) => {
    await openDecision(page, 'Supplier risk band');
    await page.getByRole('textbox', { name: 'Note on line 1' }).fill(`Checked by the end-to-end suite ${Date.now()}`);
    const save = page.getByRole('main').getByRole('button', { name: 'Save', exact: true });
    await expect(save).toBeEnabled();
    await save.click();

    // Staged, so the supplier check keeps deciding with the version the sample deployed.
    const confirm = page.getByRole('dialog').filter({ hasText: /Save Supplier risk band v\d+/ });
    await expect(confirm.getByRole('radio', { name: /Make v\d+ live/ })).toBeChecked();
    await confirm.getByRole('radio', { name: /Stage v\d+ for later/ }).check();
    await confirm.getByRole('button', { name: /^Save v\d+/ }).click();
    await expect(confirm).toBeHidden();
    await expect(save).toBeDisabled();

    await page.goto('/models?tab=decisions');
    await expect(page.getByRole('row', { name: /Supplier risk band/ })).toContainText('v1 live');
    await page.getByRole('button', { name: 'Version history of Supplier risk band' }).click();
    const history = page.getByRole('dialog', { name: 'Versions of Supplier risk band' });
    await expect(history.getByRole('row', { name: /^v2 / })).toBeVisible();
    await expect(history.getByRole('row', { name: /v1 Live/ })).toBeVisible();
  });
});

test.describe('a new decision', () => {
  test('is created from the wizard, given a rule, saved and listed', async ({ page }) => {
    const name = uniqueName('E2E discount');
    await page.goto('/models?tab=decisions');
    await page.getByRole('button', { name: 'New decision' }).click();
    const wizard = page.getByRole('dialog', { name: 'Creation Wizard' });
    await wizard.getByRole('textbox', { name: 'Display Name' }).fill(name);
    await wizard.getByRole('button', { name: 'Next Step' }).click();
    await wizard.getByRole('button', { name: 'Create Element' }).click();

    await expect(page).toHaveURL(/\/decision-editor/);
    await expect(page.getByRole('textbox', { name: 'What this decides' })).toHaveValue(name);

    // At least one line, whatever the empty table starts with.
    if ((await page.getByRole('textbox', { name: /condition$/ }).count()) === 0) {
      await page.getByRole('button', { name: 'Add line' }).click();
    }
    await page.getByRole('textbox', { name: /result$/ }).first().fill('"gold"');
    await page.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByRole('button', { name: 'Save' })).toBeDisabled();

    await page.goto('/models?tab=decisions');
    await expect(page.getByRole('row', { name: new RegExp(name) })).toBeVisible();
  });
});
