/**
 * The outgoing webhooks card, as an administrator and as anybody else sees it,
 * read in both colour schemes.
 */
import { afterAll, beforeEach, describe, expect, it, mock } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderToStaticMarkup } from 'react-dom/server';

import type { ApiOutgoingWebhook } from '../../services/domains/outgoingWebhookService';
import { standInForAppStore, type SignedInUser } from '../../test/appStoreStandIn';
import { customPropertiesOf, resolveColour, schemeVariables, type ColourScheme } from '../../testing/themeColours';
import { cssVariablesResolver, theme } from '../../theme';
import { AA_NORMAL_TEXT, contrastRatio } from '../../theme/contrast';
import { OutgoingWebhookSettings } from './OutgoingWebhookSettings';

const store = standInForAppStore((specifier, factory) => mock.module(specifier, factory));
afterAll(() => store.restore());

const PROJECT = 'project-1';

const webhooks: ApiOutgoingWebhook[] = [
  {
    id: 'o-1',
    project_id: PROJECT,
    name: 'Finance ledger',
    url: 'https://ledger.example.com/hooks/metis',
    events: ['process.completed', 'incident.raised'],
    enabled: true,
    created_at: '2026-10-21T10:00:00Z',
  },
];

const signedInAs = (roles: string[]) =>
  store.set({ currentProjectId: PROJECT, token: 'a-session', user: { username: 'gem', roles } as unknown as SignedInUser });

function drawn(): string {
  const client = new QueryClient({ defaultOptions: { queries: { retryOnMount: false } } });
  client.setQueryData(['outgoing-webhooks', PROJECT], webhooks);
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <MantineProvider theme={theme} cssVariablesResolver={cssVariablesResolver}>
        <OutgoingWebhookSettings />
      </MantineProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => signedInAs(['ADMIN']));

describe('the outgoing webhooks card', () => {
  it('names each webhook, where it goes and what it is sent, in words', () => {
    const html = drawn();
    expect(html).toContain('Finance ledger');
    expect(html).toContain('https://ledger.example.com/hooks/metis');
    expect(html).toContain('Process completed');
    expect(html).toContain('Incident raised');
  });

  it('offers an administrator the ways to add, try and read a webhook', () => {
    const html = drawn();
    for (const control of ['New outgoing webhook', 'Send test', 'Deliveries', 'Delete the Finance ledger webhook']) {
      expect(html).toContain(control);
    }
  });

  it('shows anybody else what is sent where, and no control the server would refuse them', () => {
    signedInAs(['DESIGNER']);
    const html = drawn();
    expect(html).toContain('Finance ledger');
    for (const control of ['New outgoing webhook', 'Send test', 'Deliveries', 'Delete the Finance ledger webhook']) {
      expect(html).not.toContain(control);
    }
  });
});

const STYLESHEET_FALLBACKS: Record<string, { background: string; text: string }> = {
  Button: { background: 'var(--mantine-primary-color-filled)', text: 'var(--mantine-color-white)' },
  Badge: { background: 'var(--mantine-primary-color-filled)', text: 'var(--mantine-color-white)' },
};

function labelContrast(html: string, component: 'Button' | 'Badge', label: string, scheme: ColourScheme): number {
  const properties = customPropertiesOf(html, `mantine-${component}-root`, label);
  const prefix = `--${component.toLowerCase()}`;
  const variables = schemeVariables(scheme);
  const background = resolveColour(properties[`${prefix}-bg`] ?? STYLESHEET_FALLBACKS[component].background, variables);
  const text = resolveColour(properties[`${prefix}-color`] ?? STYLESHEET_FALLBACKS[component].text, variables);
  return contrastRatio(text, background);
}

describe('the outgoing webhooks card, in either colour scheme', () => {
  for (const scheme of ['light', 'dark'] as ColourScheme[]) {
    it(`draws "New outgoing webhook" readably in ${scheme} mode`, () => {
      const ratio = labelContrast(drawn(), 'Button', 'New outgoing webhook', scheme);
      expect(ratio, `the button is ${ratio.toFixed(2)}:1 in ${scheme} mode`).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
    });

    it(`draws the events each webhook is sent readably in ${scheme} mode`, () => {
      const html = drawn();
      for (const label of ['Process completed', 'Incident raised']) {
        const ratio = labelContrast(html, 'Badge', label, scheme);
        expect(ratio, `the ${label} badge is ${ratio.toFixed(2)}:1 in ${scheme} mode`).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
      }
    });
  }
});
