import { expect, knownBug, test, uniqueName } from '../support/test';

/** Accounts, groups, organizations, projects and the People directory. */

test.describe('groups', () => {
  test('a new group is listed and can be found', async ({ page }) => {
    const name = uniqueName('e2e-reviewers').replace(/\s/g, '-');
    await page.goto('/groups');
    await page.getByRole('button', { name: 'New Group' }).click();
    const dialog = page.getByRole('dialog', { name: 'New Group' });
    await expect(dialog.getByRole('button', { name: 'Create' })).toBeDisabled();
    await dialog.getByRole('textbox', { name: 'Group Name' }).fill(name);
    await dialog.getByRole('textbox', { name: 'Description' }).fill('Created by the end-to-end suite');
    await dialog.getByRole('button', { name: 'Create' }).click();
    await expect(dialog).toBeHidden();

    await page.getByRole('textbox', { name: 'Search groups' }).fill(name);
    await expect(page.getByRole('row', { name: new RegExp(name) })).toBeVisible();
  });
});

test.describe('accounts', () => {
  test('lists the administrator and the sample accounts with their roles', async ({ page }) => {
    await page.goto('/users');
    await expect(page.getByRole('row', { name: /Development Admin @admin/ })).toContainText('Administrator');
    for (const who of ['Manager', 'Director', 'Reviewer']) {
      await expect(page.getByRole('row', { name: new RegExp(`^${who} @`) })).toBeVisible();
    }
  });

  test('a new account needs a password of eight characters, and is listed once made', async ({ page }) => {
    const username = uniqueName('e2e').replace(/\s/g, '').toLowerCase();
    await page.goto('/users');
    await page.getByRole('button', { name: 'New account' }).click();
    const dialog = page.getByRole('dialog', { name: 'New account' });
    await dialog.getByRole('textbox', { name: 'Username' }).fill(username);
    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('End To End');
    await dialog.getByRole('textbox', { name: 'Display Name' }).fill('E2E');
    await dialog.getByRole('textbox', { name: 'Email' }).fill(`${username}@example.invalid`);
    await expect(dialog.getByRole('textbox', { name: 'Organization' })).toHaveValue('Example Co');

    await dialog.getByRole('textbox', { name: 'Password' }).fill('short');
    await expect(dialog.getByRole('button', { name: 'Create' })).toBeDisabled();
    await dialog.getByRole('textbox', { name: 'Password' }).fill('long-enough-password');
    await expect(dialog.getByRole('button', { name: 'Create' })).toBeEnabled();
    await dialog.getByRole('button', { name: 'Create' }).click();
    await expect(dialog).toBeHidden();

    await page.getByRole('textbox', { name: 'Search people' }).fill(username);
    await expect(page.getByRole('row', { name: new RegExp(`@${username}`) })).toBeVisible();
  });

  test('the roles tab explains what each role may do', async ({ page }) => {
    await page.goto('/users');
    await page.getByRole('tab', { name: 'Roles' }).click();
    await expect(page.getByRole('tabpanel', { name: 'Roles' })).toContainText(/ADMIN|Administrator/);
  });
});

