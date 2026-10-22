/**
 * The outgoing webhooks card, as an administrator and as anybody else sees it,
 * read in both colour schemes.
 */
import { afterAll, beforeEach, describe, expect, it, mock } from 'bun:test';
import { MantineProvider, Table } from '@mantine/core';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

import type { ApiOutgoingDelivery, ApiOutgoingTest, ApiOutgoingWebhook } from '../../services/domains/outgoingWebhookService';
import { standInForAppStore, type SignedInUser } from '../../test/appStoreStandIn';
import { customPropertiesOf, resolveColour, schemeVariables, type ColourScheme } from '../../testing/themeColours';
import { cssVariablesResolver, theme } from '../../theme';
import { AA_NORMAL_TEXT, contrastRatio } from '../../theme/contrast';
import { DeliveryRow } from './OutgoingDeliveryLog';
import { OutgoingWebhookSettings } from './OutgoingWebhookSettings';
import { OutgoingTestResult } from './SendOutgoingTest';

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

function themed(element: ReactElement): string {
  return renderToStaticMarkup(
    <MantineProvider theme={theme} cssVariablesResolver={cssVariablesResolver}>
      {element}
    </MantineProvider>,
  );
}

/**
 * The contrast a label is drawn at, wherever it is drawn: on a badge's own
 * surface when it is in one, and in the page's text colour on the page when
 * it is plain text or a dot badge, whose label keeps the page's colours.
 */
function drawnContrast(html: string, label: string, scheme: ColourScheme): number {
  const variables = schemeVariables(scheme);
  const page = () =>
    contrastRatio(resolveColour('var(--mantine-color-text)', variables), resolveColour('var(--mantine-color-body)', variables));
  const inBadge = new RegExp(`mantine-Badge-root"[^>]*>(?:<span[^>]*>)?${label}<`).exec(html);
  if (!inBadge) return page();
  const tag = html.slice(html.lastIndexOf('<div', inBadge.index), html.indexOf('>', inBadge.index));
  if (tag.includes('data-variant="dot"')) return page();
  return labelContrast(html, 'Badge', label, scheme);
}

const delivery = (overrides: Partial<ApiOutgoingDelivery>): ApiOutgoingDelivery => ({
  id: 'd-1',
  webhook_id: 'o-1',
  event: 'process.completed',
  status: 'delivered',
  attempts: 1,
  last_status_code: 200,
  body: {},
  created_at: '2026-10-21T10:00:00Z',
  ...overrides,
});

const OUTCOMES: [string, ApiOutgoingDelivery][] = [
  ['200 · delivered', delivery({})],
  ['503 · retrying', delivery({ status: 'pending', attempts: 3, last_status_code: 503 })],
  ['401 · gave up', delivery({ status: 'failed', attempts: 10, last_status_code: 401 })],
  ['418 · test refused', delivery({ status: 'failed', test: true, last_status_code: 418 })],
];

const test: ApiOutgoingTest = {
  delivery_id: 't-1',
  event: 'process.completed',
  accepted: true,
  duration_ms: 42,
  request: { method: 'POST', url: 'https://ledger.example.com/hooks/metis', headers: {}, body: {} },
  response: { status_code: 200, body: 'ok' },
};

// A delivery's outcome was a tinted badge and a test's status a filled one.
// Green text on its tint measured 3.81:1 and yellow 2.69:1 in light mode, and
// white on the filled green 3.45:1 in both, all under AA.
describe('what came of a delivery, in either colour scheme', () => {
  for (const scheme of ['light', 'dark'] as ColourScheme[]) {
    it(`draws each delivery's outcome readably in ${scheme} mode`, () => {
      for (const [label, row] of OUTCOMES) {
        const html = themed(
          <Table>
            <Table.Tbody>
              <DeliveryRow delivery={row} selected={false} onOpen={() => {}} />
            </Table.Tbody>
          </Table>,
        );
        const ratio = drawnContrast(html, label, scheme);
        expect(ratio, `"${label}" is ${ratio.toFixed(2)}:1 in ${scheme} mode`).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
      }
    });

    it(`draws a test's status readably in ${scheme} mode`, () => {
      const ratio = drawnContrast(themed(<OutgoingTestResult test={test} />), '200', scheme);
      expect(ratio, `the status is ${ratio.toFixed(2)}:1 in ${scheme} mode`).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
    });
  }
});
