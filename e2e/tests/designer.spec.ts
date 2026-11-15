import { Designer } from '../support/designer';
import { expect, test, uniqueName } from '../support/test';

/**
 * The process designer, on the sample's expense approval.
 *
 * Nothing here deploys the sample: other specs start instances of it and
 * expect the version the seed deployed. Deploying is covered on a process this
 * spec creates for itself (see "a new process from a template").
 */
test.describe('the process designer', () => {
  test.beforeEach(async ({ page }) => {
    // A draft left by an earlier test is kept per browser; each test starts clean.
    await page.goto('/');
    await page.evaluate(() => {
      for (const key of Object.keys(localStorage)) if (/draft/i.test(key)) localStorage.removeItem(key);
    });
  });

  test('opens a deployed process with every step named on the diagram', async ({ page }) => {
    const designer = new Designer(page);
    await designer.openFromProcesses('Expense approval');

    await expect(page.getByRole('textbox', { name: 'Process name' })).toHaveValue('Expense approval');
    for (const name of ['Expense submitted', 'Decide approval level', 'Who approves?', 'Manager approves', 'Director approves', 'Approved automatically', 'Decision recorded']) {
      await expect(designer.step(name)).toBeVisible();
    }
  });

  test('opening a live process without changing it does not claim unsaved changes', async ({ page }) => {
    const designer = new Designer(page);
    await designer.openFromProcesses('Expense approval');
    await designer.select('Who approves?');

    await expect(page.getByRole('main').getByText('Not deployed yet')).toHaveCount(0);
    await expect(page.getByRole('main').getByText(/Draft kept in this browser/)).toHaveCount(0);
  });

  test('clicking a step opens its settings, named after it', async ({ page }) => {
    const designer = new Designer(page);
    await designer.openFromProcesses('Expense approval');

    const about = await designer.openTab('Manager approves', 'About');
    await expect(about.getByRole('textbox', { name: 'Name' })).toHaveValue('Manager approves');

    await designer.settings('Manager approves').getByRole('button', { name: 'Close the step settings' }).click();
    await expect(designer.settings()).toBeHidden();
  });

  test('renaming a step renames it on the diagram as it is typed', async ({ page }) => {
    const designer = new Designer(page);
    await designer.openFromProcesses('Expense approval');

    const about = await designer.openTab('Manager approves', 'About');
    await about.getByRole('textbox', { name: 'Name' }).fill('Line manager approves');
    await expect(designer.step('Line manager approves')).toBeVisible();
    await expect(designer.step('Manager approves')).toHaveCount(0);
  });

  test.describe('a fork with conditions ("Choose one path")', () => {
    test('says what happens when nothing matches, and offers a fall-back path', async ({ page }) => {
      const designer = new Designer(page);
      await designer.openFromProcesses('Expense approval');

      const setup = await designer.openTab('Who approves?', 'Setup');
      await expect(setup.getByText('If nothing matches')).toBeVisible();
      await expect(setup.getByRole('alert')).toContainText('raises an incident');

      await setup.getByRole('combobox', { name: 'Fall back to' }).click();
      await expect(page.getByRole('listbox', { name: 'Fall back to' }).getByRole('option')).toHaveCount(3);
    });

    test('the fall-back picker names each path by the step it leads to', async ({ page }) => {
      const designer = new Designer(page);
      await designer.openFromProcesses('Expense approval');

      const setup = await designer.openTab('Who approves?', 'Setup');
      await setup.getByRole('combobox', { name: 'Fall back to' }).click();
      const options = page.getByRole('listbox', { name: 'Fall back to' });
      for (const target of ['Manager approves', 'Director approves', 'Approved automatically']) {
        await expect(options.getByRole('option', { name: new RegExp(target) })).toBeVisible();
      }
    });

    test('choosing a fall-back clears the "no default path" warning', async ({ page }) => {
      const designer = new Designer(page);
      await designer.openFromProcesses('Expense approval');

      const checks = await designer.openTab('Who approves?', 'Checks');
      await expect(checks.getByText('No default path selected.')).toBeVisible();

      const setup = await designer.openTab('Who approves?', 'Setup');
      await setup.getByRole('combobox', { name: 'Fall back to' }).click();
      await page.getByRole('listbox', { name: 'Fall back to' }).getByRole('option').last().click();
      // The warning that the process would stop turns into what will happen instead.
      await expect(setup.getByRole('alert')).toContainText('A value none of the conditions expect will take this path.');

      const after = await designer.openTab('Who approves?', 'Checks');
      await expect(after.getByText('No default path selected.')).toBeHidden();
    });

    test('each path out of it shows its condition on the diagram', async ({ page }) => {
      const designer = new Designer(page);
      await designer.openFromProcesses('Expense approval');
      // Three paths leave the fork, and each carries a label.
      for (const target of ['ask-manager', 'ask-director', 'auto-approve']) {
        await expect(designer.canvas.getByRole('group', { name: `Edge from route to ${target}` })).not.toHaveText('');
      }
    });
  });

  test('the checks list problems before deploying, and one click opens the step', async ({ page }) => {
    const designer = new Designer(page);
    await designer.openFromProcesses('Expense approval');

    await designer.checksButton.click();
    const checks = page.getByRole('dialog', { name: /^Checks:/ });
    await expect(checks.getByText('Before you deploy')).toBeVisible();
    await checks.getByRole('button', { name: /"Manager approves"/ }).click();
    await expect(designer.settings('Manager approves')).toBeVisible();
  });

  test('deploying with warnings asks first, and going back deploys nothing', async ({ page }) => {
    const designer = new Designer(page);
    await designer.openFromProcesses('Expense approval');

    await page.getByRole('button', { name: 'Deploy', exact: true }).click();
    const checklist = page.getByRole('dialog', { name: 'Pre-deployment Checklist' });
    await expect(checklist).toBeVisible();
    await expect(checklist.getByRole('button', { name: 'Deploy Anyway' })).toBeVisible();
    await checklist.getByRole('button', { name: 'Go Back to Editor' }).click();
    await expect(checklist).toBeHidden();

    await page.goto('/models?tab=processes');
    await expect(page.getByRole('row', { name: /Expense approval/ })).toContainText('v1 live');
  });

  test.describe('building blocks', () => {
    test('search narrows the palette', async ({ page }) => {
      const designer = new Designer(page);
      await designer.openFromProcesses('Expense approval');
      await page.getByRole('button', { name: 'Add a step' }).click();
      const palette = page.getByRole('complementary', { name: 'Building blocks' });

      await palette.getByRole('textbox', { name: 'Search process steps' }).fill('wait');
      await expect(palette.getByRole('button', { name: 'Add Ask a person' })).toBeVisible();
      await expect(palette.getByRole('button', { name: 'Add Do several things at once' })).toBeVisible();
      await expect(palette.getByRole('button', { name: 'Add Apply a business rule' })).toBeHidden();
    });

    test('clicking a block adds it after the selected step, and undo takes it away', async ({ page }) => {
      const designer = new Designer(page);
      await designer.openFromProcesses('Expense approval');
      await designer.select('Approved automatically');
      const askSteps = designer.canvas.getByRole('group').filter({ hasText: 'Ask a person' });
      const people = await askSteps.count();

      await page.getByRole('button', { name: 'Add a step' }).click();
      await page.getByRole('complementary', { name: 'Building blocks' }).getByRole('button', { name: 'Add Ask a person' }).click();
      await expect(askSteps).toHaveCount(people + 1);

      // Adding a step after another also reconnects the paths around it, and
      // each of those is its own entry in the history, so undo is pressed
      // until the step is gone or there is nothing left to undo.
      const undo = page.getByRole('button', { name: 'Undo' });
      for (let i = 0; i < 3 && (await undo.isEnabled()); i++) {
        if ((await askSteps.count()) === people) break;
        await undo.click();
      }
      await expect(askSteps).toHaveCount(people);
    });
  });

  test('the diagram can be exported as a BPMN file', async ({ page }) => {
    const designer = new Designer(page);
    await designer.openFromProcesses('Expense approval');

    await page.getByRole('button', { name: 'More actions' }).click();
    const download = page.waitForEvent('download');
    await page.getByRole('menuitem', { name: /Export/ }).click();
    const file = await download;
    expect(file.suggestedFilename()).toMatch(/\.bpmn$|\.xml$|\.json$/);
  });

  test('simulate mode can be switched on and back', async ({ page }) => {
    const designer = new Designer(page);
    await designer.openFromProcesses('Expense approval');
    const mode = page.getByRole('radiogroup', { name: 'Design or simulate this process' });

    await mode.getByText('Simulate').click();
    await expect(mode.getByRole('radio', { name: 'Simulate' })).toBeChecked();
    await mode.getByText('Design').click();
    await expect(mode.getByRole('radio', { name: 'Design' })).toBeChecked();
  });
});