test.describe('organizations and projects', () => {
  test('the organization being worked in is marked, and its members can be seen', async ({ page }) => {
    await page.goto('/organizations');
    const row = page.getByRole('row', { name: /Example Co/ });
    await expect(row).toContainText('Working in');
    await row.getByRole('button', { name: 'Members' }).click();
    await expect(page.getByText('Development Admin').last()).toBeVisible();
  });

  test('a new organization is listed', async ({ page }) => {
    const name = uniqueName('E2E Org');
    await page.goto('/organizations');
    await page.getByRole('button', { name: 'New Organization' }).click();
    const dialog = page.getByRole('dialog', { name: 'New Organization' });
    await dialog.getByRole('textbox', { name: 'Organization Name' }).fill(name);
    await dialog.getByRole('button', { name: 'Create' }).click();
    await expect(dialog).toBeHidden();
    await expect(page.getByRole('row', { name: new RegExp(name) })).toBeVisible();
  });

  test('switching organization changes what the pages show, and switching back restores it', async ({ page }) => {
    const name = uniqueName('E2E Elsewhere');
    await page.goto('/organizations');
    await page.getByRole('button', { name: 'New Organization' }).click();
    const dialog = page.getByRole('dialog', { name: 'New Organization' });
    await dialog.getByRole('textbox', { name: 'Organization Name' }).fill(name);
    await dialog.getByRole('button', { name: 'Create' }).click();
    await expect(dialog).toBeHidden();

    await page.getByRole('button', { name: /^Organization: / }).click();
    await page.getByRole('menuitem', { name, exact: true }).click();
    await expect(page.getByRole('button', { name: `Organization: ${name}` })).toBeVisible();
    // A new organization has no projects, so none of the sample's processes.
    await page.goto('/models?tab=processes');
    await expect(page.getByRole('row', { name: /Expense approval/ })).toHaveCount(0);

    await page.getByRole('button', { name: /^Organization: / }).click();
    await page.getByRole('menuitem', { name: 'Example Co', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Organization: Example Co' })).toBeVisible();
    await page.getByRole('combobox', { name: 'Project' }).click();
    await page.getByRole('option', { name: 'Sample Project', exact: true }).click();
    await page.goto('/models?tab=processes');
    await expect(page.getByRole('row', { name: /Expense approval/ })).toBeVisible();
  });

  test('a new project is listed under the organization', async ({ page }) => {
    const name = uniqueName('E2E Project');
    await page.goto('/projects');
    await expect(page.getByRole('row', { name: /Sample Project/ })).toContainText('Selected in this browser');
    await page.getByRole('button', { name: 'New Project' }).click();
    const dialog = page.getByRole('dialog', { name: 'New Project' });
    await dialog.getByRole('combobox', { name: 'Organization' }).click();
    await page.getByRole('option', { name: 'Example Co', exact: true }).click();
    await dialog.getByRole('textbox', { name: 'Project Name' }).fill(name);
    await dialog.getByRole('button', { name: 'Create' }).click();
    await expect(dialog).toBeHidden();
    await expect(page.getByRole('row', { name: new RegExp(name) })).toBeVisible();
  });
});

test('a new project starts in the organization being worked in', async ({ page }) => {
  knownBug('New Project picks the first organization in the list (ProjectList.tsx organizations[0]), not the one being worked in');
  await page.goto('/projects');
  await page.getByRole('button', { name: 'New Project' }).click();
  // Only wrong once there is a second organization, which the test above makes.
  await expect(page.getByRole('dialog', { name: 'New Project' }).getByRole('combobox', { name: 'Organization' })).toHaveValue('Example Co', { timeout: 3_000 });
});

test.describe('People', () => {
  test('a CSV of people is imported and they are listed', async ({ page }) => {
    const username = uniqueName('person').replace(/\s/g, '').toLowerCase();
    await page.goto('/people');
    await expect(page.getByText(/People sign in to their tasks at/)).toBeVisible();

    await page.getByRole('button', { name: 'Import' }).click();
    const dialog = page.getByRole('dialog', { name: 'Import people' });
    await dialog.getByRole('button', { name: 'CSV file' }).click();
    await page.locator('input[type="file"]').setInputFiles({
      name: 'people.csv',
      mimeType: 'text/csv',
      buffer: Buffer.from(`username,display_name,email,groups\n${username},Pat Person,${username}@example.invalid,e2e-buyers\n`),
    });
    await dialog.getByRole('button', { name: 'Import' }).click();

    await page.getByRole('textbox', { name: 'Search people' }).fill(username);
    await expect(page.getByRole('main').getByText('Pat Person').first()).toBeVisible();
  });

  test('a directory needs a name and an address before it can be added', async ({ page }) => {
    await page.goto('/people');
    await page.getByRole('button', { name: 'Add directory' }).click();
    const dialog = page.getByRole('dialog', { name: 'New directory' });
    await expect(dialog.getByRole('button', { name: 'Add directory' })).toBeDisabled();
    await dialog.getByRole('textbox', { name: 'Name' }).fill('HR system');
    await dialog.getByRole('textbox', { name: 'Endpoint' }).fill('https://hr.example.invalid/api/staff');
    await expect(dialog.getByRole('button', { name: 'Add directory' })).toBeEnabled();
    await dialog.getByRole('button', { name: 'Cancel' }).click();
  });
});
