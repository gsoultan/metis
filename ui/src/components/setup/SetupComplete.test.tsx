/**
 * The wizard's last step, rendered for the two ways setup can finish.
 */
import { describe, expect, it } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import { SetupComplete } from './SetupComplete';

const textOf = (html: string) => html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');

const rendered = (fromEnvironment: boolean) =>
  textOf(
    renderToStaticMarkup(
      <MantineProvider>
        <SetupComplete fromEnvironment={fromEnvironment} onComplete={() => {}} />
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
});