test.describe('a new process', () => {
  test('is named in the wizard, built step by step, deployed, run and versioned', async ({ page }) => {
    const name = uniqueName('E2E blank');
    await page.goto('/models?tab=processes');
    await page.getByRole('button', { name: 'New process' }).click();
    const wizard = page.getByRole('dialog', { name: 'Creation Wizard' });
    await wizard.getByRole('textbox', { name: 'Display Name' }).fill(name);
    await expect(wizard.getByRole('textbox', { name: 'Technical Key' })).toHaveValue(/^e2e_blank_/);
    await wizard.getByRole('button', { name: 'Next Step' }).click();
    await expect(wizard.getByRole('heading', { name: 'Ready to Go!' })).toBeVisible();
    await wizard.getByRole('button', { name: 'Create Element' }).click();

    const designer = new Designer(page);
    await expect(page).toHaveURL(/\/designer/);
    await expect(page.getByRole('textbox', { name: 'Process name' })).toHaveValue(name);
    await expect(designer.canvas.getByText('Start with a step')).toBeVisible();
    // An empty canvas has nothing to deploy.
    await expect(page.getByRole('button', { name: 'Deploy', exact: true })).toBeDisabled();

    // Each block is added after the one selected, and joined to it.
    await designer.canvas.getByRole('button', { name: 'Add a start' }).click();
    await page.getByRole('button', { name: 'Add a step' }).click();
    const blocks = page.getByRole('complementary', { name: 'Building blocks' });
    await blocks.getByRole('button', { name: 'Add Ask a person' }).click();
    await blocks.getByRole('button', { name: 'Add Finish', exact: true }).click();
    await expect(designer.canvas.getByRole('group', { name: /^Edge from / })).toHaveCount(2);
    await page.getByRole('button', { name: 'Hide building blocks' }).first().click();

    const about = await designer.openTab('Ask a person', 'About');
    await about.getByRole('textbox', { name: 'Name' }).fill('Check the paperwork');

    await deploy(page, 'v1');
    await page.goto('/models?tab=processes');
    const row = page.getByRole('row', { name: new RegExp(name) });
    await expect(row).toContainText('v1 live');

    await row.getByRole('button', { name: 'Run' }).click();
    const run = page.getByRole('dialog', { name: `Start ${name}?` });
    await run.getByRole('button', { name: 'Start' }).click();
    await expect(run).toBeHidden();
    await page.goto('/instances');
    await expect(page.getByRole('row', { name: new RegExp(name) }).first()).toContainText('Running');

    // A change is a new version; the running instance stays on the one it started with.
    await designer.openFromProcesses(name);
    const again = await designer.openTab('Check the paperwork', 'About');
    await again.getByRole('textbox', { name: 'Name' }).fill('Check the paperwork twice');
    await deploy(page, 'v2');
    await page.goto('/models?tab=processes');
    await expect(page.getByRole('row', { name: new RegExp(name) })).toContainText('v2 live');
  });
});

/**
 * Deploys what is on the canvas as the live version, through whatever the
 * designer asks first: the checklist when there are warnings, and, from the
 * second version on, whether the new one goes live now or is staged.
 */
async function deploy(page: import('@playwright/test').Page, version: string): Promise<void> {
  await page.getByRole('button', { name: 'Deploy', exact: true }).click();
  const checklist = page.getByRole('dialog', { name: 'Pre-deployment Checklist' });
  const confirm = page.getByRole('dialog', { name: new RegExp(`^Deploy .* ${version}$`) });
  const deployed = page.getByText('Process definition deployed successfully!');
  await expect(checklist.or(confirm).or(deployed)).toBeVisible();
  if (await checklist.isVisible()) await checklist.getByRole('button', { name: 'Deploy Anyway' }).click();
  await expect(confirm.or(deployed)).toBeVisible();
  if (await confirm.isVisible()) {
    await expect(confirm.getByRole('radio', { name: new RegExp(`Make ${version} the live version`) })).toBeChecked();
    await confirm.getByRole('button', { name: `Deploy ${version} live` }).click();
  }
  await expect(deployed).toBeVisible();
}
