import { expect, type Locator, type Page } from '@playwright/test';

/** The designer's canvas, step settings and toolbar, by what a person sees. */
export class Designer {
  constructor(readonly page: Page) {}

  get canvas(): Locator {
    return this.page.getByRole('application');
  }

  /** A step on the canvas, found by the name written on it. */
  step(name: string): Locator {
    return this.canvas.getByRole('group').filter({ has: this.page.getByText(name, { exact: true }) });
  }

  /** The settings panel of the selected step. */
  settings(stepName?: string): Locator {
    return this.page.getByRole('complementary', { name: stepName ? `Settings for ${stepName}` : /^Settings for / });
  }

  get status(): Locator {
    return this.page.getByRole('main').getByText(/Not deployed yet|Draft kept in this browser|Live|deployed/i).first();
  }

  get checksButton(): Locator {
    return this.page.getByRole('button', { name: /^Checks:/ });
  }

  async openFromProcesses(processName: string): Promise<void> {
    await this.page.goto('/models?tab=processes');
    await this.page.getByRole('button', { name: `Edit ${processName}` }).click();
    await expect(this.canvas).toBeVisible();
  }

  async select(stepName: string): Promise<Locator> {
    await this.step(stepName).click();
    const panel = this.settings(stepName);
    await expect(panel).toBeVisible();
    return panel;
  }

  async openTab(stepName: string, tab: 'About' | 'Setup' | 'Data' | 'Checks'): Promise<Locator> {
    const panel = await this.select(stepName);
    await panel.getByRole('tab', { name: tab }).click();
    return panel.getByRole('tabpanel', { name: tab });
  }
}
