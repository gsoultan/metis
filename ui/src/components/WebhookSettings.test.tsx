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

import type { ApiWebhook } from '../services/domains/webhookService';
import { standInForAppStore } from '../test/appStoreStandIn';
import { customPropertiesOf, resolveColour, schemeVariables, type ColourScheme } from '../testing/themeColours';
import { cssVariablesResolver, theme } from '../theme';
import { AA_NORMAL_TEXT, contrastRatio } from '../theme/contrast';
import { CloseLegacyWindow } from './webhooks/CloseLegacyWindow';
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
