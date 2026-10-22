/**
 * The webhooks card, read in both colour schemes.
 *
 * In dark mode an accessibility scan failed the card's "Add a webhook" button
 * at 2.99:1 and its message badges at 2.14:1, where AA asks for 4.5:1: the
 * button fell back to Mantine's dark primary shade under a white label chosen
 * for the light one, and the badges kept the dark text they are given on a
 * light tint. The same light variant made the legacy-signature dialog's red
 * button 2.30:1. These are the colours each control is drawn in, measured the
 * way the page resolves them in each scheme.
 */
import { afterAll, beforeEach, describe, expect, it, mock } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

import type { ApiWebhook, ApiWebhookSimulation } from '../services/domains/webhookService';
import { standInForAppStore } from '../test/appStoreStandIn';
import { customPropertiesOf, resolveColour, schemeVariables, type ColourScheme } from '../testing/themeColours';
import { cssVariablesResolver, theme } from '../theme';
import { AA_NORMAL_TEXT, contrastRatio } from '../theme/contrast';
import { CloseLegacyWindow } from './webhooks/CloseLegacyWindow';
import { SimulationResult } from './webhooks/SimulateDelivery';
import { WebhookSettings } from './WebhookSettings';

const store = standInForAppStore((specifier, factory) => mock.module(specifier, factory));
afterAll(() => store.restore());

const PROJECT = 'project-1';

beforeEach(() => {
  store.set({ currentProjectId: PROJECT, token: 'a-session' });
});

const webhooks: ApiWebhook[] = [
  { id: 'w-1', name: 'Stripe payments', token: 'tok-stripe-1', message_name: 'payment.received', correlation_expression: 'order.id', enabled: true },
  { id: 'w-2', name: 'Carrier updates', token: 'tok-carrier-2', message_name: 'shipment.updated', enabled: true },
];

/**
 * Drawn as the app draws it: the app's theme, and the card's webhooks already
 * fetched.
 *
 * Each webhook's row builds its delivery address from the page's origin, and
 * a server render has no page; the origin is lent for the render and taken
 * back after it.
 */
function drawn(element: ReactElement): string {
  const client = new QueryClient({ defaultOptions: { queries: { retryOnMount: false } } });
  client.setQueryData(['webhooks', PROJECT], { webhooks });
  const page = globalThis as { window?: unknown };
  page.window = { location: { origin: 'https://metis.example' } };
  try {
    return renderToStaticMarkup(
      <QueryClientProvider client={client}>
        <MantineProvider theme={theme} cssVariablesResolver={cssVariablesResolver}>
          {element}
        </MantineProvider>
      </QueryClientProvider>,
    );
  } finally {
    delete page.window;
  }
}

/**
 * Where a control's colours come from when Mantine writes none of its own:
 * the component stylesheet's fallbacks, `var(--button-bg, var(--mantine-primary-color-filled))`
 * and the like.
 */
const STYLESHEET_FALLBACKS: Record<string, { background: string; text: string }> = {
  Button: { background: 'var(--mantine-primary-color-filled)', text: 'var(--mantine-color-white)' },
  Badge: { background: 'var(--mantine-primary-color-filled)', text: 'var(--mantine-color-white)' },
};

/** The contrast of a control's label against its own surface, in a colour scheme. */
function labelContrast(html: string, component: 'Button' | 'Badge', label: string, scheme: ColourScheme): number {
  const properties = customPropertiesOf(html, `mantine-${component}-root`, label);
  const prefix = `--${component.toLowerCase()}`;
  const variables = schemeVariables(scheme);
  const background = resolveColour(properties[`${prefix}-bg`] ?? STYLESHEET_FALLBACKS[component].background, variables);
  const text = resolveColour(properties[`${prefix}-color`] ?? STYLESHEET_FALLBACKS[component].text, variables);
  return contrastRatio(text, background);
}

const SCHEMES: ColourScheme[] = ['light', 'dark'];

describe('the webhooks card, in either colour scheme', () => {
  for (const scheme of SCHEMES) {
    it(`draws "Add a webhook" readably in ${scheme} mode`, () => {
      const ratio = labelContrast(drawn(<WebhookSettings />), 'Button', 'Add a webhook', scheme);
      expect(ratio, `"Add a webhook" is ${ratio.toFixed(2)}:1 in ${scheme} mode`).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
    });

    it(`draws the message each webhook becomes readably in ${scheme} mode`, () => {
      const html = drawn(<WebhookSettings />);
      for (const { message_name: message } of webhooks) {
        const ratio = labelContrast(html, 'Badge', message, scheme);
        expect(ratio, `the ${message} badge is ${ratio.toFixed(2)}:1 in ${scheme} mode`).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
      }
    });

    it(`draws the way out of a legacy window readably in ${scheme} mode`, () => {
      const html = drawn(<CloseLegacyWindow hookId="w-2" deadline="Dec 25, 2026, 9:00 AM" />);
      const label = 'Stop accepting legacy signatures now';
      const ratio = labelContrast(html, 'Button', label, scheme);
      expect(ratio, `"${label}" is ${ratio.toFixed(2)}:1 in ${scheme} mode`).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
    });
  }
});

const simulated: ApiWebhookSimulation = {
  would_act: true,
  enabled: true,
  signature: { checked: true, valid: true },
  message_name: 'payment.received',
  correlation_expression: 'order.id',
  correlation_key: 'PO-4182',
  waiting: [
    {
      instance_id: 'i-1',
      process_name: 'Purchase approval',
      business_key: 'PO-4182',
      node_id: 'wait',
      node_name: 'Wait for payment',
      since: '2026-10-21T09:00:00Z',
    },
  ],
  waiting_total: 3,
};

const VERDICTS: [string, ApiWebhookSimulation][] = [
  ['A real delivery would be acted on', simulated],
  ['A real delivery would change nothing', { ...simulated, would_act: false, already_received: true }],
  ['A real delivery would be refused', { ...simulated, would_act: false, refusal: 'This webhook is switched off.' }],
];

describe('trying a delivery', () => {
  it('is offered on every webhook', () => {
    const html = drawn(<WebhookSettings />);
    expect(html.match(/Try a delivery/g)?.length).toBe(webhooks.length);
  });

  it('names what it would move, and says how many more it did not name', () => {
    const html = drawn(<SimulationResult result={simulated} />);
    expect(html).toContain('Dry run · nothing changed');
    expect(html).toContain('Purchase approval · PO-4182');
    expect(html).toContain('Waiting at &quot;Wait for payment&quot;');
    expect(html).toContain('and 2 more');
  });

  it('says what a real delivery would do in words, for each outcome', () => {
    for (const [verdict, result] of VERDICTS) {
      expect(drawn(<SimulationResult result={result} />)).toContain(verdict);
    }
  });
});
