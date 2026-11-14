import { expect, test as setup } from '@playwright/test';

import { ADMIN_PASSWORD, ADMIN_STATE, ADMIN_USER } from '../support/env';
import { signInThroughPage } from '../support/test';

/**
 * Signs the admin in once and keeps the browser state every other spec starts from.
 *
 * Saved only once the app has chosen the organization and project to work in:
 * saved before, every spec would start with none chosen, and the pages that need
 * a project would race the app choosing one.
 */
setup('sign in as the administrator', async ({ page }) => {
  await signInThroughPage(page, ADMIN_USER, ADMIN_PASSWORD);
  // Sign-in opens the first of the account's organizations by name, and the
  // specs add organizations of their own; against a server an earlier run used,
  // one of those can come first. The sample lives in Example Co.
  const picker = page.getByRole('button', { name: /^Organization: / });
  await expect(picker).not.toHaveAccessibleName('Organization: none chosen');
  if ((await picker.getAttribute('aria-label')) !== 'Organization: Example Co') {
    await picker.click();
    await page.getByRole('menuitem', { name: 'Example Co', exact: true }).click();
  }
  await expect(page.getByRole('button', { name: 'Organization: Example Co' })).toBeVisible();
  const project = page.getByRole('combobox', { name: 'Project' });
  if ((await project.inputValue()) !== 'Sample Project') {
    await project.click();
    await page.getByRole('option', { name: 'Sample Project', exact: true }).click();
  }
  await expect(project).toHaveValue('Sample Project');
  await page.context().storageState({ path: ADMIN_STATE });
});
