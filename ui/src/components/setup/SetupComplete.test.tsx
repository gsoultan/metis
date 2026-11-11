/**
 * The wizard's last step, rendered for the two ways setup can finish.
 */
import { describe, expect, it } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import { SetupComplete } from './SetupComplete';

const textOf = (html: string) => html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');

const rendered = (fromEnvironment: boolean, wizardOnly = false) =>
  textOf(
    renderToStaticMarkup(
      <MantineProvider>
        <SetupComplete fromEnvironment={fromEnvironment} wizardOnly={wizardOnly} onComplete={() => {}} />
      </MantineProvider>,
    ),
  );

describe('SetupComplete', () => {
  // The server keeps the database and keys it started with until it restarts,
  // and the account setup just created may be in a database it is not reading.
  it('asks for a restart when setup wrote config.yaml', () => {
    const text = rendered(false);
    expect(text).toContain('config.yaml');
    expect(text).toContain('Restart Metis to run on it');
    expect(text).not.toContain('You can now log in');
  });

  it('lets somebody sign in straight away when the environment named the database and keys', () => {
    const text = rendered(true);
    expect(text).toContain('nothing to save');
    expect(text).toContain('You can now log in with your administrator account');
    expect(text).not.toContain('Restart');
  });

  // A server that was serving only the wizard starts on what it saved by
  // itself, so asking for a restart would send somebody to do it twice.
  it('says the server is starting when it was serving only the wizard', () => {
    const text = rendered(false, true);
    expect(text).toContain('config.yaml');
    expect(text).toContain('Metis is starting on the database you chose');
    expect(text).not.toContain('Restart');
  });

  // Email, sign-on and retention are not asked for here: setup is anonymous,
  // and a saved setting records which administrator chose it.
  it('points to where the server settings are changed', () => {
    for (const fromEnvironment of [true, false]) {
      expect(rendered(fromEnvironment)).toContain('Settings → Server settings');
    }
  });
});
